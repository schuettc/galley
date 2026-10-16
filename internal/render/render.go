package render

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/suggest"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

//go:embed page.html.tmpl
var pageTmpl string

//go:embed page.css
var pageCSS string

var page = template.Must(template.New("page").Parse(pageTmpl))

// heading is one sidebar entry.
type heading struct {
	Level int
	Text  string
	ID    string
}

type pageData struct {
	Title       string
	Label       string
	LogoLight   template.URL
	LogoDark    template.Srcset
	CSS         template.CSS
	Headings    []heading
	Body        template.HTML
	Mermaid     template.JS
	MermaidVars template.JS
}

// Render turns a markdown document into one self-contained HTML page in the
// theme's look. Review instructions and highlights are removed first, front
// matter is not shown, and mermaid.js is inlined only when a diagram needs it.
// Nothing in the page is fetched from anywhere, except a remote image: an
// image by relative path is read from docDir and embedded as a data: URL.
func Render(md []byte, t Theme, mermaidJS []byte, docDir string) ([]byte, error) {
	d, _, err := markdown.Parse(md)
	if err != nil {
		return nil, err
	}
	d = suggest.ClearInstructions(d)
	d.Blocks = dropFrontMatter(d.Blocks)
	src := markdown.Serialize(d)

	imgs := newImageRenderer(docDir)
	defer imgs.close()
	gm := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(
			util.Prioritized(fenceRenderer{}, 100),
			util.Prioritized(imgs, 100),
		)),
	)
	doc := gm.Parser().Parse(text.NewReader(src))
	title, headings, hasMermaid := survey(doc, src)
	var body bytes.Buffer
	if err := gm.Renderer().Render(&body, src, doc); err != nil {
		return nil, err
	}

	data := pageData{
		Title:    title,
		Label:    t.Label,
		CSS:      template.CSS(themeCSS(t) + pageCSS + "\n" + t.ExtraCSS), //nolint:gosec // galley and theme styles
		Headings: headings,
		Body:     template.HTML(body.String()), //nolint:gosec // goldmark output, raw HTML omitted
	}
	// The header shows only what the theme has. A theme with no dark logo
	// shows its light one in both schemes.
	if len(t.LogoLight) > 0 {
		dark := t.LogoDark
		if len(dark) == 0 {
			dark = t.LogoLight
		}
		data.LogoLight = svgURL(t.LogoLight)
		data.LogoDark = template.Srcset(svgURL(dark)) //nolint:gosec // inline logo
	}
	if hasMermaid {
		vars, err := json.Marshal(map[string]map[string]string{"light": mermaidVars(t.Light), "dark": mermaidVars(t.Dark)})
		if err != nil {
			return nil, err
		}
		data.Mermaid = template.JS(scriptSafe(mermaidJS)) //nolint:gosec // galley's own bundle
		data.MermaidVars = template.JS(vars)              //nolint:gosec // JSON of theme colours
	}
	var out bytes.Buffer
	if err := page.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func dropFrontMatter(blocks []docmodel.Block) []docmodel.Block {
	out := blocks[:0:0]
	for _, b := range blocks {
		if b.Kind != docmodel.FrontMatter {
			out = append(out, b)
		}
	}
	return out
}

// survey finds the page title (the first level-one heading), the sidebar's
// level-two and level-three headings, and whether any fence is a diagram.
func survey(doc ast.Node, src []byte) (string, []heading, bool) {
	title := ""
	var hs []heading
	hasMermaid := false
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			txt := plainText(n, src)
			if n.Level == 1 && title == "" {
				title = txt
			}
			if n.Level == 2 || n.Level == 3 {
				id, _ := n.AttributeString("id")
				idb, _ := id.([]byte)
				hs = append(hs, heading{Level: n.Level, Text: txt, ID: string(idb)})
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			if isMermaid(n, src) {
				hasMermaid = true
			}
		}
		return ast.WalkContinue, nil
	})
	if title == "" {
		title = "document"
	}
	return title, hs, hasMermaid
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
		case *ast.String:
			b.Write(c.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func isMermaid(n *ast.FencedCodeBlock, src []byte) bool {
	return string(n.Language(src)) == "mermaid"
}

// fenceRenderer draws a mermaid fence as <pre class="mermaid"> for mermaid.js
// to find, and every other fence as goldmark does.
type fenceRenderer struct{}

func (fenceRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, renderFence)
}

func renderFence(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	var code bytes.Buffer
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		code.Write(seg.Value(src))
	}
	esc := html.EscapeString(code.String())
	if isMermaid(n, src) {
		_, _ = fmt.Fprintf(w, "<pre class=\"mermaid\">%s</pre>\n", esc)
		return ast.WalkSkipChildren, nil
	}
	if lang := n.Language(src); lang != nil {
		_, _ = fmt.Fprintf(w, "<pre><code class=\"language-%s\">%s</code></pre>\n", html.EscapeString(string(lang)), esc)
	} else {
		_, _ = fmt.Fprintf(w, "<pre><code>%s</code></pre>\n", esc)
	}
	return ast.WalkSkipChildren, nil
}

// ImageTypes is the figures galley shows from beside a document: extension
// to Content-Type. The editor serves these as siblings, and the render embeds
// them, so the two show the same images.
var ImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".avif": "image/avif",
}

