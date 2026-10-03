// align.mjs — THE CONTENT PANE ALIGNS TO THE LIVE PAGE, AND CLEARS OUTSIDE
// BOTH VIEW.
//
// In page mode's Both view the two panes show the same document twice: the
// real page on the left, the editable prose on the right. They cannot agree
// line for line — a hero is tall on the page and one line in the editor — so
// the panes are aligned at the SECTION level: each content heading is nudged
// DOWN, by a measured amount, to sit level with the same heading in the page.
// The nudge is a ProseMirror node decoration writing `padding-top` (web/
// headingalign.ts), fed a measured map by alignHeadings() in web/preview.ts.
//
// NO OTHER GATE CAN SEE THIS. page.mjs proves the page survives the loop and
// never opens Both view's geometry; probe.mjs has no browser; the Go side has
// never heard of a heading's padding. Every claim here is a MEASUREMENT taken
// in a real chromium against a real page, which is the only place the feature
// exists. Its four claims:
//
//   ALIGNMENT IS APPLIED — after the page settles, at least one content
//   heading carries a padding-top the editor never wrote itself.
//
//   THE CAP IS RESPECTED — no heading is bumped past MAX_ALIGN_BUMP_PX (260).
//   That cap is the whole trade: where a page region renders far taller, the
//   honest gap is a full screen, and a gate that only checked "aligned" would
//   be green on a blank screen of spacer.
//
//   IT ACTUALLY ALIGNS A SECTION — the first bumped heading's TEXT top lands
//   within a tolerance of its page counterpart's top, measured through the
//   iframe's own offset. This is the claim the padding is FOR; the two above
//   are satisfied by any padding at all.
//
//   IT CLEARS OUTSIDE BOTH, AND COMES BACK — Content view leaves every heading
//   at padding 0 (a spacer with nothing to align to is a hole in the prose),
//   and returning to Both re-measures rather than restoring a stale map.
//
// Plus the direction: the feature only ever pushes DOWN. A heading the page
// renders ABOVE the editor's own position gets nothing, because pulling up
// would need negative space the editor has no room for.
//
// PIXELS, BUT NOT EXACT PIXELS. Every number here is a threshold with slack —
// fonts, images and the iframe's own growth move the page by a few px between
// runs — and a heading whose counterpart cannot be found is SKIPPED rather
// than failed, because the match key is text and a fixture is allowed to have
// prose the page renders in a form the editor does not.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/align.mjs
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. There is a `just align`
// recipe, it is in `just gates`, and it is a step of its own in ci.yml.

import { spawn } from 'node:child_process';
import { cpSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const GALLEY = process.env.GALLEY || 'bin/galley';
// Its own port. The gates bind fixed ports and run one at a time; 8266 is the
// next free one after codeblock.mjs's pair (8264/8265).
const PORT = Number(process.env.PORT || 8266);
const HERE_DIR = dirname(fileURLToPath(import.meta.url));

// THE FIXTURE, chosen for its DRIFT. galley-tools-index.html opens on a hero
// that pushes its first heading far down the rendered page while the editor
// starts that same heading near the top — real, large, top-of-document drift,
// which is exactly the gap the feature closes — and it carries a run of
// further sections beneath, so the cumulative top-to-bottom rule is exercised
// rather than one lucky heading. page.mjs opens the same page for its own
// claims.
const FIXTURE = join(
  HERE_DIR,
  '..',
  'internal',
  'htmlpage',
  'testdata',
  'galley-tools-index.html',
);

// web/preview.ts's own cap, restated. A gate that reads the product's constant
// through an import would be checking the code against itself; this is the
// NUMBER the feature promises, written out so a change to it is a change here.
const MAX_BUMP = 260;
// Slack on the cap: sub-pixel layout and the browser's own rounding.
const CAP_SLACK = 4;
// How close "level with the page" has to be to count. Section-level alignment
// is measured against the heading's counterpart in a document laid out by
// entirely different CSS; 40px is under one line of body text at this size,
// and the drift it replaces is hundreds.
const LEVEL_PX = 40;
// Slack on the measurement identity below. The layout is read a moment after
// the feature measured it, and a page that is still settling moves a few px.
const MEASURE_SLACK = 8;

// Alignment is debounced ~120ms and measures on the NEXT animation frame,
// after the iframe's load, its fonts, and its images have each moved the
// layout and re-armed the ResizeObserver. This is the settle, generously.
const SETTLE_MS = 2500;
// A view change re-runs the same debounce with no reload behind it.
const VIEW_MS = 1200;

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

const HERE = mkdtempSync(join(tmpdir(), 'galley-align-'));
const PAGE = join(HERE, 'index.html');
cpSync(FIXTURE, PAGE);

const server = spawn(
  GALLEY,
  ['edit', PAGE, '--no-open', '--port', String(PORT)],
  { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env } },
);
let serverSaid = '';
for (const stream of [server.stdout, server.stderr]) {
  stream.on('data', (b) => {
    serverSaid = (serverSaid + b).slice(-8000);
  });
}
server.on('exit', (code, signal) => {
  // SIGTERM is this gate's own shutdown and a graceful exit reports code 0 /
  // signal null — neither is a crash, so neither prints (codeblock.mjs's
  // quieting, for the same reason: a clean shutdown that looks like a
  // diagnostic teaches a reader to ignore diagnostics).
  if (signal === 'SIGTERM' || code === 0) {
    return;
  }
  console.log(
    `      [server exited] code=${code} signal=${signal}\n${serverSaid.trim()}`,
  );
});

