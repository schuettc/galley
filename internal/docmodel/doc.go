// Package docmodel defines the document tree shared by every stage of
// galley's editor: the markdown parser produces it, ygo's CRDT mirrors it,
// and the suggestion layer (Ins/Del/Highlight marks) annotates it in place.
// It has no dependency on any of those — they depend on it.
package docmodel

import "reflect"

// Doc is a document: an ordered list of top-level blocks.
type Doc struct {
	Blocks []Block
}

// BlockKind identifies the kind of a Block.
type BlockKind string

const (
	Paragraph   BlockKind = "paragraph"
	Heading     BlockKind = "heading"     // Attrs["level"] = "1".."6"
	CodeBlock   BlockKind = "codeBlock"   // Attrs["language"]; Text carries raw code
	Blockquote  BlockKind = "blockquote"  // Children
	BulletList  BlockKind = "bulletList"  // Children are ListItem
	OrderedList BlockKind = "orderedList" // Children are ListItem
	ListItem    BlockKind = "listItem"    // Children are blocks
	Rule        BlockKind = "horizontalRule"
	Image       BlockKind = "image" // Attrs["src"], Attrs["alt"]

	// A GFM table, in TipTap's shape rather than goldmark's.
	//
	// goldmark distinguishes a header ROW (extast.TableHeader) from a body
	// row; ProseMirror does not — it has one row node and two kinds of CELL.
	// This follows ProseMirror, because these strings are the element tags
	// written into the CRDT and the node names the browser's schema binds to.
	//
	// A cell's content is BLOCKS, not inlines: TipTap's tableCell and
	// tableHeader are both "block+", so a cell's text lives in a Paragraph
	// child exactly as a ListItem's does. Writing the text straight onto the
	// cell would produce a document ygo stores happily and y-prosemirror
	// cannot build a node from.
	Table       BlockKind = "table"       // Children are TableRow
	TableRow    BlockKind = "tableRow"    // Children are TableCell or TableHeader
	TableCell   BlockKind = "tableCell"   // Children are blocks; Attrs["align"]
	TableHeader BlockKind = "tableHeader" // Children are blocks; Attrs["align"]

	// Note is a comment that anchors to something other than a range of
	// prose: Attrs["anchor"] is "block" (the note is about the block it
	// follows) or "document" (the note is about the whole file). Its text
	// lives in Inlines as a single unmarked run — a comment is a note, not a
	// document, so it carries no formatting.
	//
	// A block comment's Note carries Attrs[CommentIDAttr] and no Inlines:
	// its words live in the unsent round, and the file holds only its ID
	// mark, "{>>@comment cb-…<<}" on a line of its own after the block it is
	// about. A range comment has a Highlight mark to hang its ID on; a block
	// comment has nowhere to attach, so its mark has to BE a block.
	//
	// A Note with words and no ID is a hand-typed "{>>…<<}" on its own line.
	// It parses and round-trips as before, but nothing links it to a comment.
	Note BlockKind = "note"

	// FrontMatter is the YAML ("---") or TOML ("+++") metadata block a file may
	// open with. Text carries the WHOLE block VERBATIM — both delimiter lines,
	// the body between them, and the closing delimiter's newline — because
	// galley models that it is THERE and deliberately models nothing about what
	// is inside it.
	//
	// It is a block rather than a field on Doc for the reason review.File's
	// entry in CLAUDE.md gives: every stage of this pipeline rebuilds a Doc
	// from its Blocks (suggest's clone, markdown's extractCritic, ydoc's Read),
	// and a field beside them is a field each of those has to remember to
	// carry. A block is carried by all of them and by the CRDT bridge without
	// anything being told about it.
	//
	// It carries no Inlines, so suggest.List cannot reach it and no mark can be
	// hung on it — the same standing reason a code fence is literal text. Parse
	// only ever produces one, at index 0.
	FrontMatter BlockKind = "frontMatter"

	// MathBlock is a display-math block — `$$` on its own line, some TeX, `$$`
	// on its own line. Text carries the WHOLE block VERBATIM, both delimiters
	// included, for FrontMatter's reason exactly: galley models that it is
	// THERE and models nothing about what is inside it.
	//
	// FrontMatter's entry says what galley needs is where the block ends and
	// which bytes it occupies; this is the second construct that answers to
	// that description, and the discriminator is whose language the content is
	// in. YAML, code and TeX are not markdown — there is no prose inside to
	// review, no mark to hang on it and nothing galley could be right or wrong
	// about. Where the content IS markdown prose (a `:::` directive, a
	// `> [!NOTE]` callout, a definition list) carrying it verbatim would make
	// the author's own sentences unreviewable, and markdown.Parse REFUSES those
	// with a line number instead.
	//
	// It carries no Inlines, so suggest.List cannot reach it and no mark can be
	// hung on it — the same standing reason a code fence is literal text.
	MathBlock BlockKind = "mathBlock"
)

