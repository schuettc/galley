package serve

import (
	"embed"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// assets carries the browser half: the editor bundle (editor.js, editor.css,
// mermaid.js), built by `just assets` and committed, and the favicon.
//
//go:embed assets
var assets embed.FS

// serveAssetHint serves one embedded file, and a missing one says what to run
// rather than 404ing, because the page fails silently otherwise. The hint is
// the caller's because not every asset comes from the same place: the editor
// bundle from `just assets`, the favicon from the checkout itself. Naming the
// wrong one is worse than saying nothing — it sends someone to run a command
// that cannot produce the missing file.
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
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
