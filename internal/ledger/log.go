// Package ledger is galley's memory: what was decided, by whom, and why.
//
// It is two stores and ONE DIRECTION between them, and the direction is the
// whole design (docs/superpowers/specs/2026-08-15-the-ledger.md):
//
//   - THE LOG — `<repo>/.galley/decisions.jsonl`, committed, append-only, one
//     JSON object per decision. Authoritative. Readable with no tooling, and
//     it travels with the branch, so a teammate who clones the repo gets the
//     decision history without needing anything from the machine it was made
//     on. This file.
//   - THE INDEX — a per-user SQLite store, DERIVED, never written to
//     independently. index.go.
//
// A failed ingest therefore leaves the log correct and the next ingest catches
// up: drift is self-healing rather than a class of bug.
//
// THE LEDGER IS MEMORY, NEVER TRUTH, and that is not a slogan — it is the
// contract every function here is written to. Review state stays where it has
// always been: the .md and its sidecar, files that diff and travel with the
// branch. No review path may ever REQUIRE this package. So every failure here
// is a returned error the caller is free to drop on the floor — never a panic,
// never a block, never a partially-applied review. `Append` on a read-only
// filesystem loses a memory; it must not lose a decision. See
// TestAppendFailsSoft, which is the assertion behind that sentence.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is the record's schema version, written as `v` on every line. A
// reader that meets a version it does not know keeps the line (the log is
// authoritative and lines are never rewritten) and is free to ignore fields it
// cannot interpret.
const Version = 1

// Kind is a decision's provenance. A decision without it is an anecdote — the
// spec's word — because "the proposal is gone from pending" is true of all
// four of these and means something different every time.
type Kind string

const (
	// KindApproved: the reviewer accepted the proposal as written.
	KindApproved Kind = "approved"
	// KindDeclined: the reviewer settled it without applying it, and said
	// why. The reason is the single most valuable field in this file.
	KindDeclined Kind = "declined"
	// KindHand: the reviewer rewrote AN AGENT'S PROPOSAL directly rather than
	// deciding it — the verdict by hand, and the fate no accept/reject counter
	// in galley can see. Its AUTHOR IS THE AGENT, like every other kind with
	// something to be about: the author names who wrote the thing being
	// decided, and what was decided here is a proposal. That is what puts it in
	// the agent rollup as `rewritten`, which is the line the whole rollup
	// exists for. (This used to read "like every other kind here", which was
	// not true of the verdicts or of reopen — see DeciderKinds and the Author
	// field's own comment.)
	//
	// A hand edit on the reviewer's OWN prose is KindEdited below, and the two
	// must never be spelled one word: counting every trail entry as `hand`
	// would answer "what became of my proposals" with a number that mostly
	// measures how much the reviewer wrote — a numerator and a denominator
	// about different populations, which is the exact defect AgentFate's
	// doc comment warns against.
	KindHand Kind = "hand"
	// KindEdited: the reviewer edited their own prose — a change they made
	// that no proposal was ever about. Recorded because it is the reviewer's
	// own record of what this round cost them; OUTSIDE ProposalKinds because
	// it decides nothing about anybody's proposal, and a rollup that counted
	// it would have a denominator answering a different question from its
	// numerator.
	KindEdited Kind = "edited"
	// KindEntrusted: applied under the trust verdict, with the reviewer gone.
	KindEntrusted Kind = "entrusted"
	// KindRejected: the proposal was discarded WITHOUT a reason. `reject` is
	// decline minus the record — CLAUDE.md calls it "the record-free discard of
	// an AGENT proposal", and that is a fact about the DOCUMENT and its
	// sidecar, which gain nothing: no thread, no note, nothing an agent reading
	// the file can find. The ledger is the one place it is not record-free, and
	// has to be: "which of my proposals were thrown away without comment" is
	// the same family of question as "which were declined and why", and a
	// vocabulary that could not tell a reject from a decline would answer the
	// second one wrong.
	KindRejected Kind = "rejected"

	// A CONVERSATION IS NOT A PROPOSAL, so settling one is not approving
	// anything. Both of these carry an author (whoever opened the thread) and
	// neither belongs in the agent's proposal rollup — see AgentFate.
	//
	// KindResolved: the thread was settled, keeping every word of it.
	KindResolved Kind = "resolved"
	// KindDeleted: the thread was removed, and its trace in the document with
	// it. Irreversible outside git, which is why it is its own word.
	KindDeleted Kind = "deleted"

	// THE REVIEW'S OWN VERDICT, which is a decision about the ROUND rather than
	// about any one span. Separate words from the per-proposal kinds above
	// because they are separate acts: a review approved once over eleven
	// approved proposals is twelve decisions, and a vocabulary that spelled
	// both "approved" would report twelve approvals of eleven proposals.
	KindVerdictApproved  Kind = "verdict-approved"
	KindVerdictEntrusted Kind = "verdict-entrusted"
	KindVerdictDiscarded Kind = "verdict-discarded"
	// KindVerdictRevise: sent back for another round. The one verdict that
	// does not end the review, and the count that says how many rounds it took.
	KindVerdictRevise Kind = "verdict-revise"

	// KindReopened is NOT A DECISION and is recorded anyway. A reopen decides
	// nothing — it un-ends a review — but it is the only thing that can say a
	// verdict did not hold, an agent's reopen is REQUIRED to carry a reason
	// (internal/serve/seal.go), and the reason is the single most valuable
	// field in this file. It has its own word precisely so a consumer counting
	// DECISIONS can leave it out, which a consumer that had to guess from a
	// blank field could not.
	KindReopened Kind = "reopened"

	// KindInstruction is what the reviewer asked for, attached permanently to
	// the round on which they wrote it. It records process, not a decision: the
	// revision discharges it and nothing resolves or re-anchors it afterward.
	KindInstruction Kind = "instruction"
)

