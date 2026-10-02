package markdown_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// cell builds a body cell holding one paragraph of the given inlines. A cell
// ALWAYS holds a paragraph, and a blank one holds an empty paragraph: the
// spelling of a cell is "| |" either way, so the fixed point is untouched,
// and TipTap's tableCell is `block+` — a childless cell is a node the browser
// deletes rather than renders. See TestParse_TableBlankCellHoldsAParagraph.
func cell(kind docmodel.BlockKind, align string, inlines ...docmodel.Inline) docmodel.Block {
	b := docmodel.Block{Kind: kind}
	if align != "" {
		b.Attrs = map[string]string{docmodel.AlignAttr: align}
	}
	body := docmodel.Block{Kind: docmodel.Paragraph}
	if len(inlines) > 0 {
		body.Inlines = inlines
	}
	b.Children = []docmodel.Block{body}
	return b
}

func row(cells ...docmodel.Block) docmodel.Block {
	return docmodel.Block{Kind: docmodel.TableRow, Children: cells}
}

func TestParse_Table(t *testing.T) {
	src := []byte("| Phase | State |\n| :-- | --: |\n| **1e** | `wip` |\n")
	got, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Table,
		Children: []docmodel.Block{
			row(
				cell(docmodel.TableHeader, "left", docmodel.Inline{Text: "Phase"}),
				cell(docmodel.TableHeader, "right", docmodel.Inline{Text: "State"}),
			),
			row(
				cell(docmodel.TableCell, "left", docmodel.Inline{
					Text: "1e", Marks: []docmodel.Mark{{Kind: docmodel.Bold}},
				}),
				cell(docmodel.TableCell, "right", docmodel.Inline{
					Text: "wip", Marks: []docmodel.Mark{{Kind: docmodel.Code}},
				}),
			),
		},
	}}}
	if !docmodel.Equal(got, want) {
		t.Errorf("Parse mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// A BLANK CELL HOLDS AN EMPTY PARAGRAPH, AND THE ROUND TRIP IS UNMOVED BY IT.
//
// The two halves are one test on purpose. The shape is asserted because the
// browser's schema requires it — TipTap's tableCell is `block+`, and a cell
// that arrives empty is deleted from the Y doc by y-prosemirror, projected to
// disk one cell short, and re-padded at the END by cellTexts, so every value
// after a blank cell in a non-last column slides one column left in the
// author's file just from opening it (internal/ydoc's
// TestEveryCellCrossesWithABlockInside is the bridge-side assertion, and
// web/typing.mjs §0 reads the file back out of a real browser).
//
// The round trip is asserted in the same test because the shape used to be
// declined FOR the round trip: parse.go's comment said an empty paragraph was
// a block markdown cannot write, so the next Parse would not produce it. That
// is true of a paragraph anywhere else and false inside a cell — a cell is
// spelled "| |" whether it holds an empty paragraph or no child at all — and
// the two claims are checked together so neither can be restored without the
// other being read.
//
// Every column position, because a blank LAST cell round-trips even when the
// browser has dropped it: cellTexts pads at the end.
func TestParse_TableBlankCellHoldsAParagraph(t *testing.T) {
	src := "| knob | note | unit |\n" +
		"| --- | --- | --- |\n" +
		"| timeout | | tail |\n" +
		"| | middle | s |\n" +
		"| retries | count | |\n" +
		"| | | |\n"
	doc, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blanks := 0
	for i, r := range doc.Blocks[0].Children {
		for j, c := range r.Children {
			if len(c.Children) != 1 {
				t.Fatalf("cell [%d][%d] holds %d blocks, want exactly 1", i, j, len(c.Children))
			}
			body := c.Children[0]
			if body.Kind != docmodel.Paragraph {
				t.Errorf("cell [%d][%d] holds a %q, want a paragraph", i, j, body.Kind)
			}
			if len(body.Inlines) == 0 {
				blanks++
			}
		}
	}
	if blanks != 6 {
		t.Errorf("found %d blank cells, want 6 — the fixture lost a column position", blanks)
	}

	// Serialize(Parse(x)) == x here (the fixture is already canonical), and
	// Parse(Serialize(Parse(x))) == Parse(x) — the stated property, over the
	// document the shape change is about.
	out := markdown.Serialize(doc)
	if string(out) != src {
		t.Fatalf("Serialize moved the file:\n got:  %q\n want: %q", out, src)
	}
	again, _, err := markdown.Parse(out)
	if err != nil {
		t.Fatalf("Parse [round 2]: %v", err)
	}
	if !docmodel.Equal(again, doc) {
		t.Errorf("Parse(Serialize(Parse(x))) != Parse(x):\n got:  %#v\n want: %#v", again, doc)
	}
}

// GFM normalizes a ragged table on the way in — a short row is padded with
// empty cells and a long row's extras are dropped — and goldmark implements
// exactly that. Pinned here because the serializer relies on it: every row
// coming out of Parse already has the header's column count, so writing that
// count back is a no-op for any document that was ever read.
func TestParse_TableNormalizesRaggedRows(t *testing.T) {
	src := []byte("| a | b |\n| --- | --- |\n| 1 |\n| 1 | 2 | 3 |\n")
	got, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	table := got.Blocks[0]
	for i, r := range table.Children {
		if n := len(r.Children); n != 2 {
			t.Errorf("row %d has %d cells, want 2", i, n)
		}
	}
}

// A pipe is a cell separator before it is anything else, so content pipes are
// backslash-escaped — including inside a code span, whose content nothing
// else in markdown lets you escape. GFM says so explicitly and goldmark
// implements it (its escapedPipeCell transformer splits the span around the
// backslash and drops it). Serialization depends on this being true in BOTH
// spans, so both are asserted.
func TestParse_TableEscapedPipes(t *testing.T) {
	src := []byte("| a\\|b | `c\\|d` |\n| --- | --- |\n")
	got, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cells := got.Blocks[0].Children[0].Children
	if n := len(cells); n != 2 {
		t.Fatalf("want 2 cells, got %d", n)
	}
	if text := cells[0].Children[0].Inlines[0].Text; text != "a|b" {
		t.Errorf("plain cell = %q, want %q", text, "a|b")
	}
	if text := cells[1].Children[0].Inlines[0].Text; text != "c|d" {
		t.Errorf("code-span cell = %q, want %q", text, "c|d")
	}
}

// A cell whose whole content is an image is an Image block, the same
// exception paragraph() already makes — the document model has no inline
// image, only a block one, and a cell holds blocks.
func TestParse_TableCellHoldingOnlyAnImage(t *testing.T) {
	src := []byte("| ![alt](a.png) |\n| --- |\n")
	got, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := got.Blocks[0].Children[0].Children[0].Children
	if len(body) != 1 || body[0].Kind != docmodel.Image {
		t.Fatalf("cell body = %#v, want one Image block", body)
	}
	if src := body[0].Attrs["src"]; src != "a.png" {
		t.Errorf("image src = %q, want %q", src, "a.png")
	}
}

// The canonical spelling, asserted as BYTES. This is the whole fixed-point
// argument in one test: what comes out is a function of the cells' content
// and nothing else, so a second write cannot differ from the first.
func TestSerialize_TableCanonicalForm(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"spacing is normalized to one space per side",
			"|a|b|\n|-|-|\n|1|2|\n",
			"| a | b |\n| --- | --- |\n| 1 | 2 |\n",
		},
		{
			"a column-aligned table is re-spelled, once",
			"| a   | bb  |\n| --- | --- |\n| 1   | 2   |\n",
			"| a | bb |\n| --- | --- |\n| 1 | 2 |\n",
		},
		{
			"every delimiter is three characters wide",
			"| a | b | c | d |\n| :---- | :----: | ----: | ---- |\n",
			"| a | b | c | d |\n| :-- | :-: | --: | --- |\n",
		},
		{
			"an empty cell is one space, not two",
			"| a | |\n| --- | --- |\n",
			"| a | |\n| --- | --- |\n",
		},
		{
			"a ragged row is squared to the header's column count",
			"| a | b |\n| --- | --- |\n| 1 |\n| 1 | 2 | 3 |\n",
			"| a | b |\n| --- | --- |\n| 1 | |\n| 1 | 2 |\n",
		},
		{
			"a content pipe stays escaped",
			"| a\\|b |\n| --- |\n",
			"| a\\|b |\n| --- |\n",
		},
		{
			"a pipe inside a code span stays escaped too",
			"| `c\\|d` |\n| --- |\n",
			"| `c\\|d` |\n| --- |\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			if got := string(markdown.Serialize(doc)); got != tc.want {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// A table is a PARAGRAPH to goldmark's block scanner — the table parser is a
// paragraph transformer, which is why it can reach up and take the line above
// its delimiter row as the header. Two consequences, and getting either wrong
// silently eats a block:
//
//	a table may not FOLLOW a paragraph line directly, or the paragraph's last
//	line becomes the header row and the paragraph loses it;
//	a paragraph may not follow a TABLE directly, or it becomes another row.
func TestSerialize_TableIsSeparatedFromProse(t *testing.T) {
	src := "before\n\n| a |\n| --- |\n\nafter\n"
	doc, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := string(markdown.Serialize(doc)); got != src {
		t.Fatalf("Serialize = %q, want %q", got, src)
	}
	// And inside a list item, where the blank line is decided per pair rather
	// than written unconditionally.
	tight := "- before\n\n  | a |\n  | --- |\n"
	doc, _, err = markdown.Parse([]byte(tight))
	if err != nil {
		t.Fatalf("Parse(tight): %v", err)
	}
	out := markdown.Serialize(doc)
	again, _, err := markdown.Parse(out)
	if err != nil {
		t.Fatalf("Parse(%q) [round 2]: %v", out, err)
	}
	if !docmodel.Equal(doc, again) {
		t.Errorf("a table in a list item did not survive:\n got:  %#v\n want: %#v", again, doc)
	}
}

// The phase's headline property, stated as bytes rather than as trees: after
// one write, writing again changes nothing. Every input here is deliberately
// NOT in canonical form, so the first write really does move.
//
// Second, not first. Parse-then-Serialize legitimately re-spells — that is
// what canonicalization is — and the same is already true of ordered-list
// markers ("10." becomes "1."). What must not happen is the SAVE AFTER THAT
// moving, because that is the one a reviewer sees as a diff they did not
// make.
func TestSerialize_TableSettlesOnTheSecondWrite(t *testing.T) {
	srcs := []string{
		"|a|b|\n|-|-|\n|1|2|\n",
		"| a   | bb  |\n| ----- | ----- |\n| 1   | 2   |\n",
		"| a | b |\n| :----: | ------: |\n| 1 |\n| 1 | 2 | 3 |\n",
		"| ☕😀 | `x\\|y` |\n| --- | --- |\n| {--old--} | {++new++} |\n",
		"| a |\n| --- |\n| |\n| |\n",
		"prose\n| a | b |\n| --- | --- |\n",
		"> | a |\n> | --- |\n> | b |\n",
	}
	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			first := markdown.Serialize(doc)
			again, _, err := markdown.Parse(first)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", first, err)
			}
			second := markdown.Serialize(again)
			if string(second) != string(first) {
				t.Fatalf("table did not settle:\n out1: %q\n out2: %q", first, second)
			}
		})
	}
}

