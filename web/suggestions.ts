// suggestions.ts — the browser half of galley's suggestion grammar: the
// guard over literal regions, the machinery that locates an AGENT's proposal
// in the document, and the bubble that says what a mark is.
//
// Two pieces, one file because they are two halves of one idea:
//
//   suggestionPlugin  refuses edits to literal regions (fences, tables,
//                     inline code) with a reason the UI can show. It no
//                     longer converts reviewer keystrokes into anything: the
//                     reviewer's edits apply DIRECTLY, the way every editor
//                     edits a document (the reviewer's-hand cut,
//                     docs/superpowers/specs/2026-08-14-the-reviewers-hand.md).
//                     Only the AGENT's proposals are tracked, and they arrive
//                     server-stamped through the websocket.
//   SuggestionUI      turns a click on an ins/del/highlight mark into a
//                     bubble that says what the mark is. It offers no
//                     verdict: accept and reject are gone (see render).
//
// The second half never touches the document. Every mark is written by the
// server, which rebuilds the whole fragment, and the change arrives here over
// the websocket like any other edit.
//
// WHAT USED TO LIVE HERE, and why it is gone rather than hidden. The plugin
// converted every reviewer keystroke into a tracked mark — ins for typing,
// del-re-insertion for deleting — attributed 'court' and stamped with a
// browser-minted run (`newRun`, the stamper that was twice claimed and absent;
// web/typing.mjs exists because of it). The reviewer's-hand decision retired
// that whole layer: a change the reviewer makes deliberately is a change the
// reviewer approved, so pending is purely the agent's proposals and nothing
// the reviewer does to the document ever waits on the reviewer. typing.mjs
// was kept and repurposed to assert the new contract — a real keystroke
// produces NO mark, clean text, and an unchanged pending count.

import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Transaction } from '@tiptap/pm/state';
import { ReplaceStep, ReplaceAroundStep } from '@tiptap/pm/transform';
import type { Step } from '@tiptap/pm/transform';
import type { Node as PMNode, Mark, ResolvedPos } from '@tiptap/pm/model';
import type { Editor } from '@tiptap/core';
import { ySyncPluginKey } from 'y-prosemirror';
// THE BROWSER'S SINGLE READER OF THE SERVER'S DECIDABILITY ANSWER. rail.ts
// imports nothing, so this is not a cycle — and asking it rather than spelling
// `kind !== 'comment'` here is the whole point: this file was the surface that
// spelled NOTHING, which is the seventh silent site CLAUDE.md predicts.

const suggestionPluginKey = new PluginKey('galleySuggestions');

// --- a code fence is READ-ONLY ---
//
// PHASE-1 POLICY, in one place because leaving it implicit produced three
// separate bugs in the same afternoon. A fenced code block cannot be edited in
// this editor at all: not typed into, not deleted from, not joined with the
// block above or below it.
//
// The reason is not squeamishness about code, it is that there is nowhere to
// RECORD such an edit. Fenced content is literal and galley must never rewrite
// one (CLAUDE.md says so at length); ProseMirror will not carry a mark inside a
// `code: true` node, so `ins`/`del` have no representation there; and per-line
// code suggestions are a phase-3 mechanism with a syntax of their own. Every
// path that edited a fence anyway wrote something nobody could review:
//
//   - Backspace at position 0 of a fence is a cross-block JOIN, which this
//     plugin's documented limitation applies as a real, untracked deletion. The
//     codeBlock node vanished, its text was lifted into the paragraph before
//     it, and the next projection wrote CriticMarkup litter into the author's
//     .md. Destructive, unrecoverable except from git, recorded nowhere.
//   - typing inside a fence reached the file as real text with nothing in
//     /_galley/pending: `tr.addMark` silently does nothing where the mark is
//     disallowed, so the fence was half-editable — insertions applied for real
//     and unreviewably, deletions silently no-oped.
//   - Backspace inside a fence re-inserted what it removed, unmarked. Nothing
//     was lost, but the caret walked one position left per press while the
//     document never changed.
//
// So the honest phase-1 behaviour is refusal, and refusal that SAYS SO — see
// the plugin's onRefused. Nothing here touches the DOM or the selection: only
// transactions that would CHANGE a fence are stopped, so reading, selecting,
// copying and scrolling through one all still work exactly as before.
//
// THE REVIEWER'S-HAND CUT KEPT THIS GUARD DELIBERATELY. The "nowhere to
// RECORD it" half of the rationale died with reviewer tracking — nothing of
// the reviewer's is recorded now — but the other half stands on its own: a
// fence is literal text galley never rewrites, the destroyed-fence join above
// was a real loss, and per-line code editing is a phase-3 mechanism with a
// syntax of its own. Lifting the refusal is a product decision to be made on
// purpose, not a side effect of the cut.

// The two reasons a change is refused, spelled out for whoever has to read them
// on screen. Exported so the UI cannot drift into its own wording.
export const FENCE_INSIDE =
  'a code fence is literal text — galley never rewrites one, so it is read-only here';
export const FENCE_JOIN =
  'that would merge a code fence with the block beside it — a fence is literal ' +
  'text galley never rewrites, so it is read-only here';

// The muted second line: what you CAN still do. Refusing without this is the
// difference between a rule and a wall.
export const FENCE_HINT =
  'select and copy still work — edit fenced code in your own editor';

// --- a table is READ-ONLY too ---
//
// The same cut, for the same reason, in the same voice. Phase 1 has nowhere to
// record a change to a CELL: suggest.List reads Block.Inlines and a cell's
// inlines sit a level below anything the suggestion pipeline reaches, and the
// column count and the delimiter row are STRUCTURE, which phase 1 does not
// track at all. Cell-level tracked changes are phase 3.
export const TABLE_INSIDE =
  'a table is read-only here — galley renders it, and phase 1 has nowhere to record a change to a cell';
export const TABLE_JOIN =
  'that would merge a table with the block beside it — a table is read-only here, so galley never rewrites one';
export const TABLE_HINT =
  'select and copy still work — edit the table in your own editor';

// --- front matter is literal, and it is METADATA rather than prose ---
//
// THE FOURTH LITERAL THING, AND THE ONE WITH THE MOST TO LOSE. A file's
// "---"/"+++" block is carried through galley verbatim — the parser scans it
// rather than decoding it, precisely so a key order, a comment, an anchor and a
// block scalar's indentation come back as the author wrote them. There is
// nothing in Block.Inlines to hang a mark on, no card to draw for it, and a
// YAML document with one character changed is a build that fails rather than a
// sentence that reads oddly.
//
// So it RENDERS, and every edit to it is refused — the same cut as the fence
// and the table, in the same voice. It is asked BEFORE the fence question in
// literalRegion: the node carries `code: true` (it holds literal text, and that
// is the schema's own word for it), so isFence would answer yes and the
// reviewer would be told a metadata block was a code fence.
export const FRONT_MATTER_INSIDE =
  'front matter is metadata, not prose — galley carries it through exactly as written, so it is read-only here';
export const FRONT_MATTER_JOIN =
  'that would merge the front matter with the document below it — galley never rewrites those delimiters';
export const FRONT_MATTER_HINT =
  'select and copy still work — edit the front matter in your own editor';

// --- display math is literal too, and for front matter's reason ---
//
// `$$ … $$` is carried verbatim (docmodel.MathBlock) because the content is
// TeX. It is not markdown: there is nothing in Block.Inlines to hang a mark on,
// no card to draw for it, and a formula with one character changed is a
// renderer that fails rather than a sentence that reads oddly. It is asked
// BEFORE the fence question for front matter's reason too — the node carries
// `code: true`, so isFence would answer yes and the reviewer would be told
// their equation was a code fence.
export const MATH_INSIDE =
  'display math is TeX, not prose — galley carries it through exactly as written, so it is read-only here';
export const MATH_JOIN =
  'that would merge the math block with the document around it — galley never rewrites those delimiters';
export const MATH_HINT =
  'select and copy still work — edit the formula in your own editor';

