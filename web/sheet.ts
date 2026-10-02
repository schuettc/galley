// web/sheet.ts owns the review sheet: the narrow layout's whole chrome
// (makeBottomBar — the Instructions door, the History chip, `↓ next`), the
// full-screen list surface itself (makeSheet — its head and its body),
// opening and closing it, and painting it: `paintSheet` renders
// `railThreads ∪ overallThreads` through `this.threadCard`. It is a MIXIN — an object of methods `Object.assign`ed
// onto `App.prototype` in entry.ts — not a class of its own, so every
// method here still reads and writes `this` on the live App instance
// exactly as it did before the move (`this.sheet`, `this.sheetOpen`,
// `this.comments`, `this.blocks`, `this.cards`,
// `this.sheetCards`, and so on). `this` IS TYPED AGAINST `AppShell`
// (web/appshell.ts) — see that file's own header for the this-typing
// decision.
//
// makeBottomBar sounds like top-bar chrome by name. It is not: it is the
// narrow layout's whole bar, and every one of its buttons calls into other
// groups (openInstructions, toggleVersions, step) rather than owning any
// chrome of its own — it belongs here, beside the surface it opens.

import { VERSIONS_LABEL, VERSIONS_NAME } from './versions.ts';
import { railThreads, overallThreads, threadPlacement } from './rail.ts';
import type { AppShell, Thread } from './appshell.ts';

// What the sheet's ✕ is called. A constant rather than a literal because the
// same string is the accessible name AND the tooltip, and two spellings of one
// control's name is how they come to disagree.
const SHEET_CLOSE_NAME = 'Close the review list';

// The one asymmetry, stated where the reviewer works. It used to be the
// phase-1 admission ('formatting and structure apply untracked'); the
// reviewer's-hand cut made the honest general sentence the design itself —
// nothing of the reviewer's is tracked, everything of the agent's is a
// proposal. Spelled once, shown twice: in the BAR'S ONE READOUT on a wide
// screen and in the review sheet's head on a narrow one, where the bar has no
// room for it. R1's complaint was that the old sentence scrolled below the fold
// at the bottom of the old panel; chrome does not scroll, so this one must never
// go back into a scrolling column.
//
// IT USED TO BE A BAR CELL OF ITS OWN, `.gly-census-untracked`, beside two more
// — the editor's `connected · saved …` and the shell's reply to a press. Three
// readouts side by side, each ellipsized to a few words: `your edits…`,
// `connected …`, `revision r…`. Three truncated fragments is not three
// readouts, it is one unreadable one, and it is the same incoherence the rail
// had. There is ONE readout now (App.readout), and this sentence is its last
// clause; see paintReadout for the order and why.
export const UNTRACKED_NOTE = 'instructions in this round';

