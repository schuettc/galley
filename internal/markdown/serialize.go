package markdown

import (
	"strconv"
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// Serialize renders d as canonical markdown. For every fixture under
// testdata/*.md, Serialize(Parse(f)) reproduces f byte-for-byte: ATX
// headings, "-" bullets, incrementing "1." ordered markers, backtick code
// fences, "**bold**"/"*italic*"/"***both***" emphasis, "[text](href)"
// links, "![alt](src)" images, a trailing "\" for hard breaks, a blank line
// between sibling blocks (except inside a list item, which is tight), and
// exactly one trailing newline.
// A FRONT MATTER BLOCK IS COPIED, NOT RENDERED, and it is hoisted out of the
// block loop rather than given a case in renderBlock. Two reasons, and both are
// about bytes:
//
//   - it is the author's metadata verbatim, delimiters and all (see
//     docmodel.FrontMatter), so the only correct rendering is the one that
//     writes it back unchanged; and
//   - it is the one block whose separator is not the loop's. renderBlocksLoose
//     joins with a blank line, which is right here too — but the block already
//     ends in its own newline, so it would be written with two.
func Serialize(d docmodel.Doc) []byte {
	blocks := d.Blocks
	head := ""
	if len(blocks) > 0 && blocks[0].Kind == docmodel.FrontMatter {
		head = renderFrontMatter(blocks[0])
		blocks = blocks[1:]
	}
	out := renderBlocksLoose(blocks)
	if out == "" {
		if head != "" {
			// A file that is ONLY front matter is its own bytes and nothing
			// else. No trailing blank line: there is no body for it to
			// separate from.
			return []byte(head)
		}
		return []byte("\n")
	}
	if head != "" {
		return []byte(head + "\n" + out + "\n")
	}
	return []byte(frontMatterHazard(out) + "\n")
}

// frontMatterHazard is the serializer answering for what the PARSER will make
// of its own output.
//
// A thematic break is written "---", so a document whose FIRST block is one —
// with any later line in the file that is also a bare "---" — is written as an
// opener and a closer with content between them, and the next Parse reads the
// whole span as front matter. Two blocks and everything between them, gone,
// with no keystroke. The closer need not even be another rule: a "---" inside a
// code fence is a line like any other to a scanner that walks lines.
//
// The empty-body rule in splitFrontMatter already covers the common shape (two
// leading rules, nothing between them). This covers the rest, and it asks the
// question of the BYTES rather than reasoning about which documents can reach
// it — the reasoning is what left the fence case out of the first draft.
//
// "***" is the other spelling of a thematic break, and swapping the marker
// costs three bytes in a document that was going to be misread. The leading
// newline is the backstop for anything else that could open a block (a
// hand-built paragraph spelling "+++", which Parse cannot produce): markdown
// ignores a blank first line, and front matter must start at byte 0.
func frontMatterHazard(out string) string {
	if raw, _ := splitFrontMatter([]byte(out + "\n")); raw == nil {
		return out
	}
	if strings.HasPrefix(out, yamlFence+"\n") {
		return "***" + strings.TrimPrefix(out, yamlFence)
	}
	return "\n" + out
}

// renderBlocksLoose renders blocks with a blank line between each — the
// separation used at the document root and inside a blockquote.
func renderBlocksLoose(blocks []docmodel.Block) string {
	rendered := renderedBlocks(blocks)
	texts := make([]string, len(rendered))
	for i, r := range rendered {
		texts[i] = r.text
	}
	return strings.Join(texts, "\n\n")
}

// renderedBlocks renders each block and drops the ones that render to
// nothing. A paragraph with no inlines is the case that matters: markdown
// has no way to write an empty block, so emitting one would contribute
// only its separating blank lines, which the next Parse would swallow.
// Task 4 made that reachable — a paragraph whose entire content was a
// {>>comment<<} is empty once the comment is lifted out.
//
// It also decides each list's MARKER STYLE, which is the one rendering
// decision a block cannot make on its own. Markdown separates two adjacent
// lists by their marker, not by the blank line between them: "- a" then
// "- b" is a single list however much whitespace sits between them, and it
// takes a different bullet character ("*" instead of "-") or a different
// ordered delimiter (")" instead of ".") to start a new one. Parse drops
// that distinction — both spellings read as a BulletList — so a sibling
// pair would be written flush and read back MERGED, with the second list's
// items renumbered into the first. Alternating the style whenever a list
// follows a list of the same kind is what keeps the two apart; anything
// else between them (a paragraph, a rule, a list of the other kind) already
// separates them, so the style resets.
func renderedBlocks(blocks []docmodel.Block) []renderedBlock {
	out := make([]renderedBlock, 0, len(blocks))
	prevKind, alt := docmodel.BlockKind(""), false
	for _, b := range blocks {
		// The style this block WOULD take. It is not committed until the
		// block is known to have written something: a block that renders to
		// nothing is not between its neighbours in the output either, so it
		// must not move the alternation any more than it moves prevKind.
		//
		// Flipping first and committing unconditionally was a bug — an
		// EMPTY list between two sibling lists flipped the style twice and
		// handed the second list the same marker as the first, so the two
		// merged. Exactly the failure this alternation exists to prevent.
		style := isList(b.Kind) && b.Kind == prevKind && !alt
		s, wrote := renderBlock(b, style)
		if s == "" {
			continue
		}
		out = append(out, renderedBlock{block: b, text: s})
		// wrote, not style. renderList has a LATER say than this loop does:
		// a list of empty nested items spells its markers on one line, and
		// "- - -" is a thematic break, so it re-renders with the other
		// bullet. Recording the style asked for rather than the one written
		// handed the next sibling list exactly the marker this one had just
		// used, and the two merged — the failure this alternation exists to
		// prevent, reintroduced by the guard against a different one.
		prevKind, alt = b.Kind, wrote
	}
	return out
}

// renderedBlock keeps a block's output next to the block itself, because
// how much separation two blocks need depends on what they ARE, not just on
// what they rendered to — see renderBlocksTight.
type renderedBlock struct {
	block docmodel.Block
	text  string
}

func isList(k docmodel.BlockKind) bool {
	return k == docmodel.BulletList || k == docmodel.OrderedList
}

// renderBlocksTight renders the blocks that make up a single list item with
// no separating blank line where markdown allows it. That is what makes
// tight lists tight: Task 2 established that the parser produces the same
// docmodel shape for tight and loose lists, so the serializer's choice of
// style is what fixes it.
//
// "Where markdown allows it" is the part FuzzRoundTrip found missing. A
// single newline after a paragraph line is a LAZY CONTINUATION, so a
// following block that cannot interrupt a paragraph is not a following
// block at all — it is more of the same paragraph. A second paragraph got
// glued onto the first, and an image got glued into a paragraph with other
// text, which Parse refuses outright: the file stopped loading.
//
// So the separation is decided per PAIR, and only the pairs that need it
// pay for it. Nested lists, headings, blockquotes and fenced code all
// interrupt a paragraph, which is every common tight-list shape.
func renderBlocksTight(blocks []docmodel.Block) string {
	rendered := renderedBlocks(blocks)
	var b strings.Builder
	for i, r := range rendered {
		if i > 0 {
			b.WriteByte('\n')
			if needsBlankLine(rendered[i-1].block, r.block) {
				b.WriteByte('\n')
			}
		}
		b.WriteString(r.text)
	}
	return b.String()
}

// needsBlankLine reports whether prev and next would be read as ONE block
// if written on consecutive lines.
//
// Two ways that happens. A single newline after a paragraph line is a LAZY
// CONTINUATION, so a next block that cannot interrupt a paragraph is not a
// next block at all. And two BLOCKQUOTES are one blockquote however they
// end — "> a" then "> b" is a single quote with two lines in it — which is
// the blockquote's version of the adjacent-list problem renderedBlocks
// solves by alternating markers. A blockquote has no alternate marker, so
// the blank line is the only separator there is.
func needsBlankLine(prev, next docmodel.Block) bool {
	if prev.Kind == docmodel.Blockquote && next.Kind == docmodel.Blockquote {
		return true
	}
	return endsWithParagraph(prev) && !interruptsParagraph(next)
}

// endsWithParagraph reports whether b's last line is an OPEN paragraph line
// — one that a following line would be read as continuing. A heading, a
// fenced code block and a thematic break all close themselves; a paragraph
// does not, and neither does a container whose own last block is a
// paragraph, since lazy continuation reaches down through blockquotes and
// list items.
func endsWithParagraph(b docmodel.Block) bool {
	switch b.Kind {
	case docmodel.Paragraph, docmodel.Image, docmodel.Note:
		// An image block is written "![alt](src)", which markdown reads as
		// a paragraph containing an image — so it stays open like one. A
		// note is written "{>>…<<}", which markdown reads as a paragraph
		// too, and one that is never empty. A paragraph with nothing in it
		// is not written at all, so it leaves no line open.
		return !rendersNothing(b)
	case docmodel.BulletList, docmodel.OrderedList:
		// Always the LAST item, empty or not. Every item writes at least
		// its marker, so an empty one still ends the line before it — and
		// a line holding only a marker is not an open paragraph.
		if n := len(b.Children); n > 0 {
			return endsWithParagraph(b.Children[n-1])
		}
	case docmodel.Blockquote, docmodel.ListItem:
		// The last child that is actually WRITTEN. Here the skipping is
		// right, because a block that renders to nothing contributes no
		// line at all: FuzzRoundTrip found a list whose innermost item held
		// an empty paragraph, which put a blank line in the output on
		// behalf of a block that was never there — and the blank line
		// closed the list item everything after it belonged to.
		for i := len(b.Children) - 1; i >= 0; i-- {
			if !rendersNothing(b.Children[i]) {
				return endsWithParagraph(b.Children[i])
			}
		}
	case docmodel.Table:
		// A table IS a paragraph to goldmark's block scanner: the table
		// parser is a PARAGRAPH TRANSFORMER that finds a delimiter row among
		// a paragraph's lines. So a line written directly under the last row
		// is another line of the same paragraph — and since the delimiter is
		// already above it, it becomes another ROW. A paragraph after a table
		// silently becomes table content.
		return !rendersNothing(b)
	}
	return false
}

// rendersNothing reports whether b contributes no output. It mirrors the
// cases renderedBlocks drops, structurally rather than by rendering, so
// that asking the question does not cost a second pass over the subtree.
//
// A ListItem "renders nothing" when its BODY does; its marker is still
// written, which is exactly what endsWithParagraph needs to know — a
// marker on its own leaves no paragraph line open.
func rendersNothing(b docmodel.Block) bool {
	switch b.Kind {
	case docmodel.Paragraph:
		// normalize is what renderInlines plans over, so "renders nothing"
		// is asked of exactly the sequence that would be rendered.
		return len(normalize(b.Inlines).inlines) == 0
	case docmodel.BulletList, docmodel.OrderedList:
		return len(b.Children) == 0
	case docmodel.ListItem:
		for _, c := range b.Children {
			if !rendersNothing(c) {
				return false
			}
		}
		return true
	case docmodel.Table:
		return tableRendersNothing(b)
	}
	return false
}

// interruptsParagraph reports whether b's first line ends the paragraph
// above it rather than continuing it.
//
// Rule is the member of the "does not" set that is easy to get wrong: a
// line of dashes directly under a paragraph line is not a thematic break,
// it is a SETEXT HEADING underline, and writing one there would turn the
// paragraph above into an h2 rather than separating the two.
func interruptsParagraph(b docmodel.Block) bool {
	switch b.Kind {
	case docmodel.Paragraph, docmodel.Image, docmodel.Rule, docmodel.Table, docmodel.Note, docmodel.FrontMatter:
		// Two separate reasons to need the blank line, both about a block
		// silently becoming part of the one above it.
		//
		// Table is the reason this list is dangerous to get wrong in the
		// other direction. A table does not merely fail to interrupt a
		// paragraph — it EATS its last line: the transformer takes the line
		// above the delimiter row as the header, so "prose" written flush
		// above "| a |" and "| --- |" loses "prose" from the paragraph and
		// gains it as a header row. The blank line is not cosmetic.
		//
		// A note's line begins "{", which starts no block construct — so
		// written flush under a paragraph it is a LAZY CONTINUATION and the
		// note is swallowed back into that paragraph's text, where it reads
		// as a range comment instead of a block one. The blank line is what
		// keeps the anchor kind from changing on a round trip.
		return false
	case docmodel.BulletList, docmodel.OrderedList:
		// A list interrupts a paragraph only if its first item has
		// CONTENT. An empty one does not — and worse, its whole first line
		// is the marker, so a bare "-" under a paragraph line is a SETEXT
		// HEADING underline: the paragraph above becomes an h2 and the list
		// disappears into it. FuzzRoundTrip found exactly that.
		return len(b.Children) > 0 && renderBlocksTight(b.Children[0].Children) != ""
	}
	return true
}

// renderBlock renders one block. alt selects the alternate list marker
// ("*" rather than "-", ")" rather than "."); see renderedBlocks, which is
// the only thing that can know whether it is needed.
//
// It returns the marker style ACTUALLY used, which for a list is not always
// the one asked for: renderList flips the bullet when the requested one would
// spell a thematic break. renderedBlocks alternates from the returned value,
// because what separates two adjacent lists is the character in the file, not
// the one this function was asked for.
func renderBlock(b docmodel.Block, alt bool) (string, bool) {
	switch b.Kind {
	case docmodel.Paragraph:
		return renderInlines(b.Inlines, lineContext{atLineStart: true}), alt
	case docmodel.Heading:
		level, err := strconv.Atoi(b.Attrs["level"])
		if err != nil || level < 1 {
			level = 1
		}
		hashes := strings.Repeat("#", level)
		body := renderInlines(b.Inlines, lineContext{heading: true})
		if body == "" {
			// A HEADING WITH NOTHING IN IT IS "#", NOT "# ". The space is a
			// SEPARATOR, and there is nothing on the other side of it to
			// separate from — so writing it puts trailing whitespace into
			// somebody's file, which `galley edit` then saves back on the first
			// projection with no keystroke. Same ruling as renderListMarked's
			// TrimRight for an item with no content.
			//
			// A heading is not dropped the way an empty paragraph is: "#" is a
			// block markdown CAN write, it reads back as the same empty
			// heading, and the author typed the hashes.
			return hashes, alt
		}
		return hashes + " " + body, alt
	case docmodel.CodeBlock:
		return renderCodeBlock(b), alt
	case docmodel.MathBlock:
		// VERBATIM, exactly as FrontMatter is written below, and TrimRight for
		// the same reason: Text holds the block's own trailing newline and the
		// loop supplies the separator between blocks.
		return strings.TrimRight(b.Text, "\n"), alt
	case docmodel.Blockquote:
		return renderBlockquote(b.Children), alt
	case docmodel.BulletList:
		return renderList(b.Children, false, alt)
	case docmodel.OrderedList:
		return renderList(b.Children, true, alt)
	case docmodel.Rule:
		return "---", alt
	case docmodel.Image:
		// Alt text is inline TEXT — Parse reads it as such, and rejects an
		// image whose alt holds anything but text — so it is escaped like
		// text, plus the brackets that would close the label early.
		return renderImage(b.Attrs["alt"], b.Attrs["src"]), alt
	case docmodel.Table:
		return renderTable(b), alt
	case docmodel.Note:
		// The ONE place Serialize emits comment syntax. A block or document
		// comment has no mark to hang on, so the file is the only carrier it
		// has — see note.go.
		//
		// It carries `alt` through untouched: a note is not a list, so it
		// neither consumes nor changes the marker style the alternation is
		// tracking, and swallowing it here would let two lists either side
		// of a note pick the same bullet and merge.
		return renderNote(b), alt
	case docmodel.FrontMatter:
		// Serialize hoists the leading one, which is the only place Parse can
		// produce it. A block that reaches here is from a hand-built document
		// and is written VERBATIM anyway — dropping it through the default
		// case would be a deletion, and this file's whole subject is that a
		// silent deletion is worse than a shape somebody has to look at.
		return strings.TrimRight(renderFrontMatter(b), "\n"), alt
	default:
		// ListItem is only ever rendered through renderList, and every
		// other kind is covered above; an unreached default keeps the
		// switch exhaustive-by-inspection without a panic on new kinds.
		return "", alt
	}
}

// renderBlockquote renders children loose (blank line between blocks, per
// Task 2's blockquote fixture), then prefixes every resulting line with
// "> " — or a bare ">" on a blank line, so the quote marker never trails
// whitespace.
func renderBlockquote(children []docmodel.Block) string {
	body := renderBlocksLoose(children)
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n")
}

// renderList renders a BulletList's or OrderedList's items tight: no blank
// line between items, and no blank line between a leading paragraph and a
// nested list within the same item. Item content beyond the first line —
// wrapped inline text or additional child blocks such as a nested list —
// is indented to the width of THIS item's own marker ("- " is 2 columns,
// "1. " is 3, "10. " is 4), not a flat constant: CommonMark measures a
// nested list's required indent from where the parent item's content
// actually starts, and an indent narrower than the marker de-nests it on
// reparse.
//
// It returns the style it actually used, because the flip below overrides
// what renderedBlocks asked for and renderedBlocks has to alternate from what
// reached the file.
func renderList(items []docmodel.Block, ordered, alt bool) (string, bool) {
	out := renderListMarked(items, ordered, alt)
	if !ordered && hasThematicBreakLine(out) {
		// Nested items with no content put their markers on ONE line, and
		// three of them is "- - -" — a THEMATIC BREAK, which is not a list
		// at all. The whole nested structure disappears on the next read.
		//
		// Flipping this list's bullet character is enough and is enough
		// forever: the markers after it on that line come from other lists
		// and do not move, so the line now holds two different characters,
		// and a thematic break needs one.
		return renderListMarked(items, ordered, !alt), !alt
	}
	return out, alt
}

// hasThematicBreakLine reports whether any line of out is a thematic break.
func hasThematicBreakLine(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		r := []rune(line)
		if len(r) > 0 && opensThematicBreak(r, 0, len(r)) {
			return true
		}
	}
	return false
}

