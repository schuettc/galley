package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
	"github.com/subaud-tools/galley/internal/ydoc"
)

// EVERY LIST ITEM CROSSES THE BRIDGE WITH A PARAGRAPH FIRST, AND EVERY
// BLOCKQUOTE WITH A BLOCK IN IT.
//
// The sibling of TestEveryCellCrossesWithABlockInside, for the rule that is
// STRICTER than block+. TipTap's listItem is `content: 'paragraph block*'` and
// StarterKit's copy is registered unmodified, so an item whose first child is a
// fence, a heading, a blockquote, a nested list, a table, an image, a rule or a
// note — or an item with no children at all, which is what a bare "-" is — is a
// node the browser cannot build. blockquote is `block+`, and a bare ">" is a
// Blockquote goldmark hands back childless.
//
// y-prosemirror does not SKIP such an element. createNodeFromYElement wraps
// schema.node (which is createChecked, and throws) in a try, and the catch
// DELETES the element from the Y doc; the deletion broadcasts, EditServer's
// doc.OnUpdate fires, touch() schedules Project(), and the projection writes the
// shortened document to the author's file with no keystroke. Measured in a real
// chromium against the tracked build: an ordinary numbered install list went
// from 143 bytes to 66, losing both of its commands, and the surviving third
// step was RENUMBERED to "1. done" — a file that still reads as a complete
// one-step procedure.
//
// IT IS DRIVEN FROM Parse RATHER THAN FROM A HAND-BUILT MODEL, for the reason
// the cell test gives: the hand-built model is what agreed with the bug for a
// whole phase. And it walks the FRAGMENT rather than Read's output, because a
// model that round-trips through the bridge can still be a model the browser
// refuses — which is the entire class of bug this guards.
//
// A GO GOLDEN FILE CANNOT SEE ANY OF IT: Serialize is unchanged by the fix, so
// `Serialize(Parse(x)) == x` held for every one of these before it. This test
// and `just schema` are what can fail.
func TestEveryListItemCrossesWithAParagraphFirst(t *testing.T) {
	src := []byte("# Install\n" +
		"\n" +
		"1. ```sh\n" +
		"   brew install galley\n" +
		"   ```\n" +
		"2. ```sh\n" +
		"   galley edit doc.md\n" +
		"   ```\n" +
		"3. done\n" +
		"\n" +
		"- a heading first\n" +
		"- ### deeper\n" +
		"- > quoted first\n" +
		"- - nested first\n" +
		"- ![diagram](pic.png)\n" +
		"- | h1 | h2 |\n" +
		"  | --- | --- |\n" +
		"  | a | b |\n" +
		"- {>>a note first<<}\n" +
		"-\n" +
		"- last\n" +
		"\n" +
		">\n" +
		"\n" +
		"> - ```sh\n" +
		">   ./deploy --now\n" +
		">   ```\n")
	model, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	doc := crdt.New()
	ydoc.Load(doc, own(doc), model)

	items, quotes := 0, 0
	var walk func(n any, path string)
	walk = func(n any, path string) {
		el, ok := n.(*crdt.YXmlElement)
		if !ok {
			return
		}
		where := path + "/" + el.NodeName
		kids := el.Children()
		switch el.NodeName {
		case string(docmodel.ListItem):
			items++
			if len(kids) == 0 {
				t.Errorf("%s reached the fragment EMPTY — TipTap's content is `paragraph block*`, so y-prosemirror deletes the item and everything in it", where)
				return
			}
			first, ok := kids[0].(*crdt.YXmlElement)
			if !ok {
				t.Errorf("%s opens with a %T where a block element was expected", where, kids[0])
				return
			}
			if first.NodeName != string(docmodel.Paragraph) {
				t.Errorf("%s opens with <%s> — `paragraph block*` needs a paragraph first, so y-prosemirror deletes the item and everything in it", where, first.NodeName)
			}
		case string(docmodel.Blockquote):
			quotes++
			if len(kids) == 0 {
				t.Errorf("%s reached the fragment EMPTY — TipTap's content is block+, so y-prosemirror deletes it", where)
			}
		}
		for _, c := range kids {
			walk(c, where)
		}
	}
	for _, n := range doc.GetXmlFragment(ydoc.FragmentName).Children() {
		walk(n, "")
	}

	// The counts are asserted because a walk that found nothing would report
	// every claim above as satisfied. Three ordered items, nine bullet items in
	// the second list plus the one nested inside the fourth of them, and one
	// item inside the quoted list; two blockquotes at the top level plus the
	// one inside a bullet item.
	if items != 14 {
		t.Errorf("walked %d list items, want 14 — the fixture or the walk lost some", items)
	}
	if quotes != 3 {
		t.Errorf("walked %d blockquotes, want 3 — the fixture or the walk lost some", quotes)
	}
}
