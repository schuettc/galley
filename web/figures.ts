// web/figures.ts owns the three ways a reviewer starts an instruction from a
// PLACE rather than from a selection: a figure's ⊕ region button, a
// heading's § section grip, and a code block's {} grip.
//
// It is a MIXIN — an object of methods `Object.assign`ed onto `App.prototype`
// in entry.ts — not a class of its own, so every method here still reads and
// writes `this` on the live App instance exactly as it did before the move
// (`this.editor`, `this.comments`, `this.composer`, `this.grip`, and so on).
// `this` IS TYPED AGAINST `AppShell` (web/appshell.ts) — see that file's own
// header for the this-typing decision.
//
// GRIPS WRITE. Do not confuse this file with web/figure.ts (singular), whose
// NodeViews are presentation only and explicitly forbidden from writing —
// see that file's header. This one dispatches transactions, opens the
// composer, and posts instructions; it is a different mechanism on purpose.
//
// indexOfChild, GRIP_GUTTER_PX and sectionSpan are private to this module in
// the sense that matters: each has exactly one remaining caller, and all of
// those callers are among the methods below. sectionSpan and codeBlockPos are
// exported only because web/probe.mjs tests them directly in isolation.

import { flash, motion } from './card.ts';
import { pickRegion } from './figure.ts';
import { TextSelection } from '@tiptap/pm/state';
import { coerceLevel } from './heading.ts';
import type { EditorView } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';
import type { AppShell, Thread } from './appshell.ts';
import type { BlockRef, Region } from './wire';

// How far into the left gutter the section grip sits, from the heading's own
// left edge. The document column has 20px of padding around it (see editor.css),
// so this places the button clear of the text without leaving the column.
const GRIP_GUTTER_PX = 26;

// A `.gly-figure` element, carrying the block it is currently armed for.
// `__glyBlock` is read at CLICK time rather than bind time (see armFigure),
// so it has to live on the element itself rather than in the closure that
// built the button — and it is optional because the element exists before
// the first paint has told it what block it is.
type FigureElement = HTMLElement & { __glyBlock?: BlockRef | null };