func renderListMarked(items []docmodel.Block, ordered, alt bool) string {
	lines := make([]string, len(items))
	for i, item := range items {
		marker := bulletMarker(alt)
		if ordered {
			marker = strconv.Itoa(i+1) + orderedDelim(alt) + " "
		}
		indent := strings.Repeat(" ", len(marker))
		body := renderBlocksTight(item.Children)
		if body == "" {
			// An item with no content at all. Its marker still makes the
			// item, but the space after it would be trailing whitespace in
			// somebody's file.
			lines[i] = strings.TrimRight(marker, " ")
			continue
		}
		itemLines := strings.Split(body, "\n")
		itemLines[0] = marker + itemLines[0]
		for j := 1; j < len(itemLines); j++ {
			if itemLines[j] == "" {
				// The blank line separating two of the item's blocks. It
				// stays bare: indenting it would write trailing whitespace
				// into the file for no reader to gain anything from.
				continue
			}
			itemLines[j] = indent + itemLines[j]
		}
		lines[i] = strings.Join(itemLines, "\n")
	}
	return strings.Join(lines, "\n")
}

// renderInlines (block-level entry point for Paragraph/Heading), the mark
// nesting order, and the code-span fence logic live in render_inline.go —
// escaping there must see a whole block's planned output at once, not
// just one Inline, so it isn't simple per-Inline string concatenation.

