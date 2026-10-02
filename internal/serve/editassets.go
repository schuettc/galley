package serve

import (
	"net/http"
	"path"
	"strings"
)

// editassets.go serves the files that live BESIDE the document, so an
// "![alt](pic.png)" in the markdown resolves to an actual image.
//
// THIS REVERSES A PREVIOUS RULING. Task 9 decided that edit mode's "/" serves
// the shell and nothing else — "a markdown document has no sibling assets a
// review page would, and serving the whole directory a user's document
// happens to live in is a surface nobody asked for". The first half turned
// out to be simply untrue: a spec with a screenshot or an exported diagram
// references a file next to it, and with nothing serving that file the image
// 404s in the editor no matter what the author writes. A document that cannot
// show its own figures is not a document editor.
//
// The second half of that ruling still stands, and is what shapes this. The
// directory is NOT handed to http.FileServer. Five constraints, each closing a different
// door:
//
//   - GET AND HEAD ONLY. There is nothing to write here, and a handler that
//     answered other methods would be a surface to probe.
//   - AN EXTENSION ALLOWLIST, not a denylist. A document's directory is
//     also, very often, a source tree: .env, id_rsa, a database dump and
//     every other file the author never thought about are sitting right
//     there. Only the seven image extensions an "![…](…)" can legitimately
//     point at are served, so adding a new kind of file to that directory can
//     never widen what this exposes.
//   - INSIDE THE DOCUMENT'S DIRECTORY, enforced by the KERNEL. Every read
//     goes through EditServer.root, an *os.Root opened once at construction
//     (see NewEdit). os.Root resolves each path component itself and refuses
//     to leave, which closes both traversal and symlink escape without any
//     string arithmetic — and, unlike the EvalSymlinks-then-compare it
//     replaces, it does so AT OPEN TIME, so there is no window between the
//     check and the read for the filesystem to change underneath it.
//   - THE FIGURE ITSELF IS NOT A SYMLINK. This is the door that was open.
//     Containment was proven on the resolved path while the extension was
//     checked on the URL, and nothing re-checked the extension after
//     resolution — so `ln -s .env innocent.png`, wholly inside the directory
//     and contained by every measure, served AWS_SECRET_ACCESS_KEY=hunter2
//     as image/png. A symlink is exactly a file whose NAME says one thing and
//     whose BYTES are something else, and a name-based allowlist has no
//     defence against one. Refusing it is simpler and stronger than
//     re-deriving the type from the target: a figure beside a document has no
//     reason to be a link, and a link into the same directory only ever
//     renames a file that is already reachable under its own name.
//   - NO DIRECTORY LISTING and no implicit index — a request for a directory
//     is a 404 like anything else, so this never enumerates what is there.
//
// Everything that fails any of those is a plain 404, with no detail, and that
// uniformity is deliberate: "that exists but you may not have it" is itself an
// answer about the filesystem, so a refusal and an absence are byte-identical.
//
// A GET is not a POST, so `guard` — which requires POST and a JSON body —
// cannot cover this route. But guard's reasoning is about reads too ("THE
// THREAT IS AN ORDINARY WEB PAGE. galley listens on 127.0.0.1 with no
// authentication"), and the port is a fixed default, and the tree below the
// document is served recursively. Without a header saying otherwise, any page
// in the user's browser could embed
//
//	<img src="http://127.0.0.1:8123/Pictures/2024/private.jpg" onload=… onerror=…>
//
// and read off which files exist and how big they are. It could never read the
// bytes — no CORS header, and canvas taint blocks pixel extraction — but an
// existence oracle over a user's home directory is not nothing.
// Cross-Origin-Resource-Policy: same-origin makes the browser refuse the embed
// outright, and no legitimate same-origin load is affected.

