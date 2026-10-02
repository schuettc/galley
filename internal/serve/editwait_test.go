package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// These cover what a happy-path wake does not: the registry under load, under
// nothing, and under a server that goes away. This is the first BLOCKING
// endpoint in the codebase, and every one of these is a way to hold a request
// open forever without a single assertion noticing.

// blockedWait starts one GET /_galley/wait on its own goroutine and hands back
// the channel its reply arrives on. Its own recorder per waiter:
// httptest.ResponseRecorder is not safe to share across goroutines.
func blockedWait(h http.Handler, since string, poll time.Duration) <-chan waitReply {
	out := make(chan waitReply, 1)
	go func() {
		rec := httptest.NewRecorder()
		url := "/_galley/wait?timeout=" + poll.String()
		if since != "" {
			url += "&since=" + since
		}
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var got waitReply
		if rec.Code == http.StatusOK {
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
		} else {
			got.Reason = "http-" + rec.Result().Status //nolint:bodyclose // recorder, nothing to close
		}
		out <- got
	}()
	return out
}

// waitersRegistered blocks until Waiting() reports want, so a test never races
// the goroutines it just started. Through the exported reader rather than the
// map, because Waiting() is what callers outside this file will believe.
func waitersRegistered(t *testing.T, s *EditServer, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.Waiting() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Waiting() = %d, want %d", s.Waiting(), want)
}

