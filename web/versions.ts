// versions.ts — History, which is a READING MODE and not a second application.
//
// THIS SURFACE COMPUTES NOTHING. The diff, the region ordinals, the change
// count and the join between an ask and the agent's note about it all come off
// the server already decided, because there is one implementation of each and
// it is Go's. A second one in the browser would agree on every document anyone
// tried and disagree on the one that mattered — the shape this codebase has
// paid for repeatedly (a run stamped in two places, a pairing re-derived across
// a wire, six agreeing spellings and one silence).
//
// IT COVERS, IT DOES NOT DISPLACE. `body.gly-history-mode` hides `main`; this
// section lives in `body` and never inside `.ProseMirror`, because anything
// appended in there is content and the next projection writes it to the
// author's file. Esc closes it, nothing about it traps focus, and the draft's
// scroll position is put back on the way out.
//
// TWO STAGES, AND THE SECOND ONE IS THE DRAFT WEARING DIFF MARKS.
//
//   - THE LANDING is the current document, plain, beside a rail of round cards
//     newest first. It answers "what has happened to this document", and it
//     answers it in the document's own type, because a reader who has to
//     re-learn a typeface to read their own file has been handed a second
//     product rather than a second view of the first one.
//   - THE READING STATE is one round: the same paper, the same measure, the
//     same 17px/1.7, wearing the marks of that round; and one card per change,
//     carrying the ask that produced it above the agent's own sentence about
//     it. History used to ship a second typography (15px/1.6, a 46rem measure)
//     and it is DELETED — see the stylesheet, where the argument is repeated
//     next to the rules that would otherwise be written again.
//
// THE HISTORY IS ONE LIST AND THEN IT STOPS. Squashing, grouping, filtering and
// a timeline were all considered by the spec and are recorded there as
// DELIBERATELY NOT BUILT: the history is insurance, it is unlikely to be read,
// and building a browsing surface against a list nobody opens is how a phase
// spends itself. A live hour can produce forty rounds; the record stays correct
// and stops being pleasant, and that is the trade, taken on purpose.

// VIEWS are the server's own two, in the order the reader is offered them. The
// labels are the sub-bar's own lowercase mono, which is the prototype's — a
// chip in the chrome layer never converses and never capitalises.
// ONE RAIL. The stack, the gap, the gutter and the breakpoint are the
// INSTRUCTION rail's, imported rather than re-spelled — see web/card.ts's
// header for what the second spelling of each of them cost. `stackCards` is the
// same arithmetic `paintAnchors` uses; the card anatomy and the reveal are the
// same ones a thread card wears.
import { RAIL_MIN_WIDTH } from './rail.ts';
import {
  cardShell,
  cardBody,
  revealOn,
  revealMark,
  placeCards,
  setStackHeight,
  coalesce,
  growthWatch,
} from './card.ts';

// THE WIRE'S OWN TYPES, GENERATED FROM THE GO STRUCTS THAT SEND THEM.
//
// web/wire.d.ts is written by internal/serve/wire_test.go and checked by `just
// verify`, so a field renamed on the Go side is a RED BUILD here rather than a
// `TypeError: Cannot read properties of undefined` weeks later in a gate
// nobody ran — which is exactly what `pending.suggestions` becoming
// `pending.instructions` cost. Nothing in this file describes the payload in
// its own words; describing it twice is how the two copies come to disagree.
import type { RoundView, ChangeView, DiffView, VersionsView } from './wire';

// An ARRIVAL is NOT a round, and this interface is the place that says so. It
// is assembled in the browser from `GET /_galley/revise` — which carries the
// number that landed and the agent's exception, and nothing else — so it has
// no wire struct of its own and must not borrow RoundView's.
export interface Arrival {
  n: number;
  exception?: boolean;
  why?: string;
}

// VERSIONS_LABEL is the bar's visible door to the record. It was reduced to a
// bare glyph when the pre-rounds bar was full; phase 3 removed the controls
// that created that pressure, and the first real review could not find the
// views or the history behind the symbol. The surface is important enough to
// name in the interface, especially when an arrival highlights this button.
//
// It never changes on its own click — see makeVersionsButton — so it cannot
// slide its neighbours out from under the cursor that pressed it.
export const VERSIONS_LABEL = 'History';

// VERSIONS_NAME is that control's accessible name and its tooltip: the word the
// glyph stands in for, spelled ONCE so the label, the title and the panel's own
// head cannot drift apart.
export const VERSIONS_NAME = 'history';

export const VIEWS = [
  { key: 'inplace', label: 'changes' },
  { key: 'sbs', label: 'side by side' },
];

// DEFAULT_VIEW is the marks. A reader who has opened a ROUND has already asked
// the question `in place` answers — what moved — so making them press one more
// control to see it would be a surface asking a question it can answer. The
// document plain is still what the LANDING shows, which is where decision 6's
// "v12 is just v12" now lives.
export const DEFAULT_VIEW = 'inplace';

// COULD_NOT is the exception's reason word, spelled once. It is the agent's
// report that it could not do what was asked — a round with the document
// unchanged, which is exactly what is being recorded.
export const COULD_NOT = 'could-not';

// The two stages, named so a caller can ask which one is on screen without
// knowing how it is spelled in a class.
export const STAGE_ROUNDS = 'rounds';
export const STAGE_READING = 'reading';

// ALL_ROUNDS is the quiet handle back to the landing. `‹` and not `←`: the
// primary slot carries `← back to draft`, which leaves History altogether, and
// two arrows of the same weight a few inches apart would read as two spellings
// of one gesture.
export const ALL_ROUNDS = '‹ all rounds';

// BACK_TO_DRAFT is the way out, and it lives in the PRIMARY's slot — the one
// place on this page a reviewer already looks for "the thing to press next".
export const BACK_TO_DRAFT = '← back to draft';

// The three fixed sentences of the sub-bar and the rail, spelled once because
// each is asserted somewhere and a second spelling is how the two drift.
export const IDENTICAL_SAID = 'identical — no changes in this round';
const ROUNDS_TITLE = 'ROUNDS — NEWEST FIRST';
const SEED_HEAD = 'V1 · STARTING VERSION';
const SEED_SAID = 'The file as galley opened it.';
const UNANSWERED_HEAD = 'ASK · NOT ANSWERED';

// emptySaid is what the panel says when there is nothing yet. It names the
// gesture that makes a round, because a surface that says only "nothing here"
// leaves the reviewer to guess what would put something in it.
const EMPTY_SAID = 'No history yet — the first Revise creates a version.';

const RESTORE_ARM_MS = 4000;
// The armed label, spelled once. Its idle partner carries a version number and
// is composed at paint time.
const RESTORE_ARMED = 'replace draft?';
// The third face of the restore button. It is a LABEL SPAN like the other two
// and not a `textContent` write, and that distinction is the whole of the bug
// this constant exists to close — see paintRestore.
const RESTORE_BUSY = 'restoring…';

// `CARD_GAP = 12` WAS HERE, AND IT IS DELETED. It was this rail's own name for
// the number `RAIL_GAP` has been in rail.ts since the instruction rail was
// built, and the two disagreed by two pixels for no reason anybody chose —
// which is the whole shape of this file's relationship to that one before
// 2026-08-20. One stack, one gap, imported above.

// ageSaid is the round card's second clause — how long ago, in the chrome's
// own shorthand. It is deliberately COARSE: a card that says `4m ago` and a
// card that says `4m 12s ago` carry the same information and only one of them
// is scannable down a column of five.
export function ageSaid(at: string, now: number = Date.now()): string {
  const t = Date.parse(at || '');
  if (!Number.isFinite(t)) {
    return '';
  }
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 45) return 'JUST NOW';
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${Math.max(1, mins)}M AGO`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}H AGO`;
  return `${Math.round(hours / 24)}D AGO`;
}

