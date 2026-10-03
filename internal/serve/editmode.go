// editmode.go is galley's server: EditServer serves the user's own markdown
// document live, as a ygo XML fragment TipTap binds to directly.
//
// Three consumers share one document: the browser over y-websocket at
// /yjs/{room}, the agent over JSON at /_galley/*, and the disk. Markdown
// on disk maps to docmodel.Doc (internal/markdown), docmodel.Doc maps to the
// fragment (internal/ydoc), and pending suggestions in the fragment map to
// docmodel marks (internal/suggest). The document of record is still the
// file — CriticMarkup carries pending suggestions and comment ID marks, the
// unsent round (pending.json) carries the comments' words — never the live
// ygo document, which exists only while a session runs.
package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reearth/ygo/crdt"
	ws "github.com/reearth/ygo/provider/websocket"

	"github.com/schuettc/galley/internal/debug"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/ledger"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/unsent"
	"github.com/schuettc/galley/internal/versions"
	"github.com/schuettc/galley/internal/ydoc"
)

// EditServer serves one markdown document, live as a ygo XML fragment, and
// projects it back to disk as canonical markdown. THERE IS NO SIDECAR: this
// comment described one for a phase after the file stopped writing it, and
// README says the opposite in a sentence a reader is more likely to trust.
// Instructions live in the document as {>>note<<} blocks and in the ledger with
// the round that carried them; stored versions are clean documents.
type EditServer struct {
	MdPath      string
	RuntimePath string
	Room        string

	// Notify, when set, fires after the document settles. It exists so the
	// reviewer never has to tell the agent to go and look.
	Notify *Notifier

	// OnRevise is the shell command POST /_galley/revise runs: the editor's
	// "hand this to the agent" button, wired by the CLI from --on-revise. A
	// command string rather than a func so the CLI can pass one straight from
	// a flag, and for the same reason Notifier.Command is one — "tell someone"
	// is exactly the seam where setups differ.
	//
	// It runs IMMEDIATELY and asynchronously, unlike Notifier's debounced,
	// fingerprinted firing: revise is an explicit request, so it must not be
	// coalesced or suppressed as "nothing changed" — and it typically starts
	// an agent, which can run for minutes, so the request returns 204 rather
	// than holding the browser open for the whole run. With no command
	// configured the endpoint answers 501 and says so: a Revise button that
	// silently does nothing is worse than one that explains it.
	OnRevise string

	// Log receives one line per revise run — the only place the command's
	// output can surface, since the request has already returned by then.
	Log func(string)

	// afterProject, when set, receives the just-projected markdown bytes with no
	// lock held. Page mode renders the HTML from it; markdown mode leaves it nil.
	afterProject func(md []byte)

	// pageRender is the renderer behind afterProject in page mode, retained so
	// the round boundary can ask what the last re-extraction found. Nil in
	// markdown mode, like every other page field.
	pageRender *pageRenderer

	// OnApprove, when set, is called once per approve verdict — POST
	// /_galley/revise carrying {"verdict":"approve"} on a document with no
	// markup left pending. It runs after the window is closed and the waiters
	// are woken, and never on a refused (409) approve. The CLI uses it to
	// withdraw the channel registry entry and tell the operator the review is
	// done; the server itself keeps serving, since Ctrl-C — not an approve —
	// is what stops it.
	OnApprove func()

	// Ledger, when set, is where this server's decisions are remembered.
	// Nil means the process-wide ledger.DefaultRecorder, which is what every
	// real caller wants; the field exists so a test can supply a recorder whose
	// append FAILS, which is the only way to assert the property this whole
	// wiring rests on — see ledger.go and TestALedgerFailureCannotFailADecision.
	//
	// It is not a hook and has no error to hand back. The ledger is memory,
	// never truth: nothing on this server may fail a decision because a record
	// could not be written.
	Ledger *ledger.Recorder

	// OnStop, when set, is what POST /_galley/stop runs: the editor's Done
	// button asking the session to end. The CLI wires it to the SAME orderly
	// shutdown its signal handler runs — waiters released, in-flight revision
	// waited on, final flush, Close — so there is one shutdown path and not
	// two that drift. Nil means no stop hook, and the endpoint says so rather
	// than pretending.
	OnStop func()

	doc *crdt.Doc
	yjs *ws.Server

	// root is the document's directory, opened ONCE at construction. Every
	// figure read goes through it, so traversal and symlink escape are refused
	// by the kernel rather than by string arithmetic on a URL. Opening it once
	// also means a directory swapped underneath a running server cannot
	// redirect reads to somewhere else. See editassets.go.
	root *os.Root

	// pageMode is true when this server was opened via NewEditPage — the
	// document under review is derived from an HTML page. pagePath is the
	// absolute path of that page, and pageRoot is a containment root at its
	// directory (filepath.Dir(pagePath)) so a later task can serve the page to
	// an iframe through the kernel rather than string arithmetic. In plain
	// markdown mode all three are their zero values.
	pageMode bool
	pagePath string
	pageRoot *os.Root
	// previewRel is the /_galley/preview/ URL name the page is mounted under —
	// its path relative to pageRoot. By default the page's basename (pageRoot is
	// the page's own directory); with a site root it is the page's path beneath
	// that root, so the iframe's ../asset references resolve inside pageRoot.
	previewRel string

	debounce debouncer
	exported lastExportStamp

	// versions is this document's rounds — see versions.go. Lazily opened, and
	// opening creates nothing: a document nobody has revised has no store.
	versions *versions.Store
	// pendingCut is a send waiting for the next projection to write it down,
	// and movedByAgent is how the round it becomes knows who moved
	// the document. All three are guarded by mu, because the cut happens inside
	// project with mu already held — which is the whole reason a version can
	// never be a torn read of the live document.
	pendingCut   *cutIntent
	movedByAgent bool
	// applied is the agent's round while it is still being written — every
	// `galley apply` in one revision, and every --note it carried. Under mu with
	// the rest of the cut's state, because it is read and cleared inside project.
	// See versions.go's appliedRound for why a revision cannot be cut the way a
	// proposal was.
	applied *appliedRound
	// lastLanded is the number of the last round the AGENT sent — a landing or
	// an exception. It rides GET /_galley/revise, which the page already polls,
	// so an arrival needs no new surface and no per-reader state on this side:
	// the browser compares it against what it last saw. Atomic because that
	// handler holds reviseMu and not mu.
	lastLanded atomic.Int32
	// serverWrites is non-zero exactly while this server is writing to the
	// document through Apply. It is what tells THE REVIEWER'S OWN EDIT from a
	// write the server made on somebody's behalf: every update that is neither
	// a live read's no-op transaction nor one of this server's own is a peer's,
	// and the only peer is the reviewer's browser. An atomic and not a field
	// under mu because doc.OnUpdate fires on whichever goroutine landed the
	// update, including a websocket reader's.
	serverWrites atomic.Int32
	// movedByReviewer is that observation, standing until the next cut.
	movedByReviewer atomic.Bool
	// lastCutN is the round cutIfSending last minted, read back by requestCut
	// under the same mu it was written under. It exists so the press that cut a
	// version can put its number on the revise window WITHOUT a second lock
	// order — the agent's return reads that number back to point at the
	// instruction, and the alternative was mu and reviseMu held together.
	lastCutN int

	// mu serializes every SERVER-side mutation of the document — the
	// suggest/accept/reject endpoints, Project, and Flush — so no two of them
	// interleave a read with each other's write. See mutate for what it does
	// and does not protect.
	mu sync.Mutex
	// rev counts server-side mutations, stamped into meta on each one. Guarded
	// by mu.
	rev int
	// projected is the digest of the markdown this server last wrote to
	// MdPath, so the next projection can tell a file nobody has touched from
	// one that changed underneath it. Guarded by mu, like rev: it is written
	// inside project and read nowhere else. Empty until the first projection —
	// see project for why that silence is the honest one.
	projected string

	// mode is "ask" or "live" — whether the agent is woken when the document
	// settles, or only when the reviewer asks. The zero value reads as "ask",
	// which is what makes realtime opt-in: a document that has never been told
	// to go live behaves exactly as it did in phase 1.
	//
	// Its own mutex, NOT mu. mu is held across a whole projection, and the
	// projection is what reads this to decide whether to notify — sharing the
	// lock would mean the mode endpoint blocking behind a disk write, or
	// worse, a re-entrant take.
	modeMu sync.Mutex
	mode   string

	// reviseDone is non-nil exactly while a revise command is running, and is
	// closed when it exits — the single-flight latch and the "wait for it"
	// handle in one field. Guarded by reviseMu, deliberately NOT by mu: a
	// revision runs for minutes and must not hold up a single document
	// mutation for that long.
	reviseMu   sync.Mutex
	reviseDone chan struct{}
	// watch is the REVIEWER-facing revision window, and it is deliberately not
	// the same thing as reviseDone. See reviseWatch.
	watch *reviseWatch
	// approveOnAnswer belongs to the open watch. It is set by the explicit
	// "Revise & Approve" exit and consumed only by a successful `answered` ack.
	// A refusal, failure, or cannot report clears it and leaves the review open.
	approveOnAnswer bool

	// handoffLease is the agent's window made durable — see handoff.go. Under
	// reviseMu with the watch, because they are one state: the window opens on
	// a send and closes on the agent's return. handoffLive mirrors "a lease
	// exists" for project(), which holds mu and must not take reviseMu.
	handoffLease *handoffLease
	handoffLive  atomic.Bool
	// handoffStop ends the import watcher; draftErr is the last save that
	// could not be imported, in the parser's words, for the readout. Both
	// under reviseMu with the lease.
	handoffStop chan struct{}
	// handoffDone is closed when the watcher's goroutine has exited.
	handoffDone chan struct{}
	// testImportTick, when set before a window opens, runs on that window's
	// watcher goroutine at the start of each tick, before the import. Tests
	// only: a field, not a package variable, so no other server sees it.
	testImportTick func()
	draftErr       string

	// The agent's last acknowledgment, shown by writeReviseState. Guarded by
	// reviseMu with the watch because the two are one story: the window opens
	// on a press, the ack narrates it, and a terminal ack closes it.
	ackState string
	ackNote  string
	ackAt    time.Time

	// instrSaid is every word of the reviewer's this session has already
	// recorded on a round — see reviewerInstruction, which is the only reader
	// and the only writer. Its own mutex: it is composed BEFORE mu is taken (the
	// cut's read of the conversation is deliberately off the projection path)
	// and marked AFTER requestCut returns, so it can share neither mu nor
	// reviseMu without inventing a lock order this file does not have.
	instrMu   sync.Mutex
	instrSaid map[string]bool
	// sending is the round being sent: every comment Revise has cleared out of
	// the review map and the cut has not yet recorded. saveUnsentLocked writes
	// it beside the live threads, so pending.json holds a sent comment until a
	// round does. Under mu. See sendReviewerRound.
	sending map[string]unsent.Comment
	// seenAnchored is every text comment this server has seen placed: its ID on
	// a mark in the live document. See lostanchor.go: "there is no mark now"
	// and "the reviewer deleted the words" are different claims, and only this
	// set tells them apart. Written under mu by the projection; anchorMu
	// guards it because `pending` reads it without mu.
	seenAnchored map[string]bool
	anchorMu     sync.Mutex
	// retracted is the seen text comments the last projection found with no
	// mark: the keys pending.json leaves out. Under mu. See noteRetracted.
	retracted map[string]bool

	// The exception, in the agent's own words — see handleCannot. Under reviseMu
	// with the ack for the same reason the ack is: the window opens on a press
	// and this is one of the two ways it closes.
	cannotWhy string
	cannotAt  time.Time

	// sealMu guards the seal — whether this review has ENDED, and how. See
	// seal.go, which holds every method that reads or writes these.
	//
	// Its own mutex, NOT mu and NOT reviseMu. Every mutating endpoint asks
	// "is this review sealed" before it does anything, and that question must
	// never queue behind a projection holding mu across a disk write; the
	// verdict branches of handleRevise take it while holding neither. Nothing
	// takes another lock while holding this one.
	sealMu sync.Mutex
	// sealed is the state; verdict/verdictAt/entrusted are the record it
	// carries. In-memory only, like trailEpoch and for the same reason: a
	// restart is a new session, and a tab from the previous run is already
	// refused at the websocket by the room token.
	sealed    bool
	verdict   string
	verdictAt time.Time

	// waitMu guards the blocking-read registry — GET /_galley/wait's waiters.
	//
	// Its own mutex, NOT mu and NOT reviseMu. Waiters are released from the
	// notifier's fire (a timer goroutine, reached from a projection that holds
	// mu) and from handleRevise (which holds reviseMu), so sharing either lock
	// would put an HTTP handler in front of a document write. Nothing takes
	// another lock while holding this one.
	waitMu sync.Mutex
	// waiters is the set of blocked readers. A map rather than a slice so a
	// reader whose request went away removes itself in constant time — a
	// `galley wait` Ctrl-C'd mid-poll is the ordinary case, not the exception.
	waiters map[chan waitEvent]struct{}
	// waitClosed is set once the server is shutting down, so a poll arriving
	// during shutdown is answered rather than registered into a registry
	// nothing will ever release again.
	waitClosed bool
	// waitWoke is when a reader last left the registry HAVING BEEN RELEASED —
	// the instant the re-arm gap opens. A reader woken by an event re-arms
	// immediately (the channel's whole design is re-arm-before-emit), so for
	// the few instructions between its deregistration and its next
	// registration `Waiting()` reads 0 while a session is plainly attached.
	// See awaitReArm, which is the only reader of this field.
	//
	// NOT "a waiter was here recently", which would also be true of one that
	// EXPIRED or hung up: those readers are under no obligation to come back,
	// and waiting for them would turn a genuinely unheard press into a
	// slower, still-unheard press.
	waitWoke time.Time
}

// reviseWatch is what the editor's "revising · Ns" counts against.
//
// NOT THE SAME WINDOW AS reviseDone, and the difference is the whole of R10.
// reviseDone tracks the COMMAND PROCESS, which is a notification: the usual
// --on-revise is `muster send … && muster nudge …`, which exits in
// milliseconds while the agent it woke works for minutes. A button that
// re-enabled when that process exited — or worse, when the POST returned —
// showed no in-flight state at all for a six-second revision.
//
// So this window opens when Revise is PRESSED and closes when the revision
// LANDS: the first projection whose pending fingerprint differs from the one
// taken at the ask. The same fingerprint the notifier uses, for the same
// reason — a projection that leaves the pending set where it was is not a
// landing, it is a save.
type reviseWatch struct {
	at          time.Time
	fingerprint string
	// round is the version the press cut, so the agent's return can POINT AT
	// the instruction rather than repeating it — one copy of the words, in the
	// round that carried them.
	round int
}

//go:embed edit.html
var editShellHTML string

// editShell is parsed once, at init, so a broken template fails the whole
// package's tests rather than one unlucky request. html/template rather than
// text/template because the document's own name reaches the page: a file
// called `weird".md` must not be able to break out of the attribute it lands
// in.
var editShell = template.Must(template.New("edit.html").Parse(editShellHTML))