// Anchor values for a Note block's Attrs["anchor"].
const (
	AnchorBlock    = "block"
	AnchorDocument = "document"
)

// CommentIDAttr is the Attrs key carrying a reviewer comment's ID: on a
// Highlight mark it names the comment that piece of highlighted text belongs
// to, and on a Note block it names the block comment that note marks.
//
// Unlike RunAttr it IS serialized — it is the only thing the file carries for
// a comment. The words live in the unsent round (internal/unsent), and the .md
// holds a mark at the comment's place: "{>>@comment cm-…<<}" after each
// highlighted piece, "{>>@comment cb-…<<}" on a line of its own after a block.
// galley links a comment to its place by this ID and nothing else.
//
// The two keys are not the same identity. A comment's ID is minted once and
// stored; a run is minted per session. Pieces that share an ID share a run
// (markdown.Parse stamps it), so the run stays the session's grouping
// coordinate and the ID is what survives the file.
const CommentIDAttr = "id"

// AlignAttr is the Attrs key carrying a table cell's column alignment:
// "left", "right", "center", or absent for GFM's default.
//
// It rides on the CELL, which is where goldmark puts it and where TipTap can
// render it, rather than on the table — but the file has exactly one place to
// write it, the delimiter row, so serialization reads it from the HEADER row's
// cells and every other cell's copy is advisory. A hand-built document whose
// body cells disagree with its header is written the header's way and reads
// back agreeing with itself.
const AlignAttr = "align"

// Block is a single block-level node in the document tree. Which fields are
// meaningful depends on Kind: Paragraph and Heading carry Inlines, the RAW-TEXT
// kinds (CodeBlock, FrontMatter, MathBlock) carry their bytes in Text, and
// container kinds (Blockquote, the list kinds, ListItem, Table, TableRow,
// TableCell, TableHeader) carry Children.
type Block struct {
	Kind    BlockKind
	Attrs   map[string]string
	Inlines []Inline // Paragraph, Heading
	// Text is the raw-text kinds' content, verbatim. It said "CodeBlock only"
	// while two other kinds already used it — Note's one unmarked run goes
	// through Inlines, but FrontMatter and MathBlock are here, and ydoc's
	// rawText() is the one predicate that names the set.
	Text     string  // CodeBlock, FrontMatter, MathBlock
	Children []Block // Blockquote, lists
}

// MarkKind identifies the kind of a Mark.
type MarkKind string

const (
	Bold      MarkKind = "bold"
	Italic    MarkKind = "italic"
	Code      MarkKind = "code"
	Link      MarkKind = "link"      // Attrs["href"]
	Ins       MarkKind = "ins"       // suggestion: insertion
	Del       MarkKind = "del"       // suggestion: deletion
	Highlight MarkKind = "highlight" // suggestion: comment anchor
	HardBreak MarkKind = "hardBreak" // zero-text sentinel inline
)

