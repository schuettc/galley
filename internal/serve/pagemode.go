package serve

// pagemode.go is the page-backed edit server: `galley edit page.html` splits
// the page into the prose markdown can carry and the shell it cannot (see
// internal/htmlpage), opens the normal markdown editor on the derived
// content.md, and re-renders page.html from template + content after every
// projection. To every existing surface this is a markdown review whose file
// happens to be derived — there is no second document model. The seam is one
// field on EditServer (afterProject) and this constructor.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/htmlpage"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/ydoc"
)

// pageStderr is the always-on channel for the page renderer's safety messages
// (drift refusal, render-write failure). These must reach the reviewer even on
// the plain `galley edit page.html` path, where EditServer.Log is nil because
// no --on-revise hook was wired (cmd/galley/edit.go). The spec's "Refusals and
// safety" requires the reviewer be told the page moved under them, so we speak
// directly to stderr rather than through the optional Log hook. It is a var so
// tests can capture what is written.
var pageStderr io.Writer = os.Stderr

// NewEditPage opens page.html for review: extract into .galley/pages/<base>/,
// open the normal markdown editor on content.md, and re-render the page after
// every projection.
func NewEditPage(htmlPath string) (*EditServer, error) {
	return newEditPage(htmlPath, "")
}

// NewEditPageRoot is NewEditPage with an explicit site root. The preview serves
// the page's assets from siteRoot — a kernel-contained os.Root, exactly like
// the default — instead of from the page's own directory, and mounts the page
// in the preview at its path relative to siteRoot. That lets a multi-page site
// whose subpages reference shared assets one directory up (../style.css) render
// in the preview as it does when the whole tree is served from the site root,
// without copying the assets beside every subpage.
//
// siteRoot must contain the page: if the page is not under it, construction
// fails rather than silently falling back to the page's directory. The
// containment boundary is still one os.Root with the same no-symlink, allowlist
// and same-origin guarantees — it just sits at siteRoot instead of the page's
// own folder, a directory the caller named on purpose.
func NewEditPageRoot(htmlPath, siteRoot string) (*EditServer, error) {
	return newEditPage(htmlPath, siteRoot)
}

