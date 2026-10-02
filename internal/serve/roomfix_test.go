package serve

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
	ygsync "github.com/reearth/ygo/sync"
)

// wsOuterSync is the y-websocket outer envelope tag for a sync message
// (tag 0). ygo's provider/websocket package documents its wire framing as
// "tag + payload" with "[t]ags 0-3 ... the y-protocols / y-websocket core"
// (provider/websocket/doc.go) but keeps the tag constants themselves
// unexported (msgSync et al. in server.go) — mirrored here since a
// black-box test in this package can't reach them.
const wsOuterSync = uint64(0)

// dialRoom opens a real WebSocket connection to the server's y-websocket
// endpoint for its one room — the URL EditServer mounts at /yjs/{room} (see
// Handler in editmode.go).
func dialRoom(t *testing.T, ts *httptest.Server, room string) *gws.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/yjs/" + room
	conn, resp, err := gws.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", u, err)
	}
	_ = resp.Body.Close()
	return conn
}

// drainHandshake reads the three frames ygo's ws.Server always sends a
// connecting peer (sync step-1, sync step-2, awareness) and applies any
// sync payload into doc, so the client-side doc ends up synced. Cribbed from
// ygo's own provider/websocket persistence_coalesce_test.go drainWS, which
// notes a fixed read count is required because gorilla's reader is
// permanently broken by a deadline expiry.
func drainHandshake(t *testing.T, conn *gws.Conn, doc *crdt.Doc) {
	t.Helper()
	for i := 0; i < 3; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			t.Fatalf("read handshake frame %d: %v", i, err)
		}
		dec := encoding.NewDecoder(data)
		outer, err := dec.ReadVarUint()
		if err != nil {
			t.Fatalf("decode handshake frame %d: %v", i, err)
		}
		if outer == wsOuterSync {
			if _, err := ygsync.ApplySyncMessage(doc, dec.RemainingBytes(), nil); err != nil {
				t.Fatalf("apply sync frame %d: %v", i, err)
			}
		}
	}
}

// TestEditServer_RoomSurvivesPeerDisconnect reproduces the room-orphaning bug
// found by the editor-bundle task's live verification: internal/serve pins
// s.doc = yjs.GetDoc(room) once, in the constructor, forever. ws.NewServer's
// default RoomIdleTimeout is 0 (eager-evict), so the moment a page's only
// websocket peer disconnects — a reload, a tab close, a transient drop — the
// room is destroyed. The next connection then creates a FRESH room with a
// FRESH *crdt.Doc, and every later browser edit lands there, invisible to
// the s.doc this EditServer is holding and invisible to Project,
// /_galley/pending and pending.json, which all read s.doc. Silent data loss.
//
// Before the fix (yjs.RoomIdleTimeout left at its zero-value default) this
// test fails: s.yjs.GetDoc(s.Room) after disconnect+wait returns a *different*
// doc than the one the constructor pinned (or the room vanishes and
// GetDoc returns nil), and the post-wait write below reaches that different
// doc instead of the pinned one.
func TestEditServer_RoomSurvivesPeerDisconnect(t *testing.T) {
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "hello\n")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	pinnedDoc := s.doc // what the constructor pinned into s.doc

	// Connect the one peer this "page" will ever have, complete the sync
	// handshake, then disconnect it — the reload/tab-close/transient-drop
	// scenario from the bug report.
	clientDoc := crdt.New(crdt.WithClientID(1))
	conn := dialRoom(t, ts, s.Room)
	drainHandshake(t, conn, clientDoc)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	// Wait comfortably past the window in which ygo's eager-evict teardown
	// (handleDisconnect) runs synchronously off the connection's read loop —
	// long enough that, on unfixed code, the room is long gone by the time we
	// look.
	time.Sleep(500 * time.Millisecond)

	// The room must still be resident, and it must still be the SAME doc the
	// constructor pinned — not a fresh one materialised by a later touch.
	live := s.yjs.GetDoc(s.Room)
	if live == nil {
		t.Fatal("room must still exist after its only peer disconnected")
	}
	if live != pinnedDoc {
		t.Fatal("the server's pinned doc must still be the room's doc after disconnect+wait")
	}

	// Prove it's not merely resident but still LIVE: write to the room the
	// way the server itself always does — ws.Server.Apply, never doc.Transact
	// directly (see CLAUDE.md: a reply written straight to the document is
	// correct on disk and invisible on the reviewer's screen, because Apply is
	// what captures the update and broadcasts it) — and confirm the pinned
	// doc observes it.
	if err := s.yjs.Apply(context.Background(), s.Room,
		func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
			probe := doc.GetMap("roomfixProbe") // resolve the handle before the transaction opens (CLAUDE.md)
			transact(func(txn *crdt.Transaction) {
				probe.Set(txn, "after-disconnect", "reached-the-pinned-doc")
			})
		}); err != nil {
		t.Fatal(err)
	}

	// Read outside any Transact — CLAUDE.md: Transact holds the document's
	// write lock and a read inside it deadlocks silently.
	probe := s.doc.GetMap("roomfixProbe")
	v, ok := probe.Get("after-disconnect")
	if !ok {
		t.Fatal("the post-disconnect write must land on the pinned doc")
	}
	if v != "reached-the-pinned-doc" {
		t.Fatalf("probe value = %v", v)
	}
}
