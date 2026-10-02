package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/registry"
)

// The one-editor-per-document advert: <doc>.serve.json beside the document,
// written by `galley edit` (cli.announceEdit) and read by every CLI verb that
// looks for a running server.

// roomIdleTimeout is applied to every ws.Server this package constructs
// (Server.New and EditServer.NewEdit). A galley process serves exactly one
// room for its whole lifetime, and pins doc = yjs.GetDoc(room) once, in the
// constructor, before any peer can ever reconnect. ws.NewServer's default
// RoomIdleTimeout is 0 — eager-evict: the room is destroyed the instant its
// last websocket peer disconnects (a page reload, a tab close, a transient
// network drop). The NEXT connection then creates a brand-new room with a
// brand-new *crdt.Doc, and every later browser edit lands there — invisible
// to the doc this process pinned, and so invisible to Project, /_galley/pending,
// and the sidecar, which all read the pinned doc. Silent data loss.
//
// ws.Server's config surface has no "never evict" sentinel (RoomIdleTimeout
// must be > 0 to switch out of eager-evict at all — see ygo's idle_sweep.go),
// so this is set to a duration long enough that it cannot fire within the
// life of any real `galley serve`/`galley edit` process: the server itself is
// the room's only owner, and there is nothing else to reclaim the room from.
const roomIdleTimeout = 100 * 365 * 24 * time.Hour

// roomFor names the y-websocket room for a page. The basename is enough — one
// server serves one page — and it keeps the websocket URL legible.
func roomFor(pagePath string) string {
	base := filepath.Base(pagePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// DefaultRuntimePath is where a running server advertises itself, so another
// process can find it without being told a port.
func DefaultRuntimePath(pagePath string) string {
	ext := filepath.Ext(pagePath)
	return pagePath[:len(pagePath)-len(ext)] + ".serve.json"
}

// Runtime is what a running server advertises about itself.
type Runtime struct {
	URL  string `json:"url"`
	Room string `json:"room"`
	Page string `json:"page"`
	PID  int    `json:"pid"`
}

// AlreadyServing is the refusal: a LIVE server already holds this document, and
// a second one would erase its work. Typed and exported so a caller can read
// the running server's address out of it rather than parsing the message — the
// one thing a session that wanted an editor actually needs.
type AlreadyServing struct{ Runtime Runtime }

func (e *AlreadyServing) Error() string {
	return fmt.Sprintf("%s is already open at %s (pid %d) — use that address, or stop that server first",
		filepath.Base(e.Runtime.Page), e.Runtime.URL, e.Runtime.PID)
}

// claimDocument is the ONE-SERVER-PER-DOCUMENT rule, asked at construction
// rather than at announce time, because the harm is not the advert being
// overwritten — it is the SECOND DOCUMENT existing at all.
//
// Two `galley edit` servers on one file each parse it into their own *crdt.Doc
// and each project their own back over it, and neither can see the other: A's
// suggestion lands on disk, B's next projection writes B's pre-suggestion
// document over it, and A's shutdown flush writes A's over that. Measured, one
// agent's whole session was erased with nothing logged, nothing warned, and
// both processes exiting 0 — see TestTwoEditorsOnOneDocumentEraseEachOther,
// which keeps that failure executable.
//
// A dead PID's advert is NOT a claim: crashes, SIGKILLs and closed laptops all
// leave one behind, and refusing to open a document because of a corpse would
// be a worse day-one bug than the one this fixes. readRuntime reaps it (through
// registry.Alive, the one liveness rule — see that function).
//
// ANY live advert is a claim here, including one this process wrote: a process
// that already advertises a document already has a server for it, so the second
// constructor call is the second document whatever the PIDs say. Announce asks
// the narrower question — see claimUnheldByAnother.
func claimDocument(runtimePath string) error {
	if rt, ok := readRuntime(runtimePath); ok {
		return &AlreadyServing{Runtime: rt}
	}
	return nil
}

// closeTimeout is how long Close waits for peer goroutines to exit. Short: by
// the time it is called the document is already on disk, so nothing is lost by
// giving up on a stuck connection.
const closeTimeout = 3 * time.Second

// FindRuntime reports where a server for this page is listening, if one is.
func FindRuntime(pagePath string) (Runtime, bool) {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return Runtime{}, false
	}
	return readRuntime(DefaultRuntimePath(abs))
}

// readRuntime is the single reader of a <doc>.serve.json, and it REAPS ON READ:
// an advert whose process is gone is removed and reported absent, exactly as
// registry.List does for its own directory, and through the same registry.Alive
// so there is one answer to "still running" rather than two.
//
// A DEAD SERVER IS NEVER RETURNED. FindRuntime used to hand a caller the last
// advert on disk whatever wrote it, so every CLI verb's live path would open a
// connection to a port nobody is listening on — and the fallback is the offline
// path, which is silent about having taken it. Reaping here also means a
// crashed session's leftover advert cannot block the next `galley edit`: the
// stale file is gone by the time claimDocument reads the answer.
func readRuntime(runtimePath string) (Runtime, bool) {
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		return Runtime{}, false
	}
	var rt Runtime
	if err := json.Unmarshal(raw, &rt); err != nil {
		return Runtime{}, false
	}
	if rt.URL == "" {
		return Runtime{}, false
	}
	if !registry.Alive(rt.PID) {
		_ = os.Remove(runtimePath)
		return Runtime{}, false
	}
	return rt, true
}