export const sheetMethods = {
  // The 40px bar is the narrow layout's whole chrome, and board 1i says what
  // belongs in it: THE SAME TWO DOORS THE WIDE BAR HAS, ADJACENT AND LABELLED.
  //
  // WHAT 1440px TEACHES, 620px HAS TO REPEAT. `Instructions · N` sat here and
  // `History` sat in the TOP bar, so the two controls the whole product insists
  // are peers were on opposite edges of the screen and nothing a reviewer
  // learned at one width transferred to the other. They are side by side now,
  // in that order, exactly as `.gly-census` carries them above the breakpoint.
  // `Revise · N ▾` stays in the top bar and never leaves sight, which is why it
  // is not repeated down here.
  //
  // EVERY CONTROL IN THIS BAR CARRIES A VISIBLE LABEL. `▤` was retired: it was
  // the second door to the sheet, unlabelled, and the first real review already
  // proved of the History glyph that an accessible name and a tooltip do not
  // make a bare symbol discoverable to a sighted reviewer. Nothing is lost with
  // it — the sheet is what `Instructions` opens at this width now (see
  // openInstructions), which is one door where there were two, and the door
  // says what is behind it.
  //
  // AND THE STEPPER KEEPS ITS WORD. `↑`/`↓` were a pair of glyphs; `↓ next` is
  // one control that reads. Its twin is retired rather than relabelled because
  // `stepPending` WRAPS — `↓ next` reaches every pending mark in the document,
  // in order, and comes back round — so what went is a shortcut and not a
  // destination, and `k` still steps backwards from the keyboard. Four labelled
  // controls do not fit a 390px bar; three do, which is the width the board is
  // drawn at.
  makeBottomBar(this: AppShell): {
    root: HTMLElement;
    count: HTMLButtonElement;
    versions: HTMLButtonElement;
  } {
    const root = document.createElement('div');
    root.className = 'gly-bottombar';
    root.hidden = true;

    const count = document.createElement('button');
    count.type = 'button';
    count.className = 'gly-bar-count';
    count.addEventListener('click', () => this.openInstructions());

    // The bar's own History chip. It is a SECOND ELEMENT and not the top bar's
    // moved down here, because the two bars are painted independently and only
    // ever one of them is on screen (railSurfaces). Both are painted from
    // paintVersionsButton, which is the one place that knows what the door
    // should say and whether something is waiting behind it — a second painter
    // is how the two chips would come to disagree about an arrival.
    const versions = document.createElement('button');
    versions.type = 'button';
    versions.className = 'gly-bar-versions';
    versions.textContent = VERSIONS_LABEL;
    versions.setAttribute('aria-label', VERSIONS_NAME);
    versions.setAttribute('aria-expanded', 'false');
    versions.addEventListener('click', () => this.toggleVersions());

    // The one flexible cell, and it sits AFTER the two doors and BEFORE the
    // stepper — the top bar's own rule (readouts and doors before the spacer,
    // controls after) read at the other end of the page. Nothing in this bar
    // changes width on its own click, so there is no reserve to state here.
    const spacer = document.createElement('span');
    spacer.className = 'gly-bar-spacer';

    const next = document.createElement('button');
    next.type = 'button';
    next.className = 'gly-bar-step';
    next.textContent = '↓ next';
    next.title = 'the next pending mark — wraps at the end of the document';
    next.addEventListener('click', () => this.step(1));

    root.append(count, versions, spacer, next);
    document.body.appendChild(root);
    return { root, count, versions };
  },

  makeSheet(this: AppShell): {
    root: HTMLElement;
    head: HTMLElement;
    body: HTMLElement;
  } {
    const root = document.createElement('div');
    root.className = 'gly-sheet';
    root.hidden = true;
    const head = document.createElement('div');
    head.className = 'gly-sheet-head';
    const body = document.createElement('div');
    body.className = 'gly-sheet-body';
    // The census strip drops this sentence below the breakpoint — it needs a
    // full line it does not have there. The sheet's head carries it instead, so
    // the one asymmetry is never unstated, only relocated.
    const untracked = document.createElement('span');
    untracked.className = 'gly-sheet-untracked';
    untracked.textContent = UNTRACKED_NOTE;
    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'gly-sheet-close';
    close.textContent = '✕';
    // AN ACCESSIBLE NAME, because the glyph is not one. `✕` reads to a screen
    // reader as the character it is — "multiplication x", or nothing at all —
    // and the bottom bar's own rule next door already says every control has to
    // say what it is. That rule retired `▤` outright; this one keeps its glyph
    // because a ✕ closing a panel is a convention a sighted reviewer reads
    // instantly where `▤` was not, and the first real review proved exactly
    // that difference on the History icon. Keeping the glyph and adding the
    // name is the whole of what the two cases have in common.
    close.setAttribute('aria-label', SHEET_CLOSE_NAME);
    close.title = SHEET_CLOSE_NAME;
    close.addEventListener('click', () => this.closeSheet());
    head.append(untracked, close);
    // THE SHEET HAS NO SETTLED REGION. It held resolved conversations, and
    // nothing resolves an instruction any more: a sent one leaves with its
    // round and a retracted one is deleted, so every thread is open.
    //
    // THE SHEET HAS NO CHANGED REGION EITHER, and it is worth saying here as
    // well as in makeRail, because this one had its own justification and that
    // justification is the trap. It read: "the sheet is the narrow layout's
    // whole list, so a trail only reachable at wide would be a record a phone
    // cannot read" — which is a sound argument for putting a HISTORY on both
    // surfaces, and answers the wrong question. Nobody browses the trail on
    // either surface; it is an outgoing message to the agent, and its count
    // rides the button that sends it. See rail.ts's outgoingCounts.
    root.append(head, body);
    document.body.appendChild(root);
    return { root, head, body };
  },

  openSheet(this: AppShell) {
    this.sheetOpen = true;
    this.paintSheet();
    this.paintSurfaces();
  },

  closeSheet(this: AppShell) {
    this.sheetOpen = false;
    this.paintSurfaces();
  },

  // The sheet carries the rail's card list, built by the same function, because
  // a second card renderer is a second set of verbs to keep in step. The only
  // difference is that these cards are in flow (no anchor, no light) and a
  // jump closes the sheet: a full-screen list you have to dismiss by hand after
  // asking it to show you something is a list that wasted the tap.
  paintSheet(this: AppShell) {
    const { body } = this.sheet;
    body.textContent = '';
    // suggestionCard records into this.cards, which paintAnchors positions.
    // Sheet cards are in flow and must never be positioned, so the rail's
    // list is set aside and put back rather than grown. threadCard does the
    // same thing when it is given an anchored placement (`where !== 'anchorless'`)
    // — and, below, an anchored thread genuinely is handed one, so it DOES
    // push into this.cards mid-loop. That is exactly why the save/restore
    // exists rather than "don't pass it an anchor": it is what keeps a sheet
    // card out of paintAnchors' hands regardless of whether the placement
    // underneath it has one.
    const keep = this.cards;
    this.cards = [];
    // NO PROPOSAL LOOP AND NO PAIRING — see paintRailCards, where the same two
    // went for the same reason: the wire carries no suggestions, so there was
    // never a proposal card here for a conversation to be folded into.
    // THE SHEET IS THE WHOLE REVIEW, AND IT IS THE ONLY SURFACE THAT IS. Every
    // thread belongs here — anchored, block-anchored, document-wide
    // and ANCHORLESS alike, which is exactly railThreads ∪ overallThreads — and
    // that last population is the one this list became load-bearing for. The
    // rail holds live work beside the text it is about; a thread whose highlight
    // is gone is beside nothing, so the rail's loop skips it and this is the one
    // place it renders.
    //
    // The PLACEMENT is threadPlacement's verdict, not a hardcoded foot. A
    // hardcoded foot told threadCard every thread was unanchored — which
    // drew a live, on-a-mark conversation exactly like a genuinely gone one
    // (dashed, "not tied to a mark"), and skipped
    // the title and the reveal click threadCard only wires up when it was
    // handed an anchor. No card here lights the prose whatever its placement:
    // the light is the band's — `litRunAt` reads `.gly-rail-band .gly-card` and
    // nothing else — and the sheet covers the prose it would have lit.
    const threads: Thread[] = [
      ...railThreads(this.comments),
      ...overallThreads(this.comments),
    ];
    for (const thread of threads) {
      const el = this.threadCard(thread, threadPlacement(thread, this.blocks));
      el.classList.add('gly-sheet-card');
      body.appendChild(el);
    }
    this.sheetCards = this.cards;
    this.cards = keep;
  },
};
