package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/versions"
)

func post(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(http.MethodPost, path, nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	}
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func rounds(t *testing.T, s *EditServer) []versions.Round {
	t.Helper()
	rs, err := s.Versions().List()
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func press(t *testing.T, s *EditServer, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/_galley/revise", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Revise answered %d: %s", rec.Code, rec.Body.String())
	}
}

func commentAs(t *testing.T, s *EditServer, target, instruction string) {
	t.Helper()
	rec := post(t, s.Handler(), "/_galley/instruct", map[string]any{
		"op": "comment", "target": target, "text": instruction, "author": "court",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("instruction answered %d: %s", rec.Code, rec.Body.String())
	}
}

func newEditServer(t *testing.T, dir, name, content string) *EditServer {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// waitFor blocks until at least want notifications have landed. AT LEAST: a
// caller asserting an exact count must count again after a settling window of
// its own — see TestNotifierFiresOnceTouchesSettle.
func waitFor(t *testing.T, marker string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countLines(t, marker) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notification never fired (%s)", marker)
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(bytes.Fields(raw))
}

func threadByKey(doc *crdt.Doc, key string) (review.Thread, bool) {
	for _, thread := range review.Read(doc) {
		if thread.Key == key {
			return thread, true
		}
	}
	return review.Thread{}, false
}
