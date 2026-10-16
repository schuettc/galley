package htmlpage

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/subaud-tools/galley/internal/markdown"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Extract splits an HTML page. It fails loudly, in the author's spelling,
// when the page cannot be split honestly (spec: "Refusals and safety").
func Extract(src []byte) (*Extraction, error) {
	root, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("htmlpage: cannot read this page as HTML (%w)", err)
	}

	head, err := pageHead(root)
	if err != nil {
		return nil, err
	}

	body := findElement(root, atom.Body)
	segs, blocks, slots := splitBody(body)
	if slots == 0 {
		return nil, fmt.Errorf(
			"htmlpage: nothing on this page is reviewable prose — every element needs more than markdown can say")
	}

	md := []byte(strings.Join(blocks, "\n\n") + "\n")
	if _, _, err := markdown.Parse(md); err != nil {
		return nil, fmt.Errorf(
			"htmlpage: extractor produced markdown galley refuses (%w) — this is a galley bug, report it", err)
	}

	return &Extraction{
		Markdown: md,
		Template: Template{Head: head, Segments: segs, Tail: "</body></html>"},
	}, nil
}

// standIn is the markdown block that marks shell region i in the document.
// Image form when a shot exists, marker paragraph otherwise (Task 5 decides
// which; Extract always starts with the marker form).
func standIn(i int) string {
	return fmt.Sprintf("⟦ shell %d ⟧", i+1)
}

// splitBody walks the body and sorts every element into a content region
// (a Slot plus its markdown blocks) or a verbatim shell segment. Real pages
// nest their prose inside layout containers (a <p> lives under
// <header><div class="wrap">), so the walk descends into any element that
// itself holds content: the container's open and close tags become shell that
// brackets the slots discovered within (spec: "the extractor walks the DOM at
// block level"). Whitespace between two content blocks is dropped; whitespace
// beside shell rides along with it, and trailing whitespace after the last real
// node at body level is discarded.
func splitBody(body *html.Node) (segs []Segment, blocks []string, slots int) {
	s := &bodySplitter{}
	s.walk(body)
	s.flushContent()
	s.flushShell()
	return s.segs, s.blocks, s.slots
}

// bodySplitter carries the running split state across the recursive descent:
// the shell run under construction, the content run under construction, and the
// pending whitespace whose owner is not yet known.
type bodySplitter struct {
	segs        []Segment
	blocks      []string
	slots       int
	shellCount  int
	shell       strings.Builder     // the shell run under construction
	shellReal   bool                // shell run holds a non-whitespace node
	run         []*html.Node        // the content run under construction
	filler      string              // pending whitespace, owner not yet known
	contentMemo map[*html.Node]bool // memoized hasContentDescendant results
}

func (s *bodySplitter) flushShell() {
	if !s.shellReal {
		return
	}
	s.segs = append(s.segs, Segment{Shell: s.shell.String()})
	s.blocks = append(s.blocks, standIn(s.shellCount))
	s.shellCount++
	s.shell.Reset()
	s.shellReal = false
}

func (s *bodySplitter) flushContent() {
	if len(s.run) == 0 {
		return
	}
	slot := &Slot{Index: s.slots}
	for _, n := range s.run {
		kind, _ := contentBlock(n)
		slot.Wrappers = append(slot.Wrappers, Wrapper{
			Kind: kind, Tag: n.Data, Attrs: attrString(n), Text: normalizeWords(nodeText(n)),
		})
		s.blocks = append(s.blocks, blockToMarkdown(n, kind))
	}
	s.segs = append(s.segs, Segment{Slot: slot})
	s.slots++
	s.run = nil
}

// appendShell flushes any open content run, folds the pending whitespace into
// the shell run, and marks the run as holding real (non-whitespace) shell.
func (s *bodySplitter) appendShell(html string) {
	s.flushContent()
	s.shell.WriteString(s.filler)
	s.filler = ""
	s.shell.WriteString(html)
	s.shellReal = true
}

func (s *bodySplitter) walk(parent *html.Node) {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case isWhitespace(c):
			s.filler += renderNode(c)
		case isChrome(c):
			// Chrome is shell-whole, like a verbatim leaf: the whole subtree
			// rides into the template and is never mined for prose. Tested
			// before isContent so a nav or a control strip cannot classify as
			// a paragraph on its way past.
			s.appendShell(renderNode(c))
		case isContent(c):
			if s.shellReal {
				s.shell.WriteString(s.filler)
				s.flushShell()
			}
			s.filler = ""
			s.run = append(s.run, c)
		case s.isContainer(c):
			open, err := openTag(c)
			if err != nil {
				s.appendShell(renderNode(c))
				continue
			}
			s.appendShell(open)
			s.walk(c)
			// The close tag is safe to write as "</" + c.Data + ">" because
			// golang.org/x/net/html normalises every parsed element's Data to
			// the lowercase atom string (e.g. "div", "header"), the same form
			// html.Render uses for the open tag, so the two paths agree.
			s.appendShell("</" + c.Data + ">")
		default: // shell
			s.appendShell(renderNode(c))
		}
	}
}

