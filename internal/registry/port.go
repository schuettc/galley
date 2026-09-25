package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	tools "github.com/schuettc/tools-common"
)

// Port reuse — galley-edits friction #5. `galley edit` binds a fresh random
// port on every start, so a restart (to pick up an .html rewrite, say) moves
// the URL and 404s the reviewer's open tab on its next action. The last port a
// document served on is remembered here so the next start can offer it back;
// the tab survives a restart without anyone pinning --port.
//
// The hint lives UNDER registry.Dir() (~/.galley/live/ports), NOT beside the
// document: it is machine state, not review state, and keeping it out of the
// working tree means no .gitignore entry and no file the reviewer sees. It is
// keyed by the document's ABSOLUTE PATH — the server's Room carries a
// per-launch instance token and is deliberately unstable, so it cannot key a
// fact that must outlive the launch that wrote it.
//
// It sits in a `ports` SUBDIRECTORY for the same reason session presence does
// (see sessionsDir): List/Inspect glob "*.json" in Dir itself and do not
// recurse, so a hint in Dir's root would be read as an advert, fail Validate
// for its missing room, and be reported as a refused editor on every scan. A
// subdirectory is invisible to that glob.
//
// It is only ever a HINT. The caller tries the port and falls back to a free
// one if it is taken, so a stale entry — the port claimed by something else
// since — costs one failed bind, never a refusal to serve.

type portHint struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// portsDir is a SUBDIRECTORY of Dir, kept out of the advert glob — see the
// package comment on portHintPath and the identical sessionsDir.
func portsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, "ports")
	return sub, os.MkdirAll(sub, 0o700)
}

// portHintPath names the hint file for a document. The absolute path is hashed
// so the filename is fixed-length and carries no separators; the path is stored
// inside the file too, so a hash collision is caught on read rather than
// handing back another document's port.
func portHintPath(docAbsPath string) (string, error) {
	dir, err := portsDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(docAbsPath))
	return filepath.Join(dir, "port-"+hex.EncodeToString(sum[:])[:16]+".json"), nil
}

// LastPort returns the port a document last served on, if one was remembered
// and the record is for this exact document. Any error — no hint, unreadable
// bytes, a hash collision, a nonsense port — is reported as "no hint" so the
// caller simply binds a free port.
func LastPort(docAbsPath string) (int, bool) {
	abs, err := filepath.Abs(docAbsPath)
	if err != nil {
		return 0, false
	}
	name, err := portHintPath(abs)
	if err != nil {
		return 0, false
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return 0, false
	}
	var h portHint
	if err := json.Unmarshal(raw, &h); err != nil {
		return 0, false
	}
	if h.Path != abs || h.Port < 1 || h.Port > 65535 {
		return 0, false
	}
	return h.Port, true
}

// SaveLastPort remembers the port a document is now serving on. A failure to
// write is silently dropped: the reuse is an optimisation, and a review that
// cannot persist a hint still serves.
func SaveLastPort(docAbsPath string, port int) {
	abs, err := filepath.Abs(docAbsPath)
	if err != nil {
		return
	}
	name, err := portHintPath(abs)
	if err != nil {
		return
	}
	raw, err := json.Marshal(portHint{Path: abs, Port: port})
	if err != nil {
		return
	}
	_ = tools.WriteFileAtomic(name, raw, 0o600)
}
