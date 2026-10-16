package ydoc

// Internal (package ydoc, not ydoc_test) so this can reach readLiveTestHook
// and readLiveMaxAttempts directly — the exhaustion path needs a
// deterministic way to force interference on EVERY attempt, which a test
// that only races real goroutines against each other can't fully guarantee:
// scheduling might get lucky and let one attempt through clean, especially
// with as few as readLiveMaxAttempts (8) of them.

import (
	"errors"
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// TestReadLive_ExhaustsRetriesOnPerpetualInterference is the retry
// -exhaustion half of ReadLive's contract (the racing-writer tests in
// internal/serve cover the "retry actually helps" half). readLiveTestHook
// injects a real write into the exact window between phase 1 and the
// "after" snapshot check, on every single attempt — so ReadLive must run
// out of readLiveMaxAttempts and return ErrConcurrentWrite, with no Doc to
// show for it.
func TestReadLive_ExhaustsRetriesOnPerpetualInterference(t *testing.T) {
	doc := crdt.New()
	Load(doc, func(fn func(*crdt.Transaction)) { doc.Transact(fn) }, docmodel.Doc{
		Blocks: []docmodel.Block{{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "hello"}}}},
	})

	calls := 0
	readLiveTestHook = func(d *crdt.Doc) {
		calls++
		m := d.GetMap("interference")
		d.Transact(func(txn *crdt.Transaction) {
			m.Set(txn, "n", calls)
		})
	}
	defer func() { readLiveTestHook = nil }()

	got, err := ReadLive(doc)
	if !errors.Is(err, ErrConcurrentWrite) {
		t.Fatalf("ReadLive err = %v, want ErrConcurrentWrite", err)
	}
	if len(got.Blocks) != 0 {
		t.Fatalf("ReadLive returned a non-empty Doc alongside ErrConcurrentWrite: %+v", got)
	}
	if calls != readLiveMaxAttempts {
		t.Fatalf("hook ran %d times, want exactly readLiveMaxAttempts (%d) — ReadLive should try that many times, no more, no fewer",
			calls, readLiveMaxAttempts)
	}
}

// TestReadLive_SucceedsOnceInterferenceStops is the companion case: the
// hook interferes for the first few attempts, then goes quiet — ReadLive
// must recover and return the real content, not bail out early.
func TestReadLive_SucceedsOnceInterferenceStops(t *testing.T) {
	doc := crdt.New()
	model := docmodel.Doc{
		Blocks: []docmodel.Block{{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "hello"}}}},
	}
	Load(doc, func(fn func(*crdt.Transaction)) { doc.Transact(fn) }, model)

	calls := 0
	const interfereFor = 3
	readLiveTestHook = func(d *crdt.Doc) {
		calls++
		if calls > interfereFor {
			return
		}
		m := d.GetMap("interference")
		d.Transact(func(txn *crdt.Transaction) {
			m.Set(txn, "n", calls)
		})
	}
	defer func() { readLiveTestHook = nil }()

	got, err := ReadLive(doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if !docmodel.Equal(got, model) {
		t.Fatalf("ReadLive result changed once interference stopped\n got:  %#v\n want: %#v", got, model)
	}
	if calls != interfereFor+1 {
		t.Fatalf("hook ran %d times, want %d (interfereFor+1, the first clean attempt)", calls, interfereFor+1)
	}
}

// TestReadLive_RetriesOnAPureDelete is the delete-only counterpart to the
// two tests above, both of which interfere by inserting (a YMap.Set on a
// map the document's own content never touches). A pure delete — the exact
// shape a backspace produces, and the shape the round-2 fix's own bug was
// about — never advances any client's clock (see ReadLive's doc comment:
// probed directly against ygo v1.43.0's Item.delete/buildDeleteSet), so a
// StateVector-only comparison is structurally blind to it: it would report
// "clean" on an attempt where phase 2 had in fact just read a deleted node
// back as empty. This is what forced the move to crdt.CaptureSnapshot's
// StateVector+DeleteSet pair.
//
// The hook here deletes real text from the live document — not a side map —
// on its first call only, then goes quiet. If the snapshot comparison sees
// deletes, ReadLive retries once and its SECOND attempt (now running against
// the already-deleted, otherwise-quiescent document) succeeds with the
// correct post-delete content. If it does not see deletes (the old bug),
// this fails two ways depending on exactly how the race falls: either the
// hook only runs once (no retry triggered) and the result may reflect a torn
// read, or — as a further check independent of call count — the result
// disagrees with an independent plain Read taken right after.
func TestReadLive_RetriesOnAPureDelete(t *testing.T) {
	doc := crdt.New()
	Load(doc, func(fn func(*crdt.Transaction)) { doc.Transact(fn) }, docmodel.Doc{
		Blocks: []docmodel.Block{{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "hello world"}}}},
	})

	calls := 0
	readLiveTestHook = func(d *crdt.Doc) {
		calls++
		if calls != 1 {
			return
		}
		frag := d.GetXmlFragment(FragmentName)
		el, ok := frag.Children()[0].(*crdt.YXmlElement)
		if !ok {
			t.Fatalf("fixture's first child is not an element")
		}
		txt, ok := el.Children()[0].(*crdt.YXmlText)
		if !ok {
			t.Fatalf("fixture's first run is not text")
		}
		d.Transact(func(txn *crdt.Transaction) {
			txt.Delete(txn, 0, 5) // "hello world" -> " world"; pure delete, no insert
		})
	}
	defer func() { readLiveTestHook = nil }()

	got, err := ReadLive(doc)
	if err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if calls != 2 {
		t.Fatalf("hook ran %d times, want 2 (attempt 1 interferes and gets discarded, attempt 2 is clean) — "+
			"a count of 1 means the delete-only interference was never detected", calls)
	}

	// The result must equal exactly what an independent plain Read sees
	// right now: proof ReadLive's answer reflects a single consistent
	// moment (post-delete), not phase 1's pre-delete structure paired with
	// phase 2's post-delete (torn) text.
	confirm, err := Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(got, confirm) {
		t.Fatalf("ReadLive result inconsistent with a plain Read taken right after\n ReadLive: %#v\n Read:     %#v", got, confirm)
	}
	want := docmodel.Doc{Blocks: []docmodel.Block{{Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: " world"}}}}}
	if !docmodel.Equal(got, want) {
		t.Fatalf("ReadLive result = %#v, want %#v (the delete happened for real and should be reflected)", got, want)
	}
}
