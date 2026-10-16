package ydoc_test

import (
	"testing"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

// RUNS MUST SURVIVE THE CRDT, and this is the only thing that says so.
//
// docmodel.Equal deliberately ignores RunAttr — a run is session identity,
// minted at random and never serialized, so comparing it would make the
// round-trip fixed point unstatable (see Equal). That exclusion means the
// bridge's own round-trip tests, which are all written in terms of Equal, would
// not notice the bridge dropping every run on the floor.
//
// It would matter immediately. suggest.MintRuns is idempotent only because
// marks coming back from ReadLive still carry the runs they went in with; a
// bridge that dropped them would re-mint the whole document on every mutate
// cycle, renumbering the anchor under every open card mid-session. And the run
// is what makes one span in the file one decision after it is loaded, so losing
// it here restores the split this whole line of work removed.
func TestRoundTrip_PreservesRuns(t *testing.T) {
	src := "# T\n\n{~~The `retryBudget` value controls~>The retry budget controls~~} retries.\n" +
		"\nkeep {--age--}{--age--} here.\n"
	want, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	before := runsOf(want)
	if len(before) < 4 {
		t.Fatalf("fixture carries %d runs (%v), want at least 4 — the parser is not stamping", len(before), before)
	}

	after := runsOf(roundTrip(t, want))
	if len(after) != len(before) {
		t.Fatalf("round trip changed the run count: %d -> %d\nbefore %v\nafter  %v",
			len(before), len(after), before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("run %d changed through the CRDT: %q -> %q", i, before[i], after[i])
		}
	}
}

// runsOf lists the run on every suggestion mark, in document order, so a
// dropped or rewritten token shows up as a diff rather than a silent regroup.
func runsOf(d docmodel.Doc) []string {
	var out []string
	var walk func([]docmodel.Block)
	walk = func(blocks []docmodel.Block) {
		for _, b := range blocks {
			for _, in := range b.Inlines {
				for _, m := range in.Marks {
					switch m.Kind {
					case docmodel.Ins, docmodel.Del, docmodel.Highlight:
						out = append(out, m.Attrs[docmodel.RunAttr])
					}
				}
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
	return out
}
