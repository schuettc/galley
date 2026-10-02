// typing.mjs — WHAT THE REVIEWER TYPES APPLIES, ON A REAL KEYBOARD.
//
// The gate that did not exist, and whose absence is the whole reason Gap 5
// reached a third round — kept, and REPURPOSED, because the contract it
// guards inverted.
//
// Its first life: one defect had three populations — an edit authored through
// Go, a span read from a file, and a mark the reviewer TYPES — and the third
// survived two rounds of fixes because a doc comment claimed the browser
// minted runs and nobody drove an editor to find out it did not. This file was
// written to drive the layer above the plugin: a real chromium, a real
// `galley edit`, a real Backspace keystroke, and the SERVER asked what it made
// of it.
//
// Its second life is the reviewer's-hand contract
// (docs/superpowers/specs/2026-08-14-the-reviewers-hand.md): the reviewer's
// edits apply DIRECTLY. A keystroke produces NO ins/del mark, clean text in
// the document, and leaves the pending count alone. The browser-side run
// stamper is gone because the marks it stamped are gone. A gate that can fail
// is kept and pointed at the new truth: every check below that asserts "no
// mark, no card, clean file" ran RED against the tracked-typing build and
// green after the cut.
//
// ITS THIRD LIFE IS THE SAME CONTRACT WITH THE OTHER HALF OF IT REPLACED, and
// this file was DEAD for the whole interval — it died at the first `pending()`
// call with `Cannot read properties of undefined (reading 'filter')`, and a
// gate that throws four checks into a two-hundred-check run reports nothing at
// all about the hundred and ninety-six after it.
//
// WHAT MOVED. `pendingView` was `{suggestions, comments, changes, blocks}` and
// is `{instructions, blocks}` (internal/serve/editmode.go). The agent no
// longer PROPOSES: `galley suggest` and `galley apply` are deleted verbs,
// `/_galley/suggest` is not a route, ins/del marks have no producer, and the
// reviewer no longer accepts or rejects anything. The agent EDITS THE FILE and
// the reviewer FILES INSTRUCTIONS — immutable facts of a round, not a
// conversation, with no reply box and no resolve.
//
// SO THE SENTENCE THIS FILE PINS SURVIVED AND ITS SECOND CLAUSE CHANGED WHOSE
// WORK IT IS ABOUT. "A keystroke produces no mark, clean text in the file, and
// no change to the pending count" is still the contract; `pending` used to
// count THE AGENT'S PROPOSALS and now counts THE REVIEWER'S OWN INSTRUCTIONS,
// and the claim is the same claim either way, because it was never about who
// owned the other population — it was about the reviewer's KEYSTROKES being in
// neither. Typing is not an instruction any more than it was a proposal. Every
// `pending()` read below moved from `.suggestions` to `.instructions` and no
// check changed its meaning in the move.
//
// WHAT WAS DELETED RATHER THAN TRANSLATED, said here and again at each site,
// because a gate that keeps its count by inventing an equivalent reports
// safety it does not provide:
//
//   §3 IN WHOLE — "a reviewer edit across an agent mark is a verdict by hand".
//   It POSTed an agent proposal to `/_galley/suggest`, watched it land pending
//   and server-stamped, deleted across its ins mark, and asserted the proposal
//   vanished, the sidecar stayed coherent, and the trail entry named WHOSE
//   text the hand had landed on. Every noun in that paragraph is gone: the
//   route, the verb, the mark, the pending proposal, the sidecar's
//   `suggestions` metas. There is no reviewer verdict by hand because there is
//   nothing left to have a verdict about. Nine checks.
//
//   EVERY `trailChanges` READ — the trail as the SERVER holds it. The server
//   holds no trail: `pendingView` carries no `changes` and there is no
//   `/_galley/trail` route to POST one to. The browser's trail plugin is
//   entirely alive — ghosts, highlights, retraction, the set-assignment
//   re-anchor — so each of those checks is restated against `trailEntries()`,
//   THE PLUGIN'S OWN LIST, which carries the same eight fields (`old`, `new`,
//   `proposal`, `placed`, `before`, `after`, `anchored`). The CLAIM about the
//   trail's recording is kept exactly; the claim that it REACHED THE SERVER is
//   deleted, because it does not. `proposal` is a field nothing can populate
//   now and the two checks that read it went with §3.
//
//   §8'S `.gly-revise-trail` CLAUSE — "and it is counted in what the press
//   will tell the agent". That element does not exist: the primary reads
//   `Revise · N` off the INSTRUCTION count now (see web/rounds-ux.mjs), and
//   the trail is counted on nothing. One check.
//
// A FINDING THIS REPAIR TURNED UP, NOTED HERE UNTIL IT WAS FIXED AND LEFT
// SINCE AS A RECORD OF WHAT THE FIX WAS: `App.syncTrail` used to POST the
// trail to `/_galley/trail` on every 600ms debounce, and every one of those
// 404ed — there was no route, the reviewer's record of their own edits was
// browser-local, and it died with the tab regardless. `adoptTrail`,
// `applyServerTrail`, `readoptTrail`, `scheduleTrailSync` and `syncTrail`
// are now DELETED from entry.ts (see the note where they were) rather than
// pointed at a real route: the trail is an outgoing message to the agent,
// not a history, so there is nothing here for a server copy to be FOR.
//
// WHY IT IS ITS OWN GATE. motion.mjs measures rects across a click and
// layers.mjs reads computed style; neither types, and both have charters this
// check does not belong to. Like them it is NOT part of `verify` — it needs a
// chromium and a live server, neither of which CI has.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/typing.mjs
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. There is no `just typing`
// any more — the justfile keeps a target for `rounds-ux` alone — so it is
// spelled out here rather than pointed at a recipe that is not there.

import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// The refusal's sentences, read from the same module the bundle was built from.
// The point of exporting them is that the UI cannot drift into its own wording,
// and a gate that retyped them here would be the drift it is guarding against.
import { CODE_INSIDE, CODE_HINT, FRONT_MATTER_INSIDE } from './suggestions.ts';

const GALLEY = process.env.GALLEY || 'bin/galley';
const PORT = Number(process.env.PORT || 8253);

let failures = 0;
function check(name, ok, detail) {
  if (ok) {
    console.log(`ok    ${name}`);
    return;
  }
  failures += 1;
  console.log(
    `FAIL  ${name}${detail === undefined ? '' : ` — ${JSON.stringify(detail, null, 1)}`}`,
  );
}

// THE FIXTURE'S POINT IS STILL THE BOLD WORD IN THE MIDDLE. "The retryBudget
// value controls" is three inlines, because a bold run is its own inline — the
// shape that used to split one tracked strike into three cards. Under the new
// contract it earns its keep the other way: a direct deletion across a
// formatting boundary must remove all three inlines cleanly, leave no del
// mark on any of them, and put nothing in /_galley/pending.
//
// The third paragraph was where the AGENT proposed — insert_after over "names
// the dialer" — so a reviewer deletion could span an agent's pending
// insertion: the verdict by hand. THAT WHOLE GESTURE IS GONE (see §3's
// headstone), and the paragraph is KEPT UNTOUCHED rather than removed with it,
// because §7 needs prose nothing has struck to open a composer over and the
// empty-block counts §6, §8 and §9 turn on are stated against a document of a
// known shape. Removing a paragraph to tidy up a deleted section is how a
// fixture quietly stops being able to fail.
//
// THE FOURTH PARAGRAPH IS THE ONE REFUSAL THAT SURVIVES THE CUT. Inline
// `code` is literal text and stays read-only in the editor; the refusal is
// only visible from a keyboard, which is why it stays here rather than in
// probe.mjs alone. `maxRetries` is deliberately a different word from
// `retryBudget`: the selector below finds a phrase by searching text, and two
// paragraphs holding the same word would make every selection ambiguous.
//
// THE LAST PARAGRAPH IS §6'S, AND IT IS THE ONLY ONE THE REVIEWER LEAVES
// EMPTY AND WALKS AWAY FROM. Every other deletion above leaves text standing
// in its block, so its trail entry keeps a prefix or a suffix to be re-found
// by; this one empties its block and therefore has NO text on either side,
// which is the exact shape the re-anchor could not place. It is last so that
// nothing earlier can consume it.
//
// THE EMPTY FENCE IS AN EMPTY TEXTBLOCK AT REST, AND THE REVIEWER'S HAND
// CANNOT HAVE MADE IT ONE. A code block is a textblock like any other, and one
// with nothing between its fences carries `content.size === 0` from the moment
// the file is opened — before anybody has typed. The first re-anchor rung for
// a block-emptying deletion counted EVERY empty textblock in the document, so
// this one fence is enough to make §6's ghost refuse: two empties, ambiguity,
// adrift, and Court's original bug back unchanged in a document nobody would
// call unusual. It is also unreachable to the reviewer twice over — the
// transaction filter refuses every edit inside a fence — so counting it can
// only ever produce a false refusal or a wrong anchor.
//
// THE TABLE IS HERE FOR THE OPPOSITE REASON, AND WHAT IT SAID BEFORE WAS
// MEASURED, TRUE OF THE OBSERVABLE IT LOOKED AT, AND WRONG.
//
// It used to read: a blank cell does NOT reach the browser as an empty
// paragraph, because `markdown.tableRow` gave an empty cell no child at all
// and a `tableCell` with empty content does not satisfy TipTap's `block+`, so
// y-prosemirror drops the cell — "the projected .md still reads
// `| timeout | |`, so the file is intact and the loss is the editor's
// rendering only". Every word of that was observed. The conclusion was false,
// and THE FIXTURE IS WHY: its one blank cell was in the LAST COLUMN, and
// `markdown.cellTexts` pads a short row at the END. A row that came back one
// cell short was re-padded into the same bytes it started as, so the file
// really was intact — for that cell, and for no other. Move the blank cell
// one column left and `| timeout | | tail |` is written back as
// `| timeout | tail | |`: every value after the blank slides one column, in
// the author's file, from opening the document and touching nothing.
//
// So the table now carries a blank cell in the FIRST, MIDDLE and LAST column
// and a wholly blank row, and §0 reads the file back. A fixture with only the
// last-column case PASSES AGAINST THE BUG, which is what it did.
//
// A blank cell IS an empty paragraph now (internal/markdown/parse.go gives
// every cell a block, which is the fix), so the table contributes six at-rest
// empty textblocks the reviewer's hand could not have made. That is not a
// problem for §6 and §8, it is the population their exclusion was written
// for: `trail.ts`'s `reviewerBlocks` prunes a table WHOLE. The checks below
// pin both halves — the cells are empty, and exactly one empty block outside
// a table is the one the ladder may search.
//
// §9'S THREE ARE A RUN AND THE LAST TWO OF THEM ARE ADJACENT ON PURPOSE. FOOT,
// then the fox, then §6's block, in that order and at the document's END. §6's
// block is the LAST block in the file, so it has no block after it at all;
// strike the fox above it — which is already empty by then — and the fox's own
// entry records an EMPTY block after it. Those are two different facts and the
// evidence used to spell them the same way (''), so closing the gap handed the
// fox's ghost to §6's block, beside the ghost §6's block had honestly earned.
// FOOT exists because the block above the fox would otherwise be the empty
// FENCE, and Backspace against a fence is an edit the transaction filter
// refuses — the gap would never close and the gesture would prove nothing.
//
// §8'S TWO PARAGRAPHS ARE A PAIR AND THEIR ORDER IS THE TEST. `JOINS` is the
// block the emptied one closes up INTO; `CLOSED` is the one struck whole and
// then closed. They sit far from §6's block on purpose: the joined-away block
// and §6's block must not end up with the SAME neighbours, or a check written
// to prove an entry cannot be anchored onto a stranger's block would be
// proving that two blocks in the same place look alike.
const HERE = mkdtempSync(join(tmpdir(), 'galley-typing-'));
const DOC = join(HERE, 'the-reviewers-own-keyboard.md');
const PHRASE = 'The retryBudget value controls';
const CODE_WORD = 'maxRetries';
const WHOLE = 'The last line is struck whole and leaves its own block empty.';
const JOINS = 'A settled sentence above, which the closed-up gap joins into.';
const CLOSED =
  'This sentence goes away entirely and its gap is closed after it.';
