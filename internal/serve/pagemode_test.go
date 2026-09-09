package serve

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/htmlpage"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/versions"
	"github.com/schuettc/galley/internal/ydoc"
)

// fixturePage is a small page with prose (an eyebrow paragraph and two body
// paragraphs) between shell regions (a nav and a styled mockup) — enough to
// exercise the split, the round-trip, and drift.
const fixturePage = `<!doctype html><html><head><title>x</title></head><body>
<nav class="top"><a href="/">home</a></nav>
<p class="eyebrow">A document two parties revise in rounds</p>
<p>The reviewer highlights a sentence.</p>
<pre class="term"><span class="k">galley</span> edit</pre>
<p>When the round comes back.</p>
</body></html>`

func writePage(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// retext replaces the inline text of the block whose current text is `old`
// with `now`, the way a reviewer editing a paragraph does — through the live
// document.
func retext(t *testing.T, s *EditServer, old, now string) {
	t.Helper()
	if _, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		out := docmodel.Doc{}
		for _, b := range model.Blocks {
			var text string
			for _, in := range b.Inlines {
				text += in.Text
			}
			if text == old {
				b.Inlines = []docmodel.Inline{{Text: now}}
			}
			out.Blocks = append(out.Blocks, b)
		}
		return out, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNewEditPageExtractsAndCovers(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	base := filepath.Join(dir, ".galley", "pages", "page")
	for _, name := range []string{"content.md", "template.json", "original.html"} {
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	md, err := os.ReadFile(filepath.Join(base, "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := markdown.Parse(md); err != nil {
		t.Errorf("content.md does not parse as markdown: %v", err)
	}
	if !strings.Contains(string(md), "A document two parties revise in rounds") {
		t.Errorf("content.md missing the eyebrow prose:\n%s", md)
	}
}

func TestNewEditPageRetainsPage(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if !s.pageMode {
		t.Errorf("pageMode = false, want true")
	}
	if !strings.HasSuffix(s.pagePath, "page.html") {
		t.Errorf("pagePath = %q, want suffix page.html", s.pagePath)
	}
	if s.pageRoot == nil {
		t.Errorf("pageRoot = nil, want non-nil")
	}
}

// TestMarkdownModeLeavesPageFieldsZero covers I1: a plain markdown-mode
// server (NewEdit on a .md file) must leave the page fields at their zero
// values, so nothing downstream mistakes a markdown review for a page review.
func TestMarkdownModeLeavesPageFieldsZero(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "note.md", "# Title\n\nA paragraph.\n")
	defer func() { _ = s.Close() }()

	if s.pageMode {
		t.Errorf("pageMode = true, want false")
	}
	if s.pagePath != "" {
		t.Errorf("pagePath = %q, want empty", s.pagePath)
	}
	if s.pageRoot != nil {
		t.Errorf("pageRoot = %v, want nil", s.pageRoot)
	}
	if s.pageRender != nil {
		t.Errorf("pageRender = %v, want nil", s.pageRender)
	}
}

func TestProjectRendersThePage(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	const now = "A document two parties revise together"
	retext(t, s, "A document two parties revise in rounds", now)
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<p class="eyebrow">`+now+`</p>`) {
		t.Errorf("page.html did not carry the new sentence inside the eyebrow paragraph:\n%s", out)
	}
	if !strings.Contains(string(out), `<nav class="top">`) {
		t.Errorf("page.html lost its shell nav:\n%s", out)
	}
}

// TestDriftTriggersReextract is the drift guard's new job: a page.html whose
// hash matches neither lastWritten nor originalHash is the agent's structural
// edit, not an intrusion — it routes to re-extraction, refreshes the template,
// and signals `changed`, with none of the old "not overwriting; close and
// reopen" refusal wording (that line is gone; this is now the normal
// structural round, not a stall).
func TestDriftTriggersReextract(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	var buf bytes.Buffer
	prev := pageStderr
	pageStderr = &buf
	defer func() { pageStderr = prev }()

	r := s.pageRender
	opened := r.template

	// A structurally-valid hand edit: still HTML with reviewable prose, just a
	// different structure than galley wrote — the agent restructuring the page
	// directly, out of band from a projection.
	structural := strings.Replace(fixturePage, "<nav class=\"top\"><a href=\"/\">home</a></nav>\n", "", 1)
	if structural == fixturePage {
		t.Fatal("precondition: the structural edit changed nothing")
	}
	if err := os.WriteFile(page, []byte(structural), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != structural {
		t.Errorf("galley rendered over the agent's structural edit — drift should route to re-extract, not render:\n%s", out)
	}
	if reflect.DeepEqual(opened, r.template) {
		t.Errorf("template was not refreshed from the drifted page")
	}
	if strings.Contains(templateShell(r.template), `<nav class="top">`) {
		t.Errorf("template still holds the removed nav — it was not refreshed")
	}
	if _, changed := r.lastExtract(); !changed {
		t.Errorf("structural drift reported changed = false, want true")
	}
	if strings.Contains(buf.String(), "not overwriting") {
		t.Errorf("the old refusal wording must be gone from the normal structural path, got %q", buf.String())
	}
}

// TestDriftOfUnparseablePageStillRefuses pins the tear guard the drift switch
// keeps: a page.html caught with content that no longer reads as reviewable
// HTML (the stand-in for a torn write, or genuine garbage) is not a
// restructure galley can carry — it is a refusal, cut as a could-not round via
// the same recordCannot path as any other render refusal, and the file is left
// exactly as found so nothing is clobbered while the reviewer decides what to
// do about it. This holds regardless of whether a --on-revise Log hook is
// configured (F1: the plain `galley edit page.html` path, where s.Log is nil,
// must still surface the refusal — here, as a could-not round).
func TestDriftOfUnparseablePageStillRefuses(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	// No Log hook set — this is the plain `galley edit` path.
	if s.Log != nil {
		t.Fatal("precondition: s.Log should be nil for this test")
	}

	base := countCouldNot(t, s)

	// No reviewable prose at all — Extract refuses with "nothing on this page is
	// reviewable prose", the genuine-tear case this guard exists for.
	const junk = "<html><body>someone edited the shell by hand</body></html>"
	if err := os.WriteFile(page, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != junk {
		t.Errorf("drift was overwritten — page.html should still hold the unparseable edit:\n%s", out)
	}
	if got := countCouldNot(t, s) - base; got != 1 {
		t.Fatalf("unparseable page: want 1 could-not round, got %d", got)
	}
}

func TestOriginalNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s1, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage (first): %v", err)
	}
	originalPath := filepath.Join(dir, ".galley", "pages", "page", "original.html")
	firstOriginal, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	// The page moves on disk between sessions, but stays a reviewable page.
	later := strings.Replace(fixturePage,
		"A document two parties revise in rounds",
		"A later, different sentence in the same page", 1)
	if err := os.WriteFile(page, []byte(later), 0o644); err != nil {
		t.Fatal(err)
	}

	s2, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage (second): %v", err)
	}
	defer func() { _ = s2.Close() }()

	secondOriginal, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(secondOriginal) != string(firstOriginal) {
		t.Errorf("original.html was overwritten by a later session:\nwant %q\ngot  %q", firstOriginal, secondOriginal)
	}
}

// templateShell is every verbatim shell byte in a template, joined — enough to
// ask whether a structural element (a nav, a section) still rides the shell.
func templateShell(tmpl htmlpage.Template) string {
	var b strings.Builder
	b.WriteString(tmpl.Head)
	for _, seg := range tmpl.Segments {
		b.WriteString(seg.Shell)
	}
	b.WriteString(tmpl.Tail)
	return b.String()
}

// TestContentRoundReextractsStable is the common path: a wording round renders
// content into the page and then re-extracts it, and idempotence means the
// re-extract lands on the same markdown the projection just wrote — the
// template is refreshed but nothing churns and no change is signalled.
func TestContentRoundReextractsStable(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	r := s.pageRender
	if r == nil {
		t.Fatal("pageRender = nil, want the live renderer")
	}
	opened := r.template

	const now = "A document two parties revise together"
	retext(t, s, "A document two parties revise in rounds", now)
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<p class="eyebrow">`+now+`</p>`) {
		t.Fatalf("page.html did not re-render the content edit:\n%s", out)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".galley", "pages", "page", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	ex, err := htmlpage.Extract(out)
	if err != nil {
		t.Fatalf("re-Extract of the rendered page: %v", err)
	}
	if !bytes.Equal(ex.Markdown, content) {
		t.Errorf("re-extract churned:\n--- content.md ---\n%s\n--- re-extracted ---\n%s", content, ex.Markdown)
	}

	md, changed := r.lastExtract()
	if changed {
		t.Errorf("content round reported changed = true, want false")
	}
	if !bytes.Equal(md, content) {
		t.Errorf("renderer's re-extracted markdown != content.md:\n--- content.md ---\n%s\n--- renderer ---\n%s", content, md)
	}
	// The wrapper fingerprints (Wrapper.Text) track the content by design, so a
	// wording round updates them — that is the identity pour keeping alignment
	// current, not churn. Compare STRUCTURE (shell, tags, classes), which must
	// not move.
	if !reflect.DeepEqual(structureOnly(opened), structureOnly(r.template)) {
		t.Errorf("template STRUCTURE churned on a content round:\n--- opened ---\n%#v\n--- now ---\n%#v", opened, r.template)
	}
}

// structureOnly clears the per-block text fingerprints so a template comparison
// tests structure (shell, tags, classes) rather than content, which the
// fingerprints deliberately track.
func structureOnly(t htmlpage.Template) htmlpage.Template {
	segs := make([]htmlpage.Segment, len(t.Segments))
	for i, seg := range t.Segments {
		if seg.Slot != nil {
			wraps := make([]htmlpage.Wrapper, len(seg.Slot.Wrappers))
			for j, w := range seg.Slot.Wrappers {
				w.Text = ""
				wraps[j] = w
			}
			slot := *seg.Slot
			slot.Wrappers = wraps
			seg.Slot = &slot
		}
		segs[i] = seg
	}
	t.Segments = segs
	return t
}

// TestAContentRoundNeverReloadsTheLiveEditor is the unit-level form of what
// web/page.mjs proves, and it pins the reload to the DRIFT path alone.
//
// THE RELOAD EXISTS TO PUSH A STRUCTURAL CHANGE INTO THE LIVE EDITOR, AND A
// CONTENT ROUND HAS NONE. On the no-drift path the live document is already the
// authority for every word the page holds — galley just rendered it there — so
// there is nothing to push back and everything to lose by pushing: the reload
// replaces the whole model with a RE-DERIVATION of it, carries no `extra`, and
// so drops every pending instruction mark the reviewer has filed.
//
// Measured before the gate: filing an instruction and projecting reloaded the
// editor and left the pending set EMPTY. `changed` goes true on a content round
// whenever render→re-extract does not round-trip the content's spelling, and an
// instruction carrier — the reviewer's own {==...==} — is the worst case: it is
// poured into the page as ordinary prose and comes back with its markers gone,
// which reads as a structural change to a signal that only asks whether the
// markdown differs. So the signal is not what gates the reload; the drift is.
func TestAContentRoundNeverReloadsTheLiveEditor(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	// The reviewer files an instruction on a paragraph and edits another — one
	// ordinary content round, nobody near page.html.
	const anchor = "The reviewer highlights a sentence."
	const asked = "say it is a page, and that galley renders it"
	commentAs(t, s, anchor, asked)
	const now = "A document two parties revise together"
	retext(t, s, "A document two parties revise in rounds", now)

	before, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Instructions) != 1 {
		t.Fatalf("the fixture filed %d instructions, not 1 — it cannot show the loss", len(before.Instructions))
	}
	live := liveMarkdown(t, s)
	if !bytes.Contains(live, []byte("{=="+anchor+"==}")) {
		t.Fatalf("precondition: the live document should carry the instruction's highlight:\n%s", live)
	}

	if err := s.Project(); err != nil {
		t.Fatalf("Project (content): %v", err)
	}

	if n := s.pageRender.reloadCount(); n != 0 {
		t.Errorf("a content round reloaded the live editor %d times, want 0 — the reload is structural-only", n)
	}
	after, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Instructions) != 1 || after.Instructions[0].Text != asked {
		t.Errorf("the content round left %d instruction(s) pending: %+v — want the reviewer's ask, untouched",
			len(after.Instructions), after.Instructions)
	}
	if got := liveMarkdown(t, s); !bytes.Equal(got, live) {
		t.Errorf("the content round moved the live document under the reviewer:\n--- was ---\n%s\n--- now ---\n%s", live, got)
	}

	// And the round still did its own job: the page carries the new words.
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<p class="eyebrow">`+now+`</p>`) {
		t.Errorf("the content round never reached page.html:\n%s", out)
	}
}

// TestStructuralRoundRefreshesTemplate is the other path: the agent reworks
// page.html itself (here, dropping the nav and a paragraph). galley must take
// the agent's page as it stands — not render over it — refresh the template
// from it, and surface that the content moved. The reload on that signal is
// Task 3; this only pins the signal.
func TestStructuralRoundRefreshesTemplate(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	var buf bytes.Buffer
	prev := pageStderr
	pageStderr = &buf
	defer func() { pageStderr = prev }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	r := s.pageRender
	if !strings.Contains(templateShell(r.template), `<nav class="top">`) {
		t.Fatalf("precondition: the opened template should hold the nav")
	}

	structural := strings.Replace(fixturePage, "<nav class=\"top\"><a href=\"/\">home</a></nav>\n", "", 1)
	structural = strings.Replace(structural, "<p>When the round comes back.</p>\n", "", 1)
	if structural == fixturePage {
		t.Fatal("precondition: the structural edit changed nothing")
	}
	if err := os.WriteFile(page, []byte(structural), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != structural {
		t.Errorf("galley rendered over the agent's structural edit:\n%s", out)
	}
	if strings.Contains(templateShell(r.template), `<nav class="top">`) {
		t.Errorf("template still holds the removed nav — it was not refreshed")
	}

	md, changed := r.lastExtract()
	if !changed {
		t.Errorf("structural round reported changed = false, want true")
	}
	if strings.Contains(string(md), "When the round comes back") {
		t.Errorf("re-extracted markdown still carries the removed paragraph:\n%s", md)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".galley", "pages", "page", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(md, content) {
		t.Errorf("re-extracted markdown equals content.md — no change signal to act on")
	}
}

// structuralPage is the fixture with the nav and the last body paragraph
// removed — the agent restructuring the page directly, which is a change to
// both the shell (the nav) and the content (the paragraph).
func structuralPage(t *testing.T) string {
	t.Helper()
	out := strings.Replace(fixturePage, "<nav class=\"top\"><a href=\"/\">home</a></nav>\n", "", 1)
	out = strings.Replace(out, "<p>When the round comes back.</p>\n", "", 1)
	if out == fixturePage {
		t.Fatal("precondition: the structural edit changed nothing")
	}
	return out
}

// navlessPage is the fixture with only the nav removed: a PURELY structural
// edit (one shell segment gone), not a word of prose touched. It is the shape
// the merge cases need — a restructure whose only quarrel with the live
// document is the shape, so what the round's words do is unambiguous.
func navlessPage(t *testing.T) string {
	t.Helper()
	out := strings.Replace(fixturePage, "<nav class=\"top\"><a href=\"/\">home</a></nav>\n", "", 1)
	if out == fixturePage {
		t.Fatal("precondition: the structural edit changed nothing")
	}
	return out
}

// liveMarkdown is the live document as markdown — what the reviewer's editor
// is showing, read out of the CRDT rather than off disk.
func liveMarkdown(t *testing.T, s *EditServer) []byte {
	t.Helper()
	model, err := ydoc.ReadLive(s.doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	return markdown.Serialize(model)
}

// TestStructuralRoundReloadsLiveDoc is the round-boundary handoff of the spec's
// "Reconciling back to the live editor": on a structural round the re-extracted
// content BECOMES the authority, so it must land in the LIVE document — the
// CRDT the reviewer's browser is bound to — and not merely on disk.
func TestStructuralRoundReloadsLiveDoc(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	if !strings.Contains(string(liveMarkdown(t, s)), "When the round comes back") {
		t.Fatal("precondition: the live document should hold the paragraph the agent is about to remove")
	}

	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}

	md, changed := s.pageRender.lastExtract()
	if !changed {
		t.Fatalf("structural round reported changed = false, want true")
	}
	live := liveMarkdown(t, s)
	if strings.Contains(string(live), "When the round comes back") {
		t.Errorf("the live document still holds the removed paragraph — the reload did not land:\n%s", live)
	}
	if !bytes.Equal(live, md) {
		t.Errorf("the live document is not the re-extracted content:\n--- live ---\n%s\n--- re-extracted ---\n%s", live, md)
	}
}

// TestReloadDoesNotRechurn is the feedback-loop guard. The reload is itself a
// server write, so it projects content.md, which re-renders/re-extracts the
// page — and that must NOT reload again. Once, exactly once, and the follow-on
// projection reports no change.
func TestReloadDoesNotRechurn(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	r := s.pageRender
	if n := r.reloadCount(); n != 0 {
		t.Fatalf("a content round reloaded the editor %d times, want 0", n)
	}

	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}
	if n := r.reloadCount(); n != 1 {
		t.Fatalf("structural round reloaded %d times, want exactly 1", n)
	}

	// The reload's own projection: the live document now holds the extracted
	// markdown, so re-extraction settles on the same bytes and nothing reloads.
	if err := s.Project(); err != nil {
		t.Fatalf("Project (after the reload): %v", err)
	}
	if _, changed := r.lastExtract(); changed {
		t.Errorf("the projection after a reload reported changed = true, want false — the loop has not settled")
	}
	if n := r.reloadCount(); n != 1 {
		t.Fatalf("the reload re-churned: %d reloads, want exactly 1", n)
	}

	// And a third turn of the loop, in case the second only looked settled.
	if err := s.Project(); err != nil {
		t.Fatalf("Project (third): %v", err)
	}
	if n := r.reloadCount(); n != 1 {
		t.Fatalf("the reload re-churned on a third projection: %d reloads, want exactly 1", n)
	}
}

// TestReloadSettlesWhenTheTwoSidesSpellItDifferently is the churn the reload
// nearly shipped with. The extractor and galley's serializer spell the same
// prose differently — `&`, `_` and `*` come out of an extraction escaped and
// out of a projection plain — so a page carrying any of them compared unequal
// on EVERY round: measured before the fix, six projections after one structural
// edit gave six reloads, with each reload scheduling the next projection. The
// signal must be about the document, not its spelling.
func TestReloadSettlesWhenTheTwoSidesSpellItDifferently(t *testing.T) {
	for name, prose := range map[string]string{
		"ampersand":  "a &amp; b, plainly",
		"underscore": "snake_case_word here",
		"asterisk":   "2 * 3 * 4 equals 24",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			page := writePage(t, dir, "page.html", fixturePage)

			s, err := NewEditPage(page)
			if err != nil {
				t.Fatalf("NewEditPage: %v", err)
			}
			defer func() { _ = s.Close() }()

			if err := s.Project(); err != nil {
				t.Fatalf("Project (baseline): %v", err)
			}

			// The agent restructures AND writes new prose — new because prose the
			// projection has seen before keeps its own spelling, so only prose the
			// page introduces can disagree with the serializer.
			structural := strings.Replace(structuralPage(t), "</body>", "<p>"+prose+"</p>\n</body>", 1)
			if err := os.WriteFile(page, []byte(structural), 0o644); err != nil {
				t.Fatal(err)
			}

			for i := 0; i < 6; i++ {
				if err := s.Project(); err != nil {
					t.Fatalf("Project %d: %v", i, err)
				}
			}
			if n := s.pageRender.reloadCount(); n != 1 {
				t.Errorf("reloads = %d over six projections, want exactly 1 — the loop is churning", n)
			}
			if _, changed := s.pageRender.lastExtract(); changed {
				t.Errorf("the round never settled: changed = true with nobody editing")
			}
		})
	}
}

// TestReloadGuardSuppressesASecondReloadOfTheSameContent drives the guard
// itself, with a projection that reports the page as different AFTER the reload
// on it has already landed — the shape a re-extraction that never settles would
// have. Content the live document was already reloaded on is never reloaded
// again, so the loop terminates whether or not the round-trip settles.
func TestReloadGuardSuppressesASecondReloadOfTheSameContent(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Fatalf("structural round reloaded %d times, want exactly 1", n)
	}
	live := liveMarkdown(t, s)

	// Three more re-extractions of the same page, each told the projection
	// carried something else entirely: changed stays true and the reload stays
	// spent.
	for i := 0; i < 3; i++ {
		s.pageRender.reextract([]byte("Something else entirely.\n"), s.pageRender.generation(), false, true)
		if _, changed := s.pageRender.lastExtract(); !changed {
			t.Fatalf("precondition %d: this projection should report changed = true", i)
		}
		if n := s.pageRender.reloadCount(); n != 1 {
			t.Fatalf("reloads = %d after %d unsettled re-extractions, want exactly 1", n, i+1)
		}
	}
	if got := liveMarkdown(t, s); !bytes.Equal(got, live) {
		t.Errorf("the live document moved under a suppressed reload:\n--- was ---\n%s\n--- now ---\n%s", live, got)
	}
}

// TestReloadWaitsForTheRoundBoundary: while the agent holds the file, its
// content edits are reaching the live document through the watcher's imports,
// and reloading on the page it is restructuring in the same round would throw
// them away. The signal is kept and spent on the projection that ends the
// round — which is where the spec puts the handoff back to the editor.
func TestReloadWaitsForTheRoundBoundary(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	// The round is handed over, and the agent restructures the page while it
	// holds the file.
	s.openHandoff(1, "fp", false)
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (mid-window): %v", err)
	}
	if _, changed := s.pageRender.lastExtract(); !changed {
		t.Errorf("mid-window re-extract reported changed = false — the signal must be kept, not dropped")
	}
	if n := s.pageRender.reloadCount(); n != 0 {
		t.Fatalf("reloaded %d times mid-window, want 0 — the .md is the agent's until it returns", n)
	}

	// The agent returns: the window closes and the round's projection is where
	// the reload belongs.
	s.closeHandoff()
	if err := s.Project(); err != nil {
		t.Fatalf("Project (boundary): %v", err)
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Fatalf("reloads at the round boundary = %d, want exactly 1", n)
	}
	if live := liveMarkdown(t, s); strings.Contains(string(live), "When the round comes back") {
		t.Errorf("the live document still holds the removed paragraph:\n%s", live)
	}
}

// TestAStructuralOnlyRoundStillReachesTheBoundary drives the REAL round close —
// the reviewer's press, the agent answering in page.html ALONE, and `galley ack
// --state answered` over the wire — because that is the round the feature was
// built for and the one path every test around it manufactures. They call
// Project() by hand, which produces a boundary the product never produces here:
// a structural-only round moves nothing in the document, so nothing projects,
// so the pipeline the reload hangs off never runs (B2). And the ack that should
// have started it was refused outright, because the gate on `answered` asked
// only about content.md (B1) — leaving the handoff window open, the reviewer's
// editor read-only, and the drift sitting until the next keystroke, which
// merged it the wrong way round and poured the cut prose back (B3).
//
// So this test says the ack is taken, the reload fires ONCE at the boundary,
// both the live document and content.md lose the cut prose, no empty version is
// cut on top of the round, and the cut STAYS cut through the reviewer's next
// keystroke.
func TestAStructuralOnlyRoundStillReachesTheBoundary(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()
	// A revise with nothing listening is refused, and this test is about the
	// round's close rather than about who hears the send.
	s.OnRevise = "true"

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	const cut = "When the round comes back."
	if !strings.Contains(string(liveMarkdown(t, s)), cut) {
		t.Fatal("precondition: the live document should hold the paragraph the agent is about to remove")
	}

	// The reviewer asks for the removal and presses Revise: the file is the
	// agent's from here until it returns.
	commentAs(t, s, cut, "cut this paragraph, wrapper and all")
	press(t, s, "{}")
	if !s.handoffOpenNow() {
		t.Fatal("the press did not hand the file over — there is no round to close")
	}
	sent := len(rounds(t, s))

	// THE WHOLE ANSWER IS IN THE HTML. content.md is not touched, so nothing
	// about the document has moved when the agent returns — which is exactly what
	// the old ack gate read as "nothing happened".
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	ackAs(t, s, "answered", "cut the paragraph from the page")

	if s.handoffOpenNow() {
		t.Fatal("the window outlived the ack — the reviewer's editor stays read-only")
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Fatalf("the round closed with %d reloads, want exactly 1 — the ack is the boundary", n)
	}
	if live := liveMarkdown(t, s); strings.Contains(string(live), cut) {
		t.Errorf("the live document still holds the removed paragraph — the reload did not land:\n%s", live)
	}
	// content.md is the next round's model, and the projection the reload
	// schedules is what writes it — waited for rather than forced, so this is the
	// product settling on its own.
	if md := fileUntil(t, s.MdPath, func(b []byte) bool { return !bytes.Contains(b, []byte(cut)) }); strings.Contains(md, cut) {
		t.Errorf("content.md still holds the removed paragraph — the next round would hand it back:\n%s", md)
	}

	// THE VERSION SEQUENCE. The send cut the reviewer's round; the close must not
	// cut an empty one on top of it, and must not cut two.
	if got := len(rounds(t, s)) - sent; got != 0 {
		t.Errorf("the structural round cut %d version(s) past the send, want 0: %+v", got, rounds(t, s)[sent:])
	}

	// AND THE CUT STAYS CUT. The reviewer types again; the projection that
	// carries the keystroke must not be the one that finally processes the drift,
	// because by then the content has moved and the merge pours the cut prose
	// back into whatever slot is left.
	const now = "The reviewer highlights a different sentence."
	retext(t, s, "The reviewer highlights a sentence.", now)
	if err := s.Project(); err != nil {
		t.Fatalf("Project (the reviewer's next keystroke): %v", err)
	}
	if live := liveMarkdown(t, s); strings.Contains(string(live), cut) {
		t.Errorf("the reviewer's keystroke poured the cut paragraph back into the document:\n%s", live)
	}
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), cut) {
		t.Errorf("the cut paragraph came back to the page:\n%s", out)
	}
	if !strings.Contains(string(out), now) {
		t.Errorf("the reviewer's keystroke never reached the page — the loop stopped at the structural round:\n%s", out)
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Errorf("reloads = %d, want exactly 1 (the structural round's) — the keystroke reloaded again", n)
	}
}

