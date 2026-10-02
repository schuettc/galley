package suggest_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/unsent"
)

func parseDoc(t *testing.T, src string) docmodel.Doc {
	t.Helper()
	d, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return d
}

func keyOf(t *testing.T, d docmodel.Doc, label string) string {
	t.Helper()
	for _, b := range suggest.Blocks(d) {
		if strings.Contains(b.Label, label) {
			return b.Key
		}
	}
	t.Fatalf("no block labelled %q in %#v", label, suggest.Blocks(d))
	return ""
}

// lastComment is the last comment-kind pending in d, which for these fixtures
// is the note under test.
func lastComment(t *testing.T, d docmodel.Doc) suggest.Pending {
	t.Helper()
	var last suggest.Pending
	found := false
	for _, p := range suggest.List(d) {
		if p.Kind == suggest.KindComment {
			last, found = p, true
		}
	}
	if !found {
		t.Fatalf("no comment in %#v", suggest.List(d))
	}
	return last
}

const figureDoc = "# Architecture\n\n" +
	"The system has three parts.\n\n" +
	"![the request path](flow.png)\n\n" +
	"```mermaid\ngraph TD;\n  a-->b;\n```\n"

func TestBlocks_ListsEveryTopLevelBlockWithALabel(t *testing.T) {
	d := parseDoc(t, figureDoc)
	got := suggest.Blocks(d)
	if len(got) != 4 {
		t.Fatalf("Blocks = %#v, want 4", got)
	}
	want := []struct{ kind, label string }{
		{"heading", "# Architecture"},
		{"paragraph", "The system has three parts."},
		{"image", "the request path (flow.png)"},
		{"codeBlock", "mermaid: graph TD;"},
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Label != w.label {
			t.Errorf("Blocks[%d] = (%q, %q), want (%q, %q)", i, got[i].Kind, got[i].Label, w.kind, w.label)
		}
		if !strings.HasPrefix(got[i].Key, "bk-") {
			t.Errorf("Blocks[%d].Key = %q, want a bk- key", i, got[i].Key)
		}
	}
}

// The point of a content-derived key: an edit that renumbers everything
// around a block must not move that block's key.
func TestBlockKey_SurvivesRenumberingAroundIt(t *testing.T) {
	before := parseDoc(t, figureDoc)
	imageKey := keyOf(t, before, "the request path")
	fenceKey := keyOf(t, before, "mermaid")

	// Three new blocks above, one between, one below — every ordinal moves.
	after := parseDoc(t, "Intro.\n\nMore intro.\n\n"+figureDoc+"\nA trailing paragraph.\n")
	if got := keyOf(t, after, "the request path"); got != imageKey {
		t.Errorf("image key moved: %q -> %q", imageKey, got)
	}
	if got := keyOf(t, after, "mermaid"); got != fenceKey {
		t.Errorf("fence key moved: %q -> %q", fenceKey, got)
	}
	// And the index really did move, so the test is not vacuous.
	for _, b := range suggest.Blocks(after) {
		if b.Key == imageKey && b.Index == 2 {
			t.Errorf("the image did not actually move; the test proves nothing")
		}
	}
}

func TestBlockKey_IdenticalBlocksGetDistinctKeys(t *testing.T) {
	d := parseDoc(t, "Same.\n\nOther.\n\nSame.\n\nSame.\n")
	got := suggest.Blocks(d)
	seen := map[string]bool{}
	for _, b := range got {
		if seen[b.Key] {
			t.Fatalf("duplicate key %q in %#v", b.Key, got)
		}
		seen[b.Key] = true
	}
	// Inserting an unrelated block above must not renumber the collision
	// suffixes: they count only the blocks they actually collide with.
	d2 := parseDoc(t, "New.\n\nSame.\n\nOther.\n\nSame.\n\nSame.\n")
	keys2 := map[string]bool{}
	for _, b := range suggest.Blocks(d2) {
		keys2[b.Key] = true
	}
	for _, b := range got {
		if !keys2[b.Key] {
			t.Errorf("key %q (%s) vanished when a block was inserted above", b.Key, b.Label)
		}
	}
}