export const figureMethods = {
  // --- figures, and the regions on them ---
  //
  // The NodeViews in figure.ts draw pictures and nothing else — that is their
  // whole contract, and the reason a rendered fence cannot rewrite one. Every
  // verb a figure offers is therefore fitted from OUT here, where the App
  // already owns the pending state, the composer and the network.
  //
  // Paired by DOCUMENT ORDER rather than by asking each element where it is.
  // A NodeView with no contentDOM has no honest answer to posAtDOM, and the two
  // lists — figure-shaped nodes in the document, .gly-figure elements in the
  // DOM — are produced by the same traversal in the same order, so pairing them
  // is exact. A nested figure (a diagram inside a list item) pairs too, and
  // simply gets no ⊕: it is not a top-level block, so the Go side has no key
  // for it and a thread could not be filed against it.
  figurePairs(
    this: AppShell,
  ): Array<{ node: PMNode; pos: number; index: number; el: FigureElement }> {
    const doc = this.editor.state.doc;
    const nodes: Array<{ node: PMNode; pos: number; index: number }> = [];
    doc.descendants((node, pos, parent) => {
      const figure =
        node.type.name === 'image' ||
        (node.type.name === 'codeBlock' && node.attrs.language === 'mermaid');
      if (figure) {
        nodes.push({
          node,
          pos,
          index: parent === doc ? indexOfChild(doc, pos) : -1,
        });
      }
      return true;
    });
    const els: FigureElement[] = Array.from(
      this.editor.view.dom.querySelectorAll<HTMLElement>('.gly-figure'),
    );
    if (els.length !== nodes.length) {
      // The DOM and the document disagree, which happens for one frame while a
      // NodeView is being rebuilt. Painting from a mismatched pairing would put
      // a pin on the wrong picture, so this paint is skipped and the next one
      // (every pending refresh, every update) does it.
      return [];
    }
    return nodes.map((n, i) => ({ ...n, el: els[i] }));
  },

  paintFigures(this: AppShell) {
    for (const pair of this.figurePairs()) {
      const ref = this.blocks.find((b) => b.index === pair.index) || null;
      this.armFigure(pair.el, ref);
      this.paintPins(pair.el, ref);
    }
  },

  armFigure(this: AppShell, el: FigureElement, ref: BlockRef | null) {
    let button = el.querySelector<HTMLButtonElement>(
      ':scope > .gly-region-button',
    );
    if (!ref) {
      // No key on the Go side — a nested figure, or one the last pending
      // refresh has not seen. Offering ⊕ here would open a composer that
      // cannot file anything.
      if (button) {
        button.remove();
      }
      return;
    }
    if (!button) {
      button = document.createElement('button');
      button.type = 'button';
      button.className = 'gly-region-button';
      // §6's verbatim affordance.
      button.textContent = '⊕ comment on a region';
      button.addEventListener('mousedown', (event) => event.preventDefault());
      button.addEventListener('click', () => {
        // Read at CLICK time, not at bind time: the block key is a content
        // hash and moves when the figure's own markdown changes.
        const live = el.__glyBlock;
        if (!live) {
          // Unreachable while this button exists — armFigure removes it the
          // moment `ref` goes false — but `__glyBlock` is optional on the
          // element's own type, so this is what the type checker needs to
          // hand `live` to openRegionComposer as a real BlockRef below.
          return;
        }
        pickRegion(el, (region) => this.openRegionComposer(el, live, region));
      });
      el.appendChild(button);
    }
    el.__glyBlock = ref;
  },

  // The pins, drawn from the thread list — which is why resolving one removes
  // it with no extra code: the resolve round-trips, the thread stops being
  // reported open, and the next paint has nothing to draw.
  //
  // Percentages, not pixels. The rectangle is stored as fractions of this box
  // and rendered as percentages OF this box, so a reflow never enters into it —
  // there is nothing to re-measure on resize, and so nothing that can be
  // measured wrong.
  paintPins(this: AppShell, el: FigureElement, ref: BlockRef | null) {
    let layer = el.querySelector<HTMLElement>(':scope > .gly-region-pins');
    const pins = ref
      ? this.comments.filter(
          (t): t is Thread & { region: Region } =>
            !!t.region && t.anchorKey === ref.key,
        )
      : [];
    if (!pins.length) {
      if (layer) {
        layer.remove();
      }
      return;
    }
    if (!layer) {
      layer = document.createElement('div');
      layer.className = 'gly-region-pins';
      el.appendChild(layer);
    }
    layer.textContent = '';
    for (const thread of pins) {
      const pin = document.createElement('button');
      pin.type = 'button';
      pin.className = 'gly-region-pin';
      pin.style.left = `${thread.region.x * 100}%`;
      pin.style.top = `${thread.region.y * 100}%`;
      pin.style.width = `${thread.region.w * 100}%`;
      pin.style.height = `${thread.region.h * 100}%`;
      const said =
        (thread.entries && thread.entries[0] && thread.entries[0].text) || '';
      pin.title = said || 'an instruction on this region';
      pin.setAttribute('aria-label', `instruction on a region: ${said}`);
      pin.addEventListener('mousedown', (event) => event.preventDefault());
      pin.addEventListener('click', () => this.flashThreadCard(thread.key));
      layer.appendChild(pin);
    }
  },

  // A pin cannot "reveal" the way a mark does — there is no run and no span to
  // scroll to, and the figure is already on screen or the pin could not have
  // been clicked. What the reviewer wants is the other end of the pairing: WHICH
  // card is this. So the card is scrolled into the rail's view and rung.
  // WHICHEVER SURFACE IS ON SCREEN, not the rail. This looked the card up in
  // `this.rail.root` alone and returned in silence when it was not there — so
  // below the breakpoint, where the card is in the sheet, a figure's pin
  // carried a tooltip promising a conversation and did nothing when tapped.
  // The sheet is asked FIRST and only while it is open, for the same reason
  // draftRoots puts it first: it renders the rail's threads too, so the two
  // surfaces hold cards under the same key and the answer has to be the one
  // the reviewer is looking at.
  //
  // And when the sheet is CLOSED below the breakpoint the pin opens it, because
  // "nothing happened" is the defect and a surface one tap away is not the same
  // as no surface at all.
  flashThreadCard(this: AppShell, key: string) {
    const find = (root: HTMLElement | null): HTMLElement | undefined =>
      root
        ? Array.from(root.querySelectorAll<HTMLElement>('.gly-thread')).find(
            (c) => c.dataset.key === key,
          )
        : undefined;
    if (this.surfaces().bar && !this.sheetOpen) {
      this.openSheet();
    }
    const el =
      (this.surfaces().sheet && find(this.sheet.body)) || find(this.rail.root);
    if (!el) {
      return;
    }
    el.scrollIntoView({ behavior: motion(), block: 'nearest' });
    flash(el);
  },

  openRegionComposer(
    this: AppShell,
    figureEl: HTMLElement,
    ref: BlockRef,
    region: Region,
  ) {
    const c = this.composer;
    this.hideComposer();
    c.root.hidden = false;
    c.bar.hidden = true;
    c.button.hidden = true;
    c.deny.hidden = true;
    c.form.hidden = false;
    c.input.value = '';
    c.block = { key: ref.key, label: ref.label, region };
    // §6: "the composer opens beneath it". Beneath the FIGURE rather than at
    // the rectangle, because a composer over the picture covers the thing the
    // note is about — which is the rule placeComposer now states for every
    // other opening too, so this one goes through it and inherits the flip.
    const box = figureEl.getBoundingClientRect();
    this.placeComposer(box, box, 6);
    // A region has no phrase to quote — the anchor is a rectangle on a picture
    // — so the head says the kind and stops. Quoting the figure's label here
    // would read as "an instruction about those words" over an image.
    this.headComposer('');
    c.input.focus();
  },

  // --- the section grip ---
  //
  // §5: hovering a section shows a § in the left gutter beside its heading;
  // clicking it selects the heading through the last block before the next
  // heading, and opens the composer on the whole section. (It used to open
  // with Strike disabled; the button is retired — the trail cut.)
  //
  // One button, moved, and positioned in DOCUMENT coordinates — see
  // makeGripButton and seatGrip, which the code-block grip shares.
  makeGrip(this: AppShell): HTMLButtonElement {
    return makeGripButton(this.editor.view.dom, {
      className: 'gly-grip',
      glyph: '§',
      title: 'comment on this whole section',
      pick: (target) => target.closest<HTMLElement>('h1,h2,h3,h4,h5,h6'),
      place: (el) => this.placeGrip(el),
      open: (pos) => this.openSectionComposer(pos),
    });
  },

  placeGrip(this: AppShell, headingEl: HTMLElement | null) {
    const grip = this.grip;
    if (!headingEl) {
      grip.hidden = true;
      return;
    }
    let pos: number;
    try {
      // posAtDOM(el, 0) is the position INSIDE the heading; one back is the
      // position before the node itself, which is what sectionSpan resolves.
      pos = this.editor.view.posAtDOM(headingEl, 0) - 1;
    } catch {
      grip.hidden = true;
      return;
    }
    if (!sectionSpan(this.editor.state.doc, pos)) {
      // A heading nested in a list item or a blockquote: not a top-level block,
      // so it has no key on the Go side to file a thread against.
      grip.hidden = true;
      return;
    }
    seatGrip(grip, headingEl, pos);
  },

  // --- the code-block grip ---
  //
  // A fence is read-only to TYPING and stays that way, but it is an
  // addressable block on the Go side like any other (suggest.Blocks filters
  // only Note, and AnchorBlock is documented as covering a code fence), so it
  // can carry a whole-block instruction. This grip is the ONLY path to one: a
  // SELECTION touching a fence still gets the deny line, because a range
  // comment writes a highlight mark and a `code: true` node carries none.
  //
  // Its own glyph, never the section's §. The two grips sit in the same gutter
  // a few lines apart, and one affordance meaning two things is the fault this
  // codebase files under one-surface-one-language.
  makeCodeGrip(this: AppShell): HTMLButtonElement {
    return makeGripButton(this.editor.view.dom, {
      className: 'gly-grip gly-code-grip',
      glyph: '{}',
      title: 'instruct on this whole code block',
      // A rendered fence is a <pre>. A mermaid fence is a picture with no <pre>
      // in it at all (see web/figure.ts) and keeps its own ⊕ instead, so this
      // selector is also what stops the two affordances stacking on one block.
      pick: (target) => target.closest<HTMLElement>('pre'),
      place: (el) => this.placeCodeGrip(el),
      open: (pos) => this.openCodeBlockComposer(pos),
    });
  },

  placeCodeGrip(this: AppShell, preEl: HTMLElement | null) {
    const grip = this.codeGrip;
    const pos = preEl ? codeBlockPos(this.editor.view, preEl) : null;
    if (!preEl || pos === null) {
      // A fence nested in a list item or a blockquote: not a top-level block,
      // so it has no key on the Go side to file a thread against — placeGrip's
      // rule, and the same cut suggest.Blocks makes.
      grip.hidden = true;
      return;
    }
    seatGrip(grip, preEl, pos);
  },

  // openCodeBlockComposer selects the whole fence and opens the composer on it
  // in BLOCK mode: a note against the block's key, and no mark anywhere, which
  // is what makes an instruction on read-only text possible at all.
  //
  // It OVERRIDES the placement its own dispatch just triggered, for
  // openSectionComposer's two reasons — the deny (a block note is not a range
  // comment, so the fence rule copied from a mechanism that writes marks does
  // not apply) and the target (the thread anchors to the block key).
  //
  // THE FORM OPENS STRAIGHT AWAY, where the section grip stops at the bar. The
  // bar's `Add instruction` asks the reviewer to confirm a scope the SELECTION
  // left ambiguous; this gesture named its scope by being clicked on one block,
  // the same way a figure's region drag does before openRegionComposer.
  openCodeBlockComposer(this: AppShell, pos: number) {
    const view = this.editor.view;
    const doc = view.state.doc;
    const $pos = doc.resolve(pos);
    const node = $pos.nodeAfter;
    if (!node || node.type.name !== 'codeBlock') {
      // The document moved under the grip between the hover and the click.
      return;
    }
    const index = $pos.index();
    view.dispatch(
      view.state.tr.setSelection(
        TextSelection.between(
          doc.resolve(pos),
          doc.resolve(pos + node.nodeSize),
        ),
      ),
    );
    view.focus();
    // Collapsed again when this composer goes; see releaseGrip.
    this.composer.gripFrom = view.state.selection.from;

    const c = this.composer;
    c.root.hidden = false;
    c.bar.hidden = true;
    c.button.hidden = true;
    c.deny.hidden = true;
    c.deny.textContent = '';
    c.form.hidden = false;
    c.input.value = '';
    // Assigning `value` fires no `input` event, so the box would keep the
    // height the last instruction grew it to. See openComposerForm.
    c.input.dispatchEvent(new Event('input'));
    c.note.textContent = '';
    c.note.classList.remove('gly-quiet');

    // The block key comes from the SERVER's block list — it is a content hash,
    // and nothing in the browser can compute one. openSectionComposer's rule,
    // and its sentence.
    const ref = this.blocks.find(
      (b) => b.index === index && b.kind === 'codeBlock',
    );
    c.block = ref ? { key: ref.key, label: ref.label, region: null } : null;
    if (!ref) {
      c.note.textContent =
        'not in the document yet — it lands on the next sync';
    }
    // NEVER A BARE `= false`: `.gly-composer button` is SEAL_ONLY_VERBS (see
    // web/seal.ts), so enabling send without asking the seal would hand back a
    // control the verdict had killed and no repaint would take away again.
    c.send.disabled = !ref || !!this.sealed;

    // BENEATH THE WHOLE FENCE, measured off its own BOX rather than off its
    // last line — openRegionComposer's rule, for openRegionComposer's reason: a
    // composer over the thing the note is about covers it. A <pre> has padding
    // and a read-only chip below its final glyph, and hanging the popover off
    // that glyph's coordinates put it 11px inside the fence, measured at
    // 1400×1000. The element is the honest bottom.
    const dom = view.nodeDOM(pos);
    if (dom instanceof HTMLElement) {
      const box = dom.getBoundingClientRect();
      this.placeComposer(box, box, 6);
    } else {
      // One frame mid-rebuild has no element for the node; its text still has
      // coordinates, and a composer slightly high beats one that never opens.
      this.placeComposer(
        view.coordsAtPos(pos + 1),
        view.coordsAtPos(pos + node.nodeSize - 1),
        6,
      );
    }
    // No phrase to quote. A whole-fence note is about the block, and a head
    // holding the first 28 characters of a shell command would read as an
    // instruction about those words — the region composer's reasoning.
    this.headComposer('');
    this.codeGrip.hidden = true;
    c.input.focus();
  },

  // openSectionComposer selects the whole section and opens the composer on it.
  //
  // The composer's ordinary placement runs first — our own dispatch fires a
  // selectionUpdate — and then this OVERRIDES two of its verdicts, because a
  // section comment is not a range comment:
  //
  //   deny      a range comment writes a `highlight` MARK over the selection,
  //             which is why a selection touching a fence or a table is
  //             refused. A section comment writes a block note on the heading
  //             and no mark at all, so a section containing a fence is
  //             perfectly commentable and denying it would be a rule copied
  //             from a mechanism it does not apply to.
  //   target    the thread anchors to the heading's block key, not to the
  //             selected prose.
  openSectionComposer(this: AppShell, headingPos: number) {
    const span = sectionSpan(this.editor.state.doc, headingPos);
    if (!span) {
      return;
    }
    const index = this.editor.state.doc.resolve(headingPos).index();
    const view = this.editor.view;
    const doc = view.state.doc;
    view.dispatch(
      view.state.tr.setSelection(
        TextSelection.between(doc.resolve(span.from), doc.resolve(span.to)),
      ),
    );
    view.focus();
    // Collapsed again when this composer goes; see releaseGrip.
    this.composer.gripFrom = view.state.selection.from;

    const c = this.composer;
    const heading = doc.child(index);
    c.root.hidden = false;
    c.bar.hidden = false;
    c.button.hidden = false;
    c.deny.hidden = true;
    c.deny.textContent = '';
    c.form.hidden = true;
    c.note.textContent = '';
    c.note.classList.remove('gly-quiet');

    // The block key comes from the SERVER's block list — it is a content hash,
    // and nothing in the browser can compute one. A heading the last pending
    // refresh has not seen yet (the reviewer just typed it) has no key, so the
    // grip says so rather than filing the thread against the wrong block.
    const ref = this.blocks.find(
      (b) => b.index === index && b.kind === 'heading',
    );
    c.block = ref
      ? { key: ref.key, label: heading.textContent, region: null }
      : null;
    if (!ref) {
      c.button.disabled = true;
      c.note.textContent =
        'not in the document yet — it lands on the next sync';
    } else {
      c.button.disabled = false;
    }

    // BELOW THE HEADING, for placeComposer's reason and one of its own: the
    // grip's whole gesture is "this section", and a popover forty pixels above
    // the heading covers the one line that says which section it is. The
    // second copy of that arithmetic lived here, which is why placeComposer is
    // a method rather than four lines inside placeComposerButton — a rule with
    // two spellings is a rule that will be corrected in one of them.
    const start = view.coordsAtPos(span.from + 1);
    const end = view.coordsAtPos(span.from + 1 + heading.content.size);
    this.placeComposer(start, end);
    this.headComposer(heading.textContent);
    this.grip.hidden = true;
  },
};

