package htmlpage

import (
	"testing"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

// findLinkHref searches a parsed Doc for the first Link-marked inline and
// returns its href destination, or ("", false) if none is found.
func findLinkHref(doc docmodel.Doc) (string, bool) {
	for _, b := range doc.Blocks {
		for _, in := range b.Inlines {
			if in.Has(docmodel.Link) {
				return in.Attr(docmodel.Link, "href"), true
			}
		}
	}
	return "", false
}

// TestExtractHrefRoundTrips checks that a link destination carrying
// markdown-significant characters — a parenthesis or a space — survives the
// HTML→markdown→parse round trip with its ORIGINAL destination intact. A bare
// "](dest)" destination truncates at an unbalanced ")" and drops everything
// after a space, silently corrupting the link.
func TestExtractHrefRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		href string
	}{
		{"parenthesized", "/wiki/Foo_(bar)"},
		{"with space", "/a b/c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte("<!doctype html><html><head></head><body>" +
				`<p>See <a href="` + tc.href + `">link</a> here.</p>` +
				"</body></html>")
			ex, err := Extract(src)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			doc, _, err := markdown.Parse(ex.Markdown)
			if err != nil {
				t.Fatalf("markdown.Parse rejected output: %v\n%s", err, ex.Markdown)
			}
			got, ok := findLinkHref(doc)
			if !ok {
				t.Fatalf("no Link-marked inline found; markdown:\n%s", ex.Markdown)
			}
			if got != tc.href {
				t.Errorf("link href = %q, want %q; markdown:\n%s", got, tc.href, ex.Markdown)
			}
		})
	}
}
