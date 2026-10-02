package markdown

import (
	"sort"
	"strings"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/yuin/goldmark/util"
)

// render_inline.go turns a block's docmodel.Inline sequence into markdown
// text, in two passes. Deciding whether to backslash-escape a character
// requires seeing past the boundary of the single Inline it came from — a
// lone "*" immediately before a Bold run's own "**" delimiter fuses into
// "***" on emission, and a "[x]" immediately before an unrelated, later
// Inline's literal "(y)" reads back as a real link — so escaping cannot be
// decided per Inline (fix round 2). Pass 1 (plan) lays out the block's
// entire output as a sequence of runes, each tagged with whether it is
// escapable inline-text content or a markup delimiter this renderer is
// itself emitting (never escaped). Pass 2 (render) walks that whole
// sequence and decides escaping — via needsEscapeAt in escape.go — with
// every real neighbor visible, regardless of which Inline or which
// delimiter contributed it.

// pchar is one planned output rune, tagged with whether it originated from
// inline text content (a candidate for escaping) or is markup the
// renderer itself is emitting (marker delimiters, a hard break, a code
// span's fence — always literal, never escaped).
type pchar struct {
	r         rune
	escapable bool
	// label marks a rune that will sit inside a link's or image's square
	// brackets, where "[" and "]" close the label rather than being text.
	label bool
}

// renderInlines is the block-level entry point for Paragraph and Heading:
// it plans the full output for a block's Inlines, then resolves escaping
// over the complete plan.
//
// It merges adjacent Inlines with identical mark sets before planning,
// via the same mergeAdjacent Parse itself uses (parse.go) to guarantee
// its own output. Task 3's original brief could rely on Parse's runs
// already being maximal; Task 7's suggestion transforms don't have to
// preserve that — a del/ins boundary can split what was one run into two
// adjacent Inlines that still carry identical marks (fix round 3). That
// merge is lossless: sameMarks compares Marks by Attrs too, so it never
// merges runs a caller distinguished on purpose (e.g. two Ins spans with
// different "at" timestamps).
//
// Merging alone is not enough, though, because it only fires on
// IDENTICAL mark sets — see planInlines' segment grouping for the
// differing-marks case, which is what a suggestion covering half a run
// produces.
//
// ctx says what the enclosing block does to the lines this output becomes,
// which is the part of escaping that cannot be seen from the inlines. A
// paragraph's output begins a line in the file, so a leading "-" there is a
// bullet list; a heading's does not, because the "# " prefix is already
// there, so the same "-" is only text. A heading has a rule at the other
// end of the line instead — see closesHeading.
func renderInlines(inlines []docmodel.Inline, ctx lineContext) string {
	return renderPlan(planInlines(normalize(inlines).inlines), ctx)
}

// lineContext is what renderBlock knows and the inline planner does not:
// where this output sits on the lines it will occupy.
type lineContext struct {
	// atLineStart is true when the plan's first rune leads a line in the
	// file, so a block marker there would be read as block structure.
	atLineStart bool
	// heading is true when the plan is an ATX heading's content, whose
	// trailing "#" run markdown strips as a closing sequence.
	heading bool
}

// planInput is a block's Inlines as the planner sees them, together with
// where each one came from in the caller's original slice.
//
// The provenance is what lets Substitutions answer in the CALLER's
// coordinates. suggest.List groups a Del span and an Ins span into one
// decision using this package's substitution predicate, and it addresses
// its spans by index into the block's own Inlines; a range in the
// normalized slice would be a coordinate it could not use.
type planInput struct {
	inlines []docmodel.Inline
	// from[i] and to[i] bound, in the ORIGINAL slice, the run of Inlines
	// that was dropped into or merged into inlines[i].
	from, to []int
}

