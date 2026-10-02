// preflight.mjs — A SESSION THAT STARTS CORRECT, OR DOES NOT START.
//
// `just review <doc.md>` is one command that starts a paired review session and
// REFUSES TO PRINT THE URL if the loop does not work.
//
// WHY THIS EXISTS, and it is not a product defect. In galley's first real review
// session the reviewer lost time to three things, and TWO WERE THE
// COORDINATOR'S HAND-ASSEMBLY: `galley edit` was started with no `--on-revise`,
// so pressing Revise failed into a status readout too narrow to read (Gap 1 and
// Gap 3); and the agent side was a hand-rolled one-shot `while` loop, so the
// reviewer's SECOND press vanished into a dead watcher. A reviewer's attention
// is the scarcest thing this project has, and it was spent discovering that the
// loop was wired wrong.
//
// HOW THIS DIFFERS FROM `just loop`, because the difference is the whole point.
// loop.mjs proves the loop works IN GENERAL, on a synthetic four-paragraph
// fixture, as a regression check that runs on any build. This proves it works
// HERE — this document, this configuration, this machine, this binary — in the
// sixty seconds before a human touches it. A green `just loop` on a build whose
// `just assets` was never run says nothing about the document the reviewer is
// about to open.
//
// THE FIVE CHECKS, and they run in this order because each needs the last:
//
//   1  the server is up and the document RENDERS, every block kind of it, with
//      the pending count the file actually holds;
//   2  a Revise press reaches a listener and the revision window OPENS AND
//      CLOSES — twice, because the reviewer's second press is the one that
//      vanished, and Gap 1's failure state is precisely what a reviewer cannot
//      diagnose from the strip;
//   3  an agent reply lands and APPEARS on the reviewer's screen;
//   4  a draft in a reply box SURVIVES a server-side mutation — the defect that
//      cost the most in the first session;
//   5  the file on disk is UNCHANGED by the check itself.
//
// CHECK 5 IS WHAT MAKES THIS SAFE TO RUN, and it is why checks 1-4 run against a
// BYTE-IDENTICAL COPY in a temporary directory rather than against the file at
// its own path.
//
// The honest reasoning, stated rather than assumed. Checks 2-4 have to MUTATE:
// there is no way to prove a Revise press reaches a listener without pressing
// Revise, and no way to prove a reply appears without writing one. Undoing that
// afterwards is not achievable byte-for-byte — thread keys and run tokens are
// random, the sidecar may not have existed at all, and the projection is
// debounced, so "write the original bytes back" races the thing that is writing
// them. A pre-flight that left a stray span or a sidecar entry behind would have
// damaged the document it was protecting. So the reviewer's file is only ever
// READ (hashed), the copy is what gets driven, and check 5 asserts the original
// and its sidecar are byte-identical afterwards — with the copy's own change as
// the non-vacuity guard, because "nothing changed anywhere" would pass check 5
// while proving nothing.
//
// WHAT THAT COSTS, said out loud: the copy has the same BYTES but not the same
// PATH, so a path-shaped problem — an unreadable sidecar, a symlink, a
// permission — is not covered. Check 1 runs a second time against the real
// session before the URL is printed, which is where a path-shaped problem
// surfaces.
//
// galley never calls a model and never pays for tokens. The agent side here is
// the CLI verbs and nothing else — `wait`, `pending`, `blocks`, `reply`,
// `suggest`.
//
// A GREEN PRE-FLIGHT IS NOT A WORKING PRODUCT. The closing line this prints
// says so to the reviewer, and it is not modesty. Task 9 measured it: on a build
// with a genuinely broken client run stamp, 28 of `just loop`'s 30 remaining
// checks still passed. A gate protects what we have already learned to assert;
// it cannot notice that the design does not read as one product, and every
// finding of real value in this project came from a human noticing something
// felt wrong. A pre-flight that implied otherwise would train the reviewer to
// trust it, which is the opposite of useful.
//
// Running it:
//
//   just review docs/thing.md
//   GALLEY=bin/galley node web/preflight.mjs docs/thing.md
//
// Modes (both are re-entries of this same file, the way loop.mjs re-enters
// itself as --agent):
//
//   --agent <doc> <journal>   the pre-flight's scripted stand-in: two rounds of
//                             wake, read, answer, re-arm. No model.
//   --watch <doc> <journal>   the SESSION watcher: the re-arming loop that the
//                             first session hand-rolled one-shot. It records
//                             every wake and never exits on one.
//
// It needs a chromium for playwright to drive: `npx playwright install
// chromium` once, or GALLEY_CHROME=<executable>.

