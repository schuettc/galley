package htmlpage

import (
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// contentBlock reports whether a block-level element's entire content is
// expressible in galley markdown, and the docmodel.BlockKind it maps to.
// A block is content only when markdown can say all of it; anything else is
// shell, rebuilt verbatim.
func contentBlock(n *html.Node) (kind string, ok bool) {
	k, pass := classify(n)
	if !pass {
		return "", false
	}
	return k, true
}

func classify(n *html.Node) (kind string, ok bool) {
	switch n.DataAtom {
	case atom.P:
		return string(docmodel.Paragraph), inlineOK(n) && !isStyledBand(n)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return string(docmodel.Heading), inlineOK(n)
	case atom.Ul:
		return string(docmodel.BulletList), listOK(n)
	case atom.Ol:
		return string(docmodel.OrderedList), listOK(n)
	case atom.Blockquote:
		return string(docmodel.Blockquote), quoteOK(n)
	case atom.Table:
		return string(docmodel.Table), tableOK(n)
	case atom.Pre:
		// A <pre> is an editable code block, recovered as a fenced block. It
		// is a LEAF: its content is its full text (textContent flattens any
		// nested <span>s to text), never descended into as structure.
		return string(docmodel.CodeBlock), true
	default:
		// A generic block container (div, section, header, …) is a paragraph
		// when, after span transparency, its whole content is inline and it
		// carries text. Verbatim leaves (pre, code, svg, …) never qualify.
		if isVerbatimLeaf(n) {
			return "", false
		}
		if inlineOK(n) && hasText(n) && !isStyledBand(n) {
			return string(docmodel.Paragraph), true
		}
		return "", false
	}
}

// isStyledBand reports whether a block is a styled inline COMPONENT — a
// .row/.adr/.cliref band whose columns are sibling classed spans, a decorative
// strip, and the like — rather than prose. Markdown has no syntax for "these
// spans are columns", so extracting such a block fuses its parts into run-on
// text and the band renders wrong when it is poured back (the reviewer's
// repeated "rendering of table is wrong again"). A band is therefore not
// content: the walk shells it whole, it round-trips untouched, and its wording
// is edited on the .html layer.
//
// The test is STRUCTURAL, never by class name: a block with no loose prose text
// of its own whose inline content is two or more CLASSED <span>/<i> elements is
// a band. A single classed span embedded in connecting prose — "the <span
// class=\"k\">galley</span> cli" — is not: the loose words are the sentence, the
// span merely styles one of them, and dropping the class leaves the sentence
// whole. That case stays content, as it always has. An unclassed-span row also
// stays content (see TestExtractGenericContainerAsParagraph): a bare <span> is a
// transparent wrapper the extractor may flatten, and only a class markdown
// cannot carry makes the flattening lossy.
func isStyledBand(n *html.Node) bool {
	classed := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				return false
			}
		case html.ElementNode:
			if isClassedInline(c) {
				classed++
			}
		}
	}
	return classed >= 2
}

// isClassedInline reports whether n is an inline span or icon carrying a class —
// styling markdown cannot reproduce, unlike a bare <span>/<i> which is a
// transparent wrapper.
func isClassedInline(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Span, atom.I:
		return attrValue(n, "class") != ""
	}
	return false
}

// isChrome reports whether an element is page furniture rather than review
// copy: navigation, a footer, or a control strip like a top statusbar. Chrome
// is shell-whole — the walk never descends into it and never mines it for
// prose — so it is shown in the live preview and never lands in content.md.
//
// The predicate is deliberately not tag-blind about <header>: the hero of a
// launch page is a <header> holding the headline and the intro paragraphs,
// the most important review copy on the page. Only a header that declares
// role="banner" is chrome.
func isChrome(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	// (a) a landmark of navigation or page furniture.
	if n.DataAtom == atom.Nav || n.DataAtom == atom.Footer {
		return true
	}
	switch attrValue(n, "role") {
	case "navigation", "banner", "contentinfo":
		return true
	}
	// (b) a control strip: no block prose anywhere in it, and it holds a genuine
	// control — a <nav> or a <button>. The control requirement is what separates
	// a top bar (brand + nav + theme button) from a bare row of call-to-action
	// links (the hero's "Ask for a build →") or a div-based table of label/value
	// rows: those carry links and inline text but no nav and no button, so they
	// stay content.
	return !hasBlockProse(n) && hasInteractive(n) && hasControl(n)
}

// hasControl reports whether n contains a <nav> or a <button> descendant — the
// mark of a control strip, as opposed to a row that merely contains links.
func hasControl(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.Nav || c.DataAtom == atom.Button || hasControl(c) {
			return true
		}
	}
	return false
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(strings.ToLower(a.Val))
		}
	}
	return ""
}

// hasBlockProse reports whether n or any descendant is block prose. It counts
// n itself so that a content block — a <p> carrying a link, a <ul> of links —
// can never be mistaken for a control strip.
func hasBlockProse(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Ul, atom.Ol, atom.Blockquote, atom.Table, atom.Pre:
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasBlockProse(c) {
			return true
		}
	}
	return false
}

// hasInteractive reports whether n contains a link or a button. Unlike
// hasBlockProse it looks at descendants only: a bare <a> is a line of prose
// with a link in it, not a strip of controls.
func hasInteractive(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.A || c.DataAtom == atom.Button || hasInteractive(c) {
			return true
		}
	}
	return false
}

// hasText reports whether n has at least one descendant text node holding a
// non-whitespace rune. It is the non-empty guard that keeps an empty layout
// container from becoming an empty editable paragraph.
func hasText(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			return true
		}
		if c.Type == html.ElementNode && hasText(c) {
			return true
		}
	}
	return false
}

// nodeText returns the concatenated text of every descendant text node, so a
// content block can be fingerprinted (align.go) from the same words render will
// see once it is poured back.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
			b.WriteByte(' ')
		}
		for gc := c.FirstChild; gc != nil; gc = gc.NextSibling {
			walk(gc)
		}
	}
	walk(n)
	return b.String()
}

// inlineOK reports whether every child of n is inline content markdown can
// carry: text, or one of the whitelisted inline elements, recursively.
func inlineOK(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if !inlineNodeOK(c) {
			return false
		}
	}
	return true
}

func inlineNodeOK(n *html.Node) bool {
	if n.Type == html.TextNode {
		return true
	}
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.A:
		return hasHref(n) && inlineOK(n)
	case atom.Em, atom.I, atom.Strong, atom.B, atom.Code:
		return inlineOK(n)
	case atom.Span:
		// A span is a transparent inline wrapper: it passes iff its own
		// children pass, and it carries no markup markdown must reproduce.
		return inlineOK(n)
	default:
		return false
	}
}

func hasHref(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "href" {
			return true
		}
	}
	return false
}

// listOK reports whether every li child's content passes the inline test.
// Nested lists are shell in v1.
func listOK(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			continue
		}
		if c.DataAtom != atom.Li || !inlineOK(c) {
			return false
		}
	}
	return true
}

// quoteOK reports whether every child of a blockquote is itself a passing p.
func quoteOK(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			continue
		}
		if c.DataAtom != atom.P || !inlineOK(c) {
			return false
		}
	}
	return true
}

// tableOK reports whether every cell in the table passes the inline test.
func tableOK(n *html.Node) bool {
	ok := true
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode &&
				(c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
				if !inlineOK(c) {
					ok = false
				}
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return ok
}
