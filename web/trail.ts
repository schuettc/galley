// trail.ts — the trail: the review remembers what the reviewer's hand did.
//
// The reviewer's edits apply directly (the reviewer's-hand cut) and nothing
// waits on them — but the review keeps the story (the 2026-08-15 trail spec).
// This plugin watches REVIEWER-originated transactions, records each text edit
// as a trail entry {old, new, blockKey, prefix/suffix, at}, and renders the
// record as DECORATIONS: a deletion leaves its ghost — the removed text struck
// in del-red at the spot it left — and an insertion glows in ins-teal. They
// are decorations in ProseMirror's sense, NEVER content (note.ts's doctrine:
// anything shown-but-not-content must be a decoration), so the projected .md
// stays clean text by construction.
//
// WHAT IS NOT RECORDED, each exclusion deliberate:
//
//   agent changes    a server mutation arrives as a whole-document rebuild
//                    through the ySync plugin, and every ySync-stamped
//                    transaction is skipped — the same discriminator
//                    keepPlace and the suggestion plugin already branch on.
//   structure        only TEXT steps are trailed: a ReplaceStep wholly inside
//                    one textblock whose slice is inline text. A join, a
//                    split, a wrap and a mark toggle apply untracked, exactly
//                    as the reviewer's-hand contract says formatting and
//                    structure do.
//   an undone edit   Cmd-Z RETRACTS the entry rather than recording a second
//                    inverse edit. y-prosemirror stamps undo/redo transactions
//                    (ySync meta's isUndoRedoOperation); each step is paired
//                    against an entry by position and text, an exact pair is
//                    removed, and an ambiguous one retracts nothing and
//                    records nothing — never double-record.
//
// RE-ANCHORING IS BEST-EFFORT AND HONEST. Live, entry ranges map through
// transactions; after a reload (or a rebuild, which is a reload the websocket
// delivers) an entry is re-found from blockKey + surrounding context, and one
// that cannot be placed UNAMBIGUOUSLY renders in the log only — adrift-style,
// never guessed onto the wrong text. Ambiguity refuses, as everywhere in
// galley. ONE ENTRY HAS NO TEXT TO BE FOUND BY and re-anchors structurally
// instead: a deletion that emptied its own block has no `new` and, since
// context is clamped to that block, no affixes either — its anchor is the
// empty block it left (emptyBlockAnchor). "An empty block" is not enough on
// its own, and that was a bug: it identifies a DOCUMENT as unambiguous and
// says nothing about the block being this entry's, so the entry carries the
// text of the blocks either side of its own (`before`/`after`) and a candidate
// has to match both. No evidence, or evidence that does not match, refuses.
//
// Everything below the plugin is pure arithmetic over documents and entry
// lists, exported so probe.mjs drives every rule with no browser.

import { Plugin, PluginKey } from '@tiptap/pm/state';
import { ReplaceStep } from '@tiptap/pm/transform';
import type { Step, Mappable } from '@tiptap/pm/transform';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import type { Node as PMNode, Slice, Mark } from '@tiptap/pm/model';
import { ySyncPluginKey } from 'y-prosemirror';

// isFence and isTable are the SUGGESTION PLUGIN'S predicates, imported rather
// than restated. They already answer "is this content literal to this editor",
// the transaction filter refuses every reviewer edit they say yes to, and this
// file needs the same answer for the same reason — a block the reviewer's hand
// cannot reach is a block the reviewer's hand cannot have emptied. Two
// predicates that agree for now is the shape of bug suggestions.ts already
// carries a scar from (see its codeRun comment); there is one rule.
import { isFence, isTable } from './suggestions.ts';

// TrailEntryBase is what an entry carries either way: a text edit and the
// evidence re-anchoring leans on. `before`/`after`/`proposal`/`placed` are
// optional because several construction sites in this file build an entry
// before that evidence exists yet — applyRecord's `merged` is filled in by the
// caller's own contextOf spread a line later, and a legacy sidecar row may
// carry none of them at all (see loadedEntry, serializeEntries). Every reader
// already treats a missing one exactly as it treats an explicit `null` — see
// `side`/`flag` below — so leaving them optional states the real contract
// rather than papering over it with a default.
interface TrailEntryBase {
  id: number;
  old: string;
  new: string;
  blockKey: string;
  prefix: string;
  suffix: string;
  before?: string | null;
  after?: string | null;
  proposal?: string | null;
  placed?: boolean | null;
  at: string;
  reachFrom?: number | null;
  reachTo?: number | null;
}

// AnchoredTrailEntry is an entry that has a place in the live document —
// `anchored: true` paired with numeric `from`/`to`, which every construction
// site in this file already keeps true as an invariant. Spelling it as a
// discriminated union rather than `anchored: boolean; from: number | null`
// lets `if (e.anchored)` narrow `from`/`to` for free, the way the plain JS
// already read — no extra runtime check is needed to say what the code
// already guaranteed.
export interface AnchoredTrailEntry extends TrailEntryBase {
  anchored: true;
  from: number;
  to: number;
}

// AdriftTrailEntry is an entry the ladder could not re-find: log-only, no
// ghost, no position.
export interface AdriftTrailEntry extends TrailEntryBase {
  anchored: false;
  from: null;
  to: null;
}

export type TrailEntry = AnchoredTrailEntry | AdriftTrailEntry;

// TrailAnchor is a found position — an entry's re-anchored `{from, to}`.
export interface TrailAnchor {
  from: number;
  to: number;
}

// TrailContext is what contextOf reads around a range: the affixes an entry
// re-finds its place by, and — for the one entry that has none — the
// evidence emptyBlockAnchor compares instead. See contextOf's own comment for
// what each field means and when the second pair is filled.
export interface TrailContext {
  prefix: string;
  suffix: string;
  before: string | null;
  after: string | null;
}

// BlockRef names one block by the content hash the browser cannot compute
// itself — the pending payload's own block list, read here and nowhere
// mutated. Loosely typed on purpose: this file reads only `key` and `index`,
// the same injection discipline suggestions.ts's SuggestionUI uses.
export interface BlockRef {
  key: string;
  index: number;
}

// TrailStepRecord is what recordOf reads off a single step: the text it
// changed, and — filled in by the caller a line later, from the same
// document the step applied to — the block it changed it in.
export interface TrailStepRecord {
  from: number;
  to: number;
  old: string;
  ins: string;
  proposal: string | null;
  blockKey?: string;
}

// EmptyBlockEvidence is the narrow slice of an entry that placeEmptyBlocks
// and emptyBlockAnchor ask about: the neighbour text a block-emptying
// deletion brings instead of its own, and whether the list may speak for its
// position. Every TrailEntry satisfies this structurally.
export interface EmptyBlockEvidence {
  before?: string | null;
  after?: string | null;
  placed?: boolean | null;
}

// ReanchorEntry is what reanchor needs: the needle it searches with, plus —
// for the one case that needle is empty — the evidence emptyBlockAnchor
// takes over with. Every TrailEntry satisfies this structurally too.
export interface ReanchorEntry {
  new: string;
  prefix: string;
  suffix: string;
  blockKey: string;
  before?: string | null;
  after?: string | null;
  placed?: boolean | null;
}

// SerializedTrailEntry is the wire shape POST /_galley/trail takes and the
// sidecar's own review.Change record — both serializeEntries' output and
// loadedEntry's input, since a sidecar row loaded back is read by the same
// shape it was written in.
export interface SerializedTrailEntry {
  old?: string;
  new?: string;
  blockKey?: string;
  prefix?: string;
  suffix?: string;
  before?: string | null;
  after?: string | null;
  proposal?: string | null;
  placed?: boolean | null;
  at?: string;
}

// TrailPluginState is trailPlugin's own state field: the entries and the
// DecorationSet rendered from them, plus a generation counter nothing here
// reads (kept for a caller that wants to know the state changed at all).
export interface TrailPluginState {
  entries: TrailEntry[];
  decos: DecorationSet;
  gen: number;
}

// TrailDiff is diffOf's answer: the minimal positional change between an
// undo's before and after documents.
export interface TrailDiff {
  from: number;
  to: number;
  removed: string;
  inserted: string;
}

// ReviewerBlock is one entry of reviewerBlocks' list — a textblock the
// reviewer's own hand could have emptied, and its position.
interface ReviewerBlock {
  node: PMNode;
  pos: number;
}

// Neighbours is the two-sided evidence a block-emptying entry brings: the
// text of the blocks either side, or null for "no block that side" — see
// neighboursAt's own comment for why null and '' must never be conflated.
interface Neighbours {
  before: string | null;
  after: string | null;
}

// EmptyCandidate is one empty block a reviewer's hand could have emptied,
// carrying the same neighbour evidence an entry is compared against.
interface EmptyCandidate extends Neighbours {
  pos: number;
}

export const trailPluginKey = new PluginKey<TrailPluginState>('galleyTrail');

// How much context travels with an entry, each side. Enough to discriminate
// an ordinary sentence; small enough that the sidecar stays a record and not
// a copy of the document.
export const TRAIL_CONTEXT_CHARS = 32;

// Entry identity for the widget decoration's `key` — a stable key is what
// stops ProseMirror rebuilding the ghost's DOM node on every apply. Session
// -local on purpose: an ordinal is not identity and this is never persisted.
let trailIds = 0;
const nextTrailId = () => {
  trailIds += 1;
  return trailIds;
};

// --- recording: what one step did -----------------------------------------

// textOfSlice reports a slice's inline text, or null when the slice carries
// anything that is not text — which is this module's word for "structure".
function textOfSlice(slice: Slice): string | null {
  if (slice.openStart !== 0 || slice.openEnd !== 0) {
    return null;
  }
  let text = '';
  let textOnly = true;
  slice.content.forEach((child) => {
    if (child.isText) {
      text += child.text;
    } else {
      textOnly = false;
    }
  });
  return textOnly ? text : null;
}

// The two marks an AGENT PROPOSAL wears. `highlight` is deliberately not one:
// a highlight is a comment's anchor on text that is already in the document,
// not a proposed change, and editing under one is editing prose — the
// conversation about it is settled by resolving the thread, never by typing.
// Spelled here rather than imported from MARK_KINDS because that table is
// keyed by what the SERVER calls each mark and this is a question about
// proposals; a fourth mark added there must not silently join this set.
const PROPOSAL_MARKS = new Set(['ins', 'del']);

