// wake.go is THE ONE PLACE A WAKE MEANS SOMETHING.
//
// Two carriers speak this protocol and they used to spell it twice. `galley
// wait` is the PULL half (see wait.go's header, and the README, which calls it
// the loop to reach for first); the channel's notifications are the PUSH half.
// Both have to answer the same two questions about every wake — what happened,
// and is the review over — and for one wake they answered differently.
//
// THE TRUST EXIT IS AN APPROVE THAT IS NOT AN ENDING. "✓ Accept all & approve"
// sweeps, seals, and hands the reviewer's surviving notes to the agent; the
// review is closed to the REVIEWER and stays open to the AGENT until that work
// lands (seal.go's handoff section is the whole argument). #72 taught the push
// carrier that — `approveContent` branches on the thread count — and the pull
// carrier was never told. Measured against the shipped binary at fc2319d, with
// the server holding `verdict: approved-entrusted, entrusted: 2, outstanding:
// 2`, a parked `galley wait` printed:
//
//	the reviewer approved hand.md as it stands — the review is over; do not re-arm
//
// Both halves false. `galley suggest` was still accepted against that document
// and `galley ack --state answered` still refused it as unfinished — the
// handoff was open, and the one loop the docs promote told the agent to stop.
//
// So the BRANCH and the WORDS live here and both carriers render through them.
// Not "agree by hand": this repository's own repeated lesson is that two sites
// spelling one rule is the shape just before the gap (CLAUDE.md, "six agreeing
// spellings and one silence"), and a carrier fixed while its twin is forgotten
// is how this arrived.
//
// WHAT IS DELIBERATELY NOT SHARED: `revise` and `settle`. Those two sentences
// name their own carrier's next verb — the channel's says `galley_ack`, an MCP
// tool a shell loop cannot call, and the CLI's is the re-arm pointer wait.go
// prints — and there is nothing about the REVIEW's meaning in either to
// disagree about. The four ENDINGS are where the meaning lives, and where the
// disagreement was.
package cli

import "fmt"

// wake is one wake reduced to what its meaning depends on: the reason, the
// document it is about, the reopen's note, and the two counts. Nothing else in
// either carrier's payload changes the answer.
type wake struct {
	reason string
	page   string
	// note is the reopen's reason and is empty on every other wake.
	note         string
	instructions int
}

// over reports whether the review has ENDED — whether a loop should stop
// rather than come back. It is the one question a waiting agent gets wrong at
// the highest cost, so it is asked in one place and never re-derived.
//
// AN APPROVE IS AN ENDING ONLY WHEN IT ENTRUSTED NOTHING. Both halves are
// measured facts about the same server: after a trust press with two threads
// surviving, `galley suggest` was accepted and `galley ack --state answered`
// was refused as unfinished — the review was demonstrably not over, and the pull
// carrier said it was. `handoff()` is the same branch approveContent takes, so
// the sentence and the instruction cannot come apart.
func (w wake) over() bool {
	switch w.reason {
	case reasonApprove:
		return true
	case reasonClosed:
		return true
	}
	return false
}

// sentence renders what this wake means, in the words both carriers use, and
// returns "" for the wakes each carrier says in its own voice (see the header).
func (w wake) sentence() string {
	switch w.reason {
	case reasonApprove:
		return approveContent(w.page)
	case reasonClosed:
		return goneContent(w.page, w.note)
	}
	return ""
}

// approveContent renders the approve wake's sentence. It branches exactly as
// channelInstructions and agentPromptText teach: zero surviving threads is a
// clean approve (review over, proceed); threads>0 is TRUST's entrusted handoff.
//
// THREADS IS THE BRANCH KEY, AND THE REASON HAS CHANGED WITHOUT THE ANSWER
// CHANGING. It used to be defensive: a surviving unanswered range comment left
// its HIGHLIGHT riding the suggestions count after the sweep, so suggestions
// could not be trusted to be 0. That count reads Pending.Decidable now (see
// waitWire.suggestions) and a highlight is not in it. Threads stays the key
// because it is the RIGHT question rather than the surviving one — see
// wake.handoff, which is now the one place that asks it.
func approveContent(page string) string {
	return fmt.Sprintf("The reviewer APPROVED %s as it stands. The review is over — proceed.", page)
}

// discardContent and reopenContent are the two other wakes' sentences, split
// out for the reason approveContent is: their wording is the protocol, and
// both carriers have to agree about it.
//
// A discard is TERMINAL and says so — the same "the review is over" shape as a
// clean approve, with the one fact that makes it a different ending: the
// markup was thrown away rather than applied, so nothing the agent proposed is
// in the document.
// goneContent is the sentence for the ending nobody chose. It is terminal like
// approve and discard, and unlike either it carries no verdict — so it says
// what did NOT happen, which is the part that decides what the agent does
// next: there is nothing to answer, nothing to apply, and nothing more coming.
func goneContent(page, why string) string {
	return fmt.Sprintf("The editor for %s is GONE — %s. The review ended with no verdict: "+
		"no approve, no discard, and nothing further will arrive on it. Stop waiting for this review. "+
		"If the document still needs a look, `galley edit %s` opens a new review.", page, why, page)
}