const FOOT = 'A closing paragraph, which the very last gap closes up into.';
const FOX = 'The quick brown fox pauses at the edge of the paragraph.';
// §9b'S RUN, AND THE REPEATED LINE IS THE WHOLE OF IT. TWIN appears three
// times, word for word, with a struck line between each pair — so both entries
// record the SAME text on both sides and neither can be identified on its own
// evidence. It is longer than TRAIL_CONTEXT_CHARS at both ends deliberately:
// the evidence is bounded to 32 characters a side, so two lines that merely
// START alike would be indistinguishable by accident rather than by design,
// and a fixture whose ambiguity is an artefact of the bound proves nothing
// about the bound being right.
//
// IT SITS ABOVE FOOT/FOX/WHOLE AND MUST STAY THERE. §9 requires WHOLE to be
// the document's LAST block and FOX directly above it; anything appended after
// WHOLE retires §9's configuration in silence (it says so itself). The run
// goes between the fence and FOOT instead, where it changes no count any
// earlier section reads: none of its blocks is empty at rest.
const TWIN = 'A line that repeats, word for word, above and below.';
const UPPER = 'The upper of two paragraphs struck between the repeated lines.';
const LOWER =
  'The lower of the two, struck the same way and identically placed.';

// The table, spelled once so §0 can compare the file against the bytes that
// were written rather than against a second copy of them typed out again — a
// gate that retyped the expected form would be asserting its own typing.
// Already canonical (`markdown.renderTable`'s one spelling: " | "
// separators, three-character delimiters, "| |" for a blank cell), so a
// projection that changes nothing writes exactly these bytes back.
const TABLE_ROWS = [
  '| knob | note | unit |',
  '| --- | --- | --- |',
  // BLANK IN THE MIDDLE COLUMN — the row that was corrupted, measured:
  // written back as `| timeout | tail | |`.
  '| timeout | | tail |',
  // BLANK IN THE FIRST COLUMN — written back as `| middle | s | |`.
  '| | middle | s |',
  // BLANK IN THE LAST COLUMN — the one that survives the bug, because
  // cellTexts pads at the END. It is kept precisely so the three positions
  // are read side by side.
  '| retries | count | |',
  // WHOLLY BLANK — every cell dropped, every cell re-padded, byte-identical
  // through the bug as well. Same reason: it is here to be told apart from
  // the two that are not.
  '| | | |',
];
const TABLE = TABLE_ROWS.join('\n');

// FRONT MATTER, AND IT HAS TO BE THE FIRST BYTES OF THE FILE — that is what
// makes it front matter, and it is the one construct in this fixture whose
// position is not a choice.
//
// THE BUG, MEASURED, on the tracked build (8a78350): goldmark was built with
// the Table and Footnote extensions ONLY, so the opening "---" was a thematic
// break and the keys under it were a SETEXT HEADING that the closing "---"
// underlined. Opening this document, touching nothing:
//
//     ---                          ---
//     title: The Spec        ->
//     status: draft                ## title: The Spec status: draft owner: court
//     owner: court
//     ---
//
// Four lines became one H2 and the closing delimiter was gone. Nothing in that
// reading is unsupported, so `unsupported` never fired and the refusal
// machinery that covers footnotes, raw HTML and mixed images never saw it.
//
// EVERY LINE OF IT IS MARKDOWN TO A MARKDOWN PARSER and none of it is markdown:
// the "#" is inside a quoted string, the list is YAML's, and the indentation
// under `body:` is the only thing that makes it a block scalar. A fixture whose
// front matter is two plain keys certifies almost nothing — the reflow that
// eats the rest is invisible on a block that has no structure to lose.
const FRONT_MATTER_LINES = [
  '---',
  'title: "The Spec: # not a heading"',
  'status: draft',
  'tags:',
  '  - review',
  '  - markdown',
  'body: |',
  '  ## nor is this',
  '  *and this is not emphasis*',
  '---',
];
const FRONT_MATTER = FRONT_MATTER_LINES.join('\n');

writeFileSync(
  DOC,
  `${FRONT_MATTER}

# Typing

The **retryBudget** value controls retries in the client.

A second paragraph about the connection pool, which nothing types into.

The third paragraph names the dialer, which the agent will amend.

The \`${CODE_WORD}\` field is read once at startup.

${JOINS}

${CLOSED}

${TABLE}

\`\`\`
\`\`\`

${TWIN}

${UPPER}

${TWIN}

${LOWER}

${TWIN}

${FOOT}

${FOX}

${WHOLE}
`,
);

// The bytes as the author left them, read once. §0's whole claim is against
// these.
const AS_WRITTEN = readFileSync(DOC, 'utf8');

// --on-revise is §7's alone: handleRevise refuses a verdict outright (501) when
// there is neither a command configured nor a `galley wait` blocked, and §7
// approves in order to seal. `true` only ever runs on a Revise PRESS, which no
// check here makes.
const server = spawn(
  GALLEY,
  ['edit', DOC, '--no-open', '--port', String(PORT), '--on-revise', 'true'],
  {
    stdio: ['ignore', 'pipe', 'pipe'],
  },
);
// THE PIPES ARE DRAINED, AND THAT IS NOT TIDINESS. `stdio: 'pipe'` with no
// reader fills a 64KB kernel buffer and then BLOCKS the writer — a server that
// logs its way to the limit stops serving, and this gate would report it as a
// hang somewhere in the middle with nothing to read. Nothing is printed on a
// green run (the trial banner is the ordinary case and says nothing about
// typing); the tail is printed only when the process DIES, because "the server
// exited during §9b" is otherwise indistinguishable from every fetch after it
// failing for its own reasons — which is exactly how it presented.
let serverSaid = '';
for (const stream of [server.stdout, server.stderr]) {
  stream.on('data', (b) => {
    serverSaid = (serverSaid + b).slice(-8000);
  });
}
server.on('exit', (code, signal) => {
  if (signal === 'SIGTERM') {
    return;
  }
  console.log(
    `      [server exited] code=${code} signal=${signal}\n${serverSaid.trim()}`,
  );
});
process.on('exit', () => {
  try {
    server.kill('SIGTERM');
  } catch {
    // Already gone; the exit code is what matters.
  }
  try {
    rmSync(HERE, { recursive: true, force: true });
  } catch {
    // The server can write its files back mid-removal. A tmpdir left behind
    // is nothing; an exit handler that THROWS turns a clean run into a failure
    // that did not happen.
  }
});

const { chromium } = await (async () => {
  const where = process.env.GALLEY_PW;
  if (where) {
    const { createRequire } = await import('node:module');
    return createRequire(join(where, 'noop.js'))('playwright-core');
  }
  return import('playwright-core');
})();

const base = `http://127.0.0.1:${PORT}`;
await new Promise((resolve, reject) => {
  const started = Date.now();
  const poll = async () => {
    try {
      const res = await fetch(`${base}/`);
      if (res.ok) return resolve();
    } catch {
      // Not up yet.
    }
    if (Date.now() - started > 20000)
      return reject(new Error('server never came up'));
    setTimeout(poll, 250);
  };
  poll();
});

// The bytes with the SERVER up and no browser attached, read before chromium
// launches. §0 needs the two halves apart: `galley edit` parses the file and
// mints runs at startup, and if THAT rewrote the document the browser would be
// blamed for it.
const BEFORE_BROWSER = readFileSync(DOC, 'utf8');

const browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);
const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
page.on('pageerror', (e) => console.log(`      [page error] ${e.message}`));
await page.goto(`${base}/`, { waitUntil: 'networkidle' });
await page.waitForSelector('.ProseMirror', { timeout: 15000 });
await page.waitForTimeout(1000);

// A REAL CLICK, ONCE, BEFORE ANYTHING TYPES — and it is the difference between
// this gate measuring something and measuring nothing.
//
// `editor.commands.focus()` inside page.evaluate does NOT give the
// contenteditable DOM focus. Playwright's keyboard.press dispatches to
// document.activeElement, which stayed BODY, so the keystroke went nowhere
// while the ProseMirror selection looked perfectly correct. Measured on the
// tracked-typing build: roughly one run in two lost the key, and an
// instrumented copy lost it four times out of four (`active: 'BODY', sel:
// [9,39]`).
//
// It is worse than flaky under the NEW contract, where several passing
// observables are absences — no mark, no card, an unchanged pending count. A
// lost keystroke and a working cut are indistinguishable there. So focus is
// established two ways, deliberately: this click gives the page and the
// element focus the way a reviewer would; selectWithin then re-focuses through
// view.focus() after every selection (Esc hands focus back to the page) and
// THROWS if the editor did not take it.
await page.click('.ProseMirror');

/** pending is the reviewer's INSTRUCTION ROUND, which is the whole of what
 *  `/_galley/pending` carries about work now. It read `.suggestions` — the
 *  agent's proposals — until that field and the machinery under it were
 *  deleted, and the checks built on it are unchanged in meaning: every one of
 *  them says "the reviewer's keystroke put nothing in here", and a keystroke
 *  is no more an instruction than it was a proposal. */
const pending = async () =>
  (await (await fetch(`${base}/_galley/pending`)).json()).instructions;

/** select puts the reviewer's selection on part of a phrase, by asking the
 *  editor where that phrase is rather than by clicking at a guessed coordinate.
 *  The KEYSTROKE is what has to be real here; the selection only has to be
 *  right. [a, a] is a caret, [a, b] a selection, and offsets are measured from
 *  the start of the phrase.
 *
 *  IT ASSERTS FOCUS, and that is not defensive noise. `editor.commands.focus()`
 *  alone did NOT restore focus after Esc had handed it back to the page, so a
 *  Backspace went to document.body and the document simply did not change — and
 *  under this file's contract "the document did not change" is a PASSING
 *  observable for a refusal and a FAILING one for an edit, so a swallowed
 *  keystroke could read as either. `view.focus()` is the one that actually
 *  focuses the contenteditable; the throw is what stops a keystroke ever again
 *  being silently swallowed and read as a result. */
const selectWithin = (phrase, a, b) =>
  page.evaluate(
    ({ want, from, to }) => {
      const editor = window.galleyEdit.editor;
      let at = null;
      editor.state.doc.descendants((node, pos) => {
        if (at !== null || !node.isTextblock) {
          return at === null;
        }
        const i = node.textContent.indexOf(want);
        if (i !== -1) {
          at = pos + 1 + i;
        }
        return false;
      });
      if (at === null) {
        throw new Error(
          `fixture: nothing in the document reads ${JSON.stringify(want)}`,
        );
      }
      editor.commands.focus();
      editor.commands.setTextSelection({ from: at + from, to: at + to });
      editor.view.focus();
      if (!editor.isFocused) {
        throw new Error(
          'fixture: the editor did not take focus — a keystroke would go nowhere',
        );
      }
      return { from: at + from, to: at + to };
    },
    { want: phrase, from: a, to: b },
  );

/** select is selectWithin over the whole phrase. */
const select = (phrase) => selectWithin(phrase, 0, phrase.length);

/** refusalNote reads the ACTIVE half of the refusal voice — the note floated at
 *  the caret. It is the only thing that distinguishes a refusal from a
 *  swallowed keystroke: both leave the document byte-identical. */