// attrString narrows a mark attribute — typed `any` by prosemirror-model's own
// Attrs — to the string it is meant to be, or '' for anything else. The same
// discipline suggestions.ts's attrString uses, restated here rather than
// imported: it is a private narrowing helper in both files, not a shared rule.
function attrString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/**
 * onProposalAt reports WHOSE proposal a range of the document the reviewer is
 * about to change carries: the author on the mark, or null if there is no
 * proposal there at all.
 *
 * IT IS READ HERE OR IT IS NOT READ AT ALL, and that is why this travels all
 * the way to the ledger. Deleting text under a pending mark removes that
 * mark (CLAUDE.md's reviewer's-hand rule), so by the time the trail entry
 * reaches the server — one debounce later at best, and the record is written
 * on the SETTLE, one save after that — the evidence is gone. The document the
 * step applied TO still has it, and this is the only moment anything does.
 *
 * IT RETURNS THE AUTHOR RATHER THAN A BOOLEAN, because every other proposal
 * record in the ledger carries the author off the mark, and a boolean would
 * make the hand record the one that could not.
 * `galley suggest --author NAME` puts a non-agent proposal in the document, so
 * accepting such a span would file `approved`/that-author while rewriting it
 * filed `hand`/agent — the same invariant disagreeing with itself across two
 * verbs on one span. `''` is a mark with NO author, which is a real case (a
 * mark parsed straight out of a file, where CriticMarkup has nowhere to write
 * one); the default for that is applied on the Go side, so there is one rule for it and not a second one here that agrees for
 * now. Hence null-versus-'' rather than a truthiness test — the same
 * distinction review.Change's Before/After pointers already carry.
 *
 * A REPLACEMENT ASKS ABOUT WHAT IT REPLACED; a caret insertion asks about the
 * text it lands INSIDE. Typing at the edge of a proposal is not typing on it —
 * a keystroke immediately after an inserted word is prose of the reviewer's own
 * — and THE EDGES ARE THE SCHEMA'S JOB, ALREADY DONE. `ins`, `del` and
 * `highlight` are all `inclusive: false` (entry.ts's suggestionMark), and
 * ResolvedPos.marks() drops an `inclusive: false` mark at a text-node boundary
 * unless the node on the OTHER side carries it too. So `marks()` alone answers
 * both questions: at an outer edge one side has the mark and the other does
 * not, and it comes back absent.
 *
 * THERE USED TO BE A `textOffset > 0` GUARD HERE AND IT WAS A FALSE NEGATIVE.
 * It re-asked the question the schema had already answered, and it got a
 * DIFFERENT one wrong: a proposal split across text nodes by formatting inside
 * it — `{++a **bold** word++}`, one ins run over three text nodes, the normal
 * multi-inline shape CLAUDE.md's run entry describes — has interior boundaries
 * where textOffset is 0 and the mark is on BOTH sides. Measured on this schema:
 * positions 7 and 11 of that span reported `marks: ins` and answered false, so
 * a caret insertion strictly inside the agent's proposal was recorded `edited`
 * under the reviewer instead of `hand` under the agent, and the rewrite never
 * reached AgentFate.Rewritten. probe.mjs carries the split fixture, because the
 * single-text-node one cannot see a boundary that only exists when a proposal
 * is split.
 *
 * @param doc the document the step applied to
 * @returns the proposal's author ('' if the mark carries none),
 *   or null where there is no proposal
 */
function onProposalAt(doc: PMNode, from: number, to: number): string | null {
  const proposed = (marks: readonly Mark[] | null | undefined) =>
    (marks || []).find((m) => PROPOSAL_MARKS.has(m.type.name));
  const authorOf = (mark: Mark | null | undefined): string | null =>
    mark ? attrString(mark.attrs.author) : null;
  try {
    if (to > from) {
      let hit: Mark | null = null;
      doc.nodesBetween(from, to, (node) => {
        if (hit) {
          return false;
        }
        if (node.isText) {
          hit = proposed(node.marks) || null;
        }
        return !hit;
      });
      return authorOf(hit);
    }
    const $pos = doc.resolve(from);
    return authorOf(proposed($pos.marks()));
  } catch {
    return null;
  }
}

/**
 * recordOf reads one step against the document it applied to and reports the
 * text edit it made, or null for anything the trail does not record: a
 * non-Replace step (marks, attrs), a step that crosses a block boundary (a
 * join, a split), a slice carrying nodes, and a step that changed no text.
 *
 * `proposal` names the AUTHOR of the proposed span the edit landed on, and is
 * null when it landed on none — see onProposalAt, and review.Change on the Go
 * side for what it decides.
 *
 * @param doc the document the step applied to
 */
export function recordOf(doc: PMNode, step: Step): TrailStepRecord | null {
  if (!(step instanceof ReplaceStep)) {
    return null;
  }
  let $from;
  let $to;
  try {
    $from = doc.resolve(step.from);
    $to = doc.resolve(step.to);
  } catch {
    return null;
  }
  if (!$from.parent.isTextblock || !$from.sameParent($to)) {
    return null;
  }
  const ins = textOfSlice(step.slice);
  if (ins === null) {
    return null;
  }
  const old = doc.textBetween(step.from, step.to, '\n', '\n');
  if (old === ins) {
    return null;
  }
  return {
    from: step.from,
    to: step.to,
    old,
    ins,
    proposal: onProposalAt(doc, step.from, step.to),
  };
}

// --- canonical form: an entry is its MINIMAL diff ---------------------------

function isLowSurrogate(u: number): boolean {
  return u >= 0xdc00 && u <= 0xdfff;
}

/**
 * trimAffixes reduces an old/new pair to the one region where they differ:
 * everything before `start` is a shared prefix, everything after the returned
 * halves is a shared suffix. Surrogate-guarded at both boundaries, the same
 * arithmetic review.diffBounds uses — ProseMirror positions are UTF-16 code
 * units, so a boundary inside a pair would strike half an emoji.
 *
 * IT EXISTS BECAUSE THE UNDO'S DIFF IS MINIMAL. findDiffStart/findDiffEnd
 * hand back the smallest changed range, so an entry stored un-trimmed —
 * "brown"→"blue", sharing its 'b' — could never pair with the undo that
 * reverts it, and survived as a permanent adrift phantom the agent read as
 * decided fact. Canonicalizing at RECORD time puts entries and undo diffs in
 * one coordinate system, and sharpens the ghost and highlight to the true
 * change for free.
 */
export function trimAffixes(
  old: string,
  next: string,
): { start: number; old: string; next: string } {
  let start = 0;
  const most = Math.min(old.length, next.length);
  while (start < most && old.charCodeAt(start) === next.charCodeAt(start)) {
    start += 1;
  }
  if (
    start > 0 &&
    start < old.length &&
    isLowSurrogate(old.charCodeAt(start))
  ) {
    start -= 1;
  }
  let endOld = old.length;
  let endNext = next.length;
  while (
    endOld > start &&
    endNext > start &&
    old.charCodeAt(endOld - 1) === next.charCodeAt(endNext - 1)
  ) {
    endOld -= 1;
    endNext -= 1;
  }
  if (
    endOld < old.length &&
    endNext < next.length &&
    isLowSurrogate(old.charCodeAt(endOld))
  ) {
    endOld += 1;
    endNext += 1;
  }
  return {
    start,
    old: old.slice(start, endOld),
    next: next.slice(start, endNext),
  };
}

// --- display: an entry is stored minimal and SHOWN as a word ---------------

// A boundary is whitespace or punctuation — nothing else. Symbols and marks
// are not boundaries, so an emoji or a currency sign glued to a word travels
// with it rather than cutting it in half. The `u` flag is what makes
// `\p{P}` legal AND what makes the class safe to test a whole code point
// against.
const TRAIL_BOUNDARY = /[\s\p{P}]/u;

function isHighSurrogate(u: number): boolean {
  return u >= 0xd800 && u <= 0xdbff;
}

// wordTail is the run of word characters ENDING a string, wordHead the run
// BEGINNING one. Both step by CODE POINT, never by code unit: a walk that
// stopped between an emoji's halves would hand the display a lone surrogate —
// the UTF-16 lesson diffBounds and trimAffixes already carry, one layer out.
function wordTail(text: string): string {
  let i = text.length;
  while (i > 0) {
    let j = i - 1;
    if (
      isLowSurrogate(text.charCodeAt(j)) &&
      j > 0 &&
      isHighSurrogate(text.charCodeAt(j - 1))
    ) {
      j -= 1;
    }
    if (TRAIL_BOUNDARY.test(text.slice(j, i))) {
      break;
    }
    i = j;
  }
  return text.slice(i);
}

function wordHead(text: string): string {
  let i = 0;
  while (i < text.length) {
    let j = i + 1;
    if (
      isHighSurrogate(text.charCodeAt(i)) &&
      j < text.length &&
      isLowSurrogate(text.charCodeAt(j))
    ) {
      j += 1;
    }
    if (TRAIL_BOUNDARY.test(text.slice(i, j))) {
      break;
    }
    i = j;
  }
  return text.slice(0, i);
}

/**
 * expandToWord widens a stored entry to the WORD a human reads. DISPLAY ONLY —
 * nothing it returns is ever stored, posted or compared against an undo.
 *
 * STORAGE IS MINIMAL AND MUST STAY MINIMAL. trimAffixes explains why: the
 * undo's own diff is minimal, so an entry stored any wider can never pair with
 * the undo that reverts it and survives as a phantom the agent reads as
 * decided fact. But the minimal form is mathematically true and humanly alien
 * — "could" → "should" stores as {old:'c', new:'sh'}, because "ould" is a
 * shared suffix, and a log row reading `"c" → "sh"` says nothing about what
 * the reviewer's own hand did. So the two forms are separated by SURFACE
 * rather than reconciled: the entry stays minimal, and every place that SHOWS
 * one calls this.
 *
 * The rule is one walk each way from the stored region's edges, out to the
 * nearest whitespace-or-punctuation boundary, and the SAME two affixes glued
 * onto BOTH sides — which is what makes the three cases fall out rather than
 * needing to be special-cased:
 *
 *   an entry already at word boundaries expands to itself (both affixes empty)
 *   a pure insertion inside a word shows the whole word on both sides:
 *     {old:'', new:'o'} in "word" displays wrd → word
 *   an entry with no context at all — an adrift record whose place could not
 *     be re-found — passes through in its stored form, which is all the log
 *     can honestly say about it.
 *
 * `context` is {prefix, suffix}: the document text immediately around the
 * entry. The entry's OWN stored prefix/suffix are the fallback, so a log row
 * needs no second argument; a caller with a live document (the decorations)
 * passes the live text instead. Both are bounded — TRAIL_CONTEXT_CHARS a side,
 * clamped to the block — so a word longer than its context shows as much of
 * itself as the context holds. Best-effort, display only, never stored.
 *
 * @returns lead and tail are the affixes themselves, so a caller placing
 *   DECORATIONS can widen its positions by exactly what it widened the text by.
 */
export function expandToWord(
  entry:
    | { old?: string; new?: string; prefix?: string; suffix?: string }
    | null
    | undefined,
  context?: { prefix?: string; suffix?: string } | null,
): { old: string; new: string; lead: string; tail: string } {
  const e = entry || {};
  const ctx = context || e;
  const lead = wordTail((ctx && ctx.prefix) || '');
  const tail = wordHead((ctx && ctx.suffix) || '');
  return {
    old: lead + (e.old || '') + tail,
    new: lead + (e.new || '') + tail,
    lead,
    tail,
  };
}

// --- merging: a keystroke joins the edit it continues ----------------------

/**
 * proposalOf is the sticky rule in one place: the coalesced entry's proposal is
 * the FIRST one anything in it reached, and it only ever turns on.
 *
 * Spelled as a function rather than inline because "on" is no longer a boolean
 * — `''` is a proposal whose mark carries no author, so `||` and `!!` are both
 * wrong here, and a second site writing the test by hand would get exactly that
 * wrong.
 *
 * @param rec the step just recorded
 * @param touching the entries it merged with, in
 *   document order — the same order `id` and `at` are taken from
 */
function proposalOf(
  rec: { proposal?: string | null } | null | undefined,
  touching: { proposal?: string | null }[],
): string | null {
  const on = (v: string | null | undefined) => v !== null && v !== undefined;
  const held = touching.find((e) => on(e.proposal));
  if (held) {
    return held.proposal ?? null;
  }
  return on(rec && rec.proposal) ? ((rec && rec.proposal) ?? null) : null;
}

/**
 * reachFrom / reachTo are THE REGION AN ENTRY'S STORY COVERS, which is not the
 * same as the minimal diff stored in `from`/`to`.
 *
 * ONE TYPED PHRASE PRODUCED TWO ROWS AND `△ 2 changed`, AND THIS IS WHY.
 * Selecting `simpler` and typing `the simpler shape` is one gesture; every
 * keystroke merges into one entry, and at `the simpler` the stored form is
 * `trimAffixes('simpler', 'the simpler')` = `{old: '', new: 'the '}` — because
 * `simpler` is a shared SUFFIX. That is correct storage (see trimAffixes: an
 * entry stored any wider can never pair with the undo that reverts it), and it
 * moves the entry's END seven characters back from where the caret is. The next
 * keystroke lands outside `[from, to)`, does not touch the entry, and STARTS A
 * NEW ONE — so the trail read
 *
 *     simpler → the simpler        just now
 *     simpler → simpler shape      just now
 *
 * neither of which is what was typed, and both of which are one word displayed
 * twice: expandToWord widens each fragment to `simpler` from opposite sides.
 *
 * So the union the merge is computed over is kept, and the TOUCHING TEST asks
 * it. Storage stays minimal — nothing here changes what is stored, posted or
 * compared against an undo — and "one region, one story" becomes true of the
 * region the reviewer actually rewrote rather than of the sub-range the
 * trimming left behind.
 *
 * SESSION-ONLY, DELIBERATELY. `serializeEntries` names its eight fields and
 * this is not among them: the reach is the shape of a gesture in progress, and
 * a reloaded entry is not being typed into. An entry with no reach falls back
 * to its own stored region, which is what every pre-existing entry does and
 * what the trail did everywhere before this.
 */
