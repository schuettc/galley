package suggest

import (
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
)

func delMarked(texts ...string) docmodel.Doc {
	var inlines []docmodel.Inline
	for _, t := range texts {
		inlines = append(inlines, docmodel.Inline{
			Text:  t,
			Marks: []docmodel.Mark{{Kind: docmodel.Del, Attrs: map[string]string{"author": "court"}}},
		})
	}
	return docmodel.Doc{Blocks: []docmodel.Block{{Kind: docmodel.Paragraph, Inlines: inlines}}}
}

func runsOf(d docmodel.Doc) []string {
	var out []string
	var walk func([]docmodel.Block)
	walk = func(blocks []docmodel.Block) {
		for _, b := range blocks {
			for _, in := range b.Inlines {
				for _, m := range in.Marks {
					if r := m.Attrs[docmodel.RunAttr]; r != "" {
						out = append(out, r)
					}
				}
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
	return out
}

// Two marks over identical text must still be told apart. This is the whole
// point of the run, and the reason a card whose own anchor was on screen could
// render against another mark's position.
func TestMintRunsDistinguishesIdenticalText(t *testing.T) {
	runs := runsOf(MintRuns(delMarked("age", "age")))
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d (%v)", len(runs), runs)
	}
	if runs[0] == runs[1] {
		t.Errorf("identical text produced identical runs %q — duplicate anchors will collide", runs[0])
	}
}

// Idempotence: the mutate cycle re-mints on every change, and renumbering
// there would pull the anchor out from under every open card mid-session.
func TestMintRunsPreservesExisting(t *testing.T) {
	first := MintRuns(delMarked("one", "two"))
	before := runsOf(first)
	after := runsOf(MintRuns(first))

	if len(after) != len(before) {
		t.Fatalf("run count changed: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("run %d changed on re-mint: %q -> %q", i, before[i], after[i])
		}
	}
}

// Formatting marks are not suggestions: nothing decides them, so they carry no
// identity.
func TestMintRunsIgnoresFormattingMarks(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "bold", Marks: []docmodel.Mark{{Kind: docmodel.Bold}}}},
	}}}
	if runs := runsOf(MintRuns(d)); len(runs) != 0 {
		t.Errorf("formatting marks gained runs: %v", runs)
	}
}

// Minting must not write through into the caller's document. A mutate cycle
// derives its transform from a ReadLive snapshot, and writing into a shared
// Attrs map would reach back into that snapshot.
func TestMintRunsDoesNotMutateTheInput(t *testing.T) {
	in := delMarked("age")
	_ = MintRuns(in)
	if got := in.Blocks[0].Inlines[0].Marks[0].Attrs[docmodel.RunAttr]; got != "" {
		t.Errorf("MintRuns wrote a run back into its input: %q", got)
	}
}

// Nested blocks are reached: a suggestion inside a list item or a blockquote is
// as addressable as one in a top-level paragraph.
func TestMintRunsReachesNestedBlocks(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Blockquote,
		Children: []docmodel.Block{{
			Kind:    docmodel.Paragraph,
			Inlines: []docmodel.Inline{{Text: "quoted", Marks: []docmodel.Mark{{Kind: docmodel.Ins}}}},
		}},
	}}}
	if runs := runsOf(MintRuns(d)); len(runs) != 1 {
		t.Errorf("nested suggestion did not get a run: %v", runs)
	}
}

// Two marks over identical text get two runs: a run is what tells them apart
// once an ordinal has renumbered.
func TestMintRunsKeepsIdenticalMarksApart(t *testing.T) {
	at := map[string]string{"author": "court", "at": "2026-08-07T00:00:00Z"}
	d := MintRuns(docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Paragraph,
		Inlines: []docmodel.Inline{
			{Text: "keep "},
			{Text: "age", Marks: []docmodel.Mark{{Kind: docmodel.Del, Attrs: at}}},
			{Text: " and im"},
			{Text: "age", Marks: []docmodel.Mark{{Kind: docmodel.Del, Attrs: at}}},
		},
	}}})

	pending := List(d)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d — adjacent marks were merged", len(pending))
	}
	if pending[0].Run == "" || pending[0].Run == pending[1].Run {
		t.Fatalf("runs not distinct: %q %q", pending[0].Run, pending[1].Run)
	}
}
