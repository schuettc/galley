// seal.ts — the seal: a review that has ended says so.
//
// The vocabulary in the top half is pure — the terminal readout's sentence,
// the handoff cancel button's label, and the two class-list constants naming
// every control a sealed review must kill (SEALED_VERBS) and the four of
// those it alone owns (SEAL_ONLY_VERBS) — with no `this` and no DOM built.
// probe.mjs and web/layers.mjs both import from here directly.
//
// THE BOTTOM HALF, `sealMethods`, IS A MIXIN — an object of methods
// `Object.assign`ed onto `App.prototype` in entry.ts, not a class of its own
// — and it is the seal and the handoff together: reading each state off the
// poll, the edge that flips the editor's editability and closes the verdict
// menu, the terminal bar it paints, and the two constants' owners
// (applySealedVerbs kills, releaseSealOnlyVerbs revives). The handoff rides
// alongside the seal because readHandoff is readSeal's shape for the agent's
// window and applySeal's unseal branch asks it before handing editing back.
//
// `this` IS TYPED AGAINST `AppShell` (web/appshell.ts) — see that file's own
// header for the this-typing decision. `d`, the payload readSeal and
// readHandoff both read, is verdict.ts's `ReviseWatchView` — see that file's
// own header for what GET /_galley/revise actually answers, and for why
// `entrusted`/`outstanding`/`landing`/`reopenBy`/`reopenNote` are typed
// optional rather than required: the current server never sends them.

import { postJSON } from './net.ts';
import {
  VERDICT_ENTRUSTED,
  VERDICT_DISCARDED,
  clockTime,
  APPROVE_IDLE,
} from './verdict.ts';
import type { AppShell } from './appshell.ts';
import type { ReviseWatchView } from './verdict.ts';

const CANCEL_HANDOFF = '✕ cancel';

/* --- the seal: a review that has ended says so ---
 *
 * THE PAGE USED TO LIE. After Approve the server kept serving and this editor
 * kept taking keystrokes into a document whose registry entry was already
 * withdrawn, so nothing typed could reach anyone. Browsers only let script
 * close windows script opened and this page was opened by the OS, so the fix
 * is a STATE, not a close: the bar collapses to a terminal readout naming the
 * verdict and its moment, the editor goes read-only, and exactly two acts
 * remain.
 *
 * THE TWO ACTS ARE GONE AND THE STATE IS ALL THAT IS LEFT. `SEAL_REOPEN`,
 * `SEAL_REOPENING`, `SEAL_DONE` and `SEAL_STOPPING` were the four labels of a
 * pair of buttons `makeSeal` built into a group it never appended — see there
 * for why reopen is vestigial and why done is the different case. The seal is
 * a READOUT now, and it is the whole of the terminal bar. */

/**
 * sealLine is the terminal bar's whole readout: what the verdict was, when,
 * and — for the trust exit — how much was handed over.
 *
 * The trust exit gets its OWN sentence rather than "approved" with a number
 * appended, because they are two different endings: one says the review is
 * finished, the other says work is still travelling and the reviewer is
 * watching it land.
 *
 * AND WATCHING IS A VERB, so the line has to move. "2 notes entrusted" is what
 * was HANDED OVER and never changes; `outstanding` is how much of it has not
 * been finished, counted down by the server as the agent resolves each one.
 * Without the second number the sentence is identical five seconds after the
 * press and an hour later, which on a page whose whole remaining job is to show
 * the handoff is the same lie in a quieter voice. The line does not disappear
 * when the work lands: a reviewer coming back to a closed page needs to read
 * that it finished, not merely fail to read that it did not.
 *
 * AND IT COUNTS DOWN, WHICH IS A PROPERTY AND NOT A DESCRIPTION. `outstanding`
 * was the server's pending+open, so the agent's first `galley suggest` — the
 * verb it is told to run first — rendered `2 notes entrusted · 3 outstanding`:
 * more left than was ever handed over, on this line. The server reports the
 * agent's own work in flight as `landing` now, and it gets its own clause,
 * because it is a real reason the handoff is still open and `all applied` over
 * an undecided proposal would be the next wrong sentence. The bound holds:
 * outstanding never exceeds entrusted.
 *
 * THE COUNT IS A CLAUSE, NOT A SECOND READOUT — one surface, one language, and
 * the clause is APPENDED so the sentence's first half is stable while the bar
 * ellipsizes from its end (see the readout-order rule in CLAUDE.md). At most
 * ONE clause is appended, for the same reason: the notes are what the reviewer
 * handed over and are the fact they came back for, so they win when both are
 * non-zero.
 */