const reachFrom = (e: { from: number; reachFrom?: number | null }): number => {
  const r = e.reachFrom;
  return typeof r === 'number' && Number.isFinite(r)
    ? Math.min(r, e.from)
    : e.from;
};
const reachTo = (e: { to: number; reachTo?: number | null }): number => {
  const r = e.reachTo;
  return typeof r === 'number' && Number.isFinite(r) ? Math.max(r, e.to) : e.to;
};

/**
 * applyRecord folds one recorded step into the entry list.
 *
 * ONE REGION, ONE STORY. Every anchored entry the step's range touches (mere
 * adjacency included — typing at an entry's edge continues it) is merged with
 * the step into a single entry covering the union: `old` is the ORIGINAL text
 * of that region (unrecorded document text stitched around each touched
 * entry's own old), `new` is the region's text after the step. Deleting typed
 * text therefore shrinks `new` rather than growing `old`, a deletion followed
 * by typing in its place reads as one substitution — and a region whose old
 * and new come out EQUAL records nothing at all, because the reviewer's hands
 * ended where they started: the trail is what the reviewer did, not the noise
 * of their fingers getting there.
 *
 * WHAT "TOUCHES" MEANS IS THE ENTRY'S REACH, NOT ITS STORED RANGE — see
 * reachFrom/reachTo above for the gesture that split into two rows without it.
 *
 * Returns the untouched entries (still in pre-step coordinates — the caller
 * maps them through the step) and the merged entry (already in post-step
 * coordinates: its region starts at or before the step, so its start is
 * unmoved, and its end is start plus the new text's length).
 *
 * THE MERGED ENTRY'S CONTEXT IS THE CALLER'S TO FILL, and it must. This works
 * from the document the step applied TO and cannot read the one the step
 * produced, so the affixes it hands back are empty — and empty affixes mean
 * something specific everywhere else (a deletion that emptied its own block;
 * see emptyBlockAnchor). The plugin reads contextOf against the post-step
 * document the moment it takes this entry, so "no context" never means
 * "not filled in yet".
 *
 * @param docBefore the doc the step applied to
 * @param at ISO instant for a fresh entry
 */
export function applyRecord(
  entries: TrailEntry[] | null | undefined,
  docBefore: PMNode,
  rec: TrailStepRecord,
  at: string,
): { keep: TrailEntry[]; merged: TrailEntry | null } {
  const keep: TrailEntry[] = [];
  const touching: AnchoredTrailEntry[] = [];
  for (const e of entries || []) {
    if (e.anchored && rec.from <= reachTo(e) && reachFrom(e) <= rec.to) {
      touching.push(e);
    } else {
      keep.push(e);
    }
  }
  touching.sort((a, b) => a.from - b.from);
  // The union is over the REACHES, so it only ever grows: a step inside the
  // region an entry rewrote is inside that entry's story wherever the trimming
  // put its stored range. The stitching below still walks each entry's own
  // `from`/`to`, because that is where its `old` belongs; everything between
  // is ordinary document text, read from the doc the step applied to.
  const lo = touching.length
    ? Math.min(rec.from, ...touching.map(reachFrom))
    : rec.from;
  const hi = touching.length
    ? Math.max(rec.to, ...touching.map(reachTo))
    : rec.to;
  const text = (a: number, b: number) =>
    docBefore.textBetween(a, b, '\n', '\n');
  let old = '';
  let cursor = lo;
  for (const e of touching) {
    old += text(cursor, e.from) + e.old;
    cursor = e.to;
  }
  old += text(cursor, hi);
  const next = text(lo, rec.from) + rec.ins + text(rec.to, hi);
  if (old === next) {
    return { keep, merged: null };
  }
  // CANONICAL: the stored entry is the minimal diff of its region — see
  // trimAffixes for why anything wider can never be retracted.
  const trimmed = trimAffixes(old, next);
  const carrier = touching.find((e) => e.blockKey) || rec;
  return {
    keep,
    merged: {
      id: touching.length ? touching[0].id : nextTrailId(),
      old: trimmed.old,
      new: trimmed.next,
      from: lo + trimmed.start,
      to: lo + trimmed.start + trimmed.next.length,
      // The whole region this entry's story covers, in POST-step coordinates:
      // the region starts at or before the step, so `lo` is unmoved, and it
      // ends at `lo` plus the region's new text. It contains [from, to) by
      // construction — trimming only ever takes from the ends.
      reachFrom: lo,
      reachTo: lo + next.length,
      at: touching.length ? touching[0].at : at,
      // STICKY, AND IT ONLY EVER TURNS ON. An entry that ever touched an
      // agent's proposed span rewrote one, and a later keystroke on plain
      // text beside it does not un-rewrite it — the coalesced entry is ONE
      // edit, and one edit that reached a proposal is the verdict by hand.
      // The Go side keys its settle signature on this too, so an entry that
      // grows onto a proposal takes one more save to settle rather than being
      // recorded under the earlier reading.
      //
      // THE EARLIEST PROPOSAL WINS, which is the same rule `id` and `at` above
      // already follow: a coalesced entry keeps the identity of the edit it
      // started as, and the proposal it first reached is the one it is a
      // verdict on. `null` is "no proposal" and `''` is "a proposal with no
      // author on its mark" — a real case (see onProposalAt) — so this tests
      // for null rather than for truthiness.
      proposal: proposalOf(rec, touching),
      blockKey: carrier.blockKey || '',
      prefix: '',
      suffix: '',
      anchored: true,
    },
  };
}

// --- retracting: an undone edit leaves no trail ----------------------------

/**
 * diffOf reduces a whole transaction to the minimal positional change between
 * its before and after documents.
 *
 * IT EXISTS BECAUSE AN UNDO IS NOT A SMALL STEP. Yjs owns undo here, and
 * y-prosemirror renders an UndoManager pop as one ReplaceStep over everything
 * from the first changed position to the last — measured: undoing a
 * two-character insertion arrived as a replace of the whole document, [0,55)
 * against a 53-size slice. A pairing keyed on the step's own range would
 * never fire, so the pairing is keyed on the DIFF instead: findDiffStart /
 * findDiffEnd give the minimal range, clamped where an overlap (undoing "aa"
 * out of "aaa") pulls the end past the start.
 *
 */
export function diffOf(before: PMNode, after: PMNode): TrailDiff | null {
  const start = before.content.findDiffStart(after.content);
  if (start === null) {
    return null;
  }
  const end = before.content.findDiffEnd(after.content);
  if (!end) {
    return null;
  }
  let { a, b } = end;
  if (a < start) {
    b += start - a;
    a = start;
  }
  if (b < start) {
    a += start - b;
    b = start;
  }
  try {
    return {
      from: start,
      to: a,
      removed: before.textBetween(start, a, '\n', '\n'),
      inserted: after.textBetween(start, b, '\n', '\n'),
    };
  } catch {
    return null;
  }
}

/**
 * retractReverted removes the entries an undo exactly reverses, and ONLY
 * those. y-prosemirror renders a Yjs undo as one replace over the whole
 * changed region (see diffOf), and one Cmd-Z can revert a BATCH of edits with
 * unrelated trailed text standing between them — so a single-entry pairing is
 * not enough, and a naive containment sweep retracts bystanders.
 *
 * The pairing is a reconstruction instead: over the entries whose ranges lie
 * inside the undo's span, find the subset whose reversal turns the span's
 * before-text into its after-text exactly — stitch the unchanged text around
 * each chosen entry's `old` (a non-chosen entry contributes its `new`, i.e.
 * stays as it was) and compare. Exactly one subset matching is a proof of
 * which edits the undo reverted; zero or several is ambiguity, and ambiguity
 * retracts NOTHING and records nothing — never double-record, never guess.
 * Bounded: past a dozen covered entries the search is skipped and the
 * caller's no-phantom net (dropping entries the undo itself set adrift) is
 * the answer.
 *
 * @param entries
 * @param undo
 */
export function retractReverted(
  entries: TrailEntry[] | null | undefined,
  undo: TrailDiff,
): { entries: TrailEntry[]; retracted: boolean } {
  const list = entries || [];
  const covered = list.filter(
    (e): e is AnchoredTrailEntry =>
      e.anchored && e.from >= undo.from && e.to <= undo.to,
  );
  if (!covered.length || covered.length > 12) {
    return { entries: list, retracted: false };
  }
  covered.sort((a, b) => a.from - b.from);
  const gap = (a: number, b: number) =>
    undo.removed.slice(a - undo.from, b - undo.from);
  const winners: number[] = [];
  for (let mask = 1; mask < 1 << covered.length; mask += 1) {
    let rebuilt = '';
    let cursor = undo.from;
    let ok = true;
    for (let i = 0; i < covered.length; i += 1) {
      const e = covered[i];
      if (e.from < cursor) {
        ok = false;
        break;
      }
      rebuilt += gap(cursor, e.from) + ((mask >> i) & 1 ? e.old : e.new);
      cursor = e.to;
    }
    if (!ok) {
      continue;
    }
    rebuilt += gap(cursor, undo.to);
    if (rebuilt === undo.inserted) {
      winners.push(mask);
      if (winners.length > 1) {
        break;
      }
    }
  }
  if (winners.length !== 1) {
    return { entries: list, retracted: false };
  }
  const drop = new Set<TrailEntry>(
    covered.filter((_, i) => (winners[0] >> i) & 1),
  );
  return { entries: list.filter((e) => !drop.has(e)), retracted: true };
}

// --- anchoring: where an entry lives now -----------------------------------

// textOfBlock reads a textblock's text with CHARACTER OFFSETS EQUAL TO
// POSITION OFFSETS, or null where that cannot be promised: text nodes count
// character for position, a one-position leaf (a hard break) counts as the
// same '\n' textBetween spells it, and anything else makes offset arithmetic
// a lie this refuses to tell.
function textOfBlock(node: PMNode): string | null {
  let text = '';
  let honest = true;
  node.content.forEach((child) => {
    if (child.isText) {
      text += child.text;
    } else if (child.isLeaf && child.nodeSize === 1) {
      text += '\n';
    } else {
      honest = false;
    }
  });
  return honest ? text : null;
}

// topIndexAt is the top-level child index a position falls under — the same
// ordinal suggest.BlockRef.Index carries, read and used immediately, never
// stored (an ordinal is not identity; the KEY is what an entry keeps).
function topIndexAt(doc: PMNode, pos: number): number {
  try {
    return doc.resolve(pos).index(0);
  } catch {
    return -1;
  }
}

// blockKeyAt names the block a position falls in, via the last pending
// payload's block list — a content hash the browser cannot compute itself.
// Best-effort: the edit this is recorded for is about to CHANGE the block and
// so move its key, which is why re-anchoring leans on context and treats the
// key as a narrowing hint only.
function blockKeyAt(
  doc: PMNode,
  pos: number,
  blocks: BlockRef[] | null | undefined,
): string {
  const index = topIndexAt(doc, pos);
  const ref = (blocks || []).find((b) => b && b.key && b.index === index);
  return ref ? ref.key : '';
}