// A WAITER MUST NOT OUTLIVE ITS SERVER. Without this, stopping the editor
// leaves every `galley wait` hanging on a poll nothing can ever release —
// blocked past the thing it was watching, with no error and no exit.
func TestWaitersAreReleasedWhenTheServerStops(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	h := s.Handler()

	const waiters = 5
	replies := make([]<-chan waitReply, waiters)
	for i := range replies {
		// A poll far longer than the test: if anything here passes, it passed
		// because the server released it, not because it expired.
		replies[i] = blockedWait(h, "", time.Hour)
	}
	waitersRegistered(t, s, waiters)

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for i, ch := range replies {
		select {
		case got := <-ch:
			if got.Reason != waitClosedR {
				t.Errorf("waiter %d reported %q, want %q", i, got.Reason, waitClosedR)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("waiter %d is still blocked on a server that has stopped", i)
		}
	}

	// And a poll that arrives AFTER the stop is answered rather than
	// registered into a registry nothing will release again.
	select {
	case got := <-blockedWait(h, "", time.Hour):
		if got.Reason != waitClosedR {
			t.Errorf("a poll after shutdown reported %q, want %q", got.Reason, waitClosedR)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a poll arriving after shutdown blocked forever")
	}
}

// ReleaseWaiters runs twice on an orderly shutdown — once from the CLI before
// http.Server.Shutdown, once from Close. The second must be a no-op, not a
// second close of the same channel.
func TestReleaseWaitersIsIdempotent(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")
	t.Cleanup(func() { _ = s.Close() })

	reply := blockedWait(s.Handler(), "", time.Hour)
	waitersRegistered(t, s, 1)

	s.ReleaseWaiters()
	s.ReleaseWaiters()
	s.ReleaseWaiters()

	select {
	case got := <-reply:
		if got.Reason != waitClosedR {
			t.Errorf("reason = %q, want %q", got.Reason, waitClosedR)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still blocked after three releases")
	}
}

// MANY WAITERS AND NONE. A release must reach every reader and must not block
// on any of them; with nobody listening it must be a no-op rather than a panic
// or a send into a nil map.
func TestOneReviseReleasesEveryWaiter(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	// No waiters at all: a wake with an empty registry.
	s.wakeWaiters(waitSettle, "whatever", nil)

	const waiters = 12
	replies := make([]<-chan waitReply, waiters)
	for i := range replies {
		replies[i] = blockedWait(h, "", time.Hour)
	}
	waitersRegistered(t, s, waiters)

	// No --on-revise configured. Pull is the whole point: the press exists to
	// release the session that is already here, and refusing it for want of a
	// shell command would make the feature depend on the thing it replaces.
	if rec := post(t, h, "/_galley/revise", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revise with waiters and no command: %d %s", rec.Code, rec.Body.String())
	}

	for i, ch := range replies {
		select {
		case got := <-ch:
			if got.Reason != waitRevise {
				t.Errorf("waiter %d reported %q, want %q", i, got.Reason, waitRevise)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("waiter %d never heard the press", i)
		}
	}
}

// Revise clears the browser's pending marks as it cuts the round, but that
// cleanup must not clear the payload handed to the attached agent. This is the
// exact whole-document path used by the instruction panel.
func TestReviseHandsWaiterTheCapturedDocumentInstruction(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	if rec := post(t, h, "/_galley/instruct", map[string]any{
		"op": "comment_document", "text": "Remove my name.", "author": "court",
	}); rec.Code != http.StatusOK {
		t.Fatalf("document instruction: %d %s", rec.Code, rec.Body.String())
	}
	reply := blockedWait(h, "", time.Hour)
	waitersRegistered(t, s, 1)
	if rec := post(t, h, "/_galley/revise", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}

	got := <-reply
	if got.Pending == nil || len(got.Pending.Instructions) != 1 ||
		got.Pending.Instructions[0].Text != "Remove my name." {
		t.Fatalf("wait payload = %+v, want captured document instruction", got.Pending)
	}
	current, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Instructions) != 0 {
		t.Fatalf("browser pending was not cleared after handoff: %+v", current.Instructions)
	}
	// The waiter was handed the round, and the unsent round let go of it.
	if left := loadUnsent(t, s); len(left) != 0 {
		t.Fatalf("pending.json still holds the sent instruction: %+v", left)
	}
}

func TestLiveHandsWaiterTheSameCapturedDocumentInstruction(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	if rec := post(t, h, "/_galley/instruct", map[string]any{
		"op": "comment_document", "text": "Remove my name.", "author": "court",
	}); rec.Code != http.StatusOK {
		t.Fatalf("document instruction: %d %s", rec.Code, rec.Body.String())
	}
	reply := blockedWait(h, "", time.Hour)
	waitersRegistered(t, s, 1)
	fp, _, err := s.waitFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	s.wakeSettle(fp)

	got := <-reply
	if got.Reason != waitSettle || got.Pending == nil || len(got.Pending.Instructions) != 1 ||
		got.Pending.Instructions[0].Text != "Remove my name." {
		t.Fatalf("live wait payload = %+v, want captured document instruction", got)
	}
	current, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Instructions) != 0 {
		t.Fatalf("browser pending was not cleared after live handoff: %+v", current.Instructions)
	}
	if left := loadUnsent(t, s); len(left) != 0 {
		t.Fatalf("pending.json still holds the instruction a live settle sent: %+v", left)
	}
	if _, waiting := s.ReviseWatch(); !waiting {
		t.Fatal("live handoff did not open the agent response window")
	}
}

// The reviewer's SECOND press must reach a waiter that re-armed after the
// first. The hand-rolled marker-file version of this was one-shot, and the
// second press vanishing into a dead watcher is the bug `galley wait` exists
// to not have.
func TestASecondPressReachesAReArmedWaiter(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	for press := 1; press <= 3; press++ {
		reply := blockedWait(h, "", time.Hour)
		waitersRegistered(t, s, 1)
		if rec := post(t, h, "/_galley/revise", nil); rec.Code != http.StatusNoContent {
			t.Fatalf("press %d: %d %s", press, rec.Code, rec.Body.String())
		}
		select {
		case got := <-reply:
			if got.Reason != waitRevise {
				t.Fatalf("press %d reported %q, want %q", press, got.Reason, waitRevise)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("press %d never reached the waiter", press)
		}
		waitersRegistered(t, s, 0)
	}
}

// Waiting() MUST COUNT ONLY LIVE READERS. A stale entry is worse than no
// answer at all: it reports a press as landed to a reviewer with nobody
// listening, which is the exact failure `galley wait` exists to remove.
//
// A real httptest.Server rather than a recorder, deliberately — a client
// hanging up only cancels the request context over a real connection, so the
// recorder-based tests above cannot see this case at all.
func TestWaitingCountsOnlyLiveReaders(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	ts := httptest.NewServer(s.Handler())
	// LIFO, so s.Close runs FIRST. httptest.Server.Close blocks until every
	// outstanding request completes, and a blocked long-poll is an outstanding
	// request — the other order deadlocks the package the moment a test ends
	// with a waiter still registered. s.Close releases them; ts.Close then
	// drains at once. See liveWaitServer in cmd/galley/wait_test.go.
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = s.Close() })

	if got := s.Waiting(); got != 0 {
		t.Fatalf("Waiting() = %d before anyone asked", got)
	}

	// A reader that HANGS UP mid-poll.
	ctx, cancel := context.WithCancel(context.Background())
	hungUp := make(chan struct{})
	go func() {
		defer close(hungUp)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/_galley/wait?timeout=1h", nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	waitersRegistered(t, s, 1)
	cancel()
	<-hungUp
	waitersRegistered(t, s, 0)

	// A reader that EXPIRES.
	expired := make(chan struct{})
	go func() {
		defer close(expired)
		resp, err := http.Get(ts.URL + "/_galley/wait?timeout=100ms")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-expired
	waitersRegistered(t, s, 0)

	// And the consequence that makes this matter: with the registry honestly
	// empty and no command configured, a press is refused rather than reported
	// as landed to nobody.
	if rec := post(t, s.Handler(), "/_galley/revise", nil); rec.Code != http.StatusNotImplemented {
		t.Fatalf("revise with only stale readers: %d %s, want 501", rec.Code, rec.Body.String())
	}
}

// A HOOK AND A WAITER ARE NOT ALTERNATIVES. A persistent agent woken by
// --on-revise and a session already in the room blocked on `galley wait` can
// both be listening, and silently preferring one would be a surprise that only
// shows up in the configuration nobody tests.
func TestRevisePressReachesBothTheHookAndTheWaiter(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	marker := filepath.Join(dir, "fired")
	s.OnRevise = "echo fired >> " + marker
	h := s.Handler()

	reply := blockedWait(h, "", time.Hour)
	waitersRegistered(t, s, 1)

	if rec := post(t, h, "/_galley/revise", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-reply:
		if got.Reason != waitRevise {
			t.Errorf("waiter reported %q, want %q", got.Reason, waitRevise)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never heard a press that also ran the hook")
	}
	waitFor(t, marker, 1)

	// And the reviewer-facing window opened, which is what the button counts
	// against — the press is an ask whoever answers it.
	if _, waiting := s.ReviseWatch(); !waiting {
		t.Error("the revision window did not open")
	}
}

// With neither a command NOR a reader there is genuinely nothing to hand the
// revision to, and saying so is better than a button that silently does
// nothing. This is the pre-existing 501, which pull must not erase.
func TestReviseStillRefusesWithNoCommandAndNoWaiter(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")
	t.Cleanup(func() { _ = s.Close() })

	rec := post(t, s.Handler(), "/_galley/revise", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("revise: %d %s, want 501", rec.Code, rec.Body.String())
	}
}

// NO GOROUTINE LEAK. A blocking endpoint is the easiest place in a server to
// strand one, and a stranded waiter is invisible: it holds a request open, a
// registry entry and a timer, and nothing fails.
func TestWaitStrandsNoGoroutines(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello world.\n")
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()

	// After the server is up, so its own sweeper is not counted as a leak.
	settle(t)
	base := runtime.NumGoroutine()

	// Expiries — the case that leaks if the timer or the registry entry
	// outlives the request.
	for round := 0; round < 3; round++ {
		expired := make([]<-chan waitReply, 8)
		for i := range expired {
			expired[i] = blockedWait(h, "", 60*time.Millisecond)
		}
		for i, ch := range expired {
			select {
			case got := <-ch:
				if got.Reason != waitTimeout {
					t.Fatalf("round %d waiter %d reported %q, want %q", round, i, got.Reason, waitTimeout)
				}
				if got.Fingerprint == "" {
					t.Fatalf("round %d waiter %d expired with no fingerprint to re-arm on", round, i)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("round %d waiter %d never expired", round, i)
			}
		}
	}

	// Released waiters, which must clean up by the same route.
	released := make([]<-chan waitReply, 8)
	for i := range released {
		released[i] = blockedWait(h, "", time.Hour)
	}
	waitersRegistered(t, s, len(released))
	s.wakeWaiters(waitSettle, "moved", nil)
	for i, ch := range released {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("released waiter %d never returned", i)
		}
	}
	waitersRegistered(t, s, 0)

	settle(t)
	if grown := runtime.NumGoroutine() - base; grown > 2 {
		t.Errorf("goroutines grew by %d across 32 polls — a waiter is being stranded", grown)
	}
}

// settle gives finished goroutines a moment to actually exit, so a count taken
// straight after one returns is not measuring the scheduler.
func settle(t *testing.T) {
	t.Helper()
	for i := 0; i < 10; i++ {
		runtime.Gosched()
		time.Sleep(20 * time.Millisecond)
	}
}

// A poll may not be a way to make the server write. guard() is POST-only and
// this endpoint is deliberately outside it, so the method check is the only
// thing standing between a page and a mutation-shaped request here.
func TestWaitRefusesAnythingButGET(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")
	t.Cleanup(func() { _ = s.Close() })

	rec := post(t, s.Handler(), "/_galley/wait", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /_galley/wait: %d, want 405", rec.Code)
	}
}

// A malformed ?timeout is refused rather than silently replaced by the
// default: a caller that asked for 30s and got five minutes has no way to
// notice, and its own deadline is what it would blow through.
func TestWaitRefusesAnUnreadableTimeout(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")
	t.Cleanup(func() { _ = s.Close() })

	for _, bad := range []string{"soon", "-5s", "0s", "30"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_galley/wait?timeout="+bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("timeout=%q: %d, want 400", bad, rec.Code)
		}
	}
}