// TestAPageModeRoundWithNothingChangedRefusesAnsweredAndLeavesTheWindowOpen is
// the page-mode twin of TestAnsweredRefusesAnUnparseableDraftAndAZeroChangeRound
// (handoff_test.go): it pins the negative side of the B1 ack gate — the fix
// that lets `answered` through on page-only drift (s.pageDrifted()) must not
// also let it through on a round where NOTHING happened, in EITHER layer.
// content.md is untouched (no import, no applied change) and page.html is
// untouched (no drift), so "answered" is a fabrication exactly as it is in
// markdown mode — refused with the same could-not guidance, window still open.
func TestAPageModeRoundWithNothingChangedRefusesAnsweredAndLeavesTheWindowOpen(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()
	s.OnRevise = "true"

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	commentAs(t, s, "When the round comes back.", "just take a look, no changes needed")
	press(t, s, "{}")
	if !s.handoffOpenNow() {
		t.Fatal("the press did not hand the file over — there is no round to close")
	}

	// THE AGENT DOES NOTHING: content.md is not touched and page.html is not
	// touched — the same "nothing happened" the markdown gate refuses.
	rec := postAckRec(t, s, "answered", "looked, nothing to change")
	if rec.Code != http.StatusConflict {
		t.Fatalf("zero-change answered in page mode: got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "galley cannot") {
		t.Errorf("refusal body does not point at `galley cannot`: %s", rec.Body.String())
	}
	if !s.handoffOpenNow() {
		t.Fatal("the refused ack closed the window — the reviewer's editor should still be read-only")
	}
}

// fileUntil reads path until it satisfies pred or a couple of seconds pass. The
// product's own debounced projection is what moves the file, so a test that
// asserts on content.md waits for it rather than forcing a projection of its
// own — the forcing is what hid B2.
func fileUntil(t *testing.T, path string, pred func([]byte) bool) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []byte
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			last = b
			if pred(b) {
				return string(b)
			}
		}
		if time.Now().After(deadline) {
			return string(last)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestContentRoundAfterStructuralRoundStillRenders is the loop CONTINUING, and
// the reload's sharpest edge: the structural round routed past render, so
// unless the page it re-extracted becomes the state galley knows, page.html
// stays drifted forever — every later projection re-extracts the same stale
// page instead of rendering into it, and the reviewer's next wording edit is
// reloaded away by content older than their keystroke. A wording round after a
// structural one must reach the page, and must not be reverted.
func TestContentRoundAfterStructuralRoundStillRenders(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}

	// The reviewer goes back to work on the reloaded document.
	const now = "The reviewer highlights a different sentence."
	retext(t, s, "The reviewer highlights a sentence.", now)
	if err := s.Project(); err != nil {
		t.Fatalf("Project (content): %v", err)
	}

	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), now) {
		t.Errorf("the content round never reached page.html — the loop stopped at the structural round:\n%s", out)
	}
	if strings.Contains(string(out), `<nav class="top">`) {
		t.Errorf("the agent's restructure was undone — the nav is back:\n%s", out)
	}
	if live := liveMarkdown(t, s); !strings.Contains(string(live), now) {
		t.Errorf("the reviewer's edit was reloaded away:\n%s", live)
	}
	if _, changed := s.pageRender.lastExtract(); changed {
		t.Errorf("the content round reported changed = true, want false — it should re-extract to its own content")
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Errorf("reloads = %d, want exactly 1 (the structural round's)", n)
	}
}

