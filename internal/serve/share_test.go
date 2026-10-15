package serve

import (
	"encoding/json"
	"errors"
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

// onClipboard stands in for the clipboard and records each path put on it, and
// lets the share routes answer as they do on macOS wherever the tests run.
func onClipboard(s *EditServer, fail error) *[]string {
	var got []string
	s.canShare = true
	s.putFile = func(path string) error {
		got = append(got, path)
		return fail
	}
	return &got
}

func shared(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Path
}

func attachment(t *testing.T, rec *httptest.ResponseRecorder, name string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, `filename="`+name+`"`) {
		t.Fatalf("Content-Disposition = %q, want an attachment named %s", cd, name)
	}
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

func TestDownloadHTMLIsTheRenderAsAnAttachment(t *testing.T) {
	noThemes(t)
	s := newEditServer(t, t.TempDir(), "doc.md", "# Hi\n\n## Part\n\nWords.\n")
	defer func() { _ = s.Close() }()
	rec := get(s.Handler(), "/_galley/render?download=1")
	attachment(t, rec, "doc.html")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Part") {
		t.Error("the download lacks the document")
	}
	if strings.Contains(body, "/_galley/saved") {
		t.Error("the download carries the preview's refresh script")
	}
}

func TestDownloadMarkdownIsTheFileOnDisk(t *testing.T) {
	src := "# Hi\n\nThe words on disk.\n"
	s := newEditServer(t, t.TempDir(), "doc.md", src)
	defer func() { _ = s.Close() }()
	rec := get(s.Handler(), "/_galley/download/md")
	attachment(t, rec, "doc.md")
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	raw, err := os.ReadFile(s.MdPath)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(raw) {
		t.Fatalf("body = %q, want the file %q", rec.Body.String(), raw)
	}
}

func TestShareHTMLWritesBesideDocAndPutsItOnTheClipboard(t *testing.T) {
	noThemes(t)
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Hi\n\nFirst words.\n")
	defer func() { _ = s.Close() }()
	clip := onClipboard(s, nil)
	h := s.Handler()

	path := shared(t, post(t, h, "/_galley/share/html", nil))
	want := filepath.Join(filepath.Dir(s.MdPath), "doc.html")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if len(*clip) != 1 || (*clip)[0] != want {
		t.Fatalf("clipboard got %q, want [%q]", *clip, want)
	}
	raw, err := os.ReadFile(path)
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
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), "Second words.") || strings.Contains(string(raw), "First words.") {
		t.Error("the second save did not replace the first")
	}
}

func TestShareMarkdownPutsTheMdOnTheClipboard(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Hi\n")
	defer func() { _ = s.Close() }()
	clip := onClipboard(s, nil)
	path := shared(t, post(t, s.Handler(), "/_galley/share/md", nil))
	if path != s.MdPath || !filepath.IsAbs(path) {
		t.Fatalf("path = %q, want %q", path, s.MdPath)
	}
	if len(*clip) != 1 || (*clip)[0] != s.MdPath {
		t.Fatalf("clipboard got %q, want [%q]", *clip, s.MdPath)
	}
}

func TestAClipboardFailureIs500WithItsText(t *testing.T) {
	noThemes(t)
	s := newEditServer(t, t.TempDir(), "doc.md", "# Hi\n")
	defer func() { _ = s.Close() }()
	onClipboard(s, errors.New("osascript: the clipboard said no"))
	h := s.Handler()
	for _, route := range []string{"/_galley/share/html", "/_galley/share/md"} {
		rec := post(t, h, route, nil)
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "the clipboard said no") {
			t.Errorf("%s: %d %q, want 500 with the error", route, rec.Code, rec.Body.String())
		}
	}
}

func TestShareRoutesAreMacOnly(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "doc.md", "# Hi\n")
	defer func() { _ = s.Close() }()
	clip := onClipboard(s, nil)
	s.canShare = false
	h := s.Handler()
	for _, route := range []string{"/_galley/share/html", "/_galley/share/md"} {
		if rec := post(t, h, route, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s off macOS: %d, want 404", route, rec.Code)
		}
	}
	if len(*clip) != 0 {
		t.Errorf("the clipboard was used off macOS: %q", *clip)
	}
	if strings.Contains(get(h, "/").Body.String(), `data-can-share="1"`) {
		t.Error("the shell offers sharing off macOS")
	}
	s.canShare = true
	if !strings.Contains(get(h, "/").Body.String(), `data-can-share="1"`) {
		t.Error("the shell does not offer sharing on macOS")
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
	clip := onClipboard(s, nil)
	rec := post(t, s.Handler(), "/_galley/share/html", nil)
	if len(*clip) != 0 {
		t.Errorf("a failed render reached the clipboard: %q", *clip)
	}
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
	rec = get(s.Handler(), "/_galley/render?download=1")
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("download got %d %q, want a 500 that is not an attachment", rec.Code, rec.Header().Get("Content-Disposition"))
	}
}

func TestPageModeSharesThePageItself(t *testing.T) {
	noThemes(t)
	dir := t.TempDir()
	page := writePage(t, dir, "page.html", fixturePage)
	s, err := NewEditPage(page)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	clip := onClipboard(s, nil)
	h := s.Handler()
	if rec := get(h, "/_galley/render"); rec.Code != http.StatusNotFound {
		t.Errorf("render: %d, want 404", rec.Code)
	}
	rec := get(h, "/_galley/render?download=1")
	attachment(t, rec, "page.html")
	raw, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(raw) {
		t.Error("Download HTML is not the page's bytes")
	}
	abs, _ := filepath.Abs(page)
	if path := shared(t, post(t, h, "/_galley/share/html", nil)); path != abs {
		t.Errorf("share/html path = %q, want the page %q", path, abs)
	}
	if len(*clip) != 1 || (*clip)[0] != abs {
		t.Errorf("clipboard got %q, want [%q]", *clip, abs)
	}
	if after, _ := os.ReadFile(page); string(after) != string(raw) {
		t.Error("Share HTML rewrote the page")
	}
	if rec := post(t, h, "/_galley/share/md", nil); rec.Code != http.StatusNotFound {
		t.Errorf("share/md: %d, want 404", rec.Code)
	}
	if rec := get(h, "/_galley/download/md"); rec.Code != http.StatusNotFound {
		t.Errorf("download/md: %d, want 404", rec.Code)
	}
}