// normalize is what every plan is laid out over: Inlines that carry no text
// are dropped, and neighbours whose mark sets are identical are merged.
//
// Empties go first because there is nothing to wrap in an empty run's marks
// — an empty Del would write "{----}", whose reader finds the closer where
// the content should be — and because a zero-width run left in place would
// separate two code Inlines that then choose fences that fuse. The
// hard-break sentinel is kept, whose emptiness is the point.
//
// The merge is the same rule Parse guarantees its own output by
// (mergeAdjacent, parse.go) and shares its predicates: sameMarks compares
// Marks by Attrs too, so it never merges runs a caller distinguished on
// purpose (two Ins spans with different "at" timestamps stay two). Task 3's
// original brief could rely on Parse's runs already being maximal; Task 7's
// suggestion transforms don't have to preserve that — a del/ins boundary
// can split what was one run into two adjacent Inlines that still carry
// identical marks (fix round 3).
//
// Merging alone is not enough, though, because it only fires on IDENTICAL
// mark sets — see planInlines' segment grouping for the differing-marks
// case, which is what a suggestion covering half a run produces.
//
// The two passes are FUSED rather than composed because composing them
// loses which originals a merged run came from, and that mapping is the
// whole of what makes Substitutions expressible outside this package.
func normalize(inlines []docmodel.Inline) planInput {
	var in planInput
	for i, cur := range inlines {
		if cur.Text == "" && !isHardBreak(cur) {
			continue
		}
		if k := len(in.inlines) - 1; k >= 0 &&
			sameMarks(in.inlines[k].Marks, cur.Marks) &&
			!isHardBreak(cur) && !isHardBreak(in.inlines[k]) {
			in.inlines[k].Text += cur.Text
			in.to[k] = i + 1
			continue
		}
		in.inlines = append(in.inlines, cur)
		in.from = append(in.from, i)
		in.to = append(in.to, i+1)
	}
	return in
}

// planInlines lays out a whole block's Inlines as a sequence of SEGMENTS.
// A segment is a maximal run of adjacent Inlines that agree on both:
//
//   - their wrapper — the marks that emit delimiters around a run (bold,
//     italic, link + href; see codeWrapper), and
//   - their suggestion identity — which of Ins/Del/Highlight they carry,
//     with the mark Attrs that say WHICH span it is (author, at).
//
// One segment emits one of everything: one code fence over all its code
// text, one set of emphasis/link delimiters, one CriticMarkup marker pair.
//
// That is what keeps two delimiter runs of the same kind from ever landing
// flush against each other — the corruption fix round 4 chased in code
// fences ("`a`" next to "`b`" is a 2-backtick run that closes nothing) and
// the identical hazard one mark up ("**a**" next to "**b**" is a "****"
// run that closes nothing).
//
// The invariant that makes that true is about what is actually EMITTED,
// not about what the marks are: two adjacent planned runs are always
// separated by at least one delimiter character. A wrapper change supplies
// emphasis or link delimiters, and a suggestion change supplies
// CriticMarkup markers — EXCEPT where wrapSuggestion finds no safe
// spelling and writes no markers at all. There the suggestion change
// separates nothing, so the segments either side of it would put their
// fences flush. Such a segment is therefore coalesced with its
// same-wrapper neighbours (see unmarkedRunEnd) and the whole run is
// planned as one: one fence over the concatenated text, suggestion marks
// dropped, text intact — exactly round 4's answer, applied in precisely
// the case Task 4's answer cannot spell.
//
// Round 4 had to group code runs ACROSS a suggestion change — Code("a")
// next to Code+Del("b") became one fence over "ab", losing the Del —
// because Ins/Del/Highlight emitted nothing back then, leaving the fences
// flush. Now they emit markers, so the boundary is real and the shape is
// lossless: "`a`{--`b`--}". A suggestion difference is a segment boundary
// only when it is a REAL difference — a different kind, or a different
// span (different Attrs); identical suggestion marks are merged by
// mergeAdjacent before planning ever starts.
//
// A Del segment immediately followed by an Ins segment is the
// substitution shape, {~~old~>new~~}, handled by planSubstitution.
func planInlines(inlines []docmodel.Inline) []pchar {
	var out []pchar
	walkPlan(inlines, func(u planUnit) { out = append(out, u.body...) })
	return out
}

// planUnit is one unit of walkPlan's walk: a hard break, a substitution
// spanning two segments, or a single segment — which may itself be a run
// coalesced over several marker-less ones.
type planUnit struct {
	body []pchar
	// start and end bound the unit in the walked slice. delEnd splits a
	// substitution into its deleted half, [start, delEnd), and its inserted
	// half, [delEnd, end); it is ZERO for every other unit, which is how a
	// substitution is told apart (a substitution's delEnd is at least
	// start+1, so it can never be zero).
	start, end, delEnd int
}

