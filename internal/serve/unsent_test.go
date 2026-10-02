package serve

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/unsent"
	"github.com/schuettc/galley/internal/versions"
)

// unsentDoc is a document with a paragraph to highlight and a figure to pin a
// rectangle on: the two comments whose words and place lived only in RAM.
const unsentDoc = "# T\n\nCognito mints every token.\n\n![the diagram](fig.png)\n"

// loadUnsent reads the unsent round the server wrote, failing the test on any
// error: a file this server wrote and cannot read back is the bug.
func loadUnsent(t *testing.T, s *EditServer) []unsent.Comment {
	t.Helper()
	f, err := unsent.Load(unsent.Path(s.MdPath))
	if err != nil {
		t.Fatalf("load pending.json: %v", err)
	}
	return f.Comments
}

// onlyKey is the key of the one comment pending.json holds.
func onlyKey(t *testing.T, s *EditServer) string {
	t.Helper()
	got := loadUnsent(t, s)
	if len(got) != 1 {
		t.Fatalf("pending.json holds %d comments, want 1: %+v", len(got), got)
	}
	return got[0].Key
}

func instructOK(t *testing.T, s *EditServer, body map[string]any) {
	t.Helper()
	if rec := postRec(t, s, "/_galley/instruct", body); rec.Code >= 300 {
		t.Fatalf("instruct %v: %d %s", body["op"], rec.Code, rec.Body.String())
	}
}

// figureKey is the block key of the document's only image.
func figureKey(t *testing.T, s *EditServer) string {
	t.Helper()
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range view.Blocks {
		if b.Kind == string(docmodel.Image) {
			return b.Key
		}
	}
	t.Fatalf("the fixture has no figure: %+v", view.Blocks)
	return ""
}

// fileThreeComments files one comment of each kind: a range on "Cognito", a
// rectangle on the figure, and the whole document.
func fileThreeComments(t *testing.T, s *EditServer) {
	t.Helper()
	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": "name the issuer",
	})
	instructOK(t, s, map[string]any{
		"op": "comment_block", "target": figureKey(t, s), "text": "this box is wrong",
		"region": map[string]any{"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.4},
	})
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "tighten the whole thing"})
}

func TestFilingAnInstructionWritesTheUnsentRoundBeforeTheFile(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })

	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": "name the issuer",
	})
	// No Project has run: the debounce has not fired and nothing called it.
	got := loadUnsent(t, s)
	if len(got) != 1 || got[0].Text != "name the issuer" || got[0].Quote != "Cognito" ||
		got[0].Kind != unsent.KindText {
		t.Fatalf("pending.json after one filing = %+v, want the range comment on \"Cognito\"", got)
	}
}

func TestTheUnsentRoundSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "d.md", unsentDoc)
	fileThreeComments(t, s)
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := NewEdit(filepath.Join(dir, "d.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	view, err := again.pending()
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]InstructionView{}
	for _, in := range view.Instructions {
		byText[in.Text] = in
	}
	if len(view.Instructions) != 3 {
		t.Errorf("after a restart the rail lists %d instructions, want 3: %+v", len(view.Instructions), view.Instructions)
	}
	rng, ok := byText["name the issuer"]
	if !ok {
		t.Fatalf("the range comment's words did not survive the restart: %+v", view.Instructions)
	}
	if rng.Run == "" {
		t.Errorf("the range comment survived but is unplaced: %+v", rng)
	}
	fig, ok := byText["this box is wrong"]
	if !ok {
		t.Fatalf("the figure comment did not survive the restart: %+v", view.Instructions)
	}
	if fig.Region == nil || *fig.Region != (review.Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}) {
		t.Errorf("the figure comment's rectangle = %+v, want {0.1 0.2 0.3 0.4}", fig.Region)
	}
	if fig.AnchorKey == "" {
		t.Errorf("the figure comment came back with no block to sit on: %+v", fig)
	}
	if _, ok := byText["tighten the whole thing"]; !ok {
		t.Errorf("the document comment did not survive the restart: %+v", view.Instructions)
	}
}

func TestEditingAndDeletingRewriteTheUnsentRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": "name the issuer",
	})
	key := onlyKey(t, s)

	instructOK(t, s, map[string]any{"op": "edit", "key": key, "text": "say which issuer"})
	if got := loadUnsent(t, s); len(got) != 1 || got[0].Text != "say which issuer" {
		t.Fatalf("pending.json after an edit = %+v, want the new words", got)
	}

	if rec := postRec(t, s, "/_galley/instruction/delete", map[string]any{"key": key}); rec.Code >= 300 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if got := loadUnsent(t, s); len(got) != 0 {
		t.Fatalf("pending.json after a delete = %+v, want nothing", got)
	}
}

