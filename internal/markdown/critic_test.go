package markdown_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/suggest"
)

// suggestion builds a suggestion mark carrying the author/at attrs a real
// suggestion has, used to prove those attrs do NOT survive a round trip
// through CriticMarkup (the syntax has nowhere to put them).
func suggestion(kind docmodel.MarkKind, author string) docmodel.Mark {
	return docmodel.Mark{
		Kind:  kind,
		Attrs: map[string]string{"author": author, "at": "2026-08-05T00:00:00Z"},
	}
}

// TestParse_CriticFixture pins the tree the golden fixture parses to. The
// byte-identical round trip of the same file is covered by
// TestSerialize_RoundTripsFixtures, which globs testdata/*.md.
func TestParse_CriticFixture(t *testing.T) {
	src := mustRead(t, "testdata/critic.md")
	got, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("Parse(critic.md) comments = %#v, want none (the golden fixture has no {>><<})", comments)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind:    docmodel.Heading,
			Attrs:   map[string]string{"level": "1"},
			Inlines: []docmodel.Inline{text("Review notes")},
		},
		para(
			text("The build"),
			marked(" step", mark(docmodel.Ins)),
			text(" runs first."),
		),
		para(
			text("Drop the "),
			marked("redundant", mark(docmodel.Del)),
			text(" flag."),
		),
		para(
			text("Change "),
			marked("old value", mark(docmodel.Del)),
			marked("new value", mark(docmodel.Ins)),
			text(" before merge."),
		),
		para(
			text("The "),
			marked("important part", mark(docmodel.Highlight)),
			text(" needs another look."),
		),
		para(
			text("Rename "),
			marked("oldFunc", mark(docmodel.Code), mark(docmodel.Del)),
			text(" across the API."),
		),
		para(
			text("Literal markers like "),
			marked("{++not a suggestion++}", mark(docmodel.Code)),
			text(" stay code."),
		),
		para(
			marked("Bold", mark(docmodel.Bold)),
			text(" "),
			marked("and an insertion", mark(docmodel.Ins)),
			text(" sit side by side."),
		),
		para(
			text("Emoji survive: "),
			marked("☕ 😀", mark(docmodel.Ins)),
			text("."),
		),
	}}
	if !docmodel.Equal(got, want) {
		t.Errorf("Parse(testdata/critic.md) mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// TestParse_Comment_ExtractedOutOfBand is the {>>note<<} contract: the note
// never becomes document text, it comes back as an InlineComment anchored at
// a rune offset into the block's own text, and Serialize never writes it
// back — what the caller does with a lifted note is not the file's business.
func TestParse_Comment_ExtractedOutOfBand(t *testing.T) {
	src := []byte("Check {==this claim==}{>>needs a citation<<} before merge.\n")
	doc, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	wantDoc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("Check "),
			marked("this claim", mark(docmodel.Highlight)),
			text(" before merge."),
		),
	}}
	if !docmodel.Equal(doc, wantDoc) {
		t.Errorf("Parse mismatch:\n got:  %#v\n want: %#v", doc, wantDoc)
	}
	wantComments := []markdown.InlineComment{
		{BlockPath: []int{0}, Offset: 16, Text: "needs a citation"},
	}
	if !reflect.DeepEqual(comments, wantComments) {
		t.Errorf("comments mismatch:\n got:  %#v\n want: %#v", comments, wantComments)
	}
	want := "Check {==this claim==} before merge.\n"
	if got := string(markdown.Serialize(doc)); got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

// TestParse_Comment_OffsetCountsRunes checks the offset is measured in runes
// of the block's final text (markers already removed), not bytes — the text
// before the note here is 8 runes and 15 bytes.
func TestParse_Comment_OffsetCountsRunes(t *testing.T) {
	src := []byte("☕ 😀 note{>>see this<<} here.\n")
	_, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []markdown.InlineComment{
		{BlockPath: []int{0}, Offset: 8, Text: "see this"},
	}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("comments mismatch:\n got:  %#v\n want: %#v", comments, want)
	}
}

// TestParse_Comment_BlockPathAddressesNestedBlock checks the path is the
// docmodel.Walk path of the block the note was anchored in, not just a
// top-level index: here the paragraph is the first block of the first list
// item of the second top-level block.
func TestParse_Comment_BlockPathAddressesNestedBlock(t *testing.T) {
	src := []byte("Intro.\n\n- item{>>why?<<}\n")
	_, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []markdown.InlineComment{
		{BlockPath: []int{1, 0, 0}, Offset: 4, Text: "why?"},
	}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("comments mismatch:\n got:  %#v\n want: %#v", comments, want)
	}
}