// newInstanceToken returns a short random token identifying one run of the
// edit server.
//
// It exists because a CRDT cannot recognise a document it did not build. Each
// run parses the file into a brand-new crdt.Doc, so every item carries fresh
// IDs from a fresh client ID. A browser tab left open across a restart still
// holds the previous run's document; when it reconnects and syncs, nothing in
// its state matches anything in the new document, and the merge keeps both —
// the reviewer's document appears twice, and the next projection writes the
// doubled text to disk.
//
// Naming the room after this token makes that merge unreachable rather than
// merely unlikely: the stale tab asks for a room that no longer exists and is
// refused before the websocket upgrade, so no foreign state can enter the
// document. The tab then notices the room changed on its next /rev poll and
// reloads into the new one. Nothing is lost by that reload — every mutation is
// projected to disk as it happens, so the file the new run parses already
// contains the tab's work.
func newInstanceToken() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("instance token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NewEdit builds an edit-mode server for a markdown document: the room named
// from its basename and this run's instance token, the file's content loaded
// live into the ygo fragment, and the unsent round replayed from pending.json.
//
// Room creation and the initial fragment load happen in one Apply so a peer
// connecting mid-startup never observes an empty document followed by a full
// one: ws.Server merges every transact call made inside one Apply callback
// into a single captured update, broadcast once.
func NewEdit(mdPath string) (*EditServer, error) {
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		// "document", not "page": edit mode serves a markdown file, and
		// borrowing the retired review mode's vocabulary made `galley edit nope.md`
		// report a problem with something the user never mentioned.
		return nil, fmt.Errorf("document: %w", err)
	}
	// ONE EDITOR PER DOCUMENT — see claimDocument, and the erasure it names.
	// Asked here, before the second *crdt.Doc is built, because the second
	// document is the harm; the advert is only how it is noticed. The claim is
	// a READ, so two processes starting in the same instant can both pass it —
	// the window is from here to the caller's announce, and the sequence this
	// exists for (a second `galley edit`, minutes or hours later) is not in it.
	if err := claimDocument(DefaultRuntimePath(abs)); err != nil {
		return nil, err
	}
	// THE INLINE NOTES THE PARSE LIFTS ARE DISCARDED, and that is a deletion:
	// a hand-typed {>>words<<} inside a sentence leaves the .md at the first
	// projection. Comments hand-typed into the .md are not imported, by
	// design: they are dropped. A reviewer comment's words live in
	// pending.json and its place is an ID mark, which the parse keeps.
	model, _, err := markdown.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", abs, err)
	}

	// THE UNSENT ROUND, and nothing else: every comment comes from
	// pending.json, placed by the ID marks the parse kept. Nothing is imported
	// from the file's words.
	pendingComments, err := loadUnsentRound(abs)
	if err != nil {
		return nil, err
	}
	// Identity, before the document is ever served. A peer that connects
	// during startup must see addressable marks on its first sync, not
	// unaddressed ones corrected a moment later.
	model = suggest.MintRuns(model)

	instance, err := newInstanceToken()
	if err != nil {
		return nil, err
	}
	// The room name carries this process's identity, and that is the whole
	// defence against a restart duplicating the document. See newInstanceToken.
	room := roomFor(abs) + "-" + instance
	yjs := ws.NewServer()
	yjs.RoomIdleTimeout = roomIdleTimeout
	if err := yjs.Apply(context.Background(), room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			meta := doc.GetMap("meta")
			transact(func(txn *crdt.Transaction) {
				meta.Set(txn, "page", filepath.Base(abs))
				meta.Set(txn, "schema", schemaVersion)
			})
			ydoc.Load(doc, transact, model)
		}); err != nil {
		return nil, fmt.Errorf("create room %q: %w", room, err)
	}
	doc := yjs.GetDoc(room)
	if doc == nil {
		return nil, fmt.Errorf("room %q did not materialise", room)
	}

	// The document's directory, opened once. Every figure this server answers
	// for is read through it — see serveSibling.
	root, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("open document directory: %w", err)
	}

	s := &EditServer{
		MdPath:      abs,
		RuntimePath: DefaultRuntimePath(abs),
		Room:        room,
		doc:         doc,
		yjs:         yjs,
		root:        root,
		// The trail survives a restart: it clears on the document VERDICT, not
		// on the process ending (the 2026-08-15 trail spec — review-long,
		// verdict-cleared). The epoch starts at 1 so the zero value — an
		// epochless save — is stale by construction.
		// THE BASELINE IS THE FILE AS OPENED, not the first thing this server
		// writes. Those bytes are exactly what the live document was built
		// from, so they are what "nobody has touched this since" means — and
		// establishing it here rather than at the first projection is what
		// makes the commonest case reportable at all: a hand edit in the
		// minutes after `galley edit` starts, before any mutation has caused a
		// projection. Measured with the first-projection baseline: the edit
		// was reverted in silence, which is the very defect. See project.
		projected: fileDigest(src),
		// ALWAYS a notifier, even with no command to run. It is not only the
		// thing that runs --on-settle any more: it holds the one "has the
		// document moved since the agent last saw it" decision, and a blocked
		// `galley wait` rides that decision. Leaving it nil until a flag
		// supplied a command would make pull — which needs no command at all —
		// depend on push being configured.
		Notify: &Notifier{},
	}

	// The review map is pending.json's live state. Where each comment sits is
	// not replayed: the one builder reads it off the ID marks every time.
	replayUnsent(s.doc, unsent.ToThreads(pendingComments))

	s.doc.OnUpdate(func(_ []byte, origin any) {
		// ReadLive's own mutual-exclusion Transact fires this like any other
		// transaction (ygo does not distinguish a no-op Transact from one
		// that wrote something — see ydoc.LiveReadOrigin). Without this
		// check, Project (which calls ReadLive) would requeue itself via
		// touch forever: read triggers "something changed", "something
		// changed" schedules another Project, which reads again.
		if origin == ydoc.LiveReadOrigin {
			return
		}
		// THE ROUND NOW CARRIES THE REVIEWER — see versions.go's roundAuthors.
		// This is the only place that can see it: the trail records typed TEXT
		// and not formatting or structure, and an update that is not this
		// server's own came from the one peer there is.
		if s.serverWrites.Load() == 0 {
			s.movedByReviewer.Store(true)
		}
		s.touch()
	})

	// THE DOCUMENT AS GALLEY FIRST SAW IT IS A ROUND. Without it the first
	// Revise has nothing to be a diff against, and the history would begin one
	// round after the document did.
	//
	// It is also where an external write finally has somewhere to GO. A running
	// galley still owns the file, and the CRDT still wins — that has not
	// changed and this phase does not change it — but a file that moved behind
	// galley's back is now recorded as a version on the next open, rather than
	// being a deletion nobody can name. Byte-identical content cuts nothing.
	//
	// THE MODEL SERIALIZED ONTO `src`, WHICH IS BOTH HALVES OF ONE RULE: v1 is
	// the same bytes the first projection will write.
	//
	// It said "the model serialized, NOT src", because seeding from the file's
	// own spelling would make the first diff a picture of the PARSER — every
	// setext heading, every "*" bullet and every hard wrap showing as the
	// reviewer's first round of work. That was exactly right while a projection
	// was Serialize's output. Now that a projection PRESERVES the author's
	// bytes for blocks nobody changed (see markdown.SerializeOnto), seeding
	// from the plain rendering inverts the same defect rather than avoiding it:
	// v1 would be canonical, v2 would carry the author's spelling, and the
	// first diff would paint the parser BACKWARDS.
	//
	// The invariant was never "serialize the model" — it was "v1 is the
	// document galley read, in the spelling the next projection will use", and
	// that is what SerializeOnto against src is. See seedVersions.
	// A starting version is the document, not the unsent instruction carriers
	// Galley reconstructed while opening it. The threads keep the words and
	// anchors; History starts from clean prose.
	//
	// UNLESS A HANDOFF LEASE IS OPEN. Then the bytes on disk are the agent's
	// draft mid-round, and cutting them as `opened` would launder the agent's
	// in-flight work into an anonymous version — the exact thing the lease
	// exists to prevent. The window is resumed instead; the draft is already
	// in the live document (this constructor parsed it like any startup), and
	// the agent's eventual return cuts it as the agent's round.
	lease, hasLease, leaseErr := readLease(s.leasePath())
	switch {
	case leaseErr != nil:
		// A lease that cannot be read still names in-flight agent work: keep
		// the evidence beside the store and start normally.
		_ = os.Rename(s.leasePath(), s.leasePath()+".unreadable")
		s.seedVersions(markdown.SerializeOnto(suggest.ClearInstructions(model), src))
	case hasLease:
		s.resumeHandoff(lease)
	default:
		s.seedVersions(markdown.SerializeOnto(suggest.ClearInstructions(model), src))
	}

	return s, nil
}

// Doc exposes the live document for reads.
func (s *EditServer) Doc() *crdt.Doc { return s.doc }

// Close releases everything NewEdit started: the pending debounced projection,
// and the websocket server's peer connections and per-room idle sweeper, which
// ws.NewServer starts whether or not a peer ever connects. It closes peer
// connections, so it must run AFTER the final Flush: a projection that has not
// reached disk by then never will.
func (s *EditServer) Close() error {
	// The import watcher first: it calls mutate, and a mutation landing after
	// the final flush is a write nothing will ever project. Stopping the
	// watcher does NOT close the window — the lease survives a shutdown so a
	// restart can resume the agent's round. And it is WAITED FOR: an import
	// already under way would otherwise write the lease after Close returned.
	if done := s.stopWatcher(); done != nil {
		<-done
	}
	s.debounce.stop()
	s.Notify.Stop()
	// Before the websocket shutdown, and unconditionally: a blocked reader
	// that outlived its server is a `galley wait` hanging past the editor it
	// was watching, with nothing left that could ever release it.
	s.ReleaseWaiters()
	if s.root != nil {
		_ = s.root.Close()
	}
	if s.pageRoot != nil {
		_ = s.pageRoot.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	return s.yjs.Shutdown(ctx)
}

// touch schedules a projection to disk. The notification rides on that
// projection rather than being scheduled here — see project.
func (s *EditServer) touch() {
	s.debounce.touch(exportDebounce, s.Project)
}

// SeedNotify records the document's current pending set as already-announced,
// so opening a file whose suggestions were there yesterday does not read as a
// document full of new work.
//
// Two callers, both saying the same thing in different words. The CLI calls it
// at startup: what the file arrived carrying is not news. mutate calls it after
// a write the AGENT asked for: what the agent itself just did, and was handed
// back in that request's response, is not news either. See mutate.
func (s *EditServer) SeedNotify() {
	if s.Notify == nil {
		return
	}
	model, err := s.readLive()
	if err != nil {
		// Nothing to seed with, so seed with nothing: the first projection
		// after this will compute a real fingerprint and, if it differs,
		// legitimately wake the agent. Better a spurious first notification
		// than a permanently mis-seeded one.
		return
	}
	s.Notify.Seed(s.editFingerprint(model))
}

// LastExport is when the projection last reached disk.
func (s *EditServer) LastExport() time.Time { return s.exported.get() }

// Project writes the live document back to disk: the fragment, read into the
// document model, becomes canonical markdown at MdPath (atomically). The
// unsent comments' words are not written here: they are in pending.json,
// which every instruction mutation writes before any projection runs.
//
// Code-block text is the one exception CriticMarkup cannot honestly render:
// a fence's content is literal code, so it is left untouched by Serialize —
// never rewritten into {++…++}/{--…--}/{==…==} — and any marker-shaped text
// already inside one is passed through as-is. See the CODE FENCE comment on
// project below for why nothing here can safely tell an author's fenced
// suggestion from literal text.
//
// If ydoc.ReadLive cannot get a consistent snapshot after its own retries
// (ydoc.ErrConcurrentWrite), Project writes nothing and returns that error
// unchanged — no write beats a torn write. This is deliberately not treated
// as a failure worth retrying here: the interfering write is itself a real
// edit, which fires its own doc.OnUpdate and requeues a projection through
// the ordinary debounce, so the document is not left unprojected, just
// projected on the next settle instead of this one. A caller with no next
// settle coming — a final flush on shutdown — should use Flush, which does
// retry.
func (s *EditServer) Project() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.project()
}

// Flush projects synchronously, for the one caller with no next settle
// coming: the CLI's Ctrl-C handler (Task 11). It cancels the pending debounced
// projection first — so the timer cannot fire a second one behind it, or after
// the process has already reported itself flushed — takes the same mutation
// mutex every other server-side write does, and unlike Project retries a
// refused snapshot, because "skip it, the next settle will catch it" is not
// available at shutdown.
func (s *EditServer) Flush() error {
	s.debounce.stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	for attempt := 0; attempt < mutationReadAttempts; attempt++ {
		if err = s.project(); !errors.Is(err, ydoc.ErrConcurrentWrite) {
			return err
		}
	}
	return err
}

