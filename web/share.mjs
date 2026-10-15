// share.mjs — SHARE TAKES REVISE'S SLOT, AND NOTHING ELSE ON THE BAR MOVES.
//
// After approval the reviewer hands the document to a reader. The bar's
// primary slot, which Revise leaves when the review seals, shows `Share ▾`
// instead; its menu downloads the branded HTML or the Markdown, and on macOS
// puts either file on the clipboard to paste as an attachment. The bar's
// `HTML preview ↗` button opens the same rendered page while the review is
// live.
//
// What only a browser can see, and so what this gate measures:
//
//   · Share's box is Revise's box, to the pixel, and every bar control still
//     on screen after the seal is where it was before it.
//   · History, HTML preview, the verdict and Share are one width, and every
//     visible bar button is one height.
//   · The menu's items, verbatim and in order: the two downloads everywhere,
//     the two Shares on macOS only.
//   · Download HTML and Download Markdown are downloads named doc.html and
//     doc.md.
//   · Share HTML writes <doc>.html beside <doc>.md and says so on the status
//     line; Share Markdown says so too. On macOS this really sets the
//     clipboard, to the file; nothing here reads it back.
//   · The preview link answers.
//   · A page under review (page mode) offers Download HTML and Share HTML, for
//     the page itself, and has no preview link.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/share.mjs

import { spawn } from 'node:child_process';
import {
  cpSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  SHARE_LABEL,
  SHARE_DOWNLOAD_HTML,
  SHARE_DOWNLOAD_MD,
  SHARE_SHARE_HTML,
  SHARE_SHARE_MD,
  PREVIEW_LABEL,
  sharedLine,
} from './share.ts';

// The server offers the Shares only where it can put a file on the clipboard.
const MAC = process.platform === 'darwin';

const GALLEY = process.env.GALLEY || 'bin/galley';
const PORT = Number(process.env.PORT || 8274);
const PAGE_PORT = PORT + 2;
const HERE_DIR = dirname(fileURLToPath(import.meta.url));
const FIXTURE_PAGE = join(
  HERE_DIR,
  '..',
  'internal',
  'htmlpage',
  'testdata',
  'galley-tools-index.html',
);

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

// realpath: macOS's tmpdir is a symlink, and the server reports the path it
// resolved.
const HERE = realpathSync(mkdtempSync(join(tmpdir(), 'galley-share-')));
// An empty config home, so a theme on this machine cannot change the page.
const CONFIG = join(HERE, 'config');
const DOC = join(HERE, 'doc.md');
const SAVED = join(HERE, 'doc.html');
writeFileSync(
  DOC,
  '# Share me\n\nA paragraph for the reader.\n\n## Section one\n\nMore words.\n',
);
const PAGE_DIR = join(HERE, 'site');
const PAGE = join(PAGE_DIR, 'index.html');
cpSync(FIXTURE_PAGE, PAGE);

const children = [];
let browser = null;
process.on('exit', () => {
  try {
    const proc = browser && browser.process();
    if (proc) proc.kill('SIGKILL');
  } catch {
    // Already gone.
  }
  for (const child of children) {
    try {
      child.kill('SIGTERM');
    } catch {
      // Already gone.
    }
  }
  try {
    rmSync(HERE, { recursive: true, force: true });
  } catch {
    // A tmpdir left behind is nothing; a throwing exit handler is not.
  }
});

function serve(target, port) {
  const child = spawn(
    GALLEY,
    ['edit', target, '--no-open', '--port', String(port)],
    {
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, XDG_CONFIG_HOME: CONFIG, GALLEY_HOME: '' },
    },
  );
  let said = '';
  for (const stream of [child.stdout, child.stderr]) {
    stream.on('data', (b) => {
      said = (said + b).slice(-8000);
    });
  }
  child.on('exit', (code, signal) => {
    if (signal !== 'SIGTERM') {
      console.log(`      [server exited] code=${code}\n${said.trim()}`);
    }
  });
  children.push(child);
  return `http://127.0.0.1:${port}`;
}

