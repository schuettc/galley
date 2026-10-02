package serve

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The atomic write is a rename over the destination, which by default gives
// the file the temp file's mode (0600) and, if the destination was a symlink,
// REPLACES the link with a regular file. Projecting a document the user had
// made group-readable, or a document reached through a symlink into another
// tree, quietly changed what it was.
func TestAtomicWritePreservesTheTargetsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte("after\n")); err != nil {
		t.Fatal(err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode = %04o, want 0644 — the write reset the user's permissions", got)
	}
}

func TestAtomicWriteWritesThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, "link.md")
	if err := os.WriteFile(real, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(link, []byte("after\n")); err != nil {
		t.Fatal(err)
	}

	st, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after\n" {
		t.Fatalf("the write did not reach the link's target: %q", got)
	}
}

func TestAtomicWriteCreatesAMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.md")
	if err := WriteFileAtomic(path, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("content = %q", got)
	}
}

// ws.NewServer starts a per-room idle sweeper goroutine, and nothing ever shut
// it down: a test binary that built thirty-odd servers ended with
// thirty-odd live sweepers, and a `galley edit` process exited with its
// websocket peers still attached. Close is the wiring, and both CLI shutdown
// paths call it after the final flush.
func TestCloseReturnsGoroutinesToBaseline(t *testing.T) {
	// A baseline taken AFTER one full construct/close cycle, so anything the
	// runtime or ygo starts once and keeps is already counted.
	dir := t.TempDir()
	warm := newEditServer(t, dir, "warm.md", "# Warm\n")
	if err := warm.Close(); err != nil {
		t.Fatal(err)
	}
	base := settledGoroutines()

	for i := 0; i < 8; i++ {
		s := newEditServer(t, t.TempDir(), "doc.md", "# Title\n\nHello.\n")
		if err := s.Close(); err != nil {
			t.Fatalf("EditServer.Close: %v", err)
		}
	}

	if got := settledGoroutines(); got > base+2 {
		t.Fatalf("goroutines = %d after 8 servers closed, baseline %d — Close is leaking", got, base)
	}
}

// settledGoroutines waits for the count to stop moving before reporting it:
// Shutdown waits for peer goroutines, but a sweeper observing a closed context
// can still be on its way out.
func settledGoroutines() int {
	prev := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		n := runtime.NumGoroutine()
		if n == prev {
			return n
		}
		prev = n
	}
	return prev
}
