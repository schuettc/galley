package suggest

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

var (
	tCourt = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	tAlice = time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	tBob   = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
)

func del(text, author string, at time.Time) docmodel.Inline {
	return docmodel.Inline{Text: text, Marks: []docmodel.Mark{
		{Kind: docmodel.Del, Attrs: map[string]string{"author": author, "at": at.Format(time.RFC3339)}},
	}}
}

func ins(text, author string, at time.Time) docmodel.Inline {
	return docmodel.Inline{Text: text, Marks: []docmodel.Mark{
		{Kind: docmodel.Ins, Attrs: map[string]string{"author": author, "at": at.Format(time.RFC3339)}},
	}}
}

func highlight(text, author string, at time.Time) docmodel.Inline {
	return docmodel.Inline{Text: text, Marks: []docmodel.Mark{
		{Kind: docmodel.Highlight, Attrs: map[string]string{"author": author, "at": at.Format(time.RFC3339)}},
	}}
}

func plain(text string) docmodel.Inline {
	return docmodel.Inline{Text: text}
}

func delMark(author string, at time.Time) docmodel.Mark {
	return docmodel.Mark{Kind: docmodel.Del, Attrs: map[string]string{"author": author, "at": at.Format(time.RFC3339)}}
}

// multiMark builds an inline carrying more than one mark at once — the
// shape that exercises overlapping spans of different kinds on the same
// inline (e.g. a comment on a proposed deletion).
func multiMark(text string, marks ...docmodel.Mark) docmodel.Inline {
	return docmodel.Inline{Text: text, Marks: marks}
}

func para(inlines ...docmodel.Inline) docmodel.Block {
	return docmodel.Block{Kind: docmodel.Paragraph, Inlines: inlines}
}

func doc(blocks ...docmodel.Block) docmodel.Doc {
	return docmodel.Doc{Blocks: blocks}
}

// --- List: ordering and IDs ---

func TestList_OrderingAndIDs(t *testing.T) {
	d := doc(
		para(plain("See "), highlight("note", "alice", tAlice), plain(" here.")),
		para(
			del("old", "court", tCourt),
			ins("new", "court", tCourt),
			plain(" and "),
			ins("more", "bob", tBob),
		),
		para(highlight("second note", "alice", tAlice)),
	)

	// The Del and the Ins in block 1 are a SUBSTITUTION — the file writes
	// them as one "{~~old~>new~~}" — so they are ONE entry taking one id,
	// not two consecutive ones (see substitution_test.go). Bob's unpaired
	// Ins after them still gets its own.
	got := List(d)
	wantIDs := []string{"c1", "s1", "s2", "c2"}
	if len(got) != len(wantIDs) {
		t.Fatalf("want %d pending, got %d: %+v", len(wantIDs), len(got), got)
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("entry %d: want id %q, got %q (%+v)", i, want, got[i].ID, got[i])
		}
	}

	wantKinds := []Kind{KindComment, KindReplace, KindInsert, KindComment}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Errorf("entry %d: want kind %q, got %q", i, want, got[i].Kind)
		}
	}

	wantTexts := []string{"note", "old" + replaceSep + "new", "more", "second note"}
	for i, want := range wantTexts {
		if got[i].Text != want {
			t.Errorf("entry %d: want text %q, got %q", i, want, got[i].Text)
		}
	}

	if got[1].Author != "court" || got[2].Author != "bob" {
		t.Errorf("want distinct authors on s1/s2, got %q/%q", got[1].Author, got[2].Author)
	}
	if !got[0].At.Equal(tAlice) {
		t.Errorf("want c1.At == tAlice, got %v", got[0].At)
	}
}

func TestList_ContextIsSerializedNeighborhood(t *testing.T) {
	d := doc(
		para(plain("before")),
		para(ins("mid", "court", tCourt)),
		para(plain("after")),
	)
	got := List(d)
	if len(got) != 1 {
		t.Fatalf("want 1 pending, got %d", len(got))
	}
	want := "before\n\n{++mid++}\n\nafter\n"
	if got[0].Context != want {
		t.Errorf("want context %q, got %q", want, got[0].Context)
	}
}

