package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/versions"
)

func TestDeletingAnUnsentInstructionRemovesItsCardAndTrace(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nThe retry budget is explicit.\n")
	defer func() { _ = s.Close() }()
	commentAs(t, s, "retry budget", "Make this concrete.")

	view, err := s.pending()
	if err != nil || len(view.Instructions) != 1 {
		t.Fatalf("pending instruction = %+v, %v", view.Instructions, err)
	}
	rec := post(t, s.Handler(), "/_galley/instruction/delete", map[string]any{
		"key": view.Instructions[0].Key,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete answered %d: %s", rec.Code, rec.Body.String())
	}
	view, err = s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instructions) != 0 {
		t.Fatalf("instruction survived delete: %+v", view.Instructions)
	}
	if got := liveText(t, s); !strings.Contains(got, "The retry budget is explicit.") {
		t.Fatalf("deleting the instruction changed its prose: %q", got)
	}
}

func TestStartingVersionIsRenderedAsABaseline(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nStarting prose.\n")
	defer func() { _ = s.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/_galley/versions/view?to=1&view=inplace", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("view answered %d: %s", rec.Code, rec.Body.String())
	}
	var got diffView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.From != 0 || got.View != "clean" || got.Regions != 0 {
		t.Fatalf("v1 is not a clean baseline: %+v", got)
	}
	if strings.Contains(got.HTML, "gly-ins") || !strings.Contains(got.HTML, "Starting prose") {
		t.Fatalf("v1 rendered as an insertion rather than a document: %s", got.HTML)
	}
}

func TestLegacyInstructionCarriersAreNotDocumentChanges(t *testing.T) {
	for name, legacy := range map[string]string{
		"a bare highlight": "# Title\n\nPressing {==**Revise**==} sends the round.\n",
		// The carrier galley writes now: the highlight AND its ID mark.
		"a highlight with its ID mark": "# Title\n\nPressing {==**Revise**==}{>>@comment cm-0123456789abcdef<<} sends the round.\n",
		// A block comment's carrier: its ID mark on a line of its own.
		"a block comment's ID mark": "# Title\n\nPressing **Revise** sends the round.\n\n{>>@comment cb-0123456789abcdef<<}\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nPressing **Revise** sends the round.\n")
			defer func() { _ = s.Close() }()

			round, err := s.Versions().Commit(legacy, versions.Round{
				Reason: versions.ReasonSettled, Instruction: "remove bold",
			})
			if err != nil {
				t.Fatal(err)
			}
			if changed := s.changedRegions(1, round.N); changed != 0 {
				t.Fatalf("legacy instruction anchor reports %d document changes, want 0", changed)
			}

			req := httptest.NewRequest(http.MethodGet, "/_galley/versions/view?to=2&view=inplace", nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("view answered %d: %s", rec.Code, rec.Body.String())
			}
			var got diffView
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Regions != 0 || strings.Contains(got.HTML, "{==") || strings.Contains(got.HTML, "==}") ||
				strings.Contains(got.HTML, "@comment") {
				t.Fatalf("legacy instruction syntax leaked into History: %+v", got)
			}
		})
	}
}

func TestRestoreCreatesADraftWithoutRewritingHistory(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nOriginal sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "Original sentence", "Rewrite this.")
	press(t, s, "{}")
	agentEdits(t, s, "# Title\n\nRevised sentence.\n")
	ackAs(t, s, "answered", "done")
	before := len(rounds(t, s))

	rec := post(t, s.Handler(), "/_galley/versions/restore", map[string]any{"version": 1})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("restore answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := liveText(t, s); !strings.Contains(got, "Original sentence") || strings.Contains(got, "Revised sentence") {
		t.Fatalf("v1 was not restored into the draft: %q", got)
	}
	if after := len(rounds(t, s)); after != before {
		t.Fatalf("restore rewrote history: %d rounds became %d", before, after)
	}
}