// makeGripButton builds ONE gutter grip and wires the hover that MOVES it to
// whichever block the pointer is on. Both grips — the section § and the code
// block's {} — are built through here: a second copy of the mousedown-cancel
// and the leave-guard is a second place for either to drift.
//
// ONE BUTTON, MOVED — not one per block. A gutter button per heading (or per
// fence) is a node count that grows with the document and a hover target that
// has to be kept in sync with every edit, every arriving suggestion and every
// undo; a single element that follows the pointer is none of those.
function makeGripButton(
  dom: HTMLElement,
  spec: {
    className: string;
    glyph: string;
    title: string;
    pick: (target: Element) => HTMLElement | null;
    place: (el: HTMLElement | null) => void;
    open: (pos: number) => void;
  },
): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = spec.className;
  b.textContent = spec.glyph;
  b.hidden = true;
  b.title = spec.title;
  // The editor keeps its selection only while it keeps focus, and the grip is
  // about to replace that selection with its block's — so the mousedown that
  // would blur it is cancelled, the same way the composer's is.
  b.addEventListener('mousedown', (event) => event.preventDefault());
  b.addEventListener('click', () => {
    const pos = Number(b.dataset.pos);
    if (Number.isFinite(pos)) {
      spec.open(pos);
    }
  });
  document.body.appendChild(b);

  // The grip sits in the gutter, ~26px LEFT of the block — outside the editor's
  // DOM, with empty space between the two. A bare mouseleave that hides unless
  // relatedTarget IS the grip dismissed it the instant the pointer crossed that
  // gap, so the button could never be reached. Instead, leaving the column
  // schedules a hide a beat later; entering the grip cancels it; leaving the
  // grip reschedules it. The pointer can cross the gutter without the button
  // vanishing under it.
  let hideTimer: ReturnType<typeof setTimeout> | null = null;
  const cancelHide = () => {
    if (hideTimer !== null) {
      clearTimeout(hideTimer);
      hideTimer = null;
    }
  };
  const scheduleHide = () => {
    cancelHide();
    hideTimer = setTimeout(() => spec.place(null), 220);
  };
  dom.addEventListener('mouseover', (event) => {
    const target = event.target instanceof Element ? event.target : null;
    const el = target ? spec.pick(target) : null;
    if (el && dom.contains(el)) {
      // On a block: show at once, and cancel any pending hide.
      cancelHide();
      spec.place(el);
    } else {
      // Off the block but still inside the column (the left padding between
      // the text and the gutter): defer the hide so the pointer can reach the
      // grip through the gap instead of dismissing it on the way.
      scheduleHide();
    }
  });
  dom.addEventListener('mouseleave', () => scheduleHide());
  b.addEventListener('mouseenter', cancelHide);
  b.addEventListener('mouseleave', () => scheduleHide());
  return b;
}

