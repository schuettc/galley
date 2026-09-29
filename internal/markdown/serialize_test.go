package markdown_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// TestSerialize_RoundTripsFixtures is the golden guarantee: every
// testdata/*.md fixture must survive Parse then Serialize byte-identical.
func TestSerialize_RoundTripsFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			src := mustRead(t, f)
			doc, _, err := markdown.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := markdown.Serialize(doc)
			if string(got) != string(src) {
				t.Errorf("Serialize(Parse(%s)) mismatch:\n got:  %q\n want: %q", f, got, src)
			}
		})
	}
}

func TestSerialize_NestedListIndentation(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind: docmodel.BulletList,
			Children: []docmodel.Block{
				{
					Kind: docmodel.ListItem,
					Children: []docmodel.Block{
						para(text("A")),
						{
							Kind: docmodel.BulletList,
							Children: []docmodel.Block{
								{
									Kind: docmodel.ListItem,
									Children: []docmodel.Block{
										para(text("B")),
										{
											Kind: docmodel.BulletList,
											Children: []docmodel.Block{
												{
													Kind:     docmodel.ListItem,
													Children: []docmodel.Block{para(text("C"))},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}}
	want := "- A\n  - B\n    - C\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_BlockquotePrefixOnEveryLine(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind: docmodel.Blockquote,
			Children: []docmodel.Block{
				para(text("First.")),
				para(text("Second.")),
			},
		},
	}}
	want := "> First.\n>\n> Second.\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_CodeFenceLanguage(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind:  docmodel.CodeBlock,
			Attrs: map[string]string{"language": "python"},
			Text:  "print(1)\n",
		},
	}}
	want := "```python\nprint(1)\n```\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_BoldItalicCombined(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("both", mark(docmodel.Bold), mark(docmodel.Italic))),
	}}
	want := "***both***\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_Link(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("text", linkMark("https://example.com"))),
	}}
	want := "[text](https://example.com)\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_Image(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind:  docmodel.Image,
			Attrs: map[string]string{"src": "https://example.com/i.png", "alt": "Alt text"},
		},
	}}
	want := "![Alt text](https://example.com/i.png)\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_HardBreak(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("Line one"), hardBreak(), text("Line two")),
	}}
	want := "Line one\\\nLine two\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestSerialize_BlankLineBetweenBlocks(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("First.")),
		para(text("Second.")),
	}}
	want := "First.\n\nSecond.\n"
	got := string(markdown.Serialize(doc))
	if got != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

// TestSerialize_EscapeCorpus is the fixed-point guarantee for backslash
// escaping: fix round 1 found that unconditional escaping in Serialize,
// combined with Parse never unescaping, made a backslash accumulate on
// every Serialize(Parse(...)) cycle (Parse("5 * 3 = 15") -> Serialize ->
// "5 \* 3 = 15" -> Parse (backslash retained, uncorrected) -> Serialize ->
// "5 \\* 3 = 15" -> ...). The fix is two-sided: Parse now reverses
// backslash-escapes, and Serialize now escapes only where a character
// would actually be read as markup. Neither side alone is sufficient —
// this test exercises both by going through Parse first (not building a
// docmodel.Doc by hand), so a fixed corpus item never needed escaping in
// the first place is exactly the case that would regress to escaped-then-
// mis-unescaped without both halves of the fix.
func TestSerialize_EscapeCorpus(t *testing.T) {
	corpus := []string{
		"5 * 3 = 15",
		"snake_case_name",
		"a [bracketed] aside",
		`back\slash`,
	}
	for _, src := range corpus {
		t.Run(src, func(t *testing.T) {
			source := []byte(src + "\n")
			doc1, _, err := markdown.Parse(source)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			m1 := markdown.Serialize(doc1)
			doc2, _, err := markdown.Parse(m1)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", m1, err)
			}
			m2 := markdown.Serialize(doc2)
			if !docmodel.Equal(doc1, doc2) {
				t.Errorf("Parse(Serialize(Parse(%q))) not tree-identical to Parse(%q):\n doc1: %#v\n doc2: %#v", src, src, doc1, doc2)
			}
			if string(m1) != string(m2) {
				t.Errorf("Serialize(Parse(Serialize(Parse(%q)))) not a fixed point:\n m1: %q\n m2: %q", src, m1, m2)
			}
		})
	}
}

// assertEscapeAcrossBoundary is the fixed-point check for a docmodel.Doc
// that Parse could never itself produce (its Inlines split at an
// arbitrary offset, the shape Task 7's suggestion transforms create when a
// del/ins boundary lands mid-run) — so it cannot be checked against
// docmodel.Equal(doc, reparsed) the way a Parse-derived fixture can. It
// instead checks Serialize(doc) against `want` byte-for-byte, that
// reparsing produces `normalized` (what the same text looks like once
// Parse has merged adjacent same-mark runs), and that serializing again
// reproduces `want` — the fixed point.
func assertEscapeAcrossBoundary(t *testing.T, doc docmodel.Doc, want string, normalized docmodel.Doc) {
	t.Helper()
	m1 := markdown.Serialize(doc)
	if string(m1) != want {
		t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", m1, want)
	}
	doc2, _, err := markdown.Parse(m1)
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}
	if !docmodel.Equal(doc2, normalized) {
		t.Errorf("Parse(Serialize(doc)) mismatch:\n got:  %#v\n want: %#v", doc2, normalized)
	}
	m2 := markdown.Serialize(doc2)
	if string(m1) != string(m2) {
		t.Errorf("Serialize(Parse(Serialize(doc))) not a fixed point:\n m1: %q\n m2: %q", m1, m2)
	}
}

// TestSerialize_CrossInlineBoundary_LoneAsteriskBeforeBold is fix round
// 2's repro (a): a lone "*" Inline sitting right next to a Bold-marked
// Inline. Escaping decided per-Inline (round 1's design) cannot see that
// Bold is about to contribute its own "**" right after this run, so
// "word " + "*" + Bold("emph") serialized to "word " + "*" + "**emph**" =
// "word ***emph**" — the lone asterisk fuses with Bold's delimiter and is
// lost on reparse. It must escape to "word \***emph**" instead.
func TestSerialize_CrossInlineBoundary_LoneAsteriskBeforeBold(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("word "), text("*"), marked("emph", mark(docmodel.Bold))),
	}}
	normalized := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("word *"), marked("emph", mark(docmodel.Bold))),
	}}
	assertEscapeAcrossBoundary(t, doc, "word \\***emph**\n", normalized)
}

// TestSerialize_CrossInlineBoundary_BracketBeforeLiteralParen is fix round
// 2's repro (b): two adjacent plain-text Inlines whose concatenation
// happens to look like a link. Escaping decided per-Inline sees "[x]" in
// isolation — no "(" follows it within THAT Inline's own text — so it left
// the pair unescaped; concatenated with the next Inline's literal "(y)
// after", "[x](y) after" reparses as a real link, and the literal bracket
// text is gone. Only a plan spanning the whole block can see it, which is
// what this pins.
//
// Round 2 escaped the OPENER, "\[x](y) after". Task 5 escapes the CLOSER
// instead, "[x\](y) after", and the reason is not taste: the opener cannot
// be judged on its own. A link label nests, so whether a "[" is closed by a
// given "]" depends on how many brackets between them are escaped — which
// depends on the same question one level down. FuzzRoundTrip found two
// documents that rode that circle into a corrupted file. "](" is two
// adjacent characters no other rule touches, so escaping the closer settles
// in one pass, and it is sufficient: with no unescaped "](" left in the
// text, the only ones remaining belong to real links. See closesLink.
func TestSerialize_CrossInlineBoundary_BracketBeforeLiteralParen(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("[x]"), text("(y) after")),
	}}
	normalized := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("[x](y) after")),
	}}
	assertEscapeAcrossBoundary(t, doc, "[x\\](y) after\n", normalized)
}

// TestSerialize_NestedList_BulletInOrdered exercises Fix round 1's Critical
// #2: nested-list indent must match the PARENT item's marker width (3 for
// "2. "), not a flat 2 spaces, or the nested list reparses as flattened
// siblings.
func TestSerialize_NestedList_BulletInOrdered(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind: docmodel.OrderedList,
			Children: []docmodel.Block{
				{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("A"))}},
				{
					Kind: docmodel.ListItem,
					Children: []docmodel.Block{
						para(text("B")),
						{
							Kind: docmodel.BulletList,
							Children: []docmodel.Block{
								{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("C"))}},
							},
						},
					},
				},
			},
		},
	}}
	assertListRoundTrip(t, doc, "1. A\n2. B\n   - C\n")
}

// TestSerialize_NestedList_OrderedInOrdered checks marker-width indent when
// both the parent and nested list are ordered.
func TestSerialize_NestedList_OrderedInOrdered(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind: docmodel.OrderedList,
			Children: []docmodel.Block{
				{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("A"))}},
				{
					Kind: docmodel.ListItem,
					Children: []docmodel.Block{
						para(text("B")),
						{
							Kind: docmodel.OrderedList,
							Children: []docmodel.Block{
								{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("C"))}},
							},
						},
					},
				},
			},
		},
	}}
	assertListRoundTrip(t, doc, "1. A\n2. B\n   1. C\n")
}

