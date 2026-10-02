// codeblock.mjs — A CODE BLOCK TAKES A BLOCK-LEVEL INSTRUCTION, AND ONLY FROM
// ITS OWN GRIP.
//
// A fence is read-only to TYPING and stays that way: a range comment writes a
// `highlight` mark and a `code: true` node carries none, so a selection
// touching a fence gets the deny line and no `Add instruction` button. That
// refusal is the thing this gate protects while it proves the new capability
// beside it — the gutter grip that files a WHOLE-BLOCK note against the fence's
// own key, which needs no mark anywhere and so was always possible.
//
// NO OTHER GATE CAN SEE THIS. probe.mjs drives `codeBlockPos` with a stub view
// and no browser, so it can say the arithmetic is right and nothing about
// whether a pointer over a <pre> ever produces a button; the Go side has never
// heard of a grip. The five claims here are the spec's Acceptance list, in its
// order:
//
//   THE GRIP APPEARS AND IT IS ITS OWN — hovering the fence seats a
//   `.gly-code-grip` in the left gutter beside it, and the section `§` stays
//   hidden, because one affordance meaning two things is the fault this
//   codebase files under one-surface-one-language.
//
//   THE RANGE REFUSAL IS INTACT — a selection INSIDE the fence still shows no
//   comment button and still shows the deny line, in suggestions.ts's own
//   sentence (imported, never copied — a hand-typed sentence here goes stale
//   the day the wording changes and the check keeps reporting ok).
//
//   THE GRIP OPENS THE COMPOSER IN BLOCK MODE — the FORM directly, not the
//   bar: the gesture named its scope by being clicked on one block. The head
//   says INSTRUCTION and quotes nothing, because a whole-fence note is about
//   the block and not about the first 28 characters of a shell command.
//
//   SEND FILES A BLOCK THREAD — exactly one pending instruction, `anchor:
//   "block"`, `blockKind: "codeBlock"`, `anchorKey` equal to the fence's own
//   key in the server's block list, and a card in the rail carrying that key,
//   labelled with the block, sitting BESIDE the fence rather than parked in the
//   anchorless region at the rail's foot.
//
//   IT ROUND-TRIPS THROUGH THE FILES — the file holds the comment's ID mark,
//   `{>>@comment cb-…<<}`, immediately after the fence and NOT its words; the
//   unsent round, pending.json, holds the words. Both are still there after
//   the server is stopped, and when `galley edit` is opened on the document
//   again the card is back beside the fence with the same words. A thread that
//   lives only in a session is not a thread that survives a round.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/codeblock.mjs
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. It is in `just gates` beside
// the other five, and a step of its own in ci.yml.

import { spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright-core';
// THE GATE READS THE APP'S OWN CONSTANT, not a copy of it — rounds-ux.mjs's
// discipline for CAPTURE_LABEL, and the reason is the same: the deny line's
// wording is a product decision recorded in one place, and a check that
// asserts a hand-copied sentence stops asserting anything the day it changes.
import { FENCE_INSIDE } from './suggestions.ts';

const GALLEY = process.env.GALLEY || 'bin/galley';
// TWO PORTS, because the round-trip claim is about REOPENING: the first server
// is stopped and a second is started on the same file. A single port would
// bind again a second later, which is exactly the shape that makes a run
// intermittently talk to a socket the kernel has not finished releasing.
const PORT = Number(process.env.PORT || 8264);
const REOPEN_PORT = PORT + 1;

// THE FIXTURE. A heading, prose, ONE top-level fence, prose — the fence sits a
// couple of hundred pixels down the page on purpose, so the rail card's
// placement beside it is a real measurement rather than one satisfied by the
// band's own top (see stackCards: a card at a mark in the first paragraph is
// held down to the band's ceiling, and a fence up there would let a wrongly
// anchored card pass).
const COMMAND = 'npm install -g galley';
const FENCE = '```bash\n' + COMMAND + '\ngalley edit README.md\n```';
const PROSE = 'The installer is short, and a reviewer reads it before running.';
const FIXTURE =
  '# Installing galley\n\n' +
  PROSE +
  '\n\n' +
  FENCE +
  '\n\nThat is the whole of it, and nothing else is required.\n';

// The reviewer's ask. It is never quoted back by the head — a block note has no
// phrase — so this string appears in the pending view, the card and the file,
// and nowhere else.
const ASKED = 'use pnpm here, and say why in a comment above it';

let failures = 0;
const check = (name, ok, detail) => {
  console.log(
    `${ok ? 'ok  ' : 'FAIL'}  ${name}${ok || detail === undefined ? '' : ` — ${JSON.stringify(detail)}`}`,
  );
  if (!ok) failures += 1;
};

const dir = mkdtempSync(join(tmpdir(), 'galley-codeblock-'));
const doc = join(dir, 'install.md');
writeFileSync(doc, FIXTURE);

// EVERY SERVER THIS GATE STARTS IS REMEMBERED, because it starts two and the
// second one only exists after the first is dead. A leaked `galley edit` holds
// the port and the next run measures somebody else's document (undo.mjs
// records that afternoon).
const running = [];
const start = (port) => {
  const proc = spawn(
    GALLEY,
    ['edit', doc, '--no-open', '--port', String(port), '--on-revise', 'true'],
    { stdio: ['ignore', 'pipe', 'pipe'] },
  );
  let said = '';
  for (const stream of [proc.stdout, proc.stderr]) {
    stream.on('data', (b) => {
      said = (said + b).slice(-4000);
    });
  }
  proc.on('exit', (code, signal) => {
    // SIGTERM is our own clean shutdown between phases, and a graceful exit
    // reports code 0 / signal null — neither is a crash, so neither prints.
    if (signal !== 'SIGTERM' && code !== 0) {
      console.log(`      [server ${port} exited] ${code} ${said.trim()}`);
    }
  });
  running.push(proc);
  return proc;
};

let browser = null;
process.on('exit', () => {
  try {
    const proc = browser && browser.process();
    if (proc) proc.kill('SIGKILL');
  } catch {
    // Already gone.
  }
  for (const proc of running) {
    try {
      proc.kill('SIGTERM');
    } catch {
      // Already gone.
    }
  }
  try {
    rmSync(dir, { recursive: true, force: true });
  } catch {
    // A tmpdir left behind is nothing; a throwing exit handler is not.
  }
});

// WAIT FOR THE SERVER TO ANSWER, not for a clock. Polling `/` is what page.mjs
// does; the difference here is that it runs twice, once per server, so it is a
// function rather than a block.
const up = async (port) => {
  const started = Date.now();
  for (;;) {
    try {
      if ((await fetch(`http://127.0.0.1:${port}/`)).ok) return;
    } catch {
      // Not up yet.
    }
    if (Date.now() - started > 20000) {
      throw new Error(`server on ${port} never came up`);
    }
    await new Promise((r) => setTimeout(r, 200));
  }
};

const chromePath = process.env.GALLEY_CHROME;
// Wide enough that the rail is beside the document rather than under it — the
// card's placement beside the fence is one of the claims, and a narrow window
// folds the rail away where there is nothing to measure.
const VIEWPORT = { width: 1400, height: 1000 };

// open drives a fresh page against a running server and hands back the page and
// the live pending view — the two things every section below reads. Both
// servers are opened through it, so the reopened document is looked at exactly
// the way the first one was.
// It waits on the FENCE rather than on `.ProseMirror`, which is both stricter
// and the reason this is not the other gates' launch block: everything below
// hovers a <pre>, so a page whose editor is up but whose fence has not painted
// is not a page this gate can read. (The gates' newPage/goto/wait sequence is
// five lines the dupes ratchet correctly reports as a clone; undo.mjs's rule
// applies — the answer to a clone is to not write it, not to raise a bound.)
const open = async (port) => {
  const page = await browser.newPage({ viewport: VIEWPORT });
  page.on('pageerror', (crash) =>
    console.log(`      [page ${port}] ${crash.message}`),
  );
  await page.goto(`http://127.0.0.1:${port}/`, { waitUntil: 'networkidle' });
  await page.locator('.ProseMirror pre').first().waitFor({ timeout: 15000 });
  await page.waitForTimeout(1200);
  return page;
};

const pendingOn = async (port) =>
  await (await fetch(`http://127.0.0.1:${port}/_galley/pending`)).json();

// The fence's own BlockRef, out of the server's block list. The key is a
// content hash and nothing in the browser (or in this gate) can compute one, so
// every assertion about which block the thread landed on compares against this.
const fenceRef = (view) =>
  (view.blocks || []).find((b) => b.kind === 'codeBlock') || null;

// A selection made THROUGH THE EDITOR, for rounds-ux.mjs's measured reason: a
// DOM selection set from outside races ProseMirror's re-read of it and the
// composer answers `hide` after the caller has already seen it open. codeBlock
// is a textblock, so the fence's own text is reachable the same way prose is.
const selectInFence = (page, want) =>
  page.evaluate((phrase) => {
    const editor = window.galleyEdit.editor;
    let at = null;
    editor.state.doc.descendants((node, pos) => {
      if (at !== null) return false;
      if (node.type.name === 'codeBlock') {
        const i = node.textContent.indexOf(phrase);
        if (i !== -1) at = pos + 1 + i;
      }
      return at === null;
    });
    if (at === null) {
      throw new Error(`fixture: no fence holds ${JSON.stringify(phrase)}`);
    }
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + phrase.length });
    editor.view.focus();
  }, want);