// TestStructuralRoundKeepsTheRoundsContentEdits is C1, the silent data loss the
// reload shipped with. THE AGENT MAY EDIT BOTH LAYERS IN ONE ROUND (the spec's
// "what each party touches"): the words through content.md, which the watcher
// imports into the live document, and the structure through page.html. A reload
// straight off the page REPLACES the live document with the page's own content,
// so every one of those imported words is thrown away at the boundary with
// nothing to re-import them from.
//
// The fix is the spec's own ordering — "render runs first, pouring the content
// edits into the current template; the agent's structural HTML edits come
// after" — so the round ends with the AGENT'S NEW STRUCTURE HOLDING THE ROUND'S
// WORDS, and the reload carries that merged document, not the page's stale one.
func TestStructuralRoundKeepsTheRoundsContentEdits(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	// The round is handed over, and the agent answers it in both layers.
	s.openHandoff(1, "fp", false)
	draft, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	const was, now = "The reviewer highlights a sentence.", "The reviewer highlights a different sentence."
	edited := bytes.Replace(draft, []byte(was), []byte(now), 1)
	if bytes.Equal(edited, draft) {
		t.Fatal("precondition: the agent's content edit changed nothing")
	}
	if err := os.WriteFile(s.MdPath, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.importDraft(); err != nil || !ok {
		t.Fatalf("importDraft: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(page, []byte(navlessPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (mid-window): %v", err)
	}

	// The agent returns: the boundary is where the reload fires.
	s.closeHandoff()
	if err := s.Project(); err != nil {
		t.Fatalf("Project (boundary): %v", err)
	}

	live := liveMarkdown(t, s)
	if !strings.Contains(string(live), now) {
		t.Errorf("the boundary reload discarded the agent's content edit — it is gone from the live document:\n%s", live)
	}
	if strings.Contains(string(live), "⟦ shell 1 ⟧") {
		t.Errorf("the live document still stands in for the removed nav — the restructure did not reach it:\n%s", live)
	}
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), now) {
		t.Errorf("page.html does not carry the round's words:\n%s", out)
	}
	if strings.Contains(string(out), `<nav class="top">`) {
		t.Errorf("the agent's restructure was undone — the nav is back:\n%s", out)
	}
}

