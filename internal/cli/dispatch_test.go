package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/schuettc/galley/internal/version"
	tools "github.com/schuettc/tools-common"
)

// galley's help, version and unknown-command handling are tools.App's (the
// family behaviour); galley supplies its commands, groups and About text.

// Bare `galley` is a usage error: the short grouped usage on stderr, exit 2.
func TestBareGalleyIsUsageError(t *testing.T) {
	stdout, stderr, code := galleyCLI(t)
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "usage: galley") {
		t.Fatalf("bare galley: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// `galley version`, `-v` and `--version` print the family format.
func TestVersionIsFamilyFormat(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		stdout, _, code := galleyCLI(t, arg)
		if code != 0 || strings.TrimSpace(stdout) != "galley "+version.String() {
			t.Fatalf("%s: exit %d, printed %q, want %q", arg, code, stdout, "galley "+version.String())
		}
	}
}

// An unknown command is a usage error that names the command.
func TestUnknownCommandIsAnError(t *testing.T) {
	_, stderr, code := galleyCLI(t, "bogus")
	if code != 2 || !strings.Contains(stderr, `unknown command "bogus"`) {
		t.Fatalf("bogus: exit %d, stderr %q", code, stderr)
	}
}

// The wait sentinels map to the exit codes a `while galley wait …; do` loop
// branches on, carried as *tools.ExitError so Dispatch renders the code. Driven
// directly with the sentinels — no live server needed.
func TestWaitExitMapsSentinelsToExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   error
		code int
	}{
		{"timeout", errWaitTimeout, 3},
		{"stopped", errWaitStopped, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := waitExit(tc.in)
			var ee *tools.ExitError
			if !errors.As(got, &ee) {
				t.Fatalf("waitExit(%v) = %T, want *tools.ExitError", tc.in, got)
			}
			if ee.Code != tc.code {
				t.Fatalf("waitExit(%v).Code = %d, want %d", tc.in, ee.Code, tc.code)
			}
		})
	}
}

// A non-sentinel error passes through waitExit untouched — errNoServer and an
// unknown wake reason are plain exit-1 errors, not *ExitError.
func TestWaitExitPassesOtherErrorsThrough(t *testing.T) {
	plain := errors.New("no server running")
	got := waitExit(plain)
	if !errors.Is(got, plain) {
		t.Fatalf("waitExit(plain) = %v, want the error unchanged", got)
	}
	var ee *tools.ExitError
	if errors.As(got, &ee) {
		t.Fatalf("a plain error should not become an *ExitError")
	}
}

// newApp registers every galley command plus the overridden version, and
// Dispatch routes an unknown command to a usage error (exit 2).
func TestNewAppRegistersCommands(t *testing.T) {
	app := newApp()
	var out, errw strings.Builder
	if code := app.Dispatch([]string{"nope"}, &out, &errw); code != 2 {
		t.Fatalf("Dispatch(nope) = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(errw.String(), "unknown command") {
		t.Fatalf("Dispatch(nope) stderr = %q, want an unknown-command line", errw.String())
	}
}
