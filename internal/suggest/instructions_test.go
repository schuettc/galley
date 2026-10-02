package suggest

import (
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
)

func TestClearInstructionsLeavesCleanProse(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "keep", Marks: []docmodel.Mark{{Kind: docmodel.Bold}, {Kind: docmodel.Highlight}}}}},
		{Kind: docmodel.Note, Inlines: []docmodel.Inline{{Text: "rewrite this"}}},
	}}
	got := ClearInstructions(d)
	if len(got.Blocks) != 1 || got.Blocks[0].Inlines[0].Text != "keep" ||
		len(got.Blocks[0].Inlines[0].Marks) != 1 || got.Blocks[0].Inlines[0].Marks[0].Kind != docmodel.Bold {
		t.Fatalf("cleared document = %#v", got)
	}
}

// ClearInstructions returns a clean copy. The document it is handed is the
// caller's, and a caller that compares the two (revert, pending) loses its
// highlights if the clear strips them in place.
func TestClearInstructionsLeavesItsInputAlone(t *testing.T) {
	in := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Paragraph,
		Inlines: []docmodel.Inline{{
			Text:  "the quick fox",
			Marks: []docmodel.Mark{{Kind: docmodel.Bold}, {Kind: docmodel.Highlight, Attrs: map[string]string{docmodel.CommentIDAttr: "cm-0123456789abcdef"}}},
		}},
	}}}
	out := ClearInstructions(in)
	if got := in.Blocks[0].Inlines[0].Marks; len(got) != 2 || got[1].Kind != docmodel.Highlight {
		t.Errorf("the input's marks after the clear = %+v, want bold and the highlight untouched", got)
	}
	if got := out.Blocks[0].Inlines[0].Marks; len(got) != 1 || got[0].Kind != docmodel.Bold {
		t.Errorf("the cleared copy's marks = %+v, want bold only", got)
	}
}

// A note that is the whole content of a cell, a header cell, a list item or a
// quote leaves that parent empty, which the browser's schema cannot build: the
// cell vanishes (shifting the columns) or the quote does. Sending a round
// refills it the way deleting one note does (removeBlockAt).
func TestClearInstructionsRefillsAParentItEmpties(t *testing.T) {
	note := docmodel.Block{Kind: docmodel.Note, Attrs: map[string]string{"anchor": "block"}, Inlines: []docmodel.Inline{{Text: "words"}}}
	for _, kind := range []docmodel.BlockKind{docmodel.TableCell, docmodel.TableHeader, docmodel.ListItem, docmodel.Blockquote} {
		out := ClearInstructions(docmodel.Doc{Blocks: []docmodel.Block{{Kind: kind, Children: []docmodel.Block{note}}}})
		got := out.Blocks[0].Children
		if len(got) != 1 || got[0].Kind != docmodel.Paragraph {
			t.Errorf("%s holding only a note, cleared: children %+v, want one empty paragraph", kind, got)
		}
	}
}
