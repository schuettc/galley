// channel is the listener that starts itself — an MCP channel server inside
// the galley binary, spawned by the agent harness at session start, before
// any document exists. It scans the live registry for editors this session
// owns (or that nobody owns, under its root), holds a long-poll on each, and
// forwards every wake into the session as notifications/claude/channel.
//
// It is a CLIENT of GET /_galley/wait — the same endpoint `galley wait`
// speaks — so the wake vocabulary, the --since cursor and the self-wake guard
// are all the server's, implemented once.
//
// RE-ARM BEFORE EMIT. Spike 2 measured a single-digit-millisecond window
// between a wake and the next registration in which Waiting() reads 0 and a
// Revise press is refused as unheard. The attach loop therefore re-arms its
// next poll IMMEDIATELY on wake and emits the notification from a separate
// goroutine — slow stdio never sits between the wake and the re-arm.
//
// THE WINDOW CANNOT BE CLOSED FROM THIS SIDE and no longer has to be: this
// process has one connection per attach and it is using it to carry the answer,
// so the gap is a JSON write and a round trip however fast the emit is. The
// server waits it out instead (serve.awaitReArm) — it is the side that knows
// a reader was RELEASED rather than lost. Narrowing it here still matters,
// because the wait is a bound on the reviewer's button and not a promise.
//
// NOT LICENSE-GATED, deliberately, agent-prompt's reasoning: this process
// lives inside an MCP harness where a gate message on stdout corrupts the
// JSON-RPC stream and presents as a silently dead server. Every mutating verb
// it leads the agent to — pending, suggest, ack — is gated at the server.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schuettc/galley/internal/debug"
	"github.com/schuettc/galley/internal/mcp"
	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
	"github.com/schuettc/galley/internal/version"
	tools "github.com/schuettc/tools-common"
)

// channelInstructions reaches the session's system prompt through the MCP
// handshake — the delivery mechanism `galley agent-prompt` never had. The
// content deliberately overlaps agent-prompt's: one protocol, two carriers.
// V8 rewrites agentprompt.go next against the SAME protocol, so keep this
// wording portable — its implementer reads this diff.
const channelInstructions = `You are paired with galley, a browser review surface for documents this session produces.
Events arrive as <channel source="galley" doc="…" reason="revise|settle|changed|approve|closed">. On "revise" and "settle", the notification carries the rules for answering it — read what arrives with the event, not only this.
Whenever you write to answer a round, on any event: make targeted, in-place edits to the .md file and never rewrite the whole file — the reviewer may be editing it at the same time, and a whole-file write can silently erase what they just wrote.
If you have LOST the round — your context was compacted, or this session restarted mid-review — run galley round <doc> to read it again, with the same instructions and the same keys you were given. A sent round is no longer pending, so do not reach for galley pending instead.
On "approve", the reviewer approved the document as it stands and the review is over. On "changed", re-read the document before acting. On "closed", the editor is gone and nobody is waiting for more review work.
If you expect a review and hear nothing, call galley_channel_status. It reports which open documents this session is attached to and why any others are not.
To put a document under review, call galley_open with its path: it starts the editor for this session and returns the URL — give that to the reviewer, and their rounds arrive here. Never run galley edit from a shell while this channel is present; an editor opened that way belongs to no session and any channel whose scope covers it may claim it.`

// reviseGuidance is the rule set that governs answering a "revise" or
// "settle" event — see reasonGuidance below.
const reviseGuidance = `The rules for answering this round. Revise the document by editing the .md file directly with your normal file tools — while you hold the round, galley watches the file and streams every save into the reviewer's browser, so save as often as you like. MAKE TARGETED EDITS AND NEVER REWRITE THE WHOLE FILE: the reviewer is editing that same file at the same time, so your copy is stale from the moment you read it, and one whole-file write silently destroys everything they typed since — including passages they deliberately deleted, which reappear because they are still in your copy. This has happened, and it cost a reviewer a section. A write your tools reject with "file has been modified since read" is that guard firing correctly; re-reading one line to satisfy it and writing the whole file anyway defeats the guard rather than answering it. Ordinary Markdown expresses everything: formatting, links, headings, lists, tables. Do not use raw HTML, footnotes, reference-style links, autolinks, definition lists, ::: directives, or [!NOTE] callouts; galley refuses those dialects and holds a save that will not parse until your next save fixes it.
When the whole revision is in the file, call galley_ack once with state "answered" and a one-sentence note; that commits your round — exactly once, for the whole revision, never per save. Pass "changes" with that call: one entry per change you made, each quoting a few words of what you WROTE so galley can find it, and naming in "answers" which instruction it answers by the key the round handed you. Every instruction in the round is printed with its key — the quoted words followed by [key cm-0fe4404...], or the key alone on its line for a whole-document instruction — and that key is what "answers" takes; without it the reviewer sees a change card with no ask beside it. Galley owns the list of what changed and computes it from the two documents; your entries only annotate it, so a quote it cannot place in exactly one changed passage is ignored, and so is a key it never sent you — the change still reaches the reviewer with no sentence beside it and nobody is shown an error. Quote text you actually wrote; omit "answers" when a change answers no instruction in particular. A comment is an instruction discharged by the revision itself, not a conversation to answer and not a proposal the reviewer must accept.
If the work will take more than a moment, acknowledge "working" first. If you genuinely cannot do an instruction, run galley cannot <doc> --why "what stopped you". That records an unchanged exception round for the reviewer; it is a report, not an argument or retry.`

