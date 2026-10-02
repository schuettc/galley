// verdict.ts — the verdict: what it is, and the controls that set it.
//
// A REVIEW HAS ONE QUESTION ON OFFER AT A TIME. The top half of this file is
// the pure vocabulary for asking and answering it — markup pending means the
// verdict on offer is Revise, a clean document means it is Approve, a sealed
// review's verdict is one of three fixed outcomes — every label, every button
// string and every enum value that says which of those states is true, with
// no `this` and no DOM. probe.mjs imports it from here directly: a
// browserless test can drive vocabulary.
//
// THE BOTTOM HALF, `verdictMethods`, IS A MIXIN — an object of methods
// `Object.assign`ed onto `App.prototype` in entry.ts, not a class of its own
// — and it is the primary button itself: the label vocabulary above painted
// into `#gly-revise`, the menu the press discloses when work is pending
// (Revise, or Revise & Approve), the poll response that drives all three
// (readRevise — which also drives History's and the seal's own readers, see
// its own comment), and hold/release, the browser-side queue over what the
// rail shows while the mode is live. One file owns one subject — the
// verdict, what it is and the controls that set it — rather than splitting
// the noun from the verb.
//
// `this` IS TYPED AGAINST `AppShell` (web/appshell.ts) — see that file's own
// header for the this-typing decision.
//
// SEVERAL FIELDS THE PRESS BUILDS (`reviseIdle`, `reviseBusy`, `reviseSecs`,
// `reviseApprove`, `reviseCount`, `reviseBack`) ARE OPTIONAL ON `AppShell`,
// AND THAT IS NOT DEFENSIVE TYPING. `makeRevise` sets every one of them only
// inside the branch where `#gly-revise` exists in the DOM (an unbuilt
// checkout has no such element, and the early `return null` skips the whole
// block) — entry.ts's constructor calls `this.revise = this.makeRevise()`
// directly, but nothing in the constructor assigns `reviseIdle` and its
// siblings UNCONDITIONALLY the way appshell.ts's split asks: only the
// element that gates the branch (`revise` itself) is a real `AppState`
// field, and the labels built alongside it are not. In practice all six are
// set together or none of them are, so a caller that has already checked
// `this.revise` never actually finds `this.reviseIdle` missing — but tsc
// cannot see that correlation across two different fields, so `paintRevise`
// and `postVerdict` below narrow each one with a real (and, given the
// invariant above, always inert) local check rather than asserting it away.
// The alternative — typing them required — would be exactly blurDismiss's
// mistake in the other direction: a claim `strictPropertyInitialization`
// would refuse at Task 8.

import { censusCounts } from './rail.ts';
import type { PendingThread } from './rail.ts';
import { getJSON, postJSON } from './net.ts';
import { runFor } from './runs.ts';
import { arrivalMessage, queueArrivals, holdLabel } from './arrivals.ts';
import { BACK_TO_DRAFT } from './versions.ts';
import type { AppShell, ArrivalItem } from './appshell.ts';
import type { ReviseStateView } from './wire';
import type { SuggestionLike } from './suggestions.ts';

// The full shape of what GET /_galley/revise answers.
//
// IT IS THE GENERATED TYPE NOW, not a hand-copy of it. This block used to
// restate all thirteen fields and explain why it had to: "Not in wire.d.ts —
// that file is generated only from the five view structs
// internal/serve/wire_test.go names, and `/_galley/revise` answers with a
// hand-built `map[string]any`." Both halves of that are fixed rather than
// documented: the server answers `ReviseStateView` and the struct is in
// `wireRoots`, so a rename on either side now fails `just verify` instead of
// arriving in a browser as `undefined`.
//
// `entrusted`, `outstanding`, `landing`, `reopenBy` and `reopenNote` stay
// declared here and stay OPTIONAL, and that is not caution — it is the literal
// wire shape. `writeReviseState` sets none of them, so they are undefined on
// every real payload; the entrusted-handoff vocabulary that sealLine and
// reopenLine still carry has no live server path. They are deliberately NOT in
// the Go struct: putting them there would generate a contract claiming the
// server sends fields it never sends, which is worse than the gap. Narrowing
// this is a product decision for whoever owns the handoff.
export interface ReviseWatchView extends ReviseStateView {
  entrusted?: number;
  outstanding?: number;
  landing?: number;
  reopenBy?: string;
  reopenNote?: string;
}

