// card.ts — ONE CARD, AND ONE WAY A CARD SHOWS WHAT IT IS ABOUT.
//
// THIS FILE EXISTS BECAUSE THE SAME SURFACE WAS BUILT TWICE. The draft's
// instruction rail and History's changes rail are both *a column of cards
// beside the prose they are about*, and until 2026-08-20 they shared a CSS
// vocabulary (`.gly-card`, `.gly-card-head`, `.gly-card-body`) and NOTHING
// ELSE. `web/versions.js` imported nothing from `web/rail.ts`; the two rails
// did not know each other existed. (`versions.js` and not `.ts` is deliberate
// and is not a stale reference: this clause is dated — "until 2026-08-20" —
// and on that date versions genuinely was JavaScript while rail already was
// not. A sweep of stale filenames rewrote it once; the old name is the
// correct name for the moment being described.)
// Every one of the divergences that cost
// something this week was a consequence of that and not of any decision
// anybody took:
//
//   - History's stacker did not SORT, so a non-monotonic change list — a moved
//     sentence, a table cell — mis-stacked and a card could never reach its
//     mark. `stackCards` sorts, and says why.
//   - two gaps (10 and 12) for one arithmetic.
//   - History interleaved reads and writes inside its placement loop: one
//     forced reflow per card, where `paintAnchors` reads all then writes all
//     and states the reason.
//   - History had no `ResizeObserver`, which CLAUDE.md names as the guard that
//     makes stacking-against-a-measurement safe at all ("A CARD'S HEIGHT IS NOT
//     A CONSTANT").
//   - History's own click handler reimplemented the reveal without
//     `prefers-reduced-motion`, without the stall watch, and without the ring.
//
// CLAUDE.md: *"ONE SURFACE, ONE LANGUAGE — AND THE FIX FOR TWO THINGS DOING ONE
// JOB IS DELETION, NOT A RESTYLE."* So the second implementation is deleted and
// both callers build on what is here.
//
// WHY NOT `rail.ts`. That module's first line is a promise — *"Nothing here
// touches the DOM, ProseMirror, or the network"* — and it is load-bearing:
// `probe.mjs` drives every function in it with no browser, which is the whole
// reason the rail's arithmetic is checkable at all. Putting `document` in there
// would end that. So the PURE half of the collapse (`stackCards`, `RAIL_GAP`,
// `GUTTER_PX`, `RAIL_MIN_WIDTH`) stays in rail.ts and both rails import it from
// there; the DOM half is here, and both rails import it from here. One
// implementation either way — the split is by what can be checked without a
// browser, not by which surface is calling.
//
// WHAT IS DELIBERATELY *NOT* HERE. `threadCard` is not, and must not become,
// the History card's builder. It reads `thread.run`, builds the edit and
// delete verbs and carries the two-step arm — a change is not a thread and has none of that. What is shared is the
// ANATOMY (an article, a mono head, an optional body) and the GESTURE (click or
// Enter to be shown the words this card is about). A builder that knows what a
// thread is stays in the file that knows what a thread is.

// The pure half of the collapse, which stays where probe.mjs can drive it with
// no browser. See the paragraph above about the split.
import { stackCards, RAIL_GAP } from './rail.ts';

/**
 * cardShell builds the one card anatomy: an `<article class="gly-card">` with a
 * `.gly-card-head` already in it.
 *
 * THE HEAD IS NOT OPTIONAL AND THE BODY IS. Every card on every surface leads
 * with a mono chrome line saying what kind of thing this is — `INSTRUCTION ·
 * WHOLE DOCUMENT · 4M AGO`, `CHANGE 2 OF 5 · design`, `ROUND 3 · JUST NOW` —
 * and what comes after it differs: a suggestion card quotes the document, a
 * thread card carries entries and verbs and no `.gly-card-body` at all, a change
 * card carries asks and a note. So the head is built here and the body is asked
 * for (see cardBody) by the callers that have one.
 *
 * THE ELEMENT IS AN `<article>`, INCLUDING WHERE IT USED TO BE A `<button>`.
 * History's landing cards were buttons hand-rolling the card language from
 * scratch — their own background, border, radius, padding, cursor, type and
 * colour, every one of them a second spelling of `.gly-card`'s, and every one
 * of them there because a `<button>` has a user-agent appearance to undo. A
 * card is not a button: it CONTAINS controls on three of the four surfaces it
 * renders on, and a control inside a control is a thing a screen reader cannot
 * describe (see suggestionCard, where that reasoning was first written down).
 * `revealOn` is what makes it reachable from the keyboard instead.
 *
 * @param classes extra classes, in the order they should be read
 */