// reasonGuidance is the rule set that governs answering THIS event, and it
// travels with the event rather than living in the handshake.
//
// The write rule below is the reason this function exists. It is not style
// advice: the reviewer edits the same .md in their browser while the agent
// holds the round, and on 2026-08-22 a 21-instruction round answered with one
// whole-document write brought back a section the reviewer had deleted. A rule
// that arrives with the revise is adjacent to the temptation; the same rule in
// a handshake blob is a hundred thousand tokens behind it.
//
// The endings get a sentence. Their MEANING is wake.go's — this only says what
// to do next, and must not disagree with it.
func reasonGuidance(reason string) string {
	switch reason {
	case reasonRevise, reasonSettle:
		return reviseGuidance
	case reasonApprove:
		return "The review is over. Do not re-arm, and do not ack."
	case reasonChanged:
		return "Re-read the document before doing anything else — it changed under you."
	case reasonClosed:
		return "The editor is gone and nobody is waiting. Stop watching this document."
	default:
		return ""
	}
}

// composeWake is the notification as the agent reads it: the envelope sentence,
// then the separator, then the guidance for this event. Clients that split on
// the separator (pi-channels) can dedupe identical guidance across a coalesced
// batch; clients that do not render it as a readable rule.
func composeWake(reason, envelope string, paged bool) string {
	g := reasonGuidance(reason)
	// A page-backed revise/settle carries the same page-mode guidance the pull
	// carrier appends (pageBacked): edit content.md for wording or the original
	// .html for structure, and never galley's derived files. It rides HERE, not
	// in reasonGuidance, because it depends on the DOCUMENT (is it a page?), not
	// on the reason — reasonGuidance answers by reason alone. It joins the
	// guidance half after the one separator, so a client that splits on "---"
	// still sees exactly one. Without it a channel-woken agent had no page-mode
	// guidance at all and reached into template.json to answer a structural ask.
	if paged && (reason == reasonRevise || reason == reasonSettle) {
		g += "\n" + pageBacked
	}
	if g == "" {
		return envelope
	}
	return envelope + "\n\n---\n" + g
}

var ackSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "doc":   {"type": "string", "description": "path to the document under review"},
    "state": {"type": "string", "enum": ["received", "working", "answered", "declined", "failed"]},
    "note":  {"type": "string", "description": "one sentence for the reviewer (optional)"},
    "changes": {
      "type": "array",
      "description": "optional: one entry per change you made. Galley owns the list of what changed and only matches your entries onto it; an entry it cannot place is ignored without an error.",
      "items": {
        "type": "object",
        "properties": {
          "quote":   {"type": "string", "description": "a few words of what you WROTE, so galley can find this change"},
          "answers": {"type": "array", "items": {"type": "string"}, "description": "which instruction this answers, by the key the round handed you"},
          "note":    {"type": "string", "description": "one sentence about this change, for the reviewer"}
        },
        "required": ["quote"]
      }
    }
  },
  "required": ["doc", "state"]
}`)

// statusSchema takes no arguments on purpose: the question it answers is
// always the same one, and a filter would only let the model ask it wrongly.
var statusSchema = json.RawMessage(`{"type": "object", "properties": {}}`)

// openSchema takes the document and nothing else: which session owns the
// editor is this channel's to know, and a caller-supplied owner would be the
// environment-derived owner in different clothes.
var openSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "doc": {"type": "string", "description": "path to the .md or .html document, absolute or relative to the session's working directory"}
  },
  "required": ["doc"]
}`)

// channelPoll is how long one attach poll is held open — the channel's whole
// life is re-arming, so minutes-long polls cost nothing and a short one would
// turn a quiet review into a stream of requests. The server bounds every poll
// (maxWaitPoll), so no client-side timeout backs this up.
const channelPoll = 5 * time.Minute