// tailChars and headChars bound a neighbour's text the same way contextOf
// bounds an affix, and they are SURROGATE-GUARDED for the reason every other
// walk in this file is: these strings are UTF-16, a code-unit cut can land
// between a surrogate pair, and half a pair does not survive a JSON round trip
// through the sidecar intact — the entry would come back not equal to itself.
function tailChars(s: string, n: number): string {
  if (s.length <= n) {
    return s;
  }
  let i = s.length - n;
  if (isLowSurrogate(s.charCodeAt(i))) {
    i += 1;
  }
  return s.slice(i);
}

function headChars(s: string, n: number): string {
  if (s.length <= n) {
    return s;
  }
  let i = n;
  if (isLowSurrogate(s.charCodeAt(i))) {
    i += 1;
  }
  return s.slice(0, i);
}

// neighboursAt reads the text of the blocks either side of blocks[i] — the
// identity a block-emptying deletion has instead of its own text. Bounded a
// side by TRAIL_CONTEXT_CHARS, the same bound the in-block affixes take: an
// edit further away than that inside a neighbour must not cost an entry its
// place.
//
// `null` MEANS "THERE IS NO BLOCK THAT SIDE" AND '' MEANS "THE BLOCK THAT SIDE
// IS EMPTY", AND THE DAY THEY WERE THE SAME VALUE A GHOST LANDED ON A STRANGER.
// They are different facts about the document and the comparison in
// emptyBlockAnchor cannot tell them apart if they arrive as one string: an
// entry that emptied a block whose neighbour was ITSELF empty then matched a
// block sitting at the document's edge — on BOTH sides at once, so the
// two-sided rule passed and refused nothing. It takes two block-emptying
// deletions on adjacent blocks and one Backspace to close the gap, which is
// ordinary cleanup and not an exotic gesture; measured, with the ghost of a
// sentence deleted elsewhere rendering beside the ghost that block really did
// own. web/probe.mjs pins the whole trace.
function neighboursAt(blocks: ReviewerBlock[], i: number): Neighbours {
  return {
    before:
      i > 0
        ? tailChars(blocks[i - 1].node.textContent, TRAIL_CONTEXT_CHARS)
        : null,
    after:
      i + 1 < blocks.length
        ? headChars(blocks[i + 1].node.textContent, TRAIL_CONTEXT_CHARS)
        : null,
  };
}

// A block outside the reviewer population (a fence, a table cell, a note) has
// no neighbours to report, because emptyBlockAnchor will never look at it
// again — null/null is the same "nothing recorded" a legacy record carries,
// and it lands on the same place: refusal everywhere except a document whose
// only block is the empty one.
function blockNeighbours(doc: PMNode, blockPos: number): Neighbours {
  const blocks = reviewerBlocks(doc);
  const i = blocks.findIndex((b) => b.pos === blockPos);
  return i === -1 ? { before: null, after: null } : neighboursAt(blocks, i);
}

/**
 * contextOf reads the text around a range, clamped to its own textblock —
 * the prefix/suffix anchors an entry re-finds its place by after a reload.
 *
 * AND, FOR THE ONE ENTRY THAT HAS NO TEXT AROUND IT, THE BLOCKS EITHER SIDE.
 * A deletion that emptied its own textblock has '' on both sides by
 * construction — that is what makes it the case emptyBlockAnchor exists for —
 * so within its own block there is nothing left to be identified by, and
 * "somewhere in this document there is an empty block" is not identification.
 * `before`/`after` are that entry's evidence and are filled ONLY for it: the
 * discriminator is the block being EMPTY, exactly the one verifyEntry uses, so
 * the two fields mean one thing wherever they are non-blank.
 *
 * They are not a second prefix/suffix and must not be used as one. The affixes
 * are what an entry's own text sits BETWEEN and are searched for; these are
 * what an entry's own BLOCK sits between and are only ever compared.
 *
 * EACH SIDE IS THREE-VALUED AND EVERY LAYER HAS TO KEEP IT THAT WAY: a string
 * (that block's text, '' when the block is empty), or `null` for "there is no
 * block that side". An entry that is not a block-emptying deletion carries
 * null on both sides — it has affixes to be found by and this evidence is
 * never read for it. See neighboursAt for what conflating the two cost.
 */
export function contextOf(doc: PMNode, from: number, to: number): TrailContext {
  let $from;
  try {
    $from = doc.resolve(from);
  } catch {
    return { prefix: '', suffix: '', before: null, after: null };
  }
  if (!$from.parent.isTextblock) {
    return { prefix: '', suffix: '', before: null, after: null };
  }
  const start = $from.start();
  const end = $from.end();
  const upto = Math.min(Math.max(to, from), end);
  const prefix = doc.textBetween(
    Math.max(start, from - TRAIL_CONTEXT_CHARS),
    from,
    '\n',
    '\n',
  );
  const suffix = doc.textBetween(
    upto,
    Math.min(end, upto + TRAIL_CONTEXT_CHARS),
    '\n',
    '\n',
  );
  if (prefix || suffix || $from.parent.content.size !== 0) {
    return { prefix, suffix, before: null, after: null };
  }
  return { prefix, suffix, ...blockNeighbours(doc, $from.before($from.depth)) };
}

/**
 * reviewerBlocks lists, in document order, every textblock THE REVIEWER'S OWN
 * HAND COULD HAVE EMPTIED — which is the only population `emptyBlockAnchor`
 * may search, in either direction.
 *
 * THREE KINDS ARE EXCLUDED AND EACH IS UNREACHABLE TO A KEYBOARD.
 *
 *   a code fence   `suggestions.ts`'s transaction filter refuses every edit
 *                  that rewrites one, so nothing typed here can empty it —
 *                  and an empty fence is a document shape that exists AT
 *                  REST. Measured: a `.md` holding ``` on two adjacent lines
 *                  gives the browser one empty textblock before a key is
 *                  pressed, which was enough on its own to make a real
 *                  deletion's ghost refuse for ambiguity. That is Court's
 *                  original bug reproduced by an ordinary document.
 *   a table        literal for the same reason and refused by the same
 *                  filter. Its cells are excluded WITH it, because a cell's
 *                  content is a paragraph (TipTap's `tableCell` is `block+`)
 *                  and a paragraph in a table is still not the reviewer's.
 *                  THIS EXCLUSION NOW HAS A REAL POPULATION, and the note
 *                  here used to say it did not: a blank cell was measured
 *                  never reaching the browser as an empty paragraph at all,
 *                  because `markdown.tableRow` gave an empty cell no child
 *                  and y-prosemirror DELETES a `tableCell` that cannot
 *                  satisfy `block+`. That deletion was projected to the
 *                  author's .md — every value after a blank cell slid one
 *                  column left, on open, with no keystroke — so the empty
 *                  population was a symptom of a data-corruption bug rather
 *                  than a property to rely on. Parse gives every cell a block
 *                  now, so a blank cell IS an empty paragraph and this
 *                  exclusion is load-bearing rather than precautionary.
 *                  web/typing.mjs §0 and §6 pin both facts.
 *   a note         `note.ts` renders it `contenteditable="false"`; its words
 *                  are authored through the composer and the rail, never by
 *                  typing into the document.
 *
 * Excluding them is safe in both directions and that is the whole argument: a
 * reviewer's deletion can never PRODUCE one, so counting them can only add a
 * false refusal (§6's bug) or offer a wrong anchor (§8's) — never a right
 * answer this would lose.
 *
 */
function reviewerBlocks(doc: PMNode): ReviewerBlock[] {
  const out: ReviewerBlock[] = [];
  doc.descendants((node, pos) => {
    // A table is pruned WHOLE — the cells and their paragraphs go with it.
    // `note` is checked before the textblock test because a note IS a
    // textblock (`content: 'text*'`).
    if (isTable(node) || node.type.name === 'note') {
      return false;
    }
    if (!node.isTextblock) {
      return true;
    }
    if (!isFence(node)) {
      out.push({ node, pos });
    }
    return false;
  });
  return out;
}

// side normalises one piece of neighbour evidence to the three states the
// comparison knows: a string (that block's text, '' when the block is empty)
// or null for "no block that side / nothing recorded". Anything that is not a
// string — undefined from a legacy record, null from the wire — is the second,
// and the wire cannot tell those two apart anyway: both mean there is no text
// that side to be identified by.
const side = (v: unknown): string | null => (typeof v === 'string' ? v : null);

// flag is `side` for a boolean: true/false as themselves, and ANYTHING else —
// undefined from a legacy record, null from the wire — as null, which reads
// "we do not know". `placed` is the only field it is used for, and null is the
// value that costs an entry its say in the order (see placeEmptyBlocks).
const flag = (v: unknown): boolean | null =>
  typeof v === 'boolean' ? v : null;

// emptyCandidates lists, in DOCUMENT ORDER, every empty block a reviewer's
// hand could have emptied, each carrying the neighbour evidence a
// block-emptying entry is compared against. One walk, one population, read by
// both the single-entry rule and the set rule below — which is the same rule
// twice, not two rules that agree for now.
function emptyCandidates(doc: PMNode): EmptyCandidate[] {
  const blocks = reviewerBlocks(doc);
  const out: EmptyCandidate[] = [];
  for (let i = 0; i < blocks.length; i += 1) {
    if (blocks[i].node.content.size !== 0) {
      continue;
    }
    out.push({ pos: blocks[i].pos + 1, ...neighboursAt(blocks, i) });
  }
  return out;
}

// evidenceMatches is the two-sided comparison, stated once: BOTH neighbours or
// nothing, with absent normalised to null so "no block that side" can never
// read as "an empty block that side".
function evidenceMatches(
  entry: EmptyBlockEvidence,
  candidate: EmptyCandidate,
): boolean {
  return (
    candidate.before === side(entry.before) &&
    candidate.after === side(entry.after)
  );
}

const anchorAt = (candidate: EmptyCandidate): TrailAnchor => ({
  from: candidate.pos,
  to: candidate.pos,
});

// How many block-emptying entries the set rule will search over, and how much
// searching it will do before it gives up. Both are BOUNDS on a backtracking
// search, not statements about what a reviewer might do: past either the set
// rule hands back to the weak rule below, so a very large trail degrades to a
// weaker answer rather than to a hang or to a blanket refusal.
//
// THE CAP IS THE BOUND, AND IT IS THE MEASURED ONE. The first version of this
// advertised sixteen entries with a budget of 20000 nodes underneath it — and
// the budget bit at NINE adjacent strikes and EIGHT strikes in a stanza of
// identical lines, so the sixteen-entry branch was unreachable and the number
// in the docstring was roughly double the real bound. A reviewer striking a
// ten-line list by hand got the weak answer while being told the bound was
// sixteen. Measured since, in a real chromium (141.0.7390.37), timing
// settleEntries over the document a server-side mutation leaves — median, with
// the search's own node counter beside it:
//
//   entries   adjacent strikes          identical stanza
//         8      7,260 nodes   0.2ms       24,821 nodes    0.5ms
//        10    100,616 nodes   1.6ms      354,763 nodes    8.1ms
//        11    380,264 nodes   6.1ms    1,356,173 nodes   33.2ms
//        12  1,446,508 nodes  24.9ms    5,208,491 nodes  132.5ms
//        16     315.6M nodes    6.1s       1.167G nodes    35.6s
//
// Ten is where that stops being free: eleven costs four times ten, and sixteen
// — the number that used to be advertised — is thirty-five SECONDS on the
// settle path, which is not a bound at all but a hang with a docstring.
//
// AND THE BUDGET IS NOW A BACKSTOP RATHER THAN THE BOUND. 600000 affords the
// worst shape a hand produces at the cap (354,763 nodes, 8.1ms) with room to
// spare, so for anything a reviewer can type it is the CAP that binds and the
// advertised number is the true one. It is kept, and kept low, because cost is
// entries against CANDIDATES and the cap counts only entries: ten strikes in a
// document full of blank lines is a shape no table above covers, and at roughly
// 45k nodes/ms this budget holds any such shape to about 13ms. probe.mjs §9d
// pins both halves — that the budget affords the cap, and that a shape it
// cannot afford returns promptly instead of hanging.
export const TRAIL_SET_CAP = 10;
const TRAIL_SET_NODES = 600000;