start(PORT);
await up(PORT);
browser = await chromium.launch(
  chromePath ? { executablePath: chromePath } : {},
);
const page = await open(PORT);

// --- §1 the fence is one addressable block ------------------------------

const ref = fenceRef(await pendingOn(PORT));
check(
  'the server offers the fence as an addressable block with a key',
  !!ref && !!ref.key && ref.kind === 'codeBlock',
  ref,
);
check(
  'and the editor renders it as exactly one <pre>',
  (await page.locator('.ProseMirror pre').count()) === 1,
);

// --- §2 hovering it arms a grip of its own ------------------------------

await page.hover('.ProseMirror pre');
await page.waitForSelector('.gly-code-grip:not([hidden])', { timeout: 5000 });
{
  const seat = await page.evaluate(() => {
    const grip = document.querySelector('.gly-code-grip');
    const pre = document.querySelector('.ProseMirror pre');
    const g = grip.getBoundingClientRect();
    const p = pre.getBoundingClientRect();
    const section = document.querySelector('.gly-grip:not(.gly-code-grip)');
    return {
      glyph: grip.textContent,
      title: grip.title,
      left: Math.round(g.left - p.left),
      top: Math.round(g.top - p.top),
      sectionShown: !!section && !section.hidden,
    };
  });
  check(
    'hovering the code block seats its grip in the LEFT gutter beside it',
    seat.left < 0 && Math.abs(seat.top) < 40,
    seat,
  );
  check(
    'and the grip is the code block\u2019s own \u2014 not the section \u00a7',
    seat.glyph === '{}' &&
      seat.title === 'instruct on this whole code block' &&
      seat.sectionShown === false,
    seat,
  );
}

// --- §3 a selection inside the fence is still refused -------------------
//
// THE REFUSAL THIS FEATURE MUST NOT HAVE WEAKENED. Asserted BEFORE the grip is
// clicked and from the state the grip's hover left behind, so a run that
// somehow armed the range path on a fence is red here rather than green
// everywhere.

await selectInFence(page, COMMAND);
await page.waitForTimeout(400);
{
  const refused = await page.evaluate(() => {
    const shown = (sel) => {
      const el = document.querySelector(sel);
      return !!el && !el.hidden && el.offsetParent !== null;
    };
    const deny = document.querySelector('.gly-composer-deny');
    return {
      button: shown('.gly-composer .gly-comment-button'),
      form: shown('.gly-composer .gly-composer-form'),
      deny: shown('.gly-composer-deny'),
      said: deny ? deny.textContent : '',
      selected: window.getSelection().toString(),
    };
  });
  check(
    'a selection inside the fence offers NO range composer',
    refused.selected === COMMAND && !refused.button && !refused.form,
    refused,
  );
  check(
    'and it says why, in suggestions.ts\u2019s own sentence',
    refused.deny && refused.said === FENCE_INSIDE,
    refused,
  );
}