// TestParse_MarkersInsideCodeSpanAreLiteral: a code span's content is
// literal by definition, so CriticMarkup inside one is text, not syntax.
func TestParse_MarkersInsideCodeSpanAreLiteral(t *testing.T) {
	assertCriticRoundTrip(t, "`{--x--}` and `{>>y<<}`\n", docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("{--x--}", mark(docmodel.Code)),
			text(" and "),
			marked("{>>y<<}", mark(docmodel.Code)),
		),
	}})
}

// TestParse_UnmatchedOpenerStaysLiteral: an opener with no closer is
// ordinary text and must survive as typed.
func TestParse_UnmatchedOpenerStaysLiteral(t *testing.T) {
	assertCriticRoundTrip(t, "Use {++ carefully, and {>> too.\n", docmodel.Doc{Blocks: []docmodel.Block{
		para(text("Use {++ carefully, and {>> too.")),
	}})
}

// TestParse_SpanAcrossEmphasis_AppliesPerInline: a CriticMarkup span may
// cover text that changes emphasis partway through. A suggestion mark is
// per-Inline, so the span becomes the SAME mark on each Inline it covers —
// and serializes back as one marker pair per Inline, which is not
// byte-identical to the source but is a fixed point, and loses no marks.
func TestParse_SpanAcrossEmphasis_AppliesPerInline(t *testing.T) {
	src := "a {++b **c**++} d\n"
	doc, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("a "),
			marked("b ", mark(docmodel.Ins)),
			marked("c", mark(docmodel.Bold), mark(docmodel.Ins)),
			text(" d"),
		),
	}}
	if !docmodel.Equal(doc, want) {
		t.Fatalf("Parse(%q) mismatch:\n got:  %#v\n want: %#v", src, doc, want)
	}
	assertEscapeAcrossBoundary(t, doc, "a {++b ++}{++**c**++} d\n", want)
}

// TestParse_NestedHighlightAroundInsertion: a differently-kinded span
// inside a matched span parses too, giving the inner text both marks. The
// mark order is outermost-first, matching the source nesting.
func TestParse_NestedHighlightAroundInsertion(t *testing.T) {
	assertCriticRoundTrip(t, "{=={++b++}==}\n", docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("b", mark(docmodel.Highlight), mark(docmodel.Ins))),
	}})
}

// TestParse_SubstitutionInsideHeading checks the scanner runs on Heading
// inlines, not only Paragraph ones.
func TestParse_SubstitutionInsideHeading(t *testing.T) {
	assertCriticRoundTrip(t, "# Ship {~~soon~>today~~}\n", docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind:  docmodel.Heading,
			Attrs: map[string]string{"level": "1"},
			Inlines: []docmodel.Inline{
				text("Ship "),
				marked("soon", mark(docmodel.Del)),
				marked("today", mark(docmodel.Ins)),
			},
		},
	}})
}

// assertCriticRoundTrip parses src, checks the tree, and checks that
// serializing it reproduces src byte-for-byte — the canonical-form claim
// for every shape in this file.
func assertCriticRoundTrip(t *testing.T, src string, want docmodel.Doc) {
	t.Helper()
	got, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	if !docmodel.Equal(got, want) {
		t.Errorf("Parse(%q) mismatch:\n got:  %#v\n want: %#v", src, got, want)
	}
	if out := string(markdown.Serialize(got)); out != src {
		t.Errorf("Serialize(Parse(%q)) mismatch:\n got:  %q\n want: %q", src, out, src)
	}
}

// TestSerialize_Substitution_AdjacentDelInsPair is the other direction of
// {~~old~>new~~}: a Del run immediately followed by an Ins run is the
// substitution shape, and must serialize as one span rather than
// "{--old--}{++new++}".
func TestSerialize_Substitution_AdjacentDelInsPair(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			text("Change "),
			marked("old", mark(docmodel.Del)),
			marked("new", mark(docmodel.Ins)),
			text(" now."),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "Change {~~old~>new~~} now.\n", doc)
}

// TestSerialize_InsThenDel_IsNotASubstitution guards the ordering half of
// the rule: only Del-then-Ins is a substitution. The reverse order is two
// independent spans.
func TestSerialize_InsThenDel_IsNotASubstitution(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("new", mark(docmodel.Ins)),
			marked("old", mark(docmodel.Del)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "{++new++}{--old--}\n", doc)
}

// TestSerialize_SuggestionAttrsAreNotRepresentable states the one lossy
// edge of CriticMarkup: the syntax has nowhere to carry a suggestion's
// author/at, so they are dropped on serialization. The text is not — and
// the result is still a fixed point, which is what the round trip owes.
func TestSerialize_SuggestionAttrsAreNotRepresentable(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("x", suggestion(docmodel.Ins, "ann"))),
	}}
	normalized := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("x", mark(docmodel.Ins))),
	}}
	assertEscapeAcrossBoundary(t, doc, "{++x++}\n", normalized)
}

