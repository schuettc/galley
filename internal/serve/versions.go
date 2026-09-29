package serve

// versions.go cuts rounds, serves their history, and can copy one back into the
// working draft without rewriting the record.
//
// COMMIT ON SEND is the one rule, and every cut in this file is a send: a
// Revise press hands the round to the agent, the agent's return hands it back,
// the wake a live settle fires sends without a press, and a verdict is the
// round the review ended on. Both modes use one mechanism — live simply sends
// more often.
//
// A SAVE IS NOT A SEND, and the difference is the whole of "a version is a
// round, not every save". Live mode used to cut on any projection whose digest
// had moved, so a reviewer typing prose committed a full copy of the document
// per export debounce while sending nothing to anybody. The live cut hangs off
// the notifier's own decision to wake the agent now — see EditServer.wakeSettle
// — which is the thing that actually sends, and which prose does not move.
//
// WHERE THE CUT HAPPENS IS THE WHOLE OF THE CORRECTNESS ARGUMENT. While a round
// is open the browser's CRDT is the live state and the file is a projection, so
// "the document at this instant" is a question only the projection can answer
// without tearing. So NOTHING HERE READS THE DOCUMENT TO MAKE A VERSION: a send
// RECORDS AN INTENT and then asks for a projection, and `project` — already
// holding the mutation mutex, already holding the exact bytes it is about to
// write to the author's file — is what cuts the version. A version is therefore
// always, byte for byte, a projection output. It can never be half a keystroke
// ahead of the .md, and it can never be what the agent did not read.
//
// (One function here does read the document, and it is not making a version:
// reviewerInstruction reads the open threads for the reviewer's own words, off
// the press path and the wake path, before mu is taken. It is a read of the
// CONVERSATION, not of the prose, and what it returns is recorded beside a
// version rather than becoming one.)
//
// A VERSION THAT COULD NOT BE WRITTEN MAY NOT FAIL A ROUND. This is the
// ledger's rule and it applies here for the same reason: the record is memory,
// and a read-only checkout, a full disk or a permissions error must not turn a
// Revise press into an error page. A failed cut is logged and the round goes on.

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/diff"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/ledger"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/versions"
)

// cutIntent is a send waiting for the next projection to write it down.
type cutIntent struct {
	reason      string
	instruction string
	answers     int
	// asks is the round's instructions with their keys, on the round that ASKED
	// them. See versions.Ask: the send that carries an instruction deletes its
	// thread, so this record is the only place the keys the agent was handed
	// still exist by the time it answers.
	asks []versions.Ask
	// changes is the agent's validated testimony, on the round that LANDED.
	// Only matchManifest puts anything here.
	changes []versions.Change
}

// Versions is this document's store. It creates nothing until a round is cut.
func (s *EditServer) Versions() *versions.Store {
	if s.versions == nil {
		s.versions = versions.Open(s.MdPath)
	}
	return s.versions
}

// requestCut records a send. Callers must NOT hold mu.
//
// It projects synchronously rather than waiting for the debounce, because the
// press is the instant being recorded and the agent is about to read the file:
// a version cut a debounce later would be a different document from the one
// that was handed over.
//
// AN INTENT MAY NEVER OUTLIVE THE PROJECTION IT ASKED FOR. project can return
// before it ever reaches the cut — a refused snapshot (ydoc.ErrConcurrentWrite),
// a failed write of the .md or of the sidecar — and an intent left lying in
// pendingCut would then be committed by the NEXT projection, against bytes this
// press never saw and after the press had already been told its round was v0.
// That is precisely the instruction-to-diff pairing the history exists for, so
// the intent is dropped here and the failure is logged: A VERSION THAT COULD
// NOT BE WRITTEN MAY NOT FAIL A ROUND, and it may not silently attach itself to
// a later one either.
func (s *EditServer) requestCut(reason, instruction string, answers int) {
	s.requestCutIntent(&cutIntent{reason: reason, instruction: instruction, answers: answers})
}

// requestCutIntent is requestCut for a send that carries more than the three
// things a bare cut does — the reviewer's asks, or the agent's testimony.
func (s *EditServer) requestCutIntent(in *cutIntent) int {
	s.mu.Lock()
	s.pendingCut = in
	s.lastCutN = 0
	err := s.project()
	missed := s.pendingCut != nil
	s.pendingCut = nil
	n := s.lastCutN
	s.mu.Unlock()
	if err != nil && s.Log != nil {
		s.Log(fmt.Sprintf("could not cut a version for this %s: %v", in.reason, err))
	}
	if missed && s.Log != nil {
		s.Log(fmt.Sprintf("the %s happened; the projection it asked for did not, so there is no version of it", in.reason))
	}
	return n
}

