package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/ledger"
	"github.com/schuettc/galley/internal/versions"
)

// A COMMENT'S LINE BREAKS ARE ITS WORDS. Every stage a comment passes through
// keeps them: the unsent round, the round sent to the agent, the decision log
// and History. History used to be handed one `·`-joined sentence per round,
// which put every instruction on one line and every line of each on it too.

const brokenWords = "first line\n\nthird line"

func TestLineBreaksSurviveEveryStage(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	c := &capture{}
	s.Ledger = ledger.NewRecorder(c.log)
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": brokenWords})
	instructOK(t, s, map[string]any{"op": "comment_block", "target": blockKeyOf(t, s, "paragraph", "Cognito"), "text": brokenWords})
	instructOK(t, s, map[string]any{
		"op": "comment_block", "target": figureKey(t, s), "text": brokenWords,
		"region": map[string]any{"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.4},
	})
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "to be edited"})
	var docKey string
	for _, in := range pendingView(t, s).Instructions {
		if in.Anchor == "document" {
			docKey = in.Key
		}
	}
	instructOK(t, s, map[string]any{"op": "edit", "key": docKey, "text": "x\ny"})
	want := []string{brokenWords, brokenWords, brokenWords, "x\ny"}
	slices.Sort(want)

	texts := func(stage string, got []string) {
		t.Helper()
		got = slices.Clone(got)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s holds %q, want %q verbatim", stage, got, want)
		}
	}

	var pending []string
	for _, in := range pendingView(t, s).Instructions {
		pending = append(pending, in.Text)
	}
	texts("/_galley/pending", pending)

	reply := blockedWait(h, "", time.Hour)
	waitersRegistered(t, s, 1)
	if rec := post(t, h, "/_galley/revise", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	got := <-reply
	var waited []string
	if got.Pending != nil {
		for _, in := range got.Pending.Instructions {
			waited = append(waited, in.Text)
		}
	}
	texts("the waiter's payload", waited)

	rounds, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	var sent versions.Round
	for _, r := range rounds {
		if len(r.Asks) > 0 {
			sent = r
		}
	}
	var asked []string
	for _, a := range sent.Asks {
		asked = append(asked, a.Text)
	}
	texts("rounds.jsonl's asks", asked)

	if !s.Ledger.Flush(10 * time.Second) {
		t.Fatal("ledger did not drain")
	}
	var logged []string
	for _, rec := range c.records() {
		if rec.Kind == ledger.KindInstruction {
			logged = append(logged, rec.Text)
		}
	}
	texts("the decision log", logged)

	// AND THE ROUND THAT ANSWERS IT carries the same list, as what it was asked.
	if _, err := s.Versions().Commit(readMD(t, s), versions.Round{
		At: time.Now().UTC(), Reason: versions.ReasonLanded, Answers: sent.N,
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_galley/versions", nil))
	var view struct {
		Rounds []struct {
			N                 int      `json:"n"`
			Instructions      []string `json:"instructions"`
			AskedInstructions []string `json:"askedInstructions"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("/_galley/versions: %v %s", err, rec.Body.String())
	}
	var shown, answered []string
	for _, r := range view.Rounds {
		if r.N == sent.N {
			shown = r.Instructions
		}
		if len(r.AskedInstructions) > 0 {
			answered = r.AskedInstructions
		}
	}
	texts("/_galley/versions' instructions", shown)
	texts("/_galley/versions' askedInstructions", answered)
}
