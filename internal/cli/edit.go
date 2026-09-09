// edit.go is the CLI surface for edit mode: `edit`, `pending`, `suggest`,
// `accept`, `reject`, `revise`. It mirrors serve/comments/reply/resolve's
// shape in main.go — live via a running server's JSON endpoints when
// serve.FindRuntime finds one, offline via parse -> transform -> serialize
// otherwise — but for a markdown document instead of a review page.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/serve"
	"github.com/schuettc/galley/internal/suggest"
	"github.com/schuettc/galley/internal/versions"
)

// pendingPayload mirrors the JSON shape of serve.EditServer's internal
// pendingView exactly (same field names, same tags) — GET /_galley/pending
// decodes straight into this, and the offline path builds one from a plain
// parse, so callers never see a different shape depending on where the data
// came from.
// instructionPayload and pendingPayload ARE the server's own types, aliased.
//
// They used to be hand-written near-copies, and the copy is what cost us: the
// CLI's instruction struct carried Text, Quote and At and NO KEY, while
// `serve.InstructionView` had carried `key` all along. Both carriers told the
// agent to answer "by the key the round handed you" and the round handed over
// none, so a paired agent sent `answers` on none of 23 changes across three
// rounds — it had nothing to answer with. Nothing failed loudly because a JSON
// decoder ignores what it is not asked for, and no test compared the two
// shapes.
//
// AN ALIAS CANNOT DISAGREE WITH ITSELF, which is the whole point. These are
// the same types the server marshals and the same types `web/wire.d.ts` is
// generated from, so a field added, renamed or removed on the server reaches
// every CLI surface and the browser in one move — and `just verify` fails on
// drift rather than a reviewer noticing months later.
//
// The CLI reads only some of these fields. That is fine and is not a reason to
// keep a narrower copy: the cost of the copy was never the unused fields, it
// was the one field nobody noticed was missing.
type instructionPayload = serve.InstructionView

type pendingPayload = serve.PendingView

// --- galley edit ---

// editFlags is galley edit's flag set, built by newEditFlags so the app
// registry (NewFlags) and runEdit share one side-effect-free construction.
type editFlags struct {
	port     *int
	noOpen   *bool
	onRevise *string
	onSettle *string
	quiet    *time.Duration
	root     *string
	owner    *string
}

func newEditFlags() (*flag.FlagSet, *editFlags) {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	v := &editFlags{
		port:   fs.Int("port", 0, "port to listen on (default: a free one; the URL is printed and the registry carries it)"),
		noOpen: fs.Bool("no-open", false, "do not open a browser"),
		onRevise: fs.String("on-revise", "", "shell command that hands the round's instructions to an agent, "+
			"run on demand (POST /_galley/revise, or the editor's Revise button)"),
		onSettle: fs.String("on-settle", "", "shell command to run once the document settles in live mode "+
			"(e.g. 'muster nudge galley')"),
		quiet: fs.Duration("quiet", 8*time.Second, "how long the document must sit unchanged before --on-settle fires"),
		root: fs.String("root", "", "site root to serve preview assets from, for an HTML page (default: the page's own "+
			"directory). Set it to the site root so a subpage's ../shared assets resolve in the preview "+
			"as they do when the whole site is served from that root."),
		owner: fs.String("owner", "", "session id that owns this editor — set by the channel's galley_open tool, "+
			"never by hand. That session's channel must be live; the editor stops when it stops. "+
			"Omit it for an editor that belongs to no session (a plain terminal)."),
	}
	return fs, v
}

