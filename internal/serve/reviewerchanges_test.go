package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/versions"
)

// THE AGENT MUST BE TOLD WHAT THE REVIEWER REMOVED, or it writes it back.
//
// Measured 2026-08-22 on a real review: the reviewer deleted a section in
// round 3 and the agent's round 4 restored it inside an 8k expansion answering
// a different instruction. The version history proves the deletion worked —
// absent from v3, present again in v4 — and the round notification the agent
// received carried only instructions.

func commit(t *testing.T, s *versions.Store, content string, r versions.Round) {
	t.Helper()
	if _, err := s.Commit(content, r); err != nil {
		t.Fatalf("committing v%d: %v", r.N, err)
	}
}

// TestAReviewerRoundReportsWhatWasRemoved is the case that cost a section.
func TestAReviewerRoundReportsWhatWasRemoved(t *testing.T) {
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, "# Doc\n\nKept paragraph.\n\nDoomed paragraph.\n",
		versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, "# Doc\n\nKept paragraph.\n",
		versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	got, dropped := ReviewerChanges(s)
	if dropped != 0 {
		t.Errorf("dropped %d on a two-change round", dropped)
	}
	var removed []string
	for _, c := range got {
		if c.Kind == "removed" {
			removed = append(removed, c.Before)
		}
	}
	if len(removed) == 0 {
		t.Fatalf("a round that deleted a paragraph reported no removal: %+v — this is the defect that let an agent write the reviewer's deletion back", got)
	}
	if !strings.Contains(strings.Join(removed, " "), "Doomed") {
		t.Errorf("the removal does not name the deleted text: %+v", removed)
	}
}

// TestAnAgentRoundReportsNothing is the attribution half, and it matters as
// much as the first: diffing a LANDING the same way would report the agent's
// own work back to it as the reviewer's, which inverts the very attribution
// this handover exists to carry.
func TestAnAgentRoundReportsNothing(t *testing.T) {
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, "# Doc\n\nOne.\n", versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, "# Doc\n\nOne. Two, added by the agent.\n",
		versions.Round{N: 2, Reason: versions.ReasonLanded, Authors: []string{"agent"}})

	if got, _ := ReviewerChanges(s); len(got) != 0 {
		t.Errorf("an agent's landing was reported as the reviewer's hand: %+v", got)
	}
}

