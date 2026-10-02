// Package review defines the shape of a review conversation as a Yjs document.
//
// The document is the live state: the browser edits it over the y-websocket
// protocol, the agent edits it in-process, and neither has to poll the other.
// A JSON file beside the page is the durable, git-friendly projection of it —
// written after every settled change and replayed at startup.
//
// Schema, rooted at the "threads" map:
//
//	threads: YMap                      key = slug of the section heading
//	  <slug>: YMap
//	    heading:  string               the section heading, for orphan reporting
//	    resolved: bool
//	    entries:  YArray               chronological
//	      [n]: YMap
//	        author: "court" | "agent"
//	        at:     RFC3339 string
//	        text:   YText              character-level, so concurrent edits merge
//
// One YText per entry rather than one per thread is what makes an agent reply
// arriving mid-sentence harmless: the reviewer's text and the reply are
// different objects, so neither can clobber the other.
package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/ondisk"
)

// Authors. Kept as constants because the overlay and the CLI both branch on
// them and a typo would silently create a third kind of participant.
const (
	AuthorCourt = "court"
	AuthorAgent = "agent"
)

const threadsRoot = "threads"

// Tx runs a mutation inside a document transaction.
//
// It exists because who owns the transaction differs by caller and the
// difference is load-bearing. A plain doc.Transact mutates the document but
// broadcasts to nobody; the websocket server's Apply captures the resulting
// update and fans it out to connected peers. An agent reply written through the
// first would be invisible in the reviewer's browser until they reloaded.
type Tx func(func(*crdt.Transaction))

// Session binds a document to the transaction source that should carry its
// writes.
type Session struct {
	doc *crdt.Doc
	tx  Tx
}

// Wrap binds a document to its own transactions — correct when nothing is
// connected: a scratch document, a startup replay, an offline edit.
func Wrap(doc *crdt.Doc) *Session {
	return &Session{doc: doc, tx: func(fn func(*crdt.Transaction)) { doc.Transact(fn) }}
}

// Bind binds a document to a caller-supplied transaction source — the websocket
// server's, so peers see the write.
func Bind(doc *crdt.Doc, tx Tx) *Session {
	return &Session{doc: doc, tx: tx}
}

// Doc exposes the underlying document for reads.
func (s *Session) Doc() *crdt.Doc { return s.doc }

