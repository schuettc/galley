package htmlpage

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// mdEscaper neutralizes the characters that would otherwise turn plain page
// text into a markdown construct galley cannot carry. Backslash goes first so
// the escapes it inserts are not themselves re-escaped.
//
// The set is the brief's `*_[]`#|` plus `<` and `>`: an unescaped `<` is read
// as an autolink or raw HTML, both of which galley's markdown refuses, and the
// extractor's hard constraint is to emit only markdown markdown.Parse accepts.
var mdEscaper = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"[", `\[`,
	"]", `\]`,
	"#", `\#`,
	"|", `\|`,
	"<", `\<`,
	">", `\>`,
)

func escapeMD(s string) string { return mdEscaper.Replace(s) }

// collapseWS folds every run of HTML whitespace into a single space, keeping a
// single leading or trailing space when the text had one. Prose in a
// prettified page carries the source's newlines and indentation, which are
// insignificant to HTML but which markdown would misread — a line indented
// four spaces becomes a code block. Collapsing normalizes them away while
// preserving the word boundary a space carries across an inline element; the
// renderer emits inline runs on one line, so the round-trip is stable.
func collapseWS(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// blockToMarkdown renders one content block as the markdown galley accepts for
// its kind. The kind is the docmodel.BlockKind classify already decided.
func blockToMarkdown(n *html.Node, kind string) string {
	switch kind {
	case string(docmodel.Heading):
		return strings.Repeat("#", headingLevel(n)) + " " + strings.TrimSpace(inlineChildren(n))
	case string(docmodel.BulletList):
		return listMarkdown(n, false)
	case string(docmodel.OrderedList):
		return listMarkdown(n, true)
	case string(docmodel.Blockquote):
		return quoteMarkdown(n)
	case string(docmodel.Table):
		return tableMarkdown(n)
	case string(docmodel.CodeBlock):
		// A <pre> is a leaf: its content is its full text, nested <span>s
		// flattened, emitted VERBATIM inside a fence its content cannot close.
		return codeBlockMarkdown(textContent(n))
	default: // paragraph
		return strings.TrimSpace(inlineChildren(n))
	}
}

// codeBlockMarkdown wraps verbatim code in a backtick fence wide enough that
// its own content cannot close it: N backticks where N is one more than the
// longest backtick run in the text, minimum 3 (mirroring markdown.CodeSpan's
// rule for spans). The body is emitted unchanged except for a single trailing
// newline when it lacks one, so the closing fence lands on its own line and the
// extract→render→extract round trip settles rather than growing a newline per
// pass.
func codeBlockMarkdown(text string) string {
	fence := strings.Repeat("`", fenceWidth(text))
	body := text
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return fence + "\n" + body + fence
}

// fenceWidth returns the backtick-fence width for text: one more than the
// longest run of backticks anywhere in it, never fewer than 3.
func fenceWidth(text string) int {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	width := longest + 1
	if width < 3 {
		width = 3
	}
	return width
}

func headingLevel(n *html.Node) int {
	if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
		return int(n.Data[1] - '0')
	}
	return 1
}

func listMarkdown(n *html.Node, ordered bool) string {
	var items []string
	i := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Li {
			continue
		}
		i++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", i)
		}
		items = append(items, marker+strings.TrimSpace(inlineChildren(c)))
	}
	return strings.Join(items, "\n")
}

func quoteMarkdown(n *html.Node) string {
	var paras []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.P {
			continue
		}
		paras = append(paras, "> "+strings.TrimSpace(inlineChildren(c)))
	}
	return strings.Join(paras, "\n>\n")
}

func tableMarkdown(n *html.Node) string {
	rows := tableRows(n)
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("| " + strings.Join(cells, " | ") + " |")
	}
	writeRow(rows[0])
	seps := make([]string, len(rows[0]))
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString("\n")
	writeRow(seps)
	for _, r := range rows[1:] {
		b.WriteString("\n")
		writeRow(r)
	}
	return b.String()
}

func tableRows(n *html.Node) [][]string {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Tr {
				rows = append(rows, tableCells(c))
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return rows
}

func tableCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
			cells = append(cells, strings.TrimSpace(inlineChildren(c)))
		}
	}
	return cells
}

// inlineChildren renders a block's inline content — text and the whitelisted
// inline elements — as markdown.
func inlineChildren(n *html.Node) string {
	var b strings.Builder
	var prev *html.Node
	prevOut := ""
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out := inlineNode(c)
		// Two adjacent flattened element children with no whitespace text node
		// between them (a text node would have made prev non-element) get a
		// single space so <span>REVISE</span><span>Pressing</span> renders
		// "REVISE Pressing", not "REVISEPressing".
		if prev != nil && prev.Type == html.ElementNode && c.Type == html.ElementNode &&
			prevOut != "" && out != "" &&
			!endsWithSpace(prevOut) && !beginsWithSpace(out) {
			b.WriteByte(' ')
		}
		b.WriteString(out)
		prev = c
		prevOut = out
	}
	return b.String()
}

func endsWithSpace(s string) bool { return len(s) > 0 && s[len(s)-1] == ' ' }

func beginsWithSpace(s string) bool { return len(s) > 0 && s[0] == ' ' }

func inlineNode(n *html.Node) string {
	if n.Type == html.TextNode {
		return escapeMD(collapseWS(n.Data))
	}
	if n.Type != html.ElementNode {
		return ""
	}
	switch n.DataAtom {
	case atom.A:
		// Use markdown.LinkDestination so a destination containing a space,
		// parenthesis, or control character is written in CommonMark's
		// angle-bracket form instead of being emitted bare, where a bare ")"
		// truncates the link and a bare space drops it.
		return "[" + inlineChildren(n) + "](" + markdown.LinkDestination(href(n)) + ")"
	case atom.Em, atom.I:
		return "*" + inlineChildren(n) + "*"
	case atom.Strong, atom.B:
		return "**" + inlineChildren(n) + "**"
	case atom.Span:
		// A span is transparent: emit its inner content with the span dropped.
		return inlineChildren(n)
	case atom.Code:
		// Use markdown.CodeSpan so that content containing backticks is
		// fenced with a run one longer than the longest run inside, and
		// padding is added when the content starts or ends with a backtick.
		// A plain single-backtick fence silently corrupts such content.
		return markdown.CodeSpan(textContent(n))
	default:
		return ""
	}
}

func href(n *html.Node) string {
	for _, a := range n.Attr {
		if a.Key == "href" {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
