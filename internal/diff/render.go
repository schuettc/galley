package diff

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// render.go turns the ops into the four READ-ONLY views the spec asks for:
// clean (the default), in place, reverse, and side by side.
//
// THE SERVER RENDERS AND THE BROWSER DISPLAYS. There is one implementation of
// this algorithm and it is this one; the browser is handed finished HTML and
// re-derives nothing. That is this repository's most-repeated rule — the side
// that computed the answer publishes it — and it is worth more here than
// anywhere else, because a second implementation of the pairing would agree on
// every document anyone tried and disagree on the one that mattered.

// View names one of the four readings.
type View string

const (
	// ViewClean is the default: v12 is just v12, with no markup at all.
	ViewClean View = "clean"
	// ViewInPlace marks this round's changes in the text as it now reads.
	ViewInPlace View = "inplace"
	// ViewReverse is the same diff the other way — the text as it read before.
	ViewReverse View = "reverse"
	// ViewSideBySide is the two versions whole, side by side.
	ViewSideBySide View = "sbs"
)

// Views is every view, in the order the reader is offered them.
var Views = []View{ViewClean, ViewInPlace, ViewReverse, ViewSideBySide}

// Label is what the reader sees on the control for a view.
func (v View) Label() string {
	switch v {
	case ViewClean:
		return "clean"
	case ViewInPlace:
		return "in place"
	case ViewReverse:
		return "reverse"
	case ViewSideBySide:
		return "side by side"
	}
	return string(v)
}

// ValidView reports whether s names a view, so a request carrying a typo is
// refused rather than silently answered with the default.
func ValidView(s string) bool {
	for _, v := range Views {
		if string(v) == s {
			return true
		}
	}
	return false
}

var (
	reCodeSpan = regexp.MustCompile("`([^`]+)`")
	reStrong   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reEm       = regexp.MustCompile(`\*([^*]+)\*`)
	reHeadHash = regexp.MustCompile(`^#+\s*`)
)

// inline is the small amount of markdown this surface renders: escaping first,
// then code spans, strong and emphasis. It is deliberately not a markdown
// renderer — a diff view is for reading what moved, and a construct it does not
// know is shown as the author wrote it rather than swallowed.
//
// ESCAPING IS FIRST AND IS NOT NEGOTIABLE. Every string reaching here is
// document text, which is to say text somebody else wrote.
func inline(s string) string {
	out := html.EscapeString(s)
	out = reCodeSpan.ReplaceAllString(out, "<code>$1</code>")
	out = reStrong.ReplaceAllString(out, "<strong>$1</strong>")
	out = reEm.ReplaceAllString(out, "<em>$1</em>")
	return out
}

func insHTML(t string) string { return `<span class="gly-ins">` + inline(t) + `</span>` }
func delHTML(t string) string { return `<span class="gly-del">` + inline(t) + `</span>` }

// movedHTML is THE OPEN DECISION, and it is rendered rather than left blank so
// that a move is legible while the vocabulary question is settled.
//
// A MOVED SENTENCE IS NEITHER AN INSERTION NOR A DELETION. galley's marks are
// ins, del and highlight, and all three are wrong here: the words did not
// change, so painting the WORDS says something false, and showing a move as a
// delete plus an insert is the naive rendering sentence granularity exists to
// avoid. What changed is the sentence's PLACE, so what is marked is its place —
// a DASHED rule in --gly-muted and a tag saying which end this is, deliberately
// outside the three-mark vocabulary and matching the discipline the trail ghost
// was given when it was caught borrowing the agent's del-red: apart by colour
// AND by shape.
//
// This is NOT a decision anyone has taken. Court has not ruled on whether a
// move earns a fourth mark, and the spike used a dashed muted rule for the same
// reason: to avoid pre-empting it.
func movedHTML(t, tag string) string {
	return `<span class="gly-moved">` + inline(t) +
		`<span class="gly-moved-tag">` + html.EscapeString(tag) + `</span></span>`
}

