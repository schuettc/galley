// ledger.go is the CLI over galley's memory: the committed log in the repo and
// the derived index on the machine.
//
// NOTHING HERE IS HOW A DECISION GETS RECORDED. The running editor writes to
// the log as it decides — internal/serve/ledger.go — so a record is in the
// repository the moment the reviewer acts, whether or not any of these verbs is
// ever run. `sync` moves the LOG into the INDEX, which is the direction the
// spec fixes: log first, then ingest, never the other way. A stale index is
// therefore the only staleness these verbs can have, and `rebuild` is the
// answer to any doubt about it.
//
// Three verbs, and the middle one is the point. `sync` is the ordinary
// incremental ingest. `stats` is the payoff — the GROUP BY questions the log
// cannot answer and was never meant to. `rebuild` is neither: it exists to
// PROVE the index is derived, by throwing it away and reconstructing it from
// the logs. A rebuild that produced a different answer would mean something in
// the index is truth living in the wrong store, and the spec's whole invariant
// — the ledger is memory, never truth — would be aspirational rather than
// checkable.
//
// NEVER GATED. Every command runs free — the feature-gate has been removed.
// For the rationale that applied when a gate existed: ledger is read-only,
// the log is committed to the user's own repository and readable with `cat`,
// and a record that becomes unreadable when a trial lapses is a record galley
// took hostage. `galley ledger sync` is also the command most likely to run
// from a cron or shell hook, where stderr noise and non-zero exits are wrong.
//
// It mutates nothing on a served document and reaches no running server, which
// is why it is free.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/schuettc/galley/internal/ledger"
)

const ledgerUsage = `Usage: galley ledger <sync|rebuild|stats> [flags]

  galley ledger sync [<path>] [--json]   ingest new decisions into the index
  galley ledger rebuild [--json]         wipe the index and re-read every log
  galley ledger stats [--json] [--top N] what the record says about the loop

The LOG is <repo>/.galley/decisions.jsonl — committed, append-only, one JSON
object per decision, readable with no tooling. It is authoritative, and it is
written AS DECISIONS ARE MADE: approve, decline, a change made by your own hand,
the instructions a round carried, and the review's verdict all land there without
any command here being run. Logs written before the rounds rewrite also carry
kinds no surface produces any more — reject, resolve, delete, entrust, reopen —
and the index still reads them, because a record of what happened does not stop
being true when the verb that wrote it is deleted.

The INDEX is a per-user SQLite store (~/.galley/ledger.db, or
$GALLEY_LEDGER_DIR) and is DERIVED: sync is how it catches up with the logs,
rebuild reconstructs it from scratch, and deleting it loses nothing. No review
ever requires either one.

<path> may be a document, a repository directory, or a decisions.jsonl. With
no path, sync reads every log the index already knows plus this repository's.
`

func runLedger(args []string, out, errw io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, ledgerUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "sync":
		return runLedgerSync(args[1:])
	case "rebuild":
		return runLedgerRebuild(args[1:])
	case "stats":
		return runLedgerStats(args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, ledgerUsage)
		return nil
	default:
		return fmt.Errorf("unknown ledger command %q\n\n%s", args[0], ledgerUsage)
	}
}

// ledgerArgs is parsePositional's interleaving loop with a RANGE instead of an
// exact count. Go's flag package stops parsing at the first non-flag argument,
// so `galley ledger sync doc.md --json` would leave --json sitting in the
// positionals and the flag unset — silently, which is the worst way for a
// --json flag to fail. Every other verb in this binary parses the same way; a
// new one that does not is a trap for whoever copies it next.
func ledgerArgs(fs *flag.FlagSet, args []string, max int) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	if len(positional) > max {
		return nil, fmt.Errorf("expected at most %d arguments, got %d", max, len(positional))
	}
	return positional, nil
}

// ledgerLog turns whatever the user pointed at into a log path. A document, a
// directory, or the .jsonl itself all work — the ledger is a thing people will
// reach for with the path already on their clipboard.
func ledgerLog(path string) (string, error) {
	if strings.HasSuffix(path, ".jsonl") {
		return filepath.Abs(path)
	}
	return ledger.LogPath(path)
}

