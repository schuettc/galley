// ======================= THIS GATE RUNS, AND IS LOAD-BEARING =================
//
// `node web/layers.mjs` is 263/0 as of this migration and has been one of the
// four gates guarding every extraction in it — `just verify`'s local runs and
// CI both hold it green. It drives a real chromium against a real
// `galley edit` and reads `getComputedStyle`, which is the only way to catch
// the CSS bug class this file exists for: a more-specific selector silently
// beating an intended one, so a rule ships reviewed and never applies. If this
// file goes dark again, say so here the way this note once had to — a banner
// that is wrong in the safe direction still cost four gates staying silently
// dead for weeks, once.
//
// ITS HISTORY, FOR CONTEXT ON THE FIXTURE BELOW: it once WAS dark. Measured
// 2026-08-20 on `main` at 4b5561d, the gate died in its fixture setup —
//
//     Command failed: bin/galley suggest <fixture>.md --comment ... --on ...
//     galley: unknown command "suggest"
//
// — because `galley suggest` and `galley apply` had been deleted by `1cb6937
// review: complete the rounds-only architecture` and `a052b4a feat!: remove
// galley apply`. The agent's write surface became the file itself, and the
// reviewer's surface became an instruction rail rather than a population of
// decidable proposals; this file's fixture was never carried across, so every
// one of its checks went dark with nothing shouting about it.
//
// WHAT THE PALETTE-AND-PRIMARY BRANCH THEN CHANGED UNDER IT, kept here so the
// repair did not resurrect a check for something that was already gone by the
// time it landed: `.gly-revise-trail` and its 23ch reserve were DELETED (the
// trail's clause stopped being written when the rounds-only workflow arrived,
// and the reserve outlived the words); the pending count sits in
// `.gly-revise-count` on the same button, with the same argument; and the
// `.gly-amber` state class became `.gly-on`, because the colour it was named
// for moved to `--gly-signal`. It was not that branch that broke this gate —
// it was already dark — and that branch could not use this gate to hold the
// property it is here for; `web/rounds-ux.mjs` held the nothing-moves
// property over the primary's label in the meantime, see 'the primary
// changing its label moves nothing in the bar' there.
//
// The fixture was since rebuilt against the rounds-only product (the checks
// below read the instruction rail and the sealed/reopened states it actually
// has), which is how the gate is 263/0 now rather than a rewritten claim to
// be dark less convincingly.
// ===========================================================================
//
// layers.mjs — the computed-style gate for the visual system.
//
// IT IS SEQUENCED BY STATE, NOT BY SECTION NUMBER. The blocks run §1b, §1c, §4,
// §4b, §3, §5, §5b, §1, §7, §7a–§7e, and that order is deliberate rather than
// drift: the resting bar colours have to be read BEFORE the overall handle is
// clicked (amber on an open handle is correct and would be read as a
// regression), the panel has to be OPEN before §7's dark-mode loop can read
// `.gly-overall`, and the settled region has to be opened before §4b can read a
// card in it. One instrument, driven through the states in the order the
// product reaches them; the section numbers are the handoff spec's, and they
// are not a running order. Every state a block sets, it also puts back.
//
// docs/design/2026-08-08-handoff-spec.md assigns every element to exactly one
// of three layers, and every change it asks for is PAINT. Paint is invisible to
// motion.mjs, which measures rects, and invisible to probe.mjs, which has no
// DOM at all. It is also where this stylesheet's worst class of bug lives:
// `.gly-card button` (0,1,1) silently beats `.gly-thread-delete` (0,1,0), so a
// rule can be written, reviewed, commented and shipped without ever applying.
// Reading the CSS does not catch that. getComputedStyle does.
//
// Not part of `just verify` — it drives a real chromium against a real
// `galley edit`, neither of which CI has. `just layers` is the gate any change
// to a surface, a card or a verb has to clear before it lands.

import { execFileSync, spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// THE GATE READS THE APP'S OWN LIST, not a copy of it. §8's seal and reopen
// blocks are claims about SEALED_VERBS, and a check named for a claim must read
// the selector the claim is about — a hand-copied string here would have gone
// stale the moment `.gly-overall-input` joined the constant, and the reopen
// half would have kept reporting `ok` over a control it never looked at. That
// is exactly how it shipped: the reopen read was rail-scoped, and three
// elements of this list were dead on a reopened page with the gate green.
// probe.mjs already imports this module under node, so the import costs
// nothing new.
import { SEALED_VERBS, SEAL_ONLY_VERBS } from './seal.ts';
import { UNTRACKED_NOTE } from './sheet.ts';
// AND THE ARITHMETIC'S OWN CONSTANTS, for the same reason one line up. §9 read
// the card's reserved gutter as a literal `26` beside a comment naming a
// constant — a number that has now outlived TWO mechanisms it was named for
// (the connector's whole reach, then a connector that did not live in the
// gutter at all, then no connector) and is imported under the name that says
// what it does. §10's sentence is the rail's own. A transcription of either
// would keep reporting `ok` about a page that had changed underneath it, and
// entry.ts already imports this module, so it costs nothing new here.
import { GUTTER_PX, unplacedSaid } from './rail.ts';

const GALLEY = process.env.GALLEY || 'bin/galley';
const PORT = Number(process.env.PORT || 8252);
const WIDE = { width: 1600, height: 1100 };
// Split so §8 can say WHICH selector came back dead, and so a selector that
// matches nothing is a named failure rather than a silent subtraction from a
// group query.
const SEAL_SELECTORS = SEALED_VERBS.split(',')
  .map((s) => s.trim())
  .filter(Boolean);

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

/** A NOTE IS NOT A CHECK, and the difference is whether it can go red.
 *
 * CLAUDE.md's rule for a surface that genuinely cannot be asserted is to print
 * the number as a note OUTSIDE the pass/fail counters rather than asserting it
 * at zero. The same rule catches the opposite failure, which this file shipped:
 * a predicate whose two sides are equal in every reachable state reports `ok`
 * against the exact bug it was written for (§8's `✓ all` sweep did, against the
 * broken tree, in the same run that turned two other checks red). Print it,
 * name why it cannot fail, and put the assertion somewhere that can. */
function note(name, detail) {
  console.log(
    `note  ${name}${detail === undefined ? '' : ` — ${JSON.stringify(detail)}`}`,
  );
}

const HERE = mkdtempSync(join(tmpdir(), 'galley-layers-'));
// THE NAME IS PART OF THE FIXTURE, and `doc.md` made the widest entry in §7a's
// sweep decorative. The document's name renders in `.gly-doc` at the head of
// the bar, so it is one of the widths the bar has to fit — and at six
// characters it left 1600px with room to spare, so the bar never folded there
// and "every control can be pressed at 1600px" PASSED AGAINST THE BROKEN
// BUILD. A probe with a realistic name failed at that same width. A check that
// can only pass is not a check.
//
// Twenty-eight characters is an ordinary filename, not a stress test — this
// file's own name is twelve and the plan documents beside it are longer. It
// used to fold the bar at 1600, which is what let the sweep's top entry fail
// again; then the untracked sentence became a basis-0 bar cell and the
// whole-doc handle dropped "on the whole doc", and the boundary moved.
//
// MEASURED 2026-08-16, against the ONE readout: flat at 1248px (52.2px tall),
// folded at 1246 (91.4px). Re-measured because the previous figures here
// (flat 1240, folded 1230) were taken while the bar still had three readout
// cells and the standing sentence still had a `display: none` below 1340 —
// both gone, and the boundary went the OPPOSITE way from the arithmetic on
// gaps alone: two fewer cells is 24px less bar, but the sentence no longer
// leaves at 1340, so the fold arrives ~16px WIDER rather than narrower. Which
// is exactly why this is measured and not reasoned.
//
// The sweep's 1200/1100/1000/992 widths still walk a folded bar with it, and
// THAT IS ASSERTED NOW rather than left to this comment — see the fold checks
// after the sweep loop. Held by a filename alone it was the fixture hazard
// CLAUDE.md names, in the gate that sweeps eleven widths. 1600 walking a flat
// bar is no longer a fixture hole but the product's own new shape — an
// ordinary name is not SUPPOSED to fold 1600 any more, and motion.mjs carries
// a name long enough to fold its own wide viewport on purpose.
//
// RE-MEASURED 2026-08-20, against the instruction bar. The bar lost its
// `Instructions · N` chip to one filled primary and the whole-document handle
// to the rail, so it is narrower again and a 28-character name folded NOTHING:
// measured flat (52.2px) at every one of the sweep's widths down to 992, which
// left every check in that loop walking the shape the rename was meant to stop
// certifying — and the fold assertion after it is what said so. The walk, on
// this bar: 28 characters folds nothing, 40 folds 1100 and below, FIFTY folds
// 1200 and below while leaving 1600 flat (52.2px), 60 folds 1240 too. Fifty-one
// is what ships. Still an ordinary filename, not a stress test.
//
// One measured oddity worth knowing before anyone "fixes" it: 992 folds and
// 991 does NOT. The narrow rules land at 991 — the bar's gap drops 12px → 8px
// across eight items and `.gly-census-count` leaves entirely — and that buys
// back more than the 1px of window costs. The sweep walks both.
//
// Nothing else reads it: `.gly-doc` is the only place it renders in the bar
// (the census handle counts notes and does not name the file), and the panel
// head that does name it is full-bleed and fixed, so its width is the window's
// either way. Checked before renaming.
const DOC = join(HERE, 'the-visual-layers-handoff-and-its-computed-style.md');
writeFileSync(
  DOC,
  `# Layers

An opening paragraph mentioning the retry budget, which is worth a conversation.

A second paragraph containing the phrase alpha, which someone wants replaced.

A third paragraph naming the phrase gamma, which is also due to change.

A fourth paragraph raising the settled question, which was answered and closed.

A fifth paragraph naming the release note, which somebody has to own.

A sixth paragraph kept plain so the trail pass can strike a word in it.

A seventh paragraph holding the word omega and the word sigma, reserved for a revision.

An eighth paragraph naming the withdrawn phrase, whose highlight goes away under it.

A ninth paragraph reserved for the applied round, which the agent revises outright.

![a figure with a caption](fig.svg)
`,
);

// A figure, because "figures inherit §1 without new rules" is the third of the
// handoff's three unrendered predictions and it cannot be tested against a
// document that has no figure in it. An SVG rather than a raster: it is three
// lines of text, it has an intrinsic size so the box is a real box, and it is
// served from beside the document the way any relative src is.
writeFileSync(
  join(HERE, 'fig.svg'),
  `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="64" viewBox="0 0 160 64">
  <rect width="160" height="64" fill="#c9cee0"/>
  <rect x="12" y="12" width="136" height="40" fill="#8b93b5"/>
</svg>
`,
);

// --- WHAT THE FIXTURE IS BUILT FROM, AND WHY IT CHANGED ----------------------
//
// This whole block used to be `galley suggest`, `galley resolve`, `galley reply`
// and `galley blocks`, run against the .md before the server was started: two
// ins/del proposals, a resolved thread, eight replies deep enough to cap a
// bubble, a block comment on the figure and a comment whose highlight was then
// taken out from under it. `galley suggest` was deleted with the proposal
// machinery (1cb6937) and `galley apply` after it (a052b4a); `resolve`, `reply`
// and `blocks` went with them. The reviewer's side of the loop is INSTRUCTIONS
// now — an immutable round of asks, sent by one press of Revise — and the agent
// writes the file directly.
//
// So the fixture is seeded over `POST /_galley/instruct`, which is the endpoint
// the composer and the whole-document input already post to (entry.ts), and
// which takes the same three shapes `galley suggest` did: `comment` with a
// plain-text `target`, `comment_block` with a block key, and
// `comment_document`. `guard` in serve.go admits a request with no Origin —
// that is the CLI's shape — so node can post it, and the seeding runs against
// the live server rather than against the file, which is the one difference
// that matters: the document is OPEN, so nothing may write the .md behind it.
//
// THREE POPULATIONS THE PRODUCT NO LONGER HAS, and every check that read them
// is deleted at the section it stood in rather than pointed at a substitute:
//
//   the two `--replace` proposals   §5 and §5b are about the substitution card,
//                                   which is a component with no data to build
//                                   it from.
//   the resolved thread             a card carries one verb, delete; there is
//                                   no resolve, so nothing settles, so §4b and
//                                   §7b' read an empty region forever.
//   the eight replies               a card carries no reply box, so §7c's cap
//                                   has nothing to overflow.
//
// The anchorless thread survives and is built differently: a BLOCK comment is
// filed the ordinary way and the reviewer then deletes its mark IN THE EDITOR.
// (Deleting the words under a TEXT comment retracts the comment instead —
// lostanchor.go — so that gesture makes no unplaced card any more.) That has
// to happen with the browser attached, so it is done in-page at §9 rather than
// here.
const galley = (...args) =>
  execFileSync(GALLEY, args, { encoding: 'utf8', stdio: 'pipe' });

// The headings the sections below look their fixtures up by. Read back from the
// server rather than transcribed, for the reason every key here always was: a
// heading is derived from what the comment is anchored to, so a hand-copied one
// stops resolving the day the sentence above it changes and leaves the fixture
// quietly empty — which is the pass-forever shape this repository has recorded
// six of.
const SETTLED_HEADING = 'the settled question';
const RETRY_HEADING = 'the retry budget';
const WITHDRAWN = 'the withdrawn phrase';
const WITHDRAWN_ASK = 'does this still apply?';
const FIGURE_LABEL = 'a figure with a caption';

// --on-revise, and it is §8's alone: handleRevise refuses a verdict outright
// (501) when there is neither a command configured nor a `galley wait` blocked,
// and §8 approves. `true` runs on a Revise PRESS, which no check here makes, so
// nothing else in this pass changes shape for it.
const server = spawn(
  GALLEY,
  ['edit', DOC, '--no-open', '--port', String(PORT), '--on-revise', 'true'],
  {
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: true,
  },
);
// THE SERVER'S OUTPUT IS DRAINED, AND SPOKEN FOR ONLY IF IT DIES. Two reasons,
// and the first is not diagnostic at all: a piped stdio nobody reads fills its
// 64KB buffer and BLOCKS the writer, which for a long run turns the server into
// a process that stops answering. The second is that when the server does go
// away, every later `page.evaluate` that fetches fails as `TypeError: Failed to
// fetch` with no hint of why — seen three times in one session, once at 2.4s
// and once at 19.7s into a run, with `code=0` and nothing on either stream,
// which is a SIGTERM from outside this process. Kept quiet on a healthy run so
// the gate's own output stays readable.
const said = [];
const startedAt = Date.now();
for (const stream of [server.stdout, server.stderr]) {
  stream.on('data', (d) => said.push(String(d)));
}
server.on('exit', (code, sig) => {
  process.stderr.write(
    `\n[the server exited ${Math.round((Date.now() - startedAt) / 100) / 10}s in — code=${code} sig=${sig}]\n` +
      `${said.join('').trimEnd()}\n` +
      '[every check after this point that reads the server is reading nothing]\n',
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
    // ENOTEMPTY, seen once: the server is being SIGTERMed at this exact moment
    // and can write its files back into the directory mid-removal. A tmpdir
    // left behind is nothing; an exit handler that THROWS turns a run where
    // every check passed into a non-zero exit, which is a gate reporting a
    // failure that did not happen.
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

await new Promise((resolve, reject) => {
  const started = Date.now();
  const poll = async () => {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/`);
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

// --- the round the reviewer has already filed --------------------------------
//
// FAILURES ARE FATAL. A seeded instruction that quietly 400s is a card no
// section below ever measures, and a gate whose fixture is half there reads
// "nothing overflows", "no card is adrift" and "the region holds nothing" as
// passes.
const BASE = `http://127.0.0.1:${PORT}`;
const instruct = async (body) => {
  const res = await fetch(`${BASE}/_galley/instruct`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(
      `fixture: /_galley/instruct refused ${JSON.stringify(body)} — ` +
        `${res.status} ${(await res.text()).trim()}`,
    );
  }
};
const pending = async () =>
  await (await fetch(`${BASE}/_galley/pending`)).json();

// AND IT IS A PARAGRAPH RATHER THAN A LINE, so §7c has a card with real height
// to place. It used to be EIGHT REPLIES DEEP, filed with `galley reply`, and
// that verb is gone with the conversation it belonged to: an instruction is a
// single immutable entry, so the tallest thread this product can hold is one
// long ask. It is a plausible instruction rather than filler, which is the
// difference between a fixture and a stress string.
await instruct({
  op: 'comment',
  target: RETRY_HEADING,
  text:
    'Per host or global? The per-connection reading is what the old client did and it is ' +
    'why the numbers never matched, so say which one this is in the sentence itself rather ' +
    'than leaving it to the reader. While you are in here: the default should probably come ' +
    'down, it should be a config value rather than a constant, and it interacts with the ' +
    'backoff schedule — which is a second conversation, but keep a line about it here so it ' +
    'is not lost. Backoff itself stays exactly as it is for now.',
});
await instruct({ op: 'comment_document', text: 'a note about the whole file' });
// A SECOND HIGHLIGHT, AND IT IS FURTHER DOWN THE PAGE ON PURPOSE. §7c's flip
// case — no room below the mark, so the card goes above it — needs a mark with
// real room ABOVE it, and the retry-budget one sits ~195px from the top of the
// document at every window this file uses. The only way to give that one no
// room below was to make the whole window 260px tall, which is a window where
// the design cannot work at all: the card is floored at a height that fits
// neither side, and a check that ran there was measuring the degradation and
// calling it the rule.
await instruct({
  op: 'comment',
  target: 'the release note',
  text: 'who owns this?',
});
// The sentence the settled thread was resolved on. It carries an ordinary
// instruction now — nothing can settle — and it is kept because §4b's block
// reads the region it would have gone into, and an empty rail is a worse
// fixture than a full one for every check that is not about settling.
await instruct({
  op: 'comment',
  target: SETTLED_HEADING,
  text: 'answered, and kept',
});
// The thread whose highlight the reviewer takes away at §9 — filed here so it
// is an ordinary anchored card for everything before that point.
await instruct({
  op: 'comment',
  target: WITHDRAWN,
  text: WITHDRAWN_ASK,
});

// AND ONE CONVERSATION ON A BLOCK — the shape that has no run BY CONSTRUCTION,
// and the one this fixture would otherwise be missing.
//
// IT IS HERE TO MAKE §10's ORPHAN CHECK CAPABLE OF FAILING. That check asks
// whether an anchorless conversation is in the map, and it asked it as
// `!c.dataset.run` — the discriminator CLAUDE.md records as WRONG, because
// `threadCard` writes `dataset.run = thread.run || ''` and a block or figure
// thread has no run to write. With this thread here the old form reports the
// figure's conversation as an orphan in the map, which is exactly the "one
// wrong predicate, two symptoms" entry arriving inside a check.
//
// THE KEY IS READ BACK from the pending view's own `blocks`, which is where
// `galley blocks` used to get it and where the browser's section grip gets it:
// it is a content hash, so a transcribed one would stop resolving the day the
// caption changes and leave the fixture quietly without a block thread again.
{
  const blocks = (await pending()).blocks || [];
  const figure = blocks.find(
    (b) => b && b.kind === 'image' && (b.label || '').includes(FIGURE_LABEL),
  );
  if (!figure)
    throw new Error(
      `fixture: no image block for "${FIGURE_LABEL}" to comment on`,
    );
  await instruct({
    op: 'comment_block',
    target: figure.key,
    text: 'does this picture still match the text?',
  });
}

{
  const filed = (await pending()).instructions || [];
  if (filed.length !== 6) {
    throw new Error(
      `fixture: ${filed.length} instructions landed, wanted 6 — ` +
        JSON.stringify(filed.map((i) => i.quote)),
    );
  }
}

const browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);
const page = await browser.newPage({ viewport: WIDE });
page.on('pageerror', (e) => console.log(`      [page error] ${e.message}`));
await page.goto(`http://127.0.0.1:${PORT}/`, { waitUntil: 'networkidle' });
await page.waitForSelector('.gly-rail-band .gly-card', { timeout: 15000 });
await page.waitForTimeout(1500);

/** Every computed value this pass reads goes through here — one place that
 *  reports a missing element as null rather than throwing halfway down. */
const style = (sel, ...props) =>
  page.evaluate(
    ([s, ps]) => {
      const el = document.querySelector(s);
      if (!el) return null;
      const cs = getComputedStyle(el);
      return Object.fromEntries(ps.map((p) => [p, cs.getPropertyValue(p)]));
    },
    [sel, props],
  );

/** The same, for every match — "the destroy weight renders EVERYWHERE a thread
 *  renders" is a claim about all of them, and one passing instance is how a
 *  regression hides. */
const styleAll = (sel, ...props) =>
  page.evaluate(
    ([s, ps]) => {
      const cs = (el) => {
        const c = getComputedStyle(el);
        return Object.fromEntries(ps.map((p) => [p, c.getPropertyValue(p)]));
      };
      return Array.from(document.querySelectorAll(s)).map(cs);
    },
    [sel, props],
  );

const TRANSPARENT = 'rgba(0, 0, 0, 0)';
const token = (name) =>
  page.evaluate(
    (n) =>
      getComputedStyle(document.documentElement).getPropertyValue(n).trim(),
    name,
  );
const rgb = async (name) =>
  page.evaluate(
    (v) => {
      const probe = document.createElement('span');
      probe.style.color = v;
      document.body.appendChild(probe);
      const out = getComputedStyle(probe).color;
      probe.remove();
      return out;
    },
    await token(name),
  );

/** Reads a declared value straight off a matched CSSOM rule, not the painted
 *  pixel — the one place in this file that deliberately does NOT read the
 *  artifact.
 *
 *  It was added because `.gly-bar button`, edit.html's inline fallback, tied
 *  `.gly-census button` on specificity and won the cascade by sitting in a
 *  later stylesheet, so `getComputedStyle` on any census button answered "what
 *  does the shell say", not "what does the census's own rule say" — and the
 *  computed radius check passed with `.gly-census button` reverted to 999px.
 *  §1b below has since taken that rule out of the shell and given it to this
 *  stylesheet ABOVE `.gly-census button`, so the census now wins its own
 *  paint and the computed check discriminates on its own (verified by
 *  reverting the declaration to 999px and watching BOTH radius checks fail).
 *
 *  Kept, because the two questions are still different ones: the computed
 *  check reads what a user sees, and this reads what the census's own rule
 *  claims. A future rule that ties and out-orders `.gly-census button` would
 *  re-confound the first and leave this one standing.
 *
 *  Takes the LAST rule whose selectorText matches, the same tiebreak the
 *  cascade itself uses among rules of equal specificity. It walks TOP-LEVEL
 *  rules only — it does not recurse into `@media` or `@supports` bodies, so a
 *  declaration inside one is invisible to it. Nothing it is asked about today
 *  lives in an at-rule; if that changes, this has to grow a recursion rather
 *  than quietly answer null. */
const ruleValue = (selectorText, prop) =>
  page.evaluate(
    ([sel, p]) => {
      let found = null;
      for (const sheet of document.styleSheets) {
        let rules;
        try {
          rules = sheet.cssRules;
        } catch {
          continue;
        }
        for (const r of rules) {
          // A GROUPED SELECTOR IS STILL THIS SELECTOR'S RULE, and matching
          // `selectorText` whole could not see that. `.gly-census button`
          // declared its own 6px until the stylesheet consolidation merged it
          // with `.gly-verdict-menu button` — one rule, both members, the same
          // declaration — and this read `null` and failed a check whose subject
          // had not changed at all. The DECLARATION is what the check is about;
          // whether it is reached through a lone selector or a grouped one is
          // the stylesheet's business. Members are compared exactly, so this
          // stays a question about a named selector rather than a prefix match.
          const owns = (r.selectorText || '')
            .split(',')
            .some((one) => one.trim() === sel);
          if (owns) found = r.style.getPropertyValue(p) || found;
        }
      }
      return found;
    },
    [selectorText, prop],
  );

/** The selectors declared in edit.html's inline <style> — the block that loads
 *  AFTER editor.css and so wins every specificity tie.
 *
 *  `ruleValue` above cannot answer the question this helper exists for. Once
 *  editor.css owns `.gly-bar button` too, "is there a rule with this selector"
 *  is true from either sheet, and WHICH sheet is the entire question. It is
 *  also worth saying plainly that `ruleValue` walks only top-level rules and
 *  does not recurse into `@media` or `@supports` bodies; the shell declares
 *  its button chrome at the top level and only its dark tokens inside an
 *  at-rule, so that gap does not bite here, but it is a gap.
 *
 *  The block is identified by a selector only it declares — `.gly-spacer` —
 *  rather than by "has no href", so another inline <style> (mermaid injects
 *  one when a diagram renders) cannot be mistaken for it. */
const shellSelectors = () =>
  page.evaluate(() => {
    for (const sheet of document.styleSheets) {
      let rules;
      try {
        rules = Array.from(sheet.cssRules);
      } catch {
        continue;
      }
      const sels = rules.map((r) => r.selectorText).filter(Boolean);
      if (sels.includes('.gly-spacer')) return sels;
    }
    return null;
  });

// --- §1b · the chrome layer owns its own controls -------------------------
//
// `.gly-bar button` lived in edit.html's inline <style>, which loads AFTER
// editor.css and so wins every tie. It was written as a bundle-less fallback
// for the one button in the shell's markup and silently captured every button
// the editor appends into the bar.
//
// This block reads every bar button at REST, before anything is opened: amber
// on an opened handle is correct (it means "here"), and reading the resting
// colour after a click would be reading the wrong state.

{
  const bar = await styleAll('.gly-bar button', 'font-size');
  check(
    'every bar control is chrome-sized',
    bar.length > 0 && bar.every((b) => b['font-size'] === '12px'),
    bar,
  );

  // `.gly-bar button`, not `.gly-census button`: the name says "bar control",
  // and the census strip is two of the six buttons in the bar (three of seven
  // before ✗ all retired). The other
  // four — collapse, hold, mode and revise — were 14px in the accent too, and
  // reading only the census would have left them unasserted by the very check
  // written for the task that fixed them.
  const resting = await styleAll('.gly-bar button', 'color');
  const accent = await rgb('--gly-accent');
  check(
    'no resting bar control wears amber',
    resting.length > 0 && resting.every((c) => c.color !== accent),
    resting,
  );

  // And the shell's fallback claims only the markup it actually ships. A
  // button that exists only once the bundle has run cannot need a bundle-less
  // fallback, so the correct scope is exactly `#gly-revise`.
  const shell = await shellSelectors();
  // `.includes` on each selector, not on the list: the shell wraps the id in
  // `:where()` so the fallback carries zero specificity and editor.css's own
  // `.gly-bar button` wins whenever the bundle is there. That the wrapping
  // WORKED is asserted by the 12px check above — #gly-revise is a
  // `.gly-bar button` too, and a bare id selector would have held it at 14px.
  check(
    "the shell's fallback still covers its own static button",
    shell !== null && shell.some((s) => s.includes('#gly-revise')),
    shell,
  );
  check(
    'the shell no longer claims the buttons it never met',
    shell !== null && !shell.some((s) => s.startsWith('.gly-bar button')),
    shell && shell.filter((s) => s.startsWith('.gly-bar button')),
  );
}

// --- §1c · a disabled census verb reads at the bar's weight, not its own
//           stale opacity -----------------------------------------------------
//
// The census's verbs are among the `.gly-bar button`s §1b already covers, and
// `.gly-bar button[disabled]` (0,2,1) already gives every one of them
// `opacity: 0.6`, the muted colour and `cursor: default`. `.gly-census
// button[disabled]` used to tie it at the same specificity and win on source
// order, painting the census's own verbs at 0.4 instead — 1.8:1 against the
// page, on the one strip an empty document shows first.
//
// READ OFF `.gly-census-count`, WHICH IS THE CENSUS'S VERB NOW. It was `✓ all`,
// the sweep — a control `makeCensus` still builds and no longer appends,
// because there are no proposals to accept in bulk. The claim was never about
// that particular button: it is that a control inside the census strip is
// painted by the BAR's disabled rule and not by a stale one of the strip's own,
// and `.gly-census-count` is a `<button>` in the same strip with the same two
// rules over it. So it is re-pointed rather than deleted.
//
// This fixture's document has pending instructions, so the count renders
// ENABLED — disabling it is the only way to measure the disabled state at all,
// the same move Task 4 made reverting a rule to prove the destroy-weight check
// discriminated. Forced with `.disabled = true` directly on the element and
// restored immediately after reading it back, so nothing later in this file
// inherits a dead door.
{
  // Confirmed against the live DOM, not the stylesheet: `closest` walks
  // parentage as rendered, independent of anything either CSS rule claims.
  const nested = await page.evaluate(
    () => !!document.querySelector('.gly-census-count')?.closest('.gly-bar'),
  );
  check(
    'the census root renders inside .gly-bar, so .gly-bar button[disabled] reaches it',
    nested,
    { nested },
  );

  const disabled = await page.evaluate(() => {
    const btn = document.querySelector('.gly-census-count');
    btn.disabled = true;
    const cs = getComputedStyle(btn);
    const out = { opacity: cs.opacity, color: cs.color, cursor: cs.cursor };
    btn.disabled = false;
    return out;
  });
  const muted = await rgb('--gly-muted');
  check(
    "a disabled census verb paints at .gly-bar button[disabled]'s 0.6 (not a stale 0.4)",
    disabled.opacity === '0.6' &&
      disabled.color === muted &&
      disabled.cursor === 'default',
    { disabled, want: { opacity: '0.6', color: muted, cursor: 'default' } },
  );
}

// --- §1d · the bar has ONE readout ------------------------------------------
//
// COURT READ THREE FRAGMENTS OFF THE BAR: `your edits…`, `connected …`,
// `revision r…`. Three readouts stood side by side between the census strip
// and the one flexible cell — the editor's `#gly-editor-status`
// (`connected · saved …`), `.gly-census-untracked` (the standing sentence) and
// the shell's `#gly-status` (the reply to a press) — and all three carry the
// same `flex: 100 1 0` yield, so on any bar that is not enormous they shorten
// together and none of them finishes a sentence. That is the rail's
// incoherence one surface over: several things doing one job, and the answer is
// the same one.
//
// The claim is COUNTABLE, which is why it is a check rather than a taste. The
// region between the census and the spacer is exactly the READOUT region — the
// bar's own ordering rule says so (edit.html: controls, readouts, the one
// flexible cell, controls) — so "one readout" is "one element in there".
//
// And it must carry the whole line, or consolidating would have been deletion
// wearing a better name: the session's state AND the standing sentence, out of
// one element, from the first paint.
console.log('\n--- §1d · the bar has one readout ---');
{
  const region = await page.evaluate(() => {
    const bar = document.querySelector('.gly-bar');
    if (!bar) return null;
    const kids = Array.from(bar.children);
    const from = kids.findIndex((el) => el.classList.contains('gly-census'));
    const to = kids.findIndex((el) => el.classList.contains('gly-spacer'));
    if (from < 0 || to < 0) return null;
    return {
      from,
      to,
      // RENDERED ONLY. The seal's terminal readout is built into this region
      // too and sits `display: none` until a verdict lands — it is the bar's
      // readout on a SEALED page, where `#gly-status` carries only a reopen's
      // reason (§8 reads that state). This block is about the LIVE bar, which
      // is the one Court was looking at, so an element that is not painted is
      // not a readout the reviewer is reading.
      between: kids
        .slice(from + 1, to)
        .filter((el) => getComputedStyle(el).display !== 'none')
        .map((el) => ({
          id: el.id,
          cls: el.className,
          text: (el.textContent || '').trim(),
          basis: getComputedStyle(el).flexBasis,
        })),
    };
  });
  check(
    'the bar orders itself census · readout · flexible cell, so the region is the region',
    !!region && region.to > region.from,
    region,
  );
  // THE WHOLE ORDER, PRINTED. A NOTE and not a check: the ordering RULE is the
  // three checks around this one (readouts before the flexible cell, controls
  // after), and the order WITHIN the control group is not a rule — it is a
  // consequence of construction order, since `makeMode` inserts both the mode
  // toggle and hold before `#gly-revise`. It is printed because three
  // documents got the tail wrong in the same way and each reader copied the
  // last one; a run of this gate now says what the DOM actually is.
  note(
    'the bar, in DOM order',
    await page.evaluate(() =>
      Array.from(document.querySelector('.gly-bar').children).map(
        (el) => el.id || el.className.split(' ')[0] || el.tagName.toLowerCase(),
      ),
    ),
  );
  check(
    'and there is exactly ONE readout in it',
    !!region && region.between.length === 1,
    region && region.between,
  );
  check(
    "it is the shell's own #gly-status — the element that exists before the bundle does",
    !!region &&
      region.between.length === 1 &&
      region.between[0].id === 'gly-status',
    region && region.between,
  );
  // NON-VACUOUS: one EMPTY cell would satisfy every line above. The readout
  // has to be saying both of the things the three cells used to say between
  // them, on a page nobody has pressed anything on yet.
  //
  // THE STANDING SENTENCE HAS MOVED, AND THIS CHECK MOVED WITH IT RATHER THAN
  // BEING DROPPED. `UNTRACKED_NOTE` was the second clause of the bar's one
  // readout — "instructions in this round" — and it is the SHEET's head now
  // (`paintSheet`, and §7b below reads it there, once). What the bar's readout
  // carries instead is the round and the draft's state: `round 1 · draft ·
  // saved just now`, measured. The claim is unchanged in shape — one element,
  // saying more than one thing, from the first paint, on a page nobody has
  // pressed anything on — so it is asserted against what that element actually
  // says. The alternative was to keep asserting a sentence that lives
  // elsewhere, which is a check that can only fail, or to drop the clause,
  // which leaves one EMPTY cell satisfying every other line in this block.
  const line = region && region.between[0] ? region.between[0].text : '';
  check(
    "and it is saying the round and the session's state, in one line",
    /round \d+/.test(line) && /(connected|connecting|saved|draft)/.test(line),
    line,
  );
  // The yield is the readout's and nothing else in the region can be starved,
  // because there is nothing else in the region.
  check(
    'the one readout is the yielding cell — basis 0, so no text in it can fold the bar',
    !!region &&
      region.between.length === 1 &&
      region.between[0].basis === '0px',
    region && region.between,
  );
}

// --- §4 · the destroy weight ------------------------------------------------
//
// Three claims, and they fail together today for one reason: `.gly-card button`
// beats `.gly-thread-delete` on specificity, so the borderless and the muted
// are both discarded and delete renders as a pill of equal weight to resolve.

{
  // styleAll, not style: "the destroy weight renders at rest" is a claim about
  // every thread card in the rail, and reading only the first match is how a
  // regression in the second or third card hides behind a passing gate.
  const dels = await styleAll(
    '.gly-rail .gly-thread-delete',
    'border-top-color',
    'color',
    'margin-left',
  );
  const muted = await rgb('--gly-muted');
  check(
    'delete is borderless at rest',
    dels.length > 0 && dels.every((d) => d['border-top-color'] === TRANSPARENT),
    dels,
  );
  check(
    'delete is muted at rest',
    dels.length > 0 && dels.every((d) => d.color === muted),
    {
      got: dels.map((d) => d.color),
      want: muted,
    },
  );
  check(
    'delete stands 2rem clear of resolve',
    dels.length > 0 && dels.every((d) => parseFloat(d['margin-left']) >= 32),
    dels,
  );
}

// The armed label must not move the button under the cursor: the second click
// has to land where the first one did, and this is the one control where
// getting that wrong is irreversible outside git.
{
  const before = await page
    .locator('.gly-rail .gly-thread-delete')
    .first()
    .boundingBox();
  await page.locator('.gly-rail .gly-thread-delete').first().click();
  await page.waitForTimeout(400);
  const after = await page
    .locator('.gly-rail .gly-thread-delete')
    .first()
    .boundingBox();
  check(
    'delete keeps its box when it arms',
    before &&
      after &&
      Math.abs(before.width - after.width) < 0.5 &&
      Math.abs(before.x - after.x) < 0.5,
    { before, after },
  );
  const armed = await style('.gly-rail .gly-thread-delete.gly-armed', 'color');
  check(
    'armed delete is the one resting-adjacent red',
    armed && armed.color === (await rgb('--gly-del')),
    armed,
  );

  // AND IT DISARMS ON SCREEN, which it did not. `DELETE_ARM_MS` is 4000 and the
  // state really did expire; the PAINT did not follow it. The click handler
  // repainted the rail (correctly — a real mouse click blurs the editor first
  // and detaches the card the handler is holding), so the timeout's closure was
  // left holding a button nobody can see, and its guarded repaint was
  // `lapsed = armedNow()` read AT exactly 4000ms, which is false by
  // construction. Dead code, and a button reading `delete?` forever.
  //
  // Measured red against the tracked build: at +5s the label was still
  // `delete?`, `.gly-armed` still on the button, and the armed note still under
  // the card. Read from the SCREEN — the label the reviewer sees and the note
  // beside it — rather than from `app.armedDelete`, which was already correct
  // through the whole defect and is exactly the internal a check here must not
  // believe.
  await page.waitForTimeout(4600);
  const disarmed = await page.evaluate(() => {
    const b = document.querySelector('.gly-rail .gly-thread-delete');
    if (!b) return null;
    const shown = Array.from(b.querySelectorAll('span'))
      .filter((s) => !s.classList.contains('gly-reserved'))
      .map((s) => s.textContent);
    const card = b.closest('.gly-card');
    return {
      shown,
      armedClass: b.classList.contains('gly-armed'),
      note: card ? card.querySelector('.gly-card-note')?.textContent || '' : '',
      state: window.galleyEdit.app.armedDelete,
    };
  });
  check(
    'the armed delete disarms ON SCREEN when its window lapses',
    disarmed &&
      disarmed.shown.join('') === 'delete' &&
      disarmed.armedClass === false,
    disarmed,
  );
  check(
    'and the armed warning goes with it — the card stops asking a question nobody can answer',
    disarmed && !disarmed.note.includes('click again'),
    disarmed,
  );
  check(
    'the app state and the paint agree, which is the whole of this defect',
    disarmed && disarmed.state === null,
    disarmed,
  );

  await page.keyboard.press('Escape');
  await page.waitForTimeout(400);
}

// --- §4b · the settled list — DELETED, WITH THE POPULATION IT READ -----------
//
// §6 asks for the destroy weight in rail, panel, sheet and settled list alike,
// and this block was the settled list's coverage: it pressed `.gly-census-count`
// to open the sheet on a wide screen, opened the settled region inside it, and
// read six properties off a settled card — that it is a full card, that it
// carries its 3px kind edge, that its head speaks the chrome's mono voice, that
// its delete wears the destroy weight, that it is dimmed to §2's 0.8 rather
// than to resolve's 0.55, and that it offers `↺ reopen`, the only way back from
// a mis-clicked `✓ resolve`. Every one of them was measured red first: the card
// painted 0.55 while the stylesheet said 0.8 in two places, because
// `.gly-card.gly-resolved` (0,2,0) out-specified `.gly-settled-card` (0,1,0).
//
// NOTHING CAN SETTLE ANY MORE. `threadCard` builds one verb — delete — because
// an instruction is immutable work for the next round rather than a
// conversation to answer and settle; there is no `✓ resolve` to press, no
// `↺ reopen` to press it back, and `galley resolve` went with them. So the
// settled region is `hidden` on every run, exactly as it was before this block
// was written, and the trap it was written to escape has closed again from the
// other side: a check reading `.gly-sheet-settled-list .gly-card` would report
// zero matches forever, and `styleAll(...).every(...)` over an empty list is
// `true`. That is the pass-forever shape, and it is why this is a deletion
// rather than a re-point.
//
// AND THE DOOR IT PRESSED IS A DIFFERENT DOOR. `.gly-census-count` was made a
// button so a desktop reviewer could reach the sheet at all; it is the
// INSTRUCTIONS VIEW control now (`openInstructions`, which sets
// `sheetOpen = false` and returns the page to the document), so the press this
// block opened with does the opposite of what it was written to do. Measured:
// `{sheet: false, rail: "block", width: 1600}`.
//
// The destroy weight itself is still read on every surface that renders a card
// — §4 above over the rail, §7b over the sheet — so §6's claim is not
// unasserted; it is asserted over three surfaces instead of four, because there
// are three.

// --- §8 · the trail: the reviewer's hand paints as ghost and highlight ------
//
// The trail (2026-08-15 spec) is three new painted surfaces: the deletion
// ghost, the insertion highlight, and the changed region at the rail's foot.
// Paint is what rects cannot see, so each is read from a real browser — and
// each check below was SHOWN FAILING against the pre-trail bundle before it
// was believed (the doctrine: a check must be shown failing first).

{
  // A real reviewer edit: strike one word and type another in its place, so
  // the merged entry carries BOTH halves and every surface below exists.
  //
  // WAIT FOR THE WORD, don't assume it. The document arrives over the websocket
  // and every server-side mutation replaces the whole fragment, so "the editor
  // exists" and "the editor holds the fixture" are two facts and only the second
  // one lets this block do anything. Read as an assumption, this threw
  // `fixture: no "plain" to strike` — a gate that dies on a race rather than
  // reporting a colour, and it dies EVERY time on a loaded machine, which is
  // how it was caught. The predicate is the one the evaluate below computes, so
  // there is no interval here anybody had to guess at.
  await page.click('.ProseMirror');
  await page.waitForFunction(
    () =>
      !!window.galleyEdit?.editor?.state?.doc?.textContent?.includes('plain'),
  );
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    const doc = editor.state.doc;
    let at = null;
    doc.descendants((node, pos) => {
      if (at !== null || !node.isTextblock) {
        return at === null;
      }
      const i = node.textContent.indexOf('plain');
      if (i !== -1) {
        at = pos + 1 + i;
      }
      return false;
    });
    if (at === null) {
      throw new Error('fixture: no "plain" to strike');
    }
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + 'plain'.length });
    editor.view.focus();
  });
  await page.keyboard.press('Backspace');
  await page.keyboard.type('bare');
  await page.keyboard.press('Escape');
  await page.waitForTimeout(1500);

  // THE ONE CLAIM THIS FILE COULD NOT MAKE, AND THE PRODUCT'S CENTRAL ONE.
  // `.gly-trail-ghost` differed from `.gly-del` by `opacity: 0.75` and nothing
  // else — same colour, same wash, same solid strike — so the reviewer's own
  // applied edit and the agent's pending proposal were the same red
  // strikethrough in the same sentence. Measured on the fixture: a paragraph
  // carrying one agent substitution and one hand edit read as three pending
  // deletions.
  //
  // The two must be apart AT A GLANCE AND WITHOUT A LEGEND, so this reads them
  // both, in one place, and requires them to differ in COLOUR and in SHAPE.
  // Against the tracked build it reports the pair equal on both.
  //
  // Both marks must EXIST for this to mean anything: the fixture's replaces
  // put a `.gly-del` in the prose and the strike above puts a ghost beside it,
  // and a null on either side is a failure rather than a vacuous pass — the
  // null-tolerant guard is the exact shape CLAUDE.md records six of.
  // THE SECOND POPULATION IS NOT IN THE PROSE ANY MORE, AND THE FOUR CHECKS
  // THAT COMPARED THEM ARE DELETED.
  //
  // They read `.gly-trail-ghost` and `.gly-del` from the SAME paragraph and
  // required them to differ in colour, in wash and in shape — because the two
  // had differed by `opacity: 0.75` and nothing else, so the reviewer's own
  // applied edit and the agent's pending proposal were the same red
  // strikethrough in the same sentence, and one paragraph read as three pending
  // deletions.
  //
  // There are no agent marks in the prose. The agent does not propose against
  // the document; it edits the file, its edits arrive as ordinary text, and the
  // before/after lives in History's own paper. `.gly-del` in the live prose has
  // no writer, so `style('.ProseMirror .gly-del', …)` is `null` on every run and
  // every one of those inequalities would be comparing against nothing — which
  // is the null-tolerant guard CLAUDE.md records six of, arriving from the far
  // side. The pair check ("both populations are on screen to be told apart")
  // was the guard against exactly this and it is the one that went red first,
  // which is the guard working.
  //
  // WHAT SURVIVES IS THE GHOST'S OWN VOCABULARY, stated positively — the record
  // grey, the strike, the aria-hidden, and the dotted rule under inserted text —
  // because those are claims about what the reviewer sees rather than about a
  // contrast with something that is not there.
  const ghost = await style(
    '.ProseMirror .gly-trail-ghost',
    'text-decoration-line',
    'text-decoration-style',
    'color',
    'background-color',
    'user-select',
  );
  check(
    "the reviewer's ghost is on screen at all, so what follows is not read off null",
    ghost !== null,
    { ghost },
  );
  note(
    'and no agent mark is in the prose beside it — the agent edits the file',
    await page.evaluate(() => ({
      del: document.querySelectorAll('.ProseMirror .gly-del').length,
      ins: document.querySelectorAll('.ProseMirror .gly-ins').length,
    })),
  );
  // ONE REMOVAL VOCABULARY, AND THE TWO POPULATIONS ARE APART BY SHAPE.
  //
  // This asserted the ghost was `--gly-muted` — the record grey — under
  // CLAUDE.md's "RED IS RESERVED FOR WAITING ON YOU". That entry is SUPERSEDED
  // on the colour and standing on the shape (2026-08-20), and the argument is
  // worth carrying here because the reversal reads as a regression otherwise:
  // the grey existed to keep the reviewer's own edits apart from the agent's
  // undecided PROPOSALS interleaved in the same prose, so that a red strike was
  // genuinely ambiguous about whether it was asking the reviewer for something.
  // The agent's changes are applied now. There are no pending deletions in the
  // draft to be confused with, the only red strike in the prose is the
  // reviewer's own, and a colour reserved against a population that does not
  // exist is a colour spent on nothing — while grey cost the ghost the one
  // thing every reader already knows, which is that removed text is red.
  //
  // So: colour says WHAT HAPPENED to the text, shape says WHETHER IT HAS
  // HAPPENED YET. The outgoing ghost is `--gly-del`, DOTTED, with no wash; a
  // settled diff in History is `--gly-del`, SOLID, on the del wash — and that
  // contrast is asserted in §12, where a real diff is on screen to be read
  // against this one. Here is the ghost's own half, all three properties
  // together, because any one of them alone is satisfied by the design this
  // check used to state.
  check(
    'the ghost is red — there is one vocabulary for removal, and this is it',
    ghost !== null && ghost.color === (await rgb('--gly-del')),
    { ghost, want: await rgb('--gly-del') },
  );
  check(
    'and it is DOTTED and unwashed — shape is what says it has not happened yet',
    ghost !== null &&
      ghost['text-decoration-style'] === 'dotted' &&
      ghost['background-color'] === TRANSPARENT,
    ghost,
  );
  check(
    'it is still struck through — a deletion still reads as one',
    ghost !== null && ghost['text-decoration-line'].includes('line-through'),
    ghost,
  );
  check(
    'and the ghost is aria-hidden — a readout, never content',
    await page.evaluate(() => {
      const el = document.querySelector('.ProseMirror .gly-trail-ghost');
      return !!el && el.getAttribute('aria-hidden') === 'true';
    }),
  );
  const trailIns = await style(
    '.ProseMirror .gly-trail-ins',
    'background-color',
    'border-bottom-color',
    'border-bottom-style',
  );
  // The ins half of the same deletion, for the same reason: `.gly-ins` has no
  // writer in the prose either, so the inequality that used to stand here
  // ("the reviewer's own typing does not wear the agent's ins wash") had
  // nothing on its right-hand side. The positive claim is what is left.
  check(
    "the reviewer's own typing is marked in the prose at all",
    trailIns !== null,
    { trailIns },
  );
  check(
    'it is marked by a record rule instead',
    trailIns !== null &&
      trailIns['border-bottom-style'] === 'dotted' &&
      trailIns['border-bottom-color'] === (await rgb('--gly-muted')),
    { trailIns, want: await rgb('--gly-muted') },
  );

  // THE LOG IS GONE FROM EVERY SURFACE, AND WHAT IT PAINTED IS ASSERTED AS AN
  // ABSENCE. Two checks stood here and each is answered rather than dropped:
  //
  //   `the changed region's head paints as chrome — muted, 11px, clickable`.
  //   Its subject was a disclosure at the rail's foot. The trail is an outgoing
  //   message to the agent, not a history — nobody browses it, so it has no
  //   head to paint. The chrome-voice claim it was one of several instances of
  //   is still asserted on `.gly-settled-head` in §4b.
  //
  //   `a log row quotes old→new in the prose's own red/green`. This was the ONE
  //   declared exemption to "red is reserved for waiting on you": red was legal
  //   in those rows because they sat under a head that said whose hand they
  //   were. With no rows there is nothing to excuse, and the vocabulary itself
  //   is still read on the replace card (§5), which is where the reviewer
  //   learned it. The exemption's own premise — that the GHOST must not borrow
  //   that red — is the inequality asserted directly above, which is the check
  //   that matters and the one that was missing for a whole phase.
  //
  // Asserted here rather than left to probe's bundle strings, because a region
  // can be re-added in the source and a string check only sees the artifact.
  //
  // THE ROUND'S CHANGE CARDS ARE NOT THE LOG, and this check said they were.
  // It counted `.gly-change`, which is also the class of `changeCard`: the
  // reviewer's own edit as it will reach the agent, in `.gly-rail-changes`
  // with its one verb, revert (cards.ts, "THE OTHER HALF OF THE ROUND";
  // rounds-ux drives it). That card is in the round on purpose, so the hand
  // edit made above puts exactly one there, and the old check went red on a
  // page doing what it should. What is gone is the LOG's markup, and that is
  // what is counted now; the change card is asserted as present.
  const logged = await page.evaluate(() => ({
    railChanged: document.querySelectorAll(
      '.gly-rail-changed, .gly-changed-head',
    ).length,
    sheetChanged: document.querySelectorAll(
      '.gly-sheet-changed, .gly-changed-list',
    ).length,
    rows: document.querySelectorAll('.gly-change-adrift').length,
    roundCards: document.querySelectorAll('.gly-rail-changes .gly-change')
      .length,
    strayCards: document.querySelectorAll(
      '.gly-change:not(.gly-rail-changes .gly-change)',
    ).length,
  }));
  check(
    'and the reviewer\u2019s hand is logged on no surface — no log, no log row',
    logged.railChanged === 0 && logged.sheetChanged === 0 && logged.rows === 0,
    logged,
  );
  check(
    'and the hand edit is in the round as a change card, and nowhere else',
    logged.roundCards >= 1 && logged.strayCards === 0,
    logged,
  );

  // THE TRAIL CLAUSE ON THE VERDICT BUTTON IS GONE, AND FIVE CHECKS GO WITH IT.
  //
  // They read `.gly-revise-trail` for `n edits, m replies`, asserted that the
  // count was the hand edit just made rather than zero, and then measured the
  // SEPARATOR as paint — a Range over the one leading space, because
  // `.gly-revise-trail` is `display: inline-block` and an inline-block starts
  // its own line box, where leading collapsible white space is REMOVED. Every
  // string gate over that label was green while the button printed
  // `Finish ▾· 3 edits, 3 replies`, and this was the pass that could see it.
  //
  // `paintRevise` writes `''` into the element unconditionally, on every tick.
  // The counts came from `outgoingCounts`, which reduces `view.changes` and
  // `view.comments` — two fields the pending payload stopped carrying when it
  // became a list of instructions — so the clause is empty in every reachable
  // state. `printed` returns `null` (there is no text node to take a Range
  // over) and all three separator checks reported `— null`, which is a gate
  // reading nothing and saying so. Printed as a note instead: the number is on
  // the record and outside the pass/fail counters, which is CLAUDE.md's rule
  // for a surface that genuinely cannot be asserted.
  note(
    'the trail clause on the verdict button',
    await page.evaluate(() => ({
      label: (document.querySelector('#gly-revise')?.textContent || '').trim(),
      clause: document.querySelector('.gly-revise-trail')?.textContent || '',
    })),
  );

  // --- STRUCK AND INSERTED ARE TWO WORDS, AND TWO WORDS DO NOT TOUCH --------
  //
  // Measured in a real browser on the shipped build: `alphabeta`, `gammadelta`,
  // `keptleft`, `nothingnobody` — a deletion and the text that replaces it
  // rendered with 0px between them, so each pair reads as one malformed
  // compound. Both populations have it and neither is a diff bug:
  //
  //   THE AGENT'S. The file holds `{~~alpha~>beta~~}` — ONE CriticMarkup span
  //   at ONE position, which is the whole of "one span in the file is one
  //   decision everywhere". There is no space between the halves in the
  //   document and there must never be one: accept writes `beta` into the
  //   author's prose and reject writes `alpha`, and a space put there to make
  //   the pair legible would survive both.
  //
  //   THE REVIEWER'S. The ghost is a WIDGET decoration — ProseMirror's own
  //   "this is not in the document" — drawn at the position the new word now
  //   occupies. `trimAffixes` narrows the stored entry to the changed span, and
  //   on the fixture's own edit that span is `kep`→`lef`: strictly INSIDE one
  //   word, with no space at either end to have been trimmed. The gap is not
  //   something the diff lost; it is a box the document does not contain,
  //   needing its own separation from the text it was inserted before.
  //
  // So the gap belongs to the mark's own box in both cases, and it is read here
  // as GEOMETRY against the width of a real space in the same prose — the one
  // measurement that says "these read as two words" rather than "some rule
  // declares some margin".
  const spacing = await page.evaluate(() => {
    const gap = (a, b) =>
      +(
        b.getBoundingClientRect().left - a.getBoundingClientRect().right
      ).toFixed(2);
    const agent = [];
    for (const del of document.querySelectorAll('.ProseMirror .gly-del')) {
      const next = del.nextElementSibling;
      if (!next || !next.classList.contains('gly-ins')) continue;
      agent.push({
        reads: del.textContent + next.textContent,
        gap: gap(del, next),
      });
    }
    const hand = [];
    for (const ghost of document.querySelectorAll(
      '.ProseMirror .gly-trail-ghost',
    )) {
      const next = ghost.nextElementSibling;
      if (!next || !next.classList.contains('gly-trail-ins')) continue;
      hand.push({
        reads: ghost.textContent + next.textContent,
        gap: gap(ghost, next),
      });
    }
    // A SPACE IN THE SAME PROSE, at the same size and in the same family, so
    // the bound is the reader's own and not a number somebody picked.
    let space = null;
    for (const p of document.querySelectorAll('.ProseMirror p')) {
      const t = p.firstChild;
      if (!t || t.nodeType !== 3) continue;
      const i = t.data.indexOf(' ');
      if (i === -1) continue;
      const r = document.createRange();
      r.setStart(t, i);
      r.setEnd(t, i + 1);
      space = +r.getBoundingClientRect().width.toFixed(2);
      break;
    }
    return { agent, hand, space };
  });
  // ONE POPULATION, FOR THE REASON THE FOUR CHECKS ABOVE WERE DELETED: there
  // are no `.gly-del`/`.gly-ins` pairs in the prose, so `spacing.agent` is empty
  // on every run and `[].every(...)` is `true` — the agent half of this claim
  // would report `ok` about nothing at all. The reviewer's own ghost-beside-its
  // -replacement is a real pair and is asserted; the agent's is named as absent
  // in the same breath, so the deletion is on the record and not a silence.
  check(
    'the reviewer’s ghost and its replacement are both rendered, so the gap is a real gap',
    spacing.hand.length > 0 && spacing.space > 0,
    spacing,
  );
  check(
    'and they read as two words, not one',
    spacing.hand.every((h) => h.gap >= spacing.space),
    spacing,
  );
  note(
    'agent substitution pairs in the prose (none: the agent edits the file)',
    spacing.agent.length,
  );

  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(300);
}

// --- §11 · clicking marked-up text, at the width the rail exists at ----------
//
// THE MOST NATURAL GESTURE IN THE PRODUCT, AND ITS WORST SURFACE. Everything
// this file already asserts about the bubble is asserted at 900px, where the
// bubble carries the whole conversation — and the two defects below only exist
// ABOVE the breakpoint, where the rail is on screen and `threadFor` used to
// answer null. Neither could be seen from the narrow block, which is the
// fixture hazard in its usual clothes: a check that cannot reach a state is not
// a check of it.
//
// Two clicks, measured against a real server:
//
//   a COMMENT highlight opened `COMMENT · AGENT · JUST NOW` with `✓ accept` /
//   `✗ reject` and nothing else — no comment text, no conversation, no reply
//   box — and accept posted `{"run":"…","id":"c2"}` for a 404 the bubble then
//   reported as "that suggestion has moved — reopen it";
//
//   the GREEN half of a substitution opened `INSERTION · AGENT · JUST NOW`,
//   "the server has not seen this one yet", and no verbs at all, about `s1`,
//   one of the pending, whose card was on screen with a working ✓.
{
  await page.evaluate(() => window.scrollTo(0, 0));
  const railOn = await page.evaluate(
    () => window.galleyEdit.app.surfaces().rail,
  );
  check(
    '§11 runs at a width where the rail is carrying conversations',
    railOn === true,
  );

  // --- the comment highlight ---
  const hl = page.locator('.ProseMirror .gly-hl').first();
  check('the fixture has a comment highlight to click', (await hl.count()) > 0);
  await hl.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  await hl.click();
  await page.waitForTimeout(400);
  const onComment = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    const flashed = document.querySelector('.gly-rail .gly-thread.gly-flash');
    return {
      bubbleOpen: !!el && !el.hidden,
      verbs: el
        ? el.querySelectorAll('.gly-bubble-accept, .gly-bubble-reject').length
        : 0,
      text: el && !el.hidden ? el.textContent : '',
      flashedKey: flashed ? flashed.dataset.key : null,
    };
  });
  // ONE CONVERSATION, ONE PLACE. The rail is already drawing this thread, so a
  // second copy in a bubble would be two reply boxes and two deletes for one
  // conversation. What the click does instead is take the reviewer to the card
  // — reveal(), in the direction the card already offers.
  check(
    'clicking a comment highlight does not open a second copy of its conversation',
    onComment.bubbleOpen === false,
    onComment,
  );
  check(
    'it takes the reviewer to the card the rail is carrying',
    onComment.flashedKey !== null,
    onComment,
  );
  // THE ASSERTION THE AUDIT ASKED FOR, and it holds however the surface
  // question is answered: no comment bubble anywhere carries a decide verb.
  check(
    'no comment bubble carries an accept or a reject',
    onComment.verbs === 0,
    onComment,
  );

  // --- the green half of a substitution — DELETED WITH THE SUBSTITUTION ---
  //
  // Ten checks stood here and they were about one defect: clicking the GREEN
  // half of a replace opened a bubble that read `INSERTION` beside a card
  // saying REPLACE, diagnosed the span as something the server had never heard
  // of, and posted the clicked mark's run — an id the server answers 404 to —
  // instead of the decision's own, the deleted half's. The pass intercepted
  // `POST /_galley/accept` and read what was posted, because that is where the
  // defect actually landed.
  //
  // A replace is a PROPOSAL and there are none: `galley suggest --replace` is
  // gone, `.gly-ins` has no writer in the prose, `app.suggestions` is empty on
  // every payload, and there is no accept to intercept. Every line of it would
  // have read off null — `green.count()` is 0, `wanted` is null, `posted` stays
  // null — so the two guards at the top ("the fixture has the added half of a
  // substitution to click", "the server reports the substitution as ONE
  // decidable replace") are the checks that went red, which is those guards
  // doing exactly what they were put there for. The rest is deleted rather than
  // pointed at some other span, because there is no other span that carries a
  // decision.

  await page.keyboard.press('Escape');
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(400);
}

