// Command galley-schemadump is the Go half of `just schema`. It reads
// {"id","src"} JSON objects, one per line, runs markdown.Parse over each src,
// and writes the resulting docmodel tree back out as JSON — SHAPED THE WAY
// internal/ydoc's writeBlock writes it into the shared fragment, because that
// shape is what the browser's schema is asked to build.
//
// It is a DEV-TIME TOOL and never part of a release: `just build` and `just
// cross` build ./cmd/galley alone. It
// exists so the gate's verdict is reached over markdown.Parse's REAL output
// rather than over a JavaScript re-implementation of it — a second parser would
// agree with the first right up until the case that matters.
//
// Nothing here decides anything. The verdict is web/schema.mjs's, from the
// schema TipTap actually builds; see web/schemacheck.mjs.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/subaud-tools/galley/internal/docmodel"
	"github.com/subaud-tools/galley/internal/markdown"
)

// node mirrors docmodel.Block in the three fields the bridge reads: raw Text
// for a code block, Inlines for anything with inline content, Children
// otherwise. See internal/ydoc's writeBlock, which chooses between them in
// that order.
type node struct {
	Kind     string            `json:"kind"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Text     string            `json:"text,omitempty"`
	Inlines  []inline          `json:"inlines,omitempty"`
	Children []node            `json:"children,omitempty"`
}

type inline struct {
	Text  string   `json:"text"`
	Marks []string `json:"marks,omitempty"`
}

func conv(b docmodel.Block) node {
	n := node{Kind: string(b.Kind), Attrs: b.Attrs, Text: b.Text}
	for _, in := range b.Inlines {
		i := inline{Text: in.Text}
		for _, m := range in.Marks {
			i.Marks = append(i.Marks, string(m.Kind))
		}
		n.Inlines = append(n.Inlines, i)
	}
	for _, c := range b.Children {
		n.Children = append(n.Children, conv(c))
	}
	return n
}

type row struct {
	ID  string `json:"id"`
	Src string `json:"src"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<24)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var r row
		if err := json.Unmarshal(line, &r); err != nil {
			fmt.Fprintf(os.Stderr, "galley-schemadump: %v\n", err)
			os.Exit(1)
		}
		d, _, err := markdown.Parse([]byte(r.Src))
		if err != nil {
			// A REFUSAL IS NOT A FAILURE. Parse declining a document (raw HTML,
			// a footnote, an image mixed with text) is the honest outcome the
			// gate is measuring the ALTERNATIVE to; it is reported and never
			// counted as a node the browser deletes.
			_ = out.Encode(map[string]any{"id": r.ID, "src": r.Src, "refused": err.Error()})
			continue
		}
		blocks := make([]node, 0, len(d.Blocks))
		for _, b := range d.Blocks {
			blocks = append(blocks, conv(b))
		}
		_ = out.Encode(map[string]any{
			"id":         r.ID,
			"src":        r.Src,
			"blocks":     blocks,
			"serialized": string(markdown.Serialize(d)),
		})
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "galley-schemadump: %v\n", err)
		os.Exit(1)
	}
}