/** The idle label, and the two halves the counting one is built from.
 *
 * Split because the counting label is assembled from three DOM nodes rather
 * than written as one string — the seconds need their own box to reserve, or
 * the button changes width on every decade of the counter and drags the whole
 * right-hand end of the bar with it (see makeRevise).
 *
 * A BUTTON NAMES ITS OWN PRESS, AND THIS PRESS OPENS A MENU. The label was
 * `Revise` and the menu it opened led with `→ Revise`, so a reviewer who wanted
 * to revise pressed the same word twice — and, far worse in the other
 * direction, a reviewer who wanted to APPROVE was looking at a button that said
 * Revise and would not press it. The title carried the real meaning
 * ("…or accept everything and approve — the press offers both") and a title is
 * not a label: it appears on hover, after a second or two, and never on touch.
 *
 * `Finish ▾` names the ACT OF ENDING, which is what both of the menu's exits
 * are, and the ▾ says a menu is what the press produces. The DIRECT face is
 * untouched: on a clean document the press posts the approve and the button
 * says `Approve`, because there is nothing to choose between (see askRevise,
 * and verdictLabel, which is what refuses to offer Approve while anything is
 * outstanding).
 *
 * THE MENU NAMES THE ENDINGS THAT ARE REACHABLE FROM THE PAGE, and there are
 * two of them. The third ending the seal can render — a discard, `galley
 * discard`'s verdict — has no control anywhere in the browser; that is audit
 * finding #10, it is a product decision about whether the page is the yes-only
 * surface, and it is Court's. When it lands it is one more item in this menu
 * and this label does not change, which is the point of naming the act rather
 * than one branch of it. */
export const REVISE_IDLE = 'Revise ▾';
const REVISE_BUSY_PREFIX = 'revising · ';
const REVISE_BUSY_SUFFIX = 's';

/** THE COUNT SITS ON THE VERB IT IS A COUNT OF.
 *
 * `Instructions · N` was a chip three controls to the left of the button that
 * sends those instructions, and a reviewer had to read two places to answer one
 * question: how many am I about to send. The chip is deleted at wide widths
 * (paintSurfaces) and the number is here.
 *
 * COMPOSED, NEVER WRITTEN AS ONE LABEL. The clause goes into its own reserved
 * box inside the idle face — see .gly-revise-count for the two width claims
 * that buys — so filing an instruction from a card a thousand pixels away
 * changes the digits and never the button. probe.mjs drives makeRevise and
 * paintRevise and reads back what the three DOM nodes say together.
 *
 * ZERO HAS NO CLAUSE, and that is not a special case dressed up: at zero the
 * verdict is `Approve` (verdictLabel), so this face is not the one on screen.
 * `Revise ▾` is what it says if it ever is. */
const REVISE_COUNT_JOIN = ' · ';

function reviseCountClause(n: number): string {
  return n > 0 ? `${REVISE_COUNT_JOIN}${n}` : '';
}

/**
 * reviseSeconds is R10: the Revise button says how long it has been.
 *
 * It counts until the revision LANDS, not until the request returns. The
 * request returns in milliseconds — the usual --on-revise is a notification
 * that exits at once while the agent works for minutes — so a button that went
 * back to "Revise" when the POST came back showed no in-flight state at all
 * for a six-second revision.
 *
 * Whole seconds, floored: a counter that reads 3s when three seconds have
 * passed is the only reading that cannot be accused of rounding up. A clock
 * that skewed backwards reads 0, never a negative age.
 *
 * @param ms how long since Revise was pressed
 */
function reviseSeconds(ms: number): number {
  return Math.max(0, Math.floor(ms / 1000));
}

/** The verdict button's other idle label, and its past tense. `approved` is
 * SHORTER than `Approve`, and the one-grid-cell reserve in makeRevise is what
 * makes that a non-event — no width reservation beyond the cell mechanism. */
export const APPROVE_IDLE = 'Approve';
const APPROVE_DONE = 'approved';