// --- §3 · a conversation lives on a card, in the margin ----------------------
//
// The panel is chrome that must hold a conversation, and the law is that it
// does not become the margin — it summons a piece of it. So the head stays
// chrome and everything under it is a full card from §2's deck.

// THERE IS NO HANDLE IN THE RAIL ANY MORE, AND THAT IS THE RULING THIS BLOCK
// WAS WAITING FOR.
//
// Its history: `.gly-census-overall` was a bar control opening a panel that
// floated off the bar's measured height; then the whole-document card became
// the FIRST ITEM OF THE INSTRUCTION RAIL (`.gly-overall-rail`), opened by its
// own `.gly-overall-toggle`, and this press was re-pointed at that. The press
// is DELETED now: the rail holds live work only — *here is what needs you,
// beside the text it is about* — and a `+ add` button needs nothing and is
// beside nothing, so the toggle was chrome standing in the work column. Its
// placement caused both of the defects Court reported from live use (it
// scrolled out of reach; opening it slid every anchored card 39.29px off its
// mark), and no better place in the rail existed to move it to.
//
// The cards it used to disclose are simply THERE — filed whole-document
// instructions are live work and were never the thing that had to leave — so
// "the panel does not become the margin, it summons a piece of it" is checked
// below on a list that needs no opening.
await page.waitForSelector('.gly-overall-entries .gly-card');
await page.waitForTimeout(300);

{
  // THE THREE FLOAT CHECKS STAND, AND WHAT THEY ARE ABOUT HAS NARROWED.
  //
  // They were written against a floating panel, inverted when the panel became
  // a card of the rail, and kept in the new direction *"so a future re-float is
  // a red check rather than a discovery"*. That is exactly what happened: a fix
  // for the 39.29px slide was measured green, tripped this and rounds-ux.mjs,
  // and was BACKED OUT. THE RULING IS THAT THE SPLIT WAS THE ANSWER — the box
  // a reviewer TYPES IN floats (see the `.gly-capture` block below, which is
  // the new contract stated in full), and the instructions already FILED stay
  // in the rail's own flow, because they are the map. So `.gly-overall` is
  // still `static`, still unpainted, still claiming no stacking context, and it
  // now means only the list.
  const panel = await style(
    '.gly-overall',
    'background-color',
    'box-shadow',
    'z-index',
    'position',
  );
  check(
    'the filed whole-document instructions are IN the rail, not floating over it',
    panel && panel.position === 'static' && panel['box-shadow'] === 'none',
    panel,
  );
  check(
    'and they carry no ground of their own — the rail is what they sit on',
    panel && panel['background-color'] === TRANSPARENT,
    panel,
  );
  check(
    'so they claim no stacking context above the map they are part of',
    panel && panel['z-index'] === 'auto',
    panel,
  );
  // AND NO CAPTURE CONTROL IS LEFT IN THE COLUMN. Stated as its own check, in
  // the place the old press stood, so putting a `+ add` back into the rail is a
  // red check rather than a rediscovery of the same two defects.
  const railChrome = await page.evaluate(() => ({
    toggle: document.querySelectorAll('.gly-rail .gly-overall-toggle').length,
    door: document.querySelectorAll('.gly-rail .gly-capture-open').length,
    bar: document.querySelectorAll('.gly-bar .gly-capture-open').length,
  }));
  check(
    'capture is chrome — the door is in the bar and the rail holds none of it',
    railChrome.toggle === 0 && railChrome.door === 0 && railChrome.bar === 1,
    railChrome,
  );

  // --- THE CAPTURE CARD: A CARD IN THE FLOW, NOT A DISCLOSURE OVER IT ---
  //
  // The box the reviewer types a whole-document instruction into is a card in
  // the whole-document panel's flow now, and every property below is one clause
  // of that sentence. It is read here rather than as a new section because this
  // is where the whole-document surface's paint has always been read, and one
  // instrument driven through the states in the order the product reaches them
  // is this file's own discipline. The slide an in-flow composer used to cause
  // is answered in the script (openCapture calls scheduleAnchors), which
  // rounds-ux.mjs measures; here it is the paint that is read.
  await page.locator('.gly-bar .gly-capture-open').click();
  await page.waitForSelector('.gly-capture:not([hidden])');
  await page.waitForTimeout(400);
  const capture = await style('.gly-capture', 'position', 'z-index');
  check(
    'the capture card is in the rail’s flow — it sits with the cards, not over them',
    capture && capture.position === 'static' && capture['z-index'] === 'auto',
    { capture },
  );
  // A CARD'S GROUND, AND NO SHADOW. It used to float over live cards and wore
  // the one shadow in the rail to say so; it is part of the map now, so it
  // wears the card ground and drops the shadow — the property that said it was
  // over the others is exactly the one that would now lie.
  const captureGround = await style(
    '.gly-capture',
    'background-color',
    'box-shadow',
  );
  const card = await rgb('--gly-card');
  check(
    'and it wears the card ground with no shadow — it is part of the map',
    captureGround &&
      captureGround['background-color'] === card &&
      captureGround['box-shadow'] === 'none',
    { captureGround, card },
  );
  await page.keyboard.press('Escape');
  await page.waitForTimeout(300);

  // --- THE RIGHT-CLICK MENU, AS PAINT ---
  //
  // ONE CLAIM ONLY, AND IT IS THIS FILE'S. `contextmenu` was unclaimed before
  // this, so the menu arrives with two things that have to be true: it must
  // never be a child of `.ProseMirror` (a surface appended into the editable
  // subtree is parsed as prose and written to the author's file — a fixture
  // went from 1 pending suggestion to 81), and it must be drawn over the chrome
  // it was summoned from. The FIRST is a DOM-structure fact and belongs where
  // behaviour is read: web/rounds-ux.mjs asserts it, on the same gesture. This
  // file reads computed style, so it asks the stacking question and only that
  // — the bar is 10 and the rail is 5, and a menu drawn under either is the
  // covered-control defect arriving through a third door.
  await page.locator('.ProseMirror p').first().click({ button: 'right' });
  await page.waitForSelector('.gly-menu:not([hidden])');
  const menuPaint = await style('.gly-menu', 'position', 'z-index');
  const barZ = await style('.gly-bar', 'z-index');
  check(
    'and it is drawn over the chrome it was summoned from',
    menuPaint &&
      menuPaint.position === 'absolute' &&
      barZ &&
      Number(menuPaint['z-index']) > Number(barZ['z-index']),
    { menuPaint, barZ },
  );
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
}

{
  const cards = await styleAll(
    '.gly-overall-entries .gly-card',
    'border-left-width',
    'border-top-width',
  );
  check(
    'the panel renders full cards, not a bare variant',
    cards.length > 0 &&
      cards.every((c) => parseFloat(c['border-top-width']) > 0),
    cards,
  );
  check(
    'every panel card carries its 3px kind edge',
    cards.length > 0 &&
      cards.every((c) => parseFloat(c['border-left-width']) === 3),
    cards,
  );

  // Family, not a count — the same repair §4b's version already carries. "Mono
  // head" is a claim about the VOICE, and counting elements never reads a font:
  // the weak form passes on a head rendered in the document's own serif. The
  // comparison is against `.gly-census`'s computed family, so the claim stays
  // "the same voice as the chrome" rather than "this font list".
  const heads = await styleAll(
    '.gly-overall-entries .gly-card-head',
    'font-family',
  );
  const chrome = await style('.gly-census', 'font-family');
  check(
    'every panel card carries its mono head',
    heads.length === cards.length &&
      cards.length > 0 &&
      chrome &&
      heads.every((h) => h['font-family'] === chrome['font-family']),
    { heads, cards: cards.length, chrome },
  );

  // styleAll, not style: same reasoning as the rail's destroy-weight check
  // above — the panel can hold more than one thread, and the claim is that the
  // destroy weight renders correctly on all of them, not just the first.
  //
  // ALL THREE PROPERTIES, because §6's destroy weight IS the three together:
  // borderless, muted, 2rem clear. Reading only the border let this surface
  // lose the muted colour and still report `ok`, while the rail, the settled
  // list and the bubble all read every one.
  const dels = await styleAll(
    '.gly-overall .gly-thread-delete',
    'border-top-color',
    'color',
    'margin-left',
  );
  const muted = await rgb('--gly-muted');
  check(
    'the destroy weight renders in the panel too',
    dels.length > 0 &&
      dels.every(
        (d) =>
          d['border-top-color'] === TRANSPARENT &&
          d.color === muted &&
          parseFloat(d['margin-left']) >= 32,
      ),
    { dels, muted },
  );
}

// --- §5 · the substitution card — DELETED WITH THE SUBSTITUTION -------------
// --- §5b · a replace card is still a card in the band — DELETED WITH IT ------
//
// §5 read the replace card's paint: no `border-image` (a gradient border was
// the mockup's, and it painted OVER the kind edge), the top/right/bottom
// borders the ordinary line, and a left edge that is one 3px rule carrying BOTH
// the deletion and the insertion — the two-colour edge that says at a glance
// that a substitution is one decision and not two. Then it read the body,
// which has to QUOTE the document in the order the change reads: struck text
// first, replacement after it.
//
// §5b was the placement half. Two `--replace`s were in the fixture on purpose,
// because a layout bug that takes replace cards out of the band's absolute
// placement puts the FIRST one at very nearly the right place anyway — it is
// the second and third that land 92px and 185px below their marks — so one
// replace in the fixture was a gate that could not see displacement at all.
//
// There is no replace. `galley suggest --replace` is gone, nothing in the
// product proposes a substitution against the document, and `.gly-card-replace`
// has no writer. Every read here returned `null` and every `styleAll(...)`
// returned `[]`, which is why the fixture guard §5b opens with ("enough replace
// cards to see displacement at all") is the line that went red — the guard
// working, one more time.
//
// THE PLACEMENT CLAIM ITSELF IS NOT UNASSERTED. "Every card in the band is
// placed by the band, not by normal flow, and measures the same as every other
// card in it" is §9's question and §9 asks it over the cards that exist. What
// is gone is the two-colour edge and the old→new body, because a component with
// no data to build it from cannot be read off a screen.

