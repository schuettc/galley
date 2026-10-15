package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configHome points the theme folder at a temp dir and returns it.
func configHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GALLEY_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", tmp)
	return tmp
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTheme(t *testing.T, home, name, themeJSON string) string {
	t.Helper()
	dir := filepath.Join(home, "galley", "themes", name)
	writeFile(t, filepath.Join(dir, "theme.json"), themeJSON)
	writeFile(t, filepath.Join(dir, "logo-light.svg"), "<svg>light</svg>")
	writeFile(t, filepath.Join(dir, "logo-dark.svg"), "<svg>dark</svg>")
	return dir
}

func TestThemeForFindsConfigUpward(t *testing.T) {
	home := configHome(t)
	writeTheme(t, home, "acme", `{"label":"Acme"}`)
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".galley", "config.json"), `{"theme":"acme"}`)
	doc := filepath.Join(repo, "docs", "sub", "a.md")
	writeFile(t, doc, "# A\n")
	got, err := ThemeFor(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "acme" {
		t.Fatalf("Name = %q, want acme", got.Name)
	}
}

func TestThemeForWithoutConfigIsBase(t *testing.T) {
	configHome(t)
	doc := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, doc, "# A\n")
	got, err := ThemeFor(doc)
	if err != nil {
		t.Fatal(err)
	}
	base := BaseTheme()
	if got.Name != base.Name || got.Light != base.Light {
		t.Fatalf("got %q %+v, want base %q %+v", got.Name, got.Light, base.Name, base.Light)
	}
}

func TestLoadThemeFillsMissingFromBase(t *testing.T) {
	home := configHome(t)
	writeTheme(t, home, "acme", `{"label":"Acme","light":{"accent":"#123456"}}`)
	got, err := LoadTheme("acme")
	if err != nil {
		t.Fatal(err)
	}
	base := BaseTheme()
	if got.Label != "Acme" {
		t.Errorf("Label = %q", got.Label)
	}
	if got.Light.Accent != "#123456" {
		t.Errorf("Light.Accent = %q", got.Light.Accent)
	}
	if got.Light.Bg != base.Light.Bg {
		t.Errorf("Light.Bg = %q, want base %q", got.Light.Bg, base.Light.Bg)
	}
	if got.Dark != base.Dark {
		t.Errorf("Dark = %+v, want base %+v", got.Dark, base.Dark)
	}
}

func TestLoadThemeUnknownNameErrors(t *testing.T) {
	home := configHome(t)
	_, err := LoadTheme("nope")
	if err == nil {
		t.Fatal("want an error")
	}
	dir := filepath.Join(home, "galley", "themes", "nope")
	if !strings.Contains(err.Error(), `theme "nope"`) || !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want it to name the theme and %s", err, dir)
	}
}

func TestLoadThemeMalformedJSONErrors(t *testing.T) {
	home := configHome(t)
	writeTheme(t, home, "bad", `{"label":`)
	_, err := LoadTheme("bad")
	if err == nil || !strings.Contains(err.Error(), "theme.json") {
		t.Fatalf("err = %v, want one naming theme.json", err)
	}
}

func TestLoadThemeReadsFontsAndExtraCSS(t *testing.T) {
	home := configHome(t)
	dir := writeTheme(t, home, "acme", `{"fonts":[{"family":"Aeonik","file":"Aeonik-Regular.woff2","weight":"400"}]}`)
	font := []byte{0x77, 0x4f, 0x46, 0x32, 0x00, 0x01}
	if err := os.WriteFile(filepath.Join(dir, "Aeonik-Regular.woff2"), font, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "extra.css"), ".acme{color:red}")
	got, err := LoadTheme("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fonts) != 1 || !bytes.Equal(got.Fonts[0].Data, font) || got.Fonts[0].Family != "Aeonik" {
		t.Fatalf("Fonts = %+v", got.Fonts)
	}
	if got.ExtraCSS != ".acme{color:red}" {
		t.Fatalf("ExtraCSS = %q", got.ExtraCSS)
	}
}

// A theme name is a folder name in the themes directory and nothing else: a
// project config cannot point galley at a folder outside it.
func TestThemeNameCannotLeaveTheThemesFolder(t *testing.T) {
	home := configHome(t)
	// A loadable theme.json where "../x" would land if it were joined as
	// given, so an unchecked name loads instead of failing by accident.
	writeFile(t, filepath.Join(home, "galley", "x", "theme.json"), `{"label":"outside"}`)
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".galley", "config.json"), `{"theme":"../x"}`)
	doc := filepath.Join(repo, "a.md")
	writeFile(t, doc, "# A\n")
	_, err := ThemeFor(doc)
	if err == nil || !strings.Contains(err.Error(), `theme "../x"`) {
		t.Fatalf("err = %v, want an error naming the theme", err)
	}
}

// A theme that leaves out a logo file has no logo for that scheme: galley has
// none of its own to fill it with.
func TestLoadThemeMissingLogoIsEmpty(t *testing.T) {
	home := configHome(t)
	dir := filepath.Join(home, "galley", "themes", "plain")
	writeFile(t, filepath.Join(dir, "theme.json"), `{}`)
	got, err := LoadTheme("plain")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LogoLight) != 0 || len(got.LogoDark) != 0 || got.Label != "" {
		t.Fatalf("got logos %q %q label %q, want none", got.LogoLight, got.LogoDark, got.Label)
	}
}
