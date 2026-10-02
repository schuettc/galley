package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/ydoc"
)

func fmtTx(doc *crdt.Doc) review.Tx {
	return func(fn func(*crdt.Transaction)) { doc.Transact(fn, nil) }
}

// para builds the one shape every case here varies: three inlines, the middle
// one carrying whatever marks the case is about.
func fmtPara(text string, marks ...docmodel.Mark) docmodel.Doc {
	return docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "1"},
			Inlines: []docmodel.Inline{{Text: "Title"}}},
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{
			{Text: "Pressing "},
			{Text: text, Marks: marks},
			{Text: " sends the round."},
		}},
	}}
}

func fmtBold() docmodel.Mark { return docmodel.Mark{Kind: docmodel.Bold} }
func fmtLit() docmodel.Mark {
	return docmodel.Mark{Kind: docmodel.Highlight, Attrs: map[string]string{"author": "court"}}
}

func fmtMarksOf(t *testing.T, doc *crdt.Doc) []docmodel.Mark {
	t.Helper()
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range got.Blocks[1].Inlines {
		if in.Text == "Revise" {
			return in.Marks
		}
	}
	return nil
}

func fmtKinds(marks []docmodel.Mark) []docmodel.MarkKind {
	out := make([]docmodel.MarkKind, 0, len(marks))
	for _, m := range marks {
		out = append(out, m.Kind)
	}
	return out
}

// TestAddingAMarkIsTargeted is the gesture a reviewer makes most: filing an
// instruction puts a highlight over a range and moves no prose.
func TestAddingAMarkIsTargeted(t *testing.T) {
	doc := crdt.New()
	before, after := fmtPara("Revise", fmtBold()), fmtPara("Revise", fmtBold(), fmtLit())
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("adding a highlight fell back to a full reload — every reviewing gesture would orphan undo")
	}
	if got := fmtKinds(fmtMarksOf(t, doc)); len(got) != 2 {
		t.Errorf("marks after adding a highlight = %v, want bold + highlight", got)
	}
}

// TestClearingAMarkIsTargetedANDActuallyClearsIt is the half that shipped
// broken and was caught by the suite.
//
// `Format` applies the DIFFERENCE against what is in effect, so a kind the
// attribute set does not MENTION is left switched on. Naming only the kinds
// still present meant the mark being REMOVED was never named: sending a round
// left every instruction's highlight in the document, and the projection wrote
// `{==…==}` back to the author's file.
func TestClearingAMarkIsTargetedAndClearsIt(t *testing.T) {
	doc := crdt.New()
	before, after := fmtPara("Revise", fmtBold(), fmtLit()), fmtPara("Revise", fmtBold())
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("clearing a highlight fell back to a full reload")
	}
	got := fmtKinds(fmtMarksOf(t, doc))
	if len(got) != 1 || got[0] != docmodel.Bold {
		t.Errorf("marks after clearing the highlight = %v, want bold alone — the removed kind was never named", got)
	}
}

// TestATextChangeIsTargeted is phase 3's slice, and it inverts a check this
// file used to carry.
//
// `TestATextChangeFallsBackToLoad` asserted the opposite — that any text
// difference went through `Load` — and it was right about the build it was
// written for. Load deletes the fragment's children and writes them again,
// which throws the caret to the end of the document, orphans the browser's undo
// stack, and makes a keystroke landing between the read and the Apply
// clobberable anywhere in the file. Editing the words in place is what removes
// all three, and the old assertion is replaced rather than deleted so the
// inversion is on the record.
func TestATextChangeIsTargeted(t *testing.T) {
	doc := crdt.New()
	before, after := fmtPara("Revise", fmtBold()), fmtPara("Approve", fmtBold())
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("a text change fell back to a full reload — the document is rebuilt and every undo entry in it is orphaned")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[1].Inlines[1].Text != "Approve" {
		t.Errorf("the new text did not land: %+v", got.Blocks[1].Inlines)
	}
	if got.Blocks[1].Inlines[0].Text != "Pressing " ||
		got.Blocks[1].Inlines[2].Text != " sends the round." {
		t.Errorf("the text either side of the edit moved: %+v", got.Blocks[1].Inlines)
	}
	if !got.Blocks[1].Inlines[1].Has(docmodel.Bold) {
		t.Errorf("the edited span lost its mark: %+v", got.Blocks[1].Inlines[1])
	}
}

