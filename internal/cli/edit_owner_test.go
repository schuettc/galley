package cli

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
)

// THE OWNER IS AN ARGUMENT, NEVER THE ENVIRONMENT. Under pi, in-process
// subagents rewrote the shared AGENT_SESSION_ID, so an editor launched from
// the parent's shell was stamped with a child's id and the parent's channel
// refused to attach to it — measured 2026-09-08 (spec: session identity at
// spawn). `galley edit` now takes its owner from --owner only; the channel
// passes it, a human terminal passes nothing and gets an unowned editor.

func TestRequireLiveOwnerAcceptsNoOwner(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	if err := requireLiveOwner(""); err != nil {
		t.Fatalf("an unowned editor must not be refused: %v", err)
	}
}

func TestRequireLiveOwnerRefusesADeadOwner(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	err := requireLiveOwner("ghost")
	if err == nil {
		t.Fatal("an owner with no live channel was accepted")
	}
	if got, want := err.Error(), "owner session ghost has no live channel"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRequireLiveOwnerAcceptsALiveOwner(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	if err := requireLiveOwner("session-me"); err != nil {
		t.Fatalf("a live owner was refused: %v", err)
	}
}

// runEdit refuses BEFORE it listens or creates any document state, so the
// caller gets the error back and nothing is left on disk.
func TestEditRefusesADeadOwnerBeforeListening(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")
	err := runEdit([]string{"--owner", "ghost", "--no-open", doc}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runEdit served an editor for a dead owner")
	}
	if !strings.Contains(err.Error(), "owner session ghost has no live channel") {
		t.Fatalf("error = %q", err)
	}
	entries, _ := registry.List()
	if len(entries) != 0 {
		t.Fatalf("a refused editor still advertised: %+v", entries)
	}
}

func advertFor(t *testing.T, room string) registry.Entry {
	t.Helper()
	entries, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Room == room {
			return e
		}
	}
	t.Fatalf("no advert for room %s", room)
	return registry.Entry{}
}

func TestAdvertiseStampsTheExplicitOwner(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	advertiseEdit(srv, ts.URL, "session-explicit")
	if got := advertFor(t, srv.Room).Owner; got != "session-explicit" {
		t.Fatalf("owner = %q, want session-explicit", got)
	}
}

// The environment is NOT consulted, even when it names a session. This is the
// whole fix: a polluted variable in the shell can no longer stamp an editor.
func TestAdvertiseIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "env-claude")
	t.Setenv("AGENT_SESSION_ID", "env-agent")
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	advertiseEdit(srv, ts.URL, "")
	if got := advertFor(t, srv.Room).Owner; got != "" {
		t.Fatalf("owner = %q, want unowned — the environment stamped the advert", got)
	}
}
