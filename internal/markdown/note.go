package markdown

import (
	"strings"

	"github.com/schuettc/galley/internal/docmodel"
)

// note.go is the one place galley WRITES CriticMarkup comment syntax back
// into a file, and the rules that make reading it back unambiguous.
//
// # Comment ID marks
//
// A reviewer comment's words live in the unsent round (internal/unsent's
// pending.json), never in the .md. The file carries only a MARK at the
// comment's place, naming the comment by its ID:
//
//	{==words==}{>>@comment cm-…<<}   a text comment: one mark after EACH
//	                                 highlighted piece, flush against "==}"
//	{>>@comment cb-…<<}              a block comment: on a line of its own
//	                                 after the block, or inside the cell for a
//	                                 table cell
//	(nothing)                        a document comment
//
// The first is bound to its highlight by the scanner (critic.go's
// stampCommentID) and becomes the Highlight mark's docmodel.CommentIDAttr. The
// second is a Note block carrying the ID and no words. galley links a comment
// to its place by that ID and nothing else.
//
// An ID is one token of letters, digits and hyphens (commentID). A body that
// says "@comment" and anything else is not a mark, and reads as the ordinary
// note it looks like.
//
// # Notes without an ID
//
// A {>>note<<} has always meant "a comment attached at this position in this
// paragraph's text" — a RANGE anchor, lifted out of the document into an
// InlineComment and never serialized again (see critic.go's InlineComment).
// Two anchors have no position to be lifted out of:
//
//   - BLOCK: the comment is about an image, a code fence, a whole paragraph.
//     There is no text run to highlight — docmodel blocks carry no marks —
//     so there is nothing in the file to hang the anchor on.
//   - DOCUMENT: the comment is about the file. There is not even a block.
//
// A hand-typed note on a line of its own therefore comes back as a
// docmodel.Note BLOCK rather than as an out-of-band InlineComment, and it
// round-trips byte for byte. The grammar below decides which reading a
// {>>…<<} gets.
//
// # The discriminator
//
// Reading a {>>…<<} back has to answer two questions, and they are answered by
// two different mechanisms on purpose.
//
// 1. RANGE vs. BLOCK-LEVEL — decided by POSITION, not by any marker.
//
//	A {>>…<<} is block-level exactly when it is the ENTIRE content of a
//	paragraph: nothing else survives that paragraph once the marker is
//	removed, and it is the only note in it. Anything else — a note at the
//	end of a sentence, a note between two words, two notes on one line — is
//	a range comment at its offset, exactly as before.
//
//	This is what stops a genuine inline comment at the end of a paragraph
//	from being read as a document comment: "The build is slow. {>>why?<<}"
//	leaves "The build is slow." behind, so the paragraph is not empty and
//	the note is a range anchor at offset 18 — even when that paragraph is
//	the last block in the file. Only a note standing alone on its own line,
//	with no prose of its own, is block-level at all.
//
//	Position is the right signal here because it is the one a HAND-EDITOR
//	already understands. Someone who types a note on its own line under a
//	diagram means it to be about the diagram; someone who types it after a
//	sentence means it to be about the sentence. No marker has to be learned,
//	and moving the line moves the anchor, which is what moving it looks
//	like it should do.
//
// 2. BLOCK vs. DOCUMENT — decided by an explicit MARKER, not by position.
//
//	Position cannot answer this one. A standalone note at the end of a file
//	is equally plausibly "a comment on the last block" and "a comment on the
//	whole document", and guessing wrong is not symmetric: mis-reading a
//	last-block comment as a document comment DISCARDS the anchor (there is
//	no way to recover which block it meant), while mis-reading a document
//	comment as a last-block comment leaves it attached to something real and
//	adjacent to where it was written. So the default is the recoverable one
//	— a standalone note is a BLOCK comment on the block above it — and a
//	document comment says so out loud:
//
//	    {>>@document this whole spec needs a worked example<<}
//
//	A hand-written {>>…<<} at the end of a file therefore anchors to the
//	last block. That is not a mis-read to apologise for: the note IS next to
//	that block, and `galley pending` prints which block it landed on, so the
//	author can see it and add @document if they meant the file.
//
// 3. The escape. "@block" is the same marker spelled for the other anchor,
//
//	and exists so a note whose own text begins "@document …" can still be a
//	block comment: it is written "{>>@block @document …<<}". It is only ever
//	emitted when the text would otherwise be misread, so ordinary files never
//	carry it.
//
// A marker is one leading whitespace-delimited word. Everything after the
// first space is the note's text, verbatim.
const (
	docMarker     = "@document"
	blockMarker   = "@block"
	commentMarker = "@comment"
)

