package htmlpage

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/suggest"
	"golang.org/x/net/html"
)

// Render pours markdown back into the template and returns the whole page.
// It is the inverse of Extract: shell segments ship verbatim, content regions
// pour into their slots, and the result re-extracts to the same split (the
// idempotence gate). Markdown a page cannot carry refuses via a "cannot be
// poured" error the server routes through cannot.
func Render(t Template, md []byte) ([]byte, error) {
	doc, _, err := markdown.Parse(md)
	if err != nil {
		return nil, err
	}

	// Instructions are not page content. A reviewer's highlight or a {>>…<<}
	// note block is a working-copy affordance that lives in content.md and its
	// review thread — suggest.ClearInstructions is the same lift that keeps them
	// out of a committed version. A poured page is a published artifact, so it
	// carries neither: drop them here, BEFORE region/wrapper assignment, so a
	// note never reaches renderBlock's refusal and never shifts the wrapper
	// index of the real content blocks around it. Without this a whole-document
	// or block-level instruction failed the whole round ("a note block cannot be
	// poured back into this page"). Genuinely unpourable structure (a nested
	// list) still refuses in renderBlock — that is content the page cannot carry,
	// not an instruction to be dropped.
	doc = suggest.ClearInstructions(doc)

	assigned := assignRegions(splitRegions(doc.Blocks), slotOffsets(t))

	// Render into a piece per segment — shell strings and slot outputs kept
	// apart — so pruneEmptied can peel the container that bracketed a slot the
	// reviewer emptied without ever touching verbatim shell. Joined below.
	pieces := make([]piece, 0, len(t.Segments))
	slot := 0
	for _, seg := range t.Segments {
		if seg.Slot == nil {
			pieces = append(pieces, piece{text: seg.Shell})
			continue
		}
		var sb strings.Builder
		if err := renderSlot(&sb, seg.Slot, assigned[slot]); err != nil {
			return nil, err
		}
		pieces = append(pieces, piece{text: sb.String(), isSlot: true})
		slot++
	}
	pruneEmptied(pieces)

	var b strings.Builder
	b.WriteString(t.Head)
	for _, p := range pieces {
		b.WriteString(p.text)
	}
	b.WriteString(t.Tail)
	return []byte(b.String()), nil
}

// piece is one rendered segment: a verbatim shell run, or a slot's poured
// output. Kept separate only long enough for pruneEmptied to run.
type piece struct {
	text   string
	isSlot bool
}

func renderSlot(b *strings.Builder, slot *Slot, blocks []docmodel.Block) error {
	// Each block renders in the wrapper it CAME FROM, matched by identity, so an
	// insert/delete/reorder in this slot can no longer slide a class onto the
	// wrong block (align.go). A new block takes an inherited default or a bare
	// tag; a deleted block's wrapper simply goes unused.
	wrappers := alignBlocks(blocks, slot.Wrappers)
	for i, blk := range blocks {
		// A heading or paragraph the reviewer emptied contributes no prose;
		// emitting <h3></h3> leaves a dangling heading, and if it was a
		// container's only content the empty container survives too. Skipped so
		// the block vanishes and pruneEmptied can collapse the container around
		// it.
		if blockRendersEmpty(blk) {
			continue
		}
		out, err := renderBlock(blk, wrappers[i])
		if err != nil {
			return err
		}
		b.WriteString(out)
	}
	return nil
}

// blockRendersEmpty reports whether a paragraph or heading would render with no
// visible content — the shape a reviewer's deletion leaves behind. Other kinds
// are left alone: an empty list or table is not the deletion idiom and pruning
// one blindly would be a second guess.
func blockRendersEmpty(b docmodel.Block) bool {
	switch b.Kind {
	case docmodel.Paragraph, docmodel.Heading:
		return strings.TrimSpace(renderInlines(b.Inlines)) == ""
	default:
		return false
	}
}