// channel holds the scan loop's state: which rooms are already attached, so a
// rescan is idempotent.
type channel struct {
	scope string // absolute root; only documents under it are considered
	// scopeReal is the scope with every symlink resolved, kept BESIDE the
	// spelling the caller gave rather than replacing it. On macOS /tmp is a
	// symlink to /private/tmp, so one directory has two names and a channel
	// scoped at one of them compared raw-string prefixes against adverts
	// written with the other — and attached to nothing, silently. Court's own
	// demo harness lives at a path with both spellings. Both are kept because
	// the caller's spelling is what a diagnostic must print back (`--scope
	// /tmp/x` has to read as `/tmp/x`) and the resolved one is what a
	// comparison must use.
	scopeReal string
	self      string // this session's id; "" matches only unowned entries
	scanEvery time.Duration
	// galley_open's knobs. exe is the binary the tool spawns `edit` from —
	// this process's own, so the editor is always the channel's version.
	// openPoll and openTimeout bound the wait for the new editor's advert;
	// fields, like scanEvery, so a test runs them at millisecond speed.
	exe         string
	openPoll    time.Duration
	openTimeout time.Duration
	srv         *mcp.Server
	mu          sync.Mutex
	attached    map[string]bool
	// The diagnostic's state — the answer to "why am I not attached?", which
	// until now no surface in this process could give. Rebuilt on every scan,
	// except `gone`, which is a short history: an advert that was reaped or an
	// editor that went away is no longer in the registry to be described.
	pages      map[string]string // attached room -> document, for the status text
	unattached map[string]string // live advert -> why this channel is not on it
	// stopped is every room whose editor answered "closed" or stopped
	// answering while its advert was still up. THE ADVERT OUTLIVES THE
	// EDITOR by seconds on an orderly shutdown — ReleaseWaiters runs before
	// http.Server.Shutdown, and withdrawEdit only after the flush — and
	// EditServer.waitClosed is terminal, so a re-attach in that window gets
	// `closed` again instantly and would announce the same lost review once
	// per scan. Cleared when the advert finally goes, which is the only thing
	// that could make attaching sensible again.
	stopped   map[string]bool
	problems  []registry.Problem
	gone      []string
	announced map[string]bool // stderr dedupe: one line per room per reason
	// ended is every room this channel DETACHED from on a terminal ending —
	// an approve or a discard. It is what makes a scan-loop re-attach
	// distinguishable from a cold start, which is the whole of the reopen fix:
	// the only thing that re-advertises an entry a verdict withdrew is
	// OnReopen, so a room turning up here again is a review that came back.
	// See announceReopen.
	ended map[string]bool
	// listErrOnce guards the one diagnostic scanOnce is allowed: a registry
	// that cannot be listed means the channel silently does nothing forever,
	// which has to be said somewhere — once, on stderr where the startup line
	// goes, not once per 100ms scan.
	listErrOnce sync.Once
}

func newChannel(scope, self string) *channel {
	abs, err := filepath.Abs(scope)
	if err != nil {
		abs = scope
	}
	c := &channel{
		scope:       abs,
		scopeReal:   resolveSymlinks(abs),
		self:        self,
		scanEvery:   100 * time.Millisecond,
		openPoll:    200 * time.Millisecond,
		openTimeout: 10 * time.Second,
		attached:    map[string]bool{},
		ended:       map[string]bool{},
		pages:       map[string]string{},
		unattached:  map[string]string{},
		stopped:     map[string]bool{},
		announced:   map[string]bool{},
	}
	// os.Executable can fail only in exotic setups; an empty exe makes
	// galley_open report "cannot locate the galley binary" rather than spawn
	// something else that happens to be on PATH.
	c.exe, _ = os.Executable()
	c.srv = mcp.New(mcp.Handler{
		Name:         "galley",
		Version:      version.String(),
		Instructions: channelInstructions,
		Tools: []mcp.Tool{{
			Name:        "galley_ack",
			Description: "Acknowledge a galley review: tell the reviewer where their ask stands.",
			InputSchema: ackSchema,
		}, {
			Name: "galley_channel_status",
			Description: "Why is galley attached, or not? Lists every live editor on this machine, " +
				"which ones this session receives wakes from, and the reason for each one it does not. " +
				"Call it when a reviewer says they pressed Revise and nothing arrived.",
			InputSchema: statusSchema,
		}, {
			Name: "galley_open",
			Description: "Put a document under review: start its editor for THIS session and return the URL to give the reviewer. " +
				"Idempotent — a document this session already has open returns the same URL. " +
				"This is the only way to open a document; do not run `galley edit` from a shell.",
			InputSchema: openSchema,
		}},
		Call: c.callTool,
	})
	return c
}

