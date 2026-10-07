package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
)

// THE FOUR WAYS A CHANNEL SILENTLY NEVER ATTACHES, and the one thing they all
// share: the reviewer waits, the agent waits, and neither is told. Everything
// in this file is about a channel that is not attached being ABLE TO SAY SO —
// the tests above prove the happy path, these prove the unhappy ones are
// audible.

// chanRig is one channel on pipes with a live MCP client behind it: handshake
// done, notifications drained into a queue, tool calls answered. The tests in
// channel_test.go each open-code this; the ones here need it five times over
// and one shape is what keeps them readable.
type chanRig struct {
	t     *testing.T
	ch    *channel
	in    *io.PipeWriter
	notes chan map[string]any
	sc    *bufio.Scanner
	id    int
	reply chan map[string]any
}

func newRig(t *testing.T, scope, self string, scanEvery time.Duration) *chanRig {
	t.Helper()
	clientOut, serverIn := io.Pipe()
	serverOut, clientIn := io.Pipe()
	ch := newChannel(scope, self)
	ch.scanEvery = scanEvery
	go func() { _ = ch.run(clientOut, clientIn) }()
	t.Cleanup(func() { _ = serverIn.Close() })

	r := &chanRig{
		t: t, ch: ch, in: serverIn,
		notes: make(chan map[string]any, 8),
		reply: make(chan map[string]any, 8),
	}
	r.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	r.id = 1
	r.sc = bufio.NewScanner(serverOut)
	r.sc.Buffer(make([]byte, 1<<20), 1<<24)
	if !r.sc.Scan() {
		t.Fatal("no handshake reply")
	}
	go func() {
		for r.sc.Scan() {
			var m map[string]any
			if json.Unmarshal(r.sc.Bytes(), &m) != nil {
				continue
			}
			if m["method"] == "notifications/claude/channel" {
				params, _ := m["params"].(map[string]any)
				meta, _ := params["meta"].(map[string]any)
				if meta == nil {
					meta = map[string]any{}
				}
				meta["content"], _ = params["content"].(string)
				r.notes <- meta
				continue
			}
			if _, ok := m["result"]; ok {
				r.reply <- m
			}
		}
	}()
	return r
}

func (r *chanRig) send(line string) {
	r.t.Helper()
	if _, err := io.WriteString(r.in, line+"\n"); err != nil {
		r.t.Fatal(err)
	}
}

// status calls the diagnostic tool and returns its text.
func (r *chanRig) status() string {
	r.t.Helper()
	r.id++
	r.send(`{"jsonrpc":"2.0","id":` + strconv.Itoa(r.id) +
		`,"method":"tools/call","params":{"name":"galley_channel_status","arguments":{}}}`)
	select {
	case m := <-r.reply:
		res, _ := m["result"].(map[string]any)
		content, _ := res["content"].([]any)
		if len(content) == 0 {
			r.t.Fatalf("empty tool result: %v", m)
		}
		first, _ := content[0].(map[string]any)
		text, _ := first["text"].(string)
		if bad, _ := res["isError"].(bool); bad {
			r.t.Fatalf("galley_channel_status failed: %s", text)
		}
		return text
	case <-time.After(5 * time.Second):
		r.t.Fatal("galley_channel_status never answered")
		return ""
	}
}

func (r *chanRig) note(when string) map[string]any {
	r.t.Helper()
	select {
	case m := <-r.notes:
		return m
	case <-time.After(5 * time.Second):
		r.t.Fatalf("%s: no notification arrived", when)
		return nil
	}
}

func (r *chanRig) silence(d time.Duration) {
	r.t.Helper()
	select {
	case m := <-r.notes:
		r.t.Fatalf("unexpected notification: %v", m)
	case <-time.After(d):
	}
}

// liveEditor starts a real edit server on a real listener and advertises it,
// which is what `galley edit` does minus the process.
func liveEditor(t *testing.T, doc string, e registry.Entry) (*serve.EditServer, *httptest.Server, registry.Entry) {
	t.Helper()
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	e.URL, e.Room, e.Page = ts.URL, srv.Room, doc
	if e.PID == 0 {
		e.PID = os.Getpid()
	}
	if err := registry.Write(e); err != nil {
		t.Fatal(err)
	}
	return srv, ts, e
}

