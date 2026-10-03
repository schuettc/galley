// rail.ts — the arithmetic behind the three surfaces the panel split into.
//
// Nothing here touches the DOM, ProseMirror, or the network. That is the point:
// where a card goes, which surface a viewport gets, what the census says and
// which mark `j` steps to next are all decisions over plain numbers, and
// probe.mjs drives every one of them with no browser. The DOM code in entry.ts
// measures, calls in here, and paints what it is told.

// The gap between two cards the stacker had to separate. From the handoff:
// sort by anchor top, push down, 10px gap.
export const RAIL_GAP = 10;

// The gutter reserved to the left of every card, and it is LOAD-BEARING under
// a name that finally says what it does.
//
// It was `CONNECTOR_PX` through two designs: a 26px horizontal arm was once the
// whole connector, and then the connector was a curve that did not live in the
// gutter at all, and then the connector was deleted. The number stayed all
// three times, because what it actually holds is that ONE LEFT EDGE DOWN THE
// WHOLE RAIL is what the reviewer reads — `.gly-rail-band .gly-card` insets by
// it, `.gly-rail-notice .gly-card` takes it as a margin, and `just layers` §9
// asserts every card's left edge is exactly this far from the RAIL's own box,
// which is what caught a second container laying cards out at full width. The
// constant is kept and the name is corrected: a rule named for a mechanism that
// no longer exists is the next reader's excuse to delete it.
export const GUTTER_PX = 26;

// Below this the rail is replaced (never accompanied) by the bottom bar and the
// review sheet. PIXELS, and the stylesheet's media query is the same number in
// the same unit — see the note in editor.css. A rem-based query would disagree
// with this constant the moment a reader changes their default font size, and
// the way that disagreement shows up is rail and sheet on screen together.
export const RAIL_MIN_WIDTH = 992;

// Who the reviewer is on the wire. ONE SPELLING: entry.ts posts under it. A
// second reader that spelled its own literal is how the six agreeing spellings
// in CLAUDE.md always start. (`outgoingCounts` was the third and is deleted;
// see the note where it was.)
const REVIEWER = 'court';

// The name the reviewer's comments and replies are attributed to. It matches
// review.AuthorCourt on the Go side; the agent's own suggestions arrive
// attributed to "agent". (It used to name every typed mark too — the
// reviewer's-hand cut removed those, but a conversation still needs a name.)
//
// ONE SPELLING, and it is rail.ts's: a literal 'court' here beside a literal
// 'court' there is how two agreeing copies become three and then one that
// disagrees.
export const AUTHOR = REVIEWER;

// The shapes of a pending-payload entry, in the loose form this module has
// always read them: every field optional, because a thread arrives over JSON
// and this file's whole discipline is treating what it cannot see as absent
// rather than assuming a shape it hasn't checked.
// EXPORTED so web/appshell.ts's `Thread` — the richer shape the App actually
// carries in `this.comments` — can be DECLARED against this shape rather than
// duplicating its fields by hand; see appshell.ts's own header for why a
// second, hand-copied spelling of "what a thread is" is the shape this
// codebase is already tired of.
export type PendingThreadEntry = {
  author?: string;
  text?: string;
};

export type PendingThread = {
  anchor?: string;
  anchorKey?: string;
  run?: string;
  region?: object | null;
  blockKind?: string;
  heading?: string;
  entries?: PendingThreadEntry[];
};

/**
 * stackCards places every card at its anchor's vertical position, pushing a
 * card down when the one above it is already there.
 *
 * COLLISION ARITHMETIC, AND A FLOOR THE BAND ITSELF SETS. Two marks a few
 * pixels apart need their cards pushed apart, and that is most of what this
 * does.
 *
 * THE CEILING CAME BACK, AND IT IS A DIFFERENT ARGUMENT FROM THE ONE THAT WENT.
 * It was deleted with the fold, on the reasoning that "the band starts at the
 * rail's own top, so a card at its mark is simply at its mark" — TRUE OF A RAIL
 * WHOSE FIRST CHILD IS THE BAND, and false the moment anything is pinned above
 * it. The whole-document instruction is a full card pinned FIRST in the rail
 * now (spec §2.2), so the band begins some way down the column while the mark
 * in the document's first paragraph is still near the page's top — and a card
 * placed at that mark is placed at a NEGATIVE band-local top, i.e. drawn over
 * the pinned card. Measured: the first anchored card's head sat on top of the
 * whole-document card's `delete`, and Playwright refused the click with
 * `.gly-card-head … intercepts pointer events`, which is this repository's own
 * "a rect inside the window is not a control the reviewer can press".
 *
 * `ceiling` is therefore the band's own measured top, in the same document
 * coordinates as every anchorTop, and it is the initial floor rather than a
 * per-card clamp — clamping each card independently would pile every early
 * mark's card at exactly the ceiling, on top of each other, which is the
 * collision this function exists to prevent.
 *
 * THE COORDINATES ARE THE DOCUMENT'S. `anchorTop` is a position in the page,
 * not in the window — the caller adds `window.scrollY` to what `coordsAtPos`
 * measures — so nothing here changes when the reader scrolls, and a mark
 * "above the viewport" is not a case this function has. A negative anchorTop
 * would now mean a mark above the document's own top, which does not exist.
 *
 * The sort is inside rather than assumed: the caller builds cards from
 * /_galley/pending, which is in DOCUMENT order, and document order is not
 * always the order marks measure in — a thread card is built after every
 * suggestion card whatever its mark's position. Array.prototype.sort is stable
 * (ES2019), so two marks that measure to the same top keep document order
 * between them.
 *
 * @param ceiling the highest document position a card may take
 */
