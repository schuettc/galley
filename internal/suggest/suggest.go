// Package suggest is the semantic layer over docmodel's Ins/Del/Highlight
// marks: listing pending suggestions, accepting or rejecting them, and
// creating new ones by targeting existing document text. Every function
// here is a pure transform over docmodel.Doc — no ygo, no I/O. The serve
// layer bridges these onto the live document.
package suggest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// Kind identifies what a Pending suggestion would do if accepted.
type Kind string

const (
	KindInsert Kind = "insert"
	KindDelete Kind = "delete"
	// KindReplace is a SUBSTITUTION — "{~~old~>new~~}" — which is one
	// suggestion carrying one decision: accept and the replacement stands,
	// reject and the original does, and neither may leave a half pending.
	//
	// A THIRD KIND, rather than a delete that happens to carry a
	// replacement, because every consumer that branches on Kind has to be
	// made to confront it. They all say "delete this text" for a KindDelete
	// — the rail card's head, the census tally, the arrivals sentence,
	// `galley pending`'s line — and every one of those is the wrong sentence
	// for a substitution. A new value turns each into a visible gap; a
	// widened old value turns each into a lie that reads correctly.
	KindReplace Kind = "replace"
	KindComment Kind = "comment"
)

// Decidable says whether a verdict applies to this kind at all — the ONE
// place that answers it, for every consumer, on both sides of the wire.
//
// A comment is a conversation: it is settled by resolving its thread, and a
// verdict on one would lift the highlight or remove the Note block, deleting
// the reviewer's own words. So it is not a proposal, and no surface may offer
// a verdict on it.
//
// IT IS ONE PREDICATE BECAUSE IT WAS SIX, and the sixth disagreed. Five
// callers, since deleted, each spelled `!= KindComment` separately and
// agreed; the browser's revision receipt spelled NOTHING, paired a revision's
// runs against the whole pending list, and put a comment's run under the
// card's ✓. Pending.Decidable carries this answer onto the wire
// so the browser reads it rather than making a seventh guess.
//
// THAT CARD IS DELETED AND THIS FIELD IS NOT. The receipt was consolidated
// away — one card language in the rail — but it was never the reason the
// answer travels: the browser's rail card loop, its sheet's, and its j/k step
// order all read `Pending.Decidable` and would each be a fresh guess without
// it. Do not "retire" the field on the strength of the card going.
func (k Kind) Decidable() bool { return k != KindComment }

// replaceSep joins a replace suggestion's two halves in its Text, so the
// entry reads as the replacement it is rather than as one side of one.
//
// Text is a DISPLAY string, and this separator can occur in the text
// itself, so nothing may recover the halves by splitting on it:
// Pending.Old and Pending.New carry them unambiguously and are what a
// caller needing the actual strings must use.
const replaceSep = " → "

