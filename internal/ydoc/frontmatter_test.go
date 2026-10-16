package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/ydoc"
)

// FRONT MATTER IS RAW TEXT, AND THE THREE PLACES THAT KNOW THAT HAVE TO AGREE.
//
// docmodel.FrontMatter carries its content in Block.Text, exactly as CodeBlock
// does, so writeBlock writes it as one unmarked YXmlText run and both readers
// have to put it back in Text rather than in Inlines. A kind added to the
// writer and one reader crosses the bridge and comes back with its content in
// the wrong field — which serializes to a file with the metadata gone, from a
// document the reviewer never touched.
//
// Read and ReadLive are two implementations of one walk (see table_test.go),
// so both are asserted against the same model.
const frontMatterText = "---\ntitle: The Spec\nstatus: draft\n---\n"

func frontMatterModel() docmodel.Doc {
	return docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.FrontMatter, Text: frontMatterText},
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "1"}, Inlines: []docmodel.Inline{text("The Spec")}},
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{text("Prose that must survive.")}},
	}}
}

func TestFrontMatterSurvivesTheBridge(t *testing.T) {
	want := frontMatterModel()
	got := roundTrip(t, want)
	if !docmodel.Equal(got, want) {
		t.Fatalf("front matter did not survive Load -> Read:\n got:  %#v\n want: %#v", got, want)
	}
	if got.Blocks[0].Text != frontMatterText {
		t.Errorf("front matter text came back %q", got.Blocks[0].Text)
	}
}

func TestFrontMatterReadsTheSameThroughReadLive(t *testing.T) {
	want := frontMatterModel()
	doc := crdt.New()
	ydoc.Load(doc, own(doc), want)
	got, err := ydoc.ReadLive(doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if !docmodel.Equal(got, want) {
		t.Errorf("front matter did not survive Load -> ReadLive:\n got:  %#v\n want: %#v", got, want)
	}
}

// THE WHOLE POINT, STATED IN BYTES. This is the live path in miniature: the
// file's bytes, through Parse, into the CRDT the browser edits, back out, and
// serialized over the author's file. Every one of those hops is where the four
// lines used to be lost.
func TestFrontMatterCrossesTheBridgeByteIdentical(t *testing.T) {
	src := "---\ntitle: The Spec\nlist:\n  - a\n  - b\n---\n\n# The Spec\n\nProse.\n"
	model, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	back := roundTrip(t, model)
	if got := string(markdown.Serialize(back)); got != src {
		t.Errorf("the file changed by being opened\n got: %q\nwant: %q", got, src)
	}
}
