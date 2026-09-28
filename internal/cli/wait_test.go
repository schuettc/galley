package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/serve"
)

// These are the three cases for the blocking read, and they are deliberately
// written against the ENDPOINT rather than through wait.go's helper: what is
// being pinned is the contract `galley wait` speaks, not the CLI's reading of
// it. A real EditServer behind httptest, the same harness
// TestLivePendingSuggestAccept uses.

// waitReply is GET /_galley/wait's JSON.
type waitReply struct {
	Reason      string         `json:"reason"`
	Fingerprint string         `json:"fingerprint"`
	Pending     pendingPayload `json:"pending"`
}

// waitGet issues one long-poll. No *testing.T, because the interesting cases
// run it on another goroutine and t.Fatalf off the test goroutine is a lie
// about which assertion failed.
func waitGet(baseURL, since string, timeout time.Duration) (waitReply, int, error) {
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	if timeout > 0 {
		q.Set("timeout", timeout.String())
	}
	// No client timeout: the SERVER bounds the poll, and a client that gave up
	// first would report "the server never answered" for a working endpoint.
	resp, err := http.Get(baseURL + "/_galley/wait?" + q.Encode())
	if err != nil {
		return waitReply{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return waitReply{}, resp.StatusCode, nil
	}
	var out waitReply
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return waitReply{}, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

type waitOutcome struct {
	reply waitReply
	code  int
	err   error
}

// waitAsync starts a poll on its own goroutine, so the test can drive the
// document while it is blocked.
func waitAsync(baseURL, since string, timeout time.Duration) <-chan waitOutcome {
	out := make(chan waitOutcome, 1)
	go func() {
		r, c, err := waitGet(baseURL, since, timeout)
		out <- waitOutcome{r, c, err}
	}()
	return out
}

// liveWaitServer is a real EditServer behind httptest, in ● live mode with a
// quiet window a test can outlast.
//
// ● live and not `on ask`, deliberately: a settle only reaches the notifier in
// live mode, so `on ask` is a document where nothing but a Revise press can
// ever wake a waiter. That is the intended behaviour, and it is the wrong
// fixture for the settle cases.
func liveWaitServer(t *testing.T, content string) (*serve.EditServer, *httptest.Server) {
	t.Helper()
	doc := writeDoc(t, t.TempDir(), "doc.md", content)
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	// A waiter rides the notifier's settle decision rather than making its own,
	// so the fixture needs a notifier with a test-length quiet window.
	// CONFIGURED, NOT REPLACED — the same thing runEdit does, and for the same
	// reason: NewEdit always supplies one (pull needs no command), and
	// assigning a fresh Notifier is the shape that quietly detaches the wake.
	srv.Notify.Quiet = 50 * time.Millisecond
	if err := srv.SetMode(serve.ModeLive); err != nil {
		t.Fatal(err)
	}
	// What the document arrived carrying is not news — the same thing the CLI
	// says at startup.
	srv.SeedNotify()

	ts := httptest.NewServer(srv.Handler())
	// ORDER MATTERS, AND GETTING IT WRONG HANGS THE WHOLE PACKAGE.
	//
	// t.Cleanup is LIFO, so registering srv.Close SECOND runs it FIRST.
	// httptest.Server.Close blocks until every outstanding request completes,
	// and an outstanding request is exactly what a wait test leaves behind —
	// so with the other order a test that ends with a waiter still blocked
	// (which is what TestWaitIgnoresTheAgentsOwnWrite's first half deliberately
	// asserts) deadlocks until the package timeout. srv.Close releases every
	// waiter, so it must run before ts.Close waits for them.
	//
	// The symptom is a bare `panic: test timed out` naming handleWait's select,
	// which is an hour to diagnose from scratch. Hence this comment.
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Close() })
	return srv, ts
}

