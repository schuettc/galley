// Command galley builds and serves the review a branch gets before it goes
// anywhere one-way — a force-push to an in-review PR, an upstream PR, an issue.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/schuettc/galley/internal/version"
	tools "github.com/schuettc/tools-common"
	"github.com/schuettc/tools-common/localweb"
)

// about is galley's overview, shown by `galley help` above the generated
// command list and used as the man page DESCRIPTION (tools.Config.About).
// The per-command lines it used to carry are generated from the registry now,
// so they can no longer drift from what galley actually dispatches.
const about = `^ galley — review before the one-way door.

The READING commands speak --json — pending, wait, round and ledger —
and that is where the machine-readable surface is; this line used to
claim every command did, and most did not.

pending goes through a running server when there is one, and falls back to the
file. The agent's revision is the file itself: edit the .md, then 'galley ack'.
revise has no offline form — it wakes whoever is
listening, a 'galley wait' session or --on-revise's command, and without a
server there is nothing to hand it to.

galley pushes and pulls. Push wakes someone who is not here:
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

// dispatch routes every invocation through the shared tools.App: help,
// version, man, commands and unknown-command handling are the family's; galley
// supplies its commands, groups and About text. It returns the exit code
// rather than exiting, so Dispatch can drain the decision log exactly once.
func dispatch(args []string, out, errw io.Writer) int {
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
		About:  about,
		Groups: []tools.Group{
			{Key: "review", Heading: "Review"},
			{Key: "agent", Heading: "Agent"},
			{Key: "record", Heading: "Record"},
		},
		Version: tools.Version{
			Number: version.Version(),
			Commit: version.Commit(),
			Date:   version.Date(),
		},
	})
	app.Register(tools.Command{
		Name:     "edit",
		Group:    "review",
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
			"--from galley --intent action-requested && muster nudge <alias>'",
		NewFlags: func() *flag.FlagSet { fs, _ := newEditFlags(); return fs },
		Run:      runEdit,
	})
	app.Register(tools.Command{
		Name:     "pending",
		Group:    "review",
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
		Group:    "review",
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
		Group:    "review",
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
		Group:    "review",
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
		Group:    "review",
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
		Group:    "review",
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
		Group:    "agent",
		Summary:  "print instructions for an agent about to answer the review",
		Synopsis: "agent-prompt <doc.md>",
		Help: "Print instructions for an agent answering this document review.\n" +
			"The output is static; galley never calls a model.",
		NewFlags: newAgentPromptFlags,
		Run:      runAgentPrompt,
	})
	app.Register(tools.Command{
		Name:     "channel",
		Group:    "agent",
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
	// ledger dispatches its own sub-verbs; declaring them as Subcommands lets
	// `galley ledger -h` show this help while `galley ledger sync -h` reaches
	// the sub-verb's own flag set.
	app.Register(tools.Command{
		Name:        "ledger",
		Group:       "record",
		Summary:     "galley's memory — sync, rebuild, stats",
		Synopsis:    "ledger <sync|rebuild|stats> [flags]",
		Help:        strings.TrimPrefix(ledgerUsage, "Usage: galley ledger <sync|rebuild|stats> [flags]\n\n"),
		Subcommands: []string{"sync", "rebuild", "stats"},
		Run:         runLedger,
	})
	return app
}

func openBrowser(url string) {
	// Opening a browser is a convenience; failing to is not an error worth
	// stopping the server for.
	_ = localweb.OpenBrowser(url)
}
