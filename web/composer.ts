// web/composer.ts owns the one popover a reviewer types an instruction
// into — the bar, its deny line, the form, and the note — and everything
// that decides where it sits: placeComposerButton (show/hide/keep off the
// current selection), placeComposer (the below-or-flipped-above arithmetic),
// headComposer (the `INSTRUCTION · ON "…"` head), and the two sends,
// sendComment and fileBlockComment, both POSTing to /_galley/instruct.
// It also owns the refusal note — makeRefusal, refuse, paintRefusal,
// dismissRefusal, pulseFence, hideRefusal — which reads and writes
// `composer.deny` directly, and is grouped here rather than with the bar's
// status line for exactly that reason.
// It is a MIXIN — an object of methods `Object.assign`ed onto
// `App.prototype` in entry.ts — not a class of its own, so every method
// here still reads and writes `this` on the live App instance exactly as
// it did before the move (`this.composer`, `this.editor`, `this.refusal`,
// `this.sealed`, and so on). `this` IS TYPED AGAINST `AppShell`
// (web/appshell.ts) — see that file's own header for the this-typing
// decision.
//
// composerPlacement and docRange are private to this module: composerPlacement
// was a free function in entry.ts with exactly one caller, placeComposerButton,
// and docRange, in turn, was called only from composerPlacement. Both moved
// here with their one caller. sectionSpan moved to web/figures.ts with
// openSectionComposer and placeGrip, its two callers.

import { postJSON } from './net.ts';
import { submitOnEnter, growOnInput, elide, AUTHOR } from './rail.ts';
import { literalHit } from './suggestions.ts';
import type { LiteralHit } from './suggestions.ts';
import { TextSelection } from '@tiptap/pm/state';
import type { EditorState } from '@tiptap/pm/state';
import type { ResolvedPos } from '@tiptap/pm/model';
import type { AppShell } from './appshell.ts';
import type { Region } from './wire';
import type { MenuItem } from './menu.ts';

// How long a refused-keystroke note stays put before the caret moving on can
// dismiss it. See App.dismissRefusal.
const REFUSAL_DWELL_MS = 1500;

/** How much of the anchor the composer's head quotes. A BOUND, and said out
 * loud to be one: the head is one line of chrome in the mono voice, and a
 * reviewer who selects a whole paragraph would otherwise get the paragraph
 * back in 10px capitals above the box they are typing in. 48 is what fits the
 * composer's own 21rem form at that size with room for the fixed words either
 * side. MEASURED, not derived: the head renders at 6.96px per character in this
 * stack, so 336px of form holds 48 of them, and `INSTRUCTION · ON ""` spends 19
 * — which leaves 28 for the quote. The first cut of this said 36 on an
 * estimated character width and the head overflowed by 47px; `rounds-ux.mjs`
 * reads `scrollWidth` against `clientWidth` on a deliberately over-long anchor,
 * which is what caught it. Retune it WITH the head's type or the form's width,
 * never on its own: a bound stated against a specific pair is a bound that lies
 * the moment either of them moves. */
const COMPOSER_QUOTE_CHARS = 28;

// The block this comment will be filed against, when it is not a range
// comment: {key, label, region}. Set by the section grip and by a figure
// region, cleared by every ordinary selection. One field for both because
// they are one write — a note on a block key — differing only in whether a
// rectangle rides with it.
export interface ComposerBlock {
  key: string;
  label: string;
  region: Region | null;
}

// docRange's own coordinate: a block path plus a rune range within that
// block's concatenated text. See docRange's own comment for what each part
// means and why runes rather than UTF-16 units.
export interface DocRange {
  path: number[];
  from: number;
  to: number;
  // toPath is the block the selection ENDS in, present only when that is a
  // different block from the one it starts in. Absent is the ordinary
  // within-a-block case, and the server reads its absence that way.
  toPath?: number[];
}

// The object makeComposer builds and every method in this file reads and
// writes. `range`, `block` and `sending` are not part of the literal makeComposer
// returns — they are written onto it afterwards, by placeComposerButton,
// the grip/figure openers, and sendComment respectively — so they are
// declared here rather than assumed always present.
export interface Composer {
  root: HTMLElement;
  bar: HTMLElement;
  button: HTMLButtonElement;
  deny: HTMLElement;
  form: HTMLElement;
  head: HTMLElement;
  input: HTMLTextAreaElement;
  send: HTMLButtonElement;
  cancel: HTMLButtonElement;
  esc: HTMLElement;
  note: HTMLElement;
  target: string;
  key: string | null;
  block: ComposerBlock | null;
  range?: DocRange | null;
  sending?: boolean;
  // gripFrom is where the selection a GRIP made starts, while its composer is
  // up; null otherwise. See releaseGrip.
  gripFrom?: number | null;
}