async function up(base) {
  const started = Date.now();
  for (;;) {
    try {
      const res = await fetch(`${base}/`);
      if (res.ok) return;
    } catch {
      // Not up yet.
    }
    if (Date.now() - started > 20000) throw new Error(`${base} never came up`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

const approve = (base) =>
  fetch(`${base}/_galley/revise`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ verdict: 'approve' }),
  });

const { chromium } = await (async () => {
  const where = process.env.GALLEY_PW;
  if (where) {
    const { createRequire } = await import('node:module');
    return createRequire(join(where, 'noop.js'))('playwright-core');
  }
  return import('playwright-core');
})();

const base = serve(DOC, PORT);
const pageBase = serve(PAGE, PAGE_PORT);
await up(base);
await up(pageBase);

browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);

async function open(url) {
  const context = await browser.newContext({
    viewport: { width: 1400, height: 1000 },
  });
  const page = await context.newPage();
  page.on('pageerror', (e) => console.log(`      [page error] ${e.message}`));
  await page.goto(`${url}/`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.ProseMirror', { timeout: 15000 });
  await page.waitForTimeout(1000);
  return page;
}

// Every bar control a reviewer can see, by a key that survives the seal, with
// its box.
const barBoxes = (page) =>
  page.evaluate(() => {
    const out = {};
    const all = document.querySelectorAll(
      '.gly-bar button, .gly-bar a, .gly-bar .gly-doc-name',
    );
    all.forEach((el, i) => {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) return;
      if (getComputedStyle(el).visibility === 'hidden') return;
      const key = el.id || `${el.className}#${i}`;
      out[key] = { x: r.x, y: r.y, w: r.width, h: r.height };
    });
    return out;
  });
// Every bar BUTTON a reviewer can see, with its box: the height check's set.
const barButtonBoxes = (page) =>
  page.evaluate(() => {
    const out = {};
    document.querySelectorAll('.gly-bar button').forEach((el, i) => {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) return;
      if (getComputedStyle(el).visibility === 'hidden') return;
      out[el.id || `${el.className}#${i}`] = { w: r.width, h: r.height };
    });
    return out;
  });
const boxOf = (page, sel) =>
  page.evaluate((s) => {
    const el = document.querySelector(s);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }, sel);
const same = (a, b) =>
  !!a && !!b && ['x', 'y', 'w', 'h'].every((k) => Math.abs(a[k] - b[k]) <= 1);
const shareVisible = (page) =>
  page.evaluate(() => {
    const el = document.querySelector('.gly-share');
    if (!el) return false;
    const r = el.getBoundingClientRect();
    return !el.hidden && r.width > 0 && r.height > 0;
  });
// Approval reaches the page on its next poll; wait for the seal edge to swap
// Share in, and let the check that follows report it if it never did.
const shareShown = (page) =>
  page
    .waitForFunction(
      () => {
        const el = document.querySelector('.gly-share');
        return !!el && !el.hidden && el.getBoundingClientRect().width > 0;
      },
      null,
      { timeout: 10000 },
    )
    .catch(() => {});
const menuItems = (page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.gly-share-menu button')]
      .filter((b) => b.getBoundingClientRect().height > 0)
      .map((b) => b.textContent),
  );
const pick = (page, label) =>
  page
    .click(`.gly-share-menu button:has-text("${label}")`, { timeout: 5000 })
    .catch(() => {});
// The suggested filename of the download one press of label starts.
async function downloadName(page, label) {
  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  const [download] = await Promise.all([
    page.waitForEvent('download', { timeout: 10000 }).catch(() => null),
    pick(page, label),
  ]);
  return download ? download.suggestedFilename() : null;
}
// The status line, once it says want or after ten seconds.
async function statusSays(page, want) {
  await page
    .waitForFunction(
      (w) =>
        (document.getElementById('gly-status')?.textContent || '').includes(w),
      want,
      { timeout: 10000 },
    )
    .catch(() => {});
  return page.evaluate(
    () => document.getElementById('gly-status')?.textContent || '',
  );
}