// TestOnlyWhatMovedIsRewritten is why the edit is trimmed to a common prefix
// and suffix rather than replacing the inline.
//
// The items either side of the change are what the browser's undo entries and
// anyone's caret are anchored to. Replacing the whole inline would work and
// throw all of that away for nothing, and the only way to see the difference
// from outside is to watch how much of the run the write actually touched — so
// this asserts on the SURVIVING text around a one-word change.
func TestOnlyWhatMovedIsRewritten(t *testing.T) {
	doc := crdt.New()
	build := func(word string) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind: docmodel.Paragraph,
			Inlines: []docmodel.Inline{
				{Text: "the " + word + " of the thing"},
			},
		}}}
	}
	before, after := build("shape"), build("size")
	ydoc.Load(doc, fmtTx(doc), before)
	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("fell back to a reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Inlines[0].Text != "the size of the thing" {
		t.Errorf("the trimmed edit produced the wrong text: %q", got.Blocks[0].Inlines[0].Text)
	}
}

// TestAMultiByteEditIsNotCutInHalf. The offsets handed to the CRDT are UTF-16
// units and the prefix/suffix trim walks BYTES, so the two have to meet on a
// rune boundary — this package has already paid once for a rune-indexed edit
// slicing a surrogate pair and leaving replacement characters.
func TestAMultiByteEditIsNotCutInHalf(t *testing.T) {
	doc := crdt.New()
	build := func(word string) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind:    docmodel.Paragraph,
			Inlines: []docmodel.Inline{{Text: "go 🚀 " + word + " 🚀 now"}},
		}}}
	}
	before, after := build("here"), build("there")
	ydoc.Load(doc, fmtTx(doc), before)
	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("fell back to a reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := "go 🚀 there 🚀 now"
	if got.Blocks[0].Inlines[0].Text != want {
		t.Errorf("want %q, got %q — an offset landed off a rune boundary", want, got.Blocks[0].Inlines[0].Text)
	}
}

// TestAnInlineGoingEmptyFallsBackToLoad. `writeInlines` SKIPS an empty inline —
// Insert ignores empty text — so there is no span in the document to edit, and
// every later offset in the run is computed as though it were not there.
func TestAnInlineGoingEmptyFallsBackToLoad(t *testing.T) {
	doc := crdt.New()
	before := fmtPara("Revise", fmtBold())
	after := fmtPara("", fmtBold())
	ydoc.Load(doc, fmtTx(doc), before)
	if ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Error("an inline emptied to nothing took the targeted path")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range got.Blocks[1].Inlines {
		if in.Text == "Revise" {
			t.Errorf("the fallback did not write the emptied inline: %+v", got.Blocks[1].Inlines)
		}
	}
}

// TestANewTopLevelBlockIsTargeted inverts another check this file carried.
//
// `TestAStructuralChangeFallsBackToLoad` asserted that a block appearing sent
// the write through `Load`, and it was right about the build it was written
// for. A top-level block appearing or disappearing is now one contiguous run
// replaced, which leaves every block outside that run — and every undo entry
// and caret anchored in them — untouched.
func TestANewTopLevelBlockIsTargeted(t *testing.T) {
	doc := crdt.New()
	before := fmtPara("Revise", fmtBold())
	after := fmtPara("Revise", fmtBold())
	after.Blocks = append(after.Blocks, docmodel.Block{
		Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "New."}},
	})
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("a block appended at the top level fell back to a full reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 3 || got.Blocks[2].Inlines[0].Text != "New." {
		t.Errorf("the block did not land: %+v", got.Blocks)
	}
	if got.Blocks[0].Inlines[0].Text != "Title" {
		t.Errorf("the blocks before it moved: %+v", got.Blocks)
	}
}

// TestADeletedTopLevelBlockIsTargeted — the same run, the other direction, and
// the one a reviewer makes most.
func TestADeletedTopLevelBlockIsTargeted(t *testing.T) {
	doc := crdt.New()
	before := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "One."}}},
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "Two."}}},
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "Three."}}},
	}}
	after := docmodel.Doc{Blocks: []docmodel.Block{before.Blocks[0], before.Blocks[2]}}
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("deleting a paragraph fell back to a full reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 2 ||
		got.Blocks[0].Inlines[0].Text != "One." ||
		got.Blocks[1].Inlines[0].Text != "Three." {
		t.Errorf("the wrong blocks survived: %+v", got.Blocks)
	}
}