export const composerMethods = {
  // --- refused fence edits ---
  //
  // A code fence is read-only (see suggestions.ts). Refusing silently is the
  // failure mode this is here to avoid: a keystroke that does nothing, with no
  // reason given, reads as a broken editor and invites the reviewer to keep
  // trying. So the first refused keystroke says why, right where the caret is,
  // and says what still works.

  makeRefusal(): HTMLElement {
    const el = document.createElement('div');
    el.className = 'gly-refusal';
    el.hidden = true;
    // role=status + aria-live: the refusal is the only feedback the keystroke
    // gets, so it has to reach a screen reader too.
    el.setAttribute('role', 'status');
    el.setAttribute('aria-live', 'polite');
    document.body.appendChild(el);
    return el;
  },

  // refuse is called from filterTransaction — i.e. in the middle of a dispatch,
  // with the view mid-update. Measuring layout (coordsAtPos) there would read a
  // DOM that ProseMirror has not finished restoring, so every part of this that
  // touches the view is deferred by a tick. Holding Backspace refuses many
  // times a second; each one just resets the same note and its timer.
  refuse(this: AppShell, hit: LiteralHit) {
    this.refusalPending = hit;
    if (this.refusalTick) {
      return;
    }
    this.refusalTick = window.setTimeout(() => {
      this.refusalTick = 0;
      const pending = this.refusalPending;
      if (pending) {
        this.paintRefusal(pending);
      }
    }, 0);
  },

  paintRefusal(this: AppShell, hit: LiteralHit) {
    const el = this.refusal;
    this.refusalPending = null;

    // The chip pulses on every refusal, including the ones where the deny line
    // is already saying it. That is not a second voice: the chip never says
    // anything new, it points at the rule that is already written on it.
    this.pulseFence(hit.pos);

    // ONE SENTENCE PER REFUSAL. The composer's deny line renders literalHit's
    // reason for the current SELECTION; this note renders it for the refused
    // KEYSTROKE. Both surfaces are live at the same time — select inside a
    // fence, then press Backspace — and now that they share a predicate they
    // share a sentence, so the reviewer was handed the identical text twice
    // about fifty pixels apart, which reads as two problems rather than one
    // rule. (The Strike button that used to share this rationale is retired;
    // the deny line and this note are the two surfaces left.)
    //
    // The deny line wins: it is already on screen and already positioned against
    // the selection the reviewer made. Compared by TEXT rather than by a flag, so
    // the two can never suppress each other while saying different things — a
    // `join` keystroke under an `inside` deny still gets its own note.
    const deny = this.composer.deny;
    if (!deny.hidden && deny.textContent === hit.reason) {
      this.hideRefusal();
      return;
    }

    // Position FIRST, and unhide only if there is somewhere to put it. An
    // element that unhides without a fresh position keeps the one the LAST
    // refusal left in its inline style — a note about this fence appearing
    // beside a different one, three screens away, which is worse than no note
    // at all. Every position here is a measurement that can throw, so none of
    // them may be assumed.
    const view = this.editor.view;
    let coords: { top: number; bottom: number; left: number } | null = null;
    // The caret is where the reviewer is looking; the fence's first character
    // is the fallback for a refusal the selection is not about (a paste, a
    // drop); the fence's own DOM box is the last resort.
    for (const pos of [view.state.selection.head, hit.pos + 1]) {
      try {
        coords = view.coordsAtPos(pos);
        break;
      } catch {
        coords = null;
      }
    }
    if (!coords) {
      try {
        const dom = view.nodeDOM(hit.pos);
        if (dom instanceof Element) {
          coords = dom.getBoundingClientRect();
        }
      } catch {
        coords = null;
      }
    }
    if (!coords) {
      el.hidden = true;
      return;
    }

    el.textContent = '';
    const why = document.createElement('div');
    why.className = 'gly-refusal-why';
    why.textContent = hit.reason;
    const hint = document.createElement('div');
    hint.className = 'gly-refusal-hint';
    // The REGION's hint, not the fence's. It arrives on the hit for exactly
    // that reason: a table's "edit it in your own editor" is a different
    // sentence from a fence's, and the note must not say the wrong one.
    hint.textContent = hit.hint;
    el.append(why, hint);
    el.style.top = `${coords.bottom + window.scrollY + 6}px`;
    el.style.left = `${Math.max(4, coords.left + window.scrollX)}px`;
    el.hidden = false;
    this.refusalShownAt = Date.now();

    window.clearTimeout(this.refusalTimer);
    this.refusalTimer = window.setTimeout(() => this.hideRefusal(), 6000);
  },

  // dismissRefusal is the AUTOMATIC dismissal — the caret moving on — and it is
  // deliberately not the same thing as hiding the note.
  //
  // Refusing a keystroke makes ProseMirror re-read its selection from the DOM
  // it has just restored, and that arrives here as a selectionUpdate a tick
  // later. Dismissing on it would let a refusal be cancelled by the very event
  // its own refusal caused: in the browser pass the note flashed and vanished
  // before it could be read, intermittently, depending on which of the two
  // timers won. So an automatic dismissal waits until the note is actually on
  // screen and has been readable for a moment; the 6-second timer and an
  // explicit hide are unaffected.
  dismissRefusal(this: AppShell) {
    if (
      this.refusalPending ||
      Date.now() - this.refusalShownAt < REFUSAL_DWELL_MS
    ) {
      return;
    }
    this.hideRefusal();
  },

  pulseFence(this: AppShell, pos: number) {
    let dom: Node | null = null;
    try {
      dom = this.editor.view.nodeDOM(pos);
    } catch {
      dom = null;
    }
    // A MARK IS NOT A NODE, so nodeDOM at a code span's first position hands
    // back the DOM TEXT node, which has no classList and would have dropped the
    // pulse silently for exactly the literal thing that has no chip to fall back
    // on. One step up is the <code> element ProseMirror wrapped it in. One step
    // only: any further and a refusal inside a code span would flash the whole
    // paragraph, and a fence or a figure never gets here because both already
    // arrive as elements.
    //
    // Both branches this file's own header already guarantees are
    // HTMLElement — a text node's parentElement, or the fence/figure element
    // itself — never SVG, so narrowing on `instanceof HTMLElement` rather than
    // the looser `Element` is exact for every real call site and is what
    // gives `offsetWidth` below a type to stand on.
    const target: HTMLElement | null =
      dom instanceof HTMLElement ? dom : (dom && dom.parentElement) || null;
    if (!target) {
      return;
    }
    // Removed and re-added with a forced layout read between, for the same
    // reason flash() does it: re-adding a class that is already there restarts
    // no animation, so a second refused keystroke would pulse nothing.
    target.classList.remove('gly-fence-pulse');
    void target.offsetWidth;
    target.classList.add('gly-fence-pulse');
    window.setTimeout(() => target.classList.remove('gly-fence-pulse'), 700);
  },

  hideRefusal(this: AppShell) {
    this.refusalPending = null;
    window.clearTimeout(this.refusalTimer);
    this.refusal.hidden = true;
  },

  // --- the comment composer ---

  makeComposer(this: AppShell): Composer {
    const root = document.createElement('div');
    root.className = 'gly-composer';
    root.hidden = true;

    // ONE NOUN FOR THE THING THE REVIEWER IS MAKING. The surface is called
    // Instructions, the card it produces says INSTRUCTION, and this button
    // said `comment` — two nouns and a third verb for one object, on the very
    // first gesture a reviewer makes. `comment` was honest about the WIRE
    // (/_galley/instruct still takes `op: 'comment'`, and the server's thread
    // model is a conversation) and that is exactly the wrong audience for a
    // label: the reviewer is not filing a remark for someone to reply to, they
    // are adding an instruction that the next revision discharges. The wire's
    // spelling is deliberately left alone — renaming it would be a protocol
    // change wearing a copy fix's clothes.
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'gly-comment-button';
    button.textContent = 'Add instruction';

    // The Strike button that used to sit beside comment is RETIRED (the
    // 2026-08-15 trail spec): it was a way to REQUEST a deletion when
    // deletions were proposals, and under the reviewer's-hand contract delete
    // just deletes — the button was a second spelling of Backspace, and its
    // cross-block conservatism died with it. Deletion is the keyboard's.
    const bar = document.createElement('div');
    bar.className = 'gly-composer-bar';
    bar.append(button);

    const form = document.createElement('div');
    form.className = 'gly-composer-form';
    form.hidden = true;
    // THE HEAD QUOTES THE ANCHOR, in the card's own chrome voice, because the
    // composer opens BELOW the selection now and the reviewer's eye is in the
    // box rather than on the words. `INSTRUCTION · ON "…"` is the same head the
    // card in the rail will wear a moment later, so the thing being made and
    // the thing that appears are recognisably one object. It is written by
    // whoever places the composer (placeComposerButton, openSectionComposer,
    // openRegionComposer) — the three functions that know what the anchor is.
    const head = document.createElement('div');
    head.className = 'gly-composer-head';
    const input = document.createElement('textarea');
    input.className = 'gly-composer-text';
    input.rows = 5;
    input.placeholder = 'what about it?';
    // AND IT GROWS. Five rows is where it starts; §6 of the live review is
    // that it was also where it ended. See growOnInput — the cap is this box's
    // own `max-height`, not a number here.
    growOnInput(input);
    // THE DEFECT COURT FOUND, AND WHY THE HELPER EXISTS. This box had no
    // keydown at all: a reviewer who typed a comment and pressed Enter — the
    // gesture that files a reply in the card one column over — got nothing,
    // and had to find the button. Enter files, Shift-Enter breaks the line,
    // the same everywhere.
    submitOnEnter(input, () => this.sendComment());
    const send = document.createElement('button');
    send.type = 'button';
    send.className = 'gly-composer-send';
    send.textContent = 'Add instruction';

    // THE WAY OUT WAS A KEY NOBODY WAS TOLD ABOUT. Esc has always reached
    // hideComposer through onKey's topmost-first chain, and a reviewer who had
    // opened the form had exactly two ways to shut it: guess the key, or click
    // somewhere else and hope. A surface that can only be dismissed by a
    // gesture nobody is shown is a surface with no way out, so the verb is
    // VISIBLE and the key is a whisper beside it — both, because the mouse and
    // the keyboard are two reviewers and neither should have to learn the
    // other's exit. It is borderless and muted (destroy weight's quieter
    // cousin): the composer has one loud thing in it and this is not it.
    const cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.className = 'gly-composer-cancel';
    cancel.textContent = 'cancel';

    // A WHISPER, NOT A SECOND CONTROL. Mono 10px, muted, right-aligned — the
    // chrome voice this product uses everywhere it is telling rather than
    // offering. It is a `span` and never a button precisely because pressing
    // it must do nothing: two things doing one job is the fault this codebase
    // records under one-surface-one-language, and `cancel` is the thing.
    const esc = document.createElement('span');
    esc.className = 'gly-composer-esc';
    esc.textContent = 'esc cancels';

    const actions = document.createElement('div');
    actions.className = 'gly-composer-actions';
    actions.append(send, cancel, esc);
    form.append(head, input, actions);

    // The note lives OUTSIDE the form: it carries the comment endpoint's
    // failures, which have to be readable while the form is still hidden.
    const note = document.createElement('div');
    note.className = 'gly-composer-note';

    // Shown INSTEAD of the button where a comment cannot be made: a selection
    // touching a code fence. A comment is not just a note — /_galley/suggest
    // writes a `highlight` mark over the anchored text, and ProseMirror will
    // not carry a mark inside a `code: true` block, so the request fails
    // server-side in a way that reads like a bug. Same predicate the
    // transaction filter uses, so the two cannot disagree about where a fence
    // starts.
    const deny = document.createElement('div');
    deny.className = 'gly-composer-deny';
    deny.hidden = true;

    // A native prompt() would block the whole page and is unreachable from a
    // browser test; an inline composer is neither.
    // deny replaces the whole BAR, not just the comment button: in a fence
    // neither affordance is available, and the reason is the same one for both.
    root.append(bar, deny, form, note);
    // Keeping focus in the editor keeps the selection alive — a blurred
    // ProseMirror selection is not a selection any more.
    root.addEventListener('mousedown', (e) => {
      if (e.target !== input) {
        e.preventDefault();
      }
    });
    document.body.appendChild(root);

    // ONE OPENING, TWO DOORS. `Add instruction` on the popover is one; the
    // right-click menu's `on this passage` is the other, and both have to leave
    // the composer in exactly the same state or the two gestures are two
    // features. See openComposerForm.
    button.addEventListener('click', () => this.openComposerForm());
    send.addEventListener('click', () => this.sendComment());
    // hideComposer and not merely "close the form": cancelling an instruction
    // is abandoning the whole placement, and a composer left showing its bar
    // over a selection the reviewer has finished with is the popover that will
    // not go away.
    cancel.addEventListener('click', () => this.hideComposer());

    return {
      root,
      bar,
      button,
      deny,
      form,
      head,
      input,
      send,
      cancel,
      esc,
      note,
      target: '',
      key: null,
      block: null,
    };
  },

  /**
   * openComposerForm swaps the popover's bar for its form and puts the caret in
   * it. It is the ONE opening, called by the popover's own button and by the
   * right-click menu's passage item.
   *
   * IT IS A NO-OP ON A COMPOSER THAT IS NOT OFFERING ONE. A selection inside a
   * code fence gets the deny line instead of the button (see
   * placeComposerButton), and a menu item that opened a form over a refusal
   * would be offering a write the server will 400 — the deny line's whole job
   * is that the refusal arrives before the typing.
   */
  openComposerForm(this: AppShell) {
    const c = this.composer;
    if (c.root.hidden || !c.deny.hidden) {
      return;
    }
    c.form.hidden = false;
    c.bar.hidden = true;
    c.note.textContent = '';
    c.note.classList.remove('gly-quiet');
    c.input.value = '';
    // Assigning `value` fires no `input` event, so the box would keep the
    // height the LAST instruction grew it to. See growOnInput.
    c.input.dispatchEvent(new Event('input'));
    c.input.focus();
  },

  /**
   * menuItems is what the right-click offers, and the SELECTION DECIDES THE
   * SCOPE — text selected gets an instruction on that passage, nothing selected
   * gets one on the whole document. One gesture, two scopes, no mode: the
   * answer to "which scope" is already on screen before the menu opens, so
   * there is nothing for the reviewer to choose between and nothing to switch.
   *
   * A LIST, NOT A BRANCH THAT RETURNS A POPUP. Court wants more items here
   * later (see the live-review findings, Q4), so this returns rows and
   * web/menu.ts renders however many there are. Adding the next one is a push.
   *
   * THE WHOLE-DOCUMENT ROW IS OMITTED WHERE IT CANNOT LAND. Its card lives in
   * the rail, and `paintSurfaces` hides the rail below the breakpoint and while
   * History is open. A row that opened a card into a hidden column would report
   * success and show nothing; the bar's own control is disabled in the same
   * states, which is the second half of one rule.
   */
  menuItems(this: AppShell): MenuItem[] {
    const items: MenuItem[] = [];
    const { from, to } = this.editor.state.selection;
    // `textBetween` with a space separator, so a selection spanning blocks
    // arrives as one line rather than with the block boundaries in it — the
    // head is chrome and `elide` collapses the rest.
    const said = this.editor.state.doc.textBetween(from, to, ' ').trim();
    if (said) {
      items.push({
        label: 'Instruction on this passage',
        detail: `on "${elide(said, COMPOSER_QUOTE_CHARS)}"`,
        // PLACED FIRST, THEN OPENED. Focus moves to the menu when it opens,
        // and the composer's own blur handler retires a popover that has lost
        // focus — so by the time this runs the popover may be gone. The
        // ProseMirror selection is state and survives the DOM blur, so
        // `placeComposerButton` rebuilds the placement from it; without this
        // line the item silently opens nothing.
        run: () => {
          this.placeComposerButton();
          this.openComposerForm();
        },
      });
    }
    if (!this.rail.root.hidden) {
      items.push({
        label: 'Instruction on the whole document',
        detail: said ? 'not just the selection' : '',
        run: () => this.openCapture(),
      });
    }
    return items;
  },

  placeComposerButton(this: AppShell) {
    const c = this.composer;
    // A region composer is not driven by the selection — it was opened by a
    // drag on a figure, and the caret never moved. Recomputing placement from
    // an empty selection would decide to hide it, which is the composer
    // vanishing the instant it opens.
    if (c.block && c.block.region) {
      return;
    }
    // NOR IS A GRIP'S, while its selection stands. The selection moves under
    // it without the reviewer moving: the comment's own note arrives over the
    // websocket INSIDE the section, often before the send's response, and the
    // grown range used to re-place the composer as a range composer over the
    // section, which dropped the block target and the grip's claim on the
    // selection, so nothing collapsed it (see releaseGrip). Its start is what
    // the reviewer would have to move to make a selection of their own.
    const sel = this.editor.state.selection;
    if (
      !c.root.hidden &&
      c.gripFrom !== null &&
      c.gripFrom !== undefined &&
      !sel.empty &&
      sel.from === c.gripFrom
    ) {
      return;
    }
    // A hidden composer has nothing to keep, so it always re-places.
    //
    // The guard is the KEY and nothing else — notably not the fence verdict,
    // which the pre-merge version also carried. It does not need to: the key is
    // the selection's range, this only runs on selectionUpdate, and whether a
    // range sits in a fence is a function of that range and the document. A
    // range that has not changed has not moved into or out of a fence.
    const next = composerPlacement(
      this.editor.state,
      c.root.hidden ? null : c.key,
    );
    if (next.action === 'hide') {
      this.hideComposer();
      return;
    }
    if (next.action === 'keep') {
      return;
    }
    c.key = next.key;
    c.target = next.target;
    c.range = next.range || null;
    // A fresh placement is a fresh selection, and the block mode belongs to the
    // selection the grip made. Cleared HERE rather than only in hideComposer,
    // because moving the caret out of a section leaves the composer open on the
    // new selection — and a block target left behind would file the next
    // comment against the previous section's heading.
    c.block = null;
    // And the selection is the reviewer's own now, so hiding must keep it.
    c.gripFrom = null;
    c.button.disabled = false;
    // A PLACEMENT OUTRANKS A DEFERRED DISMISSAL. See the blur handler: its
    // zero-timeout check can be queued behind the very selection that opens
    // this composer, and it would then hide a popover the reviewer had just
    // summoned. The gesture that is happening beats the gesture that has
    // already finished.
    window.clearTimeout(this.blurDismiss);
    this.blurDismiss = 0;
    c.root.hidden = false;

    // The comment affordance goes away where a comment cannot be anchored —
    // a fence, a table. `button.hidden` is set as well as the bar's so the
    // DOM says which affordance is unavailable and not merely that a
    // container is hidden.
    c.bar.hidden = next.denied;
    c.button.hidden = next.denied;
    c.deny.hidden = !next.denied;
    c.deny.textContent = next.denyReason;
    c.form.hidden = true;

    // No Strike lines any more: the button is retired (deletion is the
    // keyboard's), so the note is the comment endpoint's alone and starts
    // clean on every fresh placement.
    c.note.textContent = '';
    c.note.classList.remove('gly-quiet');

    const start = this.editor.view.coordsAtPos(next.from);
    const end = this.editor.view.coordsAtPos(next.to);
    this.placeComposer(start, end);
    this.headComposer(next.target);
  },

  // placeComposer is the ONE arithmetic that decides where the popover sits,
  // and it exists because there were two copies of it that disagreed with the
  // design in the same way.
  //
  // IT WAS ANCHORED ABOVE THE SELECTION AND SO COVERED IT. `top = start.top +
  // scrollY - 40` puts the composer's own top forty pixels above the FIRST line
  // of the selection — and the composer is taller than forty pixels the moment
  // its form opens, so the box grows down THROUGH the phrase it is about.
  // Measured on the fixture at 1440×900 before this change: composer top
  // 155.41 against a selection bottom of 215.41, sixty pixels of overlap, with
  // the reviewer typing an instruction about words the popover had hidden.
  //
  // SO IT HANGS OFF THE SELECTION'S END, and it flips above only when there is
  // genuinely no room — a selection near the foot of the window would otherwise
  // put the box off-screen, which is the covered-control defect through the
  // other door. The flip reads the composer's OWN measured height rather than a
  // guessed one, for the reason `--gly-bar-h` exists: a box whose height is a
  // constant somebody typed is a box that is wrong the first time its contents
  // change, and this one changes every time the form opens.
  //
  // NOTHING HERE MAY ANIMATE AND NOTHING MAY REFLOW. The composer is
  // `position: absolute` on `document.body` — never inside `.ProseMirror`,
  // where it would be CONTENT and the next projection would write it to the
  // author's file — so it cannot displace prose, and this writes `top`/`left`
  // and no transition, so opening it moves nothing that was not clicked.
  placeComposer(
    this: AppShell,
    start: { top: number; left: number },
    end: { bottom: number },
    gap = 8,
  ) {
    const c = this.composer;
    const left = `${start.left + window.scrollX}px`;
    // Measured while it is on screen: `hidden` is cleared by every caller
    // before this runs, so the box has a real height to be flipped against.
    const height = c.root.offsetHeight;
    const below = end.bottom + gap;
    const room = window.innerHeight - below >= height;
    const top = room ? below : Math.max(0, start.top - gap - height);
    c.root.style.top = `${top + window.scrollY}px`;
    c.root.style.left = left;
  },

  // headComposer writes the head's sentence. `ON "…"` only where there is
  // something to quote: a whole-section or whole-figure note has no phrase, and
  // a head that quoted an empty string would read as an instruction about
  // nothing. Bounded, because a reviewer may select a paragraph and the head is
  // one line of chrome, not a second copy of the document.
  headComposer(this: AppShell, quote: string) {
    // `elide` and not a second copy of it: the rail's card head and this head
    // quote the SAME anchor a moment apart, and two cuts made in two places is
    // how the composer promises one thing and the card that appears says
    // another. The bound differs (this form is narrower than the card) and is
    // passed; the cut is one function.
    const short = elide(quote, COMPOSER_QUOTE_CHARS);
    this.composer.head.textContent = short
      ? `INSTRUCTION · ON "${short}"`
      : 'INSTRUCTION';
  },

  hideComposer(this: AppShell) {
    this.releaseGrip();
    this.composer.root.hidden = true;
    this.composer.form.hidden = true;
    this.composer.bar.hidden = false;
    this.composer.button.hidden = false;
    this.composer.deny.hidden = true;
    this.composer.deny.textContent = '';
    this.composer.note.textContent = '';
    this.composer.note.classList.remove('gly-quiet');
    this.composer.target = '';
    this.composer.range = null;
    this.composer.key = null;
    this.composer.block = null;
    this.composer.button.disabled = false;
  },

  // releaseGrip collapses the selection a GRIP made, when its composer goes —
  // sent, cancelled, Esc, or dismissed by a click elsewhere.
  //
  // THE SECTION GRIP AND THE CODE GRIP SELECT THEIR WHOLE BLOCK so the reviewer
  // can see what the comment will be about. Nobody swept that selection out by
  // hand, and once the composer is gone it means nothing; left standing, the
  // next keystroke replaced the section. Measured on 73ea80a in rounds-ux:
  // send a section comment, click into the paragraph, type, and the file read
  // `#  The budget is the subject.` — a click into a selection the editor
  // regained focus with did not collapse it.
  //
  // THE CARET STAYS WHERE THE GRIP POINTED, at the selection's start: the
  // section's heading or the fence's first line, which is the place on screen
  // the reviewer was just looking at. Nothing scrolls.
  //
  // ONLY THE GRIP'S OWN SELECTION. `gripFrom` is cleared the moment a fresh
  // placement takes over (the reviewer made a selection of their own), and the
  // start is what is compared because the comment's own note lands INSIDE a
  // section, after its heading, and moves the selection's end but not its
  // start.
  releaseGrip(this: AppShell) {
    const c = this.composer;
    const from = c.gripFrom;
    c.gripFrom = null;
    if (from === null || from === undefined) {
      return;
    }
    const view = this.editor.view;
    const sel = view.state.selection;
    if (sel.empty || sel.from !== from) {
      return;
    }
    view.dispatch(
      view.state.tr.setSelection(TextSelection.create(view.state.doc, from)),
    );
  },

  // (applyStrike lived here until the trail cut. The Strike button was a
  // second spelling of Backspace once deletions applied directly, so the
  // button, its refusal plumbing and suggestions.ts's strike helpers are all
  // retired together — deletion is the keyboard's, and the trail records it.)

  // A comment is a server-side operation like every other suggestion: it
  // highlights the target text in the document AND opens a thread carrying
  // what was said. Both halves are written by /_galley/suggest, so the
  // highlight arrives here over the websocket.
  sendComment(this: AppShell) {
    const c = this.composer;
    const text = c.input.value.trim();
    if (!text) {
      c.note.textContent = 'say something first';
      return;
    }
    // ONE PRESS IS ONE COMMENT, AND THE SECOND PRESS WAITS. Enter files here
    // now (submitOnEnter), and a comment is a server-side mutation — every one
    // of them REBUILDS the whole document (CLAUDE.md). Two presses a beat
    // apart, or a second press while the first POST is still out, filed two
    // identical threads and rebuilt the document twice; nothing in galley
    // un-files a comment. submitOnEnter refuses the auto-repeat, this refuses
    // the deliberate double, and the field is disabled for the flight so the
    // refusal is visible rather than silent — the discipline fileNote already
    // had, spelled the same way on the surface that creates a thread.
    if (c.sending) {
      return;
    }
    c.sending = true;
    c.input.disabled = true;
    c.send.disabled = true;
    // AND THE SEAL WINS OVER THE FLIGHT, the same way it does in `fileNote`.
    // `false` here was written when a verdict could not land underneath a
    // comment; it can now, and this callback runs on EVERY path including the
    // successful one, so a comment that outlived the verdict handed back a live
    // box AND a live send button on a sealed page — re-enabling two controls
    // the seal had just killed, which no repaint would take away again because
    // both are `SEAL_ONLY_VERBS` and the seal is their only owner.
    const settled = () => {
      c.sending = false;
      c.input.disabled = !!this.sealed;
      c.send.disabled = !!this.sealed;
    };
    c.note.textContent = 'sending…';
    c.note.classList.remove('gly-quiet');
    // A BLOCK comment is a different write, not a range comment over a bigger
    // range: it anchors to a block key, where a range comment would put a
    // highlight over the prose. A section thread is one thread about the
    // section rather than a comment on whichever sentence happened to be
    // first; a region thread is the same write with a rectangle on it.
    if (c.block) {
      this.fileBlockComment(c, text, settled);
      return;
    }
    // The range, not the text. Sending the text made the server search for
    // it again, which failed outright whenever the selection occurred twice —
    // selecting "age" in a document containing "image" reported
    // `"age" matched 2 times`. target rides along for the CLI-shaped fallback
    // and for the error message, but path/from/to are what decide.
    const body: {
      op: string;
      target: string;
      text: string;
      author: string;
      path?: number[];
      from?: number;
      to?: number;
      toPath?: number[];
    } = { op: 'comment', target: c.target, text, author: AUTHOR };
    if (c.range) {
      body.path = c.range.path;
      body.from = c.range.from;
      body.to = c.range.to;
      if (c.range.toPath) {
        body.toPath = c.range.toPath;
      }
    }
    postJSON('/_galley/instruct', body)
      .then((res) => {
        if (res.ok) {
          this.hideComposer();
          return this.refreshPending();
        }
        return res.text().then((body) => {
          // The usual failure is honest and worth showing verbatim: the
          // selected text occurs more than once, so the server cannot tell
          // which occurrence the comment is about.
          c.note.textContent = body.trim() || `failed: ${res.status}`;
        });
      })
      .catch((err) => {
        c.note.textContent = `failed: ${err}`;
      })
      // EVERY path, including the successful one: hideComposer leaves the
      // field for the next selection to reuse, and a field left disabled by a
      // comment that WORKED is a composer nobody can type in again.
      .then(settled, settled);
  },

  fileBlockComment(
    this: AppShell,
    c: Composer,
    text: string,
    settled: () => void,
  ) {
    // `c.block` is a fresh narrow, not a new possibility: the one caller
    // (sendComment) only reaches this method inside its own `if (c.block)`,
    // but that narrowing is a fact about `c` at the call site and does not
    // travel across a function boundary — `Composer.block` is still
    // `ComposerBlock | null` here. The guard restates the caller's own
    // invariant rather than changing it.
    if (!c.block) {
      return;
    }
    const body: {
      op: string;
      target: string;
      text: string;
      author: string;
      region?: Region;
    } = {
      op: 'comment_block',
      target: c.block.key,
      text,
      author: AUTHOR,
    };
    // Only when there is one. A block note with no rectangle is a note about
    // the whole block, and sending a zero rectangle for it would draw a pin in
    // the figure's top-left corner on every figure ever commented on.
    if (c.block.region) {
      body.region = c.block.region;
    }
    postJSON('/_galley/instruct', body)
      .then((res) => {
        if (res.ok) {
          this.hideComposer();
          return this.refreshPending();
        }
        return res.text().then((body) => {
          // The honest failure is a block key the document has moved past —
          // someone edited the heading between the grip opening and the send.
          c.note.textContent = body.trim() || `failed: ${res.status}`;
        });
      })
      .catch((err) => {
        c.note.textContent = `failed: ${err}`;
      })
      .then(settled, settled);
  },
};

