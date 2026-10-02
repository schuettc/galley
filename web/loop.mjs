// loop.mjs — TWO ACTORS, ONE DOCUMENT, AND THE FILE HAS THE LAST WORD.
//
// The gate none of the other four is. `verify` compiles, `layers` reads paint,
// `motion` measures geometry across a click, `typing` drives a keyboard. Each
// tests ONE LAYER with ONE ACTOR. Every defect this branch found was TWO ACTORS
// OVER TIME — a reviewer typing while an agent writes, a Revise press with no
// listener, a document written earlier and reopened later — and not one of the
// four can express that shape.
//
// `galley wait` is what makes it expressible. Before it, scripting the agent
// side needed a hook and a spawned process; a script can now block on the
// document, answer with the CLI verbs, and re-arm. So the agent side here is a
// CHILD PROCESS — this same file, re-invoked with --agent — blocked on
// `galley wait` while the browser half types. The two are genuinely concurrent
// rather than interleaved by this script's own ordering, and that is the whole
// point: a version that ran them in turn would be a slower typing.mjs.
//
// galley never calls a model and never pays for tokens. The agent side is the
// CLI verbs and the FILE, and nothing else.
//
// THE ASSERTION NO OTHER GATE MAKES is the last section. Every existing gate
// asserts what the SCREEN shows or what the SERVER reports; none asserts that
// the document of record agrees with BOTH participants — which is exactly where
// `brownred` and the untracked keystroke lived. So this one ends by stopping the
// server and reading the file and the round log under it, with nothing
// running.
//
// ─────────────────────────────────────────────────────────────────────────────
// REWRITTEN AGAIN, AND THIS TIME THE OTHER ACTOR CHANGED JOBS.
//
// This file was DEAD before this rewrite, and had been for the whole interval
// since `galley suggest` and `galley apply` were removed. It died at line 440
// with `Cannot read properties of undefined (reading 'length')` reading
// `pendingView.suggestions`, and a gate that throws six checks into a
// fifty-check run reports nothing about the forty-four after it. A dead gate
// is worse than a switched-off one: it is on the list, it is in the justfile,
// and it certifies nothing.
//
// WHAT MOVED. `pendingView` was `{suggestions, comments, changes, blocks}` and
// is `{instructions, blocks}`. The agent does not PROPOSE any more — `galley
// suggest` and `galley apply` are deleted verbs and `/_galley/suggest` is not
// a route — it EDITS THE .md ITSELF, under a handoff galley holds open, and
// says so with one terminal `galley ack`. The reviewer does not ACCEPT or
// REJECT any more — those verbs and their cards are gone — they FILE
// INSTRUCTIONS, which are immutable facts of a round rather than a
// conversation, and then press Revise.
//
// SO THE GATE'S SUBJECT SURVIVED AND ITS CAST SWAPPED ROLES. It was: the
// reviewer edits directly, the agent proposes, the reviewer decides, and the
// file holds every outcome in the form the reviewer chose. It is: the reviewer
// edits directly, the reviewer ASKS, the agent REVISES THE FILE, and the file
// holds the agent's revision and the reviewer's own hand with nothing tracked
// on either. The last section is unchanged in kind and stronger in one
// respect — the agent's work is IN THE BYTES rather than in markup awaiting a
// decision, so "the document of record agrees with both participants" is a
// claim about the prose itself and not about a settled markup state.
//
// WHAT WAS DELETED RATHER THAN TRANSLATED. Named here and again at each site,
// because a gate that keeps its count by inventing an equivalent reports
// safety it does not provide:
//
//   §3's SUGGESTION PAIR AND §5's ACCEPT/REJECT — `galley suggest --replace`
//   twice, then `.gly-card-accept` and `.gly-card-reject` clicked in the rail,
//   then the file read back for "ACCEPTED: the replacement is in the file with
//   no marker on it" and "REJECTED: the original stands". There is no verb, no
//   card, no class and no outcome to read: the agent's revision IS the file,
//   and nobody decides it. Six checks.
//
//   §2's AND §4's REPLY AND ITS HALF-WRITTEN DRAFT — the sharpest of the ten
//   findings from the first human session, a reply box cleared mid-sentence by
//   somebody else's write, staged here as three agent writes landing under a
//   textarea with a caret at offset 9. Edit mode has no reply box: an
//   instruction card is a head, the reviewer's one sentence and a two-step
//   delete (read `.gly-rail` on a live page and see), `/_galley/reply` and
//   `/_galley/resolve` are review-mode routes only, and `draftFields()` finds
//   nothing to carry. Five checks.
//
//   AND ITS OBVIOUS SUCCESSOR WAS CONSIDERED AND REFUSED. "The reviewer keeps
//   typing in the DOCUMENT while the agent's revision lands" is the same
//   finding on today's surface, and it is STRUCTURALLY IMPOSSIBLE: entry.ts
//   runs `this.editor.setEditable(!this.handoff)`, so from the Revise press to
//   the ack the document is not editable at all. There is no in-flight
//   reviewer work for an agent write to destroy, and a check that manufactured
//   some would be testing a state the product cannot be in. §4 asserts the
//   lock instead, which is the fact that replaced the hazard.
//
//   §5's RESOLVE AND ITS TWO-SIDED THREAD — `.gly-thread-resolve`, then the
//   sidecar read back for a thread `resolved: true` holding both sides of the
//   conversation. An instruction is discharged by the revision, not resolved
//   by the reviewer, and it carries one side because there is only one side.
//   Three checks.
//
//   AND EVERY OTHER SIDECAR READ WITH THEM. `galley edit` writes no
//   `<doc>.comments.json` at any point in a round — measured, not inferred;
//   see the note where §6's sidecar check used to be. The ask's durable home
//   is `rounds.jsonl`, which §6 reads instead. One check.
//
//   §5b's `liveDeletes`/`offlineDeletes` AGREEMENT — Gap 7's grave, live and
//   offline both counting pending DELETIONS at zero. Neither number exists:
//   `pending` reports instructions, which have no kind. The claim underneath
//   it — the settled file holds no CriticMarkup of any kind — is kept and is
//   asserted over ALL spans rather than over `{--` alone. Two checks.
//
// A FINDING THIS REPAIR TURNED UP AND IS NOT THIS BRANCH'S TO FIX: a wake
// carries its instructions WITHOUT THEIR KEYS. `waitFingerprint` builds them
// through `reviewerInstructions`, which sets `Text`, `Quote` and `At` and not
// `Key`, while `galley agent-prompt` tells the agent to name the instruction
// it answered "by the key galley handed you with the round". A `galley wait`
// session is handed none. §3 pins the shape as it actually is, and the ack
// below therefore annotates its changes without an `answers` array — which
// the prompt explicitly permits, and which costs the pairing the whole
// history feature exists for.
//
// WHAT A GREEN RUN IS NOT.
//
// It is not a working product. Ten findings came out of the first real human
// session with galley and NOT ONE had been caught by a test; five more came out
// of the day this file was written, every one of them from driving a layer
// nobody had driven. A gate protects what we have already learned to assert. It
// cannot notice that something feels wrong, that a card lands in the wrong
// place, or that the loop is exhausting to actually use. Green here means the
// things that broke before have not broken again. Nothing more.
//
// Running it:
//
//   just build
//   GALLEY="$PWD/bin/galley" node web/loop.mjs
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>. There is no `just loop`
// any more — the justfile keeps a target for `rounds-ux` alone — so it is
// spelled out here rather than pointed at a recipe that is not there.

