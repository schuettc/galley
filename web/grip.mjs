// grip.mjs — ONE GRIP FOR EVERY BLOCK.
//
// Every top-level heading, fence, table, image, mermaid diagram, display-math
// block and front-matter block carries one grip in the left gutter, visible
// without hovering, and pressing it opens the block composer on that block.
// This gate holds every claim about the grip that crosses kinds. The claims a
// single kind owns stay with that kind's gate (codeblock.mjs for the fence's
// deny line, rounds-ux.mjs for the section's round).
//
// The rules that need no browser — which blocks get a grip, its face and
// label, its count, how two crowded grips stack — are web/grips.ts's, checked
// in probe.mjs. What only a browser can say is whether a reviewer can SEE and
// PRESS the grip, and what pressing it moves. That is this file.
//
// THE FIXTURE carries the shapes a wrong implementation gets wrong:
//
//   - every kind that takes a grip, and every kind that must not;
//   - an h2 directly followed by a table, two blocks closer than one grip,
//     which is the case that proves the grips stack;
//   - a table that is not the last block;
//   - a fence nested in a list item, which is not a top-level block and gets
//     no grip;
//   - a figure (an SVG served beside the document, as layers.mjs does) low
//     enough on the page that a composer placed wrongly is visible;
//   - closing paragraphs, enough that the last blocks can be scrolled near
//     the top of the window with room for the form beneath them;
//   - a last heading too long for a composer's head to quote whole.
//
// The file's name is long on purpose: a short fixture name leaves the bar's
// title room it never has for a real document, and the bar does not fold.
//
// Running it:
//
//   just grip
//
// which is `just build` and then `GALLEY="$PWD/bin/galley" node web/grip.mjs`.
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. It is in `just gates`, and a
// step of its own in ci.yml.

import { spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright-core';
// The kinds that take a grip, read from the app's own list rather than copied:
// a copy here would go on certifying the old set the day a kind is added.
import { GRIP_KINDS } from './grips.ts';
import { TABLE_HINT, TABLE_INSIDE } from './suggestions.ts';

const GALLEY = process.env.GALLEY || 'bin/galley';
// 8272, and 8273 is held for a second server: a claim about the file is a
// claim about REOPENING it, and a second server on the same port a second
// later talks to a socket the kernel has not finished releasing
// (codeblock.mjs's reason for its two).
const PORT = Number(process.env.PORT || 8272);

const FIXTURE = `---
title: The block grip
---

# The block grip

Every block a reviewer may want to speak to as a whole carries one grip.

## Budget
| key | value |
| --- | --- |
| retries | 3 |
| timeout | 30s |

- install it first
  \`\`\`bash
  npm install -g galley
  \`\`\`

> A quotation gets no grip; a selection is the way to point at it.

\`\`\`bash
galley edit README.md
\`\`\`

Some prose between the fence and the figure, so the figure sits low enough
on the page that a composer placed against the wrong box is visible.

![a figure with a caption](fig.svg)

\`\`\`mermaid
graph TD
  a --> b
\`\`\`

$$
E = mc^2
$$

---

That is the whole of it, and nothing else is required.

Closing note 1: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

Closing note 2: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

Closing note 3: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

Closing note 4: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

Closing note 5: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

Closing note 6: prose after the last block that takes a grip, so the page
scrolls far enough to put the equation near the top of the window, where
the form's grown height fits beneath it.

### A heading long enough that the composer's head has to cut it short
`;

// The figure: an SVG rather than a raster, because it is three lines of text
// with an intrinsic size, so its box is a real box.
const FIGURE = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="80">
  <rect width="200" height="80" fill="#c9cee0"/>
</svg>
`;

// FAILURES ARE COLLECTED BY NAME, so the summary line can say which promise
// broke without scrolling back through the ok lines.
const failed = [];
function check(name, ok, detail) {
  if (!ok) failed.push(name);
  const why = ok || detail === undefined ? '' : ` — ${JSON.stringify(detail)}`;
  console.log(`${ok ? 'ok  ' : 'FAIL'}  ${name}${why}`);
}

const dir = mkdtempSync(join(tmpdir(), 'galley-grip-'));
const doc = join(dir, 'block-grip-review-fixture.md');
writeFileSync(doc, FIXTURE);
writeFileSync(join(dir, 'fig.svg'), FIGURE);

// EVERY PROCESS THIS GATE STARTS IS STOPPED ON EXIT, however the run ends, so
// a failed run never leaves a `galley edit` holding the port for the next one
// to measure (undo.mjs records that afternoon).
const children = new Set();
process.on('exit', () => {
  for (const kill of children) {
    try {
      kill();
    } catch {
      // Already gone.
    }
  }
  // The server is still writing its sidecar as it stops, so the removal can
  // race it. A tmpdir left behind is nothing; a throwing exit handler is not.
  try {
    rmSync(dir, { recursive: true, force: true, maxRetries: 3 });
  } catch {
    // Left for the OS.
  }
});

// serve starts `galley edit` on the fixture and resolves once `/` answers,
// which is the server being up rather than a clock having run. The server's
// last words are kept and printed only if it dies on its own. `stopped` is
// the server's exit, so a claim about the FILE can wait for the last save.
let stopped = null;
async function serve(port) {
  const proc = spawn(
    GALLEY,
    ['edit', doc, '--no-open', '--port', String(port), '--on-revise', 'true'],
    { stdio: ['ignore', 'ignore', 'pipe'] },
  );
  children.add(() => proc.kill('SIGTERM'));
  stopped = () =>
    new Promise((done) => {
      proc.once('exit', done);
      proc.kill('SIGTERM');
    });
  let tail = '';
  proc.stderr.on('data', (b) => (tail = (tail + b).slice(-4000)));
  proc.on('exit', (code, signal) => {
    if (code && signal !== 'SIGTERM') console.log(`      [${port}] ${tail}`);
  });
  const base = `http://127.0.0.1:${port}`;
  for (let wait = 0; wait < 100; wait += 1) {
    const ok = await fetch(`${base}/`).then(
      (r) => r.ok,
      () => false,
    );
    if (ok) return base;
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`galley edit on ${port} never answered`);
}

const base = await serve(PORT);
const pending = async () => (await fetch(`${base}/_galley/pending`)).json();

const executablePath = process.env.GALLEY_CHROME || undefined;
const browser = await chromium.launch({ executablePath });
children.add(() => browser.process()?.kill('SIGKILL'));
// Wide enough that the rail is beside the document; the narrow widths are
// their own section. The wait is on the TABLE rather than on `.ProseMirror`:
// an editor that is up with its blocks not yet painted is not a page any grip
// claim can read.
const page = await browser.newPage({
  viewport: { width: 1440, height: 1000 },
});
page.on('pageerror', (e) => console.log(`      [page] ${e.message}`));
await page.goto(base, { waitUntil: 'networkidle' });
await page.locator('.ProseMirror table').first().waitFor({ timeout: 15000 });
await page.waitForTimeout(1200);
// blockDom is the element ProseMirror drew for the top-level block at
// `index`, and blockBox its box, read the way the grip reads it. Put on the
// page once, so every section asks it the same way.
await page.evaluate(() => {
  window.blockDom = (index) => {
    const view = window.galleyEdit.editor.view;
    const doc = view.state.doc;
    let pos = 0;
    for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
    return view.nodeDOM(pos);
  };
  window.blockBox = (index) => window.blockDom(index).getBoundingClientRect();
  // scopeOver reads every scope outline shown on the page against the
  // top-level blocks `from` to `to`: how many outlines there are, whether any
  // is inside the document (where it would be content), and how far the one
  // outline's top and bottom stand outside the first block's top and the last
  // block's bottom. One outline spanning the blocks is the claim.
  window.scopeOver = (from, to) => {
    const shown = [...document.querySelectorAll('.gly-grip-scope')].filter(
      (e) => e.checkVisibility() && e.getBoundingClientRect().height > 0,
    );
    const r = shown.length === 1 ? shown[0].getBoundingClientRect() : null;
    return {
      n: shown.length,
      inside: shown.some((e) => !!e.closest('.ProseMirror')),
      top: r ? +(window.blockBox(from).top - r.top).toFixed(1) : null,
      bottom: r ? +(r.bottom - window.blockBox(to).bottom).toFixed(1) : null,
    };
  };
  // spans is that claim: exactly one outline, outside the document, whose
  // edges stand at most 8px outside the first block's top and the last
  // block's bottom and never inside them.
  window.spans = (s) =>
    s.n === 1 &&
    !s.inside &&
    s.top >= 0 &&
    s.top <= 8 &&
    s.bottom >= 0 &&
    s.bottom <= 8;
  // gripSeats reads every shown grip where a reviewer would press it: each is
  // scrolled to the middle of the window first, so "pressable" is not asked
  // under the bar. Positions are in PAGE coordinates, so grips read at
  // different scrolls can be compared with each other.
  window.gripSeats = async () => {
    const view = window.galleyEdit.editor.view;
    const doc = view.state.doc;
    const out = [];
    for (const g of document.querySelectorAll(
      '.gly-block-grip:not([hidden])',
    )) {
      const index = Number(g.dataset.index);
      const node = doc.child(index);
      g.scrollIntoView({ block: 'center' });
      await new Promise((r) => requestAnimationFrame(() => r()));
      const r = g.getBoundingClientRect();
      const b = window.blockBox(index);
      // A heading's line is its TEXT, not its box: in page mode the box
      // carries the alignment padding above the words.
      let pos = 0;
      for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
      const line =
        node.type.name === 'heading' ? view.coordsAtPos(pos + 1).top : b.top;
      const at = document.elementFromPoint(
        r.left + r.width / 2,
        r.top + r.height / 2,
      );
      const y = window.scrollY;
      out.push({
        kind: g.dataset.kind,
        index,
        w: r.width,
        h: r.height,
        left: r.left,
        right: r.right,
        blockLeft: b.left,
        top: r.top + y,
        bottom: r.bottom + y,
        line: line + y,
        hit: !!at && g.contains(at),
      });
    }
    window.scrollTo(0, 0);
    return out;
  };
});