export function cardShell(...classes: string[]): {
  el: HTMLElement;
  head: HTMLElement;
} {
  const el = document.createElement('article');
  el.className = ['gly-card', ...classes.filter(Boolean)].join(' ');
  const head = document.createElement('div');
  head.className = 'gly-card-head';
  el.appendChild(head);
  return { el, head };
}

/**
 * cardBody appends the card's body and hands it back.
 *
 * Separate from cardShell because the order of what follows the head is the
 * caller's: a thread card puts entries, a reply box and its verbs after the head
 * with no body between them, and a body created eagerly would render as an empty
 * box in the middle of that stack.
 */
export function cardBody(el: HTMLElement): HTMLElement {
  const body = document.createElement('div');
  body.className = 'gly-card-body';
  el.appendChild(body);
  return body;
}

/** What a card that can point at something says it does. ONE SPELLING: it is
 *  also the predicate `.gly-card.gly-thread:not([title])` keys its `cursor:
 *  default` on, so the title and the handler are the same fact by construction
 *  rather than by two rules agreeing. */
const REVEAL_TITLE = 'show me where this is';

/**
 * revealOn makes a card answer the one question a card in a rail is asked:
 * WHICH WORDS IS THIS ABOUT.
 *
 * THE KEYBOARD IS HALF OF IT, AND HISTORY HAD NONE. The draft's cards have been
 * focusable and Enter/Space-activated since they were built; History's change
 * cards carried a bare `click` listener on a `<div>` — no `tabIndex`, no key
 * handler — so every change in a round was unreachable without a pointer. That
 * is the same class of defect as a control covered by another surface: a rect
 * inside the window is not a control the reviewer can press.
 *
 * BUTTONS AND BOXES INSIDE THE CARD ARE NOT THE CARD. A click that lands on
 * `✓ accept`, on `delete`, or in a reply textarea belongs to that control, and
 * bubbling it into a reveal would scroll the page out from under the thing the
 * reviewer was pressing. The keydown guard is `event.target !== el` for the same
 * reason and one more: Space inside a textarea types a space.
 *
 * @param el the card
 * @param act what the reveal does
 * @param title the tooltip, or '' for a card that says nothing
 */
export function revealOn(
  el: HTMLElement,
  act: () => void,
  title: string = REVEAL_TITLE,
): HTMLElement {
  el.tabIndex = 0;
  if (title) {
    el.title = title;
  }
  el.addEventListener('click', (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest('button, textarea')) {
      return;
    }
    act();
  });
  el.addEventListener('keydown', (event) => {
    if (event.target !== el || (event.key !== 'Enter' && event.key !== ' ')) {
      return;
    }
    event.preventDefault();
    act();
  });
  return el;
}

// --- the reveal itself ---
//
// MOVED HERE WHOLE FROM entry.ts, because History's `pick()` was a second,
// shorter answer to the same question and every way it was shorter was a defect:
// it hardcoded `behavior: 'smooth'` (so a reviewer who asked the operating
// system for reduced motion got an animation anyway), it did not verify the
// scroll happened, and it rang nothing when it landed. The measurement below is
// the draft's own and it applies verbatim to a diff mark on History's paper.

/** Exported for the one reveal in entry.ts that scrolls to something which is
 *  not a mark — a CARD inside the rail, flashed from the other direction — and
 *  wants `block: 'nearest'` rather than centring a whole column. The PREFERENCE
 *  is the same question wherever it is asked, so it has one answer here rather
 *  than a `matchMedia` call per call site. */
export function motion(): ScrollBehavior {
  const reduce =
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  return reduce ? 'auto' : 'smooth';
}

// --- did the scroll actually happen? ---
//
// `behavior: 'smooth'` is a REQUEST, and a browser is free to ignore it. Chrome
// with smooth scrolling disabled (a flag, an enterprise policy, some
// accessibility tooling) treats it as a silent no-op rather than falling back to
// an instant jump. Found in the browser pass: clicking a card rang the right
// mark four screens down and never moved the page, so the one thing the click
// exists to do did not happen and nothing said why. Checking
// prefers-reduced-motion is not enough — that media query reported `false` on
// the profile where this reproduced.
//
// So the scroll is asked for and then WATCHED. Two questions have to be kept
// apart, and conflating them is exactly how the first attempt at this was wrong
// in both directions at once:
//
//   HAS IT ARRIVED?   — a property of the final position. See scrollArrival.
//   IS IT MOVING?     — a property of the frames in between. See scrollStep.
//
// That attempt asked only "is any part of the mark within the viewport" two
// frames in, and used the one answer for both questions. It said yes to a mark
// parked five pixels inside the bottom edge, so the fallback never fired and
// flash() rang text that could not be read; and it said yes to an animation
// that had moved the mark one pixel, so on every profile where smooth scrolling
// genuinely worked the fallback fired and cut it to a hard jump — defeating
// smooth scrolling for every mark more than a couple of frames away, which is
// every mark the reveal exists for.

