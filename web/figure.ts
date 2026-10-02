// figure.ts — read-only figure rendering.
//
// BOTH NodeViews here are PRESENTATION ONLY. Neither dispatches a transaction,
// neither writes a node attribute, and neither makes a fence editable. A
// NodeView is the right mechanism for exactly that reason: the document is
// untouched and only the DOM is ours.
//
// If either one ever needs to WRITE, it is the wrong mechanism and the change
// belongs on the Go side where the round trip is tested. internal/serve's
// TestMermaidFenceRoundTripsByteIdentically is the gate that says so: it opens
// a document with a diagram in it and asserts the bytes written back are the
// bytes read. A rendered fence is a picture OF the fence.
//
// The chip and the caret note are the two halves of one refusal voice (§6, R8),
// and a figure gets both for the same reason a table does: a rule that only
// appears after you break it is a rule the reviewer meets as a bug.

import type { NodeViewRenderer } from '@tiptap/core';

// --- images ---

/**
 * imageNodeView renders an image as a <figure> with its alt text as the
 * caption.
 *
 * The <figure> box is what a region pin is measured against — regions are
 * stored as FRACTIONS of it — so it must be a stable element that wraps the
 * picture and nothing else. That is why the caption is inside it: a caption
 * outside the box would make the box's height depend on nothing the reader can
 * see, and a fraction of it would move when the alt text wrapped.
 */
export function imageNodeView(
  chip: (() => Element) | undefined,
): NodeViewRenderer {
  return ({ node }) => {
    const dom = document.createElement('figure');
    dom.className = 'gly-figure gly-figure-image';
    const src = node.attrs.src || '';
    // The passive half of the refusal voice, on an image for the same reason it
    // is on a fence and on a table: phase 1 has nowhere to record a change to
    // one, and a rule only discoverable by breaking it is a rule the reviewer
    // meets as a bug.
    if (chip) {
      dom.appendChild(chip());
    }
    const img = document.createElement('img');
    // A relative src resolves against "/", which is where the shell is served,
    // and serveSibling answers it from beside the document.
    img.src = src;
    img.alt = node.attrs.alt || '';
    // Dragging an image inside a contenteditable is a MOVE, which would be an
    // untracked structural edit — exactly the class of change phase 1 has
    // nowhere to record.
    img.draggable = false;
    dom.appendChild(img);
    if (node.attrs.alt) {
      const cap = document.createElement('figcaption');
      cap.textContent = node.attrs.alt;
      dom.appendChild(cap);
    }
    // ignoreMutation: everything inside this DOM is ours — the img decoding,
    // the pins Task 9 draws — and none of it is a document change ProseMirror
    // should try to read back.
    return { dom, ignoreMutation: () => true };
  };
}

// --- mermaid ---

// The mermaid module's shape, as far as this file reads it: a `render`
// function and an `initialize` function, matching the mermaid.js UMD global
// the script tag below installs. `mermaid.js` itself is not part of this
// module's type-checked surface — it is fetched at runtime as a separate
// asset (see the justfile) — so the shape is declared here rather than
// imported.
type MermaidModule = {
  initialize(opts: { startOnLoad: boolean; securityLevel: string }): void;
  render(id: string, text: string): Promise<{ svg: string }>;
};

declare global {
  interface Window {
    galleyMermaid?: MermaidModule | { default: MermaidModule };
  }
}

let mermaidLoad: Promise<MermaidModule> | null = null;

/**
 * loadMermaid fetches the renderer once, on the first mermaid fence in the
 * document, and never on a document that has none.
 *
 * The bundle is several megabytes. Making every page pay for it so that some
 * pages can draw a diagram is not a trade worth making, which is why it is a
 * separate asset (see the justfile) rather than part of editor.js.
 */
function loadMermaid(): Promise<MermaidModule> {
  if (!mermaidLoad) {
    mermaidLoad = new Promise((resolve, reject) => {
      const s = document.createElement('script');
      s.src = '/_galley/mermaid.js';
      s.onload = () => {
        const mod = window.galleyMermaid;
        if (mod && 'default' in mod) {
          resolve(mod.default);
        } else if (mod) {
          resolve(mod);
        } else {
          reject(new Error('mermaid.js loaded but installed no module'));
        }
      };
      s.onerror = () =>
        reject(new Error('mermaid.js is missing — run `just assets`'));
      document.head.appendChild(s);
    });
  }
  return mermaidLoad;
}

// Each render needs an id unique within the page — mermaid puts it on the SVG
// and on the ids inside it, and two diagrams sharing one produce a second
// diagram that renders as a copy of the first. A counter rather than
// Math.random: it is deterministic, which matters when the failure it prevents
// is "sometimes two diagrams look the same".
let mermaidSeq = 0;

/**
 * mermaidNodeView draws a mermaid fence as a diagram, WITHOUT making it
 * anything other than a read-only fence.
 *
 * The source is kept in the DOM, hidden, and shown again if rendering fails. A
 * diagram that will not parse must not become an empty box: the reviewer still
 * has to be able to read the fence, quote it, and comment on the block. Same
 * graceful degradation as a missing editor bundle — say less, never lie.
 *
 * @param chip builds the passive read-only chip, so the figure and the
 *   fence wear the same one from the same place
 */
