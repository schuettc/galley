package markdown_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// roundTrip parses src, serializes the result, and reports both the bytes and
// the parsed model — the shape every anchor test here wants.
func roundTrip(t *testing.T, src string) (docmodel.Doc, string) {
	t.Helper()
	model, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return model, string(markdown.Serialize(model))
}

// noteAt returns the anchor and text of the nth Note block at the top level.
func noteAt(t *testing.T, d docmodel.Doc, n int) (anchor, text string) {
	t.Helper()
	seen := 0
	for _, b := range d.Blocks {
		if b.Kind != docmodel.Note {
			continue
		}
		if seen == n {
			return markdown.NoteAnchor(b), markdown.NoteText(b)
		}
		seen++
	}
	t.Fatalf("no note #%d in %#v", n, d.Blocks)
	return "", ""
}

func TestNote_BlockAnchorRoundTrips(t *testing.T) {
	src := "![a diagram](arch.svg)\n\n{>>this is out of date<<}\n"
	model, out := roundTrip(t, src)
	if out != src {
		t.Fatalf("Serialize(Parse(src)) = %q, want %q", out, src)
	}
	if len(model.Blocks) != 2 {
		t.Fatalf("blocks = %#v, want image + note", model.Blocks)
	}
	if model.Blocks[0].Kind != docmodel.Image {
		t.Errorf("blocks[0].Kind = %q, want image", model.Blocks[0].Kind)
	}
	anchor, text := noteAt(t, model, 0)
	if anchor != docmodel.AnchorBlock || text != "this is out of date" {
		t.Errorf("note = (%q, %q), want (block, %q)", anchor, text, "this is out of date")
	}
}

func TestNote_DocumentAnchorRoundTrips(t *testing.T) {
	src := "# Spec\n\nSome prose.\n\n{>>@document needs a worked example<<}\n"
	model, out := roundTrip(t, src)
	if out != src {
		t.Fatalf("Serialize(Parse(src)) = %q, want %q", out, src)
	}
	anchor, text := noteAt(t, model, 0)
	if anchor != docmodel.AnchorDocument || text != "needs a worked example" {
		t.Errorf("note = (%q, %q), want (document, %q)", anchor, text, "needs a worked example")
	}
}

// The failure this whole discriminator exists to prevent: a note sitting at
// the END of the last paragraph must stay a RANGE comment, even though it is
// also the last thing in the file, where a position-only "trailing note means
// document" rule would call it a document comment.
func TestNote_InlineNoteAtEndOfParagraphIsNotADocumentComment(t *testing.T) {
	inline := "The build is slow. {>>why?<<}\n"
	model, comments, err := markdown.Parse([]byte(inline))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, b := range model.Blocks {
		if b.Kind == docmodel.Note {
			t.Fatalf("a note at the end of a paragraph became a %s block: %#v", b.Kind, model.Blocks)
		}
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %#v, want one range comment", comments)
	}
	if comments[0].Text != "why?" || comments[0].Offset != len("The build is slow.") {
		t.Errorf("comment = %+v, want text %q at offset %d", comments[0], "why?", len("The build is slow."))
	}

	// The same words, the note on its own line: now it is block-level.
	standalone := "The build is slow.\n\n{>>why?<<}\n"
	sModel, sComments, err := markdown.Parse([]byte(standalone))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(sComments) != 0 {
		t.Errorf("comments = %#v, want none (the note is a block)", sComments)
	}
	anchor, text := noteAt(t, sModel, 0)
	if anchor != docmodel.AnchorBlock || text != "why?" {
		t.Errorf("note = (%q, %q), want (block, why?)", anchor, text)
	}
}