// TestSerialize_DifferentAuthorsStayDistinctSpans is the "the suggestion
// difference is real" half of the run-boundary rule: two Ins runs by
// different authors are different spans, so they get their own markers
// rather than being fused into one.
//
// IT USED TO MERGE THEM BACK, AND NOW IT DOES NOT. Reparsing `{++a++}{++b++}`
// once produced a single Ins over "ab": the authors are the only thing telling
// the spans apart, CriticMarkup cannot carry an author, so the two marker pairs
// came back with identical marks and coalesced. Two decisions became one — the
// same rule this file now enforces in the other direction, since the reviewer
// who accepts "ab" has decided ann's edit and bob's with one click.
//
// The parser stamping a run per span (applyMark) fixes it: the marks differ, so
// rebuildInlines keeps them apart. The file is now a fixed point on the FIRST
// write rather than the second, which is why this goes through
// assertEscapeAcrossBoundary like every other shape.
//
// The authors themselves are still lost — CriticMarkup has nowhere to put
// them. What survives now
// is the span BOUNDARY, which is what decides how many decisions the file holds.
func TestSerialize_DifferentAuthorsStayDistinctSpans(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("a", suggestion(docmodel.Ins, "ann")),
			marked("b", suggestion(docmodel.Ins, "bob")),
		),
	}}
	m1 := markdown.Serialize(doc)
	if string(m1) != "{++a++}{++b++}\n" {
		t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", m1, "{++a++}{++b++}\n")
	}
	// Still two spans, not one "ab": the file said two and the file is believed.
	stillTwo := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("a", mark(docmodel.Ins)),
			marked("b", mark(docmodel.Ins)),
		),
	}}
	doc2, _, err := markdown.Parse(m1)
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}
	if !docmodel.Equal(doc2, stillTwo) {
		t.Fatalf("Parse(Serialize(doc)) mismatch:\n got:  %#v\n want: %#v", doc2, stillTwo)
	}
	// The two runs are distinct, which is what keeps them two decisions.
	if a, b := doc2.Blocks[0].Inlines[0], doc2.Blocks[0].Inlines[1]; a.Attr(docmodel.Ins, docmodel.RunAttr) ==
		b.Attr(docmodel.Ins, docmodel.RunAttr) {
		t.Errorf("both spans carry run %q — one accept would decide both",
			a.Attr(docmodel.Ins, docmodel.RunAttr))
	}
	// A fixed point on the first write now, not the second.
	assertEscapeAcrossBoundary(t, doc2, "{++a++}{++b++}\n", stillTwo)
}

// TestSerialize_SuggestionWrapsEmphasis pins the mark nesting order: the
// CriticMarkup markers go OUTSIDE emphasis and link delimiters. The
// alternative, "**{--x--}**", does not read back at all — an opener
// followed by "{" and preceded by a letter is not left-flanking, so
// CommonMark declines to open emphasis there and the bold mark is lost.
func TestSerialize_SuggestionWrapsEmphasis(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("x", mark(docmodel.Bold), mark(docmodel.Del))),
	}}
	assertEscapeAcrossBoundary(t, doc, "{--**x**--}\n", doc)
}

// TestSerialize_HardBreakSplitsASuggestion checks a hard break inside a
// suggestion: the break is a zero-text sentinel that emits its own line
// ending, so the suggestion is written as two spans around it.
func TestSerialize_HardBreakSplitsASuggestion(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("one", mark(docmodel.Ins)),
			hardBreak(),
			marked("two", mark(docmodel.Ins)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "{++one++}\\\n{++two++}\n", doc)
}

// TestParse_CommentOnlyParagraph_IsABlockAnchor: a paragraph whose entire
// content is a note is not a position in prose at all — there is no prose
// left to be a position in. It is a BLOCK anchor on the block above it, and
// unlike a range comment it stays IN the document, because the file is the
// only carrier a block anchor has (see note.go).
//
// This test previously asserted the opposite: the note was lifted out
// out-of-band and the empty paragraph dropped, so `{>>just a note<<}`
// vanished from the file on the first save. That is the behaviour this change exists to replace.
func TestParse_CommentOnlyParagraph_IsABlockAnchor(t *testing.T) {
	src := []byte("Before.\n\n{>>just a note<<}\n\nAfter.\n")
	doc, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("comments = %#v, want none — the note is a block, not a position", comments)
	}
	assertEscapeAcrossBoundary(t, doc, string(src), docmodel.Doc{Blocks: []docmodel.Block{
		para(text("Before.")),
		wordNote(docmodel.AnchorBlock, "just a note"),
		para(text("After.")),
	}})
}

// TestParse_CommentRemovalTrimsEdgeWhitespace: lifting a note out can
// leave whitespace at the start or end of a line, which markdown drops on
// the next read. Parse drops it at the same places goldmark does, so the
// text it reports is the text a re-read would produce.
func TestParse_CommentRemovalTrimsEdgeWhitespace(t *testing.T) {
	src := []byte("{>>lead<<} text {>>trail<<}\n")
	doc, comments, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %#v, want 2", comments)
	}
	assertEscapeAcrossBoundary(t, doc, "text\n", docmodel.Doc{Blocks: []docmodel.Block{
		para(text("text")),
	}})
}

