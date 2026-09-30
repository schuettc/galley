package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
)

func advertOwner(t *testing.T, room string) string {
	t.Helper()
	got, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Room == room {
			return e.Owner
		}
	}
	t.Fatalf("advert %s vanished", room)
	return ""
}

// TestClaimOnAttachGivesOneListener is the fix stated as a test: an UNOWNED
// editor under two channels' overlapping scopes must reach exactly one of them.
// The first channel to scan claims it (stamps its session id); the second then
// reads a live owner and declines, so a Revise wakes one session and not both.
func TestClaimOnAttachGivesOneListener(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")

	// A real editor, so the attach long-poll connects and the advert is not
	// reaped as unreachable — the claim happens on that same first attach.
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// UNOWNED advert under both channels' root (they share `dir` here; in
	// production it is a parent and a child directory — the nested-scope case).
	if err := registry.Write(registry.Entry{
		URL: ts.URL, Room: srv.Room, Page: doc, PID: os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stop the attach long-poll scanOnce launches

	// First channel is present (announces its session) and scans: it claims.
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	a := newChannel(dir, "session-me")
	a.scanOnce(ctx)
	if o := advertOwner(t, srv.Room); o != "session-me" {
		t.Fatalf("first channel did not claim the unowned advert: owner=%q, want session-me", o)
	}

	// A second channel under the same root must NOT re-claim it — that is the
	// broadcast this fix removes.
	b := newChannel(dir, "session-other")
	b.scanOnce(ctx)
	if o := advertOwner(t, srv.Room); o != "session-me" {
		t.Fatalf("second channel stole a live-owned advert: owner=%q", o)
	}
	b.mu.Lock()
	attached := b.attached[srv.Room]
	reason := b.unattached[srv.Room]
	b.mu.Unlock()
	if attached {
		t.Fatal("second channel attached to an advert another live session owns")
	}
	if reason == "" {
		t.Fatal("second channel recorded no reason for declining")
	}
}

// A DOCUMENT ANOTHER LIVE SESSION OWNS IS NOT THIS SESSION'S NEWS. The
// refusal is the ownership rule working, and there is nothing here to act on —
// but the harness turns channel stderr into a notification, so saying it made
// every session under an overlapping scope announce its neighbours' reviews,
// which reads exactly like the misroute the rule prevents. It stays in
// status(); it stays off stderr. A DEAD owner is different — that review is
// stranded, the line tells someone to reopen it — so that one is still said.
func TestChannelKeepsALiveOwnersDocumentOffStderr(t *testing.T) {
	announced := func(c *channel, room string) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k := range c.announced {
			if len(k) >= len(room) && k[:len(room)] == room {
				return true
			}
		}
		return false
	}

	t.Run("live owner: in status, not said", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		dir := t.TempDir()
		doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")
		srv, _, _ := liveEditor(t, doc, registry.Entry{Owner: "other-session"})
		if err := registry.AnnounceSession("other-session"); err != nil {
			t.Fatal(err)
		}
		c := newChannel(dir, "me")
		c.scanOnce(context.Background())
		c.mu.Lock()
		reason := c.unattached[srv.Room]
		c.mu.Unlock()
		if reason == "" {
			t.Fatal("status lost the reason the document was not attached")
		}
		if announced(c, srv.Room) {
			t.Errorf("said a live session's document on stderr: %q", reason)
		}
	})

	t.Run("dead owner: still said", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		dir := t.TempDir()
		doc := writeDoc(t, dir, "doc.md", "# T\n\nHi.\n")
		srv, _, _ := liveEditor(t, doc, registry.Entry{Owner: "gone-session"})
		c := newChannel(dir, "me")
		c.scanOnce(context.Background())
		if !announced(c, srv.Room) {
			t.Error("a stranded review (dead owner) was not said")
		}
	})
}