// TestSerialize_NestedList_OrderedInBullet checks the bullet marker's
// narrower 2-column width is still used correctly as the nested indent.
func TestSerialize_NestedList_OrderedInBullet(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		{
			Kind: docmodel.BulletList,
			Children: []docmodel.Block{
				{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("A"))}},
				{
					Kind: docmodel.ListItem,
					Children: []docmodel.Block{
						para(text("B")),
						{
							Kind: docmodel.OrderedList,
							Children: []docmodel.Block{
								{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("C"))}},
							},
						},
					},
				},
			},
		},
	}}
	assertListRoundTrip(t, doc, "- A\n- B\n  1. C\n")
}

// TestSerialize_NestedList_TenItemOrderedMarkerWidth checks the widest
// marker case the plan calls out: "10. " is 4 columns wide, one wider than
// every single-digit marker before it, so nesting under item 10 needs a
// 4-space indent — a flat or single-digit-derived indent would misplace it.
func TestSerialize_NestedList_TenItemOrderedMarkerWidth(t *testing.T) {
	items := make([]docmodel.Block, 10)
	for i := 0; i < 9; i++ {
		items[i] = docmodel.Block{
			Kind:     docmodel.ListItem,
			Children: []docmodel.Block{para(text("I" + strconv.Itoa(i+1)))},
		}
	}
	items[9] = docmodel.Block{
		Kind: docmodel.ListItem,
		Children: []docmodel.Block{
			para(text("I10")),
			{
				Kind: docmodel.BulletList,
				Children: []docmodel.Block{
					{Kind: docmodel.ListItem, Children: []docmodel.Block{para(text("Nested"))}},
				},
			},
		},
	}
	doc := docmodel.Doc{Blocks: []docmodel.Block{{Kind: docmodel.OrderedList, Children: items}}}
	want := "1. I1\n2. I2\n3. I3\n4. I4\n5. I5\n6. I6\n7. I7\n8. I8\n9. I9\n10. I10\n    - Nested\n"
	assertListRoundTrip(t, doc, want)
}

// assertListRoundTrip serializes doc, checks it against the exact expected
// markdown, then reparses it and checks the reparsed tree matches doc —
// catching a bug in either direction, not just a string match that could
// paper over a symmetric Parse/Serialize error.
func assertListRoundTrip(t *testing.T, doc docmodel.Doc, want string) {
	t.Helper()
	got := markdown.Serialize(doc)
	if string(got) != want {
		t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
	reparsed, _, err := markdown.Parse(got)
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}
	if !docmodel.Equal(doc, reparsed) {
		t.Errorf("round-trip mismatch:\n original: %#v\n reparsed: %#v", doc, reparsed)
	}
}

// TestSerialize_CodeSpan_BacktickInside checks Fix round 1's Important
// finding: a code span whose content contains a single backtick needs a
// wider fence (2 backticks), or the embedded backtick prematurely closes a
// single-backtick fence and the content splits in two.
func TestSerialize_CodeSpan_BacktickInside(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("a`b", mark(docmodel.Code))),
	}}
	got := markdown.Serialize(doc)
	want := "``a`b``\n"
	if string(got) != want {
		t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
	reparsed, _, err := markdown.Parse(got)
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}
	if !docmodel.Equal(doc, reparsed) {
		t.Errorf("round-trip mismatch:\n original: %#v\n reparsed: %#v", doc, reparsed)
	}
}

// TestSerialize_CodeSpan_StartsAndEndsWithBacktick checks the padding case:
// content that itself starts and ends with a backtick needs a padding
// space on each side, or the fence backticks run together with the
// content's own leading/trailing backtick.
func TestSerialize_CodeSpan_StartsAndEndsWithBacktick(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("`code`", mark(docmodel.Code))),
	}}
	got := markdown.Serialize(doc)
	want := "`` `code` ``\n"
	if string(got) != want {
		t.Fatalf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
	reparsed, _, err := markdown.Parse(got)
	if err != nil {
		t.Fatalf("Parse(Serialize(doc)): %v", err)
	}
	if !docmodel.Equal(doc, reparsed) {
		t.Errorf("round-trip mismatch:\n original: %#v\n reparsed: %#v", doc, reparsed)
	}
}

// TestSerialize_AdjacentCodeInlines_FenceMerges is fix round 3's repro:
// two adjacent Code-marked Inlines, each picking its own single-backtick
// fence from its own content alone, fuse on concatenation —
// Code("a")+Code("b") serialized independently as "`a`" next to "`b`",
// fusing their fences together, which reparses as ONE code span whose
// content gained a doubled-up backtick pair in the middle instead of
// reading back as "ab". renderInlines now merges adjacent same-marked Inlines
// (mergeAdjacent, shared with Parse) before planning, so this reduces to
// the already-correct single-Inline case: one merged Code("ab") picks one
// fence for the whole content.
func TestSerialize_AdjacentCodeInlines_FenceMerges(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("a", mark(docmodel.Code)), marked("b", mark(docmodel.Code))),
	}}
	normalized := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("ab", mark(docmodel.Code))),
	}}
	assertEscapeAcrossBoundary(t, doc, "`ab`\n", normalized)
}

// TestSerialize_AdjacentCodeInlines_BacktickInsideOneRun is the same fix,
// with a backtick already inside one of the two adjacent runs: the merged
// content "a`bc" needs a 2-backtick fence (longestBacktickRun sees the
// embedded backtick only once content is merged), not the 1-backtick
// fence either half would have picked alone.
func TestSerialize_AdjacentCodeInlines_BacktickInsideOneRun(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("a`b", mark(docmodel.Code)), marked("c", mark(docmodel.Code))),
	}}
	normalized := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("a`bc", mark(docmodel.Code))),
	}}
	assertEscapeAcrossBoundary(t, doc, "``a`bc``\n", normalized)
}

// TestSerialize_AdjacentCodeInlines_DifferingMarksKeepSeparateFences is fix round
// 4's repro. mergeAdjacent (round 3) only merges Inlines with IDENTICAL
// mark sets, so it never fired here: Code("a") next to Code+Del("b") is
// exactly the shape a suggestion striking half a code span produces, and
// Del emits no delimiter of its own in this phase. Each half therefore
// picked a one-backtick fence from its own text, and the two fences
// landed flush: the serialized text was a one-backtick opener, "a", a
// two-backtick run, "b", a one-backtick closer. That two-backtick run
// closes nothing, so the whole thing reparsed as a SINGLE code span with
// content "a" + two literal backticks + "b" — corruption — and
// re-serializing widened the fence again instead of settling.
//
// Round 4's answer was to fence the two halves together as a single
// Code("ab") and accept losing the Del mark, because Del rendered as
// nothing. Task 4 renders it as CriticMarkup, which changes the answer:
// {--...--} wraps OUTSIDE the code span, so the marker itself now
// separates the two fences and nothing has to be merged or dropped. A
// real suggestion difference (a different kind, or a different span —
// different author/at) is therefore a run boundary, and the fence is
// still chosen over the whole run on each side of it.
//
// That holds only because the markers are actually WRITTEN. Where they
// cannot be — a suggestion whose text contains its own closer — the
// boundary separates nothing and round 4's fusion comes straight back;
// planInlines coalesces those segments instead, which
// TestSerialize_UnspellableSuggestion_CoalescesWithNeighbour pins.
//
// The canonical answer: "`a`{--`b`--}" — each side keeping its OWN fence,
// with the deletion's markers between them holding the two apart. Reparses
// to exactly the original two Inlines. Losslessly, this time.
func TestSerialize_AdjacentCodeInlines_DifferingMarksKeepSeparateFences(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("a", mark(docmodel.Code)),
			marked("b", mark(docmodel.Code), mark(docmodel.Del)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "`a`{--`b`--}\n", doc)
}

// TestSerialize_AdjacentCodeInlines_DifferingMarksSizeTheirOwnFences is the
// same fix where the two halves need DIFFERENT fence widths: Code+Ins("x")
// next to Code("y`z"). (Both names once said "share"/"widen" a fence, which
// described the pre-Task-4 behaviour these tests no longer assert — each
// side now writes its own fence.) Before the run-level
// fence, this serialized to a mess whose reparse split into a code span
// with content "x" + three backticks + "y" followed by literal text
// "z" + two backticks — content corrupted in both halves, and still not a
// fixed point.
//
// Task 4's markers again make the merge unnecessary: each side keeps its
// own fence, sized from its own content ("x" takes one backtick, "y`z"
// takes two), and the "{++"/"++}" between them is what keeps those fences
// from fusing. Round-trips losslessly.
func TestSerialize_AdjacentCodeInlines_DifferingMarksSizeTheirOwnFences(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("x", mark(docmodel.Code), mark(docmodel.Ins)),
			marked("y`z", mark(docmodel.Code)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "{++`x`++}``y`z``\n", doc)
}

// TestSerialize_AdjacentCodeInlines_DifferingWrapperKeepsBothSpans guards
// the other side of round 4's grouping rule: a run must NOT extend across
// a change in emphasis or link marks, because those emit delimiters that
// both keep the two fences apart and would be dropped by merging.
// Code("a") next to Bold+Code("b") stays two code spans with a bold
// wrapper on the second, and round-trips exactly — no normalization at
// all, so docmodel.Equal against the original holds.
func TestSerialize_AdjacentCodeInlines_DifferingWrapperKeepsBothSpans(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("a", mark(docmodel.Code)),
			marked("b", mark(docmodel.Bold), mark(docmodel.Code)),
		),
	}}
	assertEscapeAcrossBoundary(t, doc, "`a`**`b`**\n", doc)
}

