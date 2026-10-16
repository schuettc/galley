package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/subaud-tools/galley/internal/review"
)

// repo builds a throwaway repository: a directory with a `.git` in it and a
// document inside.
func repo(t *testing.T) (root, doc string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc = filepath.Join(root, "doc.md")
	if err := os.WriteFile(doc, []byte("# doc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, doc
}

// readLog returns the log's lines, parsed.
func readLog(t *testing.T, path string) []Record {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d does not parse: %v\n%s", i+1, err, line)
		}
		out = append(out, rec)
	}
	return out
}

func TestLogWritesRelativeDocAndStableFields(t *testing.T) {
	root, doc := repo(t)
	err := Log(doc, Record{
		Kind: KindDeclined, Author: AuthorAgent, Review: "doc-7f3a",
		Old: "brown", New: "red", Reason: "the house style says brown",
		Quote: "the brown fox", Context: "…jumped over…",
	})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, Dir, LogName)
	raw, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(string(raw), "\n")

	// The KEY ORDER is part of the format — two builds must produce lines that
	// diff against each other. Assert the literal prefix order rather than the
	// decoded map, which has no order at all.
	want := []string{`"v":`, `"at":`, `"doc":`, `"review":`, `"kind":`, `"author":`,
		`"old":`, `"new":`, `"reason":`, `"quote":`, `"context":`}
	at := -1
	for _, key := range want {
		i := strings.Index(line, key)
		if i < 0 {
			t.Fatalf("%s missing from the record\n%s", key, line)
		}
		if i < at {
			t.Fatalf("%s is out of order\n%s", key, line)
		}
		at = i
	}

	recs := readLog(t, path)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].Doc != "doc.md" {
		t.Errorf("doc = %q, want the path RELATIVE to the repo root — an absolute one is a fact about one machine", recs[0].Doc)
	}
	if recs[0].V != Version {
		t.Errorf("v = %d, want %d", recs[0].V, Version)
	}
	if recs[0].At.IsZero() {
		t.Error("at was not stamped")
	}
	if recs[0].Reason != "the house style says brown" {
		t.Errorf("reason = %q", recs[0].Reason)
	}
}

func TestLogWritesTheMergeUnionAttribute(t *testing.T) {
	root, doc := repo(t)
	if err := Log(doc, Record{Kind: KindApproved, Author: AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, Dir, ".gitattributes")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("no .gitattributes beside the log: %v — an append-only file conflicts the instant two branches both append", err)
	}
	if !strings.Contains(string(raw), "*.jsonl merge=union") {
		t.Errorf(".gitattributes does not mark the log merge=union:\n%s", raw)
	}
}