func TestReviseRecordsTheRoundBeforeEmptyingTheUnsentRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	s.OnRevise = "true"
	fileThreeComments(t, s)
	sent := map[string]bool{}
	for _, c := range loadUnsent(t, s) {
		sent[c.Key] = true
	}

	var hooked bool
	testHookBeforeUnsentCleared = func() {
		hooked = true
		rs := rounds(t, s)
		if len(rs) == 0 {
			t.Error("no round was recorded before the unsent round was emptied")
			return
		}
		asked := map[string]bool{}
		for _, a := range rs[len(rs)-1].Asks {
			asked[a.Key] = true
		}
		for key := range sent {
			if !asked[key] {
				t.Errorf("the last round does not carry %s, and the unsent round is about to be emptied", key)
			}
		}
		still := map[string]bool{}
		for _, c := range loadUnsent(t, s) {
			still[c.Key] = true
		}
		for key := range sent {
			if !still[key] {
				t.Errorf("%s left pending.json before the round was recorded", key)
			}
		}
	}
	t.Cleanup(func() { testHookBeforeUnsentCleared = nil })

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	if !hooked {
		t.Fatal("Revise never reached the point where the unsent round is emptied")
	}
	for _, c := range loadUnsent(t, s) {
		if sent[c.Key] {
			t.Errorf("pending.json still holds the sent comment %s after Revise", c.Key)
		}
	}
}

// Bug 5: the rail filtered a retracted instruction and the wait payload and
// the notify fingerprint did not, so the agent was handed work the reviewer
// had already taken back.
func TestTheWaitPayloadAndTheRailAgree(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", "# T\n\nCognito mints every token.\n\nSecond para here.\n")
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{
		"op": "comment", "target": "Cognito mints every token.", "text": "too punchy",
	})
	keys := keysOf(t, s)
	if len(keys) != 1 {
		t.Fatalf("the fixture did not file one instruction: %v", keys)
	}
	key := keys[0]
	// The projection has to see the anchor before it can know it went.
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	reviewerDeletes(t, s, "Cognito mints every token.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	if got := keysOf(t, s); len(got) != 0 {
		t.Fatalf("the fixture did not retract the instruction from the rail: %v", got)
	}
	_, view, err := s.waitFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 0 {
		t.Errorf("the wait payload still carries the retracted instruction: %+v", view.Instructions)
	}

	// The fingerprint input: removing the retracted thread outright must not
	// move it, because the fingerprint should never have counted it.
	model, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	withThread := s.editFingerprint(model)
	if _, err := s.mutate(bySystem, func(m docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		return m, func(doc *crdt.Doc, tx review.Tx) { _ = review.Bind(doc, tx).Delete(key) }, nil
	}); err != nil {
		t.Fatal(err)
	}
	model, err = s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	if without := s.editFingerprint(model); without != withThread {
		t.Errorf("the notify fingerprint counted the retracted instruction: %s with it, %s without", withThread, without)
	}
}

func TestPageModeKeepsItsUnsentRoundBesideItsRounds(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)
	s, err := NewEditPage(page)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "make it warmer"})

	contentPath := filepath.Join(dir, ".galley", "pages", "page", "content.md")
	want := filepath.Join(versions.Open(contentPath).Dir(), "pending.json")
	f, err := unsent.Load(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Comments) != 1 || f.Comments[0].Text != "make it warmer" {
		t.Fatalf("page mode's unsent round at %s = %+v, want the comment", want, f.Comments)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("page mode wrote no unsent round beside its rounds: %v", err)
	}
}

