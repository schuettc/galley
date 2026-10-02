package markdown

import (
	"sort"
	"strings"
	"unicode"

	"github.com/schuettc/galley/internal/docmodel"
)

// critic.go is the CriticMarkup reader: the post-pass that runs over an
// already-parsed block's Inlines and turns tracked-changes syntax into
// suggestion marks.
//
//	{++text++}        -> Ins        {--text--}        -> Del
//	{==text==}        -> Highlight  {~~old~>new~~}    -> Del(old) Ins(new)
//	{>>note<<}        -> extracted out-of-band as an InlineComment
//
//	{==text==}{>>@comment cm-…<<}  -> Highlight carrying the comment's ID
//
// The last is a comment ID mark, not a comment: see note.go for the grammar
// and stampCommentID for the binding rule.
//
// The writer half lives in render_inline.go (wrapCritic, planSubstitution).
//
// It is a post-pass rather than a goldmark extension for one reason: the
// rules that make the syntax safe are stated over the PARSED inline
// sequence, not over source bytes. "Markers inside a code span are
// literal" is a statement about code spans, which only exist after
// goldmark has run; and a marker pair may legitimately straddle an
// emphasis boundary, which source-byte scanning would have to re-derive.
//
// The pass therefore flattens a block's whole inline sequence into one
// rune-per-cell slice (each cell remembering the marks and the code-span
// identity of the Inline it came from), scans THAT for markers, applies
// marks to the cells a span covers, and rebuilds Inlines from the
// survivors. Cells from a code span are opaque: they can sit inside a
// span, but their runes never spell one.

// InlineComment is a {>>note<<} lifted OUT of the document. It is
// deliberately not a docmodel mark: a comment mark would render back into
// the file on the next Serialize, and comments' words do not live in the
// markdown. Serialize writes no inline note back; the only comment syntax it
// emits is a Note block and a highlight's ID mark (note.go).
//
// BlockPath is the docmodel.Walk path of the block the note was anchored
// in (the sequence of child indices from the document root). Offset is a
// RUNE offset into that block's text once every marker has been removed —
// the position the note was attached to. A hard break contributes no runes
// to that count.
type InlineComment struct {
	BlockPath []int
	Offset    int
	Text      string
}

// markerLen is the length of every CriticMarkup delimiter. All of them are
// exactly three ASCII characters, which is what lets matchMarker compare
// byte-indexed strings against rune-indexed cells.
const markerLen = 3

// subSep separates the two halves of a substitution.
const subSep = "~>"

// criticSpan is one delimiter pair. kind is the mark a matched span
// applies; the substitution and comment forms carry no single mark and are
// flagged instead.
type criticSpan struct {
	open  string
	close string
	kind  docmodel.MarkKind
	sub   bool
	note  bool
}

var criticSpans = []criticSpan{
	{open: "{++", close: "++}", kind: docmodel.Ins},
	{open: "{--", close: "--}", kind: docmodel.Del},
	{open: "{==", close: "==}", kind: docmodel.Highlight},
	{open: "{~~", close: "~~}", sub: true},
	{open: "{>>", close: "<<}", note: true},
}

// cell is one planned input rune: the unit the scanner works in. Marks are
// carried per rune so a span that covers several Inlines (or half of one)
// needs no splitting logic of its own — rebuildInlines re-derives the
// Inline boundaries from the marks the cells ended up with.
type cell struct {
	r     rune
	marks []docmodel.Mark
	// group is the index of the source Inline for a code span's runes, so
	// two adjacent code spans that end up with identical marks are still
	// rebuilt as two spans; it is -1 for ordinary text, whose runes may
	// merge freely.
	group int
	// opaque marks a rune the scanner must not read as marker syntax: a
	// code span's content (literal by definition) or a hard break's
	// zero-text sentinel.
	opaque bool
	brk    bool // the hard-break sentinel itself
	dead   bool // consumed: marker syntax, or a comment's body
}

