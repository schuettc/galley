package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
)

// galley_open IS HOW A SESSION PUTS A DOCUMENT UNDER REVIEW. The channel is
// the session's presence inside galley, so it is the one thing that knows,
// with certainty, which session an editor should belong to. These tests cover
// the resolution order for a document that is ALREADY open; open_spawn_test.go
// covers the spawn.

// plantAdvert writes an advert for page whose URL is a stand-in editor that
// answers GET /_galley/revise with a review still open, which is all
// galley_open asks of an editor it already has.
func plantAdvert(t *testing.T, page, owner string) registry.Entry {
	t.Helper()
	ed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_galley/revise" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"sealed":false}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ed.Close)
	return plantAdvertAt(t, page, owner, ed.URL, os.Getpid())
}

func plantAdvertAt(t *testing.T, page, owner, url string, pid int) registry.Entry {
	t.Helper()
	e := registry.Entry{
		URL: url, Room: "room-" + registry.Token(page)[:8],
		Page: page, PID: pid, Owner: owner,
	}
	if err := registry.Write(e); err != nil {
		t.Fatal(err)
	}
	return e
}

func openDoc(t *testing.T, c *channel, doc string) (openResult, error) {
	t.Helper()
	text, err := c.callTool("galley_open", json.RawMessage(`{"doc":`+strconvQuote(doc)+`}`))
	if err != nil {
		return openResult{}, err
	}
	var r openResult
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("galley_open returned non-JSON %q: %v", text, err)
	}
	return r, nil
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestOpenReturnsTheEditorThisSessionAlreadyOwns(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "session-me")

	c := newChannel(dir, "session-me")
	opened := recordOpens(c)
	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != e.URL || r.Room != e.Room || r.Page != doc {
		t.Fatalf("got %+v, want the existing advert %+v", r, e)
	}
	// THE PAGE IS OPENED EVERY TIME: a second galley_open on a review still
	// running brings its page back, rather than leaving only a link.
	if got := *opened; len(got) != 1 || got[0] != e.URL {
		t.Fatalf("browser opened %v, want exactly [%s]", got, e.URL)
	}
}

// recordOpens swaps the channel's browser opener for one that records the
// URLs it was asked to open, so no test opens a real tab.
func recordOpens(c *channel) *[]string {
	var opened []string
	c.openPage = func(url string) { opened = append(opened, url) }
	return &opened
}

// AN EDITOR THAT DOES NOT ANSWER IS SHUTTING DOWN, and handing back its URL
// gives the reviewer a dead page. Its entry is waited out and a fresh editor
// takes its place.
func TestOpenWaitsOutAnEditorThatDoesNotAnswer(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	if err := registry.AnnounceSession("session-me"); err != nil {
		t.Fatal(err)
	}
	dying := exec.Command("sleep", "30")
	if err := dying.Start(); err != nil {
		t.Fatal(err)
	}
	old := plantAdvertAt(t, doc, "session-me", "http://127.0.0.1:1", dying.Process.Pid)

	c := newChannel(dir, "session-me")
	c.exe = buildGalley(t)
	recordOpens(c)
	c.openPoll = 20 * time.Millisecond
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = dying.Process.Kill()
		_ = dying.Wait()
	}()
	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatalf("galley_open: %v", err)
	}
	t.Cleanup(func() { killEditor(t, r.Room) })
	if r.Room == old.Room || r.URL == old.URL {
		t.Fatalf("handed back the dying editor: %+v", r)
	}
}

func TestOpenReportsAnEditorThatNeitherAnswersNorExits(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	plantAdvertAt(t, doc, "session-me", "http://127.0.0.1:1", os.Getpid())

	c := newChannel(dir, "session-me")
	recordOpens(c)
	c.openPoll = 10 * time.Millisecond
	c.shutdownWait = 100 * time.Millisecond
	_, err := openDoc(t, c, doc)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("pid %d", os.Getpid())) {
		t.Fatalf("expected an error naming pid %d, got %v", os.Getpid(), err)
	}
}

// AN ENDED REVIEW IN AN EDITOR STARTED FROM A TERMINAL cannot be stopped from
// here: it has no stop hook, so the reviewer is told to stop it.
func TestOpenRefusesAnEndedReviewItCannotStop(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	ed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_galley/revise":
			_, _ = w.Write([]byte(`{"sealed":true}`))
		case "/_galley/stop":
			http.Error(w, "this server was started without a stop hook", http.StatusNotImplemented)
		}
	}))
	t.Cleanup(ed.Close)
	plantAdvertAt(t, doc, "", ed.URL, os.Getpid())

	c := newChannel(dir, "session-me")
	opened := recordOpens(c)
	_, err := openDoc(t, c, doc)
	if err == nil || !strings.Contains(err.Error(), "review there has ended") {
		t.Fatalf("expected an ended-review error, got %v", err)
	}
	if len(*opened) != 0 {
		t.Fatalf("opened %v for a review that has ended", *opened)
	}
}