// TestANestedStructuralChangeStillFallsBackToLoad is the limit, and it is the
// LIBRARY's rather than a decision: `YXmlFragment` has `Delete` and
// `YXmlElement` does not, so there is no way to remove a child from a list item
// or a blockquote through ygo's XML surface at all. The fallback keeps it
// correct, and the day the library grows one this is the test that changes.
func TestANestedStructuralChangeStillFallsBackToLoad(t *testing.T) {
	item := func(texts ...string) docmodel.Block {
		var kids []docmodel.Block
		for _, t := range texts {
			kids = append(kids, docmodel.Block{
				Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: t}},
			})
		}
		return docmodel.Block{Kind: docmodel.ListItem, Children: kids}
	}
	list := func(it docmodel.Block) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{
			{Kind: docmodel.BulletList, Children: []docmodel.Block{it}},
		}}
	}
	doc := crdt.New()
	before, after := list(item("one")), list(item("one", "two"))
	ydoc.Load(doc, fmtTx(doc), before)

	if ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Error("a block added INSIDE a list item took the targeted path — ygo cannot delete an element's child")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks[0].Children[0].Children) != 2 {
		t.Errorf("the fallback did not write the nested block: %+v", got.Blocks[0].Children[0].Children)
	}
}

// TestAHardBreakMovingIsStructural. A hard break ENDS the current run, so a
// break that came or went changes which run every later character lives in —
// and an offset computed from the model would land in the wrong one.
func TestAHardBreakMovingIsStructural(t *testing.T) {
	doc := crdt.New()
	before := fmtPara("Revise", fmtBold())
	after := fmtPara("Revise", fmtBold())
	after.Blocks[1].Inlines = append([]docmodel.Inline{
		{Text: "", Marks: []docmodel.Mark{{Kind: docmodel.HardBreak}}},
	}, after.Blocks[1].Inlines...)
	before.Blocks[1].Inlines = append([]docmodel.Inline{
		{Text: ""},
	}, before.Blocks[1].Inlines...)
	ydoc.Load(doc, fmtTx(doc), before)

	if ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Error("a hard break appearing took the targeted path")
	}
}

// TestNothingToDoWritesNothing. Two identical models are not a document
// mutation: writing anyway broadcasts to every peer and disturbs an undo stack
// for no reason.
func TestNothingToDoWritesNothing(t *testing.T) {
	doc := crdt.New()
	before := fmtPara("Revise", fmtBold())
	ydoc.Load(doc, fmtTx(doc), before)
	if !ydoc.Write(doc, fmtTx(doc), before, fmtPara("Revise", fmtBold())) {
		t.Error("an identical model fell back to a full reload")
	}
	if got := fmtKinds(fmtMarksOf(t, doc)); len(got) != 1 {
		t.Errorf("marks = %v, want bold", got)
	}
}

// TestOffsetsAreUTF16 is the indexing this package has already paid for once:
// YText indexes in UTF-16 code units, and a rune-indexed edit cuts a surrogate
// pair in half. An emoji before the marked word is two code units and one rune,
// so a rune-counting implementation formats one unit short and marks the wrong
// characters.
func TestOffsetsAreUTF16(t *testing.T) {
	doc := crdt.New()
	build := func(marks ...docmodel.Mark) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{
				{Text: "go 🚀 now "},
				{Text: "Revise", Marks: marks},
				{Text: " please."},
			},
		}}}
	}
	before, after := build(), build(fmtLit())
	ydoc.Load(doc, fmtTx(doc), before)
	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("fell back to a reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range got.Blocks[0].Inlines {
		marked := in.Has(docmodel.Highlight)
		if marked && in.Text != "Revise" {
			t.Errorf("the highlight landed on %q, not on the marked word — offsets are not in UTF-16 code units", in.Text)
		}
		if !marked && in.Text == "Revise" {
			t.Errorf("the marked word carries no highlight: %+v", got.Blocks[0].Inlines)
		}
	}
}

