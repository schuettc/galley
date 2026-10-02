package review

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

func TestResolveAndReopen(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("k", "H", AuthorCourt, "look at this", at("2026-08-02T10:00:00Z"))

	if err := Wrap(doc).SetResolved("k", true); err != nil {
		t.Fatal(err)
	}
	if !Read(doc)[0].Resolved {
		t.Fatal("thread did not resolve")
	}
	if Read(doc)[0].Open() {
		t.Fatal("a resolved thread must not read as open")
	}
	if err := Wrap(doc).SetResolved("k", false); err != nil {
		t.Fatal(err)
	}
	if Read(doc)[0].Resolved {
		t.Fatal("thread did not reopen")
	}
}

func TestResolveAnUnknownSectionIsAnError(t *testing.T) {
	doc := crdt.New()
	if err := Wrap(doc).SetResolved("nope", true); err == nil {
		t.Fatal("want an error for a section with no thread")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("a", "A", AuthorCourt, "first", at("2026-08-02T10:00:00Z"))
	Wrap(doc).Append("a", "A", AuthorAgent, "replied", at("2026-08-02T10:01:00Z"))
	Wrap(doc).Append("b", "B", AuthorCourt, "second", at("2026-08-02T10:02:00Z"))
	if err := Wrap(doc).SetResolved("a", true); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(Export(doc, "page.html"))
	if err != nil {
		t.Fatal(err)
	}

	restored := crdt.New()
	if err := Import(restored, raw); err != nil {
		t.Fatal(err)
	}

	before, after := Read(doc), Read(restored)
	if len(after) != len(before) {
		t.Fatalf("want %d threads, got %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Key != after[i].Key || before[i].Resolved != after[i].Resolved {
			t.Fatalf("thread %d differs: %+v vs %+v", i, before[i], after[i])
		}
		if len(before[i].Entries) != len(after[i].Entries) {
			t.Fatalf("thread %s lost entries: %d vs %d", before[i].Key, len(before[i].Entries), len(after[i].Entries))
		}
		for j := range before[i].Entries {
			if before[i].Entries[j].Text != after[i].Entries[j].Text {
				t.Fatalf("entry text differs: %q vs %q", before[i].Entries[j].Text, after[i].Entries[j].Text)
			}
			if before[i].Entries[j].Author != after[i].Entries[j].Author {
				t.Fatalf("entry author differs")
			}
		}
	}
}

func TestImportAcceptsThePreThreadingFormat(t *testing.T) {
	// The file that existed before threads did. Nothing typed against the old
	// format may be lost when the format changes underneath it.
	legacy := []byte(`{
	  "page": "pr-diff-design.html",
	  "updated": "2026-08-03T02:50:31Z",
	  "comments": [
	    {"key": "01-shape-two-artifacts", "heading": "01 Shape — two artifacts", "text": "we also need complete CLI support"}
	  ]
	}`)

	doc := crdt.New()
	if err := Import(doc, legacy); err != nil {
		t.Fatal(err)
	}
	threads := Read(doc)
	if len(threads) != 1 {
		t.Fatalf("want 1 thread, got %d", len(threads))
	}
	if threads[0].Comment() != "we also need complete CLI support" {
		t.Fatalf("legacy comment lost: %q", threads[0].Comment())
	}
	if threads[0].Entries[0].Author != AuthorCourt {
		t.Fatal("a legacy comment must be attributed to the reviewer")
	}
}

func TestExportKeepsTheFlatCommentView(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("a", "A", AuthorCourt, "mine", at("2026-08-02T10:00:00Z"))
	Wrap(doc).Append("a", "A", AuthorAgent, "not mine", at("2026-08-02T10:01:00Z"))

	f := Export(doc, "page.html")
	if len(f.Comments) != 1 {
		t.Fatalf("want 1 flat comment, got %d", len(f.Comments))
	}
	if f.Comments[0].Text != "mine" {
		t.Fatalf("the flat view must carry the reviewer's text, got %q", f.Comments[0].Text)
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

// Thread.Region must survive the whole round trip — the document, Export, the
// sidecar, Import — or a region pin is deleted the first time anyone runs
// `galley reply`. review.File is a SHARED schema and this is the third field to
// have to say that out loud: Suggestions was the first, and a writer that did
// not round-trip it erased every suggestion's author.
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

	// And through the sidecar. ExportOnto is what every writer uses, so it is
	// what is checked.
	raw, err := json.Marshal(ExportOnto(doc, "doc.md", File{
		Suggestions: []SuggestionMeta{{ID: "s1", Author: AuthorCourt}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Suggestions) != 1 {
		t.Error("ExportOnto dropped Suggestions again")
	}
	restored := crdt.New()
	if err := Import(restored, raw); err != nil {
		t.Fatal(err)
	}
	for _, tr := range Read(restored) {
		if tr.Key != "cm-fig" {
			continue
		}
		if tr.Region == nil || *tr.Region != (Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}) {
			t.Fatalf("the sidecar round trip lost the region: %+v", tr.Region)
		}
	}
}

// Answered is the one predicate behind ✓ all's sweep and the census hint: the
// ball is in the reviewer's court exactly when the agent spoke last. A thread
// with no entries, or whose last word is the reviewer's, is unanswered.
func TestAnsweredIsWhoSpokeLast(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		th   Thread
		want bool
	}{
		{"no entries", Thread{}, false},
		{"reviewer opened, no reply", Thread{Entries: []Entry{{Author: AuthorCourt, At: now, Text: "why?"}}}, false},
		{"agent answered", Thread{Entries: []Entry{{Author: AuthorCourt, At: now, Text: "why?"}, {Author: AuthorAgent, At: now, Text: "because"}}}, true},
		{"reviewer replied again", Thread{Entries: []Entry{{Author: AuthorCourt, At: now, Text: "why?"}, {Author: AuthorAgent, At: now, Text: "because"}, {Author: AuthorCourt, At: now, Text: "but"}}}, false},
		{"agent opened", Thread{Entries: []Entry{{Author: AuthorAgent, At: now, Text: "note"}}}, true},
	}
	for _, c := range cases {
		if got := c.th.Answered(); got != c.want {
			t.Errorf("%s: Answered() = %v, want %v", c.name, got, c.want)
		}
	}
}

// Outcome must survive the document round trip like Resolved does — it is how
// the rail and the sidecar both show which no was said.
func TestOutcomeRoundTrips(t *testing.T) {
	doc := crdt.New()
	s := Wrap(doc)
	s.Append("cm-x", "heading", AuthorCourt, "no thanks", time.Now())
	if err := s.SetOutcome("cm-x", "declined"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetResolved("cm-x", true); err != nil {
		t.Fatal(err)
	}
	got := Read(doc)
	if len(got) != 1 || got[0].Outcome != "declined" || !got[0].Resolved {
		t.Fatalf("Read = %+v, want one declined resolved thread", got)
	}
	if err := s.SetOutcome("cm-missing", "declined"); !errors.Is(err, ErrNoThread) {
		t.Errorf("missing key: %v, want ErrNoThread", err)
	}
}

// The trail — Changes — is the FOURTH sidecar-held field to need this said out
// loud: like Suggestions, it has no representation in the ygo document, so a
// writer that replays the threads and writes Export's projection over the
// whole file deletes the reviewer's entire trail silently. ExportOnto is the
// carry, and this test is the round trip: File -> JSON -> File -> ExportOnto,
// with the trail intact on the far side. Written red-first, against an
// ExportOnto that did not carry the field.
func TestExportOntoCarriesTheTrail(t *testing.T) {
	// EVERY FIELD IS POPULATED, AND THE TWO NEWEST ONES DELIBERATELY. A fixture
	// that leaves Before and After at their zero value is a guard that cannot
	// fail for them: omitempty suppresses an empty string, so the JSON pins no
	// tag, and a field-wise ExportOnto that dropped both would pass unchanged.
	// A `json:"Before"` typo degrades every reloaded block-emptying entry to a
	// permanent refusal, which is precisely the failure the fields exist to
	// prevent — so it is pinned here, in the round trip, rather than trusted.
	//
	// The three rows are the three states each side has: nil for an entry that
	// is not a block-emptying deletion at all, "" for one whose neighbour is an
	// EMPTY block, and text for an ordinary neighbour. "" and nil are different
	// facts (see Change's own comment) and the sidecar has to keep them apart.
	//
	// PLACED IS THE SAME GUARD FOR THE SAME REASON, one field newer. It is a
	// *bool with three states — true, false, and absent for a legacy row — and
	// the middle one is the one omitempty could eat: `json:"placed,omitempty"`
	// on a plain bool would drop false, and every entry that had no place would
	// reload claiming it had one, which is an ORDER the browser would then
	// resolve two block-emptying deletions by. Pinned on the bytes below.
	above := "the line above"
	empty := ""
	yes := true
	no := false
	prior := File{
		Changes: []Change{
			{Old: "brown", New: "red", BlockKey: "bk-1", Prefix: "the quick ", Suffix: " fox", Placed: &yes, At: at("2026-08-15T10:00:00Z")},
			{Old: "entirely gone", Before: &above, After: &empty, Placed: &no, At: at("2026-08-15T10:01:00Z")},
			{Old: "the last line", Before: &above, At: at("2026-08-15T10:02:00Z")},
		},
	}
	// Through JSON first — the shape a sidecar on disk actually takes — so a
	// tag typo or a dropped field fails here and not in a browser.
	raw, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	// The TAGS themselves, read off the bytes: lowercase, and an empty string
	// present rather than omitted.
	for _, want := range []string{`"before":"the line above"`, `"after":""`, `"placed":true`, `"placed":false`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("the sidecar JSON does not carry %s: %s", want, raw)
		}
	}
	if strings.Count(string(raw), `"after"`) != 1 {
		t.Fatalf("a nil side must be omitted, not written: %s", raw)
	}
	if strings.Count(string(raw), `"placed"`) != 2 {
		t.Fatalf("an unrecorded placed must be omitted, and a false one written: %s", raw)
	}
	var loaded File
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Changes) != 3 || loaded.Changes[0].New != "red" || loaded.Changes[1].Old != "entirely gone" {
		t.Fatalf("the trail did not survive JSON: %+v", loaded.Changes)
	}
	if loaded.Changes[0].Before != nil || loaded.Changes[0].After != nil {
		t.Fatalf("an entry with no neighbour evidence came back with some: %+v", loaded.Changes[0])
	}
	if loaded.Changes[1].Before == nil || *loaded.Changes[1].Before != above ||
		loaded.Changes[1].After == nil || *loaded.Changes[1].After != "" {
		t.Fatalf("the empty neighbour did not survive JSON: %+v", loaded.Changes[1])
	}
	if loaded.Changes[2].After != nil {
		t.Fatalf("a block at the document's end came back with a neighbour: %+v", loaded.Changes[2])
	}
	// THREE STATES BACK OUT AGAIN. The false one is the row that proves the
	// pointer is doing its job: a plain bool would have come back true-by-
	// omission or, worse, indistinguishable from the legacy row below it.
	if loaded.Changes[0].Placed == nil || !*loaded.Changes[0].Placed {
		t.Fatalf("an entry that had a place came back without one: %+v", loaded.Changes[0])
	}
	if loaded.Changes[1].Placed == nil || *loaded.Changes[1].Placed {
		t.Fatalf("an entry that had NO place came back claiming one: %+v", loaded.Changes[1])
	}
	if loaded.Changes[2].Placed != nil {
		t.Fatalf("a legacy row must stay unrecorded, never guessed: %+v", loaded.Changes[2])
	}

	doc := crdt.New()
	Wrap(doc).Append("cm-x", "heading", AuthorCourt, "a thread, so the projection is not vacuous", at("2026-08-15T10:03:00Z"))
	got := ExportOnto(doc, "doc.md", loaded)
	if len(got.Changes) != 3 {
		t.Fatalf("ExportOnto dropped the trail: %+v", got.Changes)
	}
	// By VALUE, not by ==: these fields are pointers now, and pointer equality
	// would report "carried" for a copy that kept the addresses and lost the
	// strings — and "mangled" for one that carried the strings honestly through
	// new pointers. What is being asserted is the record, not its address.
	if !reflect.DeepEqual(got.Changes, prior.Changes) {
		t.Fatalf("ExportOnto mangled the trail: %+v", got.Changes)
	}
	// And Export alone must NOT invent one — the document does not hold it.
	if plain := Export(doc, "doc.md"); len(plain.Changes) != 0 {
		t.Fatalf("Export reported a trail the document cannot hold: %+v", plain.Changes)
	}
}

// A trail entry is STORED as its minimal diff and SHOWN as a word. The storage
// half is load-bearing (the browser's undo retraction pairs entry against undo
// only when both are minimal); the display half is what a human can read —
// "could" edited to "should" is on disk as c → sh, and a listing that prints
// that says nothing. Expanded is the display half, and the human `galley
// pending` listing is its only caller: --json still emits the stored entry,
// because a machine wants the canonical one.
//
// Written red-first against a Change with no Expanded at all.
func TestChangeExpandedShowsTheWord(t *testing.T) {
	cases := []struct {
		name          string
		in            Change
		wantOld, want string
	}{{
		name:    "the reviewer's own case: could -> should, stored c -> sh",
		in:      Change{Old: "c", New: "sh", Prefix: "it ", Suffix: "ould be fine"},
		wantOld: "could", want: "should",
	}, {
		name:    "an affix on the left rejoins too: teh -> the",
		in:      Change{Old: "eh", New: "he", Prefix: "and t", Suffix: " rest"},
		wantOld: "teh", want: "the",
	}, {
		name:    "a pure insertion inside a word shows the whole word on both sides",
		in:      Change{Old: "", New: "o", Prefix: "the w", Suffix: "rd here"},
		wantOld: "wrd", want: "word",
	}, {
		name:    "an entry already at word boundaries expands to itself",
		in:      Change{Old: "brown", New: "blue", Prefix: "The quick ", Suffix: " fox"},
		wantOld: "brown", want: "blue",
	}, {
		name:    "an adrift entry — no context at all — passes through as stored",
		in:      Change{Old: "entirely gone", New: ""},
		wantOld: "entirely gone", want: "",
	}, {
		// Go's strings are UTF-8 and the walk is rune-wise, so a multi-byte
		// rune cannot be cut in half — the browser half needs an explicit
		// surrogate guard for the same walk. Pinned on both sides anyway.
		name:    "a multi-byte rune travels whole",
		in:      Change{Old: "", New: "s", Prefix: "a re\U0001F600d", Suffix: " word"},
		wantOld: "re\U0001F600d", want: "re\U0001F600ds",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored := tc.in
			old, next := tc.in.Expanded()
			if old != tc.wantOld || next != tc.want {
				t.Fatalf("Expanded() = %q -> %q, want %q -> %q", old, next, tc.wantOld, tc.want)
			}
			// And the entry itself is untouched: display never rewrites storage.
			if tc.in != stored {
				t.Fatalf("Expanded() rewrote the stored entry: %+v, want %+v", tc.in, stored)
			}
		})
	}
}