// THE PREVIEW ADDRESS RIDES BESIDE THE REVIEW ADDRESS for a markdown review,
// so the agent can hand the reviewer the branded page; a page under review has
// no preview route, so its result carries none.
func TestOpenReturnsThePreviewForAMarkdownReview(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "session-me")

	r, err := openDoc(t, newChannel(dir, "session-me"), doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := e.URL + "/_galley/render"; r.Preview != want {
		t.Fatalf("preview = %q, want %q", r.Preview, want)
	}
}

func TestOpenReturnsNoPreviewForAPageReview(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	page := writeDoc(t, dir, "index.html", "<html><body><p>Hi</p></body></html>\n")
	plantAdvert(t, serve.AdvertisedPath(page), "session-me")

	text, err := newChannel(dir, "session-me").callTool("galley_open",
		json.RawMessage(`{"doc":`+strconvQuote(page)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, `"preview"`) {
		t.Fatalf("a page review's open result carries a preview: %s", text)
	}
}

func TestOpenClaimsAnUnownedEditor(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "")

	c := newChannel(dir, "session-me")
	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != e.URL {
		t.Fatalf("got %+v, want the unowned advert's URL %s", r, e.URL)
	}
	if got := advertOwner(t, e.Room); got != "session-me" {
		t.Fatalf("owner after open = %q, want session-me", got)
	}
}

func TestOpenRefusesADocumentAnotherLiveSessionOwns(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	plantAdvert(t, doc, "session-other")
	if err := registry.AnnounceSession("session-other"); err != nil {
		t.Fatal(err)
	}

	c := newChannel(dir, "session-me")
	_, err := openDoc(t, c, doc)
	if err == nil {
		t.Fatal("opened a document a live session owns")
	}
	if got, want := err.Error(), "open in session session-other; its wakes go there"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestOpenReportsADocumentADeadSessionStillHolds(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	plantAdvert(t, doc, "session-gone")

	c := newChannel(dir, "session-me")
	_, err := openDoc(t, c, doc)
	if err == nil {
		t.Fatal("opened a document whose editor is still shutting down")
	}
	want := fmt.Sprintf("opened by session session-gone, which has stopped; its editor is shutting down, retry in a few seconds — if it is still there, stop it (pid %d)", os.Getpid())
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// THE SCOPE DOES NOT LIMIT galley_open (#68). The path is this session's
// explicit ask for this document, so a file outside the folder the session
// started in is opened like any other — here, one this session already has
// open, which is returned rather than refused.
func TestOpenAcceptsADocumentOutsideItsScope(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	scope := t.TempDir()
	elsewhere := t.TempDir()
	doc := writeDoc(t, elsewhere, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "session-me")

	c := newChannel(scope, "session-me")
	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatalf("refused a document outside the channel's scope: %v", err)
	}
	if r.Room != e.Room {
		t.Fatalf("room = %q, want %q", r.Room, e.Room)
	}
}

// A relative path is resolved against the channel's working directory, which
// is the session's cwd — the same directory `--scope .` names.
func TestOpenResolvesARelativePath(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "session-me")
	t.Chdir(dir)

	c := newChannel(dir, "session-me")
	r, err := openDoc(t, c, "doc.md")
	if err != nil {
		t.Fatal(err)
	}
	if r.Room != e.Room {
		t.Fatalf("relative path did not resolve to the open editor: %+v", r)
	}
}

func TestSamePageComparesBothSpellings(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "work")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if !samePage(filepath.Join(link, "d.md"), filepath.Join(real, "d.md")) {
		t.Fatal("one file under two spellings was read as two files")
	}
	if samePage(filepath.Join(real, "d.md"), filepath.Join(real, "e.md")) {
		t.Fatal("two files were read as one")
	}
}

// AN ENDED REVIEW THAT IS NOT THIS SESSION'S IS LEFT ALONE. With no registry
// entry, the runtime file is the only record of the editor; one owned by
// another session is reported as open, never stopped.
func TestOpenDoesNotStopAnotherSessionsEndedReview(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	stops := 0
	ed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_galley/revise":
			_, _ = w.Write([]byte(`{"sealed":true}`))
		case "/_galley/stop":
			stops++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(ed.Close)
	raw, err := json.Marshal(serve.Runtime{URL: ed.URL, Room: "doc", Page: doc, PID: os.Getpid(), Owner: "session-other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serve.DefaultRuntimePath(doc), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	c := newChannel(dir, "session-me")
	opened := recordOpens(c)
	_, err = openDoc(t, c, doc)
	if err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("expected an already-open error, got %v", err)
	}
	if stops != 0 || len(*opened) != 0 {
		t.Fatalf("stopped %d times, opened %v; another session's editor must be left alone", stops, *opened)
	}
}
