package serve

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/ledger"
	"github.com/subaud-tools/galley/internal/review"
)

type capture struct {
	mu   sync.Mutex
	recs []ledger.Record
}

func (c *capture) log(_ string, rec ledger.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, rec)
	return nil
}

func (c *capture) records() []ledger.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ledger.Record(nil), c.recs...)
}

func TestReviseRecordsReviewerInstructionsOnTheirRound(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "The retryBudget controls retries.\n")
	c := &capture{}
	s.Ledger = ledger.NewRecorder(c.log)
	s.OnRevise = "true"
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	if rec := post(t, h, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "retryBudget", "text": "Spell this out.", "author": review.AuthorCourt,
	}); rec.Code != http.StatusOK {
		t.Fatalf("instruction: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, h, "/_galley/revise", map[string]any{}); rec.Code != http.StatusNoContent {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	if !s.Ledger.Flush(10 * time.Second) {
		t.Fatal("ledger did not drain")
	}

	var instruction *ledger.Record
	for _, rec := range c.records() {
		if rec.Kind == ledger.KindInstruction {
			r := rec
			instruction = &r
		}
	}
	if instruction == nil || instruction.Round == 0 || instruction.Text != "Spell this out." ||
		instruction.Quote != "retryBudget" || instruction.Author != ledger.AuthorReviewer {
		t.Fatalf("instruction = %+v", instruction)
	}
}
