package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestASentInstructionLEAVESTheRail is the behaviour the whole "an instruction
// is discharged by the revision itself" design rests on, and NOTHING asserted
// it.
//
// THE FINDING THIS TEST CAME FROM WAS WRONG, and the correction is worth more
// than the test. galley's first real review recorded three instructions still
// sitting in the rail after the rounds that answered them, and concluded: "THE
// REAL DEFECT IS THAT NOTHING EVER MARKS AN INSTRUCTION DONE… no mechanism
// implements the discharge, so instructions accumulate in the rail forever."
// The proposed fix was a landing-time discharge built on review.Thread's
// then-surviving Resolved/Outcome fields (Outcome has since been deleted).
//
// Measured on dev before building any of it: TWO instructions in, one send,
// ZERO instructions out. The discharge exists and runs at SEND time —
// handleRoundHandoff clears the working-copy carriers with
// suggest.ClearInstructions and deletes each thread by key — and it landed
// 2026-08-19, three days BEFORE the review that reported it missing.
//
// So the reported symptom has a different cause, and the likeliest one is
// narrower: an instruction written AFTER a round was sent is never in that
// round, and the agent's landing can still remove the phrase it was anchored
// to. That is live work whose anchor is gone, not accumulated finished work —
// and the rail called it "UNPLACED · THEIR WORDS WERE REMOVED", which reads as
// a problem either way. Whether that heading should exist is a design question
// about a population of unsent instructions; it is NOT the missing mechanism
// the finding described, and building that mechanism would have added a second
// discharge beside a working one.
//
// What was genuinely missing is this test. A behaviour every surface depends on
// and no check names is one refactor away from being the finding's own
// description of it.
func TestASentInstructionLeavesTheRail(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md,
		[]byte("# T\n\nCognito mints every token.\n\nThe identity story here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	// A revise with nothing listening is refused, and this test is about what
	// the send does to the review rather than about who hears it.
	s.OnRevise = "true"

	for _, in := range []map[string]any{
		{"op": "comment", "target": "Cognito mints every token.", "text": "too many punchy sentences"},
		{"op": "comment", "target": "The identity story here.", "text": "story is a weird word"},
	} {
		if rec := postRec(t, s, "/_galley/instruct", in); rec.Code >= 300 {
			t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
		}
	}
	before, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Instructions) != 2 {
		t.Fatalf("the fixture did not file two instructions (%d) — it cannot show a send clearing them",
			len(before.Instructions))
	}
	if stored := loadUnsent(t, s); len(stored) != 2 {
		t.Fatalf("pending.json holds %d instructions before the send, want 2: %+v", len(stored), stored)
	}

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 && rec.Code != http.StatusConflict {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}

	after, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Instructions) != 0 {
		t.Errorf("%d instruction(s) survived the send that carried them: %+v — "+
			"they will sit in the rail as live work the reviewer has already asked for",
			len(after.Instructions), after.Instructions)
	}
	// And they do not come back at the next start: the unsent round is the
	// rail's durable half.
	if stored := loadUnsent(t, s); len(stored) != 0 {
		t.Errorf("pending.json still holds %d sent instruction(s): %+v", len(stored), stored)
	}
}

// TestTheRoundKEEPSWhatTheRailGaveUp is the other half, and without it the
// check above is satisfied by simply losing the instructions. The words have to
// survive SOMEWHERE — the round record is the only place they still exist once
// the threads are deleted, which is what makes `galley round` a recovery and
// versions.Ask's own comment true.
func TestTheRoundKeepsWhatTheRailGaveUp(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte("# T\n\nCognito mints every token.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Cognito mints every token.", "text": "too many punchy sentences",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 && rec.Code != http.StatusConflict {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}

	rounds, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rounds {
		for _, a := range r.Asks {
			if a.Text == "too many punchy sentences" {
				found = true
				if a.Key == "" {
					t.Errorf("the round kept the words and lost the key — a change can only point at an ask that has a name")
				}
			}
		}
	}
	for _, c := range loadUnsent(t, s) {
		if c.Text == "too many punchy sentences" {
			t.Errorf("the round kept the words and pending.json kept them too — the next start would show them as unsent")
		}
	}
	if !found {
		t.Errorf("the send cleared the rail and the round kept nothing — the reviewer's words are gone from everywhere: %+v", rounds)
	}
}