// A cell can only hold one line, and the serializer must produce one whatever
// it is handed — these shapes cannot come from Parse, only from a hand-built
// document or from the CRDT, and each of them writes a newline into a row if
// nothing stops it. A broken row is not a rendering glitch: the rest of the
// cell becomes another row, or a paragraph, and the document is a different
// document.
func TestSerialize_TableCellStaysOnOneLine(t *testing.T) {
	body := func(blocks ...docmodel.Block) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind: docmodel.Table,
			Children: []docmodel.Block{
				{Kind: docmodel.TableRow, Children: []docmodel.Block{
					{Kind: docmodel.TableHeader, Children: blocks},
				}},
			},
		}}}
	}
	tests := []struct {
		name string
		doc  docmodel.Doc
	}{
		{"a hard break", body(docmodel.Block{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{
			{Text: "a"},
			{Marks: []docmodel.Mark{{Kind: docmodel.HardBreak}}},
			{Text: "b"},
		}})},
		{"two paragraphs", body(
			docmodel.Block{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "a"}}},
			docmodel.Block{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "b"}}},
		)},
		{"a code fence", body(docmodel.Block{Kind: docmodel.CodeBlock, Text: "one\ntwo\n"})},
		{"raw text holding a newline", body(docmodel.Block{
			Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "a\nb"}},
		})},
		{"a nested list", body(docmodel.Block{Kind: docmodel.BulletList, Children: []docmodel.Block{
			{Kind: docmodel.ListItem, Children: []docmodel.Block{
				{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "a"}}},
			}},
		}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := markdown.Serialize(tc.doc)
			// The header row and the delimiter under it — this table has one
			// row, not two — so the whole file is two lines, each ended by a
			// newline. Any more means a cell broke its row.
			if n := strings.Count(string(out), "\n"); n != 2 {
				t.Fatalf("cell wrote %d newlines, want 2 (header, delimiter): %q", n, out)
			}
			again, _, err := markdown.Parse(out)
			if err != nil {
				t.Fatalf("Parse(%q): %v", out, err)
			}
			if k := again.Blocks[0].Kind; k != docmodel.Table {
				t.Fatalf("reparsed as %q, want a table: %q", k, out)
			}
			if string(markdown.Serialize(again)) != string(out) {
				t.Errorf("did not settle: %q", out)
			}
		})
	}
}

