package versions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/ondisk"
)

// oldManifest is `rounds.jsonl` AS IT WAS WRITTEN BEFORE `v` EXISTED — the
// literal bytes, not a struct re-marshalled by this build, because a struct
// round-trip would prove only that this build agrees with itself. Every `.galley`
// directory in the wild holds lines of exactly this shape.
const oldManifest = `{"n":1,"at":"2026-08-20T10:00:00Z","authors":["court"],"reason":"opened","instruction":"","answers":0,"digest":"d1","file":"0001.md"}
{"n":2,"at":"2026-08-20T10:05:00Z","authors":["court","agent"],"reason":"revise","instruction":"tighten the opening","asks":[{"key":"md-0-9","text":"tighten the opening","quote":"The opening"}],"answers":1,"digest":"d2","file":"0002.md"}
{"n":3,"at":"2026-08-20T10:09:00Z","authors":["agent"],"reason":"landed","instruction":"","changes":[{"locator":"a tighter opening","answers":["md-0-9"],"note":"rewrote it"}],"answers":2,"digest":"d3","file":"0003.md"}
`

func writeManifest(t *testing.T, body string) *Store {
	t.Helper()
	dir := t.TempDir()
	page := filepath.Join(dir, "doc.md")
	s := Open(page)
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "rounds.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

// THE BACKWARDS COMPATIBILITY PROMISE, AND IT IS THE WHOLE OF IT: a manifest
// written by a galley that had never heard of `v` reads back with every field
// intact and every round present. A migration that lost a round would be worse
// than the bug this change is about.
func TestAManifestWrittenBeforeVersionsReadsUnchanged(t *testing.T) {
	s := writeManifest(t, oldManifest)
	rounds, problems, err := s.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("an older manifest is not a problem: %v", problems)
	}
	if len(rounds) != 3 {
		t.Fatalf("got %d rounds, want 3 — a round that vanished is the bug", len(rounds))
	}
	for _, r := range rounds {
		if r.V != 0 {
			t.Fatalf("round %d: V = %d, want 0 — the absent field must not be invented on read", r.N, r.V)
		}
		if ondisk.Future(r.V, Version) {
			t.Fatalf("round %d reads as newer than this build", r.N)
		}
	}
	if got := rounds[1].Instruction; got != "tighten the opening" {
		t.Fatalf("instruction = %q", got)
	}
	if len(rounds[1].Asks) != 1 || rounds[1].Asks[0].Key != "md-0-9" || rounds[1].Asks[0].Quote != "The opening" {
		t.Fatalf("asks = %+v — an Ask's key is the only thing a Change can point at", rounds[1].Asks)
	}
	if len(rounds[2].Changes) != 1 || rounds[2].Changes[0].Locator != "a tighter opening" ||
		len(rounds[2].Changes[0].Answers) != 1 || rounds[2].Changes[0].Note != "rewrote it" {
		t.Fatalf("changes = %+v — the agent's testimony is not derivable from anything else", rounds[2].Changes)
	}
	if rounds[1].At.UTC() != time.Date(2026, 8, 20, 10, 5, 0, 0, time.UTC) {
		t.Fatalf("at = %v", rounds[1].At)
	}
	if rounds[0].Digest != "d1" || rounds[0].File != "0001.md" {
		t.Fatalf("digest/file lost: %+v", rounds[0])
	}
}

// AND A NEW ROUND APPENDED TO AN OLD MANIFEST DOES NOT DISTURB THE OLD LINES.
// The two generations sit in one file for as long as the document lives; that
// is what merge=union guarantees and what an in-place migration would break.
func TestANewRoundJoinsAnOldManifestWithoutRewritingIt(t *testing.T) {
	s := writeManifest(t, oldManifest)
	got, err := s.Commit("hello\n", Round{Authors: []string{AuthorAgent}, Reason: ReasonLanded})
	if err != nil {
		t.Fatal(err)
	}
	if got.N != 4 {
		t.Fatalf("N = %d, want 4 — the number is computed from the rounds that could be read", got.N)
	}
	if got.V != Version {
		t.Fatalf("V = %d, want %d — the store stamps it, not the caller", got.V, Version)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), "rounds.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), oldManifest) {
		t.Fatal("the existing lines were rewritten — the manifest is append-only")
	}
	rounds, _, err := s.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 4 {
		t.Fatalf("got %d rounds, want 4", len(rounds))
	}
}