// bulletMarker and orderedDelim are the two marker styles markdown offers
// for each list kind. Which one a list gets is decided by renderedBlocks
// from its siblings, not from the list itself: the styles are
// interchangeable in meaning, and the only thing that makes one of them
// necessary is a same-kind list sitting immediately before this one.
func bulletMarker(alt bool) string {
	if alt {
		return "* "
	}
	return "- "
}

func orderedDelim(alt bool) string {
	if alt {
		return ")"
	}
	return "."
}

// renderCodeBlock writes a fenced code block whose fence its own content
// cannot close, and whose info string is legal for the fence character it
// uses. Both halves of that sentence were once "```" and a newline, and
// both were wrong in ways FuzzRoundTrip found.
//
// A block containing a "```" line — a markdown file documenting markdown —
// was written inside a fence its content closed, and split in two, losing
// the rest. And CommonMark forbids a backtick ANYWHERE in a backtick
// fence's info string, so a block whose language came from a TILDE fence
// ("~~~`0") was written as "````0": a four-backtick opener that the closer
// below could not close, growing the file by one orphaned fence per save.
// A tilde fence has no rule against backticks, so that is what such a block
// gets.
func renderCodeBlock(b docmodel.Block) string {
	info := b.Attrs["language"]
	fenceChar := byte('`')
	if strings.ContainsRune(info, '`') {
		fenceChar = '~'
	}
	width := closingFenceRun(b.Text, fenceChar) + 1
	if width < 3 {
		width = 3
	}
	fence := strings.Repeat(string(fenceChar), width)
	opener := fence + info
	if strings.HasPrefix(info, string(fenceChar)) {
		// An info string starting with the fence character would EXTEND the
		// fence run instead of following it — "~~~" plus "~`" reads as a
		// four-tilde opener that the three-tilde closer below cannot close,
		// and the file grows by an orphaned fence every save. A space
		// breaks the run, and the reader trims it back off before the info
		// string starts. It goes on the OPENER only: a closing fence may
		// carry nothing but whitespace after it, and a trailing space is
		// not something to write into somebody's file.
		opener = fence + " " + info
	}
	body := b.Text
	if body != "" && !strings.HasSuffix(body, "\n") {
		// Parse always leaves a trailing newline; a hand-built doc need
		// not, and without one the closing fence lands on the end of the
		// last content line rather than on a line of its own.
		body += "\n"
	}
	return opener + "\n" + body + fence
}

// closingFenceRun returns the length of the longest run of c on a line of
// text that would CLOSE a fence made of c.
//
// This is CommonMark's closing rule exactly, rather than "the longest run
// anywhere", because the difference is visible in the file: only a line
// holding nothing but the fence character — after at most three spaces of
// indent, and ignoring trailing whitespace — closes a block. A "```" in the
// middle of a line closes nothing, and neither does one indented four
// spaces, so neither has any business widening the fence.
func closingFenceRun(text string, c byte) int {
	longest := 0
	for _, line := range strings.Split(text, "\n") {
		// Trailing whitespace of ANY kind, not just spaces and tabs: a lone
		// carriage return is a line ending to goldmark's scanner, so
		// "```\r" closes a fence exactly as "```" does. FuzzRoundTrip found
		// that one as a code block splitting itself in two.
		line = strings.TrimRight(line, " \t\r\n\v\f")
		indented := strings.TrimLeft(line, " ")
		if len(line)-len(indented) > 3 || indented == "" {
			continue
		}
		if strings.Trim(indented, string(c)) != "" {
			continue
		}
		if n := len(indented); n > longest {
			longest = n
		}
	}
	return longest
}