// imageRenderer embeds an image whose src is a relative path as a data: URL,
// read from the document's folder through an os.Root, so a path cannot leave
// it. Any other image, and one it cannot embed (missing, outside the folder,
// not a figure type), is drawn as goldmark draws it, with its src unchanged.
type imageRenderer struct {
	root *os.Root
	base renderer.NodeRendererFunc
}

func newImageRenderer(docDir string) *imageRenderer {
	r := &imageRenderer{}
	gmhtml.NewRenderer().RegisterFuncs(captureImage{&r.base})
	if docDir != "" {
		r.root, _ = os.OpenRoot(docDir)
	}
	return r
}

func (r *imageRenderer) close() {
	if r.root != nil {
		_ = r.root.Close()
	}
}

func (r *imageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, r.renderImage)
}

func (r *imageRenderer) renderImage(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Image)
	data, ok := r.embed(string(n.Destination))
	if !entering || !ok {
		return r.base(w, src, node, entering)
	}
	_, _ = fmt.Fprintf(w, `<img src="%s" alt="%s"`, data, html.EscapeString(plainText(n, src)))
	if n.Title != nil {
		_, _ = w.WriteString(` title="`)
		gmhtml.DefaultWriter.Write(w, n.Title)
		_ = w.WriteByte('"')
	}
	if n.Attributes() != nil {
		gmhtml.RenderAttributes(w, n, gmhtml.ImageAttributeFilter)
	}
	_, _ = w.WriteString(">")
	return ast.WalkSkipChildren, nil
}

// embed answers dest as a data: URL, or false when dest is not a relative
// path to a figure inside the document's folder.
func (r *imageRenderer) embed(dest string) (string, bool) {
	if r.root == nil {
		return "", false
	}
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return "", false
	}
	ctype, ok := ImageTypes[strings.ToLower(path.Ext(u.Path))]
	if !ok {
		return "", false
	}
	f, err := r.root.Open(filepath.FromSlash(u.Path))
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", false
	}
	return "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(b), true
}

// captureImage takes goldmark's own image function from its HTML renderer.
type captureImage struct{ fn *renderer.NodeRendererFunc }

func (c captureImage) Register(k ast.NodeKind, fn renderer.NodeRendererFunc) {
	if k == ast.KindImage {
		*c.fn = fn
	}
}

func svgURL(svg []byte) template.URL {
	return template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg)) //nolint:gosec // inline logo
}

// themeCSS declares the palette as custom properties for both schemes, and
// the theme's fonts as @font-face rules with data: URLs.
func themeCSS(t Theme) string {
	var b strings.Builder
	sans := `system-ui, -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif`
	seen := map[string]bool{}
	var families []string
	for _, f := range t.Fonts {
		fmt.Fprintf(&b, "@font-face{font-family:%q;src:url(data:%s;base64,%s);font-weight:%s;font-style:%s;font-display:swap}\n",
			f.Family, fontMIME(f.File), base64.StdEncoding.EncodeToString(f.Data), orDefault(f.Weight, "normal"), orDefault(f.Style, "normal"))
		if !seen[f.Family] {
			seen[f.Family] = true
			families = append(families, fmt.Sprintf("%q", f.Family))
		}
	}
	if len(families) > 0 {
		sans = strings.Join(families, ", ") + ", " + sans
	}
	fmt.Fprintf(&b, ":root{color-scheme:light dark;--sans:%s;%s}\n", sans, vars(t.Light))
	fmt.Fprintf(&b, "@media (prefers-color-scheme: dark){:root{%s}}\n", vars(t.Dark))
	return b.String()
}

func vars(p Palette) string {
	return fmt.Sprintf("--bg:%s;--fg:%s;--muted:%s;--line:%s;--accent:%s", p.Bg, p.Fg, p.Muted, p.Line, p.Accent)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func fontMIME(file string) string {
	switch {
	case strings.HasSuffix(file, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(file, ".woff"):
		return "font/woff"
	case strings.HasSuffix(file, ".otf"):
		return "font/otf"
	default:
		return "font/ttf"
	}
}

// mermaidVars colours a diagram from a palette.
func mermaidVars(p Palette) map[string]string {
	return map[string]string{
		"background":          p.Bg,
		"primaryColor":        p.Bg,
		"primaryTextColor":    p.Fg,
		"primaryBorderColor":  p.Accent,
		"secondaryColor":      p.Line,
		"tertiaryColor":       p.Bg,
		"lineColor":           p.Muted,
		"textColor":           p.Fg,
		"mainBkg":             p.Bg,
		"nodeBorder":          p.Accent,
		"clusterBkg":          p.Line,
		"titleColor":          p.Fg,
		"edgeLabelBackground": p.Bg,
		"noteBkgColor":        p.Line,
		"noteTextColor":       p.Fg,
		"actorBkg":            p.Bg,
		"actorBorder":         p.Accent,
		"actorTextColor":      p.Fg,
		"signalColor":         p.Fg,
		"signalTextColor":     p.Fg,
	}
}

var closeScript = regexp.MustCompile(`(?i)</script`)

// scriptSafe keeps an inlined script from closing its own <script> element.
func scriptSafe(js []byte) string {
	return closeScript.ReplaceAllString(string(js), `<\/script`)
}