// RunAttr is the Attrs key carrying a suggestion mark's run: an opaque token,
// unique within a loaded document, naming ONE AUTHORED EDIT.
//
// One edit, not one mark — a run is shared by every inline a single edit
// touched, and a target of plain prose crossing a code span touches three. That
// is what makes it a decision rather than a coordinate, and it is why
// suggest.CommentOn and its siblings stamp it themselves rather than leaving
// it to suggest.MintRuns, which sees marks and cannot see edits. Two edits over
// identical adjacent text still get two runs and stay two decisions; that is
// what runs were introduced for and neither minter may collapse it.
//
// suggest.MintRuns mints for everything else as a document enters the edit
// session, and it is deliberately NOT serialized. CriticMarkup has no slot for
// it, and a run is a session coordinate, not an identity: what a comment keeps
// across sessions is its ID (CommentIDAttr), below.
//
// So a run identifies a mark for as long as the document is loaded, and is
// re-minted next time it loads. That is the right scope. Within a session —
// which is when the editor needs to say "this card points at THAT mark" — it
// is exact, and two suggestions over identical text stay distinguishable.
// Across sessions, identity is a different question, already answered
// differently: a comment by the ID written into the file after its mark
// (CommentIDAttr). Do not merge the two. Collapsing them is what produced
// the unstable ordinal thread keys that were a Critical in phase 1.
const RunAttr = "run"

// Mark is a span-level annotation on an Inline. Suggestion marks (Ins, Del,
// Highlight) carry "author" and "at" (RFC3339) in Attrs, and — once the
// document is in a session — RunAttr.
type Mark struct {
	Kind  MarkKind
	Attrs map[string]string // suggestions carry "author", "at" (RFC3339), "run"
}

// Inline is a run of text within a Paragraph or Heading block, annotated by
// zero or more marks.
type Inline struct {
	Text  string
	Marks []Mark
}

// Has reports whether i carries a mark of the given kind.
func (i Inline) Has(kind MarkKind) bool {
	for _, m := range i.Marks {
		if m.Kind == kind {
			return true
		}
	}
	return false
}

// Attr returns the value of key on i's mark of the given kind, or "" if the
// inline has no such mark or the mark has no such key.
func (i Inline) Attr(kind MarkKind, key string) string {
	for _, m := range i.Marks {
		if m.Kind == kind {
			return m.Attrs[key]
		}
	}
	return ""
}

// Equal reports whether a and b are structurally identical. nil and empty
// slices/maps compare equal — a doc parsed from markdown and a doc built by
// hand must compare equal when they are semantically the same document, even
// though the parser and the builder don't agree on nil vs. empty.
//
// RunAttr IS EXCLUDED, for the same reason and a stronger one. A run is
// session identity, minted at random and never serialized, so parsing the same
// bytes twice yields the same document with different runs — and the fixed
// point this codebase is built on, "Parse(Serialize(Parse(x))) equals
// Parse(x)", would be unstatable if Equal disagreed. A run is not content.
//
// So Equal does not assert anything about runs, and the places that must are
// explicit about it rather than relying on this: markdown's parser stamping one
// per span in the file (internal/markdown, applyMark), suggest's transforms
// stamping one per authored edit, suggest.MintRuns' idempotence, and — since
// nothing else covers it now — ydoc's bridge carrying them through the CRDT
// unchanged. Do not "restore" run comparison here; add an assertion where the
// run actually matters.
func Equal(a, b Doc) bool {
	return reflect.DeepEqual(normalizeDoc(a), normalizeDoc(b))
}

func normalizeDoc(d Doc) Doc {
	return Doc{Blocks: normalizeBlocks(d.Blocks)}
}

func normalizeBlocks(blocks []Block) []Block {
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		out[i] = normalizeBlock(b)
	}
	return out
}