// TestAWindowClosingMidRoundCannotSkipTheMerge is C-A, C1 by a TOCTOU door.
// The handoff window used to be read TWICE per structural round — once by the
// merge and again by the re-extraction — with a full ReadFile and Extract of
// the page in between. An ordinary sequence lands in that gap: a debounced
// mid-window projection is in flight when the agent acks. The merge saw the
// window OPEN and deferred to the boundary, the re-extraction saw it CLOSED and
// reloaded the live document off the UN-MERGED page, and the round's content
// edits were gone with nothing to recover them from.
//
// The window is now read ONCE per projection and threaded, so both steps defer
// together or act together. The close here is driven through the midRound seam
// so the race is deterministic rather than timing-dependent.
func TestAWindowClosingMidRoundCannotSkipTheMerge(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	// The round is handed over and the agent answers in both layers: the words
	// through content.md (imported into the live document) and the structure
	// through page.html.
	s.openHandoff(1, "fp", false)
	draft, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	const was, now = "The reviewer highlights a sentence.", "The reviewer highlights a different sentence."
	edited := bytes.Replace(draft, []byte(was), []byte(now), 1)
	if bytes.Equal(edited, draft) {
		t.Fatal("precondition: the agent's content edit changed nothing")
	}
	if err := os.WriteFile(s.MdPath, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.importDraft(); err != nil || !ok {
		t.Fatalf("importDraft: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(page, []byte(navlessPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}

	// THE RACE: the agent's ack lands between the merge and the re-extraction of
	// the mid-window projection.
	var once sync.Once
	s.pageRender.mu.Lock()
	s.pageRender.midRound = func() { once.Do(s.closeHandoff) }
	s.pageRender.mu.Unlock()
	if err := s.Project(); err != nil {
		t.Fatalf("Project (mid-window): %v", err)
	}
	s.pageRender.mu.Lock()
	s.pageRender.midRound = nil
	s.pageRender.mu.Unlock()
	if s.handoffOpenNow() {
		t.Fatal("precondition: the seam never closed the window inside the round")
	}

	// Closing cuts, and the cut projects: that projection is the round boundary
	// where the merge and the reload belong.
	if err := s.Project(); err != nil {
		t.Fatalf("Project (boundary): %v", err)
	}

	live := liveMarkdown(t, s)
	if !strings.Contains(string(live), now) {
		t.Errorf("the window closed between the merge and the re-extraction and the round's content edit "+
			"was reloaded away:\n%s", live)
	}
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), now) {
		t.Errorf("page.html does not carry the round's words:\n%s", out)
	}
	if strings.Contains(string(out), `<nav class="top">`) {
		t.Errorf("the agent's restructure was undone — the nav is back:\n%s", out)
	}
}

// TestOutOfBandPageEditKeepsTheReviewersKeystrokes is I1, C1's other face: the
// reviewer is typing (no handoff window, the browser is authoritative) when
// page.html moves under them. The drift routes past render, and a reload off
// the page replaces the live document with content OLDER than the keystrokes —
// silent loss in the ordinary out-of-window case. The words are the reviewer's;
// only the structure is the page's.
func TestOutOfBandPageEditKeepsTheReviewersKeystrokes(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}

	const now = "The reviewer highlights a different sentence."
	retext(t, s, "The reviewer highlights a sentence.", now)
	if err := os.WriteFile(page, []byte(navlessPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}

	if live := liveMarkdown(t, s); !strings.Contains(string(live), now) {
		t.Errorf("the reviewer's keystrokes were reloaded away by an out-of-band page edit:\n%s", live)
	}
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), now) {
		t.Errorf("page.html never received the reviewer's keystrokes:\n%s", out)
	}
	if strings.Contains(templateShell(s.pageRender.template), `<nav class="top">`) {
		t.Errorf("the template still holds the removed nav — the restructure was lost")
	}
}

