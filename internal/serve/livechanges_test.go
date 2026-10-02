package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
)

func editServerWith(t *testing.T, body string) *EditServer {
	t.Helper()
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// dropBlock deletes the block whose text is `said`, the way a reviewer
// selecting a paragraph and pressing Backspace does — through the live
// document, not through the file.
func dropBlock(t *testing.T, s *EditServer, said string) {
	t.Helper()
	if _, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		out := docmodel.Doc{}
		for _, b := range model.Blocks {
			var text string
			for _, in := range b.Inlines {
				text += in.Text
			}
			if text == said {
				continue
			}
			out.Blocks = append(out.Blocks, b)
		}
		return out, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTheRailCarriesTheReviewersOwnEdits is the whole point: the rail is the
// list of what this round carries, and an edit is half of that. It showed
// instructions only, so the reviewer could not see the half of their own round
// that the agent is told about.
func TestTheRailCarriesTheReviewersOwnEdits(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n")
	dropBlock(t, s, "Beta two here.")

	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Changes) == 0 {
		t.Fatal("the reviewer deleted a paragraph and the pending view reports no changes at all")
	}
	var removed []string
	for _, c := range view.Changes {
		if c.Kind == "removed" {
			removed = append(removed, c.Before)
		}
	}
	if len(removed) != 1 || removed[0] != "Beta two here." {
		t.Errorf("want one removal naming the deleted paragraph, got %+v", view.Changes)
	}
}

// TestAnUntouchedDocumentReportsNoChanges — the list is what THIS round
// carries, so a document nobody has edited since the last round has an empty
// one. Without this, every poll would report the whole document as changed the
// moment the diff was pointed at the wrong baseline.
func TestAnUntouchedDocumentReportsNoChanges(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n")
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Changes) != 0 {
		t.Errorf("an untouched document reports %d changes: %+v", len(view.Changes), view.Changes)
	}
}

// TestTheAgentsWorkIsNeverReportedAsTheReviewersHand is the guard, and it is
// the same attribution inversion ReviewerChanges filters ReasonRevise to avoid,
// arriving through the other door.
//
// While a handoff window is open the file belongs to the agent, its saves
// stream into the live document, and the browser is read-only — so a diff
// against the last version describes the AGENT's in-progress work. Reporting it
// in the reviewer's own list would tell them they had made edits they never
// made, and offer to revert them.
func TestTheAgentsWorkIsNeverReportedAsTheReviewersHand(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n")
	dropBlock(t, s, "Beta two here.")
	if view, err := s.pending(); err != nil || len(view.Changes) == 0 {
		t.Fatalf("the fixture produced no change to hide: %v %+v", err, view.Changes)
	}

	s.handoffLive.Store(true)
	defer s.handoffLive.Store(false)
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Changes) != 0 {
		t.Errorf("mid-handoff the rail reported %d change(s) as the reviewer's: %+v — those are the agent's saves",
			len(view.Changes), view.Changes)
	}
}

// TestRevertingAnEditThroughTheEndpoint is the whole verb, end to end: the
// reviewer deletes a paragraph, the rail offers it back, and pressing revert
// restores it without touching anything else.
func TestRevertingAnEditThroughTheEndpoint(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n\nGamma three here.\n")
	dropBlock(t, s, "Beta two here.")

	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for _, c := range view.Changes {
		if c.Kind == "removed" && c.Before == "Beta two here." {
			key = c.Key
		}
	}
	if key == "" {
		t.Fatalf("the removal is not addressable: %+v", view.Changes)
	}

	if rec := postRec(t, s, "/_galley/revert", map[string]any{"key": key}); rec.Code >= 300 {
		t.Fatalf("revert: %d %s", rec.Code, rec.Body.String())
	}
	model, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	got := string(markdown.Serialize(model))
	if !strings.Contains(got, "Beta two here.") {
		t.Errorf("the reverted paragraph did not come back:\n%s", got)
	}
	if !strings.Contains(got, "Gamma three here.") {
		t.Errorf("reverting took something else with it:\n%s", got)
	}
	after, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Changes) != 0 {
		t.Errorf("the reverted edit is still listed as pending: %+v", after.Changes)
	}
}

// TestAStaleCardIsRefusedRatherThanGuessed. The browser sends a key and nothing
// else, and the server recomputes — so a card left open while the document
// moved cannot revert text the server never computed.
func TestAStaleCardIsRefused(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n")
	dropBlock(t, s, "Beta two here.")
	rec := postRec(t, s, "/_galley/revert", map[string]any{"key": "ch-deadbeefdeadbeef"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("a key the server does not recognise got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestAnInstructionIsNotAnEdit is the bug the undo gate found by being made to
// prove its own precondition.
//
// Filing an instruction writes a `{==…==}` highlight, with its
// `{>>@comment …<<}` ID mark, into the document, so a
// diff of the last version against the live model reported the highlight as
// something the reviewer had CHANGED — and the rail listed it as an edit,
// directly beneath the instruction card it is. One thing, twice, on the one
// surface that exists so there is only one list. It was also offered a revert,
// which would have "put back" prose that never moved by deleting the anchor of
// an instruction just written.
func TestAnInstructionIsNotAnEdit(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n")
	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Beta two here.", "text": "tighten this",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 1 {
		t.Fatalf("the fixture filed %d instructions", len(view.Instructions))
	}
	if len(view.Changes) != 0 {
		t.Errorf("filing an instruction was reported as %d hand edit(s): %+v — "+
			"that is the instruction's own highlight, listed twice on one surface",
			len(view.Changes), view.Changes)
	}
}

// TestAnEditBesideAnInstructionIsStillReported — the fix must not silence real
// edits made in the same round as an instruction, which is the obvious way to
// over-correct it.
func TestAnEditBesideAnInstructionIsStillReported(t *testing.T) {
	s := editServerWith(t, "# T\n\nAlpha one here.\n\nBeta two here.\n\nGamma three here.\n")
	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "Beta two here.", "text": "tighten this",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	dropBlock(t, s, "Gamma three here.")

	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	var removed int
	for _, c := range view.Changes {
		if c.Kind == "removed" && c.Before == "Gamma three here." {
			removed++
		}
	}
	if removed != 1 {
		t.Errorf("a real deletion alongside an instruction was not reported: %+v", view.Changes)
	}
	if len(view.Changes) != 1 {
		t.Errorf("want exactly the one real edit, got %+v", view.Changes)
	}
}