/**
 * docRange converts the selection into the coordinates the Go side speaks:
 * a block path and a rune range within that block's concatenated text.
 *
 * This is what stops the server having to search for the selected text — the
 * failure that made commenting on "age" impossible in a document containing
 * "image". The browser already knows precisely what was selected; the only
 * job left is to say it in a vocabulary internal/suggest shares.
 *
 * Two details that are not arbitrary. Offsets are RUNES, matching
 * suggest.plainText and markdown.InlineComment.Offset rather than JavaScript's
 * UTF-16 code units — an emoji earlier in the paragraph would otherwise shift
 * every offset after it. And the path counts the same nesting docmodel.Walk
 * does, which holds because the ProseMirror schema deliberately uses
 * docmodel's own node names.
 *
 * A SELECTION THAT SPANS BLOCKS CARRIES `toPath`, and until now it carried
 * nothing at all. This returned null for one, the composer fell back to sending
 * the selected TEXT, and the server searched for that text inside single blocks
 * — the concatenation of two paragraphs is in neither, so `findUnique` reported
 * `matched 0 times` and refused. Every multi-block selection, always, and only
 * after the reviewer had finished typing their instruction.
 */
function docRange(state: EditorState): DocRange | null {
  const { from, to } = state.selection;
  const $from = state.doc.resolve(from);
  const $to = state.doc.resolve(to);
  // BOTH ENDS MUST BE IN TEXT. A selection that starts or ends outside a
  // textblock — dragging across a figure, say — has no offset to report, and
  // guessing one would anchor the comment somewhere plausible and wrong.
  if (!$from.parent.isTextblock || !$to.parent.isTextblock) {
    return null;
  }
  const pathOf = ($pos: ResolvedPos): number[] => {
    const out: number[] = [];
    for (let d = 0; d < $pos.depth; d++) {
      out.push($pos.index(d));
    }
    return out;
  };
  const path = pathOf($from);
  const runeFrom = [...state.doc.textBetween($from.start($from.depth), from)]
    .length;
  if ($from.parent === $to.parent) {
    const selected = state.doc.textBetween(from, to);
    return { path, from: runeFrom, to: runeFrom + [...selected].length };
  }
  // ACROSS BLOCKS: each end is an offset into ITS OWN block, which is what
  // suggest.CommentAcross takes. The end offset is deliberately NOT measured
  // from the start of the selection — the text between the two ends spans
  // block boundaries, and its length has nothing to do with where the
  // selection stops inside the last block.
  const runeTo = [...state.doc.textBetween($to.start($to.depth), to)].length;
  return { path, from: runeFrom, toPath: pathOf($to), to: runeTo };
}

