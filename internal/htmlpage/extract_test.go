package htmlpage

import (
	"reflect"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

func TestExtractSplitsProseFromShell(t *testing.T) {
	src := []byte(`<!doctype html><html><head><title>x</title></head><body>
<nav class="top"><a href="/">home</a></nav>
<p class="eyebrow">A document two parties revise in rounds</p>
<p>The reviewer highlights a sentence.</p>
<svg class="term"><path d="M0 0"/></svg>
<p>When the round comes back.</p>
</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// A <nav> is chrome (#3): shelled whole, never mined for prose, so it does
	// NOT recover as a paragraph. The split is now shell(nav), slot(eyebrow,
	// p), shell(svg), slot(p). (A <pre> would be editable code; the svg here is
	// a genuine verbatim leaf.)
	if got := len(ex.Template.Segments); got != 4 {
		t.Fatalf("segments = %d, want 4", got)
	}
	segs := ex.Template.Segments

	// shell(nav)
	if segs[0].Slot != nil {
		t.Errorf("segment 0 should be shell (the nav chrome), got slot")
	}
	if !strings.Contains(segs[0].Shell, "<nav") {
		t.Errorf("segment 0 shell = %q, want it to hold the nav", segs[0].Shell)
	}

	// slot 0: the two paragraphs, nav no longer among them
	if segs[1].Slot == nil {
		t.Fatalf("segment 1 should be a slot")
	}
	want0 := []Wrapper{
		{Kind: "paragraph", Tag: "p", Attrs: ` class="eyebrow"`, Text: "a document two parties revise in rounds"},
		{Kind: "paragraph", Tag: "p", Attrs: "", Text: "the reviewer highlights a sentence"},
	}
	if !reflect.DeepEqual(segs[1].Slot.Wrappers, want0) {
		t.Errorf("slot 0 wrappers = %#v, want %#v", segs[1].Slot.Wrappers, want0)
	}

	// shell(svg)
	if segs[2].Slot != nil {
		t.Errorf("segment 2 should be shell, got slot")
	}
	if !strings.Contains(segs[2].Shell, "<svg") {
		t.Errorf("segment 2 shell = %q, want it to hold the svg", segs[2].Shell)
	}

	// slot 1
	if segs[3].Slot == nil {
		t.Fatalf("segment 3 should be a slot")
	}
	want1 := []Wrapper{{Kind: "paragraph", Tag: "p", Attrs: "", Text: "when the round comes back"}}
	if !reflect.DeepEqual(segs[3].Slot.Wrappers, want1) {
		t.Errorf("slot 1 wrappers = %#v, want %#v", segs[3].Slot.Wrappers, want1)
	}

	md := string(ex.Markdown)
	if !strings.Contains(md, "The reviewer highlights a sentence.") {
		t.Errorf("markdown missing first prose paragraph:\n%s", md)
	}
	if !strings.Contains(md, "When the round comes back.") {
		t.Errorf("markdown missing second prose paragraph:\n%s", md)
	}
	if strings.Contains(md, "[home](/)") {
		t.Errorf("nav link leaked into content pane — nav is chrome:\n%s", md)
	}
	if n := strings.Count(md, "⟦ shell"); n != 2 {
		t.Errorf("stand-in count = %d, want 2 (nav, svg):\n%s", n, md)
	}

	if _, _, err := markdown.Parse(ex.Markdown); err != nil {
		t.Errorf("markdown.Parse rejected extractor output: %v\n%s", err, md)
	}
}

// TestExtractChromeIsShellHeroIsContent is the acceptance shape of the real
// page in miniature: a statusbar control strip, a toc nav and a footer are
// chrome (shell-whole, no content block, never mined for prose), while the
// hero <header> — no banner role, block prose inside — still recovers.
func TestExtractChromeIsShellHeroIsContent(t *testing.T) {
	src := []byte(`<!doctype html><html><head><title>x</title></head><body>
<div class="statusbar"><div class="wrap">
<a class="sb-brand" href="/"><b>galley</b></a>
<nav class="sb-nav"><a href="/buy/">license</a><a href="https://muster.tools">muster</a></nav>
<span class="sb-meta">local · fair core <button class="theme-toggle">theme</button></span>
</div></div>
<nav class="toc"><a href="#for">what it's for</a><a href="#how">the round</a></nav>
<header class="hero" id="top">
<p class="eyebrow">A document two parties revise in rounds</p>
<h1 class="lede">Edit the draft. Instruct the agent.</h1>
<p class="sub">galley opens a Markdown file in the browser.</p>
</header>
<footer><span>local · fair core</span> <a href="/buy/">license</a></footer>
</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	md := string(ex.Markdown)

	// The hero's prose is review copy and must survive intact.
	for _, want := range []string{
		"A document two parties revise in rounds",
		"Edit the draft. Instruct the agent.",
		"galley opens a Markdown file in the browser.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing hero prose %q:\n%s", want, md)
		}
	}

	// Chrome text must not reach the content pane at all.
	for _, bad := range []string{"license", "muster", "fair core", "theme", "what it's for", "the round"} {
		if strings.Contains(md, bad) {
			t.Errorf("chrome text %q leaked into the content pane:\n%s", bad, md)
		}
	}

	// Chrome is shell-whole: each of statusbar, toc nav and footer is one
	// verbatim segment, never descended into.
	var shells []string
	for _, seg := range ex.Template.Segments {
		if seg.Slot == nil {
			shells = append(shells, seg.Shell)
		}
	}
	if len(shells) != 2 {
		t.Fatalf("shell segments = %d, want 2 (statusbar+toc run, footer run):\n%#v", len(shells), shells)
	}
	all := strings.Join(shells, "")
	for _, want := range []string{
		`<div class="statusbar">`, `<button class="theme-toggle">theme</button>`,
		`<nav class="toc">`, `<footer>`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("chrome %q missing from the template shell:\n%s", want, all)
		}
	}

	// The hero's three blocks are the only content.
	blocks := 0
	for _, seg := range ex.Template.Segments {
		if seg.Slot != nil {
			blocks += len(seg.Slot.Wrappers)
		}
	}
	if blocks != 3 {
		t.Errorf("content blocks = %d, want 3 (the hero's eyebrow, lede and sub)", blocks)
	}
}