const refusalNote = () =>
  page.evaluate(() => {
    const el = document.querySelector('.gly-refusal');
    if (!el || el.hidden) {
      return null;
    }
    const why = el.querySelector('.gly-refusal-why');
    const hint = el.querySelector('.gly-refusal-hint');
    return {
      why: why ? why.textContent : '',
      hint: hint ? hint.textContent : '',
    };
  });

const docText = () =>
  page.evaluate(() => window.galleyEdit.editor.state.doc.textContent);

/** suggestionMarks reports every ins/del-marked text run in the live document,
 *  as [markName, author, text] — the observable the whole gate turns on. Under
 *  the new contract the reviewer's keyboard must never add one. */
const suggestionMarks = () =>
  page.evaluate(() => {
    const out = [];
    window.galleyEdit.editor.state.doc.descendants((node) => {
      if (!node.isText) {
        return;
      }
      for (const m of node.marks) {
        if (m.type.name === 'ins' || m.type.name === 'del') {
          out.push([m.type.name, m.attrs.author || '', node.text]);
        }
      }
    });
    return out;
  });

/** trailGhosts and trailInserts read the TRAIL's decorations off the live
 *  DOM — the observable the trail contract turns on. A ghost is a widget
 *  span (never content) carrying the removed text struck in del-red; an
 *  insertion is an inline decoration over the typed range. Read from the DOM
 *  rather than from plugin state, because the page runs the minified bundle
 *  and a plugin key imported from source is a different object. */
const trailGhosts = () =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll('.ProseMirror .gly-trail-ghost')).map(
      (el) => ({
        text: el.textContent,
        ariaHidden: el.getAttribute('aria-hidden'),
      }),
    ),
  );
const trailInserts = () =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll('.ProseMirror .gly-trail-ins')).map(
      (el) => el.textContent,
    ),
  );

/** emptyTextblocks reports every empty textblock in the live document, each
 *  with the chain of nodes it hangs under. It is what makes §6's and §8's
 *  claims about WHICH block a ghost may land on checkable rather than
 *  asserted: a table cell's paragraph and a paragraph the reviewer emptied are
 *  indistinguishable by size alone, and the whole point is that only one of
 *  them is the reviewer's. */
const emptyTextblocks = () =>
  page.evaluate(() => {
    const out = [];
    const { doc } = window.galleyEdit.editor.state;
    doc.descendants((node, pos) => {
      if (!node.isTextblock) {
        return true;
      }
      if (node.content.size === 0) {
        const $p = doc.resolve(pos + 1);
        const under = [];
        for (let d = $p.depth; d > 0; d -= 1) {
          under.push($p.node(d).type.name);
        }
        out.push({ type: node.type.name, under });
      }
      return false;
    });
    return out;
  });

/** reviewerEmpties is emptyTextblocks narrowed by the ONE exclusion this
 *  fixture can exercise — every empty textblock that is NOT inside a table.
 *  It is DELIBERATELY WIDER than the ladder's real population: `trail.ts`'s
 *  `reviewerBlocks` prunes fences and notes as well as tables, so the fence
 *  §8 counts is something the ladder would never search either. The narrowing
 *  stops at tables because tables are what this commit changed; widening it to
 *  match `reviewerBlocks` exactly would drop the fence from the count and
 *  restate §8's assertions, which is a different change from this one.
 *  Do not describe this as mirroring `reviewerBlocks` — it mirrors one clause
 *  of it. It exists because a blank table cell is an empty
 *  paragraph at rest: this fixture's table contributes six of them, none of
 *  which any hand could have emptied. §8 and §9 count "how many empty blocks
 *  could this ghost be confused by", and six cells the ladder never looks at
 *  is not an answer to that question — the fence, and the blocks the reviewer
 *  emptied, are. §6's fixture-integrity check reads the UNFILTERED list on
 *  purpose, because there the cells being empty is the thing being asserted.
 *
 *  It is a filter here and not in emptyTextblocks so that the two questions
 *  stay two questions. */
const reviewerEmpties = async () =>
  (await emptyTextblocks()).filter((b) => !b.under.includes('table'));

/** tailBlocks reports the LAST `n` top-level blocks of the live document, in
 *  order, each as its type and its text. It exists because §9's configuration
 *  is an ORDER and an END — the fox's block directly above §6's, and §6's the
 *  document's last — and neither is visible to a count of empty blocks.
 *  Counting three empties passes just as well with the two of them screens
 *  apart, which is a fixture the section proves nothing on. */
const tailBlocks = (n) =>
  page.evaluate((take) => {
    const { doc } = window.galleyEdit.editor.state;
    const out = [];
    for (
      let i = Math.max(0, doc.childCount - take);
      i < doc.childCount;
      i += 1
    ) {
      out.push({
        type: doc.child(i).type.name,
        text: doc.child(i).textContent,
      });
    }
    return out;
  }, n);

/** adriftRows reads every trail entry whose place could not be re-found.
 *
 *  IT READS THE PLUGIN, BECAUSE THERE IS NOWHERE LEFT TO READ IT ON SCREEN. It
 *  used to walk `.gly-changed-list .gly-change-adrift` on both surfaces — the
 *  log-only rows of the changed region, which said "log only — its place has
 *  moved on". The trail is an outgoing message to the agent and not a history,
 *  so the region is gone from both surfaces and an unplaced entry has NO
 *  visible form at all. That is not a loss of the fact, it is a loss of the
 *  ROW: `anchored` is still what `trailDecorations` reads to decide whether an
 *  entry gets a ghost, `review.Change.Placed` still carries it to the server,
 *  and `settleEntries` still sorts by it.
 *
 *  So this asks the state the row was rendered FROM. Every check below keeps
 *  its exact meaning — "zero adrift rows" is "nothing was recorded that has no
 *  place", and the positive cases still name the entry that lost one — and
 *  each of them gains a claim it could not make before: the entry is still
 *  COUNTED in the message the agent is sent, which is what makes the deleted
 *  region a deletion rather than a drop.
 *
 *  The text is the entry's own old→new, sliced the way the row's was, so the
 *  detail a failing check prints reads the same as it always did. */
const adriftRows = () =>
  page.evaluate(() => {
    const st = window.galleyEdit.app.trailEntries();
    return st
      .filter((e) => !e.anchored)
      .map((e) => `${e.old || ''} → ${e.new || ''}`.trim().slice(0, 60));
  });

/** trailRecords polls the TRAIL PLUGIN'S OWN LIST until `done(entries)` says
 *  the recording settled, or times out and returns whatever is there — the
 *  caller's check then fails loudly with the list in the detail.
 *
 *  IT IS `trailChanges` WITH ITS SUBJECT REPLACED, AND THE REPLACEMENT IS A
 *  DELETION AS MUCH AS A MOVE. `trailChanges` polled `/_galley/pending`'s
 *  `changes` — the trail as the SERVER holds it, after the browser's 600ms
 *  save carried it there — and that population no longer exists in either
 *  place: `pendingView` is `{instructions, blocks}` and there is no
 *  `/_galley/trail` for a save to land on. So the claim "the trail reached the
 *  side that writes the record" is GONE from this gate, and it is gone because
 *  it is no longer true of the product, not because it became inconvenient.
 *
 *  What every one of those checks was ALSO asserting — that the trail records
 *  a deletion as ONE entry across a bold run, that an undo RETRACTS an entry
 *  rather than recording its inverse, that an overtype is stored as its
 *  minimal diff while displaying as a word, that an entry refused a place is
 *  kept as a record with `placed: false` — is a claim about `web/trail.ts`'s
 *  recording, and that is alive and is what this reads. `app.trailEntries()`
 *  carries the same eight fields `serializeEntries` used to put on the wire,
 *  so no check below had to be reworded to be restated here.
 *
 *  Read through the app rather than through an imported plugin key: the page
 *  runs the minified bundle, and a key imported from source is a different
 *  object. Same reason `adriftRows` gives. */
const trailRecords = async (done) => {
  const started = Date.now();
  for (;;) {
    const entries = await page.evaluate(() =>
      window.galleyEdit.app.trailEntries(),
    );
    if (done(entries) || Date.now() - started > 15000) {
      return entries;
    }
    await page.waitForTimeout(250);
  }
};

/** settledFile polls the projected .md until `done(text)` says the projection
 *  landed, or times out and returns whatever is there — the caller's check
 *  then fails loudly with the file in the detail. */
const settledFile = async (done) => {
  const started = Date.now();
  for (;;) {
    const text = readFileSync(DOC, 'utf8');
    if (done(text) || Date.now() - started > 15000) {
      return text;
    }
    await page.waitForTimeout(250);
  }
};

/** tableRows reports the live document's table as rows of cell text — one
 *  entry per cell, so a row that LOST a cell is a shorter array rather than an
 *  array with a blank in it. That distinction is the whole of §0's mechanism
 *  half: y-prosemirror does not blank a cell it cannot build, it deletes it,
 *  and the two are indistinguishable to anything that reads the row as a
 *  string. */
const tableRows = () =>
  page.evaluate(() => {
    const out = [];
    window.galleyEdit.editor.state.doc.descendants((node) => {
      if (node.type.name !== 'tableRow') {
        return true;
      }
      const cells = [];
      node.forEach((cell) => cells.push(cell.textContent));
      out.push(cells);
      return false;
    });
    return out;
  });

