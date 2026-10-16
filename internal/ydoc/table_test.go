package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/ydoc"
)

func tableModel() docmodel.Doc {
	// A cell ALWAYS holds a block, and a blank one holds an empty paragraph —
	// the shape markdown.Parse produces, because TipTap's tableCell is
	// `block+` and a childless cell is a node y-prosemirror deletes rather
	// than renders. See TestEveryCellCrossesWithABlockInside.
	cell := func(kind docmodel.BlockKind, align, text string) docmodel.Block {
		b := docmodel.Block{Kind: kind}
		if align != "" {
			b.Attrs = map[string]string{docmodel.AlignAttr: align}
		}
		body := docmodel.Block{Kind: docmodel.Paragraph}
		if text != "" {
			body.Inlines = []docmodel.Inline{{Text: text}}
		}
		b.Children = []docmodel.Block{body}
		return b
	}
	return docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "before"}}},
		{Kind: docmodel.Table, Children: []docmodel.Block{
			{Kind: docmodel.TableRow, Children: []docmodel.Block{
				cell(docmodel.TableHeader, "left", "Phase"),
				cell(docmodel.TableHeader, "center", "State"),
			}},
			{Kind: docmodel.TableRow, Children: []docmodel.Block{
				cell(docmodel.TableCell, "left", "1e"),
				cell(docmodel.TableCell, "center", ""),
			}},
		}},
	}}
}

// The claim the browser side rests on: a table crosses the CRDT whole. Every
// row, every cell, the alignment attribute, and the paragraph inside each
// cell — the blank cell's empty one included.
func TestTableSurvivesTheBridge(t *testing.T) {
	want := tableModel()
	if got := roundTrip(t, want); !docmodel.Equal(got, want) {
		t.Errorf("table did not survive Load -> Read:\n got:  %#v\n want: %#v", got, want)
	}
}

// ReadLive is a second implementation of the same walk — a structural
// snapshot under one Transact, then the text reads afterwards — and a table
// is two levels of nesting deeper than anything either path carried before.
// Two implementations that disagree by one level is exactly the class of bug
// this package already has scars from, so both are asserted against the same
// model.
func TestTableReadsTheSameThroughReadLive(t *testing.T) {
	want := tableModel()
	doc := crdt.New()
	ydoc.Load(doc, own(doc), want)
	got, err := ydoc.ReadLive(doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if !docmodel.Equal(got, want) {
		t.Errorf("table did not survive Load -> ReadLive:\n got:  %#v\n want: %#v", got, want)
	}
}

// The element tags in the fragment ARE TipTap's node names — this is the
// silent-desync rule from CLAUDE.md, checked at the one place the two sides
// actually meet. A tag spelled wrong here is a write ygo accepts and a
// browser renders as nothing.
func TestTableElementTagsAreTipTapNodeNames(t *testing.T) {
	doc := crdt.New()
	ydoc.Load(doc, own(doc), tableModel())

	children := doc.GetXmlFragment(ydoc.FragmentName).Children()
	table, ok := children[1].(*crdt.YXmlElement)
	if !ok {
		t.Fatalf("second child is %T, want a YXmlElement", children[1])
	}
	if table.NodeName != "table" {
		t.Fatalf("table tag = %q, want %q", table.NodeName, "table")
	}
	rowNode, ok := table.Children()[0].(*crdt.YXmlElement)
	if !ok {
		t.Fatalf("table child is %T, want a YXmlElement", table.Children()[0])
	}
	if rowNode.NodeName != "tableRow" {
		t.Fatalf("row tag = %q, want %q", rowNode.NodeName, "tableRow")
	}
	headerNode, ok := rowNode.Children()[0].(*crdt.YXmlElement)
	if !ok {
		t.Fatalf("row child is %T, want a YXmlElement", rowNode.Children()[0])
	}
	if headerNode.NodeName != "tableHeader" {
		t.Fatalf("header cell tag = %q, want %q", headerNode.NodeName, "tableHeader")
	}
	if got := headerNode.GetAttributes()["align"]; got != "left" {
		t.Errorf("header align attribute = %q, want %q", got, "left")
	}
	// A cell's content is a PARAGRAPH element, not text hanging off the cell.
	// TipTap's tableCell content is "block+", so text written straight onto
	// the cell is a document y-prosemirror cannot build a node from.
	// TestEveryCellCrossesWithABlockInside is the general form of that claim,
	// over the cells this one does not reach — including the blank ones.
	para, ok := headerNode.Children()[0].(*crdt.YXmlElement)
	if !ok {
		t.Fatalf("cell child is %T, want a YXmlElement", headerNode.Children()[0])
	}
	if para.NodeName != "paragraph" {
		t.Errorf("cell child tag = %q, want %q", para.NodeName, "paragraph")
	}
}

// EVERY CELL CROSSES THE BRIDGE WITH A BLOCK INSIDE IT — including a blank
// one, and this is the assertion the comment above used to decline to make.
//
// TipTap's tableCell and tableHeader are `content: 'block+'`, and
// AlignedTableCell/AlignedTableHeader extend only the attributes, so the
// browser's rule is the unmodified one. y-prosemirror does not SKIP an
// element it cannot build a node from: createNodeFromYElement wraps
// schema.node in a try, and the catch DELETES the element from the Y doc.
// That deletion is broadcast, EditServer's doc.OnUpdate fires, and the
// projection writes the shortened row to the author's file — with no
// keystroke, just from opening the document. markdown's cellTexts pads a
// short row at the END, so a blank LAST cell was re-padded byte-identically
// while a blank cell in any earlier column slid every value after it one
// column left, on disk.
//
// So the model that reaches the fragment has to be one TipTap's schema
// accepts, and this walks the fragment markdown.Parse actually produces and
// says so. It is driven from Parse rather than from a hand-built model on
// purpose: the hand-built one is what agreed with the bug for a whole phase.
//
// The fixture's blank cells are in the FIRST, MIDDLE and LAST column, plus a
// wholly blank row. A fixture carrying only the last-column case passes
// against the bug.
func TestEveryCellCrossesWithABlockInside(t *testing.T) {
	src := []byte("| knob | note | unit |\n" +
		"| --- | --- | --- |\n" +
		"| timeout | | tail |\n" +
		"| | middle | s |\n" +
		"| retries | count | |\n" +
		"| | | |\n")
	model, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	doc := crdt.New()
	ydoc.Load(doc, own(doc), model)

	cells := 0
	var walk func(n any, path string)
	walk = func(n any, path string) {
		el, ok := n.(*crdt.YXmlElement)
		if !ok {
			return
		}
		where := path + "/" + el.NodeName
		if el.NodeName == string(docmodel.TableCell) || el.NodeName == string(docmodel.TableHeader) {
			cells++
			kids := el.Children()
			if len(kids) == 0 {
				t.Errorf("%s reached the fragment EMPTY — TipTap's content is block+, so y-prosemirror deletes it and the projection writes a row one cell short", where)
				return
			}
			if _, ok := kids[0].(*crdt.YXmlElement); !ok {
				t.Errorf("%s holds a %T where a block element was expected", where, kids[0])
			}
		}
		for _, c := range el.Children() {
			walk(c, where)
		}
	}
	for _, n := range doc.GetXmlFragment(ydoc.FragmentName).Children() {
		walk(n, "")
	}

	// A header row and four body rows, three columns each, and the count is
	// asserted because a walk that found no cells at all would report every
	// claim above as satisfied.
	if cells != 15 {
		t.Errorf("walked %d cells, want 15 — the fixture or the walk lost some", cells)
	}
}