func normalizeBlock(b Block) Block {
	return Block{
		Kind:     b.Kind,
		Attrs:    normalizeAttrs(b.Attrs),
		Inlines:  normalizeInlines(b.Inlines),
		Text:     b.Text,
		Children: normalizeBlocks(b.Children),
	}
}

func normalizeInlines(inlines []Inline) []Inline {
	out := make([]Inline, len(inlines))
	for i, in := range inlines {
		out[i] = Inline{Text: in.Text, Marks: normalizeMarks(in.Marks)}
	}
	return out
}

func normalizeMarks(marks []Mark) []Mark {
	out := make([]Mark, len(marks))
	for i, m := range marks {
		out[i] = Mark{Kind: m.Kind, Attrs: normalizeMarkAttrs(m.Attrs)}
	}
	return out
}

// normalizeMarkAttrs is normalizeAttrs minus the run — see Equal.
func normalizeMarkAttrs(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if k == RunAttr {
			continue
		}
		out[k] = v
	}
	return out
}

func normalizeAttrs(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

// Walk visits every block in d depth-first, in document order, calling fn
// with each block's path — the sequence of child indices from the document
// root — and a pointer to the block itself, so fn may mutate it in place.
// The path slice is reused across calls; callers that retain it must copy.
func Walk(d Doc, fn func(path []int, b *Block)) {
	walkBlocks(d.Blocks, nil, fn)
}

func walkBlocks(blocks []Block, path []int, fn func(path []int, b *Block)) {
	for i := range blocks {
		p := append(path, i) //nolint:gocritic // reused on purpose; Walk documents that callers who keep a path copy it
		fn(p, &blocks[i])
		walkBlocks(blocks[i].Children, p, fn)
	}
}

// Clone is a DEEP copy of a document, and it exists because a transform in
// this pipeline may rewrite the model it was handed IN PLACE.
//
// Every stage here rebuilds a Doc from its Blocks, and several of them reuse
// the inline and mark slices they were given rather than allocating fresh ones
// — which is fine when the caller wants only the result, and silently wrong
// the moment a caller wants to compare the BEFORE with the AFTER.
//
// Measured 2026-08-23, and it presented as a CRDT bug rather than an aliasing
// one. `EditServer.mutate` was changed to hand `ydoc.Write` both models so a
// mark-only change could be formatted in place instead of reloading the whole
// fragment. `suggest.ClearInstructions` rewrites its input, so by the time the
// write ran the two models were ONE object graph: the diff was empty, the
// targeted path correctly wrote nothing, and an instruction's highlight stayed
// in the document and was projected back to the author's file as `{==…==}`.
// `Load` had papered over it for years by overwriting the whole fragment from
// the result, so nothing had ever needed the distinction before.
//
// A CALLER THAT KEEPS A MODEL ACROSS A TRANSFORM MUST CLONE IT. That is the
// rule; this is the one implementation of it.
func Clone(d Doc) Doc { return Doc{Blocks: cloneBlocks(d.Blocks)} }

func cloneBlocks(blocks []Block) []Block {
	if blocks == nil {
		return nil
	}
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		out[i] = Block{
			Kind:     b.Kind,
			Attrs:    cloneStrs(b.Attrs),
			Inlines:  cloneInlines(b.Inlines),
			Text:     b.Text,
			Children: cloneBlocks(b.Children),
		}
	}
	return out
}

func cloneInlines(inlines []Inline) []Inline {
	if inlines == nil {
		return nil
	}
	out := make([]Inline, len(inlines))
	for i, in := range inlines {
		out[i] = Inline{Text: in.Text, Marks: cloneMarks(in.Marks)}
	}
	return out
}

func cloneMarks(marks []Mark) []Mark {
	if marks == nil {
		return nil
	}
	out := make([]Mark, len(marks))
	for i, m := range marks {
		out[i] = Mark{Kind: m.Kind, Attrs: cloneStrs(m.Attrs)}
	}
	return out
}

func cloneStrs(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