/**
 * placeEmptyBlocks resolves the block-emptying entries AS A SET, and it is the
 * ONE rule for placing them — emptyBlockAnchor below is this function with a
 * set of one, so there is no second rule to drift.
 *
 * WHY A SET AND NOT n INDEPENDENT QUESTIONS. Every rung above searches for
 * TEXT; a deletion that emptied its own block has none, so all it can bring is
 * the text of the blocks either side (`before`/`after`) and the fact that its
 * block is empty. Asked one entry at a time that evidence is often ambiguous
 * and the entry refuses — correctly, on its own terms. But two entries asked
 * TOGETHER know two things a single entry cannot: no two of them can be on the
 * SAME block (a block holds one emptiness, and applyRecord coalesces anything
 * that touches an existing entry), and they are LISTED IN DOCUMENT ORDER.
 * Measured before this existed, on the trail's own passes:
 *
 *   two paragraphs struck between three IDENTICAL lines — both entries carry
 *     the same evidence, both refused, and both ghosts were lost on the next
 *     server-side mutation. Ordinary in a list or a repeated stanza.
 *   four adjacent paragraphs struck — the two INTERIOR entries both record
 *     "an empty block above, an empty block below" and refused; the outer two
 *     anchored. Two ghosts of four.
 *   two struck between identical lines, then the lower gap closed — the lower
 *     entry's block was joined away, its stale evidence still matched the
 *     UPPER entry's block, and its ghost was drawn there: TWO GHOSTS ON ONE
 *     BLOCK, the reviewer reading two deletions on a line that lost one.
 *
 * The third of those is a WRONG ANCHOR, which spec decision 3 rates worse than
 * a missing one — and no per-entry rung can see it, because the block is only
 * taken from the other entry's point of view.
 *
 * THE ORDER IS A FACT ABOUT THE LIST, AND THE LIST ONLY SPEAKS FOR ENTRIES
 * THAT HAD A PLACE. settleEntries sorts by position on every pass, so the
 * trail is in document order — but it sorts entries with NO place to the end,
 * where their position in the list means nothing at all. So each entry carries
 * `placed`: true when it had a place at the settle that last wrote it, false
 * when it did not, and null for a record written before the field existed. An
 * entry is ORDERED — allowed to constrain and be constrained by the order —
 * exactly when that is true. A legacy record is therefore unordered, and an
 * ambiguity the order would have settled REFUSES for it, which is the honest
 * degradation: the field cannot be invented from a record that never carried
 * it.
 *
 * WHAT IS NOT USED, AND WAS MEASURED RATHER THAN ASSUMED: how far a candidate
 * is from where the entry used to be. Every server-side mutation arrives as
 * ONE whole-document ReplaceStep (y-prosemirror rebuilds the doc), and every
 * position inside a replaced range maps to the same edge of it — measured, two
 * entries 26 positions apart both mapped to 26. The distance signal is not
 * weak here, it is GONE, and a rung leaning on it would be reading noise.
 * `at` is not used either: the order edits were MADE in is not the order they
 * sit in.
 *
 * THE RESOLUTION. Each entry's compatible candidates are those its evidence
 * matches; a candidate another entry still stands in is not offered at all
 * (`held`). Over that graph the search takes the MAXIMUM-cardinality injective
 * assignments that respect the order among the ordered entries, and an entry
 * is placed only where EVERY one of them gives it the same candidate.
 * Maximum-cardinality rather than all-or-nothing so that an entry with NO
 * compatible candidate — its block is simply gone — cannot cost its neighbours
 * their ghosts; same-in-every-assignment rather than first-found because a
 * choice the search had to make is exactly the ambiguity that must refuse. Two
 * entries and three candidates they all match still refuses, and so does any
 * set whose order is not established. Note the limit of the first half: an
 * entry whose block is gone but whose stale evidence still OVERLAPS its
 * neighbours' is not dropped, it competes — several assignments are equally
 * large, they disagree, and everybody refuses. That is a lost ghost rather than
 * a wrong one, which is the safe direction, but it is not "cannot cost its
 * neighbours".
 *
 * THE BOUND IS TRAIL_SET_CAP ENTRIES, with the node budget as a backstop under
 * it — both stated, and measured, where they are declared above. Past either,
 * the weak rule: one compatible candidate and nobody else wanting it.
 *
 * @param doc
 * @param entries the block-emptying entries, IN TRAIL ORDER
 * @param held positions of empty blocks other entries still hold
 * @returns one answer per entry, in order
 */
export function placeEmptyBlocks(
  doc: PMNode,
  entries: EmptyBlockEvidence[] | null | undefined,
  held: number[] | null | undefined,
): (TrailAnchor | null)[] {
  const list = entries || [];
  const out: (TrailAnchor | null)[] = list.map(() => null);
  if (!list.length) {
    return out;
  }
  const taken = new Set(held || []);
  const candidates = emptyCandidates(doc).filter((c) => !taken.has(c.pos));
  if (!candidates.length) {
    return out;
  }
  // Who could be where. An entry with no compatible candidate at all is not a
  // refusal to be reasoned about — its block is simply not in the document any
  // more — so it drops out here and cannot affect anybody else's answer.
  const compat = list.map((e) => {
    const ok: number[] = [];
    for (let c = 0; c < candidates.length; c += 1) {
      if (evidenceMatches(e || {}, candidates[c])) {
        ok.push(c);
      }
    }
    return ok;
  });
  const live: number[] = [];
  for (let i = 0; i < list.length; i += 1) {
    if (compat[i].length) {
      live.push(i);
    }
  }
  if (!live.length) {
    return out;
  }
  // PAST THE BOUND — or past the search's own node budget — THE OLDER AND
  // WEAKER ANSWER: an entry is placed only where it has one candidate and NO
  // OTHER entry could want that one. That second half is not the old rule and
  // is not optional: without it the fallback reintroduces two ghosts on one
  // block, which is the wrong anchor this function exists to stop. Both halves,
  // and the boundary either side of the cap, are pinned in probe.mjs §9d —
  // reachable by an ordinary hand, so tested like anything else.
  const uncontested = (): (TrailAnchor | null)[] => {
    const weak: (TrailAnchor | null)[] = list.map(() => null);
    for (const i of live) {
      if (compat[i].length !== 1) {
        continue;
      }
      const only = compat[i][0];
      if (!live.some((j) => j !== i && compat[j].includes(only))) {
        weak[i] = anchorAt(candidates[only]);
      }
    }
    return weak;
  };
  if (live.length > TRAIL_SET_CAP) {
    return uncontested();
  }

  const ordered = live.map((k) => list[k].placed === true);
  const used: boolean[] = new Array(candidates.length).fill(false);
  const pick: number[] = new Array(live.length).fill(-1);
  let nodes = 0;
  let blown = false;

  // walk enumerates injective, order-respecting assignments. `floor` is the
  // candidate the last ORDERED entry took: a later ordered entry must take a
  // later candidate, which is what document order means. An unordered entry
  // neither reads the floor nor moves it. `want` prunes every branch that can
  // no longer reach the size being looked for.
  const walk = (
    k: number,
    floor: number,
    size: number,
    want: number,
    visit: (size: number) => void,
  ): void => {
    nodes += 1;
    if (nodes > TRAIL_SET_NODES) {
      blown = true;
    }
    if (blown) {
      return;
    }
    if (size + (live.length - k) < want) {
      return;
    }
    if (k === live.length) {
      visit(size);
      return;
    }
    for (const c of compat[live[k]]) {
      if (used[c] || (ordered[k] && c <= floor)) {
        continue;
      }
      used[c] = true;
      pick[k] = c;
      walk(k + 1, ordered[k] ? c : floor, size + 1, want, visit);
      used[c] = false;
      pick[k] = -1;
      if (blown) {
        return;
      }
    }
    // AND THE ENTRY MAY GO UNPLACED, which is what keeps one entry whose block
    // is gone from poisoning the rest of the set: the search is for the
    // BIGGEST assignment, not for a total one. It rescues the neighbours only
    // where that entry matches NOTHING — an entry whose stale evidence still
    // overlaps theirs competes for their slots instead, and then several
    // assignments are equally large and everybody refuses.
    walk(k + 1, floor, size, want, visit);
  };

  let best = 0;
  walk(0, -1, 0, 1, (size) => {
    if (size > best) {
      best = size;
    }
  });
  if (blown) {
    return uncontested();
  }
  if (best === 0) {
    return out;
  }
  const seen: Set<number>[] = live.map(() => new Set());
  walk(0, -1, 0, best, (size) => {
    if (size !== best) {
      return;
    }
    for (let j = 0; j < live.length; j += 1) {
      seen[j].add(pick[j]);
    }
  });
  if (blown) {
    return uncontested();
  }
  for (let j = 0; j < live.length; j += 1) {
    // ONE ANSWER IN EVERY MAXIMUM ASSIGNMENT, or none. A `-1` in the set is
    // "this entry went unplaced in one of them", which is itself a choice the
    // search had to make and so an ambiguity.
    if (seen[j].size === 1 && !seen[j].has(-1)) {
      out[live[j]] = anchorAt(candidates[[...seen[j]][0]]);
    }
  }
  return out;
}

/**
 * emptyBlockAnchor places the ONE entry the text ladder can never place: a
 * deletion that emptied its own textblock.
 *
 * THE DEFECT IT EXISTS FOR. Every rung of `reanchor` is a search for TEXT —
 * `prefix + new + suffix`, found in the document as it now reads. A deletion
 * has no `new` by definition, and `contextOf` clamps its context to the
 * entry's own textblock, which this deletion has just emptied — so both
 * affixes come back '' and the needle is the EMPTY STRING. The ladder skips an
 * empty needle and answers null, so the first thing that invalidates live
 * positions set the entry adrift and its ghost stopped rendering. That first
 * thing is not rare: EVERY server-side mutation replaces the whole document
 * (CLAUDE.md), so filing a comment, or an agent's proposal arriving, was
 * enough — measured: delete a paragraph, ghost renders, `△ 1 changed`; POST
 * one comment; ghost gone, entry still in `/_galley/pending` and in the
 * sidecar holding the deleted text. The record survived; only its place was
 * lost.
 *
 * WHAT SUCH AN ENTRY STILL KNOWS is not text but STRUCTURE: it left a
 * textblock with nothing in it, and the rebuild carries that empty block
 * through (the projection round-trips it).
 *
 * UNIQUENESS ALONE IS NOT IDENTIFICATION, AND THAT WAS THIS RUNG'S OWN BUG.
 * "Exactly one empty block in the document" says the document is unambiguous;
 * it says NOTHING about the block being the one this entry emptied, and the
 * two come apart on an ordinary gesture. Strike a paragraph, then press
 * Backspace once more to close up the blank: the join is a cross-block step,
 * so `recordOf` declines it and nothing new is recorded, the entry maps into
 * the merged paragraph where `verifyEntry` rightly refuses it, and its own
 * block no longer exists. With one other empty block anywhere in the document
 * — screens away, in prose the reviewer never touched — the ghost was drawn
 * THERE. Measured, in web/typing.mjs §8. That is a deletion reported where
 * none happened, which is a worse failure than the missing ghost this rung was
 * added to fix: spec decision 3 is that the trail refuses rather than guesses,
 * and a refusal costs a ghost while a guess costs the reader's trust in every
 * other one.
 *
 * SO THE ENTRY MUST BRING EVIDENCE, and what it brings is `before`/`after` —
 * the text of the blocks either side of the block it emptied, recorded by
 * `contextOf` at the moment of the edit and refreshed on every settle for as
 * long as the entry keeps its place. A candidate qualifies only when BOTH
 * sides match. Both, not either: after a join the surviving empty block can
 * inherit the joined block's own neighbour on one side, so a one-sided rule
 * accepts exactly the case the two-sided one refuses.
 *
 * AND "NO BLOCK THAT SIDE" IS NOT "AN EMPTY BLOCK THAT SIDE". Each side is
 * `null` for the first and a string — possibly '' — for the second, because
 * one value for both is a comparison that cannot tell them apart, and the
 * two-sided rule then passes on BOTH sides at once: an entry that emptied a
 * block whose neighbour was itself empty matched a block standing at the
 * document's edge, and its ghost was drawn on a block it never touched, beside
 * the ghost that block did own. Two block-emptying deletions on adjacent
 * blocks and one Backspace to close the gap is all it takes. Measured on the
 * shipped build; pinned in probe.mjs both directly and through settleEntries.
 *
 * The failure mode this leaves is a FALSE REFUSAL — a reviewer who rewrites a
 * neighbouring block within TRAIL_CONTEXT_CHARS of the boundary, and only
 * across a rebuild, loses a ghost into the log. That is the acceptable side.
 *
 * An entry with NO evidence (a record written by a build before these fields
 * existed, read back from the sidecar) carries nothing on either side and
 * therefore matches only a candidate that has no neighbours on either side —
 * which is to say the document's ONLY block. That is not a special case bolted
 * on; it is the same comparison, and it lands on refuse everywhere refusal is
 * the honest answer and on the one place where uniqueness really is identity.
 * It is stated here and ASSERTED in probe.mjs, three rows of it: a document of
 * [text, empty, empty] refuses, [text, empty] refuses, and [empty] alone
 * places. The first of those rows anchored — wrongly — for as long as an
 * absent side and an empty one were the same ''.
 *
 * `blockKey` IS NOT THE ANSWER HERE AND MUST NOT BE MADE TO LOOK LIKE ONE. It
 * is stamped from the doc the step applied to, so it names the block's
 * PRE-EDIT content hash — and the edit being recorded is the one that emptied
 * the block, so the key it holds is the key of text that no longer exists. It
 * was measured stale on the very case this function is for.
 *
 * @param doc
 * @param entry the entry asking
 */