func press(t *testing.T, ts *httptest.Server) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/_galley/revise", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// DEFECT 1 (REVISED). COURT RESTARTS CLAUDE CODE WITH THE REVIEW TAB STILL OPEN.
//
// The editor was started by a session that no longer exists — `--continue`
// draws a fresh id — so its advert is stamped with a session nothing on this
// machine is running. The first fix made that orphan ADOPTABLE by any channel;
// it cured the silence and caused a worse bug. With every session sharing one
// whole-workspace scope, a Revise meant for the agent you were working with was
// answered by whichever unrelated session adopted the orphan first — a misroute
// measured live, repeatedly.
//
// The rule this asserts NOW: an advert owned by ANOTHER session — gone or not —
// is never adopted. A bystander stays off it and says why; the reviewer reopens
// the document, which re-stamps it, rather than have a stranger answer for a
// session that is gone. The editor itself is bound to its session's life and
// shuts down when that session ends (TestWatchSessionStopsTheEditor), so the
// orphan it would leave is short-lived.
func TestChannelDoesNotAdoptAnOrphanedDocument(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, ts, _ := liveEditor(t, doc, registry.Entry{Owner: "session-A-restarted-away"})

	r := newRig(t, dir, "session-B-here", 25*time.Millisecond)
	// Several scans: an adopt would have attached by now.
	r.silence(300 * time.Millisecond)
	if n := srv.Waiting(); n != 0 {
		t.Fatalf("Waiting() = %d — a bystander session adopted an orphaned editor", n)
	}
	// A Revise reaches no one here, and the channel says why rather than
	// answering for a session that is gone.
	press(t, ts)
	r.silence(300 * time.Millisecond)
	if s := r.status(); !strings.Contains(s, "no longer running") {
		t.Errorf("status does not explain the declined orphan:\n%s", s)
	}
}

// The other half of the rule — a LIVE session keeps its documents —
// is TestChannelIgnoresAnotherSessionsDocument in channel_test.go, which is
// where the design's ownership cases already live, and the diagnostic for it
// is the "owned by another live session" case at the bottom of this file.

// DEFECT 2. /tmp IS A SYMLINK TO /private/tmp ON macOS, and Court's own demo
// harness lives at a path with both spellings. Scope was compared as a raw
// string prefix, so the same directory under two names attached to nothing.
// Both directions, because both are one `cd` away from each other.
func TestChannelAttachesThroughASymlinkedPath(t *testing.T) {
	for _, tc := range []struct{ name, which string }{
		{"scope is the symlinked spelling", "scope"},
		{"the document is the symlinked spelling", "page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
			real := t.TempDir()
			link := filepath.Join(t.TempDir(), "work")
			if err := os.Symlink(real, link); err != nil {
				t.Skipf("this platform cannot make a symlink: %v", err)
			}
			docDir, scope := real, link
			if tc.which == "page" {
				docDir, scope = link, real
			}
			doc := writeDoc(t, docDir, "doc.md", "# T\n\nHello.\n")
			srv, ts, _ := liveEditor(t, doc, registry.Entry{})

			r := newRig(t, scope, "", 25*time.Millisecond)
			until(t, "the attach across the symlink", func() bool { return srv.Waiting() > 0 })
			press(t, ts)
			if got := r.note("the press")["reason"]; got != "revise" {
				t.Fatalf("wake reason = %v, want revise", got)
			}
		})
	}
}

// DEFECT 3. THE AGENT CANNOT TELL "STILL READING" FROM "GONE" — and one of
// them means stop waiting. The editor releasing its waiters mid-poll is what
// a terminated `galley edit` looks like from here: `galley wait` prints
// {"reason":"closed"} and the channel used to return in silence.
func TestChannelSaysWhenTheEditorIsGone(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, _, _ := liveEditor(t, doc, registry.Entry{})

	r := newRig(t, dir, "", time.Hour) // one scan; no re-attach to muddy it
	until(t, "the attach", func() bool { return srv.Waiting() > 0 })

	srv.ReleaseWaiters() // the editor stopping, from the poll's point of view

	m := r.note("the editor going away")
	if m["reason"] != reasonClosed {
		t.Fatalf("reason = %v, want %q", m["reason"], reasonClosed)
	}
	content, _ := m["content"].(string)
	if !strings.Contains(content, "GONE") || !strings.Contains(content, doc) {
		t.Errorf("the sentence does not say the editor is gone: %q", content)
	}
	if m["doc"] != doc {
		t.Errorf("meta.doc = %v, want %s", m["doc"], doc)
	}
}

