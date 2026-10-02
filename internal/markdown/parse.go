// Package markdown parses the supported markdown subset into galley's
// docmodel document tree. It never silently mangles a construct it doesn't
// understand: raw HTML, footnotes, text-mixed images, definition lists, ":::"
// directive blocks and "> [!NOTE]" callouts all error, naming the construct and
// the source line. GFM tables are supported (phase 1e) and convert to the
// table/tableRow/tableCell tree docmodel and TipTap share.
//
// TWO CONSTRUCTS ARE CARRIED VERBATIM RATHER THAN MODELLED OR REFUSED, and the
// discriminator between carrying and refusing is whose language the CONTENT is
// in. YAML and TOML front matter (docmodel.FrontMatter, frontmatter.go) and
// "$$" display math (docmodel.MathBlock, math.go) are not markdown: there is no
// prose inside either to review, no mark to hang on one and nothing galley
// could be right or wrong about, so it models that the block is THERE and
// models nothing about what is inside it. The three dialects refused above all
// hold ordinary markdown prose, and carrying THAT verbatim would keep the bytes
// while making the author's own sentences unreviewable — see dialects.go.
//
// Front matter was the construct that was neither modelled nor refused, and so
// the one that was destroyed by being opened; the four line-break dialects were
// the same failure found again a phase later.
package markdown

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// md is the goldmark instance used to parse markdown into an AST. Auto
// heading IDs are off by default (parser.WithAutoHeadingID() would turn them
// on, so it's simply omitted).
//
// The table extension is enabled and CONVERTED. The footnote extension is
// enabled only so its syntax is recognized and can be rejected with a clear
// error: the document model has no footnote, and rendering one silently would
// mangle the document.
//
// The display-math block parser is galley's own — see math.go. It is a PARSER
// rather than the scanner front matter gets, because `$$` can open inside a
// list item or a blockquote where the raw bytes carry a container prefix that
// is not the block's.
var md = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Footnote),
	goldmark.WithParserOptions(mathBlockOption()),
)

// Parse converts markdown source into a docmodel.Doc. It returns an error,
// rather than mangling the document, when the source uses a construct the
// document model doesn't represent: raw HTML, footnotes, or an image mixed
// into a paragraph with other text.
//
// FRONT MATTER IS TAKEN OFF THE FRONT BEFORE GOLDMARK SEES ANYTHING, because
// to goldmark it is not front matter — the "---" is a thematic break and the
// keys under it are a SETEXT HEADING that the closing "---" underlines. That
// reading uses nothing unsupported, so `unsupported` never fired and the
// refusal machinery that protects footnotes, raw HTML, link references and
// mixed images did not cover the one construct galley neither modelled nor
// refused. Measured on the tracked build, opening this and touching nothing:
//
//	---                          ---
//	title: The Spec        ->
//	status: draft                ## title: The Spec status: draft owner: court
//	owner: court
//	---
//
// See frontmatter.go for why it is SCANNED rather than parsed, and docmodel's
// FrontMatter for why it is a block rather than a field.
func Parse(src []byte) (docmodel.Doc, []InlineComment, error) {
	front, rest := splitFrontMatter(src)
	root := md.Parser().Parse(text.NewReader(rest))
	// The refusal machinery names a SOURCE line, so the lines goldmark never
	// saw still have to be counted. Without this a document with four lines of
	// front matter and raw HTML on line 8 refuses "line 4", and the author
	// counts backwards from a number nothing in their file explains.
	p := &converter{src: rest, lineOffset: bytes.Count(front, []byte("\n"))}
	blocks, err := p.blocks(root)
	if err != nil {
		return docmodel.Doc{}, nil, err
	}
	if front != nil {
		blocks = append([]docmodel.Block{frontMatterBlock(front)}, blocks...)
	}
	// BEFORE extractCritic, not after. extractCriticBlocks computes every
	// InlineComment's BlockPath from the tree it walks, and legalize INSERTS a
	// child — so running it afterwards would shift the index of every block a
	// comment is anchored in without moving the path that names it.
	blocks = legalize(blocks)
	doc, comments := extractCritic(docmodel.Doc{Blocks: blocks})
	unifyCommentRuns(doc)
	return doc, comments, nil
}