// Pending is one suggestion (or comment thread anchor) found in a document.
//
// ID is an ordinal computed fresh every List call, in document order:
// "s1", "s2"... for insert/delete suggestions (sharing one counter, so a
// substitution's Del and Ins get consecutive numbers) and "c1", "c2"... for
// comments.
//
// AN ORDINAL IS NOT IDENTITY. It renumbers the moment a suggestion appears
// anywhere earlier in the document, so it is only meaningful against the
// document value it was listed from. NEVER PERSIST ONE. A thread keyed by a comment's ordinal
// silently reattaches itself to different text on the next edit; see
// unsent.NewID for what a comment is keyed by instead.
//
// The JSON tags are lower-case, the spelling every other galley payload uses,
// so an agent reading `galley pending --json` or /_galley/pending never has to
// remember which payload spells a suggestion's id "id" and which "ID".
type Pending struct {
	ID string `json:"id"`
	// Run is the mark's stable in-session identity (docmodel.RunAttr).
	// Unlike ID it does not shift when a neighbouring suggestion resolves, so
	// it is what the editor addresses a mark by.
	//
	// omitempty, because a document that never entered a session genuinely has
	// no runs — the offline CLI path reads straight from the file, where the
	// ordinal is the only coordinate anyone uses. Emitting "run": "" there
	// would advertise an identity that does not exist, and any consumer that
	// believed it would address the wrong mark.
	Run string `json:"run,omitempty"`
	// CommentID is the comment ID a comment span carries in the file
	// (docmodel.CommentIDAttr): the `cm-` ID on a highlight's pieces, or the
	// ID on a note. It IS the comment's key, so pairing a comment with its
	// place is this equality and nothing else. Empty for every proposal, and
	// for a highlight or note typed by hand with no ID.
	CommentID string    `json:"commentId,omitempty"`
	Kind      Kind      `json:"kind"`
	Author    string    `json:"author"`
	At        time.Time `json:"at"`
	// Text is the plain text the suggestion covers: inserted, deleted, or
	// highlighted, with any of the block's other formatting stripped. For a
	// "replace" it is both halves joined — "brown → red" — because a
	// substitution is one change and half of one is not what it says.
	Text string `json:"text"`
	// InsRun is the INSERTED half's run of a "replace", and empty for every
	// other kind. Run, above, is the DELETED half's, and that stays the one
	// coordinate a decision is addressed by — accept and reject take Run and
	// nothing else.
	//
	// THIS IS NOT A SECOND COORDINATE FOR THE DECISION; IT IS THE OTHER HALF'S
	// ADDRESS, and it is on the wire for the reason Decidable is: the side that
	// KNOWS the answer is the side that publishes it. A substitution is one
	// span in the file and one decision everywhere, but in the document — and
	// so in the browser's fragment — it is a Del and an Ins with two runs,
	// because markdown's applyMark is called once per half and correctly stamps
	// each. The browser clicking the GREEN half therefore had a run the wire
	// carried nowhere, matched nothing, and was told "the server has not seen
	// this one yet" about the very suggestion whose card sat 400px away with
	// two working verbs. One span read as three different things depending on
	// where the cursor landed.
	//
	// The browser must not recover the pairing itself: markdown.planSubstitution
	// is the ONE rule that decides what pairs, substitutionSpan asks it by
	// running the serializer's own walk, and a heuristic on the other side of
	// the wire is exactly the second rule CLAUDE.md forbids. So the pairing
	// travels, as one string, and the browser resolves a click through it and
	// then posts Run like every other consumer.
	//
	// omitempty for Run's own reason: a document that never entered a session
	// has no runs at all, and the offline CLI clears both (cmd/galley's
	// withoutRuns) rather than advertising an identity that is already gone.
	InsRun string `json:"insRun,omitempty"`
	// Old and New are a "replace"'s two halves: the text it removes and the
	// text it puts there. Empty for every other kind.
	//
	// They are separate fields rather than something to recover from Text,
	// because Text's separator can occur in the text itself and a consumer
	// that split on it would silently mis-report a suggestion whose words
	// happened to contain an arrow.
	Old string `json:"old,omitempty"`
	New string `json:"new,omitempty"`
	// Context is the containing top-level block, plus its immediate
	// siblings (one before, one after, where they exist), rendered back to
	// markdown with markdown.Serialize — never hand-formatted, so it always
	// reflects exactly what Serialize would write for that neighbourhood.
	Context string `json:"context"`
	// Anchor says what this entry is attached to: "range" for a mark on a run
	// of prose (every insert and delete, and a comment made with --on), and
	// "block" or "document" for a comment that has no run to attach to. It is
	// always present, never omitted — a consumer must be able to branch on it
	// without knowing which galley wrote the payload.
	Anchor AnchorKind `json:"anchor"`
	// Decidable is Kind.Decidable's answer, ON THE WIRE, so the browser learns
	// what accept and reject act on from the server that would answer them
	// rather than re-deriving it. Present on every entry and never omitted,
	// exactly like Anchor: a consumer must be able to branch on it without
	// knowing which galley wrote the payload, and `false` is the answer for a
	// comment rather than the absence of one.
	//
	// It is DERIVED, never stored and never read back in: List computes it
	// from Kind on every call, and nothing accepts it from a caller. A field
	// on the wire that could disagree with the kind beside it would be a
	// second rule again, which is the whole thing this exists to prevent.
	Decidable bool `json:"decidable"`
	// BlockKey names the block a "block" anchor is about — the stable,
	// content-derived key `--on-block` takes. Empty for every other anchor.
	BlockKey string `json:"blockKey,omitempty"`
	// Path is the docmodel.Walk path of the block the suggestion's text
	// lives in. It is opaque outside this package, which is exactly why it
	// is `json:"-"`. Serializing it would publish an internal coordinate an
	// external caller could neither interpret nor safely send back, and a
	// field on the wire is a field someone eventually depends on.
	Path []int `json:"-"`
}

// span is the internal unit every operation in this file works over: a
// maximal run of consecutive inlines within one block that carry the same
// suggestion mark, from the same author at the same instant. List locates
// suggestions by re-deriving these from scratch — there is no persistent
// identity across document values.
// spanPart is one block's share of a span that crosses blocks.
type spanPart struct {
	path       []int
	start, end int
}