{
  const kinds = ((await pending()).blocks || []).map((b) => b.kind);
  const painted = await page.evaluate(() => ({
    headings: document.querySelectorAll('.ProseMirror h1, .ProseMirror h2')
      .length,
    tables: document.querySelectorAll('.ProseMirror table').length,
    figures: document.querySelectorAll('.ProseMirror .gly-figure').length,
  }));
  check(
    'the fixture opened',
    painted.headings === 2 &&
      painted.tables === 1 &&
      painted.figures >= 2 &&
      [
        'frontMatter',
        'heading',
        'table',
        'codeBlock',
        'image',
        'mathBlock',
      ].every((k) => kinds.includes(k)),
    { painted, kinds },
  );
}

// The eligible blocks, as the SERVER lists them: the top-level blocks of a
// kind that takes a grip. The server lists top-level blocks only, so the fence
// nested in the list item is not among them, and a grip on it fails §1.
const eligible = ((await pending()).blocks || []).filter((b) =>
  GRIP_KINDS.includes(b.kind),
);
const heading2 = eligible.find(
  (b) => b.kind === 'heading' && b.label.includes('Budget'),
);

// --- §1 every grip is visible without hovering ---------------------------
//
// The pointer is parked in the corner and never moved: a grip that needs a
// hover to appear is a grip nobody finds.
await page.mouse.move(0, 0);
{
  const rest = await page.evaluate(() =>
    [...document.querySelectorAll('.gly-block-grip:not([hidden])')]
      .filter((g) => g.getBoundingClientRect().width > 0)
      .map((g) => ({ index: Number(g.dataset.index), kind: g.dataset.kind })),
  );
  const named = eligible.map((b) => ({ index: b.index, kind: b.kind }));
  check(
    'every block that takes a grip shows one at rest, with no pointer on the page',
    rest.length === eligible.length &&
      named.every((b) =>
        rest.some((g) => g.index === b.index && g.kind === b.kind),
      ),
    { rest, named },
  );
  check(
    'and nothing else has one: no paragraph, list, quotation or rule, and not the fence inside the list item',
    rest.length > 0 &&
      rest.every(
        (g) =>
          !['paragraph', 'bulletList', 'blockquote', 'horizontalRule'].includes(
            g.kind,
          ) && named.some((b) => b.index === g.index && b.kind === g.kind),
      ),
    rest,
  );
  // FAINT AT REST, FULL UNDER THE POINTER. A gutter of full-strength buttons
  // beside every block reads louder than the prose it is beside; the grip is
  // drawn at full strength only when it is being reached for or has
  // something to say (its count, §14; keyboard focus, §17).
  const strength = await page.evaluate(() =>
    [...document.querySelectorAll('.gly-block-grip:not([hidden])')].map((g) =>
      Number(getComputedStyle(g).opacity),
    ),
  );
  const first = page.locator('.gly-block-grip:not([hidden])').first();
  await first.hover();
  const hovered = await first.evaluate((g) =>
    Number(getComputedStyle(g).opacity),
  );
  await page.mouse.move(0, 0);
  check(
    'at rest every grip is faint, and the one under the pointer is at full strength',
    strength.length > 0 && strength.every((o) => o <= 0.6) && hovered === 1,
    { strength, hovered },
  );
}

// --- §2 a target in the gutter, level with its block ---------------------
//
// Each grip is read where a reviewer would press it (gripSeats).
//
// THE PAIR IS MADE CLOSER THAN ONE GRIP. At this stylesheet's margins no two
// blocks that take a grip sit closer than about 48px (measured at 1440: a
// heading over a table, 61; an h4 over a table, 48), so the heading directly
// over the table does not, by itself, crowd its grips, and "no two grips
// overlap" would hold with no stacking at all. Closing the margins between
// the two, and the heading's own line to less than one grip (with its
// margins gone it is still 31px tall, more than a 24px grip), makes them a
// pair the stacker has to separate, and the document changing size under
// the grips is what the repaint has to notice.
const closer = await page.addStyleTag({
  content: `.ProseMirror > h2 { margin-bottom: 0 !important; line-height: 16px !important; }
.ProseMirror > h2 + *, .ProseMirror > h2 + * table { margin-top: 0 !important; }`,
});
await page.waitForTimeout(300);
{
  const seats = await page.evaluate(() => window.gripSeats());
  const near = (a, b) => Math.abs(a - b) <= 2;
  check(
    'every grip is 24px square',
    seats.length > 0 &&
      seats.every(
        (s) => Math.abs(s.w - 24) <= 0.5 && Math.abs(s.h - 24) <= 0.5,
      ),
    seats.map((s) => [s.kind, s.w, s.h]),
  );
  check(
    'every grip sits in the left gutter: clear of its block and inside the window',
    seats.length > 0 &&
      seats.every((s) => s.right <= s.blockLeft && s.left >= 0),
    seats.map((s) => [s.kind, s.left, s.right, s.blockLeft]),
  );
  // Out in the margin, not against the text: a grip touching its block reads
  // as part of it.
  check(
    'every grip stands at least 12px clear of its block',
    seats.length > 0 && seats.every((s) => s.blockLeft - s.right >= 12),
    seats.map((s) => [s.kind, Math.round((s.blockLeft - s.right) * 10) / 10]),
  );
  // Level with its block's first line, or — where the block above it is
  // closer than one grip — pushed down just clear of the grip above.
  check(
    'every grip is level with its block, or just below the grip above it',
    seats.length > 0 &&
      seats.every(
        (s, i) =>
          near(s.top, s.line) ||
          (i > 0 && s.top > s.line && s.top - seats[i - 1].bottom <= 8),
      ),
    seats.map((s) => [s.kind, Math.round(s.top), Math.round(s.line)]),
  );
  check(
    'every grip can be pressed: the point at its centre is the grip',
    seats.length > 0 && seats.every((s) => s.hit),
    seats.filter((s) => !s.hit).map((s) => s.kind),
  );
  const table = seats.findIndex((s) => s.kind === 'table');
  check(
    'no two grips overlap, the heading closer than one grip to the table below it included',
    table > 0 &&
      seats[table].line - seats[table - 1].line < seats[table - 1].h &&
      seats.every((s, i) => i === 0 || s.top >= seats[i - 1].bottom),
    seats.map((s) => [
      s.kind,
      Math.round(s.line),
      Math.round(s.top),
      Math.round(s.bottom),
    ]),
  );
}
await closer.evaluate((el) => el.remove());
await page.waitForTimeout(300);

// --- §3 the grips displace nothing ---------------------------------------
//
// Every top-level block's box, with the grip layer and with it taken out of
// the page. A layer in the document's flow, or inside `.ProseMirror` where it
// would be content, moves the blocks.
{
  const boxes = () =>
    page.evaluate(() => ({
      layer: (() => {
        const el = document.querySelector('.gly-grips');
        return el
          ? el.parentElement.id === 'editor' &&
              !el.closest('.ProseMirror') &&
              el.querySelectorAll('.gly-block-grip').length > 0
          : false;
      })(),
      blocks: [...document.querySelector('.ProseMirror').children].map((e) => {
        const r = e.getBoundingClientRect();
        return [r.left, r.top, r.width, r.height]
          .map((v) => Math.round(v * 10) / 10)
          .join(',');
      }),
    }));
  const present = await boxes();
  const off = await page.addStyleTag({
    content: '.gly-grips { display: none !important; }',
  });
  const removed = await boxes();
  await off.evaluate((el) => el.remove());
  check(
    'the grips are one layer beside the document, and taking it away moves no block',
    present.layer && present.blocks.join('|') === removed.blocks.join('|'),
    { layer: present.layer, present: present.blocks, removed: removed.blocks },
  );
}

// --- §4 a click moves nothing but what was clicked ------------------------
//
// The caret is put in the first paragraph first, so "the selection is
// unchanged" is a claim about a real selection: a grip that selected its
// section to show the scope would move it.
const H2_GRIP = `.gly-block-grip[data-index="${heading2 ? heading2.index : -1}"]`;
await page.evaluate(() => {
  const editor = window.galleyEdit.editor;
  let at = 0;
  editor.state.doc.forEach((node, pos) => {
    if (!at && node.textContent.startsWith('Every block')) at = pos + 6;
  });
  editor.commands.setTextSelection(at);
});
await page.waitForTimeout(200);
const snap = () =>
  page.evaluate(() => {
    const box = (e) => {
      const b = e.getBoundingClientRect();
      return [b.left, b.top, b.width, b.height]
        .map((v) => Math.round(v * 10) / 10)
        .join(',');
    };
    const all = (sel) => [...document.querySelectorAll(sel)].map(box);
    const s = window.galleyEdit.editor.state.selection;
    return {
      blocks: all('.ProseMirror > *'),
      grips: all('.gly-block-grip'),
      cards: all('.gly-rail .gly-card'),
      bar: all('.gly-bar button'),
      shown: [...document.body.children]
        .filter((e) => {
          const cs = getComputedStyle(e);
          return (
            !e.hidden &&
            cs.display !== 'none' &&
            cs.visibility !== 'hidden' &&
            e.getBoundingClientRect().height > 0
          );
        })
        .map((e) => e.className || e.tagName),
      sel: [s.from, s.to],
    };
  });