let browser = null;
process.on('exit', () => {
  try {
    const proc = browser && browser.process();
    if (proc) proc.kill('SIGKILL');
  } catch {
    // Already gone.
  }
  try {
    server.kill('SIGTERM');
  } catch {
    // Already gone.
  }
  try {
    rmSync(HERE, { recursive: true, force: true });
  } catch {
    // A tmpdir left behind is nothing; a throwing exit handler is not.
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

browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);
const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
page.on('pageerror', (e) => console.log(`      [page error] ${e.message}`));
await page.goto(`${base}/`, { waitUntil: 'networkidle' });
await page.waitForSelector('.ProseMirror', { timeout: 15000 });
// The preview pane is the other half of every claim here: no iframe, no
// measurement to align to.
await page.waitForSelector('#gly-preview', { timeout: 15000 });
await page.waitForFunction(
  () => {
    const f = document.getElementById('gly-preview');
    const d = f && f.contentDocument;
    return !!d && d.readyState === 'complete' && !!d.querySelector('h1,h2,h3');
  },
  null,
  { timeout: 20000 },
);
await page.waitForTimeout(SETTLE_MS);

// THE ONE READING every check below is taken from, so a claim is never made
// against a layout a later query re-measured. For each content heading: its
// text, its computed padding-top, its border-box top (which is where the
// heading WOULD sit with no padding of its own — padding pushes the text down
// inside the box, not the box), and the top of the page's own heading of the
// same text, expressed in the outer document's coordinates through the
// iframe's offset. Unmatched headings carry pageTop: null and are skipped.
const read = async () =>
  await page.evaluate(() => {
    // The same key web/headingalign.ts exports as normHeading — copied, not
    // imported, because this file runs in the browser where the module's
    // TypeScript is not available. Whitespace-collapsed and lowered, so
    // `# Heading` in the editor meets `<h2>Heading</h2>` on the page.
    const norm = (s) => (s ?? '').replace(/\s+/g, ' ').trim().toLowerCase();
    const H = 'h1,h2,h3,h4,h5,h6';
    const mount = document.querySelector('#editor .ProseMirror');
    const frame = document.getElementById('gly-preview');
    const idoc = frame ? frame.contentDocument : null;
    const ifTop = frame ? frame.getBoundingClientRect().top : 0;
    const pageTops = new Map();
    if (idoc) {
      for (const h of idoc.querySelectorAll(H)) {
        const key = norm(h.textContent);
        // First occurrence wins, the same order alignHeadings walks in.
        if (!pageTops.has(key)) {
          pageTops.set(key, ifTop + h.getBoundingClientRect().top);
        }
      }
    }
    const out = [];
    for (const h of mount ? mount.querySelectorAll(H) : []) {
      const key = norm(h.textContent);
      out.push({
        key,
        pad: parseFloat(getComputedStyle(h).paddingTop) || 0,
        boxTop: h.getBoundingClientRect().top,
        pageTop: pageTops.has(key) ? pageTops.get(key) : null,
      });
    }
    return { view: document.body.className, heads: out };
  });

const both = await read();

// --- §1 alignment is applied -------------------------------------------

// A MAJORITY MUST MATCH, not merely one. The gate re-spells MAX_BUMP and
// normHeading (the browser cannot import the .ts), so a product-side key change
// would make matching SKIP headings rather than fail — every later check reads
// only matched rows, so one lucky match could carry a broken feature green.
// Requiring half the content headings to find a page counterpart makes that
// drift red here instead of shrugging past it.
const matchedCount = both.heads.filter((h) => h.pageTop !== null).length;
check(
  'page mode opened in Both view, with most headings matched across panes',
  both.view.includes('gly-view-both') &&
    both.heads.length > 0 &&
    matchedCount >= Math.ceil(both.heads.length / 2),
  {
    view: both.view,
    heads: both.heads.length,
    matched: matchedCount,
  },
);
check(
  'at least one content heading is nudged down to meet the page',
  both.heads.some((h) => h.pad > 0),
  both.heads.map((h) => [h.key.slice(0, 40), Math.round(h.pad)]),
);

// --- §2 the cap is respected -------------------------------------------

{
  const over = both.heads.filter((h) => h.pad > MAX_BUMP + CAP_SLACK);
  check(
    `and no heading is bumped past the ${MAX_BUMP}px cap — the gap stays a gap, not a screen`,
    over.length === 0,
    over.map((h) => [h.key.slice(0, 40), Math.round(h.pad)]),
  );
}

// --- §3 the bump IS the gap to the page --------------------------------

// The gap the feature measured for a heading, readable back out of the
// settled layout: padding pushes a heading's TEXT down inside its box and
// leaves the box where flow put it, so boxTop is still the position
// alignHeadings measured from (its own `cy` plus the spacers above it, which
// is what `fy - cy - cum` subtracts). pageTop − boxTop is therefore the same
// number the feature clamped, taken from the same two documents.
const gapOf = (h) => h.pageTop - h.boxTop;
const matched = both.heads.filter((h) => h.pageTop !== null);

{
  // THE ALIGNMENT IS A MEASUREMENT, not a constant. Every matched heading's
  // padding is the gap to its page counterpart, floored at zero and capped —
  // one identity that a feature which bumped by a fixed amount, matched the
  // wrong headings, forgot the cumulative subtraction, or pulled a heading up
  // would each break in its own way.
  const wrong = matched.filter(
    (h) =>
      Math.abs(h.pad - Math.max(0, Math.min(gapOf(h), MAX_BUMP))) >
      MEASURE_SLACK,
  );
  check(
    'each heading is bumped by exactly the gap to its counterpart on the page, floored and capped',
    wrong.length === 0,
    wrong.map((h) => ({
      heading: h.key.slice(0, 40),
      gap: Math.round(gapOf(h)),
      pad: Math.round(h.pad),
      want: Math.round(Math.max(0, Math.min(gapOf(h), MAX_BUMP))),
    })),
  );
}

// --- §4 and the section reads level -------------------------------------

{
  // THE CLAIM THE PADDING IS FOR, in the reader's own terms: the first bumped
  // heading's TEXT is level with the same heading on the page. Where the cap
  // BOUND it cannot be — this fixture's hero puts the first heading some 570px
  // down the rendered page and the cap deliberately pays only 260 of that — so
  // there the claim is what the cap promises instead: the gap shrank by the
  // whole cap and what is left is exactly what the cap withheld. The branch is
  // on the measurement, not on the result, so neither arm can be reached by a
  // feature that did nothing.
  const target = matched.find((h) => h.pad > 0);
  const textTop = target ? target.boxTop + target.pad : 0;
  const left = target ? textTop - target.pageTop : 0;
  const owed = target ? Math.max(0, gapOf(target) - MAX_BUMP) : 0;
  const capped = target ? gapOf(target) > MAX_BUMP : false;
  check(
    capped
      ? 'the bumped heading closed its gap by the whole cap — level is what the cap withholds'
      : 'the bumped heading sits level with the same heading on the page',
    !!target && Math.abs(Math.abs(left) - owed) <= LEVEL_PX,
    target
      ? {
          heading: target.key.slice(0, 60),
          gap: Math.round(gapOf(target)),
          pad: Math.round(target.pad),
          textTop: Math.round(textTop),
          pageTop: Math.round(target.pageTop),
          stillApart: Math.round(left),
          capWithheld: Math.round(owed),
        }
      : 'nothing was bumped — there is no aligned section to measure',
  );
}

// --- §4b the grip sits at the heading's words, not on its spacer ---------

{
  // A HEADING'S GRIP IS LEVEL WITH ITS TEXT. The spacer is padding INSIDE the
  // heading's box, so the box's top is the top of the spacer and the words
  // are a padding's height below it. A grip measured off the box would sit
  // beside the empty space above the heading it names. The words are read
  // where the editor says they are: the coordinates of the heading's first
  // character.
  const seats = await page.evaluate(() => {
    const view = window.galleyEdit.editor.view;
    const doc = view.state.doc;
    const out = [];
    for (const g of document.querySelectorAll(
      '.gly-block-grip[data-kind="heading"]',
    )) {
      const index = Number(g.dataset.index);
      let pos = 0;
      for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
      const h = view.nodeDOM(pos);
      out.push({
        key: h.textContent.slice(0, 40),
        pad: parseFloat(getComputedStyle(h).paddingTop) || 0,
        grip: g.getBoundingClientRect().top,
        text: view.coordsAtPos(pos + 1).top,
      });
    }
    return out;
  });
  const bumped = seats.filter((h) => h.pad > LEVEL_PX);
  check(
    'a heading pushed down to meet the page has its grip at its words, not on the spacer above them',
    bumped.length > 0 && bumped.every((h) => Math.abs(h.grip - h.text) <= 2),
    bumped.map((h) => [
      h.key,
      Math.round(h.pad),
      Math.round(h.grip),
      Math.round(h.text),
    ]),
  );
}

// --- §5 the feature only pushes down ------------------------------------

{
  // boxTop is where the heading's box sits with the spacers above it already
  // applied, so `pageTop - boxTop` is the gap the feature measured for THIS
  // heading. Where that is negative the page renders the heading ABOVE the
  // editor's own position and there is nothing to add: padding must be 0,
  // because the only other answer is pulling the prose up, which would need
  // negative space and would undo the alignment above it.
  const above = both.heads.filter(
    (h) => h.pageTop !== null && h.pageTop < h.boxTop - CAP_SLACK,
  );
  const pushed = above.filter((h) => h.pad > 0);
  check(
    'a heading the page renders higher than the editor gets nothing — it only ever pushes down',
    pushed.length === 0,
    {
      higherOnPage: above.length,
      wronglyPushed: pushed.map((h) => [h.key.slice(0, 40), Math.round(h.pad)]),
    },
  );
}

// --- §6 it clears outside Both, and comes back --------------------------

await page.click('.gly-view-btn[data-view="content"]');
await page.waitForTimeout(VIEW_MS);
const content = await read();
check(
  'Content view leaves every heading at zero — a spacer with nothing beside it is a hole',
  content.view.includes('gly-view-content') &&
    content.heads.length > 0 &&
    content.heads.every((h) => h.pad === 0),
  content.heads
    .filter((h) => h.pad !== 0)
    .map((h) => [h.key.slice(0, 40), h.pad]),
);

await page.click('.gly-view-btn[data-view="both"]');
await page.waitForTimeout(SETTLE_MS);
const again = await read();
check(
  'and Both re-measures — the spacers come back rather than staying cleared',
  again.view.includes('gly-view-both') && again.heads.some((h) => h.pad > 0),
  again.heads.map((h) => [h.key.slice(0, 40), Math.round(h.pad)]),
);

await browser.close();
server.kill('SIGTERM');
await new Promise((r) => setTimeout(r, 500));

console.log(
  failures === 0 ? '\nall align checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