export function sealLine(
  verdict: string,
  atMs: number,
  entrusted: number,
  outstanding: number,
  landing: number,
): string {
  const t = clockTime(atMs);
  const stamp = t ? ` ${t}` : '';
  if (verdict === VERDICT_DISCARDED) {
    return `Discarded${stamp} · markup thrown away`;
  }
  if (verdict === VERDICT_ENTRUSTED) {
    const n = entrusted || 0;
    const left = outstanding || 0;
    const inFlight = landing || 0;
    const handed = `Approved${stamp} · ${n} note${n === 1 ? '' : 's'} entrusted`;
    if (left > 0) return `${handed} · ${left} outstanding`;
    if (inFlight > 0) return `${handed} · ${inFlight} still landing`;
    return `${handed} · all applied`;
  }
  // AND HOW TO GET BACK, WHICH THIS DID NOT SAY. The seal is terminal by
  // design and there is no reopen button — that was decided, and it holds. What
  // was never decided is that the way back should be INVISIBLE: the seal is
  // in-memory only, so stopping `galley edit` and starting it again returns a
  // live review, and Approve changes no bytes, cuts no version and reaches git
  // not at all. Measured 2026-08-23: approve, restart, `sealed: false`.
  //
  // So the whole cost of a mis-pressed Approve is one command — and a reviewer
  // reading `Approved 14:32 · review closed` has no way to know that. A
  // recovery that exists and is unmentioned is this codebase's own
  // invisible-but-present defect wearing the other face, and it is answered
  // with a clause rather than with a button: a confirmation would tax every
  // review forever to protect against something a restart undoes.
  return `Approved${stamp} · review closed · restart galley edit to reopen`;
}

/** reopenLine is what the status readout says when the page comes back from
 * sealed to live because the AGENT reopened it. The reviewer is looking at
 * this page and nothing else, so the reason has to arrive here or nowhere. */
/** SEALED_VERBS is every control a sealed review must not offer: the four card
 * verbs, the boxes that are input wearing a different tag, and the census
 * strip's bulk verbs.
 *
 * IT IS THE SOLE WRITER OF THE FOUR CARD VERBS' FLAG NOW, and that is a change
 * worth stating rather than leaving to be noticed. The first four selectors
 * here used to be `dimCard`'s list as well — it disabled accept, reject,
 * resolve and delete on every card whose mark was off the viewport, because a
 * card clamped to the rail's edge was something on screen you could not read
 * and must not be able to decide. The rail scrolls with the document now, every
 * card is reachable by scrolling to it, and there is no such state; `dimCard`
 * is deleted. Two writers of one flag was the condition that produced the
 * live-page bug this constant's invariant is about (see applySealedVerbs), so
 * one fewer is the direction that makes the invariant easier to hold — and the
 * invariant itself is UNCHANGED and must stay true.
 *
 * A CLASS LIST IS A LIST, and it goes stale exactly the way `draftRoots` is
 * documented as going stale: a verb rendered under a name that is not in here
 * is not covered, and nothing says so. Every entry was walked against the DOM
 * the rail actually builds (2026-08-15) and it found two holes, neither of them
 * a dead selector:
 *
 *   - the revision receipt's `✓ accept N` / `✗ reject N` were built CLASSLESS,
 *     so a sealed page left bulk accept live. They were given
 *     `gly-card-accept` / `gly-card-reject` — one name for one verb — rather
 *     than an entry of their own here, AND THAT DECISION IS WHY THIS LIST DID
 *     NOT HAVE TO CHANGE WHEN THE RECEIPT WAS DELETED. Both selectors still
 *     match: they are `decideButton`'s, on every proposal card in the rail and
 *     in the sheet. Had the receipt been given a selector of its own, deleting
 *     it would have left a dead entry here — and a dead entry satisfies the
 *     invariant below VACUOUSLY, which is the "a check that cannot fail" shape
 *     this codebase has been bitten by more than once. §8 of web/layers.mjs
 *     reads this constant entire against a real fixture and requires every
 *     selector in it to match something, which is what would catch it.
 *   - `.gly-overall-input` files a note on the whole document through
 *     `/_galley/instruct`. It is the reply box's own case, on the panel instead
 *     of on a card, and it was missed because at the time it was an `<input>`
 *     and the sweep was reading for `<textarea>`. IT IS A `<textarea rows=2>`
 *     NOW — the whole-document note grew a second line so it could hold a
 *     second sentence — and the only reason that did not silently un-cover it
 *     is that this selector is a CLASS and never was a tag. Do not "tidy" any
 *     entry in this list into a tag selector; the element behind one is free to
 *     change tag, and this one already did.
 *
 * The seven selectors that were already here all match real elements: accept
 * and reject come from `decideButton`, resolve and delete and the reply box
 * from the thread card, and both `.gly-census button` and `.gly-composer button`
 * have live children (the retired `✗ all` was a button, not a class, so its
 * removal left nothing dangling).
 *
 * THE SETTLED CARDS MOVED SURFACES AND NOT ONE SELECTOR MOVED WITH THEM, and
 * that is the design working rather than luck. `.gly-thread-resolve`,
 * `.gly-thread-delete` and `.gly-thread-reply` are `threadCard`'s own classes,
 * and `threadCard` is the one component every surface wears — so a settled
 * card leaving the rail for the sheet changes which ancestor it hangs under and
 * nothing else. Had the rail's settled list been given verb classes of its own,
 * deleting it would have left three DEAD ENTRIES here, and a dead entry
 * satisfies the invariant below VACUOUSLY. That is the same argument the
 * revision receipt's deletion made one entry up, arriving a second time, which
 * is why it is written down rather than re-derived.
 *
 * `.gly-census button` also gained a member in the same change: the census
 * count is a `<button>` now (it opens the sheet). A sealed review hides the
 * whole strip, so covering it here is belt to that braces — but it is built
 * once and never rebuilt, so it needs the OTHER half of the invariant. See
 * SEAL_ONLY_VERBS.
 *
 * THE INVARIANT EVERY ENTRY IN THIS LIST OWES, and the one thing to know
 * before adding another: EVERY SELECTOR HERE EITHER HAS A PAINTER THE UNSEAL
 * EDGE ITSELF RUNS, OR IS IN `SEAL_ONLY_VERBS` AND IS RE-ENABLED EXPLICITLY ON
 * THAT EDGE. A selector that is in neither is a control the first seal kills
 * for the life of the tab — which is what shipped here once, in both
 * directions, and §8 in web/layers.mjs now reads this whole constant on both
 * edges rather than the rail's share of it.
 *
 * "A PAINTER THE EDGE RUNS" IS THE WHOLE OF THE FIRST CLAUSE, and it used to
 * read "a painter that re-derives its flag" — which `.gly-comment-button`
 * satisfied on paper and failed in fact. Its writers are all GESTURES:
 * `placeComposerButton` on a `selectionUpdate`, `hideComposer`, and
 * `openSectionComposer`. `applySeal` runs none of them, and cannot sensibly —
 * re-placing the composer at an edge would reset a half-typed comment and its
 * note. So a painter counts here only if `applySeal` calls it (`paintCensus`,
 * `paintRail`); anything else belongs in `SEAL_ONLY_VERBS`, where the comment
 * button now is.
 *
 * THE THIRD HOLE, AND IT IS THE MERGE'S OWN. `.gly-composer-text` is the box a
 * comment is TYPED into, and it was safe to leave out only for as long as the
 * box had no way to file by itself: the seal disables `.gly-composer button`,
 * which covers the send button, and a textarea with no keydown handler is inert
 * however live it is. `submitOnEnter` ended that — Enter now files from all four
 * composers — so a composer left OPEN across a verdict was a live box on a
 * sealed page whose Enter still posted. The server 409s, which is why this is a
 * defect and not data loss, but "type a sentence, press Enter, find out" is the
 * precise failure the seal was built to remove (see §10 of web/typing.mjs). A
 * disabled field cannot take a keydown at all, so covering the box is what makes
 * the handler unreachable; guarding inside `submitOnEnter` would put `sealed`
 * into a helper in web/rail.ts that knows nothing about the App and would have
 * to be re-guarded for every future caller.
 */
