// decisions.go is this process's handle on galley's memory, and the flush that
// writes it out on the way out.
//
// NO COMMAND HERE DECIDES ANYTHING OFFLINE. Every decision is made through a
// running editor, which records it on the live path in
// internal/serve/ledger.go, through ledger.DefaultRecorder: the recorder this
// file flushes. The flush lives in the CLI because main is the one place that knows
// the process is about to exit.
//
// THE LEDGER IS MEMORY, NEVER TRUTH: nothing here returns an error, so no
// command in this package can fail for want of a record — see
// internal/ledger.Recorder.
package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/schuettc/galley/internal/debug"
	"github.com/schuettc/galley/internal/ledger"
)

// decisions is where this process remembers what it decided. A variable rather
// than a direct reach for ledger.DefaultRecorder so a test can substitute a
// recorder it can read — and one whose append FAILS, which is the only way to
// assert that a memory cannot take a decision down with it.
var decisions = ledger.DefaultRecorder

// flushDecisions is called once, from main, on the way out. A CLI process
// decides and exits within milliseconds, so without it the queued record would
// go with the process — the one loss the async recorder trades for never
// putting a decision behind a disk write.
//
// The deadline is generous and bounded: two seconds is far more than an append
// takes and far less than a person waits. A record that cannot be written in
// that time is dropped, because the alternative is a command that hangs on the
// filesystem after the work it was asked to do is already on disk.
//
// A LOSS IS SAID OUT LOUD, ON STDERR, AND NOWHERE ELSE. Every other loss in
// this package is silent by design — the ledger is memory and a memory may go —
// but a THIN record and a WRONG one are different things, and only the process
// that lost the line knows which one it just wrote. One sentence on the way out
// costs nothing, cannot fail a command (the exit code is untouched, and it is
// stderr so no `--json` consumer's stdout is disturbed), and is the difference
// between a stats table that undercounts and a stats table that lies.
// flushDebug drains the diagnostic sink on the way out, for flushDecisions'
// reason and with its deadline: the writes are queued behind a goroutine so a
// disk can never sit in front of a round, and a CLI process exits within
// milliseconds of the last one.
//
// IT SAYS NOTHING WHEN IT LOSES A LINE, and that is the one place it differs
// from flushDecisions. A dropped DECISION makes a stats table lie, so it is
// worth a sentence; a dropped DEBUG line is a line missing from a file the
// reviewer is reading with their own eyes, and a warning on stderr from a
// process that is only being watched would be output the debug mode itself
// caused. A nil sink (debug off) returns at once and writes nothing anywhere.
func flushDebug() {
	debug.Flush(2 * time.Second)
}

func flushDecisions() {
	drained := decisions.Flush(2 * time.Second)
	lost := decisions.Dropped()
	if drained && lost == 0 {
		return
	}
	switch {
	case !drained:
		fmt.Fprintln(os.Stderr,
			"galley: the decision log did not finish writing within 2s — the ledger may be missing this decision "+
				"(the review itself is on disk; the ledger is memory, never truth)")
	default:
		fmt.Fprintf(os.Stderr,
			"galley: %d decision record(s) were dropped — the ledger is missing them "+
				"(the review itself is on disk; the ledger is memory, never truth)\n", lost)
	}
}