// newEditPage is the shared constructor. siteRoot == "" keeps the original
// behaviour: the preview root is the page's own directory and the page is
// mounted at its basename. A non-empty siteRoot must be an ancestor of the
// page; the preview root becomes siteRoot and the page is mounted at its
// path relative to it.
func newEditPage(htmlPath, siteRoot string) (*EditServer, error) {
	abs, err := filepath.Abs(htmlPath)
	if err != nil {
		return nil, err
	}

	// Where the preview's os.Root opens, and the URL name the page is mounted
	// under. By default both derive from the page's own directory. With an
	// explicit site root, the root opens there and the page is mounted at its
	// path relative to it, so the page's own ../asset references resolve inside
	// the same contained root.
	rootDir := filepath.Dir(abs)
	previewRel := filepath.Base(abs)
	if siteRoot != "" {
		absRoot, err := filepath.Abs(siteRoot)
		if err != nil {
			return nil, fmt.Errorf("page: --root: %w", err)
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil {
			return nil, fmt.Errorf("page: --root %q does not contain %q: %w", siteRoot, htmlPath, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("page: --root %q does not contain %q", siteRoot, htmlPath)
		}
		rootDir = absRoot
		previewRel = filepath.ToSlash(rel)
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("page: %w", err)
	}
	// Extract errors are already author-facing (element and line, the author's
	// spelling) — pass them straight through.
	ex, err := htmlpage.Extract(src)
	if err != nil {
		return nil, err
	}

	// One source of truth for where a page's prose lives: AdvertisedPath is
	// what the editor will advertise for this input, and the working directory
	// is that file's own. A caller resolving an advert (galley_open) asks the
	// same function rather than rebuilding the path beside it.
	dir := filepath.Dir(AdvertisedPath(abs))
	contentPath, original, err := writePageWorkdir(dir, src, ex)
	if err != nil {
		return nil, err
	}

	es, err := NewEdit(contentPath)
	if err != nil {
		return nil, err
	}

	// Retain the page and a root to serve it from: a later task streams the
	// page to a preview iframe through pageRoot, so the preview root (the page's
	// directory, or the named site root) must be readable now. Failing to open
	// it here fails construction rather than surfacing later as an unservable
	// preview.
	pr, err := os.OpenRoot(rootDir)
	if err != nil {
		// es is fully live (open s.root, running yjs room, Notify, OnUpdate).
		// Close it before abandoning it, or the content-dir root and yjs server
		// leak. es.Close releases s.root/yjs/Notify — the same Close that closes
		// pageRoot.
		_ = es.Close()
		return nil, fmt.Errorf("page: open dir: %w", err)
	}
	es.pageMode = true
	es.pagePath = abs
	es.pageRoot = pr
	es.previewRel = previewRel

	r := &pageRenderer{
		es:       es,
		pagePath: abs,
		template: ex.Template,
		// The page and the live document agree at open, on this content: it is
		// what a later round's content edits are measured against (see merge).
		converged:    ex.Markdown,
		originalHash: sha256.Sum256(original),
		// lastWritten starts as the page as opened: the first projection sees
		// the same bytes and writes over them cleanly. Thereafter it is this
		// server's own last render.
		lastWritten: sha256.Sum256(src),
	}
	es.afterProject = r.render
	es.pageRender = r
	return es, nil
}

// pageDrifted reports whether this is a page review whose page.html has moved
// since galley last wrote it — the agent's structural answer, sitting on disk
// with nothing in the document to show for it. A markdown review has no
// renderer and is always false, which is what keeps markdown mode's behaviour
// identical around the two callers in editmode.go.
//
// It takes the renderer's own lock, because unlike drifted's other callers this
// one is outside a projection. Callers must NOT hold mu (they may: this takes
// no server lock).
func (s *EditServer) pageDrifted() bool {
	r := s.pageRender
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drifted()
}

// pageBoundary runs the round's pipeline over a structural answer that no
// projection would otherwise carry. Callers must NOT hold mu.
//
// THE RELOAD RIDES A PROJECTION, AND A STRUCTURAL-ONLY ROUND HAS NONE. render/
// restructured/reextract/reload all hang off afterProject, and a round whose
// whole answer is in page.html moves nothing in the document: nothing was
// imported, so cutApplied cuts nothing, so nothing projects. The drift then sat
// until the next thing that did project — the reviewer's own next keystroke —
// and by then the round's content HAS moved, so merge took its "both layers
// moved" branch and poured the prose the agent had cut straight back in.
//
// The round's close is where the spec puts this ("round commit → render →
// re-extract → push the new content.md to the browser"), so the projection is
// forced HERE, at the boundary, when the page has drifted and nothing else has
// carried it. With no content edits of its own, projected == converged, so the
// merge leaves the page the whole authority and the cut stays cut.
//
// It asks about the drift first so an ordinary round pays a file read and
// nothing else: a round that DID move the document has already projected
// through cutApplied, and that projection adopted the page — no drift, no
// second projection, no double reload.
func (s *EditServer) pageBoundary() {
	if !s.pageDrifted() {
		return
	}
	if err := s.Project(); err != nil && s.Log != nil {
		s.Log("could not re-extract the restructured page at the round boundary: " + err.Error())
	}
}

// AdvertisedPath is the path an editor started on abs will ADVERTISE — which
// is not the path it was started with whenever abs is an HTML page: page mode
// opens the markdown editor on the prose it extracts, so the advert (and the
// registry entry, and every wake) names .galley/pages/<base>/content.md.
//
// It exists because a caller that resolves adverts (galley_open) matched on
// the path it was GIVEN and so could never find the editor it had just
// spawned for a page. The extension switch is routeEdit's, deliberately: the
// two must agree about what "an HTML page" is, and one of them has to say so.
// Pure — it touches no disk and creates nothing.
func AdvertisedPath(abs string) string {
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm":
		return filepath.Join(filepath.Dir(abs), ".galley", "pages", pageBase(abs), "content.md")
	default:
		return abs
	}
}

// pageBase is the page's name without its extension — the working directory
// name under .galley/pages/.
func pageBase(abs string) string {
	return strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
}

// writePageWorkdir lays out .galley/pages/<base>/: the round-zero cover
// (original.html), content.md and template.json. It returns the path to
// content.md and the original page bytes the drift check measures against.
func writePageWorkdir(dir string, src []byte, ex *htmlpage.Extraction) (contentPath string, original []byte, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}

	// Round zero cover: create original.html iff it does not already exist, so a
	// second session in the same directory never overwrites an earlier one's
	// original (spec: "Refusals and safety" — round zero is recoverable). The
	// O_CREATE|O_EXCL open is the atomic guard: two sessions racing to open the
	// same directory cannot both win the create, and the loser reads what the
	// winner wrote as the drift baseline (a plain ReadFile-then-WriteFile would
	// let both create it — TOCTOU).
	originalPath := filepath.Join(dir, "original.html")
	original = src
	f, oerr := os.OpenFile(originalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	switch {
	case oerr == nil:
		_, werr := f.Write(src)
		cerr := f.Close()
		if werr != nil {
			return "", nil, werr
		}
		if cerr != nil {
			return "", nil, cerr
		}
	case errors.Is(oerr, os.ErrExist):
		existing, rerr := os.ReadFile(originalPath)
		if rerr != nil {
			return "", nil, rerr
		}
		original = existing
	default:
		return "", nil, oerr
	}

	contentPath = filepath.Join(dir, "content.md")
	if err := os.WriteFile(contentPath, ex.Markdown, 0o644); err != nil {
		return "", nil, err
	}
	// template.json is regenerated at open — overwrite it.
	tmplJSON, err := json.Marshal(ex.Template)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "template.json"), tmplJSON, 0o644); err != nil {
		return "", nil, err
	}
	return contentPath, original, nil
}