// How close to the centring goal counts as arrived. Sub-pixel layout means
// exact equality never holds.
export const SCROLL_ARRIVED_PX = 4;

// Consecutive frames with the scroll offset UNMOVED before smooth is judged
// dead. Deliberately generous rather than tight: on a profile where smooth
// works, the animation does not necessarily move anything in its first frames —
// a measured per-frame trace read [0, 0, 1, 1, 1620, …], so a two-frame patience
// cuts the very animation it is meant to preserve. Six frames is about 100ms,
// imperceptible as a delay before the fallback and well clear of that latency.
export const SCROLL_STALL_FRAMES = 6;

// Frames after which the watcher stops watching and forces NOTHING. Reaching
// this while still moving means the scroll simply has further to go; a jump here
// would be the bug this whole thing exists to avoid. The fallback fires only on
// positive evidence that nothing is happening, never on a timeout.
const SCROLL_WATCH_FRAMES = 60;

// How long a rung mark stays rung.
const FLASH_MS = 1400;

// A frame's measurement: the mark's viewport-relative box, the viewport
// height, and the current/maximum scroll offsets.
type ScrollSample = {
  markTop: number;
  markBottom: number;
  viewportH: number;
  scrollY: number;
  scrollMax: number;
};

type ScrollStepResult = {
  action: 'arrived' | 'wait' | 'fallback';
  stalls: number;
};

/**
 * scrollArrival reports the scroll offset a `block: 'center'` scroll can
 * actually reach for this mark, and whether the page is already there.
 *
 * PURE, so probe.mjs can put a mark a sliver inside the bottom edge with no
 * browser.
 *
 * @param s viewport-relative mark box, viewport height, and the
 *   current/maximum scroll offsets
 */
export function scrollArrival(s: ScrollSample): {
  target: number;
  arrived: boolean;
} {
  // markTop/markBottom are viewport-relative; + scrollY puts the midpoint into
  // document coordinates, which is what a scroll offset is measured in.
  const markMid = s.scrollY + (s.markTop + s.markBottom) / 2;
  const target = Math.max(0, Math.min(markMid - s.viewportH / 2, s.scrollMax));
  return { target, arrived: Math.abs(s.scrollY - target) <= SCROLL_ARRIVED_PX };
}

/**
 * scrollStep is one frame of the watch, as a pure decision.
 *
 * Order matters. Arrival is checked FIRST so a scroll already where it was going
 * is never called stalled — otherwise a mark that needed no scroll at all would
 * collect stall frames and earn a pointless jump. Movement is checked SECOND,
 * and any movement at all resets the stall count: that is the whole defence of
 * an in-flight animation, and it deliberately does not care how far the
 * animation still has to go.
 *
 * @param sample this frame's measurement (see scrollArrival)
 * @param prev last frame's measurement, or null on the first frame
 * @param stalls consecutive unmoved frames so far
 */
export function scrollStep(
  sample: ScrollSample,
  prev: ScrollSample | null,
  stalls: number,
): ScrollStepResult {
  if (scrollArrival(sample).arrived) {
    return { action: 'arrived', stalls: 0 };
  }
  if (prev !== null && sample.scrollY !== prev.scrollY) {
    return { action: 'wait', stalls: 0 };
  }
  const next = stalls + 1;
  if (next >= SCROLL_STALL_FRAMES) {
    return { action: 'fallback', stalls: next };
  }
  return { action: 'wait', stalls: next };
}

function scrollMarkIntoView(el: Element): void {
  const behavior = motion();
  el.scrollIntoView({ behavior, block: 'center', inline: 'nearest' });
  // 'auto' is not a request, it is a jump; there is nothing to verify.
  if (behavior === 'auto') {
    return;
  }

  // clientHeight, not innerHeight: the layout viewport is what scrollIntoView
  // centres within, and it is the same height scrollMax is measured against, so
  // the two cannot disagree by the width of a scrollbar.
  const measure = () => {
    const de = document.documentElement;
    const viewportH = de.clientHeight || window.innerHeight || 0;
    const r = el.getBoundingClientRect();
    return {
      markTop: r.top,
      markBottom: r.bottom,
      viewportH,
      scrollY: window.scrollY,
      scrollMax: Math.max(0, de.scrollHeight - viewportH),
    };
  };

  let prev: ScrollSample | null = null;
  let stalls = 0;
  let frames = 0;
  const watch = () => {
    const sample = measure();
    const step = scrollStep(sample, prev, stalls);
    if (step.action === 'arrived') {
      return;
    }
    if (step.action === 'fallback') {
      el.scrollIntoView({
        behavior: 'auto',
        block: 'center',
        inline: 'nearest',
      });
      return;
    }
    prev = sample;
    stalls = step.stalls;
    frames += 1;
    if (frames < SCROLL_WATCH_FRAMES) {
      window.requestAnimationFrame(watch);
    }
  };
  window.requestAnimationFrame(watch);
}

