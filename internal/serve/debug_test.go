package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/debug"
)

// recording is a debug sink that keeps what it was given, so a check can read
// the EVENTS rather than a count. A count is the proxy this repository has been
// bitten by six times: it goes green on a wiring that records the wrong thing.
type recording struct {
	mu     sync.Mutex
	events []debug.Event
	sink   *debug.Sink
}

func (r *recording) Write(p []byte) (int, error) {
	var ev debug.Event
	if err := json.Unmarshal(p, &ev); err != nil {
		return 0, err
	}
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	return len(p), nil
}

func (r *recording) of(kind debug.Kind) []debug.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []debug.Event
	for _, ev := range r.events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func (r *recording) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// newRecording installs a recording sink as the process-wide default for the
// life of the test and returns it.
func newRecording(t *testing.T) *recording {
	t.Helper()
	r := &recording{}
	r.sink = debug.NewSink(r, nil)
	restore := debug.SetDefault(r.sink)
	t.Cleanup(func() {
		restore()
		_ = r.sink.Close()
	})
	return r
}

// drain waits for the sink's queue, which is asynchronous on purpose: a debug
// write must never sit in front of a decision. Every assertion below runs after
// one.
func (r *recording) drain(t *testing.T) {
	t.Helper()
	if !r.sink.Flush(5 * time.Second) {
		t.Fatal("the debug sink did not drain")
	}
}

// TestDebugRecordsTheRoundTheAckAndTheRefusal drives a REAL review through a
// real server and reads the five facts the 2026-08-22 reconstruction could not
// get out of galley back off the debug log.
//
// EACH CLAUSE NAMES A FACT THAT WAS UNAVAILABLE, and they are asserted on the
// event's CONTENT rather than on its presence: a round event with no asks, an
// ack event with no manifest, or a refusal with no target would satisfy a
// count and answer none of the questions this exists for.
func TestDebugRecordsTheRoundTheAckAndTheRefusal(t *testing.T) {
	rec := newRecording(t)

	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe retryBudget controls retries.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "retryBudget", "spell this out, it reads as code")

	// AN ANCHOR THE SERVER REFUSES, with the target it refused. `findUnique`
	// answers `matched 0 times` and that sentence used to reach a toast in the
	// reviewer's browser and nowhere else.
	//
	// BEFORE the press, deliberately: once the round is handed over the draft
	// belongs to the agent and refuseHandoff answers first, so a refusal
	// attempted there never reaches the transform this is about.
	bad := post(t, s.Handler(), "/_galley/instruct", map[string]any{
		"op": "comment", "target": "a phrase that is not in this document", "text": "why?", "author": "court",
	})
	if bad.Code == http.StatusOK {
		t.Fatal("the server placed an anchor on text that is not in the document")
	}

	press(t, s, "{}")

	agentEdits(t, s, "# Title\n\nThe retry budget controls retries.\n")
	ackWithChanges(t, s, "answered", "tightened the retry paragraph", []map[string]any{
		{"quote": "The retry budget", "note": "spelled it out"},
	})
	rec.drain(t)

	// 1. WHAT THE ROUND CARRIED, from the side that sent it.
	sent := rec.of(debug.KindRoundSent)
	if len(sent) == 0 {
		t.Fatal("no round.sent event — the round handoff is not recorded")
	}
	last := sent[len(sent)-1]
	if len(last.Asks) == 0 {
		t.Fatalf("the round was recorded with no asks: %+v", last)
	}
	if !strings.Contains(last.Asks[0].Text, "spell this out") {
		t.Fatalf("the ask does not carry the reviewer's words: %+v", last.Asks[0])
	}
	if last.Asks[0].Quote != "retryBudget" {
		t.Fatalf("the ask does not carry the passage it was about: %+v", last.Asks[0])
	}
	if last.Asks[0].Key == "" {
		t.Fatalf("the ask has no key — the agent's answers name one: %+v", last.Asks[0])
	}
	if last.Round == 0 {
		t.Fatalf("the round has no number: %+v", last)
	}
	if last.Where != debugWhere {
		t.Fatalf("the event does not say which process wrote it: %+v", last)
	}
	if last.Doc != s.MdPath {
		t.Fatalf("the event does not say which document it is about: %+v", last)
	}

	// 2. WHAT THE ACK CARRIED — quote, note and the manifest, as posted.
	acks := rec.of(debug.KindAckReceived)
	if len(acks) != 1 {
		t.Fatalf("recorded %d acks, want 1", len(acks))
	}
	got := acks[0]
	if got.Ack == nil || got.Ack.State != "answered" {
		t.Fatalf("the ack's state is not recorded: %+v", got.Ack)
	}
	if got.Ack.Note != "tightened the retry paragraph" {
		t.Fatalf("the ack's note is not recorded: %+v", got.Ack)
	}
	if len(got.Ack.Changes) != 1 || got.Ack.Changes[0].Quote != "The retry budget" {
		t.Fatalf("the ack's manifest is not recorded: %+v", got.Ack.Changes)
	}

	// 3. THE REFUSAL, WITH ITS TARGET.
	refusals := rec.of(debug.KindAnchorRefused)
	if len(refusals) != 1 {
		t.Fatalf("recorded %d anchor refusals, want 1", len(refusals))
	}
	if refusals[0].Target != "a phrase that is not in this document" {
		t.Fatalf("the refusal does not name what it could not place: %+v", refusals[0])
	}
	if !strings.Contains(refusals[0].Err, "matched 0 times") {
		t.Fatalf("the refusal does not carry the reason: %+v", refusals[0])
	}
	if refusals[0].Op != "comment" {
		t.Fatalf("the refusal does not name the verb that asked: %+v", refusals[0])
	}

	// 4. WHO MOVED THE DOCUMENT — the question that was answered on 2026-08-22
	// by reading version diffs and guessing from the instruction texts.
	cuts := rec.of(debug.KindVersionCut)
	if len(cuts) < 2 {
		t.Fatalf("recorded %d version cuts, want at least 2 (the press and the landing)", len(cuts))
	}
	var landed *debug.Event
	for i := range cuts {
		if cuts[i].Reason == "landed" {
			landed = &cuts[i]
		}
	}
	if landed == nil {
		t.Fatalf("the agent's landing was not recorded as a cut: %+v", cuts)
	}
	if !strings.Contains(landed.Authors, "agent") {
		t.Fatalf("the landing does not say the agent moved the document: %q", landed.Authors)
	}

	// 5. WHETHER ANYONE WAS ATTACHED. A blocking read arrives and leaves, and
	// the count is recorded either side of it — which is the reading a single
	// Waiting() sample cannot give, because a woken reader re-arms and briefly
	// reads zero.
	waitOnce(t, s)
	rec.drain(t)
	on, off := rec.of(debug.KindAttached), rec.of(debug.KindDetached)
	if len(on) == 0 || len(off) == 0 {
		t.Fatalf("a blocking read left no attach/detach record: attached=%d detached=%d", len(on), len(off))
	}
	if on[0].Waiting == nil || *on[0].Waiting < 1 {
		t.Fatalf("the attach did not record how many readers there were: %+v", on[0])
	}
	if off[len(off)-1].Waiting == nil {
		t.Fatalf("the detach did not record how many readers were left: %+v", off[len(off)-1])
	}
}

