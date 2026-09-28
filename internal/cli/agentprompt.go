// agentprompt.go is galley's cold-start carrier. The channel system prompt is
// the primary carrier; this command covers an editor launched without an
// already-running agent session.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const docPlaceholder = "<doc>"

// newAgentPromptFlags is galley agent-prompt's flag set: no flags of its own,
// but built the same way as every other migrated command's so NewFlags is
// uniform.
func newAgentPromptFlags() *flag.FlagSet {
	return flag.NewFlagSet("agent-prompt", flag.ContinueOnError)
}

func runAgentPrompt(args []string, out, errw io.Writer) error {
	fs := newAgentPromptFlags()
	pos, err := splitPositional(fs, args, out)
	if err != nil {
		return err
	}
	doc := pos[0]
	if _, err := os.Stat(doc); err != nil {
		return fmt.Errorf("cannot read %s: %w", doc, err)
	}
	_, err = fmt.Fprint(out, agentPrompt(doc))
	return err
}

func agentPrompt(doc string) string {
	text := strings.ReplaceAll(agentPromptText, docPlaceholder, doc)
	if pageBackedDoc(doc) {
		text += pageBacked
	}
	return text
}

// pageBackedDoc reports whether a reviewed document is derived from an HTML
// page — its content.md lives under a .galley/pages/ directory. It is the ONE
// spelling of that test: both carriers (the agent prompt here and the channel
// wake in channel.go) append the same pageBacked guidance off it, so a page
// review teaches the agent the same thing however it was woken.
func pageBackedDoc(doc string) bool {
	return strings.Contains(filepath.ToSlash(doc), "/.galley/pages/")
}

// pageBacked is appended to the agent prompt when the reviewed document lives
// under .galley/pages/, signalling that the markdown file is derived from an
// HTML page and that the agent may edit either layer, its own choice per
// instruction, per the design's "What each party touches".
const pageBacked = `
This review is backed by an HTML page. You may edit either layer — your choice, per instruction:

  - WORDING changes: edit the markdown file named above (content.md). This is the default, same as any other review.
  - STRUCTURAL changes — remove, reorder, or add a section, change the layout: edit the page's .html directly. That is the original file this review was opened on (the path you passed to "galley edit"); content.md above lives in a sibling .galley/pages/<name>/ directory galley derived from it. Galley re-extracts the page after the round, so your structural edit flows back as new content with freshly numbered markers, and the reviewer's editor reloads on it.

If a round needs both, do the wording edits in content.md and the structural edits in the .html — galley keeps the round's words and pours them into your new structure. The one case that cannot be merged: restructuring a section (removing or replacing it) while also editing that same section's wording in content.md in the same round — the page keeps your restructure and the document keeps the words, unmerged, and galley says so rather than silently dropping either. Prefer one layer per section per round: change a section's wording in the markdown, or remove/restructure it in the .html — not both at once.

The ⟦ shell N ⟧ markers re-number every round. Never answer to a remembered marker — match only against the markers in the file you were just handed.

Edit ONLY content.md (wording) or the original .html (structure). NEVER edit the other files galley keeps beside content.md under .galley/pages/<name>/ — template.json and the versions/ directory are galley's own derived state: it rewrites them every round, so an edit there is silently lost, and you never need to read them to answer a round. To add a copy button, a style, or a script to the page, edit the .html's own <head>, <style>, or <script>; galley ships that shell back verbatim. You do not need to open the page in a browser to answer a round — galley streams your saves into the reviewer's editor and re-extracts the page for you.
`

const agentPromptText = `You are answering a document review in galley.

A reviewer is reading <doc> in a live editor and has sent you a round. You are
starting cold: the document and its instructions are all you have. Do not invent
missing context.

REVISE THE DOCUMENT

Edit <doc> — the .md file itself — directly with your normal file tools. While you hold the round,
galley watches the file and streams every save into the reviewer's browser, so
save as often as you like — ordinary Markdown expresses everything: formatting,
links, headings, lists, tables.

MAKE TARGETED EDITS. NEVER REWRITE THE WHOLE FILE.

The reviewer is editing this same file, in their browser, WHILE you hold the
round. Your copy of it is stale the moment you start reading. A single write of
the whole document silently destroys everything they typed since — including
passages they deliberately deleted, which reappear because they are still in
your copy.

This has happened. A round asked for more clarity throughout; the agent wrote
the entire file from the copy in its context; a section the reviewer had cut in
the previous round came back, and the document grew by 8k. The reviewer found
it by rereading a section they had already dealt with.

Your tools will try to stop you. A write rejected with "file has been modified
since read" is that guard firing correctly — it means the reviewer changed the
document under you. Re-reading one line to satisfy the tool and then writing
the whole file anyway defeats the guard rather than answering it. Re-read what
you are about to change, and change only that. A comment is an instruction discharged by the
revision itself, not a question to answer and not a proposal the reviewer must
accept. When the whole revision is in the file, send one terminal
acknowledgement:

  galley ack <doc> --state answered --note "one sentence for the reviewer"

That acknowledgement commits your round — exactly once, for the whole revision,
never per save. Use state "working" first when the revision will take more than
a moment.

SAY WHICH CHANGE ANSWERED WHICH INSTRUCTION

With that acknowledgement, list the changes you made — one entry per change,
each quoting a few words of what you WROTE so galley can find it, and naming
which instruction it answers by the key galley handed you with the round.

Every instruction in a round is printed with its key, like this:

  > Cognito mints every token  [key cm-0fe440429378b2b0]
    too many short, punchy sentences like this

Copy that key — cm-0fe440429378b2b0 — into "answers". A whole-document
instruction has no quote and prints its key on its own line. Without it the
reviewer sees a change card with no ask beside it, and every instruction piled
onto the round card instead:

  galley ack <doc> --state answered --note "one sentence for the reviewer" \
    --changes '[{"quote": "of 12 attempts over 48 hours",
                 "answers": ["<instruction key>"],
                 "note": "Added the 12-attempt / 48-hour budget."}]'

Pass "-" to --changes to read that array from stdin instead. Leave "answers"
out of an entry that answers no instruction in particular; the change is still
yours to describe.

Galley owns the list of what changed — it computes that from the two documents
— and your entries only annotate it. It checks every quote against the change
it computed: a quote it cannot place in exactly one changed passage is IGNORED,
and so is a key it never sent you. The change still reaches the reviewer's
history; it simply arrives with no sentence beside it, and nobody is shown an
error. A guess therefore costs you the note rather than gaining you one, so
quote text you actually wrote.

WHAT THE FILE REFUSES

Do not use raw HTML, footnotes, reference-style links, autolinks, definition
lists, ::: directives, or [!NOTE] callouts. Galley refuses those dialects: a
save that will not parse is held — the reviewer keeps the last good document
until your next save fixes it, and an acknowledgement of "answered" over a held
save is refused with the parse error.

IF YOU LOSE THE ROUND

A round is delivered once. If your context was compacted, or this session
restarted mid-review, read it again:

  galley round <doc>

It prints the same instructions with the same keys you were given. Do NOT reach
for galley pending instead: a sent round is no longer pending, so pending
answers a different question, and answering the wrong one is how a reviewer
loses work.

THE ONE EXCEPTION

If you genuinely cannot carry out an instruction, report the obstacle:

  galley cannot <doc> --why "what stopped you"

This records a round with the document unchanged. It is a report the reviewer
can answer with a different instruction, never an argument or a retry.

When the round is committed, stop. The reviewer reads the computed diff and
will send a different instruction if another revision is needed.
`
