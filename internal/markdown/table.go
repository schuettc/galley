package markdown

import (
	"strings"

	"github.com/subaud-tools/galley/internal/docmodel"
)

// renderTable writes a GFM table in ONE canonical spelling, which is a pure
// function of the cells' content.
//
// That sentence is the entire fixed-point argument, and it is why the
// document model carries no column WIDTH at all. The obvious alternative —
// pad every column to its widest cell — is stable against itself and
// unusable in practice: every table a human has ever written reflows on the
// first save, so a reviewer who opens a document and types one character in
// the prose produces a diff touching every row of every table below it. The
// table in galley's own design spec, the document this phase exists to open,
// is written in the form below and comes back out of it byte-identical.
//
// So widths are normalized by not existing. Parse discards the author's
// spacing because there is nowhere in the model to put it, and this writes
// " | " separators and three-character delimiters whatever the content is.
//
// Two normalizations happen HERE rather than in the model, because the file
// has only one place to say either. Every row is written with the HEADER's
// column count — GFM pads a short row and drops a long row's extras, and
// goldmark already does that on parse, so a document that came from Parse is
// unchanged and only a hand-built ragged one moves. And the delimiter row's
// alignment is read from the header row's cells alone, since the delimiter
// row is the only line that can carry it.
func renderTable(b docmodel.Block) string {
	rows := b.Children
	if len(rows) == 0 {
		return ""
	}
	header := rows[0]
	cols := len(header.Children)
	if cols == 0 {
		// A table with no columns has no spelling. "||" is not a header row,
		// the delimiter under it parses as nothing, and the pair reads back
		// as a paragraph — a block that would not be a table one round later.
		// renderedBlocks drops a block that renders to nothing.
		return ""
	}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, renderRow(cellTexts(header, cols)))
	lines = append(lines, renderRow(delimiters(header, cols)))
	for _, r := range rows[1:] {
		lines = append(lines, renderRow(cellTexts(r, cols)))
	}
	return strings.Join(lines, "\n")
}

// tableRendersNothing mirrors renderTable's two empty cases structurally,
// so rendersNothing can ask without rendering the whole table to find out.
func tableRendersNothing(b docmodel.Block) bool {
	return len(b.Children) == 0 || len(b.Children[0].Children) == 0
}

// cellTexts renders one row's cells, squared to cols: a short row is padded
// with empty strings and a long row's extras are dropped, which is what GFM
// does on the way in.
func cellTexts(row docmodel.Block, cols int) []string {
	out := make([]string, cols)
	for i := 0; i < cols && i < len(row.Children); i++ {
		out[i] = renderCell(row.Children[i])
	}
	return out
}

// delimiters is the "---" row, spelled from the header cells' alignment.
// Three characters in every form — the narrowest spelling that can carry a
// colon at both ends, so all four look like one column of the same table.
func delimiters(header docmodel.Block, cols int) []string {
	out := make([]string, cols)
	for i := range out {
		align := ""
		if i < len(header.Children) {
			align = header.Children[i].Attrs[docmodel.AlignAttr]
		}
		switch align {
		case "left":
			out[i] = ":--"
		case "right":
			out[i] = "--:"
		case "center":
			out[i] = ":-:"
		default:
			out[i] = "---"
		}
	}
	return out
}

// renderRow writes one "| a | b |" line.
//
// The trailing space is written only for a cell that HAS content, so an empty
// cell is "| |" and not "|  |". Both parse identically; only one of them is
// what a person writes, and writing what people write is what lets a document
// that already holds a table be opened and saved with no diff. It also keeps
// a run of spaces nobody typed out of somebody's file.
func renderRow(cells []string) string {
	var b strings.Builder
	b.WriteByte('|')
	for _, c := range cells {
		b.WriteByte(' ')
		b.WriteString(c)
		if c != "" {
			b.WriteByte(' ')
		}
		b.WriteByte('|')
	}
	return b.String()
}

