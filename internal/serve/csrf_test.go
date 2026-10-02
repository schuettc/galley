package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mutatingEndpoints is every request that changes state, with a body each will
// accept. They are tested together on purpose: a guard that covers three of
// four is the shape this whole class of bug takes.
type mutatingEndpoint struct {
	name string
	path string
	body string
	// alsoReads marks an endpoint whose GET is a legitimate READ of the same
	// thing its POST sets. The guard still covers the POST; only the
	// method-rejection case has to know the difference, and it asserts the GET
	// answers rather than skipping it. See TestReadableEndpointsAnswerGet.
	alsoReads bool
}

func editEndpoints() []mutatingEndpoint {
	return []mutatingEndpoint{
		{"instruct", "/_galley/instruct", `{"op":"comment","target":"Hello","text":"Expand this."}`, false},
		{"cannot", "/_galley/cannot", `{"why":"the source is unavailable"}`, false},
		{"revise", "/_galley/revise", "", true},
		{"mode", "/_galley/mode", `{"mode":"live"}`, true},
		{"ack", "/_galley/ack", `{"state":"working"}`, false},
		{"stop", "/_galley/stop", "", false},
	}
}

// everyEndpoint hands each mutating endpoint to fn along with the handler
// that serves it.
func everyEndpoint(t *testing.T, fn func(t *testing.T, h http.Handler, e mutatingEndpoint)) {
	t.Helper()
	dir := t.TempDir()
	edit := newEditServer(t, dir, "doc.md", "# Title\n\nHello.\n")
	edit.OnRevise = "true"
	eh := edit.Handler()
	for _, e := range editEndpoints() {
		t.Run("edit/"+e.name, func(t *testing.T) { fn(t, eh, e) })
	}
}

func csrfRequest(e mutatingEndpoint, contentType, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, e.path, strings.NewReader(e.body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	return r
}

// Any page the user happens to have open could POST to 127.0.0.1:8123 and
// rewrite their document — or, on /_galley/revise, make the server run its
// configured shell command. Both endpoints answered CORS-simple requests, so a
// plain auto-submitting form was enough; no fetch, no preflight, no CORS
// headers needed.
func TestCrossOriginPostIsRefusedOnEveryMutatingEndpoint(t *testing.T) {
	everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, csrfRequest(e, "application/json", "https://evil.example"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin POST %s: %d %s, want 403", e.path, rec.Code, rec.Body.String())
		}
	})
}

// sameSite falls back to comparing Origin's host against the request's Host
// only when Sec-Fetch-Site did not already decide it — and csrfRequest always
// sets Sec-Fetch-Site: cross-site whenever it sets Origin, so
// TestCrossOriginPostIsRefusedOnEveryMutatingEndpoint above never exercises
// that host-comparison branch on its own. An older browser, or a request that
// simply omits the header, still carries Origin — so the mismatch must be
// caught on Origin alone.
func TestCrossOriginPostWithNoSecFetchSiteIsRefused(t *testing.T) {
	everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
		r := httptest.NewRequest(http.MethodPost, e.path, strings.NewReader(e.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin POST %s with no Sec-Fetch-Site: %d %s, want 403", e.path, rec.Code, rec.Body.String())
		}
	})
}

// The two content types a cross-site <form> can send without a preflight. They
// are refused outright, so even a request that manages to arrive with no
// Origin header at all cannot be spelled by a form.
func TestCorsSimpleContentTypesAreRefused(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		t.Run(ct, func(t *testing.T) {
			everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, csrfRequest(e, ct, ""))
				if rec.Code != http.StatusUnsupportedMediaType {
					t.Fatalf("POST %s as %s: %d %s, want 415", e.path, ct, rec.Code, rec.Body.String())
				}
			})
		})
	}
}

// The guard must not cost the two callers that matter their access: the
// editor's own page (same origin) and the CLI (no Origin header at all).
func TestSameOriginAndCLIRequestsStillReachEveryEndpoint(t *testing.T) {
	everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
		sameOrigin := httptest.NewRequest(http.MethodPost, e.path, strings.NewReader(e.body))
		sameOrigin.Header.Set("Content-Type", "application/json")
		sameOrigin.Header.Set("Origin", "http://"+sameOrigin.Host)
		sameOrigin.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, sameOrigin)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnsupportedMediaType {
			t.Fatalf("same-origin POST %s refused: %d %s", e.path, rec.Code, rec.Body.String())
		}

		cli := httptest.NewRequest(http.MethodPost, e.path, strings.NewReader(e.body))
		cli.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, cli)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnsupportedMediaType {
			t.Fatalf("CLI POST %s refused: %d %s", e.path, rec.Code, rec.Body.String())
		}
	})
}

// Every mutating endpoint answers POST and nothing else, and says so. The
// previous decode() accepted PUT while advertising "Allow: POST", so three of
// the four endpoints took a method their own 405 denied existed.
func TestOnlyPostIsAcceptedAndTheAllowHeaderSaysSo(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
				if method == http.MethodGet && e.alsoReads {
					// Asserted by TestReadableEndpointsAnswerGet instead: this
					// endpoint's GET is a read, not a mutation attempted with
					// the wrong verb.
					return
				}
				r := httptest.NewRequest(method, e.path, strings.NewReader(e.body))
				r.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("%s %s: %d %s, want 405", method, e.path, rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Allow"); got != "POST" {
					t.Fatalf("%s %s: Allow = %q, want POST", method, e.path, got)
				}
			})
		})
	}
}

// The two endpoints whose GET is a read answer it, and answer JSON. Split out
// of the method-rejection case rather than merely excluded from it, so
// "readable" cannot become a way to leave an endpoint untested.
func TestReadableEndpointsAnswerGet(t *testing.T) {
	everyEndpoint(t, func(t *testing.T, h http.Handler, e mutatingEndpoint) {
		if !e.alsoReads {
			return
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, e.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s, want 200", e.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("GET %s: Content-Type = %q", e.path, ct)
		}
	})
}
