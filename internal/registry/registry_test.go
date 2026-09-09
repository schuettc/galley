package registry

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func tempLiveDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GALLEY_LIVE_DIR", dir)
	return dir
}

func TestWriteListRemoveRoundTrip(t *testing.T) {
	tempLiveDir(t)
	e := Entry{URL: "http://127.0.0.1:1", Room: "doc-abc", Page: "/tmp/doc.md", PID: os.Getpid(), Owner: "sess-1"}
	if err := Write(e); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	// Write stamps the schema generation — a caller cannot know it and must not
	// be asked to — so the entry that comes back carries it.
	e.V = Version
	if len(got) != 1 || got[0] != e {
		t.Fatalf("List = %+v, want the entry back", got)
	}
	if err := Remove("doc-abc"); err != nil {
		t.Fatal(err)
	}
	got, _ = List()
	if len(got) != 0 {
		t.Fatalf("removed entry still listed: %+v", got)
	}
}

// A dead PID's entry is a lie about a listener — reaped on read, and the FILE
// goes too, so the next scan does not re-report it.
func TestListReapsDeadPIDs(t *testing.T) {
	dir := tempLiveDir(t)
	// PID 1 is init and never ours; a fake huge PID is reliably dead.
	dead := Entry{URL: "http://127.0.0.1:1", Room: "gone", Page: "/tmp/g.md", PID: 99999999}
	if err := Write(dead); err != nil {
		t.Fatal(err)
	}
	live := Entry{URL: "http://127.0.0.1:2", Room: "here", Page: "/tmp/h.md", PID: os.Getpid()}
	if err := Write(live); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Room != "here" {
		t.Fatalf("List = %+v, want only the live entry", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); !os.IsNotExist(err) {
		t.Error("the dead entry's file was not reaped")
	}
}

// An empty owner is UNOWNED, not owned-by-"". The JSON must omit it so a
// human reading the file sees the absence rather than an empty string.
func TestUnownedEntryOmitsOwner(t *testing.T) {
	dir := tempLiveDir(t)
	if err := Write(Entry{URL: "http://127.0.0.1:1", Room: "r", Page: "/tmp/r.md", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "" && containsOwner(raw) {
		t.Errorf("unowned entry serialized an owner field: %s", raw)
	}
}

// A room built from a path traversal is not a room; Write and Remove must
// both refuse it, and Write must leave the registry dir untouched.
func TestWriteRemoveRejectTraversal(t *testing.T) {
	dir := tempLiveDir(t)
	const bad = "../../../etc/cron.d/x"

	if err := Write(Entry{URL: "u", Room: bad, Page: "p", PID: os.Getpid()}); err == nil {
		t.Fatal("Write accepted a traversal room")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Write with a traversal room left files behind: %+v", entries)
	}

	if err := Remove(bad); err == nil {
		t.Fatal("Remove accepted a traversal room")
	}
}

func containsOwner(raw []byte) bool {
	for i := 0; i+7 <= len(raw); i++ {
		if string(raw[i:i+7]) == `"owner"` {
			return true
		}
	}
	return false
}

// --- what List refuses to report ---
//
// List is the channel's whole view of the world, and it validated NOTHING: any
// file in the directory that happened to parse became an editor to attach to,
// whatever it said. Three of the four things below were measured as real
// harm; the fourth is the one that reaches a model's context.

// ONE EDITOR, TWO ADVERTS, EVERY WAKE DOUBLED. Two entries at one URL are one
// server, and attaching to both means one Revise press proposes two rounds of
// work. Measured from a planted file and from two same-session channels alike.
func TestListReportsOneEntryPerURL(t *testing.T) {
	tempLiveDir(t)
	first := Entry{URL: "http://127.0.0.1:9001", Room: "doc-a", Page: "/tmp/doc.md", PID: os.Getpid()}
	second := first
	second.Room = "doc-a-duplicate"
	for _, e := range []Entry{first, second} {
		if err := Write(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("List reported %d entries for one server: %+v", len(got), got)
	}
}

// AN ENTRY THAT DISAGREES WITH ITS OWN FILENAME IS NOT ONE THIS PACKAGE WROTE.
// The filename IS the room — it is what Remove deletes and what the websocket
// gate checks — so a file whose contents name a different room is a lie about
// which room it is, and the honest answer is to leave it alone rather than
// hand a channel a room nothing can withdraw.
func TestListRefusesAnEntryWhoseRoomIsNotItsFilename(t *testing.T) {
	dir := tempLiveDir(t)
	raw := `{"url":"http://127.0.0.1:9002","room":"some-other-room","page":"/tmp/x.md","pid":` +
		strconv.Itoa(os.Getpid()) + `}`
	if err := os.WriteFile(filepath.Join(dir, "planted.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("List reported an entry that renamed itself: %+v", got)
	}
	// NOT REAPED: it is not this package's file to delete. A dead PID is a
	// fact about the world; a mismatched room is a fact about the file, and a
	// human put it there.
	if _, err := os.Stat(filepath.Join(dir, "planted.json")); err != nil {
		t.Errorf("List deleted a file it merely did not understand: %v", err)
	}
}

// THE URL AND THE PAGE REACH A MODEL'S CONTEXT. The channel's notification
// text is built out of the page, and the URL is the address it will issue
// requests against, so both are untrusted input from this package's point of
// view: a control character, a newline that could forge a second line of a
// <channel> tag, a scheme that is not http — none of them describe a galley
// editor, and none of them are reported.
func TestListRefusesEntriesItCannotTrust(t *testing.T) {
	dir := tempLiveDir(t)
	pid := strconv.Itoa(os.Getpid())
	for _, tc := range []struct{ name, body string }{
		{"scheme", `{"url":"file:///etc/passwd","room":"scheme","page":"/tmp/x.md","pid":` + pid + `}`},
		{"nohost", `{"url":"http://","room":"nohost","page":"/tmp/x.md","pid":` + pid + `}`},
		{"junkurl", `{"url":"not a url at all","room":"junkurl","page":"/tmp/x.md","pid":` + pid + `}`},
		{"newline", `{"url":"http://127.0.0.1:9003","room":"newline","page":"/tmp/a.md\nsuggestions=99","pid":` + pid + `}`},
		{"relative", `{"url":"http://127.0.0.1:9004","room":"relative","page":"x.md","pid":` + pid + `}`},
		{"nopage", `{"url":"http://127.0.0.1:9005","room":"nopage","page":"","pid":` + pid + `}`},
		{"nopid", `{"url":"http://127.0.0.1:9006","room":"nopid","page":"/tmp/x.md","pid":0}`},
	} {
		if err := os.WriteFile(filepath.Join(dir, tc.name+".json"), []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("List reported entries it cannot trust: %+v", got)
	}
	// And Inspect says so, one problem per file, so a channel can tell a
	// reviewer why their editor is invisible instead of shrugging.
	_, problems, err := Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 7 {
		t.Fatalf("Inspect reported %d problems, want one per planted file: %+v", len(problems), problems)
	}
}

// Write is the other side of the same rule, and the loud side: an advertiser
// that cannot be trusted learns at the moment it writes rather than being
// silently invisible to every channel forever.
func TestWriteRefusesAnEntryListWouldNotTrust(t *testing.T) {
	tempLiveDir(t)
	for _, e := range []Entry{
		{URL: "file:///x", Room: "a", Page: "/tmp/x.md", PID: os.Getpid()},
		{URL: "http://127.0.0.1:9007", Room: "b", Page: "relative.md", PID: os.Getpid()},
		{URL: "http://127.0.0.1:9008", Room: "c", Page: "/tmp/x.md", PID: 0},
	} {
		if err := Write(e); err == nil {
			t.Errorf("Write accepted an entry List would refuse: %+v", e)
		}
	}
}

// --- session presence ---

// A channel announces its session, and that record is what makes "is the
// session that owns this document still here" a question with an answer.
func TestSessionPresenceRoundTrip(t *testing.T) {
	tempLiveDir(t)
	if SessionLive("sess-1") {
		t.Fatal("a session nobody announced reads as live")
	}
	if err := AnnounceSession("sess-1"); err != nil {
		t.Fatal(err)
	}
	if !SessionLive("sess-1") {
		t.Fatal("an announced session reads as gone")
	}
	if err := WithdrawSession("sess-1"); err != nil {
		t.Fatal(err)
	}
	if SessionLive("sess-1") {
		t.Fatal("a withdrawn session still reads as live")
	}
}

// A session that died without withdrawing — which is every crash, and every
// Claude Code restart — is reaped on read, the same guarantee List gives for
// an advert. Without it a single crash would pin a document to a session that
// can never receive anything again.
func TestSessionPresenceReapsADeadChannel(t *testing.T) {
	tempLiveDir(t)
	if err := AnnounceSession("sess-dead"); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "sessions", "sess-dead.json")
	if err := os.WriteFile(name, []byte(`{"session":"sess-dead","pid":99999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if SessionLive("sess-dead") {
		t.Fatal("a session whose channel is gone reads as live")
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Error("the dead session's presence file was not reaped")
	}
}

// A session id is an environment variable, so it is not a filename. An id
// carrying a path separator must not write outside the sessions directory —
// and must still WORK, because refusing it would make that session invisible
// rather than safe.
func TestSessionPresenceSurvivesAHostileID(t *testing.T) {
	tempLiveDir(t)
	const hostile = "../../../etc/cron.d/x"
	if err := AnnounceSession(hostile); err != nil {
		t.Fatal(err)
	}
	if !SessionLive(hostile) {
		t.Fatal("an unusual session id lost its presence record")
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("hostile id wrote %d files under sessions/: %+v", len(entries), entries)
	}
	if strings.ContainsAny(entries[0].Name(), `/\`) {
		t.Errorf("the presence filename carries a path separator: %q", entries[0].Name())
	}
}

// Token is sessionToken exported, because the channel needs a filename for an
// editor's log and a page path is exactly the kind of id that is not a
// filename. Same rule both ways: plain ids pass through, anything else is
// hashed rather than refused.
func TestTokenPassesPlainIDsAndHashesTheRest(t *testing.T) {
	if got := Token("01a08409-2c9b-773e-a688-3eafc9464bba"); got != "01a08409-2c9b-773e-a688-3eafc9464bba" {
		t.Fatalf("a plain id was rewritten: %q", got)
	}
	got := Token("/Users/court/docs/plan.md")
	if !strings.HasPrefix(got, "h-") || len(got) != len("h-")+32 {
		t.Fatalf("a path was not hashed to h-<32 hex>: %q", got)
	}
	if Token("/a/b.md") == Token("/a/c.md") {
		t.Fatal("two different paths hashed to one token")
	}
}

func TestLogDirLivesUnderTheRegistryDir(t *testing.T) {
	dir := tempLiveDir(t)
	got, err := LogDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "logs"); got != want {
		t.Fatalf("LogDir = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("LogDir did not create %s: %v", got, err)
	}
}
