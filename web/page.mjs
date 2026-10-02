// page.mjs — A PAGE SURVIVES THE LOOP, AND THE SHELL SURVIVES THE REVIEW.
//
// `galley edit page.html` is a markdown review whose document is DERIVED. The
// server splits the page into the prose markdown can carry (content.md, under
// .galley/pages/<base>/) and the shell it cannot (the template), opens the
// ordinary rounds editor on content.md, and re-renders page.html from
// template+content after every projection. To every other surface it is a
// normal review; the seam is one hook (afterProject) that pours the reviewed
// markdown back into the page.
//
// NO OTHER GATE CAN SEE THIS. loop.mjs proves two actors agree on one FILE;
// this one proves the reviewed file is a PROJECTION of a page and the page is
// the reviewed file poured back — a round trip through a second representation
// that neither loop.mjs nor any probe touches. Its two claims are the two the
// design turns on:
//
//   THE PAGE SURVIVES THE LOOP — a reviewer's instruction, answered by the
//   agent editing content.md, lands as a real change in page.html, in the
//   paragraph's ORIGINAL wrapper. The change flows prose → markdown → round →
//   markdown → page, and comes out the far end inside <p class="eyebrow">.
//
//   THE SHELL SURVIVES THE REVIEW — everything markdown cannot carry (the
//   <nav>, the terminal <pre> mockup) is verbatim in the re-rendered page. The
//   reviewer edited prose; the shell is byte-for-byte what the fixture shipped.
//
// SHELL REGIONS ALWAYS APPEAR AS MARKERS: content.md carries "⟦ shell N ⟧"
// paragraphs wherever a shell region lands. There are no screenshots, so the
// gate reads those markers directly and asserts the same shape everywhere.
//
// THE AGENT EDITS content.md, NOT page.html. That is the whole discipline the
// drift guard enforces: page.html is galley's output, and an agent that edits
// it directly is changing the wrong layer. The RED form of this gate (git
// history) proved it — beat 4 edited index.html directly, the reviewed
// document never carried the change, and the check failed. The green form
// edits content.md and watches the page re-render.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/page.mjs
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. There is a `just page`
// recipe, and it is in `just gates` beside the other four.