// TestRestructureThatCannotHoldTheContentRefusesLoudly is the merge's refusal:
// a restructure whose new shape cannot carry what this round says (here a math
// block, which no page slot can pour) must NOT fall back to reloading the page
// over the document — that is the silent loss again, by another door. It is a
// could-not round, both sides are left exactly as they stand, and the words
// survive in the document and in the round's own version.
func TestRestructureThatCannotHoldTheContentRefusesLoudly(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	base := countCouldNot(t, s)

	setMathBlock(t, s, true, "A document two parties revise, with a formula")
	structural := navlessPage(t)
	if err := os.WriteFile(page, []byte(structural), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}

	if got := countCouldNot(t, s) - base; got != 1 {
		t.Errorf("a restructure that cannot hold the round's content: want 1 could-not round, got %d — it was discarded silently", got)
	}
	if n := s.pageRender.reloadCount(); n != 0 {
		t.Errorf("reloaded %d times over a refused merge, want 0 — the reload would replace the round's content", n)
	}
	live := liveMarkdown(t, s)
	if !strings.Contains(string(live), "E = mc^2") {
		t.Errorf("the live document lost the content the merge could not carry:\n%s", live)
	}
	if !strings.Contains(string(live), "with a formula") {
		t.Errorf("the live document lost this round's other words:\n%s", live)
	}
	out, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != structural {
		t.Errorf("page.html was written over a refusal — the agent's restructure must stand:\n%s", out)
	}
}