// changedSaid is `k changes`, singular where it has to be. The number is the
// SERVER'S — `roundView.Changed`, computed by diff.Regions on every request —
// and never a count of marks this page managed to draw. THE COUNT NEVER LIES.
export function changedSaid(k: number): string {
  if (!k) {
    return 'no changes';
  }
  return k === 1 ? '1 change' : `${k} changes`;
}

// roundHead is a round card's first line: WHICH ROUND, and WHEN. The ordinal is
// the round's position among the rounds that are WORK — v1 is the file as it
// arrived and is not a round anybody had — so `ROUND 1` is v2, which is what
// the reviewer counts in their head and what the prototype shows.
export function roundHead(
  ordinal: number,
  round: RoundView,
  now: number = Date.now(),
): string {
  const age = ageSaid(round.at, now);
  return age ? `ROUND ${ordinal} · ${age}` : `ROUND ${ordinal}`;
}

// roundFoot is the card's last line: which version this round produced, and how
// far it moved the document. An exception says so in the same slot, because
// that slot is where the eye already is and because there IS no count: nothing
// moved, which is the fact being recorded.
export function roundFoot(round: RoundView): string {
  if (round.reason === COULD_NOT) {
    return `v${round.n} · the agent could not`;
  }
  // NO ARROW HERE. ← is the answer's, and the foot is a version and a count.
  return `v${round.n} · ${changedSaid(round.changed)}`;
}

// askedOf is THE PAIRING, and it is the whole reason the history is worth
// keeping. A diff says what moved; a diff beside the instruction that caused it
// says whether the agent understood you, and no git log of a document can tell
// you that.
//
// A round carries its own instructions when it IS the ask, and points at them
// when it is the answer. EACH INSTRUCTION SEPARATELY, as the server lists them
// (`instructions`, from the round's recorded asks), so each is its own line in
// History with its own line breaks. A round written before asks were recorded
// has only the server's `·`-joined sentence, `instruction` or `asked`, and that
// one string is the list; it is never split or re-joined here.
export function askedOf(round: RoundView): string[] {
  if (round.instructions && round.instructions.length > 0) {
    return round.instructions;
  }
  if (round.instruction) {
    return [round.instruction];
  }
  if (round.asked) {
    return [round.asked];
  }
  return [];
}

// askOn and answerOn split what one field was carrying two of.
//
// `Round.Instruction` means DIFFERENT THINGS on the two cuts of an exchange: on
// the reviewer's it is the ask, and on the agent's landing it is the sentence
// the agent wrote when it acked. roundCards folds the ask INTO the landing —
// they are one exchange — so the card's own `instruction` is the agent's, and
// rendering it after a `→` put the answer on screen wearing the ask's arrow
// while the ask itself appeared nowhere. Measured on a real round: the card
// read `→ Defined exhaustion at the top of the section it names.` over an
// instruction that had said `what does exhaustion mean here`.
//
// The arrows are the grammar of this whole surface — → is what you asked, ← is
// what came back — so they have to be fed from the two rounds separately.
// roundNamed is the one lookup by round number this file makes.
function roundNamed(rounds: RoundView[], n: number): RoundView | null {
  return (rounds || []).find((r) => r.n === n) || null;
}

// NO_ROUND is a real, fully populated RoundView standing in for "no round is
// selected" — see paintChanges, the one caller. Every field reads its own
// zero value, which is the same value every reader here already gave an
// absent field.
const NO_ROUND: RoundView = {
  n: 0,
  at: '',
  authors: '',
  reason: '',
  instruction: '',
  answers: 0,
  asked: '',
  changed: 0,
};

function askOn(rounds: RoundView[], card: RoundView): string[] {
  if (card.answers > 0) {
    const asked = rounds.find((r) => r.n === card.answers);
    if (asked) {
      return askedOf(asked);
    }
    if (card.askedInstructions && card.askedInstructions.length > 0) {
      return card.askedInstructions;
    }
  }
  return askedOf(card);
}

// askLines is a round card's asks: one line per instruction, each with its own
// arrow, or the one line that says there was none.
function askLines(said: string[]): HTMLElement[] {
  return (said.length > 0 ? said : ['']).map((words) => {
    const ask = document.createElement('p');
    ask.className = 'gly-versions-ask';
    ask.textContent = words ? `→ ${words}` : '→ no instruction was attached';
    return ask;
  });
}

// answerOn is the agent's own sentence about the round, and it is the only
// answer there is until a manifest names one per change. It is NOT a stand-in
// for a per-change note: it is round-level by construction, so it is shown once
// for the round rather than copied onto each card.
function answerOn(card: RoundView): string {
  return card.answers > 0 ? card.instruction || '' : '';
}

// foldSingleChange — pure data reconciliation, pulled out of paintChanges.
//
// ONE CHANGE IS NOT AN AMBIGUITY. Where a round produced exactly one change,
// everything the manifest did not attribute belongs to it — there is nowhere
// else in the round it could have gone — so the ask and the agent's sentence
// go ON the card that is beside the mark, which is the card that means
// something. Holding them back put a head with an EMPTY BODY next to the
// words that moved and repeated the landing's own card above it: two cards,
// neither of them the thing the reviewer wanted.
//
// This is not the guess the manifest exists to refuse. That guess is
// SPLITTING asks across several changes with nothing saying which went where,
// and it is still refused below: with more than one change and no testimony,
// the round card keeps them, because then there really is somewhere else
// they could have gone.
function foldSingleChange(
  placed: ChangeView[],
  loose: string[],
  answered: string,
): { loose: string[]; answered: string } {
  if (placed.length === 1 && (loose.length || answered)) {
    const only = placed[0];
    only.asks = (only.asks || []).concat(loose);
    if (!only.note && answered) {
      only.note = answered;
    }
    return { loose: [], answered: '' };
  }
  return { loose, answered };
}

// changeHead is a change card's first line. `k OF K` is computed HERE, at
// render, from the list the server just handed back — an ordinal renumbers, and
// CLAUDE.md's rule that an ordinal is not identity is exactly why nothing
// persists it. PLACE is the nearest preceding heading, lowercased server-side.
export function changeHead(k: number, total: number, place?: string): string {
  const head = `CHANGE ${k} OF ${total}`;
  return place ? `${head} · ${place}` : head;
}

// whereSaid is the sub-bar's left readout: which round is being read, and which
// two versions it sits between. It is the one thing on that row that cannot be
// mistaken for the draft.
export function whereSaid(ordinal: number, from: number, to: number): string {
  return `ROUND ${ordinal} · V${from} → V${to}`;
}

// arrivalSaid is what the bar says when a round ARRIVES — the one thing on this
// page the reviewer did not do themselves.
//
// IT NAMES THE DOOR the reviewer can now also read on screen. The first real
// review proved that an accessible name and tooltip do not make a bare glyph
// discoverable to a sighted reviewer; the control visibly says History now.
//
// AND IT IS SHORT BECAUSE THE CELL ELLIPSISES AT ITS END. The readout is the
// bar's one flexible box and it truncates on the right, so a clause put last is
// a clause that can be lost — the ORDER IS THE TRUNCATION POLICY, which this
// codebase already states about `paintReadout` composing three clauses and had
// to be told again about one. Measured in a real browser at 1440px on a
// 16-character filename: `v3 · the agent revised the document — rounds shows
// what changed` rendered as `v3 · the agent revised the docum…`, so the half
// that says WHERE TO READ IT — the only actionable half — never reached the
// reviewer at all.
//
// THE EXCEPTION SPENDS ITS ROOM ON THE REASON, which is what the reviewer needs
// to write the next instruction from.
export function arrivalSaid(round: Arrival | null): string {
  if (!round || !round.n) {
    return '';
  }
  if (round.exception) {
    return `v${round.n} · could not${round.why ? `: ${round.why}` : ''}`;
  }
  return `v${round.n} · agent revised · see History`;
}