export function stackCards<T extends { anchorTop: number; height: number }>(
  cards: T[],
  gap = RAIL_GAP,
  ceiling = -Infinity,
): (T & { top: number })[] {
  const sorted = cards.slice().sort((a, b) => a.anchorTop - b.anchorTop);
  let floor = ceiling;
  return sorted.map((card) => {
    const top = Math.max(card.anchorTop, floor);
    floor = top + card.height + gap;
    return { ...card, top };
  });
}

// THERE IS NO `connectorPath` HERE ANY MORE, AND ITS DELETION IS THE ENTRY.
//
// It was one cubic Bézier from a mark to the card about it, with horizontal
// control points at both ends, drawn only on hover or focus. It is gone with
// the whole connector — every geometry function, the shared SVG overlay,
// `.gly-connector-leg` and `-arm`, and the four refusals that decided when a
// line could be drawn. See web/lit.ts, which is what answers the question the
// line was answering, and CLAUDE.md's entry for why a line could not.

/**
 * railSurfaces decides which of the three surfaces render, and is the ONLY
 * place that decision is made.
 *
 * ONE SURFACE, ONE STATE. Rendering the rail and the sheet together is the bug
 * this function exists to make impossible, and it is still impossible: the
 * sheet WINS wherever it is open, and the rail is not painted underneath it.
 * `collapsed` comes back out because narrow FORCES it — the caller must paint
 * the collapsed layout without having to re-derive why.
 *
 * THE SHEET IS NOT A NARROW-ONLY SURFACE ANY MORE, and that is this function's
 * one real change. It used to answer `sheet: false` at every width at or above
 * RAIL_MIN_WIDTH, which was correct while the rail carried a settled region of
 * its own: the sheet was the narrow layout's REPLACEMENT for the rail, so it
 * only had to exist where the rail did not. The rail holds live work only now
 * (the 2026-08-16 spec) and settled conversations live in the sheet — so a
 * sheet reachable only below 992px would put `↺ reopen`, the sole way back from
 * a mis-clicked `✓ resolve`, out of reach of every desktop reviewer. That is
 * "a resolved thread may never be invisible-but-present" arriving through the
 * opposite door from the one it was first measured at (layers §7b′, which was
 * written when the SHEET was the surface with no settled region).
 *
 * THAT SETTLED REGION IS GONE NOW, AND THE ANSWER STANDS: nothing resolves an
 * instruction any more, but the sheet is still the one surface listing every
 * thread, the anchorless ones included, so it stays reachable at every width.
 *
 * It is the review's whole list at every width, and it is reached the same way
 * at every width: the census count, which is a button.
 */
export function railSurfaces({
  width,
  collapsed,
  sheetOpen,
}: {
  width: number;
  collapsed?: boolean;
  sheetOpen?: boolean;
}): { rail: boolean; bar: boolean; sheet: boolean; collapsed: boolean } {
  if (width < RAIL_MIN_WIDTH) {
    return { rail: false, bar: true, sheet: !!sheetOpen, collapsed: true };
  }
  if (sheetOpen) {
    return { rail: false, bar: false, sheet: true, collapsed: !!collapsed };
  }
  return { rail: !collapsed, bar: false, sheet: false, collapsed: !!collapsed };
}

/**
 * decidable says whether accept and reject act on this pending entry — and it
 * ANSWERS BY READING THE SERVER, not by looking at the kind itself.
 *
 * `suggest.Kind.Decidable` is the one predicate; `/_galley/pending` carries its
 * answer per entry as `decidable`, and this reads that field. The alternative —
 * `s.kind !== 'comment'` here — is a SECOND RULE THAT AGREES FOR NOW, and this
 * codebase has already paid for that shape twice (the serializer's pairing,
 * re-derived on the wrong side of the wire; and the run stamper the Go comments
 * claimed the browser owned).
 *
 * It was written because the browser had no rule at all where it needed one:
 * `arrivals.ts`'s `batchRows` paired a revision's runs against the whole
 * pending list with no kind filter, so a revision carrying a comment put that
 * comment's run under the revision receipt's ✓ — measured accepting a
 * conversation, lifting the reviewer's highlight and leaving it anchored to
 * nothing. THAT CARD IS GONE (one card language: a revision's results are its
 * own cards), and this predicate is NOT. It was never the card's — it is the
 * server's answer, and three surfaces still read it, below.
 *
 * ABSENT MEANS NO, deliberately. A payload with no `decidable` field is a
 * payload this bundle was not built against; the bundle and the server ship in
 * one binary, so that cannot happen in a session — and if it somehow does, a
 * verb withheld is recoverable and a verb offered on a conversation is not.
 *
 * THAT JUSTIFICATION IS THIS SIDE'S AND DOES NOT TRAVEL. It rests on the bundle
 * and the server being one binary, which is true of the browser and false of
 * the CLI: `galley accept --all` was a separate process asking whatever `galley
 * edit` was running, with no version handshake in the protocol, so a newer CLI
 * against an older server really did see the field absent, and there absent
 * meant ASK THE KIND. A reader of this field outside the browser has to decide
 * the question again.
 *
 * WHAT IT GOVERNS IS WHAT IS OFFERED. Every surface that puts a ✓ or a ✗ in
 * front of the reviewer, or that `a`/`r` can land on, asks this: the rail's
 * card loop, the sheet's, and stepOrder. censusCounts does NOT: it counts
 * threads, not verbs, and nothing it does can reach an endpoint.
 *
 * @param s a pending-payload entry
 */