func runEdit(args []string, out, errw io.Writer) error {
	fs, v := newEditFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	port, noOpen, onRevise, onSettle, quiet, root, owner := v.port, v.noOpen, v.onRevise, v.onSettle, v.quiet, v.root, v.owner

	if err := requireLiveOwner(*owner); err != nil {
		return err
	}

	srv, err := routeEdit(pos[0], *root)
	if err != nil {
		return err
	}

	logLine := func(line string) {
		fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), line)
	}
	if *onRevise != "" {
		srv.OnRevise = *onRevise
		srv.Log = logLine
	}
	// Wiring the notifier does NOT turn it on. Phase 1 refused to wire it at
	// all, because capture mode's ratified contract was "nothing fires on its
	// own" and setting --on-revise must not silently enrol anyone in
	// settle-triggered notification. Phase 2a keeps that promise with a
	// different mechanism: the notifier is configured here but the server only
	// touches it in LIVE mode, and `on ask` — the default — is exactly phase
	// 1's behaviour. A document that is never told to go live never fires.
	//
	// CONFIGURED, NOT REPLACED. NewEdit always makes one, because the notifier
	// now holds the single "has the document moved since the agent last saw
	// it" decision that a blocked `galley wait` rides — and pull needs no
	// command at all. Assigning a fresh Notifier here would work, but it is the
	// shape that quietly detaches every waiter, so the pattern is worth not
	// setting.
	srv.Notify.Quiet = *quiet
	if *onSettle != "" {
		srv.Notify.Command = *onSettle
		srv.Notify.Log = logLine
	}
	// Whatever this document was carrying when it was opened is not news — not
	// to an --on-settle command, and not to a `galley wait` that arms in the
	// same second.
	srv.SeedNotify()

	// Reuse the port this document last served on so a restart does not 404 the
	// reviewer's open tab (friction #5). Only when the reviewer did not pin one:
	// an explicit --port is an instruction, not a default to override. A
	// remembered port that is now taken falls back to a free one — the hint
	// costs at most one failed bind, never a refusal to serve.
	wantPort := *port
	if wantPort == 0 {
		if p, ok := registry.LastPort(srv.MdPath); ok {
			wantPort = p
		}
	}
	ln, url, err := serve.Listen(fmt.Sprintf("127.0.0.1:%d", wantPort))
	if err != nil && wantPort != 0 && *port == 0 {
		ln, url, err = serve.Listen("127.0.0.1:0")
	}
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		registry.SaveLastPort(srv.MdPath, addr.Port)
	}
	if err := announceEdit(srv, url, *owner); err != nil {
		return fmt.Errorf("announce: %w", err)
	}
	defer withdrawEdit(srv)

	// Approve is terminal for the review but not for the session: the editor
	// keeps serving (Ctrl-C stops it, not the verdict), so only the registry
	// entry — the channel's discovery surface — comes down, same as a normal
	// shutdown's withdrawEdit but without touching the runtime file a still-live
	// `galley pending`/`suggest` needs.
	srv.OnApprove = func() {
		_ = registry.Remove(srv.Room)
		fmt.Println("review approved — Ctrl-C to stop")
	}

	fmt.Printf("document  %s\n", srv.MdPath)
	fmt.Printf("room      %s\n", srv.Room)
	fmt.Printf("serving   %s\n", url)
	for _, line := range editStartupLines(filepath.Base(srv.MdPath), *onRevise, *onSettle, *quiet) {
		fmt.Println(line)
	}
	fmt.Println("\nEdits sync as you type. Ctrl-C to stop.")

	if !*noOpen {
		openBrowser(url)
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// DONE IS CTRL-C FROM THE PAGE, AND IT TAKES THE SAME PATH. The terminal
	// bar's Done button POSTs /_galley/stop, which runs this hook — and this
	// hook does exactly what the signal does: closes one channel the select
	// below is already watching. A second shutdown sequence written for the
	// endpoint would be a second ordering of release-waiters, wait-for-revision,
	// flush, close, and the two would drift the first time either was touched.
	//
	// sync.OnceFunc because a double-press (or a press racing Ctrl-C) must not
	// close a closed channel.
	stopped := make(chan struct{})
	srv.OnStop = sync.OnceFunc(func() {
		fmt.Println("done — stopping the editor")
		close(stopped)
	})

	// BIND THE EDITOR TO THE SESSION THAT OPENED IT. An editor is launched
	// detached, so it outlives the call that started it — and, left alone,
	// the SESSION too: an advert owned by a session that is gone is exactly
	// the orphan the channel used to adopt and misroute. It no longer adopts
	// (see channel.claim); instead the editor takes itself down when its
	// session ends, so no orphan is left for anyone to answer.
	//
	// The session's liveness is its channel's presence (registry.AnnounceSession
	// / SessionLive). requireLiveOwner already proved it at startup, so an
	// --owner here always binds; an editor with no owner has no session to die
	// with and serves until Ctrl-C, as it always has.
	if *owner != "" {
		go watchOwnerSession(ctx, *owner, srv.OnStop)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-stopped:
		return shutdownEdit(srv, httpSrv)
	case <-ctx.Done():
		return shutdownEdit(srv, httpSrv)
	}
}