import { spawn, spawnSync } from 'node:child_process';
import {
  appendFileSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const GALLEY = process.env.GALLEY || 'bin/galley';
const PORT = Number(process.env.PORT || 8254);

// The fixture's five paragraphs each have one job, and none of them is padding.
//
//   §1 carries a **bold** run the reviewer's strike CROSSES. Three inlines is
//      the shape that used to split a tracked strike into three cards; a
//      direct deletion must remove all three cleanly, and §6 reads the file
//      with the server stopped to prove nothing of it — text or markup —
//      survived.
//   §2 is the phrase the reviewer's INSTRUCTION quotes, so it is where the
//      round's one ask is anchored.
//   §3 and §4 are what the agent REVISES. Two of them, not one, because the
//      agent's return is a REVISION and not an edit: it is several writes over
//      several seconds, and a gate built on one write cannot tell "the file is
//      streamed into the browser as it is saved" from "the file is read once
//      when the round closes".
//   §5 is touched by NOBODY. It is the control for "no text neither side
//      wrote" — a document that came back subtly rewritten by the round trip
//      itself would show here and nowhere else.
//
// `dialer`, `listener` and `resolver` are deliberately different words from
// each other and from anything in §1–2, so a selector that searches the
// document by text can never find two candidates.
const FIXTURE = `# The loop

The **retryBudget** value controls retries in the client.

A second paragraph about the connection pool, which nothing types into.

The third paragraph names the dialer, which the agent will want changed.

The fourth paragraph names the listener, which the agent will want changed too.

The fifth paragraph names the resolver, which nobody touches at all.
`;

const STRIKE = 'The retryBudget value controls';
const ANCHOR = 'the connection pool';
const REVIEWER_ASKED =
  'name the two ends concretely — dialer and listener are not enough';

// THE AGENT'S THREE WRITES, IN THE ORDER IT MAKES THEM. Each is one save of
// the whole .md, which is what an agent editing a file with ordinary tools
// actually does, and each must reach the reviewer's open browser on its own —
// see §4, which waits for the FIRST and the LAST separately.
const AGENT_DIALER = 'the outbound dialer (internal/pool.go)';
const AGENT_LISTENER = 'the inbound listener (internal/accept.go)';
const AGENT_CLOSING = 'Both ends are named in internal/README.md.';
const AGENT_NOTE = 'named both ends and pointed at the files';
const UNTOUCHED =
  'The fifth paragraph names the resolver, which nobody touches at all.';

// --- the agent half ---------------------------------------------------------
//
// One process, three verbs and a file, no model. It runs as a CHILD of the
// reviewer half and reports through a JSONL journal rather than through
// stdout, because the reviewer half has to READ its progress WHILE it is still
// running — "the agent is still blocked" is an assertion, and a pipe that is
// only drained at exit cannot make it.

/** agentMain is the whole agent: arm, wake, read, REVISE THE FILE, ack,
 *  re-arm. It is `galley agent-prompt`'s own instructions, executed — which is
 *  the point of it being here at all, since a loop that answered through some
 *  private channel would certify a protocol nobody is told to use. */
function agentMain(doc, journalPath) {
  const say = (line) =>
    appendFileSync(journalPath, `${JSON.stringify(line)}\n`);
  // stderr is where the trial banner goes, so it is captured and only ever
  // reported on failure. stdout with --json is the payload and nothing else.
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
      // Exit 3 (timed out) and exit 4 (the editor stopped) print nothing on
      // stdout. The exit code is the answer in both cases.
    }
    return { ...r, parsed, ms: Date.now() - started };
  };

  // A backstop, not a policy. A real loop arms with no --timeout at all; this
  // one must not outlive a browser half that died before pressing Revise, or a
  // failing gate would hang instead of failing.
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
    // The whole instruction, field for field, so §3 can pin the SHAPE the
    // agent is handed rather than only its text — including the key it is not
    // given. See the header's finding.
    instructions: handed.map((i) => ({
      text: i.text,
      quote: i.quote,
      key: i.key === undefined ? null : i.key,
    })),
  });

  // `galley pending` AGAIN, ON PURPOSE, AND IT IS A DIFFERENT CLAIM NOW.
  //
  // It used to be here because a loop that trusted the wake would never notice
  // the wake and the endpoint disagreeing. They do not disagree — they answer
  // DIFFERENT QUESTIONS, and an agent that confuses them re-reads an empty
  // round and concludes it was woken for nothing. `pending` means "what has
  // the reviewer got outstanding", and a round that has been SENT is not
  // outstanding: the press moved it to the agent. galley's own MCP prose says
  // this in so many words — *use that captured round; do not replace it with a
  // later galley pending read, because a sent round is no longer pending* —
  // and this is the only place it is asserted from a running loop.
  const read = galley('pending', doc, '--json');
  let view = null;
  try {
    view = JSON.parse(read.out);
  } catch {
    say({ step: 'unreadable', code: read.code, err: read.err.trim() });
    return;
  }
  say({ step: 'read', pending: (view.instructions || []).length });
  if (!handed.length) {
    say({ step: 'nothing-to-answer' });
    return;
  }

  // THE REVISION: THREE WRITES TO THE .md ITSELF, spaced so each is its own
  // save. This is `galley agent-prompt`'s instruction verbatim — *edit the .md
  // file itself, directly, with your normal file tools… galley watches the
  // file and streams every save into the reviewer's browser, so save as often
  // as you like* — and the spacing is the part with a claim on it: three
  // saves that the browser is asserted to have received individually is what
  // makes "streams" a tested word rather than a documented one.
  const rewrite = (from, to) => {
    const before = readFileSync(doc, 'utf8');
    const after = before.replace(from, to);
    writeFileSync(doc, after);
    say({ step: 'wrote', from, to, changed: after !== before });
  };
  const sleep = (ms) =>
    spawnSync(process.execPath, ['-e', `setTimeout(()=>{},${ms})`]);
  rewrite('the dialer', AGENT_DIALER);
  sleep(1200);
  rewrite('the listener', AGENT_LISTENER);
  sleep(1200);
  rewrite('changed too.\n', `changed too.\n\n${AGENT_CLOSING}\n`);
  sleep(1200);

  // THE ACK IS THE SEND — it is what commits the round, once, after every
  // write and never per save. `--changes` annotates what was written; the
  // entries carry NO `answers` array because the wake handed no keys (header),
  // and the prompt permits an entry that names no instruction.
  const acked = galley(
    'ack',
    doc,
    '--state',
    'answered',
    '--note',
    AGENT_NOTE,
    '--changes',
    JSON.stringify([
      { quote: AGENT_DIALER, note: 'Named the outbound end and its file.' },
      { quote: AGENT_LISTENER, note: 'Named the inbound end and its file.' },
    ]),
  );
  say({ step: 'acked', code: acked.code, err: acked.err.trim() });

  // THE RE-ARM, WITH THE FINGERPRINT IT WAS GIVEN — and wait.go's header says
  // in so many words what happens next: the cursor is STALE, because the loop
  // wrote between two waits, so this returns AT ONCE carrying the agent's own
  // revision. One wasted iteration, not a missed wake. The reviewer half
  // asserts that current truth rather than the tidier one.
  say({ step: 'rearming', since: woke.parsed.fingerprint });
  const again = waitFor('--since', woke.parsed.fingerprint, '--timeout', '20s');
  say({
    step: 'rearmed',
    ms: again.ms,
    code: again.code,
    reason: again.parsed ? again.parsed.reason : null,
    fingerprint: again.parsed ? again.parsed.fingerprint : null,
    mine: again.parsed
      ? ((again.parsed.pending || {}).instructions || []).length
      : null,
  });

  // AND THE SAME ARM AGAIN, ON THE CURSOR THAT CAME BACK. This is what settles
  // the loop: with a cursor that describes the document as it now stands, the
  // wait BLOCKS instead of catching up, and reports a plain timeout. Without
  // this the wasted iteration above would be indistinguishable from a loop that
  // spins forever.
  if (again.parsed) {
    const settled = waitFor(
      '--since',
      again.parsed.fingerprint,
      '--timeout',
      '6s',
    );
    say({
      step: 'settled',
      ms: settled.ms,
      code: settled.code,
      err: settled.err.trim(),
    });
  }
  say({ step: 'done' });
}

