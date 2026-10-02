package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/ydoc"
)

// keysOf reads THE PENDING VIEW, which is what every surface renders from.
func keysOf(t *testing.T, s *EditServer) []string {
	t.Helper()
	view, err := s.pending()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	var out []string
	for _, in := range view.Instructions {
		out = append(out, in.Key)
	}
	return out
}

// reviewerDeletes removes the block whose plain text starts with `prefix`, the
// way a reviewer selecting a sentence and pressing Backspace does: through the
// live document, not through the file. `importDraft` is the AGENT's path and is
// gated on a handoff lease, so driving this with a file write measures nothing
// — the first cut of these tests did exactly that and the deletion never
// reached the CRDT at all.
func reviewerDeletes(t *testing.T, s *EditServer, prefix string) {
	t.Helper()
	code, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		out := docmodel.Doc{}
		for _, b := range model.Blocks {
			var said string
			for _, in := range b.Inlines {
				said += in.Text
			}
			if strings.HasPrefix(said, prefix) {
				continue
			}
			out.Blocks = append(out.Blocks, b)
		}
		return out, nil, nil
	})
	if err != nil {
		t.Fatalf("reviewer delete: %d %v", code, err)
	}
}

// TestDeletingTheSentenceDeletesTheInstruction is Court's rule, and it is the
// idiom this codebase already uses for the agent's marks: deleting text under a
// mark is the decision, by hand.
func TestDeletingTheSentenceDeletesTheInstruction(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md,
		[]byte("# T\n\nCognito mints every token.\n\nSecond para here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Cognito mints every token.", "text": "too punchy",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	if got := keysOf(t, s); len(got) != 1 {
		t.Fatalf("the fixture did not file one instruction: %v", got)
	}
	// The projection has to SEE it anchored before it can know it went. That is
	// the guard, not an artefact of the test: "no mark now" and "the reviewer
	// deleted the words" are different claims.
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	// The reviewer deletes the sentence their instruction was about.
	reviewerDeletes(t, s, "Cognito mints every token.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	if got := keysOf(t, s); len(got) != 0 {
		t.Errorf("the instruction outlived the words it was about: %v — it would sit in the rail as work the reviewer already retracted", got)
	}
	// AND A RESTART DOES NOT BRING IT BACK: pending.json is what a new process
	// reads, and it no longer holds the comment.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	if got := keysOf(t, again); len(got) != 0 {
		t.Errorf("a restart brought the retracted instruction back: %v", got)
	}
}

// TestAnAgentWriteThatRemovesACommentsWordsCutsNoRound is the spurious round.
// In live mode a settle is a send, so any fingerprint the agent did not cause
// is a round. The agent rewrites the words a seen comment highlights; mutate
// seeds the notifier with what the agent was handed, and the projection that
// follows must hand the notifier that same fingerprint. A projection that
// writes the review map (deleting the thread) moves it, and cuts a round.
func TestAnAgentWriteThatRemovesACommentsWordsCutsNoRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	s.Notify.Quiet = 50 * time.Millisecond
	if err := s.SetMode(ModeLive); err != nil {
		t.Fatal(err)
	}
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?"})
	if err := s.Project(); err != nil { // seen
		t.Fatal(err)
	}
	if _, err := s.mutate(byAgent, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		out := docmodel.Clone(model)
		for i := range out.Blocks {
			for j := range out.Blocks[i].Inlines {
				if strings.Contains(out.Blocks[i].Inlines[j].Text, "quick") {
					out.Blocks[i].Inlines[j] = docmodel.Inline{Text: "A dog"}
				}
			}
		}
		return out, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := roundCount(t, s)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if after := roundCount(t, s); after != before {
		t.Errorf("the agent's own write cut %d round(s) after it (%d -> %d)", after-before, before, after)
	}
}

