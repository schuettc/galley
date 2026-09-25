// Package serve hosts a review page as a live Yjs document.
//
// The point of the server is the round-trip. A review page read from disk is a
// one-way document: the reviewer's objections have to be retyped somewhere else
// to be acted on. Served, the page and the agent share one document — the
// reviewer types in the browser, the agent reads and replies in-process, and
// each sees the other's words appear without asking.
//
// Three consumers, one document:
//
//	browser  → y-websocket at /yjs/{room}, the ygo provider
//	agent    → JSON over /_galley/*, against the same in-process *crdt.Doc
//	disk     → a JSON projection beside the page, written after every change
//
// The disk projection is what survives the server: it is git-friendly, it is
// what `galley comments` reads when nothing is running, and it is replayed
// into a fresh document at startup.
package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"
	ws "github.com/reearth/ygo/provider/websocket"
	tools "github.com/schuettc/tools-common"

	"github.com/schuettc/galley/internal/ledger"
	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/suggest"
)

//go:embed overlay.js
var overlayJS string

//go:embed overlay.css
var overlayCSS string

// assets carries the browser half: Go's own wasm_exec.js loader and the
// compiled client. wasm_exec.js is committed; galley.wasm is built by
// `just wasm` and deliberately not committed, so a 4MB binary does not land in
// git on every change. `just build` always builds it first.
//
//go:embed assets
var assets embed.FS

// bodyClose matches the final closing body tag, however it is spelled.
var bodyClose = regexp.MustCompile(`(?i)</body\s*>`)

// exportDebounce is short: the disk projection should trail the document
// closely, because it is what the CLI reads. The notification debounce is a
// separate, much longer window — see Notifier.
const exportDebounce = 400 * time.Millisecond

// schemaVersion is stamped into the document so a future change to the thread
// shape can recognise what it is reading.
const schemaVersion = 1

// Server serves one page, one room, and the comment file beside it.
type Server struct {
	PagePath     string
	CommentsPath string
	RuntimePath  string
	Room         string

	// Ledger, when set, is where this server's decisions are remembered; nil
	// means the process-wide ledger.DefaultRecorder. See EditServer.Ledger — the
	// field exists for the same reason, and carries the same contract: the
	// ledger is memory, never truth, and nothing here may fail a decision for
	// want of a record.
	Ledger *ledger.Recorder

	// Notify, when set, fires after the reviewer stops typing. It exists so the
	// reviewer never has to tell the agent to go and look.
	Notify *Notifier

	doc *crdt.Doc
	yjs *ws.Server

	debounce debouncer
	exported lastExportStamp

	static http.Handler
	once   sync.Once
}

// debouncer schedules a delayed action, replacing any run still pending —
// shared by Server and EditServer so a reviewer's page and an editor's
// document debounce their disk projection identically rather than each
// carrying its own near-copy of the same timer dance.
type debouncer struct {
	mu    sync.Mutex
	timer *time.Timer
	// running counts scheduled runs that have not finished — one ticket per
	// timer, handed back either by the run itself or by whoever cancels it.
	running sync.WaitGroup
}

// touch (re)schedules fn to run after delay, cancelling whatever was already
// pending. fn's error has nowhere to go here, same as before the extraction:
// a caller that needs to observe it (shutdown's synchronous flush) calls the
// export method directly instead of going through touch.
func (d *debouncer) touch(delay time.Duration, fn func() error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil && d.timer.Stop() {
		// Stopped before it fired, so the run it was counted for will never
		// happen and its ticket has to be handed back here.
		d.running.Done()
	}
	d.running.Add(1)
	d.timer = time.AfterFunc(delay, func() {
		defer d.running.Done()
		_ = fn()
	})
}

