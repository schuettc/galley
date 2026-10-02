package serve

import (
	"context"

	"github.com/reearth/ygo/crdt"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

// DELETE THE SENTENCE, DELETE THE INSTRUCTION ABOUT IT.
//
// Court: "if we highlight a sentence and add an instruction and then delete the
// sentence, we should delete the instruction as well."
//
// It is the idiom this codebase already uses one population over. Deleting text
// under an AGENT's pending mark has always been the verdict by hand — the
// proposal vanishes and `suggest.ReplayAttribution` tolerates the absent mark by
// design. An instruction is the reviewer's own mark over their own words, and
// removing those words retracts it.
//
// A TEXT COMMENT'S PLACE IS ITS ID ON A HIGHLIGHT, and nothing else. Editing a
// word inside the highlight keeps the ID, so the comment stays; deleting every
// highlighted word deletes every mark carrying the ID, and that is the one
// event this sweeps.
//
// TWO GUARDS, and they guard different things:
//
//   - ONLY A TEXT COMMENT. `Anchor` is what says so: a block or whole-document
//     comment has no highlight by construction, so it has no mark to lose, and
//     sweeping one would delete every whole-document instruction at the first
//     projection. A block comment whose note disappears stays, unplaced.
//   - ONLY A PLACE THIS SERVER HAS SEEN. "There is no mark now" is not "the
//     reviewer deleted it". A comment restored from pending.json whose mark the
//     .md no longer carries — an edit made while galley was stopped, a file
//     restored from git — also has no mark, and deleting on that evidence
//     would destroy comments nobody touched, at startup, silently.
//     `seenAnchored` records the IDs that HAVE been placed, so the predicate is
//     the one actually wanted: the place was there, and now it is gone. It is
//     per process, and the cost is stated rather than hidden: a comment whose
//     place was already gone before this process started is never swept. It
//     stays an unplaced card, which is the honest rendering.
//
// IT RUNS FROM `project`, which is where the server first learns the reviewer
// moved: a browser edit reaches the CRDT, `doc.OnUpdate` fires, `touch`
// schedules the debounced projection. There is no HTTP hook for typing, and the
// browser must NOT decide this — a thread is transiently unplaced on its first
// paint there, every time, because the projection reaches `/_galley/pending`
// before the websocket reaches the document, and a browser-side sweep would
// delete instructions as they arrived.
//
// IT CHANGES NO TEXT. The highlight went with the words the reviewer deleted, so
// there is nothing left to detach from the document. It deletes the thread from
// the review map and rewrites pending.json, before `project` writes the .md.
//
// AN EARLIER EAGER DELETION HERE CUT SPURIOUS ROUNDS, measured in
// `web/rounds-ux.mjs`, and was backed out for a filter that hid the comment and
// left it in the review map, so a restart brought it back. What is different
// now: the notify fingerprint is taken after this sweep, from the one builder
// every surface uses, so the projection that sweeps already announces the
// comment as gone, and the review-map write moves nothing the notifier
// compares. Re-measured with this write in place: rounds-ux's retraction
// checks (§5.4) pass.

// noteAnchored records which text comments are placed in model: a thread whose
// key is a comment ID on a highlight. Callers hold mu.
func (s *EditServer) noteAnchored(model docmodel.Doc) {
	threads := review.Read(s.doc)
	if len(threads) == 0 {
		return
	}
	pending := suggest.List(model)
	if s.seenAnchored == nil {
		s.seenAnchored = map[string]bool{}
	}
	for _, th := range threads {
		if th.Resolved || th.Anchor != "" {
			continue
		}
		if _, ok := suggest.PairFor(pending, th); ok {
			s.seenAnchored[th.Key] = true
		}
	}
}

// sweepRetracted deletes every text comment this server saw placed whose ID is
// on no mark in model, and rewrites pending.json without it. It writes the
// review map only, through Apply, so peers see the thread go; the fragment is
// untouched. Callers hold mu, and call it before the .md is written.
func (s *EditServer) sweepRetracted(model docmodel.Doc) error {
	if len(s.seenAnchored) == 0 {
		return nil
	}
	pending := suggest.List(model)
	var gone []string
	for _, th := range review.Read(s.doc) {
		if th.Resolved || th.Anchor != "" || !s.seenAnchored[th.Key] {
			continue
		}
		if _, ok := suggest.PairFor(pending, th); !ok {
			gone = append(gone, th.Key)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	s.serverWrites.Add(1)
	defer s.serverWrites.Add(-1)
	if err := s.yjs.Apply(context.Background(), s.Room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			sess := review.Bind(doc, transact)
			for _, key := range gone {
				_ = sess.Delete(key)
			}
		}); err != nil {
		return err
	}
	for _, key := range gone {
		delete(s.seenAnchored, key)
	}
	return s.saveUnsentLocked()
}