// shutdownEdit is the ONE orderly stop — reached by Ctrl-C, by SIGTERM, and by
// the terminal bar's Done through EditServer.OnStop. The order of its four
// steps is the whole of it, and each one is load-bearing; see the comments
// inside.
func shutdownEdit(srv *serve.EditServer, httpSrv *http.Server) error {
	// BEFORE Shutdown, which waits for in-flight requests: a blocked
	// GET /_galley/wait is an in-flight request, and one that would hold
	// shutdown open for the whole grace period and then have its connection
	// yanked. Releasing first lets every `galley wait` exit with an honest
	// "the editor stopped" rather than a torn connection.
	srv.ReleaseWaiters()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)

	// ReviseInFlight hands back a nil channel when nothing is running —
	// never block on it unconditionally, only inside this branch where
	// inFlight being true guarantees ch is the real, closeable one.
	if ch, inFlight := srv.ReviseInFlight(); inFlight {
		fmt.Println("\na revision is still running — waiting up to 5s for it to finish")
		select {
		case <-ch:
			fmt.Println("revision finished")
		case <-time.After(reviseShutdownGrace):
			// Not killed: the goroutine (and whatever shell command it
			// started) keeps running as an orphan after this process
			// exits. Silence would be worse than an honest "I don't know
			// how this ends."
			fmt.Println("revision still running — abandoning the wait; " +
				"it will keep running after this process exits, but nothing here will see how it finishes")
		}
	}

	// Flush synchronously: the debounced projection may still be pending,
	// and the reviewer's (or agent's) last edit must not die with the
	// process. Its error is fatal-with-message on purpose — a silently
	// dropped final edit is worse than a loud one.
	if err := srv.Flush(); err != nil {
		return fmt.Errorf("final flush: %w", err)
	}
	// AFTER the flush, never before: Close drops the peer connections and
	// stops the websocket server's idle sweeper, so anything still to be
	// written must already be written.
	if err := srv.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
	fmt.Printf("\n%s\n", srv.MdPath)
	return nil
}

// editStartupLines reports how this session is configured to answer a Revise
// press: pull (`galley wait`), then the two push variants layered on top of
// it. Pull comes first and is printed unconditionally, because it is the
// primary loop — the session already in the room blocks on it, no flag
// required — and push (--on-revise, --on-settle) is the cold-start and
// live-notify fallback for when nobody is. Presenting push first would read
// as though the two were equals; they are not.
//
// Deliberately silent about who is actually waiting right now. At the point
// this runs the listener has not accepted a single connection yet, so
// "waiting: 0" would be true but meaningless — and even once the server is
// serving, a waiter can attach or leave between one line of output and the
// next (see EditServer.Waiting). These lines describe how the server was
// CONFIGURED; asking who is listening this second belongs to the moment a
// Revise press actually asks, which is exactly what handleRevise's 501
// message does.
//
// KEPT DELIBERATELY SHORT. This prints on every single `galley edit`, unlike
// `document`/`sidecar`/`room`/`serving` above it whose length is the path's
// fault, not the label's. An early draft spelled out the full reasoning here
// — why a waiter is dynamic, why a settle only wakes one in ● live — and
// that reasoning is real, but it belongs somewhere a reader goes once
// (`galley edit -h`, and this doc comment), not somewhere it is read and
// re-read on every startup. Each line keeps exactly one fact: what is
// configured, that a waiter is reached either way, and — for on-settle — the
// mode asymmetry, since that is the one a reader cannot guess.
func editStartupLines(mdBase, onRevise, onSettle string, quiet time.Duration) []string {
	const label = "%-9s  %s"
	lines := []string{
		fmt.Sprintf(label, "pull", fmt.Sprintf(
			"`galley wait %s` — waits for Revise (● live: also a settle)", mdBase)),
	}

	if onRevise != "" {
		lines = append(lines, fmt.Sprintf(label, "on-revise",
			fmt.Sprintf("%s  (on demand only — the editor's Revise button, or `galley revise`)", onRevise)))
	} else {
		// The half that used to go unsaid: a missing --on-revise announced
		// nothing, while a missing --on-settle got a line below. One dead hook
		// silent and the other narrated cost a live diagnosis, twice — this
		// says the same thing --on-settle's unset branch always has, at the
		// same length. The full "refuses only if neither is listening" case
		// is in `galley edit -h`, not repeated here.
		lines = append(lines, fmt.Sprintf(label, "on-revise",
			"unset — a Revise press still reaches `galley wait "+mdBase+"` if blocked"))
	}

	// Said out loud in both directions. Live mode with no command configured
	// is silence, and silence is indistinguishable from a broken agent — so
	// the reviewer learns here, at startup, rather than by flipping the toggle
	// and waiting for nothing to happen.
	if onSettle != "" {
		lines = append(lines, fmt.Sprintf(label, "on-settle",
			fmt.Sprintf("%s  (after %s of quiet, in ● live mode only)", onSettle, quiet)))
	} else {
		// NOT "still wakes" unqualified — that claim went stale the moment
		// this handled --on-revise being optional. A Revise press wakes a
		// `galley wait` session in EITHER mode; a settle only wakes one once
		// the session has been switched to ● live, whether or not --on-settle
		// itself is set (Notifier.SetWake is the pull half of the same event
		// Command pushes). Collapsing that into one unconditional "wakes"
		// would be wrong within a minute of `on ask`, the default, staying
		// the default — so the mode asymmetry stays even in the short form.
		lines = append(lines, fmt.Sprintf(label, "on-settle",
			"unset — `galley wait "+mdBase+"` wakes on Revise always, on settle only in ● live"))
	}

	return lines
}

