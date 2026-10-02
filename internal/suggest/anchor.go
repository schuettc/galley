// anchor.go is the part of the suggestion layer that answers "what is this
// comment ABOUT?" for the two anchors that are not a range of prose.
//
// A range comment has a Highlight mark on the text it covers, and that mark
// IS its anchor. Neither of the other two has anywhere to put one: docmodel
// blocks carry no marks (an image and a code fence are blocks), and a comment
// on the whole file has no node at all. Both therefore live in the document
// as a docmodel.Note block — see markdown/note.go for how that is written to
// and read back from the file — and this file turns a Note's POSITION into
// the thing it is about.
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

// AnchorKind says what a comment thread is attached to.
type AnchorKind string

const (
	// AnchorRange is the original anchor: a run of prose carrying a
	// Highlight mark. Unchanged by this file.
	AnchorRange AnchorKind = "range"
	// AnchorBlock is one whole block — an image, a code fence, a paragraph —
	// named by the stable key BlockKey derives from its content.
	AnchorBlock AnchorKind = "block"
	// AnchorDocument is the file itself: an overall note, attached to
	// nothing inside it.
	AnchorDocument AnchorKind = "document"
)

// Anchor is a comment's target: the kind, plus whatever names it. Target is
// the quoted text for a range, the block key for a block, and empty for a
// document comment.
type Anchor struct {
	Kind   AnchorKind `json:"kind"`
	Target string     `json:"target,omitempty"`
}

// BlockRef is one addressable block: what an agent needs to aim
// `--on-block` at it.
//
// Only TOP-LEVEL blocks are addressable. A block nested inside a list item
// or a blockquote has no line of its own to hang a note under — a note
// written there would be indented into the item and read back as part of it
// — so the addressable unit is the outermost block, and a note that does end
// up nested anchors to that same outermost block (see AnchorFor).
type BlockRef struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Label is a short human line naming the block: an image's alt text, a
	// fence's language and first line, a heading's text. It is for a person
	// (or an agent) choosing which block to comment on; nothing keys off it.
	Label string `json:"label"`
	// Index is the block's position among the document's top-level blocks.
	// DISPLAY ONLY — it renumbers the instant anything is inserted above it,
	// which is the whole reason Key exists. Never persist it, never pass it
	// back in.
	Index int `json:"index"`
}

// Blocks lists every addressable block in d, in document order, each with the
// stable key --on-block takes.
func Blocks(d docmodel.Doc) []BlockRef {
	keys := blockKeys(d)
	out := make([]BlockRef, 0, len(d.Blocks))
	for i, b := range d.Blocks {
		if b.Kind == docmodel.Note {
			continue
		}
		out = append(out, BlockRef{
			Key:   keys[i],
			Kind:  string(b.Kind),
			Label: blockLabel(b),
			Index: i,
		})
	}
	return out
}

// blockKeys returns a key per top-level block, parallel to d.Blocks, empty
// for the Note blocks that are not themselves addressable.
//
// The key is derived from the block's OWN CONTENT — the same way CommentKey
// is derived from the quote it anchors to, and for the same reason. A path
// ("blocks[3]") and an ordinal both renumber the moment anything is inserted
// above them, and this codebase has already paid for that once: a comment
// thread keyed by an ordinal silently reattached itself to different text
// (see CommentKey, and CLAUDE.md's "an ordinal ID is not identity"). A
// content hash moves only when the block itself changes, which is exactly
// when a comment on it deserves re-examination anyway.
//
// The content is the block's canonical markdown, so the key is defined by
// what the file says rather than by an internal representation that could be
// refactored underneath it. Two IDENTICAL blocks — the same paragraph twice,
// or two horizontal rules — hash the same, so an occurrence counter breaks
// the tie: the second identical block is "<hash>-2". That counts only
// blocks it collides with, so inserting an unrelated block above it does not
// move it.
func blockKeys(d docmodel.Doc) []string {
	out := make([]string, len(d.Blocks))
	seen := map[string]int{}
	for i, b := range d.Blocks {
		if b.Kind == docmodel.Note {
			continue
		}
		base := digestKey("bk-", string(b.Kind), blockContent(b))
		seen[base]++
		if n := seen[base]; n > 1 {
			out[i] = fmt.Sprintf("%s-%d", base, n)
			continue
		}
		out[i] = base
	}
	return out
}