// walkPlan is the walk planInlines emits, factored out so that
// Substitutions can decide what one suggestion is by RUNNING the
// renderer's own decision rather than restating it. Both callers see the
// same units in the same order; the renderer keeps the bodies and
// Substitutions keeps the boundaries.
func walkPlan(inlines []docmodel.Inline, unit func(planUnit)) {
	for i := 0; i < len(inlines); i++ {
		if isHardBreak(inlines[i]) {
			// A hard break is a zero-text sentinel that emits its own
			// line ending; it ends whatever segment preceded it.
			unit(planUnit{body: literalChars("\\\n"), start: i, end: i + 1})
			continue
		}
		end := segmentEnd(inlines, i)
		if body, next, ok := planSubstitution(inlines, i, end); ok {
			unit(planUnit{body: body, start: i, delEnd: end, end: next})
			i = next - 1
			continue
		}
		body, emitted := planSegment(inlines[i:end])
		if !emitted {
			// This segment wrote no markers, so nothing separates it from
			// a same-wrapper neighbour. Absorb every such neighbour and
			// plan them as one run, which puts one fence (and one set of
			// emphasis delimiters) around the lot.
			end = unmarkedRunEnd(inlines, i, end)
			body = planUnmarkedRun(inlines[i:end])
		}
		unit(planUnit{body: body, start: i, end: end})
		i = end - 1
	}
}

// Substitution names one pair of inline ranges the serializer writes as a
// single "{~~old~>new~~}" span: the deleted half is [DelStart, DelEnd) and
// the inserted half [InsStart, InsEnd), as indices into the Inlines slice
// that was handed to Substitutions.
type Substitution struct {
	DelStart, DelEnd int
	InsStart, InsEnd int
}

// Substitutions reports every substitution Serialize will write for a
// block's Inlines.
//
// It exists so that ONE suggestion in the file is one suggestion
// everywhere else. "{~~brown~>red~~}" is a single span on disk, and
// suggest.List used to report it as two independently decidable ones:
// reject the delete half alone and the file says "brown{++red++}", whose
// only completion is the word "brownred", which neither the author nor the
// reviewer ever wrote. That is reached by two individually reasonable
// decisions, with the intermediate state already on disk.
//
// The rule that decides what pairs is planSubstitution's — a PURE deletion
// immediately followed by a PURE insertion under the same wrapper — and it
// has to stay one rule: a second copy is how the file, `galley pending`,
// the CLI and the rail start disagreeing about what one suggestion is. So
// this restates nothing. It runs walkPlan, the renderer's own walk, and
// reports where that walk chose to pair. Where the serializer DECLINES —
// the substitution markers occur in the text, so one span would be
// ambiguous (unsafeForSubstitution) — the file genuinely holds two spans
// and nothing is reported, which is the honest answer there.
//
// The suggestion model stays outside this package: nothing here knows what
// a `run` is, and the only coordinate spoken back is an index into the
// Inlines the caller handed over.
func Substitutions(inlines []docmodel.Inline) []Substitution {
	in := normalize(inlines)
	var out []Substitution
	walkPlan(in.inlines, func(u planUnit) {
		if u.delEnd == 0 {
			return
		}
		out = append(out, Substitution{
			DelStart: in.from[u.start],
			DelEnd:   in.to[u.delEnd-1],
			InsStart: in.from[u.delEnd],
			InsEnd:   in.to[u.end-1],
		})
	})
	return out
}

// unmarkedRunEnd extends the marker-less segment [start, end) over every
// following segment that shares its wrapper and also writes no markers. It
// stops at a hard break, at a wrapper change (whose delimiters separate
// the runs), and at the first segment that does write markers (which
// separate them itself).
func unmarkedRunEnd(inlines []docmodel.Inline, start, end int) int {
	w := wrapperOf(inlines[start])
	for end < len(inlines) {
		in := inlines[end]
		if in.Has(docmodel.HardBreak) || wrapperOf(in) != w {
			break
		}
		next := segmentEnd(inlines, end)
		if _, emitted := planSegment(inlines[end:next]); emitted {
			break
		}
		end = next
	}
	return end
}

// planUnmarkedRun plans a coalesced run of marker-less segments: one body
// (so a code run spanning the whole thing takes ONE fence) inside one set
// of emphasis and link delimiters. No CriticMarkup is written — by
// definition, none of these segments had a spelling.
func planUnmarkedRun(run []docmodel.Inline) []pchar {
	return wrapMarks(planSegmentBody(run), run[0])
}