// pruneEmptied removes a structural container left empty because the slot it
// bracketed rendered to nothing — the empty grid cell (<div class="tg"></div>)
// a reviewer's deletion leaves behind. It works on the piece list, never the
// assembled page, so it can only ever delete a tag that BRACKETS a slot: a
// container's open tag is the tail of the shell before a slot and its close is
// the head of the shell after it. Decorative page furniture that was always
// empty (<span class="lamp"></span>) lives wholly inside one shell string, never
// straddling a slot, so it is structurally out of reach here and always kept.
//
// Unchanged content never triggers it: a non-empty slot is skipped, so a pour
// of the original markdown peels nothing and renders byte-identical (the
// idempotence gate). A container carrying an id is left standing even when
// empty — it is a landmark or an anchor target, and losing it would 404 a link
// the page keeps elsewhere; the classed layout cell that has no id is the thing
// this prunes. Only the container IMMEDIATELY bracketing the slot is peeled: a
// grid cell, not the section three levels up that merely happens to be empty
// once every cell inside it is gone.
func pruneEmptied(pieces []piece) {
	for i := range pieces {
		if !pieces[i].isSlot || strings.TrimSpace(pieces[i].text) != "" {
			continue
		}
		if i-1 < 0 || i+1 >= len(pieces) {
			continue
		}
		for {
			name, cut, ok := trailingOpenTag(pieces[i-1].text)
			if !ok {
				break
			}
			off, ok := leadingCloseTag(pieces[i+1].text, name)
			if !ok {
				break
			}
			pieces[i-1].text = pieces[i-1].text[:cut]
			pieces[i+1].text = pieces[i+1].text[off:]
		}
	}
}

// trailingOpenTag finds the last element open tag at the end of a shell string,
// after trailing whitespace. It returns the tag's lowercase name and the byte
// offset where the tag begins (so s[:cut] drops it). A tag carrying an id is
// reported as no match: an id'd container is a landmark pruneEmptied keeps. A
// close tag, comment, doctype or self-closing/void tag is likewise no match —
// none of them opens a container a slot could have filled.
func trailingOpenTag(s string) (name string, cut int, ok bool) {
	trimmed := strings.TrimRight(s, " \t\r\n")
	if !strings.HasSuffix(trimmed, ">") {
		return "", 0, false
	}
	// Walk back to the '<' that opens this final tag, honoring quoted attribute
	// values so a '>' inside an attribute cannot be mistaken for the tag end.
	inQuote := false
	start := -1
	for j := len(trimmed) - 2; j >= 0; j-- {
		c := trimmed[j]
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == '<' && !inQuote {
			start = j
			break
		}
	}
	if start < 0 {
		return "", 0, false
	}
	tag := trimmed[start:]
	if strings.HasPrefix(tag, "</") || strings.HasPrefix(tag, "<!") || strings.HasSuffix(tag, "/>") {
		return "", 0, false
	}
	if strings.Contains(tag, " id=\"") {
		return "", 0, false
	}
	j := 1
	for j < len(tag) {
		c := tag[j]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '/' {
			break
		}
		j++
	}
	name = strings.ToLower(tag[1:j])
	if name == "" {
		return "", 0, false
	}
	return name, start, true
}

// leadingCloseTag reports whether a shell string opens (after leading
// whitespace) with the close tag for name, and the offset just past it, so
// s[off:] drops it.
func leadingCloseTag(s, name string) (off int, ok bool) {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	lead := len(s) - len(trimmed)
	close := "</" + name + ">"
	if !strings.HasPrefix(strings.ToLower(trimmed), close) {
		return 0, false
	}
	return lead + len(close), true
}

// region is a run of content blocks and the shell that precedes it — the
// number of the nearest stand-in above it, 0 for leading content.
type region struct {
	precedingShell int
	blocks         []docmodel.Block
}

// splitRegions cuts the document into content runs at every stand-in block,
// dropping the stand-ins themselves (they are visual, not structural).
func splitRegions(blocks []docmodel.Block) []region {
	regions := []region{{}}
	for _, blk := range blocks {
		if n, ok := standInNumber(blk); ok {
			regions = append(regions, region{precedingShell: n})
			continue
		}
		last := &regions[len(regions)-1]
		last.blocks = append(last.blocks, blk)
	}
	return regions
}

// slotOffsets returns, per slot in template order, how many shell segments
// precede it — the shell number a stand-in must carry to pour into it.
func slotOffsets(t Template) []int {
	var offsets []int
	shells := 0
	for _, seg := range t.Segments {
		if seg.Slot == nil {
			shells++
			continue
		}
		offsets = append(offsets, shells)
	}
	return offsets
}

