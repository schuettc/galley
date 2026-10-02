package serve

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/schuettc/galley/internal/diff"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/suggest"
)

// WHAT THE REVIEWER HAS CHANGED BY HAND SINCE THE LAST ROUND, computed live.
//
// ONE SURFACE, ALL OF IT. Court: "the list on the right rail is the list of
// instructions going to be revised. we don't need a second list or surface…
// one surface. all of the instructions."
//
// The rail held instruction threads and nothing else, so it showed half of what
// a round carries: the reviewer's own edits are sent too — that is what the
// round's `changes` are for, and without them an agent rewrites the reviewer's
// deletions back — and there was no surface for them anywhere. The recorded
// design said so out loud ("the trail has no surface anywhere") and answered a
// missing half with a SECOND list, reachable from the Revise button. That is
// the two-things-doing-one-job shape this codebase fixes by deletion.
//
// SO THE RAIL BECOMES WHAT THIS ROUND CARRIES, which is the honest description
// of a column that already holds instructions written and not yet sent. Its old
// rule — *here is what needs you, beside the text it is about* — survives the
// change better than it reads: an edit is beside its text more reliably than an
// unplaced instruction is.
//
// IT IS NOT `ReviewerChanges`, AND THE DIFFERENCE IS THE MOMENT. That one
// diffs the last committed ROUND against its predecessor: it describes a round
// already cut, and is what the agent is handed. This diffs the last committed
// version against the LIVE document: it describes a round not yet sent, which
// is the only thing a reviewer can still act on. The two agree at the instant
// of a send, and answer different questions either side of it.
//
// NOTHING WHILE A HANDOFF IS OPEN, and that is not caution. During the window
// the file belongs to the agent, its saves stream into the live document, and
// the browser is read-only — so a diff against the last version would report
// THE AGENT'S in-progress work as the reviewer's own hand. That is the same
// attribution inversion `ReviewerChanges` filters `ReasonRevise` to avoid,
// arriving through the other door.
func (s *EditServer) liveChanges(model docmodel.Doc) ([]ReviewerChange, int) {
	if s.handoffLive.Load() {
		return nil, 0
	}
	store := s.Versions()
	if store == nil {
		return nil, 0
	}
	latest, ok, err := store.Latest()
	if err != nil || !ok {
		return nil, 0
	}
	before, err := store.Content(latest.N)
	if err != nil {
		// A version that cannot be read reports nothing rather than failing
		// the poll — the ledger's rule, for the ledger's reason: a read-only
		// checkout must not turn the reviewer's sidebar into an error.
		return nil, 0
	}
	out, dropped := summarise(diff.Diff(plainOf(before), plainText(model)))
	for i := range out {
		out[i].Key = changeKey(out[i])
	}
	return out, dropped
}

// plainText and plainOf strip the reviewer's own INSTRUCTION MARKUP from both
// sides before they are compared.
//
// AN INSTRUCTION IS NOT AN EDIT, and without this it looked like one. Filing an
// instruction writes a `{==…==}` highlight into the document, so a diff of the
// last version against the live model reported `"Beta two here." → "{==Beta two
// here.==}"` as something the reviewer had changed — and the rail listed it as
// an edit, directly beneath the instruction card it IS. One thing, twice, on
// the one surface that exists so there is only one list.
//
// Worse, it was OFFERED A REVERT: pressing it would have "put back" prose that
// never moved, by deleting the anchor of an instruction the reviewer had just
// written. Measured through the undo gate, whose check was made to prove its
// own precondition and reported `kind: "changed"`, status 200, and a document
// that did not move.
//
// `suggest.ClearInstructions` is the same lift the version seed already uses,
// for the same reason: what is compared has to be the DOCUMENT, not the
// document plus the carriers of the round being composed about it.
func plainText(model docmodel.Doc) string {
	return string(markdown.Serialize(suggest.ClearInstructions(model)))
}

func plainOf(src string) string {
	model, _, err := markdown.Parse([]byte(src))
	if err != nil {
		// A version this build cannot parse is compared as it stands rather
		// than not at all: the diff is still honest about the bytes, and the
		// alternative is a rail that silently reports nothing.
		return src
	}
	return plainText(model)
}

// changeKey names one change so a card can ask for it back. Content-derived
// because an ordinal renumbers the moment anything earlier changes, and a card
// holding a stale one would revert the wrong words.
func changeKey(c ReviewerChange) string {
	sum := sha256.Sum256([]byte(c.Kind + "\x00" + c.Before + "\x00" + c.After))
	return "ch-" + hex.EncodeToString(sum[:8])
}