// planSegment plans one segment: its body, then its emphasis and link
// delimiters, then its CriticMarkup markers OUTSIDE those — "{--**x**--}",
// "{--[x](y)--}", "{--`x`--}".
//
// Markers outermost is not cosmetic. CommonMark decides whether a "**" or
// "*" can open or close emphasis from the characters on either side of it
// (the flanking rules), and a marker is punctuation. With the markers
// inside, "a**{--x--}**" has an opener followed by "{" and preceded by a
// letter, which is not left-flanking, so it never reads back as bold at
// all — the emphasis is silently lost. With the markers outside,
// "a{--**x**--}" puts punctuation on the far side of every emphasis
// delimiter, which is exactly what the flanking rules accept. It is also
// the form CriticMarkup is written in by hand: the marker wraps the
// phrase, formatting and all.
// It reports whether any CriticMarkup marker was actually written:
// planInlines needs that answer to know whether this segment is separated
// from its neighbour by anything at all (see unmarkedRunEnd).
func planSegment(run []docmodel.Inline) ([]pchar, bool) {
	return wrapCritic(wrapMarks(planSegmentBody(run), run[0]), run[0])
}

// segmentEnd returns the exclusive end index of the segment starting at
// start: the maximal run of Inlines sharing start's segment key. A hard
// break always ends a segment.
func segmentEnd(inlines []docmodel.Inline, start int) int {
	k := segmentKey(inlines[start])
	end := start + 1
	for end < len(inlines) {
		in := inlines[end]
		if in.Has(docmodel.HardBreak) || segmentKey(in) != k {
			break
		}
		end++
	}
	return end
}

// segmentKey is what adjacent Inlines must agree on to share one set of
// delimiters. It is a comparable struct on purpose: grouping compares
// keys with ==, which is exact for the href and for the suggestion key.
type segKey struct {
	wrapper codeWrapper
	sugg    string
}

func segmentKey(in docmodel.Inline) segKey {
	return segKey{wrapper: wrapperOf(in), sugg: suggestionKey(in)}
}

// suggestionKinds is the fixed order suggestion marks are keyed and
// wrapped in — outermost first. Ins/Del sit innermost so that an
// insertion inside a highlighted region writes as {=={++x++}==}: the
// highlight is the anchor, the suggestion is the change within it.
var suggestionKinds = []docmodel.MarkKind{docmodel.Highlight, docmodel.Del, docmodel.Ins}

// suggestionKey canonicalizes an Inline's suggestion marks — kinds in
// suggestionKinds order, each with its Attrs sorted by key — into a
// comparable string. Attrs are part of the key because they are what make
// two same-kind spans DIFFERENT spans: two Ins runs by different authors
// are two suggestions, and must not be written as one marker pair.
func suggestionKey(in docmodel.Inline) string {
	var b strings.Builder
	for _, kind := range suggestionKinds {
		for _, m := range in.Marks {
			if m.Kind != kind {
				continue
			}
			b.WriteString(string(kind))
			b.WriteByte('(')
			keys := make([]string, 0, len(m.Attrs))
			for k := range m.Attrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b.WriteString(k)
				b.WriteByte('=')
				b.WriteString(m.Attrs[k])
				b.WriteByte(';')
			}
			b.WriteByte(')')
			break
		}
	}
	return b.String()
}

// codeWrapper is the part of an Inline's mark set that emits delimiters
// around a code span — precisely what wrapMarks' wrap calls key on.
//
// It is a comparable struct on purpose: segment grouping compares
// wrappers with ==, which is exact for the href too.
type codeWrapper struct {
	bold   bool
	italic bool
	link   bool
	href   string
}

func wrapperOf(in docmodel.Inline) codeWrapper {
	return codeWrapper{
		bold:   in.Has(docmodel.Bold),
		italic: in.Has(docmodel.Italic),
		link:   in.Has(docmodel.Link),
		href:   in.Attr(docmodel.Link, "href"),
	}
}

