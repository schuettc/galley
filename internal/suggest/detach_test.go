package suggest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

// Detach is the DOCUMENT half of deleting a thread: the trace the conversation
// left in the file. Everything else about a delete is the review map's and
// pending.json's business.
//
// Two block comments on one block are the case that decides whether the
// pairing is real or a coincidence: each thread takes the note carrying ITS ID,
// so deleting one leaves the other's mark exactly where it was.
func TestDetachTakesTheRightOneOfTwoNotesOnOneBlock(t *testing.T) {
	const first, second = "cb-0000000000000001", "cb-0000000000000002"
	d := parseDoc(t, "# Spec\n\nThe build is slow.\n\n{>>@comment "+first+"<<}\n\n{>>@comment "+second+"<<}\n")
	threads := []review.Thread{
		{Key: first, Anchor: string(suggest.AnchorBlock)},
		{Key: second, Anchor: string(suggest.AnchorBlock)},
	}

	out, ok := suggest.Detach(d, threads, second)
	if !ok {
		t.Fatal("Detach found nothing to remove")
	}
	got := string(markdown.Serialize(out))
	if !strings.Contains(got, first) || strings.Contains(got, second) {
		t.Errorf("want only %s left, got %q", first, got)
	}
	if !strings.Contains(got, "The build is slow.") {
		t.Errorf("Detach took the prose with it: %q", got)
	}
}

// A document comment has no mark in the file, so deleting one changes nothing
// in it: not even a word note that says the same thing.
func TestDetachingADocumentCommentChangesNothing(t *testing.T) {
	d := parseDoc(t, "# Spec\n\n{>>@document look at this<<}\n")
	threads := []review.Thread{{Key: "cd-0000000000000001", Anchor: string(suggest.AnchorDocument),
		Entries: []review.Entry{{Author: review.AuthorCourt, Text: "look at this"}}}}
	out, ok := suggest.Detach(d, threads, "cd-0000000000000001")
	if ok {
		t.Error("Detach claimed to remove a mark a document comment does not have")
	}
	if got := string(markdown.Serialize(out)); !strings.Contains(got, "look at this") {
		t.Errorf("Detach removed a note by its words: %q", got)
	}
}

// A range thread's trace is its Highlight mark, and lifting it must not touch
// the words it covers.
func TestDetachLiftsARangeCommentsHighlight(t *testing.T) {
	at := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	d := parseDoc(t, "A picture and an age.\n")
	d, err := suggest.CommentOn(d, "age", "cm-0000000000000019", review.AuthorCourt, at)
	if err != nil {
		t.Fatal(err)
	}
	threads := []review.Thread{{
		Key:     "cm-0000000000000019",
		Heading: "age",
		Entries: []review.Entry{{Author: review.AuthorCourt, At: at, Text: "which age?"}},
	}}

	out, ok := suggest.Detach(d, threads, "cm-0000000000000019")
	if !ok {
		t.Fatal("Detach did not find the highlight")
	}
	got := string(markdown.Serialize(out))
	if strings.Contains(got, "{==") {
		t.Errorf("the highlight survived: %q", got)
	}
	if !strings.Contains(got, "A picture and an age.") {
		t.Errorf("Detach took the prose with it: %q", got)
	}
}

// Two highlights with the same text, author and instant are two comments, and
// the ID alone tells them apart, in both directions.
func TestPairForPairsByIDWhenTextsCollide(t *testing.T) {
	at := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	pending := []suggest.Pending{
		{Run: "run-a", Kind: suggest.KindComment, Anchor: suggest.AnchorRange,
			Author: review.AuthorCourt, At: at, Text: "old", CommentID: idA},
		{Run: "run-b", Kind: suggest.KindComment, Anchor: suggest.AnchorRange,
			Author: review.AuthorCourt, At: at, Text: "old", CommentID: idB},
	}
	for key, run := range map[string]string{idA: "run-a", idB: "run-b"} {
		p, ok := suggest.PairFor(pending, review.Thread{Key: key, Heading: "old"})
		if !ok || p.Run != run {
			t.Errorf("%s paired (%q, %v), want (%q, true)", key, p.Run, ok, run)
		}
	}
}

