package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/ledger"
)

// ledgerRepo builds a throwaway repository with a document and n decisions
// already logged, and points the index at a temp dir.
func ledgerRepo(t *testing.T, recs ...ledger.Record) (root, doc string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc = filepath.Join(root, "doc.md")
	if err := os.WriteFile(doc, []byte("# doc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if err := ledger.Log(doc, rec); err != nil {
			t.Fatal(err)
		}
	}
	return root, doc
}

// capture runs fn with stdout redirected and returns what it printed.
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	_ = r.Close()
	return sb.String(), runErr
}

func TestLedgerSyncThenStats(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	_, doc := ledgerRepo(t,
		ledger.Record{Kind: ledger.KindApproved, Author: ledger.AuthorAgent},
		ledger.Record{Kind: ledger.KindDeclined, Author: ledger.AuthorAgent, Reason: "out of scope"},
		ledger.Record{Kind: ledger.KindHand, Author: ledger.AuthorAgent},
	)

	out, err := capture(t, func() error { return runLedger([]string{"sync", doc}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+3") || !strings.Contains(out, "3 decisions in the index") {
		t.Fatalf("sync did not report the three decisions:\n%s", out)
	}

	out, err = capture(t, func() error { return runLedger([]string{"stats", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	var s ledger.Stats
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("stats --json is not JSON: %v\n%s", err, out)
	}
	if s.Total != 3 || s.Agent.Total != 3 || s.Agent.Accepted != 1 || s.Agent.Rewritten != 1 {
		t.Fatalf("stats = %+v", s)
	}
	if len(s.DeclineReasons) != 1 || s.DeclineReasons[0].Reason != "out of scope" {
		t.Fatalf("declineReasons = %+v", s.DeclineReasons)
	}

	// The human form has to say the thing the rollup exists to say.
	out, err = capture(t, func() error { return runLedger([]string{"stats"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"by kind", "top decline reasons", "out of scope", "rewritten"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats is missing %q:\n%s", want, out)
		}
	}
}

// TestLedgerSyncIsIncrementalAcrossInvocations: the CLI is the shape this
// actually runs in — separate processes, each opening the index fresh.
//
// THE FIRST VERSION OF THIS TEST COULD NOT FAIL, and the reason is worth as
// much as the check. It synced one record, appended a DIFFERENT one, synced
// again and asserted `added == 1 && total == 2`. Both numbers are what a
// WHOLLY NON-INCREMENTAL sync produces too: re-reading from byte zero finds
// the first record already present, `UNIQUE (source, digest)` rejects it, and
// the insert count is 1 either way. The assertion was about DEDUPE, under a
// name that claims something dedupe guarantees on its own.
//
// So the check is now about the BYTES, which is the only place the two
// behaviours differ. After the first sync the already-ingested line is
// overwritten IN PLACE with garbage of the same length — same length so the
// shrink branch (a branch switch or a revert, which correctly re-reads from
// zero) is not what is being measured — and a valid record is appended after
// it. An incremental sync never looks at those bytes again: it starts at the
// stored offset, reads one good line, and reports nothing skipped. A sync that
// re-read from zero would meet the garbage and report `skipped >= 1`.
//
// A hand-edited log is not a hypothetical shape — CLAUDE.md's ledger entry
// records that the log is committed, cloned into every worktree, and merged
// with `merge=union`, which is why the shrink branch exists at all.
func TestLedgerSyncIsIncrementalAcrossInvocations(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	root, doc := ledgerRepo(t, ledger.Record{Kind: ledger.KindApproved, Author: ledger.AuthorAgent})

	if _, err := capture(t, func() error { return runLedger([]string{"sync", doc}, os.Stdout, os.Stderr) }); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, ".galley", "decisions.jsonl")
	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	first := bytes.IndexByte(before, '\n')
	if first < 0 {
		t.Fatalf("the log has no complete first line — the fixture stopped exercising anything:\n%s", before)
	}
	// Same length, so the file does not shrink and the re-read-from-zero branch
	// this test is NOT about stays shut.
	garbled := append(bytes.Repeat([]byte("x"), first), before[first:]...)
	if err := os.WriteFile(logPath, garbled, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ledger.Log(doc, ledger.Record{Kind: ledger.KindDeclined, Author: ledger.AuthorAgent, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	// No path this time: the source is already known.
	out, err := capture(t, func() error { return runLedger([]string{"sync", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	// `skipped` IS PER SOURCE AND NOT AT THE TOP LEVEL, which the first cut of
	// this got wrong — and getting it wrong reproduced the exact defect being
	// fixed: a field that is not in the payload unmarshals to 0, so the
	// assertion read 0, passed, and could never have failed. Measured by
	// forcing Sync to re-read from byte zero: still green.
	var rep struct {
		Added   int `json:"added"`
		Total   int `json:"total"`
		Sources []struct {
			Path    string `json:"path"`
			Skipped int    `json:"skipped"`
			Reset   bool   `json:"reset"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	// SELECTED BY PATH, because a bare `sync` syncs EVERY source the index has
	// remembered and this checkout's own committed decision log is one of them.
	// Asserting on `sources[0]` read whichever the query happened to order
	// first — a check whose subject depends on the machine it runs on.
	found := false
	for _, src := range rep.Sources {
		if src.Path != logPath {
			continue
		}
		found = true
		if src.Reset {
			t.Fatalf("the log was re-read from zero because it SHRANK — the fixture changed its length, so this is not measuring incrementality:\n%s", out)
		}
		if src.Skipped != 0 {
			t.Errorf("the second sync skipped %d line(s) — it re-read bytes it had already ingested:\n%s",
				src.Skipped, out)
		}
	}
	if !found {
		t.Fatalf("the fixture's own log is not in the report at all:\n%s", out)
	}
	if rep.Added != 1 || rep.Total != 2 {
		t.Fatalf("added %d total %d, want 1 and 2 — a second sync duplicated rows", rep.Added, rep.Total)
	}
}

func TestLedgerRebuildReproducesTheIndex(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	_, doc := ledgerRepo(t,
		ledger.Record{Kind: ledger.KindApproved, Author: ledger.AuthorAgent},
		ledger.Record{Kind: ledger.KindEntrusted, Author: ledger.AuthorAgent},
	)
	if _, err := capture(t, func() error { return runLedger([]string{"sync", doc}, os.Stdout, os.Stderr) }); err != nil {
		t.Fatal(err)
	}
	before, err := capture(t, func() error { return runLedger([]string{"stats", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}

	out, err := capture(t, func() error { return runLedger([]string{"rebuild", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Added int `json:"added"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Added != 2 || rep.Total != 2 {
		t.Fatalf("rebuild read back %d of %d — it must re-read every log from zero", rep.Added, rep.Total)
	}

	after, err := capture(t, func() error { return runLedger([]string{"stats", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("rebuild changed the answers:\nbefore %s\nafter  %s", before, after)
	}
}

// TestLedgerSyncTakesADocumentADirectoryOrTheLogItself — the ledger is a thing
// people reach for with a path already on their clipboard.
func TestLedgerSyncTakesADocumentADirectoryOrTheLogItself(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	root, doc := ledgerRepo(t, ledger.Record{Kind: ledger.KindApproved, Author: ledger.AuthorAgent})
	log := filepath.Join(root, ".galley", "decisions.jsonl")

	for _, arg := range []string{doc, root, log} {
		out, err := capture(t, func() error { return runLedger([]string{"sync", arg, "--json"}, os.Stdout, os.Stderr) })
		if err != nil {
			t.Fatalf("sync %s: %v", arg, err)
		}
		var rep struct {
			Sources []struct {
				Path string `json:"path"`
			} `json:"sources"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatal(err)
		}
		if rep.Total != 1 {
			t.Fatalf("sync %s indexed %d decisions, want 1", arg, rep.Total)
		}
		if len(rep.Sources) != 1 {
			t.Fatalf("sync %s found %d sources, want the one log — the three forms did not resolve alike", arg, len(rep.Sources))
		}
	}
}

func TestLedgerStatsOnAnEmptyIndexSaysSo(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	out, err := capture(t, func() error { return runLedger([]string{"stats"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing recorded yet") {
		t.Fatalf("an empty index must say so rather than print a table of zeroes:\n%s", out)
	}
}

// TestLedgerSubVerbsHelpIsNotAnError pins the fix round 2 regression: giving
// the top-level `ledger` registry entry a Synopsis/Help/NewFlags made
// tools.App intercept -h anywhere in a command's args and render TOP-LEVEL
// help, so `galley ledger rebuild -h` and `galley ledger stats -h` never
// reached ledgerArgs's own flag.ErrHelp and instead fell into the synthetic
// "takes no arguments" usageErr — a plain UsageError that main's Dispatch
// maps to exit 2 rather than the exit 0 a `-h` must produce. The real fix is
// making the ledger registration Summary-only (see main.go); this
// test pins the sub-verb behavior directly against runLedger, which is what
// every registration variant ultimately calls. `sync -h` already worked
// (runLedgerSync's usageErr(fs, err) passes flag.ErrHelp through unchanged)
// and is included here as the control case.
func TestLedgerSubVerbsHelpIsNotAnError(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	for _, sub := range []string{"sync", "rebuild", "stats"} {
		t.Run(sub, func(t *testing.T) {
			_, err := capture(t, func() error { return runLedger([]string{sub, "-h"}, os.Stdout, os.Stderr) })
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("runLedger([%q, -h]) = %v, want flag.ErrHelp (exit-0-equivalent)", sub, err)
			}
		})
	}
}

func TestLedgerRejectsAnUnknownSubcommand(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	if err := runLedger([]string{"pillage"}, os.Stdout, os.Stderr); err == nil {
		t.Fatal("want an error for an unknown ledger command")
	}
	if err := runLedger([]string{"rebuild", "somepath"}, os.Stdout, os.Stderr); err == nil {
		t.Fatal("rebuild takes no arguments and must say so")
	}
}

// TestLedgerReportsDecisionsNotRows is the worktree case at the CLI. Court
// works in worktrees constantly, so the same committed log sits at many source
// paths on one machine — and the number the command prints has to be
// decisions, with the storage-level row count still available in --json for
// anyone who wants the per-source fact.
func TestLedgerReportsDecisionsNotRows(t *testing.T) {
	t.Setenv("GALLEY_LEDGER_DIR", t.TempDir())
	rootA, _ := ledgerRepo(t,
		ledger.Record{Kind: ledger.KindApproved, Author: ledger.AuthorAgent, Quote: "one"},
		ledger.Record{Kind: ledger.KindDeclined, Author: ledger.AuthorAgent, Reason: "no", Quote: "two"},
	)
	logA := filepath.Join(rootA, ".galley", "decisions.jsonl")

	// A second checkout of the same repository: identical bytes, new path.
	worktree := t.TempDir()
	logB := filepath.Join(worktree, ".galley", "decisions.jsonl")
	if err := os.MkdirAll(filepath.Dir(logB), 0o755); err != nil {
		t.Fatal(err)
	}
	shared, err := os.ReadFile(logA) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logB, shared, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := capture(t, func() error { return runLedger([]string{"sync", logA}, os.Stdout, os.Stderr) }); err != nil {
		t.Fatal(err)
	}
	out, err := capture(t, func() error { return runLedger([]string{"sync", logB, "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Total int `json:"total"`
		Rows  int `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Total != 2 {
		t.Errorf("total = %d, want 2 decisions — a second checkout must not double-count", rep.Total)
	}
	if rep.Rows != 4 {
		t.Errorf("rows = %d, want 4 — the per-source record is the honest one and stays", rep.Rows)
	}

	// Named, not bare: a bare `sync` also registers the repository this test
	// binary is running inside, which would make the source count this test
	// asserts a fact about the developer's cwd.
	human, err := capture(t, func() error { return runLedger([]string{"sync", logB}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human, "2 decisions in the index") {
		t.Errorf("the human form must print decisions, not rows:\n%s", human)
	}

	out, err = capture(t, func() error { return runLedger([]string{"stats", "--json"}, os.Stdout, os.Stderr) })
	if err != nil {
		t.Fatal(err)
	}
	var s ledger.Stats
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if s.Total != 2 || s.Agent.Total != 2 || s.Agent.Accepted != 1 || s.Agent.Declined != 1 {
		t.Errorf("stats = %+v, want the two decisions counted once each", s)
	}
	if s.Sources != 2 {
		t.Errorf("sources = %d, want 2 — the source count IS per source", s.Sources)
	}
}