func TestRestoreRefusesToDiscardCurrentInstructions(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nKeep this.\n")
	defer func() { _ = s.Close() }()
	commentAs(t, s, "Keep this", "Explain why.")
	rec := post(t, s.Handler(), "/_galley/versions/restore", map[string]any{"version": 1})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "send or delete") {
		t.Fatalf("restore with unsent work answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReviseAndApproveSealsOnlyAfterSuccessfulAnswer(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nDraft sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "Draft sentence", "Make it final.")
	press(t, s, `{"approveOnAnswer":true}`)
	if s.Sealed() {
		t.Fatal("Revise & Approve sealed before the agent answered")
	}
	agentEdits(t, s, "# Title\n\nFinal sentence.\n")
	ackAs(t, s, "answered", "done")
	if !s.Sealed() {
		t.Fatal("a successful entrusted answer did not approve the review")
	}
	rs := rounds(t, s)
	if got := rs[len(rs)-1].Reason; got != "verdict" {
		t.Fatalf("last round reason = %q, want verdict", got)
	}
}

// TestReviseAndApproveWithNoInstructionsApprovesNow pins channels-review's bug:
// Revise & Approve on a round with no instructions used to hang the review open
// forever. The "then approve" half rode on the agent acking a successful
// answer, and an empty round gives the agent nothing to apply, so the ack never
// came and no verdict was recorded. With nothing to hand over there is nothing
// to wait on, so the verdict lands at once.
func TestReviseAndApproveWithNoInstructionsApprovesNow(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nDraft sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	// No instruction filed: the trust exit has nothing to hand the agent.
	press(t, s, `{"approveOnAnswer":true}`)
	if !s.Sealed() {
		t.Fatal("Revise & Approve with no instructions never approved — the verdict was dropped")
	}
	rs := rounds(t, s)
	if got := rs[len(rs)-1].Reason; got != "verdict" {
		t.Fatalf("last round reason = %q, want verdict", got)
	}
}

func TestReviseAndApproveLeavesCannotOpen(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nDraft sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "Draft sentence", "Make it final.")
	press(t, s, `{"approveOnAnswer":true}`)
	rec := post(t, s.Handler(), "/_galley/cannot", map[string]any{"why": "the source is missing"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cannot answered %d: %s", rec.Code, rec.Body.String())
	}
	if s.Sealed() {
		t.Fatal("a cannot report approved the review")
	}
}

func TestReviseAndApproveLeavesFailedAnswerOpen(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nDraft sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "Draft sentence", "Make it final.")
	press(t, s, `{"approveOnAnswer":true}`)
	ackAs(t, s, "failed", "the edit could not be applied")
	if s.Sealed() {
		t.Fatal("a failed answer approved the review")
	}
}

