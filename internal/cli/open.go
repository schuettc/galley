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
	"path/filepath"

	"github.com/schuettc/galley/internal/registry"
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
	if r, found, err := c.findOpen(abs); err != nil || found {
		if err != nil {
			return "", err
		}
		return r.text()
	}
	r, err := c.spawnEditor(abs)
	if err != nil {
		return "", err
	}
	return r.text()
}

// findOpen answers "is this document already being served, and by whom".
// found=false with a nil error means nothing is open and the caller spawns.
func (c *channel) findOpen(abs string) (openResult, bool, error) {
	entries, _, err := registry.Inspect()
	if err != nil {
		return openResult{}, false, fmt.Errorf("cannot read the live registry: %w", err)
	}
	for _, e := range entries {
		if !samePage(e.Page, abs) {
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

// spawnEditor is implemented in the next task.
func (c *channel) spawnEditor(abs string) (openResult, error) {
	return openResult{}, fmt.Errorf("galley_open: spawning %s is not implemented yet", abs)
}
