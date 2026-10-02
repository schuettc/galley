package suggest

import (
	"github.com/schuettc/galley/internal/docmodel"
)

// MintRuns gives every suggestion mark in d an identity, returning the result.
// Marks that already carry one keep it unchanged.
//
// Where this is called matters. It runs at the session boundary — as a
// document enters the edit server, and after every transform, before the
// result goes back into the CRDT — rather than inside ydoc.Load. Load is a
// transparent writer: what you Load is exactly what you Read, and the bridge's
// round-trip tests assert that. Minting in there would quietly make Load
// return more than it was given, and cost the property that found most of the
// document model's real bugs.
//
// Idempotence is what makes it safe on that hot path. The mutate cycle is
// ReadLive -> transform -> Load, and it runs on every change; marks that came
// back from ReadLive already carry their runs, so only the ones the transform
// just created get new ones. Re-minting instead would renumber the whole
// document under every open card, mid-session.
//
// It mints ONE RUN PER MARK, and that is right for everything it is handed.
// What it is handed is a DOCUMENT — marks parsed back from CriticMarkup, marks
// the browser wrote — in which adjacent same-author, same-second marks are the
// "{--age--}{--age--}" case above and must stay two decisions. That is
// indistinguishable, from the document alone, from one authored edit spread
// across several inlines. So this does not try to tell them apart: the
// transforms that DO know which they made — CommentOn, CommentOnRange and
// CommentAcross — stamp one run across every inline their edit touches
// before this ever sees it, and the skip below leaves it exactly as stamped.
// Do not add grouping here. It would have to guess, and it would guess wrong on
// the single case runs were introduced for.
//
// Tokens are random rather than sequential because there are four minters —
// this one, markdown's applyMark, the comment transforms and the BROWSER — in
// different packages and two languages, running at different times, with no
// counter they could share.
//
// THE FOURTH IS THE CLIENT, and it is why a typed edit no longer arrives here
// to be split. web/suggestions.js stamps ONE run across every inline the mark
// it just applied touches, for exactly the reason commentMarked does on
// this side: the editor knows the extent of that edit and this function
// cannot. A mark the reviewer types reaches here already carrying a run, and
// the skip above leaves it as stamped.
//
// This comment used to say the browser never minted. That was TRUE, and it was
// the bug: a typed edit spanning formatting got one run PER MARK here and split
// into a card per inline. Two other doc comments meanwhile asserted the
// opposite, which is what made the gap look handled through two rounds of
// fixing everything around it. Read web/suggestions.js before changing this
// sentence in either direction.
func MintRuns(d docmodel.Doc) docmodel.Doc {
	out := d
	out.Blocks = mintBlocks(d.Blocks)
	return out
}

func mintBlocks(blocks []docmodel.Block) []docmodel.Block {
	if blocks == nil {
		return nil
	}
	out := make([]docmodel.Block, len(blocks))
	for i, b := range blocks {
		b.Inlines = mintInlines(b.Inlines)
		b.Children = mintBlocks(b.Children)
		out[i] = b
	}
	return out
}

func mintInlines(inlines []docmodel.Inline) []docmodel.Inline {
	if inlines == nil {
		return nil
	}
	out := make([]docmodel.Inline, len(inlines))
	for i, in := range inlines {
		if len(in.Marks) > 0 {
			marks := make([]docmodel.Mark, len(in.Marks))
			for j, m := range in.Marks {
				marks[j] = mintMark(m)
			}
			in.Marks = marks
		}
		out[i] = in
	}
	return out
}

func mintMark(m docmodel.Mark) docmodel.Mark {
	if !addressable(m.Kind) || m.Attrs[docmodel.RunAttr] != "" {
		return m
	}
	// Copied rather than written through: the caller's document may share this
	// map with a model something else still holds, and a mutate cycle that
	// wrote into it would reach back into the snapshot it was derived from.
	attrs := make(map[string]string, len(m.Attrs)+1)
	for k, v := range m.Attrs {
		attrs[k] = v
	}
	attrs[docmodel.RunAttr] = newRun()
	m.Attrs = attrs
	return m
}

// addressable reports whether a mark kind is one a card can point at.
// Formatting marks are not suggestions and have nothing to decide, so they
// carry no identity.
func addressable(k docmodel.MarkKind) bool {
	return k == docmodel.Ins || k == docmodel.Del || k == docmodel.Highlight
}

// newRun is docmodel.NewRun. It moved there when the CriticMarkup parser
// became a minter too: markdown cannot import suggest, and all the minters
// have to draw from one namespace.
func newRun() string { return docmodel.NewRun() }