// TestHistoryViewJoinsTestimonyToTheRegionsItNames is the whole of Task 5.0 in
// one round: the reviewer asks three things, the agent lands two changes and
// testifies about them, and the view the reading state reads must pair each
// note with the region its quotation actually falls in — re-matched here, from
// the two documents, exactly as the ack matched it.
func TestHistoryViewJoinsTestimonyToTheRegionsItNames(t *testing.T) {
	const doc = "# Retries\n\nThe retry budget is explicit.\n\n## Timeouts\n\nThe timeout is generous.\n"
	s := newEditServer(t, t.TempDir(), "doc.md", doc)
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "The retry budget is explicit.", "Give the budget a number.")
	commentAs(t, s, "The timeout is generous.", "Say how long.")
	commentAs(t, s, "Retries", "Rename this heading.")

	view, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	key := map[string]string{}
	for _, in := range view.Instructions {
		key[in.Text] = in.Key
	}
	if len(key) != 3 {
		t.Fatalf("pending instructions = %+v", view.Instructions)
	}

	press(t, s, "{}")
	agentEdits(t, s, "# Retries\n\nThe retry budget is 12 attempts over 48 hours.\n\n## Timeouts\n\nThe timeout is 30 seconds.\n")
	ackWithChanges(t, s, "answered", "done", []map[string]any{
		{
			"quote":   "12 attempts over 48 hours",
			"answers": []string{key["Give the budget a number."]},
			"note":    "Added the 12-attempt budget.",
		},
		{"quote": "The timeout is 30 seconds.", "note": "Made the timeout concrete."},
	})

	rs := rounds(t, s)
	landed := rs[len(rs)-1].N
	req := httptest.NewRequest(http.MethodGet, "/_galley/versions/view?to="+strconv.Itoa(landed)+"&view=inplace", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("view answered %d: %s", rec.Code, rec.Body.String())
	}
	var got diffView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Regions != 2 {
		t.Fatalf("regions = %d, want 2: %s", got.Regions, got.HTML)
	}
	// THE BROWSER MUST BE ABLE TO ADDRESS A REGION. The ids are this render's
	// ordinals and nothing else; the count must not have moved to get them.
	for k := 0; k < got.Regions; k++ {
		if want := `data-gly-region="` + strconv.Itoa(k) + `"`; !strings.Contains(got.HTML, want) {
			t.Fatalf("rendered diff carries no %s: %s", want, got.HTML)
		}
	}

	byNote := map[string]changeView{}
	var askOnly []changeView
	for _, c := range got.Changes {
		if c.Region < 0 {
			askOnly = append(askOnly, c)
			continue
		}
		byNote[c.Note] = c
	}
	budget, ok := byNote["Added the 12-attempt budget."]
	if !ok {
		t.Fatalf("the agent's note did not reach the view: %+v", got.Changes)
	}
	if budget.Region != 0 || budget.Place != "retries" {
		t.Fatalf("budget note joined to %+v, want region 0 under retries", budget)
	}
	if len(budget.Asks) != 1 || budget.Asks[0] != "Give the budget a number." {
		t.Fatalf("budget note carries asks %+v, want the ask it named", budget.Asks)
	}
	timeout, ok := byNote["Made the timeout concrete."]
	if !ok {
		t.Fatalf("the unattributed note did not reach the view: %+v", got.Changes)
	}
	if timeout.Region != 1 || timeout.Place != "timeouts" {
		t.Fatalf("timeout note joined to %+v, want region 1 under timeouts", timeout)
	}
	if len(timeout.Asks) != 0 {
		t.Fatalf("a change naming no ask was given one: %+v", timeout.Asks)
	}
	// NOTHING THE REVIEWER SENT MAY DISAPPEAR. Two asks went unnamed and both
	// must come back placeless rather than not at all.
	unanswered := map[string]bool{}
	for _, c := range askOnly {
		for _, a := range c.Asks {
			unanswered[a] = true
		}
	}
	if len(askOnly) != 2 || !unanswered["Say how long."] || !unanswered["Rename this heading."] {
		t.Fatalf("asks nobody answered vanished from the view: %+v", got.Changes)
	}
}