import { spawn, spawnSync } from 'node:child_process';
import {
  appendFileSync,
  cpSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const GALLEY = process.env.GALLEY || 'bin/galley';
const PORT = Number(process.env.PORT || 8262);
const HERE_DIR = dirname(fileURLToPath(import.meta.url));

// The fixture is one of the launch pages (internal/htmlpage/testdata) — a real
// page with a <nav>, a terminal <pre> mockup, and eighteen shell regions. It
// is copied in as index.html so the working directory galley derives is
// .galley/pages/index/.
const FIXTURE = join(
  HERE_DIR,
  '..',
  'internal',
  'htmlpage',
  'testdata',
  'galley-tools-index.html',
);

// THE PARAGRAPH THE REVIEWER INSTRUCTS AND THE AGENT REVISES. The eyebrow is
// short prose in a WRAPPER WITH A CLASS — <p class="eyebrow"> — which is what
// makes "the new sentence in its original wrapper" a claim worth making: a
// render that dropped the wrapper would put the sentence in a bare <p> and
// this gate would see it.
const OLD_SENTENCE = 'A document two parties revise in rounds';
const NEW_SENTENCE =
  'A page two parties revise in rounds, and galley renders it from the markdown';
const WRAPPED_NEW = `<p class="eyebrow">${NEW_SENTENCE}</p>`;

// The reviewer's ask, anchored on the eyebrow.
const REVIEWER_ASKED = 'say it is a page, and that galley renders it';
const AGENT_NOTE = 'named the page and its rendering';

// A marker stand-in the editor must show: content.md carries this verbatim
// because the shell region above the eyebrow could not be screenshotted.
const MARKER = '⟦ shell 1 ⟧';

// Two things markdown cannot carry, verbatim in the re-rendered page:
const SHELL_NAV = '<nav class="toc"';
const SHELL_PRE = 'galley wait draft.md';

// RED, off by default. With PAGE_RED=1 the agent edits index.html DIRECTLY
// instead of content.md — the wrong layer. The drift guard leaves galley's
// output alone and the reviewed document never carries the change, so the
// page-survives-the-loop check fails. Kept as a switch so the red run is
// reproducible rather than a story.
const RED = process.env.PAGE_RED === '1';

// --- the agent half ---------------------------------------------------------
//
// One process, three verbs and a file, no model — the same shape loop.mjs
// uses. It runs as a CHILD of the reviewer half and reports through a JSONL
// journal so the reviewer half can read its progress WHILE it is still blocked.

function agentMain(doc, htmlPath, journalPath) {
  const say = (line) =>
    appendFileSync(journalPath, `${JSON.stringify(line)}\n`);
  const galley = (...args) => {
    const r = spawnSync(GALLEY, args, { encoding: 'utf8' });
    return { code: r.status, out: r.stdout || '', err: r.stderr || '' };
  };
  const waitFor = (...extra) => {
    const started = Date.now();
    const r = galley('wait', doc, '--json', ...extra);
    let parsed = null;
    try {
      parsed = JSON.parse(r.out);
    } catch {
      // Exit 3 (timed out) / exit 4 (editor stopped) print nothing on stdout.
    }
    return { ...r, parsed, ms: Date.now() - started };
  };

  say({ step: 'armed' });
  const woke = waitFor('--timeout', '120s');
  if (!woke.parsed) {
    say({ step: 'gave-up', code: woke.code, err: woke.err.trim() });
    return;
  }
  const handed = (woke.parsed.pending || {}).instructions || [];
  say({
    step: 'woke',
    reason: woke.parsed.reason,
    fingerprint: woke.parsed.fingerprint,
    instructions: handed.map((i) => ({ text: i.text, quote: i.quote })),
  });
  if (!handed.length) {
    say({ step: 'nothing-to-answer' });
    return;
  }

  // THE REVISION. Green: edit content.md, the reviewed document, and galley
  // renders the page from it. Red: edit index.html directly — the wrong layer,
  // which the drift guard refuses to overwrite and the reviewed document never
  // sees.
  const target = RED ? htmlPath : doc;
  const before = readFileSync(target, 'utf8');
  const after = before.replace(OLD_SENTENCE, NEW_SENTENCE);
  writeFileSync(target, after);
  say({ step: 'wrote', target, red: RED, changed: after !== before });

  // Give the file watcher / render time to project before the ack closes the
  // window — the same shape loop.mjs's spacing has.
  spawnSync(process.execPath, ['-e', 'setTimeout(()=>{},1200)']);

  const acked = galley('ack', doc, '--state', 'answered', '--note', AGENT_NOTE);
  say({ step: 'acked', code: acked.code, err: acked.err.trim() });
  say({ step: 'done' });
}

if (process.argv[2] === '--agent') {
  agentMain(process.argv[3], process.argv[4], process.argv[5]);
  process.exit(0);
}

// --- the reviewer half ------------------------------------------------------

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

const HERE = mkdtempSync(join(tmpdir(), 'galley-page-'));
const PAGE = join(HERE, 'index.html');
const DOC = join(HERE, '.galley', 'pages', 'index', 'content.md');
const JOURNAL = join(HERE, 'agent.jsonl');
cpSync(FIXTURE, PAGE);
writeFileSync(JOURNAL, '');

// Shell regions always land as ⟦ shell N ⟧ markers in content.md, so this gate
// asserts the same shape on any machine: markers in content.md, never
// screenshots.
const server = spawn(
  GALLEY,
  ['edit', PAGE, '--no-open', '--port', String(PORT)],
  {
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env },
  },
);
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