func TestSerialize_ExactlyOneTrailingNewline(t *testing.T) {
	doc := docmodel.Doc{Blocks: []docmodel.Block{
		para(text("Hello")),
	}}
	got := markdown.Serialize(doc)
	want := "Hello\n"
	if string(got) != want {
		t.Errorf("Serialize mismatch:\n got:  %q\n want: %q", got, want)
	}
}

// TestSerialize_EscapesLineLeadingBlockMarkers pins the escaping rule that
// closed the biggest pre-existing round-trip gap Task 4's probe found: a
// character that is only ordinary text INSIDE a line can be the whole
// meaning of the line when it leads one. Parse("\\-\n") gives a paragraph
// whose text is "-"; writing that back bare gives "-\n", which the next
// Parse reads as a BULLET LIST. The paragraph is gone and the document has
// silently changed shape.
//
// Escaping cannot be decided from the character alone, so this table is
// organized around the distinction that matters: the same character is
// escaped where it would open block structure and left bare where it would
// not. "####### x" is seven hashes, one past ATX's limit of six, so it is
// already just text; "1234567890." is ten digits, one past the ordered
// list's limit of nine. Escaping those would be noise, and noise in a
// serializer is a diff in somebody's file.
//
// Every case asserts BYTE IDENTITY through the round trip, which is the
// strong form: the source is already the canonical spelling, so a case
// that needs an escape and a case that must not have one both fail loudly
// if the rule moves in either direction.
func TestSerialize_EscapesLineLeadingBlockMarkers(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"bullet dash", "\\- x\n"},
		{"bullet plus", "\\+ x\n"},
		{"bullet star", "\\* x\n"},
		{"empty bullet is still a bullet", "\\-\n"},
		{"blockquote", "\\> x\n"},
		{"atx heading", "\\# x\n"},
		{"atx heading at the six-hash limit", "\\###### x\n"},
		{"ordered list dot", "1\\. x\n"},
		{"ordered list paren", "1\\) x\n"},
		{"ordered list at the nine-digit limit", "123456789\\. x\n"},
		{"thematic break of dashes", "\\---\n"},
		{"thematic break of stars", "\\*\\*\\*\n"},
		{"thematic break of underscores", "\\_\\_\\_\n"},
		{"spaced thematic break of dashes", "\\- - -\n"},
		{"spaced thematic break of stars", "\\* * *\n"},
		{"spaced thematic break of underscores", "\\_ _ _\n"},
		{"setext underline of equals", "\\===\n"},
		{"setext underline of two dashes", "\\--\n"},
		{"tilde code fence", "\\~~~\n"},
		{"marker after a hard break", "a\\\n\\- b\n"},
		{"marker inside a list item", "- \\- x\n"},
		{"marker inside a blockquote", "> \\- x\n"},
		{"marker after emphasis on the same line", "*a* - x\n"},
		{"seven hashes are not a heading", "####### x\n"},
		{"ten digits are not an ordered list", "1234567890. x\n"},
		{"a hash without a space is not a heading", "#x\n"},
		{"a dash without a space is not a bullet", "-x\n"},
		{"a dot mid-line is not a list marker", "a 1. b\n"},
		{"a heading's content is not block structure", "# - x\n"},
		{"a heading's content is not a list marker", "# 1. x\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			// The escape is only worth anything if it survives: reparse
			// and check the tree is the same one, not merely that the
			// bytes stopped changing.
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the tree:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_AdjacentSameKindLists_StayDistinct is FuzzRoundTrip's first
// crasher, minimized from "0.\n0)": two sibling lists of the same kind
// merged into ONE list on reparse, and the second list's items were
// renumbered into the first's.
//
// Markdown separates two adjacent lists by their MARKER, not by the blank
// line: "- a" then "- b" is one list however much whitespace sits between
// them, and it is a change of bullet character ("-" to "*") or of ordered
// delimiter ("." to ")") that starts a new one. Parse drops that
// distinction — both spellings are a BulletList — so Serialize has to
// reintroduce it, which it can only do by looking at a block's SIBLINGS.
//
// The document model draws a distinction here that markdown draws too, so
// this is a shape change, not a normalization: a reader who splits one list
// in two must not have it silently rejoined the next time the file is
// written.
func TestSerialize_AdjacentSameKindLists_StayDistinct(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"two bullet lists", "- a\n\n* b\n"},
		{"three bullet lists alternate back", "- a\n\n* b\n\n- c\n"},
		{"two ordered lists", "1. a\n\n1) b\n"},
		{"three ordered lists alternate back", "1. a\n\n1) b\n\n1. c\n"},
		{"the fuzz crasher, both items empty", "1.\n\n1)\n"},
		{"different kinds need no alternation", "- a\n\n1. b\n"},
		{"a paragraph between them resets the marker", "- a\n\nx\n\n- b\n"},
		{"nested sibling lists inside one item", "- a\n  - b\n  * c\n"},
		{"sibling lists inside a blockquote", "> - a\n>\n> * b\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip merged or split a list:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_AdjacentEmphasisDelimiters_FuseAndLoseTheEmphasis pins the
// known class FuzzRoundTrip found second. It is pre-existing and
// deliberately NOT fixed here — Task 5's brief names it as out of scope —
// so this test's job is to say exactly where the edge is, and to fail if it
// ever moves.
//
// The segment invariant (see planInlines) guarantees that two adjacent
// segments are separated by at least one delimiter CHARACTER. It does not
// guarantee they are separated by a delimiter BOUNDARY, and emphasis is
// where the difference bites: Italic("a ") plans "*a *" and Italic+Bold("b")
// plans "***b***", so the closing "*" of the first lands flush against the
// opening "***" of the second and CommonMark sees one run of four. A
// four-star run is not two delimiters, it is one delimiter of a length that
// means something else.
//
// What is lost is the MARK, not the text: by the second write the outer
// italic has degraded into escaped literal stars, and every letter is still
// there. That is the same trade the rest of this package makes wherever a
// mark has no spelling — losing a mark is recoverable from the sidecar,
// losing the author's words is not — and it is why FuzzRoundTrip's
// alphanumeric property does not fire here. The bytes settle on the third
// serialization, within the budget the fuzz target allows every input.
//
// The real fix is for planInlines to separate two emphasis runs the way it
// separates two code fences — by choosing delimiters that cannot fuse — and
// when that lands, the emphasis survives round 1 and these rounds collapse
// to one.
func TestSerialize_AdjacentEmphasisDelimiters_FuseAndLoseTheEmphasis(t *testing.T) {
	rounds := []string{
		// "*a *" and "***b***" written flush: one run of four stars.
		"*a ****b***c\\*\n",
		// The outer italic is gone, spelled as literal stars instead.
		"\\*a \\****b***c\\*\n",
		"\\*a \\****b***c\\*\n",
	}
	cur := "*a **b***c*"
	for i, want := range rounds {
		doc, _, err := markdown.Parse([]byte(cur))
		if err != nil {
			t.Fatalf("round %d: Parse(%q): %v", i, cur, err)
		}
		got := string(markdown.Serialize(doc))
		if got != want {
			t.Fatalf("round %d: Serialize(Parse(%q)) = %q, want %q\n"+
				"if the emphasis now survives, the delimiter-fusion class is fixed", i, cur, got, want)
		}
		cur = got
	}
	// The text is what may not be lost, and none of it is.
	if !strings.Contains(cur, "a ") || !strings.Contains(cur, "b") || !strings.Contains(cur, "c") {
		t.Errorf("the fusion ate text, not just marks: %q", cur)
	}
}

// TestSerialize_FusedEmphasis_CostsARoundEach is the measurement that sets
// FuzzRoundTrip's round budget, and the reason that budget is a FUNCTION of
// the input rather than a constant.
//
// The renderer escapes its way out of one fused delimiter run per
// serialization, so a document with more of them takes proportionally
// longer to settle: the counts below grow by exactly one per repetition.
// The round trip converges — it is not a runaway — but no fixed number of
// serializations bounds it, which is why FuzzRoundTrip counts the fused
// runs in the first write and allows one round for each.
//
// If the fusion class is ever fixed, every count here becomes 1 and the
// fuzz target's budget can go back to being a constant.
func TestSerialize_FusedEmphasis_CostsARoundEach(t *testing.T) {
	for repeats, want := range []int{3, 4, 5, 6, 7} {
		t.Run(strconv.Itoa(repeats), func(t *testing.T) {
			src := "*a **b***" + strings.Repeat(" ***c** **d***", repeats)
			cur, got := src, 0
			for got < 20 {
				doc, _, err := markdown.Parse([]byte(cur))
				if err != nil {
					t.Fatalf("round %d: Parse(%q): %v", got, cur, err)
				}
				next := string(markdown.Serialize(doc))
				got++
				if next == cur {
					break
				}
				cur = next
			}
			if got != want {
				t.Errorf("%d fused repetition(s) settled in %d serializations, want %d\n"+
					"a LOWER number means the fusion class improved and FuzzRoundTrip's "+
					"per-run budget can shrink; a higher one means it got worse", repeats, got, want)
			}
		})
	}
}

// TestSerialize_ListItemBlocks_SeparatedWhereMarkdownNeedsIt is
// FuzzRoundTrip's fourth crasher, minimized from "* 0\n\n\t![]()". A list
// item's blocks were joined with a single newline — that is what makes a
// tight list tight — but a single newline after a paragraph line is a LAZY
// CONTINUATION, so the next block was read as more of the same paragraph.
//
// The damage depends on what followed. A second paragraph was silently
// glued onto the first, which is quiet corruption of the author's text. An
// image was glued on too, and "![](x)" inside a paragraph with other text
// is a construct Parse refuses — so the file stopped loading altogether,
// which is how the fuzzer found it.
//
// The separation markdown needs is not a property of either block alone, it
// is a property of the PAIR: a blank line is required exactly when the
// previous block ends in an open paragraph and the next one cannot
// interrupt a paragraph. Nested lists, headings, blockquotes and fenced
// code all interrupt, so the common tight-list shapes stay tight and the
// fixtures are untouched.
//
// The subtle member of the "cannot interrupt" set is Rule: "---" directly
// under a paragraph line is not a thematic break at all, it is a SETEXT
// HEADING underline, and it would have turned the paragraph above it into
// an h2.
func TestSerialize_ListItemBlocks_SeparatedWhereMarkdownNeedsIt(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"two paragraphs need the blank line", "- a\n\n  b\n"},
		{"the fuzz crasher: an image after a paragraph", "- 0\n\n  ![](x)\n"},
		{"a rule after a paragraph would underline it", "- a\n\n  ---\n"},
		{"a paragraph after a nested list", "- a\n  - b\n\n  c\n"},
		{"a nested list interrupts, so it stays tight", "- a\n  - b\n"},
		{"a heading interrupts", "- a\n  # h\n"},
		{"a blockquote interrupts", "- a\n  > q\n"},
		{"sibling items are still tight", "- a\n- b\n"},
		{"every item split, and the list stays one list", "- a\n\n  b\n- c\n\n  d\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip merged the item's blocks:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_NULIsNotALineBoundary is FuzzRoundTrip's fifth crasher, and
// the only one that never settled at all: "*\x00 **0***\x00*" alternated
// between two spellings forever, so every save of the file was a diff and
// every second save undid the last.
//
// The cause was a sentinel collision. escape.go's flanking rule needs to
// treat the start and end of the run as whitespace, and it did that by
// returning a sentinel rune for an out-of-range index — rune 0. Rune 0 is
// NUL, which is a character text can actually contain, so a "*" sitting
// next to a NUL was judged as if it sat at the start of the line: not
// flanking, no escape needed. It was flanking, and the "*" came back as
// emphasis.
//
// The fix is to ask about the POSITION rather than to invent a rune for
// "off the end", because there is no rune that is safe to invent.
//
// The two cases are the same bug from both ends: the minimal shape, where
// NULs on either side are the only reason a "*" looks unflanked, and the
// crasher itself, where the misjudged escape is what kept the round trip
// oscillating.
func TestSerialize_NULIsNotALineBoundary(t *testing.T) {
	t.Run("a star between NULs is still flanking", func(t *testing.T) {
		doc := docmodel.Doc{Blocks: []docmodel.Block{para(text("\x00*\x00*\x00"))}}
		got := markdown.Serialize(doc)
		want := "\x00\\*\x00\\*\x00\n"
		if string(got) != want {
			t.Fatalf("Serialize = %q, want %q", got, want)
		}
		again, _, err := markdown.Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q): %v", got, err)
		}
		if !docmodel.Equal(doc, again) {
			t.Errorf("the stars came back as emphasis:\n got:  %#v\n want: %#v", again, doc)
		}
	})
	t.Run("the crasher settles instead of oscillating", func(t *testing.T) {
		cur := "*\x00 **0***\x00*"
		var prev string
		for i := 0; i < 6; i++ {
			doc, _, err := markdown.Parse([]byte(cur))
			if err != nil {
				t.Fatalf("round %d: Parse(%q): %v", i, cur, err)
			}
			prev, cur = cur, string(markdown.Serialize(doc))
			if cur == prev {
				return
			}
		}
		t.Errorf("still moving after 6 serializations: %q then %q", prev, cur)
	})
}

// TestSerialize_CodeBlockFence is FuzzRoundTrip's sixth crasher, minimized
// from "~~~`0", and it is two bugs that share a cause: the code-block fence
// was a constant. "```" was written whatever the block contained and
// whatever its info string was.
//
// Content first, because it is the one a real document hits. A code block
// that contains a "```" line — a markdown file documenting markdown, a
// README with a nested example — was written inside a fence its own content
// closes. The block split in two and the rest of the content became
// document text. That is silent, total loss of a code block.
//
// Then the info string. CommonMark forbids a backtick ANYWHERE in a
// backtick fence's info string, and "~~~`0" is a tilde-fenced block whose
// info string is "`0". Written as "````0" that is a four-backtick fence
// with the info string "0", which the three-backtick closer below it cannot
// close — so every round appended another orphaned fence and the file grew
// without bound. A tilde fence has no such rule, so that is what an info
// string with a backtick in it gets.
//
// The width rule is CommonMark's closing rule exactly, not an
// approximation: only a line holding nothing but the fence character, after
// at most three spaces of indent, closes a block. So a "```" in the middle
// of a line does not widen the fence, and neither does one indented four
// spaces — widening for those would be a diff in the file for no reader's
// benefit.
func TestSerialize_CodeBlockFence(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"content holding a fence widens the opener", "````\n```\n````\n"},
		{"content holding a wider fence widens further", "`````\n````\n`````\n"},
		{"a fence indented three spaces still closes", "````\n   ```\n````\n"},
		{"a fence indented four spaces does not", "```\n    ```\n```\n"},
		{"a fence mid-line does not close", "```\na ``` b\n```\n"},
		// FuzzRoundTrip's thirty-third crasher: a lone carriage return is a
		// line ending to goldmark's scanner, so "```\r" closes a fence
		// exactly as "```" does and has to widen the opener the same way.
		{"a fence followed by a carriage return still closes", "````\n```\r\n````\n"},
		{"the fuzz crasher: a backtick in the info string", "~~~`0\n~~~\n"},
		{"a tilde fence widens for tildes, not backticks", "~~~~`0\n~~~\n~~~~\n"},
		{"an info string starting with the fence char is spaced off", "~~~ ~`\n~~~\n"},
		{"the same, with a tab inside the info string", "~~~ ~\t~`\n~~~\n"},
		{"an ordinary block is untouched", "```go\nx\n```\n"},
		{"an empty block is untouched", "```\n```\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("the code block did not survive:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
	t.Run("content with no trailing newline gets one", func(t *testing.T) {
		// Parse always ends a code block's text with a newline; a
		// hand-built doc need not, and without one the closing fence would
		// land on the end of the last content line.
		doc := docmodel.Doc{Blocks: []docmodel.Block{{Kind: docmodel.CodeBlock, Text: "x"}}}
		got := markdown.Serialize(doc)
		want := "```\nx\n```\n"
		if string(got) != want {
			t.Fatalf("Serialize = %q, want %q", got, want)
		}
	})
}

// TestSerialize_EscapesHeadingClosingSequence is FuzzRoundTrip's third
// crasher, minimized from "# # # #", and its eighth, minimized from
// "# 0\r#\r# #". A trailing run of "#" on an ATX heading line is a CLOSING
// SEQUENCE, not content: markdown strips it. So a heading whose text ends
// in "#" was written back with that "#" bare, the next Parse ate it, and
// the heading got one character shorter on every save — "# # # #" erodes
// through "# # #", "# #", "# " to nothing.
//
// Slow erosion is the worst shape a round-trip bug can take. Nothing looks
// wrong after any single save; the damage is only visible against a version
// of the file from several saves ago, by which time there is nothing left
// to recover the text from.
//
// The rule is CommonMark's own: a closing sequence must be preceded by
// whitespace (or be the whole content), and may be followed only by
// whitespace. So "a#" needs no escape — nothing separates the "#" from the
// word, and it is already content — while "a #" does.
//
// Which characters count as that whitespace is MEASURED against goldmark,
// not assumed, and the eighth crasher is why. A lone carriage return is a
// line ending to goldmark's scanner, so "# a\r#" closes and erodes exactly
// like "# a #", while a vertical tab or form feed does not close and must
// not be escaped. The same asymmetry applies to the opener: "#\rx" is a
// heading, so a "#" before a carriage return needs the escape too, but
// "-\rx" is not a bullet and does not.
func TestSerialize_EscapesHeadingClosingSequence(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"a lone hash is the whole heading", "# \\#\n"},
		{"the fuzz crasher", "# # \\#\n"},
		{"a hash run at the end", "# a \\##\n"},
		{"deeper heading level", "### a \\#\n"},
		{"a carriage return also lets the run close", "# a\r\\#\n"},
		{"a hash joined to a word is content", "# a#\n"},
		{"a hash mid-heading is content", "# a # b\n"},
		{"a hash run that starts the heading is content", "# ### x\n"},
		{"a vertical tab does not let the run close", "# a\v#\n"},
		{"a form feed does not either", "# a\f#\n"},
		{"a paragraph's trailing hash needs nothing", "a #\n"},
		{"a hash before a carriage return opens a heading", "\\#\rx\n"},
		{"a hash before a vertical tab does not", "#\vx\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the heading:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_EscapesAngleMarkup is FuzzRoundTrip's ninth crasher,
// minimized from "\\<0@0>". Parse gives the text "<0@0>", Serialize wrote it
// bare, and the next Parse read an AUTOLINK — a construct this package
// refuses rather than mangles, so the file stopped loading.
//
// That is the same family as the marker-removal splice: "x}<{----}b>x"
// serializes to "x}<b>x", which Parse rejects as raw HTML. It was the most
// urgent of Task 4's known classes because it is the only one that fails
// loudly, and the answer was always that the complete fix belongs in the
// escape layer. This is it — one rule closes both, so
// TestSerialize_MarkerRemovalSplicesTextSafely is now a round-trip
// assertion rather than an assertion that the file breaks.
//
// The rule has two halves because "<" opens two different kinds of thing. A
// tag or a declaration is "<" followed by a letter, "/", "!" or "?", and it
// may contain spaces ("<a href=\"x\">"). An autolink may not contain a
// space or a "<" at all, but it may start with anything — "<0@0>" starts
// with a digit, which is why the first half alone missed it.
//
// The autolink half needs one more test than "something before a >": an
// autolink is either a URI, which needs a scheme and so a ":", or an email,
// which needs an "@". Without that, a nested CriticMarkup soup — which
// produces a great many "<...>" spans that mean nothing — came out covered
// in backslashes it did not need.
//
// The union still over-escapes: "<y" with no closing ">" is not markup, and
// gets a backslash it does not need. That is the deliberate direction to
// err in.
// An unnecessary "\\<" reparses to exactly "<" and is stable, while a
// missing one is a file that will not open — and text like "<y" in prose is
// nearly always meant literally anyway. What is NOT over-escaped is the
// shape that actually shows up in writing: a "<" used as less-than, with a
// space after it, is left alone.
func TestSerialize_EscapesAngleMarkup(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"the fuzz crasher: an email autolink", "\\<0@0>\n"},
		{"a URI autolink", "\\<https://example.com>\n"},
		{"an open tag", "\\<b>x\n"},
		{"a closing tag", "x\\</b>\n"},
		{"a tag with attributes and spaces", "\\<a href=\"x\">y\n"},
		{"an HTML comment", "\\<!-- c -->\n"},
		{"a processing instruction", "\\<?php ?>\n"},
		{"less-than in prose is left alone", "5 < 6 and 7 > 8\n"},
		{"an empty pair is not markup", "a <> b\n"},
		{"angles with no scheme or address are not an autolink", "a <}> b\n"},
		{"less-than at the end of a line", "a <\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the text:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_LinkAndImageAttributes is FuzzRoundTrip's tenth crasher,
// minimized from "![\\\\]()". An image was written as "![" + alt + "](" +
// src + ")" with both attributes pasted in RAW, which is the one place this
// package wrote text without asking what markdown would make of it.
//
// Alt text is inline text — Parse reads it as such, and REJECTS an image
// whose alt holds anything but plain text — so a "*" in it comes back as
// emphasis and the image stops parsing, and a backslash erodes one round at
// a time. It also sits inside square brackets, where "[" and "]" close the
// label rather than being text, so those need escaping even though they are
// harmless in the open.
//
// A destination has different rules again, and the bare form is a trap: it
// ends at the first space and its parentheses must balance. A destination
// holding either was written straight into "](...)" and the link simply
// stopped being a link. The angle form has neither rule, so it is used
// wherever the bare form is ambiguous — and only there, so ordinary URLs
// still read like markdown people write by hand.
//
// The link case is the same bug at a different site: a link's LABEL is
// planned inline with the rest of the block, so the bracket escaping rides
// on the planned runes (pchar.label) rather than on a mode for the whole
// plan.
func TestSerialize_LinkAndImageAttributes(t *testing.T) {
	roundTrips := []struct {
		name string
		src  string
	}{
		{"a star in alt text would be emphasis", "![a\\*b\\*c](x)\n"},
		{"a bracket in alt text would close the label", "![a\\]b\\[c](x)\n"},
		{"a backtick in alt text would be a code span", "![a\\`b](x)\n"},
		{"a bracket in a link label", "[a\\]b](x)\n"},
		{"a destination holding a space", "[a](<b c>)\n"},
		{"an ordinary URL stays bare", "[a](https://example.com)\n"},
		{"an empty destination stays empty", "[a]()\n"},
	}
	for _, tc := range roundTrips {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the link:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}

	// The attribute values a hand-built doc can hold but no markdown source
	// spells directly — a bare backslash, a destination with parentheses.
	built := []struct {
		name     string
		alt, src string
		want     string
	}{
		{"a backslash in alt text", `a\b`, "x", "![a\\b](x)\n"},
		{"brackets in alt text", "a]b[c", "x", "![a\\]b\\[c](x)\n"},
		{"a destination with balanced parens", "a", "b(c)", "![a](<b(c)>)\n"},
		{"a destination with an angle bracket", "a", "b<c", "![a](<b\\<c>)\n"},
		// FuzzRoundTrip's seventeenth crasher, from "![\\\\](]())": alt text
		// ending in a backslash escaped the label's own "]" instead of
		// itself, so the label never closed.
		{"alt text ending in a backslash", "\\", "x", "![\\\\](x)\n"},
		{"the crasher, with its destination too", "\\", "]()", "![\\\\](<]()>)\n"},
	}
	for _, tc := range built {
		t.Run(tc.name, func(t *testing.T) {
			doc := docmodel.Doc{Blocks: []docmodel.Block{{
				Kind:  docmodel.Image,
				Attrs: map[string]string{"alt": tc.alt, "src": tc.src},
			}}}
			got := markdown.Serialize(doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("attributes did not survive:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_EscapesBangBeforeALink is FuzzRoundTrip's eleventh crasher,
// minimized from "!**[0]()**": a paragraph holding the text "!" followed by
// a bold link. Written back, the "!" landed against the link's own "["
// delimiter and the pair became "![" — an IMAGE.
//
// That is not a cosmetic difference. An image's label is not inline
// content: Parse rejects an image whose alt text holds anything but plain
// text, so a bold link turned into an image whose alt is "**0**" is a
// document that no longer loads.
//
// The escape belongs on the "!" only when the "[" after it is a delimiter
// this renderer emitted. Where the "[" is the author's own text it takes
// the escape itself, and writing both would put two backslashes where one
// already does the job — which is why escapeCtx carries the plan's
// escapable flags.
func TestSerialize_EscapesBangBeforeALink(t *testing.T) {
	t.Run("a bang before an emitted link delimiter", func(t *testing.T) {
		// The crasher's own shape, spelled as the source it settles on: a
		// literal "!" followed by a bold link, which is exactly the pair
		// the escape has to keep apart.
		src := "\\![**0**]()\n"
		doc, _, err := markdown.Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		if n := len(doc.Blocks[0].Inlines); n != 2 {
			t.Fatalf("Parse(%q) gave %d inlines, want a text run and a linked one", src, n)
		}
		got := markdown.Serialize(doc)
		if string(got) != src {
			t.Fatalf("Serialize(Parse(%q)) = %q, want %q", src, got, src)
		}
		again, _, err := markdown.Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q) [round 2]: %v", got, err)
		}
		if !docmodel.Equal(doc, again) {
			t.Errorf("the link became an image:\n got:  %#v\n want: %#v", again, doc)
		}
	})
	t.Run("a bang before a link whose label holds brackets", func(t *testing.T) {
		// FuzzRoundTrip's sixteenth crasher, from "!*[[]]()*". The first
		// attempt at this rule asked a forward scan whether the "[" after
		// the "!" opened a link, and that scan stopped at the first "]" —
		// which here is a content bracket the same pass was about to
		// escape. It answered "not a link" for a link, and the "!" went
		// unescaped. A delimiter "[" needs no such question: this renderer
		// only ever writes one as a link opener.
		src := "\\![*\\[\\]*]()\n"
		doc, _, err := markdown.Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		got := markdown.Serialize(doc)
		if string(got) != src {
			t.Fatalf("Serialize(Parse(%q)) = %q, want %q", src, got, src)
		}
		again, _, err := markdown.Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q) [round 2]: %v", got, err)
		}
		if !docmodel.Equal(doc, again) {
			t.Errorf("the link became an image:\n got:  %#v\n want: %#v", again, doc)
		}
	})
	t.Run("a bang before the author's own brackets needs only one escape", func(t *testing.T) {
		// The "[" here is the author's text, not a delimiter, so the "!"
		// takes no escape — the pair is broken by escaping the "](" that
		// would have closed the image (closesLink).
		doc := docmodel.Doc{Blocks: []docmodel.Block{para(text("![a](b)"))}}
		got := markdown.Serialize(doc)
		want := "![a\\](b)\n"
		if string(got) != want {
			t.Fatalf("Serialize = %q, want %q", got, want)
		}
		again, _, err := markdown.Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q): %v", got, err)
		}
		if !docmodel.Equal(doc, again) {
			t.Errorf("round trip changed the text:\n got:  %#v\n want: %#v", again, doc)
		}
	})
}

// TestSerialize_EscapesLinkReferenceDefinition is FuzzRoundTrip's twelfth
// crasher, minimized from "[0]:0 \f". The trailing form feed is what keeps
// goldmark from reading the source itself as a link reference definition,
// so Parse gives an ordinary paragraph whose text is "[0]:0" — and writing
// that back bare produced a definition after all. Parse refuses those
// rather than mangling them, so the file stopped loading.
//
// It is the block-structure family again, one construct further out: a link
// reference definition is a BLOCK, and blocks are decided from the start of
// a line. The signature is the "]:" that no inline construct has — an
// inline link is "](", and a bracketed label with no colon after it is just
// text.
//
// The escape goes on the CLOSER, and the three cases below marked with
// crasher numbers are why. Escaping the opening "[" needs three questions
// answered at once — does the label reach past this line, which brackets in
// between are already escaped, and is this "[" the one that opens — and the
// third is circular, because escaping one "[" makes a LATER "[" into a
// valid opener. From the closer it is one question: walk back to the
// nearest bracket the renderer did not neutralize and ask whether it leads
// a line. "a [b]: c" does not, so it keeps its bytes; "[a[b]]: c" does not
// either, and markdown agrees it is no definition.
//
// The one case the closer cannot take is a "]:" inside a CODE SPAN, which
// markdown offers no way to escape. There the opening "[" takes it instead
// (opensUnescapableReference) — the two rules do not argue, because both
// only ever add a backslash and neither reads the other's output.
func TestSerialize_EscapesLinkReferenceDefinition(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"the fuzz crasher", "[0\\]:0\n"},
		// The twenty-first crasher, "[](]:0": the bracket the scan had to
		// walk past is one closesLink escapes, so it closes nothing.
		{"a bracket the closer-rule escapes does not stop the scan", "[\\](\\]:0\n"},
		// The twenty-fourth, "[  \n]:0": a definition's label may span
		// LINES, so the opener leading the block is still the opener.
		{"a label spanning a hard break", "[\\\n\\]:0\n"},
		// The twenty-fifth, "[\\\n[]:0": escaping the first "[" used to turn
		// the SECOND one into a valid opener — one rule changing its own
		// answer. From the closer there is no such loop.
		{"a second opener on the next line", "[\\\n[\\]:0\n"},
		// FuzzRoundTrip's thirtieth crasher, "[](`]:`": the "]:" is inside a
		// CODE SPAN, which markdown gives no way to escape — and block
		// structure is decided before any inline parsing, so the scanner
		// never learns it was meant to be code. Where the closer cannot
		// take the escape, the opener has to.
		{"a closer inside a code span, which cannot be escaped", "\\[\\](`]:`\n"},
		// And the thirty-sixth, "[\\\n[`]:`": escaping the second opener
		// would have made the FIRST one valid, so both take a backslash.
		{"two openers, an unescapable closer", "\\[\\\n\\[`]:`\n"},
		{"a colon with no destination is still escaped", "[a\\]:\n"},
		{"inside a list item", "- [a\\]: b\n"},
		{"inside a blockquote", "> [a\\]: b\n"},
		{"mid-line is not a definition", "a [b]: c\n"},
		{"a nested bracket disqualifies the label", "[a[b]]: c\n"},
		{"an inline link is untouched", "[a](b)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the text:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_LinkSpanningEmphasis_BecomesSeveralLinks pins the class
// FuzzRoundTrip found thirteenth, minimized from "[**0*0* 0**0](0)". A link
// whose content changes emphasis part-way through is written as SEVERAL
// adjacent links, one per emphasis run, repeating the destination each
// time: "[**a** b](x)" comes back as "[**a**](x)[ b](x)".
//
// It is NOT fixed here, and the reason is worth stating precisely, because
// it is not laziness. The document model has no link IDENTITY — a Link mark
// carries a destination and nothing else — so "one link over two emphasis
// runs" and "two adjacent links with the same destination" are the same
// value. docmodel.Equal says so, which this test asserts: the round trip
// loses nothing the model can express, and it is a byte fixed point on the
// first write.
//
// Making the serializer emit one link would mean planning the link OUTSIDE
// the emphasis segmentation, and that collides head-on with Task 4's
// measured rule that CriticMarkup markers go outside the link delimiters: a
// suggestion covering half a link cannot have its markers both outside the
// link and inside one set of brackets. That is a design decision about the
// model and the segmentation together, not a bug fix, so it belongs to
// whoever revisits either.
//
// What it costs today: an HTML renderer emits two <a> elements where the
// author wrote one. What it costs FuzzRoundTrip is that the serialized
// bytes repeat the destination once per run, so the alphanumeric property
// cannot compare them — see splitsALink there.
func TestSerialize_LinkSpanningEmphasis_BecomesSeveralLinks(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"bold inside a link splits it", "[**a** b](x)\n", "[**a**](x)[ b](x)\n"},
		{"a code span does not split it", "[a `b` c](x)\n", "[a `b` c](x)\n"},
		{"emphasis over the whole link does not split it", "[*a*](x)\n", "[*a*](x)\n"},
		{"two links with one destination become one", "[a](x)[b](x)\n", "[ab](x)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q\n"+
					"if a split link now stays one link, the class is fixed and "+
					"FuzzRoundTrip's splitsALink tolerance can go", tc.src, got, tc.want)
			}
			// The split is invisible to the model, which is the whole
			// reason it is tolerable: nothing the document can express is
			// lost, and the bytes settle immediately.
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("the split lost something the model CAN express:\n got:  %#v\n want: %#v", again, doc)
			}
			if m2 := markdown.Serialize(again); string(m2) != string(got) {
				t.Errorf("not a fixed point: %q then %q", got, m2)
			}
		})
	}
}