func (c *channel) callTool(name string, args json.RawMessage) (string, error) {
	switch name {
	case "galley_ack":
	case "galley_channel_status":
		return c.status(), nil
	case "galley_open":
		var p struct {
			Doc string `json:"doc"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", err
		}
		return c.open(p.Doc)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
	var p struct {
		Doc     string      `json:"doc"`
		State   string      `json:"state"`
		Note    string      `json:"note"`
		Changes []AckChange `json:"changes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	rt, ok := serve.FindRuntime(p.Doc)
	if !ok {
		return "", fmt.Errorf("no running editor for %s", p.Doc)
	}
	if err := postAck(rt.URL, p.State, p.Note, p.Changes); err != nil {
		return "", err
	}
	return "acknowledged: " + p.State, nil
}

// --- who this channel may attach to, and why it may not ---

// resolveSymlinks answers what a path REALLY is, and answers it for a path
// that does not exist yet: EvalSymlinks fails outright on a missing leaf, so
// the deepest existing ancestor is resolved and the rest re-joined. A scope
// directory always exists; a document may have been deleted while its editor
// still serves it, and "the file is gone" must not silently become "out of
// scope".
func resolveSymlinks(p string) string {
	if p == "" {
		return p
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)
	if base == "" || dir == p {
		return p
	}
	return filepath.Join(resolveSymlinks(dir), base)
}

// underRoot is the containment test the scan used to spell inline as a raw
// string prefix. Both spellings of the pair are compared by inScope; this one
// only decides containment, on paths already in one spelling.
func underRoot(page, root string) bool {
	if page == "" || root == "" {
		return false
	}
	return page == root ||
		filepath.Dir(page) == root ||
		strings.HasPrefix(page, root+string(filepath.Separator))
}

// inScope compares the document against the root in BOTH spellings, because
// one directory can have two names and the reviewer's editor and this
// channel's scope are typed by different hands at different times. Measured:
// a channel scoped at /tmp/…/work and an editor page at /private/tmp/…/work —
// the same directory — attached to nothing, with no diagnostic.
//
// The raw comparison is kept and tried FIRST. Resolution is a syscall per
// path, it can fail, and on the overwhelmingly common path the two spellings
// are already identical; the resolved comparison is the fallback that makes
// the uncommon one work rather than a replacement that makes every scan pay.
func (c *channel) inScope(page string) bool {
	if underRoot(page, c.scope) || underRoot(page, c.scopeReal) {
		return true
	}
	if real := resolveSymlinks(page); real != page {
		return underRoot(real, c.scope) || underRoot(real, c.scopeReal)
	}
	return false
}

// claim decides ownership: an editor belongs to the ONE session that opened it,
// and no other session ever attaches to it — whether that session is alive or
// gone.
//
// THIS FILE ONCE ADOPTED ORPHANS, and that adoption was the misroute. `--continue`
// draws a fresh session id, so a restart orphaned every document the previous
// session opened; adoption let the NEXT session pick the review back up instead
// of hearing silence. The intent was "re-home a document to ITS session after a
// restart." The behaviour, once every session shares one whole-workspace scope,
// was "re-home it to whichever unrelated session scans the orphan first" — a
// Revise meant for the agent you were working with fell into a stranger's lap.
// Measured, live, more than once. That is what this rule now refuses.
//
// THE RULE NOW: attach ONLY to an advert this session owns — its Owner is this
// session's id — or one that is UNOWNED (Owner "", a plain-terminal `galley
// edit` that belongs to no session; claimForAttach then stamps this session so
// exactly one channel takes it). An advert owned by ANOTHER session is never
// this channel's, live or dead. A dead owner is not an invitation to adopt: the
// editor is bound to its session's life (see runEdit's session watcher) and
// shuts itself down when that session ends, so a live advert owned by a dead
// session is a transient the channel reports and leaves alone. If a restart
// stranded a review, the fix is to reopen it — galley_open starts the editor
// from inside the channel and stamps this session as owner — never to have a
// bystander answer for it.
func (c *channel) claim(e registry.Entry) (bool, string) {
	switch {
	case e.Owner == "" || e.Owner == c.self:
		return true, ""
	case registry.SessionLive(e.Owner):
		return false, fmt.Sprintf("owned by session %s, whose channel is still running — "+
			"its wakes go to that session, not this one", e.Owner)
	default:
		return false, fmt.Sprintf("opened by session %s, which is no longer running — "+
			"its editor is shutting down; open it again with galley_open to review it in this session", e.Owner)
	}
}

// claimForAttach takes EXCLUSIVE ownership of an UNOWNED advert on
// the first attach, by stamping this session's id on it, so no other channel
// under whose root it also falls attaches to it too and its wakes fan out to
// both sessions. registry.Claim is a compare-and-set under an advisory lock, so
// of two channels racing one advert exactly one wins.
//
// It returns ok=false with a reason only when another LIVE session claimed it
// first — the loser leaves it alone. ok=true means proceed: the advert is
// already ours, we just claimed it, or a registry error made us attach through
// (a possible duplicate beats silence, the trade this file makes wherever
// presence cannot be proven). Claiming is skipped when there is nothing to
// stamp: a channel with no session id attaches to unowned adverts as before,
// and stamping "" would leave the advert unowned.
func (c *channel) claimForAttach(e registry.Entry) (bool, string) {
	c.mu.Lock()
	already := c.attached[e.Room]
	c.mu.Unlock()
	if already || c.self == "" || e.Owner == c.self {
		return true, ""
	}
	claimed, err := registry.Claim(e.Room, c.self)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "[galley channel] could not claim %s (%v) — attaching without exclusive ownership\n", e.Page, err)
		return true, ""
	case !claimed:
		return false, fmt.Sprintf("%s is open, but another live session claimed it first — its wakes go there", e.Page)
	default:
		return true, ""
	}
}