export function decidable(
  s: { decidable?: boolean } | null | undefined,
): boolean {
  return !!(s && s.decidable);
}

/* --- what the reviewer is about to tell the agent ---------------------------
 *
 * THE TRAIL IS AN OUTGOING MESSAGE, NOT A HISTORY, AND THAT IS WHY IT HAS NO
 * BROWSABLE SURFACE. This is the reframe from the 2026-08-16 spec and it is the
 * part that has to survive, because every future proposal to give the trail a
 * home will arrive sounding reasonable.
 *
 * `cmd/galley/agentprompt.go` tells the paired session, verbatim: *"The pending
 * view's `changes` are the reviewer's own direct edits — decided facts to read
 * as context, never things to relitigate or re-propose against."* `galley
 * pending` prints them, and `/_galley/pending` carries them. So the trail's
 * audience is THE AGENT. The ghosts in the prose are the visible form of an
 * outgoing message — when the reviewer strikes a phrase and presses the verdict
 * button, the agent is told they did, without which it may re-propose the
 * phrasing just removed.
 *
 * Which fixes WHEN it is wanted, and there are exactly two moments: just after
 * typing (*did that land* — answered by the ghost appearing, in seconds) and
 * just before sending (*what am I about to tell the agent* — answered here).
 * After that git has it and the ledger has it, and NOBODY BROWSES IT.
 *
 * A rail section, a sheet section, a bar cell with a popover, a gutter and a
 * focus mode were all considered and all rejected, and they failed for one
 * reason: they answered WHERE. The question was WHEN. So the count goes on the
 * button that sends it, and any future proposal for a history drawer has to
 * answer when the reviewer would open it before it says where it would live.
 *
 * IT IS THE POPULATION THE AGENT IS SENT, READ RATHER THAN RE-DERIVED. Both
 * numbers come off the SAME pending payload the agent's `galley pending` renders
 * from — `changes` for the edits, the reviewer-authored entries of `comments`
 * for the replies — so the label cannot become a second spelling that agrees
 * for now. In particular the edits number is NOT the browser's own live trail
 * (trailPluginKey's entries): that list runs ahead of the server by one debounce
 * and is not yet anything the agent could be told. What the server holds is what
 * the agent gets, and what the agent gets is what the button counts.
 */

/* `outgoingCounts`, `outgoingLabel`, `OUTGOING_JOIN`, `OUTGOING_EDITS` and
 * `OUTGOING_REPLIES` ARE DELETED — the reframe above is not, and it is the half
 * that had to survive.
 *
 * They reduced the pending payload to `· 6 edits, 2 replies` for a reserved box
 * on the verdict button. Both numbers came off fields the rounds-only wire does
 * not carry: `pendingView` is `{instructions, blocks}`, so `view.changes` was
 * always `undefined` and the reviewer-authored `comments` entries went with the
 * conversation verbs. `.gly-revise-trail`, the box they were written into, was
 * deleted when the words stopped being written; nothing has called either
 * function since, and probe.mjs was checking their arithmetic against no caller
 * at all — the shape this repository has recorded six of.
 *
 * THE COUNT ON THE BUTTON THAT SENDS IT IS STILL THE DESIGN. It is
 * `reviseCountClause(pendingCount)` in `.gly-revise-count` now — one number, the
 * instructions in the round, read off the same payload `galley pending` renders
 * — and the argument for the reserved box is unchanged and lives on that class.
 * Any future proposal for a trail drawer still has to answer WHEN before WHERE.
 */

/**
 * censusCounts reduces the /_galley/pending payload to the thread numbers the
 * page reads.
 *
 * IT TAKES THE SERVER'S ANSWER, NOT THE RAIL'S. The count is derived from the
 * same projection the document is, which is the whole reason the census is a
 * separate surface: a strip that counted the rail's cards would count what was
 * rendered, and what is rendered is what is near the viewport.
 *
 * IT COUNTS THREADS ONLY. It used to count suggestions too — a total, a
 * per-kind breakdown and an allowlist of kinds holding the two together — and
 * the payload no longer carries any, so every one of those numbers was zero.
 *
 * `threads` IS EVERY UNSENT INSTRUCTION, the whole-document ones included,
 * because `verdictLabel` turns `threads === 0` into **Approve**, and a total
 * that left the whole-document instruction out would offer Approve on a
 * document with an instruction still to send.
 */
export function censusCounts(
  view: { comments?: PendingThread[] } | null | undefined,
) {
  const threads = (view && view.comments) || [];
  return { threads: threads.length };
}

/* --- ✓ all IS GONE, AND SO IS EVERY WORD IT SPOKE ---------------------------
 *
 * `SWEEP_ACCEPT`, `SWEEP_SETTLE`, `sweepLabel`, `SWEEP_TITLE` and `sweepSaid`
 * were the sweep's label, its tooltip and the sentence the readout printed
 * after it. The button they dressed POSTed /_galley/sweep — one of the twelve
 * endpoints internal/serve/rounds_surface_test.go asserts are 404 — and
 * `makeCensus` had already stopped appending it to the strip, so nothing could
 * be pressed and nothing could be said. Both populations it acted on are gone
 * with the proposal era: there are no proposals to accept in bulk and no
 * conversations to settle.
 *
 * WHAT THE COPY GOT RIGHT IS WORTH KEEPING EVEN THOUGH THE BUTTON IS NOT.
 * `SWEEP_TITLE` was rewritten once because it PROMISED AN EXEMPTION IT DID NOT
 * GIVE — "open questions survive", over a predicate (`Answered()` = the agent
 * spoke last) that closed every agent question nobody had replied to. Measured
 * on a clean fixture, one click resolved three never-answered threads with no
 * confirmation, no count and no undo. The rule that came out of it outlives the
 * button: a bulk verb's label carries the counts it will act on, its tooltip
 * describes the act rather than the exemption, and the readout afterwards says
 * what the SERVER did rather than what the button intended.
 */

