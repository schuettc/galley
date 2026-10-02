package ydoc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/ydoc"
)

// own binds a document to its own transactions — the scratch-document case.
func own(doc *crdt.Doc) review.Tx {
	return func(fn func(*crdt.Transaction)) { doc.Transact(fn) }
}

// roundTrip loads model into a fresh document and reads it back.
func roundTrip(t *testing.T, model docmodel.Doc) docmodel.Doc {
	t.Helper()
	doc := crdt.New()
	ydoc.Load(doc, own(doc), model)
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func text(s string) docmodel.Inline { return docmodel.Inline{Text: s} }

func marked(s string, marks ...docmodel.Mark) docmodel.Inline {
	return docmodel.Inline{Text: s, Marks: marks}
}

func mark(k docmodel.MarkKind, kv ...string) docmodel.Mark {
	m := docmodel.Mark{Kind: k}
	if len(kv) > 0 {
		m.Attrs = map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m.Attrs[kv[i]] = kv[i+1]
		}
	}
	return m
}

func para(inlines ...docmodel.Inline) docmodel.Block {
	return docmodel.Block{Kind: docmodel.Paragraph, Inlines: inlines}
}

// TestRoundTrip_Fixtures is the headline guarantee: every Task 2 fixture tree
// survives model → fragment → model unchanged.
func TestRoundTrip_Fixtures(t *testing.T) {
	for _, name := range []string{"basic.md", "blocks.md", "critic.md", "inline.md"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "markdown", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			want, _, err := markdown.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := roundTrip(t, want)
			if !docmodel.Equal(want, got) {
				t.Errorf("round trip changed the document\nwant %#v\ngot  %#v", want, got)
			}
		})
	}
}

// TestReadLive_MatchesReadForEveryFixture pins ReadLive's two-phase snapshot
// (structure inside a Transact, text after it) against Read's direct walk:
// with no concurrent writer, the two must agree exactly. The race between
// them is what TestProjectDoesNotRaceConcurrentLoad in internal/serve
// exercises — this test is the correctness half, not the concurrency half.
func TestReadLive_MatchesReadForEveryFixture(t *testing.T) {
	for _, name := range []string{"basic.md", "blocks.md", "critic.md", "inline.md"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "markdown", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			model, _, err := markdown.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			doc := crdt.New()
			ydoc.Load(doc, own(doc), model)

			want, err := ydoc.Read(doc)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got, err := ydoc.ReadLive(doc)
			if err != nil {
				t.Fatalf("ReadLive: %v", err)
			}
			if !docmodel.Equal(want, got) {
				t.Errorf("ReadLive disagreed with Read\n Read:     %#v\n ReadLive: %#v", want, got)
			}
		})
	}
}

// TestRoundTrip_SuggestionMarksCarryAuthorAndAt covers the marks the review
// layer writes: their attrs are the whole point, so losing them is silent
// attribution loss.
func TestRoundTrip_SuggestionMarksCarryAuthorAndAt(t *testing.T) {
	at := "2026-08-05T12:30:00Z"
	want := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "2"},
			Inlines: []docmodel.Inline{marked("Review", mark(docmodel.Highlight, "author", "court", "at", at))}},
		para(
			text("keep "),
			marked("added", mark(docmodel.Ins, "author", "court", "at", at)),
			text(" "),
			marked("removed", mark(docmodel.Del, "author", "agent", "at", at)),
			text(" "),
			marked("noted", mark(docmodel.Highlight, "author", "agent", "at", at)),
			text(" "),
			marked("bold+ins", mark(docmodel.Bold), mark(docmodel.Ins, "author", "court", "at", at)),
			text(" "),
			marked("here", mark(docmodel.Link, "href", "https://example.com/a?b=1&c=2")),
		),
	}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("suggestion marks lost\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRoundTrip_Multibyte is the UTF-16 boundary: YXmlText addresses code
// units, and an emoji is one rune but two of them. A rune- or byte-indexed
// bridge cuts the surrogate pair in half.
func TestRoundTrip_Multibyte(t *testing.T) {
	want := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("café ☕ 😀"),
			marked("café ☕ 😀", mark(docmodel.Bold)),
			text(" tail"),
			marked("😀", mark(docmodel.Ins, "author", "court", "at", "2026-08-05T00:00:00Z")),
			text("café"),
		),
		{Kind: docmodel.CodeBlock, Attrs: map[string]string{"language": "go"},
			Text: "// café ☕ 😀\nfmt.Println(\"😀\")\n"},
	}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("multibyte text corrupted\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRoundTrip_HardBreak covers the zero-text sentinel inline, including one
// that carries the marks of the run it interrupts.
func TestRoundTrip_HardBreak(t *testing.T) {
	want := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("line one"),
			docmodel.Inline{Marks: []docmodel.Mark{mark(docmodel.HardBreak)}},
			text("line two"),
			docmodel.Inline{Marks: []docmodel.Mark{
				mark(docmodel.Ins, "author", "court", "at", "2026-08-05T00:00:00Z"),
				mark(docmodel.HardBreak),
			}},
			marked("line three", mark(docmodel.Ins, "author", "court", "at", "2026-08-05T00:00:00Z")),
		),
	}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("hard break lost\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRoundTrip_NestedContainers covers the container kinds two levels deep.
func TestRoundTrip_NestedContainers(t *testing.T) {
	want := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Blockquote, Children: []docmodel.Block{
			para(text("quoted")),
			{Kind: docmodel.BulletList, Children: []docmodel.Block{
				{Kind: docmodel.ListItem, Children: []docmodel.Block{
					para(text("one")),
					{Kind: docmodel.OrderedList, Children: []docmodel.Block{
						{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("deep"))}},
					}},
				}},
			}},
		}},
		{Kind: docmodel.Rule},
		{Kind: docmodel.Image, Attrs: map[string]string{"src": "https://example.com/i.png", "alt": "Alt"}},
		para(),
	}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("nesting lost\nwant %#v\ngot  %#v", want, got)
	}
}

