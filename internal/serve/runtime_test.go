package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every CLI verb finds a running editor through its advert, without being
// handed a port.
func TestRuntimeAdvertisementRoundTrip(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "doc.md")
	if _, ok := FindRuntime(doc); ok {
		t.Fatal("found a runtime before one was advertised")
	}
	raw, err := json.Marshal(Runtime{URL: "http://127.0.0.1:9999", Room: "doc", Page: doc, PID: os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DefaultRuntimePath(doc), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	rt, ok := FindRuntime(doc)
	if !ok || rt.URL != "http://127.0.0.1:9999" || rt.Room != "doc" {
		t.Fatalf("runtime wrong: %+v ok=%v", rt, ok)
	}
}

func TestDefaultRuntimePathSitsBesideTheDocument(t *testing.T) {
	if got := DefaultRuntimePath("/tmp/x/doc.md"); got != "/tmp/x/doc.serve.json" {
		t.Fatalf("got %s", got)
	}
}