let agent = null;
let browser = null;
process.on('exit', () => {
  try {
    const proc = browser && browser.process();
    if (proc) proc.kill('SIGKILL');
  } catch {
    // Already gone.
  }
  for (const child of [agent, server]) {
    try {
      if (child) child.kill('SIGTERM');
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

const journal = () =>
  readFileSync(JOURNAL, 'utf8')
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l));
const said = (step) => journal().find((l) => l.step === step) || null;
const heard = async (step, ms) => {
  const started = Date.now();
  for (;;) {
    const line = said(step);
    if (line) return line;
    if (Date.now() - started > ms) return null;
    await new Promise((r) => setTimeout(r, 200));
  }
};

browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);
const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
page.on('pageerror', (e) => console.log(`      [page error] ${e.message}`));
await page.goto(`${base}/`, { waitUntil: 'networkidle' });
await page.waitForSelector('.ProseMirror', { timeout: 15000 });
await page.waitForTimeout(1000);
await page.click('.ProseMirror');

const pendingLive = async () =>
  await (await fetch(`${base}/_galley/pending`)).json();
const docText = () =>
  page.evaluate(() => window.galleyEdit.editor.state.doc.textContent);

const select = (phrase) =>
  page.evaluate((want) => {
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
    editor.commands.setTextSelection({ from: at, to: at + want.length });
    editor.view.focus();
    if (!editor.isFocused) {
      throw new Error(
        'fixture: the editor did not take focus — a keystroke would go nowhere',
      );
    }
    return { from: at, to: at + want.length };
  }, phrase);

// --- §1 galley opened the page as a derived markdown review --------------

{
  // content.md exists under .galley/pages/index/ — the page was split, not
  // opened as-is. Read from disk because it is the document of record here.
  let content = '';
  try {
    content = readFileSync(DOC, 'utf8');
  } catch {
    // Left empty — the check reports it.
  }
  check(
    'galley derived content.md from the page',
    content.includes(OLD_SENTENCE) && content.includes(MARKER),
    {
      hasProse: content.includes(OLD_SENTENCE),
      hasMarker: content.includes(MARKER),
    },
  );
}

// --- §2 the editor shows prose AND a marker stand-in --------------------
//
// The marker floor is the CI shape, forced here. The editor must show the
// reviewer real prose to edit AND the stand-ins that mark the shell it cannot
// — asserted from the live document, not a screenshot, because CI has neither
// a screenshot nor a Chrome to take one.

{
  const text = await docText();
  check(
    'the editor shows the page prose the reviewer can edit',
    text.includes(OLD_SENTENCE),
    text.slice(0, 200),
  );
  check(
    'and it shows at least one shell stand-in — the marker floor, not a screenshot',
    text.includes(MARKER),
    text.slice(0, 200),
  );
}

// --- §2b the block grips in each view -------------------------------------
//
// The grips belong to the content pane. In Content view they sit in the
// gutter beside its blocks; in Both view the gutter is the content pane's own,
// so no grip hangs over the live page beside it; in HTML view the content
// pane is gone and its grips with it. A view switch carries each grip with its
// block: the gap between them is the same in every view that shows them.
{
  const grips = () =>
    page.evaluate(() => {
      const view = window.galleyEdit.editor.view;
      const doc = view.state.doc;
      const e = document.getElementById('editor').getBoundingClientRect();
      const pv = document.getElementById('gly-preview');
      const p = pv && pv.checkVisibility() ? pv.getBoundingClientRect() : null;
      return [...document.querySelectorAll('.gly-block-grip')].map((g) => {
        const index = Number(g.dataset.index);
        let pos = 0;
        for (let i = 0; i < index; i += 1) pos += doc.child(i).nodeSize;
        const node = doc.child(index);
        const r = g.getBoundingClientRect();
        const b = view.nodeDOM(pos).getBoundingClientRect();
        const line =
          node.type.name === 'heading' ? view.coordsAtPos(pos + 1).top : b.top;
        return {
          index,
          shown: g.checkVisibility(),
          beside: r.right <= b.left && b.left - r.right <= 16,
          gap: Math.round((b.left - r.right) * 10) / 10,
          rise: Math.round((r.top - line) * 10) / 10,
          inEditor: r.left >= e.left && r.right <= e.right,
          overPage:
            !!p &&
            r.left < p.right &&
            r.right > p.left &&
            r.top < p.bottom &&
            r.bottom > p.top,
        };
      });
    });
  const show = async (v) => {
    await page.click(`.gly-view-btn[data-view="${v}"]`);
    await page.waitForTimeout(800);
    return grips();
  };
  const content = await show('content');
  check(
    'in Content view every block grip is shown, in the gutter beside its block',
    content.length > 0 && content.every((g) => g.shown && g.beside),
    content.filter((g) => !g.shown || !g.beside),
  );
  const both = await show('both');
  check(
    'in Both view every grip is inside the content pane and none hangs over the live page',
    both.length > 0 &&
      both.every((g) => g.shown && g.beside && g.inEditor && !g.overPage),
    both.filter((g) => !g.shown || !g.beside || !g.inEditor || g.overPage),
  );
  check(
    'and switching between them carries every grip with its block',
    both.length === content.length &&
      both.every(
        (g, i) => g.gap === content[i].gap && g.rise === content[i].rise,
      ),
    both.map((g, i) => [g.gap, content[i]?.gap, g.rise, content[i]?.rise]),
  );
  const html = await show('html');
  check(
    'in HTML view no grip is shown',
    html.length > 0 && html.every((g) => !g.shown),
    html.filter((g) => g.shown),
  );
  await show('both');
}

// --- §3 the reviewer instructs the eyebrow, and arms the agent ----------

agent = spawn(
  process.execPath,
  [process.argv[1], '--agent', DOC, PAGE, JOURNAL],
  { stdio: ['ignore', 'pipe', 'pipe'] },
);
agent.stderr.on('data', (b) => {
  const line = String(b).trim();
  if (line && !line.startsWith('galley: free trial')) {
    console.log(`      [agent] ${line}`);
  }
});
check(
  'the agent armed on the derived document',
  !!(await heard('armed', 10000)),
  journal(),
);
await page.waitForTimeout(1500);

await select(OLD_SENTENCE);
await page.waitForTimeout(300);
await page.click('.gly-composer .gly-comment-button');
await page.fill('.gly-composer .gly-composer-text', REVIEWER_ASKED);
await page.click('.gly-composer .gly-composer-send');
await page.waitForTimeout(1000);

const instruction = await (async () => {
  const view = await pendingLive();
  return view.instructions.find((i) => i.quote === OLD_SENTENCE) || null;
})();
check(
  "the reviewer's instruction anchored on the eyebrow prose",
  !!instruction && instruction.text === REVIEWER_ASKED,
  instruction || (await pendingLive()).instructions,
);
check(
  'and the agent is still blocked — nothing wakes it but Revise',
  !!said('armed') && !said('woke') && !said('gave-up'),
  journal(),
);

// --- §4 Revise, the agent revises content.md, the page re-renders --------

await page.click('#gly-revise');
await page.click('.gly-verdict-menu .gly-verdict-revise');

const woke = await heard('woke', 20000);
check(
  'pressing Revise woke the agent that was blocked on the document',
  !!woke && woke.reason === 'revise',
  woke || journal(),
);
check(
  "and it was handed the reviewer's ask, with the words it quotes",
  !!woke &&
    (woke.instructions || []).length === 1 &&
    woke.instructions[0].text === REVIEWER_ASKED &&
    woke.instructions[0].quote === OLD_SENTENCE,
  woke && woke.instructions,
);

const wrote = await heard('wrote', 20000);
check(
  'the agent made its revision',
  !!wrote && wrote.changed,
  wrote || journal(),
);

// WAIT FOR THE PROJECTION THIS CHECK IS ABOUT — the page on disk carrying THIS
// revision's sentence — not for the clock and not for any write. The poll is
// an explicit synchronous read of the file (readFileSync), never an async
// waitForFunction whose promise would resolve truthy on the first tick.
const pageHas = async (want, ms) => {
  const started = Date.now();
  for (;;) {
    let cur = '';
    try {
      cur = readFileSync(PAGE, 'utf8');
    } catch {
      // Between writes; try again.
    }
    if (cur.includes(want) || Date.now() - started > ms) return cur;
    await new Promise((r) => setTimeout(r, 200));
  }
};

{
  const rendered = await pageHas(NEW_SENTENCE, 20000);
  // THE PAGE SURVIVES THE LOOP. The reviewer's instruction, answered by the
  // agent editing content.md, is in page.html — in the eyebrow's ORIGINAL
  // wrapper, poured back through the template. In the red form the agent edited
  // the page directly and the reviewed content.md never carried the change, so
  // this render never happens: the check fails, which is what the drift guard
  // is for.
  const content = (() => {
    try {
      return readFileSync(DOC, 'utf8');
    } catch {
      return '';
    }
  })();
  // TWO DISTINCT ASSERTIONS. A direct edit of index.html (the wrong layer)
  // satisfies the second check alone — the page byte contains the sentence
  // because the agent hacked it in — but content.md never carries the change,
  // so the first check fails. Keeping them separate makes each layer's failure
  // visible on its own; a single dual condition would mask a wrong-layer edit
  // by letting the content half veto the whole thing in the red run, or — more
  // dangerously — would accept a page-only edit as green if the condition were
  // written the other way. The first check is the drift guard's trip-wire.
  check(
    'content.md (the reviewed layer) carries the new sentence',
    content.includes(NEW_SENTENCE),
    {
      contentHasNew: content.includes(NEW_SENTENCE),
      contentHasOld: content.includes(OLD_SENTENCE),
    },
  );
  check(
    'the page re-rendered the reviewed sentence in its original wrapper',
    rendered.includes(WRAPPED_NEW),
    { pageHasWrapped: rendered.includes(WRAPPED_NEW) },
  );
  // THE SHELL SURVIVES THE REVIEW. Everything markdown cannot carry is verbatim
  // in the re-rendered page: the table-of-contents <nav> and the terminal <pre>
  // mockup, neither of which the reviewer ever saw as editable prose.
  check(
    'and the shell the reviewer never touched is verbatim in the page',
    rendered.includes(SHELL_NAV) && rendered.includes(SHELL_PRE),
    {
      nav: rendered.includes(SHELL_NAV),
      pre: rendered.includes(SHELL_PRE),
    },
  );
}

// --- §5 the ack commits the round, seen as an ordinary round -------------

{
  const acked = await heard('acked', 25000);
  check(
    'the agent committed the round with one terminal ack',
    !!acked && acked.code === 0,
    acked || journal(),
  );
  await page
    .waitForFunction(() => window.galleyEdit.editor.isEditable === true, null, {
      timeout: 20000,
    })
    .catch((e) => {
      console.log(
        `      [page] waitForFunction(editable) stalled: ${e.message}`,
      );
    });
  check(
    'and the document came back to the reviewer — editable again',
    (await page.evaluate(() => window.galleyEdit.editor.isEditable)) === true,
  );
}

await browser.close();

// --- §6 THE DOCUMENT OF RECORD, WITH NOTHING RUNNING --------------------
//
// Stop the server and read the bytes: the page on disk, the derived document,
// and the round log under it. A page-backed review's record is three files,
// and this is the only place all three are read with no session's opinion in
// the way.

server.kill('SIGTERM');
await new Promise((r) => setTimeout(r, 1500));

{
  const settledPage = readFileSync(PAGE, 'utf8');
  check(
    'the settled page carries the revision, in its wrapper, with the shell intact',
    settledPage.includes(WRAPPED_NEW) &&
      settledPage.includes(SHELL_NAV) &&
      settledPage.includes(SHELL_PRE) &&
      !settledPage.includes(`>${OLD_SENTENCE}<`),
    {
      wrapped: settledPage.includes(WRAPPED_NEW),
      nav: settledPage.includes(SHELL_NAV),
      pre: settledPage.includes(SHELL_PRE),
      old: settledPage.includes(`>${OLD_SENTENCE}<`),
    },
  );
  const settledContent = readFileSync(DOC, 'utf8');
  check(
    'the derived document holds the revision as plain markdown, with the marker floor kept',
    settledContent.includes(NEW_SENTENCE) &&
      !settledContent.includes(OLD_SENTENCE) &&
      settledContent.includes(MARKER),
    settledContent.split('\n').filter((l) => /revise|shell 1/.test(l)),
  );
}

{
  // THE ROUNDS, READ OFF THE LOG. The reviewer's press cut a round carrying the
  // ask; the agent's ack cut a round naming the agent. This is where the page
  // review is an ordinary round — the log under a derived document is the same
  // log a plain .md keeps.
  const roundsFile = join(
    dirname(DOC),
    '.galley',
    'versions',
    'content.md',
    'rounds.jsonl',
  );
  let cut = [];
  try {
    cut = readFileSync(roundsFile, 'utf8')
      .trim()
      .split('\n')
      .map((l) => JSON.parse(l));
  } catch {
    // Left empty — the check reports the path.
  }
  const asked = cut.filter((r) => r.reason === 'revise').pop();
  const landed = cut.filter((r) => r.reason === 'landed').pop();
  check(
    "the reviewer's press cut a round that carries the ask",
    !!asked && (asked.asks || []).some((a) => a.text === REVIEWER_ASKED),
    { roundsFile, reasons: cut.map((r) => r.reason), asked },
  );
  check(
    "and the agent's ack cut a round of its own, after the ask",
    !!landed &&
      (landed.instruction || '').includes(AGENT_NOTE) &&
      asked &&
      asked.n < landed.n,
    landed,
  );
}

{
  const done = await heard('done', 5000);
  check(
    'the agent ran the whole round and exited on its own',
    !!done,
    journal(),
  );
}

console.log(
  failures === 0 ? '\nall page checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
