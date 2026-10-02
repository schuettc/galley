package serve

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// onePixelPNG is a real, decodable 1x1 PNG — not a placeholder string — so the
// test proves bytes reach the client unaltered rather than merely that some
// 200 was answered.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func editAssetServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	s := newEditServer(t, dir, "doc.md", "# Spec\n\n![a figure](pic.png)\n")
	t.Cleanup(func() { _ = s.Close() })
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

// The bug this exists to fix: `![alt](pic.png)` beside the document 404'd,
// because edit mode served the shell and nothing else.
func TestEditServesASiblingImage(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /pic.png = %s, want 200", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got == "" {
		t.Error("no Content-Security-Policy on a user-content response")
	}
	body := make([]byte, len(onePixelPNG)+8)
	n, _ := resp.Body.Read(body)
	if !bytes.Equal(body[:n], onePixelPNG) {
		t.Errorf("body = %x, want the png verbatim", body[:n])
	}
}

// SVG is served renderable, on purpose (see assetTypes) — text/plain would be
// safe and would also mean no diagram ever displays.
func TestEditServesSVGAsAnImageWithASandbox(t *testing.T) {
	ts, dir := editAssetServer(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	if err := os.WriteFile(filepath.Join(dir, "diagram.svg"), svg, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/diagram.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("Content-Type = %q, want image/svg+xml — an SVG served any other way does not render", ct)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "default-src 'none'; sandbox" {
		t.Errorf("Content-Security-Policy = %q — an SVG can carry <script>, so a direct "+
			"navigation must land in an opaque origin", csp)
	}
}

func TestEditRefusesEverythingButImages(t *testing.T) {
	ts, dir := editAssetServer(t)
	// A secret that happens to live beside the document, and a source file.
	for name, body := range map[string]string{
		".env":         "AWS_SECRET_ACCESS_KEY=hunter2\n",
		"notes.txt":    "private\n",
		"doc.md":       "# Spec\n",
		"script.js":    "alert(1)\n",
		"doc.comments": "{}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		resp, err := http.Get(ts.URL + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /%s = %s, want 404 — only the image allowlist is served", name, resp.Status)
		}
	}
}

func TestEditRefusesTraversal(t *testing.T) {
	ts, dir := editAssetServer(t)
	outside := filepath.Join(filepath.Dir(dir), "outside.png")
	if err := os.WriteFile(outside, onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	for _, p := range []string{
		"/../outside.png",
		"/./../outside.png",
		"/%2e%2e/outside.png",
		"/sub/../../outside.png",
		"//etc/hosts.png",
	} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body := make([]byte, 8)
		n, _ := resp.Body.Read(body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && bytes.Equal(body[:n], onePixelPNG[:n]) {
			t.Errorf("GET %s served the file outside the document's directory", p)
		}
	}
}

// A textually-contained path can still point outside: EvalSymlinks is what
// makes the containment check about physical location rather than spelling.
func TestEditRefusesASymlinkEscapingTheDirectory(t *testing.T) {
	ts, dir := editAssetServer(t)
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "innocent.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	resp, err := http.Get(ts.URL + "/innocent.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /innocent.png = %s, want 404 — the symlink resolves outside the document's directory",
			resp.Status)
	}
}

// HEAD is allowed and everything but GET/HEAD is refused — as a 404, not a
// 405, so a refusal is byte-identical to an absence and this route never
// confirms it exists.
//
// HEAD used to be refused too, and a test asserted it. There is no security
// argument for that: GET already returns strictly more, http.ServeContent
// answers a HEAD for free, and refusing one only deviates from HTTP.
func TestEditAssetsAreReadOnly(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	head, err := http.Head(ts.URL + "/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD /pic.png = %s, want 200", head.Status)
	}
	if ct := head.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("HEAD Content-Type = %q, want image/png", ct)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req, err := http.NewRequest(method, ts.URL+"/pic.png", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s /pic.png = %s, want 404", method, resp.Status)
		}
	}
}

// A directory whose name ends in an allowlisted extension must not list or
// serve anything.
func TestEditRefusesADirectory(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.Mkdir(filepath.Join(dir, "assets.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/assets.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /assets.png = %s, want 404", resp.Status)
	}
}

// An image in a subdirectory beside the document is legitimate — "![](img/a.png)"
// — and must still work.
func TestEditServesASubdirectoryImage(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.Mkdir(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "a.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/img/a.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /img/a.png = %s, want 200", resp.Status)
	}
}