type span struct {
	id       string
	path     []int
	kind     Kind
	markKind docmodel.MarkKind
	author   string
	at       time.Time
	// run is docmodel.RunAttr: this mark's stable in-session identity. Part
	// of the span grouping key, not decoration — see listSpans.
	run string
	// commentID is the comment ID the span's mark or note carries; see
	// Pending.CommentID.
	commentID  string
	text       string
	start, end int // inline index range [start, end) within the block's Inlines
	// parts is set only for a span that crosses BLOCKS — one entry per block
	// it touches, in document order, the first of which repeats path/start/end
	// above. Nil for the ordinary single-block span, so every consumer that
	// reads path/start/end keeps working untouched.
	//
	// It exists because `spansForMark` walks block by block: one run over three
	// paragraphs produced three spans, three Pendings and three cards for ONE
	// decision. See coalesceRuns.
	parts []spanPart
	// note is set for a span that is a docmodel.Note BLOCK rather than a run
	// of marked inlines — a block or document comment. start/end/markKind/
	// author/at are all meaningless for one: the file has nowhere to record a
	// note's author (pending.json does), and there is no inline range
	// because the whole block is the anchor.
	note bool
	// ins is the INSERTED half of a KindReplace span, and nil for every
	// other kind. The outer span stays the DELETED half in every respect —
	// path, start, end, author, at, run — so everything that already
	// works over a span keeps working; Pending.InsRun reads this half's run.
	ins *span
	// oldText and newText are the two halves' plain text, kept because text
	// is the joined display form for a replace and the halves cannot be
	// recovered from it.
	oldText, newText string
}

// List returns every pending suggestion in d, in document order.
func List(d docmodel.Doc) []Pending {
	spans := listSpans(d)
	out := make([]Pending, len(spans))
	// One blockKeys pass for the whole call, computed only if a note needs it.
	//
	// blockKeys SERIALIZES EVERY BLOCK to markdown, and this loop used to call
	// it twice per note — once through AnchorFor and once through noteContext.
	// At 50 notes that was 67 ms per List, ~1,600x the same document with none,
	// while the sidebar polls List on a timer and project() calls it on every
	// debounce. See BenchmarkList50Notes.
	var keys []string
	for i, sp := range spans {
		p := Pending{
			ID:        sp.id,
			Run:       sp.run,
			CommentID: sp.commentID,
			Kind:      sp.kind,
			Author:    sp.author,
			At:        sp.at,
			Text:      sp.text,
			Old:       sp.oldText,
			New:       sp.newText,
			Context:   contextFor(d, sp.path),
			Anchor:    AnchorRange,
			Decidable: sp.kind.Decidable(),
			Path:      append([]int(nil), sp.path...),
		}
		// The inserted half's run, where there is one — the pairing this
		// package already made, published rather than left for a consumer to
		// guess at. See Pending.InsRun.
		if sp.ins != nil {
			p.InsRun = sp.ins.run
		}
		if sp.note {
			if keys == nil {
				keys = blockKeys(d)
			}
			a := anchorForKeys(d, sp.path, keys)
			p.Anchor, p.BlockKey = a.Kind, a.Target
			p.Context = noteContext(d, a, keys)
		}
		out[i] = p
	}
	return out
}

// noteContext renders what a block-anchored note is about: the target block
// itself, as markdown. A document comment is about no block, so it has no
// context to show and gets none rather than an arbitrary neighbourhood.
func noteContext(d docmodel.Doc, a Anchor, keys []string) string {
	if a.Kind != AnchorBlock {
		return ""
	}
	for i, k := range keys {
		if k != "" && k == a.Target {
			return string(markdown.Serialize(docmodel.Doc{Blocks: d.Blocks[i : i+1]}))
		}
	}
	return ""
}

