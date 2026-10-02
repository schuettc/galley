package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// `galley help` prints galley's guidance (tools.Config.About) and then the
// commands under galley's three groups, on stdout, exit 0.
func TestHelpShowsGuidanceAndGroups(t *testing.T) {
	stdout, _, code := galleyCLI(t, "help")
	if code != 0 {
		t.Fatalf("help exited %d", code)
	}
	for _, want := range []string{
		"galley — review before the one-way door.",
		"Pull is the better loop",
		"Review\n", "Agent\n", "Record\n",
		"usage: galley",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help lacks %q:\n%s", want, stdout)
		}
	}
	for _, spelling := range []string{"-h", "--help"} {
		if out, _, code := galleyCLI(t, spelling); code != 0 || out != stdout {
			t.Fatalf("%s differs from help (exit %d)", spelling, code)
		}
	}
}

// Every galley command carries a synopsis and help text for `help <cmd>`,
// `-h` and `man`; the built-ins are tools.App's.
func TestEveryCommandHasHelp(t *testing.T) {
	var out, errw strings.Builder
	if code := newApp().Dispatch([]string{"commands", "--json"}, &out, &errw); code != 0 {
		t.Fatalf("commands --json exited %d", code)
	}
	var cmds []struct {
		Name, Synopsis, Help string
	}
	if err := json.Unmarshal([]byte(out.String()), &cmds); err != nil {
		t.Fatal(err)
	}
	builtin := map[string]bool{"help": true, "version": true, "update": true, "man": true, "commands": true}
	for _, c := range cmds {
		if builtin[c.Name] {
			continue
		}
		if c.Synopsis == "" || c.Help == "" {
			t.Errorf("%s: synopsis %q, help %d chars; both are required", c.Name, c.Synopsis, len(c.Help))
		}
	}
}

// Task 5's coverage check: `galley commands --json` must parse and list every
// command this binary actually dispatches — the mutating verbs, the reading
// verbs, and the four built-ins tools.App registers itself (help, man,
// commands, version — "update" is deliberately excluded below, since galley
// does not ship a self-update path worth asserting on here). A command
// missing from this list is a command `commands --json` silently dropped,
// which is exactly the drift `galley agent-prompt` and any external tooling
// built on the machine-readable index would inherit without warning.
func TestCommandsJSONListsEveryCommand(t *testing.T) {
	app := newApp()
	var out, errw strings.Builder
	code := app.Dispatch([]string{"commands", "--json"}, &out, &errw)
	if code != 0 {
		t.Fatalf("commands --json exited %d, stderr: %s", code, errw.String())
	}

	var cmds []struct {
		Name     string `json:"name"`
		Synopsis string `json:"synopsis"`
		Summary  string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out.String()), &cmds); err != nil {
		t.Fatalf("commands --json did not parse as JSON: %v\noutput:\n%s", err, out.String())
	}

	got := map[string]bool{}
	for _, c := range cmds {
		if c.Name == "" {
			t.Errorf("a command entry has no name: %+v", c)
		}
		got[c.Name] = true
	}

	want := []string{
		// galley's own mutating and reading verbs.
		"edit", "pending", "cannot", "revise", "ack",
		"round", "wait", "agent-prompt", "channel", "ledger",
		// built-ins tools.App registers on every app.
		"help", "man", "commands", "version",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("commands --json is missing %q\noutput:\n%s", name, out.String())
		}
	}
	if len(cmds) < len(want) {
		t.Errorf("commands --json listed %d commands, want at least %d", len(cmds), len(want))
	}
}

// `galley man` must render a real roff man page — the one thing every galley
// invocation of `man galley` (were it installed) would show.
func TestManRendersARoffPage(t *testing.T) {
	app := newApp()
	var out, errw strings.Builder
	code := app.Dispatch([]string{"man"}, &out, &errw)
	if code != 0 {
		t.Fatalf("man exited %d, stderr: %s", code, errw.String())
	}
	if !strings.Contains(out.String(), ".TH GALLEY 1") {
		t.Fatalf("man output does not contain the roff title header:\n%s", out.String())
	}
	// Spot-check that a real command's synopsis made it into the page, so this
	// isn't just asserting on the fixed header/footer boilerplate.
	if !strings.Contains(out.String(), "channel") {
		t.Errorf("man output does not mention the channel command:\n%s", out.String())
	}
}

// ledger owns its sub-verbs: `ledger -h` is ledger's help, `ledger stats -h`
// reaches stats, and a mistyped sub-verb is an error even with -h, never a
// silent exit 0 with the parent's help.
func TestLedgerSubverbHelp(t *testing.T) {
	if out, _, code := galleyCLI(t, "ledger", "-h"); code != 0 || !strings.Contains(out, "subcommands: sync, rebuild, stats") {
		t.Fatalf("ledger -h: exit %d, %q", code, out)
	}
	if _, stderr, code := galleyCLI(t, "ledger", "bogus", "-h"); code == 0 || !strings.Contains(stderr, `unknown ledger command "bogus"`) {
		t.Fatalf("ledger bogus -h: exit %d, stderr %q; want the unknown-sub-verb error", code, stderr)
	}
}