// A {>>note<<} THAT IS THE WHOLE OF A CELL IS A BLOCK COMMENT, AND IT IS
// WRITTEN BACK INTO THE CELL.
//
// note.go's grammar answers "range vs. block-level" by POSITION — a note is
// block-level exactly when it is the entire content of its paragraph — and a
// table cell is a third case that sentence did not consider. The cell's
// paragraph IS the note, so extractCriticBlocks promotes it to a docmodel.Note
// exactly as it does in prose, and renderCell has to be able to write one:
// there is no cell text left to hang a range anchor on, so the range reading
// collapses, and DECLINING the promotion inside a table is worse than either —
// criticPass empties the paragraph, the cell renders "| |", and the comment
// leaves the file with an anchor pointing at nothing.
//
// Measured against the shipped serializer, which had no Note case at all:
// "| x | {>>note here<<} | z |" came back "| x | note here | z |". The markers
// were gone, the comment read as prose, and the only surviving copy was in the
// sidecar — the zero-tooling promise (an agent reads pending state from the .md
// alone) broken by opening the document and saving it.
//
// EVERY COLUMN POSITION, and the header row too. Unlike the blank-cell bug this
// one is visible from a Go golden file at any position — cellTexts pads at the
// END and nothing here is short — but the positions are still all held, because
// a fix written into cellParts is a fix that could be conditioned on an index.
func TestSerialize_NoteInACellStaysInTheFile(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"first column", "| a | b | c |\n| --- | --- | --- |\n| {>>first<<} | y | z |\n"},
		{"middle column", "| a | b | c |\n| --- | --- | --- |\n| x | {>>note here<<} | z |\n"},
		{"last column", "| a | b | c |\n| --- | --- | --- |\n| x | y | {>>last<<} |\n"},
		{"header cell", "| {>>hdr<<} | b |\n| --- | --- |\n| x | y |\n"},
		{"the only cell", "| {>>alone<<} |\n| --- |\n"},
		{"two notes in one cell", "| a | b |\n| --- | --- |\n| x | {>>one<<} {>>two<<} |\n"},
		{"a pipe in the note", "| a | b |\n| --- | --- |\n| x | {>>a\\|b<<} |\n"},
		{"an asterisk in the note", "| a | b |\n| --- | --- |\n| x | {>>a \\*star\\* here<<} |\n"},
		// A table cell's block comment is its ID mark, in the cell.
		{"a comment's ID mark", "| a | b |\n| --- | --- |\n| x | {>>@comment cb-0123456789abcdef<<} |\n"},
		{"an ID mark in a header cell", "| {>>@comment cb-0123456789abcdef<<} | b |\n| --- | --- |\n| x | y |\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, side, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(side) != 0 {
				t.Errorf("a note that is the whole of its cell was lifted into %d inline comment(s) — "+
					"it is block-level, so it stays in the tree: %+v", len(side), side)
			}
			out := markdown.Serialize(doc)
			if string(out) != tc.src {
				t.Fatalf("the comment did not survive the round trip:\n got:  %q\n want: %q", out, tc.src)
			}
			again, _, err := markdown.Parse(out)
			if err != nil {
				t.Fatalf("Parse [round 2]: %v", err)
			}
			if !docmodel.Equal(again, doc) {
				t.Errorf("Parse(Serialize(Parse(x))) != Parse(x):\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// The other side of the position discriminator, held so the fix above cannot
// be widened into it. A note with prose BESIDE it in the cell leaves that prose
// behind, so the paragraph is not empty, so it is a RANGE comment at its offset
// — lifted into the sidecar and never serialized again, exactly as the same
// note would be at the end of a sentence in ordinary prose. The cell keeps its
// text and loses the marker, and that is the whole-package rule for range
// comments rather than anything table-shaped.
func TestSerialize_NoteBesideTextInACellIsARangeComment(t *testing.T) {
	src := "| a | b |\n| --- | --- |\n| x | text {>>beside<<} |\n"
	doc, side, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(side) != 1 {
		t.Fatalf("want 1 inline comment lifted out, got %d: %+v", len(side), side)
	}
	if side[0].Text != "beside" {
		t.Errorf("comment text = %q, want %q", side[0].Text, "beside")
	}
	if side[0].Offset != 4 {
		t.Errorf("comment offset = %d, want 4 (after \"text\")", side[0].Offset)
	}
	// The path reaches THROUGH the table: block 0, row 1, cell 1, paragraph 0.
	if got, want := side[0].BlockPath, []int{0, 1, 1, 0}; !slices.Equal(got, want) {
		t.Errorf("BlockPath = %v, want %v", got, want)
	}
	docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
		if b.Kind == docmodel.Note {
			t.Errorf("a note beside text became a Note block; it is a range comment")
		}
	})
	want := "| a | b |\n| --- | --- |\n| x | text |\n"
	if out := markdown.Serialize(doc); string(out) != want {
		t.Errorf("Serialize:\n got:  %q\n want: %q", out, want)
	}
}

