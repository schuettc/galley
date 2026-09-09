// Command galley builds and serves the review a branch gets before it goes
// anywhere one-way — a force-push to an in-review PR, an upstream PR, an issue.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/serve"
	"github.com/schuettc/galley/internal/version"
	tools "github.com/schuettc/tools-common"
)

const usage = `^ galley — review before the one-way door.

Usage:
  galley serve <page.html> [flags]           serve a review page as a live document
  galley comments <page.html> [flags]        print the review conversation
  galley edit <doc.md|page.html> [flags]     serve a markdown document as a live, TipTap editor
  galley pending <doc.md> [flags]            list the reviewer's instructions for the open round
  galley cannot <doc.md> --why "…"           report the one case you could not do it
  galley revise <doc.md>                     wake whoever is listening — a waiter, or --on-revise
  galley ack <doc.md> --state <s> [--note]     tell the reviewer where their ask stands
  galley wait <doc.md> [flags]               block until the reviewer asks, then print what is pending
  galley round <doc.md> [--n N] [--json]     re-read a round already sent — for a session that restarted
  galley agent-prompt <doc.md>               print instructions for an agent about to answer the review
  galley channel [flags]                     MCP channel server — pushes review wakes into the session (see docs)
  galley ledger <sync|rebuild|stats>         galley's memory — decisions are logged as they are made;
                                               sync brings the per-user index up to date, stats reads it

  galley version                             print the build stamp

The READING commands speak --json — comments, pending, wait, round and
ledger — and that is where the machine-readable surface is; this
line used to claim every command did, and most did not.

pending goes through a running server when there is one, and falls back to the
file. The agent's revision is the file itself: edit the .md, then 'galley ack'.
revise has no offline form — it wakes whoever is
listening, a 'galley wait' session or --on-revise's command, and without a
server there is nothing to hand it to.

galley pushes and pulls. Push wakes someone who is not here:
  galley serve review.html --on-comment '<shell command>'
  galley edit doc.md --on-revise '<shell command>'

Pull is for the agent session that already IS here, holding the branch and the
discussion — it waits on the document instead of being spawned fresh:
  galley wait doc.md

Pull is the better loop and should be the default. When there is no session in
the room, push can spawn one — with the document and nothing else — and
'galley agent-prompt' is the text that tells it what to do:
  galley edit doc.md --on-revise 'claude -p "$(galley agent-prompt doc.md)"'

The channel is pull, packaged: 'galley channel' is an MCP server the agent
harness starts with the session. It attaches to every editor this session
opens — and any unowned one under its root — and pushes each Revise, settle
and Approve into the session as a channel event. No listener to remember.
  claude --channels plugin:galley@galley                         installed
  claude --dangerously-load-development-channels server:galley   development

The installed path comes from this repository's own marketplace manifest
(.claude-plugin/marketplace.json); the development path needs a project
.mcp.json running 'galley channel'. A channel needs a launch flag either
way — installing changes WHICH flag, not whether one is required. See
plugin/README.md.

Run 'galley <command> -h' for a command's flags.
`

func usageErr(fs *flag.FlagSet, err error) error {
	if !errors.Is(err, flag.ErrHelp) {
		fs.Usage()
	}
	return err
}

// splitPositional parses fs allowing flags before or after positionals
// (tools.SplitArgs), then requires exactly one positional (every galley verb
// takes exactly one document). It replaces parsePositional for commands
// registered with NewFlags/Synopsis/Help: -h is rendered by tools.App's HelpFor
// from those, so this no longer needs a SetUsage call or usageErr's
// fs.Usage()-on-parse-error handling of its own — tools.ParseFlags already
// renders help and wraps a bad flag as a UsageError.
func splitPositional(fs *flag.FlagSet, args []string, out io.Writer) ([]string, error) {
	flagArgs, pos := tools.SplitArgs(fs, args)
	if err := tools.ParseFlags(fs, flagArgs, out); err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, tools.UsageError{Msg: fmt.Sprintf("expected 1 argument(s), got %d", len(pos))}
	}
	return pos, nil
}

// Dispatch is the exported entrypoint cmd/galley's main() calls. It runs
// dispatch and then drains the decision log exactly once before returning the
// process exit code.
func Dispatch(args []string, out, errw io.Writer) int {
	code := dispatch(args, out, errw)
	// THE LAST THING, BEFORE ANY EXIT. Decisions are remembered on a background
	// goroutine so a file write can never sit in front of one; a CLI process
	// decides and exits within milliseconds, so without this drain the record
	// would leave with the process. It is bounded and it cannot fail — see
	// flushDecisions. dispatch() never itself calls os.Exit, so this drain runs
	// on every path before the one exit below.
	flushDecisions()
	flushDebug()
	return code
}