// reviseShutdownGrace bounds how long Ctrl-C waits for an in-flight revision
// before reporting it abandoned. A few seconds, not the revision's own
// unbounded runtime (see EditServer.handleRevise's "DELIBERATELY NO TIMEOUT"):
// the operator asked to stop, and "wait an unknown number of minutes" is not
// a reasonable answer to Ctrl-C.
// routeEdit dispatches to the correct EditServer constructor based on the
// file extension: .html and .htm open the page-backed editor via
// serve.NewEditPage; every other extension (including .md) uses serve.NewEdit.
func routeEdit(path, root string) (*serve.EditServer, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		if root != "" {
			return serve.NewEditPageRoot(path, root)
		}
		return serve.NewEditPage(path)
	default:
		if root != "" {
			return nil, fmt.Errorf("--root applies to an HTML page, not %s", filepath.Ext(path))
		}
		return serve.NewEdit(path)
	}
}

const reviseShutdownGrace = 5 * time.Second

// announceEdit and withdrawEdit are EditServer's equivalent of Server's
// Announce/Withdraw methods, which EditServer does not have. serve.Runtime and
// serve.DefaultRuntimePath (via EditServer.RuntimePath, computed the same way)
// are shared with Server, so a plain `galley pending`/`suggest`/etc. finds an
// edit-mode server exactly the way `galley reply`/`resolve` find a review one.
func announceEdit(srv *serve.EditServer, url, owner string) error {
	raw, err := json.MarshalIndent(serve.Runtime{
		URL:  url,
		Room: srv.Room,
		Page: srv.MdPath,
		PID:  os.Getpid(),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(srv.RuntimePath, append(raw, '\n')); err != nil {
		return err
	}
	advertiseEdit(srv, url, owner)
	return nil
}

// advertiseEdit writes THE registry entry. It is factored out of announceEdit
// so a reopen, if one ever re-advertises, writes the same bytes startup does —
// two spellings of one entry format would drift, and the drift would present
// as a channel that silently never re-attaches.
//
// The live registry is the channel's discovery surface — see internal/registry.
// Owner is --owner's value: the session whose channel asked for this editor,
// or "" for an editor that belongs to no session and is claimable by any
// channel whose root covers it. It is NEVER read from the environment here;
// see requireLiveOwner. Best-effort: an editor that cannot advertise is still
// an editor, so a registry failure is reported but does not refuse to serve.
func advertiseEdit(srv *serve.EditServer, url, owner string) {
	abs, err := filepath.Abs(srv.MdPath)
	if err != nil {
		abs = srv.MdPath
	}
	if err := registry.Write(registry.Entry{
		URL:   url,
		Room:  srv.Room,
		Page:  abs,
		PID:   os.Getpid(),
		Owner: owner,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "galley: live registry: %v\n", err)
	}
}

func withdrawEdit(srv *serve.EditServer) {
	_ = os.Remove(srv.RuntimePath)
	_ = registry.Remove(srv.Room)
}

// requireLiveOwner is the whole of the ownership check: --owner names a
// session, and that session must have a channel listening right now.
//
// THE OWNER IS AN ARGUMENT, NEVER THE ENVIRONMENT. This command used to read
// CLAUDE_CODE_SESSION_ID / AGENT_SESSION_ID and stamp whatever it found. Under
// pi, in-process subagents rewrite the shared AGENT_SESSION_ID, so an editor
// launched from the parent's shell after a dispatch was stamped with a child's
// id and the parent's channel — correctly — refused it (2026-09-08). The
// channel now opens editors for its own session and passes the id here;
// nothing else is trusted to know it. An owner with no live presence is
// refused outright rather than served unbound: an editor that claims a
// session nothing is listening for is the misroute this flag exists to end.
func requireLiveOwner(owner string) error {
	if owner == "" {
		return nil
	}
	if !registry.SessionLive(owner) {
		return fmt.Errorf("owner session %s has no live channel", owner)
	}
	return nil
}

// watchOwnerSession stops the editor once the session that opened it is gone,
// calling the same orderly-stop hook Ctrl-C does (OnStop — which withdraws the
// advert). It is the editor half of Option A: an editor belongs to exactly one
// session and does not outlive it, so the channel never has an orphan to adopt.
//
// The check is DEBOUNCED. A channel can restart within a living session (the
// MCP server re-spawns) and leave a brief gap where the presence file is
// absent; taking the editor down on the first miss would kill a review over a
// flicker. Requiring several consecutive misses waits out that gap while still
// reaping an editor within seconds of its session actually ending — and the
// only real cost of erring long is an orphan the channel already refuses to
// touch, never a misroute.
func watchOwnerSession(ctx context.Context, owner string, stop func()) {
	// ~6s grace: enough to ride out a channel that restarts within a living
	// session, short enough to reap the editor seconds after its session ends.
	watchSession(ctx, owner, 2*time.Second, 3, stop)
}

// watchSession is watchOwnerSession's testable core: it calls stop once the
// owner's presence has been missing for missesToStop consecutive polls. Split
// out so a test can drive it at millisecond speed rather than the production
// six-second grace.
func watchSession(ctx context.Context, owner string, interval time.Duration, missesToStop int, stop func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if registry.SessionLive(owner) {
				misses = 0
				continue
			}
			misses++
			if misses >= missesToStop {
				stop()
				return
			}
		}
	}
}