// fileDigest is what the projection remembers about the bytes it wrote, so the
// next one can tell "nobody has touched this" from "somebody has". A digest and
// not the content: the document is the reviewer's prose and can be large, and
// this is only ever compared for equality.
func fileDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// project is Project's body, with mu already held.
func (s *EditServer) project() error {
	// ReadLive, not Read: this document is live and served, so a debounce
	// -timer-driven Project can run concurrently with an ordinary reviewer
	// edit landing through Apply. Read's plain fragment walk races that (see
	// ydoc.ReadLive's comment; reproduced under -race by
	// TestProjectDoesNotRaceConcurrentLoad).
	model, err := ydoc.ReadLive(s.doc)
	if err != nil {
		return err
	}

	// AN INSTRUCTION WHOSE WORDS THE REVIEWER DELETED GOES WITH THEM. This is
	// the one place the server learns the reviewer moved — typing has no HTTP
	// hook. It records which text comments are placed and, when the set whose
	// place has gone moves, rewrites pending.json without them, before the .md
	// is written below, so the unsent round still reaches disk first. It
	// writes nothing to the review map: see lostanchor.go for why. A failed
	// save fails the projection, for the same ordering; see noteRetracted.
	s.noteAnchored(model)
	if err := s.noteRetracted(model); err != nil {
		return err
	}

	// A CODE FENCE IS NEVER REWRITTEN. Its text is literal by definition, so
	// {--…--} inside one is characters, not a suggestion — a shell script that
	// rewrites the string, a document about CriticMarkup, a template using
	// brace delimiters. An earlier build stripped every marker-shaped thing out
	// of every code block on every projection and dropped the inner text of a
	// {--…--} outright: opening a document and stopping the server was enough
	// to destroy it, with nothing recorded anywhere.
	//
	// There is no honest way to tell an author's fenced suggestion from
	// literal text, and nothing needs one: ProseMirror cannot put a mark inside
	// a code block, and suggest.List only ever looks at Block.Inlines, so this
	// pipeline never legitimately creates a fenced marker in the first place.
	// Per-line code suggestions are a phase-3 mechanism with its own syntax.
	// PROJECTED ONTO THE FILE THAT IS THERE, so a block nobody changed keeps
	// the author's own bytes. Serialize renders the MODEL, and the model does
	// not carry spelling — a setext heading, a hard-wrapped paragraph, a "*"
	// bullet and "__bold__" all come back the serializer's way — so an
	// unconditional projection rewrote things nobody edited. Measured
	// 2026-08-22: opening a document and quitting, with no browser attached and
	// no key pressed, changed six separate things. See markdown.SerializeOnto,
	// which states the rule and why it is self-verifying.
	//
	// THE PREVIOUS BYTES ARE THIS SERVER'S OWN LAST OUTPUT after the first
	// projection, which is what makes the preservation hold for the life of the
	// session rather than only once: last time's output already carried the
	// author's spelling, so this time's does too. A read that FAILS falls back
	// to the plain rendering — never to a refusal, for the ledger's reason one
	// layer over: a file galley cannot read must not turn a save into an error.
	prev, prevErr := os.ReadFile(s.MdPath)
	if prevErr != nil {
		prev = nil
	}
	out := markdown.SerializeOnto(model, prev)
	// A RUNNING GALLEY OWNS THE FILE, AND THE OVERWRITE USED TO BE SILENT.
	// Every projection writes the CRDT over the document, so an edit made to
	// the .md by hand while `galley edit` is serving is gone at the next
	// settle. That is the design and not a defect — the document a browser
	// and an agent are both bound to is the live one, and merging a
	// foreign write back into a CRDT that cannot recognise a document it did
	// not build is a different piece of work (see CLAUDE.md on why a fresh
	// parse cannot be merged with the served doc). What WAS a defect is that
	// nothing said so: an appended section on disk, `galley reopen` answering
	// 204, the section gone, no error and no log line — and a whole class of
	// "my edit vanished" with nowhere to look. It is now reported, once per
	// divergence, on the surface the operator is already watching.
	//
	// COMPARED AGAINST WHAT THIS SERVER LAST SAW THERE, not against what it is
	// about to write: the projection legitimately changes the file on every
	// settle, so "the bytes differ from my output" is true constantly and says
	// nothing. The baseline starts as the file NewEdit read (see there — a hand
	// edit before the first projection is the commonest case and was the silent
	// one), and moves to each projection's own output.
	// WHILE A HANDOFF WINDOW IS OPEN THE FILE BELONGS TO THE AGENT and this
	// function does not write it: the browser is read-only, so every change in
	// the live document during the window came IN through an import of that
	// same file, and writing our canonical spelling back out would race the
	// agent's next save. Ownership returns — and this write resumes — the
	// moment the window closes, which is why every close path cuts (and so
	// projects) AFTER closing. See handoff.go.
	if !s.handoffLive.Load() {
		if s.projected != "" {
			if prevErr == nil && fileDigest(prev) != s.projected {
				if s.Log != nil {
					s.Log(fmt.Sprintf("%s changed on disk since galley last saw it — the live document is "+
						"authoritative and has just been written over it. Make the change in the editor, or stop "+
						"galley first", filepath.Base(s.MdPath)))
				}
			}
		}
		if err := writeFileAtomic(s.MdPath, out); err != nil {
			return err
		}
		s.projected = fileDigest(out)
	}

	s.exported.mark()

	fp := s.editFingerprint(model)
	// AND THIS IS WHERE A VERSION IS CUT, with mu held and with the very bytes
	// that just reached the author's file. Nowhere else in the server reads the
	// document to make a version: doing so would be a second answer to "what
	// does this document say right now", and the CRDT is live while a round is
	// open. See versions.go.
	s.cutIfSending(out, false, 0)

	// The notification rides on the projection, and this is the one place in
	// the server that already holds both halves of what a reviewer could be
	// waiting on: the pending suggestions and the threads, read from one
	// validated snapshot.
	//
	// NOT in touch() and NOT in applyModel, and the reason is the whole point
	// of realtime mode. touch() runs inside doc.OnUpdate, on the goroutine
	// that just landed a keystroke, so computing a pending set there would
	// mean a full ydoc.ReadLive per character. applyModel only ever sees
	// SERVER-side mutations — the agent's own suggest/accept/reject — and the
	// change this phase exists to notice is the REVIEWER typing, which arrives
	// over the websocket and never touches applyModel at all.
	//
	// Every mutation reaches here, because every mutation fires doc.OnUpdate
	// and every OnUpdate schedules a projection. Touching unconditionally is
	// deliberate: the notifier's own fingerprint check is what decides whether
	// to fire, and deciding it again here would be a second implementation of
	// the same rule.
	//
	// That includes the mutations the AGENT asked for, and they are quiet
	// without any exception here: mutate seeds the notifier with what the agent
	// saw, so the fingerprint this hands over matches and the check declines.
	// A reviewer keystroke that landed in the same debounce window still fires,
	// because it moves the fingerprint past that seed — which is why the seed
	// lives there and not a "skip this one" flag here.
	//
	// Firing AFTER the file is on disk is also the honest order. The command
	// typically starts an agent that reads the document, and an agent woken
	// before the projection lands would read the previous revision.
	s.notifyFingerprint(fp)
	// THE PAGE-MODE SEAM, and the one place a projection reaches outside itself.
	// It runs AFTER the successful .md write and the version cut, and with mu
	// released — page mode's render routes refusals through recordCannot, which
	// takes mu and reviseMu, so holding either here would deadlock. The caller's
	// deferred Unlock still balances: we relock before returning.
	if s.afterProject != nil {
		md := out
		s.mu.Unlock()
		s.afterProject(md)
		s.mu.Lock()
	}
	return nil
}

// notifyFingerprint hands the just-projected fingerprint to the notifier, if
// this session is live. Callers hold mu.
//
// `on ask` returns without touching, and does NOT discard the notifier: the
// command stays configured so a reviewer can toggle back and forth without
// restarting the server with a flag. The seed is deliberately left alone too —
// going live after a stretch of ask means the next settle compares against
// whatever was last announced, which is exactly the question "is there
// anything new since the agent last looked".
func (s *EditServer) notifyFingerprint(fp string) {
	if s.Mode() != ModeLive {
		return
	}
	// Re-attached here, at the one call site, rather than wired once in
	// NewEdit: Notify is an exported field, and a caller that assigns a fresh
	// Notifier would otherwise detach every blocked reader silently. Cheap,
	// idempotent, and it makes the wake impossible to lose.
	s.Notify.SetWake(s.wakeSettle)
	s.Notify.Touch(fp)
}

// ReviseWatch reports whether the reviewer is still waiting on a revision they
// asked for, and when they asked. See reviseWatch for why this is a different
// question from ReviseInFlight's.
func (s *EditServer) ReviseWatch() (time.Time, bool) {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	if s.watch == nil {
		return time.Time{}, false
	}
	return s.watch.at, true
}

// watchRound is the round the open response window answers, or 0 when no press
// is outstanding. It is separate from ReviseWatch because that answers "is a
// press outstanding" and this answers "which one" — and a caller handed a bool
// and an instant has no way to name the round in a diagnostic.
func (s *EditServer) watchRound() int {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	if s.watch == nil {
		return 0
	}
	return s.watch.round
}

// The two modes a session can be in. `on ask` is the default and means the
// agent hears nothing until the reviewer presses Revise; `● live` means the
// document wakes it whenever it settles.
const (
	ModeAsk  = "ask"
	ModeLive = "live"
)

// Mode reports whether this session is live or on ask.
func (s *EditServer) Mode() string {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	if s.mode == "" {
		return ModeAsk
	}
	return s.mode
}

// SetMode switches between `on ask` and `● live`, refusing anything else.
//
// It does not check that a notifier is configured. Going live with nothing to
// wake is a real state — the CLI says so at startup — and refusing the toggle
// would leave the reviewer unable to see the surface at all. The mode is the
// reviewer's intent; whether anyone is listening is the operator's problem.
func (s *EditServer) SetMode(mode string) error {
	if mode != ModeAsk && mode != ModeLive {
		return fmt.Errorf("unknown mode %q — want %q or %q", mode, ModeAsk, ModeLive)
	}
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	s.mode = mode
	return nil
}

// handleMode reads or sets the session's mode.
//
// GET is open like every other read here. POST goes through decode — and so
// through guard — exactly as accept and reject do: this one changes server
// BEHAVIOUR, and a page on another site putting a document into live mode
// would start running the configured command on the reviewer's machine.
//
// The payload carries "mode" and nothing else. The design sketch also listed
// "hold" and "queued", but hold is a browser-side queue over what the rail
// displays — the suggestions are in the CRDT either way — so the server has no
// honest value for either field, and emitting a constant false would advertise
// state it does not keep.
func (s *EditServer) handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]string{"mode": s.Mode()})
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.SetMode(in.Mode); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"mode": s.Mode()})
}

// trailChanges reads the trail under its own lock, copied so a caller can
// marshal it while a save lands.
//
// The copy is SHALLOW, so the *string Before/After travel by pointer and every
// caller's slice shares them with the stored trail. Safe because nothing in Go
// Handler is edit mode's HTTP surface: the editor shell at /, the y-websocket
// endpoint the browser binds the fragment through, the JSON endpoints the
// agent (and the editor's own buttons) drive, and the built editor bundle.
//
// The only static content behind it is the document's own
// figures: serveSibling answers GET and HEAD for the image files beside the
// .md, and nothing else. Anything that is not "/" and not a figure is a 404.
// A "serve the whole directory" http.FileServer is refused — see editassets.go for why the extension allowlist is
// load-bearing, why every read goes through an os.Root rather than through
// string arithmetic on the URL, and why a figure that is a SYMLINK is refused
// even when it points inside the directory.
func (s *EditServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/yjs/{room}", s.onlyThisRoom(s.yjs))
	mux.HandleFunc("/_galley/editor.js", serveAssetHint("assets/editor.js",
		"application/javascript; charset=utf-8", "`just assets`, which builds the editor bundle"))
	mux.HandleFunc("/_galley/editor.css", serveAssetHint("assets/editor.css",
		"text/css; charset=utf-8", "`just assets`, which builds the editor bundle"))
	// The diagram renderer, fetched by the figure NodeView on the first mermaid
	// fence in a document and never on a document that has none. Its own asset
	// rather than part of editor.js because it is several megabytes; see the
	// justfile's assets target.
	mux.HandleFunc("/_galley/mermaid.js", serveAssetHint("assets/mermaid.js",
		"application/javascript; charset=utf-8", "`just assets`, which builds the editor bundle"))
	// The caret, committed rather than built — unlike the three routes above,
	// this one never 404s on a fresh checkout.
	mux.HandleFunc("/_galley/favicon.svg", serveAssetHint("assets/favicon.svg", "image/svg+xml",
		"`git checkout -- internal/serve/assets/favicon.svg`; it is committed"))
	// The split-pane preview iframe fetches the live page and its assets here.
	// Only live in page mode; a plain 404 in markdown mode. See preview.go.
	mux.HandleFunc("/_galley/preview/", s.servePreview)
	mux.HandleFunc("/_galley/pending", s.handlePending)
	mux.HandleFunc("/_galley/instruct", s.handleInstruction)
	mux.HandleFunc("/_galley/revert", s.handleRevert)
	mux.HandleFunc("/_galley/instruction/delete", s.handleInstructionDelete)
	// The one case the agent's revision has no answer for. The revision
	// itself lands by editing the .md under the handoff — see handoff.go.
	mux.HandleFunc("/_galley/cannot", s.handleCannot)
	mux.HandleFunc("/_galley/mode", s.handleMode)
	mux.HandleFunc("/_galley/revise", s.handleRevise)
	mux.HandleFunc("/_galley/stop", s.handleStop)
	mux.HandleFunc("/_galley/ack", s.handleAck)
	// The reviewer taking the document back mid-window — see handoff.go.
	mux.HandleFunc("/_galley/handoff/cancel", s.handleHandoffCancel)
	mux.HandleFunc("/_galley/wait", s.handleWait)
	// History list and rendered views are reads. Restore is deliberately a
	// separate guarded mutation: it copies a version into the working draft and
	// never rewrites the version store.
	mux.HandleFunc("/_galley/versions", s.handleVersions)
	mux.HandleFunc("/_galley/versions/view", s.handleVersionView)
	mux.HandleFunc("/_galley/versions/restore", s.handleVersionRestore)
	mux.HandleFunc("/_galley/rev", s.handleEditRev)
	mux.HandleFunc("/_galley/saved", s.handleEditSaved)
	mux.HandleFunc("/", s.handleEditRoot)
	return mux
}