// innerHTML is the word diff inside one sentence, for the case the threshold
// rule allowed. side "new" renders the sentence as it now reads with the
// removed words struck beside their replacements; side "old" renders it as it
// read before.
func innerHTML(a, b, side string) string {
	wa, ga := wordsWithGaps(a)
	wb, gb := wordsWithGaps(b)
	// THE SPACING IS THE SOURCE'S, NOT A GUESS. The tokenizer drops whitespace
	// so that respacing is not a change; putting one space between every token
	// on the way back out writes prose nobody typed — `per-host` came back
	// `per - host`, and `client.retry_budget` came back `client. retry_budget`
	// under a punctuation rule that could not tell a dotted identifier from the
	// end of a sentence. wordsWithGaps reports where the spaces actually were.
	join := func(ws []string, gaps []bool, lo, hi int) string {
		var b strings.Builder
		for i := lo; i < hi; i++ {
			if i > lo && gaps[i] {
				b.WriteByte(' ')
			}
			b.WriteString(ws[i])
		}
		return b.String()
	}
	// lead is the whitespace BEFORE a run, which belongs between the runs rather
	// than inside either of them: a run that starts mid-word (`per` + `-host`)
	// must not be pushed off the word it belongs to.
	lead := func(gaps []bool, lo, hi int) string {
		if lo < hi && lo > 0 && gaps[lo] {
			return " "
		}
		return ""
	}
	var out []string
	for _, oc := range InnerOps(a, b) {
		switch oc.Tag {
		case TagEqual:
			out = append(out, lead(ga, oc.I1, oc.I2)+inline(join(wa, ga, oc.I1, oc.I2)))
		case TagDelete:
			out = append(out, lead(ga, oc.I1, oc.I2)+delHTML(join(wa, ga, oc.I1, oc.I2)))
		case TagInsert:
			out = append(out, lead(gb, oc.J1, oc.J2)+insHTML(join(wb, gb, oc.J1, oc.J2)))
		default:
			// TWO WORDS AT ONE POSITION MUST STILL READ AS TWO WORDS — the space
			// between a deletion and its replacement is the renderer's, not the
			// diff's, and it is unconditional for that reason.
			d := delHTML(join(wa, ga, oc.I1, oc.I2))
			n := insHTML(join(wb, gb, oc.J1, oc.J2))
			pair := d + " " + n
			if side != "new" {
				pair = n + " " + d
			}
			out = append(out, lead(ga, oc.I1, oc.I2)+pair)
		}
	}
	return strings.Join(out, "")
}

// renderBlock is one finished block of a view.
type renderBlock struct {
	kind Kind
	// special is "gone" or "new" for a whole block removed or added, and empty
	// otherwise. A separate field rather than a fake Kind so the block's own
	// kind survives to the markup.
	special string
	body    string
	// region is this render's ordinal for a block that IS one whole change, and
	// -1 for a block that merely contains prose. A whole block gone is one
	// region and its element carries the id itself; the alternative — a span
	// wrapped around the whole body — would give the browser an element to
	// address that is not the element the reader sees move.
	region int
}

// regionAttr is THE BROWSER'S ONLY HANDLE ON A REGION, and it is computed per
// render and never stored.
//
// The value is the ordinal RegionOps just handed out for this pair of
// documents, which is also the index of the matching entry in the view
// response's Changes — that is the whole point of it: the card in the rail and
// the mark on the paper are two renderings of one region and must be able to
// find each other. CLAUDE.md: an ordinal ID is not identity and must never be
// persisted. This one lives exactly as long as the HTML it is written into; a
// later round renumbers everything and that is correct, because `CHANGE k OF K`
// is a fact about a reading, not about a change.
func regionAttr(k int) string {
	if k < 0 {
		return ""
	}
	return ` data-gly-region="` + itoa(k) + `"`
}

