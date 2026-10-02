// Package unsent owns the unsent round: the reviewer's comments that have not
// been sent to the agent yet, kept in `pending.json` beside the document's
// rounds (`<doc dir>/.galley/versions/<doc>/pending.json`).
//
// It is the ONE place a comment's words live. The .md carries only an ID mark
// at the comment's place — `{==words==}{>>@comment cm-…<<}` after highlighted
// text, `{>>@comment cb-…<<}` on its own line after a block, and nothing at all
// for a whole-document comment — and galley links a comment to its place by
// that ID alone. Words in the file were what forced every match on text, and
// every match on text was a way to pair a comment with the wrong place.
//
// The file is written BEFORE the .md on every add, edit and delete, so an ID
// mark in the .md always has its comment here. The reverse is allowed: a
// comment with no mark (galley stopped between the two writes) is shown as
// unplaced, never lost.
//
// # Policy
//
// PRIVATE, in internal/ondisk's terms. The file is scratch state written and
// read back by one running galley, in a gitignored directory, and the caller
// already has a quarantine path for a file it cannot read. So an unknown key
// is refused (ErrUnreadable) and a newer `v` is refused (ondisk.ErrFuture):
// misreading half a round costs the reviewer's words or their attribution,
// and a newer file must be left untouched rather than overwritten.
package unsent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tools "github.com/schuettc/tools-common"

	"github.com/schuettc/galley/internal/ondisk"
	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/versions"
)

// Version is the schema generation this build writes and the newest it reads.
const Version = 1

// fileName is the unsent round's name inside the document's versions
// directory.
const fileName = "pending.json"

// Kind says what a comment is about.
type Kind string

const (
	// KindText is a comment on selected text, marked by `cm-` IDs after each
	// highlighted piece.
	KindText Kind = "text"
	// KindBlock is a comment on a block — section, figure, figure rectangle,
	// table cell, code block — marked by a `cb-` note on its own line.
	KindBlock Kind = "block"
	// KindDocument is a comment on the whole document. It has no mark.
	KindDocument Kind = "document"
)

// ErrUnreadable is wrapped by every Load error that means the file is corrupt
// or of a shape this build does not know, as opposed to newer
// (ondisk.ErrFuture). The caller quarantines an unreadable file and refuses to
// touch a newer one.
var ErrUnreadable = errors.New("unreadable unsent round")

// Comment is one unsent reviewer comment.
//
// Key is the comment's ID, minted once by NewID and never re-derived. Quote is
// what the comment was on when it was made — the highlighted words or the
// block's heading — for the round message; it is never used to find the
// comment's place. Region is the rectangle on a figure, nil for every other
// comment.
type Comment struct {
	Key       string         `json:"key"`
	Kind      Kind           `json:"kind"`
	Text      string         `json:"text"`
	Author    string         `json:"author"`
	At        time.Time      `json:"at"`
	BlockKind string         `json:"blockKind,omitempty"`
	Region    *review.Region `json:"region,omitempty"`
	Quote     string         `json:"quote,omitempty"`
}

// File is the whole of `pending.json`.
type File struct {
	V        int       `json:"v"`
	Comments []Comment `json:"comments"`
}

// Path is where the unsent round for the document at mdPath is kept: beside
// its rounds, in the directory internal/versions owns.
func Path(mdPath string) string {
	return filepath.Join(versions.Open(mdPath).Dir(), fileName)
}

// Load reads the unsent round at path. A missing file is an empty round, and
// reading one creates nothing.
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := ondisk.Strict(raw, &f); err != nil {
		return File{}, fmt.Errorf("%w %s: %w", ErrUnreadable, path, err)
	}
	if ondisk.Future(f.V, Version) {
		return File{}, ondisk.Newer("the unsent round "+path, f.V, Version,
			"galley leaves it untouched; open the document with that galley or move the file aside")
	}
	return f, nil
}

// Save writes f to path atomically, stamping the version on the way out
// rather than trusting the caller's.
func Save(path string, f File) error {
	f.V = Version
	if f.Comments == nil {
		f.Comments = []Comment{}
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	// The versions directory is created lazily by the store's first Commit,
	// and a comment can be made before any round has been cut.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return tools.WriteFileAtomic(path, append(raw, '\n'), 0o644)
}

// NewID mints a comment ID: the kind's prefix and 16 random hex digits.
//
// The prefixes are `cm-`, `cb-` and `cd-`, never `bk-`, which is the block
// key's prefix (suggest.BlockKey): a comment ID and a block ID must look
// different.
func NewID(k Kind) string {
	var b [8]byte
	// crypto/rand.Read never returns an error on the platforms galley builds
	// for, and a short read would be a broken system, not a recoverable case.
	_, _ = rand.Read(b[:])
	return prefix(k) + hex.EncodeToString(b[:])
}

func prefix(k Kind) string {
	switch k {
	case KindBlock:
		return "cb-"
	case KindDocument:
		return "cd-"
	default:
		return "cm-"
	}
}

// FromThreads converts the live review threads to unsent comments, in order.
//
// A thread is an unsent comment when it is unresolved and has a reviewer entry
// with words in it — the filter the instruction builders apply. Text, Author
// and At come from the first such entry, verbatim.
func FromThreads(ts []review.Thread) []Comment {
	var out []Comment
	for _, th := range ts {
		if th.Resolved {
			continue
		}
		e, ok := reviewerEntry(th)
		if !ok {
			continue
		}
		out = append(out, Comment{
			Key: th.Key, Kind: kindOf(th.Anchor), Text: e.Text, Author: e.Author, At: e.At,
			BlockKind: th.BlockKind, Region: th.Region, Quote: th.Heading,
		})
	}
	return out
}

func reviewerEntry(th review.Thread) (review.Entry, bool) {
	for _, e := range th.Entries {
		if e.Author == review.AuthorCourt && strings.TrimSpace(e.Text) != "" {
			return e, true
		}
	}
	return review.Entry{}, false
}

// ToThreads converts unsent comments back to review threads, one entry each.
func ToThreads(cs []Comment) []review.Thread {
	out := make([]review.Thread, 0, len(cs))
	for _, c := range cs {
		out = append(out, review.Thread{
			Key: c.Key, Heading: c.Quote, Anchor: anchorOf(c.Kind), BlockKind: c.BlockKind, Region: c.Region,
			Entries: []review.Entry{{Author: c.Author, At: c.At, Text: c.Text}},
		})
	}
	return out
}

// kindOf and anchorOf map between a Kind and review.Thread's Anchor, where an
// empty Anchor is a range of text.
func kindOf(anchor string) Kind {
	switch anchor {
	case "block":
		return KindBlock
	case "document":
		return KindDocument
	default:
		return KindText
	}
}

func anchorOf(k Kind) string {
	switch k {
	case KindBlock:
		return "block"
	case KindDocument:
		return "document"
	default:
		return ""
	}
}