// onlyThisRoom refuses any room but the one this run created, before the
// websocket upgrade — so a peer holding a previous run's document cannot sync
// it in. Without this the ws server would happily open the requested room and
// merge whatever the peer offered, which is how a restart duplicated the whole
// document. See newInstanceToken.
func (s *EditServer) onlyThisRoom(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("room") != s.Room {
			// Gone, not NotFound: the room named is exactly the kind of thing
			// that did exist and stopped, and the tab asking for it is about
			// to reload into the current one.
			http.Error(w, "that room belonged to a previous run of galley edit; reload", http.StatusGone)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *EditServer) handleEditRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.serveSibling(w, r)
		return
	}
	var buf bytes.Buffer
	previewURL := ""
	if s.pageMode {
		previewURL = "/_galley/preview/" + s.previewRel
	}
	if err := editShell.Execute(&buf, struct {
		Title      string
		Room       string
		PageMode   bool
		PreviewURL string
	}{
		Title:      filepath.Base(s.MdPath),
		Room:       s.Room,
		PageMode:   s.pageMode,
		PreviewURL: previewURL,
	}); err != nil {
		http.Error(w, "edit shell: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// handleEditRev reports the document's mtime, so the page can notice
// that something outside the editor rewrote the file underneath it.
//
// It also carries this run's room name. A tab that outlived a restart is
// holding a room that no longer exists — its websocket is being refused, so it
// cannot see anything — and comparing this field against the room it booted
// with is how it learns to reload.
func (s *EditServer) handleEditRev(w http.ResponseWriter, r *http.Request) {
	st, err := os.Stat(s.MdPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"rev":  st.ModTime().UnixNano(),
		"room": s.Room,
	})
}

// handleEditSaved reports when the projection last reached disk, in
// epoch milliseconds, so the editor can say "on disk" honestly rather than
// meaning "synced to a peer".
func (s *EditServer) handleEditSaved(w http.ResponseWriter, r *http.Request) {
	at := s.LastExport()
	var ms int64
	if !at.IsZero() {
		ms = at.UnixMilli()
	}
	writeJSON(w, map[string]int64{"saved": ms})
}

// PendingView is the reviewer instruction round handed to the agent.
type PendingView struct {
	Instructions []InstructionView  `json:"instructions"`
	Blocks       []suggest.BlockRef `json:"blocks,omitempty"`
	// Changes is what the reviewer altered BY HAND in the round being sent —
	// computed from two versions, never from the browser's ghosts. See
	// reviewerchanges.go for why an agent that is not told this rewrites the
	// reviewer's deletions back into the document.
	Changes []ReviewerChange `json:"changes,omitempty"`
	// ChangesDropped is how many changes the cap left out, so a truncation is
	// stated rather than read as "that is all of them".
	ChangesDropped int `json:"changesDropped,omitempty"`
}

// InstructionView is the complete work order handed to the agent. Instructions
// are immutable facts of a reviewer round; they are not conversations and do
// not carry accept/reject/resolve state.
type InstructionView struct {
	Key       string         `json:"key"`
	Text      string         `json:"text"`
	Quote     string         `json:"quote,omitempty"`
	At        time.Time      `json:"at"`
	Run       string         `json:"run,omitempty"`
	Anchor    string         `json:"anchor,omitempty"`
	AnchorKey string         `json:"anchorKey,omitempty"`
	BlockKind string         `json:"blockKind,omitempty"`
	Region    *review.Region `json:"region,omitempty"`
}

func (s *EditServer) handlePending(w http.ResponseWriter, r *http.Request) {
	view, err := s.pending()
	if err != nil {
		writeBusy(w, err)
		return
	}
	writeJSON(w, view)
}

// pending reads the live document WITHOUT taking the mutation mutex.
//
// That is the ratified rule for this server, not a local shortcut: a pure
// READ may skip mu; a read-modify-write may not. Two reasons, both
// load-bearing. ydoc.ReadLive validates its own snapshot by construction — it
// compares crdt.CaptureSnapshot before and after, so a read that interleaved
// with any write (insert or delete) is discarded and retried rather than
// returned — which means a read needs nothing from mu that it does not
// already have. And a poller must not queue behind a long mutation: the
// editor's sidebar refreshes on a timer, and making it block until an agent's
// suggest finishes would turn a background write into visible UI stutter.
//
// The rule does not extend to mutations, and the distinction is the whole
// design: ReadLive guarantees the snapshot was consistent WHEN TAKEN, not that
// it is still current when a transform computed from it is written back. Only
// mu gives a read-modify-write that guarantee against other server-side
// writers. See mutate.
func (s *EditServer) pending() (PendingView, error) {
	model, err := s.readLive()
	if err != nil {
		return PendingView{}, err
	}
	view := PendingView{
		Instructions: s.liveInstructions(model),
		Blocks:       suggest.Blocks(model),
	}
	// AND WHAT THE REVIEWER CHANGED BY HAND, on the same payload as what they
	// wrote about it. The rail is the list of what this round carries, and an
	// edit is half of that. See liveChanges.
	view.Changes, view.ChangesDropped = s.liveChanges(model)
	if view.Instructions == nil {
		view.Instructions = []InstructionView{}
	}
	return view, nil
}

func (s *EditServer) editFingerprint(model docmodel.Doc) string {
	h := sha256.New()
	_, _ = h.Write(markdown.Serialize(model))
	for _, instruction := range s.liveInstructions(model) {
		_, _ = fmt.Fprintf(h, "\x00%s\x00%s\x00", instruction.Quote, instruction.Text)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (s *EditServer) ReviseInFlight() (<-chan struct{}, bool) {
	s.reviseMu.Lock()
	defer s.reviseMu.Unlock()
	if s.reviseDone == nil {
		return nil, false
	}
	return s.reviseDone, true
}

// writePending answers a successful mutation with the document's new pending
// state, so the caller — browser or agent — does not need a second round trip
// to see what its own request produced.
func (s *EditServer) writePending(w http.ResponseWriter) {
	view, err := s.pending()
	if err != nil {
		writeBusy(w, err)
		return
	}
	writeJSON(w, view)
}

// writeMutationError answers a failed mutation. A 503 goes through writeBusy so
// the caller gets the same retriable sentence a failed READ produces — these
// endpoints were leaking ydoc's raw "concurrent write" text instead, which
// names an internal package and does not say what to do about it. Everything
// else is the transform's own message, which is exactly what the caller needs.
func writeMutationError(w http.ResponseWriter, code int, err error) {
	if code == http.StatusServiceUnavailable {
		writeBusy(w, err)
		return
	}
	http.Error(w, err.Error(), code)
}

// writeBusy answers a read that could not get a consistent snapshot. 503 with
// a retriable body rather than 500: nothing is broken, the document was simply
// being written to throughout, and trying again in a moment is the right
// response.
func writeBusy(w http.ResponseWriter, err error) {
	if errors.Is(err, ydoc.ErrConcurrentWrite) {
		http.Error(w, "the document is being written to right now — retry in a moment",
			http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// mutationReadAttempts bounds how many times a server-side mutation re-reads
// the document after ydoc.ReadLive refuses a snapshot. ReadLive already
// retries 8 times internally against ordinary interference; these outer
// attempts only matter when a browser is writing continuously, and a request
// that cannot get a snapshot in four goes is better answered 503 than held
// open.
const mutationReadAttempts = 4

func (s *EditServer) readLive() (docmodel.Doc, error) {
	var err error
	for attempt := 0; attempt < mutationReadAttempts; attempt++ {
		var model docmodel.Doc
		model, err = ydoc.ReadLive(s.doc)
		if !errors.Is(err, ydoc.ErrConcurrentWrite) {
			return model, err
		}
	}
	return docmodel.Doc{}, err
}

// requester says who asked for a server-side mutation, and exists for exactly
// one reason: a wake-up is FOR the agent, so the agent's own writes must not
// produce one. See mutate's seed and notifyFingerprint.
//
// The zero value is the REVIEWER, deliberately: a mutation nobody classified
// wakes the agent, which is this server's previous behaviour and the safe
// direction to fail in. A spurious wake is a nuisance; a missed one is a
// decision the agent never hears about.
type requester int

const (
	byReviewer requester = iota
	byAgent
	bySystem
)

// requesterFor reads the actor off the author a mutation carries.
//
// AUTHOR, NOT TRANSPORT. The tempting alternative is to call a request with no
// Origin header the agent's — serve.guard already treats a missing Origin as
// "the CLI, curl, an agent" — but that is the wrong question asked of the right
// evidence: a request with no Origin can be the reviewer's own, from curl or a
// script, and classifying that as the agent would make the reviewer's own
// change silent.
//
// The author is a distinction this server already carries and already trusts
// for exactly this meaning: review.Read reads `Author == AuthorCourt` as "the
// reviewer said this", and the editor bundle names itself on every mutating
// request it sends (web/entry.js's AUTHOR). So anything that is NOT the
// reviewer's own name is a write on the agent's behalf.
func requesterFor(author string) requester {
	if author == review.AuthorCourt {
		return byReviewer
	}
	return byAgent
}

// mutate is the shape every server-side transform of the document takes:
// under the mutation mutex, read a validated snapshot, transform it as a plain
// docmodel value, and write the whole thing back through the websocket
// server's Apply so peers see it.
//
// The mutex is what makes the read-transform-write sequence atomic WITH
// RESPECT TO OTHER SERVER-SIDE WRITERS — the instruction and revert
// endpoints, the send, the agent's import, Project, and Flush. Nothing in ygo offers that atomicity to piggyback on, so
// there is nothing cheaper to use instead.
//
// In particular it cannot be Apply-scoped, and the reason is worth stating
// precisely because the phase-1 plan proposed exactly that ("read via ydoc.Read
// inside the Apply callback, before transact"). Apply holds NO document lock
// across its callback — established by reading ygo v1.43.0's
// provider/websocket/inject.go: Apply subscribes an OnUpdate capture, hands fn
// a transact helper that is a plain doc.Transact with Apply's own origin, and
// merges whatever that helper captured into one broadcast. The lock is taken
// and released inside each individual transact call, nowhere else. (Reading
// inside the callback is therefore fine, and this file already does it —
// NewEdit and applyModel both call doc.GetMap there.)
//
// What Apply gives is broadcast atomicity, NOT state atomicity: several
// transact calls in one callback reach peers as a single update, but the
// document is unlocked between them, and unlocked between the callback
// starting and its first transact. So a read placed inside the callback is no
// more serialized against a concurrent writer than a read placed outside it —
// it would buy nothing, which is why the mutex, and not callback placement, is
// what makes this correct.
//
// KNOWN, ACCEPTED LIMITATION — last writer wins against the BROWSER. The
// mutex does not (and cannot) hold back a peer's edit arriving over the
// websocket, and the write-back is ydoc.Load, a full fragment rebuild. So a
// keystroke landing in the window between the read and the Apply is clobbered:
// the rebuilt fragment reflects the snapshot plus this mutation, not the
// keystroke. The window is milliseconds, and capture mode means the reviewer
// is asking the agent to work and then reading the result rather than typing
// through it — but it is a real hole, not a theoretical one. Phase 3's
// targeted-mutation design (edit the specific runs a suggestion touches,
// instead of reloading the document) is what removes it; do not attempt
// targeted fragment mutation before then.
func (s *EditServer) mutate(by requester, fn func(docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error)) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateLocked(by, fn)
}

// mutateLocked is mutate's body, with mu already held. mutateUnsent shares it
// so it can mirror the unsent round before releasing mu.
func (s *EditServer) mutateLocked(by requester, fn func(docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error)) (int, error) {
	model, err := s.readLive()
	if err != nil {
		return statusFor(err), err
	}
	// CLONED BEFORE THE TRANSFORM, and the clone is the whole reason the
	// targeted write is safe. A transform here may rewrite the model it was
	// handed in place — suggest.ClearInstructions does — so keeping `model` and
	// passing it as the BEFORE would hand ydoc.Write two views of one object
	// graph, an empty diff, and a document nobody wrote to. See docmodel.Clone,
	// which records what that looked like when it happened.
	before := docmodel.Clone(model)
	updated, extra, err := fn(model)
	if err != nil {
		return 0, err // the caller decides what a transform failure means
	}
	// Marks the transform just created have no identity yet. Minting here —
	// after the transform, before the write — is what makes every suggestion
	// in the live document addressable, and it is idempotent, so the marks
	// that came back from readLive keep the runs the browser already has.
	updated = suggest.MintRuns(updated)
	if err := s.applyModel(before, updated, extra); err != nil {
		return http.StatusInternalServerError, err
	}
	// THE AGENT HAS JUST LOOKED. Every mutating endpoint answers with the whole
	// pending view (writePending), so an agent that wrote here has already been
	// handed everything pending as of its own write — including anything the
	// reviewer did that it had not yet heard about. Recording that state as
	// already-announced is therefore not a suppression, it is the truth the
	// notifier's fingerprint is there to express: "has the document moved since
	// the agent last saw it".
	//
	// Seeding rather than skipping the notify, and the difference is the whole
	// correctness argument. The notification rides on a DEBOUNCED projection
	// (see project), so one projection can cover this write and a reviewer
	// keystroke that landed alongside it. A skipped notify would swallow the
	// reviewer's half; a seed cannot — the projected fingerprint includes the
	// keystroke, so it differs from what was seeded here and fires. It also
	// leaves the notifier's own fingerprint check the single decider of whether
	// to fire, which is the rule project's comment states and this must not
	// duplicate.
	//
	// Not gated on live mode. The seed says what the agent has seen, which is
	// true whether or not anyone is listening right now, and making the baseline
	// depend on a toggle the agent never sees would mean a stretch of `on ask`
	// ending with the agent's own suggestions read as news the moment the
	// reviewer goes live.
	switch by {
	case byAgent:
		s.SeedNotify()
		// AND THE ROUND NOW CARRIES THE AGENT. This is the one place that knows
		// it exactly — see versions.go's roundAuthors, which asks about the
		// reviewer two other ways because neither of those is exact.
		s.markMovedByAgent()
	case byReviewer:
		// Reviewer mutations made through an endpoint are server writes too, so
		// OnUpdate deliberately ignores them. Name the reviewer here instead;
		// otherwise an instruction-only round has no author even though the
		// ledger correctly carries what Court asked for.
		s.movedByReviewer.Store(true)
	}
	return http.StatusOK, nil
}

func statusFor(err error) int {
	if errors.Is(err, ydoc.ErrConcurrentWrite) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// applyModel writes model back to the live document in ONE Apply — so the meta
// bump, the fragment reload, and whatever thread write rides along with it
// merge into a single captured update, broadcast once, rather than reaching a
// connected peer as several documents in a row.
//
// The rev bump is not decoration: Apply refuses a mutation that produces no
// changes, and it gives a peer a single value to watch to know the server
// rewrote the document underneath it. Callers hold mu.
func (s *EditServer) applyModel(before, model docmodel.Doc, extra func(*crdt.Doc, review.Tx)) error {
	s.rev++
	rev := s.rev
	s.serverWrites.Add(1)
	defer s.serverWrites.Add(-1)
	return s.yjs.Apply(context.Background(), s.Room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			// Resolved before the transaction opens: GetMap takes the same
			// non-reentrant lock transact holds.
			meta := doc.GetMap("meta")
			transact(func(txn *crdt.Transaction) { meta.Set(txn, "rev", rev) })
			// WRITE, NOT LOAD — see ydoc.Write. Load deletes the fragment's
			// children and writes them again, which orphans the browser's undo
			// stack whole; Write formats in place when the only difference is
			// marks on identical text, and falls back to Load otherwise. Three
			// of the five server-side mutations are mark-only, and they are
			// every gesture a reviewer makes while reviewing.
			ydoc.Write(doc, transact, before, model)
			if extra != nil {
				extra(doc, transact)
			}
		})
}

// instructionRequest is the reviewer's one markup action: attach an
// instruction to a text range, block, or the whole document.
type instructionRequest struct {
	Op string `json:"op"`
	// Key names the instruction an "edit" rewrites. Every other op CREATES an
	// instruction and so has no key yet; this is the one that addresses one
	// that already exists, and it is the thread's stable key rather than an
	// ordinal, for the reason this repository has recorded twice — an ordinal
	// renumbers the moment anything before it changes.
	Key    string `json:"key"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Anchor string `json:"anchor"`
	Text   string `json:"text"`
	Target string `json:"target"`
	Author string `json:"author"`
	// Path/From/To are the editor's way of saying "comment on THIS", where
	// Target is the CLI's "comment on the one place that says this". When Path
	// is present it wins: the browser has the reviewer's actual selection, and
	// asking the server to find it again by text is what made commenting on a
	// word that occurs twice fail. From/To are rune offsets into the block's
	// concatenated inline text.
	Path []int `json:"path"`
	// ToPath is the block the selection ENDS in, when that is a different block
	// from the one it starts in. Absent for the ordinary within-a-block
	// selection, which is why it is a separate field rather than Path becoming
	// a pair: a cross-block selection is the unusual case and the wire should
	// read that way.
	//
	// Its absence is what made every multi-block selection fail. The browser
	// had no way to say "this ends over there", so it sent the concatenated
	// TEXT instead and the server searched for it inside single blocks.
	ToPath []int `json:"toPath"`
	From   int   `json:"from"`
	To     int   `json:"to"`
	// Region is the rectangle on a figure a "comment_block" note points at,
	// in fractions of the figure's box. Optional, and absent is the ordinary
	// case: a block note with no rectangle is a note about the whole block.
	Region *review.Region `json:"region,omitempty"`
}

// errNotPending is an edit naming a key that is not pending: never filed,
// already deleted or sent, or retracted. handleInstruction answers it 404.
var errNotPending = errors.New("there is no unsent instruction")

func (s *EditServer) handleInstruction(w http.ResponseWriter, r *http.Request) {
	var in instructionRequest
	if !decode(w, r, &in) {
		return
	}
	if s.refuseSealed(w, verbReview) {
		return
	}
	if s.refuseHandoff(w) {
		return
	}
	author := in.Author
	if author == "" {
		author = review.AuthorCourt
	}
	if author != review.AuthorCourt {
		http.Error(w, "instructions belong to the reviewer", http.StatusForbidden)
		return
	}
	at := time.Now().UTC()

	// Validated BEFORE the mutation, so a refused rectangle leaves nothing
	// behind. Writing the note first and then rejecting the region would put an
	// unpinned note in the reviewer's file with no record that the pin was the
	// part that failed — the same shape as every half-applied write this
	// codebase has had to unpick.
	//
	// Refused, never clamped: a clamped rectangle is a pin somewhere plausible,
	// and a plausible-but-wrong anchor attached to a real comment is precisely
	// the failure this line of work exists to remove.
	if in.Region != nil {
		if in.Op != "comment_block" {
			// There is no box to be a fraction of. A region on a document note
			// or on a range comment is a rectangle nothing can ever draw.
			writeMutationError(w, http.StatusBadRequest,
				fmt.Errorf("suggest: a region needs a block to be a region OF — op %q has none", in.Op))
			return
		}
		if err := in.Region.Valid(); err != nil {
			writeMutationError(w, http.StatusBadRequest, err)
			return
		}
	}

	code, err := s.mutateUnsent(requesterFor(author), func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		switch in.Op {
		case "comment":
			// A comment is a highlight in the document, carrying the comment's
			// ID on every piece, PLUS a thread carrying what was actually said.
			// The ID is minted here, once, and is the thread's key for its whole
			// life: the file's mark and pending.json's comment are linked by it
			// and by nothing else. The highlight's ordinal is a display
			// coordinate and is never persisted (see suggest.Pending.ID).
			key := unsent.NewID(unsent.KindText)
			out, err := commentRanged(model, in, key, author, at)
			if err != nil {
				return docmodel.Doc{}, nil, err
			}
			// The heading is what the highlight actually covers, not what the
			// browser called it: the two differ whenever the selection had
			// whitespace on it, and a cross-paragraph comment's pieces are
			// joined the way List joins them.
			heading := in.Target
			if anchored, ok := suggest.CommentText(out, key); ok {
				heading = anchored
			}
			text := in.Text
			return out, func(doc *crdt.Doc, tx review.Tx) {
				review.Bind(doc, tx).Append(key, heading, author, text, at)
			}, nil
		case "edit":
			// EDITING AN INSTRUCTION IS ONE MUTATION, NOT A DELETE FOLLOWED BY
			// A FILE. The obvious client-side shape — remove it, post it
			// again — was considered and refused: it is two round trips with
			// the instruction absent in between, so a failure of the second
			// leaves the reviewer's words nowhere at all, and for a RANGE
			// instruction it cannot even be attempted in the other order,
			// since CommentOn refuses to highlight text that already carries
			// one. Both halves of the change land inside the one mutate
			// closure or neither does.
			//
			// A RETRACTED COMMENT IS NOT PENDING, so it cannot be edited. Its
			// thread stays in the review map until the next send (see
			// lostanchor.go), so finding the key there is not enough: an edit
			// saved into it is saved into a comment no surface shows, and the
			// reviewer's words are gone without a word said. A key that is not
			// pending is a 404, and the browser keeps the words.
			threads := review.Read(s.doc)
			var target review.Thread
			for _, thread := range threads {
				if thread.Key != in.Key {
					continue
				}
				for _, entry := range thread.Entries {
					if entry.Author == review.AuthorCourt && strings.TrimSpace(entry.Text) != "" {
						target = thread
					}
				}
			}
			if target.Key == "" || s.retractedIn(model)[in.Key] {
				return docmodel.Doc{}, nil, fmt.Errorf("%w %q", errNotPending, in.Key)
			}
			text := strings.TrimSpace(in.Text)
			if text == "" {
				return docmodel.Doc{}, nil, fmt.Errorf("an instruction with no words is a delete — use it")
			}
			// THE WORDS ARE IN pending.json ALONE, for every kind, so an edit
			// changes nothing in the document: the file holds only the
			// comment's ID mark, which an edit does not move. The model goes
			// back unchanged and applyModel's rev bump carries the review write.
			key := in.Key
			return model, func(doc *crdt.Doc, tx review.Tx) {
				// The heading is left alone — an edit changes what was ASKED,
				// never what it was asked ABOUT.
				review.Bind(doc, tx).SetComment(key, "", text, at)
			}, nil
		case "comment_block", "comment_document":
			// A block comment's ID mark goes into the document on its own line
			// after the block; a document comment has no mark at all. The words
			// and any rectangle live in pending.json alone, linked by the ID.
			out, key, err := commentAnchored(model, in)
			if err != nil {
				return docmodel.Doc{}, nil, err
			}
			anchor := suggest.Anchor{Kind: suggest.AnchorDocument}
			if in.Op == "comment_block" {
				anchor = suggest.Anchor{Kind: suggest.AnchorBlock, Target: in.Target}
			}
			heading := headingForAnchor(out, anchor)
			text := in.Text
			region := in.Region
			return out, func(doc *crdt.Doc, tx review.Tx) {
				// The words, the anchor and the rectangle in ONE transaction:
				// see review.AppendAbout.
				review.Bind(doc, tx).AppendAbout(key, heading, author, text, at, review.About{
					Anchor: string(anchor.Kind), BlockKind: suggest.BlockKindFor(out, anchor), Region: region,
				})
			}, nil
		default:
			return docmodel.Doc{}, nil, fmt.Errorf(
				"unknown instruction target %q — want comment, comment_block, comment_document or edit", in.Op)
		}
	})
	if err != nil {
		if code == 0 && errors.Is(err, errNotPending) {
			code = http.StatusNotFound
		}
		if code == 0 {
			// A transform failure is the caller's problem, not the server's:
			// text that matched zero or several times, or already carries a
			// pending suggestion of the same family. suggest's own message
			// names the count, which is exactly what the caller needs.
			code = http.StatusBadRequest
		}
		// THE REFUSAL EXISTED ONLY AS A TOAST. `"…" matched 0 times, want
		// exactly 1` out of suggest.findUnique reached the reviewer's browser
		// and nowhere else — not the server log, not the ledger, not the
		// version store — so diagnosing one meant asking the reviewer what
		// their screen had said. Recorded WITH the target, because the message
		// alone does not say what could not be placed on what.
		s.debugAnchorRefused(in.Op, in.Target, err)
		writeMutationError(w, code, err)
		return
	}
	s.writePending(w)
}

// handleInstructionDelete removes one unsent reviewer instruction and the
// trace it placed in the draft. Sent instructions live in the immutable round
// ledger and never reach this endpoint.
func (s *EditServer) handleInstructionDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.refuseSealed(w, verbReview) {
		return
	}
	if s.refuseHandoff(w) {
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	if in.Key == "" {
		http.Error(w, "which instruction? pass its key", http.StatusBadRequest)
		return
	}
	code, err := s.mutateUnsent(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		threads := review.Read(s.doc)
		var instruction bool
		for _, thread := range threads {
			if thread.Key != in.Key {
				continue
			}
			for _, entry := range thread.Entries {
				if entry.Author == review.AuthorCourt && strings.TrimSpace(entry.Text) != "" {
					instruction = true
					break
				}
			}
		}
		if !instruction {
			return docmodel.Doc{}, nil, fmt.Errorf("there is no unsent instruction %q", in.Key)
		}
		out, _ := suggest.Detach(model, threads, in.Key)
		return out, func(doc *crdt.Doc, tx review.Tx) {
			_ = review.Bind(doc, tx).Delete(in.Key)
		}, nil
	})
	if err != nil {
		if code == 0 {
			code = http.StatusBadRequest
		}
		writeMutationError(w, code, err)
		return
	}
	s.writePending(w)
}

// commentRanged applies a comment anchored to a RANGE OF PROSE, choosing
// between the three coordinates a caller can give it.
//
// It is a function rather than a switch inside handleInstruction because that
// handler was already at the cyclomatic bound the linter enforces, and adding
// the cross-block arm took it over. Extracting the choice is the fix; a
// suppression would have been this repository's own "any suppression carries an
// inline rationale" spent on a rationale that was really just "the function is
// too big".
//
// THE ORDER IS SPECIFIC-TO-GENERAL. The browser knows exactly what the reviewer
// selected and says so in coordinates; the CLI knows only some words and asks
// the server to find them. Asking the server to search when the page already
// knew is what made commenting on a word that occurs twice fail.
func commentRanged(model docmodel.Doc, in instructionRequest, id, author string, at time.Time) (docmodel.Doc, error) {
	// A COMMENT WITH NO WORDS IS REFUSED, not filed as a bare highlight: its ID
	// mark would sit in the .md with no comment in pending.json for it to name.
	if strings.TrimSpace(in.Text) == "" {
		return docmodel.Doc{}, errors.New("an instruction needs words")
	}
	switch {
	case in.Path != nil && in.ToPath != nil:
		// A SELECTION THAT CROSSES A BLOCK BOUNDARY. It used to fall to the
		// Target branch below, where `findUnique` searches block by block for a
		// string that is the concatenation of two — zero matches, every time,
		// and only after the reviewer had finished typing their instruction.
		return suggest.CommentAcross(model, in.Path, in.From, in.ToPath, in.To, id, author, at)
	case in.Path != nil:
		return suggest.CommentOnRange(model, in.Path, in.From, in.To, id, author, at)
	default:
		return suggest.CommentOn(model, in.Target, id, author, at)
	}
}

// commentAnchored mints the block or document comment named by in and writes
// its mark: an ID note after the block, or, for the whole document, nothing,
// so the model comes back unchanged. A comment with no words is refused, as
// for a range comment: its mark would name nothing in pending.json.
func commentAnchored(model docmodel.Doc, in instructionRequest) (docmodel.Doc, string, error) {
	if strings.TrimSpace(in.Text) == "" {
		return docmodel.Doc{}, "", errors.New("an instruction needs words")
	}
	if in.Op != "comment_block" {
		return model, unsent.NewID(unsent.KindDocument), nil
	}
	key := unsent.NewID(unsent.KindBlock)
	out, err := suggest.CommentOnBlock(model, in.Target, key)
	return out, key, err
}

// headingForAnchor names what a thread is about, for the panel and for
// `galley pending` — the block's label, or the document. Read off the
// document the comment was just written into, so it reflects the block as it
// stands rather than as the caller described it.
func headingForAnchor(model docmodel.Doc, a suggest.Anchor) string {
	if a.Kind != suggest.AnchorBlock {
		return "the whole document"
	}
	for _, b := range suggest.Blocks(model) {
		if b.Key == a.Target {
			return b.Label
		}
	}
	return a.Target
}

// reviewerRound is one reviewer-to-agent handoff after its working-copy
// affordances have been removed. Revise and live settle deliberately share
// this value and the function that builds it: the trigger may differ, but what
// the agent receives and what History records may not.
type reviewerRound struct {
	pending     PendingView
	round       int
	fingerprint string
}

// sendReviewerRound performs the common half of an explicit Revise and an
// automatic live settle.
//
// Capture first, then clean, then cut. The captured pending view is the work
// order the agent receives; the clean projection is the document version the
// reviewer sent. Highlight marks and note blocks are addresses for unsent
// instructions, not authored prose, so they belong in neither the version nor
// its diff. The instruction text has already been copied into the round record
// and ledger before those affordances are removed.
func (s *EditServer) sendReviewerRound(reason string) (reviewerRound, int, error) {
	handoff, err := s.pending()
	if err != nil {
		return reviewerRound{}, statusFor(err), err
	}
	inRound := make(map[string]bool, len(handoff.Instructions))
	for _, instruction := range handoff.Instructions {
		inRound[instruction.Key] = true
	}
	said, instructions, markSaid := s.reviewerInstruction(inRound)
	keys := make([]string, 0, len(handoff.Instructions))
	// THE ROUND IS WHERE THE KEYS SURVIVE. The mutation below DELETES every
	// thread this send carries — an instruction is discharged by the revision,
	// not resolved — so by the time the agent's manifest names a key, the
	// review holds no record of it at all. Recording the asks on the round that
	// asked them is what makes the agent's testimony checkable rather than
	// taken on trust. See versions.Ask and manifest.go.
	asks := make([]versions.Ask, 0, len(handoff.Instructions))
	for _, instruction := range handoff.Instructions {
		if instruction.Key != "" {
			keys = append(keys, instruction.Key)
		}
		asks = append(asks, versions.Ask{
			Key: instruction.Key, Text: instruction.Text, Quote: instruction.Quote,
		})
	}
	if testHookAfterRoundCaptured != nil {
		testHookAfterRoundCaptured()
	}
	if code, err := s.mutate(bySystem, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		// HELD ASIDE BEFORE THE CLEAR, under mu and outside any Transact: once
		// the threads below are deleted, the review map no longer holds the
		// round being sent, and anything that rewrites pending.json from the
		// map before the cut (a comment filed in this window, a save from
		// inside project) would drop it. See saveUnsentLocked.
		//
		// MERGED, NEVER ASSIGNED, and released by this send's own keys: a
		// Revise press and a live settle can be in here at once, and either
		// one assigning or nil-ing the whole map would drop the other's round.
		if s.sending == nil {
			s.sending = map[string]unsent.Comment{}
		}
		maps.Copy(s.sending, sendingOf(review.Read(s.doc), keys))
		// A RETRACTED COMMENT GOES WITH THIS CLEAR. Its words were deleted, so
		// the pending view hid it and it is not among keys; its thread has
		// waited in the review map for this write, which already reaches every
		// peer and is seeded below. See lostanchor.go.
		retracted := s.retractedIn(model)
		s.forgetRetractedLocked(retracted)
		// AND A COMMENT FILED SINCE THE CAPTURE IS NOT RETRACTED BY IT.
		// ClearInstructions lifts every highlight, this round's or not, so a
		// text comment filed between the capture and this write loses its mark
		// here without the reviewer deleting anything. Forgetting that it was
		// placed shows it as an unplaced card, rather than letting the next
		// projection read the lifted mark as a retraction and hide it.
		s.forgetPlacedLocked(keys)
		return suggest.ClearInstructions(model), func(doc *crdt.Doc, tx review.Tx) {
			session := review.Bind(doc, tx)
			for _, key := range keys {
				_ = session.Delete(key)
			}
			for key := range retracted {
				_ = session.Delete(key)
			}
		}, nil
	}); err != nil {
		// Nothing was sent: whatever the clear did not delete is still in the
		// map, and nothing is in flight.
		s.mu.Lock()
		s.releaseSendingLocked(keys)
		s.mu.Unlock()
		return reviewerRound{}, code, err
	}

	// Clearing the working-copy carriers is part of THIS send, not a new live
	// edit. Seed the cleaned pending state so the cleanup cannot wake the agent
	// a second time after the handoff that caused it.
	s.SeedNotify()

	if testHookAfterInstructionsCleared != nil {
		testHookAfterInstructionsCleared()
	}
	round := s.requestCutIntent(&cutIntent{reason: reason, instruction: said, asks: asks})
	// MARKED ONLY IF THE ROUND WAS ACTUALLY CUT — see reviewerInstruction. A
	// projection that never reached the cut has recorded nothing, and marking
	// here anyway would take the reviewer's words out of the next round too.
	if round != 0 {
		for i := range instructions {
			instructions[i].Round = round
		}
		s.rememberAll(instructions)
		markSaid()
	}
	fp, _, _ := s.waitFingerprint()
	s.debugRoundSent(reason, round, fp, asks)

	// THE UNSENT ROUND IS EMPTIED LAST. The marks were cleared above and the
	// round was recorded inside project (a version is cut nowhere else, from
	// the bytes project wrote, so the marks are necessarily gone by then), and
	// only now does pending.json let go of what was sent. A crash anywhere
	// before this line leaves every sent comment still in pending.json, shown
	// as unplaced; a crash after it finds the round recorded. Never neither.
	// That holds against every other writer of pending.json in between because
	// s.sending, not the review map, is what carries the sent comments until
	// here: a comment filed mid-send, or a save from inside project, writes
	// them too.
	//
	// Rewritten from the review map rather than emptied: a comment filed
	// between the capture and the clear was not sent and is still in the map.
	// A version that could not be written (round == 0) does not hold this
	// back: such a round is still sent, by the rule that a failed version
	// never fails a round.
	if testHookBeforeUnsentCleared != nil {
		testHookBeforeUnsentCleared()
	}
	s.mu.Lock()
	s.releaseSendingLocked(keys)
	err = s.saveUnsentLocked()
	s.mu.Unlock()
	if err != nil && s.Log != nil {
		s.Log("could not empty the sent comments out of the unsent round: " + err.Error())
	}
	return reviewerRound{pending: handoff, round: round, fingerprint: fp}, http.StatusOK, nil
}

// openResponseWindow says which reviewer round the next agent return answers.
// A Revise press and a live settle both send work, so both open this same
// window. approveOnAnswer is the one intentional trigger-level difference:
// only the explicit Revise & Approve choice can set it.
func (s *EditServer) openResponseWindow(round int, fp string, approveOnAnswer bool) {
	s.reviseMu.Lock()
	s.watch = &reviseWatch{at: time.Now(), fingerprint: fp, round: round}
	s.approveOnAnswer = approveOnAnswer
	s.ackState, s.ackNote = "", ""
	s.reviseMu.Unlock()
	// The window IS the handoff: from here until the agent's return the .md
	// file is the agent's editing surface. See handoff.go.
	s.openHandoff(round, fp, approveOnAnswer)
}

// handleRevise starts the configured command and returns immediately. See
// EditServer.OnRevise for why it is neither debounced nor awaited.
//
// Single-flight: a second POST while one command is still running is refused
// with 409 rather than starting a rival process. Two agents revising the same
// document concurrently would race each other's edits into the same
// fragment, and an impatient double-click on the Revise button is the ordinary
// way that happens.
//
// DELIBERATELY NO TIMEOUT. An agent revision legitimately runs for minutes —
// reading the work order, thinking, editing the file — and any timeout short
// enough to be useful against a genuinely hung command would also kill real
// work mid-revision, leaving the document half-revised with no way to tell
// which half. Supervision is the
// caller's: ReviseInFlight exposes the state, Log carries the outcome, and the
// operator can see the process. If a bound is ever wanted it belongs in the
// configured command itself (`timeout 600 …`), where the person who knows how
// long their agent should take can set it.
func (s *EditServer) handleRevise(w http.ResponseWriter, r *http.Request) {
	// GET is the button asking how long it has been (R10) — a plain read, open
	// like every other read here, and answered before the guard because the
	// guard's whole job is to refuse everything that is not a POST from this
	// page or a local tool.
	if r.Method == http.MethodGet {
		s.writeReviseState(w)
		return
	}
	// The same guard every other mutating endpoint runs. This one takes no
	// body, so it does not go through decode — which is exactly how it came to
	// have only a method check, and how a cross-origin form post was able to
	// make this server run its configured command through `sh -c`.
	if !guard(w, r) {
		return
	}
	// A sealed review has no next round to ask for and no verdict left to
	// render. Refused here rather than after the body is read, because every
	// branch below this point is a mutation of a review that has ended.
	if s.refuseSealed(w, verbReview) {
		return
	}
	// This endpoint has never taken a body before now — a plain press is
	// still a bodyless (or `{}`) POST, from the browser's `postJSON` and from
	// `galley revise` alike — so the read is lenient by construction. A
	// malformed or absent body is a plain revise, not an error, and only a
	// verdict this handler actually recognises changes what happens below.
	var req struct {
		Verdict         string `json:"verdict"`
		ApproveOnAnswer bool   `json:"approveOnAnswer"`
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if len(bytes.TrimSpace(raw)) > 0 {
			_ = json.Unmarshal(raw, &req) // a malformed body is a plain revise, not an error
		}
	}
	// Read here, not in the goroutine: these are set once by the CLI before
	// serving starts, and copying them keeps the async run from reading fields
	// a later caller might be writing.
	cmd, logf := s.OnRevise, s.Log
	// THE GUARD IS THE ASK'S, AND ONLY THE ASK'S. It used to front this whole
	// handler on the reasoning that "an approve nobody hears is equally
	// unheard", and that sentence is the part that was wrong. A revise is HANDED
	// TO somebody: it asks for another pass, and with neither a command nor a
	// reader there is nobody to do one, so saying so is better than a button
	// that silently does nothing. A verdict is handed to nobody — it ENDS the
	// review. It seals, it records, and every later poll of this server reads
	// that ending off the seal (see handleWait), so an approve pressed with
	// nothing attached is heard by whoever attaches next, forever.
	//
	// Two things said it was in the wrong place before this. `handleDiscard`
	// — the other ending — never had the guard at all, so one document had two
	// answers for the same state. And every seal test in this package sets
	// `s.OnRevise = "true"` for no reason but to get past it: a workaround
	// written that many times is a design being told something.
	//
	// See below for the ask's own version, which also waits out the re-arm gap
	// before it believes `Waiting() == 0`.
	if req.Verdict == "approve" {
		s.approveReview(true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// A PLAIN ASK MID-WINDOW IS DELIBERATELY NOT REFUSED. A second press is a
	// second ask the blocked reader must hear — the single-flight comment
	// below has said so since `galley wait` existed, and it is the recovery
	// path when a woken agent dies. Under the handoff it is also harmless:
	// the locked browser can have written nothing new, so the press re-sends
	// the same work and re-opens the same window.

	// THE ASK'S OWN GUARD, and it waits before it refuses. Pull means the
	// session that is already here is blocked on GET /_galley/wait, and
	// releasing it is the entire job of the button — no shell hook, no message
	// bus. Only with neither a command NOR a reader is there genuinely nothing
	// to hand the revision to.
	//
	// `Waiting() == 0` IS NOT "NOBODY IS ATTACHED". A reader that was just
	// woken is unregistered for as long as its answer takes to write and reach
	// it, and every client of this endpoint re-arms in that instant — so a
	// press landing there was refused, naming a flag, while a channel was
	// plainly attached and about to poll again. Measured on the tracked build:
	// 191 of 400 back-to-back presses refused, sub-millisecond each (103 in
	// 1500 in the field, where the channel's emit slows the presses down).
	// awaitReArm waits that window out, and waits it out ONLY when a reader was
	// released rather than lost.
	//
	// The press is otherwise gone for good, which is why this is closed rather
	// than merely re-worded: a press moves no fingerprint, so `--since` cannot
	// catch it up on the next poll, and the reviewer watches `revising · Ns`
	// count up against an ask nobody has.
	if cmd == "" && !s.awaitReArm() {
		http.Error(w, "this server was started without --on-revise and nothing is blocked on `galley wait`, "+
			"so there is nothing to hand the revision to",
			http.StatusNotImplemented)
		return
	}

	// The button chooses WHEN to send. Everything after that choice is the same
	// handoff live mode performs: capture, clean, cut, and open one response
	// window pointing at the round the agent is answering.
	sent, code, err := s.sendReviewerRound(versions.ReasonRevise)
	if err != nil {
		writeMutationError(w, code, err)
		return
	}

	// REVISE & APPROVE WITH NOTHING TO REVISE APPROVES NOW. The trust exit sends
	// the round and then approves — but the "then approve" half rides on the
	// agent's ack of a SUCCESSFUL answer (autoApprove in handleAck), and a round
	// with no instructions gives the agent nothing to apply, so it can never
	// produce that ack. The approve was therefore dropped and the review hung
	// open forever (no verdict round, no approve event reached the channel).
	// With no instruction to hand over there is nothing to wait on, so the
	// verdict lands here: the revise round is already cut above (the reviewer's
	// own edits were handed off), and approveReview seals it, writes the verdict
	// round, and wakes the attached session. A round that DOES carry
	// instructions still defers — the agent revises them first, then its ack
	// approves — which is the trust exit exactly as it has always worked.
	if req.ApproveOnAnswer && len(sent.pending.Instructions) == 0 {
		s.approveReview(true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	done := make(chan struct{})
	// Single-flight guards the COMMAND PROCESS, and only that. With no command
	// there is no rival process to refuse, and a second press is a second ask
	// the blocked reader must hear — the hand-rolled marker-file version of
	// this was one-shot, and losing the reviewer's second press is exactly the
	// bug that made `galley wait` worth building.
	if cmd != "" {
		s.reviseMu.Lock()
		if s.reviseDone != nil {
			s.reviseMu.Unlock()
			http.Error(w, "a revision is already in flight — wait for it to finish before requesting another",
				http.StatusConflict)
			return
		}
		s.reviseDone = done
		s.reviseMu.Unlock()
	}
	// The window the button counts against opens HERE and is deliberately not
	// closed by the deferred cleanup below: the command exiting is not the
	// revision landing. See reviseWatch. One opener for the press and the live
	// settle alike — it resets the ack (a fresh ask must not wear the last
	// review's answer) and opens the file handoff with the window.
	s.openResponseWindow(sent.round, sent.fingerprint, req.ApproveOnAnswer)

	// THE VERDICT THAT DOES NOT END THE REVIEW, and the count that says how
	// many rounds a document took. The trail is deliberately NOT swept up here:
	// revise leaves it standing, because the review is still on and the trail
	// is part of its record — the settle rule is what records those entries.
	s.remember(verdictRecord(ledger.KindVerdictRevise, ""))

	// AFTER reviseMu is released. The waiter registry has its own lock and
	// nothing here needs the two held together; taking them in one order here
	// and another anywhere else is how this file would acquire its first
	// deadlock.
	s.wakeWaiters(waitRevise, sent.fingerprint, &sent.pending)

	if cmd != "" {
		go func() {
			defer func() {
				s.reviseMu.Lock()
				s.reviseDone = nil
				s.reviseMu.Unlock()
				// After clearing the flag, so anything woken by this channel
				// sees ReviseInFlight report false rather than racing it.
				close(done)
			}()
			runRevise(cmd, logf)
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}

// approveReview seals the current document and publishes the ending to every
// carrier. clearAck is true for a direct Approve press; an automatic approval
// keeps the successful answer visible until the terminal state replaces it.
func (s *EditServer) approveReview(clearAck bool) {
	fp, _, _ := s.waitFingerprint()
	s.reviseMu.Lock()
	s.watch = nil
	s.approveOnAnswer = false
	if clearAck {
		s.ackState, s.ackNote = "", ""
	}
	s.reviseMu.Unlock()
	// A verdict mid-window ends the window: the file is the reviewer's again,
	// and the cut below is the projection that writes the canonical document
	// back over whatever draft the agent had. Closed BEFORE the cut for
	// exactly that reason.
	s.closeHandoff()
	s.seal(VerdictApproved, 0)
	s.remember(verdictRecord(ledger.KindVerdictApproved, ""))
	s.requestCut(versions.ReasonVerdict, "", 0)
	s.wakeWaiters(waitApprove, fp, nil)
	if s.OnApprove != nil {
		s.OnApprove()
	}
	if s.Log != nil {
		s.Log("approved")
	}
}

// writeReviseState answers the button's question: am I still waiting, is a
// command actually running, and how long has it been?
//
// Two flags rather than one, because they are two different facts and the
// button needs both. `running` is a command PROCESS executing, which is the
// only thing the server would refuse a second ask for. `waiting` is the
// reviewer still expecting something to land. A notification-style --on-revise
// makes them differ within milliseconds: running goes false at once, waiting
// stays true until the agent writes something — and a Revise button that is
// still counting but clickable is the honest rendering of "you asked, nothing
// has come back, ask again if you like".
func (s *EditServer) writeReviseState(w http.ResponseWriter) {
	_, running := s.ReviseInFlight()
	at, waiting := s.ReviseWatch()
	var since int64
	if waiting {
		since = time.Since(at).Milliseconds()
	}
	// The ack lives under reviseMu — same lock as the watch, for the reason
	// given on the fields themselves — so it is read here rather than through
	// a second exported accessor nobody else needs.
	s.reviseMu.Lock()
	ack, ackNote, ackAt := s.ackState, s.ackNote, s.ackAt
	cannot, cannotAt := s.cannotWhy, s.cannotAt
	s.reviseMu.Unlock()
	var ackAgo int64
	if ack != "" {
		ackAgo = time.Since(ackAt).Milliseconds()
	}
	var cannotAgo int64
	if cannot != "" {
		cannotAgo = time.Since(cannotAt).Milliseconds()
	}
	// The seal rides this state rather than the pending view, because it is
	// the same question the button already asks every poll — "what should I
	// be showing?" — and the terminal bar is that answer's last branch. The
	// pending view is the WORK; the seal is whether there is any left to do.
	st := s.sealState()
	var verdictAt int64
	if !st.VerdictAt.IsZero() {
		verdictAt = st.VerdictAt.UnixMilli()
	}
	writeJSON(w, ReviseStateView{
		Running:  running,
		Waiting:  waiting,
		SinceMs:  since,
		Ack:      ack,
		AckNote:  ackNote,
		AckAgoMs: ackAgo,
		// THE ROUND THAT ARRIVED, and it rides here rather than on the pending
		// view because the pending view is the WORK and phase 2's answer is not
		// work — it is already in the document. A number, so the page can tell
		// this arrival from the last one without the server tracking readers.
		Landed: int(s.lastLanded.Load()),
		// THE EXCEPTION, in the agent's words. Separate from `ack` deliberately:
		// `failed` is a status about a press and this is a REPORT about an
		// instruction, and the reviewer's answer to it is a different
		// instruction rather than a retry.
		Cannot:      cannot,
		CannotAgoMs: cannotAgo,
		// THE HANDOFF LOCK rides this state for the seal's reason: it is the
		// same "what should I be showing?" the page already asks every poll.
		// draftError is the one sentence about a save that will not import.
		Handoff:    s.handoffOpenNow(),
		DraftError: s.draftError(),
		Sealed:     st.Sealed,
		Verdict:    st.Verdict,
		VerdictAt:  verdictAt,
	})
}

// ReviseStateView is what GET /_galley/revise answers, and it is a STRUCT
// because it is a CONTRACT.
//
// It was a `map[string]any` written inline in writeReviseState — thirteen keys
// the browser polls every second, none of them a type either side could be held
// to. That is the shape the wire contract exists to end: `pending.suggestions`
// was renamed to `pending.instructions`, two browser gates read the old field,
// and nobody noticed for weeks. A map cannot be generated into web/wire.d.ts,
// so a rename here could not have failed `just verify` — it could only have
// failed in a browser, silently, as `undefined`.
//
// Court, on the general rule: "We should have everything typed and defined up
// front. Especially when going between go and typescript. We should go further
// if possible and make this a contract neither can break without the other."
//
// The comments that were on the map's keys are on the fields, unchanged: they
// say WHY each thing rides this state rather than the pending view, and that
// reasoning is the same whether the payload is a map or a type.
type ReviseStateView struct {
	Running  bool   `json:"running"`
	Waiting  bool   `json:"waiting"`
	SinceMs  int64  `json:"sinceMs"`
	Ack      string `json:"ack"`
	AckNote  string `json:"ackNote"`
	AckAgoMs int64  `json:"ackAgoMs"`
	// Landed is THE ROUND THAT ARRIVED, and it rides here rather than on the
	// pending view because the pending view is the WORK and the agent's answer
	// is not work — it is already in the document. A number, so the page can
	// tell this arrival from the last one without the server tracking readers.
	Landed int `json:"landed"`
	// Cannot is THE EXCEPTION, in the agent's words. Separate from Ack
	// deliberately: `failed` is a status about a press and this is a REPORT
	// about an instruction, and the reviewer's answer to it is a different
	// instruction rather than a retry.
	Cannot      string `json:"cannot"`
	CannotAgoMs int64  `json:"cannotAgoMs"`
	// Handoff is THE HANDOFF LOCK, riding this state for the seal's reason: it
	// is the same "what should I be showing?" the page already asks every poll.
	// DraftError is the one sentence about a save that will not import.
	Handoff    bool   `json:"handoff"`
	DraftError string `json:"draftError"`
	// Sealed, Verdict and VerdictAt are the review's ending. The pending view
	// is the WORK; the seal is whether there is any left to do.
	Sealed    bool   `json:"sealed"`
	Verdict   string `json:"verdict"`
	VerdictAt int64  `json:"verdictAt"`
}

// ackStates is the five things an agent can say about a review, and the only
// two questions they answer: did anyone hear, and is it still worth waiting.
// received and working keep the window open; answered, declined and failed
// close it with a stated outcome — the half of Gap 1 a status path can close.
// The timeout for "no ack ever came" is deliberately NOT here; see the
// design spec.
var ackStates = map[string]bool{
	"received": true, "working": true, "answered": true, "declined": true, "failed": true,
}

// handleAck is the status return path: an agent's answer to a revision
// window opened by handleRevise. It never blocks on anything the way a
// revise command might — this is a narration of state, not a command launch
// — so it is safe to hold reviseMu across the whole write.
func (s *EditServer) handleAck(w http.ResponseWriter, r *http.Request) {
	if !guard(w, r) {
		return
	}
	var req struct {
		State string `json:"state"`
		Note  string `json:"note"`
		// Changes is the agent's OPTIONAL testimony about what it wrote. It
		// annotates the change list; it never declares one. See manifest.go.
		Changes []ackChange `json:"changes"`
	}
	if !decode(w, r, &req) {
		return
	}
	// AS RECEIVED, AND BEFORE EVERY REFUSAL BELOW. An ack this endpoint rejects
	// is the one nothing else in galley records: the agent's own account of it
	// lives in a transcript belonging to another project. See debug.go.
	s.debugAck(s.watchRound(), req.State, req.Note, req.Changes)
	if !ackStates[req.State] {
		http.Error(w, "state must be one of received, working, answered, declined, failed; got "+strconv.Quote(req.State),
			http.StatusBadRequest)
		return
	}
	// AN ACK IS A CLAIM ABOUT A QUESTION, AND HERE THERE IS NONE. Every state
	// this endpoint takes answers something: `working` and `received` say a
	// press was heard, `answered`, `declined` and `failed` say how it came out.
	// With no revision window open and no verdict rendered, nothing has been
	// asked of anybody — so the ack is not a status, it is a sentence about a
	// conversation that never started, and it used to be 204 with the
	// reviewer's own bar rendering the agent's word beside a press nobody made.
	//
	// A SEALED REVIEW IS STILL A QUESTION, and that is the whole of the
	// exception. The verdict is the last thing said in a round the agent was
	// part of — the trust exit asks for work outright — so `working`, `failed`
	// and `declined` stay sayable after it, per the rule below, and `answered`
	// is judged on its own terms there.
	//
	// A TERMINAL ACK CLOSES THE WINDOW IT ANSWERS, so a repeat of one lands
	// here on the next press-less state and is refused by this same rule
	// rather than by a second one counting acks.
	if _, asked := s.ReviseWatch(); !asked && !s.Sealed() {
		http.Error(w, "nothing has been asked of you on this document — there is no revision window open and "+
			"no verdict has been rendered, so there is nothing to be working on and nothing to have answered. "+
			"An ack answers a press; wait for one",
			http.StatusConflict)
		return
	}
	// AND OF THE FIVE, `answered` IS THE ONE THE SERVER CAN KNOW IS FALSE EVEN
	// WHEN A QUESTION WAS PUT. It says the response is IN THE DOCUMENT — so
	// when nothing could
	// have been put there, it is not a status, it is a fabrication, and it used
	// to be answered 204. That is how a trust verdict came out of a session
	// reading "answered" over an agent that had been refused every verb it
	// tried: the reviewer's own page told them the work was done.
	//
	// Two false cases, and they are the only two:
	//
	//   - the entrusted handoff is still outstanding — the work this ack claims
	//     is measurably not in the document;
	//   - the review ended with nothing entrusted — nothing was asked, so
	//     nothing can have been answered.
	//
	// EVERY OTHER STATE STAYS OPEN EVERYWHERE, deliberately. `working`,
	// `failed` and `declined` are true things to say about a review that has
	// ended — an agent that cannot do the entrusted work MUST be able to say
	// so, and the sealed page is where the reviewer reads it. Refusing those
	// would push the honest agent toward the one word that is still accepted,
	// which is the opposite of the point.
	// A stated outcome ends the count: the reviewer no longer has anything to
	// wait FOR once the agent has said how the review came out. Leaving the
	// window open past that point is Gap 1 again — a counter that never
	// stops, this time dressed up in a status the reviewer already has.
	terminal := req.State == "answered" || req.State == "declined" || req.State == "failed"
	// THE AGENT'S RETURN IS A CLAIM ABOUT THE FILE, and with a handoff window
	// open the file is checkable. Refusals happen BEFORE the window is
	// touched: a refused ack leaves the window open and the draft held.
	// `declined` and `failed` still import best-effort — partial work is
	// still work — but they are honest states about a round that did not
	// come out, so nothing about the file can make them false.
	if terminal && s.handoffOpenNow() {
		imported, ierr := s.importDraft()
		if req.State == "answered" {
			if ierr != nil {
				http.Error(w, fmt.Sprintf("the draft in %s cannot be imported: %v — fix the file and ack again",
					filepath.Base(s.MdPath), ierr), http.StatusConflict)
				return
			}
			// A STRUCTURAL ANSWER IS AN ANSWER, AND IT IS NOT IN content.md.
			// Both facts above are about the markdown, and a page review's agent
			// answers "remove this section" by editing page.html — the layer the
			// agent prompt sends it to for exactly this — which moves the document
			// not at all. Read as nothing-happened, that refused the ack for the
			// round the live loop was built for and left the window open, so the
			// reviewer's editor stayed read-only for good. A drifted page is the
			// work, measurably on disk. Markdown mode has no page and no renderer,
			// so its gate is unchanged to the byte.
			if !imported && !s.appliedOpen() && !s.pageDrifted() {
				http.Error(w, "nothing has changed since the round was handed over — if the instruction cannot be "+
					"done, report it with `galley cannot` instead of answering", http.StatusConflict)
				return
			}
		}
		// The ack note is the agent's one sentence about the whole revision —
		// the round's description, exactly as an apply's --note used to be.
		if req.Note != "" && s.appliedOpen() {
			s.openApplied(req.Note)
		}
		// AND THE MANIFEST IS THE SENTENCE PER CHANGE. Validated here, while
		// the window is still open and the round it answers is still nameable,
		// and filed on the accumulator the cut below commits. An entry the
		// server cannot prove is dropped and the ack still succeeds — the 204
		// is not contingent on the agent's bookkeeping.
		s.setAppliedChanges(s.ackManifest(req.Changes))
	}
	s.reviseMu.Lock()
	s.ackState, s.ackNote, s.ackAt = req.State, req.Note, time.Now()
	autoApprove := terminal && req.State == "answered" && s.approveOnAnswer
	if terminal {
		s.watch = nil
		s.approveOnAnswer = false
	}
	s.reviseMu.Unlock()
	// AND A TERMINAL ACK IS THE AGENT'S SEND. Phase 2's revision lands in the
	// document over several edits, so there is no single write that is "the
	// answer arriving" the way one proposal was — the answer arrives when the
	// agent says it has, which is the sentence this endpoint already carries in
	// both carriers' own words ("answered" once your response is in the
	// document). Commit on send, with the agent's send being its return.
	//
	// After the window is closed, so `answering` cannot re-read a watch this ack
	// has just retired. A round with nothing applied is a no-op here: cutApplied
	// returns without touching the store.
	if terminal {
		// The window closes BEFORE the cut, so the cut's projection is the
		// write that puts the canonical document back on disk — ownership
		// returns to the live document in the same act that records the round.
		s.closeHandoff()
		s.cutApplied()
		// And a page review's round can close with the whole answer on the OTHER
		// file, which no projection would carry: see pageBoundary. No-op in
		// markdown mode.
		s.pageBoundary()
	}
	if autoApprove {
		s.approveReview(false)
	}
	if s.Log != nil {
		s.Log("ack: " + req.State + noteSuffix(req.Note))
	}
	w.WriteHeader(http.StatusNoContent)
}

// noteSuffix formats an ack's optional note for the log line, so a bare
// "ack: working" and "ack: working — reading the comment thread" both read
// naturally rather than the latter carrying an empty trailing dash.
func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return " — " + note
}

func runRevise(cmd string, logf func(string)) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if logf == nil {
		return
	}
	if err != nil {
		logf(fmt.Sprintf("revise failed: %v: %s", err, bytes.TrimSpace(out)))
		return
	}
	logf("revision requested")
}

// --- the blocking read: GET /_galley/wait ---
//
// galley's other verbs PUSH: --on-revise and --on-settle wake someone who is
// not here. This is the PULL half — the session already holding the branch,
// the discussion and the hours of context blocks on the document and is woken
// when the reviewer asks. See docs/superpowers/specs/2026-08-09-the-agent-hook.md.
//
// Nothing here decides whether the document has moved. Both release paths ride
// a decision that already exists: the notifier's fire (which carries mutate's
// self-wake seed) for a settle, and handleRevise's press for a revise. A
// second implementation of "has anything changed" would work, pass, and drift
// the first time either side was touched.

// The eight things a poll can report. revise, settle, approve, discard,
// reopen and changed are wakes; the other two are how the caller learns to
// re-arm or to stop. Each terminal ending carries its own reason so a waiter
// (and the CLI's exit path) can tell it apart from an ordinary ask for another
// pass — and reopen is the one wake that says the review is LIVE again, which
// is why a channel returns on approve and discard but keeps polling through
// this one.
//
// `changed` IS THE HONEST ONE, AND IT IS THE CATCH-UP'S. Every other reason
// names an EVENT the server watched happen; the catch-up watched nothing — it
// found the caller's cursor behind and the document moved, which says THAT
// something happened and never WHAT. It answered `settle` unconditionally, and
// `settle` is a specific claim: the document settled and the live-mode
// notifier fired. On a session `on ask` — the default — nothing settles a
// waiter at all, so the word named an event that could not have occurred; and
// an approve or a discard missed in the same gap arrived under it too, sending
// an agent to answer a review that had ended. A terminal ending is replayed
// from the seal, because the server still holds it and it is still TRUE; for
// everything else `changed` says the whole of what is known — re-read
// everything.
const (
	waitRevise  = "revise"
	waitSettle  = "settle"
	waitApprove = "approve"
	waitChanged = "changed"
	waitTimeout = "timeout"
	waitClosedR = "closed"
)

// endingReason maps a sealed review's verdict onto the wake that announces it,
// so the catch-up and the presses that fired the original wakes cannot drift
// into naming one ending two ways. The trust exit is an approve — the count of
// what it entrusted rides the pending view, exactly as it does on the live
// wake.
func endingReason(verdict string) (string, bool) {
	if verdict == VerdictApproved {
		return waitApprove, true
	}
	return "", false
}

// waitEvent is one release, handed to a blocked reader.
type waitEvent struct {
	reason  string
	fp      string
	pending *PendingView
}

// waitReply is GET /_galley/wait's JSON body.
type waitReply struct {
	Reason      string       `json:"reason"`
	Fingerprint string       `json:"fingerprint"`
	Pending     *PendingView `json:"pending"`
}

// How long ONE poll is held open. The caller names its own bound with
// ?timeout=; these are the default and the ceiling.
//
// A ceiling at all, rather than "hold it until something happens": a long-poll
// that never returns is indistinguishable from a wedged one, and an expiry
// that reports the CURRENT fingerprint is also how a `galley wait` with no
// deadline renews its cursor between polls.
const (
	defaultWaitPoll = 5 * time.Minute
	maxWaitPoll     = time.Hour
)

// registerWaiter adds a reader to the registry, reporting false when the
// server is already shutting down.
func (s *EditServer) registerWaiter() (chan waitEvent, bool) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	if s.waitClosed {
		return nil, false
	}
	if s.waiters == nil {
		s.waiters = make(map[chan waitEvent]struct{})
	}
	// Buffered by one, and every send is a select/default: a release must
	// never block on a reader, whether there are none, one, or fifty. The
	// buffer is what makes a release landing microseconds before the handler
	// reaches its select still arrive rather than be dropped on the floor.
	ch := make(chan waitEvent, 1)
	s.waiters[ch] = struct{}{}
	s.debugWaiter(debug.KindAttached, len(s.waiters), "a blocking read registered")
	return ch, true
}

func (s *EditServer) unregisterWaiter(ch chan waitEvent) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	delete(s.waiters, ch)
	s.debugWaiter(debug.KindDetached, len(s.waiters), "a blocking read ended")
}

// releasedWaiter is unregisterWaiter for the exit that promises a return: a
// reader WOKEN by an event re-arms at once, so the moment it leaves is the
// moment the re-arm gap opens. One lock, one stamp — see waitWoke. `rearming`
// is false for the shutdown release, whose readers are leaving for good.
func (s *EditServer) releasedWaiter(ch chan waitEvent, rearming bool) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	delete(s.waiters, ch)
	if rearming {
		s.waitWoke = time.Now()
	}
	// THE RE-ARM GAP IS THE WHOLE REASON THIS IS AN EVENT AND NOT A SAMPLE.
	// A reader woken by an event re-arms at once, so Waiting() reads zero for a
	// few milliseconds while an agent is very much attached — a single sample
	// cannot tell that from nobody listening, and a stream of arrivals and
	// departures can.
	note := "a blocking read was released for good"
	if rearming {
		note = "a blocking read was woken and is re-arming"
	}
	s.debugWaiter(debug.KindDetached, len(s.waiters), note)
}

// Waiting reports how many blocking reads are registered right now — the
// question handleRevise asks before refusing a press for want of a command,
// and the one a startup or status line asks to say whether anyone is listening.
//
// A STRAIGHT READ, NOT A RESERVATION. A waiter can arrive or leave between this
// call and whatever the caller does with the answer, and nothing here pretends
// otherwise: a caller acting on it should report what it SAW, not promise what
// will happen next. Making it a reservation would mean holding waitMu across
// the caller's own work, which is exactly what this file's lock discipline
// forbids — the registry lock is taken alone, always.
//
// ONLY LIVE READERS COUNT. A poll that expired, that was released, or whose
// client hung up removes itself in handleWait's defer, so an entry here means a
// request that is still open. A stale entry would tell a reviewer their press
// landed when nobody was listening, which is the failure this whole endpoint
// exists to remove, wearing a different hat.
func (s *EditServer) Waiting() int {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return len(s.waiters)
}

// How long a press will wait for a reader that is re-arming, and how recently
// that reader must have been released for it to be worth waiting at all.
//
// Both are bounds on a window measured in single-digit milliseconds — the
// channel's own header records it. Measured against the tracked build through
// a real long-poll that re-arms with no work between presses: 191 of 400
// back-to-back presses refused, each in that window (a real channel emits a
// notification too, which is why the reported field rate is lower — 103 in
// 1500). They are generous against those numbers rather than tight against
// them: the cost of being generous is a reviewer's button taking a quarter of
// a second in the rare case, and the cost of being tight is the press it
// exists to save.
const (
	reArmWait  = 250 * time.Millisecond
	reArmFresh = 2 * time.Second
)

// awaitReArm gives a reader that was just woken the chance to register its
// next poll before a press is refused for want of one, and reports whether one
// is registered when it returns.
//
// THE WINDOW IS REAL AND IT IS NOT CLOSEABLE FROM THE READER'S SIDE.
// handleWait deregisters BEFORE it writes its response — deliberately, because
// the alternative is a press told it landed into a handler that has already
// left its select — so between a wake and the next poll there are a JSON write
// and a network round trip during which `Waiting()` is honestly 0. The reader
// cannot close it: it has one connection and it is using it to carry the
// answer. The server can, by waiting, because it knows the difference between
// "nobody is there" and "somebody was released a moment ago and every client
// this endpoint has re-arms at once".
//
// A POLL RATHER THAN A CONDITION VARIABLE. The wait is bounded, rare, and on
// an HTTP handler for a button press; a broadcast would mean a second
// synchronisation object on waitMu whose only job is to be signalled on a path
// (registerWaiter) that runs on every poll of every attached channel.
func (s *EditServer) awaitReArm() bool {
	s.waitMu.Lock()
	n, woke := len(s.waiters), s.waitWoke
	s.waitMu.Unlock()
	if n > 0 {
		return true
	}
	if woke.IsZero() || time.Since(woke) > reArmFresh {
		// Nobody was released recently, so nobody owes this server a poll.
		return false
	}
	deadline := time.Now().Add(reArmWait)
	for time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		if s.Waiting() > 0 {
			return true
		}
	}
	return false
}

// wakeWaiters releases every blocked reader. Never blocks and never spawns:
// a full channel already holds a release the reader has not read yet. A Revise
// carries the captured round because clearing the browser's pending state is
// part of the same handoff; re-reading would erase the message being sent.
func (s *EditServer) wakeWaiters(reason, fp string, pending *PendingView) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	for ch := range s.waiters {
		select {
		case ch <- waitEvent{reason: reason, fp: fp, pending: pending}:
		default:
		}
	}
}

// wakeSettle is the notifier's hook, so a settle releases readers on exactly
// the decision that would have run a --on-settle command — self-wake guard and
// all. See Notifier.SetWake.
//
// AND IT IS WHERE A LIVE ROUND IS CUT, because this is the moment the document
// is actually SENT. Live mode has no press, so the spec's "a version is cut per
// exchange" needs something that knows an exchange happened, and the notifier
// already holds that one decision: it fires when the pending set has moved
// since the agent last saw it, and declines when it has not — including the
// self-wake guard mutate seeds, so the agent cannot wake itself and cannot
// commit a round for having answered.
//
// HANGING OFF THE DECISION RATHER THAN RE-MAKING IT is this hook's own rule,
// and it is why the version cut used to be wrong: cutIfSending asked "is this
// live and did the digest move", which is a save, not a send. A reviewer typing
// prose moves the digest every 400ms export debounce and moves no fingerprint
// at all, so it committed a full copy of the document per keystroke burst while
// waking nobody. Two conditions for one question is exactly the second
// implementation SetWake exists to refuse.
//
// THE VERSION FIRST, THEN THE READERS. A released `galley wait` is an agent
// about to read the document, and the round it is reading must already be on
// disk when it does. requestCut projects synchronously, so the cut is against
// bytes that have settled — the notifier's quiet window is precisely the
// promise that nothing has moved since.
func (s *EditServer) wakeSettle(fp string) {
	// Live chooses WHEN to send; it does not define a second kind of handoff.
	// Capture and clean through the same path as Revise, open the same response
	// window, and carry the same immutable work order to blocked readers.
	sent, _, err := s.sendReviewerRound(versions.ReasonSettled)
	if err != nil {
		if s.Log != nil {
			s.Log("could not send the settled reviewer round: " + err.Error())
		}
		return
	}
	s.openResponseWindow(sent.round, sent.fingerprint, false)
	// The cursor is the fingerprint that CAUSED this wake. Cleanup deliberately
	// moved the current pending set again; returning that post-clean value can
	// equal the caller's old cursor and make a real wake look like nothing moved.
	s.wakeWaiters(waitSettle, fp, &sent.pending)
}

// ReleaseWaiters ends every blocked GET /_galley/wait and refuses new ones, so
// `galley wait` exits rather than hanging past the editor it was watching.
//
// Idempotent, and called twice on an orderly shutdown on purpose: once by the
// CLI BEFORE http.Server.Shutdown — which waits for in-flight requests, and a
// long-poll is an in-flight request that would otherwise hold shutdown open for
// the whole grace period — and once by Close, which is the only handle a test
// or an embedder has.
func (s *EditServer) ReleaseWaiters() {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	s.waitClosed = true
	for ch := range s.waiters {
		select {
		case ch <- waitEvent{reason: waitClosedR}:
		default:
		}
		delete(s.waiters, ch)
	}
}

// waitFingerprint is the cursor a blocking read compares against: the document
// and its live instructions, hashed by editFingerprint — the same
// one answer to "has anything a reviewer could be waiting on changed" that the
// notifier and the revision window both use. handleRevise takes the window's
// fingerprint from here too, so there is exactly one of these in the file.
//
// UNDER mu, unlike pending() — and the difference is which reader can afford
// it. review.Read walks the review map, and ygo's YArray.Len/Get take no lock
// at all (the same hole YXmlFragment.Children has, one type over), so this
// read raced a server-side thread delete under -race: measured through
// handleWait against sendReviewerRound's instruction cleanup, reproducibly
// (~1 in 25) all the way back to before the handoff work. Every server-side
// writer of that map holds mu, so mu closes it. This caller is a long poll —
// a blocked reader that can wait out a projection — where pending() is the
// browser's hot path and keeps its documented lock-free tradeoff; the
// review-map analogue of ydoc.ReadLive is the full fix and is still owed.
// Callers hold NEITHER mu nor reviseMu (handleWait, sendReviewerRound after
// requestCut returns, approveReview before it takes reviseMu).
func (s *EditServer) waitFingerprint() (string, PendingView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	model, err := s.readLive()
	if err != nil {
		return "", PendingView{}, err
	}
	view := PendingView{Instructions: s.liveInstructions(model)}
	// WHAT THE REVIEWER CHANGED BY HAND rides the same payload as what they
	// wrote about it. Read here rather than at the press because this is where
	// the round is composed for handover, and because a version that cannot be
	// read returns nothing rather than failing the round.
	view.Changes, view.ChangesDropped = ReviewerChanges(s.Versions())
	if view.Instructions == nil {
		view.Instructions = []InstructionView{}
	}
	return s.editFingerprint(model), view, nil
}

// handleWait holds the request until something the caller is waiting for
// happens, then answers with why, the fingerprint to re-arm on, and everything
// pending.
//
// GET, and open like every other read here: guard() is POST-only and exists to
// stop a cross-origin page making this server WRITE. A blocking read changes
// nothing.
func (s *EditServer) handleWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed — this endpoint takes GET", http.StatusMethodNotAllowed)
		return
	}
	poll := defaultWaitPoll
	if raw := r.URL.Query().Get("timeout"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			http.Error(w, "timeout must be a positive Go duration such as 30s; got "+strconv.Quote(raw),
				http.StatusBadRequest)
			return
		}
		poll = min(d, maxWaitPoll)
	}
	since := r.URL.Query().Get("since")

	// REGISTER BEFORE READING. The other order drops a release that lands
	// between the read and the registration — the re-arm gap --since exists to
	// close, reintroduced one level down where no cursor can see it.
	ch, ok := s.registerWaiter()
	if !ok {
		writeJSON(w, waitReply{Reason: waitClosedR})
		return
	}
	defer s.unregisterWaiter(ch)

	fp, view, err := s.waitFingerprint()
	if err != nil {
		writeBusy(w, err)
		return
	}
	// THE ENDING IS A FACT, NOT AN EVENT, so it is answered whether or not the
	// caller has a cursor and whether or not the fingerprint moved. A review
	// that has ended is over for every reader of it, including one that
	// arrived after the verdict landed and one whose cursor is exactly current
	// — a clean approve moves no fingerprint at all, so the catch-up below
	// could never see the most ordinary ending there is, and the poll simply
	// blocked until it expired and reported `timeout` over a closed review.
	//
	// This is a REPLAY only in the sense that the caller may have missed the
	// live wake; what it asserts is the present tense, read from the seal the
	// verdict wrote. Nothing loops on it: both terminal endings tell their
	// reader to stop, and `galley wait` and the channel both do.
	//
	// Deregister BEFORE the write in this branch and the next, for the reason
	// spelled out on the select's branches below: while this reader is still
	// counted, a Revise press is told it landed and its event goes nowhere.
	if st := s.sealState(); st.Sealed {
		// A seal carrying a verdict this vocabulary does not know is not
		// announced as an ending — inventing a word is the defect this branch
		// exists to remove — and falls through to the ordinary paths below.
		if reason, ended := endingReason(st.Verdict); ended {
			s.unregisterWaiter(ch)
			writeJSON(w, waitReply{Reason: reason, Fingerprint: fp, Pending: &view})
			return
		}
	}
	// The catch-up. The caller's cursor is behind, so what it is waiting for
	// has already happened and will not happen again — answer now rather than
	// blocking for an event that is in the past.
	//
	// `changed` AND NOT `settle`: see the reason vocabulary above. What is
	// known here is that the document moved past the caller's cursor, and the
	// reason it moved was never recorded — the reasons this endpoint reports
	// all ride a decision that happened elsewhere, and a decision that has
	// already been taken has nothing left in the server to be read back out of
	// it. The one exception is the ending, and it is answered above.
	if since != "" && since != fp {
		s.unregisterWaiter(ch)
		writeJSON(w, waitReply{Reason: waitChanged, Fingerprint: fp, Pending: &view})
		return
	}

	timer := time.NewTimer(poll)
	defer timer.Stop()

	// DEREGISTER FIRST IN EVERY BRANCH BELOW, before doing any work.
	//
	// The deferred unregisterWaiter runs after a branch's BODY, and these
	// bodies are not cheap: a full waitFingerprint (a ReadLive over the whole
	// document) plus a JSON write. Throughout that window this reader is still
	// in the registry, so Waiting() counts it, handleRevise answers the
	// reviewer 204, and wakeWaiters succeeds into the buffer of a handler that
	// has already left the select — the press is dropped while the reviewer is
	// told it landed.
	//
	// --since CANNOT RECOVER THAT ONE, which is what makes it worth narrowing:
	// a press does not move the pending set, so the fingerprint is unchanged
	// and the next poll simply blocks. The reviewer watches `revising · Ns`
	// count up forever — the press-into-a-dead-reader failure this feature
	// exists to remove, arriving silently.
	//
	// Closing it OUTRIGHT needs an acknowledgement the long-poll has no way to
	// send, so this does not pretend to. It narrows the window to a few
	// instructions, and a press landing inside it now gets an honest 501
	// instead of a false 204 — the better of the two failures.
	//
	// The defer stays: unregisterWaiter is a map delete, idempotent, and the
	// r.Context().Done() branch and every early return still need it.
	select {
	case ev := <-ch:
		// RELEASED BY AN EVENT, so this reader owes the server another poll and
		// the gap between the two is the server's to wait out — see awaitReArm.
		// Stamped here and only here: an expiry, a hung-up client and the
		// shutdown release below all promise nothing, and waiting on a reader
		// that is never coming back would make an honest refusal a slow one.
		s.releasedWaiter(ch, ev.reason != waitClosedR)
		if ev.reason == waitClosedR {
			writeJSON(w, waitReply{Reason: waitClosedR, Fingerprint: fp})
			return
		}
		// Revise carries the immutable round captured before its pending marks
		// were cleared. Other wakes still report the latest visible state.
		if ev.pending != nil {
			fp, view = ev.fp, *ev.pending
		} else {
			fp, view, err = s.waitFingerprint()
			if err != nil {
				writeBusy(w, err)
				return
			}
		}
		writeJSON(w, waitReply{Reason: ev.reason, Fingerprint: fp, Pending: &view})
	case <-timer.C:
		s.unregisterWaiter(ch)
		fp, view, err := s.waitFingerprint()
		if err != nil {
			writeBusy(w, err)
			return
		}
		// The CURRENT fingerprint, never the one the caller sent. An expiry is
		// how a wait with no deadline renews its cursor, and handing back a
		// stale one would make the next poll catch up on a change this one
		// already declined — the agent's own write, most often.
		writeJSON(w, waitReply{Reason: waitTimeout, Fingerprint: fp, Pending: &view})
	case <-r.Context().Done():
		// The caller went away. Nothing to write to.
	}
}