func TestList_ContextAtDocumentEdges(t *testing.T) {
	d := doc(para(ins("only", "court", tCourt)))
	got := List(d)
	if len(got) != 1 {
		t.Fatalf("want 1 pending, got %d", len(got))
	}
	if got[0].Context != "{++only++}\n" {
		t.Errorf("want context %q, got %q", "{++only++}\n", got[0].Context)
	}
}

// --- Accept / Reject: each kind, independently ---

// substitutionDoc is a Del run straight into an Ins run: the one shape the
// file writes as "{~~old~>new~~}", and therefore ONE suggestion with one
// decision. The insert and delete cases below deliberately do NOT use it —
// a half of a substitution is not an insertion or a deletion, and testing
// those semantics through one is what let this package believe for a while
// that it could decide the halves apart.
func substitutionDoc() docmodel.Doc {
	return doc(para(del("old", "court", tCourt), ins("new", "court", tCourt)))
}

// insertDoc and deleteDoc are the standalone shapes: an Ins with no Del
// before it, and a Del with no Ins after it.
func insertDoc() docmodel.Doc {
	return doc(para(plain("keep "), ins("new", "court", tCourt)))
}

func deleteDoc() docmodel.Doc {
	return doc(para(del("old", "court", tCourt), plain(" keep")))
}

