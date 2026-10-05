package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/ydoc"
)

// galley#44: a change made to the file outside the editor is loaded, kept or
// left alone, never written over in silence. See saveLocked.

// reviewerTypes replaces old with repl in the live document the way the
// browser lands a keystroke: a peer update, not an endpoint mutation.
func reviewerTypes(t *testing.T, s *EditServer, old, repl string) {
	t.Helper()
	before, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	after := docmodel.Clone(before)
	found := false
	for i, b := range after.Blocks {
		for j, in := range b.Inlines {
			if strings.Contains(in.Text, old) {
				after.Blocks[i].Inlines[j].Text = strings.Replace(in.Text, old, repl, 1)
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the live document has no %q", old)
	}
	if err := s.yjs.Apply(t.Context(), s.Room, func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		ydoc.Write(doc, transact, before, after)
	}); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// stop is the CLI's shutdown, minus the HTTP server: the final flush, then
// Close.
func stop(t *testing.T, s *EditServer) {
	t.Helper()
	if err := s.Flush(); err != nil {
		t.Fatalf("final flush: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// diskNoticeOf is the sentence the bar shows, read off GET /_galley/revise.
func diskNoticeOf(t *testing.T, s *EditServer) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_galley/revise", nil))
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("revise state: %v: %s", err, rec.Body.String())
	}
	notice, _ := view["diskNotice"].(string)
	return notice
}

// recovered is every file in the document's .galley/recovery/.
func recovered(t *testing.T, s *EditServer) []string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(s.MdPath), ".galley", "recovery")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, string(mustRead(t, filepath.Join(dir, e.Name()))))
	}
	return out
}

// CASE 1 OF galley#44: the file is moved away while the editor runs, and
// stopping the editor used to write it back at the old path.
func TestAMovedFileIsNotWrittenBack(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nFirst paragraph.\n")
	if err := os.Mkdir(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.MdPath, filepath.Join(dir, "b", "doc.md")); err != nil {
		t.Fatal(err)
	}
	stop(t, s)
	if _, err := os.Stat(s.MdPath); !os.IsNotExist(err) {
		t.Fatalf("a file came back at the old path: %v", err)
	}
}

// CASE 2 OF galley#44: the agent rewrites the open file and nobody touches the
// editor. The rewrite used to be put back the way galley had it.
func TestAnOutsideRewriteIsLoadedAndKept(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "c.md",
		"# Title\n\nStatus: draft\nWhere: here\n\nFirst paragraph.\n\nSecond paragraph.\n")
	rewrite := "# Title\n\nStatus: final\n\nWhere: there\n\nRewritten first paragraph.\n\nSecond paragraph.\n"
	writeFile(t, s.MdPath, rewrite)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(liveText(t, s), "Rewritten first paragraph.") {
		t.Errorf("the live document did not load the rewrite:\n%s", liveText(t, s))
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, s.MdPath)); got != rewrite {
		t.Errorf("the rewrite was written over:\n%s", got)
	}
	if !strings.Contains(liveText(t, s), "Where: there") {
		t.Errorf("the live document does not hold the rewrite:\n%s", liveText(t, s))
	}
	_ = s.Close()
}

// respelled is a document SerializeOnto does not give back byte for byte: a
// table whose delimiter row it rewrites (galley#52) and tight code fences.
const respelled = "# Title\n\nSome prose.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nx := 1\n```\n```sh\necho hi\n```\n\nLast paragraph.\n"

// AN UNTOUCHED DOCUMENT IS NOT WRITTEN, even one the projection respells: the
// live side is compared with the baseline's own projection, not the file.
func TestAnUntouchedDocumentIsNotWritten(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", respelled)
	before, err := os.Stat(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	stop(t, s)
	after, err := os.Stat(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, s.MdPath)); got != respelled {
		t.Errorf("an untouched document was rewritten:\n%s", got)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("an untouched document was written: mtime %v, was %v", after.ModTime(), before.ModTime())
	}
}

// A CLASH: the reviewer and the file both moved. The reviewer's copy is
// written, the file as found is kept in .galley/recovery/, and the bar says so.
func TestAClashKeepsTheReviewersCopyAndRecoversTheFile(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	defer func() { _ = s.Close() }()
	reviewerTypes(t, s, "Second para here.", "Second para, from the reviewer.")
	outside := "# T\n\nthe quick red fox jumps.\n\nSecond para here.\n"
	writeFile(t, s.MdPath, outside)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, s.MdPath))
	if !strings.Contains(got, "from the reviewer") || strings.Contains(got, "red fox") {
		t.Errorf("the file is not the reviewer's copy:\n%s", got)
	}
	if kept := recovered(t, s); len(kept) != 1 || kept[0] != outside {
		t.Errorf(".galley/recovery/ does not hold the file as found: %q", kept)
	}
	notice := diskNoticeOf(t, s)
	if !strings.HasPrefix(notice, "d.md was changed outside the editor while you were editing.") ||
		!strings.Contains(notice, ".galley/recovery/d-") {
		t.Errorf("notice = %q", notice)
	}
}

// A MISSING FILE is said, never recreated, and the reviewer's unsaved edits
// are kept in .galley/recovery/ at stop.
func TestAMissingFileIsSaidAndItsEditsRecoveredAtStop(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	if err := os.Remove(s.MdPath); err != nil {
		t.Fatal(err)
	}
	reviewerTypes(t, s, "Second para here.", "Second para, from the reviewer.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if got, want := diskNoticeOf(t, s), "d.md is no longer at this path. Nothing is being saved."; got != want {
		t.Errorf("notice = %q, want %q", got, want)
	}
	stop(t, s)
	if _, err := os.Stat(s.MdPath); !os.IsNotExist(err) {
		t.Fatalf("a missing file was recreated: %v", err)
	}
	kept := recovered(t, s)
	if len(kept) != 1 || !strings.Contains(kept[0], "from the reviewer") {
		t.Errorf("the unsaved edits are not in .galley/recovery/: %q", kept)
	}
}

// AN UNPARSEABLE FILE is neither loaded nor written over.
func TestAnUnparseableFileIsLeftAlone(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	defer func() { _ = s.Close() }()
	bad := "<div>raw html is refused</div>\n"
	writeFile(t, s.MdPath, bad)
	reviewerTypes(t, s, "Second para here.", "Second para, from the reviewer.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, s.MdPath)); got != bad {
		t.Errorf("an unparseable file was written over:\n%s", got)
	}
	want := "d.md was changed outside the editor in a form galley can't read. Nothing is being saved until it's fixed."
	if got := diskNoticeOf(t, s); got != want {
		t.Errorf("notice = %q, want %q", got, want)
	}
}

// commentedFox is the reviewer's unsent text comment on "the quick fox",
// projected so its marker is in the file.
func commentedFox(t *testing.T) *EditServer {
	t.Helper()
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?"})
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if n := len(loadUnsent(t, s)); n != 1 {
		t.Fatalf("the fixture's comment is not in pending.json: %d", n)
	}
	return s
}

// commentState is where the one comment stands: in pending.json, in the
// pending view, and placed there or not.
func commentState(t *testing.T, s *EditServer) (inFile, inView, placed bool) {
	t.Helper()
	inFile = len(loadUnsent(t, s)) == 1
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range view.Instructions {
		if v.Text == "which fox?" {
			inView, placed = true, v.Run != ""
		}
	}
	return inFile, inView, placed
}

// A LOAD WHOSE FILE LACKS A COMMENT'S MARKER is not the reviewer deleting
// their words: the comment stays, unsent and unplaced.
func TestALoadWithoutTheMarkerKeepsTheCommentUnplaced(t *testing.T) {
	s := commentedFox(t)
	writeFile(t, s.MdPath, "# T\n\nthe quick fox jumps.\n\nSecond para, rewritten whole by the agent.\n")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(liveText(t, s), "rewritten whole") {
		t.Fatal("the fixture's outside save was not loaded")
	}
	inFile, inView, placed := commentState(t, s)
	if !inFile || !inView {
		t.Fatalf("the comment was dropped: pending.json=%v view=%v", inFile, inView)
	}
	if placed {
		t.Error("the comment is placed on a document that has no marker for it")
	}
	// And a later projection does not retract it either.
	reviewerTypes(t, s, "rewritten whole", "rewritten wholly")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if inFile, inView, _ := commentState(t, s); !inFile || !inView {
		t.Errorf("the next projection dropped the comment: pending.json=%v view=%v", inFile, inView)
	}
}