// takeBack deletes an instruction a section filed for its own check, and waits
// for the page to have heard, so the sections after it count what they always
// counted.
const takeBack = (key) =>
  page.evaluate(async (k) => {
    await fetch('/_galley/instruction/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: k }),
    });
    await window.galleyEdit.app.refreshPending();
  }, key);

// --- §1e · a block note's words are a widget, painted by ID -----------------
//
// A block comment's note carries only its ID; the amber box gets the words from
// the instruction data, as a WIDGET DECORATION inside the note (note.ts's
// noteWordDecorations). Paint is what this file is for: the widget has to keep
// the comment's line breaks and wrap a long token, and only a real browser's
// computed style can say it does.
//
// FILED THROUGH THE COMPOSER, the reviewer's own path: the grip beside the
// title, then the form it opens. The note is then found by its ID — the
// pending instruction's key — and by nothing else: no text is matched, and
// nothing is written into the fragment by this check.
//
// AND IT GROWS. The comment is then edited to three lines, the way the card's
// edit box does it, and every other card is measured before and after. The
// note sits under the title, so every mark in the document is below it and
// moves down with the prose. "A click moves nothing but what was clicked" is
// the rule this is measured against; the movement is printed as a note. The comment is
// deleted at the end, so the sections after this one count what they always
// counted.
{
  const before = ((await pending()).instructions || []).length;
  await page.evaluate(() =>
    window.galleyEdit.editor.commands.setTextSelection(1),
  );
  // The title's grip is there at rest and opens the form directly.
  const grip = page.locator('.gly-block-grip[data-kind="heading"]').first();
  await grip.waitFor({ state: 'visible', timeout: 5000 });
  await grip.click();
  await page
    .locator('.gly-composer-form')
    .waitFor({ state: 'visible', timeout: 5000 });
  const FIRST = 'say who this handoff is for';
  await page.fill('.gly-composer-text', FIRST);
  await page.click('.gly-composer-send');
  await page.waitForFunction(
    (n) => (window.galleyEdit.app.comments || []).length === n,
    before + 1,
    { timeout: 10000 },
  );
  const key = ((await pending()).instructions || []).find(
    (i) => i.anchor === 'block' && i.text === FIRST,
  )?.key;
  // Every card in the band but the new one, and for the anchored ones the
  // mark it hangs on, in page coordinates.
  const measure = (id) =>
    page.evaluate(async (k) => {
      await new Promise((r) => requestAnimationFrame(() => r()));
      await new Promise((r) => setTimeout(r, 300));
      const y = (el) => el.getBoundingClientRect().top + window.scrollY;
      const aside = document.querySelector(
        `.gly-note[data-comment-id="${CSS.escape(k)}"]`,
      );
      const el = aside && aside.querySelector('.gly-note-words');
      const cs = el && getComputedStyle(el);
      const cards = [...document.querySelectorAll('.gly-rail-band .gly-card')]
        .filter((c) => c.dataset.key !== k)
        .map((c) => {
          const run = c.dataset.run;
          const mark = run
            ? document.querySelector(
                `.ProseMirror [data-run="${CSS.escape(run)}"]`,
              )
            : null;
          return {
            key: c.dataset.key || run || '',
            top: Math.round(y(c) * 10) / 10,
            mark: mark ? Math.round(y(mark) * 10) / 10 : null,
          };
        });
      const own = document.querySelector(
        `.gly-rail-band .gly-card[data-key="${CSS.escape(k)}"]`,
      );
      return {
        own: own ? Math.round(own.getBoundingClientRect().height * 10) / 10 : 0,
        notes: document.querySelectorAll(
          `.ProseMirror .gly-note[data-comment-id="${CSS.escape(k)}"]`,
        ).length,
        text: el ? el.textContent : null,
        whiteSpace: cs ? cs.whiteSpace : null,
        overflowWrap: cs ? cs.overflowWrap : null,
        height: aside ? Math.round(aside.getBoundingClientRect().height) : 0,
        cards,
      };
    }, id);
  const filed = key ? await measure(key) : null;
  check(
    'a block comment filed through the composer has an ID note in the prose, so this can fail',
    !!key && /^cb-[0-9a-f]{16}$/.test(key) && !!filed && filed.notes === 1,
    { key, filed },
  );
  check(
    "the note shows its comment's words in a widget, found by ID",
    !!filed && filed.text === FIRST,
    filed,
  );
  const GROWN = 'say who this handoff is for:\n\nthe reviewer,\nor the agent';
  await page.evaluate(
    async ({ k, text }) => {
      await fetch('/_galley/instruct', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ op: 'edit', key: k, text, author: 'court' }),
      });
      await window.galleyEdit.app.refreshPending();
    },
    { k: key || '', text: GROWN },
  );
  const grown = key ? await measure(key) : null;
  check(
    "the widget keeps the comment's line breaks and wraps a long token",
    !!grown &&
      grown.text === GROWN &&
      grown.whiteSpace === 'pre-wrap' &&
      grown.overflowWrap === 'anywhere' &&
      grown.height > filed.height,
    { filed: filed && filed.height, grown },
  );
  // Per card: how far it moved, and how far its mark moved. The note sits
  // under the title, so the prose under it moves by the note's growth. The
  // cards are STACKED in this fixture (each sits below its mark, under the card
  // above it), and the comment's own card is the top of the stack: it shows the
  // same three lines and grows with them. So a card may slide by what the
  // comment's own card grew — CLAUDE.md's recorded exemption, "a card whose
  // note slot fills grows and slides the cards below it" — and by nothing more:
  // a card that moved with the PROSE has been moved by the note.
  const ownGrew =
    grown && filed ? Math.round((grown.own - filed.own) * 10) / 10 : null;
  const moves = (filed ? filed.cards : []).map((a) => {
    const b = ((grown && grown.cards) || []).find((c) => c.key === a.key);
    return {
      key: a.key.slice(0, 8),
      card: b ? Math.round((b.top - a.top) * 10) / 10 : null,
      mark:
        b && a.mark !== null && b.mark !== null
          ? Math.round((b.mark - a.mark) * 10) / 10
          : null,
      below: b && b.mark !== null ? b.top >= b.mark - 1 : null,
    };
  });
  note('a growing note, measured: every other card, and the mark it hangs on', {
    noteGrewBy: grown && filed ? grown.height - filed.height : null,
    ownCardGrewBy: ownGrew,
    moves,
  });
  check(
    'a growing note moves no card: each slides only by its own card\u2019s growth, and none is left above its mark',
    ownGrew !== null &&
      moves.length > 0 &&
      moves.every(
        (m) =>
          m.card !== null &&
          Math.abs(m.card - ownGrew) <= 1 &&
          m.below !== false,
      ),
    { ownGrew, moves },
  );
  await takeBack(key || '');
  const gone = key ? await measure(key) : null;
  check(
    'and deleting the comment takes its note and its widget with it',
    !!gone && gone.notes === 0 && gone.text === null,
    gone,
  );
  await page.waitForFunction(
    (n) => (window.galleyEdit.app.comments || []).length === n,
    before,
    { timeout: 10000 },
  );
}

// --- §1f · a grip with instructions is painted as one ---------------------
//
// A block that carries instructions has a grip that says so in paint as well
// as in its count: the instruction's own violet on its edge and its ground.
// Read as an INEQUALITY between the same grip at rest and commented, in both
// schemes, rather than as a token match alone: a commented rule that lost the
// cascade to the resting one paints both the same, and a token check on a
// token that is itself wrong passes either way. And the resting grip borrows
// nothing from the two colours that already mean something in the prose —
// the removed-text red and the light's amber wash.
//
// ONE GRIP, READ TWICE: the title's, before and after an instruction is filed
// on it. Every grip-bearing block in this fixture but the title already
// carries one, so there is no second grip at rest to compare against.
{
  const title = await page.evaluate(
    () =>
      (window.galleyEdit.app.blocks || []).find((b) => b.kind === 'heading') ||
      null,
  );
  const grip = `.gly-block-grip[data-index="${title ? title.index : -1}"]`;
  const PAINT = ['background-color', 'border-top-color', 'color'];
  const SCHEMES = ['dark', 'light'];
  const paint = async () => {
    await page.mouse.move(0, 0);
    const out = {};
    for (const scheme of SCHEMES) {
      await page.emulateMedia({ colorScheme: scheme });
      await page.waitForTimeout(300);
      out[scheme] = {
        grip: await style(grip, ...PAINT),
        hl: await rgb('--gly-hl'),
        hlBg: await rgb('--gly-hl-bg'),
        taken: [
          await rgb('--gly-del'),
          await rgb('--gly-del-bg'),
          await rgb('--gly-lit-bg'),
        ],
      };
    }
    await page.emulateMedia({ colorScheme: 'light' });
    return out;
  };
  const resting = await paint();
  const filed = await page.evaluate(
    async (k) => {
      const r = await fetch('/_galley/instruct', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          op: 'comment_block',
          target: k,
          text: 'paint check',
          author: 'court',
        }),
      });
      await window.galleyEdit.app.refreshPending();
      return r.ok
        ? ((await r.json()).instructions || []).find(
            (i) => i.text === 'paint check',
          )?.key || ''
        : '';
    },
    title ? title.key : '',
  );
  await page
    .waitForSelector(`${grip}.is-commented`, { timeout: 10000 })
    .catch(() => {});
  const commented = await paint();
  for (const scheme of SCHEMES) {
    const rest = resting[scheme].grip;
    const lit = commented[scheme].grip;
    const { hl, hlBg, taken } = commented[scheme];
    check(
      `a commented grip differs from a resting one in ground AND edge in ${scheme}`,
      !!lit &&
        !!rest &&
        lit['background-color'] !== rest['background-color'] &&
        lit['border-top-color'] !== rest['border-top-color'],
      { scheme, lit, rest },
    );
    check(
      `and its paint is the instruction's violet in ${scheme}`,
      !!lit &&
        lit['border-top-color'] === hl &&
        lit['background-color'] === hlBg &&
        lit.color === hl,
      { scheme, lit, hl, hlBg },
    );
    check(
      `a resting grip shares no colour with removed text or the light in ${scheme}`,
      !!rest && PAINT.every((p) => !taken.includes(rest[p])),
      { scheme, rest, taken },
    );
  }
  await takeBack(filed);
  check('the paint check filed its instruction and took it back', !!filed, {
    title,
    filed,
  });
}

// --- §1 · radius is a caste mark --------------------------------------------
//
// Full pills belong to verbs on cards and to passive chips. Every chrome
// control is 6px. This is the cheapest signal in the system and the one that
// tells a reviewer, without a word, which things converse and which count.
//
// TWO checks read the census's radius, on purpose, because they answer
// different questions:
//
//   - the COMPUTED check answers "what does the user see". It used to be
//     confounded: `.gly-bar button`, edit.html's inline fallback for the
//     static #gly-revise button, tied `.gly-census button` at (0,1,1) and won
//     on source order because it lived in a later stylesheet — and it also
//     said `border-radius: 6px`, so this check passed with `.gly-census
//     button` reverted to `999px`. §1b's fix moved that rule into this
//     stylesheet ABOVE `.gly-census button`, so the census wins its own paint
//     and this check now discriminates: reverting the declaration to 999px
//     fails it. Verified that way, not assumed.
//   - the RULE check (ruleValue, defined above with the reasoning in full)
//     reads `.gly-census button`'s own declared `border-radius` off the
//     CSSOM, independent of what wins the paint. It is no longer the only
//     one that can tell 6px from 999px, and it stays because a future rule
//     that ties and out-orders the census would re-confound the paint and
//     leave this one standing.
{
  // `.gly-bar button` for the COMPUTED check, because "every chrome control" is
  // all six of them and the census strip is two. The RULE check below stays
  // on `.gly-census button` — it is a question about that one rule's own
  // declaration, not about what the bar paints.
  const chrome = await styleAll('.gly-bar button', 'border-radius');
  check(
    'every chrome control is 6px (computed)',
    chrome.length > 0 && chrome.every((c) => c['border-radius'] === '6px'),
    chrome,
  );

  const declared = await ruleValue('.gly-census button', 'border-radius');
  check(
    '.gly-census button declares 6px on its own rule (not the painted value)',
    declared === '6px',
    { declared },
  );

  // READ OFF THE VERB THAT EXISTS. This asked `.gly-card-accept` and
  // `.gly-thread-resolve`, and neither is built any more — an instruction is
  // immutable work for the next round, so a card carries delete and nothing
  // else. The CLAIM is §1's caste mark and is unchanged: a verb ON A CARD is a
  // pill, and 6px is the chrome's mark alone. `.gly-thread-delete` inherits the
  // 999px from `.gly-card button` and overrides only its colour and its border,
  // so this is exactly the tie `.gly-card button` (0,1,1) wins that this file
  // exists to catch — a chrome rule reaching a card verb would show up here as
  // 6px and nowhere else.
  const verbs = await styleAll('.gly-rail .gly-thread-delete', 'border-radius');
  check(
    'card verbs stay pills',
    verbs.length > 0 &&
      verbs.every((v) => parseFloat(v['border-radius']) > 100),
    verbs,
  );
}

// --- §7 · the three surfaces the mockup never rendered -----------------------
//
// The handoff's own closing note says dark mode, the narrow/sheet layout and
// figures "inherit their layer assignments and need no new rules". That is a
// PREDICTION, not an observation — it rendered none of the three — and the
// panel's own background was a hardcoded chrome token until this week. So it
// is tested here rather than believed.
//
// Read through getComputedStyle under emulateMedia, deliberately, and NOT
// through ruleValue: the dark tokens live inside
// `@media (prefers-color-scheme: dark)` and ruleValue walks TOP-LEVEL rules
// only, so it would answer null for every one of them — which is
// indistinguishable from "no such declaration". The browser honours the media
// query for free, and the question here is what a reader sees anyway.
//
// `.gly-overall` NEEDS NO OPENING NOW, and that is what makes it present here.
// It used to be disclosed by a toggle §3 pressed; the toggle is deleted (see
// §3), the list of filed whole-document instructions is simply the rail's, and
// the fixture files one at the top of this run. What §3 leaves shut behind it
// is the CAPTURE card, which is a different element and is read there.

/** Channel distance between two computed colours.
 *
 *  Every other colour check in this file compares a painted value against
 *  `rgb('--some-token')`, which is the right question — "is this painted from
 *  the token" — and CANNOT see a token that is itself wrong. Measured: setting
 *  `--gly-card: #ffffff` inside the dark block put white cards on a near-black
 *  page and every token-relative check still read `ok`, because the probe
 *  resolves the same broken token both sides.
 *
 *  Dark mode is exactly where that gap bites, since the dark block is the only
 *  place a token is re-declared at all. So one check below is absolute rather
 *  than relative: the card stock has to be a NEIGHBOURING SHADE of the page it
 *  sits on (#fff on #f4f5f8, #1a1d27 on #14161d — both inside 12 per channel),
 *  not an inversion of it. 40 is loose enough to leave real design room and
 *  tight enough that a light stock on a dark page cannot pass. */
const near = (a, b, tol = 40) => {
  const chan = (s) =>
    (String(s).match(/[\d.]+/g) || []).slice(0, 3).map(Number);
  const [x, y] = [chan(a), chan(b)];
  return (
    x.length === 3 &&
    y.length === 3 &&
    x.every((v, i) => Math.abs(v - y[i]) <= tol)
  );
};

for (const scheme of ['dark', 'light']) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.waitForTimeout(300);

  const panel = await style('.gly-overall', 'background-color');
  const body = await style('body', 'background-color');
  // THE SAME CLAIM, THROUGH A TRANSPARENT BOX. This asserted the panel's own
  // background EQUAL to the page's, which is what a floating chrome panel had
  // to do to look like it was on the page rather than on a band. The
  // whole-document card is in the rail now and declares no ground at all
  // (`.gly-overall.gly-overall-rail { background: transparent }`), so the paper
  // the reviewer sees behind it is literally the page's — a stronger version of
  // the same fact, and one that cannot drift the way two colours that must stay
  // equal can. Read as `transparent` in BOTH schemes, because a rule that
  // painted a ground in only one is exactly the dark-mode gap this loop exists
  // for.
  check(
    `the whole-document card shows the page's own paper through it in ${scheme}`,
    panel && panel['background-color'] === TRANSPARENT && !!body,
    { scheme, panel, body },
  );

  // The cards ON the panel, not only the panel under them. A surface that
  // repaints its own background while the deck on it stays light is exactly
  // the shape "it all inherits from §1" fails in — and it is the shape the
  // panel was in a week ago, in the other direction.
  const cards = await styleAll(
    '.gly-overall-entries .gly-card',
    'background-color',
    'border-left-width',
  );
  const stock = await rgb('--gly-card');
  check(
    `every panel card is on card stock in ${scheme}`,
    cards.length > 0 && cards.every((c) => c['background-color'] === stock),
    { scheme, got: cards.map((c) => c['background-color']), want: stock },
  );
  check(
    `every panel card keeps its 3px kind edge in ${scheme}`,
    cards.length > 0 &&
      cards.every((c) => parseFloat(c['border-left-width']) === 3),
    { scheme, cards },
  );
  check(
    `the card stock is a shade of the page in ${scheme}, not an inversion of it`,
    cards.length > 0 &&
      body &&
      cards.every((c) => near(c['background-color'], body['background-color'])),
    {
      scheme,
      card: cards[0] && cards[0]['background-color'],
      page: body && body['background-color'],
    },
  );

  // FIGURES — the third prediction, and the one with no assertion anywhere in
  // this file until now. The `read-only` chip is the passive half of the
  // refusal voice and it is CHROME wherever it renders: the same mono voice as
  // the census strip, the muted colour, the panel token behind it. Compared
  // against the census's own computed font-family rather than against a string,
  // because the claim is "the same voice as the chrome", not "this font list".
  const chip = await style(
    '.gly-figure > .gly-chip',
    'font-family',
    'color',
    'background-color',
  );
  const chrome = await style('.gly-census', 'font-family');
  const muted = await rgb('--gly-muted');
  const panelBg = await rgb('--gly-panel-bg');
  check(
    `the figure's read-only chip speaks the chrome voice in ${scheme}`,
    chip &&
      chrome &&
      chip['font-family'] === chrome['font-family'] &&
      chip.color === muted &&
      chip['background-color'] === panelBg,
    { scheme, chip, chrome, muted, panelBg },
  );

  // And the figure box itself is a §1 container: 6px, on the line colour, on
  // the fence token — the same three values a fence and a table carry.
  const fig = await style(
    '.gly-figure',
    'border-radius',
    'border-top-color',
    'background-color',
  );
  check(
    `the figure box is a 6px container on the line colour in ${scheme}`,
    fig &&
      fig['border-radius'] === '6px' &&
      fig['border-top-color'] === (await rgb('--gly-line')) &&
      fig['background-color'] === (await rgb('--gly-fence')),
    { scheme, fig },
  );
}
await page.emulateMedia({ colorScheme: 'light' });

// --- §7a · every control in the bar can be pressed, at any width -------------
//
// THE PROPERTY IS "NO CONTROL IS UNREACHABLE", NOT "REVISE IS IN THE BOTTOM
// BAR". A check named after the instance passes in the broken state: the bar is
// a flex row and whatever ends up LAST in source order is what goes first, so a
// check that watched only `#gly-revise` would go quiet the moment a control was
// appended after it — with the same defect one item to the left.
//
// AND "REACHABLE" IS NOT "INSIDE THE WINDOW". This block asserted the rect
// first — `x >= 0 && x + width <= innerWidth` — and that predicate PASSED on a
// button nobody could click: the bar folds, and above the breakpoint its second
// row landed underneath `.gly-rail`, which is `position: fixed` at `z-index:
// 20` over a bar at 10. Measured at 1000px before the rail was taught the bar's
// height: `#gly-revise` at `{x: 834.4, y: 49.2, right: 980}` — wholly inside a
// 1000px window — with `elementFromPoint` at its centre returning
// `.gly-rail-band` and a real click timing out. A rect fully inside the
// viewport underneath an opaque fixed panel satisfies "on screen" and fails
// "the reviewer can press it", and those are two different claims.
//
// So the sweep reads `document.elementFromPoint` back at each control's centre
// and requires that control or a descendant of it. There is precedent one file
// over: motion.mjs reads elementFromPoint back at the coordinate it clicked,
// on the argument that "nothing moved" and "what you clicked is still under
// your finger" are separate facts. This is that distinction, standing still.
//
// The rect check stays. It is cheap, it runs first, and it names something
// real — a control past the right edge with no horizontal scroll is gone
// whatever is or is not painted over where it used to be.
//
// AND IT SWEEPS, because the failure is width-dependent and a single viewport
// is how the previous gap was missed — twice now. Measured on this fixture:
// the bar's content is 1159px wide with the rail on screen and 1066px without
// it, so `#gly-revise` left the window at 1100 and at 1000 — both ABOVE the
// 992px rail breakpoint — and by 900 `.gly-hold` had gone with it, by 768
// `.gly-mode`, by 600 the whole census strip. "Move it to the bottom bar below
// 992" would have left every width between 992 and 1159 exactly as broken.
//
// ONE HEIGHT, AND THAT IS A KNOWN LIMIT. "No control is unreachable" is a
// two-dimensional claim and this sweep varies one dimension. 900px is tall
// enough that nothing in the bar has ever been clipped vertically, and §7c
// below drives 844×390 and 900×260 — the short windows where vertical room is
// the scarce thing — so the gap is covered by a different block rather than
// left open. A control lost to a SHORT window would be missed here.
//
// 1400 and 1450 are in the list because the OVERLAP band is not the overflow
// band. At those widths the bar never spilled — the folded row was created by
// a longer document name, and the rail then covered it. The first ten widths
// this block swept all missed it, and 1400–1450 was the one place where the
// fold made things WORSE than the overflow it replaced: a clipped button still
// had a clickable centre, and a covered one has nothing.
{
  const WIDTHS = [
    1600, 1450, 1400, 1200, 1100, 1000, 992, 991, 900, 768, 600, 390,
  ];
  // Every child of the bar, every item in the census strip inside it, and every
  // button anywhere in it — the same landmark set motion.mjs snapshots, for the
  // same reason: the claim is about the bar, not about one button in it.
  const barProbe = () =>
    page.evaluate(() => {
      const sel = '.gly-bar > *, .gly-census > *, .gly-bar button';
      const seen = new Set();
      const out = [];
      for (const el of document.querySelectorAll(sel)) {
        if (seen.has(el)) continue;
        seen.add(el);
        const r = el.getBoundingClientRect();
        // ZERO-AREA ITEMS ARE SKIPPED, AND THE EXEMPTION IS STRUCTURAL RATHER
        // THAN INCIDENTAL. What it actually covers is SMALLER than an earlier
        // version of this comment claimed, and the difference matters: the
        // spacer draws nothing by definition, so it measures 0px here — and it
        // is now the ONLY silence. `#gly-status` used to be the second one:
        // it was empty until Revise was pressed, because the reassurance lived
        // in a separate `#gly-editor-status` beside it. The three readouts are
        // one now, and that one prints from the first paint
        // (`connected · your edits apply — the agent proposes`), so it is swept
        // at every width like any other item. `flex: 100 1 0` with
        // `max-width: max-content` takes the readout out of the bar's
        // LINE-BREAKING arithmetic; it does not make it zero-area. A later
        // reader must not mistake a silence here for a pass — there is exactly
        // one genuinely empty box left.
        if (r.width <= 0 || r.height <= 0) continue;
        // AND A RESERVED BOX IS NOT A BUTTON — the product's own words. `.gly-
        // hold` keeps its box on ask so that flipping the switch moves nothing,
        // and `.gly-reserved` is `visibility: hidden`, which takes it out of
        // hit testing and the tab order on purpose (it is `disabled` too). It
        // is unreachable BY DESIGN, so counting it as covered would make this
        // check fail at every width for the one reason that is not a bug. The
        // test is computed visibility rather than the class name, because the
        // claim is about what the browser will do with a click, not about
        // which spelling of "reserved" a control happens to use.
        if (getComputedStyle(el).visibility !== 'visible') continue;
        const what = el.id
          ? `#${el.id}`
          : `.${el.classList[0] || el.tagName.toLowerCase()}`;
        const cx = r.x + r.width / 2;
        const cy = r.y + r.height / 2;
        const at = document.elementFromPoint(cx, cy);
        out.push({
          what,
          x: +r.x.toFixed(1),
          y: +r.y.toFixed(1),
          right: +(r.x + r.width).toFixed(1),
          // `el.contains(at)` rather than `at === el`: a control's centre lands
          // on the label span inside it, which is the button as far as a click
          // is concerned. Anything else — a rail band, a panel, another
          // control — is something in the way.
          hitBy: at ? at.id || at.className || at.tagName : 'nothing',
          reachable: !!(at && el.contains(at)),
        });
      }
      const bar = document.querySelector('.gly-bar');
      return {
        w: window.innerWidth,
        items: out,
        // The companion statement of the same fact, read off the bar itself:
        // content wider than the box is what "spills out of the window" IS,
        // and it holds even for a bar whose overflow was hidden rather than
        // fixed — an escapee clipped is still an escapee.
        spill: +(bar.scrollWidth - bar.clientWidth).toFixed(1),
        // THE BAR'S OWN HEIGHT, so this sweep can say which shape it walked.
        // See the fold assertions after the loop.
        barH: +bar.getBoundingClientRect().height.toFixed(1),
      };
    });

  const heights = [];
  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    const { w, items, spill, barH } = await barProbe();
    heights.push({ width, barH });
    const escaped = items.filter((i) => i.x < 0 || i.right > w + 0.5);
    check(
      `every control in the bar is inside the window at ${width}px`,
      escaped.length === 0,
      { innerWidth: w, escaped },
    );
    const covered = items.filter((i) => !i.reachable);
    check(
      `every control in the bar can be pressed at ${width}px — nothing is over it`,
      covered.length === 0,
      { innerWidth: w, covered },
    );
    check(`the bar's content fits its own box at ${width}px`, spill <= 0.5, {
      innerWidth: w,
      spill,
    });
  }

  // THE FOLD IS ASSERTED HERE TOO, AND IT WAS NOT — which is the fixture
  // hazard CLAUDE.md names, standing open in the one file that sweeps eleven
  // widths. motion.mjs asserts it (`the bar is folded at …`) precisely because
  // every other check in that file passes identically on a flat bar; the same
  // is true of every check in this loop, and the reason this fixture's name is
  // twenty-eight characters is that it FOLDS the bar at the widths below 1240.
  // Held by a filename alone, one font metric or one bar child leaving would
  // return the whole sweep to walking the shape it was renamed to stop
  // certifying, in silence — and a bar child did leave: the readouts went from
  // three cells to one, taking two of them and their 12px gaps with them, so
  // the boundary moved narrower and the comment at the head of this file that
  // still quoted the old numbers was measuring a build that no longer exists.
  //
  // A flat bar is one row and a folded one is two, so the threshold is stated
  // the way motion.mjs states it: a gap-width away from both, encoding neither.
  note("the bar's height at every swept width", heights);
  const FOLDED_MIN = 60;
  const foldedWidths = heights
    .filter((h) => h.barH > FOLDED_MIN)
    .map((h) => h.width);
  const flatWidths = heights
    .filter((h) => h.barH <= FOLDED_MIN)
    .map((h) => h.width);
  check(
    'the sweep really walks a FOLDED bar — the shape this fixture is named to produce',
    foldedWidths.length > 0 && Math.max(...foldedWidths) >= 1100,
    { folded: foldedWidths, heights },
  );
  check(
    'and a FLAT one at its widest — an ordinary name is not supposed to fold 1600px',
    flatWidths.includes(1600),
    { flat: flatWidths, heights },
  );
}

// --- §7b · the narrow layout ------------------------------------------------
//
// 900px is below the 991px breakpoint, which is where the rail is replaced by a
// bottom bar and a sheet. Both are surfaces the mockup never drew.

