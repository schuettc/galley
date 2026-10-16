package markdown_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

// The failure this phase exists to remove, as a test.
//
//	galley: parse …: markdown: table not supported (line 224)
//
// galley once refused to open a document with a table at all, which is a tool
// refusing the format its stated job is written in. The refusal was clean — a
// parse error naming the line, not a corrupted document — and clean is the
// right way to fail, but it is still a refusal.
//
// The tables are held to a fixed point: every "|" line comes back
// byte-identical, in the same order, because the canonical form renderTable
// writes was chosen to be what people write by hand.
func TestParse_OpensADocumentWithTables(t *testing.T) {
	path := filepath.Join("testdata", "acceptance-doc.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, _, err := markdown.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}

	tables := 0
	docmodel.Walk(doc, func(_ []int, b *docmodel.Block) {
		if b.Kind == docmodel.Table {
			tables++
		}
	})
	if tables == 0 {
		t.Fatal("the fixtureparsed with no table in it — the fixture this test is about has moved")
	}

	// Compare the table lines only — every "|" line must come back
	// byte-identical, in the same order. (A whole-file comparison would fail on
	// unrelated prose: Serialize writes each paragraph on one line, so a
	// hard-wrapped paragraph would reflow; that is a separate concern from
	// tables.)
	before, after := pipeLines(string(src)), pipeLines(string(markdown.Serialize(doc)))
	if len(before) == 0 {
		t.Fatal("the fixturehas no table rows in it — the fixture this test is about has moved")
	}
	if len(before) != len(after) {
		t.Fatalf("the spec has %d table lines and writes back %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("table line %d moved — the projection is not a fixed point:\n from: %q\n   to: %q",
				i+1, before[i], after[i])
		}
	}
}

// pipeLines is every line of a document that belongs to a table row, with any
// blockquote or list-item indent kept: a row that gains or loses its "> " is a
// row that moved out of its container.
func pipeLines(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimLeft(strings.TrimPrefix(strings.TrimSpace(line), ">"), " "), "|") {
			out = append(out, line)
		}
	}
	return out
}