// planSegmentBody plans a segment's content: its text as escapable
// content runes, except that a contiguous stretch of Code-marked members
// becomes ONE fenced code span over the concatenation of their text.
// Code-span content is never backslash-escaped, so the fence and its body
// are literal runes; the fence width is chosen from the full
// concatenation, so no member can pick a fence that fuses with its
// neighbor's (fix round 4).
func planSegmentBody(run []docmodel.Inline) []pchar {
	var out []pchar
	for i := 0; i < len(run); i++ {
		if !run[i].Has(docmodel.Code) {
			out = append(out, contentChars(run[i].Text)...)
			continue
		}
		var text strings.Builder
		end := i
		for end < len(run) && run[end].Has(docmodel.Code) {
			text.WriteString(run[end].Text)
			end++
		}
		out = append(out, literalChars(renderCode(text.String()))...)
		i = end - 1
	}
	return out
}

// planSubstitution plans "{~~old~>new~~}" when the segment [start, delEnd)
// is a pure deletion immediately followed by a segment that is a pure
// insertion under the same wrapper — the shape Parse produces from a
// substitution, so writing it back as one span is what closes that round
// trip. It returns the planned runes and the index just past the
// insertion segment. Each half keeps its own emphasis and link
// delimiters, inside the one shared marker pair.
//
// "Pure" is required in both directions: a Del that also carries a
// Highlight is not the deleted half of a substitution, it is a highlighted
// deletion, and collapsing the two would drop the highlight.
func planSubstitution(inlines []docmodel.Inline, start, delEnd int) ([]pchar, int, bool) {
	if delEnd >= len(inlines) {
		return nil, 0, false
	}
	del, ins := inlines[start], inlines[delEnd]
	if !onlySuggestion(del, docmodel.Del) || !onlySuggestion(ins, docmodel.Ins) {
		return nil, 0, false
	}
	if ins.Has(docmodel.HardBreak) || wrapperOf(del) != wrapperOf(ins) {
		return nil, 0, false
	}
	insEnd := segmentEnd(inlines, delEnd)
	old := wrapMarks(planSegmentBody(inlines[start:delEnd]), del)
	nw := wrapMarks(planSegmentBody(inlines[delEnd:insEnd]), ins)
	// The substitution's own markers must not occur in either half, or
	// the reader would split or close the span in the wrong place. When
	// they do, decline: the caller falls back to two separate spans,
	// each of which picks a form that is safe for its own text
	// (wrapSuggestion).
	if unsafeForSubstitution(planString(old)) || unsafeForSubstitution(planString(nw)) {
		return nil, 0, false
	}
	body := literalChars("{~~")
	body = append(body, old...)
	body = append(body, literalChars(subSep)...)
	body = append(body, nw...)
	body = append(body, literalChars("~~}")...)
	return body, insEnd, true
}

func unsafeForSubstitution(text string) bool {
	return strings.Contains(text, "~~}") || strings.Contains(text, subSep)
}

// onlySuggestion reports whether in carries kind and no other suggestion
// mark.
func onlySuggestion(in docmodel.Inline, kind docmodel.MarkKind) bool {
	if !in.Has(kind) {
		return false
	}
	for _, k := range suggestionKinds {
		if k != kind && in.Has(k) {
			return false
		}
	}
	return true
}

// wrapCritic wraps an already-planned body in in's CriticMarkup markers,
// innermost to outermost in reverse suggestionKinds order, as literal,
// non-escapable runes. Planning them as delimiters rather than content is
// load-bearing: as content they would be run through needsEscapeAt with
// the rest of the block and could come out backslashed.
//
// The suggestion's Attrs (author, at) are NOT written: CriticMarkup has
// nowhere to carry them. They live in the sidecar; the file carries the
// change itself.
//
// The one attr that IS written is a highlight's comment ID, as its ID mark
// "{>>@comment id<<}" flush after the closing "==}" — the spelling
// critic.go's stampCommentID binds back to the highlight. Every segment of
// a highlight gets its own, so a comment crossing emphasis or a code span
// writes one mark per piece. It is written only when the highlight's own
// markers were: a highlight with no spelling (its text holds "==}") has no
// "==}" to follow, and the comment then reads as unplaced.
func wrapCritic(body []pchar, in docmodel.Inline) ([]pchar, bool) {
	emitted, highlighted := false, false
	for i := len(suggestionKinds) - 1; i >= 0; i-- {
		kind := suggestionKinds[i]
		if !in.Has(kind) {
			continue
		}
		wrapped, ok := wrapSuggestion(body, kind)
		body, emitted = wrapped, emitted || ok
		highlighted = highlighted || (ok && kind == docmodel.Highlight)
	}
	if id := in.Attr(docmodel.Highlight, docmodel.CommentIDAttr); highlighted && validCommentID(id) {
		body = append(body, literalChars("{>>"+commentMark(id)+"<<}")...)
	}
	return body, emitted
}