/**
 * unplacedSaid labels the small in-rail section for instructions whose
 * original text anchor is gone. The instruction cards themselves remain
 * visible and actionable below it; this is a heading, not a pointer to a
 * second surface.
 *
 * IT NAMES THE STATE RATHER THAN COUNTING IT, and that is the change. `1
 * unplaced instruction` is a number in a place where the reviewer can already
 * see how many cards are under it, and "unplaced" alone says nothing about
 * WHY — the card below looks like every other card, and the one fact that
 * explains it (the words it was about are not in the document any more) was
 * being carried by an apologising grey sentence inside the card instead. The
 * head is the honest home for it: one line of chrome voice, said once for the
 * section, rather than repeated on every card in it. Board 1b.
 *
 * The plural keeps a count because at two or more "its words" is wrong and the
 * number is the cheapest thing that makes the sentence true again.
 */
export function unplacedSaid(n: number | string | null | undefined): string {
  const count = Number(n) || 0;
  if (count < 1) {
    return '';
  }
  return count === 1
    ? 'unplaced \u00b7 its words were removed'
    : `unplaced \u00b7 ${count} \u00b7 their words were removed`;
}

/**
 * stepPending returns the run `j` or `k` moves to next.
 *
 * Wrapping, and starting at opposite ends: with nothing stepped yet, forward
 * starts at the top of the document and back starts at the bottom, so neither
 * key is a no-op the first time it is pressed. A current run that is no longer
 * in the list — it was just decided, or an agent's edit landed — starts over
 * rather than reporting nothing.
 *
 * @param runs pending run ids in document order
 */
export function stepPending(
  runs: string[],
  current: string | null,
  direction: 1 | -1,
): string | null {
  if (runs.length === 0) {
    return null;
  }
  const at = current === null ? -1 : runs.indexOf(current);
  if (at === -1) {
    return direction < 0 ? runs[runs.length - 1] : runs[0];
  }
  return runs[(at + direction + runs.length) % runs.length];
}

/**
 * keyTargetIsEditable reports whether a keystroke belongs to whatever the
 * reviewer is typing into, rather than to the rail.
 *
 * The document itself is contenteditable, so this is what keeps `j` from
 * stepping the rail while someone is writing the letter j. Esc is the way out:
 * it blurs the field, and then the single-letter keys are the rail's.
 */
export function keyTargetIsEditable(el: unknown): boolean {
  if (!el || typeof el !== 'object') {
    return false;
  }
  if ('isContentEditable' in el && el.isContentEditable) {
    return true;
  }
  // `el` narrows only to "has a tagName property", not to "that property is
  // a string" — `el.tagName` is still `unknown`. A real DOM element's
  // `tagName` is always a string, so naming that instead of coercing
  // whatever showed up is both the honest type and the fix for `String()`
  // printing "[object Object]" for anything that was not one.
  const tag =
    'tagName' in el && typeof el.tagName === 'string'
      ? el.tagName.toLowerCase()
      : '';
  return tag === 'input' || tag === 'textarea' || tag === 'select';
}

/**
 * submitOnEnter is the ONE keydown contract for every place galley takes prose
 * from the reviewer: Enter files what is typed, Shift-Enter breaks the line.
 *
 * IT IS ONE FUNCTION BECAUSE IT WAS THREE HANDLERS AND ONLY TWO OF THEM
 * EXISTED. The rail's reply box and the proposal card's reply box each carried
 * their own copy of these six lines; the composer that CREATES a comment
 * carried none, so a reviewer who typed a comment and pressed Enter — the
 * gesture that had just worked in the reply box one card away — got nothing,
 * and the whole-document note filed only through a single-line input's
 * implicit form submission, where Shift-Enter could not break a line at all.
 * A contract spelled once per surface is a contract a fourth surface is added
 * without. Every composer in this editor goes through here; a new one that
 * does not is the same bug again.
 *
 * The submit is a CALLBACK rather than a form: two of the four surfaces post
 * straight to an endpoint and have no form to submit, and threading a fake one
 * through them would be furniture in place of the one line that matters.
 *
 * THE GUARD IS FOUR CONDITIONS AND EVERY ONE OF THEM FILES SOMETHING NOBODY
 * ASKED FOR.
 *
 *   shiftKey     Shift-Enter breaks the line. The original contract.
 *   repeat       A HELD Enter auto-repeats, and each repeat is a keydown: one
 *                comment filed PER REPEAT, at the keyboard's rate, each one a
 *                POST and each POST a whole-document rebuild (CLAUDE.md — every
 *                server-side mutation replaces the document). A finger resting
 *                a moment too long on the key that files is an ordinary
 *                accident, and N identical threads is not a state anything in
 *                galley can undo. This surface is where Enter first became
 *                able to file at all, so the exposure came in with it.
 *   isComposing  An IME's Enter COMMITS the candidate being composed — it is
 *                not a submit, and taking it as one files a half-written
 *                sentence and steals the commit.
 *   keyCode 229  The same claim, made the way older and non-conforming
 *                browsers make it: some fire the composition's Enter with
 *                `isComposing` already false and only the legacy keyCode left
 *                to say so. Reading both is the standard belt-and-braces here,
 *                and the cost of the second read is nothing.
 *
 * IN-FLIGHT IS THE CALLER'S, AND IT IS NOT OPTIONAL. This guard stops the key
 * repeating; it cannot stop a second deliberate press landing while the first
 * POST is still out. Every `submit` passed in here disables its field for the
 * duration of its own write (the discipline `fileNote` already had) — see
 * App.sendComment and App.sendReply.
 *
 * @param field the textarea or input
 * @param submit what Enter files
 * @returns the field, so a caller can chain
 */