// The verdict composerPlacement hands back: hide the toolbar, leave it
// exactly as it is, or place it afresh with everything a fresh placement
// needs. A discriminated union on `action` rather than one shape with
// optional fields, so a caller that has ruled out 'hide' and 'keep' is left
// holding every 'place' field without a null check.
export type ComposerVerdict =
  | { action: 'hide' }
  | { action: 'keep' }
  | {
      action: 'place';
      key: string;
      from: number;
      to: number;
      target: string;
      range: DocRange | null;
      denied: boolean;
      denyReason: string;
    };

/**
 * composerPlacement decides what the toolbar should show for the current
 * selection: hide it, leave it exactly as it is, or place it afresh.
 *
 * KEYED ON THE SELECTION'S RANGE, NEVER ON ITS TEXT. Two different ranges can
 * read exactly the same — the same phrase as prose and again inside a code
 * fence is the case that broke this — and a text-keyed guard cannot tell them
 * apart, so it skipped the recompute and the toolbar kept the PREVIOUS
 * selection's verdicts and the previous selection's coordinates — measured
 * both ways round when the Strike button still read them, and the deny half
 * of the defect (a stale `denied` over identically-worded text) is exactly as
 * live for the comment affordance now. The bubble also stayed sitting over
 * the old selection.
 *
 * The guard itself is worth keeping rather than recomputing unconditionally:
 * an unchanged selection that re-fires must not reset an open comment form
 * out from under someone mid-sentence. Positions are the honest identity of a
 * selection; text never was.
 *
 * THE VERDICT COMES FROM HERE, and it traces to one predicate. `denied` is
 * the literal region — a fence or a table; no comment can be anchored in one
 * — and `denyReason` is literalHit's own sentence, the same one the refused
 * keystroke renders at the caret: the button the reviewer does not get and
 * the keystroke that gets refused say one thing.
 *
 * Still pure — literalHit reads only the state it is handed, which is what
 * lets probe.mjs drive this with no browser.
 *
 * `prevKey` is the key of what the toolbar is showing now, or null when it is
 * hidden (in which case it always re-places).
 */