// --- a markdown review -------------------------------------------------------

{
  const page = await open(base);

  check(
    'before approval the Share button is not on the bar',
    !(await shareVisible(page)),
  );
  const preview = await page.evaluate(() => {
    const b = document.querySelector('.gly-bar .gly-preview-open');
    if (!b) return null;
    const bar = [...document.querySelectorAll('.gly-bar > *')];
    return {
      text: b.textContent,
      shown: b.getBoundingClientRect().width > 0,
      afterMode:
        bar.indexOf(b) > bar.indexOf(document.querySelector('.gly-mode')),
      beforeRevise:
        bar.indexOf(b) < bar.indexOf(document.getElementById('gly-revise')),
    };
  });
  check(
    'the bar carries the HTML preview button, between the mode toggle and Revise',
    !!preview &&
      preview.text === PREVIEW_LABEL &&
      preview.shown &&
      preview.afterMode &&
      preview.beforeRevise,
    preview,
  );
  const opened = await page.evaluate(() => {
    const calls = [];
    const real = window.open;
    window.open = (...a) => {
      calls.push(a);
      return null;
    };
    document.querySelector('.gly-bar .gly-preview-open')?.click();
    window.open = real;
    return calls;
  });
  check(
    'pressing it opens /_galley/render in a new tab, without an opener',
    opened.length === 1 &&
      opened[0][0] === '/_galley/render' &&
      opened[0][1] === '_blank' &&
      String(opened[0][2] || '').includes('noopener'),
    opened,
  );
  {
    const res = await fetch(`${base}/_galley/render`);
    const body = await res.text();
    check(
      'the preview answers with the rendered document',
      res.status === 200 && body.includes('Section one'),
      { status: res.status },
    );
  }
  const previewBefore = await boxOf(page, '.gly-preview-open');
  const historyBefore = await boxOf(page, '.gly-versions-open');
  const reviseBefore = await boxOf(page, '#gly-revise');
  check(
    'History, HTML preview and Revise share one width',
    !!historyBefore &&
      !!previewBefore &&
      !!reviseBefore &&
      Math.abs(historyBefore.w - previewBefore.w) <= 1 &&
      Math.abs(reviseBefore.w - previewBefore.w) <= 1,
    { historyBefore, previewBefore, reviseBefore },
  );
  {
    const heights = Object.entries(await barButtonBoxes(page));
    const h0 = heights[0]?.[1].h;
    check(
      'every visible bar button is one height',
      heights.length > 1 && heights.every(([, b]) => Math.abs(b.h - h0) <= 1),
      heights.map(([k, b]) => ({ k, w: b.w, h: b.h })),
    );
  }

  const before = await barBoxes(page);
  const revise = await boxOf(page, '#gly-revise');
  const approved = await approve(base);
  check('the approve was accepted', approved.ok, approved.status);
  await shareShown(page);
  check(
    'after approval the Share button is on the bar',
    await shareVisible(page),
  );
  const share = await boxOf(page, '.gly-share');
  const previewAfter = await boxOf(page, '.gly-preview-open');
  check(
    'the HTML preview button keeps its box across approval',
    same(previewBefore, previewAfter),
    { previewBefore, previewAfter },
  );
  check("Share occupies exactly Revise's box", same(share, revise), {
    share,
    revise,
  });
  const historyAfter = await boxOf(page, '.gly-versions-open');
  check(
    "Share's width is HTML preview's and History's",
    !!share &&
      !!previewAfter &&
      !!historyAfter &&
      Math.abs(share.w - previewAfter.w) <= 1 &&
      Math.abs(share.w - historyAfter.w) <= 1,
    { share, previewAfter, historyAfter },
  );
  const label = await page.evaluate(
    () => document.querySelector('.gly-share')?.textContent || '',
  );
  check('the Share button reads its label', label === SHARE_LABEL, label);
  const reviseGone = await page.evaluate(() => {
    const el = document.getElementById('gly-revise');
    return !el || el.getBoundingClientRect().width === 0;
  });
  check('Revise has left the slot', reviseGone);
  const after = await barBoxes(page);
  const moved = Object.keys(after)
    .filter((k) => k in before && !k.includes('gly-share'))
    .filter((k) => !same(before[k], after[k]))
    .map((k) => ({ k, before: before[k], after: after[k] }));
  const kept = Object.keys(after).filter((k) => k in before);
  check('no other bar control moved', moved.length === 0 && kept.length > 0, {
    moved,
    kept,
  });

  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  const items = await menuItems(page);
  const want = MAC
    ? [SHARE_DOWNLOAD_HTML, SHARE_DOWNLOAD_MD, SHARE_SHARE_HTML, SHARE_SHARE_MD]
    : [SHARE_DOWNLOAD_HTML, SHARE_DOWNLOAD_MD];
  check(
    `the menu holds ${MAC ? 'the downloads then the Shares' : 'the downloads and no Share (not macOS)'}, verbatim`,
    JSON.stringify(items) === JSON.stringify(want),
    items,
  );
  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});

  check(
    'Download HTML downloads doc.html',
    (await downloadName(page, SHARE_DOWNLOAD_HTML)) === 'doc.html',
  );
  check(
    'Download Markdown downloads doc.md',
    (await downloadName(page, SHARE_DOWNLOAD_MD)) === 'doc.md',
  );

  if (MAC) {
    await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
    await pick(page, SHARE_SHARE_HTML);
    const line = sharedLine(SAVED);
    const status = await statusSays(page, line);
    check(
      'Share HTML says the file is ready to paste',
      line === 'doc.html copied · paste to attach' && status.includes(line),
      status,
    );
    check('doc.html is beside doc.md', existsSync(SAVED), SAVED);
    if (existsSync(SAVED)) {
      const html = readFileSync(SAVED, 'utf8');
      check(
        'the saved page is the document, without the preview refresh',
        html.includes('A paragraph for the reader.') &&
          !html.includes('/_galley/saved'),
      );
    }
    await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
    await pick(page, SHARE_SHARE_MD);
    const mdStatus = await statusSays(page, sharedLine(DOC));
    check(
      'Share Markdown says the .md is ready to paste',
      mdStatus.includes('doc.md copied · paste to attach'),
      mdStatus,
    );
  }
}