// --- galley pending ---

// pendingFlags is galley pending's flag set, built by newPendingFlags so the
// app registry (NewFlags) and runPending share one construction.
type pendingFlags struct {
	asJSON *bool
}

func newPendingFlags() (*flag.FlagSet, *pendingFlags) {
	fs := flag.NewFlagSet("pending", flag.ContinueOnError)
	v := &pendingFlags{
		asJSON: fs.Bool("json", false, "emit the pending view verbatim"),
	}
	return fs, v
}

func runPending(args []string, out, errw io.Writer) error {
	fs, v := newPendingFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	asJSON := v.asJSON
	doc := pos[0]

	view, live, err := loadPending(doc)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	source := "on disk"
	if live {
		source = "live"
	}
	if len(view.Instructions) == 0 {
		fmt.Printf("nothing pending on %s (%s)\n", filepath.Base(doc), source)
		return nil
	}
	fmt.Printf("%d instruction(s) on %s (%s)\n\n", len(view.Instructions), filepath.Base(doc), source)
	fmt.Print(formatReviewerChanges(view.Changes, view.ChangesDropped))
	fmt.Print(formatInstructions(view.Instructions))
	return nil

}

// formatInstructions is the one human rendering of a captured instruction
// round. Both wake carriers use it because the Revise handoff clears the live
// pending marks: re-reading /pending after the wake cannot recover the quote
// that says where the instruction belongs.
// formatReviewerChanges is what the reviewer changed BY HAND, rendered above
// their instructions.
//
// IT LEADS THE ROUND, and that placement is the whole point. An agent reads a
// round in order and acts on it in order; a list of "do not undo these" that
// arrives after eight instructions telling it to expand and clarify has
// already lost. The measured failure it exists to prevent — a deleted section
// rewritten back inside an expansion answering a different ask — happened to
// an agent that was told to be clearer and told nothing about the deletion.
//
// Removals lead within it for the same reason: an addition the agent
// duplicates is untidy, a removal it reverses is the reviewer's work destroyed.
func formatReviewerChanges(changes []serve.ReviewerChange, dropped int) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The reviewer also edited the document directly. Do not undo these.\n\n")
	for _, kind := range []string{"removed", "changed", "added"} {
		var lines []string
		for _, c := range changes {
			if c.Kind != kind {
				continue
			}
			switch kind {
			case "removed":
				lines = append(lines, fmt.Sprintf("  · %s", quoteClip(c.Before)))
			case "changed":
				lines = append(lines, fmt.Sprintf("  · %s → %s", quoteClip(c.Before), quoteClip(c.After)))
			case "added":
				lines = append(lines, fmt.Sprintf("  · %s", quoteClip(c.After)))
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s (%d):\n%s\n\n", strings.ToUpper(kind), len(lines), strings.Join(lines, "\n"))
	}
	if dropped > 0 {
		// SAID OUT LOUD. A cap nobody is told about reads as "that is all of
		// them", which is worse than the cap.
		fmt.Fprintf(&b, "(%d more not listed — the reviewer reworked a lot by hand; read the document.)\n\n", dropped)
	}
	return b.String()
}

// quoteClip keeps a summary line readable without hiding that it was clipped.
func quoteClip(s string) string {
	const max = 120
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return strconv.Quote(s)
	}
	return strconv.Quote(s[:max]) + "…"
}

