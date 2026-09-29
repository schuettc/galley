package markdown_test

import (
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// spellings is every arrangement that produced a list item whose first child is
// not a paragraph, or a blockquote with no children at all. Each one is a
// document the browser used to DELETE A NODE FROM on open, with no keystroke,
// and the projection wrote the deletion to the author's file.
//
// They are held in one table because they are ONE BUG. Eighteen were confirmed
// by hand before the generic gate existed; `just schema` reaches every one of
// them and more from a generated cross product, and this table is the Go-side
// statement of the same invariant so that `just verify` alone cannot go green
// on a tree that has lost it.
//
// EVERY ENTRY IS ITS OWN CANONICAL SERIALIZATION, which is the second half of
// what these assert. THE ROUND-TRIP OBJECTION WAS NEVER REAL: an empty
// Paragraph renders to nothing, renderedBlocks drops a block that renders to
// nothing, and a list marker and a blockquote's ">" are written whether or not
// the body renders anything. #63 recorded the objection as real for a list item
// — "prepending a paragraph in Parse would move the author's bytes" — without
// measuring it, and it is the same objection markdown.tableRow's comment
// records being wrong about a blank cell.
//
// THREE ENTRIES CARRY A `want` AND EVERY ONE OF THEM IS A PRE-EXISTING
// NORMALIZATION, measured against the tracked build and byte-identical to what
// it wrote. They are recorded rather than dropped, because an entry quietly
// removed for "not round-tripping" is an entry removed from the coverage of the
// bug this table is about.
var spellings = []struct {
	name string
	src  string
	// want is the serialization when it is not src itself.
	want string
}{
	{name: "a fence first", src: "- alpha\n- ```sh\n  brew install galley\n  ```\n- gamma\n"},
	{name: "a fence first, ordered", src: "1. alpha\n2. ```sh\n   brew install galley\n   ```\n3. gamma\n"},
	{name: "indented code first", src: "- alpha\n- ```\n  four space code\n  ```\n- gamma\n"},
	{name: "a heading first", src: "- alpha\n- # a heading\n- gamma\n"},
	{name: "a deeper heading first", src: "- alpha\n- ### a heading\n- gamma\n"},
	{name: "a blockquote first", src: "- alpha\n- > quoted words\n- gamma\n"},
	{name: "a nested bullet list first", src: "- alpha\n- - nested\n- gamma\n"},
	{name: "a nested ordered list first", src: "- alpha\n- 1. nested\n- gamma\n"},
	{name: "a table first", src: "- alpha\n- | h1 | h2 |\n  | --- | --- |\n  | a | b |\n- gamma\n"},
	{name: "an image first", src: "- alpha\n- ![diagram](pic.png)\n- gamma\n"},
	// goldmark reads "- ---" as a thematic break that ENDS the list, so the
	// rule is a sibling of it rather than a child. Pre-existing, unchanged.
	{name: "a rule first", src: "- alpha\n- ---\n- gamma\n", want: "- alpha\n\n---\n\n- gamma\n"},
	{name: "a note first", src: "- alpha\n- {>>a note<<}\n- gamma\n"},
	{name: "a document note first", src: "- alpha\n- {>>@document a note<<}\n- gamma\n"},
	// Two notes on one line become two Note BLOCKS, which are written on two
	// lines. Pre-existing (extractCriticBlocks' "nothing survived" rule),
	// unchanged.
	{name: "two notes first", src: "- alpha\n- {>>one<<} {>>two<<}\n- gamma\n", want: "- alpha\n- {>>one<<}\n\n  {>>two<<}\n- gamma\n"},
	{name: "a bare bullet marker", src: "- alpha\n-\n- gamma\n"},
	{name: "a bare ordered marker", src: "1. alpha\n2.\n3. gamma\n"},
	{name: "an empty fence first", src: "- alpha\n- ```\n  ```\n- gamma\n"},
	// The blank line inside the item is not needed to keep the ">" off the
	// paragraph above it, so renderBlocksTight does not write one.
	// Pre-existing, unchanged.
	{name: "an empty blockquote inside an item", src: "- alpha\n- a leading paragraph\n\n  >\n- gamma\n", want: "- alpha\n- a leading paragraph\n  >\n- gamma\n"},
	// S3, the same family with no list in it at all.
	{name: "a bare blockquote", src: "# Notes\n\n>\n\nTail.\n"},
	{name: "a bare blockquote inside a quote", src: "> a leading quoted paragraph\n>\n> >\n"},
	// S2, the cascade: the item is the list's only item and the list is the
	// blockquote's only child, so losing the item used to leave NOTHING.
	{name: "the whole cascade", src: "> - ```sh\n>   ./deploy --now\n>   ```\n"},
	// THE WORST CONFIRMED CASE. Two steps and both commands gone, and the
	// survivor renumbered 3 -> 1, so the file read as a complete one-step
	// procedure.
	{
		name: "the install list",
		src:  "# Install\n\n1. ```sh\n   brew install galley\n   ```\n2. ```sh\n   galley edit doc.md\n   ```\n3. done\n",
	},
}

// TestEveryListItemOpensWithAParagraph is the invariant TipTap's `listItem`
// content rule states — `paragraph block*`, which is STRICTER than block+ and
// is why "does it have children" is not the question.
//
// y-prosemirror does not skip a node it cannot build: createNodeFromYElement
// calls schema.node, which is createChecked and throws, and the catch deletes
// el._item out of the Y doc. The deletion broadcasts, EditServer's OnUpdate
// fires, touch() schedules Project(), and the author's file is rewritten.
//
// A GO GOLDEN FILE CANNOT SEE THIS BUG and no testdata fixture is added for it,
// for markdown.tableRow's reason one shape over: Serialize is UNCHANGED by the
// fix, so `Serialize(Parse(x)) == x` passed for every spelling above before it
// too. Only a check that reads the SHAPE (here), the fragment (internal/ydoc)
// or the bytes after a real browser has opened the file (web/schema.mjs's
// corpus, and the measurement in .superpowers/schema-legal-report.md) can fail
// against the bug.
func TestEveryListItemOpensWithAParagraph(t *testing.T) {
	for _, tc := range spellings {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			docmodel.Walk(doc, func(path []int, b *docmodel.Block) {
				if b.Kind != docmodel.ListItem {
					return
				}
				if len(b.Children) == 0 {
					t.Errorf("listItem at %v has no children; `paragraph block*` needs one", path)
					return
				}
				if got := b.Children[0].Kind; got != docmodel.Paragraph {
					t.Errorf("listItem at %v opens with %q, want %q — the browser deletes it",
						path, got, docmodel.Paragraph)
				}
			})
		})
	}
}