// pageRenderer re-renders page.html from the template after each projection and
// then re-extracts the finished page, so THE TEMPLATE RIDES THE LOOP instead of
// being frozen at open (spec: "HTML pages — the template rides the loop"). It
// refuses a render that will not pour back, and refuses a page it can no longer
// read as HTML.
type pageRenderer struct {
	es       *EditServer
	pagePath string

	// mu guards the read-modify-write of template/lastWritten/lastCannot/
	// extracted/changed/reloaded/reloadGen/converged across a whole render.
	// template moved under the lock when it started being rewritten every round.
	// afterProject runs without the server mutex and a debounce can refire while
	// a render is in flight, so two renders can overlap; this is the renderer's
	// own lock, NOT the EditServer mutex (recordCannot takes that one, so sharing
	// it would deadlock).
	mu           sync.Mutex
	template     htmlpage.Template
	originalHash [32]byte
	lastWritten  [32]byte
	// lastCannot is the most recent render-refusal reason we cut a could-not
	// round for, or "" once a render has since succeeded. It dedupes the refusal
	// contract: an invalid intermediate state held while the reviewer types
	// should not spam a could-not round per keystroke (I2).
	lastCannot string

	// extracted/changed are the last re-extraction's result: the markdown the
	// page now yields, and whether it differs from the markdown the projection
	// carried. changed is NECESSARY for a reload but not sufficient: only the
	// DRIFT path reloads (see reextract's `structural`), because on the no-drift
	// path this difference is the round trip's own, not the agent's.
	extracted []byte
	changed   bool
	// reloaded is the markdown the live document was last reloaded ON, held
	// until the loop settles (a re-extraction that reports no change clears it).
	// It is the feedback-loop guard — see reextract.
	reloaded []byte
	reloads  int
	// reloadGen counts CLAIMED reloads (reloads counts landed ones). It is the
	// generation a render carries from the moment it starts, so a projection
	// overtaken by a reload can be told from a current one — see overtaken.
	reloadGen int
	// midRound is a TEST SEAM, nil in production: it runs between the merge and
	// the re-extraction — the gap a handoff close used to be able to land in
	// unseen (C-A). Read under mu so setting it cannot race a projection.
	midRound func()

	// converged is the content the page and the live document last both held —
	// the last round that settled. It is what tells a drift whether THIS round
	// carries content edits at all, which is the difference between a page that
	// is the whole authority and one that must be merged into. See merge.
	converged []byte
}

// reloadCount is how many times a structural round has pushed re-extracted
// content into the live document. The feedback-loop guard's evidence: a reload
// projects, and that projection re-extracts, so this must not climb per round.
func (r *pageRenderer) reloadCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloads
}

// generation is the reload generation a render starting now would carry — what
// render reads for itself, exposed for a caller entering the pipeline lower
// down (a test driving reextract directly).
func (r *pageRenderer) generation() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloadGen
}

// lastExtract reports the most recent re-extraction: the markdown page.html now
// yields, and whether that differs from the content the projection wrote.
func (r *pageRenderer) lastExtract() (md []byte, changed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.extracted, r.changed
}

// render is the afterProject hook: it receives the just-projected markdown with
// no server lock held. It guards its own fields with r.mu, but never holds that
// lock across recordCannot: recordCannot cuts a round, which re-enters render on
// the same goroutine, so holding r.mu there would self-deadlock. The dedupe
// memo is set under the lock BEFORE the call precisely so that re-entrant render
// sees the same reason and returns without cutting another round.
//
// TWO PATHS, CHOSEN BY DRIFT. No drift is a content round: render the reviewer's
// markdown into the current template, then re-extract the page we just wrote —
// only the TEMPLATE is refreshed from it, never the live document, because the
// live document is already the authority for every word that page holds. Drift
// is a structural round: the agent reworked page.html itself, so the TEMPLATE
// comes from the page as it stands rather than from this renderer's frozen
// copy, the re-extraction derives the next round's content from the finished
// page, and THAT is the only path that reloads the reviewer's editor. See
// restructured for what the round's own words do there.
func (r *pageRenderer) render(md []byte) {
	r.mu.Lock()
	tmpl, drift, gen := r.template, r.drifted(), r.reloadGen
	r.mu.Unlock()
	if drift {
		r.restructured(md, gen)
		return
	}

	out, err := htmlpage.Render(tmpl, md)
	if err != nil {
		// Reviewed markdown that will not pour back into its slot refuses via
		// cannot rather than writing a broken page. Render's error is already
		// an author-facing sentence.
		r.cannot(err.Error())
		return
	}
	r.mu.Lock()
	// Re-check under the lock: another projection may have written between the
	// drift read above and here, and the page must only be overwritten from a
	// state this renderer knows.
	if r.drifted() {
		r.mu.Unlock()
		r.restructured(md, gen)
		return
	}
	if r.overtaken(md, gen) {
		r.mu.Unlock()
		return
	}
	werr := os.WriteFile(r.pagePath, out, 0o644)
	if werr == nil {
		r.lastWritten = sha256.Sum256(out)
	}
	r.mu.Unlock()
	if werr != nil {
		_, _ = fmt.Fprintln(pageStderr, werr.Error())
		return
	}
	r.reextract(md, gen, r.es.handoffLive.Load(), false)
}

