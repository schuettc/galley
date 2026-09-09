package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/registry"
)

// galley_open IS HOW A SESSION PUTS A DOCUMENT UNDER REVIEW. The channel is
// the session's presence inside galley, so it is the one thing that knows,
// with certainty, which session an editor should belong to. These tests cover
// the resolution order for a document that is ALREADY open; open_spawn_test.go
// covers the spawn.

func plantAdvert(t *testing.T, page, owner string) registry.Entry {
	t.Helper()
	e := registry.Entry{
		URL: "http://127.0.0.1:1", Room: "room-" + registry.Token(page)[:8],
		Page: page, PID: os.Getpid(), Owner: owner,
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
	r, err := openDoc(t, c, doc)
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != e.URL || r.Room != e.Room || r.Page != doc {
		t.Fatalf("got %+v, want the existing advert %+v", r, e)
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
	want := "opened by session session-gone, which has stopped; its editor is shutting down, retry in a few seconds"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestOpenRefusesADocumentOutsideItsScope(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	scope := t.TempDir()
	elsewhere := t.TempDir()
	doc := writeDoc(t, elsewhere, "doc.md", "# T\n")

	c := newChannel(scope, "session-me")
	_, err := openDoc(t, c, doc)
	if err == nil {
		t.Fatal("opened a document outside the channel's scope")
	}
	if !strings.Contains(err.Error(), doc) || !strings.Contains(err.Error(), scope) {
		t.Fatalf("error names neither the document nor the scope: %q", err)
	}
}

// A relative path is resolved against the channel's working directory, which
// is the session's cwd — the same directory `--scope .` names.
func TestOpenResolvesARelativePath(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n")
	e := plantAdvert(t, doc, "session-me")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

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
