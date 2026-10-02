package htmlpage

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
)

// task3Fixture is the shell-first page from extract_test.go: nav (recovered
// prose), a two-paragraph content region (the first paragraph classed), an
// <svg> mockup (a genuine verbatim shell leaf — a <pre> is now editable code),
// and a one-paragraph region.
const task3Fixture = `<!doctype html><html><head><title>x</title></head><body>
<nav class="top"><a href="/">home</a></nav>
<p class="eyebrow">A document two parties revise in rounds</p>
<p>The reviewer highlights a sentence.</p>
<svg class="term"><path d="M0 0"/></svg>
<p>When the round comes back.</p>
</body></html>`

// contentFirstFixture opens with prose, so its single stand-in sits between
// the two content regions — deleting it merges them forward.
const contentFirstFixture = `<!doctype html><html><head><title>x</title></head><body>
<p>Alpha one.</p>
<svg class="term"><path d="M0 0"/></svg>
<p>Beta two.</p>
</body></html>`

// fixtureCorpus pairs a fixture's bytes with a label for diagnostics.
type fixtureCorpus struct {
	name string
	src  []byte
}

// fixtureFiles is the idempotence corpus: the Task 3 inline fixture plus every
// real launch page under testdata/*.html — the launch pages are the acceptance
// tests.
func fixtureFiles(t *testing.T) []fixtureCorpus {
	t.Helper()
	corpus := []fixtureCorpus{{name: "task3Fixture", src: []byte(task3Fixture)}}

	paths, err := filepath.Glob(filepath.Join("testdata", "*.html"))
	if err != nil {
		t.Fatalf("glob testdata: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("testdata/*.html is empty — the launch pages are the acceptance corpus")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		corpus = append(corpus, fixtureCorpus{name: filepath.Base(p), src: b})
	}
	return corpus
}

func TestRenderRoundTrip(t *testing.T) {
	e, err := Extract([]byte(task3Fixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	out, err := Render(e.Template, e.Markdown)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, `<nav class="top"><a href="/">home</a></nav>`) {
		t.Errorf("nav shell missing from render:\n%s", got)
	}
	if !strings.Contains(got, `<svg class="term">`) {
		t.Errorf("svg shell missing from render:\n%s", got)
	}
	if !strings.Contains(got, `<p class="eyebrow">A document two parties revise in rounds</p>`) {
		t.Errorf("eyebrow paragraph not preserved with its class:\n%s", got)
	}

	// Edit the classed paragraph's text and re-render: the class survives, the
	// new text pours into it.
	edited := bytes.Replace(e.Markdown,
		[]byte("A document two parties revise in rounds"),
		[]byte("A wholly new eyebrow line"), 1)
	out2, err := Render(e.Template, edited)
	if err != nil {
		t.Fatalf("Render edited: %v", err)
	}
	if !strings.Contains(string(out2), `<p class="eyebrow">A wholly new eyebrow line</p>`) {
		t.Errorf("edited text did not pour into the eyebrow paragraph:\n%s", out2)
	}
}

func TestRenderNewParagraphGetsBareTag(t *testing.T) {
	e, err := Extract([]byte(task3Fixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Append a third paragraph to region 0 (after the two wrapped ones).
	md := bytes.Replace(e.Markdown,
		[]byte("The reviewer highlights a sentence."),
		[]byte("The reviewer highlights a sentence.\n\nA newly added paragraph."), 1)

	out, err := Render(e.Template, md)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `<p>A newly added paragraph.</p>`) {
		t.Errorf("new paragraph did not get a bare <p> tag:\n%s", got)
	}
	// It must not have inherited a wrapper's class.
	if strings.Contains(got, `<p class="eyebrow">A newly added paragraph.</p>`) {
		t.Errorf("new paragraph wrongly inherited a wrapper:\n%s", got)
	}
}

func TestRenderDeletedStandInMergesForward(t *testing.T) {
	e, err := Extract([]byte(contentFirstFixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Delete the (only) stand-in — the reviewer removed the picture between
	// the two prose runs.
	lines := strings.Split(string(e.Markdown), "\n")
	var kept []string
	for _, ln := range lines {
		if strings.Contains(ln, "⟦ shell") {
			continue
		}
		kept = append(kept, ln)
	}
	md := []byte(strings.Join(kept, "\n"))

	out, err := Render(e.Template, md)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	// Both prose runs land in slot 0, in order.
	iAlpha := strings.Index(got, "Alpha one.")
	iBeta := strings.Index(got, "Beta two.")
	iSvg := strings.Index(got, "<svg")
	if iAlpha < 0 || iBeta < 0 {
		t.Fatalf("prose runs missing:\n%s", got)
	}
	if iAlpha >= iBeta || iBeta >= iSvg {
		t.Errorf("both runs should precede the (empty-slot) shell; got Alpha=%d Beta=%d svg=%d:\n%s",
			iAlpha, iBeta, iSvg, got)
	}
	// The shell region ships regardless of the deleted stand-in.
	if iSvg < 0 {
		t.Errorf("svg shell dropped after stand-in deletion:\n%s", got)
	}
	// Slot 1 renders empty: nothing prose-ish sits after the svg.
	after := got[iSvg:]
	if strings.Contains(after, "<p>") {
		t.Errorf("slot 1 should be empty, but a paragraph rendered after the svg:\n%s", after)
	}
}

// TestRenderPoursCodeFence checks that a fenced code block typed into the prose
// now pours back into a <pre> (Move 1): it is editable code, no longer refused.
func TestRenderPoursCodeFence(t *testing.T) {
	e, err := Extract([]byte(task3Fixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// The reviewer typed a fenced code block into the prose.
	md := bytes.Replace(e.Markdown,
		[]byte("When the round comes back."),
		[]byte("```\ncode fence\n```"), 1)

	out, err := Render(e.Template, md)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// A brand-new code block gets a bare <pre> (no matching wrapper).
	if !strings.Contains(string(out), "<pre>code fence\n</pre>") {
		t.Errorf("code fence did not pour into a <pre>:\n%s", out)
	}
}

// TestRenderPreCodeRoundTrip checks the <pre> round trip end to end: a page
// whose only content is a classed <pre> extracts to a fenced block, renders
// back to <pre class="code"> with the verbatim lines, and settles under
// extract→render→extract. A triple-backtick body uses a longer fence and
// round-trips unbroken.
func TestRenderPreCodeRoundTrip(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"two lines", "line1\nline2"},
		{"triple backtick", "before\n```\nafter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := []byte("<!doctype html><html><head></head><body>" +
				`<pre class="code">` + c.body + `</pre>` +
				"</body></html>")
			e, err := Extract(src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			out, err := Render(e.Template, e.Markdown)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			got := string(out)
			want := `<pre class="code">` + c.body + "\n</pre>"
			if !strings.Contains(got, want) {
				t.Errorf("render missing %q:\n%s", want, got)
			}
			// extract→render→extract settles.
			e2, err := Extract(out)
			if err != nil {
				t.Fatalf("re-Extract: %v", err)
			}
			if !bytes.Equal(e.Markdown, e2.Markdown) {
				t.Errorf("markdown churned:\n--- first ---\n%s\n--- second ---\n%s",
					e.Markdown, e2.Markdown)
			}
			if !reflect.DeepEqual(e.Template, e2.Template) {
				t.Errorf("template churned:\n--- first ---\n%#v\n--- second ---\n%#v",
					e.Template, e2.Template)
			}
		})
	}
}

// tableFixture is a minimal HTML page whose single slot will accept a GFM
// table from the reviewer.
const tableFixture = `<!doctype html><html><head><title>x</title></head><body>
<p>Intro paragraph.</p>
</body></html>`

// TestRenderDropsInstructionNote checks that a reviewer instruction — a
// whole-document {>>@document …<<} note block, or an inline highlight — neither
// refuses the pour nor leaks into the published page. Instructions are
// working-copy affordances (suggest.ClearInstructions: "highlights and note
// blocks … never enter a version") and a poured page is a published artifact,
// so they are dropped. Regression: a whole-document instruction on an htmlpage
// doc failed the whole round with "a note block cannot be poured back into this
// page — pages carry prose, not note".
func TestRenderDropsInstructionNote(t *testing.T) {
	e, err := Extract([]byte(task3Fixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for name, note := range map[string]string{
		// A whole-document note from an older galley, with its words.
		"a document note": "{>>@document rewrite the intro to be punchier<<}",
		// The carrier galley writes now: a block comment's ID mark.
		"a block comment's ID mark": "{>>@comment cb-0123456789abcdef<<}",
	} {
		t.Run(name, func(t *testing.T) {
			md := append([]byte(note+"\n\n"), e.Markdown...)
			out, err := Render(e.Template, md)
			if err != nil {
				t.Fatalf("Render refused an instruction note: %v", err)
			}
			got := string(out)
			if strings.Contains(got, "rewrite the intro to be punchier") || strings.Contains(got, "@comment") {
				t.Errorf("instruction text leaked into the published page:\n%s", got)
			}
			// The surrounding prose still pours into its slot.
			if !strings.Contains(got, `<p class="eyebrow">A document two parties revise in rounds</p>`) {
				t.Errorf("prose lost when the note was dropped:\n%s", got)
			}
		})
	}
}

// TestRenderRefusesNestedList checks Finding 1: a list item that carries
// block-level children (a nested list) must refuse via the cannot-be-poured
// error rather than silently dropping the nested content.
func TestRenderRefusesNestedList(t *testing.T) {
	e, err := Extract([]byte(tableFixture))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// A nested list — the reviewer introduced a sub-list inside a list item.
	md := []byte("- outer item\n  - nested item\n")

	_, err = Render(e.Template, md)
	if err == nil {
		t.Fatal("Render accepted a nested list without error")
	}
	if !strings.Contains(err.Error(), "cannot be poured back into this page") {
		t.Errorf("err = %q, want the cannot-be-poured refusal", err.Error())
	}
}

// TestRenderTableCellTagFromKind checks Finding 2: renderTable must key the
// cell element tag on the cell block's Kind (TableHeader → <th>,
// TableCell → <td>) rather than on the row index. The test uses a hand-
// built table block with a TableCell in row 0 and a TableHeader in row 1 —
// impossible from GFM markdown but necessary to expose the ri==0 bug: the old
// code would flip the tags; the fixed code keys purely on Kind.
func TestRenderTableCellTagFromKind(t *testing.T) {
	// Row 0 holds a TableCell (body kind), row 1 holds a TableHeader.
	// A renderer keyed on ri==0 emits <th> for row-0 and <td> for row-1;
	// a renderer keyed on Kind emits <td> for row-0 and <th> for row-1.
	table := docmodel.Block{
		Kind: docmodel.Table,
		Children: []docmodel.Block{
			{
				Kind: docmodel.TableRow,
				Children: []docmodel.Block{
					{
						Kind: docmodel.TableCell,
						Children: []docmodel.Block{
							{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "body-in-row0"}}},
						},
					},
				},
			},
			{
				Kind: docmodel.TableRow,
				Children: []docmodel.Block{
					{
						Kind: docmodel.TableHeader,
						Children: []docmodel.Block{
							{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "header-in-row1"}}},
						},
					},
				},
			},
		},
	}
	got, err := renderTable(table)
	if err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	// TableCell in row 0 → must be <td>, not <th>.
	if !strings.Contains(got, "<td>body-in-row0</td>") {
		t.Errorf("TableCell in row 0 should render as <td>; got:\n%s", got)
	}
	// TableHeader in row 1 → must be <th>, not <td>.
	if !strings.Contains(got, "<th>header-in-row1</th>") {
		t.Errorf("TableHeader in row 1 should render as <th>; got:\n%s", got)
	}
}

func TestExtractRenderExtractSettles(t *testing.T) {
	for _, f := range fixtureFiles(t) {
		t.Run(f.name, func(t *testing.T) {
			e1, err := Extract(f.src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			p2, err := Render(e1.Template, e1.Markdown)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			e2, err := Extract(p2)
			if err != nil {
				t.Fatalf("re-Extract: %v", err)
			}
			if !bytes.Equal(e1.Markdown, e2.Markdown) {
				t.Errorf("markdown churned:\n--- first ---\n%s\n--- second ---\n%s",
					e1.Markdown, e2.Markdown)
			}
			if !reflect.DeepEqual(e1.Template, e2.Template) {
				t.Errorf("template churned:\n--- first ---\n%#v\n--- second ---\n%#v",
					e1.Template, e2.Template)
			}
		})
	}
}

// TestFixtureRecoveryFloor locks the prose-recovery gains from Tasks 1-4 against
// the real launch-page corpus. Content blocks = sum of len(seg.Slot.Wrappers)
// over all slots in a page's template.
//
// These are observed-recovery floors, not targets; the point is that a change
// re-trapping prose into shell trips this test — widening recovery is fine,
// shrinking below these observed values is not.
//
// Observed on 2026-08-29:
//   - galley-tools-index.html: 61 content blocks across 24 slots, 25 shells
//   - galley-tools-buy.html:   13 content blocks across  5 slots,  6 shells
//
// Re-observed on 2026-08-30 after Move 1 recovers <pre> as editable code — the
// index page's two terminal mockups stopped being shell, so its content rose
// and its shells fell (observed, not target):
//   - galley-tools-index.html: 63 content blocks across 23 slots, 24 shells
//   - galley-tools-buy.html:   13 content blocks across  5 slots,  6 shells
//
// Re-observed on 2026-08-30 after chrome (nav/footer/control-strips) became
// shell-whole — the statusbar, the toc nav and the footer stopped surfacing
// as content paragraphs, so counts drop (observed, not target):
//   - galley-tools-index.html: 57 content blocks across 19 slots, 20 shells
//   - galley-tools-buy.html:    8 content blocks across  2 slots,  3 shells
func TestFixtureRecoveryFloor(t *testing.T) {
	type pageFloor struct {
		name       string
		minContent int
		minShells  int
	}
	floors := []pageFloor{
		// floor 55 = observed 57 minus 2; guards against re-trapping prose in shell
		{name: "galley-tools-index.html", minContent: 55, minShells: 1},
		// floor 6 = observed 8 minus 2; guards against re-trapping prose in shell
		{name: "galley-tools-buy.html", minContent: 6, minShells: 1},
	}

	corpus := fixtureFiles(t)
	byName := make(map[string][]byte, len(corpus))
	for _, f := range corpus {
		byName[f.name] = f.src
	}

	for _, fl := range floors {
		t.Run(fl.name, func(t *testing.T) {
			src, ok := byName[fl.name]
			if !ok {
				t.Fatalf("fixture %q not found in testdata", fl.name)
			}
			e, err := Extract(src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			contentBlocks := 0
			shells := 0
			for _, seg := range e.Template.Segments {
				if seg.Slot != nil {
					contentBlocks += len(seg.Slot.Wrappers)
				} else {
					shells++
				}
			}
			if contentBlocks < fl.minContent {
				t.Errorf("%s: content blocks = %d, want >= %d — prose may have been re-trapped into shell",
					fl.name, contentBlocks, fl.minContent)
			}
			// Cheap sanity: structure (shells) must still be present.
			if shells < fl.minShells {
				t.Errorf("%s: shells = %d, want > 0 — structural shell regions vanished",
					fl.name, shells)
			}
		})
	}
}

// chromeAcceptance is one fixture's worth of assertions for
// TestFixtureChromeLeavesContentPane: chrome text absent, hero and Pass B
// prose present.
type chromeAcceptance struct {
	fixture      string
	chromeText   []string // must not appear in content.md at all
	exactlyOnce  []string // must appear exactly once (shared with a legitimate content occurrence)
	stackedNav   string   // the design doc's stacked-nav bug signature; empty if the page has no toc nav
	heroPhrase   string   // hero content that must survive
	passBPhrases []string // Pass B generic-container prose that must survive
}

// assertChromeAcceptance checks one fixture's markdown against the design's
// acceptance criteria (docs/superpowers/specs/2026-08-30-html-chrome-shell-design.md,
// "Acceptance — the real page is the gate").
func assertChromeAcceptance(t *testing.T, md string, c chromeAcceptance) {
	t.Helper()
	for _, bad := range c.chromeText {
		if strings.Contains(md, bad) {
			t.Errorf("chrome text %q leaked into content.md:\n%s", bad, md)
		}
	}
	for _, phrase := range c.exactlyOnce {
		if n := strings.Count(md, phrase); n != 1 {
			t.Errorf("phrase %q appears %d times in content.md, want exactly 1 (toc nav text must not also surface):\n%s", phrase, n, md)
		}
	}
	if c.stackedNav != "" && strings.Contains(md, c.stackedNav) {
		t.Errorf("toc nav surfaced as a stacked-link paragraph in content.md:\n%s", md)
	}
	if !strings.Contains(md, c.heroPhrase) {
		t.Errorf("hero content %q missing from content.md:\n%s", c.heroPhrase, md)
	}
	for _, want := range c.passBPhrases {
		if !strings.Contains(md, want) {
			t.Errorf("Pass B prose %q missing from content.md:\n%s", want, md)
		}
	}
}

// TestFixtureChromeLeavesContentPane locks the design's acceptance criteria
// against the real launch pages: the statusbar, the section-link nav and the
// footer must not surface any of their text as content, while the hero and
// the Pass B prose (round table, CLI reference, pricing rows) must still
// recover untouched.
func TestFixtureChromeLeavesContentPane(t *testing.T) {
	corpus := fixtureFiles(t)
	byName := make(map[string][]byte, len(corpus))
	for _, f := range corpus {
		byName[f.name] = f.src
	}

	cases := []chromeAcceptance{
		{
			fixture: "galley-tools-index.html",
			// Chrome-only text — the statusbar meta/theme button, and the
			// footer's copyright and sister-tool line — must not leak in.
			chromeText: []string{
				"local · fair core",
				"theme",
				"© 2026 Court Schuett",
				// Markdown-link form: the footer renders its links as
				// [muster.tools](…). The bare-text form never appears, so
				// asserting it absent would guard nothing.
				"sister tool: [muster.tools]",
				"llms.txt",
			},
			// The toc nav's section links repeat some section kickers verbatim
			// ("what it's for", "the cli", "setup"), so a bare substring check
			// can't tell the chrome copy from the legitimate kicker paragraph.
			// Each of these has no other source of the phrase in the page's
			// prose, so it must appear exactly once — from the section's own
			// <p class="kicker">, not also from the shelled nav.
			exactlyOnce: []string{"what it's for", "the cli", "setup"},
			// The design doc names this exact stacked-nav sentence as the bug
			// this change fixes ("the section links `galley what it's for the
			// round the cli …`"): if the toc nav ever stopped being chrome,
			// its six links would run together as one paragraph reading like
			// this.
			// If the toc nav ever stopped being chrome, its links would
			// recover as markdown links — [what it's for](#for) — which the
			// section kicker (plain text "what it's for") never produces. The
			// bare stacked sentence never rendered pre-fix (the links were
			// [text](#anchor)), so this is the form that actually guards.
			stackedNav: "[what it's for](#for)",
			heroPhrase: "Edit the draft. Instruct the agent. Read what the round changed.",
			// Pass B recovered prose (round table, CLI reference) must still
			// recover — chrome removal must not collateral-damage generic-
			// container paragraph recovery.
			passBPhrases: []string{
				"A sent round is no longer pending",    // round table row (.rows > .row)
				"Send the round from the command line", // CLI reference row (.adr.cliref > .row2)
			},
		},
		{
			fixture: "galley-tools-buy.html",
			// Same statusbar and footer chrome as the index page (no toc nav
			// on this page).
			chromeText: []string{
				"local · fair core",
				"theme",
				"© 2026 Court Schuett",
				"sister tool: [muster.tools]",
			},
			heroPhrase: "A galley license is $99.",
			// Pass B pricing row (div-based label/value pair, no nav or
			// button — content, not chrome) must still recover.
			passBPhrases: []string{"macOS and Linux, Apple silicon and x86-64 — one static binary."},
		},
	}

	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			src, ok := byName[c.fixture]
			if !ok {
				t.Fatalf("fixture %q not found in testdata", c.fixture)
			}
			e, err := Extract(src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			assertChromeAcceptance(t, string(e.Markdown), c)
		})
	}
}

// TestFixtureCorpusHasContentRegions guards against a regression where a real
// page collapses entirely to shell: idempotence would then pass trivially. Each
// launch page must extract with at least 3 reviewable content regions — the
// prose blocks that land in content.md. (A "region" here is a reviewed prose
// block, not a slot: the buy hero is three consecutive paragraphs that form one
// contiguous slot, but they are three regions the reviewer edits. A page that
// collapsed to shell would yield zero, which is what this catches.)
func TestFixtureCorpusHasContentRegions(t *testing.T) {
	for _, f := range fixtureFiles(t) {
		if f.name == "task3Fixture" {
			continue // inline fixture; the real pages are the acceptance corpus
		}
		t.Run(f.name, func(t *testing.T) {
			e, err := Extract(f.src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			regions := 0
			for _, seg := range e.Template.Segments {
				if seg.Slot != nil {
					regions += len(seg.Slot.Wrappers)
				}
			}
			if regions < 3 {
				t.Errorf("%s extracted %d content regions, want >= 3 — page collapsed to shell",
					f.name, regions)
			}
		})
	}
}