// overtaken reports whether this projection has been passed by a reload —
// whether the markdown it carries is older than the live document. Called with
// r.mu held, so it decides in the same critical section as the write it guards.
//
// A PROJECTION SPEAKS FOR THE DOCUMENT IT READ, AND ONLY UNTIL A RELOAD MOVES
// IT. afterProject runs with the server mutex released, so projections overlap:
// one arriving after a reload landed renders PRE-RELOAD content over the page,
// and the next re-extraction carries that straight back into the document —
// undoing the restructure that just landed. Measured before this guard: eight
// overlapping projections of one drifted page, a second reload carrying the
// pre-restructure content on 4 runs in 20. The write that overtook this one
// projects on its own, so the current document already has a render coming.
//
// Two questions, because neither alone is enough. The GENERATION catches a
// render that started before a reload was claimed and reached its write after
// (the claim is what moves it, and that happens before the write lands). The
// LIVE DOCUMENT catches a render that started after the claim but carries
// markdown a projection read before it — its generation is already current, so
// only the words give it away. A read that fails is not a reason to stop
// rendering (project's own rule one layer up).
func (r *pageRenderer) overtaken(md []byte, gen int) bool {
	if r.reloadGen != gen {
		return true
	}
	model, err := ydoc.ReadLive(r.es.doc)
	if err != nil {
		return false
	}
	// sameDocument, not bytes: a projection preserves the author's spelling
	// (markdown.SerializeOnto) and this rendering does not, so the two spell the
	// same document differently on every ordinary round.
	return !sameDocument(md, markdown.Serialize(model))
}

// cannot cuts a could-not round for a page the renderer will not touch, deduped:
// only when the reason differs from the last one recorded, so typing through an
// invalid intermediate state does not spam rounds (I2). Never called with r.mu
// held — recordCannot re-enters render on this goroutine.
func (r *pageRenderer) cannot(why string) {
	r.mu.Lock()
	novel := why != r.lastCannot
	if novel {
		r.lastCannot = why
	}
	r.mu.Unlock()
	if novel {
		r.es.recordCannot(why)
	}
}

// restructured is THE DRIFT PATH: page.html moved out of band, so the agent's
// structure stands — but the words this round carries are not the page's to
// throw away, and taking the page whole is what threw them away.
//
// THE SPEC'S OWN ORDERING IS THE FIX: "render runs first, pouring the content
// edits into the current template. The agent's structural HTML edits come after
// render, so render never clobbers them." Here that is one step: the round's
// content is rendered into the template just re-extracted FROM the drifted
// page, so the page ends up holding the AGENT'S NEW STRUCTURE AROUND THE
// ROUND'S WORDS, and the reload below fires only on what is left over — a
// structural change the content cannot express.
//
// The page is read and extracted twice on this path (once here for the
// refreshed template, once in reextract for what the merged page yields). That
// is the price of leaving reextract the single place the reload is decided, on
// the rarer of the two paths.
//
// THE HANDOFF WINDOW IS READ ONCE, HERE, AND THREADED like gen. Read twice —
// once by the merge and again by the re-extraction — a window CLOSING in the
// gap between them (a debounced mid-window projection still in flight when the
// agent acks, an ordinary sequence) made the merge defer to the boundary and
// the re-extraction reload off the UN-MERGED page, which is the silent loss of
// the round's words this path exists to prevent. One read means both steps
// defer together or act together; a window that moves under this projection is
// answered by the projection its transition cuts.
func (r *pageRenderer) restructured(projected []byte, gen int) {
	src, rerr := os.ReadFile(r.pagePath)
	if rerr != nil {
		_, _ = fmt.Fprintln(pageStderr, rerr.Error())
		return
	}
	ex, err := htmlpage.Extract(src)
	if err != nil {
		// A tear, not a restructure — the ordinary refusal, before anything is
		// written anywhere. See reextract.
		r.cannot(err.Error())
		return
	}
	window := r.es.handoffLive.Load()
	if !r.merge(ex, projected, gen, window) {
		return
	}
	r.mu.Lock()
	mid := r.midRound
	r.mu.Unlock()
	if mid != nil {
		mid()
	}
	r.reextract(projected, gen, window, true)
}

