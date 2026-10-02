package suggest

import (
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// ONE AUTHORED EDIT IS ONE SPAN.
//
// The companion clause to CLAUDE.md's "one span in the file is one decision
// everywhere". That rule defends the file -> cards direction; these tests
// defend intent -> file, which is where the corruption came from: a target of
// plain prose crossing a code span was applied one inline at a time against a
// live document, so one authored edit arrived as three independently decidable
// cards, and three individually reasonable decisions put a word neither side
// wrote on disk.
//
// Every test here mints runs, because MINTING IS WHAT MADE IT LIVE-ONLY.
// Offline no run is ever minted, every token is "", List's grouping key
// matches on emptiness and the pieces collapse by accident. A test that skips
// MintRuns passes against the bug.

const (
	codeSpanBefore = "The retryBudget value controls retries."
	codeSpanAfter  = "The retry budget controls retries."
	codeSpanTarget = "The retryBudget value controls"
)

// codeSpanDoc is the review's reproduction at the model level:
//
//	The `retryBudget` value controls retries.
//
// Three inlines, because a code span is its own inline. codeSpanTarget is
// plain prose — it carries no markup, so it matches — and it crosses all three.
func codeSpanDoc() docmodel.Doc {
	return docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Paragraph,
		Inlines: []docmodel.Inline{
			{Text: "The "},
			{Text: "retryBudget", Marks: []docmodel.Mark{{Kind: docmodel.Code}}},
			{Text: " value controls retries."},
		},
	}}}
}

func docText(d docmodel.Doc) string {
	out := ""
	var walk func([]docmodel.Block)
	walk = func(blocks []docmodel.Block) {
		for _, b := range blocks {
			out += plainText(b.Inlines)
			walk(b.Children)
		}
	}
	walk(d.Blocks)
	return out
}

func testAt() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) }

// One reviewer selection across a code span is one highlight, not three cards.
func TestCommentOnAcrossAFormattingRunIsOneSpanLive(t *testing.T) {
	edited, err := CommentOn(codeSpanDoc(), codeSpanTarget, "cm-0000000000000005", "court", testAt())
	if err != nil {
		t.Fatalf("CommentOn: %v", err)
	}

	pending := List(MintRuns(edited))
	if len(pending) != 1 {
		for i, p := range pending {
			t.Logf("  [%d] %s %q", i, p.Kind, p.Text)
		}
		t.Fatalf("one authored comment produced %d suggestions, want 1", len(pending))
	}
	if pending[0].Kind != KindComment {
		t.Errorf("kind = %s, want %s", pending[0].Kind, KindComment)
	}
	if pending[0].Text != codeSpanTarget {
		t.Errorf("highlight = %q, want the whole selection %q", pending[0].Text, codeSpanTarget)
	}
}

// CommentOnRange is the BROWSER's path — what the editor posts when a reviewer
// drags a selection — and it splits for the same reason. No agent is involved
// here at all, which is why no prompt could ever have mitigated this.
func TestCommentOnRangeAcrossAFormattingRunIsOneSpanLive(t *testing.T) {
	edited, err := CommentOnRange(codeSpanDoc(), []int{0}, 0, len([]rune(codeSpanTarget)), "cm-00000000000000ff", "court", testAt())
	if err != nil {
		t.Fatalf("CommentOnRange: %v", err)
	}

	pending := List(MintRuns(edited))
	if len(pending) != 1 {
		for i, p := range pending {
			t.Logf("  [%d] %s %q", i, p.Kind, p.Text)
		}
		t.Fatalf("one dragged selection produced %d cards, want 1", len(pending))
	}
}

