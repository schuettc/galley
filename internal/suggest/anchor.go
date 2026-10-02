// anchor.go is the part of the suggestion layer that answers "what is this
// comment ABOUT?" for the two anchors that are not a range of prose.
//
// A range comment has a Highlight mark on the text it covers, and that mark
// IS its anchor. Neither of the other two has anywhere to put one: docmodel
// blocks carry no marks (an image and a code fence are blocks), and a comment
// on the whole file has no node at all. A block comment therefore lives in the
// document as a docmodel.Note carrying its ID — see markdown/note.go for how
// that is written to and read back from the file — and this file turns that
// Note's POSITION into the thing it is about. A document comment has no mark
// in the file at all.
package suggest

import (
	"fmt"
	"strings"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
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
// The key is derived from the block's OWN CONTENT. A path ("blocks[3]") and an
// ordinal both renumber the moment anything is inserted above them, and this
// codebase has already paid for that once: a comment thread keyed by an
// ordinal silently reattached itself to different text (CLAUDE.md's "an
// ordinal ID is not identity"). A
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
// project() calls it on every debounce. So every caller that resolves more
// than one anchor computes the keys once and passes them here.
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

// CommentOnBlock writes the mark of the block comment id after the block named
// by key: a {>>@comment id<<} on its own line. The comment's words are not
// written; they live in the unsent round, linked to this mark by id alone.
//
// The mark goes after any notes already under that block, so several comments
// on one block stack in the order they were made and every one of them still
// resolves to the same anchor.
func CommentOnBlock(d docmodel.Doc, key, id string) (docmodel.Doc, error) {
	keys := blockKeys(d)
	target := -1
	for i, k := range keys {
		if k != "" && k == key {
			target = i
			break
		}
	}
	if target < 0 {
		return docmodel.Doc{}, fmt.Errorf(
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
	blocks = append(blocks, markdown.NewCommentNote(id))
	blocks = append(blocks, clone.Blocks[insert:]...)
	clone.Blocks = blocks
	return clone, nil
}

// BlockKindFor is the kind of the block a block anchor names. Empty for any
// other anchor, and for a key that names nothing.
func BlockKindFor(d docmodel.Doc, a Anchor) string {
	if a.Kind != AnchorBlock {
		return ""
	}
	return blockKindsByKey(d)[a.Target]
}

// blockKindsByKey maps every addressable block key in d to its kind, in one
// blockKeys pass.
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
