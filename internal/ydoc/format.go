package ydoc

import (
	"unicode/utf16"
	"unicode/utf8"

	"github.com/reearth/ygo/crdt"
	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/review"
)

// WRITE APPLIES A MODEL WITHOUT DESTROYING THE DOCUMENT WHEN IT DOES NOT HAVE
// TO, AND THAT IS THE WHOLE OF WHY UNDO WORKS.
//
// `Load` deletes the fragment's children and writes them again. It is correct
// and it is what every server-side mutation used to do, so every one of them
// left the browser's undo stack pointing at items that no longer exist.
// Measured 2026-08-23 in a real chromium (`just undo-probe`): three edits typed,
// three levels of undo recovered with nothing filed — and after ONE instruction
// was filed, three ⌘Z changed the document not at all. Not one level. Zero.
//
// THE OBSERVATION THAT MAKES THE FIX SMALL: of the five server-side mutations,
// three change MARKS ONLY. Filing an instruction adds a highlight over a range,
// deleting one removes it, and sending a round clears them — no prose moves in
// any of the three, and those three are every gesture a reviewer makes while
// reviewing. The other two, restoring a version and importing the agent's
// draft, genuinely replace the document, and resetting undo across them is
// right rather than merely tolerable: there is nothing sensible to ⌘Z across
// "restore v4 as draft".
//
// SO THE DECISION IS MADE FROM THE TWO MODELS AND NOT FROM THE CALL SITE. Write
// compares what it is replacing with what it is writing; if the only difference
// is marks on identical text, it formats those runs in place, and otherwise it
// falls back to Load. Nothing about the callers changed, no caller can forget
// to opt in, and the fallback means the worst case is exactly today's
// behaviour. `EditServer.mutate`'s standing warning — do not attempt targeted
// fragment mutation before phase 3 — is answered rather than ignored: this
// touches no structure at all, and refuses the moment structure moves.
//
// IT REUSES `formatAttrs`, WHICH IS NOT AN ECONOMY. That function returns the
// COMPLETE attribute set for a run — every kind named, nil for the ones that
// are off — which is exactly what `Format` needs in order to both set and clear
// a mark. A second attribute builder here would be this repository's most
// recorded defect: two spellings of one rule, agreeing until the day one of
// them changes.
//
// Returns true when the write was targeted, for tests and for nothing else.
func Write(doc *crdt.Doc, tx review.Tx, before, after docmodel.Doc) bool {
	plan, structural, ok := planWrite(before, after)
	if !ok {
		Load(doc, tx, after)
		return false
	}
	if structural != nil {
		frag := doc.GetXmlFragment(FragmentName)
		tx(func(txn *crdt.Transaction) {
			if structural.del > 0 {
				frag.Delete(txn, structural.at, structural.del)
			}
			for i, b := range structural.insert {
				writeBlock(txn, frag, structural.at+i, b)
			}
		})
		return true
	}
	if len(plan) == 0 {
		// Nothing moved at all. Writing anything would be a document mutation
		// with no content behind it — which is a broadcast to every peer, and
		// an undo stack disturbed for no reason.
		return true
	}
	// Resolved before the transaction opens, the rule this package states
	// everywhere: GetXmlFragment briefly takes the document lock itself.
	frag := doc.GetXmlFragment(FragmentName)
	done := false
	tx(func(txn *crdt.Transaction) {
		done = applyPlan(txn, nodesOf(frag.Children()), before.Blocks, after.Blocks, plan)
	})
	if !done {
		// THE DOCUMENT AND THE MODEL DISAGREED, which under `mu` should not
		// happen — `before` was read from this same document moments ago. It is
		// checked anyway and it falls back rather than proceeding, because the
		// failure mode of formatting against wrong offsets is silently marking
		// the wrong words, and the failure mode of falling back is a lost undo
		// stack. Those are not close.
		Load(doc, tx, after)
		return false
	}
	return true
}