// run starts the scan loop and serves MCP until the reader closes. The scan
// lives HERE rather than in runChannel so the test's newChannel(...).run(pipes)
// exercises the same lifecycle the real command does. The context cancels
// every held long-poll when the reader closes — without it, an attach parked
// on a five-minute poll outlives the session that wanted its wakes.
func (c *channel) run(r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// THIS SESSION IS NOW HERE, and saying so is what lets every OTHER channel
	// tell an orphaned document from one that is still spoken for — see claim.
	// Best-effort in both directions: a channel that cannot write its presence
	// still attaches to everything it owns, and one that dies without
	// withdrawing is reaped by PID on the next read.
	if c.self != "" {
		if err := registry.AnnounceSession(c.self); err != nil {
			fmt.Fprintf(os.Stderr, "[galley channel] cannot record this session's presence: %v — "+
				"editors this session opens cannot bind to its lifetime and may keep serving after it ends\n", err)
		}
		defer func() { _ = registry.WithdrawSession(c.self) }()
	}
	go func() {
		for {
			c.scanOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.scanEvery):
			}
		}
	}()
	return c.srv.Run(r, w)
}

// scanOnce attaches to everything this channel may attach to, AND RECORDS WHY
// IT DID NOT ATTACH TO THE REST. That second half is new and is the point: an
// advert skipped in silence is indistinguishable from no advert at all, and
// the two call for opposite actions from whoever is waiting.
func (c *channel) scanOnce(ctx context.Context) {
	entries, problems, err := registry.Inspect()
	if err != nil {
		c.listErrOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "[galley channel] cannot read the live registry: %v — no editors will be attached\n", err)
		})
		return
	}
	unattached := map[string]string{}
	// An out-of-scope advert can never wake THIS session, so its unattached
	// reason is recorded for status() but kept off stderr — the startup line
	// exists to break in-scope silence, and a cross-repo editor is not that.
	quiet := map[string]bool{}
	live := map[string]bool{}
	for _, e := range entries {
		live[e.Room] = true
		c.mu.Lock()
		stopped := c.stopped[e.Room]
		c.mu.Unlock()
		if stopped {
			unattached[e.Room] = fmt.Sprintf("%s was open, but its editor stopped answering — "+
				"the advert is still up and has not been withdrawn", e.Page)
			continue
		}
		if !c.inScope(e.Page) {
			unattached[e.Room] = fmt.Sprintf("%s is open, but outside this channel's scope (%s)", e.Page, c.scope)
			quiet[e.Room] = true
			continue
		}
		ok, why := c.claim(e)
		if !ok {
			unattached[e.Room] = fmt.Sprintf("%s is open, %s", e.Page, why)
			continue
		}
		// CLAIM-ON-ATTACH: take exclusive ownership of an unowned/orphaned advert
		// so no other channel under whose root it also falls attaches to it too.
		// See claimForAttach.
		if okc, reason := c.claimForAttach(e); !okc {
			unattached[e.Room] = reason
			continue
		}
		c.mu.Lock()
		seen := c.attached[e.Room]
		if !seen {
			c.attached[e.Room] = true
			c.pages[e.Room] = e.Page
		}
		c.mu.Unlock()
		if !seen {
			// WHETHER AN AGENT WAS ATTACHED AT A GIVEN MOMENT. `say` answers
			// this once per room per reason and is deliberately deduplicated,
			// so it cannot be read as a timeline; this can.
			debug.Log(debug.Event{
				Kind: debug.KindAttached, Where: "channel", Doc: e.Page, Room: e.Room,
				Note: "the channel attached to this editor",
			})
			go c.attach(ctx, e)
		}
	}
	c.mu.Lock()
	c.unattached = unattached
	c.problems = problems
	for room := range c.stopped {
		if !live[room] {
			delete(c.stopped, room)
		}
	}
	c.mu.Unlock()
	for room, why := range unattached {
		if quiet[room] {
			continue
		}
		c.say(room, why, why)
	}
	for _, p := range problems {
		c.say(p.File, p.Reason, "ignoring "+p.String())
	}
}

// say writes one diagnostic line to stderr — where the startup line goes, and
// where the harness debug log (~/.claude/debug/<session>.txt) picks it up —
// AT MOST ONCE per room per reason. The scan runs every two seconds and most
// of what it reports is a standing state, so an undeduplicated line would be a
// log nobody reads, which is the same as no diagnostic at all.
func (c *channel) say(key, reason, line string) {
	c.mu.Lock()
	k := key + "\x00" + reason
	said := c.announced[k]
	c.announced[k] = true
	c.mu.Unlock()
	if !said {
		fmt.Fprintf(os.Stderr, "[galley channel] %s\n", line)
	}
}

// remember keeps a short history of adverts that stopped being attachable —
// an editor that went away, an advert that answered nothing. They are gone
// from the registry by the time anyone asks, so the scan cannot rebuild them
// and the status would otherwise report a clean slate over a review that
// ended badly. Bounded: this process lives as long as the session does.
func (c *channel) remember(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gone = append(c.gone, line)
	if len(c.gone) > 8 {
		c.gone = c.gone[len(c.gone)-8:]
	}
}