export function submitOnEnter<
  T extends {
    addEventListener(
      type: 'keydown',
      listener: (event: KeyboardEvent) => void,
    ): void;
  },
>(field: T, submit: () => void): T {
  field.addEventListener('keydown', (event) => {
    if (
      event.isComposing ||
      event.keyCode === 229 ||
      event.key !== 'Enter' ||
      event.shiftKey ||
      event.repeat
    ) {
      return;
    }
    event.preventDefault();
    submit();
  });
  return field;
}

/**
 * growOnInput makes an instruction box as tall as what is typed into it.
 *
 * COURT ASKED FOR THIS ON THE WHOLE-DOCUMENT BOX and it belongs to every box
 * for the same reason `submitOnEnter` does: `rows` is a STARTING height, not a
 * size, and a field that keeps its starting height while the sentence grows
 * hides the beginning of what the reviewer is writing behind its own scrollbar.
 * Three surfaces set `rows` and none of them grew — the whole-document input,
 * the composer and the instruction edit box, all `rows = 5` now — so the
 * contract is spelled once here and each of the three calls it, exactly as they
 * each call `submitOnEnter`.
 *
 * THE CAP IS THE STYLESHEET'S, NOT THIS FUNCTION'S, and that is deliberate.
 * The three boxes share one `max-height`, half the window, in one rule in
 * editor.css: room for many lines, never the whole window.
 * A number here would be a second cap, in a second language, disagreeing with
 * the first the day either moved — the twin-carrier defect this repository
 * records more than any other. So the height is written, the browser clamps
 * it, and `overflow-y` is set from whether the clamp bit.
 *
 * `height = 'auto'` FIRST, because `scrollHeight` is never smaller than the
 * height already set: without the reset a box that grew to six lines stays six
 * lines after the reviewer deletes five of them.
 *
 * @param field the textarea
 * @returns the field, so a caller can chain
 */
export function growOnInput(field: HTMLTextAreaElement): HTMLTextAreaElement {
  const fit = () => {
    field.style.height = 'auto';
    const want = field.scrollHeight;
    // THE BORDER TOO. `scrollHeight` stops at the padding, and the boxes are
    // `border-box`, so writing it alone made every fitted box its own border
    // shorter than the rows it opened at, clipping the last line by 2px.
    const edge = field.offsetHeight - field.clientHeight;
    field.style.height = `${want + (edge > 0 ? edge : 0)}px`;
    // Read back what the stylesheet's own `max-height` allowed. Equal means the
    // clamp bit, so the box scrolls; otherwise nothing is hidden and a
    // scrollbar would be furniture.
    field.style.overflowY = field.clientHeight < want ? 'auto' : 'hidden';
  };
  field.addEventListener('input', fit);
  // Reset is the caller's, through the same event the reviewer's typing fires:
  // `input` is not dispatched by assigning `value`, so every surface that
  // clears its box dispatches one. One entry point, no second method to forget.
  return field;
}

// --- what a thread is about ---

/** How much of the anchor a card's head quotes. A RESERVE, stated out loud to
 * be one, in the shape `.gly-census-count` (24ch) and the whole-doc handle
 * (16ch) already use: the head is chrome in the 10px mono voice, and the anchor
 * it names is whatever the reviewer selected. Court selected a multi-block
 * passage and got roughly twenty lines of capitals back as a card head — the
 * card was mostly its own title.
 *
 * MEASURED against the card the head sits in, not guessed. The rail is 320px
 * (`--gly-rail-w`) less its 16px right padding and the band's 26px gutter
 * (`--gly-rail-gutter`), and the card spends 0.6rem of padding either side plus
 * its 1px borders: 256.8px of measure. The head renders at 6.96px per character
 * in this stack (the rate `COMPOSER_QUOTE_CHARS` was measured at, same family,
 * same 0.62rem), so one line holds 36 characters. TWO LINES is the bound — a
 * head is allowed to wrap once and no further — and the fixed words spend 24 of
 * the 72 (`INSTRUCTION · ` is 14, ` · about now` at its longest is 10), leaving
 * 48. 44 is taken, so the last character of the longest age is still inside the
 * bound rather than exactly on it.
 *
 * Retune it WITH the rail's width or the head's type, never on its own: a bound
 * stated against a specific pair lies the moment either of them moves. The
 * anchor is not LOST by this — `lostAnchorLine` quotes it whole in the card's
 * body, in the removal vocabulary, which is where the reviewer reads words
 * rather than a title. */
export const HEAD_QUOTE_CHARS = 44;

/** elide is the bound, applied. One function, so the two heads that quote an
 * anchor (`threadLabel`'s range and block branches) cannot come to disagree
 * about where the cut is. Whitespace is collapsed first: a selection spanning
 * blocks arrives with the newlines in it, and a head is one line of chrome. */
export function elide(said: string, max: number = HEAD_QUOTE_CHARS): string {
  const flat = (said || '').replace(/\s+/g, ' ').trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
}