// stop cancels whatever run is pending, if any. A caller about to do the work
// synchronously (EditServer.Flush at shutdown) uses this so the timer does not
// fire a second, redundant projection behind it — or, worse, after the process
// has already reported itself flushed.
// It also WAITS for a run already in flight. time.Timer.Stop cancels a timer
// that has not fired and reports false for one that has — and a projection
// already inside fn is exactly the case a caller means to be finished with.
// Without the wait, Close returns while Project is still writing the file: the
// server says it has stopped and the document lands afterwards. It surfaced as
// `TempDir RemoveAll: directory not empty` in CI, a test's own directory being
// deleted out from under a projection that Close had promised was over.
func (d *debouncer) stop() {
	d.mu.Lock()
	if d.timer != nil {
		if d.timer.Stop() {
			d.running.Done()
		}
		d.timer = nil
	}
	d.mu.Unlock()
	// Outside the lock: fn may touch() again, and waiting under the mutex it
	// would need is how that becomes a deadlock instead of a delay.
	d.running.Wait()
}

// lastExportStamp tracks when a projection last reached disk — read by the
// overlay via handleSaved to tell the reviewer their words are durable, and
// by the editor's equivalent once Task 9 adds it.
type lastExportStamp struct {
	mu sync.Mutex
	at time.Time
}

func (l *lastExportStamp) mark() {
	l.mu.Lock()
	l.at = time.Now()
	l.mu.Unlock()
}

func (l *lastExportStamp) get() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.at
}

// New builds a server for a page. An empty commentsPath defaults to the page
// path with its extension replaced by ".comments.json".
func New(pagePath, commentsPath string) (*Server, error) {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("page: %w", err)
	}
	// One server per page — see claimDocument. Server projects to the sidecar
	// where EditServer projects to the .md, but the erasure is the same shape:
	// two documents, each overwriting the other's projection.
	if err := claimDocument(DefaultRuntimePath(abs)); err != nil {
		return nil, err
	}
	if commentsPath == "" {
		commentsPath = DefaultCommentsPath(abs)
	}

	room := roomFor(abs)
	yjs := ws.NewServer()
	yjs.RoomIdleTimeout = roomIdleTimeout

	// A room does not exist until someone touches it, and GetDoc returns nil
	// until it does. Apply auto-creates it — but it rejects a mutation that
	// produces no changes, so the room is opened by writing something worth
	// having: which page this document belongs to, and which schema it speaks.
	if err := yjs.Apply(context.Background(), room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			meta := doc.GetMap("meta")
			transact(func(txn *crdt.Transaction) {
				meta.Set(txn, "page", filepath.Base(abs))
				meta.Set(txn, "schema", schemaVersion)
			})
		}); err != nil {
		return nil, fmt.Errorf("create room %q: %w", room, err)
	}
	doc := yjs.GetDoc(room)
	if doc == nil {
		return nil, fmt.Errorf("room %q did not materialise", room)
	}

	s := &Server{
		PagePath:     abs,
		CommentsPath: commentsPath,
		RuntimePath:  DefaultRuntimePath(abs),
		Room:         room,
		doc:          doc,
		yjs:          yjs,
	}

	// Replay whatever the last session left behind, before anything can
	// observe the document — so startup never looks like a burst of new
	// comments to the notifier.
	if raw, err := os.ReadFile(s.CommentsPath); err == nil {
		if err := review.Import(s.doc, raw); err != nil {
			return nil, fmt.Errorf("replay %s: %w", s.CommentsPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	s.doc.OnUpdate(func(_ []byte, _ any) { s.touch() })
	return s, nil
}

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

// DefaultCommentsPath is the comment file that sits beside a page.
func DefaultCommentsPath(pagePath string) string {
	ext := filepath.Ext(pagePath)
	return pagePath[:len(pagePath)-len(ext)] + ".comments.json"
}

// DefaultRuntimePath is where a running server advertises itself, so another
// process can find it without being told a port.
func DefaultRuntimePath(pagePath string) string {
	ext := filepath.Ext(pagePath)
	return pagePath[:len(pagePath)-len(ext)] + ".serve.json"
}

// Doc exposes the live document for reads.
func (s *Server) Doc() *crdt.Doc { return s.doc }

// Append adds an entry to a thread and fans the change out to connected peers.
//
// It goes through the websocket server's Apply rather than the document's own
// Transact for exactly one reason: Apply captures the resulting update and
// broadcasts it. A reply written straight to the document would be correct on
// disk and invisible on the reviewer's screen.
func (s *Server) Append(key, heading, author, text string, at time.Time) error {
	return s.yjs.Apply(context.Background(), s.Room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			review.Bind(doc, transact).Append(key, heading, author, text, at)
		})
}

// SetResolved marks a thread settled, or reopens it, and tells the peers.
func (s *Server) SetResolved(key string, resolved bool) error {
	var inner error
	err := s.yjs.Apply(context.Background(), s.Room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			inner = review.Bind(doc, transact).SetResolved(key, resolved)
		})
	if err != nil {
		return err
	}
	return inner
}