// TestSerialize_DeletionContainingItsOwnCloser: CriticMarkup has no
// escape, and the reader closes at the FIRST closer, so "{--" + "--}" +
// "--}" would read back as an empty deletion followed by loose text —
// losing the author's words. The deletion is written in its other
// spelling instead, a substitution with an empty insertion, which closes
// on "~~}" and reads back as exactly Del("--}").
func TestSerialize_DeletionContainingItsOwnCloser(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("--}", mark(docmodel.Del))),
	}}
	assertEscapeAcrossBoundary(t, doc, "{~~--}~>~~}\n", doc)
}

// TestSerialize_InsertionContainingItsOwnCloser is the same fallback for
// an insertion: "{~~~>new~~}", a substitution with an empty deletion.
func TestSerialize_InsertionContainingItsOwnCloser(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("++}", mark(docmodel.Ins))),
	}}
	assertEscapeAcrossBoundary(t, doc, "{~~~>++}~~}\n", doc)
}

// TestSerialize_SuggestionWithNoSafeSpelling states the limit: text that
// breaks BOTH spellings (or a highlight, which has only one) is written
// plainly. The mark is dropped and the
// text survives exactly, which is the property that must never bend.
func TestSerialize_SuggestionWithNoSafeSpelling(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  docmodel.Doc
		want string
	}{
		{
			name: "deletion breaking both spellings",
			doc:  docmodel.Doc{Blocks: []docmodel.Block{para(marked("--}~~}", mark(docmodel.Del)))}},
			want: "--}~~}\n",
		},
		{
			name: "highlight, which has no second spelling",
			doc:  docmodel.Doc{Blocks: []docmodel.Block{para(marked("==}", mark(docmodel.Highlight)))}},
			want: "==}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := markdown.Serialize(tc.doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", got, tc.want)
			}
			reparsed, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(Serialize(doc)): %v", err)
			}
			gotText := reparsed.Blocks[0].Inlines[0].Text
			wantText := tc.doc.Blocks[0].Inlines[0].Text
			if gotText != wantText {
				t.Errorf("text corrupted: got %q, want %q", gotText, wantText)
			}
			if len(reparsed.Blocks[0].Inlines[0].Marks) != 0 {
				t.Errorf("marks = %#v, want none (the mark is the part that is allowed to be lost)", reparsed.Blocks[0].Inlines[0].Marks)
			}
		})
	}
}

// TestSerialize_AdjacentEmphasisSegments_DoNotFuse is the emphasis
// analogue of fix round 4's fence fusion, which Task 4's segmentation
// would otherwise have made reachable: Bold("a") next to Bold+Del("b")
// must not serialize as "**a**" + "**{--b--}**", whose interior "****"
// closes nothing. The markers sit outside the emphasis, so the two "**"
// runs are separated by "{--" and both survive.
func TestSerialize_AdjacentEmphasisSegments_DoNotFuse(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("a", mark(docmodel.Bold)),
			marked("b", mark(docmodel.Bold), mark(docmodel.Del)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "**a**{--**b**--}\n", doc)
}

// TestSerialize_SubstitutionAcrossEmphasis checks both halves keep their
// own emphasis inside the one shared marker pair.
func TestSerialize_SubstitutionAcrossEmphasis(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("old", mark(docmodel.Bold), mark(docmodel.Del)),
			marked("new", mark(docmodel.Bold), mark(docmodel.Ins)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "{~~**old**~>**new**~~}\n", doc)
}

