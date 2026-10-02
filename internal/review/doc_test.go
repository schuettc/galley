package review

import (
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAppendCreatesTheThread(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("01-shape", "01 Shape", AuthorCourt, "the split is right", at("2026-08-02T10:00:00Z"))

	threads := Read(doc)
	if len(threads) != 1 {
		t.Fatalf("want 1 thread, got %d", len(threads))
	}
	got := threads[0]
	if got.Key != "01-shape" || got.Heading != "01 Shape" {
		t.Fatalf("thread identity wrong: %+v", got)
	}
	if got.Resolved {
		t.Fatal("a new thread must start open")
	}
	if got.Comment() != "the split is right" {
		t.Fatalf("comment lost: %q", got.Comment())
	}
}

func TestRepliesAppendInOrderAndDoNotBecomeTheComment(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("k", "H", AuthorCourt, "why two artifacts?", at("2026-08-02T10:00:00Z"))
	Wrap(doc).Append("k", "H", AuthorAgent, "because the binary must stand alone", at("2026-08-02T10:01:00Z"))
	Wrap(doc).Append("k", "H", AuthorCourt, "fair", at("2026-08-02T10:02:00Z"))

	got := Read(doc)[0]
	if len(got.Entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got.Entries))
	}
	if got.Entries[1].Author != AuthorAgent {
		t.Fatalf("entries out of order: %+v", got.Entries)
	}
	// Comment() must stay the reviewer's FIRST word, not their latest, or a
	// thread's identity would drift every time they follow up.
	if got.Comment() != "why two artifacts?" {
		t.Fatalf("comment drifted to %q", got.Comment())
	}
}

func TestReadIsOrderedByKey(t *testing.T) {
	doc := crdt.New()
	for _, k := range []string{"c", "a", "b"} {
		Wrap(doc).Append(k, k, AuthorCourt, "x", at("2026-08-02T10:00:00Z"))
	}
	got := Read(doc)
	if got[0].Key != "a" || got[1].Key != "b" || got[2].Key != "c" {
		t.Fatalf("unstable order: %v", []string{got[0].Key, got[1].Key, got[2].Key})
	}
}

func TestSetCommentCreatesThenEditsInPlace(t *testing.T) {
	doc := crdt.New()
	s := Wrap(doc)
	when := at("2026-08-02T10:00:00Z")

	s.SetComment("k", "H", "the split is", when)
	if got := Read(doc)[0].Comment(); got != "the split is" {
		t.Fatalf("first write: %q", got)
	}

	// Typing more must extend the same entry, not create a second one.
	s.SetComment("k", "H", "the split is right", when)
	got := Read(doc)[0]
	if len(got.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got.Entries))
	}
	if got.Comment() != "the split is right" {
		t.Fatalf("edit lost: %q", got.Comment())
	}

	// And deleting back down must work too.
	s.SetComment("k", "H", "the split", when)
	if got := Read(doc)[0].Comment(); got != "the split" {
		t.Fatalf("shrink lost: %q", got)
	}
}

func TestSetCommentHandlesMultibyteText(t *testing.T) {
	// Y.Text addresses characters, Go strings address bytes. A comment with an
	// accent or an emoji is where that difference corrupts text, so establish
	// the behaviour rather than assume it.
	doc := crdt.New()
	s := Wrap(doc)
	when := at("2026-08-02T10:00:00Z")

	for _, step := range []string{
		"café",
		"café ☕",
		"café ☕ está",
		"café ☕ está bien",
		"café ☕ bien",
		"café bien",
	} {
		s.SetComment("k", "H", step, when)
		if got := Read(doc)[0].Comment(); got != step {
			t.Fatalf("multibyte edit corrupted: want %q, got %q", step, got)
		}
	}
}