// TestEveryBlockquoteHoldsABlock is S3: `blockquote` is `block+`, and a bare
// ">" is a Blockquote goldmark hands back with no children at all. Confirmed
// deleted at every position.
func TestEveryBlockquoteHoldsABlock(t *testing.T) {
	for _, tc := range spellings {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			docmodel.Walk(doc, func(path []int, b *docmodel.Block) {
				if b.Kind == docmodel.Blockquote && len(b.Children) == 0 {
					t.Errorf("blockquote at %v has no children; `block+` needs one", path)
				}
			})
		})
	}
}

// TestMakingTheShapeLegalMovesNoBytes is the measurement #63 declined to take.
// Every spelling above is written back BYTE-IDENTICALLY, so the shape the
// browser needs costs the author nothing — no marker, no blank line, no
// whitespace. The same was checked across the whole `just schema` corpus and
// every .md this repository ships: 379 of 399 inputs serialize to the byte they
// did before the fix, and the 20 that differ are the empty-heading and
// note-heading cases, both of which STOP writing something (see
// TestAnEmptyHeadingWritesNoTrailingSpace and
// TestANoteThatIsTheWholeOfAHeadingKeepsItsMarkers).
//
// IT PASSES AGAINST THE TRACKED BUILD TOO, AND THAT IS THE WHOLE POINT — it is
// not a discriminator and must not be read as one. Serialize cannot see this
// bug; what this asserts is that the fix did not introduce a SECOND one. The
// checks that go red are TestEveryListItemOpensWithAParagraph (18 of its
// subtests), TestEveryBlockquoteHoldsABlock (3), and internal/ydoc's
// TestEveryListItemCrossesWithAParagraphFirst.
func TestMakingTheShapeLegalMovesNoBytes(t *testing.T) {
	for _, tc := range spellings {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			want := tc.want
			if want == "" {
				want = tc.src
			}
			if got := string(markdown.Serialize(doc)); got != want {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, want)
			}
			again, _, err := markdown.Parse([]byte(want))
			if err != nil {
				t.Fatalf("Parse [round 2]: %v", err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("not a fixed point")
			}
		})
	}
}