// armed returns the fingerprint a caller would re-arm on: an expired poll must
// report the CURRENT fingerprint, which is also how a `galley wait` loop with
// no --timeout keeps its cursor honest across poll windows.
func armed(t *testing.T, baseURL string) string {
	t.Helper()
	got, code, err := waitGet(baseURL, "", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("probe: HTTP %d — GET /_galley/wait must answer 200", code)
	}
	if got.Reason != "timeout" {
		t.Fatalf("a poll with nothing pending reported %q, want %q", got.Reason, "timeout")
	}
	if got.Fingerprint == "" {
		t.Fatal("an expired poll reported no fingerprint — there is nothing to re-arm on")
	}
	return got.Fingerprint
}

func instructAs(t *testing.T, baseURL, target, instruction string) {
	t.Helper()
	body := fmt.Sprintf(`{"op":"comment","target":%q,"text":%q,"author":"court"}`, target, instruction)
	resp, err := http.Post(baseURL+"/_galley/instruct", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("instruct: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("instruct: HTTP %d", resp.StatusCode)
	}
}

// 1. A WAKE ARRIVES. The reviewer moves the document while a waiter is
// blocked; the waiter returns with the reason and the fingerprint it woke at.
func TestWaitWakesWhenTheReviewerMoves(t *testing.T) {
	_, ts := liveWaitServer(t, "# Title\n\nHello world.\n")
	since := armed(t, ts.URL)

	done := waitAsync(ts.URL, since, 10*time.Second)
	// Registered before the mutation lands, so this is a genuine wake and not
	// the catch-up path wearing its name.
	time.Sleep(100 * time.Millisecond)

	instructAs(t, ts.URL, "world", "Use galley here.")

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("wait: %v", got.err)
		}
		if got.code != http.StatusOK {
			t.Fatalf("wait: HTTP %d", got.code)
		}
		if got.reply.Reason != "settle" {
			t.Errorf("reason = %q, want %q", got.reply.Reason, "settle")
		}
		if got.reply.Fingerprint == since {
			t.Error("woke on the fingerprint it was already holding — nothing to re-arm past")
		}
		if len(got.reply.Pending.Instructions) == 0 {
			t.Error("woke with an empty pending payload — the caller has to ask again to learn what changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never woke — a reviewer's edit settled and nothing was released")
	}
}