// status is galley_channel_status: the answer to "I pressed Revise and nothing
// happened — why?", which no surface in this process could give.
//
// THE THREE SILENCES IT TELLS APART are the ones that look identical from
// inside a session and call for different actions: nothing is open (open
// something), something is open that another live session owns (that session
// is answering, not this one), and something is open outside this channel's
// root (the scope is wrong, or the document is).
func (c *channel) status() string {
	c.mu.Lock()
	attached := make([]string, 0, len(c.pages))
	for _, page := range c.pages {
		attached = append(attached, "  "+page)
	}
	unattached := make([]string, 0, len(c.unattached))
	for _, why := range c.unattached {
		unattached = append(unattached, "  "+why)
	}
	problems := make([]string, 0, len(c.problems))
	for _, p := range c.problems {
		problems = append(problems, "  "+p.String())
	}
	gone := append([]string(nil), c.gone...)
	c.mu.Unlock()
	sort.Strings(attached)
	sort.Strings(unattached)

	var b strings.Builder
	self := c.self
	if self == "" {
		fmt.Fprintf(&b, "galley channel — scope %s, no session id "+
			"(neither CLAUDE_CODE_SESSION_ID nor AGENT_SESSION_ID was set, so this channel attaches to unowned documents only)\n", c.scope)
	} else {
		fmt.Fprintf(&b, "galley channel — scope %s, session %s\n", c.scope, self)
	}
	if len(attached) > 0 {
		fmt.Fprintf(&b, "attached (%d) — a Revise here reaches this session:\n%s\n",
			len(attached), strings.Join(attached, "\n"))
	} else {
		b.WriteString("attached (0) — no editor is currently sending wakes to this session.\n")
	}
	if len(unattached) > 0 {
		fmt.Fprintf(&b, "open but NOT attached (%d):\n%s\n", len(unattached), strings.Join(unattached, "\n"))
	}
	if len(attached) == 0 && len(unattached) == 0 && len(problems) == 0 {
		b.WriteString("no live editor is advertising itself: nothing on this machine is running `galley edit`, " +
			"so there is no review to hear from. Call galley_open with a document to start one.\n")
	}
	if len(problems) > 0 {
		fmt.Fprintf(&b, "adverts this channel refused (%d):\n%s\n", len(problems), strings.Join(problems, "\n"))
	}
	if len(gone) > 0 {
		fmt.Fprintf(&b, "recently gone (%d):\n  %s\n", len(gone), strings.Join(gone, "\n  "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// waitWire is the part of GET /_galley/wait's body the channel reads — the
// reason, the cursor, and the captured instructions. The handoff clears the
// live pending marks, so this response is the only place the channel can read
// the quote that anchors each instruction.
type waitWire struct {
	Reason      string `json:"reason"`
	Fingerprint string `json:"fingerprint"`
	// Note is the reopen's reason — the one thing in a wake that the model
	// cannot read back out of `galley pending`.
	Note string `json:"note,omitempty"`
	// Pending IS the server's own view, not a narrower re-declaration of it.
	// This used to be an inline anonymous struct carrying Instructions alone —
	// a THIRD spelling of the wait envelope beside `waitResult` and the
	// server's `waitReply` — and a narrower copy is how a field goes missing
	// on one side for a phase with every gate green. It cost us the
	// instruction key once already.
	Pending pendingPayload `json:"pending"`
}

// attach holds one editor's long-poll until the editor goes away — AND SAYS SO
// WHEN IT DOES. Every return from this loop used to be silent, which left the
// agent unable to tell "the reviewer is still reading" from "the review is
// gone"; both are silence, and only one of them means stop waiting.
func (c *channel) attach(ctx context.Context, e registry.Entry) {
	defer func() {
		c.mu.Lock()
		delete(c.attached, e.Room)
		delete(c.pages, e.Room)
		c.mu.Unlock()
		// THE DETACH IS THE DEFER, because this function has seven exits and a
		// line at each of them is six chances to miss one — which is how a
		// session comes to believe it is attached to a review that ended.
		debug.Log(debug.Event{
			Kind: debug.KindDetached, Where: "channel", Doc: e.Page, Room: e.Room,
			Note: "the channel stopped polling this editor",
		})
	}()
	// answered separates the two silences this function can end in. Once ONE
	// poll has come back, there was an editor there and its disappearance is
	// news the session must have. Before that, the advert pointed at nothing
	// from the start — a recycled PID, or an editor killed without
	// withdrawing — and announcing a review that ended would be announcing a
	// review that never began. That case is reaped and recorded instead.
	answered := false
	client := &http.Client{} // no client timeout: the server bounds every poll
	c.mu.Lock()
	delete(c.ended, e.Room)
	c.mu.Unlock()
	since := ""
	for {
		q := url.Values{}
		if since != "" {
			q.Set("since", since)
		}
		q.Set("timeout", channelPoll.String())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+"/_galley/wait?"+q.Encode(), nil)
		if err != nil {
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return // this session is shutting down; nothing to tell it
			}
			if !answered {
				c.reapStaleAdvert(e, err)
				return
			}
			c.emitGone(e, "the editor stopped answering")
			return
		}
		answered = true
		if resp.StatusCode == http.StatusServiceUnavailable {
			// The document was being written to throughout the poll —
			// retriable by design, same backoff as `galley wait`.
			_ = resp.Body.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitBusyBackoff):
			}
			continue
		}
		var reply waitWire
		err = json.NewDecoder(resp.Body).Decode(&reply)
		_ = resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.emitGone(e, "the editor's answer could not be read")
			return
		}
		switch reply.Reason {
		case reasonRevise, reasonSettle, reasonChanged, reasonApprove:
			// RE-ARM BEFORE EMIT: update the cursor and let the loop issue the
			// next poll; the emission rides its own goroutine so stdio latency
			// never widens the unregistered window.
			since = reply.Fingerprint
			go c.emit(e, reply.Reason, reply.Fingerprint, reply.Note,
				reply.Pending, len(reply.Pending.Instructions))
			// THE TWO TERMINAL ENDINGS DETACH; THE REOPEN DOES NOT. Approve and
			// discard both end the review and both withdraw the registry entry
			// server-side, so there is nothing left to poll and this attach
			// returns. A reopen says the opposite — the review is LIVE again —
			// so the loop keeps polling, and the reopen's own re-advertise is
			// what brings a channel that had already detached back through the
			// scan loop.
			if reply.Reason == reasonApprove {
				// REMEMBERED, not merely dropped. The room is what tells the
				// next attach that this is a re-attach and not a cold start —
				// see announceReopen for why that distinction is the reopen
				// event's only carrier once there is no waiter left to wake.
				c.mu.Lock()
				c.ended[e.Room] = true
				c.mu.Unlock()
				return // the review is over; the registry entry is withdrawn server-side
			}
		case reasonTimeout:
			since = reply.Fingerprint
		case reasonClosed:
			// THE EDITOR RELEASED ITS WAITERS, which is what `galley edit`
			// does on its way out (shutdownEdit releases before it shuts the
			// listener down, so a held poll gets an honest answer rather than
			// a torn connection). `galley wait` prints {"reason":"closed"} and
			// exits; the channel used to return in silence, leaving the agent
			// waiting on a review that no longer exists.
			if ctx.Err() == nil {
				c.emitGone(e, "the editor stopped")
			}
			return
		default:
			if ctx.Err() == nil {
				c.emitGone(e, fmt.Sprintf("the editor answered with an unknown reason %q", reply.Reason))
			}
			return
		}
	}
}