// TestOverlappingProjectionsReloadOnce is I2: afterProject runs with the server
// mutex released, so two projections genuinely overlap here. The feedback-loop
// guard is only a guard if the claim is staked in the SAME critical section as
// the decision it guards — staked afterwards, every overlapping re-extraction
// decides to reload and every one of them does it, each a full replacement of
// the document the reviewer is bound to.
func TestOverlappingProjectionsReloadOnce(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	// What a projection carries: the content.md this server just wrote.
	projected, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}

	// render is what afterProject calls, and it is called with the server mutex
	// released — so these are eight projections of the same drifted page landing
	// on top of each other, which a debounce refiring under a slow render is.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.pageRender.render(projected)
		}()
	}
	wg.Wait()

	if n := s.pageRender.reloadCount(); n != 1 {
		t.Errorf("eight overlapping projections of one drifted page reloaded %d times, want exactly 1", n)
	}
}

// TestAStructuralReloadHasNoPendingInstructionsToLose is I3, and it pins an
// ASSUMPTION rather than a mechanism. The reload replaces the whole model, so
// every live instruction mark goes with it — the spec's "anchors within a
// round" says that is safe because "the round's instructions are discharged
// before the structure changes", and nothing asserted it. This is that
// assertion, and the load-bearing line is the one AFTER THE SEND: the rail is
// already empty when the agent is handed the round, so the reload that ends it
// has nothing to lose.
//
// It is load-bearing rather than decorative: measured while writing this, an
// instruction filed and NOT sent is simply gone from the pending set after the
// structural round — the reload dropped it, silently, leaving exactly the same
// empty rail a discharge leaves. Nothing here can tell those two apart
// afterwards, which is why the assertion is placed at the send. Cross-reload
// mark preservation is deliberately not built: Task 5's gate is where a live
// thread surviving a fragment reload becomes visible.
func TestAStructuralReloadHasNoPendingInstructionsToLose(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()
	// A revise with nothing listening is refused, and this test is about what the
	// send does to the review rather than about who hears it.
	s.OnRevise = "true"

	if err := s.Project(); err != nil {
		t.Fatalf("Project (baseline): %v", err)
	}
	if rec := postRec(t, s, "/_galley/instruct", map[string]any{
		"op": "comment", "target": "The reviewer highlights a sentence.", "text": "cut this section",
	}); rec.Code >= 300 {
		t.Fatalf("instruct: %d %s", rec.Code, rec.Body.String())
	}
	before, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Instructions) != 1 {
		t.Fatalf("the fixture filed %d instructions, not 1 — it cannot show the discharge", len(before.Instructions))
	}

	if rec := postRec(t, s, "/_galley/revise", map[string]any{}); rec.Code >= 300 && rec.Code != http.StatusConflict {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	sent, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.Instructions) != 0 {
		t.Fatalf("%d instruction(s) are still live when the agent is handed the round: %+v — "+
			"the boundary reload replaces the whole model and will drop them",
			len(sent.Instructions), sent.Instructions)
	}

	// The agent answers in the HTML and returns the file; the round boundary is
	// the projection that closing the window cuts, and that is where the reload
	// replaces the model.
	if err := os.WriteFile(page, []byte(structuralPage(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	s.closeHandoff()
	if err := s.Project(); err != nil {
		t.Fatalf("Project (structural): %v", err)
	}
	if n := s.pageRender.reloadCount(); n != 1 {
		t.Fatalf("structural round reloaded %d times, want exactly 1", n)
	}

	after, err := s.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Instructions) != 0 {
		t.Errorf("%d instruction(s) were live when the reload replaced the model: %+v — the whole-model reload drops them",
			len(after.Instructions), after.Instructions)
	}
}

// countCouldNot returns how many could-not rounds the server has cut.
func countCouldNot(t *testing.T, s *EditServer) int {
	t.Helper()
	n := 0
	for _, r := range rounds(t, s) {
		if r.Reason == versions.ReasonCouldNot {
			n++
		}
	}
	return n
}

// setMathBlock drops any math block in the live document and, when present is
// true, appends one — a block a page cannot carry, so the next projection's
// render refuses. It also retexts the eyebrow to a fresh value so each call is
// a real change and a projection actually fires.
func setMathBlock(t *testing.T, s *EditServer, present bool, eyebrow string) {
	t.Helper()
	if _, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		out := docmodel.Doc{}
		for _, b := range model.Blocks {
			if b.Kind == docmodel.MathBlock {
				continue
			}
			var text string
			for _, in := range b.Inlines {
				text += in.Text
			}
			if b.Kind == docmodel.Paragraph && strings.HasPrefix(text, "A document two parties revise") {
				b.Inlines = []docmodel.Inline{{Text: eyebrow}}
			}
			out.Blocks = append(out.Blocks, b)
		}
		if present {
			out.Blocks = append(out.Blocks, docmodel.Block{
				Kind: docmodel.MathBlock,
				Text: "$$\nE = mc^2\n$$\n",
			})
		}
		return out, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestRenderRefusalDedupes covers I2: a render refusal cuts a could-not round
// only when its reason differs from the last one recorded, and a successful
// render clears the memo so the next refusal is reported again. The bad content
// lives in the document (a math block a page cannot carry), so every projection
// re-derives the same refusal — exactly the typing-through-invalid-state case.
func TestRenderRefusalDedupes(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	base := countCouldNot(t, s)

	// A math block enters the document: the projection's render refuses.
	setMathBlock(t, s, true, "A document two parties revise, take one")
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := countCouldNot(t, s) - base; got != 1 {
		t.Fatalf("first refusal: want 1 could-not round, got %d", got)
	}

	// The document changes again but still carries the math block: same refusal,
	// no new round.
	setMathBlock(t, s, true, "A document two parties revise, take two")
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := countCouldNot(t, s) - base; got != 1 {
		t.Fatalf("repeated identical refusal: want still 1 could-not round, got %d", got)
	}

	// The math block leaves: the render succeeds and clears the memo.
	setMathBlock(t, s, false, "A document two parties revise, take three")
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := countCouldNot(t, s) - base; got != 1 {
		t.Fatalf("after a successful render: want still 1 could-not round, got %d", got)
	}

	// The same refusal returns: with the memo cleared it reports again.
	setMathBlock(t, s, true, "A document two parties revise, take four")
	if err := s.Project(); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := countCouldNot(t, s) - base; got != 2 {
		t.Fatalf("refusal after a successful render: want 2 could-not rounds, got %d", got)
	}
}

// TestConcurrentProjectIsRaceFree covers M2: two goroutines projecting a
// page-backed server at once must not race on the renderer's fields. Run under
// -race to catch a regression.
func TestConcurrentProjectIsRaceFree(t *testing.T) {
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)

	s, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = s.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Project()
		}()
	}
	wg.Wait()
}