// arrivedSaid is the ARRIVAL STRIP's sentence — board 1e, verbatim:
// `round 3 answered · v4 · 3 changes`.
//
// IT CARRIES BOTH NUMBERS BECAUSE THEY ARE TWO DIFFERENT FACTS AND THE
// REVIEWER USES BOTH. The ROUND is the exchange they counted in their head —
// their third ask, answered — and it is an ORDINAL among the rounds that are
// work, computed on every paint by `ordinalOf` and never stored (an ordinal
// renumbers, which this repository has paid for more than once). The VERSION is
// what the record and every card in it is keyed by, so `v4` is the word that
// makes the strip and History agree about which cut is being talked about.
//
// THE COUNT IS THE SERVER'S, through `changedSaid`, and never a count of marks
// this page managed to draw. The strip is the FIRST thing that says how much
// moved, before anything has been rendered to count.
//
// AN EXCEPTION DOES NOT SAY `answered`, and it does not say a count either.
// `galley cannot` cuts a round with the document unchanged — that is the fact
// being recorded — so a strip reading `round 3 answered · v4 · 0 changes` would
// be three-quarters of a lie told in the reviewer's own vocabulary. It says
// what happened instead, in the same slot, which is `roundFoot`'s own answer to
// the same question one surface over.
export function arrivedSaid(rounds: RoundView[], n: number): string {
  const round = (rounds || []).find((r) => r.n === n);
  if (!round) {
    return '';
  }
  const ordinal = ordinalOf(rounds, n) || workRounds(rounds).length;
  if (round.reason === COULD_NOT) {
    return `round ${ordinal} · the agent could not · v${n}`;
  }
  return `round ${ordinal} answered · v${n} · ${changedSaid(round.changed)}`;
}

// workRounds is every round that is WORK — the file as galley opened it is not
// a round anybody had, and it renders as the dashed foot card of the landing
// instead of as an entry in the list.
export function workRounds(
  rounds: RoundView[] | null | undefined,
): RoundView[] {
  return (rounds || []).filter((r) => !(r.n === 1 && r.reason === 'opened'));
}

// roundCards is WHAT A REVIEWER MEANS BY "A ROUND", which is not what the store
// means by one.
//
// The store records every version, and one exchange is TWO of them: the cut
// taken when Revise was pressed (the ask, and whatever the reviewer had typed
// into their own draft) and the cut taken when the agent returned (the answer,
// and the changes). Both carry the same instruction — the answering round
// resolves it through `answers`, which is the whole point of that field — so a
// card per version rendered the same ask twice a few pixels apart, once over
// `no changes` and once over the real count. Measured on a two-exchange fixture:
// five cards for two asks, three of them saying `no changes`.
//
// A ROUND IS THE ANSWERED CUT WHERE THERE IS ONE. An asking cut that some later
// round answers is folded into that round's card; an asking cut nobody has
// answered yet keeps its own, because a question outstanding is exactly the
// thing the reviewer opened this to see. NOTHING IS DROPPED — every ask still
// has a card, which is the property, not "each filter looks right".
//
// This is GROUPING FOR DISPLAY and it reads the server's own `answers`; it is
// not a second opinion about what a round is. The count on the card, the
// version it names and the asks it carries all still come off the record.
export function roundCards(
  rounds: RoundView[] | null | undefined,
): RoundView[] {
  const work = workRounds(rounds);
  const folded = new Set<number>();
  for (const r of work) {
    if (r.answers > 0) {
      folded.add(r.answers);
    }
  }
  return work.filter((r) => !folded.has(r.n));
}

// ordinalOf is a round's position among the CARDS, 1-based, or 0 for a round
// with no card of its own. An asking cut folded into the round that answered it
// takes that round's ordinal, because they are one exchange and the reviewer
// counts exchanges. Computed on every paint from the list the server delivered,
// never stored — see changeHead.
export function ordinalOf(rounds: RoundView[], n: number): number {
  const cards = roundCards(rounds);
  for (let i = 0; i < cards.length; i += 1) {
    if (cards[i].n === n || cards[i].answers === n) {
      return i + 1;
    }
  }
  return 0;
}

export interface VersionsPanelOptions {
  // TASK 8 CORRECTION: was `Promise<T>`. entry.ts constructs VersionsPanel
  // with net.ts's own `getJSON`, which returns `Promise<T | null>` for any
  // T (a failed fetch resolves null, not a rejection) — net.ts's own header
  // already named this exact mismatch as "invisible only because entry.ts
  // is not yet typechecked (Task 8)". Both real callers below (refresh,
  // the diff loader) already fold `| null` into the T they request, so this
  // widening changes nothing they do; it only makes the type honest about
  // what net.ts's getJSON can actually return.
  getJSON: <T>(path: string) => Promise<T | null>;
  docName?: string;
  onOpen?: () => void;
  onClose?: () => void;
  onCount?: (n: number) => void;
  onRestored?: (n: number, message: string) => void;
  onStage?: (panel: VersionsPanel) => void;
}

export class VersionsPanel {
  getJSON: <T>(path: string) => Promise<T | null>;
  docName: string;
  open: boolean;
  view: string;
  stage: string;
  rounds: RoundView[];
  selected: number;
  chosen: boolean;
  changes: ChangeView[];
  regions: number;
  refused: boolean;
  from: number;
  to: number;
  picked: number | null;
  hovered: number | null;

  // THE DOM HANDLES, ASSIGNED HERE FROM DIRECTLY WITHIN THE CONSTRUCTOR.
  //
  // What was `build()` — called once, from this constructor, and from
  // nowhere else — is inlined into it below rather than kept as a separate
  // method. `strictPropertyInitialization` only traces assignments made
  // directly in the constructor's own body; a field assigned by a method the
  // constructor calls is, to the typechecker, still possibly never assigned,
  // and the honest fix is not a definite-assignment assertion (forbidden by
  // this conversion's own rules) but putting the assignment where the
  // checker can see it. Nothing about WHEN these run changed: the statements
  // below execute in the same order, synchronously, that `build()` used to.
  root: HTMLElement;
  sub: HTMLElement;
  body: HTMLElement;
  paper: HTMLElement;
  rail: HTMLElement;
  where: HTMLElement;
  restoreButton: HTMLButtonElement;
  restoreIdle: HTMLElement;
  restoreArmed: HTMLElement;
  restoreBusy: HTMLElement;
  // restoring is the button's third state, held here rather than read back off
  // the DOM for the reason CLAUDE.md gives for the armed-delete flag: transient
  // control state lives on the object, so a repaint re-derives it instead of
  // finding it half-written on an element.
  restoring: boolean;
  viewButtons: Map<string, HTMLButtonElement>;
  cards: HTMLElement | null;
  onOpen: () => void;
  onClose: () => void;
  onCount: (n: number) => void;
  onRestored: (n: number, message: string) => void;
  onStage: (panel: VersionsPanel) => void;
  restoreArmedAt: number;
  scheduleStack: () => void;
  growth: ReturnType<typeof growthWatch>;

