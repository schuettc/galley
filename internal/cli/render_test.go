package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderHome points galley's theme folder at a temp dir, as internal/render's
// tests do, and returns it.
func renderHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GALLEY_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", tmp)
	return tmp
}

const renderMD = "# Title\n\n## Section\n\nSome text.\n"

func TestRenderWritesBesideTheDocAndPrintsThePath(t *testing.T) {
	renderHome(t)
	dir := t.TempDir()
	doc := writeDoc(t, dir, "notes.md", renderMD)
	out, errw, code := galleyCLI(t, "render", doc)
	if code != 0 {
		t.Fatalf("render exited %d: %s", code, errw)
	}
	want := filepath.Join(dir, "notes.html")
	if strings.TrimSpace(out) != want {
		t.Fatalf("printed %q, want %q", out, want)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if !strings.Contains(page, `class="g-body"`) || !strings.Contains(page, "Section") {
		t.Fatalf("not the branded page:\n%s", page)
	}
	if strings.Contains(page, "/_galley/saved") {
		t.Fatal("the written page carries the preview's refresh script")
	}
}

func TestRenderWritesWhereDashOSays(t *testing.T) {
	renderHome(t)
	dir := t.TempDir()
	doc := writeDoc(t, dir, "notes.md", renderMD)
	dest := filepath.Join(t.TempDir(), "elsewhere.html")
	out, errw, code := galleyCLI(t, "render", doc, "-o", dest)
	if code != 0 {
		t.Fatalf("render exited %d: %s", code, errw)
	}
	if strings.TrimSpace(out) != dest {
		t.Fatalf("printed %q, want %q", out, dest)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.html")); err == nil {
		t.Fatal("-o also wrote beside the document")
	}
}

func TestRenderUnknownThemeFailsAndWritesNothing(t *testing.T) {
	renderHome(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".galley"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, filepath.Join(dir, ".galley"), "config.json", `{"theme":"nosuchbrand"}`)
	doc := writeDoc(t, dir, "notes.md", renderMD)
	_, errw, code := galleyCLI(t, "render", doc)
	if code == 0 {
		t.Fatal("render with an unknown theme exited 0")
	}
	if !strings.Contains(errw, "nosuchbrand") {
		t.Fatalf("error does not name the theme: %q", errw)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.html")); err == nil {
		t.Fatal("a failed render wrote a file")
	}
}

func TestRenderRefusesAnHTMLPage(t *testing.T) {
	renderHome(t)
	dir := t.TempDir()
	page := writeDoc(t, dir, "page.html", "<html><body>x</body></html>")
	_, errw, code := galleyCLI(t, "render", page)
	if code == 0 {
		t.Fatal("render of an .html page exited 0")
	}
	if !strings.Contains(errw, "already HTML") {
		t.Fatalf("error does not say the page is already HTML: %q", errw)
	}
}