// emitGone is the wake the server can never send, for the same structural
// reason announceReopen exists: the editor is not there to send it. A review
// that ends by the editor going away ends with no verdict — no approve, no
// discard, no entrusted handoff — and the agent's correct response is to stop
// waiting, which it can only choose if it is told.
//
// COUNTS ARE UNKNOWN, NEVER ZERO, exactly as in announceReopen: there is
// nothing left to read them from, and "0 suggestions" would say the document
// is clean at the moment the whole review was lost.
//
// `why` rides the note field, which is the same slot the reopen's reason uses
// and for the same reason: it is the one fact in this wake that the model
// cannot recover from anywhere else.
func (c *channel) emitGone(e registry.Entry, why string) {
	c.mu.Lock()
	c.stopped[e.Room] = true
	c.mu.Unlock()
	c.remember(fmt.Sprintf("%s — %s (no verdict was given)", e.Page, why))
	c.say(e.Room, "gone", fmt.Sprintf("%s: %s — the review ended without a verdict", e.Page, why))
	c.emit(e, reasonClosed, "", why, pendingPayload{}, countUnknown)
}

// reapStaleAdvert handles the advert that answered nothing on its very first
// poll: the registry says an editor is at that URL and nothing is. registry
// .List reaps an advert whose PID is dead, but a PID the operating system has
// RECYCLED answers signal 0 from whatever now holds it, so the advert survives
// every scan and this channel would re-attach to nothing forever, believing
// itself attached.
//
// THE PROBE IS THE PROOF, and it is one this channel makes anyway. No wake is
// emitted — nothing was ever attached, so there is no review to have ended —
// but the advert comes down and the reason is recorded where a session can
// read it back.
func (c *channel) reapStaleAdvert(e registry.Entry, cause error) {
	_ = registry.Remove(e.Room)
	line := fmt.Sprintf("%s — unreachable: its advert points at %s and nothing answered there (%v); "+
		"the advert has been withdrawn. A process that died without withdrawing, or a recycled PID.",
		e.Page, e.URL, cause)
	c.remember(line)
	c.say(e.Room, "unreachable", line)
}

// The four ENDING sentences — approveContent, discardContent, goneContent and
// reopenContent — used to live here, beside the one carrier that had been
// taught the trust exit's branch. They live in wake.go now, with the branch
// itself, because the OTHER carrier was never taught it: see wake.go's header
// for what a parked `galley wait` printed during an open handoff.

// countUnknown is what a caller passes for a count it could not read, and
// countMeta is what the meta says about one: `unknown`, never a digit.
//
// Only announceReopen can produce it — every other wake carries the counts in
// the same response that carried the reason, so a wake that arrived at all
// arrived with its numbers. There the count is a second request that can fail
// on its own, and reporting a failed read as `0` would tell the agent the
// document is clean at the exact moment a fresh round opens on it.
const countUnknown = -1

func countMeta(n int) string {
	if n < 0 {
		return "unknown"
	}
	return strconv.Itoa(n)
}

