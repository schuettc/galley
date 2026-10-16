package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/subaud-tools/galley/internal/versions"
)

// A ROUND IS DELIVERED ONCE, AND UNTIL NOW THERE WAS NO WAY TO READ IT AGAIN.
//
// The instructions reach the agent in the notification `galley wait` prints or
// the channel pushes. After that they are gone: there is no round verb, and
// `wait` blocks for the NEXT one. An agent whose context is compacted mid-round,
// or which restarts, has lost what it was asked to do — and the standing rule
// gives it no fallback, correctly: a sent round is no longer pending, so
// re-reading `galley pending` returns the wrong thing rather than the round.
//
// NOT HYPOTHETICAL. The session that answered the rounds in galley's own first
// real review appears to have restarted mid-review; muster warned "messaging a
// new session for the first time under a previously used name (was it
// restarted?)". The two defects that review found were both about the agent
// acting on something other than what it had been sent.
//
// THE DATA ALREADY EXISTS AND ONLY NEEDED READING. `rounds.jsonl` stores each
// round's `asks` with key, text and quote — exactly what the notification
// renders — because a Change has to be able to point AT an Ask. Nothing is
// captured here and nothing new is stored.
//
// IT RENDERS THROUGH `formatInstructions`, the same function `galley wait` and
// the channel render with, so a re-read and the original cannot drift. That is
// the twin-carrier rule this repository has paid for more than any other: a
// second renderer would agree on the day it was written and disagree on the day
// somebody changed one of them.
//
// REJECTED: `galley pending --round N`. It overloads the one command whose
// entire contract is that pending is LIVE state and a sent round is not
// pending — the exact confusion the rule exists to prevent, spelled into the
// flag set of the command that states it.

// roundFlags is galley round's flag set, built by newRoundFlags so the app
// registry (NewFlags) and runRound share one construction.
type roundFlags struct {
	n      *int
	asJSON *bool
}

func newRoundFlags() (*flag.FlagSet, *roundFlags) {
	fs := flag.NewFlagSet("round", flag.ContinueOnError)
	v := &roundFlags{
		n:      fs.Int("n", 0, "the round to read (default: the latest one carrying instructions)"),
		asJSON: fs.Bool("json", false, "emit the stored round record as JSON"),
	}
	return fs, v
}

func runRound(args []string, out, errw io.Writer) error {
	fs, v := newRoundFlags()
	// `splitPositional` requires exactly one document — no panic on a bare
	// `galley round` with no document, and no silent acceptance of a second
	// argument either. Measured before this migration: a manual
	// `len(rest) != 1` check backed by `ledgerArgs`'s max-only bound, one
	// panic away from a bare invocation, the single most likely way somebody
	// meets a new verb.
	rest, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	n, asJSON := v.n, v.asJSON
	doc := rest[0]

	store := versions.Open(doc)
	rounds, err := store.List()
	if err != nil {
		return fmt.Errorf("reading this document's rounds: %w", err)
	}
	if len(rounds) == 0 {
		return errors.New("this document has no rounds yet — nothing has been sent")
	}

	want, err := pickRound(rounds, *n)
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(want)
	}
	fmt.Print(roundText(want))
	return nil
}

// pickRound resolves --n, or finds the round to recover when it is absent.
//
// THE DEFAULT IS THE LATEST ROUND THAT ASKED, not simply the latest. The caller
// is an agent that lost the instructions it was working on, and the newest
// round is very often its OWN landing — which carries no asks and would answer
// the question with the agent's own work.
func pickRound(rounds []versions.Round, n int) (versions.Round, error) {
	if n > 0 {
		for _, r := range rounds {
			if r.N == n {
				return r, nil
			}
		}
		return versions.Round{}, fmt.Errorf("no round %d — this document has %d", n, len(rounds))
	}
	for i := len(rounds) - 1; i >= 0; i-- {
		if len(rounds[i].Asks) > 0 {
			return rounds[i], nil
		}
	}
	// EVERY ROUND WITH NO ASKS IS STILL AN ANSWER, not an error: a document
	// whose rounds are all landings, or all cut before the manifest recorded
	// asks, has a real history and simply nothing to re-read. Handing back the
	// latest one lets roundText say that in words rather than making the
	// caller guess from an exit code.
	return rounds[len(rounds)-1], nil
}

// roundText renders one round the way the agent was given it.
func roundText(r versions.Round) string {
	var b strings.Builder
	fmt.Fprintf(&b, "round %d · %s", r.N, r.Reason)
	if len(r.Authors) > 0 {
		fmt.Fprintf(&b, " · %s", strings.Join(r.Authors, ", "))
	}
	if !r.At.IsZero() {
		fmt.Fprintf(&b, " · %s", r.At.Format("2006-01-02 15:04"))
	}
	b.WriteByte('\n')
	if r.Answers > 0 {
		fmt.Fprintf(&b, "answers round %d\n", r.Answers)
	}
	if len(r.Asks) == 0 {
		// SAID IN WORDS, and it names the two shapes that reach here, because
		// "no instructions" over a landing reads as data loss to somebody who
		// has just lost their instructions.
		b.WriteString("\nno instructions on this round — a landing, or a round cut " +
			"before galley recorded asks\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d instruction(s)\n\n", len(r.Asks))
	b.WriteString(formatInstructions(asksAsInstructions(r.Asks)))
	return b.String()
}

// asksAsInstructions is the ONE conversion, and it exists so the rendering can
// be `formatInstructions` rather than a second copy of it. An Ask is what was
// STORED; an instructionPayload is what the notification carried, and the three
// fields the renderer reads are the three an Ask keeps.
func asksAsInstructions(asks []versions.Ask) []instructionPayload {
	out := make([]instructionPayload, 0, len(asks))
	for _, a := range asks {
		out = append(out, instructionPayload{Key: a.Key, Text: a.Text, Quote: a.Quote})
	}
	return out
}
