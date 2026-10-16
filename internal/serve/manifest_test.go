package serve

import (
	"net/http"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/versions"
)

// THE SERVER OWNS THE CHANGE LIST AND THE AGENT MAY ONLY ANNOTATE IT. These
// pin the two checks and, between them, the one thing the feature promises:
// an annotation that does not match a computed change is DROPPED, never
// guessed at and never allowed to invent a change of its own.
func TestManifestDropsWhatItCannotProve(t *testing.T) {
	before := "The budget should stay explicit and readable in the config file.\n"
	after := "The budget of 12 attempts over 48 hours stays explicit in the config file.\n"
	sent := []string{"t-1", "t-2"}

	kept, dropped := matchManifest(before, after, sent, []ackChange{
		{Quote: "of 12 attempts over 48 hours", Answers: []string{"t-1"}, Note: "Bounded it."},
		{Quote: "a sentence nobody wrote", Answers: []string{"t-1"}, Note: "Fabricated."},
		{Quote: "of 12 attempts over 48 hours", Answers: []string{"t-9"}, Note: "Unsent key."},
	})

	if dropped != 2 {
		t.Errorf("want 2 dropped, got %d", dropped)
	}
	if len(kept) != 1 {
		t.Fatalf("want 1 kept, got %d: %+v", len(kept), kept)
	}
	if kept[0].Note != "Bounded it." || len(kept[0].Answers) != 1 || kept[0].Answers[0] != "t-1" {
		t.Errorf("wrong entry survived: %+v", kept[0])
	}
	if kept[0].Locator != "of 12 attempts over 48 hours" {
		t.Errorf("locator is the agent's own quote, got %q", kept[0].Locator)
	}
}

func TestManifestKeepsNothingWhenNothingChanged(t *testing.T) {
	same := "One sentence.\n"
	kept, dropped := matchManifest(same, same, []string{"t-1"}, []ackChange{
		{Quote: "One sentence.", Answers: []string{"t-1"}, Note: "Claims a change that is not there."},
	})
	if len(kept) != 0 || dropped != 1 {
		t.Errorf("a claim about an unchanged document must not survive: kept=%+v dropped=%d", kept, dropped)
	}
}

// AN UNATTRIBUTED CHANGE IS LEGAL. The agent is allowed to say what it wrote
// without claiming it answered anybody — the reading state renders that with
// no ask line — so an empty Answers must not be mistaken for a failed check.
func TestManifestKeepsAChangeThatAnswersNobody(t *testing.T) {
	kept, dropped := matchManifest("One sentence.\n", "One sentence. And a second.\n", []string{"t-1"},
		[]ackChange{{Quote: "And a second.", Note: "Added a follow-on."}})
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("an unattributed change was dropped: kept=%+v dropped=%d", kept, dropped)
	}
	if len(kept[0].Answers) != 0 {
		t.Errorf("answers were invented: %+v", kept[0].Answers)
	}
}

// A QUOTE THAT LANDS IN TWO REGIONS NAMES NEITHER OF THEM. "Exactly one" is
// the check, not "at least one": a locator matching two changes cannot say
// which change the note is about, and guessing is the one thing forbidden.
func TestManifestDropsAQuoteThatMatchesTwoRegions(t *testing.T) {
	before := "Alpha one.\n\nA middle paragraph.\n\nBeta one.\n"
	after := "Alpha one. The budget is bounded.\n\nA middle paragraph.\n\nBeta one. The budget is bounded.\n"
	kept, dropped := matchManifest(before, after, []string{"t-1"},
		[]ackChange{{Quote: "The budget is bounded.", Answers: []string{"t-1"}, Note: "Which one?"}})
	if len(kept) != 0 || dropped != 1 {
		t.Errorf("an ambiguous locator survived: kept=%+v dropped=%d", kept, dropped)
	}
}

// THE WHOLE LOOP, THROUGH THE REAL ENDPOINTS. The reviewer asks, the agent
// edits the file and returns with a manifest, and the round that lands carries
// the testimony it could prove and none of the testimony it could not — while
// the ack itself still answers 204, because the landing is never contingent on
// the agent's bookkeeping.
func TestAnAckCarriesTheManifestOntoTheRound(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe budget should stay explicit.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "budget", "bound it — say how many attempts over how long")
	press(t, s, "{}")
	asked := rounds(t, s)
	if len(asked) != 2 {
		t.Fatalf("the press cut %d rounds, want 2", len(asked))
	}
	// THE KEYS SURVIVE THE SEND THAT DELETED THEIR THREADS. Without this the
	// second check below has nothing to check against and every entry naming a
	// real key would drop.
	if len(asked[1].Asks) != 1 || asked[1].Asks[0].Key == "" {
		t.Fatalf("the reviewer's round did not record its asks: %+v", asked[1].Asks)
	}
	key := asked[1].Asks[0].Key

	agentEdits(t, s, "# Title\n\nThe budget of 12 attempts over 48 hours stays explicit.\n")

	ackWith(t, s, "answered", "bounded the budget", []ackChange{
		{Quote: "of 12 attempts over 48 hours", Answers: []string{key}, Note: "Added the 12-attempt / 48-hour budget."},
		{Quote: "a sentence nobody wrote", Answers: []string{key}, Note: "Fabricated."},
		{Quote: "of 12 attempts over 48 hours", Answers: []string{"t-nobody-sent-this"}, Note: "Unsent key."},
	})

	rs := rounds(t, s)
	landed := rs[len(rs)-1]
	if landed.Reason != versions.ReasonLanded {
		t.Fatalf("the last round was cut for %q, want %q", landed.Reason, versions.ReasonLanded)
	}
	if len(landed.Changes) != 1 {
		t.Fatalf("the landed round carries %d changes, want 1: %+v", len(landed.Changes), landed.Changes)
	}
	c := landed.Changes[0]
	if c.Locator != "of 12 attempts over 48 hours" || c.Note != "Added the 12-attempt / 48-hour budget." {
		t.Errorf("the surviving entry is not the provable one: %+v", c)
	}
	if len(c.Answers) != 1 || c.Answers[0] != key {
		t.Errorf("the entry lost the ask it answers: %+v", c.Answers)
	}
	// AND NOTHING WAS GUESSED.
	for _, x := range landed.Changes {
		if strings.Contains(x.Note, "Fabricated") || strings.Contains(x.Note, "Unsent") {
			t.Errorf("an unprovable entry landed on the round: %+v", x)
		}
	}
}

// A ROUND WITH NO MANIFEST IS THE ORDINARY ROUND, and it must stay one: the
// field is optional on both carriers, and an agent that never learned about it
// lands exactly the round it landed before.
func TestAnAckWithNoManifestLandsAnOrdinaryRound(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe budget should stay explicit.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "budget", "bound it")
	press(t, s, "{}")
	agentEdits(t, s, "# Title\n\nThe budget of 12 attempts stays explicit.\n")
	ackAs(t, s, "answered", "bounded it")

	rs := rounds(t, s)
	landed := rs[len(rs)-1]
	if landed.Changes != nil {
		t.Errorf("a manifest nobody sent turned up on the round: %+v", landed.Changes)
	}
	if !strings.Contains(landed.Instruction, "bounded it") {
		t.Errorf("the round lost the ack note: %q", landed.Instruction)
	}
}

func ackWith(t *testing.T, s *EditServer, state, note string, changes []ackChange) {
	t.Helper()
	rec := post(t, s.Handler(), "/_galley/ack", map[string]any{
		"state": state, "note": note, "changes": changes,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ack %s answered %d: %s", state, rec.Code, rec.Body.String())
	}
}