// extractCritic runs the CriticMarkup pass over every block in d that carries
// inlines, and returns the rewritten document plus the comments it lifted out.
//
// It returns a new Doc rather than editing in place because one block can
// become SEVERAL — see extractCriticBlocks, where that is the whole point.
func extractCritic(d docmodel.Doc) (docmodel.Doc, []InlineComment) {
	var out []InlineComment
	return docmodel.Doc{Blocks: extractCriticBlocks(d.Blocks, "", nil, &out)}, out
}

// extractCriticBlocks rewrites one level of the tree, and is where the rule
// "a {>>…<<} a human typed is never removed from the file" is ENFORCED rather
// than merely intended.
//
// The two Criticals this exists for were both the same shape: standaloneNote
// recognised exactly one arrangement — a paragraph whose whole content is a
// single note — and every near-miss fell through to the lifting path, whose
// outcome is that criticPass empties the paragraph and renderedBlocks then
// drops the empty paragraph entirely. The author's words left the file with no
// error and no trace.
//
// Recognising more shapes would not have fixed that; it would have moved the
// cliff. So the guarantee here is about the OUTCOME of not recognising
// something. If the extraction consumed the paragraph's ENTIRE content, then
// whatever was in it was comment syntax and nothing else — the block cannot be
// written back as prose, and dropping it takes the comments with it. Every
// comment lifted out of such a paragraph therefore comes back as a Note block,
// in order, however imprecise the anchor that gives it. There is no count
// limit and no shape test: the condition is "nothing survived", which is
// exactly the condition under which the old code deleted.
//
// This subsumes the single-note case standaloneNote used to special-case, so
// that function is gone: one note is just the commonest N.
//
// The complementary half is noteline.go, which runs in the converter and
// splits a paragraph at a LINE that is nothing but notes. That one is about
// giving a note the right anchor; this one is about never losing it. Neither
// replaces the other.
//
// A Heading is deliberately NOT covered by the PROMOTION. Turning a heading
// whose content is only a note into a note would delete the heading, which is
// the same class of silent loss pointing the other way.
//
// What that reasoning missed is where the note then went. Lifting it as a range
// left an anchor into an EMPTY STRING, and the heading serialized to "# " — so
// `# {>>write the title here<<}` came back `# ` on open, with the author's
// words gone from the .md and trailing whitespace written in their place.
// renderCellNote's comment names that exact outcome "the WORST of the three",
// and the "nothing survived" guarantee above is stated to make it impossible;
// it simply did not reach a block that is not a Paragraph.
//
// So the guarantee now covers every inline-bearing block, with the ACTION
// chosen by what the block can survive:
//
//   - a Paragraph is REPLACED by its notes, as before. Nothing is lost: a
//     paragraph whose whole content was comment syntax has no prose to keep.
//   - anything else — a Heading, and a Note whose own text spells a marker —
//     KEEPS ITS MARKERS. The block's inlines are left exactly as the author
//     typed them and the extraction is discarded, so the file round-trips
//     byte-identically and the words stay where they were written. That is the
//     ruling CLAUDE.md makes for a code fence: a {>>…<<} with no anchor to be
//     given is not a comment, it is text.
//
// Refusing the document was the third option and is wrong here: `unsupported`
// is for constructs the MODEL cannot represent — raw HTML, footnotes — and this
// one it represents exactly, byte for byte. Refusing a shape you can carry
// losslessly is a capability regression, not honesty.
//
// parent is the kind of the block whose children these are, and it is here for
// listItem alone: `paragraph block*` requires the FIRST child to be a
// paragraph, and a promotion at index 0 would put a Note there — the shape
// legalize exists to prevent, arriving after legalize has run. An empty
// Paragraph goes in front of the notes, which is the same refill legalize and
// suggest.removeNoteBlock use and renders to nothing on the way back out.
func extractCriticBlocks(blocks []docmodel.Block, parent docmodel.BlockKind, path []int, out *[]InlineComment) []docmodel.Block {
	res := make([]docmodel.Block, 0, len(blocks))
	for _, b := range blocks {
		// The path a child records is the parent's index in the OUTPUT, which
		// is where res has reached — the parent is appended below.
		here := append(append([]int(nil), path...), len(res))
		if len(b.Children) > 0 {
			b.Children = extractCriticBlocks(b.Children, b.Kind, here, out)
		}
		if len(b.Inlines) == 0 {
			res = append(res, b)
			continue
		}
		inlines, comments := criticPass(b.Inlines)
		if len(inlines) == 0 && len(comments) > 0 {
			if b.Kind != docmodel.Paragraph {
				// Keep the markers. b.Inlines is untouched, the comments are
				// discarded, and the block writes back exactly what was read.
				res = append(res, b)
				continue
			}
			if len(res) == 0 && parent == docmodel.ListItem {
				res = append(res, docmodel.Block{Kind: docmodel.Paragraph})
			}
			for _, c := range comments {
				res = append(res, noteBlock(c.Text))
			}
			continue
		}
		b.Inlines = inlines
		for _, c := range comments {
			c.BlockPath = here
			*out = append(*out, c)
		}
		res = append(res, b)
	}
	return res
}

