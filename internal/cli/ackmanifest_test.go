package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/serve"
	"github.com/subaud-tools/galley/internal/versions"
)

// ONE PROTOCOL, TWO CARRIERS, ONE TEST. The CLI's --changes and the MCP
// galley_ack tool are two separate declarations of the same contract, and
// CLAUDE.md records what happens when only one of them is driven: the other
// goes on saying the old thing for as long as nothing looks. Both are driven
// here, against a real editor, and the round they land must be the same round.
func TestBothCarriersLandTheSameManifest(t *testing.T) {
	for _, carrier := range []struct {
		name string
		send func(t *testing.T, ch *channel, baseURL, doc string, changes []AckChange)
	}{
		{
			name: "cli",
			send: func(t *testing.T, _ *channel, baseURL, _ string, changes []AckChange) {
				t.Helper()
				if err := postAck(baseURL, "answered", "bounded the budget", changes); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mcp",
			send: func(t *testing.T, ch *channel, _, doc string, changes []AckChange) {
				t.Helper()
				args, err := json.Marshal(map[string]any{
					"doc": doc, "state": "answered", "note": "bounded the budget", "changes": changes,
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := ch.callTool("galley_ack", args); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(carrier.name, func(t *testing.T) {
			t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
			dir := t.TempDir()
			doc := writeDoc(t, dir, "doc.md", "# Title\n\nThe budget should stay explicit.\n")
			srv, err := serve.NewEdit(doc)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.Close() })
			srv.OnRevise = "true"
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			advertise(t, doc, ts.URL, srv.Room)

			// The reviewer asks, and presses.
			postJSON(t, ts.URL+"/_galley/instruct", map[string]any{
				"op": "comment", "target": "budget", "text": "bound it", "author": "court",
			})
			postJSON(t, ts.URL+"/_galley/revise", map[string]any{})
			store := versions.Open(doc)
			asked := list(t, store)
			if len(asked) != 2 || len(asked[1].Asks) != 1 {
				t.Fatalf("the press did not record one ask: %+v", asked)
			}
			key := asked[1].Asks[0].Key

			// The agent edits the file; the handoff watcher imports it.
			if err := os.WriteFile(doc, []byte("# Title\n\nThe budget of 12 attempts stays explicit.\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			waitForImport(t, doc, "12 attempts")

			carrier.send(t, newChannel(dir, ""), ts.URL, doc, []AckChange{
				{Quote: "of 12 attempts", Answers: []string{key}, Note: "Bounded the budget."},
				{Quote: "a sentence nobody wrote", Answers: []string{key}, Note: "Fabricated."},
			})

			rs := list(t, store)
			landed := rs[len(rs)-1]
			if landed.Reason != versions.ReasonLanded {
				t.Fatalf("the last round is %q, want %q: %+v", landed.Reason, versions.ReasonLanded, rs)
			}
			if len(landed.Changes) != 1 {
				t.Fatalf("%s landed %d changes, want 1: %+v", carrier.name, len(landed.Changes), landed.Changes)
			}
			got := landed.Changes[0]
			if got.Locator != "of 12 attempts" || got.Note != "Bounded the budget." ||
				len(got.Answers) != 1 || got.Answers[0] != key {
				t.Errorf("%s landed the wrong entry: %+v", carrier.name, got)
			}
		})
	}
}

// A MALFORMED MANIFEST IS THE CALLER'S TYPO, NOT THE SERVER'S JUDGEMENT, so it
// is refused here rather than posted as an ack carrying none of what was typed.
func TestChangesFlagRefusesWhatItCannotParse(t *testing.T) {
	if _, err := readChanges(`{"quote": "not an array"}`); err == nil {
		t.Error("a JSON object was accepted where an array is required")
	}
	if got, err := readChanges(""); err != nil || got != nil {
		t.Errorf("an absent --changes is not an error: %+v %v", got, err)
	}
	got, err := readChanges(`[{"quote":"a","answers":["k"],"note":"n"}]`)
	if err != nil || len(got) != 1 || got[0].Quote != "a" || got[0].Answers[0] != "k" || got[0].Note != "n" {
		t.Errorf("a well-formed manifest did not parse: %+v %v", got, err)
	}
}

// advertise writes the <doc>.serve.json a local tool finds a running editor
// through — the door FindRuntime opens, and so the door the MCP carrier takes.
func advertise(t *testing.T, doc, url, room string) {
	t.Helper()
	raw, err := json.Marshal(serve.Runtime{URL: url, Room: room, Page: doc, PID: os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serve.DefaultRuntimePath(doc), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, url string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s answered %s", url, resp.Status)
	}
}

func list(t *testing.T, s *versions.Store) []versions.Round {
	t.Helper()
	rs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// waitForImport waits for the handoff watcher's own tick to load the agent's
// save. There is no synchronous door to it from this package, and asserting on
// a round before the draft is in the document would be asserting on a race.
func waitForImport(t *testing.T, doc, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(doc)
		if err == nil && strings.Contains(string(raw), want) {
			// The file is the agent's while the window is open; give the
			// watcher a tick to have seen this content.
			time.Sleep(500 * time.Millisecond)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the agent's save never appeared in the document")
}
