// ack is the status half of answering a review. The revision itself is the
// .md file, which the agent edits directly and which needs nothing here; this
// is the one message with no passage to anchor to: "heard you", "working on it",
// "done", "no", "can't". The server RECORDS it and the revising readout
// (GET /_galley/revise) carries it — the raw material for telling an agent
// that is thinking from one that died. Rendering it in the browser is a
// deferred gap, not something this file can promise.
//
// No offline form, exactly like revise: an ack with no server has no one to
// reassure.
package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/schuettc/galley/internal/serve"
)

// ackFlags is galley ack's flag set, built by newAckFlags so the app registry
// (NewFlags) and runAck share one construction.
type ackFlags struct {
	state   *string
	note    *string
	changes *string
}

func newAckFlags() (*flag.FlagSet, *ackFlags) {
	fs := flag.NewFlagSet("ack", flag.ContinueOnError)
	v := &ackFlags{
		state: fs.String("state", "", "one of: received, working, answered, declined, failed"),
		note:  fs.String("note", "", "a sentence for the reviewer (optional)"),
		changes: fs.String("changes", "", `a JSON array annotating the changes you made: `+
			`[{"quote":"…","answers":["<key>"],"note":"…"}]. "-" reads it from stdin`),
	}
	return fs, v
}

func runAck(args []string, out, errw io.Writer) error {
	fs, v := newAckFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	state, note, changes := v.state, v.note, v.changes
	// A plain error, not a UsageError: missing --state has always been exit 1
	// here (the pre-migration usageErr call wrapped a plain error too), and
	// this is the one case the migration must not change to exit 2.
	if *state == "" {
		return fmt.Errorf("--state is required")
	}
	manifest, err := readChanges(*changes)
	if err != nil {
		return err
	}
	rt, ok := serve.FindRuntime(pos[0])
	if !ok {
		return errNoServer(pos[0])
	}
	return postAck(rt.URL, *state, *note, manifest)
}

// AckChange is one entry of the reply manifest: the agent's own quotation of
// text it wrote, the instruction keys it believes that change answered, and its
// sentence about it. IT IS AN ANNOTATION AND NEVER A DECLARATION — the server
// computes what changed and drops any entry it cannot place, which is why
// nothing here is required to be right for a round to land.
type AckChange struct {
	Quote   string   `json:"quote"`
	Answers []string `json:"answers,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// readChanges parses --changes. A MALFORMED MANIFEST IS A USAGE ERROR AND AN
// UNPROVABLE ONE IS NOT, and the split is deliberate: the server drops an entry
// whose quote it cannot find because judging that is the server's job, but a
// caller who wrote JSON that will not parse has said nothing at all, and
// posting an ack that silently carries none of what they typed would hide a
// typo behind a success.
func readChanges(arg string) ([]AckChange, error) {
	if arg == "" {
		return nil, nil
	}
	raw := []byte(arg)
	if arg == "-" {
		var err error
		if raw, err = io.ReadAll(os.Stdin); err != nil {
			return nil, fmt.Errorf("could not read --changes from stdin: %w", err)
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var out []AckChange
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("--changes is not a JSON array of {quote, answers, note}: %w", err)
	}
	return out, nil
}

// postAck is the one implementation both entry points share — the CLI here,
// and the channel's galley_ack tool in channel.go.
func postAck(baseURL, state, note string, changes []AckChange) error {
	// A STRUCT, NOT A map[string]string — which is what this was, and a map of
	// strings has nowhere to put the array.
	raw, err := json.Marshal(struct {
		State   string      `json:"state"`
		Note    string      `json:"note"`
		Changes []AckChange `json:"changes,omitempty"`
	}{State: state, Note: note, Changes: changes})
	if err != nil {
		return err
	}
	resp, err := http.Post(baseURL+"/_galley/ack", "application/json", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("the editor did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ack refused: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

// postApprove sends exactly what the page's Approve button sends, so the
// channel's galley_approve tool ends a review the same way a press does.
func postApprove(baseURL string) error {
	resp, err := http.Post(baseURL+"/_galley/revise", "application/json",
		strings.NewReader(`{"verdict":"approve"}`))
	if err != nil {
		return fmt.Errorf("the editor did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("approve refused: %s", strings.TrimSpace(string(body)))
	}
	return nil
}
