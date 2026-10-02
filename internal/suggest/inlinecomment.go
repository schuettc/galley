// inlinecomment.go is the on-ramp a {>>note<<} INSIDE a sentence takes from
// the file into the sidecar's conversation model — the third of the three
// things a CriticMarkup comment can be, and the only one that leaves the .md.
//
// markdown/note.go states the grammar: a note that is the whole content of its
// paragraph is a docmodel.Note BLOCK and stays in the file (suggest.NoteThread
// and ReconcileNotes are its side of this); a note sitting at a POSITION in
// prose has something to be lifted out of, so markdown.Parse lifts it into an
// InlineComment and Serialize never re-emits it. That lift is the reason this
// file exists: once Parse has run, the note's words are in the []InlineComment
// and NOWHERE ELSE. Anything that then writes the document back without
// recording them destroys them — not moves them, destroys them, since the .md
// no longer carries the marker and the sidecar never carried a thread.
//
// It lives in suggest rather than in whichever caller needed it first for the
// reason attribution.go gives for ReplayAttribution: there are two callers,
// the edit server (internal/serve, at startup) and the offline CLI
// (cmd/galley, on every parse), the key they mint has to be THE SAME STRING
// in both, and a thread whose key differs between the two paths is a
// conversation that forks in half the first time a reviewer closes the editor.
// See CLAUDE.md's "one predicate, exported by the side that answers it".

package suggest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
)

// InlineCommentKey names the thread a {>>note<<} becomes: deterministic from
// where it was anchored, so two comments in the same document never collide.
//
// It only has to be unique at the MOMENT OF IMPORT — once created, the
// thread's identity going forward is its key as recorded in the sidecar, not
// how that key was derived. That matters because the coordinates it is built
// from (the block path and the rune offset within the block) both move as the
// document is edited, and the key deliberately does not follow them. Compare
// suggest.CommentKey, which is derived once from content and author for the
// same reason: an address that recomputes is an address that reattaches a
// conversation to different text.
func InlineCommentKey(c markdown.InlineComment) string {
	parts := make([]string, len(c.BlockPath))
	for i, p := range c.BlockPath {
		parts[i] = strconv.Itoa(p)
	}
	return fmt.Sprintf("%s%s-%d", inlineKeyPrefix, strings.Join(parts, "-"), c.Offset)
}

// inlineKeyPrefix marks a thread as an inline note's.
const inlineKeyPrefix = "md-"

// InlineCommentHeading names what an inline note is ABOUT: the block it sits
// in, through the same blockLabel every other anchored thread is named by.
//
// ONE DERIVATION, TWO MINTING SITES. An inline note's thread is opened offline
// by ImportInlineComments below and live by serve's own importInlineComments —
// separate code that shared only the KEY. This is exported so it can share the
// NAME too, rather than two spellings of "the block's text" that agree until
// one of them is edited. See CLAUDE.md's "one predicate, exported by the side
// that answers it".
//
// The empty string is the honest answer when the path does not resolve: the
// heading is a label, `galley pending` prints it verbatim, and inventing one
// from a block that is not there would name the wrong prose.
func InlineCommentHeading(d docmodel.Doc, c markdown.InlineComment) string {
	b, ok := blockAt(d, c.BlockPath)
	if !ok {
		return ""
	}
	return blockLabel(*b)
}

// ImportInlineComments opens a thread for every inline comment the parse
// lifted that prior has no thread for, and returns the amended thread list.
//
// The author is the REVIEWER, not the agent, and for the same reason
// importNotes uses the reviewer: a marker found in the file was typed by
// whoever edited the file. An agent's own comment arrives through `galley
// suggest --comment`, which opens its thread itself under a suggest.CommentKey
// and never goes through this path.
//
// EXISTING KEYS ARE SKIPPED, and that is not defensive — it is reachable. A
// mutation that writes only the SIDECAR (`galley approve <thread-key>`,
// `reply`, `resolve`) leaves the .md untouched, so the marker is still in the
// file on the next parse and this runs a second time over a note that already
// has a thread. Without the skip that second run would append the note's own
// words as a fresh entry underneath the conversation about it, and a third
// would do it again. Skipping also preserves whatever the thread has become
// since — its replies, and its Resolved flag.
//
// The document is taken so the thread can be NAMED — see
// InlineCommentHeading. It was not, and an inline note was the one thread kind
// with no heading at all.
func ImportInlineComments(d docmodel.Doc, prior []review.Thread, comments []markdown.InlineComment, author string, at time.Time) []review.Thread {
	if len(comments) == 0 {
		return prior
	}
	have := make(map[string]bool, len(prior))
	for _, t := range prior {
		have[t.Key] = true
	}
	out := prior
	for _, c := range comments {
		key := InlineCommentKey(c)
		if have[key] {
			continue
		}
		have[key] = true
		out = append(out, review.Thread{
			Key:     key,
			Heading: InlineCommentHeading(d, c),
			Entries: []review.Entry{{Author: author, At: at, Text: c.Text}},
		})
	}
	return out
}
