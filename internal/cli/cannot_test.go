package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/versions"
)

// The exception has an offline form now: with no server there is still a
// round to record — the document unchanged, the reason beside it — because a
// report that vanishes when nobody is serving is a report the reviewer never
// reads.
func TestCannotRecordsARoundOffline(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("# T\n\nProse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _ := captureOutput(t, func() {
		if err := runCannot([]string{doc, "--why", "the source it cites is gone"}, os.Stdout, os.Stderr); err != nil {
			t.Fatalf("offline cannot: %v", err)
		}
	})
	if !strings.Contains(stdout, "no server running") {
		t.Errorf("offline cannot did not say it ran offline: %q", stdout)
	}
	rounds, err := versions.Open(doc).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) == 0 {
		t.Fatal("no round recorded")
	}
	last := rounds[len(rounds)-1]
	if last.Reason != versions.ReasonCouldNot {
		t.Fatalf("last round reason %q, want could-not", last.Reason)
	}
	if len(last.Authors) != 1 || last.Authors[0] != versions.AuthorAgent {
		t.Fatalf("authors: %v", last.Authors)
	}
	if !strings.Contains(last.Instruction, "the source it cites is gone") {
		t.Fatalf("the reason is not on the round: %q", last.Instruction)
	}
	content, err := versions.Open(doc).Content(last.N)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "Prose.") {
		t.Fatalf("the round's content is not the document: %q", content)
	}
}
