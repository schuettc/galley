package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/versions"
)

// The round that motivated the whole design, end to end through the real
// endpoints: "bold Galley" is an inline formatting change no operation-specific
// apply could express, and the file-edit handoff expresses it as ordinary
// Markdown. Between the press and the ack the document is the agent's — the
// reviewer's endpoints refuse — and the return lands as one agent round
// answering the round that asked.
func TestTheBoldGalleyRoundLandsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nA paragraph about Galley.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "Galley", "bold this")
	press(t, s, "{}")
	asked := rounds(t, s)[len(rounds(t, s))-1]

	// The press handed the file over: the lock is on the wire and the door.
	req := httptest.NewRequest(http.MethodGet, "/_galley/revise", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var state struct {
		Handoff bool `json:"handoff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil || !state.Handoff {
		t.Fatalf("the press did not open the handoff: %s", rec.Body.String())
	}
	if rec := postRec(t, s, "/_galley/instruct",
		map[string]any{"op": "comment", "target": "Galley", "text": "another"}); rec.Code != http.StatusConflict {
		t.Fatalf("instruct mid-window answered %d, want 409", rec.Code)
	}

	// The agent's revision is the file, in ordinary Markdown.
	agentEdits(t, s, "# Title\n\nA paragraph about **Galley**.\n")
	ackAs(t, s, "answered", "bolded the product name")

	rs := rounds(t, s)
	landed := rs[len(rs)-1]
	if landed.Reason != versions.ReasonLanded || landed.Answers != asked.N {
		t.Fatalf("landed round = %+v, want landed answering v%d", landed, asked.N)
	}
	if versions.Authors(landed.Authors) != versions.AuthorAgent {
		t.Fatalf("landed round authors = %q", versions.Authors(landed.Authors))
	}
	if changed := s.changedRegions(asked.N, landed.N); changed != 1 {
		t.Errorf("the formatting round reports %d changed regions, want 1", changed)
	}
	onDisk, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "**Galley**") {
		t.Fatalf("the canonical file lost the bold: %s", onDisk)
	}
	// And the document is the reviewer's again.
	if s.handoffOpenNow() {
		t.Fatal("the window outlived the answer")
	}
}
