// handoff.go is the ownership window: while a lease exists, the .md file
// belongs to the AGENT — the browser is read-only, projection does not write
// the file, and a watcher imports the agent's saves into the live document.
// Outside a window nothing here runs: the save decision (saveLocked) loads an
// outside change or keeps it aside, and never writes over it in silence.
package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/ondisk"
)

// handoffLeaseVersion is the lease's schema generation, written as `v`.
//
// THE LEASE IS THE ONE PRIVATE FORMAT AND IT FAILS CLOSED BOTH WAYS — a key
// this build does not know and a version newer than this one are each refused,
// and readLease is the only reader. That is affordable here and nowhere else in
// this change, for three reasons stated together because no one of them would
// be enough:
//
//   - ONE WRITER, ONE READER, ONE WORKING DIRECTORY. The file is
//     `.galley/versions/<doc>/handoff.json`, gitignored, written by the
//     `galley edit` that opened the document and read only by the next run of
//     the same command over the same document. Nothing scans it, nothing
//     fetches it, no other build has any business in it.
//   - REFUSING COSTS A RESUME; BELIEVING COSTS THE ROUND. A rename here is
//     invisible today: resumeHandoff reads Round 0, an empty Fingerprint and a
//     false ApproveOnAnswer, so the agent's in-flight work is resumed as a
//     window answering nothing, which is the exact laundering the lease exists
//     to prevent. The file's own comments already say what a type error costs
//     and what a rename does not.
//   - THE QUARANTINE ALREADY EXISTS. editmode.go renames an unreadable lease to
//     `handoff.json.unreadable` and starts normally, so a refusal preserves the
//     evidence rather than destroying it, and the window is rebuilt by the
//     reviewer taking the document back.
//
// A lease with no `v` is generation 1 — the shape this field was added to — so
// a window open across the upgrade still resumes.
const handoffLeaseVersion = 1

// handoffLease is the window made durable. It is written on every transition
// so a restart mid-response reopens the window instead of cutting the agent's
// draft as an anonymous `opened` round.
type handoffLease struct {
	// V is the schema generation — see handoffLeaseVersion.
	V int `json:"v"`
	// Round is the reviewer round being answered — reviseWatch.round persisted.
	Round int `json:"round"`
	// Baseline is the digest of the last projection before the window opened:
	// what "the agent changed nothing" is measured against.
	Baseline    string    `json:"baseline"`
	Fingerprint string    `json:"fingerprint"`
	OpenedAt    time.Time `json:"openedAt"`
	// LastImported is the digest of the last file content successfully loaded
	// into the live document, so the watcher can skip a file it has already
	// imported.
	LastImported    string `json:"lastImported,omitempty"`
	ApproveOnAnswer bool   `json:"approveOnAnswer,omitempty"`
}

// leasePath lives beside the round manifest: the lease is round state.
func (s *EditServer) leasePath() string {
	return filepath.Join(s.Versions().Dir(), "handoff.json")
}

// openHandoff makes the response window durable and hands the file to the
// agent. It takes reviseMu itself; call it with neither lock held —
// openResponseWindow calls it after the watch is set.
func (s *EditServer) openHandoff(round int, fp string, approveOnAnswer bool) {
	baseline := s.projectedDigest()
	l := &handoffLease{
		V:     handoffLeaseVersion,
		Round: round, Baseline: baseline, Fingerprint: fp,
		OpenedAt: time.Now().UTC(), ApproveOnAnswer: approveOnAnswer,
	}
	s.reviseMu.Lock()
	s.handoffLease = l
	s.reviseMu.Unlock()
	if err := writeLease(s.leasePath(), l); err != nil && s.Log != nil {
		// The ledger's rule: a lease that could not be written may not fail the
		// round. The window still works for this process's lifetime; only
		// restart recovery is lost, and the log says so.
		s.Log(fmt.Sprintf("could not persist the handoff window: %v", err))
	}
	s.handoffLive.Store(true)
	s.startWatcher()
	if s.Log != nil {
		s.Log(fmt.Sprintf("%s is the agent's until it returns — the browser is read-only", filepath.Base(s.MdPath)))
	}
}

