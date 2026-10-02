package serve

import (
	"net/http"
	"strings"
	"testing"
)

func TestRemovedWorkflowEndpointsAreGone(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "Hello world.\n")
	h := s.Handler()
	for _, path := range []string{
		"/_galley/suggest", "/_galley/accept", "/_galley/reject",
		"/_galley/accept-all", "/_galley/reject-all", "/_galley/sweep",
		"/_galley/decline", "/_galley/reply", "/_galley/resolve",
		"/_galley/delete", "/_galley/reopen", "/_galley/discard",
	} {
		if rec := post(t, h, path, map[string]any{}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestInstructionHasItsOwnEndpoint(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "Hello world.\n")
	rec := post(t, s.Handler(), "/_galley/instruct", map[string]any{
		"op": "comment", "target": "world", "text": "Be specific.", "author": "court",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("instruction: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInstructionRangeSelectsTheExactDuplicate(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "duplicate.md", "# T\n\nword then word.\n")
	defer func() { _ = s.Close() }()
	rec := post(t, s.Handler(), "/_galley/instruct", map[string]any{
		"op": "comment", "target": "word", "text": "Change the second one.",
		"author": "court", "path": []int{1}, "from": 10, "to": 14,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("range instruction answered %d: %s", rec.Code, rec.Body.String())
	}
	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 1 || view.Instructions[0].Quote != "word" {
		t.Fatalf("pending instructions = %+v, want the selected duplicate", view.Instructions)
	}
	if view.Instructions[0].Key == "" || view.Instructions[0].Run == "" {
		t.Fatalf("pending instruction lost its browser anchor: %+v", view.Instructions[0])
	}
	// THE SECOND "word", by its ID: the file says which one, and the key is
	// that ID.
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	want := "word then {==word==}{>>@comment " + view.Instructions[0].Key + "<<}."
	if md := readMD(t, s); !strings.Contains(md, want) {
		t.Fatalf("the file does not mark the second word with the instruction's ID: %q, want %q", md, want)
	}
}