// blockContent is the canonical markdown of one block on its own — what the
// key hashes.
func blockContent(b docmodel.Block) string {
	return string(markdown.Serialize(docmodel.Doc{Blocks: []docmodel.Block{b}}))
}

// blockLabelMax bounds a label so `galley blocks` stays one line per block.
const blockLabelMax = 72

// blockLabel is a short human line naming a block.
func blockLabel(b docmodel.Block) string {
	switch b.Kind {
	case docmodel.Image:
		if alt := b.Attrs["alt"]; alt != "" {
			return truncateLabel(alt + " (" + b.Attrs["src"] + ")")
		}
		return truncateLabel(b.Attrs["src"])
	case docmodel.CodeBlock:
		first, _, _ := strings.Cut(b.Text, "\n")
		if lang := b.Attrs["language"]; lang != "" {
			return truncateLabel(lang + ": " + strings.TrimSpace(first))
		}
		return truncateLabel(strings.TrimSpace(first))
	case docmodel.Heading:
		return truncateLabel(strings.Repeat("#", headingLevel(b)) + " " + plainText(b.Inlines))
	case docmodel.Rule:
		return "---"
	case docmodel.Paragraph:
		return truncateLabel(plainText(b.Inlines))
	default:
		first, _, _ := strings.Cut(strings.TrimSpace(blockContent(b)), "\n")
		return truncateLabel(first)
	}
}

func headingLevel(b docmodel.Block) int {
	switch b.Attrs["level"] {
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	case "6":
		return 6
	default:
		return 1
	}
}

func truncateLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= blockLabelMax {
		return s
	}
	return string(r[:blockLabelMax-1]) + "…"
}

// AnchorFor resolves what the Note block at path is about.
//
// The rules, and what each one protects:
//
//   - An explicit "@document" marker wins outright. Position cannot tell a
//     comment on the last block from a comment on the file, so the one that
//     loses information says so in the file. See markdown/note.go.
//   - A NESTED note (inside a list item, a blockquote) anchors to the
//     top-level block that contains it. Nothing finer is addressable, and
//     answering "the list" is both true and reachable.
//   - Otherwise the note anchors to the nearest top-level block ABOVE it,
//     skipping other notes so several notes can stack under one block.
//   - A note with nothing above it has no block to be about, so it is a
//     document comment. This keeps a hand-written note at the top of a file
//     from resolving to an empty key.
func AnchorFor(d docmodel.Doc, path []int) Anchor {
	return anchorForKeys(d, path, blockKeys(d))
}

// anchorForKeys is AnchorFor with the block keys already computed.
//
// blockKeys SERIALIZES EVERY BLOCK to markdown, so calling it once per note is
// quadratic in the document: 50 notes made List 136 ms — 1,765x slower than the
// same document with none — while the sidebar polls List on a timer and
// project() calls it on every debounce. This is the regression AcceptAll's own
// comment records having already paid for once ("that made 400 suggestions take
// minutes"), so every caller that resolves more than one anchor computes the
// keys once and passes them here.
func anchorForKeys(d docmodel.Doc, path []int, keys []string) Anchor {
	b, ok := blockAt(d, path)
	if !ok || b.Kind != docmodel.Note {
		return Anchor{Kind: AnchorRange}
	}
	if markdown.NoteAnchor(*b) == docmodel.AnchorDocument {
		return Anchor{Kind: AnchorDocument}
	}
	if len(path) > 1 {
		return Anchor{Kind: AnchorBlock, Target: keys[path[0]]}
	}
	for j := path[0] - 1; j >= 0; j-- {
		if d.Blocks[j].Kind == docmodel.Note {
			continue
		}
		return Anchor{Kind: AnchorBlock, Target: keys[j]}
	}
	return Anchor{Kind: AnchorDocument}
}

// blockAt resolves a docmodel.Walk path to the block it names.
func blockAt(d docmodel.Doc, path []int) (*docmodel.Block, bool) {
	blocks := d.Blocks
	var b *docmodel.Block
	for _, i := range path {
		if i < 0 || i >= len(blocks) {
			return nil, false
		}
		b = &blocks[i]
		blocks = b.Children
	}
	return b, b != nil
}

