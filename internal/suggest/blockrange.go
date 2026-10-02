package suggest

import (
	"fmt"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/docmodel"
)

// A SELECTION THAT CROSSES A BLOCK BOUNDARY CAN BE COMMENTED ON.
//
// Until now it could not, and the failure was guaranteed rather than
// occasional. `docRange` in the browser refused to produce coordinates for a
// selection whose two ends were in different blocks, so the composer fell back
// to sending the selected TEXT — and that text is the concatenation of two
// blocks, which no single block contains. `findUnique` searched block by block
// and reported, every time:
//
//	suggest: "Four Cognito behaviors that shaped this Each of these was
//	measured rather than read, …" matched 0 times, want exactly 1
//
// The reviewer made an ordinary selection, the browser opened the composer, it
// accepted their typing, and only THEN did the server refuse — after the words
// were written. Every multi-block selection, always.
//
// ONE RUN ACROSS EVERY BLOCK IT TOUCHES, which is this package's own rule
// applied one level up. docmodel.RunAttr names ONE AUTHORED EDIT and every
// inline that edit touches carries the same token — that is what makes it a
// decision rather than a coordinate. A selection over three paragraphs is one
// decision; it was only ever "one block" because nothing had asked it to be
// more. The run is minted ONCE here and handed to every block in the span.
//
// THE ENDS ARE PARTIAL AND THE MIDDLE IS WHOLE. The first block is marked from
// the offset the selection started at to its end, the last from its start to
// where the selection stopped, and everything between them entirely.
//
// ONE ID ON EVERY PIECE, for the same reason: the file has no run, so the
// comment ID the caller minted is what says, after a restart, that the pieces
// are one comment (markdown gives pieces that share an ID one run).
func CommentAcross(d docmodel.Doc, fromPath []int, from int, toPath []int, to int, id, author string, at time.Time) (docmodel.Doc, error) {
	if pathEqual(fromPath, toPath) {
		// Not a cross-block selection at all. Answered by the single-block
		// path rather than duplicated here, so there is one implementation of
		// what commenting on a range means.
		return CommentOnRange(d, fromPath, from, to, id, author, at)
	}
	span, err := blocksBetween(d, fromPath, toPath)
	if err != nil {
		return docmodel.Doc{}, err
	}
	first, last := span[0], span[len(span)-1]
	if n := runeLen(d, first); from < 0 || from >= n {
		return docmodel.Doc{}, fmt.Errorf(
			"suggest: the selection starts at %d, outside the block at %v (%d runes)", from, first, n)
	}
	if n := runeLen(d, last); to <= 0 || to > n {
		return docmodel.Doc{}, fmt.Errorf(
			"suggest: the selection ends at %d, outside the block at %v (%d runes)", to, last, n)
	}

	// REFUSED BEFORE ANYTHING IS WRITTEN, block by block, so a selection
	// crossing something already commented on cannot half-apply. The
	// single-block path makes the same refusal for the same reason.
	for i, path := range span {
		lo, hi := 0, runeLen(d, path)
		if i == 0 {
			lo = from
		}
		if i == len(span)-1 {
			hi = to
		}
		m := match{path: append([]int(nil), path...), start: lo, end: hi}
		if cAuthor, cAt, found := conflictingComment(d, m); found {
			return docmodel.Doc{}, fmt.Errorf(
				"suggest: part of that selection already has a pending comment by %s (at %s)", cAuthor, cAt)
		}
	}

	run := newRun()
	clone := cloneDoc(d)
	var said []string
	docmodel.Walk(clone, func(path []int, b *docmodel.Block) {
		i := indexOfPath(span, path)
		if i < 0 {
			return
		}
		lo, hi := 0, len([]rune(plainText(b.Inlines)))
		if i == 0 {
			lo = from
		}
		if i == len(span)-1 {
			hi = to
		}
		before, matched, after := sliceByRuneRange(b.Inlines, lo, hi)
		said = append(said, plainText(matched))
		b.Inlines = concatInlines(before, commentMarked(matched, id, author, at, run), after)
	})

	// THE TARGET IS THE WHOLE SELECTION, joined the way a soft line break is
	// joined everywhere else in this pipeline: with a space. It is what the
	// card quotes, so it has to read as the words the reviewer highlighted
	// rather than as a list of fragments.
	target := strings.Join(said, " ")
	if strings.TrimSpace(target) == "" {
		return docmodel.Doc{}, fmt.Errorf("suggest: that selection has no text in it")
	}
	return clone, nil
}

// blocksBetween is every TEXT block from fromPath to toPath inclusive, in
// document order.
//
// It refuses a backwards range rather than silently swapping the ends: a caller
// handing them the wrong way round has a bug, and quietly commenting on the
// span anyway would hide it. The browser always sends them in document order —
// ProseMirror's selection does — so this is a contract check, not a repair.
func blocksBetween(d docmodel.Doc, fromPath, toPath []int) ([][]int, error) {
	var order [][]int
	docmodel.Walk(d, func(path []int, b *docmodel.Block) {
		// Only blocks that can CARRY a mark. A container has no inlines of its
		// own, and a Note block's inlines are a comment's own words — marking
		// those would be a suggestion on a comment, which nothing can decide.
		if len(b.Inlines) == 0 || b.Kind == docmodel.Note {
			return
		}
		order = append(order, append([]int(nil), path...))
	})
	start, end := indexOfPath(order, fromPath), indexOfPath(order, toPath)
	if start < 0 {
		return nil, fmt.Errorf("suggest: no text block at path %v", fromPath)
	}
	if end < 0 {
		return nil, fmt.Errorf("suggest: no text block at path %v", toPath)
	}
	if end < start {
		return nil, fmt.Errorf("suggest: the selection ends at %v, which is before it starts at %v", toPath, fromPath)
	}
	return order[start : end+1], nil
}

func indexOfPath(paths [][]int, path []int) int {
	for i, p := range paths {
		if pathEqual(p, path) {
			return i
		}
	}
	return -1
}

func runeLen(d docmodel.Doc, path []int) int {
	b, ok := blockAt(d, path)
	if !ok || b == nil {
		return 0
	}
	return len([]rune(plainText(b.Inlines)))
}
