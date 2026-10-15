package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noThemes points galley's theme folder at an empty temp dir, so a theme on
// the machine running the tests cannot change what they see.
func noThemes(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GALLEY_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRenderRouteServesBrandedPage(t *testing.T) {
	noThemes(t)
	s := newEditServer(t, t.TempDir(), "doc.md", "# Hi\n\n## Part\n\nWords.\n")
	defer func() { _ = s.Close() }()
	rec := get(s.Handler(), "/_galley/render")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"Part", "/_galley/saved"} {
		if !strings.Contains(body, want) {
			t.Errorf("render lacks %q", want)
		}
	}
}

func TestShareHTMLWritesBesideDoc(t *testing.T) {
	noThemes(t)
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Hi\n\nFirst words.\n")
	defer func() { _ = s.Close() }()
	h := s.Handler()

	rec := post(t, h, "/_galley/share/html", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(s.MdPath), "doc.html")
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
	raw, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/_galley/saved") {
		t.Error("the saved file carries the preview's refresh script")
	}
	if !strings.Contains(string(raw), "First words.") {
		t.Error("the saved file lacks the document")
	}

	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Hi\n\nSecond words.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := post(t, h, "/_galley/share/html", nil); rec.Code != http.StatusOK {
		t.Fatalf("second save: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ = os.ReadFile(got.Path)
	if !strings.Contains(string(raw), "Second words.") || strings.Contains(string(raw), "First words.") {
		t.Error("the second save did not replace the first")
	}
}

func TestShareHTMLThemeErrorIs500(t *testing.T) {
	noThemes(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".galley"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".galley", "config.json"), []byte(`{"theme":"nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newEditServer(t, dir, "doc.md", "# Hi\n")
	defer func() { _ = s.Close() }()
	rec := post(t, s.Handler(), "/_galley/share/html", nil)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `theme "nope"`) {
		t.Fatalf("got %d %q, want 500 naming the theme", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "doc.html")); !os.IsNotExist(err) {
		t.Fatalf("doc.html was written: %v", err)
	}
	rec = get(s.Handler(), "/_galley/render")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `theme "nope"`) {
		t.Fatalf("preview got %d %q, want 500 naming the theme", rec.Code, rec.Body.String())
	}
}

func TestShareRoutesAbsentInPageMode(t *testing.T) {
	noThemes(t)
	page := writePage(t, t.TempDir(), "page.html", fixturePage)
	s, err := NewEditPage(page)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	h := s.Handler()
	if rec := get(h, "/_galley/render"); rec.Code != http.StatusNotFound {
		t.Errorf("render: %d, want 404", rec.Code)
	}
	if rec := post(t, h, "/_galley/share/html", nil); rec.Code != http.StatusNotFound {
		t.Errorf("share/html: %d, want 404", rec.Code)
	}
}

func TestShellCarriesDocPath(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Hi\n")
	defer func() { _ = s.Close() }()
	body := get(s.Handler(), "/").Body.String()
	if !strings.Contains(body, `data-path="`+s.MdPath+`"`) {
		t.Fatalf("shell lacks data-path=%q", s.MdPath)
	}
	if !filepath.IsAbs(s.MdPath) {
		t.Fatalf("MdPath %q is not absolute", s.MdPath)
	}
}