// assetTypes is the allowlist: extension -> the Content-Type it is served as.
//
// SVG IS SERVED AS image/svg+xml, DELIBERATELY, and it is the one entry here
// that is a judgement rather than a lookup.
//
// An SVG is a document, not an opaque blob: it can carry <script>. Served as
// text/plain it would be inert — and would also not render, which defeats the
// entire point of serving it, since a browser only paints an SVG in an <img>
// when it arrives as image/svg+xml. Exported diagrams (the mermaid and
// draw.io case this whole change is about) are overwhelmingly SVG, so
// "safe and useless" is not a real option.
//
// So it renders, and the script surface is closed by the response headers
// instead (see serveSibling):
//
//   - Through an <img>, which is how a document's figure is displayed,
//     scripts in an SVG DO NOT RUN AT ALL. That is a browser rule, not a
//     galley policy, and it covers the case that matters.
//   - Navigated to directly — the one way a script could run — the response
//     carries "Content-Security-Policy: sandbox", which loads it into an
//     opaque origin. A script inside it is then not same-origin with galley
//     and cannot reach the mutating endpoints on 127.0.0.1, which is the
//     only thing worth protecting here (they have no authentication, by
//     design; see guard).
//
// The residual risk is a hostile SVG in a directory the user chose to edit a
// document in, viewed directly, in a browser that ignores CSP sandbox. That
// is a narrower exposure than the file simply not loading is a cost.
//
// Keys are lower-case; the lookup folds the request's extension to match, so
// IMG_0001.JPG off a phone is served like any other figure.
var assetTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".avif": "image/avif",
}

// serveSibling answers a request for a file beside the document, or 404s.
func (s *EditServer) serveSibling(w http.ResponseWriter, r *http.Request) {
	// HEAD is allowed: http.ServeContent answers it for free, GET already
	// returns strictly more, and refusing it deviates from HTTP for no
	// security benefit. Everything else is a 404 like every other refusal
	// here — not a 405, which would tell a caller this route exists.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	name, ok := siblingName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctype, ok := assetTypes[strings.ToLower(path.Ext(name))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.root == nil {
		http.NotFound(w, r)
		return
	}

	// Lstat, not Stat: the extension above names the file the URL asked for,
	// and a symlink is precisely a file whose name and bytes disagree. Reading
	// through one is what made `ln -s .env innocent.png` serve a secret as
	// image/png. It also has to be a REGULAR file — a directory, a fifo or a
	// device node named "x.png" is not a figure.
	fi, err := s.root.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	// Open through the root as well rather than trusting the Lstat: os.Root
	// re-resolves every component in the kernel at open time, so a directory
	// component swapped between the two calls cannot redirect the read.
	f, err := s.root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", ctype)
	// nosniff so the browser cannot upgrade a mislabelled file into something
	// executable, and the sandbox so an SVG opened directly lands in an opaque
	// origin rather than galley's. See assetTypes.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// The browser refuses to embed this from another origin at all, which is
	// what turns "an attacker cannot read the bytes" into "an attacker cannot
	// learn the file is there". See the header comment.
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	// The document's own assets change while it is being edited, and a
	// cached figure is a figure that silently stops matching the text.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// siblingName maps a request path to the root-relative name os.Root takes, or
// reports that it does not name one.
//
// This is now spelling only — containment is the kernel's job, not this
// function's. path.Clean folds away "." and any ".." the URL carried, and what
// is left is checked for the two things os.Root cannot be asked about
// meaningfully: an empty name, and a Windows path separator.
//
// The backslash check is a SEGMENT check, not the substring test it replaces.
// `strings.Contains(clean, "..")` refused "fig..final.png" — an ordinary
// filename — with a 404 and no explanation, while doing nothing that
// path.Clean had not already done. Backslashes are the case that genuinely
// remains: `just verify` cross-builds for windows, where filepath would read
// `a\..\b` as traversal that the slash-based Clean above never saw.
func siblingName(urlPath string) (string, bool) {
	clean := path.Clean("/" + urlPath)
	name := strings.TrimPrefix(clean, "/")
	if name == "" || name == "." {
		return "", false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == ".." || strings.Contains(seg, `\`) {
			return "", false
		}
	}
	return name, true
}