func TestNote_HandEditedFileRoundTripsByteIdentically(t *testing.T) {
	cases := map[string]string{
		"after a fence": "```go\nfmt.Println(1)\n```\n\n{>>this leaks<<}\n",
		"after a heading and before more prose": "# Title\n\n{>>too terse<<}\n\n" +
			"Body text.\n",
		"two block notes in a row":    "A.\n\n{>>one<<}\n\n{>>two<<}\n",
		"document note in the middle": "{>>@document overall: fine<<}\n\nA.\n",
		"note inside a list item":     "- item\n\n  {>>about the item<<}\n",
		"empty note":                  "A.\n\n{>><<}\n",
		"note with markdown in it":    "A.\n\n{>>use \\*emphasis\\* and \\`code\\` here<<}\n",
		"inline note mid-paragraph":   "One two three.\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			model, out := roundTrip(t, src)
			if out != src {
				t.Fatalf("Serialize(Parse(src)) = %q, want %q", out, src)
			}
			// And again: the second cycle must be a fixed point too.
			_, out2 := roundTrip(t, out)
			if out2 != out {
				t.Fatalf("second cycle = %q, want %q", out2, out)
			}
			_ = model
		})
	}
}

// A note whose text is markdown-significant must come back verbatim: goldmark
// parses the paragraph before the CriticMarkup scanner ever sees it, so an
// unescaped "*" would be eaten as an emphasis delimiter.
func TestNote_TextWithMarkdownSurvives(t *testing.T) {
	want := "use *emphasis* and `code` here"
	src := string(markdown.Serialize(docmodel.Doc{Blocks: []docmodel.Block{
		para(text("A.")),
		markdown.NewNote(docmodel.AnchorBlock, want),
	}}))
	model, out := roundTrip(t, src)
	if out != src {
		t.Fatalf("not a fixed point: %q vs %q", out, src)
	}
	_, got := noteAt(t, model, 0)
	if got != want {
		t.Errorf("note text = %q, want %q", got, want)
	}
}

// The @block escape: a BLOCK note whose text itself starts with "@document"
// must not be read back as a document note.
func TestNote_MarkerEscape(t *testing.T) {
	for _, tc := range []struct {
		anchor string
		text   string
	}{
		{docmodel.AnchorBlock, "@document is a marker word"},
		{docmodel.AnchorBlock, "@block is too"},
		{docmodel.AnchorDocument, "@document twice over"},
		{docmodel.AnchorDocument, ""},
		{docmodel.AnchorBlock, ""},
	} {
		src := string(markdown.Serialize(docmodel.Doc{Blocks: []docmodel.Block{
			para(text("A.")),
			markdown.NewNote(tc.anchor, tc.text),
		}}))
		model, out := roundTrip(t, src)
		if out != src {
			t.Fatalf("not a fixed point for %+v: %q vs %q", tc, out, src)
		}
		anchor, got := noteAt(t, model, 0)
		if anchor != tc.anchor || got != tc.text {
			t.Errorf("note %+v round-tripped to (%q, %q)", tc, anchor, got)
		}
	}
}

// A note that cannot be spelled keeps the author's words and loses the
// marker — the same ruling wrapSuggestion makes for an unspellable deletion.
func TestNote_UnwritableTextKeepsTheWords(t *testing.T) {
	const words = "this closes early <<} oops"
	if !markdown.UnwritableNoteText(words) {
		t.Fatalf("UnwritableNoteText(%q) = false, want true", words)
	}
	out := string(markdown.Serialize(docmodel.Doc{Blocks: []docmodel.Block{
		markdown.NewNote(docmodel.AnchorBlock, words),
	}}))
	if strings.Contains(out, "{>>") {
		t.Errorf("Serialize wrote a marker it cannot close: %q", out)
	}
	if !strings.Contains(out, "this closes early") {
		t.Errorf("Serialize dropped the author's words: %q", out)
	}
}

// noteBodies is every {>>…<<} body in src, in order — what must still be in
// the file after a round trip.
func noteBodies(src string) []string {
	var out []string
	for rest := src; ; {
		i := strings.Index(rest, "{>>")
		if i < 0 {
			return out
		}
		rest = rest[i+3:]
		j := strings.Index(rest, "<<}")
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j+3:]
	}
}