// closeHandoff returns file ownership to the live document. It does NOT cut
// anything — callers cut after closing, so the cut's projection is the write
// that puts the canonical document back on disk.
func (s *EditServer) closeHandoff() {
	s.handoffLive.Store(false)
	_ = s.stopWatcher()
	s.reviseMu.Lock()
	s.handoffLease = nil
	s.reviseMu.Unlock()
	if err := clearLease(s.leasePath()); err != nil && s.Log != nil {
		s.Log(fmt.Sprintf("could not clear the handoff lease: %v", err))
	}
}

// resumeHandoff reopens a window a previous run left open — a restart mid
// -response. The on-disk file is the agent's draft: NewEdit parsed it into the
// live document like any startup, so the resume only restores the window's
// state and attribution, never the content.
func (s *EditServer) resumeHandoff(l *handoffLease) {
	s.reviseMu.Lock()
	s.watch = &reviseWatch{at: l.OpenedAt, fingerprint: l.Fingerprint, round: l.Round}
	s.approveOnAnswer = l.ApproveOnAnswer
	s.handoffLease = l
	s.reviseMu.Unlock()
	if l.LastImported != "" {
		// Imports already happened in the previous run; who moved the document
		// must survive the restart, or the resumed round loses its author.
		s.mu.Lock()
		s.markMovedByAgent()
		s.mu.Unlock()
		s.openApplied("")
	}
	s.handoffLive.Store(true)
	s.startWatcher()
	if s.Log != nil {
		s.Log(fmt.Sprintf("resumed the agent's window for round %d", l.Round))
	}
}

func (s *EditServer) handoffOpenNow() bool { return s.handoffLive.Load() }

// refuseHandoff is the reviewer-side lock: while the agent holds the file,
// every reviewer mutation of the document is refused the way a sealed one is.
// The agent's own return paths (ack, cannot) and the cancel are the doors and
// deliberately do not ask this.
func (s *EditServer) refuseHandoff(w http.ResponseWriter) bool {
	if !s.handoffOpenNow() {
		return false
	}
	http.Error(w, "the agent is revising — the document is read-only until it returns (or cancel the handoff)",
		http.StatusConflict)
	return true
}

// handleHandoffCancel is the reviewer taking the document back. A parseable
// draft is imported first — partial work is still work — and an unparseable
// one is preserved under .galley/recovery/ before the projection resumes and
// restores the canonical document over it.
func (s *EditServer) handleHandoffCancel(w http.ResponseWriter, r *http.Request) {
	if !guard(w, r) {
		return
	}
	if !s.handoffOpenNow() {
		http.Error(w, "no handoff is open", http.StatusConflict)
		return
	}
	// RESCUED IS THE ONE LICENCE TO WRITE OVER A FILE GALLEY DID NOT MAKE: the
	// draft is kept in .galley/recovery/, so the projection below may restore
	// the canonical document over it, and over those bytes only (their
	// digest): a save that lands after the rescue is not kept anywhere yet, so
	// it goes through the ordinary decision. A draft that could not be kept is
	// left on disk, as any unreadable file is.
	rescued := ""
	if _, err := s.importDraft(); err != nil {
		if saved, digest, serr := s.rescueDraft(); serr == nil {
			rescued = digest
			if s.Log != nil {
				s.Log("the draft could not be imported; preserved at " + saved)
			}
		}
	}
	s.reviseMu.Lock()
	s.watch = nil
	s.approveOnAnswer = false
	s.reviseMu.Unlock()
	s.closeHandoff()
	s.setDraftError("")
	// Whatever WAS imported is the agent's round; a cancel with nothing
	// imported cuts nothing (cutApplied no-ops on a nil accumulator), so the
	// explicit projection below is what restores the canonical document.
	s.cutApplied()
	s.mu.Lock()
	err := s.projectAs(rescued)
	s.mu.Unlock()
	if err != nil && s.Log != nil {
		s.Log("could not restore the canonical document after the cancel: " + err.Error())
	}
	if s.Log != nil {
		s.Log("handoff cancelled — the document is the reviewer's again")
	}
	w.WriteHeader(http.StatusNoContent)
}