// --- inline `code` is literal too, and it is a MARK, not a region ---
//
// THE SAME RULE ONE LEVEL DOWN, WHERE THE MACHINERY ABOVE CANNOT SEE IT.
// literalRegion asks a NODE question and literalHit walks nodesBetween; a
// fence and a table are nodes, so both are found. Inline `code` is a MARK on
// text inside a paragraph, so it never appears in that walk — and StarterKit's
// code mark is `excludes: '_'`, which makes ProseMirror's Mark.addToSet refuse
// to put `ins` or `del` on code text at all. The suggestion mark is not split
// there; it is never applied. So the fence's second and third breaches were
// live here, verbatim, with no guard:
//
//   - typing inside a code span reached the file with nothing in
//     /_galley/pending — under tracked typing, an edit the contract never saw,
//     projected into the author's .md;
//   - deleting inside one was a silent no-op: the text removed and re-inserted
//     unmarked, the document byte-identical, only the caret moved.
//
// (Both breaches are described in the tracked-typing world that found them;
// the guard outlived that world because inline code is literal text galley
// never rewrites, the same standing reason the fence keeps.)
//
// ONLY `inside`, AND THE ASYMMETRY IS THE WHOLE DESIGN. A fence refuses a JOIN
// as well, because a join destroys a fence. A mark has no boundary to be
// merged across, and an edit running from prose THROUGH a code span into prose
// is the reviewer deleting a sentence that happens to contain a code word:
// "The `retryBudget` value controls". That must keep working. Refusing it would
// mean a reviewer cannot delete a phrase because of one word inside it — an
// obstruction they meet constantly, and a loud obstruction met constantly is a
// worse product than a silent bug met rarely.
//
// WHAT CROSSING USED TO COST, closed by the reviewer's-hand cut rather than
// by the schema decision everyone expected. Under tracked typing the code
// word's own deletion was recorded nowhere (`excludes: '_'` kept `del` off
// it), so accepting the card left the word in the file — Gap 6, symptom 1.
// A direct deletion has no card and no re-insert: the crossing deletes the
// code word with its phrase, which is what the reviewer asked for. typing.mjs
// asserts it from a real keyboard.
export const CODE_INSIDE =
  'inline code is literal text — galley never rewrites it, so it is read-only here';

// The muted second line, and it has to be TRUE or it is worse than silence.
// Both halves are checked in probe.mjs: prose either side of a code span still
// edits, and a strike running THROUGH one is accepted.
export const CODE_HINT =
  'the prose around it still edits — and a phrase containing it can still be struck';

// The three literal things, and the sentences each one refuses in. Spelled
// once: the transaction filter and the Comment guard both render whatever
// literalHit hands back, so a fourth is one entry here.
const LITERAL: Record<
  NodeLiteralKind,
  { inside: string; join: string; hint: string }
> & { code: { inside: string; hint: string } } = {
  fence: { inside: FENCE_INSIDE, join: FENCE_JOIN, hint: FENCE_HINT },
  table: { inside: TABLE_INSIDE, join: TABLE_JOIN, hint: TABLE_HINT },
  frontMatter: {
    inside: FRONT_MATTER_INSIDE,
    join: FRONT_MATTER_JOIN,
    hint: FRONT_MATTER_HINT,
  },
  mathBlock: { inside: MATH_INSIDE, join: MATH_JOIN, hint: MATH_HINT },
  // NO `join` FOR CODE, and its absence is the policy rather than an omission.
  // A mark has no boundary to be merged across, codeRun only ever answers
  // `inside`, and a sentence nothing can reach is a sentence that drifts.
  code: { inside: CODE_INSIDE, hint: CODE_HINT },
};

// THE GRIP IS THE WAY IN, where there is one. A top-level fence, table,
// equation or front matter carries a grip beside it that files an
// instruction on the whole block, so the hint ends by saying so. A NESTED one
// has no grip — the server addresses top-level blocks only — and the clause
// there would send the reviewer looking for a control that is not on the
// page, so literalHit adds it by position, never by kind. The grip is named by
// where it is and not by its face, which is `+` on a block with no
// instructions and their count on a block with some.
const GRIP_CLAUSE =
  ' — or press the button to its left to leave an instruction on ';

// What the grip's instruction is on, as the clause names it. A mermaid fence
// is a diagram on the page, and its grip calls it one.
function wholeBlock(what: NodeLiteralKind, node: PMNode): string {
  switch (what) {
    case 'table':
      return 'the whole table';
    case 'frontMatter':
      return 'the front matter';
    case 'mathBlock':
      return 'the whole equation';
    default:
      return node.attrs.language === 'mermaid'
        ? 'the whole diagram'
        : 'the whole code block';
  }
}

// isFence reports whether a node's content is literal. `code: true` is the
// schema's own word for it (codeBlock sets it); the name check is belt and
// braces for a schema that spells the flag differently.
export function isFence(node: PMNode | null | undefined): boolean {
  return (
    !!node && (node.type.spec.code === true || node.type.name === 'codeBlock')
  );
}

// isTable reports whether a node is a table. The whole table is one region —
// rows and cells are never asked about separately, because there is no edit
// to a cell that is not an edit to the table.
export function isTable(node: PMNode | null | undefined): boolean {
  return !!node && node.type.name === 'table';
}

// isCodeText reports whether a text node's content is literal because of a
// MARK it carries — isFence's twin, one level down. `code: true` on the mark
// spec is the schema's own word for it and the same flag codeBlock sets, so
// this asks the spec rather than the name for the same belt-and-braces reason
// isFence does.
export function isCodeText(node: PMNode | null | undefined): boolean {
  return (
    !!node && node.isText && node.marks.some((m) => m.type.spec.code === true)
  );
}

/**
 * codeRun reports the contiguous run of code-marked text that a change over
 * [lo, hi) falls wholly INSIDE, or null when none does.
 *
 * CONTIGUOUS, not per-inline: `code` sitting beside `code`+`bold` is two text
 * nodes and one code span on screen, and a rule that answered per-node would
 * refuse an edit inside one half and accept the identical edit inside the
 * other.
 *
 * The arithmetic, since off-by-one here is the whole game — and the two cases
 * differ, exactly as they do for a fence:
 *
 *   a removal or replacement (hi > lo) is inside when the range lies wholly
 *   within one run: [run.pos, run.end]. Deleting the whole run counts; a code
 *   span is not editable and that includes being editable to zero. ONE prose
 *   character anywhere in the range makes it a CROSSING, which is accepted —
 *   that is the reviewer deleting a phrase, not editing the code.
 *
 *   an insertion (hi === lo) is inside only when the caret is STRICTLY within,
 *   run.pos < lo < run.end. At either boundary the caret sits between prose and
 *   code, and typing in that gap is not typing in the span.
 *
 * A MEASURED RESIDUAL AT THE TRAILING BOUNDARY, recorded rather than refused.
 * ProseMirror's ResolvedPos.marks() takes the marks of the node BEFORE the
 * caret and `code` is inclusive, so text typed at exactly run.end lands
 * code-marked — new prose rendered and serialized as code. (Under tracked
 * typing this also made it unmarkable and therefore untracked; direct
 * application removed that half of the defect and left the cosmetic half.)
 * Refusing it would mean a reviewer cannot type after a code word that ends a
 * paragraph, which is precisely the wall this rule exists to avoid; the honest
 * fix is a schema decision about what `code` excludes. Pinned in probe.mjs.
 */
interface CodeRun {
  pos: number;
  end: number;
}

function codeRun(doc: PMNode, lo: number, hi: number): CodeRun | null {
  let $lo: ResolvedPos;
  try {
    $lo = doc.resolve(lo);
  } catch {
    return null;
  }
  const parent = $lo.parent;
  if (!parent.isTextblock) {
    return null;
  }
  // Only the textblock lo sits in. A range reaching into another block has an
  // `hi` past every run in this one, so containment fails on its own and no
  // separate cross-block test is needed.
  const runs: CodeRun[] = [];
  let at = $lo.start();
  let open: CodeRun | null = null;
  parent.forEach((child) => {
    const end = at + child.nodeSize;
    if (isCodeText(child)) {
      if (open) {
        open.end = end;
      } else {
        open = { pos: at, end };
        runs.push(open);
      }
    } else {
      open = null;
    }
    at = end;
  });
  for (const run of runs) {
    const inside =
      hi > lo ? lo >= run.pos && hi <= run.end : lo > run.pos && lo < run.end;
    if (inside) {
      return run;
    }
  }
  return null;
}

