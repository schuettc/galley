// Package registry is where running editors advertise themselves to a channel
// that starts before any document is open.
//
// The per-document <doc>.serve.json sibling answers "where is the server for
// THIS path" — discovery for a caller that already knows the path. A channel
// knows no paths; it needs "what is being edited right now, anywhere", and
// that is this directory: one JSON file per live editor, written on announce,
// removed on withdraw, reaped on read when the writing process is gone.
//
// OWNERSHIP is the session id of whoever ran `galley edit`, set from the
// caller's harness session id (see cmd/galley's sessionID). Empty means
// UNOWNED — opened by a human in a plain terminal — and unowned entries are
// claimable by any channel under whose root they fall. See the channel design
// spec.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/schuettc/galley/internal/ondisk"
)

// Version is the schema generation of an advert and of a session presence
// record, written as `v` on every file this build writes.
//
// THE REGISTRY IS FORWARD-COMPATIBLE AND MUST STAY THAT WAY. Two galley builds
// run against one `~/.galley/live/` as a matter of course: an editor started
// before an upgrade keeps its advert alive for hours, and the channel that
// scans for it is a different, longer-lived process that may be older or newer
// than every editor it finds. So an unknown KEY is ignored — a strict decoder
// here would turn "one process was upgraded" into "the channel can no longer
// see my editors", with no upgrade order that avoids it — and an unknown
// VERSION is REPORTED AND LEFT ALONE rather than reaped, because reaping is a
// deletion and this build cannot judge the liveness of a record whose rules it
// does not know.
//
// What makes a rename loud here is not the decoder, which cannot see a key it
// was never told about: it is TestTheAdvertKeysAreTheContract, which pins the
// key set and goes red on the commit that renames one. See internal/ondisk.
//
// A file with no `v` is generation 1 — the shape the field was added to.
const Version = 1

type Entry struct {
	// V is the schema generation — see Version. Absent on every advert written
	// before this field existed, which reads as generation 1.
	V     int    `json:"v"`
	URL   string `json:"url"`
	Room  string `json:"room"`
	Page  string `json:"page"`
	PID   int    `json:"pid"`
	Owner string `json:"owner,omitempty"`
}

// Dir is $GALLEY_LIVE_DIR when set — tests, and anyone isolating registries —
// else ~/.galley/live. Created on demand so Write never races a fresh machine.
//
// 0o700, not 0o755: the registry is per-user by design, and the room token is
// the FILENAME — the same token onlyThisRoom gates the websocket on — so a
// world-readable listing hands any local user the key to every live room.
func Dir() (string, error) {
	d := os.Getenv("GALLEY_LIVE_DIR")
	if d == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, ".galley", "live")
	}
	return d, os.MkdirAll(d, 0o700)
}

// validRoom rejects any Room that could carry a path component out of the
// registry directory. This package is the last line of defense between a
// caller-supplied room name and a file write or delete on disk, so a room
// like "../../../etc/cron.d/x" — or anything else filepath.Base would not
// hand back unchanged — is refused rather than joined into a path.
func validRoom(room string) error {
	if room == "" || room == "." || room == ".." || filepath.Base(room) != room {
		return fmt.Errorf("registry: invalid room %q", room)
	}
	return nil
}