// dispatch handles galley's special top-level routes and otherwise routes
// through the shared tools.App. It returns the process exit code rather than
// exiting, so Dispatch can drain the decision log exactly once before exit.
//
// The three special routes are handled BEFORE app.Dispatch because tools.App's
// own conventions differ from galley's here: a no-args or `help` invocation
// prints the App's grouped usage to STDERR and exits 2, where galley prints its
// own `usage` const to STDOUT and exits 0; and the built-in `version` prints
// "galley <stamp>" where galley prints version.String() bare. (The registered
// version command below is overridden to match too, so `galley version` is
// correct whichever path reaches it.)
func dispatch(args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return 0
	case "version", "-v", "--version":
		_, _ = fmt.Fprintln(out, version.String())
		return 0
	}
	return newApp().Dispatch(args, out, errw)
}

// newApp builds the tools.App with every galley command registered. Summaries
// are short one-liners; per-command Synopsis/Help/NewFlags come in later tasks,
// and each command still self-handles its own flags (including -h) through its
// own flag set for now.
func newApp() *tools.App {
	app := tools.New(tools.Config{
		Name:   "galley",
		Domain: "galley.tools",
		Version: tools.Version{
			Number: version.Version(),
			Commit: version.Commit(),
			Date:   version.Date(),
		},
	})
	// Override the built-in version command: galley's output is version.String()
	// verbatim, not the family default "galley <stamp>".
	app.Register(tools.Command{
		Name:    "version",
		Summary: "print the build stamp",
		Run: func(_ []string, out, _ io.Writer) error {
			_, _ = fmt.Fprintln(out, version.String())
			return nil
		},
	})
	app.Register(tools.Command{
		Name:     "serve",
		Summary:  "serve a review page as a live document",
		Synopsis: "serve <page.html> [flags]",
		Help: "Serves a review page as a live document: the reviewer reads it in the\n" +
			"browser and leaves comments on it, and --on-comment runs once those\n" +
			"comments have sat unchanged for --quiet. Read them with `galley\n" +
			"comments <page.html>`.",
		NewFlags: func() *flag.FlagSet { fs, _ := newServeFlags(); return fs },
		Run:      runServe,
	})
	app.Register(tools.Command{
		Name:     "comments",
		Summary:  "print the review conversation",
		Synopsis: "comments <page.html> [flags]",
		Help: "Prints the conversation a served page has collected, read from its\n" +
			"projection file — so it works whether or not `galley serve` is running.",
		NewFlags: func() *flag.FlagSet { fs, _ := newCommentsFlags(); return fs },
		Run:      runComments,
	})
	app.Register(tools.Command{
		Name:     "edit",
		Summary:  "serve a markdown document as a live, TipTap editor",
		Synopsis: "edit <doc.md|page.html> [flags]",
		Help: "Revise wakes whoever is listening: a session blocked on `galley wait <doc>`\n" +
			"always, plus --on-revise's command if one is configured — refused only when\n" +
			"neither is present. A hook and a waiter are not alternatives; both fire if\n" +
			"both are there. A settle only reaches `galley wait` while the editor is in\n" +
			"● live mode (toggled in the browser), whether or not --on-settle is set.\n\n" +
			"The hook is told a round arrived, NOT to go read `galley pending` — a sent\n" +
			"round is no longer pending, so the woken agent runs `galley wait <doc>`,\n" +
			"which prints the captured round it was woken by.\n\n" +
			"--owner <session-id> binds the editor to a session whose channel is live: the\n" +
			"channel's galley_open tool passes it, and the editor stops when that session\n" +
			"ends. Without --owner the editor belongs to no session.\n\n" +
			"--on-revise example:\n" +
			"  galley edit doc.md --on-revise 'muster send <alias> " +
			"\"galley: revision requested — run: galley wait <doc>\" " +
			"--from galley-serve --intent action-requested && muster nudge <alias>'",
		NewFlags: func() *flag.FlagSet { fs, _ := newEditFlags(); return fs },
		Run:      runEdit,
	})
	app.Register(tools.Command{
		Name:     "pending",
		Summary:  "list the reviewer's instructions for the open round",
		Synopsis: "pending <doc.md> [flags]",
		Help: "List the reviewer's instructions for the OPEN round — the ones they have\n" +
			"left but not yet sent. A SENT round is no longer pending, so after a wake\n" +
			"this shows only newer, unsent work; `galley wait <doc>` is what re-reads\n" +
			"the round that woke you.\n\n" +
			"Goes through a running editor when there is one, and falls back to the\n" +
			"file.",
		NewFlags: func() *flag.FlagSet { fs, _ := newPendingFlags(); return fs },
		Run:      runPending,
	})
	app.Register(tools.Command{
		Name:     "cannot",
		Summary:  "report the one case you could not do it",
		Synopsis: "cannot <doc.md> --why \"what stopped you\"",
		Help: "For the one case a revision has no answer for: you genuinely could\n" +
			"not do what was asked. It is recorded as a round of its own — the document\n" +
			"unchanged, the reason beside the instruction — so the reviewer reads it where\n" +
			"they read everything else, and answers with a different instruction.\n\n" +
			"It is not a retry and not a refusal to be argued with. A reason is required:\n" +
			"a report with none is something the reviewer cannot act on.",
		NewFlags: func() *flag.FlagSet { fs, _ := newCannotFlags(); return fs },
		Run:      runCannot,
	})
	app.Register(tools.Command{
		Name:     "revise",
		Summary:  "wake whoever is listening",
		Synopsis: "revise <doc.md>",
		Help: "Sends the round and wakes whoever is listening for its instructions: a\n" +
			"session blocked on `galley wait <doc>` if one is there, and --on-revise's\n" +
			"command if `galley edit` was started with one. Refused only when neither\n" +
			"is — see `galley edit -h`. There is no offline equivalent: without a\n" +
			"server there is nothing to hand the work to.",
		NewFlags: newReviseFlags,
		Run:      runRevise,
	})
	app.Register(tools.Command{
		Name:     "ack",
		Summary:  "tell the reviewer where their ask stands",
		Synopsis: "ack <doc.md> --state <state> [--note \"…\"]",
		Help: "Tell the reviewer where their ask stands. received and working keep the\n" +
			"revising counter honest; answered, declined and failed close it with a\n" +
			"stated outcome. The revision itself goes into the document — edit the .md\n" +
			"directly with your normal file tools; this ack commits the round ONCE, for\n" +
			"the whole revision, never per save.\n\n" +
			"--changes annotates what you wrote. Galley owns the list of what changed and\n" +
			"matches your entries onto it; an entry whose quote it cannot place, or which\n" +
			"names a key it never sent, is dropped without an error.",
		NewFlags: func() *flag.FlagSet { fs, _ := newAckFlags(); return fs },
		Run:      runAck,
	})
	app.Register(tools.Command{
		Name:     "round",
		Summary:  "re-read a round already sent",
		Synopsis: "round <doc.md> [--n N] [--json]",
		Help: "Re-read a round that was already sent — the instructions, with their keys,\n" +
			"exactly as the agent was given them.\n\n" +
			"Without --n it prints the most recent round that ASKED for something, which\n" +
			"is the one an agent that restarted is trying to recover. --n names a round\n" +
			"by its number, whatever kind it is.",
		NewFlags: func() *flag.FlagSet { fs, _ := newRoundFlags(); return fs },
		Run:      runRound,
	})
	app.Register(tools.Command{
		Name:     "wait",
		Summary:  "block until the reviewer asks, then print what is pending",
		Synopsis: "wait <doc.md> [flags]",
		Help: "Blocks until the reviewer presses Revise, presses Approve, or — in ● live\n" +
			"mode — until the document settles, then prints the captured round. An\n" +
			"approve ends the review; the other wakes ask for an answer. A review that\n" +
			"has ALREADY ended is reported at once rather than waited on. There is no\n" +
			"offline form: without a running editor there is nothing to wait for in a\n" +
			"file.\n\n" +
			"Pass the fingerprint each wake prints back as --since on the next call, so\n" +
			"an event that arrives between two waits is caught up rather than lost. A\n" +
			"catch-up wakes with the reason `changed`, which names no event: the cursor\n" +
			"was behind and the document moved, and the editor does not record what\n" +
			"moved it — re-read everything and diff it against what you last saw.\n\n" +
			"Exit codes: 0 woke · 3 --timeout elapsed · 4 the editor stopped",
		NewFlags: func() *flag.FlagSet { fs, _ := newWaitFlags(); return fs },
		Run:      runWait,
	})
	app.Register(tools.Command{
		Name:     "agent-prompt",
		Summary:  "print instructions for an agent about to answer the review",
		Synopsis: "agent-prompt <doc.md>",
		Help: "Print instructions for an agent answering this document review.\n" +
			"The output is static; galley never calls a model.",
		NewFlags: newAgentPromptFlags,
		Run:      runAgentPrompt,
	})
	app.Register(tools.Command{
		Name:     "channel",
		Summary:  "MCP channel server — pushes review wakes into the session",
		Synopsis: "channel [--scope <dir>]",
		Help: "An MCP channel server on stdio. Registered in .mcp.json and named in\n" +
			"--channels, it opens editors for this session through its galley_open tool,\n" +
			"attaches to every editor this session owns, plus\n" +
			"any editor under --scope that no session owns at all, and pushes each\n" +
			"Revise, settle, Approve — and the editor going away —\n" +
			"into the session as a channel event. Every editor it does NOT attach to\n" +
			"is reported with its reason, on stderr and through the\n" +
			"galley_channel_status tool.",
		NewFlags: func() *flag.FlagSet { fs, _ := newChannelFlags(); return fs },
		Run:      runChannel,
	})
	// Summary-only, deliberately: tools.App intercepts -h anywhere in args for
	// any command carrying Synopsis/Help/NewFlags, so `galley ledger sync -h`
	// (and rebuild/stats) would render THIS command's top-level help and never
	// reach the sub-verb's own flag set. ledger owns a switch that already
	// handles -h/--help/help and hands off to a sub-verb's own flags — leaving
	// Synopsis/Help off here is what lets -h flow through to them instead of
	// being caught at the door.
	app.Register(tools.Command{
		Name:    "ledger",
		Summary: "galley's memory — sync, rebuild, stats",
		Run:     runLedger,
	})
	return app
}

