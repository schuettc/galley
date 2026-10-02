package suggest

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// The reported bug, as a test. Commenting on "age" in a document that also
// contains "image" failed with `"age" matched 2 times, want exactly 1` — the
// browser knew the exact range the reviewer had selected and threw it away.
// Given the range there is nothing to disambiguate.
func TestCommentOnRangeHandlesAmbiguousText(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "an image and an age"}},
	}}}

	// The bare "age" at runes 16..19, not the one inside "image".
	out, err := CommentOnRange(d, []int{0}, 16, 19, "cm-0000000000000001", "court", time.Now().UTC())
	if err != nil {
		t.Fatalf("CommentOnRange: %v", err)
	}
	pending := List(out)
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if pending[0].CommentID != "cm-0000000000000001" {
		t.Errorf("the highlight carries ID %q, want the one it was given", pending[0].CommentID)
	}
	if pending[0].Text != "age" {
		t.Errorf("highlighted %q, want %q", pending[0].Text, "age")
	}
	// And it must be the LAST occurrence — proving it did not simply find the
	// first match the way the search-based path would have.
	got := string(markdown.Serialize(out))
	if !strings.Contains(got, "an image and an {==age==}") {
		t.Errorf("highlighted the wrong occurrence: %q", got)
	}
}

// The search-based path is the CLI's and still refuses ambiguity, because
// there it is genuinely ambiguous — there is no selection to consult.
func TestCommentOnStillRefusesAmbiguousText(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "an image and an age"}},
	}}}
	if _, err := CommentOn(d, "age", "cm-0000000000000002", "court", time.Now().UTC()); err == nil {
		t.Error("want an ambiguity error from the search path, got nil")
	}
}

func TestCommentOnRangeRejectsAnImpossibleRange(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "short"}},
	}}}
	at := time.Now().UTC()
	for _, tc := range []struct {
		name     string
		from, to int
		path     []int
	}{
		{"past the end", 2, 99, []int{0}},
		{"inverted", 4, 2, []int{0}},
		{"empty", 2, 2, []int{0}},
		{"negative", -1, 3, []int{0}},
		{"no such block", 0, 3, []int{7}},
	} {
		if _, err := CommentOnRange(d, tc.path, tc.from, tc.to, "cm-0000000000000003", "court", at); err == nil {
			t.Errorf("%s: want an error, got nil", tc.name)
		}
	}
}

// Offsets are runes, not bytes: a range past a multi-byte character must still
// land on what the reviewer selected.
func TestCommentOnRangeCountsRunes(t *testing.T) {
	d := docmodel.Doc{Blocks: []docmodel.Block{{
		Kind:    docmodel.Paragraph,
		Inlines: []docmodel.Inline{{Text: "café ☕ age"}},
	}}}
	out, err := CommentOnRange(d, []int{0}, 7, 10, "cm-0000000000000004", "court", time.Now().UTC())
	if err != nil {
		t.Fatalf("CommentOnRange: %v", err)
	}
	pending := List(out)
	if len(pending) != 1 || pending[0].Text != "age" {
		t.Fatalf("rune offsets misread: %+v", pending)
	}
}
