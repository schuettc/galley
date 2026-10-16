package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/suggest"
	"github.com/subaud-tools/galley/internal/versions"
	"github.com/subaud-tools/galley/internal/ydoc"
)

// applied_test.go drives PHASE 2's WHOLE LOOP through the real endpoints: the
// reviewer marks up and presses Revise, the agent revises through the real write
// surface, the round returns. The three things it asserts are the three things
// the phase claims — the document changed, the version was cut, and the pairing
// reads.

// agentEdits is the agent's real write surface now: save the .md, and the
// handoff window's import loads it into the live document. importDraft is the
// watcher's own tick, called synchronously so the test needs no sleep.
func agentEdits(t *testing.T, s *EditServer, content string) {
	t.Helper()
	if err := os.WriteFile(s.MdPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if imported, err := s.importDraft(); err != nil || !imported {
		t.Fatalf("import: imported=%v err=%v", imported, err)
	}
}

func ackAs(t *testing.T, s *EditServer, state, note string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"state": state, "note": note})
	req := httptest.NewRequest(http.MethodPost, "/_galley/ack", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ack %s answered %d: %s", state, rec.Code, rec.Body.String())
	}
}

func liveText(t *testing.T, s *EditServer) string {
	t.Helper()
	model, err := ydoc.ReadLive(s.doc)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, blk := range model.Blocks {
		for _, in := range blk.Inlines {
			b.WriteString(in.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// THE ROUND, END TO END. A reviewer's instruction, an applied revision, and the
// round that comes back — asserted on the document, on the version, and on the
// pairing, which are the three claims phase 2 makes and each of which can be
// true while the others are false.
func TestAnAppliedRoundChangesTheDocumentAndCutsOneVersion(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe retryBudget controls retries.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	// The reviewer marks up: a comment IS the instruction.
	commentAs(t, s, "retryBudget", "spell this out, it reads as code")
	press(t, s, "{}")
	asked := rounds(t, s)
	if len(asked) != 2 {
		t.Fatalf("the press cut %d rounds, want 2", len(asked))
	}
	if !strings.Contains(asked[1].Instruction, "spell this out") {
		t.Fatalf("the reviewer's round did not carry the instruction: %q", asked[1].Instruction)
	}

	// The agent revises: on the real write surface, which is the FILE. Two
	// saves in one round, which is the ordinary shape and the shape a
	// per-write cut gets wrong.
	agentEdits(t, s, "# Title\n\nThe retry budget controls retries.\n")
	agentEdits(t, s, "# Title\n\nThe retry budget controls retries. It defaults to three.\n")

	// THE DOCUMENT CHANGED, and nothing is waiting on a decision.
	if got := liveText(t, s); !strings.Contains(got, "The retry budget controls retries. It defaults to three.") {
		t.Errorf("the revision is not in the document: %q", got)
	}
	model, err := ydoc.ReadLive(s.doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range suggest.List(model) {
		if p.Kind.Decidable() {
			t.Errorf("an applied revision left something to decide: %+v", p)
		}
	}

	// The agent returns. THAT is the send.
	ackAs(t, s, "answered", "tightened the retry paragraph")

	rs := rounds(t, s)
	if len(rs) != 3 {
		t.Fatalf("the applied round cut %d rounds, want 3 (opened, revise, landed): %+v", len(rs), rs)
	}
	landed := rs[2]
	if landed.Reason != versions.ReasonLanded {
		t.Errorf("the agent's round was cut for %q, want %q", landed.Reason, versions.ReasonLanded)
	}
	if versions.Authors(landed.Authors) != versions.AuthorAgent {
		t.Errorf("the applied round is authored %q", versions.Authors(landed.Authors))
	}
	// THE PAIRING: what it was asked, and what it did.
	if landed.Answers != asked[1].N {
		t.Errorf("the agent's round answers v%d, want v%d", landed.Answers, asked[1].N)
	}
	if !strings.Contains(landed.Instruction, "tightened the retry paragraph") {
		t.Errorf("the round did not carry the agent's ack note: %q", landed.Instruction)
	}
	// AND IT IS THE FILE.
	onDisk, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.Versions().Content(landed.N)
	if err != nil {
		t.Fatal(err)
	}
	if body != string(onDisk) {
		t.Errorf("the version and the file disagree:\nversion %q\nfile    %q", body, onDisk)
	}
}

// REVISION AND LIVE ARE TWO TRIGGERS FOR ONE EXCHANGE. The reviewer round is
// clean in both, the agent can acknowledge both, and the landing points back
// at the round that carried the instruction. This is the regression for the
// live path cutting `{==**Revise**==}` as a reviewer change and then refusing
// the agent's terminal ack because only the button had opened a watch.
func TestReviseAndLiveSendTheSameRound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		send   func(*testing.T, *EditServer)
	}{
		{
			name:   "revise",
			reason: versions.ReasonRevise,
			send: func(t *testing.T, s *EditServer) {
				t.Helper()
				s.OnRevise = "true"
				press(t, s, "{}")
			},
		},
		{
			name:   "live",
			reason: versions.ReasonSettled,
			send: func(t *testing.T, s *EditServer) {
				t.Helper()
				fp, _, err := s.waitFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				s.wakeSettle(fp)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := newEditServer(t, dir, "doc.md", "# Title\n\nPressing **Revise** sends the round.\n")
			defer func() { _ = s.Close() }()

			commentAs(t, s, "Revise", "remove bold")
			tc.send(t, s)

			rs := rounds(t, s)
			if len(rs) != 2 {
				t.Fatalf("the reviewer send cut %d rounds, want opened + reviewer: %+v", len(rs), rs)
			}
			asked := rs[1]
			if asked.Reason != tc.reason {
				t.Errorf("reviewer round reason = %q, want %q", asked.Reason, tc.reason)
			}
			if asked.Instruction != "remove bold" {
				t.Errorf("reviewer round instruction = %q, want remove bold", asked.Instruction)
			}
			if versions.Authors(asked.Authors) != versions.AuthorReviewer {
				t.Errorf("reviewer round authors = %q", versions.Authors(asked.Authors))
			}
			body, err := s.Versions().Content(asked.N)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(body, "{==") || !strings.Contains(body, "**Revise**") {
				t.Errorf("reviewer version is not clean, formatted prose: %q", body)
			}
			if changed := s.changedRegions(rs[0].N, asked.N); changed != 0 {
				t.Errorf("an instruction-only reviewer round reports %d content changes, want 0", changed)
			}

			pending, err := s.pending()
			if err != nil {
				t.Fatal(err)
			}
			if len(pending.Instructions) != 0 {
				t.Errorf("sent instructions remain pending: %+v", pending.Instructions)
			}

			agentEdits(t, s, "# Title\n\nPressing Revise sends the round.\n")
			ackAs(t, s, "answered", "removed the requested emphasis")

			rs = rounds(t, s)
			if len(rs) != 3 {
				t.Fatalf("the exchange cut %d rounds, want opened + reviewer + agent: %+v", len(rs), rs)
			}
			landed := rs[2]
			if landed.Reason != versions.ReasonLanded || landed.Answers != asked.N {
				t.Errorf("agent round = %+v, want landed answering v%d", landed, asked.N)
			}
			if versions.Authors(landed.Authors) != versions.AuthorAgent {
				t.Errorf("agent round authors = %q", versions.Authors(landed.Authors))
			}
			body, err = s.Versions().Content(landed.N)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(body, "**Revise**") || !strings.Contains(body, "Pressing Revise sends") {
				t.Errorf("agent version did not remove only the bold styling: %q", body)
			}
			if changed := s.changedRegions(asked.N, landed.N); changed != 1 {
				t.Errorf("the agent formatting revision reports %d changes, want 1", changed)
			}
		})
	}
}

// SEVERAL SAVES ARE ONE ROUND. Cutting per write would commit a round holding
// one sixth of the answer and leave the rest to be swept into whoever pressed
// next — see versions.go's appliedRound.
func TestSeveralSavesThroughTheExchangeAreOneRound(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nAlpha beta gamma delta.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	press(t, s, "{}")

	for _, body := range []string{
		"# Title\n\nOne beta gamma delta.\n",
		"# Title\n\nOne two gamma delta.\n",
		"# Title\n\nOne two three delta.\n",
		"# Title\n\nOne two three four.\n",
	} {
		agentEdits(t, s, body)
		if err := s.Project(); err != nil {
			t.Fatal(err)
		}
	}
	if rs := rounds(t, s); len(rs) != 2 {
		t.Fatalf("four saves cut %d rounds before the agent returned, want 2 (opened, revise)", len(rs))
	}
	ackAs(t, s, "answered", "renumbered the list")
	rs := rounds(t, s)
	if len(rs) != 3 {
		t.Fatalf("the return cut %d rounds, want 3", len(rs))
	}
	body, err := s.Versions().Content(rs[2].N)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "One two three four.") {
		t.Errorf("the round does not hold the whole revision: %q", body)
	}
}

// AN AGENT THAT NEVER RETURNS STILL GETS ITS ROUND, and it gets it before the
// send that overtook it — otherwise its work is recorded as the reviewer's.
func TestAnAgentThatNeverAcksStillGetsItsRoundFirst(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nOne sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	press(t, s, "{}")

	agentEdits(t, s, "# Title\n\nOne better sentence.\n")
	// No ack. The reviewer presses again — the first press's command has to have
	// exited first, since a second press over one in flight is single-flighted.
	if done, running := s.ReviseInFlight(); running {
		<-done
	}
	press(t, s, "{}")

	rs := rounds(t, s)
	if len(rs) != 4 {
		t.Fatalf("cut %d rounds, want 4 (opened, revise, landed, revise): %+v", len(rs), rs)
	}
	if rs[2].Reason != versions.ReasonLanded {
		t.Errorf("v%d was cut for %q, want the agent's landing first", rs[2].N, rs[2].Reason)
	}
	// The agent never acked, so there is no note to carry — the note is the
	// terminal ack's now. What must survive is the WORK and its author.
	body, err := s.Versions().Content(rs[2].N)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "One better sentence.") {
		t.Errorf("the recovered round lost the agent's work: %q", body)
	}
	if versions.Authors(rs[2].Authors) != versions.AuthorAgent {
		t.Errorf("the recovered round is authored %q — the agent's work must not read as the reviewer's",
			versions.Authors(rs[2].Authors))
	}
}

// THE EXCEPTION IS A ROUND, AND IT IS VISIBLY NOT A REVISION: same bytes, its
// own reason, the words in the instruction column where every other round's
// words are.
func TestTheExceptionIsARoundOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nOne sentence.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"
	press(t, s, "{}")

	raw, _ := json.Marshal(map[string]any{"why": "the API this describes is not in the branch I have"})
	req := httptest.NewRequest(http.MethodPost, "/_galley/cannot", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cannot answered %d: %s", rec.Code, rec.Body.String())
	}

	rs := rounds(t, s)
	if len(rs) != 3 {
		t.Fatalf("the exception cut %d rounds, want 3: %+v", len(rs), rs)
	}
	got := rs[2]
	if got.Reason != versions.ReasonCouldNot {
		t.Errorf("the exception was cut for %q, want %q", got.Reason, versions.ReasonCouldNot)
	}
	if !strings.Contains(got.Instruction, "not in the branch I have") {
		t.Errorf("the exception does not carry its reason: %q", got.Instruction)
	}
	// SAME BYTES. Nothing happened, and that is the fact being recorded.
	before, err := s.Versions().Content(rs[1].N)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Versions().Content(got.N)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("an exception moved the document:\nv%d %q\nv%d %q", rs[1].N, before, got.N, after)
	}
	// AND IT REACHES THE REVIEWER'S OWN SURFACE.
	req = httptest.NewRequest(http.MethodGet, "/_galley/revise", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var state map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if why, _ := state["cannot"].(string); !strings.Contains(why, "not in the branch") {
		t.Errorf("the reviewer's own readout says nothing about it: %v", state["cannot"])
	}
	if n, _ := state["landed"].(float64); int(n) != got.N {
		t.Errorf("the arrival number is %v, want v%d", state["landed"], got.N)
	}
}

