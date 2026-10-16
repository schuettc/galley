// Package ydoc bridges galley's document model into ygo's XML fragment — the
// shared type TipTap edits through y-prosemirror.
//
// The fragment is the live document. Every name in it is TipTap's: element tags
// are ProseMirror node names ("paragraph", "bulletList", "horizontalRule"),
// element attributes are node attributes, and a mark is a formatting attribute
// on a YXmlText run, keyed by the mark's name. Nothing here is a private
// encoding galley invented; a y-prosemirror client attached to the same
// fragment sees the document it expects.
//
// Three things about ygo's XML surface are load-bearing, all established by
// probe rather than assumed:
//
//   - YXmlText.Insert INHERITS the formatting in effect at the cursor when it
//     is passed nil or empty attributes, and otherwise applies the DIFFERENCE
//     between what it is passed and what is already in effect. Writing a plain
//     run after a bold one with nil attributes silently makes it bold. Every
//     run is therefore written with the complete attribute set — every mark
//     that is off named explicitly with a nil value, which clears it. That set
//     is not a fixed list: a kind nobody named stays switched on, so it spans
//     the marks this package knows about PLUS every kind present in the block,
//     including ones from TipTap extensions it has never heard of. See
//     runKinds.
//
//   - YXmlText positions are UTF-16 code units, exactly as YText's are: "😀" is
//     one rune and two positions. This package never computes an offset. It
//     appends at Len(), which is already in the right units, so there is no
//     opportunity to cut a surrogate pair in half.
//
//   - Reads take the document's read lock (YXmlText.ToString does), and
//     ygo's transaction holds the write lock, which is not reentrant. So Load
//     resolves and measures the fragment BEFORE opening its transaction, and
//     reaches every nested node through the handle that created it. Read runs
//     entirely outside a transaction.
//
// Unlike YMap, YXmlFragment.Children DOES surface nested shared types, so the
// ForEach-skips-nested hazard that shapes internal/review does not apply here.
// It was probed; it does not reproduce on the XML types.
//
// That matters more since phase 1e than it did before. A table is three
// levels of nested elements — table > tableRow > tableCell > paragraph — and
// it is precisely the shape the YMap hazard eats: an attached ForEach walk
// would report zero rows and return a Block with the right kind and no
// content, which projects to disk as a table that lost every cell. Nothing in
// this file uses ForEach, and nothing added to it should. Children() and
// indexed access are what is probed; a walk written any other way is not.
package ydoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/review"
)

// FragmentName is the root XML fragment the editor binds to. y-prosemirror's
// default is "prosemirror"; galley uses "content" and configures the client to
// match, so the name is spelled once, here.
const FragmentName = "content"

// markOrder is the canonical order marks are read back in.
//
// A formatting run's attributes are a map, so the order marks were written in
// is not recoverable — and it is not meaningful either: docmodel's Has and
// Attr are order-independent, and ProseMirror likewise treats a mark set as a
// set ranked by schema order. This is that rank. It is docmodel's declaration
// order, which is what the markdown parser already produces.
var markOrder = []docmodel.MarkKind{
	docmodel.Bold,
	docmodel.Italic,
	docmodel.Code,
	docmodel.Link,
	docmodel.Ins,
	docmodel.Del,
	docmodel.Highlight,
}

// container is what a block can be written into: the root fragment or an
// element. Both carry these methods; the interface exists only so the writer
// does not need two code paths.
type container interface {
	InsertElement(txn *crdt.Transaction, index int, elem *crdt.YXmlElement)
	InsertText(txn *crdt.Transaction, index int, txt *crdt.YXmlText)
}