// TestSerialize_CriticFixedPointCorpus is the adversarial net Task 5's
// fuzzer will formalize: for each input, Serialize(Parse(src)) must be a
// fixed point — parsing and re-serializing it reproduces the same bytes.
// Every case here is a shape that broke, or nearly broke, while Task 4
// was being written: markers that half-match, empty spans, markers whose
// closer appears in their own content, markers pressed against emphasis,
// code fences, hard breaks and each other.
//
// The claim is about BYTES, not about the tree: the first serialization
// is allowed to normalize (a paragraph emptied by lifting out its only
// comment is gone; nested spans of different kinds come back in the
// nesting order the writer emits, which need not be the order they were
// typed in). What may not change afterwards is the bytes — that is what
// makes a file stable in git.
//
// The claim is also about THESE inputs, not about every input: text that
// writes literal marker characters can be re-read as syntax on the next
// pass, so it settles on a later serialization instead of the first. That
// class is pinned separately, and honestly, by
// TestSerialize_LiteralMarkerText_Convergence.
func TestSerialize_CriticFixedPointCorpus(t *testing.T) {
	corpus := []string{
		"{++a++}", "{--a--}", "{==a==}", "{~~a~>b~~}", "{>>n<<}",
		"{++", "++}", "{~~a~~}", "{~~~>~~}", "{++++}", "{--}--}",
		"{++a{--b--}c++}", "{=={++b++}==}", "{~~{++a++}~>b~~}",
		"a{++b++}{--c--}d", "{--a--}{++b++}", "{++a++}{--b--}",
		"`{++a++}`", "{++`a`++}", "{++`a`++}`b`", "`a`{--`b`--}",
		"{++a++}\\\n{--b--}", "**{++a++}**", "{++**a**++}",
		"[{++a++}](x)", "{++[a](x)++}", "{++☕😀++}", "{++a\nb++}",
		"{++a+++}", "{--a---}", "{==a===}", "{~~a~>b~>c~~}",
		"{>>{++x++}<<}", "{++{>>x<<}++}", "{>>a<<}{>>b<<}",
		"# {++h++}", "- {--i--}\n- {++j++}", "> {==q==}",
		"{++a++}{++b++}", "{~~a~>b~~}{++c++}", "{~~a~>b~~}{--c--}",
		"{++ ++}", "{-- --}", "text {++ with spaces ++} here",
		"{{++a++}}", "{++}", "{+++}", "{++++++}", "~>", "{~~~~}",
		"{++a\\*b++}", "{++a`b++}", "{++a[b](c)++}",
		"{--~~}--}", "{++--}++}", "{==--}==}", "{~~--}~>++}~~}",
		"a {>>n<<} b", "{>>n<<} a", "a {>>n<<}", "a\\\n{>>n<<}",
	}
	for _, src := range corpus {
		t.Run(src, func(t *testing.T) {
			doc1, _, err := markdown.Parse([]byte(src + "\n"))
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			m1 := markdown.Serialize(doc1)
			doc2, _, err := markdown.Parse(m1)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", m1, err)
			}
			m2 := markdown.Serialize(doc2)
			if string(m1) != string(m2) {
				t.Errorf("not a fixed point:\n src: %q\n m1:  %q\n m2:  %q", src, m1, m2)
			}
		})
	}
}

// TestSerialize_UnspellableSuggestion_CoalescesWithNeighbour is fix round
// 1's Critical. wrapSuggestion can find no spelling for a suggestion whose
// text contains its own closer (and, for a highlight, there is no second
// spelling at all), in which case it writes no markers. Task 4's first
// draft still treated that segment as its own planned run, so nothing at
// all separated it from its neighbour: Code("a") next to
// Code+Del("--}~~}") wrote "`a`" flush against "`--}~~}`", whose interior
// two-backtick run closes nothing. That reparsed as ONE code span with two
// literal backticks injected into the content, and re-serializing widened
// the fence again instead of settling — round 4's exact pathology, back
// through the door Task 4 opened.
//
// Not reachable from Parse (first-closer-wins means Parse never produces
// such text), but trivially reachable from a hand-built doc: a reviewer
// highlighting a code span that contains "==}" is enough, which is exactly
// what Task 7 builds.
//
// The canonical answer is round 4's answer, applied only where Task 4's
// answer cannot be spelled: the marker-less segment is coalesced with its
// same-wrapper neighbours and the run is planned as one — one fence, one
// set of emphasis delimiters, suggestion marks dropped, TEXT INTACT.
func TestSerialize_UnspellableSuggestion_CoalescesWithNeighbour(t *testing.T) {
	code, bold, italic := mark(docmodel.Code), mark(docmodel.Bold), mark(docmodel.Italic)
	del, hl := mark(docmodel.Del), mark(docmodel.Highlight)
	tests := []struct {
		name       string
		inlines    []docmodel.Inline
		want       string
		normalized []docmodel.Inline
	}{
		{
			name:       "code run, deletion with no spelling",
			inlines:    []docmodel.Inline{marked("a", code), marked("--}~~}", code, del)},
			want:       "`a--}~~}`\n",
			normalized: []docmodel.Inline{marked("a--}~~}", code)},
		},
		{
			name:       "code run, highlight (which has no second spelling)",
			inlines:    []docmodel.Inline{marked("a", code), marked("==}", code, hl)},
			want:       "`a==}`\n",
			normalized: []docmodel.Inline{marked("a==}", code)},
		},
		{
			name:       "three segments, unspellable in the middle",
			inlines:    []docmodel.Inline{marked("a", code), marked("==}", code, hl), marked("b", code)},
			want:       "`a==}b`\n",
			normalized: []docmodel.Inline{marked("a==}b", code)},
		},
		{
			name:       "under a bold wrapper",
			inlines:    []docmodel.Inline{marked("a", bold, code), marked("--}~~}", bold, code, del)},
			want:       "**`a--}~~}`**\n",
			normalized: []docmodel.Inline{marked("a--}~~}", bold, code)},
		},
		{
			name:       "under an italic wrapper",
			inlines:    []docmodel.Inline{marked("a", italic, code), marked("==}", italic, code, hl)},
			want:       "*`a==}`*\n",
			normalized: []docmodel.Inline{marked("a==}", italic, code)},
		},
		{
			name:       "emphasis delimiters, no code span",
			inlines:    []docmodel.Inline{marked("a", bold), marked("--}~~}", bold, del)},
			want:       "**a--}~~}**\n",
			normalized: []docmodel.Inline{marked("a--}~~}", bold)},
		},
		{
			name:       "both segments unspellable",
			inlines:    []docmodel.Inline{marked("--}~~}", code, del), marked("==}", code, hl)},
			want:       "`--}~~}==}`\n",
			normalized: []docmodel.Inline{marked("--}~~}==}", code)},
		},
		{
			// The other side of the rule: a neighbour that DOES write
			// markers separates itself, so it must not be swallowed and
			// its mark must survive.
			name:       "spellable neighbour is not coalesced",
			inlines:    []docmodel.Inline{marked("a", code, mark(docmodel.Ins)), marked("==}", code, hl)},
			want:       "{++`a`++}`==}`\n",
			normalized: []docmodel.Inline{marked("a", code, mark(docmodel.Ins)), marked("==}", code)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := docmodel.Doc{Blocks: []docmodel.Block{para(tc.inlines...)}}
			normalized := docmodel.Doc{Blocks: []docmodel.Block{para(tc.normalized...)}}
			assertEscapeAcrossBoundary(t, doc, tc.want, normalized)
			if got, want := inlineText(tc.normalized), inlineText(tc.inlines); got != want {
				t.Errorf("text not preserved: normalized form says %q, original text is %q", got, want)
			}
			reparsed, _, err := markdown.Parse([]byte(tc.want))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.want, err)
			}
			if got, want := inlineText(reparsed.Blocks[0].Inlines), inlineText(tc.inlines); got != want {
				t.Errorf("text corrupted through the round trip: got %q, want %q", got, want)
			}
		})
	}
}