// TestLoad_UsesTipTapNames pins the wire names: TipTap and y-prosemirror match
// nodes by tag, so a renamed tag is an empty editor, not a test failure.
func TestLoad_UsesTipTapNames(t *testing.T) {
	doc := crdt.New()
	ydoc.Load(doc, own(doc), docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "1"},
			Inlines: []docmodel.Inline{text("T")}},
		{Kind: docmodel.BulletList, Children: []docmodel.Block{
			{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("i"))}},
		}},
		{Kind: docmodel.Rule},
	}})
	want := `<heading level="1">T</heading><bulletList><listItem><paragraph>i</paragraph></listItem></bulletList><horizontalRule></horizontalRule>`
	if got := doc.GetXmlFragment(ydoc.FragmentName).ToXML(); got != want {
		t.Errorf("fragment XML\nwant %s\ngot  %s", want, got)
	}
}

// TestLoad_ReplacesExistingContent: Load is a replace, not an append.
func TestLoad_ReplacesExistingContent(t *testing.T) {
	doc := crdt.New()
	ydoc.Load(doc, own(doc), docmodel.Doc{Blocks: []docmodel.Block{
		para(text("first")), para(text("second")),
	}})
	want := docmodel.Doc{Blocks: []docmodel.Block{para(marked("only", mark(docmodel.Bold)))}}
	ydoc.Load(doc, own(doc), want)

	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(want, got) {
		t.Errorf("Load appended instead of replacing\nwant %#v\ngot  %#v", want, got)
	}
}

// TestLoad_WritesThroughTheSuppliedTransaction: the whole reason Load takes a
// review.Tx is that the websocket server's Apply is what broadcasts. A Load
// that reached for doc.Transact would be correct on disk and invisible on the
// reviewer's screen.
func TestLoad_WritesThroughTheSuppliedTransaction(t *testing.T) {
	doc := crdt.New()
	calls := 0
	tx := review.Tx(func(fn func(*crdt.Transaction)) {
		calls++
		doc.Transact(fn)
	})
	ydoc.Load(doc, tx, docmodel.Doc{Blocks: []docmodel.Block{para(text("x"))}})
	if calls != 1 {
		t.Errorf("supplied Tx called %d times, want exactly 1", calls)
	}
}