// Load replaces the fragment's contents with model, writing through tx.
//
// tx rather than doc.Transact is the point: the websocket server's Apply
// captures the resulting update and fans it out to connected peers, and a
// document written straight through doc.Transact is correct on disk and
// invisible in the reviewer's browser.
func Load(doc *crdt.Doc, tx review.Tx, model docmodel.Doc) {
	// Both of these read the document, so both happen before the transaction
	// opens and takes the write lock.
	frag := doc.GetXmlFragment(FragmentName)
	existing := frag.Len()

	tx(func(txn *crdt.Transaction) {
		if existing > 0 {
			frag.Delete(txn, 0, existing)
		}
		writeBlocks(txn, frag, model.Blocks)
	})
}

// writeBlocks appends blocks to an already-attached parent.
//
// Top-down is deliberate, and it is NOT merely a style preference — an earlier
// comment here read as "either order works", which is only half true.
//
// An element inserted into an attached parent is itself attached, so its own
// children attach as they are inserted and there is no buffered subtree to
// reason about. Bottom-up does produce the same document: ygo buffers writes to
// a detached node and flushes them on attach, probed against v1.43.0. But a
// detached YXmlText is INVISIBLE TO READS while it holds them — after
// txt.Insert(txn, 0, "hello", nil), a detached txt reports Len() == 0 and
// ToString() == "", and only reports 5 and "hello" once it has been attached.
// Any code that writes bottom-up and then measures what it wrote gets zero and
// no error. Building top-down means every handle is live the moment it is used.
func writeBlocks(txn *crdt.Transaction, parent container, blocks []docmodel.Block) {
	for i, b := range blocks {
		writeBlock(txn, parent, i, b)
	}
}

// rawText reports whether a block's content lives in Block.Text as one
// unmarked run rather than in Inlines or Children.
//
// ONE PREDICATE, ASKED THREE TIMES. The writer and both readers (readBlock and
// elementSnapshot.toBlock, which mirror each other line for line) each need the
// answer, and a kind added to two of the three would be written into the
// fragment as raw text and read back as an EMPTY block — a document that
// projects to disk with the block's content gone. The same shape as the six
// agreeing `!= KindComment` copies CLAUDE.md records, one layer down.
//
// Every member is literal text galley never rewrites: a fence is the author's
// code, front matter is the author's metadata, and a math block is the author's
// TeX — all three carried verbatim, in a language that is not markdown.
func rawText(kind docmodel.BlockKind) bool {
	return kind == docmodel.CodeBlock ||
		kind == docmodel.FrontMatter ||
		kind == docmodel.MathBlock
}

func writeBlock(txn *crdt.Transaction, parent container, index int, b docmodel.Block) {
	el := crdt.NewYXmlElement(string(b.Kind))
	parent.InsertElement(txn, index, el)

	for _, k := range sortedKeys(b.Attrs) {
		el.SetAttribute(txn, k, b.Attrs[k])
	}

	switch {
	case rawText(b.Kind):
		// Raw text: no marks, no inline structure.
		txt := crdt.NewYXmlText()
		el.InsertText(txn, 0, txt)
		txt.Insert(txn, 0, b.Text, nil)
	case len(b.Inlines) > 0:
		writeInlines(txn, el, b.Inlines)
	default:
		writeBlocks(txn, el, b.Children)
	}
}

// writeInlines writes a block's inline content as YXmlText runs, breaking out
// to a <hardBreak> element for the zero-text sentinel.
//
// A hard break is a node in ProseMirror's schema, not a mark, so it cannot ride
// on a text run — and a zero-length run could not carry formatting anyway
// (YXmlText.Insert ignores empty text). It ends the current run and starts a
// new one after it.
func writeInlines(txn *crdt.Transaction, parent *crdt.YXmlElement, inlines []docmodel.Inline) {
	index := 0
	var run *crdt.YXmlText
	// Every mark kind that has to be named on every run in this block. See
	// runKinds: naming only the kinds this package knows about is not enough.
	kinds := runKinds(inlines)

	for _, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			br := crdt.NewYXmlElement(string(docmodel.HardBreak))
			parent.InsertElement(txn, index, br)
			index++
			// The marks of the run the break interrupts, kept so the model
			// round-trips. TipTap has no attributes on hardBreak and ignores
			// these; nothing else writes them.
			for _, m := range in.Marks {
				if m.Kind == docmodel.HardBreak {
					continue
				}
				br.SetAttributeValue(txn, string(m.Kind), encodeMarkAttrs(m.Attrs))
			}
			run = nil
			continue
		}
		// An empty inline that is not a hard break has nothing to carry its
		// marks: YXmlText.Insert ignores empty text, so there would be no run
		// to format. The markdown layer drops these too.
		if in.Text == "" {
			continue
		}
		if run == nil {
			run = crdt.NewYXmlText()
			parent.InsertText(txn, index, run)
			index++
		}
		// Len is a plain field read, not a locking one, so it is safe here;
		// and it is already in UTF-16 code units, which is what Insert wants.
		run.Insert(txn, run.Len(), in.Text, formatAttrs(kinds, in.Marks))
	}
}