// merge pours this round's content into the restructured page, and reports
// whether the round may carry on to the re-extraction.
//
// WITHOUT IT THE ROUND'S CONTENT EDITS ARE SILENTLY RELOADED AWAY. The agent
// may answer in BOTH layers (spec: "what each party touches") — its content.md
// edits reach the live document through the watcher's imports — and the
// reviewer may be typing while the page moves out of band. A reload taken
// straight off the page replaces every one of those words with the page's own,
// at a boundary, with nothing anywhere to recover them from.
//
// Three states need no merge, and the first of them is the feature's headline
// case, so it is a deliberate ordering and not an optimisation:
//
//   - THE ROUND CARRIES NO CONTENT EDITS. The page is the whole authority, so a
//     section the agent cut in the HTML alone STAYS CUT (spec acceptance: "the
//     section gone from the rendered page — wrapper and all"). Rendering here
//     would pour the cut prose straight back in.
//   - THE PAGE ALREADY SAYS WHAT THE ROUND SAYS. Nothing to pour.
//   - A WINDOW IS OPEN (`window`, read once per projection by restructured and
//     threaded here so this and the reload it feeds cannot disagree about it —
//     see there). The .md is the agent's until it returns and the page may
//     still be half-restructured; the merge belongs to the projection that ENDS
//     the round, with the reload it feeds (see reextract).
//
// THE FIRST CASE HAS A PRICE, AND IT IS THE PRICE OF CUT-STAYS-CUT: a STALE
// AGENT PAGE-WRITE REVERTS SETTLED KEYSTROKES. A page written from content the
// agent read before the reviewer's last settled round is, from here, exactly a
// page whose author cut those words — the round carries no content edits of its
// own (projected == converged), so no merge happens and the reload takes the
// stale page's words. Telling the two apart needs a 3-way markdown merge, which
// is not built; the keystrokes are recoverable from the round's version IN LIVE
// MODE, where a settle wakes the agent and cuts a version — in on-ask mode
// there is no wake to cut one, so there may be no version to recover from.
//
// When both layers moved it is a genuine conflict and the words win, which is
// the only choice that cannot lose work — but it can pour back prose the agent
// cut in the HTML, so it says so rather than doing it quietly.
func (r *pageRenderer) merge(ex *htmlpage.Extraction, projected []byte, gen int, window bool) bool {
	r.mu.Lock()
	converged := r.converged
	r.mu.Unlock()
	if window || sameDocument(projected, converged) || sameDocument(projected, ex.Markdown) {
		return true
	}

	out, err := htmlpage.Render(ex.Template, projected)
	if err != nil {
		// THE RESTRUCTURE CANNOT HOLD WHAT THIS ROUND SAYS — a slot the content
		// needs is gone, or a block the new shape cannot pour. LOUD, NOT SILENT:
		// falling through to the reload would replace the round's content with
		// the page's, which is the loss this whole path exists to prevent. Both
		// sides are left exactly as they stand — the page keeps the restructure,
		// the live document keeps the words — and the round's version holds the
		// bytes. The drift is not adopted, so the next projection tries again;
		// cannot's dedupe is what stops that repeating as a round per attempt.
		r.cannot(fmt.Sprintf("%s was restructured, but this round's content edits cannot be poured into the "+
			"new structure (%v) — the page keeps the restructure and the document keeps the words",
			filepath.Base(r.pagePath), err))
		return false
	}
	// The write is under the lock, with the overtaken check: a projection a
	// reload has passed must not put its older words back on the page.
	//
	// lastWritten is deliberately NOT moved here: adopt records the page as the
	// state galley knows, and only once the round has converged. Discharging the
	// drift at this write would let the next projection render over a page whose
	// restructure the document has not taken in yet — and a re-extraction that
	// then refuses (a tear) would leave the drift adopted and unretried.
	r.mu.Lock()
	passed := r.overtaken(projected, gen)
	var werr error
	if !passed {
		werr = os.WriteFile(r.pagePath, out, 0o644)
	}
	r.mu.Unlock()
	if passed {
		return false
	}
	if werr != nil {
		_, _ = fmt.Fprintln(pageStderr, werr.Error())
		return false
	}
	if !sameDocument(ex.Markdown, converged) {
		// SAY WHAT HAPPENED, NOT MORE. This fires whenever the page's own content
		// moved as well as its shape, which includes a shell-only restructure (a
		// removed region renumbers every later stand-in) where no prose was poured
		// back at all. Claiming a conflict there cries wolf; the accurate claim in
		// both cases is that the document's words were kept.
		_, _ = fmt.Fprintf(pageStderr, "htmlpage: %s and its content both changed this round — the "+
			"document's words were kept\n", filepath.Base(r.pagePath))
	}
	return true
}