// THE ADVERT NAMES THE PROSE, NOT THE PAGE. An editor started on page.html
// serves .galley/pages/page/content.md and advertises that, so anything
// resolving an advert has to ask for the same transformation rather than
// rebuilding it — which is how galley_open came to search for a path no
// editor ever publishes. Case-insensitive, and .md (and everything else)
// passes through untouched.
func TestAdvertisedPathNamesTheProseAnEditorServes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/w/doc.md", "/w/doc.md"},
		{"/w/notes.txt", "/w/notes.txt"},
		{"/w/page.html", "/w/.galley/pages/page/content.md"},
		{"/w/page.htm", "/w/.galley/pages/page/content.md"},
		{"/w/PAGE.HTML", "/w/.galley/pages/PAGE/content.md"},
		{"/w/sub/index.html", "/w/sub/.galley/pages/index/content.md"},
	} {
		if got := AdvertisedPath(tc.in); got != tc.want {
			t.Errorf("AdvertisedPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// And the constructor derives its working directory from that same function:
// a real page-mode editor's MdPath is exactly what AdvertisedPath predicted,
// so the two can never drift.
func TestPageModeServesTheAdvertisedPath(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "post.html")
	if err := os.WriteFile(page, []byte("<html><head><title>T</title></head><body><p>Hello.</p></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewEditPage(page)
	if err != nil {
		t.Fatalf("NewEditPage: %v", err)
	}
	defer func() { _ = srv.Close() }()
	if got, want := srv.MdPath, AdvertisedPath(page); got != want {
		t.Errorf("MdPath = %q, want the advertised path %q", got, want)
	}
}
