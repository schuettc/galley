package markdown_test

import (
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// TestTheLineBreakDialectsAreNeverReflowed is the check the four measured
// defects were found without.
//
// FOUR DIALECTS, ONE BUG: each is a plain paragraph to CommonMark, and the
// converter turns a soft line break into a space. That is deliberate for prose
// and it destroys these. Run red against the tracked build — with math.go and
// dialects.go deleted — every row reports the reflowed one-liner in `got`, from
// `Serialize(Parse(src))` alone, which is what a `galley suggest` or a
// projection writes to the author's file.
//
// The claim is the BYTES, not the tree: a construct that reads back byte for
// byte is one nothing has happened to, and a construct that refuses has not
// been written over. There is no third acceptable outcome, and "one line where
// there were four" is the second one this table exists to fail on.
func TestTheLineBreakDialectsAreNeverReflowed(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// refuse is the substring the refusal must name, or "" if the
		// construct is modelled and must survive byte-identical.
		refuse string
	}{{
		name: "display math is modelled and carried verbatim",
		src:  "$$\nE = mc^2\n$$\n",
	}, {
		name: "display math under prose keeps its own lines",
		src:  "Some prose.\n\n$$\n\\alpha\n\\beta\n$$\n",
	}, {
		name:   "a definition list is refused",
		src:    "Apple\n: A fruit that grows on trees.\n",
		refuse: "definition list not supported (line 2)",
	}, {
		name:   "a ::: directive is refused",
		src:    "# Head\n\n:::note\nThis is an admonition body.\nIt has two lines.\n:::\n",
		refuse: "::: directive block not supported (line 3)",
	}, {
		name:   "a > [!NOTE] callout is refused",
		src:    "> [!NOTE]\n> Useful information that users should know.\n",
		refuse: "> [!NOTE] callout not supported (line 1)",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(c.src))
			if c.refuse != "" {
				if err == nil {
					t.Fatalf("Parse accepted it and wrote %q — a reflow is not an outcome",
						markdown.Serialize(doc))
				}
				if !strings.Contains(err.Error(), c.refuse) {
					t.Fatalf("refusal = %q, want it to name %q", err, c.refuse)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := string(markdown.Serialize(doc)); got != c.src {
				t.Errorf("the bytes changed:\n got:  %q\n want: %q", got, c.src)
			}
		})
	}
}

// TestParse_DisplayMathInterruptsAParagraph is the shape a golden fixture
// CANNOT hold, because it is not a fixed point: the serializer writes a blank
// line between two blocks, so a math block written flush under a sentence comes
// back with one and the file is not byte-identical. The claim is about the
// TREE — that the paragraph ended — and it is the one shape the reflow bug is
// worst in, because there is no blank line to hint that two things are meant.
func TestParse_DisplayMathInterruptsAParagraph(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("Some prose.\n$$\nE = mc^2\n$$\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2: %#v", len(doc.Blocks), doc.Blocks)
	}
	if doc.Blocks[0].Kind != docmodel.Paragraph || doc.Blocks[1].Kind != docmodel.MathBlock {
		t.Fatalf("kinds = %s, %s", doc.Blocks[0].Kind, doc.Blocks[1].Kind)
	}
	if got := doc.Blocks[1].Text; got != "$$\nE = mc^2\n$$\n" {
		t.Errorf("math text = %q, want the block verbatim including both delimiters", got)
	}
}

// TestParse_UnterminatedDisplayMathIsAParagraph is front matter's third rule
// for this construct: a delimiter that never closes is not a block, and the
// line goes back to meaning what it always meant. Without it a stray "$$" would
// turn every line under it into literal TeX — unreviewable prose, which is the
// harm the refusals above exist to avoid.
func TestParse_UnterminatedDisplayMathIsAParagraph(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("$$\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != docmodel.Paragraph {
		t.Fatalf("blocks = %#v, want one paragraph", doc.Blocks)
	}
	if got := string(markdown.Serialize(doc)); got != "$$\n" {
		t.Errorf("Serialize = %q, want the author's own line back", got)
	}
}