// CommentOnBlock attaches a note to the block named by key, writing it into
// the document as a {>>note<<} on its own line immediately after that block,
// and returns the new comment's THREAD KEY (see CommentKeyFor).
//
// The note goes after any notes already under that block, so several
// comments on one block stack in the order they were made and every one of
// them still resolves to the same anchor.
func CommentOnBlock(d docmodel.Doc, key, note, author string, at time.Time) (docmodel.Doc, string, error) {
	if err := checkNote(note); err != nil {
		return docmodel.Doc{}, "", err
	}
	keys := blockKeys(d)
	target := -1
	for i, k := range keys {
		if k != "" && k == key {
			target = i
			break
		}
	}
	if target < 0 {
		return docmodel.Doc{}, "", fmt.Errorf(
			"suggest: no block with key %q", key)
	}

	// Past the notes already attached to this block, so ordering is stable
	// and none of them is re-anchored by the insertion.
	insert := target + 1
	for insert < len(d.Blocks) && d.Blocks[insert].Kind == docmodel.Note {
		insert++
	}

	clone := cloneDoc(d)
	blocks := make([]docmodel.Block, 0, len(clone.Blocks)+1)
	blocks = append(blocks, clone.Blocks[:insert]...)
	blocks = append(blocks, markdown.NewNote(docmodel.AnchorBlock, note))
	blocks = append(blocks, clone.Blocks[insert:]...)
	clone.Blocks = blocks
	return clone, CommentKeyFor(Anchor{Kind: AnchorBlock, Target: key}, note, author, at), nil
}

// CommentOnDocument attaches a note to the file itself, written as a
// {>>@document note<<} on its own line at the END of the document, and
// returns the new comment's THREAD KEY.
func CommentOnDocument(d docmodel.Doc, note, author string, at time.Time) (docmodel.Doc, string, error) {
	if err := checkNote(note); err != nil {
		return docmodel.Doc{}, "", err
	}
	clone := cloneDoc(d)
	clone.Blocks = append(clone.Blocks, markdown.NewNote(docmodel.AnchorDocument, note))
	return clone, CommentKeyFor(Anchor{Kind: AnchorDocument}, note, author, at), nil
}

// checkNote refuses a note the file cannot carry.
//
// Refusing is the right answer here rather than mangling: a block or document
// comment exists ONLY in the markdown (a range comment at least has a
// highlight to fall back on), so a note that cannot be spelled has nowhere
// else to be. Better a clear error at the point of writing than a comment
// that silently reads back as prose. See markdown.UnwritableNoteText.
func checkNote(note string) error {
	switch {
	case strings.TrimSpace(note) == "":
		return fmt.Errorf("suggest: a comment needs some text")
	case markdown.UnwritableNoteText(note):
		return fmt.Errorf(
			"suggest: a comment cannot contain a line break or the sequence \"<<}\" — " +
				"CriticMarkup has no escape for either, so the note would be cut short in the file")
	}
	return nil
}

// NoteThread is one block or document comment as it stands in the FILE: the
// anchor it resolves to, its text, and a human label for what it is about.
// It is what a thread has to be opened against when the sidecar has no thread
// for it yet.
type NoteThread struct {
	Anchor Anchor
	Text   string
	Label  string
	// Kind is the docmodel kind of the block this note anchors to, empty for
	// a document anchor. It is recorded on the thread (review.Thread.BlockKind)
	// because it is the only thing that can tell an EDIT to the commented block
	// from its DELETION once the old key is gone — see ReconcileNotes.
	Kind string
	// Ordinal breaks the one tie the text cannot: two notes on one anchor whose
	// WORDS are identical. It counts, in document order, how many earlier notes
	// share this note's anchor and text, so the first of them is 0 and keys
	// exactly as it would if it were alone. See NewNoteThread.
	Ordinal int
}

// Notes lists every block and document comment in d, in document order.
func Notes(d docmodel.Doc) []NoteThread {
	notes, _ := notesWithPaths(d)
	return notes
}