// AllKinds is every kind a call site writes, in vocabulary order. It exists so
// a test can assert the words are distinct — with twelve of them a copy-paste
// duplicate is a real way to silently merge two acts in every GROUP BY — and so
// a reader has one list rather than a walk of the const block.
var AllKinds = []Kind{
	KindApproved, KindDeclined, KindHand, KindEdited, KindEntrusted, KindRejected,
	KindResolved, KindDeleted,
	KindVerdictApproved, KindVerdictEntrusted, KindVerdictDiscarded, KindVerdictRevise,
	KindReopened,
	KindInstruction,
}

// NonDecisionKinds is every kind that is RECORDED and is not a DECISION, and it
// exists so that sentence is enforced somewhere rather than only asserted in a
// doc comment. `reopened` earned its own word on exactly one argument — "so a
// consumer counting decisions can leave it out" — and galley's own consumer
// (`galley ledger stats`, Index.CountDecisions) counted it anyway for a
// release. Every count of DECISIONS filters on this list; the by-kind tally
// deliberately does not, because it is a record of what is in the log.
var NonDecisionKinds = []Kind{KindReopened, KindInstruction}

// ProposalKinds is the subset that names what became of ONE PROPOSAL. It is the
// agent rollup's population — see AgentFate — and the reason that rollup's
// denominator cannot drift when a thread is resolved or a review is sealed.
var ProposalKinds = []Kind{KindApproved, KindEntrusted, KindDeclined, KindRejected, KindHand}

// DeciderKinds is every kind whose `author` names WHO DECIDED rather than who
// wrote the thing decided — and it exists because that is a SECOND reading of
// one field, which nothing said out loud until it was audited.
//
// The rule for every other kind is the one KindHand's comment states: the
// author names whoever WROTE the thing being decided. `approved`, `rejected`,
// `declined`, `entrusted` and `hand` carry the proposal's author (normally the
// agent); `resolved` and `deleted` carry whoever opened the thread. Deciding is
// the reviewer's act by construction, so recording the decider there would be a
// column with one value in it — the author is the only place the OTHER party
// can be named, and naming them is what makes "what became of my proposals"
// answerable at all.
//
// THE KINDS IN THIS LIST HAVE NO OTHER PARTY. A verdict is about the ROUND and
// a reopen un-ends one; neither has a span, a thread or a proposal anybody
// wrote, so "whoever wrote the thing being decided" has no referent and the
// decider is the only honest answer. `reopened` is the one place that answer is
// genuinely two-valued and worth having — an agent may reopen, and must say why.
//
// It is NOT a defect to be fixed by moving values around: there is nothing to
// move them to. It is a fact a consumer has to know, so it is a LIST rather
// than a sentence, disjoint from ProposalKinds by assertion — which is the
// property that makes AgentFate's `WHERE author = agent AND kind IN
// ProposalKinds` sound, since no row it can reach is authored by its decider.
//
// **A QUERY THAT GROUPS BY AUTHOR ACROSS BOTH HALVES IS COUNTING TWO DIFFERENT
// THINGS IN ONE COLUMN.** There is no such query today — the only one that
// reads `author` at all is the agent rollup, and it is scoped to ProposalKinds.
// Anything new that reads the field has to pick a side of this list first.
var DeciderKinds = []Kind{
	KindVerdictApproved, KindVerdictEntrusted, KindVerdictDiscarded, KindVerdictRevise,
	KindReopened,
}