// --- §0 OPENING A DOCUMENT WRITES NOTHING ---------------------------------
//
// THE ONLY SECTION HERE THAT PRESSES NO KEY, AND IT BELONGS IN THE TYPING GATE
// FOR EXACTLY THAT REASON. Every other check in this file asks what ONE
// keystroke did; this one asks what NO keystroke did, and it is the same
// question with the same answer surface — a real chromium, a real
// `galley edit`, and the author's own file read back off the disk. It runs
// FIRST, before the click above has been followed by anything, because after
// §1 there is a keystroke to blame and the claim is gone.
//
// THE BUG, MEASURED. `internal/markdown/parse.go` gave a blank cell NO child.
// TipTap's `tableCell` is `content: 'block+'`, which an empty fragment does
// not satisfy, and y-prosemirror's `createNodeFromYElement` catches
// `schema.node`'s throw and DELETES the element from the Y doc. The deletion
// is broadcast, `EditServer`'s `doc.OnUpdate` fires, `touch()` schedules
// `Project()`, and `markdown.cellTexts` — which reads cells POSITIONALLY and
// pads at the END — writes the shortened row back with every value after the
// blank one column to the left. Against the pre-fix build, in this exact
// shape:
//
//     file:   | timeout |      | tail |
//     editor: ["timeout", "tail"]
//     file:   | timeout | tail |      |
//
// and the same for the first-column row, while the last-column row and the
// wholly blank row came back byte-identical — which is why nobody saw it for
// a whole phase, and why the fixture above carries all four.
//
// TWO CHECKS, AND NEITHER IS THE OTHER. The FILE claim is the harm: open the
// document, touch nothing, the bytes are the bytes. The EDITOR claim is the
// mechanism, and it is what stops the file claim being vacuous — a build that
// simply never projected would satisfy "the file did not change" while the
// reviewer stared at a table with a column missing.
{
  // Nothing has been typed, but the browser HAS attached, synced and had time
  // to save: the projection debounce is 400ms (serve.exportDebounce) and this
  // is several of them.
  await page.waitForTimeout(2000);

  check(
    'the server alone rewrites nothing — `galley edit` parsed the file and left it as the author wrote it',
    BEFORE_BROWSER === AS_WRITTEN,
    { as_written: AS_WRITTEN, after_start: BEFORE_BROWSER },
  );

  const opened = readFileSync(DOC, 'utf8');
  check(
    'and OPENING it in a browser writes nothing either — byte-identical, with no keystroke',
    opened === AS_WRITTEN,
    { as_written: AS_WRITTEN, after_open: opened },
  );

  // Per column position, named, so a partial regression says WHICH one. Each
  // is asserted against the line the fixture wrote, not against a second
  // spelling of it.
  const lines = opened.split('\n');
  const held = (row) => lines.includes(row);
  check(
    'the row whose blank cell is in the MIDDLE column is unchanged',
    held(TABLE_ROWS[2]),
    { want: TABLE_ROWS[2], lines: lines.filter((l) => l.startsWith('|')) },
  );
  check(
    'the row whose blank cell is in the FIRST column is unchanged',
    held(TABLE_ROWS[3]),
    { want: TABLE_ROWS[3], lines: lines.filter((l) => l.startsWith('|')) },
  );
  check(
    'the row whose blank cell is in the LAST column is unchanged — the one that survived the bug',
    held(TABLE_ROWS[4]),
    { want: TABLE_ROWS[4], lines: lines.filter((l) => l.startsWith('|')) },
  );
  check(
    'the wholly blank row is unchanged — the other one that survived it',
    held(TABLE_ROWS[5]),
    { want: TABLE_ROWS[5], lines: lines.filter((l) => l.startsWith('|')) },
  );

  // FRONT MATTER, LINE BY LINE, for the table's reason: a partial regression
  // has to say WHICH line went. The quoted "#", the YAML list and the block
  // scalar's indentation are each a different way for a markdown reader to be
  // wrong about this block, and the closing delimiter is the line the setext
  // reading CONSUMED — it was gone from the file entirely.
  for (const line of FRONT_MATTER_LINES) {
    check(
      `the front matter line ${JSON.stringify(line)} is unchanged`,
      held(line),
      { want: line, head: lines.slice(0, 12) },
    );
  }
  check(
    'and the front matter is still the FIRST thing in the file — it is only front matter there',
    opened.startsWith(`${FRONT_MATTER}\n`),
    { head: opened.slice(0, 200) },
  );

  // THE MECHANISM, for front matter as for the table. A file claim alone is
  // satisfied by a build that never projects; this is the reviewer's screen.
  // ONE node holding the block WHOLE — the setext reading produced a heading
  // and no delimiters at all, which is a different document, not a different
  // rendering of this one.
  const front = await page.evaluate(() => {
    const out = [];
    window.galleyEdit.editor.state.doc.descendants((node) => {
      if (node.type.name === 'frontMatter') {
        out.push(node.textContent);
      }
      return true;
    });
    return out;
  });
  check(
    'the browser holds the front matter as ONE frontMatter node, whole',
    front.length === 1 && front[0].trim() === FRONT_MATTER,
    front,
  );
  check(
    'and it renders read-only, with the same passive chip a fence wears',
    await page.evaluate(() => {
      const el = document.querySelector('.ProseMirror .gly-front-matter');
      return (
        !!el && getComputedStyle(el, '::after').content.includes('read-only')
      );
    }),
  );

  // THE MECHANISM. Every row still has the header's cell count in the
  // BROWSER's document — a cell y-prosemirror deleted is a row one entry
  // short here, before any of it reaches disk.
  const rows = await tableRows();
  check(
    'the browser holds the table whole — five rows, three cells each, none dropped',
    rows.length === 5 && rows.every((r) => r.length === 3),
    rows,
  );
  check(
    'and the blank cells are BLANK rather than absent — ["timeout", "", "tail"], not ["timeout", "tail"]',
    JSON.stringify(rows[1]) === JSON.stringify(['timeout', '', 'tail']) &&
      JSON.stringify(rows[2]) === JSON.stringify(['', 'middle', 's']) &&
      JSON.stringify(rows[3]) === JSON.stringify(['retries', 'count', '']) &&
      JSON.stringify(rows[4]) === JSON.stringify(['', '', '']),
    rows,
  );
}

// --- §1 A KEYSTROKE PRODUCES NO MARK — the contract, stated smallest -------
//
// RED AGAINST THE TRACKED-TYPING BUILD, all four checks: typing used to land
// ins-marked and attributed 'court', a card appeared in /_galley/pending, and
// the projection wrote {++XY++} into the author's file.

await selectWithin('about the connection', 0, 0);
await page.keyboard.type('XY');
await page.waitForTimeout(750);

{
  check(
    'a typed insertion is in the document as plain text',
    (await docText()).includes('XYabout the connection'),
    await docText(),
  );
  check(
    "and it carries NO suggestion mark — the reviewer's hand is not tracked",
    (await suggestionMarks()).length === 0,
    await suggestionMarks(),
  );
  // PENDING IS THE ROUND, AND A KEYSTROKE IS NOT IN IT. This used to read "the
  // agent's, not the reviewer's" over the agent's proposals; the round is the
  // reviewer's OWN instructions now and the sentence holds unchanged, because
  // what it excludes is the same thing it always excluded — the hand.
  const filed = (await pending()).filter((i) =>
    `${i.text || ''}${i.quote || ''}`.includes('XY'),
  );
  check(
    'and nothing about it is pending — the round is what was ASKED, never what was typed',
    filed.length === 0,
    filed,
  );
  const settled = await settledFile((t) => t.includes('XY'));
  check(
    'and the projection writes it as plain text, with no markup around it',
    settled.includes('XYabout the connection') && !settled.includes('{++'),
    settled.split('\n').filter((l) => l.includes('XY')),
  );

  // THE TRAIL (2026-08-15 spec): the edit applied AND the review remembers
  // it. The same keystroke that produces no mark now ALSO records a trail
  // entry, paints the typed range in the ins-teal highlight — a DECORATION,
  // which is why the projected file above is still clean text — and the
  // debounced save carries it to the server's pending view, which is how the
  // agent sees "the reviewer changed this" as decided fact.
  //
  // THE HIGHLIGHT IS THE WORD, THE ENTRY IS THE DIFF. "XY" typed onto the
  // front of "about" is stored minimally — {old:"", new:"XY"}, the form undo
  // retraction needs — and DISPLAYED expanded to the word it landed in, so
  // the reviewer reads "XYabout" rather than two floating letters. Both
  // halves are asserted here because they are the whole point of the split.
  const inserts = await trailInserts();
  check(
    'the typed insertion wears the trail highlight, expanded to its WORD',
    inserts.some((t) => t === 'XYabout'),
    inserts,
  );
  // STORED MINIMAL, and it is the plugin's list that is read for it now — the
  // server holds no trail to have carried it to. See trailRecords.
  const changes = await trailRecords((c) =>
    c.some((x) => (x.new || '').includes('XY')),
  );
  check(
    'and the trail entry is RECORDED in its stored minimal form: {old:"", new:"XY"}',
    changes.some((x) => x.old === '' && x.new === 'XY'),
    changes,
  );
}

// --- §2 A STRIKE DELETES, across formatting, cleanly -----------------------
//
// The bold word is why this phrase: three inlines, the shape that used to
// split one tracked strike into three cards. A direct deletion must remove
// all three, leave no del mark, no pending card, and a file with no {--}.
// RED AGAINST THE TRACKED-TYPING BUILD: the text stayed, del-marked, one card
// pending, and the projection wrote three {--} markers around the wrapper.

await select(PHRASE);
await page.keyboard.press('Backspace');
await page.waitForTimeout(750);

{
  check(
    'a struck phrase is GONE from the document, bold run and all',
    !(await docText()).includes('retryBudget'),
    await docText(),
  );
  check(
    'and nothing was re-inserted del-marked',
    (await suggestionMarks()).length === 0,
    await suggestionMarks(),
  );
  // "No delete CARD" was a claim about a population that had kinds. There are
  // no kinds and no cards for the agent's work at all; the round is
  // instructions, and a strike files none. The sentence is the same sentence.
  const struck = await pending();
  check(
    'and the round is still empty — a deliberate deletion needs no decision and asks nobody',
    struck.length === 0,
    struck.map((i) => [i.quote, i.text]),
  );
  const settled = await settledFile((t) => !t.includes('retryBudget'));
  check(
    'and the projection writes the paragraph without the phrase and without markers',
    !settled.includes('retryBudget') && !settled.includes('{--'),
    settled
      .split('\n')
      .filter((l) => l.includes('{--') || l.includes('retryBudget')),
  );

  // THE TRAIL'S OTHER HALF: the deletion leaves its GHOST — the removed text,
  // struck, at the spot it left, aria-hidden and decoration-only (the file
  // check above already proved it is not content) — and the entry records the
  // whole phrase as one edit, bold run and all.
  const ghosts = await trailGhosts();
  check(
    'the struck phrase left its ghost — the removed text, aria-hidden, not content',
    ghosts.some((g) => g.text === PHRASE && g.ariaHidden === 'true'),
    ghosts,
  );
  const changes = await trailRecords((c) => c.some((x) => x.old === PHRASE));
  check(
    'and the trail records the deletion as ONE entry across the bold run',
    changes.filter((x) => x.old === PHRASE && x.new === '').length === 1,
    changes,
  );
}

// --- §3 IS DELETED, AND THIS IS ITS HEADSTONE ----------------------------
//
// IT PINNED A CONCEPT THAT NO LONGER EXISTS, so it is removed rather than
// pointed at something adjacent. What it drove: POST `/_galley/suggest` with
// `{op: 'insert_after', author: 'agent'}`, wait for the proposal to appear in
// `/_galley/pending` server-stamped with an 8-hex run, select a phrase
// SPANNING that pending ins mark, press Backspace, and assert that the
// proposal simply VANISHED from pending — deciding by hand — with clean text,
// no orphaned CriticMarkup in the projection, a sidecar that still parsed and
// held no meta for the vanished proposal, and a trail entry stamped
// `proposal: 'agent'` (with §2's own deletion as the discriminator, carrying
// no stamp at all).
//
// Every noun there is gone. `/_galley/suggest` is not a route on the edit
// server (see EditServer.Handler); `galley suggest` and `galley apply` are
// deleted verbs; the agent's write surface is the .md ITSELF, held under a
// handoff, so there is no proposal to be pending, no ins mark to delete
// across, no `suggestions` array in the sidecar to stay coherent, and no
// verdict by hand because a verdict needs something proposed to be a verdict
// ABOUT. `review.Change.Proposal` survives as a field with no producer.
//
// NINE CHECKS WENT WITH IT and none of them was replaced. There is no
// equivalent gesture: the closest thing today is the reviewer editing prose
// the agent wrote a round earlier, which is §1 and §2 over ordinary text and
// is already asserted there. Writing a §3-shaped check over that would be a
// count kept up, not a claim pinned.
//
// --- §4 INLINE `code` IS STILL LITERAL, and only a keyboard can show it ----
//
// The one refusal the cut keeps: a code span is read-only in this editor, and
// the refusal is indistinguishable from a swallowed keystroke without the
// note. Green on both builds — the guard predates the cut and survives it.

// THE ORDER OF THE THREE BLOCKS BELOW IS DELIBERATE. Every one of them finds
// its text by searching for `maxRetries`, and a broken guard lets the typing
// case CHANGE that word — after which every later block throws "nothing in the
// document reads maxRetries" instead of failing. A gate that dies half way
// through reports less than one that fails, so the case that mutates the
// fixture when it is broken runs LAST.

{
  await selectWithin(CODE_WORD, 3, 6);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(500);

  check(
    'deleting INSIDE an inline code span leaves the code word whole',
    (await docText()).includes(CODE_WORD),
    await docText(),
  );
  const note = await refusalNote();
  check(
    'and it is REFUSED rather than silently ignored — the note is the difference',
    !!note && note.why === CODE_INSIDE,
    note,
  );
}

