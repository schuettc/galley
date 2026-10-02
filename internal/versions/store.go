// Package versions is where a document's rounds are kept.
//
// A VERSION IS A ROUND, AND A VERSION IS THE FILE — byte for byte the same
// projection output that reached the author's .md, which is what bounds what
// one can ever hold. What changed between any two of them is COMPUTED by
// internal/diff and NEVER WRITTEN DOWN, because a stored diff is a second copy
// of a fact that can disagree with the documents it came from. That half is
// unconditional: no rendered diff reaches this directory in any phase.
//
// "EVERY VERSION IS A CLEAN DOCUMENT — NO MARKUP ON DISK, EVER" IS PHASE 3's
// SENTENCE, and it is stated here in the future tense on purpose. It becomes
// true the day the agent's changes are APPLIED rather than proposed (spec
// decision 2), because then there is nothing undecided left for CriticMarkup to
// spell. It is FALSE today, and saying otherwise in the present tense would be
// an invariant a reader could rely on and a `galley suggest` could break: a
// version equals the projection, the projection carries the agent's pending
// ins/del/highlight marks, and `{--**bold**--}{++brave++}` on disk is exactly
// what those are. Nothing here strips them — a version that differed from the
// file would be a version of a document nobody has.
// internal/serve's TestAVersionIsTheFileAndNoDiffReachesDisk holds both halves
// against a fixture that carries an undecided proposal, which is the fixture
// the claim needs to be checkable at all.
//
// # Where they live, and why
//
// Beside the document, in `<dir>/.galley/versions/<document>/`: `0001.md`,
// `0002.md`, … one clean markdown file per round, plus `rounds.jsonl`, one JSON
// line per round carrying who moved it, when, why it was cut, and the
// instruction that produced it.
//
// FULL COPIES, NOT DIFFS, and not de-duplicated either. A version is a few KB;
// a reference to another version is not a document, and the moment one round's
// content is expressed in terms of another's, "every version is a clean
// document" stops being true of the thing on disk.
//
// Three homes were considered and two were declined.
//
// THE LEDGER was declined for the CONTENT and is where the process record
// eventually belongs. `.galley/decisions.jsonl` is committed and merged with
// `merge=union`, so a rebase replay or a cherry-pick legitimately keeps both
// sides' lines — a mechanism that is exactly right for short decision records
// and exactly wrong for multi-kilobyte document bodies, which it would duplicate
// wholesale. The ledger also lives at the REPO ROOT, found by walking up for
// `.git`, so a document edited outside a repository has no ledger at all: the
// recording surface has no error to hand back, by design, and a round that
// vanished silently would break the one promise the history makes, which is
// that it is complete.
//
// GIT was declined because a running galley owns the FILE and owns nothing
// else. Cutting a version mid-round would mean writing to the reviewer's index
// and their history — during a rebase, in a dirty tree, in a directory that may
// not be a repository at all — which is a side effect galley has no licence to
// take. Git also has nowhere to put the instruction that produced a round
// without inventing a trailer convention, and the pairing of a round with its
// instruction is the whole reason the history is worth keeping.
//
// BESIDE THE DOCUMENT is what content does here already: the running
// editor's advert is `<doc>.serve.json` in the same directory, and moving a
// document takes its versions with it. In a repository whose documents sit at the root — the
// common case, and this repository's own — that path is literally
// `.galley/versions/<doc>/<n>.md`.
//
// AND IT IS GITIGNORED, which is what "the history is insurance" means when the
// insurance is full copies. `galley edit` starts writing here the moment it
// opens a document, so committed by default a live hour's forty rounds of
// somebody's draft arrive in their next pull request. The ledger's opposite
// ruling is not a precedent for this one: `.galley/decisions.jsonl` is ONE LINE
// per decision and is deliberately shared. Everything here is nevertheless
// built to be committed the day a team decides it wants to — standalone files,
// an append-only manifest, and the `merge=union` attribute written into the
// directory on the first commit — so un-ignoring `**/.galley/versions/` is the
// whole of that change. See the spec's "the history is insurance, not a
// feature".
package versions

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tools "github.com/schuettc/tools-common"

	"github.com/schuettc/galley/internal/ondisk"
)

// Author names a party that moved the document in a round. A round may carry
// BOTH: in live mode the CRDT merged two people's work before anything was cut,
// and `v12 · court and agent` is information rather than an embarrassment.
const (
	AuthorReviewer = "court"
	AuthorAgent    = "agent"
)

