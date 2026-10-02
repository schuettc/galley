package unsent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/ondisk"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/versions"
)

// ONE RULE FOR WHERE THE ROUNDS LIVE. The unsent round sits beside them, and a
// second spelling of that directory is a second place to get it wrong.
func TestPathIsBesideTheRounds(t *testing.T) {
	if got, want := Path("/d/x/doc.md"), "/d/x/.galley/versions/doc.md/pending.json"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	md := filepath.Join(t.TempDir(), "notes.md")
	if got, want := Path(md), filepath.Join(versions.Open(md).Dir(), "pending.json"); got != want {
		t.Fatalf("Path = %q, want %q — the store's directory, not a second spelling of it", got, want)
	}
}

// THE FILE IS WHERE THE WORDS LIVE NOW, so a round trip that changed one byte
// of them changed what the reviewer said. The text carries a blank line and the
// note-closing marker on purpose: both are what the .md could not hold.
func TestARoundTripKeepsEveryFieldVerbatim(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	want := File{V: Version, Comments: []Comment{
		{Key: "cm-0123456789abcdef", Kind: KindText, Text: "line one\n\nline three <<} stays", Author: review.AuthorCourt, At: at, Quote: "the words"},
		{Key: "cb-0123456789abcdef", Kind: KindBlock, Text: "this part of the figure", Author: review.AuthorCourt, At: at.Add(time.Minute),
			BlockKind: "image", Region: &review.Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}, Quote: "Figure 1"},
		{Key: "cd-0123456789abcdef", Kind: KindDocument, Text: "the whole thing is too long", Author: review.AuthorCourt, At: at.Add(2 * time.Minute)},
	}}
	path := filepath.Join(t.TempDir(), ".galley", "versions", "doc.md", "pending.json")
	// V is stamped by Save, not trusted from the caller.
	in := want
	in.V = 0
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"v": 1`) {
		t.Fatalf("the file must carry v 1:\n%s", raw)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the round:\n got %+v\nwant %+v", got, want)
	}
}

// NO FILE IS NO COMMENTS, and asking must not be what brings one into
// existence.
func TestAMissingFileIsAnEmptyRound(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "versions", "doc.md")
	got, err := Load(filepath.Join(dir, "pending.json"))
	if err != nil {
		t.Fatalf("a missing file is not an error: %v", err)
	}
	if len(got.Comments) != 0 {
		t.Fatalf("comments = %+v, want none", got.Comments)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load created %s (stat err %v)", dir, err)
	}
}

func writeRaw(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pending.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A NEWER FILE IS REFUSED, AND THE REFUSAL SAYS SO — the caller leaves it
// untouched rather than overwriting a round this build cannot read.
func TestANewerFileIsRefused(t *testing.T) {
	_, err := Load(writeRaw(t, `{"v":2,"comments":[]}`))
	if !errors.Is(err, ondisk.ErrFuture) {
		t.Fatalf("err = %v, want ondisk.ErrFuture", err)
	}
	if errors.Is(err, ErrUnreadable) {
		t.Fatal("a newer file is not a corrupt one — the two have different remedies")
	}
}

// A NEWER GALLEY MAY ADD A KEY. The version is read before the strict decode,
// or the new key reads as corruption and the caller quarantines the very file
// ErrFuture exists to leave untouched.
func TestANewerFileWithANewKeyIsRefusedAsNewerNotUnreadable(t *testing.T) {
	_, err := Load(writeRaw(t, `{"v":2,"comments":[],"extra":1}`))
	if !errors.Is(err, ondisk.ErrFuture) {
		t.Fatalf("err = %v, want ondisk.ErrFuture", err)
	}
	if errors.Is(err, ErrUnreadable) {
		t.Fatal("a newer file with a key this build does not know was called unreadable — the caller would quarantine it")
	}
}

// PRIVATE MEANS STRICT: a key this build does not know is a shape it cannot
// honour, and reading half of it misattributes the round.
func TestAnUnknownFieldIsRefused(t *testing.T) {
	_, err := Load(writeRaw(t, `{"v":1,"comments":[{"key":"cm-0123456789abcdef","kind":"text","txet":"x","author":"court","at":"2026-10-01T09:30:00Z"}]}`))
	if err == nil {
		t.Fatal("a renamed key decoded in silence")
	}
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v, want ErrUnreadable so the caller can quarantine the file", err)
	}
	if !strings.Contains(err.Error(), "txet") {
		t.Fatalf("the refusal must name the key it did not know, got %v", err)
	}
	if _, err := Load(writeRaw(t, `{"v":1,"comm`)); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("a truncated file: err = %v, want ErrUnreadable", err)
	}
}

