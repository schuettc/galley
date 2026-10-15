// Package render turns a markdown document into one self-contained, branded
// HTML page: the file Share's Download HTML and Share HTML hand over and the
// preview tab shows. It
// knows nothing about the server; internal/serve calls it with the document
// and the embedded mermaid.js.
package render

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/schuettc/tools-common"
)

// Theme is the look of a rendered page. Every theme shares galley's one
// layout; a theme changes only colours, fonts, logo, label and extra CSS.
type Theme struct {
	Name, Label         string
	Light, Dark         Palette
	Fonts               []Font
	LogoLight, LogoDark []byte
	ExtraCSS            string
}

// Palette is one colour scheme's five colours.
type Palette struct {
	Bg     string `json:"bg"`
	Fg     string `json:"fg"`
	Muted  string `json:"muted"`
	Line   string `json:"line"`
	Accent string `json:"accent"`
}

// Font is one font file to embed. Data is the file's bytes, read at load.
type Font struct {
	Family string `json:"family"`
	File   string `json:"file"`
	Data   []byte `json:"-"`
	Weight string `json:"weight"`
	Style  string `json:"style"`
}

//go:embed base
var baseFS embed.FS

// themeFile is theme.json as written; every field may be left out.
type themeFile struct {
	Label string  `json:"label"`
	Light Palette `json:"light"`
	Dark  Palette `json:"dark"`
	Fonts []Font  `json:"fonts"`
}

// BaseTheme is galley's own look, used when a project names no theme and for
// every colour a theme leaves out. It has no logo and no label, so its page
// has no header.
func BaseTheme() Theme {
	raw, err := baseFS.ReadFile("base/theme.json")
	if err != nil {
		panic(err)
	}
	var f themeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		panic(err)
	}
	return Theme{Name: "base", Label: f.Label, Light: f.Light, Dark: f.Dark}
}

// themesDir is where themes live: ~/.config/galley/themes (XDG-aware).
func themesDir() string { return filepath.Join(tools.ConfigDir("galley"), "themes") }

// LoadTheme reads the theme folder of that name, filling anything it leaves
// out from the base theme.
func LoadTheme(name string) (Theme, error) {
	if !plainName(name) {
		return Theme{}, fmt.Errorf("theme %q: a theme is named by its folder in %s, not a path", name, themesDir())
	}
	dir := filepath.Join(themesDir(), name)
	raw, err := os.ReadFile(filepath.Join(dir, "theme.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Theme{}, fmt.Errorf("theme %q: no theme.json in %s", name, dir)
	}
	if err != nil {
		return Theme{}, fmt.Errorf("theme %q: %w", name, err)
	}
	var f themeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Theme{}, fmt.Errorf("theme %q: %s: %w", name, filepath.Join(dir, "theme.json"), err)
	}
	t := BaseTheme()
	t.Name = name
	if f.Label != "" {
		t.Label = f.Label
	}
	t.Light = fill(f.Light, t.Light)
	t.Dark = fill(f.Dark, t.Dark)
	for _, font := range f.Fonts {
		data, err := os.ReadFile(filepath.Join(dir, font.File))
		if err != nil {
			return Theme{}, fmt.Errorf("theme %q: font file %s: %w", name, font.File, err)
		}
		font.Data = data
		t.Fonts = append(t.Fonts, font)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "logo-light.svg")); err == nil {
		t.LogoLight = b
	}
	if b, err := os.ReadFile(filepath.Join(dir, "logo-dark.svg")); err == nil {
		t.LogoDark = b
	}
	if b, err := os.ReadFile(filepath.Join(dir, "extra.css")); err == nil {
		t.ExtraCSS = string(b)
	}
	return t, nil
}

// plainName reports whether n names an entry directly inside a folder: not
// empty, not "." or "..", and without a path separator — so a theme name
// cannot reach outside the themes folder it is joined onto.
func plainName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\`)
}

// fill takes each colour p leaves empty from base.
func fill(p, base Palette) Palette {
	pick := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	return Palette{
		Bg:     pick(p.Bg, base.Bg),
		Fg:     pick(p.Fg, base.Fg),
		Muted:  pick(p.Muted, base.Muted),
		Line:   pick(p.Line, base.Line),
		Accent: pick(p.Accent, base.Accent),
	}
}

// ThemeFor is the theme the document's project chooses: the nearest
// .galley/config.json from the document's folder upward, or the base theme
// when there is none.
func ThemeFor(docPath string) (Theme, error) {
	abs, err := filepath.Abs(docPath)
	if err != nil {
		return Theme{}, err
	}
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		cfg := filepath.Join(dir, ".galley", "config.json")
		raw, err := os.ReadFile(cfg)
		if err == nil {
			var c struct {
				Theme string `json:"theme"`
			}
			if err := json.Unmarshal(raw, &c); err != nil {
				return Theme{}, fmt.Errorf("%s: %w", cfg, err)
			}
			if c.Theme == "" {
				return BaseTheme(), nil
			}
			return LoadTheme(c.Theme)
		}
		if parent := filepath.Dir(dir); parent == dir {
			return BaseTheme(), nil
		}
	}
}