// reviseFace — pure computation of the revise/approve verdict button's face:
// which case it is in (approve vs. revise), whether it is disabled, and its
// tooltip. Pulled out of paintRevise because none of it touches `this` or the
// DOM; the comments below travelled with it from there.
function reviseFace(
  history: boolean,
  reviseWaiting: boolean,
  approved: boolean,
  verdictIsApprove: boolean,
  reviseRunning: boolean,
  approveNotBefore: number,
  now: number,
): { approve: boolean; disabled: boolean; title: string } {
  // HISTORY WINS THE SLOT OUTRIGHT while it is open — a busy counter or an
  // Approve face under a reading mode would be offering a verb the surface
  // on screen cannot take.
  const approve = !history && !reviseWaiting && (approved || verdictIsApprove);
  // Disabled only while a COMMAND IS RUNNING, which is the only state the
  // server would refuse a second ask for. Still waiting with nothing running
  // leaves the button clickable while it keeps counting — which is the
  // honest rendering of "you asked, nothing has come back, ask again if you
  // like", and the case where the agent died is exactly when a reviewer
  // needs that. A landed approve is the exception: the review is over, and
  // the guard lives HERE, where disabled is re-computed every second — a
  // guard in askRevise alone would be undone by the next tick.
  const approveCooling = approve && now < approveNotBefore;
  // The way out of a reading mode is never disabled. Every other reason this
  // button goes dead is a reason not to SEND, and leaving History sends
  // nothing — a disabled exit is the covered-control defect wearing the one
  // set of clothes CLAUDE.md has not already recorded it in.
  const disabled = !history && (approved || reviseRunning || approveCooling);
  // The title follows the verdict the label shows: an Approve button
  // explaining how to hand the document back would be the label and the
  // tooltip disagreeing about what the click does. The Revise face's press
  // DISCLOSES now (see askRevise), so its title says that rather than
  // promising a post the press no longer makes.
  const title = history
    ? 'leave History and go back to the draft — nothing here changed it'
    : reviseWaiting
      ? 'asked, and nothing has landed yet — the counter stops when the document moves'
      : approve
        ? 'approve the document as it stands — the review is over'
        : 'hand back for revision, or accept everything and approve — the press offers both';
  return { approve, disabled, title };
}

/** The verdict menu's two exits, over a document with work still pending.
 * Fixed labels on real buttons: neither string ever changes, so the menu
 * cannot resize under the cursor that opened it, and probe.mjs pins both
 * verbatim in the shipped bundle. */
export const MENU_REVISE = 'Revise';
export const MENU_TRUST = 'Revise & Approve';

/** The verdicts the server can seal a review with that the seal paints apart.
 * The third, `approved`, is sealLine's default branch and needs no name. */
export const VERDICT_ENTRUSTED = 'approved-entrusted';
export const VERDICT_DISCARDED = 'discarded';

/** clockTime renders a verdict's moment as the reviewer reads a clock. The
 * DATE is deliberately absent: a seal is read in the session that produced it,
 * and a page still open tomorrow is a page whose server is long gone. */
