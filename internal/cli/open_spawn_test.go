package cli

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/registry"
	"github.com/subaud-tools/galley/internal/serve"
)

// buildGalley compiles the real binary once per test that needs it. The
// advert is written before the first request is served, and this test only
// reads the advert and probes the listener.
func buildGalley(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "galley")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", exe, "../../cmd/galley")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("building galley: %v", err)
	}
	return exe
}

// script writes an executable shell script standing in for the binary, for
// the two failure shapes a real editor is too slow or too healthy to produce.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-galley")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func killEditor(t *testing.T, room string) {
	t.Helper()
	entries, _ := registry.List()
	for _, e := range entries {
		if e.Room == room {
			_ = syscall.Kill(e.PID, syscall.SIGTERM)
		}
	}
	until(t, "the editor to withdraw its advert", func() bool {
		entries, _ := registry.List()
		for _, e := range entries {
			if e.Room == room {
				return false
			}
		}
		return true
	})
}

func TestOpenSpawnsAnEditorOwnedByThisSession(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	c := newChannel(dir, "session-me")
	c.exe = buildGalley(t)
	opened := recordOpens(c)
	c.openPoll = 20 * time.Millisecond

	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatalf("galley_open: %v", err)
	}
	t.Cleanup(func() { killEditor(t, r.Room) })
	// The spawned editor opens its own page; the channel opening it too
	// would put two tabs in front of the reviewer.
	if len(*opened) != 0 {
		t.Fatalf("the channel opened %v on the spawn path; the editor does that", *opened)
	}

	if r.Page != doc || r.URL == "" || r.Room == "" {
		t.Fatalf("incomplete result %+v", r)
	}
	if got := advertOwner(t, r.Room); got != "session-me" {
		t.Fatalf("spawned editor's owner = %q, want session-me", got)
	}
	resp, err := http.Get(r.URL + "/_galley/pending")
	if err != nil {
		t.Fatalf("the URL does not answer: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s/_galley/pending = %d", r.URL, resp.StatusCode)
	}

	// Idempotent: a second open returns the same editor, no second spawn.
	again, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatal(err)
	}
	// THE RUNTIME FILE NAMES THE OWNER, so an approved review — whose registry
	// entry is gone — can still be recognised as this session's.
	if rt, ok := serve.FindRuntime(doc); !ok || rt.Owner != "session-me" {
		t.Fatalf("runtime file %+v (found %v), want owner session-me", rt, ok)
	}
	if again.Room != r.Room {
		t.Fatalf("second open started a second editor: %s then %s", r.Room, again.Room)
	}
	if got := *opened; len(got) != 1 || got[0] != r.URL {
		t.Fatalf("second open opened %v, want exactly [%s]", got, r.URL)
	}

	logDir, _ := registry.LogDir()
	if _, err := os.Stat(filepath.Join(logDir, registry.Token(doc)+".log")); err != nil {
		t.Fatalf("no editor log written: %v", err)
	}
}

// A REVIEW THAT HAS ENDED IS NOT HANDED BACK. After an approve the old
// editor serves a sealed page; galley_open stops it and starts a fresh review.
func TestOpenStartsAFreshReviewWhenTheLastOneEnded(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	c := newChannel(dir, "session-me")
	c.exe = buildGalley(t)
	recordOpens(c)
	c.openPoll = 20 * time.Millisecond

	first, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatalf("galley_open: %v", err)
	}
	if err := postApprove(first.URL); err != nil {
		t.Fatalf("approve: %v", err)
	}
	second, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatalf("galley_open after approve: %v", err)
	}
	t.Cleanup(func() { killEditor(t, second.Room) })
	// The room, not the URL: a fresh editor reclaims the last port on purpose.
	if second.Room == first.Room {
		t.Fatalf("handed back the ended review: %+v then %+v", first, second)
	}
	for _, e := range mustList(t) {
		if e.Room == first.Room {
			t.Fatalf("the ended editor is still advertised: %+v", e)
		}
	}
	st, err := reviseState(second.URL)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sealed {
		t.Fatal("the fresh review is already sealed")
	}
}