// isFrontMatter reports whether a node is the file's front matter block. By
// NAME and not by `code: true`, which the node also carries and which a fence
// carries too — see FRONT_MATTER_INSIDE.
export function isFrontMatter(node: PMNode | null | undefined): boolean {
  return !!node && node.type.name === 'frontMatter';
}

// isMathBlock reports whether a node is a display-math block. By NAME and not
// by `code: true`, for isFrontMatter's reason — see MATH_INSIDE.
export function isMathBlock(node: PMNode | null | undefined): boolean {
  return !!node && node.type.name === 'mathBlock';
}

// The four literal things a NODE can be — everything LITERAL below carries
// except `code`, which is a MARK and never a node (see literalHit). Kept
// separate from the mark's own kind so LITERAL[what][kind] below can be typed
// without a dynamic `join` lookup on the one entry (`code`) that has none.
type NodeLiteralKind = 'fence' | 'table' | 'frontMatter' | 'mathBlock';

// literalRegion names the read-only region a node is, or null.
export function literalRegion(
  node: PMNode | null | undefined,
): NodeLiteralKind | null {
  // BEFORE the fence question. Front matter holds literal text and so declares
  // `code: true`, which is what isFence reads — asked the other way round, a
  // reviewer editing their file's metadata would be told it was a code fence.
  if (isFrontMatter(node)) {
    return 'frontMatter';
  }
  // BEFORE the fence question too, and for the same reason: a math block
  // declares `code: true`, so isFence would claim it.
  if (isMathBlock(node)) {
    return 'mathBlock';
  }
  if (isFence(node)) {
    return 'fence';
  }
  if (isTable(node)) {
    return 'table';
  }
  return null;
}

/**
 * literalHit reports the read-only region that a change over [from, to) would
 * rewrite, or null if none would.
 *
 * ONE implementation, deliberately: the transaction filter, the comment guard
 * and anything later that offers a suggestion affordance all ask this same
 * question, and two predicates that disagree by a single position is exactly
 * the class of bug this file already carries scars from (the since-retired
 * strikeRefusal once carried its own fence test and disagreed with the
 * keyboard about a selection running from prose into a fence).
 *
 * THREE REGIONS AND ONE MARK, ONE PREDICATE. A code fence, a table, a front
 * matter block and an inline `code` span are all literal to this editor for the
 * same reason — phase 1 has nowhere to record a change to any of them — so they
 * are answered here and differ only in the sentence they say. A second
 * predicate for tables, for front matter, or for code, is exactly the shape of
 * bug the scar above is from.
 *
 * THE REGIONS ARE ASKED FIRST, and the mark only when they come up empty. A
 * fence or a table is the bigger thing and the sentence the reviewer needs;
 * code cannot be inside either of them anyway (a codeBlock's spec.marks is '').
 *
 * The arithmetic, since off-by-one here is the whole game. A fence node
 * occupies [pos, end); its literal text is [pos + 1, end - 1).
 *
 *   a removal or replacement (to > from) rewrites the fence when its range
 *   overlaps the NODE, not merely the node's text: the backspace that destroys
 *   a fence removes [pos - 1, pos + 1] and takes out no fenced character at all
 *   — it removes the boundary that makes the fence a fence. Deleting the
 *   whole node counts too; a fence is not editable and that includes being
 *   editable to zero.
 *
 *   an insertion (to === from) rewrites it only when the caret is strictly
 *   INSIDE the node — pos + 1 (the very start of the text, which is where the
 *   destructive backspace was reported from) up to end - 1. Typing at pos or
 *   at end is typing in the gap between two blocks, which is not the fence's
 *   business.
 *
 * A code hit carries no `node` (a mark is not one) and is never a `join` — see
 * codeRun for the arithmetic and for why crossing one is accepted.
 */
export interface LiteralHit {
  node: PMNode | null;
  pos: number;
  end: number;
  kind: 'inside' | 'join';
  what: NodeLiteralKind | 'code';
  reason: string;
  hint: string;
}

export function literalHit(
  doc: PMNode,
  from: number,
  to: number,
): LiteralHit | null {
  const lo = Math.min(from, to);
  const hi = Math.max(from, to);
  let hit: LiteralHit | null = null;
  doc.nodesBetween(
    Math.max(0, lo - 1),
    Math.min(doc.content.size, hi + 1),
    (node, pos, parent) => {
      if (hit) {
        return false;
      }
      const what = literalRegion(node);
      if (!what) {
        return true;
      }
      const end = pos + node.nodeSize;
      const rewrites = hi > lo ? lo < end && hi > pos : lo > pos && lo < end;
      if (rewrites) {
        // Wholly within the region's own content is an edit TO it; anything
        // reaching past either boundary is a join with what is beside it, and
        // the two deserve different sentences.
        const kind = lo >= pos + 1 && hi <= end - 1 ? 'inside' : 'join';
        const hint = LITERAL[what].hint;
        hit = {
          node,
          pos,
          end,
          kind,
          what,
          reason: LITERAL[what][kind],
          hint:
            parent === doc ? hint + GRIP_CLAUSE + wholeBlock(what, node) : hint,
        };
      }
      // A fence's children hold nothing this asks about, and a table's rows
      // and cells are the table — descending would report a cell where the
      // reviewer sees a table, and the caret positions would be the cell's.
      return false;
    },
  );
  if (hit) {
    return hit;
  }
  // The MARK question, asked only where the node walk found nothing.
  const run = codeRun(doc, lo, hi);
  if (!run) {
    return null;
  }
  return {
    // A mark is not a node, and nothing may pretend otherwise: paintRefusal
    // reads `pos` and asks the view for a DOM element there, and handing it a
    // node that does not exist is how a note ends up beside the wrong text.
    node: null,
    pos: run.pos,
    end: run.end,
    kind: 'inside',
    what: 'code',
    reason: LITERAL.code.inside,
    hint: LITERAL.code.hint,
  };
}

// changedRanges reports the ranges of the document a step applies to that the
// step would REWRITE, in that document's coordinates — a superset of what the
// step REMOVES, because an insertion removes nothing and still has to be
// caught (typing inside a fence is a zero-width range at the caret).
//
// Steps that change no content — AddMarkStep, RemoveMarkStep, AttrStep,
// DocAttrStep — report nothing and are never refused. A mark is not a rewrite,
// and ProseMirror already drops one it cannot put inside a code block.
function changedRanges(step: Step): { from: number; to: number }[] {
  if (step instanceof ReplaceStep) {
    return [{ from: step.from, to: step.to }];
  }
  if (step instanceof ReplaceAroundStep) {
    // The gap is content the step keeps and moves. Wrapping a fence in a
    // blockquote, or lifting it out of one, moves the node without touching a
    // byte of its text — structural, applied directly like every other edit,
    // and not this rule's business.
    return [
      { from: step.from, to: step.gapFrom },
      { from: step.gapTo, to: step.to },
    ];
  }
  return [];
}

// refusedLiteral reports the read-only region a whole transaction would
// rewrite, or null. Each step is asked against the document IT applied to
// (tr.docs[i]), because a later step's coordinates mean nothing in the earlier
// document.
function refusedLiteral(tr: Transaction): LiteralHit | null {
  for (let i = 0; i < tr.steps.length; i += 1) {
    const doc = tr.docs[i];
    if (!doc) {
      continue;
    }
    for (const range of changedRanges(tr.steps[i])) {
      const hit = literalHit(doc, range.from, range.to);
      if (hit) {
        return hit;
      }
    }
  }
  return null;
}

// The three marks, in one table: the schema's mark name, the kind the server
// calls it (suggest.Kind), and the class both the stylesheet and the DOM use.
// Spelled once — the bubble's class→kind lookup, the panel's kind→class
// lookup and markRuns all read it, so a fourth mark is one line here.
interface MarkKindDef {
  typeName: string;
  kind: string;
  className: string;
}