// run is the single-command entry point the tests drive. It mirrors dispatch's
// special routes and otherwise calls the command's Run directly, so a caller
// gets the command's error back verbatim — flag.ErrHelp for `edit --help`, the
// unknown-command error for a removed verb. main() routes through app.Dispatch
// instead; run() is not on the process's exit path (flushing lives in main).
func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], os.Stdout, os.Stderr)
	case "comments":
		return runComments(args[1:], os.Stdout, os.Stderr)
	case "edit":
		return runEdit(args[1:], os.Stdout, os.Stderr)
	case "pending":
		return runPending(args[1:], os.Stdout, os.Stderr)
	case "cannot":
		return runCannot(args[1:], os.Stdout, os.Stderr)
	case "revise":
		return runRevise(args[1:], os.Stdout, os.Stderr)
	case "ack":
		return runAck(args[1:], os.Stdout, os.Stderr)
	case "round":
		return runRound(args[1:], os.Stdout, os.Stderr)
	case "wait":
		return runWait(args[1:], os.Stdout, os.Stderr)
	case "agent-prompt":
		return runAgentPrompt(args[1:], os.Stdout, os.Stderr)
	case "channel":
		return runChannel(args[1:], os.Stdout, os.Stderr)
	case "ledger":
		return runLedger(args[1:], os.Stdout, os.Stderr)
	case "version", "--version", "-v":
		fmt.Println(version.String())
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// serveFlags is galley serve's flag set, built by newServeFlags so the app
// registry (NewFlags) and runServe share one side-effect-free construction.
type serveFlags struct {
	port      *int
	comments  *string
	noOpen    *bool
	onComment *string
	quiet     *time.Duration
}

