package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE PLUGIN SKILL IS THE THIRD CARRIER, AND IT IS THE ONE PEOPLE ACTUALLY
// READ. The protocol already had two declarations of itself — the MCP
// handshake's `channelInstructions` and the cold-start `agentPromptText` —
// bound together, now by `TestEveryCarrierTeachesTheProtocol` in
// protocolclaims_test.go, for the reason this repository records more often
// than any other: a rule corrected in one carrier goes on being wrong in the
// other for as long as nothing drives it.
//
// `plugin/galley/skills/reviewing-a-document/SKILL.md` is a third, and it was
// bound by nothing. That is not a symmetry argument. Court: *"we never
// actually use the cli. we want to use this through claude code."* The two Go
// carriers are strings in this binary and are exercised by every test in this
// package; the skill is a file in another directory that no Go test opened,
// and it is the copy a paired session loads before it does anything. The
// carrier nobody checks is the carrier everybody uses.
//
// It is held to the SAME claims as the other two rather than to a list of its
// own — a third list is a third thing to drift.
func pluginSkill(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "plugin", "galley", "skills",
		"reviewing-a-document", "SKILL.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the plugin skill is a carrier of this protocol and must be readable: %v", err)
	}
	return string(b)
}

// EVERY VERB THE SKILL NAMES MUST EXIST. `galley suggest` was deleted and four
// browser gates went on referencing it for weeks, green the whole time because
// nobody ran them. A skill naming a dead verb fails the same way and worse:
// there is no gate to go red, only a session running a command that is not
// there, mid-review, in front of the reviewer.
func TestThePluginSkillNamesOnlyRealVerbs(t *testing.T) {
	skill := pluginSkill(t)
	real := realCommands(t)
	// The prose says "galley watches the file", "galley computes", "galley
	// refuses" — English, not commands. A verb is only claimed as one where it
	// is inside a fenced block or inline code, which is how the skill spells
	// every command it actually wants run.
	// Odd chunks only: splitting on a backtick puts code spans at odd indices
	// and prose at even ones. Walking every chunk reads the prose too, and the
	// skill says "galley computes the real change list" — English about what
	// the program does, not a command anybody runs.
	chunks := strings.Split(skill, "`")
	for i := 1; i < len(chunks); i += 2 {
		chunk := chunks[i]
		fields := strings.Fields(chunk)
		if len(fields) < 2 || fields[0] != "galley" {
			continue
		}
		verb := fields[1]
		if !real[verb] {
			t.Errorf("the plugin skill tells the agent to run %q, which is not a command", "galley "+verb)
		}
	}
}

// realCommands reads the command list from the registry galley actually
// dispatches (`commands --json`, names and aliases), rather than keeping a
// second copy of it here. A hand-maintained list would agree with the binary
// right up until somebody adds or removes a verb, which is the one moment this
// check exists for.
func realCommands(t *testing.T) map[string]bool {
	t.Helper()
	var out, errw strings.Builder
	if code := newApp().Dispatch([]string{"commands", "--json"}, &out, &errw); code != 0 {
		t.Fatalf("commands --json exited %d: %s", code, errw.String())
	}
	var cmds []struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}
	if err := json.Unmarshal([]byte(out.String()), &cmds); err != nil {
		t.Fatal(err)
	}
	real := map[string]bool{}
	for _, c := range cmds {
		real[c.Name] = true
		for _, a := range c.Aliases {
			real[a] = true
		}
	}
	if len(real) < 10 {
		t.Fatalf("only %d commands in the registry; the index is broken", len(real))
	}
	return real
}