// markMovedByAgent records that the agent moved the document in this round.
// Callers hold mu.
func (s *EditServer) markMovedByAgent() { s.movedByAgent = true }

// appliedRound is the agent's round WHILE IT IS BEING WRITTEN.
//
// A PROPOSAL WAS ATOMIC AND A REVISION IS NOT, and that is the whole reason this
// type exists. Phase 1 could treat the agent's return as one event — the first
// projection whose pending fingerprint moved — because one `galley suggest` is
// one proposal and the reviewer decides it. A revision is six edits over ten
// seconds, so cutting on the first of them would commit a round that holds one
// sixth of the answer and leave the other five to be swept into whatever round
// came next, attributed to whoever pressed it. That is not "cut its round
// exactly once".
//
// SO THE AGENT'S SEND IS ITS RETURN, which is the same rule as everywhere else
// in this file — commit on send — with the agent's send being the thing the
// protocol already calls its answer: a terminal ack (`answered`, `declined`,
// `failed`), or the exception report. `galley ack --state answered` already
// means, in both carriers' own words, *"once your response is in the
// document"*.
//
// AND NO ROUND IS EVER LOST TO AN AGENT THAT NEVER RETURNS. If any other send
// happens while one of these is open — a Revise press, a live wake, a verdict —
// cutIfSending cuts it FIRST, by the landing-first rule already stated there,
// with whatever notes it had. An agent that goes quiet costs the pairing, never
// the round.
type appliedRound struct {
	// answers is the round this one replies to, captured when the agent first
	// moved the document: the open ask if there is one, otherwise the last round
	// cut, which is what a live exchange has instead of a press.
	answers int
	// said accumulates every --note across this round. One revision is several
	// edits and one sentence about the whole of it.
	said []string
	// changes is the agent's testimony about this round, as matchManifest left
	// it: what it could prove, and nothing else. See manifest.go.
	changes []versions.Change
}

func (a *appliedRound) intent(reason string) *cutIntent {
	return &cutIntent{
		reason:      reason,
		instruction: clip(strings.Join(a.said, " · "), instructionLimit),
		answers:     a.answers,
		changes:     a.changes,
	}
}

// openApplied notes that the agent has moved the document, and what it said
// about it. Callers must NOT hold mu.
func (s *EditServer) openApplied(note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = &appliedRound{answers: s.answering()}
	}
	if n := strings.TrimSpace(note); n != "" {
		s.applied.said = append(s.applied.said, n)
	}
}

// setAppliedChanges files the agent's validated testimony on the round it is
// about. Callers must NOT hold mu.
//
// A ROUND WITH NOTHING APPLIED HAS NOTHING TO TESTIFY ABOUT, so an ack landing
// with no accumulator open drops the manifest with it — which is the same
// answer matchManifest gives an entry it cannot place, reached one level up.
func (s *EditServer) setAppliedChanges(changes []versions.Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		return
	}
	s.applied.changes = changes
}

// answering is which round the agent is replying to. Callers hold mu.
//
// The open ask first — that is a press, and the press stamped its own round
// number on the watch precisely so the answer could point at it. Failing that,
// the last round cut, which is what LIVE mode has: there is no press, so the
// reviewer's instruction went into a `settled` round when the wake fired.
func (s *EditServer) answering() int {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	if s.watch != nil {
		return s.watch.round
	}
	return s.lastCutN
}

// cutApplied commits the agent's round (a landed one). Callers must NOT hold mu.
func (s *EditServer) cutApplied() int {
	s.mu.Lock()
	a := s.applied
	s.applied = nil
	s.mu.Unlock()
	if a == nil {
		return 0
	}
	return s.requestCutIntent(a.intent(versions.ReasonLanded))
}

