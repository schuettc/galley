package serve

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/review"
)

const beforeDoc = "# Title\n\nAlpha one here.\n\nBeta two here.\n\nGamma three here.\n"

// TestRevertingARemovalPutsTheBlockBackWhereItWas is the case Court asked for:
// undo the paragraph I deleted, and keep the edits I made after it. ⌘Z cannot
// do that at any price — it is sequential, and this is targeted.
func TestRevertingARemovalPutsTheBlockBackWhereItWas(t *testing.T) {
	after := "# Title\n\nAlpha one here.\n\nGamma three, reworded.\n"
	got, err := revertChange(beforeDoc, after, ReviewerChange{Kind: "removed", Before: "Beta two here."}, plainOf)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	want := "# Title\n\nAlpha one here.\n\nBeta two here.\n\nGamma three, reworded.\n"
	if got != want {
		t.Errorf("the block came back in the wrong place.\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// TestRevertingARemovalAtTheTopOfTheDocument — there is no block above it to
// find the place through, which is the one case the search cannot answer.
func TestRevertingARemovalAtTheTop(t *testing.T) {
	after := "Alpha one here.\n\nBeta two here.\n"
	got, err := revertChange(beforeDoc, after, ReviewerChange{Kind: "removed", Before: "# Title"}, plainOf)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !strings.HasPrefix(got, "# Title\n\nAlpha one here.") {
		t.Errorf("the first block did not come back first:\n%q", got)
	}
}

// TestRevertingAnAddition takes out a block the reviewer wrote.
func TestRevertingAnAddition(t *testing.T) {
	after := beforeDoc[:len(beforeDoc)-1] + "\n\nDelta four, new.\n"
	got, err := revertChange(beforeDoc, after, ReviewerChange{Kind: "added", After: "Delta four, new."}, plainOf)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if strings.Contains(got, "Delta four") {
		t.Errorf("the added block survived its own revert:\n%q", got)
	}
	if !strings.Contains(got, "Beta two here.") {
		t.Errorf("reverting an addition took something else with it:\n%q", got)
	}
}

// TestRevertingAChangeRestoresTheOldWords.
func TestRevertingAChangeRestoresTheOldWords(t *testing.T) {
	after := "# Title\n\nAlpha one here.\n\nBeta two here.\n\nGamma three, reworded.\n"
	got, err := revertChange(beforeDoc, after,
		ReviewerChange{Kind: "changed", Before: "Gamma three here.", After: "Gamma three, reworded."}, plainOf)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got != beforeDoc {
		t.Errorf("reverting the only change did not restore the document.\nwant:\n%q\ngot:\n%q", beforeDoc, got)
	}
}

// TestAnAmbiguousRevertIsREFUSED, and this is the check that matters most.
//
// A revert that lands the words in a plausible-but-wrong place is worse than a
// button that says no: the reviewer would have to notice, and the whole point
// of the verb is that they should not have to hold the document in their head.
func TestAnAmbiguousRevertIsRefused(t *testing.T) {
	before := "# T\n\nSame line.\n\nMiddle.\n\nSame line.\n"
	after := "# T\n\nSame line.\n\nMiddle.\n\nSame line.\n\nSame line.\n"
	if _, err := revertChange(before, after, ReviewerChange{Kind: "added", After: "Same line."}, plainOf); err == nil {
		t.Error("an addition matching three blocks was reverted anyway — one of them at random")
	}
	if _, err := revertChange(before, after,
		ReviewerChange{Kind: "changed", Before: "Middle.", After: "Same line."}, plainOf); err == nil {
		t.Error("a change whose text appears three times was reverted anyway")
	}
}

// TestARemovalThatIsNotAWholeBlockIsRefused — half a sentence has no block to
// put back, and guessing where it went is the failure this refuses.
func TestARemovalThatIsNotAWholeBlockIsRefused(t *testing.T) {
	after := "# Title\n\nAlpha here.\n\nBeta two here.\n\nGamma three here.\n"
	if _, err := revertChange(beforeDoc, after, ReviewerChange{Kind: "removed", Before: "one"}, plainOf); err == nil {
		t.Error("a partial removal was reverted, which means it was put back somewhere guessed")
	}
}

// TestRevertingKeepsEveryOtherEdit is the whole difference from undo, asserted
// rather than implied: two edits, revert the first, the second survives.
func TestRevertingKeepsEveryOtherEdit(t *testing.T) {
	after := "# Title\n\nAlpha one here.\n\nGamma three, reworded.\n"
	got, err := revertChange(beforeDoc, after, ReviewerChange{Kind: "removed", Before: "Beta two here."}, plainOf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Gamma three, reworded.") {
		t.Errorf("reverting one edit undid another — that is what undo does, and the point of this is that it does not:\n%q", got)
	}
}

// threeParas is a range comment's paragraph, a block comment's paragraph and
// the paragraph the reviewer edits by hand, in that order.
const threeParas = "# T\n\nAlpha one here.\n\nBeta two here.\n\nGamma three here.\n"

// commentedServer files a range comment on paragraph 1 and a block comment on
// paragraph 2, and returns the server with the two keys.
func commentedServer(t *testing.T) (s *EditServer, text, block string) {
	t.Helper()
	s = editServerWith(t, threeParas)
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 9, "text": "which one?"})
	instructOK(t, s, map[string]any{
		"op": "comment_block", "target": blockKeyOf(t, s, "paragraph", "Beta two here."), "text": "say more",
	})
	for _, c := range loadUnsent(t, s) {
		switch {
		case strings.HasPrefix(c.Key, "cm-"):
			text = c.Key
		case strings.HasPrefix(c.Key, "cb-"):
			block = c.Key
		}
	}
	if text == "" || block == "" {
		t.Fatalf("the fixture filed %+v, want one text and one block comment", loadUnsent(t, s))
	}
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	return s, text, block
}

// changeKeyOf is the key the rail offers for the one change of kind.
func changeKeyOf(t *testing.T, s *EditServer, kind string) string {
	t.Helper()
	for _, c := range pendingView(t, s).Changes {
		if c.Kind == kind {
			return c.Key
		}
	}
	t.Fatalf("no %s change is offered: %+v", kind, pendingView(t, s).Changes)
	return ""
}

// TestRevertingOneEditKeepsEveryInstructionMark is bug 4. Revert rebuilt the
// document from the plain text the rail compares, so putting back one
// paragraph lifted every instruction's mark in the document with it.
func TestRevertingOneEditKeepsEveryInstructionMark(t *testing.T) {
	for _, tc := range []struct {
		kind string
		edit func(t *testing.T, s *EditServer)
	}{
		{"removed", func(t *testing.T, s *EditServer) { dropBlock(t, s, "Gamma three here.") }},
		{"added", func(t *testing.T, s *EditServer) {
			if _, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
				model.Blocks = append(model.Blocks, docmodel.Block{
					Kind: docmodel.Paragraph, Inlines: []docmodel.Inline{{Text: "Delta four, new."}},
				})
				return model, nil, nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed", func(t *testing.T, s *EditServer) { reviewerRewrites(t, s, "three", "3") }},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s, text, block := commentedServer(t)
			tc.edit(t, s)
			if rec := postRec(t, s, "/_galley/revert", map[string]any{"key": changeKeyOf(t, s, tc.kind)}); rec.Code >= 300 {
				t.Fatalf("revert: %d %s", rec.Code, rec.Body.String())
			}
			if err := s.Project(); err != nil {
				t.Fatal(err)
			}

			md := readMD(t, s)
			if !strings.Contains(md, "Gamma three here.") || strings.Contains(md, "Delta four") || strings.Contains(md, "Gamma 3") {
				t.Errorf("the edit was not reverted:\n%s", md)
			}
			if !strings.Contains(md, "{==Alpha one==}{>>@comment "+text+"<<}") {
				t.Errorf("the revert lifted the text comment's mark:\n%s", md)
			}
			if !strings.Contains(md, "Beta two here.\n\n{>>@comment "+block+"<<}\n") {
				t.Errorf("the revert lifted or moved the block comment's note:\n%s", md)
			}
			view := pendingView(t, s)
			if len(view.Instructions) != 2 {
				t.Fatalf("after the revert the rail lists %+v, want both comments", view.Instructions)
			}
			for _, in := range view.Instructions {
				if in.Run == "" && in.AnchorKey == "" {
					t.Errorf("the revert left %s unplaced: %+v", in.Key, in)
				}
			}
			if len(view.Changes) != 0 {
				t.Errorf("the reverted edit is still listed: %+v", view.Changes)
			}
		})
	}
}

// TestRevertRefusesAnEditThatRunsThroughAHighlight: the changed words are
// split by an instruction's mark, so no literal substitution exists that
// keeps the mark. Refused by name, and the document does not move.
func TestRevertRefusesAnEditThatRunsThroughAHighlight(t *testing.T) {
	s, _, _ := commentedServer(t)
	reviewerRewrites(t, s, "one", "uno")
	before := liveMarkdown(t, s)

	rec := postRec(t, s, "/_galley/revert", map[string]any{"key": changeKeyOf(t, s, "changed")})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("revert answered %d %s, want 400", rec.Code, rec.Body.String())
	}
	const want = "that edit runs through an instruction's highlight — delete the instruction or change the words by hand"
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("revert said %q, want %q", rec.Body.String(), want)
	}
	if got := liveMarkdown(t, s); !bytes.Equal(got, before) {
		t.Errorf("a refused revert moved the document:\n--- was ---\n%s\n--- now ---\n%s", before, got)
	}
}
