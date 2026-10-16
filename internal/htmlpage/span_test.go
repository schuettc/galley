package htmlpage

import (
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/markdown"
)

// TestExtractSpanIsTransparent checks that a paragraph whose prose is wrapped
// in styling spans is content: the span text is kept and the span itself is
// dropped, and the resulting markdown is accepted by markdown.Parse.
func TestExtractSpanIsTransparent(t *testing.T) {
	src := []byte(`<!doctype html><html><head></head><body>` +
		`<p class="sub">the <span class="k">galley</span> cli</p>` +
		`</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	md := string(ex.Markdown)
	if !strings.Contains(md, "the galley cli") {
		t.Errorf("span text not kept flat:\n%s", md)
	}
	if strings.Contains(md, "span") {
		t.Errorf("span wrapper leaked into markdown:\n%s", md)
	}
	if _, _, err := markdown.Parse(ex.Markdown); err != nil {
		t.Errorf("markdown.Parse rejected extractor output: %v\n%s", err, md)
	}
}

// TestExtractSpanInPreFlattened confirms that a span inside a <pre> is
// flattened to text: a <pre> is now editable code (Move 1), not a verbatim
// shell leaf, so its content is textContent with the span dropped and the
// span markup does not survive into the fenced block.
func TestExtractSpanInPreFlattened(t *testing.T) {
	src := []byte(`<!doctype html><html><head></head><body>` +
		`<pre class="term"><span class="k">galley</span> edit</pre>` +
		`<p>after</p>` +
		`</body></html>`)
	ex, err := Extract(src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	md := string(ex.Markdown)
	if !strings.Contains(md, "```\ngalley edit\n```") {
		t.Errorf("pre not recovered as a fenced block with flattened text:\n%s", md)
	}
	if strings.Contains(md, "span") {
		t.Errorf("span wrapper leaked into markdown:\n%s", md)
	}
	if _, _, err := markdown.Parse(ex.Markdown); err != nil {
		t.Errorf("markdown.Parse rejected extractor output: %v\n%s", err, md)
	}
}