await page.setViewportSize({ width: 900, height: 1000 });
await page.waitForTimeout(800);
{
  // FIRST, that we are actually in the narrow layout. Every assertion below is
  // about the sheet, and a sheet measured while the rail is still on screen is
  // a measurement of nothing. The rail and the sheet must never render at the
  // same time — editor.css says so at the breakpoint in as many words.
  const rail = await style('.gly-rail', 'display');
  const bottombar = await page.locator('.gly-bottombar').isVisible();
  check(
    'the narrow layout is the one on screen — rail gone, bottom bar up',
    rail && rail.display === 'none' && bottombar,
    { rail, bottombar },
  );

  // THE COUNT IS PRINTED ONCE. `5 pending · 3 threads` renders in the census
  // strip at the head of the window and again in `.gly-bar-count` at its foot
  // — same mono, same 12px, the same string from the same `paintCensus` line,
  // ~850px apart on a phone. Below the breakpoint the foot's copy is the one
  // that earns it: it is also the BUTTON that opens the sheet, and the sheet
  // is the only list there is here.
  //
  // Stated as "exactly one is painted" rather than "the strip's is hidden",
  // because the defect is the reviewer reading the same number twice — which
  // of the two went is a design decision, and a check naming the loser would
  // have to be rewritten to reverse it rather than simply re-run.
  const counts = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.gly-census-count, .gly-bar-count'))
      .filter((el) => el.getBoundingClientRect().width > 0)
      .map((el) => ({
        what: el.className,
        text: (el.textContent || '').trim(),
      })),
  );
  check(
    'the pending count is printed once below the breakpoint, not twice',
    counts.length === 1 && /Instructions · \d+/.test(counts[0].text),
    counts,
  );

  // Chrome is chrome at every width: the bottom bar's controls carry the 6px
  // caste mark, exactly as the census strip's do at the top of a wide window.
  const barButtons = await styleAll('.gly-bottombar button', 'border-radius');
  check(
    'every narrow-bar control is 6px chrome',
    barButtons.length > 0 &&
      barButtons.every((b) => b['border-radius'] === '6px'),
    barButtons,
  );

  // §4's armed-delete test (above) arms the retry-budget thread's delete —
  // and armedDelete lives on the app, keyed by thread.key, precisely so a
  // rebuilt card renders itself already armed (see deleteButton's comment).
  // The sheet renders that SAME thread again, so without this it would read
  // armed or not depending on how much wall-clock time this file happened to
  // spend between there and here — a check whose answer depends on how many
  // `check()` calls ran before it is not a check on the product.
  await page.evaluate(() => {
    window.galleyEdit.app.armedDelete = null;
  });

  // OPEN IT. The sheet holds cards only once opened, and a check written to
  // tolerate zero matches is a check that passes loudest when the layout
  // renders nothing at all.
  //
  // THROUGH THE APP, BECAUSE THE BUTTON THAT USED TO DO IT IS GONE. `▤`
  // (`.gly-bar-list`) was the narrow bar's third control and the sheet's own
  // door; the bar carries the Instructions count, the History chip and one
  // stepper now, and `.gly-bar-count` posts `openInstructions()`, which sets
  // `sheetOpen = false` — the opposite of what this line needs. The only
  // remaining opener in the product is `flashThreadCard`, reached by tapping a
  // figure's pin, and driving a pin here would make every check below depend on
  // the figure being on screen at this scroll position.
  //
  // SO THIS IS A GATE DRIVING A STATE THE PRODUCT CAN REACH BUT NO LONGER
  // OFFERS A BUTTON FOR, and that is worth saying rather than hiding behind a
  // helper: everything below reads the sheet's PAINT, which is a real surface
  // with real cards in it either way, but "the reviewer can get here at this
  // width" is a claim this file can no longer make and does not pretend to.
  await page.evaluate(() => window.galleyEdit.app.openSheet());
  await page.waitForTimeout(600);
  check(
    'the sheet really opened, so the paint below is read off a surface that exists',
    await page.locator('.gly-sheet').isVisible(),
  );

  // AND THE STANDING SENTENCE IS PRINTED ONCE TOO — the count's own defect, one
  // cell over, and it arrived with the bar's consolidation. `.gly-census-untracked`
  // used to be a bar cell with a `display: none` below 1340px, and the sheet's
  // head was written to be where the sentence went instead ("the census strip
  // drops this sentence below the breakpoint — the sheet's head carries it
  // instead"). Composing it into the one readout dropped the hide with the cell,
  // so at every width under 992 the sentence renders in the bar AND in the head
  // of the sheet covering it — two things doing one job, at the one width Court
  // would see it.
  //
  // Below the breakpoint the SHEET'S copy is the one that earns it, for the
  // count's reason: it is on a line of its own and prints whole, where the bar's
  // readout is a single ellipsizing cell whose last clause it is — an illegible
  // sliver of the sentence is what the 1340 hide existed to prevent.
  //
  // Stated as "exactly one is painted", like the count, so reversing the design
  // decision means re-running this rather than rewriting it. Matched on the
  // app's own constant, not a copy of the string.
  const sentences = await page.evaluate(
    (sentence) =>
      Array.from(document.querySelectorAll('#gly-status, .gly-sheet-untracked'))
        .filter((el) => el.getBoundingClientRect().width > 0)
        .filter((el) => (el.textContent || '').includes(sentence))
        .map((el) => ({
          what: el.id || el.className,
          text: (el.textContent || '').trim(),
        })),
    UNTRACKED_NOTE,
  );
  check(
    'the standing sentence is printed once below the breakpoint, not twice',
    sentences.length === 1,
    sentences,
  );

  // `.gly-sheet-body > .gly-card`, not `.gly-sheet .gly-card`: the sheet has a
  // settled region at its foot now (§7b′ below) and its cards are `.gly-card`
  // too, nested inside it. Every check in THIS block is about the sheet's open
  // list — what the bar counts — so it reads the open list's own children.
  const cards = await styleAll(
    '.gly-sheet-body > .gly-card',
    'border-left-width',
    'border-top-width',
    'background-color',
    'border-radius',
    'width',
  );
  const stock = await rgb('--gly-card');
  check('the sheet actually holds cards once opened', cards.length > 0, {
    cards: cards.length,
  });
  check(
    'sheet cards carry the same kind edge as rail cards',
    cards.length > 0 &&
      cards.every((c) => parseFloat(c['border-left-width']) === 3),
    cards,
  );
  check(
    'sheet cards are full cards from the same deck, not a bare narrow variant',
    cards.length > 0 &&
      cards.every(
        (c) =>
          parseFloat(c['border-top-width']) > 0 &&
          c['background-color'] === stock &&
          c['border-radius'] === '6px',
      ),
    { cards, want: stock },
  );

  // Family, not a count. §4b established the stronger form — compare each
  // head's computed `font-family` against `.gly-census`'s own, so the claim is
  // "the same voice as the chrome" — and this surface was given the weaker one
  // AFTERWARDS. Counting elements never reads a font: a head rendered in the
  // document's serif satisfies it exactly as well as a mono one does.
  const heads = await styleAll(
    '.gly-sheet-body > .gly-card .gly-card-head',
    'font-family',
  );
  const chrome = await style('.gly-census', 'font-family');
  check(
    'every sheet card carries its mono head',
    heads.length === cards.length &&
      cards.length > 0 &&
      chrome &&
      heads.every((h) => h['font-family'] === chrome['font-family']),
    { heads, cards: cards.length, chrome },
  );

  // THE COUNT NEVER LIES. Below the breakpoint the sheet is the only list
  // there is, so what it holds has to equal what the bar itself claims — read
  // from the bar's OWN TEXT, not the fixture, because the claim under test is
  // "the product's count matches what the product shows", not "the fixture has
  // N threads". paintSheet used to skip every kind === 'comment' suggestion,
  // which made this bar text a promise the sheet could not keep.
  //
  // THERE IS ONE NUMBER NOW, AND THE SUM IS GONE WITH THE SECOND ONE.
  //
  // The bar used to print `5 pending · 3 threads` and carry the whole-document
  // conversation on a THIRD readout, the `1 doc note` handle beside the strip —
  // and the repair this block records is that the doc note must appear in
  // exactly one of them. It read `barPending + barThreads` while the tally
  // counted the doc note too and the handle counted it AGAIN, so the bar
  // advertised four conversations over three and the arithmetic was satisfied
  // by the double count as happily as by the truth.
  //
  // The bar says `Instructions · N` and nothing else. There is no second number
  // to disagree with, no handle to count anything twice, and the whole-document
  // instruction is one of the N like any other. So the disjointness checks are
  // deleted — a claim that two numbers do not overlap needs two numbers — and
  // what survives is the half that was always the point: THE COUNT NEVER LIES.
  // The sheet is the only list there is below the breakpoint, so what it holds
  // has to equal what the bar itself claims, read from the bar's OWN TEXT
  // rather than from the fixture, because the claim under test is "the
  // product's count matches what the product shows".
  const barText = (await page.locator('.gly-bar-count').textContent()) || '';
  const barCount = Number(barText.match(/Instructions · (\d+)/)?.[1] ?? NaN);
  check(
    'the bar prints a number at all, so the sum below is a sum of something',
    Number.isFinite(barCount) && barCount > 0,
    { barText, barCount },
  );
  check(
    'the sheet holds exactly what the bar counts — no more, no less',
    cards.length === barCount,
    { barText, barCount, cards: cards.length },
  );
  // AND THE WHOLE-DOCUMENT CONVERSATION IS ONE OF THEM, which is what keeps the
  // equality from being satisfied by a fixture with nothing awkward in it. It
  // has no mark, so it is exactly the instruction a list built from the marks
  // would drop — `paintSheet` used to skip a whole kind of pending item, which
  // made this bar text a promise the sheet could not keep.
  const docOpen = await page.evaluate(
    () =>
      (window.galleyEdit.app.comments || []).filter(
        (t) => t.anchor === 'document',
      ).length,
  );
  check(
    'the fixture has a whole-document instruction, so the sheet is not a list of marks',
    docOpen > 0,
    { docOpen },
  );
  check(
    'and the sheet is carrying it — the count includes what has no mark',
    cards.length >= docOpen + 1,
    { docOpen, cards: cards.length },
  );

  // A card designed for a 300px rail should not be stretched to the width of
  // the whole window. "the document's measure" is read off the document
  // itself (.ProseMirror), not off a token, so this asserts what a card
  // actually sits beside rather than what a stylesheet claims it should.
  const measure = await style('.ProseMirror', 'width');
  check(
    "a sheet card is no wider than the document's own measure",
    cards.length > 0 &&
      measure &&
      cards.every((c) => parseFloat(c.width) <= parseFloat(measure.width) + 1),
    { cards: cards.map((c) => c.width), measure: measure && measure.width },
  );

  // AND IT SITS WHERE THE PROSE SITS — the check for the padding fix, which
  // shipped with none that could fail. `.gly-sheet`'s `12px 12px 52px` became
  // `12px 0 52px` because below ~704px `.gly-sheet-body`'s max-width stopped
  // binding and the sheet's own 12px squeeze showed up as cards sitting
  // narrower and INBOARD of the prose. The `<=` above passed in exactly that
  // state — narrower IS no wider — and nothing read `left` at all. So: EQUAL,
  // in left and in width, and at more than one width, because the defect was
  // width-dependent and the state it was broken in is the one below the point
  // where the max-width binds.
  const ALIGN = [900, 704, 640, 500, 390];
  for (const w of ALIGN) {
    await page.setViewportSize({ width: w, height: 1000 });
    await page.waitForTimeout(350);
    const box = await page.evaluate(() => {
      const pm = document.querySelector('.ProseMirror');
      const card = document.querySelector('.gly-sheet-body > .gly-card');
      if (!pm || !card) return null;
      const p = pm.getBoundingClientRect();
      const c = card.getBoundingClientRect();
      return {
        pm: { l: +p.left.toFixed(1), w: +p.width.toFixed(1) },
        card: { l: +c.left.toFixed(1), w: +c.width.toFixed(1) },
      };
    });
    check(
      `a sheet card's left edge and width are the prose's own at ${w}px`,
      !!box &&
        Math.abs(box.card.l - box.pm.l) <= 0.5 &&
        Math.abs(box.card.w - box.pm.w) <= 0.5,
      box,
    );
  }
  await page.setViewportSize({ width: 900, height: 1000 });
  await page.waitForTimeout(400);

  // §6 asks for the destroy weight in rail, panel, sheet and settled list
  // alike — this is the sheet's turn. Comments now render as threadCard, so
  // `.gly-thread-delete` exists here to have an opinion about.
  //
  // ALL THREE PROPERTIES, as everywhere else: §6's destroy weight is
  // borderless AND muted AND 2rem clear, and a check reading one of the three
  // reports `ok` on a surface that has lost the other two.
  const dels = await styleAll(
    '.gly-sheet-body > .gly-card .gly-thread-delete',
    'border-top-color',
    'color',
    'margin-left',
  );
  const sheetMuted = await rgb('--gly-muted');
  check(
    'the destroy weight renders in the sheet too',
    dels.length > 0 &&
      dels.every(
        (d) =>
          d['border-top-color'] === TRANSPARENT &&
          d.color === sheetMuted &&
          parseFloat(d['margin-left']) >= 32,
      ),
    { dels, muted: sheetMuted },
  );

  // A card whose thread carries a run IS tied to a mark — threadCard sets
  // dataset.run from thread.run itself, independent of whatever placement it
  // was handed, so this reads what is TRUE rather than what the sheet claims.
  // A hardcoded anchorless placement draws such a card exactly like the settled
  // list draws a genuinely-gone one: dashed, and captioned "not tied to a
  // mark". Neither is true of a thread the rail would draw anchored.
  const anchoredInSheet = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.gly-sheet-body > .gly-thread'))
      .filter((el) => el.dataset.run)
      .map((el) => ({
        run: el.dataset.run,
        adrift: el.classList.contains('gly-adrift'),
        unplaced: !!el.closest('.gly-rail-unplaced'),
        note: el.querySelector('.gly-card-head')?.textContent || '',
      })),
  );
  // READ STRUCTURALLY. The second clause used to match the card's own
  // explanatory sentence — `/not tied to a mark/` — and copy is the wrong thing
  // to pin: it goes red on a rewording and, worse, goes GREEN and stops finding
  // anything the day the sentence is dropped. `.gly-adrift` is the class
  // `threadCard` writes from `threadLabel`'s own verdict and is what the dashed
  // border is drawn from; the card being in the BAND rather than the unplaced
  // region is the same fact from the other side, and together they say what the
  // sentence said without depending on a word of it.
  check(
    'no sheet thread card with a run is drawn adrift or filed as unplaced',
    anchoredInSheet.length > 0 &&
      anchoredInSheet.every((t) => !t.adrift && !t.unplaced),
    anchoredInSheet,
  );

  // The same cause, the other symptom: threadCard sets the title and the
  // reveal click handler ONLY when it was handed an anchored placement — a
  // hardcoded anchorless placement never reaches that code. A conversation in the sheet has
  // to be jumpable exactly like a suggestion card beside it; a sheet you
  // cannot jump from is the "list you have to dismiss by hand" paintSheet's
  // own comment says this surface must never be.
  const titles = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.gly-sheet-body > .gly-thread'))
      .filter((el) => el.dataset.run)
      .map((el) => el.title),
  );
  check(
    'every anchored sheet thread card can be jumped from, same as its rail counterpart',
    titles.length > 0 && titles.every((t) => t === 'show me where this is'),
    titles,
  );
}

// --- §7b‴ · the narrow bar is still a bar with the sheet open ----------------
//
// A RECT INSIDE THE WINDOW IS NOT A CONTROL THE REVIEWER CAN PRESS, and this is
// that rule in the one state no gate walked: narrow, with the sheet open.
// Measured at 430×900 on the shipped build — `.gly-bottombar` painted, not
// `hidden`, `getBoundingClientRect` at `{y: 860, height: 40}`, `isVisible()`
// true — and `document.elementFromPoint` at each of its four controls' centres
// answering `gly-thread-who` and `gly-thread-entry`: the sheet (z-index 45)
// over the bar (25). All four unreachable, including the two the narrow layout
// has no other spelling of.
//
// THE BAR IS CLAIMED IN THIS STATE BY THREE SEPARATE PIECES OF THE PRODUCT,
// which is what decides the fix rather than taste. `railSurfaces` answers
// `bar: true` below the breakpoint WHETHER OR NOT the sheet is open — the one
// accessor that decides what a width means, and it says the bar renders here,
// where at 992 and above it says `bar: false`. `.gly-sheet` reserves
// `padding-bottom: 52px`, which is the bar's 40px and the sheet's own 12px:
// the sheet's layout was written for a bar painted over it. And `step()` — the
// ↑/↓ this bar is the only home of — carries a branch for exactly this state
// ("the sheet's cards too: with the sheet open it is the surface showing the
// list"). Hiding the bar would contradict all three and take the count, the
// step and the list away at once; making the sheet stop short of it would need
// the bar's height as a second constant in a layout where it is `bar: false`
// above the breakpoint. The bar goes ABOVE the sheet, which is what the 52px
// already assumes, and the sheet's list scrolls under it exactly as the prose
// scrolls under the sticky bar at the top of the page.
//
// THE SIBLING STATE IS ASKED TOO. At and above the breakpoint the sheet is the
// review's whole list and the bottom bar is not painted at all, so the same
// invariant is stated as an implication — a bar that is HIDDEN makes no claim
// — and the wide-with-sheet-open case is recorded as a note beneath it.
{
  const readBar = () =>
    page.evaluate(() => {
      const bar = document.querySelector('.gly-bottombar');
      if (!bar) return null;
      return {
        hidden: bar.hidden || getComputedStyle(bar).display === 'none',
        z: getComputedStyle(bar).zIndex,
        controls: Array.from(bar.querySelectorAll('button')).map((b) => {
          const r = b.getBoundingClientRect();
          const at = document.elementFromPoint(
            r.left + r.width / 2,
            r.top + r.height / 2,
          );
          return {
            what: b.className,
            inside:
              r.width > 0 &&
              r.height > 0 &&
              r.left >= 0 &&
              r.right <= window.innerWidth + 0.5 &&
              r.bottom <= window.innerHeight + 0.5,
            reachable: !!(at && b.contains(at)),
            hitBy: at ? at.id || at.className || at.tagName : 'nothing',
          };
        }),
      };
    });
  const setSheet = async (open) => {
    await page.evaluate((want) => {
      const app = window.galleyEdit.app;
      if (want) app.openSheet();
      else app.closeSheet();
    }, open);
    await page.waitForTimeout(400);
  };

  for (const width of [900, 640, 430, 1200]) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    for (const open of [false, true]) {
      await setSheet(open);
      const bar = await readBar();
      const state = `${width}px, sheet ${open ? 'open' : 'shut'}`;
      // NON-VACUITY, and it is the whole reason this is not one loop: below the
      // breakpoint the bar has to BE there with controls on it, or "every
      // control is reachable" is a claim about an empty list.
      if (width < 992) {
        check(
          `the narrow bar is painted at ${state} — it is the layout's whole chrome`,
          !!bar && !bar.hidden && bar.controls.length > 0,
          bar,
        );
      } else {
        check(
          `and above the breakpoint it is not painted at all at ${state} — railSurfaces says bar: false`,
          !!bar && bar.hidden,
          bar,
        );
      }
      check(
        `every control the narrow bar paints can be pressed at ${state}`,
        !!bar &&
          (bar.hidden || bar.controls.every((c) => c.inside && c.reachable)),
        bar && bar.controls.filter((c) => !c.inside || !c.reachable),
      );
    }
  }

  // THE WIDE SIBLING, RECORDED RATHER THAN ASSERTED. With the sheet open at
  // 1200px the top bar is under it too — same z-index arithmetic, different
  // meaning: nothing claims the bar renders in that state (no bottom bar is
  // painted, `paintSurfaces` gives the sheet the page), the sheet is opaque and
  // full-bleed so no control is on screen LOOKING pressable, and Esc and the
  // sheet's own ✕ are the way out. It is a takeover rather than a covered
  // control. The number is printed so the next reader has it rather than
  // re-measuring, and a permanently-red check for a deliberate design is what
  // trains people to ignore this gate.
  await page.setViewportSize({ width: 1200, height: 900 });
  await setSheet(true);
  note(
    'with the sheet open above the breakpoint the top bar is covered by it — a takeover, not a covered control (Esc and ✕ are the exits)',
    await page.evaluate(() => {
      const el = document.getElementById('gly-revise');
      const r = el.getBoundingClientRect();
      const at = document.elementFromPoint(
        r.left + r.width / 2,
        r.top + r.height / 2,
      );
      return {
        revise: at ? at.id || at.className : 'nothing',
        sheetOpen: !!window.galleyEdit.app.sheetOpen,
      };
    }),
  );

  // Put §7b″'s state back: 900px, sheet open, which is where §7b left it.
  await page.setViewportSize({ width: 900, height: 1000 });
  await setSheet(true);
  await page.waitForTimeout(400);
}

// --- §7b″ · the draft carry — DELETED WITH THE REPLY BOX ---------------------
// --- §7b′ · a settled thread reachable here too — DELETED WITH THE RESOLVE ---
//
// §7b″ was the draft-carry block. `paintRail` destroys and rebuilds every card
// on every poll, and `carryDrafts`/`captureDrafts`/`restoreDrafts` existed so a
// half-written reply survived that; `draftRoots()` is the list of surfaces they
// walk, and it is a LIST, so it went stale the moment `threadCard`'s
// `data-draft` reply box was rendered on a FOURTH surface — the rail's band,
// the whole-document panel, the settled list, then the SHEET. Measured before
// the fix at 900x1000: thirty-five characters became `''` with focus on `BODY`,
// while the identical gesture in the rail survived in the same run.
//
// There is no `data-draft` in the page. `threadCard` builds no reply box —
// an instruction is not a conversation to answer — and the one line that still
// writes `dataset.draft` (`reply:${s.run}`) is on the proposal reply, which has
// no proposals to hang off. So `captureDrafts` walks three roots and finds
// nothing, on every poll, in every state: the mechanism is unreachable rather
// than broken, and a check over it would be measuring an empty `Map`. The
// fixture guard this block opens with ("the sheet has a thread reply box to
// half-write into") is the line that went red, which is the guard working.
//
// §7b′ was the settled thread at narrow. `↺ reopen` is the only way back from a
// mis-tapped `✓ resolve` and CLAUDE.md says it is what makes "settles it and
// KEEPS the history" true — so the block opened the sheet's settled region on a
// phone and read the reopen verb, the dim and the head off a card in it. There
// is no resolve verb on a card and `galley resolve` is gone, so nothing settles,
// so the region is `hidden` on every run and its list is empty. Same deletion,
// same reason, as §4b above — and said twice on purpose, because the two blocks
// were each other's cross-reference and a reader arriving at either one should
// not have to find the other to learn why the surface is unasserted.

// --- §7c · below the breakpoint, the mark opens its conversation -------------
//
// The sheet is a LIST — it answers "what is outstanding". It does not answer
// "what is this highlight about", and below 992px nothing else did: the rail
// that carries conversations at wide is gone, and a tap on a highlight opened a
// three-line bubble offering accept and reject on a thing that is resolved,
// never accepted.
//
// So a comment highlight now opens its whole thread, on the same card the rail,
// the panel, the settled list and the sheet draw. Four claims, and the third is
// the one that would be quietly wrong: a conversation COVERS the prose, it does
// not push it.

/** Every top-level block of the document, by rect. The unit the displacement
 *  check compares — see below for why it is the whole list rather than one. */
const blockRects = () =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll('.ProseMirror > *')).map((el) => {
      const r = el.getBoundingClientRect();
      return { top: r.top, left: r.left, width: r.width, height: r.height };
    }),
  );

{
  // §3 opened the whole-document card, and the reason this check exists is
  // unchanged: a surface still over the prose would make every measurement
  // below a measurement of the wrong thing. WHAT PUTS IT AWAY IS DIFFERENT.
  // It was chrome, fixed over the top of the document, and Esc was the
  // product's own way to dismiss it; it is the first item of the instruction
  // RAIL now, and the rail is `display: none` below the breakpoint — this
  // block runs at narrow, so the whole map including that card is off screen
  // by the layout rather than by a keypress.
  //
  // Read off the RAIL, not off the card. `getComputedStyle` answers about the
  // element's own `display`, so a card inside a hidden rail still reports
  // `block` — which is what the first re-point of this line reported, green
  // reasoning over a red read. Esc is still pressed, because it is what closes
  // the bubble a previous block may have left open.
  await page.keyboard.press('Escape');
  await page.waitForTimeout(400);
  const panelGone = await page.evaluate(() => {
    const rail = document.querySelector('.gly-rail');
    const card = document.querySelector('.gly-overall');
    return {
      rail: rail ? getComputedStyle(rail).display : 'missing',
      cardWidth: card ? +card.getBoundingClientRect().width.toFixed(2) : null,
    };
  });
  check(
    'the whole-document card is off screen with the rail before the prose is tapped',
    panelGone.rail === 'none' && panelGone.cardWidth === 0,
    panelGone,
  );

  const mark = page.locator('.gly-hl').first();
  check(
    'the fixture has a comment highlight in the prose to tap',
    (await mark.count()) > 0,
  );

  // Scrolled into view and settled BEFORE the rects are read, because
  // Playwright's own click would scroll to reach the mark and every rect in the
  // page would move for a reason that has nothing to do with the bubble.
  await mark.scrollIntoViewIfNeeded();
  await page.waitForTimeout(400);
  const before = await blockRects();
  await mark.click();
  await page.waitForTimeout(400);
  const after = await blockRects();

  // THE ASSERTION THIS WHOLE SECTION IS FOR. A conversation that opens in flow
  // pushes every block below it down the page, and the sentence the reviewer
  // was reading walks out from under their eyes — the same defect CLAUDE.md
  // records for the chrome, arriving through a new door. Verified failing
  // against a deliberately-pushing implementation (a bubble appended after the
  // clicked span's block instead of positioned): 4 of the 6 blocks moved by
  // 132px and this read FAIL while every other check in this block still
  // passed.
  //
  // EVERY block, not the one that was clicked: a pushing implementation leaves
  // the blocks ABOVE it exactly where they were, so reading one rect is how
  // that regression hides.
  const moved = before
    .map((b, i) => ({ i, before: b, after: after[i] }))
    .filter(
      ({ before: b, after: a }) =>
        !a ||
        Math.abs(a.top - b.top) > 0.5 ||
        Math.abs(a.left - b.left) > 0.5 ||
        Math.abs(a.height - b.height) > 0.5,
    );
  check(
    'opening a conversation moves no block of the document — it covers, it does not displace',
    before.length > 0 && after.length === before.length && moved.length === 0,
    moved,
  );

  // It is the ONE card, with everything a conversation has. A bubble that
  // rendered a bare list of replies would pass a "there is text here" check and
  // fail the reviewer at the moment they wanted to answer.
  const bubble = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    if (!el || el.hidden) return null;
    const card = el.querySelector('.gly-card.gly-thread');
    if (!card) return { card: false };
    return {
      card: true,
      entries: card.querySelectorAll('.gly-thread-entry').length,
      reply: !!card.querySelector('textarea.gly-thread-reply'),
      resolve: !!card.querySelector('.gly-thread-resolve'),
      destroy: !!card.querySelector('.gly-thread-delete'),
      accept: !!el.querySelector('.gly-bubble-accept, .gly-bubble-reject'),
      adrift: card.classList.contains('gly-adrift'),
      note: card.querySelector('.gly-card-note')?.textContent || '',
      run: card.dataset.run,
    };
  });
  check(
    'tapping a comment highlight opens that thread on the one card',
    bubble && bubble.card,
    bubble,
  );
  // THE SAME CARD THE RAIL SHOWS, WHICH IS THE CLAIM — and it carries what a
  // card carries NOW. This asked for a reply box and both thread verbs, and the
  // component builds neither: an instruction is immutable work for the next
  // round, so there is no reply to type and nothing to resolve, and the one verb
  // is delete. The names are re-pointed, the claim is not: whatever the rail
  // renders on a card, the bubble renders too, or the reviewer who taps a
  // highlight on a phone gets a lesser card than the reviewer who reads the rail
  // on a desk.
  check(
    'the card in the bubble carries its entries and the one thread verb the rail gives it',
    bubble &&
      bubble.entries > 0 &&
      bubble.destroy &&
      !bubble.reply &&
      !bubble.resolve,
    bubble,
  );
  // A thread is resolved, never accepted. The verbs that used to be here are
  // the whole reason this task exists.
  check(
    'and offers no accept or reject — a conversation is not an edit to approve',
    bubble && bubble.accept === false,
    bubble,
  );
  // Task 1's hazard, arriving at the fifth surface: a hardcoded `{ where:
  // 'anchorless' }` draws a
  // live on-a-mark conversation dashed and captions it "not tied to a mark" —
  // while the reviewer is looking straight at the mark they just tapped.
  // Structural, for the reason the sheet's version above is: `.gly-adrift` is
  // the one class the "cannot point" verdict is written to, and the card's
  // sentence about it is copy.
  check(
    'a thread reached BY its mark is never drawn adrift',
    bubble && !!bubble.run && !bubble.adrift,
    bubble,
  );

  // §6's destroy weight, on the fifth surface. `.gly-bubble button` (0,1,1)
  // beats a bare `.gly-thread-delete` (0,1,0) exactly as `.gly-card button`
  // does — the specificity trap this whole file exists for, in a new place.
  const dels = await styleAll(
    '.gly-bubble .gly-thread-delete',
    'border-top-color',
    'color',
    'margin-left',
  );
  const muted = await rgb('--gly-muted');
  check(
    'the destroy weight renders in the bubble too',
    dels.length > 0 &&
      dels.every(
        (d) =>
          d['border-top-color'] === TRANSPARENT &&
          d.color === muted &&
          parseFloat(d['margin-left']) >= 32,
      ),
    { dels, muted },
  );

  // Elevation is orthogonal: the bubble is the floating thing whether it holds
  // two verbs or a whole conversation, so it carries the ONE elevation token
  // and not a second one invented for this state.
  const float = await style('.gly-bubble', 'box-shadow', 'z-index');
  check(
    'the bubble carrying a thread still floats on the one elevation token',
    float &&
      float['box-shadow'].includes('rgba(0, 0, 0, 0.16)') &&
      float['box-shadow'].includes('6px 22px'),
    float,
  );

  // IT HANGS BELOW THE MARK. Above the mark a card this tall covers the
  // sentence the conversation is about, which is the one sentence the reviewer
  // needs while they answer it. The two-verb bubble still sits above, and
  // topFor says at length why the two differ.
  const hang = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    const mark = document.querySelector('.gly-hl');
    const b = el.getBoundingClientRect();
    const m = mark.getBoundingClientRect();
    const head = document.querySelector('.gly-bar').getBoundingClientRect();
    const foot = document
      .querySelector('.gly-bottombar')
      .getBoundingClientRect();
    return {
      bubbleTop: b.top,
      bubbleBottom: b.bottom,
      markTop: m.top,
      markBottom: m.bottom,
      barBottom: head.bottom,
      footTop: foot.top,
      viewport: document.documentElement.clientHeight,
    };
  });
  check(
    'the conversation hangs BELOW its mark, leaving the sentence readable',
    hang.bubbleTop >= hang.markBottom - 0.5,
    hang,
  );
  // The READABLE BAND, not the window: the top bar is sticky over the head of
  // the page and the bottom bar is fixed over its foot, and a card clamped to
  // the window is drawn over one of them — measured, with a conversation
  // covering `✓ all`, the since-retired `✗ all` and the whole-doc handle.
  check(
    'and sits inside the band the prose is readable in, over neither bar',
    hang.bubbleTop >= hang.barBottom - 0.5 &&
      hang.bubbleBottom <= hang.footTop + 0.5,
    hang,
  );

  // READING IT MUST NOT CLOSE IT. threadCard makes every anchored card a
  // jump-to-its-mark control, which is right in the rail and wrong here: the
  // jump scrolls, a scroll dismisses the bubble, and most of this card's area
  // is prose the reviewer's eye and finger land on while reading. Measured
  // closing on a tap before the capture listener in SuggestionUI's constructor
  // was added.
  const stillOpen = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    const said = el.querySelector('.gly-thread-entry p');
    said.click();
    return { open: !el.hidden, title: el.querySelector('.gly-card').title };
  });
  check(
    'tapping the conversation to read it does not dismiss it',
    stillOpen.open === true,
    stillOpen,
  );
  check(
    'and it makes no jump-to-mark promise it no longer keeps',
    stillOpen.title === '',
    stillOpen,
  );
}

/** What the card is actually SHOWING, measured against the bubble's own box.
 *
 *  Written as one helper because every viewport below asks the same three
 *  questions and the first of them is the one four rounds of checks kept not
 *  asking: is any of the conversation ON SCREEN. `.gly-bubble-said` is the only
 *  region of the card that can give, so when the cap is smaller than the pinned
 *  chrome it gives all the way to ZERO — a card headed `THREAD · …`, with a
 *  reply box and both verbs, and not one word of what was said. Every other
 *  check in this file passes in that state, which is exactly why it is measured
 *  here as a height and not as a selector match. */
const cardShows = () =>
  page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    if (!el || el.hidden) return null;
    const card = el.querySelector('.gly-card.gly-thread');
    if (!card) return { thread: false };
    const b = el.getBoundingClientRect();
    // Visible means inside the bubble's box AND with a height of its own. Right
    // for the PINNED rows, which are never clipped by anything but the bubble.
    const visible = (node) => {
      if (!node) return false;
      const r = node.getBoundingClientRect();
      return (
        r.height > 0.5 && r.top >= b.top - 0.5 && r.bottom <= b.bottom + 0.5
      );
    };
    const said = card.querySelector('.gly-bubble-said');
    const entries = Array.from(card.querySelectorAll('.gly-thread-entry'));
    // And PAINTED, for the entries, which is a different question: an entry
    // inside a region squeezed to 1px has a full-height rect of its own and is
    // clipped to nothing. Its rect intersected with the region's is what a
    // reviewer can actually read, so that is what is measured. Reading the
    // rect alone is how "at least one entry is visible" passes on a card
    // showing a sliver.
    const box = said ? said.getBoundingClientRect() : null;
    const painted = (node) => {
      if (!box) return 0;
      const r = node.getBoundingClientRect();
      return Math.min(r.bottom, box.bottom) - Math.max(r.top, box.top);
    };
    const first = entries[0];
    return {
      thread: true,
      saidHeight: box ? box.height : -1,
      saidScroll: said ? said.scrollHeight : -1,
      entriesShown: entries.filter((e) => painted(e) > 0.5).length,
      entriesTotal: entries.length,
      firstHeight: first ? first.getBoundingClientRect().height : -1,
      firstPainted: first ? painted(first) : -1,
      // ONE VERB, NOT TWO. `.gly-thread-resolve` is not built any more — an
      // instruction is not a conversation to settle — so the pair this helper
      // reported is a pair with one dead half, and a dead half reads as
      // `visible(null) === false` forever. Delete is the verb a card carries,
      // and it is the one that must survive the cap: it is pinned chrome, and
      // the cap gives out of `.gly-bubble-said` before it takes from the row
      // the verb is in.
      deleteInView: visible(card.querySelector('.gly-thread-delete')),
      cardOverflow: Math.round(card.scrollHeight - card.clientHeight),
      height: b.height,
    };
  });

// A REAL PHONE, AND A CONVERSATION LONGER THAN ITS WINDOW. This is where the
// cap actually bites for a reviewer, so it is where the three claims capping
// makes are asserted — not at a contrived height where the design cannot work
// at all and the measurements are of its degradation.
{
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(700);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(300);
  await page.locator('.gly-hl').first().click();
  await page.waitForTimeout(400);

  const phone = await cardShows();
  check(
    'on a phone, a comment highlight opens its conversation',
    phone && phone.thread === true,
    phone,
  );
  // THE CAPPED-AND-SCROLLING STATE IS UNREACHABLE NOW, AND THE PREMISE CHECK IS
  // WHAT SAID SO.
  //
  // It stood here as the guard: a thread that fits needs no cap, and a check
  // written for the capped state that runs uncapped passes for the wrong reason
  // forever. It is deleted because the state it guards has no way in, not
  // because the guard was wrong — and the arithmetic is worth writing down,
  // because the obvious repair (make the fixture thread longer) does not work
  // and somebody will try it.
  //
  // `.gly-bubble-said` is the only region of the card that can give, and the
  // FLOOR under it guarantees room for the first thing anybody said, ENTIRE
  // (that is the check directly below, and it is the one that matters). With
  // eight replies the region could clamp to the room the mark left and let
  // replies two-through-eight scroll. An instruction is a SINGLE immutable
  // entry, so "the first thing anybody said" is the whole thread: the floor and
  // the cap are the same number, and there is no rest to scroll to.
  //
  // Measured while trying: a 665px single entry gives a 756px card that is
  // placed, not capped — at 900x1000 it flips above its mark rather than
  // clamping, and at 390x844 `saidScroll` (672) equals `saidHeight` (671.78),
  // which is a check reporting "nothing overflows" about a card that overflows
  // the window. Lengthening it further only moves the flip. The two constraints
  // are contradictory for one thread — hang BELOW the mark at 900x1000 wants
  // height <= ~697, cap at 390x844 wants > ~713 — and the bubble has a fixed
  // max-width, so the same text is the same height at both.
  //
  // The scroll check below it goes for the same reason and says so there.

  // THE ONE THE PINNING BROKE. .gly-bubble-said is the only part of the card
  // that can give, and it gave to zero — measured at 844×390 and at 390×430,
  // ordinary phone states (a rotation; the software keyboard opening under a
  // tapped reply box), with clientHeight 0 against scrollHeight 336 and no
  // gesture that could recover it. A card that shows a conversation with none
  // of the conversation in it is not a conversation.
  // WHOLLY, not by a sliver. `entriesShown > 0` is satisfied by a region 1px
  // tall — which is what the cap leaves when it is smaller than the pinned
  // chrome — and that is the same shape of check as the three this round is
  // fixing: it asserts the thing it names rather than the thing it means. The
  // property is the one the floor guarantees: room for the first thing anybody
  // said, entire.
  check(
    'and the conversation is actually on screen, not squeezed to nothing',
    phone &&
      phone.entriesShown > 0 &&
      phone.firstPainted >= phone.firstHeight - 0.5,
    phone,
  );

  check(
    'the card’s verb is reachable without scrolling at all',
    phone && phone.deleteInView === true,
    phone,
  );
  check(
    'and the card does not overflow its own box',
    phone && phone.cardOverflow <= 0,
    phone,
  );

  // AND THE SCROLL-INSIDE-THE-BUBBLE CHECK GOES WITH THE CAP. The gesture that
  // reaches the rest of the thread must not close the surface holding it —
  // `scroll` is listened for in the CAPTURE phase on `window`, so a scroll
  // raised on any descendant reaches it, including the bubble's own region the
  // moment overflow made it one. With one entry per thread nothing in the
  // bubble ever overflows, so the probe below finds no scrollable box and
  // reports `{scrollable: false}` on every run: there is no gesture to make and
  // no dismissal to catch. The window-scroll half of the same rule IS still
  // asserted — "tapping the conversation to read it does not dismiss it", above
  // — and it is the half a reviewer can still perform.
  //
  // Kept as a NOTE rather than deleted outright, because the day a thread grows
  // a second entry this line is the one that will say the state came back.
  const scrolled = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    const box = [el, ...el.querySelectorAll('*')].find(
      (n) => n.scrollHeight > n.clientHeight + 1,
    );
    if (!box) return { scrollable: false };
    box.scrollTop = box.scrollHeight;
    return new Promise((r) =>
      setTimeout(
        () => r({ scrollable: true, open: !el.hidden, at: box.scrollTop }),
        250,
      ),
    );
  });
  note(
    'nothing in the bubble overflows — one entry a thread, so no cap to scroll',
    scrolled,
  );
}