  constructor({
    getJSON,
    docName,
    onOpen,
    onClose,
    onCount,
    onRestored,
    onStage,
  }: VersionsPanelOptions) {
    this.getJSON = getJSON;
    this.docName = docName || '';
    this.open = false;
    this.view = DEFAULT_VIEW;
    this.stage = STAGE_ROUNDS;
    this.rounds = [];
    this.selected = 0;
    // THE CAUSE OF THE v1 LANDING BUG, NAMED. `selected` alone cannot tell
    // "nobody has chosen a round" from "the reviewer chose v1", and the App
    // refreshes this panel ONCE AT PAGE LOAD to paint the door's count — at
    // which point the only round on record is usually v1, so the old
    // "is the selection still a known round?" test pinned the selection to the
    // starting version for the life of the tab. Every later refresh found v1
    // still in the list, kept it, and History opened on `v1 · Starting version`
    // with `No earlier version to compare` over two rounds of real work.
    // Measured on merged dev: rounds ["2","1"], selected "1". The only thing
    // that ever moved it again was an arrival calling showRound.
    //
    // So the panel records WHETHER A ROUND WAS CHOSEN, and follows the newest
    // until one is. A choice is a click or an arrival, and nothing else.
    this.chosen = false;
    this.changes = [];
    this.regions = 0;
    this.refused = false;
    this.from = 0;
    this.to = 0;
    this.picked = null;
    this.hovered = null;
    this.cards = null;
    this.onOpen = onOpen || (() => {});
    this.onClose = onClose || (() => {});
    this.onCount = onCount || (() => {});
    this.onRestored = onRestored || (() => {});
    this.onStage = onStage || (() => {});
    this.restoreArmedAt = 0;
    this.restoring = false;
    // A CARD THAT GROWS AFTER IT WAS PLACED LEAVES EVERY CARD BELOW IT STALE.
    // The instruction rail's own guard (`App.cardSizes`), over this rail's
    // cards, because the hazard is the same one: the stack is exact arithmetic
    // over heights measured ONCE, and a card here grows without any repaint to
    // catch it — a long ask rewrapping as the window narrows, a web font landing
    // after first paint, the `is-picking` opacity pass touching a card mid-round.
    // It cannot feed itself: `stack` writes `top` on cards and a height on their
    // CONTAINER, which the observer does not watch, and a card is `left: 0;
    // right: 0` inside that container so its box depends on the container's
    // WIDTH and never on its height. Same exemption, stated the same way, as
    // paintAnchors' one size write.
    this.scheduleStack = coalesce(() => this.stack());
    this.growth = growthWatch(this.scheduleStack);

    // THE DOM HANDLES, BUILT HERE — WHAT USED TO BE build(), INLINED. See the
    // field block above for why: this constructor is the only caller, and the
    // typechecker only traces a field's assignment through the constructor's
    // own body, not through a method it calls.
    const root = document.createElement('section');
    root.className = 'gly-versions';
    root.hidden = true;
    // Labelled as a region rather than a dialog: nothing here is modal, focus
    // is not trapped, and calling it a dialog would promise a behaviour it
    // deliberately does not have.
    root.setAttribute('role', 'region');
    root.setAttribute('aria-label', 'document history');

    // --- the sub-bar: only the reading state has one ---
    const sub = document.createElement('div');
    sub.className = 'gly-versions-sub';
    sub.hidden = true;

    const where = document.createElement('span');
    where.className = 'gly-versions-where';

    const views = document.createElement('div');
    views.className = 'gly-versions-views';
    this.viewButtons = new Map();
    for (const v of VIEWS) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'gly-versions-view-pick';
      b.dataset.view = v.key;
      b.textContent = v.label;
      // pickView returns load()'s promise, which bottoms out in load()'s own
      // bare `.catch(() => {})` below — a listener may not itself return a
      // Promise (its rejection would have nowhere to go), and there is
      // nothing left to catch a second time here.
      b.addEventListener('click', () => {
        void this.pickView(v.key);
      });
      views.appendChild(b);
      this.viewButtons.set(v.key, b);
    }

    // DESTROY WEIGHT, AND NEVER AGAIN A PEER OF THE VIEW TOGGLES. Restoring
    // overwrites the reviewer's draft and is the one act on this surface with
    // no undo outside git; it shipped as a bordered pill in the same row-style
    // as `Changes` and `Side by side`, which made the loudest-consequence
    // control on the page look exactly like a way of LOOKING at something.
    // Borderless, muted, far right, and it arms — see restore().
    const restore = document.createElement('button');
    restore.type = 'button';
    restore.className = 'gly-versions-restore';
    // BOTH LABELS, IN ONE CELL, because this one arms. `restore vN as draft`
    // becomes `replace draft?` on its own click, and writing that straight into
    // textContent resized the control under the cursor between the arming press
    // and the confirming one — the second click landing somewhere the first was
    // not, on a button that overwrites the draft. `.gly-thread-delete` solved
    // this for the other destroy-weight verb and the copy never carried across;
    // `docs/design/2026-08-08-handoff-spec.md` §4 requires it of the WEIGHT, not
    // of that one button.
    const restoreIdle = document.createElement('span');
    const restoreArmed = document.createElement('span');
    restoreArmed.textContent = RESTORE_ARMED;
    // THREE FACES IN ONE CELL, not two and a `textContent` write. `restore()`
    // used to announce itself with `this.restoreButton.textContent =
    // 'restoring…'`, which REPLACES the button's children — so it destroyed
    // both spans above, and every later `paintRestore` wrote labels and
    // toggled classes on DETACHED nodes. A failed restore therefore left a
    // button reading "restoring…" for the life of the panel: enabled, since
    // paintRestore does clear `disabled`, and permanently mislabelled, with
    // the reserve that keeps it from moving on its own click gone with the
    // spans. Both failure branches called paintRestore and neither could
    // recover, because the thing they repaint was no longer in the document.
    const restoreBusy = document.createElement('span');
    restoreBusy.textContent = RESTORE_BUSY;
    restore.append(restoreIdle, restoreArmed, restoreBusy);
    // restore()'s own chain ends in a `.catch` that reports failure through
    // onRestored (didRestore -> say), so a listener wrapping it in `void`
    // loses nothing — the rejection already has somewhere to go before it
    // would reach here.
    restore.addEventListener('click', () => {
      void this.restore();
    });
    this.restoreButton = restore;
    this.restoreIdle = restoreIdle;
    this.restoreArmed = restoreArmed;
    this.restoreBusy = restoreBusy;
    this.where = where;

    sub.append(where, views, restore);

    const body = document.createElement('div');
    body.className = 'gly-versions-body';
    this.paper = document.createElement('article');
    this.paper.className = 'gly-versions-paper';
    this.rail = document.createElement('aside');
    this.rail.className = 'gly-versions-rail';
    this.rail.setAttribute('aria-label', 'rounds and changes');
    body.append(this.paper, this.rail);

    root.append(sub, body);
    // In body, never inside the editable node. See the header.
    document.body.appendChild(root);
    this.root = root;
    this.sub = sub;
    this.body = body;
    this.paintViews();
    // DELEGATED, because every card in this rail is destroyed and rebuilt on
    // every paint and a listener bound to one is a listener bound to a corpse.
    this.rail.addEventListener('mouseover', (e) => this.hoverFrom(e.target));
    this.rail.addEventListener('mouseout', (e) => {
      // Chrome fires mouseout with a null relatedTarget when the hovered
      // element is REMOVED, and a repaint removes every card — so a repaint
      // would otherwise put the dimming back one frame before the new card
      // arrives. Ask whether the element is still in the document.
      // e.target is an EventTarget to the DOM's own types and an Element to
      // this listener, which is delegated off the rail and so only ever hears
      // its own subtree — a real `mouseout` always carries an Element target.
      //
      // THIS SUPERSEDES A RECORDED DECISION, so both halves are kept. The
      // JSDoc version was deliberately NOT a guard, and said so: "a null
      // target here would throw exactly as it did before, and adding a
      // defence against a state nothing produces would be a behaviour change
      // smuggled in under a type annotation." That reasoning was right about
      // its own situation — a cast costs nothing and buys nothing, so letting
      // the throw stand was the honest choice.
      //
      // What changed is the rule, not the reasoning: this conversion forbids
      // assertions, so the shape has to be narrowed rather than asserted, and
      // a narrowing necessarily has a false branch where the cast had none.
      // The difference is confined to the state the original comment already
      // called one nothing produces: a non-Element target now falls through
      // to the same `hoverFrom(null)` the undimming path takes, where it used
      // to throw. On every state this listener can actually reach, the two
      // are identical.
      const gone = e.target;
      if (gone instanceof Element && gone.isConnected === false) return;
      this.hoverFrom(null);
    });