// Entry is one message in a thread.
type Entry struct {
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

// Thread is the conversation attached to one section of the page.
//
// Anchor and AnchorKey say what the thread is ABOUT when that is not a range
// of prose: "block" (with AnchorKey naming the block, see suggest.BlockKey) or
// "document". Both are omitempty and both are absent on every thread written
// before block anchors existed, where an empty Anchor means the original
// range anchor — so an older sidecar loads unchanged and a consumer that has
// never heard of anchors reads exactly what it read before.
type Thread struct {
	Key       string  `json:"key"`
	Heading   string  `json:"heading"`
	Resolved  bool    `json:"resolved"`
	Outcome   string  `json:"outcome,omitempty"`
	Entries   []Entry `json:"entries"`
	Anchor    string  `json:"anchor,omitempty"`
	AnchorKey string  `json:"anchorKey,omitempty"`
	// BlockKind is the docmodel kind of the block AnchorKey named when the
	// thread was opened — "image", "paragraph", "heading", "codeBlock". It is
	// stored in the unsent round so a block comment whose mark is gone can
	// still say what it was about. Where a placed comment sits, and on what
	// kind of block, is read off its ID mark every time (serve's
	// instructionsOf), never from these fields.
	//
	// Empty on a range thread and on any thread written before this field
	// existed.
	BlockKind string `json:"blockKind,omitempty"`
	// Region is the rectangle on a figure this thread points at, when it
	// points at part of one rather than the whole block. Nil for every other
	// thread, and nil is the ORDINARY case — see Region.
	Region *Region `json:"region,omitempty"`
}

// Region is a rectangle on a figure, in FRACTIONS of the figure's own box —
// never pixels.
//
// A figure reflows with the document column, and a pixel rectangle recorded at
// 620px wide points somewhere else at 680px and off the picture entirely on a
// phone. Fractions survive every reflow because they are defined relative to
// the thing they are on.
//
// IT RIDES IN THE SIDECAR, NOT IN THE .md, and that is a deliberate asymmetry
// rather than an oversight. A rectangle has no readable markdown spelling, and
// inventing one would cost the property the whole design rests on: that a note
// is legible in the file with no tooling. So the note's WORDS go into the file
// as an ordinary block note on the figure — {>>…<<} under the image — and only
// the rectangle lives beside the author and the timestamp, which are in the
// sidecar for exactly the same reason.
//
// What that costs when the sidecar is lost is therefore bounded and known: a
// region note degrades to a block note on the figure. It never loses the words.
type Region struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Valid reports whether r is a non-empty rectangle inside the figure box.
//
// Checked at the endpoint and refused with a 400 rather than clamped: a clamp
// turns "the browser sent nonsense" into a pin somewhere plausible, and a pin
// somewhere plausible is exactly the failure this whole line of work is about.
func (r Region) Valid() error {
	if r.W <= 0 || r.H <= 0 {
		return fmt.Errorf("review: region has no area (%gx%g)", r.W, r.H)
	}
	if r.X < 0 || r.Y < 0 || r.X+r.W > 1 || r.Y+r.H > 1 {
		return fmt.Errorf("review: region (%g,%g %gx%g) is not inside the figure box", r.X, r.Y, r.W, r.H)
	}
	return nil
}

// Comment is the reviewer's own text — the first thing they wrote in the
// thread. It is what "read my comments" means.
func (t Thread) Comment() string {
	for _, e := range t.Entries {
		if e.Author == AuthorCourt {
			return e.Text
		}
	}
	return ""
}

// Open reports whether the thread still wants attention.
func (t Thread) Open() bool {
	return !t.Resolved && t.Comment() != ""
}

// Answered reports whether the agent has spoken last.
//
// Open and Answered ask different questions on purpose: Open asks "did the
// reviewer start something", Answered asks "who spoke last". A thread with no
// entries has nobody to have spoken last, so it reads as unanswered; a thread
// whose newest entry is the reviewer's own follow-up is unanswered too, even
// though it is Open — the ball is back in the reviewer's court.
func (t Thread) Answered() bool {
	if len(t.Entries) == 0 {
		return false
	}
	return t.Entries[len(t.Entries)-1].Author != AuthorCourt
}

// Version is the sidecar's schema generation, written as `v` on every file
// this build projects.
//
// THE SIDECAR IS FORWARD-COMPATIBLE ON THE WAY IN AND FAILS CLOSED ON THE WAY
// OUT, and that asymmetry is the whole of the ruling. It is the one format
// here that is BOTH a file and a wire payload — cmd/galley's loadReview decodes
// this same type off `/_galley/threads` from whatever galley is running — and
// it is written by two processes, the live server's projection and the offline
// CLI's mutations. So:
//
//   - READING IS ALWAYS SAFE AND IS ALWAYS ALLOWED. Unknown keys are ignored
//     and a newer version still loads. A strict decoder would make `galley
//     pending` refuse to talk to a server one commit ahead of it, and would
//     make an older binary unable to so much as LIST the review — for a file it
//     is not about to touch.
//   - REPLAYING IS THE WRITE PATH, AND IT REFUSES. review.Import is what every
//     mutation runs first (serve.mutateSidecar's replay, the live server's
//     startup) and ImportFile's own comment states the hazard: A FIELD THE
//     REPLAY SKIPS IS A FIELD DELETED ON THE NEXT PROJECTION. That is exactly
//     what a newer sidecar is — a file full of fields this build's replay does
//     not know — so believing it and projecting over it does not fail to read
//     the review, it ERASES it. The refusal is internal/ledger/index.go's
//     shape, not internal/license's: a working artefact whose reader can be
//     told what to do about it.
//
// What makes a RENAME loud — which no decoder on this side can see, since the
// reader is looking for the key that vanished — is
// TestTheSidecarKeysAreTheContract. See internal/ondisk.
//
// A sidecar with no `v` is generation 1: every file written before this field
// existed has the shape the field was added to, and must keep loading and
// keep being writable.
const Version = 1

// File is the durable projection written beside the page.
//
// It carries both shapes on purpose: "threads" is the real state, and
// "comments" is the flat reviewer-text-only view that predates threading, kept
// so an older file still loads and so anything reading the file casually gets
// the obvious thing.
type File struct {
	// V is the schema generation — see Version. Zero on a sidecar written
	// before the field existed, and on one decoded off an older server's
	// /_galley/threads, both of which are generation 1.
	V           int              `json:"v"`
	Page        string           `json:"page"`
	Updated     time.Time        `json:"updated"`
	Threads     []Thread         `json:"threads"`
	Comments    []Comment        `json:"comments"`
	Suggestions []SuggestionMeta `json:"suggestions,omitempty"`
	// Changes is the trail: the reviewer's own direct edits, recorded by the
	// browser and kept for the life of the review (the 2026-08-15 trail spec).
	// SIDECAR-HELD WITH NO REPRESENTATION IN THE DOCUMENT, exactly like
	// Suggestions — so ExportOnto MUST carry it, or the first projection after
	// a reply deletes the whole trail silently. The document verdict is what
	// retires it: either approve exit clears the field server-side; Revise
	// leaves it standing, because the review is still on.
	Changes []Change `json:"changes,omitempty"`
}

// Change is one entry in the reviewer's trail: what their own hand removed and
// inserted, where, and when. The author is implicit — only the reviewer's hand
// writes trail entries; the agent's changes arrive as proposals with their own
// lifecycle.
//
// BlockKey, Prefix and Suffix are the re-anchoring coordinates: after a reload
// (or a full fragment rebuild) the browser re-finds the spot best-effort from
// the surrounding text, and an entry it cannot place unambiguously renders in
// the log only — never guessed onto the wrong text.
//
// Before and After are the SAME job for the one entry the affixes cannot do it
// for: a deletion that emptied its own block has no text on either side of
// itself by construction, so its prefix and suffix are both empty and there is
// nothing in its own block left to find it by. These carry the text of the
// blocks either side of it instead — the only identity such an entry has, and
// without them the browser can tell that SOME block in the document is empty
// but not that it is this entry's. They are compared, never searched for, and
// they are nil for every other kind of entry.
//
// THEY ARE POINTERS BECAUSE THE EMPTY STRING IS A REAL ANSWER, AND A DIFFERENT
// ONE FROM ABSENT. "" is "the block that side is empty" and nil is "there is no
// block that side" — the document's first or last block — and the browser's
// comparison refuses whenever the two do not agree. Held as a plain string the
// two collapse into one value the moment the trail is written to the sidecar,
// and the reload puts a ghost on a block its entry never touched (see
// web/trail.js's neighboursAt, where that was measured). `omitempty` on a
// pointer omits nil and KEEPS a pointer to "", which is exactly the
// distinction; anything that changes these fields' type or tags must keep it.
//
// AND BECAUSE THEY ARE POINTERS, A COPY OF A Change SHARES THEM. Every copy of
// the trail Go makes is a SHALLOW one — `copy(out, s.changes)` in
// internal/serve's trailChanges/trailState, and the assignment in ExportOnto —
// so the server's stored trail, every pending view and every sidecar
// projection point at the SAME strings — all three of them, Proposal below
// included. That is safe today for one reason only: NOTHING IN GO EVER WRITES
// THESE FIELDS. They arrive from the browser and go back to it, and Go reads
// them. A writer added on this side must copy the string into a new pointer (or
// deep-copy the slice it is mutating) before touching one, or it edits the
// evidence under a projection already in flight.
//
// PROPOSAL IS THE ONE THING ONLY THE BROWSER CAN SEE, and it is why this
// struct has a third pointer. A hand edit that lands on text the AGENT
// proposed is the reviewer rewriting a proposal — the verdict by hand CLAUDE.md
// names, the fate no accept/reject counter in galley can report — and a hand
// edit anywhere else is the reviewer working on their own prose, which is about
// no proposal at all. The two are indistinguishable by the time the entry
// reaches the server: deleting text under a pending mark REMOVES that mark, so
// a server reading the document at save time (let alone at settle time, two
// saves later) finds nothing where the proposal was. web/trail.js stamps it at
// record time, against the document the step applied TO, where the mark is
// still there to be read; internal/serve/ledger.go is what it is for.
//
// IT CARRIES THE AUTHOR, NOT A BOOLEAN, and the pointer is what makes that
// sayable. Every other proposal record in the ledger names whoever wrote the
// thing being decided, off the mark (serve.ProposalRecord); `galley suggest
// --author NAME` puts a non-agent proposal in the document, so a boolean here
// would file `approved`/that-author for accepting a span and `hand`/agent for
// rewriting the same one — one invariant disagreeing with itself across two
// verbs. nil is "this edit was on no proposal"; a pointer to "" is "a proposal
// whose mark carries no author", which is a real case a file-parsed mark
// reaches, and ProposalRecord's own default is what resolves it. Same
// distinction, and the same reason, as Before/After above.
//
// It is STICKY across coalescing (trail.js's applyRecord): an entry that ever
// touched a proposal rewrote one, and a later keystroke on plain text beside it
// does not un-rewrite it. The FIRST proposal it reached is the one it keeps,
// which is the rule the entry's identity and instant already follow.
//
// PLACED IS ABOUT THE LIST, NOT ABOUT THIS ENTRY, and it is the fourth pointer
// for the same reason as the other three. Changes is an ORDERED slice and the
// order is DOCUMENT ORDER: web/trail.js's settleEntries sorts by position on
// every pass, and the browser saves the list whole. That order is the only
// thing that can tell two block-emptying deletions apart when their evidence is
// identical — two paragraphs struck between three repeated lines carry the same
// Before and the same After, and each on its own is unidentifiable, while the
// pair in order is not. But settleEntries sorts an entry with NO place to the
// END of the list, where its position says nothing, so the order can only be
// read for the entries that HAD one. This says which those are: true when the
// entry had a place at the settle that wrote this record, false when it did
// not.
//
// NIL IS A LEGACY ROW AND MUST DEGRADE TO REFUSAL. A trail written before this
// field existed says nothing about where its entries sat, so the browser reads
// nil as "this list cannot speak for me", leaves such an entry out of the order
// it resolves by, and REFUSES the ambiguity the order would have settled rather
// than guessing at it — spec decision 3, and the same shape as an evidence-free
// record matching only a document's single block. Hence a *bool and not a bool:
// false ("this entry had no place") and absent ("nobody recorded whether it
// did") are different facts, and `omitempty` on a pointer omits nil while
// KEEPING a pointer to false, which is exactly the distinction. Held as a plain
// bool the two collapse and every legacy row would claim an order it never had.
//
// Like Before, After and Proposal it is WRITTEN ONLY BY THE BROWSER: Go reads
// it, copies it shallowly, and hands it back. See the pointer-sharing paragraph
// above before adding a writer on this side.
type Change struct {
	Old      string    `json:"old"`
	New      string    `json:"new"`
	BlockKey string    `json:"blockKey,omitempty"`
	Prefix   string    `json:"prefix,omitempty"`
	Suffix   string    `json:"suffix,omitempty"`
	Before   *string   `json:"before,omitempty"`
	After    *string   `json:"after,omitempty"`
	Proposal *string   `json:"proposal,omitempty"`
	Placed   *bool     `json:"placed,omitempty"`
	At       time.Time `json:"at"`
}

// Expanded is the entry as a HUMAN reads it, and it is a DISPLAY form only:
// never stored, never returned by `pending --json`, never compared against
// anything.
//
// A trail entry is stored as its MINIMAL diff, because the browser's undo
// retraction can only pair entry against undo when both are minimal (see
// web/trail.js's trimAffixes, and the phantom it was written to kill). That
// form is mathematically true and humanly alien: "could" edited to "should"
// is stored as c → sh, since "ould" is a shared suffix, and a listing that
// prints it says nothing about what the reviewer's hand did. So the affixes
// the trim took are put back for the reader — the same walk out to the
// nearest whitespace-or-punctuation boundary web/trail.js's expandToWord
// does, over the SAME anchoring context the entry already carries, so the
// terminal and the editor cannot report one edit two ways.
//
// Both sides get the same two affixes, which is what makes the cases fall out
// rather than needing to be named: an entry already at word boundaries
// expands to itself, and a pure insertion inside a word shows the whole word
// on both sides. An entry with no context — an adrift record whose place
// could not be re-found — is returned exactly as stored, which is all that
// can honestly be said about it.
func (c Change) Expanded() (old, next string) {
	lead := wordTail(c.Prefix)
	tail := wordHead(c.Suffix)
	return lead + c.Old + tail, lead + c.New + tail
}

// endsWord reports whether a rune closes a word. Whitespace and punctuation
// only: a symbol or a mark glued to a word travels with it rather than cutting
// it in half. unicode.IsPunct is General_Category P, which is exactly what the
// browser half's \p{P} matches.
func endsWord(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r)
}