// writeUnsentRaw puts raw bytes where the document's unsent round lives.
func writeUnsentRaw(t *testing.T, md string, raw []byte) string {
	t.Helper()
	path := unsent.Path(md)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestANewerUnsentRoundRefusesToStartAndIsLeftAlone(t *testing.T) {
	md := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(md, []byte(unsentDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"v":2,"comments":[],"extra":1}`)
	path := writeUnsentRaw(t, md, raw)

	if s, err := NewEdit(md); err == nil {
		_ = s.Close()
		t.Fatal("galley started over a newer unsent round, and would overwrite it at the first comment")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(raw) {
		t.Errorf("the newer unsent round was touched: %q, %v", got, err)
	}
}

func TestAnUnreadableUnsentRoundIsMovedAsideAndGalleyStarts(t *testing.T) {
	md := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(md, []byte(unsentDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	path := writeUnsentRaw(t, md, []byte(`{"v":1,"comments":[`))
	var said bytes.Buffer
	unsentStderr = &said
	t.Cleanup(func() { unsentStderr = os.Stderr })

	s, err := NewEdit(md)
	if err != nil {
		t.Fatalf("an unreadable unsent round stopped galley starting: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := os.Stat(path + ".unreadable"); err != nil {
		t.Errorf("the unreadable unsent round was not kept beside the store: %v", err)
	}
	if !strings.Contains(said.String(), "unreadable") {
		t.Errorf("moving the unsent round aside was silent: %q", said.String())
	}
}

func TestAnUnsentRoundGalleyCannotReadForAnotherReasonIsAnError(t *testing.T) {
	md := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(md, []byte(unsentDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory where the file should be: not corrupt, so not ours to move.
	if err := os.MkdirAll(unsent.Path(md), 0o755); err != nil {
		t.Fatal(err)
	}
	if s, err := NewEdit(md); err == nil {
		_ = s.Close()
		t.Fatal("galley started over an unsent round it could not read")
	}
	if _, err := os.Stat(unsent.Path(md) + ".unreadable"); err == nil {
		t.Error("an I/O error was quarantined as if the file were corrupt")
	}
}

// NEVER NEITHER, under a concurrent filing. Revise clears the sent threads out
// of the review map before the round is recorded, and a comment filed in that
// window rewrites pending.json from the map: without the sent keys held aside,
// they left the file before any round carried them.
func TestACommentFiledWhileARoundIsSentKeepsTheSentRoundInPendingJSON(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	s.OnRevise = "true"
	fileThreeComments(t, s)
	sent := map[string]bool{}
	for _, c := range loadUnsent(t, s) {
		sent[c.Key] = true
	}

	var hooked bool
	testHookAfterInstructionsCleared = func() {
		hooked = true
		instructOK(t, s, map[string]any{"op": "comment_document", "text": "filed mid-send"})
		still := map[string]bool{}
		var late bool
		for _, c := range loadUnsent(t, s) {
			still[c.Key] = true
			late = late || c.Text == "filed mid-send"
		}
		for key := range sent {
			if !still[key] {
				t.Errorf("%s left pending.json before its round was recorded", key)
			}
		}
		if !late {
			t.Error("the comment filed mid-send is not in pending.json")
		}
	}
	t.Cleanup(func() { testHookAfterInstructionsCleared = nil })

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	if !hooked {
		t.Fatal("Revise never reached the point between the clear and the cut")
	}
	got := loadUnsent(t, s)
	if len(got) != 1 || got[0].Text != "filed mid-send" {
		t.Errorf("pending.json after the send = %+v, want only the comment filed mid-send", got)
	}
}

// Two sends can overlap (a Revise press and a live settle), so one send
// finishing must not release another's in-flight round.
func TestOneSendDoesNotReleaseAnotherSendsRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	s.OnRevise = "true"
	fileThreeComments(t, s)
	other := unsent.Comment{Key: "cd-00000000000000aa", Kind: unsent.KindDocument,
		Text: "another send's round", Author: review.AuthorCourt, At: time.Now()}
	s.mu.Lock()
	s.sending = map[string]unsent.Comment{other.Key: other}
	s.mu.Unlock()

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	got := loadUnsent(t, s)
	if len(got) != 1 || got[0].Key != other.Key {
		t.Errorf("pending.json after one send = %+v, want only the other send's in-flight %s", got, other.Key)
	}
}

// A comment that could not be stored is not reported as filed.
func TestACommentThatCannotBeStoredIsAnError(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	path := unsent.Path(s.MdPath)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory where the file goes: nothing can be renamed over it.
	if err := os.MkdirAll(filepath.Join(path, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := postRec(t, s, "/_galley/instruct", map[string]any{"op": "comment_document", "text": "kept?"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("filing a comment pending.json could not hold answered %d %s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "could not store the unsent round") {
		t.Errorf("the 500 does not say what failed: %q", rec.Body.String())
	}
}

// A note typed into the file by hand gets its thread at startup, and that
// thread is mirrored at once: pending.json is the unsent round as the rail
// shows it from the first moment, not from the first mutation after.
func TestANoteImportedAtStartupIsInTheUnsentRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc+"\n{>>@document tighten the whole thing<<}\n")
	t.Cleanup(func() { _ = s.Close() })
	got := loadUnsent(t, s)
	if len(got) != 1 || got[0].Text != "tighten the whole thing" || got[0].Kind != unsent.KindDocument {
		t.Fatalf("pending.json after opening a file with a hand-typed note = %+v, want the document comment", got)
	}
}