func TestCommentOnBlock_WritesAnIDNoteAfterTheBlock(t *testing.T) {
	d := parseDoc(t, figureDoc)
	key := keyOf(t, d, "the request path")
	const id = "cb-0123456789abcdef"

	out, err := suggest.CommentOnBlock(d, key, id)
	if err != nil {
		t.Fatalf("CommentOnBlock: %v", err)
	}

	// The mark and nothing else: the comment's words are not the file's.
	md := string(markdown.Serialize(out))
	wantLine := "![the request path](flow.png)\n\n{>>@comment " + id + "<<}\n"
	if !strings.Contains(md, wantLine) {
		t.Fatalf("serialized:\n%s\nwant it to contain:\n%s", md, wantLine)
	}

	// And it comes back as a block anchor on the same block, carrying the ID.
	back := parseDoc(t, md)
	var found bool
	for _, p := range suggest.List(back) {
		if p.Anchor != suggest.AnchorBlock {
			continue
		}
		found = true
		if p.BlockKey != key {
			t.Errorf("BlockKey = %q, want %q", p.BlockKey, key)
		}
		if p.CommentID != id || p.Text != "" {
			t.Errorf("note = (%q, %q), want the ID %q and no words", p.CommentID, p.Text, id)
		}
		if !strings.Contains(p.Context, "![the request path](flow.png)") {
			t.Errorf("Context = %q, want the image block", p.Context)
		}
	}
	if !found {
		t.Fatalf("List(%q) found no block comment: %#v", md, suggest.List(back))
	}
}

// A document comment must NOT be mistaken for a comment on the last block,
// and a note on the last block must not be mistaken for a document comment.
func TestAnchors_LastBlockVersusDocument(t *testing.T) {
	onLast := parseDoc(t, figureDoc+"\n{>>the fence is wrong<<}\n")
	onDoc := parseDoc(t, figureDoc+"\n{>>@document the whole thing is wrong<<}\n")

	fenceKey := keyOf(t, onLast, "mermaid")

	lastPending := lastComment(t, onLast)
	if lastPending.Anchor != suggest.AnchorBlock || lastPending.BlockKey != fenceKey {
		t.Errorf("trailing bare note = %+v, want a block anchor on the fence (%s)", lastPending, fenceKey)
	}
	docPending := lastComment(t, onDoc)
	if docPending.Anchor != suggest.AnchorDocument {
		t.Errorf("trailing @document note = %+v, want a document anchor", docPending)
	}
}

// The hand-edit case the discriminator is built around: the SAME words at the
// end of the last paragraph are a range comment, not a document comment.
func TestAnchors_NoteAtTheEndOfAParagraphStaysARange(t *testing.T) {
	d := parseDoc(t, "# T\n\nThe last paragraph. {>>really?<<}\n")
	for _, p := range suggest.List(d) {
		if p.Anchor != suggest.AnchorRange {
			t.Fatalf("pending %+v: a note inside a paragraph must stay a range anchor", p)
		}
	}
	// It is an out-of-band InlineComment, exactly as before this change.
	_, comments, err := markdown.Parse([]byte("The last paragraph. {>>really?<<}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %#v, want one range comment", comments)
	}
}

func TestCommentOnBlock_UnknownKeyIsNamed(t *testing.T) {
	d := parseDoc(t, figureDoc)
	_, err := suggest.CommentOnBlock(d, "bk-nope", "cb-0123456789abcdef")
	if err == nil || !strings.Contains(err.Error(), "bk-nope") {
		t.Fatalf("err = %v, want one naming the key", err)
	}
}

// Several comments on one block stack, and each still resolves to that block.
func TestCommentOnBlock_StacksAndKeepsEveryAnchor(t *testing.T) {
	d := parseDoc(t, figureDoc)
	key := keyOf(t, d, "the request path")
	d, err := suggest.CommentOnBlock(d, key, "cb-0000000000000001")
	if err != nil {
		t.Fatalf("CommentOnBlock: %v", err)
	}
	d, err = suggest.CommentOnBlock(d, key, "cb-0000000000000002")
	if err != nil {
		t.Fatalf("CommentOnBlock: %v", err)
	}

	md := string(markdown.Serialize(d))
	if !strings.Contains(md, "{>>@comment cb-0000000000000001<<}\n\n{>>@comment cb-0000000000000002<<}") {
		t.Fatalf("serialized:\n%s\nwant both notes in order", md)
	}
	back := parseDoc(t, md)
	n := 0
	for _, p := range suggest.List(back) {
		if p.Anchor != suggest.AnchorBlock {
			continue
		}
		n++
		if p.BlockKey != key {
			t.Errorf("%q anchored to %q, want %q", p.CommentID, p.BlockKey, key)
		}
	}
	if n != 2 {
		t.Errorf("found %d block comments, want 2", n)
	}
}

// Resolving a block comment removes the note from the file — the only place
// a block comment is recorded.
func TestAcceptRemovesANoteBlock(t *testing.T) {
	d := parseDoc(t, figureDoc)
	key := keyOf(t, d, "the request path")
	d, err := suggest.CommentOnBlock(d, key, "cb-0123456789abcdef")
	if err != nil {
		t.Fatalf("CommentOnBlock: %v", err)
	}
	var id string
	for _, p := range suggest.List(d) {
		if p.Anchor == suggest.AnchorBlock {
			id = p.ID
		}
	}
	if id == "" {
		t.Fatal("no block comment to accept")
	}
	out, err := suggest.Accept(d, id)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if md := string(markdown.Serialize(out)); strings.Contains(md, "{>>") {
		t.Errorf("accepted note still in the file:\n%s", md)
	}
	// The document is otherwise untouched.
	if got, want := string(markdown.Serialize(out)), figureDoc; got != want {
		t.Errorf("document changed:\n got: %q\nwant: %q", got, want)
	}
}

