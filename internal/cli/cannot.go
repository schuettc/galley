// cannot.go is THE EXCEPTION, and it is a REPORT rather than a turn in a
// conversation.
//
// An instruction is discharged by the revision itself, so there is nothing to
// resolve and nothing to reply to. What is left is the one case with no
// revision. It is a ROUND — visible where every other round is visible, in the
// history, paired with the instruction it could not answer — and NOT an ack:
// `failed` is a status about a press, and this is a report about an
// instruction whose only answer is a different instruction.
//
// The agent's WRITE surface is the .md file itself (see the handoff in
// internal/serve); this file carries the one verb that has no revision.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/serve"
	"github.com/subaud-tools/galley/internal/suggest"
	"github.com/subaud-tools/galley/internal/versions"
)

// cannotFlags is galley cannot's flag set, built by newCannotFlags so the app
// registry (NewFlags) and runCannot share one construction.
type cannotFlags struct {
	why *string
}

func newCannotFlags() (*flag.FlagSet, *cannotFlags) {
	fs := flag.NewFlagSet("cannot", flag.ContinueOnError)
	v := &cannotFlags{
		why: fs.String("why", "", "what stopped you — one line, for the reviewer"),
	}
	return fs, v
}

func runCannot(args []string, out, errw io.Writer) error {
	fs, v := newCannotFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	why := v.why
	doc := pos[0]
	// A plain error, not a UsageError: missing --why has always been exit 1
	// here (the pre-migration usageErr call wrapped a plain error too), and
	// this is the one case the migration must not change to exit 2.
	if strings.TrimSpace(*why) == "" {
		return errors.New("--why is required: say what stopped you")
	}
	rt, ok := serve.FindRuntime(doc)
	if !ok {
		return offlineCannot(doc, *why)
	}
	body, _ := json.Marshal(map[string]string{"why": *why})
	resp, err := http.Post(rt.URL+"/_galley/cannot", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("the editor did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("the report was refused: %s", strings.TrimSpace(string(msg)))
	}
	fmt.Println("reported — the reviewer sees it as a round of its own")
	return nil
}

// offlineCannot records the exception with no server running: the round is
// still a fact worth keeping, and losing it to a stopped editor would make
// the report vanish exactly when nobody was there to hear it. The content
// committed is the canonical serialization of what is on disk — the same rule
// seedVersions states — and unchanged bytes are the point: this round IS
// "nothing moved, here is why".
func offlineCannot(docPath, why string) error {
	abs, err := filepath.Abs(docPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	model, _, err := markdown.Parse(raw)
	if err != nil {
		return err
	}
	store := versions.Open(abs)
	content := string(markdown.Serialize(suggest.ClearInstructions(model)))
	// INSPECT, NOT Latest — A DROPPED LINE IS A ROUND THAT VANISHED, and this is
	// the command that pays for it. `cannot` is a SECOND PROGRAM appending to a
	// manifest a running galley also writes, and both the answers pointer below
	// and the number Commit assigns are computed from the rounds that could be
	// read. So a line the reader gave up on does not merely disappear from the
	// history: it makes this round answer the wrong instruction, or take a
	// number already on disk. The read loop used to swallow that silently.
	rounds, problems, err := store.Inspect()
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "warning: %s/rounds.jsonl %s\n", store.Dir(), p)
	}
	answers := 0
	if len(rounds) > 0 {
		answers = rounds[len(rounds)-1].N
	}
	if _, err := store.Commit(content, versions.Round{
		At:          time.Now().UTC(),
		Authors:     []string{versions.AuthorAgent},
		Reason:      versions.ReasonCouldNot,
		Instruction: "could not: " + why,
		Answers:     answers,
	}); err != nil {
		return err
	}
	fmt.Println("reported — recorded as a round of its own (no server running)")
	return nil
}
