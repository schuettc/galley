package markdown

import (
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
)

// TestOpeningADocumentChangesNothing is the incident this file exists for.
//
// Measured 2026-08-22 against the shipped binary: `galley edit` on a document
// spelled this way, no browser attached and no key pressed, then quit — and
// cmd/galley's shutdown Flush rewrote six separate things, none of which
// anybody asked for.
func TestOpeningADocumentChangesNothing(t *testing.T) {
	const src = `Setext Heading
==============

A hard wrapped
paragraph over
three lines.

* one
* two

__bold__ and _em_.
`
	doc, _, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	got := string(SerializeOnto(doc, []byte(src)))
	if got != src {
		t.Errorf("opening a document rewrote it.\n--- was ---\n%s\n--- now ---\n%s", src, got)
	}
	// The half that says the check is worth anything: the plain serializer
	// really does change every one of these, so a green result above is the
	// preservation working and not the fixture being canonical already.
	if plain := string(Serialize(doc)); plain == src {
		t.Fatal("the fixture is already canonical — this test cannot fail")
	}
}

// TestOnlyTheEditedBlockIsRewritten is the property that made the no-op skip
// the wrong fix. Skipping an unchanged write leaves this case untouched: one
// edit, and every other paragraph in the file is reformatted around it.
func TestOnlyTheEditedBlockIsRewritten(t *testing.T) {
	const src = `A hard wrapped
paragraph that
nobody touched.

* untouched list
* second item

Another wrapped
paragraph nobody
touched either.
`
	doc, _, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	// Edit ONE word in the middle block, the way a reviewer would.
	doc.Blocks[1].Children[0].Children[0].Inlines[0].Text = "edited list"

	got := string(SerializeOnto(doc, []byte(src)))
	for _, keep := range []string{
		"A hard wrapped\nparagraph that\nnobody touched.",
		"Another wrapped\nparagraph nobody\ntouched either.",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("an untouched paragraph was reflowed by an edit elsewhere.\nwant to still contain:\n%s\ngot:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "edited list") {
		t.Errorf("the edit did not land:\n%s", got)
	}
}

// TestTwoIdenticalParagraphsTakeTwoChunks pins the consumption rule. Both
// paragraphs mean the same thing, so both match one key; using one chunk twice
// would be right by accident here and wrong the moment their spellings differ.
func TestTwoIdenticalParagraphsTakeTwoChunks(t *testing.T) {
	const src = `Repeated
line.

Middle.

Repeated
line.
`
	doc, _, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	got := string(SerializeOnto(doc, []byte(src)))
	if n := strings.Count(got, "Repeated\nline."); n != 2 {
		t.Errorf("want both wrapped copies preserved, got %d:\n%s", n, got)
	}
}

// TestAnUnplaceableBlockCostsARenderAndNeverAWrongWrite drives the degradation
// path: a thematic break reports no segments anywhere under it, so chunkStarts
// cannot place it and merges it into its neighbour. The merged chunk reparses
// to two blocks, fails the one-block test, and both are written the
// serializer's way. What must NOT happen is a chunk landing on the wrong block.
func TestAnUnplaceableBlockCostsARenderAndNeverAWrongWrite(t *testing.T) {
	const src = `First
wrapped.

---

Second
wrapped.
`
	doc, _, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	got := string(SerializeOnto(doc, []byte(src)))
	reparsed, _, err := Parse([]byte(got))
	if err != nil {
		t.Fatalf("the output does not parse: %v\n%s", err, got)
	}
	if len(reparsed.Blocks) != len(doc.Blocks) {
		t.Errorf("block count moved: %d -> %d\n%s", len(doc.Blocks), len(reparsed.Blocks), got)
	}
}

// TestEveryPreservedByteReparsesToWhatItReplaced is the whole guarantee, asked
// over the corpus rather than argued: whatever SerializeOnto writes must parse
// back to the document it was given.
func TestEveryPreservedByteReparsesToWhatItReplaced(t *testing.T) {
	for _, src := range []string{
		"Setext\n======\n\n* a\n* b\n",
		"# H\n\ntext __b__ here\n\n> quoted\n> lines\n",
		"| a | b |\n| --- | --- |\n| 1 | 2 |\n",
		"```sh\necho hi\n```\n\npara\n",
		"---\ntitle: t\n---\n\n* item\n",
	} {
		doc, _, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("parsing %q: %v", src, err)
		}
		out := SerializeOnto(doc, []byte(src))
		back, _, err := Parse(out)
		if err != nil {
			t.Fatalf("output of %q does not parse: %v\n%s", src, err, out)
		}
		if !docmodel.Equal(doc, back) {
			t.Errorf("preserved bytes changed the document.\nsrc:\n%s\nout:\n%s", src, out)
		}
	}
}

// TestAChangedCommentIDIsRewritten: preservation keys on what a block renders
// to, and the ID mark is part of that, so a block whose only change is an ID
// is written fresh rather than handed its old bytes.
func TestAChangedCommentIDIsRewritten(t *testing.T) {
	prev := []byte("Some {==words==}{>>@comment cm-old<<} here.\n")
	doc, _, err := Parse(prev)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i, in := range doc.Blocks[0].Inlines {
		for j, m := range in.Marks {
			if m.Kind == docmodel.Highlight {
				doc.Blocks[0].Inlines[i].Marks[j].Attrs[docmodel.CommentIDAttr] = "cm-new"
			}
		}
	}
	want := "Some {==words==}{>>@comment cm-new<<} here.\n"
	if got := string(SerializeOnto(doc, prev)); got != want {
		t.Errorf("SerializeOnto = %q, want %q", got, want)
	}
}