// Reason says why a version was cut. COMMIT ON SEND is the one rule; these are
// the sends.
const (
	// ReasonOpened is the document as galley first saw it — or as it was found
	// after somebody wrote to the file behind galley's back. It is what gives
	// the first real round something to be a diff against.
	ReasonOpened = "opened"
	// ReasonRevise is a Revise press: the reviewer's round, committed as it is
	// handed over.
	ReasonRevise = "revise"
	// ReasonLanded is the agent's return: the revision arriving.
	ReasonLanded = "landed"
	// ReasonSettled is a live exchange. There is no press, so the send is the
	// moment the agent is actually WOKEN — not every settle, and emphatically
	// not every save: a reviewer typing prose settles constantly and hands over
	// nothing.
	ReasonSettled = "settled"
	// ReasonVerdict is the round the review ended on.
	ReasonVerdict = "verdict"
	// ReasonCouldNot is THE EXCEPTION, and it is a round for the reason a press
	// with no edit beside it is one: an instruction was answered, and the answer
	// was that it could not be done. The bytes are unchanged BY DEFINITION —
	// this is the one reason where that is the fact being recorded rather than a
	// reason to skip the cut. The spec: an exception *"is not a turn in a
	// conversation, it is a report"*.
	ReasonCouldNot = "could-not"
)

// Version is the schema generation of a `rounds.jsonl` line, written as `v` on
// every row this build appends.
//
// THE MANIFEST IS FORWARD-COMPATIBLE AND THE VERSION DOES NOT CHANGE THAT. It
// is committed and merged with merge=union, so one file legitimately holds
// lines written by several builds on several machines — and a reader that
// refused a line from a newer galley would drop rounds out of a history whose
// only promise is that it is complete. So a future version is KEPT, REPORTED
// (see Inspect) and read for the fields this build understands, exactly the
// policy internal/ledger's Version states for the decisions log.
//
// A row with no `v` at all is generation 1: every line written before this
// field existed has precisely the shape the field was added to.
const Version = 1

// Round is one version's record. The DOCUMENT is not in here — it is the file
// this row names, and there is exactly one copy of it.
type Round struct {
	// V is the schema generation — see Version. First, so the head of a line
	// answers "which galley wrote this" and so it reads the same way the
	// ledger's records do.
	V       int       `json:"v"`
	N       int       `json:"n"`
	At      time.Time `json:"at"`
	Authors []string  `json:"authors"`
	Reason  string    `json:"reason"`
	// Instruction is what the reviewer asked for, on the round that asked it. A
	// DIFF SAYS WHAT MOVED; A DIFF BESIDE THE INSTRUCTION THAT CAUSED IT SAYS
	// WHETHER THE AGENT UNDERSTOOD YOU, and no git log of a document can tell
	// you that.
	Instruction string `json:"instruction"`
	// Asks is the round's instructions WITH THEIR KEYS, on the round that
	// asked them, so a Change may point AT one. Instruction keeps the joined
	// sentence the History landing renders and is not computed from these:
	// nothing is served by spelling the same summary twice. Empty on rounds cut
	// before the manifest existed.
	Asks []Ask `json:"asks,omitempty"`
	// Changes is THE AGENT'S TESTIMONY, on the round that LANDED. See Change.
	Changes []Change `json:"changes,omitempty"`
	// Answers is the round this one is a reply to, or 0. The agent's version
	// POINTS AT the instruction rather than repeating it: one copy of the
	// words, in the round that carried them.
	Answers int `json:"answers"`
	// Digest is the sha256 of the version's own bytes, so a file swapped
	// underneath the manifest is detectable rather than silently believed.
	Digest string `json:"digest"`
	File   string `json:"file"`
}

// Ask is one instruction as it was handed to the agent, on the round that
// asked it. Round.Instruction keeps the joined sentence the History landing
// renders; this is the same round's asks with their identity intact, because
// A CHANGE CAN ONLY POINT AT AN ASK IF THE ASK HAS A NAME — and by the time the
// agent answers, the instruction's thread has been deleted from the review by
// the send that carried it, so the round record is the only place the keys the
// server sent still exist.
type Ask struct {
	Key   string `json:"key"`
	Text  string `json:"text"`
	Quote string `json:"quote,omitempty"`
}

