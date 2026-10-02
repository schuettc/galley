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
// left in the file. Everything else about a delete is the sidecar's business.
//
// Two identical notes on one anchor are the case that decides whether the
// pairing is real or a coincidence — each thread has to take ITS OWN note, so
// deleting one leaves the other's words exactly where they were.
func TestDetachTakesTheRightOneOfTwoIdenticalNotes(t *testing.T) {
	d := parseDoc(t, "# Spec\n\nThe build is slow.\n\n{>>look at this<<}\n\n{>>look at this<<}\n")
	notes := suggest.Notes(d)
	if len(notes) != 2 {
		t.Fatalf("fixture has %d notes, want 2", len(notes))
	}
	at := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	threads := []review.Thread{
		suggest.NewNoteThread(notes[0], review.AuthorCourt, at),
		suggest.NewNoteThread(notes[1], review.AuthorCourt, at),
	}
	if threads[0].Key == threads[1].Key {
		t.Fatal("two identical notes were given one key — they are two conversations")
	}

	out, ok := suggest.Detach(d, threads, threads[1].Key)
	if !ok {
		t.Fatal("Detach found nothing to remove")
	}
	got := string(markdown.Serialize(out))
	if strings.Count(got, "look at this") != 1 {
		t.Errorf("want exactly one note left, got %q", got)
	}
	if !strings.Contains(got, "The build is slow.") {
		t.Errorf("Detach took the prose with it: %q", got)
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

// Reword is the DOCUMENT half of editing an instruction, and the two identical
// notes are here for the reason they are in the delete test above: the pairing
// has to be real, so rewording one may not touch the other's words.
func TestRewordChangesOneNoteAndLeavesItsTwinAlone(t *testing.T) {
	d := parseDoc(t, "# Spec\n\nThe build is slow.\n\n{>>look at this<<}\n\n{>>look at this<<}\n")
	notes := suggest.Notes(d)
	if len(notes) != 2 {
		t.Fatalf("fixture has %d notes, want 2", len(notes))
	}
	at := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	threads := []review.Thread{
		suggest.NewNoteThread(notes[0], review.AuthorCourt, at),
		suggest.NewNoteThread(notes[1], review.AuthorCourt, at),
	}

	out, ok := suggest.Reword(d, threads, threads[1].Key, "say which build")
	if !ok {
		t.Fatal("Reword found no note to rewrite")
	}
	got := string(markdown.Serialize(out))
	if strings.Count(got, "look at this") != 1 {
		t.Errorf("the twin was rewritten too: %q", got)
	}
	if !strings.Contains(got, "{>>say which build<<}") {
		t.Errorf("the new words never reached the file: %q", got)
	}
	if !strings.Contains(got, "The build is slow.") {
		t.Errorf("Reword took the prose with it: %q", got)
	}
}

// A RANGE INSTRUCTION HAS NO WORDS IN THE DOCUMENT, so there is nothing here to
// reword and the honest answer is to change nothing and say so. The words are
// the sidecar's; the file holds only a Highlight over prose the reviewer did
// not write, and rewriting THAT would rewrite the author's own sentence.
func TestRewordLeavesARangeInstructionsProseAlone(t *testing.T) {
	at := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	d := parseDoc(t, "A picture and an age.\n")
	d, err := suggest.CommentOn(d, "age", "cm-000000000000001a", review.AuthorCourt, at)
	if err != nil {
		t.Fatal(err)
	}
	threads := []review.Thread{{
		Key:     "c-range",
		Heading: "age",
		Entries: []review.Entry{{Author: review.AuthorCourt, At: at, Text: "which age?"}},
	}}
	before := string(markdown.Serialize(d))
	out, ok := suggest.Reword(d, threads, "c-range", "which age exactly?")
	if ok {
		t.Error("Reword claimed to have rewritten a document that holds no note")
	}
	if got := string(markdown.Serialize(out)); got != before {
		t.Errorf("Reword changed the file anyway:\n got %q\nwant %q", got, before)
	}
}