/**
 * threadLabel says what a thread card is about, and whether it has lost its
 * place.
 *
 * THREE SHAPES, AND ONLY ONE OF THEM CAN BE ADRIFT. A range thread hangs on a
 * highlight in the prose; a block thread is about a whole block (a figure, a
 * heading, a fence); a document thread is about the file. Only the first has a
 * mark to lose, so only the first can be "not tied to a mark" — and that is
 * exactly the sentence the rail used to print on all three, because before the
 * anchors landed "no run" and "adrift" were the same fact. On a document thread
 * it is simply false, and it reads as a comment that lost its place when
 * nothing of the kind happened.
 *
 * A section thread gets §, because the grip that opens one scopes a whole
 * section and the reviewer needs to see that is what the thread is on rather
 * than a line of prose that happens to be a heading. The heading arrives as its
 * markdown label ("## Design"), which is the Go side's blockLabel; the hashes
 * are how a *file* names a level and are noise on a card that already says §.
 *
 * @param state whether a RANGE thread found its mark
 */
export function threadLabel(
  thread:
    | {
        anchor?: string;
        heading?: string;
        blockKind?: string;
        // `| null`: WIDENED, not narrowed — same reason threadPlacement's own
        // `region` field carries the note. `region` is only ever checked for
        // truthiness below (`if (t.region) { … }`), never read for a
        // property, so `null` is exactly as inert here as `undefined` was.
        // A real Thread's own `region` is `Region | null` (appshell.ts),
        // which `object | undefined` alone could not accept.
        region?: object | null;
      }
    | null
    | undefined,
  state?: { anchored?: boolean } | null,
): { label: string; adrift: boolean; note: string } {
  const t = thread || {};
  const heading = t.heading || '';
  if (t.anchor === 'document') {
    // `whole document`, not `on the whole document`: the head is a mono
    // chrome line reading `INSTRUCTION \u00b7 WHOLE DOCUMENT \u00b7 AGE`, and the
    // preposition was carrying a sentence that head no longer is. Board 1b.
    return { label: 'whole document', adrift: false, note: '' };
  }
  if (t.anchor === 'block') {
    if (t.region) {
      // §6's verbatim string. A region thread is about a rectangle nobody can
      // name in words, so the card says what KIND of thing it is on and the pin
      // on the figure says which.
      return { label: 'on figure region', adrift: false, note: '' };
    }
    if (t.blockKind === 'heading') {
      return {
        label: `on §${elide(heading.replace(/^#+\s*/, ''))}`,
        adrift: false,
        note: '',
      };
    }
    return {
      label: `on ${elide(heading) || 'a block'}`,
      adrift: false,
      note: '',
    };
  }
  const anchored = !state || state.anchored !== false;
  // THE APOLOGY IS DELETED AND THE FACT IS KEPT, WHICH ARE NOT THE SAME THING.
  // The note used to read *"this instruction is not tied to a mark — its
  // original highlight is gone; you can still delete it"*: three clauses, of
  // which the third tells the reviewer about a verb they are looking at, the
  // first two say the same thing twice, and none of them says WHICH WORDS
  // WENT — the only fact the reviewer cannot recover from the screen. The
  // section head names the state once (unplacedSaid) and the card QUOTES its
  // lost anchor struck through in its own body (see threadCard's lostAnchor),
  // in the removal vocabulary the prose already speaks. `adrift` is unchanged
  // and still the thing that dashes the card.
  return {
    label: elide(heading) || 'comment',
    adrift: !anchored,
    note: '',
  };
}

// THE FOLD IS GONE, AND SO IS EVERYTHING THAT SERVED IT.
//
// `foldCards`, `FOLD_CAP` and `moreLabel` split the measured cards into a map
// and two dim clusters clamped to the edges of the VIEWPORT, with a "+N more"
// line counting whatever the cap kept out of them. Every one of them existed
// to manage the overflow that `position: fixed` on `.gly-rail` guaranteed: a
// viewport-locked column holding unbounded content. The rail is as tall as the
// document now and scrolls with it, so work below the window is reached by
// scrolling to it, the bar's census is the ONE readout of what is outstanding,
// and there is nothing left to clamp, dim, cap or count.
//
// `dimCard` went with them, and its absence is load-bearing rather than
// incidental: it was the other writer of the accept/reject/resolve/delete
// `disabled` flags, so the seal is now their sole owner — see SEALED_VERBS in
// entry.ts, whose invariant this makes easier to hold rather than harder.
//
// See docs/superpowers/specs/2026-08-16-the-rail-scrolls.md.

// --- what the reviewer was in the middle of typing ---

/**
 * carryDrafts decides what to put back into a rebuilt card, and it exists
 * because the rail is rebuilt from scratch on every pending refresh — the 1.5s
 * poll and every mutation, INCLUDING the agent's own reply landing.
 *
 * A half-typed reply is destroyed with the card it was typed into. The
 * codebase already knew this hazard and answered it once: the overall thread
 * was given its own slot so that "a poll that rebuilt this would take the
 * caret out of a sentence someone was in the middle of writing". Thread cards
 * never got the same protection, and in live mode the loop closes — answering
 * a reviewer is what wipes what they are writing back.
 *
 * The unit of identity is the FIELD KEY, not the element and not an ordinal: a
 * card is a different DOM node after every repaint, and its position in the
 * rail renumbers whenever a thread above it resolves (see CLAUDE.md — an
 * ordinal is not identity). A reply field is keyed by its thread's stable
 * `key`, so the text goes back into the same conversation it was aimed at or
 * it goes nowhere.
 *
 * THREE THINGS TRAVEL, not one. The text alone is not enough: putting a
 * sentence back with the caret at position 0 is still taking the caret out of
 * it, and putting it back without focus means the next keystroke goes to the
 * document. Value, selection and focus move together or the fix is cosmetic.
 *
 * A field that no longer exists — its thread resolved while the reply was
 * being written — is dropped rather than resurrected. Nothing here creates a
 * card; a draft is carried across a rebuild, never held against one.
 *
 * @param saved what was in the fields before the rebuild
 * @param fields the fields that exist now
 * @returns what to write back, in `fields` order
 */
