package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/subaud-tools/galley/internal/serve"
)

// newRenderFlags is galley render's flag set and the -o destination.
func newRenderFlags() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	o := fs.String("o", "", "write the page here instead of <name>.html beside the document")
	return fs, o
}

// runRender writes the branded page Share HTML writes, without an editor: the
// same renderer and theme lookup, no refresh script. A theme or render error
// writes nothing, so it doubles as the check of a theme.
func runRender(args []string, out, _ io.Writer) error {
	fs, o := newRenderFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	doc := pos[0]
	if strings.EqualFold(filepath.Ext(doc), ".html") {
		return fmt.Errorf("%s: a page review is already HTML; render takes a markdown document", doc)
	}
	page, err := serve.RenderDoc(doc)
	if err != nil {
		return err
	}
	dest := *o
	if dest == "" {
		abs, err := filepath.Abs(doc)
		if err != nil {
			return err
		}
		dest = strings.TrimSuffix(abs, filepath.Ext(abs)) + ".html"
	}
	if err := serve.WriteFileAtomic(dest, page); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, dest)
	return err
}
