package markdown

import (
	"strings"
	"unicode"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// noteline.go implements the half of note.go's grammar the reader was missing.
//
// note.go states the discriminator: a {>>…<<} is BLOCK-level exactly when it is
// "the whole content of its LINE" — that is what makes "text. {>>why?<<}" a
// range comment and "{>>why?<<}" on its own line a comment on the block above.
// extractCritic implemented a narrower rule, "the whole content of its
// PARAGRAPH", and every near-miss fell through to the out-of-band lifting path,
// which REMOVES the note from the file. Two of the shapes it missed are things
// a hand-editor types without thinking:
//
//	"para\n{>>note<<}\n"       -> "para\n"   the note is gone
//	"{>>one<<}\n{>>two<<}\n"   -> "\n"       both are gone, the paragraph too
//
// Whether a reviewer's words survived depended on whether they pressed Enter
// twice. So this is not "recognise one more shape" — the next near-miss would
// delete too. The rule is the one the design already stated, applied to the
// unit it was always about:
//
//	A SOURCE LINE inside a paragraph whose content is nothing but notes
//	becomes its own block, one Note per note on it.
//
// Everything else is untouched. A line that mixes prose and notes keeps its
// notes as range comments at their offsets, which is correct and is what
// TestNote_InlineNoteAtEndOfParagraphIsNotADocumentComment pins.
//
// It runs in the CONVERTER rather than in critic.go because a line boundary is
// something only the converter can still see. A soft line break becomes a
// space in the model, deliberately — a paragraph reflows — and by the time
// critic.go runs, "para\n{>>note<<}" and "para {>>note<<}" are the same
// document. They must not become the same block.

// splitNoteLines rewrites one converted block into the blocks its source lines
// describe: a paragraph holding note-only lines becomes those notes as
// paragraphs of their own, with the prose around them rejoined.
//
// softBreaks are rune offsets into b's concatenated text where the source had
// a soft line break; hard breaks are already in the model as a HardBreak
// inline. Both are line boundaries here.
//
// Returns b unchanged — the same slice header, no copy — whenever there is
// nothing to split, which is every block in a document that has no note-only
// line folded into a paragraph.
func splitNoteLines(b docmodel.Block, softBreaks []int) []docmodel.Block {
	if b.Kind != docmodel.Paragraph || len(b.Inlines) == 0 {
		return []docmodel.Block{b}
	}
	lines := splitParagraphLines(b.Inlines, softBreaks)
	if len(lines) == 0 {
		return []docmodel.Block{b}
	}
	// One line is the case extractCritic already handles: if that whole line
	// is notes it is the whole paragraph, and standaloneNote sees it. The
	// exception is a line carrying SEVERAL notes, which needs one block each.
	anyNote := false
	for _, ln := range lines {
		if noteCount(ln.inlines) > 0 {
			anyNote = true
			break
		}
	}
	if !anyNote {
		return []docmodel.Block{b}
	}

	var out []docmodel.Block
	var pending []docmodel.Inline
	flush := func() {
		if len(pending) > 0 {
			out = append(out, docmodel.Block{Kind: docmodel.Paragraph, Inlines: mergeAdjacent(pending)})
			pending = nil
		}
	}
	for _, ln := range lines {
		n := noteCount(ln.inlines)
		if n == 0 {
			// Rejoin with the separator the source had: a soft break is the
			// space the converter already turned it into (it is inside the
			// preceding run), a hard break is its own inline.
			if len(pending) > 0 && ln.hardBreakBefore {
				pending = append(pending, docmodel.Inline{
					Marks: []docmodel.Mark{{Kind: docmodel.HardBreak}},
				})
			}
			pending = append(pending, ln.inlines...)
			continue
		}
		flush()
		// One paragraph per note on the line, so extractCritic's existing
		// single-note rule turns each into its own Note block. Two notes in
		// one paragraph have one line between them and two blocks need two.
		for _, text := range splitNoteRuns(plainInlineText(ln.inlines)) {
			out = append(out, docmodel.Block{
				Kind:    docmodel.Paragraph,
				Inlines: []docmodel.Inline{{Text: text}},
			})
		}
	}
	flush()
	if len(out) == 0 {
		return []docmodel.Block{b}
	}
	return out
}

// paragraphLine is one source line of a paragraph.
type paragraphLine struct {
	inlines []docmodel.Inline
	// hardBreakBefore records that the boundary opening this line was a hard
	// break rather than a soft one, so rejoined prose keeps it.
	hardBreakBefore bool
}

// splitParagraphLines cuts a paragraph's inlines at every line boundary: the
// recorded soft-break offsets, and every HardBreak inline.
func splitParagraphLines(inlines []docmodel.Inline, softBreaks []int) []paragraphLine {
	var lines []paragraphLine
	var cur []docmodel.Inline
	hard := false
	cut := func(nextHard bool) {
		lines = append(lines, paragraphLine{inlines: cur, hardBreakBefore: hard})
		cur, hard = nil, nextHard
	}

	soft := append([]int(nil), softBreaks...)
	off := 0
	for _, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			cut(true)
			continue
		}
		r := []rune(in.Text)
		// A run can span several soft breaks; walk them in order and cut the
		// run where each falls.
		start := 0
		for len(soft) > 0 && soft[0] > off && soft[0] <= off+len(r) {
			at := soft[0] - off
			soft = soft[1:]
			cur = append(cur, docmodel.Inline{Text: string(r[start:at]), Marks: in.Marks})
			cut(false)
			start = at
		}
		for len(soft) > 0 && soft[0] <= off {
			soft = soft[1:]
		}
		if start < len(r) {
			cur = append(cur, docmodel.Inline{Text: string(r[start:]), Marks: in.Marks})
		}
		off += len(r)
	}
	cut(false)
	return lines
}