export function clockTime(ms: number): string {
  if (!ms) {
    return '';
  }
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

// What verdictLabel reads: the two shapes /_galley/pending has answered with
// (a rounds-only `{instructions}` payload, or the older `{comments}` shape
// censusCounts reduces), loosened to the fields this function itself touches.
// `comments` reuses rail.ts's own exported `PendingThread` rather than a
// hand-rolled copy of it — see rail.ts's header on why that type is exported
// at all — and censusCounts' declared parameter type is what this is checked
// against, so a drift there is a compile error here rather than a silent
// disagreement.
type VerdictView = {
  instructions?: { text?: string }[] | null;
  comments?: PendingThread[];
  changes?: { kind?: string }[];
};

/**
 * verdictLabel is the button reading the document's state. Markup pending
 * means the verdict on offer is Revise; a clean document means it is Approve.
 * The label is a PRE-FLIGHT indicator on purpose: a reviewer who just made
 * markup and still sees Approve is looking at the lost-markup failure before
 * pressing, not diagnosing it afterwards.
 *
 * It reads censusCounts, the same reduction the strip shows, rather than
 * counting the raw list itself — one rule, not a second one that agrees for
 * now.
 *
 * THE REVIEWER'S OWN HAND EDITS ARE MARKUP TOO. `view.changes` is what the
 * reviewer altered by hand since the last round (refreshPending), and it is
 * outgoing work exactly as an instruction is: a Revise hands it to the agent.
 * Left out, the button read Approve over unsent edits — and Approve seals the
 * review without recording them as a round, so the very failure this pre-flight
 * label exists to show (markup pending, but the press says it is clean) was the
 * one case it missed. A pending change now offers Revise in both branches.
 */
export function verdictLabel(view: VerdictView | null | undefined): string {
  const changes = view && view.changes;
  const hasChanges = Array.isArray(changes) && changes.length > 0;
  const instructions = view && view.instructions;
  if (Array.isArray(instructions)) {
    return instructions.length > 0 || hasChanges ? REVISE_IDLE : APPROVE_IDLE;
  }
  const counts = censusCounts(view);
  return counts.threads > 0 || hasChanges ? REVISE_IDLE : APPROVE_IDLE;
}

// The body postVerdict sends. `verdict`/`approveOnAnswer` are the two real
// shapes the three call sites build; `trust` is read below (`body.trust`)
// but no call site here ever sets it — see postVerdict's own note on why
// that branch is currently dead, and left rather than deleted.
type VerdictBody = {
  verdict?: string;
  approveOnAnswer?: boolean;
  trust?: boolean;
};

export const verdictMethods = {
  paintHold(this: AppShell) {
    const { hold, holdOff, holdOn } = this.modeUI;
    // Both labels are written on every paint and both stay laid out — see
    // makeMode. Only which of them is VISIBLE changes, so the button's width
    // is the wider of the two whatever state it is in.
    holdOff.textContent = holdLabel(false, 0);
    holdOn.textContent = holdLabel(true, this.heldArrivals.length);
    holdOff.classList.toggle('gly-reserved', this.holding);
    holdOn.classList.toggle('gly-reserved', !this.holding);
    hold.title = this.holding
      ? 'show the arrivals that landed while you were holding — they are already in the document'
      : 'keep new arrivals out of the rail — they still land in the document, they just wait for a card';
    hold.classList.toggle('gly-on', this.holding);
  },

  // --- Revise, and how long it has been (R10) ---
  //
  // The button's wiring lives here rather than in the shell's inline script,
  // where it started. The shell could say "requesting a revision…" and then
  // re-enable the button the moment the POST returned, and that is the whole
  // of R10's complaint: the request returns in milliseconds and the revision
  // does not. Only the editor polls, so only the editor can count.
  //
  // The bundle also takes over the button's CONTENT, and that is not cosmetic.
  // `Revise` becomes `revising · 0s` on this button's own click and then keeps
  // counting, so it is the one control in the bar whose width changes twice
  // over: once on the click, and again on every decade of the counter.
  // Measured with a real --on-revise: 80.58 → 139.58px, and since Revise is
  // the rightmost item every control to its left slid 59px. Both labels are
  // therefore laid out at once in one grid cell, with the seconds reserved
  // inside the counting one — see .gly-revise. The shell keeps its plain
  // `Revise` for an unbuilt checkout; this replaces it the moment the bundle
  // claims the button.
  makeRevise(this: AppShell): HTMLButtonElement | null {
    // `getElementById` cannot know the tag from a string id — narrowed with
    // `instanceof` rather than asserted. `#gly-revise` is a real `<button>`
    // in every shell (internal/serve/edit.html), so this is inert in
    // practice; it also refuses the same case the old `!el` check refused
    // (no element at all), plus the case of the id existing on some other
    // tag, which the untyped form silently built a "button" out of.
    const el = document.getElementById('gly-revise');
    if (!(el instanceof HTMLButtonElement)) {
      return null;
    }
    el.classList.add('gly-revise');
    el.textContent = '';
    const idle = document.createElement('span');
    idle.className = 'gly-revise-label';
    // `Revise` · N · `▾`, in three nodes rather than one string, because the
    // middle one has to be a box that a count cannot resize. See
    // probe.mjs's paintedRevise for what the three read as together.
    // THE RESERVED SLOT, AND WHAT USED TO BE IN IT. This box carried the
    // trail's clause — `Finish ▾ · 6 edits, 2 replies`, what the press would
    // tell the agent — and the rounds-only workflow has no trail to count, so
    // paintRevise stopped writing it and left the reserve behind. The claim it
    // was written for is unchanged and now belongs to the pending count: a
    // number that changes on SOMEBODY ELSE'S click may move the text and never
    // the button, because a click moves nothing except the thing that was
    // clicked.
    const count = document.createElement('span');
    count.className = 'gly-revise-count';
    idle.append('Revise', count, ' ▾');
    this.reviseCount = count;
    const busy = document.createElement('span');
    busy.className = 'gly-revise-label';
    const secs = document.createElement('span');
    secs.className = 'gly-revise-secs';
    secs.textContent = '0';
    busy.append(REVISE_BUSY_PREFIX, secs, REVISE_BUSY_SUFFIX);
    // The THIRD label in the SAME grid cell: the verdict's Approve face (and,
    // once one lands, its past tense). The cell technique is the reservation —
    // the button is exactly as wide as the widest of the three, and nothing in
    // the bar moves when the verdict flips. Reserved from birth, unlike idle:
    // Revise is the resting state until the first census answers.
    const approve = document.createElement('span');
    approve.className = 'gly-revise-label gly-reserved';
    approve.textContent = APPROVE_IDLE;
    // THE FOURTH LABEL IN THE SAME GRID CELL: the way out of History.
    //
    // `← back to draft` is not a fifth control somewhere else on the bar. It is
    // the PRIMARY, wearing its reading-mode face, because the primary's slot is
    // the one place on this page a reviewer already looks for "the thing to
    // press next" — and in History there is exactly one such thing. Putting it
    // in the same cell as the other three is what keeps the swap free: the
    // button is as wide as the widest of the four at every moment, so entering
    // and leaving History cannot move `#gly-status` or the census beside it.
    const back = document.createElement('span');
    back.className = 'gly-revise-label gly-reserved';
    back.textContent = BACK_TO_DRAFT;
    el.append(idle, busy, approve, back);
    this.reviseIdle = idle;
    this.reviseBusy = busy;
    this.reviseSecs = secs;
    this.reviseApprove = approve;
    this.reviseBack = back;
    el.addEventListener('click', () => this.askRevise());
    return el;
  },

  // askRevise is the PRESS, and it only decides. Clean document: the verdict
  // on offer is Approve and the press posts it directly, exactly as it always
  // has. Work pending: the press DISCLOSES the two exits — a plain revise, or
  // the trusted accept-all-and-approve — instead of posting, because a button
  // whose one label hides two different verbs would be guessing which one the
  // reviewer meant. The posting itself, with its status lines and its 409
  // grammar, lives unchanged in postVerdict below.
  askRevise(this: AppShell) {
    if (!this.revise || this.revise.disabled) {
      return;
    }
    // THE READING MODE OWNS THE PRIMARY WHILE IT IS ON SCREEN, and the guard is
    // before the `approved` one deliberately: an approved review still has a
    // History to read, and a way out of it that did nothing would strand the
    // reviewer on a page whose only exit is the browser's back button.
    if (this.versionsPanel && this.versionsPanel.open) {
      this.toggleVersions();
      return;
    }
    if (this.approved) {
      return;
    }
    if (this.verdict === APPROVE_IDLE) {
      if (Date.now() < this.approveNotBefore) {
        return;
      }
      this.postVerdict({ verdict: 'approve' }, true);
      return;
    }
    if (this.verdictOpen) {
      this.closeVerdictMenu();
    } else {
      this.openVerdictMenu();
    }
  },

  postVerdict(this: AppShell, body: VerdictBody, approving: boolean) {
    // `this.revise` is genuinely nullable (an unbuilt checkout has no
    // `#gly-revise`) — narrowed here with a real check rather than asserted,
    // because every call site above has already checked it on `this.revise`
    // itself and tsc cannot carry that narrowing across the method boundary.
    // Inert in practice: this function is reached only from askRevise (which
    // already refused a null `this.revise`) and from the verdict menu, which
    // askRevise is the only opener of.
    const revise = this.revise;
    if (!revise) {
      return;
    }
    if (!approving) {
      // Arm before the request: polling can observe the cleared round and flip
      // this same control to Approve before the response callback runs.
      this.approveNotBefore = Date.now() + 1000;
      window.setTimeout(() => this.paintRevise(), 1000);
    }
    // Disabled for the round trip so an impatient double-click cannot ask for
    // a second revision the server would only refuse with 409. The server's
    // single-flight check is the real guard; this keeps the ordinary case from
    // reaching it. paintRevise takes over once the state comes back.
    revise.disabled = true;
    this.say(approving ? 'approving…' : 'requesting a revision…');
    // Captured before the request, so the `.then` closure below narrows
    // against a local rather than re-reading `this.reviseApprove` — a
    // closure loses whatever narrowing the outer function had. Genuinely
    // optional for the reason this file's header states; guarded again
    // inside the closure rather than assumed.
    const reviseApprove = this.reviseApprove;
    // Fire-and-forget by design: the chain already ends in a `.catch` that
    // reports failure through `say`, which the reviewer is already looking
    // at because this press just wrote to it ('approving…' / 'requesting a
    // revision…'). A caller of `postVerdict` cannot await it without going
    // async itself for no benefit — the click handler already returned.
    void postJSON('/_galley/revise', body)
      .then((res) => {
        if (res.status === 204) {
          if (approving) {
            // The past tense is EARNED: the label reads `approved` only after
            // the server took the verdict, and paintRevise keeps the button
            // disabled from here on — the review is over.
            this.approved = true;
            if (reviseApprove) {
              reviseApprove.textContent = APPROVE_DONE;
            }
            // The verdict retires the trail: the server wiped its copy in the
            // same act that took the approve, and this page's ghosts,
            // highlights and log go with it.
            this.clearTrail();
            // THE TRUST EXIT IS AN APPROVE THAT IS NOT AN ENDING, and this
            // line said it was for ANY approving press — one line under a menu
            // item whose own title reads "surviving unanswered notes travel to
            // the agent". It flashes rather than sticks (applySeal's say('')
            // clears it a poll later) which is why it is milder than the same
            // wrong sentence on the CLI, but it is the same wrong sentence.
            // The branch is `body.trust`, which is what the reviewer pressed;
            // the seal that arrives a poll later says the same thing in the
            // terminal bar, with the counts.
            //
            // `body.trust` IS NEVER SET BY ANY CALL SITE BELOW — a dead
            // branch, left rather than deleted per this migration's own rule:
            // typing proves it unreachable today, and a fix belongs to
            // whoever wires the trust exit's own request body, not to this
            // conversion.
            this.say(
              body.trust
                ? 'approved — your remaining notes travel to the agent'
                : 'approved — the review is over',
            );
            this.refreshVersions();
            return null;
          }
          this.say('revision requested');
          // The round is sent, so a whole-document composer left open over the
          // rail is a draft with nowhere to go: its text was never part of
          // what just went to the agent (only FILED instructions travel), and
          // leaving it open reads as pending work that is not. Close it and
          // discard — openCapture clears the box on the next open.
          this.closeCapture();
          // AND THE TRAIL SETTLES ON SEND. The reviewer's hand edits were the
          // round's outgoing message; pressing Revise hands them off, so the
          // ghosts and glows that were "what am I about to tell the agent"
          // have said it. Left standing they re-anchor onto the agent's
          // rebuilt document — where the edit is already baseline text — and
          // render as a stale pending change over prose nobody is going to act
          // on again. clearTrail here is the same wipe approve does, at the
          // other verdict: the record now lives in the round (versions +
          // ledger), not in live decorations. The server keeps no trail to
          // re-hydrate on reload (ReviewerChanges is computed from versions),
          // so this client wipe is the whole of it.
          this.clearTrail();
          this.refreshVersions();
          return this.refreshPending();
        }
        // 409 means two different things by verb. Revising: a revision is
        // ALREADY running, which is what asking for one wanted — `galley
        // revise` says exactly this from the command line; the two must not
        // disagree about what happened. Approving: markup landed between the
        // label's last paint and the press, the server refused ("there is
        // markup pending"), and the verdict on offer has flipped.
        if (res.status === 409) {
          this.say(
            approving
              ? 'markup arrived while approving — the verdict is Revise now'
              : 'a revision is already in flight — it will act on whatever is pending when it finishes',
          );
          return null;
        }
        return res.text().then((text) => {
          this.say(
            `${approving ? 'approve' : 'revise'} failed: ${text.trim() || res.status}`,
          );
        });
      })
      .catch((err) => {
        this.say(`${approving ? 'approve' : 'revise'} failed: ${err}`);
      })
      .then(() => this.readRevise());
  },

  // --- the verdict menu ---
  //
  // A DISCLOSURE, and every rule that travels with one is load-bearing here.
  // It appends to document.body — NEVER inside .ProseMirror, where anything
  // appended becomes CONTENT and the next projection writes it into the .md
  // (measured: a fixture went from 1 pending to 81) — and it is positioned
  // absolutely off the button's rect MEASURED at open time, because the bar
  // folds and its height is not a constant. It COVERS whatever is under it
  // and displaces nothing; the button itself does not move or change on the
  // opening press — the menu is the thing the press created. Esc closes it
  // the way Esc closes every other surface (see onKey), outside-click closes
  // it, and so does a scroll or a resize, the bubble's reasons: the bar is
  // sticky, so the page moving under an absolutely-positioned menu leaves it
  // anchored to nothing.
  makeVerdictMenu(this: AppShell): HTMLElement {
    const el = document.createElement('div');
    el.className = 'gly-verdict-menu';
    el.hidden = true;
    el.setAttribute('role', 'menu');
    const revise = document.createElement('button');
    revise.type = 'button';
    revise.className = 'gly-verdict-revise';
    revise.textContent = MENU_REVISE;
    revise.title =
      'hand the document to the agent with everything still pending';
    revise.addEventListener('click', () => {
      this.closeVerdictMenu();
      this.postVerdict({}, false);
    });
    const trust = document.createElement('button');
    trust.type = 'button';
    trust.className = 'gly-verdict-trust';
    trust.textContent = MENU_TRUST;
    trust.title =
      'send these instructions and approve only after the agent successfully applies them';
    trust.addEventListener('click', () => {
      this.closeVerdictMenu();
      this.postVerdict({ approveOnAnswer: true }, false);
    });
    el.append(revise, trust);
    document.body.appendChild(el);
    // Outside-click closes, in capture so it runs whatever the click lands
    // on. The button is NOT "outside": askRevise owns the toggle, and closing
    // here too would reopen on the same press (see the note there).
    document.addEventListener(
      'click',
      (event) => {
        if (
          !this.verdictOpen ||
          (event.target instanceof Node && el.contains(event.target))
        ) {
          return;
        }
        if (
          this.revise &&
          event.target instanceof Node &&
          this.revise.contains(event.target)
        ) {
          return;
        }
        this.closeVerdictMenu();
      },
      true,
    );
    // Same shape as the bubble's scroll listener, same reason to read the
    // event: scroll does not bubble, capture on window hears everything, and
    // only the page moving under the menu makes its position stale.
    window.addEventListener(
      'scroll',
      (event) => {
        if (!this.verdictOpen) {
          return;
        }
        if (event.target instanceof Node && el.contains(event.target)) {
          return;
        }
        this.closeVerdictMenu();
      },
      true,
    );
    window.addEventListener('resize', () => this.closeVerdictMenu());
    return el;
  },

  openVerdictMenu(this: AppShell) {
    // See postVerdict's own note: genuinely nullable, narrowed rather than
    // asserted, and inert given the only caller (askRevise) already checked.
    const revise = this.revise;
    if (!revise) {
      return;
    }
    if (!this.verdictMenu) {
      this.verdictMenu = this.makeVerdictMenu();
    }
    const menu = this.verdictMenu;
    // Position FIRST, from the button's own rect read at open time — the
    // refusal note's discipline: an element unhidden with a stale inline
    // position appears somewhere the button is not. Unhide before measuring
    // the menu's own width, because a hidden box measures zero.
    const rect = revise.getBoundingClientRect();
    menu.hidden = false;
    menu.style.top = `${rect.bottom + window.scrollY + 4}px`;
    // Right-aligned under the button: Revise is the rightmost control in the
    // bar, so the menu grows leftward over the page rather than off its edge.
    menu.style.left = `${Math.max(4, rect.right + window.scrollX - menu.offsetWidth)}px`;
    this.verdictOpen = true;
  },

  closeVerdictMenu(this: AppShell) {
    if (this.verdictMenu) {
      this.verdictMenu.hidden = true;
    }
    this.verdictOpen = false;
  },

  readRevise(this: AppShell) {
    return getJSON<ReviseWatchView>('/_galley/revise')
      .then((d) => {
        if (!d) {
          return;
        }
        this.reviseWaiting = !!d.waiting;
        this.reviseRunning = !!d.running;
        // Anchored to a local instant derived from the server's elapsed time,
        // so the seconds keep climbing between polls without this page having
        // to trust its own clock against the server's.
        this.reviseStartedAt = Date.now() - (d.sinceMs || 0);
        this.paintRevise();
        this.readArrival(d);
        this.readSeal(d);
        this.readHandoff(d);
      })
      .catch(() => {});
  },

  paintRevise(this: AppShell) {
    // Each of these is genuinely optional on AppShell — see this file's own
    // header. In practice all five are set together by makeRevise or none of
    // them are, so this guard never actually trips; it is here because tsc
    // cannot see that correlation, and the alternative is the wrong half of
    // blurDismiss's lesson.
    const revise = this.revise;
    const idle = this.reviseIdle;
    const busy = this.reviseBusy;
    const secs = this.reviseSecs;
    const approveLabel = this.reviseApprove;
    if (!revise || !idle || !busy || !secs || !approveLabel) {
      return;
    }
    const ms = this.reviseWaiting ? Date.now() - this.reviseStartedAt : 0;
    // The digits, and only the digits. All three labels stay laid out and only
    // their visibility changes, so neither this click nor the tick a second
    // from now can move the controls beside it — see makeRevise. Busy wins
    // while a revision is outstanding; otherwise the verdict decides between
    // Approve (which an earned `approved` keeps hold of) and Revise.
    secs.textContent = `${reviseSeconds(ms)}`;
    // THE COUNT NEVER LIES: it is the length of the instruction list the
    // SERVER handed this page in the same payload the verdict was read from
    // (refreshPending), never a count of the cards the rail managed to draw.
    // Written into the reserved box, never as a whole label, so no count can
    // move the button. See makeRevise and .gly-revise-count.
    if (this.reviseCount) {
      this.reviseCount.textContent = reviseCountClause(this.pendingCount);
    }
    const history = !!(this.versionsPanel && this.versionsPanel.open);
    const { approve, disabled, title } = reviseFace(
      history,
      this.reviseWaiting,
      this.approved,
      this.verdict === APPROVE_IDLE,
      this.reviseRunning,
      this.approveNotBefore,
      Date.now(),
    );
    idle.classList.toggle(
      'gly-reserved',
      history || this.reviseWaiting || approve,
    );
    busy.classList.toggle('gly-reserved', history || !this.reviseWaiting);
    approveLabel.classList.toggle('gly-reserved', !approve);
    if (this.reviseBack) {
      this.reviseBack.classList.toggle('gly-reserved', !history);
    }
    revise.classList.toggle('gly-history-out', history);
    revise.disabled = disabled;
    revise.classList.toggle('gly-on', this.reviseWaiting);
    revise.title = title;
  },

  // --- hold and release ---
  //
  // HOLD IS A BROWSER-SIDE QUEUE OVER WHAT THE RAIL DISPLAYS, NOT A
  // SERVER-SIDE GATE ON THE DOCUMENT. The suggestions are in the CRDT and on
  // disk either way, and pretending otherwise would mean the file and the
  // screen disagreeing about what the document contains. A held arrival is a
  // CARD NOT YET SHOWN, never a suggestion not yet made — and the opposite
  // reading is the intuitive one, which is why it is said here and again in
  // arrivals.ts.
  toggleHold(this: AppShell) {
    if (this.holding) {
      this.release();
      return;
    }
    this.holding = true;
    this.paintHold();
  },

  // withhold takes the arrivals out of the rail's hands and returns what is
  // left to announce — which is nothing, by construction. The section is
  // resolved HERE rather than at release, because by then the document has
  // moved on and the heading an arrival landed under may not be the heading it
  // is under any more.
  withhold(this: AppShell, arrived: ArrivalItem[]): ArrivalItem[] {
    const runs = this.runsNow();
    for (const a of arrived) {
      if (a.run) {
        this.held.add(a.run);
      }
    }
    this.heldArrivals = queueArrivals(
      this.heldArrivals,
      arrived.map((a): ArrivalItem => {
        // `a` is the loose arrival shape, not a full SuggestionLike (it
        // carries no `text`) — runFor only ever reads `kind`/`author` off it
        // through sameSuggestion, which is exactly what this reconstructs.
        // Same pattern as pending.ts's noticeArrivals, for the same reason.
        const suggestion: SuggestionLike = {
          kind: a.kind || '',
          run: a.run,
          author: a.author,
        };
        return {
          ...a,
          section: this.sectionFor(runFor(runs, { run: a.run, suggestion })),
        };
      }),
    );
    return [];
  },

  release(this: AppShell) {
    const batch = this.heldArrivals;
    this.holding = false;
    this.held.clear();
    this.heldArrivals = [];
    for (const a of batch) {
      if (a.run) {
        this.newRuns.add(a.run);
      }
    }
    this.paintMode();
    this.paintRail();
    if (batch.length === 0) {
      return;
    }
    // One summary for the batch, not one strip per arrival — the reviewer
    // asked to be told all at once, which is what holding meant.
    this.arrivalQueue = queueArrivals(this.arrivalQueue, batch);
    this.stripBatch = batch;
    this.showStrip(arrivalMessage(batch));
    this.pulseCensus();
    this.clearNewLater();
  },
};