func newServeFlags() (*flag.FlagSet, *serveFlags) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	v := &serveFlags{
		port:      fs.Int("port", 0, "port to listen on (default: a free one; the URL is printed and the registry carries it)"),
		comments:  fs.String("comments", "", "projection file (default: <page>.comments.json)"),
		noOpen:    fs.Bool("no-open", false, "do not open a browser"),
		onComment: fs.String("on-comment", "", "shell command to run once comments settle (e.g. 'muster nudge galley')"),
		quiet:     fs.Duration("quiet", 8*time.Second, "how long comments must sit unchanged before --on-comment fires"),
	}
	return fs, v
}

func runServe(args []string, out, errw io.Writer) error {
	fs, v := newServeFlags()
	page, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	port, comments, noOpen, onComment, quiet := v.port, v.comments, v.noOpen, v.onComment, v.quiet

	srv, err := serve.New(page[0], *comments)
	if err != nil {
		return err
	}

	if *onComment != "" {
		srv.Notify = &serve.Notifier{
			Command: *onComment,
			Quiet:   *quiet,
			Log: func(line string) {
				fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), line)
			},
		}
		// Whatever the last session left behind is not news.
		srv.Notify.Seed(serve.Fingerprint(review.Read(srv.Doc())))
		defer srv.Notify.Stop()
	}

	ln, url, err := serve.Listen(fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if err := srv.Announce(url); err != nil {
		return fmt.Errorf("announce: %w", err)
	}
	defer srv.Withdraw()

	existing := review.Read(srv.Doc())
	fmt.Printf("page      %s\n", srv.PagePath)
	fmt.Printf("comments  %s\n", srv.CommentsPath)
	fmt.Printf("room      %s\n", srv.Room)
	fmt.Printf("serving   %s\n", url)
	if len(existing) > 0 {
		fmt.Printf("replayed  %d threads from the last session\n", len(existing))
	}
	if *onComment != "" {
		fmt.Printf("on-comment  %s  (after %s of quiet)\n", *onComment, *quiet)
	}
	fmt.Println("\nComments sync as you type. Ctrl-C to stop.")

	if !*noOpen {
		openBrowser(url)
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		// Flush synchronously: the debounced projection may still be pending,
		// and the reviewer's last sentence must not die with the process.
		if err := srv.Export(); err != nil {
			return fmt.Errorf("final export: %w", err)
		}
		// AFTER the export, never before: Close drops the peer connections and
		// stops the websocket server's idle sweeper, so anything still to be
		// written must already be written.
		if err := srv.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
		fmt.Printf("\n%d threads in %s\n", len(review.Read(srv.Doc())), srv.CommentsPath)
		return nil
	}
}