// notesWithPaths is Notes with each note's docmodel.Walk path alongside it, in
// the same order.
//
// The path stays INSIDE this package, exactly as Pending.Path does: it is an
// internal coordinate that renumbers the moment a block is inserted above it,
// so publishing one would hand a caller an address it could neither interpret
// nor safely send back. The one thing outside this file that needs it —
// removing the note a thread is the conversation for — is Detach, which is
// here.
func notesWithPaths(d docmodel.Doc) ([]NoteThread, [][]int) {
	keys := blockKeys(d)
	out := make([]NoteThread, 0, 4)
	paths := make([][]int, 0, 4)
	seen := map[[3]string]int{}
	for _, sp := range noteSpans(d) {
		// anchorForKeys, not AnchorFor: one blockKeys pass for the whole call
		// rather than one per note. See anchorForKeys.
		a := anchorForKeys(d, sp.path, keys)
		label := "the whole document"
		kind := ""
		if a.Kind == AnchorBlock {
			for i, k := range keys {
				if k != "" && k == a.Target {
					label = blockLabel(d.Blocks[i])
					kind = string(d.Blocks[i].Kind)
					break
				}
			}
		}
		n := NoteThread{Anchor: a, Text: sp.text, Label: label, Kind: kind}
		// In document order, so a note keeps its ordinal as long as the notes
		// before it do — appending a third identical note does not re-key the
		// first two.
		n.Ordinal = seen[noteIdentity(n)]
		seen[noteIdentity(n)]++
		out = append(out, n)
		paths = append(paths, sp.path)
	}
	return out, paths
}

// noteIdentity is what two notes have to share before an ordinal is needed to
// tell them apart.
func noteIdentity(n NoteThread) [3]string {
	return [3]string{string(n.Anchor.Kind), n.Anchor.Target, n.Text}
}

// BlockKindFor is the kind of the block a block anchor names, for a caller
// opening a thread outside the Notes path (the live comment endpoints).
// Empty for any other anchor, and for a key that names nothing.
func BlockKindFor(d docmodel.Doc, a Anchor) string {
	if a.Kind != AnchorBlock {
		return ""
	}
	return blockKindsByKey(d)[a.Target]
}