let opened = false;
{
  const grip = page.locator(H2_GRIP);
  const box = (await grip.count()) ? await grip.boundingBox() : null;
  const before = await snap();
  if (box) {
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.click(x, y);
    opened = await page
      .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
      .then(
        () => true,
        () => false,
      );
    const after = await snap();
    const under = await page.evaluate(
      ([px, py, sel]) => {
        const at = document.elementFromPoint(px, py);
        const g = document.querySelector(sel);
        return !!at && !!g && g.contains(at);
      },
      [x, y, H2_GRIP],
    );
    const fresh = after.shown.filter((c) => !before.shown.includes(c));
    check(
      'clicking the heading\u2019s grip moves no block, grip, card or bar control',
      ['blocks', 'grips', 'cards', 'bar'].every(
        (k) => before[k].join('|') === after[k].join('|'),
      ),
      ['blocks', 'grips', 'cards', 'bar'].filter(
        (k) => before[k].join('|') !== after[k].join('|'),
      ),
    );
    check(
      'the grip is still under the pointer, and the composer is the only new box',
      under && fresh.length === 1 && fresh[0] === 'gly-composer',
      { under, fresh },
    );
    check(
      'and the selection is the reviewer\u2019s, unchanged',
      before.sel.join(',') === after.sel.join(','),
      { before: before.sel, after: after.sel },
    );
  } else {
    check('the heading has a grip to click', false, H2_GRIP);
  }
  const focus = await page.evaluate(() => {
    const form = document.querySelector('.gly-composer-form');
    return {
      form: !!form && !form.hidden,
      typing: !!document.activeElement?.classList.contains('gly-composer-text'),
    };
  });
  check(
    'the composer opens straight to its form, with the caret in it',
    opened && focus.form && focus.typing,
    focus,
  );
}

// --- §5 a section instruction --------------------------------------------
let filedKey = '';
{
  const scope = await page.evaluate(
    (index) => {
      const doc = window.galleyEdit.editor.state.doc;
      if (index < 0) return null;
      const level = doc.child(index).attrs.level;
      let want = 1;
      for (let i = index + 1; i < doc.childCount; i += 1) {
        const c = doc.child(i);
        if (c.type.name === 'heading' && c.attrs.level <= level) break;
        want += 1;
      }
      const over = window.scopeOver(index, index + want - 1);
      return { want, over, ok: window.spans(over) };
    },
    heading2 ? heading2.index : -1,
  );
  // ONE OUTLINE AROUND THE SECTION. A box drawn round each block under the
  // heading reads as that many separate things, which is not what the
  // instruction is about.
  check(
    'the scope is ONE outline round the whole section, from the heading to the section\u2019s last block',
    !!scope && scope.want > 1 && scope.ok,
    scope,
  );

  if (opened) {
    await page.keyboard.type('tighten this section');
    await page.keyboard.press('Enter');
  }
  let list = [];
  for (let wait = 0; wait < 40 && !list.length; wait += 1) {
    list = (await pending()).instructions || [];
    if (!list.length) await page.waitForTimeout(250);
  }
  const one = list.length === 1 ? list[0] : null;
  filedKey = one ? one.key : '';
  check(
    'Enter files one BLOCK instruction on the heading\u2019s own key',
    !!one &&
      !!heading2 &&
      one.text === 'tighten this section' &&
      one.anchor === 'block' &&
      one.blockKind === 'heading' &&
      one.anchorKey === heading2.key,
    one || list,
  );
  const beside = await page
    .waitForSelector(`.gly-rail-band .gly-thread[data-key="${filedKey}"]`, {
      timeout: filedKey ? 10000 : 1,
    })
    .then(
      () =>
        page.evaluate(
          ([key, index]) => {
            const card = document.querySelector(
              `.gly-rail-band .gly-thread[data-key="${key}"]`,
            );
            return Math.round(
              card.getBoundingClientRect().top - window.blockBox(index).top,
            );
          },
          [filedKey, heading2.index],
        ),
      () => null,
    );
  check(
    'its card sits beside the heading',
    beside !== null && Math.abs(beside) <= 2,
    beside,
  );

  // THE KEYSTROKE AFTER. A grip that selected its section left that
  // selection standing, and the next keystroke replaced the section.
  await page.locator('.ProseMirror p', { hasText: 'nothing else' }).click();
  // The caret goes to the paragraph's end by position: End is the end of a
  // visual line, and where the click landed on it is the window's business.
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    let at = 0;
    editor.state.doc.forEach((node, pos) => {
      if (node.textContent.endsWith('is required.'))
        at = pos + node.nodeSize - 1;
    });
    editor.commands.setTextSelection(at);
  });
  await page.keyboard.type('x');
  let disk = '';
  for (let wait = 0; wait < 40; wait += 1) {
    disk = readFileSync(doc, 'utf8');
    if (disk.includes('required.x')) break;
    await page.waitForTimeout(250);
  }
  check(
    'typing in the prose afterwards leaves the heading as it was',
    disk.includes('required.x') && disk.includes('\n## Budget\n'),
    disk,
  );
}

// --- §9 a refusal inside a table points at its grip ----------------------
//
// A selection inside the table is refused, and the muted line under the
// refusal names the grip beside it: the one way to speak to the table. Read
// off the page, so the line the reviewer sees is the line checked.
{
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    let at = null;
    editor.state.doc.descendants((node, pos) => {
      if (at !== null) return false;
      if (node.isText && node.text.includes('retries')) at = pos;
      return at === null;
    });
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + 'retries'.length });
    editor.view.focus();
  });
  await page.waitForTimeout(400);
  const said = await page.evaluate(() => {
    const shown = (el) => !!el && !el.hidden && el.offsetParent !== null;
    const deny = document.querySelector('.gly-composer-deny');
    const hint = document.querySelector('.gly-composer-deny-hint');
    return {
      deny: shown(deny) ? deny.textContent : null,
      hint: shown(hint) ? hint.textContent : null,
    };
  });
  check(
    'a selection inside the table is refused in the table\u2019s own sentence',
    said.deny === TABLE_INSIDE,
    said,
  );
  check(
    'and the line under it says the grip beside the table takes an instruction on the whole table',
    !!said.hint &&
      said.hint.startsWith(TABLE_HINT) &&
      said.hint.endsWith(
        '\u2014 or press the button to its left to leave an instruction on the whole table',
      ),
    said,
  );
  await page.keyboard.press('Escape');
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    editor.commands.setTextSelection(editor.state.selection.from);
    editor.commands.blur();
  });
  await page.waitForTimeout(200);
}

// fileOn presses the grip of the one block of `kind`, reads where the
// composer opened against the block, types `text` and sends it, and returns
// what was read and the instruction the server filed for those words.
//
// The page is given the server's block list first. Every filing inserts a note
// and renumbers the blocks after it, and a grip pressed before the page has
// heard says, rightly, that its block is not in the document yet. The block
// is scrolled near the top of the window, where a reviewer reads it, so the
// form's grown height fits beneath it.
async function fileOn(kind, text) {
  const ref = ((await pending()).blocks || []).find((b) => b.kind === kind);
  const { opened, seat } = await openGrip(ref);
  if (opened) await page.keyboard.type(text);
  const one = opened ? await sendFor(text) : null;
  return { ref, opened, seat, one };
}

// openGrip presses the grip of the server's block `ref` and reads where the
// composer opened against it. See fileOn.
async function openGrip(ref) {
  const grip = page.locator(
    `.gly-block-grip[data-index="${ref ? ref.index : -1}"]`,
  );
  if (!ref || (await grip.count()) !== 1) {
    return { opened: false, seat: null };
  }
  await page
    .waitForFunction(
      ([key, index]) =>
        (window.galleyEdit.app.blocks || []).some(
          (b) => b.key === key && b.index === index,
        ),
      [ref.key, ref.index],
      { timeout: 10000 },
    )
    .catch(() => {});
  await page.evaluate(
    (index) => window.scrollBy(0, window.blockBox(index).top - 150),
    ref.index,
  );
  await page.waitForTimeout(150);
  await grip.click();
  const opened = await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .then(
      () => true,
      () => false,
    );
  // A figure the sync has just redrawn is a box with no picture in it for a
  // moment; its box is read once every picture on the page has its height.
  await page
    .waitForFunction(
      () =>
        [...document.querySelectorAll('.ProseMirror img')].every(
          (i) => i.complete && i.getBoundingClientRect().height > 0,
        ),
      null,
      { timeout: 3000 },
    )
    .catch(() => {});
  const seat = await page.evaluate((index) => {
    const b = window.blockBox(index);
    const c = document.querySelector('.gly-composer').getBoundingClientRect();
    const deny = document.querySelector('.gly-composer-deny');
    const mark = document.querySelector('.gly-composer-region');
    return {
      height: Math.round(b.height),
      below: Math.round((c.top - b.bottom) * 10) / 10,
      covers: c.top < b.bottom && c.bottom > b.top,
      deny: !!deny && !deny.hidden,
      head: document.querySelector('.gly-composer-head').textContent,
      mark: !!mark && mark.checkVisibility() ? mark.textContent : null,
      scope: window.spans(window.scopeOver(index, index)),
    };
  }, ref.index);
  return { opened, seat };
}