// coalesceRuns folds consecutive spans that share a RUN into one.
//
// `spansForMark` walks block by block, so a run stamped across three paragraphs
// — which is exactly what a cross-block comment is (see CommentAcross) —
// arrived as three spans. That would be three Pendings, three cards in the rail
// and three threads' worth of chrome for ONE decision the reviewer made with
// one selection.
//
// A RUN IS THE ONLY THING THAT CAN SAY "THESE ARE ONE DECISION", which this
// package already states about inlines within a block; blocks are the same
// sentence one level up. Nothing else may be used to join them: two comments by
// the same author at the same instant over adjacent paragraphs are two
// decisions and must stay two, and only the run tells them apart.
//
// An empty run never joins anything. A mark read from a file has no run until
// MintRuns gives it one, and folding those together would merge unrelated
// comments that happen to be adjacent.
//
// NOR DOES A DIFFERENT COMMENT ID. Pieces that share an ID share a run (the
// parser draws that boundary from the file), so two IDs under one run are two
// comments however the runs came to agree, and joining them would delete one
// comment's place.
func coalesceRuns(spans []span) []span {
	out := make([]span, 0, len(spans))
	for _, sp := range spans {
		last := len(out) - 1
		if last >= 0 && sp.run != "" && out[last].run == sp.run &&
			out[last].markKind == sp.markKind && !out[last].note && !sp.note &&
			(out[last].commentID == "" || sp.commentID == "" || out[last].commentID == sp.commentID) {
			if len(out[last].parts) == 0 {
				out[last].parts = []spanPart{{path: out[last].path, start: out[last].start, end: out[last].end}}
			}
			out[last].parts = append(out[last].parts, spanPart{path: sp.path, start: sp.start, end: sp.end})
			// Joined with a space, the way this pipeline joins a soft line
			// break everywhere else, so the quote reads as the words the
			// reviewer highlighted rather than as fragments run together.
			out[last].text += " " + sp.text
			continue
		}
		out = append(out, sp)
	}
	return out
}

// CommentOn finds the one place target occurs in d's text and highlights it
// (preserving its existing marks), stamping id on the highlight: the comment's
// identity, minted by the caller (unsent.NewID) and written into the file as
// the mark's ID. target must occur exactly once, or CommentOn fails naming
// how many times it actually matched.
func CommentOn(d docmodel.Doc, target, id, author string, at time.Time) (docmodel.Doc, error) {
	m, err := findUnique(d, target)
	if err != nil {
		return docmodel.Doc{}, err
	}
	return commentAtMatch(d, m, target, id, author, at)
}

// CommentOnRange highlights the rune range [from, to) within the block at path
// and stamps id on it.
//
// This is the editor's path, and the difference from CommentOn is the whole
// point: the browser already knows exactly what the reviewer selected, so
// there is nothing to search for and nothing to be ambiguous about.
// Round-tripping that selection through its text is what made commenting on
// "age" fail, with `"age" matched 2 times, want exactly 1`, in a document that
// also contained the word "image".
//
// Offsets are runes into the block's concatenated inline text — the same
// coordinates plainText, findUnique and markdown.InlineComment.Offset all
// already use.
func CommentOnRange(d docmodel.Doc, path []int, from, to int, id, author string, at time.Time) (docmodel.Doc, error) {
	block, ok := blockAt(d, path)
	if !ok || block == nil {
		return docmodel.Doc{}, fmt.Errorf("suggest: no block at path %v", path)
	}
	n := len([]rune(plainText(block.Inlines)))
	if from < 0 || to > n || from >= to {
		return docmodel.Doc{}, fmt.Errorf(
			"suggest: range [%d,%d) is not inside the block at %v (%d runes)", from, to, path, n)
	}
	m := match{path: append([]int(nil), path...), start: from, end: to}
	target := string([]rune(plainText(block.Inlines))[from:to])
	return commentAtMatch(d, m, target, id, author, at)
}

// commentAtMatch is what both comment paths do once the span is known: refuse
// to double-comment it and highlight it under id. One implementation, so a fix
// to either caller's addressing cannot drift from the other's semantics.
func commentAtMatch(d docmodel.Doc, m match, target, id, author string, at time.Time) (docmodel.Doc, error) {
	if cAuthor, cAt, found := conflictingComment(d, m); found {
		return docmodel.Doc{}, fmt.Errorf("suggest: %q already has a pending comment by %s (at %s)", target, cAuthor, cAt)
	}
	run := newRun()
	clone := cloneDoc(d)
	docmodel.Walk(clone, func(path []int, b *docmodel.Block) {
		if !pathEqual(path, m.path) {
			return
		}
		before, matched, after := sliceByRuneRange(b.Inlines, m.start, m.end)
		b.Inlines = concatInlines(before, commentMarked(matched, id, author, at, run), after)
	})

	// An invariant check: if List cannot see the highlight this call just
	// created under its ID, the transform is wrong and the caller must not go
	// on to open a thread against text that has no mark.
	if _, ok := commentByID(clone, id); !ok {
		return docmodel.Doc{}, fmt.Errorf("suggest: internal error: could not locate the comment just created")
	}
	return clone, nil
}