// Inject splices the overlay into a page, immediately before the closing body
// tag so the page's own scripts have already run. A page with no body tag gets
// the overlay appended.
//
// The overlay is never written into the page itself: the generator stays dumb,
// and any HTML file becomes annotatable.
func Inject(page []byte) []byte {
	overlay := []byte(
		"<style>\n" + overlayCSS + "</style>\n" +
			"<script src=\"/_galley/wasm_exec.js\"></script>\n" +
			"<script>\n" + overlayJS + "</script>\n")
	loc := bodyClose.FindAllIndex(page, -1)
	if len(loc) == 0 {
		return append(append([]byte{}, page...), overlay...)
	}
	at := loc[len(loc)-1][0]
	out := make([]byte, 0, len(page)+len(overlay))
	out = append(out, page[:at]...)
	out = append(out, overlay...)
	out = append(out, page[at:]...)
	return out
}

// touch schedules a projection to disk and, once things settle, a notification.
func (s *Server) touch() {
	s.debounce.touch(exportDebounce, s.Export)
	s.Notify.Touch(fingerprint(review.Read(s.doc)))
}

// Export writes the document's projection beside the page, atomically, so a
// half-written file is never left behind for the agent to read.
//
// Plain review.Export, not ExportOnto: Server serves HTML review pages, whose
// sidecars never carry a Suggestions block (that field exists only on the
// markdown editor's side, see EditServer.project), so there is nothing here
// for a merge to preserve. A merge would also have to read the existing
// sidecar first, which trades away a property this path depends on — Export
// self-heals a corrupt or missing sidecar by overwriting it. Reading it first
// makes a damaged file (a git merge conflict landed in it, say) fail the
// export instead, silently dropping the debounced projection and, on the
// SIGINT path, the whole session's threads.
func (s *Server) Export() error {
	f := review.Export(s.doc, filepath.Base(s.PagePath))
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.CommentsPath, append(raw, '\n')); err != nil {
		return err
	}
	s.exported.mark()
	return nil
}

// LastExport is when the projection last reached disk. The overlay polls this
// to tell the reviewer their words are durable — "synced to a peer" and "on
// disk" are different claims, and only the second one survives a crash.
func (s *Server) LastExport() time.Time {
	return s.exported.get()
}

// Load reads the projection from disk. A missing file is not an error — it is
// the normal state before the first comment.
func Load(commentsPath string) (review.File, error) {
	f := review.File{Threads: []review.Thread{}, Comments: []review.Comment{}}
	raw, err := os.ReadFile(commentsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, err
	}
	return f, nil
}

// WriteFileAtomic writes raw to path through a temp file and a rename, so a
// reader never observes a half-written document — and without changing what
// the file IS.
//
// The plain form of this gets two things wrong on a file the user already
// owns. A temp file is created 0600, so renaming it over a document the user
// had made group- or world-readable silently locks it down. And a rename onto
// a SYMLINK replaces the link with a regular file, detaching a document that
// was deliberately reached through one — a doc symlinked into a notes tree
// becomes two divergent files.
//
// So: resolve the path first and write through the link, and carry the
// existing file's permission bits onto the replacement. A file that does not
// exist yet is created 0644 (before umask), the ordinary default for a
// document rather than the private default of a temp file.
//
// One residual, deliberately: a rename detaches path from its inode, so a
// HARD link to the same document keeps the old content under its other name.
// Preserving that would mean truncating and rewriting in place, which gives up
// the atomicity this function exists for. A torn document is worse.
func WriteFileAtomic(path string, raw []byte) error {
	target, perm, err := resolveWriteTarget(path)
	if err != nil {
		return err
	}
	// The temp/fsync/rename itself is the family's (tools.WriteFileAtomic);
	// what stays here is galley's policy about WHICH path and WHICH mode.
	return tools.WriteFileAtomic(target, raw, perm)
}