// The hole the extension allowlist left open: it was checked on r.URL.Path,
// but the bytes were read from the EvalSymlinks-RESOLVED path and the
// extension was never re-checked after resolution. A symlink INSIDE the
// document's directory pointing at a non-image IN THE SAME DIRECTORY passes
// every containment check and is served under an allowlisted MIME type.
//
//	ln -s .env innocent.png
//	curl localhost:8123/innocent.png
//	# 200, Content-Type: image/png, body: AWS_SECRET=hunter2
//
// This falsifies editassets.go's own stated invariant — "adding a new kind of
// file to that directory can never widen what this exposes". It can: the file
// that widened it was already there.
func TestEditRefusesASymlinkToANonImageInside(t *testing.T) {
	ts, dir := editAssetServer(t)
	secret := filepath.Join(dir, ".env")
	if err := os.WriteFile(secret, []byte("AWS_SECRET_ACCESS_KEY=hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "innocent.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	resp, err := http.Get(ts.URL + "/innocent.png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /innocent.png = %s, want 404 — the resolved file is not an image", resp.Status)
	}
	if bytes.Contains(body, []byte("hunter2")) {
		t.Errorf("served the secret as %q: %s", resp.Header.Get("Content-Type"), body)
	}
}

// A symlinked DIRECTORY component, which the suite never pinned. The code
// handles it correctly today; nothing held it there, so a refactor to an
// os.Lstat on the final component alone would have passed the whole suite.
func TestEditRefusesASymlinkedDirectoryComponent(t *testing.T) {
	ts, dir := editAssetServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa.png"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "ssh")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	resp, err := http.Get(ts.URL + "/ssh/id_rsa.png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /ssh/id_rsa.png = %s, want 404", resp.Status)
	}
	if bytes.Contains(body, []byte("PRIVATE KEY")) {
		t.Error("leaked the contents of a symlinked directory outside the document's")
	}
}

// The figure route is a GET, so guard — which requires POST and a JSON body —
// cannot apply to it. But guard's own reasoning ("THE THREAT IS AN ORDINARY
// WEB PAGE; galley listens on 127.0.0.1 with no authentication") is about
// reads too, and nothing here reflected it.
//
// An attacker cannot read the bytes: there is no CORS header and canvas taint
// blocks pixel extraction. What they get is a cross-origin EXISTENCE-AND-
// DIMENSIONS ORACLE over the whole tree below the document, from any page in
// the user's browser, against a port that is a fixed default:
//
//	<img src="http://127.0.0.1:8123/Pictures/2024/private.jpg"
//	     onload="report('exists', this.naturalWidth, this.naturalHeight)"
//	     onerror="report('absent')">
//
// Cross-Origin-Resource-Policy: same-origin makes the browser refuse the embed
// outright, and affects no legitimate same-origin load.
func TestEditFiguresAreNotEmbeddableCrossOrigin(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/pic.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Dest", "image")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want same-origin — without it any page "+
			"in the browser can probe which files exist under the document's directory", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q — the bytes must never be readable cross-origin", got)
	}
}

// A real file whose NAME contains "..". strings.Contains(clean, "..") is a
// substring test, so "fig..final.png" — a perfectly ordinary filename — 404s
// with no explanation. The check is load-bearing only for Windows backslashes
// and belongs on a path SEGMENT.
func TestEditServesAFilenameContainingDots(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.WriteFile(filepath.Join(dir, "fig..final.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/fig..final.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /fig..final.png = %s, want 200 — \"..\" inside a NAME is not traversal", resp.Status)
	}
}

// Backslash traversal: the only reason the ".." check exists at all, and
// `just verify` cross-builds for Windows. On a Unix host a backslash is an
// ordinary filename character, so these are 404s either way — the test is here
// so the Windows-relevant vectors are on record rather than assumed.
func TestEditRefusesBackslashTraversal(t *testing.T) {
	ts, dir := editAssetServer(t)
	outside := filepath.Join(filepath.Dir(dir), "outside.png")
	if err := os.WriteFile(outside, onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	for _, p := range []string{
		`/..\..\outside.png`,
		`/..%5c..%5coutside.png`,
		`/%2e%2e%5coutside.png`,
	} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s = 200, want a refusal", p)
		}
		if bytes.Equal(body, onePixelPNG) {
			t.Errorf("GET %s served the file outside the document's directory", p)
		}
	}
}

// Uppercase extensions are real — a phone writes IMG_0001.JPG — and the
// allowlist lookup already lower-cases. Pinned because the extension check is
// about to move to the resolved path, where it is easy to drop the fold.
func TestEditServesAnUppercaseExtension(t *testing.T) {
	ts, dir := editAssetServer(t)
	if err := os.WriteFile(filepath.Join(dir, "FIG.PNG"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + "/FIG.PNG")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /FIG.PNG = %s, want 200", resp.Status)
	}
}

// Committed, never built, so it is served from any checkout, including one
// where `just build`/`just assets` never ran.
func TestFaviconIsAlwaysServed(t *testing.T) {
	ts, _ := editAssetServer(t)
	resp, err := http.Get(ts.URL + "/_galley/favicon.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatalf("favicon.svg not served: %d, %d bytes", resp.StatusCode, len(body))
	}
	if got := resp.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("served as %q, want image/svg+xml", got)
	}
}