// rescueDraft keeps the file as it is now and returns where, and the digest
// of what it kept.
func (s *EditServer) rescueDraft() (string, string, error) {
	raw, err := os.ReadFile(s.MdPath)
	if err != nil {
		return "", "", err
	}
	saved, err := s.rescue(raw)
	return saved, fileDigest(raw), err
}

// rescue keeps raw under .galley/recovery/ beside the document and returns
// where. The handoff cancel keeps an unimportable draft there; the save keeps
// a file it is about to write over (a clash) and the unsaved edits of a file
// that went missing. Never over an earlier rescue: two in one second get a
// suffix.
func (s *EditServer) rescue(raw []byte) (string, error) {
	dir := filepath.Join(filepath.Dir(s.MdPath), ".galley", "recovery")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stem := fmt.Sprintf("%s-%s", strings.TrimSuffix(filepath.Base(s.MdPath), ".md"),
		time.Now().UTC().Format("20060102T150405Z"))
	for n := 1; ; n++ {
		name := stem + ".md"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.md", stem, n)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(raw); err != nil {
			_ = f.Close()
			return "", err
		}
		return path, f.Close()
	}
}

// appliedOpen is whether the agent's round accumulator holds anything —
// whether any import (or, historically, apply) has landed this window.
func (s *EditServer) appliedOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied != nil
}

func (s *EditServer) projectedDigest() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fileDigest(s.baseBytes)
}

// importPoll is how often the watcher looks at the file while a window is
// open. One file, a bounded window, a digest compare per tick: cheap enough
// that a dependency-free poll beats teaching the binary fsnotify.
const importPoll = 300 * time.Millisecond

// startWatcher begins the import loop. Idempotent: a second open while one
// loop runs keeps the running loop.
func (s *EditServer) startWatcher() {
	s.reviseMu.Lock()
	if s.handoffStop != nil {
		s.reviseMu.Unlock()
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	s.handoffStop, s.handoffDone = stop, done
	tickHook := s.testImportTick
	s.reviseMu.Unlock()
	go func() {
		defer close(done)
		tick := time.NewTicker(importPoll)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				if tickHook != nil {
					tickHook()
				}
				// An error here is HELD state, not a failure — the draft stays
				// on disk, the readout says so, and the next save retries.
				_, _ = s.importDraft()
			}
		}
	}()
}

// stopWatcher tells the watcher to stop and returns a channel closed once its
// goroutine has exited (nil when none was running). Only Close waits on it: an
// import still under way when the window closes is already harmless (a closed
// window gets no import mark; see importDraft), but one still under way when
// the server shuts down would write after the caller was told it had finished.
// Never wait on it from the watcher's own goroutine.
func (s *EditServer) stopWatcher() <-chan struct{} {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	done := s.handoffDone
	if s.handoffStop != nil {
		close(s.handoffStop)
		s.handoffStop, s.handoffDone = nil, nil
	}
	return done
}

// importDraft loads the agent's saved file into the live document as one
// agent-attributed mutation. It answers (false, nil) when there is nothing new
// to import, (false, err) when the draft cannot be read or parsed — in which
// case the draft is HELD: the live document keeps its last good state and the
// file is not overwritten — and (true, nil) on success.
func (s *EditServer) importDraft() (bool, error) {
	// A VALUE, NOT THE POINTER. reviseMu guards the lease's FIELDS, and the
	// only way a reader can carry that guarantee past the unlock is to leave
	// with a copy — the watcher ticks this every importPoll while any other
	// caller may be writing LastImported through the shared struct below.
	// Taking `l := s.handoffLease` here and reading l.LastImported after the
	// unlock is what TestTheLeaseIsReadUnderMu reproduces.
	s.reviseMu.Lock()
	open := s.handoffLease != nil
	var l handoffLease
	if open {
		l = *s.handoffLease
	}
	s.reviseMu.Unlock()
	if !open {
		return false, nil
	}
	digest, fresh, err := s.loadDraft(l)
	if err != nil {
		s.setDraftError(err.Error())
		return false, err
	}
	if !fresh {
		return false, nil
	}
	s.setDraftError("")
	// Every import extends ONE uncommitted agent round — the same accumulator
	// the return's terminal ack cuts. See versions.go's appliedRound.
	s.openApplied("")
	// Same rule on the way out: writeLease json.Marshals what it is handed, so
	// handing it the shared pointer reads every field with no lock held. It
	// takes the snapshot instead.
	//
	// AND A CLOSED WINDOW NO LONGER GETS AN IMPORT MARK. The old code kept the
	// pointer read at the top of the function, so a lease closed underneath
	// this call still had its stale copy written back — recreating, after
	// closeHandoff had deleted it, a lease file for a window nobody is in.
	var snap handoffLease
	marked := false
	s.reviseMu.Lock()
	if s.handoffLease != nil {
		s.handoffLease.LastImported = digest
		snap, marked = *s.handoffLease, true
	}
	s.reviseMu.Unlock()
	if marked {
		if err := writeLease(s.leasePath(), &snap); err != nil && s.Log != nil {
			s.Log(fmt.Sprintf("could not persist the import mark: %v", err))
		}
	}
	if s.Log != nil {
		s.Log("imported the agent's save")
	}
	return true, nil
}

