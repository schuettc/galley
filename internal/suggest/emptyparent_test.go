package suggest_test

import (
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

// emptyparent_test.go is about the note that is the WHOLE of its parent.
//
// Removing it — delete-thread through suggest.Detach — used to take the
// parent's last child and leave the parent empty.
// A childless tableCell, tableHeader, listItem or blockquote is a node the
// browser's schema CANNOT BUILD, and y-prosemirror does not skip one: it
// deletes it out of the Y doc and broadcasts the deletion, EditServer projects,
// and the author's file is rewritten with no keystroke. In a table that is a
// COLUMN SHIFT, because markdown.cellTexts squares a row to the header's width
// by padding at the END.
//
// THE BYTES ARE THE CLAIM, NOT THE MODEL, so every case below serializes. But
// a plain Serialize of the Go model cannot see this bug: renderCell of a
// childless cell and of a cell holding an empty paragraph both yield "", and a
// list marker and a ">" are written whether or not the body renders anything.
// The corruption is what comes back AFTER the browser has refused the node. So
// the bytes are read through asTheBrowserWouldBuildIt, below.
//
// AND THE LAST COLUMN CANNOT FAIL. cellTexts pads at the end, so a note-only
// cell in the LAST column round-trips byte-identical through the bug — the same
// column position that let the blank-cell bug survive a whole phase. It is
// asserted anyway, as a fence, and TestTheLastColumnIsNotTheDiscriminatingCase
// records out loud that it is not what turns this red.

// asTheBrowserWouldBuildIt is what reaches disk after a document has been
// opened: the model with every node the shipped TipTap schema cannot build
// removed, exactly as y-prosemirror's createNodeFromYElement does it —
// schema.node is createChecked, it throws on content that does not match, and
// the catch deletes el._item rather than skipping the node.
//
// It is a MODEL of the browser, and deliberately a small one: the content rules
// it encodes are copied from the node definitions the bundle ships (@tiptap
// 2.27.2; web/entry.js registers them and overrides only attributes and
// plugins, never `content`), and nothing else about ProseMirror is simulated.
// The browser's own gates stay where they can see a real fragment and a real
// browser — internal/ydoc's TestEveryCellCrossesWithABlockInside and
// web/typing.mjs §0. What this earns is the one thing neither of those can give
// a Go package: the exact bytes each column position comes back as, cheaply,
// on every `just verify`.
//
// TestTheModelOfTheBrowserReproducesTheMeasuredColumnShift pins it against the
// one measurement in the record, so a simulator that quietly did nothing could
// not pass.
func asTheBrowserWouldBuildIt(d docmodel.Doc) docmodel.Doc {
	return docmodel.Doc{Blocks: buildableChildren(d.Blocks)}
}

func buildableChildren(blocks []docmodel.Block) []docmodel.Block {
	var out []docmodel.Block
	for _, b := range blocks {
		b.Children = buildableChildren(b.Children)
		if !schemaCanBuild(b) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// schemaCanBuild is the shipped content rule for every kind that has one worth
// stating. Anything not named here is `inline*`, `text*` or `*`, which no
// removal in this package can violate.
func schemaCanBuild(b docmodel.Block) bool {
	switch b.Kind {
	// `content: 'block+'`
	case docmodel.TableCell, docmodel.TableHeader, docmodel.Blockquote:
		return len(b.Children) > 0
	// `content: 'paragraph block*'` — the first child must be a paragraph,
	// which is stricter than block+ and is its own hazard; see the report.
	case docmodel.ListItem:
		return len(b.Children) > 0 && b.Children[0].Kind == docmodel.Paragraph
	// `content: 'tableRow+'` / `content: 'listItem+'`
	case docmodel.Table, docmodel.BulletList, docmodel.OrderedList:
		return len(b.Children) > 0
	}
	return true
}

// onOpen is the file as it comes back from a browser that has opened it and
// been touched by nobody.
func onOpen(d docmodel.Doc) string {
	return string(markdown.Serialize(asTheBrowserWouldBuildIt(d)))
}

// whyID is the block comment every fixture here carries, as its ID mark.
const whyID = "cb-00000000000000aa"

// deleteOnlyThread is the delete-thread verb: serve's editmode handler reaches
// suggest.Detach with the thread's key, and the result goes through mutate ->
// ydoc.Load into the live fragment. The note is found by its ID.
func deleteOnlyThread(t *testing.T, d docmodel.Doc) docmodel.Doc {
	t.Helper()
	threads := []review.Thread{{Key: whyID, Anchor: string(suggest.AnchorBlock)}}
	out, ok := suggest.Detach(d, threads, whyID)
	if !ok {
		t.Fatal("Detach found nothing to remove")
	}
	return out
}

// A NOTE-ONLY CELL IS DELETED WITHOUT MOVING THE COLUMN BESIDE IT.
//
// Every column position. The first two are the discriminating ones:
// a value after the emptied cell is what slides.
func TestDeletingANoteOnlyCellKeepsEveryOtherColumnInPlace(t *testing.T) {
	head := "| knob | note | unit |\n| --- | --- | --- |\n"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "first column",
			src:  head + "| {>>@comment " + whyID + "<<} | mid | tail |\n",
			want: head + "| | mid | tail |\n",
		},
		{
			name: "middle column",
			src:  head + "| lead | {>>@comment " + whyID + "<<} | tail |\n",
			want: head + "| lead | | tail |\n",
		},
		{
			name: "last column",
			src:  head + "| lead | mid | {>>@comment " + whyID + "<<} |\n",
			want: head + "| lead | mid | |\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := parseDoc(t, c.src)
			if got := onOpen(deleteOnlyThread(t, d)); got != c.want {
				t.Errorf("delete-thread wrote\n got:  %q\n want: %q", got, c.want)
			}
		})
	}
}

