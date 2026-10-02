// Package review defines the shape of a review's unsent comments as a Yjs
// document.
//
// The document is the live state: the browser edits it over the y-websocket
// protocol, the server edits it in-process, and neither has to poll the other.
// Its durable mirror is the unsent round, internal/unsent's pending.json,
// which the edit server writes after every change and replays at startup.
//
// Schema, rooted at the "threads" map:
//
//	threads: YMap                      key = the comment's ID (unsent.NewID)
//	  <id>: YMap
//	    heading:  string               the quoted words the comment is on
//	    resolved: bool
//	    entries:  YArray               chronological
//	      [n]: YMap
//	        author: "court" | "agent"
//	        at:     RFC3339 string
//	        text:   YText              character-level, so concurrent edits merge
//
// The reviewer's words are a YText, so an edit is the smallest delete and
// insert (SetComment) and a concurrent edit from another peer merges.
package review

import (
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf16"

	"github.com/reearth/ygo/crdt"
)

// Authors. Kept as constants because the browser and the CLI both branch on
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
// update and fans it out to connected peers. A server write through the first
// would be invisible in the reviewer's browser until they reloaded.
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

// Thread is one unsent comment: its key, the words it is on, and what the
// reviewer wrote.
//
// Anchor says what the thread is ABOUT when that is not a range of prose:
// "block" or "document". An empty Anchor means a range of prose.
//
// WHICH block is never stored. It is read off the comment's ID mark every time
// (serve's instructionsOf).
type Thread struct {
	Key      string  `json:"key"`
	Heading  string  `json:"heading"`
	Resolved bool    `json:"resolved"`
	Outcome  string  `json:"outcome,omitempty"`
	Entries  []Entry `json:"entries"`
	Anchor   string  `json:"anchor,omitempty"`
	// BlockKind is the docmodel kind of the block the thread was opened on —
	// "image", "paragraph", "heading", "codeBlock". It is stored in the unsent
	// round so a block comment whose mark is gone can still say what it was
	// about. Where a placed comment sits, and on what kind of block, is read
	// off its ID mark every time (serve's instructionsOf), never from here.
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
// IT IS KEPT WITH THE COMMENT, NOT IN THE .md. The file carries only the
// comment's ID mark, {>>@comment cb-…<<} under the image, and the rectangle
// is stored in pending.json beside the comment's words, author and time, like
// every other fact about the comment.
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

// ErrNoThread is returned when an operation names a key that has no thread.
var ErrNoThread = errors.New("no thread for that section")

// Append adds an entry to a thread, creating the thread if the key names none
// yet.
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
// anchor is "block" or "document", and blockKind is the kind of the block for
// the first of those.
//
// A separate call rather than two more parameters on Append: every existing
// caller of Append is opening a RANGE thread, where the anchor is the
// document's own Highlight mark and there is nothing extra to record, and
// widening that signature would make each of them state a default it does not
// care about. It is a no-op on a thread that does not exist yet — Append
// creates the thread, so this runs after it.
func (s *Session) SetAnchor(key, anchor, blockKind string) {
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
// longer applies at all, and it is irreversible. Nothing calls this except
// the card's delete control, the endpoint behind it, and the send's clear.
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
