package serve

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Notifier runs a command once the document has settled.
//
// It is deliberately a shell command rather than a muster call: the binary must
// be worth running without any of the rest of the toolchain, and "tell someone"
// is exactly the seam where setups differ.
type Notifier struct {
	// Command is run through `sh -c` after Quiet elapses with no further edits.
	Command string
	// Quiet is how long the document must sit unchanged first. Firing on every
	// keystroke would wake the agent mid-sentence, repeatedly.
	Quiet time.Duration
	// Log receives one line per fired or failed notification.
	Log func(string)

	mu      sync.Mutex
	timer   *time.Timer
	lastRun string
	wake    func(string)
}

// SetWake registers what to release when the notifier decides the document has
// moved — the PULL half of the same event Command pushes.
//
// A hook off fire's one decision, deliberately, rather than a second
// fingerprint check somewhere else. A blocked `galley wait` must wake on
// exactly what would have woken a shell hook — including the self-wake guard
// mutate seeds, which lives in lastRun and nowhere else — and the only way to
// guarantee that is to hang off the decision instead of re-making it.
//
// nil clears it. Safe on a nil Notifier, like every other method here.
func (n *Notifier) SetWake(f func(fp string)) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.wake = f
}

// Touch schedules a notification, replacing any already pending.
func (n *Notifier) Touch(fp string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	// Nothing to push and nobody pulling: the timer would fire into an empty
	// room. Checked here under the lock rather than at the top, because
	// SetWake can arrive between two Touches.
	if n.Command == "" && n.wake == nil {
		return
	}
	if n.timer != nil {
		n.timer.Stop()
	}
	// NOTHING NEW TO SAY — and arming it anyway is not the harmless no-op it
	// looks like. fire would decline this fingerprint today, because it is the
	// one last announced; but lastRun can MOVE before the timer elapses (Seed,
	// when the agent writes and is handed everything pending in the same
	// breath), and this stale fingerprint then differs from the new one and
	// fires — waking the agent through a timer armed before its own write. So a
	// projection that says exactly what was last announced CANCELS the pending
	// notification and arms nothing.
	//
	// Reachable since a live round is cut on the wake: that cut asks for a
	// projection of its own, which touches with the fingerprint the fire it came
	// from has just consumed. Caught by TestAgentReplyDoesNotWakeTheAgent.
	if fp == n.lastRun {
		return
	}
	quiet := n.Quiet
	if quiet <= 0 {
		quiet = 8 * time.Second
	}
	n.timer = time.AfterFunc(quiet, func() { n.fire(fp) })
}

func (n *Notifier) fire(fp string) {
	n.mu.Lock()
	// Nothing actually changed since the last notification — a reviewer who
	// typed and undid should not wake anyone.
	if fp == n.lastRun {
		n.mu.Unlock()
		return
	}
	n.lastRun = fp
	cmd := n.Command
	logf := n.Log
	wake := n.wake
	n.mu.Unlock()

	// The pull half first, and out of the lock: a blocked reader is a session
	// already holding the context, and it should not queue behind a push
	// command that may legitimately run for minutes.
	if wake != nil {
		wake(fp)
	}
	// A notifier can exist purely to release waiters — pull needs no shell
	// command, which is the whole point of it.
	if cmd == "" {
		return
	}

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if logf == nil {
		return
	}
	if err != nil {
		logf(fmt.Sprintf("notify failed: %v: %s", err, bytes.TrimSpace(out)))
		return
	}
	logf("notified")
}

// Seed records the current state as already-notified, so replaying a previous
// session's comments at startup does not immediately wake anyone.
func (n *Notifier) Seed(fp string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastRun = fp
}

// Stop cancels any pending notification.
func (n *Notifier) Stop() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.timer != nil {
		n.timer.Stop()
	}
}
