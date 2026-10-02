package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/serve"
	"github.com/schuettc/galley/internal/versions"
)

// A COMMENT WITH A BLANK LINE IN IT IS READ BACK LINE BY LINE: every line in
// order, each indented two spaces, the blank one too (Open question 8 of the
// one-comment-system plan keeps formatInstructions' format as it is).
func linesOf(t *testing.T, out string, want ...string) {
	t.Helper()
	got := strings.Split(out, "\n")
	for i := 0; i+len(want) <= len(got); i++ {
		if strings.Join(got[i:i+len(want)], "\n") == strings.Join(want, "\n") {
			return
		}
	}
	t.Errorf("the lines %q are not in order in:\n%s", want, out)
}

func TestTheRoundReadBackKeepsEveryLine(t *testing.T) {
	out := roundText(versions.Round{N: 2, Reason: versions.ReasonRevise, At: time.Now().UTC(),
		Asks: []versions.Ask{ask("cd-aaa", "a\n\nb", "")}})
	linesOf(t, out, "[key cd-aaa]", "  a", "  ", "  b")
}

func TestOfflinePendingKeepsEveryLine(t *testing.T) {
	doc := writeDoc(t, t.TempDir(), "d.md", "# T\n\nCognito mints every token.\n")
	s, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	raw, err := json.Marshal(map[string]any{"op": "comment_document", "text": "a\n\nb"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/_galley/instruct", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("instruct answered %d", resp.StatusCode)
	}
	ts.Close()
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	view, err := offlinePending(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 1 || view.Instructions[0].Text != "a\n\nb" {
		t.Fatalf("offline pending = %+v, want the words with their blank line", view.Instructions)
	}
	linesOf(t, formatInstructions(view.Instructions), "  a", "  ", "  b")
}