// flash rings a mark for long enough to be seen and no longer. The class is
// removed first and a layout read forced between: re-adding a class that is
// already there restarts nothing, so clicking the same card twice would
// otherwise do nothing the second time.
export function flash(el: Element): void {
  el.classList.remove('gly-flash');
  // getBoundingClientRect forces the same layout read offsetWidth would —
  // the value is unused either way. Element carries it where HTMLElement's
  // offsetWidth does not, and a rung mark is not always an HTMLElement (an SVG
  // path, for instance).
  void el.getBoundingClientRect();
  el.classList.add('gly-flash');
  window.setTimeout(() => el.classList.remove('gly-flash'), FLASH_MS);
}

/**
 * revealMark is the whole gesture in one call — take the page to the words and
 * ring them — and it is what every card's reveal ends in.
 *
 * The two halves are one gesture and were separated only by which file they
 * happened to be written in: History scrolled and did not ring, so a reviewer
 * who was already looking at roughly the right paragraph got no answer to the
 * question they had just asked. Verifying the scroll and then saying nothing
 * about where it landed is half a reveal.
 */
export function revealMark(el: Element): void {
  scrollMarkIntoView(el);
  flash(el);
}

// --- placing a column of cards against the prose it is a map of ---

// The last height written per container, so `placeCards` can compare before it
// writes. A WeakMap rather than a field on the caller: the container is rebuilt
// on every repaint and the entry goes with it, where a field would have to be
// reset by hand on every rebuild and would be forgotten on the third surface.
const heights = new WeakMap<HTMLElement, string>();

// One card as placeCards receives it: an element, its anchor's top (in the
// same page coordinates as `ceiling`, or null when this pass could not
// measure it), and its height.
type CardMeasurement = {
  el: HTMLElement;
  anchorTop: number | null;
  height: number;
};

/**
 * placeCards is the WHOLE placement pass, and it is one function because it was
 * two.
 *
 * The instruction rail's `paintAnchors` and History's `align()` were the same
 * twenty-five lines with five accidental differences between them, every one of
 * which was a defect on the History side — no sort, a wider gap, reads
 * interleaved with writes, a rounded top fed forward unrounded, and a height
 * written unconditionally with a trailing gap. The arithmetic was already
 * shared-able (`stackCards` has always been in rail.ts); what was not was the
 * PASS around it, so the pass is here now and neither caller owns a copy.
 *
 * WHAT EACH CALLER STILL OWNS IS THE MEASUREMENT, and that is the real
 * difference between the two rails rather than an accident: the draft measures a
 * mark through ProseMirror (`coordsAtPos`, or a block's element) and History
 * measures a diff mark through `querySelector` on rendered HTML. Neither can be
 * expressed in the other's terms. So the caller hands over what it measured and
 * this places it.
 *
 * READS ARE ALREADY DONE. Every `anchorTop` and `height` in `measured` was read
 * before this was called, which is the contract that keeps one reflow from
 * becoming one per card: `getBoundingClientRect` forces layout and every style
 * assignment invalidates it, so a pass that interleaves them pays per card.
 *
 * NOTHING BUT A POSITION IS WRITTEN ON A CARD. That is what makes it safe for a
 * `ResizeObserver` over those same cards to schedule this pass: a re-measure
 * cannot resize anything, so the observer cannot feed itself. The ONE size
 * written is the CONTAINER's, and it is the stated exception — the container's
 * children are absolutely positioned and contribute no height of their own, so
 * without it whatever follows the map is drawn over the top of it; and a card is
 * inset `left`/`right` inside the container, so its box depends on the
 * container's WIDTH and never on its height.
 *
 * ADRIFT CARDS GO AFTER THE MAP, IN ORDER, AND ARE COUNTED. A card whose anchor
 * this pass could not measure has `top: auto` — its static position, the
 * container's own top — so it would be drawn at the top of the map while
 * contributing nothing to the height below it; with every card adrift the
 * container stayed 0 tall and the whole map was underneath them. The end of the
 * map is the honest place: a card that points at nothing has no mark to be
 * beside, and that is where everything else with nowhere to be already goes.
 *
 * @param container the positioned context the cards live in
 * @param measured
 * @param ceiling the highest page position a card may take — the
 *   container's own top, in the same coordinates as every anchorTop
 * @returns the height written
 */