// defaultFileMode is what a file this package CREATES gets. 0644 before umask
// — a document, not a secret. An existing file's own mode always wins.
const defaultFileMode os.FileMode = 0o644

// resolveWriteTarget reports the real path to rename onto and the permission
// bits the result must end up with. A missing file is not an error: it is the
// first projection.
func resolveWriteTarget(path string) (string, os.FileMode, error) {
	st, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return path, defaultFileMode, nil
	case err != nil:
		return "", 0, err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return path, st.Mode().Perm(), nil
	}
	// Write THROUGH the link, at whatever it finally points at, with that
	// file's mode — so the link survives and keeps meaning what it meant.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A dangling link: there is nothing to preserve and nothing to
		// resolve to, so treat the link itself as the destination.
		return path, defaultFileMode, nil //nolint:nilerr // a broken link is a first write, not a failure to report
	}
	rst, err := os.Stat(resolved)
	if err != nil {
		return "", 0, err
	}
	return resolved, rst.Mode().Perm(), nil
}

func writeFileAtomic(path string, raw []byte) error { return WriteFileAtomic(path, raw) }

// Offline applies a change to a page's projection with no server running:
// replay the file into a scratch document, mutate it, write it back.
//
// It exists so `galley reply` is a real command rather than one that only
// works when something else happens to be up. When a server IS running the
// caller should go through it instead, so the browser sees the change live.
func Offline(pagePath, commentsPath string, fn func(*crdt.Doc) error) error {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return err
	}
	if commentsPath == "" {
		commentsPath = DefaultCommentsPath(abs)
	}
	// Loaded whole, not just replayed: the scratch document holds threads and
	// nothing else, so writing Export's projection over the file would delete
	// every field File carries that the document cannot — Suggestions, whose
	// author and timestamp exist nowhere else. See review.ExportOnto.
	prior, err := Load(commentsPath)
	if err != nil {
		return err
	}
	doc := crdt.New()
	if raw, err := os.ReadFile(commentsPath); err == nil {
		if err := review.Import(doc, raw); err != nil {
			return fmt.Errorf("replay %s: %w", commentsPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fn(doc); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(review.ExportOnto(doc, filepath.Base(abs), prior), "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(commentsPath, append(raw, '\n'))
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

// claimUnheldByAnother is claimDocument's other audience, and the difference is
// only in whose advert counts. Announce may rewrite OUR OWN entry — that is how
// a reopen re-advertises the same server, and how a port change is published —
// but it may never take one from another live process.
func claimUnheldByAnother(runtimePath string) error {
	if rt, ok := readRuntime(runtimePath); ok && rt.PID != os.Getpid() {
		return &AlreadyServing{Runtime: rt}
	}
	return nil
}

// Announce records where this server can be reached, so `galley reply` finds
// it without being handed a port.
//
// It refuses rather than overwrites, which is the same rule claimDocument asks
// at construction, asked again at the last moment before the advert changes
// hands: New claims, then a port is bound, and a server that announced in that
// window would otherwise take the document's identity from the process that
// already holds it — FindRuntime returns whoever announced LAST, so a `galley
// reply` would then be delivered to the wrong server.
func (s *Server) Announce(url string) error {
	if err := claimUnheldByAnother(s.RuntimePath); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(Runtime{
		URL:  url,
		Room: s.Room,
		Page: s.PagePath,
		PID:  os.Getpid(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.RuntimePath, append(raw, '\n'))
}

// Withdraw removes the advertisement on shutdown — OURS, and never somebody
// else's. An unconditional remove orphans a live server: the advert is one file
// per document, so the first process to exit used to delete whatever was there,
// leaving a running server no CLI verb could find. Nothing errors, and every
// `galley pending`/`reply`/`suggest` silently falls back to the offline path.
//
// A dead process's advert IS removed — that is the same reaping readRuntime
// does, and leaving it would be the stale-entry bug back again.
func (s *Server) Withdraw() { withdrawRuntime(s.RuntimePath) }

// withdrawRuntime removes an advert only if it is ours to remove: another live
// process's stays, anything else (ours, a dead process's, unreadable bytes) goes.
func withdrawRuntime(runtimePath string) {
	if rt, ok := readRuntime(runtimePath); ok && rt.PID != os.Getpid() {
		return
	}
	_ = os.Remove(runtimePath)
}

// Close releases everything the server started: the pending debounced
// projection, and the websocket server's peer connections and per-room idle
// sweeper.
//
// ws.NewServer starts that sweeper whether or not a peer ever connects, and
// nothing here used to stop it — a test binary building thirty-odd servers
// finished with thirty-odd live sweepers, and a real process exited with its
// peers still attached. Call it after the final flush: it closes peer
// connections, so anything still to be written must already be written.
//
// closeTimeout bounds the wait rather than blocking shutdown on a peer that
// will not go quietly.
func (s *Server) Close() error {
	s.debounce.stop()
	s.Notify.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	return s.yjs.Shutdown(ctx)
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

// Handler is the HTTP surface: the injected page at /, the y-websocket endpoint
// for the browser, the JSON endpoints for the agent, and any sibling asset the
// page references.
func (s *Server) Handler() http.Handler {
	s.once.Do(func() {
		s.static = http.FileServer(http.Dir(filepath.Dir(s.PagePath)))
	})
	mux := http.NewServeMux()
	mux.Handle("/yjs/{room}", s.yjs)
	mux.HandleFunc("/_galley/wasm_exec.js", serveAsset("assets/wasm_exec.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/_galley/galley.wasm", serveAsset("assets/galley.wasm", "application/wasm"))
	// The caret, committed rather than built — see editmode.go's identical
	// route for why it is unconditional where wasm_exec.js is not.
	mux.HandleFunc("/_galley/favicon.svg", serveAsset("assets/favicon.svg", "image/svg+xml"))
	mux.HandleFunc("/_galley/threads", s.handleThreads)
	mux.HandleFunc("/_galley/reply", s.handleReply)
	mux.HandleFunc("/_galley/resolve", s.handleResolve)
	mux.HandleFunc("/_galley/rev", s.handleRev)
	mux.HandleFunc("/_galley/saved", s.handleSaved)
	mux.HandleFunc("/_galley/room", s.handleRoom)
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

// serveAsset serves one embedded file. A missing galley.wasm means the binary
// was built without `just build`; say so plainly rather than 404ing, because
// the page fails silently otherwise.
func serveAsset(name, contentType string) http.HandlerFunc {
	return serveAssetHint(name, contentType, "`just build`, which compiles the wasm client first")
}

// serveAssetHint is serveAsset with the build instruction spelled out, because
// not every embedded asset comes from the same recipe: the wasm client is
// built by `just build`, the editor bundle by `just assets`. Naming the wrong
// one is worse than saying nothing — it sends someone to run a command that
// cannot produce the missing file.
func serveAssetHint(name, contentType, hint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := assets.ReadFile(name)
		if err != nil {
			http.Error(w, "galley was built without "+path.Base(name)+
				" — run "+hint, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(raw)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.static.ServeHTTP(w, r)
		return
	}
	page, err := os.ReadFile(s.PagePath)
	if err != nil {
		http.Error(w, "page unreadable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(Inject(page))
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"room": s.Room})
}

func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, review.Export(s.doc, filepath.Base(s.PagePath)))
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Key == "" || strings.TrimSpace(in.Text) == "" {
		http.Error(w, "key and text are required", http.StatusBadRequest)
		return
	}
	if err := s.Append(in.Key, "", review.AuthorAgent, in.Text, time.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, review.Export(s.doc, filepath.Base(s.PagePath)))
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key      string `json:"key"`
		Resolved *bool  `json:"resolved"`
	}
	if !decode(w, r, &in) {
		return
	}
	resolved := true
	if in.Resolved != nil {
		resolved = *in.Resolved
	}
	// Read BEFORE the mutation: afterwards the only thing left is a key, and
	// the ledger's record of a settled conversation has to say what it was
	// about. REVIEW MODE IS A DECISION SURFACE TOO — it is the older one, and
	// leaving one of galley's two resolve paths unrecorded is exactly how a
	// store stops being able to answer "how often do I settle rather than
	// decide".
	settled, _ := threadByKey(s.doc, in.Key)
	if err := s.SetResolved(in.Key, resolved); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// Only the settling half — see EditServer's handleThreadResolve for why a
	// reopen decides nothing.
	if resolved {
		s.remember(ThreadRecord(ledger.KindResolved, settled))
	}
	writeJSON(w, review.Export(s.doc, filepath.Base(s.PagePath)))
}

// remember writes one decision made on the review page. Same contract as edit
// mode's: it returns nothing, so nothing here can fail a settle for want of a
// record. The document is the PAGE — review mode has no markdown file to name,
// and the path a decision was made about is the honest answer either way.
//
// No review id: review mode's room is not a per-run token, so there is nothing
// here that groups one round's decisions the way edit mode's room does.
func (s *Server) remember(rec ledger.Record) {
	r := s.Ledger
	if r == nil {
		r = ledger.DefaultRecorder
	}
	r.Record(s.PagePath, rec)
}

// guard is what every mutating endpoint runs before it does anything. It
// answers the request itself and returns false when the request must not
// proceed.
//
// THE THREAT IS AN ORDINARY WEB PAGE. galley listens on 127.0.0.1 with no
// authentication — by design, it is the user's own machine — so any page the
// user has open in the same browser can send it requests, and the browser will
// attach nothing that identifies the sender. Before this guard existed, a
// cross-origin POST rewrote the user's document, and a cross-origin POST to
// /_galley/revise made the server run its configured command through `sh -c`.
// Both were verified. No fetch and no CORS cooperation is needed for that: a
// plain auto-submitting <form> is a "simple request", exempt from preflight,
// and the attacker never has to read the response to have done the damage.
//
// Three checks, each closing a different door:
//
//   - POST ONLY, and the Allow header says exactly that. The previous code
//     also accepted PUT while advertising "Allow: POST"; a method the 405
//     denies existed is a method nothing is testing.
//   - CONTENT-TYPE MUST BE application/json. A cross-site form can only spell
//     text/plain, application/x-www-form-urlencoded or multipart/form-data.
//     Requiring anything else forces an attacker onto fetch/XHR, which needs a
//     preflight this server never answers. This is the load-bearing half.
//   - SAME-SITE ONLY. Origin (sent by every browser on POST) must match the
//     host being addressed, and an explicit cross-site Sec-Fetch-Site is
//     refused outright. A request with NO Origin is allowed: that is the CLI
//     and anything else that is not a browser, and it is not something a page
//     can produce.
func guard(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed — this endpoint takes POST", http.StatusMethodNotAllowed)
		return false
	}
	if ct := r.Header.Get("Content-Type"); !isJSONContentType(ct) {
		http.Error(w, "this endpoint takes application/json; got "+strconv.Quote(ct),
			http.StatusUnsupportedMediaType)
		return false
	}
	if !sameSite(r) {
		http.Error(w,
			"refused: this request came from another site. galley serves your own machine "+
				"with no authentication, so it only accepts requests from its own page or a local tool.",
			http.StatusForbidden)
		return false
	}
	return true
}

// isJSONContentType reports whether ct is application/json, ignoring
// parameters (a charset is legal and common) and case.
//
// An EMPTY Content-Type is refused too. It is tempting to allow it for
// bodyless requests like /_galley/revise, but "no header" is precisely what a
// hand-rolled cross-site request would send, and every legitimate caller here
// — the browser's own fetch and the CLI's http.Post — sets it.
func isJSONContentType(ct string) bool {
	mediaType, _, err := mime.ParseMediaType(ct)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

// sameSite reports whether the request can be trusted not to have been sent by
// another site's page. See guard for the reasoning.
func sameSite(r *http.Request) bool {
	// Sent by every modern browser and by nothing else, so it is checked first
	// and taken at face value when present. "none" is a user-initiated
	// navigation, not a page acting on its own behalf.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Not a browser: the CLI, curl, an agent. A page cannot suppress this
		// header on a POST, so its absence is evidence, not a gap.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if !guard(w, r) {
		return false
	}
	// UNKNOWN FIELDS ARE REFUSED, WHICH IS THE INBOUND HALF OF THE WIRE
	// CONTRACT.
	//
	// web/wire.d.ts holds the browser to the Go structs in the Go->browser
	// direction: rename a field there and `just verify` fails. Nothing held the
	// browser->Go direction to anything at all. encoding/json IGNORES a field
	// it does not recognise, so a page posting `{"tex": "..."}` at a handler
	// reading `Text` was a 204 and a no-op — the press worked, the server did
	// nothing, and there was no error anywhere to look at. That is the same
	// silence `pending.suggestions` -> `pending.instructions` cost weeks over,
	// pointing the other way.
	//
	// A 400 naming the field is what an unknown field deserves: the sender is
	// this repository's own bundle, shipped in the same binary as this server,
	// so a field mismatch is a BUG and never a version skew to be tolerated.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// handleRev lets the page notice that the agent regenerated it underneath.
func (s *Server) handleRev(w http.ResponseWriter, r *http.Request) {
	st, err := os.Stat(s.PagePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int64{"rev": st.ModTime().UnixNano()})
}

// handleSaved reports when the projection last reached disk, in epoch
// milliseconds — the overlay compares it against the moment of the last
// keystroke to say "on disk" honestly.
func (s *Server) handleSaved(w http.ResponseWriter, r *http.Request) {
	at := s.LastExport()
	var ms int64
	if !at.IsZero() {
		ms = at.UnixMilli()
	}
	writeJSON(w, map[string]int64{"saved": ms})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// Notifier runs a command once the document has settled.
//
// It is deliberately a shell command rather than a muster call: the binary must
// be worth running without any of the rest of the toolchain, and "tell someone"
// is exactly the seam where setups differ.
type Notifier struct {
	// Command is run through `sh -c` after Quiet elapses with no further edits.
	Command string
	// Quiet is how long the document must sit unchanged first. Firing on every
	// keystroke would wake the agent mid-sentence, repeatedly.
	Quiet time.Duration
	// Log receives one line per fired or failed notification.
	Log func(string)

	mu      sync.Mutex
	timer   *time.Timer
	lastRun string
	wake    func(string)
}

// SetWake registers what to release when the notifier decides the document has
// moved — the PULL half of the same event Command pushes.
//
// A hook off fire's one decision, deliberately, rather than a second
// fingerprint check somewhere else. A blocked `galley wait` must wake on
// exactly what would have woken a shell hook — including the self-wake guard
// mutate seeds, which lives in lastRun and nowhere else — and the only way to
// guarantee that is to hang off the decision instead of re-making it.
//
// nil clears it. Safe on a nil Notifier, like every other method here.
func (n *Notifier) SetWake(f func(fp string)) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.wake = f
}

// Touch schedules a notification, replacing any already pending.
func (n *Notifier) Touch(fp string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	// Nothing to push and nobody pulling: the timer would fire into an empty
	// room. Checked here under the lock rather than at the top, because
	// SetWake can arrive between two Touches.
	if n.Command == "" && n.wake == nil {
		return
	}
	if n.timer != nil {
		n.timer.Stop()
	}
	// NOTHING NEW TO SAY — and arming it anyway is not the harmless no-op it
	// looks like. fire would decline this fingerprint today, because it is the
	// one last announced; but lastRun can MOVE before the timer elapses (Seed,
	// when the agent writes and is handed everything pending in the same
	// breath), and this stale fingerprint then differs from the new one and
	// fires — waking the agent through a timer armed before its own write. So a
	// projection that says exactly what was last announced CANCELS the pending
	// notification and arms nothing.
	//
	// Reachable since a live round is cut on the wake: that cut asks for a
	// projection of its own, which touches with the fingerprint the fire it came
	// from has just consumed. Caught by TestAgentReplyDoesNotWakeTheAgent.
	if fp == n.lastRun {
		return
	}
	quiet := n.Quiet
	if quiet <= 0 {
		quiet = 8 * time.Second
	}
	n.timer = time.AfterFunc(quiet, func() { n.fire(fp) })
}

func (n *Notifier) fire(fp string) {
	n.mu.Lock()
	// Nothing actually changed since the last notification — a reviewer who
	// typed and undid should not wake anyone.
	if fp == n.lastRun {
		n.mu.Unlock()
		return
	}
	n.lastRun = fp
	cmd := n.Command
	logf := n.Log
	wake := n.wake
	n.mu.Unlock()

	// The pull half first, and out of the lock: a blocked reader is a session
	// already holding the context, and it should not queue behind a push
	// command that may legitimately run for minutes.
	if wake != nil {
		wake(fp)
	}
	// A notifier can exist purely to release waiters — pull needs no shell
	// command, which is the whole point of it.
	if cmd == "" {
		return
	}

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if logf == nil {
		return
	}
	if err != nil {
		logf(fmt.Sprintf("notify failed: %v: %s", err, bytes.TrimSpace(out)))
		return
	}
	logf("notified")
}

// Seed records the current state as already-notified, so replaying a previous
// session's comments at startup does not immediately wake anyone.
func (n *Notifier) Seed(fp string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastRun = fp
}

// Stop cancels any pending notification.
func (n *Notifier) Stop() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.timer != nil {
		n.timer.Stop()
	}
}

// Fingerprint identifies a set of threads, so an edit that lands back on the
// previous text does not count as a change. Agent replies are included: a reply
// changes the conversation, and the reviewer should see it reflected.
func Fingerprint(threads []review.Thread) string { return fingerprint(threads) }

// FingerprintPending identifies everything a reviewer could be waiting on: the
// pending suggestions and the comment threads together.
//
// Edit mode's document changes in ways review mode's cannot — a suggestion
// appears, is accepted, is rejected — and none of that touches a thread. A
// notifier fingerprinting threads alone would sit silent through an entire
// editing session.
//
// Run is deliberately EXCLUDED. It is session identity, re-minted on every
// load (see docmodel.RunAttr and suggest.MintRuns), so including it would make
// every restart read as a document full of new work and wake the agent for
// nothing. Kind, author and text are what a reader would call the same
// suggestion.
func FingerprintPending(suggestions []suggest.Pending, threads []review.Thread) string {
	h := sha256.New()
	for _, s := range suggestions {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", s.Kind, s.Author, s.Text)
	}
	// A separator, so a suggestion's text can never be confused for the start
	// of the thread half.
	_, _ = io.WriteString(h, "\x01")
	_, _ = io.WriteString(h, fingerprint(threads))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func fingerprint(threads []review.Thread) string {
	h := sha256.New()
	for _, t := range threads {
		_, _ = fmt.Fprintf(h, "%s\x00%v\x00", t.Key, t.Resolved)
		for _, e := range t.Entries {
			_, _ = fmt.Fprintf(h, "%s\x00%s\x00", e.Author, e.Text)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Listen binds the address and reports the URL to reach it on, so the caller can
// print and open a port the OS chose.
func Listen(addr string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	host := ln.Addr().String()
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		host = fmt.Sprintf("127.0.0.1:%d", tcp.Port)
	}
	return ln, "http://" + host, nil
}