// --- a page under review ------------------------------------------------------

{
  const page = await open(pageBase);
  check(
    'a page review has no HTML preview button',
    (await page.locator('.gly-preview-open').count()) === 0,
  );
  const approved = await approve(pageBase);
  check('the page approve was accepted', approved.ok, approved.status);
  await shareShown(page);
  check('the page review shows Share after approval', await shareVisible(page));
  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  const items = await menuItems(page);
  const want = MAC
    ? [SHARE_DOWNLOAD_HTML, SHARE_SHARE_HTML]
    : [SHARE_DOWNLOAD_HTML];
  check(
    `a page review offers ${MAC ? 'Download HTML and Share HTML' : 'only Download HTML (not macOS)'}`,
    JSON.stringify(items) === JSON.stringify(want),
    items,
  );
  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  check(
    'Download HTML downloads the page itself',
    (await downloadName(page, SHARE_DOWNLOAD_HTML)) === 'index.html',
  );
  if (MAC) {
    const before = readFileSync(PAGE, 'utf8');
    await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
    await pick(page, SHARE_SHARE_HTML);
    const status = await statusSays(
      page,
      'index.html copied · paste to attach',
    );
    check(
      'Share HTML puts the page on the clipboard and says so',
      status.includes('index.html copied · paste to attach'),
      status,
    );
    check(
      'Share HTML leaves the page as it was',
      readFileSync(PAGE, 'utf8') === before,
    );
  }
}

console.log(
  failures === 0 ? '\nall share checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
