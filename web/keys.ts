// web/keys.ts owns the rail's keyboard: the Esc chain, and the j/k stepper
// that walks pending suggestions in document order. It is a MIXIN — an object
// of methods `Object.assign`ed onto `App.prototype` in entry.ts — not a
// class of its own, so every method here still reads and writes `this` on
// the live App instance exactly as it did before the move (`this.stepped`,
// `this.suggestions`, `this.cards`, `this.sheetCards`, `this.versionsPanel`,
// and so on).
//
// `this` IS TYPED AGAINST `AppShell` (web/appshell.ts), the shape every
// mixin's `this` shares from here on — see that file's own header for the
// three options weighed and why this one was chosen.

import { stepPending, keyTargetIsEditable } from './rail.ts';
import { revealMark } from './card.ts';
import { closeMenu, menuOpen, moveMenuFocus } from './menu.ts';
import { runFor, markElement } from './runs.ts';
import type { AppShell } from './appshell.ts';

export const keyMethods = {
  // onKey is the rail's keyboard, and it is deliberately quiet while anything
  // is being typed into. The document is contenteditable, so j and k are
  // ordinary letters most of the time; Esc is the way out of the field and into
  // the stepper, which is why it is listed as blurring rather than only
  // dismissing.
  onKey(this: AppShell, event: KeyboardEvent) {
    if (event.metaKey || event.ctrlKey || event.altKey) {
      return;
    }
    if (event.key === 'Escape') {
      // Esc is the one key that acts INSIDE a field: it dismisses what is open,
      // and failing that hands focus back to the page so the rest of these keys
      // become available.
      // THE MENU IS TOPMOST, so it is first — the ordering rule this chain is
      // built on. It is drawn over everything the pointer was on and it is the
      // most recent thing the reviewer opened, so a press that closed anything
      // else while it was up would be closing something they cannot see.
      if (menuOpen(this.menu)) {
        closeMenu(this.menu);
        return;
      }
      this.hideRefusal();
      this.bubble.hide();
      // A BLOCK GRIP'S COMPOSER HANDS FOCUS BACK TO THE GRIP. The keyboard
      // reached the composer through that button, and closing it into nowhere
      // would leave the reviewer to Tab back from the top of the page.
      const opener = this.composer.root.hidden ? null : this.composer.opener;
      this.hideComposer();
      if (opener && opener.isConnected) {
        opener.focus();
      }
      // The verdict menu closes on Esc like every other surface here — a
      // disclosure the press opened, put away by the one key that means "put
      // that away". Reopening is a fresh press.
      if (this.verdictOpen) {
        this.closeVerdictMenu();
        return;
      }
      if (this.sheetOpen) {
        this.closeSheet();
        return;
      }
      // THE ROUNDS, and the position in this chain is the ordering rule this
      // chain is built on: TOPMOST FIRST. The record floats at z-index 44, over
      // the whole-document panel below it, so Esc has to reach it before the
      // panel — otherwise a reviewer with both open presses Esc, watches
      // something they cannot see close, and presses it again.
      if (this.versionsPanel.open) {
        // ONE KEY MAY NOT DO TWO THINGS IN ONE PRESS, and inside History Esc
        // has two meanings stacked: release the change a reviewer pinned, and
        // leave the reading mode. The pin is the innermost, so it goes first —
        // the same TOPMOST-FIRST rule this whole chain is built on, one level
        // further in. `unpin` reports whether it had anything to release, so
        // the outer meaning is not swallowed on a press with nothing pinned.
        if (this.versionsPanel.unpin()) {
          return;
        }
        this.versionsPanel.hide();
        this.paintVersionsButton();
        return;
      }
      // The capture card closes on Esc like every other surface here — it is
      // something on screen that is in the way, and the one key that means
      // "put that away" has to reach it. Its `cancel` button says the same
      // thing for the mouse; the two are one exit spelled for two reviewers.
      if (this.capture && !this.capture.root.hidden) {
        this.closeCapture();
        return;
      }
      const active = document.activeElement;
      // `document.activeElement` is `Element | null` to the DOM's own types,
      // and `blur()` is only on the HTML/SVG element mixin, not on `Element`
      // itself — a real `instanceof` narrowing where this used to read the
      // method's presence off `active.blur`. Every element `keyTargetIsEditable`
      // answers true for (an input, a textarea, a select, or a
      // contenteditable node) is an HTMLElement in this document, so nothing
      // reachable here changes which branch runs.
      if (keyTargetIsEditable(active) && active instanceof HTMLElement) {
        active.blur();
      }
      return;
    }
    // THE MENU'S OWN ARROWS, BEFORE THE EDITABLE GUARD AND BEFORE THE STEPPER.
    // A menu the mouse can reach and the keyboard cannot is half a menu, and
    // `j`/`k` walking the document underneath an open menu would be two
    // surfaces answering one press.
    if (menuOpen(this.menu)) {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        if (moveMenuFocus(this.menu, event.key === 'ArrowDown' ? 1 : -1)) {
          event.preventDefault();
        }
      }
      return;
    }
    if (
      keyTargetIsEditable(event.target) ||
      keyTargetIsEditable(document.activeElement)
    ) {
      return;
    }
    // `a` AND `r` ARE DELETED, AND THE GUARD BELOW OUTLIVES THEM. They ran
    // `decideStepped`, which POSTed /_galley/accept and /_galley/reject — two
    // of the twelve endpoints internal/serve/rounds_surface_test.go asserts
    // are 404. Nothing could reach them: `stepped` is armed only by `step`, and
    // `step` walks `this.suggestions`, which is empty by construction on the
    // rounds-only wire), so nothing is lost: there is no verdict on an
    // instruction. The rail's card verbs are edit and delete, and they are on
    // the card.
    //
    // NOT WHILE HISTORY IS UP. History is a READING mode: `body.gly-history-mode`
    // takes `main` to `display: none` and puts a version's paper in its place,
    // and the whole premise of the surface is that it computes nothing and
    // decides nothing. `j`/`k` are inert there — `markElement` finds nothing
    // inside a hidden `main` — but they still moved `this.stepped`, which is
    // state a reading mode may not write. Every neighbour already knew:
    // `paintReadout`, `paintSurfaces` and the Esc chain all branch on the panel
    // being open. This switch was the one reader that did not ask, which is this
    // codebase's most-repeated shape — the rule spelled in every place but one,
    // and the silent place is the one that costs you.
    //
    // j/k AND `↓ next` WALK THE INSTRUCTION CARDS — see stepOrder, which used
    // to walk `this.suggestions.filter(decidable)` over a wire that carries no
    // suggestions, and so reached nothing at all. The note that stood here
    // named both exits ("point the stepper at the instruction cards or take
    // that check with it") and this is the first of them.
    if (this.versionsPanel.open) {
      return;
    }
    switch (event.key) {
      case 'j':
        event.preventDefault();
        this.step(1);
        break;
      case 'k':
        event.preventDefault();
        this.step(-1);
        break;
      default:
        break;
    }
  },

  // stepOrder is the runs `j`/`k` and `↓ next` walk: the INSTRUCTIONS ON THE
  // PAGE, in the order the reviewer sees them.
  //
  // IT USED TO BE `this.suggestions.filter(decidable)`, AND IT WAS EMPTY. The
  // comment here said so out loud — "IT IS EMPTY ON THE ROUNDS-ONLY WIRE …
  // and the stepper therefore reaches nothing" — and concluded that stepping
  // "survives its two verbs because stepping is navigation: it asks the server
  // for nothing and cannot fail". Cannot fail is true and is not the standard:
  // two keys and a labelled button in the bar advertised a way through the
  // review and did nothing at all when pressed. This codebase already has the
  // sentence for that — a door that answers the press and shows nothing is
  // worse than one that refuses — and it was written about a control that at
  // least ran its handler.
  //
  // The population it should always have walked is the one the rail exists to
  // hold: the reviewer's live instructions, beside the text they are about.
  // `pendingView` is `{instructions, blocks}`, so those ARE the pending view.
  //
  // READ OFF THE PAINTED CARDS RATHER THAN RE-DERIVED FROM `this.comments`,
  // and that is the whole reason it cannot drift: the rail's stacker has
  // already sorted them into document order and dropped the ones with nowhere
  // to be, so this is exactly what is on screen. Re-deriving the order here
  // would be a second spelling that agrees until the day a placement rule
  // changes on one side only — this repository's most-recorded defect.
  //
  // The SHEET is the fallback surface and not a second population: below the
  // rail's breakpoint the rail's cards are not on the page and the sheet's are
  // the same instructions on the surface that is.
  //
  // IT WALKS THREAD KEYS AND NOT RUNS, which is the correction that made this
  // work at all. A run means a MARK, and `!!thread.run` is not "does this
  // instruction have a place" — CLAUDE.md records that exact wrong predicate
  // costing every block-anchored conversation its card. Keying on runs here
  // repeated it: measured on the real fixture, the rail held two instruction
  // cards and BOTH reported `data-run` absent, so a run-keyed stepper was
  // still walking an empty list — the same defect one population over. A key
  // is the thread's own stable identity and every card has one.
  stepOrder(this: AppShell): string[] {
    const surface = this.cards.length ? this.cards : this.sheetCards;
    const seen = new Set<string>();
    const out: string[] = [];
    for (const card of surface) {
      const key = card.thread.key;
      if (key && !seen.has(key)) {
        seen.add(key);
        out.push(key);
      }
    }
    return out;
  },

  // step moves to the next pending mark and shows it wherever the current
  // surface shows things: the card flashes when the rail is open, and the
  // bubble opens on the mark itself when it is not.
  step(this: AppShell, direction: 1 | -1) {
    const next = stepPending(this.stepOrder(), this.stepped, direction);
    if (!next) {
      return;
    }
    this.stepped = next;
    // The sheet's cards too: with the sheet open it is the surface showing the
    // list, and outlining a card behind it would be outlining nothing.
    const all = this.cards.concat(this.sheetCards);
    for (const card of all) {
      card.el.classList.toggle('gly-stepped', card.thread.key === next);
    }
    const card = all.find((c) => c.thread.key === next);
    if (!card) {
      return;
    }
    // A MARK IF THERE IS ONE, THE CARD IF THERE IS NOT — and the second half is
    // not a fallback for a failure, it is the ordinary case for an instruction
    // on a block or on the whole document. Those have no mark by construction
    // (that is what docmodel.Note exists for), so "step to the mark" would
    // silently skip every one of them.
    //
    // `suggestion: null` is not a gap being papered over: `runFor` takes the
    // exact-run branch whenever `run` is set, and the loose-peer fallback
    // beside it exists for a mark-derived suggestion that lost its run. An
    // instruction's run is the thread's, minted server-side, so the exact
    // branch is the only one that can apply.
    const run = card.run
      ? runFor(this.runsNow(), { run: card.run, suggestion: null })
      : null;
    const el = run ? markElement(this.editor.view, run) : null;
    if (!el) {
      // flashThreadCard is the ONE implementation of "show me that card",
      // already used by the click path from a mark in the prose. Reaching for
      // it here rather than scrolling the element by hand is what keeps one
      // reveal gesture in the product.
      this.flashThreadCard(next);
      return;
    }
    revealMark(el);
    this.scheduleAnchors();
    if (!this.rail.root.hidden) {
      return;
    }
    // No rail to flash a card in: open the bubble on the mark, which carries the
    // same metadata line and the same two verbs.
    const found = this.bubble.markAt(el);
    if (found) {
      this.bubble.show(el, found);
    }
  },
};