// planWrite reports what each changed block needs, and false if anything this
// cannot write exactly differs anywhere in the tree.
//
// What it refuses is STRUCTURE: kind, attributes, raw text, child count, inline
// count, and the position of every hard break. A hard break is structural even
// though it arrives as a mark, because `writeInlines` ends the current run at
// one — so a break that moved changes which run every following character lives
// in, and an offset computed from the model would land somewhere else entirely.
func planWrite(before, after docmodel.Doc) ([]blockWork, *structOp, bool) {
	if len(before.Blocks) == len(after.Blocks) {
		var plan []blockWork
		ok := walkPair(before.Blocks, after.Blocks, nil, &plan)
		return plan, nil, ok
	}
	// A DIFFERENT NUMBER OF TOP-LEVEL BLOCKS: one contiguous run replaced.
	//
	// The common prefix and suffix are trimmed and everything between them is
	// deleted and rewritten. That is not the minimal edit for two separate
	// insertions far apart — the blocks between them are rewritten too — and it
	// is deliberately not chased: minimality here costs an alignment algorithm
	// and buys nothing the reviewer can see, while the SCOPE is already the
	// point. The old behaviour rewrote the whole document.
	head := 0
	for head < len(before.Blocks) && head < len(after.Blocks) &&
		docmodel.Equal(docmodel.Doc{Blocks: before.Blocks[head : head+1]},
			docmodel.Doc{Blocks: after.Blocks[head : head+1]}) {
		head++
	}
	tail := 0
	for tail < len(before.Blocks)-head && tail < len(after.Blocks)-head &&
		docmodel.Equal(
			docmodel.Doc{Blocks: before.Blocks[len(before.Blocks)-1-tail : len(before.Blocks)-tail]},
			docmodel.Doc{Blocks: after.Blocks[len(after.Blocks)-1-tail : len(after.Blocks)-tail]}) {
		tail++
	}
	op := &structOp{
		at:     head,
		del:    len(before.Blocks) - head - tail,
		insert: after.Blocks[head : len(after.Blocks)-tail],
	}
	// The matched head and tail are IDENTICAL by docmodel.Equal, so they need
	// no text or mark work — which is why this returns no block plan. Anything
	// that differed is inside the replaced run and is written fresh.
	return nil, op, true
}

// structOp is a run of top-level blocks replaced in one go: delete `del` blocks
// at `at`, then write `insert` there.
//
// TOP LEVEL ONLY, AND THE REASON IS THE LIBRARY. `YXmlFragment` has `Delete`;
// `YXmlElement` does not — there is no way to remove a child from a list item
// or a blockquote through ygo's XML surface at all. So a block appearing or
// disappearing INSIDE a container still falls back to `Load`, and says so here
// rather than being discovered by whoever reads the plan and wonders.
type structOp struct {
	at     int
	del    int
	insert []docmodel.Block
}

func walkPair(before, after []docmodel.Block, at []int, plan *[]blockWork) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.Kind != a.Kind || b.Text != a.Text || !sameAttrs(b.Attrs, a.Attrs) {
			return false
		}
		here := append(append([]int(nil), at...), i)
		marks, edits, ok := inlinesDiffer(b.Inlines, a.Inlines)
		if !ok {
			return false
		}
		if marks || len(edits) > 0 {
			*plan = append(*plan, blockWork{path: here, marks: marks, edits: edits})
		}
		if !walkPair(b.Children, a.Children, here, plan) {
			return false
		}
	}
	return true
}

// blockWork is what one block needs: its marks re-asserted, some of its inlines'
// text replaced, or both.
type blockWork struct {
	path  []int
	marks bool
	// edits is the indices of the inlines whose TEXT changed, ascending.
	edits []int
}