// assignRegions pours each region into its slot by order with merge-forward:
// a region lands in the slot whose shell offset is the largest not exceeding
// its preceding shell, so a deleted stand-in's run stays with the previous
// slot and the orphaned slot renders empty (spec: "stand-ins are visual, not
// structural"). Leading content belongs to the first slot.
func assignRegions(regions []region, offsets []int) [][]docmodel.Block {
	out := make([][]docmodel.Block, len(offsets))
	for _, r := range regions {
		if idx := targetSlot(r.precedingShell, offsets); idx >= 0 {
			out[idx] = append(out[idx], r.blocks...)
		}
	}
	return out
}

func targetSlot(preceding int, offsets []int) int {
	best := -1
	for i, off := range offsets {
		if off <= preceding && (best == -1 || off > offsets[best]) {
			best = i
		}
	}
	if best == -1 && len(offsets) > 0 {
		best = 0
	}
	return best
}

var (
	shotStandIn   = regexp.MustCompile(`^shots/shell-\d{3}\.png$`)
	markerStandIn = regexp.MustCompile(`^⟦ shell (\d+) ⟧$`)
	shotNumber    = regexp.MustCompile(`^shots/shell-0*(\d+)\.png$`)
)

// standInNumber reports whether a block is a shell stand-in and returns the
// 1-based shell number it carries. The two forms must match Extract and Task 5
// exactly: an image at shots/shell-NNN.png, or a marker paragraph "⟦ shell N ⟧".
func standInNumber(b docmodel.Block) (int, bool) {
	switch b.Kind {
	case docmodel.Image:
		src := b.Attrs["src"]
		if !shotStandIn.MatchString(src) {
			return 0, false
		}
		m := shotNumber.FindStringSubmatch(src)
		return atoi(m[1]), true
	case docmodel.Paragraph:
		m := markerStandIn.FindStringSubmatch(blockText(b))
		if m == nil {
			return 0, false
		}
		return atoi(m[1]), true
	default:
		return 0, false
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func blockText(b docmodel.Block) string {
	var s strings.Builder
	for _, in := range b.Inlines {
		s.WriteString(in.Text)
	}
	return s.String()
}

// renderBlock renders one content block, wrapping it in the slot's original
// element when the wrapper's kind still matches, or a bare tag otherwise.
func renderBlock(b docmodel.Block, w *Wrapper) (string, error) {
	switch b.Kind {
	case docmodel.Paragraph:
		return wrapBlock(w, b.Kind, "p", renderInlines(b.Inlines)), nil
	case docmodel.Heading:
		return wrapBlock(w, b.Kind, "h"+headingLevelAttr(b), renderInlines(b.Inlines)), nil
	case docmodel.BulletList:
		inner, err := renderListItems(b)
		if err != nil {
			return "", err
		}
		return wrapBlock(w, b.Kind, "ul", inner), nil
	case docmodel.OrderedList:
		inner, err := renderListItems(b)
		if err != nil {
			return "", err
		}
		return wrapBlock(w, b.Kind, "ol", inner), nil
	case docmodel.Blockquote:
		return wrapBlock(w, b.Kind, "blockquote", renderQuote(b)), nil
	case docmodel.Table:
		inner, err := renderTable(b)
		if err != nil {
			return "", err
		}
		return wrapBlock(w, b.Kind, "table", inner), nil
	case docmodel.CodeBlock:
		// A fenced code block pours back into a <pre>: its verbatim text,
		// HTML-escaped, with no inline marks (code is literal text).
		return wrapBlock(w, b.Kind, "pre", html.EscapeString(b.Text)), nil
	default:
		return "", cannotPour(b.Kind)
	}
}

func cannotPour(kind docmodel.BlockKind) error {
	return fmt.Errorf(
		"htmlpage: a %s block cannot be poured back into this page — pages carry prose, not %s",
		kind, kind)
}

// wrapBlock wraps inner in the block's element: the original wrapper when its
// kind still matches, otherwise a bare tag for the kind.
func wrapBlock(w *Wrapper, kind docmodel.BlockKind, bareTag, inner string) string {
	tag, attrs := bareTag, ""
	if w != nil && w.Kind == string(kind) {
		tag, attrs = w.Tag, w.Attrs
	}
	return "<" + tag + attrs + ">" + inner + "</" + tag + ">"
}

func headingLevelAttr(b docmodel.Block) string {
	if lvl := b.Attrs["level"]; lvl != "" {
		return lvl
	}
	return "1"
}

func renderListItems(b docmodel.Block) (string, error) {
	var s strings.Builder
	for _, item := range b.Children {
		inner, err := childInlines(item)
		if err != nil {
			return "", err
		}
		s.WriteString("<li>")
		s.WriteString(inner)
		s.WriteString("</li>")
	}
	return s.String(), nil
}

func renderQuote(b docmodel.Block) string {
	var s strings.Builder
	for _, c := range b.Children {
		s.WriteString("<p>")
		s.WriteString(renderInlines(c.Inlines))
		s.WriteString("</p>")
	}
	return s.String()
}

func renderTable(b docmodel.Block) (string, error) {
	var s strings.Builder
	for _, row := range b.Children {
		s.WriteString("<tr>")
		for _, cell := range row.Children {
			cellTag := "td"
			if cell.Kind == docmodel.TableHeader {
				cellTag = "th"
			}
			inner, err := childInlines(cell)
			if err != nil {
				return "", err
			}
			s.WriteString("<" + cellTag + ">")
			s.WriteString(inner)
			s.WriteString("</" + cellTag + ">")
		}
		s.WriteString("</tr>")
	}
	return s.String(), nil
}

// childInlines renders the inline content of a container's block children
// (a list item's or table cell's paragraphs) as a single inline run.
// It returns a refusal error if any child carries its own Children (e.g. a
// nested list inside a list item), because pages carry prose, not structure,
// and silently dropping nested content violates the spec's contract.
func childInlines(b docmodel.Block) (string, error) {
	var s strings.Builder
	for _, c := range b.Children {
		if len(c.Children) > 0 {
			return "", cannotPour(c.Kind)
		}
		s.WriteString(renderInlines(c.Inlines))
	}
	return s.String(), nil
}

// inlineMarkOrder is the outer-to-inner nesting order for inline marks, the
// inverse of convert's inlineNode: a link wraps emphasis wraps strong wraps
// code.
var inlineMarkOrder = []docmodel.MarkKind{
	docmodel.Link, docmodel.Italic, docmodel.Bold, docmodel.Code,
}

// renderInlines turns a block's flat Inline sequence into nested HTML, the
// inverse of Task 3's inline markdown: Link→<a href>, Italic→<em>, Bold→
// <strong>, Code→<code>, with html.EscapeString on text.
func renderInlines(inlines []docmodel.Inline) string {
	return renderInlineRun(inlines, nil)
}

func renderInlineRun(inlines []docmodel.Inline, applied []docmodel.Mark) string {
	var b strings.Builder
	for i := 0; i < len(inlines); {
		mark, ok := nextMark(inlines[i], applied)
		if !ok {
			b.WriteString(html.EscapeString(inlines[i].Text))
			i++
			continue
		}
		j := i + 1
		for j < len(inlines) && sharesMark(inlines[j], mark) {
			j++
		}
		open, closeTag := inlineTag(mark)
		b.WriteString(open)
		b.WriteString(renderInlineRun(inlines[i:j], append(applied, mark)))
		b.WriteString(closeTag)
		i = j
	}
	return b.String()
}

// nextMark returns the outermost renderable mark on in not already opened by
// an enclosing run.
func nextMark(in docmodel.Inline, applied []docmodel.Mark) (docmodel.Mark, bool) {
	for _, kind := range inlineMarkOrder {
		for _, m := range in.Marks {
			if m.Kind == kind && !appliedHas(applied, m) {
				return m, true
			}
		}
	}
	return docmodel.Mark{}, false
}

func sharesMark(in docmodel.Inline, mark docmodel.Mark) bool {
	for _, m := range in.Marks {
		if markKey(m) == markKey(mark) {
			return true
		}
	}
	return false
}

func appliedHas(applied []docmodel.Mark, mark docmodel.Mark) bool {
	for _, m := range applied {
		if markKey(m) == markKey(mark) {
			return true
		}
	}
	return false
}

// markKey identifies a mark for grouping: kind, plus a link's href so two
// adjacent links to different targets are not merged into one <a>.
func markKey(m docmodel.Mark) string {
	if m.Kind == docmodel.Link {
		return string(m.Kind) + "\x00" + m.Attrs["href"]
	}
	return string(m.Kind)
}

func inlineTag(m docmodel.Mark) (open, close string) {
	switch m.Kind {
	case docmodel.Link:
		return `<a href="` + html.EscapeString(m.Attrs["href"]) + `">`, `</a>`
	case docmodel.Italic:
		return "<em>", "</em>"
	case docmodel.Bold:
		return "<strong>", "</strong>"
	case docmodel.Code:
		return "<code>", "</code>"
	default:
		return "", ""
	}
}