func TestSetCommentDoesNotDisturbReplies(t *testing.T) {
	doc := crdt.New()
	s := Wrap(doc)
	when := at("2026-08-02T10:00:00Z")

	s.SetComment("k", "H", "why?", when)
	s.Append("k", "H", AuthorAgent, "because", when)
	s.SetComment("k", "H", "why two artifacts?", when)

	got := Read(doc)[0]
	if len(got.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got.Entries))
	}
	if got.Comment() != "why two artifacts?" {
		t.Fatalf("comment: %q", got.Comment())
	}
	if got.Entries[1].Text != "because" {
		t.Fatalf("reply disturbed: %q", got.Entries[1].Text)
	}
}

func TestSetCommentHandlesSurrogatePairs(t *testing.T) {
	// ygo's YText addresses UTF-16 code units, so an emoji outside the Basic
	// Multilingual Plane occupies two indices while being one rune. Rune-indexed
	// edits cut the pair in half and leave replacement characters. Established by
	// probe; pinned here so it stays fixed.
	doc := crdt.New()
	s := Wrap(doc)
	when := at("2026-08-02T10:00:00Z")

	for _, step := range []string{
		"ship 😀",
		"ship 😀 it",
		"ship 😀🎉 it",
		"ship 🎉 it",
		"ship it",
		"😀",
		"",
	} {
		s.SetComment("k", "H", step, when)
		if got := Read(doc)[0].Comment(); got != step {
			t.Fatalf("surrogate pair corrupted: want %q, got %q", step, got)
		}
	}
}

// A fraction outside [0,1] is not a region on this figure. Storing one would
// draw a pin off the picture, where nothing can click it to resolve it — and a
// pin nobody can reach is a thread nobody can settle.
func TestRegionRejectsFractionsOutsideTheBox(t *testing.T) {
	for _, bad := range []Region{
		{X: -0.1, Y: 0, W: 0.5, H: 0.5},
		{X: 0, Y: -0.1, W: 0.5, H: 0.5},
		{X: 0, Y: 0, W: 1.5, H: 0.5},
		{X: 0.9, Y: 0, W: 0.5, H: 0.5}, // runs off the right edge
		{X: 0, Y: 0.9, W: 0.5, H: 0.5}, // runs off the bottom edge
		{X: 0, Y: 0, W: 0, H: 0.5},     // zero-width
		{X: 0, Y: 0, W: 0.5, H: 0},     // zero-height
	} {
		if err := bad.Valid(); err == nil {
			t.Errorf("Valid() accepted %+v", bad)
		}
	}
	// The whole figure is a legal region, and so is a sliver.
	for _, good := range []Region{
		{X: 0.1, Y: 0.1, W: 0.8, H: 0.8},
		{X: 0, Y: 0, W: 1, H: 1},
	} {
		if err := good.Valid(); err != nil {
			t.Errorf("Valid() refused %+v: %v", good, err)
		}
	}
}

// Thread.Region must survive the document round trip. Its durable half, the
// unsent round, is internal/unsent's (FromThreads/ToThreads).
func TestARegionSurvivesTheRoundTrip(t *testing.T) {
	doc := crdt.New()
	s := Wrap(doc)
	s.Append("cm-fig", "a figure", AuthorCourt, "this axis is unlabelled", at("2026-08-07T10:00:00Z"))
	s.SetAnchor("cm-fig", "block", "image")
	s.SetRegion("cm-fig", &Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4})
	// A thread with NO region must stay that way: a nil region is the ordinary
	// block note, and inventing a zero rectangle for it would draw a pin at the
	// figure's top-left corner on every figure ever commented on.
	s.Append("cm-plain", "a paragraph", AuthorCourt, "reads oddly", at("2026-08-07T10:01:00Z"))

	got := Read(doc)
	if len(got) != 2 {
		t.Fatalf("want 2 threads, got %d", len(got))
	}
	byKey := map[string]Thread{}
	for _, tr := range got {
		byKey[tr.Key] = tr
	}
	if r := byKey["cm-fig"].Region; r == nil || *r != (Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}) {
		t.Fatalf("the document lost the region: %+v", byKey["cm-fig"].Region)
	}
	if byKey["cm-plain"].Region != nil {
		t.Errorf("a thread with no region grew one: %+v", byKey["cm-plain"].Region)
	}
}