// THIS IS THE HARM, and the reason counting spans is not enough, reached the
// likeliest way anyone actually meets it: a reviewer OPENS YESTERDAY'S
// DOCUMENT. Nothing is authored in this session at all — the span is already in
// the file, correct and clean, and reading it is what used to split it into
// three. It starts from bytes on disk and exercises the read path, which is
// where the document's OWN span boundaries have to survive.
//
// Decide the pieces one at a time the way a reviewer would, and check the
// document after EVERY decision, not only at the end. There are exactly two
// coherent outcomes: the edit was taken, or it was not. The review reproduced a
// third, "The retryBudgetThe retry budget controls retries.", which is a word
// neither side wrote.
func TestDecidingASpanReadFromAFilePieceByPieceNeverCorruptsTheDocument(t *testing.T) {
	// No heading: docText concatenates every block, and the point of comparison
	// here is the prose of the paragraph the span lives in.
	const onDisk = "{~~The `retryBudget` value controls~>The retry budget controls~~} retries.\n"

	parsed, _, err := markdown.Parse([]byte(onDisk))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	live := MintRuns(parsed)

	if n := len(List(live)); n != 1 {
		for i, p := range List(live) {
			t.Logf("  [%d] %s %q", i, p.Kind, p.Text)
		}
		t.Fatalf("one span in the file read as %d suggestions once loaded, want 1", n)
	}

	coherent := func(stage string, d docmodel.Doc) {
		t.Helper()
		for _, c := range []struct {
			how    string
			accept bool
		}{{"taking the rest", true}, {"declining the rest", false}} {
			done := decideRest(t, d, c.accept)
			got := docText(done)
			if got != codeSpanBefore && got != codeSpanAfter {
				t.Fatalf("%s, %s: document reads %q\n  want either %q (declined) or %q (taken)",
					stage, c.how, got, codeSpanBefore, codeSpanAfter)
			}
		}
	}

	coherent("as opened", live)
	for i := 0; i < 8; i++ {
		pending := List(live)
		var next Pending
		found := false
		for _, p := range pending {
			if p.Kind != KindComment {
				next, found = p, true
				break
			}
		}
		if !found {
			return
		}
		live, err = applyDecisionOn(live, next.ID, next.Kind != KindDelete)
		if err != nil {
			t.Fatalf("deciding %s %q: %v", next.Kind, next.Text, err)
		}
		coherent("after deciding "+string(next.Kind)+" "+next.Text, live)
	}
	t.Fatalf("still decidable after 8 rounds: %d spans left", len(List(live)))
}

func applyDecisionOn(d docmodel.Doc, id string, accept bool) (docmodel.Doc, error) {
	if accept {
		return Accept(d, id)
	}
	return Reject(d, id)
}

// decideRest takes (or declines) every decidable suggestion left in d, one at a
// time against a fresh List, the way a reviewer finishing the round would.
func decideRest(t *testing.T, d docmodel.Doc, accept bool) docmodel.Doc {
	t.Helper()
	// Bounded, so a decision that leaves its span behind fails instead of
	// spinning.
	for i := 0; i < 64; i++ {
		next := ""
		for _, p := range List(d) {
			if p.Decidable {
				next = p.ID
				break
			}
		}
		if next == "" {
			return d
		}
		var err error
		if d, err = applyDecisionOn(d, next, accept); err != nil {
			t.Fatalf("deciding %s: %v", next, err)
		}
	}
	t.Fatalf("still decidable after 64 decisions: %+v", List(d))
	return d
}

// The guarantee MintRuns exists for, stated against the AUTHORING path rather
// than against a hand-built document: two separately authored edits over
// identical adjacent text stay two decisions. Stamping one run per authored
// edit must not become one run per document.
//
// Drop the run from spanKey and this fails, but ONE STEP EARLIER than the count
// assertion below: the second CommentOnRange returns "could not locate the
// comment just created", because commentAtMatch checks that List can see the
// highlight it just made and — with the two merged into one span — it cannot.
// That check is the guarantee's other guard, and it is a better failure than a
// wrong count. Do not "fix" the fixture to route around it.
func TestTwoSeparatelyAuthoredEditsStayTwoDecisions(t *testing.T) {
	// ADJACENT, deliberately: the two comments touch, so nothing but the run
	// can keep them apart. Separated by other text — "age and image", which is
	// what this fixture used to be — they could not group whatever the runs
	// were, and the count assertion below could not fail.
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "ageage"}},
	}}}

	first, err := CommentOnRange(d, []int{0}, 0, 3, "cm-0000000000000006", "court", testAt())
	if err != nil {
		t.Fatalf("first comment: %v", err)
	}
	// The second "age", butted straight against the first and authored in the
	// same second — indistinguishable by author and instant alone.
	second, err := CommentOnRange(first, []int{0}, 3, 6, "cm-0000000000000007", "court", testAt())
	if err != nil {
		t.Fatalf("second comment: %v", err)
	}

	pending := List(MintRuns(second))
	if len(pending) != 2 {
		for i, p := range pending {
			t.Logf("  [%d] %s %q run=%q", i, p.Kind, p.Text, p.Run)
		}
		t.Fatalf("two authored comments produced %d suggestions, want 2", len(pending))
	}
	if pending[0].Run == "" || pending[0].Run == pending[1].Run {
		t.Errorf("runs not distinct: %q %q — one accept would decide both", pending[0].Run, pending[1].Run)
	}
}