const MARK_KINDS: MarkKindDef[] = [
  { typeName: 'ins', kind: 'insert', className: 'gly-ins' },
  { typeName: 'del', kind: 'delete', className: 'gly-del' },
  { typeName: 'highlight', kind: 'comment', className: 'gly-hl' },
];

const KIND_BY_CLASS: Record<string, string> = {};
export const CLASS_BY_KIND: Record<string, string> = {};
for (const k of MARK_KINDS) {
  KIND_BY_CLASS[k.className] = k.kind;
  CLASS_BY_KIND[k.kind] = k.className;
}

const MARK_SELECTOR = MARK_KINDS.map((k) => `.${k.className}`).join(', ');

/**
 * suggestionPlugin is the guard over literal regions, and ONLY that.
 *
 * It used to be the tracked-changes converter — every local insertion picked
 * up an `ins` mark, every deletion was re-inserted `del`-marked, both
 * attributed to the reviewer and stamped with a browser-minted run, all in an
 * appendTransaction that mapped positions through the batch. The
 * reviewer's-hand cut removed that whole layer: the reviewer's edits apply
 * directly, so there is nothing to convert, nothing to attribute and nothing
 * to stamp. What remains is the one thing filtering was always right for —
 * refusing an edit whose correct outcome is that nothing happens.
 *
 * CODE FENCES, TABLES AND INLINE `code` are refused outright, in
 * filterTransaction — see literalHit. A code span is refused only where the
 * change falls wholly INSIDE it; one that runs through it is a deletion of
 * the prose around it and applies like any other. The cost of "no" alone (an
 * editor that feels broken) is paid off by onRefused, which is called with
 * the region and a reason so the UI can say why.
 *
 * Everything else — typing, deleting, pasting, IME composition, cross-block
 * deletion, formatting, structure — passes through untouched and applies the
 * way ProseMirror applies it. The agent's proposals are unaffected: they are
 * made server-side, stamped server-side, and arrive through the websocket as
 * remote changes this plugin has never touched.
 *
 * @param onRefused called when a literal edit is refused
 */
export function suggestionPlugin(
  onRefused?: (hit: LiteralHit) => void,
): Plugin {
  return new Plugin({
    key: suggestionPluginKey,

    // A code fence is read-only. Note what is NOT filtered — two exemptions,
    // each with its own reason, because an unexplained exemption in a guard is
    // a hole nobody can tell from a decision:
    //
    //   a transaction that changes no document content — a selection move, a
    //   plain click inside the fence — because reading and selecting a fence
    //   must keep working;
    //
    //   a REMOTE change (y-prosemirror's sync, which is also how undo lands).
    //   Refusing one would not undo it, it would only make this browser's
    //   document disagree with the CRDT every other peer holds — silent
    //   divergence, which is worse than the edit. The agent and the Go side
    //   have their own rule about fences, and it is enforced where they write.
    //
    //   (There used to be a third — this plugin's OWN appended transaction,
    //   exempted by a TRACKED meta. The appendTransaction is gone with the
    //   tracking layer, so nothing sets the meta and the exemption went with
    //   it.)
    filterTransaction(tr) {
      if (!tr.docChanged || tr.getMeta(ySyncPluginKey)) {
        return true;
      }
      const hit = refusedLiteral(tr);
      if (!hit) {
        return true;
      }
      if (onRefused) {
        // The UI's half of the refusal. It runs mid-dispatch, so anything it
        // does that reads layout or dispatches has to defer itself — see
        // App.refuse.
        onRefused(hit);
      }
      return false;
    },
  });
}

// --- locating a suggestion in the document ---

/**
 * markRuns reports every suggestion run in a document: a maximal stretch of
 * consecutive inlines inside ONE textblock carrying the same mark, from the
 * same author, at the same instant. That is exactly the grouping
 * internal/suggest's `span` uses, so a run here is a card there.
 *
 * One implementation serves three callers that used to want three: the
 * bubble asking "which suggestion did I just click", the panel asking "where
 * is this card's text", and the panel asking "is it on screen". Marks are
 * collected per kind rather than per node, because a run can carry two (a
 * highlighted insertion) and each is its own suggestion.
 *
 * Positions are document positions, so `from`/`to` survive being handed to
 * view.coordsAtPos or view.domAtPos.
 */
export interface SuggestionRun {
  kind: string;
  typeName: string;
  className: string;
  author: string;
  at: string;
  runId: string;
  text: string;
  from: number;
  to: number;
}

// attrString narrows a mark attribute — typed `any` by prosemirror-model's
// own Attrs — to the string it is meant to be, or '' for anything else. The
// same "no assertions, narrow with a real check" discipline rail.ts's
// keyTargetIsEditable uses, applied to the one place this file reads an
// attribute off a mark.
function attrString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

export function markRuns(doc: PMNode): SuggestionRun[] {
  const out: SuggestionRun[] = [];
  doc.descendants((node, pos) => {
    if (!node.isTextblock) {
      return true;
    }
    const start = pos + 1;
    const open = new Map<string, SuggestionRun>();
    const close = (typeName: string) => {
      const run = open.get(typeName);
      if (run) {
        out.push(run);
        open.delete(typeName);
      }
    };
    node.content.forEach((child, offset) => {
      const from = start + offset;
      const to = from + child.nodeSize;
      for (const k of MARK_KINDS) {
        const mark = markOf(child, k.typeName);
        if (!mark) {
          close(k.typeName);
          continue;
        }
        const author = attrString(mark.attrs.author);
        const at = attrString(mark.attrs.at);
        // The mark's identity joins the grouping key, mirroring the same
        // change in internal/suggest's listSpans. Without it the two sides
        // disagree about what a suggestion IS: two distinct marks by one
        // author within the same second are identical under (author, at), so
        // "{--age--}{--age--}" would be one card here and two spans there,
        // and the invariant this function's doc comment promises — a run here
        // is a card there — would quietly stop holding.
        const runId = attrString(mark.attrs.run);
        const run = open.get(k.typeName);
        // What makes this ADJACENCY rather than mere similarity is the
        // close() above: a child without this mark ends the open run, so two
        // identical marks either side of unmarked text are two suggestions.
        // (A `run.to === from` guard here would be unfalsifiable — a
        // textblock's children are contiguous by construction — and this
        // repo does not keep checks that cannot fail.)
        if (
          run &&
          run.author === author &&
          run.at === at &&
          run.runId === runId
        ) {
          run.to = to;
          run.text += child.textContent;
          continue;
        }
        close(k.typeName);
        open.set(k.typeName, {
          kind: k.kind,
          typeName: k.typeName,
          className: k.className,
          author,
          at,
          runId,
          text: child.textContent,
          from,
          to,
        });
      }
    });
    for (const k of MARK_KINDS) {
      close(k.typeName);
    }
    // Inline children are not descended into: they are the run's own parts.
    return false;
  });
  out.sort((a, b) => a.from - b.from);
  return out;
}