// TestAStaleLocatorRendersItsChangeBareAndReturnsItsAsk is the other half of the
// re-match: testimony whose quotation is no longer in the diff is believed by
// nobody. The round is committed directly because a manifest this shape cannot
// survive the ack — which is the point, since what is stored can still go stale
// under a restore, a hand-edited store, or a build that matched differently.
func TestAStaleLocatorRendersItsChangeBareAndReturnsItsAsk(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nThe timeout is generous.\n")
	defer func() { _ = s.Close() }()

	asking, err := s.Versions().Commit("# Title\n\nThe timeout is generous.\n", versions.Round{
		Reason:      versions.ReasonRevise,
		Instruction: "Say how long.",
		Asks:        []versions.Ask{{Key: "k1", Text: "Say how long."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	landed, err := s.Versions().Commit("# Title\n\nThe timeout is 30 seconds.\n", versions.Round{
		Reason:  versions.ReasonLanded,
		Answers: asking.N,
		Changes: []versions.Change{{
			Locator: "a sentence nobody ever wrote",
			Answers: []string{"k1"},
			Note:    "Made the timeout concrete.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_galley/versions/view?to="+strconv.Itoa(landed.N)+"&view=inplace", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("view answered %d: %s", rec.Code, rec.Body.String())
	}
	var got diffView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// THE CHANGE IS STILL DRAWN, AND IT IS DRAWN BARE — which is what this
	// test's own name says, and what the assertion underneath it did not check.
	// It required the join to come back with ONE entry, which read the stale
	// locator as a reason to draw NOTHING for a region that had plainly
	// changed. The server owns the list of what changed; the agent's testimony
	// only annotates it, so testimony that cannot be placed costs the SENTENCE
	// and never the card.
	var bare, placeless int
	for _, c := range got.Changes {
		switch {
		case c.Region >= 0:
			bare++
			if c.Note != "" {
				t.Errorf("a stale locator's note reached the paper anyway: %+v", c)
			}
		default:
			placeless++
			if len(c.Asks) != 1 || c.Asks[0] != "Say how long." {
				t.Errorf("the ask did not come back placeless: %+v", c)
			}
		}
	}
	if bare != got.Regions {
		t.Errorf("%d regions changed but %d were drawn: %+v", got.Regions, bare, got.Changes)
	}
	if placeless != 1 {
		t.Errorf("want the unclaimed ask back exactly once, got %d: %+v", placeless, got.Changes)
	}
}

// viewOf reads one round's reading-state view through the real endpoint, which
// is the only reading of it the browser has.
func viewOf(t *testing.T, s *EditServer, to int) diffView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/_galley/versions/view?to="+strconv.Itoa(to)+"&view=inplace", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("view answered %d: %s", rec.Code, rec.Body.String())
	}
	var got diffView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// A ROUND THAT ANSWERED IS NOT A ROUND THAT REFUSED, AND AN ABSENT MANIFEST
// SAYS NOTHING ABOUT EITHER.
//
// joinTestimony walked the agent's manifest and nothing else, so a changed
// region the manifest never mentioned produced NO CARD AT ALL — and since no
// agent sends a manifest yet, that is every round in the product. Measured in a
// browser against a real revision: `regions: 1` with `changes:
// [{region: -1, asks: ["Tighten this."]}]` — the reviewer looking at their own
// edit on the paper, at a rail that had drawn nothing for it, under two cards
// reading ASK · NOT ANSWERED.
//
// Two claims were being confused. WHETHER AN ASK WAS ANSWERED is the ROUND's
// outcome — `could-not` is the reason a refusal has, and every other landing
// answered. WHICH CHANGE answered it is the manifest's, and it is a refinement.
// Reading the absence of the second as a negative on the first is this file's
// own recorded trap: the predicate is right and the sentence over it is a lie.
func TestAnUnattributedChangeStillGetsItsCard(t *testing.T) {
	const doc = "# Retries\n\nThe retry budget is explicit.\n"
	s := newEditServer(t, t.TempDir(), "doc.md", doc)
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "The retry budget is explicit.", "Give the budget a number.")
	press(t, s, "{}")
	agentEdits(t, s, "# Retries\n\nThe retry budget is 12 attempts.\n")
	// The ordinary case today: a terminal ack carrying NO manifest at all.
	ackAs(t, s, "answered", "tightened it")

	rounds := rounds(t, s)
	landed := rounds[len(rounds)-1]
	view := viewOf(t, s, landed.N)

	if view.Regions < 1 {
		t.Fatalf("the fixture changed nothing, so nothing can be asserted: %+v", view)
	}
	placed := 0
	for _, c := range view.Changes {
		if c.Region >= 0 {
			placed++
		}
	}
	if placed != view.Regions {
		t.Errorf("%d regions changed but %d carry a card — an unattributed change must still be drawn: %+v",
			view.Regions, placed, view.Changes)
	}
	if view.Refused {
		t.Errorf("a round that answered is not a refusal: reason=%q", landed.Reason)
	}
}

// AND A ROUND THAT REALLY DID REFUSE STILL SAYS SO. The wording is reserved for
// the one state that earns it, which is what makes it worth reading.
func TestARefusedRoundIsMarkedRefused(t *testing.T) {
	const doc = "# Retries\n\nThe retry budget is explicit.\n"
	s := newEditServer(t, t.TempDir(), "doc.md", doc)
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	commentAs(t, s, "The retry budget is explicit.", "Give the budget a number.")
	press(t, s, "{}")
	s.recordCannot("the number is not decided yet")
	rounds := rounds(t, s)
	landed := rounds[len(rounds)-1]
	if landed.Reason != versions.ReasonCouldNot {
		t.Fatalf("fixture did not produce a could-not round: %+v", landed)
	}
	if view := viewOf(t, s, landed.N); !view.Refused {
		t.Errorf("a could-not round must be marked refused: %+v", view)
	}
}
