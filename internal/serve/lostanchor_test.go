package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
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
	// AND IT IS GONE, not hidden: the review map no longer holds it, so a
	// restart cannot bring it back.
	if threads := review.Read(s.doc); len(threads) != 0 {
		t.Errorf("the retracted thread is still in the review map: %+v", threads)
	}
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