// unifyCommentRuns gives every highlight piece that carries one comment ID the
// run of the first such piece, in document order.
//
// The scanner stamps one run per span it closes (applyMark), and that is right
// for a span: it is the only thing that knows where the file's span boundaries
// are. But one comment can be SEVERAL spans in the file — a highlight crossing
// emphasis is written as one span per segment, and one crossing a paragraph as
// one span per block — each followed by the same ID mark. The ID is the
// boundary the file draws around the comment, so this package, which reads it,
// is the one that stamps the run that follows it. suggest.MintRuns cannot: it
// sees marks, not the file.
//
// Pieces that become identical in their marks are merged again, which keeps
// Parse's maximal-run invariant.
func unifyCommentRuns(d docmodel.Doc) {
	runs := map[string]string{}
	docmodel.Walk(d, func(_ []int, b *docmodel.Block) {
		changed := false
		for i, in := range b.Inlines {
			for k, m := range in.Marks {
				id := m.Attrs[docmodel.CommentIDAttr]
				if m.Kind != docmodel.Highlight || id == "" {
					continue
				}
				run, seen := runs[id]
				if !seen {
					runs[id] = m.Attrs[docmodel.RunAttr]
					continue
				}
				if m.Attrs[docmodel.RunAttr] == run {
					continue
				}
				attrs := make(map[string]string, len(m.Attrs))
				for key, v := range m.Attrs {
					attrs[key] = v
				}
				attrs[docmodel.RunAttr] = run
				marks := cloneMarks(in.Marks)
				marks[k] = docmodel.Mark{Kind: m.Kind, Attrs: attrs}
				b.Inlines[i].Marks = marks
				changed = true
			}
		}
		if changed {
			b.Inlines = mergeAdjacent(b.Inlines)
		}
	})
}

// legalize makes every container in the tree a shape the BROWSER'S SCHEMA CAN
// BUILD. It is the same invariant tableRow states for a cell — "A CELL ALWAYS
// HOLDS A BLOCK" — for the two containers markdown can spell empty or
// paragraph-less, and it exists for exactly the same reason.
//
// Element tags in the shared fragment are TipTap's (CLAUDE.md), so a node this
// package writes has to satisfy TipTap's CONTENT rule as well as carry its
// name. Two rules markdown can violate without writing anything unusual:
//
//   - listItem is `paragraph block*` — STRICTER than block+. The first child
//     must be a paragraph, and goldmark hands back whatever the item's first
//     line was: a fence, indented code, a heading, a blockquote, a nested list,
//     a table, an image, a rule, a note — or nothing at all, for a bare "-".
//   - blockquote is `block+`. A bare ">" is a Blockquote with no children.
//
// y-prosemirror does not skip a node it cannot build. createNodeFromYElement
// calls schema.node, which is createChecked and THROWS, and the catch deletes
// el._item out of the Y doc; the deletion broadcasts, EditServer's OnUpdate
// fires, touch() schedules Project(), and the author's file is rewritten with
// NO KEYSTROKE. Measured on the tracked build, opening this and touching
// nothing:
//
//  1. ```sh                 1. done
//     brew install galley
//     ```              ->
//  2. ```sh
//     galley edit doc.md
//     ```
//  3. done
//
// Two steps and both commands gone, and the survivor RENUMBERED 3 -> 1, so the
// file reads as a complete one-step procedure. And it cascades: an emptied
// bulletList fails `listItem+` next, and a blockquote holding only that list
// fails `block+` after it, so "> - ```sh ./deploy --now ``` " left nothing.
//
// THE ROUND-TRIP OBJECTION IS NOT REAL HERE EITHER, and that is the part to
// keep — it is the same objection tableRow's comment records being wrong about
// a blank cell, and #63 recorded it as REAL for a list item without measuring
// it. An empty Paragraph renders to nothing, renderedBlocks DROPS a block that
// renders to nothing, and a list marker and a blockquote's ">" are written
// whether or not the body renders anything. So "- x" is spelled "- x" with or
// without the leading empty paragraph, a bare "-" is still "-", and a bare ">"
// is still ">": Serialize is byte-identical on every input, which is asserted
// over the whole corpus rather than argued (TestLegalizeChangesNoBytes), and
// Parse(Serialize(Parse(x))) == Parse(x) is untouched.
//
// It belongs here and NOT in ydoc for tableRow's reason: a bridge that
// synthesised the paragraph would make Parse and ydoc.Read two different models
// of one file, and every round-trip property both layers rest on is stated over
// ONE model. internal/suggest's removeNoteBlock already refills an emptied
// parent with the same empty Paragraph, arriving at the same shape from the
// other end.
//
// Deliberately NOT filled: a list with no items (`listItem+`) and the doc's own
// `block+`. goldmark produces neither, and filling them would WRITE a marker or
// a paragraph the author never typed — the one thing this pass must not do.
func legalize(blocks []docmodel.Block) []docmodel.Block {
	for i := range blocks {
		blocks[i].Children = legalize(blocks[i].Children)
		switch blocks[i].Kind {
		case docmodel.ListItem:
			blocks[i].Children = withLeadingParagraph(blocks[i].Children)
		case docmodel.Blockquote:
			if len(blocks[i].Children) == 0 {
				blocks[i].Children = []docmodel.Block{{Kind: docmodel.Paragraph}}
			}
		}
	}
	return blocks
}