// sameSuggestion is the one identity test both directions use — the bubble
// mapping a clicked run onto a server suggestion, and the panel mapping a card
// back onto a run.
//
// When both sides carry a run id this is real identity, and duplicates stop
// being a problem worth managing: two suggestions that read exactly the same
// are simply two suggestions, each reachable. The id is minted server-side
// (docmodel.RunAttr) and reaches the mark through data-run.
//
// The fallback below is a genuine MATCH rather than an identity, and is what
// this function used to be entirely: (kind, text, author) are the three things
// both sides can see without an id. `at` is deliberately not among them,
// because the server reports it rounded to a second through JSON and a
// mismatch there would silently anchor nothing. It still runs for a mark that
// predates the id — one loaded from a document the current session did not
// mint — and callers must still handle ambiguity themselves (the bubble
// refuses; the panel shows the first and says so).
// AND A SUBSTITUTION IS ADDRESSABLE BY EITHER HALF, because the reviewer can
// click either half. One span in the file is one decision everywhere — but in
// the FRAGMENT it is a Del and an Ins with two runs (markdown's applyMark runs
// once per half and correctly stamps each), and the wire's `run` is the deleted
// half's, deliberately and for good reasons stated in suggest.substitutionSpan.
// So a click on the GREEN half carried a run that matched nothing: the bubble
// found zero candidates, guessed "the server has not seen this one yet", and
// offered no verbs — about `s1`, one of the three pending, whose card sat 400px
// to the right with a working ✓ accept. One edit read as three different things
// depending on where the cursor landed.
//
// `insRun` is the OTHER HALF'S ADDRESS, carried on the wire by the side that
// computed the pairing (suggest.Pending.InsRun). It is read here and nowhere
// else, and reading it is NOT re-deriving the pairing — that rule is
// markdown.planSubstitution's, it is why the file says `{~~…~>…~~}` at all, and
// a heuristic here ("a del run followed by an ins run") would be the second
// spelling this codebase has paid for twice. The decision is still addressed by
// `run`: whichever half was clicked, `decide` posts the deleted half's, exactly
// as the card does.
// A suggestion as either side of sameSuggestion sees it — the server's own
// pending entry, a MARK run read out of the document (see markRuns), or
// loosePeer's synthesised stand-in for a replace. Loosely typed on purpose:
// this file reads only the handful of fields below, and the real shape of a
// server suggestion is the App's — injected here exactly as `pending` is.
export interface SuggestionLike {
  kind: string;
  text?: string;
  old?: string;
  author?: string;
  at?: string;
  run?: string;
  runId?: string;
  insRun?: string;
  // The server's own decidability answer for this entry (suggest.Kind's
  // Decidable, carried onto the wire — see rail.ts's `decidable`). Not read
  // by anything in THIS file; added so `this.suggestions` (web/appshell.ts)
  // can be typed as `SuggestionLike[]` and still be filtered by `decidable`
  // without a second, narrower shape standing in for the same wire entry.
  decidable?: boolean;
}

export function sameSuggestion(a: SuggestionLike, b: SuggestionLike): boolean {
  const aRun = a.runId || a.run || '';
  const bRun = b.runId || b.run || '';
  if (aRun && bRun) {
    if (aRun === bRun) {
      return true;
    }
    return (
      (!!a.insRun && a.insRun === bRun) || (!!b.insRun && b.insRun === aRun)
    );
  }
  return (
    a.kind === b.kind &&
    a.text === b.text &&
    (a.author || '') === (b.author || '')
  );
}

/**
 * loosePeer is what the id-less half of sameSuggestion may be compared
 * against: the shape a server-side suggestion takes as a MARK in the fragment.
 *
 * A REPLACE STANDS OVER TWO MARKS AND READS LIKE NEITHER. A substitution —
 * "{~~old~>new~~}" — is one suggestion, kind 'replace' with text "old → new",
 * but the fragment still holds a Del and an Ins and no mark anywhere reads
 * "old → new". A straight (kind, text, author) comparison therefore finds
 * nothing, and the card goes adrift. (The id-less case was born when reviewer
 * typing minted markless suggestions; it survives for marks loaded from a
 * document no server has stamped yet, so the fallback stays.)
 *
 * It resolves to the DELETED half — the same half the Go side hands out as
 * the suggestion's run. One choice about which mark a substitution is
 * addressed by, made once and made the same on both sides.
 */
export function loosePeer(
  suggestion: SuggestionLike | null | undefined,
): SuggestionLike | null {
  if (!suggestion) {
    return null;
  }
  if (suggestion.kind !== 'replace') {
    return suggestion;
  }
  return { kind: 'delete', text: suggestion.old, author: suggestion.author };
}

// (The "striking a selection" section — strikeRefusal, strikeTransaction,
// strikeSelection and CROSS_BLOCK_STRIKE — lived here until the trail cut
// retired the toolbar's Strike button. A strike was already Backspace's own
// deletion under the reviewer's-hand contract, so the button was a second
// spelling of the keyboard, and its cross-block conservatism died with it.
// Deletion is deletion; the transaction filter above still refuses the
// literal regions, and web/trail.ts now records what the hand did.)

// --- the bubble ---

// THE HEAD NAMES THE SUGGESTION, NOT THE MARK UNDER THE CURSOR. `replace` is
// here because a substitution's two marks are one suggestion, and the bubble
// used to read `DELETION` on its left half and `INSERTION` on its right — two
// names for one decision, on a card whose own head says REPLACE. Where the
// server matched, the kind printed is the SERVER'S; the mark's own kind is the
// fallback for a mark nothing on the wire claims.
const KIND_LABEL: Record<string, string> = {
  insert: 'insertion',
  delete: 'deletion',
  replace: 'replacement',
  comment: 'comment',
};

// A thread as this file sees one — the `/_galley/pending` comment entry
// threadFor matches against, and threadCard/onReveal's opaque argument. Only
// `run` is ever read here; everything else the card component and the App
// carry belongs to their own shapes, not this one.
export interface ThreadLike {
  run?: string;
}

// What markAt reads off a clicked span, widened to the mark's whole run —
// the shape `show`, `match`, `threadFor` and `render` all pass around.
interface FoundMark {
  kind: string;
  text: string;
  author: string;
  at: string;
  run: string;
}

// match's answer: either the server's own entry for the clicked mark, or a
// sentence explaining why there is none — never both, though nothing here
// forces the exclusivity, matching what match actually returns.
interface MatchResult {
  suggestion?: SuggestionLike;
  note?: string;
}

export interface SuggestionUIOptions {
  editor: Editor;
  pending: () => SuggestionLike[];
  threads?: () => ThreadLike[];
  threadCard?: (thread: ThreadLike) => Element;
  railShown?: () => boolean;
  frame?: () => { top: number; bottom: number };
  onReveal?: (thread: ThreadLike) => void;
}

/**
 * SuggestionUI is the click-to-bubble half: click a marked span, get its
 * author and its age. The two buttons that asked the server to decide it are
 * DELETED — see render.
 *
 * IT CARRIES TWO DIFFERENT THINGS, and which one it carries is decided in
 * threadFor, once. A COMMENT highlight, below the width where the rail exists,
 * gets the whole conversation on the one thread card — because below that width
 * there is no rail to carry it and a tap on a highlight is the only way to reach
 * the thread it is about. Anything else gets the head and one sentence saying
 * the decision is not here. The two differ in what they render and in WHERE they
 * are placed; see topFor for why the first hangs the other way up.
 */
export class SuggestionUI {
  editor: Editor;
  pending: () => SuggestionLike[];
  threads: () => ThreadLike[];
  threadCard: ((thread: ThreadLike) => Element) | null;
  railShown: () => boolean;
  onReveal: ((thread: ThreadLike) => void) | null;
  frame: () => { top: number; bottom: number };
  anchor: Element | null;
  found: FoundMark | null;
  el: HTMLDivElement;