// loadDraft reads the agent's file and, when it is new, loads it into the
// live document and makes it the baseline: the draft is now what the file and
// the live document agree on, so the save after the window closes finds
// nothing moved and writes nothing over the agent's spelling. It answers the
// file's digest and whether it was new. UNDER mu FROM THE READ TO THE
// BASELINE, so a save cannot land between them.
func (s *EditServer) loadDraft(l handoffLease) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.MdPath)
	if err != nil {
		return "", false, err
	}
	digest := fileDigest(raw)
	if digest == l.Baseline || digest == l.LastImported {
		return digest, false, nil
	}
	// THE DRAFT REPLACES THE MODEL, AND NOTHING ELSE. Comments the agent types
	// into the file are not imported, by design: an inline {>>…<<} the parse
	// lifts is discarded and leaves the .md at the next projection, a
	// standalone one stays in the draft as the note it reads as, and no
	// thread is opened for either. The reviewer's own
	// comments are untouched: their words are in pending.json, and the ID
	// marks the agent kept place them.
	model, _, err := markdown.Parse(raw)
	if err != nil {
		return "", false, err
	}
	live, err := s.loadLocked(model)
	if err != nil {
		return "", false, err
	}
	s.setBaseline(live, raw)
	return digest, true, nil
}

// setDraftError and draftError carry the one sentence the readout needs when
// the agent's save cannot be imported. Under reviseMu because the reader is
// writeReviseState, which already holds it.
func (s *EditServer) setDraftError(msg string) {
	s.reviseMu.Lock()
	s.draftErr = msg
	s.reviseMu.Unlock()
}

func (s *EditServer) draftError() string {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	return s.draftErr
}

func writeLease(path string, l *handoffLease) error {
	// Stamped on the way out rather than trusted from the caller: importDraft
	// writes back a snapshot it took off the shared struct, and a lease resumed
	// from a v0 file would otherwise be rewritten still claiming no version.
	stamped := *l
	stamped.V = handoffLeaseVersion
	raw, err := json.MarshalIndent(stamped, "", "  ")
	if err != nil {
		return err
	}
	// The versions dir is created lazily by the store's first Commit; a window
	// can open before any round has ever been cut, so the lease creates it too.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'))
}

func readLease(path string) (*handoffLease, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var l handoffLease
	// STRICT, and the only strict decode in this change — see
	// handoffLeaseVersion. A key this build does not know means the file was
	// written against a shape this build cannot honour, and honouring half of
	// it resumes a window answering the wrong round.
	if err := ondisk.Strict(raw, &l); err != nil {
		return nil, false, fmt.Errorf("unreadable handoff lease %s: %w", path, err)
	}
	if ondisk.Future(l.V, handoffLeaseVersion) {
		return nil, false, ondisk.Newer("the handoff lease "+path, l.V, handoffLeaseVersion,
			"the window is not resumed and the file is kept beside the store")
	}
	return &l, true, nil
}

func clearLease(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
