package markdown_test

import (
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

// FRONT MATTER IS THE ONE CONSTRUCT GALLEY NEITHER MODELLED NOR REFUSED.
//
// Measured on the tracked build (8a78350), `galley accept --all` over a file
// nobody had touched:
//
//	---                          ---
//	title: The Spec        ->
//	status: draft                ## title: The Spec status: draft owner: court
//	owner: court
//	---
//
// Four lines became one H2 heading and the closing delimiter was deleted. The
// cause was that goldmark was built with the Table and Footnote extensions
// only, so the opening "---" was a THEMATIC BREAK and the keys under it were a
// SETEXT HEADING underlined by the closing "---" — a reading in which nothing
// is unsupported, so the refusal machinery that protects footnotes, raw HTML,
// link references and mixed images never fired.
//
// Every offline write verb did it (accept --all, reject, decline, approve
// --all, suggest), and so did opening the document in a browser, because they
// all reach the file through Parse and Serialize.
//
// These assert the fix's whole contract: the block is MODELLED, it is carried
// BYTE-IDENTICALLY, and the fixed point Parse(Serialize(Parse(x))) == Parse(x)
// survives — including for the shapes where a "---" is NOT front matter.

// frontMatterCases are documents whose bytes must survive Parse -> Serialize
// unchanged. Byte identity is the assertion that matters here: front matter is
// metadata galley does not model the INSIDE of, so anything but the author's
// own bytes is a rewrite of a file nobody edited.
var frontMatterCases = []struct {
	name string
	src  string
	// want is the serialization when it is not src itself.
	want string
}{
	{
		name: "front matter alone",
		src:  "---\ntitle: The Spec\nstatus: draft\nowner: court\n---\n",
	},
	{
		name: "front matter and prose",
		src:  "---\ntitle: The Spec\n---\n\n# The Spec\n\nProse that must survive.\n",
	},
	{
		name: "toml front matter",
		src:  "+++\ntitle = \"The Spec\"\ndraft = true\n+++\n\n# The Spec\n",
	},
	{
		name: "toml front matter alone",
		src:  "+++\ntitle = \"The Spec\"\n+++\n",
	},
	{
		// EVERY LINE OF THIS IS MARKDOWN TO A MARKDOWN PARSER and none of it is
		// markdown. The list is YAML's, the "#" is inside a quoted string, the
		// "---" is a document separator inside the block, and the indentation
		// is the only thing that makes the keys mean anything.
		name: "front matter that looks like markdown",
		src:  "---\ntitle: \"# not a heading\"\nlist:\n  - a\n  - b\nbody: |\n  ## nor this\n  *not emphasis*\n---\n\nprose\n",
	},
	{
		name: "no blank line after the closing delimiter",
		src:  "---\ntitle: The Spec\n---\n# The Spec\n",
		// The gap between two blocks is the serializer's, everywhere: it writes
		// exactly one blank line between the front matter and the body, the same
		// normalization it already applies between any two blocks.
		want: "---\ntitle: The Spec\n---\n\n# The Spec\n",
	},
	{
		name: "windows line endings",
		src:  "---\r\ntitle: The Spec\r\n---\r\n\r\nprose\r\n",
		// The block's own bytes come back VERBATIM, carriage returns and all.
		// The body below it is the serializer's, as it always was, and is
		// written with the newlines it writes everywhere else.
		want: "---\r\ntitle: The Spec\r\n---\r\n\nprose\n",
	},
}

func TestFrontMatterSurvivesTheRoundTripByte(t *testing.T) {
	for _, c := range frontMatterCases {
		t.Run(c.name, func(t *testing.T) {
			want := c.want
			if want == "" {
				want = c.src
			}
			doc, _, err := markdown.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := string(markdown.Serialize(doc)); got != want {
				t.Errorf("Serialize(Parse(src)) rewrote the file\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestFrontMatterIsItsOwnBlock states WHERE it lives. A block at index 0 rather
// than a Doc field, because every mutation in this codebase rebuilds a Doc from
// its Blocks (suggest's clone, critic's extract, ydoc's Read) — a field beside
// them is a field every one of those has to remember, which is the shape of the
// review.File bug CLAUDE.md records.
func TestFrontMatterIsItsOwnBlock(t *testing.T) {
	for _, c := range []struct{ name, src, text string }{
		{"yaml", "---\ntitle: x\n---\n\nprose\n", "---\ntitle: x\n---\n"},
		{"toml", "+++\ntitle = \"x\"\n+++\n\nprose\n", "+++\ntitle = \"x\"\n+++\n"},
		{"alone", "---\ntitle: x\n---\n", "---\ntitle: x\n---\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(doc.Blocks) == 0 {
				t.Fatal("no blocks")
			}
			b := doc.Blocks[0]
			if b.Kind != docmodel.FrontMatter {
				t.Fatalf("first block is %q, want %q", b.Kind, docmodel.FrontMatter)
			}
			if b.Text != c.text {
				t.Errorf("front matter text\n got: %q\nwant: %q", b.Text, c.text)
			}
			// It carries no inline content, so nothing in the suggestion
			// pipeline can hang a mark on it — the same standing reason a code
			// fence is literal text.
			if len(b.Inlines) != 0 || len(b.Children) != 0 {
				t.Errorf("front matter carries inlines/children: %+v", b)
			}
		})
	}
}

// TestFrontMatterKeepsTheHeadingThatFollowsIt is the bug stated exactly. On the
// tracked build the keys became the heading; the real heading has to still be
// the first thing after the block.
func TestFrontMatterKeepsTheHeadingThatFollowsIt(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("---\ntitle: The Spec\nstatus: draft\n---\n\n# The Spec\n\nProse.\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	kinds := make([]docmodel.BlockKind, len(doc.Blocks))
	for i, b := range doc.Blocks {
		kinds[i] = b.Kind
	}
	want := []docmodel.BlockKind{docmodel.FrontMatter, docmodel.Heading, docmodel.Paragraph}
	if len(kinds) != len(want) {
		t.Fatalf("blocks %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("blocks %v, want %v", kinds, want)
		}
	}
	if lvl := doc.Blocks[1].Attrs["level"]; lvl != "1" {
		t.Errorf("heading level %q, want 1 — a level 2 here is the setext reading, which is the bug", lvl)
	}
}

// notFrontMatter is every "---" that is NOT front matter and must keep the
// meaning it has today. The fix is worthless if it takes the thematic break
// with it.
var notFrontMatter = []struct {
	name string
	src  string
	want string
}{
	{
		name: "a thematic break mid-document",
		src:  "Intro paragraph.\n\n---\n\nAfter the break.\n",
	},
	{
		name: "an unterminated opener is still a thematic break",
		src:  "---\n\nprose with no closing delimiter\n",
	},
	{
		// Serialize writes a Rule as "---", so two leading rules would come
		// back as an opener and a closer with a blank line between them. An
		// EMPTY front matter block carries no metadata and is therefore not
		// front matter — which is what keeps this a fixed point.
		name: "two leading thematic breaks",
		src:  "***\n\n***\n",
		want: "---\n\n---\n",
	},
	{
		// The case the empty-body rule does NOT cover: a leading rule, prose,
		// and another rule. Serialized naively the file reads back as front
		// matter with "prose" inside it. The leading marker is written "***"
		// instead — measured against the bytes rather than reasoned about.
		name: "a leading rule with a rule after it",
		src:  "***\n\nprose\n\n***\n",
		want: "***\n\nprose\n\n---\n",
	},
	{
		// A fence's contents are literal text and the front matter scanner
		// walks raw lines, so a "---" inside one could close an opener above
		// it. Same guard, reached a different way.
		name: "a leading rule and a fence holding a delimiter",
		src:  "***\n\n```sh\n---\n```\n",
		want: "***\n\n```sh\n---\n```\n",
	},
	{
		name: "a setext heading that is not at the top of the file",
		src:  "prose\n\nThe Title\n---\n",
		want: "prose\n\n## The Title\n",
	},
	{
		name: "an empty front matter block is two thematic breaks",
		src:  "---\n---\n",
		want: "---\n\n---\n",
	},
	{
		name: "plus signs that never close",
		src:  "+++\ntitle = \"x\"\n",
		want: "+++ title = \"x\"\n",
	},
}

func TestWhatIsNotFrontMatterStillIsNot(t *testing.T) {
	for _, c := range notFrontMatter {
		t.Run(c.name, func(t *testing.T) {
			want := c.want
			if want == "" {
				want = c.src
			}
			doc, _, err := markdown.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			for i, b := range doc.Blocks {
				if b.Kind == docmodel.FrontMatter {
					t.Fatalf("block %d is front matter, and this document has none: %q", i, b.Text)
				}
			}
			if got := string(markdown.Serialize(doc)); got != want {
				t.Errorf("Serialize(Parse(src))\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestFrontMatterIsAFixedPoint is the property everything else in this package
// rests on, stated over every case in this file: parsing the serialization of a
// parse yields the same document. It is the check that catches a serializer
// whose output the parser reads differently — which is precisely how a leading
// thematic break could turn into front matter.
func TestFrontMatterIsAFixedPoint(t *testing.T) {
	var srcs []string
	for _, c := range frontMatterCases {
		srcs = append(srcs, c.src)
	}
	for _, c := range notFrontMatter {
		srcs = append(srcs, c.src)
	}
	for _, src := range srcs {
		doc, _, err := markdown.Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		out := markdown.Serialize(doc)
		again, _, err := markdown.Parse(out)
		if err != nil {
			t.Fatalf("Parse(Serialize(Parse(%q))): %v", src, err)
		}
		if !docmodel.Equal(doc, again) {
			t.Errorf("not a fixed point for %q — serialized to %q", src, out)
		}
	}
}

// TestFrontMatterDoesNotMoveTheLineNumberInARefusal. The refusal machinery
// names a SOURCE line, and stripping the front matter before goldmark sees the
// rest would renumber every line under it — so a document galley refuses would
// name a line the author has to count backwards from.
func TestFrontMatterDoesNotMoveTheLineNumberInARefusal(t *testing.T) {
	// The raw HTML is on line 8 of the file.
	src := "---\ntitle: x\nstatus: draft\n---\n\nprose\n\n<div>raw</div>\n"
	_, _, err := markdown.Parse([]byte(src))
	if err == nil {
		t.Fatal("raw HTML was not refused")
	}
	if !strings.Contains(err.Error(), "line 8") {
		t.Errorf("refusal names the wrong line: %v", err)
	}
}