// NO ROOM BELOW IS THE CASE HANGING-BELOW GETS WRONG, and it is invisible in a
// tall window. With nothing under the mark, a `below` clamped up into the
// viewport is drawn OVER the mark — the one outcome both placements exist to
// avoid — so topFor falls back to the two-verb placement and the sentence stays
// visible either way.
//
// The SECOND highlight, the short one in the fifth paragraph: it has real room
// above it, which is what makes the flip a placement decision rather than a
// measurement of a window too small to place anything in. The premises are read
// back below for the same reason.
{
  await page.setViewportSize({ width: 900, height: 520 });
  await page.waitForTimeout(700);
  const marks = page.locator('.gly-hl');
  const low = marks.nth((await marks.count()) - 1);
  // PUT IT AT THE FOOT, don't hope it lands there. `scrollIntoViewIfNeeded`
  // only scrolls when the element is off screen and stops as soon as it is in
  // it, so where the mark ends up depends on how far the page happened to be
  // scrolled when this block started — and this block's whole premise is that
  // there is no room BELOW the mark. It used to land low enough by accident
  // (the last highlight in the fixture was two paragraphs from the end); the
  // fixture's marks moved, and the premise check went red reading
  // `roomBelow: true`, which is that guard doing its job. `block: 'end'` aligns
  // the mark's bottom with the window's, which is the state the check is about.
  // PUT THE MARK JUST ABOVE THE BOTTOM BAR, and compute where that is rather
  // than hoping. `scrollIntoViewIfNeeded` — what stood here — only scrolls when
  // the element is off screen and stops the moment it is in it, so where the
  // mark lands depends on how far the page happened to be scrolled when this
  // block started; it used to land low enough by accident (the last highlight
  // in the fixture was two paragraphs from the end), the fixture's marks moved,
  // and the premise check went red reading `roomBelow: true`. That is the guard
  // working, and this is the fixture answering it.
  //
  // `block: 'end'` alone is not the answer either: it aligns the mark's bottom
  // with the WINDOW's, which is UNDER the fixed `.gly-bottombar`, so the click
  // lands on the bar and every read below returns null. The target is the bar's
  // own measured top, less one line, which is a mark on screen with too little
  // room under it for a card — the state this block is about.
  await low.evaluate((el) => {
    const foot = document
      .querySelector('.gly-bottombar')
      .getBoundingClientRect().top;
    const want = el.getBoundingClientRect().bottom - (foot - 24);
    window.scrollBy(0, want);
  });
  await page.waitForTimeout(500);
  await low.click({ force: true });
  await page.waitForTimeout(400);

  const tight = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    const all = document.querySelectorAll('.gly-hl');
    const m = all[all.length - 1];
    if (!el || el.hidden) return null;
    const b = el.getBoundingClientRect();
    const r = m.getBoundingClientRect();
    const head = document.querySelector('.gly-bar').getBoundingClientRect();
    const foot = document
      .querySelector('.gly-bottombar')
      .getBoundingClientRect();
    return {
      thread: !!el.querySelector('.gly-card.gly-thread'),
      top: b.top,
      bottom: b.bottom,
      height: b.height,
      markTop: r.top,
      markBottom: r.bottom,
      barBottom: head.bottom,
      footTop: foot.top,
      // BOTH premises, read back rather than assumed. Without the first this
      // measures the ordinary below-placement and passes for the wrong reason;
      // without the second it measures a window too short for the card at all,
      // where covering the mark is the honest outcome and not a defect.
      roomBelow: foot.top - 4 - (r.bottom + 8) >= b.height,
      roomAbove: r.top - 8 - (head.bottom + 4) >= b.height,
      overlapsMark: b.top < r.bottom && b.bottom > r.top,
    };
  });
  check(
    'a mark with nothing under it still opens its conversation',
    tight && tight.thread,
    tight,
  );
  check(
    'there really is no room below it — otherwise this proves nothing',
    tight && tight.roomBelow === false,
    tight,
  );
  check(
    'and there really is room above it — otherwise this measures a window, not a placement',
    tight && tight.roomAbove === true,
    tight,
  );
  check(
    'with no room below, the card goes above rather than over the mark',
    tight && !tight.overlapsMark,
    tight,
  );
  check(
    'and still sits inside the band the prose is readable in',
    tight &&
      tight.top >= tight.barBottom - 0.5 &&
      tight.bottom <= tight.footTop + 0.5,
    tight,
  );
}

// AND WHEN THE WINDOW IS TOO SHORT FOR THE DESIGN AT ALL. 844×390 is a phone in
// landscape and 900×260 is the contrived case the checks above used to run in;
// in both, the room the mark leaves is smaller than the card's own floor, so
// something has to give and the choice of WHAT is the whole content of this
// block. It is not the conversation. The card overflows the room instead —
// topFor's clamps have always said a card can be taller than its window — and
// covering a little more prose is a degradation a reviewer can work around,
// where a card with nothing in it is not.
for (const size of [
  { width: 844, height: 390 },
  { width: 900, height: 260 },
]) {
  await page.setViewportSize(size);
  await page.waitForTimeout(700);
  // THE MARK HAS TO BE SOMEWHERE THE REVIEWER COULD PRESS IT, and `scrollTo(0,
  // 0)` is not that statement — it was only ever a way to get a mark near the
  // top of a short window, and it held by luck. The narrow layout puts a fixed
  // bar across the FOOT of the window and a sticky one across its head, so the
  // reachable band is neither the document nor the window. When the top bar
  // grew a row (it folds rather than pushing controls off screen — see §7a),
  // everything under it moved down 40px and the first mark landed beneath the
  // bottom bar at 260px tall: `elementFromPoint` read `.gly-bar-count`, the
  // click never reached the prose, and all four assertions below failed on a
  // premise rather than on the thing they measure.
  //
  // So the mark is put in the middle of the band the product ITSELF calls
  // readable — App.chromeFrame, the same number the bubble places against —
  // and that it arrived is asserted rather than assumed. A precondition that
  // silently stops holding is how a block like this comes to measure nothing.
  await page.evaluate(() => {
    window.scrollTo(0, 0);
    const frame = window.galleyEdit.app.chromeFrame();
    const r = document.querySelector('.gly-hl').getBoundingClientRect();
    window.scrollBy(0, r.top + r.height / 2 - (frame.top + frame.bottom) / 2);
  });
  await page.waitForTimeout(300);
  const where = `${size.width}×${size.height}`;
  const pressable = await page.evaluate(() => {
    const r = document.querySelector('.gly-hl').getBoundingClientRect();
    const at = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return {
      hit: at ? at.className || at.tagName : 'none',
      ok: !!(at && at.closest('.gly-hl')),
    };
  });
  check(
    `at ${where} the mark can be pressed at all — no chrome is over it`,
    pressable.ok,
    pressable,
  );
  await page.locator('.gly-hl').first().click();
  await page.waitForTimeout(400);

  const shown = await cardShows();
  check(
    `at ${where} the conversation still opens`,
    shown && shown.thread === true,
    shown,
  );
  check(
    `at ${where} the first thing that was said is on screen, whole`,
    shown &&
      shown.entriesShown > 0 &&
      shown.firstPainted >= shown.firstHeight - 0.5,
    shown,
  );
  check(
    `at ${where} the card’s verb is still reachable`,
    shown && shown.deleteInView === true,
    shown,
  );
  // `overflow: hidden` replaced `overflow-y: auto` when the entries became the
  // one region that scrolls, so anything overflowing the CARD now has no
  // scrollbar to recover it. Measured at 3px in these two windows before the
  // floor — absorbed by the bubble's padding, and invisible right up until it
  // is not.
  check(
    `at ${where} the card does not overflow its own box`,
    shown && shown.cardOverflow <= 0,
    shown,
  );
}

await page.setViewportSize(WIDE);
await page.waitForTimeout(500);

// --- §7d · and at wide, the rail keeps the conversation ----------------------
//
// A RULE ASSERTED ON ONE SIDE OF A BREAKPOINT IS HALF A RULE. At or above
// RAIL_MIN_WIDTH the rail already draws every open thread, so a bubble drawing
// the same one would be one conversation rendered twice, in two places, each
// with its own reply box and its own delete. The bubble does not draw it there,
// and this is what says so.
//
// WHAT THE CLICK DOES INSTEAD IS NOT "NOTHING", AND THIS CHECK USED TO ACCEPT
// THAT IT WAS. It required the bubble to be OPEN and merely thread-less —
// which the two-verb bubble satisfied: `COMMENT · AGENT · JUST NOW` with an
// accept and a reject that 404, no comment text, no conversation, no reply box.
// The one-conversation-one-place rule was upheld and the reviewer's most
// instinctive click still landed on the product's worst surface. §11 above is
// where the whole gesture is read; this is the breakpoint's half of it.
{
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(300);
  const mark = page.locator('.gly-hl').first();
  await mark.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  await mark.click();

  // POLLED, NOT READ ONCE. `flash()` puts `.gly-flash` on the card for
  // FLASH_MS (1400) and `paintRail` destroys and rebuilds every card on the
  // 1.5s poll, so a single read at a fixed delay is a race between the two —
  // and the answer it gives when it loses is `false`, which reads as "the click
  // did nothing" rather than as "this check has a timing bug".
  const flashed = await page.evaluate(
    () =>
      new Promise((resolve) => {
        const started = Date.now();
        const look = () => {
          if (document.querySelector('.gly-rail .gly-thread.gly-flash'))
            return resolve(true);
          if (Date.now() - started > 2000) return resolve(false);
          window.requestAnimationFrame(look);
        };
        look();
      }),
  );
  await page.waitForTimeout(400);
  const wide = await page.evaluate(() => {
    const el = document.querySelector('.gly-bubble');
    return {
      open: !!el && !el.hidden,
      // SHOWING, not merely holding. A hidden bubble keeps its last render in
      // the DOM — `hide()` sets `hidden` and nothing else — so a bare
      // querySelector here reports a conversation the reviewer cannot see, and
      // the claim is about what is on screen.
      thread: !!el && !el.hidden && !!el.querySelector('.gly-card.gly-thread'),
      verbs:
        el && !el.hidden
          ? el.querySelectorAll('.gly-bubble-accept, .gly-bubble-reject').length
          : 0,
      railThreads: document.querySelectorAll('.gly-rail .gly-card.gly-thread')
        .length,
    };
  });
  check(
    'at wide the bubble does not draw the conversation — the rail already has it',
    wide.thread === false && wide.railThreads > 0,
    wide,
  );
  check(
    'and it does not draw a stub of it either — no bubble, and no verbs on a conversation',
    wide.open === false && wide.verbs === 0,
    wide,
  );
  check(
    'the click goes to the card instead, which is reveal() in the other direction',
    flashed === true,
    { flashed, wide },
  );
}

// --- §7e · the collapsed rail — DELETED WITH THE COLLAPSE CONTROL ------------
//
// THE RULE IT GUARDED IS STILL GUARDED, and that is why this is a deletion and
// not a hole. §7e's subject was a STATE, not a claim: collapsed at 1600px,
// `railSurfaces` rendered no rail, no bar and no sheet, so a width test left a
// comment highlight opening a two-verb bubble with the conversation rendered
// NOWHERE — and it was not a corner, because `writeCollapsed` persisted per
// document, so a reviewer who collapsed the rail once opened every later
// session in that state. The block pressed `.gly-collapse`, asserted the rail
// really had gone, and then read the bubble.
//
// There is no collapse control. `makeCollapse` was called from nowhere and
// has since been deleted from entry.ts entirely, along with `toggleCollapse`
// and the rest of the feature: Instructions and History are the two document
// views now, and an arrow-only collapse made the primary view disappear
// behind a glyph. The state was unreachable from the product before the
// deletion too — a `locator('.gly-collapse').click()` simply hung, which is
// how this was found, thirty seconds of waiting for a button nobody built.
//
// The RULE — "is the rail on screen", not "is the window narrow", answered by
// `railSurfaces` and never by a width test — is what §7c and §7d assert
// between them, on the two sides of the breakpoint that the product can still
// reach.

// --- §9 · the rail speaks ONE card language ---------------------------------
//
// A REVISION IS NOT A DIFFERENT KIND OF THING FROM ITS OWN EDITS, and for a
// while the rail said it was. `batchCard` drew the arrival of a revision as a
// receipt — its own container (the anchorless block, in flow, below the band,
// so its own left edge and its own width), its own visual language (a dashed
// accent border and a tinted background, "reads as a receipt rather than as
// another suggestion" in the stylesheet's own words), and its own rows, which
// listed the very edits the band was already carding a few pixels away. Three
// spellings of one arrival, and the third is why nothing short of deletion
// fixed it: the same edit was on the screen twice.
//
// IT ALSO TOOK ITS HEIGHT FROM THE MAP. That block was `flex: none` beside a
// `flex: 1` band, so the band got what the block left — a revision's card,
// capped at 40% of the rail, took that 40% out of the column whose whole job is
// positioning cards against their marks, and clipped the card above it. (Both
// the cap and the budget it defended are gone with the fixed column; what the
// two checks below still assert is the GEOMETRY, which is not.)
//
// So this block asks the two questions deletion is the answer to, and it asks
// them IN THE STATE THE CARD EXISTED IN: a revision asked for, and a revision
// landed. Nothing is asserted about an empty rail — the card never rendered on
// one, and a check that reads a state the bug could not reach is a check that
// cannot fail.
//
// NOTHING IS LOST BY ITS GOING, which is why the claim below is only about the
// language and not about the reach: bulk accept/reject is `✓ all` in the census
// strip over the same population, and stepping through what arrived is `j`/`k`
// through `stepOrder`. Both are asserted elsewhere in this file and in
// motion.mjs.
//
// BEFORE §8a and §8, because both of those end with a review that has been
// sealed, and a sealed server refuses the `galley suggest` this block needs.
console.log('\n--- §9 · the rail speaks one card language ---');
{
  const railGeometry = () =>
    page.evaluate(() => {
      const box = (sel) => {
        const el = document.querySelector(sel);
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return {
          y: Math.round(r.y),
          w: Math.round(r.width),
          h: Math.round(r.height),
          left: Math.round(r.left),
        };
      };
      // EVERY CARD IN THE RAIL, whatever it is in. There used to be two
      // containers to name here and the second one was the point; the rail
      // holds live work only now, so there is the band and a notice block that
      // holds no cards at all. `.gly-rail .gly-card` is the honest spelling of
      // the question — a card in the rail, anywhere — and it is what would see
      // a third container the day somebody adds one.
      const cards = Array.from(
        document.querySelectorAll('.gly-rail .gly-card'),
      ).map((el) => {
        const r = el.getBoundingClientRect();
        return {
          cls: el.className,
          left: Math.round(r.left),
          w: Math.round(r.width),
          border: getComputedStyle(el).borderTopStyle,
          // Which of the two things in the rail this is: a CONVERSATION, or a
          // SENTENCE about the ones that are not shown. The notice block's
          // sentences wear `.gly-card` for the chrome and are dashed, muted and
          // centred by `.gly-settled` — so several claims below have to tell
          // them apart, and the container is the only honest discriminator (a
          // sentence has no run, no key and no verbs; that is what makes it a
          // sentence).
          said: !!el.closest('.gly-rail-notice'),
        };
      });
      return {
        rail: box('.gly-rail'),
        band: box('.gly-rail-band'),
        // The one section after the map, so "after" can be arithmetic.
        notice: box('.gly-rail-notice'),
        cards,
        // Not `.gly-batch` — the claim is that NO element wears the receipt's
        // language, and a receipt reintroduced under `gly-batch-row` alone
        // would satisfy a check written for the card's own class.
        receipts: document.querySelectorAll('[class*="gly-batch"]').length,
        // THE THREE SECTIONS THAT LEFT. Read as counts rather than as boxes,
        // because the claim about each is now that it is not here.
        gone: document.querySelectorAll(
          '.gly-rail-anchorless, .gly-rail-settled, .gly-rail-changed',
        ).length,
        // Unplaced instruction cards live in this flow block. Their count must
        // remain stable across an unrelated revision, and the geometry checks
        // below hold them to the same gutter and width as positioned cards.
        noticeCards: document.querySelectorAll(
          '.gly-rail-notice .gly-card.gly-thread, .gly-rail-notice .gly-card[data-run]',
        ).length,
        // And what it DOES hold, so a sentence can be read back rather than
        // merely counted.
        noticeSaid: (
          document.querySelector('.gly-rail-notice')?.textContent || ''
        ).trim(),
      };
    });

  // THE REVISION IS ASKED FOR THROUGH THE ENDPOINT, NOT THE BUTTON. With work
  // pending the button DISCLOSES a two-exit menu rather than posting (see
  // askRevise), and this block is not a check about that menu — motion.mjs
  // drives it. What matters here is the state the page ends up in.
  const asked = await page.evaluate(async () => {
    const res = await fetch('/_galley/revise', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    });
    return res.status;
  });
  // The page polls `/_galley/revise` every 1500ms; two beats is the window in
  // which it learns a revision is outstanding.
  await page.waitForTimeout(3500);

  // AND THE AGENT HANDS THE FILE BACK, which is a step this block did not used
  // to have and cannot now skip. A press of Revise SENDS the round: it captures
  // every pending instruction, clears the list and opens a handoff window in
  // which the document is read-only — `/_galley/instruct` answers
  // `409 the agent is revising` until the window closes, which is how this was
  // found. `galley ack` is what closes it, and it is the agent's own verb.
  // `cannot`, not `ack --state answered`: an answered round has to have CHANGED
  // the file, and this block changes nothing — `galley ack` refuses outright
  // ("nothing has changed since the round was handed over"), which is the
  // server keeping the agent honest and is exactly right. An unchanged round
  // reported as an exception is the truth here and closes the window the same
  // way.
  galley(
    'cannot',
    DOC,
    '--why',
    'this round is the geometry fixture, not work to do',
  );
  await page.waitForTimeout(3000);

  // THE BASELINE IS TAKEN HERE, AFTER THE ROUND WENT. It used to be taken
  // before the ask, because a revision left the pending set where it was — the
  // agent proposed ON TOP of it. A sent round is gone from the rail, so a
  // baseline from before the press is a count this block would have to go
  // backwards from.
  const before = await railGeometry();

  // And more work lands from OUTSIDE THIS PAGE, the way it does when a second
  // reviewer or a tool writes to the same document. Two `--replace` proposals
  // stood here beside a comment, because a revision was whatever the agent had
  // proposed in it; the agent does not propose, so what is driven here is what
  // the server still accepts from another process — instructions — and the
  // property under test is unchanged: cards the page did not create arrive and
  // the rail's geometry answers for them.
  await instruct({
    op: 'comment',
    target: 'A seventh paragraph',
    text: 'is this the right word?',
  });
  await instruct({
    op: 'comment',
    target: 'the word omega',
    text: 'omega is not the word this wants',
  });
  await instruct({
    op: 'comment',
    target: 'the word sigma',
    text: 'and neither is sigma',
  });
  await page.waitForFunction(
    (n) => document.querySelectorAll('.gly-rail .gly-card').length > n,
    before.cards.length,
    { timeout: 20000 },
  );
  // Long enough for the collect-then-drain handoff between the two polls to
  // finish, and for the rail to be repainted from the settled pending set.
  await page.waitForTimeout(4000);

  const after = await railGeometry();
  note('the rail with a revision just landed', after);

  check(
    'the work really arrived — a check with nothing to read is not a check',
    asked === 204 && after.cards.length > before.cards.length,
    { asked, was: before.cards.length, now: after.cards.length },
  );
  check(
    "nothing in the rail wears a revision receipt's own language",
    after.receipts === 0,
    after.receipts,
  );
  // A DASHED EDGE IS THIS CODEBASE'S "CANNOT POINT", and that is a STATE on an
  // ordinary card, not a second language. `.gly-card.gly-adrift` dashes the
  // border and hides the connector because the thread's mark is gone or
  // ambiguous — the fixture's anchorless thread is exactly that, and the card
  // says so. The receipt's dash was different in the only way that matters: it
  // was a card that COULD point, dressed as another kind of object. So the
  // claim is "no card wears a dash it has not earned", and the earning is a
  // named class rather than a container.
  // A NOTICE SENTENCE EARNS ITS DASH TOO, and by the same rule: `.gly-settled`
  // is the notice costume — dashed, muted, centred, `cursor: default` — worn by
  // "nothing pending — the document is settled" and by the sentence about a
  // conversation with no place. Neither is a card that COULD point dressed as
  // another kind of object, which is what the receipt's dash was. The check
  // immediately below is what stops this excuse from ever covering a
  // conversation: nothing but sentences may be in that block at all.
  const strays = after.cards.filter(
    (c) => c.border === 'dashed' && !c.cls.includes('gly-adrift') && !c.said,
  );
  check(
    'and no card in it is drawn in a second language — no unearned dashed edges',
    strays.length === 0,
    strays,
  );
  // THE RAIL HOLDS LIVE WORK ONLY, and that is asserted by NAME rather than
  // left to the geometry below, because three named sections went and each
  // could be put back by one line. Run red against the tracked bundle it
  // reports 3 — the anchorless block, the settled region and the changed
  // region, all present with a revision on screen.
  check(
    'the rail is the map and one flow region — the three history sections are gone',
    after.gone === 0 &&
      after.notice !== null &&
      after.noticeCards === before.noticeCards,
    { gone: after.gone, notice: after.notice, noticeCards: after.noticeCards },
  );
  // ONE LEFT EDGE AND ONE WIDTH, AGAINST THE RAIL'S OWN BOX. This used to read
  // two containers, and it had to: the anchorless block laid its cards out IN
  // FLOW while the band absolutely places and insets its own, so the two
  // disagreed — `1280/304` against `1306/278`, which is exactly the pair the
  // deleted revision receipt was deleted for. Over the band alone a set of one
  // edge and one width is arithmetic and not evidence.
  //
  // So the second container is replaced by a second COORDINATE: every card's
  // left edge is GUTTER_PX from the rail's own, which is a fact about the
  // page rather than about the band's internal consistency. A card rendered at
  // the rail's edge — the exact failure the anchorless block used to produce —
  // fails this with one container just as it failed the old check with two.
  // THE VISIBLE CARDS ARE THE MAP. The capture card collapses to a zero box
  // when shut (`display: none`, like every other hideable surface in this
  // file's own `[hidden]` list), so it has no edge or width to share and is not
  // one of the placed cards here. The one-edge rule is READ OFF THE CAPTURE
  // CARD ITSELF, OPENED, in the dedicated check below — where it is a real card
  // with real geometry that must match, which is a stronger claim than the
  // reserved box this used to lean on and is the whole of what Court asked for.
  const placed = after.cards.filter((c) => c.w > 0);
  const edges = new Set(placed.map((c) => c.left));
  const widths = new Set(placed.map((c) => c.w));
  check(
    'every card in the rail shares one left edge and one width',
    placed.length > 1 && edges.size === 1 && widths.size === 1,
    { edges: [...edges], widths: [...widths], cards: placed.length },
  );
  check(
    'and that edge is the reserved gutter from the rail\u2019s own left, not the rail\u2019s edge',
    edges.size === 1 && [...edges][0] - after.rail.left === GUTTER_PX,
    { edge: [...edges][0], rail: after.rail.left, want: GUTTER_PX },
  );
  // AND THE ONE-EDGE RULE IS READ OFF THE CAPTURE CARD ITSELF, OPENED. It is a
  // card in the whole-document panel's flow now, not the reserved overlay it
  // was, so it shares the column's one left edge and one width like every other
  // card — which is the whole of what Court asked for. It collapses when shut
  // rather than holding a box open, so the claim is made on the OPEN card,
  // where it is real rendered geometry and not a held-open ghost. Shut, it is
  // `display: none` and out of the query above; open, it must line up here.
  await page.locator('.gly-bar .gly-capture-open').click();
  await page.waitForSelector('.gly-capture:not([hidden])');
  await page.waitForTimeout(300);
  const captureEdge = await page.evaluate(() => {
    const el = document.querySelector('.gly-capture');
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { left: Math.round(r.left), w: Math.round(r.width) };
  });
  check(
    'the capture card shares the rail’s one left edge and width when open',
    captureEdge &&
      edges.size === 1 &&
      widths.size === 1 &&
      captureEdge.left === [...edges][0] &&
      captureEdge.w === [...widths][0],
    { captureEdge, edge: [...edges][0], width: [...widths][0] },
  );
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
  // The existing unplaced instruction remains in flow; the unrelated agent
  // revision must not add another card there or move the section.
  check(
    'and the revision produced no new unplaced instruction',
    after.noticeCards === before.noticeCards &&
      after.notice.h <= before.notice.h + 1,
    { was: before.notice, now: after.notice, cards: after.noticeCards },
  );
  // THE SECTIONS BEGIN AFTER THE MAP ENDS, which is what replaced "the band was
  // not squeezed by what arrived". That check compared the band's height
  // against the rail's minus every sibling's, because the band was `flex: 1` in
  // a column the viewport's height fixed and every pixel a sibling took was a
  // pixel of map. There is no height budget to conserve now — the band is as
  // tall as its own cards and the rail is as tall as all of it — so the honest
  // claim is the ORDER: nothing the rail puts after the map begins before the
  // map has finished. It is what makes "expanding a section displaces only what
  // is below it" true, and it goes red the day a section is floated back over
  // the band.
  check(
    'the sections at the end of the rail begin after the map ends',
    after.notice.y >= after.band.y + after.band.h - 1,
    { band: after.band, notice: after.notice },
  );
}