import { spawn, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import {
  appendFileSync,
  copyFileSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { basename, extname, join, resolve } from 'node:path';

const GALLEY = process.env.GALLEY || 'bin/galley';

// FAULT INJECTION, AND WHY IT IS IN THE SHIPPED FILE.
//
// A check that has never been seen to fail is a check nobody knows the meaning
// of. Three of the five here can only be made to fail by breaking the RUNTIME —
// unsetting the hook, silencing the agent, driving the wrong file — and doing
// that by hand each time means the proof is a story rather than a command.
//
// Nothing sets these in normal use, an active one is printed in red-hot plain
// English at the top of the run, and `in-place` refuses to touch a document
// outside a temporary directory. The other two — a block kind the client cannot
// build (check 1) and a draft the rail does not carry (check 4) — are NOT here,
// because faking them would prove the harness rather than the product; they are
// demonstrated against real builds with those features removed. See
// task-10-report.md.
//
//   no-hook    start the pre-flight server with no --on-revise and arm no
//              agent, so a press has nothing to hand the revision to. Gap 1's
//              failure state exactly, and what the first real session hit.
//   one-shot   the agent takes ONE wake and exits — the hand-rolled `while`
//              loop this whole command exists because of.
//   mute       the agent wakes and reads and does NOT answer, so the revision
//              window opens and never closes.
//   in-place   drive the reviewer's own file instead of the copy. The mistake
//              check 5 exists to catch.
const BREAK = process.env.GALLEY_PREFLIGHT_BREAK || '';

// 8251-8254 are the four browser gates (motion, layers, typing, loop). The
// pre-flight takes the next one so `just review` can run while a gate is
// running. The SESSION port is pinned here rather than inherited: galley's own
// default is now a FREE port (two reviews at once collided on the old fixed
// 8123, and an unguessable port also denies a local page something to aim at),
// but this gate has to know where to look, so it asks for one explicitly.
const PREFLIGHT_PORT = Number(process.env.GALLEY_PREFLIGHT_PORT || 8255);
const SESSION_PORT = Number(process.env.GALLEY_REVIEW_PORT || 8123);

// The reviewer's question, and the two answers. The agent finds its thread by
// matching REVIEWER_ASKED exactly, which is discovery through `galley pending`
// — the real contract — rather than a key handed to it out of band.
const REVIEWER_ASKED = 'galley preflight: does this anchor still hold?';
const AGENT_ANSWERED = [
  'galley preflight: round 1 — the agent woke on Revise and answered here.',
  'galley preflight: round 2 — the SECOND press woke it too, which is the whole point.',
];
const AGENT_NOTED = 'galley preflight: the agent can write to this document.';
const DRAFT = 'and while I have you, the second thing';
// Mid-sentence, not at the end: focusing a textarea puts the caret at the end
// on its own, so a check that left it there could not tell a carried selection
// from a browser default.
const CARET = 9;

const galley = (...args) => {
  const r = spawnSync(GALLEY, args, { encoding: 'utf8' });
  return { code: r.status, out: r.stdout || '', err: r.stderr || '' };
};

/** galleyJSON runs a --json verb and returns the parsed payload, or null. */
const galleyJSON = (...args) => {
  const r = galley(...args);
  try {
    return JSON.parse(r.out);
  } catch {
    return null;
  }
};

/** waitOnce is ONE `galley wait`. Exit 3 (--timeout elapsed) and exit 4 (the
 *  editor stopped) print nothing on stdout; the exit code is the whole answer
 *  in both cases. */
function waitOnce(doc, since, extra = []) {
  const args = ['wait', doc, '--json'];
  if (since) {
    args.push('--since', since);
  }
  const started = Date.now();
  const r = galley(...args, ...extra);
  let parsed = null;
  try {
    parsed = JSON.parse(r.out);
  } catch {
    // Nothing on stdout — see above.
  }
  return { ...r, parsed, ms: Date.now() - started };
}

// THE RE-ARM, AND WHY IT IS ONE FUNCTION SHARED BY BOTH MODES.
//
// The hand-rolled watcher's bug was not that it was hand-rolled. It was that it
// was ONE-SHOT: it took a wake and exited, so the reviewer's second press
// arrived at nothing. Every correct loop over `galley wait` has the same three
// obligations, and having one implementation of them is the only way the
// pre-flight can claim to be testing what the session runs.
//
//   1  RE-ARM IMMEDIATELY, so the window in which a press has no reader is a
//      few milliseconds rather than a whole round of work.
//   2  CARRY THE FINGERPRINT FORWARD, because a caller that drops --since
//      loses anything that happens in that window. A wake reports the
//      fingerprint the server is currently at; that is the next arm's cursor.
//   3  SKIP THE SETTLES. Re-arming on a cursor the loop's own writes have moved
//      past returns AT ONCE with reason `changed` — the stale cursor wait.go's
//      header documents. It is one wasted iteration, not a missed wake, and the
//      loop settles on the cursor that comes back. A loop that treated it as a
//      wake would answer the reviewer twice.
//
// It never exits on a wake. It exits when the EDITOR stops (exit 4) or when the
// caller says it has had enough.
function armLoop(doc, { onWake, onSettle, onGiveUp, timeout, stopAfter }) {
  let since = '';
  let revises = 0;
  let settles = 0;
  for (;;) {
    const extra = timeout ? ['--timeout', timeout] : [];
    const w = waitOnce(doc, since, extra);
    if (w.code === 4 || (w.parsed && w.parsed.reason === 'closed')) {
      return { reason: 'editor-stopped', revises, settles };
    }
    if (!w.parsed) {
      if (onGiveUp) {
        onGiveUp(w);
      }
      return {
        reason: w.code === 3 ? 'timed-out' : 'unreadable',
        revises,
        settles,
      };
    }
    since = w.parsed.fingerprint;
    if (w.parsed.reason === 'revise') {
      revises += 1;
      onWake(w, revises);
      if (stopAfter && revises >= stopAfter) {
        return { reason: 'done', revises, settles };
      }
      continue;
    }
    // A settle (● live mode reporting quiet) or a `changed` (the catch-up
    // branch answering a cursor this loop's own writes moved past, which names
    // no event because the server keeps none). Neither is a press.
    settles += 1;
    if (onSettle) {
      onSettle(w, settles);
    }
    // A pathological guard, not a policy. A reviewer typing continuously in
    // ● live mode produces settles indefinitely and that is correct; a run of
    // them with no press at all in a bounded pre-flight is a wedge.
    if (settles > 200) {
      return { reason: 'settle-storm', revises, settles };
    }
  }
}

// --- the pre-flight's agent: two rounds, four verbs, no model ---------------
//
// It runs as a CHILD of the reviewer half and reports through a JSONL journal
// rather than through stdout, because the reviewer half has to read its
// progress WHILE it is still running — "the agent is still blocked" is an
// assertion, and a pipe drained only at exit cannot make it.

function agentMain(doc, journalPath) {
  const say = (line) =>
    appendFileSync(journalPath, `${JSON.stringify(line)}\n`);
  say({ step: 'armed' });

  const answer = (w, round) => {
    say({
      step: 'woke',
      round,
      reason: w.parsed.reason,
      fingerprint: w.parsed.fingerprint,
      suggestions: (w.parsed.pending.suggestions || []).length,
      threads: (w.parsed.pending.comments || []).length,
    });

    // Asked TWICE on purpose. The wake already carried the pending view, and a
    // loop that trusted it would never notice the two disagreeing — which is a
    // thing only a second question can see.
    const view = galleyJSON('pending', doc, '--json');
    if (!view) {
      say({ step: 'unreadable', round });
      return;
    }
    const thread = (view.comments || []).find((t) =>
      (t.entries || []).some((e) => e.text === REVIEWER_ASKED),
    );
    say({
      step: 'read',
      round,
      threads: (view.comments || []).length,
      suggestions: (view.suggestions || []).length,
      key: thread ? thread.key : null,
    });
    if (!thread) {
      say({ step: 'nothing-to-answer', round });
      return;
    }

    if (BREAK === 'mute') {
      say({ step: 'muted', round });
      return;
    }

    const replied = galley('reply', doc, thread.key, AGENT_ANSWERED[round - 1]);
    say({
      step: 'replied',
      round,
      key: thread.key,
      code: replied.code,
      said: replied.out.trim(),
    });

    if (round === 1) {
      // ONE WRITE THAT IS NOT A REPLY, so the loop is not proved only over the
      // sidecar. A block note reaches the .md itself, and the block key comes
      // from `galley blocks` — the agent discovers its own target rather than
      // being handed one.
      const blocks = galleyJSON('blocks', doc, '--json') || [];
      const target = blocks.find((b) => b.kind === 'paragraph') || blocks[0];
      if (target) {
        const noted = galley(
          'suggest',
          doc,
          '--comment',
          AGENT_NOTED,
          '--on-block',
          target.key,
        );
        say({
          step: 'noted',
          round,
          block: target.key,
          code: noted.code,
          said: noted.out.trim(),
        });
      } else {
        say({ step: 'no-block-to-note', round });
      }
    }
  };

  // A backstop, not a policy. A real session's watcher arms with no --timeout
  // at all; this one must not outlive a browser half that died before pressing
  // Revise, or a failing gate would hang instead of failing.
  const end = armLoop(doc, {
    onWake: answer,
    onSettle: (w, n) =>
      say({ step: 'settled-through', n, fingerprint: w.parsed.fingerprint }),
    onGiveUp: (w) => say({ step: 'gave-up', code: w.code, err: w.err.trim() }),
    timeout: '180s',
    stopAfter: BREAK === 'one-shot' ? 1 : 2,
  });
  say({ step: 'done', ...end });
}

// --- the session watcher: the loop the first session hand-rolled one-shot ----
//
// It writes to a journal AND to stdout, and it is deliberately NOT an agent: it
// never answers the review. galley calls no model, so the thing that answers is
// the paired session that is already here, running its own `galley wait`. Two
// waiters do not compete — wakeWaiters fans out to every registered reader — so
// this one's job is to be the RECORD that the press happened, which is what the
// first session had nowhere durable to keep (Gap 3).

function watchMain(doc, journalPath) {
  const say = (line) => {
    appendFileSync(journalPath, `${JSON.stringify(line)}\n`);
    if (line.step === 'wake') {
      const n = line.n;
      process.stdout.write(
        `\n  ● Revise #${n} — ${line.suggestions} pending suggestion(s), ` +
          `${line.threads} thread(s)\n    read it   galley pending ${doc}\n\n`,
      );
    }
  };
  say({ step: 'armed' });
  const end = armLoop(doc, {
    onWake: (w, n) =>
      say({
        step: 'wake',
        n,
        at: new Date().toISOString(),
        fingerprint: w.parsed.fingerprint,
        suggestions: (w.parsed.pending.suggestions || []).length,
        threads: (w.parsed.pending.comments || []).length,
      }),
    onSettle: (w, n) =>
      say({ step: 'settle', n, fingerprint: w.parsed.fingerprint }),
    onGiveUp: (w) => say({ step: 'gave-up', code: w.code, err: w.err.trim() }),
  });
  say({ step: 'stopped', ...end });
  if (end.reason === 'editor-stopped') {
    process.stdout.write('\n  the editor stopped — the watcher is done\n');
  }
}

if (process.argv[2] === '--agent') {
  agentMain(process.argv[3], process.argv[4]);
  process.exit(0);
}
if (process.argv[2] === '--watch') {
  watchMain(process.argv[3], process.argv[4]);
  process.exit(0);
}

// --- the reviewer half ------------------------------------------------------

const sha = (path) =>
  createHash('sha256').update(readFileSync(path)).digest('hex');
const sidecarOf = (doc) =>
  `${doc.slice(0, doc.length - extname(doc).length)}.comments.json`;
const runtimeOf = (doc) =>
  `${doc.slice(0, doc.length - extname(doc).length)}.serve.json`;

/** snapshot is everything about a path that check 5 will compare: its bytes,
 *  its size, and whether it was there at all. A file that did not exist and now
 *  does is exactly as much of a change as a file whose bytes moved. */
const snapshot = (path) =>
  existsSync(path)
    ? { present: true, sha: sha(path), size: statSync(path).size }
    : { present: false, sha: null, size: null };

const sameSnapshot = (a, b) =>
  a.present === b.present && a.sha === b.sha && a.size === b.size;

const portFree = (port) =>
  new Promise((res) => {
    const s = createServer();
    s.once('error', () => res(false));
    s.once('listening', () => s.close(() => res(true)));
    s.listen(port, '127.0.0.1');
  });

const waitForServer = async (base, ms) => {
  const started = Date.now();
  for (;;) {
    try {
      const r = await fetch(`${base}/`);
      if (r.ok) {
        return true;
      }
    } catch {
      // Not up yet.
    }
    if (Date.now() - started > ms) {
      return false;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
};

// --- reporting --------------------------------------------------------------
//
// Grouped under the five checks the brief names, because a flat list of oks is
// a thing a reviewer skims. The heading is what failed; the lines under it are
// why.

let failures = 0;
let group = null;
function section(n, title) {
  group = { n, title, failed: 0 };
  console.log(`\n${n}  ${title}`);
}
function check(name, ok, detail) {
  if (ok) {
    console.log(`   ok    ${name}`);
    return true;
  }
  failures += 1;
  if (group) {
    group.failed += 1;
  }
  console.log(`   FAIL  ${name}`);
  if (detail !== undefined) {
    for (const line of JSON.stringify(detail, null, 1).split('\n')) {
      console.log(`         ${line}`);
    }
  }
  return false;
}
function note(text) {
  console.log(`   ·     ${text}`);
}

// --- the document the reviewer asked for ------------------------------------

const raw = process.argv[2];
if (!raw || raw.startsWith('-')) {
  console.error('usage: node web/preflight.mjs <doc.md>');
  process.exit(2);
}
const DOC = resolve(raw);
if (!existsSync(DOC)) {
  console.error(`there is no file at ${DOC}`);
  process.exit(2);
}

console.log(`\ngalley review — pre-flight on ${DOC}\n`);

if (BREAK) {
  const known = ['no-hook', 'one-shot', 'mute', 'in-place'];
  if (!known.includes(BREAK)) {
    console.error(
      `GALLEY_PREFLIGHT_BREAK=${BREAK} is not one of ${known.join(', ')}`,
    );
    process.exit(2);
  }
  // in-place DRIVES THE REVIEWER'S OWN FILE. It exists to prove check 5 fails
  // when it should, and a proof that can damage a real document is not one
  // anybody should be able to run by accident.
  if (
    BREAK === 'in-place' &&
    !/^(\/tmp\/|\/private\/var\/folders\/|\/var\/folders\/)/.test(DOC)
  ) {
    console.error(
      'GALLEY_PREFLIGHT_BREAK=in-place mutates the document it is pointed at, so it is refused\n' +
        'outside a temporary directory. Copy the file to /tmp and point it there.',
    );
    process.exit(2);
  }
  console.log(
    `  ⚠ BROKEN ON PURPOSE — GALLEY_PREFLIGHT_BREAK=${BREAK}. This run is a proof that the`,
  );
  console.log(
    '    pre-flight fails when it should, not a pre-flight. It will not start a session.\n',
  );
}

// A SESSION ALREADY RUNNING IS A REFUSAL, NOT A WARNING. Two `galley edit`
// servers on one document race their projections into the same file, and the
// second one's startup parse is of whatever the first last wrote.
{
  const rtPath = runtimeOf(DOC);
  if (existsSync(rtPath)) {
    let rt = null;
    try {
      rt = JSON.parse(readFileSync(rtPath, 'utf8'));
    } catch {
      rt = null;
    }
    // `url`, LOWER CASE. serve.Runtime's tags are `url`/`room`/`page`/`pid`
    // (internal/serve/serve.go), and reading it as `rt.URL` gets undefined —
    // which makes every live session look stale and this refusal unreachable.
    // Measured: with a real server up on the document, this printed "a stale
    // .serve.json … (no server answers it)" and went on to start a second one.
    const url =
      rt && typeof rt.url === 'string' ? rt.url.replace(/\/$/, '') : '';
    const live = url ? await waitForServer(url, 1500) : false;
    if (live) {
      console.error(
        `a galley session is ALREADY running on this document at ${url}` +
          `${rt.pid ? ` (pid ${rt.pid})` : ''}\n` +
          '  stop it before starting another — two servers project into the same file.',
      );
      process.exit(2);
    }
    console.log(
      `  note      a stale ${basename(rtPath)} is beside the document (no server answers it)`,
    );
  }
}

// THE OFFLINE PARSE FIRST, before a browser is launched or a port is taken.
// This is the cheapest possible version of "can galley open this at all" — the
// failure the spec that started the editor work hit, and it costs 40ms.
const offline = galley('pending', DOC, '--json');
let offlineView = null;
try {
  offlineView = JSON.parse(offline.out);
} catch {
  offlineView = null;
}
if (!offlineView) {
  console.error(
    `galley cannot read this document offline, so there is no point opening it:\n  ${offline.err.trim()}`,
  );
  process.exit(1);
}

const beforeDoc = snapshot(DOC);
const beforeSidecar = snapshot(sidecarOf(DOC));
const version = galley('--version').out.trim() || galley('version').out.trim();

console.log(
  `  binary    ${GALLEY}${version ? `  (${version.split('\n')[0]})` : ''}`,
);
console.log(
  `  document  ${beforeDoc.size} bytes, sha ${beforeDoc.sha.slice(0, 12)}`,
);
console.log(
  `  sidecar   ${
    beforeSidecar.present
      ? `${beforeSidecar.size} bytes, sha ${beforeSidecar.sha.slice(0, 12)}`
      : 'none'
  }`,
);

// --- the copy ---------------------------------------------------------------

const HERE = mkdtempSync(join(tmpdir(), 'galley-preflight-'));
// THE ONE LINE CHECK 5 TURNS ON. Everything below drives COPY; the reviewer's
// own file is only ever hashed. `in-place` points COPY at the original, which
// is the mistake this whole structure exists to make impossible — and is how
// check 5 is shown to fail when it should.
const COPY = BREAK === 'in-place' ? DOC : join(HERE, basename(DOC));
const COPY_SIDECAR = sidecarOf(COPY);
const JOURNAL = join(HERE, 'agent.jsonl');
const PRESSES = join(HERE, 'revise.log');
if (COPY !== DOC) {
  copyFileSync(DOC, COPY);
  if (beforeSidecar.present) {
    copyFileSync(sidecarOf(DOC), COPY_SIDECAR);
  }
}
writeFileSync(JOURNAL, '');
writeFileSync(PRESSES, '');
console.log(`  copy      ${COPY}`);

if (sha(COPY) !== beforeDoc.sha) {
  console.error(
    'the copy is not byte-identical to the document — refusing to go on',
  );
  process.exit(1);
}

// --- lifecycle --------------------------------------------------------------

let server = null;
let agent = null;
let browser = null;
let session = null;
let watcher = null;
let cleaned = false;

function stopPreflight() {
  if (cleaned) {
    return;
  }
  cleaned = true;
  try {
    const proc = browser && browser.process();
    if (proc) {
      proc.kill('SIGKILL');
    }
  } catch {
    // Already gone, or a browser with no local process.
  }
  for (const child of [agent, server]) {
    try {
      if (child) {
        child.kill('SIGTERM');
      }
    } catch {
      // Already gone; the exit code is what matters.
    }
  }
}

process.on('exit', () => {
  stopPreflight();
  for (const child of [watcher, session]) {
    try {
      if (child) {
        child.kill('SIGTERM');
      }
    } catch {
      // Already gone.
    }
  }
  // The tmpdir is left in place ONLY if a check failed, because it holds the
  // agent journal and the copy the failure happened to — which is the first
  // thing anyone diagnosing it will want.
  if (failures === 0) {
    try {
      rmSync(HERE, { recursive: true, force: true });
    } catch {
      // The server can write its files back mid-removal. A tmpdir left behind
      // is nothing; an exit handler that THROWS turns a clean run into a
      // failure that did not happen.
    }
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

// --- §1 the server is up and the document renders ---------------------------

section(1, 'the server is up and the document renders');

const preflightPort = (await portFree(PREFLIGHT_PORT)) ? PREFLIGHT_PORT : 0;
// THE HOOK IS SET HERE TOO, and not only on the real session. `--on-revise`
// changes what handleRevise DOES — with no command and no reader it answers
// 501, with a command it always answers 204 and always runs it — so a
// pre-flight that omitted it would be exercising a different code path from the
// one the reviewer gets.
const RECORD = `printf '%s\\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> ${JSON.stringify(PRESSES)}`;
const serverArgs = ['edit', COPY, '--no-open', '--port', String(preflightPort)];
if (BREAK !== 'no-hook') {
  serverArgs.push('--on-revise', RECORD);
}
server = spawn(GALLEY, serverArgs, { stdio: ['ignore', 'pipe', 'pipe'] });
let serverOut = '';
server.stdout.on('data', (b) => {
  serverOut += String(b);
});
server.stderr.on('data', (b) => {
  const line = String(b).trim();
  if (line && !line.startsWith('galley: free trial')) {
    console.log(`         [server] ${line}`);
  }
});

const base = await (async () => {
  const started = Date.now();
  for (;;) {
    const m = serverOut.match(/serving\s+(http:\/\/\S+)/);
    if (m) {
      const url = m[1].replace(/\/$/, '');
      if (await waitForServer(url, 20000)) {
        return url;
      }
      return null;
    }
    if (Date.now() - started > 20000) {
      return null;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
})();

if (!check('the server came up and answered on /', !!base, { serverOut })) {
  console.log('\nthe pre-flight cannot go on without a server. No URL.');
  process.exit(1);
}
note(`serving ${base}`);

const pendingLive = async () => (await fetch(`${base}/_galley/pending`)).json();
const reviseState = async () => (await fetch(`${base}/_galley/revise`)).json();

const pageErrors = [];
browser = await chromium.launch(
  process.env.GALLEY_CHROME
    ? { executablePath: process.env.GALLEY_CHROME }
    : {},
);
const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
page.on('pageerror', (e) => pageErrors.push(e.message));
await page.goto(`${base}/`, { waitUntil: 'networkidle' });
const mounted = await page
  .waitForSelector('.ProseMirror', { timeout: 20000 })
  .then(() => true)
  .catch(() => false);
await page.waitForTimeout(1500);

check('the editor mounted', mounted);
// A PAGE ERROR IS A FAILURE, NOT A LOG LINE. A block kind the client cannot
// build throws during the initial render, and the document then renders
// PARTIALLY — which looks like a document, and is not one. This is the check
// that would have caught "the spec could not be opened until tables were
// supported" the first time.
check(
  'nothing threw while the document rendered',
  pageErrors.length === 0,
  pageErrors,
);

// EVERY BLOCK IN THE FILE IS ON SCREEN, compared as a multiset of kinds against
// galley's own read of the same bytes. `galley blocks` skips Note blocks (they
// are not addressable), so the editor's `note` nodes are counted separately and
// against the threads the file's {>>…<<} notes reconcile into.
//
// THE ORACLE IS THE OFFLINE PARSE, TAKEN BEFORE THE SERVER EXISTED, and the
// first version of this check got that wrong in the one way that made it
// worthless. It called `galley blocks <copy>`, which goes through loadPending —
// and loadPending prefers a LIVE server when one is running. So the oracle and
// the thing under test were the same document, and the check compared the
// editor to itself.
//
// It was measured, not reasoned. Against a build with the table extensions
// removed from web/entry.ts — the exact historical failure, the spec that could
// not be opened until tables were supported — this check printed
// `ok  all 22 block(s) the file holds are on screen` and named no tables at
// all. The tables were not merely unrendered: y-prosemirror does not ignore an
// element whose nodeName the schema lacks, it DELETES it from the Yjs document,
// and galley projects that deletion to the .md. Measured on that build with a
// real browser attached and nothing typed: 8 table rows to 0, 2006 bytes to
// 1695. The reviewer's tables were gone from the file, from opening it.
//
// note.ts's header records the same class for block notes, which is why
// NoteBlock is registered even though nothing edits it. This is the check that
// would say so out loud for the next node kind somebody forgets.
{
  const fileBlocks = offlineView.blocks || [];
  const onScreen = await page.evaluate(() => {
    const out = [];
    window.galleyEdit.editor.state.doc.forEach((node) =>
      out.push(node.type.name),
    );
    return out;
  });
  const tally = (list) => {
    const m = {};
    for (const k of list) {
      m[k] = (m[k] || 0) + 1;
    }
    return m;
  };
  const want = tally(fileBlocks.map((b) => b.kind));
  const got = tally(onScreen.filter((k) => k !== 'note'));
  const kinds = Object.keys(want).sort();
  const missing = kinds.filter((k) => (got[k] || 0) !== want[k]);
  check(
    `all ${fileBlocks.length} block(s) the file holds are on screen` +
      ` (${kinds.map((k) => `${k}×${want[k]}`).join(' ')})`,
    fileBlocks.length > 0 && missing.length === 0,
    {
      theFileHas: want,
      theEditorHas: got,
      wrong: missing.map((k) => `${k}: file ${want[k]}, screen ${got[k] || 0}`),
      soFarThisMeans:
        'a block kind the client cannot build is not merely unrendered — y-prosemirror deletes it' +
        ' from the Yjs document and galley projects the deletion to the .md.',
    },
  );
  const notesOnScreen = onScreen.filter((k) => k === 'note').length;
  if (notesOnScreen > 0) {
    note(`${notesOnScreen} block/document note(s) rendered in the prose`);
  }

  // AND OPENING IT DID NOT REWRITE IT. Nothing has typed yet: the browser has
  // loaded, synced and settled, and that is all. A file whose bytes moved from
  // being LOOKED AT is a thing the reviewer has to know before they start.
  //
  // Reported at two different volumes on purpose, because the two causes are
  // not the same size. Blocks lost is the check above and it FAILS. Bytes moved
  // with every block still present is normalisation — a hand-written `*` bullet
  // becoming `-`, a table's pipes padded — which is worth saying and is not a
  // reason to refuse a reviewer their session.
  const opened = snapshot(COPY);
  if (!sameSnapshot(beforeDoc, opened) && missing.length === 0) {
    note(
      `opening it rewrote the file (${beforeDoc.size} → ${opened.size} bytes) with every block still` +
        ' present — markdown normalisation, not loss. It happened to the COPY; the session started' +
        ' below will do the same to your own file, so commit first if that matters.',
    );
  }
}

// THE RIGHT PENDING COUNT, and "right" means the screen agrees with what the
// server says the file holds. An editor that renders the document but paints an
// empty rail is the exact shape of a session that looks fine and answers
// nothing.
//
// TWO THINGS ABOUT THE PENDING PAYLOAD THAT THE OBVIOUS ARITHMETIC GETS WRONG,
// and this check got both wrong first — measured against the every-block-kind
// document, where it reported the rail two cards short of a correct render.
//
//   A COMMENT IS REPORTED TWICE. A `{==highlight==}`, a `{>>block note<<}` and
//   a `{>>@document note<<}` each appear in `suggestions` with kind `comment`
//   AND in `comments` as the thread they reconcile into. They are one thing and
//   they get ONE card — the thread's. Counting both lists expects a card the
//   editor is right not to draw.
//
//   A DOCUMENT-ANCHORED THREAD'S CARD IS NOT IN `#gly-rail`. It has no span to
//   sit beside, so it is placed outside the rail's own element. Scoping the
//   query to `.gly-rail` silently drops it — which reads as a missing card and
//   is a card in a different place.
{
  const live = await pendingLive();
  // `decidable`, the server's own per-entry answer, rather than a kind test
  // written here: the rail cards this counts are drawn through rail.ts's
  // `decidable`, and a check that re-derives the predicate it is checking can
  // only ever agree with itself.
  const wantSuggestions = live.suggestions.filter((s) => s.decidable).length;
  const wantThreads = (live.comments || []).length;
  const rail = await page.evaluate(() => ({
    suggestions: document.querySelectorAll(
      '.gly-card:not(.gly-thread):not(.gly-settled)',
    ).length,
    threads: document.querySelectorAll('.gly-card.gly-thread').length,
  }));
  check(
    `the screen shows the ${wantSuggestions} suggestion(s) and ${wantThreads} thread(s) the server reports`,
    rail.suggestions === wantSuggestions && rail.threads === wantThreads,
    {
      onScreen: rail,
      server: { suggestions: wantSuggestions, threads: wantThreads },
    },
  );
  // The offline read is REPORTED, not asserted equal. Gap 7 is open: a
  // deletion the serializer had to break around a `**bold**` wrapper reads as
  // more spans offline than live, and failing a pre-flight on a known-open
  // divergence would stop a reviewer for something that is already written
  // down. What matters is that the reviewer is told.
  const off = offlineView.suggestions.length;
  if (off !== live.suggestions.length) {
    note(
      `offline reads ${off} suggestion(s) where the session reads ${live.suggestions.length}` +
        ' — the live/offline divergence Gap 7 records, not a new fault',
    );
  }
}

// --- the reviewer's thread, which checks 3 and 4 need ------------------------

await page.click('.ProseMirror');

/** select puts the reviewer's selection on a phrase by asking the editor where
 *  it is, and THROWS if the editor did not take focus — a swallowed keystroke
 *  leaves the document unchanged, which is what several of the defects this
 *  file guards also look like. */
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
      throw new Error(`nothing in the document reads ${JSON.stringify(want)}`);
    }
    editor.commands.focus();
    editor.commands.setTextSelection({ from: at, to: at + want.length });
    editor.view.focus();
    if (!editor.isFocused) {
      throw new Error(
        'the editor did not take focus — a keystroke would go nowhere',
      );
    }
    return { from: at, to: at + want.length };
  }, phrase);

// A PHRASE OUT OF THE REVIEWER'S OWN DOCUMENT, found rather than supplied. It
// has to be unmarked prose in a top-level paragraph (a fence and a table deny
// the composer, and an inline `code` span is Gap 6's territory) and it has to
// occur exactly once, or the anchor is ambiguous about which one it took.
const anchor = await page.evaluate(() => {
  const doc = window.galleyEdit.editor.state.doc;
  const whole = doc.textContent;
  const runs = [];
  doc.forEach((node) => {
    if (node.type.name !== 'paragraph') {
      return;
    }
    node.forEach((child) => {
      if (child.isText && (!child.marks || child.marks.length === 0)) {
        runs.push(child.text);
      }
    });
  });
  for (const run of runs.sort((a, b) => b.length - a.length)) {
    const words = run.trim().split(/\s+/);
    for (let n = Math.min(words.length, 8); n >= 3; n--) {
      for (let i = 0; i + n <= words.length; i++) {
        const phrase = words.slice(i, i + n).join(' ');
        if (phrase.length < 16 || phrase.length > 90) {
          continue;
        }
        if (/[{}]/.test(phrase)) {
          continue;
        }
        if (whole.indexOf(phrase) === whole.lastIndexOf(phrase)) {
          return phrase;
        }
      }
    }
  }
  return null;
});

let threadKey = null;
let threadVia = 'the composer';
if (anchor) {
  note(`anchoring the pre-flight thread on ${JSON.stringify(anchor)}`);
  await select(anchor);
  await page.waitForTimeout(400);
  await page.click('.gly-composer .gly-comment-button');
  await page.fill('.gly-composer .gly-composer-text', REVIEWER_ASKED);
  await page.click('.gly-composer .gly-composer-send');
  await page.waitForTimeout(1200);
} else {
  // A document with no unmarked prose paragraph — all tables, all fences. The
  // composer needs a range and there is none, so the thread is opened on the
  // document instead. Said out loud: this path does not exercise the composer.
  threadVia = 'a document comment (no plain prose to anchor on)';
  galley('suggest', COPY, '--comment', REVIEWER_ASKED, '--document');
  await page.waitForTimeout(1500);
}
{
  const view = await pendingLive();
  const t = (view.comments || []).find((x) =>
    (x.entries || []).some((e) => e.text === REVIEWER_ASKED),
  );
  threadKey = t ? t.key : null;
}
if (
  !check(
    `the reviewer's question opened a thread, via ${threadVia}`,
    !!threadKey,
    {
      comments: (await pendingLive()).comments,
    },
  )
) {
  console.log(
    '\nwithout a thread there is no reply box, so checks 3 and 4 cannot run. No URL.',
  );
  stopPreflight();
  process.exit(1);
}

// --- the agent arms, and hears nothing --------------------------------------

const journal = () =>
  readFileSync(JOURNAL, 'utf8')
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l));
const said = (step, round) =>
  journal().find(
    (l) => l.step === step && (round === undefined || l.round === round),
  ) || null;
const heard = async (step, round, ms) => {
  const started = Date.now();
  for (;;) {
    const line = said(step, round);
    if (line) {
      return line;
    }
    if (Date.now() - started > ms) {
      return null;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
};

if (BREAK !== 'no-hook') {
  agent = spawn(process.execPath, [process.argv[1], '--agent', COPY, JOURNAL], {
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, GALLEY_PREFLIGHT_BREAK: BREAK },
  });
  agent.stderr.on('data', (b) => {
    const line = String(b).trim();
    if (line && !line.startsWith('galley: free trial')) {
      console.log(`         [agent] ${line}`);
    }
  });
  await heard('armed', undefined, 10000);
}
// A blocked `galley wait` has to actually REACH the server before Revise can
// find it — the endpoint refuses a press with no listener and no --on-revise,
// and this pre-flight's whole subject is that refusal.
await page.waitForTimeout(1800);

// --- §2 and §3, twice -------------------------------------------------------

/** sampleRevise watches GET /_galley/revise from the moment before the press
 *  until the window closes. Sampling rather than reading once, because the
 *  window's whole life can be under a second: a single read after the click
 *  cannot tell "it never opened" from "it opened and closed while I was
 *  asking", and those are opposite answers. */
function sampleRevise(everyMs = 60) {
  const seen = [];
  let stop = false;
  const loop = async () => {
    while (!stop) {
      try {
        seen.push({ at: Date.now(), ...(await reviseState()) });
      } catch {
        // The server going away is caught by the checks, not here.
      }
      await new Promise((r) => setTimeout(r, everyMs));
    }
  };
  const done = loop();
  return {
    seen,
    stop: async () => {
      stop = true;
      await done;
      return seen;
    },
  };
}

const draftBox = page.locator(`[data-draft="reply:${threadKey}"]`).first();
const draftState = () =>
  page.evaluate((k) => {
    const el = document.querySelector(`[data-draft="reply:${k}"]`);
    return {
      present: !!el,
      boxes: document.querySelectorAll(`[data-draft="reply:${k}"]`).length,
      value: el ? el.value : null,
      selection: el ? [el.selectionStart, el.selectionEnd] : null,
      focused: !!el && document.activeElement === el,
      entries: document.querySelectorAll(
        `.gly-thread[data-key="${k}"] .gly-thread-entry`,
      ).length,
      cards: document.querySelectorAll(
        '.gly-card:not(.gly-thread):not(.gly-settled)',
      ).length,
    };
  }, threadKey);

const rounds = [];

for (const round of [1, 2]) {
  if (round === 1) {
    section(
      2,
      'a Revise press reaches a listener, and the window opens and closes',
    );
  } else {
    console.log(
      '\n   — and the SECOND press, which is the one the first session lost —',
    );
  }

  // THE HALF-WRITTEN REPLY GOES IN BEFORE THE PRESS, both rounds. It is not
  // submitted, and everything the agent does next happens around it.
  await page.waitForSelector(`[data-draft="reply:${threadKey}"]`, {
    timeout: 15000,
  });
  await draftBox.click();
  await draftBox.fill(DRAFT);
  const before = await page.evaluate(
    ({ k, caret }) => {
      const el = document.querySelector(`[data-draft="reply:${k}"]`);
      el.setSelectionRange(caret, caret);
      return {
        value: el.value,
        selection: [el.selectionStart, el.selectionEnd],
        focused: document.activeElement === el,
        entries: document.querySelectorAll(
          `.gly-thread[data-key="${k}"] .gly-thread-entry`,
        ).length,
        cards: document.querySelectorAll(
          '.gly-card:not(.gly-thread):not(.gly-settled)',
        ).length,
      };
    },
    { k: threadKey, caret: CARET },
  );

  const pressesBefore = readFileSync(PRESSES, 'utf8')
    .split('\n')
    .filter(Boolean).length;
  const sampler = sampleRevise();
  await page.waitForTimeout(150);
  await page.click('#gly-revise');

  // AND STRAIGHT BACK INTO THE DRAFT, because that is what a reviewer does —
  // asks for the revision and carries on with the sentence they were in the
  // middle of. It is also the only way the focus half of check 4 can mean
  // anything: pressing a button takes focus off the textarea, so a draft found
  // unfocused afterwards would only be recording the reviewer's own click.
  await draftBox.click();
  await page.evaluate(
    ({ k, caret }) => {
      const el = document.querySelector(`[data-draft="reply:${k}"]`);
      el.setSelectionRange(caret, caret);
    },
    { k: threadKey, caret: CARET },
  );

  const woke = await heard('woke', round, 30000);
  check(
    `press #${round} woke the agent that was blocked on the document`,
    !!woke && woke.reason === 'revise',
    woke || journal(),
  );
  const replied = await heard('replied', round, 30000);
  check(
    `and the agent answered on the reviewer's thread, live`,
    !!replied && replied.code === 0 && /live/.test(replied.said || ''),
    replied || journal(),
  );
  if (round === 1) {
    const noted = await heard('noted', 1, 20000);
    check(
      'and wrote a block note into the document itself, not only the sidecar',
      !!noted && noted.code === 0,
      noted || journal(),
    );
  }

  // Wait for the window to close, then stop sampling. Closing is a projection
  // whose pending fingerprint differs from the one taken at the ask — the
  // agent's reply is exactly that.
  const closed = await (async () => {
    const started = Date.now();
    for (;;) {
      const s = await reviseState();
      if (!s.waiting) {
        return true;
      }
      if (Date.now() - started > 25000) {
        return false;
      }
      await new Promise((r) => setTimeout(r, 200));
    }
  })();
  const seen = await sampler.stop();

  const opened = seen.some((s) => s.waiting);
  // THE TWO HALVES COME FROM TWO DIFFERENT READERS, and mixing them up cost a
  // false failure. `opened` is the sampler's — only a sampler can see a window
  // whose whole life is under a second. `closed` is the poll above, which reads
  // until `waiting` is false and is therefore authoritative about the close.
  // The first version asked the SAMPLER whether the window had closed, by
  // looking at its last sample; but stop() ends the sampler mid-sleep without
  // taking a final reading, so on a fast round the last sample predates the
  // close by up to one interval and a perfectly correct close reported as a
  // window counting up forever. Measured: 10 samples, `closed: true`, FAIL.
  // WHAT THE SERVER SAID, in the failure detail and only there. A press with
  // nothing to hand the revision to answers 501, and that answer goes to the
  // shell's status line — the readout Gap 3 measured at 64.98px against a
  // scrollWidth of 141, half a word. It is the one thing a reviewer cannot read
  // and the first thing whoever is diagnosing this needs.
  const shellSaid = await page
    .evaluate(
      () => (document.getElementById('gly-status') || {}).textContent || '',
    )
    .catch(() => '');
  check(
    `the revision window OPENED on press #${round}`,
    opened,
    opened
      ? undefined
      : {
          samples: seen.length,
          everWaiting: false,
          theStatusLineSaid: shellSaid,
        },
  );
  check(
    `and CLOSED when the revision landed — not left counting up forever (Gap 1)`,
    opened && closed,
    {
      opened,
      closed,
      stillCountingAfter: closed
        ? null
        : `${Math.round((seen[seen.length - 1] || {}).sinceMs / 1000)}s`,
      samples: seen.length,
      theStatusLineSaid: shellSaid,
    },
  );
  const pressesAfter = readFileSync(PRESSES, 'utf8')
    .split('\n')
    .filter(Boolean).length;
  check(
    "and --on-revise's command ran, so the press has a record the watcher's re-arm gap cannot lose",
    pressesAfter === pressesBefore + 1,
    { pressesBefore, pressesAfter },
  );

  if (round === 1) {
    section(3, "the agent's reply lands and appears on the reviewer's screen");
  }

  // Waited out by the ENTRY ARRIVING rather than by a fixed delay: the arrival
  // is the rebuild. paintRail destroys and rebuilds every card on every poll,
  // so an entry that was not there when the draft was re-established cannot be
  // on screen without paintRail having run under it.
  await page
    .waitForFunction(
      ({ k, n }) =>
        document.querySelectorAll(
          `.gly-thread[data-key="${k}"] .gly-thread-entry`,
        ).length >= n,
      { k: threadKey, n: before.entries + 1 },
      { timeout: 25000 },
    )
    .catch(() => {});
  await page.waitForTimeout(1500);

  const onScreen = await page.evaluate((k) => {
    const card = document.querySelector(`.gly-thread[data-key="${k}"]`);
    if (!card) {
      return null;
    }
    return Array.from(card.querySelectorAll('.gly-thread-entry')).map(
      (row) => ({
        who: (row.querySelector('.gly-thread-who') || {}).textContent || '',
        said: (row.querySelector('p') || {}).textContent || '',
      }),
    );
  }, threadKey);
  const want = AGENT_ANSWERED[round - 1];
  check(
    `round ${round}: the agent's answer is on screen, under the agent's name`,
    !!onScreen &&
      onScreen.some((e) => e.said === want && /agent|claude/i.test(e.who)),
    onScreen,
  );

  if (round === 1) {
    section(4, 'a draft in a reply box survives a server-side mutation');
  } else {
    console.log('\n   — and again, under the second round of writes —');
  }

  const after = await draftState();
  // NON-VACUOUS FIRST. If the agent's writes never landed, or landed without
  // rebuilding the surface the draft sits on, the draft would survive for a
  // reason that has nothing to do with carrying it — a green check on the exact
  // bug.
  check(
    "the agent's write rebuilt the thread card the draft is sitting in",
    after.entries >= before.entries + 1,
    { before: { entries: before.entries, cards: before.cards }, after },
  );
  // ALL THREE TOGETHER. Text put back with the caret at 0, or with focus gone
  // to the document, is not a draft that survived — it is a draft the reviewer
  // has to find their place in again.
  check(
    "the reviewer's unsent draft survived — text, caret and focus",
    after.value === DRAFT &&
      after.selection &&
      after.selection[0] === CARET &&
      after.focused,
    after,
  );

  rounds.push({ round, before, after });
}

// The second press is the whole reason this file exists, so it gets its own
// sentence rather than being inferable from two rounds of oks.
{
  const wakes = journal().filter((l) => l.step === 'woke');
  const presses = readFileSync(PRESSES, 'utf8')
    .split('\n')
    .filter(Boolean).length;
  check(
    'BOTH presses were heard — the re-arming loop did not die on the first, and no press was lost',
    wakes.length === 2 && presses === 2,
    { wakes: wakes.length, presses },
  );
}

// --- §5 the file on disk is unchanged by the check itself --------------------

await page.evaluate(() => {
  const el = document.querySelector('textarea[data-draft]');
  if (el) {
    el.value = '';
  }
});
await browser.close();
browser = null;
server.kill('SIGTERM');
await new Promise((r) => setTimeout(r, 2000));
server = null;
if (agent) {
  agent.kill('SIGTERM');
  agent = null;
}

section(5, 'the file on disk is unchanged by the check itself');

const afterDoc = snapshot(DOC);
const afterSidecar = snapshot(sidecarOf(DOC));
const copyDoc = snapshot(COPY);
const copySidecar = snapshot(COPY_SIDECAR);

// NON-VACUOUS FIRST, and this is the check that keeps §5 from being a
// tautology. "Nothing changed anywhere" passes the two assertions below while
// proving that nothing was ever driven. The copy MUST have moved — the agent
// wrote a block note into its prose and four thread entries into its sidecar —
// and the original must not have.
check(
  'the copy the pre-flight drove really did change (so this section is not vacuous)',
  !sameSnapshot(beforeDoc, copyDoc) ||
    !sameSnapshot(beforeSidecar, copySidecar),
  { copyDoc, copySidecar, beforeDoc, beforeSidecar },
);
check(
  "the reviewer's document is byte-identical to how the pre-flight found it",
  sameSnapshot(beforeDoc, afterDoc),
  { before: beforeDoc, after: afterDoc },
);
check(
  `the sidecar is byte-identical too${beforeSidecar.present ? '' : ' — and was not created'}`,
  sameSnapshot(beforeSidecar, afterSidecar),
  { before: beforeSidecar, after: afterSidecar },
);
// A runtime file beside the document is how `galley pending` and `galley wait`
// find a session. The pre-flight's server announced itself beside the COPY; if
// one appeared beside the original, something opened the wrong file.
check(
  'and nothing announced a server beside the original',
  !existsSync(runtimeOf(DOC)),
  runtimeOf(DOC),
);

// --- the verdict ------------------------------------------------------------

if (failures > 0) {
  if (BREAK) {
    console.log(
      `\n${failures} check(s) FAILED with GALLEY_PREFLIGHT_BREAK=${BREAK}, which is what that break is for.\n` +
        'Read the failures above as the answer to "what does this check actually catch".',
    );
    process.exit(1);
  }
  console.log(
    `\n${failures} check(s) FAILED. A session that starts broken is worse than one that does not start,\n` +
      'so no URL is printed and no server is left running.\n' +
      `\nWhat the pre-flight left behind to diagnose it:\n  ${HERE}\n` +
      `    ${basename(COPY)}          the copy it drove (your own file is untouched)\n` +
      '    agent.jsonl        every step the agent took, in order\n' +
      '    revise.log         every press --on-revise recorded\n',
  );
  process.exit(1);
}

if (BREAK) {
  console.log(
    `\nEVERY CHECK PASSED WITH GALLEY_PREFLIGHT_BREAK=${BREAK} SET.\n` +
      'That is itself a failure: the break was supposed to make a check fail, and it did not.\n' +
      'Either the break no longer reaches the thing it breaks, or the check it targets is asleep.\n',
  );
  process.exit(1);
}

console.log('\nall five checks passed.\n');

// --- the session ------------------------------------------------------------
//
// Only now. The flags are the ones a paired session actually wants, and each
// one is here because its absence cost a real session time:
//
//   --on-revise   a press is NEVER refused and ALWAYS recorded. Without it,
//                 handleRevise answers 501 whenever no `galley wait` happens to
//                 be registered, and 501 goes to the status readout that
//                 ellipsizes at half a word (Gap 3). It is also the only record
//                 that survives the watcher's re-arm gap: a press does not move
//                 the pending fingerprint, so --since cannot recover one that
//                 landed with no reader.
//   --no-open     the browser is opened by hand below, after the URL is
//                 printed, so the reviewer sees the address even if their
//                 default browser does something surprising.

const sessionPort = (await portFree(SESSION_PORT)) ? SESSION_PORT : 0;
const WAKES = join(HERE, 'wakes.jsonl');
const SESSION_PRESSES = join(HERE, 'presses.log');
writeFileSync(WAKES, '');
writeFileSync(SESSION_PRESSES, '');

const sessionRecord = `printf '%s\\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> ${JSON.stringify(SESSION_PRESSES)}`;
session = spawn(
  GALLEY,
  [
    'edit',
    DOC,
    '--no-open',
    '--port',
    String(sessionPort),
    '--on-revise',
    sessionRecord,
  ],
  { stdio: ['ignore', 'pipe', 'pipe'] },
);
let sessionOut = '';
session.stdout.on('data', (b) => {
  sessionOut += String(b);
  process.stdout.write(String(b));
});
session.stderr.on('data', (b) => {
  const line = String(b).trim();
  if (line && !line.startsWith('galley: free trial')) {
    process.stderr.write(`${line}\n`);
  }
});

const sessionURL = await (async () => {
  const started = Date.now();
  for (;;) {
    const m = sessionOut.match(/serving\s+(http:\/\/\S+)/);
    if (m) {
      const url = m[1].replace(/\/$/, '');
      if (await waitForServer(url, 20000)) {
        return url;
      }
      return null;
    }
    if (Date.now() - started > 20000) {
      return null;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
})();

if (!sessionURL) {
  console.log(
    '\nthe pre-flight passed but the session server did not come up. No URL.',
  );
  process.exit(1);
}

// CHECK 1 AGAIN, AGAINST THE REAL SESSION. The copy has the same bytes but not
// the same path, and this is where a path-shaped problem — an unreadable
// sidecar, a document under a symlink — would show. It is a pure read: no
// browser, no press, nothing written.
{
  const live = await (await fetch(`${sessionURL}/_galley/pending`)).json();
  const wantS = offlineView.suggestions.length;
  const wantT = (offlineView.comments || []).length;
  const gotS = live.suggestions.length;
  const gotT = (live.comments || []).length;
  if (gotS !== wantS || gotT !== wantT) {
    console.log(
      `\n  note      the session reads ${gotS} suggestion(s)/${gotT} thread(s) where the file reads` +
        ` ${wantS}/${wantT} — see Gap 7 if a deletion crosses a **bold** wrapper`,
    );
  }
}

// THE SESSION WATCHER. Not an agent: it never answers the review, because
// galley calls no model. It is the re-arming loop the first session hand-rolled
// one-shot, and its job is to make a second press visible.
watcher = spawn(process.execPath, [process.argv[1], '--watch', DOC, WAKES], {
  stdio: ['ignore', 'pipe', 'pipe'],
});
watcher.stdout.on('data', (b) => process.stdout.write(String(b)));
watcher.stderr.on('data', (b) => {
  const line = String(b).trim();
  if (line && !line.startsWith('galley: free trial')) {
    process.stderr.write(`[watch] ${line}\n`);
  }
});
await new Promise((r) => setTimeout(r, 1200));

console.log(`
──────────────────────────────────────────────────────────────────────────────
  ready      ${sessionURL}

  document   ${DOC}
  wakes      ${WAKES}
  presses    ${SESSION_PRESSES}

  the agent side, for the session that is already here:

      galley wait ${DOC} --json

  A watcher is already re-armed on this document and records every press to
  wakes.jsonl; --on-revise records every press the server accepted to
  presses.log. If those two ever disagree, a press landed in a watcher's re-arm
  gap — which is the failure this command exists to make visible rather than
  silent.

  Ctrl-C stops the session and the watcher together.
──────────────────────────────────────────────────────────────────────────────

  WHAT A GREEN PRE-FLIGHT DOES NOT MEAN.

  It means the things that broke before have not broken again on this document,
  this build and this machine. It does not mean the product is good. It cannot
  tell you whether the design reads as one product, whether the rail lands where
  your eye already is, whether the free tier is the right one, or whether the
  loop is exhausting to actually use — and every finding of real value in this
  project came from a human noticing something felt wrong. Task 9 measured the
  gap: on a build with a genuinely broken client run stamp, 28 of its 30
  remaining checks still passed. Trust this to tell you the wiring is right.
  Do not let it tell you the thing is right.
`);

if (!process.env.GALLEY_NO_OPEN) {
  spawn('open', [sessionURL], { stdio: 'ignore', detached: true }).unref();
}

// ONE goodbye, however many ways the run ends. Ctrl-C, a SIGTERM and the
// server exiting on its own can all arrive together — the session prints its
// own shutdown lines and a second "stopping" underneath them reads as a second
// stop.
await new Promise((resolveWait) => {
  let said = false;
  const bye = () => {
    if (said) {
      return;
    }
    said = true;
    console.log('\nstopping the session and the watcher.');
    resolveWait();
  };
  process.on('SIGINT', bye);
  process.on('SIGTERM', bye);
  session.on('exit', bye);
});
