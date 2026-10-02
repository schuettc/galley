package markdown

import (
	"strings"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// SerializeOnto writes d as markdown, KEEPING THE AUTHOR'S OWN BYTES for every
// top-level block that has not changed.
//
// WHY THIS EXISTS. Serialize renders the document model, and the model does not
// carry spelling: a setext heading comes back "# h", "* item" comes back "-
// item", "__bold__" comes back "**bold**", and a paragraph hard-wrapped over
// three lines comes back on one, because Parse turns a soft line break into a
// space. None of that is a change anybody made. Measured 2026-08-22 on a real
// document: opening it in the editor and quitting without touching a key
// rewrote six separate things, because cmd/galley's shutdown Flush projects
// unconditionally and a projection is Serialize's output.
//
// The paper-over was to skip the write when nothing changed. That leaves the
// harm exactly where the reviewer actually meets it — fix one typo in a
// forty-paragraph post and the other thirty-nine are reformatted around it, in
// the same commit, and the diff no longer says what was done.
//
// THE RULE IS SELF-VERIFYING, WHICH IS WHY IT NEEDS NO ALIGNMENT ARGUMENT.
// prev is chopped into the source of each of its top-level blocks; a chunk is
// KEPT as a candidate only if parsing that chunk on its own yields exactly one
// block, and the candidate is only USED for a block whose rendering is byte-
// identical to the rendering of the chunk's own reparse. So a substituted chunk
// provably reparses to the block it replaced. Nothing here has to reason about
// how Parse's block list lines up with goldmark's children — through
// splitNoteLines, legalize and extractCritic, all three of which can change the
// count — because no index is ever carried across.
//
// IT IS TOP-LEVEL ONLY, and that is a floor rather than a limit: a list whose
// third item was edited re-renders whole, because the candidate for the list no
// longer matches. Recursing into containers is a later change; it cannot make
// this one wrong, only tighter.
//
// The rendering used as the key is renderedBlocks' own, taken from the SAME
// call that produces the output, so a list's marker style (which is decided by
// its neighbours and not by the block) is part of the key. A block that renders
// differently because of where it sits simply misses its candidate and is
// written the serializer's way — the safe direction, and the only one that
// cannot invent bytes.
//
// A chunk is consumed when it is used, so two identical paragraphs take two
// different chunks, in order, and never the same one twice.
func SerializeOnto(d docmodel.Doc, prev []byte) []byte {
	blocks := d.Blocks
	head := ""
	if len(blocks) > 0 && blocks[0].Kind == docmodel.FrontMatter {
		// Front matter is already verbatim in Text and Serialize writes it
		// back unchanged, so it has nothing to preserve and is not a candidate.
		head = renderFrontMatter(blocks[0])
		blocks = blocks[1:]
	}

	rendered := renderedBlocks(blocks)
	texts := make([]string, len(rendered))
	for i, r := range rendered {
		texts[i] = r.text
	}
	for key, chunks := range candidates(prev) {
		for i := range texts {
			if len(chunks) == 0 {
				break
			}
			if texts[i] == key {
				texts[i] = chunks[0]
				chunks = chunks[1:]
			}
		}
	}
	out := strings.Join(texts, "\n\n")

	if out == "" {
		if head != "" {
			return []byte(head)
		}
		return []byte("\n")
	}
	if head != "" {
		return []byte(head + "\n" + out + "\n")
	}
	return []byte(frontMatterHazard(out) + "\n")
}

// candidates maps a rendering to the ORIGINAL SOURCE of every top-level block
// in prev that renders that way, in document order.
//
// The key is the rendering of the chunk's own reparse, so a lookup answers
// exactly one question: "is there a piece of the old file that means the same
// thing as this block". Everything a chunk carries beyond that meaning — the
// author's wrapping, their bullet character, their emphasis marker, a
// {>>note<<} the parse lifts out — is what SerializeOnto is putting back.
//
// A chunk whose reparse is not exactly one block is dropped rather than
// repaired: it is a chunk whose boundaries this function got wrong (see
// chunkStarts on the nodes it cannot place), and a wrong boundary must cost a
// re-render and never a wrong write.
func candidates(prev []byte) map[string][]string {
	out := map[string][]string{}
	_, rest := splitFrontMatter(prev)
	for _, chunk := range chunkStarts(rest) {
		doc, comments, err := Parse([]byte(chunk))
		if err != nil || len(comments) > 0 {
			// A refusal cannot be a candidate, and neither can a chunk whose
			// parse LIFTS something out of it: the whole document's parse
			// lifted the same note out of the model, and writing the chunk
			// back verbatim would put the marker back in the file. A lifted
			// note is not imported as a comment, so it leaves the file at the
			// next save; that is decided by the parse, not here.
			continue
		}
		r := renderedBlocks(doc.Blocks)
		if len(r) != 1 {
			continue
		}
		out[r[0].text] = append(out[r[0].text], strings.TrimRight(chunk, "\n"))
	}
	return out
}

// chunkStarts cuts src into the source text of each top-level block.
//
// A block runs from the start of the LINE its first content byte falls on to
// the start of the line the next block begins at. Snapping to the line start is
// what picks up a container's own prefix — a blockquote's ">" and a list
// marker sit on the same line as the content whose segment goldmark reports —
// and running to the next block's line is what picks up a setext heading's
// "===" underline, which is part of the heading's source and not part of any
// node's reported segments.
//
// A NODE WITH NO SEGMENTS ANYWHERE UNDER IT CANNOT BE PLACED, and a thematic
// break is one. Rather than guess, it is merged into the chunk before it: the
// merged chunk then reparses to two blocks, fails candidates' one-block test,
// and both blocks are written the serializer's way. The feature degrades to
// "these two blocks are not preserved" instead of to a wrong boundary.
func chunkStarts(src []byte) []string {
	root := md.Parser().Parse(text.NewReader(src))
	var starts []int
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		pos, ok := firstSegment(n)
		if !ok {
			continue // merged into the previous chunk; see the doc comment.
		}
		starts = append(starts, lineStart(src, pos))
	}
	out := make([]string, 0, len(starts))
	for i, s := range starts {
		end := len(src)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if s > end {
			continue
		}
		out = append(out, string(src[s:end]))
	}
	return out
}

// firstSegment reports the offset of the first source byte under n, which is
// the leftmost start over every segment any descendant reports.
func firstSegment(n ast.Node) (int, bool) {
	best, ok := 0, false
	var walk func(ast.Node)
	walk = func(x ast.Node) {
		// BLOCKS ONLY. Lines() PANICS on an inline node ("can not call with
		// inline nodes"), and an inline's offsets would say nothing this
		// function needs anyway — a block's own segments already start at its
		// first content byte.
		if x.Type() != ast.TypeBlock {
			return
		}
		if l := x.Lines(); l != nil {
			for i := 0; i < l.Len(); i++ {
				if s := l.At(i).Start; !ok || s < best {
					best, ok = s, true
				}
			}
		}
		for c := x.FirstChild(); c != nil; c = c.NextSibling() {
			walk(c)
		}
	}
	walk(n)
	return best, ok
}

// lineStart returns the offset of the start of the line containing pos.
func lineStart(src []byte, pos int) int {
	if pos > len(src) {
		pos = len(src)
	}
	i := strings.LastIndexByte(string(src[:pos]), '\n')
	return i + 1
}