// commentMarked is matched with a comment highlight added to every inline: one
// run, so one span, and one ID, so one comment.
//
// ONE AUTHORED EDIT IS ONE SPAN. A target is matched against the document's
// PLAIN TEXT, so a selection crossing a code span (or bold, or a link) marks
// every inline it crosses; a code span is its own inline. Left to MintRuns
// those inlines would get a run EACH, and one selection would arrive as one
// card per inline. Stamped here because this is the transform that knows what
// the reviewer asked for; MintRuns sees only a document, where the same shape
// is also what `{--age--}{--age--}` looks like after a load from disk. It skips
// a mark that already carries a run, so these survive it unchanged.
func commentMarked(matched []docmodel.Inline, id, author string, at time.Time, run string) []docmodel.Inline {
	out := make([]docmodel.Inline, len(matched))
	for i, in := range matched {
		mk := authoredMark(docmodel.Highlight, author, at, run)
		mk.Attrs[docmodel.CommentIDAttr] = id
		out[i] = docmodel.Inline{Text: in.Text, Marks: append(cloneMarks(in.Marks), mk)}
	}
	return out
}

// commentByID is the comment span carrying id, as List reports it.
func commentByID(d docmodel.Doc, id string) (Pending, bool) {
	for _, p := range List(d) {
		if p.Kind == KindComment && p.CommentID == id {
			return p, true
		}
	}
	return Pending{}, false
}

// CommentText is the text the comment named id highlights, every piece joined
// as List joins them, and whether there is one.
func CommentText(d docmodel.Doc, id string) (string, bool) {
	p, ok := commentByID(d, id)
	return p.Text, ok
}

// digestKey is blockKeys' key derivation: parts length-prefixed rather than
// delimiter-joined, so a part that happens to contain the delimiter cannot
// spell a different tuple with the same digest.
func digestKey(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return prefix + hex.EncodeToString(h.Sum(nil))[:16]
}

// listSpans finds every suggestion span in d and assigns ordinal IDs:
// insert, delete and replace spans share one "s" counter, comments get
// their own "c" counter. Both counters are assigned in document order
// within their own family; the returned slice is then re-sorted into
// overall document order, mixing kinds, which is what List presents.
//
// Substitutions are paired into single spans BEFORE the counter runs, so
// "{~~brown~>red~~}" takes one id rather than two consecutive ones. The
// ordinal stays the display and CLI coordinate it always was — `galley
// accept s1` keeps working — and after grouping there is simply one fewer
// id in the list.
func listSpans(d docmodel.Doc) []span {
	suggestions := append(spansForMark(d, docmodel.Ins), spansForMark(d, docmodel.Del)...)
	sortSpans(suggestions)
	suggestions = pairSubstitutions(d, suggestions)
	for i := range suggestions {
		suggestions[i].id = fmt.Sprintf("s%d", i+1)
	}

	// Notes share the comment counter with highlights: to a reviewer they are
	// all "comments", numbered c1, c2… in document order, and which anchor a
	// given one uses is a property of the comment rather than a separate
	// family of thing to accept or reject.
	comments := append(spansForMark(d, docmodel.Highlight), noteSpans(d)...)
	sortSpans(comments)
	comments = coalesceRuns(comments)
	for i := range comments {
		comments[i].id = fmt.Sprintf("c%d", i+1)
	}

	all := make([]span, 0, len(suggestions)+len(comments))
	all = append(all, suggestions...)
	all = append(all, comments...)
	sortSpans(all)
	return all
}

// pairSubstitutions collapses each Del span immediately followed by an Ins
// span that the SERIALIZER writes as one "{~~old~>new~~}" into a single
// replace span, leaving everything else exactly as it found it.
//
// It walks neighbours in the already-sorted list because a substitution is
// adjacency in the file: the pairing rule requires the deleted half's
// segment to run straight into the inserted half's, so the two spans are
// consecutive in document order or they are not a pair at all.
func pairSubstitutions(d docmodel.Doc, spans []span) []span {
	out := make([]span, 0, len(spans))
	for i := 0; i < len(spans); i++ {
		if i+1 < len(spans) {
			if merged, ok := substitutionSpan(d, spans[i], spans[i+1]); ok {
				out = append(out, merged)
				i++
				continue
			}
		}
		out = append(out, spans[i])
	}
	return out
}

