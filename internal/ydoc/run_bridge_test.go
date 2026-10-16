package ydoc_test

import (
	"testing"

	"github.com/reearth/ygo/crdt"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/suggest"
	"github.com/subaud-tools/galley/internal/ydoc"
)

// The claim the whole phase rests on: a run survives the CRDT, so the browser
// can see it. Asserted through the real bridge rather than the model alone,
// because the mark attribute encoding is generic and nothing else forces it to
// stay that way — a future change that enumerated known attributes instead
// would break run silently, and every card would fall back to guessing.
func TestRunsSurviveTheBridge(t *testing.T) {
	want := suggest.MintRuns(docmodel.Doc{Blocks: []docmodel.Block{
		para(
			marked("age", mark(docmodel.Del, "author", "court", "at", "2026-08-07T00:00:00Z")),
			text(" and im"),
			marked("age", mark(docmodel.Del, "author", "court", "at", "2026-08-07T00:00:00Z")),
		),
	}})

	doc := crdt.New()
	ydoc.Load(doc, own(doc), want)

	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	wantRuns, gotRuns := bridgeRuns(want), bridgeRuns(got)
	if len(gotRuns) != 2 {
		t.Fatalf("want 2 runs back, got %d (%v)", len(gotRuns), gotRuns)
	}
	if wantRuns[0] != gotRuns[0] || wantRuns[1] != gotRuns[1] {
		t.Errorf("runs did not survive the bridge: %v -> %v", wantRuns, gotRuns)
	}
	if gotRuns[0] == gotRuns[1] {
		t.Error("the two marks came back sharing one run")
	}
}

// Load stays transparent: what goes in comes back out, runs and all. This is
// the property that kept MintRuns out of Load — the bridge's round-trip tests
// depend on it, and it is what found most of the document model's real bugs.
func TestLoadDoesNotInventRuns(t *testing.T) {
	plain := docmodel.Doc{Blocks: []docmodel.Block{
		para(marked("unminted", mark(docmodel.Ins, "author", "claude"))),
	}}

	doc := crdt.New()
	ydoc.Load(doc, own(doc), plain)

	got, err := ydoc.Read(doc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !docmodel.Equal(plain, got) {
		t.Errorf("Load returned more than it was given\nwant %#v\ngot  %#v", plain, got)
	}
	if runs := bridgeRuns(got); len(runs) != 0 {
		t.Errorf("Load minted runs of its own: %v", runs)
	}
}

func bridgeRuns(d docmodel.Doc) []string {
	var out []string
	for _, b := range d.Blocks {
		for _, in := range b.Inlines {
			for _, m := range in.Marks {
				if r := m.Attrs[docmodel.RunAttr]; r != "" {
					out = append(out, r)
				}
			}
		}
	}
	return out
}