    // The stack is arithmetic over measured heights, so a width change makes
    // every card's top stale. Re-measured, never remembered — and coalesced,
    // because a drag-resize fires this faster than a layout read can answer.
    window.addEventListener('resize', () => this.scheduleStack());
  }

  show() {
    this.open = true;
    this.root.hidden = false;
    this.onOpen();
    return this.refresh();
  }

  hide() {
    this.open = false;
    this.root.hidden = true;
    this.unpin();
    // CLOSING ENDS THE READING, AND WITH IT THE CHOICE. `chosen` is a pin for
    // the session the reviewer is in — it exists so a refresh mid-read cannot
    // yank them off the round they are looking at — and a pin that outlived
    // the surface would be the v1-landing bug wearing a shorter fuse: come
    // back an hour and four rounds later and History would still open on
    // whatever was newest when you last closed it.
    this.chosen = false;
    this.onClose();
  }

  toggle() {
    return this.open ? (this.hide(), Promise.resolve()) : this.show();
  }

  // showRound is HOW A REVIEWER LANDS ON A ROUND THAT ARRIVED, and it is a
  // CHOICE — see `chosen`, and the bug it is named for.
  showRound(n: number, view?: string): Promise<void> {
    if (n) {
      this.selected = n;
      this.chosen = true;
    }
    this.stage = STAGE_READING;
    if (view) {
      this.view = view;
      this.paintViews();
    }
    return this.show();
  }

  // openRound is the landing's own click: read THIS round. It is the only
  // gesture that moves between the two stages in that direction, and it is a
  // navigation click, which is the one kind of click a full-surface swap is
  // allowed to follow.
  openRound(n: number): Promise<void> {
    this.selected = n;
    this.chosen = true;
    this.stage = STAGE_READING;
    this.view = DEFAULT_VIEW;
    this.restoreArmedAt = 0;
    this.unpin();
    this.paintViews();
    this.paintSub();
    this.onStage(this);
    return this.load();
  }

  // allRounds is `‹ all rounds` — back to the landing, keeping which round was
  // being read so the card the reviewer came from is still the obvious one.
  allRounds(): Promise<void> {
    this.stage = STAGE_ROUNDS;
    this.unpin();
    this.paintSub();
    this.onStage(this);
    return this.load();
  }

  pickView(key: string): Promise<void> {
    this.view = key;
    this.paintViews();
    this.unpin();
    return this.load();
  }

  paintViews(): void {
    for (const [key, b] of this.viewButtons) {
      const on = key === this.view;
      b.classList.toggle('is-on', on);
      b.setAttribute('aria-pressed', on ? 'true' : 'false');
    }
  }

  // refresh reads the history and, unless the reviewer has CHOSEN a round,
  // lands on the newest — the reviewer opening this wants to know what the last
  // one did, and making them pick first would be a surface asking a question it
  // can answer. See `chosen` for why "is the selection a known round?" was not
  // that test.
  refresh(): Promise<void> {
    // THE GENERATED ENVELOPE, not an inline restatement of it. This read was
    // typed `{ doc?: string; rounds?: RoundView[] }` — the two field names in
    // the history payload that nothing held either side to, while every type
    // NAMED INSIDE it (RoundView, DiffView, ChangeView) was under contract. A
    // contract with a hole in the envelope is a contract about the letters and
    // not the address. `versionsView` is in wireRoots now, so `doc` and
    // `rounds` cannot be renamed on the Go side without this failing to build.
    return this.getJSON<VersionsView | null>('/_galley/versions')
      .then((view) => {
        if (!view) {
          return;
        }
        this.rounds = view.rounds || [];
        this.onCount(this.rounds.length);
        if (view.doc) {
          this.docName = view.doc;
        }
        const known = this.rounds.some((r) => r.n === this.selected);
        if (!this.chosen || !known) {
          this.selected = this.rounds.length
            ? this.rounds[this.rounds.length - 1].n
            : 0;
        }
        this.paintSub();
        this.onStage(this);
        return this.load();
      })
      .catch(() => {});
  }

  selectedRound(): RoundView | null {
    return this.rounds.find((round) => round.n === this.selected) || null;
  }

  selectedOrdinal(): number {
    return ordinalOf(this.rounds, this.selected);
  }

  // --- loading ---
  //
  // ONE FETCH SHAPE FOR BOTH STAGES. The landing asks for the newest version
  // against NO earlier one (`from=0`), which is the request the server already
  // answers with the clean document — the same code path v1 takes, for the same
  // reason: there is nothing to compare, so the reading is the document. That
  // is why the landing needs no new endpoint and no new server work.
  load(): Promise<void> {
    if (!this.selected) {
      this.paper.textContent = '';
      this.changes = [];
      this.refused = false;
      this.paintRail();
      return Promise.resolve();
    }
    const reading = this.stage === STAGE_READING;
    const path = reading
      ? `/_galley/versions/view?to=${this.selected}&view=${this.view}`
      : `/_galley/versions/view?to=${this.selected}&from=0&view=inplace`;
    return this.getJSON<DiffView | null>(path)
      .then((got) => {
        if (!got) {
          return;
        }
        // innerHTML, and the server is what makes it safe: internal/diff
        // escapes every character of the document before it draws anything,
        // and there is a Go check that says so. The alternative — parsing the
        // rendered fragment here — would be a second renderer, which is the
        // one thing this file exists not to be.
        this.paper.innerHTML = got.html;
        this.paper.dataset.from = String(got.from);
        this.paper.dataset.to = String(got.to);
        this.paper.dataset.regions = String(got.regions || 0);
        this.from = got.from || 0;
        this.to = got.to || 0;
        this.regions = got.regions || 0;
        this.refused = !!got.refused;
        this.changes = reading ? got.changes || [] : [];
        this.nameSides();
        this.paintSub();
        this.paintRail();
        this.stack();
        this.watchCards();
      })
      .catch(() => {});
  }

  // nameSides puts the VERSION NUMBERS on the side-by-side heads.
  //
  // The renderer writes `before` and `after` because it is handed two documents
  // and told nothing about where they came from — it does not know a round
  // number and should not. The panel does, and this is a LABEL rather than a
  // second reading of the diff: nothing about the marks, the columns or their
  // contents is touched. `v3 · before` beside `v4 · after` is the difference
  // between a reader who knows which half is the draft and one who is guessing.
  nameSides(): void {
    const heads = this.paper.querySelectorAll('.gly-sbs-head');
    if (heads.length !== 2) {
      return;
    }
    heads[0].textContent = `v${this.from} · before`;
    heads[1].textContent = `v${this.to} · after`;
  }

  // --- the sub-bar ---

  paintSub(): void {
    const reading = this.stage === STAGE_READING;
    this.sub.hidden = !reading;
    this.root.classList.toggle('is-reading', reading);
    if (!reading) {
      return;
    }
    const round = this.selectedRound();
    if (!round) {
      return;
    }
    // IDENTICAL SIDES SAY SO, and they say it in the readout rather than by
    // leaving the paper blank. A round can genuinely move nothing — the agent
    // reported it could not, or the reviewer's press cut a version off a draft
    // they had not touched — and an empty diff with no sentence over it reads
    // as a surface that failed to load.
    this.where.textContent =
      this.regions === 0 && this.from > 0
        ? IDENTICAL_SAID
        : whereSaid(this.selectedOrdinal(), this.from || round.n - 1, round.n);
    const first = round.n === 1;
    for (const [key, button] of this.viewButtons) {
      button.disabled = first && key === 'sbs';
    }
    if (first && this.view === 'sbs') {
      this.view = 'inplace';
      this.paintViews();
    }
    this.paintRestore();
  }

  paintRestore(): void {
    const round = this.selectedRound();
    if (!round) {
      this.restoreButton.disabled = true;
      return;
    }
    this.restoreButton.disabled = this.restoring;
    const armed =
      !this.restoring && Date.now() - this.restoreArmedAt < RESTORE_ARM_MS;
    // The idle label carries the version, so it is written every paint; the
    // armed one is fixed. Whichever is not showing is `.gly-reserved` — laid
    // out and never drawn, so the cell stays as wide as the wider of the two
    // and the button cannot move on its own click.
    this.restoreIdle.textContent = `restore v${round.n} as draft`;
    this.restoreIdle.classList.toggle('gly-reserved', armed || this.restoring);
    this.restoreArmed.classList.toggle('gly-reserved', !armed);
    this.restoreBusy.classList.toggle('gly-reserved', !this.restoring);
    this.restoreButton.classList.toggle('is-armed', armed);
    this.restoreButton.title = armed
      ? 'press again to overwrite the current draft with this version'
      : 'copy this version over the current draft — the draft is not sent, and git still has it';
  }

  // --- the rail ---

  paintRail(): void {
    // The observer is holding cards this line is about to destroy — see
    // `unwatchCards`, and `paintRail` in entry.ts, which is the same rebuild
    // with the same hazard. `this.cards` goes with them: it is the container
    // `stack` and `paintPick` reach through, and a stale handle to a detached
    // node is a write nobody sees.
    this.unwatchCards();
    this.cards = null;
    this.rail.textContent = '';
    if (this.stage === STAGE_READING) {
      this.paintChanges();
    } else {
      this.paintRounds();
    }
  }

  paintRounds(): void {
    // `.gly-rail-head` — the same reserved slot the draft's whole-document
    // handle sits in, and the same one `‹ all rounds` sits in one stage over.
    // See the stylesheet: the region above the cards is the one region that has
    // to be stable, and it held three unlike controls at three heights.
    const title = document.createElement('div');
    title.className = 'gly-versions-rail-title gly-rail-head';
    title.textContent = ROUNDS_TITLE;
    this.rail.appendChild(title);

    const work = roundCards(this.rounds);
    if (!work.length) {
      const p = document.createElement('p');
      p.className = 'gly-versions-empty';
      p.textContent = EMPTY_SAID;
      this.rail.appendChild(p);
      return;
    }
    const now = Date.now();
    // Newest first. The list is insurance and the thing most likely to be
    // wanted is the thing that just happened.
    for (let i = work.length - 1; i >= 0; i -= 1) {
      const round = work[i];
      // A CARD, THROUGH THE ONE PRIMITIVE. This was a `<button>` hand-rolling
      // the card language from scratch — its own background, border, radius,
      // padding, cursor, type and colour, every one of them a second spelling of
      // `.gly-card`'s, and every one there only to undo a button's user-agent
      // appearance. It never even carried `.gly-card`, so on the landing stage
      // this rail spoke a language the rest of the product does not. `revealOn`
      // is what keeps the keyboard: Enter and Space open the round exactly as a
      // click does, which is what a `<button>` was buying.
      const { el, head } = cardShell('gly-versions-round');
      el.dataset.round = String(round.n);
      el.dataset.reason = round.reason || '';
      // APART BY SHAPE AS WELL AS BY WORD. A round where nothing happened has
      // to read differently from one where something did, in a list where every
      // other entry is a diff.
      el.classList.toggle('gly-versions-cannot', round.reason === COULD_NOT);
      head.textContent = roundHead(i + 1, round, now);

      const body = cardBody(el);
      body.append(...askLines(askOn(this.rounds, round)));

      const answered = answerOn(round);
      if (answered) {
        const answer = document.createElement('p');
        answer.className = 'gly-versions-answer';
        answer.textContent = `← ${answered}`;
        body.appendChild(answer);
      }

      const foot = document.createElement('span');
      foot.className = 'gly-versions-foot';
      foot.textContent = roundFoot(round);
      if (!round.answers) {
        // AN ASK NOBODY HAS ANSWERED YET IS THE ONE THE REVIEWER MOST WANTS TO
        // SEE, and it has no version of the agent's to name — the cut it points
        // at is the reviewer's own draft. So the foot says where it IS rather
        // than counting a document nobody has moved.
        foot.textContent =
          round.reason === COULD_NOT
            ? roundFoot(round)
            : `v${round.n} · with the agent`;
      }
      body.appendChild(foot);

      // openRound() returns load()'s promise, whose own bare
      // `.catch(() => {})` already absorbs a fetch failure — revealOn's `act`
      // is typed `() => void`, so the wrapper both satisfies that and marks
      // the intent rather than leaving the return value unmentioned.
      revealOn(
        el,
        () => {
          void this.openRound(round.n);
        },
        'read this round',
      );
      this.rail.appendChild(el);
    }

    // THE FOOT CARD IS THE FILE AS IT ARRIVED, and it is dashed because it is
    // not work anybody did. It is not clickable: there is no earlier version to
    // read it against, so a click would open a reading state with nothing in
    // it — which is the state this whole phase exists to stop History opening
    // on by itself.
    // Through the same primitive as everything else in this column, and for the
    // reason the round card is: it carried `.gly-card-head` and a
    // `.gly-versions-ask` without ever being a `.gly-card`, so it wore the card
    // vocabulary while re-declaring the card's own border, radius, padding and
    // type in a rule of its own. It gets NO `revealOn` — there is nothing to
    // reveal, which is exactly what the dashed edge says.
    const { el: seed, head: seedHead } = cardShell('gly-versions-seed');
    seedHead.textContent = SEED_HEAD;
    const seedSaid = document.createElement('p');
    seedSaid.className = 'gly-versions-ask';
    seedSaid.textContent = SEED_SAID;
    cardBody(seed).appendChild(seedSaid);
    this.rail.appendChild(seed);
  }

  paintChanges(): void {
    const back = document.createElement('button');
    back.type = 'button';
    back.className = 'gly-versions-all gly-rail-head';
    back.textContent = ALL_ROUNDS;
    // allRounds() returns load()'s promise, whose own bare `.catch(() => {})`
    // already absorbs a fetch failure — same shape as pickView above.
    back.addEventListener('click', () => {
      void this.allRounds();
    });
    this.rail.appendChild(back);

    // THE ROUND CARD, AND IT CARRIES THE SAME PAIRING THE LANDING ALREADY DOES.
    //
    // What is known at ROUND level is an ask and the agent's sentence back, and
    // the landing card has shown both, together, from the day it was built.
    // This surface had the identical two facts and took them apart: the answer
    // was bare text at the top of the rail, the ask was a card stranded at the
    // foot beside whatever change happened to be last, and between them sat a
    // CHANGE card with an empty body. Court, looking at the two side by side:
    // *"if we have the info here we should have it here."*
    //
    // This is NOT the attribution guess the manifest exists to avoid. Pairing a
    // change with an ask needs testimony; pairing the ROUND with its own ask and
    // its own answer needs nothing — they are two fields of one exchange, and
    // `roundCards` folds them for the landing on exactly that reasoning. An ask
    // the manifest DID attribute is not repeated here: it is on its change card,
    // where something actually claimed it.
    // A SELECTION CAN NAME NO ROUND — a repaint can land between a click and
    // the refresh that fetches the list — and both readers below answer
    // correctly for an absent one (`answerOn` on no `answers`, `roundHead` on
    // no `at`, which `ageSaid` reads as no age). NO_ROUND is a real, fully
    // populated RoundView rather than a cast over `{}`: every field the
    // fallback needs to read reads its own zero value, which every one of
    // those readers already treats the same way it treated `undefined` — and
    // it states that without weakening the type at the many sites where
    // RoundView's fields are exactly right. A rename on the Go side still
    // fails here: `round.at` is only a field because the Go struct says it is.
    const round = roundNamed(this.rounds, this.selected) || NO_ROUND;
    const placed = this.changes.filter((c) => c.region >= 0);
    const unplaced = this.changes.filter((c) => c.region < 0);
    const folded = foldSingleChange(
      placed,
      unplaced.flatMap((c) => c.asks || []),
      answerOn(round),
    );
    const answered = folded.answered;
    const loose = folded.loose;

    if (answered || loose.length) {
      const { el: card, head } = cardShell('gly-versions-round-card');
      card.classList.toggle('gly-versions-unanswered', !!this.refused);
      head.textContent = this.refused
        ? UNANSWERED_HEAD
        : roundHead(ordinalOf(this.rounds, this.selected), round);
      const body = cardBody(card);
      for (const ask of loose) {
        const line = document.createElement('p');
        line.className = 'gly-versions-ask';
        line.textContent = `\u2192 ${ask}`;
        body.appendChild(line);
      }
      if (answered) {
        const line = document.createElement('p');
        line.className = 'gly-versions-answer';
        line.textContent = `\u2190 ${answered}`;
        body.appendChild(line);
      }
      // NO `revealOn`. This card is the exchange, not a change — it points at
      // no region and there is nothing on the paper for it to take the reviewer
      // to. A card that answers a press by doing nothing is worse than one that
      // does not offer the press.
      this.rail.appendChild(card);
    }

    const cards = document.createElement('div');
    cards.className = 'gly-versions-cards';
    this.rail.appendChild(cards);
    this.cards = cards;

    // The empty line is about the CHANGES, so it asks whether any were drawn —
    // not whether the join returned anything, which the round card above may
    // have consumed entirely on a round that moved nothing.
    if (!placed.length) {
      const p = document.createElement('p');
      p.className = 'gly-versions-empty';
      p.textContent = this.regions
        ? 'No change in this round is placed.'
        : IDENTICAL_SAID;
      cards.appendChild(p);
      return;
    }
    // `k OF K` counts the changes that HAVE a place on the paper. An ask nobody
    // answered is in this list too — it must be, or something the reviewer sent
    // would disappear — but it is not the k'th change, because it is not a
    // change at all.
    let k = 0;
    for (const change of placed) {
      const { el: card, head } = cardShell('gly-versions-change');
      const body = cardBody(card);

      // THE ASK-ONLY BRANCH IS DELETED, AND IT COULD NEVER HAVE RUN. `placed` is
      // `this.changes.filter((c) => c.region >= 0)` twenty lines up, so the
      // `else` this loop carried — the unplaced ask rendered as a card of its
      // own, under ASK or NOT ANSWERED — tested a condition the filter had
      // already made true for every element. It was a whole paragraph of
      // reasoning about a state nothing in this function can reach, which is the
      // check-that-cannot-fail shape one level down: a reader maintains it as
      // though changing it would change the screen. The population it was
      // written for is real and is handled — `unplaced` is collected beside
      // `placed` and its asks go onto the ROUND card above (or, where the round
      // produced exactly one change, onto that change), which is where the
      // reasoning it carried now lives.
      k += 1;
      card.dataset.region = String(change.region);
      head.textContent = changeHead(k, placed.length, change.place);

      for (const ask of change.asks || []) {
        const line = document.createElement('p');
        line.className = 'gly-versions-ask';
        line.textContent = `→ ${ask}`;
        body.appendChild(line);
      }
      // A CHANGE THE MANIFEST DID NOT ATTRIBUTE RENDERS WITH NO ASK LINE — the
      // server dropped the join rather than guessing, and guessing here would
      // reintroduce exactly what it refused one layer down.

      // AN ABSENT NOTE IS ABSENT, AND THIS USED TO QUOTE THE PAPER INSTEAD.
      // The change is already marked in the prose a few inches to the left, so
      // a body repeating it said the same thing twice and made silence look
      // like an answer. The card still carries its head, its place and its
      // click — which is what makes it navigable — and the sentence appears
      // when the agent actually wrote one.
      if (change.note) {
        const note = document.createElement('p');
        note.className = 'gly-versions-note';
        note.textContent = `← ${change.note}`;
        body.appendChild(note);
      }

      // THE SAME REVEAL EVERY OTHER CARD IN THIS PRODUCT HAS. These cards used
      // to carry a bare `click` listener and nothing else — no `tabIndex`, no
      // Enter, no Space — so every change in a round was unreachable without a
      // pointer, on the one surface whose entire navigation is *the cards are
      // the navigation*.
      revealOn(card, () => this.pick(change.region));
      cards.appendChild(card);
    }
  }

  // THERE IS NO `quoteOf` HERE ANY MORE, AND ITS DELETION IS THE ENTRY. It read
  // the text of the element carrying a region's id and stood in for a note the
  // agent had not written. That is the paper's own words, repeated three inches
  // to the right of the paper — and worse, it filled a silence with something
  // that looked like an answer. A card with no note now has no note line.

  // --- alignment ---
  //
  // A CARD SITS BESIDE ITS MARK, and where two cards want one place the lower
  // one gives way. THIS IS `paintAnchors`, ONE SURFACE OVER, AND IT USED TO BE
  // A SECOND IMPLEMENTATION OF IT. `align()` is deleted; what it did wrong is
  // worth recording, because every item was a real difference in behaviour and
  // none of them was a decision anybody took:
  //
  //   IT DID NOT SORT. `stackCards` sorts by `anchorTop` and says why; this
  //   walked the cards in the order the server listed them and pushed each one
  //   below the last. That is only the same answer while the change list is
  //   MONOTONIC in page position — and a moved sentence, a table cell, or any
  //   region whose mark measures above its predecessor's breaks it: the first
  //   card takes a floor the second can never climb back above, so the second
  //   is drawn arbitrarily far below the words it is about, permanently.
  //
  //   AND THAT ORDER IS NOT HYPOTHETICAL — internal/diff says so itself, in
  //   renderSideBySide's own header: a move earns its ordinal at its DELETION
  //   (RegionOps skips the inserted half, because the sentence moved once), and
  //   `renderFlow` stamps BOTH ends with that one ordinal, so the element this
  //   lookup resolves — querySelector's FIRST match in document order — is the
  //   ARRIVAL end wherever a sentence moved earlier in the file. The ordinal
  //   therefore comes from where the sentence WAS and the mark from where it
  //   IS, and nothing makes those two orders agree. That file names this file's
  //   `align()` and `pick()` by name as the readers of it. NOTE, HONESTLY: no
  //   fixture in the tree exhibits it, so no check here has been shown red on
  //   it — the sort is right on the argument, and a check written over a state
  //   nothing reaches would be one of the six this repository already records.
  //
  //   IT INTERLEAVED READS AND WRITES. `card.style.top = …` invalidates layout
  //   and the next iteration's `getBoundingClientRect` forces it back — one
  //   reflow per card, in a loop, on every render and every resize frame.
  //   `paintAnchors` measures everything, then places everything, and states
  //   that as its reason.
  //
  //   IT WROTE A ROUNDED TOP AND CARRIED THE UNROUNDED ONE FORWARD, so what was
  //   on screen and what the next card was placed against disagreed by up to
  //   half a pixel each time, compounding down the column. Nothing is rounded
  //   now: the arithmetic is exact and the browser lays out sub-pixel anyway.
  //
  //   IT WROTE THE HEIGHT UNCONDITIONALLY, with a trailing gap included, on
  //   every call — dirtying layout to say what it already said. `setCardsHeight`
  //   compares first, the way `setBandHeight` does.
  //
  //   IT READ THE BREAKPOINT BACK OUT OF THE STYLESHEET. `getComputedStyle` on
  //   the container, to ask whether the media query had taken it out of
  //   `relative`, was a seventh spelling of 992 AND a forced style recalc. The
  //   question "is the rail beside the paper" has one answer in this codebase
  //   and it is `RAIL_MIN_WIDTH`, which is what `railSurfaces` asks.
  //
  //   AND IT RAN AT RENDER AND ON RESIZE AND NOWHERE ELSE. CLAUDE.md: *"A
  //   CARD'S HEIGHT IS NOT A CONSTANT, so nothing may be stacked against a
  //   measurement and then left there."* A card here grows for the same reasons
  //   one in the draft does — a long ask wrapping when the window narrows, a
  //   web font arriving after first paint — and every card below a grown one
  //   was left stale with nothing to catch it. `cardSizes` is the guard that
  //   entry states as the precondition for this shape being safe at all, and it
  //   is safe from feeding itself for the same stated reason: this pass writes
  //   POSITION, never a card's size.
  //
  // A hover or a click may still change a class and never a box — this rail's
  // oldest rule, unchanged.
  stack(): void {
    if (!this.open || this.stage !== STAGE_READING || !this.cards) {
      return;
    }
    const cards = [...this.cards.children].filter(
      (el): el is HTMLElement =>
        el instanceof HTMLElement && el.classList.contains('gly-card'),
    );
    if (!cards.length) {
      setStackHeight(this.cards, null);
      return;
    }
    // A narrow viewport puts the rail under the paper, where "beside its mark"
    // is not a place that exists. Flow, then, and no arithmetic — and the same
    // constant the draft rail decides its own existence by, so the two surfaces
    // cannot come to disagree about what a width means.
    if (window.innerWidth < RAIL_MIN_WIDTH) {
      for (const card of cards) card.style.top = '';
      setStackHeight(this.cards, null);
      return;
    }

    // READS FIRST, THEN WRITES — see above, and paintAnchors, which carries the
    // measurement. The page offset is read ONCE: every rect below is in viewport
    // coordinates and the stacker works in the page's, so taking it per card
    // would mix two frames of scroll into one layout if the page moved mid-pass.
    const scrollY = window.scrollY;
    const origin = this.cards.getBoundingClientRect().top + scrollY;
    const measured = cards.map((el) => {
      const region = el.dataset.region;
      const mark =
        region === undefined
          ? null
          : this.paper.querySelector(`[data-gly-region="${region}"]`);
      return {
        el,
        // getBoundingClientRect, not offsetHeight: offsetHeight is rounded to a
        // whole pixel, and a stack of cards each reported half a pixel short is
        // a stack that overlaps by the rounding.
        height: el.getBoundingClientRect().height,
        anchorTop: mark ? mark.getBoundingClientRect().top + scrollY : null,
      };
    });

    // The container's own top is the ceiling, in the same page coordinates as
    // every anchorTop — a card is `position: absolute` inside it, so what is
    // written is container-local. Same argument as the band's, one surface over.
    // AND THE PLACEMENT ITSELF IS THE DRAFT RAIL'S, NOT A SECOND ONE. See
    // placeCards: the sort, the gap, the adrift tail and the one compared size
    // write are all there, once, for both columns.
    placeCards(this.cards, measured, origin);
  }

  // The growth watch and the frame coalescer are `growthWatch`/`coalesce` in
  // web/card.ts, over this rail's own cards. They are named here so the two
  // rails read the same and so `paintRail` has something to call before it
  // destroys every card the observer is holding.
  watchCards(): void {
    if (this.cards) {
      this.growth.watch(this.cards.children);
    }
  }

  unwatchCards(): void {
    this.growth.unwatch();
  }

  // --- selection, which is DIMMING and reaches both columns ---
  //
  // NO RINGS AND NO STEPPERS. The cards are the navigation: the selected pair
  // stays full, every other card drops to 70% and every other mark on the paper
  // to 60%, so the answer to "which words is this card about" is the only thing
  // on the page still at full strength. A ring would be a fourth mark in a
  // vocabulary that already has three, and a prev/next stepper would be a
  // control for a gesture the cards already are.
  //
  // CLASSES ONLY. Nothing here writes a layout property — see stack().

  hoverFrom(target: EventTarget | null): void {
    if (this.picked !== null) {
      return;
    }
    // A delegated listener's target is an EventTarget to the DOM's own types;
    // this one only ever hears the rail's own subtree, so it narrows to an
    // HTMLElement — real `instanceof`, not the JSDoc cast this used to carry,
    // and null still passes through as null exactly as it did before.
    const el = target instanceof HTMLElement ? target : null;
    const found = el ? el.closest('.gly-versions-change') : null;
    const card = found instanceof HTMLElement ? found : null;
    const region =
      card && card.dataset.region !== undefined
        ? Number(card.dataset.region)
        : null;
    if (region === this.hovered) {
      return;
    }
    this.hovered = region;
    this.paintPick();
  }

  // pick is the reveal — THE SAME ONE, which it was not.
  //
  // It had its own three lines and every way they were shorter was a defect the
  // draft had already paid for and written down:
  //
  //   A HARDCODED `behavior: 'smooth'` ignores `prefers-reduced-motion`. A
  //   reviewer who has asked the operating system for less motion got an
  //   animated scroll from this surface and an instant one from every other.
  //
  //   NOTHING VERIFIED THE SCROLL. `smooth` is a REQUEST and Chrome with smooth
  //   scrolling off treats it as a silent no-op — the recorded failure is
  //   *"clicking a card rang the right mark four screens down and never moved
  //   the page"*. `scrollMarkIntoView` watches the frames and falls back to a
  //   jump on positive evidence that nothing is happening.
  //
  //   NOTHING RANG. Half a reveal: taken to the words and not shown which.
  //
  //   AND THE NULL CHECK CAME LAST. `paintPick` dimmed the whole paper to 60%
  //   and lifted a card to full — a state that says *these are the words* — and
  //   only then looked for the mark. With no mark that is the entire visible
  //   result: the surface answers the press by dimming itself and going nowhere.
  //   The lookup is first now, and a card with nothing to point at changes
  //   nothing at all.
  pick(region: number): void {
    if (region < 0) {
      return;
    }
    const mark = this.paper.querySelector(`[data-gly-region="${region}"]`);
    if (!mark) {
      return;
    }
    this.picked = region;
    this.hovered = region;
    this.paintPick();
    revealMark(mark);
  }

  // unpin answers Esc, and it reports whether it had anything to release — the
  // caller needs to know, because Esc's other meaning on this surface is "leave
  // History", and one key may not do two things in one press.
  unpin(): boolean {
    const had = this.picked !== null;
    this.picked = null;
    this.hovered = null;
    this.paintPick();
    return had;
  }

  paintPick(): void {
    const on = this.picked !== null ? this.picked : this.hovered;
    const picking = on !== null;
    this.rail.classList.toggle('is-picking', picking);
    this.paper.classList.toggle('is-picking', picking);
    if (this.cards) {
      // A real `instanceof` narrowing rather than the JSDoc cast this used
      // to carry — every child here is one of this file's own cards, built
      // by cardShell, so the guard never actually skips one.
      for (const card of this.cards.children) {
        if (!(card instanceof HTMLElement)) {
          continue;
        }
        card.classList.toggle(
          'is-picked',
          picking && Number(card.dataset.region) === on,
        );
      }
    }
    for (const mark of this.paper.querySelectorAll('[data-gly-region]')) {
      if (!(mark instanceof HTMLElement)) {
        continue;
      }
      mark.classList.toggle(
        'is-picked',
        picking && Number(mark.dataset.glyRegion) === on,
      );
    }
  }

  restore() {
    const round = this.selectedRound();
    if (!round || this.restoreButton.disabled) return;
    if (Date.now() - this.restoreArmedAt >= RESTORE_ARM_MS) {
      this.restoreArmedAt = Date.now();
      this.paintRestore();
      window.setTimeout(() => {
        // Re-derived from panel state and repainted UNCONDITIONALLY. The old
        // form guarded the repaint on `lapsed`, which is false by construction
        // at exactly the moment the timer runs — a disarm that never reached
        // the screen. See CLAUDE.md's entry on armed deletes.
        this.restoreArmedAt = 0;
        this.paintRestore();
      }, RESTORE_ARM_MS);
      return;
    }
    this.restoring = true;
    this.paintRestore();
    return fetch('/_galley/versions/restore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: round.n }),
    })
      .then(async (response) => {
        if (!response.ok) {
          const message =
            (await response.text()).trim() ||
            `restore failed: ${response.status}`;
          this.restoreArmedAt = 0;
          this.restoring = false;
          this.paintRestore();
          this.onRestored(round.n, message);
          return;
        }
        this.restoreArmedAt = 0;
        this.restoring = false;
        this.paintRestore();
        this.onRestored(round.n, '');
      })
      .catch((error) => {
        this.restoreArmedAt = 0;
        this.restoring = false;
        this.paintRestore();
        this.onRestored(round.n, String(error));
      });
  }
}