export function composerPlacement(
  state: EditorState,
  prevKey: string | null,
): ComposerVerdict {
  const { from, to, empty } = state.selection;
  if (empty) {
    return { action: 'hide' };
  }
  const target = state.doc.textBetween(from, to, ' ').trim();
  if (!target) {
    return { action: 'hide' };
  }
  const key = `${from}:${to}`;
  if (prevKey === key) {
    return { action: 'keep' };
  }
  const literal = literalHit(state.doc, from, to);
  // A COMMENT IS ANCHORED BY THE SERVER, WHICH IS WHY INLINE `code` DOES NOT
  // DENY ONE. literalHit refuses a change wholly inside a code span — the span
  // is literal text, read-only in this editor — but a comment is not an edit
  // to it. The browser posts a range to /_galley/suggest and the server builds
  // the highlight with an explicit mark array, which skips `excludes: '_'`
  // entirely. Measured before this exemption was written: `galley suggest
  // --comment --on retryBudget` on a paragraph reading "The `retryBudget`
  // value controls" writes {==`retryBudget`==} and `galley pending` lists it.
  // Denying it here would take away something that works today, which is the
  // one thing a refusal must never do.
  //
  // A fence and a table still deny, because there the SERVER has nowhere to
  // anchor either — suggest.List only ever reads Block.Inlines.
  //
  // (There is no `refusal` field any more: it carried strikeRefusal's answer
  // for the Strike button, and the button is retired — a keyboard deletion
  // over a literal region is refused by the transaction filter, which says
  // its own sentence at the caret.)
  const denied = !!literal && literal.what !== 'code';
  return {
    action: 'place',
    key,
    from,
    // BOTH ENDS, because the composer hangs off the selection's END now and no
    // longer floats above its start. `range` carries a from/to too, but that is
    // the wire's coordinate — a path plus offsets the SERVER anchors by — and
    // reading it for geometry would be two spellings of one coordinate, which
    // is this codebase's most-repeated defect. These are ProseMirror document
    // positions and `coordsAtPos` is what they are for.
    to,
    target,
    range: docRange(state),
    denied,
    denyReason: denied && literal ? literal.reason : '',
  };
}
