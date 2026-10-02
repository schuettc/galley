package versions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	doc := filepath.Join(dir, "guide.md")
	if err := os.WriteFile(doc, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Open(doc)
}

func TestAskingAboutVersionsDoesNotCreateAny(t *testing.T) {
	s := newStore(t)
	rounds, err := s.List()
	if err != nil {
		t.Fatalf("List on an empty store: %v", err)
	}
	if len(rounds) != 0 {
		t.Errorf("an unrevised document has %d rounds", len(rounds))
	}
	if _, err := os.Stat(s.Dir()); !os.IsNotExist(err) {
		t.Errorf("reading the history created %s", s.Dir())
	}
}

func TestEveryVersionIsACleanDocumentAndAFullCopy(t *testing.T) {
	s := newStore(t)
	for i, body := range []string{"one\n", "one\ntwo\n", "one\ntwo\nthree\n"} {
		r, err := s.Commit(body, Round{Authors: []string{AuthorReviewer}, Reason: ReasonRevise})
		if err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
		if r.N != i+1 {
			t.Errorf("round %d numbered %d", i+1, r.N)
		}
		got, err := s.Content(r.N)
		if err != nil {
			t.Fatalf("read back %d: %v", r.N, err)
		}
		if got != body {
			t.Errorf("version %d came back %q, want %q", r.N, got, body)
		}
	}
	// A FULL COPY, NOT A REFERENCE. Every file stands alone, so the second
	// version is readable with nothing but itself.
	raw, err := os.ReadFile(filepath.Join(s.Dir(), "0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "one\ntwo\n" {
		t.Errorf("0002.md is not the whole document: %q", raw)
	}
}

func TestNoDiffIsEverStored(t *testing.T) {
	s := newStore(t)
	if _, err := s.Commit("The rail is fixed.\n", Round{Reason: ReasonOpened}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("The rail scrolls.\n", Round{Reason: ReasonRevise}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := map[string]bool{".gitattributes": true, "0001.md": true, "0002.md": true, "rounds.jsonl": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("the store holds %q, which is neither a version nor the manifest", n)
		}
	}
	// AND WHAT IS ON DISK IS WHAT IT WAS HANDED, NOTHING ADDED AND NOTHING
	// TAKEN OUT. Deliberately not "no CriticMarkup anywhere in the store": this
	// store never writes a marker of its own, so such a check would be green
	// whatever it was handed — and the caller legitimately hands it markup,
	// because a version is byte-for-byte the file and a hand-written file can
	// carry suggestion marks. See the package comment.
	const marked = "The rail {~~is fixed~>scrolls~~}.\n"
	r, err := s.Commit(marked, Round{Reason: ReasonLanded})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Content(r.N)
	if err != nil {
		t.Fatal(err)
	}
	if got != marked {
		t.Errorf("the store rewrote what it was handed: %q, want %q", got, marked)
	}
}

func TestARoundMayCarryBothAuthors(t *testing.T) {
	s := newStore(t)
	r, err := s.Commit("live work\n", Round{
		Authors: []string{AuthorReviewer, AuthorAgent},
		Reason:  ReasonSettled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Authors(r.Authors); got != "court and agent" {
		t.Errorf("a live round reads as %q", got)
	}
	back, ok, err := s.Get(r.N)
	if err != nil || !ok {
		t.Fatalf("round %d did not survive the manifest: ok=%v err=%v", r.N, ok, err)
	}
	if len(back.Authors) != 2 {
		t.Errorf("both authors did not survive the manifest: %v", back.Authors)
	}
}

func TestTheInstructionIsCarriedOnceAndPointedAt(t *testing.T) {
	s := newStore(t)
	ask, err := s.Commit("v1\n", Round{
		Authors: []string{AuthorReviewer}, Reason: ReasonRevise,
		Instruction: "make the second paragraph shorter",
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := s.Commit("v2\n", Round{
		Authors: []string{AuthorAgent}, Reason: ReasonLanded, Answers: ask.N,
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Instruction != "" {
		t.Errorf("the answering round repeated the instruction: %q", answer.Instruction)
	}
	if answer.Answers != ask.N {
		t.Errorf("the answering round points at %d, want %d", answer.Answers, ask.N)
	}
}

func TestTheManifestSurvivesAUnionMerge(t *testing.T) {
	// merge=union keeps BOTH sides' lines, so one round can genuinely appear
	// twice and the file can arrive out of order. The history's job is to be
	// complete and readable, so it must survive both.
	s := newStore(t)
	if _, err := s.Commit("a\n", Round{Reason: ReasonOpened}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit("b\n", Round{Reason: ReasonRevise}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir(), "rounds.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	scrambled := lines[1] + "\n" + lines[0] + "\n" + lines[1] + "\n" + "{not json}\n"
	if err := os.WriteFile(path, []byte(scrambled), 0o644); err != nil {
		t.Fatal(err)
	}
	rounds, err := s.List()
	if err != nil {
		t.Fatalf("List over a merged manifest: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("got %d rounds from a duplicated, reordered, part-corrupt manifest, want 2", len(rounds))
	}
	if rounds[0].N != 1 || rounds[1].N != 2 {
		t.Errorf("the rounds came back out of order: %d, %d", rounds[0].N, rounds[1].N)
	}
}

func TestTheStoreDeclaresItsMergeRule(t *testing.T) {
	s := newStore(t)
	if _, err := s.Commit("a\n", Round{Reason: ReasonOpened}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), ".gitattributes"))
	if err != nil {
		t.Fatalf("the store did not declare a merge rule: %v", err)
	}
	if !strings.Contains(string(raw), "merge=union") {
		t.Errorf("the manifest is committed and not union-merged: %q", raw)
	}
}

func TestADigestIsRecordedForEveryVersion(t *testing.T) {
	s := newStore(t)
	r, err := s.Commit("the body\n", Round{Reason: ReasonOpened, At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Digest) != 64 {
		t.Errorf("no digest recorded for version %d: %q", r.N, r.Digest)
	}
	if r.File != "0001.md" {
		t.Errorf("version 1 is in %q", r.File)
	}
}

// A ROUND CUT BEFORE THE MANIFEST EXISTED MUST STILL READ. rounds.jsonl is
// append-only and there are lines in the wild written before Asks and Changes
// were fields, so the claim that matters is not that the new fields serialize —
// it is that an old line survives a read and comes back saying exactly what it
// said.
func TestRoundTripsUnknownAndNewFields(t *testing.T) {
	s := newStore(t)

	// A round cut before Asks/Changes existed.
	old, err := s.Commit("# d\n", Round{
		Reason: ReasonOpened, Authors: []string{AuthorReviewer},
		Instruction: "Tighten the opening.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A round cut after they exist.
	if _, err := s.Commit("# d\n\nbody\n", Round{
		Reason: ReasonLanded, Authors: []string{AuthorAgent}, Answers: old.N,
		Instruction: "Tighten the opening.",
		Asks:        []Ask{{Key: "t-1", Text: "Tighten the opening.", Quote: "The ingest worker"}},
		Changes: []Change{{
			Locator: "body", Answers: []string{"t-1"}, Note: "Cut the preamble.",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rounds, got %d", len(got))
	}
	if got[0].Instruction != old.Instruction || got[0].Asks != nil || got[0].Changes != nil {
		t.Errorf("old round did not round-trip: %+v", got[0])
	}
	if len(got[1].Asks) != 1 || got[1].Asks[0].Key != "t-1" || got[1].Asks[0].Quote != "The ingest worker" {
		t.Errorf("asks did not round-trip: %+v", got[1].Asks)
	}
	if len(got[1].Changes) != 1 || got[1].Changes[0].Note != "Cut the preamble." ||
		got[1].Changes[0].Locator != "body" ||
		len(got[1].Changes[0].Answers) != 1 || got[1].Changes[0].Answers[0] != "t-1" {
		t.Errorf("changes did not round-trip: %+v", got[1].Changes)
	}
	// AND THE OLD LINE IS STILL AN OLD LINE ON DISK. omitempty is what makes
	// the fields additive: a round with neither must not gain two null keys.
	raw, err := os.ReadFile(filepath.Join(s.Dir(), "rounds.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(raw), "\n", 2)[0]
	if strings.Contains(first, "asks") || strings.Contains(first, "changes") {
		t.Errorf("a round with no manifest wrote the keys anyway: %s", first)
	}
}