// TestParse_TheDialectTestsAreExact is the other half of every refusal: a false
// refusal is a document that will not open, so each detector has to be shown
// NOT firing on the ordinary text nearest to it.
func TestParse_TheDialectTestsAreExact(t *testing.T) {
	ok := []struct{ name, src string }{
		{"a doubled dollar in prose", "It costs $$5 and\nthe line goes on.\n"},
		{"a colon with no space after it", "Rule\n:no space, so not a definition.\n"},
		{"a colon opening the FIRST line", ": not a definition list, just a colon.\n"},
		{"an alert marker outside a blockquote", "[!NOTE]\nnot GitHub's callout.\n"},
		{"an alert marker that is not alone on its line", "> [!NOTE] and more on the same line.\n"},
		{"a lowercase bracket-bang", "> [!note]\n> not the marker GitHub reads.\n"},
		{"two colons", "::not a directive\nand a second line.\n"},
		{"a directive inside a fence", "```\n:::note\n```\n"},
		{"a definition marker inside a fence", "```\nTerm\n: def\n```\n"},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := markdown.Parse([]byte(c.src)); err != nil {
				t.Fatalf("refused ordinary text: %v", err)
			}
		})
	}
}

// TestParse_DisplayMathAfterAPartlyConsumedTab is the crash galley 0.12.0
// shipped with: `galley edit` on the seven bytes "1. >\t$$" panicked with a
// slice out of range, and a running editor parses every outside save, so a
// crash there is a dead editor and the reviewer's unsaved work with it.
//
// THE CAUSE IS A TAB, NOT A NEWLINE. A blockquote's ">" takes one column of
// the tab after it and leaves the rest as the line's PADDING, and the line
// PeekLine hands a block parser starts with that padding as spaces. An index
// into that line is therefore Padding bytes ahead of the source, and the math
// parser used it as a source offset. With no newline to absorb the overshoot
// the start passed the end; with one, the block held the wrong bytes — the
// "$$" delimiters went missing from the file, and the closing line's leftover
// opened blocks of its own.
func TestParse_DisplayMathAfterAPartlyConsumedTab(t *testing.T) {
	t.Run("the crashing inputs parse", func(t *testing.T) {
		for _, src := range []string{"1. >\t$$", "a\n1. >\t$$", ">\t$$"} {
			doc, _, err := markdown.Parse([]byte(src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			if got := string(markdown.Serialize(doc)); !strings.Contains(got, "$$") {
				t.Errorf("Parse(%q) lost the author's \"$$\": Serialize = %q", src, got)
			}
		}
	})
	cases := []struct{ name, src, want string }{
		{"in a quote", ">\t$$\n>\tx\n>\t$$\n", "$$\n  x\n$$\n"},
		{"in a quote in a list", "1. >\t$$\n   >\tx\n   >\t$$\n", "$$\n   x\n$$\n"},
		{"two in a row", ">\t$$\n>\tx\n>\t$$\n>\t$$\n>\ty\n>\t$$\n", "$$\n  x\n$$\n|$$\n  y\n$$\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			var maths []string
			others := 0
			docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
				switch b.Kind {
				case docmodel.MathBlock:
					maths = append(maths, b.Text)
				case docmodel.Paragraph:
					// A list item's leading empty paragraph is the schema's
					// (listItem is `paragraph block*`), not the math's.
					if len(b.Inlines) > 0 {
						others++
					}
				}
			})
			if got := strings.Join(maths, "|"); got != c.want {
				t.Errorf("math text = %q, want %q", got, c.want)
			}
			if others != 0 {
				t.Errorf("%d stray paragraph(s) beside the math: %#v", others, doc.Blocks)
			}
		})
	}
	t.Run("a lone opener under a tab is a paragraph that keeps its dollars", func(t *testing.T) {
		doc, _, err := markdown.Parse([]byte("1. >\t$$\nx\n$$\n"))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got := strings.Count(string(markdown.Serialize(doc)), "$$"); got != 2 {
			t.Errorf("Serialize = %q, want both \"$$\" lines kept", markdown.Serialize(doc))
		}
	})
}