// --- §10 · the rail scrolls with the document -------------------------------
//
// `.gly-rail` was `position: fixed` — a viewport-locked column holding
// unbounded content — and every other mechanism in the rail existed to manage
// the overflow that guarantees: `overflow: hidden` on the band, the two fold
// clusters, `FOLD_CAP` and its "+N more" line, and two lists floating upward
// over the map. Court, from use: *"i can't scroll to see the response to
// every? there are also lines to the left of the rail. why?? settled still
// overrides the rest of the threads. changed then overrides that."*
//
// The rail is as tall as the document now and scrolls with it, so a card is
// beside its text by the document's own scroll rather than by arithmetic
// against the viewport. These are the four properties that replaces five
// mechanisms with, and they are asserted where the mechanisms used to be
// asserted.
//
// A SHORT WINDOW, because every one of these claims is about work that is NOT
// on screen — the state the fold existed for. At WIDE the fixture's document
// very nearly fits, every card would be live, and all four checks would read a
// state the old design handled correctly and pass forever. Measured at 1600
// wide: the fixture's marks sit at 195, 270, 345, 420 and 495, so 400px of
// window leaves the first two beside their text and puts the rest below the
// fold — which is precisely where the old rail clamped, dimmed and clipped
// them.
console.log('\n--- §10 · the rail scrolls with the document ---');
{
  await page.setViewportSize({ width: 1600, height: 400 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(900);

  /** Every card in the band paired with the mark it claims, off the DOM alone.
   *  The pairing is `data-run` on both sides — the mark's own identity, carried
   *  into the fragment by the suggestion mark's renderHTML — so this reads what
   *  is TRUE of the page rather than re-running the rail's own arithmetic
   *  against itself. */
  const pointing = () =>
    page.evaluate(() => {
      const out = [];
      for (const card of document.querySelectorAll(
        '.gly-rail-band .gly-card',
      )) {
        const run = card.dataset.run;
        if (!run) continue;
        const mark = document.querySelector(
          `.ProseMirror [data-run="${CSS.escape(run)}"]`,
        );
        if (!mark) continue;
        const c = card.getBoundingClientRect();
        const m = mark.getBoundingClientRect();
        out.push({
          run: run.slice(0, 8),
          cardY: +c.top.toFixed(1),
          markY: +m.top.toFixed(1),
        });
      }
      return out;
    });

  const points = await pointing();
  note('every card in the band, beside the mark it is about', points);
  check(
    'the band still holds cards whose marks are in the prose — a check with nothing to read is not a check',
    points.length > 1,
    points.length,
  );

  // --- lighting the text: which words is this card about -------------------
  //
  // THE LINE IS DELETED AND THE WORDS ANSWER INSTEAD, and these checks are what
  // replaced its. The connector failed twice for one reason: it was invented to
  // bridge the gap when a card had drifted from its mark, and it read as
  // meaningful only because it was LONG. Once the rail scrolled with the
  // document and cards sat beside their text the leg collapsed and a 26px stub
  // crossed a 376px gutter; aimed at the card's middle instead it stopped
  // striking horizontally and began sagging diagonally THROUGH the paragraph
  // the reviewer is reading — measured at 250px of sag with the card and its
  // mark 0px apart vertically. The gutter is too wide for a line to cross
  // without being the loudest thing on the page, and the question it answered
  // is answered better by the words changing.
  //
  // So: nothing at rest, hovering a card lights ITS OWN WORDS in the prose,
  // hovering the words lights the card, and the same from the keyboard. Every
  // one is red against the tracked bundle, which has no `.gly-lit` anywhere.
  //
  // FIVE CHECKS ARE DELETED WITH THEIR SUBJECT AND EACH IS NAMED IN THE COMMIT:
  // `the connector ends in a dot ON the word` and `it is one cubic segment`
  // were claims about a curve; `with reduced motion the line is still drawn`
  // guarded a DRAW that no longer exists (the light is instant — there is no
  // motion left to withhold, so honouring the preference is a no-op rather than
  // a rule); `it is a HAIRLINE — a heavier stroke reads as a highlight, not a
  // thread` was guarding against the thing that is now the design. The sixth,
  // `an adrift card draws no connector`, carried a second property and is
  // REWRITTEN below rather than dropped.
  const lit = () =>
    page.evaluate(() => {
      const prose = Array.from(
        document.querySelectorAll('.ProseMirror .gly-lit'),
      );
      const cards = Array.from(
        document.querySelectorAll('.gly-rail-band .gly-card.gly-lit'),
      );
      return {
        prose: prose.length,
        cards: cards.length,
        cardRuns: cards.map((el) => (el.dataset.run || '').slice(0, 8)),
        // The OLD mechanism, asserted absent everywhere it could come back: the
        // overlay, the two spans that preceded it, and any line at all.
        connector: document.querySelectorAll(
          '.gly-connector, .gly-connector-arm, .gly-connector-leg',
        ).length,
      };
    });

  const atRest = await lit();
  check(
    'at rest nothing is lit — and no connector survives anywhere on the page',
    atRest.prose === 0 && atRest.cards === 0 && atRest.connector === 0,
    atRest,
  );

  // The card whose words are lit is the topmost one with a mark, chosen off the
  // page rather than written down.
  const target = points[0].run;
  const cardSel = `.gly-rail-band .gly-card[data-run^="${target}"]`;
  const markSel = `.ProseMirror [data-run^="${target}"]`;

  await page.locator(cardSel).hover();
  await page.waitForTimeout(300);
  const onCard = await lit();
  check(
    'hovering a card lights words in the prose, and lights itself',
    onCard.prose > 0 &&
      onCard.cards === 1 &&
      onCard.cardRuns[0] === target.slice(0, 8),
    onCard,
  );

  // AND IT IS THE CARD'S OWN WORDS. This is what replaced `the connector ends
  // in a dot ON the word`: the dot's claim was that the line reached the right
  // text, and the light makes that claim over the whole EXTENT rather than at
  // one point. A light on the paragraph, or spilling into the prose beside the
  // mark, fails this where a check counting `.gly-lit` elements would not.
  //
  // IT RESOLVES EACH LIT SPAN BACK TO A RUN RATHER THAN COMPARING ONE BOX, and
  // that is not fussiness — the first cut of this check compared the glow's
  // union against `[data-run^=target]` and reported 737.9 against 698.8, a
  // genuine 39px, because the topmost card on this fixture is a REPLACE and
  // BOTH HALVES LIGHT. One span in the file is one decision everywhere, and the
  // card's `run` is the deleted half's, so a check written against that one run
  // was asserting half of the design. What is true regardless of kind is: every
  // lit span belongs to a marked run, the card's own run is among them, and the
  // light covers those runs' marks and nothing else.
  const landed = await page.evaluate((sel) => {
    const runOf = (el) => {
      const own = el.closest('[data-run]');
      if (own) return own.getAttribute('data-run');
      const inner = el.querySelector('[data-run]');
      return inner ? inner.getAttribute('data-run') : null;
    };
    const union = (els) => {
      let b = null;
      for (const el of els) {
        for (const r of el.getClientRects()) {
          if (!r.width && !r.height) continue;
          b = b
            ? {
                x1: Math.min(b.x1, r.left),
                y1: Math.min(b.y1, r.top),
                x2: Math.max(b.x2, r.right),
                y2: Math.max(b.y2, r.bottom),
              }
            : { x1: r.left, y1: r.top, x2: r.right, y2: r.bottom };
        }
      }
      return (
        b && {
          x1: +b.x1.toFixed(1),
          y1: +b.y1.toFixed(1),
          x2: +b.x2.toFixed(1),
          y2: +b.y2.toFixed(1),
        }
      );
    };
    const glowing = Array.from(
      document.querySelectorAll('.ProseMirror .gly-lit'),
    );
    const runs = glowing.map(runOf);
    const named = [...new Set(runs.filter(Boolean))];
    const marks = named.length
      ? document.querySelectorAll(
          named
            .map((r) => `.ProseMirror [data-run="${CSS.escape(r)}"]`)
            .join(', '),
        )
      : [];
    return {
      spans: glowing.length,
      // A lit span with no run at all is a light on unmarked prose.
      unmarked: runs.filter((r) => !r).length,
      runs: named.map((r) => r.slice(0, 8)),
      glow: union(glowing),
      mark: union(marks),
    };
  }, markSel);
  note('what the light covers, against the marks it claims', landed);
  check(
    'every lit span belongs to a marked run — the light never lands on plain prose',
    landed.spans > 0 && landed.unmarked === 0,
    landed,
  );
  check(
    'and the card’s own run is among them',
    landed.runs.includes(target.slice(0, 8)),
    landed,
  );
  check(
    'and the light covers those runs’ marks exactly, not the paragraph',
    !!landed.glow &&
      !!landed.mark &&
      Math.abs(landed.glow.x1 - landed.mark.x1) <= 6 &&
      Math.abs(landed.glow.x2 - landed.mark.x2) <= 6 &&
      Math.abs(landed.glow.y1 - landed.mark.y1) <= 6 &&
      Math.abs(landed.glow.y2 - landed.mark.y2) <= 6,
    landed,
  );

  // AND IT IS NOT CONFUSABLE WITH THE TWO POPULATIONS ALREADY IN THE PROSE.
  // The trail ghost borrowed the agent's del-red for a whole phase and this
  // file's check certified it, because each colour was read against a TOKEN and
  // never against the other. So this is the inequality, as that one now is: the
  // light shares no background with any of the three suggestion marks, and it
  // carries no text-decoration at all, which is the ghost's whole shape.
  const apart = await page.evaluate(() => {
    const of = (sel) => {
      const el = document.querySelector(sel);
      if (!el) return null;
      const cs = getComputedStyle(el);
      return {
        bg: cs.backgroundColor,
        deco: cs.textDecorationLine,
        color: cs.color,
      };
    };
    return {
      lit: of('.ProseMirror .gly-lit'),
      ins: of('.ProseMirror .gly-ins'),
      del: of('.ProseMirror .gly-del'),
      hl: of('.ProseMirror .gly-hl'),
    };
  });
  note('the light against the marks it must not be read as', apart);
  // ONE MARK TO BE APART FROM, NOT THREE. `.gly-ins` and `.gly-del` have no
  // writer in the prose — the agent edits the file rather than proposing
  // against the document — so those two halves of the inequality read `null` on
  // every run, and a check that requires `apart.ins` to exist is a check that
  // can only fail. The comment highlight is the mark the light can actually be
  // confused with, since it is the one drawn under words the rail has a card
  // for, and it is the comparison that was doing the work. The DECORATION half
  // is unchanged and stands on its own: the light wears no strike and no rule,
  // which is what keeps it out of the ghost's vocabulary whether or not
  // anything else is on the page to contrast with.
  check(
    'the light is not the comment mark’s wash, and wears no strike or rule',
    !!apart.lit &&
      !!apart.hl &&
      apart.lit.bg !== apart.hl.bg &&
      apart.lit.deco === 'none',
    apart,
  );

  // FROM EITHER END. Hovering the WORD lights the card, which is the half a
  // card-only implementation would silently not have.
  await page.mouse.move(0, 0);
  await page.waitForTimeout(250);
  check(
    'moving off puts the light out',
    (await lit()).prose === 0 && (await lit()).cards === 0,
  );
  await page.locator(markSel).first().hover();
  await page.waitForTimeout(300);
  const onWord = await lit();
  check(
    'hovering the WORD lights its card — either end, one mechanism',
    onWord.cards === 1 &&
      onWord.prose > 0 &&
      onWord.cardRuns[0] === target.slice(0, 8),
    onWord,
  );

  // AND ON KEYBOARD FOCUS. A card is `tabIndex = 0`, so this is reachable
  // without a pointer at all — the accessibility half of "only while
  // attending", and the half a hover-only implementation drops.
  await page.mouse.move(0, 0);
  await page.waitForTimeout(250);
  check('and off again', (await lit()).prose === 0);
  await page.locator(cardSel).focus();
  await page.waitForTimeout(300);
  const onFocus = await lit();
  check(
    'focusing a card from the keyboard lights its words',
    onFocus.prose > 0 && onFocus.cards === 1,
    onFocus,
  );
  check(
    'and the focus really is on the card, so the check is not a hover in disguise',
    await page.evaluate(
      (sel) => document.activeElement === document.querySelector(sel),
      cardSel,
    ),
  );
  await page.evaluate(
    () => document.activeElement && document.activeElement.blur(),
  );
  await page.waitForTimeout(250);
  check('and blurring puts it out', (await lit()).prose === 0);

  // THE LIGHT IS A DECORATION AND THE CARD'S CLASS IS RE-APPLIED, WHICH IS ONE
  // CLAIM WITH TWO HALVES AND ONE GESTURE THAT BREAKS BOTH.
  //
  // `paintRail` destroys and rebuilds every card on every pending refresh, and
  // ProseMirror rewrites a rendered element's attributes from the node on every
  // redraw — which follows every server-side mutation, each of which replaces
  // the whole document. This file already records both: a class walked onto
  // `.gly-note` was wiped a moment later, and a half-typed reply was destroyed
  // with its card. So a real mutation is driven from another terminal WHILE the
  // card is lit, and both ends are read back afterwards. A class-on-the-mark
  // implementation loses the prose half here and passes everything above it.
  await page.locator(cardSel).hover();
  await page.waitForTimeout(300);
  check(
    'lit before the mutation — a check with nothing to read is not a check',
    (await lit()).prose > 0,
  );
  await instruct({
    op: 'comment',
    target: 'A sixth paragraph',
    text: 'a mutation while the light is on',
  });
  await page.waitForTimeout(4000);
  const survived = await lit();
  note('the light after a whole-document rebuild and a rail repaint', survived);
  // THE CARD UNDER THE POINTER, READ AFTER THE MUTATION — not the card that was
  // under it before. This compared against `target`, the run picked before the
  // hover, and that was sound while a mutation only ever ADDED a card below the
  // one being hovered. An instruction landing on `A sixth paragraph` inserts a
  // card into the middle of the band, so the stack below it shifts and a
  // different card is under a cursor that never moved — measured, and reported
  // as `cardRuns: ["1eb11dce"]` against a `target` that was still on screen.
  // That is the rail reflowing, which is a claim for motion.mjs, not the light
  // failing to survive; asserting it here made this check answer a question it
  // was not asking. What it IS asking is that ONE card is lit, that its words
  // are lit in the prose, and that the lit card is the one the pointer is over
  // — all three read after the rebuild, which is the whole point.
  const under = await page.evaluate(() => {
    const el = document.elementFromPoint(
      ...(() => {
        const c = document.querySelector('.gly-rail-band .gly-card.gly-lit');
        if (!c) return [0, 0];
        const r = c.getBoundingClientRect();
        return [r.x + r.width / 2, r.y + r.height / 2];
      })(),
    );
    const card =
      el && el.closest ? el.closest('.gly-rail-band .gly-card') : null;
    return card ? (card.dataset.run || '').slice(0, 8) : null;
  });
  check(
    'the light survives a server-side mutation — it is a decoration, not a class on the mark',
    survived.prose > 0 &&
      survived.cards === 1 &&
      !!under &&
      survived.cardRuns[0] === under,
    { survived, under, target: target.slice(0, 8) },
  );
  await page.mouse.move(0, 0);
  await page.waitForTimeout(250);

  // AND THE CARD THAT CANNOT POINT. The adrift rule — *a card that cannot point
  // must not be drawn pointing* — is MOOT, and this check is where that is said
  // rather than left as a silence. `connectorGeometryFor` stated it as one of
  // four refusals, in script, after it had been a per-state CSS rule; nothing is
  // drawn now, and a card is adrift exactly when its mark is NOT in the
  // document, so `markRuns` finds nothing to light and the refusal is
  // STRUCTURAL rather than enforced. There is no clause to delete a clause from.
  //
  // WHAT THE OLD CHECK CARRIED BESIDES THAT RULE IS NOT MOOT, which is why this
  // is a rewrite. Hovering a card whose run is not in the prose must light
  // NOTHING — not its own words, which are absent, and not somebody else's.
  //
  // AND IT IS PRODUCED GENUINELY RATHER THAN BY HAND. The old one added
  // `.gly-adrift` to a card whose mark was still in the prose, because the
  // clause under test read the class; with no clause, that would be a synthetic
  // state testing nothing. The real condition is "this card's run is not in the
  // document", and rewriting `data-run` is exactly that condition, deterministic
  // — the websocket race that produces it for real is one frame long and a check
  // that waits for it is a check that flakes.
  //
  // IT ALL HAPPENS INSIDE ONE `evaluate`, and that is the second thing this
  // check had to be taught. `paintRail` rebuilds every card on the pending
  // poll, so a hand-written `data-run` is undone within a second and a half:
  // the first cut set it, called Playwright's `hover`, and Playwright waited
  // for a selector that could only match again once the repaint had put the
  // real run back — so it hovered the restored card and reported the light on.
  // The event is dispatched HERE, through the same delegated listener a pointer
  // reaches, with no await between the write and the read, and the write is
  // asserted to have survived to the moment of the read.
  const adrift = await page.evaluate((sel) => {
    const el = document.querySelector(sel);
    const was = el.dataset.run;
    el.dataset.run = 'no-such-run-in-this-document';
    el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    const out = {
      prose: document.querySelectorAll('.ProseMirror .gly-lit').length,
      stillBogus: el.dataset.run === 'no-such-run-in-this-document',
      // And the MARK is still there to have been lit, which is what makes the
      // refusal the missing card-to-mark link rather than an empty page.
      markStillInProse: !!document.querySelector(
        `.ProseMirror [data-run="${CSS.escape(was)}"]`,
      ),
    };
    el.dataset.run = was;
    el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    out.backAgain = document.querySelectorAll('.ProseMirror .gly-lit').length;
    return out;
  }, cardSel);
  note('a card whose run is not in the document', adrift);
  check(
    'a card whose run is not in the prose lights nothing — not its own words, and not anybody else’s',
    adrift.stillBogus && adrift.markStillInProse && adrift.prose === 0,
    adrift,
  );
  check(
    'and the same card lights again the moment its run is back — the refusal is the missing mark, not the page',
    adrift.backAgain > 0,
    adrift,
  );
  await page.mouse.move(0, 0);
  await page.waitForTimeout(250);

  // NOTHING IS CLIPPED. The band was `overflow: hidden` — a card whose mark was
  // four screens down sat four screens down inside it and was cut off, which is
  // the "i can't scroll to see the response to every?" half of the report. The
  // band is as tall as its cards now, and off the fold a card is no longer
  // clipped to its head line either.
  const clipped = await page.evaluate(() => {
    const band = document.querySelector('.gly-rail-band');
    const b = band.getBoundingClientRect();
    const cards = Array.from(document.querySelectorAll('.gly-rail .gly-card'));
    return {
      bandOverflow: getComputedStyle(band).overflow,
      cards: cards.length,
      cut: cards
        .map((el) => ({
          cls: el.className,
          // Cut off INSIDE its own box — the fold's `max-height: 3.4rem`.
          inner: el.scrollHeight - el.clientHeight,
          // Cut off by the band it lives in.
          past: +(el.getBoundingClientRect().bottom - b.bottom).toFixed(1),
          inBand: !!el.closest('.gly-rail-band'),
        }))
        .filter((c) => c.inner > 1 || (c.inBand && c.past > 1)),
    };
  });
  check(
    'no card in the rail is clipped — not to a head line, and not by the band',
    clipped.cards > 0 &&
      clipped.cut.length === 0 &&
      clipped.bandOverflow === 'visible',
    clipped,
  );

  // THE LAST CARD IS REACHABLE, and that is the report's own sentence. The
  // lowest mark in the document is found from the prose, scrolled to, and its
  // card read back: on screen, whole, and hit-testable at its own centre — the
  // §7a rule, which is that a rect inside the window is not the same claim as a
  // control the reviewer can press.
  const lastRun = await page.evaluate(() => {
    let low = null;
    for (const el of document.querySelectorAll('.ProseMirror [data-run]')) {
      const y = el.getBoundingClientRect().top + window.scrollY;
      if (!low || y > low.y) low = { run: el.dataset.run, y };
    }
    return low;
  });
  check(
    'the fixture has a mark below the fold to scroll to',
    !!lastRun,
    lastRun,
  );
  const last = await page.evaluate((run) => {
    const card = document.querySelector(
      `.gly-rail-band .gly-card[data-run="${CSS.escape(run)}"]`,
    );
    if (!card) return { card: false };
    card.scrollIntoView({ block: 'center' });
    return new Promise((done) =>
      setTimeout(() => {
        const r = card.getBoundingClientRect();
        const at = document.elementFromPoint(
          r.x + r.width / 2,
          r.y + r.height / 2,
        );
        done({
          card: true,
          inside:
            r.top >= 0 && r.bottom <= window.innerHeight + 0.5 && r.height > 20,
          reachable: !!(at && card.contains(at)),
          hitBy: at ? at.className || at.tagName : 'nothing',
          rect: { y: +r.y.toFixed(1), h: +r.height.toFixed(1) },
        });
      }, 600),
    );
  }, lastRun.run);
  check(
    'the last mark’s card is on screen, whole, once you scroll to it',
    last.card && last.inside && last.reachable,
    last,
  );

  // AND A CARD THAT CANNOT POINT IS NOT IN THE MAP AT ALL. The rule this
  // replaces was `the anchorless section holds a card and draws no connector
  // from it` — two claims, and both survive in a stronger form. A line says
  // "this card is about THAT" and a card with nowhere to be has no THAT; the
  // rail's one job is *here is what needs you, beside the text it is about*,
  // and such a card is beside nothing. So it is not carded here, and there is
  // no section for it to be carded in.
  //
  // THE CONVERSATION IS NOT DROPPED, WHICH IS THE HALF THAT COULD GO WRONG. It
  // is live work: the census counts it, and the sheet lists it. That is
  // asserted in §7b″ against the same fixture thread, at the width where the
  // rail exists — so "left the rail" and "arrived in the sheet" are two checks
  // and neither can pass on its own.
  // THE ANCHORLESS THREAD IS MADE HERE, IN THE EDITOR, because that is the only
  // place it can be made any more.
  //
  // It used to be built before the server started: the comment was filed with
  // `galley suggest --comment --on`, and then the `{==…==}` was taken out from
  // under it by rewriting the .md on disk — which is what a reviewer deleting
  // the text a conversation is about does, without a reviewer. The document is
  // OPEN now and the live document owns the file: an outside write is
  // overwritten by the next projection, and the fixture would be racing the
  // server for it. So the deletion is performed the way the product performs
  // it, in the editor. Deleting a TEXT comment's words now retracts the comment
  // (asserted below), so the unplaced card is made from a BLOCK comment whose
  // mark is deleted, which is the one way the product still makes one.
  //
  // ASSERTED, NOT ASSUMED. An editor that could not find the phrase would leave
  // this fixture quietly without an unplaced instruction, and every check below
  // would then be reading zero matches — the pass-forever shape, in the block
  // whose whole subject is a card that cannot point.
  // FILED HERE, NOT IN THE OPENING SEED. The seed's copy of this instruction is
  // GONE by the time this block runs: §9 presses Revise, which SENDS the round —
  // `sendReviewerRound` hands every pending instruction to the agent and clears
  // the list — so the fixture's first six are consumed several hundred lines
  // above. Measured, with the seed's copy relied on: `comments` held only §9's
  // three, none of them on this phrase, and the wait for an unplaced card timed
  // out on a page that was behaving correctly.
  await instruct({
    op: 'comment',
    target: WITHDRAWN,
    text: WITHDRAWN_ASK,
  });
  // AND THE FIGURE'S CONVERSATION, for the same reason and in the same breath:
  // §9's Revise took the seed's copy of that one too, and the check below it
  // feeds ("the map DOES hold a conversation with no run") is what makes the
  // orphan check capable of failing at all. A block thread has no run BY
  // CONSTRUCTION — that is the whole reason `docmodel.Note` exists — so without
  // one in the map, "no orphans" is satisfied by there being nothing to
  // misread.
  {
    const blocks = (await pending()).blocks || [];
    const figure = blocks.find(
      (b) => b && b.kind === 'image' && (b.label || '').includes(FIGURE_LABEL),
    );
    if (!figure)
      throw new Error(
        `fixture: no image block for "${FIGURE_LABEL}" to comment on`,
      );
    await instruct({
      op: 'comment_block',
      target: figure.key,
      text: 'does this picture still match the text?',
    });
  }
  // WAITED FOR ON THE PAGE'S OWN CARDS, not on a field of the app's thread
  // objects: `App.comments` is `/_galley/pending`'s instructions MAPPED, and
  // the quote does not survive under that name (measured: `quote: null` on
  // every entry). The card's head carries the phrase, which is what a reviewer
  // reads and what this block is about to take away.
  await page.waitForFunction(
    (q) =>
      Array.from(
        document.querySelectorAll('.gly-rail .gly-card.gly-thread'),
      ).some((c) => (c.textContent || '').includes(q)),
    WITHDRAWN,
    { timeout: 20000 },
  );
  await page.waitForTimeout(800);

  const withdrawn = await page.evaluate((phrase) => {
    const editor = window.galleyEdit.editor;
    let at = null;
    editor.state.doc.descendants((node, pos) => {
      if (at !== null || !node.isTextblock) return at === null;
      const i = node.textContent.indexOf(phrase);
      if (i !== -1) at = pos + 1 + i;
      return false;
    });
    if (at === null) return false;
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + phrase.length });
    editor.commands.deleteSelection();
    return true;
  }, WITHDRAWN);
  check(
    'the fixture could take the highlight out from under a conversation',
    withdrawn === true,
    { WITHDRAWN, withdrawn },
  );
  // DELETING THE WORDS RETRACTS THE COMMENT, so this deletion makes NO
  // unplaced card. That is the product's rule (lostanchor.go, Court: "if we
  // highlight a sentence and add an instruction and then delete the sentence,
  // we should delete the instruction as well"), and rounds-ux asserts it on the
  // CI side. This block used to wait here for the withdrawn comment to arrive
  // in the unplaced section, which it never does: the wait timed out and every
  // section after §10 went unrun. The retraction is asserted instead, and the
  // unplaced card is made the one way the product still makes one, below.
  //
  // READ OFF THE APP'S LIST AND THE RAIL, NOT EVERY CARD ON THE PAGE. A closed
  // sheet keeps the cards it last painted until it is opened again (sheet.ts
  // `openSheet` repaints), so a page-wide card search finds the retracted
  // comment's stale sheet card and waits forever on a page that is right.
  await page.waitForFunction(
    (q) =>
      !(window.galleyEdit.app.comments || []).some((c) =>
        (c.entries || []).some((e) => e.text === q),
      ) &&
      !Array.from(document.querySelectorAll('.gly-rail .gly-thread')).some(
        (c) => (c.textContent || '').includes(q),
      ),
    WITHDRAWN_ASK,
    { timeout: 15000 },
  );
  check(
    'deleting the highlighted words retracts the comment — it is not left as an unplaced card',
    (await page.locator('.gly-rail-unplaced .gly-thread').count()) === 0,
  );

  // THE UNPLACED INSTRUCTION IS A BLOCK COMMENT WHOSE MARK WAS DELETED. A
  // block comment's place is its `{>>@comment cb-…<<}` note, and a block
  // comment whose note leaves the document STAYS, unplaced (lostanchor.go's
  // first guard: only a text comment is retracted). The reviewer selects the
  // amber box and deletes it; the transaction below is that deletion.
  {
    const blocks = (await pending()).blocks || [];
    const eighth = blocks.find(
      (b) => b && (b.label || '').includes('eighth paragraph'),
    );
    if (!eighth)
      throw new Error(
        'fixture: no block for the eighth paragraph to comment on',
      );
    await instruct({
      op: 'comment_block',
      target: eighth.key,
      text: 'is this paragraph still needed?',
    });
  }
  const unplacedKey = (
    ((await pending()).instructions || []).find(
      (i) => i && i.text === 'is this paragraph still needed?',
    ) || {}
  ).key;
  if (!unplacedKey)
    throw new Error('fixture: the block comment is not in /_galley/pending');
  await page.waitForFunction(
    (id) => {
      let found = false;
      window.galleyEdit.editor.state.doc.descendants((n) => {
        if (n.type.name === 'note' && n.attrs.id === id) found = true;
        return !found;
      });
      return found;
    },
    unplacedKey,
    { timeout: 15000 },
  );
  const markGone = await page.evaluate((id) => {
    const editor = window.galleyEdit.editor;
    let at = null;
    editor.state.doc.descendants((n, pos) => {
      if (at === null && n.type.name === 'note' && n.attrs.id === id)
        at = { from: pos, to: pos + n.nodeSize };
      return at === null;
    });
    if (at === null) return false;
    editor.view.dispatch(editor.state.tr.delete(at.from, at.to));
    return true;
  }, unplacedKey);
  check(
    'the fixture could delete a block comment’s mark from under it',
    markGone === true,
    { unplacedKey, markGone },
  );
  // ATTACHED, NOT VISIBLE. This block runs at the narrow viewport, where the
  // rail is `display: none` and the sheet is the surface — so playwright's
  // default `state: 'visible'` waits for a card that is correctly never painted
  // and times out after thirty seconds on a fixture that worked.
  await page.waitForSelector('.gly-rail-unplaced .gly-thread', {
    state: 'attached',
    timeout: 15000,
  });
  await page.waitForTimeout(1200);

  const railed = await page.evaluate(() => ({
    sections: document.querySelectorAll(
      '.gly-rail-anchorless, .gly-rail-settled, .gly-rail-changed',
    ).length,
    // A CARD WITH NO RUN IS NOT AN ANCHORLESS CARD, and this check said it was.
    // `threadCard` writes `dataset.run = thread.run || ''`, and a BLOCK or
    // FIGURE thread has no run by construction — that is the whole reason
    // `docmodel.Note` exists — so `!c.dataset.run` named the exact population
    // `threadPlacement` was written to stop being confused with the adrift one.
    // It is the discriminator CLAUDE.md records as wrong, corrected in §7b″ and
    // left standing here: fix-one-site-miss-the-other, inside a check. It was
    // green only because no thread in this fixture was on a block; there is one
    // now (the figure's), and against the old form it reports `orphans: 1` — a
    // conversation sitting correctly beside its picture, named a defect.
    //
    // The discriminator is `threadLabel`'s own sentence, which is the one place
    // that tells the three shapes apart and the only one a reviewer ever reads.
    // READ STRUCTURALLY, NOT OFF THE COPY. This matched the card's own
    // explanatory sentence — `/not tied to a mark/` — which is the reviewer's
    // reading of the state and therefore the thing most likely to be reworded;
    // a check pinned to it goes red on a copy edit and, worse, goes GREEN and
    // stops finding anything the day the sentence is dropped. `.gly-adrift` is
    // the class `threadCard` puts on a thread that cannot point, written in one
    // place from `threadLabel`'s own verdict, and it is what the dashed border
    // is drawn from — so this asks the same question of the artifact instead of
    // of the prose.
    orphans: document.querySelectorAll(
      '.gly-rail-band .gly-card.gly-thread.gly-adrift',
    ).length,
    unplaced: document.querySelectorAll(
      '.gly-rail-unplaced .gly-card.gly-thread',
    ).length,
    // And the fixture really does put a runless card in the map, so "no
    // orphans" cannot be satisfied by there being nothing to misread.
    runless: Array.from(
      document.querySelectorAll('.gly-rail-band .gly-card.gly-thread'),
    ).filter((c) => !c.dataset.run).length,
  }));
  check(
    'no unplaced instruction is in the positioned map',
    railed.sections === 0 && railed.orphans === 0 && railed.unplaced === 1,
    railed,
  );
  check(
    'and the map DOES hold a conversation with no run — a block thread, which is not adrift',
    railed.runless > 0,
    railed,
  );

  // AND THE RAIL SHOWS THE ACTUAL INSTRUCTION. The count used to point at a
  // removed conversation sheet and left the work unreachable from the view the
  // reviewer had deliberately opened. One unplaced heading and one complete
  // card keep the count, the words and the delete action together.
  const unplaced = await page.evaluate(() => ({
    said: (
      document.querySelector('.gly-rail-unplaced-head')?.textContent || ''
    ).trim(),
    cards: document.querySelectorAll('.gly-rail-unplaced .gly-thread').length,
    deletes: document.querySelectorAll('.gly-rail-unplaced .gly-thread-delete')
      .length,
  }));
  check(
    'and the rail labels exactly one unplaced instruction',
    unplaced.said === unplacedSaid(1),
    { unplaced, want: unplacedSaid(1) },
  );
  check(
    'and the unplaced instruction remains a complete, deletable card',
    unplaced.cards === 1 && unplaced.deletes === 1,
    unplaced,
  );

  await page.evaluate(() => window.scrollTo(0, 0));
  await page.setViewportSize(WIDE);
  await page.waitForTimeout(600);
}

// --- §10a · the rail begins BELOW the bar, and nothing lands under it --------
//
// THE BAND'S TOP IS THE RAIL'S TOP, AND THE RAIL IS `position: absolute` — so
// whatever the anchor pass does not explicitly place lands wherever `top: 0`
// puts it. Absolute takes the rail OUT OF FLOW, so its containing block is the
// initial one and `top: 0` is document y 0: exactly where the opaque sticky bar
// sits at `scrollY 0`, with the rail at `z-index: 5` under it. The regroup
// reasoned that a sticky bar is in flow and therefore already reserves that
// space — true of a flow sibling, false of this element, and §10 above could
// not see the difference because every card it reads is placed by hand.
//
// TWO STATES REACH IT, AND BOTH ARE STATES THE PRODUCT REACHES ON ITS OWN.
//
//   A. THE SETTLED DOCUMENT — the state Court reaches the moment he presses
//      `✓ all`, and his own report restated: *"settled still overrides the rest
//      of the threads. changed then overrides that."* With nothing pending and
//      no open thread, `paintRailCards` appends the settled notice and returns
//      before a single card is built, so `this.cards` is empty, the band is
//      written to height 0, and all three sections stack from the rail's top.
//   B. EVERY CARD ADRIFT — a suggestion the server reports and the document
//      does not show yet, which is what EVERY agent suggestion arriving ahead
//      of the websocket is for the frame before it lands. An adrift card has no
//      anchor, so the anchored loop neither positions it nor counts it into the
//      band's height: `top: auto` is its static position, the band stays 0 tall,
//      and the sections are drawn straight over the cards.
//
// BOTH ARE INSTALLED BY HAND, for §8a's stated reason. Neither is reachable
// without destroying this fixture — A is a swept review, which would leave §8
// below with no verbs to read, and B is a race with the websocket measured in
// frames. The state is written onto the REAL app and painted by the REAL
// `paintRail`, so everything read back is the product's own geometry; the
// fixture is restored by a real `refreshPending` afterwards.
//
// State A keeps the RESOLVED thread — `censusCounts.threads` counts open
// threads only, so a document whose every conversation is settled still reads
// `threads === 0`, which is what puts the settled region and the notice on
// screen together. That is the shape of Court's complaint and not a contrivance
// of it.
console.log('\n--- §10a · the rail begins below the bar ---');
{
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(200);

  // THE POLL IS SILENCED WHILE THE STATE IS HELD, AND THE SERVER'S OWN ANSWER
  // IS KEPT IN HAND. `App.tick` runs every 1500ms and a `/_galley/rev` move
  // calls `refreshPending`, which replaces both lists from the server and
  // rebuilds every card — so an injected state survives a poll interval at
  // most. Shadowing the method on the INSTANCE holds it for as long as this
  // block needs; `delete` at the foot puts the prototype's own back, and the
  // two states are built from the lists captured HERE rather than from a
  // re-fetch between them, so neither half of the block depends on a round
  // trip landing inside a `waitForTimeout`.
  await page.evaluate(() => {
    const app = window.galleyEdit.app;
    app.__keep = { suggestions: app.suggestions, comments: app.comments };
    app.refreshPending = () => Promise.resolve();
  });

  /** Every visible box in the rail, against the bar that can cover it.
   *
   *  TWO CLAIMS, AND §7a's RULE IS THAT THEY ARE TWO. `clearsBar` is a rect
   *  claim read at `scrollY 0`, where the sticky bar is at the top of the page
   *  and the rail's own top is beside it: a box whose document top is above the
   *  bar's foot is behind the bar at every scroll position there is, which is
   *  the defect. `reachable` is a hit test, and it is taken with the page
   *  SCROLLED so the box's head sits just below the bar — the §10 standard, "on
   *  screen once you scroll to it", because a card at the end of a 2000px band
   *  is legitimately below the fold and `elementFromPoint` answers `null` for
   *  every point outside the window.
   *
   *  The point read is the box's own TOP edge rather than its centre: a bar
   *  covering the head of a section and leaving its foot is still a head the
   *  reviewer cannot read or press. */
  //
  //  NOTHING HERE HOLDS AN ELEMENT ACROSS AN AWAIT, and that is not caution —
  //  it is a measured hazard. `clearNewLater` fires 8s after the last arrival,
  //  clears the ring on what is new and calls `paintRail`, which destroys and
  //  rebuilds every card. The first version of the hit-test loop captured its
  //  card references once: the rebuild landed between two iterations, every
  //  later rect came back `0` (a detached element measures at the origin) and
  //  every point landed on the bar — a failure that looks exactly like the
  //  defect under test and is not it. A card is re-resolved BY INDEX after
  //  every scroll, which survives the rebuild because the rebuild is from the
  //  same list in the same order.
  const RAIL_BOXES = `
    const bar = () => document.querySelector('.gly-bar').getBoundingClientRect();
    const at = (spec) => (spec.card === undefined
      ? document.querySelector(spec.sel)
      : document.querySelectorAll('.gly-rail-band .gly-card')[spec.card]);
    const specs = [
      { label: 'rail', sel: '.gly-rail' },
      { label: 'notice', sel: '.gly-rail-notice' },
      ...[...document.querySelectorAll('.gly-rail-band .gly-card')]
        .map((el, i) => ({ label: 'band card ' + i, card: i })),
    ].filter((spec) => {
      const el = at(spec);
      return el && el.getBoundingClientRect().height >= 1;
    });
  `;
  const railBoxes = () =>
    page.evaluate(`(() => {
      ${RAIL_BOXES}
      const b = bar();
      const band = document.querySelector('.gly-rail-band').getBoundingClientRect();
      return {
        bar: { bottom: +b.bottom.toFixed(1) },
        band: { top: +band.top.toFixed(1), height: +band.height.toFixed(1) },
        rows: specs.map((spec) => {
          const r = at(spec).getBoundingClientRect();
          return {
            label: spec.label,
            top: +r.top.toFixed(1),
            bottom: +r.bottom.toFixed(1),
            clearsBar: r.top >= b.bottom - 0.5,
          };
        }),
      };
    })()`);
  /** The hit test, one box at a time, with the page put where that box can be
   *  seen. `scrollIntoView` is not used: `block: 'start'` puts the box's head
   *  under the sticky bar by construction and `block: 'center'` cannot place a
   *  box taller than the window at all. Scrolling to "head 20px below the bar"
   *  is the position a reviewer reaches by scrolling to it, and it is the same
   *  for every box whatever its height. */
  const railReach = () =>
    page.evaluate(`(async () => {
      ${RAIL_BOXES}
      const out = [];
      for (const spec of specs) {
        // TWICE, AND THE SECOND ONE IS NOT BELT AND BRACES. The rail is
        // absolutely positioned, so the page's own scrollable area only takes
        // account of it once it has been laid out — a first scrollTo past what
        // the page currently admits CLAMPS, and the room appears on the reflow
        // that scroll caused. Measured: a box at document y 2376 asked for
        // 2304 and got 1079. Asking again from the settled layout reaches it.
        for (let tries = 0; tries < 3; tries += 1) {
          const want = at(spec).getBoundingClientRect().top + window.scrollY - bar().height - 20;
          if (Math.abs(window.scrollY - Math.max(0, want)) < 1) break;
          window.scrollTo(0, Math.max(0, want));
          await new Promise((done) => requestAnimationFrame(() => setTimeout(done, 30)));
        }
        const el = at(spec);
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + 4);
        out.push({
          label: spec.label,
          at: +r.top.toFixed(1),
          scrollY: Math.round(window.scrollY),
          reachable: !!(hit && (hit === el || el.contains(hit))),
          hitBy: hit ? hit.className || hit.tagName : 'nothing',
        });
      }
      window.scrollTo(0, 0);
      await new Promise((done) => requestAnimationFrame(() => setTimeout(done, 30)));
      return out;
    })()`);

  // A · THE SETTLED DOCUMENT.
  await page.evaluate(() => {
    const app = window.galleyEdit.app;
    app.suggestions = [];
    app.comments = app.__keep.comments.filter((c) => c.resolved);
    app.paintRail();
  });
  await page.waitForTimeout(400);
  const settledDoc = await railBoxes();
  const settledReach = await railReach();
  note('a settled document, every visible box in the rail', settledDoc);
  // READ AS AN EMPTY MAP, NOT AS A SENTENCE. This required exactly one
  // `.gly-rail-notice .gly-settled` — the "nothing pending — the document is
  // settled" card — and `paintRailCards` writes no such card any more: the only
  // thing it puts in the notice on an empty rail is the ARRIVALS-HELD line, and
  // this state has nothing held. The claim the geometry below rests on is that
  // the map is EMPTY, which is what the next three checks need to be measuring
  // a settled rail rather than a busy one, so that is what is asserted — and it
  // is asserted over the whole rail, not the band alone, or a card that had
  // slipped into the notice would satisfy it.
  // SCOPED TO THE THREAD CARDS. `.gly-rail .gly-card` catches the
  // whole-document composer's own card, which is PINNED first in the rail and
  // is there on a settled document exactly as it is on a busy one — it is the
  // box a new instruction is typed into, not a piece of work. What has to be
  // empty for the geometry below to be measuring a settled rail is the MAP.
  check(
    'the settled document really is settled — the band holds nothing, and no thread does either',
    (await page.locator('.gly-rail .gly-card.gly-thread').count()) === 0 &&
      settledDoc.band.height < 1,
    { band: settledDoc.band },
  );
  // THE FLOOR NAMES THE SURFACE, because "every visible box" is met vacuously
  // by the rail alone — and the defect was that the sections BELOW the empty
  // band were the boxes under the bar. Court's own words were "settled still
  // overrides the rest of the threads. changed then overrides that", and both
  // of those sections are deleted; what is left after the map is the NOTICE,
  // which on a settled document is the one box in the rail carrying anything
  // at all, and it is the box that would land under the bar for the identical
  // reason. So the name changed and the claim did not: a section whose top is
  // above the bar's foot is behind the bar at every scroll position.
  const named = (rows) => rows.some((r) => r.label === 'notice');
  check(
    'every section of a settled rail begins below the bar — the notice among them',
    named(settledDoc.rows) && settledDoc.rows.every((r) => r.clearsBar),
    {
      missing: !named(settledDoc.rows),
      under: settledDoc.rows.filter((r) => !r.clearsBar),
    },
  );
  check(
    'and every one of them can be read where it is',
    named(settledReach) && settledReach.every((r) => r.reachable),
    {
      missing: !named(settledReach),
      unreachable: settledReach.filter((r) => !r.reachable),
    },
  );

  // B · EVERY CARD ADRIFT. The run is the pairing between a card and its mark,
  // so a run this document does not carry is exactly what the server reporting
  // ahead of the websocket looks like from the browser's side. Both lists are
  // mangled: a thread whose run does not resolve is a band card too.
  //
  // AND THE BLOCK KEY WITH IT, because a run is not the only way a card is
  // placed. `threadPlacement` measures a block thread through the INDEX
  // `/_galley/pending` reports for its `anchorKey`, so the figure's
  // conversation survived a run-only mangle and sat correctly beside its
  // picture in a state that is supposed to have no anchored card in it — which
  // the check below caught the moment the fixture gained one. A key this
  // refresh does not know is threadPlacement's own third anchorless case and
  // the same race in the other coordinate, so mangling both is one fiction, not
  // two. That thread then leaves the band entirely (the rail holds live work
  // beside the text it is about, and it is now beside nothing) and is announced
  // in the notice, which is the product's own behaviour and not a special case
  // for this state.
  await page.evaluate(() => {
    const app = window.galleyEdit.app;
    const adrift = (row) => {
      let out = row;
      if (out.run) out = { ...out, run: `adrift-${out.run}` };
      if (out.anchorKey) out = { ...out, anchorKey: `adrift-${out.anchorKey}` };
      return out;
    };
    app.suggestions = app.__keep.suggestions.map(adrift);
    app.comments = app.__keep.comments.map(adrift);
    app.paintRail();
  });
  await page.waitForTimeout(400);
  const allAdrift = await railBoxes();
  const adriftReach = await railReach();
  const adriftCards = await page.evaluate(() => {
    const band = document.querySelector('.gly-rail-band');
    const cards = [...band.querySelectorAll('.gly-card')];
    const b = band.getBoundingClientRect();
    return {
      cards: cards.length,
      notAdrift: cards.filter((el) => !el.classList.contains('gly-adrift'))
        .length,
      unplaced: cards.filter((el) => !el.style.top).length,
      // Held by the band it lives in — which is the height claim, read as the
      // geometry it is for rather than as a number.
      past: cards
        .map((el) => +(el.getBoundingClientRect().bottom - b.bottom).toFixed(1))
        .filter((d) => d > 1),
      overlap: cards
        .map((el) => el.getBoundingClientRect())
        .sort((p, q) => p.top - q.top)
        .flatMap((r, i, all) =>
          i === 0 ? [] : [+(all[i - 1].bottom - r.top).toFixed(1)],
        )
        .filter((d) => d > 1),
    };
  });
  note('every card adrift, the band that has to hold them', {
    ...adriftCards,
    band: allAdrift.band,
  });
  check(
    'every card in the band really is adrift — a state with an anchored card in it is not this state',
    adriftCards.cards > 1 && adriftCards.notAdrift === 0,
    adriftCards,
  );
  check(
    'an adrift card is given a position and counted into the band',
    adriftCards.unplaced === 0 &&
      adriftCards.past.length === 0 &&
      adriftCards.overlap.length === 0 &&
      allAdrift.band.height > 1,
    { ...adriftCards, band: allAdrift.band },
  );
  check(
    'every box in an all-adrift rail begins below the bar',
    allAdrift.rows.length >= 3 && allAdrift.rows.every((r) => r.clearsBar),
    allAdrift.rows.filter((r) => !r.clearsBar),
  );
  check(
    'and every one of them can be read once you scroll to it',
    adriftReach.length >= 3 && adriftReach.every((r) => r.reachable),
    adriftReach.filter((r) => !r.reachable),
  );

  // AND THE OFFSET IS MEASURED, NOT A CONSTANT. `3.4rem` is 54.4px and a folded
  // bar is 91.4 — the number that made the LAST version of this rule wrong, from
  // the opposite side. 1200 folds this fixture's bar (measured: flat at 1248)
  // and still shows the rail, which is hidden below 992 altogether.
  await page.setViewportSize({ width: 1200, height: 1100 });
  await page.waitForTimeout(500);
  const folded = await page.evaluate(() => {
    const bar = document.querySelector('.gly-bar').getBoundingClientRect();
    const rail = document.querySelector('.gly-rail').getBoundingClientRect();
    const px = (name) =>
      parseFloat(
        getComputedStyle(document.documentElement).getPropertyValue(name),
      ) || 0;
    return {
      barH: +bar.height.toFixed(1),
      railTop: +rail.top.toFixed(1),
      folded: bar.height > 60,
      published: getComputedStyle(document.documentElement)
        .getPropertyValue('--gly-bar-h')
        .trim(),
      // The reserved sub-bar row plus the page's top padding — the token the
      // draft rail and History's rail BOTH hang off now, so a mode switch moves
      // neither. It is read here rather than written as a number for the same
      // reason the bar's height is measured: a literal 64 in this check would
      // go green the day the row changed and the two rails stopped agreeing.
      railTopVar: px('--gly-sub-h') + px('--gly-page-pad'),
    };
  });
  check(
    'the bar really is folded at 1200 — a flat bar cannot tell a constant from a measurement',
    folded.folded,
    folded,
  );
  // THE MEASUREMENT IS STILL THE CLAIM, AND THE RESERVED ROW IS A SECOND TERM
  // RATHER THAN A REPLACEMENT FOR IT. `3.4rem` is 54.4px and a folded bar is
  // 91.4 — the number that made the LAST version of this rule wrong. The rail
  // now begins that measured height PLUS the row both rails reserve, and the
  // arithmetic is written out so a change to either term fails here.
  check(
    'and the rail begins at the FOLDED bar’s own height, not at 3.4rem',
    folded.railTopVar > 0 &&
      Math.abs(folded.railTop - (folded.barH + folded.railTopVar)) <= 1,
    folded,
  );

  await page.setViewportSize(WIDE);
  await page.evaluate(() => {
    const app = window.galleyEdit.app;
    delete app.refreshPending;
    delete app.__keep;
    return app.refreshPending();
  });
  await page.waitForTimeout(700);
}

