// wait.go is galley's PULL half.
//
// Every other hook here pushes: --on-revise and --on-settle run a shell command
// to wake someone who is not here. That is right for a persistent agent with an
// inbox and wrong for paired review, where the valuable participant is the
// session that is ALREADY here — holding the branch, the discussion and the
// hours of context that made the review worth having. `galley wait` inverts it:
// that session blocks on the document and is woken when the reviewer asks.
//
// TRANSPORT: LONG-POLL, NOT SERVER-SENT EVENTS.
//
// `galley wait` asks one question once and answers it with an exit code. A
// long-poll is exactly that shape — one request, one response, no protocol
// beyond HTTP, and it works through anything that speaks it. SSE is the better
// fit for a consumer that stays open and takes a stream of events, which is
// precisely what the editor's websocket already is; adding a second streaming
// channel for a client whose whole life is one answer would buy nothing and
// cost a framing format, a reconnect policy and a parser.
//
// The one thing a long-poll needs that a stream does not is a way to survive
// its own expiry, because a poll held open forever is indistinguishable from a
// wedged one. That is what --since and the `timeout` reason are for: each
// expiry reports the fingerprint the server is CURRENTLY at, and the next poll
// re-arms on it. Nothing falls into the gap between two polls, because the gap
// is exactly what a stale --since detects.
//
// WHAT THE CURSOR CANNOT DETECT IS AN ENDING, and it never could: a clean
// approve decides nothing and moves no fingerprint, so a poll armed on a
// perfectly current cursor blocked through the verdict and reported `timeout`
// over a review that was already over. The seal is a FACT the server holds
// rather than an event it fired, so any poll of an ended review is answered
// with its ending — with or without a cursor, moved fingerprint or not. That
// is why this command exits on approve and discard and tells the caller not to
// re-arm: coming back would be told the same ending again, correctly.
//
// KNOWN CAVEAT FOR ANYONE WRITING A LOOP AROUND THIS: --since goes stale if
// YOU write between two waits.
//
// The printed fingerprint is the document as it stood when this command
// returned. If the loop then answers the review — edits the file, acks the
// round — the document moves, and the cursor in hand no longer
// describes it. Passing it back as --since is then a stale cursor, and the next
// wait catches up IMMEDIATELY on the loop's own work.
//
// It costs one wasted iteration, and then the loop settles. It is not a missed
// wake, and a waiter that is ALREADY BLOCKED is not affected at all — the
// server's self-wake guard covers that case, and there is a test for it. It
// only bites the arm-work-arm shape.
//
// WHAT THE WASTED ITERATION ACTUALLY CARRIES, because an earlier draft of this
// comment got it wrong and a loop written against that sentence would size its
// catch-up handling wrong. It said the agent is "shown only suggestions it
// wrote itself". It is not. A wake carries the WHOLE pending view — every open
// suggestion and every thread, the reviewer's included — because Pending is a
// SNAPSHOT and never a delta. What is wasted about the iteration is only what
// PROVOKED it: the loop's own writes moved the fingerprint past the cursor in
// hand, and nothing the reviewer did is new. Measured on a green `just loop`:
// four entries, two the agent's and two the reviewer's, on a wake nobody asked
// for. A loop that reads a wake as "here is what is new" will re-process work
// it has already answered, every round. Diff against what you last saw.
//
// Two ways around it, neither requiring anything from galley: drop --since on
// the arm that follows your own writes and accept the small race, or do the
// writing before the wait rather than between two of them. The real fix is for
// the mutating verbs to print the fingerprint the way this one does, so a
// loop's cursor is never stale; that is a change to those commands, not to
// this one.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/debug"
	"github.com/schuettc/galley/internal/serve"
	tools "github.com/schuettc/tools-common"
)

// The eight things a poll can report. Mirrors the endpoint's vocabulary in
// internal/serve; revise, settle, approve, discard, reopen and changed are
// wakes.
//
// discard is TERMINAL like approve — the review ended, it just ended without
// the markup being applied — so it exits 0 and does not tell the caller to
// re-arm. reopen is the opposite of terminal: the review is live again, and
// the caller re-arms on it exactly as it would on a revise.
//
// changed is the CATCH-UP's, and it is the one wake that names no event: the
// cursor this caller sent is behind, so the document moved while it was away
// and the server does not keep why. It used to answer `settle` there, which
// says the document settled in ● live mode — a claim, and one that is simply
// false on a session `on ask`. Re-read everything is the whole of what it
// means, which is also what a settle asks for, so a loop treats the two alike.
const (
	reasonRevise  = "revise"
	reasonSettle  = "settle"
	reasonApprove = "approve"
	reasonChanged = "changed"
	reasonTimeout = "timeout"
	reasonClosed  = "closed"
)

// waitResult is GET /_galley/wait's JSON, and — with --json — this command's
// entire stdout.
type waitResult struct {
	Reason      string         `json:"reason"`
	Fingerprint string         `json:"fingerprint"`
	Pending     pendingPayload `json:"pending"`
	// Note is the reopen's reason and rides only that wake — the one thing in
	// a wake that is not a fact about the document.
	Note string `json:"note,omitempty"`
}