// --- §4 the grip opens the composer in BLOCK mode -----------------------
//
// ESC FIRST, AND IT IS PART OF THE CLAIM. The deny popover is placed under the
// refused selection and lands ON the fence, so the reviewer's way out of a
// refusal has to be the way in to the grip: `esc cancels`, the whisper the
// composer prints beside its own cancel. Without it the pointer never reaches
// the block again — measured, as a 30s hover timeout.
await page.keyboard.press('Escape');
await page.waitForFunction(
  () => document.querySelector('.gly-composer').hidden,
  null,
  { timeout: 5000 },
);
await page.hover('.ProseMirror pre');
await page.waitForSelector('.gly-code-grip:not([hidden])', { timeout: 5000 });
await page.click('.gly-code-grip');
await page.waitForSelector('.gly-composer-form:not([hidden])', {
  timeout: 5000,
});
{
  const opened = await page.evaluate(() => {
    const el = (sel) => document.querySelector(sel);
    return {
      bar: !el('.gly-composer-bar').hidden,
      deny: !el('.gly-composer-deny').hidden,
      head: el('.gly-composer-head').textContent,
      send: el('.gly-composer-send').disabled,
      block: window.galleyEdit.app.composer.block,
      below:
        Math.round(el('.gly-composer').getBoundingClientRect().top) >=
        Math.round(el('.ProseMirror pre').getBoundingClientRect().top),
    };
  });
  check(
    'clicking the grip opens the FORM \u2014 no bar to press, no deny line',
    !opened.bar && !opened.deny && !opened.send,
    opened,
  );
  check(
    'headed INSTRUCTION with nothing quoted \u2014 a whole-fence note is about the block',
    opened.head === 'INSTRUCTION',
    opened,
  );
  check(
    'and it is aimed at the fence\u2019s own block key, clear of the block it is about',
    !!opened.block && opened.block.key === ref.key && opened.below,
    opened,
  );
}

// --- §5 sending files ONE block thread on that fence --------------------

await page.fill('.gly-composer-text', ASKED);
await page.click('.gly-composer-send');
await page.waitForSelector('.gly-rail-band .gly-thread', { timeout: 10000 });
await page.waitForTimeout(600);

const filed = await pendingOn(PORT);
{
  const list = filed.instructions || [];
  const one = list.length === 1 ? list[0] : null;
  check(
    'exactly one instruction is pending, and it is a BLOCK one on the fence',
    !!one &&
      one.text === ASKED &&
      one.anchor === 'block' &&
      one.blockKind === 'codeBlock' &&
      one.anchorKey === ref.key,
    one || list,
  );
  // WHAT THE AGENT IS HANDED TO IDENTIFY THE BLOCK. `quote` on a block thread
  // is the server's own LABEL for the block (`bash: npm install -g galley`) and
  // not a phrase the reviewer selected — there was no selection. It is checked
  // against the block list rather than spelled here, so a label rule that
  // changes shows up as one red line instead of a stale literal, and `region`
  // is empty because a fence is not a rectangle on a picture.
  check(
    'it names the block rather than a phrase — the label, and no region',
    !!one && one.quote === ref.label && !one.region,
    { quote: one && one.quote, label: ref.label, region: one && one.region },
  );

  const card = await page.evaluate(() => {
    const el = document.querySelector('.gly-rail-band .gly-thread');
    const pre = document.querySelector('.ProseMirror pre');
    return {
      cards: document.querySelectorAll('.gly-rail-band .gly-thread').length,
      key: el ? el.dataset.key : '',
      head: el ? el.querySelector('.gly-card-head').textContent : '',
      said: el
        ? Array.from(el.querySelectorAll('.gly-thread-entry p')).map(
            (p) => p.textContent,
          )
        : [],
      adrift: el ? el.classList.contains('gly-adrift') : true,
      // Both in viewport coordinates in the same frame: the card is placed at
      // its anchor's document top, so beside the fence means the same number.
      offset:
        el && pre
          ? Math.round(
              el.getBoundingClientRect().top - pre.getBoundingClientRect().top,
            )
          : null,
    };
  });
  check(
    'a card is in the rail carrying that thread and the reviewer\u2019s words',
    card.cards === 1 &&
      !!one &&
      card.key === one.key &&
      card.said.length === 1 &&
      card.said[0] === ASKED,
    card,
  );
  check(
    'and it is ANCHORED TO THE BLOCK \u2014 labelled with it, beside it, not adrift',
    !card.adrift &&
      card.head.includes(ref.label.slice(0, 12)) &&
      card.offset !== null &&
      card.offset > -12 &&
      card.offset < 140,
    { ...card, label: ref.label },
  );
}