func inlineText(inlines []docmodel.Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		b.WriteString(in.Text)
	}
	return b.String()
}

// TestSerialize_LiteralMarkerText_Convergence pins a class this package does
// NOT fully handle, because CriticMarkup has no escape: marker characters that
// survive one write as literal text can be READ AS SYNTAX on the next one.
// Both documented cases settle on the first write now — see each one's note —
// but the class is why convergence, not first-write stability, is what the
// fuzz target asserts.
//
// Task 4's report originally claimed such text was "a fixed point from the
// first serialization onward". That is false, and these two cases are the
// proof (found by the reviewer, and by a marker-soup search over 200k
// inputs). The true property is weaker and is what Task 5's fuzz must be
// built on: the round trip CONVERGES — here on the third serialization —
// and it can lose text on the way. Only a document that never writes
// literal marker characters is stable on the first write.
//
// The complete fix is to make CriticMarkup escapable, which means moving
// backslash-unescaping to after the marker scan, so a content character that
// would complete a marker window can be escaped instead.
// Until then this test documents where the edge actually is, so a future
// change that moves it shows up as a diff here rather than as a surprise
// in someone's file.
func TestSerialize_LiteralMarkerText_Convergence(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		rounds []string // successive serializations, until stable
	}{
		{
			// One rewrite — the substitution's halves are respelled as two
			// plain deletions — and then a fixed point.
			//
			// It used to take a SECOND rewrite, and lose the punctuation
			// between them: the two `{--…--}` came back with identical marks,
			// coalesced into one deletion, and re-serialized as `x{--{b--}x`.
			// Two spans in the file became one, and a character vanished. The
			// parser's per-span run stops the coalescing, so what the first
			// write produced is what stays.
			name:   "deletion nested in substitution respells, then holds",
			src:    "x{--{~~}{--b~>~~}x\n",
			rounds: []string{"x{--{--}{--b--}x\n", "x{--{--}{--b--}x\n"},
		},
		{
			// Now byte-stable on the FIRST write, and this one was ugly.
			//
			// The two `{==…==}` used to coalesce on reparse, which spliced
			// their contents into the literal text `{>><<}`; the next read
			// took THAT as a comment, lifted it out of the document, and the
			// highlight was gone — earlier still, before block anchors, the
			// whole line went with it. None of it happens now: the spans stay
			// two, nothing splices, and the reviewer's file is returned
			// unchanged.
			name:   "literal comment markers survive as written",
			src:    "{=={>>==}{==<<}==}\n",
			rounds: []string{"{=={>>==}{==<<}==}\n", "{=={>>==}{==<<}==}\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.src
			for i, want := range tc.rounds {
				doc, _, err := markdown.Parse([]byte(cur))
				if err != nil {
					t.Fatalf("round %d: Parse(%q): %v", i, cur, err)
				}
				got := string(markdown.Serialize(doc))
				if got != want {
					t.Fatalf("round %d: Serialize(Parse(%q)) = %q, want %q", i, cur, got, want)
				}
				cur = got
			}
		})
	}
}