// AND IT SAYS IT ONCE. The advert OUTLIVES the editor by seconds on an orderly
// shutdown — `galley edit` releases its waiters before it shuts the listener
// down and only withdraws the advert after the final flush — and a server that
// has released its waiters answers "closed" to every new poll forever. A scan
// loop that re-attached into that window would announce the same lost review
// once every two seconds, for as long as the advert stayed up.
func TestChannelSaysTheEditorIsGoneOnlyOnce(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, _, _ := liveEditor(t, doc, registry.Entry{})

	// A REAL scan interval: the re-attach is the thing under test.
	r := newRig(t, dir, "", 25*time.Millisecond)
	until(t, "the attach", func() bool { return srv.Waiting() > 0 })
	srv.ReleaseWaiters()
	if got := r.note("the editor going away")["reason"]; got != reasonClosed {
		t.Fatalf("reason = %v, want %q", got, reasonClosed)
	}
	// The advert is deliberately left in place, exactly as a shutting-down
	// editor leaves it.
	r.silence(500 * time.Millisecond)
}

// DEFECT 4. ONE EDITOR, TWO ADVERTS, EVERY WAKE DOUBLED. Measured from a
// planted entry and from two same-session channels alike: the channel attached
// once per ENTRY, and one Revise press became two notifications — two rounds of
// work proposed against one ask.
func TestChannelWakesOnceForADuplicatedAdvert(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, ts, e := liveEditor(t, doc, registry.Entry{})
	dup := e
	dup.Room = e.Room + "-duplicate"
	if err := registry.Write(dup); err != nil {
		t.Fatal(err)
	}

	r := newRig(t, dir, "", 25*time.Millisecond)
	until(t, "the attach", func() bool { return srv.Waiting() > 0 })
	time.Sleep(200 * time.Millisecond) // several scans: a second attach would have landed
	if n := srv.Waiting(); n != 1 {
		t.Fatalf("Waiting() = %d — the duplicate advert bought a second attach", n)
	}

	press(t, ts)
	if got := r.note("the press")["reason"]; got != "revise" {
		t.Fatalf("wake reason = %v, want revise", got)
	}
	r.silence(400 * time.Millisecond) // the second copy of the same wake
}

// DEFECT 4b. A RECYCLED PID KEEPS A LIE ALIVE. The advert's PID answers to
// signal 0 because the operating system handed it to something else, so
// registry.List reports an editor that is not there. Nothing reaps it and
// nothing says anything: the channel attaches to a URL that answers nothing,
// forever, believing it is attached.
func TestChannelReapsAnAdvertNothingAnswers(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	// A live PID that is not an editor (this test process) and a URL that
	// nothing is listening on: the recycled-PID advert, exactly.
	// Owned by THIS session (a bystander would now decline it, never probe it):
	// the reap-on-unreachable path is for an editor the channel legitimately
	// attaches to and finds answering nothing.
	stale := registry.Entry{
		URL: "http://127.0.0.1:1", Room: "recycled", Page: doc, PID: os.Getpid(),
		Owner: "session-now",
	}
	if err := registry.Write(stale); err != nil {
		t.Fatal(err)
	}

	r := newRig(t, dir, "session-now", 25*time.Millisecond)
	until(t, "the stale advert to be reaped", func() bool {
		entries, err := registry.List()
		return err == nil && len(entries) == 0
	})
	// The reap and the status's reason are two steps in the channel; on a
	// slow runner a single read could land between them (galley #13).
	until(t, "the status to report the unreachable advert", func() bool {
		return strings.Contains(r.status(), "unreachable")
	})
	// AND IT IS NOT AN EDITOR GOING AWAY. Nothing was ever attached, so the
	// "the review is gone" wake would be a review that never existed.
	r.silence(300 * time.Millisecond)
}