if (process.argv[2] === '--agent') {
  agentMain(process.argv[3], process.argv[4]);
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

const HERE = mkdtempSync(join(tmpdir(), 'galley-loop-'));
const DOC = join(HERE, 'two-actors-one-document.md');
const JOURNAL = join(HERE, 'agent.jsonl');
writeFileSync(DOC, FIXTURE);
writeFileSync(JOURNAL, '');

const server = spawn(
  GALLEY,
  ['edit', DOC, '--no-open', '--port', String(PORT)],
  {
    stdio: ['ignore', 'pipe', 'pipe'],
  },
);
// THE PIPES ARE DRAINED, AND THAT IS NOT TIDINESS. `stdio: 'pipe'` with no
// reader fills a 64KB kernel buffer and then BLOCKS the writer, which stops
// the server serving mid-run and presents as a hang with nothing to read.
// Printed only when the process DIES, because "the server exited during §4" is
// otherwise indistinguishable from every later fetch failing on its own.
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
  // The browser gets its own line because it is not a child with a signal, and
  // because it was the one thing here only shut down on the happy path: a throw
  // anywhere between launch and §6 left a chromium running. Browser.close is
  // async and an exit handler is not, so the process behind it is killed
  // directly; on a clean run it is already gone and this finds nothing.
  try {
    const proc = browser && browser.process();
    if (proc) proc.kill('SIGKILL');
  } catch {
    // Already gone, or a browser that has no local process. Either way the
    // exit code is what matters.
  }
  for (const child of [agent, server]) {
    try {
      if (child) child.kill('SIGTERM');
    } catch {
      // Already gone; the exit code is what matters.
    }
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

/** journal is everything the agent has said SO FAR. Re-read every time: the
 *  whole point of a file is that a running child's progress is legible without
 *  waiting for it to exit. */
const journal = () =>
  readFileSync(JOURNAL, 'utf8')
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l));
const said = (step) => journal().find((l) => l.step === step) || null;
/** heard blocks until the agent says `step`, or gives up. It returns the line
 *  or null — never throws — so a stalled agent FAILS a check with the journal
 *  in the detail rather than killing the run half way through. */
