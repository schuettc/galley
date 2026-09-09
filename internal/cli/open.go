// open.go is galley_open: the channel starting an editor for its own session.
//
// THE CHANNEL IS THE SESSION'S PRESENCE INSIDE GALLEY, so it is the one
// process that knows, with certainty and without reading an environment
// variable, which session a new editor belongs to. Before this tool the agent
// ran `galley edit` from its shell and the editor stamped whatever session id
// that shell carried — which under pi, after an in-process subagent had run,
// was the subagent's (measured 2026-09-08). The channel then refused the
// editor as another session's, correctly, and the reviewer typed into a
// document nobody was attached to. Spec: session identity at spawn.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
)

// openResult is what galley_open returns, as JSON text: enough for the agent
// to hand the reviewer a URL and to recognise the document in later wakes.
type openResult struct {
	URL  string `json:"url"`
	Room string `json:"room"`
	Page string `json:"page"`
}

func (r openResult) text() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// samePage is path equality in BOTH spellings, for the reason inScope compares
// both: /tmp is /private/tmp on macOS, and the editor's advert and the tool's
// argument are typed by different hands.
func samePage(a, b string) bool {
	if a == b {
		return true
	}
	return resolveSymlinks(a) == resolveSymlinks(b)
}

// open is the tool body. Resolution order, and it is the order the spec fixes:
// in scope; already open by this session (return it); open and unowned
// (claim it — the human-terminal editor being adopted, the same adoption the
// scan performs); open by another LIVE session (theirs, refuse); open by a
// session that has stopped (its editor is going down on its own — see
// watchOwnerSession — refuse and say retry); otherwise spawn.
func (c *channel) open(doc string) (string, error) {
	if doc == "" {
		return "", fmt.Errorf("galley_open needs a document path")
	}
	abs, err := filepath.Abs(doc)
	if err != nil {
		return "", err
	}
	if !c.inScope(abs) {
		return "", fmt.Errorf("%s is outside this channel's scope %s", abs, c.scope)
	}
	// WHAT THE EDITOR ADVERTISES IS NOT WHAT IT IS STARTED WITH. `galley edit
	// page.html` opens the markdown editor on the prose it extracts, so its
	// advert — and the registry entry every wake is matched against — names
	// .galley/pages/<base>/content.md. Resolution therefore matches on adv
	// while the spawn argument and the log token stay keyed on the input: the
	// child is still told to open the page, and the log is still named after
	// what the agent asked for.
	adv := serve.AdvertisedPath(abs)
	if r, found, err := c.findOpen(adv); err != nil || found {
		if err != nil {
			return "", err
		}
		return r.text()
	}
	r, err := c.spawnEditor(abs, adv)
	if err != nil {
		return "", err
	}
	return r.text()
}

// findOpen answers "is this document already being served, and by whom".
// found=false with a nil error means nothing is open and the caller spawns.
// It takes the ADVERTISED path (see open), never the path the tool was given.
func (c *channel) findOpen(adv string) (openResult, bool, error) {
	entries, _, err := registry.Inspect()
	if err != nil {
		return openResult{}, false, fmt.Errorf("cannot read the live registry: %w", err)
	}
	for _, e := range entries {
		if !samePage(e.Page, adv) {
			continue
		}
		r := openResult{URL: e.URL, Room: e.Room, Page: e.Page}
		switch {
		case e.Owner == c.self:
			return r, true, nil
		case e.Owner == "":
			// A channel with no session id has nothing to stamp; the advert
			// stays unowned and the scan attaches to it as before.
			if c.self == "" {
				return r, true, nil
			}
			claimed, err := registry.Claim(e.Room, c.self)
			if err != nil {
				return openResult{}, false, fmt.Errorf("cannot claim %s: %w", e.Page, err)
			}
			if !claimed {
				return openResult{}, false, fmt.Errorf("%s is open, but another live session claimed it first; its wakes go there", e.Page)
			}
			return r, true, nil
		case registry.SessionLive(e.Owner):
			return openResult{}, false, fmt.Errorf("open in session %s; its wakes go there", e.Owner)
		default:
			return openResult{}, false, fmt.Errorf("opened by session %s, which has stopped; "+
				"its editor is shutting down, retry in a few seconds", e.Owner)
		}
	}
	return openResult{}, false, nil
}

// spawnEditor starts `galley edit <abs> --no-open --owner <self>` and waits
// for its advert.
//
// DETACHED, AND ON PURPOSE IN EVERY PARTICULAR. Setsid puts the editor in its
// own session and process group, so a channel that restarts (the MCP server
// re-spawns within a living session) does not take the review down with it —
// the editor's own owner watch decides when to stop, on the session's
// presence, with a six-second debounce that rides out exactly that restart.
// Stdin is /dev/null: nothing will ever write to it, and an inherited pipe is
// how a detached process ends up blocked or killed on its parent's fd. Stdout
// and stderr go to a log named after the page, truncated on each open, so a
// startup failure has somewhere to be read from.
//
// This process does NOT hold the child. cmd.Wait runs on its own goroutine so
// the zombie is reaped when the editor eventually exits; the tool returns as
// soon as the advert appears.
func (c *channel) spawnEditor(abs, adv string) (openResult, error) {
	if c.exe == "" {
		return openResult{}, fmt.Errorf("cannot locate the galley binary to start an editor with")
	}
	logDir, err := registry.LogDir()
	if err != nil {
		return openResult{}, fmt.Errorf("cannot create the editor log directory: %w", err)
	}
	logPath := filepath.Join(logDir, registry.Token(abs)+".log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return openResult{}, fmt.Errorf("cannot open the editor log %s: %w", logPath, err)
	}
	args := []string{"edit", abs, "--no-open"}
	if c.self != "" {
		args = append(args, "--owner", c.self)
	}
	cmd := exec.Command(c.exe, args...)
	cmd.Stdin = nil // /dev/null
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return openResult{}, fmt.Errorf("cannot start galley edit: %w", err)
	}
	_ = logf.Close() // the child holds its own descriptor
	fmt.Fprintf(os.Stderr, "[galley channel] started editor pid=%d for %s (log %s)\n", cmd.Process.Pid, abs, logPath)

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(c.openTimeout)
	tick := time.NewTicker(c.openPoll)
	defer tick.Stop()
	for {
		select {
		case waitErr := <-exited:
			status := "exited"
			if waitErr != nil {
				status = waitErr.Error() // "exit status N"
			}
			return openResult{}, fmt.Errorf("galley edit %s before advertising (%s):\n%s", status, logPath, tailLines(logPath, 20))
		case <-deadline:
			return openResult{}, fmt.Errorf("galley edit started (pid %d) but advertised nothing within %s; see %s",
				cmd.Process.Pid, c.openTimeout, logPath)
		case <-tick.C:
			if r, ok := c.ownAdvert(adv); ok {
				return r, nil
			}
		}
	}
}

// ownAdvert reports the advert for the ADVERTISED path that belongs to this
// session — the one the editor just spawned writes once it is listening. A
// channel with no session id spawned an unowned editor and looks for an
// unowned advert.
func (c *channel) ownAdvert(adv string) (openResult, bool) {
	entries, _, err := registry.Inspect()
	if err != nil {
		return openResult{}, false
	}
	for _, e := range entries {
		if samePage(e.Page, adv) && e.Owner == c.self {
			return openResult{URL: e.URL, Room: e.Room, Page: e.Page}, true
		}
	}
	return openResult{}, false
}

// tailLines is the last n lines of a file, or "" when it cannot be read — the
// editor's log is best-effort context on an error, never the error itself.
func tailLines(path string, n int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