func TestMergeUnionIsAppendedToAnExistingGitattributes(t *testing.T) {
	root, doc := repo(t)
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No trailing newline, on purpose: a rule glued onto the tail of another
	// rule is two broken rules.
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.png binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Log(doc, Record{Kind: KindApproved, Author: AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".gitattributes")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "*.png binary") {
		t.Errorf("the existing rule was destroyed:\n%s", raw)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 || lines[1] != "*.jsonl merge=union" {
		t.Errorf("want the union rule on its own line, got:\n%q", raw)
	}

	// And a second decision must not add it twice.
	if err := Log(doc, Record{Kind: KindApproved, Author: AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(dir, ".gitattributes")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), "*.jsonl") != 1 {
		t.Errorf(".gitattributes grew a duplicate rule:\n%s", again)
	}
}

// TestRepoRootFindsAWorktreeGitFile is the case an IsDir() check walks past. A
// git worktree's `.git` is a FILE, and galley's own development happens in
// worktrees whose ancestor is a BARE repository — so the naive check does not
// merely miss the root, it finds the wrong one, several levels up, shared by
// every sibling worktree.
func TestRepoRootFindsAWorktreeGitFile(t *testing.T) {
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, ".worktrees", "feature")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"),
		[]byte("gitdir: "+outer+"/.git/worktrees/feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(inner, "doc.md")
	if err := os.WriteFile(doc, []byte("# doc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := RepoRoot(doc)
	if err != nil {
		t.Fatal(err)
	}
	// t.TempDir can hand back a symlinked path (/var -> /private/var on
	// macOS); compare through EvalSymlinks so this is about the WALK.
	if resolve(t, root) != resolve(t, inner) {
		t.Fatalf("root = %s, want the worktree %s — the walk went past a .git FILE", root, inner)
	}
}

func resolve(t *testing.T, p string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return out
}

// TestAppendFailsSoft is THE INVARIANT'S ASSERTION: the ledger is memory,
// never truth. Every way the log can be unwritable returns an error the caller
// is free to ignore — no panic, no block, and nothing half-written. If this
// test ever needs a `recover()` to pass, the ledger has become load-bearing.
func TestAppendFailsSoft(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory is not read-only")
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("the ledger PANICKED where it must only return: %v", r)
			}
			close(done)
		}()

		// 1. No repository above the document at all.
		orphan := filepath.Join(t.TempDir(), "loose.md")
		if err := os.WriteFile(orphan, []byte("x"), 0o600); err != nil {
			t.Error(err)
			return
		}
		if err := Log(orphan, Record{Kind: KindApproved}); err == nil {
			t.Error("want an error with no repo above the document")
		}

		// 2. A repository whose root cannot be written to.
		root, doc := repo(t)
		if err := os.Chmod(root, 0o500); err != nil {
			t.Error(err)
			return
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
		if err := Log(doc, Record{Kind: KindApproved}); err == nil {
			t.Error("want an error when .galley cannot be created")
		}

		// 3. A record with no provenance is not a decision, and saying so is
		//    still a returned error.
		root2, doc2 := repo(t)
		if err := Log(doc2, Record{Author: AuthorAgent}); err == nil {
			t.Error("want an error for a record with no kind")
		}
		if _, err := os.Stat(filepath.Join(root2, Dir, LogName)); err == nil {
			t.Error("a refused record still created the log")
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the ledger BLOCKED — a failed append must return, not wait")
	}
}

// TestAppendIsAtomicPerLine runs the shape production has: many independent
// appenders, each with its own file handle, exactly as separate galley
// processes would. Every line must arrive whole and parseable, and none may
// interleave inside another.
func TestAppendIsAtomicPerLine(t *testing.T) {
	root, doc := repo(t)
	const writers, each = 16, 25

	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				// A long, distinctive payload: a short record could interleave
				// and still happen to parse.
				err := Log(doc, Record{
					Kind:   KindHand,
					Author: AuthorReviewer,
					Quote:  fmt.Sprintf("w%02d-i%02d-%s", w, i, strings.Repeat("x", 200)),
				})
				if err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	recs := readLog(t, filepath.Join(root, Dir, LogName))
	if len(recs) != writers*each {
		t.Fatalf("got %d lines, want %d", len(recs), writers*each)
	}
	seen := map[string]bool{}
	for _, rec := range recs {
		if seen[rec.Quote] {
			t.Fatalf("duplicate record %q", rec.Quote[:12])
		}
		seen[rec.Quote] = true
		if !strings.HasSuffix(rec.Quote, strings.Repeat("x", 200)) {
			t.Fatalf("a record was torn in half: %q", rec.Quote)
		}
	}
	if len(seen) != writers*each {
		t.Fatalf("%d distinct records, want %d", len(seen), writers*each)
	}
}

// TestNewlinesInAReasonStayOnOneLine: a decline note is free text a reviewer
// types, and a line break in it must not become a line break in a JSONL file.
func TestNewlinesInAReasonStayOnOneLine(t *testing.T) {
	root, doc := repo(t)
	if err := Log(doc, Record{
		Kind: KindDeclined, Author: AuthorAgent,
		Reason: "no.\n\nnot this one either\r\n",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, Dir, LogName)) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "\n"); n != 1 {
		t.Fatalf("%d newlines in a one-record log, want 1:\n%s", n, raw)
	}
	recs := readLog(t, filepath.Join(root, Dir, LogName))
	if recs[0].Reason != "no.\n\nnot this one either\r\n" {
		t.Errorf("the reason did not round-trip: %q", recs[0].Reason)
	}
}

// TestAuthorConstantsMatchReview guards the one bit of duplication this
// package accepts. review is imported HERE and nowhere in the package itself —
// it pulls the CRDT in behind it, which a decision log has no business
// carrying — so this is what stops the two vocabularies drifting.
func TestAuthorConstantsMatchReview(t *testing.T) {
	if AuthorReviewer != review.AuthorCourt {
		t.Errorf("ledger says %q, review says %q", AuthorReviewer, review.AuthorCourt)
	}
	if AuthorAgent != review.AuthorAgent {
		t.Errorf("ledger says %q, review says %q", AuthorAgent, review.AuthorAgent)
	}
}