const heard = async (step, ms) => {
  const started = Date.now();
  for (;;) {
    const line = said(step);
    if (line) return line;
    if (Date.now() - started > ms) return null;
    await new Promise((r) => setTimeout(r, 200));
  }
};
/** heardAll is heard for a step the agent says more than once. Waiting for the
 *  FIRST `wrote` line and then counting them is a race the first draft of this
 *  file lost: the count was taken between two writes and reported one. */
const heardAll = async (step, n, ms) => {
  const started = Date.now();
  for (;;) {
    const lines = journal().filter((l) => l.step === step);
    if (lines.length >= n || Date.now() - started > ms) return lines;
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

// A REAL CLICK, ONCE, BEFORE ANYTHING TYPES. `editor.commands.focus()` inside
// page.evaluate does NOT give the contenteditable DOM focus — activeElement
// stays BODY and the keystroke goes nowhere while the ProseMirror selection
// looks perfectly correct. typing.mjs paid for this discovery; this file only
// has to not repeat it.
await page.click('.ProseMirror');

const pendingLive = async () =>
  await (await fetch(`${base}/_galley/pending`)).json();
const docText = () =>
  page.evaluate(() => window.galleyEdit.editor.state.doc.textContent);

/** select puts the reviewer's selection on a phrase by asking the editor where
 *  that phrase is, and THROWS if the editor did not take focus — because a
 *  swallowed keystroke leaves the document unchanged, which is exactly what
 *  several of the defects this file guards also look like. */
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

// --- §1 the agent arms, and hears nothing --------------------------------

agent = spawn(process.execPath, [process.argv[1], '--agent', DOC, JOURNAL], {
  stdio: ['ignore', 'pipe', 'pipe'],
});
agent.stderr.on('data', (b) => {
  const line = String(b).trim();
  // The trial banner is the ordinary case and says nothing about the loop.
  if (line && !line.startsWith('galley: free trial')) {
    console.log(`      [agent] ${line}`);
  }
});
check(
  'the agent armed on the document',
  !!(await heard('armed', 10000)),
  journal(),
);
// A blocked `galley wait` has to actually reach the server before Revise can
// find it — the endpoint refuses a press with no listener and no --on-revise.
await page.waitForTimeout(1500);

// --- §2 the reviewer works, alone ----------------------------------------

// AN EDIT ACROSS A BOLD RUN, ON A REAL KEYBOARD. One strike, three inlines —
// and under the reviewer's hand it DELETES, directly: no card, no mark,
// nothing of the reviewer's ever pending.
await select(STRIKE);
await page.keyboard.press('Backspace');
await page.waitForTimeout(750);

{
  const text = await docText();
  check(
    "the reviewer's strike across the bold run deleted it, bold word and all",
    !text.includes('retryBudget'),
    text,
  );
  const view = await pendingLive();
  // "Nothing pending" used to mean no proposal awaiting a decision; it means
  // no ASK awaiting an agent now, and the sentence is the same sentence — the
  // reviewer's own hand is in neither population.
  check(
    'and the round is still empty — a deliberate deletion needs no decision and asks nobody',
    view.instructions.length === 0,
    view.instructions.map((i) => [i.quote, i.text]),
  );
}

// AN INSTRUCTION, THROUGH THE COMPOSER THE REVIEWER ACTUALLY USES. Not a POST
// to `/_galley/instruct`: this half of the loop is the reviewer, and a check
// that reached for a transport to make the reviewer's mark would be testing
// the wrong actor.
await select(ANCHOR);
await page.waitForTimeout(300);
await page.click('.gly-composer .gly-comment-button');
await page.fill('.gly-composer .gly-composer-text', REVIEWER_ASKED);
await page.click('.gly-composer .gly-composer-send');
await page.waitForTimeout(1000);

const instruction = await (async () => {
  const view = await pendingLive();
  return view.instructions.find((i) => i.quote === ANCHOR) || null;
})();
check(
  "the reviewer's instruction is anchored on the phrase they selected",
  !!instruction,
  (await pendingLive()).instructions,
);
check(
  'and it carries what the reviewer said, word for word',
  !!instruction && instruction.text === REVIEWER_ASKED,
  instruction,
);
// THE CARD IS ON SCREEN AND CARRIES ONE SIDE, which is the whole shape of an
// instruction: it is a fact of a round, not a conversation. Asserted rather
// than assumed, because the thread card component is shared with review mode's
// two-sided one and a reply box appearing here would be the product quietly
// growing a conversation back.
//
// ONE SIDE IS THE CLAIM, AND IT IS NOT THE SAME CLAIM AS ONE VERB — this check
// asserted both under one name and the second half went stale the day the rail
// gained `edit`. A reviewer's own words must be revisable, so an instruction
// card carries edit at settle weight and delete at destroy weight; what would
// mean the conversation had grown back is a REPLY BOX (`reply === false`) and a
// second speaker in `said`, both of which are still asserted exactly. The verbs
// are pinned as a pair rather than by count so that a third one arriving is a
// decision somebody has to come here and make.
const railCard = await page.evaluate(() => {
  const el = document.querySelector('.gly-rail-band .gly-thread');
  if (!el) return null;
  return {
    said: Array.from(el.querySelectorAll('.gly-thread-entry p')).map(
      (p) => p.textContent,
    ),
    reply: !!el.querySelector('[data-draft]'),
    verbs: Array.from(el.querySelectorAll('.gly-card-actions button')).map(
      (b) => (b.firstChild ? b.firstChild.textContent : '').trim(),
    ),
  };
});
check(
  'and it is a card in the rail carrying ONE side — an instruction is not a conversation',
  !!railCard &&
    railCard.said.length === 1 &&
    railCard.said[0] === REVIEWER_ASKED &&
    railCard.reply === false &&
    JSON.stringify(railCard.verbs) === JSON.stringify(['edit', 'delete']),
  railCard,
);

// THE AGENT HAS HEARD NONE OF IT. Two writes that reached the document — a
// direct deletion and an instruction — and a blocked `wait` stays blocked. The
// deletion is the sharper half: the spec says a direct edit changes no pending
// state and therefore wakes nobody, because a decision needs no response. This
// is the half of the contract no other gate can state, because stating it
// needs a second actor that is demonstrably alive and demonstrably silent.
check(
  'the agent is STILL blocked after two reviewer writes — nothing wakes it but Revise',
  !!said('armed') && !said('woke') && !said('gave-up'),
  journal(),
);

// --- §3 Revise, and the agent is handed the round ------------------------

// WITH WORK PENDING THE PRESS DISCLOSES, IT DOES NOT POST (the verdict
// button's two exits — see verdict.ts's askRevise). This file predated that menu
// and clicked once, which opened the disclosure and woke nobody: the agent's
// two-minute backstop expired and every later section died on a null run. The
// plain revise is the menu's first exit, so the reviewer's gesture is the two
// clicks it really is.
await page.click('#gly-revise');
await page.click('.gly-verdict-menu .gly-verdict-revise');

const woke = await heard('woke', 20000);
check(
  'pressing Revise woke the agent that was blocked on the document',
  !!woke && woke.reason === 'revise',
  woke || journal(),
);
{
  const handed = (woke && woke.instructions) || [];
  check(
    "and the agent was handed the reviewer's ask, with the words it quotes",
    handed.length === 1 &&
      handed[0].text === REVIEWER_ASKED &&
      handed[0].quote === ANCHOR,
    handed,
  );
  // AND ITS KEY, which this check spent a phase asserting the ABSENCE of.
  // The old form pinned the defect deliberately — `reviewerInstructions`
  // set Text, Quote and At only, so a `galley wait` session could not name
  // the instruction its change answered though `galley agent-prompt` told it
  // to — and said "fixing it turns this check red and makes somebody read
  // this comment". It did, and this is that reader.
  //
  // What the red actually caught is worth keeping: the fix that landed first
  // taught `pending()` and the offline path to carry the key and MISSED THIS
  // BUILDER, which is the one the round takes. The check went red not because
  // the key had arrived but because aliasing the CLI struct to the server's
  // made the empty field serialize as "" rather than vanish — a different
  // failure from the one being pinned, and the reason the gap was visible at
  // all. A gate that pins a defect earns its keep by failing for the wrong
  // reason.
  check(
    'and its key, so the agent can name the instruction its change answered',
    handed.length === 1 &&
      typeof handed[0].key === 'string' &&
      handed[0].key.length > 0,
    handed,
  );
}
{
  // THE PRESS CLEARED THE ROUND, and the agent's own second read agrees. Two
  // observations of one fact from opposite sides of the wire: the reviewer's
  // rail is empty, and `galley pending` — run by the child, after the wake —
  // reports nothing outstanding. An agent that re-reads `pending` instead of
  // using the round it was handed sees exactly this and concludes it was woken
  // for nothing.
  const view = await pendingLive();
  check(
    'Revise sent the round — the reviewer has nothing outstanding',
    view.instructions.length === 0,
    view.instructions.map((i) => i.text),
  );
  const read = await heard('read', 15000);
  check(
    "and a SENT round is no longer pending, which is what the agent's second read reports",
    !!read && read.pending === 0,
    read || journal(),
  );
}
// THE DOCUMENT IS THE AGENT'S WHILE THE ROUND IS OUT, and the lock is a fact
// of the product rather than a courtesy: seal.ts runs
// `setEditable(!this.handoff)`. It is asserted here because it is what
// replaced the reply-draft hazard this file used to stage — there is no
// in-flight reviewer work for the agent's writes to destroy, and that is a
// stronger guarantee than carrying it across.
await page
  .waitForFunction(() => window.galleyEdit.editor.isEditable === false, null, {
    timeout: 15000,
  })
  .catch(() => {});
check(
  'and the document is not editable while the round is with the agent',
  (await page.evaluate(() => window.galleyEdit.editor.isEditable)) === false,
);

// --- §4 the agent revises the FILE, and the reviewer watches it land ------
//
// THE ASSERTION THAT REPLACED THE PROPOSAL CARDS, and it is a bigger claim
// than they were. The agent no longer hands the reviewer something to decide;
// it EDITS THE .md, and galley watches that file and streams every save into
// the open browser. So the thing to prove is that a write made by another
// process, to a file on disk, appears in the reviewer's live document — and
// that it does so PER SAVE rather than once at the end, which is the whole of
// what `agent-prompt`'s *save as often as you like* promises.
//
// EACH WRITE IS WAITED FOR SEPARATELY AND THE FIRST ONE IS WHY. A check that
// only waited for the last write would pass identically on a build that
// imported the file once, when the round closed — and a reviewer watching a
// document that does not move for thirty seconds has no way to tell a working
// agent from a dead one.
{
  const wrote = await heardAll('wrote', 1, 20000);
  check(
    "the agent's first write reached the file",
    wrote.length >= 1 && wrote[0].changed,
    wrote,
  );
  await page
    .waitForFunction(
      (want) => window.galleyEdit.editor.state.doc.textContent.includes(want),
      AGENT_DIALER,
      { timeout: 20000 },
    )
    .catch(() => {});
  check(
    "and the FIRST save is in the reviewer's open document, before the round is committed",
    (await docText()).includes(AGENT_DIALER) && !said('acked'),
    { text: (await docText()).slice(0, 200), acked: said('acked') },
  );

  const all = await heardAll('wrote', 3, 25000);
  check(
    'the agent made all three writes to the .md',
    all.length === 3 && all.every((w) => w.changed),
    all,
  );
  await page
    .waitForFunction(
      (want) => window.galleyEdit.editor.state.doc.textContent.includes(want),
      AGENT_CLOSING,
      { timeout: 25000 },
    )
    .catch(() => {});
  const text = await docText();
  check(
    "and every one of them landed in the reviewer's browser — the file streams in as it is saved",
    text.includes(AGENT_DIALER) &&
      text.includes(AGENT_LISTENER) &&
      text.includes(AGENT_CLOSING),
    text.slice(0, 400),
  );
  // AND THE REVIEWER'S OWN HAND SURVIVED ALL THREE. The agent's saves are
  // whole-file imports and every one of them replaces the browser's document
  // (ydoc.Load deletes the fragment's children and writes them again), so the
  // reviewer's strike from §2 crosses three rebuilds made by somebody else.
  // This is the closest thing left to the destroyed-draft finding, and unlike
  // it, it is about work that was COMMITTED rather than in flight.
  check(
    "and the reviewer's own deletion survived all three rebuilds",
    !text.includes('retryBudget'),
    text.slice(0, 200),
  );
}

// --- §5 the ack commits the round, and the agent re-arms -----------------

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
    .catch(() => {});
  check(
    'and the document came back to the reviewer — editable again once the round closed',
    (await page.evaluate(() => window.galleyEdit.editor.isEditable)) === true,
  );
}

// THE RE-ARM, AND THE CAVEAT wait.go PUTS IN ITS OWN HEADER. The agent wrote
// between two waits, so the fingerprint it was handed no longer describes the
// document and the next wait catches up IMMEDIATELY on the agent's own work.
// One wasted iteration, not a missed wake, and it is the CURRENT truth, so it
// is what is asserted. The fix belongs to the mutating verbs printing a
// fingerprint, not here.
{
  const rearmed = await heard('rearmed', 30000);
  // `changed`, NOT `settle`, AND THE WORD IS THE POINT. This catch-up is
  // nobody's settle: the loop's own writes moved the fingerprint past the
  // cursor in hand, and the server keeps no record of what moved it — it used
  // to answer `settle` here, which claims the document settled in ● live mode,
  // on a session that is `on ask` and where nothing settles a waiter at all.
  // The same word carried a missed approve or discard, and an agent answering
  // a sealed review collects 409s on every verb.
  check(
    're-arming on the fingerprint it was GIVEN returns at once — the stale cursor wait.go documents',
    !!rearmed && rearmed.reason === 'changed' && rearmed.ms < 3000,
    rearmed || journal(),
  );
  // A WAKE IS A SNAPSHOT, NOT A DELTA, and this is the check that says so.
  //
  // The composition it asserts inverted with the product and the SENTENCE did
  // not. It used to be three — the agent's two proposals and the reviewer's
  // comment — because a wake carries the WHOLE pending view rather than what
  // is new. The whole pending view is now the reviewer's outstanding round,
  // and the round was SENT by the press that woke this loop, so the honest
  // number is ZERO: the catch-up hands the agent a document that moved and no
  // ask at all. A loop author who reads a wake as "here is what needs
  // answering" answers nothing here and answers the same round twice when it
  // is not empty; this is the check that would tell them.
  check(
    'and the wake carries the WHOLE pending view, which after a sent round is empty',
    !!rearmed && rearmed.mine === 0,
    rearmed,
  );
  const settled = await heard('settled', 25000);
  // Exit 3 is `--timeout` elapsed. The loop settles rather than spinning: with
  // a cursor that describes the document as it stands, the wait BLOCKS.
  check(
    'and re-arming on the cursor that came back BLOCKS — the loop settles, it does not spin',
    !!settled && settled.code === 3 && settled.ms >= 5500,
    settled || journal(),
  );
}

await browser.close();

// --- §6 THE DOCUMENT OF RECORD, WITH NOTHING RUNNING ---------------------
//
// The assertion no other gate makes. layers, motion and typing all end by
// asking a live server or a live browser what it thinks; both of those are the
// session's opinion of the file, and `brownred` and the untracked keystroke
// both lived in the gap between that opinion and the bytes. So the server is
// STOPPED first, and everything below reads the file, the sidecar beside it,
// the committed round log under it, and galley's own offline parse of the same
// bytes.

server.kill('SIGTERM');
await new Promise((r) => setTimeout(r, 1500));

const settledDoc = readFileSync(DOC, 'utf8');

/** spansIn lists every CriticMarkup span in the file with its kind, so a claim
 *  about "what is left in the document" is made over ALL of them rather than
 *  over the ones the check happened to look for. */
const spansIn = (text) =>
  [...text.matchAll(/\{(--|\+\+|~~|==|>>)([\s\S]*?)(--|\+\+|~~|==|<<)\}/g)].map(
    (m) => ({ kind: m[1], body: m[2] }),
  );

{
  // NOTHING IS TRACKED, BY EITHER HAND. The reviewer's strike applied
  // directly, and the agent wrote ordinary Markdown into the file — so the
  // document ends this run with NO CriticMarkup span of any kind. Under the
  // tracked build this same run ended with three `{--}` markers (the
  // serializer breaking the reviewer's strike around the bold wrapper — Gap 7)
  // and a proposal awaiting a decision; both are the OLD truth, and either
  // reappearing means a tracking layer grew back.
  //
  // IT IS ALL SPANS AND NOT `{--` ALONE, which is what the two deleted
  // live/offline deletion-count checks were reaching for through a population
  // that no longer has kinds. PRINTED, NOT ONLY ASSERTED: `check` shows its
  // detail on failure alone, and the count this section turns on stays legible
  // on a green run too.
  const spans = spansIn(settledDoc);
  console.log(
    `      [file] ${spans.length} span(s)${spans.length ? `: ${spans.map((s) => s.kind).join(' ')}` : ''}`,
  );
  check(
    'the settled file holds NO CriticMarkup of any kind — neither hand is tracked',
    spans.length === 0,
    spans,
  );
}

{
  // NO TEXT NEITHER SIDE WROTE, and each side's own work present as plain
  // text. The struck phrase is gone without residue, the paragraph it lived in
  // survives, and the one paragraph nobody touched is byte-for-byte what the
  // fixture wrote — a round trip that quietly rewrote the document would show
  // there and nowhere else.
  check(
    "the reviewer's strike is applied in the file — phrase gone, paragraph kept, no residue",
    !settledDoc.includes('retryBudget') &&
      settledDoc.includes('retries in the client.'),
    settledDoc.split('\n').filter((l) => l.includes('retries')),
  );
  check(
    "the agent's revision is in the file, in the agent's own words, decided by nobody",
    settledDoc.includes(AGENT_DIALER) &&
      settledDoc.includes(AGENT_LISTENER) &&
      settledDoc.includes(AGENT_CLOSING) &&
      !settledDoc.includes('the dialer,') &&
      !settledDoc.includes('the listener,'),
    settledDoc.split('\n').filter((l) => /dialer|listener|README/.test(l)),
  );
  check(
    'and the paragraph NOBODY touched is byte-identical to the fixture',
    settledDoc.includes(UNTOUCHED),
    settledDoc.split('\n').filter((l) => /resolver/.test(l)),
  );
}

// THE SIDECAR IS NOT READ HERE ANY MORE, AND THE CHECK THAT READ IT IS GONE.
//
// It asserted the reviewer's words on disk in the reviewer's name — the
// sidecar being "the ONLY record of who suggested what" — and it ran RED on
// this repair with `{"unreadable": "missing"}`. Measured rather than assumed:
// `galley edit` writes no `<doc>.comments.json` AT ALL, at any point in a
// round. Open a document, file an instruction, press Revise, and the directory
// holds the .md and the runtime `.serve.json` and nothing else.
//
// So this file has no sidecar claim to make, and inventing one would be a
// count kept up. THE ASK'S DURABLE HOME IS THE ROUND LOG, which is read
// directly below: `rounds.jsonl` carries the reviewer's press with its `asks`
// array, and that is where the pairing this gate exists to prove actually
// lives. The check that used to be here has moved its subject rather than its
// wording — "the reviewer's ask is on disk" is asserted, from the file that
// holds it.
//
// (Whether edit mode SHOULD still write a sidecar is a product question this
// gate is not the place to answer. It is recorded here because nothing else
// records it.)

{
  // THE ROUNDS, READ OFF THE LOG WITH NOTHING RUNNING. This is where the two
  // actors are paired: the reviewer's press is a round carrying the ASK, the
  // agent's ack is a round carrying what it says it did, and the history is
  // the only place that pairing is durable. Reading it here — from the file
  // rather than from a running server's answer — is what makes it a fact about
  // the record instead of about the session.
  const roundsFile = join(
    HERE,
    '.galley',
    'versions',
    'two-actors-one-document.md',
    'rounds.jsonl',
  );
  let cut = [];
  try {
    cut = readFileSync(roundsFile, 'utf8')
      .trim()
      .split('\n')
      .map((l) => JSON.parse(l));
  } catch {
    // Left empty on purpose — the check reports the path it could not read.
  }
  const asked = cut.filter((r) => r.reason === 'revise').pop();
  const landed = cut.filter((r) => r.reason === 'landed').pop();
  check(
    "the reviewer's press cut a round that carries the ask",
    !!asked &&
      (asked.authors || []).includes('court') &&
      (asked.asks || []).some((a) => a.text === REVIEWER_ASKED),
    { roundsFile, reasons: cut.map((r) => r.reason), asked },
  );
  check(
    "and the agent's ack cut a round naming the agent and carrying what it said it did",
    !!landed &&
      (landed.authors || []).includes('agent') &&
      (landed.instruction || '').includes(AGENT_NOTE),
    landed,
  );
  check(
    "and the agent's annotations are on that round, one per change it made",
    !!landed &&
      (landed.changes || []).length === 2 &&
      (landed.changes || []).some((c) =>
        (c.locator || '').includes('outbound dialer'),
      ) &&
      (landed.changes || []).some((c) =>
        (c.locator || '').includes('inbound listener'),
      ),
    landed && landed.changes,
  );
  // AND THE ORDER, which is the pairing stated as a fact about the list: the
  // ask was cut before the answer. A history that recorded the agent's work
  // inside the reviewer's round is the defect this ordering exists to refuse,
  // and it is invisible to any check that reads the two rounds separately.
  check(
    "and the ask was cut BEFORE the answer — a round holds one actor's send",
    !!asked && !!landed && asked.n < landed.n,
    cut.map((r) => [r.n, r.reason, (r.authors || []).join('+')]),
  );
}

{
  // GALLEY'S OWN OFFLINE READ OF THE SAME BYTES. The live and offline paths
  // must agree about a document nobody is serving, and here they agree at
  // zero: the round was sent and answered, so there is nothing outstanding,
  // and the file holds no markup for an offline parse to find work in.
  const offline = spawnSync(GALLEY, ['pending', DOC, '--json'], {
    encoding: 'utf8',
  });
  let parsed = null;
  try {
    parsed = JSON.parse(offline.stdout);
  } catch {
    // Left null on purpose — the check below reports the stderr instead.
  }
  check(
    "and galley's own offline read of the settled bytes reports nothing outstanding",
    !!parsed &&
      Array.isArray(parsed.instructions) &&
      parsed.instructions.length === 0,
    { parsed, err: offline.stderr.trim() },
  );
}

{
  // THE LOOP RAN TO THE END. Everything above could pass with an agent that
  // died after its last useful write, and a gate that cannot tell a finished
  // loop from an abandoned one is not watching the loop.
  const done = await heard('done', 5000);
  check(
    'the agent ran the whole round and exited on its own',
    !!done,
    journal(),
  );
}

console.log(
  failures === 0 ? '\nall loop checks passed' : `\n${failures} FAILED`,
);
process.exit(failures === 0 ? 0 : 1);