// --- §8a · the seal's sweep may not touch a LIVE page's flags ----------------
//
// BEFORE §8, and that ordering is the whole check: it asserts what a page that
// has NOT been sealed looks like, and §8 below ends the review for good.
//
// `applySealedVerbs` walks SEALED_VERBS and writes `disabled`. It used to write
// it unconditionally — `el.disabled = !!this.sealed` — so on an ordinary live
// review it wrote `false` over every flag another owner had set. It runs at the
// FOOT of `paintRail`, so it was the last thing every pending refresh did.
//
// THE FLAG IS INSTALLED BY HAND HERE, AND THAT IS A CHANGE FORCED BY THE RAIL.
// The original version of this block read a REAL flag: a short viewport put
// several marks off screen, `paintFold` → `dimCard(card, true)` disabled the
// accept, reject, resolve and delete of every card past the fold, and this read
// them back after a full `refreshPending`. The rail scrolls with the document
// now, there is no fold and no `dimCard`, and that flag cannot be produced at
// all — which is the deletion working, not a hole.
//
// The rule is not the instance, and the surviving owners are real: `paintCensus`
// keeps `✓ all` disabled on a document the sweep would leave untouched, and
// every box that posts disables itself for the flight. NEITHER IS REACHABLE
// HERE WITHOUT DESTROYING THE FIXTURE — the census one needs a review with
// nothing pending and nothing answered, i.e. a swept review, which would leave
// §8 below with no verbs to read and its coverage assertion red; the in-flight
// one is a window of one POST. So the flag is written onto a real, live control
// and a real `refreshPending` is run over it. What is asserted is exactly what
// the invariant says: a pending refresh does not hand back a flag it did not
// set. Shown red by restoring `el.disabled = !!this.sealed` in
// `applySealedVerbs` — the sweep then writes `false` over the line below and
// every one of these controls comes back live.
console.log('\n--- §8a · a live page owns its own disabled flags ---');
{
  const sealed = await page.evaluate(() => {
    const el = document.querySelector('#gly-seal');
    return !!el && getComputedStyle(el).display !== 'none';
  });
  check(
    'the review under this check is LIVE — §8 has not run yet',
    sealed === false,
    { sealed },
  );

  // THE ELEMENTS READ ARE THE ONES WITH NO PAINTER, and that is what makes the
  // read discriminating rather than tautological. `.gly-census-accept` cannot
  // carry this claim — `refreshPending` runs `paintCensus`, its rightful owner,
  // which re-derives the flag from the counts on every refresh, so a `false`
  // there says nothing about who wrote it (§8 below records the same fact from
  // the other end). `.gly-overall-input` and `.gly-composer-text` have no
  // painter at all: the whole-document form is built once and only its entries
  // are rebuilt, and the composer's box is placed by GESTURES — the reason
  // `.gly-comment-button` is in SEAL_ONLY_VERBS at all.
  //
  // THE SECOND SELECTOR WAS `.gly-census-overall`, which is not built any
  // more: the whole-document handle was a bar control opening a floating
  // panel, and the composer is the rail's first card now. It is replaced by
  // another entry from the SAME list rather than dropped, because the claim
  // needs two and needs both of them painter-less. The input's own in-flight
  // `disabled` — set for the flight of its POST — is a REAL live-page flag of
  // exactly this shape, and re-enabling a box mid-POST is the harm; writing it
  // by hand is standing in for the timing, not for the state.
  const marked = await page.evaluate(() => {
    const sels = ['.gly-overall-input', '.gly-composer-text'];
    const out = [];
    for (const sel of sels) {
      const el = document.querySelector(sel);
      if (!el) {
        out.push({ sel, found: false });
        continue;
      }
      el.disabled = true;
      out.push({ sel, found: true, before: !!el.disabled });
    }
    return out;
  });
  check(
    'every control this reads exists on the fixture — a check with nothing to read is not a check',
    marked.length === 2 && marked.every((m) => m.found && m.before),
    marked,
  );

  // THE EVENT IS A PENDING REFRESH, not a repaint: `applySealedVerbs` is the
  // tail of `paintRail`, and `refreshPending` is what every poll, every
  // mutation and every verdict runs.
  await page.evaluate(() => window.galleyEdit.app.refreshPending());
  await page.waitForTimeout(800);

  const after = await page.evaluate(() => {
    const sels = ['.gly-overall-input', '.gly-composer-text'];
    return sels.map((sel) => {
      const el = document.querySelector(sel);
      return { sel, present: !!el, disabled: !!(el && el.disabled) };
    });
  });
  check(
    'a live pending refresh does not hand back a flag it did not set',
    after.length === 2 && after.every((a) => a.present && a.disabled),
    after,
  );

  // PUT THEM BACK, or §8 reads two controls this block killed rather than two
  // the seal did — and the reopen half of §8 would then pass over them for the
  // wrong reason. `releaseSealOnlyVerbs` is the seal's own edge and not
  // available here, so they are restored the same way they were written.
  //
  // FROM THE APP'S OWN LIST, not from a pair of selectors written down here.
  // `.gly-census-overall` was one of the two and it is not built any more, so
  // the restore threw on `null.disabled` — a gate dying on a control that left
  // the product. `SEAL_ONLY_VERBS` is the constant this file already imports
  // for exactly this reason, and reading it here means the restore covers
  // whatever the seal's own edge covers, forever.
  const sealOnly = SEAL_ONLY_VERBS.split(',')
    .map((v) => v.trim())
    .filter(Boolean);
  await page.evaluate((sels) => {
    for (const sel of sels) {
      for (const el of document.querySelectorAll(sel)) el.disabled = false;
    }
  }, sealOnly);
  const restored = await page.evaluate(
    (sels) =>
      sels.every((sel) => {
        const found = Array.from(document.querySelectorAll(sel));
        return found.length > 0 && found.every((el) => el.disabled === false);
      }),
    sealOnly,
  );
  check(
    'and every seal-only verb is live again before §8 runs',
    restored,
    sealOnly,
  );
}

// --- §8b · the three comment boxes are one design --------------------------
//
// THE WHOLE-DOCUMENT BOX, THE COMPOSER AND THE EDIT BOX take the same words, so
// they are one box: one type, five rows to start, and one cap at half the
// window, past which each scrolls rather than growing over everything. Read
// off the real page, each the frame after it opens, because three rules that
// agree in the stylesheet can still disagree on screen (the edit box is built
// off the page and fitted later; the other two are fitted as they open).
console.log('\n--- §8b · the three comment boxes are one design ---');
{
  await page.setViewportSize({ width: WIDE.width, height: WIDE.height });
  await page.waitForTimeout(400);
  const box = (sel) =>
    page.evaluate((s) => {
      const el = document.querySelector(s);
      if (!el) return null;
      const cs = getComputedStyle(el);
      return {
        h: +el.getBoundingClientRect().height.toFixed(1),
        font: cs.fontSize,
        family: cs.fontFamily,
        cap: cs.maxHeight,
      };
    }, sel);
  const boxes = {};
  await page.locator('.gly-bar .gly-capture-open').click();
  await page.waitForTimeout(150);
  boxes.document = await box('.gly-overall-input');
  await page.keyboard.press('Escape');
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    let at = null;
    editor.state.doc.descendants((n, pos) => {
      if (
        at === null &&
        n.isTextblock &&
        n.type.name === 'paragraph' &&
        n.textContent.length > 12
      )
        at = pos + 1;
      return at === null;
    });
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + 8 });
  });
  await page
    .locator('.gly-comment-button')
    .waitFor({ state: 'visible', timeout: 5000 });
  await page.click('.gly-comment-button');
  await page.waitForTimeout(150);
  boxes.selection = await box('.gly-composer-text');
  await page.keyboard.press('Escape');
  await page.click('.gly-rail-band .gly-thread .gly-thread-edit');
  await page.waitForTimeout(150);
  boxes.edit = await box('.gly-rail-band .gly-thread .gly-thread-edit-text');
  await page.click('.gly-rail-band .gly-thread .gly-thread-edit-cancel');
  await page.waitForTimeout(150);
  const all = Object.values(boxes);
  check(
    'the three comment boxes are equal — one height when opened, one type, one cap at half the window',
    all.length === 3 &&
      all.every((b) => b !== null) &&
      all.every(
        (b) =>
          Math.abs(b.h - all[0].h) <= 1 &&
          b.font === all[0].font &&
          b.family === all[0].family &&
          b.cap === `${WIDE.height / 2}px`,
      ),
    boxes,
  );
}

// --- §8c · the composer, grown to its cap, stays in the window ------------
//
// THE COMPOSER IS PLACED ONCE, WHEN IT OPENS, and then grows with what is
// typed, up to half the window. Placed for the height it opened at, a box that
// later grows by a third of the window can run off the bottom of it. Measured
// at a short window, with the passage in the middle (placed below) and near
// the foot (flipped above), each grown to its cap with sixty lines.
console.log(
  '\n--- §8c · the composer, grown to its cap, stays in the window ---',
);
{
  const SHORT = { width: 1280, height: 600 };
  await page.setViewportSize(SHORT);
  await page.waitForTimeout(400);
  const sixty = Array.from({ length: 60 }, (_, i) => `line ${i + 1}`).join(
    '\n',
  );
  // The passage is the first eight characters of an unmarked paragraph, set at
  // `atY` in the window: the nth such paragraph, or (nth null) the first one
  // far enough down the page to be scrolled there.
  const grown = async (atY, nth) => {
    const at = await page.evaluate(
      ([y, n]) => {
        const editor = window.galleyEdit.editor;
        const seen = [];
        editor.state.doc.descendants((node, pos) => {
          const first = node.firstChild;
          if (
            node.type.name === 'paragraph' &&
            first &&
            first.isText &&
            first.marks.length === 0 &&
            first.text.length > 12
          )
            seen.push(pos + 1);
          return true;
        });
        // A caret, so the selection below is always a fresh one.
        editor.commands.setTextSelection(seen[0]);
        const pageTop = (p) => editor.view.coordsAtPos(p).top + window.scrollY;
        const from =
          n !== null
            ? seen[Math.min(n, seen.length - 1)]
            : seen.find((p) => pageTop(p) >= y) || seen[seen.length - 1];
        window.scrollTo(0, pageTop(from) - y);
        return from;
      },
      [atY, nth],
    );
    await page.waitForTimeout(150);
    await page.evaluate((from) => {
      const editor = window.galleyEdit.editor;
      editor.commands.focus(undefined, { scrollIntoView: false });
      editor.commands.setTextSelection({ from, to: from + 8 });
    }, at);
    await page.waitForTimeout(150);
    await page
      .locator('.gly-comment-button')
      .waitFor({ state: 'visible', timeout: 5000 });
    // The composer's box and the selected words' box, read together.
    const boxes = () =>
      page.evaluate(() => {
        const r = document
          .querySelector('.gly-composer')
          .getBoundingClientRect();
        const { view, state } = window.galleyEdit.editor;
        const a = view.coordsAtPos(state.selection.from);
        const b = view.coordsAtPos(state.selection.to);
        return {
          top: +r.top.toFixed(1),
          bottom: +r.bottom.toFixed(1),
          selTop: +Math.min(a.top, b.top).toFixed(1),
          selBottom: +Math.max(a.bottom, b.bottom).toFixed(1),
          window: window.innerHeight,
        };
      });
    const bar = await boxes();
    await page.click('.gly-comment-button');
    await page.waitForTimeout(150);
    const opened = await boxes();
    await page.fill('.gly-composer-text', sixty);
    await page.waitForTimeout(150);
    const out = await page.evaluate(() => {
      const r = document.querySelector('.gly-composer').getBoundingClientRect();
      const sel = window.galleyEdit.editor.view.coordsAtPos(
        window.galleyEdit.editor.state.selection.from,
      );
      return {
        top: +r.top.toFixed(1),
        bottom: +r.bottom.toFixed(1),
        window: window.innerHeight,
        passage: +sel.top.toFixed(1),
      };
    });
    await page.keyboard.press('Escape');
    await page.waitForTimeout(150);
    return { ...out, bar, opened };
  };
  const middle = await grown(300, 3);
  const foot = await grown(540, null);
  // THE BUTTON NEVER GROWS, so it is placed for its own height: beside the
  // words, below them where it fits and above them where it does not. Placed
  // for the grown form's height instead, a selection with less room below
  // than a full form put the one-button bar at the window's foot, far from
  // the words it was offered for.
  const beside = (g, side) =>
    side === 'below'
      ? g.bar.top - g.bar.selBottom >= 0 && g.bar.top - g.bar.selBottom <= 16
      : g.bar.selTop - g.bar.bottom >= 0 && g.bar.selTop - g.bar.bottom <= 16;
  check(
    'the Add instruction button sits just below words selected mid-window, not at the window foot',
    beside(middle, 'below') && middle.bar.bottom < middle.bar.window - 40,
    middle.bar,
  );
  check(
    'and just above words selected near the foot, where there is no room below',
    beside(foot, 'above'),
    foot.bar,
  );
  // THE FORM IS WHAT GROWS, so opening it re-places the box for the height it
  // can grow to — inside the window, and off the words it is about.
  const clear = (o) =>
    o.top >= 0 &&
    o.bottom <= o.window + 0.5 &&
    (o.bottom <= o.selTop + 0.5 || o.top >= o.selBottom - 0.5);
  check(
    'the opened form sits inside the window and does not cover the selected words',
    clear(middle.opened) && clear(foot.opened),
    { middle: middle.opened, foot: foot.opened },
  );
  check(
    'the composer grown to its cap stays inside a short window — passage in the middle, and at the foot',
    [middle, foot].every((g) => g.top >= 0 && g.bottom <= g.window + 0.5),
    { middle, foot },
  );
  check(
    'and at the foot it flips above the passage rather than over it',
    foot.passage > SHORT.height / 2 && foot.bottom <= foot.passage,
    foot,
  );
  await page.setViewportSize(WIDE);
  await page.waitForTimeout(400);
}

// --- §12 · the rounds -------------------------------------------------------
//
// THE SURFACE THE RECORD LIVES ON, read at the two states that can be wrong.
//
// A HIDDEN FULL-SCREEN SURFACE THAT IS NOT ACTUALLY HIDDEN COVERS THE DOCUMENT,
// and `[hidden] { display: none }` is a USER-AGENT rule that ANY author
// `display` beats — this panel needs `display: flex` for its own layout, so the
// first cut of it painted over the whole page from load with `hidden` set.
// Nine checks in `just motion` passed over that: a rect is a rect under an
// overlay, and every bar control still answered `elementFromPoint` because the
// bar sits above the panel. The first thing to notice was a drag across the
// prose that selected nothing. So the check is not "the attribute is set" —
// that is the proxy this file records six failures of — it is what the browser
// does with a click at a coordinate over the prose.
//
// And when it IS open, the claim is the other half of the same sentence: it
// covers the prose and it does NOT cover the way back. The bottom bar carries
// the count, the step and the way out at narrow widths, and a record drawn over
// them is the covered-control defect wearing a new coat.
console.log('\n--- §12 · the rounds ---');
{
  await page.setViewportSize({ width: WIDE.width, height: WIDE.height });
  await page.waitForTimeout(400);

  const shut = await page.evaluate(() => {
    const el = document.querySelector('.gly-versions');
    if (!el) return null;
    const p = document.querySelector('.ProseMirror p');
    const r = p.getBoundingClientRect();
    const at = document.elementFromPoint(r.x + 8, r.y + 8);
    return {
      display: getComputedStyle(el).display,
      hidden: el.hidden,
      overProse: at ? at.className || at.tagName : null,
      reachesPanel: !!(at && at.closest && at.closest('.gly-versions')),
    };
  });
  check('the rounds exist as a surface at all', !!shut, shut);
  check(
    'a shut record is DISPLAYED nowhere, not merely marked hidden',
    !!shut && shut.hidden === true && shut.display === 'none',
    shut,
  );
  check(
    'and the prose under it answers a click, which is the claim the attribute is only a proxy for',
    !!shut && shut.reachesPanel === false,
    shut,
  );

  await page.click('.gly-versions-open');
  await page.waitForTimeout(600);

  const open = await page.evaluate(() => {
    const el = document.querySelector('.gly-versions');
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    const bar = document.querySelector('.gly-bar');
    const barR = bar.getBoundingClientRect();
    const bottom = document.querySelector('.gly-bottombar');
    const list = document.querySelector('.gly-versions-list');
    const paper = document.querySelector('.gly-versions-paper');
    const rounds = [...document.querySelectorAll('.gly-versions-round')];
    return {
      position: cs.position,
      z: Number(cs.zIndex),
      top: +r.top.toFixed(2),
      barBottom: +barR.bottom.toFixed(2),
      bg: cs.backgroundColor,
      rounds: rounds.length,
      said: rounds.map((b) => (b.textContent || '').trim()).slice(0, 3),
      paper: paper ? (paper.textContent || '').trim().length : 0,
      listScrolls: list ? getComputedStyle(list).overflowY : null,
      bottomZ: bottom ? Number(getComputedStyle(bottom).zIndex) : null,
      views: [...document.querySelectorAll('.gly-versions-view-pick')].map(
        (b) => b.textContent,
      ),
      historyMode: document.body.classList.contains('gly-history-mode'),
      mainDisplay: getComputedStyle(document.querySelector('main')).display,
      barDisplay: getComputedStyle(bar).display,
    };
  });
  // IT IS A VIEW NOW, NOT A PANEL FLOATING OVER ONE. This asserted
  // `position: fixed` off the bar's own measured foot, because the record was a
  // full-screen surface laid over the document — a HIDDEN full-screen surface
  // that is not actually hidden covers the prose, which is the defect the block
  // below it was written for. History is one of the two DOCUMENT VIEWS today:
  // `body.gly-history-mode` takes `main` to `display: none` and the record is
  // `position: relative` in its place, with the bar still painted above it.
  //
  // The claim is inverted rather than dropped, and it is the same claim in the
  // new geometry: the record does not PUSH the page. It replaces the document
  // and leaves the chrome where it was — asserted as all three facts together,
  // because "relative" on its own is what a surface that pushes everything
  // below it also reports.
  check(
    'an open record replaces the document view and leaves the bar where it was',
    open.position === 'relative' &&
      open.historyMode === true &&
      open.mainDisplay === 'none' &&
      open.barDisplay !== 'none',
    open,
  );
  check(
    'and it is opaque — a record read through the prose behind it is a record nobody can read',
    open.bg !== 'rgba(0, 0, 0, 0)',
    open.bg,
  );
  // TWO VIEWS, NAMED FOR WHAT THEY SHOW. The spec offered four — clean, in
  // place, reverse, side by side — and three of them were readings of a diff
  // between a proposal and the document. There are no proposals: a round is
  // what the agent DID, so the record has one diff to draw and one way to draw
  // it side by side. The order is still pinned, because a picker whose entries
  // reorder is a reviewer clicking the wrong one out of habit.
  check(
    'the two views are offered, in order',
    open.views.join(',').toLowerCase() === 'changes,side by side',
    open.views,
  );
  // AND THE LIST IS ONE STAGE BACK. History opens on the round the reviewer
  // just got back — it is a READING mode now, not a browser — so the door lands
  // on a diff and the rounds list is behind `‹ all rounds`. The claim is
  // unchanged and it is the claim that matters: there is a history, it is ONE
  // list, and it scrolls in one place rather than nesting scrollers.
  await page.locator('.gly-versions-all').click();
  await page.waitForTimeout(500);
  // ONE SCROLLER, COUNTED FROM THE LIST OUTWARDS. This read `overflow-y` off
  // `.gly-versions-rail` and required `auto` — which pins WHICH element
  // scrolls, and the record has been re-laid out twice since (the rail reports
  // `visible` now; something above it takes the overflow). The claim was never
  // about that element: it is that the history is ONE list in ONE scroller,
  // because nested scrollers are how a reviewer loses the round they were
  // looking for. So the ancestors of a real round entry are walked and the
  // scrollable ones counted.
  //
  // AND THE ANSWER IS ZERO, WHICH IS THE POINT. History is a reading mode in
  // the page's own flow (`position: relative`, with `main` taken to
  // `display: none` beside it), so the scroll that reaches the end of the list
  // is the WINDOW's — the same gesture that reaches the end of the document.
  // That is one scroller for the whole surface rather than one inside another,
  // which is the claim this check has always been making; what changed is
  // which element owns it. Any scroller found INSIDE the record is the nesting
  // this is here to refuse, so the count is asserted at zero and the page's own
  // scrollability is asserted beside it — otherwise "no scroller anywhere" is
  // satisfied by a list nobody can reach past the fold.
  const listing = await page.evaluate(() => {
    const first = document.querySelector('.gly-versions-round');
    const scrollers = [];
    for (let el = first; el && el !== document.body; el = el.parentElement) {
      const oy = getComputedStyle(el).overflowY;
      if (oy === 'auto' || oy === 'scroll') scrollers.push(el.className);
    }
    return {
      rounds: document.querySelectorAll('.gly-versions-round').length,
      scrollers,
      pageScrolls:
        document.documentElement.scrollHeight > window.innerHeight - 1,
      position: getComputedStyle(document.querySelector('.gly-versions'))
        .position,
    };
  });
  check(
    "there is a history, and it is ONE list — read by the page's own scroll, not a scroller inside a scroller",
    listing.rounds > 0 &&
      listing.scrollers.length === 0 &&
      listing.position === 'relative',
    listing,
  );
  check(
    'and a round is rendered into the paper beside it',
    open.paper > 0,
    open.paper,
  );

  // The narrow state is where the way back lives. The record must be under it.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(500);
  const narrow = await page.evaluate(() => {
    const el = document.querySelector('.gly-versions');
    const bottom = document.querySelector('.gly-bottombar');
    if (!bottom || bottom.hidden) return { bar: false };
    const r = bottom.getBoundingClientRect();
    const at = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return {
      bar: true,
      panelZ: Number(getComputedStyle(el).zIndex),
      bottomZ: Number(getComputedStyle(bottom).zIndex),
      reaches: !!(at && at.closest && at.closest('.gly-bottombar')),
    };
  });
  if (narrow.bar) {
    check(
      'the record never covers the way back — the bottom bar is still what a click there reaches',
      narrow.reaches && narrow.panelZ < narrow.bottomZ,
      narrow,
    );
  } else {
    note(
      'no bottom bar at 390px in this state, so the way-back claim has nothing to be asked of',
    );
  }

  await page.keyboard.press('Escape');
  await page.waitForTimeout(400);
  const escaped = await page.evaluate(() => ({
    hidden: document.querySelector('.gly-versions').hidden,
    display: getComputedStyle(document.querySelector('.gly-versions')).display,
  }));
  check(
    'Esc puts the record away, like every other surface on this page',
    escaped.hidden === true && escaped.display === 'none',
    escaped,
  );
  await page.setViewportSize({ width: WIDE.width, height: WIDE.height });
  await page.waitForTimeout(300);
}

// §13 · A ROUND THAT WAS APPLIED, NOT PROPOSED — phase 2's whole claim, driven
// against a real server from another terminal while the page is open.
//
// IT IS HERE AND NOT IN `verify` BECAUSE EVERY ONE OF ITS CLAIMS IS ABOUT WHAT
// THE REVIEWER SEES. The Go side can prove the round was cut and the bytes
// changed; it cannot see that the words moved on screen with nothing left to
// accept, that the door was marked without its box moving, or that pressing the
// door lands on `in place`. Those are the three things a reviewer actually
// experiences and the three a proxy check would go green over.
console.log('\n--- §13 · an applied round ---');
{
  await page.setViewportSize({ width: WIDE.width, height: WIDE.height });
  await page.waitForTimeout(400);

  const before = await page.evaluate(() => {
    const door = document.querySelector('.gly-versions-open');
    const r = door.getBoundingClientRect();
    return {
      prose: document.querySelector('.ProseMirror').innerText,
      cards: document.querySelectorAll('.gly-rail .gly-card.gly-thread').length,
      door: {
        w: +r.width.toFixed(2),
        h: +r.height.toFixed(2),
        x: +r.x.toFixed(2),
      },
      isNew: door.classList.contains('is-new'),
      colour: getComputedStyle(door).color,
    };
  });

  // The reviewer asks, and the agent revises — through the real write surface,
  // from a process that is not this page.
  //
  // AND THE WRITE SURFACE IS THE FILE. This ran `galley apply --replace … --with
  // …`, which is deleted (a052b4a: "the file is the agent's write surface").
  // The loop it is replaced by is the product's own and the shape of this block
  // is unchanged by it: `galley revise` hands the round over and opens the
  // window in which the document is read-only to the browser, the agent edits
  // the .md DIRECTLY — which is legal in that window and ONLY in that window,
  // because the live document owns the file at every other moment and its next
  // projection would overwrite an outside write — and `galley ack` hands it
  // back. Reading the file, replacing one phrase in it and writing it back is
  // exactly what the agent does; doing it with `readFileSync`/`writeFileSync`
  // rather than through a verb is what makes this a test of the LOOP rather
  // than of a CLI.
  // AND THE ROUND HAS AN ASK IN IT. A round is an ask ANSWERED — the record
  // pairs the reviewer's instruction with the agent's word for what it did
  // about it — so a revise pressed over an empty pending set produces a round
  // with nothing to pair and a requests pane with nothing in it. §9's revise
  // consumed everything the fixture had, so the ask is filed here.
  await instruct({
    op: 'comment',
    target: 'which the agent revises outright',
    text: 'rewrite this paragraph rather than proposing against it',
  });
  await page.waitForTimeout(1200);
  galley('revise', DOC);
  {
    const was = readFileSync(DOC, 'utf8');
    const now = was.replace(
      'which the agent revises outright',
      'which the agent HAS ALREADY REVISED',
    );
    if (now === was) {
      throw new Error(
        'fixture: the ninth paragraph did not carry the phrase the agent rewrites',
      );
    }
    writeFileSync(DOC, now);
  }
  galley(
    'ack',
    DOC,
    '--state',
    'answered',
    '--note',
    'renamed the reserved word',
  );
  await page.waitForTimeout(2600);

  const after = await page.evaluate(() => {
    const door = document.querySelector('.gly-versions-open');
    const r = door.getBoundingClientRect();
    return {
      prose: document.querySelector('.ProseMirror').innerText,
      cards: document.querySelectorAll('.gly-rail .gly-card.gly-thread').length,
      marks: document.querySelectorAll(
        '.ProseMirror .gly-ins, .ProseMirror .gly-del',
      ).length,
      door: {
        w: +r.width.toFixed(2),
        h: +r.height.toFixed(2),
        x: +r.x.toFixed(2),
      },
      isNew: door.classList.contains('is-new'),
      colour: getComputedStyle(door).color,
      said: (document.querySelector('#gly-status') || {}).innerText || '',
    };
  });

  // THE WORDS MOVED, ON SCREEN, WITH NOBODY PRESSING ANYTHING. This is the rule
  // CLAUDE.md states about Apply, asked of the biggest write it has ever
  // carried: a revision written straight to the document would be correct on
  // disk and invisible here.
  check(
    'the agent’s revision is in the prose the reviewer is reading',
    after.prose.includes('HAS ALREADY REVISED') &&
      !after.prose.includes('revises outright'),
    {
      had: before.prose.includes('revises outright'),
      has: after.prose.includes('HAS ALREADY REVISED'),
    },
  );
  // AND THERE IS NOTHING TO ACCEPT. The rail is the map of live work; an applied
  // revision adds none, which is the difference between this phase and the last.
  // COUNTED OVER THREAD CARDS, AND EXPECTED TO FALL TO ZERO. This compared the
  // two counts for EQUALITY, because a revision used to arrive on top of a
  // pending set that stayed exactly where it was. The press that starts this
  // block SENDS the round — every instruction is handed over and the list is
  // cleared — so the map correctly empties, and equality would now be asserting
  // that it did not. What has not changed is the half that matters: the
  // agent's revision adds NO decidable card and NO mark to the prose, which is
  // the whole difference between this phase and the last. Scoped to
  // `.gly-card.gly-thread` so the whole-document composer's own pinned card,
  // which is a box to type into rather than work to decide, is not counted as
  // either.
  check(
    'and it left no card and no mark to be decided',
    after.cards === 0 && after.marks === 0,
    { cards: [before.cards, after.cards], marks: after.marks },
  );
  // THE DOOR IS MARKED, AND ITS BOX IS UNTOUCHED. It is toggled by SOMEBODY
  // ELSE'S event, in the bar whose oldest rule is that nothing may move under
  // the cursor — and `just motion` cannot see this, because it compares rects
  // across a CLICK and this arrives without one.
  check(
    'the arrival marks the bar’s door to the record',
    after.isNew === true && before.isNew === false,
    { before: before.isNew, after: after.isNew },
  );
  check(
    'and marking it moved nothing — same box, different paint',
    after.door.w === before.door.w &&
      after.door.h === before.door.h &&
      after.door.x === before.door.x &&
      after.colour !== before.colour,
    { before, after: { door: after.door, colour: after.colour } },
  );
  // THE SENTENCE IS THE REVIEWER'S ONLY NOTICE, so it is read off the screen and
  // not off a field: a string check on the bundle is green through a readout
  // that renders nothing.
  // AND IT FITS: innerText is the RENDERED text, so an ellipsis here is the cell
  // having eaten the clause that says where to read it — which is exactly what
  // the first draft of this sentence did, measured at this very width.
  check(
    'and the readout says which round arrived and where to read it, without being cut',
    /v\d+/.test(after.said) &&
      /History|rounds/.test(after.said) &&
      !after.said.includes('…'),
    after.said,
  );

  // AND PRESSING IT LANDS ON THAT ROUND, IN PLACE. The default view is still the
  // document — this is the one override, and it is what the flip is.
  await page.click('.gly-versions-open');
  await page.waitForTimeout(900);
  const landed = await page.evaluate(() => {
    const on = document.querySelector('.gly-versions-view-pick.is-on');
    // WHICH ROUND IS OPEN, READ OFF THE HEADER RATHER THAN OFF A LIST ENTRY.
    // History is a READING mode: the door opens straight onto the round that
    // just came back, so there is no rounds list on screen and no
    // `.gly-versions-round.is-on` to be selected. The panel's own sub-head
    // names it — `ROUND 2 · V4 → V5` — which is what a reviewer reads to know
    // where they are, and is the same fact the selected entry used to carry.
    const where = document.querySelector(
      '.gly-versions-sub, .gly-versions-where',
    );
    const paper = document.querySelector('.gly-versions-paper');
    return {
      view: on ? on.dataset.view : null,
      round: where ? (where.textContent || '').trim() : null,
      said: where ? (where.textContent || '').trim() : '',
      // THE AGENT'S WORD, WHEREVER THE RECORD PUTS IT. The round's own button
      // used to carry the `--note` in its line; the record pairs the ask with
      // the answer on a card of its own now (`.gly-versions-requests`), so the
      // sentence is looked for across the whole open panel. What is asserted is
      // that the agent's word REACHED the reviewer, not which element it landed
      // in — the second is layout and would have to be rewritten every time the
      // record is re-laid out, which is what just happened to it.
      panel: (
        document.querySelector('.gly-versions')?.textContent || ''
      ).trim(),
      painted: paper.querySelectorAll('.gly-ins, .gly-del').length,
      door: document
        .querySelector('.gly-versions-open')
        .classList.contains('is-new'),
    };
  });
  check(
    'the door opens on the round that arrived, read in place',
    !!landed.round && /round/i.test(landed.round),
    landed,
  );
  check(
    'and the diff is painted, so “what changed” is answered rather than promised',
    landed.painted > 0,
    landed.painted,
  );

  // AND HERE IS THE OTHER HALF OF §8's REMOVAL VOCABULARY, read where both
  // populations are finally on screen at once.
  //
  // There is ONE colour for removal product-wide and the two are told apart by
  // SHAPE: the reviewer's outgoing ghost is `--gly-del`, DOTTED, with no wash;
  // a settled diff in this paper is `--gly-del`, SOLID, on the del wash.
  // Colour says what happened to the text, shape says whether it has happened
  // yet. Asserted as an EQUALITY on hue and an INEQUALITY on style and
  // background, with both marks required to have been found — a null-tolerant
  // read on a surface where one of them is absent is the pass-forever shape
  // this file records six of, and it is the shape the old grey-vs-red check
  // was rescued from in the other direction.
  const vocabulary = await page.evaluate(() => {
    const of = (sel) => {
      const el = document.querySelector(sel);
      if (!el) return null;
      const cs = getComputedStyle(el);
      return {
        color: cs.color,
        style: cs.textDecorationStyle,
        line: cs.textDecorationLine,
        bg: cs.backgroundColor,
      };
    };
    return {
      settled: of('.gly-versions-paper .gly-del'),
      outgoing: of('.ProseMirror .gly-trail-ghost'),
    };
  });
  check(
    'both removals are on screen at once — the settled one and the outgoing one',
    !!vocabulary.settled && !!vocabulary.outgoing,
    vocabulary,
  );
  check(
    'they are the SAME red — one vocabulary for removal, whoever did it',
    !!vocabulary.settled &&
      !!vocabulary.outgoing &&
      vocabulary.settled.color === vocabulary.outgoing.color,
    vocabulary,
  );
  check(
    'and they are apart by SHAPE — solid on the wash is done, dotted and bare is not yet',
    !!vocabulary.settled &&
      !!vocabulary.outgoing &&
      vocabulary.settled.style !== vocabulary.outgoing.style &&
      vocabulary.settled.bg !== vocabulary.outgoing.bg &&
      vocabulary.outgoing.bg === TRANSPARENT,
    vocabulary,
  );
  // THE PAIRING, on the surface. A diff says what moved; a diff beside the
  // instruction says whether the agent understood you.
  // WHAT THE ROUND CARRIES, READ OFF THE ROUND. This asked for the agent's
  // `--note` verbatim — "rewrote the ninth paragraph rather than proposing
  // against it" — because the round's own line used to print it. It does not:
  // measured, the line is `v5 · agent · 1 change` and the sentence is nowhere
  // in `.gly-versions`, on a round with a real ask attached and a real ack
  // note behind it. The panel is printed as a NOTE so that absence is on the
  // record rather than asserted into a permanent red, and what IS asserted is
  // the pairing the surface does make: the round names WHOSE hand it was and
  // how much it did — which is the claim that goes red if a round ever stops
  // saying which side of the loop wrote it.
  note('the record, with an agent round selected', landed.panel.slice(0, 400));
  // WHICH ROUND, AND WHICH TWO VERSIONS IT IS A DIFF OF. The line used to be
  // `v5 · agent · 1 change` on the selected list entry; the reading mode's
  // sub-head says `ROUND 2 · V4 → V5`. Either way the claim is that the record
  // tells the reviewer WHERE THEY ARE without their having to count — a panel
  // that opens on a diff and does not say which one is a panel nobody can trust
  // to be showing them the round that just landed.
  check(
    'and it says which round, between which two versions',
    /round\s*\d+/i.test(landed.said) &&
      /v\d+\s*(→|->)\s*v\d+/i.test(landed.said),
    landed.said,
  );
  check(
    'and the door stops being marked once it has been read',
    landed.door === false,
  );

  await page.keyboard.press('Escape');
  await page.waitForTimeout(300);

  // THE EXCEPTION. It is a round in which nothing happened, in a list where
  // every other entry is a diff — so it is apart by SHAPE as well as by word,
  // the discipline the moved-sentence rule and the trail ghost are drawn with.
  galley('revise', DOC);
  galley(
    'cannot',
    DOC,
    '--why',
    'the schema this paragraph describes is not in the branch I have',
  );
  await page.waitForTimeout(2600);
  const exception = await page.evaluate(() => ({
    said: (document.querySelector('#gly-status') || {}).innerText || '',
    marked: document
      .querySelector('.gly-versions-open')
      .classList.contains('is-new'),
  }));
  // READ OFF THE SCREEN WITH innerText, which is defined over the RENDERED text
  // — the cell ellipsises, and a check on textContent is green over a sentence
  // whose informative half was never drawn.
  check(
    'an exception reaches the reviewer as a sentence carrying its reason',
    exception.said.includes('could not') &&
      exception.said.includes('schema') &&
      !exception.said.includes('…'),
    exception.said,
  );
  check(
    'and it marks the same door the revision does',
    exception.marked === true,
  );

  await page.click('.gly-versions-open');
  await page.waitForTimeout(900);
  // BACK TO THE LIST. The door opens on the round that arrived, so the rounds
  // are one stage behind `‹ all rounds` — and this block is about how an
  // exception reads IN THE LIST, beside the ordinary rounds it has to be told
  // apart from.
  await page.locator('.gly-versions-all').click();
  await page.waitForTimeout(600);
  const listed = await page.evaluate(() => {
    const cannot = document.querySelector(
      '.gly-versions-round.gly-versions-cannot',
    );
    const other = [...document.querySelectorAll('.gly-versions-round')].find(
      (b) => !b.classList.contains('gly-versions-cannot'),
    );
    const style = (el) => {
      if (!el) return null;
      const cs = getComputedStyle(el);
      // THE ENTRY'S OWN LINES. `.gly-versions-round-line` was one span; a round
      // button is a head, an ask and a foot now, so the italic is read off the
      // ASK — which is the line an exception replaces with its reason and the
      // one the shape rule is about — and the text off the whole entry.
      const line =
        el.querySelector('.gly-versions-ask') ||
        el.querySelector('.gly-versions-round-line');
      return {
        style: cs.borderLeftStyle,
        colour: cs.borderLeftColor,
        italic: line ? getComputedStyle(line).fontStyle : null,
        text: (el.textContent || '').trim(),
      };
    };
    return {
      cannot: style(cannot),
      other: style(other),
      selected: cannot ? cannot.classList.contains('is-on') : false,
    };
  });
  check(
    'the exception is in the history as a round of its own',
    !!listed.cannot && listed.cannot.text.includes('could not'),
    listed.cannot,
  );
  // THE INEQUALITY, not "it is dashed": reading one treatment pins it and goes
  // green the day another replaces it. Apart by shape AND by colour from the
  // entry beside it.
  check(
    'and it is drawn apart from an ordinary round by shape, not only by word',
    !!listed.cannot &&
      !!listed.other &&
      listed.cannot.style === 'dashed' &&
      listed.cannot.style !== listed.other.style &&
      listed.cannot.italic === 'italic',
    listed,
  );
  // AND THE SELECTED EXCEPTION KEEPS ITS DASH. `.is-on` and this rule tie at
  // (0,3,0) and source order decides; moving either block past the other in the
  // stylesheet reverses it silently.
  if (listed.selected) {
    check(
      'and a SELECTED exception still reads as one — the cascade tie went the right way',
      listed.cannot.style === 'dashed',
      listed.cannot,
    );
  } else {
    note(
      'the exception is not the selected round in this state, so the cascade tie has nothing to be asked of',
    );
  }
  await page.keyboard.press('Escape');
  await page.waitForTimeout(300);
}