// AND IT NAMES WHAT THE PRODUCT BUILDS, which is not a tidy-up: web/layers.mjs
// §8 reads this constant rather than a copy of it and requires EVERY selector
// in it to match something, precisely so that a dead entry is a named failure
// instead of a silent subtraction from a group query. Four entries had gone
// dead — `.gly-card-accept`, `.gly-card-reject`, `.gly-thread-resolve` and
// `.gly-thread-reply`, the accept/reject pair and the two conversation verbs —
// because an instruction is immutable work for the next round rather than a
// proposal to decide or a conversation to answer. `threadCard` builds edit and
// delete, and those are what is named here. A list that names controls nobody
// renders reads as coverage and is not: the seal's claim is about the controls
// a sealed review must not OFFER, and it can only be checked against controls
// that exist. (Two copies of this paragraph stood here, one of them stale about
// how many verbs `threadCard` builds; a doubled note is a note that will be
// half-updated next time.)
//
// AND THOSE FOUR CONTROLS ARE NOT MERELY UNBUILT NOW — THEY ARE DELETED.
// `suggestionCard` and `decideButton` (the accept/reject pair) and `sendReply`
// (the reply box) are gone from this file, so the classes cannot come back by
// accident; §8's rule is what would have caught them if they had been left
// named here.
//
// AND `.gly-census button` IS NARROWED BY ONE, WHICH IS THE ONE EXEMPTION THIS
// LIST HAS. The census strip is a group of VIEW DOORS, and a door is not a
// verb — it was only ever in here because the count beside it is a door to a
// list of cards whose every verb dies with the seal. History's door leads
// somewhere READING is the whole point, and reading is all a sealed review has
// left. `:not(.gly-versions-open)` says that in the selector rather than in a
// second sweep that would fight this one.
//
// Measured before the exemption: the door was shown, on screen and reachable by
// `elementFromPoint`, and `disabled` — the exact combination that reads as a
// broken control rather than an absent one, which this codebase already rates
// as the worse of the two.
export const SEALED_VERBS =
  '.gly-thread-delete, ' +
  '.gly-thread-edit, .gly-thread-edit-text, .gly-thread-edit-save, ' +
  '.gly-overall-input, .gly-census button:not(.gly-versions-open), ' +
  '.gly-composer button, ' +
  '.gly-composer-text, .gly-capture button, .gly-capture-open';