// mark stamps a region ordinal onto the element the region ALREADY renders as,
// instead of wrapping it in a second one. A region is always emitted here as a
// single outermost tag — gly-chg, gly-ins, gly-del, gly-moved, or the block
// element of a whole block — so there is an element to stamp, and adding
// another would put a box around the mark that the stylesheet knows nothing
// about and that the reader would eventually see.
func mark(s string, k int) string {
	if k < 0 || !strings.HasPrefix(s, "<") {
		return s
	}
	i := strings.IndexByte(s, '>')
	if i < 0 {
		return s
	}
	return s[:i] + regionAttr(k) + s[i:]
}

// regionIndex maps every op that PAINTS to the ordinal of the region it belongs
// to, by asking RegionOps rather than counting again.
//
// The insertion end of a move is not its own region and takes its partner's
// ordinal: the sentence moved once, the reader attends to it once, and both
// ends of it must answer to the same card.
func regionIndex(ops []*Op) map[*Op]int {
	m := make(map[*Op]int, len(ops))
	for k, o := range RegionOps(ops) {
		m[o] = k
		if o.Moved && o.Partner != nil {
			m[o.Partner] = k
		}
	}
	return m
}

// regionOf is the lookup, with -1 for anything unpainted or unmarked.
func regionOf(idx map[*Op]int, o *Op) int {
	if k, ok := idx[o]; ok {
		return k
	}
	return -1
}

// Render draws one of the four views as an HTML fragment. The caller supplies
// the two documents; nothing is read from disk and nothing is stored.
func Render(before, after string, view View) string {
	if view == ViewSideBySide {
		l, r := renderSideBySide(before, after)
		return `<div class="gly-sbs">` +
			`<div class="gly-sbs-col"><div class="gly-sbs-head">before</div>` + l + `</div>` +
			`<div class="gly-sbs-col"><div class="gly-sbs-head">after</div>` + r + `</div></div>`
	}
	return renderFlow(Diff(before, after), view)
}

func renderFlow(ops []*Op, view View) string {
	var blocks []renderBlock
	var cur []string
	curKind := KindPara
	// THE IDS ARE THIS RENDER'S, and they are handed out before a single tag is
	// written so that the paper and the rail cannot disagree about which change
	// is which. See regionAttr.
	idx := regionIndex(ops)
	flush := func() {
		if len(cur) == 0 {
			return
		}
		// A TABLE ROW AND A CODE LINE ARE EACH THE SENTENCE OF THEIR BLOCK, so
		// putting the block back together joins them with the LINE BREAK they
		// were split on. Joined with a space — which is right for prose and was
		// what the first cut did for everything — a three-row table came back
		// as `| setting | default | | --- | --- | | timeout | 30s |` on one
		// line, which is a table nobody can read about a change they can see.
		sep := " "
		if curKind.Atomic() {
			sep = "\n"
		}
		blocks = append(blocks, renderBlock{kind: curKind, body: strings.Join(cur, sep), region: -1})
		cur = nil
	}
	for _, o := range ops {
		if o.Absorbed {
			continue
		}
		u := o.Old
		if u == nil {
			u = o.New
		}
		if u.Kind == KindBoundary && o.Op == OpEqual {
			flush()
			curKind = u.BKind
			continue
		}
		if o.Op == OpEqual {
			text := o.New.Text
			if view == ViewReverse {
				text = o.Old.Text
			}
			if view == ViewClean {
				text = o.New.Text
			}
			cur = append(cur, inline(text))
			continue
		}
		if view == ViewClean {
			// CLEAN IS THE DOCUMENT AND CARRIES NO MARKUP. Only the new side
			// survives: a change reads as its new sentence, an insertion as
			// itself, a deletion as nothing at all.
			switch o.Op {
			case OpChange:
				cur = append(cur, inline(o.New.Text))
			case OpInsert:
				if o.WholeBlock {
					flush()
					blocks = append(blocks, renderBlock{kind: blockKind(o, o.New), body: memberText(o, "new", inline), region: -1})
				} else {
					cur = append(cur, inline(o.New.Text))
				}
			}
			continue
		}

		k := regionOf(idx, o)
		switch o.Op {
		case OpChange:
			var body string
			switch {
			case o.Inner:
				s := "new"
				if view == ViewReverse {
					s = "old"
				}
				body = innerHTML(o.Old.Text, o.New.Text, s)
			case view == ViewReverse:
				body = insHTML(o.New.Text) + " " + delHTML(o.Old.Text)
			default:
				body = delHTML(o.Old.Text) + " " + insHTML(o.New.Text)
			}
			cur = append(cur, mark(`<span class="gly-chg">`+body+`</span>`, k))
		case OpDelete:
			txt := memberText(o, "old", func(s string) string { return s })
			switch {
			case o.Moved:
				cur = append(cur, mark(movedHTML(txt, "moved away"), k))
			case o.WholeBlock:
				flush()
				blocks = append(blocks, renderBlock{
					kind: blockKind(o, o.Old), special: "gone",
					body:   delHTML(txt) + `<span class="gly-diff-tag">` + blockGoneLabel(o) + `</span>`,
					region: k,
				})
			default:
				cur = append(cur, mark(delHTML(txt), k))
			}
		case OpInsert:
			txt := memberText(o, "new", func(s string) string { return s })
			switch {
			case o.Moved:
				cur = append(cur, mark(movedHTML(txt, "moved here"), k))
			case o.WholeBlock:
				flush()
				blocks = append(blocks, renderBlock{
					kind: blockKind(o, o.New), special: "new",
					body:   insHTML(txt) + `<span class="gly-diff-tag">` + blockAddedLabel(o) + `</span>`,
					region: k,
				})
			default:
				cur = append(cur, mark(insHTML(txt), k))
			}
		}
	}
	flush()

	var out []string
	for _, b := range blocks {
		out = append(out, wrapBlock(b))
	}
	return strings.Join(out, "\n")
}