// sendFor presses Enter in the open composer and returns the instruction the
// server filed for `text`, once it is listed. The first listing is the whole
// one: a block instruction is written with its anchor in one transaction.
async function sendFor(text) {
  await page.keyboard.press('Enter');
  let one = null;
  for (let wait = 0; wait < 40 && !one; wait += 1) {
    one =
      ((await pending()).instructions || []).find((i) => i.text === text) ||
      null;
    if (!one) await page.waitForTimeout(250);
  }
  return one;
}

// cardBeside reads the rail card for `key` against the block at `index`: how
// far apart their tops are, and what the card's head says.
const cardBeside = (key, index) =>
  page
    .waitForSelector(`.gly-rail-band .gly-thread[data-key="${key}"]`, {
      timeout: key ? 10000 : 1,
    })
    .then(
      () =>
        page.evaluate(
          ([k, i]) => {
            const card = document.querySelector(
              `.gly-rail-band .gly-thread[data-key="${k}"]`,
            );
            const b = window.blockBox(i);
            const top = card.getBoundingClientRect().top;
            // The card above it in the rail, if any: a card pushed down is
            // pushed down by that one.
            const above = [
              ...document.querySelectorAll('.gly-rail-band .gly-card'),
            ]
              .map((c) => c.getBoundingClientRect())
              .filter((r) => r.top < top)
              .reduce((m, r) => Math.max(m, r.bottom), -Infinity);
            return {
              offset: Math.round(top - b.top),
              clear: Math.round(top - above),
              head: card.querySelector('.gly-card-head').textContent,
            };
          },
          [key, index],
        ),
      () => null,
    );

// --- §7a a block the page has not heard of files nothing ----------------
//
// A grip pressed before the page has the server's key for its block says so,
// and the instruction cannot be sent, by the button or by Enter: there is no
// key to file it on, and filing it as anything else puts it somewhere the
// reviewer did not point.
{
  const before = ((await pending()).instructions || []).length;
  const saved = await page.evaluate(() => {
    const app = window.galleyEdit.app;
    const kept = app.blocks;
    app.blocks = [];
    return kept.length;
  });
  await page.locator('.gly-block-grip[data-kind="table"]').click();
  await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  const said = await page.evaluate(() => ({
    note: document.querySelector('.gly-composer-note').textContent,
    send: document.querySelector('.gly-composer-send').disabled,
  }));
  // Every write the page attempts, not only the ones the server accepted: an
  // instruction with no key, sent as anything else, is a write to somewhere
  // the reviewer did not point, whether or not the server finds it.
  const posts = [];
  const watch = (r) => {
    if (r.method() === 'POST' && r.url().includes('/_galley/instruct')) {
      posts.push(r.postData());
    }
  };
  page.on('request', watch);
  await page.keyboard.type('nowhere to go');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(1500);
  page.off('request', watch);
  const after = ((await pending()).instructions || []).length;
  check(
    'a block the page has no key for says it is not in the document yet, and Enter sends nothing',
    saved > 0 &&
      said.note.startsWith('not in the document yet') &&
      said.send &&
      posts.length === 0 &&
      after === before,
    { said, posts, before, after },
  );

  // --- §7b and the box picks the block up when the page hears of it ------
  //
  // The note says the block lands on the next sync, so the next sync has to
  // land it: the refresh that brings the key makes the open box sendable on
  // that block, clears the note, and keeps every word already typed. It is
  // read before Esc and nothing is sent, so the instruction count the later
  // sections read is untouched.
  const tableKey = ((await pending()).blocks || []).find(
    (b) => b.kind === 'table',
  )?.key;
  await page.evaluate(() => window.galleyEdit.app.refreshPending());
  await page.waitForTimeout(300);
  const landed = await page.evaluate(() => ({
    open: !document.querySelector('.gly-composer-form').hidden,
    note: document.querySelector('.gly-composer-note').textContent,
    send: document.querySelector('.gly-composer-send').disabled,
    words: document.querySelector('.gly-composer-text').value,
    key: window.galleyEdit.app.composer.block?.key || null,
  }));
  check(
    'when the refresh brings its key, the open box files on that block, the note goes, and the words stay',
    !!tableKey &&
      landed.open &&
      landed.note === '' &&
      !landed.send &&
      landed.words === 'nowhere to go' &&
      landed.key === tableKey,
    { landed, tableKey },
  );
  await page.keyboard.press('Escape');
  await page.evaluate(() => window.galleyEdit.app.refreshPending());
}

// gripsNow reads every grip as the reviewer meets it: its face, its name,
// whether it is painted as carrying instructions, whether it is on the page
// at all, and its box relative to `#editor`, so a scroll between two reads is
// not a move. `centre` is in page coordinates, for asking later what is
// under the place a grip stood.
const gripsNow = (on = page) =>
  on.evaluate(() => {
    const host = document.getElementById('editor').getBoundingClientRect();
    return [...document.querySelectorAll('.gly-block-grip')].map((g) => {
      const r = g.getBoundingClientRect();
      return {
        index: Number(g.dataset.index),
        kind: g.dataset.kind,
        face: g.textContent,
        label: g.getAttribute('aria-label'),
        title: g.title,
        commented: g.classList.contains('is-commented'),
        opacity: Number(getComputedStyle(g).opacity),
        shown: g.checkVisibility(),
        off: g.hidden && g.disabled,
        centre: [r.left + r.width / 2, r.top + r.height / 2 + window.scrollY],
        box: [r.left - host.left, r.top - host.top, r.width, r.height]
          .map((n) => Math.round(n * 10) / 10)
          .join(),
      };
    });
  });
const gripOf = (grips, kind) => grips.find((g) => g.kind === kind) || null;

// gripSays waits until the grip of the block at `index` shows `face`: the
// face follows the pending refresh after a send or a delete, not the press.
const gripSays = (index, face) =>
  page
    .waitForFunction(
      ([i, f]) =>
        document.querySelector(`.gly-block-grip[data-index="${i}"]`)
          ?.textContent === f,
      [index, face],
      { timeout: 10000 },
    )
    .catch(() => {});

// The table's grip before anything is filed on it, for §14.
const unfiled = await gripsNow();

// --- §7 a table instruction, end to end ----------------------------------
const filed = filedKey ? [filedKey] : [];
let tableKey = '';
let tableFiled = '';
{
  const { ref, opened, seat, one } = await fileOn(
    'table',
    'add a units column',
  );
  tableKey = ref ? ref.key : '';
  check(
    'the table\u2019s grip opens the form, with no refusal, just beneath the table and clear of it',
    opened &&
      !!seat &&
      !seat.deny &&
      seat.below >= 0 &&
      seat.below <= 8 &&
      !seat.covers,
    seat,
  );
  check(
    'the composer says it is on this table, and outlines the table alone',
    !!seat &&
      !!ref &&
      seat.head === 'INSTRUCTION \u00b7 ON this table' &&
      seat.scope,
    seat,
  );
  check(
    'sending files one BLOCK instruction on the table\u2019s key, quoting its header',
    !!one &&
      !!ref &&
      one.anchor === 'block' &&
      one.blockKind === 'table' &&
      one.anchorKey === ref.key &&
      one.quote === 'table: key, value',
    one,
  );
  tableFiled = one ? one.key : '';
  if (tableFiled) filed.push(tableFiled);
  const card = await cardBeside(tableFiled, ref ? ref.index : -1);
  // Level with the table, or, where the section's card above runs past the
  // table's top, stacked just under that card.
  check(
    'its card sits beside the table and says it is on the table by its header',
    !!card &&
      (Math.abs(card.offset) <= 2 ||
        (card.offset > 0 && card.clear >= 0 && card.clear <= 16)) &&
      card.head.includes('on table: key, value'),
    card,
  );
  const note = await page
    .waitForSelector(
      `.ProseMirror .gly-note[data-comment-id="${tableFiled || 'none'}"]`,
      { timeout: tableFiled ? 10000 : 1 },
    )
    .then(
      () =>
        page.evaluate((k) => {
          const aside = document.querySelector(
            `.ProseMirror .gly-note[data-comment-id="${k}"]`,
          );
          const table = document.querySelector('.ProseMirror table');
          return {
            tag: aside.tagName,
            words: aside.textContent.includes('add a units column'),
            below:
              aside.getBoundingClientRect().top >=
              table.getBoundingClientRect().bottom,
          };
        }, tableFiled),
      () => null,
    );
  check(
    'the instruction\u2019s note is in the document, under the table',
    !!note && note.tag === 'ASIDE' && note.words && note.below,
    note,
  );
}