// runKinds is the set of mark kinds every run in a block must name: the ones
// this package knows about, plus every kind actually present in the block.
//
// The second half is the load-bearing one. Insert applies the DIFFERENCE
// between the attributes it is passed and the ones already in effect, so a
// kind the attribute set does not mention is simply left switched on. Naming
// only markOrder would let a mark from a TipTap extension this package has
// never heard of — StarterKit ships strike — smear across every run to its
// right, which would quietly contradict the promise readMarkAttrs makes to
// preserve unrecognised marks. Read them faithfully, then corrupt them on the
// way back out.
//
// Block scope rather than run scope: a hard break starts a fresh YXmlText
// whose formatting state is empty, so per-run would also be correct, but the
// block's set is a superset and costs one pass over inlines that are already
// in hand.
func runKinds(inlines []docmodel.Inline) map[string]bool {
	kinds := make(map[string]bool, len(markOrder))
	for _, k := range markOrder {
		kinds[string(k)] = true
	}
	for _, in := range inlines {
		for _, m := range in.Marks {
			if m.Kind == docmodel.HardBreak {
				continue
			}
			kinds[string(m.Kind)] = true
		}
	}
	return kinds
}

// formatAttrs is the COMPLETE attribute set for a run: every kind in kinds,
// the ones present on this inline carrying their attributes and the rest
// carrying nil. The nils are not noise — they are what clears the formatting
// inherited from the run to the left. See the package comment.
func formatAttrs(kinds map[string]bool, marks []docmodel.Mark) crdt.Attributes {
	attrs := make(crdt.Attributes, len(kinds))
	for k := range kinds {
		attrs[k] = nil
	}
	for _, m := range marks {
		if m.Kind == docmodel.HardBreak {
			continue
		}
		attrs[string(m.Kind)] = encodeMarkAttrs(m.Attrs)
	}
	return attrs
}

// encodeMarkAttrs renders a mark's attributes as the value of its formatting
// attribute.
//
// A map, not a JSON string. y-prosemirror stores a mark as
// {markName: mark.attrs} where mark.attrs is a plain object — and ygo encodes
// map[string]any with lib0's object tag, so this is byte-identical to what a
// JS client writes. A JSON string in the same slot would reach TipTap as a
// string and every attribute on it (a link's href, a suggestion's author) would
// read as undefined. Marks with no attributes get an empty object, which is
// what ProseMirror's mark.attrs is for an attribute-less mark.
func encodeMarkAttrs(attrs map[string]string) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

// Read projects the fragment back into the document model.
//
// It runs outside any transaction: YXmlText.ToString takes the document's read
// lock, and ygo's transaction holds the non-reentrant write lock.
func Read(doc *crdt.Doc) (docmodel.Doc, error) {
	// Children returns ygo's unexported xmlNode interface, so the loop lives
	// here rather than in a helper taking the slice as a parameter.
	var blocks []docmodel.Block
	for _, n := range doc.GetXmlFragment(FragmentName).Children() {
		el, ok := n.(*crdt.YXmlElement)
		if !ok {
			return docmodel.Doc{}, fmt.Errorf("ydoc: fragment %q holds a %T where a block element was expected", FragmentName, n)
		}
		b, err := readBlock(el)
		if err != nil {
			return docmodel.Doc{}, err
		}
		blocks = append(blocks, b)
	}
	return docmodel.Doc{Blocks: blocks}, nil
}