// TestSerialize_EscapesBracketBeforeALinkDelimiter is FuzzRoundTrip's
// eighteenth crasher, minimized from "![[\\]]()\\]()": a paragraph whose
// text run ends "![" sitting immediately before a link's own "["
// delimiter. Written bare, the "![" opened an IMAGE whose label swallowed
// the link — and an image's alt text may only be plain text, so Parse
// refused the result and the file stopped loading.
//
// The escape goes on the "](" at the END — the trailing literal text that
// would have closed the image — not on the "[" at the start. Judging the
// opener means knowing which of the brackets between it and a candidate
// closer are themselves escaped, and that is circular; judging the closer
// is two adjacent characters and settles in one pass. See closesLink.
//
// The general shape of this and the sixteenth crasher is the same: whether
// one character needs a backslash can depend on whether another one is
// going to get one. Re-deriving that from the runes is what went wrong
// twice; asking the plan what it emitted (escapeCtx's escapable and labels)
// is what fixes the parts that remain.
func TestSerialize_EscapesBracketBeforeALinkDelimiter(t *testing.T) {
	src := "![[\\]]()\\]()\n"
	doc, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	if doc.Blocks[0].Kind != docmodel.Paragraph {
		t.Fatalf("Parse(%q) gave a %s, want a paragraph holding a link", src, doc.Blocks[0].Kind)
	}
	got := markdown.Serialize(doc)
	if string(got) != src {
		t.Fatalf("Serialize(Parse(%q)) = %q, want %q", src, got, src)
	}
	again, _, err := markdown.Parse(got)
	if err != nil {
		t.Fatalf("Parse(%q) [round 2]: %v", got, err)
	}
	if !docmodel.Equal(doc, again) {
		t.Errorf("the paragraph became an image:\n got:  %#v\n want: %#v", again, doc)
	}
}