// --- §14 a block with instructions shows how many ------------------------
//
// The grip's face is `+` on a block with none and their count on a block with
// some, and its name says so. The face swaps inside ONE fixed box: a grip that
// widened with its count would move under the pointer that just filed it.
{
  const was = gripOf(unfiled, 'table');
  await gripSays(was ? was.index : -1, '1');
  const now = await gripsNow();
  const is = gripOf(now, 'table');
  check(
    'before filing, the table\u2019s grip reads + and its name counts nothing',
    !!was &&
      was.face === '+' &&
      !was.label.includes('already') &&
      !was.commented,
    was,
  );
  check(
    'after one instruction it reads 1, its name ends (1 already), and it is painted as commented, at full strength',
    !!is &&
      is.face === '1' &&
      is.label.endsWith('(1 already)') &&
      is.title === is.label &&
      is.commented &&
      is.opacity === 1,
    is,
  );
  // The note the instruction put under the table moves every block after it,
  // so the grips that must not have moved are the table's and those above it.
  const moved = unfiled
    .filter((g) => !!was && g.index <= was.index)
    .map((g) => ({
      g,
      now: now.find((n) => n.index === g.index && n.kind === g.kind),
    }))
    .filter(({ g, now: n }) => !n || n.box !== g.box);
  check(
    'its box is the same box, and no grip at or above the table moved',
    !!was && !!is && is.box === was.box && moved.length === 0,
    { was: was && was.box, is: is && is.box, moved },
  );
  // THE SECTION'S GRIP COUNTS THE SECTION'S OWN. The table sits under ## Budget
  // and carries an instruction of its own; the heading's grip counts §5's
  // section instruction and not the table's.
  const section = now.find((g) => g.index === heading2?.index);
  check(
    'the section\u2019s grip counts its own instruction, not the table\u2019s inside it',
    !!section && section.face === '1' && section.label.endsWith('(1 already)'),
    section,
  );

  // A second, then taken back from its card.
  const second = await fileOn('table', 'and a default column');
  await gripSays(was ? was.index : -1, '2');
  const two = gripOf(await gripsNow(), 'table');
  check(
    'a second instruction on the table makes it 2, in the same box',
    !!second.one && !!two && two.face === '2' && two.box === is?.box,
    { two, one: second.one },
  );
  const del = page.locator(
    `.gly-rail-band .gly-thread[data-key="${second.one?.key || 'none'}"] .gly-thread-delete`,
  );
  if (second.one) {
    // Two presses: the first arms it, the second deletes.
    await del.click();
    await del.click();
  }
  await gripSays(was ? was.index : -1, '1');
  const back = gripOf(await gripsNow(), 'table');
  check(
    'deleting it from its card takes the count back down',
    !!back && back.face === '1' && back.box === is?.box,
    back,
  );
}

// --- §8 display math and front matter ------------------------------------
for (const [kind, text, head] of [
  ['mathBlock', 'define c', 'INSTRUCTION \u00b7 ON this equation'],
  ['frontMatter', 'add a date', 'INSTRUCTION \u00b7 ON the front matter'],
]) {
  const { ref, opened, seat, one } = await fileOn(kind, text);
  check(
    `the ${kind} grip opens the form beneath its block, saying what it is on`,
    opened &&
      !!seat &&
      !seat.deny &&
      seat.below >= 0 &&
      seat.below <= 8 &&
      !seat.covers &&
      seat.head === head,
    seat,
  );
  check(
    `and files one BLOCK instruction on the ${kind}\u2019s key`,
    !!one &&
      !!ref &&
      one.anchor === 'block' &&
      one.blockKind === kind &&
      one.anchorKey === ref.key,
    one,
  );
  if (one) filed.push(one.key);
}

// --- §10 a figure, whole ---------------------------------------------------
//
// The image's grip opens the composer beneath the picture, on the whole
// picture, and offers the way to a part of it. The grip is the figure's one
// control: nothing is drawn inside the picture to reach it.
{
  const { ref, opened, seat, one } = await fileOn('image', 'wrong colours');
  check(
    'the figure’s grip opens the form beneath the figure, on the whole figure, offering Mark a region',
    opened &&
      !!seat &&
      !seat.deny &&
      seat.below >= 0 &&
      seat.below <= 8 &&
      !seat.covers &&
      seat.head === 'INSTRUCTION \u00b7 ON this figure' &&
      seat.mark === 'Mark a region',
    seat,
  );
  check(
    'and files one BLOCK instruction on the image, with no region',
    !!one &&
      !!ref &&
      one.anchor === 'block' &&
      one.blockKind === 'image' &&
      one.anchorKey === ref.key &&
      !one.region,
    one,
  );
  if (one) filed.push(one.key);
  const controls = await page.evaluate(
    (index) => {
      const el = window.blockDom(index);
      return {
        grips: document.querySelectorAll(
          `.gly-block-grip[data-index="${index}"]`,
        ).length,
        inside: el.querySelectorAll('button:not(.gly-region-pin)').length,
      };
    },
    ref ? ref.index : -1,
  );
  check(
    'the figure has one grip beside it and no control of its own inside it',
    controls.grips === 1 && controls.inside === 0,
    controls,
  );
}

// figureOf is the `.gly-figure` box of the top-level block at `index`, in
// viewport coordinates, and whether it is picking.
const figureOf = (index) =>
  page.evaluate((i) => {
    const el = window.blockDom(i);
    const r = el.getBoundingClientRect();
    const form = document.querySelector('.gly-composer-form');
    return {
      x: r.left,
      y: r.top,
      w: r.width,
      h: r.height,
      picking: el.classList.contains('gly-picking'),
      drafts: el.querySelectorAll('.gly-region-draft').length,
      form: !!form && form.checkVisibility(),
      words: document.querySelector('.gly-composer-text').value,
      head: document.querySelector('.gly-composer-head').textContent,
    };
  }, index);

// The diagram, as the server lists it: the top-level fence whose language is
// mermaid, found by the editor and matched to the server's block by index.
const diagram = await (async () => {
  const index = await page.evaluate(() => {
    let at = -1;
    window.galleyEdit.editor.state.doc.forEach((node, _pos, i) => {
      if (node.attrs.language === 'mermaid') at = i;
    });
    return at;
  });
  return ((await pending()).blocks || []).find((b) => b.index === index);
})();

// --- §12 Esc during the pick goes back to the whole figure ----------------
//
// Pressing Mark a region and then thinking better of it keeps the words: the
// form comes back on the whole figure, and the picture is left as it was.
{
  const { opened } = await openGrip(diagram);
  if (opened) await page.keyboard.type('kept words');
  const mark = page.locator('.gly-composer-region');
  if (opened && (await mark.isVisible())) await mark.click();
  const picking = diagram ? await figureOf(diagram.index) : null;
  await page.keyboard.press('Escape');
  await page.waitForTimeout(200);
  const back = diagram ? await figureOf(diagram.index) : null;
  check(
    'Mark a region puts the figure into picking and puts the form away',
    !!picking && picking.picking && !picking.form,
    picking,
  );
  check(
    'Esc while picking returns to the whole-figure form with the words kept, and leaves nothing in the figure',
    !!back &&
      back.form &&
      back.words === 'kept words' &&
      back.head === 'INSTRUCTION \u00b7 ON this diagram' &&
      !back.picking &&
      back.drafts === 0,
    back,
  );
  await page.keyboard.press('Escape');
}

// --- §11 a region of a diagram, end to end -------------------------------
let regionFiled = '';
{
  const { opened } = await openGrip(diagram);
  if (opened) await page.keyboard.type('this arrow');
  const mark = page.locator('.gly-composer-region');
  if (opened && (await mark.isVisible())) await mark.click();
  const box = diagram ? await figureOf(diagram.index) : null;
  check(
    'Mark a region on the diagram puts it into picking with the form put away',
    !!box && box.picking && !box.form,
    box,
  );
  // A real drag, in steps, from a quarter in to three quarters across and
  // three fifths down.
  if (box) {
    await page.mouse.move(box.x + box.w * 0.25, box.y + box.h * 0.25);
    await page.mouse.down();
    await page.mouse.move(box.x + box.w * 0.75, box.y + box.h * 0.6, {
      steps: 8,
    });
    await page.mouse.up();
    await page.waitForTimeout(200);
  }
  const after = diagram ? await figureOf(diagram.index) : null;
  check(
    'the drag brings the form back with the words in it, on a region of this diagram',
    !!after &&
      after.form &&
      !after.picking &&
      after.drafts === 0 &&
      after.words === 'this arrow' &&
      after.head === 'INSTRUCTION \u00b7 ON a region of this diagram',
    after,
  );
  const one = after && after.form ? await sendFor('this arrow') : null;
  const near = (a, b) => Math.abs(a - b) <= 0.02;
  const r = one && one.region;
  check(
    'sending files the instruction on the diagram with the rectangle that was dragged',
    !!r &&
      one.anchorKey === diagram.key &&
      near(r.x, 0.25) &&
      near(r.y, 0.25) &&
      near(r.w, 0.5) &&
      near(r.h, 0.35),
    one,
  );
  regionFiled = one ? one.key : '';
  if (regionFiled) filed.push(regionFiled);
  const pin = await page
    .waitForSelector('.ProseMirror .gly-figure-mermaid .gly-region-pin', {
      timeout: regionFiled ? 10000 : 1,
    })
    .then(
      (el) =>
        el.evaluate((p) => ({
          left: parseFloat(p.style.left),
          top: parseFloat(p.style.top),
          width: parseFloat(p.style.width),
          height: parseFloat(p.style.height),
        })),
      () => null,
    );
  const pct = (a, b) => Math.abs(a - b) <= 2;
  check(
    'a pin is drawn on the diagram where the rectangle was',
    !!pin &&
      pct(pin.left, 25) &&
      pct(pin.top, 25) &&
      pct(pin.width, 50) &&
      pct(pin.height, 35),
    pin,
  );
  let rung = false;
  if (pin) {
    await page
      .locator('.ProseMirror .gly-figure-mermaid .gly-region-pin')
      .click();
    rung = await page
      .waitForSelector(
        `.gly-rail .gly-thread[data-key="${regionFiled}"].gly-flash`,
        { timeout: 1000 },
      )
      .then(
        () => true,
        () => false,
      );
  }
  check('and pressing the pin rings its card in the rail', rung);
}