// LiveReadOrigin marks the Transact call ReadLive makes internally. A real
// edit's Transact — always routed through the websocket server's Apply, per
// CLAUDE.md — carries Apply's own origin sentinel, never this one, so a
// doc.OnUpdate subscriber can tell "someone actually wrote" from "ReadLive
// just needed the lock" by comparing the origin it receives against this
// value. Without that check, a consumer that treats every OnUpdate firing as
// "something changed, reschedule a projection" — as EditServer's debounce
// does — would reschedule itself forever: ygo fires OnUpdate for every
// Transact, including a no-op one (established by probe), so ReadLive's own
// mutual-exclusion Transact would otherwise count as an edit and requeue
// another Project, which calls ReadLive again, forever.
//
// Exported so a consumer's OnUpdate callback can compare against it (see
// EditServer's use), not as an invitation to reuse it as an origin for a
// real, mutating Transact: doing that would make an actual edit
// indistinguishable from ReadLive's own no-op read here, and any OnUpdate
// subscriber filtering on this value — as EditServer's does — would silently
// swallow it.
var LiveReadOrigin = new(struct{})

// readLiveMaxAttempts bounds ReadLive's retry loop (see below). 8 is
// generous for the interference this guards against — a handful of
// concurrent edits landing in the same instant — without letting a
// persistently hostile write pattern spin close to indefinitely.
const readLiveMaxAttempts = 8

// readLiveTestHook, when non-nil, runs once per ReadLive attempt, after
// phase 1 (the structural snapshot) but before the "did anything change"
// check — a way for this package's own tests to inject a write into that
// exact window deterministically, rather than needing to out-race real
// goroutine scheduling to exercise the retry-exhaustion path. Nil in
// production; never set outside a test.
var readLiveTestHook func(doc *crdt.Doc)

// ErrConcurrentWrite is ReadLive's error when readLiveMaxAttempts consecutive
// snapshots were each invalidated by an interleaving write before it could
// finish reading one consistently. The caller must not use whatever partial
// result it might otherwise have assembled — there isn't one; ReadLive
// returns a zero Doc alongside this error. EditServer.Project treats it as
// "skip this projection" rather than an error to surface: the write that
// invalidated the last attempt is itself a real edit, which will fire its
// own doc.OnUpdate and requeue a projection through the ordinary debounce —
// so nothing is lost, just deferred to the next settle. A caller with no
// "next settle" coming (a final flush on shutdown) should decide for itself
// whether retrying ReadLive again is worth it; Project does not retry on
// its own.
var ErrConcurrentWrite = errors.New("ydoc: could not read a consistent snapshot of a live document (concurrent writes kept interleaving)")

