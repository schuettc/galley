package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/ydoc"
)

// DISPLAY MATH IS RAW TEXT, AND THE THREE PLACES THAT KNOW THAT HAVE TO AGREE.
//
// The front matter file next door states the rule; this is the second kind
// under it. docmodel.MathBlock carries its content in Block.Text, so writeBlock
// writes it as one unmarked YXmlText run and both readers have to put it back
// in Text rather than in Inlines. A kind added to the writer and one reader
// crosses the bridge and comes back with its content in the wrong field — a
// document the reviewer never touched, projected to disk with the formula gone.
//
// Read and ReadLive are two implementations of one walk, so both are asserted
// against the same model.
const mathText = "$$\nE = mc^2\n$$\n"

func mathModel() docmodel.Doc {
	return docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "1"}, Inlines: []docmodel.Inline{text("Equations")}},
		{Kind: docmodel.MathBlock, Text: mathText},
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{text("Prose that must survive.")}},
	}}
}

func TestMathBlockSurvivesTheBridge(t *testing.T) {
	want := mathModel()
	got := roundTrip(t, want)
	if !docmodel.Equal(got, want) {
		t.Fatalf("display math did not survive Load -> Read:\n got:  %#v\n want: %#v", got, want)
	}
	if got.Blocks[1].Text != mathText {
		t.Errorf("math text came back %q", got.Blocks[1].Text)
	}
}

func TestMathBlockReadsTheSameThroughReadLive(t *testing.T) {
	want := mathModel()
	doc := crdt.New()
	ydoc.Load(doc, own(doc), want)
	got, err := ydoc.ReadLive(doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if !docmodel.Equal(got, want) {
		t.Errorf("display math did not survive Load -> ReadLive:\n got:  %#v\n want: %#v", got, want)
	}
}

// THE WHOLE POINT, STATED IN BYTES — the live path in miniature: the file's
// bytes, through Parse, into the CRDT the browser edits, back out, and
// serialized over the author's file.
//
// THE FIXTURE CARRIES A BLOCK WITH NO BODY AND ONE INSIDE A CONTAINER, because
// a math block whose body is one line at the top level is the shape that cannot
// fail — `text*` accepts a childless node and a list item re-indents what it
// holds, and both are where a raw-text kind is actually lost.
func TestMathBlockCrossesTheBridgeByteIdentical(t *testing.T) {
	src := "# Equations\n\n$$\na_1 * b_2 = \\sum_{i=1}^{n} x_i\n$$\n\n$$\n$$\n\n" +
		"- a bullet\n  $$\n  \\gamma\n  $$\n\n> $$\n> \\alpha\n> $$\n\nProse.\n"
	model, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	back := roundTrip(t, model)
	if got := string(markdown.Serialize(back)); got != src {
		t.Errorf("the file changed by being opened\n got: %q\nwant: %q", got, src)
	}
}