// TestAMarkInsideAListItemIsTargeted — the walk descends, so an instruction on
// a list item is as surgical as one on a paragraph.
func TestAMarkInsideAListItemIsTargeted(t *testing.T) {
	build := func(marks ...docmodel.Mark) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind: docmodel.BulletList,
			Children: []docmodel.Block{{
				Kind: docmodel.ListItem,
				Children: []docmodel.Block{{
					Kind:    docmodel.Paragraph,
					Inlines: []docmodel.Inline{{Text: "one"}, {Text: "two", Marks: marks}},
				}},
			}},
		}}}
	}
	doc := crdt.New()
	before, after := build(), build(fmtLit())
	ydoc.Load(doc, fmtTx(doc), before)
	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("a mark inside a list item fell back to a full reload")
	}
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	item := got.Blocks[0].Children[0].Children[0].Inlines
	for _, in := range item {
		if in.Text == "two" && !in.Has(docmodel.Highlight) {
			t.Errorf("the nested mark was not applied: %+v", item)
		}
	}
}

// TestAnUnknownMarkKindIsCleared is the case the union of before-and-after
// kinds exists for, and it is the ONLY case that distinguishes it.
//
// `runKinds` already seeds every kind in `markOrder`, so a KNOWN mark being
// removed is named whichever side you build the set from — which is why the
// obvious test for this (clear a highlight) passes with the union deleted, and
// proves nothing. An UNRECOGNISED kind is different: `readMarkAttrs` reads it
// faithfully and `runKinds` exists to preserve it, so nothing seeds it. Build
// the attribute set from the after-inlines alone and a removal nobody named is
// a removal that silently does not happen.
func TestAnUnknownMarkKindIsCleared(t *testing.T) {
	ext := docmodel.Mark{Kind: docmodel.MarkKind("underline")}
	doc := crdt.New()
	before, after := fmtPara("Revise", fmtBold(), ext), fmtPara("Revise", fmtBold())
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("clearing an extension mark fell back to a full reload")
	}
	for _, m := range fmtMarksOf(t, doc) {
		if m.Kind == "underline" {
			t.Errorf("an unrecognised mark survived its own removal: %v — "+
				"the attribute set never named it, so Format left it switched on",
				fmtKinds(fmtMarksOf(t, doc)))
		}
	}
}

// TestAppendingToASentenceDoesNotPanic pins the boundary that did.
//
// When one string is the other plus a suffix, the common prefix lands exactly
// on `len(a)` and the rune-boundary backoff read `a[len(a)]` — index 34 of a
// 34-byte string, from an ordinary edit that appended to a sentence. A position
// at the end of a string is already on a rune boundary; there is nothing to
// back off from.
func TestAppendingToASentenceDoesNotPanic(t *testing.T) {
	for _, pair := range [][2]string{
		{"The retry budget", "The retry budget is explicit"},
		{"The retry budget is explicit", "The retry budget"},
		{"", "something new"},
		{"go 🚀", "go 🚀 now"},
	} {
		doc := crdt.New()
		build := func(text string) docmodel.Doc {
			return docmodel.Doc{Blocks: []docmodel.Block{{
				Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: text}},
			}}}
		}
		before, after := build(pair[0]), build(pair[1])
		ydoc.Load(doc, fmtTx(doc), before)
		ydoc.Write(doc, fmtTx(doc), before, after)
		got, err := ydoc.Read(doc)
		if err != nil {
			t.Fatalf("%q -> %q: %v", pair[0], pair[1], err)
		}
		var said string
		if len(got.Blocks) > 0 && len(got.Blocks[0].Inlines) > 0 {
			said = got.Blocks[0].Inlines[0].Text
		}
		if said != pair[1] {
			t.Errorf("%q -> %q produced %q", pair[0], pair[1], said)
		}
	}
}

// TestAnIDOnlyChangeIsATargetedMarkWrite: stamping or changing a highlight's
// comment ID changes a mark's attributes and nothing else, so it must take
// the targeted path rather than reloading the fragment — and the new id must
// actually land.
func TestAnIDOnlyChangeIsATargetedMarkWrite(t *testing.T) {
	doc := crdt.New()
	withID := func(id string) docmodel.Mark {
		return docmodel.Mark{Kind: docmodel.Highlight, Attrs: map[string]string{
			"author": "court", docmodel.CommentIDAttr: id,
		}}
	}
	before, after := fmtPara("Revise", withID("cm-old")), fmtPara("Revise", withID("cm-new"))
	ydoc.Load(doc, fmtTx(doc), before)

	if !ydoc.Write(doc, fmtTx(doc), before, after) {
		t.Fatal("an id-only change fell back to a full reload")
	}
	marks := fmtMarksOf(t, doc)
	if len(marks) != 1 || marks[0].Attrs[docmodel.CommentIDAttr] != "cm-new" {
		t.Errorf("marks after the id change = %#v, want one highlight with id cm-new", marks)
	}
}