const (
	idA = "cm-aaaaaaaaaaaaaaaa"
	idB = "cm-bbbbbbbbbbbbbbbb"
)

// ONE COMMENT, EVERY PIECE. A selection across paragraphs, or around a code
// span, is several highlights that carry one ID, and deleting the comment
// lifts all of them, leaving a neighbour's alone.
func TestDetachLiftsEveryPieceOfOneComment(t *testing.T) {
	d := parseDoc(t, "Start {==one==}{>>@comment "+idA+"<<}\n\n"+
		"{==two==}{>>@comment "+idA+"<<} `code` {==three==}{>>@comment "+idA+"<<} and "+
		"{==keep==}{>>@comment "+idB+"<<} end.\n")
	threads := []review.Thread{
		{Key: idA, Heading: "one two", Entries: []review.Entry{{Author: review.AuthorCourt, Text: "cut"}}},
		{Key: idB, Heading: "keep", Entries: []review.Entry{{Author: review.AuthorCourt, Text: "keep"}}},
	}

	out, ok := suggest.Detach(d, threads, idA)
	if !ok {
		t.Fatal("Detach found no piece of the comment")
	}
	got := string(markdown.Serialize(out))
	if strings.Contains(got, idA) {
		t.Errorf("a piece of the deleted comment survived: %q", got)
	}
	if !strings.Contains(got, "{==keep==}{>>@comment "+idB+"<<}") {
		t.Errorf("the neighbouring comment was lifted too: %q", got)
	}
	for _, w := range []string{"Start one", "two `code` three and"} {
		if !strings.Contains(got, w) {
			t.Errorf("Detach took the prose with it, want %q in %q", w, got)
		}
	}
}

// PAIRING IS THE ID, NEVER THE WORDS. Two highlights with identical text by
// one author are two comments, and each thread finds its own.
func TestPairForIgnoresTextAndUsesTheID(t *testing.T) {
	d := parseDoc(t, "{==same words==}{>>@comment "+idA+"<<} and then "+
		"{==same words==}{>>@comment "+idB+"<<}.\n")
	pending := suggest.List(d)
	if len(pending) != 2 || pending[0].Run == pending[1].Run {
		t.Fatalf("fixture: want two comment spans with two runs, got %+v", pending)
	}
	for i, key := range []string{idA, idB} {
		th := review.Thread{Key: key, Heading: "same words",
			Entries: []review.Entry{{Author: review.AuthorCourt, Text: "why?"}}}
		p, ok := suggest.PairFor(pending, th)
		if !ok {
			t.Errorf("%s did not pair", key)
			continue
		}
		if p.Run != pending[i].Run {
			t.Errorf("%s paired run %q, want %q — the other comment's", key, p.Run, pending[i].Run)
		}
	}
	// The words alone pair nothing: a thread whose key is no mark's ID is
	// unplaced, whatever its heading says.
	if _, ok := suggest.PairFor(pending, review.Thread{Key: "cm-cccccccccccccccc", Heading: "same words"}); ok {
		t.Error("a thread paired on its heading")
	}
}

// A thread whose trace is already gone — the note deleted by hand, the
// highlighted text edited away — reports NOT FOUND rather than removing
// something adjacent. The caller still drops the thread; nothing else moves.
func TestDetachReportsNothingRatherThanGuessing(t *testing.T) {
	d := parseDoc(t, "# Spec\n\nThe build is slow.\n\n{>>a different note<<}\n")
	threads := []review.Thread{{
		Key: "cd-gone", Anchor: string(suggest.AnchorDocument),
		Entries: []review.Entry{{Author: review.AuthorCourt, Text: "the note that is gone"}},
	}}
	out, ok := suggest.Detach(d, threads, "cd-gone")
	if ok {
		t.Error("Detach claimed to have removed a trace that is not there")
	}
	if got := string(markdown.Serialize(out)); !strings.Contains(got, "a different note") {
		t.Errorf("Detach removed the wrong note: %q", got)
	}
}