// A HEADER CELL IS THE SAME BUG WITH A WIDER BLAST RADIUS: the header row is
// what renderTable takes `cols` from, so a header cell the browser deletes
// narrows the WHOLE table, and cellTexts then drops the last cell of every body
// row as a long-row extra.
func TestDeletingANoteOnlyHeaderCellKeepsTheTablesWidth(t *testing.T) {
	src := "| {>>@comment " + whyID + "<<} | note | unit |\n| --- | --- | --- |\n| a | b | c |\n"
	want := "| | note | unit |\n| --- | --- | --- |\n| a | b | c |\n"
	d := parseDoc(t, src)
	if got := onOpen(deleteOnlyThread(t, d)); got != want {
		t.Errorf("delete-thread wrote\n got:  %q\n want: %q", got, want)
	}
}

// NOT TABLE-SPECIFIC. A note that is the whole of a list item empties a
// `paragraph block*` parent, and a note that is the whole of a blockquote
// empties a `block+` one. An emptied list item is deleted, and the items below
// it move up — in an ordered list they also renumber.
//
// The list-item case has a SECOND hazard this test does not cover and cannot
// fix here: `- {>>n<<}` parses to a listItem whose only child is a `note`, and
// `paragraph block*` requires the FIRST child to be a paragraph, so that item
// is already unbuildable BEFORE anything is deleted. See the report; it is
// markdown.Parse's to fix, in the shape parse.go:295 already used for cells.
func TestDeletingANoteThatIsTheWholeOfALisItemOrABlockquote(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "bullet item",
			src:  "- one\n- {>>@comment " + whyID + "<<}\n- three\n",
			want: "- one\n-\n- three\n",
		},
		{
			name: "ordered item",
			src:  "1. one\n2. {>>@comment " + whyID + "<<}\n3. three\n",
			want: "1. one\n2.\n3. three\n",
		},
		{
			name: "blockquote",
			src:  "Prose.\n\n> {>>@comment " + whyID + "<<}\n",
			want: "Prose.\n\n>\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := parseDoc(t, c.src)
			if got := onOpen(deleteOnlyThread(t, d)); got != c.want {
				t.Errorf("delete-thread wrote\n got:  %q\n want: %q", got, c.want)
			}
		})
	}
}

// THE EMPTIED FILE IS THE HONEST ANSWER, and is the one parent deliberately
// left unfilled. A document whose only block was the note is a document the
// author has emptied; an empty Paragraph at the top level is a block markdown
// cannot write, and renderedBlocks would drop it on the way out.
func TestDeletingTheOnlyBlockLeavesAnEmptyFile(t *testing.T) {
	d := parseDoc(t, "{>>@comment "+whyID+"<<}\n")
	out := deleteOnlyThread(t, d)
	// THE MODEL, not only the bytes: an empty Paragraph at the top level would
	// serialize away to the same "\n" (renderedBlocks drops a block that renders
	// nothing), so a byte assertion here could not fail and would certify
	// whatever the refill did. What must not happen is a phantom block reaching
	// the fragment.
	if len(out.Blocks) != 0 {
		t.Errorf("want no blocks left, got %d: %#v", len(out.Blocks), out.Blocks)
	}
	want := string(markdown.Serialize(docmodel.Doc{}))
	if got := onOpen(out); got != want {
		t.Errorf("want a file as empty as an empty one (%q), got %q", want, got)
	}
}