// criticPass is the whole reader for one block's inline sequence.
func criticPass(inlines []docmodel.Inline) ([]docmodel.Inline, []InlineComment) {
	cells := flattenCells(inlines)
	notes := scanCritic(cells, 0, len(cells))
	trimLineEdges(cells)
	collapseNoteSeams(cells, notes)
	return rebuildInlines(cells), resolveNotes(cells, notes)
}

// collapseNoteSeams removes the separator that lifting a {>>note<<} out of
// the text leaves doubled. "A with {>>n<<} B" has whitespace on both sides
// of the note and only one of them was ever a separator between words, so
// the other has to go with the note: without this, extraction reads back as
// "A with  B".
//
// The rule is: a note takes the whitespace that FOLLOWS it, but only when
// whitespace also PRECEDES it. Everything about that is deliberate.
//
// Only when whitespace precedes it, because a lone space is not the
// extraction's doing — it is the author's. "a {>>n<<}b" collapsing to "ab"
// would fuse two words that were never joined, and "word {>>n<<}, next"
// leaving "word, next" would delete a character on a guess about who the
// space belonged to. A wrong space is cosmetic; a fused word is text loss.
//
// The FOLLOWING run rather than the preceding one, because the offset the
// note reports is measured forward: keeping the whitespace on the left of
// the seam leaves the anchor pointing at the first character of the next
// word ("in it."), where killing the left one would leave it pointing at
// the space in front of that word. Both are self-consistent — resolveNotes
// counts surviving cells, so no arithmetic has to be kept in step by hand —
// but only one of them anchors a comment to a word.
//
// It runs AFTER trimLineEdges so that "the nearest surviving cell" already
// excludes the whitespace markdown cannot carry back anyway; a note at a
// line edge therefore finds no neighbour here and is left entirely to the
// trim.
func collapseNoteSeams(cells []cell, notes []pendingNote) {
	for _, n := range notes {
		left := scanSurviving(cells, n.at-1, -1)
		if left < 0 || !isSeamSpace(cells[left]) {
			continue
		}
		for i := scanSurviving(cells, n.end, 1); i >= 0 && isSeamSpace(cells[i]); i = scanSurviving(cells, i+1, 1) {
			cells[i].dead = true
		}
	}
}

// isSeamSpace reports whether a cell is whitespace this pass may remove. A
// hard break is not whitespace but a line ending, and a code span's content
// is literal — "`x `{>>n<<} b" must not collapse to "`x `b", which changes
// what the span abuts.
func isSeamSpace(c cell) bool {
	return !c.dead && !c.brk && !c.opaque && unicode.IsSpace(c.r)
}

// scanSurviving returns the index of the first cell from i onward in the
// direction step that has not been consumed, or -1 if the walk runs off the
// end of the block.
func scanSurviving(cells []cell, i, step int) int {
	for ; i >= 0 && i < len(cells); i += step {
		if !cells[i].dead {
			return i
		}
	}
	return -1
}

