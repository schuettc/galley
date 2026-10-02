package serve

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A restart of a page review keeps every unsent comment in its place. page.html
// never carries comment marks, so content.md is the only file that holds them:
// re-extracting it from the page at every open threw each place away and left
// only the words.
func TestAPageRestartKeepsEveryCommentPlaced(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	commentAs(t, s, "The reviewer highlights a sentence.", "cut this sentence")
	instructOK(t, s, map[string]any{
		"op": "comment_block", "target": blockKeyOf(t, s, "paragraph", "When the round comes back."), "text": "say more",
	})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	for _, in := range pendingView(t, s).Instructions {
		if in.Run == "" && in.AnchorKey == "" {
			t.Fatalf("precondition: %q is unplaced before the restart: %+v", in.Text, in)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage (restart): %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	view := pendingView(t, again).Instructions
	if len(view) != 2 {
		t.Fatalf("after a restart the rail lists %d instructions, want 2: %+v", len(view), view)
	}
	for _, in := range view {
		switch in.Text {
		case "cut this sentence":
			if in.Run == "" {
				t.Errorf("the text comment came back unplaced: %+v", in)
			}
		case "say more":
			if in.AnchorKey == "" {
				t.Errorf("the block comment came back unplaced: %+v", in)
			}
		default:
			t.Errorf("after a restart the rail lists an instruction nobody filed: %+v", in)
		}
	}
}

// A page changed outside galley between sessions is a different document: its
// prose is extracted afresh, and the unsent comments keep their words, unplaced.
func TestAPageChangedBetweenSessionsIsExtractedAfresh(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	commentAs(t, s, "The reviewer highlights a sentence.", "cut this sentence")
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	later := strings.Replace(fixturePage, "When the round comes back.", "A sentence written outside galley.", 1)
	if err := os.WriteFile(page, []byte(later), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	was := pageStderr
	pageStderr = &log
	t.Cleanup(func() { pageStderr = was })

	again, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage (restart): %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	content, err := os.ReadFile(filepath.Join(dir, ".galley", "pages", "page", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "A sentence written outside galley.") ||
		strings.Contains(string(content), "When the round comes back.") {
		t.Errorf("content.md was not extracted from the changed page:\n%s", content)
	}
	view := pendingView(t, again).Instructions
	if len(view) != 1 || view[0].Text != "cut this sentence" {
		t.Fatalf("after the page changed the rail lists %+v, want the one comment's words", view)
	}
	if view[0].Run != "" || view[0].AnchorKey != "" {
		t.Errorf("the comment is placed on a page that was extracted afresh: %+v", view[0])
	}
	if !strings.Contains(log.String(), "unplaced") {
		t.Errorf("the open does not say the unsent comments are kept unplaced:\n%s", log.String())
	}
}