func blockKind(o *Op, u *Unit) Kind {
	if o.KindHint != "" {
		return o.KindHint
	}
	if u != nil {
		return u.Kind
	}
	return KindPara
}

// memberText joins a coalesced region's own sentences, so a whole paragraph
// removed reads as the paragraph and not as its first sentence.
func memberText(o *Op, side string, f func(string) string) string {
	if len(o.Members) == 0 {
		u := o.Old
		if side == "new" {
			u = o.New
		}
		if u == nil {
			return ""
		}
		return f(u.Text)
	}
	var parts []string
	for _, m := range o.Members {
		u := m.Old
		if side == "new" {
			u = m.New
		}
		if u != nil {
			parts = append(parts, f(u.Text))
		}
	}
	return strings.Join(parts, " ")
}

func blockGoneLabel(o *Op) string {
	if o.Count == 1 {
		return "removed"
	}
	return "removed — " + plural(o.Count, "sentence") + " at once"
}

func blockAddedLabel(o *Op) string {
	if o.Count <= 1 {
		return "added"
	}
	return "added — " + plural(o.Count, "sentence")
}

func plural(n int, word string) string {
	s := itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func wrapBlock(b renderBlock) string {
	a := regionAttr(b.region)
	switch b.special {
	case "gone":
		return `<p class="gly-diff-gone"` + a + `>` + b.body + `</p>`
	case "new":
		return `<p class="gly-diff-added"` + a + `>` + b.body + `</p>`
	}
	switch b.kind {
	case KindHeading:
		// THE LEVEL IS THE HEADING'S, NOT A CONSTANT. Every heading used to be
		// drawn as an h3, so a document's title and a section head inside it
		// came out the same size — the same document read one way in the draft
		// and another in History, which is the second typography this surface
		// exists not to have. The hashes still go; the level they encoded does
		// not.
		return `<h` + strconv.Itoa(headingLevel(b.body)) + ` class="gly-diff-h">` +
			reHeadHash.ReplaceAllString(b.body, "") +
			`</h` + strconv.Itoa(headingLevel(b.body)) + `>`
	case KindCode, KindTable, KindMath:
		return `<pre class="gly-diff-atomic">` + b.body + `</pre>`
	case KindListItem:
		return `<ul class="gly-diff-list"><li>` + b.body + `</li></ul>`
	case KindQuote:
		return `<blockquote class="gly-diff-quote">` + b.body + `</blockquote>`
	}
	return `<p>` + b.body + `</p>`
}

// renderSideBySide draws the two versions whole, with the changed sentences lit
// on both sides. Nothing is struck or inserted here: the point of this view is
// that each version reads as itself.
//
// IT WRAPS ITS BLOCKS LIKE THE OTHER THREE. The first cut emitted every column
// as a run of paragraphs, so a heading read as `# The rail is a map` and a
// three-row table as one line of pipes — the same defect the flow renderer had,
// in the one view whose whole claim is that each version reads AS ITSELF.
//
// AND IT STAMPS ITS REGIONS LIKE THE OTHER TWO THAT CARRY MARKUP, which is the
// same lesson a third time. The reading rail places each change card beside its
// mark by looking `[data-gly-region="k"]` up in the rendered diff; this view
// stamped nothing, so choosing `side by side` failed every lookup at once and
// every card fell back to stacking from the top of the rail. CLAUDE.md: A CLICK
// MOVES NOTHING EXCEPT THE THING THAT WAS CLICKED — the reader clicked a
// reading, and the rail beside it is not the thing they clicked. A view that
// paints a change and does not name it is a view the rail cannot be drawn
// against, so the stamp belongs to the renderer and not to whoever remembers.
//
// THE ORDINALS ARE RegionOps' AND NOT THIS FUNCTION'S. It walks the same ops
// slice renderFlow walks and asks regionIndex — the shared lookup — for every
// k, rather than counting changed ops as it goes. Counting again would produce
// the same numbers on every document anyone tried and different ones the day a
// filter moved (a move is one region at its DELETION, an absorbed op is no
// region at all), and the disagreement would be silent: a card would point at
// one change in `changes` and at a different one in `side by side`, with each
// view internally consistent and nothing to catch it. CLAUDE.md's own words for
// it: one list, three readers.
//
// THE AFTER COLUMN CARRIES THE ID WHEREVER THERE IS AN AFTER. A card is the
// agent's testimony about what it WROTE, so the sentence the card describes is
// the one on the right, and that is where the reader should be taken and what
// should light beside its card. It is also the only choice that keeps the id
// UNIQUE: the browser resolves a region with querySelector, which answers with
// the first match in document order, and the before column is emitted first —
// so stamping both sides would silently hand every card the old reading of its
// own change. That is why a MOVE is stamped at its arrival here and at both
// ends in the flow views: two columns are one lookup, and the end the agent put
// there is the after end.
//
// A LOOKUP THAT CAN MATCH TWO ELEMENTS IS NOT A LOOKUP, and the browser side of
// this contract is ALREADY weaker than it reads — which is said out loud here
// because this is where the decision gets made for the first time. renderFlow
// stamps both ends of a move with the one ordinal a move earns (deliberate, and
// asserted in diff_test.go), so a region id is not unique in that markup
// either; versions.js then reads it two ways — align() and pick() take
// querySelector's FIRST match while paintPick() takes querySelectorAll's every
// match, so a moved sentence already lights at both ends and scrolls to one.
// That is a browser-side question and this package is not the place to answer
// it. What this package can refuse to do is make the rare case the common one:
// side by side renders every changed sentence TWICE, so stamping both columns
// would put a duplicate id on every ordinary change and not merely on a move.
// The next reader who wants the second column stamped for symmetry is the
// reason this paragraph exists — symmetry is not the property, findability is,
// and a card can only be beside one thing.
func renderSideBySide(before, after string) (string, string) {
	ops := Diff(before, after)
	idx := regionIndex(ops)
	l, r := newColumn(), newColumn()
	for _, o := range ops {
		if o.Absorbed {
			continue
		}
		u := o.Old
		if u == nil {
			u = o.New
		}
		k := regionOf(idx, o)
		if u.Kind == KindBoundary {
			if o.Op == OpEqual {
				l.brk(u.BKind)
				r.brk(u.BKind)
				continue
			}
			// A BREAK THAT EXISTS IN ONLY ONE VERSION IS A REGION WITH NOTHING
			// TO LIGHT, and it still gets a card. Splitting a paragraph in two
			// changes no sentence, so neither column has a word to paint —
			// but the count on the wire includes it, the rail draws a card for
			// it, and a card whose lookup fails is the whole defect above. So
			// the id goes on an EMPTY span at the position the break was made
			// or unmade: nothing to see, something to find. It carries its own
			// class rather than gly-lit-old/new because it is not a lit
			// sentence and must never be given a lit sentence's paint — an
			// empty box in the change colour is a mark for words that do not
			// exist. The side is the side the break is a fact about: an
			// inserted break is a fact about the after text, a removed one
			// about the before.
			anchor := `<span class="gly-lit-break"></span>`
			if o.Op == OpInsert {
				r.push(mark(anchor, k))
			} else {
				l.push(mark(anchor, k))
			}
			continue
		}
		switch o.Op {
		case OpEqual:
			l.push(inline(o.Old.Text))
			r.push(inline(o.New.Text))
		case OpChange:
			l.push(`<span class="gly-lit-old">` + inline(o.Old.Text) + `</span>`)
			r.push(mark(`<span class="gly-lit-new">`+inline(o.New.Text)+`</span>`, k))
		case OpDelete:
			// A DELETION HAS NO AFTER TEXT, AND ITS CARD STILL HAS TO SIT
			// SOMEWHERE. The rule above says the after column wherever there is
			// an after; here there is none, and the honest reading of that is
			// not "leave it unstamped" — an unstamped region is exactly the
			// card that falls back to the top of the rail and moves, which is
			// the defect this whole function is being changed for. So the id
			// goes on the left, the one element the region renders as, and the
			// card lands beside the words that are about to stop existing. The
			// uniqueness argument is untouched: a deletion has no counterpart
			// on the right to compete with it. The ARRIVAL end of a move is not
			// this case — it is an OpInsert with a partner, it is on the right,
			// and it takes the ordinal there, so the departure end is left
			// unstamped rather than claiming its partner's id a second time.
			body := `<span class="gly-lit-old">` + inline(memberText(o, "old", func(s string) string { return s })) + `</span>`
			if o.Moved && o.Partner != nil && !o.Partner.Absorbed {
				// The arrival end will be stamped on the right, so the
				// departure end must not claim the same id a second time. The
				// guard is on the partner being RENDERED and not merely on
				// Moved, because a partner swallowed into a coalesced
				// neighbour never reaches this loop — and the ordinal would
				// then be carried by nobody, which fails silently: the card
				// looks fine and sits in the wrong place.
				l.push(body)
				break
			}
			l.push(mark(body, k))
		case OpInsert:
			r.push(mark(`<span class="gly-lit-new">`+inline(memberText(o, "new", func(s string) string { return s }))+`</span>`, k))
		}
	}
	return l.done(), r.done()
}

// column accumulates one side of the side-by-side reading, block by block.
type column struct {
	out  []string
	cur  []string
	kind Kind
}

func newColumn() *column { return &column{kind: KindPara} }

func (c *column) push(s string) { c.cur = append(c.cur, s) }

// brk ends the block in hand and names the one that follows.
func (c *column) brk(next Kind) {
	c.flush()
	c.kind = next
}

func (c *column) flush() {
	if len(c.cur) == 0 {
		return
	}
	sep := " "
	if c.kind.Atomic() {
		sep = "\n"
	}
	c.out = append(c.out, wrapBlock(renderBlock{kind: c.kind, body: strings.Join(c.cur, sep)}))
	c.cur = nil
}

func (c *column) done() string {
	c.flush()
	return strings.Join(c.out, "\n")
}

// headingLevel counts the hashes a heading opens with, clamped to what HTML
// has. A block reaches here as its own raw markdown line, so the level is still
// in the bytes; nothing upstream carries it as a field.
func headingLevel(body string) int {
	n := 0
	for _, r := range strings.TrimLeft(body, " \t") {
		if r != '#' {
			break
		}
		n++
		if n == 6 {
			break
		}
	}
	if n == 0 {
		return 3
	}
	return n
}
