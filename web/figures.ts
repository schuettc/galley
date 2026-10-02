// web/figures.ts owns the ways a reviewer starts an instruction from a PLACE
// rather than from a selection: the block grip beside every heading, fence,
// table, figure, display-math block and front matter, and a figure's region,
// reached from that grip's composer by Mark a region.
//
// It is a MIXIN — an object of methods `Object.assign`ed onto `App.prototype`
// in entry.ts — not a class of its own, so every method here still reads and
// writes `this` on the live App instance (`this.editor`, `this.comments`,
// `this.composer`, `this.grips`, and so on). `this` IS TYPED AGAINST
// `AppShell` (web/appshell.ts) — see that file's own header for the
// this-typing decision.
//
// GRIPS WRITE. Do not confuse this file with web/figure.ts (singular), whose
// NodeViews are presentation only and explicitly forbidden from writing —
// see that file's header. This one opens the composer and posts
// instructions; it is a different mechanism on purpose.
//
// THE GRIP'S RULES ARE web/grips.ts's: which blocks get one, what it says,
// and how two crowded grips stack. This file measures, asks, and paints.
//
// THE GRIPS ARE ONE LAYER, A SIBLING OF `.ProseMirror` inside `#editor` and
// never a child of it: anything appended inside `.ProseMirror` is content,
// and the next projection writes it to the author's file. Each grip is
// absolutely positioned, so the layer moves nothing in the document.
//
// A GRIP NEVER TOUCHES THE SELECTION. It used to select its whole block to
// show what the instruction would be about, and the selection outlived the
// composer: the next keystroke replaced the section. The scope is a
// decoration now (web/scope.ts) and the selection stays the reviewer's.
//
// indexOfChild and sectionSpan are private to this module in the sense that
// matters: their callers are among the methods below. sectionSpan is exported
// only because web/probe.mjs tests it directly in isolation.