// trimLineEdges drops what markdown cannot carry back once markers have
// been removed: whitespace at the start of a line (the block's first line,
// or the line after a hard break), and whitespace or a hard break at
// either edge of the block.
//
// For a document with no comments in it this is a strict no-op — goldmark
// has already stripped exactly this whitespace, and cannot produce a block
// that begins or ends with a hard break. It exists because lifting a
// {>>note<<} OUT of the text is the one thing that can put them back:
// "{>>n<<} text" would leave a leading space, and "text\<newline>{>>n<<}"
// a trailing break, both of which the next Parse silently drops — so
// Serialize would produce different bytes on the second round than on the
// first.
//
// Whitespace BEFORE an interior hard break is deliberately left alone:
// galley writes hard breaks as a trailing backslash, and "a  \<newline>"
// does preserve those two spaces, so trimming them would lose text that
// round-trips today. Code-span content is left alone for the same reason —
// a fence carries its own padding, and its content is literal.
func trimLineEdges(cells []cell) {
	trimEdge(cells, 0, len(cells), 1)
	trimEdge(cells, len(cells)-1, -1, -1)
	lineStart := true
	for i := range cells {
		c := &cells[i]
		switch {
		case c.dead:
		case c.brk:
			lineStart = true
		case lineStart && !c.opaque && unicode.IsSpace(c.r):
			c.dead = true
		default:
			lineStart = false
		}
	}
}

// trimEdge kills the leading run of unrepresentable cells — whitespace and
// hard breaks — walking from index from toward stop in steps of step.
func trimEdge(cells []cell, from, stop, step int) {
	for i := from; i != stop; i += step {
		c := &cells[i]
		if c.dead {
			continue
		}
		if !c.brk && (c.opaque || !unicode.IsSpace(c.r)) {
			return
		}
		c.dead = true
	}
}

func flattenCells(inlines []docmodel.Inline) []cell {
	var out []cell
	for i, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			out = append(out, cell{marks: in.Marks, group: i, opaque: true, brk: true})
			continue
		}
		code := in.Has(docmodel.Code)
		group := -1
		if code {
			group = i
		}
		for _, r := range in.Text {
			out = append(out, cell{r: r, marks: in.Marks, group: group, opaque: code})
		}
	}
	return out
}

// pendingNote is a comment found by the scanner, spanning the cells
// [at, end) — from its opening "{>>" to just past its closing "<<}". The
// rune offset can only be computed once the whole scan is done and every
// marker's cells are dead; end is what collapseNoteSeams needs to find the
// text on the far side of the note.
type pendingNote struct {
	at   int
	end  int
	text string
}

// scanCritic scans cells[lo:hi) left to right, applying marks and killing
// marker syntax in place. An opener with no matching closer stays literal
// text; the first matching closer wins, so a span never has to guess at
// nesting of its own kind. A matched span's interior is rescanned, which
// is how {=={++b++}==} gets both marks.
//
// Two memos keep the scan linear in the number of cells rather than
// quadratic on adversarial input like "{++{++{++…": a failed closer search
// from i proves no closer of that kind exists anywhere in (i, hi), so
// every later opener of the same kind in this range fails too; and a
// substitution whose closer is j but which has no "~>" before it proves
// the same for every substitution opener before j (they would all find the
// same first closer, over a subrange of the same separator-free text).
func scanCritic(cells []cell, lo, hi int) []pendingNote {
	var notes []pendingNote
	noCloser := make([]bool, len(criticSpans))
	noSep := lo
	// hl is the highlight span this level closed most recently, so a note
	// that opens on the very next cell can be read as its ID mark.
	var hl closedHighlight
	for i := lo; i < hi; i++ {
		k := openerAt(cells, i, hi)
		if k < 0 || noCloser[k] {
			continue
		}
		s := criticSpans[k]
		if s.sub && i < noSep {
			continue
		}
		j := findMarker(cells, i+markerLen, hi, s.close)
		if j < 0 {
			noCloser[k] = true
			continue
		}
		body, bodyEnd := i+markerLen, j
		switch {
		case s.note:
			text := cellText(cells, body, bodyEnd)
			if id, ok := commentID(text); ok && hl.run != "" && i == hl.end {
				// The highlight's ID mark: it names the comment, it is not
				// one, so nothing is lifted and the words stay in the text.
				stampCommentID(cells, hl, id)
			} else {
				notes = append(notes, pendingNote{at: i, end: j + markerLen, text: text})
			}
			kill(cells, i, j+markerLen)
		case s.sub:
			sep := findMarker(cells, body, bodyEnd, subSep)
			if sep < 0 {
				noSep = j
				continue
			}
			applyMark(cells, body, sep, docmodel.Del)
			applyMark(cells, sep+len(subSep), bodyEnd, docmodel.Ins)
			kill(cells, i, body)
			kill(cells, sep, sep+len(subSep))
			kill(cells, j, j+markerLen)
			notes = append(notes, scanCritic(cells, body, bodyEnd)...)
		default:
			run := applyMark(cells, body, bodyEnd, s.kind)
			kill(cells, i, body)
			kill(cells, j, j+markerLen)
			notes = append(notes, scanCritic(cells, body, bodyEnd)...)
			if s.kind == docmodel.Highlight {
				hl = closedHighlight{lo: body, hi: bodyEnd, end: j + markerLen, run: run}
			}
		}
		i = j + markerLen - 1
	}
	return notes
}

