// Package version carries the build stamp. The justfile and the release
// workflow both set these via -ldflags -X, so a local build and a release
// build report the same thing.
package version

import tools "github.com/schuettc/tools-common"

var (
	version = "dev"
	commit  = "none"
	date    = ""
)

// Version returns the semantic version this binary was stamped with.
func Version() string { return version }

// Commit returns the short commit this binary was built from.
func Commit() string { return commit }

// Date returns the YYYY-MM-DD the binary was built, or "" for a raw
// `go build`. The licensing gate treats "" as "inside every update window":
// someone building from source is already past the honor line the stamp
// enforces.
func Date() string { return date }

// String renders the full build stamp in the family format.
func String() string {
	return tools.Version{Number: version, Commit: commit, Date: date}.String()
}
