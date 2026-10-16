package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates the whole package's test run from the operator's real
// config dir and browser. Tests that need a specific dir still set their own
// GALLEY_CONFIG_DIR with t.Setenv, which shadows this baseline.
func TestMain(m *testing.M) {
	code := func() int {
		dir, err := os.MkdirTemp("", "galley-cmd-test-config")
		if err != nil {
			fmt.Fprintln(os.Stderr, "TestMain: mkdtemp:", err)
			return 1
		}
		defer func() { _ = os.RemoveAll(dir) }()

		// A do-nothing `open` and `xdg-open` first on PATH: galley_open opens
		// the review page, in this process or in the editor it spawns (which
		// inherits PATH), and a test must not open tabs on the machine running it.
		bin, err := os.MkdirTemp("", "galley-cmd-test-bin")
		if err != nil {
			fmt.Fprintln(os.Stderr, "TestMain: mkdtemp:", err)
			return 1
		}
		defer func() { _ = os.RemoveAll(bin) }()
		for _, name := range []string{"open", "xdg-open"} {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "TestMain: write", name+":", err)
				return 1
			}
		}
		if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
			fmt.Fprintln(os.Stderr, "TestMain: setenv:", err)
			return 1
		}

		oldDir, hadDir := os.LookupEnv("GALLEY_CONFIG_DIR")
		if err := os.Setenv("GALLEY_CONFIG_DIR", dir); err != nil {
			fmt.Fprintln(os.Stderr, "TestMain: setenv:", err)
			return 1
		}
		defer func() {
			if hadDir {
				_ = os.Setenv("GALLEY_CONFIG_DIR", oldDir)
			} else {
				_ = os.Unsetenv("GALLEY_CONFIG_DIR")
			}
		}()

		return m.Run()
	}()
	os.Exit(code)
}

// captureOutput runs fn with os.Stdout/os.Stderr redirected and returns what
// each received.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	if err := outW.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errW.Close(); err != nil {
		t.Fatal(err)
	}
	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatal(err)
	}
	return string(outBytes), string(errBytes)
}

func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// galleyCLI runs galley exactly as the binary does (dispatch, the tools.App
// path) and returns what it printed and its exit code.
func galleyCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errw strings.Builder
	code = dispatch(args, &out, &errw)
	return out.String(), errw.String(), code
}