// inlinesDiffer reports what moved inside one block: whether any MARK changed,
// which inlines' TEXT changed, and whether the block is something this can
// write at all.
//
// TEXT IS NO LONGER A REFUSAL, and that is phase 3's whole slice. It used to
// return false the moment any inline's text differed, which sent every edit —
// the agent's revision, a revert, a version restore — through `Load`, and Load
// deletes the fragment's children and writes them again. That is what throws
// the caret to the end of the document, orphans the browser's undo stack, and
// makes a keystroke landing between the read and the Apply clobberable
// anywhere in the file.
//
// WHAT IS STILL REFUSED IS STRUCTURE. A different number of inlines, a hard
// break coming or going (it ends a run, so every later offset moves), or one
// inline changing its TEXT and its MARKS at once — that last is not
// theoretically hard, it is simply two edits to one span, and doing them in one
// pass would mean deciding an order this has no reason to decide. Each falls
// back, and the fallback is why every case here is opt-in and provable on its
// own.
func inlinesDiffer(before, after []docmodel.Inline) (marks bool, edits []int, ok bool) {
	if len(before) != len(after) {
		return false, nil, false
	}
	for i := range before {
		// A hard break is a run boundary, so its coming or going is structure.
		if before[i].Has(docmodel.HardBreak) != after[i].Has(docmodel.HardBreak) {
			return false, nil, false
		}
		sameText := before[i].Text == after[i].Text
		sameMark := sameMarks(before[i].Marks, after[i].Marks)
		switch {
		case sameText && sameMark:
		case sameText:
			marks = true
		case sameMark:
			// AN INLINE THAT WENT EMPTY, OR ARRIVED FROM EMPTY, IS STRUCTURE.
			// `writeInlines` skips an empty inline entirely — `Insert` ignores
			// empty text — so there is no span in the document to edit, and the
			// offsets of everything after it in the run are computed as though
			// it were not there.
			if before[i].Text == "" || after[i].Text == "" {
				return false, nil, false
			}
			edits = append(edits, i)
		default:
			return false, nil, false
		}
	}
	return marks, edits, true
}

func sameAttrs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// sameMarks compares marks IN ORDER and including their attributes. Order is
// canonical on both sides — `readMarkAttrs` builds it that way and the markdown
// layer does too — so comparing in order is exact rather than merely cheap.
//
// The RUN attribute is included, and that is the opposite of `docmodel.Equal`'s
// rule for a reason. Equal excludes it because two parses of one file are the
// same document; here the question is whether the CRDT's marks match what is
// about to be written, and a run that changed IS a change the browser has to be
// told about.
func sameMarks(a, b []docmodel.Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || !sameAnyAttrs(a[i].Attrs, b[i].Attrs) {
			return false
		}
	}
	return true
}

func sameAnyAttrs(a, b map[string]string) bool { return sameAttrs(a, b) }

// applyPlan walks the fragment beside the model and does each block's work.
// The name is not writeBlocks because bridge.go already has one, and two
// functions of that name in one package writing the same fragment is a
// collision waiting to be read wrong. It returns false the moment the two disagree about shape, which is
// the caller's signal to fall back.
func applyPlan(txn *crdt.Transaction, children []any, before, blocks []docmodel.Block, plan []blockWork) bool {
	for _, work := range plan {
		if !writeOne(txn, children, before, blocks, work) {
			return false
		}
	}
	return true
}

func writeOne(txn *crdt.Transaction, children []any, before, blocks []docmodel.Block, work blockWork) bool {
	path := work.path
	node, block := children, blocks
	var el *crdt.YXmlElement
	for _, i := range path {
		if i >= len(node) || i >= len(block) {
			return false
		}
		e, isEl := node[i].(*crdt.YXmlElement)
		if !isEl {
			return false
		}
		el = e
		// Children() takes NO lock (see ReadLive), so this is safe inside the
		// transaction; every other read here is a plain field.
		node = nodesOf(e.Children())
		block = block[i].Children
	}
	if el == nil {
		return false
	}
	return writeInlineSpans(txn, node, blocksInlinesAt(before, path), blocksInlinesAt(blocks, path), work)
}