// substitutionSpan pairs del and ins into one replace span when — and only
// when — markdown.Substitutions says the serializer writes exactly those
// two ranges as one "{~~old~>new~~}".
//
// THE PREDICATE IS THE SERIALIZER'S, NOT A SECOND ONE THAT AGREES WITH IT.
// markdown.planSubstitution already decides what pairs (a pure deletion
// immediately followed by a pure insertion under the same wrapper, whose
// halves can be spelled inside one span), that decision is why the file
// says "{~~brown~>red~~}" at all, and it is exercised by the round-trip
// golden fixtures and the fuzz target. Asking it rather than restating it
// is what keeps the file, `galley pending`, the CLI and the rail from
// disagreeing about what one suggestion is — including where it DECLINES,
// as it does when the substitution markers occur in the text: there the
// file really does hold two spans and two decisions is the honest answer.
//
// The exact-range match is deliberate. A suggest span groups by mark,
// author, at and run; a serializer segment also splits on the wrapper, so a
// Del span running from plain into bold text is one span here and two
// segments there. Requiring the ranges to coincide means this only ever
// groups what the file will actually write as one, and reports two
// otherwise — the direction that is merely more cards, never a decision
// that half-applies.
//
// The merged span is the DELETED half in every respect but kind, text and
// ins, and its RUN is the deleted half's. Both halves have a run and only
// the listed one can be decided by the browser, so the choice has to be
// made once and stated: the deleted half is the text that was already in
// the document, so it is what the reviewer selected, what the anchor
// machinery measures a position through, and what sits first in document
// order — a card pointing at the start of the replaced phrase points where
// the eye already is. The inserted half's run is not a second coordinate
// for the same decision; there is one decision.
func substitutionSpan(d docmodel.Doc, del, ins span) (span, bool) {
	if del.kind != KindDelete || ins.kind != KindInsert || !pathEqual(del.path, ins.path) {
		return span{}, false
	}
	b, ok := blockAt(d, del.path)
	if !ok || b == nil {
		return span{}, false
	}
	for _, s := range markdown.Substitutions(b.Inlines) {
		if s.DelStart != del.start || s.DelEnd != del.end ||
			s.InsStart != ins.start || s.InsEnd != ins.end {
			continue
		}
		merged := del
		merged.kind = KindReplace
		merged.oldText, merged.newText = del.text, ins.text
		merged.text = del.text + replaceSep + ins.text
		half := ins
		merged.ins = &half
		return merged, true
	}
	return span{}, false
}

// spanKey is WHAT MAKES TWO ADJACENT MARKED INLINES ONE SPAN, and there is
// exactly one definition of it because more than one has already cost this
// package a bug.
//
// The run is part of the key rather than merely riding along: two distinct
// marks by one author within the same second are indistinguishable by
// author+at, so without it "{--age--}{--age--}" collapses into one span and a
// single accept decides both. And it is what carries a span the other way —
// one edit, or one CriticMarkup span in the file, crossing several inlines
// keeps ONE run and so stays one decision (see markdown's applyMark and
// commentMarked).
//
// findUnattributedRun walks the same boundaries for a different purpose and
// used to hand-copy this comparison. It calls this now. If you add a field,
// both get it; that is the point.
type spanKey struct{ author, at, run string }

func spanKeyOf(in docmodel.Inline, mk docmodel.MarkKind) spanKey {
	return spanKey{
		author: in.Attr(mk, "author"),
		at:     in.Attr(mk, "at"),
		run:    in.Attr(mk, docmodel.RunAttr),
	}
}

// spansForMark scans every inline-bearing block of d for maximal runs of
// consecutive inlines carrying mk under one spanKey, in document order.
func spansForMark(d docmodel.Doc, mk docmodel.MarkKind) []span {
	var spans []span
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		// A Note block's inlines are a comment's own words, not document
		// prose. Nothing marks them and nothing should: a mark found there
		// would be a suggestion on a comment, which this pipeline has no way
		// to accept.
		if len(b.Inlines) == 0 || b.Kind == docmodel.Note {
			return
		}
		blockPath := append([]int(nil), path...)
		i := 0
		for i < len(b.Inlines) {
			if !b.Inlines[i].Has(mk) {
				i++
				continue
			}
			key := spanKeyOf(b.Inlines[i], mk)
			j := i + 1
			for j < len(b.Inlines) && b.Inlines[j].Has(mk) && spanKeyOf(b.Inlines[j], mk) == key {
				j++
			}
			author, atStr := key.author, key.at
			at, _ := time.Parse(time.RFC3339, atStr)
			spans = append(spans, span{
				path:     blockPath,
				kind:     kindFor(mk),
				markKind: mk,
				author:   author,
				at:       at,
				run:      key.run,
				// The first inline's: every inline of one span is one mark, and
				// a highlight's ID rides on it.
				commentID: b.Inlines[i].Attr(mk, docmodel.CommentIDAttr),
				text:      plainText(b.Inlines[i:j]),
				start:     i,
				end:       j,
			})
			i = j
		}
	})
	return spans
}

