package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/unsent"
)

// BLOCK AND DOCUMENT COMMENTS ARE LINKED TO THEIR PLACE BY ID ALONE. A block
// comment's words and rectangle live in pending.json; the file carries a
// {>>@comment cb-…<<} line after the block and nothing else. A document
// comment leaves no mark at all.

// blockMark matches a block comment's ID mark, wherever it sits.
var blockMark = regexp.MustCompile(`\{>>@comment cb-[0-9a-f]{16}<<\}`)

// blockKeyOf is the key of the first addressable block of kind whose label
// contains label.
func blockKeyOf(t *testing.T, s *EditServer, kind, label string) string {
	t.Helper()
	for _, b := range pendingView(t, s).Blocks {
		if b.Kind == kind && strings.Contains(b.Label, label) {
			return b.Key
		}
	}
	t.Fatalf("no %s block labelled %q in %+v", kind, label, pendingView(t, s).Blocks)
	return ""
}

// saveUnsentRound writes pending.json for the document at md, as an earlier
// galley would have left it.
func saveUnsentRound(t *testing.T, md string, cs ...unsent.Comment) {
	t.Helper()
	path := unsent.Path(md)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unsent.Save(path, unsent.File{Comments: cs}); err != nil {
		t.Fatal(err)
	}
}

