package review

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/ondisk"
)

// oldSidecar is `<doc>.comments.json` AS AN OLDER GALLEY WROTE IT — literal
// bytes, no `v`, carrying one of every shape the file holds. A struct
// re-marshalled by this build would prove only that this build agrees with
// itself.
const oldSidecar = `{
  "page": "/tmp/doc.md",
  "updated": "2026-08-20T10:00:00Z",
  "threads": [
    {
      "key": "md-0-9",
      "heading": "The opening",
      "resolved": false,
      "outcome": "",
      "entries": [
        {"author": "court", "at": "2026-08-20T09:59:00Z", "text": "tighten this"},
        {"author": "agent", "at": "2026-08-20T10:00:00Z", "text": "done"}
      ],
      "anchor": "block",
      "anchorKey": "b-1",
      "blockKind": "paragraph",
      "region": {"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.4}
    }
  ],
  "comments": [{"key": "md-0-9", "heading": "The opening", "text": "tighten this"}],
  "suggestions": [{"id": "s1", "kind": "insert", "author": "agent", "at": "2026-08-20T09:58:00Z", "quote": "brave"}],
  "changes": [{"old": "c", "new": "sh", "blockKey": "b-1", "prefix": "we ", "suffix": "ould", "proposal": "agent", "placed": true, "at": "2026-08-20T09:57:00Z"}]
}
`

// THE BACKWARDS COMPATIBILITY PROMISE FOR THE SIDECAR. Every field of the old
// shape survives the decode, and the replay — the write path — accepts it. An
// existing `.galley` directory in the wild must keep working, and the sidecar
// is the one file where "keep working" and "do not silently erase" are the same
// sentence.
func TestASidecarWrittenBeforeVersionsLoadsAndReplays(t *testing.T) {
	var f File
	if err := json.Unmarshal([]byte(oldSidecar), &f); err != nil {
		t.Fatal(err)
	}
	if f.V != 0 {
		t.Fatalf("V = %d, want 0 — the absent field must not be invented on read", f.V)
	}
	if err := f.Compatible(); err != nil {
		t.Fatalf("an older sidecar must be replayable: %v", err)
	}
	if len(f.Threads) != 1 || len(f.Threads[0].Entries) != 2 {
		t.Fatalf("threads = %+v", f.Threads)
	}
	tr := f.Threads[0]
	if tr.Key != "md-0-9" || tr.Heading != "The opening" || tr.Anchor != "block" ||
		tr.BlockKind != "paragraph" {
		t.Fatalf("thread = %+v", tr)
	}
	if tr.Region == nil || tr.Region.X != 0.1 || tr.Region.H != 0.4 {
		t.Fatalf("region = %+v — a rectangle rides in the sidecar and nowhere else", tr.Region)
	}
	if len(f.Suggestions) != 1 || f.Suggestions[0].Author != "agent" {
		t.Fatalf("suggestions = %+v — the only record of who suggested what", f.Suggestions)
	}
	if len(f.Changes) != 1 {
		t.Fatalf("changes = %+v — the trail has no other home", f.Changes)
	}
	ch := f.Changes[0]
	if ch.Proposal == nil || *ch.Proposal != "agent" {
		t.Fatalf("proposal = %v — a pointer because absent and empty are different facts", ch.Proposal)
	}
	if ch.Placed == nil || !*ch.Placed {
		t.Fatalf("placed = %v — nil is a legacy row and must stay distinguishable from false", ch.Placed)
	}

	// And the replay itself, which is what a mutation runs before it projects.
	doc := crdt.New()
	if err := Import(doc, []byte(oldSidecar)); err != nil {
		t.Fatalf("Import refused an older sidecar — that is a review nobody can edit again: %v", err)
	}
	got := Read(doc)
	if len(got) != 1 || len(got[0].Entries) != 2 || got[0].BlockKind != "paragraph" {
		t.Fatalf("replayed threads = %+v", got)
	}
}

// READING IS ALWAYS ALLOWED. `galley pending` and loadReview decode this same
// type off a running server's wire, and a strict reader would refuse to talk to
// a galley one commit ahead of it — for a file it is not about to touch.
func TestANewerSidecarStillReadsEveryFieldThisBuildKnows(t *testing.T) {
	raw := `{"v":99,"page":"/tmp/doc.md","updated":"2026-08-20T10:00:00Z",
	  "threads":[{"key":"k","heading":"h","resolved":false,"entries":[{"author":"court","at":"2026-08-20T10:00:00Z","text":"hi"}]}],
	  "comments":[],"mood":"cheerful"}`
	var f File
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("a newer sidecar must still be readable: %v", err)
	}
	if len(f.Threads) != 1 || f.Threads[0].Entries[0].Text != "hi" {
		t.Fatalf("threads = %+v", f.Threads)
	}
	if f.V != 99 {
		t.Fatalf("V = %d", f.V)
	}
}