// seatGrip puts a grip in the gutter beside the block it names, and records the
// position that block starts at for the click to read.
//
// DOCUMENT coordinates (absolute + scrollY) rather than viewport ones, so
// scrolling moves it with its block for free. That is not a micro-optimisation:
// §2 forbids per-frame reflow, and a fixed-position grip would need re-measuring
// on every scroll frame to stay beside the block it names.
function seatGrip(grip: HTMLButtonElement, el: HTMLElement, pos: number) {
  const box = el.getBoundingClientRect();
  grip.dataset.pos = String(pos);
  grip.style.top = `${box.top + window.scrollY}px`;
  grip.style.left = `${box.left + window.scrollX - GRIP_GUTTER_PX}px`;
  grip.hidden = false;
}

// codeBlockPos is the position BEFORE the TOP-LEVEL codeBlock a <pre> renders,
// or null for anything else — a fence inside a list item, or a <pre> the
// document no longer has a node for.
//
// Two candidates rather than placeGrip's flat `- 1`, and the difference is the
// element: a heading IS its own contentDOM, so posAtDOM(el, 0) is reliably the
// position inside it, while a <pre>'s content lives in the <code> within. Which
// of the two positions a browser hands back for the wrapper is not worth
// depending on, so the candidate that actually resolves to a top-level
// codeBlock wins and nothing else is accepted.
//
// Exported only because web/probe.mjs drives it directly, with a stub view over
// a real document — sectionSpan's reason, and the same trade.
export function codeBlockPos(view: EditorView, el: HTMLElement): number | null {
  let inside: number;
  try {
    inside = view.posAtDOM(el, 0);
  } catch {
    return null;
  }
  const doc = view.state.doc;
  for (const pos of [inside - 1, inside]) {
    if (pos < 0 || pos > doc.content.size) {
      continue;
    }
    const $pos = doc.resolve(pos);
    const node = $pos.nodeAfter;
    if ($pos.depth === 0 && node && node.type.name === 'codeBlock') {
      return pos;
    }
  }
  return null;
}