// TestDebugOffLeaksNothingThroughARealReview is the "prove it is off" half, and
// it is stated over what the REVIEWER WOULD SEE rather than over a flag.
//
// THE CHECK READS STDERR, WHICH IS THE ONE PLACE A LEAK IS VISIBLE. `debug.On()
// is false` is what the code SET — the proxy this repository records six
// pass-forever checks against — and it stays true of a sink that falls back to
// stderr when none is installed, of a call site that prints directly, and of
// anything else that decides a diagnostic is harmless. So the whole of a real
// review runs with os.Stderr redirected and the bytes are counted.
//
// The witness sink beside it is the second claim and a narrower one: nothing
// reaches a destination that was in force earlier in the process. It is not
// what makes this check discriminating and is not described as though it were.
func TestDebugOffLeaksNothingThroughARealReview(t *testing.T) {
	stale := &recording{}
	sink := debug.NewSink(stale, nil)
	restore := debug.SetDefault(sink)
	defer func() {
		restore()
		_ = sink.Close()
	}()

	// Debug goes OFF here, which is the shipped state with GALLEY_DEBUG unset.
	off := debug.SetDefault(nil)
	defer off()
	if debug.On() {
		t.Fatal("debug reports itself on with no sink installed")
	}

	dir := t.TempDir()
	captured, stopCapture := captureStderr(t)

	s := newEditServer(t, dir, "doc.md", "# Title\n\nThe retryBudget controls retries.\n")
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	commentAs(t, s, "retryBudget", "spell this out, it reads as code")
	press(t, s, "{}")
	post(t, s.Handler(), "/_galley/instruct", map[string]any{
		"op": "comment", "target": "not in this document at all", "text": "why?", "author": "court",
	})
	agentEdits(t, s, "# Title\n\nThe retry budget controls retries.\n")
	ackAs(t, s, "answered", "done")
	waitOnce(t, s)

	// Drained BEFORE stderr is restored: whatever sink is in force writes on its
	// own goroutine, so a check that read the capture first would report clean
	// on a leak that simply had not been written yet.
	debug.Flush(5 * time.Second)
	stopCapture()

	if got := captured(); got != "" {
		t.Fatalf("a full review wrote %d bytes to stderr with debug off:\n%s", len(got), got)
	}
	if !sink.Flush(5 * time.Second) {
		t.Fatal("the uninstalled sink did not drain")
	}
	if n := stale.count(); n != 0 {
		t.Fatalf("a full review wrote %d debug events into a sink no longer in force: %+v", n, stale.events)
	}
	written, dropped, failed := sink.Counts()
	if written != 0 || dropped != 0 || failed != 0 {
		t.Fatalf("the uninstalled sink counted %d/%d/%d; want 0/0/0", written, dropped, failed)
	}
}

// captureStderr redirects os.Stderr into a file for the duration and returns a
// reader for what landed there plus the call that puts the real one back. Both
// are needed separately: the restore has to happen before the assertion so a
// failing t.Fatalf is readable.
func captureStderr(t *testing.T) (read func() string, stop func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr.txt")
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = f
	var once sync.Once
	stop = func() {
		once.Do(func() {
			os.Stderr = prev
			_ = f.Close()
		})
	}
	t.Cleanup(stop)
	return func() string {
		b, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}, stop
}

// waitOnce registers and releases one blocking reader through the real
// registry, which is what an attached agent is. The poll is given no time to
// block: the claim under test is that the arrival and the departure are both
// recorded, not how long it waited.
func waitOnce(t *testing.T, s *EditServer) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/_galley/wait?timeout=1ms", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("wait answered %d: %s", w.Code, w.Body.String())
	}
}