// WHAT "@document" MEANS INSIDE A CELL: exactly what it means everywhere else,
// and the container does not get a vote.
//
// The grammar's second discriminator is an explicit MARKER precisely because
// POSITION cannot answer block-vs-document — and a cell is a position. Forcing
// a cell's note to the block anchor would silently rewrite the author's own
// marker out of their file, which is the loss this whole fix is about, one word
// smaller. So the marker is honoured and written back verbatim, and the note
// anchors to the document, not to the cell it happens to be typed in.
//
// The "@block" escape travels with it: a block note whose own text starts
// "@document" is spelled "{>>@block @document …<<}" in a cell too, or the next
// Parse reads the author's words as a marker.
func TestSerialize_DocumentNoteInACell(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		anchor string
		text   string
	}{
		{
			name:   "the marker is honoured in a cell",
			src:    "| a | b |\n| --- | --- |\n| x | {>>@document the units in this table are wrong<<} |\n",
			anchor: docmodel.AnchorDocument,
			text:   "the units in this table are wrong",
		},
		{
			name:   "the escape is written in a cell",
			src:    "| a | b |\n| --- | --- |\n| x | {>>@block @document is the first word here<<} |\n",
			anchor: docmodel.AnchorBlock,
			text:   "@document is the first word here",
		},
		{
			name:   "a bare marker with no text",
			src:    "| a | b |\n| --- | --- |\n| x | {>>@document<<} |\n",
			anchor: docmodel.AnchorDocument,
			text:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			notes := 0
			docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
				if b.Kind != docmodel.Note {
					return
				}
				notes++
				if got := markdown.NoteAnchor(*b); got != tc.anchor {
					t.Errorf("anchor = %q, want %q", got, tc.anchor)
				}
				if got := markdown.NoteText(*b); got != tc.text {
					t.Errorf("text = %q, want %q", got, tc.text)
				}
			})
			if notes != 1 {
				t.Fatalf("found %d notes in the table, want 1", notes)
			}
			if out := markdown.Serialize(doc); string(out) != tc.src {
				t.Errorf("the marker did not survive:\n got:  %q\n want: %q", out, tc.src)
			}
		})
	}
}