// closedHighlight is a highlight span scanCritic has just closed: its body is
// cells [lo, hi), its closing "==}" ends at end, and run is the run applyMark
// stamped on it, which is how its own mark is told apart from any other
// highlight on the same cells.
type closedHighlight struct {
	lo, hi, end int
	run         string
}

// stampCommentID binds a comment ID mark to the highlight it follows.
//
// THE MARK MUST SIT FLUSH AGAINST THE "==}", with nothing between them, and
// that is the whole binding rule. The serializer only ever writes it there (see
// wrapCritic), so anything else — a space, a word, a mark after some other
// span — is not one of galley's marks, and it reads as the ordinary note it
// looks like.
//
// The id goes onto the Highlight mark carrying THIS span's run on every cell
// the span covered, so a span that crossed emphasis or a code span carries it
// on every inline it became. Each mark is replaced rather than edited: the
// Attrs map may be shared with cells that are not this span's.
func stampCommentID(cells []cell, hl closedHighlight, id string) {
	for i := hl.lo; i < hl.hi; i++ {
		for k, m := range cells[i].marks {
			if m.Kind != docmodel.Highlight || m.Attrs[docmodel.RunAttr] != hl.run {
				continue
			}
			attrs := make(map[string]string, len(m.Attrs)+1)
			for key, v := range m.Attrs {
				attrs[key] = v
			}
			attrs[docmodel.CommentIDAttr] = id
			marks := cloneMarks(cells[i].marks)
			marks[k] = docmodel.Mark{Kind: m.Kind, Attrs: attrs}
			cells[i].marks = marks
			break
		}
	}
}

// openerAt returns the index into criticSpans of the delimiter pair whose
// opener starts at cells[i], or -1.
func openerAt(cells []cell, i, hi int) int {
	for k, s := range criticSpans {
		if matchMarker(cells, i, hi, s.open) {
			return k
		}
	}
	return -1
}

// matchMarker reports whether the marker s starts at cells[i]. Every
// marker is ASCII, so its byte length is its rune length and s[k] is a
// rune. Dead cells (already-consumed syntax) and opaque ones (code-span
// content, the hard-break sentinel) never match: that is what makes
// markers inside `code` literal.
func matchMarker(cells []cell, i, hi int, s string) bool {
	if i < 0 || i+len(s) > hi {
		return false
	}
	for k := 0; k < len(s); k++ {
		c := cells[i+k]
		if c.dead || c.opaque || c.r != rune(s[k]) {
			return false
		}
	}
	return true
}

// findMarker returns the index of the first match of s in cells[lo:hi), or -1.
func findMarker(cells []cell, lo, hi int, s string) int {
	for i := lo; i+len(s) <= hi; i++ {
		if matchMarker(cells, i, hi, s) {
			return i
		}
	}
	return -1
}

