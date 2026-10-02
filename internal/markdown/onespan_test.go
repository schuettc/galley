package markdown

import (
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
)

// ONE SPAN IN THE FILE IS ONE SPAN IN THE DOCUMENT, however many inlines it
// crosses.
//
// This is CLAUDE.md's rule in its ORIGINAL direction — file -> cards — and the
// parser is where it has to be enforced, because the parser is the only thing
// that ever sees the file's own span boundaries. `{~~…~~}` is ONE CriticMarkup
// span; that its deleted half happens to cross a code span is a fact about
// formatting, not about how many decisions the reviewer has.
//
// Downstream cannot recover this. suggest.List groups adjacent marks by author,
// instant and run, and a document read from a file has neither author nor
// instant on an unattributed mark — so if the parser does not say which inlines
// came from one span, nothing later can. Opening a correct document then splits
// it, with no agent, no reviewer action and no edit involved.

// runsFor returns the run on every inline carrying a mark of the given kind, in
// document order.
func runsFor(d docmodel.Doc, kind docmodel.MarkKind) []string {
	var out []string
	for _, b := range d.Blocks {
		for _, in := range b.Inlines {
			if in.Has(kind) {
				out = append(out, in.Attr(kind, docmodel.RunAttr))
			}
		}
	}
	return out
}

func parseOrFail(t *testing.T, src string) docmodel.Doc {
	t.Helper()
	d, _, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// allSame reports whether every element is equal and non-empty.
func allSame(runs []string) bool {
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if r == "" || r != runs[0] {
			return false
		}
	}
	return true
}

// The reported defect, at the level it originates: a substitution whose deleted
// half crosses a code span. Three inlines, ONE span in the file, so one run.
func TestSubstitutionCrossingACodeSpanIsOneRun(t *testing.T) {
	d := parseOrFail(t, "# T\n\n{~~The `retryBudget` value controls~>The retry budget controls~~} retries.\n")

	del := runsFor(d, docmodel.Del)
	if len(del) != 3 {
		t.Fatalf("expected the deleted half to cross 3 inlines, got %d — fixture is wrong", len(del))
	}
	if !allSame(del) {
		t.Errorf("one {~~…~~} in the file produced deleted-half runs %v — "+
			"want one run shared by all three, or opening the document splits it", del)
	}

	// The two HALVES are two spans by design: substitutionSpan pairs them by
	// range and keeps the deleted half's run, and mutateSpan resolves both in
	// one pass. They must not share a token.
	ins := runsFor(d, docmodel.Ins)
	if len(ins) != 1 || ins[0] == "" || ins[0] == del[0] {
		t.Errorf("ins runs %v vs del runs %v — the halves need distinct non-empty runs", ins, del)
	}
}

// The same for a comment anchor, which is the shape a reviewer's own dragged
// selection leaves in the file.
func TestHighlightCrossingACodeSpanIsOneRun(t *testing.T) {
	d := parseOrFail(t, "# T\n\n{==The `retryBudget` value controls==} retries.\n")

	runs := runsFor(d, docmodel.Highlight)
	if len(runs) != 3 {
		t.Fatalf("expected the highlight to cross 3 inlines, got %d — fixture is wrong", len(runs))
	}
	if !allSame(runs) {
		t.Errorf("one {==…==} in the file produced runs %v — want one run shared by all three", runs)
	}
}

// A deletion crossing bold gets the same treatment. The serializer will still
// write it as two segments (it splits on the wrapper) and suggest.List will
// still report the file's two spans honestly — but the DELETION is one thing
// the reviewer decides once, and that is what the run says.
func TestDeletionCrossingBoldIsOneRun(t *testing.T) {
	d := parseOrFail(t, "# T\n\n{--**bold** and plain--} rest.\n")

	runs := runsFor(d, docmodel.Del)
	if len(runs) < 2 {
		t.Fatalf("expected the deletion to cross at least 2 inlines, got %d — fixture is wrong", len(runs))
	}
	if !allSame(runs) {
		t.Errorf("one {--…--} across bold produced runs %v — want one run shared", runs)
	}
}

// Two SEPARATE spans in the file are two decisions, and this is the guarantee
// that stops the fix above from becoming "one run per document". It is
// MintRuns' own stated reason for existing.
func TestTwoAdjacentSpansInTheFileAreTwoRuns(t *testing.T) {
	d := parseOrFail(t, "# T\n\nkeep {--age--}{--age--} here.\n")

	runs := runsFor(d, docmodel.Del)
	if len(runs) != 2 {
		t.Fatalf("two {--…--} in the file produced %d marked inline(s) (%v) — "+
			"want 2, or one accept decides both", len(runs), runs)
	}
	if runs[0] == "" || runs[0] == runs[1] {
		t.Errorf("adjacent separate deletions share run %q — one accept would decide both", runs[0])
	}
}

// Runs must never reach the file. CriticMarkup has no slot for one, and a run
// is a session coordinate: what persists is a comment's ID.
func TestRunsAreNotSerialized(t *testing.T) {
	src := "# T\n\n{~~The `retryBudget` value controls~>The retry budget controls~~} retries.\n"
	out := string(Serialize(parseOrFail(t, src)))
	if out != src {
		t.Errorf("round trip changed the file:\n got %q\nwant %q", out, src)
	}
}