// TestSerialize_MarkerRemovalSplicesTextSafely was written by Task 4 to
// document a class that FAILED: removing markers joins the text on either
// side of them, and here "<" and "b>" — separated by an empty deletion in
// the source — became "<b>", which is raw HTML. Parse refuses raw HTML
// rather than mangling it, so the file no longer loaded at all. Task 4
// called that the most urgent of its eight classes, being the only one that
// fails loudly, and said the complete fix belonged in the escape layer.
//
// Task 5 put it there. FuzzRoundTrip found the same class from the other
// direction — "\<0@0>" serialized to an autolink, which Parse also refuses
// — and one rule in needsEscapeAt (opensAngleMarkup) closes both: the
// spliced "<" is now escaped, so it stays text.
//
// Task 4's version of this test asserted the FAILURE and said in its own
// message that if the round trip ever succeeded, the class was fixed and
// the test should become a round-trip assertion. This is that assertion.
// The splice still happens — the markers are gone and the text either side
// of them is now adjacent — it just no longer means anything it did not
// mean before.
func TestSerialize_MarkerRemovalSplicesTextSafely(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("x}<{----}b>x\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m1 := markdown.Serialize(doc)
	if string(m1) != "x}\\<b>x\n" {
		t.Fatalf("Serialize = %q, want %q", m1, "x}\\<b>x\n")
	}
	again, _, err := markdown.Parse(m1)
	if err != nil {
		t.Fatalf("Parse(%q): %v", m1, err)
	}
	if !docmodel.Equal(doc, again) {
		t.Errorf("the splice changed the text:\n got:  %#v\n want: %#v", again, doc)
	}
	if m2 := markdown.Serialize(again); string(m2) != string(m1) {
		t.Errorf("not a fixed point: %q then %q", m1, m2)
	}
}

// blockPlainText is the text an InlineComment's Offset indexes into: the
// concatenated runes of one block's Inlines. A hard break contributes no
// runes, which is exactly how flattenCells counts it.
func blockPlainText(b docmodel.Block) string {
	var sb strings.Builder
	for _, in := range b.Inlines {
		sb.WriteString(in.Text)
	}
	return sb.String()
}

// TestParse_Comment_SeamWhitespaceCollapses pins what the text reads like
// once a {>>note<<} has been lifted out of it, in every position a note can
// sit in — and pins, for each one, the word the comment is still anchored
// to.
//
// The rule the cases below encode: a note takes the whitespace that FOLLOWS
// it with it, but only when whitespace also PRECEDES it. That is the
// narrowest rule that removes the doubled separator the extraction itself
// introduced, and it is narrow on purpose:
//
//   - "a {>>n<<}b" keeps its space. Deleting a separator that the source
//     only had one of would fuse two words the author kept apart.
//   - "word {>>n<<}, next" keeps its space, for the same reason: the source
//     had one space there and the result still has one. Whether that space
//     "belonged to" the note or to the sentence is not decidable from the
//     text, and guessing wrong deletes an author's character.
//   - a code span's padding is never the preceding whitespace: its content
//     is literal, and "`x `{>>n<<} b" collapsing to "`x `b" would change
//     what the span sits next to.
//   - whitespace before a hard break is left alone, for the reason
//     trimLineEdges already documents: "a  \" preserves it.
//
// A note that is a block's ENTIRE content is deliberately not in this table
// — the empty-paragraph case is owned elsewhere and nothing here changes it.
//
// Offsets are asserted as the text FROM the anchor onward, because an
// off-by-one in a rune offset is invisible as a number and obvious as a
// word.
func TestParse_Comment_SeamWhitespaceCollapses(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    string
		offsets []int
		anchors []string
	}{
		{
			name:    "between words",
			src:     "A paragraph with {>>an inline note<<} in it.\n",
			want:    "A paragraph with in it.\n",
			offsets: []int{17},
			anchors: []string{"in it."},
		},
		{
			name:    "at the start of a block",
			src:     "{>>n<<} text\n",
			want:    "text\n",
			offsets: []int{0},
			anchors: []string{"text"},
		},
		{
			name:    "at the end of a block",
			src:     "text {>>n<<}\n",
			want:    "text\n",
			offsets: []int{4},
			anchors: []string{""},
		},
		{
			name:    "with no surrounding whitespace",
			src:     "a{>>n<<}b\n",
			want:    "ab\n",
			offsets: []int{1},
			anchors: []string{"b"},
		},
		{
			name:    "whitespace on the left only",
			src:     "a {>>n<<}b\n",
			want:    "a b\n",
			offsets: []int{2},
			anchors: []string{"b"},
		},
		{
			name:    "against punctuation",
			src:     "word {>>n<<}, next\n",
			want:    "word , next\n",
			offsets: []int{5},
			anchors: []string{", next"},
		},
		{
			name:    "two notes in a row",
			src:     "A {>>one<<} {>>two<<} B\n",
			want:    "A B\n",
			offsets: []int{2, 2},
			anchors: []string{"B", "B"},
		},
		{
			name:    "inside emphasis",
			src:     "**bold {>>n<<} text**\n",
			want:    "**bold text**\n",
			offsets: []int{5},
			anchors: []string{"text"},
		},
		{
			name:    "after a code span's own padding",
			src:     "`x `{>>n<<} b\n",
			want:    "`x ` b\n",
			offsets: []int{2},
			anchors: []string{" b"},
		},
		{
			name:    "before a hard break",
			src:     "a {>>n<<}\\\nb\n",
			want:    "a \\\nb\n",
			offsets: []int{2},
			anchors: []string{"b"},
		},
		{
			name:    "runs of whitespace either side",
			src:     "a  {>>n<<}  b\n",
			want:    "a  b\n",
			offsets: []int{3},
			anchors: []string{"b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, comments, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			if got := string(markdown.Serialize(doc)); got != tc.want {
				t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, tc.want)
			}
			if len(comments) != len(tc.offsets) {
				t.Fatalf("comments = %#v, want %d of them", comments, len(tc.offsets))
			}
			plain := []rune(blockPlainText(doc.Blocks[0]))
			for i, c := range comments {
				if c.Offset != tc.offsets[i] {
					t.Errorf("comment %d Offset = %d, want %d (text is %q)", i, c.Offset, tc.offsets[i], string(plain))
				}
				if c.Offset < 0 || c.Offset > len(plain) {
					t.Fatalf("comment %d Offset = %d is outside %q", i, c.Offset, string(plain))
				}
				if got := string(plain[c.Offset:]); got != tc.anchors[i] {
					t.Errorf("comment %d anchors at %q, want %q (text is %q)", i, got, tc.anchors[i], string(plain))
				}
			}
		})
	}
}

