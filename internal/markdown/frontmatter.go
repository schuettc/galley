package markdown

import (
	"bytes"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// FRONT MATTER IS SCANNED HERE RATHER THAN PARSED BY GOLDMARK, and the choice
// is deliberate twice over.
//
// goldmark has no front matter extension in its own tree — the ones that exist
// are third-party — and every one of them PARSES the YAML or TOML into a map.
// galley has nothing to do with what is inside the block: it is metadata, not
// prose, there is no suggestion to make about it and no card to draw for it.
// What galley needs is the two facts a scanner can give and a parser throws
// away: WHERE the block ends, and EXACTLY WHICH BYTES it occupies.
//
// So the block is carried verbatim. Serialize writes back the author's own
// bytes rather than a re-emission of a decoded document, which is the only way
// a key order, a comment, an anchor, a block scalar's indentation or a trailing
// space survives a file being opened. A YAML round trip through a decoder is
// not byte-identical and could not be made so.
//
// THE DELIMITERS ARE PART OF Text. They are the author's bytes too, they are
// what makes the block recognisable when it is read back, and holding them
// means Serialize is a copy rather than a reconstruction — there is no second
// spelling of "---" for the two sides to disagree about.
const (
	yamlFence = "---"
	tomlFence = "+++"
)

// splitFrontMatter returns the file's front matter block, verbatim, and the
// rest of the source. raw is nil when the file has none, in which case rest is
// src unchanged.
//
// The rules are the strict reading, and each one is load-bearing:
//
//   - IT MUST BE AT BYTE 0. Jekyll, Hugo and Astro all agree, and it is what
//     keeps a "---" anywhere else in the file a thematic break.
//   - THE OPENER AND THE CLOSER ARE THE SAME MARKER. "---" is not closed by
//     "+++"; a file that opens one and never closes it has no front matter at
//     all, and its first line goes back to being the thematic break it is
//     today.
//   - THE BODY MUST HOLD SOMETHING. An empty block carries no metadata, and
//     the emptiness is what keeps this a FIXED POINT: Serialize writes a
//     thematic break as "---", so a document opening with two of them is
//     written "---\n\n---\n", which this would otherwise read back as one
//     front matter block and two blocks would be gone. Serialize guards the
//     non-empty case (see frontMatterHazard); this rule is what makes the
//     common case need no guard at all.
func splitFrontMatter(src []byte) (raw, rest []byte) {
	fence := openingFence(src)
	if fence == "" {
		return nil, src
	}
	// The opener's own line, including its newline: everything after it is the
	// body until a closing line is found.
	pos := lineEnd(src, 0)
	body := pos
	for pos < len(src) {
		end := lineEnd(src, pos)
		if isFenceLine(src[pos:end], fence) {
			if len(bytes.TrimSpace(src[body:pos])) == 0 {
				// An empty block is not front matter — see above.
				return nil, src
			}
			return src[:end], src[end:]
		}
		pos = end
	}
	// No closing delimiter anywhere in the file. This is NOT front matter, and
	// saying so is what leaves an ordinary leading thematic break alone.
	return nil, src
}

// openingFence returns the marker src opens with, or "".
func openingFence(src []byte) string {
	for _, fence := range []string{yamlFence, tomlFence} {
		if end := lineEnd(src, 0); isFenceLine(src[:end], fence) && end > len(fence) {
			// end > len(fence) requires a LINE ENDING after the marker. A file
			// whose entire content is "---" is a thematic break, not an
			// unterminated front matter block.
			return fence
		}
	}
	return ""
}

// isFenceLine reports whether line — one line WITH its ending, if it has one —
// is exactly the given marker. Trailing whitespace is tolerated (a delimiter
// with a space after it is still a delimiter to every tool that reads these);
// leading whitespace is not, because an indented marker is not a delimiter to
// any of them.
func isFenceLine(line []byte, fence string) bool {
	trimmed := bytes.TrimRight(line, " \t\r\n")
	return string(trimmed) == fence
}

// lineEnd returns the offset just past the end of the line starting at pos —
// past its "\n" if it has one, else len(src).
func lineEnd(src []byte, pos int) int {
	i := bytes.IndexByte(src[pos:], '\n')
	if i < 0 {
		return len(src)
	}
	return pos + i + 1
}

// frontMatterBlock is the docmodel block for raw.
func frontMatterBlock(raw []byte) docmodel.Block {
	return docmodel.Block{Kind: docmodel.FrontMatter, Text: string(raw)}
}

// renderFrontMatter writes a front matter block back out. It is a COPY, with
// one correction: a block that does not end in a newline gets one, so the
// document under it starts on its own line. Parse always leaves the newline;
// a hand-built block need not.
func renderFrontMatter(b docmodel.Block) string {
	if b.Text == "" {
		return ""
	}
	if b.Text[len(b.Text)-1] == '\n' {
		return b.Text
	}
	return b.Text + "\n"
}
