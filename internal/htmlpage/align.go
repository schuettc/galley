package htmlpage

import (
	"sort"
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// align.go matches the blocks a reviewer poured back to the wrappers they came
// out of BY IDENTITY, not by position — the fix for the whole re-pour
// corruption class (muster #175, and the guide/docs/buy friction). See
// docs/superpowers/specs/2026-09-04-html-pour-robustness-design.md.
//
// renderSlot used to pair block[i] with wrapper[i]: insert, delete, or reorder
// one block and every class after it landed on the wrong content. alignBlocks
// instead matches each poured block to the source wrapper whose original text
// it most resembles, so an edit keeps its class, a reorder carries its class
// with it, a deletion drops its wrapper, and a genuinely new block takes a
// default rather than stealing a neighbour's.

// matchThreshold is the word-overlap a poured block and a source wrapper must
// share to be judged the same block. High enough that two different paragraphs
// do not match, low enough that a reworded one still does — "The retry budget
// is explicit." vs "The retry budget is five." share four of six words.
const matchThreshold = 0.5

// alignBlocks returns, per poured block, the wrapper to render it in — the
// wrapper it descends from, an inherited default for a new block, or nil for a
// bare tag. It never returns a wrapper of a different kind, and never assigns
// one wrapper to two blocks.
func alignBlocks(blocks []docmodel.Block, wrappers []Wrapper) []*Wrapper {
	out := make([]*Wrapper, len(blocks))
	fps := make([]string, len(blocks))
	for i := range blocks {
		fps[i] = blockFingerprint(blocks[i])
	}

	// Every same-kind (block, wrapper) pair that clears the threshold, best
	// first, assigned greedily so the strongest resemblance wins the wrapper
	// even when a weaker pair sits earlier in the document. Order-independent by
	// construction, which is what lets a reorder keep its classes.
	type cand struct {
		bi, wi int
		score  float64
	}
	var cands []cand
	for bi := range blocks {
		if fps[bi] == "" {
			continue
		}
		for wi := range wrappers {
			if string(blocks[bi].Kind) != wrappers[wi].Kind {
				continue
			}
			s := similarity(fps[bi], wrappers[wi].Text)
			if s >= matchThreshold {
				cands = append(cands, cand{bi, wi, s})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	wrapUsed := make([]bool, len(wrappers))
	for _, c := range cands {
		if out[c.bi] != nil || wrapUsed[c.wi] {
			continue
		}
		w := wrappers[c.wi]
		out[c.bi] = &w
		wrapUsed[c.wi] = true
	}

	// Positional fallback for leftovers: a block that matched no wrapper AND a
	// same-kind wrapper that matched no block are, in order, most likely one
	// block rewritten past recognition (the whole paragraph replaced). Pair them
	// by document order. This is safe precisely because identity matching above
	// already claimed every block that still resembles its origin, so a leftover
	// wrapper is genuinely orphaned — pairing it cannot slide a class off a live
	// block, which is the corruption this file exists to stop.
	leftoverW := map[string][]int{}
	for wi := range wrappers {
		if !wrapUsed[wi] {
			leftoverW[wrappers[wi].Kind] = append(leftoverW[wrappers[wi].Kind], wi)
		}
	}
	for bi := range blocks {
		if out[bi] != nil || fps[bi] == "" {
			continue
		}
		q := leftoverW[string(blocks[bi].Kind)]
		if len(q) == 0 {
			continue
		}
		wi := q[0]
		leftoverW[string(blocks[bi].Kind)] = q[1:]
		w := wrappers[wi]
		out[bi] = &w
		wrapUsed[wi] = true
	}

	// A block that STILL matched nothing is genuinely new (added: more blocks
	// than wrappers). It inherits a class only when every matched wrapper of its
	// kind agrees on one — a new code fence beside `<pre class="code">` blocks
	// gets `.code`; a new paragraph in a slot whose paragraphs disagree gets a
	// bare tag rather than a wrong guess.
	for bi := range blocks {
		if out[bi] != nil {
			continue
		}
		if inh := unanimousWrapper(blocks[bi].Kind, wrappers, wrapUsed); inh != nil {
			out[bi] = inh
		}
	}
	return out
}

// unanimousWrapper returns the wrapper shared by every MATCHED wrapper of a
// kind, or nil if there is none or they disagree. Only matched wrappers vote:
// an unmatched wrapper is a deleted block, and a deleted block's styling should
// not decide a new one's.
func unanimousWrapper(kind docmodel.BlockKind, wrappers []Wrapper, used []bool) *Wrapper {
	var pick *Wrapper
	for wi := range wrappers {
		if !used[wi] || wrappers[wi].Kind != string(kind) {
			continue
		}
		w := wrappers[wi]
		if pick == nil {
			pick = &w
			continue
		}
		if pick.Tag != w.Tag || pick.Attrs != w.Attrs {
			return nil
		}
	}
	return pick
}

// blockFingerprint is the normalised text a block is matched on: its words,
// lowercased and single-spaced, with markdown/markup gone. Two spellings of the
// same content fingerprint identically, so extract (from the HTML node) and
// render (from the poured block) agree.
func blockFingerprint(b docmodel.Block) string {
	switch b.Kind {
	case docmodel.CodeBlock:
		return normalizeWords(b.Text)
	case docmodel.Paragraph, docmodel.Heading:
		return normalizeWords(inlinesText(b.Inlines))
	default:
		return normalizeWords(childrenText(b))
	}
}

func inlinesText(inlines []docmodel.Inline) string {
	var s strings.Builder
	for _, in := range inlines {
		s.WriteString(in.Text)
	}
	return s.String()
}

func childrenText(b docmodel.Block) string {
	var s strings.Builder
	var walk func(docmodel.Block)
	walk = func(n docmodel.Block) {
		s.WriteString(inlinesText(n.Inlines))
		s.WriteByte(' ')
		if n.Text != "" {
			s.WriteString(n.Text)
			s.WriteByte(' ')
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(b)
	return s.String()
}

// normalizeWords lowercases, keeps only letters/digits as word characters, and
// collapses everything else to single spaces — so punctuation, tag syntax, and
// whitespace do not affect the match.
func normalizeWords(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// similarity is the Jaccard overlap of two normalised word strings: shared
// words over total distinct words. 1.0 for identical text, 0.0 for disjoint.
// Word-set rather than sequence so a light reorder inside a block still reads
// as the same block.
func similarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	aw := strings.Fields(a)
	bw := strings.Fields(b)
	set := make(map[string]bool, len(aw))
	for _, w := range aw {
		set[w] = true
	}
	inter := 0
	seen := make(map[string]bool, len(bw))
	for _, w := range bw {
		if seen[w] {
			continue
		}
		seen[w] = true
		if set[w] {
			inter++
		}
	}
	union := len(set)
	for w := range seen {
		if !set[w] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
