package serve

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/reearth/ygo/crdt"
	"github.com/schuettc/galley/internal/diff"
	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/review"
)

// REVERTING ONE EDIT IS NOT UNDO, AND THAT IS WHY IT EXISTS.
//
// ⌘Z is SEQUENTIAL: it walks back through what you did, most recent first.
// This is TARGETED — put back the paragraph I deleted three edits ago and keep
// the two I made after it — which is what Word's Reject and Docs' suggestion
// controls do, and what undo cannot do at any price.
//
// Court, on the card needing a press: "what if the edit card did have something
// to press… isn't this how most editors work?" It is, and the one part that
// does not transfer is accept/reject: in those tools a change is a PROPOSAL
// awaiting somebody's decision, and here the edit is the reviewer's own and
// already applied. Nobody is being asked. So there is one verb, and it is this.
//
// IT WORKS IN BLOCKS OF SOURCE, NOT IN A RENDERED MODEL, and that is what keeps
// it from being a second markdown writer. `diff.Blocks` splits a document into
// blocks whose `Text` is the RAW SOURCE — "# A careful review", "- one" — so a
// document is rebuilt by joining them, exactly as it was written. Reconstructing
// from `docmodel` instead would mean re-rendering, and this repository has one
// renderer on purpose.
//
// IT REFUSES WHAT IT CANNOT DO EXACTLY. A revert that lands the words in a
// plausible-but-wrong place is worse than a button that says no: the reviewer
// would have to notice, and the whole point of the verb is that they should not
// have to hold the document in their head. Three shapes are handled and
// anything else is refused by name.
//
// IT REBUILDS FROM THE MARKED DOCUMENT, and finds its blocks through `clean`.
// The change was found by comparing documents with every instruction mark
// lifted (see plainText), so `before` and the change's words are clean; the
// document it writes back is `afterMarked`, the live serialization with every
// `{==…==}{>>@comment …<<}` and every ID note still in it. Rebuilding from the
// clean side put the paragraph back and took every instruction's place with
// it. `clean` is applied one block at a time, to find a block by what it says;
// the block written back is always its marked source.
func revertChange(before, afterMarked string, target ReviewerChange, clean func(string) string) (string, error) {
	beforeBlocks, afterBlocks := diff.Blocks(before), diff.Blocks(afterMarked)
	said := make([]string, len(afterBlocks))
	for i, b := range afterBlocks {
		said[i] = strings.TrimSpace(clean(b.Text))
	}
	want := strings.TrimSpace(target.Before)
	now := strings.TrimSpace(target.After)

	switch target.Kind {
	case "removed":
		// A BLOCK THE REVIEWER DELETED, put back where it was. Its place is
		// found through the block ABOVE it that still exists — an index into
		// the old document means nothing in the new one, which is this
		// codebase's oldest rule about ordinals wearing different clothes.
		at := blockIndex(beforeBlocks, want)
		if at < 0 {
			return "", errors.New("that removal does not match a whole block, so there is no place to put it back")
		}
		insert := len(afterBlocks)
		for i := at - 1; i >= 0; i-- {
			if j := saidIndex(said, strings.TrimSpace(beforeBlocks[i].Text)); j >= 0 {
				insert = j + 1
				break
			}
		}
		if at == 0 {
			insert = 0
		}
		// AFTER THE NEIGHBOUR'S OWN NOTES. A block comment's ID note sits on
		// its own line under the block it is about, and says nothing once
		// clean. Putting the restored block between the two would hand the
		// comment to the wrong block.
		for insert > 0 && insert < len(afterBlocks) && said[insert] == "" {
			insert++
		}
		out := make([]diff.Block, 0, len(afterBlocks)+1)
		out = append(out, afterBlocks[:insert]...)
		out = append(out, beforeBlocks[at])
		out = append(out, afterBlocks[insert:]...)
		return joinBlocks(out), nil

	case "added":
		// The marked block goes whole: any mark inside it is on words the
		// reviewer is taking back, which is what deleting them does anyway.
		at := saidIndex(said, now)
		if at < 0 {
			return "", errors.New("that addition does not match a whole block, so it cannot be taken out on its own")
		}
		return joinBlocks(append(append([]diff.Block{}, afterBlocks[:at]...), afterBlocks[at+1:]...)), nil

	case "changed":
		// SUBSTITUTED INSIDE THE ONE BLOCK THAT HOLDS IT, and only if exactly
		// one does. Two blocks containing the same sentence is genuinely
		// ambiguous, and picking the first would be the wrong-place failure
		// this function refuses rather than guesses at.
		hits := 0
		at := -1
		for i := range afterBlocks {
			if strings.Contains(said[i], now) {
				hits++
				at = i
			}
		}
		if hits != 1 {
			return "", errors.New("that edit's text appears " + plural(hits) + " in the document, so reverting it would have to guess which")
		}
		// THE WORDS MUST STAND IN THE MARKED SOURCE AS THEY STAND IN THE CLEAN.
		// A highlight's markers inside the changed words leave no literal
		// substitution that keeps the mark, and moving the mark to where it
		// probably belongs is the guess this function does not make.
		//
		// BUT ONLY THE WORDS THAT CHANGED. A change is found a sentence at a
		// time, so `now` is the whole changed sentence, and a highlight
		// anywhere in it (not through the edit, just beside it) means the
		// sentence never stands literally in the marked source. So the
		// substitution is narrowed to the words that differ, widened a word at
		// a time on each side only until it is unique in the block.
		from, to, err := narrowedEdit(afterBlocks[at].Text, want, now)
		if err != nil {
			return "", err
		}
		out := append([]diff.Block{}, afterBlocks...)
		out[at].Text = strings.Replace(out[at].Text, from, to, 1)
		return joinBlocks(out), nil
	}
	return "", errors.New("galley does not know how to revert a " + target.Kind)
}

// words splits text into words, each carrying the whitespace after it, so the
// pieces join back to the text exactly.
var words = regexp.MustCompile(`\s*\S+\s*|\s+`)

// narrowedEdit is the substitution that turns now back into want inside the
// marked source: the run of words that differs between the two, with as many
// unchanged words either side as it takes to occur exactly once in source.
// No occurrence at all means a highlight's markers run through the changed
// words, which is refused rather than guessed around.
func narrowedEdit(source, want, now string) (from, to string, err error) {
	w, n := words.FindAllString(want, -1), words.FindAllString(now, -1)
	pre := 0
	for pre < len(w) && pre < len(n) && w[pre] == n[pre] {
		pre++
	}
	post := 0
	for post < len(w)-pre && post < len(n)-pre && w[len(w)-1-post] == n[len(n)-1-post] {
		post++
	}
	for k := 0; ; k++ {
		lo, hiN, hiW := max(pre-k, 0), min(len(n)-post+k, len(n)), min(len(w)-post+k, len(w))
		from = strings.Join(n[lo:hiN], "")
		to = strings.Join(w[lo:hiW], "")
		whole := lo == 0 && hiN == len(n)
		if from == "" && !whole {
			continue
		}
		switch strings.Count(source, from) {
		case 1:
			return from, to, nil
		case 0:
			return "", "", errRevertThroughHighlight
		}
		if whole {
			return "", "", errors.New("that edit's text appears more than once in the document, so reverting it would have to guess which")
		}
	}
}

// errRevertThroughHighlight is the refusal for a change whose words an
// instruction's highlight runs through.
var errRevertThroughHighlight = errors.New("that edit runs through an instruction's highlight — delete the instruction or change the words by hand")