func runLedgerSync(args []string) error {
	fs := flag.NewFlagSet("ledger sync", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the result as JSON")
	fs.Usage = func() { fmt.Fprint(os.Stderr, ledgerUsage) }
	pos, err := ledgerArgs(fs, args, 1)
	if err != nil {
		return usageErr(fs, err)
	}

	var extra []string
	if len(pos) == 1 {
		log, lerr := ledgerLog(pos[0])
		if lerr != nil {
			return lerr
		}
		extra = append(extra, log)
	} else if log, lerr := ledger.LogPath("."); lerr == nil {
		// This repository, if there is one. A cwd outside any repo is not an
		// error — the known sources are still worth syncing.
		extra = append(extra, log)
	}

	x, err := ledger.OpenIndex()
	if err != nil {
		return err
	}
	defer func() { _ = x.Close() }()

	results, err := x.SyncAll(extra...)
	if err != nil {
		return err
	}
	total, err := x.CountDecisions()
	if err != nil {
		return err
	}
	rows, err := x.Count()
	if err != nil {
		return err
	}
	return reportSync("synced", results, total, rows, *asJSON, x.Path())
}

func runLedgerRebuild(args []string) error {
	fs := flag.NewFlagSet("ledger rebuild", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the result as JSON")
	fs.Usage = func() { fmt.Fprint(os.Stderr, ledgerUsage) }
	if _, err := ledgerArgs(fs, args, 0); err != nil {
		// -h/--help/help is flag.ErrHelp, not a usage violation — preserve it so
		// Dispatch maps it to exit 0 (and renders the flags this fs.Usage prints)
		// rather than folding it into the synthetic "takes no arguments" error,
		// which app.Dispatch treats as a plain error (exit 1).
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageErr(fs, fmt.Errorf("rebuild takes no arguments — it re-reads every known log"))
	}

	x, err := ledger.OpenIndex()
	if err != nil {
		return err
	}
	defer func() { _ = x.Close() }()

	results, err := x.Rebuild()
	if err != nil {
		return err
	}
	total, err := x.CountDecisions()
	if err != nil {
		return err
	}
	rows, err := x.Count()
	if err != nil {
		return err
	}
	return reportSync("rebuilt", results, total, rows, *asJSON, x.Path())
}

// syncReport is the --json shape shared by sync and rebuild.
//
// BOTH numbers, because they are two facts and only one of them is what a
// person means. `rows` is the storage-level count, per source — this file, at
// this path, contained these lines. `total` is DECISIONS, collapsing the same
// decision recorded in several checkouts of one repository, which is the
// number to trust and the number the human form prints.
type syncReport struct {
	Index   string              `json:"index"`
	Action  string              `json:"action"`
	Sources []ledger.SyncResult `json:"sources"`
	Added   int                 `json:"added"`
	Total   int                 `json:"total"`
	Rows    int                 `json:"rows"`
}

func reportSync(action string, results []ledger.SyncResult, total, rows int, asJSON bool, indexPath string) error {
	added := 0
	for _, r := range results {
		added += r.Added
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(syncReport{
			Index: indexPath, Action: action,
			Sources: results, Added: added, Total: total, Rows: rows,
		})
	}

	if len(results) == 0 {
		fmt.Println("no decision logs known yet — `galley ledger sync <repo>` points it at one")
		return nil
	}
	for _, r := range results {
		switch {
		case r.Missing:
			fmt.Printf("  %s\n    no log yet\n", r.Path)
		default:
			notes := []string{fmt.Sprintf("+%d", r.Added)}
			if r.Skipped > 0 {
				notes = append(notes, fmt.Sprintf("%d unparseable", r.Skipped))
			}
			if r.Future > 0 {
				// The human half of ledger.Version's policy: the rows are kept
				// and indexed, and the reader is told the population is mixed.
				notes = append(notes, fmt.Sprintf("%d from a newer galley", r.Future))
			}
			if r.Torn {
				notes = append(notes, "last line still being written")
			}
			if r.Reset {
				notes = append(notes, "log shrank, re-read from zero")
			}
			fmt.Printf("  %s\n    %s\n", r.Path, strings.Join(notes, " · "))
		}
	}
	// The trailing number is DECISIONS, not rows — a decision recorded once
	// and cloned into five worktrees is one decision. `rows` rides the --json
	// shape for anyone who wants the storage-level fact.
	fmt.Printf("\n%s: %d log%s · %d new · %d decision%s in the index\n",
		action, len(results), plural(len(results)), added, total, plural(total))
	return nil
}

func runLedgerStats(args []string) error {
	fs := flag.NewFlagSet("ledger stats", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the rollup as JSON")
	top := fs.Int("top", 5, "how many decline reasons to list")
	fs.Usage = func() { fmt.Fprint(os.Stderr, ledgerUsage) }
	if _, err := ledgerArgs(fs, args, 0); err != nil {
		// See the identical guard in runLedgerRebuild: -h must stay flag.ErrHelp
		// (exit 0) rather than being folded into the synthetic usage error.
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageErr(fs, fmt.Errorf("stats takes no arguments"))
	}

	x, err := ledger.OpenIndex()
	if err != nil {
		return err
	}
	defer func() { _ = x.Close() }()

	s, err := x.Stats(*top)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	printStats(s)
	return nil
}

// printStats prints the rollup, and prints NOTHING it cannot support. An
// empty index says so rather than rendering a table of zeroes that reads like
// a finding.
func printStats(s ledger.Stats) {
	if s.Total == 0 {
		fmt.Println("nothing recorded yet — decisions are logged as they are made, " +
			"so either none have been or this index has not synced them yet (`galley ledger sync`)")
		return
	}
	fmt.Printf("%d decision%s · %d document%s · %d log%s\n",
		s.Total, plural(s.Total), s.Docs, plural(s.Docs), s.Sources, plural(s.Sources))
	// A REOPEN IS RECORDED AND IS NOT A DECISION, and this line is what keeps
	// the table's own arithmetic honest: `by kind` below counts every row, so
	// without saying how many rows the headline left out, the columns simply do
	// not add up and the reader has no way to find out why.
	if s.NotDecisions > 0 {
		decide := "records that decide"
		if s.NotDecisions == 1 {
			decide = "record that decides"
		}
		fmt.Printf("plus %d %s nothing (%s) — kept for the reason, never counted\n",
			s.NotDecisions, decide, kindList(ledger.NonDecisionKinds))
	}
	if s.First != "" {
		fmt.Printf("%s → %s\n", s.First, s.Last)
	}

	fmt.Println("\nby kind")
	for _, kc := range s.ByKind {
		// Wide enough for the longest kind in the vocabulary
		// (`verdict-entrusted`, 17), so the counts stay in one column: a table
		// whose numbers step right on the two longest words reads as two
		// tables.
		fmt.Printf("  %-17s %d\n", kc.Kind, kc.Count)
	}

	fmt.Println("\ntop decline reasons")
	if len(s.DeclineReasons) == 0 {
		fmt.Println("  (none stated — decline takes a --note, and it is the point of declining)")
	}
	for _, rc := range s.DeclineReasons {
		fmt.Printf("  %3d  %s\n", rc.Count, oneLine(rc.Reason))
	}

	a := s.Agent
	fmt.Printf("\nthe agent's proposals (%d)\n", a.Total)
	if a.Total == 0 {
		fmt.Println("  (none recorded)")
		return
	}
	fmt.Printf("  accepted   %3d   %.0f%%\n", a.Accepted, a.AcceptRate*100)
	fmt.Printf("  declined   %3d\n", a.Declined)
	// Its own line, not folded into declined: the difference between the two
	// verbs is whether a reason was given, and the table above is headed "top
	// decline reasons" precisely because that difference is the interesting one.
	fmt.Printf("  rejected   %3d   discarded with no reason given\n", a.Rejected)
	// The line the whole rollup exists for: a rewrite is neither an accept nor
	// a reject, and no counter in galley's review surface can see one. It
	// counts hand edits that landed ON A PROPOSAL and nothing else — the
	// reviewer's edits to their own prose are `edited` in the table above,
	// which is a fact about the round and not about anybody's proposal.
	fmt.Printf("  rewritten  %3d   rewritten by hand — no accept/reject counter sees these\n", a.Rewritten)
}

// oneLine flattens a decline reason for a terminal table. The reason is free
// text a reviewer typed; a newline in it would break the column alignment of
// every row after it.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	// Truncate by RUNE. A byte slice through a multi-byte character prints a
	// replacement glyph, and a reviewer's reason is prose in whatever language
	// they wrote it in.
	if r := []rune(s); len(r) > 72 {
		return string(r[:69]) + "…"
	}
	return s
}

// kindList names a kind set for a sentence — built from the list rather than
// spelled, so a second non-decision kind cannot be excluded from the count and
// left out of the explanation.
func kindList(kinds []ledger.Kind) string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return strings.Join(out, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