// A comment's target search must not reach into an existing note's text —
// otherwise a note that repeats the word makes the next comment on it report
// "matched 2 times".
func TestFindUnique_IgnoresNoteText(t *testing.T) {
	d := parseDoc(t, "The widget works.\n\n{>>the widget is undefined<<}\n")
	out, err := suggest.CommentOn(d, "widget", "cm-0000000000000022", "court", time.Now())
	if err != nil {
		t.Fatalf("CommentOn: %v", err)
	}
	if md := string(markdown.Serialize(out)); !strings.Contains(md, "{>>the widget is undefined<<}") {
		t.Errorf("the note was rewritten:\n%s", md)
	}
}

// A block key names a BLOCK; a comment key names a THREAD. They are stored in
// the same sidecar and looked up by string, so a collision would attach a
// conversation to the wrong thing. The prefixes are what keeps them apart, and
// this test is what keeps the prefixes.
func TestBlockKeysAndCommentKeysCannotCollide(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "the same words"}}},
	}}
	blockKey := suggest.Blocks(d)[0].Key
	if !strings.HasPrefix(blockKey, "bk-") {
		t.Errorf("block key %q lost its namespace prefix", blockKey)
	}
	for _, k := range []unsent.Kind{unsent.KindText, unsent.KindBlock, unsent.KindDocument} {
		if id := unsent.NewID(k); strings.HasPrefix(id, "bk-") {
			t.Errorf("a %s comment's key %q shares the block keys' prefix", k, id)
		}
	}
}

// anchor != "range" ⟺ run == "". A run names a MARK (docmodel.RunAttr, minted
// by MintRuns onto Ins/Del/Highlight inlines); a Note is a block with unmarked
// inlines, so a block or document anchor structurally cannot carry one — not
// even mid-session, after MintRuns has run over the whole document.
//
// The browser keys its cards on [data-run], so a card for a note has to be
// addressed by Anchor instead. Pinning the rule here is what stops someone
// "fixing" the empty run by minting one, which would make a note look like a
// mark that nothing can accept or reject.
func TestABlockOrDocumentAnchorNeverCarriesARun(t *testing.T) {
	d := parseDoc(t, "A paragraph with {++an insertion++} in it.\n\n"+
		"![a figure](f.png)\n\n{>>the axis labels are swapped<<}\n\n{>>@document overall: good<<}\n")
	d = suggest.MintRuns(d)

	var sawRange, sawBlock, sawDocument bool
	for _, p := range suggest.List(d) {
		switch p.Anchor {
		case suggest.AnchorRange:
			sawRange = true
			if p.Run == "" {
				t.Errorf("%s: a range anchor must carry a run after MintRuns", p.ID)
			}
		case suggest.AnchorBlock:
			sawBlock = true
			if p.Run != "" {
				t.Errorf("%s: a block anchor carried run %q; a note has no mark to name", p.ID, p.Run)
			}
		case suggest.AnchorDocument:
			sawDocument = true
			if p.Run != "" {
				t.Errorf("%s: a document anchor carried run %q", p.ID, p.Run)
			}
		}
	}
	if !sawRange || !sawBlock || !sawDocument {
		t.Fatalf("fixture did not produce all three anchors: range=%v block=%v document=%v",
			sawRange, sawBlock, sawDocument)
	}
}

// blockKeys SERIALIZES EVERY BLOCK to markdown, and it was called once per
// note, by List (through AnchorFor and again through noteContext). The measured cost was 137 ms
// of CPU per List at 50 notes — 1,765x the same document with none — while the
// sidebar polls on a timer and project() fires on every debounce.
func benchDoc(notes int) docmodel.Doc {
	var blocks []docmodel.Block
	for i := 0; i < 400; i++ {
		blocks = append(blocks, docmodel.Block{
			Kind:    docmodel.Paragraph,
			Inlines: []docmodel.Inline{{Text: "Paragraph number " + strconv.Itoa(i) + " of the document."}},
		})
		if i < notes {
			blocks = append(blocks, markdown.NewCommentNote(fmt.Sprintf("cb-%016x", i)))
		}
	}
	return docmodel.Doc{Blocks: blocks}
}

func BenchmarkListNoNotes(b *testing.B) {
	d := benchDoc(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = suggest.List(d)
	}
}

func BenchmarkList100Notes(b *testing.B) {
	d := benchDoc(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = suggest.List(d)
	}
}

func BenchmarkList50Notes(b *testing.B) {
	d := benchDoc(50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = suggest.List(d)
	}
}