// Validate is the one description of an advert this package will report, and
// it is applied on BOTH sides — Write refuses to record what List would refuse
// to report, so an advertiser learns at the moment it writes instead of being
// silently invisible to every channel forever.
//
// EVERYTHING HERE REACHES SOMEWHERE IT MATTERS. The room is a FILENAME (and
// the websocket's gate). The URL is an address this process will issue
// requests to. And the page is rendered into the channel's notification text,
// which lands in a model's context — so a newline in it could forge a second
// line of a `<channel …>` tag. None of the three is this package's to trust:
// two of them come from a caller's environment and all three come off disk,
// where anything may have written them.
func (e Entry) Validate() error {
	if err := validRoom(e.Room); err != nil {
		return err
	}
	if e.PID <= 0 {
		return fmt.Errorf("registry: entry %q has no pid", e.Room)
	}
	if err := validURL(e.URL); err != nil {
		return err
	}
	if err := validPage(e.Page); err != nil {
		return err
	}
	// An owner is compared, never joined into a path (see sessionToken), so
	// the only rule is that it cannot carry something no session id could.
	if len(e.Owner) > 256 || hasControl(e.Owner) {
		return errors.New("registry: implausible owner")
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validURL keeps the advert to what a galley editor can actually be: an HTTP
// address with a host. Deliberately NOT restricted to loopback, even though
// every editor binds 127.0.0.1 today — a bind address is the server's decision
// to change, and a validator that quietly outlives that decision would present
// as a channel that never attaches.
func validURL(raw string) error {
	if raw == "" || len(raw) > 2048 || hasControl(raw) {
		return errors.New("registry: implausible url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("registry: unparseable url %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("registry: url scheme %q is not http", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("registry: url %q has no host", raw)
	}
	if u.User != nil {
		return fmt.Errorf("registry: url %q carries credentials", raw)
	}
	return nil
}

// validPage: an absolute path, because that is what an advertiser writes
// (filepath.Abs) and because a relative one is meaningless to a reader in
// another working directory — the exact silent mismatch a channel cannot
// diagnose.
func validPage(p string) error {
	if p == "" || len(p) > 4096 || hasControl(p) {
		return errors.New("registry: implausible page")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("registry: page %q is not absolute", p)
	}
	return nil
}

// Write is atomic — temp file then rename — for the same reason the runtime
// sibling is: a channel's scan must never read half an entry.
func Write(e Entry) error {
	// Stamped here, not asked of the caller: an advertiser has no way to know
	// which generation the bytes it hands over will be written in, and a record
	// naming a version it was not written by is worse than one naming none.
	e.V = Version
	if err := e.Validate(); err != nil {
		return err
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+e.Room+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, e.Room+".json")); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func Remove(room string) error {
	if err := validRoom(room); err != nil {
		return err
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	// The claim lock is a sibling of the advert (see Claim); it is not a *.json
	// file, so Inspect never sees it, but it should not outlive the advert.
	_ = os.Remove(filepath.Join(dir, room+".lock"))
	err = os.Remove(filepath.Join(dir, room+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Claim atomically takes ownership of room's advert for newOwner, but ONLY when
// it is currently unowned or its owner's session is no longer live — the same
// two conditions channel.claim attaches under. It returns whether this call
// took ownership.
//
// This is what makes an editor reach EXACTLY ONE channel. Without it, an
// unowned advert under two channels' roots (nested scopes: a parent and a child
// directory) is claimable by both, so every Revise/approve/editor-gone wake
// fans out to both sessions — the cross-session "broadcast". The first channel
// to attach stamps itself here; the next scanner then reads a live owner and
// declines. Unlike simply refusing an unowned advert, this leaves no silence:
// the channel that claimed it IS the listener.
//
// Serialized against other Claim callers on the same room by an advisory lock on
// a sibling <room>.lock file. The lock is on a SEPARATE, stable inode on
// purpose: Write replaces <room>.json via temp+rename, so an flock taken on the
// advert's own fd would not survive the swap — the loser would read the
// pre-rename inode and re-claim. The lock file is never a *.json advert, so
// Inspect ignores it; Remove deletes it with the advert.
func Claim(room, newOwner string) (bool, error) {
	if newOwner == "" {
		return false, errors.New("registry: an empty owner cannot claim")
	}
	if err := validRoom(room); err != nil {
		return false, err
	}
	dir, err := Dir()
	if err != nil {
		return false, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, room+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	raw, err := os.ReadFile(filepath.Join(dir, room+".json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil // the advert is gone; there is nothing to claim
		}
		return false, err
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return false, err
	}
	switch {
	case e.Owner == newOwner:
		return true, nil // already ours
	case e.Owner != "" && SessionLive(e.Owner):
		return false, nil // spoken for by a live session — not ours to take
	}
	e.Owner = newOwner
	if err := Write(e); err != nil {
		return false, err
	}
	return true, nil
}

// List reads every entry, reaping the dead: an entry whose PID no longer runs
// is removed rather than reported, because a stale entry tells a channel to
// attach to nothing — the "nobody listening" failure inverted.
func List() ([]Entry, error) {
	entries, _, err := Inspect()
	return entries, err
}

// Problem is one file in the registry directory that could not be reported as
// an editor, and why. It exists because SILENCE IS THE DEFECT: a channel that
// drops a file on the floor and says nothing leaves a reviewer typing into a
// document whose advert was refused for a reason nobody can see.
type Problem struct {
	File   string // base name, as a human would find it in the directory
	Reason string
}

func (p Problem) String() string { return p.File + ": " + p.Reason }

// Inspect is List plus the files it declined and why. List is the hot path
// (every scan) and keeps its signature; Inspect is what a diagnostic reads.
//
// THREE RULES, IN THIS ORDER, AND THE ORDER IS THE POINT:
//
//  1. VALIDATE, so a file this package would never have written is never
//     reported and never deleted — it is somebody else's, and reporting the
//     problem is the whole of what we owe it.
//  2. REAP THE DEAD, unchanged: a PID that no longer runs is a fact about the
//     world, and the file is ours to remove.
//  3. ONE ENTRY PER URL, because two adverts at one address are one server,
//     and attaching to both turns one Revise press into two rounds of work
//     proposed against one ask (measured, twice). The newest advert wins —
//     a re-advertise is the most recent truth about that server — with the
//     filename as a deterministic tie-break.
func Inspect() ([]Entry, []Problem, error) {
	dir, err := Dir()
	if err != nil {
		return nil, nil, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	var problems []Problem
	type candidate struct {
		e    Entry
		name string
		mod  time.Time
	}
	var kept []candidate
	for _, name := range names {
		base := filepath.Base(name)
		raw, err := os.ReadFile(name)
		if err != nil {
			problems = append(problems, Problem{base, "unreadable: " + err.Error()})
			continue
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			problems = append(problems, Problem{base, "not valid JSON; left for a human"})
			continue
		}
		if ondisk.Future(e.V, Version) {
			// REPORTED AND LEFT ALONE — never reaped. See Version: an advert
			// from a newer galley may say things about liveness this build
			// cannot read, and deleting it would take a live editor's only
			// advert away from every other reader on the machine too. It is
			// declined for the same reason rule 1 declines a file this package
			// would never have written: it is somebody else's.
			problems = append(problems, Problem{base, fmt.Sprintf(
				"advert is at schema v%d and this galley knows v%d; left for a newer galley", e.V, Version)})
			continue
		}
		if err := e.Validate(); err != nil {
			problems = append(problems, Problem{base, err.Error()})
			continue
		}
		// The filename IS the room — it is what Remove deletes and what the
		// websocket gate checks — so an entry naming a different one is a lie
		// about which room it is, and no channel should act on it.
		if room := strings.TrimSuffix(base, ".json"); room != e.Room {
			problems = append(problems, Problem{base, fmt.Sprintf("names room %q but is filed as %q", e.Room, room)})
			continue
		}
		if !Alive(e.PID) {
			_ = os.Remove(name)
			continue
		}
		mod := time.Time{}
		if st, err := os.Stat(name); err == nil {
			mod = st.ModTime()
		}
		kept = append(kept, candidate{e, base, mod})
	}
	// Newest first, filename as the tie-break, so the survivor of a duplicate
	// is the same one on every scan.
	sort.SliceStable(kept, func(i, j int) bool {
		if !kept[i].mod.Equal(kept[j].mod) {
			return kept[i].mod.After(kept[j].mod)
		}
		return kept[i].name < kept[j].name
	})
	seen := map[string]string{}
	var out []Entry
	for _, c := range kept {
		if first, dup := seen[c.e.URL]; dup {
			problems = append(problems, Problem{c.name,
				"a second advert for the server already advertised by " + first + "; ignored so one press is one wake"})
			continue
		}
		seen[c.e.URL] = c.name
		out = append(out, c.e)
	}
	return out, problems, nil
}

// --- session presence ---
//
// WHO IS STILL HERE. An advert carries the id of the session that opened the
// document, and until now nothing on this machine could answer "is that
// session still running?" — so an advert stamped by a session that has since
// restarted was foreign to every channel forever, and nothing said so.
//
// THE PID IN THE ADVERT CANNOT ANSWER IT: that PID is the EDITOR's, and the
// editor outlives the session that started it — a browser tab left open across
// a Claude Code restart is exactly the case, and it is an ordinary morning.
// The only process on the machine whose life is the session's own life is the
// session's CHANNEL: Claude Code spawns it at session start and it dies with
// the session. So a channel announces itself here, keyed by session id and
// carrying its own PID, and "is that session live" becomes the same question
// registry.List already answers about editors — does this PID still run — with
// the same reaper.
//
// A presence file is ADVISORY. Its absence never means a session is dead in
// some verifiable sense; it means nothing here is listening for that session.
// Two rules read it: the channel declines an advert owned by a live OTHER
// session (its wakes go there), and an editor bound to its own session shuts
// down when that session's presence goes (see cmd/galley's watchOwnerSession).

// Session is one live session's presence record.
//
// ID IS SPELLED `session` ON DISK, so the Go name and the key differ and a
// rename on either side looks harmless from the other — which is precisely the
// case the golden key-set test exists for. A record whose `session` key moved
// decodes to an empty ID, SessionLive's closing equality fails, and every
// document that session owns quietly stops being seen as owned.
type Session struct {
	// V is the schema generation — see Version.
	V   int    `json:"v"`
	ID  string `json:"session"`
	PID int    `json:"pid"`
}

// sessionsDir is a SUBDIRECTORY of Dir on purpose: List globs "*.json" in Dir
// itself and does not recurse, so presence records can never be mistaken for
// adverts by a reader that has not been told about them.
func sessionsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, "sessions")
	return sub, os.MkdirAll(sub, 0o700)
}

// sessionFile names the presence file for a session id. A session id comes
// from the caller's harness session id (see cmd/galley's sessionID), so it is
// not this package's to trust as a filename: anything that is not a plain,
// short, path-free token is HASHED rather than refused, because refusing
// would leave the session with no presence at all and turn an odd id into
// permanent invisibility.
func sessionFile(id string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("registry: empty session id")
	}
	return filepath.Join(dir, sessionToken(id)+".json"), nil
}

func sessionToken(id string) string {
	plain := len(id) <= 64 && id != "." && id != ".."
	if plain {
		for _, r := range id {
			if r != '-' && r != '_' && r != '.' &&
				(r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				plain = false
				break
			}
		}
	}
	if plain {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "h-" + hex.EncodeToString(sum[:16])
}

// Token is sessionToken, exported for the one caller outside this package
// that needs a filename derived from an id: the channel's galley_open names an
// editor's log after the page it serves, and a page path is not a filename.
func Token(id string) string { return sessionToken(id) }

// LogDir is where galley_open sends an editor's stdout and stderr — a
// SUBDIRECTORY of Dir, like sessionsDir and for the same reason: Inspect globs
// "*.json" in Dir itself and never recurses, so a log can never be mistaken
// for an advert. 0o700 like everything else here; the registry is per-user.
func LogDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, "logs")
	return sub, os.MkdirAll(sub, 0o700)
}

// AnnounceSession records that this process is listening for `id`. Called by
// `galley channel` at startup; safe to call more than once.
func AnnounceSession(id string) error {
	name, err := sessionFile(id)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(Session{V: Version, ID: id, PID: os.Getpid()}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(name)
	tmp, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// WithdrawSession removes this session's presence record. Best-effort by
// design: a session that dies without running it leaves a record whose PID is
// dead, and SessionLive reaps that on the next read — the same guarantee the
// advert side has always had.
func WithdrawSession(id string) error {
	name, err := sessionFile(id)
	if err != nil {
		return err
	}
	err = os.Remove(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// SessionLive reports whether a channel is currently listening for `id`. A
// record whose PID no longer runs is removed on read, exactly as List reaps a
// dead advert: a stale presence file would pin a document to a session that
// cannot receive anything, which is the silence this whole mechanism exists to
// end.
func SessionLive(id string) bool {
	if id == "" {
		return false
	}
	name, err := sessionFile(id)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return false
	}
	var s Session
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	if ondisk.Future(s.V, Version) {
		// NOT LIVE, AND NOT REAPED. This build cannot read a newer record's
		// rules, and the section comment above says which way to err: a presence
		// file is advisory and its absence means only that nothing HERE is
		// listening. Removing it would be this build deciding a question it has
		// just admitted it cannot answer.
		return false
	}
	if !Alive(s.PID) {
		_ = os.Remove(name)
		return false
	}
	// The token is a hash for an unusual id, so confirm the record is really
	// the one asked for rather than a collision or a hand-edited file.
	return s.ID == id
}

// Alive asks the kernel whether the PID exists. Signal 0 delivers nothing and
// answers only that; EPERM still means "exists". Unix-correct; on Windows
// FindProcess errs for a dead PID, which lands in the same false branch.
//
// EXPORTED BECAUSE IT IS THE ONE LIVENESS RULE, AND THERE MUST NOT BE A SECOND.
// List reaps a dead entry on read; internal/serve's FindRuntime reaps a dead
// <doc>.serve.json on read and refuses to start a second editor on a document a
// LIVE one holds — three questions, one answer, because a stale entry is normal
// (a crash, a SIGKILL, a laptop sleep) and two rules that disagree about what
// "still running" means would let one surface block on a corpse while the other
// attached to it. A recycled PID is the known false positive of asking the
// kernel this way, and it is accepted here for the same reason it always was:
// the alternative is a liveness probe, which answers a different question and
// can hang.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