// recordCannot is the exception: the agent could not do what was asked, so the
// round is the report and nothing moved. See handleCannot.
//
// It closes the reviewer's window for the same reason a terminal ack does —
// the ask has an answer now, and the answer is no — and it names the reason on
// the readout, because a round nobody opens is not where a reviewer finds out
// that nothing happened.
func (s *EditServer) recordCannot(why string) {
	s.reviseMu.Lock()
	s.cannotWhy, s.cannotAt = why, time.Now()
	s.watch = nil
	s.approveOnAnswer = false
	s.reviseMu.Unlock()
	// The exception ends the window too: the file is the reviewer's again, and
	// closing before the cuts below is what lets their projections write the
	// canonical document back to disk.
	s.closeHandoff()
	// Whatever the agent HAD applied before it gave up is its own round and is
	// cut first: partial work is still work, and rolling it into the exception
	// would say the document did not move when it did.
	s.cutApplied()
	s.mu.Lock()
	answers := s.answering()
	s.mu.Unlock()
	s.requestCut(versions.ReasonCouldNot, clip("could not: "+why, instructionLimit), answers)
}

// cutIfSending is project's own half, called with mu held and with the exact
// bytes the projection just wrote.
//
// TWO SENDS CAN LAND IN ONE PROJECTION, AND THEY ARE TWO ROUNDS. This used to
// be a switch, so an explicit press arriving in the same projection as the
// agent's return won it and the landing — with the number of the ask it was
// answering — was discarded: one press, one answer, one version, and a hole in
// a record whose only stated job is completeness. Both are cut, LANDING FIRST,
// because that is the order they happened in: the agent's work was in the CRDT
// before the press that shares this projection could have been made.
//
// The two rounds carry the same bytes, which is correct and not a duplicate —
// a round is a SEND, and an instruction with no edit beside it is still an
// instruction. See roundAuthors for who is named on which.
//
// A LIVE SETTLE IS NOT SEEN HERE AT ALL, and that is the fix for the defect
// this file shipped with: it used to cut a round on any live projection whose
// digest had moved, which is a round PER CHANGED SAVE — a reviewer typing prose
// cut a full copy of the document every 400ms export debounce, and sent nothing
// to anybody while doing it. The send in live mode is the notifier's own
// decision to wake the agent, so the cut hangs off that decision instead. See
// wakeSettle.
func (s *EditServer) cutIfSending(out []byte, landed bool, asked int) {
	intent := s.pendingCut
	s.pendingCut = nil

	var sends []*cutIntent
	// AN AGENT THAT NEVER RETURNED STILL GETS ITS ROUND, and it gets it FIRST,
	// which is this function's own rule applied to a second kind of landing: the
	// agent's work was in the CRDT before the send that shares this projection
	// could have been made. It is folded WITH the fingerprint landing rather
	// than cut beside it — an agent that both proposed and applied answered once
	// — and it keeps the notes it had, because a partial pairing is better than
	// none and none is what the alternative records.
	switch {
	case s.applied != nil && (landed || intent != nil):
		sends = append(sends, s.applied.intent(versions.ReasonLanded))
		s.applied = nil
	case landed:
		sends = append(sends, &cutIntent{reason: versions.ReasonLanded, answers: asked})
	}
	if intent != nil {
		sends = append(sends, intent)
	}
	if len(sends) == 0 {
		return
	}

	store := s.Versions()
	latest, ok, err := store.Latest()
	if err != nil {
		s.logVersionFailure(err)
		return
	}
	// THE OBSERVED MOVEMENT BELONGS TO THE FIRST ROUND CUT. Two sends in one
	// projection share their bytes, so naming the same movement on both would
	// say the document was moved twice. A press over a document nobody has
	// touched since the landing is a round with no author, which the store
	// tolerates by design: a round with nobody named is still a round.
	authors := s.roundAuthors()
	for _, send := range sends {
		// A LIVE SETTLE THAT MOVED NOTHING IS NOT A ROUND. An explicit press or
		// a landing is a round whether or not the words changed — an agent that
		// returned without changing anything is still an answer — but a settle
		// has no such claim on its own: the notifier legitimately wakes on a
		// pending set that moved without the prose moving with it.
		if send.reason == versions.ReasonSettled && send.instruction == "" && ok && latest.Digest == digestOf(out) {
			continue
		}
		r, err := store.Commit(string(out), versions.Round{
			At:          time.Now().UTC(),
			Authors:     authors,
			Reason:      send.reason,
			Instruction: send.instruction,
			Answers:     send.answers,
			Asks:        send.asks,
			Changes:     send.changes,
		})
		if err != nil {
			s.logVersionFailure(err)
			continue
		}
		// THE ARRIVAL THE REVIEWER IS TOLD ABOUT. A round the AGENT sent is news
		// on the reviewer's page — the document under their cursor just changed
		// — and a round they sent themselves is not. It is a number and not a
		// flag so the page can tell one arrival from the next without the server
		// holding any per-reader state: the browser remembers what it has seen.
		if r.Reason == versions.ReasonLanded || r.Reason == versions.ReasonCouldNot {
			s.lastLanded.Store(int32(r.N)) //nolint:gosec // a round number; a document never reaches 2^31 rounds
		}
		s.debugVersionCut(r)
		authors = nil
		latest, ok = r, true
		s.movedByAgent = false
		s.movedByReviewer.Store(false)
		s.lastCutN = r.N
		if s.Log != nil {
			who := versions.Authors(r.Authors)
			if who == "" {
				who = r.Reason
			}
			s.Log(fmt.Sprintf("v%d · %s", r.N, who))
		}
	}
}