// TestRoundTrip_OverTheWire: the fragment has to survive the sync path, not
// just an in-process read. Formatting attribute values are encoded as lib0
// objects, so this is the test that would catch an unencodable value type.
func TestRoundTrip_OverTheWire(t *testing.T) {
	src := crdt.New()
	want := docmodel.Doc{Blocks: []docmodel.Block{
		{Kind: docmodel.Heading, Attrs: map[string]string{"level": "3"},
			Inlines: []docmodel.Inline{text("café ☕ 😀")}},
		para(
			marked("linked", mark(docmodel.Link, "href", "https://example.com")),
			marked("suggested", mark(docmodel.Ins, "author", "court", "at", "2026-08-05T00:00:00Z")),
		),
	}}
	ydoc.Load(src, own(src), want)

	dst := crdt.New()
	if err := dst.ApplyUpdate(src.EncodeStateAsUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	got, err := ydoc.Read(dst)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(want, got) {
		t.Errorf("document did not survive the wire\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRead_EmptyDocument: an untouched document reads as an empty doc, not an
// error and not a phantom block.
func TestRead_EmptyDocument(t *testing.T) {
	got, err := ydoc.Read(crdt.New())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Blocks) != 0 {
		t.Errorf("empty document read as %#v", got)
	}
}

// TestRead_RejectsATextNodeAtTheTopLevel: the fragment's children are blocks.
// Bare text there is a malformed document, and reading it as one would lose it
// silently.
func TestRead_RejectsATextNodeAtTheTopLevel(t *testing.T) {
	doc := crdt.New()
	frag := doc.GetXmlFragment(ydoc.FragmentName)
	txt := crdt.NewYXmlText()
	doc.Transact(func(txn *crdt.Transaction) {
		frag.InsertText(txn, 0, txt)
		txt.Insert(txn, 0, "loose", nil)
	})
	if _, err := ydoc.Read(doc); err == nil {
		t.Error("Read accepted a bare text node at the top level")
	}
}

// TestRead_AcceptsJSONEncodedMarkAttrs: the bridge writes mark attrs as lib0
// objects (what y-prosemirror produces), but a peer that JSON-encodes them
// into a string should still be readable rather than silently dropped.
func TestRead_AcceptsJSONEncodedMarkAttrs(t *testing.T) {
	doc := crdt.New()
	frag := doc.GetXmlFragment(ydoc.FragmentName)
	p := crdt.NewYXmlElement("paragraph")
	txt := crdt.NewYXmlText()
	doc.Transact(func(txn *crdt.Transaction) {
		frag.InsertElement(txn, 0, p)
		p.InsertText(txn, 0, txt)
		txt.Insert(txn, 0, "a", crdt.Attributes{"link": `{"href":"https://example.com"}`})
		txt.Insert(txn, txt.Len(), "b", crdt.Attributes{"link": nil, "bold": "{}"})
	})
	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{para(
		marked("a", mark(docmodel.Link, "href", "https://example.com")),
		marked("b", mark(docmodel.Bold)),
	)}}
	if !docmodel.Equal(want, got) {
		t.Errorf("JSON-encoded mark attrs\nwant %#v\ngot  %#v", want, got)
	}
}

// TestLoad_DoesNotBleedAnUnknownMarkRightward is the reviewer's probe shape.
//
// YXmlText.Insert applies the DIFFERENCE between the attributes it is passed
// and the ones already in effect, so any mark kind the run's attribute set
// fails to name stays switched on for the rest of the text node. A mark from a
// TipTap extension this package has never heard of — StarterKit ships strike —
// would smear across every run after it.
func TestLoad_DoesNotBleedAnUnknownMarkRightward(t *testing.T) {
	const unknown = docmodel.MarkKind("underline")
	want := docmodel.Doc{Blocks: []docmodel.Block{para(
		marked("A", mark(unknown)),
		text("B"),
		marked("C", mark(docmodel.Bold)),
	)}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("unknown mark bled rightward\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRoundTrip_UnknownMarkSurvivesAReload holds the contract Read already
// advertises — an unrecognised mark is preserved rather than dropped — across
// the full Read → Load → Read cycle, which is what a document edited in TipTap
// and written back through galley actually does. The mark must come back on
// its own run and nowhere else.
func TestRoundTrip_UnknownMarkSurvivesAReload(t *testing.T) {
	const unknown = docmodel.MarkKind("strike")
	want := docmodel.Doc{Blocks: []docmodel.Block{para(
		text("plain "),
		marked("struck", mark(unknown, "reason", "obsolete")),
		text(" after "),
		marked("bold", mark(docmodel.Bold)),
	)}}

	doc := crdt.New()
	ydoc.Load(doc, own(doc), want)
	once, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(want, once) {
		t.Fatalf("first read\nwant %#v\ngot  %#v", want, once)
	}

	// Reload what was read, into a fresh document, and read it again.
	again := crdt.New()
	ydoc.Load(again, own(again), once)
	twice, err := ydoc.Read(again)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(want, twice) {
		t.Errorf("unknown mark lost or spread on reload\nwant %#v\ngot  %#v", want, twice)
	}
}

// TestLoad_ClearsAnUnknownMarkAcrossAHardBreak: a hard break starts a new
// YXmlText, so formatting cannot carry across it — but the runs after it are
// still written from the same block, and must still name the block's unknown
// kinds to stay clear of them.
func TestLoad_ClearsAnUnknownMarkAcrossAHardBreak(t *testing.T) {
	const unknown = docmodel.MarkKind("superscript")
	want := docmodel.Doc{Blocks: []docmodel.Block{para(
		marked("up", mark(unknown)),
		docmodel.Inline{Marks: []docmodel.Mark{mark(docmodel.HardBreak)}},
		text("down"),
	)}}
	got := roundTrip(t, want)
	if !docmodel.Equal(want, got) {
		t.Errorf("unknown mark crossed a hard break\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRead_MergesAdjacentRunsWithIdenticalMarks pins a normalization the CRDT
// performs for us: two inlines with the same mark set are one formatting run
// in the fragment and read back as one inline. It is the same normalization
// the markdown parser applies, so a parsed document is unaffected — but a
// hand-built one is, and callers should know.
func TestRead_MergesAdjacentRunsWithIdenticalMarks(t *testing.T) {
	got := roundTrip(t, docmodel.Doc{Blocks: []docmodel.Block{
		para(text("one "), text("two"), marked("three", mark(docmodel.Bold))),
	}})
	want := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("one two"), marked("three", mark(docmodel.Bold))),
	}}
	if !docmodel.Equal(want, got) {
		t.Errorf("adjacent-run merge\nwant %#v\ngot  %#v", want, got)
	}
}

// TestRoundTrip_NoteBlock pins the fragment shape a block or document comment
// takes, because that shape is a CONTRACT WITH THE BROWSER: the TipTap schema
// binds to this fragment directly, and a node it has no definition for is a
// node it silently drops (see CLAUDE.md on the shared fragment's names).
//
// The node is <note>, its anchor is a node attribute spelled "anchor" with
// value "block" or "document", and the comment's own words are one unmarked
// text run inside it. A browser build that has not yet learned this node must
// not be given a document containing one.
func TestRoundTrip_NoteBlock(t *testing.T) {
	model := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("A paragraph.")),
		{
			Kind:    docmodel.Note,
			Attrs:   map[string]string{"anchor": docmodel.AnchorBlock},
			Inlines: []docmodel.Inline{text("about that paragraph")},
		},
		{
			Kind:    docmodel.Note,
			Attrs:   map[string]string{"anchor": docmodel.AnchorDocument},
			Inlines: []docmodel.Inline{text("about the file")},
		},
	}}
	got := roundTrip(t, model)
	if !docmodel.Equal(got, model) {
		t.Fatalf("note blocks did not survive the fragment:\n got: %#v\nwant: %#v", got, model)
	}
	for i, b := range got.Blocks[1:] {
		if string(b.Kind) != "note" {
			t.Errorf("blocks[%d].Kind = %q, want the TipTap node name \"note\"", i+1, b.Kind)
		}
	}
}

// TestRoundTrip_NoteCarriesItsCommentID: a block comment's mark is a <note>
// with an anchor, an id and NO text run — its words are in the unsent round.
// The fragment has to carry that shape both ways, empty content included.
func TestRoundTrip_NoteCarriesItsCommentID(t *testing.T) {
	model := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("A paragraph.")),
		{Kind: docmodel.Note, Attrs: map[string]string{
			"anchor": docmodel.AnchorBlock, docmodel.CommentIDAttr: "cb-0123456789abcdef",
		}},
	}}
	got := roundTrip(t, model)
	if !docmodel.Equal(got, model) {
		t.Fatalf("the ID note did not survive the fragment:\n got: %#v\nwant: %#v", got, model)
	}
	if n := len(got.Blocks[1].Inlines); n != 0 {
		t.Errorf("the ID note came back with %d inlines, want none", n)
	}
}

// TestRoundTrip_HighlightCarriesItsCommentID: a text comment's ID rides on the
// highlight format beside its run, and both come back exactly. Equal ignores
// runs, so the run is asserted by hand.
func TestRoundTrip_HighlightCarriesItsCommentID(t *testing.T) {
	model := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("keep "),
			marked("this", mark(docmodel.Highlight,
				docmodel.CommentIDAttr, "cm-0123456789abcdef", docmodel.RunAttr, "r1")),
			text(" here"),
		),
	}}
	got := roundTrip(t, model)
	if !docmodel.Equal(got, model) {
		t.Fatalf("the highlight's id did not survive the fragment:\n got: %#v\nwant: %#v", got, model)
	}
	if run := got.Blocks[0].Inlines[1].Attr(docmodel.Highlight, docmodel.RunAttr); run != "r1" {
		t.Errorf("run = %q, want r1", run)
	}
}
