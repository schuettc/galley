package serve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/ondisk"
)

// oldLease is `handoff.json` AS AN OLDER GALLEY WROTE IT — literal bytes, no
// `v`. A window can be open across an upgrade, and a lease this build refuses
// is an agent's in-flight round resumed as an anonymous `opened` one.
const oldLease = `{
  "round": 7,
  "baseline": "b0",
  "fingerprint": "fp0",
  "openedAt": "2026-08-20T10:00:00Z",
  "lastImported": "d9",
  "approveOnAnswer": true
}
`

func leaseAt(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "handoff.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE BACKWARDS COMPATIBILITY PROMISE FOR THE LEASE. Strict is the one place in
// this change that refuses an unknown key, and it must not refuse the shape it
// was added to.
func TestALeaseWrittenBeforeVersionsStillResumes(t *testing.T) {
	l, ok, err := readLease(leaseAt(t, oldLease))
	if err != nil || !ok {
		t.Fatalf("readLease = %v, %v — an open window across an upgrade must resume", ok, err)
	}
	if l.V != 0 {
		t.Fatalf("V = %d, want 0 — the absent field must not be invented on read", l.V)
	}
	if l.Round != 7 || l.Baseline != "b0" || l.Fingerprint != "fp0" ||
		l.LastImported != "d9" || !l.ApproveOnAnswer {
		t.Fatalf("lease = %+v — every field decides how the round is attributed", l)
	}
	if l.OpenedAt.IsZero() {
		t.Fatal("openedAt lost — the resumed watch is stamped from it")
	}
}

// A REWRITE STAMPS THE GENERATION. importDraft writes the lease back on every
// import, and a resumed v0 lease that kept claiming no version would leave the
// file behind this build forever.
func TestRewritingAResumedLeaseStampsTheGeneration(t *testing.T) {
	path := leaseAt(t, oldLease)
	l, _, err := readLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLease(path, l); err != nil {
		t.Fatal(err)
	}
	back, _, err := readLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.V != handoffLeaseVersion {
		t.Fatalf("V = %d, want %d", back.V, handoffLeaseVersion)
	}
	if back.Round != 7 || !back.ApproveOnAnswer {
		t.Fatalf("the rewrite lost state: %+v", back)
	}
}

// THE ONE STRICT FORMAT. A key this build does not know means the file was
// written against a shape it cannot honour, and honouring half of it resumes a
// window answering the wrong round — Round 0, no fingerprint, no
// approve-on-answer, with the agent's work laundered into an anonymous version.
// Refusing costs a resume; the quarantine in editmode.go keeps the evidence.
func TestALeaseWithARenamedKeyIsRefusedRatherThanHalfBelieved(t *testing.T) {
	// `round` renamed to `roundN` — the exact silent rename this change exists
	// to make loud. Nothing else about the file is wrong.
	renamed := strings.Replace(oldLease, `"round":`, `"roundN":`, 1)
	l, ok, err := readLease(leaseAt(t, renamed))
	if err == nil {
		t.Fatalf("a renamed key decoded in silence: lease = %+v, ok = %v", l, ok)
	}
	if ok {
		t.Fatal("a refused lease must not be reported as present")
	}
	if !strings.Contains(err.Error(), "roundN") {
		t.Fatalf("the refusal must name the key it did not know, got %v", err)
	}
}

// A lease from a NEWER galley is refused the same way and for the same reason,
// with a sentence naming the remedy — internal/ledger/index.go's shape.
func TestALeaseFromANewerGalleyIsRefused(t *testing.T) {
	_, ok, err := readLease(leaseAt(t, `{"v":99,"round":7,"baseline":"b0","fingerprint":"fp0","openedAt":"2026-08-20T10:00:00Z"}`))
	if err == nil || ok {
		t.Fatal("a lease this build cannot read must not be resumed")
	}
	if !errors.Is(err, ondisk.ErrFuture) {
		t.Fatalf("err = %v, want the ErrFuture sentinel", err)
	}
	if !strings.Contains(err.Error(), "v99") || !strings.Contains(err.Error(), "v1") {
		t.Fatalf("refusal %q must say what it found and what it knows", err)
	}
}

// A missing lease is still no lease and no error — the ordinary state of a
// document with no window open. The strict decoder must not have changed that.
func TestNoLeaseIsStillNotAnError(t *testing.T) {
	if _, ok, err := readLease(filepath.Join(t.TempDir(), "nope.json")); ok || err != nil {
		t.Fatalf("readLease on a missing file = %v, %v", ok, err)
	}
}

// The lease is PRIVATE and strict, so its key set is enforced by the decoder
// rather than by a golden list — but only for keys the file ADDS. A key
// REMOVED from the struct is invisible to a strict decoder too (the file simply
// stops carrying it), so the list is pinned here as well.
func TestTheLeaseKeysAreTheContract(t *testing.T) {
	keys, err := ondisk.Keys(handoffLease{V: 1, Round: 1, Baseline: "b", Fingerprint: "f",
		LastImported: "l", ApproveOnAnswer: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "v,round,baseline,fingerprint,openedAt,lastImported,approveOnAnswer"
	if got := strings.Join(keys, ","); got != want {
		t.Fatalf("lease keys = %s\n          want %s", got, want)
	}
}