  /**
   * @param opts.pending the latest /_galley/pending suggestions
   * NO `post` AND NO `refresh`: this surface asks the server for nothing. Both
   * were injected for `decide`, the accept/reject POST, and went with it.
   * @param opts.threads the latest /_galley/pending comments
   * @param opts.threadCard the ONE card
   *   component — the same one the rail, the panel, the settled list and the
   *   sheet render. It arrives by injection rather than by importing entry.ts,
   *   the same way `post`, `pending` and `refresh` do: this file is the half of
   *   suggestion mode that knows nothing about the App, and an import here
   *   would be a cycle (entry.ts already imports this).
   * @param opts.railShown whether the notes rail is on
   *   screen carrying conversations — the whole of the decision, see threadFor.
   * @param opts.frame the band of the
   *   window the prose is readable in, in VIEWPORT coordinates. Injected for
   *   the same reason the rest is: both bars are out of flow over the prose and
   *   entry.ts is what owns them, so it answers rather than teaching this file
   *   their selectors. Defaults to the whole window.
   */
  constructor({
    editor,
    pending,
    threads,
    threadCard,
    railShown,
    frame,
    onReveal,
  }: SuggestionUIOptions) {
    this.editor = editor;
    this.pending = pending;
    this.threads = threads || (() => []);
    this.threadCard = threadCard || null;
    this.railShown = railShown || (() => true);
    // Where the rail IS carrying the conversation, this is what a click on its
    // highlight does instead of opening a second copy of it — see show. It is
    // injected for the same reason threadCard is: this file knows nothing about
    // the App, and the App owns the surfaces a card can be on.
    this.onReveal = onReveal || null;
    this.frame =
      frame ||
      (() => ({
        top: 0,
        bottom:
          document.documentElement.clientHeight || window.innerHeight || 0,
      }));
    // What the bubble is currently open ON. Kept so a pending refresh can
    // redraw the conversation in place — see restage.
    this.anchor = null;
    this.found = null;

    this.el = document.createElement('div');
    this.el.className = 'gly-bubble';
    this.el.hidden = true;
    document.body.appendChild(this.el);

    document.addEventListener('click', this.onDocumentClick, true);
    // A CONVERSATION REACHED BY TAPPING ITS MARK DOES NOT NEED "SHOW ME WHERE
    // THIS IS" — the answer is the sentence directly above it. threadCard wires
    // that click on every anchored card and it is right on all four of the
    // other surfaces; here it calls reveal, reveal scrolls, and a scroll
    // dismisses the bubble. Measured: tapping a conversation to read it closed
    // it, which on a narrow window is most of the card's own area.
    //
    // Stopped in the CAPTURE phase on the bubble, which is an ANCESTOR of the
    // card and so runs before the card's own listener — a capture listener on
    // the card itself would not, because listeners on the target element fire
    // in registration order whatever their phase. The exemption is the same
    // partition threadCard's own handler uses, so the verbs and the reply box
    // are reached exactly as they are everywhere else.
    this.el.addEventListener(
      'click',
      (event) => {
        if (!this.el.classList.contains('gly-bubble-thread')) {
          return;
        }
        const target = event.target;
        if (target instanceof Element && target.closest('button, textarea')) {
          return;
        }
        event.stopPropagation();
      },
      true,
    );
    window.addEventListener('resize', () => this.hide());
    // THE CAPTURE PHASE IS WHY THIS HAS TO READ ITS EVENT. `scroll` does not
    // bubble, so listening on `window` in capture is what catches a scrollable
    // ANCESTOR of the mark moving it — and it catches every other scroll on the
    // page with it, including the bubble's own. Once a capped conversation made
    // the bubble a scroll container, the gesture that reaches `✓ resolve` and
    // `delete` was the gesture that closed the surface holding them: measured at
    // 390×844 with an eleven-entry thread, one wheel over the card and the
    // bubble was gone.
    //
    // The distinction is exactly the one the placement cares about. The page
    // moving under the bubble makes its position stale, so it must go; the
    // bubble scrolling INSIDE ITSELF moves nothing it was placed against.
    window.addEventListener(
      'scroll',
      (event) => {
        if (event.target instanceof Node && this.el.contains(event.target)) {
          return;
        }
        this.hide();
      },
      true,
    );
  }

  // An ARROW FIELD, not a method: it is handed to `addEventListener` as a
  // bare reference two lines into the constructor, and a method reference
  // detached from its instance loses `this` the moment it is invoked as a
  // callback. The old form bound it by hand (`this.onDocumentClick =
  // this.onDocumentClick.bind(this)`) — correct at runtime, but the type
  // eslint sees for `this.onDocumentClick` is still "unbound class method",
  // so the static check cannot tell the rebinding happened. A field is bound
  // by construction and needs no rebind for the checker or the reader to
  // trust.
  onDocumentClick = (event: MouseEvent): void => {
    const target = event.target;
    if (target instanceof Node && this.el.contains(target)) {
      return;
    }
    const span =
      target instanceof Element ? target.closest(MARK_SELECTOR) : null;
    if (!span || !this.editor.view.dom.contains(span)) {
      this.hide();
      return;
    }
    const found = this.markAt(span);
    if (!found) {
      this.hide();
      return;
    }
    this.show(span, found);
  };

  // markAt reads the FULL marked run the clicked span belongs to, from the
  // document rather than from the DOM. A run split by other formatting — half
  // of an insertion in bold — is several spans but one suggestion, and the
  // server matches on the whole run's text. markRuns does the widening; this
  // only has to say which run the click landed in.
  markAt(span: Element): FoundMark | null {
    const view = this.editor.view;
    let pos: number;
    try {
      pos = view.posAtDOM(span, 0);
    } catch {
      return null;
    }
    const kind = KIND_BY_CLASS[markClass(span)];
    if (!kind) {
      return null;
    }
    const run = markRuns(view.state.doc).find(
      (r) => r.kind === kind && r.from <= pos && pos < r.to,
    );
    if (!run) {
      return null;
    }
    // The run rides along. It is the mark's identity, and it is what makes a
    // bubble decision exact for two marks that read the same — the same fix
    // the rail's cards got. sameSuggestion already prefers it when both sides
    // carry one, so `match` needs no change.
    return {
      kind: run.kind,
      text: run.text,
      author: run.author,
      at: run.at,
      run: run.runId,
    };
  }

  // match maps a marked run onto the SERVER'S OWN ENTRY for it — by run where
  // both sides carry one (including a substitution's inserted half; see
  // sameSuggestion), and by (kind, text, author) for a mark no session has
  // stamped. Deliberately no guessing: ids are ordinals re-derived on every
  // read (see suggest.Pending), so picking one of two identical-looking
  // candidates would sooner or later accept the wrong one.
  //
  // IT RETURNS THE ENTRY, NOT AN ID, and that is what closed the seventh silent
  // site. `render` has to ask two questions of the server's answer — what is
  // this, and may it be decided — and an id can answer neither. It used to hand
  // back `{ id }` alone, so the bubble drew ✓ accept / ✗ reject on everything
  // it could name, including a comment: the press posted
  // `/_galley/accept {run, id}`, the server's own `Decidable` filter turned it
  // into a 404, and the bubble then said "that suggestion has moved — reopen
  // it", which is false twice over.
  match(found: FoundMark): MatchResult {
    const candidates = this.pending().filter((p) => sameSuggestion(p, found));
    if (candidates.length === 1) {
      return { suggestion: candidates[0] };
    }
    if (candidates.length === 0) {
      // NO DIAGNOSIS. This used to read "the server has not seen this one yet —
      // it lands on the next save", which is a GUESS about why nothing matched,
      // and it was wrong in the ordinary case: the green half of a substitution
      // reached this branch about a suggestion the server had seen, listed,
      // numbered and carded. What is actually known is that this surface has no
      // decision to offer, and where the decision lives.
      return { note: 'no decision available here — see the rail' };
    }
    return {
      note: 'several suggestions here read the same — open the CLI: galley pending',
    };
  }

  /**
   * threadFor maps a clicked mark onto the conversation it is about, or null
   * when the bubble should stay the two-verb bubble it has always been.
   *
   * THE MATCH IS THE RUN, and nothing else. A run is the mark's own identity,
   * minted server-side (docmodel.RunAttr), carried into the DOM as data-run and
   * reported back on the thread by /_galley/pending — so the two sides are
   * comparing the same string rather than agreeing about text. What it resolves
   * TO is a thread carrying `suggest.CommentKey`, which is the stable identity
   * every verb on the card posts. The ORDINAL id is not used here and must not
   * be: it is re-derived on every read (see suggest.Pending) and renumbers the
   * moment a neighbour resolves. Two threads on one run would be a bug in the
   * server, and this answers null rather than guessing which.
   *
   * IT NO LONGER ASKS WHERE THE CONVERSATION SHOULD BE DRAWN — that is `show`'s
   * question, below, and separating the two is what let a comment highlight
   * stop being a dead end. This answers only "which conversation is this mark
   * about", which is a fact about the payload and the same at every width.
   */
  threadFor(found: FoundMark | null): ThreadLike | null {
    if (!found || found.kind !== 'comment' || !found.run || !this.threadCard) {
      return null;
    }
    const hits = (this.threads() || []).filter((t) => t && t.run === found.run);
    return hits.length === 1 ? hits[0] : null;
  }