/** SEAL_ONLY_VERBS is the half of SEALED_VERBS the seal itself owns, because
 * NOTHING ELSE DOES — nothing, that is, that the unseal EDGE runs.
 *
 * `applySealedVerbs` only ever writes `true` (see it for why), and for the
 * elements a painter rebuilds or re-derives that is exactly right: writing
 * `false` over `paintCensus`'s quiet sweep — or over a box's own in-flight flag
 * — is this function inventing a second, wrong answer to a question another
 * owner already answers. But four of the elements the sweep reaches have no such
 * owner at all, and for those the one-way rule is the mirror-image bug — a
 * reopened review with four permanently dead controls:
 *
 *   - `.gly-census-overall` is built ONCE in `makeCensus` and never rebuilt;
 *     `paintCensus` re-derives `✓ all` and nothing else. Dead, it is not that
 *     the whole-document panel looks wrong — it CANNOT BE OPENED.
 *   - `.gly-overall-input` survives every paint (it is `makeCaptureCard`'s, and
 *     the capture card is built ONCE — on the first press of the bar's door —
 *     and thereafter only shown and hidden). Its own submit handler is the only
 *     other writer of the flag, and that path is unreachable once the box is
 *     dead. Note the card may not exist yet on a page nobody has pressed the
 *     door on; `applySealedVerbs` walks a live query, so a selector matching
 *     nothing is inert, and `openCapture` re-derives the flag from
 *     `this.sealed` every time it opens.
 *   - `.gly-composer-send` is built once in `makeComposer` and appended to the
 *     body.
 *   - `.gly-comment-button`, its sibling, was left OUT of this list for one
 *     round on the strength of "it recovers through
 *     `placeComposerButton`/`hideComposer`" — and it does, but only when the
 *     reviewer makes a gesture, and a gesture is not an edge. Its three writers
 *     are `placeComposerButton` (only on the `place` verdict; a selection that
 *     has not moved is `keep`, which writes no flag), `hideComposer` and
 *     `openSectionComposer`, and the unseal edge runs none of them. So a
 *     composer left placed across a seal came back from Reopen with a dead
 *     comment button, and clicking it could not even fix it: `.gly-composer`
 *     cancels its own mousedown to keep the editor's selection alive, so the
 *     click that lands on the dead button moves no selection, fires no
 *     `selectionUpdate`, and the second click does nothing either. Measured red
 *     in §8 of web/layers.mjs, which reads the flag BEFORE any post-reopen
 *     gesture for exactly this reason.
 *
 * Giving these fake painters was the alternative and it is worse: it would put
 * `!this.sealed` into functions that otherwise know nothing about the seal, and
 * `paintOverall` runs on every pending refresh, so it would race the input's
 * own in-flight `disabled` and re-enable a box mid-POST. Re-running
 * `placeComposerButton` from the edge is the same mistake in the composer's
 * shape: a fresh placement clears the note, closes the form and drops the block
 * target, so an edge that "just repaints" would throw away a half-written
 * comment. The seal is genuinely these four elements' only owner, so the seal
 * writes both edges — and it writes the live one on the EDGE (`applySeal`), not
 * in `paintSeal`, which runs on every poll.
 *
 * ONE FLAG THIS DOES OVERWRITE, and it is named rather than left to be
 * discovered: `openSectionComposer` disables the comment button while the
 * heading it selected is not in the server's block list yet ("not in the
 * document yet — it lands on the next sync"). A seal and a reopen crossing that
 * window hand the button back early. It is transient by construction — the next
 * pending refresh brings the key, and the next selection re-derives the flag —
 * and the alternative is the composer keeping a permanent dead button so that a
 * one-poll advisory can never be overruled.
 *
 * `.gly-composer-text` joins for its SIBLING'S reason, exactly: it is built once
 * in `makeComposer` and appended to the body, and the unseal edge runs no
 * painter that touches it. Left out, the first seal would make the comment box
 * untypeable for the life of the tab — and it would LOOK like the composer
 * worked, because Reopen hands back the button that opens it and the box only
 * refuses once the reviewer is already typing into it. Its one other writer is
 * `sendComment`'s in-flight `settled`, which now defers to the seal for the same
 * reason `fileNote` does.
 *
 * `.gly-census-count` IS THE SIXTH, AND IT JOINED THE MOMENT IT STOPPED BEING A
 * READOUT. It was a `<span>` — nothing to disable, nothing to re-enable — and
 * it is a button now, because the sheet is the review's whole list at every
 * width and this is how it is opened. Like `.gly-census-overall` beside it, it
 * is built ONCE in `makeCensus` and never rebuilt: `paintCensus` writes its
 * TEXT on every poll and re-derives the `disabled` flag of `✓ all` and nothing
 * else. Left out of this list, the first seal would take the way into the
 * settled conversations away for the life of the tab, and Reopen would hand
 * back every verb on the page except the one that reaches the record. It is
 * caught by §8's own rule and not by a fresh one — every selector in
 * SEALED_VERBS either has a painter the unseal edge runs, or is here.
 *
 * `.gly-composer-cancel` IS THE SEVENTH AND IT JOINED WITH ITS OWN BIRTH. It
 * is built once in `makeComposer` beside `.gly-composer-send`, appended to the
 * body with it, and reached by `.gly-composer button` in SEALED_VERBS — so it
 * is disabled by the first seal and there is no painter anywhere that would
 * ever hand it back. Left out, a reopened review would carry a composer whose
 * only visible way out was dead for the life of the tab, which is the worse
 * half of the mirror-image bug this list exists for: `Add instruction` dying is
 * a verb you cannot use, `cancel` dying is a surface you cannot leave.
 *
 * AND `.gly-census-overall` HAS LEFT IT, for the reason four selectors left
 * SEALED_VERBS: the whole-document handle was a bar control opening a floating
 * panel, then the rail's own first card, and it is a bar control again
 * (`.gly-capture-open`, matched by the `.gly-census button` entry) opening a
 * card that floats over the rail. Its INPUT kept its class through both moves,
 * which is why this list did not have to.
 *
 * `.gly-capture-open` IS NAMED DIRECTLY BECAUSE IT LEFT THE STRIP. It was a
 * `.gly-census button` for one round — covered by that entry without being
 * mentioned — and it moved OUT of the census strip, which is a group of view
 * doors and no place for a verb (see makeCaptureButton). A control whose seal
 * coverage rode on a container it has left is coverage that disappears with the
 * move and takes no check with it, so it is spelled here. It is NOT in
 * SEAL_ONLY_VERBS: `paintCaptureVerb` re-derives its flag on every
 * `paintSurfaces` and reads `this.sealed` as part of the answer, so it has a
 * painter, and a seal-only entry would be this list claiming an owner it does
 * not have.
 *
 * `.gly-capture-cancel` IS HERE BECAUSE ITS TWIN IS, AND BECAUSE BORROWING THE
 * TWIN'S CLASS IS WHAT WENT WRONG. The capture card's exit first wore
 * `.gly-composer-cancel` — it does the same job — and the seal reaches the
 * composer's cancel through `.gly-composer button`, a DESCENDANT selector this
 * button is not underneath. So a sealed review had two elements of that class
 * and killed one: `just layers` §8 measured `{found: 2, killed: false}` while
 * every other selector in this list reported `killed: true`. The button has its
 * own class now, `.gly-capture button` is in SEALED_VERBS so the seal reaches
 * it the way it reaches the composer's, and the entry is HERE for the same
 * reason `.gly-composer-cancel` is: nothing re-derives its flag, so the seal
 * that killed it is the only thing that could hand it back. Its INPUT is
 * still here, which is the entry that ever mattered — a live box on a sealed
 * page whose Enter still posts is the precise failure this list was built to
 * remove. web/layers.mjs §8 reads this constant rather than a copy and
 * requires every selector in it to match something, so a dead entry is a named
 * failure instead of a silent subtraction from a group query. */
