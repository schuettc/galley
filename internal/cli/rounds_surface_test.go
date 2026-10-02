package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRemovedWorkflowCommandsAreGone(t *testing.T) {
	for _, command := range []string{
		"approve", "decline", "reply", "resolve", "delete", "blocks",
		"suggest", "accept", "reject", "reopen", "discard",
		"serve", "comments",
	} {
		_, stderr, code := galleyCLI(t, command)
		if code != 2 || !strings.Contains(stderr, "unknown command") {
			t.Errorf("galley %s: exit %d, stderr %q, want unknown command", command, code, stderr)
		}
		if help, _, _ := galleyCLI(t, "help"); strings.Contains(help, "\n  "+command+" ") {
			t.Errorf("help still lists removed command %q", command)
		}
	}
}

func TestOfflinePendingUsesAnEmptyInstructionArray(t *testing.T) {
	doc := writeDoc(t, t.TempDir(), "clean.md", "# Clean\n\nNothing pending.\n")
	view, err := offlinePending(doc)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != `{"instructions":[]}` {
		t.Fatalf("offline pending = %s, want an empty instruction array", got)
	}
}