// blocksInlinesAt returns the inlines of the block the path names.
func blocksInlinesAt(blocks []docmodel.Block, path []int) []docmodel.Inline {
	cur := blocks
	for n, i := range path {
		if i >= len(cur) {
			return nil
		}
		if n == len(path)-1 {
			return cur[i].Inlines
		}
		cur = cur[i].Children
	}
	return nil
}

// writeInlineSpans applies one block's work: the TEXT of the inlines that
// changed, then the marks if any moved.
//
// TEXT FIRST, RIGHT TO LEFT, and both halves of that are load-bearing. Right to
// left because an edit changes the length of the run, so doing an earlier
// inline first would move every later inline's offset out from under the next
// edit. Text before marks because `formatSpans` re-asserts attributes across
// the block using the AFTER model's lengths, which are only true once the text
// is the after text.
//
// THE EDIT IS TRIMMED TO WHAT ACTUALLY MOVED. A common prefix and suffix are
// left alone, so changing one word in a paragraph deletes and re-inserts one
// word rather than the paragraph — which is what keeps the items around it, and
// therefore the browser's undo entries and anyone's caret, alive. Replacing the
// whole inline would work and would throw all of that away for no reason.
func writeInlineSpans(txn *crdt.Transaction, nodes []any, was, inlines []docmodel.Inline, work blockWork) bool {
	for i := len(work.edits) - 1; i >= 0; i-- {
		at := work.edits[i]
		run, off, ok := spanOf(nodes, was, at)
		if !ok {
			return false
		}
		oldText, newText := was[at].Text, inlines[at].Text
		keepHead := commonPrefix(oldText, newText)
		keepTail := commonSuffix(oldText[keepHead:], newText[keepHead:])
		cutFrom := off + utf16Len(oldText[:keepHead])
		cutLen := utf16Len(oldText[keepHead : len(oldText)-keepTail])
		// CHECKED AGAINST THE RUN BEFORE ANYTHING IS WRITTEN. Deleting past the
		// end of a run would take whatever follows with it, and a model that
		// disagrees with the document is the case this refuses rather than
		// guesses at. `Len` is a plain field read, not a locking one.
		if cutFrom+cutLen > run.Len() {
			return false
		}
		add := newText[keepHead : len(newText)-keepTail]
		if cutLen > 0 {
			run.Delete(txn, cutFrom, cutLen)
		}
		if add != "" {
			run.Insert(txn, cutFrom, add, formatAttrs(runKinds(inlines), inlines[at].Marks))
		}
	}
	if !work.marks {
		return true
	}
	return formatSpans(txn, nodes, was, inlines)
}

// spanOf locates one inline: the run it lives in and its UTF-16 offset within
// that run. It mirrors `writeInlines`' own iteration — one run per block, ended
// and restarted at each hard break, empty inlines skipped because `Insert`
// ignored them and so they occupy no offsets.
func spanOf(nodes []any, inlines []docmodel.Inline, want int) (*crdt.YXmlText, int, bool) {
	run, at, node := (*crdt.YXmlText)(nil), 0, 0
	next := func() bool {
		for node < len(nodes) {
			t, isText := nodes[node].(*crdt.YXmlText)
			node++
			if isText {
				run, at = t, 0
				return true
			}
		}
		return false
	}
	for i, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			run = nil
			continue
		}
		if in.Text == "" {
			continue
		}
		if run == nil && !next() {
			return nil, 0, false
		}
		if i == want {
			return run, at, true
		}
		at += utf16Len(in.Text)
	}
	return nil, 0, false
}