// A COMMENT THE REVIEWER TOOK BACK stays taken back, even when an outside
// save restores the file as it was with the marker.
func TestALoadRestoringTheMarkerDoesNotBringBackARetractedComment(t *testing.T) {
	s := commentedFox(t)
	withMarker := string(mustRead(t, s.MdPath))
	reviewerTypes(t, s, "the quick fox", "")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if inFile, inView, _ := commentState(t, s); inFile || inView {
		t.Fatalf("the fixture's deletion did not retract the comment: pending.json=%v view=%v", inFile, inView)
	}
	writeFile(t, s.MdPath, withMarker)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(liveText(t, s), "the quick fox") {
		t.Fatal("the fixture's outside save was not loaded")
	}
	if inFile, inView, _ := commentState(t, s); inFile || inView {
		t.Errorf("an outside marker brought back a retracted comment: pending.json=%v view=%v", inFile, inView)
	}
}

// A SAVE LANDING BETWEEN THE READ AND THE RENAME survives: the write's last
// check finds the file is not the one it decided about, discards its
// replacement and decides again, and the outside save is kept.
func TestAnOutsideSaveDuringTheWriteSurvives(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	defer func() { _ = s.Close() }()
	outside := "# T\n\nthe quick red fox jumps.\n\nSecond para here.\n"
	landed := false
	s.mu.Lock()
	s.testWritePrepared = func() {
		if !landed {
			landed = true
			writeFile(t, s.MdPath, outside)
		}
	}
	s.mu.Unlock()
	reviewerTypes(t, s, "Second para here.", "Second para, from the reviewer.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if !landed {
		t.Fatal("the hook never ran: nothing was written")
	}
	if kept := recovered(t, s); len(kept) != 1 || kept[0] != outside {
		t.Errorf("the save that landed mid-write was lost: recovery holds %q", kept)
	}
	if got := string(mustRead(t, s.MdPath)); !strings.Contains(got, "from the reviewer") {
		t.Errorf("the reviewer's copy was not written after the retry:\n%s", got)
	}
	tmps, _ := filepath.Glob(filepath.Join(filepath.Dir(s.MdPath), ".d.md-*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("a discarded replacement was left behind: %v", tmps)
	}
}

// THE PAGE'S CUE STILL MOVES WHEN NOTHING IS WRITTEN. The browser re-reads
// the pending list when /_galley/rev moves, and rev was the file's mtime: a
// whole-document instruction moves no markdown, so a save that writes nothing
// left the new card off the rail.
func TestARevMovesOnASaveThatWritesNothing(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	defer func() { _ = s.Close() }()
	rev := func() float64 {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_galley/rev", nil))
		var view map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("rev: %v: %s", err, rec.Body.String())
		}
		n, _ := view["rev"].(float64)
		return n
	}
	before := rev()
	time.Sleep(10 * time.Millisecond)
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "open with the decision"})
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if after := rev(); after == before {
		t.Error("/_galley/rev did not move after a save, so the page never re-reads the pending list")
	}
	// And a missing file is a rev, not an error.
	if err := os.Remove(s.MdPath); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_galley/rev", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/_galley/rev with the file missing answered %d", rec.Code)
	}
}

// THE PAGE LOAD LOOKS AT THE FILE. With no watcher, an agent's save between
// rounds would otherwise wait for the next save to be loaded, and a reviewer
// opening the page would read the document as it was.
func TestOpeningThePageLoadsAnOutsideSave(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	defer func() { _ = s.Close() }()
	writeFile(t, s.MdPath, "# T\n\nthe quick red fox jumps.\n\nSecond para here.\n")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / answered %d", rec.Code)
	}
	if !strings.Contains(liveText(t, s), "red fox") {
		t.Errorf("opening the page did not load the outside save:\n%s", liveText(t, s))
	}
}

// AN UNPARSEABLE FILE AT STOP keeps the reviewer's unsaved edits in
// .galley/recovery/, as a missing one does: the save leaves the file alone, so
// nothing else holds them.
func TestAnUnparseableFileAtStopKeepsTheEditsInRecovery(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	bad := "<div>raw html is refused</div>\n"
	writeFile(t, s.MdPath, bad)
	reviewerTypes(t, s, "Second para here.", "Second para, from the reviewer.")
	stop(t, s)
	if got := string(mustRead(t, s.MdPath)); got != bad {
		t.Errorf("an unparseable file was written over:\n%s", got)
	}
	kept := recovered(t, s)
	if len(kept) != 1 || !strings.Contains(kept[0], "from the reviewer") {
		t.Errorf("the unsaved edits are not in .galley/recovery/: %q", kept)
	}
}