// REPLAYING IS THE WRITE PATH, AND IT REFUSES. ImportFile's own comment says a
// field the replay skips is deleted on the next projection — so a sidecar full
// of fields this build cannot replay is not a read failure, it is an erasure
// waiting for the next save.
func TestANewerSidecarIsRefusedByTheReplay(t *testing.T) {
	raw := `{"v":99,"page":"/tmp/doc.md","updated":"2026-08-20T10:00:00Z",
	  "threads":[{"key":"k","heading":"h","entries":[]}],"comments":[]}`
	doc := crdt.New()
	err := Import(doc, []byte(raw))
	if err == nil {
		t.Fatal("the replay believed a sidecar it cannot round-trip; the next projection would delete the rest")
	}
	if !errors.Is(err, ondisk.ErrFuture) {
		t.Fatalf("err = %v, want the ErrFuture sentinel so a caller can branch on the condition", err)
	}
	for _, want := range []string{"v99", "upgrade galley", "drop every field"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q is missing %q — a working artefact's reader must be told what to do", err, want)
		}
	}
	if len(Read(doc)) != 0 {
		t.Fatal("the refusal must happen before anything is written into the document")
	}
}

// Every projection this build writes names the generation that wrote it,
// including one built by ExportOnto over a prior file that had none.
func TestEveryProjectionNamesItsGeneration(t *testing.T) {
	doc := crdt.New()
	Wrap(doc).Append("k", "h", AuthorCourt, "hi", time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	if got := Export(doc, "/tmp/doc.md").V; got != Version {
		t.Fatalf("Export V = %d, want %d", got, Version)
	}
	onto := ExportOnto(doc, "/tmp/doc.md", File{V: 0, Suggestions: []SuggestionMeta{{ID: "s1"}}})
	if onto.V != Version {
		t.Fatalf("ExportOnto V = %d, want %d — the writer names itself, not the file it read", onto.V, Version)
	}
	if len(onto.Suggestions) != 1 {
		t.Fatal("ExportOnto must still carry what the document does not hold")
	}
}

// THE KEY SET IS THE CONTRACT. The sidecar tolerates unknown keys on purpose,
// so a rename is invisible to every reader; this list is what goes red on the
// commit that causes one. It covers every struct the file is made of, because
// ImportFile deletes what it does not replay and a renamed key inside a Thread
// is as final as one at the top level.
func TestTheSidecarKeysAreTheContract(t *testing.T) {
	yes := true
	s := "agent"
	for _, tc := range []struct {
		name string
		val  any
		want string
	}{
		{"File", File{V: 1, Page: "p", Threads: []Thread{}, Comments: []Comment{},
			Suggestions: []SuggestionMeta{{}}, Changes: []Change{{}}},
			"v,page,updated,threads,comments,suggestions,changes"},
		{"Thread", Thread{Key: "k", Heading: "h", Resolved: true, Outcome: "o", Entries: []Entry{},
			Anchor: "block", BlockKind: "paragraph", Region: &Region{}},
			"key,heading,resolved,outcome,entries,anchor,blockKind,region"},
		{"Entry", Entry{Author: "a", Text: "t"}, "author,at,text"},
		{"Region", Region{}, "x,y,w,h"},
		{"Comment", Comment{Key: "k", Heading: "h", Text: "t"}, "key,heading,text"},
		{"SuggestionMeta", SuggestionMeta{ID: "i", Kind: "k", Author: "a", Quote: "q"}, "id,kind,author,at,quote"},
		{"Change", Change{Old: "o", New: "n", BlockKey: "b", Prefix: "p", Suffix: "s",
			Before: &s, After: &s, Proposal: &s, Placed: &yes},
			"old,new,blockKey,prefix,suffix,before,after,proposal,placed,at"},
	} {
		keys, err := ondisk.Keys(tc.val)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keys, ","); got != tc.want {
			t.Errorf("%s keys = %s\n                    want %s\n"+
				"a renamed key is not merely unread here: ImportFile deletes what the replay skips", tc.name, got, tc.want)
		}
	}
}