func TestAccept_Insert_DropsMarkKeepsText(t *testing.T) {
	got, err := Accept(insertDoc(), "s1")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	want := doc(para(plain("keep "), plain("new")))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestReject_Insert_RemovesText(t *testing.T) {
	got, err := Reject(insertDoc(), "s1")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	want := doc(para(plain("keep ")))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestAccept_Delete_RemovesText(t *testing.T) {
	got, err := Accept(deleteDoc(), "s1")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	want := doc(para(plain(" keep")))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestReject_Delete_DropsMarkKeepsText(t *testing.T) {
	got, err := Reject(deleteDoc(), "s1")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	want := doc(para(plain("old"), plain(" keep")))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestAcceptReject_Comment_DropsHighlightEitherWay(t *testing.T) {
	d := doc(para(plain("hey "), highlight("note", "alice", tAlice), plain(" more")))
	want := doc(para(plain("hey "), plain("note"), plain(" more")))

	accepted, err := Accept(d, "c1")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if !docmodel.Equal(accepted, want) {
		t.Fatalf("Accept: want %+v, got %+v", want, accepted)
	}

	rejected, err := Reject(d, "c1")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if !docmodel.Equal(rejected, want) {
		t.Fatalf("Reject: want %+v, got %+v", want, rejected)
	}
}

// A SUBSTITUTION IS NOT A PAIR OF DECISIONS, so there is no "either order"
// to converge from. This test used to assert that accepting one half left
// the other pending and that finishing both, in either order, reached the
// same document. Both halves of that were true, and the reachable
// intermediate — "old{++new++}", whose only completion is the word
// "oldnew" — was the bug: two individually reasonable decisions, with the
// incoherent state on disk before the second one. See substitution_test.go
// for the four-row table.
func TestSubstitutionResolvesBothHalvesAtOnce(t *testing.T) {
	pending := List(substitutionDoc())
	if len(pending) != 1 || pending[0].Kind != KindReplace {
		t.Fatalf("want one replace, got %+v", pending)
	}

	accepted, err := Accept(substitutionDoc(), pending[0].ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if want := doc(para(plain("new"))); !docmodel.Equal(accepted, want) {
		t.Fatalf("accept: want %+v, got %+v", want, accepted)
	}
	if left := List(accepted); len(left) != 0 {
		t.Fatalf("accept left a half pending: %+v", left)
	}

	rejected, err := Reject(substitutionDoc(), pending[0].ID)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if want := doc(para(plain("old"))); !docmodel.Equal(rejected, want) {
		t.Fatalf("reject: want %+v, got %+v", want, rejected)
	}
	if left := List(rejected); len(left) != 0 {
		t.Fatalf("reject left a half pending: %+v", left)
	}
}

func TestAccept_UnknownID_Errors(t *testing.T) {
	d := substitutionDoc()
	if _, err := Accept(d, "s99"); err == nil || !strings.Contains(err.Error(), "s99") {
		t.Fatalf("want error naming unknown id s99, got %v", err)
	}
}

func TestReject_UnknownID_Errors(t *testing.T) {
	d := substitutionDoc()
	if _, err := Reject(d, "c7"); err == nil || !strings.Contains(err.Error(), "c7") {
		t.Fatalf("want error naming unknown id c7, got %v", err)
	}
}

func TestCommentOn_ZeroMatches_ErrorsNamingCount(t *testing.T) {
	d := doc(para(plain("The quick brown fox")))
	_, err := CommentOn(d, "slowpoke", "cm-000000000000000a", "court", tCourt)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "0") {
		t.Fatalf("want error naming a count of 0, got %v", err)
	}
}

// --- CommentOn ---

func TestCommentOn_HighlightsAndStampsTheGivenID(t *testing.T) {
	const id = "cm-0123456789abcdef"
	d := doc(para(plain("Hello world")))
	got, err := CommentOn(d, "world", id, "alice", tAlice)
	if err != nil {
		t.Fatalf("CommentOn: %v", err)
	}
	mark := highlight("world", "alice", tAlice)
	mark.Marks[0].Attrs[docmodel.CommentIDAttr] = id
	want := doc(para(plain("Hello "), mark))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
	// The ID is the comment's identity, not the ordinal List reports for the
	// highlight: the ordinal renumbers on the next edit.
	pending := List(got)
	if len(pending) != 1 || pending[0].ID != "c1" || pending[0].CommentID != id {
		t.Fatalf("want one highlight listed as c1 carrying %s, got %+v", id, pending)
	}
}

func TestCommentOn_TwoMatches_Errors(t *testing.T) {
	d := doc(para(plain("world world")))
	if _, err := CommentOn(d, "world", "cm-000000000000000b", "alice", tAlice); err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("want error naming a count of 2, got %v", err)
	}
}

// --- purity: inputs are never mutated ---

func TestOperationsDoNotMutateInput(t *testing.T) {
	d := doc(para(del("old", "court", tCourt), ins("new", "court", tCourt), plain(" extra")))
	original := doc(para(del("old", "court", tCourt), ins("new", "court", tCourt), plain(" extra")))

	// s1 is the substitution — one id covering both marks, so its accept and
	// its reject each mutate two inlines. That is the transform most likely
	// to write through into the caller's slice, which is what this checks.
	if _, err := Accept(d, "s1"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := Reject(d, "s1"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	// "extra" carries no suggestion mark yet, so this exercises CommentOn's
	// mutation path rather than tripping conflictingComment.
	if _, err := CommentOn(d, "extra", "cm-0000000000000020", "court", tCourt); err != nil {
		t.Fatalf("CommentOn: %v", err)
	}

	if !docmodel.Equal(d, original) {
		t.Fatalf("want input untouched, got %+v", d)
	}
}

// --- fix round 1: overlapping spans (different kinds legal, same kind refused) ---

// TestCommentOn_RefusesToStackOntoAnExistingComment: a second Highlight on
// text already under comment would make one of the two authors' threads
// invisible to List the same way a doubled Del mark would.
func TestCommentOn_RefusesToStackOntoAnExistingComment(t *testing.T) {
	d := doc(para(highlight("word", "alice", tAlice)))

	_, err := CommentOn(d, "word", "cm-000000000000000c", "bob", tBob)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Fatalf("want error naming the existing comment's author, got %v", err)
	}
}

// TestReject_LeavesADifferentAuthorsSameKindMarkAlone guards the removal
// half directly: on a document that already has two Del marks on one
// inline (as a hand-built or externally-sourced document could),
// docmodel.Inline.Attr returns only
// the FIRST match — so List surfaces just one of the two as a pending
// suggestion, a pre-existing docmodel limitation this package does not
// attempt to lift. What matters here is that rejecting the one List DID
// surface removes exactly that mark instance and nothing else: bob's Del
// must survive untouched on the same inline, rather than being wiped out
// by a removal that matched on kind alone.
func TestReject_LeavesADifferentAuthorsSameKindMarkAlone(t *testing.T) {
	d := doc(para(multiMark("word", delMark("court", tCourt), delMark("bob", tBob))))

	pending := List(d)
	if len(pending) != 1 || pending[0].Author != "court" {
		t.Fatalf("want List to surface only court's (Attr-first) suggestion, got %+v", pending)
	}

	got, err := Reject(d, pending[0].ID)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	// court's Del is gone (rejecting a delete drops the mark, keeping the
	// text); bob's Del survives on the same inline, untouched.
	want := doc(para(multiMark("word", delMark("bob", tBob))))
	if !docmodel.Equal(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}

	// And bob's suggestion — invisible before, because it sat behind
	// court's in Attr's first-match order — is now independently visible.
	after := List(got)
	if len(after) != 1 || after[0].Author != "bob" {
		t.Fatalf("want bob's suggestion now surfaced, got %+v", after)
	}
}

// A note is the author's own words, not document prose. A suggestion that
// targeted one was applied as an UNMARKED edit — the insertion merged into the
// note's text with no marker, the deletion never performed, and a reparse
// showing nothing pending. It became a legal target the moment docmodel.Note
// arrived, because suggest filters blocks on len(b.Inlines) == 0 and a Note
// HAS inlines.
//
// Later commits added the Kind guards, so this is a regression test for
// something already fixed rather than a new fix — and it is the test the
// branch never wrote. Without it, the guards are two unexplained conditions
// that the next person to touch findUnique deletes.
func TestSuggestCannotTargetANote(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("The build is slow.\n\n{>>this is out of date<<}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommentOn(doc, "out of date", "cm-000000000000000d", "reviewer", time.Now().UTC()); err == nil {
		t.Error("a comment was allowed to target a note's text")
	}
	// And the same words in real prose still work, so the guard is about the
	// block kind and not about the string.
	prose, _, err := markdown.Parse([]byte("The build is out of date.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommentOn(prose, "out of date", "cm-0000000000000021", "reviewer", time.Now().UTC()); err != nil {
		t.Errorf("the guard refused ordinary prose too: %v", err)
	}
}

// A hand-written note's body loses its markdown on the way IN.
//
// goldmark parses the paragraph before critic.go's scanner runs, so cellText
// flattens the body to plain runes: a link's destination is deleted from the
// author's own file. The write side is correct — note.go escapes a body so it
// survives verbatim — but only for words the note was built with.
//
// Not a regression (the same flattening always fed the sidecar), but this
// branch promotes the flattened text to durable FILE content, so a reviewer
// who pastes a link into a note now loses the URL from their document.
//
// Skipped: fixing it means giving the note body its own escaped-source
// extraction rather than reusing cellText, which is a larger change than the
// deletion bugs this task exists for and touches the marker scanner they run
// through. Recorded here rather than left unwritten. See review finding
// "Important 9 — a hand-written note body loses its markdown".
func TestSuggestNoteBodyKeepsItsMarkdown(t *testing.T) {
	t.Skip("Important 9: cellText flattens a note body; fix is out of scope for the deletion repair")

	doc, _, err := markdown.Parse([]byte("{>>see [docs](https://example.com/x)<<}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(markdown.Serialize(doc)); !strings.Contains(got, "https://example.com/x") {
		t.Errorf("the note's link destination was deleted: %q", got)
	}
}