// A REPORT WITH NO REASON IS REFUSED. The answer to an exception is a different
// instruction, and there is nothing to write one from.
func TestTheExceptionNeedsAReason(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nOne sentence.\n")
	defer func() { _ = s.Close() }()
	req := httptest.NewRequest(http.MethodPost, "/_galley/cannot", strings.NewReader(`{"why":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a reasonless report answered %d, want 400", rec.Code)
	}
}

// PHASE 2 DELETES NOTHING. suggest still proposes, accept still accepts, and the
// two verbs live side by side in one document.
func TestTheReviewersWordsAreRecordedOnceAndNotOnEveryRoundAfter(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe retryBudget controls retries.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "retryBudget", "spell this out")
	press(t, s, "{}")
	if done, running := s.ReviseInFlight(); running {
		<-done
	}
	// The agent revises. NOTHING RESOLVES THE THREAD — that is the phase.
	agentEdits(t, s, "# Title\n\nThe retry budget controls retries.\n")
	ackAs(t, s, "answered", "done")
	// The reviewer presses again with nothing new to say.
	press(t, s, "{}")

	rs := rounds(t, s)
	var carried []int
	for _, r := range rs {
		if strings.Contains(r.Instruction, "spell this out") {
			carried = append(carried, r.N)
		}
	}
	if len(carried) != 1 {
		t.Errorf("the reviewer's words are on rounds %v — an instruction belongs to the round that carried it", carried)
	}

	// AND SOMETHING NEW IS STILL RECORDED. The suppression is of the repetition,
	// not of the conversation. The empty press above re-opened the agent's
	// window, and a window locks the reviewer's endpoints — so the reviewer
	// takes the document back first, which is the new workflow's own shape.
	cancelHandoff(t, s)
	commentAs(t, s, "controls retries", "and say where the default lives")
	if done, running := s.ReviseInFlight(); running {
		<-done
	}
	press(t, s, "{}")
	last := rounds(t, s)
	got := last[len(last)-1]
	if !strings.Contains(got.Instruction, "where the default lives") {
		t.Errorf("a new instruction was swallowed with the old one: %q", got.Instruction)
	}
	if strings.Contains(got.Instruction, "spell this out") {
		t.Errorf("the round repeated an instruction already recorded: %q", got.Instruction)
	}
}

// ackWithChanges is ackAs plus the reply manifest — the agent's testimony about
// the round it just landed. Kept beside ackAs rather than folded into it so the
// tests that do not care about the manifest read as they did.
func ackWithChanges(t *testing.T, s *EditServer, state, note string, changes []map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"state": state, "note": note, "changes": changes})
	req := httptest.NewRequest(http.MethodPost, "/_galley/ack", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ack %s answered %d: %s", state, rec.Code, rec.Body.String())
	}
}
