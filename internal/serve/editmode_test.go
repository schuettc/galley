package serve

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditShellCarriesPageMode verifies that the edit shell template exposes
// data-page-mode and data-preview on the #editor element so the web app can
// build the split pane on mount.
//
// Page-mode path: data-page-mode="1" and data-preview="/_galley/preview/<base>".
// Markdown-mode path: data-page-mode="" (empty attribute) and data-preview="".
func TestEditShellCarriesPageMode(t *testing.T) {
	t.Run("page mode carries attributes", func(t *testing.T) {
		dir := t.TempDir()
		page := writePage(t, dir, "index.html", fixturePage)

		s, err := NewEditPage(page)
		if err != nil {
			t.Fatalf("NewEditPage: %v", err)
		}
		defer func() { _ = s.Close() }()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		s.Handler().ServeHTTP(rec, req)

		body := rec.Body.String()
		if !strings.Contains(body, `data-page-mode="1"`) {
			t.Errorf("page-mode shell: data-page-mode=\"1\" not found in:\n%s", body)
		}
		wantPreview := `data-preview="/_galley/preview/` + filepath.Base(page) + `"`
		if !strings.Contains(body, wantPreview) {
			t.Errorf("page-mode shell: %s not found in:\n%s", wantPreview, body)
		}
	})

	t.Run("markdown mode attributes are empty", func(t *testing.T) {
		s := newEditServer(t, t.TempDir(), "note.md", "# Title\n\nA paragraph.\n")
		defer func() { _ = s.Close() }()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		s.Handler().ServeHTTP(rec, req)

		body := rec.Body.String()
		if !strings.Contains(body, `data-page-mode=""`) {
			t.Errorf("markdown-mode shell: data-page-mode=\"\" not found in:\n%s", body)
		}
		if !strings.Contains(body, `data-preview=""`) {
			t.Errorf("markdown-mode shell: data-preview=\"\" not found in:\n%s", body)
		}
	})
}

// TestEditShellTemplateDirectly exercises editShell.Execute directly (the way
// the production code does) to verify the template fields round-trip correctly.
func TestEditShellTemplateDirectly(t *testing.T) {
	type shellData struct {
		Title      string
		Room       string
		PageMode   bool
		PreviewURL string
		Path       string
	}

	t.Run("page mode fields in template", func(t *testing.T) {
		var buf bytes.Buffer
		data := shellData{
			Title:      "index.html",
			Room:       "test-room",
			PageMode:   true,
			PreviewURL: "/_galley/preview/index.html",
		}
		// editShell is the package-level embedded template.
		if err := editShell.Execute(&buf, data); err != nil {
			// If the template doesn't know PageMode yet, this will fail with a
			// template execution error — that is the expected failing state.
			t.Fatalf("editShell.Execute: %v", err)
		}
		got := buf.String()
		if !strings.Contains(got, `data-page-mode="1"`) {
			t.Errorf("template output missing data-page-mode=\"1\":\n%s", got)
		}
		if !strings.Contains(got, `data-preview="/_galley/preview/index.html"`) {
			t.Errorf("template output missing data-preview:\n%s", got)
		}
	})

	t.Run("markdown mode fields in template", func(t *testing.T) {
		var buf bytes.Buffer
		data := shellData{
			Title:      "note.md",
			Room:       "test-room",
			PageMode:   false,
			PreviewURL: "",
		}
		if err := editShell.Execute(&buf, data); err != nil {
			// html/template errors on unknown struct fields: if the template
			// references PageMode and the data struct no longer has it, Execute
			// fails loudly rather than rendering empty. We do NOT use
			// template.Must here so we get a useful message.
			t.Fatalf("editShell.Execute: %v", err)
		}
		got := buf.String()
		if !strings.Contains(got, `data-page-mode=""`) {
			t.Errorf("markdown-mode template missing data-page-mode=\"\":\n%s", got)
		}
	})
}

// Ensure the package-level editShell var is exported for reference in tests.
// (It is declared in editmode.go as a package-level var; we reference it here
// by name; if the name changes the test breaks loudly.)
var _ *template.Template = editShell