// errWaitTimeout and errWaitStopped are the two non-zero answers, and they are
// kept apart because a caller's loop reacts to them differently: a timeout is
// "ask again", a stopped editor is "stop asking". main.go maps them to exit 3
// and exit 4 — the message alone is not something `while galley wait …; do`
// can branch on.
var (
	errWaitTimeout = errors.New("timed out")
	errWaitStopped = errors.New("the editor stopped — there is nothing left to wait for")
)

// waitPoll is how long ONE request is held open. A `galley wait` with no
// --timeout is not one infinite request but a sequence of these, each re-armed
// on the fingerprint the last expiry reported.
//
// Minutes rather than seconds: this is loopback, an idle poll costs nothing,
// and a short one would turn a quiet afternoon of review into a steady stream
// of requests for no gain.
const waitPoll = 2 * time.Minute

// waitBusyBackoff is how long to pause after a 503. The endpoint answers that
// only when the document was being written to throughout its own retries, so
// the fix is to come back in a moment rather than to give up — but not
// instantly, or a browser mid-paragraph becomes a spin.
const waitBusyBackoff = 250 * time.Millisecond

// waitExit translates waitFor's sentinels into the *ExitError codes Dispatch's
// exit mapping wants — 3 for a --timeout that elapsed, 4 for an editor that
// went away — while waitFor itself keeps returning the sentinels (wait_test.go
// asserts on those directly). A `while galley wait …; do …; done` re-arms after
// a 3 and gives up on a 4, and it can only tell them apart by the exit code.
// Everything else (errNoServer, an unknown wake reason) is a plain exit-1 error
// and passes through untouched.
func waitExit(err error) error {
	switch {
	case errors.Is(err, errWaitTimeout):
		return tools.Exitf(3, "%s", err.Error())
	case errors.Is(err, errWaitStopped):
		return tools.Exitf(4, "%s", err.Error())
	default:
		return err
	}
}

// waitFlags is galley wait's flag set, built by newWaitFlags so the app
// registry (NewFlags) and runWait share one construction.
type waitFlags struct {
	since   *string
	timeout *time.Duration
	asJSON  *bool
}

func newWaitFlags() (*flag.FlagSet, *waitFlags) {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	v := &waitFlags{
		since:   fs.String("since", "", "the fingerprint this caller last saw; returns at once if the document has moved past it"),
		timeout: fs.Duration("timeout", 0, "give up after this long (default: wait indefinitely)"),
		asJSON:  fs.Bool("json", false, "emit the wake verbatim"),
	}
	return fs, v
}

func runWait(args []string, out, errw io.Writer) error {
	fs, v := newWaitFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	since, timeout, asJSON := v.since, v.timeout, v.asJSON
	doc := pos[0]

	// No offline path, deliberately. Every other verb degrades to editing the
	// file on disk; there is nothing on disk to wait FOR, and a `wait` that
	// returned immediately because no server was running would be a loop that
	// spins instead of one that blocks.
	rt, ok := serve.FindRuntime(doc)
	if !ok {
		return errNoServer(doc)
	}

	res, err := waitFor(rt.URL, *since, *timeout)
	if err != nil {
		return waitExit(err)
	}

	// THE OTHER CARRIER, RECORDED IN THE SAME SHAPE. A rule fixed in one of two
	// carriers and not the other is this codebase's most-repeated defect, and a
	// debug mode that watched only the channel would be a fresh instance of it:
	// a shell loop's round would be exactly as unrecoverable as the one that
	// had to be read out of another project's transcript.
	//
	// Recorded on BOTH branches, because --json is what an agent reads and the
	// prose is what a human reads, and which one was in play is part of the
	// reconstruction. What is logged is what this process PRINTED.
	if *asJSON {
		// MARSHALLED ONCE AND PRINTED FROM THAT, so the bytes recorded and the
		// bytes the agent read are the same bytes rather than two renderings
		// that agree for now. Byte-identical to the json.Encoder this replaced:
		// Encode writes MarshalIndent's output followed by one newline.
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		debugWake(doc, res, string(b))
		fmt.Println(string(b))
		return nil
	}

	rendered := wakeText(doc, res)
	debugWake(doc, res, rendered)
	fmt.Print(rendered)
	return nil
}

// debugWake records one wake AS PRINTED. See internal/debug; off unless
// GALLEY_DEBUG is set, and it returns nothing so no caller can turn a full disk
// into a failed wait.
func debugWake(doc string, res waitResult, rendered string) {
	if !debug.On() {
		return
	}
	asks := make([]debug.Ask, 0, len(res.Pending.Instructions))
	for _, in := range res.Pending.Instructions {
		asks = append(asks, debug.Ask{Key: in.Key, Text: in.Text, Quote: in.Quote})
	}
	debug.Log(debug.Event{
		Kind:         debug.KindRoundSent,
		Where:        "wait",
		Doc:          doc,
		Reason:       res.Reason,
		Fingerprint:  res.Fingerprint,
		Note:         res.Note,
		Rendered:     rendered,
		Instructions: debug.Int(len(res.Pending.Instructions)),
		Asks:         asks,
	})
}