// withLeadingParagraph returns children with a Paragraph guaranteed first, which
// is what listItem's `paragraph block*` requires and the only refill that
// satisfies it.
func withLeadingParagraph(children []docmodel.Block) []docmodel.Block {
	if len(children) > 0 && children[0].Kind == docmodel.Paragraph {
		return children
	}
	return append([]docmodel.Block{{Kind: docmodel.Paragraph}}, children...)
}

// converter carries the source buffer through the AST walk. (Named converter to
// avoid colliding with the imported goldmark/parser package.)
type converter struct {
	src []byte

	// lineOffset is how many lines of the FILE sit above src — the front
	// matter block, which goldmark never sees. Every refusal names a line in
	// the author's file, not a line in what was handed to the parser.
	lineOffset int

	// depth counts nested inlines calls, so the soft-break offsets below are
	// recorded once per BLOCK rather than once per emphasis span.
	depth int
	// softBreaks holds rune offsets, into the concatenated text of the block
	// currently being converted, at which the SOURCE had a soft line break.
	//
	// The model has no soft break — the converter turns one into a space,
	// deliberately, so a paragraph reflows — and that is exactly the
	// information noteLines needs and cannot recover afterwards. A note flush
	// under a paragraph and a note after a typed space are the same document
	// by the time critic.go runs, and they must not be the same block: the
	// second is a range comment (TestNote_InlineNoteAtEndOfParagraph...) and
	// the first is a note ON that paragraph. So the offsets are carried the
	// few statements from here to splitNoteLines and then dropped. They never
	// enter docmodel and never reach the .md.
	softBreaks []int
}

// line returns the 1-indexed source line containing byte offset pos.
func (p *converter) line(pos int) int {
	if pos < 0 {
		pos = 0
	}
	if pos > len(p.src) {
		pos = len(p.src)
	}
	return bytes.Count(p.src[:pos], []byte("\n")) + 1 + p.lineOffset
}

func (p *converter) unsupported(kind string, n ast.Node) error {
	return fmt.Errorf("markdown: %s not supported (line %d)", kind, p.line(n.Pos()))
}

// refuse is unsupported for a node whose NAME the author has never seen.
//
// A REFUSAL IS READ BY SOMEBODY WHO HAS TO CHANGE THE FILE, and every other
// refusal in this package already knows it: "raw HTML", "footnote", "::: directive
// block", "definition list", "> [!NOTE] callout" are all things an author can
// look for. The `default:` arms passed `n.Kind().String()`, which is GOLDMARK'S
// INTERNAL AST KIND, and shipped it to a reviewer's terminal. Measured on the
// binary 2026-08-23:
//
//	galley: markdown: AutoLink not supported (line 1)
//	galley: markdown: LinkReferenceDefinition not supported (line 3)
//
// Nothing in either file says "AutoLink" or "LinkReferenceDefinition". The
// author wrote `<https://example.com>` and `[foo]: /url` — both core CommonMark,
// both things galley genuinely cannot model yet — and was handed the name of a
// Go type in a library they do not know they depend on. CLAUDE.md records both
// constructs as known gaps whose fix is MODELLING rather than refusing; until
// that lands, the refusal is the whole of the product for these documents, so
// it is the whole of what has to be good.
//
// SO A REFUSAL SAYS THREE THINGS: what is there, in the spelling the author
// typed, and — where there is one — what to write instead. A rewrite is offered
// only where it is genuinely equivalent; inventing one for raw HTML or a
// footnote would be worse than the silence.
//
// The fallback keeps the kind name rather than dropping it. An unrecognised
// construct is a construct nobody has written a sentence for yet, and a name a
// maintainer can grep beats "something on line 4".
func (p *converter) refuse(n ast.Node) error {
	// AN EMAIL AUTOLINK IS NOT A URL, and the generic sentence told the author
	// to rewrite it as one. `<court@example.com>` came back "autolinks like
	// <https://example.com> are not supported — write the URL inline as
	// [https://example.com](https://example.com)", which names a construct the
	// author did not write and gives advice that does not apply to the one they
	// did. goldmark already knows the difference; only this did not ask.
	if a, ok := n.(*ast.AutoLink); ok && a.AutoLinkType == ast.AutoLinkEmail {
		return fmt.Errorf("markdown: %s (line %d)",
			"email autolinks like <name@example.com> are not supported — "+
				"write the address as [name@example.com](mailto:name@example.com)",
			p.line(n.Pos()))
	}
	if said, ok := refusals[n.Kind().String()]; ok {
		return fmt.Errorf("markdown: %s (line %d)", said, p.line(n.Pos()))
	}
	return p.unsupported(n.Kind().String(), n)
}