func courtSaid(key string, kind unsent.Kind, text string) unsent.Comment {
	return unsent.Comment{Key: key, Kind: kind, Text: text, Author: review.AuthorCourt,
		At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
}

// Bug 2: editing a block comment to say something with a blank line in it put
// the words into the .md as prose, because the words were the note.
func TestEditingABlockInstructionWithALineBreakChangesOnlyItsWords(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	instructOK(t, s, map[string]any{"op": "comment_block", "target": figureKey(t, s), "text": "fix it"})
	key := onlyKey(t, s)
	instructOK(t, s, map[string]any{"op": "edit", "key": key, "text": "first\n\nsecond"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	md := readMD(t, s)
	if got := blockMark.FindAllString(md, -1); len(got) != 1 || got[0] != "{>>@comment "+key+"<<}" {
		t.Errorf("the file carries marks %q, want exactly the one for %s: %q", got, key, md)
	}
	if strings.Contains(md, "first") || strings.Contains(md, "second") || strings.Contains(md, "fix it") {
		t.Errorf("the comment's words are in the file: %q", md)
	}
	if got := loadUnsent(t, s); len(got) != 1 || got[0].Text != "first\n\nsecond" {
		t.Errorf("pending.json = %+v, want the edited words verbatim", got)
	}
}

func TestBlockAndDocumentCommentsAcceptLineBreaks(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	for _, body := range []map[string]any{
		{"op": "comment_block", "target": figureKey(t, s), "text": "a\nb"},
		{"op": "comment_document", "text": "a\nb"},
	} {
		if rec := postRec(t, s, "/_galley/instruct", body); rec.Code != 200 {
			t.Errorf("%v with a line break answered %d %s, want 200", body["op"], rec.Code, rec.Body.String())
		}
	}
	for _, c := range loadUnsent(t, s) {
		if c.Text != "a\nb" {
			t.Errorf("pending.json holds %q, want the line break kept", c.Text)
		}
	}
}

func TestADocumentCommentLeavesTheFileUntouched(t *testing.T) {
	s := newEditServer(t, t.TempDir(), "d.md", unsentDoc)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	before := readMD(t, s)
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "tighten the whole thing"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if after := readMD(t, s); after != before {
		t.Errorf("a document comment changed the file:\n before %q\n after  %q", before, after)
	}
	got := pendingView(t, s).Instructions
	if len(got) != 1 || got[0].Anchor != "document" || got[0].Text != "tighten the whole thing" {
		t.Errorf("the rail lists %+v, want the document comment", got)
	}
	if c := loadUnsent(t, s); len(c) != 1 || !strings.HasPrefix(c[0].Key, "cd-") {
		t.Errorf("pending.json = %+v, want one cd- comment", c)
	}
}

// everyKindDoc has a block of every kind a comment can sit on, and a table
// cell that already carries a block comment's mark (cellKey, in pending.json).
const everyKindDoc = "# Title\n\n" +
	"Cognito mints every token.\n\n" +
	"## Section two\n\n" +
	"A plain paragraph.\n\n" +
	"Second paragraph here.\n\n" +
	"A paragraph to comment on.\n\n" +
	"```go\nx := 1\n```\n\n" +
	"![the diagram](fig.png)\n\n" +
	"| a | b |\n| --- | --- |\n| x | {>>@comment " + cellKey + "<<} |\n"

const cellKey = "cb-00000000000000c1"

func TestTheUnsentRoundSurvivesARestartForEveryKind(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "d.md")
	if err := os.WriteFile(md, []byte(everyKindDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	saveUnsentRound(t, md, courtSaid(cellKey, unsent.KindBlock, "this cell is wrong"))
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	// What each block comment must sit on after the restart, by its words.
	on := map[string][2]string{
		"this cell is wrong":      {"table", ""},
		"say more":                {"paragraph", "A paragraph to comment on."},
		"rename the section":      {"heading", "Section two"},
		"this code is wrong":      {"codeBlock", "x := 1"},
		"this box is wrong":       {"image", "the diagram"},
		"name the issuer":         {"", ""},
		"join these":              {"", ""},
		"tighten the whole thing": {"", ""},
	}
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{1}, "from": 0, "to": 7, "text": "name the issuer"})
	instructOK(t, s, map[string]any{"op": "comment", "path": []int{3}, "from": 2, "toPath": []int{4}, "to": 6, "text": "join these"})
	instructOK(t, s, map[string]any{"op": "comment_block", "target": blockKeyOf(t, s, "paragraph", "A paragraph to comment on."), "text": "say more"})
	instructOK(t, s, map[string]any{"op": "comment_block", "target": blockKeyOf(t, s, "heading", "Section two"), "text": "rename the section"})
	instructOK(t, s, map[string]any{"op": "comment_block", "target": blockKeyOf(t, s, "codeBlock", "x := 1"), "text": "this code is wrong"})
	instructOK(t, s, map[string]any{
		"op": "comment_block", "target": figureKey(t, s), "text": "this box is wrong",
		"region": map[string]any{"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.4},
	})
	instructOK(t, s, map[string]any{"op": "comment_document", "text": "tighten the whole thing"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, c := range loadUnsent(t, s) {
		keys[c.Text] = c.Key
	}
	if len(keys) != len(on) {
		t.Fatalf("pending.json holds %d comments before the restart, want %d: %v", len(keys), len(on), keys)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	view := pendingView(t, again)
	if len(view.Instructions) != len(on) {
		t.Errorf("after a restart the rail lists %d instructions, want %d: %+v", len(view.Instructions), len(on), view.Instructions)
	}
	kindOf := map[string]string{}
	labelOf := map[string]string{}
	for _, b := range view.Blocks {
		kindOf[b.Key], labelOf[b.Key] = b.Kind, b.Label
	}
	for _, in := range view.Instructions {
		want, ok := on[in.Text]
		if !ok {
			t.Errorf("after a restart the rail lists an instruction nobody filed: %+v", in)
			continue
		}
		if in.Key != keys[in.Text] {
			t.Errorf("%q came back as %s, want %s", in.Text, in.Key, keys[in.Text])
		}
		switch {
		case want[0] != "":
			if kindOf[in.AnchorKey] != want[0] || !strings.Contains(labelOf[in.AnchorKey], want[1]) {
				t.Errorf("%q sits on %q (%s %q), want the %s %q", in.Text, in.AnchorKey,
					kindOf[in.AnchorKey], labelOf[in.AnchorKey], want[0], want[1])
			}
		case in.Anchor == "document":
		default:
			if in.Run == "" {
				t.Errorf("%q came back unplaced: %+v", in.Text, in)
			}
		}
		if in.Text == "this box is wrong" &&
			(in.Region == nil || *in.Region != (review.Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4})) {
			t.Errorf("the figure comment's rectangle = %+v, want {0.1 0.2 0.3 0.4}", in.Region)
		}
	}
}

// galley stopped between the pending.json write and the .md write: the words
// are saved and the marks never reached the file. The comments are listed,
// unplaced, and stay.
func TestACommentWhoseMarkNeverReachedTheFileIsUnplacedNotLost(t *testing.T) {
	md := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(md, []byte(foxDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	saveUnsentRound(t, md,
		courtSaid("cb-00000000000000b1", unsent.KindBlock, "about the paragraph"),
		courtSaid("cm-00000000000000a1", unsent.KindText, "about the fox"))
	s, err := NewEdit(md)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	check := func(when string) {
		got := pendingView(t, s).Instructions
		if len(got) != 2 {
			t.Fatalf("%s the rail lists %+v, want both comments", when, got)
		}
		for _, in := range got {
			if in.AnchorKey != "" || in.Run != "" {
				t.Errorf("%s %s is placed (%q, %q) with no mark in the file", when, in.Key, in.AnchorKey, in.Run)
			}
		}
		if c := loadUnsent(t, s); len(c) != 2 {
			t.Errorf("%s pending.json = %+v, want both comments", when, c)
		}
	}
	check("at startup")
	if err := s.Project(); err != nil {
		t.Fatal(err)
	}
	check("after a projection")
}

// The mark's position decides what a block comment is about.
func TestMovingTheMarkLineMovesTheComment(t *testing.T) {
	md := filepath.Join(t.TempDir(), "d.md")
	const key = "cb-00000000000000b2"
	mark := "{>>@comment " + key + "<<}\n\n"
	under := func(first bool) string {
		if first {
			return "# T\n\nFirst para.\n\n" + mark + "Second para.\n"
		}
		return "# T\n\nFirst para.\n\nSecond para.\n\n" + mark
	}
	saveUnsentRound(t, md, courtSaid(key, unsent.KindBlock, "about one of them"))
	for _, first := range []bool{true, false} {
		if err := os.WriteFile(md, []byte(under(first)), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := NewEdit(md)
		if err != nil {
			t.Fatal(err)
		}
		want := blockKeyOf(t, s, "paragraph", "Second para.")
		if first {
			want = blockKeyOf(t, s, "paragraph", "First para.")
		}
		got := pendingView(t, s).Instructions
		if len(got) != 1 || got[0].Key != key || got[0].AnchorKey != want {
			t.Errorf("with the mark under the %s paragraph the rail lists %+v, want %s on %s",
				map[bool]string{true: "first", false: "second"}[first], got, key, want)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
