package render

import (
	"strings"
	"testing"
)

const fakeMermaid = "/*MERMAID*/"

func render(t *testing.T, md string, th Theme) string {
	t.Helper()
	out, err := Render([]byte(md), th, []byte(fakeMermaid))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRenderIsSelfContained(t *testing.T) {
	md := "# Title\n\nSee https://example.com and [this](https://example.com/x).\n\n## Data\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```mermaid\ngraph TD\n  A-->B\n```\n"
	out := render(t, md, BaseTheme())
	for _, bad := range []string{`src="http`, `<link rel="stylesheet"`, `@import`, `url(http`} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	if !strings.Contains(out, "<table>") {
		t.Error("table did not render")
	}
}

func TestRenderBuildsSidebar(t *testing.T) {
	out := render(t, "# T\n\n## One\n\n### Two\n\n#### Three\n", BaseTheme())
	nav := out[strings.Index(out, "<nav"):]
	nav = nav[:strings.Index(nav, "</nav>")]
	for _, want := range []string{`href="#one"`, `href="#two"`} {
		if !strings.Contains(nav, want) {
			t.Errorf("sidebar lacks %s: %s", want, nav)
		}
	}
	if strings.Contains(nav, "three") || strings.Contains(nav, "Three") {
		t.Errorf("sidebar lists an h4: %s", nav)
	}
}

func TestRenderMermaidBlock(t *testing.T) {
	out := render(t, "# T\n\n```mermaid\ngraph TD\n  A-->B\n```\n", BaseTheme())
	if !strings.Contains(out, `<pre class="mermaid"`) {
		t.Error("no mermaid block")
	}
	if !strings.Contains(out, "A--&gt;B") {
		t.Error("diagram source missing")
	}
	if n := strings.Count(out, fakeMermaid); n != 1 {
		t.Errorf("mermaid.js appears %d times, want 1", n)
	}
}

func TestRenderNoMermaidNoScript(t *testing.T) {
	out := render(t, "# T\n\nplain\n\n```go\nfunc x() {}\n```\n", BaseTheme())
	if strings.Contains(out, fakeMermaid) {
		t.Error("mermaid.js included for a document without a diagram")
	}
}

func TestRenderDropsInstructions(t *testing.T) {
	out := render(t, "Keep {==this==}{>>change it<<} text\n", BaseTheme())
	if !strings.Contains(out, "this") {
		t.Error("anchored text lost")
	}
	for _, bad := range []string{"change it", "{=="} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
}

func TestRenderUsesTheme(t *testing.T) {
	th := BaseTheme()
	th.Light.Accent = "#123456"
	th.Label = "Acme"
	th.ExtraCSS = ".x{}"
	out := render(t, "# T\n", th)
	for _, want := range []string{"#123456", "Acme", ".x{}"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}