// applyMark adds kind to every surviving text cell in the range. The
// hard-break sentinel is skipped: it renders as a line ending, not as
// text, so a suggestion mark on it would be dropped by the writer and
// break the fixed point.
// ONE SPAN IN THE FILE IS ONE SPAN IN THE DOCUMENT, however many inlines it
// crosses, and the run is how that survives. This function is called exactly
// once per span the scanner closes, so it is the ONLY place that ever knows
// where the file's own span boundaries are — a substitution's two halves are
// two calls and correctly get two runs.
//
// Nothing downstream can recover it. suggest.List groups adjacent marks by
// author, instant and run, and an unattributed mark read from a file has
// neither author nor instant to tell it from its neighbour. So without a run
// stamped here, a substitution whose deleted half crosses a code span — three
// inlines, because a code span is its own inline — read as THREE independent
// decisions as soon as a session minted a token per mark. Opening a correct
// document split it, and the projection wrote the split back to disk.
//
// The run also stops rebuildInlines coalescing two ADJACENT spans into one
// inline. "{--age--}{--age--}" is two deletions the reviewer decides
// separately; identical marks used to merge them into a single "ageage" that
// one accept decided, which is the guarantee MintRuns' doc comment claims and
// could not deliver for anything read from a file.
//
// It returns the run it stamped, so the scanner can find this span's own mark
// again when a comment ID mark follows it (stampCommentID).
func applyMark(cells []cell, lo, hi int, kind docmodel.MarkKind) string {
	run := docmodel.NewRun()
	for i := lo; i < hi; i++ {
		if cells[i].dead || cells[i].brk {
			continue
		}
		// cloneMarks first: the slice is shared with every other cell from
		// the same source Inline.
		cells[i].marks = append(cloneMarks(cells[i].marks),
			docmodel.Mark{Kind: kind, Attrs: map[string]string{docmodel.RunAttr: run}})
	}
	return run
}

func kill(cells []cell, lo, hi int) {
	for i := lo; i < hi; i++ {
		cells[i].dead = true
	}
}

// cellText is the plain text of a range — used for a comment's body, which
// keeps its characters but loses any markup inside it (a comment is a
// note, not a document).
func cellText(cells []cell, lo, hi int) string {
	var b strings.Builder
	for i := lo; i < hi; i++ {
		if cells[i].dead || cells[i].brk {
			continue
		}
		b.WriteRune(cells[i].r)
	}
	return b.String()
}

// rebuildInlines turns the surviving cells back into Inlines, starting a
// new run wherever the mark set changes or one code span ends and another
// begins, then re-establishes Parse's maximal-run invariant with the same
// mergeAdjacent every other producer uses.
func rebuildInlines(cells []cell) []docmodel.Inline {
	var out []docmodel.Inline
	var b strings.Builder
	var cur cell
	open := false
	flush := func() {
		if open && b.Len() > 0 {
			out = append(out, docmodel.Inline{Text: b.String(), Marks: cur.marks})
		}
		b.Reset()
		open = false
	}
	for _, c := range cells {
		if c.dead {
			continue
		}
		if c.brk {
			flush()
			out = append(out, docmodel.Inline{Marks: c.marks})
			continue
		}
		if !open || c.group != cur.group || !sameMarks(c.marks, cur.marks) {
			flush()
			cur, open = c, true
		}
		b.WriteRune(c.r)
	}
	flush()
	return mergeAdjacent(out)
}

// resolveNotes converts each note's cell anchor into a rune offset into
// the block's final text: the number of surviving text cells before it.
// Notes are sorted by anchor because a note nested inside another span is
// found by the recursive rescan, after notes at the outer level.
func resolveNotes(cells []cell, notes []pendingNote) []InlineComment {
	if len(notes) == 0 {
		return nil
	}
	sort.SliceStable(notes, func(a, b int) bool { return notes[a].at < notes[b].at })
	out := make([]InlineComment, 0, len(notes))
	n, runes := 0, 0
	for i := range cells {
		for n < len(notes) && notes[n].at == i {
			out = append(out, InlineComment{Offset: runes, Text: notes[n].text})
			n++
		}
		if !cells[i].dead && !cells[i].brk {
			runes++
		}
	}
	for ; n < len(notes); n++ {
		out = append(out, InlineComment{Offset: runes, Text: notes[n].text})
	}
	return out
}