func TestWaitPrintsTheCapturedInstructionInsteadOfPointingAtClearedPending(t *testing.T) {
	res := waitResult{
		Reason:      reasonRevise,
		Fingerprint: "abc123",
		Pending: pendingPayload{Instructions: []instructionPayload{{
			Quote: "The sentence under review.", Text: "Make this concrete.",
		}}},
	}
	got := wakeText("draft.md", res)
	for _, want := range []string{"The sentence under review.", "Make this concrete.", "captured round"} {
		if !strings.Contains(got, want) {
			t.Errorf("wake lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "read it   galley pending") {
		t.Errorf("wake still sends the agent to the cleared live view:\n%s", got)
	}
}

// 2. --since CATCHES UP. A fingerprint the document has already moved past
// returns AT ONCE. This is the re-arm gap: between a waiter exiting and being
// re-armed, an event has nobody to deliver to, and in live mode that window is
// hit constantly.
func TestWaitCatchesUpOnAStaleSince(t *testing.T) {
	_, ts := liveWaitServer(t, "# Title\n\nHello world.\n")

	const stale = "0000000000000000"
	start := time.Now()
	got, code, err := waitGet(ts.URL, stale, 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("wait: HTTP %d", code)
	}
	if elapsed > 2*time.Second {
		t.Errorf("blocked for %s — a --since the document has moved past must return without waiting", elapsed)
	}
	if got.Reason == "timeout" {
		t.Errorf("reported %q — a stale --since is news, not an expiry", got.Reason)
	}
	if got.Fingerprint == stale || got.Fingerprint == "" {
		t.Errorf("fingerprint = %q, want the one the document is actually at", got.Fingerprint)
	}
}

// THE FIXTURE MUST SURVIVE A TEST THAT ENDS WITH A WAITER STILL BLOCKED,
// which is the natural shape of a wait test rather than an exotic one:
// TestWaitIgnoresTheAgentsOwnWrite's first half asserts precisely that, and is
// one deleted second half away from being this test.
//
// Flip liveWaitServer's cleanup order and this deadlocks the entire package on
// teardown, reported as a bare `panic: test timed out` naming handleWait's
// select. It is here so the hazard is executable rather than only a comment.
func TestTeardownSurvivesAStillBlockedWaiter(t *testing.T) {
	srv, ts := liveWaitServer(t, "# Title\n\nHello world.\n")

	// Deliberately never awaited. Teardown is what has to unwedge it.
	waitAsync(ts.URL, "", time.Hour)

	deadline := time.Now().Add(5 * time.Second)
	for srv.Waiting() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Asserted, so this can never pass by never having blocked in the first
	// place — which would make it a test of nothing.
	if srv.Waiting() == 0 {
		t.Fatal("the waiter never registered, so teardown is not being tested")
	}
}

// NO OFFLINE PATH. Every other verb degrades to editing the file on disk;
// there is nothing on disk to wait FOR, and a `wait` that returned at once
// because no editor was running would turn a blocking loop into a spinning
// one.
func TestWaitWithNoServerRefusesRatherThanDegrading(t *testing.T) {
	doc := writeDoc(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")

	_, stderr, code := galleyCLI(t, "wait", doc)
	if code == 0 {
		t.Fatal("want a non-zero exit with no server running")
	}
	if !strings.Contains(stderr, "no server running") {
		t.Fatalf("stderr %q should say no server is running", stderr)
	}
}

// waitUntilWaiting blocks until a waiter has actually registered with srv, so
// a test that posts a wake right after starting one does not race the
// goroutine that has not reached GET /_galley/wait yet. Same poll shape as
// TestTeardownSurvivesAStillBlockedWaiter's inline loop, pulled out because
// this file's new approve case needs it too.
func waitUntilWaiting(t *testing.T, srv *serve.EditServer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for srv.Waiting() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.Waiting() == 0 {
		t.Fatal("the waiter never registered")
	}
}

// An approve travels the same pipe as a revise and must exit the wait loop
// the same way — reason in hand, exit 0 — because the agent's next act is
// decided by the reason, not by the transport.
func TestWaitReportsAnApprove(t *testing.T) {
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nClean.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	done := make(chan waitOutcome, 1)
	go func() {
		reply, code, err := waitGet(ts.URL, "", 5*time.Second)
		done <- waitOutcome{reply: reply, code: code, err: err}
	}()
	waitUntilWaiting(t, srv) // the file's existing helper that polls Waiting() > 0; write it if absent
	resp, err := http.Post(ts.URL+"/_galley/revise", "application/json", strings.NewReader(`{"verdict":"approve"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	out := <-done
	if out.err != nil || out.code != 200 {
		t.Fatalf("wait: code %d err %v", out.code, out.err)
	}
	if out.reply.Reason != "approve" {
		t.Errorf("reason = %q, want approve", out.reply.Reason)
	}
}

// --timeout elapsing and the editor stopping are both non-zero, and they are
// DIFFERENT: a loop re-arms after one and gives up after the other, and it can
// only tell them apart by the exit code main.go derives from these sentinels.
func TestWaitDistinguishesTimeoutFromAStoppedEditor(t *testing.T) {
	srv, ts := liveWaitServer(t, "# Title\n\nHello world.\n")

	_, err := waitFor(ts.URL, "", 400*time.Millisecond)
	if !errors.Is(err, errWaitTimeout) {
		t.Errorf("a quiet editor gave %v, want errWaitTimeout", err)
	}
	// A caller that gave up still needs the cursor, or coming back means
	// replaying whatever happened while it was away. The error is the only
	// channel a non-zero exit has.
	if !strings.Contains(err.Error(), "--since") {
		t.Errorf("timeout message %q offers nothing to re-arm on", err.Error())
	}

	stopped := make(chan error, 1)
	go func() {
		_, err := waitFor(ts.URL, "", 30*time.Second)
		stopped <- err
	}()
	time.Sleep(200 * time.Millisecond)
	_ = srv.Close()

	select {
	case err := <-stopped:
		if !errors.Is(err, errWaitStopped) {
			t.Errorf("a stopped editor gave %v, want errWaitStopped", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waitFor never returned after the editor stopped")
	}
}