// roundAuthors names who moved the document since the last cut.
//
// ALTERNATION IS A PROPERTY OF BATCHING, NOT A LAW. In live mode the CRDT
// merged both parties' work before anything was cut, so a round genuinely can
// carry both — and `v12 · court and agent` is information, not an evasion.
//
// Both halves are observed at the source rather than inferred from the bytes.
// The agent's: every server-side mutation on its behalf goes through mutate,
// which says so. The reviewer's: every update to the document that is neither a
// live read's no-op transaction nor one of this server's own writes came from
// the one peer there is. THE TRAIL IS NOT THE SIGNAL — it records typed TEXT
// and not formatting or structure, so a reviewer who only reordered a list
// would go unnamed.
func (s *EditServer) roundAuthors() []string {
	var out []string
	if s.movedByReviewer.Load() {
		out = append(out, versions.AuthorReviewer)
	}
	if s.movedByAgent {
		out = append(out, versions.AuthorAgent)
	}
	return out
}

func (s *EditServer) logVersionFailure(err error) {
	if s.Log != nil {
		s.Log(fmt.Sprintf("the round happened; the version did not reach disk: %v", err))
	}
}

func digestOf(b []byte) string { return fileDigest(b) }

// seedVersions cuts the document as galley first saw it, so the first real
// round has something to be a diff against.
//
// THE CONTENT IS THE SERIALIZED MODEL, NOT THE FILE'S OWN BYTES, and the caller
// passes it that way for the reason every other round is a projection output. A
// version is what galley READ, and the parse is where reading happens: a setext
// heading is a `#`, a `*` bullet is a `-`, `__bold__` is `**bold**`, and an
// inline {>>note<<} has been lifted into the sidecar and will never be written
// back. Seed from the raw file and the very first diff attributes all of that to
// the reviewer — including the disappearance of every margin note they wrote —
// on any document that did not already arrive in canonical form. Nothing else in
// the history can ever show it, because every later round is Serialize's output
// compared against another one.
//
// It also covers the case the spec calls out: an external write to the .md is
// still lost to the CRDT while a round is open, but there is somewhere to PUT
// it now — reopening the document records what was on disk as a version rather
// than as a deletion nobody can name. Canonicalising it first is what makes
// that record a diff of the WRITE rather than a diff of the writer's markdown
// dialect.
func (s *EditServer) seedVersions(content []byte) {
	store := s.Versions()
	latest, ok, err := store.Latest()
	if err != nil {
		s.logVersionFailure(err)
		return
	}
	if ok && latest.Digest == digestOf(content) {
		return
	}
	if _, err := store.Commit(string(content), versions.Round{
		At:     time.Now().UTC(),
		Reason: versions.ReasonOpened,
	}); err != nil {
		s.logVersionFailure(err)
	}
}

