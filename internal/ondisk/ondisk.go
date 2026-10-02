// Package ondisk is the ONE description of how galley's persisted formats
// carry a schema version and what a reader does with one it does not know.
//
// Every format here gained a `v` on the same day and for the same reason: a
// renamed key decodes to a zero value in silence, and every one of these
// files is read by a program — or a BUILD — other than the one that wrote it.
// A version does not stop a rename inside one generation; what it buys is the
// ability to say "this file is from a galley I do not understand" instead of
// quietly reading half of it.
//
// # The two policies, and why there are two
//
// A format is either FORWARD-COMPATIBLE or PRIVATE, and the difference decides
// both the version rule and the unknown-field rule. Getting this backwards in
// the strict direction is the expensive mistake: an older binary that REFUSES
// a file a newer binary wrote is a galley that stops working the moment one
// process on the machine is upgraded, and there is no upgrade order that fixes
// it because the two run concurrently.
//
//   - FORWARD-COMPATIBLE — the file is read by builds other than the one that
//     wrote it (committed and merged with merge=union, or scanned out of a
//     shared directory by a long-lived process, or fetched off the wire).
//     Unknown fields are TOLERATED, a newer version is KEPT AND COUNTED rather
//     than refused, and what makes a rename loud is a golden key-set test in
//     the owning package — the only check that can see a rename at all, since
//     a reader cannot detect a key it was never told about. That is
//     `rounds.jsonl` and `~/.galley/live/`.
//
//   - PRIVATE — written and read back by ONE galley, in a gitignored working
//     directory, with a quarantine path already in place for a file it cannot
//     read. Unknown fields are REFUSED and a newer version is REFUSED, because
//     the file is scratch state whose loss costs a resume and whose silent
//     misreading costs the round's attribution. That is the handoff lease,
//     and the unsent round, `pending.json`.
//
// # Fail closed, and which of the two ways
//
// Two formats got this right before this package existed and their failure
// modes are the two on offer. internal/ledger/index.go refuses a database at a
// schema NEWER than the binary with a sentence naming the remedy, and migrates
// one older; internal/license/key.go refuses anything that is not exactly its
// version with a sentinel error. Newer is what `Newer` spells, and it is the
// index's shape rather than the license's wherever the file is a working
// artefact a human can be told what to do about.
package ondisk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrFuture is the sentinel every "this file is from a newer galley" error
// wraps, so a caller can branch on the condition without matching prose.
var ErrFuture = errors.New("written by a newer galley")

// Legacy is the version a record written before its format carried one reads
// as. ZERO IS NOT UNKNOWN — it is the first generation, whose shape is exactly
// the shape the field was added to, so a file written yesterday must keep
// loading unchanged. Every gate here treats 0 and 1 alike, and that is the
// whole of the backwards compatibility promise.
const Legacy = 0

// Future reports whether v names a generation newer than known. Legacy (0)
// never is.
func Future(v, known int) bool { return v > known }

// Newer is the refusal a PRIVATE format hands back, in the ledger index's
// shape: what was found, what this build knows, and what the human can do
// about it. `remedy` names the file or the action — it is the half a bare
// version mismatch always leaves the reader to guess at.
func Newer(format string, v, known int, remedy string) error {
	return fmt.Errorf("%s is at schema v%d and this galley knows v%d — %s: %w",
		format, v, known, remedy, ErrFuture)
}

// Strict decodes raw into dst and REFUSES A KEY IT DOES NOT KNOW.
//
// It is the only spelling of DisallowUnknownFields in this repository, so the
// decision to be strict is made by choosing this function rather than by
// remembering a decoder option four times. Use it only for a PRIVATE format —
// see the package comment for why the other three must not.
func Strict(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

// Keys reports the JSON key set a value marshals to, at the TOP LEVEL only, in
// the order encoding/json emits them.
//
// It exists so a package can pin its on-disk contract in a test: a rename that
// nothing on the reading side can detect — because the reader is looking for
// the key that vanished — turns the golden list red on the commit that causes
// it. That is the check the tolerant formats have INSTEAD of
// DisallowUnknownFields, and it works in the direction the decoder option
// cannot.
func Keys(v any) ([]string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("ondisk: %T does not marshal to an object", v)
	}
	var out []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("ondisk: unexpected key token %v", tok)
		}
		out = append(out, name)
		if err := skipValue(dec); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// skipValue consumes one whole value, nested objects and arrays included, so
// Keys never mistakes a nested field name for a top-level one.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}