export function emptyBlockAnchor(
  doc: PMNode,
  entry: EmptyBlockEvidence | null | undefined,
): TrailAnchor | null {
  // ONE ENTRY IS A SET OF ONE, and this is that call rather than a second
  // implementation of the same comparison. With one entry the search finds one
  // assignment per compatible candidate and forces an answer only when there
  // is exactly one of them, which IS the uniqueness rule this function has
  // always applied; the order plays no part, because a set of one has no
  // order. Everything the docstring above claims — the two-sided evidence,
  // absent-is-not-empty, an evidence-free record matching only the document's
  // ONLY block — is that comparison, and it is stated once in
  // evidenceMatches/emptyCandidates so a set and a single entry can never
  // answer differently.
  //
  // What the SET knows and a single entry cannot is above, in
  // placeEmptyBlocks: no two entries may take one block, and the trail is in
  // document order. settleEntries asks in the plural for that reason; this
  // form is the ladder's rung (`reanchor`) and probe.mjs's direct handle on
  // the comparison itself.
  return placeEmptyBlocks(doc, [entry || {}], [])[0];
}

/**
 * reanchor re-finds an entry's place from blockKey + context, best-effort and
 * honest: the needle is prefix + new + suffix, a match inside the entry's own
 * block wins when it is unique there, a unique match anywhere in the document
 * is accepted otherwise, and ANYTHING ELSE — no match, several matches, no
 * context at all — answers null, which renders the entry in the log only.
 * Never guessed onto the wrong text.
 *
 * @param doc
 * @param entry
 * @param blocks the pending payload's blocks
 */
export function reanchor(
  doc: PMNode,
  entry: ReanchorEntry,
  blocks: BlockRef[] | null | undefined,
): TrailAnchor | null {
  const mid = entry.new || '';
  const prefix = entry.prefix || '';
  const suffix = entry.suffix || '';
  // NO TEXT ON ANY SIDE is a deletion that emptied its own block, and no rung
  // below can place it — the needle would be ''. It has a rung of its own,
  // and it is structural rather than textual: see emptyBlockAnchor. The ENTRY
  // goes with it, because that rung needs the entry's own evidence and not
  // merely the document.
  //
  // THIS IS THE SINGLE-ENTRY ANSWER, AND NOTHING IN PRODUCTION REACHES IT. A
  // set of such entries knows two things one of them cannot
  // (placeEmptyBlocks), so the settle pass asks in the plural: settleEntries
  // calls reanchor only for `need === 'find'`, which is `!contextless` by
  // construction, so this branch is dead below it and its only callers are
  // probe.mjs's direct ones. It is kept because it is the same rule, not a
  // second one — emptyBlockAnchor is placeEmptyBlocks with a set of one, and
  // the comparison itself lives in evidenceMatches/emptyCandidates — so the
  // refusal checks written against it are checks of the rule the settle pass
  // runs. A caller holding one entry and nothing else would land here; there
  // is no such caller today.
  if (contextless({ new: mid, prefix, suffix })) {
    return emptyBlockAnchor(doc, entry);
  }
  // The ladder: full context first; where the full needle finds NOTHING (a
  // neighbouring edit was itself undone or revised, so one stored side is
  // stale), each half-context is tried — still under the uniqueness rule, and
  // only when the weaker needle is long enough that a unique match is
  // identification rather than luck. A ghost (no `new`) has only its context
  // to be found by, so it gets no weaker rung. AMBIGUITY STOPS THE LADDER:
  // several matches is a document this entry cannot claim a place in, and a
  // weaker needle can only be more ambiguous.
  const rungs = [
    { needle: prefix + mid + suffix, lead: prefix.length, least: 1 },
  ];
  if (mid) {
    rungs.push(
      { needle: prefix + mid, lead: prefix.length, least: 12 },
      { needle: mid + suffix, lead: 0, least: 12 },
    );
  }
  for (const rung of rungs) {
    if (!rung.needle || rung.needle.length < rung.least) {
      continue;
    }
    const matches: { from: number; to: number; top: number }[] = [];
    doc.descendants((node, pos) => {
      if (!node.isTextblock) {
        return true;
      }
      const text = textOfBlock(node);
      if (text === null) {
        return false;
      }
      let i = text.indexOf(rung.needle);
      while (i !== -1) {
        const from = pos + 1 + i + rung.lead;
        matches.push({
          from,
          to: from + mid.length,
          top: topIndexAt(doc, pos + 1),
        });
        i = text.indexOf(rung.needle, i + 1);
      }
      return false;
    });
    if (matches.length === 0) {
      continue;
    }
    if (entry.blockKey) {
      const ref = (blocks || []).find((b) => b && b.key === entry.blockKey);
      if (ref && Number.isFinite(ref.index)) {
        const within = matches.filter((m) => m.top === ref.index);
        if (within.length === 1) {
          return { from: within[0].from, to: within[0].to };
        }
      }
    }
    return matches.length === 1
      ? { from: matches[0].from, to: matches[0].to }
      : null;
  }
  return null;
}

// verifyEntry asks whether an anchored entry's positions still tell the
// truth: an insertion's range must read exactly its `new`, and a ghost's
// point must still sit between its own prefix and suffix.
export function verifyEntry(doc: PMNode, e: TrailEntry): boolean {
  if (!e.anchored || !Number.isFinite(e.from)) {
    return false;
  }
  try {
    if (e.to > doc.content.size || e.from < 0) {
      return false;
    }
    if (e.new) {
      return doc.textBetween(e.from, e.to, '\n', '\n') === e.new;
    }
    const $p = doc.resolve(e.from);
    if (!$p.parent.isTextblock) {
      return false;
    }
    const start = $p.start();
    const end = $p.end();
    const prefix = e.prefix || '';
    const suffix = e.suffix || '';
    if (!prefix && !suffix) {
      // A CONTEXTLESS GHOST CANNOT BE VERIFIED BY CONTEXT. The two comparisons
      // below are '' === '' at every position of every textblock in the
      // document, so an entry whose live positions a rebuild has already
      // invalidated would pass here and keep an anchor it has no claim to —
      // a ghost guessed onto the wrong text, which is the one thing the spec
      // says never to do. What such an entry actually knows about itself is
      // that its own block was left EMPTY (see emptyBlockAnchor, which is what
      // re-finds it), so that is the question asked.
      return $p.parent.content.size === 0;
    }
    return (
      doc.textBetween(
        Math.max(start, e.from - prefix.length),
        e.from,
        '\n',
        '\n',
      ) === prefix &&
      doc.textBetween(
        e.from,
        Math.min(end, e.from + suffix.length),
        '\n',
        '\n',
      ) === suffix
    );
  } catch {
    return false;
  }
}

// contextless names the ONE entry the text ladder can never place: a deletion
// that emptied its own textblock, which has no `new` and — context being
// clamped to that block — no affixes either. It is the same test `reanchor`
// branches on, spelled once so the set pass and the ladder cannot disagree
// about which entries the empty-block rule is for.
const contextless = (e: {
  new: string;
  prefix: string;
  suffix: string;
}): boolean => !e.new && !e.prefix && !e.suffix;

/**
 * settleEntries is the pass every doc change ends on: anchored entries that
 * still verify keep their place (context refreshed), ones that do not are
 * re-anchored — or set adrift, log-only, when re-anchoring refuses. Adrift
 * entries only RETRY their anchor when asked (a rebuild, a load): retrying on
 * every keystroke would be a whole-document scan per character.
 *
 * IT IS TWO PASSES BECAUSE THE BLOCK-EMPTYING ENTRIES ARE ONE QUESTION. Every
 * other entry is found by its own text and can be settled alone; a deletion
 * that emptied its block has no text, and n of them against m empty blocks is
 * an ASSIGNMENT — see placeEmptyBlocks for what a set knows that an entry on
 * its own does not, and for the three configurations that were measured wrong
 * before it existed. So the first pass decides what each entry needs, the
 * empty-block entries are resolved together, and the second pass builds the
 * list.
 *
 * A BLOCK AN ENTRY STILL STANDS IN IS NOT FREE. An entry that verified this
 * pass holds its empty block, and `held` takes it out of the candidates — one
 * emptiness is one entry's (applyRecord coalesces anything that touches an
 * existing entry, so a second edit in an emptied block joins its record rather
 * than starting one). Without that, an entry whose own block was joined away
 * could be handed a neighbour's and the reviewer would read two deletions on a
 * line that lost one.
 *
 * AND EVERY ENTRY LEAVES CARRYING `placed`, which is what the NEXT settle — or
 * the next session, through the sidecar — reads to know whether this list's
 * order speaks for it. Written from the outcome, never assumed: an entry that
 * ends this pass adrift is sorted to the end below, so its position in the
 * list stops meaning anything and it must not be allowed to constrain anyone.
 */