// The invariant this whole task exists for. A note a human typed is never
// removed from the file — not by an imprecise anchor, not by a shape the
// parser does not recognise. Where galley cannot tell what a note is about it
// must guess badly and visibly, never delete.
//
// Every case here is a shape a hand-editor produces without thinking, and
// every one of them silently emptied the file before this task.
func TestNote_IsNeverDeletedFromTheFile(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"flush under a paragraph", "para\n{>>note<<}\n"},
		{"two on consecutive lines", "{>>one<<}\n{>>two<<}\n"},
		{"two on one line", "{>>one<<} {>>two<<}\n"},
		{"three on consecutive lines", "{>>a<<}\n{>>b<<}\n{>>c<<}\n"},
		{"after a hard break", "a\\\n{>>n<<}\n"},
		{"before a hard-broken line", "{>>note<<}  \nx\n"},
		{"under a paragraph, with a blank line after", "para\n{>>note<<}\n\nmore\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			out := string(markdown.Serialize(doc))

			// Count, not equality: the anchor may legitimately be re-spelled
			// (a note flush under a paragraph gains its blank line). What may
			// never change is how many of the author's notes survive.
			if got, want := strings.Count(out, "{>>"), strings.Count(tc.in, "{>>"); got != want {
				t.Errorf("%d notes survived, want %d\n in: %q\nout: %q", got, want, tc.in, out)
			}
			// And the words themselves.
			for _, word := range noteBodies(tc.in) {
				if !strings.Contains(out, word) {
					t.Errorf("note body %q was deleted\n in: %q\nout: %q", word, tc.in, out)
				}
			}
		})
	}
}

// Whatever spelling the fix chooses, the SECOND write must be a fixed point —
// this codebase's convergence bar. A first write that re-spells is fine; a
// second write that re-spells again is a document that never settles.
func TestNote_RecoveredShapesConverge(t *testing.T) {
	for _, in := range []string{
		"para\n{>>note<<}\n",
		"{>>one<<}\n{>>two<<}\n",
		"{>>one<<} {>>two<<}\n",
		"a\\\n{>>n<<}\n",
		"{>>note<<}  \nx\n",
		"para\n{>>note<<}\n\nmore\n",
		"{>>a<<}\n{>>b<<}\n{>>c<<}\n",
	} {
		doc, _, err := markdown.Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		first := markdown.Serialize(doc)
		again, _, err := markdown.Parse(first)
		if err != nil {
			t.Fatal(err)
		}
		if second := markdown.Serialize(again); string(second) != string(first) {
			t.Errorf("not a fixed point on the second write\nin:     %q\nfirst:  %q\nsecond: %q", in, first, second)
		}
	}
}

// The STRUCTURAL half of "a note a human typed is never deleted".
//
// TestNote_IsNeverDeletedFromTheFile covers the shapes a hand-editor produces.
// This covers the ones nobody would type on purpose — marker soup a fuzzer or a
// generated document can reach — and it exists because a RULE is not enough.
// Teaching the parser to recognise one more shape leaves the next unrecognised
// shape being deleted just as quietly, which is exactly how the two Criticals
// in this task came to be written in the first place.
//
// So the guarantee is about the OUTCOME of not recognising something: a
// paragraph whose entire content was comment syntax cannot be emptied and
// dropped. Whatever was lifted out of it comes back as notes, however imprecise
// their anchor. Anchor it badly, keep it as literal text — but never drop it.
func TestNote_AnEmptiedParagraphKeepsItsNotes(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     int
	}{
		{"two highlights each wrapping only a comment", "{=={>>a<<}==}{=={>>b<<}==}\n", 2},
		{"the same, separated by a space", "{=={>>a<<}==} {=={>>b<<}==}\n", 2},
		{"an insertion and a deletion, each only a comment", "{++{>>a<<}++}{--{>>b<<}--}\n", 2},
		{"one highlight wrapping only a comment", "{=={>>a<<}==}\n", 1},
		{"two bare notes with no separator", "{>>a<<}{>>b<<}\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			out := string(markdown.Serialize(doc))
			if got := strings.Count(out, "{>>"); got != tc.want {
				t.Errorf("%d notes survived, want %d\n in: %q\nout: %q", got, tc.want, tc.in, out)
			}
			for _, body := range noteBodies(tc.in) {
				if !strings.Contains(out, body) {
					t.Errorf("note body %q was deleted\n in: %q\nout: %q", body, tc.in, out)
				}
			}
			// And it settles, like every other recovered shape.
			again, _, err := markdown.Parse([]byte(out))
			if err != nil {
				t.Fatalf("reparse %q: %v", out, err)
			}
			if second := string(markdown.Serialize(again)); second != out {
				t.Errorf("not a fixed point\n first:  %q\n second: %q", out, second)
			}
		})
	}
}