// wordTail is the run of word characters ENDING s, wordHead the run BEGINNING
// it. Both walk by RUNE. (The browser half needs an explicit surrogate guard
// for the same walk — its strings are UTF-16 and a code-unit step splits an
// emoji. Go's are UTF-8 and the decoders are rune-wise, so the hazard is
// answered by the language here rather than by a check.)
func wordTail(s string) string {
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if endsWord(r) {
			return s[i:]
		}
		i -= size
	}
	return s
}

func wordHead(s string) string {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if endsWord(r) {
			return s[:i]
		}
		i += size
	}
	return s
}

// Comment is the legacy flat view of a thread's reviewer text.
type Comment struct {
	Key     string `json:"key"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

// SuggestionMeta is a suggestion's attribution, projected into the sidecar
// because CriticMarkup — the syntax the suggestion itself lives in the file
// as, e.g. {++inserted++} — has nowhere to carry who made it or when (see
// markdown's wrapCritic). Quote is the suggestion's own text, or, for a
// suggestion inside a code block's fence, the whole line it touches — so a
// later load can match a meta back to the mark or the line it belongs to
// without either side needing a persistent ID across edits.
type SuggestionMeta struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Quote  string    `json:"quote"`
}

// SortThreads is THE order a review's threads are reported in: by key.
//
// It is a rule and not a detail, because there are two paths and they were
// different. `Read` has always sorted — "stable between runs" is its own
// docstring — while the offline CLI assembled its list by APPENDING: sidecar
// rows, then the inline notes the parse just lifted, then the block and
// document notes reconciliation found orphaned. Both orders are deterministic
// and they are not the same order, so `galley pending` listed one document's
// threads two ways depending on whether a reviewer happened to have it open —
// measured as `['md-1-17', 'cd-1c…']` offline against `['cd-1c…', 'md-1-17']`
// live. That is a difference an agent diffing across the moment a reviewer
// opens a file sees as the whole list moving, and it also decides WHICH thread
// "the first one" is for anything that reads a listing positionally.
//
// A key is minted once and stored (see unsent.NewID), so ordering by it is a
// fact about the review rather than about the reader — which is what makes it
// the order both paths can hold.
func SortThreads(threads []Thread) {
	sort.Slice(threads, func(i, j int) bool { return threads[i].Key < threads[j].Key })
}

// Read projects the document into plain Go values, ordered by key (see
// SortThreads) so output is stable between runs.
//
// Keys plus Get rather than ForEach, deliberately: ygo's attached ForEach walks
// yield plain values and skip nested shared types, so a map of maps iterates
// zero times through ForEach while Keys reports every one of them. Every value
// in this schema is a nested type, so ForEach would silently read an empty
// document.
func Read(doc *crdt.Doc) []Thread {
	root := doc.GetMap(threadsRoot)
	keys := root.Keys()
	sort.Strings(keys)

	out := make([]Thread, 0, len(keys))
	for _, key := range keys {
		tm, ok := mustGet[*crdt.YMap](root, key)
		if !ok {
			continue
		}
		out = append(out, readThread(key, tm))
	}
	return out
}

func readThread(key string, tm *crdt.YMap) Thread {
	t := Thread{Key: key}
	if v, ok := tm.Get("heading"); ok {
		t.Heading, _ = v.(string)
	}
	if v, ok := tm.Get("resolved"); ok {
		t.Resolved, _ = v.(bool)
	}
	if v, ok := tm.Get("outcome"); ok {
		t.Outcome, _ = v.(string)
	}
	if v, ok := tm.Get("anchor"); ok {
		t.Anchor, _ = v.(string)
	}
	if v, ok := tm.Get("anchorKey"); ok {
		t.AnchorKey, _ = v.(string)
	}
	if v, ok := tm.Get("blockKind"); ok {
		t.BlockKind, _ = v.(string)
	}
	// Four scalars rather than a nested map, on purpose. Every nested shared
	// type in this schema has to be reached with Keys+Get because ygo's
	// attached ForEach skips them, and each one is a prelim handle that has to
	// be threaded through the transaction that created it. A rectangle is four
	// numbers; making it a fifth nested type would add that whole hazard to a
	// value that needs none of it.
	//
	// Presence is W: a valid region has area (see Region.Valid), so a zero
	// width is exactly "no region", which is what every thread that is not on
	// part of a figure has.
	if w, ok := numberAt(tm, "regionW"); ok && w > 0 {
		x, _ := numberAt(tm, "regionX")
		y, _ := numberAt(tm, "regionY")
		h, _ := numberAt(tm, "regionH")
		t.Region = &Region{X: x, Y: y, W: w, H: h}
	}
	// Index rather than ForEach, for the same reason Read uses Keys: every
	// element here is a nested map, which an attached ForEach walk skips.
	if arr, ok := mustGet[*crdt.YArray](tm, "entries"); ok {
		for i := 0; i < arr.Len(); i++ {
			em, ok := arr.Get(i).(*crdt.YMap)
			if !ok {
				continue
			}
			t.Entries = append(t.Entries, readEntry(em))
		}
	}
	return t
}

func readEntry(em *crdt.YMap) Entry {
	var e Entry
	if v, ok := em.Get("author"); ok {
		e.Author, _ = v.(string)
	}
	if v, ok := em.Get("at"); ok {
		if s, ok := v.(string); ok {
			e.At, _ = time.Parse(time.RFC3339, s)
		}
	}
	if v, ok := em.Get("text"); ok {
		if txt, ok := v.(*crdt.YText); ok {
			e.Text = txt.ToString()
		}
	}
	return e
}

// ErrNoThread is returned when an operation names a section that has no thread.
var ErrNoThread = errors.New("no thread for that section")

// Append adds an entry to a thread, creating the thread if the section has
// never been commented on.
//
// Every read happens before the transaction opens, and every nested type is
// reached through the prelim handle that created it. That is not a style
// choice: Transact holds the document's write lock for the duration of fn, and
// both Doc.GetMap and YMap.Get take that same non-reentrant lock, so a read
// inside the transaction deadlocks silently with no stack to show for it.
// Transaction offers root accessors only — there is no in-transaction way to
// read a nested value.
func (s *Session) Append(key, heading, author, text string, at time.Time) {
	root := s.doc.GetMap(threadsRoot)
	thread, _ := mustGet[*crdt.YMap](root, key)
	var entries *crdt.YArray
	if thread != nil {
		entries, _ = mustGet[*crdt.YArray](thread, "entries")
	}

	s.tx(func(txn *crdt.Transaction) {
		if thread == nil {
			thread = crdt.NewMapPrelim()
			root.Set(txn, key, thread)
			thread.Set(txn, "heading", heading)
			thread.Set(txn, "resolved", false)
		} else if heading != "" {
			thread.Set(txn, "heading", heading)
		}
		if entries == nil {
			entries = crdt.NewArrayPrelim()
			thread.Set(txn, "entries", entries)
		}

		entry := crdt.NewMapPrelim()
		entries.PushType(txn, entry)
		entry.Set(txn, "author", author)
		entry.Set(txn, "at", at.UTC().Format(time.RFC3339))

		body := crdt.NewTextPrelim()
		entry.Set(txn, "text", body)
		if text != "" {
			body.Insert(txn, 0, text, nil)
		}
	})
}

// SetAnchor records what a thread is about when that is not a range of prose:
// anchor is "block" or "document", and anchorKey names the block for the
// first of those.
//
// A separate call rather than two more parameters on Append: every existing
// caller of Append is opening a RANGE thread, where the anchor is the
// document's own Highlight mark and there is nothing extra to record, and
// widening that signature would make each of them state a default it does not
// care about. It is a no-op on a thread that does not exist yet — Append
// creates the thread, so this runs after it.
func (s *Session) SetAnchor(key, anchor, anchorKey, blockKind string) {
	if anchor == "" {
		return
	}
	root := s.doc.GetMap(threadsRoot)
	thread, ok := mustGet[*crdt.YMap](root, key)
	if !ok {
		return
	}
	s.tx(func(txn *crdt.Transaction) {
		thread.Set(txn, "anchor", anchor)
		if anchorKey != "" {
			thread.Set(txn, "anchorKey", anchorKey)
		}
		if blockKind != "" {
			thread.Set(txn, "blockKind", blockKind)
		}
	})
}

// SetResolved marks a thread settled, or reopens it.
// numberAt reads a stored float, tolerating the integer form a JSON round trip
// or another CRDT client may have left behind. A region whose x happened to be
// 0 and came back as an int is a region silently dropped.
func numberAt(m *crdt.YMap, key string) (float64, bool) {
	v, ok := m.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// SetRegion records the rectangle on a figure a thread points at, or clears it
// when r is nil.
//
// A separate call rather than more parameters on Append, for the same reason
// SetAnchor is one: every existing caller opens a thread with no rectangle, and
// widening that signature would make each of them state a default it does not
// care about. It is a no-op on a thread that does not exist yet — Append
// creates the thread, so this runs after it.
func (s *Session) SetRegion(key string, r *Region) {
	root := s.doc.GetMap(threadsRoot)
	thread, ok := mustGet[*crdt.YMap](root, key)
	if !ok {
		return
	}
	s.tx(func(txn *crdt.Transaction) {
		if r == nil {
			// Zero width IS absence — see readThread. Written rather than
			// deleted so a clear replicates like every other change.
			thread.Set(txn, "regionW", float64(0))
			return
		}
		thread.Set(txn, "regionX", r.X)
		thread.Set(txn, "regionY", r.Y)
		thread.Set(txn, "regionW", r.W)
		thread.Set(txn, "regionH", r.H)
	})
}

// SetComment makes the reviewer's own entry in a thread read exactly text,
// creating the thread and the entry if this is their first word.
//
// The edit is expressed as the smallest delete+insert that gets there, rather
// than as a wholesale replacement, so a concurrent edit from another peer
// survives and the document does not churn on every keystroke.
//
// Indices are in runes, not bytes: Y.Text addresses characters, and a byte
// offset would corrupt any comment containing a multibyte character. There is a
// test that types one.
func (s *Session) SetComment(key, heading, text string, at time.Time) {
	root := s.doc.GetMap(threadsRoot)
	thread, _ := mustGet[*crdt.YMap](root, key)

	var entries *crdt.YArray
	var body *crdt.YText
	if thread != nil {
		entries, _ = mustGet[*crdt.YArray](thread, "entries")
		if entries != nil {
			for i := 0; i < entries.Len(); i++ {
				em, ok := entries.Get(i).(*crdt.YMap)
				if !ok {
					continue
				}
				if author, _ := mustGet[string](em, "author"); author == AuthorCourt {
					body, _ = mustGet[*crdt.YText](em, "text")
					break
				}
			}
		}
	}

	if body != nil && body.ToString() == text {
		return
	}
	cur := ""
	if body != nil {
		cur = body.ToString()
	}

	s.tx(func(txn *crdt.Transaction) {
		if thread == nil {
			thread = crdt.NewMapPrelim()
			root.Set(txn, key, thread)
			thread.Set(txn, "heading", heading)
			thread.Set(txn, "resolved", false)
		} else if heading != "" {
			thread.Set(txn, "heading", heading)
		}
		if entries == nil {
			entries = crdt.NewArrayPrelim()
			thread.Set(txn, "entries", entries)
		}
		if body == nil {
			entry := crdt.NewMapPrelim()
			entries.PushType(txn, entry)
			entry.Set(txn, "author", AuthorCourt)
			entry.Set(txn, "at", at.UTC().Format(time.RFC3339))
			body = crdt.NewTextPrelim()
			entry.Set(txn, "text", body)
			if text != "" {
				body.Insert(txn, 0, text, nil)
			}
			return
		}
		start, endCur, endNew := diffBounds(cur, text)
		if endCur > start {
			body.Delete(txn, start, endCur-start)
		}
		if endNew > start {
			body.Insert(txn, start, string(utf16.Decode(utf16.Encode([]rune(text))[start:endNew])), nil)
		}
	})
}

// diffBounds returns the offsets bounding the one region where cur and next
// differ: everything before start is a shared prefix, everything after
// endCur/endNext is a shared suffix.
//
// Offsets are UTF-16 code units, because that is what ygo's YText addresses —
// established by probe, not assumed. Rune offsets agree for everything in the
// Basic Multilingual Plane and silently corrupt anything outside it: an emoji
// is one rune and two code units, so a rune-indexed delete cuts a surrogate
// pair in half and leaves replacement characters behind.
func diffBounds(cur, next string) (start, endCur, endNext int) {
	a := utf16.Encode([]rune(cur))
	b := utf16.Encode([]rune(next))

	maxStart := min(len(a), len(b))
	for start < maxStart && a[start] == b[start] {
		start++
	}
	// Never begin the edit between a surrogate pair's halves.
	if start > 0 && start < len(a) && isLowSurrogate(a[start]) {
		start--
	}

	endCur, endNext = len(a), len(b)
	for endCur > start && endNext > start && a[endCur-1] == b[endNext-1] {
		endCur--
		endNext--
	}
	// Nor end it there. Both sides advance together: the suffix scan only
	// stopped here because the units matched, so they are the same half.
	if endCur < len(a) && endNext < len(b) && isLowSurrogate(a[endCur]) {
		endCur++
		endNext++
	}
	return start, endCur, endNext
}

func isLowSurrogate(u uint16) bool { return u >= 0xDC00 && u <= 0xDFFF }

func (s *Session) SetResolved(key string, resolved bool) error {
	root := s.doc.GetMap(threadsRoot)
	thread, ok := mustGet[*crdt.YMap](root, key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoThread, key)
	}
	s.tx(func(txn *crdt.Transaction) {
		thread.Set(txn, "resolved", resolved)
	})
	return nil
}

// SetOutcome records which no was said — the reviewer's disposition on a
// thread, alongside whether it is resolved. A transcription of SetResolved:
// the handle is resolved before the transaction opens, and a thread the key
// does not name is an error rather than a silent no-op, for the same reason
// Delete's absence is an error — a caller with a typo has no way left to
// notice otherwise.
func (s *Session) SetOutcome(key, outcome string) error {
	root := s.doc.GetMap(threadsRoot)
	thread, ok := mustGet[*crdt.YMap](root, key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoThread, key)
	}
	s.tx(func(txn *crdt.Transaction) {
		thread.Set(txn, "outcome", outcome)
	})
	return nil
}

// Delete removes a thread outright — the conversation and every entry in it.
//
// IT IS THE ONE DESTRUCTIVE OPERATION IN THIS FILE, and it is separate from
// SetResolved because the two mean different things. Resolving settles a
// conversation and keeps what everybody said; deleting is for a comment that no
// longer applies at all, and it is irreversible outside git. Nothing calls this
// except an explicit `galley delete`, the card's delete control, and the
// endpoint behind them.
//
// The thread's absence is an ERROR rather than a no-op: a delete that silently
// succeeds against a key naming nothing is a delete agreeing with a typo, and
// the caller has no way left to notice.
//
// Read before the transaction opens, like every other mutation here — Get and
// GetMap take the same non-reentrant lock Transact holds.
func (s *Session) Delete(key string) error {
	root := s.doc.GetMap(threadsRoot)
	if _, ok := mustGet[*crdt.YMap](root, key); !ok {
		return fmt.Errorf("%w: %s", ErrNoThread, key)
	}
	s.tx(func(txn *crdt.Transaction) {
		root.Delete(txn, key)
	})
	return nil
}

func mustGet[T any](m *crdt.YMap, key string) (T, bool) {
	var zero T
	v, ok := m.Get(key)
	if !ok {
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

// ExportOnto is Export with every field of prior that the DOCUMENT does not
// hold carried across unchanged.
//
// File is a shared schema with more than one owner. The ygo document holds
// threads (and the flat Comments view derived from them) and nothing else —
// Suggestions is written by the markdown side, which has no representation in
// this document at all. So a writer that replays only the threads and then
// writes Export's fresh projection over the whole file DELETES the
// suggestions: that is exactly how `galley reply` and `galley resolve` erased
// every suggestion's author and timestamp, the only record of them there is.
//
// Anything added to File from here on must be carried here too unless the
// document genuinely holds it. The rule is the field list, not this one field.
func ExportOnto(doc *crdt.Doc, page string, prior File) File {
	f := Export(doc, page)
	f.Suggestions = prior.Suggestions
	f.Changes = prior.Changes
	return f
}

// Export projects the document into the durable file shape.
//
// It reports ONLY what the document holds. A caller writing the result over an
// existing sidecar wants ExportOnto — see there for why.
func Export(doc *crdt.Doc, page string) File {
	threads := Read(doc)
	return File{
		V:        Version,
		Page:     page,
		Updated:  time.Now().UTC(),
		Threads:  threads,
		Comments: CommentsOf(threads),
	}
}

// CommentsOf derives File.Comments from File.Threads — the flat,
// reviewer-text-only view that predates threading and is kept so an older
// reader still gets the obvious thing.
//
// It is EXPORTED and it is the only derivation, because the two writers of a
// sidecar are two processes: Export here (the live server's projection) and
// cmd/galley's offline mutations. A second copy is how the two came to
// disagree — the offline one returned a nil slice for a file with no
// commentable thread, so a sidecar the live server writes as "comments": []
// came back from an offline `galley delete` as "comments": null, one
// projection later, with nothing having changed about the review.
//
// Never nil: the field has no omitempty and the two spellings of empty are a
// difference a reader has to handle for no reason.
func CommentsOf(threads []Thread) []Comment {
	out := make([]Comment, 0, len(threads))
	for _, t := range threads {
		if c := t.Comment(); c != "" {
			out = append(out, Comment{Key: t.Key, Heading: t.Heading, Text: c})
		}
	}
	return out
}

// Compatible reports whether this build may REPLAY f — which is to say,
// whether it may go on to write over the file f came from.
//
// It is separate from decoding on purpose. A caller that only wants to READ a
// review (galley pending, loadReview over the wire, serve.Load) never asks
// this and never should: a newer sidecar's threads, comments and suggestions
// decode perfectly well into the fields this build knows, and refusing to show
// them would be a strictness that costs a reader everything and protects
// nothing. A caller that is about to REPLAY asks, because the replay is the
// first half of a projection that will drop every field it did not replay.
func (f File) Compatible() error {
	if ondisk.Future(f.V, Version) {
		return ondisk.Newer("the review sidecar", f.V, Version,
			"upgrade galley, or the next projection would drop every field this build cannot replay")
	}
	return nil
}

// Import replays a durable file into a document.
//
// It accepts a file written before threading existed, so nothing typed against
// the old format is lost when the format changes underneath it.
func Import(doc *crdt.Doc, raw []byte) error {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return err
	}
	return ImportFile(doc, f)
}

// ImportFile is Import over an already-decoded file — for a caller that had to
// look at (or amend) the projection before replaying it, and must replay
// exactly what it ended up with rather than the bytes it started from.
func ImportFile(doc *crdt.Doc, f File) error {
	// THE GATE IS HERE AND NOT IN Load, because this is the write path. Every
	// mutation replays the sidecar through this function and then projects the
	// result back over the file, and the paragraph below says what a field this
	// replay does not know is worth: nothing, permanently. Refusing a sidecar
	// from a newer galley is the only way to not be the thing that deletes it.
	// See Version for why reading it is nonetheless fine.
	if err := f.Compatible(); err != nil {
		return err
	}
	s := Wrap(doc)
	if len(f.Threads) > 0 {
		for _, t := range f.Threads {
			for _, e := range t.Entries {
				at := e.At
				if at.IsZero() {
					at = f.Updated
				}
				s.Append(t.Key, t.Heading, e.Author, e.Text, at)
			}
			s.SetAnchor(t.Key, t.Anchor, t.AnchorKey, t.BlockKind)
			// Every field of Thread has to be replayed here, not just the ones
			// the replay happened to be written for: this is the load path for
			// the sidecar, and a field the replay skips is a field deleted on
			// the next projection. That is exactly how Suggestions was lost.
			s.SetRegion(t.Key, t.Region)
			if t.Resolved {
				_ = s.SetResolved(t.Key, true)
			}
			if t.Outcome != "" {
				_ = s.SetOutcome(t.Key, t.Outcome)
			}
		}
		return nil
	}
	for _, c := range f.Comments {
		s.Append(c.Key, c.Heading, AuthorCourt, c.Text, f.Updated)
	}
	return nil
}