// TestSerialize_EscapesNestedLinkLabelBrackets is FuzzRoundTrip's
// nineteenth crasher, minimized from "0![[]\\]()". The document is a single
// run of TEXT — "0![[]]()", no links or images in it at all — and written
// bare it came back as an image with the alt text "[]" sitting in a
// paragraph next to other text, which Parse refuses.
//
// The escape rule used to ask, of each "[", whether a "](" closed it. A
// link label NESTS — CommonMark lets it hold balanced bracket pairs — so
// answering that means knowing which of the brackets in between are
// escaped, which is the same question again. Counting depth only moved the
// circle: escaping an inner "[" changes whether the outer one is closed.
//
// Escaping the "](" instead has no loop in it, and these three cases are
// the ones that show why the answer is also less noisy. Each canonical
// spelling here is the FUZZER'S OWN INPUT, unchanged — the escape lands
// exactly where a person writing the text by hand would have put it.
func TestSerialize_EscapesNestedLinkLabelBrackets(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"the fuzz crasher", "0![[]\\]()\n"},
		{"a nested pair inside a real label", "[a[b]c\\](d)\n"},
		{"an unbalanced bracket is not a label", "[a] (b)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the text:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_CodeSpanPaddingCountsSpacesOnly is FuzzRoundTrip's
// twenty-second crasher, minimized from "“   \v   “". A code span whose
// content begins and ends with a space needs a padding space on each side,
// because markdown strips one — unless the content is ENTIRELY spaces, in
// which case it strips nothing and the padding would be wrong.
//
// Both halves of that rule were wrong, in opposite directions, and the
// fuzzer found each. "Not entirely blank" was strings.TrimSpace, which
// counts a vertical tab and U+00A0 as blank where goldmark does not — so
// "  \v  " went out unpadded and came back two characters shorter every
// save. And it does NOT count a carriage return as blank where goldmark
// does, so "\r " was padded when nothing would strip it and the file grew
// by two characters every save instead. Erosion one way, unbounded growth
// the other, and neither visible in any single diff.
//
// The rule now asks goldmark (util.IsBlank) rather than restating it, since
// the parser is what this package's output has to satisfy.
func TestSerialize_CodeSpanPaddingCountsSpacesOnly(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"a vertical tab is content, so the spaces are padded", "  \v  ", "`   \v   `\n"},
		{"a non-breaking space is content too", " \u00a0 ", "`  \u00a0  `\n"},
		{"entirely spaces is stripped by nobody, so no padding", "   ", "`   `\n"},
		{"an ordinary edge space is padded", " a ", "`  a  `\n"},
		{"a vertical tab alone needs nothing", "\v", "`\v`\n"},
		// FuzzRoundTrip's twenty-third crasher, "`\r `". goldmark counts a
		// carriage return as blank, so it strips nothing here — and the
		// padding this used to add came back as content, growing the file
		// by two characters every save.
		{"a carriage return is blank to goldmark, so no padding", "\r ", "`\r `\n"},
		{"and blank at both ends is left alone too", " \r ", "` \r `\n"},
		{"one edge space alone is stripped by nobody", " a", "` a`\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := docmodel.Doc{Blocks: []docmodel.Block{para(
				marked(tc.content, mark(docmodel.Code)),
			)}}
			got := markdown.Serialize(doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("the code span lost content:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_EscapesTableDelimiterRow is FuzzRoundTrip's twenty-sixth
// crasher, minimized from "0\\\n-\\|". A paragraph that grows a delimiter row
// by accident silently becomes a TABLE — and "0", a hard break, and "-|" is a
// one-column table. Phase 1e made tables real, so the consequence changed
// from "the file will not load" to "the paragraph quietly became something
// else", which makes this escape matter more rather than less.
//
// The check is deliberately confined to lines that are not the block's
// first. A table needs a header row ABOVE its delimiter row, and the first
// line of a block has none, so "-|" written on its own stays exactly as the
// author typed it. Only a line after a hard break has a header above it.
//
// What counts as a delimiter row is measured against goldmark rather than
// reasoned from the GFM spec: it takes ":-:" with no pipe at all, and
// refuses ":|" for having no dash.
func TestSerialize_EscapesTableDelimiterRow(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"the fuzz crasher", "0\\\n\\-|\n"},
		{"a leading pipe", "0\\\n\\|--|--|\n"},
		{"colons with no pipe are still a delimiter row", "0\\\n\\:-:\n"},
		{"a carriage return is whitespace here too", "0\\\n\\-\r|\n"},
		{"no dash is no delimiter row", "0\\\n:|\n"},
		{"a pipe alone is not one either", "0\\\n|\n"},
		{"the block's first line has no header above it", "-|\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := markdown.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := markdown.Serialize(doc)
			if string(got) != tc.src {
				t.Fatalf("Serialize(Parse(%q)) = %q, want %q", tc.src, got, tc.src)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q) [round 2]: %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("round trip changed the text:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_EmptyNestedListItems is FuzzRoundTrip's twenty-ninth
// crasher, minimized from "*\t*\n\t\t*\n\t0)\n0)". A list item with no
// content puts its marker on the same line as the nested list inside it, so
// three levels of empty items wrote "- - - " — which is a THEMATIC BREAK,
// not a list at all. The whole nested structure vanished on the next read,
// and the lists that followed it merged into each other.
//
// Two things were wrong. An empty item wrote its marker WITH the trailing
// space, which is whitespace at the end of a line in somebody's file for no
// reader's benefit. And nothing checked whether the markers, once stacked,
// spelled something other than a list.
//
// Flipping this list's bullet character fixes it and keeps fixing it: the
// markers after it on that line belong to other lists and do not move, so
// the line now holds two different characters, and a thematic break needs
// one.
func TestSerialize_EmptyNestedListItems(t *testing.T) {
	// nest builds n levels of bullet list, each with one item, the
	// innermost of which is empty.
	//
	// EVERY ITEM OPENS WITH AN EMPTY PARAGRAPH, because that is the document
	// Parse produces and this test round-trips through Parse. TipTap's
	// listItem is `paragraph block*`, so an item whose first child is not a
	// paragraph is a node the browser deletes — see markdown.legalize. The
	// paragraph costs nothing on the way out: renderedBlocks drops a block
	// that renders to nothing, so the wanted bytes below are unchanged.
	var nest func(n int) docmodel.Block
	nest = func(n int) docmodel.Block {
		item := docmodel.Block{Kind: docmodel.ListItem, Children: []docmodel.Block{{Kind: docmodel.Paragraph}}}
		if n > 1 {
			item.Children = append(item.Children, nest(n-1))
		}
		return docmodel.Block{Kind: docmodel.BulletList, Children: []docmodel.Block{item}}
	}
	tests := []struct {
		depth int
		want  string
	}{
		{1, "-\n"},
		{2, "- -\n"},
		{3, "* - -\n"},
		{4, "- * - -\n"},
	}
	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.depth), func(t *testing.T) {
			doc := docmodel.Doc{Blocks: []docmodel.Block{nest(tc.depth)}}
			got := markdown.Serialize(doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("the nesting did not survive:\n got:  %#v\n want: %#v", again, doc)
			}
		})
	}
}

// TestSerialize_EmptyListAfterAParagraph is FuzzRoundTrip's thirty-first
// crasher, minimized from "* 0\\\n\\\n0\n\n\t*". A nested list whose first
// item is EMPTY renders its whole first line as the marker — just "-" — and
// a bare "-" under a paragraph line is a SETEXT HEADING UNDERLINE. The
// paragraph above became an h2 and the list vanished into it.
//
// So "a list interrupts a paragraph" needed the qualifier markdown puts on
// it: only a list whose first item has CONTENT does. Without content there
// is nothing to interrupt with, and the marker means something else
// entirely.
func TestSerialize_EmptyListAfterAParagraph(t *testing.T) {
	// item guarantees the leading paragraph TipTap's `paragraph block*`
	// requires and Parse now writes (markdown.legalize). It changes no byte of
	// the wanted output — renderedBlocks drops a block that renders to nothing
	// — and it is what makes these fixtures documents Parse could produce.
	item := leadingPara
	bullets := func(items ...docmodel.Block) docmodel.Block {
		return docmodel.Block{Kind: docmodel.BulletList, Children: items}
	}
	tests := []struct {
		name string
		doc  docmodel.Doc
		want string
		// normalized is the tree the output reads back as, where that is
		// not the tree that went in. An empty paragraph is not writable —
		// renderedBlocks drops it — so a doc holding one settles on the
		// second serialization rather than the first, which is a
		// normalization and not a loss.
		normalized *docmodel.Doc
	}{
		{
			name: "an empty nested list needs the blank line",
			doc:  docmodel.Doc{Blocks: []docmodel.Block{bullets(item(para(text("a")), bullets(item())))}},
			want: "- a\n\n  -\n",
		},
		{
			// FuzzRoundTrip's thirty-second crasher, from
			// "* *\t\f\n\t0)\n0)". The innermost item holds an EMPTY
			// paragraph, which is not written at all — but it made
			// endsWithParagraph claim an open paragraph line, so a blank
			// line went in on behalf of a block that was never there, and
			// that blank line closed the item everything after it belonged
			// to.
			name: "an empty paragraph leaves no line open",
			doc: docmodel.Doc{Blocks: []docmodel.Block{bullets(item(
				bullets(item(para())),
				docmodel.Block{Kind: docmodel.OrderedList, Children: []docmodel.Block{item()}},
			))}},
			want: "- -\n  1.\n",
			normalized: &docmodel.Doc{Blocks: []docmodel.Block{bullets(item(
				bullets(item()),
				docmodel.Block{Kind: docmodel.OrderedList, Children: []docmodel.Block{item()}},
			))}},
		},
		{
			// FuzzRoundTrip's thirty-seventh crasher, from
			// "* *\t0\n\t*\n\t0)\n0)". The nested list's LAST item is
			// empty, so the last line written is a bare marker — not an
			// open paragraph. Skipping that item to find "the last thing
			// written" reached back to the paragraph in the item before it
			// and inserted a blank line, which closed the outer item.
			name: "an empty last item still ends the line",
			doc: docmodel.Doc{Blocks: []docmodel.Block{bullets(item(
				bullets(item(para(text("a"))), item()),
				docmodel.Block{Kind: docmodel.OrderedList, Children: []docmodel.Block{item()}},
			))}},
			want: "- - a\n  -\n  1.\n",
		},
		{
			name: "a nested list with content stays tight",
			doc:  docmodel.Doc{Blocks: []docmodel.Block{bullets(item(para(text("a")), bullets(item(para(text("b"))))))}},
			want: "- a\n  - b\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdown.Serialize(tc.doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			want := tc.doc
			if tc.normalized != nil {
				want = *tc.normalized
			}
			if !docmodel.Equal(want, again) {
				t.Errorf("the list became a heading underline:\n got:  %#v\n want: %#v", again, want)
			}
			if m2 := markdown.Serialize(again); string(m2) != string(got) {
				t.Errorf("not a fixed point: %q then %q", got, m2)
			}
		})
	}
}