export const SEAL_ONLY_VERBS =
  '.gly-census-count, .gly-overall-input, .gly-composer-send, ' +
  '.gly-composer-cancel, .gly-comment-button, .gly-composer-text, ' +
  '.gly-capture-cancel';

export function reopenLine(by: string, note: string): string {
  // ONE OWNER FOR ONE SENTENCE. pressReopen used to write its own optimistic
  // line — "reopened — the review is live again" — and then read the seal back
  // at once, which put this function's output straight over it. Two strings for
  // one event, and the bare word won because it arrived second. The better one
  // lives here now, and pressReopen calls this rather than spelling it a second
  // time.
  const who =
    by === 'agent'
      ? 'reopened by the agent'
      : 'reopened — the review is live again';
  return note ? `${who} · ${note}` : who;
}

export const sealMethods = {
  // --- the handoff ---
  //
  // readHandoff is readSeal's shape for the agent's window: the editability
  // flip is an EDGE, adopted from the same poll the seal rides. The seal
  // outranks it in both directions — a sealed page is not editable whatever
  // the window says, and applySeal's unseal branch asks this flag before it
  // hands editing back.
  readHandoff(this: AppShell, d: ReviseWatchView) {
    const was = !!this.handoff;
    const now = !!d.handoff;
    this.handoff = now;
    this.draftError = d.draftError || '';
    if (now !== was) {
      if (now) {
        this.editor.setEditable(false);
      } else if (!this.sealed) {
        this.editor.setEditable(!this.handoff);
      }
    }
    this.paintCancel();
    this.paintReadout();
  },

  // The way back from a handoff: one button, built once, in a reserved box —
  // `visibility: hidden` PLUS `disabled` when no window is open (a button
  // hidden in CSS is still a button to the keyboard), so the window opening
  // moves nothing in the bar.
  makeHandoffCancel(this: AppShell): HTMLButtonElement | null {
    const anchor = document.getElementById('gly-revise');
    if (!anchor || !anchor.parentNode) {
      return null;
    }
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'gly-handoff-cancel';
    b.textContent = CANCEL_HANDOFF;
    b.title =
      'take the document back from the agent — a parseable draft is kept as its round';
    b.disabled = true;
    b.addEventListener('click', () => {
      if (b.disabled) {
        return;
      }
      this.say('cancelling the handoff…');
      postJSON('/_galley/handoff/cancel', {})
        .then(() => this.readRevise())
        .catch(() => {});
    });
    anchor.parentNode.insertBefore(b, anchor.nextSibling);
    this.paintCancelOn(b);
    return b;
  },

  paintCancel(this: AppShell) {
    if (this.cancelBtn) {
      this.paintCancelOn(this.cancelBtn);
    }
  },

  paintCancelOn(this: AppShell, b: HTMLButtonElement) {
    const on = !!this.handoff && !this.sealed;
    b.disabled = !on;
    b.classList.toggle('gly-cancel-hidden', !on);
  },

  // --- the seal ---
  //
  // readSeal is the one place the terminal state is adopted, and it is written
  // as a TRANSITION rather than a repaint because two of the three things it
  // does are edges, not states: the editor's editability flips exactly when
  // the seal does, and the agent's reopen reason is a sentence said ONCE, at
  // the moment the page comes back to life.
  readSeal(this: AppShell, d: ReviseWatchView) {
    const was = !!this.sealed;
    const now = !!d.sealed;
    this.sealed = now;
    this.sealVerdict = d.verdict || '';
    this.sealAt = d.verdictAt || 0;
    this.sealEntrusted = d.entrusted || 0;
    // How much of the entrusted work is NOT FINISHED. Read on every poll
    // like the rest of the record — the seal already rides this endpoint, so
    // the countdown needs no surface of its own.
    this.sealOutstanding = d.outstanding || 0;
    // And the agent's own work in flight, which is a different population and
    // was once counted into the one above — see sealLine, where that made the
    // countdown count up.
    this.sealLanding = d.landing || 0;
    if (now !== was) {
      this.applySeal(was, d);
    }
    this.paintSeal();
  },

  // applySeal is the edge. A sealed review takes NO INPUT: the editor stops
  // being editable (ProseMirror's own flag, so a keystroke never reaches the
  // document and therefore never reaches the trail), the verdict menu closes,
  // and the rail's verbs go dead. Coming back is the exact inverse, plus the
  // one thing that only exists on this edge — the agent's reason.
  applySeal(this: AppShell, was: boolean, d: ReviseWatchView) {
    if (this.sealed) {
      this.closeVerdictMenu();
      this.editor.setEditable(false);
      // Whatever the shell status was mid-flight, the verdict is now the
      // whole story; the terminal readout says it and this line stops
      // competing with it.
      this.say('');
      return;
    }
    this.editor.setEditable(true);
    // THE FLAGS COME BACK FROM THEIR OWNERS, never from a blanket re-enable.
    // `applySealedVerbs` only turns things OFF (see it for why), so nothing
    // hands the rail's verbs back on its own — and the seal left every one of
    // them dead, which on a reopened review is a page with nothing the
    // reviewer can press. The next `refreshPending` is not a way out either:
    // it runs when `/_galley/rev` moves, and a reopen does not touch the
    // document. So repaint, and let the owners answer: `paintRail` rebuilds
    // every card, with fresh, live buttons, and `paintCensus` re-derives `✓ all`
    // from the counts rather than assuming it. (`dimCard` used to re-disable the
    // four verbs on every card whose mark was off the viewport on the way past.
    // The rail scrolls with the document now, no card is unreachable, and a
    // rebuilt card is simply live.)
    //
    // AND THE FOUR WITH NO OWNER ARE RE-ENABLED HERE, because a repaint does
    // not reach them: the census strip's whole-document handle, the panel's
    // note box and the composer's two buttons are each built once and never
    // rebuilt, so the seal is the only writer of their flag in either
    // direction. Without this line a reopened review cannot even OPEN the
    // whole-document panel. §8 in web/layers.mjs drives the handle and the
    // composer rather than reading hidden surfaces, and it was shown red first
    // — twice: three elements the first time, and the comment button the
    // second, after the invariant was sharpened from "has a painter" to "has a
    // painter THIS EDGE RUNS".
    this.paintCensus();
    this.paintRail();
    this.releaseSealOnlyVerbs();
    // A reopened review takes the verdict button back from `approved`: the
    // label is EARNED by a verdict, and this one has been retired.
    this.approved = false;
    if (this.reviseApprove) {
      this.reviseApprove.textContent = APPROVE_IDLE;
    }
    if (was) {
      // THE AGENT'S REOPEN ARRIVES HERE OR NOWHERE. The reviewer is looking
      // at a sealed page; the poll is what notices, and the status readout is
      // the surface they are already reading.
      this.say(reopenLine(d.reopenBy || '', d.reopenNote || ''));
    }
  },

  // makeSeal builds the terminal bar's two halves and leaves both off.
  //
  // A DIFFERENT SET OF CHILDREN, SWAPPED ON A STATE CHANGE — which is allowed;
  // what is not is anything moving on a CLICK. So the readout goes BEFORE the
  // bar's one flexible cell (a readout that pushes a button is the same defect
  // as a button that pushes its neighbour) and the two buttons go after it,
  // each carrying both of its labels in one grid cell so neither can resize
  // under the cursor that pressed it.
  makeSeal(this: AppShell): { readout: HTMLElement } {
    const bar = document.querySelector('.gly-bar');
    const spacer = bar && bar.querySelector('.gly-spacer');
    const readout = document.createElement('span');
    readout.className = 'gly-seal gly-seal-off';
    readout.id = 'gly-seal';
    readout.setAttribute('role', 'status');
    readout.setAttribute('aria-live', 'polite');

    // `↺ Reopen`, `✓ Done`, THEIR GROUP AND `sealButton` ARE DELETED, and the
    // group is why this is not the removal of a working control: `makeSeal`
    // built `.gly-seal-actions`, set it `hidden`, put both buttons in it and
    // NEVER APPENDED IT TO THE BAR. A detached, hidden button dispatches no
    // click, so neither verb has been pressable for as long as that has been
    // true — web/layers.mjs §8 already asserts the population at zero rather
    // than reading paint nobody can see, and it says so in as many words.
    //
    // REOPEN IS VESTIGIAL, AND THE WHOLE PRODUCT SAYS SO, not just this file.
    // There is no `/_galley/reopen` route on the EditServer, nothing anywhere
    // sets `EditServer.sealed` back to false, and `galley reopen` is gone from
    // the CLI — internal/serve/rounds_surface_test.go and
    // cmd/galley/rounds_surface_test.go each assert their half. Resolve, reply
    // and reopen were the conversation verbs of the proposal era; a rounds-only
    // review has no conversation to reopen, so nothing succeeds it. WHAT
    // REPLACES IT FOR A REVIEWER WHO APPROVED TOO SOON: the seal is in-memory
    // and per-process (see internal/serve/seal.go — one writer, one direction),
    // so ending the editor and running `galley edit` again is a live review on
    // the same document, with the unsent instructions still in pending.json.
    //
    // DONE IS THE OPPOSITE CASE AND IT IS RECORDED RATHER THAN QUIETLY TAKEN.
    // `/_galley/stop` EXISTS and works — `handleStop` runs `EditServer.OnStop`,
    // which is `cmd/galley/edit.go`'s one orderly shutdown, the same path
    // Ctrl-C takes. Its button was withheld before this change, not by it, and
    // deleting the unpressable copy leaves that endpoint with no browser caller
    // at all: the honest way to end a session from the page is now to end it in
    // the terminal that started it, which is what `handleStop` itself tells a
    // caller when no hook is installed. Anyone restoring the terminal bar's
    // verbs should restore Done alone.
    if (bar && spacer) {
      bar.insertBefore(readout, spacer);
    } else if (bar) {
      bar.append(readout);
    }
    return { readout };
  },

  // The live controls the seal replaces. The document's NAME stays — it is
  // what the page is about, sealed or not — and so does THE READOUT, which is
  // where a reopen's reason lands.
  //
  // The readout used to be two elements and this list held one of them: the
  // editor's `connected · saved …` was hidden and the shell's reply line was
  // kept. With one element there is nothing to hide, and the half that had to
  // go is content rather than a box — `paintReadout` prints only what the seal
  // said while `sealed` is true, so the standing sentence ("your edits apply")
  // is off a page whose document is not editable. Hiding the element instead
  // would take the reopen's reason with it.
  // HISTORY SURVIVES THE SEAL, AND ITS LOSS WAS NEVER ARGUED — it was
  // collateral. This list held `this.census.root`, the whole strip, and the
  // History door lives inside that strip because Instructions and History are
  // peers. So sealing a review took the record of it off the page:
  // `layers.mjs` measured a `.gly-versions-open` click hanging for thirty
  // seconds and throwing, on a door that was `display: none`.
  //
  // A SEALED REVIEW IS EXACTLY WHEN SOMEBODY WANTS TO READ WHAT HAPPENED. The
  // reasoning recorded for hiding the strip ended "the single press that
  // changes that is the press that brings the door back" — and reopen was
  // deleted afterwards. Hiding was acceptable BECAUSE there was a way back;
  // there is no way back. This codebase already names that shape: an exemption
  // whose subject is deleted must be deleted with it.
  //
  // THE COUNT GOES AND THE DOOR STAYS, which is why this is a list of CHILDREN
  // now rather than the container. `.gly-census-count` opens the instruction
  // list, and on a sealed page every verb on every one of those cards is dead,
  // so the door leads to a surface nothing can be done on. History is the
  // opposite: it is a reading surface, and reading is the only thing left.
  //
  // `.gly-capture-open` IS DELIBERATELY NOT HERE. It left the strip, so hiding
  // the container never reached it either — and `paintCaptureVerb` re-derives
  // its flag on every paintSurfaces and reads `this.sealed` as part of the
  // answer. Adding it would be a second writer of one control's dead state,
  // which is the pair-of-writers shape SEAL_ONLY_VERBS exists to keep to one.
  //
  // The strip's own box stays on the page carrying one live chip. That is
  // deliberate rather than tolerated: moving the door OUT of the strip was the
  // other exit, and it would put a view switch among the controls after the
  // spacer — the placement `+ Instruction` was measured into and out of, four
  // checks red at once. One chip in a group of view doors is a smaller lie
  // than a view door filed with the verbs.
  sealHides(this: AppShell): (HTMLElement | null)[] {
    return [
      this.census && this.census.count,
      this.modeUI && this.modeUI.toggle,
      this.modeUI && this.modeUI.hold,
      this.revise,
    ];
  },

  paintSeal(this: AppShell) {
    if (!this.sealUI) {
      return;
    }
    const sealed = !!this.sealed;
    for (const el of this.sealHides()) {
      if (!el) {
        continue;
      }
      el.classList.toggle('gly-seal-off', sealed);
      // A HIDDEN CONTROL IS STILL A CONTROL TO THE KEYBOARD — the fold
      // clusters' lesson, and the reserved box's. `display: none` takes it out
      // of the tab order on its own, but the two claims must not both rest on
      // one stylesheet rule.
      if (el instanceof HTMLButtonElement) {
        el.disabled = sealed;
      }
    }
    if (!sealed) {
      // HANDED BACK TO THEIR OWN PAINTERS, never left at `false`. hold is
      // disabled on ask and Revise is disabled while a command runs; a blanket
      // re-enable here would be this function inventing a second, wrong answer
      // to a question two others already answer correctly.
      this.paintMode();
      this.paintRevise();
    }
    this.sealUI.readout.classList.toggle('gly-seal-off', !sealed);
    this.sealUI.readout.textContent = sealed
      ? sealLine(
          this.sealVerdict,
          this.sealAt,
          this.sealEntrusted,
          this.sealOutstanding,
          this.sealLanding,
        )
      : '';
    this.applySealedVerbs();
  },

  // A SEALED REVIEW SHOWS ITS RECORD BUT TAKES NO INPUT. The rail keeps
  // rendering — the conversations and the log are the record — and every verb
  // on it goes dead, plus the reply boxes, which are input by a different name.
  //
  // Queried off the DOCUMENT rather than off draftRoots(), because the bubble
  // is a body-level surface with the same verbs on it and a sealed page must
  // not leave one live way in.
  //
  // IT ONLY EVER TURNS THINGS OFF. `el.disabled = !!this.sealed` looks
  // symmetrical and is not: on a LIVE page it wrote `false` over flags that
  // belong to other owners. It runs at the FOOT of `paintRail`, so it was the
  // last thing every pending refresh did. The instance that was measured is
  // gone with the mechanism that produced it — `dimCard` disabled the four card
  // verbs on every card whose mark was off the viewport, this handed them
  // straight back, and permanently, because `dimCard` early-returned on the
  // class the card still carried. The rail scrolls with the document now, there
  // are no off-viewport cards and no `dimCard`, and the SEAL IS THE ONLY WRITER
  // OF THOSE FOUR FLAGS.
  //
  // THE RULE IS NOT THE INSTANCE, AND THE OTHER OWNERS ARE STILL THERE.
  // `paintCensus`'s `✓ all` (quiet on a document the sweep would leave
  // untouched — a lit one would be a button that lies), the section composer's
  // own button (dead until its block has a key), and every box that disables
  // itself for the flight of its own POST were re-enabled the same way, and
  // would be again. So: return early when the review is live, and let
  // `paintMode`, `paintRevise`, `paintCensus` and the in-flight writers stay the
  // sole owners of the flag — exactly the discipline `paintSeal` already applies
  // to the bar. The unseal transition re-derives them by their owners rather
  // than leaving them stale: `paintSeal` calls `paintMode`/`paintRevise` itself,
  // and `applySeal` repaints the rail and the census on the way back to live.
  // §8a in web/layers.mjs is the gate, and it was shown red first — twice, the
  // second time against a build with this early return deleted, after the fold
  // that gave the first version of it a real flag to read was itself deleted.
  //
  // ONE-WAY IS ONLY HALF A RULE, though, and the half without coverage is the
  // same bug wearing the other face: four of the elements this reaches have no
  // painter the unseal edge runs, and for them the return above is permanent.
  // They are `SEAL_ONLY_VERBS`, and `releaseSealOnlyVerbs` is their owner on
  // the way back — see that constant for which four and why they are not given
  // painters instead.
  applySealedVerbs(this: AppShell) {
    if (!this.sealed) {
      return;
    }
    for (const el of document.querySelectorAll(SEALED_VERBS)) {
      if (
        el instanceof HTMLButtonElement ||
        el instanceof HTMLTextAreaElement
      ) {
        el.disabled = true;
      }
    }
  },

  // THE UNSEAL EDGE, and only the edge. Every other element in SEALED_VERBS is
  // handed back by a painter this edge calls; these four have none — the first
  // three have no painter at all, and the comment button's writers are all
  // gestures, which an edge cannot make. So the seal that killed them is what
  // has to revive them. Called from `applySeal` rather than from `paintSeal`
  // because `paintSeal` runs on every poll, and a per-poll `disabled = false`
  // here would write over the one flag these elements DO set for themselves —
  // the overall input disables itself while its POST is in flight.
  releaseSealOnlyVerbs(this: AppShell) {
    for (const el of document.querySelectorAll(SEAL_ONLY_VERBS)) {
      if (
        el instanceof HTMLButtonElement ||
        el instanceof HTMLTextAreaElement
      ) {
        el.disabled = false;
      }
    }
  },
};
