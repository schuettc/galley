package suggest

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// A substitution is ONE suggestion with ONE decision.
//
// "{~~brown~>red~~}" is CriticMarkup's substitution form and galley writes
// it: select a word in the editor, type over it, and that is what lands on
// disk. List used to report it as TWO pending suggestions, a delete and an
// insert, each separately decidable — and two of the four half-decisions
// leave the file holding a word nobody wrote:
//
//	accept the delete half  -> The quick {++red++} fox      coherent
//	reject the insert half  -> The quick {--brown--} fox    coherent
//	reject the delete half  -> The quick brown{++red++} fox -> "brownred"
//	accept the insert half  -> The quick {--brown--}red fox -> "brownred"
//
// Both bad states are reached by making two individually reasonable
// decisions, and the intermediate is on disk before the second one.

const subDoc = "The quick {~~brown~>red~~} fox\n"

func parseDoc(t *testing.T, src string) docmodel.Doc {
	t.Helper()
	d, _, err := markdown.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return d
}

func TestSubstitutionListsAsOneSuggestion(t *testing.T) {
	got := List(parseDoc(t, subDoc))
	if len(got) != 1 {
		t.Fatalf("List reported %d pending, want 1: %+v", len(got), got)
	}
	p := got[0]
	if p.Kind != KindReplace {
		t.Errorf("kind = %q, want %q", p.Kind, KindReplace)
	}
	if p.Old != "brown" || p.New != "red" {
		t.Errorf("halves = %q -> %q, want %q -> %q", p.Old, p.New, "brown", "red")
	}
	// Text reads as a replacement rather than as one half of one.
	if want := "brown" + replaceSep + "red"; p.Text != want {
		t.Errorf("text = %q, want %q", p.Text, want)
	}
	// Context is the block as Serialize writes it, which is already the
	// substitution's own spelling.
	if !strings.Contains(p.Context, "{~~brown~>red~~}") {
		t.Errorf("context = %q, want the substitution in it", p.Context)
	}
}

func TestSubstitutionTakenReadsAsTheReplacement(t *testing.T) {
	d := parseDoc(t, subDoc)
	taken, _ := readings(d)
	if taken != "The quick red fox" {
		t.Errorf("taken reads %q, want %q", taken, "The quick red fox")
	}
	p := List(d)[0]
	if got := "The quick " + p.New + " fox"; got != taken {
		t.Errorf("the one span's New gives %q, want the taken reading %q", got, taken)
	}
}

func TestSubstitutionDeclinedReadsAsTheOriginal(t *testing.T) {
	d := parseDoc(t, subDoc)
	_, declined := readings(d)
	if declined != "The quick brown fox" {
		t.Errorf("declined reads %q, want %q", declined, "The quick brown fox")
	}
	p := List(d)[0]
	if got := "The quick " + p.Old + " fox"; got != declined {
		t.Errorf("the one span's Old gives %q, want the declined reading %q", got, declined)
	}
}

// No reading of the file can say "brownred" — the whole point. That word was
// reachable only by deciding the halves apart, and the halves cannot be decided
// apart when nothing lists them apart: the read path reports one replace, and
// no insert or delete beside it.
func TestSubstitutionHasNoIncoherentReading(t *testing.T) {
	d := parseDoc(t, subDoc)
	for _, p := range List(d) {
		if p.Kind == KindInsert || p.Kind == KindDelete {
			t.Fatalf("a half of the substitution listed on its own: %+v", p)
		}
	}
	taken, declined := readings(d)
	for _, r := range []string{taken, declined} {
		if strings.Contains(r, "brownred") {
			t.Fatalf("a reading of the file says %q", r)
		}
	}
}