export function settleEntries(
  doc: PMNode,
  entries: TrailEntry[] | null | undefined,
  blocks: BlockRef[] | null | undefined,
  retryAdrift: boolean,
): TrailEntry[] {
  const list = entries || [];
  // What each entry needs: keep its place, be found by its text, be resolved
  // with the other block-emptying entries, or be left alone.
  const need = list.map((e): 'keep' | 'find' | 'set' | 'hold' => {
    if (e.anchored && verifyEntry(doc, e)) {
      return 'keep';
    }
    if (e.anchored || retryAdrift) {
      return contextless(e) ? 'set' : 'find';
    }
    return 'hold';
  });
  const held: number[] = [];
  for (let i = 0; i < list.length; i += 1) {
    const e = list[i];
    // A verified contextless entry's `from` IS its block's content start —
    // verifyEntry only passes it where that block is empty.
    if (need[i] === 'keep' && contextless(e) && e.anchored) {
      held.push(e.from);
    }
  }
  const asked: TrailEntry[] = [];
  for (let i = 0; i < list.length; i += 1) {
    if (need[i] === 'set') {
      asked.push(list[i]);
    }
  }
  const placed = placeEmptyBlocks(doc, asked, held);

  const out: TrailEntry[] = [];
  let next = 0;
  for (let i = 0; i < list.length; i += 1) {
    let a = list[i];
    if (need[i] === 'keep' && a.anchored) {
      a = { ...a, ...contextOf(doc, a.from, a.to) };
    } else if (need[i] === 'set' || need[i] === 'find') {
      const hit = need[i] === 'set' ? placed[next] : reanchor(doc, a, blocks);
      if (need[i] === 'set') {
        next += 1;
      }
      if (hit) {
        // AND THE REACH IS THE FOUND RANGE, not whatever it was before. An
        // entry re-found by its own text has been placed by that text and
        // nothing else, so the only region it can honestly claim is the one it
        // was found at — carrying a stale reach across a rebuild would let a
        // later keystroke join an entry through coordinates the search has
        // already replaced.
        a = {
          ...a,
          from: hit.from,
          to: hit.to,
          reachFrom: hit.from,
          reachTo: hit.to,
          anchored: true,
        };
        a = { ...a, ...contextOf(doc, a.from, a.to) };
      } else {
        // The log keeps the stored context — it is the entry's only hope of
        // ever re-anchoring, and the only "where" the log can still say.
        a = {
          ...a,
          anchored: false,
          from: null,
          to: null,
          reachFrom: null,
          reachTo: null,
        };
      }
    }
    out.push({ ...a, placed: a.anchored === true });
  }
  out.sort((x, y) => {
    const ax = x.anchored ? x.from : Infinity;
    const ay = y.anchored ? y.from : Infinity;
    return ax - ay || 0;
  });
  return out;
}

// PARTITIONENTRIES IS DELETED, AND ITS SUBJECT IS WHAT WENT.
//
// It split the trail for the changed region: anchored entries decorated the
// prose AND listed in the log, adrift ones listed in the log ONLY. The log is
// gone — the trail is an outgoing message to the agent and not a history, so
// there is nowhere for a row to render — and with it goes the only distinction
// that function drew. There is no second surface for an entry to appear on and
// therefore no partition to assert.
//
// THE FACT IT SORTED ON IS NOT LOST, and that is what makes this a deletion
// rather than a removal. `anchored` is still what decides whether an entry gets
// a GHOST (see trailDecorations, which reads it directly), `verifyEntry` and
// `reanchor` still decide it, and `review.Change.Placed` still carries it to the
// server — where `settleEntries`' order rule reads it. What an adrift entry has
// lost is a row saying "log only — its place has moved on"; what it has instead
// is no ghost, which is the same claim made by the surface that is left.

// --- the decorations -------------------------------------------------------

// ghostEl builds the deletion ghost: the removed text, struck in del-red, at
// the spot it left. aria-hidden and NOT content — a widget decoration is
// ProseMirror's own "this is not in the document", which is what keeps the
// projected .md clean by construction (the note.ts pattern). Its size is its
// natural inline size and nothing else; cards and anchors already tolerate
// content height changes through the ResizeObserver.
function ghostEl(old: string, tight: boolean): () => HTMLSpanElement {
  return () => {
    const el = document.createElement('span');
    el.className = tight
      ? 'gly-trail-ghost gly-trail-tight'
      : 'gly-trail-ghost';
    el.setAttribute('aria-hidden', 'true');
    el.textContent = old;
    return el;
  };
}

// THE PUNCTUATION THAT CLOSES A CLAUSE, and it is a CHARACTER TEST rather than
// a class of any kind, because that is the whole of what the question is.
//
// The ghost carries its own right-hand gap (see `.gly-trail-ghost` in
// editor.css) so that a deletion and the word that replaced it read as two
// words. Where the reviewer struck a trailing clause, what is on the ghost's
// right is not a word but the sentence's own full stop — and 0.3em there
// renders `is told, .`, a space before the punctuation, which is the same
// "reads as a typo in the prose" the one-sided rule exists to avoid, arriving
// from the other side.
//
// CLOSING PUNCTUATION ONLY. An opening bracket or quote to the ghost's right is
// the start of the NEXT thing and wants the separation exactly as a word does;
// it is only marks that BELONG to what came before — the clause the ghost is —
// that must sit tight against it. The list is spelled as characters and covers
// the typographic forms as well as the ASCII ones, because a document written
// in a word processor is full of the former and a rule that only knew `'` and
// `"` would be right on half the documents it meets.
const CLOSING_PUNCTUATION = '.,;:!?)]}…»”’\'"';

function tightAfter(doc: PMNode, to: number): boolean {
  // ONE CHARACTER, READ FROM THE LIVE DOCUMENT. The entry's stored anchors are
  // a snapshot and the prose has moved on (the reason `trailDecorations` reads
  // its context from `doc` at all), and `to` is already the widened display
  // end, so this is the character the reader will actually see beside the
  // ghost. `textBetween` over an empty or out-of-range span answers '' rather
  // than throwing, which is the honest answer for an entry at the very end of
  // the document: nothing follows, so there is no gap to eat.
  if (to >= doc.content.size) {
    return false;
  }
  const next = doc.textBetween(to, Math.min(to + 1, doc.content.size), '', '');
  return next.length === 1 && CLOSING_PUNCTUATION.includes(next);
}

export function trailDecorations(
  doc: PMNode,
  entries: TrailEntry[] | null | undefined,
): DecorationSet {
  const decos: Decoration[] = [];
  for (const e of entries || []) {
    if (!e.anchored) {
      continue;
    }
    // DISPLAY EXPANDS TO THE WORD; STORAGE STAYS MINIMAL (expandToWord). The
    // context is read from the LIVE document rather than the entry's stored
    // anchors, because those are a snapshot and the prose has moved on. The
    // POSITIONS widen by exactly what the text did — a ghost left at the
    // minimal start renders "t" + ghost("teh") + "he", which is the alien
    // reading this cut exists to end.
    const show = expandToWord(e, contextOf(doc, e.from, e.to));
    const from = e.from - show.lead.length;
    const to = e.to + show.tail.length;
    // The highlight is gated on the STORED new: an entry that inserted
    // nothing gets no ins-teal, however wide its word is. Its ghost still
    // carries the word, so the deletion reads as a word replaced by a word.
    if (e.new && to > from) {
      decos.push(Decoration.inline(from, to, { class: 'gly-trail-ins' }));
    }
    if (show.old) {
      const tight = tightAfter(doc, to);
      decos.push(
        Decoration.widget(from, ghostEl(show.old, tight), {
          side: -1,
          // The ghost is a readout, not a place the caret can live.
          ignoreSelection: true,
          // The key carries the TEXT as well as the id: two widgets with the
          // same key are the same widget to ProseMirror and the DOM is not
          // rebuilt — so an id alone leaves a stale ghost standing whenever
          // the entry's word grows under the reviewer's next keystroke.
          // The TIGHTNESS rides in the key for the same reason the text does:
          // two widgets with one key are one widget to ProseMirror and the DOM
          // is not rebuilt, so a ghost whose neighbour changed from a word to a
          // full stop under the reviewer's next keystroke would keep the class
          // it was born with.
          key: `gly-trail-${e.id}:${tight ? 't' : 'w'}:${show.old}`,
        }),
      );
    }
  }
  return DecorationSet.create(doc, decos);
}

// serializeEntries is the wire shape POST /_galley/trail takes — and the
// sidecar's review.Change, field for field. Positions stay home: the sidecar
// records context, never coordinates, because coordinates die with the
// session.
export function serializeEntries(
  entries: TrailEntry[] | null | undefined,
): SerializedTrailEntry[] {
  return (entries || []).map((e) => ({
    old: e.old || '',
    new: e.new || '',
    blockKey: e.blockKey || '',
    prefix: e.prefix || '',
    suffix: e.suffix || '',
    // The neighbouring blocks, and they are CARRIED rather than left in the
    // session because they are the whole of a block-emptying deletion's
    // identity (emptyBlockAnchor). An entry that reloads without them can
    // never be placed again — honestly, since it would have nothing to be
    // placed BY, but a record that could have kept its ghost across a reload
    // and did not is a loss the sidecar can just as easily prevent.
    //
    // NULL TRAVELS AS NULL. '' is "the block that side is empty" and null is
    // "there is no block that side" — a serializer that flattened the second
    // into the first would put the conflation neighboursAt was fixed for back
    // on the wire, where it would outlive the session in the sidecar. Go holds
    // these as *string for the same reason (review.Change).
    before: side(e.before),
    after: side(e.after),
    // WHAT THE SERVER CANNOT RE-DERIVE. Everything else on this line is text
    // the server could in principle re-read from the document; this is the one
    // fact that exists only at the instant the step was taken, because the edit
    // itself removes the mark it is about. It decides whether the ledger
    // records this as the reviewer rewriting a proposal — and WHOSE — or as the
    // reviewer working on their own prose. See onProposalAt and, on the Go
    // side, review.Change.Proposal.
    //
    // NULL TRAVELS AS NULL here too, for the same reason it does above: '' is a
    // proposal whose mark carries no author and null is no proposal at all, and
    // Go holds it as a *string to keep them apart.
    proposal: side(e.proposal),
    // WHETHER THIS ENTRY HAD A PLACE WHEN THE LIST WAS WRITTEN — the fact that
    // makes the list's ORDER usable. settleEntries sorts by position, so the
    // trail is in document order, but it sorts entries with no place to the
    // END where their position means nothing. placeEmptyBlocks resolves the
    // block-emptying entries as a set and leans on that order to tell two
    // entries with identical evidence apart; it may only do so for entries the
    // list can speak for, and this is how it knows which those are.
    //
    // A LEGACY ROW READS null AND IS THEREFORE UNORDERED, which is the only
    // honest reading: nothing in a record written before this field existed
    // says where in the prose it sat, and an ambiguity the order would have
    // settled refuses instead. Go holds it as a *bool for exactly the
    // Before/After reason — false is a real answer and a different one from
    // absent.
    placed: flag(e.placed),
    at: e.at || '',
  }));
}

// loadedEntry is serializeEntries' inverse: a sidecar change becomes an entry
// with no position yet — settleEntries' retryAdrift pass is what anchors it.
export function loadedEntry(
  c: SerializedTrailEntry | null | undefined,
): TrailEntry {
  return {
    id: nextTrailId(),
    old: (c && c.old) || '',
    new: (c && c.new) || '',
    blockKey: (c && c.blockKey) || '',
    prefix: (c && c.prefix) || '',
    suffix: (c && c.suffix) || '',
    // A sidecar record that carries no side at all — a legacy row, or one
    // whose block stood at the document's edge — reads as null, which is what
    // emptyBlockAnchor compares an absent neighbour against.
    before: side(c && c.before),
    after: side(c && c.after),
    // Absent on a record written before the field existed, which reads null:
    // "we do not know that this rewrote a proposal" is the honest default, and
    // the alternative would invent rewrites out of old rows.
    proposal: side(c && c.proposal),
    // Absent on a legacy row, which reads null: "this list cannot speak for
    // where this entry sat", and the set rule leaves such an entry out of the
    // order it resolves by. See serializeEntries, and placeEmptyBlocks for what
    // that costs and why it is the right cost.
    placed: flag(c && c.placed),
    at: (c && c.at) || '',
    anchored: false,
    from: null,
    to: null,
  };
}

