package serve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/schuettc/tools-common"
)

// exportDebounce is short: the disk projection should trail the document
// closely, because it is what the CLI reads. The notification debounce is a
// separate, much longer window — see Notifier.
const exportDebounce = 400 * time.Millisecond

// schemaVersion is stamped into the document so a future change to the thread
// shape can recognise what it is reading.
const schemaVersion = 1

// debouncer schedules a delayed action, replacing any run still pending: the
// edit server's disk projection, scheduled on every change and run once the
// document settles.
type debouncer struct {
	mu    sync.Mutex
	timer *time.Timer
	// running counts scheduled runs that have not finished — one ticket per
	// timer, handed back either by the run itself or by whoever cancels it.
	running sync.WaitGroup
}

// touch (re)schedules fn to run after delay, cancelling whatever was already
// pending. fn's error has nowhere to go here, same as before the extraction:
// a caller that needs to observe it (shutdown's synchronous flush) calls the
// export method directly instead of going through touch.
func (d *debouncer) touch(delay time.Duration, fn func() error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil && d.timer.Stop() {
		// Stopped before it fired, so the run it was counted for will never
		// happen and its ticket has to be handed back here.
		d.running.Done()
	}
	d.running.Add(1)
	d.timer = time.AfterFunc(delay, func() {
		defer d.running.Done()
		_ = fn()
	})
}

// stop cancels whatever run is pending, if any. A caller about to do the work
// synchronously (EditServer.Flush at shutdown) uses this so the timer does not
// fire a second, redundant projection behind it — or, worse, after the process
// has already reported itself flushed.
// It also WAITS for a run already in flight. time.Timer.Stop cancels a timer
// that has not fired and reports false for one that has — and a projection
// already inside fn is exactly the case a caller means to be finished with.
// Without the wait, Close returns while Project is still writing the file: the
// server says it has stopped and the document lands afterwards. It surfaced as
// `TempDir RemoveAll: directory not empty` in CI, a test's own directory being
// deleted out from under a projection that Close had promised was over.
func (d *debouncer) stop() {
	d.mu.Lock()
	if d.timer != nil {
		if d.timer.Stop() {
			d.running.Done()
		}
		d.timer = nil
	}
	d.mu.Unlock()
	// Outside the lock: fn may touch() again, and waiting under the mutex it
	// would need is how that becomes a deadlock instead of a delay.
	d.running.Wait()
}

// lastExportStamp tracks when a projection last reached disk — read by
// handleEditSaved to tell the reviewer their words are durable.
type lastExportStamp struct {
	mu sync.Mutex
	at time.Time
}

func (l *lastExportStamp) mark() {
	l.mu.Lock()
	l.at = time.Now()
	l.mu.Unlock()
}

func (l *lastExportStamp) get() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.at
}

// WriteFileAtomic writes raw to path through a temp file and a rename, so a
// reader never observes a half-written document — and without changing what
// the file IS.
//
// The plain form of this gets two things wrong on a file the user already
// owns. A temp file is created 0600, so renaming it over a document the user
// had made group- or world-readable silently locks it down. And a rename onto
// a SYMLINK replaces the link with a regular file, detaching a document that
// was deliberately reached through one — a doc symlinked into a notes tree
// becomes two divergent files.
//
// So: resolve the path first and write through the link, and carry the
// existing file's permission bits onto the replacement. A file that does not
// exist yet is created 0644 (before umask), the ordinary default for a
// document rather than the private default of a temp file.
//
// One residual, deliberately: a rename detaches path from its inode, so a
// HARD link to the same document keeps the old content under its other name.
// Preserving that would mean truncating and rewriting in place, which gives up
// the atomicity this function exists for. A torn document is worse.
func WriteFileAtomic(path string, raw []byte) error {
	target, perm, err := resolveWriteTarget(path)
	if err != nil {
		return err
	}
	// The temp/fsync/rename itself is the family's (tools.WriteFileAtomic);
	// what stays here is galley's policy about WHICH path and WHICH mode.
	return tools.WriteFileAtomic(target, raw, perm)
}

// defaultFileMode is what a file this package CREATES gets. 0644 before umask
// — a document, not a secret. An existing file's own mode always wins.
const defaultFileMode os.FileMode = 0o644

// resolveWriteTarget reports the real path to rename onto and the permission
// bits the result must end up with. A missing file is not an error: it is the
// first projection.
func resolveWriteTarget(path string) (string, os.FileMode, error) {
	st, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return path, defaultFileMode, nil
	case err != nil:
		return "", 0, err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return path, st.Mode().Perm(), nil
	}
	// Write THROUGH the link, at whatever it finally points at, with that
	// file's mode — so the link survives and keeps meaning what it meant.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A dangling link: there is nothing to preserve and nothing to
		// resolve to, so treat the link itself as the destination.
		return path, defaultFileMode, nil //nolint:nilerr // a broken link is a first write, not a failure to report
	}
	rst, err := os.Stat(resolved)
	if err != nil {
		return "", 0, err
	}
	return resolved, rst.Mode().Perm(), nil
}

func writeFileAtomic(path string, raw []byte) error { return WriteFileAtomic(path, raw) }