// commonPrefix and commonSuffix are in BYTES over the two strings, returned so
// the caller can slice with them; the offsets handed to the CRDT are converted
// to UTF-16 units, which is what YText indexes in. They stop on a rune boundary
// so a multi-byte character is never cut in half.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	// `n < len(a)` FIRST, and it is not defensive. When the two strings share a
	// whole prefix — one is the other plus a suffix — `n` lands exactly on
	// `len(a)`, and `a[n]` is out of range: measured as a panic, index 34 of a
	// 34-byte string, from an ordinary edit that appended to a sentence. A
	// position at the end of a string is already on a rune boundary and needs
	// no backing off at all.
	for n > 0 && n < len(a) && !utf8.RuneStart(a[n]) {
		n--
	}
	return n
}

func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	for n > 0 && !utf8.RuneStart(a[len(a)-n]) {
		n--
	}
	return n
}

// formatSpans re-applies every run's attributes from the model, mirroring
// `writeInlines`' own iteration exactly — one run per block, ended and restarted
// at each hard break, empty inlines skipped because `Insert` ignored them and so
// they occupy no offsets.
//
// It re-applies the WHOLE block rather than only the inlines that changed. That
// is not laziness: `Format` writes the difference against what is already in
// effect, so re-asserting an unchanged run is a no-op in the document, and
// computing a minimal set would be a second opinion about which inline moved on
// top of the one `inlinesDiffer` already formed.
func formatSpans(txn *crdt.Transaction, nodes []any, was, inlines []docmodel.Inline) bool {
	// THE KINDS ARE THE UNION OF BOTH SIDES, AND THE GAP IT CLOSES IS NARROW
	// AND REAL. `Format` applies the DIFFERENCE against what is already in
	// effect, so a kind the attribute set does not MENTION is left switched on —
	// `runKinds` says exactly that about `Insert`.
	//
	// For every kind this package KNOWS, `runKinds` already seeds the whole of
	// `markOrder`, so naming the after-inlines alone would clear them correctly.
	// What it would miss is an UNRECOGNISED kind — a mark from a TipTap
	// extension, which `readMarkAttrs` reads faithfully and `runKinds` exists to
	// preserve — that is present in `before` and gone from `after`. Nothing
	// would name it, so it would survive a removal that was asked for.
	//
	// (An earlier draft of this comment claimed the union was what fixed
	// TestReviseAndLiveSendTheSameRound. It was not: that failure was
	// `docmodel.Clone`'s — the two models were one object graph — and this was
	// changed on the way past. Left corrected rather than quietly deleted,
	// because a comment that credits the wrong fix teaches the next reader the
	// wrong thing to suspect.)
	kinds := runKinds(append(append([]docmodel.Inline(nil), was...), inlines...))
	run, at, node := (*crdt.YXmlText)(nil), 0, 0
	next := func() bool {
		for node < len(nodes) {
			t, isText := nodes[node].(*crdt.YXmlText)
			node++
			if isText {
				run, at = t, 0
				return true
			}
		}
		return false
	}
	for _, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			run = nil
			continue
		}
		if in.Text == "" {
			continue
		}
		if run == nil && !next() {
			return false
		}
		n := utf16Len(in.Text)
		// THE LENGTH IS CHECKED BEFORE ANYTHING IS WRITTEN. Formatting past the
		// end of a run would mark whatever follows, and a model that disagrees
		// with the document is exactly the case this refuses rather than
		// guesses at. Len is a plain field read, not a locking one.
		if at+n > run.Len() {
			return false
		}
		run.Format(txn, at, n, formatAttrs(kinds, in.Marks))
		at += n
	}
	return true
}

// nodesOf widens the library's unexported child-node slice to []any so this
// file can hold onto one. `crdt.xmlNode` cannot be named outside the package;
// ranging over it and assigning each element to `any` is what bridge.go's own
// walk does, one element at a time.
func nodesOf[T any](children []T) []any {
	out := make([]any, 0, len(children))
	for _, c := range children {
		out = append(out, c)
	}
	return out
}

// utf16Len is the length YText indexes in: UTF-16 code units, not runes and not
// bytes. This package already records what a rune-indexed edit does to a
// surrogate pair — it cuts it in half and leaves replacement characters.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }
