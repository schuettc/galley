// manifest.go validates THE AGENT'S TESTIMONY about a round it just landed.
//
// THE SERVER OWNS THE CHANGE LIST; THE AGENT MAY ONLY ANNOTATE IT. What
// changed between the version the agent was handed and the file it left behind
// is computed here, by internal/diff, exactly as it is computed everywhere else
// — the agent's manifest may not declare a change, only comment on one that the
// diff already found. That is the whole rule, and both checks below are it:
//
//   - the QUOTE, normalised the way the diff normalises, must fall inside
//     EXACTLY ONE changed region. Not "at least one": a locator that lands in
//     two regions cannot say which change its note is about, and guessing which
//     is the one thing this file exists to refuse.
//   - every key in ANSWERS must be one the server actually sent that round.
//     The instruction's thread is deleted by the send that carried it, so the
//     keys live on the round record (versions.Round.Asks) and nowhere else.
//
// AN ENTRY FAILING EITHER CHECK IS DROPPED, AND THE ROUND STILL LANDS. The
// change renders anyway — with no ask line and no note, which is the rendering
// an unattributed change already gets — and the drop count is logged so a
// mis-shaped manifest is visible rather than silent. It is deliberately NOT an
// error return: CLAUDE.md's "AN ERROR RETURN IS AN INVITATION, and the ledger's
// whole invariant is that nobody accepts it" cuts straight against making a
// round's landing contingent on the agent's own bookkeeping. The agent's work
// is in the file either way; its notes about that work are not worth failing
// the landing over.
//
// NOTHING HERE IS STORED AS A DIFF. What survives is versions.Change — the
// agent's own quotation and its own sentence, neither of which is derivable
// from the two documents — and the quotation is re-matched against a freshly
// computed diff every time a reading state renders it.
package serve

import (
	"fmt"
	"os"
	"strings"

	"github.com/subaud-tools/galley/internal/diff"
	"github.com/subaud-tools/galley/internal/versions"
)

// ackChange is one entry of the manifest as it arrives on the wire, on both
// carriers. Quote is the agent's own quotation of text it wrote — its way of
// saying WHICH change it means — and it is a quote rather than an index for
// the reason no ordinal is ever persisted here: an ordinal renumbers, and
// `CHANGE k OF K` is computed at render time from the recomputed region list.
type ackChange struct {
	Quote   string   `json:"quote"`
	Answers []string `json:"answers,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// matchManifest keeps the entries it can prove and counts the rest.
func matchManifest(before, after string, sent []string, in []ackChange) (kept []versions.Change, dropped int) {
	if len(in) == 0 {
		return nil, 0
	}
	regions := changedTexts(diff.Diff(before, after))
	known := make(map[string]bool, len(sent))
	for _, k := range sent {
		known[k] = true
	}
	for _, e := range in {
		quote := diff.Norm(e.Quote)
		if quote == "" || !inExactlyOneRegion(quote, regions) || !allSent(e.Answers, known) {
			dropped++
			continue
		}
		kept = append(kept, versions.Change{
			Locator: strings.TrimSpace(e.Quote),
			Answers: e.Answers,
			Note:    strings.TrimSpace(e.Note),
		})
	}
	return kept, dropped
}

// allSent is the second check. AN EMPTY Answers IS LEGAL and passes: a change
// the agent chose not to attribute is still a change it made, and the reading
// state renders it with no ask line. Refusing one would push an agent toward
// naming a key it is not sure of, which is the opposite of the point.
func allSent(answers []string, known map[string]bool) bool {
	for _, k := range answers {
		if !known[k] {
			return false
		}
	}
	return true
}

// soleRegion is the first check AND ITS ANSWER: the index of the one region
// whose after-text contains the quote, or -1 when none does or more than one
// does. The counting is the check — a locator landing in two regions cannot say
// which change its note is about, and guessing is what this file refuses.
//
// It returns the INDEX rather than a yes, because the reading state needs the
// same answer the ack needed and needs to know WHICH: the ack asks "may I keep
// this testimony?" and the render asks "beside which mark do I draw it?", and
// those are one question asked twice. Two functions answering it would agree
// until the day they did not, and that day a note appears beside the wrong
// change with nothing to catch it.
//
// The index is an ordinal in RegionOps' order for THIS pair of documents — the
// same ordinal the rendered HTML writes into data-gly-region — and it is never
// stored anywhere. See versions.Change.
func soleRegion(quote string, regions []string) int {
	found := -1
	for i, r := range regions {
		if r == "" || !strings.Contains(r, quote) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

// inExactlyOneRegion is soleRegion asked as a yes-or-no, for the caller that
// only needs to know whether to keep an entry.
func inExactlyOneRegion(quote string, regions []string) bool {
	return soleRegion(quote, regions) >= 0
}

// changedTexts is the AFTER side of every region diff.Regions would count,
// normalised once so a quote can be tested against it directly.
//
// It walks the same ops on the same terms Regions does — an absorbed op is
// part of a neighbour and speaks for nothing on its own, and a move is one
// fact counted at its deletion — so the population an entry is matched against
// is the population the reader is shown, rather than a second reading of the
// same list that would agree until it did not.
//
// A DELETION HAS NO AFTER TEXT and therefore matches nothing, which is correct:
// the manifest quotes text the agent WROTE, and nobody can quote a removal. A
// MOVE reaches its relocated text through the partner the diff already paired,
// for the same reason — the words are on the page, at the far end of one fact.
//
// IT IS POSITIONAL, AND THE EMPTY STRINGS ARE LOAD-BEARING. Region k's text is
// out[k] — the same k the render wrote into data-gly-region — so a region with
// no quotable after text holds "" rather than being skipped. Skipping would
// shift every later region's index by one and hand a note to its neighbour,
// which is precisely the defect this file's whole discipline exists to prevent.
// An empty entry matches no quote, because soleRegion refuses it outright and a
// quote is never empty by the time it gets here.
func changedTexts(ops []*diff.Op) []string {
	regions := diff.RegionOps(ops)
	out := make([]string, len(regions))
	for i, o := range regions {
		var parts []string
		add := func(u *diff.Unit) {
			if u == nil || u.Kind == diff.KindBoundary {
				return
			}
			parts = append(parts, u.Text)
		}
		add(o.New)
		if o.Moved && o.Partner != nil {
			add(o.Partner.New)
		}
		for _, m := range o.Members {
			add(m.New)
		}
		if len(parts) == 0 {
			continue
		}
		out[i] = diff.Norm(strings.Join(parts, " "))
	}
	return out
}

// regionPlaces is WHERE ON THE PAGE each region is, said the way a reader would
// say it: the nearest heading above it, lowercased. Positional like
// changedTexts and in the same order, so places[k] describes region k.
//
// It is the nearest PRECEDING heading rather than the enclosing section
// computed from heading levels, because the card only needs to answer "where am
// I being sent?" — and a heading that is itself the change names itself, which
// is the right answer for a renamed heading.
func regionPlaces(ops []*diff.Op) []string {
	regions := diff.RegionOps(ops)
	at := make(map[*diff.Op]int, len(regions))
	for i, o := range regions {
		at[o] = i
	}
	out := make([]string, len(regions))
	place := ""
	for _, o := range ops {
		if o.Absorbed {
			continue
		}
		u := o.New
		if u == nil {
			u = o.Old
		}
		if u != nil && u.Kind == diff.KindHeading {
			place = heading(u.Text)
		}
		if i, ok := at[o]; ok {
			out[i] = place
		}
	}
	return out
}

// heading strips the markdown that makes a heading a heading and lowercases
// what is left, because the card renders a place, not a title.
func heading(text string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(text), "#")))
}

// ackManifest validates the agent's testimony against the round it answers.
//
// THE TWO DOCUMENTS ARE THE ONES THE AGENT WORKED BETWEEN: the version cut by
// the send that opened this window, and the file the agent has left on disk —
// which, while the window is open, IS the agent's draft. Both are read through
// historyDocument for the reason changedRegions reads them that way, so the
// regions matched against here are the regions the reading state will show.
//
// A ROUND NOBODY CAN NAME PROVES NOTHING. If the handoff's own version cannot
// be read — a cut that never reached disk, a store that will not open — there
// is no computed change list to annotate, so every entry is dropped rather than
// believed. That is the file's rule, not a degradation of it.
//
// Callers must NOT hold mu.
func (s *EditServer) ackManifest(in []ackChange) []versions.Change {
	if len(in) == 0 {
		return nil
	}
	s.reviseMu.Lock()
	l := s.handoffLease
	s.reviseMu.Unlock()
	round := 0
	if l != nil {
		round = l.Round
	}
	kept, dropped := matchManifest(s.handoffBefore(round), s.landedFile(), s.keysSent(round), in)
	if dropped > 0 && s.Log != nil {
		s.Log(fmt.Sprintf("manifest: %d of %d changes dropped (unmatched quote or unsent key)", dropped, len(in)))
	}
	return kept
}

// handoffBefore is the document the agent was handed, or "" when the round that
// handed it over has no readable version — in which case nothing changed as far
// as the diff is concerned and the whole manifest drops.
func (s *EditServer) handoffBefore(round int) string {
	if round <= 0 {
		return ""
	}
	content, err := s.Versions().Content(round)
	if err != nil {
		return ""
	}
	return historyDocument(content)
}

// landedFile is what the agent left on disk. While a handoff is open the .md is
// the agent's own draft, which is exactly the thing being testified about.
func (s *EditServer) landedFile() string {
	raw, err := os.ReadFile(s.MdPath)
	if err != nil {
		return ""
	}
	return historyDocument(string(raw))
}

// keysSent is the identity half: the keys this server actually handed over on
// the round being answered, read off the round record because the send that
// carried them deleted their threads. See versions.Ask.
func (s *EditServer) keysSent(round int) []string {
	if round <= 0 {
		return nil
	}
	r, ok, err := s.Versions().Get(round)
	if err != nil || !ok {
		return nil
	}
	out := make([]string, 0, len(r.Asks))
	for _, a := range r.Asks {
		if a.Key != "" {
			out = append(out, a.Key)
		}
	}
	return out
}