// reextract derives the next round's content model from page.html as it now
// stands, and is the whole point of the live loop: the template is refreshed
// from the finished page rather than frozen at open. `projected` is the markdown
// this round wrote to content.md; on the structural path anything else coming
// back out of the page is the agent's work, recorded as `changed` and reloaded
// into the reviewer's live editor at this round boundary (spec: "Reconciling
// back to the live editor"). Refusal is reserved for a genuine tear — a page
// that no longer reads as HTML, or an extraction galley's own markdown parser
// refuses (Extract validates that itself and says so) — never for an ordinary
// restructure.
//
// `structural` IS WHICH PATH CALLED, AND IT IS WHAT GATES THE RELOAD. The reload
// exists to push the agent's page.html edits into the reviewer's live editor, so
// it belongs to the drift path alone. On the no-drift path galley has just
// RENDERED the live document into the page: the editor is already authoritative
// for every word here, and a reload would replace the whole model — no `extra` —
// with a round-tripped RE-DERIVATION of itself.
//
// THAT IS NOT HYPOTHETICAL, AND `changed` ALONE CANNOT SEE IT. render→re-extract
// need not round-trip the content's spelling, and an instruction carrier the
// reviewer has just filed does not round-trip at all: {==quoted==} is poured
// into the page as ordinary prose and comes back with its markers gone, so an
// ORDINARY content round reads as changed and the reload dropped the reviewer's
// pending instruction on the floor.
// Measured by web/page.mjs (the instruction gone from the pending set the moment
// it was filed) and pinned by TestAContentRoundNeverReloadsTheLiveEditor.
func (r *pageRenderer) reextract(projected []byte, gen int, window, structural bool) {
	src, rerr := os.ReadFile(r.pagePath)
	if rerr != nil {
		_, _ = fmt.Fprintln(pageStderr, rerr.Error())
		return
	}
	ex, err := htmlpage.Extract(src)
	if err != nil {
		r.cannot(err.Error())
		return
	}
	// THE RELOAD IS A ROUND BOUNDARY, NOT A MOMENT DURING ONE. While a handoff
	// window is open the .md belongs to the agent (handoff.go): it is editing
	// both layers of this page, and its content edits reach the live document
	// through the watcher's imports. Reloading on the page mid-window would throw
	// those imports away and there would be nothing to re-import them from — the
	// file has not moved, so the watcher skips it. The signal is recorded below
	// and keeps: every close path cuts and therefore projects, so the reload
	// fires on the projection that ends the round, which is where the spec puts
	// it ("Reconciling back to the live editor"). `window` is READ ONCE PER
	// PROJECTION BY THE CALLER and threaded here, so the merge that feeds this
	// reload and this reload cannot disagree about it — see restructured. A
	// window that moves under this projection is answered by the projection its
	// transition cuts.

	r.mu.Lock()
	if r.reloadGen != gen {
		// A reload was claimed under this projection: its verdict is about a
		// document that has since moved, and acting on it reverts the round that
		// landed. (The generation only — not the live-document half of overtaken:
		// this is also the seam a test drives directly with markdown of its own.)
		r.mu.Unlock()
		return
	}
	r.template = ex.Template
	r.extracted = ex.Markdown
	r.changed = !sameDocument(ex.Markdown, projected)
	changed := r.changed
	if !changed {
		r.reloaded = nil
	}
	// THE FEEDBACK-LOOP GUARD. A reload is itself a server write, so it projects
	// content.md, which re-renders and re-extracts this page — arriving back
	// here. Idempotence should make that second pass report no change, but the
	// loop must terminate whether or not it does, so the reload is keyed on the
	// CONTENT it would load: markdown the live document was already reloaded on
	// is never reloaded again. The memo is discharged the moment the loop
	// settles (no change), so a later round that legitimately re-derives the
	// same content still reloads.
	//
	// THE CLAIM IS STAKED HERE, IN THE SAME CRITICAL SECTION AS THE DECISION IT
	// GUARDS. afterProject runs with the server mutex released, so projections
	// genuinely overlap; a claim staked one acquisition later lets every one of
	// them decide to reload and then do it, each a full replacement of the
	// document the reviewer is bound to. Measured that way: eight overlapping
	// projections of one drifted page, up to five reloads. reload RELEASES it if
	// nothing lands, so a refused write retries on the next projection.
	reload := structural && changed && !window && !bytes.Equal(ex.Markdown, r.reloaded)
	if reload {
		r.reloaded = ex.Markdown
		r.reloadGen++
	}
	// The page reads cleanly again: clear the refusal memo so the next refusal
	// is reported.
	r.lastCannot = ""
	r.mu.Unlock()

	if window {
		return
	}
	// A CONTENT ROUND ENDS HERE: the template has been refreshed and there is
	// nothing to push back, whatever `changed` said. It still adopts, or
	// `converged` stops tracking the rounds and the next restructure merges
	// against a stale baseline — but ONLY THE PAGE THIS RENDERER JUST WROTE. A
	// page that moved between the write above and the read here is the agent's
	// structural answer arriving in the gap, and adopting it would discharge the
	// drift with nothing reloaded; left alone, the next projection takes the
	// structural path and carries it properly.
	if !structural {
		r.mu.Lock()
		mine := sha256.Sum256(src) == r.lastWritten
		r.mu.Unlock()
		if mine {
			r.adopt(src, ex.Markdown)
		}
		return
	}
	// A CHANGE ALREADY RELOADED IS NOT RELOADED AGAIN, and the page is left as
	// it stands: the live document holds this content, so there is nothing to
	// carry either way.
	if changed && !reload {
		return
	}
	// A RELOAD THAT DID NOT LAND LEAVES THE PAGE THE AGENT'S. Adopting below
	// would let the next projection render the content this reload failed to
	// replace straight over the agent's restructured page.
	if reload && !r.reload(ex.Markdown) {
		return
	}
	r.adopt(src, ex.Markdown)
}

