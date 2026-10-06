package serve

import (
	"maps"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

// DELETE THE SENTENCE, DELETE THE INSTRUCTION ABOUT IT.
//
// Court: "if we highlight a sentence and add an instruction and then delete the
// sentence, we should delete the instruction as well."
//
// It is the idiom this codebase used one population over: deleting text under
// an AGENT's pending mark was the verdict by hand, and the proposal vanished.
// An instruction is the reviewer's own mark over their own words, and removing
// those words retracts it.
//
// A TEXT COMMENT'S PLACE IS ITS ID ON A HIGHLIGHT, and nothing else. Editing a
// word inside the highlight keeps the ID, so the comment stays; deleting every
// highlighted word deletes every mark carrying the ID, and that is the one
// event that retracts the comment.
//
// TWO GUARDS, and they guard different things:
//
//   - ONLY A TEXT COMMENT. `Anchor` is what says so: a block or whole-document
//     comment has no highlight by construction, so it has no mark to lose, and
//     retracting one would hide every whole-document instruction at the first
//     projection. A block comment whose note disappears stays, unplaced.
//   - ONLY A PLACE THIS SERVER HAS SEEN. "There is no mark now" is not "the
//     reviewer deleted it". A comment restored from pending.json whose mark the
//     .md no longer carries — an edit made while galley was stopped, a file
//     restored from git — also has no mark, and retracting on that evidence
//     would drop comments nobody touched, at startup, silently.
//     `seenAnchored` records the IDs that HAVE been placed, so the predicate is
//     the one actually wanted: the place was there, and now it is gone. It is
//     per process, and the cost is stated rather than hidden: a comment whose
//     place was already gone before this process started is never retracted. It
//     stays an unplaced card, which is the honest rendering.
//
// IT RUNS FROM `project`, which is where the server first learns the reviewer
// moved: a browser edit reaches the CRDT, `doc.OnUpdate` fires, `touch`
// schedules the debounced projection. There is no HTTP hook for typing, and the
// browser must NOT decide this — a thread is transiently unplaced on its first
// paint there, every time, because the projection reaches `/_galley/pending`
// before the websocket reaches the document, and a browser-side retraction
// would drop instructions as they arrived.
//
// IT WRITES NOTHING TO THE CRDT. The review map lives in the same Yjs document
// as the text, so a write to it reaches every peer as an update, like a
// keystroke. The comment
// leaves every surface because it leaves THE PENDING VIEW. `liveInstructions`
// hides a seen text comment that has no mark in the model it is reading, and
// every reader goes through it: the rail, `galley wait`, the notify
// fingerprint, SeedNotify and round capture. The thread stays in the review map
// until the next send, whose clear deletes it with the threads it sent (see
// sendReviewerRound). pending.json is the review map MINUS the retracted keys,
// so a restart does not bring the comment back; `project` rewrites it, before
// the .md, whenever the retracted set changes.
//
// WHY NO WRITE, measured on the eager version (6d03cfb), which deleted the
// thread from inside `project`:
//
//   - IT CUT A ROUND NOBODY ASKED FOR. In live mode a settle is a send. An
//     agent write that removed a comment's words was seeded into the notifier
//     by mutate with the comment still counted (its thread was still there);
//     the projection then deleted the thread and handed the notifier a
//     different fingerprint. TestAnAgentWriteThatRemovesACommentsWordsCutsNoRound
//     cut one round on 6d03cfb. The same window broke the fingerprint for any
//     reader between the write and the projection
//     (TestTheWaitPayloadAndTheRailAgree).
//   - AN UNDO LOST THE COMMENT. Deleting the words, letting a projection run,
//     and pressing undo brings back the words and the highlight with its ID,
//     and the thread was already gone: the mark named nothing.
//     TestUndoingTheDeletionBringsTheCommentBack. With no write, the next
//     projection finds the mark again and the comment is back, in the rail and
//     in pending.json.
//   - WHAT THE GATE DID NOT SHOW. 6d03cfb's report re-ran `web/rounds-ux.mjs`
//     with the write in place, 5 green of 6, and read that as the earlier
//     spurious rounds gone. rounds-ux drives the retraction checks (§5.4) in
//     ask mode only, where no settle sends, so it could not have seen a
//     live-mode round. The Go test above is the measurement.
//
// The earlier history, for the record: an eager deletion here was built first
// and backed out once already, for spurious rounds in rounds-ux, and moving
// the write to a goroutine through `mutate` made them worse. The structural
// reason is unchanged: any document write in live mode can become a round.

// noteAnchored records which text comments are placed in model: a thread whose
// key is a comment ID on a highlight. Callers hold mu.
func (s *EditServer) noteAnchored(model docmodel.Doc) {
	threads := review.Read(s.doc)
	if len(threads) == 0 {
		return
	}
	pending := suggest.List(model)
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	if s.seenAnchored == nil {
		s.seenAnchored = map[string]bool{}
	}
	for _, th := range threads {
		if th.Anchor != "" {
			continue
		}
		if _, ok := suggest.PairFor(pending, th); ok {
			s.seenAnchored[th.Key] = true
		}
	}
}

// retractedIn is every text comment this server saw placed that has no mark in
// model, and every one withdrawn across an outside load whatever model says.
// Callers hold mu: it reads the review map, so not inside a Transact.
func (s *EditServer) retractedIn(model docmodel.Doc) map[string]bool {
	pending := suggest.List(model)
	out := map[string]bool{}
	s.anchorMu.Lock()
	seen := maps.Clone(s.seenAnchored)
	withdrawn := maps.Clone(s.withdrawn)
	s.anchorMu.Unlock()
	for _, th := range review.Read(s.doc) {
		if th.Anchor != "" {
			continue
		}
		if withdrawn[th.Key] {
			out[th.Key] = true
			continue
		}
		if !seen[th.Key] {
			continue
		}
		if _, ok := suggest.PairFor(pending, th); !ok {
			out[th.Key] = true
		}
	}
	return out
}

// noteRetracted recomputes the retracted set for model and, only when it moved,
// rewrites pending.json without it. Callers hold mu, and call it before the .md
// is written.
//
// A FAILED SAVE FAILS THE PROJECTION, and the .md is not written: pending.json
// reaches disk before the .md, always, or a restart reads a file whose marks
// name comments pending.json does not hold (an undo's restored highlight) or
// holds comments it should not (a retraction). The set is kept only once it is
// saved, so the next projection tries again.
func (s *EditServer) noteRetracted(model docmodel.Doc) error {
	now := s.retractedIn(model)
	if maps.Equal(now, s.retracted) {
		return nil
	}
	was := s.retracted
	s.retracted = now
	if err := s.saveUnsentLocked(); err != nil {
		s.retracted = was
		return err
	}
	return nil
}

// isRetracted is the pending view's filter: a text comment this server saw
// placed, with no mark in the model being read, or one withdrawn across an
// outside load. It is computed per read and not taken from the last
// projection's set, so a reader between a write and the next projection
// (SeedNotify after an agent's write, handleWait) sees what that projection
// will see. Readers need not hold mu; `pending` does not.
func (s *EditServer) isRetracted(v InstructionView) bool {
	if v.Anchor != "" {
		return false
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	if s.withdrawn[v.Key] {
		return true
	}
	return v.Run == "" && s.seenAnchored[v.Key]
}

// forgetRetractedLocked drops keys the send just deleted from every set.
// Callers hold mu.
func (s *EditServer) forgetRetractedLocked(keys map[string]bool) {
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	for key := range keys {
		delete(s.seenAnchored, key)
		delete(s.withdrawn, key)
		delete(s.retracted, key)
	}
}

// forgetPlacedLocked forgets that any text comment NOT in sent was placed, so a
// mark the send's own clear lifted is never read as a retraction. Callers hold
// mu.
func (s *EditServer) forgetPlacedLocked(sent []string) {
	in := make(map[string]bool, len(sent))
	for _, key := range sent {
		in[key] = true
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	for key := range s.seenAnchored {
		if !in[key] {
			delete(s.seenAnchored, key)
		}
	}
}

// keepCommentsAcrossLocked is the save's half of the same rule page mode's
// reload keeps: A DOCUMENT THAT ARRIVED FROM OUTSIDE IS NOT THE REVIEWER
// MOVING THEIR WORDS, in either direction. Before live is replaced by
// incoming (a load), it settles two things, so the accounting after the load
// reads the reviewer's comments as the reviewer left them:
//
//   - A COMMENT THE OUTSIDE SIDE DROPPED STAYS, UNPLACED. Any text comment
//     placed in live whose mark incoming lacks is forgotten as ever placed, so
//     it reads as unplaced rather than retracted: it stays unsent, on the rail
//     with its quote and in pending.json. A marker that comes back later is
//     tracked again by the next noteAnchored.
//   - A COMMENT THE REVIEWER TOOK BACK STAYS TAKEN BACK. Every comment
//     retracted in live is withdrawn: retracted from now on whatever marks the
//     document carries. Without this, an outside writer restoring a file saved
//     with the marker (an editor's buffer, a git checkout) handed the comment
//     back to pending.json and the agent.
//
// It returns the sets as they were before, for restoreAnchors if the load
// fails, or nil when it changed nothing. Callers hold mu.
func (s *EditServer) keepCommentsAcrossLocked(live, incoming docmodel.Doc) *anchorSnapshot {
	threads := review.Read(s.doc)
	if len(threads) == 0 {
		return nil
	}
	taken := s.retractedIn(live)
	now, next := suggest.List(live), suggest.List(incoming)
	var orphaned []string
	for _, th := range threads {
		if th.Anchor != "" || taken[th.Key] {
			continue
		}
		if _, ok := suggest.PairFor(now, th); !ok {
			continue
		}
		if _, ok := suggest.PairFor(next, th); !ok {
			orphaned = append(orphaned, th.Key)
		}
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	pin := false
	for key := range taken {
		if !s.withdrawn[key] {
			pin = true
		}
	}
	if len(orphaned) == 0 && !pin {
		return nil
	}
	before := &anchorSnapshot{placed: maps.Clone(s.seenAnchored), withdrawn: maps.Clone(s.withdrawn)}
	for _, key := range orphaned {
		delete(s.seenAnchored, key)
	}
	if s.withdrawn == nil {
		s.withdrawn = map[string]bool{}
	}
	maps.Copy(s.withdrawn, taken)
	return before
}

// anchorSnapshot is what keepCommentsAcrossLocked changed, as it was before.
type anchorSnapshot struct {
	placed, withdrawn map[string]bool
}

// restoreAnchors puts back what keepCommentsAcrossLocked changed. nil changes
// nothing.
func (s *EditServer) restoreAnchors(before *anchorSnapshot) {
	if before == nil {
		return
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	s.seenAnchored, s.withdrawn = before.placed, before.withdrawn
}

// placedSnapshot is a copy of which text comments this server has seen placed,
// for a caller that may have to put it back. Safe with or without mu.
func (s *EditServer) placedSnapshot() map[string]bool {
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	return maps.Clone(s.seenAnchored)
}

// restorePlaced puts back a snapshot taken by placedSnapshot. A nil snapshot
// (the caller never reached the point of taking one) changes nothing.
func (s *EditServer) restorePlaced(placed map[string]bool) {
	if placed == nil {
		return
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	s.seenAnchored = placed
}