// ReadLive is Read, safe to call on a document a websocket server may be
// concurrently writing to (a served room) — Read is not, and must only be
// used where nothing else can be mutating the same *crdt.Doc at that moment
// (tests, one-shot CLI reads of a document nothing is serving).
//
// The hazard: YXmlFragment.Children and YXmlElement.GetAttributes/
// GetAttributeValues take NO lock at all — probed and confirmed against
// v1.43.0 — unlike almost everything else in ygo's XML surface, which either
// takes the document's read lock itself (YXmlText.ToString/ToDelta) or is
// only ever called already holding the write lock (inside a Transact). A
// structural walk of the fragment (Children, recursively) races an Apply
// -driven write's own structural mutations under `go test -race`: both read
// and write the same linked list, and only the writer takes any lock at all.
//
// The fix looks like the CLAUDE.md rule it sits next to — "resolve outside,
// touch the document only inside the transaction" — but is NOT simply
// "wrap Read's body in a Transact": YXmlText.ToString/ToDelta each take the
// document's READ lock, and a Transact holds the WRITE lock for the same
// non-reentrant mutex, on the same goroutine — calling either from inside
// a Transact deadlocks (established by probe; the "never read inside
// Transact" rule again, just for a different pair of ygo calls than the one
// CLAUDE.md already names). So this runs in two phases: PHASE 1, inside one
// Transact, walks the structure only (Children/NodeName/GetAttributes*,
// all lock-free) and snapshots it — capturing each YXmlText node's POINTER
// rather than its content. PHASE 2, after the Transact has released the
// write lock, reads that content (ToString/ToDelta, each taking their own
// brief read lock, safe again now).
//
// That split reopens a narrower window of its own: a peer write landing
// between phase 1 capturing a YXmlText's pointer and phase 2 reading through
// it can delete the very node phase 1 pointed at. ToString/ToDelta do not
// error on a deleted node — by design, a Y.Text tombstone reads as empty —
// so phase 2 would silently materialise a Block with the right shape but
// missing text: a heading snapshot mid-delete reads back as "" instead of
// its actual content, and Project would write "#" to disk where "# Title"
// belongs. Not a data race (every access here is properly lock-protected;
// -race has nothing to flag) and not a crash — a quiet, textually-valid
// wrong answer, which is worse. Found by fuzzing the exact interleaving:
// racing Apply-driven reloads against Project while polling the file on
// disk surfaced hundreds of empty-heading writes in tens of thousands of
// iterations.
//
// The guard is a validated snapshot: crdt.CaptureSnapshot(doc) before phase 1
// opens and again after phase 2 finishes reading every captured pointer,
// compared with crdt.EqualSnapshots. A Yjs Snapshot is a StateVector PLUS a
// DeleteSet — that second half is load-bearing and not optional here: a pure
// delete (the exact shape a backspace produces, and the shape this whole bug
// is about) marks an item's Deleted flag and records the range in the
// transaction's delete set, but never advances any client's clock — probed
// directly against v1.43.0 (crdt/item.go's delete, crdt/store.go's
// buildDeleteSet). So a StateVector-only comparison, an earlier version of
// this guard, is STRUCTURALLY BLIND to a delete-only interleaving: it
// reported "clean" while phase 2 had in fact just read a deleted node back
// as empty. CaptureSnapshot's DeleteSet catches exactly that, confirmed by
// the same kind of probe: capture, run a pure YXmlText.Delete with nothing
// inserted, capture again — StateVector compares equal, EqualSnapshots does
// not.
//
// Like StateVector, CaptureSnapshot takes the document's own write lock to
// compute — no separate deferred callback (an OnUpdate subscription, say)
// that could still be pending when the check runs, just a direct,
// immediately-consistent read of the same store every writer mutates under
// that same lock. If the two snapshots disagree, some write (insert OR
// delete) landed somewhere in the interval and the whole two-phase read is
// discarded and retried — the comparison window is deliberately wider than
// the strictly vulnerable one (it also starts ticking before phase 1 opens,
// not just between the phases), which costs a few avoidable retries when a
// write lands harmlessly before phase 1 even starts, in exchange for having
// no window of its own to reason about. A clean comparison means no write —
// of either shape — could have interleaved with the reads that produced this
// Doc, by construction, not by getting lucky on timing.
func ReadLive(doc *crdt.Doc) (docmodel.Doc, error) {
	// Must resolve before the Transact opens, same reason as everywhere else
	// in this file: GetXmlFragment briefly takes the document lock itself.
	frag := doc.GetXmlFragment(FragmentName)

	for attempt := 0; attempt < readLiveMaxAttempts; attempt++ {
		before := crdt.CaptureSnapshot(doc)

		var top []any
		var snapErr error
		doc.Transact(func(_ *crdt.Transaction) {
			for _, n := range frag.Children() {
				sn, err := snapshotXMLNode(n)
				if err != nil {
					snapErr = err
					return
				}
				top = append(top, sn)
			}
		}, LiveReadOrigin)
		if snapErr != nil {
			return docmodel.Doc{}, snapErr
		}

		if readLiveTestHook != nil {
			readLiveTestHook(doc)
		}

		var blocks []docmodel.Block
		var matErr error
		for _, sn := range top {
			es, ok := sn.(elementSnapshot)
			if !ok {
				matErr = fmt.Errorf("ydoc: fragment %q holds a %T where a block element was expected", FragmentName, sn)
				break
			}
			b, err := es.toBlock()
			if err != nil {
				matErr = err
				break
			}
			blocks = append(blocks, b)
		}
		if matErr != nil {
			// A structural error reflects a genuinely malformed document
			// (readBlock's own default case is the same, non-retried, kind
			// of error) — phase 1 already ran under the write lock in full,
			// so this cannot be a torn read the retry would fix.
			return docmodel.Doc{}, matErr
		}

		after := crdt.CaptureSnapshot(doc)
		if crdt.EqualSnapshots(before, after) {
			return docmodel.Doc{Blocks: blocks}, nil
		}
		// Something wrote — inserted or deleted — in between; the pointers
		// phase 1 captured may no longer mean what phase 2 just read from
		// them. Discard and retry.
	}
	return docmodel.Doc{}, ErrConcurrentWrite
}