// TestTheFirstRoundReportsNothing — v1 has no predecessor to diff against, and
// "everything in the document" is not a list of what the reviewer changed.
func TestTheFirstRoundReportsNothing(t *testing.T) {
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, "# Doc\n\nOne.\n",
		versions.Round{N: 1, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	if got, _ := ReviewerChanges(s); len(got) != 0 {
		t.Errorf("the opening round reported changes: %+v", got)
	}
}

// TestAnUnreadableStoreDoesNotFailTheRound holds the ledger's rule: a version
// that cannot be read must not turn a Revise press into an error page. The
// cost of nothing is an agent told less; the cost of an error is a review that
// stops.
func TestAnUnreadableStoreDoesNotFailTheRound(t *testing.T) {
	if got, dropped := ReviewerChanges(nil); got != nil || dropped != 0 {
		t.Errorf("a nil store produced %+v / %d instead of nothing", got, dropped)
	}
}

// TestTheCapIsStatedRatherThanSilent — a truncation nobody is told about reads
// as "that is all of them", which is worse than the truncation.
func TestTheCapIsStatedRatherThanSilent(t *testing.T) {
	var before, after strings.Builder
	before.WriteString("# Doc\n\n")
	after.WriteString("# Doc\n\n")
	// EVERY PARAGRAPH DISTINCT. The first version of this fixture varied the
	// text on `i%7`, so most paragraphs repeated — and once summarise deduped,
	// the cap was never reached and the test failed for a reason that had
	// nothing to do with the cap. A fixture whose items collapse cannot
	// exercise a bound on how many items there are.
	for i := 0; i < reviewerChangeCap+10; i++ {
		fmt.Fprintf(&before, "Paragraph number %d is distinct from every other one.\n\n", i)
	}
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, before.String(), versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, after.String(), versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	got, dropped := ReviewerChanges(s)
	if len(got) > reviewerChangeCap {
		t.Errorf("reported %d changes, cap is %d", len(got), reviewerChangeCap)
	}
	if dropped == 0 {
		t.Errorf("%d changes fitted under a cap of %d with nothing dropped — the cap is not being counted",
			len(got), reviewerChangeCap)
	}
}

// TestOneChangeIsReportedOnce covers the shape of the ops list: `coalesceBlocks`
// hands a boundary head's place to the first real unit under it and marks the
// superseded op `Absorbed`, so a deleted paragraph arrives as TWO delete ops
// carrying the same text. Every reader in this repo filters absorbed ops;
// this one has to as well, or the agent is told a paragraph was removed twice
// and goes looking for a second that never existed.
func TestOneChangeIsReportedOnce(t *testing.T) {
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, "# Draft\n\nKept paragraph.\n\nA section the reviewer will delete.\n",
		versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, "# Draft\n\nKept paragraph.\n",
		versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	got, _ := ReviewerChanges(s)
	var removals int
	for _, c := range got {
		if c.Kind == "removed" {
			removals++
		}
	}
	if removals != 1 {
		t.Errorf("one deleted paragraph produced %d removals: %+v", removals, got)
	}
}

// TestTwoIDENTICALParagraphsAreTwoRemovals is why the absorbed filter is not
// interchangeable with deduping identical entries, which is what this file did
// first.
//
// A dedupe collapses these two into one and UNDER-REPORTS the reviewer's own
// deletions to the agent — the exact harm this file exists to prevent,
// reintroduced by a workaround for a misread of the ops list. The filter is
// correct here because the second op is a real, separate deletion that no
// boundary absorbed.
func TestTwoIdenticalParagraphsAreTwoRemovals(t *testing.T) {
	s := versions.Open(t.TempDir() + "/doc.md")
	// SEPARATED BY KEPT PROSE, and that is the fixture doing work. Adjacent
	// identical paragraphs coalesce into ONE region, so a fixture with the two
	// side by side reports one removal and cannot tell a correct filter from a
	// dedupe — it passes either way. Measured: the first cut of this test had
	// them adjacent and was green against the very dedupe it was written to
	// forbid.
	commit(t, s, "# Draft\n\nCut this line.\n\nKeep me.\n\nCut this line.\n\nKeep me too.\n",
		versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, "# Draft\n\nKeep me.\n\nKeep me too.\n",
		versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	got, _ := ReviewerChanges(s)
	var removals int
	for _, c := range got {
		if c.Kind == "removed" {
			removals++
		}
	}
	if removals != 2 {
		t.Errorf("two identical deleted paragraphs produced %d removals, want 2: %+v — a dedupe would report 1 and the agent would put one of them back", removals, got)
	}
}

// TestNoBoundarySentinelReachesTheAgent pins the guard that was lost once
// already, by a restore from a stale backup, and would have shipped.
//
// `Units` opens every block with a sentinel whose text is "\x00para" or
// "\x00heading". A deleted block yields a delete op for it, and the empty-text
// guard does not catch it because a sentinel is not empty. Measured before the
// filter existed: a round removing one paragraph told the agent
// `REMOVED (2): "Cut this line." · "\x00para"`.
//
// This asserts on the TEXT rather than on the count, because a count is
// satisfied by the wrong two entries — which is exactly how the sibling test
// passed while handing a sentinel over.
func TestNoBoundarySentinelReachesTheAgent(t *testing.T) {
	// ADJACENT IDENTICAL PARAGRAPHS, and the fixture is the whole test. The
	// first cut used one deleted paragraph at the end of the document, which
	// produces NO boundary delete op — so the check passed with the filter
	// removed, which is to say it could not fail. This shape is where the
	// sentinel was actually measured.
	s := versions.Open(t.TempDir() + "/doc.md")
	commit(t, s, "# Draft\n\nKeep me.\n\nCut this line.\n\nCut this line.\n\nKeep me too.\n",
		versions.Round{N: 1, Reason: versions.ReasonOpened})
	commit(t, s, "# Draft\n\nKeep me.\n\nKeep me too.\n",
		versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"}})

	got, _ := ReviewerChanges(s)
	for _, c := range got {
		if strings.Contains(c.Before, "\x00") || strings.Contains(c.After, "\x00") {
			t.Errorf("a boundary sentinel reached the agent as the reviewer's words: %+v", c)
		}
	}
	if len(got) == 0 {
		t.Fatalf("no changes at all — the fixture stopped exercising anything")
	}
}