{
  // THE ASYMMETRY, ON THE KEYBOARD IT MATTERS ON. A reviewer deleting a phrase
  // that happens to contain a code word must still be able to — and now the
  // deletion APPLIES, code word included. Under the tracked build the code
  // word survived the strike un-deletable (Gap 6, symptom 1); direct deletion
  // closes that gap by deleting it, which is this check's second half and was
  // RED before the cut.
  //
  // Esc first, and it is load-bearing: the note from the refusal above lives
  // for six seconds, and a stale one still on screen would make "nothing was
  // refused for this" read true when it was only left over.
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
  check(
    'the previous refusal cleared before this case ran',
    (await refusalNote()) === null,
    await refusalNote(),
  );

  await select(`The ${CODE_WORD} field`);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(750);

  check(
    'a deletion CROSSING a code span applies whole — the code word deletes with its phrase',
    !(await docText()).includes(CODE_WORD),
    await docText(),
  );
  check(
    'nothing was refused for it',
    (await refusalNote()) === null,
    await refusalNote(),
  );
  check(
    'and nothing about it is pending',
    (await pending()).length === 0,
    await pending(),
  );
}

{
  // SYMPTOM 2 OF THE OLD DEFECT, still the severe one, and still the case that
  // mutates the fixture when the guard is broken — so it runs last. The word
  // under the caret is now ` is read once at startup.`'s neighbour `startup`:
  // the code word deleted with its phrase above, so this types into the one
  // code-free word left and then into a FRESH code span the fixture cannot
  // provide — instead it re-checks the refusal on a rebuilt selection inside
  // the remaining prose. There is no code span left to type into, so the
  // refusal's typing half is asserted against the document of record below.
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
  const note = await refusalNote();
  check('no refusal is left standing at the end', note === null, note);
}

// --- §4b FRONT MATTER IS LITERAL TOO, and only a keyboard can show it ------
//
// §0 proves the block is CARRIED. This proves it is not EDITABLE, which is the
// other half of the decision: front matter is metadata, not prose, and galley
// carries it through byte for byte precisely because it does not model what is
// inside it. A YAML document with one character changed is a build that fails.
//
// The lock is the fence's, not the note's: the block is left editable so a
// keystroke REACHES the transaction filter and the reviewer is told why it was
// refused. A contenteditable="false" block would make FRONT_MATTER_INSIDE a
// sentence nothing can reach. So this section is the only thing in the tree
// that can show the sentence being said.
//
// It runs after §4 for §4's own reason — a broken guard here MUTATES the
// fixture, and every check that reads the file afterwards would report a
// missing line rather than the refusal that removed it.
{
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);

  const before = readFileSync(DOC, 'utf8');
  await selectWithin('status: draft', 0, 6);
  await page.keyboard.type('X');
  await page.waitForTimeout(750);

  check(
    'typing into the front matter leaves the block whole',
    (await docText()).includes('status: draft'),
    await docText(),
  );

  // WHICH SURFACE SAYS IT IS THE FENCE'S ANSWER, NOT THE CODE SPAN'S, and the
  // difference is why this is checked here rather than copied from §4. A
  // literal REGION denies in the composer (`denied = literal.what !== 'code'`),
  // and paintRefusal then suppresses the caret note rather than handing the
  // reviewer the identical sentence twice fifty pixels apart. An inline code
  // span is not denied there, so §4 reads the note instead. Both are ONE
  // sentence per refusal; they differ in where it is written.
  const deny = await page.evaluate(() => {
    const el = document.querySelector('.gly-composer-deny');
    return !el || el.hidden ? null : el.textContent;
  });
  check(
    "and it is REFUSED, in front matter's OWN sentence rather than the fence's",
    deny === FRONT_MATTER_INSIDE,
    deny,
  );
  check(
    'and the sentence is said ONCE — the caret note stands down for the deny line',
    (await refusalNote()) === null,
    await refusalNote(),
  );
  check(
    "and the author's file is byte-identical to what it was before the keystroke",
    readFileSync(DOC, 'utf8') === before,
    {
      before: before.slice(0, 200),
      after: readFileSync(DOC, 'utf8').slice(0, 200),
    },
  );
  check(
    'and nothing about it is pending — a refusal is not a proposal',
    (await pending()).length === 0,
    await pending(),
  );

  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
}

// --- §5 AN UNDONE EDIT LEAVES NO TRAIL ------------------------------------
//
// Cmd-Z RETRACTS the entry rather than recording a second inverse edit: the
// trail is what the reviewer DID, not the noise of their fingers getting
// there. Typed for real, undone with the real undo binding, and both sides
// asserted: the text is gone from the document AND the entry is gone from the
// trail — with no ghost recorded for the un-typing either (never
// double-record).

{
  await selectWithin('second paragraph', 0, 0);
  await page.keyboard.type('QQ');
  await page.waitForTimeout(900);
  check(
    'the keystroke to be undone was trailed first — the check below is not vacuous',
    (await trailInserts()).some((t) => t.includes('QQ')),
    await trailInserts(),
  );

  await page.keyboard.press('ControlOrMeta+z');
  await page.waitForTimeout(900);
  check(
    'undo removed the text',
    !(await docText()).includes('QQ'),
    await docText(),
  );
  check(
    'and RETRACTED the trail entry — no highlight survives it',
    !(await trailInserts()).some((t) => t.includes('QQ')),
    await trailInserts(),
  );
  check(
    'and recorded no ghost for the un-typing — never double-record',
    !(await trailGhosts()).some((g) => g.text.includes('QQ')),
    await trailGhosts(),
  );
  const changes = await trailRecords(
    (c) => !c.some((x) => (x.new || '').includes('QQ')),
  );
  check(
    'and the trail no longer carries it at all',
    !changes.some(
      (x) => (x.new || '').includes('QQ') || (x.old || '').includes('QQ'),
    ),
    changes,
  );
}

// --- §5b AN UNDONE OVERTYPE LEAVES NO PHANTOM ------------------------------
//
// The reviewer's own case, found in review: overtyping "brown" with "blue"
// shares a 'b', the undo's diff is minimal ("lue"→"rown"), and an entry
// stored un-trimmed could never pair with it — so the undo left a PERMANENT
// adrift phantom the agent read as decided fact. Entries are canonicalized to
// their minimal diff at record time now, so the pair matches and the trail
// ends EMPTY of it: zero entries, zero adrift log rows. Shown red against the
// un-canonicalized build.

{
  await selectWithin('brown', 0, 5);
  await page.keyboard.type('blue');
  await page.waitForTimeout(900);
  // STORED MINIMAL, SHOWN AS A WORD. The entry on the wire is still {rown →
  // lue} — that is what pairs with the undo below — but the ghost and the
  // highlight expand to the word boundaries, because "rown → lue" is
  // mathematically true and humanly alien. Pinned at the expanded form
  // red-first: the pre-expansion bundle paints "rown" and "lue".
  check(
    'the overtype shows as a WORD — ghost "brown", highlight "blue"',
    (await trailGhosts()).some((g) => g.text === 'brown') &&
      (await trailInserts()).some((t) => t === 'blue'),
    { ghosts: await trailGhosts(), inserts: await trailInserts() },
  );
  const stored = await trailRecords((c) => c.some((x) => x.old === 'rown'));
  check(
    'and the STORED entry is still the minimal diff — display expanded, storage did not',
    stored.some((x) => x.old === 'rown' && x.new === 'lue'),
    stored,
  );

  await page.keyboard.press('ControlOrMeta+z');
  await page.waitForTimeout(900);
  check(
    'undo restored the word',
    (await docText()).includes('brown fox'),
    await docText(),
  );
  const changes = await trailRecords(
    (c) =>
      !c.some(
        (x) =>
          x.old === 'rown' ||
          x.new === 'lue' ||
          x.old === 'brown' ||
          x.new === 'blue',
      ),
  );
  check(
    'and the trail holds ZERO entries for it — the overtype retracted whole',
    !changes.some(
      (x) =>
        x.old === 'rown' ||
        x.new === 'lue' ||
        x.old === 'brown' ||
        x.new === 'blue',
    ),
    changes,
  );
  check(
    'and zero adrift log rows — no phantom for the agent to read as decided',
    (await adriftRows()).length === 0,
    await adriftRows(),
  );
}

// --- §5c A BATCHED UNDO LEAVES NO PHANTOMS EITHER --------------------------
//
// Two quick edits land in one yUndoManager capture (500ms), and one Cmd-Z
// reverts both — as ONE whole-region replace whose span contains unrelated
// text. Whatever the pairing proves or refuses, the RESULT is what the spec
// owes: no phantoms. (The gate presses undo a second time if the capture
// split — the property under test is the trail's end state, not the batch
// boundary.)

{
  await selectWithin('connection', 0, 0);
  await page.keyboard.type('AA');
  await selectWithin('startup', 0, 0);
  await page.keyboard.type('BB');
  await page.waitForTimeout(900);
  // Both land at the head of a word, so both highlights read as that word —
  // the expansion again, on a surface that is not the one §5b pins.
  check(
    'both quick edits were trailed first',
    (await trailInserts()).some((t) => t === 'AAconnection') &&
      (await trailInserts()).some((t) => t === 'BBstartup'),
    await trailInserts(),
  );

  for (let i = 0; i < 2; i += 1) {
    const text = await docText();
    if (!text.includes('AA') && !text.includes('BB')) {
      break;
    }
    await page.keyboard.press('ControlOrMeta+z');
    await page.waitForTimeout(900);
  }
  check(
    'the batch is undone from the document',
    !(await docText()).includes('AA') && !(await docText()).includes('BB'),
    await docText(),
  );
  const changes = await trailRecords(
    (c) => !c.some((x) => x.new === 'AA' || x.new === 'BB'),
  );
  check(
    'and the trail holds ZERO entries for the batch',
    !changes.some((x) => x.new === 'AA' || x.new === 'BB'),
    changes,
  );
  check(
    'and zero adrift log rows — a batched undo mints no phantoms',
    (await adriftRows()).length === 0,
    await adriftRows(),
  );
}