// indexOfChild turns a top-level position into the index of the child that
// starts there — the same ordinal suggest.BlockRef.Index carries, because both
// count every top-level block including the notes.
function indexOfChild(doc: PMNode, pos: number): number {
  let at = 0;
  for (let i = 0; i < doc.childCount; i += 1) {
    if (at === pos) {
      return i;
    }
    at += doc.child(i).nodeSize;
  }
  return -1;
}

/**
 * sectionSpan is the span of the section a heading opens: the heading itself,
 * then every following top-level block until a heading of the SAME level or
 * SHALLOWER.
 *
 * LEVEL-AWARE, NOT "THE NEXT HEADING". An h3 nested under an h2 is part of that
 * h2's section, and stopping at it would select the first paragraph of a
 * section the reviewer can plainly see is longer — and then open a thread about
 * that paragraph, labelled with the heading. A wrong anchor that looks right is
 * the failure this whole line of work exists to remove.
 *
 * Top-level only, which is the same cut suggest.Blocks makes on the Go side: a
 * heading nested in a list item or a blockquote is not an addressable block
 * there, so a section grip beside it would have no key to file a thread
 * against.
 *
 * Returns null for a position that is not a top-level heading, so a caller
 * cannot accidentally treat a paragraph as a section.
 *
 * `headingPos` is the position BEFORE the heading node.
 */
export function sectionSpan(
  doc: PMNode,
  headingPos: number,
): { from: number; to: number } | null {
  const $h = doc.resolve(headingPos);
  if ($h.depth !== 0) {
    return null;
  }
  const heading = $h.nodeAfter;
  if (!heading || heading.type.name !== 'heading') {
    return null;
  }
  const level = coerceLevel(heading.attrs.level);
  let to = headingPos + heading.nodeSize;
  for (let i = $h.index() + 1; i < doc.childCount; i += 1) {
    const child = doc.child(i);
    if (
      child.type.name === 'heading' &&
      coerceLevel(child.attrs.level) <= level
    ) {
      break;
    }
    to += child.nodeSize;
  }
  return { from: headingPos, to };
}
