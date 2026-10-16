// debug.go is edit mode's half of the opt-in diagnostic record — what this
// server handed the agent, what came back, what it refused, what it cut, and
// whether anybody was listening.
//
// THE SURFACE IS METHODS THAT RETURN NOTHING, for internal/ledger's reason
// stated a second time: an error return is an invitation, and a call site that
// propagated one would turn a full disk into a refused Revise press. There is
// no error here to check, so no handler can be written to check it. The rest —
// the bounded queue, the goroutine, the recover — is internal/debug's.
//
// OFF COSTS ONE NIL CHECK. debug.Log's first line returns on a nil sink, and
// every payload below is built from values already in hand, so nothing here is
// worth guarding with debug.On() except a payload that would have to be
// COPIED to be recorded (the ack manifest is the only one, and it is small).
package serve

import (
	"github.com/subaud-tools/galley/internal/debug"
	"github.com/subaud-tools/galley/internal/versions"
)

// debugWhere names this process in the log. Two processes write into one file
// when GALLEY_DEBUG names the same path, and a line that did not say which one
// wrote it would be unreadable in exactly the situation this exists for.
const debugWhere = "edit"

// dbg stamps the fields every event from this server carries and hands the rest
// to the sink. It is the ONE place those three fields are filled, so an event
// added later cannot forget them.
func (s *EditServer) dbg(ev debug.Event) {
	ev.Doc = s.MdPath
	ev.Room = s.Room
	ev.Where = debugWhere
	debug.Log(ev)
}

// debugRoundSent records the round this server captured, with the asks exactly
// as they go into the version store.
//
// THIS IS NOT THE NOTIFICATION AND IS NOT MEANT TO BE. The rendered
// notification is written by whichever carrier delivered it (see
// cmd/galley/channel.go and cmd/galley/wait.go), and it is a SEPARATE line with
// the same Kind and a different Where — because the one question the
// 2026-08-22 reconstruction could not answer was whether what the agent read
// matched what the server sent, and a single merged line cannot be asked it.
func (s *EditServer) debugRoundSent(reason string, round int, fp string, asks []versions.Ask) {
	out := make([]debug.Ask, 0, len(asks))
	for _, a := range asks {
		out = append(out, debug.Ask{Key: a.Key, Text: a.Text, Quote: a.Quote})
	}
	s.dbg(debug.Event{
		Kind:         debug.KindRoundSent,
		Reason:       reason,
		Round:        round,
		Fingerprint:  fp,
		Asks:         out,
		Instructions: debug.Int(len(asks)),
	})
}

// debugAck records the agent's return AS RECEIVED — before validation, before
// the manifest drops anything, and whether or not the endpoint goes on to
// refuse it. A refused ack is the one this file most needs to hold: nothing
// else in galley records that it happened, and the agent's own report of it
// lives in a transcript belonging to another project.
func (s *EditServer) debugAck(round int, state, note string, changes []ackChange) {
	if !debug.On() {
		// The only payload here that has to be COPIED to be recorded. Guarded
		// so an off server does not walk the manifest at all.
		return
	}
	out := make([]debug.AckChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, debug.AckChange{Quote: c.Quote, Note: c.Note, Answers: c.Answers})
	}
	s.dbg(debug.Event{
		Kind:  debug.KindAckReceived,
		Round: round,
		Ack:   &debug.Ack{State: state, Note: note, Changes: out},
	})
}

// debugAnchorRefused records an instruction galley would not place, WITH THE
// TARGET TEXT it was asked to place it on.
//
// That refusal — `"…" matched 0 times, want exactly 1` out of
// suggest.findUnique — reached a toast in the reviewer's browser and nowhere
// else: not the server log, not the ledger, not the version store. Diagnosing
// one meant asking the reviewer what their browser had said.
func (s *EditServer) debugAnchorRefused(op, target string, err error) {
	if err == nil {
		return
	}
	s.dbg(debug.Event{
		Kind:   debug.KindAnchorRefused,
		Op:     op,
		Target: target,
		Err:    err.Error(),
	})
}

// debugVersionCut records a committed round: its number, its reason, and WHO
// MOVED THE DOCUMENT.
//
// The authors are the answer to "reviewer or agent" that is not an inference:
// roundAuthors observes it at the source (the agent's half in mutate, the
// reviewer's in doc.OnUpdate), where the version diff can only be read
// backwards and the instruction texts guessed from.
func (s *EditServer) debugVersionCut(r versions.Round) {
	s.dbg(debug.Event{
		Kind:    debug.KindVersionCut,
		Round:   r.N,
		Reason:  r.Reason,
		Authors: versions.Authors(r.Authors),
		Note:    r.Instruction,
		Answers: r.Answers,
	})
}

// debugWaiter records a blocking reader arriving or leaving, with the count
// this server holds AFTER the change.
//
// `Waiting() == 0 IS NOT "nobody is attached"` — a reader woken by an event
// re-arms immediately and briefly reads zero, which is what awaitReArm exists
// for. That caveat is the reason this is recorded as an EVENT with a count
// rather than sampled: a log of arrivals and departures can be read for the gap
// a single sample cannot see.
//
// It is called with waitMu HELD, which is safe and is the reason debug.Log is
// wait-free: an atomic load and a non-blocking send, no lock of its own and no
// filesystem anywhere near it.
func (s *EditServer) debugWaiter(kind debug.Kind, n int, note string) {
	s.dbg(debug.Event{Kind: kind, Waiting: debug.Int(n), Note: note})
}