// TestSerialize_AdjacentBlockquotes_StayDistinct is FuzzRoundTrip's
// thirty-fourth crasher, minimized from "* >*\n\n  >0" and found only after
// eleven minutes of fuzzing. Two sibling blockquotes inside a list item were
// written on consecutive lines, and "> a" followed by "> b" is ONE
// blockquote with two lines in it. The two collapsed into one.
//
// It is the blockquote's version of the adjacent-list problem, which
// renderedBlocks solves by alternating the list marker. A blockquote has no
// alternate marker, so a blank line is the only separator there is — and
// unlike the paragraph cases, it is needed however the first quote ENDS,
// because it is the "> " prefix that joins them rather than a lazy
// continuation.
func TestSerialize_AdjacentBlockquotes_StayDistinct(t *testing.T) {
	quote := func(blocks ...docmodel.Block) docmodel.Block {
		return docmodel.Block{Kind: docmodel.Blockquote, Children: blocks}
	}
	item := leadingPara
	tests := []struct {
		name string
		doc  docmodel.Doc
		want string
	}{
		{
			name: "two quotes in one list item",
			doc: docmodel.Doc{Blocks: []docmodel.Block{{
				Kind:     docmodel.BulletList,
				Children: []docmodel.Block{item(quote(para(text("a"))), quote(para(text("b"))))},
			}}},
			want: "- > a\n\n  > b\n",
		},
		{
			name: "a quote ending in a heading still needs it",
			doc: docmodel.Doc{Blocks: []docmodel.Block{{
				Kind: docmodel.BulletList,
				Children: []docmodel.Block{item(
					quote(docmodel.Block{
						Kind:    docmodel.Heading,
						Attrs:   map[string]string{"level": "1"},
						Inlines: []docmodel.Inline{text("h")},
					}),
					quote(para(text("b"))),
				)},
			}}},
			want: "- > # h\n\n  > b\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdown.Serialize(tc.doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(tc.doc, again) {
				t.Errorf("the quotes merged:\n got:  %#v\n want: %#v", again, tc.doc)
			}
		})
	}
}

