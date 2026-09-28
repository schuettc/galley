package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const promptDoc = "notes/review-me.md"

func TestAgentPromptInterpolatesTheDocumentAndIsAgentNeutral(t *testing.T) {
	got := agentPrompt(promptDoc)
	if !strings.Contains(got, "galley ack "+promptDoc) || strings.Contains(got, docPlaceholder) {
		t.Fatalf("document was not interpolated:\n%s", got)
	}
	lower := strings.ToLower(got)
	for _, vendor := range []string{"claude", "anthropic", "codex", "openai", "cursor", "gemini", "copilot"} {
		if strings.Contains(lower, vendor) {
			t.Errorf("prompt names vendor %q", vendor)
		}
	}
}

func TestAgentPromptPageBackedTellsTheAgentItMayRestructureTheHTML(t *testing.T) {
	// A page-backed review (the doc lives under .galley/pages/) gains the
	// pageBacked appendix, which now tells the agent it may edit either layer:
	// content.md for wording, the page's .html directly for structure — the
	// opposite of the old "markdown only" prohibition.
	pageDoc := filepath.Join("proj", ".galley", "pages", "launch", "content.md")
	got := agentPrompt(pageDoc)
	for _, want := range []string{
		"edit the page's .html directly",
		"Galley re-extracts the page after the round",
		"the reviewer's editor reloads on it",
		"markers re-number every round",
		"match only against the markers in the file you were just handed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("page-backed prompt is missing %q:\n%s", want, got)
		}
	}
	// The old, now-false claims must be gone.
	for _, stale := range []string{
		"You edit ONLY the markdown",
		"changes the wrong layer",
		"galley will refuse to overwrite it",
	} {
		if strings.Contains(got, stale) {
			t.Fatalf("page-backed prompt still contains the stale claim %q:\n%s", stale, got)
		}
	}
	// A plain markdown review gets no such appendix.
	if plain := agentPrompt(promptDoc); strings.Contains(plain, "edit the page's .html directly") {
		t.Fatal("the page-backed appendix leaked into a non-page-backed prompt")
	}
}

func TestAgentPromptRefusesADocumentItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.md")
	stdout, _ := captureOutput(t, func() {
		if err := runAgentPrompt([]string{missing}, os.Stdout, os.Stderr); err == nil {
			t.Error("agent-prompt accepted a missing document")
		}
	})
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("failed prompt wrote stdout: %q", stdout)
	}
}

func TestAgentPromptIsFreeAndDoesNotStartTheTrial(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GALLEY_CONFIG_DIR", dir)
	doc := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(doc, []byte("# T\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := galleyCLI(t, "agent-prompt", doc)
	if code != 0 {
		t.Fatalf("agent-prompt exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "galley ack "+doc) {
		t.Fatalf("no prompt on stdout: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "trial")); !os.IsNotExist(err) {
		t.Fatalf("agent-prompt started the trial: %v", err)
	}
}
