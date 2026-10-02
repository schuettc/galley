package serve

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reearth/ygo/crdt"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/unsent"
)

// THE UNSENT ROUND IS KEPT IN pending.json, beside the document's rounds. The
// CRDT review map is the live state every peer and every reader sees; the file
// is its durable mirror, rewritten under mu inside the same mutation that
// changed the map, so it reaches disk before any projection can write the .md.
// A comment's words, rectangle and kind used to live only in this process, and
// a restart lost every range comment and every figure rectangle. See
// internal/unsent for the format.

// testHookBeforeUnsentCleared runs in sendReviewerRound immediately before the
// unsent round is emptied, so a test can see that the round was recorded
// first.
var testHookBeforeUnsentCleared func()

// unsentStderr is where startup says it moved an unreadable unsent round
// aside: NewEdit has no Log yet when it reads the file.
var unsentStderr io.Writer = os.Stderr

// unsentPath is this document's unsent round.
func (s *EditServer) unsentPath() string { return unsent.Path(s.MdPath) }

// saveUnsentLocked mirrors the review map into pending.json. Callers hold mu,
// and call it after the Apply that changed the map has returned: review.Read
// inside a Transact deadlocks.
func (s *EditServer) saveUnsentLocked() error {
	return unsent.Save(s.unsentPath(), unsent.File{Comments: unsent.FromThreads(review.Read(s.doc))})
}

// mutateUnsent is mutate for a write that changes an instruction: the unsent
// round is rewritten before mu is released, so no projection can put the
// change into the .md first.
//
// A failed save is the caller's error, not the ledger's shrug: a comment that
// could not be stored must not be reported as filed.
func (s *EditServer) mutateUnsent(by requester, fn func(docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error)) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, err := s.mutateLocked(by, fn)
	if err != nil {
		return code, err
	}
	if err := s.saveUnsentLocked(); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("could not store the unsent round: %w", err)
	}
	return code, nil
}

// loadUnsentRound reads the unsent round for the document at abs, for NewEdit.
//
// A newer file is refused and left untouched, so galley does not start: this
// build would overwrite it at the first comment. An unreadable file is moved
// aside to `.unreadable` and the server starts with nothing unsent, the
// handoff lease's precedent; its marks in the .md then match nothing. Any
// other error (permission denied, a directory at the path) is returned: it is
// not evidence of a corrupt file, and moving it is not ours to do.
func loadUnsentRound(abs string) ([]unsent.Comment, error) {
	path := unsent.Path(abs)
	f, err := unsent.Load(path)
	switch {
	case errors.Is(err, unsent.ErrUnreadable):
		aside := path + ".unreadable"
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("%w; and it could not be moved aside: %w", err, rerr)
		}
		_, _ = fmt.Fprintf(unsentStderr, "galley: %v — moved aside to %s, starting with no unsent comments\n", err, aside)
		return nil, nil
	case err != nil:
		return nil, err
	}
	return f.Comments, nil
}

// replayUnsent writes the unsent round's threads into the review map at
// startup. review.Wrap, not Apply: no peer exists yet, as for importNotes.
func replayUnsent(doc *crdt.Doc, threads []review.Thread) {
	if len(threads) == 0 {
		return
	}
	s := review.Wrap(doc)
	for _, th := range threads {
		for _, e := range th.Entries {
			s.Append(th.Key, th.Heading, e.Author, e.Text, e.At)
		}
		s.SetAnchor(th.Key, th.Anchor, th.AnchorKey, th.BlockKind)
		if th.Region != nil {
			s.SetRegion(th.Key, th.Region)
		}
	}
}

// liveInstructions is the live server's instruction list: the review map,
// through the one builder, with retracted range comments hidden.
func (s *EditServer) liveInstructions(model docmodel.Doc) []InstructionView {
	threads := review.Read(s.doc)
	return instructionsOf(unsent.FromThreads(threads), model, anchorKeysOf(threads), s.retracted)
}

// anchorKeysOf maps each thread to the block it sits on. A block comment's
// anchor key is not stored in pending.json: it comes from pairing the comment
// with its note in the parsed file (ReconcileNotes), which is the review map's
// thread, never the unsent comment.
func anchorKeysOf(threads []review.Thread) map[string]string {
	out := map[string]string{}
	for _, th := range threads {
		if th.AnchorKey != "" {
			out[th.Key] = th.AnchorKey
		}
	}
	return out
}

// instructionsOf is THE ONE INSTRUCTION BUILDER. The rail's /_galley/pending,
// `galley wait`, the notify fingerprint, round capture and offline `galley
// pending` all list instructions through it, so none of them can disagree
// about which instructions there are. There were three, and the wait builder
// had no retraction filter: the agent was handed comments the reviewer had
// taken back by deleting their words.
//
// hide drops a comment the live server watched lose its place (see
// noteAnchored); offline it is nil.
func instructionsOf(comments []unsent.Comment, model docmodel.Doc, anchorKeys map[string]string,
	hide func(th review.Thread, paired bool) bool) []InstructionView {
	pending := suggest.List(model)
	threads := unsent.ToThreads(comments)
	out := make([]InstructionView, 0, len(comments))
	for i, c := range comments {
		th := threads[i]
		th.AnchorKey = anchorKeys[c.Key]
		run := ""
		p, paired := suggest.PairFor(pending, th)
		if paired {
			run = p.Run
		}
		// DELETE THE SENTENCE, DELETE THE INSTRUCTION ABOUT IT. See
		// noteAnchored.
		if hide != nil && hide(th, paired) {
			continue
		}
		out = append(out, InstructionView{
			Key: c.Key, Text: strings.TrimSpace(c.Text), Quote: strings.TrimSpace(c.Quote),
			At: c.At.UTC(), Run: run, Anchor: th.Anchor, AnchorKey: th.AnchorKey,
			BlockKind: c.BlockKind, Region: c.Region,
		})
	}
	return out
}

// OfflineInstructions lists the unsent round of the document at mdPath with no
// editor running: pending.json, placed against the .md, through the same
// builder the live server uses.
func OfflineInstructions(mdPath string) ([]InstructionView, error) {
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	model, inline, err := markdown.Parse(raw)
	if err != nil {
		return nil, err
	}
	f, err := unsent.Load(unsent.Path(abs))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	threads := suggest.ImportInlineComments(model, unsent.ToThreads(f.Comments), inline, review.AuthorCourt, now)
	// The file's block and document notes, paired with the unsent round exactly
	// as the live server pairs them at startup, so a note nothing has opened a
	// thread for is still reported, under the same key.
	threads, orphans, _, _ := suggest.ReconcileNotes(model, threads)
	for _, n := range orphans {
		threads = append(threads, suggest.NewNoteThread(n, review.AuthorCourt, now))
	}
	return instructionsOf(unsent.FromThreads(threads), model, anchorKeysOf(threads), nil), nil
}