// reviewerInstruction is WHAT THE REVIEWER IS ASKING FOR, in their own words,
// at the moment the round is handed over.
//
// A COMMENT IS AN INSTRUCTION. It is composed here from the two populations the
// Revise button already counts — the reviewer's outstanding conversation and
// their direct edits — and NOT re-derived in the browser, so the history and
// the button cannot disagree about what was sent.
//
// IT IS STILL READ OFF THE THREADS, which is the only place it lives while the
// sidecar exists — but it is now SAID ONCE.
//
// AN INSTRUCTION BELONGS TO THE ROUND THAT CARRIED IT. Phase 1 recorded the
// outstanding conversation on every round, and stated the consequence rather
// than hiding it: an instruction the agent had not discharged was recorded on
// the next round too. PHASE 2 MAKES THAT WORSE RATHER THAN THE SAME, and that is
// why it is fixed here. A revision DISCHARGES an instruction — the thing it
// asked for happened — so nothing resolves the thread any more, and every later
// round in a session would repeat the same words forever: measured in a browser,
// `v4 · revise` carried `spell this out — it reads as a code identifier` for a
// second time, three rounds after the agent had already done it. A history whose
// every entry is paired with the same instruction is a history with no pairing.
//
// SO THE WORDS ARE MARKED AS SAID ONLY WHEN THE ROUND THAT CARRIES THEM IS
// ACTUALLY CUT. reviewerInstruction hands back a `mark` the caller runs after
// requestCut returns a real round number, for the reason requestCut drops an
// intent that outlived its projection: a version that could not be written may
// not fail a round, and it may not take the reviewer's words out of the NEXT one
// either.
//
// A REPLY REOPENS IT, and gets no special case: a reviewer who says something
// new says something this has not seen, so it is recorded on the next round.
// What is suppressed is the REPETITION, not the conversation.
//
// The direct-edit count is deliberately NOT deduplicated — it is a count of what
// is outstanding at this instant, not a sentence somebody wrote, and two rounds
// legitimately carry the same one.
//
// Callers must not hold mu.
func (s *EditServer) reviewerInstruction() (string, []ledger.Record, func()) {
	var said, fresh []string
	var records []ledger.Record
	s.instrMu.Lock()
	for _, th := range review.Read(s.doc) {
		if th.Resolved {
			continue
		}
		for _, e := range th.Entries {
			if e.Author != review.AuthorCourt {
				continue
			}
			t := strings.TrimSpace(e.Text)
			id := th.Key + "\x00" + e.At.UTC().Format(time.RFC3339Nano) + "\x00" + t
			if t == "" || s.instrSaid[id] {
				continue
			}
			said = append(said, t)
			fresh = append(fresh, id)
			records = append(records, ledger.Record{
				At: e.At.UTC(), Kind: ledger.KindInstruction, Author: ledger.AuthorReviewer,
				Text: t, Quote: strings.TrimSpace(th.Heading), Context: clipContext(th.Heading),
			})
		}
	}
	s.instrMu.Unlock()
	mark := func() {
		s.instrMu.Lock()
		defer s.instrMu.Unlock()
		if s.instrSaid == nil {
			s.instrSaid = map[string]bool{}
		}
		for _, t := range fresh {
			s.instrSaid[t] = true
		}
	}
	return clip(strings.Join(said, " · "), instructionLimit), records, mark
}

// instructionLimit bounds what one round records of the reviewer's words. It is
// a BOUND on a record, not a limit on what may be said: the conversation itself
// is untouched and the history is a list, not a transcript.
const instructionLimit = 600

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// roundView is one entry of the history, as the browser reads it.
type roundView struct {
	N           int    `json:"n"`
	At          string `json:"at"`
	Authors     string `json:"authors"`
	Reason      string `json:"reason"`
	Instruction string `json:"instruction"`
	// Answers is the round this one replies to, and Asked is that round's
	// instruction — resolved HERE rather than in the browser, so the pairing
	// has one implementation and the list has one shape.
	Answers int    `json:"answers"`
	Asked   string `json:"asked"`
	// Changed is how many regions this round moved against the one before it.
	// A count, not a diff: it is recomputed on every request from the two
	// documents, and nothing about it is stored.
	Changed int `json:"changed"`
}

type versionsView struct {
	Doc    string      `json:"doc"`
	Rounds []roundView `json:"rounds"`
}