// commentID reads a note body as a comment ID mark: "@comment" and one token
// of ASCII letters, digits and hyphens, nothing else. ok is false for any
// other body, which is then an ordinary note.
func commentID(body string) (id string, ok bool) {
	word, rest, _ := strings.Cut(body, " ")
	if word != commentMarker || !validCommentID(rest) {
		return "", false
	}
	return rest, true
}

// validCommentID reports whether id can be written inside an ID mark and read
// back as the same ID. galley mints "cm-"/"cb-"/"cd-" and sixteen hex digits;
// the grammar accepts the wider token so the reader never has to know the
// minter's spelling.
func validCommentID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// commentMark is the ID mark's spelling, without its "{>>" and "<<}".
func commentMark(id string) string { return commentMarker + " " + id }

// splitNoteMarker reads a note body's leading anchor marker, returning the
// anchor it names and the text after it. With no marker the anchor is block —
// the recoverable default; see this file's header.
func splitNoteMarker(body string) (anchor, text string) {
	word, rest, _ := strings.Cut(body, " ")
	switch word {
	case docMarker:
		return docmodel.AnchorDocument, rest
	case blockMarker:
		return docmodel.AnchorBlock, rest
	}
	return docmodel.AnchorBlock, body
}

// noteBody is splitNoteMarker's inverse: the body text to write between
// "{>>" and "<<}" for a note with this anchor and this text.
//
// A block note takes the explicit "@block" marker only when its text would
// otherwise be read as a marker — which is the whole reason "@block" exists.
func noteBody(anchor, text string) string {
	if anchor == docmodel.AnchorDocument {
		if text == "" {
			return docMarker
		}
		return docMarker + " " + text
	}
	if startsWithMarker(text) {
		if text == "" {
			return blockMarker
		}
		return blockMarker + " " + text
	}
	return text
}

// startsWithMarker reports whether a note with these words would be misread
// as a marker. An "@comment" note is misread only when it spells a whole ID
// mark; any other note that begins "@comment" already reads back as itself,
// and escaping it would rewrite a line the author typed.
func startsWithMarker(text string) bool {
	word, _, _ := strings.Cut(text, " ")
	if _, ok := commentID(text); ok {
		return true
	}
	return word == docMarker || word == blockMarker
}

// noteBlock builds the docmodel.Note a standalone {>>…<<} parses to. Its text
// is one unmarked run: a comment is a note, not a document, so criticPass has
// already flattened whatever markup was inside it (see cellText).
//
// An ID mark is a Note with the ID and no words: the words are in the unsent
// round, not in the file.
func noteBlock(body string) docmodel.Block {
	if id, ok := commentID(body); ok {
		return NewCommentNote(id)
	}
	anchor, text := splitNoteMarker(body)
	b := docmodel.Block{Kind: docmodel.Note, Attrs: map[string]string{"anchor": anchor}}
	if text != "" {
		b.Inlines = []docmodel.Inline{{Text: text}}
	}
	return b
}

// NoteAnchor reports a Note block's anchor kind, defaulting to block for a
// block whose attrs were built by hand and left it out.
func NoteAnchor(b docmodel.Block) string {
	if b.Attrs["anchor"] == docmodel.AnchorDocument {
		return docmodel.AnchorDocument
	}
	return docmodel.AnchorBlock
}

// NoteText is a Note block's comment text.
func NoteText(b docmodel.Block) string {
	var out strings.Builder
	for _, in := range b.Inlines {
		out.WriteString(in.Text)
	}
	return out.String()
}

// NewNote builds a Note block — the constructor every producer outside this
// package uses, so the attrs are spelled in exactly one place.
func NewNote(anchor, text string) docmodel.Block {
	if anchor != docmodel.AnchorDocument {
		anchor = docmodel.AnchorBlock
	}
	b := docmodel.Block{Kind: docmodel.Note, Attrs: map[string]string{"anchor": anchor}}
	if text != "" {
		b.Inlines = []docmodel.Inline{{Text: text}}
	}
	return b
}

// NewCommentNote builds a block comment's mark: a Note carrying the comment's
// ID and no words.
func NewCommentNote(id string) docmodel.Block {
	return docmodel.Block{Kind: docmodel.Note, Attrs: map[string]string{
		"anchor":               docmodel.AnchorBlock,
		docmodel.CommentIDAttr: id,
	}}
}

