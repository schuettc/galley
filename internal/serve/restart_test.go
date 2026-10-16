package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/ydoc"
)

const restartDoc = "# Title\n\nFirst paragraph.\n\nSecond paragraph.\n"

func writeRestartDoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(restartDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The hazard the room token exists to make unreachable.
//
// This is not a test of galley's behaviour — it is a test of the CRDT's, kept
// executable so nobody removes the guard believing the merge would be
// harmless. Two runs of NewEdit over one file build two documents that share
// no item IDs, because each run generates a fresh client ID and inserts the
// parsed content from empty. Merging them keeps everything from both, which is
// the only correct thing a CRDT can do with two documents it cannot tell apart
// — and it means the reviewer's document appears twice, and the next
// projection writes the doubled text to disk.
//
// Court saw this as a 21-line document becoming 41 lines.
func TestMergingTwoRunsWouldDuplicateTheDocument(t *testing.T) {
	path := writeRestartDoc(t)

	first, err := NewEdit(path)
	if err != nil {
		t.Fatalf("first NewEdit: %v", err)
	}
	// What a browser tab still holds when the server it was talking to dies.
	peerState := first.doc.EncodeStateAsUpdate()

	second, err := NewEdit(path)
	if err != nil {
		t.Fatalf("second NewEdit: %v", err)
	}
	before, err := ydoc.ReadLive(second.doc)
	if err != nil {
		t.Fatalf("read before merge: %v", err)
	}
	if err := second.doc.ApplyUpdate(peerState); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	after, err := ydoc.ReadLive(second.doc)
	if err != nil {
		t.Fatalf("read after merge: %v", err)
	}

	if len(after.Blocks) <= len(before.Blocks) {
		t.Fatalf("this test documents why onlyThisRoom exists, and the premise "+
			"no longer holds: merging a previous run's state left %d blocks, "+
			"up from %d. If a CRDT merge across runs is now safe, the room "+
			"token and its guard can go — but check before deleting them",
			len(after.Blocks), len(before.Blocks))
	}
}

// Two runs over the same file never share a room, so the merge above has no
// way to happen over the wire.
func TestRestartChangesTheRoom(t *testing.T) {
	path := writeRestartDoc(t)

	first, err := NewEdit(path)
	if err != nil {
		t.Fatalf("first NewEdit: %v", err)
	}
	second, err := NewEdit(path)
	if err != nil {
		t.Fatalf("second NewEdit: %v", err)
	}

	if first.Room == second.Room {
		t.Errorf("both runs claimed room %q; a tab that outlived a restart "+
			"would reconnect and duplicate the document", first.Room)
	}
	// The basename still leads, so the URL stays legible.
	if !strings.HasPrefix(second.Room, "doc-") {
		t.Errorf("room %q should still be named after the document", second.Room)
	}
}

// A peer holding a previous run's room is refused before the websocket
// upgrade — the point at which its state would otherwise enter the document.
func TestStaleRoomIsRefusedBeforeUpgrade(t *testing.T) {
	path := writeRestartDoc(t)

	first, err := NewEdit(path)
	if err != nil {
		t.Fatalf("first NewEdit: %v", err)
	}
	second, err := NewEdit(path)
	if err != nil {
		t.Fatalf("second NewEdit: %v", err)
	}

	srv := httptest.NewServer(second.Handler())
	defer srv.Close()

	// Exactly what a reconnecting WebsocketProvider asks for.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/yjs/"+first.Room, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusGone {
		t.Errorf("stale room got %d, want %d — a previous run's peer must never "+
			"reach the ws server", resp.StatusCode, http.StatusGone)
	}
}

// The current room reaches the page through the poll it already runs, which is
// how a stale tab learns to reload.
func TestRevReportsTheCurrentRoom(t *testing.T) {
	path := writeRestartDoc(t)

	s, err := NewEdit(path)
	if err != nil {
		t.Fatalf("NewEdit: %v", err)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/_galley/rev")
	if err != nil {
		t.Fatalf("get /rev: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got struct {
		Rev  int64  `json:"rev"`
		Room string `json:"room"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Room != s.Room {
		t.Errorf("/rev reported room %q, want %q", got.Room, s.Room)
	}
	if got.Rev == 0 {
		t.Error("/rev dropped the mtime while gaining the room")
	}
}