// wrapSuggestion writes one suggestion mark around an already-planned
// body, choosing a form whose closing marker does not already occur
// inside that body.
//
// CriticMarkup has no escape mechanism, and the reader closes a span at
// the FIRST matching closer, so a deletion whose text happens to contain
// "--}" cannot be written as "{--…--}": the reader would stop at the
// text's own "--}" and the rest of the text would fall out of the span.
// That is content corruption, which this package does not do.
//
// A deletion has a second spelling that closes on a different marker —
// the substitution form with an empty insertion, "{~~old~>~~}" — and an
// insertion likewise, "{~~~>new~~}". When the primary form is unsafe and
// that one is safe, it is used instead; both read back as exactly the
// original mark (the empty half is dropped as an empty run).
//
// When neither form is safe — text containing both "--}" and "~~}", or a
// highlight containing "==}", which has no alternate spelling — the
// markers are omitted and the text is written plainly. The suggestion
// mark is lost; the text is not. Losing a mark is recoverable from the
// sidecar, losing the author's words is not.
//
// The complete fix is to make CriticMarkup escapable, which means moving
// backslash-unescaping to AFTER the marker scan so an escaped marker
// character can be scanned as literal — a change to the escape layer, not
// to this one.
// It reports whether it wrote markers. A false answer is load-bearing,
// not merely informational: a segment that writes nothing does not
// separate itself from its neighbour, so planInlines must coalesce it
// rather than leave two fences (or two "**" runs) flush.
func wrapSuggestion(body []pchar, kind docmodel.MarkKind) ([]pchar, bool) {
	text := planString(body)
	if s := criticSpanFor(kind); !strings.Contains(text, s.close) {
		return wrap(body, s.open, s.close), true
	}
	if !strings.Contains(text, "~~}") && !strings.Contains(text, subSep) {
		switch kind {
		case docmodel.Del:
			return wrap(body, "{~~", subSep+"~~}"), true
		case docmodel.Ins:
			return wrap(body, "{~~"+subSep, "~~}"), true
		}
	}
	return body, false
}

// planString is the text a planned run will contribute to the output,
// before escaping. Escaping only ever inserts backslashes, and never
// before a CriticMarkup marker character, so a marker sequence that is
// absent here is absent from the final output too.
func planString(body []pchar) string {
	var b strings.Builder
	for _, p := range body {
		b.WriteRune(p.r)
	}
	return b.String()
}

func criticSpanFor(kind docmodel.MarkKind) criticSpan {
	for _, s := range criticSpans {
		if s.kind == kind {
			return s
		}
	}
	// Unreachable: wrapCritic only asks for the three kinds in
	// suggestionKinds, each of which has an entry in criticSpans.
	return criticSpan{}
}

// wrapMarks wraps an already-planned body in in's emphasis and link
// delimiters, innermost to outermost per the plan's fixed nesting order
// (link, bold, italic), as literal, non-escapable runes.
func wrapMarks(body []pchar, in docmodel.Inline) []pchar {
	if in.Has(docmodel.Italic) {
		body = wrap(body, "*", "*")
	}
	if in.Has(docmodel.Bold) {
		body = wrap(body, "**", "**")
	}
	if in.Has(docmodel.Link) {
		body = wrap(markLabel(body), "[", "]("+renderDestination(in.Attr(docmodel.Link, "href"))+")")
	}
	return body
}

// markLabel flags an already-planned body as sitting inside a link's or
// image's square brackets, so renderPlan escapes the "[" and "]" that would
// otherwise close the label early. It is a flag on the planned runes rather
// than a mode on the whole plan because only PART of a block is a label:
// the escaping pass sees the whole block at once.
func markLabel(body []pchar) []pchar {
	out := make([]pchar, len(body))
	for i, p := range body {
		p.label = true
		out[i] = p
	}
	return out
}