// commentsFlags is galley comments's flag set, built by newCommentsFlags so
// the app registry (NewFlags) and runComments share one construction.
type commentsFlags struct {
	file     *string
	asJSON   *bool
	openOnly *bool
}

func newCommentsFlags() (*flag.FlagSet, *commentsFlags) {
	fs := flag.NewFlagSet("comments", flag.ContinueOnError)
	v := &commentsFlags{
		file:     fs.String("comments", "", "projection file (default: <page>.comments.json)"),
		asJSON:   fs.Bool("json", false, "emit the projection verbatim"),
		openOnly: fs.Bool("open", false, "only threads that are unresolved"),
	}
	return fs, v
}

func runComments(args []string, out, errw io.Writer) error {
	fs, v := newCommentsFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	file, asJSON, openOnly := v.file, v.asJSON, v.openOnly
	page := pos[0]

	f, live, err := loadReview(page, *file)
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(f)
	}

	threads := f.Threads
	if *openOnly {
		var keep []review.Thread
		for _, t := range threads {
			if t.Open() {
				keep = append(keep, t)
			}
		}
		threads = keep
	}
	if len(threads) == 0 {
		fmt.Printf("no comments on %s\n", filepath.Base(page))
		return nil
	}

	source := "on disk"
	if live {
		source = "live"
	}
	fmt.Printf("%d threads on %s (%s)\n\n", len(threads), filepath.Base(page), source)
	for _, t := range threads {
		mark := " "
		if t.Resolved {
			mark = "✓"
		}
		fmt.Printf("%s %s   [%s]\n", mark, t.Heading, t.Key)
		for _, e := range t.Entries {
			who := "court"
			if e.Author == review.AuthorAgent {
				who = "claude"
			}
			for i, line := range strings.Split(e.Text, "\n") {
				prefix := fmt.Sprintf("  %-6s ", who)
				if i > 0 {
					prefix = "         "
				}
				fmt.Printf("%s%s\n", prefix, line)
			}
		}
		fmt.Println()
	}
	return nil
}

// loadReview prefers a running server, whose document is ahead of the debounced
// projection on disk.
func loadReview(page, file string) (review.File, bool, error) {
	if rt, ok := serve.FindRuntime(page); ok {
		resp, err := http.Get(rt.URL + "/_galley/threads")
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			var f review.File
			if err := json.NewDecoder(resp.Body).Decode(&f); err == nil {
				return f, true, nil
			}
		}
	}
	path := file
	if path == "" {
		abs, err := filepath.Abs(page)
		if err != nil {
			return review.File{}, false, err
		}
		path = serve.DefaultCommentsPath(abs)
	}
	f, err := serve.Load(path)
	return f, false, err
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	// Opening a browser is a convenience; failing to is not an error worth
	// stopping the server for.
	_ = cmd.Start()
}