func TestExtractRefusesAllShell(t *testing.T) {
	src := []byte(`<!doctype html><html><head></head><body>
<div class="tg"></div>
<svg><path/></svg>
</body></html>`)
	_, err := Extract(src)
	if err == nil {
		t.Fatal("Extract accepted an all-shell page")
	}
	want := "htmlpage: nothing on this page is reviewable prose — every element needs more than markdown can say"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestExtractGenericContainerAsParagraph(t *testing.T) {
	src := []byte(`<!doctype html><html><head></head><body>` +
		`<div class="rows"><div class="row"><span>REVISE</span><span>Pressing Revise commits.</span></div></div>` +
		`</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var slot *Slot
	for _, s := range ex.Template.Segments {
		if s.Slot != nil {
			slot = s.Slot
		}
	}
	if slot == nil {
		t.Fatalf("no slot found in %#v", ex.Template.Segments)
	}
	want := []Wrapper{{Kind: "paragraph", Tag: "div", Attrs: ` class="row"`, Text: "revise pressing revise commits"}}
	if !reflect.DeepEqual(slot.Wrappers, want) {
		t.Errorf("wrappers = %#v, want %#v", slot.Wrappers, want)
	}
	md := string(ex.Markdown)
	if !strings.Contains(md, "REVISE Pressing Revise commits.") {
		t.Errorf("markdown missing flattened prose (single space between spans):\n%s", md)
	}
	// div.rows brackets the recovered paragraph as shell.
	if !strings.Contains(md, "⟦ shell") {
		t.Errorf("div.rows should bracket the slot as shell:\n%s", md)
	}
}

func TestExtractRefusesBadHTML(t *testing.T) {
	src := []byte(strings.Repeat("<div>", 600))
	_, err := Extract(src)
	if err == nil {
		t.Fatal("Extract accepted HTML that html.Parse rejects")
	}
	if !strings.HasPrefix(err.Error(), "htmlpage: cannot read this page as HTML (") {
		t.Errorf("err = %q, want the bad-HTML refusal", err.Error())
	}
}

func TestExtractMarkdownAlwaysAccepted(t *testing.T) {
	// The paragraph's TEXT looks like markdown: emphasis, a link reference,
	// and an autolink. escapeMD must neutralize the constructs galley cannot
	// carry so markdown.Parse accepts the extractor's output.
	src := []byte(`<!doctype html><html><head></head><body>` +
		`<p>a *b* [c]: d &lt;https://e&gt;</p>` +
		`</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	md := string(ex.Markdown)
	if !strings.Contains(md, `\*b\*`) {
		t.Errorf("emphasis not escaped:\n%s", md)
	}
	if !strings.Contains(md, `\[c\]`) {
		t.Errorf("link reference not escaped:\n%s", md)
	}
	if _, _, err := markdown.Parse(ex.Markdown); err != nil {
		t.Errorf("markdown.Parse rejected markdown-looking text: %v\n%s", err, md)
	}
}

// findCodeInline searches a parsed Doc for the first Code-marked inline and
// returns its text, or ("", false) if none is found.
func findCodeInline(doc docmodel.Doc) (string, bool) {
	for _, b := range doc.Blocks {
		for _, in := range b.Inlines {
			if in.Has(docmodel.Code) {
				return in.Text, true
			}
		}
	}
	return "", false
}

// TestExtractCodeSpanContainsBacktick checks that a <code> element whose
// text contains backticks produces valid markdown whose code-span text
// round-trips to the original string.
func TestExtractCodeSpanContainsBacktick(t *testing.T) {
	codeText := "fmt.Sprintf(\"`%s`\", x)"
	src := []byte("<!doctype html><html><head></head><body>" +
		"<p>Try <code>" + codeText + "</code> here.</p>" +
		"</body></html>")
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	doc, _, err := markdown.Parse(ex.Markdown)
	if err != nil {
		t.Fatalf("markdown.Parse rejected output: %v\n%s", err, ex.Markdown)
	}
	got, ok := findCodeInline(doc)
	if !ok {
		t.Fatalf("no Code-marked inline found in parsed doc; markdown:\n%s", ex.Markdown)
	}
	if got != codeText {
		t.Errorf("code span text = %q, want %q", got, codeText)
	}
}

// TestExtractPreAsCodeBlock checks that a <pre> is recovered as one fenced
// code block: its text lands verbatim inside the fence, its element is recorded
// as a codeBlock wrapper carrying the pre's tag and attributes, and the
// extractor's markdown is accepted by markdown.Parse.
func TestExtractPreAsCodeBlock(t *testing.T) {
	src := []byte("<!doctype html><html><head></head><body>" +
		`<pre class="code">line1` + "\n" + `line2</pre>` +
		"</body></html>")
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var slot *Slot
	for _, s := range ex.Template.Segments {
		if s.Slot != nil {
			slot = s.Slot
		}
	}
	if slot == nil {
		t.Fatalf("no slot found in %#v", ex.Template.Segments)
	}
	want := []Wrapper{{Kind: "codeBlock", Tag: "pre", Attrs: ` class="code"`, Text: "line1 line2"}}
	if !reflect.DeepEqual(slot.Wrappers, want) {
		t.Errorf("wrappers = %#v, want %#v", slot.Wrappers, want)
	}

	md := string(ex.Markdown)
	if !strings.Contains(md, "```\nline1\nline2\n```") {
		t.Errorf("markdown missing fenced code block with the two lines:\n%s", md)
	}

	doc, _, err := markdown.Parse(ex.Markdown)
	if err != nil {
		t.Fatalf("markdown.Parse rejected extractor output: %v\n%s", err, md)
	}
	if got := codeBlockText(doc); got != "line1\nline2\n" {
		t.Errorf("parsed code block text = %q, want %q", got, "line1\nline2\n")
	}
}

// TestExtractPreWithTripleBacktick checks that a <pre> whose text contains a
// triple-backtick line is fenced with a longer run so its content cannot close
// the fence, and the text round-trips through markdown.Parse unbroken.
func TestExtractPreWithTripleBacktick(t *testing.T) {
	src := []byte("<!doctype html><html><head></head><body>" +
		"<pre>before\n```\nafter</pre>" +
		"</body></html>")
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	md := string(ex.Markdown)
	if !strings.Contains(md, "````\nbefore\n```\nafter\n````") {
		t.Errorf("triple-backtick content not wrapped in a longer fence:\n%s", md)
	}
	doc, _, err := markdown.Parse(ex.Markdown)
	if err != nil {
		t.Fatalf("markdown.Parse rejected extractor output: %v\n%s", err, md)
	}
	if got := codeBlockText(doc); got != "before\n```\nafter\n" {
		t.Errorf("parsed code block text = %q, want %q", got, "before\n```\nafter\n")
	}
}

// codeBlockText returns the text of the first CodeBlock in doc, or "".
func codeBlockText(doc docmodel.Doc) string {
	for _, b := range doc.Blocks {
		if b.Kind == docmodel.CodeBlock {
			return b.Text
		}
	}
	return ""
}

// TestExtractCodeSpanEdgeBacktick checks that a <code> element whose text
// starts and ends with a backtick round-trips correctly through markdown.
func TestExtractCodeSpanEdgeBacktick(t *testing.T) {
	codeText := "`foo`"
	src := []byte("<!doctype html><html><head></head><body>" +
		"<p>See <code>" + codeText + "</code> there.</p>" +
		"</body></html>")
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	doc, _, err := markdown.Parse(ex.Markdown)
	if err != nil {
		t.Fatalf("markdown.Parse rejected output: %v\n%s", err, ex.Markdown)
	}
	got, ok := findCodeInline(doc)
	if !ok {
		t.Fatalf("no Code-marked inline found in parsed doc; markdown:\n%s", ex.Markdown)
	}
	if got != codeText {
		t.Errorf("code span text = %q, want %q", got, codeText)
	}
}