// TestUndoingTheDeletionBringsTheCommentBack: the reviewer deletes the
// highlighted words, a projection runs, and they press undo. The words and the
// highlight, with its ID, come back, and so does the comment: in the rail and
// in pending.json. Deleting the thread at the projection made this a comment
// lost to an undo.
func TestUndoingTheDeletionBringsTheCommentBack(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?"})
	key := onlyKey(t, s)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	withMark, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	reviewerDeletes(t, s, "the quick fox")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if got := loadUnsent(t, s); len(got) != 0 {
		t.Fatalf("the fixture did not retract the comment from pending.json: %+v", got)
	}
	gone, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	// Undo is the browser writing the old fragment back, ID attrs and all.
	if err := s.yjs.Apply(t.Context(), s.Room, func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		ydoc.Write(doc, transact, gone, withMark)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	v := pendingView(t, s)
	if len(v.Instructions) != 1 || v.Instructions[0].Key != key || v.Instructions[0].Run == "" {
		t.Errorf("after undo the rail lists %+v, want %s placed", v.Instructions, key)
	}
	if got := loadUnsent(t, s); len(got) != 1 || got[0].Key != key {
		t.Errorf("after undo pending.json = %+v, want %s", got, key)
	}
}

func roundCount(t *testing.T, s *EditServer) int {
	t.Helper()
	rs, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	return len(rs)
}

// TestAWholeDocumentInstructionIsNeverSwept is the counter-case: a block or
// whole-document instruction has NO anchor text by construction, so it has no
// mark to lose and must never be swept.
//
// IT DOES NOT PROVE THE `Anchor` CLAUSE, and that is recorded rather than
// implied: `seenAnchored` catches the same threads for a different reason — one
// that never paired is never a candidate. Both guards are kept (see
// lostanchor.go for why); only one of them is reachable, so only one of them
// has a red proof.
func TestAWholeDocumentInstructionIsNeverSwept(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte("# T\n\nCognito mints every token.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment_document", "text": "tighten the whole thing",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	before := keysOf(t, s)
	if len(before) != 1 {
		t.Fatalf("the fixture did not file a whole-document instruction: %v", before)
	}
	for i := 0; i < 3; i++ {
		if err := s.Project(); err != nil {
			t.Fatal(err)
		}
	}
	if got := keysOf(t, s); len(got) != 1 {
		t.Errorf("a whole-document instruction was swept: %v — it never had a mark to lose", got)
	}
}

// TestAnInstructionNeverSeenAnchoredSurvives holds the guard honest. A comment
// whose mark this process never saw also has no mark, and sweeping on that
// evidence would destroy instructions nobody touched, with no keystroke.
func TestAnInstructionNeverSeenAnchoredSurvives(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte("# T\n\nCognito mints every token.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Cognito mints every token.", "text": "too punchy",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	// NEVER PROJECTED WHILE ANCHORED, so this server has never seen the pairing
	// — exactly the state a restart lands in.
	s.seenAnchored = map[string]bool{}
	reviewerDeletes(t, s, "Cognito mints every token.")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if got := keysOf(t, s); len(got) != 1 {
		t.Errorf("an instruction this server never saw anchored was swept: %v — no mark is not evidence the reviewer deleted anything", got)
	}
}

// TestTrimmingTheSentenceKeepsTheInstruction is the boundary. Deleting HALF the
// sentence leaves the mark on what survives, so the instruction stays: the
// reviewer trimmed their sentence, they did not retract the note.
func TestTrimmingTheSentenceKeepsTheInstruction(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md,
		[]byte("# T\n\nCognito mints every token on request.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Cognito mints every token on request.", "text": "too punchy",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "{==") {
		t.Fatalf("the fixture has no highlight to trim: %s", got)
	}
	// Trim the tail off the highlighted span, leaving the mark on the rest.
	trimmed := strings.Replace(string(got), " on request.==}", "==}", 1)
	if trimmed == string(got) {
		t.Fatalf("the fixture did not trim: %s", got)
	}
	if err := os.WriteFile(md, []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.importDraft(); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if got := keysOf(t, s); len(got) != 1 {
		t.Errorf("trimming the sentence retracted the instruction: %v — the mark still covers what survived", got)
	}
}

// TestTheSendDeletesARetractedThread: the retracted comment's thread waits in
// the review map for the next send, whose clear deletes it with the threads it
// sent. It is not one of the round's asks: the pending view hid it.
func TestTheSendDeletesARetractedThread(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	s.OnRevise = "true"
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?"})
	retracted := onlyKey(t, s)
	instructOK(t, s, map[string]any{"op": "comment", "target": "Second para here.", "text": "cut it"})
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	reviewerDeletes(t, s, "the quick fox")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if _, ok := threadByKey(s.doc, retracted); !ok {
		t.Fatal("the fixture's retracted thread already left the review map before the send")
	}

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	if threads := review.Read(s.doc); len(threads) != 0 {
		t.Errorf("the send left threads in the review map: %+v", threads)
	}
	rs, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rs[len(rs)-1].Asks {
		if a.Key == retracted {
			t.Errorf("the round asks for the retracted comment %s", retracted)
		}
	}
}