// wakeText is the whole of what `galley wait` prints for one wake, RETURNED
// rather than printed, so a test can read the words an agent is actually
// handed. What it says is the CONTRACT — the sentence tells the loop whether to
// keep going — and a contract nothing can assert is one that gets a branch
// wrong in silence. That is exactly what happened: see wake.go.
func wakeText(doc string, res waitResult) string {
	wk := res.wake(doc)
	var b strings.Builder
	fmt.Fprintf(&b, "%s   %s\n", res.Reason, res.Fingerprint)
	fmt.Fprintf(&b, "%d instruction(s) on %s\n", len(res.Pending.Instructions), wk.page)
	if len(res.Pending.Instructions) > 0 {
		b.WriteByte('\n')
		b.WriteString(formatInstructions(res.Pending.Instructions))
	}
	if s := wk.sentence(); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	// THE CATCH-UP SAYS SO IN WORDS, because "changed" is the one reason that
	// is about this CALLER's cursor rather than about the document: nothing
	// woke you, you were away. What is printed below is the whole pending view
	// as it stands, which is exactly what to act on — and what NOT to do is
	// treat it as news, since some of it may be work this loop did itself.
	if res.Reason == reasonChanged {
		fmt.Fprintf(&b, "\n%s moved while this cursor was behind — the editor does not record what moved it; "+
			"re-read everything below and diff against what you last saw\n", wk.page)
	}
	// An ending makes the pointer block a trap: a loop told to re-arm here
	// blocks forever on an editor about to go away. Which wakes ARE endings is
	// wake.over()'s question and not this function's — an approve that
	// entrusted work is not one, and printing this line under it is the defect
	// wake.go exists to close.
	if wk.over() {
		b.WriteString("do not re-arm — a wait on a review that has ended is answered with the same ending, forever\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\nThe instructions above are the captured round. `galley pending` now shows only a newer, unsent round.\n"+
		"re-arm    galley wait %s --since %s\n", doc, res.Fingerprint)
	return b.String()
}

// wake reduces this command's wake to the shape wake.go's shared vocabulary
// speaks, so the pull carrier and the push carrier answer "what does this mean"
// with the same code rather than with two sentences that agree by hand.
func (res waitResult) wake(doc string) wake {
	return wake{
		reason:       res.Reason,
		page:         filepath.Base(doc),
		note:         res.Note,
		instructions: len(res.Pending.Instructions),
	}
}

// waitFor polls until something wakes it, the deadline passes, or the editor
// goes away.
func waitFor(baseURL, since string, timeout time.Duration) (waitResult, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	// NO client-side timeout. The server bounds every poll; a client that gave
	// up first would report a perfectly live editor as a dead one, which is the
	// one answer a waiting loop must never get wrong.
	client := &http.Client{}

	// The last fingerprint the editor reported, carried into the timeout
	// message. A caller that set --timeout and got nothing still needs a cursor
	// to re-arm on, and the error is the only channel it has — without it,
	// giving up and coming back means replaying whatever happened meanwhile.
	seen := since

	for {
		poll := waitPoll
		if !deadline.IsZero() {
			left := time.Until(deadline)
			if left <= 0 {
				if seen != "" {
					return waitResult{}, fmt.Errorf("%w after %s — nothing was asked for in that window; re-arm with --since %s",
						errWaitTimeout, timeout, seen)
				}
				return waitResult{}, fmt.Errorf("%w after %s — nothing was asked for in that window", errWaitTimeout, timeout)
			}
			poll = min(poll, left)
		}

		resp, err := waitHTTP(client, baseURL, since, poll)
		if err != nil {
			// The connection IS the liveness signal here: the editor went away
			// mid-poll, and there is nothing left to re-arm against.
			return waitResult{}, errWaitStopped
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			// The document was being written to throughout — retriable by
			// design, and the cursor we hold is still the honest one.
			_ = resp.Body.Close()
			time.Sleep(waitBusyBackoff)
			continue
		}
		var res waitResult
		if err := decodeOK(resp, &res); err != nil {
			return waitResult{}, err
		}

		switch res.Reason {
		case reasonRevise, reasonSettle, reasonChanged, reasonApprove:
			return res, nil
		case reasonClosed:
			return waitResult{}, errWaitStopped
		case reasonTimeout:
			// Re-arm on what the SERVER just saw, never on the cursor we sent.
			// An expiry means the settle decision declined — usually because
			// the only writes were the agent's own — and carrying the old
			// cursor forward would turn that decline into a wake on the very
			// next poll.
			since = res.Fingerprint
			seen = res.Fingerprint
		default:
			return waitResult{}, fmt.Errorf("the editor reported an unknown wake reason %q", res.Reason)
		}
	}
}

func waitHTTP(client *http.Client, baseURL, since string, poll time.Duration) (*http.Response, error) {
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	q.Set("timeout", poll.String())
	return client.Get(baseURL + "/_galley/wait?" + q.Encode())
}