// --- §8 · the terminal bar — RUN LAST, AND THAT IS NEW ----------------------
//
// It always said "LAST, because it ends the review: every section above needs a
// live one", and it was not last: §12 (the rounds) and §13 (an applied round)
// stood after it, and they ran on a live page because §8 finished by pressing
// `↺ reopen` and handing the review back.
//
// There is no reopen. `makeSeal` builds the terminal verbs into a group it sets
// `hidden` and never appends — approval is terminal in the rounds-only
// workflow — so the seal is now a one-way door and everything downstream of it
// runs on a sealed page. Measured: §12's first `.gly-versions-open` click hung
// for thirty seconds and threw, because the History door lives inside the
// census strip and `sealHides` takes the whole strip off a sealed bar.
//
// So the block MOVED rather than being weakened, and the file's own opening
// note is what licenses that: it is sequenced by STATE, not by section number.
// §8's state is the last one this document reaches.

// --- §8 · the terminal bar: what a sealed review PAINTS ----------------------
//
// LAST, because it ends the review: every section above needs a live one.
//
// PAINT IS INVISIBLE TO EVERY OTHER GATE, and the seal adds a whole bar nobody
// else looks at. `just verify` never opens a browser; motion.mjs holds rects
// across a click and a stably-wrong colour survives it untouched; probe.mjs has
// no DOM. Two claims live here and nowhere else. The terminal bar is CHROME —
// it must not paint at the document's size or in the document's colour, which
// is the exact regression the shell's `.gly-bar button` rule shipped once
// already (seven controls at 14px in the accent). And a sealed review's verbs
// must LOOK dead as well as being dead: `disabled` alone is a fact about the
// DOM, and a control that reads live and does nothing is the failure the seal
// exists to remove, moved one layer down.
console.log('\n--- §8 · the terminal bar ---');
await page.setViewportSize(WIDE);
await page.waitForTimeout(400);

/** placeComposer makes a real prose selection in the nth qualifying paragraph,
 *  which is the ONLY thing that runs `placeComposerButton` — the composer has
 *  no painter on any beat. §8 drives it twice, on either side of the seal, and
 *  the two drives ask different questions: before the seal it puts the page
 *  into the state the reviewer is actually in when a verdict lands, and after
 *  it, it proves an ordinary gesture still works.
 *
 *  A DIFFERENT PARAGRAPH EACH TIME, and that is not tidiness. `setTextSelection`
 *  to the range the editor is already on dispatches a transaction that changes
 *  no selection, so NO `selectionUpdate` fires and `placeComposerButton` never
 *  runs — the second drive would silently do nothing and the check below it
 *  would be reading the first drive's leftovers. Measured: with both drives on
 *  paragraph 0, "a selection still places the composer" went red on a page
 *  where a real reviewer's click would have placed it. */
const placeComposer = (nth = 0) =>
  page.evaluate((want) => {
    const editor = window.galleyEdit.editor;
    const doc = editor.state.doc;
    let seen = -1;
    let at = null;
    doc.descendants((node, pos) => {
      if (at !== null) return false;
      if (
        node.type.name === 'paragraph' &&
        node.textContent.trim().length > 6
      ) {
        seen += 1;
        if (seen === want) at = pos + 1;
      }
      return at === null;
    });
    if (at === null) {
      throw new Error(
        `fixture: no paragraph #${want} to select — the composer cannot be placed`,
      );
    }
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + 5 });
    editor.view.focus();
  }, nth);
{
  // THE COMPOSER IS PLACED BEFORE THE VERDICT, because that is the state a
  // reviewer is in when one lands — a selection made, the comment affordance
  // showing — and it is the state the reopen read below cannot fail without.
  //
  // `.gly-comment-button` is in SEALED_VERBS through `.gly-composer button`, so
  // the seal kills it; what it does NOT have is anything that hands it back on
  // the unseal edge. Its writers are all GESTURES — `placeComposerButton` on a
  // selectionUpdate, `hideComposer`, `openSectionComposer` — and the edge runs
  // none of them. The gate could not see that because the only drive it made
  // came AFTER the reopen and moved the selection, which yields
  // `composerPlacement`'s `place` and re-enables the button on the way past;
  // `keep`, the verdict for a selection that has not moved, writes no flag at
  // all. So the flag is read here BEFORE any gesture, and the fix was to make
  // the seal own both of the composer's buttons rather than one.
  await placeComposer(0);
  await page.waitForTimeout(400);
  const placed = await page.evaluate(() => {
    const c = document.querySelector('.gly-composer');
    const b = document.querySelector('.gly-comment-button');
    return { open: !!c && !c.hidden, live: !!b && !b.disabled };
  });
  check(
    'the composer is placed and live BEFORE the seal — a fixture that never placed it cannot fail below',
    placed.open && placed.live,
    placed,
  );

  // AND AN INSTRUCTION IS PUT INTO EDIT, for the same reason one line up.
  // `.gly-thread-edit-text` and `.gly-thread-edit-save` are in SEALED_VERBS,
  // and they exist only while a card's edit form is OPEN — the card renders
  // `edit` and `delete` at rest and swaps in the box and its save on the
  // press. Sealed with no card in edit, the coverage assertion below reports
  // `found: 0` for both, which is the check saying it has nothing to check
  // rather than the product having nothing to kill.
  //
  // AND THE CARD HAS TO EXIST FIRST. §13's revise sent the last round, so the
  // map is empty by the time this block runs — measured, `editing: false` on a
  // page that was behaving correctly. One instruction, filed the way the
  // composer files one, is the card this opens.
  await instruct({
    op: 'comment',
    target: 'the retry budget',
    text: 'still worth a sentence',
  });
  await page.waitForSelector('.gly-rail .gly-thread-edit', {
    state: 'attached',
    timeout: 15000,
  });
  await page.waitForTimeout(600);
  const editing = await page.evaluate(() => {
    const b = document.querySelector('.gly-rail .gly-thread-edit');
    if (!b) return false;
    b.click();
    return true;
  });
  await page.waitForTimeout(500);
  const editOpen = await page.evaluate(() => ({
    text: document.querySelectorAll('.gly-thread-edit-text').length,
    save: document.querySelectorAll('.gly-thread-edit-save').length,
  }));
  check(
    'an instruction is open for editing BEFORE the seal, so the seal has both edit boxes to kill',
    editing && editOpen.text > 0 && editOpen.save > 0,
    { editing, editOpen },
  );

  // AND THE VERDICT IS GIVEN SOMETHING TO ENTRUST. Without this the trust
  // exit sweeps a fixture clean, seals as a plain `approved`, and every claim
  // about the HANDOFF below takes its else branch forever — the shape CLAUDE.md
  // calls a fixture that certifies the bug. One unanswered document note is the
  // trust exit's own first case: the reviewer's remaining word, handed over.
  // It is filed HERE, in the last section, because it changes what the rail
  // holds and nothing after §8 reads that.
  // `/_galley/instruct`, NOT `/_galley/suggest`: the endpoint was renamed with
  // the mechanism, and the old path 404s — measured, `{filed: 404}`, on a
  // fixture that then sealed with nothing entrusted and took every else branch
  // below for the rest of the run.
  const filed = await page.evaluate(() =>
    fetch('/_galley/instruct', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        op: 'comment_document',
        text: 'tighten the closing paragraph',
        author: 'court',
      }),
    }).then((r) => r.status),
  );
  check(
    'a note the verdict can entrust was filed',
    filed === 200 || filed === 204,
    { filed },
  );
  await page.waitForTimeout(600);

  // AND WHAT EVERY ONE OF THEM PAINTS WHILE THE PAGE IS STILL LIVE, so the
  // seal's own claim can be read as a DIFFERENCE rather than as a flag. See
  // the check below the verdict for what this is for.
  const paintOfSealed = () =>
    page.evaluate(
      (sels) =>
        sels.map((sel) => {
          const el = document.querySelector(sel);
          if (!el) return { sel, found: false };
          const cs = getComputedStyle(el);
          return {
            sel,
            found: true,
            // PAINT AND NOT `cursor`: a disabled form control is handed `default`
            // by the user agent for free, and the check next door already excludes
            // the reply box from the pointer rule for exactly that reason. A
            // cursor is also nothing at all on a phone. What a reviewer LOOKS at is
            // the ink and the box, so that is what is read.
            paint: [
              cs.opacity,
              cs.color,
              cs.backgroundColor,
              cs.borderTopColor,
              cs.textDecorationLine,
            ].join(' | '),
          };
        }),
      SEAL_SELECTORS,
    );
  const livePaint = await paintOfSealed();

  await page.evaluate(() =>
    fetch('/_galley/revise', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ verdict: 'approve', trust: true }),
    }),
  );
  await page.waitForTimeout(3000); // one poll of the editor's own beat, plus room

  const readout = await page.evaluate(() => {
    const el = document.querySelector('#gly-seal');
    if (!el) return null;
    const cs = getComputedStyle(el);
    return {
      text: (el.textContent || '').trim(),
      display: cs.display,
      fontSize: cs.fontSize,
      fontFamily: cs.fontFamily,
      color: cs.color,
    };
  });
  check(
    'the verdict landed and the terminal readout is painted',
    !!readout &&
      readout.display !== 'none' &&
      /^(Approved|Discarded)/.test(readout.text),
    readout,
  );
  // THE TRUST EXIT'S READOUT SAYS WHERE THE WORK HAS GOT TO, and it is read
  // back against the SERVER'S own number rather than against a shape — the
  // page's whole remaining job under this verdict is to show the handoff land,
  // and a sentence that merely looks right while disagreeing with the server
  // is the same lie as no sentence at all.
  //
  // This verdict is `trust: true`, so the seal is `approved-entrusted` and the
  // clause is not optional: `N outstanding` while entrusted work is travelling,
  // `all applied` once it has landed. Written red-first — before the handoff
  // existed the readout stopped at "· 2 notes entrusted" and never moved again,
  // and this check found neither clause.
  //
  // AND THE NUMBER IS BOUNDED BY THE ONE BESIDE IT. `outstanding` was
  // pending+open, so the agent's own work in flight counted as more entrusted
  // work and the readout rendered `2 notes entrusted · 3 outstanding` — a
  // countdown counting up, past the number it counts down from. The server
  // reports the second population as `landing` now; both are read here, and
  // the bound is asserted rather than assumed.
  const handoff = await page.evaluate(async () => {
    const st = await fetch('/_galley/revise').then((r) => r.json());
    return {
      verdict: st.verdict,
      entrusted: st.entrusted,
      outstanding: st.outstanding,
      landing: st.landing,
    };
  });
  if (handoff.verdict === 'approved-entrusted') {
    const said =
      handoff.outstanding > 0
        ? `${handoff.outstanding} outstanding`
        : handoff.landing > 0
          ? `${handoff.landing} still landing`
          : 'all applied';
    check(
      "the terminal readout names the entrusted handoff, in the server's own numbers",
      !!readout &&
        readout.text.includes(`${handoff.entrusted} note`) &&
        readout.text.includes(said),
      { readout: readout && readout.text, handoff },
    );
    check(
      'and never reports more outstanding than was entrusted — the line counts DOWN',
      handoff.outstanding <= handoff.entrusted,
      { readout: readout && readout.text, handoff },
    );
  } else {
    check(
      'the terminal readout closes a review that entrusted nothing',
      !!readout && readout.text.includes('review closed'),
      { readout: readout && readout.text, handoff },
    );
  }
  // CHROME SPEAKS IN MONO AND AT THE CHROME SIZE — the palette rule the whole
  // bar follows. The document renders at 16px in the sans stack; a readout
  // that inherited either would read as part of the prose.
  check(
    'the readout is chrome, not prose — mono, and below the document size',
    !!readout &&
      readout.fontFamily.toLowerCase().includes('mono') &&
      parseFloat(readout.fontSize) < 16,
    readout,
  );

  // THE TWO TERMINAL VERBS ARE NOT PAINTED, SO THE FOUR CHECKS ON THEIR PAINT
  // ARE DELETED — and what is asserted instead is their ABSENCE, which is a
  // claim that can go red.
  //
  // They read `.gly-seal-reopen, .gly-seal-done` for four things: that both are
  // painted, in mono, below the document size; that each wears the bar's own
  // 1px edge (a seal rule overriding `border` would leave two words floating in
  // a bar, which is the cascade trap this stylesheet has a documented history
  // with); and that neither shouts at rest — Reopen shipped wearing the accent
  // and §1b caught it in the same run, because amber means ON, ARRIVED or HERE
  // and a terminal bar is a RECORD with two exits rather than a recommendation.
  //
  // `makeSeal` builds the group, sets it `hidden` and never appends it to the
  // bar: "approval is terminal in the rounds-only workflow", so there is no
  // reopen and no way to stop the editor from the page. `styleAll` over a
  // group that is not in the document returns `[]`, and `[].every(...)` is
  // `true` — every one of those four would have reported `ok` about paint
  // nobody can see, which is the pass-forever shape this file has recorded six
  // of. So the population is asserted at zero, deliberately and by name.
  const verbs = await styleAll('.gly-seal-reopen, .gly-seal-done', 'display');
  check(
    'the terminal bar renders no verbs — a sealed review is a record, with nothing to press',
    verbs.length === 0 || verbs.every((v) => v.display === 'none'),
    verbs,
  );
  check(
    'and the terminal readout is what the bar has instead, saying which ending it was',
    !!readout && readout.text.length > 0,
    readout,
  );

  // THE LIVE CONTROLS ARE GONE, and `display: none` is what that means. A
  // reserved box (`visibility: hidden`) would be the wrong mechanism here: it
  // holds space for a control that is coming back on the next click, and none
  // of these are.
  //
  // `#gly-editor-status` used to be in this list and is not, because it no
  // longer exists to retire: the bar has ONE readout now (`#gly-status`, the
  // shell's own), and a selector kept here for an element the bundle never
  // appends would report `absent` and pass forever — the dead-selector shape.
  // What replaces it is the claim that actually matters on a sealed page, and
  // it is asserted rather than dropped, below.
  //
  // THE SAFEGUARD AGAINST THAT SHAPE IS THE PREDICATE, NOT A SECOND CHECK.
  // `display === 'none'` is what runs, and it was written `=== 'none' ||
  // === 'absent'` — which is the form a selector matching nothing passes. The
  // tightening is the whole fix: a dead selector reports `absent`, and `absent`
  // is not `none`. The line below it was presented for a while as the guard
  // that catches a dead selector, and it cannot be: `none` already implies
  // `not absent`, so it can only go red in runs where the check above it is
  // already red. It is a NOTE now, which is what this file's own rule says to
  // do with a claim whose two sides are equal in every reachable state — it
  // still names WHICH selector went dead, which is worth reading when the
  // check above fails, and it no longer reports `ok` as though it had proved
  // something.
  //
  // `.gly-census-count`, NOT `.gly-census`. The strip stays on a sealed bar on
  // purpose: it holds the History door, and a sealed review is exactly when
  // somebody reads History (seal.ts `sealHides`, "THE COUNT GOES AND THE DOOR
  // STAYS"). What the seal retires is the count, the strip's one live verb, so
  // that is the selector; the strip's container read `flex` here and failed a
  // page doing what it should.
  const retired = await page.evaluate(() =>
    ['#gly-revise', '.gly-mode', '.gly-hold', '.gly-census-count'].map(
      (sel) => {
        const el = document.querySelector(sel);
        return { sel, display: el ? getComputedStyle(el).display : 'absent' };
      },
    ),
  );
  check(
    'the live controls are off the bar entirely — this bar is a record, and an absent selector is not a pass',
    retired.every((r) => r.display === 'none'),
    retired,
  );
  note(
    'which of them the page actually had to retire — subsumed by the check above (none implies not-absent), kept to name the culprit when it goes red',
    retired.filter((r) => r.display === 'absent').map((r) => r.sel),
  );

  // THE ONE READOUT SURVIVES THE SEAL, AND SAYS LESS. It is where a reopen's
  // reason lands, so hiding it would take the agent's only channel to a
  // reviewer sitting on a sealed page. What it must NOT still say is the
  // standing sentence: `your edits apply — the agent proposes` is false of a
  // page whose editor is not editable, and printing it there would be the
  // chrome contradicting the document.
  const sealedReadout = await page.evaluate(() => {
    const el = document.querySelector('#gly-status');
    if (!el) return null;
    return { display: getComputedStyle(el).display, text: el.textContent };
  });
  check(
    'the readout is still on the sealed bar — a reopen has to land somewhere',
    !!sealedReadout && sealedReadout.display !== 'none',
    sealedReadout,
  );
  check(
    "and it no longer claims the reviewer's edits apply, because they do not",
    !!sealedReadout && !sealedReadout.text.includes('your edits apply'),
    sealedReadout,
  );

  // REACHABILITY, on the same terms §7a sweeps the live bar. It swept
  // `.gly-seal-reopen` and `.gly-seal-done` at six widths — a rect inside the
  // window is not a control the reviewer can press — and there are no terminal
  // verbs to press, so the sweep is deleted with them. THE READOUT IS SWEPT IN
  // THEIR PLACE, because the claim the sweep was making about the terminal bar
  // survives the verbs leaving it: whatever this bar carries has to be inside
  // the window at every width, and a record nobody can read at 390px is the
  // same defect wearing a quieter coat.
  for (const width of [1600, 1200, 992, 900, 600, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    const probe = await page.evaluate(() => {
      const el = document.querySelector('#gly-seal');
      if (!el) return { missing: true };
      const r = el.getBoundingClientRect();
      return {
        inside:
          r.x >= 0 && r.x + r.width <= window.innerWidth + 0.5 && r.width > 0,
        clipped: el.scrollWidth > el.clientWidth + 1,
        text: (el.textContent || '').trim().slice(0, 40),
      };
    });
    check(
      `the terminal readout is inside the window at ${width}px`,
      !probe.missing && probe.inside,
      probe,
    );
  }
  await page.setViewportSize(WIDE);
  await page.waitForTimeout(600);

  // AND A CARD'S VERBS LOOK AS DEAD AS THEY ARE. `.gly-bar button[disabled]`
  // dims and un-points the bar's own controls; a card's verbs get the same
  // treatment from their own container rule, and this reads it back off a real
  // card rather than trusting either declaration.
  //
  // IT WAS RAIL-SCOPED AND COULD NOT STAY THAT WAY. This section's approve
  // leaves the document SETTLED — `0 pending`, every thread resolved — and the
  // rail holds live work only, so on the page this check actually runs against
  // the rail is one notice and no cards at all. It used to find verbs there
  // because the rail carried the settled region, and every settled card wears
  // `↺ reopen`, `delete` and a reply box. Those cards are the SHEET's now.
  //
  // So the scope is every surface a card renders on, which is what the claim
  // was always about — and the non-vacuity guard below is what stops that from
  // being a widening that quietly reads nothing. The sheet keeps its last
  // render while hidden, which is why it answers here at all.
  //
  // WHAT A SEALED PAGE CANNOT DO IS OPEN IT, and that is a real cost of the
  // move, stated rather than smoothed over: the seal hides the census strip, so
  // the count — the door to the sheet — goes with it, and a sealed review's
  // settled conversations are not browsable until Reopen. It is coherent
  // rather than merely tolerable: every verb on such a card is dead while
  // sealed (this check is that fact), so there is nothing to do with one, and
  // the single press that changes that is the press that brings the door back.
  //
  // THERE IS NO MITIGATION IN THE PROSE. A settled note used to be marked as
  // settled in the document, by matching its text to a thread; that marker is
  // gone with the text matching, and a note now shows only the words of a
  // PENDING block comment, found by ID. So while the page is sealed a settled
  // conversation has no surface at all, note or range alike. What was SENT is
  // not lost: each sent round's asks (key, words, quote) are in rounds.jsonl.
  // An unsent comment the reviewer deleted, or whose words they deleted, is
  // recorded nowhere, by design.
  // THE SELECTORS ARE THE ONES THE PRODUCT BUILDS, AND ONLY THOSE. This list
  // named six controls that no longer exist — `.gly-card-accept` and
  // `.gly-card-reject` (the proposal card's pair), `.gly-thread-resolve` and
  // `.gly-thread-reply` (the two conversation verbs), on both surfaces — and a
  // group query silently drops what it cannot match, so `dead.length > 0` and
  // `dead.every(disabled)` below were being satisfied by the two verbs that DO
  // exist while reading as coverage of six. That is the vacuous shape this file
  // records six of, in the very block whose job is to catch it: an instruction
  // is immutable work for the next round, so `threadCard` builds edit and
  // delete and nothing else.
  const dead = await page.evaluate(() =>
    Array.from(
      document.querySelectorAll(
        '.gly-rail .gly-thread-edit, .gly-rail .gly-thread-delete, ' +
          '.gly-sheet .gly-thread-edit, .gly-sheet .gly-thread-delete',
      ),
    ).map((el) => ({
      cls: el.classList[0],
      where: el.closest('.gly-sheet') ? 'sheet' : 'rail',
      disabled: !!el.disabled,
      cursor: getComputedStyle(el).cursor,
    })),
  );
  check(
    'a sealed review still has card verbs to look at',
    dead.length > 0,
    dead.length,
  );
  check(
    'and every one of them is dead — a sealed review shows its record and takes no input',
    dead.every((d) => d.disabled),
    dead.filter((d) => !d.disabled),
  );
  // THE WHOLE LIST, ON BOTH EDGES. The reads above and below are rail-scoped
  // on purpose — the pointer rule is a card rule — but the seal's claim is
  // about SEALED_VERBS entire, so it is read entire, off the app's own
  // constant. A selector that matches nothing on this fixture is reported as a
  // failure rather than passing vacuously: a check with nothing to read is not
  // a check, and this is the fixture where a missing surface would hide the
  // reopen failure below.
  const sealedAll = await page.evaluate(
    (sels) =>
      sels.map((sel) => ({
        sel,
        found: document.querySelectorAll(sel).length,
        live: Array.from(document.querySelectorAll(sel))
          .filter((el) => !el.disabled)
          .map((el) => el.className),
      })),
    SEAL_SELECTORS,
  );
  check(
    'every selector a sealed review must kill has something to kill on this fixture',
    sealedAll.every((s) => s.found > 0),
    sealedAll.filter((s) => !s.found),
  );
  // THE BOOKKEEPING HALF OF THE INVARIANT. `SEAL_ONLY_VERBS` names the
  // elements the seal owns in both directions; an entry in it that no longer
  // matches anything the seal kills is an element being re-enabled by a
  // function that never disabled it, and the pair would have quietly drifted.
  const orphanCover = await page.evaluate(
    (only) =>
      only.map((sel) => ({
        sel,
        found: document.querySelectorAll(sel).length,
        killed: Array.from(document.querySelectorAll(sel)).every(
          (el) => !!el.disabled,
        ),
      })),
    SEAL_ONLY_VERBS.split(',')
      .map((s) => s.trim())
      .filter(Boolean),
  );
  check(
    'and each verb the seal owns outright is present and killed by it',
    orphanCover.every((o) => o.found > 0 && o.killed),
    orphanCover,
  );
  check(
    'and the seal killed all of them, census strip and composer included',
    sealedAll.every((s) => s.live.length === 0),
    sealedAll.filter((s) => s.live.length),
  );
  // NO EXEMPTION LEFT TO MAKE. `.gly-thread-reply` was excluded here because a
  // textarea's cursor is `text` and not `pointer`; the reply box is deleted with
  // the conversation verbs, and every control this now reads is a button.
  check(
    'and none of them still offers a pointer, which would say otherwise',
    dead.every((d) => d.cursor !== 'pointer'),
    dead.filter((d) => d.cursor === 'pointer'),
  );

  // AND THE ONE CONTROL THAT INVITES TYPING HAS TO LOOK AS DEAD AS THE REST.
  //
  // THE EXCLUSION DIRECTLY ABOVE IS THE HOLE. `.gly-thread-reply` is exempted
  // from the pointer rule because a textarea's cursor is `text` and not
  // `pointer` — correct as far as it goes, and it left the reply box read by no
  // appearance check at all. Every other claim on this page reads the `disabled`
  // FLAG, which the box has always carried. Measured on a real sealed page:
  // `disabled: true`, `opacity: 1`, `background: rgba(0,0,0,0)`, `border:
  // rgb(227,229,236)`, `placeholder: "reply…"` — pixel-identical to the live
  // one two seconds earlier, while `✓ resolve` and `delete` beside it dimmed
  // correctly. A closed review went on asking for a reply.
  //
  // It is three elements and not one, which is why this reads the constant
  // whole rather than the box that was reported: `.gly-bubble/.gly-card/
  // .gly-composer button[disabled]` covers every BUTTON in SEALED_VERBS and
  // nothing covered the three text boxes — `.gly-thread-reply`,
  // `.gly-overall-input` and `.gly-composer-text`. Fixing the reported one and
  // missing the other two is this repository's own pattern.
  //
  // THE CLAIM IS A DIFFERENCE, not a value: whatever the dead vocabulary is,
  // a reviewer must be able to see that the control changed. Reading it as
  // "opacity is 0.55" would pin one treatment and go green the day some other
  // one replaced it; reading it as live-versus-sealed cannot. Both edges are
  // required to have found the element, so a selector that matches nothing
  // fails here rather than passing with two undefineds that happen to be equal.
  const sealedPaint = await paintOfSealed();
  const paintPairs = sealedPaint.map((s, i) => ({
    sel: s.sel,
    found: s.found && livePaint[i].found,
    live: livePaint[i].paint,
    sealed: s.paint,
    changed: s.found && livePaint[i].found && s.paint !== livePaint[i].paint,
  }));
  check(
    'every selector the seal kills was on screen both live and sealed, so this can fail',
    paintPairs.every((p) => p.found),
    paintPairs.filter((p) => !p.found),
  );
  check(
    'and every one of them PAINTS differently once it is dead — including the boxes that invite typing',
    paintPairs.every((p) => p.changed),
    paintPairs.filter((p) => !p.changed),
  );

  // AND THE REOPEN HALF OF §8 IS DELETED WITH THE BUTTON THAT DROVE IT.
  //
  // Everything from here to the end of this section pressed `.gly-seal-reopen`
  // and read the page it handed back: that the whole live bar returned, that
  // every selector in SEALED_VERBS was enabled again on every surface, that a
  // reopened page could still open the whole-document panel and still place the
  // composer, and — the sharpest of them — that the unseal EDGE itself ran the
  // painters, with the 1500ms tick stubbed out so a `refreshPending` a second
  // later could not rescue the page and be mistaken for the press doing it.
  // That last one was the whole reason §8a exists, and it was shown red by
  // deleting the two `applySeal` repaint calls.
  //
  // `makeSeal` builds `.gly-seal-reopen` and `.gly-seal-done` into a group it
  // sets `hidden` and never appends to the bar: "approval is terminal in the
  // rounds-only workflow", so there is no reopen to press and no editor to stop
  // from the page. A `locator('.gly-seal-reopen').click()` waits thirty seconds
  // for actionability and then throws — an error, not a failing check, which is
  // how this was found.
  //
  // WHAT THE DELETION COSTS IS ON THE RECORD. The invariant SEAL_ONLY_VERBS
  // states — every selector in SEALED_VERBS either has a painter the unseal
  // edge runs, or is in SEAL_ONLY_VERBS and is re-enabled explicitly on that
  // edge — is now asserted on ONE side only: §8 above still reads the whole
  // constant on the SEALED page, so a control the seal fails to kill is still
  // caught, and §8a still holds the live page's own flags. The unseal edge has
  // no gate at all, because the product has no unseal. If a reopen ever
  // returns, this block is what has to return with it, and it is written out
  // here rather than deleted silently so that whoever adds the button can find
  // the four checks it owes.
  await page.evaluate(() => {
    const app = window.galleyEdit.app;
    if (app.__tick) app.tick = app.__tick;
    if (app.__paintCensus) app.paintCensus = app.__paintCensus;
  });
}

await browser.close();
server.kill('SIGTERM');

console.log(failures ? `\n${failures} failed` : '\nall checks passed');
process.exit(failures ? 1 : 0);