// mergeAdopt is the FIRST adoption's shape: the sidecar's list and whatever
// the reviewer already typed before the first pending payload landed, server
// first, deduped by content-and-instant-and-PLACE. Skipping the server list
// whenever a local entry existed — the first implementation — let one early
// keystroke clobber a whole previous session's record.
//
// PLACE IS PART OF THE KEY BECAUSE `at` IS STAMPED ONCE PER TRANSACTION. Every
// entry a single transaction records carries the same instant (the plugin's
// `const at`), so a replace-all — or any multi-step transaction correcting the
// same misspelling in two places — produces entries whose old and new are
// byte-identical, whose instant is identical, and whose only difference is
// WHERE they are. Keyed on content-and-instant alone those read as one record
// and all but the first were dropped at the first adoption: an edit the
// reviewer's own hand made, gone from the trail with nothing left to show it
// ever existed. Same shape as the ordinals rule one layer out — text and a
// timestamp are not identity, and two edits are two edits.
//
// SO IT IS A PAIRING AND NOT A KEY — one for one, and the local copy wins its
// slot. The sidecar records context and never coordinates (serializeEntries),
// so every loaded entry arrives UNPLACED: put the place in the key and a placed
// local entry can never match the record of itself, and the trail carries that
// edit twice from the adoption on — one deletion ghosted twice on one block,
// now that a block-emptying entry re-anchors on its neighbours.
//
// THAT WAS DEFENCE, AND IT WAS WRITTEN DOWN AS DEFENCE RATHER THAN AS A BUG
// REPORT. No gesture was known to reach it: `App.syncTrail` refused to POST
// while `trailLoaded` was false and `adoptTrail` set that flag once and never
// cleared it, so the merge ran EXACTLY ONCE per page load, and the locals it
// met were keystrokes made between the editor opening and the first pending
// payload landing — entries the server had never seen. For one to collide with
// a sidecar record it would have had to share that record's text AND its
// millisecond, and the millisecond was this page's own clock. The pairing was
// what kept that true if the populations ever overlapped (an adoption after a
// POST, a second merge path); it was never the cure for a symptom anyone had
// produced.
//
// `App.syncTrail`, `adoptTrail` and the POST loop they describe are DELETED
// (see the note in entry.ts where they were): the trail is an outgoing
// message to the agent now, not a history with a server copy to adopt from.
// Nothing in this file's production callers still sets `trailPluginKey`'s
// `{ load: … }` meta that would run `mergeAdopt` below — the pairing logic
// is kept as the record of a defence that was real while the adoption path
// existed, not as a claim that the path still runs.
//
// Each loaded record may therefore be claimed by AT MOST ONE local entry of
// the same content-and-instant, and the local entry takes the slot because it
// is the copy that knows where it is. Two entries of one transaction still
// find two records and stay two; a local edit the sidecar has no record of
// runs the pairing out and is appended, which is the case the merge exists
// for.
//
// AND THE INSTANT IS COMPARED AS AN INSTANT, NEVER AS THE STRING IT ARRIVED
// AS, because the two sides do not spell it the same way. The browser mints
// `new Date().toISOString()`, which always writes three fraction digits; Go
// holds it as a `time.Time` and re-marshals RFC3339*Nano*, which TRIMS
// trailing zeros. Measured through the round trip: `2026-08-15T10:00:00.100Z`
// comes back `…:00.1Z`, and `…:00.000Z` comes back `…:00Z`. Keyed on the raw
// string, every entry whose millisecond ends in a zero — roughly one in ten,
// and EVERY entry landing on an exact second — could never pair with the
// record of itself, which is the one thing this function is for. `Date.parse`
// normalises both spellings to the same number; anything that is not a
// parseable instant (a test's `T1`, an empty `at`) is kept as itself, so it
// still pairs with its own copy and never with a different one.
export function mergeAdopt(
  loaded: TrailEntry[] | null | undefined,
  local: TrailEntry[] | null | undefined,
): TrailEntry[] {
  // The instant, canonical: see above — the wire spelling is not stable.
  const instant = (v: string): string => {
    const ms = Date.parse(v);
    return Number.isNaN(ms) ? `raw:${v}` : String(ms);
  };
  const sig = (e: TrailEntry): string =>
    `${e.old}\u0000${e.new}\u0000${instant(e.at)}`;
  const out = (loaded || []).slice();
  // The unclaimed slots for each signature, in document order, so a pairing
  // takes them one at a time rather than by chance.
  const slots = new Map<string, number[]>();
  out.forEach((e, i) => {
    const k = sig(e);
    if (!slots.has(k)) {
      slots.set(k, []);
    }
    const bucket = slots.get(k);
    if (bucket) {
      bucket.push(i);
    }
  });
  for (const e of local || []) {
    const free = slots.get(sig(e));
    const idx = free && free.length ? free.shift() : undefined;
    if (idx !== undefined) {
      out[idx] = e;
    } else {
      // APPENDED, WHICH IS THE ONE PLACE THE LIST IS NOT IN DOCUMENT ORDER.
      // placeEmptyBlocks reads the order off the list and trusts `placed` to
      // say whose position speaks; a local entry with no record of itself
      // lands at the END carrying placed: true, so it would read as ordered
      // while sitting somewhere its position means nothing. Not reachable
      // today — such an entry is anchored and verifies, so settleEntries needs
      // 'keep' for it and never asks the set rule — and the very next settle
      // sorts the whole list by position anyway. If a future merge path can
      // append an ADRIFT local entry, this is where the order breaks and the
      // append has to clear `placed` on its way in.
      out.push(e);
    }
  }
  return out;
}

// --- the plugin ------------------------------------------------------------

function mapEntry(e: TrailEntry, map: Mappable): TrailEntry {
  if (!e.anchored) {
    return e;
  }
  const from = map.map(e.from, -1);
  const to = e.to === e.from ? from : map.map(e.to, 1);
  const out = { ...e, from, to: Math.max(from, to) };
  // The reach travels with the range it contains, or the gesture it is the
  // shape of is broken by the next step that maps positions — a paste, an
  // autocorrect, anything with more than one step in its transaction.
  if (
    typeof e.reachFrom === 'number' &&
    Number.isFinite(e.reachFrom) &&
    typeof e.reachTo === 'number' &&
    Number.isFinite(e.reachTo)
  ) {
    const rf = map.map(e.reachFrom, -1);
    const rt = e.reachTo === e.reachFrom ? rf : map.map(e.reachTo, 1);
    out.reachFrom = Math.min(rf, out.from);
    out.reachTo = Math.max(rt, out.to);
  }
  return out;
}

/**
 * trailPlugin observes the document and keeps the trail: entries in plugin
 * state, ghosts and highlights as its DecorationSet.
 *
 * `getBlocks` is injected the way the suggestion plugin's onRefused is — the
 * plugin is built while the Editor is being constructed and the App that
 * holds the block list does not exist until after that returns.
 */
export function trailPlugin(
  getBlocks: (() => BlockRef[]) | null | undefined,
): Plugin<TrailPluginState> {
  const blocks = () => (getBlocks ? getBlocks() : []) || [];
  return new Plugin<TrailPluginState>({
    key: trailPluginKey,
    state: {
      init: () => ({ entries: [], decos: DecorationSet.empty, gen: 0 }),
      apply(tr, prev) {
        // `Transaction.getMeta` is typed `any` by prosemirror-state's own
        // declaration; `meta` is read once and every use below narrows it
        // with a real check (`in`, `Array.isArray`) rather than trusting the
        // caller's shape.
        const meta = tr.getMeta(trailPluginKey);
        if (
          meta &&
          typeof meta === 'object' &&
          'load' in meta &&
          Array.isArray(meta.load)
        ) {
          // merge: the first adoption folds the sidecar's list in AROUND what
          // the reviewer already typed (server first, deduped). Without the
          // flag — an epoch re-adoption, a 409 recovery — the server's list
          // REPLACES: the trail was retired or moved on elsewhere, and the
          // server is the authority on a record this page's copy has lost.
          const loaded = meta.load.map(loadedEntry);
          const base =
            'merge' in meta && meta.merge
              ? mergeAdopt(loaded, prev.entries)
              : loaded;
          const entries = settleEntries(tr.doc, base, blocks(), true);
          return {
            entries,
            decos: trailDecorations(tr.doc, entries),
            gen: prev.gen + 1,
          };
        }
        if (meta && typeof meta === 'object' && 'clear' in meta && meta.clear) {
          return { entries: [], decos: DecorationSet.empty, gen: prev.gen + 1 };
        }
        if (!tr.docChanged) {
          return prev;
        }
        const ys = tr.getMeta(ySyncPluginKey);
        let entries = prev.entries;
        if (ys) {
          // Remote, or undo/redo — never recorded. An undo/redo first gets
          // its chance to RETRACT the entries it exactly reverses — paired by
          // reconstruction against the TRANSACTION'S DIFF, because
          // y-prosemirror renders an undo as one whole-region replace (see
          // diffOf and retractReverted). Then everything maps and re-settles,
          // and a rebuild's survivors re-anchor from context exactly as a
          // reload's do.
          const undoing = !!ys.isUndoRedoOperation;
          if (undoing && tr.docs.length) {
            const undo = diffOf(tr.docs[0], tr.doc);
            if (undo) {
              entries = retractReverted(entries, undo).entries;
            }
          }
          const wasAnchored = new Set(
            entries.filter((e) => e.anchored).map((e) => e.id),
          );
          for (let i = 0; i < tr.steps.length; i += 1) {
            const map = tr.steps[i].getMap();
            entries = entries.map((e) => mapEntry(e, map));
          }
          entries = settleEntries(tr.doc, entries, blocks(), true);
          if (undoing) {
            // THE NO-PHANTOM NET. An entry that loses its anchor IN THE UNDO
            // ITSELF is, overwhelmingly, an edit the undo just reverted with
            // pairing the reconstruction could not prove (a batched Cmd-Z,
            // repeated text). A phantom carried forward as "decided fact" is
            // the one failure the trail must never produce, so an entry the
            // reviewer's own undo set adrift is dropped — entries that were
            // ALREADY adrift (a reload's log-only records) are not touched,
            // and a remote rebuild never enters this branch.
            entries = entries.filter(
              (e) => e.anchored || !wasAnchored.has(e.id),
            );
          }
          return {
            entries,
            decos: trailDecorations(tr.doc, entries),
            gen: prev.gen + 1,
          };
        }
        // The reviewer's own hand.
        const at = new Date().toISOString();
        for (let i = 0; i < tr.steps.length; i += 1) {
          const step = tr.steps[i];
          const rec = recordOf(tr.docs[i], step);
          if (rec) {
            rec.blockKey = blockKeyAt(tr.docs[i], rec.from, blocks());
            const { keep, merged } = applyRecord(entries, tr.docs[i], rec, at);
            const map = step.getMap();
            entries = keep.map((e) => mapEntry(e, map));
            if (merged) {
              // THE CONTEXT IS READ HERE, against the document this step just
              // produced. applyRecord works from the doc the step applied TO
              // and cannot see the result, so what it hands back carries EMPTY
              // affixes — and empty affixes are the signature of the one
              // honest contextless entry there is, a deletion that emptied its
              // own block (verifyEntry and emptyBlockAnchor both key on it).
              // Left to the settle pass below, a fresh entry survived
              // verification only VACUOUSLY — '' === '' at every position of
              // every textblock — and every rule about a contextless entry had
              // to spare that case before it could say anything. Filled here,
              // "no context" means exactly one thing again.
              const after = tr.docs[i + 1] || tr.doc;
              if (merged.anchored) {
                entries.push({
                  ...merged,
                  ...contextOf(after, merged.from, merged.to),
                });
              }
            }
          } else {
            const map = step.getMap();
            entries = entries.map((e) => mapEntry(e, map));
          }
        }
        entries = settleEntries(tr.doc, entries, blocks(), false);
        return {
          entries,
          decos: trailDecorations(tr.doc, entries),
          gen: prev.gen + 1,
        };
      },
    },
    props: {
      decorations(state) {
        // getState is only ever called on this plugin's OWN key from within
        // its own decorations prop, so the state ProseMirror hands back
        // always includes it — this fallback is the honest, inert answer for
        // the shape TypeScript otherwise has no way to know is unreachable.
        const st = trailPluginKey.getState(state);
        return st ? st.decos : DecorationSet.empty;
      },
    },
  });
}