// --- §14, a region and a delete -------------------------------------------
//
// A rectangle on a figure is an instruction on that figure, and its grip
// counts it. A block whose one instruction is taken back from its card is a
// block with none again: `+`, a name that counts nothing, the resting paint.
{
  const index = diagram ? diagram.index : -1;
  await gripSays(index, '1');
  const fig = (await gripsNow()).find((g) => g.index === index);
  check(
    'the region on the diagram counts toward the diagram\u2019s grip',
    !!fig &&
      fig.face === '1' &&
      fig.label.endsWith('(1 already)') &&
      fig.commented,
    fig,
  );

  const fence = ((await pending()).blocks || []).find(
    (b) => b.kind === 'codeBlock' && b.label.includes('galley edit'),
  );
  const rest = (await gripsNow()).find((g) => g.index === fence?.index);
  const { one } = await fileOn('codeBlock', 'quote the path');
  await gripSays(fence ? fence.index : -1, '1');
  const lit = (await gripsNow()).find((g) => g.index === fence?.index);
  const del = page.locator(
    `.gly-rail-band .gly-thread[data-key="${one?.key || 'none'}"] .gly-thread-delete`,
  );
  if (one) {
    await del.click();
    await del.click();
  }
  await gripSays(fence ? fence.index : -1, '+');
  const gone = (await gripsNow()).find((g) => g.index === fence?.index);
  check(
    'deleting a block\u2019s one instruction from its card returns its grip to +, unpainted, in the same box',
    !!rest &&
      !!lit &&
      !!gone &&
      lit.face === '1' &&
      lit.commented &&
      gone.face === '+' &&
      !gone.commented &&
      !gone.label.includes('already') &&
      gone.box === lit.box,
    { rest, lit, gone },
  );
}

// --- §19 the grips sit under the bar ------------------------------------
//
// The title's grip is scrolled until it is level with the sticky bar. The bar
// is drawn over it, as it is over the prose, and every control in the bar is
// still the thing under its own centre.
{
  await page.evaluate(() => window.scrollTo(0, 0));
  const under = await page.evaluate(async () => {
    const grip = document.querySelector('.gly-block-grip[data-kind="heading"]');
    const bar = document.querySelector('.gly-bar');
    const g0 = grip.getBoundingClientRect();
    const b0 = bar.getBoundingClientRect();
    window.scrollBy(0, g0.top + g0.height / 2 - (b0.top + b0.height / 2));
    await new Promise((r) => requestAnimationFrame(() => r()));
    const g = grip.getBoundingClientRect();
    const b = bar.getBoundingClientRect();
    const at = document.elementFromPoint(
      g.left + g.width / 2,
      g.top + g.height / 2,
    );
    const controls = [...bar.querySelectorAll('button')]
      .filter(
        (c) =>
          c.checkVisibility({ visibilityProperty: true }) &&
          c.getBoundingClientRect().width > 0,
      )
      .map((c) => {
        const r = c.getBoundingClientRect();
        const hit = document.elementFromPoint(
          r.left + r.width / 2,
          r.top + r.height / 2,
        );
        return { name: c.className || c.id, hit: !!hit && c.contains(hit) };
      });
    const z = (sel) =>
      Number(getComputedStyle(document.querySelector(sel)).zIndex);
    return {
      level: g.top < b.bottom && g.bottom > b.top,
      covered: !!at && !grip.contains(at) && !!at.closest('.gly-bar'),
      controls,
      layer: z('.gly-grips'),
      rail: z('.gly-rail'),
    };
  });
  check(
    'a grip scrolled under the bar is covered by it, and every bar control is still the thing at its centre',
    under.level &&
      under.covered &&
      under.controls.length > 0 &&
      under.controls.every((c) => c.hit),
    under,
  );
  check(
    'and the grips are stacked no higher than the rail',
    under.layer <= under.rail,
    under,
  );
  await page.evaluate(() => window.scrollTo(0, 0));
}

// --- §17 the keyboard ----------------------------------------------------
//
// The grips are the next stops after the document, in the document's order.
// Enter on one opens its composer with the caret in it, and Esc puts the
// reviewer back on the grip they came from.
const focused = () =>
  page.evaluate(() => {
    const a = document.activeElement;
    return a && a.classList.contains('gly-block-grip')
      ? { index: Number(a.dataset.index), kind: a.dataset.kind }
      : { tag: a ? a.tagName : null, cls: a ? String(a.className) : null };
  });
{
  const order = await page.evaluate(() =>
    [...document.querySelectorAll('.gly-block-grip')].map((g) => ({
      index: Number(g.dataset.index),
      kind: g.dataset.kind,
      label: g.getAttribute('aria-label') || '',
    })),
  );
  check(
    'the grips are in the document’s order, and each is named for what it is on',
    order.length === eligible.length &&
      order.every((g, i) => i === 0 || g.index > order[i - 1].index) &&
      order.every((g) => g.label.startsWith('Add an instruction on ')),
    order,
  );
  // A click in the last block first: the browser walks the tab order from
  // where the reviewer last pressed, and that is the document's end.
  // The caret is put there with a synchronous focus: the editor's own focus
  // command lands a frame later, and would take focus back from a grip.
  await page.locator('.ProseMirror h3').click();
  await page.evaluate(() => {
    const editor = window.galleyEdit.editor;
    editor.view.focus();
    editor.commands.setTextSelection(editor.state.doc.content.size - 1);
  });
  await page.waitForTimeout(100);
  // A control drawn INSIDE the document (a figure's region pin) is the
  // document's own and comes first; the first stop past the document is the
  // claim.
  let first = null;
  for (let tab = 0; tab < 8; tab += 1) {
    await page.keyboard.press('Tab');
    first = await focused();
    const inside = await page.evaluate(
      () => !!document.activeElement?.closest('.ProseMirror'),
    );
    if (!inside) break;
  }
  await page.keyboard.press('Tab');
  const second = await focused();
  const strong = await page.evaluate(() =>
    Number(getComputedStyle(document.activeElement).opacity),
  );
  check('a grip with keyboard focus is at full strength', strong === 1, strong);
  check(
    'Tab from the end of the document reaches the first grip, and Tab again the next',
    order.length > 1 &&
      first.index === order[0].index &&
      first.kind === order[0].kind &&
      second.index === order[1].index &&
      second.kind === order[1].kind,
    { first, second, order: order.slice(0, 2) },
  );
  await page.keyboard.press('Enter');
  const opened = await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .then(
      () => true,
      () => false,
    );
  const typing = await page.evaluate(
    () => !!document.activeElement?.classList.contains('gly-composer-text'),
  );
  await page.keyboard.press('Escape');
  const back = await focused();
  const shut = await page.evaluate(
    () => document.querySelector('.gly-composer').hidden,
  );
  check(
    'Enter opens its composer with the caret in it, and Esc closes it and puts focus back on that grip',
    opened &&
      typing &&
      shut &&
      back.index === second.index &&
      back.kind === second.kind,
    { opened, typing, shut, back, second },
  );
}

// ESC FINDS THE GRIP OF THE BLOCK, not the button that stood at its place.
// A block written in above the one the composer is about renumbers every
// grip after it; the grip Esc returns to is the one beside the same block.
{
  const blocks = (await pending()).blocks || [];
  const math = blocks.find((b) => b.kind === 'mathBlock');
  const title = blocks.find((b) => b.kind === 'heading');
  await page.locator('.gly-block-grip[data-kind="mathBlock"]').focus();
  await page.keyboard.press('Enter');
  await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  const pre = await page.evaluate(() => ({
    open: !document.querySelector('.gly-composer').hidden,
    act: String(document.activeElement?.className),
  }));
  await page.evaluate(
    (key) =>
      fetch('/_galley/instruct', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          op: 'comment_block',
          target: key,
          text: 'a note filed elsewhere',
        }),
      }),
    title ? title.key : '',
  );
  const moved = await page
    .waitForFunction(
      (index) =>
        document.querySelector('.gly-block-grip[data-kind="mathBlock"]')
          ?.dataset.index === String(index),
      math ? math.index + 1 : -1,
      { timeout: 10000 },
    )
    .then(
      () => true,
      () => false,
    );
  const open = await page.evaluate(
    () => !document.querySelector('.gly-composer').hidden,
  );
  await page.keyboard.press('Escape');
  const back = await focused();
  const note = ((await pending()).instructions || []).find(
    (i) => i.text === 'a note filed elsewhere',
  );
  if (note) filed.push(note.key);
  check(
    'Esc returns to the grip of the block the composer was about, after a block lands above it',
    moved &&
      open &&
      !!math &&
      back.kind === 'mathBlock' &&
      back.index === math.index + 1,
    { pre, moved, open, back, math: math && math.index },
  );
}