// elementSnapshot is a lock-free copy of one YXmlElement's shape, taken
// inside ReadLive's Transact. attrsStr mirrors what GetAttributes returns
// (used when this snapshot is materialised as a Block, matching readBlock);
// attrsRaw mirrors GetAttributeValues (used when it turns out to be a
// hardBreak leaf instead, matching readMarkAttrs's expected input). Both are
// captured up front, inside the transaction, because the live element
// itself must not be touched again once the transaction has closed — ReadLive
// makes no assumption about what happens to the document after that point.
type elementSnapshot struct {
	nodeName string
	attrsStr map[string]string
	attrsRaw map[string]any
	children []any
}

// textSnapshot defers the one read a Transact cannot safely make: the actual
// text. It carries nothing but the pointer, resolved in phase 2.
type textSnapshot struct{ txt *crdt.YXmlText }

// snapshotXMLNode is phase 1: a lock-free structural copy, recursive, called
// only from inside ReadLive's Transact. It must never call ToString or
// ToDelta — see ReadLive's comment.
func snapshotXMLNode(n any) (any, error) {
	switch v := n.(type) {
	case *crdt.YXmlText:
		return textSnapshot{txt: v}, nil
	case *crdt.YXmlElement:
		es := elementSnapshot{
			nodeName: v.NodeName,
			attrsStr: v.GetAttributes(),
			attrsRaw: v.GetAttributeValues(),
		}
		for _, child := range v.Children() {
			cs, err := snapshotXMLNode(child)
			if err != nil {
				return nil, err
			}
			es.children = append(es.children, cs)
		}
		return es, nil
	default:
		return nil, fmt.Errorf("ydoc: unknown xml node type %T", n)
	}
}

// toBlock is phase 2: turns a structural snapshot into a docmodel.Block,
// reading each textSnapshot's actual content now that no Transact is open —
// a line-for-line mirror of readBlock, operating on the snapshot tree
// instead of live crdt handles.
func (es elementSnapshot) toBlock() (docmodel.Block, error) {
	b := docmodel.Block{Kind: docmodel.BlockKind(es.nodeName)}
	if len(es.attrsStr) > 0 {
		b.Attrs = es.attrsStr
	}

	for _, child := range es.children {
		switch c := child.(type) {
		case textSnapshot:
			if rawText(b.Kind) {
				b.Text += c.txt.ToString()
				continue
			}
			b.Inlines = append(b.Inlines, readInlines(c.txt)...)
		case elementSnapshot:
			if c.nodeName == string(docmodel.HardBreak) {
				b.Inlines = append(b.Inlines, docmodel.Inline{
					Marks: append(readMarkAttrs(c.attrsRaw), docmodel.Mark{Kind: docmodel.HardBreak}),
				})
				continue
			}
			sub, err := c.toBlock()
			if err != nil {
				return docmodel.Block{}, err
			}
			b.Children = append(b.Children, sub)
		default:
			return docmodel.Block{}, fmt.Errorf("ydoc: <%s> holds an unknown snapshot node %T", es.nodeName, child)
		}
	}
	return b, nil
}