// ReconcileNotes pairs the file's block and document comments against the
// threads the sidecar already holds, and reports what is missing.
//
// It exists because these two comments are recorded in two places that are
// each authoritative about a different half. The FILE carries the note itself
// — that is the zero-tooling promise, that an agent reads pending state from
// the .md alone — and the SIDECAR carries the conversation that grew around
// it: replies, resolution, who said what and when. Neither can be rebuilt
// from the other, so on every load they have to be matched back up, and
// getting that wrong duplicates a thread on every restart.
//
// There is no shared identifier to match on: a thread's key is derived once,
// at creation, and is never recomputed to FIND a thread by — deriving one to
// look up with would reattach a conversation to whatever the derivation
// happened to land on, which is the hazard CommentKey's own comment is about.
// So the match is made on what both sides DO have — the anchor and the
// reviewer's own words — in two passes, and the second pass is the one that
// earns its keep:
//
// AND THAT IS ALSO THE MIGRATION STORY FOR ANY CHANGE TO THE DERIVATION. Note
// keys used to digest the instant they were minted at, which no reader could
// reproduce from the file; they no longer do (CommentKeyFor). Every sidecar
// written before that change still works, because this function matches on
// TEXT and the thread it matches keeps the key it was stored under — matched
// rather than orphaned, so no duplicate opens beside it either. cmd/galley's
// TestALegacyNoteKeyStaysReachable is that promise, and it is what makes a
// derivation change a change rather than a data migration.
//
//  1. Exact: same anchor kind, same anchor key, same text.
//  2. Loose: same anchor kind, same text, any anchor key. This is what
//     survives an EDIT TO THE COMMENTED BLOCK. The key is derived from the
//     block's content, so fixing a typo in the paragraph a comment is about
//     moves the key — and without this pass the thread would be orphaned and
//     a fresh, reply-less duplicate opened beside it. A matched thread has
//     its anchor key refreshed to the block's current one, which is reported
//     through the returned changed flag so the caller knows to write back.
//
// THE LOOSE PASS CANNOT TELL AN EDIT FROM A DELETION, and that is the whole
// reason for the guard below. A note is a free-standing block: delete the
// block it was about and the note remains, AnchorFor re-resolves it to
// "nearest top-level block above" — which is now something else entirely —
// and rewriting AnchorKey with that result turns "this diagram is wrong" into
// a comment about the "# Title" heading, on disk and in the panel, with no
// signal. That is CLAUDE.md's "an ordinal ID is not identity" arriving by a
// different route: the thread KEY is content-derived as promised, but the
// ANCHOR is positional, and the loose pass launders a positional re-resolution
// into an authoritative rewrite.
//
// So a re-anchor that lands on a block of a DIFFERENT KIND is refused and the
// note orphaned instead — an image's comment does not become a heading's. A
// re-anchor within the same kind is applied, because that is what an edit
// looks like, but it is REPORTED through reanchored so the rail can say the
// target moved rather than silently showing the comment on something new.
//
// Each thread matches at most one note, so two identical notes on one block
// keep two threads.
//
// Notes with no thread at all come back as orphans for the caller to open
// threads for. Threads with no note are left exactly as they are: the note
// may have been resolved (which deletes it from the file) or the file may be
// mid-edit, and deleting a conversation because its anchor is momentarily
// missing is the one outcome nothing can undo.
func ReconcileNotes(d docmodel.Doc, threads []review.Thread) (out []review.Thread, orphans []NoteThread, reanchored []Reanchor, changed bool) {
	out = append([]review.Thread(nil), threads...)
	notes := Notes(d)
	matched := matchNotes(notes, out)
	kinds := blockKindsByKey(d)
	for i, n := range notes {
		j := matched[i]
		if j < 0 {
			orphans = append(orphans, n)
			continue
		}
		if prev := out[j].AnchorKey; prev != "" && prev != n.Anchor.Target {
			// The thread's RECORDED kind, not kinds[prev]: prev names a block
			// that is gone from d in BOTH cases — that is exactly why the loose
			// pass exists — so looking it up here would return "" for a plain
			// edit and detach every thread the pass was written to carry.
			// review.Thread.BlockKind is what the old block WAS.
			//
			// Editing a paragraph gives a new key for a paragraph; deleting an
			// image hands its note to whatever heading sits above it. So a
			// re-anchor across kinds keeps the PAIRING — the conversation and
			// its replies stay attached to this note, and no reply-less
			// duplicate is opened beside it — and refuses the REWRITE. The
			// thread goes on naming the block it was actually about, which no
			// longer exists, so it reads as detached instead of as a comment
			// about something it was never about. Reported so the rail can say
			// so out loud.
			//
			// An empty recorded kind is "no opinion" and allows the rewrite: a
			// sidecar written before this field existed must keep loading.
			if was := out[j].BlockKind; was != "" && was != kinds[n.Anchor.Target] {
				reanchored = append(reanchored, Reanchor{
					Key: out[j].Key, From: prev, To: n.Anchor.Target, Refused: true,
				})
				continue
			}
			reanchored = append(reanchored, Reanchor{
				Key: out[j].Key, From: prev, To: n.Anchor.Target,
			})
		}
		if out[j].AnchorKey != n.Anchor.Target {
			out[j].AnchorKey = n.Anchor.Target
			changed = true
		}
		if n.Kind != "" && out[j].BlockKind != n.Kind {
			out[j].BlockKind = n.Kind
			changed = true
		}
		if out[j].Heading != n.Label {
			out[j].Heading = n.Label
			changed = true
		}
	}
	return out, orphans, reanchored, changed
}

// matchNotes pairs each note against at most one thread, and each thread
// against at most one note, returning the thread index per note (-1 for a note
// nothing holds a conversation for).
//
// IT IS THE ONE PAIRING RULE, and it is a function so it stays that way:
// ReconcileNotes runs it on every load to carry conversations across an edit,
// and Detach runs it to find the note a thread being deleted is about. A second
// implementation would agree with this one right up until either grew a case.
//
// Two passes, exact then loose — see ReconcileNotes for what the loose one
// survives and why the caller, not this function, decides whether a loose match
// may rewrite an anchor key.
func matchNotes(notes []NoteThread, threads []review.Thread) []int {
	matched := make([]int, len(notes))
	for i := range matched {
		matched[i] = -1
	}
	taken := make([]bool, len(threads))
	match := func(n NoteThread, exact bool) int {
		for i := range threads {
			if taken[i] || threads[i].Anchor != string(n.Anchor.Kind) {
				continue
			}
			if exact && threads[i].AnchorKey != n.Anchor.Target {
				continue
			}
			if openingText(threads[i]) != n.Text {
				continue
			}
			return i
		}
		return -1
	}
	for pass := 0; pass < 2; pass++ {
		for i, n := range notes {
			if matched[i] >= 0 {
				continue
			}
			if j := match(n, pass == 0); j >= 0 {
				matched[i], taken[j] = j, true
			}
		}
	}
	return matched
}