// THE MODEL-LEVEL FORM OF THE SAME CLAIM, so a future refill that happens to
// serialize right but leaves the wrong shape in the fragment is still caught:
// ydoc.Load writes this model straight into the live fragment, and the browser
// reads THAT, not the bytes.
func TestNoRemovalLeavesAParentTheBrowserCannotBuild(t *testing.T) {
	srcs := []string{
		"| knob | note | unit |\n| --- | --- | --- |\n| {>>@comment " + whyID + "<<} | mid | tail |\n",
		"| {>>@comment " + whyID + "<<} | note |\n| --- | --- |\n| a | b |\n",
		"- one\n- {>>@comment " + whyID + "<<}\n- three\n",
		"Prose.\n\n> {>>@comment " + whyID + "<<}\n",
	}
	for _, src := range srcs {
		out := deleteOnlyThread(t, parseDoc(t, src))
		docmodel.Walk(out, func(path []int, b *docmodel.Block) {
			if !schemaCanBuild(*b) {
				t.Errorf("%q left a %q at %v the browser deletes rather than renders", src, b.Kind, path)
			}
		})
	}
}

// THE LAST COLUMN IS NOT THE DISCRIMINATING CASE, and this test exists to say
// so where the next person writing a fixture will read it. cellTexts pads a
// short row at the END, so a note-only cell in the last column comes back
// byte-identical even when the cell has been deleted outright — which is
// exactly how the blank-cell bug survived a whole phase. A table fixture whose
// only note-only cell is last certifies this bug rather than catching it.
func TestTheLastColumnIsNotTheDiscriminatingCase(t *testing.T) {
	head := "| knob | note | unit |\n| --- | --- | --- |\n"
	broken := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Table,
		Children: []docmodel.Block{
			headerRow("knob", "note", "unit"),
			// The row a broken removal leaves: two cells where the header has
			// three, the third having been deleted outright.
			bodyRow("lead", "mid"),
		},
	}}}
	if got := string(markdown.Serialize(broken)); got != head+"| lead | mid | |\n" {
		t.Fatalf("the last-column row is not padding as this test claims: %q", got)
	}
	// And the first column, where the same deletion is loud.
	broken.Blocks[0].Children[1] = bodyRow("mid", "tail")
	if got := string(markdown.Serialize(broken)); got != head+"| mid | tail | |\n" {
		t.Errorf("want the measured column shift, got %q", got)
	}
}

// The simulator is pinned to the one measurement in the record: CLAUDE.md's
// "| timeout | | tail | came back | timeout | tail | | on open", produced by a
// cell that reached the fragment with no child inside it.
func TestTheModelOfTheBrowserReproducesTheMeasuredColumnShift(t *testing.T) {
	head := "| knob | note | unit |\n| --- | --- | --- |\n"
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Table,
		Children: []docmodel.Block{
			headerRow("knob", "note", "unit"),
			{Kind: docmodel.TableRow, Children: []docmodel.Block{
				textCell("timeout"),
				{Kind: docmodel.TableCell}, // no child: the shape parse.go:295 forbids
				textCell("tail"),
			}},
		},
	}}}
	if got := string(markdown.Serialize(d)); got != head+"| timeout | | tail |\n" {
		t.Fatalf("fixture does not spell the measured row: %q", got)
	}
	if got := onOpen(d); got != head+"| timeout | tail | |\n" {
		t.Errorf("the model of the browser did not reproduce the measured shift, got %q", got)
	}
}

func headerRow(texts ...string) docmodel.Block {
	r := docmodel.Block{Kind: docmodel.TableRow}
	for _, s := range texts {
		c := textCell(s)
		c.Kind = docmodel.TableHeader
		r.Children = append(r.Children, c)
	}
	return r
}

func bodyRow(texts ...string) docmodel.Block {
	r := docmodel.Block{Kind: docmodel.TableRow}
	for _, s := range texts {
		r.Children = append(r.Children, textCell(s))
	}
	return r
}

func textCell(text string) docmodel.Block {
	body := docmodel.Block{Kind: docmodel.Paragraph}
	if text != "" {
		body.Inlines = []docmodel.Inline{{Text: text}}
	}
	return docmodel.Block{Kind: docmodel.TableCell, Children: []docmodel.Block{body}}
}
