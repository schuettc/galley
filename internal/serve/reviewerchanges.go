package serve

import (
	"strings"

	"github.com/subaud-tools/galley/internal/diff"
	"github.com/subaud-tools/galley/internal/versions"
)

// ReviewerChange is one thing the reviewer changed in the document BY HAND,
// as distinct from an instruction they wrote about it.
//
// WHY THIS EXISTS, measured rather than supposed. On 2026-08-22 a reviewer
// deleted a section in round 3 and the agent's round 4 put it back inside an
// 8k expansion. The version history proves the deletion worked: the section is
// absent from v3, the round the reviewer sent, and present again in v4, the
// agent's landing. The agent had no way to know — the round notification it
// received carried only instructions, and closed by telling it not to read
// `galley pending`.
//
// AND THE TRAIL IT WOULD HAVE READ THERE DOES NOT EXIST. `PendingView` had no
// changes field; the `/_galley/trail` route the browser's own
// `SerializedTrailEntry` type documents is in no Go file; and `trailChanges`
// is a doc comment with no function under it. The reviewer's hand edits were
// published to nothing, so this is not "the notification omits what pending
// has" — nothing had it.
//
// COMPUTED FROM TWO VERSIONS, NEVER FROM THE BROWSER'S GHOSTS. The trail in
// `web/trail.ts` deliberately REFUSES to place a ghost it cannot anchor
// unambiguously — correct for anchoring, where a wrong mark is worse than a
// missing one, and fatal here, because a refused ghost would be an unreported
// deletion. A document diff cannot refuse. It also makes the reviewer's side
// symmetric with the agent's, where the contract already says galley owns the
// computed list and the agent's entries only annotate it.
type ReviewerChange struct {
	// Key ADDRESSES ONE CHANGE, and it is derived from content rather than
	// position. The list is recomputed on every poll, so an ordinal would
	// renumber the moment an earlier edit was reverted — this repository's
	// oldest rule, and the card holding a stale one would revert the wrong
	// passage. Two identical changes are genuinely one thing to this key, and
	// reverting either is the same act.
	//
	// Empty on the round handed to the AGENT: an agent never addresses a change
	// (it is told what moved, not asked to undo it), and a key on that payload
	// would be a field nobody reads.
	Key string `json:"key,omitempty"`
	// Kind is "removed", "changed" or "added" — the reviewer's own edit, in
	// the vocabulary the agent needs to act on. "removed" is the one that
	// costs when it is missing.
	Kind string `json:"kind"`
	// Before is the text as it stood. Empty on an addition.
	Before string `json:"before,omitempty"`
	// After is the text as it stands now. Empty on a removal.
	After string `json:"after,omitempty"`
}

// reviewerChangeCap bounds what one round reports.
//
// A NUMBER, not a shrug: a reviewer who reworks a whole draft by hand can
// produce hundreds of units, and a notification that long buries the
// instructions it exists to carry. Past the cap the round says how many more
// there were rather than silently stopping — a truncation nobody is told about
// reads as "that is all of them", which is the failure this repository records
// under silent caps.
const reviewerChangeCap = 40

// reviewerChanges reports what the reviewer changed by hand between the round
// being sent and the one before it.
//
// It returns nothing rather than an error on every unreadable path. A version
// that cannot be read must not fail a round — the ledger's rule, for the
// ledger's reason: a read-only checkout must not turn a Revise press into an
// error page. The cost of returning nothing is an agent told less than it
// could have been; the cost of returning an error is a review that stops.
func ReviewerChanges(store *versions.Store) ([]ReviewerChange, int) {
	if store == nil {
		return nil, 0
	}
	latest, ok, err := store.Latest()
	if err != nil || !ok || latest.N < 2 {
		return nil, 0
	}
	// Only a round the REVIEWER sent describes the reviewer's hand. An
	// agent's landing diffed the same way would report the agent's work as
	// the reviewer's, which is the attribution inversion this whole handover
	// exists to prevent.
	if latest.Reason != versions.ReasonRevise {
		return nil, 0
	}
	after, err := store.Content(latest.N)
	if err != nil {
		return nil, 0
	}
	before, err := store.Content(latest.N - 1)
	if err != nil {
		return nil, 0
	}
	return summarise(diff.Diff(before, after))
}

// summarise turns diff ops into the reviewer-facing vocabulary, and reports
// how many it left out.
//
// IT FILTERS RATHER THAN DEDUPES, and the difference is not cosmetic.
//
// The first cut of this file deduped identical entries, on the reading that a
// deleted paragraph produced two indistinguishable delete ops. They are
// distinguishable — the second carries `Absorbed` — and I had not looked.
// Worse, the dedupe was WRONG IN A WAY THE FILTER IS NOT: a document with two
// identical paragraphs in different places, both deleted, is two removals, and
// collapsing them under-reports the reviewer's own edits to the agent. That is
// the exact harm this file exists to prevent, reintroduced by a workaround for
// a misreading of it.
func summarise(ops []*diff.Op) ([]ReviewerChange, int) {
	var out []ReviewerChange
	var dropped int
	for _, op := range ops {
		// THE HEAD A BOUNDARY HANDED ITS PLACE TO. `coalesceBlocks` gives a
		// block's opening sentinel's place to the first real unit under it and
		// marks the superseded op Absorbed, so a deleted paragraph arrives as
		// two delete ops carrying the same text. render.go filters this twice,
		// manifest.go filters it, diff.go's own walk filters it. This is the
		// documented shape of the list, not an anomaly in it.
		if op.Absorbed {
			continue
		}
		// A BOUNDARY IS NOT PROSE. `Units` opens every block with a sentinel
		// whose text is "\x00para" or "\x00heading", and a deleted block yields
		// a delete op for it. Measured before this guard: a round told the
		// agent `REMOVED (2): "Cut this line." · "\x00para"` — an internal
		// marker handed over as the reviewer's own words. The empty-text guard
		// below does NOT catch it, because a sentinel is not empty.
		if isBoundary(op.Old) || isBoundary(op.New) {
			continue
		}
		var c ReviewerChange
		switch op.Op {
		case diff.OpDelete:
			if op.Old == nil {
				continue
			}
			c = ReviewerChange{Kind: "removed", Before: strings.TrimSpace(op.Old.Text)}
		case diff.OpChange:
			if op.Old == nil || op.New == nil {
				continue
			}
			c = ReviewerChange{
				Kind:   "changed",
				Before: strings.TrimSpace(op.Old.Text),
				After:  strings.TrimSpace(op.New.Text),
			}
		case diff.OpInsert:
			if op.New == nil {
				continue
			}
			c = ReviewerChange{Kind: "added", After: strings.TrimSpace(op.New.Text)}
		default:
			// Equal, and anything this vocabulary does not know. A moved
			// sentence is deliberately not reported: it is neither removed nor
			// added, and telling an agent a passage was "removed" when it is
			// still in the document a screen away is exactly the wrong thing
			// to say to something about to decide whether to write it back.
			continue
		}
		// A boundary unit carries no prose; an empty summary line tells the
		// agent nothing and costs it attention.
		if c.Before == "" && c.After == "" {
			continue
		}
		if len(out) >= reviewerChangeCap {
			dropped++
			continue
		}
		out = append(out, c)
	}
	return out, dropped
}

// isBoundary reports whether a unit is a block-opening sentinel rather than
// prose. Nil is not a boundary — an insert has no Old and a delete has no New.
func isBoundary(u *diff.Unit) bool { return u != nil && u.Kind == diff.KindBoundary }