// handleVersions serves THE HISTORY: one list, each entry paired with the
// instruction that produced it, and nothing else.
//
// THE HISTORY IS INSURANCE, NOT A FEATURE. Squashing, grouping, filtering and a
// timeline were all considered and are deliberately NOT BUILT — a live hour can
// produce forty rounds and the answer is not to build a browsing surface
// against a list nobody opens. If the noisy case ever becomes a real complaint,
// collapsing consecutive same-author rounds is the first thing to try. Recorded
// here so the option is known, not so it gets built.
func (s *EditServer) handleVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "the history is read-only", http.StatusMethodNotAllowed)
		return
	}
	rounds, err := s.Versions().List()
	if err != nil {
		http.Error(w, "could not read this document's rounds: "+err.Error(), http.StatusInternalServerError)
		return
	}
	asked := map[int]string{}
	for _, x := range rounds {
		if x.Instruction != "" {
			asked[x.N] = x.Instruction
		}
	}
	view := versionsView{Doc: s.docName(), Rounds: []roundView{}}
	for i, x := range rounds {
		rv := roundView{
			N: x.N, At: x.At.UTC().Format(time.RFC3339), Authors: versions.Authors(x.Authors),
			Reason: x.Reason, Instruction: x.Instruction, Answers: x.Answers,
			Asked: asked[x.Answers],
		}
		if i > 0 {
			rv.Changed = s.changedRegions(rounds[i-1].N, x.N)
		}
		view.Rounds = append(view.Rounds, rv)
	}
	writeJSON(w, view)
}

// changedRegions is the count the history shows beside a round. COMPUTED, NEVER
// STORED — it is read off the two documents on every request, so it cannot
// disagree with them.
func (s *EditServer) changedRegions(from, to int) int {
	a, err := s.Versions().Content(from)
	if err != nil {
		return 0
	}
	b, err := s.Versions().Content(to)
	if err != nil {
		return 0
	}
	a = historyDocument(a)
	b = historyDocument(b)
	return diff.Regions(diff.Diff(a, b))
}

// historyDocument returns the semantic document a historical snapshot holds.
//
// New reviewer rounds are cut after ClearInstructions, but snapshots written
// by older builds can contain `{==…==}` highlights and standalone instruction
// notes. Those were working-copy addresses, not prose changes. Normalizing on
// read keeps the append-only store and its digests intact while ensuring that
// History, change counts, and restoration all describe the document rather
// than Galley's former transport syntax.
func historyDocument(content string) string {
	model, _, err := markdown.Parse([]byte(content))
	if err != nil {
		// A historical file that cannot be parsed is still evidence. Show its raw
		// bytes rather than hiding the whole version behind a normalization error.
		return content
	}
	return string(markdown.Serialize(suggest.ClearInstructions(model)))
}

type diffView struct {
	From    int    `json:"from"`
	To      int    `json:"to"`
	View    string `json:"view"`
	HTML    string `json:"html"`
	Regions int    `json:"regions"`
	// Refused says the agent reported it could NOT do the work — the one state
	// in which an ask may be shown as unanswered. Every other landing answered,
	// and reading a missing manifest entry as a refusal is how two red cards
	// ended up over a revision that had plainly done what was asked.
	Refused bool `json:"refused"`
	Runs    int  `json:"runs"`
	// Changes is THE JOIN — one entry per thing the reader must account for in
	// this round: every change the agent testified about, paired with the asks
	// it says it answered, plus every ask nobody claimed. See changeView.
	Changes []changeView `json:"changes,omitempty"`
}