// headFits opens the grip of the block at `index` and reads its composer's
// head: what it says, and whether it fits its box.
const headFits = async (index, region) => {
  await page.locator(`.gly-block-grip[data-index="${index}"]`).click();
  await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  const head = await page.evaluate((r) => {
    const app = window.galleyEdit.app;
    // The region head is written by the same function a finished drag calls;
    // only the drag is skipped.
    if (r) app.headBlockComposer(app.composer.grip, '', true);
    const h = document.querySelector('.gly-composer-head');
    return { text: h.textContent, sw: h.scrollWidth, cw: h.clientWidth };
  }, region);
  await page.keyboard.press('Escape');
  return head;
};

// --- a composer's head is never wider than its box -----------------------
//
// Every kind's head, the region's, and a heading too long to quote whole, at
// the widest window and the narrowest.
const headsAt = async (width) => {
  await page.setViewportSize({ width, height: 900 });
  await page.waitForTimeout(600);
  const heads = [];
  for (const b of (await pending()).blocks || []) {
    if (!GRIP_KINDS.includes(b.kind)) continue;
    heads.push(await headFits(b.index, false));
    const fig = b.kind === 'codeBlock' && b.label.includes('graph');
    if (fig) heads.push(await headFits(b.index, true));
  }
  check(
    `at ${width}px no block composer’s head runs past its box`,
    heads.length > eligible.length &&
      heads.some((h) => h.text.includes('region')) &&
      heads.every((h) => h.text && h.sw <= h.cw),
    heads.filter((h) => !h.text || h.sw > h.cw),
  );
};
await headsAt(1440);

// --- §15 every width ------------------------------------------------------
//
// The column keeps the grip's gutter at every width a desktop window is
// dragged to, so no grip is pushed off the window's left edge or under the
// text, and the page never scrolls sideways to make room.
for (const width of [1440, 1100, 992, 800, 390]) {
  await page.setViewportSize({ width, height: 900 });
  await page.waitForTimeout(600);
  const seats = await page.evaluate(() => window.gripSeats());
  const fit = await page.evaluate(() => ({
    iw: window.innerWidth,
    sw: document.documentElement.scrollWidth,
  }));
  const bad = seats.filter(
    (s) =>
      !(
        s.left >= 0 &&
        s.right <= fit.iw &&
        s.right <= s.blockLeft &&
        Math.abs(s.w - 24) <= 0.5 &&
        Math.abs(s.h - 24) <= 0.5 &&
        s.hit
      ),
  );
  check(
    `at ${width}px every grip is in the window, in the gutter, 24px square and pressable`,
    seats.length === eligible.length && bad.length === 0,
    { n: seats.length, bad },
  );
  check(
    `at ${width}px no two grips overlap, and nothing scrolls sideways`,
    seats.every((s, i) => i === 0 || s.top >= seats[i - 1].bottom) &&
      fit.sw <= fit.iw,
    { fit, tops: seats.map((s) => [s.kind, Math.round(s.top)]) },
  );
}