// refusals is the author-facing sentence for each construct the model does not
// carry, keyed by goldmark's AST kind name.
//
// Each entry names the construct the way the AUTHOR spelled it and, where the
// rewrite is exactly equivalent, gives it. Adding a kind here is how a leaked
// internal name becomes a sentence somebody can act on.
var refusals = map[string]string{
	// `<https://example.com>`. The rewrite is exact: an autolink and an inline
	// link with the URL as its text render identically everywhere.
	"AutoLink": "autolinks like <https://example.com> are not supported — " +
		"write the URL inline as [https://example.com](https://example.com)",
	// `[foo]: /url`, with `[foo]` used above it. The rewrite is exact for the
	// definition's own consumers, which is why it names them.
	"LinkReferenceDefinition": "reference-style link definitions like [foo]: /url are not supported — " +
		"write each link inline as [text](/url) and delete the definition line",
}

// blocks converts every block-level child of parent into docmodel.Blocks.
func (p *converter) blocks(parent ast.Node) ([]docmodel.Block, error) {
	var out []docmodel.Block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		b, err := p.block(n)
		if err != nil {
			return nil, err
		}
		// p.softBreaks describes the paragraph p.block just converted; every
		// other kind ignores it. See splitNoteLines.
		out = append(out, splitNoteLines(b, p.softBreaks)...)
	}
	return out, nil
}

// block converts a single block-level node into a docmodel.Block.
func (p *converter) block(n ast.Node) (docmodel.Block, error) {
	switch n.Kind() {
	case kindMathBlock:
		// Verbatim, delimiters and all — see math.go and docmodel.MathBlock.
		return docmodel.Block{
			Kind: docmodel.MathBlock,
			Text: string(n.Lines().Value(p.src)),
		}, nil
	case ast.KindParagraph:
		return p.paragraph(n)
	case ast.KindTextBlock:
		// Tight list items hold their content in a TextBlock rather than a
		// Paragraph (goldmark drops the <p> wrapper for tight lists), but
		// the document model makes no such distinction.
		return p.paragraph(n)
	case ast.KindHeading:
		h := n.(*ast.Heading)
		inlines, err := p.inlines(n, nil)
		if err != nil {
			return docmodel.Block{}, err
		}
		return docmodel.Block{
			Kind:    docmodel.Heading,
			Attrs:   map[string]string{"level": strconv.Itoa(h.Level)},
			Inlines: inlines,
		}, nil
	case ast.KindCodeBlock:
		return docmodel.Block{
			Kind: docmodel.CodeBlock,
			Text: string(n.Lines().Value(p.src)),
		}, nil
	case ast.KindFencedCodeBlock:
		fcb := n.(*ast.FencedCodeBlock)
		block := docmodel.Block{
			Kind: docmodel.CodeBlock,
			Text: string(n.Lines().Value(p.src)),
		}
		if lang := fcb.Language(p.src); len(lang) > 0 {
			block.Attrs = map[string]string{"language": string(lang)}
		}
		return block, nil
	case ast.KindBlockquote:
		children, err := p.blocks(n)
		if err != nil {
			return docmodel.Block{}, err
		}
		return docmodel.Block{Kind: docmodel.Blockquote, Children: children}, nil
	case ast.KindList:
		return p.list(n.(*ast.List))
	case ast.KindThematicBreak:
		return docmodel.Block{Kind: docmodel.Rule}, nil
	case ast.KindHTMLBlock:
		return docmodel.Block{}, p.unsupported("raw HTML", n)
	case extast.KindTable:
		return p.table(n.(*extast.Table))
	case extast.KindFootnote, extast.KindFootnoteList:
		return docmodel.Block{}, p.unsupported("footnote", n)
	default:
		return docmodel.Block{}, p.refuse(n)
	}
}