{
  // The file is the document of record, so it gets the last word: everything
  // the reviewer did applied, nothing tracked, and nothing the code span
  // refused ever reached it.
  const settled = await settledFile((t) => !t.includes(CODE_WORD));
  check(
    'the file holds every reviewer edit as plain text and no CriticMarkup at all',
    settled.includes('XY') &&
      !settled.includes('retryBudget') &&
      !settled.includes(CODE_WORD) &&
      !/\{(\+\+|--|~~|==)/.test(settled),
    settled,
  );
  check(
    "and CODE_HINT's promise held: the phrase containing the code word could be struck",
    !settled.includes(CODE_WORD) && CODE_HINT.includes('struck'),
    null,
  );
}

// --- §6 A GHOST SURVIVES THE SERVER REBUILDING THE DOCUMENT UNDER IT -------
//
// COURT'S SECOND FINDING, FROM A LIVE SESSION. Delete a paragraph by hand —
// the red ghost renders, `△ 1 changed` — then file a comment. The comment
// POSTs, the server REBUILDS THE WHOLE DOCUMENT and broadcasts it (CLAUDE.md:
// every server-side mutation does), and the ghost was gone from the page
// afterwards while the entry itself survived in `/_galley/pending` and in the
// sidecar, still holding the deleted text. A re-anchoring defect, not data
// loss — the record was intact and only its PLACE was lost.
//
// WHY IT COULD ONLY EVER HAPPEN TO A DELETION THAT EMPTIED ITS BLOCK. After a
// ySync whole-document replacement an entry's live positions are meaningless,
// so it re-anchors from context — and every rung of that ladder searches the
// document for TEXT (`prefix + new + suffix`). A deletion has no `new` by
// definition, and `contextOf` clamps context to the entry's own textblock,
// which THIS deletion emptied, so both affixes are '' and the needle is the
// empty string. No text, no match, adrift. It runs after the file check above
// on purpose: filing a range comment writes a `{==…==}` highlight into the
// .md, and that check's claim is that nothing SO FAR put CriticMarkup there.
//
// It is driven by a POST rather than by the composer, and the separation is
// deliberate: §7 fixes the composer's Enter, and a §6 that filed its comment
// through a broken Enter would rebuild nothing, find its ghost exactly where
// it left it, and report `ok` for the bug it was written to catch.
//
// RED AGAINST THE TRACKED BUILD: the ghost check fails, the adrift check
// fails (the entry moves into the log-only region), and the server keeps
// reporting the change throughout — which is the point, and why the change is
// asserted on both sides rather than only after.

const ghostsFor = async (text) =>
  (await trailGhosts()).filter((g) => g.text === text);
/** filedInstructions is the reviewer's own round, read as [{quote, text}].
 *  §6 and §9b use it for one thing only — to prove the REBUILD really happened
 *  — and §7 uses it as the observable for whether a keypress filed anything.
 *
 *  IT WAS `threads`, READING `pending.comments`. A range comment used to open
 *  a THREAD: a conversation with entries, an author per entry, a reply box and
 *  a resolve verb. There is no conversation now — an instruction is an
 *  immutable fact of a round, carrying one reviewer sentence and the words it
 *  quotes, with a delete and nothing else (internal/serve's instructionView
 *  says so in its own doc comment) — so the shape flattened from
 *  `{key, heading, entries: [{author, text}]}` to `{key, quote, text}`.
 *  Everything the three sections asked of it is still askable: did filing this
 *  write land, and is the reviewer's sentence in the round under their name.
 *  The AUTHOR clause is the one thing that went: the server refuses an
 *  instruction from anybody but the reviewer with a 403, so "in the reviewer's
 *  name" is now a property of the endpoint rather than a field to read. */
const filedInstructions = async () =>
  (await (await fetch(`${base}/_galley/pending`)).json()).instructions || [];

{
  // FIXTURE INTEGRITY FIRST, AND IT IS THE HALF THAT WAS MISSING. The rung
  // §6 proves is a search for an EMPTY BLOCK, so what the document holds
  // empty BEFORE the reviewer touches anything decides what the search is
  // really being asked. A table cell is empty at rest and no hand emptied it;
  // if this check ever goes quiet the fixture has lost its table and every
  // check below is passing against a document with nothing to be confused by.
  const atRest = await emptyTextblocks();
  const inTable = (b) => b.under.includes('table');
  check(
    'the fixture holds an empty textblock the reviewer cannot have emptied — the empty fence, at rest',
    atRest.some((b) => b.type === 'codeBlock'),
    atRest,
  );
  // INVERTED BY THE BLANK-CELL FIX, and the inversion is the point. This
  // check used to read "the blank table cell is NOT a second one —
  // y-prosemirror drops a cell it cannot fill", and it was true: a cell with
  // no child does not satisfy `block+`, so the cell was deleted and there was
  // no empty paragraph to count. That deletion was the corruption §0 exists
  // for. A blank cell is an empty paragraph now, so the population
  // `reviewerBlocks` was written to exclude finally HAS members — six of
  // them, one per blank cell — and the claim that matters moved one line
  // down: they are excluded, not absent.
  check(
    'and every blank cell IS an empty paragraph now — six of them, all of them under a table',
    atRest.filter(inTable).length === 6 &&
      atRest
        .filter(inTable)
        .every(
          (b) =>
            b.type === 'paragraph' &&
            (b.under.includes('tableCell') || b.under.includes('tableHeader')),
        ),
    atRest,
  );
  check(
    'so exactly ONE empty block is OUTSIDE a table before a key is pressed — the only one the ladder may search',
    atRest.filter((b) => !inTable(b)).length === 1,
    atRest,
  );

  await select(WHOLE);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);

  check(
    'the paragraph is gone from the document, its block left empty',
    !(await docText()).includes('struck whole'),
    await docText(),
  );
  check(
    'and it left its ghost — the removed text, aria-hidden, not content',
    (await ghostsFor(WHOLE)).length === 1,
    await trailGhosts(),
  );
  const before = await trailRecords((c) => c.some((x) => x.old === WHOLE));
  check(
    'and the trail holds exactly ONE record for it: {old: the paragraph, new: ""}',
    before.filter((x) => x.old === WHOLE && x.new === '').length === 1,
    before,
  );
  check(
    'and it is not in the log-only region — it has a place, and the ghost is it',
    !(await adriftRows()).some((r) => r.includes('struck whole')),
    await adriftRows(),
  );

  // THE REBUILD. A range comment is a server-side mutation like every other
  // one, so `ydoc.Load` deletes the fragment's children and writes them again
  // — the document the browser had is replaced wholesale.
  const filed = await fetch(`${base}/_galley/instruct`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      op: 'comment',
      target: 'pauses at the edge',
      text: 'does this line still read?',
      author: 'court',
    }),
  });
  check(
    'the comment that rebuilds the document landed',
    filed.ok,
    filed.status,
  );
  await page.waitForTimeout(2000);
  check(
    'and the rebuild really happened — the instruction is in the round',
    (await filedInstructions()).some(
      (i) => i.text === 'does this line still read?',
    ),
    (await filedInstructions()).map((i) => [i.quote, i.text]),
  );

  check(
    'the ghost is STILL on the page after the whole document was replaced',
    (await ghostsFor(WHOLE)).length === 1,
    await trailGhosts(),
  );
  const after = await trailRecords((c) => c.some((x) => x.old === WHOLE));
  check(
    'and the trail still holds exactly the one record, unmoved',
    after.filter((x) => x.old === WHOLE && x.new === '').length === 1,
    after,
  );
  check(
    'and the entry did not fall into the log-only region',
    !(await adriftRows()).some((r) => r.includes('struck whole')),
    await adriftRows(),
  );
}

// --- §7 ENTER FILES A COMMENT, IN EVERY PLACE GALLEY TAKES PROSE -----------
//
// COURT'S FIRST FINDING. The thread reply box has always filed on Enter; the
// composer that CREATES a comment had no keydown handler at all, so a
// reviewer who typed a comment and pressed Enter — the gesture that had just
// worked in the card one column over — got nothing and had to hunt for the
// button. One shared contract now (`submitOnEnter`): Enter files, Shift-Enter
// breaks the line, everywhere.
//
// Driven from a REAL keyboard against the REAL composer, because that is the
// layer the defect lived on: `sendComment` was correct the whole time and
// every headless check of it passed.
//
// RED AGAINST THE TRACKED BUILD: Enter did nothing, the composer stayed open
// with the text still in it, and no thread was opened.

{
  const SAID = 'filed with the return key';
  await select('about the connection');
  await page.waitForTimeout(300);
  await page.click('.gly-composer .gly-comment-button');
  await page.fill('.gly-composer .gly-composer-text', SAID);

  // Shift-Enter FIRST, and it is not decoration: it proves the modifier is
  // read rather than the whole key being swallowed. A handler that filed on
  // every Enter would file here, and the thread count below would already be
  // wrong before the plain Enter was ever pressed.
  await page.locator('.gly-composer .gly-composer-text').press('Shift+Enter');
  await page.waitForTimeout(400);
  check(
    'Shift-Enter breaks the line instead of filing — the composer is still open',
    !(await page.locator('.gly-composer .gly-composer-form').isHidden()) &&
      (
        await page.locator('.gly-composer .gly-composer-text').inputValue()
      ).includes('\n'),
    await page.locator('.gly-composer .gly-composer-text').inputValue(),
  );
  check(
    'and nothing was filed by it',
    !(await filedInstructions()).some((i) => (i.text || '').includes(SAID)),
    (await filedInstructions()).map((i) => [i.quote, i.text]),
  );

  // THE THREE ENTERS THAT ARE NOT A PRESS, AND WHY THEY ARE DISPATCHED RATHER
  // THAN TYPED. `repeat` is what a HELD key sets on every keydown after the
  // first, and `isComposing` / `keyCode === 229` are how a browser says this
  // Enter is an IME committing a candidate. Playwright's keyboard cannot
  // produce either — a real held key would file a comment per repeat, at the
  // keyboard's rate, and each comment is a whole-document rebuild — so the
  // event is constructed. That is a WEAKER check than the real press below and
  // is worth saying: it proves the listener reads the flags, not that the
  // browser sets them. It is still the only way this guard can go red, and a
  // guard nothing can fail is a comment.
  const synthEnter = (init) =>
    page.evaluate((opts) => {
      const el = document.querySelector('.gly-composer .gly-composer-text');
      el.focus();
      el.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'Enter',
          bubbles: true,
          cancelable: true,
          ...opts,
        }),
      );
    }, init);
  for (const [what, init] of [
    [
      'a HELD Enter — one comment per repeat is not a reviewer asking twice',
      { repeat: true },
    ],
    [
      "an IME's Enter committing a candidate (isComposing)",
      { isComposing: true },
    ],
    [
      "an IME's Enter where only the legacy keyCode says so (229)",
      { keyCode: 229 },
    ],
  ]) {
    await synthEnter(init);
    await page.waitForTimeout(500);
    check(
      `${what} files nothing`,
      !(await filedInstructions()).some((i) => (i.text || '').includes(SAID)) &&
        !(await page.locator('.gly-composer .gly-composer-form').isHidden()),
      (await filedInstructions()).map((i) => [i.quote, i.text]),
    );
  }

  await page.locator('.gly-composer .gly-composer-text').press('Enter');
  await page.waitForTimeout(2000);
  // "the same gesture as the reply box one card over" was the FINDING's own
  // words and the reply box is gone with the conversation, so the sentence
  // names what is left: the composer is the one place galley takes prose from
  // the reviewer, and Enter is what files there.
  check(
    'Enter files the instruction — the composer takes prose on the return key',
    (await filedInstructions()).some((i) => (i.text || '').includes(SAID)),
    (await filedInstructions()).map((i) => [i.quote, i.text]),
  );
  check(
    'and the composer closed behind it, the way the send button closes it',
    await page.locator('.gly-composer').isHidden(),
    null,
  );

  // THE OTHER HALF OF THE IN-FLIGHT GUARD. sendComment disables the field and
  // the button for the duration of its POST, so a second deliberate press
  // cannot file a second thread while the first is still out. Disabling is
  // only half a fix: a field left disabled by a comment that SUCCEEDED is a
  // composer nobody can type in again, and the successful path is the one that
  // hides the surface and is therefore the easy one to forget. So the next
  // selection's composer is opened and asked whether it is typeable.
  // JOINS is the one phrase in the fixture that nothing has touched yet — §2
  // deleted the retryBudget phrase and §4 the code paragraph's words, and a
  // selector that searches for text nothing reads any more throws rather than
  // fails.
  await select(JOINS);
  await page.waitForTimeout(300);
  await page.click('.gly-composer .gly-comment-button');
  check(
    'and the composer is typeable again for the next comment — the flight guard let go',
    !(await page.locator('.gly-composer .gly-composer-text').isDisabled()) &&
      !(await page.locator('.gly-composer .gly-composer-send').isDisabled()),
    null,
  );
  await page.keyboard.press('Escape');
  await page.waitForTimeout(300);

  // The comment above is another whole-document rebuild, so §6's ghost gets
  // one more crossing — through the reviewer's OWN gesture this time, which
  // is exactly the sequence Court reported.
  check(
    "and §6's ghost survived that rebuild too — Court's own sequence, end to end",
    (await ghostsFor(WHOLE)).length === 1,
    await trailGhosts(),
  );
}