// changeView is one card of the History reading state: `-> the ask that
// produced it` above `<- the agent's note about it`, beside the mark on the
// paper it belongs to.
//
// REGION IS THIS RENDER'S ORDINAL AND NOTHING ELSE. It is the 0-based index of
// the region in the list just computed from the two documents, matching the
// data-gly-region the same render wrote into the HTML, and it is never stored:
// an ordinal renumbers, and CLAUDE.md's rule that an ordinal is not identity is
// exactly why versions.Change persists the agent's QUOTATION instead. Region is
// -1 for an ask-only entry, which is an ask the reviewer sent that no change
// claimed — the dashed refusal card. NOTHING THE REVIEWER SENT EVER
// DISAPPEARS: an ask nobody answered is the single most important thing this
// surface can show, and dropping it would make silence look like agreement.
type changeView struct {
	Region int      `json:"region"`
	Place  string   `json:"place,omitempty"`
	Asks   []string `json:"asks,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// handleVersionView renders one of the four readings of one round.
//
// THE SERVER COMPUTES AND THE BROWSER DISPLAYS. There is one implementation of
// the diff and it is internal/diff's; the browser is handed finished HTML and
// re-derives nothing. A second implementation would agree on every document
// anyone tried and disagree on the one that mattered — which is this
// repository's most expensive recurring defect, and the reason `Pending.
// Decidable` and `markdown.Substitutions` are both published by the side that
// computed them.
func (s *EditServer) handleVersionView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "a view is read-only", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	view := q.Get("view")
	if view == "" {
		view = string(diff.ViewClean)
	}
	if !diff.ValidView(view) {
		http.Error(w, fmt.Sprintf("no such view %q — try %s", view, viewNames()), http.StatusBadRequest)
		return
	}
	to, err := strconv.Atoi(q.Get("to"))
	if err != nil {
		http.Error(w, "which round? pass ?to=<n>", http.StatusBadRequest)
		return
	}
	rounds, err := s.Versions().List()
	if err != nil {
		http.Error(w, "could not read this document's rounds: "+err.Error(), http.StatusInternalServerError)
		return
	}
	from := 0
	if raw := q.Get("from"); raw != "" {
		if from, err = strconv.Atoi(raw); err != nil {
			http.Error(w, "from must be a round number", http.StatusBadRequest)
			return
		}
	} else {
		for _, x := range rounds {
			if x.N < to && x.N > from {
				from = x.N
			}
		}
	}
	after, err := s.Versions().Content(to)
	if err != nil {
		http.Error(w, fmt.Sprintf("there is no v%d of this document", to), http.StatusNotFound)
		return
	}
	before := ""
	if from > 0 {
		if before, err = s.Versions().Content(from); err != nil {
			http.Error(w, fmt.Sprintf("there is no v%d of this document", from), http.StatusNotFound)
			return
		}
	}
	after = historyDocument(after)
	before = historyDocument(before)
	// v1 is the baseline, not an insertion of the entire document into an empty
	// file. There is no earlier version to compare, so every requested reading
	// resolves to the clean starting document.
	if from == 0 {
		writeJSON(w, diffView{
			From: 0, To: to, View: string(diff.ViewClean),
			HTML: diff.Render(after, after, diff.ViewClean),
		})
		return
	}
	ops := diff.Diff(before, after)
	writeJSON(w, diffView{
		From: from, To: to, View: view,
		HTML:    diff.Render(before, after, diff.View(view)),
		Regions: diff.Regions(ops),
		Runs:    diff.Runs(ops),
		Changes: joinTestimony(ops, roundNamed(rounds, to), asksOf(rounds, roundNamed(rounds, to).Answers)),
		Refused: roundNamed(rounds, to).Reason == versions.ReasonCouldNot,
	})
}

// roundNamed is the round n, or a zero round when the list does not hold it.
func roundNamed(rounds []versions.Round, n int) versions.Round {
	for _, x := range rounds {
		if x.N == n {
			return x
		}
	}
	return versions.Round{}
}

// asksOf is the instructions round n carried, read off the round that ASKED
// them. The instruction threads were deleted by the send that carried them, so
// this record is the only place the keys still exist. See versions.Ask.
func asksOf(rounds []versions.Round, n int) []versions.Ask {
	if n <= 0 {
		return nil
	}
	return roundNamed(rounds, n).Asks
}