// noteCount reports how many notes a line holds if the line is NOTHING BUT
// notes, and 0 otherwise.
//
// "Nothing but" is strict on purpose. A line that mixes prose and a note keeps
// today's behaviour — the note stays a range comment at its offset — and a
// line carrying any MARK is not a note line at all: a code span is literal by
// definition, and marked marker-shaped text is a suggestion, not a comment.
func noteCount(inlines []docmodel.Inline) int {
	for _, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			return 0
		}
		if len(in.Marks) > 0 {
			return 0
		}
	}
	return len(splitNoteRuns(plainInlineText(inlines)))
}

// splitNoteRuns returns each "{>>…<<}" in s, in order, if s is nothing but
// notes and whitespace. Otherwise it returns nil.
//
// Deliberately a narrow scanner rather than a call into critic.go's: this only
// has to recognise the one shape it acts on, and anything it does not
// recognise falls through to the behaviour that already exists. A line holding
// "{=={>>a<<}==}" is not a run of bare notes, so it is left entirely alone.
func splitNoteRuns(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}
		if !strings.HasPrefix(s[i:], noteOpen) {
			return nil
		}
		end := strings.Index(s[i+len(noteOpen):], noteClose)
		if end < 0 {
			return nil
		}
		stop := i + len(noteOpen) + end + len(noteClose)
		out = append(out, s[i:stop])
		i = stop
	}
	return out
}

const (
	noteOpen  = "{>>"
	noteClose = "<<}"
)

// plainInlineText concatenates a line's text, which is all splitNoteRuns needs
// — a note line carries no marks by definition.
func plainInlineText(inlines []docmodel.Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		b.WriteString(in.Text)
	}
	return b.String()
}

// runeLen is the total rune length of a run of inlines — the coordinate
// p.softBreaks is recorded in.
func runeLen(inlines []docmodel.Inline) int {
	n := 0
	for _, in := range inlines {
		n += len([]rune(in.Text))
	}
	return n
}
