package cli

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/serve"
)

// galley_approve IS THE PAGE'S APPROVE BUTTON, CALLED FROM THE CONVERSATION.
// It must leave the editor exactly where a press leaves it: sealed. A second
// call meets the seal like a second press would, and says so.
func TestGalleyApproveSealsTheReview(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# Title\n\nThe budget should stay explicit.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	advertise(t, doc, ts.URL, srv.Room)

	ch := newChannel(dir, "")
	args, _ := json.Marshal(map[string]any{"doc": doc})
	if _, err := ch.callTool("galley_approve", args); err != nil {
		t.Fatalf("galley_approve: %v", err)
	}
	if !srv.Sealed() {
		t.Fatal("galley_approve returned without sealing the review")
	}
	if _, err := ch.callTool("galley_approve", args); err == nil || !strings.Contains(err.Error(), "sealed") {
		t.Errorf("a second approve was not refused as sealed: %v", err)
	}
}

func TestGalleyApproveWithNoEditorIsAnError(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	args, _ := json.Marshal(map[string]any{"doc": filepath.Join(dir, "nobody.md")})
	if _, err := newChannel(dir, "").callTool("galley_approve", args); err == nil ||
		!strings.Contains(err.Error(), "no running editor") {
		t.Errorf("approve with no editor did not say so: %v", err)
	}
}