// TestSerialize_EmptyListBetweenSiblingLists is fix round 1's Important 1: a
// list that renders to NOTHING sat between two sibling lists and flipped
// the marker alternation on its way past, so the second list got the same
// marker as the first and the two merged on reparse — the exact failure
// TestSerialize_AdjacentSameKindLists_StayDistinct exists to prevent,
// reached through the one door that test did not cover.
//
// The alternation is now committed only when the block actually writes
// something, which is the shape prevKind already had: a block that is not
// in the output is not between its neighbours either, and must not move
// anything.
func TestSerialize_EmptyListBetweenSiblingLists(t *testing.T) {
	item := func(cs ...docmodel.Block) docmodel.Block {
		return docmodel.Block{Kind: docmodel.ListItem, Children: cs}
	}
	bullets := func(items ...docmodel.Block) docmodel.Block {
		return docmodel.Block{Kind: docmodel.BulletList, Children: items}
	}
	ordered := func(items ...docmodel.Block) docmodel.Block {
		return docmodel.Block{Kind: docmodel.OrderedList, Children: items}
	}
	tests := []struct {
		name string
		doc  docmodel.Doc
		want string
		// normalized is what the output reads back as. An empty list is not
		// writable, so it is gone from the second serialization on — a
		// normalization, and the point is that the lists AROUND it survive.
		normalized docmodel.Doc
	}{
		{
			name: "an empty bullet list between two bullet lists",
			doc: docmodel.Doc{Blocks: []docmodel.Block{
				bullets(item(para(text("a")))), bullets(), bullets(item(para(text("b")))),
			}},
			want:       "- a\n\n* b\n",
			normalized: docmodel.Doc{Blocks: []docmodel.Block{bullets(item(para(text("a")))), bullets(item(para(text("b"))))}},
		},
		{
			name: "two empty lists between them still alternate once",
			doc: docmodel.Doc{Blocks: []docmodel.Block{
				bullets(item(para(text("a")))), bullets(), bullets(), bullets(item(para(text("b")))),
			}},
			want:       "- a\n\n* b\n",
			normalized: docmodel.Doc{Blocks: []docmodel.Block{bullets(item(para(text("a")))), bullets(item(para(text("b"))))}},
		},
		{
			name: "an empty ordered list between two ordered lists",
			doc: docmodel.Doc{Blocks: []docmodel.Block{
				ordered(item(para(text("a")))), ordered(), ordered(item(para(text("b")))),
			}},
			want:       "1. a\n\n1) b\n",
			normalized: docmodel.Doc{Blocks: []docmodel.Block{ordered(item(para(text("a")))), ordered(item(para(text("b"))))}},
		},
		{
			name: "an empty paragraph between them does the same",
			doc: docmodel.Doc{Blocks: []docmodel.Block{
				bullets(item(para(text("a")))), para(), bullets(item(para(text("b")))),
			}},
			want:       "- a\n\n* b\n",
			normalized: docmodel.Doc{Blocks: []docmodel.Block{bullets(item(para(text("a")))), bullets(item(para(text("b"))))}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdown.Serialize(tc.doc)
			if string(got) != tc.want {
				t.Fatalf("Serialize = %q, want %q", got, tc.want)
			}
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(tc.normalized, again) {
				t.Errorf("the lists merged:\n got:  %#v\n want: %#v", again, tc.normalized)
			}
			if m2 := markdown.Serialize(again); string(m2) != string(got) {
				t.Errorf("not a fixed point: %q then %q", got, m2)
			}
		})
	}
}