// paragraph converts a Paragraph or TextBlock node. A paragraph whose only
// content is a single image becomes an Image block, per the document model:
// markdown images are inline syntax, but the model has no inline image, only
// a block one.
func (p *converter) paragraph(n ast.Node) (docmodel.Block, error) {
	// BEFORE ANY INLINE IS CONVERTED, because the evidence is the SOURCE LINES
	// and a soft break is a space by the time inlines() returns. See
	// dialects.go: three constructs that are one paragraph to CommonMark and
	// are destroyed by being reflowed.
	//
	// A callout's marker is read only at the head of a blockquote, which is
	// where GitHub reads it — that is what `head` is.
	head := n.PreviousSibling() == nil && n.Parent() != nil && n.Parent().Kind() == ast.KindBlockquote
	if err := p.refuseDialect(n, head); err != nil {
		return docmodel.Block{}, err
	}
	if img, ok := soloImage(n); ok {
		alt, err := p.plainText(img)
		if err != nil {
			return docmodel.Block{}, err
		}
		return docmodel.Block{
			Kind: docmodel.Image,
			Attrs: map[string]string{
				// goldmark hands back the destination's RAW source bytes,
				// backslashes and all, exactly as it does for a Text node —
				// so a destination Serialize escaped to fit inside "(...)"
				// or "<...>" must be unescaped here, or the backslash
				// accumulates a cycle at a time.
				"src": unescape(string(img.Destination)),
				// Alt text is parsed as ordinary inline content (goldmark
				// re-parses the bracketed label the same way it parses
				// paragraph text), so it can carry backslash-escapes that
				// need the same unescape as any other text run.
				"alt": unescape(alt),
			},
		}, nil
	}
	inlines, err := p.inlines(n, nil)
	if err != nil {
		return docmodel.Block{}, err
	}
	return docmodel.Block{Kind: docmodel.Paragraph, Inlines: inlines}, nil
}

// soloImage reports whether n's only content is a single image, in which
// case the paragraph should become an Image block rather than text.
func soloImage(n ast.Node) (*ast.Image, bool) {
	if n.ChildCount() != 1 {
		return nil, false
	}
	img, ok := n.FirstChild().(*ast.Image)
	return img, ok
}

// plainText concatenates the text content of n's children, used for image
// alt text where marks have no meaning.
func (p *converter) plainText(n ast.Node) (string, error) {
	var buf bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		t, ok := c.(*ast.Text)
		if !ok {
			return "", p.refuse(c)
		}
		buf.Write(t.Value(p.src))
		if t.SoftLineBreak() {
			buf.WriteByte(' ')
		}
	}
	return buf.String(), nil
}

// list converts a List node into a BulletList or OrderedList block whose
// children are ListItem blocks.
func (p *converter) list(l *ast.List) (docmodel.Block, error) {
	kind := docmodel.BulletList
	if l.IsOrdered() {
		kind = docmodel.OrderedList
	}
	var items []docmodel.Block
	for n := l.FirstChild(); n != nil; n = n.NextSibling() {
		if n.Kind() != ast.KindListItem {
			return docmodel.Block{}, p.refuse(n)
		}
		children, err := p.blocks(n)
		if err != nil {
			return docmodel.Block{}, err
		}
		items = append(items, docmodel.Block{Kind: docmodel.ListItem, Children: children})
	}
	return docmodel.Block{Kind: kind, Children: items}, nil
}

// table converts a GFM table into the tree docmodel and TipTap share:
// table > tableRow > tableCell|tableHeader > paragraph.
//
// goldmark's shape differs in one place and it is worth naming, because the
// obvious mirror is wrong. goldmark makes the header a distinct NODE
// (extast.TableHeader) holding ordinary cells. ProseMirror has no header row
// — it has one row node and two kinds of cell — and these kind strings are
// the element tags that cross the CRDT, so this follows ProseMirror.
func (p *converter) table(t *extast.Table) (docmodel.Block, error) {
	var rows []docmodel.Block
	for n := t.FirstChild(); n != nil; n = n.NextSibling() {
		var (
			r   docmodel.Block
			err error
		)
		switch v := n.(type) {
		case *extast.TableHeader:
			r, err = p.tableRow(v, docmodel.TableHeader)
		case *extast.TableRow:
			r, err = p.tableRow(v, docmodel.TableCell)
		default:
			return docmodel.Block{}, p.refuse(n)
		}
		if err != nil {
			return docmodel.Block{}, err
		}
		rows = append(rows, r)
	}
	return docmodel.Block{Kind: docmodel.Table, Children: rows}, nil
}