// adopt takes the page as it now stands to be the state galley knows, which is
// what discharges the drift the structural round was routed by.
//
// WITHOUT IT THE LOOP STOPS AFTER ONE STRUCTURAL ROUND. drifted() asks whether
// page.html moved since galley last wrote it, and the structural path
// deliberately does not write it — so the drift stands forever, every later
// projection re-extracts the same page instead of rendering into it, and the
// reviewer's next wording edit is not only never rendered but RELOADED AWAY by
// the stale content that page still yields. The page has just been re-extracted
// into this renderer's own template and (when it moved) into the live document,
// so it is galley's state now; the next content round renders into it normally.
//
// Called only once the round has converged: the content and the page agree, or
// the reload that made them agree has landed. `md` is what both now hold — the
// baseline the next drift measures that round's content edits against.
func (r *pageRenderer) adopt(src, md []byte) {
	r.mu.Lock()
	r.lastWritten = sha256.Sum256(src)
	r.converged = md
	r.mu.Unlock()
}

// reload makes the re-extracted content the authority: it replaces the LIVE
// document — the CRDT the reviewer's browser is bound to — with the markdown
// page.html now yields, so a structural round ends with the reviewer looking at
// the restructured document rather than a stale copy fighting the file (spec:
// "Reconciling back to the live editor").
//
// IT IS REACHED ONLY FROM THE DRIFT PATH (see reextract's `structural`): a
// content round has nothing structural to push and everything to lose here.
//
// IT IS THE RESTORE PATH, and deliberately nothing new: mutate reads a
// validated snapshot under the server mutex and writes the whole model back
// through applyModel's single yjs.Apply, which marks serverWrites for the
// duration so the replacement counts as a SERVER write and not a reviewer edit,
// bumps the rev peers watch, and leaves the next projection to write content.md
// cleanly. See handleVersionRestore, which loads a historical document exactly
// this way.
//
// ON THE AGENT'S BEHALF (byAgent) because this content IS the agent's
// structural work, read back off the page it just restructured: the round it
// lands in carries the agent as an author, and the notifier is seeded so the
// agent is not woken with its own restructure as news.
//
// Called from reextract with r.mu and the server mutex both RELEASED — mutate
// takes the server mutex itself, and the projection seam already hands
// afterProject the round boundary with that mutex released (see project).
//
// The caret goes to the start and the browser's undo stack is orphaned, as with
// any full load. That is the accepted cost at a ROUND BOUNDARY: the reviewer
// has just sent and is not mid-edit, which is the same trade restore makes.
//
// IT CARRIES NO `extra`, SO PENDING INSTRUCTION MARKS GO WITH THE OLD MODEL,
// and that is safe for exactly one reason: the round's instructions are
// discharged AT THE SEND (handleRoundHandoff clears the carriers and deletes
// the threads), so the rail is already empty when the agent is handed the round
// this reload ends. The spec states the assumption — "the round's instructions
// are discharged before the structure changes" — and
// TestAStructuralReloadHasNoPendingInstructionsToLose pins it. The case this
// drops silently is an instruction filed with NO round in flight at all — the
// reviewer writes it, never sends, and the structural reload replaces the
// model it was anchored to; only Task 5's gate can see what a real browser
// does with a thread across a fragment reload.
//
// The reload CLAIM is staked by reextract, in the same critical section as the
// decision; this releases it if nothing lands.
//
// It reports whether the live document now holds md.
func (r *pageRenderer) reload(md []byte) bool {
	model, _, err := markdown.Parse(md)
	if err != nil {
		// Extract already parsed this markdown, so this is a galley bug rather
		// than a page problem — and it is deliberately NOT routed through cannot:
		// cannot cuts a round, which re-enters render on this goroutine, and
		// reextract has just cleared the refusal memo that dedupe depends on. Say
		// it once, on the channel that needs no hook, and leave the page alone.
		r.unclaim(md)
		_, _ = fmt.Fprintf(pageStderr,
			"htmlpage: the re-extracted content does not parse as markdown (%v) — this is a galley bug, report it\n", err)
		return false
	}
	// Counted BEFORE the replacement lands, off the model this reload is about
	// to discard, so the loud line below can name what went with it — see the
	// doc comment above for why anything found here is, by assumption, an
	// instruction filed with no round in flight.
	pending, pendingErr := r.es.pending()
	if _, err := r.es.mutate(byAgent, func(docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		return model, nil, nil
	}); err != nil {
		// Nothing landed, so release the claim: the next projection retries.
		r.unclaim(md)
		_, _ = fmt.Fprintf(pageStderr, "htmlpage: could not reload the editor on the re-extracted content: %v\n", err)
		return false
	}
	r.mu.Lock()
	r.reloads++
	r.mu.Unlock()
	lost := ""
	if pendingErr == nil && len(pending.Instructions) > 0 {
		lost = fmt.Sprintf(" — %d pending instruction(s) went with the replaced document", len(pending.Instructions))
	}
	_, _ = fmt.Fprintf(pageStderr, "htmlpage: %s was restructured — the editor reloads on %d bytes of re-extracted content%s\n",
		filepath.Base(r.pagePath), len(md), lost)
	return true
}