  /**
   * show puts the right surface on the mark that was clicked, and there are
   * three answers rather than the two this used to have.
   *
   * A COMMENT HIGHLIGHT IS A CONVERSATION, WHEREVER IT IS DRAWN. Clicking one
   * is the most natural gesture in a document with markup in it, and at wide
   * width it produced a bubble reading `COMMENT · AGENT · JUST NOW` with
   * `✓ accept` / `✗ reject` and NOTHING ELSE — no comment text, no
   * conversation, no reply box, and two verbs that 404 (see match, and
   * render, which offers none). The reviewer's most instinctive click was the
   * product's worst surface.
   *
   * ONE CONVERSATION, ONE PLACE — and the rail decides, "is the rail on
   * screen", not "is the window narrow". A thread drawn here while the rail is
   * drawing it too is one conversation in two places, each with its own reply
   * box and its own delete. So where the rail IS carrying it, the click does
   * the only useful thing left: it takes the reviewer to the card — the same
   * reveal() the card already offers in the other direction — and opens no
   * bubble at all. Where the rail is NOT (below the breakpoint, or collapsed at
   * any width, which railSurfaces answers and a width test would get wrong),
   * the bubble IS where the conversation lives and it carries the whole card,
   * reply box and both verbs included.
   *
   * A comment mark with no thread to show falls through to the two-verb bubble,
   * which now renders as a head and an honest note: there is nothing to decide
   * on a conversation and nothing to read either.
   */
  show(span: Element, found: FoundMark): void {
    this.anchor = span;
    this.found = found;
    const thread = this.threadFor(found);
    if (thread && this.railShown() && this.onReveal) {
      this.hide();
      this.onReveal(thread);
      return;
    }
    if (thread) {
      this.renderThread(thread);
    } else {
      this.render(found, this.match(found));
    }
    const rect = span.getBoundingClientRect();
    // CLEARED BEFORE ANYTHING IS MEASURED, not after. A cap left over from the
    // last mark this bubble opened on is a constraint on the card being sized
    // now, and both leastUseful and topFor read a height off the DOM.
    this.el.style.maxHeight = '';
    this.el.hidden = false;
    if (thread) {
      // A CONVERSATION IS CAPPED TO THE ROOM IT HAS, before it is measured. A
      // thread is unbounded — ten replies is a card taller than a laptop window
      // — and a card taller than both sides of its mark can only be drawn OVER
      // the sentence it is about, which is the one thing this placement exists
      // to prevent. Capped to the better of the two sides, one of them always
      // fits, and what does not fit scrolls (see .gly-bubble-thread). The
      // two-verb bubble is three lines and is left uncapped.
      //
      // BUT NEVER BELOW WHAT MAKES IT A CONVERSATION — see leastUseful.
      //
      // THE TWO ROUND OPPOSITE WAYS, and each direction is the safe one for
      // what it bounds. The room is floored, because `${149.59375}px`
      // serialises as `149.594px` — LARGER than the room it was computed from,
      // and that was enough to lose the below/above comparison in topFor by a
      // ten-thousandth of a pixel and flip the card to the wrong side of its
      // mark. The floor is ceiled, because a cap a fraction under it clips the
      // first entry by a descender, which is this whole defect arriving one
      // pixel at a time. Where they meet, the conversation wins the pixel.
      const cap = Math.max(
        Math.floor(this.roomFor(rect)),
        Math.ceil(this.leastUseful()),
      );
      this.el.style.maxHeight = `${cap}px`;
    }
    this.el.style.top = `${this.topFor(rect, !!thread)}px`;
    // BOTH ENDS OF THIS AXIS, not just the left one. This was `Math.max(4, …)`
    // — a floor with no ceiling — while topFor reasons at length about both of
    // its edges. So a mark late in a line put the card past the right edge of
    // the window: measured at 700px the bubble's right edge was 708.7 against
    // an `innerWidth` of 700 and `document.scrollWidth` went to 709, which is a
    // phone gaining sideways scroll it never had. The verbs stayed reachable,
    // so what this costs is a few pixels of cosmetics and a scrollbar rather
    // than a lost control — but the card is placed AGAINST its mark, and where
    // the mark is near the edge the honest placement is the edge.
    //
    // The width is MEASURED off the card, after `hidden` is cleared and the cap
    // is set, for the same reason topFor measures rather than assumes: it is a
    // max-width, a font size and a wrap count, not a constant. `clientWidth` on
    // the root element rather than `innerWidth`, because a classic scrollbar is
    // window width the page cannot paint in.
    const room = document.documentElement.clientWidth;
    const width = this.el.getBoundingClientRect().width;
    // Where the two clamps disagree — a card wider than the window it is in —
    // the LEFT edge wins, because a card whose beginning is off screen cannot
    // be read at all, and Math.min/Math.max in this order is what says so.
    const most = Math.max(4, room - width - 4) + window.scrollX;
    const want = rect.left + window.scrollX;
    this.el.style.left = `${Math.max(4 + window.scrollX, Math.min(want, most))}px`;
  }

  /**
   * leastUseful is the height below which this stops being a conversation.
   *
   * The pinned rows — head, reply box, both verbs — cannot give at all
   * (`flex: none`), so everything a smaller cap takes comes out of the ENTRIES,
   * and the entries give ALL THE WAY TO ZERO. Measured at 844×390, a phone in
   * landscape, and again at 390×430, a phone with its software keyboard up
   * under a tapped reply box: a card headed `THREAD · …`, with a reply box and
   * both verbs, and not one word of what was said — `clientHeight: 0` against
   * `scrollHeight: 606`, so no gesture could recover it either. Both are
   * ordinary states, not corners: a rotation, and typing.
   *
   * So the cap has a floor, and the floor is everything except the conversation
   * PLUS THE FIRST THING ANYBODY SAID. Measured off the card already on screen
   * rather than declared, because the chrome's height is a font size and a wrap
   * count, not a constant.
   *
   * Below that the honest thing is to overflow the room the mark leaves, and
   * that is what happens: topFor has always said a card can be taller than the
   * space it is placed in (see its clamps), so it clamps into the readable band
   * and covers a little more prose. That is a degradation a reviewer can scroll
   * around. A card with nothing in it is not.
   *
   * Read while the bubble is UNCAPPED — which is why show clears the inline
   * max-height before it calls this.
   */
  leastUseful(): number {
    const said = this.el.querySelector('.gly-bubble-said');
    const first = said && said.firstElementChild;
    if (!said || !first) {
      return 0;
    }
    // RECTS, not offsetHeight/clientHeight: those two round to whole pixels and
    // the errors do not cancel — measured, the floor landed 1px short and the
    // first entry was clipped by exactly one line's descender, which is the
    // same defect this exists to prevent arriving a pixel at a time.
    //
    // And from the region's top to the first entry's BOTTOM, so the entry's own
    // top margin is inside the answer rather than eaten out of it.
    const box = this.el.getBoundingClientRect();
    const region = said.getBoundingClientRect();
    const shown = first.getBoundingClientRect().bottom - region.top;
    return box.height - region.height + Math.max(0, shown);
  }

  // roomFor is the taller of the two gaps the mark leaves in the readable band
  // — above it and below it — which is the most room any placement can have.
  // The 12 is the 8px stand-off plus the 4px the clamps keep off the edge.
  roomFor(rect: DOMRect): number {
    const band = this.frame();
    const room = Math.max(rect.top - band.top, band.bottom - rect.bottom) - 12;
    if (room > 0) {
      return room;
    }
    // A MARK CAN BE TALLER THAN THE BAND IT SITS IN — a comment run wrapping
    // most of a short window — and then both gaps are negative. That must not
    // reach the CSSOM: a negative max-height is DROPPED, which leaves the
    // previous open's cap in place and hands topFor a card sized for a
    // different mark, after which it is drawn over this one. There is no
    // roomier side to prefer, so the cap becomes the band itself and the card
    // covers the mark because there is nowhere left that does not.
    return Math.max(0, band.bottom - band.top - 8);
  }

