package suggest

import (
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
)

// detach.go is the document half of DELETING a thread — the verb that removes
// a comment which no longer applies, as opposed to resolving one, which settles
// it and keeps every word of it.
//
// A thread leaves exactly one trace in the document, and which one depends on
// its anchor:
//
//   - RANGE: a Highlight mark on the prose it covers, on every piece that
//     carries the comment's ID. Deleting drops the mark from each piece and
//     keeps the text.
//   - BLOCK or DOCUMENT: a docmodel.Note block, which IS the note. There is no
//     mark to lift; the file is where those words live, and removing the block
//     is the only way to take them out of it. This is the one path in galley
//     that removes a {>>…<<} a human typed, and it exists precisely so that
//     nothing else has to.
//
// The sidecar half — dropping the conversation itself — is review.Session.
// Delete, and every caller does both.

// PairFor reports the comment highlight a text comment's thread is about: the
// comment span whose ID is the thread's key.
//
// THE ID AND NOTHING ELSE. The key is minted once (unsent.NewID), stamped on
// every piece of the highlight and written into the file after each one, so
// pairing is equality. It used to recompute a digest of the highlighted words
// and then fall back to matching the thread's heading against them, which lost
// a comment the moment the reviewer edited a word inside its own highlight
// (bug 3) and could hand one comment's conversation to another's identical
// words.
//
// A thread whose key is no span's ID has no place in this document, and the
// honest answer is "unplaced", whatever its heading says.
//
// RANGE anchors only. A block or document comment's trace is a docmodel.Note,
// paired through matchNotes, not here.
//
// It is a function over the pending LIST rather than over the document because
// every caller already has one — the serve layer's instruction builder and its
// lost-anchor record — and listing twice is the per-item rescan this package
// has paid for before.
func PairFor(pending []Pending, t review.Thread) (Pending, bool) {
	if t.Key == "" {
		return Pending{}, false
	}
	for _, p := range pending {
		if p.Kind == KindComment && p.Anchor == AnchorRange && p.CommentID == t.Key {
			return p, true
		}
	}
	return Pending{}, false
}

// Detach removes from d whatever trace the thread named by key left there, and
// reports whether it found one.
//
// NOT FINDING ONE IS AN ORDINARY OUTCOME, not an error: the note may have been
// deleted by hand, the highlighted words edited away, or the thread may never
// have had a mark to begin with. The honest answer then is to change nothing
// and say so — the caller still drops the conversation, and removing something
// merely adjacent is the outcome nothing can undo.
func Detach(d docmodel.Doc, threads []review.Thread, key string) (docmodel.Doc, bool) {
	var t review.Thread
	found := false
	for _, th := range threads {
		if th.Key == key {
			t, found = th, true
			break
		}
	}
	if !found {
		return d, false
	}

	if t.Anchor == string(AnchorBlock) || t.Anchor == string(AnchorDocument) {
		notes, paths := notesWithPaths(d)
		matched := matchNotes(notes, threads)
		for i, j := range matched {
			if j >= 0 && threads[j].Key == key {
				return removeBlockAt(d, paths[i]), true
			}
		}
		return d, false
	}

	return liftComment(d, key)
}

// liftComment drops the comment highlight carrying id from EVERY inline of
// every block, and reports whether there was one. One comment can be several
// pieces — a selection across paragraphs, or around a code span — and every
// piece carries the ID, so lifting by the ID takes them all and nothing else.
// The words stay: the highlight is over the author's prose. By ID, where
// Accept and Reject lift one span named by its run or ordinal (applyDecision):
// a decision is about the span on a card, and this removes the comment, which
// is every span carrying its ID.
func liftComment(d docmodel.Doc, id string) (docmodel.Doc, bool) {
	if id == "" {
		return d, false
	}
	clone := cloneDoc(d)
	lifted := false
	docmodel.Walk(clone, func(_ []int, b *docmodel.Block) {
		if b.Kind == docmodel.Note {
			return
		}
		for i := range b.Inlines {
			in := &b.Inlines[i]
			if in.Attr(docmodel.Highlight, docmodel.CommentIDAttr) != id {
				continue
			}
			var kept []docmodel.Mark
			for _, m := range in.Marks {
				if m.Kind == docmodel.Highlight && m.Attrs[docmodel.CommentIDAttr] == id {
					continue
				}
				kept = append(kept, m)
			}
			in.Marks = kept
			lifted = true
		}
	})
	if !lifted {
		return d, false
	}
	return clone, true
}

// Reword rewrites the words of the note the thread named by key left in the
// document, and reports whether it found one.
//
// It is Detach's sibling and shares its whole argument about where a thread's
// words actually live. A RANGE thread keeps its words in the sidecar alone —
// the document holds only a Highlight over prose the reviewer did not write —
// so there is nothing here to reword and this answers false, correctly and
// without changing anything. A BLOCK or DOCUMENT thread's words ARE a
// docmodel.Note block in the file, and rewriting that block is the only way an
// edit reaches the .md at all. Not finding a note is an ordinary outcome for
// the same reasons Detach lists: it may have been deleted by hand, or the
// thread may never have had one.
//
// THE THREAD'S KEY IS NOT RECOMPUTED, AND THAT IS DELIBERATE. A note's key
// digests the note's own words (CommentKeyFor), so rewording one changes what
// its key WOULD be — but the sidecar is upserted by the key it already has, and
// ReconcileNotes pairs a note back onto its thread on Entries[0].Text, never on
// the key. So as long as the caller writes the same new text into both halves
// in ONE mutation, the pairing holds and the old key stays reachable, which is
// exactly the migration story CLAUDE.md records for keys that have drifted
// before. Re-keying here would orphan every reply already in the conversation.
func Reword(d docmodel.Doc, threads []review.Thread, key, text string) (docmodel.Doc, bool) {
	var t review.Thread
	found := false
	for _, th := range threads {
		if th.Key == key {
			t, found = th, true
			break
		}
	}
	if !found {
		return d, false
	}
	if t.Anchor != string(AnchorBlock) && t.Anchor != string(AnchorDocument) {
		return d, false
	}
	notes, paths := notesWithPaths(d)
	matched := matchNotes(notes, threads)
	for i, j := range matched {
		if j < 0 || threads[j].Key != key {
			continue
		}
		clone := cloneDoc(d)
		b, ok := blockAt(clone, paths[i])
		if !ok {
			return d, false
		}
		// One unmarked inline, which is what every note this package writes
		// holds: a note's words are prose the reviewer typed into a box, never
		// marked-up document text, and ydoc.writeBlock crosses it as a single
		// YXmlText run.
		b.Inlines = []docmodel.Inline{{Text: text}}
		return clone, true
	}
	return d, false
}