// joinTestimony pairs THE ASK THAT PRODUCED A CHANGE with THE AGENT'S NOTE
// ABOUT IT, and places both beside the region of this render they belong to.
//
// THE LOCATOR IS RE-MATCHED, NOT TRUSTED. versions.Change stores the agent's own
// quotation of what it wrote, and that quotation is put through the SAME
// matcher the ack used — changedTexts and soleRegion, called here rather than
// spelled again — against a diff computed fresh from the two documents a moment
// ago. A locator that now falls in no region, or in two, is DROPPED FROM THE
// JOIN and its change renders bare: the same rendering an unattributed change
// already gets, and the only honest one, because a note whose text is no longer
// on the page cannot be shown to be about anything in particular.
//
// AN ASK IS RESOLVED BY KEY AND NEVER BY POSITION. Change.Answers holds keys,
// Round.Asks holds keys, and the two lists are not parallel and were never
// promised to be — an agent may answer the third ask and not the first, and a
// positional read would then attribute its note to the wrong sentence while
// looking entirely correct.
//
// NOTHING THE REVIEWER SENT EVER DISAPPEARS. Every ask no joined change claimed
// comes back with Region -1 — the dashed ask-only card of the design — because
// an instruction that produced no visible change is the single most important
// thing this surface can tell a reviewer, and dropping it would make silence
// look like agreement. That includes an ask claimed only by a change whose
// locator no longer matches: the note is placeless, so the ask is unclaimed,
// and it is shown rather than quietly swallowed by testimony nobody can site.
func joinTestimony(ops []*diff.Op, landed versions.Round, asks []versions.Ask) []changeView {
	regions := changedTexts(ops)
	places := regionPlaces(ops)
	if len(regions) == 0 && len(asks) == 0 {
		return nil
	}
	said := make(map[string]string, len(asks))
	for _, a := range asks {
		said[a.Key] = a.Text
	}
	// The agent's testimony, indexed by the region it names. An entry naming a
	// region twice is the agent's own contradiction and the last one wins;
	// there is nothing to be gained by refusing the round over it.
	note := make(map[int]string, len(landed.Changes))
	spoke := make(map[int][]string, len(landed.Changes))
	claimed := make(map[string]bool, len(asks))
	for _, c := range landed.Changes {
		quote := diff.Norm(c.Locator)
		if quote == "" {
			continue
		}
		k := soleRegion(quote, regions)
		if k < 0 {
			continue
		}
		note[k] = c.Note
		for _, key := range c.Answers {
			text, ok := said[key]
			if !ok || text == "" {
				continue
			}
			spoke[k] = append(spoke[k], text)
			claimed[key] = true
		}
	}
	// A CARD PER REGION, AND THE SERVER OWNS THE LIST. What changed is computed
	// from the two documents and is known whether or not the agent said a word
	// about it — so a change the manifest never mentions is still a change the
	// reviewer made happen and must still be drawn, at its own place, in the
	// order it reads down the page. Walking the manifest instead drew NOTHING
	// for it, which is what a reviewer saw looking at their own edit beside an
	// empty rail.
	//
	// AN ABSENT NOTE IS ABSENT. There is deliberately no stand-in text here:
	// the change itself is already marked on the paper a few inches away, so a
	// body quoting it would say the same thing twice and dress silence up as an
	// answer. The card carries its head and its place, which is what makes it
	// navigable; the sentence appears when somebody actually wrote one.
	out := make([]changeView, 0, len(regions)+len(asks))
	for k := range regions {
		out = append(out, changeView{Region: k, Place: places[k], Note: note[k], Asks: spoke[k]})
	}
	// AND THEN THE ASKS NOBODY CLAIMED. Whether they were ANSWERED is the
	// round's own outcome and not this join's to decide — see refusedRound,
	// which is the only thing that may say no.
	for _, a := range asks {
		if a.Text == "" || claimed[a.Key] {
			continue
		}
		out = append(out, changeView{Region: -1, Asks: []string{a.Text}})
	}
	return out
}

// handleVersionRestore copies a historical document into the live working
// draft. It does not amend or delete a round; the next send records a normal
// new round from this restored draft.
func (s *EditServer) handleVersionRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.refuseSealed(w, verbReview) {
		return
	}
	if in.Version < 1 {
		http.Error(w, "which version? pass a positive version number", http.StatusBadRequest)
		return
	}
	if s.refuseHandoff(w) {
		return
	}
	if _, waiting := s.ReviseWatch(); waiting {
		http.Error(w, "a revision is still landing — wait for it before restoring a version", http.StatusConflict)
		return
	}
	pending, err := s.pending()
	if err != nil {
		writeBusy(w, err)
		return
	}
	if len(pending.Instructions) != 0 {
		http.Error(w, "send or delete the current instructions before restoring a version", http.StatusConflict)
		return
	}
	content, err := s.Versions().Content(in.Version)
	if err != nil {
		http.Error(w, fmt.Sprintf("there is no v%d of this document", in.Version), http.StatusNotFound)
		return
	}
	model, _, err := markdown.Parse([]byte(content))
	if err != nil {
		http.Error(w, fmt.Sprintf("could not read v%d: %v", in.Version, err), http.StatusInternalServerError)
		return
	}
	model = suggest.ClearInstructions(model)
	code, err := s.mutate(byReviewer, func(docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		return model, nil, nil
	})
	if err != nil {
		writeMutationError(w, code, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func viewNames() string {
	var names []string
	for _, v := range diff.Views {
		names = append(names, string(v))
	}
	return strings.Join(names, ", ")
}

func (s *EditServer) docName() string { return filepath.Base(s.MdPath) }
