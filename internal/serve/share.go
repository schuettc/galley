package serve

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/schuettc/galley/internal/render"
)

// The branded page Share hands over and the preview tab shows. One renderer
// makes both from the file on disk, so the preview is what Download HTML and
// Share HTML hand over, plus a refresh script. A page under review is already
// the HTML a reader gets, so in page mode both hand over the page itself and
// there is no render.

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
func (s *EditServer) renderDoc() ([]byte, error) { return RenderDoc(s.MdPath) }

// RenderDoc renders the markdown document at mdPath, as it stands on disk, in
// its project's theme with galley's embedded mermaid.js: the page Download
// HTML and Share HTML hand over, and what `galley render` writes.
func RenderDoc(mdPath string) ([]byte, error) {
	theme, err := render.ThemeFor(mdPath)
	if err != nil {
		return nil, err
	}
	md, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, err
	}
	mermaid, _ := assets.ReadFile("assets/mermaid.js")
	return render.Render(md, theme, mermaid)
}

// handleRender serves the preview tab, or with ?download=1 Download HTML: the
// same page without the refresh script, as an attachment named <name>.html.
// In page mode only the download answers, with the page's own bytes. A theme
// or render error is the page, never a half-styled one.
func (s *EditServer) handleRender(w http.ResponseWriter, r *http.Request) {
	download := r.URL.Query().Get("download") == "1"
	if s.pageMode {
		if !download {
			http.NotFound(w, r)
			return
		}
		raw, err := os.ReadFile(s.pagePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		attach(w, filepath.Base(s.pagePath), "text/html; charset=utf-8", raw)
		return
	}
	out, err := s.renderDoc()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if download {
		attach(w, s.shareName(".html"), "text/html; charset=utf-8", out)
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

// handleDownloadMD is Download Markdown: the .md as it stands on disk.
func (s *EditServer) handleDownloadMD(w http.ResponseWriter, r *http.Request) {
	if s.pageMode {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(s.MdPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	attach(w, s.shareName(".md"), "text/markdown; charset=utf-8", raw)
}

// attach answers body as a download the browser saves under name: quoted
// when the name is plain ASCII, the RFC 2231 form when it is not.
func attach(w http.ResponseWriter, name, contentType string, body []byte) {
	cd := `attachment; filename="` + name + `"`
	if strings.ContainsAny(name, "\"\\") || strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r > 0x7e }) >= 0 {
		cd = mime.FormatMediaType("attachment", map[string]string{"filename": name})
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", cd)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// shareName is the document's base name with ext in place of .md.
func (s *EditServer) shareName(ext string) string {
	base := filepath.Base(s.MdPath)
	return strings.TrimSuffix(base, filepath.Ext(base)) + ext
}

// handleShareHTML is Share HTML: it writes the rendered page to <name>.html
// beside <name>.md, replacing an earlier save, puts that file on the
// clipboard and answers its path. In page mode the file is the page itself,
// and nothing is written. Off macOS there is no clipboard to put a file on,
// so the route is not there.
func (s *EditServer) handleShareHTML(w http.ResponseWriter, r *http.Request) {
	if !guard(w, r) {
		return
	}
	if !s.canShare {
		http.NotFound(w, r)
		return
	}
	if s.pageMode {
		s.shareFile(w, s.pagePath)
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
	if err := writeFileAtomic(dest, out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.shareFile(w, dest)
}

// handleShareMD is Share Markdown: the .md on disk, on the clipboard.
func (s *EditServer) handleShareMD(w http.ResponseWriter, r *http.Request) {
	if s.pageMode {
		http.NotFound(w, r)
		return
	}
	if !guard(w, r) {
		return
	}
	if !s.canShare {
		http.NotFound(w, r)
		return
	}
	abs, err := filepath.Abs(s.MdPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.shareFile(w, abs)
}

// shareFile puts path on the clipboard and answers it, or answers the
// clipboard's error for the status line.
func (s *EditServer) shareFile(w http.ResponseWriter, path string) {
	if err := s.putFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

// putFileOnClipboard puts the file itself, not its path, on the macOS
// clipboard, so pasting into a chat attaches it. The path is an argument to
// the script, never part of its text.
func putFileOnClipboard(path string) error {
	out, err := exec.Command("osascript",
		"-e", "on run argv",
		"-e", "set the clipboard to (POSIX file (item 1 of argv))",
		"-e", "end run",
		path).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("could not put %s on the clipboard: %s", filepath.Base(path), msg)
		}
		return fmt.Errorf("could not put %s on the clipboard: %w", filepath.Base(path), err)
	}
	return nil
}