// The invariant underneath both note tests, stated as the disjunction it
// actually is, and checked over every shape either of them names plus the
// package's own fixtures.
//
// "Never deleted" cannot mean "always in the .md": a range comment —
// "The build is slow. {>>why?<<}" — is DESIGNED to be lifted out of the file
// into the sidecar, with a Highlight left on the text it covers. That is the
// whole comment lifecycle and CLAUDE.md states it.
//
// What must never happen is the third outcome, which is what both Criticals
// were: the note leaves the file AND is not recoverable — lifted against a
// BlockPath naming a block that the same pass just deleted, so nothing can
// ever put it back. So: every note in the input is either still in the output,
// or lifted into a comment whose BlockPath names a block that EXISTS.
func TestNote_IsInTheFileOrRecoverableFromTheSidecar(t *testing.T) {
	for _, in := range []string{
		"para\n{>>note<<}\n",
		"{>>one<<}\n{>>two<<}\n",
		"{>>one<<} {>>two<<}\n",
		"a\\\n{>>n<<}\n",
		"{>>note<<}  \nx\n",
		"The build is slow. {>>why?<<}\n",
		"{=={>>a<<}==}{=={>>b<<}==}\n",
		"{>>a<<}{==x{>>b<<}==}\n",
		"{++{>>a<<}++}{--{>>b<<}--}\n",
		"{~~{>>a<<}~>x~~}\n",
		"> {>>q<<}\n",
		"- {>>l<<}\n",
		"# {>>h<<}\n",
	} {
		doc, lifted, err := markdown.Parse([]byte(in))
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		out := string(markdown.Serialize(doc))
		kept := strings.Count(out, "{>>")
		if kept+len(lifted) < strings.Count(in, "{>>") {
			t.Errorf("a note went missing entirely\n in: %q\nout: %q  kept=%d lifted=%d",
				in, out, kept, len(lifted))
		}
		// Every lifted comment must name a block that survived, or it is
		// anchored to nothing and can never be shown again.
		for _, c := range lifted {
			if !blockExistsAt(doc, c.BlockPath) {
				t.Errorf("comment %q lifted from %q is anchored to a block that no longer exists (path %v)",
					c.Text, in, c.BlockPath)
			}
		}
	}
}

// blockExistsAt reports whether path names a block in d.
func blockExistsAt(d docmodel.Doc, path []int) bool {
	blocks := d.Blocks
	for i, idx := range path {
		if idx < 0 || idx >= len(blocks) {
			return false
		}
		if i == len(path)-1 {
			return true
		}
		blocks = blocks[idx].Children
	}
	return false
}