func kindFor(mk docmodel.MarkKind) Kind {
	switch mk {
	case docmodel.Ins:
		return KindInsert
	case docmodel.Del:
		return KindDelete
	default:
		return KindComment
	}
}

func sortSpans(spans []span) {
	sort.SliceStable(spans, func(i, j int) bool {
		if c := pathCompare(spans[i].path, spans[j].path); c != 0 {
			return c < 0
		}
		return spans[i].start < spans[j].start
	})
}

// pathCompare orders paths the way docmodel.Walk visits them: a block's
// path is a prefix of its children's, and pre-order DFS visits the parent
// first, so plain lexicographic comparison of the index sequences (treating
// a prefix as "less") reproduces document order exactly.
func pathCompare(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// blockAt lives in anchor.go and returns a *docmodel.Block. There was a second
// implementation here, returning a value, which the anchors and 1b lines each
// grew independently — same name, same path semantics, different files, so no
// merge conflict and a package that simply did not compile. The pointer form is
// the one that has to survive: AnchorFor dereferences it and removeBlockAt
// mutates parent.Children through it. Two functions answering "which block is
// at this path" is how the two sides start to disagree about what a path means.

func pathEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// contextFor renders the top-level block containing path, plus its
// immediate siblings, back to markdown. It always works at the TOP level
// (path[0]) rather than path's own nesting: a bare list item or blockquote
// child cannot serialize on its own (Serialize only knows how to render the
// eight top-level block kinds), but the top-level block enclosing it —
// paragraph, list, blockquote, whatever it is — always can, and still
// carries the suggestion's immediate neighbourhood inside it.
func contextFor(d docmodel.Doc, path []int) string {
	idx := path[0]
	lo, hi := idx-1, idx+2
	if lo < 0 {
		lo = 0
	}
	if hi > len(d.Blocks) {
		hi = len(d.Blocks)
	}
	return string(markdown.Serialize(docmodel.Doc{Blocks: d.Blocks[lo:hi]}))
}

// findUnique locates the one occurrence of target in d's text, searching
// the concatenated plain text of every inline-bearing block. The offsets it
// returns are RUNE offsets into that block's text (not UTF-16 code units —
// converting for the ydoc bridge, which indexes YXmlText in UTF-16, is that
// bridge's job, not this package's).
func findUnique(d docmodel.Doc, target string) (match, error) {
	if target == "" {
		return match{}, fmt.Errorf("suggest: target text must not be empty")
	}
	targetRunes := []rune(target)

	var matches []match
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		// Notes are excluded: --replace/--after/--on target the DOCUMENT, and
		// a search that also swept the text of existing comments would report
		// "matched 2 times" for a phrase someone had merely quoted back in a
		// note — and could suggest an edit to a comment.
		if len(b.Inlines) == 0 || b.Kind == docmodel.Note {
			return
		}
		runes := []rune(plainText(b.Inlines))
		blockPath := append([]int(nil), path...)
		for i := 0; i+len(targetRunes) <= len(runes); i++ {
			if runeSliceEqual(runes[i:i+len(targetRunes)], targetRunes) {
				matches = append(matches, match{path: blockPath, start: i, end: i + len(targetRunes)})
			}
		}
	})

	switch len(matches) {
	case 0:
		return match{}, fmt.Errorf("suggest: %q matched 0 times, want exactly 1", target)
	case 1:
		return matches[0], nil
	default:
		return match{}, fmt.Errorf("suggest: %q matched %d times, want exactly 1", target, len(matches))
	}
}

func runeSliceEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// match is a single located occurrence of a target string: the block it
// was found in and its RUNE offset range within that block's plain text.
type match struct {
	path       []int
	start, end int
}

// plainText is the concatenation of every inline's text in order. A hard
// break contributes no runes (its Text is empty), consistent with how the
// rest of the codebase counts offsets over a block's text (see
// markdown.InlineComment's Offset).
func plainText(inlines []docmodel.Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		b.WriteString(in.Text)
	}
	return b.String()
}