// Reanchor reports a thread whose anchor key moved because the block it is
// about was edited. It is not an error — it is what the loose pass exists to
// do — but it is not invisible either: the rail has to be able to say "the
// text this is about has changed" rather than showing a comment beside prose
// that no longer matches it.
type Reanchor struct {
	Key  string // the thread
	From string // the block key it was recorded against
	To   string // the block key it resolves to now
	// Refused is set when the move was NOT applied because it crossed block
	// kinds — the deletion case. The thread still names From, which no longer
	// exists: it is detached, not re-pointed, and the rail should say the
	// target was deleted rather than show the comment beside To.
	Refused bool
}

// blockKindsByKey maps every addressable block key in d to its kind, so a
// re-anchor can be checked for "same kind of thing" without a second scan per
// note. Computed once per ReconcileNotes call — see Notes on why that matters.
func blockKindsByKey(d docmodel.Doc) map[string]string {
	keys := blockKeys(d)
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		if k == "" {
			continue
		}
		out[k] = string(d.Blocks[i].Kind)
	}
	return out
}

// openingText is the text a thread STARTED with — the words that were written
// into the file as the {>>note<<}, whoever wrote them.
//
// Deliberately NOT review.Thread.Comment, which is "the reviewer's own text"
// and returns "" for a thread the agent opened. Every comment the agent makes
// through /_galley/suggest is one of those, so matching on Comment orphaned
// every agent-written note on the next load and opened a duplicate thread
// beside it, attributed to the reviewer — silently, once per restart.
func openingText(t review.Thread) string {
	if len(t.Entries) == 0 {
		return ""
	}
	return t.Entries[0].Text
}

// NewNoteThread is the thread a note with no thread yet becomes.
//
// The key takes the note's Ordinal as well as its text, and since the key
// stopped digesting the instant (CommentKeyFor) the ordinal is the ONLY thing
// telling two identical notes on one anchor apart — review.Session.Append
// upserts by key, so without it they would merge into one thread holding both
// their words. It was already the only thing that worked: every import path
// stamps its orphans with ONE time.Now() and one author constant, so the
// instant never discriminated between two notes imported together; it only
// discriminated between two READERS of the same note, which is precisely the
// bug it was removed for. The ordinal is 0 for the first note of its (anchor,
// text), and CommentKeyFor is fed the plain text there, so the overwhelmingly
// common case keys exactly as it reads.
func NewNoteThread(n NoteThread, author string, at time.Time) review.Thread {
	return review.Thread{
		Key:       CommentKeyFor(n.Anchor, ordinalText(n.Text, n.Ordinal), author, at),
		Heading:   n.Label,
		Anchor:    string(n.Anchor.Kind),
		AnchorKey: n.Anchor.Target,
		BlockKind: n.Kind,
		Entries:   []review.Entry{{Author: author, At: at, Text: n.Text}},
	}
}

// ordinalText is the note text CommentKeyFor digests: the words themselves for
// the first note of its (anchor, text), and the words plus a counter for each
// later duplicate. digestKey length-prefixes every part, so the suffix cannot
// spell a different note's text.
func ordinalText(text string, ordinal int) string {
	if ordinal == 0 {
		return text
	}
	return text + "\x00#" + strconv.Itoa(ordinal)
}

// noteSpans finds every Note block in d, at any depth, as a span the rest of
// this package can list, order and resolve alongside the mark-based ones.
func noteSpans(d docmodel.Doc) []span {
	var spans []span
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		if b.Kind != docmodel.Note {
			return
		}
		spans = append(spans, span{
			path:      append([]int(nil), path...),
			kind:      KindComment,
			note:      true,
			commentID: b.Attrs[docmodel.CommentIDAttr],
			text:      markdown.NoteText(*b),
		})
	})
	return spans
}

