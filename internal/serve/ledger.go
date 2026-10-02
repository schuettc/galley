// ledger.go is edit mode's memory: every decision this server reaches, written
// to the repository's committed decision log at the moment it is reached.
//
// THE LEDGER IS MEMORY, NEVER TRUTH, and this file is where that stops being a
// slogan. Review state stays exactly where it was — the .md and its sidecar —
// and nothing here is on the path that puts it there. The whole surface is
// EditServer.remember, which returns nothing: there is no error for a handler
// to check, so no handler can be written to refuse an accept because a disk was
// full. internal/ledger.Recorder does the rest (bounded queue, its own
// goroutine, a recover around the append), and TestALedgerFailureCannotFailA
// Decision drives a server whose every append fails and asserts the decisions
// still land.
//
// LATENCY: THE DECISION PATH PAYS A CHANNEL SEND AND NOTHING ELSE. The append
// is a file write — a repo-root walk, a MkdirAll, a .gitattributes check and an
// O_APPEND write — and on a network mount or a wedged disk that is unbounded.
// None of it happens here. What happens here is building a small struct and
// handing it to a buffered channel, which is why remember can be called from
// inside a handler that has just decided forty proposals in one mutation
// without the reviewer waiting on forty file writes. The trade is stated in
// Recorder: a process that exits without flushing loses whatever is queued.
// `galley edit`'s shutdown and the CLI's exit both flush.
//
// EVERY RECORD IS TAKEN BEFORE THE MUTATION AND WRITTEN AFTER IT. Before,
// because a Pending is meaningful only against the model it was listed from —
// the ordinal-identity rule CLAUDE.md states, and the same reason handleDecline
// captures its text inside the transform. After, because a transform that runs
// and then fails to apply decided nothing, and a ledger that recorded it would
// be remembering something that never happened.
package serve

import (
	"strings"

	"github.com/schuettc/galley/internal/ledger"
	"github.com/schuettc/galley/internal/review"
)

// recorder is the ledger this server writes through — the process-wide default
// unless a caller (which in practice means a test) supplied its own.
func (s *EditServer) recorder() *ledger.Recorder {
	if s.Ledger != nil {
		return s.Ledger
	}
	return ledger.DefaultRecorder
}

// remember writes one decision. It returns nothing, on purpose: see the file
// comment.
//
// The REVIEW is this run's room — `<basename>-<token>`, minted per run of
// `galley edit` (newInstanceToken). It is the only identifier a session has
// that groups the decisions of one round together, and it is honest about what
// it names: a new run is a new round, and two rounds on one document are two
// values here rather than one.
func (s *EditServer) remember(rec ledger.Record) {
	rec.Review = s.Room
	s.recorder().Record(s.MdPath, rec)
}

// rememberAll writes several decisions, in the order they were reached.
func (s *EditServer) rememberAll(recs []ledger.Record) {
	for _, rec := range recs {
		s.remember(rec)
	}
}

// contextLimit bounds the `context` field, in RUNES.
//
// suggest.Pending.Context is the containing block plus its neighbours rendered
// back to markdown, which for a proposal inside a long code fence or a big
// table is kilobytes. The log is committed, append-only and grows forever, so
// one unbounded field per line is the difference between a file a reviewer can
// open and one they cannot. Two hundred runes is a paragraph's worth — enough
// to place a decision when reading the log by eye, which is the only thing this
// field is for; `quote`, `old` and `new` carry the decision itself and are
// never clipped.
const contextLimit = 200

// clipContext bounds a context field by RUNE, never by byte: a byte slice
// through a multi-byte character prints a replacement glyph, and a document is
// prose in whatever language it was written in.
func clipContext(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > contextLimit {
		return strings.TrimSpace(string(r[:contextLimit-1])) + "…"
	}
	return s
}

// ThreadRecord is one decision about one conversation.
//
// The AUTHOR is whoever opened it — the record's author names who authored the
// thing being decided, and for a thread that is the first entry's writer. A
// thread with no entries at all is a note reconciled off the file that nobody
// has spoken in yet; the reviewer is the honest default there, since a note in
// the file with no sidecar row was typed by hand.
//
// The QUOTE is the thread's heading — what the conversation is about — and
// falls back to its opening words when there is none. A heading is set on
// create and is usually the anchored text, but a thread reconciled off a note
// the sidecar had never opened a row for has nothing there, and a delete's
// record is, from the moment it lands, the whole of what is left to say the
// conversation existed. A row naming nothing would be a memory of nothing.
func ThreadRecord(kind ledger.Kind, th review.Thread) ledger.Record {
	author := review.AuthorCourt
	var opening string
	if len(th.Entries) > 0 {
		author = th.Entries[0].Author
		opening = th.Entries[0].Text
	}
	quote := th.Heading
	if strings.TrimSpace(quote) == "" {
		quote = opening
	}
	return ledger.Record{
		Kind:    kind,
		Author:  author,
		Quote:   quote,
		Context: clipContext(opening),
	}
}

// verdictRecord is the review's own ending, which is a decision about the
// ROUND and not about any one span — hence its own kinds, and hence the empty
// quote: there is no text it is about.
//
// IT NO LONGER STAMPS ITS OWN INSTANT, and that is the fix rather than a
// simplification. It used to be the only record on this path that did, while
// the proposals a trust verdict sweeps went out unstamped for Append to fill —
// on the WORKER, after the queue — so the verdict's `at` was strictly EARLIER
// than the proposals it had just swept, and Index.Decisions (ordered by
// at_unix) read the round back in an order that never happened. Recorder.Record
// stamps every record as it is handed over, which is decision time for all of
// them; the one record that still carries its own instant is a hand edit, whose
// instant is the reviewer's keystroke and is genuinely earlier than the save
// that reported it.
func verdictRecord(kind ledger.Kind, reason string) ledger.Record {
	return ledger.Record{
		Kind:   kind,
		Author: review.AuthorCourt,
		Reason: reason,
	}
}
