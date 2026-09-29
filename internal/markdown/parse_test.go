package markdown_test

import (
	"os"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func para(inlines ...docmodel.Inline) docmodel.Block {
	return docmodel.Block{Kind: docmodel.Paragraph, Inlines: inlines}
}

func text(s string) docmodel.Inline {
	return docmodel.Inline{Text: s}
}

// leadingPara builds a ListItem with the leading Paragraph TipTap's
// `paragraph block*` requires and markdown.legalize therefore writes. Every
// fixture that hand-builds a list item and then compares it against a Parse
// goes through it, because an item without that paragraph is a document Parse
// cannot produce and the BROWSER DELETES — it is not a shape a round-trip
// property can be stated over. It costs no byte on the way out: an empty
// paragraph renders to nothing and renderedBlocks drops it.
func leadingPara(children ...docmodel.Block) docmodel.Block {
	if len(children) > 0 && children[0].Kind == docmodel.Paragraph {
		return docmodel.Block{Kind: docmodel.ListItem, Children: children}
	}
	return docmodel.Block{
		Kind:     docmodel.ListItem,
		Children: append([]docmodel.Block{{Kind: docmodel.Paragraph}}, children...),
	}
}

func marked(s string, marks ...docmodel.Mark) docmodel.Inline {
	return docmodel.Inline{Text: s, Marks: marks}
}

func mark(kind docmodel.MarkKind) docmodel.Mark {
	return docmodel.Mark{Kind: kind}
}

func linkMark(href string) docmodel.Mark {
	return docmodel.Mark{Kind: docmodel.Link, Attrs: map[string]string{"href": href}}
}

func hardBreak() docmodel.Inline {
	return docmodel.Inline{Marks: []docmodel.Mark{{Kind: docmodel.HardBreak}}}
}

func TestParse_Fixtures(t *testing.T) {
	tests := []struct {
		name string
		path string
		want docmodel.Doc
	}{
		{
			name: "basic",
			path: "testdata/basic.md",
			want: docmodel.Doc{Blocks: []docmodel.Block{
				{
					Kind:    docmodel.Heading,
					Attrs:   map[string]string{"level": "1"},
					Inlines: []docmodel.Inline{text("Title")},
				},
				para(
					text("This is "),
					marked("bold", mark(docmodel.Bold)),
					text(" and "),
					marked("italic", mark(docmodel.Italic)),
					text(" and "),
					marked("code", mark(docmodel.Code)),
					text(" and a "),
					marked("link", linkMark("https://example.com")),
					text("."),
				),
				para(
					text("Line one"),
					hardBreak(),
					text("Line two"),
				),
			}},
		},
		{
			name: "blocks",
			path: "testdata/blocks.md",
			want: docmodel.Doc{Blocks: []docmodel.Block{
				{
					Kind: docmodel.BulletList,
					Children: []docmodel.Block{
						{
							Kind:     docmodel.ListItem,
							Children: []docmodel.Block{para(text("Item one"))},
						},
						{
							Kind: docmodel.ListItem,
							Children: []docmodel.Block{
								para(text("Item two")),
								{
									Kind: docmodel.BulletList,
									Children: []docmodel.Block{
										{
											Kind:     docmodel.ListItem,
											Children: []docmodel.Block{para(text("Nested item"))},
										},
									},
								},
							},
						},
					},
				},
				{
					Kind: docmodel.OrderedList,
					Children: []docmodel.Block{
						{
							Kind:     docmodel.ListItem,
							Children: []docmodel.Block{para(text("First item"))},
						},
						{
							Kind:     docmodel.ListItem,
							Children: []docmodel.Block{para(text("Second item"))},
						},
					},
				},
				{
					Kind: docmodel.Blockquote,
					Children: []docmodel.Block{
						para(text("A quote paragraph.")),
						{
							Kind: docmodel.BulletList,
							Children: []docmodel.Block{
								{
									Kind:     docmodel.ListItem,
									Children: []docmodel.Block{para(text("Quoted item"))},
								},
							},
						},
					},
				},
				{
					Kind:  docmodel.CodeBlock,
					Attrs: map[string]string{"language": "go"},
					Text:  "fmt.Println(\"hi\")\n",
				},
				{Kind: docmodel.Rule},
				{
					Kind: docmodel.Image,
					Attrs: map[string]string{
						"src": "https://example.com/img.png",
						"alt": "Alt text",
					},
				},
			}},
		},
		{
			name: "inline",
			path: "testdata/inline.md",
			want: docmodel.Doc{Blocks: []docmodel.Block{
				para(
					marked("bold", mark(docmodel.Bold)),
					marked("italic", mark(docmodel.Italic)),
					text(" and "),
					marked("code", mark(docmodel.Code)),
					marked("bold again", mark(docmodel.Bold)),
					text(" adjacent."),
				),
				para(
					marked("Wait, really?!", mark(docmodel.Italic)),
					text(" spans punctuation."),
				),
				para(
					text("café ☕ 😀 unicode, "),
					marked("bold café ☕", mark(docmodel.Bold)),
					text(" and "),
					marked("italic 😀 too", mark(docmodel.Italic)),
					text("."),
				),
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := mustRead(t, tc.path)
			got, _, err := markdown.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !docmodel.Equal(got, tc.want) {
				t.Errorf("Parse(%s) mismatch:\n got:  %#v\n want: %#v", tc.path, got, tc.want)
			}
		})
	}
}

// TestParse_MergesAdjacentInlinesWithSoftBreak exercises the "merge
// adjacent inlines with identical mark sets" behavior directly: a soft
// line break inside an emphasis span produces two goldmark Text nodes
// (one per source line) that share the same mark set and must collapse
// into a single Inline. None of the fixtures above have two adjacent
// inlines with identical marks, so without this test a broken
// sameMarks/mergeAdjacent could pass the whole suite.
func TestParse_MergesAdjacentInlinesWithSoftBreak(t *testing.T) {
	src := []byte("*emphasis\nspans a line* rest.\n")
	got, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("emphasis spans a line", mark(docmodel.Italic)),
			text(" rest."),
		),
	}}
	if !docmodel.Equal(got, want) {
		t.Errorf("Parse mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// The refusal this phase removes. galley declined its own design spec on
// exactly this shape; the test that asserted the refusal is now the test that
// asserts it is gone.
func TestParse_TableNoLongerErrors(t *testing.T) {
	src := []byte("intro\n\n| a | b |\n| - | - |\n| 1 | 2 |\n")
	doc, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n := len(doc.Blocks); n != 2 {
		t.Fatalf("want a paragraph and a table, got %d blocks", n)
	}
	if k := doc.Blocks[1].Kind; k != docmodel.Table {
		t.Errorf("second block = %q, want %q", k, docmodel.Table)
	}
}

func TestParse_RawHTML_Errors(t *testing.T) {
	src := []byte("intro\n\n<div>\nhello\n</div>\n")
	_, _, err := markdown.Parse(src)
	if err == nil {
		t.Fatal("Parse: want error for raw HTML, got nil")
	}
	if !strings.Contains(err.Error(), "raw HTML") {
		t.Errorf("Parse error = %q, want it to mention %q", err.Error(), "raw HTML")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("Parse error = %q, want it to mention %q", err.Error(), "line 3")
	}
}

func TestParse_Footnote_Errors(t *testing.T) {
	src := []byte("intro[^1]\n\n[^1]: a footnote\n")
	_, _, err := markdown.Parse(src)
	if err == nil {
		t.Fatal("Parse: want error for footnote, got nil")
	}
	if !strings.Contains(err.Error(), "footnote") {
		t.Errorf("Parse error = %q, want it to mention %q", err.Error(), "footnote")
	}
}

func TestParse_ImageMixedWithText_Errors(t *testing.T) {
	src := []byte("before ![alt](https://example.com/i.png) after\n")
	_, _, err := markdown.Parse(src)
	if err == nil {
		t.Fatal("Parse: want error for image mixed with text, got nil")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("Parse error = %q, want it to mention %q", err.Error(), "line 1")
	}
}

// TestParse_CodeSpanLineEndingsBecomeSpaces is FuzzRoundTrip's seventh
// crasher, minimized from "`0 0\n“0`", and unlike the others it is a Parse
// bug rather than a Serialize one: Parse was reporting content the source
// does not mean.
//
// CommonMark converts a line ending inside a code span to a space —
// "`a\nb`" is the content "a b", and goldmark's own renderer agrees. Parse
// read the raw source bytes instead and kept the newline, so the document
// model held a code span markdown has no way to write back. Serialize wrote
// the newline out, and the second line of the paragraph then began with the
// code span's own "```" fence, which is a FENCED CODE BLOCK opener. The
// paragraph became a code block and the rest of the line became its info
// string, of which markdown keeps only the first word — so the text was
// gone.
//
// The fence is what makes this safe once the newline is gone: a fence of
// three or more backticks only happens when the content holds a run of two,
// and a backtick anywhere after a backtick fence disqualifies the line as
// an opener. It is only a line ending inside the span that can put a bare
// "```" at the start of a line.
func TestParse_CodeSpanLineEndingsBecomeSpaces(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"a soft line ending", "`a\nb`\n", "a b"},
		{"trailing spaces are kept, the newline is a space", "`a  \nb`\n", "a   b"},
		{"the fuzz crasher", "`0 0\n``0`\n", "0 0 ``0"},
		{"no line ending, no change", "`a b`\n", "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			inlines := doc.Blocks[0].Inlines
			if len(inlines) != 1 || inlines[0].Text != tc.want {
				t.Fatalf("Parse(%q) inlines = %#v, want one code span %q", tc.src, inlines, tc.want)
			}
			// And the content must now be writable: no line ending means no
			// line can start with the span's own fence.
			out := markdown.Serialize(doc)
			again, _, err := markdown.Parse(out)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", out, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("the code span did not survive %q:\n got:  %#v\n want: %#v", out, again, doc)
			}
		})
	}
}