// Authors. These MIRROR review.AuthorCourt / review.AuthorAgent and are
// restated rather than imported: internal/review pulls the CRDT in behind it,
// and this package has no business carrying that weight for two string
// constants. TestAuthorConstantsMatchReview imports review in the TEST alone
// and asserts the two vocabularies have not drifted.
const (
	AuthorAgent    = "agent"
	AuthorReviewer = "court"
)

// Record is one decision. THE FIELD ORDER IS THE JSON ORDER and is part of the
// format: encoding/json emits struct fields in declaration order, so two
// galley builds append lines that diff cleanly against each other.
//
// Nothing is `omitempty`, deliberately. A line with every key present is
// greppable (`grep '"kind":"declined"'` finds every one), reads the same at
// every position in the file, and cannot be told apart from a line written by
// an older build that simply had nothing to say in that field. The cost is a
// few bytes per line in a file that gains one line per decision.
type Record struct {
	V      int       `json:"v"`
	At     time.Time `json:"at"`
	Doc    string    `json:"doc"`
	Review string    `json:"review"`
	Kind   Kind      `json:"kind"`
	// Author NAMES A DIFFERENT PARTY DEPENDING ON KIND, and that is the one
	// thing about this record a consumer cannot work out from the field itself.
	//
	// For every kind with something to be about it is the party who WROTE the
	// thing being decided — the proposal's author, the thread's opener — never
	// the reviewer who decided it. For the kinds in DeciderKinds there is no
	// such party (a verdict is about the round; a reopen un-ends one) and it is
	// the decider instead. See DeciderKinds, which is that split as a list so
	// a new kind has to be put on one side of it.
	//
	// The vocabulary is TWO WORDS, AuthorAgent and AuthorReviewer, at every
	// site — serve.ledgerAuthor is the boundary that keeps a third one out.
	Author  string `json:"author"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Reason  string `json:"reason"`
	Quote   string `json:"quote"`
	Context string `json:"context"`
	Round   int    `json:"round"`
	Text    string `json:"text"`
}

// Digest identifies a decision BY ITS CONTENT — the key the index dedupes on,
// and the answer to what `merge=union` does across repositories.
//
// The union merge is correct for concurrent appends and keeps BOTH sides'
// lines, which means a cherry-pick, a rebase replay, or a merge of two branches
// that each recorded the same decision leaves that record in the log twice at
// different line numbers. Position can never see that, so content has to.
//
// EVERY FIELD PARTICIPATES. A digest that skipped one would collapse two
// decisions that differ only in it — `old`/`new` on two hand edits at the same
// instant, or two declines with different reasons — and the whole point is to
// remove duplicates without ever removing a decision.
//
// It hashes the PARSED fields rather than the raw line, so a record
// reserialized by another galley build (different spacing, a field order that
// moved) still matches the one already indexed. And each field is
// LENGTH-PREFIXED rather than separated by a delimiter: a delimiter can occur
// inside a reviewer's decline note, and "ab"+"c" and "a"+"bc" must not hash
// alike. Length prefixes make the encoding injective, so a collision needs a
// SHA-256 collision rather than an unlucky quote.
func (r Record) Digest() string {
	h := sha256.New()
	field := func(s string) {
		_, _ = fmt.Fprintf(h, "%d:", len(s))
		_, _ = io.WriteString(h, s)
	}
	field(strconv.Itoa(r.V))
	field(strconv.FormatInt(r.At.UTC().UnixNano(), 10))
	field(r.Doc)
	field(r.Review)
	field(string(r.Kind))
	field(r.Author)
	field(r.Old)
	field(r.New)
	field(r.Reason)
	field(r.Quote)
	field(r.Context)
	field(strconv.Itoa(r.Round))
	field(r.Text)
	return hex.EncodeToString(h.Sum(nil))
}

// ErrNoRepo is returned when no repository encloses the path a decision was
// made about. The log lives in the repo; with no repo there is nowhere to put
// it, and that is a soft failure like every other one here.
var ErrNoRepo = errors.New("no git repository above this path")

// LogName is the log's basename. It is joined under Dir, never at the root:
// `.galley/` is a directory galley owns, so the `.gitattributes` this package
// writes cannot collide with the repository's own.
const (
	Dir     = ".galley"
	LogName = "decisions.jsonl"
)

// mergeUnion is the .gitattributes line, and the spec names the trap it
// closes explicitly: an append-only file conflicts the instant two branches
// both append to it, and git's default merge produces a conflict marker in the
// middle of a JSONL file — which is to say, two corrupt lines. `merge=union`
// keeps both sides' lines. It is written in the same breath as the log's first
// line, because retrofitting it after the first conflict is misery.
const mergeUnion = "*.jsonl merge=union"

// RepoRoot walks up from start looking for `.git`, and returns the directory
// holding it.
//
// IT DOES NOT REQUIRE `.git` TO BE A DIRECTORY, and that is load-bearing here
// rather than pedantic: galley's own development happens in worktrees (the
// Justfile has a whole comment about it), and a worktree's `.git` is a FILE
// containing a gitdir pointer. An `IsDir()` check walks straight past it —
// and in this repo's layout, where worktrees live under `galley/.worktrees/`
// with a BARE repository as their ancestor, it would keep walking until it
// found that bare directory and then write every worktree's decisions into
// one shared log at the wrong root.
//
// Shelling out to `git rev-parse --show-toplevel` was the other option and was
// not taken: a ledger append must not spawn a process, and must not care
// whether git is on PATH.
func RepoRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	// A file argument (the usual case: a document path) means start looking
	// from its directory.
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if _, err := os.Lstat(filepath.Join(abs, ".git")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("%w: %s", ErrNoRepo, start)
		}
		abs = parent
	}
}

// LogPath is the log for the repository enclosing start.
func LogPath(start string) (string, error) {
	root, err := RepoRoot(start)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, Dir, LogName), nil
}

// Log records a decision about docPath in that document's repository, filling
// in the fields the caller should not have to compute: the version, the
// timestamp, and the document's path RELATIVE TO THE REPO ROOT (an absolute
// path is a fact about one machine, and this file is committed).
//
// The error is for the caller to log or ignore. It is never a reason to fail
// a review — see the package comment.
func Log(docPath string, rec Record) error {
	root, err := RepoRoot(docPath)
	if err != nil {
		return err
	}
	if rec.Doc == "" {
		abs, aerr := filepath.Abs(docPath)
		if aerr != nil {
			return aerr
		}
		if rel, rerr := filepath.Rel(root, abs); rerr == nil {
			rec.Doc = filepath.ToSlash(rel)
		} else {
			rec.Doc = filepath.ToSlash(abs)
		}
	}
	return Append(filepath.Join(root, Dir, LogName), rec)
}

// Append writes one record as one line.
//
// ATOMIC PER LINE, and it takes three things to be so. The record is marshalled
// WHOLE before the file is opened, so a marshalling failure never leaves a
// half-line behind. The file is opened O_APPEND, so the kernel resolves the
// offset at write time and two processes appending concurrently cannot land on
// the same bytes. And the line goes out in ONE Write call — a Fprintf, a
// bufio.Writer, or a separate write for the '\n' would each give another
// appender a window to interleave inside a record.
//
// The newline cannot appear inside the payload: encoding/json escapes control
// characters in strings, so a decline reason with a line break in it arrives as
// `\n` in the JSON and the line stays one line.
//
// The reader still tolerates a torn final line anyway (see Index.Sync). Belt
// and braces on purpose: a single write() to a local file is atomic in every
// filesystem galley runs on, but "in practice" is not a property, and a crash
// between the write and the flush is not something this code gets a say in.
func Append(logPath string, rec Record) error {
	if rec.V == 0 {
		rec.V = Version
	}
	if rec.At.IsZero() {
		rec.At = time.Now()
	}
	// UTC, always. A ledger read on a machine in another timezone must sort
	// and group the same way it does here, and RFC3339 text only sorts
	// lexicographically when every line carries the same offset.
	rec.At = rec.At.UTC()
	if rec.Kind == "" {
		return errors.New("ledger: a record with no kind is not a decision")
	}

	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line := append(raw, '\n') //nolint:gocritic // raw is not used again

	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Before the first line, not after it — see mergeUnion.
	if err := ensureMergeUnion(dir); err != nil {
		return err
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if n != len(line) {
		return fmt.Errorf("ledger: short write, %d of %d bytes", n, len(line))
	}
	return cerr
}

// ensureMergeUnion puts `*.jsonl merge=union` in the log directory's
// .gitattributes, creating the file if it is absent and appending the line if
// the file exists without it.
//
// The create path is O_EXCL and treats "already exists" as success rather than
// as a race to lose: N processes appending decisions at once all reach here,
// and exactly one of them may create the file.
func ensureMergeUnion(dir string) error {
	path := filepath.Join(dir, ".gitattributes")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, werr := f.WriteString(mergeUnion + "\n")
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		return cerr
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "*.jsonl") {
			return nil
		}
	}
	// Someone else's .gitattributes, with rules of its own. Append rather
	// than rewrite, and lead with a newline if the file did not end with one
	// — a rule glued onto the tail of another rule is two broken rules.
	add := mergeUnion + "\n"
	if len(raw) > 0 && !strings.HasSuffix(string(raw), "\n") {
		add = "\n" + add
	}
	out, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := out.WriteString(add)
	cerr := out.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