func isContent(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	_, ok := contentBlock(n)
	return ok
}

// isContainer reports whether an element is not itself content but holds
// content somewhere within, so the walk should descend into it and keep its
// open and close tags as shell. Verbatim leaves (pre, svg, script, and the
// like) are never descended into: their inner markup is preserved byte-for-
// byte even on the off chance it parses a block-level child.
//
// isContainer is a method on bodySplitter so it can consult the memoized
// hasContentDescendant table and avoid O(n²) work on deeply nested pages.
func (s *bodySplitter) isContainer(n *html.Node) bool {
	if n.Type != html.ElementNode || isContent(n) || isVerbatimLeaf(n) {
		return false
	}
	return s.hasContentDescendant(n)
}

func isVerbatimLeaf(n *html.Node) bool {
	switch n.DataAtom {
	// atom.Pre is NOT here: a <pre> is editable code, recovered as a fenced
	// block by classify and rendered back to <pre> by renderBlock.
	case atom.Svg, atom.Script, atom.Style, atom.Textarea,
		atom.Code, atom.Math, atom.Canvas, atom.Template:
		return true
	default:
		return false
	}
}

// hasContentDescendant reports whether any descendant of n classifies as a
// content block — the signal that n is a container worth descending into
// rather than a verbatim shell leaf. Results are memoized in s.contentMemo
// so each node is visited at most once per Extract, keeping the overall walk
// O(n) rather than O(n²) on pathological nesting depths.
func (s *bodySplitter) hasContentDescendant(n *html.Node) bool {
	if v, ok := s.contentMemo[n]; ok {
		return v
	}
	if s.contentMemo == nil {
		s.contentMemo = make(map[*html.Node]bool)
	}
	result := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isChrome(c) {
			// A chrome subtree must not drag its parent open as a container:
			// skipped first, so a nav of links cannot count as content here
			// when the walk itself would have taken it as shell.
			continue
		}
		if isContent(c) {
			result = true
			break
		}
		if c.Type == html.ElementNode && !isVerbatimLeaf(c) && s.hasContentDescendant(c) {
			result = true
			break
		}
	}
	s.contentMemo[n] = result
	return result
}

func isWhitespace(n *html.Node) bool {
	return n.Type == html.TextNode && strings.TrimSpace(n.Data) == ""
}

// pageHead renders the page opening — doctype, <html …>, <head>…</head>,
// <body …> — so Render can rebuild the page around the slots. Fidelity is to
// the rendered form, not the original bytes (spec: "The round-trip guarantee").
func pageHead(root *html.Node) (string, error) {
	var b strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.DoctypeNode {
			if err := html.Render(&b, c); err != nil {
				return "", err
			}
		}
	}
	htmlNode := findElement(root, atom.Html)
	if htmlNode == nil {
		return "", fmt.Errorf("htmlpage: cannot read this page as HTML (no <html> element)")
	}
	open, err := openTag(htmlNode)
	if err != nil {
		return "", err
	}
	b.WriteString(open)
	if headNode := findElement(htmlNode, atom.Head); headNode != nil {
		if err := html.Render(&b, headNode); err != nil {
			return "", err
		}
	}
	body := findElement(htmlNode, atom.Body)
	open, err = openTag(body)
	if err != nil {
		return "", err
	}
	b.WriteString(open)
	return b.String(), nil
}

// openTag renders an element's opening tag alone, matching html.Render's own
// attribute serialization so re-extraction is stable.
func openTag(n *html.Node) (string, error) {
	clone := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace, Attr: n.Attr}
	var b bytes.Buffer
	if err := html.Render(&b, clone); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "</"+n.Data+">"), nil
}

func renderNode(n *html.Node) string {
	var b bytes.Buffer
	if err := html.Render(&b, n); err != nil {
		return ""
	}
	return b.String()
}

func findElement(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, a); found != nil {
			return found
		}
	}
	return nil
}

// attrString renders an element's attributes verbatim in source order, each
// with its leading space, e.g. ` class="eyebrow"`.
func attrString(n *html.Node) string {
	var b strings.Builder
	for _, a := range n.Attr {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(a.Val))
		b.WriteByte('"')
	}
	return b.String()
}