import { flash, motion } from './card.ts';
import { pickRegion } from './figure.ts';
import { coerceLevel } from './heading.ts';
import { gripFace, gripLabel, gripTargets, stackGrips } from './grips.ts';
import type { GripTarget } from './grips.ts';
import { scopeKey } from './scope.ts';
import type { EditorView } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';
import type { AppShell, Thread } from './appshell.ts';
import type { BlockRef, Region } from './wire';

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
  // simply gets no pins: it is not a top-level block, so the Go side has no
  // key for it and a thread could not be filed against it.
  figurePairs(
    this: AppShell,
  ): Array<{ node: PMNode; pos: number; index: number; el: HTMLElement }> {
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
    const els: HTMLElement[] = Array.from(
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
      this.paintPins(pair.el, ref);
    }
  },

  // The pins, drawn from the thread list — which is why resolving one removes
  // it with no extra code: the resolve round-trips, the thread stops being
  // reported open, and the next paint has nothing to draw.
  //
  // Percentages, not pixels. The rectangle is stored as fractions of this box
  // and rendered as percentages OF this box, so a reflow never enters into it —
  // there is nothing to re-measure on resize, and so nothing that can be
  // measured wrong.
  paintPins(this: AppShell, el: HTMLElement, ref: BlockRef | null) {
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

  // --- the block grip ---
  //
  // ONE BUTTON PER BLOCK, ALWAYS VISIBLE, in one layer. The grips used to be
  // one hover button per kind, moved to whichever block the pointer was on and
  // hidden 220ms after it left — a race to cross the gutter, and nothing at all
  // without a pointer. A button per block costs a node per heading, and buys a
  // control a reviewer can see, reach and Tab to.

  // makeGripLayer builds the layer, once, after `.ProseMirror` in `#editor`.
  // The click is delegated off the layer because paintGrips replaces buttons
  // as blocks come and go, and it reads the grip's block at CLICK time: a
  // position is stale the moment the document changes.
  makeGripLayer(this: AppShell): HTMLElement {
    const layer = document.createElement('div');
    layer.className = 'gly-grips';
    layer.addEventListener('click', (event) => {
      const grip =
        event.target instanceof Element
          ? event.target.closest<HTMLButtonElement>('.gly-block-grip')
          : null;
      if (!grip) {
        return;
      }
      const index = Number(grip.dataset.index);
      const target = gripTargets(this.editor.state.doc).find(
        (t) => t.index === index && t.kind === grip.dataset.kind,
      );
      if (target) {
        this.openBlockComposer(target, grip);
      }
    });
    (this.editor.view.dom.parentElement || document.body).appendChild(layer);
    return layer;
  },

  // paintGrips reconciles one grip per target and puts each beside its block.
  //
  // BUTTONS ARE REUSED BY INDEX AND KIND, and moved only when they are out of
  // order, so a grip holding focus keeps it across a repaint — paintRail's
  // lesson, where a rebuilt card took the caret out of a reply.
  //
  // EVERY TOP IS READ BEFORE ANY IS WRITTEN, paintAnchors' rule: interleaving
  // the two forces a layout per grip. Tops are relative to `#editor`, which
  // the layer is positioned in, so a scroll moves the grips with their blocks
  // and needs no repaint at all.
  paintGrips(this: AppShell) {
    const layer = this.grips;
    const host = layer.parentElement;
    if (!host) {
      return;
    }
    const view = this.editor.view;
    const doc = view.state.doc;
    const targets = gripTargets(doc);
    const have = new Map<string, HTMLButtonElement>();
    for (const b of layer.querySelectorAll<HTMLButtonElement>(
      ':scope > .gly-block-grip',
    )) {
      have.set(`${b.dataset.index}:${b.dataset.kind}`, b);
    }
    const grips = targets.map((t, i) => {
      const id = `${t.index}:${t.kind}`;
      const b = have.get(id) || makeBlockGrip(t);
      have.delete(id);
      if (layer.children[i] !== b) {
        layer.insertBefore(b, layer.children[i] || null);
      }
      b.textContent = gripFace(0);
      const label = gripLabel(t, doc.child(t.index).textContent, 0);
      b.setAttribute('aria-label', label);
      b.title = label;
      return b;
    });
    for (const gone of have.values()) {
      gone.remove();
    }

    const box = host.getBoundingClientRect();
    const column = view.dom.getBoundingClientRect();
    const inset = parseFloat(getComputedStyle(view.dom).paddingLeft) || 0;
    const tops = targets.map((t) => blockTop(view, t) - box.top);
    const size = grips.length ? grips[0].offsetHeight : 0;
    const gap =
      parseFloat(getComputedStyle(layer).getPropertyValue('--gly-grip-gap')) ||
      0;
    const placed = stackGrips(tops, size, gap);

    layer.style.left = `${column.left + inset - box.left}px`;
    grips.forEach((b, i) => {
      b.style.top = `${placed[i]}px`;
    });
  },

  // openBlockComposer is the one opener for every block's grip.
  //
  // IT DOES NOT TOUCH THE SELECTION. The scope is painted as a decoration and
  // the composer is placed against the block, so whatever the reviewer had
  // selected is still selected when the composer goes.
  //
  // THE FORM OPENS STRAIGHT AWAY. The bar's `Add instruction` asks the reviewer
  // to confirm a scope a SELECTION left ambiguous; this gesture named its scope
  // by being pressed beside one block.
  //
  // A FIGURE'S IS ON THE WHOLE FIGURE, and offers Mark a region (markRegion)
  // to narrow it to a part of the picture.
  openBlockComposer(
    this: AppShell,
    target: GripTarget,
    opener: HTMLElement | null,
  ) {
    const view = this.editor.view;
    const doc = view.state.doc;
    const node = doc.nodeAt(target.pos);
    if (!node || node.type.name !== target.kind) {
      // The document moved between the paint and the press.
      return;
    }
    const span =
      target.kind === 'heading'
        ? sectionSpan(doc, target.pos)
        : { from: target.pos, to: target.pos + node.nodeSize };
    if (!span) {
      return;
    }
    this.hideComposer();
    const c = this.composer;
    // A PLACEMENT OUTRANKS A DEFERRED DISMISSAL — placeComposerButton's rule.
    // Pressing the grip blurred the editor, and the blur's zero-timeout check
    // must not hide the composer this press is opening.
    window.clearTimeout(this.blurDismiss);
    this.blurDismiss = 0;

    // The block key comes from the SERVER's block list — it is a content hash,
    // and nothing in the browser can compute one. A block the last refresh has
    // not seen (the reviewer just typed it) has no key, so the composer says
    // so rather than filing against the wrong block.
    const ref = this.blocks.find(
      (b) => b.index === target.index && b.kind === target.kind,
    );
    const label = target.kind === 'heading' ? node.textContent : '';
    c.block = ref
      ? { key: ref.key, label: label || ref.label, region: null }
      : null;
    c.opener = opener;
    c.grip = target;
    // Only where there is a key to file the rectangle on.
    c.mark.hidden = !(target.figure && ref);
    c.root.hidden = false;
    c.bar.hidden = true;
    c.button.hidden = true;
    c.deny.hidden = true;
    c.deny.textContent = '';
    c.denyHint.hidden = true;
    c.denyHint.textContent = '';
    c.form.hidden = false;
    c.input.value = '';
    // Assigning `value` fires no `input` event, so the box would keep the
    // height the last instruction grew it to. See openComposerForm.
    c.input.dispatchEvent(new Event('input'));
    c.note.textContent = ref
      ? ''
      : 'not in the document yet — it lands on the next sync';
    c.note.classList.remove('gly-quiet');
    // NEVER A BARE `= false`: `.gly-composer button` is SEAL_ONLY_VERBS (see
    // web/seal.ts), so enabling send without asking the seal would hand back a
    // control the verdict had killed.
    c.send.disabled = !ref || !!this.sealed;

    view.dispatch(view.state.tr.setMeta(scopeKey, span));

    // BENEATH THE BLOCK, never over it: a composer over the thing the note is
    // about covers it. A heading's is beneath its LINE, which says which
    // section this is; the rest of the section is outlined, not covered.
    // Anything else is beneath its own box — a <pre> has padding and a chip
    // below its last glyph, and its text's coordinates put the composer
    // inside the fence.
    const dom = target.kind === 'heading' ? null : view.nodeDOM(target.pos);
    if (dom instanceof HTMLElement) {
      const box = dom.getBoundingClientRect();
      this.placeComposer(box, box, 6);
    } else {
      this.placeComposer(
        view.coordsAtPos(target.pos + 1),
        view.coordsAtPos(target.pos + node.nodeSize - 1),
      );
    }
    this.headBlockComposer(target, label);
    c.input.focus();
  },

  // markRegion is Mark a region: the form is put away, its words kept, and the
  // figure goes into picking. A finished drag brings the form back on that
  // rectangle; Esc, or a press too short to be a drag, brings it back as it
  // was. The composer does not move: it is already beneath the figure, clear
  // of the picture being dragged on.
  //
  // The figure is found at PRESS time by the grip's index: a NodeView is
  // rebuilt whenever its block's markdown changes, so the element the composer
  // opened against may not be the one on the page now.
  markRegion(this: AppShell) {
    const c = this.composer;
    const target = c.grip;
    const pair =
      target && this.figurePairs().find((p) => p.index === target.index);
    if (!target || !pair || !c.block || c.picking) {
      return;
    }
    c.form.hidden = true;
    c.note.textContent =
      'drag across the figure to mark a region · esc goes back';
    c.note.classList.add('gly-quiet');
    const back = (region: Region | null) => {
      c.picking = null;
      if (c.root.hidden || !c.block) {
        return;
      }
      c.block = { ...c.block, region };
      c.form.hidden = false;
      c.note.textContent = '';
      c.note.classList.remove('gly-quiet');
      this.headBlockComposer(target, '', !!region);
      c.input.focus();
    };
    const kept = c.block.region;
    c.picking = pickRegion(pair.el, back, () => back(kept));
  },
};

// makeBlockGrip builds one grip. Its face, its label and its place are
// paintGrips', which writes them on every paint.
function makeBlockGrip(t: GripTarget): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'gly-block-grip';
  b.dataset.index = String(t.index);
  b.dataset.kind = t.kind;
  return b;
}

// blockTop is where a grip lines up with its block, in viewport coordinates.
// A heading's is its TEXT, not its box: in page mode the box carries the
// padding that aligns it with the live page, above the words. Every other
// block's is its own box. A block mid-rebuild with no element falls back to
// its first position's coordinates.
function blockTop(view: EditorView, t: GripTarget): number {
  const dom = t.kind === 'heading' ? null : view.nodeDOM(t.pos);
  if (dom instanceof HTMLElement) {
    return dom.getBoundingClientRect().top;
  }
  try {
    return view.coordsAtPos(t.pos + 1).top;
  } catch {
    return 0;
  }
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
 * there, so a grip beside it would have no key to file a thread against.
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