func mustList(t *testing.T) []registry.Entry {
	t.Helper()
	es, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// THE EDITOR galley_open STARTS OPENS THE PAGE ITSELF: it is started without
// --no-open, so `galley edit` opens the browser as it does from a terminal.
func TestOpenStartsTheEditorWithoutNoOpen(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	argsFile := filepath.Join(t.TempDir(), "args")
	c := newChannel(dir, "session-me")
	c.exe = script(t, "echo \"$@\" > '"+argsFile+"'\nexit 1\n")
	c.openPoll = 10 * time.Millisecond

	_, _ = openDoc(t, c, doc)
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the editor was not started: %v", err)
	}
	if want := "edit " + doc + " --owner session-me\n"; string(got) != want {
		t.Fatalf("editor args = %q, want %q", got, want)
	}
}

func TestOpenReportsAnEditorThatExitsBeforeAdvertising(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	c := newChannel(dir, "session-me")
	c.exe = script(t, "echo 'boom: no licence' >&2\nexit 3\n")
	c.openPoll = 10 * time.Millisecond

	_, err := openDoc(t, c, doc)
	if err == nil {
		t.Fatal("a dead editor was reported as open")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit status 3") || !strings.Contains(msg, "boom: no licence") {
		t.Fatalf("error carries neither the exit status nor the log tail:\n%s", msg)
	}
}

func TestOpenReportsAnEditorThatNeverAdvertises(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	c := newChannel(dir, "session-me")
	c.exe = script(t, "sleep 2\n")
	c.openPoll = 10 * time.Millisecond
	c.openTimeout = 150 * time.Millisecond

	_, err := openDoc(t, c, doc)
	if err == nil {
		t.Fatal("a silent editor was reported as open")
	}
	logDir, _ := registry.LogDir()
	if !strings.Contains(err.Error(), filepath.Join(logDir, registry.Token(doc)+".log")) {
		t.Fatalf("timeout error does not name the log: %q", err)
	}
}

func TestOpenRefusesWhenTheBinaryIsUnknown(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	c := newChannel(dir, "session-me")
	c.exe = ""
	if _, err := openDoc(t, c, doc); err == nil || !strings.Contains(err.Error(), "galley binary") {
		t.Fatalf("expected a binary-location error, got %v", err)
	}
}

func TestTailLinesReturnsTheLastN(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tailLines(p, 2); got != "c\nd" {
		t.Fatalf("tailLines = %q, want %q", got, "c\nd")
	}
	if got := tailLines(p, 10); got != "a\nb\nc\nd" {
		t.Fatalf("tailLines over-long = %q", got)
	}
	if got := tailLines(filepath.Join(t.TempDir(), "missing"), 2); got != "" {
		t.Fatalf("tailLines on a missing file = %q, want empty", got)
	}
}

// AN HTML PAGE IS OPENED BY THE PATH THE EDITOR ADVERTISES, NOT THE PATH IT
// WAS GIVEN. `galley edit page.html` serves the prose it extracts, so the
// advert names .galley/pages/<base>/content.md; matching adverts on the input
// path meant galley_open never recognised the editor it had just spawned and
// timed out on every HTML page. This drives the real binary end to end.
func TestOpenAnHTMLPageResolvesTheAdvertisedContentFile(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	page := filepath.Join(dir, "post.html")
	if err := os.WriteFile(page, []byte(minimalHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	c := newChannel(dir, "session-me")
	c.exe = buildGalley(t)
	c.openPoll = 20 * time.Millisecond

	r, err := openDoc(t, c, page)
	if err != nil {
		t.Fatalf("galley_open on an HTML page: %v", err)
	}
	t.Cleanup(func() { killEditor(t, r.Room) })

	if r.URL == "" || r.Room == "" {
		t.Fatalf("incomplete result %+v", r)
	}
	if !strings.HasSuffix(r.Page, string(filepath.Separator)+"content.md") {
		t.Fatalf("Page = %q, want the advertised content.md the editor serves", r.Page)
	}
	if got := advertOwner(t, r.Room); got != "session-me" {
		t.Fatalf("spawned editor's owner = %q, want session-me", got)
	}

	// And it resolves rather than spawning a second editor next time.
	again, err := openDoc(t, c, page)
	if err != nil {
		t.Fatal(err)
	}
	if again.Room != r.Room {
		t.Fatalf("second open started a second editor: %s then %s", r.Room, again.Room)
	}
}