// A COMMENT ID AND A BLOCK ID MUST LOOK DIFFERENT. `bk-` is the block key's
// prefix (suggest/anchor.go), so a comment never uses it.
func TestIDsCarryTheirKindAndNeverCollideWithBlockKeys(t *testing.T) {
	for kind, re := range map[Kind]*regexp.Regexp{
		KindText:     regexp.MustCompile(`^cm-[0-9a-f]{16}$`),
		KindBlock:    regexp.MustCompile(`^cb-[0-9a-f]{16}$`),
		KindDocument: regexp.MustCompile(`^cd-[0-9a-f]{16}$`),
	} {
		id := NewID(kind)
		if !re.MatchString(id) {
			t.Fatalf("NewID(%s) = %q, want %s", kind, id, re)
		}
		if strings.HasPrefix(id, "bk-") {
			t.Fatalf("NewID(%s) = %q collides with the block key prefix", kind, id)
		}
	}
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		id := NewID(KindText)
		if seen[id] {
			t.Fatalf("NewID repeated %q after %d IDs", id, i)
		}
		seen[id] = true
	}
}

// THE LIVE STATE AND ITS MIRROR CARRY THE SAME VALUES. One thread of each
// anchor kind, with the figure rectangle and block kind that only the file
// keeps; a thread with no reviewer words is not an unsent comment.
// AN UNKNOWN KIND IS A PROGRAMMING ERROR. Minting it a `cm-` ID would write a
// mark the .md places as highlighted text, whatever the comment was really on.
func TestNewIDPanicsOnAnUnknownKind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewID minted an ID for a kind it does not know")
		}
	}()
	NewID(Kind("figure"))
}

func TestThreadsRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	ts := []review.Thread{
		{Key: "cm-0123456789abcdef", Heading: "the words", Entries: []review.Entry{{Author: review.AuthorCourt, At: at, Text: "line one\n\nline three"}}},
		{Key: "cb-0123456789abcdef", Heading: "Figure 1", Anchor: "block", BlockKind: "image", Region: &review.Region{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
			Entries: []review.Entry{{Author: review.AuthorCourt, At: at.Add(time.Minute), Text: "this corner"}}},
		{Key: "cd-0123456789abcdef", Anchor: "document", Entries: []review.Entry{{Author: review.AuthorCourt, At: at.Add(2 * time.Minute), Text: "too long"}}},
	}
	cs := FromThreads(ts)
	if len(cs) != 3 {
		t.Fatalf("got %d comments, want 3: %+v", len(cs), cs)
	}
	for i, want := range []Kind{KindText, KindBlock, KindDocument} {
		if cs[i].Kind != want {
			t.Fatalf("comment %d kind = %q, want %q", i, cs[i].Kind, want)
		}
	}
	if cs[0].Quote != "the words" || cs[0].Text != "line one\n\nline three" || cs[0].Author != review.AuthorCourt || !cs[0].At.Equal(at) {
		t.Fatalf("comment 0 = %+v — Quote is the Heading, and Text/Author/At the reviewer's entry", cs[0])
	}
	if got := ToThreads(cs); !reflect.DeepEqual(got, ts) {
		t.Fatalf("round trip changed the threads:\n got %+v\nwant %+v", got, ts)
	}

	skipped := []review.Thread{
		{Key: "cm-agentonly000000", Entries: []review.Entry{{Author: review.AuthorAgent, At: at, Text: "an answer"}}},
		{Key: "cm-blank0000000000", Entries: []review.Entry{{Author: review.AuthorCourt, At: at, Text: "  \n "}}},
		{Key: "cm-none00000000000"},
	}
	if got := FromThreads(skipped); len(got) != 0 {
		t.Fatalf("threads with no reviewer words became comments: %+v", got)
	}
	// The reviewer's entry is found even when it is not the first one.
	later := []review.Thread{{Key: "cm-later0000000000", Entries: []review.Entry{
		{Author: review.AuthorAgent, At: at, Text: "an answer"},
		{Author: review.AuthorCourt, At: at.Add(time.Minute), Text: "the ask"},
	}}}
	if got := FromThreads(later); len(got) != 1 || got[0].Text != "the ask" {
		t.Fatalf("FromThreads = %+v, want the reviewer's entry", got)
	}
}