func readBlock(el *crdt.YXmlElement) (docmodel.Block, error) {
	b := docmodel.Block{Kind: docmodel.BlockKind(el.NodeName)}
	if attrs := el.GetAttributes(); len(attrs) > 0 {
		b.Attrs = attrs
	}

	for _, child := range el.Children() {
		switch n := child.(type) {
		case *crdt.YXmlText:
			// Which field the text lands in is decided by the block's kind,
			// not by the node: a code block's text is raw, everything else's
			// is inline content.
			if rawText(b.Kind) {
				b.Text += n.ToString()
				continue
			}
			b.Inlines = append(b.Inlines, readInlines(n)...)
		case *crdt.YXmlElement:
			if n.NodeName == string(docmodel.HardBreak) {
				b.Inlines = append(b.Inlines, docmodel.Inline{
					Marks: append(readMarkAttrs(n.GetAttributeValues()), docmodel.Mark{Kind: docmodel.HardBreak}),
				})
				continue
			}
			sub, err := readBlock(n)
			if err != nil {
				return docmodel.Block{}, err
			}
			b.Children = append(b.Children, sub)
		default:
			return docmodel.Block{}, fmt.Errorf("ydoc: <%s> holds an unknown node type %T", el.NodeName, child)
		}
	}
	return b, nil
}

// readInlines turns one YXmlText into inlines, one per formatting run.
//
// ToDelta already splits the text at every formatting boundary and coalesces
// runs that share a mark set, which is the same normalization the markdown
// parser applies when it merges adjacent inlines.
func readInlines(txt *crdt.YXmlText) []docmodel.Inline {
	var out []docmodel.Inline
	for _, d := range txt.ToDelta() {
		s, ok := d.Insert.(string)
		if !ok || s == "" {
			continue
		}
		out = append(out, docmodel.Inline{Text: s, Marks: readMarkAttrs(d.Attributes)})
	}
	return out
}

// readMarkAttrs turns a formatting attribute map into marks, in canonical
// order: the kinds this package knows about first, in markOrder, then anything
// else alphabetically so an unrecognised mark from a TipTap extension survives
// a round trip rather than being dropped.
func readMarkAttrs(attrs map[string]any) []docmodel.Mark {
	if len(attrs) == 0 {
		return nil
	}
	known := make(map[string]bool, len(markOrder))
	var out []docmodel.Mark
	for _, k := range markOrder {
		known[string(k)] = true
		if v, ok := attrs[string(k)]; ok && v != nil {
			out = append(out, docmodel.Mark{Kind: k, Attrs: decodeMarkAttrs(v)})
		}
	}
	var extra []string
	for k, v := range attrs {
		if !known[k] && v != nil {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		out = append(out, docmodel.Mark{Kind: docmodel.MarkKind(k), Attrs: decodeMarkAttrs(attrs[k])})
	}
	return out
}

// decodeMarkAttrs reads a mark's attributes back out of its formatting value.
//
// The map form is what this package writes and what y-prosemirror writes. The
// string form is accepted too, decoded as JSON, so a peer that encoded the
// attributes into the attribute value is read rather than silently flattened
// to an attribute-less mark.
func decodeMarkAttrs(v any) map[string]string {
	var raw map[string]any
	switch t := v.(type) {
	case map[string]any:
		raw = t
	case string:
		if json.Unmarshal([]byte(t), &raw) != nil {
			return nil
		}
	default:
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		s, ok := val.(string)
		if !ok {
			s = fmt.Sprintf("%v", val)
		}
		out[k] = s
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