// saidIndex is the ONE block whose clean text is `want`, or -1 if none is or
// more than one is.
func saidIndex(said []string, want string) int {
	found := -1
	for i, s := range said {
		if s != want {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// blockIndex is the ONE block whose whole text is `said`, or -1 if none is or
// more than one is. Ambiguity is refused rather than resolved by position.
func blockIndex(blocks []diff.Block, said string) int {
	found := -1
	for i, b := range blocks {
		if strings.TrimSpace(b.Text) != said {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// joinBlocks rebuilds a document from blocks whose Text is raw source. The
// blank line between blocks is what `diff.Blocks` split on, so putting it back
// is the inverse rather than a formatting choice.
func joinBlocks(blocks []diff.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if t := strings.TrimRight(b.Text, "\n"); strings.TrimSpace(t) != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func plural(n int) string {
	if n == 0 {
		return "nowhere"
	}
	return "more than once"
}

// handleRevert puts one of the reviewer's own edits back.
//
// IT RECOMPUTES THE LIST RATHER THAN TRUSTING THE PRESS. The browser sends a
// key and nothing else: the words to restore come from the server's own diff of
// the last version against the live document, so a card left open while the
// document moved cannot revert text the server never computed. A key it does
// not recognise is a 404 and not a guess.
//
// IT REBUILDS THROUGH THE ORDINARY WRITE PATH — parse the reverted markdown and
// hand it to `mutate` — so it inherits the projection, the version cut and the
// notify discipline every other mutation has. It is a TEXT change, so
// `ydoc.Write` correctly falls back to a full reload and the browser's undo
// stack is orphaned. That is accepted rather than overlooked: Court, asked
// directly, said "we can lose the undo. that's ok." Targeted text mutation is
// what removes the cost, and this becomes cheap the day it lands without
// changing here.
func (s *EditServer) handleRevert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.refuseSealed(w, verbReview) {
		return
	}
	if s.refuseHandoff(w) {
		return
	}
	if in.Key == "" {
		http.Error(w, "which edit? pass its key", http.StatusBadRequest)
		return
	}

	code, err := s.mutate(byReviewer, func(model docmodel.Doc) (docmodel.Doc, func(*crdt.Doc, review.Tx), error) {
		store := s.Versions()
		latest, ok, err := store.Latest()
		if err != nil || !ok {
			return docmodel.Doc{}, nil, errors.New("this document has no round to revert against yet")
		}
		before, err := store.Content(latest.N)
		if err != nil {
			return docmodel.Doc{}, nil, errors.New("the last round cannot be read, so there is nothing to restore from")
		}
		// THE SAME COMPARISON THE RAIL MADE, or the key would not be found and
		// the revert would be computed against a document that includes the
		// instruction markup the rail deliberately strips. See plainText.
		//
		// THE MARKED SIDE IS SERIALIZED FROM THE MODEL ITSELF. plainText
		// compares a cleared copy (ClearInstructions never touches the model it
		// is given), so the model still carries the marks this revert exists
		// to keep.
		marked := string(markdown.Serialize(model))
		before = plainOf(before)
		changes, _ := summarise(diff.Diff(before, plainText(model)))
		for i := range changes {
			changes[i].Key = changeKey(changes[i])
		}
		var target *ReviewerChange
		for i := range changes {
			if changes[i].Key == in.Key {
				target = &changes[i]
				break
			}
		}
		if target == nil {
			return docmodel.Doc{}, nil, errUnknownChange
		}
		reverted, err := revertChange(before, marked, *target, plainOf)
		if err != nil {
			return docmodel.Doc{}, nil, err
		}
		out, _, err := markdown.Parse([]byte(reverted))
		if err != nil {
			return docmodel.Doc{}, nil, err
		}
		return out, nil, nil
	})
	if err != nil {
		if errors.Is(err, errUnknownChange) {
			http.Error(w, "that edit is not in this round any more — the document moved under the card", http.StatusNotFound)
			return
		}
		if code == 0 {
			code = http.StatusBadRequest
		}
		writeMutationError(w, code, err)
		return
	}
	s.writePending(w)
}

// errUnknownChange separates "the card is stale" from "this cannot be reverted",
// because the two want different words and different status codes.
var errUnknownChange = errors.New("no such change in this round")
