package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schuettc/galley/internal/serve"
)

// Bug 6: a range comment's words lived only in the running editor, so
// `galley pending` after the editor stopped did not list it at all.
func TestOfflinePendingListsARangeInstructionAfterTheEditorStops(t *testing.T) {
	doc := writeDoc(t, t.TempDir(), "d.md", "# T\n\nCognito mints every token.\n")
	s, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	raw, err := json.Marshal(map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": "name the issuer",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/_galley/instruct", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("instruct answered %d", resp.StatusCode)
	}
	ts.Close()
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	view, err := offlinePending(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 1 {
		t.Fatalf("offline pending lists %d instructions after the editor stopped, want 1: %+v",
			len(view.Instructions), view.Instructions)
	}
	got := view.Instructions[0]
	if got.Key == "" || got.Quote != "Cognito" || got.Text != "name the issuer" {
		t.Errorf("offline pending = %+v, want key, quote \"Cognito\" and the words", got)
	}
}
