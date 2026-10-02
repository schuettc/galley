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
//   - BLOCK: a docmodel.Note carrying the comment's ID, on its own line after
//     the block. There is no mark to lift, so the note itself is removed.
//   - DOCUMENT: nothing. A document comment has no mark in the file.
//
// The other half — dropping the comment itself, and with it its words in
// pending.json — is review.Session.Delete, and every caller does both.

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
// A BLOCK comment pairs the same way, with the Note carrying its ID; the
// Pending then names the block the note sits under (BlockKey), read off the
// note's position, never stored. A document comment has no mark and never
// pairs.
//
// It is a function over the pending LIST rather than over the document because
// every caller already has one — the serve layer's instruction builder and its
// lost-anchor record — and listing twice is the per-item rescan this package
// has paid for before.
func PairFor(pending []Pending, t review.Thread) (Pending, bool) {
	if t.Key == "" {
		return Pending{}, false
	}
	note := t.Anchor == string(AnchorBlock)
	if !note && t.Anchor != "" && t.Anchor != string(AnchorRange) {
		return Pending{}, false
	}
	for _, p := range pending {
		if p.Kind == KindComment && (p.Anchor != AnchorRange) == note && p.CommentID == t.Key {
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

	switch t.Anchor {
	case string(AnchorBlock):
		return removeCommentNote(d, key)
	case string(AnchorDocument):
		return d, false
	}
	return liftComment(d, key)
}

// removeCommentNote removes the Note carrying id, at whatever depth it sits,
// and reports whether there was one. removeBlockAt refills a parent the
// removal would leave empty.
func removeCommentNote(d docmodel.Doc, id string) (docmodel.Doc, bool) {
	if id == "" {
		return d, false
	}
	var at []int
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		if at == nil && b.Kind == docmodel.Note && b.Attrs[docmodel.CommentIDAttr] == id {
			at = append([]int(nil), path...)
		}
	})
	if at == nil {
		return d, false
	}
	return removeBlockAt(d, at), true
}

// liftComment drops the comment highlight carrying id from EVERY inline of
// every block, and reports whether there was one. One comment can be several
// pieces — a selection across paragraphs, or around a code span — and every
// piece carries the ID, so lifting by the ID takes them all and nothing else.
// The words stay: the highlight is over the author's prose.
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