// --- §8 A GHOST IS NEVER PUT ON A BLOCK ITS ENTRY NEVER TOUCHED ------------
//
// THE OTHER HALF OF §6, AND THE ONE THAT MATTERS MORE. §6 asks that an entry
// which emptied its block keeps its place across a rebuild. This asks the
// question that makes that answer safe: when the entry's own block is GONE,
// does the rung refuse — or does it hand the ghost to whatever empty block
// happens to be lying about? Spec decision 3 is "refuses rather than
// guesses", and a red ghost drawn on a paragraph the reviewer never touched
// is that decision broken in the one direction that misleads instead of
// merely disappointing: the reviewer reads a deletion where none happened.
//
// THE GESTURE IS ORDINARY, WHICH IS WHY IT IS THIS ONE. Strike a line whole,
// then press Backspace once more to close the blank it left — the way anyone
// removes a paragraph. That second press is a cross-block join, so `recordOf`
// declines it (a join is not a text edit) and no entry records it; the
// existing entry maps forward into the merged paragraph, where `verifyEntry`
// correctly refuses it, and the ladder falls through to the empty-block rung
// with §6's empty block still standing several paragraphs away.
//
// RED IN TWO STAGES, BECAUSE THE TWO DEFECTS SHADOW EACH OTHER. Against HEAD
// the empty fence is a second candidate, so the rung refuses here by ACCIDENT
// — for ambiguity, not for identity — while §6 fails instead. With the
// literal blocks excluded and nothing else changed, §6 goes green and THIS
// section goes red: one empty block left in the document, unique, and the
// ghost of a sentence deleted five paragraphs away renders on it. Both are
// green only once the rung asks for evidence that the block is the entry's
// own. A gate that showed only the first stage would have certified a fix
// that moved the wrong anchor from a fence into the reviewer's own prose.
{
  const ghostText = async () => (await trailGhosts()).map((g) => g.text);

  await select(CLOSED);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);
  check(
    'the line is struck whole and leaves its own block empty, ghost and all',
    (await ghostsFor(CLOSED)).length === 1 &&
      !(await docText()).includes('goes away entirely'),
    await ghostText(),
  );
  const twoEmpty = await reviewerEmpties();
  check(
    'and now TWO blocks the reviewer emptied stand in the document, plus the fence that was always empty',
    twoEmpty.length === 3,
    twoEmpty,
  );

  // The second press: close the gap. An ordinary gesture, and the one that
  // takes the entry's own block out of the document underneath it.
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);
  check(
    'the blank is closed — the paragraph above it is whole and unchanged',
    (await docText()).includes(JOINS),
    await docText(),
  );
  check(
    "and the entry's own block is GONE: only §6's block and the empty fence are left",
    (await reviewerEmpties()).length === 2,
    await reviewerEmpties(),
  );

  // THE ASSERTION THE WHOLE SECTION IS FOR.
  check(
    "the ghost of the closed-up line is NOWHERE on the page — not on §6's block, not anywhere",
    (await ghostsFor(CLOSED)).length === 0,
    await ghostText(),
  );
  check(
    "and §6's block still carries its OWN ghost, exactly once — it was not taken over",
    (await ghostsFor(WHOLE)).length === 1,
    await ghostText(),
  );
  check(
    'and the refused entry has no place, which is what a record with no place is',
    (await adriftRows()).some((r) => r.includes('goes away entirely')),
    await adriftRows(),
  );
  const held = await trailRecords((c) =>
    c.some((x) => (x.old || '').includes('goes away entirely')),
  );
  check(
    'and the RECORD still stands — the place was refused, the change was not lost',
    held.some((x) => (x.old || '').includes('goes away entirely')),
    held,
  );
  // AND THE CHECK THAT WENT WITH `.gly-revise-trail` IS DELETED, NOT MOVED.
  // It read the outgoing clause off the verdict button — `Finish ▾ · 6 edits,
  // 2 replies` — and asserted the placeless entry was COUNTED in what the
  // press would tell the agent, which is what made the deleted changed-region
  // a deletion rather than a drop. That clause does not exist: the primary
  // reads `Revise · N` and N is the INSTRUCTION count (web/rounds-ux.mjs pins
  // it), the trail never reaches the server, and there is no surface anywhere
  // that counts it. So the reviewer is no longer told the entry exists before
  // they press, and no check here can honestly say they are. The claim above —
  // the RECORD stands with no place — is the whole of what is left of it.
}

// --- §9 AN EMPTY BLOCK BESIDE YOU IS NOT THE SAME AS NO BLOCK AT ALL -------
//
// §8'S GESTURE, ONE PARAGRAPH FURTHER DOWN, AND THAT IS THE WHOLE DIFFERENCE.
// There the emptied block had ordinary prose on both sides, so the evidence an
// entry carries — the text of the blocks either side of its own — discriminated
// on its own. Here the fox's block has §6's ALREADY-EMPTY block below it, and
// §6's block is the last in the document, so it has nothing below IT. The
// evidence recorded '' for both, because "the block that side is empty" and
// "there is no block that side" were the same value, and after the gap closes
// the fox's entry matches §6's surviving block on BOTH sides — the two-sided
// rule passing precisely where it was supposed to refuse.
//
// RED AGAINST THE BUILD THIS SECTION SHIPPED WITH: the fox's ghost renders on
// §6's block, beside §6's own, and the reviewer reads two deletions on a line
// that lost one. It takes two block-emptying deletions on adjacent blocks and a
// gap close — ordinary cleanup at the end of a document, which is where the
// last paragraph of a file usually is. §8 could never see it: every block it
// empties has prose under it.
{
  // THE CONFIGURATION NOTHING ELSE IN THIS FILE BUILDS, read off the document
  // BEFORE the gesture: the fox's paragraph is the second-to-last block and
  // §6's already-empty block is the LAST, so the fox's entry will record an
  // empty block after it and §6's has no block after it at all. That ORDER and
  // that END are the whole of what §9 tests, and a count of empty blocks sees
  // neither — a fixture edit that puts a paragraph between FOX and WHOLE, or
  // anything after WHOLE, keeps the count at 3 and leaves this section
  // certifying §8's configuration a second time.
  const standing = await tailBlocks(2);
  await select(FOX);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);
  check(
    'the fox line is struck whole, and its own ghost stands where it was',
    (await ghostsFor(FOX)).length === 1 &&
      !(await docText()).includes('quick brown fox'),
    (await trailGhosts()).map((g) => g.text),
  );
  const foot = await tailBlocks(2);
  check(
    "and it stands directly above §6's empty block, at the foot of the document",
    (await reviewerEmpties()).length === 3 &&
      standing.length === 2 &&
      standing[0].text === FOX &&
      standing[1].text === '' &&
      foot.length === 2 &&
      foot.every((b) => b.type === 'paragraph' && b.text === ''),
    { standing, foot, empty: await reviewerEmpties() },
  );

  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);
  check(
    'the blank closes into the paragraph above it, whole and unchanged',
    (await docText()).includes(FOOT),
    await docText(),
  );
  check(
    "and the fox's own block is gone: §6's block and the empty fence are what is left",
    (await reviewerEmpties()).length === 2,
    await reviewerEmpties(),
  );

  // THE ASSERTION THE SECTION IS FOR.
  check(
    "the fox's ghost is NOWHERE — an empty neighbour is not the same evidence as no neighbour",
    (await ghostsFor(FOX)).length === 0,
    (await trailGhosts()).map((g) => g.text),
  );
  check(
    "and §6's block still carries its OWN ghost, exactly once — one deletion, one ghost",
    (await ghostsFor(WHOLE)).length === 1,
    (await trailGhosts()).map((g) => g.text),
  );
  check(
    'and the refused entry is one with no place — which is a state, not a region it went to',
    (await adriftRows()).some((r) => r.includes('quick brown fox')),
    await adriftRows(),
  );
  const kept = await trailRecords((c) =>
    c.some((x) => (x.old || '').includes('quick brown fox')),
  );
  check(
    'and the record still stands — the place was refused, the record was not lost',
    kept.some((x) => (x.old || '').includes('quick brown fox')),
    kept,
  );
}

// --- §9b TWO DELETIONS ARE TWO GHOSTS, AND NEITHER IS THE OTHER'S ---------
//
// COURT'S THIRD FINDING, AND THE FOURTH SHAPE OF ONE BUG. §6 gave a
// block-emptying deletion a rung of its own, §8 made that rung ask for
// evidence, §9 made the evidence three-valued — and every one of those asks
// about ONE entry. Two of them against two empty blocks is not that question
// twice: it is an ASSIGNMENT, and the trail's own order is a fact about the
// SET that no per-entry rung can see. This section is the gesture that shows
// it, on a real keyboard, across a real server-side mutation.
//
// WHY THE REPEATED LINE. §9's three-valued evidence already tells an ADJACENT
// pair apart — the upper records "an empty block below" and the lower "an
// empty block above" — so two paragraphs struck in ordinary prose survive a
// rebuild on the build this section was written against, and a fixture built
// from one would report `ok` for a bug it never reached. TWIN is what closes
// that: three identical lines, two strikes between them, and both entries
// carry the same text on both sides. Measured before the set rule: both
// ghosts gone on the first mutation, both entries in the log-only region.
//
// AND THE SECOND HALF IS THE REFUSAL, WHICH IS THE HALF THAT MATTERS MORE. §8
// asks what happens when an entry's own block is joined away and some OTHER
// empty block is lying about; this asks the same thing when that other block
// is one ANOTHER ENTRY IS STANDING ON. Measured before the set rule: the
// closed-up entry's stale evidence matched the surviving entry's block
// exactly, and its ghost was drawn there — TWO GHOSTS ON ONE BLOCK, the
// reviewer reading two deletions on a line that lost one. That is the
// wrong-anchor failure spec decision 3 forbids, reached from a direction no
// amount of per-entry evidence can close: a block is only taken from the other
// entry's point of view.
//
// RED AGAINST THE BUILD THIS SECTION SHIPPED WITH, in both halves: the two
// ghosts vanish on the mutation, and — with that fixed and nothing else — the
// closed-up entry's ghost lands on its neighbour's block.

/** ghostPlaces reads every trail ghost off the live DOM WITH THE TOP-LEVEL
 *  BLOCK IT STANDS IN. A count of ghosts cannot tell "two ghosts on two
 *  blocks" from "two ghosts on one block", and the second of those is the
 *  wrong anchor this section exists to catch — so the block is read, not
 *  inferred. */
const ghostPlaces = () =>
  page.evaluate(() => {
    const root = document.querySelector('.ProseMirror');
    return Array.from(root.querySelectorAll('.gly-trail-ghost')).map((el) => {
      let block = el;
      while (block.parentElement && block.parentElement !== root) {
        block = block.parentElement;
      }
      return {
        text: el.textContent,
        block: Array.prototype.indexOf.call(root.children, block),
      };
    });
  });