// A DEL THAT IS NOT PURE IS NOT HALF OF A SUBSTITUTION. A highlighted
// deletion is a deletion someone is talking about; collapsing it into the
// insertion after it would drop the highlight, so the serializer declines
// to pair it and so must List.
func TestHighlightedDeletionDoesNotGroup(t *testing.T) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind: docmodel.Paragraph,
		Inlines: []docmodel.Inline{
			{Text: "The quick "},
			{Text: "brown", Marks: []docmodel.Mark{
				authoredMark(docmodel.Highlight, "court", at, newRun()),
				authoredMark(docmodel.Del, "court", at, newRun()),
			}},
			{Text: "red", Marks: []docmodel.Mark{authoredMark(docmodel.Ins, "court", at, newRun())}},
			{Text: " fox"},
		},
	}}}
	if subs := markdown.Substitutions(d.Blocks[0].Inlines); len(subs) != 0 {
		t.Fatalf("the serializer paired a highlighted deletion: %+v", subs)
	}
	// Document order, so the highlight — which starts where the deletion
	// does — sits between the two halves it refused to let merge.
	kinds := kindsOf(List(d))
	want := []Kind{KindDelete, KindComment, KindInsert}
	if !sameKinds(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

// A del and an ins with anything between them are two spans in the file and
// two decisions here.
func TestSeparatedDeleteAndInsertDoNotGroup(t *testing.T) {
	d := parseDoc(t, "The quick {--brown--} and {++red++} fox\n")
	kinds := kindsOf(List(d))
	want := []Kind{KindDelete, KindInsert}
	if !sameKinds(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

// An ins BEFORE a del is not a substitution either: the form is
// "{~~old~>new~~}", and the file would have to reorder the reviewer's text
// to spell the other way round.
func TestInsertBeforeDeleteDoesNotGroup(t *testing.T) {
	d := parseDoc(t, "The quick {++red++}{--brown--} fox\n")
	kinds := kindsOf(List(d))
	want := []Kind{KindInsert, KindDelete}
	if !sameKinds(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

// WHERE THE SERIALIZER DECLINES TO PAIR, TWO CARDS IS THE HONEST ANSWER.
// The substitution markers occur in the deleted text, so one span would be
// ambiguous to its own reader (unsafeForSubstitution); the file really does
// hold two spans, and List follows the file.
func TestUnpairableSubstitutionListsAsTwo(t *testing.T) {
	const src = "The quick {--a~>b--}{++red++} fox\n"
	d := parseDoc(t, src)
	// The premise: the file's own spelling is still two spans.
	if got := string(markdown.Serialize(d)); !strings.Contains(got, "{--a~>b--}") {
		t.Fatalf("serialize = %q, want the deletion written on its own", got)
	}
	if subs := markdown.Substitutions(d.Blocks[0].Inlines); len(subs) != 0 {
		t.Fatalf("the serializer paired an unspellable substitution: %+v", subs)
	}
	kinds := kindsOf(List(d))
	want := []Kind{KindDelete, KindInsert}
	if !sameKinds(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

// The ordinal stays the display coordinate: after grouping there is simply
// one fewer id in the list, and the ones that remain still number from 1 in
// document order.
func TestSubstitutionRenumbersTheOrdinals(t *testing.T) {
	d := parseDoc(t, "A {++one++} b {~~brown~>red~~} c {--two--} d\n")
	got := List(d)
	if len(got) != 3 {
		t.Fatalf("List reported %d, want 3: %+v", len(got), got)
	}
	for i, want := range []struct {
		id   string
		kind Kind
	}{{"s1", KindInsert}, {"s2", KindReplace}, {"s3", KindDelete}} {
		if got[i].ID != want.id || got[i].Kind != want.kind {
			t.Errorf("entry %d = %s/%s, want %s/%s", i, got[i].ID, got[i].Kind, want.id, want.kind)
		}
	}
	if got[1].Old != "brown" || got[1].New != "red" {
		t.Errorf("s2 halves = %q -> %q, want brown -> red", got[1].Old, got[1].New)
	}
}

// The run is the DEL half's: see substitutionSpan. Both halves have one, so
// which one is Run has to be stable and stated; the other is InsRun.
func TestSubstitutionRunIsTheDeletedHalfs(t *testing.T) {
	d := MintRuns(parseDoc(t, subDoc))
	got := List(d)
	if len(got) != 1 {
		t.Fatalf("List reported %d, want 1", len(got))
	}
	del := d.Blocks[0].Inlines[1]
	if got[0].Run == "" || got[0].Run != del.Attr(docmodel.Del, docmodel.RunAttr) {
		t.Fatalf("run = %q, want the Del mark's %q", got[0].Run, del.Attr(docmodel.Del, docmodel.RunAttr))
	}
	ins := d.Blocks[0].Inlines[2]
	if got[0].InsRun == "" || got[0].InsRun != ins.Attr(docmodel.Ins, docmodel.RunAttr) {
		t.Fatalf("insRun = %q, want the Ins mark's %q", got[0].InsRun, ins.Attr(docmodel.Ins, docmodel.RunAttr))
	}
}

func kindsOf(pendings []Pending) []Kind {
	out := make([]Kind, len(pendings))
	for i, p := range pendings {
		out[i] = p.Kind
	}
	return out
}

func sameKinds(a, b []Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BOTH HALVES OF ONE SPAN ARE ADDRESSABLE, AND ONLY ONE OF THEM IS THE
// DECISION.
//
// The file holds one span, this package reports one suggestion, and the
// reviewer makes one decision — but the DOCUMENT holds a Del and an Ins, with
// two runs, because markdown's applyMark is called once per half and correctly
// stamps each. Everything Go-side addresses the deleted half's run and that is
// right. The browser cannot: a reviewer clicking the GREEN half of a
// substitution is clicking a mark whose run appeared nowhere on the wire, so it
// matched nothing and the bubble said "the server has not seen this one yet"
// about the very suggestion whose card was on screen with two working verbs.
//
// So the pairing this package already computed is PUBLISHED. It is not a second
// coordinate for the decision — Run is still what accept and reject take — and
// it is emphatically not a rule the browser re-derives, which is the thing
// CLAUDE.md's first law here forbids on either side of the wire.
func TestASubstitutionPublishesBothHalvesRuns(t *testing.T) {
	// ONE PARSE, read twice. A run is re-minted by every parse (CLAUDE.md), so
	// a second parseDoc here would compare two documents' tokens and fail on a
	// correct implementation — which it did, on the first run of this test.
	d := parseDoc(t, subDoc)
	got := List(d)
	if len(got) != 1 {
		t.Fatalf("List reported %d pending, want 1: %+v", len(got), got)
	}
	p := got[0]
	if p.Run == "" {
		t.Fatal("the deleted half has no run — the decision has no coordinate")
	}
	if p.InsRun == "" {
		t.Fatal("the inserted half's run is not on the wire, so a click on the " +
			"green half of a substitution can reach nothing")
	}
	// TWO RUNS, NOT ONE. The halves are two CriticMarkup spans to the parser
	// (applyMark runs twice) and their runs differ by construction; a test that
	// tolerated equality would pass against an implementation that copied Run
	// into InsRun and taught the browser nothing.
	if p.Run == p.InsRun {
		t.Errorf("both halves report the same run %q — then this field says nothing", p.Run)
	}
	// AND IT IS THE INSERTED HALF'S, which is checkable against the document
	// itself: the run on the Ins-marked inline carrying "red".
	want := ""
	docmodel.Walk(d, func(_ []int, b *docmodel.Block) {
		for _, in := range b.Inlines {
			if in.Text == "red" && in.Attr(docmodel.Ins, docmodel.RunAttr) != "" {
				want = in.Attr(docmodel.Ins, docmodel.RunAttr)
			}
		}
	})
	if want == "" {
		t.Fatal("fixture: no ins-marked \"red\" inline to read a run from")
	}
	if p.InsRun != want {
		t.Errorf("insRun = %q, want the inserted half's own run %q", p.InsRun, want)
	}
}

// EVERY OTHER KIND CARRIES NOTHING, so a consumer can branch on presence.
func TestOnlyASubstitutionCarriesAnInsertedHalf(t *testing.T) {
	src := "A {++fresh++} line with {--struck--} words and {==noted==} ones.\n"
	for _, p := range List(parseDoc(t, src)) {
		if p.InsRun != "" {
			t.Errorf("%s (%s) carries insRun %q, which only a replace has",
				p.ID, p.Kind, p.InsRun)
		}
	}
}