export function placeCards(
  container: HTMLElement,
  measured: CardMeasurement[],
  ceiling: number,
  gap: number = RAIL_GAP,
): number {
  let bottom = 0;
  const anchored = measured.filter(
    (m): m is CardMeasurement & { anchorTop: number } => m.anchorTop !== null,
  );
  for (const placed of stackCards(anchored, gap, ceiling)) {
    // NOT ROUNDED, and the two halves of that are one decision. `align()` wrote
    // a rounded top and fed the UNROUNDED value to the next card's floor, so
    // what was on screen and what the next placement assumed disagreed by up to
    // half a pixel each time, compounding down the column. The arithmetic is
    // exact and the browser lays out sub-pixel; rounding buys nothing and costs
    // that.
    placed.el.style.top = `${placed.top - ceiling}px`;
    bottom = Math.max(bottom, placed.top + placed.height - ceiling);
  }
  for (const m of measured) {
    if (m.anchorTop !== null) {
      continue;
    }
    if (bottom > 0) {
      bottom += gap;
    }
    m.el.style.top = `${bottom}px`;
    bottom += m.height;
  }
  setStackHeight(container, bottom);
  return bottom;
}

/**
 * setStackHeight is the one size write, and it COMPARES BEFORE IT WRITES.
 *
 * This runs on every scroll frame in the draft and from a ResizeObserver in
 * History; assigning an unchanged height would dirty layout for nothing and hand
 * the observer a reason to fire again. `null` clears it, which is what a
 * container whose cards are in flow needs — at a width where the rail goes under
 * the paper there is no "beside its mark" to aim at and no height to reserve.
 */
export function setStackHeight(
  container: HTMLElement,
  px: number | null,
): void {
  const next = px === null ? '' : `${Math.max(0, Math.round(px))}px`;
  if (heights.get(container) !== next) {
    heights.set(container, next);
    container.style.height = next;
  }
}

/**
 * coalesce turns a burst of events into one pass in the next animation frame.
 *
 * A scroll fires faster than a layout read can answer and a drag-resize is a
 * burst, and every read in a placement pass forces layout. Both rails need
 * exactly this and both had written their own; it is four lines, which is
 * precisely the size at which two copies drift without anyone noticing.
 */
export function coalesce(run: () => void): () => void {
  let queued = false;
  return () => {
    if (queued) {
      return;
    }
    queued = true;
    window.requestAnimationFrame(() => {
      queued = false;
      run();
    });
  };
}

/**
 * growthWatch watches cards for the one thing a placement pass cannot see: a
 * card that changes size AFTER it was placed.
 *
 * CLAUDE.md: *"A CARD'S HEIGHT IS NOT A CONSTANT, so nothing may be stacked
 * against a measurement and then left there."* The stack is exact arithmetic
 * over heights measured once, so a card that grows leaves every card below it
 * stale and is drawn straight over its neighbour's reply box and buttons. Two
 * real ways it grows with no repaint to catch it in the draft — the reply box's
 * `resize: vertical` grip, and an entry arriving — and in History a long ask
 * rewrapping as the window narrows, or a web font landing after first paint.
 *
 * Observing an element already observed is a no-op, so `watch` is safe to call
 * on every paint; `unwatch` is how the observer stops holding cards the last
 * rebuild destroyed. A browser with no `ResizeObserver` gets both as no-ops
 * rather than a thrown constructor.
 */
export function growthWatch(schedule: () => void): {
  watch(elements: Iterable<Element> | null | undefined): void;
  unwatch(): void;
  readonly live: boolean;
} {
  const observer =
    typeof ResizeObserver === 'function'
      ? new ResizeObserver(() => schedule())
      : null;
  return {
    watch(elements) {
      if (!observer) {
        return;
      }
      for (const el of elements || []) {
        observer.observe(el);
      }
    },
    unwatch() {
      if (observer) {
        observer.disconnect();
      }
    },
    get live() {
      return !!observer;
    },
  };
}

export function anchorNote(el: HTMLElement): HTMLElement {
  let note = el.querySelector<HTMLElement>('.gly-card-note');
  if (!note) {
    note = document.createElement('div');
    note.className = 'gly-card-note';
    el.appendChild(note);
  }
  return note;
}