// --- §16 the narrowest window --------------------------------------------
//
// At 390 the table's composer still fits the window and leaves its grip
// pressable; and the review sheet, open, is drawn over the grips.
{
  await headsAt(390);
  const table = ((await pending()).blocks || []).find(
    (b) => b.kind === 'table',
  );
  await page.evaluate(
    (index) => window.scrollBy(0, window.blockBox(index).top - 150),
    table ? table.index : 0,
  );
  await page.waitForTimeout(150);
  await page.locator('.gly-block-grip[data-kind="table"]').click();
  await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  const where = () =>
    page.evaluate(() => {
      const g = document
        .querySelector('.gly-block-grip[data-kind="table"]')
        .getBoundingClientRect();
      const c = document.querySelector('.gly-composer').getBoundingClientRect();
      const at = document.elementFromPoint(
        g.left + g.width / 2,
        g.top + g.height / 2,
      );
      return {
        left: c.left,
        right: c.right,
        iw: window.innerWidth,
        grip: !!at && !!at.closest('.gly-block-grip'),
        over: at ? String(at.className) : null,
        sheet: !!at && !!at.closest('.gly-sheet, .gly-bottombar'),
      };
    });
  const narrow = await where();
  check(
    'at 390px the table’s composer is inside the window, and its grip is still pressable',
    narrow.left >= 0 && narrow.right <= narrow.iw && narrow.grip,
    narrow,
  );
  await page.keyboard.press('Escape');
  await page.locator('.gly-bar-count').click();
  await page
    .waitForSelector('.gly-sheet:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  const sheet = await where();
  check(
    'and with the review sheet open, the sheet is what is under a grip’s place, not the grip',
    sheet.sheet && !sheet.grip,
    sheet,
  );
  await page.keyboard.press('Escape');
  await page.setViewportSize({ width: 1440, height: 1000 });
}

// fenceIndex is the top-level index of the fixture's plain code block, the
// one that is not a diagram.
const fenceIndex = () =>
  page.evaluate(() => {
    let at = -1;
    window.galleyEdit.editor.state.doc.forEach((node, _pos, i) => {
      if (
        at < 0 &&
        node.type.name === 'codeBlock' &&
        node.attrs.language !== 'mermaid'
      )
        at = i;
    });
    return at;
  });

// boxUnder reads the open composer against the block at `index`, in the
// window as it is now: how far below the block's bottom the box starts, and
// where the box and the window end.
const boxUnder = (index) =>
  page.evaluate((i) => {
    const b = window.blockBox(i);
    const c = document.querySelector('.gly-composer').getBoundingClientRect();
    return {
      open: !document.querySelector('.gly-composer-form').hidden,
      below: +(c.top - b.bottom).toFixed(1),
      top: +c.top.toFixed(1),
      bottom: +c.bottom.toFixed(1),
      blockTop: +b.top.toFixed(1),
      blockBottom: +b.bottom.toFixed(1),
      window: window.innerHeight,
      scrollY: window.scrollY,
      scrolls:
        document.querySelector('.gly-composer-text').style.overflowY === 'auto',
    };
  }, index);

// --- §20 a grip low in a short window opens its box beneath its block ----
//
// THE BOX STAYS WITH ITS BLOCK. With a code block in the lower half of a
// short window there is not room beneath it for the form, and the box used
// to be hung from the window's foot instead — far below the block, over the
// next one. The page is scrolled just far enough that the form fits beneath
// its block, and the box opens there. Growing, it is held to the room the
// window has — here none past its opening height, since the page scrolled
// only that far — and the words scroll inside it.
{
  await page.setViewportSize({ width: 1440, height: 600 });
  await page.waitForTimeout(600);
  const index = await fenceIndex();
  // The block's top at 62% of the window: in its lower half, with less room
  // beneath it than the form needs.
  await page.evaluate(
    (i) => window.scrollBy(0, window.blockBox(i).top - 370),
    index,
  );
  await page.waitForTimeout(200);
  const before = await page.evaluate(() => window.scrollY);
  await page.locator(`.gly-block-grip[data-index="${index}"]`).click();
  await page
    .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
    .catch(() => {});
  await page.waitForTimeout(200);
  const low = await boxUnder(index);
  check(
    'a grip on a block in the lower half of a short window opens its box just beneath the block, scrolling the page to make room',
    low.open &&
      low.below >= 0 &&
      low.below <= 8 &&
      low.top >= 0 &&
      low.bottom <= low.window + 0.5 &&
      low.blockBottom > 0 &&
      low.scrollY > before,
    { ...low, before },
  );
  await page.evaluate(() => {
    const t = document.querySelector('.gly-composer-text');
    t.value = Array.from({ length: 60 }, (_, n) => `line ${n + 1}`).join('\n');
    t.dispatchEvent(new Event('input'));
  });
  await page.waitForTimeout(150);
  const grown = await boxUnder(index);
  check(
    'and with sixty lines typed it stays beneath the block and inside the window, and scrolls inside itself',
    grown.open &&
      Math.abs(grown.top - low.top) <= 1 &&
      grown.bottom <= grown.window + 0.5 &&
      grown.scrolls,
    { low, grown },
  );
  await page.evaluate(() => window.galleyEdit.app.hideComposer());
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.waitForTimeout(400);
}

// --- §21 a click off an empty box closes it ------------------------------
//
// An open box with nothing typed in it is put away by a click anywhere
// outside it, a grip's box and a selection's alike; one holding words stays,
// so a stray click never throws them away. Another grip still moves the box
// to that grip.
{
  const index = await fenceIndex();
  const table = ((await pending()).blocks || []).find(
    (b) => b.kind === 'table',
  );
  // A point in the empty margin, left of every grip: nothing there to press.
  const offBox = () => page.mouse.click(4, 500);
  const state = () =>
    page.evaluate(() => ({
      open: !document.querySelector('.gly-composer').hidden,
      words: document.querySelector('.gly-composer-text').value,
      on: window.galleyEdit.app.composer.grip?.index ?? null,
    }));
  const openGripBox = async (i) => {
    await page.evaluate(
      (n) => window.scrollBy(0, window.blockBox(n).top - 150),
      i,
    );
    await page.waitForTimeout(150);
    await page.locator(`.gly-block-grip[data-index="${i}"]`).click();
    await page
      .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
      .catch(() => {});
  };
  // The words of the first closing paragraph, selected, and the form opened
  // from the button beside them. The caret is put down first: selecting the
  // words already selected is no selection change, so no button would come.
  const openWordsBox = async () => {
    await page.evaluate(() => {
      const editor = window.galleyEdit.editor;
      const doc = editor.state.doc;
      const index = [...Array(doc.childCount).keys()].find((i) =>
        doc.child(i).textContent.startsWith('Closing note 1'),
      );
      let at = 1;
      for (let i = 0; i < index; i += 1) at += doc.child(i).nodeSize;
      window.scrollBy(0, editor.view.coordsAtPos(at).top - 200);
      editor.view.focus();
      editor.commands.setTextSelection(at);
      editor.commands.setTextSelection({ from: at, to: at + 12 });
    });
    await page
      .locator('.gly-comment-button')
      .click({ timeout: 3000 })
      .catch(() => {});
    await page
      .waitForSelector('.gly-composer-form:not([hidden])', { timeout: 3000 })
      .catch(() => {});
  };
  const prose = () =>
    page.locator('.ProseMirror p', { hasText: 'Closing note 3' }).click();

  await openGripBox(index);
  const gripOpen = await state();
  await offBox();
  await page.waitForTimeout(150);
  const gripShut = await state();
  check(
    'a click off a grip’s empty box closes it',
    gripOpen.open && !gripShut.open,
    { gripOpen, gripShut },
  );

  await openGripBox(index);
  await page.keyboard.type('keep these words');
  await offBox();
  await page.waitForTimeout(150);
  const gripKept = await state();
  await prose();
  await page.waitForTimeout(150);
  const gripKeptProse = await state();
  check(
    'a grip’s box holding words stays open, words and all, through a click off it and a click in the prose',
    [gripKept, gripKeptProse].every(
      (s) => s.open && s.words === 'keep these words' && s.on === index,
    ),
    { gripKept, gripKeptProse },
  );
  await page.evaluate(() => window.galleyEdit.app.hideComposer());

  await openWordsBox();
  const wordsOpen = await state();
  await offBox();
  await page.waitForTimeout(150);
  const wordsShut = await state();
  check(
    'a click off a selection’s empty box closes it',
    wordsOpen.open && !wordsShut.open,
    { wordsOpen, wordsShut },
  );

  await openWordsBox();
  await page.keyboard.type('and these');
  await offBox();
  await page.waitForTimeout(150);
  const wordsKept = await state();
  await prose();
  await page.waitForTimeout(150);
  const wordsKeptProse = await state();
  check(
    'a selection’s box holding words stays open, words and all, through a click off it and a click in the prose',
    [wordsKept, wordsKeptProse].every((s) => s.open && s.words === 'and these'),
    { wordsKept, wordsKeptProse },
  );
  await page.evaluate(() => window.galleyEdit.app.hideComposer());

  await openGripBox(index);
  await page.keyboard.type('moving on');
  await openGripBox(table ? table.index : -1);
  const moved = await state();
  check(
    'pressing another grip moves the box to that grip',
    !!table && moved.open && moved.on === table.index && moved.words === '',
    moved,
  );
  await page.evaluate(() => window.galleyEdit.app.hideComposer());
  await page.evaluate(() => window.scrollTo(0, 0));
}

await browser.close();

// --- §6 the file holds the marks and nothing of the grip -----------------
//
// The server is stopped, so what is read is its last save.
{
  await stopped();
  const disk = readFileSync(doc, 'utf8');
  const marks = [...disk.matchAll(/\{>>@comment (cb-[0-9a-f]+)<<\}/g)].map(
    (m) => m[1],
  );
  check(
    'the file holds exactly the instructions\u2019 marks, and no grip face or label',
    marks.length === filed.length &&
      filed.every((k) => marks.includes(k)) &&
      !disk.includes('+') &&
      !disk.includes('Mark a region') &&
      !disk.includes('Add an instruction') &&
      !disk.includes('gly-region') &&
      !disk.includes('<div'),
    { marks, filed, disk },
  );
  // The table's mark is a block of its own: after the last row, before the
  // paragraph that follows, and inside no cell.
  check(
    'the table\u2019s mark is on its own line after the table, before the next block',
    !!tableFiled &&
      disk.includes(
        `| timeout | 30s |\n\n{>>@comment ${tableFiled}<<}\n\n- install it first`,
      ),
    disk,
  );
}

// --- §7, reopened ---------------------------------------------------------
//
// A second server on the same file, on the held port: the instruction and its
// card come back from the file, which is what was saved and nothing else.
{
  const again = await serve(PORT + 1);
  const list = (await (await fetch(`${again}/_galley/pending`)).json())
    .instructions;
  const back = (list || []).find((i) => i.key === tableFiled);
  check(
    'reopened, the table instruction is back on the same table',
    !!back &&
      back.anchor === 'block' &&
      back.blockKind === 'table' &&
      back.anchorKey === tableKey &&
      back.text === 'add a units column',
    back || list,
  );
  const reopened = await chromium.launch({ executablePath });
  children.add(() => reopened.process()?.kill('SIGKILL'));
  const view = await reopened.newPage({
    viewport: { width: 1440, height: 1000 },
  });
  await view.goto(again, { waitUntil: 'networkidle' });
  const card = await view
    .waitForSelector(`.gly-rail-band .gly-thread[data-key="${tableFiled}"]`, {
      timeout: tableFiled ? 15000 : 1,
    })
    .then(
      () => true,
      () => false,
    );
  check('and so is its card', card);
  const region = (list || []).find((i) => i.key === regionFiled)?.region;
  const near = (a, b) => Math.abs(a - b) <= 0.02;
  check(
    'reopened, the diagram’s region is the rectangle that was dragged',
    !!region &&
      near(region.x, 0.25) &&
      near(region.y, 0.25) &&
      near(region.w, 0.5) &&
      near(region.h, 0.35),
    region || list,
  );

  // --- §18 no grip while nothing can be filed ----------------------------
  //
  // On this second server, because the states end the review. History takes
  // the document off the page and the grips with it. While the agent holds
  // the round, and once the review is approved, the server refuses every
  // instruction, so every grip is hidden AND disabled: a button hidden in CSS
  // is still a button to the keyboard. The handoff ends, so the grips are
  // shown to come back from it; approval does not.
  // grips reads the layer and every grip on the second server's page, and
  // whether anything at the places the grips stood live is still a grip.
  let stood = [];
  const grips = async () => {
    const all = await gripsNow(view);
    const under = await view.evaluate(
      (pts) => ({
        layer: !document.querySelector('.gly-grips').hidden,
        hit: pts.some(([x, y]) => {
          const at = document.elementFromPoint(x, y - window.scrollY);
          return !!at && !!at.closest('.gly-block-grip');
        }),
      }),
      stood,
    );
    return { ...under, all };
  };
  const live = (g) =>
    g.layer && g.all.length > 0 && g.all.every((x) => x.shown && !x.off);
  const gone = (g) =>
    !g.layer &&
    !g.hit &&
    g.all.length > 0 &&
    g.all.every((x) => !x.shown && x.off);
  const until = (state) =>
    view
      .waitForFunction((s) => !!window.galleyEdit.app[s] === true, state, {
        timeout: 10000,
      })
      .then(
        () => true,
        () => false,
      );
  const post = (path, body) =>
    view.evaluate(
      ([p, b]) =>
        fetch(p, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(b),
        }).then((r) => r.status),
      [path, body],
    );
  await view.waitForSelector('.gly-block-grip', { timeout: 10000 });
  await view.waitForTimeout(500);
  const before = await grips();
  // The places a reviewer could press a grip at this scroll.
  stood = before.all.map((x) => x.centre).filter(([, y]) => y > 120 && y < 900);

  await view.locator('.gly-versions-open').click();
  await view.waitForTimeout(500);
  const history = await view.evaluate(() =>
    [...document.querySelectorAll('.gly-block-grip')].some((g) =>
      g.checkVisibility(),
    ),
  );
  await view.keyboard.press('Escape');
  await view.waitForTimeout(500);
  const after = await grips();
  check(
    'with History open no grip is shown, and closing it brings every grip back in its place',
    live(before) &&
      !history &&
      live(after) &&
      after.all.map((x) => x.box).join('|') ===
        before.all.map((x) => x.box).join('|'),
    { before: before.all.length, history, after: after.all.length },
  );

  const sent = await post('/_galley/revise', {});
  const held = await until('handoff');
  await view.waitForTimeout(500);
  const holding = await grips();
  const cancelled = await post('/_galley/handoff/cancel', {});
  await view
    .waitForFunction(() => !window.galleyEdit.app.handoff, null, {
      timeout: 10000,
    })
    .catch(() => {});
  await view.waitForTimeout(500);
  const returned = await grips();
  check(
    'while the agent holds the round every grip is hidden and disabled, and they come back when it hands the document back',
    sent < 300 && held && cancelled < 300 && gone(holding) && live(returned),
    {
      sent,
      held,
      cancelled,
      holding: holding.all[0],
      returned: returned.all[0],
    },
  );

  const approved = await post('/_galley/revise', { verdict: 'approve' });
  const sealed = await until('sealed');
  await view.waitForTimeout(500);
  const closed = await grips();
  check(
    'once the review is approved every grip is hidden and disabled, and nothing is pressable where one was',
    sealed && stood.length > 0 && gone(closed),
    {
      approved,
      sealed,
      stood,
      closed: closed.all[0],
      layer: closed.layer,
      hit: closed.hit,
    },
  );
  await reopened.close();
  await stopped();
}

console.log(
  failed.length === 0
    ? '\nall block-grip checks passed'
    : `\n${failed.length} FAILED: ${failed.join('; ')}`,
);
process.exit(failed.length === 0 ? 0 : 1);