// sliceByRuneRange splits inlines at the two rune offsets start and end,
// which need not fall on existing inline boundaries. Every inline that
// overlaps [start, end) is split as needed; whichever of its marks it
// already carried — known kinds or not — rides unchanged onto every piece
// it is split into. Only the middle return value's inlines occupy
// [start, end); before and after are untouched apart from being split at
// the boundary.
func sliceByRuneRange(inlines []docmodel.Inline, start, end int) (before, matched, after []docmodel.Inline) {
	pos := 0
	for _, in := range inlines {
		r := []rune(in.Text)
		inStart, inEnd := pos, pos+len(r)
		pos = inEnd

		switch {
		case inEnd <= start:
			before = append(before, in)
		case inStart >= end:
			after = append(after, in)
		default:
			lo, hi := max(0, start-inStart), min(len(r), end-inStart)
			if lo > 0 {
				before = append(before, docmodel.Inline{Text: string(r[:lo]), Marks: cloneMarks(in.Marks)})
			}
			matched = append(matched, docmodel.Inline{Text: string(r[lo:hi]), Marks: cloneMarks(in.Marks)})
			if hi < len(r) {
				after = append(after, docmodel.Inline{Text: string(r[hi:]), Marks: cloneMarks(in.Marks)})
			}
		}
	}
	return before, matched, after
}

// conflictingComment reports the comment highlight already on m's range, if
// any — the check the comment paths use to refuse stacking a second comment
// onto text that already carries one. This is a creation-time invariant only:
// it keeps every highlight unambiguous about which single author's comment it
// is, without requiring List to understand two highlights on one inline. It does not run across kinds — a Highlight coexisting with a Del on
// the same text is the ordinary "comment on a proposed deletion" case and
// stays fully legal.
func conflictingComment(d docmodel.Doc, m match) (author, at string, found bool) {
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		if found || !pathEqual(path, m.path) {
			return
		}
		_, matched, _ := sliceByRuneRange(b.Inlines, m.start, m.end)
		for _, in := range matched {
			if in.Has(docmodel.Highlight) {
				author, at, found = in.Attr(docmodel.Highlight, "author"), in.Attr(docmodel.Highlight, "at"), true
				return
			}
		}
	})
	return author, at, found
}

// authoredMark is a suggestion mark carrying its author, its instant and the
// run naming the authored edit it belongs to — the three fields, and the only
// three, that spansForMark groups by.
func authoredMark(mk docmodel.MarkKind, author string, at time.Time, run string) docmodel.Mark {
	attrs := map[string]string{"author": author, "at": at.Format(time.RFC3339)}
	if run != "" {
		attrs[docmodel.RunAttr] = run
	}
	return docmodel.Mark{Kind: mk, Attrs: attrs}
}

func concatInlines(parts ...[]docmodel.Inline) []docmodel.Inline {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]docmodel.Inline, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// --- deep copy: every public function here returns a docmodel.Doc wholly
// independent of its input, so a caller mutating the result (or the
// original) can never see the other move. ---

func cloneDoc(d docmodel.Doc) docmodel.Doc {
	return docmodel.Doc{Blocks: cloneBlocks(d.Blocks)}
}

func cloneBlocks(blocks []docmodel.Block) []docmodel.Block {
	if blocks == nil {
		return nil
	}
	out := make([]docmodel.Block, len(blocks))
	for i, b := range blocks {
		out[i] = cloneBlock(b)
	}
	return out
}

func cloneBlock(b docmodel.Block) docmodel.Block {
	return docmodel.Block{
		Kind:     b.Kind,
		Attrs:    cloneAttrs(b.Attrs),
		Inlines:  cloneInlines(b.Inlines),
		Text:     b.Text,
		Children: cloneBlocks(b.Children),
	}
}

func cloneInlines(inlines []docmodel.Inline) []docmodel.Inline {
	if inlines == nil {
		return nil
	}
	out := make([]docmodel.Inline, len(inlines))
	for i, in := range inlines {
		out[i] = docmodel.Inline{Text: in.Text, Marks: cloneMarks(in.Marks)}
	}
	return out
}

func cloneMarks(marks []docmodel.Mark) []docmodel.Mark {
	if marks == nil {
		return nil
	}
	out := make([]docmodel.Mark, len(marks))
	for i, m := range marks {
		out[i] = docmodel.Mark{Kind: m.Kind, Attrs: cloneAttrs(m.Attrs)}
	}
	return out
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}
