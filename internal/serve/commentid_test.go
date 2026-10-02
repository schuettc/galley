package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/unsent"
	"github.com/schuettc/galley/internal/ydoc"
)

// A SELECTED-TEXT COMMENT IS LINKED TO ITS PLACE BY ID ALONE. These tests file
// range comments the way the browser does and then move the document the way
// the reviewer does, through the CRDT.

// reviewerRewrites replaces old with new in every inline of the live document,
// keeping each inline's marks: the reviewer typing over a word inside a
// highlight. It is the browser's path, a ydoc.Write of an edited model under
// Apply, and not a server mutation, so no run is minted and no seed is taken.
func reviewerRewrites(t *testing.T, s *EditServer, old, new string) {
	t.Helper()
	before, err := s.readLive()
	if err != nil {
		t.Fatal(err)
	}
	after := docmodel.Clone(before)
	changed := false
	for i := range after.Blocks {
		for j := range after.Blocks[i].Inlines {
			in := &after.Blocks[i].Inlines[j]
			if strings.Contains(in.Text, old) {
				in.Text = strings.ReplaceAll(in.Text, old, new)
				changed = true
			}
		}
	}
	if !changed {
		t.Fatalf("the document does not say %q", old)
	}
	if err := s.yjs.Apply(t.Context(), s.Room, func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		ydoc.Write(doc, transact, before, after)
	}); err != nil {
		t.Fatal(err)
	}
}

// pendingView is /_galley/pending, read the way the rail reads it.
func pendingView(t *testing.T, s *EditServer) PendingView {
	t.Helper()
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func readMD(t *testing.T, s *EditServer) string {
	t.Helper()
	b, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const foxDoc = "# T\n\nthe quick fox jumps.\n\nSecond para here.\n"

// TestEditingAHighlightedWordKeepsTheInstruction: the reviewer
// changes a word their own comment highlights; the comment is about the same
// place, so it stays, placed. Linking by the highlighted words lost it.
func TestEditingAHighlightedWordKeepsTheInstruction(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?",
	})
	key := onlyKey(t, s)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	reviewerRewrites(t, s, "quick", "slow")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	view := pendingView(t, s)
	if len(view.Instructions) != 1 || view.Instructions[0].Key != key {
		t.Fatalf("after editing a highlighted word the rail lists %+v, want %s", view.Instructions, key)
	}
	if view.Instructions[0].Run == "" {
		t.Errorf("the instruction survived but is unplaced: %+v", view.Instructions[0])
	}
	if got := onlyKey(t, s); got != key {
		t.Errorf("pending.json holds %s, want %s", got, key)
	}
	if md := readMD(t, s); !strings.Contains(md, "the slow fox==}{>>@comment "+key+"<<}") {
		t.Errorf("the file lost the comment's mark: %q", md)
	}
}

// TestDeletingTheHighlightedWordsRemovesTheComment is "delete the text, delete
// the comment", now all the way down: gone from every surface AND from
// pending.json, so a restart does not bring it back.
func TestDeletingTheHighlightedWordsRemovesTheComment(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "which fox?",
	})
	onlyKey(t, s)
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	reviewerDeletes(t, s, "the quick fox")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}

	if got := keysOf(t, s); len(got) != 0 {
		t.Errorf("the instruction outlived the words it was about: %v", got)
	}
	if got := loadUnsent(t, s); len(got) != 0 {
		t.Errorf("pending.json still holds the retracted comment: %+v — a restart brings it back", got)
	}
}

// TestACommentWhoseMarkThisServerNeverSawIsNotSwept is the guard. No mark now
// is not evidence the reviewer deleted anything: this process never saw one.
func TestACommentWhoseMarkThisServerNeverSawIsNotSwept(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte(foxDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	c := unsent.Comment{
		Key: unsent.NewID(unsent.KindText), Kind: unsent.KindText, Text: "which fox?",
		Author: review.AuthorCourt, At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Quote: "the quick fox",
	}
	if err := unsent.Save(unsent.Path(md), unsent.File{Comments: []unsent.Comment{c}}); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for i := 0; i < 2; i++ {
		if err := s.Project(); err != nil {
			t.Fatal(err)
		}
	}

	if got := loadUnsent(t, s); len(got) != 1 || got[0].Key != c.Key {
		t.Fatalf("pending.json = %+v, want the comment this server never saw placed", got)
	}
	view := pendingView(t, s)
	if len(view.Instructions) != 1 || view.Instructions[0].Key != c.Key {
		t.Fatalf("the rail lists %+v, want %s", view.Instructions, c.Key)
	}
	if view.Instructions[0].Run != "" {
		t.Errorf("a comment with no mark in the file was placed: %+v", view.Instructions[0])
	}
}

var commentMark = regexp.MustCompile(`==\}\{>>@comment (cm-[0-9a-f]{16})<<\}`)

// TestACrossParagraphInstructionSurvivesARestartAsOne: one selection over three
// paragraphs is one comment, in the file (one ID on every piece) and after a
// restart (one instruction, one span).
func TestACrossParagraphInstructionSurvivesARestartAsOne(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "d.md",
		"# T\n\nFour Cognito behaviors shaped this.\n\nEach was measured rather than read.\n\nAnd each explains a decision above.\n")
	instructOK(t, s, map[string]any{
		"op": "comment", "path": []int{1}, "from": 5, "toPath": []int{3}, "to": 8, "text": "cut this run",
	})
	key := onlyKey(t, s)
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	md := readMD(t, s)
	marks := commentMark.FindAllStringSubmatch(md, -1)
	if len(marks) != 3 {
		t.Fatalf("the file has %d ID marks, want one after each of 3 pieces: %q", len(marks), md)
	}
	for _, m := range marks {
		if m[1] != key {
			t.Errorf("a piece carries %s, want %s: %q", m[1], key, md)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := NewEdit(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	view := pendingView(t, again)
	if len(view.Instructions) != 1 || view.Instructions[0].Key != key || view.Instructions[0].Run == "" {
		t.Fatalf("after a restart the rail lists %+v, want one placed instruction %s", view.Instructions, key)
	}
	model, err := again.readLive()
	if err != nil {
		t.Fatal(err)
	}
	var spans int
	for _, p := range suggest.List(model) {
		if p.Kind == suggest.KindComment {
			spans++
		}
	}
	if spans != 1 {
		t.Errorf("after a restart the document holds %d comment spans, want 1", spans)
	}
}

// TestARangeInstructionWithNoWordsIsRefused: a highlight with no comment would
// put a mark in the .md that pending.json has no comment for.
func TestARangeInstructionWithNoWordsIsRefused(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", foxDoc)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	before := readMD(t, s)
	rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "path": []int{1}, "from": 0, "to": 13, "text": "  ",
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "an instruction needs words") {
		t.Errorf("a range instruction with no words answered %d %q, want 400 \"an instruction needs words\"",
			rec.Code, rec.Body.String())
	}
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	if after := readMD(t, s); after != before {
		t.Errorf("a refused instruction changed the file:\n got %q\nwant %q", after, before)
	}
}