// UnwritableNoteText reports whether text cannot be written inside a
// {>>…<<} without corrupting it.
//
// CriticMarkup has no escape mechanism and its reader closes a span at the
// FIRST matching closer, so a note whose own text contains "<<}" would be cut
// short and the remainder would fall out into the document as prose. There is
// no alternate spelling for a comment the way there is for a deletion (see
// render_inline.go's wrapSuggestion), so the only honest answers are "refuse
// it at the API" — which is what the suggest layer does with this — and
// "write the words and lose the marker", which is what renderNote falls back
// to for a note that got into a document some other way.
//
// A newline is refused for the same reason: the marker has to occupy ONE line
// to be a block of its own, and a note broken across two lines would be read
// back as a paragraph with a stray "<<}" in it.
func UnwritableNoteText(text string) bool {
	return strings.Contains(text, "<<}") || strings.ContainsAny(text, "\r\n")
}

// renderNote writes a Note block as "{>>body<<}" on a line of its own.
//
// The body is planned as ESCAPABLE content, not as literal runes, and that is
// load-bearing: goldmark parses the paragraph before critic.go's scanner ever
// runs, so an unescaped "*" inside the note is consumed as an emphasis
// delimiter and simply disappears on the way back in. Escaping it is what
// makes the note's text survive verbatim.
//
// A note whose text cannot be spelled at all (see UnwritableNoteText) is
// written as PLAIN TEXT with the markers dropped. Same ruling as
// wrapSuggestion's: losing the anchor is recoverable from the sidecar, losing
// the author's words is not.
func renderNote(b docmodel.Block) string {
	return spellNote(b, lineContext{atLineStart: true})
}

// renderCellNote writes a Note block that is the whole content of a TABLE
// CELL. It is renderNote with the other line context, and it exists because a
// cell is the THIRD anchor case this file's header did not consider.
//
// The grammar's first discriminator is POSITION: a {>>…<<} is block-level
// exactly when it is the entire content of its paragraph. A cell holds blocks,
// and a cell whose only content is a note holds a paragraph that IS the note,
// so extractCriticBlocks promotes it exactly as it does in prose — and it is
// right to. The RANGE reading collapses here: a range anchor needs text to sit
// at an offset in, and this cell has none left once the marker is removed.
//
// The third option — declining the promotion inside a table, leaving an inline
// comment on the cell — is the WORST of the three, and it is worse in the
// direction this package is scarred by. criticPass empties the paragraph, the
// cell renders "| |", and the reviewer's comment leaves the .md entirely with
// an anchor pointing at an empty string. That is precisely the outcome
// extractCriticBlocks' "nothing survived" rule was written to make impossible,
// and the rule does not stop applying because the paragraph has a "|" either
// side of it.
//
// Without this, renderCell fell through to the inlines case and wrote the
// note's BARE TEXT: "| x | {>>note here<<} | z |" came back
// "| x | note here | z |", and the comment read as prose after opening a file
// and saving it. An ID mark in a cell is the same shape and needs the same
// case: it is how a table cell's block comment keeps its place.
//
// "@document" IS HONOURED IN A CELL. Position cannot answer block-vs-document
// anywhere — that is why the marker exists — so a cell, being a position, gets
// no vote either. Rewriting the marker out because the note happens to sit in a
// table would be this same loss one word smaller.
//
// lineContext{} rather than atLineStart: the "| " prefix is already written, so
// the note's first rune does not lead a line and nothing in it needs the
// block-leading escapes. escapeCellText then escapes any "|" in the note's own
// text, including one inside the marker pair — a raw pipe there would split the
// row and take the rest of the comment into the next cell.
func renderCellNote(b docmodel.Block) string {
	return spellNote(b, lineContext{})
}

// spellNote writes a note's body between "{>>" and "<<}". A BLOCK note
// carrying a comment ID is written as its ID mark and nothing else: the
// comment's words are not the file's to hold.
//
// A DOCUMENT note never is, whatever it carries. An ID mark always reads back
// as a block note, so spelling one for a document note would turn a comment on
// the whole file into a comment on the block above it. Document comments have
// no mark at all; a document note that arrives with an id anyway is written as
// the @document form it is, without the id.
func spellNote(b docmodel.Block, ctx lineContext) string {
	if id := b.Attrs[docmodel.CommentIDAttr]; validCommentID(id) && NoteAnchor(b) == docmodel.AnchorBlock {
		return renderPlan(literalChars("{>>"+commentMark(id)+"<<}"), ctx)
	}
	text := NoteText(b)
	if UnwritableNoteText(text) {
		return renderInlines([]docmodel.Inline{{Text: text}}, ctx)
	}
	plan := literalChars("{>>")
	plan = append(plan, contentChars(noteBody(NoteAnchor(b), text))...)
	plan = append(plan, literalChars("<<}")...)
	return renderPlan(plan, ctx)
}