  /**
   * topFor decides which side of the mark the bubble hangs from, in PAGE
   * coordinates (the bubble is `position: absolute`, so everything here adds
   * window.scrollY exactly once).
   *
   * THE TWO PLACEMENTS DIFFER ON PURPOSE — do not unify them. A two-verb bubble
   * is three lines about the mark and sits ABOVE it: it covers the line before
   * the one under discussion and leaves that one readable. A CONVERSATION is a
   * whole card — a head, every reply, a reply box and two verbs — and above the
   * mark a card that tall covers the sentence the conversation is about, which
   * is the one sentence the reviewer needs to read while answering it. So a
   * thread HANGS BELOW, and the prose it is about stays above it, uncovered.
   *
   * The fallback when a thread does not FIT below is `above` — the two-verb
   * placement — rather than a below clamped up into the viewport. A clamped
   * below is drawn OVER the mark, which is the single outcome both placements
   * exist to avoid; above still leaves the sentence uncovered, and roomFor has
   * already made sure it fits there.
   *
   * BOTH CLAMP TO THE READABLE BAND, NOT TO THE WINDOW. The top bar is sticky
   * over the head of the page and the narrow layout's bottom bar is fixed over
   * its foot; a card clamped to the window is drawn over one of them — measured,
   * with a conversation covering `✓ all`, `✗ all` and the whole-doc handle. The
   * clamps still fire despite roomFor, because a card has a minimum height of
   * its own (a head, a two-row reply box and two verbs) that a short enough
   * window cannot honour.
   */
  topFor(rect: DOMRect, thread: boolean): number {
    // Measured after unhiding: a hidden element has no height to place by.
    const h = this.el.offsetHeight;
    const page = window.scrollY;
    const band = this.frame();
    const ceiling = page + band.top + 4;
    const above = rect.top + page - h - 8;
    if (!thread) {
      return Math.max(ceiling, above);
    }
    const floor = page + band.bottom - h - 4;
    const below = rect.bottom + page + 8;
    return below <= floor ? below : Math.max(ceiling, above);
  }

  // renderThread hands the whole bubble over to the one card. Nothing is added
  // around it — no head, no verbs of the bubble's own — because a conversation
  // already says what it is about and already carries the two verbs it has.
  renderThread(thread: ThreadLike): void {
    this.el.textContent = '';
    this.el.classList.add('gly-bubble-thread');
    // Every caller of renderThread reaches it through threadFor, which
    // answers non-null only when this.threadCard is itself set — so this is
    // narrowing what is already guaranteed, not a new refusal path.
    if (!this.threadCard) {
      return;
    }
    const card = this.threadCard(thread);
    // The other half of the constructor's capture listener: the card promises
    // "show me where this is" in its tooltip, and here the click behind that
    // promise is stopped. A promise left on screen with nothing behind it is
    // the worse of the two halves to forget.
    card.removeAttribute('title');
    this.el.appendChild(card);
  }

  /**
   * restage redraws an open conversation from freshly polled data.
   *
   * Without it a reply posts, lands in the file, and does not appear in the
   * card the reviewer typed it into — which below the breakpoint is the only
   * copy of that conversation on screen.
   *
   * IT REFUSES TO TOUCH WORK IN PROGRESS. Rebuilding a card destroys its reply
   * box, and destroying a reply box someone is typing into is the exact
   * work-losing shape armedDelete and carryDrafts were written to end. So a
   * bubble holding focus, or holding so much as a half-typed word, is left
   * exactly as it is; the next poll after the reviewer is done redraws it.
   */
  restage(): void {
    // Only a conversation is redrawn. The two-verb bubble reports its own
    // decision optimistically and a poll landing mid-decision would wipe
    // "accepting…" off the screen.
    //
    // this.anchor and this.found are only ever set together (see show), so
    // the added `!this.found` narrows what was already true rather than
    // refusing a state this bubble could actually be in.
    if (
      this.el.hidden ||
      !this.anchor ||
      !this.found ||
      !this.el.classList.contains('gly-bubble-thread')
    ) {
      return;
    }
    // Captured now, before any further call: TypeScript cannot see that
    // nothing below reassigns these fields, so it would otherwise widen them
    // back to nullable after the first method call.
    const anchor = this.anchor;
    const found = this.found;
    if (this.el.contains(document.activeElement)) {
      return;
    }
    for (const field of this.el.querySelectorAll('[data-draft]')) {
      if (field instanceof HTMLTextAreaElement && field.value) {
        return;
      }
    }
    // The mark may be gone — resolved from the CLI lifts the highlight, and a
    // rebuilt fragment replaces the span this is hung from. Either way there is
    // no conversation on screen to be about.
    if (!this.editor.view.dom.contains(anchor) || !this.threadFor(found)) {
      this.hide();
      return;
    }
    this.show(anchor, found);
  }

  /**
   * render is the bubble's OTHER shape: a head and one honest sentence, for a
   * mark this surface has nothing to say about.
   *
   * IT HAS NO VERBS LEFT, AND THE PATH THEY WERE ON WAS UNREACHABLE BEFORE IT
   * LOST THEM. `button` and `decide` drew ✓ accept / ✗ reject and POSTed
   * `/_galley/accept` and `/_galley/reject` — two of the twelve endpoints
   * internal/serve/rounds_surface_test.go asserts are 404. Nothing could reach
   * them: `pending()` is the App's `this.suggestions`, `pendingView` is
   * `{instructions, blocks}` and carries no suggestions at all, so `match()`
   * takes its zero-candidate branch on every mark and hands back a `note` —
   * which returns from this function several lines above where the pair used to
   * be built. The `state` parameter went with them; its only writer was the
   * optimistic self-report of a decision in flight.
   *
   * WHAT REPLACES THEM IS THE SENTENCE `match` ALREADY WROTE: *no decision
   * available here — see the rail*. An instruction is not decided, and its own
   * card carries the two verbs it does have.
   */
  render(found: FoundMark, matched: MatchResult): void {
    this.el.textContent = '';
    this.el.classList.remove('gly-bubble-thread');
    const suggestion = matched && matched.suggestion;

    const head = document.createElement('div');
    head.className = 'gly-bubble-head';
    // The SERVER'S kind where it claimed this mark — a substitution's two marks
    // are one suggestion, and the head must not say `insertion` about the
    // green half of the `replace` on the card beside it.
    const kind = (suggestion && suggestion.kind) || found.kind;
    head.textContent = [
      KIND_LABEL[kind] || kind,
      found.author || 'unattributed',
      age(found.at),
    ]
      .filter(Boolean)
      .join(' · ');
    this.el.appendChild(head);

    const say = (text: string) => {
      const note = document.createElement('div');
      note.className = 'gly-bubble-note';
      note.textContent = text;
      this.el.appendChild(note);
    };

    if (matched && matched.note) {
      say(matched.note);
    }
  }

  // `button` AND `decide` ARE DELETED. They were the bubble's ✓ accept / ✗
  // reject pair and the POST behind them; see render for why no click could
  // ever reach either, and what the reviewer is told instead.

  hide(): void {
    this.el.hidden = true;
  }
}

function markClass(span: Element): string {
  for (const k of MARK_KINDS) {
    if (span.classList.contains(k.className)) {
      return k.className;
    }
  }
  return '';
}

function markOf(node: PMNode, typeName: string): Mark | null {
  return node.marks.find((m) => m.type.name === typeName) || null;
}

// age renders an RFC3339 stamp as something a reader can act on. Anything
// unparseable renders as nothing rather than "Invalid Date".
//
// So does anything before the epoch, which is not a hypothetical: CriticMarkup
// has nowhere to write a suggestion's author or timestamp, so a suggestion
// read straight out of a file arrives with Go's ZERO time — year 1 — and renders as "739833d ago" unless it is caught here.
export function age(iso: string | undefined): string {
  if (!iso) {
    return '';
  }
  const then = Date.parse(iso);
  if (Number.isNaN(then) || then <= 0) {
    return '';
  }
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 45) {
    return 'just now';
  }
  const mins = Math.round(secs / 60);
  if (mins < 60) {
    return `${mins}m ago`;
  }
  const hours = Math.round(mins / 60);
  if (hours < 24) {
    return `${hours}h ago`;
  }
  return `${Math.round(hours / 24)}d ago`;
}