// tableRow converts one row's cells. kind is what THIS row's cells are:
// tableHeader for the header row, tableCell for every body row.
//
// The cell body goes through paragraph() rather than inlines() so a cell
// holding nothing but an image becomes an Image block, exactly as the same
// content in a paragraph does. A cell holds blocks, so the exception applies
// unchanged; asking inlines() directly would refuse the image outright.
func (p *converter) tableRow(row ast.Node, kind docmodel.BlockKind) (docmodel.Block, error) {
	var cells []docmodel.Block
	for n := row.FirstChild(); n != nil; n = n.NextSibling() {
		c, ok := n.(*extast.TableCell)
		if !ok {
			return docmodel.Block{}, p.refuse(n)
		}
		body, err := p.paragraph(c)
		if err != nil {
			return docmodel.Block{}, err
		}
		out := docmodel.Block{Kind: kind}
		if a := alignName(c.Alignment); a != "" {
			out.Attrs = map[string]string{docmodel.AlignAttr: a}
		}
		// A CELL ALWAYS HOLDS A BLOCK, INCLUDING A BLANK ONE. An empty cell
		// gets an empty Paragraph, exactly like every other cell.
		//
		// This comment used to say the opposite — "an empty cell holds NO
		// child", because an empty paragraph is a block markdown cannot write
		// and a round trip would not be equal to itself. The objection is
		// real everywhere ELSE and does not apply here, which is the whole
		// point: a cell is spelled `| |` whether it holds an empty paragraph
		// or nothing at all (renderCell of a childless paragraph joins zero
		// parts and yields ""), so the fixed point holds either way and
		// Parse(Serialize(Parse(x))) == Parse(x) is untouched. There is no
		// second spelling to choose between; there never was.
		//
		// What the childless form cost was the BROWSER. Element tags in the
		// shared fragment are TipTap's (CLAUDE.md), so the model must be a
		// shape TipTap's schema accepts — and `tableCell` is `block+`, which
		// an empty fragment does not satisfy. y-prosemirror does not skip a
		// node it cannot build: it DELETES it from the Y doc (sync-plugin's
		// createNodeFromYElement catches schema.node's throw and deletes
		// el._item), the deletion is broadcast, EditServer's OnUpdate fires,
		// and the projection writes the shortened row to disk. cellTexts pads
		// a short row at the END, so a blank LAST cell came back
		// byte-identical — which is why this hid — while a blank cell in any
		// earlier column slid every value after it one column left, in the
		// author's file, with no keystroke. Measured before this line
		// changed: `| timeout | | tail |` became `| timeout | tail | |`
		// on open.
		//
		// So the shape belongs here rather than in the bridge: the bridge
		// synthesising the paragraph would make Parse and ydoc.Read produce
		// two different models of one file, and the round-trip properties
		// both layers rest on are stated over ONE model.
		out.Children = []docmodel.Block{body}
		cells = append(cells, out)
	}
	return docmodel.Block{Kind: docmodel.TableRow, Children: cells}, nil
}

// alignName is goldmark's alignment as the file spells it. AlignNone returns
// "" and gets no attribute at all: a "---" column is the default, and writing
// "none" into Attrs would make an absent attribute and a present one two
// different documents that serialize identically.
func alignName(a extast.Alignment) string {
	switch a {
	case extast.AlignLeft:
		return "left"
	case extast.AlignRight:
		return "right"
	case extast.AlignCenter:
		return "center"
	}
	return ""
}