// TestParse_AStandaloneCommentIDIsANoteWithNoWords: a block comment's mark in
// the file is "{>>@comment cb-…<<}" on its own line. Its words live in the
// unsent round, so the Note it parses to carries the id and no text at all —
// after a paragraph, after a fence and inside a table cell alike.
func TestParse_AStandaloneCommentIDIsANoteWithNoWords(t *testing.T) {
	const id = "cb-0123456789abcdef"
	want := map[string]string{"anchor": docmodel.AnchorBlock, docmodel.CommentIDAttr: id}
	check := func(t *testing.T, b docmodel.Block) {
		t.Helper()
		if b.Kind != docmodel.Note {
			t.Fatalf("block is a %q, want a note: %#v", b.Kind, b)
		}
		if !reflect.DeepEqual(b.Attrs, want) {
			t.Errorf("note attrs = %v, want %v", b.Attrs, want)
		}
		if len(b.Inlines) != 0 {
			t.Errorf("note inlines = %#v, want none — the words are not in the file", b.Inlines)
		}
	}
	cases := []struct {
		name string
		src  string
		pick func(docmodel.Doc) docmodel.Block
	}{
		{"after a paragraph", "para\n\n{>>@comment " + id + "<<}\n",
			func(d docmodel.Doc) docmodel.Block { return d.Blocks[1] }},
		{"after a fence", "```go\nx := 1\n```\n\n{>>@comment " + id + "<<}\n",
			func(d docmodel.Doc) docmodel.Block { return d.Blocks[1] }},
		{"in a table cell", "| a | b |\n| --- | --- |\n| x | {>>@comment " + id + "<<} |\n",
			func(d docmodel.Doc) docmodel.Block { return d.Blocks[0].Children[1].Children[1].Children[0] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, comments, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(comments) != 0 {
				t.Errorf("comments = %#v, want none", comments)
			}
			check(t, tc.pick(doc))
			if got := string(markdown.Serialize(doc)); got != tc.src {
				t.Errorf("Serialize = %q, want the source back %q", got, tc.src)
			}
		})
	}
}

// TestParse_AWordNoteIsReadAsBefore pins what a hand-typed note with words
// still parses to: the spec drops this case, and this is the default
// behaviour it gets, not a feature.
func TestParse_AWordNoteIsReadAsBefore(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("para\n\n{>>legacy words<<}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b := doc.Blocks[1]
	if b.Kind != docmodel.Note || markdown.NoteText(b) != "legacy words" {
		t.Fatalf("block = %#v, want a note reading %q", b, "legacy words")
	}
	if _, ok := b.Attrs[docmodel.CommentIDAttr]; ok {
		t.Errorf("a word note carries an id: %v", b.Attrs)
	}
}

// TestNote_ADocumentNoteNeverWritesACommentID: a whole-document comment never
// has a mark in the file (its words live in the unsent round, and there is no
// place to point at). An ID mark always reads back as a BLOCK note, so a
// document note that somehow carries an id must not be spelled as one — it
// would come back as a comment on the block above it. It is written as the
// @document form it is, and the id is left out.
func TestNote_ADocumentNoteNeverWritesACommentID(t *testing.T) {
	const id = "cd-0123456789abcdef"
	for _, tc := range []struct {
		name, text, want string
	}{
		{"with words", "needs a worked example", "{>>@document needs a worked example<<}"},
		{"with no words", "", "{>>@document<<}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := markdown.NewNote(docmodel.AnchorDocument, tc.text)
			note.Attrs[docmodel.CommentIDAttr] = id
			model := docmodel.Doc{Blocks: []docmodel.Block{
				{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "Some prose."}}},
				note,
			}}
			out := string(markdown.Serialize(model))
			if strings.Contains(out, "@comment") {
				t.Fatalf("Serialize = %q: a document note was written as an ID mark", out)
			}
			if want := "Some prose.\n\n" + tc.want + "\n"; out != want {
				t.Errorf("Serialize = %q, want %q", out, want)
			}
			back, _, err := markdown.Parse([]byte(out))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if anchor, _ := noteAt(t, back, 0); anchor != docmodel.AnchorDocument {
				t.Errorf("read back as a %q note, want document", anchor)
			}
		})
	}
}
