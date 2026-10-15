package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/schuettc/galley/internal/render"
	tools "github.com/schuettc/tools-common"
)

// The branded page Share writes and the preview tab shows. One renderer makes
// both from the file on disk, so the preview is what Save HTML writes, plus a
// refresh script. Markdown mode only: a page under review is already the HTML
// a reader gets, so page mode has neither route.

// previewRefresh reloads the preview tab after each save of the document, on
// the /_galley/saved stamp and cadence web/preview.ts uses.
const previewRefresh = `<script>
(function () {
  var last = null;
  setInterval(function () {
    fetch('/_galley/saved', { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (last !== null && v.saved > last) location.reload();
        last = v.saved;
      })
      .catch(function () {});
  }, 1500);
})();
</script>
`

// renderDoc renders the document as it stands on disk in its project's theme.
func (s *EditServer) renderDoc() ([]byte, error) {
	theme, err := render.ThemeFor(s.MdPath)
	if err != nil {
		return nil, err
	}
	md, err := os.ReadFile(s.MdPath)
	if err != nil {
		return nil, err
	}
	mermaid, _ := assets.ReadFile("assets/mermaid.js")
	return render.Render(md, theme, mermaid)
}

// handleRender serves the preview tab. A theme or render error is the page,
// never a half-styled one.
func (s *EditServer) handleRender(w http.ResponseWriter, r *http.Request) {
	if s.pageMode {
		http.NotFound(w, r)
		return
	}
	out, err := s.renderDoc()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page := string(out)
	if i := strings.LastIndex(page, "</body>"); i >= 0 {
		page = page[:i] + previewRefresh + page[i:]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

// handleShareHTML writes the rendered page to <name>.html beside <name>.md,
// replacing an earlier save, and answers the path it wrote. The server saves
// because a browser download cannot say where the file landed.
func (s *EditServer) handleShareHTML(w http.ResponseWriter, r *http.Request) {
	if s.pageMode {
		http.NotFound(w, r)
		return
	}
	if !guard(w, r) {
		return
	}
	out, err := s.renderDoc()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	abs, err := filepath.Abs(s.MdPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dest := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".html"
	// NOT the document writer: that one follows a symlink on purpose (see
	// WriteFileAtomic). The export is galley's own output, so temp + rename
	// replaces whatever entry is at <name>.html, a link included, and never
	// writes into a file somewhere else.
	if err := tools.WriteFileAtomic(dest, out, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"path": dest})
}