// TestSerialize_DestinationWithANewline is fix round 1's Important 2. A link
// or image destination holding a NEWLINE has no spelling in markdown: the
// bare form ends at the first space, and the angle form forbids line
// endings just as firmly — so "[x](<a\nb>)" is not a link, the "<" that
// opens it is RAW HTML, and Parse refuses the file outright.
//
// It is percent-encoded, which is what a URL does with a newline anyway.
// That is a normalization rather than a loss — "a%0Ab" says exactly what
// "a\nb" said — and the second write is a fixed point.
//
// Only the newline. The other awkward characters are measured, not assumed:
// a space, a tab, a NUL and a lone carriage return all survive the angle
// form intact, so encoding those too would be a diff in somebody's file for
// nothing.
func TestSerialize_DestinationWithANewline(t *testing.T) {
	image := func(src string) docmodel.Doc {
		return docmodel.Doc{Blocks: []docmodel.Block{{
			Kind:  docmodel.Image,
			Attrs: map[string]string{"alt": "x", "src": src},
		}}}
	}
	t.Run("a newline is percent-encoded", func(t *testing.T) {
		got := markdown.Serialize(image("a\nb"))
		want := "![x](a%0Ab)\n"
		if string(got) != want {
			t.Fatalf("Serialize = %q, want %q", got, want)
		}
		again, _, err := markdown.Parse(got)
		if err != nil {
			t.Fatalf("Parse(%q): %v", got, err)
		}
		if src := again.Blocks[0].Attrs["src"]; src != "a%0Ab" {
			t.Errorf("src = %q, want %q", src, "a%0Ab")
		}
		if m2 := markdown.Serialize(again); string(m2) != string(got) {
			t.Errorf("not a fixed point: %q then %q", got, m2)
		}
	})
	// Everything else keeps its bytes, and keeps them through the angle
	// form. If goldmark ever stops accepting one of these, the round-trip
	// assertion here is what says so.
	for _, src := range []string{"a b", "a\tb", "a\rb", "a\x00b", "a(b)c", "a<b"} {
		t.Run(strconv.Quote(src), func(t *testing.T) {
			doc := image(src)
			got := markdown.Serialize(doc)
			again, _, err := markdown.Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if !docmodel.Equal(doc, again) {
				t.Errorf("destination did not survive %q:\n got:  %#v\n want: %#v", got, again, doc)
			}
			if m2 := markdown.Serialize(again); string(m2) != string(got) {
				t.Errorf("not a fixed point: %q then %q", got, m2)
			}
		})
	}
	t.Run("a link href too, not just an image src", func(t *testing.T) {
		doc := docmodel.Doc{Blocks: []docmodel.Block{para(marked("x", docmodel.Mark{
			Kind:  docmodel.Link,
			Attrs: map[string]string{"href": "a\nb"},
		}))}}
		got := markdown.Serialize(doc)
		want := "[x](a%0Ab)\n"
		if string(got) != want {
			t.Fatalf("Serialize = %q, want %q", got, want)
		}
		if _, _, err := markdown.Parse(got); err != nil {
			t.Fatalf("Parse(%q): %v", got, err)
		}
	})
}

// TestSerialize_ThematicBreakFlipKeepsSiblingListsApart is FuzzRoundTrip's
// finding from the phase-1e seed sweep, and it is a LIST bug rather than a
// table one — the seed that led the mutator here has no pipe in it.
//
// renderedBlocks alternates a list's marker whenever a same-kind list sits
// immediately before it, because markdown separates two adjacent lists by
// their marker and by nothing else. renderList then has a second, later say
// in the matter: a list of empty nested items puts its markers on one line,
// and three of them is "- - -", a THEMATIC BREAK, so it re-renders with the
// other bullet.
//
// Those two decisions were not talking to each other. renderedBlocks recorded
// the style it ASKED for, renderList wrote the other one, and the next
// sibling list therefore chose exactly the marker its predecessor had just
// used — so the two merged into one list, which is precisely the failure the
// alternation exists to prevent. It cost a round per list pair to unwind,
// which is how it surfaced: a document of fifty sibling lists ran out of
// convergence budget rather than reading back wrong all at once.
func TestSerialize_ThematicBreakFlipKeepsSiblingListsApart(t *testing.T) {
	// A list whose whole content is empty nested items: its markers land on
	// one line, so the first spelling is "- - -" and the flip fires.
	nested := docmodel.Block{Kind: docmodel.BulletList, Children: []docmodel.Block{
		{Kind: docmodel.ListItem, Children: []docmodel.Block{
			{Kind: docmodel.BulletList, Children: []docmodel.Block{
				{Kind: docmodel.ListItem, Children: []docmodel.Block{
					{Kind: docmodel.BulletList, Children: []docmodel.Block{
						{Kind: docmodel.ListItem},
					}},
				}},
			}},
		}},
	}}
	after := docmodel.Block{Kind: docmodel.BulletList, Children: []docmodel.Block{
		{Kind: docmodel.ListItem},
	}}
	doc := docmodel.Doc{Blocks: []docmodel.Block{nested, after}}

	out := markdown.Serialize(doc)
	again, _, err := markdown.Parse(out)
	if err != nil {
		t.Fatalf("Parse(%q): %v", out, err)
	}
	if n := len(again.Blocks); n != 2 {
		t.Fatalf("two sibling lists were written as %d block(s) — they merged: %q", n, out)
	}
	if got := markdown.Serialize(again); string(got) != string(out) {
		t.Errorf("did not settle on the first write:\n out1: %q\n out2: %q", out, got)
	}
}