// renderImage plans a whole "![alt](src)" at once.
//
// Planning the alt text ALONE was a bug FuzzRoundTrip found: escaping is
// decided over a plan, and a plan holding only the label cannot see the "]"
// that comes after it. Alt text ending in a backslash therefore escaped
// that "]" instead of itself — "![\](x)" — and the label never closed, so
// the image ran on into whatever followed and Parse rejected the result.
//
// It is the same reason renderInlines plans a whole block rather than one
// Inline (fix round 2): a character's escaping depends on its real
// neighbours in the output, and a delimiter this renderer emits is as real
// a neighbour as any.
func renderImage(alt, src string) string {
	plan := literalChars("![")
	plan = append(plan, markLabel(contentChars(alt))...)
	plan = append(plan, literalChars("]("+renderDestination(src)+")")...)
	return renderPlan(plan, lineContext{atLineStart: true})
}

// renderDestination writes a link's or image's destination.
//
// CommonMark gives destinations two spellings, and the bare one is a trap:
// it stops at the first ASCII space, and its parentheses have to balance —
// so a destination holding a space, or "(", was written straight into
// "](...)" and the link simply stopped being a link. The angle form
// "<...>" has none of those rules; it only needs its own "<", ">" and
// backslashes escaped.
//
// So the bare form is used exactly where it is unambiguous, which is every
// ordinary URL, and anything else takes the angle form. That keeps the
// common case reading like markdown people write by hand while making the
// awkward case correct rather than broken.
//
// A NEWLINE has no spelling in either form — the angle form forbids line
// endings as firmly as the bare form does — so "[x](<a\nb>)" is not a link
// at all and the "<" that opens it is raw HTML, which Parse refuses: a file
// that will not load. It is percent-encoded instead, which is what a URL
// does with a newline anyway. That is a NORMALIZATION and not a loss: the
// mark's destination comes back as "a%0Ab", which still says exactly the
// same thing, and the second write is a fixed point.
//
// Only the newline. Every other awkward character was measured against
// goldmark and survives the angle form intact — a space, a tab, a NUL, and
// (goldmark's own reading of CommonMark) a lone carriage return. Encoding
// those too would be a diff in somebody's file for nothing.
func renderDestination(dest string) string {
	if dest == "" {
		// "[a]()" is a valid link with an empty destination; "[a](<>)"
		// means the same thing and reads worse.
		return ""
	}
	dest = strings.ReplaceAll(dest, "\n", "%0A")
	if isBareDestination(dest) {
		return dest
	}
	var b strings.Builder
	b.WriteByte('<')
	for _, r := range dest {
		if r == '<' || r == '>' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('>')
	return b.String()
}

// isBareDestination reports whether dest can be written without the angle
// brackets: no ASCII space or control character (which would end it), no
// parenthesis (which would have to balance), and no "<", ">" or backslash
// (which the reader would take as markup or an escape).
func isBareDestination(dest string) bool {
	for _, r := range dest {
		switch {
		case r <= ' ', r == '(', r == ')', r == '<', r == '>', r == '\\':
			return false
		}
	}
	return true
}

func wrap(body []pchar, open, close string) []pchar {
	out := make([]pchar, 0, len(open)+len(body)+len(close))
	out = append(out, literalChars(open)...)
	out = append(out, body...)
	out = append(out, literalChars(close)...)
	return out
}

func literalChars(s string) []pchar {
	out := make([]pchar, 0, len(s))
	for _, r := range s {
		out = append(out, pchar{r: r})
	}
	return out
}

func contentChars(s string) []pchar {
	out := make([]pchar, 0, len(s))
	for _, r := range s {
		out = append(out, pchar{r: r, escapable: true})
	}
	return out
}

// renderPlan resolves escaping over the full planned sequence and emits
// it. plan spans an entire block, so needsEscapeAt (escape.go) sees every
// real neighbor a character will have in the final output, including ones
// contributed by a different Inline or by another mark's own delimiters.
//
// It also tracks where each output LINE begins, which is what lets
// needsEscapeAt tell text from block structure. A block can span several
// lines: a hard break plans "\\\n", and everything after it leads a line
// of its own that a bare "-" or "#" would turn into a list or a heading.
// ctx.atLineStart says whether the plan's own first rune leads a line —
// false for a heading, whose "# " prefix has already been written.
//
// Only escapABLE runes are considered, so a line beginning inside a code
// span (whose content markdown gives no way to escape) is left alone; that
// is the known newline-inside-a-code-span class — CommonMark converts such
// a newline to a space, and the padding rule and that conversion disagree —
// untouched here.
func renderPlan(plan []pchar, ctx lineContext) string {
	r := make([]rune, len(plan))
	at := escapeCtx{
		heading:   ctx.heading,
		escapable: make([]bool, len(plan)),
		labels:    make([]bool, len(plan)),
	}
	for i, p := range plan {
		r[i] = p.r
		at.escapable[i] = p.escapable
		at.labels[i] = p.label
	}
	lineStart := -1
	if ctx.atLineStart {
		lineStart = 0
	}
	var b strings.Builder
	for i, p := range plan {
		if i > 0 && r[i-1] == '\n' {
			lineStart = i
		}
		at.lineStart = lineStart
		if p.escapable && needsEscapeAt(r, i, at) {
			b.WriteByte('\\')
		}
		b.WriteRune(p.r)
	}
	return b.String()
}

// CodeSpan wraps text in a CommonMark backtick code-span fence, choosing a
// fence length and optional padding so the text is preserved exactly through a
// parse round-trip. It is the exported face of renderCode, provided so that
// callers outside this package (e.g. internal/htmlpage) can produce correct
// code spans without duplicating the CommonMark rule.
func CodeSpan(text string) string { return renderCode(text) }

// LinkDestination writes a link's destination for a CommonMark "](...)"
// construct, choosing the bare form for ordinary URLs and the angle-bracket
// form "<...>" for any destination that would otherwise break — one holding a
// space, a parenthesis, or a control character. It is the exported face of
// renderDestination, provided so that callers outside this package (e.g.
// internal/htmlpage) can emit correct destinations without duplicating the
// CommonMark rule.
func LinkDestination(dest string) string { return renderDestination(dest) }

// renderCode wraps text in a backtick code-span fence. Per CommonMark, the
// fence must use more backticks than the longest run already inside the
// content (or a single embedded backtick, e.g. "a`b", would prematurely
// close a single-backtick fence). If the content starts or ends with a
// backtick — or with a space, which CommonMark's code-span reader would
// otherwise strip as the fence's own padding — a single space is added on
// that side so the content survives the round trip unchanged; goldmark's
// code-span parser strips exactly one such padding space per side before
// Parse ever sees the text (see parser.codeSpanParser.Parse), so the extra
// space added here is not itself part of the content.
//
// This fence is chosen from text alone, so it is only correct when text
// already reflects everything that will end up inside this fence — see
// planSegmentBody, the sole caller, which concatenates a whole contiguous
// code run before calling this so no adjacent code span is left to pick a
// fence width that would fuse with this one.
func renderCode(text string) string {
	fence := strings.Repeat("`", longestBacktickRun(text)+1)
	body := text
	if needsCodePadding(body) {
		body = " " + body + " "
	}
	return fence + body + fence
}

func longestBacktickRun(s string) int {
	longest, cur := 0, 0
	for _, r := range s {
		if r == '`' {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
	}
	return longest
}

// needsCodePadding reports whether renderCode must add a space at each end
// so the content survives the read back. It mirrors goldmark's own stripping
// rule EXACTLY, and deliberately asks goldmark rather than restating the
// rule: this package's guarantee is that what it writes, goldmark reads back
// unchanged, so the parser's predicate is the specification here and a
// hand-copied one is a bug waiting for the next goldmark release.
//
// The rule (parser/code_span.go): one character is stripped from each end
// when the content is not entirely blank AND BOTH ends are a space or a
// newline. FuzzRoundTrip found this wrong in both halves. It was an OR over
// the ends, so " a" was padded when nothing would have stripped it — noise
// rather than damage. And "not entirely blank" was strings.TrimSpace, which
// counts a vertical tab and U+00A0 as blank where goldmark's util.IsBlank
// does not — so "  \v  " read as blank, was left unpadded, and lost two
// characters per save. (The two agree about a carriage return: both call it
// blank. An earlier version of this comment claimed otherwise and named a
// second failure that does not exist — checked against goldmark v1.8.5.)
func needsCodePadding(s string) bool {
	if s == "" {
		return false
	}
	first, last := s[0], s[len(s)-1]
	if first == '`' || last == '`' {
		return true
	}
	return isCodeEdge(first) && isCodeEdge(last) && !util.IsBlank([]byte(s))
}

// isCodeEdge is goldmark's isSpaceOrNewline, which is unexported.
func isCodeEdge(c byte) bool {
	return c == ' ' || c == '\n'
}
