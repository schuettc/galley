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
//   - a closing paragraph.
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
}

// --- §2 a large target, in the gutter, level with its block --------------
//
// Each grip is scrolled to the middle of the window before it is read, so
// "pressable" is asked where a reviewer would press it rather than under the
// bar. Positions are in PAGE coordinates, so grips read at different scrolls
// can be compared with each other.
//
// THE PAIR IS MADE CLOSER THAN ONE GRIP. At this stylesheet's margins no two
// blocks that take a grip sit closer than about 48px (measured at 1440: a
// heading over a table, 61; an h4 over a table, 48), so the heading directly
// over the table does not, by itself, crowd its grips, and "no two grips
// overlap" would hold with no stacking at all. Closing the margins between
// the two makes them a pair the stacker has to separate, and the document
// changing size under the grips is what the repaint has to notice.
const closer = await page.addStyleTag({
  content: `.ProseMirror > h2 { margin-bottom: 0 !important; }
.ProseMirror > h2 + *, .ProseMirror > h2 + * table { margin-top: 0 !important; }`,
});
await page.waitForTimeout(300);
{
  const seats = await page.evaluate(async () => {
    const view = window.galleyEdit.editor.view;
    const doc = view.state.doc;
    const out = [];
    for (const g of document.querySelectorAll(
      '.gly-block-grip:not([hidden])',
    )) {
      const index = Number(g.dataset.index);
      let pos = 0;
      for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
      const node = doc.child(index);
      g.scrollIntoView({ block: 'center' });
      await new Promise((r) => requestAnimationFrame(() => r()));
      const r = g.getBoundingClientRect();
      const b = view.nodeDOM(pos).getBoundingClientRect();
      // A heading's line is its TEXT, not its box: in page mode the box
      // carries the alignment padding above the words.
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
  });
  const near = (a, b) => Math.abs(a - b) <= 2;
  check(
    'every grip is at least 32px square',
    seats.length > 0 && seats.every((s) => s.w >= 32 && s.h >= 32),
    seats.map((s) => [s.kind, s.w, s.h]),
  );
  check(
    'every grip sits in the left gutter: clear of its block and inside the window',
    seats.length > 0 &&
      seats.every((s) => s.right <= s.blockLeft && s.left >= 0),
    seats.map((s) => [s.kind, s.left, s.right, s.blockLeft]),
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
      const lit = [...document.querySelector('.ProseMirror').children]
        .map((e, i) => (e.classList.contains('gly-grip-scope') ? i : -1))
        .filter((i) => i >= 0);
      return { want, lit, from: index };
    },
    heading2 ? heading2.index : -1,
  );
  check(
    'the scope outlines exactly the section: the heading and every block under it',
    !!scope &&
      scope.lit.length === scope.want &&
      scope.lit[0] === scope.from &&
      scope.lit[scope.lit.length - 1] === scope.from + scope.want - 1,
    scope,
  );

  if (opened) {
    await page.keyboard.type('tighten this section');
    await page.keyboard.press('Enter');
  }
  let list = [];
  for (let wait = 0; wait < 40 && list.length === 0; wait += 1) {
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
            const view = window.galleyEdit.editor.view;
            const doc = view.state.doc;
            let pos = 0;
            for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
            const h = view.nodeDOM(pos);
            return Math.round(
              card.getBoundingClientRect().top - h.getBoundingClientRect().top,
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
  await page.keyboard.press('End');
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
    'the file holds exactly the instruction\u2019s mark, and no grip face or label',
    marks.length === 1 &&
      marks[0] === filedKey &&
      !disk.includes('+') &&
      !disk.includes('Mark a region') &&
      !disk.includes('Add an instruction'),
    { marks, filedKey, disk },
  );
}

console.log(
  failed.length === 0
    ? '\nall block-grip checks passed'
    : `\n${failed.length} FAILED: ${failed.join('; ')}`,
);
process.exit(failed.length === 0 ? 0 : 1);