// Change is THE AGENT'S TESTIMONY ABOUT A CHANGE, AND IT IS NOT A DIFF.
//
// The package's rule stands untouched: what changed between two versions is
// computed by internal/diff and never written down. None of these three
// fields is derivable from the two documents — which is exactly why they must
// be kept. Locator is the agent's own quotation of the text it wrote, and it
// is how the agent says WHICH change it means; it is re-matched against a
// freshly computed diff every time a reading state renders, so a locator that
// no longer matches renders its note placeless rather than being believed.
// That is the same discipline Digest already applies to a file swapped
// underneath the manifest.
//
// Locator is TEXT, never a region index: an ordinal renumbers, and
// `CHANGE k OF K` is computed at render time from the recomputed region list.
type Change struct {
	Locator string   `json:"locator"`
	Answers []string `json:"answers,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Store is one document's versions directory.
type Store struct {
	dir string // …/.galley/versions/<document>
	doc string // the document's base name
}

// DirName is the directory versions live under, inside .galley.
const DirName = "versions"

// galleyDir is the per-directory galley folder. The ledger uses the same name
// at the repo root; this one is beside the document, for the reason the package
// comment gives.
const galleyDir = ".galley"

// Open returns the store for a document. It does not create anything: a
// document that has never been opened has no versions, and asking about them
// must not be what brings a directory into existence.
func Open(mdPath string) *Store {
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		abs = mdPath
	}
	base := filepath.Base(abs)
	return &Store{
		dir: filepath.Join(filepath.Dir(abs), galleyDir, DirName, base),
		doc: base,
	}
}

// Dir is where this document's versions are kept.
func (s *Store) Dir() string { return s.dir }

// manifest is the rounds file.
func (s *Store) manifest() string { return filepath.Join(s.dir, "rounds.jsonl") }

// Problem is one line of the manifest that could not be reported as a round,
// and why.
//
// IT EXISTS BECAUSE SILENCE IS THE DEFECT — the same sentence internal/registry
// carries over the same split, and for a worse reason here: a dropped advert is
// rediscovered on the next scan, while a dropped round is a version that has
// left the history for good with no log line and no counter. Line is 1-based
// and counts every line of the file, blank ones included, so it is the number
// an editor jumps to.
type Problem struct {
	Line   int
	Reason string
}

func (p Problem) String() string { return fmt.Sprintf("line %d: %s", p.Line, p.Reason) }

// List reads every round, in order. A store that does not exist has no rounds
// and is not an error — that is the ordinary state of a document nobody has
// revised yet.
//
// List is the hot path and keeps its signature; a caller that wants to know
// what was DROPPED — and something always should — reads Inspect. The split is
// registry.List/registry.Inspect's, deliberately, so there is one shape in this
// codebase for "report the good ones, and separately account for the rest".
func (s *Store) List() ([]Round, error) {
	rounds, _, err := s.Inspect()
	return rounds, err
}

// Inspect is List plus the lines it could not report and why.
//
// TWO CONDITIONS, AND ONLY ONE OF THEM LOSES A ROUND. A line that will not
// parse is dropped and counted: the manifest is committed under merge=union, a
// bad merge resolution is a thing that can happen to it, and losing forty
// readable rounds because one is malformed is the opposite of a complete
// history. A line from a NEWER galley is KEPT and counted — see Version — and
// appears in the returned rounds, because every field this build knows is still
// on it and dropping the row would be the refusal the forward-compatible policy
// exists to prevent.
func (s *Store) Inspect() ([]Round, []Problem, error) {
	f, err := os.Open(s.manifest())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Round
	var problems []Problem
	lineno := 0
	sc := bufio.NewScanner(f)
	// A round's instruction is the reviewer's own prose and has no length
	// limit worth guessing at, so the line buffer is generous.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		lineno++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Round
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			// A LINE THIS CANNOT READ IS NOT A REASON TO LOSE THE REST. The
			// manifest is committed and merged with merge=union, so a bad merge
			// resolution is a thing that can happen to it; the history's job is
			// to be complete, and dropping forty readable rounds because one is
			// malformed is the opposite of that.
			//
			// BUT IT IS A REASON TO SAY SO. This `continue` used to be the whole
			// of the handling: the round vanished from the history with no log
			// line and no counter, which is indistinguishable from a round that
			// was never cut — and cmd/galley/cannot.go computes both its answers
			// pointer and (through Commit) its own round number from the rounds
			// that could be read.
			problems = append(problems, Problem{lineno, "unreadable: " + err.Error()})
			continue
		}
		if ondisk.Future(r.V, Version) {
			// KEPT, NOT REFUSED — see Version. Reported so a reader knows the
			// row may carry facts this build cannot show.
			problems = append(problems, Problem{lineno, fmt.Sprintf(
				"round %d is at schema v%d and this galley knows v%d; read for the fields it understands",
				r.N, r.V, Version)})
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return out, problems, err
	}
	// merge=union keeps both sides' lines and does not promise an order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].N < out[j].N })
	return dedupe(out), problems, nil
}

// dedupe collapses two lines that record the same round. The union merge keeps
// both sides of a cherry-pick, so one round genuinely can appear twice; the
// first spelling wins, because a round's facts are written once and never
// amended.
func dedupe(in []Round) []Round {
	seen := map[int]bool{}
	out := in[:0]
	for _, r := range in {
		if seen[r.N] {
			continue
		}
		seen[r.N] = true
		out = append(out, r)
	}
	return out
}

// Latest is the most recent round, if there is one.
func (s *Store) Latest() (Round, bool, error) {
	rounds, err := s.List()
	if err != nil || len(rounds) == 0 {
		return Round{}, false, err
	}
	return rounds[len(rounds)-1], true, nil
}

// Get is one round's record.
func (s *Store) Get(n int) (Round, bool, error) {
	rounds, err := s.List()
	if err != nil {
		return Round{}, false, err
	}
	for _, r := range rounds {
		if r.N == n {
			return r, true, nil
		}
	}
	return Round{}, false, nil
}

// Content is version n's document, exactly as it was committed.
func (s *Store) Content(n int) (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, fileName(n)))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Commit cuts a version: the document as it stands, plus the round's facts.
//
// The number is assigned here and nowhere else. The caller supplies everything
// except N, At, Digest and File, which are the store's to know.
func (s *Store) Commit(content string, r Round) (Round, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Round{}, fmt.Errorf("versions: %w", err)
	}
	if err := ensureMergeUnion(s.dir); err != nil {
		return Round{}, err
	}
	rounds, err := s.List()
	if err != nil {
		return Round{}, err
	}
	// The version is the store's to stamp, like N, At, Digest and File: a
	// caller cannot know which generation the bytes it is handing over will be
	// written in, and a row naming a version it was not written by is worse
	// than a row naming none.
	r.V = Version
	r.N = 1
	if len(rounds) > 0 {
		r.N = rounds[len(rounds)-1].N + 1
	}
	if r.At.IsZero() {
		r.At = time.Now().UTC()
	} else {
		r.At = r.At.UTC()
	}
	if len(r.Authors) == 0 {
		// A round with no author named is still a round. Saying so is better
		// than guessing, and the history is insurance: it must be complete
		// before it is tidy.
		r.Authors = nil
	}
	sum := sha256.Sum256([]byte(content))
	r.Digest = hex.EncodeToString(sum[:])
	r.File = fileName(r.N)

	if err := writeFileAtomic(filepath.Join(s.dir, r.File), []byte(content)); err != nil {
		return Round{}, err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return Round{}, err
	}
	// THE DOCUMENT IS WRITTEN BEFORE THE ROW THAT NAMES IT. A row pointing at a
	// file that is not there is a hole in a record whose only job is to be
	// complete; a file no row names is an orphan the next Commit renumbers past
	// and nothing is lost.
	f, err := os.OpenFile(s.manifest(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Round{}, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return Round{}, err
	}
	return r, nil
}

func fileName(n int) string { return fmt.Sprintf("%04d.md", n) }

// mergeUnion is the same rule the ledger's log carries, for the same reason: a
// committed append-only JSONL merged any other way loses one side's lines on
// every rebase replay.
const mergeUnion = "*.jsonl merge=union\n"

func ensureMergeUnion(dir string) error {
	path := filepath.Join(dir, ".gitattributes")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte(mergeUnion), 0o644)
	}
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), "*.jsonl") {
		return nil
	}
	body := string(raw)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body+mergeUnion), 0o644)
}

// writeFileAtomic writes through a temporary file in the same directory, so a
// crash mid-write leaves the previous bytes rather than half of the new ones.
func writeFileAtomic(path string, b []byte) error {
	return tools.WriteFileAtomic(path, b, 0o644)
}

// Authors renders a round's authors the way the history says them: `court`,
// `agent`, or `court and agent`.
func Authors(as []string) string {
	switch len(as) {
	case 0:
		return ""
	case 1:
		return as[0]
	}
	return strings.Join(as[:len(as)-1], ", ") + " and " + as[len(as)-1]
}
