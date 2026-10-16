package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/versions"
)

func roundStore(t *testing.T, rs ...versions.Round) string {
	t.Helper()
	doc := filepath.Join(t.TempDir(), "doc.md")
	s := versions.Open(doc)
	for _, r := range rs {
		if _, err := s.Commit("# doc\n\nbody\n", r); err != nil {
			t.Fatalf("committing v%d: %v", r.N, err)
		}
	}
	return doc
}

func ask(key, text, quote string) versions.Ask {
	return versions.Ask{Key: key, Text: text, Quote: quote}
}

// TestARoundCanBeReadAgain is the whole point: a round is delivered once, and
// an agent whose context was compacted mid-round had no sanctioned way back to
// it. `wait` blocks for the NEXT one and `pending` is live state, correctly.
func TestARoundCanBeReadAgain(t *testing.T) {
	doc := roundStore(t,
		versions.Round{N: 1, Reason: versions.ReasonOpened, At: time.Now().UTC()},
		versions.Round{N: 2, Reason: versions.ReasonRevise, At: time.Now().UTC(),
			Authors: []string{"court"},
			Asks: []versions.Ask{
				ask("cm-aaa", "spell this out", "the retryBudget"),
				ask("cd-bbb", "tighten the opening", ""),
			}},
	)
	out, err := capture(t, func() error { return runRound([]string{doc}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"round 2", "spell this out", "the retryBudget",
		"[key cm-aaa]", "[key cd-bbb]", "tighten the opening",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the re-read is missing %q:\n%s", want, out)
		}
	}
}

// TestTheDefaultIsTheRoundThatASKED. The caller is an agent that lost the
// instructions it was working on, and the NEWEST round is very often its own
// landing — which carries no asks and would answer the question with the
// agent's own work.
func TestTheDefaultIsTheRoundThatAsked(t *testing.T) {
	doc := roundStore(t,
		versions.Round{N: 1, Reason: versions.ReasonOpened},
		versions.Round{N: 2, Reason: versions.ReasonRevise, Authors: []string{"court"},
			Asks: []versions.Ask{ask("cm-aaa", "spell this out", "the retryBudget")}},
		versions.Round{N: 3, Reason: versions.ReasonLanded, Authors: []string{"agent"}, Answers: 2},
	)
	out, err := capture(t, func() error { return runRound([]string{doc}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "round 2") || !strings.Contains(out, "spell this out") {
		t.Errorf("the default did not land on the round that asked:\n%s", out)
	}
	if strings.Contains(out, "round 3") {
		t.Errorf("the default landed on the agent's own landing:\n%s", out)
	}
}

// TestAnExplicitNumberReadsThatRoundWhateverItIs — --n is an address, not a
// filter, so it reaches a landing too.
func TestAnExplicitNumberReadsThatRound(t *testing.T) {
	doc := roundStore(t,
		versions.Round{N: 1, Reason: versions.ReasonOpened},
		versions.Round{N: 2, Reason: versions.ReasonRevise,
			Asks: []versions.Ask{ask("cm-aaa", "spell this out", "q")}},
		versions.Round{N: 3, Reason: versions.ReasonLanded, Answers: 2},
	)
	out, err := capture(t, func() error { return runRound([]string{doc, "--n", "3"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "round 3") || !strings.Contains(out, "answers round 2") {
		t.Errorf("--n 3 did not read round 3:\n%s", out)
	}
	if !strings.Contains(out, "no instructions on this round") {
		t.Errorf("a landing must SAY it carries no instructions, to somebody who has just lost theirs:\n%s", out)
	}
}

// TestAMissingRoundIsRefusedRatherThanGuessed.
func TestAMissingRoundIsRefused(t *testing.T) {
	doc := roundStore(t, versions.Round{N: 1, Reason: versions.ReasonOpened})
	if _, err := capture(t, func() error { return runRound([]string{doc, "--n", "9"}, os.Stdout, os.Stderr) }); err == nil {
		t.Fatal("reading a round that does not exist succeeded")
	}
}

// TestTheJSONIsTheStoredRecord — the machine-readable form is the round as
// stored, not a second shape assembled for printing.
func TestTheRoundJSONIsTheStoredRecord(t *testing.T) {
	doc := roundStore(t,
		versions.Round{N: 1, Reason: versions.ReasonRevise,
			Asks: []versions.Ask{ask("cm-aaa", "spell this out", "the retryBudget")}},
	)
	out, err := capture(t, func() error { return runRound([]string{doc, "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	var got versions.Round
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the --json form does not decode as a Round: %v\n%s", err, out)
	}
	if len(got.Asks) != 1 || got.Asks[0].Key != "cm-aaa" {
		t.Errorf("the asks did not survive the round trip: %+v", got.Asks)
	}
}

// TestTheReReadRendersThroughTheNotificationsOwnFormatter is the twin-carrier
// rule, asserted rather than intended: a second renderer would agree the day it
// was written and disagree the day somebody changed one of them.
func TestTheReReadUsesTheNotificationsFormatter(t *testing.T) {
	asks := []versions.Ask{
		ask("cm-aaa", "spell this out", "the retryBudget"),
		ask("cd-bbb", "tighten the opening", ""),
	}
	doc := roundStore(t, versions.Round{N: 1, Reason: versions.ReasonRevise, Asks: asks})
	out, err := capture(t, func() error { return runRound([]string{doc}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	want := formatInstructions(asksAsInstructions(asks))
	if !strings.Contains(out, want) {
		t.Errorf("the re-read is not formatInstructions' output — the two carriers have drifted.\nwant:\n%s\ngot:\n%s", want, out)
	}
}

// TestRoundWithNoDocumentIsRefused. `ledgerArgs` bounds the positional count
// from ABOVE only, so the bare invocation parsed cleanly and indexed an empty
// slice — a panic, from the single most likely way somebody meets a new verb.
func TestRoundWithNoDocumentIsRefused(t *testing.T) {
	if err := runRound(nil, os.Stdout, os.Stderr); err == nil {
		t.Fatal("`galley round` with no document succeeded")
	}
}
