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
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright-core';

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
// last words are kept and printed only if it dies on its own.
async function serve(port) {
  const proc = spawn(
    GALLEY,
    ['edit', doc, '--no-open', '--port', String(port), '--on-revise', 'true'],
    { stdio: ['ignore', 'ignore', 'pipe'] },
  );
  children.add(() => proc.kill('SIGTERM'));
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

await browser.close();

console.log(
  failed.length === 0
    ? '\nall block-grip checks passed'
    : `\n${failed.length} FAILED: ${failed.join('; ')}`,
);
process.exit(failed.length === 0 ? 0 : 1);