// TestParse_ACommentIDBindsToTheHighlightBeforeIt: a "{>>@comment id<<}" that
// sits flush against a highlight's closing "==}" is that highlight's ID mark.
// It names the comment the words belong to, it is not a comment of its own, so
// nothing is lifted out of the block and the id lands on the highlight.
func TestParse_ACommentIDBindsToTheHighlightBeforeIt(t *testing.T) {
	const id = "cm-0123456789abcdef"
	doc, comments, err := markdown.Parse([]byte("a {==b==}{>>@comment " + id + "<<} c\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("comments = %#v, want none — an ID mark is not a comment", comments)
	}
	if got := blockPlainText(doc.Blocks[0]); got != "a b c" {
		t.Errorf("paragraph text = %q, want %q", got, "a b c")
	}
	var found bool
	for _, in := range doc.Blocks[0].Inlines {
		if in.Text != "b" {
			continue
		}
		found = true
		if got := in.Attr(docmodel.Highlight, docmodel.CommentIDAttr); got != id {
			t.Errorf("highlight id = %q, want %q (inline %#v)", got, id, in)
		}
	}
	if !found {
		t.Fatalf("no inline %q in %#v", "b", doc.Blocks[0].Inlines)
	}
}

// TestParse_PiecesWithOneIDShareOneRun: one comment whose highlight crosses a
// formatting boundary or a paragraph is several pieces in the file, each with
// its own ID mark. The file's ID is the boundary, so every piece gets the same
// run, and the suggestion layer sees ONE comment span.
func TestParse_PiecesWithOneIDShareOneRun(t *testing.T) {
	src := "{==x==}{>>@comment cm-1<<} and **{==y==}{>>@comment cm-1<<}**\n\n" +
		"{==z==}{>>@comment cm-1<<} next\n"
	doc, comments, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("comments = %#v, want none", comments)
	}
	runs := map[string]bool{}
	pieces := 0
	docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
		for _, in := range b.Inlines {
			if !in.Has(docmodel.Highlight) {
				continue
			}
			pieces++
			if got := in.Attr(docmodel.Highlight, docmodel.CommentIDAttr); got != "cm-1" {
				t.Errorf("piece %q id = %q, want cm-1", in.Text, got)
			}
			runs[in.Attr(docmodel.Highlight, docmodel.RunAttr)] = true
		}
	})
	if pieces != 3 {
		t.Fatalf("found %d highlight pieces, want 3", pieces)
	}
	if len(runs) != 1 || runs[""] {
		t.Errorf("runs = %v, want one non-empty run shared by every piece", runs)
	}
	var spans int
	for _, p := range suggest.List(doc) {
		if p.Kind == suggest.KindComment {
			spans++
		}
	}
	if spans != 1 {
		t.Errorf("suggest.List reports %d comment spans, want 1", spans)
	}
}