// renderCell renders a cell's blocks as the one line a table row can hold.
//
// Parse produces a Paragraph, an Image, or a Note here — the Note when the
// cell's whole content is a {>>comment<<} — and the editor refuses every change
// inside a table, so the other shapes can arrive only from a hand-built
// document. They are still rendered rather than dropped:
// losing an author's words silently is the failure mode this package is
// scarred by, and Serialize has no error channel to refuse through.
func renderCell(cell docmodel.Block) string {
	return escapeCellText(strings.Join(cellParts(cell.Children), " "))
}

func cellParts(blocks []docmodel.Block) []string {
	var parts []string
	for _, b := range blocks {
		switch {
		case b.Kind == docmodel.Note:
			// A cell whose whole content is a {>>note<<} parses to a Note
			// BLOCK inside the cell, and it is written back as one. Ahead of
			// the inlines case, which is where it used to land: a Note carries
			// its text in Inlines, so falling through wrote the comment's words
			// as prose and dropped the markers. See renderCellNote.
			parts = append(parts, renderCellNote(b))
		case b.Kind == docmodel.Image:
			parts = append(parts, renderImage(b.Attrs["alt"], b.Attrs["src"]))
		case b.Kind == docmodel.CodeBlock:
			// A fence needs three lines and a row has one. A code SPAN is
			// the closest thing a cell can hold, and it keeps the text.
			parts = append(parts, renderCode(strings.TrimRight(b.Text, "\n")))
		case len(b.Inlines) > 0:
			// lineContext{} — atLineStart is false, because the "| " prefix
			// is already written. A cell's content never leads a line in the
			// file, so a leading "-" in it is text and not a bullet, exactly
			// as in a heading.
			parts = append(parts, renderInlines(dropHardBreaks(b.Inlines), lineContext{}))
		default:
			parts = append(parts, cellParts(b.Children)...)
		}
	}
	return parts
}

// dropHardBreaks replaces the hard-break sentinel with a plain space.
//
// A hard break plans "\\\n" and a table row is ONE LINE, so the break has
// nowhere to go — and GFM's own answer, a literal <br>, is raw HTML, which
// Parse refuses. A space is the honest degradation: the words either
// side stay, and stay apart. Parse never puts a break in a cell; only a
// hand-built document or the CRDT can.
func dropHardBreaks(inlines []docmodel.Inline) []docmodel.Inline {
	out := make([]docmodel.Inline, 0, len(inlines))
	for _, in := range inlines {
		if in.Has(docmodel.HardBreak) {
			out = append(out, docmodel.Inline{Text: " "})
			continue
		}
		out = append(out, in)
	}
	return out
}

// escapeCellText makes a rendered cell safe to sit inside a "| ... |" row.
// Two invariants, and the format dies without either.
//
// A ROW IS ONE LINE. Any newline reaching here splits the row and turns the
// rest of the cell into another row — or, past the last row, into a
// paragraph. dropHardBreaks removes the one source Parse can produce; this is
// the backstop for the rest (a code span holding a raw newline, a fence
// flattened above), and it is a backstop rather than the only guard because a
// stray "\\" left in front of the newline would survive as a literal
// backslash in the file.
//
// AN UNESCAPED "|" SPLITS THE CELL. GFM resolves a row into cells BEFORE any
// inline parsing, so the escape belongs on the cell's whole rendered text —
// including inside a code span, whose content the inline escaper deliberately
// never touches because markdown gives no way to escape it. That is not a
// workaround: the GFM spec says a pipe may be included "by escaping it,
// including inside other inline spans", and goldmark implements exactly that
// (its escapedPipeCell transformer splits the span around the backslash and
// drops it). Parse's own unescape covers the ordinary case for free, since
// "|" is CommonMark ASCII punctuation.
//
// Escaping every pipe unconditionally is correct because this package emits
// no pipe of its own — no delimiter it writes is a "|" — so every pipe
// arriving here is content.
func escapeCellText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.TrimSpace(s)
}
