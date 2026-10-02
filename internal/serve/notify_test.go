package serve

import (
	"path/filepath"
	"testing"
	"time"
)

// Three touches in quick succession must produce one notification, not three:
// the reviewer is typing, and each keystroke should reset the clock.
func TestNotifierFiresOnceTouchesSettle(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "fired")
	n := &Notifier{Command: "echo fired >> " + marker, Quiet: 40 * time.Millisecond}
	defer n.Stop()

	for _, fp := range []string{"a", "ab", "abc"} {
		n.Touch(fp)
	}
	waitFor(t, marker, 1)
	// waitFor only proves at least one fired. The claim in the name is that
	// three touches produce EXACTLY one, so give the other two a generous
	// window to show up and then count.
	time.Sleep(200 * time.Millisecond)
	if got := countLines(t, marker); got != 1 {
		t.Fatalf("three quick touches fired %d notifications, want exactly 1", got)
	}
}

// Re-notifying on identical content — a reviewer who typed and undid — must
// not wake anyone a second time.
func TestNotifierStaysQuietWhenNothingChanged(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "fired")
	n := &Notifier{Command: "echo fired >> " + marker, Quiet: 30 * time.Millisecond}
	defer n.Stop()

	n.Touch("settled")
	waitFor(t, marker, 1)
	n.Touch("settled")
	time.Sleep(120 * time.Millisecond)
	if got := countLines(t, marker); got != 1 {
		t.Fatalf("want 1 notification, got %d", got)
	}
}

// What the document arrived carrying must not read as a burst of new work the
// moment the server comes up.
func TestSeedSuppressesTheStartupState(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "fired")
	n := &Notifier{Command: "echo fired >> " + marker, Quiet: 20 * time.Millisecond}
	defer n.Stop()

	n.Seed("from last time")
	n.Touch("from last time")
	time.Sleep(100 * time.Millisecond)
	if got := countLines(t, marker); got != 0 {
		t.Fatalf("the startup state woke someone: %d notifications", got)
	}
}