export function carryDrafts(
  // TASK 8 CORRECTION: `start`/`end` were declared `number`. entry.ts's own
  // captureDrafts builds this array straight from a real textarea's
  // `.selectionStart`/`.selectionEnd`, which the DOM types honestly as
  // `number | null` (null on an input type that does not support a
  // selection) — and this function's own body already treats a non-finite
  // bound as "the end" (`Number.isFinite(d.start) ? d.start : limit`), so
  // null was always a real, handled input, just an untyped one.
  saved:
    | {
        key: string;
        value: string;
        start: number | null;
        end: number | null;
        focused: boolean;
      }[]
    | null
    | undefined,
  fields: { key: string; value: string }[] | null | undefined,
): {
  key: string;
  value: string;
  start: number;
  end: number;
  focus: boolean;
}[] {
  const drafts = new Map<
    string,
    {
      key: string;
      value: string;
      start: number | null;
      end: number | null;
      focused: boolean;
    }
  >();
  for (const d of saved || []) {
    // First wins. Two fields under one key is a bug in the caller; picking the
    // later one would silently prefer whichever the DOM happened to order last.
    if (d && d.key && !drafts.has(d.key)) {
      drafts.set(d.key, d);
    }
  }
  const out: {
    key: string;
    value: string;
    start: number;
    end: number;
    focus: boolean;
  }[] = [];
  for (const field of fields || []) {
    const d = drafts.get(field.key);
    if (!d) {
      continue;
    }
    const had = String(d.value || '');
    if (!had && !d.focused) {
      // An empty unfocused field is not a draft. Restoring it would be a write
      // that says nothing, and it would steal focus from wherever it went.
      continue;
    }
    // The rebuilt field wins on TEXT if it somehow has any — a card built with
    // content in its input is a card that knows something we do not — but the
    // caret and the focus still come back, because those are the reviewer's.
    const fresh = String(field.value || '');
    const value = fresh ? fresh : had;
    const limit = value.length;
    const start = clamp(
      typeof d.start === 'number' && Number.isFinite(d.start) ? d.start : limit,
      0,
      limit,
    );
    const end = clamp(
      typeof d.end === 'number' && Number.isFinite(d.end) ? d.end : start,
      start,
      limit,
    );
    out.push({ key: field.key, value, start, end, focus: !!d.focused });
  }
  return out;
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(Math.max(n, lo), hi);
}

// --- where a thread belongs ---

/**
 * threadPlacement answers the one question the rail was asking wrong: does
 * this thread have a PLACE in the document, and if so how is it found?
 *
 * `!!thread.run` used to stand in for that, and it was right for exactly as
 * long as every thread hung on a mark. A block thread and a figure-region
 * thread have no mark BY CONSTRUCTION — that is the whole reason docmodel.Note
 * exists — so both fell through to the anchorless region at the rail's foot,
 * far from the figure they are about, and both were still drawn with a
 * connector arm reaching left to a position that means nothing. One wrong
 * predicate, two symptoms.
 *
 * A block thread is not anchorless. It names a block key, `/_galley/pending`
 * reports every addressable block with the INDEX of that key among the
 * document's top-level children, and the browser can measure that child. So
 * the answer is three-valued, not two:
 *
 *   mark        a range thread on a highlight — measured through its run
 *   block       a block or figure thread — measured through its block's element
 *   anchorless  genuinely nowhere: a range thread whose highlight is gone or
 *               ambiguous, or a block key this pending refresh does not know
 *
 * Only the first two get a connector, and that is the rule rather than a
 * detail: the line exists to say "this card is about THAT", and a card with no
 * place has no THAT to point at. Drawing one anyway is the visible half of the
 * same mistake.
 *
 * THE THIRD VERDICT NAMES A CONDITION AND NOT A PLACE, AND THAT IS WHY IT
 * SURVIVED THE PLACE BEING DELETED TWICE. It was first called `foot`, for the
 * foot of a fixed viewport-height column; when the rail became document-tall it
 * was renamed for the CONDITION, and rendered in a section in flow after the
 * map. The rail holds live work only now — *here is what needs you, beside the
 * text it is about* — and a card that is beside nothing is not that, so the
 * old loose section was deleted, then the count stopped opening the sheet and
 * the full card returned in `.gly-rail-unplaced`. The verdict is unchanged,
 * because it was never about where the card went: it tells each surface that
 * this card must stay in flow and must not offer a jump to missing prose.
 *
 * The index comes from the server's block list and is used IMMEDIATELY, never
 * stored: it renumbers the instant anything is inserted above it, which is why
 * the key exists. Same rule as an ordinal suggestion id.
 *
 * @param blocks the pending payload's blocks
 */