// removeBlockAt returns d without the block at path — what resolving a block or
// document comment does to its Note, since there is no mark to lift, and
// equally what an applied deletion that emptied its own paragraph does (see
// apply.go). It was `removeNoteBlock` while a note was its only subject; the
// guard below is about what the BROWSER can build and has never been about
// notes, so the name says the act.
//
// A NOTE THAT IS THE WHOLE OF ITS PARENT LEAVES A BLOCK BEHIND, NOT A HOLE.
// Removing the only child of a cell, a list item or a blockquote produces a
// parent the BROWSER'S SCHEMA CANNOT BUILD, and y-prosemirror does not skip
// such a node: createNodeFromYElement calls schema.node, which is
// createChecked, and catches the throw by deleting el._item out of the Y doc.
// The deletion broadcasts, EditServer's OnUpdate fires, touch() schedules
// Project(), and the author's file is rewritten with no keystroke — the exact
// shape markdown.Parse's "A CELL ALWAYS HOLDS A BLOCK" invariant exists to
// forbid, arriving from the other end. In a table it is a COLUMN SHIFT, because
// markdown.cellTexts squares a row to the header's width by padding at the END:
// delete the note in "| {>>why<<} | mid | tail |" and the row comes back
// "| mid | tail | |", with mid under the first column's heading.
//
// So the emptied parent is refilled with an empty Paragraph — the same shape
// markdown.Parse already writes into a blank cell, so this produces a model
// Parse could have produced rather than a second one beside it, and the same
// spelling on disk: renderCell of a childless paragraph joins zero parts and
// yields "", and a list marker and a blockquote's ">" are written whether or
// not the body renders anything.
//
// The top-level branch takes no such filling. The doc is `block+` too, but a
// file whose only block was the note is a file the author has emptied, and an
// empty Paragraph at the top level is a block markdown genuinely cannot write —
// it would render to nothing, be dropped by renderedBlocks, and not survive to
// be the block it claims to be. There the hole is the honest answer.
func removeBlockAt(d docmodel.Doc, path []int) docmodel.Doc {
	clone := cloneDoc(d)
	if len(path) == 1 {
		clone.Blocks = removeAt(clone.Blocks, path[0])
		return clone
	}
	parent, ok := blockAt(clone, path[:len(path)-1])
	if !ok {
		return clone
	}
	parent.Children = removeAt(parent.Children, path[len(path)-1])
	if len(parent.Children) == 0 && mustHoldABlock(parent.Kind) {
		parent.Children = []docmodel.Block{{Kind: docmodel.Paragraph}}
	}
	return clone
}

// mustHoldABlock reports whether a childless block of this kind is a node the
// browser's schema cannot build.
//
// The list is read off the TipTap node definitions the bundle actually ships
// (@tiptap 2.27.2, web/entry.js registers them and overrides only attributes
// and plugins — never `content`), and it is the complete set of parents a
// docmodel.Note can be the sole child of:
//
//   - tableCell, tableHeader — `content: 'block+'` (extension-table-cell,
//     extension-table-header; AlignedTableCell/AlignedTableHeader extend
//     addAttributes only).
//   - blockquote — `content: 'block+'` (extension-blockquote, taken from
//     StarterKit unmodified).
//   - listItem — `content: 'paragraph block*'` (extension-list-item), which is
//     STRICTER than block+ and is why this predicate is not "does it have
//     children": the first child must be a paragraph, so an empty Paragraph is
//     the only refill that satisfies every member of this list at once.
//
// Deliberately NOT here: table (`tableRow+`) and the lists (`listItem+`) never
// hold a Note directly, and tableRow is `(tableCell | tableHeader)*`, which an
// empty row satisfies. The doc's `block+` is handled at the call site above.
//
// It is local to this file because removeBlockAt is the only caller — removeAt
// has no other — and an exported predicate with one consumer is a second place
// for the rule to drift. The browser-side assertions this rests on stay where
// they can see a real fragment and a real browser: internal/ydoc's
// TestEveryCellCrossesWithABlockInside and web/typing.mjs §0.
func mustHoldABlock(k docmodel.BlockKind) bool {
	switch k {
	case docmodel.TableCell, docmodel.TableHeader, docmodel.ListItem, docmodel.Blockquote:
		return true
	}
	return false
}

func removeAt(blocks []docmodel.Block, i int) []docmodel.Block {
	if i < 0 || i >= len(blocks) {
		return blocks
	}
	out := make([]docmodel.Block, 0, len(blocks)-1)
	out = append(out, blocks[:i]...)
	return append(out, blocks[i+1:]...)
}
