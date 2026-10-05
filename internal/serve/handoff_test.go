package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/versions"
	"github.com/schuettc/galley/internal/ydoc"
)

// cancelHandoff is the reviewer taking the document back mid-window — the
// escape the read-only window trades for its safety, used by tests that file
// a new instruction after a press nobody has answered.
func cancelHandoff(t *testing.T, s *EditServer) {
	t.Helper()
	if rec := postRec(t, s, "/_galley/handoff/cancel", map[string]any{}); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel handoff answered %d: %s", rec.Code, rec.Body.String())
	}
}

func postRec(t *testing.T, s *EditServer, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestTheWindowLocksTheReviewersEndpoints(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nProse about Galley.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.openResponseWindow(1, "fp", false)

	instruct := map[string]any{"op": "comment", "target": "Prose about Galley.", "text": "bold this"}
	if rec := postRec(t, s, "/_galley/instruct", instruct); rec.Code != http.StatusConflict {
		t.Fatalf("instruct mid-window: got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := postRec(t, s, "/_galley/versions/restore", map[string]any{"version": 1}); rec.Code != http.StatusConflict {
		t.Fatalf("restore mid-window: got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// A second ASK stays open mid-window: it is the recovery path when a woken
	// agent dies, and the locked browser can have written nothing new for it
	// to capture. (It lands as 204 or as the single-flight/no-reader refusal,
	// never as the handoff's.)
	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); strings.Contains(rec.Body.String(), "agent is revising") {
		t.Fatalf("the handoff refused a second ask: %d %s", rec.Code, rec.Body.String())
	}

	// The revise state carries the lock for the page to read.
	req := httptest.NewRequest(http.MethodGet, "/_galley/revise", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var state struct {
		Handoff bool `json:"handoff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil || !state.Handoff {
		t.Fatalf("revise state does not carry the lock: %s (err %v)", rec.Body.String(), err)
	}

	// Cancel returns the document to the reviewer.
	if rec := postRec(t, s, "/_galley/handoff/cancel", map[string]any{}); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if s.handoffOpenNow() {
		t.Fatal("cancel left the window open")
	}
	if rec := postRec(t, s, "/_galley/instruct", instruct); rec.Code == http.StatusConflict &&
		strings.Contains(rec.Body.String(), "agent is revising") {
		t.Fatalf("instruct still locked after cancel: %s", rec.Body.String())
	}
}

func TestACancelRescuesAnUnparseableDraft(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nProse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.openResponseWindow(1, "fp", false)

	if err := os.WriteFile(md, []byte("<div>unfinished</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := postRec(t, s, "/_galley/handoff/cancel", map[string]any{}); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: got %d: %s", rec.Code, rec.Body.String())
	}
	// The draft is preserved under recovery, and the canonical doc is back.
	entries, err := os.ReadDir(filepath.Join(dir, ".galley", "recovery"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovery dir: entries=%v err=%v", entries, err)
	}
	restored, _ := os.ReadFile(md)
	if !strings.Contains(string(restored), "Prose.") || strings.Contains(string(restored), "<div>") {
		t.Fatalf("canonical document not restored: %s", restored)
	}
}

func TestARestartMidWindowResumesInsteadOfLaundering(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nProse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s1, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	s1.openResponseWindow(1, "fp", false)
	// The agent saved a draft, it was imported, and then galley died.
	if err := os.WriteFile(md, []byte("# T\n\n**Prose**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.importDraft(); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close() // not a clean window close — the lease survives

	s2, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()

	// The window is back, and no anonymous opened round was cut for the draft.
	if !s2.handoffOpenNow() {
		t.Fatal("restart did not reopen the window")
	}
	for _, r := range mustList(t, s2) {
		if r.Reason == versions.ReasonOpened && r.N > 1 {
			t.Fatalf("the draft was laundered into an opened round: %+v", r)
		}
	}
	// …and the agent's eventual return still cuts an agent-authored round.
	s2.closeHandoff()
	if n := s2.cutApplied(); n == 0 {
		t.Fatal("no round cut after resume")
	}
	rounds := mustList(t, s2)
	last := rounds[len(rounds)-1]
	if len(last.Authors) != 1 || last.Authors[0] != versions.AuthorAgent {
		t.Fatalf("resumed round authors: %v", last.Authors)
	}
	if last.Answers != 1 {
		t.Fatalf("resumed round answers %d, want 1", last.Answers)
	}
}

func mustList(t *testing.T, s *EditServer) []versions.Round {
	t.Helper()
	rounds, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	return rounds
}

func postAckRec(t *testing.T, s *EditServer, state, note string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"state": state, "note": note})
	req := httptest.NewRequest(http.MethodPost, "/_galley/ack", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAnsweredRefusesAnUnparseableDraftAndAZeroChangeRound(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nProse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.openResponseWindow(1, "fp", false)

	// Nothing changed since the baseline: answered is a fabrication — refuse,
	// and leave the window open for the honest states.
	if rec := postAckRec(t, s, "answered", "did nothing"); rec.Code != http.StatusConflict {
		t.Fatalf("zero-change answered: got %d, want 409: %s", rec.Code, rec.Body.String())
	}

	// An unparseable draft: refuse, and do not overwrite the draft.
	if err := os.WriteFile(md, []byte("<div>nope</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := postAckRec(t, s, "answered", "done"); rec.Code != http.StatusConflict {
		t.Fatalf("unparseable answered: got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	got, _ := os.ReadFile(md)
	if !strings.Contains(string(got), "<div>") {
		t.Fatal("refusing the ack overwrote the agent's draft")
	}

	// A real change lands as one agent round answering round 1, the note is
	// the round's description, and the file comes back canonical.
	if err := os.WriteFile(md, []byte("# T\n\n**Prose**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := postAckRec(t, s, "answered", "bolded it"); rec.Code != http.StatusNoContent {
		t.Fatalf("answered: got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	rounds, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	last := rounds[len(rounds)-1]
	if last.Reason != versions.ReasonLanded || last.Answers != 1 {
		t.Fatalf("last round: %+v", last)
	}
	if len(last.Authors) != 1 || last.Authors[0] != versions.AuthorAgent {
		t.Fatalf("authors: %v", last.Authors)
	}
	if last.Instruction != "bolded it" {
		t.Fatalf("the ack note did not become the round's description: %q", last.Instruction)
	}
	if _, ok, _ := readLease(s.leasePath()); ok {
		t.Fatal("lease survived the answered ack")
	}
	final, _ := os.ReadFile(md)
	if !strings.Contains(string(final), "**Prose**") {
		t.Fatalf("resumed projection lost the agent's change: %s", final)
	}
}

func TestSeveralSavesAreOneAgentRound(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nAlpha beta gamma.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.openHandoff(1, "fp", false)

	saves := []string{
		"# T\n\nAlpha **beta** gamma.\n",
		"# T\n\nAlpha **beta** gamma delta.\n",
	}
	for _, save := range saves {
		if err := os.WriteFile(md, []byte(save), 0o644); err != nil {
			t.Fatal(err)
		}
		awaitState(t, 3*time.Second, func() bool {
			model, err := ydoc.ReadLive(s.Doc())
			if err != nil {
				return false
			}
			return string(markdown.Serialize(model)) == save
		})
	}

	// Still ONE open agent round, not two — nothing has been cut yet.
	rounds, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rounds {
		if r.Reason == versions.ReasonLanded {
			t.Fatalf("a landed round was cut before the agent returned: %+v", r)
		}
	}
}

func awaitState(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestAnAgentSaveStreamsIntoTheLiveDocument(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Title\n\nA paragraph about Galley.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.openHandoff(1, "fp", false)

	// The exact failure this design exists for: an inline formatting change.
	if err := os.WriteFile(md, []byte("# Title\n\nA paragraph about **Galley**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := s.importDraft()
	if err != nil || !imported {
		t.Fatalf("import: imported=%v err=%v", imported, err)
	}

	// The live document now carries the bold — read it back out of the CRDT.
	model, err := ydoc.ReadLive(s.Doc())
	if err != nil {
		t.Fatal(err)
	}
	out := string(markdown.Serialize(model))
	if !strings.Contains(out, "**Galley**") {
		t.Fatalf("live document did not take the bold:\n%s", out)
	}

	// A second call with an unchanged file imports nothing.
	if imported, err := s.importDraft(); err != nil || imported {
		t.Fatalf("re-import of an unchanged file: imported=%v err=%v", imported, err)
	}

	// An unparseable draft is held, not imported and not overwritten.
	if err := os.WriteFile(md, []byte("<div>raw html is refused</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if imported, err := s.importDraft(); imported || err == nil {
		t.Fatalf("bad draft: imported=%v err=%v", imported, err)
	}
	if s.draftError() == "" {
		t.Fatal("parse failure not surfaced")
	}
	held, _ := os.ReadFile(md)
	if !strings.Contains(string(held), "<div>") {
		t.Fatal("the held draft was overwritten")
	}
}

func TestProjectionIsSuspendedWhileTheWindowIsOpen(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Title\n\nA paragraph about Galley.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	s.openHandoff(1, "fp", false)
	if _, ok, err := readLease(s.leasePath()); err != nil || !ok {
		t.Fatalf("open wrote no lease: ok=%v err=%v", ok, err)
	}

	// The agent writes the file; a projection must NOT overwrite it.
	draft := []byte("# Title\n\nA paragraph about **Galley**.\n")
	if err := os.WriteFile(md, draft, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("project: %v", err)
	}
	got, _ := os.ReadFile(md)
	if string(got) != string(draft) {
		t.Fatalf("projection overwrote the agent's draft:\n%s", got)
	}

	// Closing returns the file to the save decision, which finds it moved and
	// the live document not: the draft is LOADED, never written over. (It
	// used to be: the canonical document went back over a draft nobody had
	// imported, which is galley#44 at the window's edge.)
	s.closeHandoff()
	if _, ok, _ := readLease(s.leasePath()); ok {
		t.Fatal("close left the lease behind")
	}
	if err := s.Project(); err != nil {
		t.Fatalf("project after close: %v", err)
	}
	got, _ = os.ReadFile(md)
	if string(got) != string(draft) {
		t.Fatalf("the save after close wrote over the draft:\n%s", got)
	}
	model, err := ydoc.ReadLive(s.Doc())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown.Serialize(model)), "**Galley**") {
		t.Fatal("the save after close did not load the draft")
	}
}

func TestLeaseRoundTripsAndClears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.json")
	l := &handoffLease{Round: 7, Baseline: "abc", Fingerprint: "fp", OpenedAt: time.Now().UTC(), ApproveOnAnswer: true}
	if err := writeLease(path, l); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := readLease(path)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got.Round != 7 || got.Baseline != "abc" || !got.ApproveOnAnswer {
		t.Fatalf("lease did not round-trip: %+v", got)
	}
	if err := clearLease(path); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := readLease(path); ok {
		t.Fatal("lease still present after clear")
	}
	// A missing file is "no lease", never an error — and clearing one that is
	// already gone is a no-op, because every close path clears unconditionally.
	if _, ok, err := readLease(filepath.Join(dir, "nope.json")); ok || err != nil {
		t.Fatalf("missing lease: ok=%v err=%v", ok, err)
	}
	if err := clearLease(filepath.Join(dir, "nope.json")); err != nil {
		t.Fatalf("clearing a missing lease: %v", err)
	}
	// A corrupt lease is an error, not a silent no-lease: it names an agent's
	// in-flight round, and dropping it silently launders that work.
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLease(path); err == nil {
		t.Fatal("corrupt lease read as no lease")
	}
}

// Close waits for an import already under way. The watcher is stopped first
// so nothing lands after the final flush, and an import that had started
// before the stop must finish before Close returns: otherwise it writes the
// handoff lease into a directory the caller has been told is finished with.
func TestCloseWaitsForAnImportUnderWay(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# T\n\nAlpha.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.testImportTick = func() {
		once.Do(func() { close(started) })
		<-release
	}
	s.openHandoff(1, "fp", false)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the watcher never ticked")
	}
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned while an import was still under way")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close never returned after the import finished")
	}
}