export function threadPlacement(
  thread:
    | {
        run?: string;
        anchor?: string;
        anchorKey?: string;
        // `| null`: WIDENED, not narrowed — `region` is never read on the
        // input side below (`t.run`/`t.anchor`/`t.anchorKey` are the whole of
        // it), so this was always an inert field on this type. A real
        // Thread's own `region` is `object | null` (see appshell.ts), which
        // `null` on this input type could not previously accept; nothing
        // about what this function DOES changes.
        region?: object | null;
      }
    | null
    | undefined,
  blocks: { key: string; index: number }[] | null | undefined,
): {
  where: 'mark' | 'block' | 'anchorless';
  run: string;
  index: number;
  region: object | null;
} {
  const t = thread || {};
  if (t.run) {
    return { where: 'mark', run: t.run, index: -1, region: null };
  }
  if (t.anchor === 'block' && t.anchorKey) {
    const block = (blocks || []).find((b) => b && b.key === t.anchorKey);
    if (block && Number.isFinite(block.index) && block.index >= 0) {
      // The region rides along so the card can sit beside the RECTANGLE rather
      // than beside the top of a tall picture — the pin and the card then name
      // the same height, which is the only way the pairing reads.
      return {
        where: 'block',
        run: '',
        index: block.index,
        region: t.region || null,
      };
    }
  }
  return { where: 'anchorless', run: '', index: -1, region: null };
}

// --- the overall thread ---

/**
 * overallThreads and railThreads split the thread list in TWO, and they are
 * written as one group so the parts cannot drift into overlapping — or, worse,
 * into leaving a thread out.
 *
 * The overall card is permanent and the document-anchored threads are its
 * entries. If the rail ALSO carded them, a note about the whole file would
 * render twice — which is R9's complaint ("a comment rendered as two objects")
 * arriving again through a new surface rather than through the old one. One
 * predicate, used from both sides, is what makes that impossible instead of
 * merely unlikely.
 *
 * There is no third, settled part any more: nothing resolves an instruction.
 * A sent one leaves with its round (the send's clear deletes it) and a
 * retracted one is deleted, so every thread the page holds is open.
 *
 * The two are exhaustive and mutually exclusive over any thread list, which
 * probe.mjs asserts as a partition rather than as two separate filters.
 */
// GENERIC OVER `T extends PendingThread`, AND NOT JUST `PendingThread`
// ITSELF, so a caller whose own thread type carries more than this file
// reads (web/appshell.ts's `Thread`, built for the App the mixins share)
// gets that richer type BACK. A non-generic filter would answer
// `PendingThread[]` regardless of what went in, which loses every field a
// caller added — exactly the shape that forced sheet.ts's threadCard calls
// back through a second, poorer type. The filtering itself is unchanged.
export function overallThreads<T extends PendingThread>(
  threads: T[] | null | undefined,
): T[] {
  return (threads || []).filter((t) => t.anchor === 'document');
}

export function railThreads<T extends PendingThread>(
  threads: T[] | null | undefined,
): T[] {
  return (threads || []).filter((t) => t.anchor !== 'document');
}

// THE CHANGED REGION IS GONE FROM BOTH SURFACES, AND SO IS EVERYTHING THAT
// SERVED IT.
//
// `changedHandle` ("△ 3 changed"), `changedKey`, `readChangedOpen` and
// `writeChangedOpen` were the settled region's siblings for the trail: a
// collapsible, counted log of the reviewer's own direct edits, at the foot of
// the rail and again at the foot of the sheet. They are deleted because THE
// TRAIL IS NOT A HISTORY — see the outgoingCounts note above for the argument.
// Nobody browses it, so nothing needs a collapse state remembered per document
// for it, and a section that is never opened is chrome about nothing.
//
// The ghosts in the prose are untouched: they are the trail's visible form and
// they answer the only question anyone asks of it in the moment (`did that
// land`). What replaces the log is the COUNT ON THE BUTTON THAT SENDS IT.
//
// See docs/superpowers/specs/2026-08-16-the-rail-holds-live-work.md.

/**
 * changesSaid heads the rail's section of the reviewer's OWN edits.
 *
 * ONE SURFACE, ALL OF IT. Court: "the list on the right rail is the list of
 * instructions going to be revised… one surface. all of the instructions."
 * The rail held instruction threads only, which is half of what a round
 * carries — the reviewer's hand edits are sent too, and until now there was no
 * surface for them anywhere. The answer previously proposed was a SECOND list
 * reachable from the Revise button; the answer taken is that there is one list
 * and it was missing half its contents.
 *
 * It counts, unlike `unplacedSaid`, because the number is the thing a reviewer
 * checks before pressing send: how much of this round is mine.
 */
export function changesSaid(n: number | string | null | undefined): string {
  const count = Number(n) || 0;
  if (count < 1) {
    return '';
  }
  return count === 1 ? 'your edit \u00b7 1' : `your edits \u00b7 ${count}`;
}

/**
 * changeLine is what one edit card says it did, in the vocabulary the product
 * already uses for removal and arrival.
 *
 * A REWORD CAN ARRIVE AS TWO ENTRIES, and that is stated rather than smoothed
 * over: `summarise` reports the diff's own ops, and the diff pairs a delete
 * with an insert into one "changed" only when its units align. Measured on a
 * real edit — "Gamma three here." to "Gamma three, reworded." — it came back as
 * an `added` and a `removed` rather than one `changed`. Rendering them as two
 * cards is honest about what the agent is being told; inventing a pairing here
 * would be a second opinion about a diff this codebase computes in one place.
 */
export function changeLine(kind: string): string {
  switch (kind) {
    case 'removed':
      return 'removed';
    case 'added':
      return 'added';
    case 'changed':
      return 'changed';
    default:
      return kind;
  }
}