// A DROPPED LINE IS A ROUND THAT VANISHED. It is still dropped — losing forty
// readable rounds because one is malformed is the opposite of a complete
// history — but it is no longer SILENT, which is what made it
// indistinguishable from a round that was never cut.
func TestAnUnreadableLineIsCountedRatherThanSwallowed(t *testing.T) {
	s := writeManifest(t, oldManifest+"{\"n\":4,\"reason\": TRUNCATED\n")
	rounds, problems, err := s.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 3 {
		t.Fatalf("got %d rounds, want the 3 readable ones", len(rounds))
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the one bad line counted", problems)
	}
	if problems[0].Line != 4 {
		t.Fatalf("line = %d, want 4 — the number an editor jumps to", problems[0].Line)
	}
	if !strings.Contains(problems[0].String(), "unreadable") {
		t.Fatalf("problem = %q", problems[0])
	}
	// List keeps its signature and its behaviour for the hot path.
	list, err := s.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %v (%v), want the same three rounds", list, err)
	}
}

// A LINE FROM A NEWER GALLEY IS KEPT, NOT REFUSED. The manifest is committed
// under merge=union and crosses machines and builds; a reader that dropped a
// round because it was written by tomorrow's galley would destroy the insurance
// this directory exists to be.
func TestARoundFromANewerGalleyIsKeptAndReported(t *testing.T) {
	s := writeManifest(t, oldManifest+
		`{"v":99,"n":4,"at":"2026-08-20T11:00:00Z","authors":["agent"],"reason":"landed","answers":2,"digest":"d4","file":"0004.md","somethingNew":{"deep":1}}`+"\n")
	rounds, problems, err := s.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 4 {
		t.Fatalf("got %d rounds, want 4 — a future round is kept", len(rounds))
	}
	if rounds[3].Digest != "d4" || rounds[3].File != "0004.md" {
		t.Fatalf("the fields this build understands must still be read: %+v", rounds[3])
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "v99") {
		t.Fatalf("problems = %v, want the mixed population reported", problems)
	}
}

// THE KEY SET IS THE CONTRACT, and this is the check a rename cannot get past.
//
// No decoder on the reading side can see a renamed key — it is looking for the
// one that vanished — so for a format that must tolerate unknown fields this
// list is the only thing that can go red on the commit that causes it. Every
// field is populated, so a NEW field also turns it red: that is deliberate.
// Adding one is a decision about whether Version must move with it, and this
// list is where that decision is made rather than discovered.
func TestTheRoundKeysAreTheContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want string
	}{
		{"Round", Round{
			V: 1, N: 1, At: time.Now(), Authors: []string{"court"}, Reason: ReasonRevise,
			Instruction: "x", Asks: []Ask{{Key: "k"}}, Changes: []Change{{Locator: "l"}},
			Answers: 1, Digest: "d", File: "f",
		}, "v,n,at,authors,reason,instruction,asks,changes,answers,digest,file"},
		{"Ask", Ask{Key: "k", Text: "t", Quote: "q"}, "key,text,quote"},
		{"Change", Change{Locator: "l", Answers: []string{"a"}, Note: "n"}, "locator,answers,note"},
	} {
		keys, err := ondisk.Keys(tc.val)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keys, ","); got != tc.want {
			t.Errorf("%s keys = %s\n            want %s\n"+
				"a renamed key is read by nobody and written by everybody; a new key needs a Version decision", tc.name, got, tc.want)
		}
	}
}