// inlines walks the inline children of parent, producing a flat, merged
// sequence of docmodel.Inlines. marks holds the marks accumulated from
// enclosing emphasis/link nodes on the way down.
func (p *converter) inlines(parent ast.Node, marks []docmodel.Mark) ([]docmodel.Inline, error) {
	p.depth++
	defer func() { p.depth-- }()
	top := p.depth == 1
	if top {
		p.softBreaks = p.softBreaks[:0]
	}

	var out []docmodel.Inline
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		switch n.Kind() {
		case ast.KindText:
			t := n.(*ast.Text)
			// goldmark's block scanner recognizes a backslash-escape well
			// enough to keep the escaped character from being read as
			// markup, but never strips the backslash from the segment's
			// raw source bytes — unescape does that here. See escape.go.
			value := unescape(string(t.Value(p.src)))
			if t.SoftLineBreak() {
				value += " "
			}
			out = append(out, docmodel.Inline{Text: value, Marks: cloneMarks(marks)})
			if top && t.SoftLineBreak() {
				// The offset of the line boundary, in runes into this block's
				// text. Recorded after the run is appended, and only at the
				// top level: a soft break inside emphasis or a link cannot
				// begin a note line, since a note line carries no marks.
				// mergeAdjacent only concatenates, so these offsets stay valid
				// through it.
				p.softBreaks = append(p.softBreaks, runeLen(out))
			}
			if t.HardLineBreak() {
				out = append(out, docmodel.Inline{
					Marks: append(cloneMarks(marks), docmodel.Mark{Kind: docmodel.HardBreak}),
				})
			}
		case ast.KindString:
			s := n.(*ast.String)
			out = append(out, docmodel.Inline{Text: string(s.Value), Marks: cloneMarks(marks)})
		case ast.KindCodeSpan:
			value, err := p.plainText(n)
			if err != nil {
				return nil, err
			}
			out = append(out, docmodel.Inline{
				// CommonMark converts a line ending inside a code span to a
				// space, and goldmark's own renderer does the same — but
				// plainText reads the raw source bytes, where the newline is
				// still a newline. Keeping it would put a code span in the
				// document that markdown cannot write back: Serialize emits
				// the newline, and the next line of the paragraph then
				// begins with the span's own "```" fence, which opens a
				// FENCED CODE BLOCK. The paragraph becomes a code block and
				// its text becomes an info string, of which only the first
				// word survives.
				Text:  strings.ReplaceAll(value, "\n", " "),
				Marks: append(cloneMarks(marks), docmodel.Mark{Kind: docmodel.Code}),
			})
		case ast.KindEmphasis:
			e := n.(*ast.Emphasis)
			mk := docmodel.Italic
			if e.Level >= 2 {
				mk = docmodel.Bold
			}
			nested, err := p.inlines(n, append(cloneMarks(marks), docmodel.Mark{Kind: mk}))
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
		case ast.KindLink:
			l := n.(*ast.Link)
			// Same as an image's src: the destination arrives unescaped only
			// if Parse unescapes it.
			href := unescape(string(l.Destination))
			linkMark := docmodel.Mark{Kind: docmodel.Link, Attrs: map[string]string{"href": href}}
			nested, err := p.inlines(n, append(cloneMarks(marks), linkMark))
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
		case ast.KindImage:
			return nil, p.unsupported("image mixed with text", n)
		case ast.KindRawHTML:
			return nil, p.unsupported("raw HTML", n)
		case extast.KindFootnoteLink, extast.KindFootnoteBacklink:
			return nil, p.unsupported("footnote", n)
		default:
			return nil, p.refuse(n)
		}
	}
	return mergeAdjacent(out), nil
}

func cloneMarks(marks []docmodel.Mark) []docmodel.Mark {
	out := make([]docmodel.Mark, len(marks))
	copy(out, marks)
	return out
}

// mergeAdjacent merges neighboring inlines that carry identical mark sets
// (in order) into a single inline, so that e.g. two adjacent goldmark Text
// nodes produced across a soft line break collapse into one run.
func mergeAdjacent(inlines []docmodel.Inline) []docmodel.Inline {
	var out []docmodel.Inline
	for _, in := range inlines {
		if n := len(out); n > 0 && sameMarks(out[n-1].Marks, in.Marks) && !isHardBreak(in) && !isHardBreak(out[n-1]) {
			out[n-1].Text += in.Text
			continue
		}
		out = append(out, in)
	}
	return out
}

func isHardBreak(in docmodel.Inline) bool {
	return in.Has(docmodel.HardBreak)
}

func sameMarks(a, b []docmodel.Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind {
			return false
		}
		if len(a[i].Attrs) != len(b[i].Attrs) {
			return false
		}
		for k, v := range a[i].Attrs {
			if b[i].Attrs[k] != v {
				return false
			}
		}
	}
	return true
}