// TestTheInstallListKeepsBothCommands names the harm rather than the shape,
// because the shape is what a future reader will be tempted to "simplify" and
// the harm is what stops them.
//
// Measured against the tracked build in a real chromium, opening this document
// and pressing NOTHING: 143 bytes became 66, both `brew install galley` and
// `galley edit doc.md` gone, and `3. done` renumbered to `1. done`. That is
// worse than visible corruption — the file still reads as a complete one-step
// procedure.
func TestTheInstallListKeepsBothCommands(t *testing.T) {
	const src = "# Install\n\n1. ```sh\n   brew install galley\n   ```\n2. ```sh\n   galley edit doc.md\n   ```\n3. done\n"
	doc, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var fences []string
	docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
		if b.Kind == docmodel.CodeBlock {
			fences = append(fences, strings.TrimSpace(b.Text))
		}
	})
	want := []string{"brew install galley", "galley edit doc.md"}
	if len(fences) != len(want) {
		t.Fatalf("got %d fences %q, want %q", len(fences), fences, want)
	}
	for i := range want {
		if fences[i] != want[i] {
			t.Errorf("fence %d = %q, want %q", i, fences[i], want[i])
		}
	}
	list := doc.Blocks[1]
	if list.Kind != docmodel.OrderedList || len(list.Children) != 3 {
		t.Fatalf("the list is %q with %d items, want an orderedList with 3", list.Kind, len(list.Children))
	}
	// AND EACH ITEM IS BUILDABLE. Without this the test passes against the
	// tracked build — every fence is in the model there too; the loss happens
	// in the browser — and a check that cannot fail is worse than no check.
	for i, item := range list.Children {
		if len(item.Children) == 0 || item.Children[0].Kind != docmodel.Paragraph {
			t.Errorf("item %d does not open with a paragraph, so the browser deletes it with its command inside", i+1)
		}
	}
	if got := string(markdown.Serialize(doc)); got != src {
		t.Errorf("Serialize = %q, want the file unchanged", got)
	}
}

// TestANoteThatIsTheWholeOfAHeadingKeepsItsMarkers is S4.
//
// `# {>>write the title here<<}` came back `# `: criticPass emptied the
// heading, the note was lifted as a RANGE comment anchored to an empty string,
// and the author's words left the .md entirely — with trailing whitespace
// written in their place. renderCellNote's comment names that outcome "the
// WORST of the three".
//
// The heading is not promoted to a Note block: that would delete the heading,
// which critic.go correctly calls the same class of loss pointing the other
// way. It is not refused either — `unsupported` is for constructs the MODEL
// cannot represent, and this one it represents byte for byte. So the markers
// STAY: a {>>…<<} with no anchor to be given is not a comment, it is text, and
// the words remain where the author typed them and where the zero-tooling
// promise reads them from.
func TestANoteThatIsTheWholeOfAHeadingKeepsItsMarkers(t *testing.T) {
	for _, src := range []string{
		"# {>>write the title here<<}\n",
		"### {>>write the title here<<}\n",
		"# {>>one<<} {>>two<<}\n",
		"- alpha\n- # {>>write the title here<<}\n- gamma\n",
	} {
		doc, comments, err := markdown.Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		if got := string(markdown.Serialize(doc)); got != src {
			t.Errorf("Serialize(Parse(%q)) = %q — the author's words moved", src, got)
		}
		for _, c := range comments {
			t.Errorf("Parse(%q) lifted %q out of the file with nothing to anchor it to", src, c.Text)
		}
	}
	// A note that is only PART of a heading is unaffected: there is text to
	// anchor to, so it is lifted as a range comment exactly as before.
	doc, comments, err := markdown.Parse([]byte("## title {>>why<<}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 1 || comments[0].Text != "why" {
		t.Fatalf("comments = %+v, want one \"why\"", comments)
	}
	if got := string(markdown.Serialize(doc)); got != "## title\n" {
		t.Errorf("Serialize = %q, want %q", got, "## title\n")
	}
}

// TestAnEmptyHeadingWritesNoTrailingSpace is the other half of S4's measured
// outcome. The space after the hashes is a SEPARATOR, and with nothing on the
// far side of it there is nothing to separate — so writing it puts trailing
// whitespace into somebody's file, which the first projection then saves back
// with no keystroke.
func TestAnEmptyHeadingWritesNoTrailingSpace(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"#\n", "#\n"},
		{"# \n", "#\n"},
		{"###\n", "###\n"},
		{"> #\n", "> #\n"},
		{"- alpha\n- #\n- gamma\n", "- alpha\n- #\n- gamma\n"},
	} {
		doc, _, err := markdown.Parse([]byte(tc.src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		if got := string(markdown.Serialize(doc)); got != tc.want {
			t.Errorf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.want)
		}
	}
}