export function mermaidNodeView(
  chip: (() => Element) | undefined,
): NodeViewRenderer {
  return ({ node }) => {
    const dom = document.createElement('figure');
    dom.className = 'gly-figure gly-figure-mermaid';
    if (chip) {
      dom.appendChild(chip());
    }

    const source = document.createElement('pre');
    source.className = 'gly-figure-source';
    source.textContent = node.textContent;

    const target = document.createElement('div');
    target.className = 'gly-figure-render';
    dom.append(target, source);

    const id = `gly-m-${(mermaidSeq += 1)}`;
    loadMermaid()
      .then((mermaid) => {
        // securityLevel strict: a diagram's labels come from the author's file,
        // and the rendered SVG is same-origin with every galley endpoint on
        // this server. startOnLoad false because nothing here wants mermaid
        // walking the page on its own — the NodeViews decide what renders.
        mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });
        return mermaid.render(id, node.textContent);
      })
      .then(({ svg }) => {
        target.innerHTML = svg;
        source.hidden = true;
      })
      .catch(() => {
        // Left as source, with the read-only chip still on it.
        target.remove();
        dom.classList.add('gly-figure-unrendered');
      });

    // No contentDOM: the fence's text is not editable here and never was. The
    // <pre> above is a COPY for reading and copying, which is the half of the
    // fence rule that stays true — "select and copy still work".
    return { dom, ignoreMutation: () => true };
  };
}

// --- picking a region ---

// A drag shorter than this in either direction is a CLICK, not a rectangle.
// Filing it would send a region the server refuses anyway (Region.Valid wants
// area), so it cancels instead — a click that produces an error message the
// reviewer did not ask for is worse than a click that does nothing.
const REGION_MIN_FRACTION = 0.01;

/**
 * pickRegion puts a figure into pick mode: crosshair, a dashed rectangle that
 * follows the drag, Esc to cancel.
 *
 * FRACTIONS FROM THE FIRST MOVE, NOT PIXELS CONVERTED AT THE END. The figure can
 * reflow mid-drag — a lazily-decoded image changing height is the common case —
 * and a pixel origin captured before that is measured against a box that no
 * longer exists. Fractions are re-derived from the live box on every event, so a
 * reflow moves the rectangle with the picture instead of away from it.
 *
 * Every exit path removes every listener. A crosshair cursor left on the
 * document because a drag ended outside the figure is the kind of state that
 * survives until reload.
 *
 * A PICK THAT ENDS WITH NO RECTANGLE CALLS onCancel: Esc, or a press too short
 * to be a drag. The caller put something away to make room for the pick (the
 * composer's form), and it is the caller that knows how to bring it back.
 *
 * @param figure the .gly-figure box the fractions are OF
 * @param onPicked
 * @param onCancel
 * @returns cancel, for a caller that needs to exit pick mode itself; it
 *   calls neither callback
 */
export function pickRegion(
  figure: HTMLElement,
  onPicked: (region: { x: number; y: number; w: number; h: number }) => void,
  onCancel: () => void,
): () => void {
  figure.classList.add('gly-picking');
  const draft = document.createElement('div');
  draft.className = 'gly-region-draft';
  draft.hidden = true;
  figure.appendChild(draft);

  let start: { x: number; y: number } | null = null;

  const frac = (e: PointerEvent): { x: number; y: number } => {
    const b = figure.getBoundingClientRect();
    return {
      x: Math.min(1, Math.max(0, (e.clientX - b.left) / b.width)),
      y: Math.min(1, Math.max(0, (e.clientY - b.top) / b.height)),
    };
  };

  const exit = () => {
    figure.classList.remove('gly-picking');
    draft.remove();
    figure.removeEventListener('pointerdown', down);
    figure.removeEventListener('pointermove', move);
    figure.removeEventListener('pointerup', up);
    document.removeEventListener('keydown', key, true);
  };

  const down = (e: PointerEvent) => {
    e.preventDefault();
    start = frac(e);
    draft.hidden = false;
    try {
      figure.setPointerCapture(e.pointerId);
    } catch {
      // Capture is a convenience — the drag still tracks without it.
    }
  };

  const move = (e: PointerEvent) => {
    if (!start) {
      return;
    }
    const now = frac(e);
    Object.assign(draft.style, {
      left: `${Math.min(start.x, now.x) * 100}%`,
      top: `${Math.min(start.y, now.y) * 100}%`,
      width: `${Math.abs(now.x - start.x) * 100}%`,
      height: `${Math.abs(now.y - start.y) * 100}%`,
    });
  };

  const up = (e: PointerEvent) => {
    if (!start) {
      exit();
      onCancel();
      return;
    }
    const now = frac(e);
    const region = {
      x: Math.min(start.x, now.x),
      y: Math.min(start.y, now.y),
      w: Math.abs(now.x - start.x),
      h: Math.abs(now.y - start.y),
    };
    exit();
    if (region.w < REGION_MIN_FRACTION || region.h < REGION_MIN_FRACTION) {
      onCancel();
      return;
    }
    onPicked(region);
  };

  // Capture phase, so Esc leaves pick mode before anything else acts on it —
  // the rail's own Escape handler would otherwise close a composer that is not
  // even open yet and leave the crosshair behind.
  const key = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      exit();
      onCancel();
    }
  };

  figure.addEventListener('pointerdown', down);
  figure.addEventListener('pointermove', move);
  figure.addEventListener('pointerup', up);
  document.addEventListener('keydown', key, true);
  return exit;
}