// --- §6 the mark is in the FILE, after the fence; the words are not ------

// The unsent round, beside the document's versions (internal/unsent.Path).
const unsentPath = join(
  dir,
  '.galley',
  'versions',
  'install.md',
  'pending.json',
);
const MARK = /```\n\n\{>>@comment (cb-[0-9a-f]{16})<<\}/;
let filedKey = '';
{
  const onDisk = readFileSync(doc, 'utf8');
  const mark = onDisk.match(MARK);
  filedKey = mark ? mark[1] : '';
  check(
    'the file carries {>>@comment cb-\u2026<<} immediately after the fence, and not the words',
    !!mark && !onDisk.includes(ASKED),
    onDisk,
  );
  const unsent = JSON.parse(readFileSync(unsentPath, 'utf8'));
  const saved = (unsent.comments || []).filter((c) => c.key === filedKey);
  check(
    'pending.json holds the words, under the key the mark names',
    saved.length === 1 && saved[0].text === ASKED && saved[0].kind === 'block',
    unsent,
  );
}

await browser.close();
browser = null;
running.pop().kill('SIGTERM');
await new Promise((r) => setTimeout(r, 1500));

// --- §7 THE ROUND TRIP: reopen the document, with nothing running -------
//
// The file is read once with no session's opinion in the way, and then
// `galley edit` is started on it again — a save/reload the long way round,
// which is the only form that proves the thread was stored rather than
// remembered. A block note that came back as a paragraph, or on the wrong
// block, or as a range thread, all fail here and nowhere else.

{
  const settled = readFileSync(doc, 'utf8');
  check(
    'with the editor stopped, the fence and its mark are both still in the file',
    settled.includes(FENCE) &&
      !!filedKey &&
      settled.includes('```\n\n{>>@comment ' + filedKey + '<<}'),
    settled,
  );
}

start(REOPEN_PORT);
await up(REOPEN_PORT);
browser = await chromium.launch(
  chromePath ? { executablePath: chromePath } : {},
);
const reopened = await open(REOPEN_PORT);
await reopened.waitForSelector('.gly-rail-band .gly-thread', {
  timeout: 10000,
});

{
  const view = await pendingOn(REOPEN_PORT);
  const again = fenceRef(view);
  const list = view.instructions || [];
  const one = list.length === 1 ? list[0] : null;
  check(
    'reopening the document reads the comment back as a block instruction on the fence',
    !!one &&
      one.key === filedKey &&
      one.text === ASKED &&
      one.anchor === 'block' &&
      one.blockKind === 'codeBlock' &&
      !!again &&
      one.anchorKey === again.key,
    { one, again },
  );
  const card = await reopened.evaluate(() => {
    const el = document.querySelector('.gly-rail-band .gly-thread');
    return {
      cards: document.querySelectorAll('.gly-rail-band .gly-thread').length,
      head: el ? el.querySelector('.gly-card-head').textContent : '',
      entries: el ? el.querySelectorAll('.gly-thread-entry').length : 0,
      words: (el && el.querySelector('.gly-thread-entry p')?.textContent) || '',
      adrift: el ? el.classList.contains('gly-adrift') : true,
    };
  });
  check(
    'and the reviewer sees the same card in the rail, still on the block, with the same words',
    card.cards === 1 &&
      !card.adrift &&
      card.entries === 1 &&
      card.words === ASKED &&
      !!again &&
      card.head.includes(again.label.slice(0, 12)),
    { ...card, label: again && again.label },
  );
}

await browser.close();
browser = null;

console.log(
  failures === 0 ? '\nall code-block checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
