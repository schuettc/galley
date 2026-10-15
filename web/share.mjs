// share.mjs — SHARE TAKES REVISE'S SLOT, AND NOTHING ELSE ON THE BAR MOVES.
//
// After approval the reviewer hands the document to a reader. The bar's
// primary slot, which Revise leaves when the review seals, shows `Share ▾`
// instead; its menu saves the branded HTML beside the document (and copies
// that path), copies the document's own path, and holds a disabled place for
// a link. The right-hand panel carries a `Preview ↗` link to the same
// rendered page while the review is live.
//
// What only a browser can see, and so what this gate measures:
//
//   · Share's box is Revise's box, to the pixel, and every bar control still
//     on screen after the seal is where it was before it.
//   · The menu's three items, verbatim, and the link item disabled.
//   · Save HTML writes <doc>.html beside <doc>.md, says so on the status line,
//     and puts that path on the clipboard.
//   · The preview link answers.
//   · A page under review (page mode) offers only Copy path, for the page, and
//     has no preview link.
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
  SHARE_SAVE_HTML,
  SHARE_COPY_PATH,
  SHARE_COPY_LINK,
  SHARE_LINK_LATER,
  PREVIEW_LINK,
} from './share.ts';

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
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], {
    origin: url,
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
      .map((b) => ({
        label: b.textContent,
        disabled: b.disabled,
        title: b.title,
      })),
  );

// --- a markdown review -------------------------------------------------------

{
  const page = await open(base);

  check(
    'before approval the Share button is not on the bar',
    !(await shareVisible(page)),
  );
  const preview = await page.evaluate(() => {
    const a = document.querySelector('.gly-rail-preview');
    return a
      ? {
          text: a.textContent,
          href: a.getAttribute('href'),
          target: a.target,
          shown: a.getBoundingClientRect().width > 0,
        }
      : null;
  });
  check(
    'the rail carries the Preview link, opening a new tab',
    !!preview &&
      preview.text === PREVIEW_LINK &&
      preview.href === '/_galley/render' &&
      preview.target === '_blank' &&
      preview.shown,
    preview,
  );
  if (preview) {
    const res = await fetch(`${base}${preview.href}`);
    const body = await res.text();
    check(
      'the preview link answers with the rendered document',
      res.ok && body.includes('Section one'),
      { status: res.status },
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
  check("Share occupies exactly Revise's box", same(share, revise), {
    share,
    revise,
  });
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
  check(
    'the menu holds the three items, verbatim',
    JSON.stringify(items.map((i) => i.label)) ===
      JSON.stringify([SHARE_SAVE_HTML, SHARE_COPY_PATH, SHARE_COPY_LINK]),
    items,
  );
  const link = items.find((i) => i.label === SHARE_COPY_LINK);
  check(
    'the link item is disabled and says why',
    !!link && link.disabled && link.title === SHARE_LINK_LATER,
    link,
  );

  await page
    .click(`.gly-share-menu button:has-text("${SHARE_SAVE_HTML}")`, {
      timeout: 5000,
    })
    .catch(() => {});
  const line = 'saved doc.html · path copied';
  await page
    .waitForFunction(
      (want) =>
        (document.getElementById('gly-status')?.textContent || '').includes(
          want,
        ),
      line,
      { timeout: 10000 },
    )
    .catch(() => {});
  const status = await page.evaluate(
    () => document.getElementById('gly-status')?.textContent || '',
  );
  check('the status line says what was saved', status.includes(line), status);
  check('doc.html is beside doc.md', existsSync(SAVED), SAVED);
  if (existsSync(SAVED)) {
    const html = readFileSync(SAVED, 'utf8');
    check(
      'the saved page is the document, without the preview refresh',
      html.includes('A paragraph for the reader.') &&
        !html.includes('/_galley/saved'),
    );
  }
  const clip = await page.evaluate(() => navigator.clipboard.readText());
  check('the saved path is on the clipboard', clip === SAVED, clip);

  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  await page
    .click(`.gly-share-menu button:has-text("${SHARE_COPY_PATH}")`, {
      timeout: 5000,
    })
    .catch(() => {});
  await page.waitForTimeout(300);
  const mdClip = await page.evaluate(() => navigator.clipboard.readText());
  check('Copy path copies the .md path', mdClip === DOC, mdClip);
}

// --- a page under review ------------------------------------------------------

{
  const page = await open(pageBase);
  check(
    'a page review has no Preview link',
    (await page.locator('.gly-rail-preview').count()) === 0,
  );
  const approved = await approve(pageBase);
  check('the page approve was accepted', approved.ok, approved.status);
  await shareShown(page);
  check('the page review shows Share after approval', await shareVisible(page));
  await page.click('.gly-share', { timeout: 5000 }).catch(() => {});
  const items = await menuItems(page);
  check(
    'a page review offers only Copy path',
    JSON.stringify(items.map((i) => i.label)) ===
      JSON.stringify([SHARE_COPY_PATH]),
    items,
  );
  await page
    .click(`.gly-share-menu button:has-text("${SHARE_COPY_PATH}")`, {
      timeout: 5000,
    })
    .catch(() => {});
  await page.waitForTimeout(300);
  const clip = await page.evaluate(() => navigator.clipboard.readText());
  check("Copy path copies the page's path", clip === PAGE, clip);
}

console.log(
  failures === 0 ? '\nall share checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
