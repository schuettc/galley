package suggest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

func threePara() docmodel.Doc {
	para := func(s string) docmodel.Block {
		return docmodel.Block{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: s}}}
	}
	return docmodel.Doc{Blocks: []docmodel.Block{
		para("Four Cognito behaviors shaped this."),
		para("Each was measured rather than read."),
		para("And each explains a decision above."),
	}}
}

var when = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// TestASelectionCrossingBlocksAnchors is the defect. Every multi-block
// selection failed, always, and only AFTER the reviewer had typed their
// instruction: the browser sent the concatenated text and `findUnique` searched
// block by block, so no single block could ever contain it.
func TestASelectionCrossingBlocksAnchors(t *testing.T) {
	d := threePara()
	out, err := suggest.CommentAcross(d, []int{0}, 5, []int{2}, 8, "cm-000000000000000e", "court", when)
	if err != nil {
		t.Fatalf("a selection across three paragraphs was refused: %v", err)
	}
	pending := suggest.List(out)
	if len(pending) != 1 {
		t.Fatalf("one selection produced %d cards: %+v — a run across blocks is ONE decision", len(pending), pending)
	}
	p := pending[0]
	for _, want := range []string{"Cognito behaviors shaped this.", "Each was measured", "And each"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("the quote is missing %q: %q", want, p.Text)
		}
	}
	if p.Run == "" {
		t.Error("the span carries no run, so nothing can address it")
	}
	if p.CommentID != "cm-000000000000000e" {
		t.Errorf("the span carries ID %q, want the one it was given", p.CommentID)
	}
}

// TestEveryTouchedBlockCarriesTheSameRun is what makes it one decision rather
// than three that happen to look alike. docmodel.RunAttr names ONE authored
// edit; this is that rule applied across blocks.
func TestEveryTouchedBlockCarriesTheSameRun(t *testing.T) {
	out, err := suggest.CommentAcross(threePara(), []int{0}, 5, []int{2}, 8, "cm-000000000000000f", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	runs := map[string]int{}
	ids := map[string]int{}
	marked := 0
	for _, b := range out.Blocks {
		for _, in := range b.Inlines {
			if in.Has(docmodel.Highlight) {
				marked++
				runs[in.Attr(docmodel.Highlight, docmodel.RunAttr)]++
				ids[in.Attr(docmodel.Highlight, docmodel.CommentIDAttr)]++
			}
		}
	}
	if marked == 0 {
		t.Fatal("nothing was marked at all")
	}
	if len(runs) != 1 {
		t.Errorf("the span was stamped with %d runs, want 1: %v — three runs is three decisions", len(runs), runs)
	}
	// AND ONE ID ON EVERY PIECE: the file has no run, so the ID is what says,
	// after a restart, that the pieces are one comment.
	if len(ids) != 1 || ids["cm-000000000000000f"] != marked {
		t.Errorf("the pieces carry IDs %v, want cm-000000000000000f on all %d", ids, marked)
	}
}

// TestTheEndsArePartialAndTheMiddleIsWhole.
func TestTheEndsArePartialAndTheMiddleIsWhole(t *testing.T) {
	out, err := suggest.CommentAcross(threePara(), []int{0}, 5, []int{2}, 8, "cm-0000000000000010", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	said := func(i int) string {
		var b strings.Builder
		for _, in := range out.Blocks[i].Inlines {
			if in.Has(docmodel.Highlight) {
				b.WriteString(in.Text)
			}
		}
		return b.String()
	}
	if got := said(0); got != "Cognito behaviors shaped this." {
		t.Errorf("the first block should be marked from the offset to its end, got %q", got)
	}
	if got := said(1); got != "Each was measured rather than read." {
		t.Errorf("a middle block should be marked whole, got %q", got)
	}
	if got := said(2); got != "And each" {
		t.Errorf("the last block should be marked from its start to the offset, got %q", got)
	}
}

// TestDeletingACrossBlockCommentLiftsEveryBlocksHighlight. Lifting it from the
// first block alone would leave the rest marked with a run no thread points at.
func TestDeletingACrossBlockCommentLiftsEveryHighlight(t *testing.T) {
	out, err := suggest.CommentAcross(threePara(), []int{0}, 5, []int{2}, 8, "cm-0000000000000011", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	pending := suggest.List(out)
	if len(pending) != 1 {
		t.Fatalf("want one span to decide, got %d", len(pending))
	}
	lifted, err := suggest.Accept(out, pending[0].ID)
	if err != nil {
		t.Fatalf("lifting the highlight: %v", err)
	}
	// And the delete verb, which lifts by the ID.
	detached, ok := suggest.Detach(out, []review.Thread{{Key: "cm-0000000000000011"}}, "cm-0000000000000011")
	if !ok {
		t.Fatal("Detach found no piece of the comment")
	}
	for name, d := range map[string]docmodel.Doc{"accept": lifted, "detach": detached} {
		for i, b := range d.Blocks {
			for _, in := range b.Inlines {
				if in.Has(docmodel.Highlight) {
					t.Errorf("%s: block %d kept its highlight after the comment was lifted: %q", name, i, in.Text)
				}
			}
		}
	}
}

// TestTwoAdjacentCommentsStayTwo is the counter-case for the coalescer, and the
// one that says it may only join on the RUN. Two selections over neighbouring
// paragraphs are two decisions however alike they look.
func TestTwoAdjacentCommentsStayTwo(t *testing.T) {
	d := threePara()
	one, err := suggest.CommentOnRange(d, []int{0}, 0, 4, "cm-0000000000000012", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	two, err := suggest.CommentOnRange(one, []int{1}, 0, 4, "cm-0000000000000013", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	if got := suggest.List(two); len(got) != 2 {
		t.Errorf("two separate comments on adjacent blocks collapsed into %d: %+v", len(got), got)
	}
}

// TestABackwardsRangeIsRefused. The browser always sends document order, so
// this is a contract check — and swapping the ends quietly would hide a caller
// bug rather than report it.
func TestABackwardsRangeIsRefused(t *testing.T) {
	if _, err := suggest.CommentAcross(threePara(), []int{2}, 0, []int{0}, 4, "cm-0000000000000014", "court", when); err == nil {
		t.Error("a backwards selection was accepted")
	}
}

// TestOverlappingAnExistingCommentIsRefusedBeforeAnythingIsWritten — a
// selection crossing something already commented on must not half-apply.
func TestOverlappingAnExistingCommentIsRefused(t *testing.T) {
	first, err := suggest.CommentOnRange(threePara(), []int{1}, 0, 4, "cm-0000000000000015", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	out, err := suggest.CommentAcross(first, []int{0}, 5, []int{2}, 8, "cm-0000000000000016", "court", when)
	if err == nil {
		t.Fatal("a selection across an existing comment was accepted")
	}
	if len(out.Blocks) != 0 {
		t.Error("a refused call returned a document, which invites a caller to write it")
	}
}

// TestASameBlockRangeGoesThroughTheOnePath — there is one implementation of
// what commenting on a range means, and a degenerate cross-block call reaches
// it rather than duplicating it.
func TestASameBlockRangeGoesThroughTheOnePath(t *testing.T) {
	across, err := suggest.CommentAcross(threePara(), []int{1}, 0, []int{1}, 4, "cm-0000000000000017", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := suggest.CommentOnRange(threePara(), []int{1}, 0, 4, "cm-0000000000000017", "court", when)
	if err != nil {
		t.Fatal(err)
	}
	if !docmodel.Equal(across, direct) {
		t.Error("a same-block CommentAcross produced a different document from CommentOnRange")
	}
}