{
  await select(UPPER);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);
  await select(LOWER);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(900);

  check(
    'both paragraphs are gone from the document, each leaving its own block empty',
    !(await docText()).includes('upper of two paragraphs') &&
      !(await docText()).includes('lower of the two'),
    await docText(),
  );
  const struck = await ghostPlaces();
  check(
    'and both ghosts stand, on two DIFFERENT blocks',
    struck.filter((g) => g.text === UPPER).length === 1 &&
      struck.filter((g) => g.text === LOWER).length === 1 &&
      new Set(
        struck
          .filter((g) => g.text === UPPER || g.text === LOWER)
          .map((g) => g.block),
      ).size === 2,
    struck,
  );

  // THE FIXTURE'S POINT, READ OFF THE SERVER RATHER THAN ASSERTED. If these
  // two records ever stop carrying identical evidence the run has lost its
  // repeated line, and everything below is proving §9's configuration a second
  // time.
  const pair = await trailRecords(
    (c) => c.some((x) => x.old === UPPER) && c.some((x) => x.old === LOWER),
  );
  const upperRow = pair.find((x) => x.old === UPPER);
  const lowerRow = pair.find((x) => x.old === LOWER);
  check(
    'the two records carry IDENTICAL evidence — neither can be identified on its own',
    !!upperRow &&
      !!lowerRow &&
      upperRow.before === lowerRow.before &&
      upperRow.after === lowerRow.after &&
      upperRow.before !== null &&
      upperRow.after !== null,
    { upperRow, lowerRow },
  );
  check(
    "and each says it HAD a place, which is what makes the trail's order readable at all",
    upperRow.placed === true && lowerRow.placed === true,
    { upperRow, lowerRow },
  );

  // THE REBUILD, exactly as §6 stages it: a comment POST is a server-side
  // mutation, so the whole document is replaced and every live position with
  // it. Everything the two entries have left is what they recorded.
  const filed = await fetch(`${base}/_galley/instruct`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      op: 'comment',
      target: 'very last gap',
      text: 'and this one rebuilds the document under the pair',
      author: 'court',
    }),
  });
  check(
    'the comment that rebuilds the document landed',
    filed.ok,
    filed.status,
  );
  await page.waitForTimeout(2000);
  check(
    'and the rebuild really happened — the instruction is in the round',
    (await filedInstructions()).some(
      (i) => i.text === 'and this one rebuilds the document under the pair',
    ),
    (await filedInstructions()).map((i) => [i.quote, i.text]),
  );

  // THE ASSERTION THE SECTION IS FOR.
  const after = await ghostPlaces();
  const mine = after.filter((g) => g.text === UPPER || g.text === LOWER);
  check(
    'BOTH ghosts are still on the page after the whole document was replaced',
    mine.filter((g) => g.text === UPPER).length === 1 &&
      mine.filter((g) => g.text === LOWER).length === 1,
    after,
  );
  check(
    'and they are still on two different blocks — two deletions, two lines',
    new Set(mine.map((g) => g.block)).size === 2,
    mine,
  );
  check(
    'and neither fell into the log-only region',
    !(await adriftRows()).some(
      (r) =>
        r.includes('upper of two paragraphs') || r.includes('lower of the two'),
    ),
    await adriftRows(),
  );

  // AND NOW THE REFUSAL. Close the LOWER blank — the ordinary second
  // Backspace §8 uses — and the lower entry's own block leaves the document
  // while its evidence still matches the upper entry's block exactly.
  // The caret goes to the head of the LAST empty block standing under a
  // repeated line, which is the lower entry's own. An empty block cannot be
  // found by its text — it has none — so it is reached through the line above
  // it, and the LAST such pair is taken because the upper entry's block sits
  // under a repeated line too and must not be the one closed.
  await page.evaluate((twin) => {
    const editor = window.galleyEdit.editor;
    const { doc } = editor.state;
    let at = null;
    let pos = 0;
    for (let i = 0; i < doc.childCount - 1; i += 1) {
      pos += doc.child(i).nodeSize;
      if (
        doc.child(i).textContent === twin &&
        doc.child(i + 1).isTextblock &&
        doc.child(i + 1).content.size === 0
      ) {
        at = pos + 1;
      }
    }
    if (at === null) {
      throw new Error('fixture: no empty block under a repeated line');
    }
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at });
    editor.view.focus();
    if (!editor.isFocused) {
      throw new Error(
        'fixture: the editor did not take focus — a keystroke would go nowhere',
      );
    }
  }, TWIN);
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(1200);

  const closed = await ghostPlaces();
  check(
    "the lower entry's ghost is NOWHERE — a block another entry stands on is not free",
    closed.filter((g) => g.text === LOWER).length === 0,
    closed,
  );
  check(
    'and the upper entry still carries its OWN ghost, exactly once — it was not doubled',
    closed.filter((g) => g.text === UPPER).length === 1,
    closed,
  );
  check(
    'and the refused entry has no place — the record stands, the place was refused',
    (await adriftRows()).some((r) => r.includes('lower of the two')),
    await adriftRows(),
  );
  const kept = await trailRecords((c) =>
    c.some((x) => x.old === LOWER && x.placed === false),
  );
  check(
    'and it stands as a record with NO PLACE — the change was not lost, the place was refused',
    kept.some((x) => x.old === LOWER && x.placed === false) &&
      kept.some((x) => x.old === UPPER && x.placed === true),
    kept,
  );
}

// --- §10 A SEALED REVIEW RECORDS NOTHING, FROM A REAL KEYBOARD ------------
//
// LAST, because it ends the review — and it is now genuinely last, after §6-§9
// rather than after §5c, which makes it a STRONGER claim than it was on its own
// branch: the page it seals is carrying §6's surviving ghost and the log-only
// rows §8 and §9 left, so every claim below is made against a trail that
// actually holds something. On dev alone every decoration it reads was already
// absent by §5c's undo, and each check would have passed on an empty page.
// That ordering is what turned "the verdict retired the trail" from a green
// check into the regression recorded at its site.
//
// This is the gate that made the seal worth building. The failure it exists to
// remove is not a rendering bug: after Approve the server kept serving, the
// page kept taking keystrokes, and the registry entry was already withdrawn —
// so the reviewer typed into a document that could never send, and found out
// by finding out. Every other check here proves that a keystroke DOES
// something; this one proves that after a verdict it does not.
//
// A REAL KEYBOARD, for this file's own reason: `setEditable(false)` is a claim
// about a flag, and the claim under test is about what happens when someone
// types. The whole population beneath it — the plugin, the trail recorder, the
// debounced save, the projection — is exercised by pressing keys and by nothing
// else.
{
  const before = await docText();
  const fileBefore = readFileSync(DOC, 'utf8');

  await fetch(`${base}/_galley/revise`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ verdict: 'approve', trust: true }),
  });
  // One poll of the editor's own beat, plus room: the page adopts the seal
  // from /_galley/revise, not from the response to a request it did not make.
  await page.waitForTimeout(3000);

  const state = await page.evaluate(() => ({
    editable: window.galleyEdit.editor.isEditable,
    sealShown: (() => {
      const el = document.querySelector('#gly-seal');
      return !!el && getComputedStyle(el).display !== 'none';
    })(),
    seal: (document.querySelector('#gly-seal') || {}).textContent || '',
  }));
  check(
    'the verdict sealed the page — the terminal bar is up',
    state.sealShown && /^(Approved|Discarded)/.test(state.seal.trim()),
    state,
  );
  check(
    'and the editor is no longer editable',
    state.editable === false,
    state,
  );

  // THE VERDICT NO LONGER RETIRES THE TRAIL, AND THAT IS A REGRESSION THIS
  // GATE FOUND RATHER THAN A CHECK THAT STOPPED APPLYING. It asserted zero
  // ghosts and zero highlights after the seal, and it ran RED on the repair:
  // eight ghosts survived, including §6's, §8's and §9's, on a page that is no
  // longer editable.
  //
  // THE MECHANISM, READ OFF THE PRODUCT. `App.clearTrail` is the browser's
  // half and it is called from exactly one place — the 204 coming back from
  // THIS PAGE'S OWN approving press. Every other route to a verdict reached it
  // through the EPOCH: the server bumped `trailEpoch` in the same act that
  // wiped its copy, `adoptTrail` compared it on the next ordinary pending
  // poll, and a mismatch made the server's list REPLACE this page's. The
  // server publishes no `trailEpoch` and keeps no copy — `pendingView` is
  // `{instructions, blocks}` — so a verdict taken anywhere but in this tab
  // never reaches this tab's trail at all. §10 approves through `POST
  // /_galley/revise`, which is a second tab, a `galley` verb, or an
  // `--on-revise` command, and is the ordinary case rather than an exotic one.
  //
  // IT IS PRINTED AND NOT ASSERTED, which is this codebase's stated answer for
  // a surface that genuinely cannot make its claim: a permanently-red check
  // over a known gap trains people to ignore the gate. Fixing it is a server
  // change (republish the epoch, or retire the trail on the seal the page
  // already adopts) and belongs to whoever owns the trail's second life, not
  // to the repair that made this file run again. When it is fixed, this note
  // becomes the check it was.
  {
    const left = { ghosts: await trailGhosts(), inserts: await trailInserts() };
    console.log(
      `      [note] the verdict left ${left.ghosts.length} ghost(s) and` +
        ` ${left.inserts.length} highlight(s) standing on a sealed page —` +
        " the trail is retired only by this tab's own press (see above)",
    );
  }

  // WHAT THE VERDICT LEFT, so the three checks after the keystroke can be
  // stated as DELTAS. They asserted zero, which was only ever expressible
  // because the retirement above emptied the trail first; the claim they were
  // making — a keystroke into a sealed page records NOTHING — is untouched by
  // the retirement being broken, and a delta states it without borrowing the
  // broken thing's word for it. It is the stronger form of the two in one
  // respect the absolute never was: it would catch a page that retired the
  // trail correctly AND then recorded the sealed keystroke into the empty one.
  const sealedGhosts = (await trailGhosts()).length;
  const sealedInserts = (await trailInserts()).length;
  const sealedAdrift = (await adriftRows()).length;
  const sealedRecords = (
    await page.evaluate(() => window.galleyEdit.app.trailEntries())
  ).length;

  // AND NOW THE KEYS — through a real MOUSE CLICK into the prose, not through
  // selectWithin.
  //
  // The helper every other section uses refuses here, and its refusal is the
  // first half of the answer: it asserts the editor took focus, and a
  // non-editable ProseMirror does not. That is a fixture guard doing its job,
  // not a failure — so the gesture is spelled out at the level a reviewer
  // actually performs it: click where the words are, then type and backspace,
  // the two gestures that produce an insertion and a ghost on a live page.
  const target = await page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('.ProseMirror > *')).find(
      (n) => n.textContent.includes('about the connection'),
    );
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x + Math.min(40, r.width / 2), y: r.y + r.height / 2 };
  });
  check(
    'the paragraph to type into is on screen — otherwise this proves nothing',
    !!target,
    target,
  );
  await page.mouse.click(target.x, target.y);
  await page.waitForTimeout(200);
  check(
    'and the click did not put a caret in it — a sealed document is not focusable',
    (await page.evaluate(() => window.galleyEdit.editor.view.hasFocus())) ===
      false,
  );
  await page.keyboard.type('ZZZ');
  await page.keyboard.press('Backspace');
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(1200);

  check(
    'a keystroke into a sealed page changes NOTHING in the document',
    (await docText()) === before,
    { before: before.slice(0, 80), after: (await docText()).slice(0, 80) },
  );
  check(
    'and leaves no trail decoration behind it — not one ghost or highlight MORE',
    (await trailGhosts()).length === sealedGhosts &&
      (await trailInserts()).length === sealedInserts,
    {
      was: { ghosts: sealedGhosts, inserts: sealedInserts },
      now: { ghosts: await trailGhosts(), inserts: await trailInserts() },
    },
  );
  check(
    'and no NEW adrift row — nothing was recorded to lose its place',
    (await adriftRows()).length === sealedAdrift,
    { was: sealedAdrift, now: await adriftRows() },
  );

  // THE RECORDER IS ASKED, not just the screen. A page that recorded an entry
  // and simply failed to DRAW it would look identical from the DOM alone, so
  // the plugin's own list is read: a resurrected recorder surfaces here even
  // when nothing is painted. It used to be the SERVER that was asked, which
  // was the stronger form — and the server keeps no trail now, so the strength
  // is gone with the population and is not being claimed.
  const changes = await trailRecords((c) => c.length > sealedRecords);
  check(
    'and the trail gained NOT ONE record — a sealed review has no hand to record',
    changes.length === sealedRecords,
    { was: sealedRecords, now: changes },
  );

  // And the file. The document of record gets the last word here too.
  await page.waitForTimeout(1200);
  check(
    'and the .md on disk is byte-identical to what the verdict left',
    readFileSync(DOC, 'utf8') === fileBefore,
    {
      wasLength: fileBefore.length,
      nowLength: readFileSync(DOC, 'utf8').length,
    },
  );
}

await browser.close();
server.kill('SIGTERM');

console.log(
  failures === 0 ? '\nall typing checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
