package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/ondisk"
)

// AN ADVERT AS AN OLDER GALLEY WROTE IT — literal bytes, no `v`. One
// `~/.galley/live/` is shared by every galley on the machine and an editor
// started before an upgrade keeps advertising for hours, so this is not a
// migration case: it is Tuesday.
func TestAnAdvertWrittenBeforeVersionsIsStillFound(t *testing.T) {
	dir := tempLiveDir(t)
	body := fmt.Sprintf(`{"url":"http://127.0.0.1:7788","room":"doc-old","page":"/tmp/doc.md","pid":%d,"owner":"sess-1"}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, "doc-old.json"), []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, problems, err := Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("an older advert is not a problem: %v", problems)
	}
	if len(got) != 1 {
		t.Fatalf("got %d adverts, want the old one — a channel that stops seeing editors is the failure", len(got))
	}
	if got[0].V != 0 || got[0].PID != os.Getpid() || got[0].Owner != "sess-1" || got[0].URL != "http://127.0.0.1:7788" {
		t.Fatalf("advert = %+v", got[0])
	}
	if ondisk.Future(got[0].V, Version) {
		t.Fatal("an absent version must read as the first generation")
	}
}

// A NEWER ADVERT IS REPORTED AND LEFT ALONE. Reaping is a deletion, and this
// build has just said it cannot read the record's rules — deleting it would
// take a live editor's only advert away from every other reader on the machine.
func TestAnAdvertFromANewerGalleyIsReportedAndNotReaped(t *testing.T) {
	dir := tempLiveDir(t)
	name := filepath.Join(dir, "doc-new.json")
	body := fmt.Sprintf(`{"v":99,"url":"http://127.0.0.1:7789","room":"doc-new","page":"/tmp/doc.md","pid":%d,"mood":"cheerful"}`, os.Getpid())
	if err := os.WriteFile(name, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, problems, err := Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a record this build cannot read must not be acted on: %+v", got)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "v99") {
		t.Fatalf("problems = %v, want the file named and the reason given", problems)
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("the advert was deleted — it is somebody else's: %v", err)
	}
}

// A newer PRESENCE record answers "not live" and is likewise left on disk. The
// section comment says which way to err: absence means only that nothing HERE
// is listening.
func TestASessionFromANewerGalleyIsNotLiveAndNotReaped(t *testing.T) {
	dir := tempLiveDir(t)
	sub := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(sub, "sess-9.json")
	body := fmt.Sprintf(`{"v":99,"session":"sess-9","pid":%d}`, os.Getpid())
	if err := os.WriteFile(name, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if SessionLive("sess-9") {
		t.Fatal("this build cannot read a newer record's rules and must not vouch for it")
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("the presence file was deleted — that is this build answering a question it cannot: %v", err)
	}
}

// A presence record from an older galley is exactly the shape this field was
// added to, and must keep answering.
func TestASessionWrittenBeforeVersionsIsStillLive(t *testing.T) {
	dir := tempLiveDir(t)
	sub := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"session":"sess-old","pid":%d}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(sub, "sess-old.json"), []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SessionLive("sess-old") {
		t.Fatal("an older presence record stopped answering — every document that session owns just lost its owner")
	}
}

// THE KEY SET IS THE CONTRACT — see internal/ondisk.Keys. The registry
// tolerates unknown keys by design, so nothing on the reading side can notice a
// rename; this list is the only check that can.
//
// `session` is the case it is most needed for: Session.ID is spelled `session`
// on disk, so the Go name and the key already differ and a rename on either
// side looks harmless from the other.
func TestTheAdvertKeysAreTheContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want string
	}{
		{"Entry", Entry{V: 1, URL: "u", Room: "r", Page: "p", PID: 1, Owner: "o"}, "v,url,room,page,pid,owner"},
		{"Session", Session{V: 1, ID: "s", PID: 1}, "v,session,pid"},
	} {
		keys, err := ondisk.Keys(tc.val)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keys, ","); got != tc.want {
			t.Errorf("%s keys = %s\n              want %s\n"+
				"a renamed key decodes to a zero value in silence: PID 0 is a corpse, an empty owner is unowned", tc.name, got, tc.want)
		}
	}
}