func formatInstructions(instructions []instructionPayload) string {
	var b strings.Builder
	for _, instruction := range instructions {
		// THE KEY IS PRINTED FIRST AND LABELLED, because the agent has to copy
		// it into its ack's `answers` and the contract names it by that word.
		// An unlabelled token beside a quote reads as decoration; `key ...` is
		// the field it goes into, spelled the same way the ack spells it.
		//
		// It leads rather than trails for the case with no quote — a
		// whole-document instruction has no heading, and a key printed after
		// the text would be adrift between two instructions with nothing
		// saying which one it belongs to.
		switch {
		case instruction.Key != "" && instruction.Quote != "":
			fmt.Fprintf(&b, "> %s  [key %s]\n", instruction.Quote, instruction.Key)
		case instruction.Key != "":
			fmt.Fprintf(&b, "[key %s]\n", instruction.Key)
		case instruction.Quote != "":
			fmt.Fprintf(&b, "> %s\n", instruction.Quote)
		}
		for _, line := range strings.Split(instruction.Text, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// loadPending reads the live server's pending view when one is running and
// reachable, falling back to an offline parse — same "unreachable falls
// through, a real rejection does not" convention loadReview/runReply use.
func loadPending(docPath string) (pendingPayload, bool, error) {
	if rt, ok := serve.FindRuntime(docPath); ok {
		resp, err := getPendingHTTP(rt.URL)
		if err == nil {
			var view pendingPayload
			if decErr := decodeOK(resp, &view); decErr != nil {
				return pendingPayload{}, false, decErr
			}
			return view, true, nil
		}
	}
	view, err := offlinePending(docPath)
	return view, false, err
}

// offlinePending builds the same shape a live GET /_galley/pending would,
// straight from the file: no ygo document needed, since suggest.List works
// over a plain docmodel.Doc. Attribution comes back through the same replay
// the live server runs at startup, so `galley pending` reports the same
// author and time whether or not a session happens to be up — a suggestion
// nothing has attributed still prints "(unattributed)".
func offlinePending(docPath string) (pendingPayload, error) {
	abs, err := filepath.Abs(docPath)
	if err != nil {
		return pendingPayload{}, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return pendingPayload{}, err
	}
	model, inline, err := markdown.Parse(raw)
	if err != nil {
		return pendingPayload{}, err
	}
	threads := suggest.ImportInlineComments(model, nil, inline, review.AuthorCourt, time.Now().UTC())
	// Reconcile the file's block/document notes against the sidecar's threads
	// exactly as the live server does at startup, so `galley pending` reports
	// a hand-written note whether or not anything has opened a thread for it
	// yet — and reports it under the same anchor either way.
	threads, orphans, _, _ := suggest.ReconcileNotes(model, threads)
	for _, n := range orphans {
		threads = append(threads, newNoteThread(n))
	}
	instructions := instructionsFromThreads(threads)
	if instructions == nil {
		instructions = []instructionPayload{}
	}
	view := pendingPayload{Instructions: instructions}
	// THE SAME ANSWER WITH OR WITHOUT A SERVER. `galley pending` read offline
	// must not report a smaller round than the live one — that equivalence is
	// this repository's own promise, and a reviewer's deletion is exactly the
	// thing an agent must not be told about only sometimes. The store is on
	// disk beside the document, so nothing needs a server to read it.
	view.Changes, view.ChangesDropped = serve.ReviewerChanges(versions.Open(docPath))
	return view, nil
}

func instructionsFromThreads(threads []review.Thread) []instructionPayload {
	var out []instructionPayload
	for _, th := range threads {
		if th.Resolved {
			continue
		}
		for _, e := range th.Entries {
			if e.Author != review.AuthorCourt || strings.TrimSpace(e.Text) == "" {
				continue
			}
			out = append(out, instructionPayload{
				Key:  th.Key,
				Text: strings.TrimSpace(e.Text), Quote: strings.TrimSpace(th.Heading), At: e.At.UTC(),
			})
		}
	}
	return out
}

// newNoteThread is the ONE spelling of "a {>>note<<} the sidecar has never
// seen becomes a thread", for every offline path — `pending`, `delete`,
// `approve <thread-key>` and `reply`/`resolve`'s seedFileThreads.
//
// It exists because there were four spellings and they disagreed. Three passed
// `time.Time{}` and the fourth `time.Now()`, and while suggest.CommentKeyFor
// digested that instant the fourth minted a key the other three could not
// name: `galley pending` printed cb-f623…, `galley reply` answered "no thread
// with key cb-f623…" for it one command later, and neither matched what
// serve.NewEdit's importNotes minted when a reviewer opened the document. The
// key no longer digests the instant (see CommentKeyFor), so the four agree by
// construction now — this function is what keeps them agreeing.
//
// The instant is time.Now(): the file carries none, so first-sight is the only
// honest answer, and it is exactly what the live importNotes stamps. The
// author is the REVIEWER, for the reason ImportInlineComments gives — a marker
// found in a file was typed by whoever edits the file.
func newNoteThread(n suggest.NoteThread) review.Thread {
	return suggest.NewNoteThread(n, review.AuthorCourt, time.Now().UTC())
}

// --- galley revise ---

// newReviseFlags is galley revise's flag set: no flags of its own, but built
// the same way as every other migrated command's so NewFlags is uniform.
func newReviseFlags() *flag.FlagSet {
	return flag.NewFlagSet("revise", flag.ContinueOnError)
}

func runRevise(args []string, out, errw io.Writer) error {
	fs := newReviseFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	doc := pos[0]

	rt, ok := serve.FindRuntime(doc)
	if !ok {
		return errNoServer(doc)
	}
	resp, err := postReviseHTTP(rt.URL)
	if err != nil {
		return errNoServer(doc)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		fmt.Println("revision requested")
		return nil
	case http.StatusConflict:
		return reviseConflict(rt.URL, resp)
	case http.StatusNotImplemented:
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s (start `galley edit %s --on-revise '...'` to enable it)",
			strings.TrimSpace(string(msg)), doc)
	default:
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server rejected the revise request: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
}

// reviseConflict decides what a 409 from POST /_galley/revise MEANS, and it
// exists because this command used to assume there was only one thing it could
// mean.
//
// There was, once. handleRevise's single-flight guard answers 409 when a
// --on-revise command is still running, and that is genuinely not an error:
// work is already happening, which is what asking for a revision wanted. So
// `galley revise` printed "a revision is already in flight", returned nil, and
// exited 0 — for EVERY 409.
//
// The seal (#69) added a second, opposite one. `refuseSealed` answers 409 for a
// review that has ENDED: there is no next round to ask for and nothing was
// woken. Read as the first, it printed a reassuring sentence over a refusal and
// exited 0 — measured on a sealed fixture, and left as a comment at a
// deliberately-absent assertion in seal_test.go rather than fixed there.
//
// THE DISCRIMINATOR IS THE SERVER'S OWN ANSWER, NOT A SECOND COPY OF ITS RULE.
// Matching the refusal's wording here would be CLAUDE.md's "six agreeing
// spellings and one silence" with a string literal for a spelling: the seal's
// sentence would be pinned in two repositories' worth of places and the next
// 409 anyone adds would be silently misread again, exactly as this one was. GET
// /_galley/revise already publishes `sealed` and `running` — the same server,
// answering the same question it just answered with the status code — so the
// CLI asks it.
//
// A STATE READ THAT FAILS FALLS TO THE ERROR SIDE. The refusal is what the
// server actually said, and reporting it is never wrong; claiming success we
// cannot justify is. Only a positive "a revision is running" buys the
// reassuring sentence and the zero exit.
func reviseConflict(baseURL string, resp *http.Response) error {
	// The body is closed by runRevise's own defer, like every other branch of
	// its switch.
	raw, _ := io.ReadAll(resp.Body)
	said := strings.TrimSpace(string(raw))
	st, err := reviseState(baseURL)
	if err == nil && st.Running && !st.Sealed {
		fmt.Println("a revision is already in flight — it will act on whatever is pending when it finishes")
		return nil
	}
	if said == "" {
		said = "the server refused the revision request"
	}
	return errors.New(said)
}

// reviseStateView is the part of GET /_galley/revise this file reads. It is
// deliberately narrow: two facts, both of which the same handler uses to decide
// which 409 to answer with.
type reviseStateView struct {
	Running bool `json:"running"`
	Sealed  bool `json:"sealed"`
}

func reviseState(baseURL string) (reviseStateView, error) {
	resp, err := http.Get(baseURL + "/_galley/revise")
	if err != nil {
		return reviseStateView{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return reviseStateView{}, fmt.Errorf("revise state: %s", resp.Status)
	}
	var st reviseStateView
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return reviseStateView{}, err
	}
	return st, nil
}

// errNoServer is TERMINAL, and says so, because the shape it is read in is a
// retry loop. A review exists only while an editor is running, so every caller
// here — wait, ack, the edit-mode probes — is asking about something that does
// not exist yet rather than something running late. Measured: an agent told
// only "no server running" read it as transient and spun `galley wait` 360
// times against a document nobody had opened. Naming the command was not
// enough; the message has to refuse the retry.
func errNoServer(doc string) error {
	return fmt.Errorf("no server running for %s — a review exists only while `galley edit %s` is running, "+
		"so waiting or retrying will not start one; open it first", doc, doc)
}

// --- HTTP calls, factored out so they can be exercised against an
// httptest.Server directly, with no runtime file and no subprocess. Each one
// is exactly the request; interpreting the response (success, fall back
// offline, or surface the server's own error text) is the caller's job, the
// same division runReply/runResolve in main.go already use. ---

func getPendingHTTP(baseURL string) (*http.Response, error) {
	return http.Get(baseURL + "/_galley/pending")
}

func postReviseHTTP(baseURL string) (*http.Response, error) {
	return http.Post(baseURL+"/_galley/revise", "application/json", nil)
}

// decodeOK reads a 200 response's JSON body into v; a non-200 response is
// turned into an error carrying the server's own message (the same text a
// human running the live endpoint by hand would see), rather than a generic
// "request failed". Either way the body is closed here, so no caller has to
// remember to.
func decodeOK(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return errors.New(strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// writeFileAtomic is internal/serve's WriteFileAtomic under the name every
// caller in this file already uses. It is NOT a local copy any more: the
// atomic write has to preserve the target's permission bits and write through
// a symlink rather than replacing it, and two implementations of that would
// drift the first time one of them was fixed.
func writeFileAtomic(path string, raw []byte) error { return serve.WriteFileAtomic(path, raw) }