// unclaim releases the reload claim reextract staked, so a reload that did not
// land is retried by the next projection rather than swallowed by the guard.
// The GENERATION is deliberately not rolled back: it is cheap to be one
// generation too cautious (the projections it stands down are re-run by the
// next settle, and the drift was not adopted), and rolling it back would race
// the very renders it was raised to stop.
func (r *pageRenderer) unclaim(md []byte) {
	r.mu.Lock()
	if bytes.Equal(r.reloaded, md) {
		r.reloaded = nil
	}
	r.mu.Unlock()
}

// sameDocument asks whether two markdowns say the same thing, which is what
// "the page moved" has to mean here and byte equality does not. It compares
// PROSE AND STRUCTURE ONLY: the parse-and-serialize round trip drops comment
// and instruction carriers, so a reviewer's live instruction is not a change to
// the page and never triggers a structural round on its own — which is what
// this signal wants, since those carriers never reach page.html either.
//
// THE TWO SIDES SPELL THE SAME DOCUMENT DIFFERENTLY, ALWAYS AND HARMLESSLY.
// The extractor writes markdown its own way and galley's serializer writes it
// another — an `&`, an `_` or a `*` in prose comes out of an extraction escaped
// and out of a projection plain — so a page whose prose contains one of those
// characters compares unequal on every single round. Read as a change, that is
// a reload per projection and a reload schedules the next projection: measured,
// six projections gave six reloads with no edit from anybody. The question the
// structural signal actually asks is whether the DOCUMENT differs, so both
// sides are put through the same parse and serializer before they are compared.
func sameDocument(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	am, _, err := markdown.Parse(a)
	if err != nil {
		return false
	}
	bm, _, err := markdown.Parse(b)
	if err != nil {
		return false
	}
	return bytes.Equal(markdown.Serialize(am), markdown.Serialize(bm))
}

// drifted reports whether page.html changed on disk since galley last wrote
// it. That is a fact the pipeline acts on, not a refusal to narrate: under the
// live loop it means the agent restructured the page between rounds, and
// render routes it straight to reextract instead of rendering over it. It is
// always called with r.mu held, so its read of lastWritten is inside the
// renderer's own critical section. The genuine tear this guard exists for — a
// page caught mid-write, or one that no longer reads as reviewable HTML at
// all — is not decided here: it surfaces one step later, in reextract, where
// Extract either succeeds on whatever bytes are on disk (a restructure) or
// refuses them (a tear), via the ordinary cannot path. A ReadFile failure here
// (the file briefly missing) simply reports no drift for this cycle; the next
// projection sees it.
func (r *pageRenderer) drifted() bool {
	current, cerr := os.ReadFile(r.pagePath)
	if cerr != nil {
		return false
	}
	cur := sha256.Sum256(current)
	return cur != r.lastWritten && cur != r.originalHash
}