// THE UNIFYING REQUIREMENT. A channel that finds nothing must be able to say
// WHICH nothing: no document open at all, a document open that another live
// session owns, or a document open outside this channel's root. The last two
// are exactly the states that end with a reviewer pressing Revise into the
// void, and until now all three looked identical from inside the session.
func TestChannelStatusTellsTheThreeSilencesApart(t *testing.T) {
	t.Run("no document is open", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		r := newRig(t, t.TempDir(), "me", 25*time.Millisecond)
		time.Sleep(150 * time.Millisecond)
		s := r.status()
		if !strings.Contains(s, "no live editor") {
			t.Errorf("status does not say the registry is empty:\n%s", s)
		}
	})

	t.Run("owned by another live session", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		dir := t.TempDir()
		doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
		liveEditor(t, doc, registry.Entry{Owner: "other-session"})
		if err := registry.AnnounceSession("other-session"); err != nil {
			t.Fatal(err)
		}
		r := newRig(t, dir, "me", 25*time.Millisecond)
		time.Sleep(200 * time.Millisecond)
		s := r.status()
		if !strings.Contains(s, "owned by session other-session") {
			t.Errorf("status does not name the owning session:\n%s", s)
		}
		if !strings.Contains(s, doc) {
			t.Errorf("status does not name the document:\n%s", s)
		}
	})

	t.Run("open outside my scope", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		elsewhere := t.TempDir()
		doc := writeDoc(t, elsewhere, "doc.md", "# T\n\nHello.\n")
		liveEditor(t, doc, registry.Entry{})
		scope := t.TempDir()
		r := newRig(t, scope, "me", 25*time.Millisecond)
		time.Sleep(200 * time.Millisecond)
		s := r.status()
		if !strings.Contains(s, "outside this channel's scope") || !strings.Contains(s, scope) {
			t.Errorf("status does not report the scope mismatch (scope %s):\n%s", scope, s)
		}
	})
}

// OWNERSHIP DECIDES FIRST; THE SCOPE ONLY GOVERNS EDITORS NOBODY OWNS (#68).
// An editor this session owns attaches wherever its file is. Everything else
// outside the scope is left alone exactly as before: an unowned one is neither
// attached nor claimed, another live session's stays theirs, and a channel
// with no session id owns nothing.
func TestChannelAttachesItsOwnEditorOutsideItsScope(t *testing.T) {
	t.Run("owned by this session", func(t *testing.T) {
		t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
		doc := writeDoc(t, t.TempDir(), "doc.md", "# T\n\nHello.\n")
		srv, ts, _ := liveEditor(t, doc, registry.Entry{Owner: "me"})
		r := newRig(t, t.TempDir(), "me", 25*time.Millisecond)
		until(t, "the attach outside the scope", func() bool { return srv.Waiting() > 0 })
		s := r.status()
		if !strings.Contains(s, "attached (1)") || !strings.Contains(s, doc) {
			t.Errorf("status does not list the owned editor as attached:\n%s", s)
		}
		press(t, ts)
		if got := r.note("the press")["reason"]; got != "revise" {
			t.Fatalf("wake reason = %v, want revise", got)
		}
	})

	for _, tc := range []struct{ name, self, owner string }{
		{"unowned", "me", ""},
		{"owned by another live session", "me", "other-session"},
		{"no session id", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
			if tc.owner != "" {
				if err := registry.AnnounceSession(tc.owner); err != nil {
					t.Fatal(err)
				}
			}
			doc := writeDoc(t, t.TempDir(), "doc.md", "# T\n\nHello.\n")
			srv, _, e := liveEditor(t, doc, registry.Entry{Owner: tc.owner})
			scope := t.TempDir()
			r := newRig(t, scope, tc.self, 25*time.Millisecond)
			time.Sleep(200 * time.Millisecond)
			if n := srv.Waiting(); n != 0 {
				t.Fatalf("Waiting() = %d — attached outside the scope", n)
			}
			s := r.status()
			if !strings.Contains(s, "outside this channel's scope") || !strings.Contains(s, scope) {
				t.Errorf("status does not report the scope mismatch (scope %s):\n%s", scope, s)
			}
			entries, _, err := registry.Inspect()
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range entries {
				if got.Room == e.Room && got.Owner != tc.owner {
					t.Fatalf("owner = %q, want %q — the advert was claimed", got.Owner, tc.owner)
				}
			}
		})
	}
}

// THE EDITOR HALF OF OPTION A. An editor is launched detached, so nothing but
// this takes it down when the session that opened it ends — and an editor that
// outlives its session is exactly the orphan the channel now refuses to adopt.
// While the session's presence is live the editor keeps serving; once it is
// gone for the debounced count of polls, the editor runs the same orderly stop
// Ctrl-C does.
func TestWatchSessionStopsTheEditor(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	owner := "session-owning-this-editor"
	if err := registry.AnnounceSession(owner); err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchSession(ctx, owner, 15*time.Millisecond, 3, sync.OnceFunc(func() { close(stopped) }))

	// While the session is live, the editor must keep serving.
	select {
	case <-stopped:
		t.Fatal("the editor stopped while its session was still live")
	case <-time.After(200 * time.Millisecond):
	}

	// The session ends — its presence goes — and the editor takes itself down.
	if err := registry.WithdrawSession(owner); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the editor did not stop after its session ended")
	}
}