// emit renders one wake as a channel notification. `note` is the reopen's
// reason and is empty on every other wake — a single parameter rather than two
// entry points, because a wrapper nothing called was dead code the linter
// caught immediately.
func (c *channel) emit(e registry.Entry, reason, fp, note string, pending pendingPayload, instructions int) {
	// THE FOUR ENDINGS COME FROM wake.go, which is the ONE place either
	// carrier turns a wake into a meaning. This switch used to hold them, and
	// `galley wait` held its own four — which is how the pull carrier came to
	// tell an agent the review was over during an open handoff.
	//
	// It answers "" for the two wakes each carrier says in its own voice, and
	// those two are this switch: they name galley_ack, an MCP tool a shell
	// loop cannot call, so there is nothing here for the other carrier to
	// agree or disagree with. See wake.go's header.
	content := wake{reason: reason, page: e.Page, note: note, instructions: instructions}.sentence()
	if content == "" {
		switch reason {
		case reasonSettle:
			content = fmt.Sprintf("%s settled in live mode — %d instruction(s).", e.Page, instructions)
		default:
			content = fmt.Sprintf("Revision requested on %s — %d instruction(s).", e.Page, instructions)
		}
		if len(pending.Instructions) > 0 {
			// THE REVIEWER'S OWN EDITS LEAD THE ROUND. An agent reads in
			// order and acts in order; "do not undo these" arriving after
			// eight asks to expand and clarify has already lost. This is the
			// carrier the measured failure happened on.
			content += " The captured round follows:\n\n" +
				formatReviewerChanges(pending.Changes, pending.ChangesDropped) +
				strings.TrimRight(formatInstructions(pending.Instructions), "\n")
		}
	}
	content = composeWake(reason, content, pageBackedDoc(e.Page))
	meta := map[string]string{
		"doc":          e.Page,
		"reason":       reason,
		"fingerprint":  fp,
		"instructions": countMeta(instructions),
	}
	// Additive, and only where there is one: a "note" key on every wake would
	// advertise a field that means nothing on four of the five.
	if note != "" {
		meta["note"] = note
	}
	// THE NOTIFICATION AS SENT, and it is recorded HERE — after `content` is
	// composed and before it goes out — because the whole point is the rendered
	// text the agent actually read, not a reconstruction of it from the parts.
	// On 2026-08-22 the only surviving copy of this string was a Claude Code
	// transcript belonging to a different project. Opt-in; see internal/debug.
	debug.Log(debug.Event{
		Kind:         debug.KindRoundSent,
		Where:        "channel",
		Doc:          e.Page,
		Room:         e.Room,
		Reason:       reason,
		Fingerprint:  fp,
		Note:         note,
		Rendered:     content,
		Instructions: debug.Int(instructions),
		Asks:         debugAsks(pending.Instructions),
	})
	_ = c.srv.Notify(content, meta)
}

// debugAsks copies the captured round into the debug record's own type. It is
// the ASKS AS DELIVERED — key, words and quote — which is the second half of
// the question the rendered text alone cannot answer: whether the key the agent
// was handed is the key the server recorded on the round.
func debugAsks(in []instructionPayload) []debug.Ask {
	if len(in) == 0 || !debug.On() {
		return nil
	}
	out := make([]debug.Ask, 0, len(in))
	for _, p := range in {
		out = append(out, debug.Ask{Key: p.Key, Text: p.Text, Quote: p.Quote})
	}
	return out
}

// channelFlags is galley channel's flag set, built by newChannelFlags so the
// app registry (NewFlags) and runChannel share one construction.
type channelFlags struct {
	scope *string
}

func newChannelFlags() (*flag.FlagSet, *channelFlags) {
	fs := flag.NewFlagSet("channel", flag.ContinueOnError)
	v := &channelFlags{
		scope: fs.String("scope", ".", "root directory; only documents under it are attached"),
	}
	return fs, v
}

func runChannel(args []string, out, errw io.Writer) error {
	fs, v := newChannelFlags()
	// channel takes no positional argument, so this parses directly rather
	// than through splitPositional — the same reason there is nothing to
	// split.
	if err := tools.ParseFlags(fs, args, out); err != nil {
		return err
	}
	scope := v.scope
	self := sessionID()
	cwd, _ := os.Getwd()
	// The three measurements from the design spec, answered on every start and
	// visible in the harness debug log (~/.claude/debug/<session>.txt).
	fmt.Fprintf(os.Stderr, "[galley channel] cwd=%s session_id_present=%t scope=%s\n", cwd, self != "", *scope)

	c := newChannel(*scope, self)
	// AND THE ROOT AS IT WILL ACTUALLY BE COMPARED. A scope typed one way and
	// an editor opened the other way round a symlink is the most likely way a
	// launch silently attaches to nothing — /tmp is /private/tmp on macOS —
	// so when the two spellings differ, both are on the record from the first
	// line of the log.
	if c.scopeReal != c.scope {
		fmt.Fprintf(os.Stderr, "[galley channel] scope %s resolves to %s; documents under either spelling attach\n",
			c.scope, c.scopeReal)
	}
	c.scanEvery = 2 * time.Second
	return c.run(os.Stdin, os.Stdout)
}
