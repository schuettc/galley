// probe.mjs — the build-time sanity check for the editor bundle.
//
// There is no JS test harness in this repo and no intention of adding one:
// the editor's real verification is Task 12's Playwright pass in a browser.
// But two things can be checked without one, and both have already been the
// kind of mistake that costs an afternoon in a browser:
//
//   1. the suggestion plugin's behaviour. ProseMirror's state, model and
//      transform packages need no DOM, so the plugin can be driven directly:
//      type, delete, delete your own insertion, type over a selection.
//   2. the heading level arriving as a STRING. Task 6 leaves it that way, and
//      an untreated string level renders every heading as h1.
//
// Plus the built bundle parses. `just assets` runs this; a failure here means
// the committed bundle is wrong, which nothing downstream would notice until
// someone opened the page.

import { readFileSync } from 'node:fs';
import { Script } from 'node:vm';

import { Extension, getSchema } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import TableRow from '@tiptap/extension-table-row';
// The editor registers Link separately from StarterKit (entry.ts), so a schema
// built without it has no link mark — and "a typed edit across a LINK is one
// edit" would be unstatable, which is the kind of hole that let a typed edit
// across formatting go unchecked in the first place.
import Link from '@tiptap/extension-link';
import { EditorState, TextSelection } from '@tiptap/pm/state';
// The two the trail's set pass needs to stage a SERVER-SIDE MUTATION with no
// browser: every one of them reaches the editor as one whole-document
// ReplaceStep (y-prosemirror rebuilds the doc), which is what invalidates every
// live position at once.
import { ReplaceStep } from '@tiptap/pm/transform';
import { Slice } from '@tiptap/pm/model';
import { ySyncPluginKey } from 'y-prosemirror';

import {
  suggestionPlugin,
  literalHit,
  isFence,
  isTable,
  isFrontMatter,
  isMathBlock,
  literalRegion,
  isCodeText,
  FENCE_INSIDE,
  FENCE_JOIN,
  FENCE_HINT,
  TABLE_INSIDE,
  TABLE_JOIN,
  TABLE_HINT,
  FRONT_MATTER_INSIDE,
  FRONT_MATTER_JOIN,
  FRONT_MATTER_HINT,
  MATH_INSIDE,
  MATH_JOIN,
  MATH_HINT,
  CODE_INSIDE,
  CODE_HINT,
  markRuns,
  sameSuggestion,
  loosePeer,
} from './suggestions.ts';
import {
  TolerantHeading,
  UncreatableCodeBlock,
  FiguredCodeBlock,
  FiguredImage,
  ReadOnlyTable,
  AlignedTableCell,
  AlignedTableHeader,
  Ins,
  Del,
  Highlight,
  placeOf,
  placeIn,
  spanIn,
} from './entry.ts';
// The seal vocabulary moved out of entry.ts — see web/seal.ts's header.
import { sealLine, reopenLine } from './seal.ts';
import {
  MODE_ASK,
  MODE_LIVE,
  modeLabel,
  modeOn,
  modeTitle,
  nextMode,
  roundPhrase,
  PHASE_DRAFT,
  PHASE_AGENT,
} from './bar.ts';
// composerPlacement moved to web/composer.ts with its one caller,
// placeComposerButton — see that module's header.
// coerceLevel moved to web/heading.ts — shared between TolerantHeading
// (entry.ts) and sectionSpan (figures.ts).
import { coerceLevel } from './heading.ts';
// sectionSpan and the figure and block-grip methods moved to web/figures.ts —
// see that module's header.
import { sectionSpan } from './figures.ts';
// The block grip's rules, which need no browser: which blocks get one, what its
// face and label say, how many instructions it carries, and how two grips
// closer than one grip's height are kept apart.
import {
  GRIP_KINDS,
  gripTargets,
  gripCount,
  gripFace,
  gripLabel,
  stackGrips,
} from './grips.ts';
import { composerPlacement } from './composer.ts';
// The verdict vocabulary moved out of entry.ts — see web/verdict.ts's header.
import {
  verdictMethods,
  verdictLabel,
  REVISE_IDLE,
  APPROVE_IDLE,
  MENU_REVISE,
  MENU_TRUST,
  clockTime,
  VERDICT_ENTRUSTED,
  VERDICT_DISCARDED,
} from './verdict.ts';
import {
  VIEWS,
  DEFAULT_VIEW,
  COULD_NOT,
  ALL_ROUNDS,
  BACK_TO_DRAFT,
  IDENTICAL_SAID,
  ageSaid,
  changedSaid,
  changeHead,
  roundHead,
  roundFoot,
  whereSaid,
  workRounds,
  roundCards,
  ordinalOf,
  arrivalSaid,
  askedOf,
  VERSIONS_LABEL,
  VERSIONS_NAME,
} from './versions.ts';
import {
  diffPending,
  arrivalMessage,
  arrivalNeedsStrip,
  queueArrivals,
  nextArrival,
  holdLabel,
  arrivalAuthor,
  ARRIVAL_AGENT,
} from './arrivals.ts';
// The reveal moved out of entry.ts and into web/card.ts with the rest of the
// one-card collapse — History was answering the same question a second, shorter
// way. Same functions, checked the same way, imported from where they live now.
import {
  scrollArrival,
  scrollStep,
  SCROLL_ARRIVED_PX,
  SCROLL_STALL_FRAMES,
} from './card.ts';
import { FrontMatterBlock } from './frontmatter.ts';
import { litDecorations, sameRuns } from './lit.ts';
import { MathBlock } from './math.ts';
import { NoteBlock, noteWordDecorations } from './note.ts';
import {
  contextOf,
  TRAIL_CONTEXT_CHARS,
  recordOf,
  applyRecord,
  trimAffixes,
  expandToWord,
  retractReverted,
  diffOf,
  reanchor,
  emptyBlockAnchor,
  verifyEntry,
  settleEntries,
  placeEmptyBlocks,
  TRAIL_SET_CAP,
  trailDecorations,
} from './trail.ts';
import {
  stackCards,
  railSurfaces,
  censusCounts,
  unplacedSaid,
  decidable,
  stepPending,
  keyTargetIsEditable,
  submitOnEnter,
  threadLabel,
  growOnInput,
  HEAD_QUOTE_CHARS,
  overallThreads,
  railThreads,
  threadPlacement,
  carryDrafts,
  RAIL_GAP,
  RAIL_MIN_WIDTH,
} from './rail.ts';
import { strandedValue } from './cards.ts';

let failures = 0;

function check(name, ok, detail) {
  if (ok) {
    console.log(`ok    ${name}`);
    return;
  }
  failures += 1;
  console.log(
    `FAIL  ${name}${detail === undefined ? '' : ` — ${JSON.stringify(detail)}`}`,
  );
}

// paintedRevise is the primary's label as PRODUCTION paints it: makeRevise
// builds the button over a fake DOM, paintRevise writes the digits and the
// count and picks the face, and this reads back the one label left
// unreserved. The fake carries only what those two methods touch.
function paintedRevise({ pendingCount = 0, waitedMs = null }) {
  class FakeEl {
    constructor(tag) {
      this.tagName = tag;
      this.className = '';
      this.nodes = [];
      this.text = '';
      this.disabled = false;
      this.title = '';
    }
    get classList() {
      const names = () => this.className.split(/\s+/).filter(Boolean);
      return {
        contains: (c) => names().includes(c),
        add: (c) => {
          if (!names().includes(c)) this.className = [...names(), c].join(' ');
        },
        remove: (c) => {
          this.className = names()
            .filter((n) => n !== c)
            .join(' ');
        },
        toggle: (c, on) => {
          if (on ?? !names().includes(c)) {
            if (!names().includes(c))
              this.className = [...names(), c].join(' ');
          } else {
            this.className = names()
              .filter((n) => n !== c)
              .join(' ');
          }
        },
      };
    }
    append(...nodes) {
      this.nodes.push(...nodes);
    }
    get textContent() {
      return this.nodes.length
        ? this.nodes
            .map((n) => (typeof n === 'string' ? n : n.textContent))
            .join('')
        : this.text;
    }
    set textContent(v) {
      this.nodes = [];
      this.text = String(v);
    }
    addEventListener() {}
  }
  class FakeButton extends FakeEl {}
  const had = {
    document: globalThis.document,
    HTMLButtonElement: globalThis.HTMLButtonElement,
  };
  const button = new FakeButton('button');
  globalThis.document = {
    getElementById: (id) => (id === 'gly-revise' ? button : null),
    createElement: (tag) => new FakeEl(tag),
  };
  globalThis.HTMLButtonElement = FakeButton;
  try {
    const shell = {
      reviseWaiting: waitedMs !== null,
      reviseStartedAt: Date.now() - (waitedMs ?? 0),
      reviseRunning: false,
      approved: false,
      approveNotBefore: 0,
      verdict: REVISE_IDLE,
      pendingCount,
    };
    shell.revise = verdictMethods.makeRevise.call(shell);
    verdictMethods.paintRevise.call(shell);
    return button.nodes
      .filter((n) => !n.classList.contains('gly-reserved'))
      .map((n) => n.textContent)
      .join('|');
  } finally {
    globalThis.document = had.document;
    globalThis.HTMLButtonElement = had.HTMLButtonElement;
  }
}

// --- the schema the editor actually builds ---

const schema = getSchema([
  StarterKit.configure({ history: false, heading: false, codeBlock: false }),
  TolerantHeading,
  UncreatableCodeBlock,
  // Registered exactly as the editor registers it, so the link mark this
  // schema carries is the one a reviewer actually types across.
  Link.configure({ openOnClick: false, autolink: false }),
  // The same four the Editor registers, so schema.nodes.table exists here and
  // the boundary table below runs over the node the browser actually builds.
  ReadOnlyTable.configure({ resizable: false }),
  TableRow,
  AlignedTableHeader,
  AlignedTableCell,
  // The literal-region checks below reach a real <frontMatter> node through
  // this schema; without it they could only be written about a node that does
  // not exist, which is the unstatable outcome the mark half of this file
  // records paying for.
  FrontMatterBlock,
  MathBlock,
  Ins,
  Del,
  Highlight,
  Extension.create({ name: 'probe' }),
]);

// --- the shared fragment's node names ---

// FRAGMENT_NODES is every element name galley's Go side writes into the shared
// Yjs fragment: the string value of each docmodel.BlockKind constant.
//
// It is checked from BOTH sides, and neither half is sufficient alone.
// internal/ydoc's TestEveryBlockKindExistsInTheBrowserSchema parses docmodel's
// source and fails if a BlockKind is missing from this list, so a Go author who
// adds a node kind is told about this file. The check below fails if a name in
// the list is not a node in the schema the editor actually builds.
//
// The stakes are not cosmetic. y-prosemirror's createNodeFromYElement calls
// schema.node(el.nodeName, …) and its catch DELETES the element from the Yjs
// document — an unknown node is not ignored, it is erased, broadcast, and
// projected to disk. docmodel.Note shipped with no counterpart here, so opening
// any document containing a block note destroyed the note.
//
// A text search would not have caught it: StarterKit registers most of these
// implicitly and 'paragraph', 'heading' and the rest appear nowhere in web/ as
// literals. Only the built schema knows.
const FRAGMENT_NODES = [
  'paragraph',
  'heading',
  'codeBlock',
  'blockquote',
  'bulletList',
  'orderedList',
  'listItem',
  'horizontalRule',
  'image',
  'note',
  'frontMatter',
  'mathBlock',
  'table',
  'tableRow',
  'tableCell',
  'tableHeader',
];

// The editor's real extension list, minus the ones that need a live document
// (Collaboration) or a DOM (suggestionMode). Every extension that contributes a
// NODE is here, which is what this schema is for.
const fragmentSchema = getSchema([
  StarterKit.configure({ history: false, heading: false, codeBlock: false }),
  TolerantHeading,
  // The extensions the editor SHIPS, NodeViews and all. A NodeView cannot
  // change a schema — but which extension carries it can, and this schema is
  // what decides whether a name galley writes into the fragment can be built.
  // Checking a schema the editor does not actually construct is how the
  // <note> deletion went unnoticed once already.
  FiguredCodeBlock,
  FiguredImage,
  NoteBlock,
  // Front matter, for the reason every node here is registered: galley writes
  // <frontMatter> into the fragment, and a schema that cannot build one is a
  // schema that deletes the author's metadata on open.
  FrontMatterBlock,
  MathBlock,
  // The table nodes belong here for the same reason every other node does:
  // this schema is what decides whether a name galley writes into the fragment
  // can be built. A table left out of it is a table y-prosemirror deletes.
  ReadOnlyTable.configure({ resizable: false }),
  TableRow,
  AlignedTableHeader,
  AlignedTableCell,
  // Registered for exactly the reason every node above is: galley writes a
  // link mark into the fragment, and a schema that cannot build one is not a
  // model of the editor. It was missing here while `schema` had it.
  Link.configure({ openOnClick: false, autolink: false }),
  Ins,
  Del,
  Highlight,
]);

for (const name of FRAGMENT_NODES) {
  check(
    `the schema declares a <${name}> node`,
    Object.prototype.hasOwnProperty.call(fragmentSchema.nodes, name),
  );
}

// --- the same guard for MARKS, which did not exist and cost a whole case ---
//
// FRAGMENT_MARKS is every mark name galley's Go side writes into the shared
// fragment: the string value of each docmodel.MarkKind, minus hardBreak, which
// is a docmodel sentinel INLINE rather than a ProseMirror mark.
//
// WHY THIS EXISTS. The node half of this file has been guarded from both sides
// for a while — internal/ydoc's TestEveryBlockKindExistsInTheBrowserSchema
// parses docmodel's source and fails when a BlockKind is missing from
// FRAGMENT_NODES, and the loop above fails when a listed name is not in the
// schema. The mark half had NOTHING, and it cost exactly what an unguarded
// half costs: `Link` is registered by the editor separately from StarterKit,
// both schemas here were built without it, and so "a typed edit across a link
// is one edit" could not be written at all. Not failing, not passing —
// unstatable, which is the one outcome no reviewer can see.
//
// Checked against BOTH schemas, because they drifted apart: `schema` gained
// link and `fragmentSchema` had not.
const FRAGMENT_MARKS = [
  'bold',
  'italic',
  'code',
  'link',
  'ins',
  'del',
  'highlight',
];

for (const name of FRAGMENT_MARKS) {
  check(
    `the fragment schema declares a "${name}" mark`,
    Object.prototype.hasOwnProperty.call(fragmentSchema.marks, name),
  );
  check(
    `the suggestion-mode schema declares a "${name}" mark`,
    Object.prototype.hasOwnProperty.call(schema.marks, name),
  );
}

// And the two must not drift apart again in either direction. A mark added to
// entry.ts and to one schema here but not the other is the same silent hole,
// pointing the other way.
{
  const a = Object.keys(schema.marks).sort();
  const b = Object.keys(fragmentSchema.marks).sort();
  check(
    'both probe schemas carry the same marks',
    a.join(',') === b.join(','),
    { schema: a, fragmentSchema: b },
  );
}

// RECORDED, NOT FIXED: `schema` (the suggestion-mode one, above) still lacks
// the `image` and `note` NODES that fragmentSchema carries, so nothing here
// drives the plugin over either. Adding them shifts document positions in the
// fence and table boundary tables below, which is a change to checks this task
// has no reproduction for; it belongs to whoever next touches those.

// A hand-typed {>>words<<} note arrives as <note anchor="…"> holding one
// unmarked text run (ydoc.writeBlock's `len(b.Inlines) > 0` branch), beside
// the ID notes below that hold none. A node whose content
// expression cannot hold that run is deleted by the same catch that deletes an
// unknown one, so declaring the node is only half the fix — it has to be
// declared with the shape the fragment actually carries.
{
  let built = null;
  try {
    built = fragmentSchema.node('note', { anchor: 'block' }, [
      fragmentSchema.text('the axis labels are swapped'),
    ]);
  } catch (err) {
    check(
      'a <note> from the fragment builds against the schema',
      false,
      err.message,
    );
  }
  if (built) {
    check(
      'a <note> from the fragment builds against the schema',
      built.textContent === 'the axis labels are swapped',
    );
    check(
      'a <note> keeps its anchor attribute',
      built.attrs.anchor === 'block',
    );
  }
}

// A BLOCK COMMENT'S MARK is <note anchor id> with NO text run: its words live
// in the unsent round, and the file carries only "{>>@comment cb-…<<}".
// An attribute the node does not declare does NOT throw: prosemirror-model
// (1.25.11, computeAttrs) silently drops it, so the note builds without its id
// and the browser's next write takes the id out of the fragment and the file.
// That silent drop is why this compares attrs.id rather than trusting the
// build, and why `just schema`, which only sees deleted nodes, cannot catch
// it — so the id has to be declared in the same commit Go starts writing it.
{
  let built = null;
  try {
    built = fragmentSchema.node(
      'note',
      { anchor: 'block', id: 'cb-0123456789abcdef' },
      [],
    );
    built.check();
  } catch (err) {
    built = null;
    check(
      'a <note id> with no text builds against the schema',
      false,
      err.message,
    );
  }
  if (built) {
    check(
      'a <note id> with no text builds against the schema',
      built.childCount === 0,
    );
    check(
      'a <note id> keeps attrs.id',
      built.attrs.id === 'cb-0123456789abcdef',
      built.attrs,
    );
  }
}

// A TEXT COMMENT'S ID rides on its highlight, beside the run. A mark attribute
// the schema does not declare is dropped when y-prosemirror builds the mark,
// and the next write from the browser removes it from the fragment and the
// file — so it is declared on the highlight mark, in both probe schemas.
for (const [label, s] of [
  ['fragment', fragmentSchema],
  ['suggestion-mode', schema],
]) {
  let m = null;
  try {
    m = s.mark('highlight', { run: 'r1', id: 'cm-0123456789abcdef' });
    m.type.checkAttrs(m.attrs);
  } catch (err) {
    m = null;
    check(
      `a highlight carrying id keeps it through the ${label} schema`,
      false,
      err.message,
    );
  }
  if (m) {
    check(
      `a highlight carrying id keeps it through the ${label} schema`,
      m.attrs.id === 'cm-0123456789abcdef' && m.attrs.run === 'r1',
      m.attrs,
    );
  }
}

const para = (text, marks) =>
  schema.node('paragraph', null, text ? [schema.text(text, marks)] : []);
const docOf = (...nodes) => schema.node('doc', null, nodes);
const stateOf = (doc) =>
  EditorState.create({ schema, doc, plugins: [suggestionPlugin()] });

function apply(state, fill) {
  const tr = state.tr;
  fill(tr);
  return state.apply(tr);
}

// Every text run as [text, "mark:author+mark:author"], which is what the
// assertions below are actually about.
function runs(state) {
  const out = [];
  state.doc.descendants((node) => {
    if (node.isText) {
      out.push([
        node.text,
        node.marks.map((m) => `${m.type.name}:${m.attrs.author}`).join('+'),
      ]);
    }
  });
  return out;
}

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// --- the reviewer's hand: edits apply directly, unmarked ---
//
// The inverse of what this section used to pin, and every check below ran RED
// against the tracked-typing build before the cut. The plugin no longer
// converts a local edit into anything: typing lands as plain text with no ins
// mark, deleting removes rather than re-inserting del-marked text, and a
// deletion across an AGENT's pending mark is the verdict by hand — the
// proposal simply vanishes. See
// docs/superpowers/specs/2026-08-14-the-reviewers-hand.md.

{
  const st = apply(stateOf(docOf(para('hello world'))), (tr) =>
    tr.insertText('X', 6),
  );
  check(
    'typing applies as plain text, unmarked',
    same(runs(st), [['helloX world', '']]),
    runs(st),
  );
}

{
  const st = apply(stateOf(docOf(para('hello world'))), (tr) =>
    tr.delete(1, 6),
  );
  check(
    'deleting removes — nothing is re-inserted struck',
    same(runs(st), [[' world', '']]),
    runs(st),
  );
}

{
  // THE VERDICT BY HAND. Deleting text that carries the agent's pending ins
  // mark simply deletes it: the proposal is gone from the document, so it is
  // gone from pending — decided, by the hand that could have pressed reject.
  // The projection side of the same gesture (clean .md, sane sidecar) is
  // asserted with a real server in typing.mjs §3.
  const theirs = schema.marks.ins.create({
    author: 'agent',
    at: '2026-01-01T00:00:00Z',
    run: 'ab12cd34',
  });
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('keep '),
      schema.text('theirs', [theirs]),
    ]),
  );
  const before = markRuns(doc).length;
  const st = apply(stateOf(doc), (tr) => tr.delete(5, 12));
  check(
    "deleting across an agent's pending insertion deletes it — a verdict by hand",
    same(runs(st), [['keep', '']]) &&
      before === 1 &&
      markRuns(st.doc).length === 0,
    {
      runs: runs(st),
      pendingBefore: before,
      pendingAfter: markRuns(st.doc).length,
    },
  );
}

{
  const st = apply(stateOf(docOf(para('hello world'))), (tr) =>
    tr.replaceWith(1, 6, schema.text('howdy')),
  );
  check(
    'typing over a selection replaces it outright',
    same(runs(st), [['howdy world', '']]),
    runs(st),
  );
}

{
  const st = apply(stateOf(docOf(para('one'), para('two'))), (tr) =>
    tr.delete(2, 7),
  );
  check(
    'a cross-block deletion applies clean, not corrupted',
    st.doc.textContent === 'owo' && runs(st).every(([, marks]) => marks === ''),
    { text: st.doc.textContent, runs: runs(st) },
  );
}

{
  const st = apply(stateOf(docOf(para('hello'))), (tr) => {
    tr.setMeta(ySyncPluginKey, { isChangeOrigin: true });
    tr.insertText('Z', 3);
  });
  check(
    'a remote change is left alone',
    same(runs(st), [['heZllo', '']]),
    runs(st),
  );
}

{
  // AN AGENT'S ARRIVED MARKS ARE NOT DISTURBED by an ordinary edit beside
  // them: the plugin appends nothing, so the mark the server stamped is
  // exactly the mark still there after the reviewer types next to it.
  const theirs = schema.marks.ins.create({
    author: 'agent',
    at: '2026-01-01T00:00:00Z',
    run: 'ab12cd34',
  });
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('keep '),
      schema.text('theirs', [theirs]),
    ]),
  );
  const st = apply(stateOf(doc), (tr) => tr.insertText('X', 3));
  check(
    "typing beside an agent's pending mark leaves the mark exactly as it arrived",
    same(runs(st), [
      ['keXep ', ''],
      ['theirs', 'ins:agent'],
    ]) &&
      markRuns(st.doc).length === 1 &&
      markRuns(st.doc)[0].runId === 'ab12cd34',
    { runs: runs(st), marks: markRuns(st.doc) },
  );
}

// --- a code fence is READ-ONLY ---
//
// The one rule this repo has paid for three times over: fenced content is
// literal and galley must never rewrite it. A reviewer reproduced all three
// breaches in a browser — Backspace at position 0 of a fence DESTROYED it
// (the codeBlock lifted away into the paragraph above and the next projection
// wrote CriticMarkup litter into the .md), typing inside one reached the file
// untracked, and Backspace inside one drifted the caret while nothing changed.
//
// Two things are checked here, separately, because they fail separately: the
// PREDICATE (does literalHit answer the question correctly at every boundary
// position) and the FILTER (does the plugin actually refuse the transaction).
// `state.apply` runs filterTransaction and returns the state unchanged when a
// plugin says no — the same path EditorView.dispatch takes — so the refusal is
// exercised here for real rather than described.

const fence = (text) =>
  schema.node('codeBlock', null, text ? [schema.text(text)] : []);

// before[0,8) · fence[8,21) · after[21,28). The fence's literal text is
// [9, 20): 9 is the position the destructive Backspace was pressed at.
const fenced = () => docOf(para('before'), fence('alpha bravo'), para('after'));

// stateWithRefusals returns a state whose plugin records every refusal, so the
// reason handed to the UI is checked and not assumed.
function stateWithRefusals(doc) {
  const refusals = [];
  const state = EditorState.create({
    schema,
    doc,
    plugins: [suggestionPlugin((hit) => refusals.push(hit))],
  });
  return { state, refusals };
}

// refused drives one transaction against a fenced document and reports what
// happened: whether the filter said no, and whether the document and selection
// came through untouched.
function refused(fill, at) {
  const { state, refusals } = stateWithRefusals(fenced());
  const start =
    at === undefined
      ? state
      : state.apply(state.tr.setSelection(TextSelection.create(state.doc, at)));
  const tr = start.tr;
  fill(tr);
  const next = start.apply(tr);
  return {
    blocked: next === start,
    text: next.doc.textContent,
    fences: countFences(next.doc),
    head: next.selection.head,
    reason: refusals.length ? refusals[0].reason : '',
    kind: refusals.length ? refusals[0].kind : '',
    at: refusals.length ? refusals[0].pos : -1,
  };
}

function countFences(doc) {
  let n = 0;
  doc.descendants((node) => {
    if (isFence(node)) {
      n += 1;
    }
  });
  return n;
}

const INTACT = 'beforealpha bravoafter';

{
  const doc = fenced();
  const kindAt = (from, to) => {
    const hit = literalHit(doc, from, to);
    return hit ? hit.kind : 'none';
  };
  // Every boundary, in one table, because every one of them is an off-by-one
  // waiting to happen. `join` is the destructive class: a change that reaches
  // past the fence's own text and merges it with what is beside it.
  const table = {
    'insert in the paragraph before': kindAt(3, 3),
    'insert in the block gap before the fence': kindAt(8, 8),
    'insert at the very start of the fence text': kindAt(9, 9),
    'insert mid-fence': kindAt(14, 14),
    'insert at the very end of the fence text': kindAt(20, 20),
    'insert in the block gap after the fence': kindAt(21, 21),
    'insert in the paragraph after': kindAt(24, 24),
    'delete inside the paragraph before': kindAt(5, 7),
    'delete ending exactly at the fence boundary': kindAt(5, 8),
    'join the fence into the block before it': kindAt(7, 9),
    'delete inside the fence': kindAt(11, 12),
    'delete the fence text entire': kindAt(9, 20),
    'join the block after into the fence': kindAt(20, 22),
    'delete the whole fence node': kindAt(8, 21),
    'delete from prose through the fence into prose': kindAt(3, 24),
    'delete inside the paragraph after': kindAt(23, 25),
  };
  const want = {
    'insert in the paragraph before': 'none',
    'insert in the block gap before the fence': 'none',
    'insert at the very start of the fence text': 'inside',
    'insert mid-fence': 'inside',
    'insert at the very end of the fence text': 'inside',
    'insert in the block gap after the fence': 'none',
    'insert in the paragraph after': 'none',
    'delete inside the paragraph before': 'none',
    'delete ending exactly at the fence boundary': 'none',
    'join the fence into the block before it': 'join',
    'delete inside the fence': 'inside',
    'delete the fence text entire': 'inside',
    'join the block after into the fence': 'join',
    'delete the whole fence node': 'join',
    'delete from prose through the fence into prose': 'join',
    'delete inside the paragraph after': 'none',
  };
  check(
    'literalHit answers every boundary position correctly',
    same(table, want),
    table,
  );
}

check(
  'isFence knows a code block from a paragraph',
  isFence(fence('x')) && !isFence(para('x')) && !isFence(null),
);

// --- a table is the second literal region ---
//
// The fence's boundary table (above) is the proof that literalHit's
// arithmetic is right. This is the proof it GENERALIZED — the same table, the
// same expected answers, over a table node instead of a fence. If these two
// ever disagree, the predicate has grown a second implementation inside
// itself, which is what phase 1e was explicitly not allowed to do.
const tableNode = () =>
  schema.node('table', null, [
    schema.node('tableRow', null, [
      schema.node('tableHeader', null, [para('a')]),
      schema.node('tableHeader', null, [para('b')]),
    ]),
  ]);

check(
  'isTable knows a table from a paragraph and a fence',
  isTable(tableNode()) &&
    !isTable(para('x')) &&
    !isTable(fence('x')) &&
    !isTable(null),
);

{
  const doc = docOf(para('before'), tableNode(), para('after'));
  const start = 1 + 'before'.length + 1; // the table's own position
  const end = start + doc.child(1).nodeSize;

  check(
    'a selection wholly inside a table is refused, as inside',
    (() => {
      const hit = literalHit(doc, start + 3, start + 4);
      return (
        !!hit &&
        hit.what === 'table' &&
        hit.kind === 'inside' &&
        hit.reason === TABLE_INSIDE
      );
    })(),
  );

  check(
    'a selection running from prose into a table is refused, as a join',
    (() => {
      const hit = literalHit(doc, 3, start + 3);
      return (
        !!hit &&
        hit.what === 'table' &&
        hit.kind === 'join' &&
        hit.reason === TABLE_JOIN
      );
    })(),
  );

  check('deleting the whole table is refused', !!literalHit(doc, start, end));

  check(
    'typing in the gap before a table is NOT refused',
    literalHit(doc, start, start) === null,
  );

  check(
    'typing in the gap after a table is NOT refused',
    literalHit(doc, end, end) === null,
  );

  check(
    'prose beside a table is fully markable',
    literalHit(doc, 1, 4) === null,
  );

  check(
    'the fence and the table say different sentences',
    TABLE_INSIDE !== FENCE_INSIDE && TABLE_JOIN !== FENCE_JOIN,
  );
}

// --- front matter is the third literal region ---
//
// The same boundary claims again, over the block a file OPENS with. It is the
// one that was neither modelled nor refused: goldmark read the "---" as a
// thematic break and the keys under it as a setext heading, so `galley edit`
// destroyed a file's metadata with no keystroke and no refusal.
//
// Two things are checked here that the table's section cannot say. It is asked
// BEFORE the fence — the node declares `code: true`, so isFence answers yes to
// it, and a reviewer editing their file's metadata must not be told it is a
// code fence. And it is the FIRST block in the document, which is the one
// position a boundary bug hides in: `pos` is 0 and an off-by-one there reads
// as "the gap before the block", which is a position that does not exist.
const frontMatterNode = () =>
  schema.node('frontMatter', null, [
    schema.text('---\ntitle: The Spec\n---\n'),
  ]);

check(
  'isFrontMatter knows the block from a paragraph and a fence',
  isFrontMatter(frontMatterNode()) &&
    !isFrontMatter(para('x')) &&
    !isFrontMatter(fence('x')) &&
    !isFrontMatter(null),
);

check(
  'a fence is not front matter, and front matter is not a fence to literalRegion',
  literalRegion(frontMatterNode()) === 'frontMatter' &&
    literalRegion(fence('x')) === 'fence',
);

{
  const doc = docOf(frontMatterNode(), para('after'));
  const start = 0; // the front matter block opens the document
  const end = start + doc.child(0).nodeSize;

  check(
    'a selection wholly inside front matter is refused, as inside',
    (() => {
      const hit = literalHit(doc, start + 3, start + 6);
      return (
        !!hit &&
        hit.what === 'frontMatter' &&
        hit.kind === 'inside' &&
        hit.reason === FRONT_MATTER_INSIDE &&
        hit.hint.startsWith(FRONT_MATTER_HINT)
      );
    })(),
  );

  check(
    'a selection running from front matter into the prose below is refused, as a join',
    (() => {
      const hit = literalHit(doc, start + 3, end + 3);
      return (
        !!hit &&
        hit.what === 'frontMatter' &&
        hit.kind === 'join' &&
        hit.reason === FRONT_MATTER_JOIN
      );
    })(),
  );

  check(
    'deleting the whole front matter block is refused',
    !!literalHit(doc, start, end),
  );

  check(
    'typing in the gap after front matter is NOT refused',
    literalHit(doc, end, end) === null,
  );

  check(
    'the prose under front matter is fully markable',
    literalHit(doc, end + 1, end + 4) === null,
  );

  check(
    "front matter says its own sentences, not the fence's",
    FRONT_MATTER_INSIDE !== FENCE_INSIDE &&
      FRONT_MATTER_JOIN !== FENCE_JOIN &&
      FRONT_MATTER_INSIDE !== TABLE_INSIDE,
  );
}

// --- display math is the fourth literal region ---------------------------
//
// The same boundary claims once more, over `$$ … $$`. It is the construct that
// was neither modelled nor refused after front matter was: to CommonMark it is
// a plain paragraph, so the converter turned its two soft breaks into spaces
// and `$$\nE = mc^2\n$$` came back `$$ E = mc^2 $$` — measured with the real
// binary, one `galley suggest`, no keystroke.
//
// It is asked BEFORE the fence for front matter's reason: the node declares
// `code: true`, so isFence answers yes to it, and a reviewer editing an
// equation must not be told it is a code fence.
const mathNode = () =>
  schema.node('mathBlock', null, [schema.text('$$\nE = mc^2\n$$\n')]);

check(
  'isMathBlock knows the block from a paragraph, a fence and front matter',
  isMathBlock(mathNode()) &&
    !isMathBlock(para('x')) &&
    !isMathBlock(fence('x')) &&
    !isMathBlock(frontMatterNode()) &&
    !isMathBlock(null),
);

check(
  'a math block is not a fence to literalRegion',
  literalRegion(mathNode()) === 'mathBlock' &&
    literalRegion(fence('x')) === 'fence',
);

{
  const doc = docOf(para('before'), mathNode(), para('after'));
  const start = doc.child(0).nodeSize;
  const end = start + doc.child(1).nodeSize;

  check(
    'a selection wholly inside display math is refused, as inside',
    (() => {
      const hit = literalHit(doc, start + 3, start + 6);
      return (
        !!hit &&
        hit.what === 'mathBlock' &&
        hit.kind === 'inside' &&
        hit.reason === MATH_INSIDE &&
        hit.hint.startsWith(MATH_HINT)
      );
    })(),
  );

  check(
    'a selection running from display math into the prose below is refused, as a join',
    (() => {
      const hit = literalHit(doc, start + 3, end + 3);
      return (
        !!hit &&
        hit.what === 'mathBlock' &&
        hit.kind === 'join' &&
        hit.reason === MATH_JOIN
      );
    })(),
  );

  check(
    'deleting the whole math block is refused',
    !!literalHit(doc, start, end),
  );

  check(
    'typing in the gap after display math is NOT refused',
    literalHit(doc, end, end) === null,
  );

  check(
    'the prose under display math is fully markable',
    literalHit(doc, end + 1, end + 4) === null,
  );

  check(
    "display math says its own sentences, not the fence's or the metadata block's",
    MATH_INSIDE !== FENCE_INSIDE &&
      MATH_JOIN !== FENCE_JOIN &&
      MATH_INSIDE !== FRONT_MATTER_INSIDE &&
      MATH_INSIDE !== TABLE_INSIDE,
  );
}

// --- the hint names the grip, where there is one ---
//
// A top-level fence, table, equation or front matter has a grip beside it, so
// the refusal's muted second line can say where an instruction on the whole
// block goes. A NESTED one has no grip, and the clause there would send the
// reviewer looking for a control that is not on the page: the hint has to be
// TRUE, so the clause is asked by position and not by kind. And it names the
// grip by where it is, never by its face: the face is `+` on a block with no
// instructions and their count on one with some.
{
  const grip = (noun) =>
    ` — or press the button to its left to leave an instruction on ${noun}`;
  const hintAt = (doc, pos) => {
    const hit = literalHit(doc, pos, pos + 1);
    return hit ? hit.hint : null;
  };
  const before = 1 + 'before'.length + 1;
  const mermaid = schema.node('codeBlock', { language: 'mermaid' }, [
    schema.text('graph TD\n  a --> b'),
  ]);
  const top = {
    table: hintAt(
      docOf(para('before'), tableNode(), para('after')),
      before + 3,
    ),
    fence: hintAt(docOf(para('before'), fence('alpha bravo')), before + 2),
    mermaid: hintAt(docOf(para('before'), mermaid), before + 2),
    math: hintAt(docOf(para('before'), mathNode()), before + 2),
    frontMatter: hintAt(docOf(frontMatterNode(), para('after')), 2),
  };
  check(
    'a top-level table, fence, diagram, equation and front matter each point at their grip',
    top.table === TABLE_HINT + grip('the whole table') &&
      top.fence === FENCE_HINT + grip('the whole code block') &&
      top.mermaid === FENCE_HINT + grip('the whole diagram') &&
      top.math === MATH_HINT + grip('the whole equation') &&
      top.frontMatter === FRONT_MATTER_HINT + grip('the front matter'),
    top,
  );

  const quoted = docOf(
    para('before'),
    schema.node('blockquote', null, [tableNode()]),
  );
  const listed = docOf(
    para('before'),
    schema.node('bulletList', null, [
      schema.node('listItem', null, [para('install'), fence('npm i')]),
    ]),
  );
  // The offsets walk in: blockquote (1), table (1), row (1), cell (1), and a
  // character into the cell's paragraph; list (1), item (1), the paragraph,
  // then a character into the fence.
  const nested = {
    table: hintAt(quoted, before + 5),
    fence: hintAt(listed, before + 2 + 'install'.length + 2 + 2),
  };
  check(
    'a table in a quotation and a fence in a list item have no grip, and their hint does not mention one',
    nested.table === TABLE_HINT && nested.fence === FENCE_HINT,
    nested,
  );
}

// The three ways this editor could create or change a table, each removed.
const tableContext = (parent) => ({
  name: 'table',
  options: ReadOnlyTable.options || {},
  storage: {},
  editor: null,
  type: schema.nodes.table,
  parent,
});

{
  check(
    'the table extension contributes no ProseMirror plugins (no tableEditing, no columnResizing)',
    ReadOnlyTable.config.addProseMirrorPlugins.call(
      tableContext(() => [
        { spec: 'tableEditing' },
        { spec: 'columnResizing' },
      ]),
    ).length === 0,
  );

  check(
    'the table extension contributes no keyboard shortcuts (Tab does not walk cells)',
    Object.keys(
      ReadOnlyTable.config.addKeyboardShortcuts.call(
        tableContext(() => ({ Tab: () => true })),
      ),
    ).length === 0,
  );

  check(
    'the table extension exposes no commands (insertTable is not reachable)',
    Object.keys(
      ReadOnlyTable.config.addCommands.call(
        tableContext(() => ({ insertTable: () => () => true })),
      ),
    ).length === 0,
  );

  // The one thing removing the editing must not cost.
  check(
    'the table nodes are still in the schema, and cells still hold blocks',
    !!schema.nodes.table &&
      !!schema.nodes.tableRow &&
      !!schema.nodes.tableCell &&
      !!schema.nodes.tableHeader &&
      schema.nodes.tableCell.spec.content === 'block+',
  );

  // The align attribute the Go side writes must exist on both cell nodes, or
  // the column renders left-aligned however the file spells it.
  check(
    'both cell nodes declare the align attribute',
    'align' in schema.nodes.tableCell.spec.attrs &&
      'align' in schema.nodes.tableHeader.spec.attrs,
  );
}

{
  // THE CRITICAL ONE. Backspace at position 0 of a fence is a cross-block
  // join — tr.join(8) is exactly the step ProseMirror's joinBackward builds —
  // and the plugin's documented limitation used to apply it as a real,
  // untracked deletion that destroyed the fence.
  const r = refused((tr) => tr.join(8), 9);
  check(
    'Backspace at the START of a fence cannot destroy it',
    r.blocked &&
      r.fences === 1 &&
      r.text === INTACT &&
      r.head === 9 &&
      r.kind === 'join',
    r,
  );
}

{
  // The same join from the other side: Delete pressed at the end of the
  // paragraph before the fence pulls the fence into it.
  const r = refused((tr) => tr.join(8), 7);
  check(
    'Delete at the END of the block before a fence cannot destroy it',
    r.blocked && r.fences === 1 && r.text === INTACT && r.head === 7,
    r,
  );
}

{
  // And from below: Backspace at the start of the paragraph after a fence
  // would otherwise append that prose to the fence's literal text.
  const r = refused((tr) => tr.join(21), 22);
  check(
    'Backspace at the START of the block after a fence cannot join it',
    r.blocked && r.fences === 1 && r.text === INTACT && r.head === 22,
    r,
  );
}

{
  const r = refused((tr) => tr.insertText('X', 14), 14);
  check(
    'typing inside a fence writes nothing',
    r.blocked && r.text === INTACT && r.kind === 'inside' && r.reason !== '',
    r,
  );
}

{
  // Position 0 of the fence text, the caret the destructive Backspace was
  // pressed from. An insertion here is INSIDE the fence even though it sits on
  // its boundary — the position the predicate is easiest to get wrong at.
  const r = refused((tr) => tr.insertText('X', 9), 9);
  check(
    'typing at the very start of a fence writes nothing',
    r.blocked && r.text === INTACT,
    r,
  );
}

{
  // The caret is the tell. This used to walk one position left per keystroke
  // while the document never changed.
  const r = refused((tr) => tr.delete(13, 14), 14);
  check(
    'Backspace inside a fence deletes nothing and does not drift the caret',
    r.blocked && r.text === INTACT && r.head === 14,
    r,
  );
}

{
  const r = refused((tr) => tr.delete(9, 15), 15);
  check(
    'deleting a selected range inside a fence is refused',
    r.blocked && r.text === INTACT,
    r,
  );
}

{
  const r = refused((tr) => tr.replaceWith(9, 15, schema.text('rm -rf')), 15);
  check(
    'typing over a selection inside a fence is refused',
    r.blocked && r.text === INTACT,
    r,
  );
}

{
  // Partial refusal is not on offer: a filter can only say no to the whole
  // transaction. A deletion running from prose THROUGH a fence into prose is
  // refused entire, which is the safe direction — the alternative rewrites a
  // fence.
  const r = refused((tr) => tr.delete(3, 24), 24);
  check(
    'a deletion spanning prose and a fence is refused entire',
    r.blocked && r.text === INTACT && r.fences === 1,
    r,
  );
}

{
  // Refusing a REMOTE change would not undo it — it would only make this
  // browser disagree with the CRDT every other peer holds. Silent divergence
  // is worse than the edit.
  const { state } = stateWithRefusals(fenced());
  const next = state.apply(
    (() => {
      const tr = state.tr;
      tr.setMeta(ySyncPluginKey, { isChangeOrigin: true });
      tr.insertText('Z', 14);
      return tr;
    })(),
  );
  check(
    'a remote change inside a fence still applies',
    next.doc.textContent === 'beforealphaZ bravoafter',
    next.doc.textContent,
  );
}

{
  // The refusal has to be DISCOVERABLE: the plugin hands the UI a reason and
  // the fence it belongs to, which is what puts a note on screen at the caret.
  const { state, refusals } = stateWithRefusals(fenced());
  state.apply(state.tr.insertText('X', 14));
  check(
    'a refused fence edit reports a reason and the fence position',
    refusals.length === 1 &&
      refusals[0].pos === 8 &&
      refusals[0].end === 21 &&
      /read-only/.test(refusals[0].reason),
    refusals.map((r) => [r.pos, r.end, r.reason]),
  );
}

{
  // The whole point of refusing only what rewrites a fence: prose either side
  // of one still edits exactly as prose does — applied directly, unmarked —
  // with the fence untouched between the two edits.
  const { state } = stateWithRefusals(fenced());
  const typed = state.apply(state.tr.insertText('X', 3));
  const deleted = typed.apply(typed.tr.delete(23, 25));
  const marked = [];
  deleted.doc.descendants((node) => {
    if (node.isText && node.marks.length) {
      marked.push([node.text, node.marks.map((m) => m.type.name).join('+')]);
    }
  });
  check(
    'prose either side of a fence is completely unaffected',
    deleted.doc.textContent === 'beXforealpha bravoter' &&
      same(marked, []) &&
      countFences(deleted.doc) === 1,
    { text: deleted.doc.textContent, marked },
  );
}

// --- a fence cannot be CREATED here either ---
//
// Turning off the ways this editor could MAKE a fence is what keeps the
// read-only rule from being a trap: ``` + space used to make an untracked,
// empty codeBlock that the reviewer then could not type in, delete, or
// un-fence. The three checks below are the three creation paths, and the fourth
// is the one that must NOT have been broken on the way — the node itself, which
// every fence in every document is parsed into.
//
// This drives the extension's config directly rather than through an Editor:
// input rules and keymaps need a view, the schema does not, and what is being
// asserted is what the extension CONTRIBUTES.

const codeBlockContext = (parent) => ({
  name: 'codeBlock',
  options: UncreatableCodeBlock.options || {},
  storage: {},
  editor: null,
  type: schema.nodes.codeBlock,
  parent,
});

{
  const rules = UncreatableCodeBlock.config.addInputRules.call(
    codeBlockContext(() => ['x', 'y']),
  );
  check(
    'the codeBlock extension contributes no input rules (``` and ~~~ are off)',
    Array.isArray(rules) && rules.length === 0,
    rules && rules.length,
  );
}

{
  // The inherited shortcuts are kept — they act INSIDE a fence and are refused
  // by the transaction filter with a note — except the one that CREATES (and
  // un-fences) one.
  const inherited = {
    'Mod-Alt-c': () => true,
    Backspace: () => true,
    Enter: () => true,
  };
  const kept = UncreatableCodeBlock.config.addKeyboardShortcuts.call(
    codeBlockContext(() => inherited),
  );
  check(
    'Mod-Alt-c is gone and the in-fence shortcuts are kept',
    !('Mod-Alt-c' in kept) && 'Backspace' in kept && 'Enter' in kept,
    Object.keys(kept),
  );
}

{
  // The extension's own plugin turns a paste carrying `vscode-editor-data`
  // into a new fence. Nothing else in the extension needs a plugin.
  const plugins = UncreatableCodeBlock.config.addProseMirrorPlugins.call(
    codeBlockContext(() => [{ spec: 'vscode' }]),
  );
  check(
    'the VS Code paste-to-fence handler is gone',
    Array.isArray(plugins) && plugins.length === 0,
    plugins && plugins.length,
  );
}

{
  // The extension the editor ACTUALLY installs is FiguredCodeBlock, which
  // extends the one above with a NodeView. Extending is how a mermaid fence
  // gets drawn as a diagram — and it is also the one way the fence guard could
  // be re-armed by accident, since `extend` merges config and a stray
  // addInputRules or addProseMirrorPlugins there would silently put ``` and the
  // VS Code paste handler back. So the guarantees are re-asserted on the
  // extension that ships, not only on its parent.
  const figured = (parent) => ({
    name: 'codeBlock',
    options: FiguredCodeBlock.options || {},
    storage: {},
    editor: null,
    type: schema.nodes.codeBlock,
    parent,
  });
  // `extend` keeps ONLY the overridden keys in config and reaches the rest
  // through `parent`, so "does not re-arm creation" is exactly "declares
  // neither of these, and its parent is the extension that removed them".
  check(
    'the figure extension is the uncreatable fence, extended',
    FiguredCodeBlock.parent === UncreatableCodeBlock,
  );
  check(
    'the figure NodeView does not re-arm fence creation',
    FiguredCodeBlock.config.addInputRules === undefined &&
      FiguredCodeBlock.config.addProseMirrorPlugins === undefined &&
      FiguredCodeBlock.config.addKeyboardShortcuts === undefined,
  );

  // And the NodeView itself only claims a MERMAID fence. Every other fence must
  // keep the plain <pre><code> rendering, because "select and copy still work"
  // is half the refusal and a picture of a shell script cannot be copied.
  const view = FiguredCodeBlock.config.addNodeView.call(figured(() => null));
  const plain = view({
    node: schema.node('codeBlock', { language: 'sh' }, [schema.text('ls')]),
  });
  check('a non-mermaid fence gets no NodeView at all', plain === null, plain);
}

{
  // The one thing disabling creation must not cost. `codeBlock: false` on
  // StarterKit removes the EXTENSION so this one can replace it; if it removed
  // the NODE, every fence in every document would be dropped on load — a far
  // worse bug than the one being fixed.
  const node = schema.nodes.codeBlock;
  check(
    'the codeBlock node is still in the schema, and still literal',
    !!node && node.spec.code === true && node.spec.marks === '',
    {
      present: !!node,
      code: node && node.spec.code,
      marks: node && node.spec.marks,
    },
  );
}

// A paragraph reading "The retryBudget value controls" whose middle word
// carries one formatting mark — three inlines, which is what makes this the
// case Gap 4 was reported for, now on the reviewer's own keyboard.
// A link needs an href to be a link at all; the rest take no attributes.
const formatting = (markName) =>
  schema.marks[markName].create(
    markName === 'link' ? { href: 'https://example.invalid' } : null,
  );

const formatted = (markName) =>
  docOf(
    schema.node('paragraph', null, [
      schema.text('The '),
      schema.text('retryBudget', [formatting(markName)]),
      schema.text(' value controls'),
    ]),
  );

const WHOLE = 'The retryBudget value controls';

// --- an edit across formatting applies whole, and leaves nothing behind ---
//
// This section used to be the burst/adjacency/run-stamp machinery — insRuns,
// withTickingClock, serverSpans, newRun's shape and degradation — because a
// typed edit across a bold run was three inlines and one decision, and only a
// browser-minted run could say so. The reviewer's-hand cut removed the marks
// the stamp existed for, so the machinery went with it (suggestions.ts keeps
// the history). What is worth pinning now is the inverse: a direct edit
// across any formatting boundary applies to every inline it covers, leaves no
// ins/del mark on any of them, and the count of pending decisions it creates
// is ZERO — not one, not three.

for (const markName of ['bold', 'italic', 'strike', 'link']) {
  {
    const st0 = stateOf(formatted(markName));
    const st = st0.apply(st0.tr.delete(1, 1 + WHOLE.length));
    check(
      `a deletion across ${markName} removes all three inlines, marks nothing, decides nothing`,
      st.doc.textContent === '' && markRuns(st.doc).length === 0,
      { text: st.doc.textContent, marks: markRuns(st.doc) },
    );
  }
  {
    // The replacement half. The new text keeps whatever formatting it was
    // given — formatting is the reviewer's to apply — and picks up no
    // suggestion mark.
    const st0 = stateOf(formatted(markName));
    const st = st0.apply(
      st0.tr.replaceWith(1, 1 + WHOLE.length, [
        schema.text('one '),
        schema.text('two', [formatting(markName)]),
      ]),
    );
    check(
      `typing over a selection across ${markName} replaces it, unmarked`,
      st.doc.textContent === 'one two' && markRuns(st.doc).length === 0,
      { text: st.doc.textContent, marks: markRuns(st.doc) },
    );
  }
}

{
  // THE CROSSING DELETES THE CODE WORD TOO. Under tracked typing the code
  // span was EXCLUDED from the strike (`excludes: '_'` kept `del` off it):
  // the word was re-inserted unmarked, its deletion recorded nowhere — Gap 6,
  // symptom 1, pinned here for two rounds. Direct deletion closes it: the
  // whole phrase goes, code word included, because that is what deleting a
  // phrase means. typing.mjs asserts the same thing from a real keyboard.
  const st0 = stateOf(formatted('code'));
  const st = st0.apply(st0.tr.delete(1, 1 + WHOLE.length));
  check(
    'a deletion crossing a code span deletes the code word with its phrase (Gap 6 closed)',
    st.doc.textContent === '' && markRuns(st.doc).length === 0,
    { text: st.doc.textContent, marks: markRuns(st.doc) },
  );
}

// --- inline `code` IS LITERAL TOO, and it is a MARK, not a region ---
//
// The fence's second and third breaches were live inside an inline code span
// with no guard at all: typing in one reached the file as real untracked text,
// and deleting in one was a silent no-op. literalHit could not see either,
// because it walks nodesBetween and asks a NODE question — a fence and a table
// are nodes, and a code span is a mark on text inside a paragraph.
//
// THE ASYMMETRY IS THE WHOLE DESIGN, and most of what is below is its NEGATIVE
// half. A fence refuses `inside` and `join`; a code span refuses `inside` only,
// because a mark has no boundary to be joined across and an edit running
// THROUGH one is the reviewer deleting a phrase that happens to contain a code
// word. An over-broad guard here trades a silent bug met rarely for a loud
// obstruction met constantly, and the loud one is worse. The three cases that
// catch it are the crossing deletion, the selection that merely touches an
// edge, and the one that starts inside and ends outside.

// 'The ' is [1,5), 'retryBudget' is [5,16), ' value controls' is [16,31).
const CODE_DOC = () => formatted('code');

{
  const doc = CODE_DOC();
  const kindAt = (from, to) => {
    const hit = literalHit(doc, from, to);
    return hit ? `${hit.what}:${hit.kind}` : 'none';
  };
  // The same table the fence gets, and for the same reason: every one of these
  // is an off-by-one waiting to happen. Here half of them must answer 'none'
  // ON PURPOSE, which is the half a well-meaning widening breaks first.
  const table = {
    'insert in the prose before': kindAt(3, 3),
    'insert at the leading edge of the code span': kindAt(5, 5),
    'insert mid-code': kindAt(10, 10),
    'insert at the trailing edge of the code span': kindAt(16, 16),
    'insert in the prose after': kindAt(20, 20),
    'delete inside the prose before': kindAt(2, 4),
    'delete ending exactly at the leading edge': kindAt(2, 5),
    'delete starting exactly at the trailing edge': kindAt(16, 19),
    'delete inside the code span': kindAt(8, 11),
    'delete the code span entire': kindAt(5, 16),
    'delete from prose into the code span': kindAt(3, 10),
    'delete from inside the code span into prose': kindAt(10, 20),
    'delete from prose through the code span into prose': kindAt(1, 31),
  };
  const want = {
    'insert in the prose before': 'none',
    // The caret at either edge sits between prose and code, and typing in that
    // gap is not typing in the span. The trailing edge carries a MEASURED
    // residual — the last check in this section.
    'insert at the leading edge of the code span': 'none',
    'insert mid-code': 'code:inside',
    'insert at the trailing edge of the code span': 'none',
    'insert in the prose after': 'none',
    'delete inside the prose before': 'none',
    // TOUCHING IS NOT ENTERING. A selection ending where the code begins, or
    // beginning where it ends, takes no code character with it.
    'delete ending exactly at the leading edge': 'none',
    'delete starting exactly at the trailing edge': 'none',
    'delete inside the code span': 'code:inside',
    // A code span is not editable, and that includes being editable to zero.
    'delete the code span entire': 'code:inside',
    // THE THREE THAT MUST STAY ACCEPTED — each is a reviewer deleting prose
    // that happens to touch a code word.
    'delete from prose into the code span': 'none',
    'delete from inside the code span into prose': 'none',
    'delete from prose through the code span into prose': 'none',
  };
  check(
    'literalHit answers every code-span boundary correctly',
    same(table, want),
    table,
  );

  const hit = literalHit(doc, 8, 11);
  check(
    'a code hit says CODE_INSIDE, carries CODE_HINT, and names NO node',
    !!hit &&
      hit.reason === CODE_INSIDE &&
      hit.hint === CODE_HINT &&
      hit.node === null,
    hit && { reason: hit.reason, hint: hit.hint, node: hit.node },
  );
  // The refusal note is positioned from `pos`, so it has to name the whole code
  // span rather than the three characters the keystroke touched.
  check(
    'a code hit spans the whole code run, not the edited part',
    !!hit && hit.pos === 5 && hit.end === 16,
    hit && [hit.pos, hit.end],
  );
}

check(
  "isCodeText knows code-marked text from prose and from a fence's text",
  isCodeText(schema.text('x', [schema.marks.code.create(null)])) &&
    !isCodeText(schema.text('x')) &&
    !isCodeText(fence('x')) &&
    !isCodeText(null),
);

check(
  "the code span says its own sentence, not the fence's or the table's",
  CODE_INSIDE !== FENCE_INSIDE &&
    CODE_INSIDE !== TABLE_INSIDE &&
    CODE_HINT !== FENCE_HINT &&
    CODE_HINT !== TABLE_HINT,
);

// SUGGESTED counts only suggestion marks, and the distinction is load-bearing
// HERE and nowhere else in this file: every fixture in this section is
// code-marked already, so `marks !== ''` is true of text that is merely
// formatted. The question is whether an edit left a PROPOSAL behind — which,
// for the reviewer's own hand, must now always be no.
const SUGGESTED = /(^|\+)(ins|del|highlight):/;
const suggested = (state) =>
  runs(state).filter(([, marks]) => SUGGESTED.test(marks));

// refusedIn drives one transaction against a document and reports what the
// FILTER made of it — the same shape `refused` uses for a fence, over a
// document whose literal thing is a mark rather than a node.
function refusedIn(doc, fill) {
  const { state, refusals } = stateWithRefusals(doc);
  const tr = state.tr;
  fill(tr);
  const next = state.apply(tr);
  return {
    blocked: next === state,
    text: next.doc.textContent,
    marked: suggested(next),
    reason: refusals.length ? refusals[0].reason : '',
    kind: refusals.length ? refusals[0].kind : '',
    what: refusals.length ? refusals[0].what : '',
  };
}

{
  // SYMPTOM 2, the severe one. Before the refusal this produced
  // ["retryXXBudget", "code"] — real text in the author's file with nothing in
  // /_galley/pending, because addToSet will not put `ins` on code text.
  const r = refusedIn(CODE_DOC(), (tr) => tr.insertText('XX', 10));
  check(
    'typing INSIDE a code span is refused, and nothing reaches the document',
    r.blocked &&
      r.text === WHOLE &&
      r.reason === CODE_INSIDE &&
      r.kind === 'inside' &&
      r.what === 'code',
    r,
  );
}

{
  // SYMPTOM 3. Before the refusal this was a silent no-op: the text removed and
  // re-inserted unmarked, the document byte-identical, only the caret moved.
  // `marked` is empty either way, which is exactly why the document alone could
  // never tell the two apart — the refusal is what the check reads.
  const r = refusedIn(CODE_DOC(), (tr) => tr.delete(8, 11));
  check(
    'deleting INSIDE a code span is refused instead of silently doing nothing',
    r.blocked &&
      r.text === WHOLE &&
      r.marked.length === 0 &&
      r.reason === CODE_INSIDE &&
      r.kind === 'inside',
    r,
  );
}

{
  const r = refusedIn(CODE_DOC(), (tr) => tr.delete(5, 16));
  check(
    'deleting the WHOLE code span is refused too — not editable, including to zero',
    r.blocked && r.text === WHOLE && r.reason === CODE_INSIDE,
    r,
  );
}

{
  // THE CHECK THAT MATTERS MOST. If this ever fails, the guard has traded a
  // silent bug for a loud obstruction, and the reviewer meets the loud one
  // every time a sentence they want gone contains a code word. The crossing
  // now APPLIES — the phrase is gone, code word included, and nothing was
  // marked, because a deliberate deletion needs no decision.
  const r = refusedIn(CODE_DOC(), (tr) => tr.delete(1, 1 + WHOLE.length));
  check(
    'a deletion CROSSING a code span is still accepted — and applies whole',
    !r.blocked && r.reason === '' && r.text === '' && r.marked.length === 0,
    r,
  );
}

{
  const before = refusedIn(CODE_DOC(), (tr) => tr.delete(2, 5));
  const after = refusedIn(CODE_DOC(), (tr) => tr.delete(16, 19));
  check(
    'a selection that merely TOUCHES a code span at either edge is not refused',
    !before.blocked &&
      !after.blocked &&
      before.reason === '' &&
      after.reason === '',
    { before, after },
  );
}

{
  const r = refusedIn(CODE_DOC(), (tr) => tr.delete(10, 20));
  check(
    'a deletion starting INSIDE and ending OUTSIDE is a crossing, not an inside',
    !r.blocked && r.reason === '',
    r,
  );
}

{
  // THE HINT HAS TO BE TRUE, so it is checked rather than asserted in prose.
  // "the prose around it still edits" — and "a phrase containing it can still
  // be struck" is the crossing check above.
  const typed = refusedIn(CODE_DOC(), (tr) => tr.insertText('XX', 3));
  check(
    'CODE_HINT is true: the prose beside a code span still edits, unmarked',
    !typed.blocked && typed.text.includes('XX') && typed.marked.length === 0,
    typed,
  );
}

{
  const sel = (state, from, to) =>
    state.apply(
      state.tr.setSelection(TextSelection.create(state.doc, from, to)),
    );
  const inside = EditorState.create({ schema, doc: CODE_DOC() });
  // The one predicate, asked directly now that the Strike button (and its
  // strikeRefusal wrapper) is retired with it: a selection inside a code span
  // is the keyboard's refusal, and one that crosses it is not.
  const hitInside = literalHit(sel(inside, 8, 11).doc, 8, 11);
  check(
    "a selection inside a code span refuses with the keyboard's sentence",
    hitInside !== null && hitInside.reason === CODE_INSIDE,
    hitInside,
  );
  check(
    'a selection that crosses a code span is not refused',
    literalHit(inside.doc, 1, 1 + WHOLE.length) === null,
    literalHit(inside.doc, 1, 1 + WHOLE.length),
  );

  // A COMMENT IS ANCHORED BY THE SERVER, which builds the highlight with an
  // explicit mark array and so skips exclusion entirely. Measured with the CLI
  // before the exemption in composerPlacement was written: `galley suggest
  // --comment --on retryBudget` over "The `retryBudget` value controls" writes
  // {==`retryBudget`==} and `galley pending` lists it. So Comment stays offered
  // on a code span while Strike is disabled with the reason on it — the true
  // state of affairs, and denying the comment would take away something that
  // works today, which is the one thing a refusal must never do.
  const placed = composerPlacement(sel(inside, 5, 16), null);
  check(
    'a comment is still OFFERED on a code span — the exemption stands',
    placed.action === 'place' &&
      placed.denied === false &&
      placed.denyReason === '',
    placed,
  );
  // And the regions are untouched by that exemption: a fence has nowhere for
  // the SERVER to anchor either, so it still denies outright.
  const fenceState = EditorState.create({ schema, doc: fenced() });
  const denied = composerPlacement(sel(fenceState, 10, 13), null);
  check(
    'a fence still DENIES the comment outright',
    denied.action === 'place' &&
      denied.denied === true &&
      denied.denyReason === FENCE_INSIDE,
    denied,
  );
}

{
  // THE MEASURED RESIDUAL, pinned rather than refused, and SMALLER than it
  // was. ResolvedPos.marks() takes the marks of the node BEFORE the caret and
  // `code` is inclusive, so text typed at exactly the trailing edge lands
  // code-marked — new prose rendered and serialized as code. (Under tracked
  // typing this also made it unmarkable and therefore untracked; direct
  // application removed that half and left this cosmetic half.) Refusing it
  // would mean a reviewer cannot type after a code word that ends a
  // paragraph, which is the wall this whole rule exists to avoid. The honest
  // fix is a schema decision about what `code` excludes; if someone makes it,
  // this check fails and tells them the client was never the reason.
  const st0 = stateOf(CODE_DOC());
  const st = st0.apply(st0.tr.insertText('XX', 16));
  const typed = runs(st).find(([text]) => text.includes('XX'));
  check(
    "typing at a code span's trailing edge STILL lands code-marked",
    !!typed && typed[0] === 'retryBudgetXX' && !SUGGESTED.test(typed[1]),
    typed,
  );
  // The leading edge does not share the defect: marks() finds prose before it.
  const lead = stateOf(CODE_DOC());
  const led = lead.apply(lead.tr.insertText('XX', 5));
  check(
    "typing at a code span's LEADING edge lands as plain prose, unmarked",
    runs(led).some(([text, marks]) => text.includes('XX') && marks === ''),
    runs(led),
  );
}

// --- finding a suggestion in the document ---
//
// markRuns is what BOTH directions of the card↔mark mapping run through: the
// bubble asking which suggestion a click landed in, and the panel asking where
// a card's text is and whether it is on screen. It has to group exactly the
// way internal/suggest's `span` does, or the panel scrolls to the wrong text.

const insMark = (author, at) => schema.marks.ins.create({ author, at });
const delMark = (author, at) => schema.marks.del.create({ author, at });

{
  // Two ins runs from the same author at the same instant, separated by plain
  // text, are TWO suggestions — adjacency, not similarity.
  const m = insMark('court', '2026-01-01T00:00:00Z');
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('one', [m]),
      schema.text(' gap '),
      schema.text('two', [m]),
    ]),
  );
  const runs = markRuns(doc);
  check(
    'markRuns splits identical marks that are not adjacent',
    runs.length === 2 && runs[0].text === 'one' && runs[1].text === 'two',
    runs.map((r) => r.text),
  );
}

{
  // Adjacent inlines sharing a mark are ONE run even when other formatting
  // splits them into two nodes — half an insertion in bold is one suggestion,
  // and the server matches on the whole run's text.
  const m = insMark('court', '2026-01-01T00:00:00Z');
  const bold = schema.marks.bold.create();
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('plain', [m]),
      schema.text('bold', [m, bold]),
    ]),
  );
  const runs = markRuns(doc);
  check(
    'markRuns joins one run split by other formatting',
    runs.length === 1 && runs[0].text === 'plainbold',
    runs.map((r) => r.text),
  );
}

{
  // Same text, same position, two authors: two runs, and each must find only
  // its own card. A panel that matched on text alone would scroll to
  // whichever came first.
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('mine', [insMark('court', '2026-01-01T00:00:00Z')]),
      schema.text('mine', [insMark('agent', '2026-01-01T00:00:01Z')]),
    ]),
  );
  const runs = markRuns(doc);
  const card = { kind: 'insert', text: 'mine', author: 'agent' };
  const hits = runs.filter((r) => sameSuggestion(r, card));
  check(
    'sameSuggestion separates identical text by different authors',
    runs.length === 2 && hits.length === 1 && hits[0].author === 'agent',
    runs.map((r) => [r.text, r.author]),
  );
}

{
  // The card→mark hop the panel actually makes: a suggestion as
  // /_galley/pending reports it, resolved to a position in this document.
  // markRuns must report positions that resolve back to the same text.
  const doc = docOf(
    para('first paragraph'),
    schema.node('paragraph', null, [
      schema.text('before '),
      schema.text('struck', [delMark('agent', '2026-01-01T00:00:00Z')]),
      schema.text(' after'),
    ]),
  );
  const card = { kind: 'delete', text: 'struck', author: 'agent' };
  const run = markRuns(doc).find((r) => sameSuggestion(r, card));
  check(
    'a panel card resolves to the position of its own text',
    !!run && doc.textBetween(run.from, run.to) === 'struck',
    run && [run.from, run.to, doc.textBetween(run.from, run.to)],
  );
}

{
  // A SUBSTITUTION'S CARD MUST STILL FIND ITS MARK. "{~~brown~>red~~}" is one
  // pending item — kind 'replace', text "brown → red" — but the fragment holds
  // a Del and an Ins, and nothing in it reads "brown → red". A mark the
  // reviewer typed carries no run until the next server mutation mints one, so
  // the id-less fallback is the only route, and without loosePeer it finds
  // nothing and the card goes adrift beside text plainly on screen.
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('The quick '),
      schema.text('brown', [delMark('court', '2026-01-01T00:00:00Z')]),
      schema.text('red', [insMark('court', '2026-01-01T00:00:00Z')]),
      schema.text(' fox'),
    ]),
  );
  const card = {
    kind: 'replace',
    text: 'brown → red',
    old: 'brown',
    new: 'red',
    author: 'court',
  };
  const runs = markRuns(doc);
  check(
    'a replace card reads like no mark in the document',
    runs.filter((r) => sameSuggestion(r, card)).length === 0,
    runs.map((r) => r.text),
  );
  const hits = runs.filter((r) => sameSuggestion(r, loosePeer(card)));
  check(
    'a replace card resolves to its DELETED half — the same half its run is',
    hits.length === 1 && doc.textBetween(hits[0].from, hits[0].to) === 'brown',
    hits.map((r) => [r.kind, r.text]),
  );
  // Everything else is handed straight back, so the fallback keeps behaving
  // exactly as it did for every other kind.
  const plain = { kind: 'delete', text: 'struck', author: 'agent' };
  check('loosePeer leaves every other kind alone', loosePeer(plain) === plain);
  check('loosePeer of nothing is nothing', loosePeer(null) === null);
}

{
  // ONE SPAN IN THE FILE IS ONE DECISION EVERYWHERE — INCLUDING WHEN THE
  // REVIEWER CLICKS ITS GREEN HALF.
  //
  // The block above is the panel's direction (card → mark) for an UNSTAMPED
  // substitution. This is the BUBBLE's direction (mark → card) for a stamped
  // one, which is the ordinary live case and the one that was broken: the
  // wire's `run` is the deleted half's, the inserted half carries a run of its
  // own, and a click on the added text matched NOTHING. Measured in a real
  // browser: `INSERTION · AGENT · JUST NOW`, "the server has not seen this one
  // yet — it lands on the next save", and no verbs — about `s1`, one of the
  // three pending, with its card 400px away carrying ✓ accept. The red half of
  // the same span gave `DELETION` and two working verbs.
  //
  // The pairing is READ, not re-derived: `insRun` is suggest.Pending's, carried
  // by the side that ran the serializer's own walk.
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('Retry '),
      schema.text('three times with a fixed', [
        delMark('agent', '2026-08-16T09:00:00Z'),
      ]),
      schema.text('up to three times with an exponential', [
        insMark('agent', '2026-08-16T09:00:00Z'),
      ]),
      schema.text(' backoff.'),
    ]),
  );
  // Stamped, the way a live session's fragment is: two runs, one per half.
  const stamped = docOf(
    schema.node('paragraph', null, [
      schema.text('Retry '),
      schema.text('three times with a fixed', [
        schema.marks.del.create({
          author: 'agent',
          at: '2026-08-16T09:00:00Z',
          run: 'del-1',
        }),
      ]),
      schema.text('up to three times with an exponential', [
        schema.marks.ins.create({
          author: 'agent',
          at: '2026-08-16T09:00:00Z',
          run: 'ins-1',
        }),
      ]),
      schema.text(' backoff.'),
    ]),
  );
  const runs = markRuns(stamped);
  check(
    'the fixture really holds two stamped halves',
    runs.length === 2 && runs[0].runId === 'del-1' && runs[1].runId === 'ins-1',
    runs.map((r) => [r.kind, r.runId]),
  );
  // What /_galley/pending reports for that span: ONE entry, addressed by the
  // deleted half, carrying the inserted half's run beside it.
  const entry = {
    id: 's1',
    kind: 'replace',
    run: 'del-1',
    insRun: 'ins-1',
    author: 'agent',
    old: 'three times with a fixed',
    new: 'up to three times with an exponential',
    text: 'three times with a fixed → up to three times with an exponential',
    decidable: true,
  };
  const clicked = (i) => ({
    kind: runs[i].kind,
    text: runs[i].text,
    author: runs[i].author,
    run: runs[i].runId,
  });
  check(
    'clicking the RED half finds the one suggestion, as it always did',
    [entry].filter((p) => sameSuggestion(p, clicked(0))).length === 1,
  );
  check(
    'clicking the GREEN half finds THE SAME ONE — not nothing, and not a second',
    [entry].filter((p) => sameSuggestion(p, clicked(1))).length === 1,
    [entry].filter((p) => sameSuggestion(p, clicked(1))),
  );
  // And it resolves to the entry itself, so the head can print the SERVER's
  // kind (`replacement`) rather than the mark's (`insertion`) and the press can
  // post the DELETED half's run.
  const hit = [entry].find((p) => sameSuggestion(p, clicked(1)));
  check(
    'and the decision it resolves to is the replace, addressed by the deleted half',
    hit.kind === 'replace' && hit.run === 'del-1' && hit.id === 's1',
    hit,
  );
  // NO INSRUN, NO WIDENING. A payload from an older server carries none, and a
  // click on the green half then finds nothing — which is what it always did,
  // not a new failure, and is why this is a field rather than a guess.
  const noPair = { ...entry, insRun: undefined };
  check(
    'an entry with no inserted-half address widens nothing',
    [noPair].filter((p) => sameSuggestion(p, clicked(1))).length === 0,
  );
  // AND IT DOES NOT MATCH A STRANGER. `insRun` is one string and identity, not
  // a family resemblance.
  const other = { ...entry, insRun: 'ins-9' };
  check(
    'the widening is by identity — another run is still another run',
    [other].filter((p) => sameSuggestion(p, clicked(1))).length === 0,
  );
  check(
    'the unstamped fixture is untouched by any of this',
    markRuns(doc).length === 2,
  );
}

{
  // Two marks on one run (a comment anchored on an insertion) are two
  // suggestions and must both be found — collecting per NODE rather than per
  // kind would report only one of them.
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('both', [
        insMark('court', '2026-01-01T00:00:00Z'),
        schema.marks.highlight.create({
          author: 'agent',
          at: '2026-01-01T00:00:02Z',
        }),
      ]),
    ]),
  );
  const kinds = markRuns(doc)
    .map((r) => r.kind)
    .sort();
  check(
    'markRuns reports both marks on a doubly-marked run',
    same(kinds, ['comment', 'insert']),
    kinds,
  );
}

// (The toolbar's strike section retired with the button — the trail cut.
// A strike was Backspace's own deletion under the reviewer's-hand contract,
// so the button was a second spelling of the keyboard; the literal-region
// refusals it shared are pinned above through literalHit, and typing.mjs
// drives the keyboard's own deletion for real.)

// --- the toolbar must not show a STALE verdict ---
//
// The selection toolbar keeps a guard so that an unchanged selection re-firing
// does not reset an open comment form mid-sentence. That guard used to be
// keyed on the selection's TEXT, which cannot distinguish two ranges that read
// the same — so moving between the same phrase as prose and inside a code
// fence skipped the recompute entirely and the toolbar kept the wrong
// verdict. Both directions were wrong, so both are checked (they were found
// through the since-retired Strike button's refusal; the deny half they pin
// now is the comment affordance's and is exactly as live).

{
  // [paragraph "rm -rf /"][codeBlock "rm -rf /"] — identical text, and the
  // only thing telling the two selections apart is where they are.
  const phrase = 'rm -rf /';
  const doc = docOf(
    para(phrase),
    schema.node('codeBlock', null, [schema.text(phrase)]),
  );
  const prose = [1, 1 + phrase.length];
  const fence = [prose[1] + 2, prose[1] + 2 + phrase.length];
  const at = ([from, to]) => {
    const st = stateOf(doc);
    return st.apply(st.tr.setSelection(TextSelection.create(doc, from, to)));
  };

  check(
    'the two ranges really do read the same',
    doc.textBetween(prose[0], prose[1]) === phrase &&
      doc.textBetween(fence[0], fence[1]) === phrase,
    [doc.textBetween(prose[0], prose[1]), doc.textBetween(fence[0], fence[1])],
  );

  const first = composerPlacement(at(prose), null);
  check(
    'a plain selection places the toolbar, denied nothing',
    first.action === 'place' &&
      first.denied === false &&
      first.denyReason === '',
    first,
  );

  // prose → fence. Under the stale guard this returned "keep", leaving the
  // comment offered over a fence it would only fail against after the click.
  const toFence = composerPlacement(at(fence), first.key);
  check(
    'moving to identically-worded text in a FENCE recomputes the deny',
    toFence.action === 'place' &&
      toFence.denied === true &&
      /code fence/.test(toFence.denyReason || ''),
    toFence,
  );

  // fence → prose. Under the stale guard this returned "keep" too, denying
  // the comment over text that anchors perfectly well.
  const toProse = composerPlacement(at(prose), toFence.key);
  check(
    'moving back to identically-worded PROSE clears the deny',
    toProse.action === 'place' && toProse.denied === false,
    toProse,
  );

  // The two placements must also carry different coordinates to position from,
  // or the toolbar would recompute the verdict and still sit over the old
  // selection.
  check(
    'the two placements position from different points',
    toFence.from !== toProse.from,
    [toFence.from, toProse.from],
  );

  // The guard still has to DO something: an unchanged selection must not
  // re-place, or an open comment form would be reset out from under someone.
  check(
    'an unchanged selection is left alone',
    composerPlacement(at(prose), toProse.key).action === 'keep',
    composerPlacement(at(prose), toProse.key),
  );
}

{
  const doc = docOf(para('hello'));
  const st = stateOf(doc);
  check(
    'an empty selection hides the toolbar',
    composerPlacement(
      st.apply(st.tr.setSelection(TextSelection.create(doc, 3))),
      null,
    ).action === 'hide',
  );
}

// --- the trail: what the reviewer's hand did (web/trail.ts) ---
//
// The reviewer's edits apply directly and the trail is their record: text
// steps become entries, entries become decorations and a log, an undone edit
// retracts, and re-anchoring after a reload refuses rather than guesses.
// Everything here is the pure half; typing.mjs drives the same contract from
// a real keyboard and the layers pass reads the paint.

// unplacedEntry builds an entry with no position yet — what settleEntries'
// retryAdrift pass is handed when an entry lost its place. `placed` is left
// absent unless the fixture states it, which is an entry no settle has
// written: the case the set rule must leave out of the order.
let unplacedIds = 1000;
const unplacedEntry = (c) => {
  unplacedIds += 1;
  return {
    id: unplacedIds,
    old: c.old || '',
    new: c.new || '',
    blockKey: c.blockKey || '',
    prefix: c.prefix || '',
    suffix: c.suffix || '',
    before: typeof c.before === 'string' ? c.before : null,
    after: typeof c.after === 'string' ? c.after : null,
    ...(typeof c.placed === 'boolean' ? { placed: c.placed } : {}),
    anchored: false,
    from: null,
    to: null,
  };
};

{
  // recordOf: what one step is to the trail.
  const st = stateOf(docOf(para('hello world')));
  const ins = st.tr.insertText('XY', 3);
  check(
    'a typed insertion records {old:"", new}',
    JSON.stringify(recordOf(st.doc, ins.steps[0])) ===
      JSON.stringify({ from: 3, to: 3, old: '', ins: 'XY' }),
    recordOf(st.doc, ins.steps[0]),
  );
  const del = st.tr.delete(1, 6);
  check(
    'a deletion records the removed text',
    JSON.stringify(recordOf(st.doc, del.steps[0])) ===
      JSON.stringify({ from: 1, to: 6, old: 'hello', ins: '' }),
    recordOf(st.doc, del.steps[0]),
  );
  const rep = st.tr.insertText('Z', 2, 4);
  check(
    'typing over a selection records both halves',
    JSON.stringify(recordOf(st.doc, rep.steps[0])) ===
      JSON.stringify({ from: 2, to: 4, old: 'el', ins: 'Z' }),
    recordOf(st.doc, rep.steps[0]),
  );

  // The skips, each the contract's own: structure and formatting are not
  // trailed (text steps only), and a cross-block deletion is structure.
  const split = st.tr.split(3);
  check(
    'a block split records nothing — structure is not trailed',
    recordOf(st.doc, split.steps[0]) === null,
  );
  const marked = st.tr.addMark(1, 6, schema.marks.bold.create());
  check(
    'a mark toggle records nothing — formatting is not trailed',
    recordOf(st.doc, marked.steps[0]) === null,
  );
  const two = stateOf(docOf(para('one'), para('two')));
  const join = two.tr.delete(2, 7);
  check(
    'a cross-block deletion records nothing',
    recordOf(two.doc, join.steps[0]) === null,
  );
}

{
  // applyRecord: one region, one story. Typing coalesces; deleting what was
  // typed shrinks the entry; a region restored by hand records NOTHING —
  // the trail is what the reviewer did, not the noise of their fingers
  // getting there.
  const doc = docOf(para('hello world'));
  const first = applyRecord([], doc, { from: 3, to: 3, old: '', ins: 'XY' });
  check(
    'a fresh edit opens an entry at its own range',
    first.keep.length === 0 &&
      first.merged &&
      first.merged.new === 'XY' &&
      first.merged.from === 3 &&
      first.merged.to === 5 &&
      first.merged.old === '',
    first.merged,
  );

  // The next keystroke lands at the entry's edge in the NEW doc; the merge is
  // asked against that doc.
  const doc2 = docOf(para('heXYllo world'));
  const grown = applyRecord([first.merged], doc2, {
    from: 5,
    to: 5,
    old: '',
    ins: 'Z',
  });
  check(
    'an adjacent keystroke continues the entry rather than opening a second',
    grown.keep.length === 0 &&
      grown.merged &&
      grown.merged.new === 'XYZ' &&
      grown.merged.from === 3 &&
      grown.merged.to === 6,
    grown.merged,
  );

  const doc3 = docOf(para('heXYZllo world'));
  const undone = applyRecord([grown.merged], doc3, {
    from: 3,
    to: 6,
    old: 'XYZ',
    ins: '',
  });
  check(
    'typing and then deleting it all leaves NO trail — nothing was done',
    undone.keep.length === 0 && undone.merged === null,
    undone,
  );

  // Delete, then type in the hole: one substitution, not two entries.
  const ghost = applyRecord([], doc, { from: 1, to: 6, old: 'hello', ins: '' });
  check(
    'a pure deletion is a zero-width entry carrying its old text',
    ghost.merged &&
      ghost.merged.old === 'hello' &&
      ghost.merged.new === '' &&
      ghost.merged.from === 1 &&
      ghost.merged.to === 1,
    ghost.merged,
  );
  const hole = docOf(para(' world'));
  const swapped = applyRecord([ghost.merged], hole, {
    from: 1,
    to: 1,
    old: '',
    ins: 'HI',
  });
  check(
    'typing into a deletion reads as one substitution',
    swapped.merged &&
      swapped.merged.old === 'hello' &&
      swapped.merged.new === 'HI' &&
      swapped.merged.from === 1 &&
      swapped.merged.to === 3,
    swapped.merged,
  );

  // AN ENTRY IS ITS MINIMAL DIFF — canonicalized at record time, shared
  // affixes trimmed — because the undo's own diff is minimal and the two must
  // live in one coordinate system or retraction can never match. Found by
  // review: overtyping "brown" with "blue" stored {old:"brown", new:"blue"},
  // the undo diffed to {removed:"lue", inserted:"rown"}, nothing paired, and
  // the entry survived as a permanent adrift phantom the agent read as
  // decided fact.
  const overDoc = docOf(para('brown fox'));
  const over = applyRecord([], overDoc, {
    from: 1,
    to: 6,
    old: 'brown',
    ins: 'blue',
  });
  check(
    'an overtype is stored as its minimal diff — shared affixes trimmed',
    over.merged &&
      over.merged.old === 'rown' &&
      over.merged.new === 'lue' &&
      over.merged.from === 2 &&
      over.merged.to === 5,
    over.merged,
  );

  // And a hand-restore of ORIGINAL text drops the entry the same way.
  const typedOver = docOf(para('XXllo world'));
  const entry = { ...first.merged, old: 'he', new: 'XX', from: 1, to: 3 };
  const restored = applyRecord([entry], typedOver, {
    from: 1,
    to: 3,
    old: 'XX',
    ins: 'he',
  });
  check(
    'restoring the original text by hand retracts the entry',
    restored.keep.length === 0 && restored.merged === null,
    restored,
  );
}

{
  // diffOf: an undo is NOT a small step — y-prosemirror renders one as a
  // whole-region replace (measured: undoing two characters arrived as a
  // replace of the whole document), so the pairing runs on the transaction's
  // DIFF. These drive the reduction over the real measured shape.
  const before = docOf(para('Undo'), para('A QQsecond paragraph.'));
  const after = docOf(para('Undo'), para('A second paragraph.'));
  const d = diffOf(before, after);
  check(
    'a whole-document undo reduces to the two characters it removed',
    d !== null &&
      d.removed === 'QQ' &&
      d.inserted === '' &&
      before.textBetween(d.from, d.to) === 'QQ',
    d,
  );
  check('identical documents diff to nothing', diffOf(before, before) === null);
  // The overlap clamp: undoing "aa" out of "aaa" — diff start and end cross.
  const aaa = docOf(para('xaaay'));
  const xa = docOf(para('xay'));
  const clamped = diffOf(aaa, xa);
  check(
    'an overlapping diff clamps rather than crossing itself',
    clamped !== null &&
      clamped.to - clamped.from === 2 &&
      clamped.inserted === '' &&
      aaa.textBetween(clamped.from, clamped.to) === 'aa',
    clamped,
  );
}

{
  // trimAffixes: the canonical form's own arithmetic, surrogate guards
  // included — an entry boundary must never land inside an emoji's pair.
  check(
    'shared affixes trim to the minimal diff',
    JSON.stringify(trimAffixes('brown', 'blue')) ===
      JSON.stringify({ start: 1, old: 'rown', next: 'lue' }),
    trimAffixes('brown', 'blue'),
  );
  check(
    'a pure insertion and a pure deletion trim to themselves',
    trimAffixes('', 'XY').next === 'XY' &&
      trimAffixes('hello', '').old === 'hello',
  );
  const emoji = trimAffixes('a\u{1F600}b', 'a\u{1F601}b');
  check(
    'the trim never splits a surrogate pair',
    emoji.old === '\u{1F600}' &&
      emoji.next === '\u{1F601}' &&
      emoji.start === 1,
    emoji,
  );
}

{
  // expandToWord: STORAGE IS MINIMAL, DISPLAY IS A WORD. The stored entry is
  // the minimal diff because retraction needs it (trimAffixes above); every
  // surface that SHOWS one rejoins the affixes the trim took, onto BOTH sides,
  // out to the nearest whitespace-or-punctuation boundary. Pure, so the rail's
  // rows, the sheet's rows and the inline ghost cannot each grow their own
  // idea of what the reviewer changed.
  const could = expandToWord(
    { old: 'c', new: 'sh' },
    { prefix: 'it ', suffix: 'ould be fine' },
  );
  check(
    'the reviewer\'s own case: {old:"c", new:"sh"} inside "should" displays could → should',
    could.old === 'could' &&
      could.new === 'should' &&
      could.lead === '' &&
      could.tail === 'ould',
    could,
  );
  const teh = expandToWord(
    { old: 'eh', new: 'he' },
    { prefix: 'and t', suffix: ' rest' },
  );
  check(
    'an affix on the LEFT rejoins too — teh → the',
    teh.old === 'teh' && teh.new === 'the' && teh.lead === 't',
    teh,
  );
  const wrd = expandToWord(
    { old: '', new: 'o' },
    { prefix: 'the w', suffix: 'rd here' },
  );
  check(
    'a pure insertion inside a word displays the whole word on both sides — wrd → word',
    wrd.old === 'wrd' && wrd.new === 'word',
    wrd,
  );
  const whole = expandToWord(
    { old: 'brown', new: 'blue' },
    { prefix: 'The quick ', suffix: ' fox' },
  );
  check(
    'an entry already at word boundaries displays ITSELF — expansion is identity there',
    whole.old === 'brown' &&
      whole.new === 'blue' &&
      whole.lead === '' &&
      whole.tail === '',
    whole,
  );
  const adrift = expandToWord({ old: 'gone', new: '' }, null);
  check(
    'an adrift entry — no anchor, no context — passes through in stored form',
    adrift.old === 'gone' &&
      adrift.new === '' &&
      adrift.lead === '' &&
      adrift.tail === '',
    adrift,
  );
  const stored = expandToWord({
    old: 'c',
    new: 'sh',
    prefix: 'it ',
    suffix: 'ould be fine',
  });
  check(
    "with no context argument the entry's OWN stored context is the context",
    stored.old === 'could' && stored.new === 'should',
    stored,
  );

  // THE UTF-16 LESSON, one layer out: the walk steps by CODE POINT. A
  // unit-wise walk stops between an emoji's halves and hands the display a
  // lone surrogate — the same defect diffBounds and trimAffixes are guarded
  // against, in the one place that had no guard yet.
  const pair = expandToWord(
    { old: '', new: 's' },
    { prefix: 'a re\u{1F600}d', suffix: ' word' },
  );
  const lone = (s) =>
    /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?:[^\uD800-\uDBFF]|^)[\uDC00-\uDFFF]/.test(
      s,
    );
  check(
    'the boundary walk never splits a surrogate pair — the emoji travels whole',
    pair.old === 're\u{1F600}d' &&
      pair.new === 're\u{1F600}ds' &&
      !lone(pair.old) &&
      !lone(pair.new),
    pair,
  );
}

{
  // ONE TYPED PHRASE IS ONE ROW, AND IT WAS TWO — measured in a real browser
  // as `△ 2 changed` reading
  //
  //     simpler → the simpler        just now
  //     simpler → simpler shape      just now
  //
  // for the single gesture "select `simpler`, type `the simpler shape`".
  // Neither row is what was typed, and `simpler` is displayed twice because
  // expandToWord widened two fragments to the same word from opposite sides.
  //
  // THE CAUSE IS THE TRIMMING, NOT THE DEBOUNCE. At `the simpler` the merged
  // region is `simpler` → `the simpler`, whose minimal diff is `{old: '',
  // new: 'the '}` — `simpler` being a shared SUFFIX — so the entry's stored end
  // moved seven characters back from the caret, and the space that came next
  // touched nothing and started a second entry. No server-side mutation is
  // involved; this reproduces with nothing but keystrokes.
  //
  // This is a KEYSTROKE-BY-KEYSTROKE drive of applyRecord, which is what the
  // plugin does for a real keyboard, and it reads the two REVIEWER-VISIBLE
  // facts: how many rows the log shows, and what one of them says.
  const START = 'Per host is ';
  const REST = ' and is what the prototype does.';
  const para = (t) =>
    docOf(schema.node('paragraph', null, t ? [schema.text(t)] : []));
  const gesture = (word, typed) => {
    let text = START + word + REST;
    let entries = [];
    // ProseMirror's own shape for typing over a selection: the first character
    // REPLACES the selection, every one after it is a caret insertion at the
    // end of what has been typed so far.
    const at = START.length + 1;
    const steps = [{ from: at, to: at + word.length, ins: typed[0] }];
    for (let i = 1; i < typed.length; i += 1) {
      steps.push({ from: at + i, to: at + i, ins: typed[i] });
    }
    for (const s of steps) {
      const before = para(text);
      const rec = {
        from: s.from,
        to: s.to,
        old: text.slice(s.from - 1, s.to - 1),
        ins: s.ins,
      };
      const { keep, merged } = applyRecord(entries, before, rec);
      const delta = s.ins.length - (s.to - s.from);
      text = text.slice(0, s.from - 1) + s.ins + text.slice(s.to - 1);
      const after = para(text);
      entries = keep.map((e) =>
        e.anchored && e.from >= s.to
          ? { ...e, from: e.from + delta, to: e.to + delta }
          : e,
      );
      if (merged) {
        entries.push({
          ...merged,
          ...contextOf(after, merged.from, merged.to),
        });
      }
    }
    return { entries, text };
  };

  const one = gesture('simpler', 'the simpler shape');
  check(
    'one typed phrase is ONE entry, however the trimming lands inside it',
    one.entries.length === 1,
    one.entries.map((e) => expandToWord(e)),
  );
  const shown = one.entries.length === 1 ? expandToWord(one.entries[0]) : null;
  check(
    'and the row says what was actually typed',
    shown !== null &&
      shown.old === 'simpler' &&
      shown.new === 'the simpler shape',
    shown,
  );
  check(
    'the document ended up where the keystrokes put it — the drive is real',
    one.text === `${START}the simpler shape${REST}`,
    one.text,
  );

  // THE DISCRIMINATING FIXTURE IS THE ONE WHOSE TYPED TEXT CONTAINS THE OLD
  // WORD. A replacement that shares no affix with what it replaces never
  // trims, so its stored range stays under the caret and it coalesced
  // correctly through the whole defect — a check built from one would pass with
  // the reach deleted.
  const plain = gesture('simpler', 'bigger');
  check(
    'a replacement sharing no affix was always one entry — which is why ' +
      'the fixture above has to share one',
    plain.entries.length === 1 &&
      expandToWord(plain.entries[0]).new === 'bigger',
    plain.entries.map((e) => expandToWord(e)),
  );

  // The other direction: a shared PREFIX moves the entry's start, and the
  // caret is at the end, so this one coalesced too. Kept because it is the
  // half a reader would assume was equally broken, and asserting it says which
  // half the reach is actually for.
  const pre = gesture('simpler', 'simpler still');
  check(
    'a shared prefix is one entry as well',
    pre.entries.length === 1 &&
      expandToWord(pre.entries[0]).new === 'simpler still',
    pre.entries.map((e) => expandToWord(e)),
  );

  // AND STORAGE IS STILL MINIMAL, which is the property the reach must not
  // cost: an entry stored wider than the undo's own diff can never pair with
  // the undo that reverts it (see trimAffixes) and survives as a phantom.
  const mid = gesture('simpler', 'the simpler');
  check(
    'the reach does not widen what is STORED — the entry is still the minimal diff',
    mid.entries.length === 1 &&
      mid.entries[0].old === '' &&
      mid.entries[0].new === 'the ',
    mid.entries[0],
  );
  // An entry with no reach must still work: it falls back to its own stored
  // range, which is what every entry did before the reach existed.
  check(
    'an entry with no reach merges on its stored range, as it always did',
    (() => {
      const placed = {
        ...mid.entries[0],
        from: 13,
        to: 17,
        anchored: true,
        reachFrom: null,
        reachTo: null,
      };
      return (
        applyRecord([placed], para(`${START}the simpler${REST}`), {
          from: 15,
          to: 15,
          old: '',
          ins: 'X',
        }).merged !== null
      );
    })(),
  );
}

{
  // The DECORATIONS are a display surface too, so they expand with everything
  // else: the ghost carries the expanded removed word and MOVES to the
  // expanded start (a ghost left at the minimal start renders "t" + "teh" +
  // "he"), and the highlight covers the whole inserted word.
  const decosOf = (doc, e) => {
    const all = trailDecorations(doc, [e]).find();
    return {
      ins: all.find(
        (d) => d.type.attrs && d.type.attrs.class === 'gly-trail-ins',
      ),
      ghost: all.find(
        (d) => d.spec && String(d.spec.key || '').startsWith('gly-trail-'),
      ),
    };
  };
  // "could" overtyped to "should": stored {old:'c', new:'sh'} at [11,13).
  const should = docOf(para('The quick should fox'));
  const wide = decosOf(should, {
    id: 1,
    anchored: true,
    from: 11,
    to: 13,
    old: 'c',
    new: 'sh',
    prefix: '',
    suffix: '',
    at: 'T1',
    blockKey: '',
  });
  check(
    'the highlight covers the expanded inserted word, not the two typed letters',
    !!wide.ins && should.textBetween(wide.ins.from, wide.ins.to) === 'should',
    wide.ins && [wide.ins.from, wide.ins.to],
  );
  // "teh" fixed to "the": stored {old:'eh', new:'he'} at [12,14), so the
  // affix is on the LEFT and the ghost has to MOVE — left at the minimal
  // start it renders "t" + ghost("teh") + "he".
  const the = docOf(para('The quick the fox'));
  const left = decosOf(the, {
    id: 2,
    anchored: true,
    from: 12,
    to: 14,
    old: 'eh',
    new: 'he',
    prefix: '',
    suffix: '',
    at: 'T2',
    blockKey: '',
  });
  check(
    "and the ghost sits at the expanded word's own start, not the diff's",
    !!left.ghost &&
      left.ghost.from === 11 &&
      !!left.ins &&
      the.textBetween(left.ins.from, left.ins.to) === 'the',
    {
      ghost: left.ghost && left.ghost.from,
      ins: left.ins && [left.ins.from, left.ins.to],
    },
  );
}

{
  // retractReverted: an undone edit leaves no trail — and ONLY what the undo
  // provably reverted retracts. The pairing is a reconstruction over the
  // undo's span, because one Cmd-Z can revert a BATCH with unrelated trailed
  // text standing between the reverted edits.
  const e = { id: 1, anchored: true, from: 3, to: 5, old: '', new: 'XY' };
  const hit = retractReverted([e], {
    from: 3,
    to: 5,
    removed: 'XY',
    inserted: '',
  });
  check(
    'an exact undo pair retracts its entry',
    hit.retracted === true && hit.entries.length === 0,
    hit,
  );
  const miss = retractReverted([e], {
    from: 3,
    to: 4,
    removed: 'X',
    inserted: '',
  });
  check(
    'a partial undo retracts nothing — ambiguity refuses',
    miss.retracted === false && miss.entries.length === 1,
    miss,
  );
  const ghost = {
    id: 2,
    anchored: true,
    from: 1,
    to: 1,
    old: 'hello',
    new: '',
  };
  const back = retractReverted([ghost], {
    from: 1,
    to: 1,
    removed: '',
    inserted: 'hello',
  });
  check(
    'undoing a deletion retracts its ghost',
    back.retracted === true && back.entries.length === 0,
    back,
  );
  // The canonicalized overtype, end to end: entry {rown→lue}, undo restores.
  const over = {
    id: 3,
    anchored: true,
    from: 2,
    to: 5,
    old: 'rown',
    new: 'lue',
  };
  const restore = retractReverted([over], {
    from: 2,
    to: 5,
    removed: 'lue',
    inserted: 'rown',
  });
  check(
    "undoing an overtype retracts it — the reviewer's own case",
    restore.retracted === true && restore.entries.length === 0,
    restore,
  );

  // A BATCH: one undo reverts two edits with a BYSTANDER entry between them —
  // an earlier, un-reverted edit whose text sits inside the undo's span. The
  // reconstruction retracts exactly the two and leaves the bystander, where a
  // containment sweep would have taken all three.
  //
  // Span [10, 30): "..AA..[XY]..BB.." reverts to "....[XY]....".
  const batch = [
    { id: 4, anchored: true, from: 12, to: 14, old: '', new: 'AA' },
    { id: 5, anchored: true, from: 18, to: 20, old: '', new: 'XY' },
    { id: 6, anchored: true, from: 24, to: 26, old: '', new: 'BB' },
  ];
  // Spans are literal: positions 12-14 hold AA, 18-20 XY, 24-26 BB, filler
  // elsewhere — so `removed` is the span's before-text and `inserted` the
  // after-text with only AA and BB gone.
  const undo = {
    from: 10,
    to: 30,
    removed: '..' + 'AA' + '....' + 'XY' + '....' + 'BB' + '....',
    inserted: '..' + '....' + 'XY' + '....' + '....',
  };
  const swept = retractReverted(batch, undo);
  check(
    'a batched undo retracts its own two edits and spares the bystander',
    swept.retracted === true &&
      swept.entries.length === 1 &&
      swept.entries[0].id === 5,
    swept.entries.map((e) => e.id),
  );
}

{
  // reanchor: best-effort and honest. Unique context anchors; ambiguity
  // refuses; the block key narrows an ambiguity the document alone cannot.
  const doc = docOf(para('hello XYworld'), para('other prose entirely'));
  const found = reanchor(
    doc,
    { new: 'XY', prefix: 'llo ', suffix: 'wor', blockKey: '' },
    [],
  );
  check(
    'a unique context re-anchors the entry',
    found !== null && doc.textBetween(found.from, found.to) === 'XY',
    found,
  );

  const twice = docOf(para('ab XYcd and ab XYcd'));
  check(
    'an ambiguous context refuses — log only, never guessed',
    reanchor(
      twice,
      { new: 'XY', prefix: 'b ', suffix: 'c', blockKey: '' },
      [],
    ) === null,
  );

  const blocks = docOf(para('ab XYcd here'), para('ab XYcd there'));
  const narrowed = reanchor(
    blocks,
    { new: 'XY', prefix: 'b ', suffix: 'c', blockKey: 'bk-2' },
    [{ key: 'bk-2', index: 1 }],
  );
  check(
    'the block key narrows what the document alone cannot',
    narrowed !== null && narrowed.from > blocks.child(0).nodeSize,
    narrowed,
  );

  // NO CONTEXT AT ALL IS NOT "NOTHING KNOWN" — it is a deletion that emptied
  // its own block, and this rung is Court's second finding. Every rung above
  // searches for TEXT, and such an entry has none: `new` is empty by
  // definition and contextOf clamps to the block the deletion just emptied,
  // so the needle was '' and the entry went adrift the first time its live
  // positions were invalidated — which every server-side mutation does, since
  // every one of them replaces the whole document. What it still knows is
  // STRUCTURAL: it left an empty textblock, BETWEEN TWO PARTICULAR BLOCKS.
  //
  // BOTH HALVES ARE LOAD-BEARING AND THE SECOND ONE WAS MISSING. A rung that
  // asked only "is there exactly one empty block" answers a question about the
  // DOCUMENT, not about the entry, and the two come apart the moment the
  // entry's own block stops existing — a join, an undo, a refill. The entry
  // brings its neighbours as evidence and a candidate has to match both.
  check(
    'no context at all still refuses where there is no empty block to claim',
    reanchor(doc, { new: '', prefix: '', suffix: '', blockKey: '' }, []) ===
      null,
  );

  const emptied = docOf(
    para('hello XYworld'),
    para(''),
    para('other prose entirely'),
  );
  const emptiedEntry = {
    new: '',
    prefix: '',
    suffix: '',
    blockKey: '',
    before: 'hello XYworld',
    after: 'other prose entirely',
  };
  const placed = reanchor(emptied, emptiedEntry, []);
  check(
    'a deletion that emptied its block re-anchors to that block',
    placed !== null &&
      placed.from === placed.to &&
      emptied.resolve(placed.from).parent.content.size === 0,
    placed,
  );
  check(
    'and emptyBlockAnchor is where that lives, so the plugin has one rule to share',
    JSON.stringify(emptyBlockAnchor(emptied, emptiedEntry)) ===
      JSON.stringify(placed),
    placed,
  );

  // THE GUESS THE RUNG USED TO MAKE. One empty block in the document, and an
  // entry that has nothing to do with it — the shape a Backspace-to-close-up
  // leaves behind, where the entry's own block was joined away and some other
  // block is empty screens further down.
  check(
    'an entry whose neighbours do not match the one empty block REFUSES — a ghost is never handed to a stranger',
    emptyBlockAnchor(emptied, {
      before: 'a paragraph elsewhere',
      after: 'and the one after it',
    }) === null,
    emptyBlockAnchor(emptied, {
      before: 'a paragraph elsewhere',
      after: 'and the one after it',
    }),
  );
  check(
    'and it refuses on ONE side mismatching too — both neighbours or nothing',
    emptyBlockAnchor(emptied, {
      before: 'hello XYworld',
      after: 'and the one after it',
    }) === null,
  );

  // AND THE OTHER DIRECTION: two empty blocks used to refuse for ambiguity,
  // which cost every one of them its ghost. With evidence they are two
  // IDENTIFIED blocks and each entry keeps its own.
  const twoOwned = docOf(
    para('alpha'),
    para(''),
    para('bravo'),
    para(''),
    para('charlie'),
  );
  const firstOwned = emptyBlockAnchor(twoOwned, {
    before: 'alpha',
    after: 'bravo',
  });
  const secondOwned = emptyBlockAnchor(twoOwned, {
    before: 'bravo',
    after: 'charlie',
  });
  check(
    'two empty blocks with different neighbours are two identified blocks, and each entry takes its own',
    firstOwned !== null &&
      secondOwned !== null &&
      firstOwned.from !== secondOwned.from &&
      twoOwned.resolve(firstOwned.from).parent.content.size === 0 &&
      twoOwned.resolve(secondOwned.from).parent.content.size === 0,
    [firstOwned, secondOwned],
  );
  const twins = docOf(
    para('same'),
    para(''),
    para('same'),
    para(''),
    para('same'),
  );
  check(
    'but two empty blocks between the SAME neighbours is ambiguity, and ambiguity still refuses',
    emptyBlockAnchor(twins, { before: 'same', after: 'same' }) === null,
  );

  // AN ENTRY WITH NO EVIDENCE — a record written before these fields existed,
  // read back from the sidecar. It has nothing to be identified by and is
  // refused BY THE SAME COMPARISON rather than by a special case: `side`
  // normalises its absent fields to null/null, and null/null can only match a
  // block with no block on either side of it — the document's only block.
  check(
    'an entry carrying no evidence refuses even where exactly one block is empty',
    emptyBlockAnchor(docOf(para('a'), para('')), {}) === null,
  );
  check(
    "and where the document's ONLY block is the empty one it places — there uniqueness really is identity",
    JSON.stringify(emptyBlockAnchor(docOf(para('')), {})) ===
      JSON.stringify({ from: 1, to: 1 }),
    emptyBlockAnchor(docOf(para('')), {}),
  );
  // THE THIRD ROW OF THAT INVARIANT, AND IT USED TO ANCHOR. The sentence above
  // it claims an evidence-free record matches ONLY the document's only block;
  // measured against the shipped build it also matched the LAST of two trailing
  // empty blocks, because "no block that side" and "an empty block that side"
  // were the same '' and the last block's ''/'' therefore read as the only
  // block's. All three rows are pinned so the invariant is asserted by a check
  // rather than by prose.
  check(
    'and it refuses on the last of two trailing empty blocks — no neighbours is not an EMPTY neighbour',
    emptyBlockAnchor(docOf(para('text'), para(''), para('')), {}) === null,
    emptyBlockAnchor(docOf(para('text'), para(''), para('')), {}),
  );

  // ABSENT IS NOT EMPTY, AND CONFLATING THEM HANDED A GHOST TO A STRANGER.
  // `neighboursAt` answered '' both for "there is no block that side" and for
  // "the block that side is empty", so an entry that emptied a block whose
  // neighbour was ITSELF empty matched a block at the document's edge — on
  // BOTH sides, so the two-sided rule passed and refused nothing. It takes two
  // block-emptying deletions on adjacent blocks and one gap close, which is
  // ordinary cleanup: strike the last paragraph whole, strike the one above it
  // whole, then Backspace once more to close the blank.
  {
    const gone = docOf(para('above'), para(''), para(''));
    // The upper entry, recorded when the block below it was already empty.
    const upper = { before: 'above', after: '' };
    check(
      'an entry whose neighbour was an EMPTY block claims the block it names',
      JSON.stringify(emptyBlockAnchor(gone, upper)) ===
        JSON.stringify({ from: 8, to: 8 }),
      emptyBlockAnchor(gone, upper),
    );
    // The gap closes: the upper empty block joins away, and the block left
    // standing is the LOWER one — a block that entry never touched. Its `after`
    // is the document's end, not an empty block.
    const closed = docOf(para('above'), para(''));
    check(
      "and refuses that same block once the gap closed — the survivor is at the document's end, not beside an empty block",
      emptyBlockAnchor(closed, upper) === null,
      emptyBlockAnchor(closed, upper),
    );
  }

  // THE SAME THING END TO END, THROUGH THE PASSES THE EDITOR ACTUALLY RUNS —
  // the gesture rather than the arithmetic, with every position mapped through
  // its own transaction and every context recorded by contextOf at the moment
  // of the edit, exactly as the plugin records it. Strike the last paragraph
  // whole; strike the paragraph above it whole; Backspace once more to close
  // the blank. The lower entry keeps its place by verifying (its block is
  // still empty); the upper entry's own block was joined away and must NOT be
  // re-anchored onto its neighbour's. Measured on the shipped build: both
  // ghosts landed on the one block, so a sentence deleted elsewhere rendered
  // struck through beside the sentence that block really did lose.
  {
    const ABOVE =
      'A settled sentence above, which the closed-up gap joins into.';
    const UPPER = 'The quick brown fox pauses at the edge of the paragraph.';
    const LOWER =
      'The last line is struck whole and leaves its own block empty.';
    const startOf = (d, i) => {
      let at = 0;
      for (let k = 0; k < i; k += 1) {
        at += d.child(k).nodeSize;
      }
      return at + 1;
    };
    let state = EditorState.create({
      schema,
      doc: docOf(para(ABOVE), para(UPPER), para(LOWER)),
    });
    const strike = (i) => {
      const at = startOf(state.doc, i);
      const tr = state.tr.delete(at, at + state.doc.child(i).content.size);
      state = state.apply(tr);
      return tr.mapping;
    };
    let live = [];
    const carry = (mapping) => {
      live = live.map((e) => ({
        ...e,
        from: mapping.map(e.from),
        to: mapping.map(e.to),
      }));
    };
    const record = (id, old, i) => {
      const at = startOf(state.doc, i);
      live.push({
        id,
        anchored: true,
        from: at,
        to: at,
        old,
        new: '',
        blockKey: '',
        at: `T${id}`,
        ...contextOf(state.doc, at, at),
      });
    };
    carry(strike(2));
    record(1, LOWER, 2);
    check(
      'the entry that empties the LAST block records no block after it',
      live[0].before !== null && live[0].after === null,
      { before: live[0].before, after: live[0].after },
    );
    carry(strike(1));
    record(2, UPPER, 1);
    check(
      'and the one above it records an EMPTY block after it — a different fact, and stored as one',
      live[1].after === '',
      { before: live[1].before, after: live[1].after },
    );
    live = settleEntries(state.doc, live, [], false);
    // Backspace at the head of the emptied block: the blank closes up into the
    // block above. A cross-block step, so nothing new is recorded.
    const join = state.tr.join(startOf(state.doc, 1) - 1);
    carry(join.mapping);
    state = state.apply(join);
    live = settleEntries(state.doc, live, [], true);
    const placed = live.filter((e) => e.anchored);
    check(
      'after the gap closes exactly ONE ghost stands on the surviving empty block — the other refuses',
      placed.length === 1 && placed[0].old === LOWER,
      live.map((e) => ({
        old: e.old.slice(0, 12),
        anchored: e.anchored,
        from: e.from,
      })),
    );
  }

  // AND THE EVIDENCE ITSELF SAYS WHICH IT MEANS. contextOf records `null` for a
  // side with no block at all and '' for a side whose block is empty; a build
  // that writes '' for both is the conflation above, upstream of the compare.
  {
    const edge = docOf(para('above'), para(''));
    check(
      'contextOf reports no block that side as null, not as an empty one',
      JSON.stringify(contextOf(edge, 8, 8)) ===
        JSON.stringify({
          prefix: '',
          suffix: '',
          before: 'above',
          after: null,
        }),
      contextOf(edge, 8, 8),
    );
    const between = docOf(para('above'), para(''), para(''));
    check(
      "and an EMPTY block that side as '' — the two are different facts",
      JSON.stringify(contextOf(between, 8, 8)) ===
        JSON.stringify({ prefix: '', suffix: '', before: 'above', after: '' }),
      contextOf(between, 8, 8),
    );
  }

  check(
    'and no empty block at all refuses too — there is nothing to be found by',
    emptyBlockAnchor(doc, emptiedEntry) === null,
  );

  // WHAT THE REVIEWER'S HAND CANNOT HAVE EMPTIED IS NOT A CANDIDATE. An empty
  // fence is a shape a plain .md produces AT REST (``` on two adjacent lines),
  // and counting it was enough on its own to make a real deletion's ghost
  // refuse for ambiguity — Court's original bug, reproduced by an ordinary
  // document. The same goes for a table's cells and for a note, both of them
  // read-only to the keyboard by construction.
  const emptyCell = schema.node('table', null, [
    schema.node('tableRow', null, [schema.node('tableCell', null, [para('')])]),
  ]);
  //
  // EACH EXCLUSION IS HELD BY TWO CHECKS AND THEY PULL IN OPPOSITE DIRECTIONS,
  // because the exclusion is wrong in two directions. The AMBIGUITY row is the
  // false-refusal side — Court's original bug, where counting a literal block
  // gave a real deletion a second candidate with the same neighbours and the
  // ghost refused. The ALONE row is the wrong-anchor side: with the literal
  // counted, an entry that emptied the document's last block would be handed
  // the literal instead.
  //
  // THE ALONE ROW ASKS FOR `after: null` AND THAT ONE TOKEN IS THE WHOLE CHECK.
  // It reads `{ before: 'alpha', after: null }` because the literal it must not
  // be given IS the document's last block, and a last block reports no block
  // after it — null. Written `after: ''` (which is what a last block reported
  // back when absent and empty were the same value) the comparison refuses on
  // the SIDE EVIDENCE before the exclusion is ever consulted, and the check
  // reports ok with reviewerBlocks deleted whole. Verified by deleting it:
  // these six go red, and nothing else in probe.mjs does.
  for (const [what, node] of [
    ['fence', fence('')],
    ['table cell', emptyCell],
  ]) {
    const withLiteral = docOf(
      para('hello XYworld'),
      para(''),
      para('other prose entirely'),
      node,
    );
    check(
      `an empty ${what} is not a candidate — the real deletion still finds its own block`,
      JSON.stringify(emptyBlockAnchor(withLiteral, emptiedEntry)) ===
        JSON.stringify(placed),
      emptyBlockAnchor(withLiteral, emptiedEntry),
    );
    const shared = docOf(para('a'), para(''), para('a'), node, para('a'));
    check(
      `and an empty ${what} between the SAME neighbours does not make the real one ambiguous`,
      JSON.stringify(emptyBlockAnchor(shared, { before: 'a', after: 'a' })) ===
        JSON.stringify({
          from: shared.child(0).nodeSize + 1,
          to: shared.child(0).nodeSize + 1,
        }),
      emptyBlockAnchor(shared, { before: 'a', after: 'a' }),
    );
    check(
      `and an empty ${what} alone offers nothing at all — it is no hand's work to be found`,
      emptyBlockAnchor(docOf(para('alpha'), node), {
        before: 'alpha',
        after: null,
      }) === null,
      emptyBlockAnchor(docOf(para('alpha'), node), {
        before: 'alpha',
        after: null,
      }),
    );
  }

  // The note goes through fragmentSchema, which is the one that carries
  // NoteBlock — the schema above is the suggestion plugin's and has no note in
  // it. A note IS a textblock (`content: 'text*'`), so it is the one exclusion
  // that has to be taken before the textblock test rather than by pruning a
  // container, and an implementation that forgot that would count it.
  {
    const noteP = (t) =>
      fragmentSchema.node('paragraph', null, t ? [fragmentSchema.text(t)] : []);
    const withNote = fragmentSchema.node('doc', null, [
      noteP('hello XYworld'),
      noteP(''),
      noteP('other prose entirely'),
      fragmentSchema.node('note', null, []),
    ]);
    check(
      "an empty note is not a candidate — its words are the composer's, never a keystroke's",
      JSON.stringify(emptyBlockAnchor(withNote, emptiedEntry)) ===
        JSON.stringify(placed),
      emptyBlockAnchor(withNote, emptiedEntry),
    );
    const sharedNote = fragmentSchema.node('doc', null, [
      noteP('a'),
      noteP(''),
      noteP('a'),
      fragmentSchema.node('note', null, []),
      noteP('a'),
    ]);
    check(
      'and an empty note between the SAME neighbours does not make the real one ambiguous',
      JSON.stringify(
        emptyBlockAnchor(sharedNote, { before: 'a', after: 'a' }),
      ) ===
        JSON.stringify({
          from: sharedNote.child(0).nodeSize + 1,
          to: sharedNote.child(0).nodeSize + 1,
        }),
      emptyBlockAnchor(sharedNote, { before: 'a', after: 'a' }),
    );
    const noteAlone = fragmentSchema.node('doc', null, [
      noteP('alpha'),
      fragmentSchema.node('note', null, []),
    ]);
    check(
      "and an empty note alone offers nothing at all — it is no hand's work to be found",
      emptyBlockAnchor(noteAlone, { before: 'alpha', after: null }) === null,
      emptyBlockAnchor(noteAlone, { before: 'alpha', after: null }),
    );
  }

  // A CONTEXTLESS GHOST CANNOT BE VERIFIED BY CONTEXT, which is the same rule
  // seen from the other side: '' === '' is true at every position of every
  // textblock, so the old check kept such an entry anchored wherever a rebuild
  // happened to leave it. It is verified against its BLOCK instead.
  const ghost = (from) => ({
    id: 1,
    anchored: true,
    from,
    to: from,
    old: 'gone',
    new: '',
    prefix: '',
    suffix: '',
    at: 'T1',
    blockKey: '',
  });
  check(
    'a contextless ghost verifies in an empty block and nowhere else',
    verifyEntry(emptied, ghost(emptied.child(0).nodeSize + 1)) === true &&
      verifyEntry(emptied, ghost(3)) === false,
  );
}

// TWO DELETIONS ARE TWO GHOSTS — the block-emptying entries settle AS A SET.
//
// COURT'S THIRD FINDING, AND THE FOURTH SHAPE OF ONE BUG. §6 gave a
// block-emptying deletion a rung of its own; §8 made that rung ask for
// evidence; §9 made the evidence three-valued. Each of those asks about ONE
// entry, and n of them against m empty blocks is not n copies of that question
// — it is an ASSIGNMENT, and document order is a fact about the set that no
// per-entry rung can see. Three configurations were measured on the build
// before this section, driving the passes below:
//
//   two paragraphs struck between three IDENTICAL lines — both entries carry
//     the same evidence, both refused for ambiguity, and both ghosts were lost
//     on the first server-side mutation. `0/2 anchored`.
//   FOUR adjacent paragraphs struck — the two INTERIOR entries both record an
//     empty block above and an empty block below, so both refused while the
//     outer two anchored. `2/4 anchored`.
//   two struck between identical lines, then the LOWER gap closed — the lower
//     entry's block was joined away, its stale evidence still matched the
//     upper entry's block, and its ghost was drawn THERE. Two ghosts on one
//     block: the reviewer reads two deletions on a line that lost one, which
//     is the wrong-anchor failure §8 exists to forbid, reached from a
//     direction §8 could not see.
//
// AND TWO ADJACENT PARAGRAPHS ALREADY WORKED, which is why the fixtures below
// carry the repeated line. §9's three-valued evidence discriminates a pair —
// the upper records "an empty block below", the lower "an empty block above" —
// so a fixture of two adjacent strikes in ordinary prose passes with the set
// rule REMOVED, and a section built only from one would certify nothing. It is
// pinned anyway, because it must keep working.
{
  const TWIN = 'A line that repeats, word for word, above and below.';
  const startOf = (d, i) => {
    let at = 0;
    for (let k = 0; k < i; k += 1) {
      at += d.child(k).nodeSize;
    }
    return at + 1;
  };
  // The gesture, through the passes the editor actually runs: strike a
  // paragraph whole, record the entry with contextOf against the document the
  // step produced, and settle — exactly the plugin's own order. `rebuild` is
  // the server-side mutation: y-prosemirror replaces the whole document, so
  // every live position maps to the same edge of one ReplaceStep and the
  // entries re-anchor from context alone.
  const hand = (texts) => {
    let state = EditorState.create({
      schema,
      doc: docOf(...texts.map((t) => para(t))),
    });
    let live = [];
    const carry = (mapping) => {
      live = live.map((e) => ({
        ...e,
        from: mapping.map(e.from),
        to: mapping.map(e.to),
      }));
    };
    return {
      strike(i) {
        const at = startOf(state.doc, i);
        const tr = state.tr.delete(at, at + state.doc.child(i).content.size);
        state = state.apply(tr);
        carry(tr.mapping);
        const now = startOf(state.doc, i);
        live.push({
          id: live.length + 1,
          anchored: true,
          from: now,
          to: now,
          old: texts[i],
          new: '',
          blockKey: '',
          at: `T${live.length}`,
          ...contextOf(state.doc, now, now),
        });
        live = settleEntries(state.doc, live, [], false);
        return this;
      },
      close(i) {
        const tr = state.tr.join(startOf(state.doc, i) - 1);
        state = state.apply(tr);
        carry(tr.mapping);
        live = settleEntries(state.doc, live, [], true);
        return this;
      },
      rebuild() {
        const same = state.doc.copy(state.doc.content);
        const tr = state.tr.step(
          new ReplaceStep(
            0,
            state.doc.content.size,
            new Slice(same.content, 0, 0),
          ),
        );
        state = state.apply(tr);
        carry(tr.mapping);
        live = settleEntries(state.doc, live, [], true);
        return this;
      },
      // The observable: which ghost stands where, and how many blocks they
      // stand on. TWO entries on ONE block is a distinct failure from two
      // entries on no block, and a count of anchored entries cannot tell them
      // apart — which is exactly how the wrong anchor above stayed invisible.
      ghosts() {
        const placed = live.filter((e) => e.anchored);
        return {
          on: placed.map((e) => `${e.old}@${e.from}`),
          count: placed.length,
          blocks: new Set(placed.map((e) => e.from)).size,
          adrift: live.filter((e) => !e.anchored).map((e) => e.old),
        };
      },
    };
  };

  // THE CONFIGURATION THAT COST BOTH GHOSTS. Identical neighbours on both
  // sides of both blocks, which is an ordinary list or a repeated stanza and
  // not a contrivance.
  const twins = hand([
    TWIN,
    'the first one struck',
    TWIN,
    'the second one struck',
    TWIN,
  ])
    .strike(1)
    .strike(3);
  check(
    'two struck between identical lines are individually unidentifiable — the rung refuses each alone',
    emptyBlockAnchor(
      docOf(para(TWIN), para(''), para(TWIN), para(''), para(TWIN)),
      {
        before: TWIN.slice(-TRAIL_CONTEXT_CHARS),
        after: TWIN.slice(0, TRAIL_CONTEXT_CHARS),
      },
    ) === null,
  );
  const twinsAfter = twins.rebuild().ghosts();
  check(
    'but as a SET they are two ghosts on two blocks, and the server rebuilding the document does not cost them',
    twinsAfter.count === 2 && twinsAfter.blocks === 2,
    twinsAfter,
  );

  // FOUR ADJACENT. The interior two record an empty block on BOTH sides — the
  // one shape §9's three-valued evidence still cannot tell apart — so the pair
  // in the middle is exactly what order settles.
  const four = hand(['a heading line', 'w', 'x', 'y', 'z', 'a closing line'])
    .strike(1)
    .strike(2)
    .strike(3)
    .strike(4)
    .rebuild()
    .ghosts();
  check(
    'four adjacent paragraphs struck are four ghosts on four blocks — the interior two used to refuse',
    four.count === 4 && four.blocks === 4,
    four,
  );

  // THE PAIR THAT ALREADY WORKED, PINNED. It passes with the set rule removed
  // and is here so it cannot regress under it.
  const pair = hand(['a heading line', 'w', 'x', 'a closing line'])
    .strike(1)
    .strike(2)
    .rebuild()
    .ghosts();
  check(
    'two ADJACENT in ordinary prose keep working — the evidence discriminates a pair on its own',
    pair.count === 2 && pair.blocks === 2,
    pair,
  );
  const apart = hand(['alpha', 'w', 'bravo', 'x', 'charlie'])
    .strike(1)
    .strike(3)
    .rebuild()
    .ghosts();
  check(
    'and two NON-adjacent with distinct neighbours keep working too',
    apart.count === 2 && apart.blocks === 2,
    apart,
  );
  const three = hand(['a heading line', 'w', 'x', 'y', 'a closing line'])
    .strike(1)
    .strike(2)
    .strike(3)
    .rebuild()
    .ghosts();
  check(
    'three adjacent are three ghosts — the run is resolved end to end, not just at its edges',
    three.count === 3 && three.blocks === 3,
    three,
  );

  // REFUSAL IS STILL REACHABLE AND STILL CORRECT, and these are the shapes it
  // is reachable in. A rule that anchored everything would have deleted the
  // safety, not fixed the bug.
  //
  // (1) A BLOCK ANOTHER ENTRY STILL STANDS IN IS NOT FREE. Strike two between
  // identical lines, then close the LOWER blank — an ordinary second
  // Backspace. The lower entry's own block is gone; its stale evidence still
  // matches the upper entry's block exactly, and the upper entry is standing
  // there. Measured before this section: the lower ghost was drawn on the
  // upper's block, both of them on one line.
  const taken = hand([
    TWIN,
    'the upper one struck',
    TWIN,
    'the lower one struck',
    TWIN,
  ])
    .strike(1)
    .strike(3)
    .close(3)
    .ghosts();
  check(
    'a block an entry still stands in is not offered to another — the closed-up entry refuses',
    taken.count === 1 &&
      taken.on[0].startsWith('the upper one struck') &&
      taken.adrift.length === 1 &&
      taken.adrift[0] === 'the lower one struck',
    taken,
  );

  // (2) MORE CANDIDATES THAN ENTRIES, ALL ALIKE. Two entries and three
  // identical slots: the order says the first is above the second and says
  // nothing about which slot either is in. Three assignments respect it, so
  // both refuse — the order narrows an ambiguity, it does not abolish one.
  const spare = docOf(
    para(TWIN),
    para(''),
    para(TWIN),
    para(''),
    para(TWIN),
    para(''),
    para(TWIN),
  );
  const evidence = {
    before: TWIN.slice(-TRAIL_CONTEXT_CHARS),
    after: TWIN.slice(0, TRAIL_CONTEXT_CHARS),
    old: 'gone',
    new: '',
    prefix: '',
    suffix: '',
    placed: true,
  };
  const roomy = settleEntries(
    spare,
    [
      {
        ...evidence,
        id: 1,
        old: 'the first',
        at: 'T1',
        anchored: false,
        from: null,
        to: null,
      },
      {
        ...evidence,
        id: 2,
        old: 'the second',
        at: 'T2',
        anchored: false,
        from: null,
        to: null,
      },
    ],
    [],
    true,
  );
  check(
    'two entries and THREE slots they all match still refuses — order narrows ambiguity, it does not abolish it',
    roomy.every((e) => !e.anchored),
    roomy.map((e) => [e.old, e.anchored]),
  );

  // (3) AN ENTRY NO SETTLE HAS PLACED HAS NO ORDER TO BE RESOLVED BY, and must
  // degrade to refusal rather than to a guess. `placed` is what says the
  // trail's order speaks for an entry; one no settle has written carries none
  // and is left out of the order — so the very configuration the set rule
  // fixes goes back to refusing. That is the honest degradation: the fact
  // cannot be invented for an entry nobody has placed.
  const legacy = docOf(para(TWIN), para(''), para(TWIN), para(''), para(TWIN));
  const older = settleEntries(
    legacy,
    [
      unplacedEntry({
        old: 'the first',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T1',
      }),
      unplacedEntry({
        old: 'the second',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T2',
      }),
    ],
    [],
    true,
  );
  check(
    'a trail with no `placed` on any entry refuses the ambiguity the order would have settled',
    older.every((e) => !e.anchored),
    older.map((e) => [e.old, e.anchored, e.placed]),
  );
  const current = settleEntries(
    legacy,
    [
      unplacedEntry({
        old: 'the first',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T1',
        placed: true,
      }),
      unplacedEntry({
        old: 'the second',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T2',
        placed: true,
      }),
    ],
    [],
    true,
  );
  check(
    'and the SAME two rows carrying `placed` resolve — which is what makes the row above a degradation and not a bug',
    current.every((e) => e.anchored) &&
      new Set(current.map((e) => e.from)).size === 2,
    current.map((e) => [e.old, e.from]),
  );

  // AND ONE ROW OF EACH IS NOT AN ORDER. `placed` is a fact about ONE entry, so
  // a trail is not ordered-or-not as a whole: an entry no settle has written
  // can sit beside one a settle wrote, and an entry that went adrift on the last settle sits
  // beside one that did not. An unordered entry neither reads the floor nor
  // moves it, so it constrains nobody and nobody constrains it — the two rows
  // below match the same two slots, BOTH assignments respect the single order
  // there is, and both refuse. That is the difference between a hard CONSTRAINT
  // and a tie-break, and neither fixture above can state it: `legacy` is
  // unordered on both rows and `current` ordered on both.
  const mixed = settleEntries(
    legacy,
    [
      unplacedEntry({
        old: 'the first',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T1',
        placed: true,
      }),
      unplacedEntry({
        old: 'the second',
        new: '',
        before: evidence.before,
        after: evidence.after,
        at: 'T2',
      }),
    ],
    [],
    true,
  );
  check(
    'one row carrying `placed` and one without is not an order between them — the pair refuses',
    mixed.every((e) => !e.anchored),
    mixed.map((e) => [e.old, e.anchored, e.placed]),
  );

  // AND THE FIELD IS WRITTEN FROM THE OUTCOME of every settle, never assumed.
  check(
    'every settled entry leaves carrying `placed`, read from what actually happened',
    current.every((e) => e.placed === true) &&
      older.every((e) => e.placed === false),
    [current.map((e) => e.placed), older.map((e) => e.placed)],
  );
}

// --- §9d: THE BOUND, AND THE WEAKER ANSWER PAST IT --------------------------
//
// The set search is exhaustive, so it is bounded twice — by how many entries it
// will search over (TRAIL_SET_CAP) and by how much searching it will do
// (TRAIL_SET_NODES) — and past either it hands back to the weak rule: an entry
// is placed only where it has ONE compatible candidate and no other entry could
// want that one. Every part of that was untested, and the bound itself was
// advertised at roughly double what it was: the docstring said sixteen entries
// while a budget of 20000 nodes stopped the search at NINE adjacent strikes and
// EIGHT strikes in a stanza of identical lines, so the seventeen-entry branch
// could not be reached and a reviewer striking a ten-line list got the weak
// answer while being told the bound was sixteen.
//
// SO THE BOUND WAS MEASURED, in a real chromium (141.0.7390.37, this machine),
// timing settleEntries over the document a server-side mutation leaves — the
// settle that actually has to re-anchor them all. Median of the run, and the
// node counts are the search's own counter:
//
//   entries   adjacent strikes        strikes in an identical stanza
//         8      7,260 nodes  0.2ms       24,821 nodes   0.5ms
//        10    100,616 nodes  1.6ms      354,763 nodes   8.1ms
//        11    380,264 nodes  6.1ms    1,356,173 nodes  33.2ms
//        12  1,446,508 nodes 24.9ms    5,208,491 nodes 132.5ms
//        16    315.6M nodes    6.1s        1.167G nodes  35.6s
//
// TEN is therefore the boundary and the cap is set there: the worst shape an
// ordinary hand produces at ten costs 8.1ms, eleven costs four times that, and
// sixteen — the number that used to be advertised — is thirty-five SECONDS on
// the settle path. The node budget is no longer the thing that bites for these
// shapes (600000 admits 354,763 with room), which is what makes the cap the
// stated bound and the budget what it always should have been: the backstop for
// a shape nobody measured, since cost depends on the CANDIDATES too and a
// document full of blank lines is not in the table above.
{
  const TWIN = 'A line that repeats, word for word, above and below.';
  // One unique slot (between `alpha` and `bravo`) and `slots` identical ones.
  // The unique slot is what makes a DEGRADATION visible: past the bound the
  // weak rule still places it, so "the search gave up" and "everything refused"
  // are two different answers here rather than one.
  const stanza = (slots) => {
    const nodes = [para('alpha'), para(''), para('bravo')];
    for (let i = 0; i < slots; i += 1) {
      nodes.push(para(TWIN), para(''));
    }
    nodes.push(para(TWIN));
    return docOf(...nodes);
  };
  const twin = {
    before: TWIN.slice(-TRAIL_CONTEXT_CHARS),
    after: TWIN.slice(0, TRAIL_CONTEXT_CHARS),
  };
  const only = { before: 'alpha', after: 'bravo' };
  const rows = (n, ev, from) => {
    const out = [];
    for (let i = 0; i < n; i += 1) {
      out.push({
        id: (from || 0) + i + 1,
        old: `struck ${(from || 0) + i + 1}`,
        new: '',
        prefix: '',
        suffix: '',
        blockKey: '',
        at: `T${(from || 0) + i + 1}`,
        anchored: false,
        from: null,
        to: null,
        placed: true,
        ...ev,
      });
    }
    return out;
  };
  const ghosts = (settled) => ({
    count: settled.filter((e) => e.anchored).length,
    blocks: new Set(settled.filter((e) => e.anchored).map((e) => e.from)).size,
    on: settled.filter((e) => e.anchored).map((e) => `${e.old}@${e.from}`),
  });

  // AT THE CAP, THE WORST ORDINARY SHAPE RESOLVES — which is the claim the two
  // numbers have to make together. TRAIL_SET_CAP entries in a stanza of
  // identical lines is the most ambiguous evidence a hand can produce (every
  // entry matches every slot) and the most expensive shape measured, so a
  // budget that cannot afford THIS one cannot afford anything at the cap, and
  // the advertised bound would be a number no reviewer ever reaches.
  const atCap = ghosts(
    settleEntries(stanza(TRAIL_SET_CAP), rows(TRAIL_SET_CAP, twin), [], true),
  );
  check(
    `${TRAIL_SET_CAP} entries — the cap — resolve in a stanza of identical lines: the budget affords the shape the cap advertises`,
    atCap.count === TRAIL_SET_CAP && atCap.blocks === TRAIL_SET_CAP,
    atCap,
  );

  // ONE PAST IT, AND THE WHOLE SET GETS THE WEAKER ANSWER — which is a
  // DEGRADATION and not a refusal: the entry whose evidence names one slot
  // nobody else wants is still placed, and the ambiguous ones lose their ghosts
  // to the log rather than landing somewhere wrong.
  const over = ghosts(
    settleEntries(
      stanza(TRAIL_SET_CAP),
      [...rows(1, only), ...rows(TRAIL_SET_CAP, twin, 1)],
      [],
      true,
    ),
  );
  check(
    'one entry past the cap and the set degrades to the weak rule — which still places what is unambiguous',
    over.count === 1 && over.blocks === 1 && over.on[0].startsWith('struck 1'),
    over,
  );

  // AND THE WEAK RULE'S SECOND HALF IS NOT OPTIONAL. "One compatible candidate"
  // is not "one candidate that is mine": two entries whose blocks were joined
  // away can share one surviving slot, and a fallback that reads only the first
  // half puts both ghosts on it — the two-ghosts-on-one-block wrong anchor the
  // set rule exists to stop, reintroduced by the path taken when the set rule
  // gives up. Measured with the second half deleted: 2 ghosts, 1 block.
  const contested = ghosts(
    settleEntries(
      stanza(TRAIL_SET_CAP),
      [...rows(2, only), ...rows(TRAIL_SET_CAP - 1, twin, 2)],
      [],
      true,
    ),
  );
  check(
    'past the cap, a single candidate ANOTHER entry could want is still refused — no two ghosts on one block',
    contested.count === 0,
    contested,
  );

  // THE BUDGET IS THE BACKSTOP FOR WHAT THE CAP CANNOT SEE. The cap counts
  // ENTRIES; the cost is entries against CANDIDATES, and a document with twice
  // as many blank lines as strikes is not a shape any measurement covered. At
  // the cap against twice the slots this search is 45,400,741 nodes and 1.24s
  // — measured with the budget lifted — against a cap that reports the set as
  // perfectly ordinary. So this one is asserted as ELAPSED TIME, because time
  // is the only thing the budget changes here: the answer is refusal either
  // way, and a check reading the answer alone would pass on a build with no
  // budget at all.
  const started = Date.now();
  const blown = ghosts(
    settleEntries(
      stanza(TRAIL_SET_CAP * 2),
      rows(TRAIL_SET_CAP, twin),
      [],
      true,
    ),
  );
  const took = Date.now() - started;
  check(
    'a shape the cap admits and the budget cannot afford degrades PROMPTLY rather than hanging',
    took < 250 && blown.count === 0,
    { took, ...blown },
  );

  // And the rule is one rule: the set of one that emptyBlockAnchor is goes
  // through the same bound, so a caller holding a single entry can never be
  // answered by a different function than the settle pass uses.
  check(
    'the set of one is the same function — a lone entry with a unique slot is placed by placeEmptyBlocks itself',
    JSON.stringify(placeEmptyBlocks(stanza(2), rows(1, only))) ===
      JSON.stringify([emptyBlockAnchor(stanza(2), rows(1, only)[0])]),
  );
}

{
  // The log is a PARTITION of the trail: an entry that re-anchors decorates
  // the prose AND lists in the log; one that cannot lists in the log ONLY —
  // and every entry lands in exactly one of the two, which is the property
  // "nothing is dropped" actually depends on (the same doctrine as the rail's
  // three thread regions, extended to the new region's own population).
  const doc = docOf(para('hello XYworld'));
  const loaded = [
    unplacedEntry({
      old: '',
      new: 'XY',
      prefix: 'llo ',
      suffix: 'wor',
      at: 'T1',
    }),
    unplacedEntry({
      old: 'gone',
      new: '',
      prefix: 'never seen',
      suffix: 'anywhere',
      at: 'T2',
    }),
  ];
  const settled = settleEntries(doc, loaded, [], true);
  // THE GHOST IS THE TRAIL'S ONLY SURFACE, so `anchored` decides whether an
  // entry is visible at all. It used to decide which of TWO surfaces an entry
  // reached — the prose and the log, or the log only — and `partitionEntries`
  // was the rule; the log is gone (the trail is an outgoing message, not a
  // history), so the claim is no longer a partition over two lists but a
  // presence over one.
  //
  // TWO decorations for the anchored one, not one: the entry inserted "XY"
  // into the word "world", so display expands it to world → XYworld and the
  // ghost carries the word as it was (expandToWord's rule, applied at the
  // decoration site like everywhere else). The adrift entry decorates nothing
  // at all, which is the property this check is actually about, and the one
  // the partition check was really resting on.
  check(
    'an entry that re-anchored gets its ghost; one that could not gets nothing',
    trailDecorations(doc, settled).find().length === 2 &&
      trailDecorations(doc, [settled.find((e) => !e.anchored)]).find()
        .length === 0,
  );
  // AND IT IS STILL IN THE LIST. That is what stops the deletion above from
  // being a drop: an entry with no place has no ghost and is still an entry —
  // read off `settleEntries` itself now that `outgoingCounts` is gone, which is
  // the same claim one function nearer the fact.
  check(
    'and an entry with no place is still in the list the trail keeps',
    settled.length === 2 && settled.some((e) => !e.anchored),
    { entries: settled.length },
  );
  const ghosted = settleEntries(
    doc,
    [
      {
        id: 9,
        anchored: true,
        from: 7,
        to: 7,
        old: 'gone',
        new: '',
        prefix: 'hello ',
        suffix: 'XYwor',
        at: 'T3',
        blockKey: '',
      },
    ],
    [],
    false,
  );
  check(
    'a deletion ghost is a widget decoration — NOT content',
    trailDecorations(doc, ghosted).find().length === 1,
    ghosted,
  );
}

// WHAT THE REVIEWER IS ABOUT TO TELL THE AGENT — AND THE NINE CHECKS THAT
// COUNTED IT ARE DELETED WITH THE FUNCTIONS THEY WERE ABOUT.
//
// `outgoingCounts` and `outgoingLabel` produced `· 6 edits, 2 replies` for
// `.gly-revise-trail`. That box was deleted when the rounds-only workflow left
// nothing to write in it, and both fields the pair counted left the wire with
// it: `pendingView` is `{instructions, blocks}` — no `changes`, and no
// reviewer-authored `comments` entries, because there are no conversations. So
// nine checks were pinning the arithmetic of two functions with no caller and
// no data, which is exactly what this file calls a check that certifies
// nothing.
//
// WHAT THE BUTTON SAYS NOW IS STILL CHECKED, one file over and one surface up:
// `reviseCountClause(pendingCount)` writes the round's instruction count into
// `.gly-revise-count`, and web/rounds-ux.mjs reads that off the real product.
// The claim that survives all of it is the reframe in rail.ts — the trail is an
// OUTGOING MESSAGE and not a history, so it has no browsable surface and its
// count rides the button that sends it.

// --- the instruction rail's arithmetic ---
//
// The rail is the panel's replacement and it is a GEOMETRY, not a list: a card
// sits at its mark's vertical position, and only moves when another card is
// already there. All of that is arithmetic on measured numbers, so all of it is
// checked here rather than in a browser, and the browser pass is left to prove
// the two things arithmetic cannot — that a jump lands, and that rail and sheet
// are never both on screen.

{
  // Anchors far enough apart never move: a card at its mark's own height is
  // the whole point, and pushing one that did not have to move would break the
  // connection between the card and the line it is about.
  const placed = stackCards([
    { run: 'a', anchorTop: 100, height: 60 },
    { run: 'b', anchorTop: 400, height: 60 },
  ]);
  check(
    'cards whose anchors do not collide keep their anchor tops',
    placed[0].top === 100 && placed[1].top === 400,
    placed,
  );
}

{
  // The collision rule, exactly as specified: sort by anchor top, push down,
  // 10px gap. The second card's anchor is 20px below the first's, but the first
  // is 60px tall — so the second lands at 100 + 60 + 10.
  const placed = stackCards([
    { run: 'a', anchorTop: 100, height: 60 },
    { run: 'b', anchorTop: 120, height: 40 },
  ]);
  check(
    'a colliding card is pushed down by exactly one gap',
    placed[1].top === 100 + 60 + RAIL_GAP,
    placed,
  );
}

{
  // Push-down CASCADES. Three anchors within a few pixels must end up as three
  // stacked cards, not two overlapping ones — the second card's pushed
  // position, not its anchor, is what the third has to clear.
  const placed = stackCards([
    { run: 'a', anchorTop: 100, height: 50 },
    { run: 'b', anchorTop: 105, height: 50 },
    { run: 'c', anchorTop: 110, height: 50 },
  ]);
  check(
    'push-down cascades through a run of colliding anchors',
    placed[1].top === 160 && placed[2].top === 220,
    placed.map((p) => p.top),
  );
}

{
  // Input order is not document order. The rule says sort by anchor top, and
  // the rail is built from /_galley/pending, which is in document order — but
  // document order is not screen order for a card whose mark moved.
  const placed = stackCards([
    { run: 'late', anchorTop: 500, height: 40 },
    { run: 'early', anchorTop: 100, height: 40 },
  ]);
  check(
    'stacking sorts by anchor top, whatever order it is handed',
    placed[0].run === 'early' && placed[1].run === 'late',
    placed.map((p) => p.run),
  );
}

{
  // THE STACKER CLAMPS NOTHING. It used to take a `ceiling` — the band's own
  // measured top, because the band was one flex item under a fold cluster in a
  // viewport-height column — and a mark scrolled off the top of the window had
  // a NEGATIVE anchor top that had to be kept rather than clamped to zero. The
  // rail is as tall as the document and its coordinates are the document's, so
  // neither case exists: there is no ceiling argument and no negative anchor.
  // What is left is the property that survived both — a card goes exactly where
  // its mark is unless another card is already there.
  const placed = stackCards([
    { run: 'high', anchorTop: 0, height: 60 },
    { run: 'low', anchorTop: 4200, height: 60 },
  ]);
  check(
    'a card sits at its mark, however far down the document that is',
    placed[0].top === 0 && placed[1].top === 4200,
    placed.map((p) => p.top),
  );
}

{
  // NO TWO CARDS MAY OCCUPY THE SAME PIXELS, for any anchors and any heights.
  // The stacker's whole job, stated as the property rather than as one case —
  // because the overlap a reviewer actually hit (a card drawn over the reply
  // box of the one above it) was never the arithmetic being wrong. It was the
  // arithmetic being fed a height measured before the card grew, which is a
  // FRESHNESS bug, and the pixel proof of the fix has to be in a browser
  // holding a card open across a poll. This is the half that can be checked
  // here: given true heights, the output never overlaps.
  const heights = [12, 240, 61, 8, 133, 47];
  const tops = [-200, 40, 41, 41, 900, 12];
  const cards = heights.map((height, i) => ({
    run: `r${i}`,
    anchorTop: tops[i],
    height,
  }));
  const stack = stackCards(cards, RAIL_GAP);
  let overlap = null;
  for (let i = 0; i < stack.length; i += 1) {
    for (let j = i + 1; j < stack.length; j += 1) {
      const a = stack[i];
      const b = stack[j];
      if (a.top < b.top + b.height && b.top < a.top + a.height) {
        overlap = [a, b];
      }
    }
  }
  check(
    'no two stacked cards ever overlap, whatever their heights',
    overlap === null,
    overlap,
  );
  // Three cards wanting the same top is the case that produced the report:
  // each is pushed clear of the one before it by its FULL height plus the gap.
  const crowd = stack.filter(
    (c) => c.run === 'r1' || c.run === 'r2' || c.run === 'r3',
  );
  check(
    'cards competing for one anchor are separated by height plus gap',
    crowd.every(
      (c, i) =>
        i === 0 || c.top >= crowd[i - 1].top + crowd[i - 1].height + RAIL_GAP,
    ),
    crowd,
  );
}

{
  // LIGHTING THE TEXT, WITHOUT A BROWSER. `litDecorations` is the whole of the
  // prose end: it asks `markRuns` — the ONE definition this bundle has of
  // "these inlines are one suggestion" — and lights the runs it is handed.
  //
  // FOUR CHECKS ARE DELETED WITH THEIR SUBJECT HERE, and each is named because
  // a deletion nobody justifies cannot be told from a defect somebody hid:
  // `the curve leaves the word sideways and arrives at the card sideways`,
  // `both control points lie between the ends, so the bend is a lean and not a
  // loop`, `the path is one cubic segment from the word to the card` and `a
  // nearly-level, nearly-adjacent card still bends by the floor, not by the
  // fraction` (with the box's own `the floor really does put the control points
  // outside the ends`). All five were claims about `connectorPath`'s control
  // points — the property no rendered path can be read for, which is why they
  // were here rather than in the browser pass. There is no path. They carried
  // nothing but the curve's shape, and the question they served — which words
  // is this card about — is what these two checks and layers §10 answer now.
  const del = schema.marks.del.create({
    author: 'agent',
    at: '2026-08-16T09:00:00Z',
    run: 'aaaa1111',
  });
  const ins = schema.marks.ins.create({
    author: 'agent',
    at: '2026-08-16T09:00:00Z',
    run: 'bbbb2222',
  });
  const doc = docOf(
    schema.node('paragraph', null, [
      schema.text('keep '),
      schema.text('brown', [del]),
      schema.text('red', [ins]),
      schema.text(' and the rest'),
    ]),
    para('a second paragraph with nothing marked in it'),
  );
  const spans = (set) => {
    const out = [];
    set.find().forEach((d) => out.push([d.from, d.to]));
    return out;
  };

  check(
    'nothing is lit when nothing is asked for',
    spans(litDecorations(doc, [])).length === 0 &&
      spans(litDecorations(doc, null)).length === 0,
  );

  // THE MARK'S OWN EXTENT AND NOT THE BLOCK'S. A light over the paragraph would
  // satisfy any check that counts decorations, which is why the RANGE is read.
  const one = spans(litDecorations(doc, ['aaaa1111']));
  check(
    'one run lights exactly the inlines that carry it',
    one.length === 1 && one[0][0] === 6 && one[0][1] === 11,
    one,
  );

  // BOTH HALVES OF A SUBSTITUTION TOGETHER. One span in the file is one
  // decision everywhere, and the card's `run` is the DELETED half's — so
  // lighting only that would answer half the question about a card whose whole
  // subject is the swap. App.litRuns reads `insRun` off the wire to get here;
  // this asserts the painter does the rest.
  const both = spans(litDecorations(doc, ['aaaa1111', 'bbbb2222']));
  check(
    'a substitution lights both halves, because one span is one decision',
    both.length === 2 && both[0][0] === 6 && both[1][1] === 14,
    both,
  );

  check(
    'a run that is not in the document lights nothing — an adrift card needs no rule',
    spans(litDecorations(doc, ['no-such-run'])).length === 0,
  );

  check(
    'sameRuns tells one lit set from another, so a hover that changes nothing dispatches nothing',
    sameRuns(['a', 'b'], ['a', 'b']) &&
      !sameRuns(['a'], ['a', 'b']) &&
      !sameRuns(['a', 'b'], ['b', 'a']) &&
      !sameRuns(null, []) &&
      sameRuns([], []),
  );
}

{
  // ONE SURFACE, TWO STATES. This is the invariant the whole narrow layout
  // rests on, so it is asserted directly rather than inferred from the CSS.
  const wide = railSurfaces({
    width: 1400,
    collapsed: false,
    sheetOpen: false,
  });
  check(
    'a wide viewport gets the rail and nothing else',
    wide.rail && !wide.bar && !wide.sheet,
    wide,
  );

  const collapsed = railSurfaces({
    width: 1400,
    collapsed: true,
    sheetOpen: false,
  });
  check(
    'collapsing hides the rail without summoning the sheet',
    !collapsed.rail && !collapsed.bar && !collapsed.sheet,
    collapsed,
  );

  // THE SHEET IS THE REVIEW'S LIST AT EVERY WIDTH, and this is the check that
  // had to change. It used to read `sheet === false` at 1400 whatever
  // `sheetOpen` said — correct while the sheet was the rail's REPLACEMENT for
  // the widths the rail does not exist at, and a straight regression the moment
  // settled and anchorless conversations moved into it: `↺ reopen` is the only
  // way back from a mis-clicked `✓ resolve`, and it would have lived on a
  // surface no desktop reviewer could open. Run red against the tracked build
  // it reports `{rail: true, sheet: false}`.
  const wideSheet = railSurfaces({
    width: 1400,
    collapsed: false,
    sheetOpen: true,
  });
  check(
    'a wide viewport can open the sheet — the settled conversations live there now',
    wideSheet.sheet === true,
    wideSheet,
  );
  // AND THE RAIL GOES WHEN IT DOES. One surface, one state: the invariant
  // below is not weakened by the sheet becoming reachable, it is enforced from
  // the other side.
  check(
    'and the rail is not painted underneath it',
    wideSheet.rail === false,
    wideSheet,
  );

  const narrow = railSurfaces({
    width: 800,
    collapsed: false,
    sheetOpen: false,
  });
  check(
    'a narrow viewport forces collapse and shows the bottom bar',
    !narrow.rail && narrow.bar && narrow.collapsed === true,
    narrow,
  );

  const sheet = railSurfaces({ width: 800, collapsed: false, sheetOpen: true });
  check(
    'the sheet never renders beside the rail',
    sheet.sheet === true && sheet.rail === false,
    sheet,
  );

  // ONE SURFACE, ONE STATE — stated over every combination rather than over the
  // one that used to be unreachable. This is the invariant railSurfaces exists
  // for and it survives the sheet becoming a wide-screen surface unchanged.
  const every = [];
  for (const width of [800, 991, RAIL_MIN_WIDTH, 1400]) {
    for (const collapsed of [false, true]) {
      for (const sheetOpen of [false, true]) {
        every.push({
          at: { width, collapsed, sheetOpen },
          got: railSurfaces({ width, collapsed, sheetOpen }),
        });
      }
    }
  }
  check(
    'rail and sheet cannot both be true at any width, collapse or sheet state',
    every.every((e) => !(e.got.rail && e.got.sheet)),
    every.filter((e) => e.got.rail && e.got.sheet),
  );

  // The boundary is 992 INCLUSIVE of the rail — the handoff's "wide (>=992px)".
  check(
    'the surface boundary is exactly RAIL_MIN_WIDTH',
    railSurfaces({ width: RAIL_MIN_WIDTH, collapsed: false, sheetOpen: false })
      .rail === true &&
      railSurfaces({
        width: RAIL_MIN_WIDTH - 1,
        collapsed: false,
        sheetOpen: false,
      }).bar === true,
  );
}

{
  // The census counts THE SERVER'S ANSWER. This is the check that documents the
  // constraint: censusCounts is handed the /_galley/pending payload and has no
  // access to a card list, so there is no path by which it can report what was
  // rendered instead of what is pending.
  const view = { comments: [{ key: 'cm-1' }, { key: 'cm-2' }] };
  const c = censusCounts(view);
  check('the census counts every thread', c.threads === 2, c);
  check(
    'an empty payload counts to zero rather than throwing',
    censusCounts({}).threads === 0 && censusCounts(undefined).threads === 0,
  );
  // The payload carries no suggestions, so the census has no suggestion half
  // left: nothing beside the thread numbers.
  check(
    'the census reports thread numbers and nothing else',
    Object.keys(c).sort().join(',') === 'threads',
    Object.keys(c),
  );

  // The total is still every open conversation, which is the number Approve
  // is withheld on. A doc instruction ALONE must not read as a settled document.
  check(
    'a lone open doc instruction still counts in the total verdictLabel reads',
    censusCounts({
      comments: [{ key: 'cd-1', anchor: 'document' }],
    }).threads === 1,
  );
  check(
    'and verdictLabel still refuses Approve on it',
    verdictLabel({
      comments: [{ key: 'cd-1', anchor: 'document' }],
    }) === REVISE_IDLE,
  );
}

{
  // ✓ ALL IS DELETED, AND SO ARE THE TEN CHECKS ON ITS COPY. `SWEEP_TITLE`,
  // `SWEEP_ACCEPT`, `SWEEP_SETTLE`, `sweepLabel` and `sweepSaid` were the
  // tooltip, the label and the after-sentence of a button that POSTed
  // /_galley/sweep — a 404 on the rounds-only server, on a control `makeCensus`
  // had already stopped appending to the strip. Neither population it swept
  // exists: there are no proposals to accept and no conversations to settle.
  //
  // WHAT THE CHECKS WERE FOR IS RECORDED IN rail.ts RATHER THAN LOST. They read
  // the reviewer-visible STRINGS and not the predicate, deliberately, because
  // the defect was a tooltip promising "open questions survive" over a predicate
  // (`Answered()` = the agent spoke last) that closed every agent question
  // nobody had replied to — a check on the predicate would have been green
  // through the whole of it. That rule outlives the button and is written down
  // where the constants were.

  // Unplaced instructions are complete cards in the rail now; this is the
  // section label above them, not a direction to another surface.
  // IT NAMES THE STATE, IT DOES NOT COUNT IT. `1 unplaced instruction` put a
  // number where the reviewer can already see the cards, and said nothing about
  // WHY the section exists — the fact that explains it (the words these
  // instructions were about are not in the document any more) was being carried
  // by an apologising sentence inside every card, which is deleted.
  check(
    'the rail head names what happened to an unplaced instruction',
    unplacedSaid(1) === 'unplaced \u00b7 its words were removed',
    unplacedSaid(1),
  );
  check(
    'and the plural keeps a count, because "its words" is false of several',
    unplacedSaid(3) === 'unplaced \u00b7 3 \u00b7 their words were removed',
    unplacedSaid(3),
  );
  check(
    'and it says nothing when every instruction is placed',
    unplacedSaid(0) === '' &&
      unplacedSaid(undefined) === '' &&
      unplacedSaid(-1) === '',
  );
}

{
  const runs = ['a', 'b', 'c'];
  check('j steps forward and wraps', stepPending(runs, 'c', 1) === 'a');
  check('k steps back and wraps', stepPending(runs, 'a', -1) === 'c');
  // Nothing stepped yet: forward starts at the top of the document, back starts
  // at the bottom. Starting both at the top would make `k` a no-op the first
  // time it is pressed, which reads as a broken key.
  check(
    'stepping with nothing selected starts at the right end',
    stepPending(runs, null, 1) === 'a' && stepPending(runs, null, -1) === 'c',
  );
  // The stepped mark can be decided out from under the stepper.
  check(
    'stepping from a run that has gone starts over',
    stepPending(runs, 'deleted', 1) === 'a',
  );
  check(
    'stepping an empty document reports nothing',
    stepPending([], null, 1) === null,
  );
}

{
  check(
    'keys are ignored inside the document and inside inputs',
    keyTargetIsEditable({ isContentEditable: true }) === true &&
      keyTargetIsEditable({ tagName: 'TEXTAREA' }) === true &&
      keyTargetIsEditable({ tagName: 'INPUT' }) === true,
  );
  check(
    'keys are not ignored on ordinary chrome',
    keyTargetIsEditable({ tagName: 'BUTTON' }) === false &&
      keyTargetIsEditable(null) === false,
  );
}

{
  // ONE KEYDOWN CONTRACT FOR EVERY PLACE GALLEY TAKES PROSE. It was three
  // handlers, of which only two existed: the reply boxes filed on Enter and
  // the composer that CREATES a comment had no handler at all, so a reviewer
  // who typed a comment and pressed Enter got nothing. submitOnEnter is the
  // one copy now, and web/typing.mjs drives it from a real keyboard; this
  // pins the decision itself, with no DOM in the way.
  const fired = [];
  let prevented = 0;
  const field = {
    handler: null,
    addEventListener(name, fn) {
      if (name === 'keydown') {
        this.handler = fn;
      }
    },
  };
  const back = submitOnEnter(field, () => fired.push(true));
  check(
    'submitOnEnter returns the field it wired, so a caller can chain',
    back === field && typeof field.handler === 'function',
  );
  const press = (key, shiftKey) =>
    field.handler({
      key,
      shiftKey,
      preventDefault: () => {
        prevented += 1;
      },
    });

  press('Enter', false);
  check(
    'Enter files what was typed, and takes the key so nothing else sees it',
    fired.length === 1 && prevented === 1,
  );
  press('Enter', true);
  check(
    'Shift-Enter breaks the line — it files nothing and the key is NOT taken',
    fired.length === 1 && prevented === 1,
  );
  press('a', false);
  check(
    'and an ordinary key is none of its business',
    fired.length === 1 && prevented === 1,
  );
}

// --- a heading grip's section span ---
//
// A section is a heading plus everything under it up to the NEXT heading of the
// same level OR SHALLOWER. "Up to the next heading" alone is wrong and wrong in
// the direction that is hardest to notice: an h3 inside an h2's section would
// end the h2, so the grip beside a section the reviewer can plainly see is six
// paragraphs long would outline the first one and file a thread about it. The
// arithmetic is pure, so it is checked here rather than by dragging in a
// browser.

{
  const heading = (level, text) =>
    schema.node('heading', { level }, [schema.text(text)]);
  const doc = docOf(
    heading(2, 'Design'), // 0
    para('first'), // 1
    heading(3, 'Detail'), // 2
    para('second'), // 3
    heading(2, 'Testing'), // 4
    para('third'), // 5
    para('trailing'), // 6
  );

  // The position BEFORE top-level child i — what the grip resolves from a
  // heading's DOM node.
  const posOf = (i) => {
    let at = 0;
    for (let n = 0; n < i; n += 1) {
      at += doc.child(n).nodeSize;
    }
    return at;
  };
  // Which top-level children a span covers.
  const blocksIn = (span) => {
    const out = [];
    let at = 0;
    for (let i = 0; i < doc.childCount; i += 1) {
      const size = doc.child(i).nodeSize;
      if (at >= span.from && at + size <= span.to) {
        out.push(i);
      }
      at += size;
    }
    return out;
  };

  check(
    'an h2 takes its nested h3 with it, and stops before the next h2',
    blocksIn(sectionSpan(doc, posOf(0))).join(',') === '0,1,2,3',
    blocksIn(sectionSpan(doc, posOf(0))),
  );
  check(
    'a nested h3 takes only what is under it',
    blocksIn(sectionSpan(doc, posOf(2))).join(',') === '2,3',
    blocksIn(sectionSpan(doc, posOf(2))),
  );
  check(
    'the last section runs to the end of the document',
    blocksIn(sectionSpan(doc, posOf(4))).join(',') === '4,5,6',
    blocksIn(sectionSpan(doc, posOf(4))),
  );
  // The span must land ON block boundaries: the scope outline draws a node
  // decoration over every block wholly inside it, so a span that started
  // inside the heading would leave the heading itself unoutlined.
  check(
    'a section span starts at its heading and ends on a block boundary',
    sectionSpan(doc, posOf(2)).from === posOf(2) &&
      sectionSpan(doc, posOf(2)).to === posOf(4),
    sectionSpan(doc, posOf(2)),
  );
  // Not a heading: there is no section, and the answer must say so rather
  // than outlining a paragraph and calling it a section.
  check(
    'a position that is not a heading has no section span',
    sectionSpan(doc, posOf(1)) === null,
  );
}

// --- the block grip ---
//
// Which top-level blocks get a grip, what the grip says, and where two of them
// go when their blocks are closer than one grip. All of it is arithmetic over a
// document and a list of threads, so it is checked here; what a browser has to
// prove (the grip is visible, pressable, in the gutter) is web/grip.mjs's.
//
// The document goes through fragmentSchema, the one that carries every node the
// editor builds — image, note, front matter and display math included — so a
// kind this file names is a kind the browser actually has.

{
  const f = fragmentSchema;
  const text = (t) => [f.text(t)];
  const nestedFence = f.node('bulletList', null, [
    f.node('listItem', null, [
      f.node('paragraph', null, text('run this')),
      f.node('codeBlock', { language: 'bash' }, text('nested')),
    ]),
  ]);
  const doc = f.node('doc', null, [
    f.node('frontMatter', null, text('title: x')), // 0
    f.node('heading', { level: 1 }, text('Title')), // 1
    f.node('paragraph', null, text('intro')), // 2
    f.node('heading', { level: 2 }, text('Design')), // 3
    f.node('table', null, [
      f.node('tableRow', null, [
        f.node('tableHeader', null, [f.node('paragraph', null, text('key'))]),
        f.node('tableHeader', null, [f.node('paragraph', null, text('value'))]),
      ]),
    ]), // 4
    nestedFence, // 5
    f.node('blockquote', null, [f.node('paragraph', null, text('quoted'))]), // 6
    f.node('note', null, []), // 7 — counted in the index, never a target
    f.node('codeBlock', { language: 'bash' }, text('npm install')), // 8
    f.node('image', { src: 'flow.svg', alt: 'flow' }), // 9
    f.node('codeBlock', { language: 'mermaid' }, text('graph TD; a-->b')), // 10
    f.node('mathBlock', null, text('E = mc^2')), // 11
    f.node('horizontalRule'), // 12
    f.node('paragraph', null, text('the end')), // 13
  ]);
  const targets = gripTargets(doc);
  const posOf = (i) => {
    let at = 0;
    for (let n = 0; n < i; n += 1) {
      at += doc.child(n).nodeSize;
    }
    return at;
  };

  check(
    'the grip goes on front matter, headings, a table, a fence, an image, a diagram and display math, in document order',
    targets.map((t) => `${t.kind}@${t.index}`).join(',') ===
      'frontMatter@0,heading@1,heading@3,table@4,codeBlock@8,image@9,codeBlock@10,mathBlock@11',
    targets.map((t) => `${t.kind}@${t.index}`),
  );
  // The index is the top-level ordinal WITH notes counted, because that is
  // BlockRef.Index, and the grip finds its block's key by it at click time.
  check(
    "each target's position is the top-level child its index names",
    targets.every((t) => t.pos === posOf(t.index)),
    targets.map((t) => [t.index, t.pos]),
  );
  check(
    'no paragraph, list, blockquote, note or rule gets a grip, and neither does the fence inside the list item',
    !targets.some((t) =>
      [
        'paragraph',
        'bulletList',
        'blockquote',
        'note',
        'horizontalRule',
      ].includes(t.kind),
    ) && !targets.some((t) => t.index === 5),
    targets,
  );
  check(
    'only the image and the diagram are figures',
    targets
      .filter((t) => t.figure)
      .map((t) => t.index)
      .join(',') === '9,10',
    targets.filter((t) => t.figure),
  );
  check(
    'every kind the grip goes on is a node the editor builds',
    GRIP_KINDS.length === 6 &&
      GRIP_KINDS.every((k) =>
        Object.prototype.hasOwnProperty.call(fragmentSchema.nodes, k),
      ),
    GRIP_KINDS,
  );

  check(
    'the face is + with no instructions, the count up to nine, and 9+ past it',
    [0, 1, 9, 10, 31].map(gripFace).join(' ') === '+ 1 9 9+ 9+',
    [0, 1, 9, 10, 31].map(gripFace),
  );

  const heading = { kind: 'heading', figure: false };
  const said = {
    heading: gripLabel(heading, '## Design', 0),
    headingTwo: gripLabel(heading, '## Design', 2),
    headingOne: gripLabel(heading, 'Design', 1),
    table: gripLabel({ kind: 'table', figure: false }, 'table: key, value', 0),
    diagram: gripLabel(
      { kind: 'codeBlock', figure: true },
      'mermaid: graph TD',
      0,
    ),
    image: gripLabel({ kind: 'image', figure: true }, 'flow (flow.svg)', 0),
    math: gripLabel({ kind: 'mathBlock', figure: false }, 'E = mc^2', 0),
    front: gripLabel({ kind: 'frontMatter', figure: false }, '---', 0),
    code: gripLabel({ kind: 'codeBlock', figure: false }, 'bash: npm', 0),
  };
  check(
    'a heading grip names its section in words, without the markdown hashes',
    said.heading === 'Add an instruction on the section "Design"' &&
      said.headingOne ===
        'Add an instruction on the section "Design" (1 already)',
    said,
  );
  check(
    'and says how many instructions it already carries',
    said.headingTwo ===
      'Add an instruction on the section "Design" (2 already)',
    said.headingTwo,
  );
  check(
    'every other kind names what it is',
    said.table === 'Add an instruction on this table' &&
      said.diagram === 'Add an instruction on this diagram' &&
      said.image === 'Add an instruction on this figure' &&
      said.math === 'Add an instruction on this equation' &&
      said.front === 'Add an instruction on the front matter' &&
      said.code === 'Add an instruction on this code block',
    said,
  );
  check(
    'no grip label carries a raw markdown # or |',
    Object.values(said).every((l) => !/[#|]/.test(l)),
    said,
  );
  const long = gripLabel(heading, `## ${'word '.repeat(40)}`, 0);
  check(
    'a long heading is bounded in the label, as it is in a card head',
    long.length < 'Add an instruction on the section ""'.length + 50 &&
      long.includes('…'),
    long,
  );

  const threads = [
    { anchor: 'block', anchorKey: 'bk-a' },
    { anchor: 'block', anchorKey: 'bk-a', region: { x: 0, y: 0, w: 1, h: 1 } },
    { anchor: 'block', anchorKey: 'bk-b' },
    { anchor: 'range', anchorKey: '', run: 'r1' },
    { anchor: 'document', anchorKey: '' },
  ];
  check(
    "a grip counts the block's own instructions, a region comment included",
    gripCount(threads, 'bk-a') === 2 && gripCount(threads, 'bk-b') === 1,
    [gripCount(threads, 'bk-a'), gripCount(threads, 'bk-b')],
  );
  check(
    'and never a range or a whole-document instruction',
    gripCount(threads, '') === 0 && gripCount(threads, 'bk-none') === 0,
    gripCount(threads, ''),
  );

  const stacked = stackGrips([0, 10, 100], 32, 4);
  check(
    'a grip closer than one grip to the one above is pushed clear of it',
    stacked.join(',') === '0,36,100',
    stacked,
  );
  const clear = stackGrips([0, 50, 200], 32, 4);
  check(
    'grips already clear of each other are not moved',
    clear.join(',') === '0,50,200',
    clear,
  );
  const crowd = stackGrips([5, 5, 6, 40, 41], 32, 4);
  check(
    'a crowd of grips comes out non-decreasing, each clear of the last',
    crowd.every((t, i) => i === 0 || t - crowd[i - 1] >= 36),
    crowd,
  );
}

{
  // --- keepPlace's two coordinates, over a document with nesting in it ---
  //
  // EVERY server-side mutation replaces the whole document, so the caret is
  // put back from a remembered top-level block index plus the block's text up
  // to the caret. `placeIn` has to turn that text length back into a POSITION,
  // and a position is not a character count: every nested node between the
  // block's inner start and the caret costs a position that the flattened text
  // does not spend. A list item is two of them, and each block boundary the
  // prefix crosses is three more — so `inner + prefix.length` lands short, and
  // it lands short SILENTLY, because the block's flattened text still starts
  // with the prefix and the guard passes.
  //
  // MEASURED IN A BROWSER before this was written: caret at 440 in the third
  // item of a three-item list, one `galley suggest` in a paragraph far below,
  // the caret restored to 432, and the reviewer's next keystroke landed inside
  // "charlie w|ith". See docs/superpowers/reviews/2026-08-07-keepplace-edges.md.
  const item = (text) => schema.node('listItem', null, [para(text)]);
  const doc = docOf(
    para('Alpha, an ordinary flat paragraph.'),
    schema.node('bulletList', null, [
      item('alpha item'),
      item('bravo item'),
      item('charlie item'),
    ]),
    schema.node('blockquote', null, [para('quoted prose')]),
    schema.node('codeBlock', null, [schema.text('fence one\nfence two')]),
  );
  // The inner start of top-level block `i` of `d` — one past its opening
  // token, which is where placeIn measures a prefix from.
  const innerOf = (d, i) => {
    let at = 0;
    for (let n = 0; n < i; n += 1) {
      at += d.child(n).nodeSize;
    }
    return at + 1;
  };
  const startOf = (i) => innerOf(doc, i);
  // The place the reviewer's caret at `pos` would have been remembered as,
  // built the way rememberPlace builds it.
  const placeAt = (pos) =>
    placeOf(
      EditorState.create({
        schema,
        doc,
        selection: TextSelection.create(doc, pos),
      }),
    );

  // A flat top-level paragraph is the case the mitigation always handled, and
  // it must keep working: it is the shape most of the document is.
  const flat = startOf(0) + 6;
  check(
    'a caret in a flat paragraph round-trips',
    placeIn(doc, placeAt(flat)) === flat,
    [placeIn(doc, placeAt(flat)), flat],
  );

  // One level of nesting: bulletList > listItem > paragraph. `inner +
  // prefix.length` is two positions short here.
  const first = startOf(1) + 2 + 5;
  check(
    'a caret in the FIRST list item round-trips',
    placeIn(doc, placeAt(first)) === first,
    [placeIn(doc, placeAt(first)), first],
  );

  // Two block boundaries crossed as well: the flattened prefix spends one '\n'
  // where the document spends four positions, twice over.
  const third =
    startOf(1) +
    doc.child(1).child(0).nodeSize +
    doc.child(1).child(1).nodeSize +
    2 +
    7;
  check(
    'a caret in the THIRD list item round-trips',
    placeIn(doc, placeAt(third)) === third,
    [placeIn(doc, placeAt(third)), third],
  );

  const quoted = startOf(2) + 1 + 6;
  check(
    'a caret in a blockquote round-trips',
    placeIn(doc, placeAt(quoted)) === quoted,
    [placeIn(doc, placeAt(quoted)), quoted],
  );

  // A code fence's newlines are real characters in one text node, so this one
  // was already right — it is here so a fix cannot break it.
  const fenced = startOf(3) + 14;
  check(
    'a caret in a code fence round-trips',
    placeIn(doc, placeAt(fenced)) === fenced,
    [placeIn(doc, placeAt(fenced)), fenced],
  );

  // The refusal the mitigation is built on: when the block at the remembered
  // index is not the block the caret was in, say nothing rather than guess.
  const moved = docOf(
    para('Alpha, an ordinary flat paragraph.'),
    para('something else entirely'),
  );
  check(
    'a block whose text no longer matches is refused, not guessed at',
    placeIn(moved, placeAt(third)) === null,
    placeIn(moved, placeAt(third)),
  );
  check(
    'an index past the end of the new document is refused',
    placeIn(docOf(para('only one')), placeAt(quoted)) === null,
  );

  // --- a RANGE has two ends, and both of them are anchorable ---
  //
  // `rememberPlace` used to refuse a range outright, so a reviewer who swept
  // out a phrase and then had a suggestion land lost the selection AND the
  // caret: measured in a browser as a 20-character sweep at 194→214 of a
  // 1349-position document ending collapsed at 1351, the very end, off screen.
  // That matters more than a collapsed caret because the selection is the
  // INPUT to galley's own verbs — strike and the comment composer both operate
  // on it.
  //
  // Both ends go through the same placeOf/placeIn machinery the caret does.
  // The extra guard is on the TEXT BETWEEN them: two ends that each resolve
  // honestly still do not prove the span between them is the reviewer's, if
  // something inside it changed. Same rule as everywhere else here — a
  // selection restored over the wrong words is worse than one the reviewer can
  // see has gone.
  const rangeAt = (from, to, back = false) =>
    placeOf(
      EditorState.create({
        schema,
        doc,
        selection: TextSelection.create(
          doc,
          back ? to : from,
          back ? from : to,
        ),
      }),
    );
  const span = (d, place) => {
    const s = spanIn(d, place);
    return s === null ? null : [s.from, s.to, s.back];
  };

  check(
    'a forward range in a flat paragraph round-trips',
    JSON.stringify(span(doc, rangeAt(flat, flat + 4))) ===
      JSON.stringify([flat, flat + 4, false]),
    span(doc, rangeAt(flat, flat + 4)),
  );

  // DIRECTION IS PART OF THE SELECTION. A sweep right-to-left has its head at
  // the START of the range, and that is where the next keystroke goes;
  // restoring it forwards moves the caret to the other end of the phrase.
  check(
    'a backward range keeps its direction',
    JSON.stringify(span(doc, rangeAt(flat, flat + 4, true))) ===
      JSON.stringify([flat, flat + 4, true]),
    span(doc, rangeAt(flat, flat + 4, true)),
  );

  // Nesting, on BOTH ends: each is the position arithmetic that was wrong for
  // the caret, and getting it wrong on one end alone skews the span.
  check(
    'a range inside the third list item round-trips',
    JSON.stringify(span(doc, rangeAt(third, third + 4))) ===
      JSON.stringify([third, third + 4, false]),
    span(doc, rangeAt(third, third + 4)),
  );

  // Across two top-level blocks, with everything between them unchanged.
  check(
    'a range spanning blocks round-trips when nothing between it changed',
    JSON.stringify(span(doc, rangeAt(flat, quoted))) ===
      JSON.stringify([flat, quoted, false]),
    span(doc, rangeAt(flat, quoted)),
  );

  // Only the head end lost its block: fall back to the end that did resolve,
  // collapsed. A caret the reviewer can see beats a selection that is a guess.
  const requoted = docOf(
    para('Alpha, an ordinary flat paragraph.'),
    schema.node('bulletList', null, [
      item('alpha item'),
      item('bravo item'),
      item('charlie item'),
    ]),
    schema.node('blockquote', null, [para('different prose entirely')]),
    schema.node('codeBlock', null, [schema.text('fence one\nfence two')]),
  );
  check(
    'a range with one end lost falls back to the end that resolved',
    JSON.stringify(span(requoted, rangeAt(flat, quoted))) ===
      JSON.stringify([flat, flat, false]),
    span(requoted, rangeAt(flat, quoted)),
  );

  // Both ends resolve, but a block BETWEEN them grew — so the span would now
  // cover text the reviewer never selected. Refuse the range, keep the head.
  const stretched = docOf(
    para('Alpha, an ordinary flat paragraph.'),
    schema.node('bulletList', null, [
      item('alpha item'),
      item('bravo item'),
      item('charlie item'),
      item('delta item'),
    ]),
    schema.node('blockquote', null, [para('quoted prose')]),
    schema.node('codeBlock', null, [schema.text('fence one\nfence two')]),
  );
  const requoted2 = innerOf(stretched, 2) + 1 + 6;
  check(
    'a range whose contents changed collapses to the head, not the wrong span',
    JSON.stringify(span(stretched, rangeAt(flat, quoted))) ===
      JSON.stringify([requoted2, requoted2, false]),
    span(stretched, rangeAt(flat, quoted)),
  );

  // Neither end survives: the documented refusal, unchanged — and the caller
  // routes it through the arrival strip rather than absorbing it.
  check(
    'a range with neither end resolvable is refused, not guessed at',
    span(
      docOf(para('nothing like it'), para('nor this')),
      rangeAt(third, quoted),
    ) === null,
    span(
      docOf(para('nothing like it'), para('nor this')),
      rangeAt(third, quoted),
    ),
  );

  // A collapsed caret still comes back as a zero-width span, so the caller has
  // one shape to handle and the controls above keep their meaning.
  check(
    'a collapsed caret is a zero-width span',
    JSON.stringify(span(doc, placeAt(third))) ===
      JSON.stringify([third, third, false]),
    span(doc, placeAt(third)),
  );
}

// --- what a thread card says it is about ---
//
// A thread has three shapes and they are NOT interchangeable: a range thread
// hangs on a highlight, a block thread is about a whole block, and a document
// thread is about the file. Before the anchors landed all three rendered the
// same way — and the two that have no mark were labelled "not tied to a mark",
// which is true of a range thread whose highlight went missing and simply
// WRONG of the other two: a document thread is not adrift, it is exactly where
// it belongs. Getting that sentence onto the wrong card tells a reviewer their
// comment lost its place when nothing of the sort happened.

{
  const range = threadLabel({ heading: 'the request path' });
  check(
    'a range thread is labelled by the text it highlights',
    range.label === 'the request path' && range.adrift === false,
    range,
  );

  const orphan = threadLabel({ heading: 'gone' }, { anchored: false });
  // THE APOLOGY IS DELETED AND THE FACT IS KEPT. `adrift` is still the only
  // thing that discriminates this population — it dashes the card and withholds
  // the jump — but the grey sentence it used to carry ("this instruction is not
  // tied to a mark — its original highlight is gone; you can still delete it")
  // told the reviewer about a verb they were looking at and never said WHICH
  // WORDS went. The card quotes its lost anchor now, struck through in the
  // removal colour; the section head names the state once.
  check(
    'a range thread with no mark is the only one called adrift',
    orphan.adrift === true && orphan.note === '',
    orphan,
  );

  const doc = threadLabel({
    anchor: 'document',
    heading: 'the whole document',
  });
  check(
    'a document thread is about the whole document, and is never adrift',
    doc.label === 'whole document' && doc.adrift === false && doc.note === '',
    doc,
  );

  const heading = threadLabel({
    anchor: 'block',
    blockKind: 'heading',
    heading: '## Design',
  });
  check(
    'a thread on a heading cards as a section thread',
    heading.label === 'on §Design' && heading.adrift === false,
    heading,
  );

  const block = threadLabel({
    anchor: 'block',
    blockKind: 'image',
    heading: 'the request path (flow.png)',
  });
  check(
    'a thread on a block names the block',
    block.label === 'on the request path (flow.png)' && block.adrift === false,
    block,
  );

  // --- AND THE HEAD IS BOUNDED, WHICH IS §7 OF THE LIVE REVIEW ---
  //
  // Court filed an instruction on a passage spanning many blocks and the card
  // that appeared wore the WHOLE passage as its head: roughly twenty lines of
  // capitals, a card that was mostly its own title. The composer's head had
  // carried a stated bound since it was written (COMPOSER_QUOTE_CHARS) and the
  // CARD's had none, so the two surfaces quoted the same anchor a moment apart
  // and only one of them cut it.
  //
  // READ AGAINST THE APP'S OWN RESERVE, not a copy of it: `HEAD_QUOTE_CHARS`
  // is imported, so retuning the bound with the rail's width or the head's type
  // moves this check with it rather than leaving it asserting a number nothing
  // uses any more.
  const longSaid =
    'Four Cognito behaviors that shaped this Each of these was measured ' +
    'rather than read, and each explains a decision above that otherwise ' +
    'looks arbitrary.';
  const long = threadLabel({ heading: longSaid });
  check(
    'a card head quotes a bounded anchor, not the whole selected passage',
    long.label.length === HEAD_QUOTE_CHARS &&
      long.label.endsWith('\u2026') &&
      longSaid.startsWith(long.label.slice(0, -1)),
    long,
  );
  // THE NEWLINES GO WITH IT. A selection spanning blocks arrives with the block
  // boundaries in it, and a head is ONE LINE of chrome — a bound that counted
  // characters and kept the newlines would still draw a paragraph.
  const wrapped = threadLabel({ heading: 'first block\n\nsecond block' });
  check(
    'and a multi-block anchor is flattened to one line',
    wrapped.label === 'first block second block',
    wrapped,
  );
  // THE BLOCK BRANCHES ARE BOUND BY THE SAME FUNCTION, because a section
  // heading can be as long as any sentence and the § branch had its own
  // spelling of the label. One cut, two callers.
  const longHeading = threadLabel({
    anchor: 'block',
    blockKind: 'heading',
    heading: `## ${longSaid}`,
  });
  check(
    'a long section heading is bounded too',
    longHeading.label.startsWith('on \u00a7') &&
      longHeading.label.endsWith('\u2026') &&
      longHeading.label.length === HEAD_QUOTE_CHARS + 4,
    longHeading,
  );
  // A SHORT ANCHOR IS UNTOUCHED, which is the half a bound gets wrong by
  // ellipsizing everything. `elide` cuts only past the reserve.
  check(
    'and an anchor inside the bound is left exactly as it was',
    threadLabel({ heading: 'the request path' }).label === 'the request path',
  );
}

// --- the instruction box grows with what is typed into it ---
//
// §6 of the live review: *"overall instruction here pretty small and didn't
// auto expand"*. Three surfaces set `rows` and none of them grew — `rows` is a
// STARTING height, not a size, so a reviewer writing a second sentence watched
// the first one scroll out of a three-line box.
//
// THE CAP IS THE STYLESHEET'S AND THIS PROVES THE HANDOFF RATHER THAN THE
// NUMBER. `growOnInput` writes the height from `scrollHeight` and reads back
// what the browser allowed, so the cap can live in exactly one place (each
// box's own `max-height`) instead of being a number in the TypeScript that
// disagrees with the CSS the day either moves. There is no layout engine here,
// so what is checked is the CONTRACT: it listens for `input`, it resets to
// `auto` before measuring (without which a box that grew never shrinks), and it
// writes both `height` and `overflowY`.
{
  const events = {};
  const style = { height: '', overflowY: '' };
  let scrollHeight = 96;
  const fake = {
    style,
    clientHeight: 96,
    addEventListener(type, fn) {
      events[type] = fn;
    },
    get scrollHeight() {
      // The reset is what makes a shrink possible, so it is what is observed:
      // a real textarea's scrollHeight is only honest once the height it is
      // being measured against is `auto`.
      return style.height === 'auto' ? scrollHeight : 999;
    },
  };
  growOnInput(fake);
  check(
    'growOnInput listens for input and nothing else',
    typeof events.input === 'function' && Object.keys(events).length === 1,
    Object.keys(events),
  );
  events.input();
  check(
    'it measures against a reset height, so a shrunk box really shrinks',
    style.height === '96px',
    style.height,
  );
  check(
    'and it hides the scrollbar while nothing is hidden',
    style.overflowY === 'hidden',
    style.overflowY,
  );
  // THE CLAMP BIT: the stylesheet's `max-height` held the box under what the
  // content wants, so something IS below the fold and the box has to scroll.
  scrollHeight = 400;
  events.input();
  check(
    'and it scrolls once the stylesheet\u2019s own cap has bitten',
    style.height === '400px' && style.overflowY === 'auto',
    JSON.stringify(style),
  );
}

// AND THE CAP IS ONE RULE FOR ALL THREE BOXES. The whole-document box, the
// composer and the edit box are one design: one type, one padding and one cap,
// half the window, read off the BUILT stylesheet (a separate esbuild output the
// binary embeds). No other rule may give any of them a cap of its own, or the
// three grow to three different heights again.
{
  let css = '';
  try {
    css = readFileSync(
      new URL('../internal/serve/assets/editor.css', import.meta.url),
      'utf8',
    );
  } catch {
    css = '';
  }
  const boxes = [
    '.gly-overall-input',
    '.gly-composer-text',
    '.gly-thread-edit-text',
  ];
  // INNERMOST RULES ONLY: a body may hold no brace, so a rule inside
  // `@media (…) { … }` is read as itself rather than folded into the media
  // block's body, where its selector could not be seen.
  const rules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({
    sel: m[1].split(',').map((x) => x.trim()),
    body: m[2],
  }));
  const shared = rules.find(
    (r) =>
      boxes.every((b) => r.sel.includes(b)) &&
      /max-height:\s*50vh/.test(r.body),
  );
  check(
    'the three comment boxes share one rule: one font, one cap at half the window',
    !!shared && /font:\s*13px\/1\.5/.test(shared.body),
    shared,
  );
  const ownCaps = rules.filter(
    (r) =>
      r !== shared &&
      r.sel.some((x) => boxes.includes(x)) &&
      /max-height/.test(r.body),
  );
  check(
    'and none of the three carries a cap of its own',
    !!css && ownCaps.length === 0,
    ownCaps,
  );
}

// --- the overall thread ---
//
// R7: there was nowhere to say anything about the file as a whole. The rail
// pins one permanent card for it, and the document-anchored threads are ITS
// entries — which means they must be taken OUT of the ordinary card list, or a
// document note renders twice, once in each place. That is R9's complaint
// (one comment, two objects) reintroduced by a new surface, so the split is
// arithmetic here rather than a filter written twice in the DOM code.

{
  const threads = [
    { key: 'a', anchor: 'document', heading: 'the whole document' },
    { key: 'b', heading: 'a range thread' },
    { key: 'd', anchor: 'block', anchorKey: 'bk-1', heading: 'a block' },
  ];
  check(
    'the overall card takes every document thread',
    overallThreads(threads)
      .map((t) => t.key)
      .join('') === 'a',
    overallThreads(threads),
  );
  check(
    'and the rail keeps everything that is not one',
    railThreads(threads)
      .map((t) => t.key)
      .join('') === 'bd',
    railThreads(threads),
  );
  // Asserted as a PARTITION rather than as two filters: every thread lands in
  // exactly one region, which is the property "nothing is dropped" actually
  // depends on. Two separate membership checks would still pass on the day a
  // third state fell through both of them.
  const all = [...overallThreads(threads), ...railThreads(threads)]
    .map((t) => t.key)
    .sort();
  check(
    'the two regions are a partition — nothing dropped, nothing twice',
    all.join('') === 'abd',
    all,
  );
}

// --- a block note shows its comment's words, found by ID ---
//
// galley writes a block comment's mark as <note anchor id> with no text: the
// words live in the unsent round, and the file carries only the ID. So the
// amber box gets its words from the instruction data, painted as a WIDGET
// DECORATION inside the note — never as DOM appended into .ProseMirror, which
// is content, and never by matching the note's text or its place in the
// document order. The ID is the one link, and these checks pin that it is.
//
// The fake `document` is the probe's only DOM: the widget's builder runs
// lazily, so the check reads back the real span it would put on the page.
{
  const fakeDocument = {
    createElement: (tag) => ({ tagName: tag, className: '', textContent: '' }),
  };
  const widgetsOf = (doc, words) => {
    const had = globalThis.document;
    globalThis.document = fakeDocument;
    try {
      return noteWordDecorations(doc, words)
        .find()
        .map((d) => {
          const el = d.type.toDOM(null, () => d.from);
          return {
            id: doc.resolve(d.from).parent.attrs.id,
            text: el.textContent,
            cls: el.className,
            key: d.spec.key,
          };
        });
    } finally {
      globalThis.document = had;
    }
  };
  const para = (text) =>
    fragmentSchema.nodes.paragraph.create(null, fragmentSchema.text(text));
  const idNote = (id) =>
    fragmentSchema.nodes.note.create({ anchor: 'block', id });

  // Two comments with IDENTICAL words are two comments. Text pairing gave the
  // first note both; order pairing gave each the other's once the notes moved.
  const words = { 'cb-a': 'same', 'cb-b': 'same' };
  const forward = fragmentSchema.nodes.doc.create(null, [
    para('one'),
    idNote('cb-a'),
    para('two'),
    idNote('cb-b'),
  ]);
  const got = widgetsOf(forward, words);
  check(
    'two notes whose comments have IDENTICAL text each get their own words',
    got.length === 2 &&
      got[0].id === 'cb-a' &&
      got[1].id === 'cb-b' &&
      got.every((w) => w.text === 'same' && w.cls === 'gly-note-words') &&
      got[0].key !== got[1].key,
    got,
  );
  const distinct = { 'cb-a': 'about the first', 'cb-b': 'about the second' };
  const swapped = fragmentSchema.nodes.doc.create(null, [
    idNote('cb-b'),
    para('one'),
    idNote('cb-a'),
  ]);
  const moved = widgetsOf(swapped, distinct);
  check(
    'swap the document order and the words follow the id, not the order',
    moved.length === 2 &&
      moved[0].id === 'cb-b' &&
      moved[0].text === 'about the second' &&
      moved[1].id === 'cb-a' &&
      moved[1].text === 'about the first',
    moved,
  );

  // A hand-typed {>>words<<} has no id. It shows its own text through the
  // content hole, with no code of its own here.
  const legacy = fragmentSchema.nodes.doc.create(null, [
    para('one'),
    fragmentSchema.nodes.note.create(
      { anchor: 'block' },
      fragmentSchema.text('typed by hand'),
    ),
  ]);
  check(
    'a note with no id gets no widget, and its own text still renders',
    widgetsOf(legacy, { '': 'never shown' }).length === 0 &&
      legacy.child(1).textContent === 'typed by hand',
  );

  // A mark whose ID matches no saved comment is a dropped case: nothing is
  // painted, and nothing else is done about it.
  check(
    'a note whose id has no words gets no widget',
    widgetsOf(forward, { 'cb-a': 'only this one' }).length === 1 &&
      widgetsOf(forward, {}).length === 0,
  );

  const multi = widgetsOf(
    fragmentSchema.nodes.doc.create(null, [idNote('cb-a')]),
    {
      'cb-a': 'line one\n\nline three',
    },
  );
  check(
    'the widget text keeps \\n',
    multi.length === 1 && multi[0].text === 'line one\n\nline three',
    multi,
  );

  // The key carries the words: two widgets with one key are one widget to
  // ProseMirror, and an edited comment would keep its old words on screen.
  const before = widgetsOf(forward, { 'cb-a': 'first wording' });
  const after = widgetsOf(forward, { 'cb-a': 'second wording' });
  check(
    "an edited comment's widget is a new widget",
    before.length === 1 && after.length === 1 && before[0].key !== after[0].key,
    [before, after],
  );
}

// --- where a thread belongs ---
//
// Found the first time a human used the rail: a thread on a figure region sat
// at the FOOT of the rail, several screens from the figure it was about, with a
// connector arm reaching left across empty space. Two symptoms, one predicate —
// `!!thread.run` was standing in for "has a place in the document", and those
// two questions came apart the moment block and figure anchors landed, because
// a block thread has no mark BY CONSTRUCTION.
//
// The arithmetic is three-valued for exactly that reason: a mark is one way to
// have a place, a block is another, and only a thread with neither is
// ANCHORLESS — where there is nothing to be beside and nothing to light. (That
// verdict was called `foot` while the rail was a fixed column with a foot to
// put things at. It names the condition now, not a location. The arm it used to
// refuse is deleted along with the whole connector; the verdict outlived it,
// because it was never about what got drawn.)

{
  const blocks = [
    { key: 'bk-head', kind: 'heading', index: 0 },
    { key: 'bk-fig', kind: 'image', index: 4 },
  ];

  const range = threadPlacement(
    { run: 'r7', heading: 'the request path' },
    blocks,
  );
  check(
    'a range thread is placed by its mark',
    range.where === 'mark' && range.run === 'r7',
    range,
  );

  const figure = threadPlacement(
    {
      anchor: 'block',
      anchorKey: 'bk-fig',
      region: { x: 0.1, y: 0.5, w: 0.2, h: 0.2 },
    },
    blocks,
  );
  check(
    'a figure thread is placed by its BLOCK, not sent to the anchorless section',
    figure.where === 'block' && figure.index === 4,
    figure,
  );
  check(
    'and its region travels with it, so the card can meet the pin',
    figure.region && figure.region.y === 0.5,
    figure,
  );

  const section = threadPlacement(
    { anchor: 'block', anchorKey: 'bk-head' },
    blocks,
  );
  check(
    'a block thread with no region is placed at its block',
    section.where === 'block' && section.index === 0 && section.region === null,
    section,
  );

  // The two that genuinely have nowhere to go.
  const orphan = threadPlacement({ heading: 'gone' }, blocks);
  check(
    'a range thread whose mark is gone is anchorless',
    orphan.where === 'anchorless' && orphan.index === -1,
    orphan,
  );

  const unknown = threadPlacement(
    { anchor: 'block', anchorKey: 'bk-vanished' },
    blocks,
  );
  check(
    'a block key this refresh does not know is anchorless rather than guessed at',
    unknown.where === 'anchorless' && unknown.index === -1,
    unknown,
  );

  // A document thread has no anchorKey at all and must never be positioned:
  // the overall card owns it, and "the whole file" has no coordinates.
  const whole = threadPlacement({ anchor: 'document' }, blocks);
  check(
    'a document thread has no place in the column',
    whole.where === 'anchorless',
    whole,
  );

  // A run WINS over a block key. Both can be present — a range thread carries
  // no anchorKey today, but the payload is additive and the mark is the more
  // precise of the two answers.
  const both = threadPlacement(
    { run: 'r9', anchor: 'block', anchorKey: 'bk-fig' },
    blocks,
  );
  check(
    'a thread with both a mark and a block is placed by the mark',
    both.where === 'mark' && both.run === 'r9',
    both,
  );
}

// THE FOLD IS GONE, AND ITS CHECKS WITH IT.
//
// What stood here drove `foldCards`, `FOLD_CAP` and `moreLabel`: which cards
// were past the top or bottom edge of the VIEWPORT, which of them the cap kept
// and in what order, that the count and the undrawn list were the same number,
// and that a card being typed into was never cut. Every one of those was a
// claim about a mechanism for managing overflow in a viewport-locked column,
// and the column is now as tall as the document. There is no fold, no cap and
// no remainder line to be right about; work below the window is reached by
// scrolling to it, and the bar's census is the one readout of how much there
// is. `just layers` §10 is where the replacement properties are asserted —
// beside its mark, not clipped, reachable, and pointing — because they are
// facts about a real browser's layout rather than arithmetic.

// --- a half-typed reply survives the rebuild ---
//
// THE ONE THAT DESTROYS WORK. paintRail empties the rail and rebuilds every
// card on every pending refresh — the 1.5s poll and every mutation — so a reply
// half-written into a thread card was destroyed with the card, mid-sentence. In
// live mode the agent answering the reviewer is itself a mutation, which closes
// the loop: replying to someone wipes what they are writing back.
//
// The codebase already knew this hazard. The overall thread was given its own
// slot precisely so a poll could not "take the caret out of a sentence someone
// was in the middle of writing"; the thread cards never got the same
// protection. This is that protection, as arithmetic over field keys — value,
// caret and focus, because restoring the text alone still takes the caret out
// of the sentence.

{
  const saved = [
    {
      key: 'reply:k1',
      value: 'that is not what the spec',
      start: 9,
      end: 9,
      focused: true,
    },
    { key: 'reply:k2', value: '', start: 0, end: 0, focused: false },
    {
      key: 'reply:k3',
      value: 'half a thought',
      start: 14,
      end: 14,
      focused: false,
    },
  ];
  const fields = [
    { key: 'reply:k1', value: '' },
    { key: 'reply:k2', value: '' },
    { key: 'reply:k3', value: '' },
  ];
  const put = carryDrafts(saved, fields);
  const k1 = put.find((p) => p.key === 'reply:k1');
  check(
    'a half-typed reply comes back with its text, its caret and its focus',
    !!k1 &&
      k1.value === 'that is not what the spec' &&
      k1.start === 9 &&
      k1.focus === true,
    k1,
  );
  check(
    'an empty unfocused field is not a draft and is left alone',
    !put.some((p) => p.key === 'reply:k2'),
    put,
  );
  // Unfocused but written-in: the reviewer typed, clicked into the document,
  // and a poll landed. The words are still theirs.
  const k3 = put.find((p) => p.key === 'reply:k3');
  check(
    'text typed and then left is carried too, without stealing focus',
    !!k3 && k3.value === 'half a thought' && k3.focus === false,
    k3,
  );

  // A thread resolved while its reply was being written. Nothing here creates
  // a card, so the draft is dropped rather than resurrected against one.
  check(
    'a draft for a card that no longer exists is dropped, not resurrected',
    carryDrafts(saved, [{ key: 'reply:k9', value: '' }]).length === 0,
  );

  // A selection, not just a caret — a reviewer who swept a phrase to replace it
  // must not have the range collapsed under them by a poll.
  const swept = carryDrafts(
    [{ key: 'r', value: 'the whole phrase', start: 4, end: 9, focused: true }],
    [{ key: 'r', value: '' }],
  );
  check(
    'a selected range in a reply survives as a range',
    swept[0].start === 4 && swept[0].end === 9,
    swept,
  );

  // Bounds are clamped against the value actually restored: a caret past the
  // end of the text is a throw from setSelectionRange in some browsers and a
  // silently wrong caret in the rest.
  const past = carryDrafts(
    [{ key: 'r', value: 'short', start: 99, end: 120, focused: true }],
    [{ key: 'r', value: '' }],
  );
  check(
    'a caret past the end of the text is clamped to it',
    past[0].start === 5 && past[0].end === 5,
    past,
  );

  // A rebuilt field that somehow arrives with text of its own knows something
  // this side does not, so its text wins — but the caret and the focus are
  // still the reviewer's.
  const fresh = carryDrafts(
    [{ key: 'r', value: 'mine', start: 2, end: 2, focused: true }],
    [{ key: 'r', value: 'the server put this here' }],
  );
  check(
    'a field rebuilt with content of its own keeps it, and still takes focus back',
    fresh[0].value === 'the server put this here' && fresh[0].focus === true,
    fresh,
  );

  // Two fields under one key is a caller bug; the first is taken so the
  // behaviour is at least deterministic rather than DOM-order dependent.
  const dupe = carryDrafts(
    [
      { key: 'r', value: 'first', start: 5, end: 5, focused: true },
      { key: 'r', value: 'second', start: 6, end: 6, focused: false },
    ],
    [{ key: 'r', value: '' }],
  );
  check(
    'a duplicated key resolves to the first draft, deterministically',
    dupe.length === 1 && dupe[0].value === 'first',
    dupe,
  );
}

// --- did the reveal's scroll actually happen? ---
//
// A card click asks for `block: 'center'` and then has to decide whether the
// browser honoured it, because `behavior: 'smooth'` is a request a browser may
// silently drop. The first version of this decision asked "is any part of the
// mark within the viewport" two frames in, and was wrong in BOTH directions —
// each of the two reproduced failures is a check below:
//
//   a mark parked a few pixels inside the bottom edge satisfied bare
//   intersection, so no fallback fired and flash() rang unreadable text;
//
//   an animation that had moved the mark one pixel ALSO satisfied bare
//   intersection, so the fallback fired and cut every working smooth scroll to
//   a hard jump.
//
// Both are arithmetic on a measured sample, so both are checked here with no
// browser. `scrollArrival` answers "has it arrived" (a property of the final
// position); `scrollStep` answers "is it moving" (a property of the frames in
// between). Keeping them separate is the fix.

// A viewport 1141 tall over a long document, matching the reproduced case.
const view = (over) => ({ viewportH: 1141, scrollMax: 5000, ...over });

{
  // REPRO 1. markTop 1136 of a 1141 viewport: five pixels of the mark are
  // showing at the very bottom edge. Bare intersection called this arrived.
  // Centring did not: the mark's midpoint is ~575px below the viewport's, so
  // the scroll plainly has not happened.
  const sliver = view({ markTop: 1136, markBottom: 1155, scrollY: 400 });
  const a = scrollArrival(sliver);
  check(
    'a sliver of the mark at the bottom edge is NOT arrived',
    a.arrived === false,
    a,
  );
  // And the watch must therefore reach the fallback rather than sit forever.
  let stalls = 0;
  let action = 'wait';
  let prev = null;
  for (let i = 0; i < SCROLL_STALL_FRAMES + 2 && action === 'wait'; i += 1) {
    const step = scrollStep(sliver, prev, stalls);
    action = step.action;
    stalls = step.stalls;
    prev = sliver; // a dead smooth scroll never moves scrollY
  }
  check(
    'a stalled scroll with the mark at the edge falls back',
    action === 'fallback' && stalls === SCROLL_STALL_FRAMES,
    { action, stalls },
  );
}

{
  // A mark just inside the TOP edge is the same bug mirrored — bare
  // intersection was symmetric, so this direction has to be checked too.
  const a = scrollArrival(view({ markTop: -14, markBottom: 5, scrollY: 400 }));
  check(
    'a sliver of the mark at the top edge is NOT arrived',
    a.arrived === false,
    a,
  );
}

{
  // REPRO 2. The measured trace was scrollY [0, 0, 1, 1, 1620, …] with the mark
  // ~1620px away: an animation that has moved ONE PIXEL. It has not arrived,
  // but it is moving, and movement must win — otherwise the fallback cuts it.
  const far = (scrollY) =>
    view({ markTop: 1620 - scrollY, markBottom: 1639 - scrollY, scrollY });
  const first = scrollStep(far(0), null, 0);
  check(
    'the first frame of a scroll waits rather than falling back',
    first.action === 'wait',
    first,
  );
  const moved = scrollStep(far(1), far(0), first.stalls);
  check(
    'an in-flight smooth scroll that has moved 1px is NOT cut',
    moved.action === 'wait' && moved.stalls === 0,
    moved,
  );
}

{
  // The trace in full, frame by frame, against the real stall threshold: a
  // working smooth scroll must reach its destination without EVER falling back.
  // This is the check that fails if SCROLL_STALL_FRAMES is tightened back to
  // two, because the trace does not move at all for its first two frames.
  const trace = [0, 0, 1, 1, 1620, 2400, 2900, 3050, 3060];
  const target = 3060;
  // 561/580 rather than 570/589: centring is on the mark's MIDPOINT, so a 19px
  // mark is centred in a 1141 viewport when its top is at 561, not 570. Getting
  // this wrong is how the fixture first claimed a completed scroll had not
  // arrived — the same midpoint-vs-edge confusion the code itself had.
  const sampleAt = (y) =>
    view({
      markTop: target + 561 - y,
      markBottom: target + 580 - y,
      scrollY: y,
      scrollMax: 5000,
    });
  let stalls = 0;
  let prev = null;
  const actions = [];
  for (const y of trace) {
    const s = sampleAt(y);
    const step = scrollStep(s, prev, stalls);
    actions.push(step.action);
    if (step.action !== 'wait') {
      break;
    }
    stalls = step.stalls;
    prev = s;
  }
  check(
    'a working smooth scroll is never cut short and ends arrived',
    !actions.includes('fallback') && actions[actions.length - 1] === 'arrived',
    { actions, target },
  );
}

{
  // CLAMPING. A mark near the end of the document cannot be centred — the
  // scroll runs out first — so "as centred as this document allows" has to
  // count as arrived. An inset-margin test would call this a failure forever
  // and re-scroll to the offset it is already at, cutting the animation. Here
  // scrollY is already at scrollMax and the mark sits well below centre.
  const clamped = view({
    markTop: 900,
    markBottom: 919,
    scrollY: 5000,
    scrollMax: 5000,
  });
  const a = scrollArrival(clamped);
  check(
    'a mark the document cannot centre is arrived at the clamp',
    a.arrived === true && a.target === 5000,
    a,
  );
  check(
    'a clamped arrival never falls back',
    scrollStep(clamped, null, SCROLL_STALL_FRAMES - 1).action === 'arrived',
    scrollStep(clamped, null, SCROLL_STALL_FRAMES - 1),
  );
}

{
  // A mark already centred needs no scroll and must not collect stall frames on
  // its way to a pointless jump — which is why arrival is tested before
  // movement in scrollStep, not after.
  const centred = view({ markTop: 561, markBottom: 580, scrollY: 400 });
  check(
    'a mark already centred is arrived',
    scrollArrival(centred).arrived === true,
    scrollArrival(centred),
  );
  check(
    'an already-centred mark never falls back',
    scrollStep(centred, centred, SCROLL_STALL_FRAMES - 1).action === 'arrived',
  );
}

{
  // The tolerance is a real window, and it is not so wide that the sliver cases
  // above slip through it: SCROLL_ARRIVED_PX away is arrived, one more is not.
  const at = (off) =>
    view({ markTop: 561 + off, markBottom: 580 + off, scrollY: 400 });
  check(
    'the arrival tolerance admits exactly SCROLL_ARRIVED_PX',
    scrollArrival(at(SCROLL_ARRIVED_PX)).arrived === true &&
      scrollArrival(at(SCROLL_ARRIVED_PX + 2)).arrived === false,
    [
      scrollArrival(at(SCROLL_ARRIVED_PX)),
      scrollArrival(at(SCROLL_ARRIVED_PX + 2)),
    ],
  );
}

// --- heading levels arrive as strings ---

{
  const node = schema.node('heading', { level: '2' }, [schema.text('title')]);
  const [tag] = schema.nodes.heading.spec.toDOM(node);
  check(
    'a string heading level renders h2, not h1 or h-undefined',
    tag === 'h2',
    tag,
  );
}

check(
  'coerceLevel tolerates every shape a level arrives in',
  coerceLevel('3') === 3 &&
    coerceLevel(3) === 3 &&
    coerceLevel('') === 1 &&
    coerceLevel(null) === 1 &&
    coerceLevel('9') === 1,
  [
    coerceLevel('3'),
    coerceLevel(3),
    coerceLevel(''),
    coerceLevel(null),
    coerceLevel('9'),
  ],
);

// --- the marks the Go bridge writes ---

for (const name of ['ins', 'del', 'highlight']) {
  const mark = schema.marks[name];
  check(
    `the ${name} mark carries author and at`,
    !!mark && 'author' in mark.spec.attrs && 'at' in mark.spec.attrs,
  );
}

// bindsContentField reports whether the bundle configures the collaboration
// extension's `field` to "content" — following one level of minifier variable
// hoisting, since esbuild turns `field: FIELD` into `field:HE` with
// `HE="content"` declared elsewhere.
// A MINIFIED IDENTIFIER IS NOT A WORD, AND `$` IS NOT A CHARACTER IN A REGEX.
// This followed the minifier's variable with `new RegExp('\\b' + value + …)`,
// and both halves of that are wrong for the names esbuild actually mints: `$`
// is not a word character, so `\b` before `$N` asks for a boundary that a
// preceding comma cannot supply, and an unescaped `$` in the pattern is an
// end-of-input ANCHOR rather than the character it was copied from. Two silent
// bugs in one expression, and they only fire when the minifier happens to hand
// this particular literal a `$`-prefixed name — which it did, on a build whose
// bundle was perfectly correct, reporting the collaboration field unbound. The
// check is right and its plumbing was not.
function bindsContentField(src) {
  for (const [, value] of src.matchAll(
    /field\s*:\s*("content"|'content'|[A-Za-z_$][\w$]*)/g,
  )) {
    if (value === '"content"' || value === "'content'") {
      return true;
    }
    const name = value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    // `(?:^|[^\w$])` is the boundary `\b` cannot express here: the character
    // before an identifier must not itself be part of one.
    if (new RegExp(`(?:^|[^\\w$])${name}\\s*=\\s*["']content["']`).test(src)) {
      return true;
    }
  }
  return false;
}

// --- the built bundle ---

{
  const path = new URL('../internal/serve/assets/editor.js', import.meta.url);
  let src = '';
  try {
    src = readFileSync(path, 'utf8');
  } catch {
    check('the built bundle exists', false, String(path));
  }
  if (src) {
    // Compiled, not run: an IIFE bundle's first act in node would be to look
    // for a DOM that is not there. Compiling proves it parses, which is the
    // failure a bad build actually produces.
    let parsed = true;
    try {
      new Script(src, { filename: 'editor.js' });
    } catch (err) {
      parsed = false;
      check('the built bundle parses', false, err.message);
    }
    if (parsed) {
      check('the built bundle parses', true);
    }
    // The Go side writes the document under doc.GetXmlFragment("content")
    // (ydoc.FragmentName). The Collaboration extension's own default is
    // "default", so a bundle that lost the configuration binds a DIFFERENT,
    // empty fragment: a blank page against a document that is perfectly fine
    // on disk, with nothing in the console to say why.
    //
    // A bare src.includes('"content"') proves nothing — a minified bundle is
    // full of textContent, contentMatch, fieldset. This resolves the actual
    // `field:` call site instead, following the minifier's variable if it
    // hoisted the literal. Mutation-checked: changing entry.ts's FIELD makes
    // it fail.
    check(
      'the built bundle binds the collaboration field to "content"',
      bindsContentField(src),
    );
    check(
      'the built bundle installs window.galleyEdit',
      src.includes('galleyEdit'),
    );
    // The committed bundle is what the server actually serves, and the fence
    // guard is worth nothing if the bundle predates it. All three sentences the
    // reviewer can be shown have to be in there — the two reasons and the hint.
    // (Minification keeps string literals intact, so this is exact.)
    //
    // The strings are chosen to be unique to the fence guard, which is not
    // fussiness: `read-only here` was the obvious phrase to match on and it is
    // ALSO in the panel's "threads are read-only here" note, so it is present
    // in bundles built long before any of this — checked against the pre-fix
    // bundle, where it matches and these three do not.
    //
    // This used to match `no comment inside a code fence`, the comment guard's
    // own wording. That wording is GONE on purpose: the comment guard, the
    // strike guard and the transaction filter now all render literalHit's reason,
    // so there is exactly one sentence per refusal and no second copy to check.
    // FENCE_JOIN stands in for the comment guard's half — it reaches the bundle
    // only through the shared constants.
    check(
      'the built bundle carries the read-only-fence refusals',
      src.includes('galley never rewrites one') &&
        src.includes('that would merge a code fence') &&
        src.includes('edit fenced code in your own editor'),
    );

    // The table's own three, for the same reason: the binary embeds this
    // bundle, so a passing unit test proves nothing about what a running
    // server serves. A refusal sentence that exists only in the source is a
    // reviewer typing into a cell and being told nothing.
    check(
      'the built bundle carries the read-only-table refusals',
      src.includes('a table is read-only here') &&
        src.includes('that would merge a table with the block beside it') &&
        src.includes('edit the table in your own editor'),
    );

    check(
      'the built bundle carries the table nodes',
      src.includes('tableRow') &&
        src.includes('tableHeader') &&
        src.includes('tableCell'),
    );

    // A mark's identity has to survive minification to reach the DOM, and it
    // is the one thing here with no visible symptom when it does not: cards
    // silently fall back to matching by text, which is exactly the bug the
    // attribute was added to remove. The binary embeds this bundle, so a
    // passing unit test proves nothing about what a running server serves.
    check(
      'the built bundle carries the run attribute',
      src.includes('data-run'),
    );

    // The note node must survive minification, and its absence has no visible
    // symptom until a reviewer's block note is silently erased from their file
    // on the next projection. The binary embeds this bundle, so a passing unit
    // test proves nothing about what a running server serves.
    check(
      'the built bundle carries the note node',
      src.includes('data-galley-note'),
    );

    // THE COPY THE REVIEWER READS, IN THE BUNDLE THE BINARY EMBEDS. Every
    // string below replaces one that was measured lying to a reviewer, and
    // every one of them lives in a source file a passing unit test proves
    // nothing about: the binary serves this file. Both directions are pinned —
    // the sentence that must be there, and the sentence that must not — because
    // a check that only looks for the new words goes green on a bundle carrying
    // both.
    // THE SWEEP'S TWO COPY CHECKS ARE INVERTED, NOT DROPPED. They pinned
    // `threads waiting on the agent survive` present and `open questions
    // survive` absent — the tooltip rewritten after one click was measured
    // resolving three never-answered agent threads — and `✓ accept N · settle
    // N` present. `✓ all` is deleted with the /_galley/sweep POST behind it, so
    // BOTH sentences and the label must now be absent; the old one is still
    // named, because the lie is what a returning sweep would most likely bring
    // back with it.
    check(
      'the built bundle makes no bulk promise, honest or otherwise',
      !src.includes('threads waiting on the agent survive') &&
        !src.includes('open questions survive'),
    );
    check(
      'and it carries no bulk label to promise it with',
      !src.includes(' · settle '),
    );
    check(
      'the verdict button keeps Revise inline and opens no native prompt',
      src.includes('Revise') &&
        !src.includes('Instruction for the whole document:'),
    );
    check(
      'the bubble stops diagnosing what it cannot know',
      src.includes('no decision available here') &&
        !src.includes('the server has not seen this one yet') &&
        !src.includes('that suggestion has moved'),
    );
    // NOT `!src.includes('claude suggested')`, which the batch sentence would
    // have walked straight past: the coalesced form read `claude suggested {n}
    // edits`, assembled around a template hole, so the two-word phrase is not
    // in the source at all. The whole word is what is banned, and the bundle
    // carries third-party code with no such string in it either — measured
    // zero, so this is a real zero rather than a hopeful one.
    check(
      'and no surface in the bundle names a vendor',
      !/claude/i.test(src) && src.includes(' suggested '),
    );
    // INVERTED, AND THE INVERSION IS THE RULING. It read *the whole-doc
    // composer lives in the instruction rail*, and it was green over both of
    // the defects the first real review reported: the composer scrolled out of
    // reach, and opening it slid every anchored card 39.29px off its mark. The
    // rail holds live work only — *here is what needs you, beside the text it
    // is about* — and a `+ add` button needs nothing and is beside nothing.
    // Capture is chrome now: `+ Instruction` in the bar, a right-click in the
    // document, and `.gly-capture` for the typing. The old handle's sentence is
    // asserted ABSENT, because a rail that grew one back is the defect
    // returning and a check that only looked for the new string would not see
    // it. `gly-overall-rail` stays: the instructions already FILED are still
    // the rail's, which is the half that never had to move.
    check(
      'capture is chrome, and the rail carries no handle of its own',
      src.includes('gly-capture') &&
        src.includes('+ Instruction') &&
        src.includes('gly-overall-rail') &&
        src.includes('add an instruction on the whole doc') &&
        !src.includes('+ instruction on the whole document'),
    );

    // The trail: the ghost and the highlight. Written red-first against the
    // pre-trail bundle — a binary embedding that bundle records nothing however
    // green the source tree is.
    //
    // AND NO ENDPOINT, WHICH IS THE HALF THAT IS INVERTED RATHER THAN DROPPED.
    // This used to assert `/_galley/trail` was in the bundle. There is no such
    // route on the EditServer: the browser POSTed the whole trail on a 600ms
    // debounce after every keystroke AND again on every 1.5s poll, advanced its
    // baseline only on `res.ok`, and therefore retried a 404 for as long as the
    // tab stayed open. The trail is an outgoing message and not a stored
    // history, so there was never anything for the endpoint to be — see the
    // note in entry.ts where `syncTrail` was. Asserted ABSENT so a save loop
    // cannot come back unnoticed; run red against the tracked bundle, which
    // carries the string.
    check(
      'the built bundle carries the trail — ghost and highlight',
      src.includes('gly-trail-ghost') && src.includes('gly-trail-ins'),
    );
    check(
      'and it POSTs the trail nowhere — an outgoing message has no save',
      !src.includes('/_galley/trail'),
    );
    // AND IT CARRIES NO LOG OF IT ON ANY SURFACE. This check used to assert
    // the opposite — `gly-rail-changed`, `gly-changed-head`, `△` and
    // `gly-sheet-changed` all present — and it is INVERTED rather than deleted,
    // because a deleted check cannot see the region being quietly put back. The
    // trail is an outgoing message to the agent, not a history: nobody browses
    // it, so it has no surface, and the count rides the button that sends it.
    // Run red against the tracked bundle it reports every one of these strings
    // present.
    check(
      'and no surface logs it — the trail is a message, not a history',
      !src.includes('gly-rail-changed') &&
        !src.includes('gly-changed-head') &&
        !src.includes('gly-sheet-changed') &&
        !src.includes('gly-changed-list') &&
        !src.includes('gly-change-adrift'),
    );
    // Instructions and History are peer document views now. The instruction
    // count is on its own named control beside History, not hidden inside the
    // verdict button's label.
    check(
      'the built bundle carries the instruction count beside History',
      src.includes('Instructions · ') &&
        src.includes('gly-census-count') &&
        src.includes('gly-versions-open'),
    );

    // THE SEAL. A binary embedding a pre-seal bundle serves a page that keeps
    // taking keystrokes into a review that has ended — which is the exact
    // failure the seal exists to remove, and which no Go test can see. Written
    // red-first against the pre-seal bundle: every one of these is absent
    // there.
    // AND THE TWO VERBS ARE ASSERTED ABSENT, which is this file's own answer to
    // a check whose subject left the product: invert it, never delete it, so a
    // control being quietly put back is something a run can see. `makeSeal`
    // built `.gly-seal-actions` with `↺ Reopen` and `✓ Done` in it, set it
    // `hidden` and never appended it — both were unpressable — and they are
    // deleted now. Reopen is vestigial in every layer (no route, no CLI, and
    // both rounds_surface_test.go files assert it); `/_galley/stop` is real and
    // has no browser caller left, which is recorded at makeSeal rather than
    // smoothed over.
    check(
      'the built bundle carries no terminal verbs — the seal is a readout',
      !src.includes('gly-seal-reopen') &&
        !src.includes('gly-seal-done') &&
        !src.includes('/_galley/reopen') &&
        !src.includes('/_galley/stop'),
    );
    check(
      'the built bundle carries the three verdict readouts',
      src.includes('review closed') &&
        src.includes('entrusted') &&
        src.includes('markup thrown away'),
    );
    // AND THE HANDOFF'S OWN CLAUSE. A binary embedding a bundle from before
    // the handoff serves a terminal bar that states a number and then never
    // moves it, over a stretch whose whole point is that work is landing —
    // and no Go test can see a string that never reaches the page.
    check(
      'the built bundle carries the handoff countdown',
      src.includes('outstanding') &&
        src.includes('all applied') &&
        src.includes('still landing'),
    );
    // The agent's reopen reaches the reviewer through the status readout and
    // nowhere else — they are looking at a sealed page and at nothing else.
    check(
      'the built bundle carries the agent-reopen readout',
      src.includes('reopened by the agent'),
    );
    // A sealed review takes NO INPUT, and this check had to be MADE
    // discriminating: `setEditable` alone is in every TipTap bundle ever
    // built, and `gly-thread-reply` has been in this one since the rail
    // landed, so the first draft of this line reported `ok` against the
    // pre-seal bundle — the exact shape CLAUDE.md says to distrust. What is
    // new is the class the terminal bar hides its neighbours with, and the
    // verb SELECTOR the seal disables through, whose tail (`.gly-thread-reply,
    // .gly-overall-input, .gly-census button`) exists nowhere else. It used to
    // be distinguished from `dimCard`'s own list, which stopped four entries
    // earlier; that list is deleted with the fold and the seal owns those four
    // flags alone now, which is one fewer writer rather than a new one.
    //
    // THE TAIL IT WATCHES MOVED, BECAUSE THE LIST DID. `SEALED_VERBS` named four
    // controls the product no longer builds — `.gly-card-accept`,
    // `.gly-card-reject`, `.gly-thread-resolve` and `.gly-thread-reply`, the
    // accept/reject pair and the two conversation verbs — and a list naming
    // controls nobody renders reads as coverage and is not. The distinguishing
    // property is unchanged: this tail exists nowhere else in the bundle, so it
    // still tells a sealing build from one that merely has TipTap in it.
    //
    // AND THE TAIL MOVED AGAIN, FOR THE ONE EXEMPTION THE LIST HAS.
    // `.gly-census button` became `.gly-census button:not(.gly-versions-open)`
    // when History stopped dying with the seal: the census strip is a group of
    // VIEW DOORS, a door is not a verb, and a sealed review is exactly when
    // somebody wants to read what happened. The `:not(...)` is asserted here
    // rather than merely tolerated, because a build that dropped it would
    // disable the door while leaving it on screen — shown, reachable and dead,
    // which this codebase rates as worse than absent.
    check(
      'the built bundle carries the sealed bar and its dead verbs',
      src.includes('gly-seal-off') &&
        src.includes(
          '.gly-overall-input, .gly-census button:not(.gly-versions-open)',
        ),
    );
    // AND THE BOX A COMMENT IS TYPED INTO, which is asserted separately because
    // it was added for a reason neither list's tail records: `submitOnEnter`
    // gave every composer an Enter that files, so the composer's textarea can
    // now post without its send button and had to stop being live on a sealed
    // page. It is in BOTH lists — sealed like the rest, and released on the
    // unseal edge like its sibling send button, which has no painter either. A
    // bundle with it in only one of them is a tab whose comment box dies at the
    // first verdict and never comes back.
    check(
      'the built bundle seals the composer textarea, and releases it again',
      src.includes('.gly-composer button, .gly-composer-text') &&
        src.includes('.gly-comment-button, .gly-composer-text'),
    );
    // AND THE HALF THE SEAL OWNS OUTRIGHT — the controls with no painter the
    // unseal EDGE runs, which is the sharpened form of the invariant (a
    // gesture-driven writer is not an edge). A bundle carrying SEALED_VERBS
    // without this list is a bundle that kills those controls for the life of
    // the tab, and the head of the list is what tells the two apart.
    //
    // `.gly-census-count` IS IN IT NOW, and it joined the moment it stopped
    // being a `<span>`. It is the door to the review's whole list — the sheet,
    // where the settled conversations live — it is built ONCE in `makeCensus`
    // and never rebuilt, and `paintCensus` re-derives the `disabled` flag of
    // `✓ all` and nothing else. Left out, the first seal would take the way
    // back to `↺ reopen` away permanently and Reopen would hand back every verb
    // on the page except the one that reaches the record. Run red against the
    // tracked bundle it reports the old tail with no count in it.
    //
    // `.gly-census-overall` HAS LEFT IT, and the head of the list is
    // `.gly-census-count` now. The whole-document handle was a bar control
    // opening a floating panel; then it was the first card of the instruction
    // rail, reached by `.gly-overall-toggle`; it is a bar control again
    // (`.gly-capture-open`, one of `.gly-census button` and covered by that
    // entry), and the box it opens floats over the rail as `.gly-capture`. The
    // INPUT is the entry in this list that ever mattered, and it kept its class
    // through both moves for exactly that reason. The count is still here for
    // the reason it joined — it is built ONCE in `makeCensus` and never
    // rebuilt, so a seal that did not release it would kill the door to the
    // review's own list for the life of the tab.
    check(
      'the built bundle carries the verbs the seal owns in both directions',
      src.includes(
        '.gly-census-count, .gly-overall-input, .gly-composer-send, ',
      ) && src.includes('.gly-comment-button, .gly-composer-text'),
    );
    // And the Strike button is GONE — with its class, its label and its
    // cross-block sentence. Deletion is the keyboard's; a bundle still
    // carrying any of the three is a bundle built before the cut.
    check(
      "the Strike button is out of the bundle — deletion is the keyboard's",
      !src.includes('gly-strike-button') &&
        !src.includes('⌫ strike') &&
        !src.includes('strike works inside one block'),
    );

    // The rail replaced the panel. Both halves are asserted, because a bundle
    // that grew the rail while keeping the panel would render two overlapping
    // lists of the same suggestions and look, at a glance, like it worked.
    // --- arrivals: what landed while the reviewer was reading ---
    {
      const prev = [{ run: 'a', kind: 'insert', text: 'one' }];
      const next = [
        { run: 'a', kind: 'insert', text: 'one' },
        { run: 'b', kind: 'delete', text: 'two' },
      ];
      const d = diffPending(prev, next);
      check(
        'an arrival is detected by run',
        d.arrived.length === 1 && d.arrived[0].run === 'b',
      );
      check(
        'an unchanged suggestion is not an arrival',
        d.arrived.every((a) => a.run !== 'a'),
      );

      // The reviewer's OWN typing must not announce itself.
      //
      // The plan spelled this as `mine.arrived.filter(a => a.author !== 'court')
      // .length === 0`, which passes whether or not the filtering happens: with no
      // filtering, arrived is court's own row and the predicate excludes it
      // anyway. The claim is that arrived is EMPTY, so that is what is asserted.
      const mine = diffPending(prev, [
        ...prev,
        { run: 'c', kind: 'insert', text: 'x', author: 'court' },
      ]);
      check(
        'the reviewer is not told about their own edit',
        mine.arrived.length === 0,
      );

      const gone = diffPending(next, prev);
      check(
        'a decided suggestion is reported as resolved',
        gone.resolved.length === 1 && gone.resolved[0] === 'b',
      );
      check(
        'nothing arrives when nothing changed',
        diffPending(next, next).arrived.length === 0,
      );
    }

    {
      check(
        'one arrival names its section',
        arrivalMessage([
          { kind: 'insert', section: 'Shape', author: 'agent' },
        ]) === 'agent suggested an insert in §Shape, below your viewport',
      );
      check(
        'several arrivals coalesce',
        arrivalMessage([
          { kind: 'insert', author: 'agent' },
          { kind: 'delete', author: 'agent' },
          { kind: 'insert', author: 'agent' },
        ]) ===
          'agent suggested 3 edits while you read — show me steps through them',
      );
      // A document with no headings has no section, and "in §, below your
      // viewport" is not a sentence.
      check(
        'a sectionless arrival drops the clause rather than printing an empty one',
        arrivalMessage([{ kind: 'delete', author: 'agent' }]) ===
          'agent suggested a delete, below your viewport',
      );
      check('nothing arrived says nothing', arrivalMessage([]) === '');
      // A substitution is one change and has its own word: falling through to the
      // "an edit" default would be vaguer than the payload already is, and
      // borrowing "a delete" would name half of what arrived.
      check(
        'a replacement is announced as one',
        arrivalMessage([{ kind: 'replace', author: 'agent' }]) ===
          'agent suggested a replacement, below your viewport',
      );

      // THE STRIP NAMED A VENDOR AND NOTHING ELSE ON THE SCREEN DID. `claude` was
      // hard-coded in both sentences while every card head, the standing sentence
      // and the CLI's own default all said `agent` — so a proposal filed by
      // `galley suggest --author dana` was announced as having come from claude,
      // three inches from a card reading `REPLACE · DANA · JUST NOW`. Shown red
      // against the tracked bundle: the two checks above read `claude suggested …`.
      check(
        'the arrival names ITS OWN author, whoever filed it',
        arrivalMessage([{ kind: 'replace', author: 'dana' }]) ===
          'dana suggested a replacement, below your viewport',
      );
      check(
        'and the string `claude` appears in no arrival sentence at all',
        ![
          arrivalMessage([{ kind: 'insert', author: 'agent' }]),
          arrivalMessage([{ kind: 'insert' }]),
          arrivalMessage([
            { kind: 'insert', author: 'dana' },
            { kind: 'delete', author: 'dana' },
          ]),
        ].some((s) => s.includes('claude')),
      );
      // An unnamed proposer is the ordinary case for a mark parsed out of a file:
      // CriticMarkup has nowhere to record an author, so the strip has no name to
      // print and prints the general one rather than an empty space.
      check(
        'an arrival with no author falls back to the general name',
        arrivalMessage([{ kind: 'insert' }]) ===
          `${ARRIVAL_AGENT} suggested an insert, below your viewport`,
      );
      // TWO PARTIES HAVE NO SINGLE NAME. Picking the first would attribute the
      // other's work to it, which is the same defect as `claude` with a subtler
      // cause.
      check(
        'a batch from two parties is announced under neither of them',
        arrivalMessage([
          { kind: 'insert', author: 'dana' },
          { kind: 'insert', author: 'agent' },
        ]) ===
          `${ARRIVAL_AGENT} suggested 2 edits while you read — show me steps through them`,
      );
      check(
        "a batch from ONE party keeps that party's name",
        arrivalAuthor([{ author: 'dana' }, { author: 'dana' }]) === 'dana',
      );
    }

    {
      // THE STRIP IS FOR WHAT THE REVIEWER CANNOT SEE. An arrival whose mark is on
      // screen announces itself by sliding into the rail and flashing; a strip on
      // top of that is noise.
      const viewport = { height: 800, railVisible: true };
      check(
        'an on-screen arrival needs no strip',
        arrivalNeedsStrip({ top: 400 }, viewport) === false,
      );
      check(
        'an arrival below the fold needs the strip',
        arrivalNeedsStrip({ top: 1200 }, viewport) === true,
      );
      check(
        'an arrival scrolled off the top needs the strip',
        arrivalNeedsStrip({ top: -50 }, viewport) === true,
      );
      check(
        'an arrival the document does not show yet needs the strip',
        arrivalNeedsStrip({ top: null }, viewport) === true,
      );
      // Collapsed rail: there is no card to slide in, so every arrival is
      // invisible without the strip.
      check(
        'a collapsed rail makes every arrival need the strip',
        arrivalNeedsStrip({ top: 400 }, { height: 800, railVisible: false }) ===
          true,
      );
    }

    {
      // The queue is what `show me` steps through, and it is NOT the rail's card
      // list: a card can be decided or scrolled past without the reviewer having
      // seen what arrived.
      const q1 = queueArrivals([], [{ run: 'a' }, { run: 'b' }]);
      const q2 = queueArrivals(q1, [{ run: 'b' }, { run: 'c' }]);
      check(
        'a repeated arrival is queued once',
        q2.map((a) => a.run).join(',') === 'a,b,c',
      );

      const step = nextArrival(q2, [{ run: 'b' }, { run: 'c' }]);
      check(
        'show me skips an arrival that is no longer pending',
        step.arrival.run === 'b' &&
          step.queue.map((a) => a.run).join(',') === 'c',
      );
      check(
        'a queue with nothing live left steps nowhere',
        nextArrival(q2, []).arrival === null,
      );
    }

    {
      // HOLD IS A QUEUE OVER WHAT IS DISPLAYED, NOT A GATE ON THE DOCUMENT. The
      // suggestions are in the CRDT either way; hold decides which of them the
      // rail draws a card for, and nothing else.
      check(
        'the button says what pressing it does, both ways',
        holdLabel(false, 0) === '⏸ hold' &&
          holdLabel(true, 3) === '▶ release · 3',
      );
      check(
        'holding nothing still says so honestly',
        holdLabel(true, 0) === '▶ release · 0',
      );

      // The census takes the SERVER's payload and never the rail's hold, which
      // is what lets the count keep telling the truth while the rail holds its
      // tongue.
      check(
        'the census is not filtered by hold',
        censusCounts({ comments: [{ key: 'a' }, { key: 'b' }] }).threads === 2,
      );
    }

    {
      // R10. The counter runs until the revision LANDS, not until the request
      // returns — the request returns in milliseconds and the revision does not.
      //
      // These read the label PRODUCTION PAINTS: makeRevise builds the button's
      // three-node faces and paintRevise writes the digits and picks the face,
      // over a fake DOM just rich enough for the two of them. A pure string
      // helper standing in for the button could agree with itself forever.
      check(
        'a settled button says Revise and discloses its menu',
        paintedRevise({ pendingCount: 0 }) === 'Revise ▾',
        paintedRevise({ pendingCount: 0 }),
      );
      check(
        'the composed primary carries the pending count on the verb',
        paintedRevise({ pendingCount: 4 }) === 'Revise · 4 ▾',
        paintedRevise({ pendingCount: 4 }),
      );
      check(
        'the counter reads whole seconds, floored',
        paintedRevise({ waitedMs: 0 }) === 'revising · 0s' &&
          paintedRevise({ waitedMs: 1500 }) === 'revising · 1s' &&
          paintedRevise({ waitedMs: 6200 }) === 'revising · 6s',
        paintedRevise({ waitedMs: 1500 }),
      );
      // A clock that skewed backwards must not print a negative age.
      check(
        'a backwards clock reads 0s, never -1s',
        paintedRevise({ waitedMs: -500 }) === 'revising · 0s',
        paintedRevise({ waitedMs: -500 }),
      );
    }

    {
      // The verdict label is the document's state: markup pending → the press
      // DISCLOSES, nothing pending → the press approves. The button never asks the
      // reviewer to remember which; it reads the same census the strip does.
      check(
        'an instruction round offers Revise',
        verdictLabel({ instructions: [{ text: 'tighten this' }] }) ===
          'Revise ▾',
      );
      check(
        'an empty instruction round offers Approve',
        verdictLabel({ instructions: [] }) === 'Approve',
      );
      check(
        'a clean document offers Approve',
        verdictLabel({ comments: [] }) === 'Approve',
      );
      // An open thread holds up Approve.
      check(
        'an open legacy note still offers Revise through its thread',
        verdictLabel({ comments: [{}] }) === 'Revise ▾',
      );
      // The reviewer's own hand edits are outgoing markup: a pending change
      // offers Revise, or the button reads Approve over unsent edits and the
      // press seals the review without recording them as a round.
      check(
        'a reviewer hand edit offers Revise in the rounds-only path',
        verdictLabel({ instructions: [], changes: [{ kind: 'removed' }] }) ===
          'Revise ▾',
      );
      check(
        'a reviewer hand edit offers Revise in the census path',
        verdictLabel({
          comments: [],
          changes: [{ kind: 'removed' }],
        }) === 'Revise ▾',
      );
      check(
        'no hand edits leaves the clean document on Approve',
        verdictLabel({ instructions: [], changes: [] }) === 'Approve',
      );

      // THE BUTTON AND THE MENU'S FIRST ITEM SAID THE SAME WORD. `Revise` opened a
      // menu reading `→ Revise` / `✓ Accept all & approve`, so revising cost two
      // presses of one word and approving asked a reviewer to press a button
      // labelled Revise — which is the half nobody does. The label names the ACT OF
      // ENDING now, and the menu names the endings.
      check(
        'the rounds-only label names the direct action',
        REVISE_IDLE === 'Revise ▾',
        REVISE_IDLE,
      );
      // The direct path is untouched: a clean document still approves on one press,
      // with no menu and no disclosure glyph on the label.
      check(
        'the direct approve face carries no disclosure glyph',
        !APPROVE_IDLE.includes('▾') && APPROVE_IDLE === 'Approve',
        APPROVE_IDLE,
      );
      // Both menu items are still there and still verbatim — the fix is the label
      // above them, not a rewrite of the exits.
      check(
        'the menu still names both endings the page can reach',
        MENU_REVISE === 'Revise' && MENU_TRUST === 'Revise & Approve',
      );
    }

    {
      // THE TERMINAL READOUT NAMES THE VERDICT AND ITS MOMENT. Three endings and
      // three sentences: the trust exit is NOT "approved" with a number appended,
      // because "the review is finished" and "work is still travelling and you are
      // watching it land" are two different things to have been told.
      const at = new Date(2026, 7, 15, 11, 42).getTime();
      check(
        'a pure approve reads as closed',
        sealLine('approved', at, 0) ===
          'Approved 11:42 · review closed · restart galley edit to reopen',
      );
      check(
        'the trust exit counts what it handed over',
        sealLine(VERDICT_ENTRUSTED, at, 2, 2) ===
          'Approved 11:42 · 2 notes entrusted · 2 outstanding',
      );
      check(
        'one entrusted note is not "1 notes"',
        sealLine(VERDICT_ENTRUSTED, at, 1, 1) ===
          'Approved 11:42 · 1 note entrusted · 1 outstanding',
      );
      // WATCHING IS A VERB. The reviewer walked away from a page whose only
      // remaining job is to show the handoff land, so the line has to MOVE as the
      // agent finishes each note — and has to say so when it is done, because a
      // reviewer coming back needs to read that it finished rather than merely
      // fail to read that it did not. Written red-first: before the handoff
      // existed the count was `entrusted` alone and this line was identical five
      // seconds after the press and an hour later.
      check(
        'the count falls as the agent lands the work',
        sealLine(VERDICT_ENTRUSTED, at, 2, 1) ===
          'Approved 11:42 · 2 notes entrusted · 1 outstanding',
      );
      check(
        'and the completed handoff says so, keeping what was handed over',
        sealLine(VERDICT_ENTRUSTED, at, 2, 0) ===
          'Approved 11:42 · 2 notes entrusted · all applied',
      );
      // AND IT COUNTS DOWN, WHICH IS THE PROPERTY THE SENTENCE BESIDE IT CLAIMS.
      // `outstanding` was the server's pending+open, so ONE `galley suggest` —
      // the first verb the agent is told to run — rendered `2 notes entrusted · 3
      // outstanding` on the page the reviewer walked away from. Reproduced against
      // the shipped binary; the server reports the two populations apart now, and
      // the bound is what is checked here rather than the arithmetic that produced
      // it.
      check(
        'the agent working on an entrusted note does not add to what was entrusted',
        sealLine(VERDICT_ENTRUSTED, at, 2, 2, 1) ===
          'Approved 11:42 · 2 notes entrusted · 2 outstanding',
      );
      // AND "all applied" IS NOT SAID OVER MARKUP NOBODY DECIDED. With every note
      // resolved and the agent's own proposal still pending the handoff is still
      // open — the server refuses `answered` there — so the readout must not be
      // the one that means finished.
      check(
        'a proposal the agent has not landed gets its own clause, not "all applied"',
        sealLine(VERDICT_ENTRUSTED, at, 2, 0, 1) ===
          'Approved 11:42 · 2 notes entrusted · 1 still landing',
      );
      check(
        'a discard says what happened to the markup',
        sealLine(VERDICT_DISCARDED, at, 0) ===
          'Discarded 11:42 · markup thrown away',
      );
      // A seal with no timestamp is a seal read before the server answered; the
      // sentence still has to be a sentence rather than "Approved  · review
      // closed" with a hole in it.
      check(
        'a verdict with no instant still reads',
        sealLine('approved', 0, 0) ===
          'Approved · review closed · restart galley edit to reopen' &&
          clockTime(0) === '',
      );
      // The agent's reopen carries its reason INTO the readout, because the
      // reviewer is looking at a sealed page and at nothing else.
      check(
        'an agent reopen names the agent and its reason',
        reopenLine('agent', 'the second example was wrong') ===
          'reopened by the agent · the second example was wrong',
      );
      // THE REVIEWER'S OWN REOPEN HAS ONE SENTENCE, NOT TWO. pressReopen used to
      // write "reopened — the review is live again" on the press and then read the
      // seal back at once, which put reopenLine('reviewer','') — the bare word
      // "reopened" — straight over it. Two strings for one event, and the shorter
      // and worse one won because it arrived second. reopenLine is the single owner
      // now. Both presses are deleted with the terminal bar's two buttons; the
      // sentence survives them because `applySeal` still writes it on the edge a
      // wake could bring, and it is the one sentence for the event either way.
      check(
        "a reviewer's own reopen says the review is live again",
        reopenLine('reviewer', '') === 'reopened — the review is live again',
      );
      check(
        "and a reviewer's reopen still carries a reason when there is one",
        reopenLine('reviewer', 'one more pass') ===
          'reopened — the review is live again · one more pass',
      );
      // THE TWO LABELS ARE DELETED AND SO IS THE CHECK ON THEM. `SEAL_REOPEN` and
      // `SEAL_DONE` sized a grid cell in a `.gly-seal-actions` group `makeSeal`
      // built, set `hidden` and never appended — so neither button was ever in the
      // document and neither label was ever measured against anything. The bundle
      // check below asserts their ABSENCE instead, which is a claim that can go red.
    }

    {
      // A ✓ THAT CANNOT WORK IS NOT OFFERED, and this is the block that says so.
      //
      // The measurement behind `decidable` was made against a card that no longer
      // exists: `arrivals.ts`'s `batchRows` paired a revision's runs against the
      // whole pending list with NO kind filter, and the revision receipt built its
      // ✓ from every row it got, so a revision carrying a comment put the comment's
      // run into POST /_galley/accept — 200 before the server learned to refuse it,
      // the reviewer's highlight lifted, the conversation left anchored to nothing,
      // and an `approved` row filed against the agent in the population every
      // proposal number is computed from.
      //
      // THE CARD IS DELETED AND THE PREDICATE IS NOT, which is the reason this
      // block was rewritten rather than removed with it. `decidable` was never the
      // card's rule; it is suggest.Kind.Decidable's answer, carried onto the wire
      // per entry, and three surfaces still read it — the rail's card loop, the
      // sheet's, and stepOrder. What is asserted here is the predicate itself, and
      // the two checks that prove it is not a `kind !== 'comment'` in disguise fail
      // against one, in opposite directions.
      check(
        'an entry the SERVER calls decidable is offered, whatever its kind reads',
        decidable({ kind: 'comment', decidable: true }) === true,
      );
      check(
        'an entry with no answer on it is not offered — absent means no',
        decidable({ kind: 'insert' }) === false && decidable(null) === false,
      );
    }

    // THE MODE IS A SWITCH WITH ONE LABEL, not a badge with two.
    //
    // It used to read `on ask` / `● live`, and this block used to assert both
    // strings — "the toggle names the state it is in, both ways". Two nouns,
    // neither of which says "this is a control", and "on ask" a riddle to anyone
    // who has not read the spec. The mode is the one control whose whole job is to
    // say which world you are in, and it was not doing it.
    //
    // Nothing is lost by naming only the on state: the mode's entire effect is
    // whether `⏸ hold` is on screen, and Revise is visible in both states, so the
    // thing you would do instead of going live is in front of you either way.
    {
      check(
        'the switch has ONE label, whatever the mode',
        modeLabel() === 'live',
      );
      check(
        'and the mode decides its POSITION, not its words',
        modeOn(MODE_LIVE) === true && modeOn(MODE_ASK) === false,
      );
      // The old badge's most important property survives the redesign: an
      // unrecognised mode must never read as live. A reviewer who believes the
      // agent is not listening while it is has been told the opposite of the
      // truth about the one thing this control exists to report.
      check(
        'an unknown mode is never shown as live',
        modeOn('') === false && modeOn('sideways') === false,
      );
      check(
        'the switch asks for the other mode',
        nextMode(MODE_ASK) === MODE_LIVE && nextMode(MODE_LIVE) === MODE_ASK,
      );
      // A switch's label is its state, so the verb has to live in the hover text —
      // and both facts have to be there: what is happening now, and what a click
      // does about it.
      check(
        'the hover text says what is happening now, both ways',
        modeTitle(MODE_LIVE).includes('woken whenever the document settles') &&
          modeTitle(MODE_ASK).includes('hears nothing until you press Revise'),
      );
      check(
        'and what a click will do about it, both ways',
        modeTitle(MODE_LIVE).includes('click to stop') &&
          modeTitle(MODE_ASK).includes('click to go live'),
      );
      check(
        'no state of the switch says "on ask" any more',
        !modeTitle(MODE_ASK).includes('on ask') &&
          !modeTitle(MODE_LIVE).includes('on ask') &&
          !modeLabel().includes('ask'),
      );
    }

    check(
      'the built bundle carries the instruction rail',
      src.includes('gly-rail') && src.includes('gly-card'),
    );
    // AND NOT ONE OF THE FIVE MECHANISMS THAT MANAGED ITS OVERFLOW. Dimming was
    // struck from this design once, came back bounded by a cap, and is now gone
    // with the fixed column that made it necessary — along with the clusters it
    // clamped cards into, the cap, and the remainder line that counted what the
    // cap kept out. The ABSENCE is asserted, in the built artifact, because a
    // bundle carrying any of these is a bundle built before the regroup and
    // every one of them would render.
    check(
      'the built bundle has no fold left in it — no clusters, no dimming, no cap',
      !src.includes('gly-offscreen') &&
        !src.includes('gly-rail-above') &&
        !src.includes('gly-rail-below') &&
        !src.includes('gly-folded-out') &&
        !src.includes('gly-fold-more') &&
        !src.includes('and 1 more '),
    );
    // AND NOTHING REPLACED THEM. The rail was four sections — anchored cards,
    // anchorless, settled, changed — and it is the map and a notice. Each of
    // the three left for its own reason (see makeRail), and this asserts the
    // ABSENCE, because a bundle is the one place a section can come back
    // without a source reader noticing. Run red against the tracked bundle it
    // reports `gly-rail-anchorless` and `gly-rail-settled` both present.
    check(
      'the built bundle has one section after the map, and it is a notice',
      src.includes('gly-rail-notice') &&
        !src.includes('gly-rail-anchorless') &&
        !src.includes('gly-rail-settled'),
    );
    // THE EMPTY RAIL TEACHES, AND THE SETTLED NOTICE IT REPLACED IS ASSERTED
    // ABSENT. An Approve-faced primary already says the document is settled, so
    // the notice was a second, quieter voice for it; what a cold-open page
    // never said is how to ask for a change at all. Run red against the tracked
    // bundle it reports the old sentence present and the new one missing.
    check(
      'the built bundle carries the teaching empty state, and not the settled notice',
      src.includes('Select any words in the document to ask for a change.') &&
        src.includes('go to the agent as one round.') &&
        src.includes('gly-rail-teach') &&
        !src.includes('nothing pending — the document is settled'),
    );
    // §2.2: the whole-document instruction is a card in the rail and nothing
    // else — that part is unchanged. WHERE IT IS ASKED FOR IS NOT: the handle
    // was the rail's own first card and is the bar's control now, for the
    // reason spelled where `+ instruction on the whole document` is asserted
    // absent above. The verb carries no count, because the count is on the
    // primary and the cards are directly underneath it.
    check(
      'the built bundle offers the whole-document instruction from the BAR',
      src.includes('+ Instruction') &&
        !src.includes('Whole-document instruction · '),
    );
    // §2.3: an instruction is editable. Both the verb and the endpoint it
    // reaches, because a button that posts nothing is the shape a check that
    // reads a class name cannot see.
    check(
      'the built bundle carries the edit verb and the one op it posts',
      (src.includes('gly-thread-edit') && src.includes('"edit"')) ||
        src.includes("op:'edit'") ||
        src.includes('op: "edit"'),
    );
    // An instruction whose highlight disappeared remains a full card in the
    // rail; it is not replaced by copy pointing at the removed sheet.
    check(
      'the built bundle keeps unplaced instructions in the rail',
      src.includes('its words were removed') &&
        src.includes('gly-rail-unplaced'),
    );

    check(
      'the built bundle carries the sheet',
      src.includes('gly-sheet') && src.includes('gly-bottombar'),
    );
    // The breakpoint is a number in two files and they must be the same number.
    // A rem-based media query and a px-based JS check agree at the default font
    // size and disagree at every other one — and the way that disagreement shows
    // up on screen is the rail and the sheet at the same time.
    check(
      'the built bundle carries the keyboard stepper',
      src.includes('keydown') && src.includes('gly-stepped'),
    );

    // `instruction · <about> · <age>` is COMPOSED now — the head joins its
    // clauses, so the literal `instruction · ` a minifier could once be relied
    // on to emit is not in the bundle any more. The claim is the same one; it
    // is asserted through the pieces the bundle really carries.
    check(
      'the built bundle carries the established card and instruction language',
      src.includes('gly-thread') &&
        src.includes('gly-instruction-actions') &&
        /\["instruction",|\['instruction',/.test(src),
    );
    // THE SETTLED REGION IS GONE FROM THE SHEET TOO. Nothing resolves an
    // instruction any more — a sent one leaves with its round and a retracted
    // one is deleted — so there is no settled thread for a region to hold, and
    // its absence is asserted in the bundle, the one place it could come back
    // without a source reader noticing.
    check(
      'the built bundle carries no settled region on any surface',
      !src.includes('gly-sheet-settled') &&
        !src.includes('gly-settled-head') &&
        !src.includes('gly-resolved') &&
        !src.includes('gly-declined'),
    );
    // A block note's words are painted from the instruction data, by ID, as
    // a DECORATION; the text- and order-paired settled marker is gone.
    check(
      'the built bundle paints a block note\u2019s words as a DECORATION, by ID',
      src.includes('gly-note-words') &&
        src.includes('glyNoteWords') &&
        !src.includes('gly-note-settled') &&
        !src.includes('glySettledNotes'),
    );
    // Delete: the second verb, its endpoint, its two-step arming, and the
    // reopen that makes "keeps the history" true.
    check(
      'the built bundle carries delete, armed rather than instant',
      src.includes('/_galley/instruction/delete') &&
        src.includes('gly-thread-delete') &&
        src.includes('delete?'),
    );
    check(
      'an instruction card does not render a reopen affordance',
      src.includes('gly-instruction-actions') &&
        !src.includes('gly-thread-reopen'),
    );
    // A confirm() would block the whole page — and this page has a websocket
    // under it. The arming IS the confirmation.
    check(
      'nothing in the bundle opens a modal dialog to confirm a delete',
      !/\bwindow\.confirm\(/.test(src) && !/[^.\w]confirm\(/.test(src),
    );
    // R9: a comment was rendered as two objects — a suggestion card offering
    // accept/reject AND a thread entry below it. The panel note that stood in
    // for the missing verbs goes with it.
    check(
      'threads are no longer read-only chrome',
      !src.includes('resolve them from the CLI'),
    );

    // Region picking, and the card it produces. §11 fixes "on figure region"
    // verbatim, and it reaches the bundle only through threadLabel. The way in
    // is the figure grip's Mark a region; the ⊕ button on the picture is gone.
    check(
      'the built bundle carries region picking',
      src.includes('gly-region-draft') &&
        src.includes('on figure region') &&
        src.includes('Mark a region') &&
        !src.includes('comment on a region'),
    );

    // §6/R4. Both halves have to be in the bundle the binary embeds: the figure
    // views themselves, and the URL the mermaid renderer is fetched from. A
    // bundle that lost the fetch renders every diagram as its own source and
    // looks exactly like a document whose diagrams all failed to parse.
    check(
      'the built bundle carries the figure views',
      src.includes('gly-figure') && src.includes('/_galley/mermaid.js'),
    );

    // The block grip: one button beside every block that takes a whole-block
    // instruction. A bundle without it is a document where a section, a fence
    // or a table cannot be instructed at all, and looks exactly like one where
    // they can. The two hover grips it replaced are gone from the bundle, so
    // no gutter offers two affordances for one gesture.
    check(
      'the built bundle carries the block grip, and not the hover grips it replaced',
      src.includes('gly-block-grip') &&
        src.includes('Add an instruction on ') &&
        !src.includes('instruct on this whole code block'),
    );

    // R7. The placeholder is §11's verbatim string, and a bundle that lost it
    // is a document with nowhere to comment on the whole file, looking exactly
    // like a document that has one. It is no longer the ONLY affordance — the
    // bar's door and the right-click menu are two more, which is R7 answered
    // three ways instead of one — so the box it labels is asserted with them.
    check(
      'the built bundle carries the overall thread and the box it is written in',
      src.includes('add an instruction on the whole doc') &&
        src.includes('gly-overall') &&
        src.includes('gly-capture'),
    );
    // AND THE TWO DOORS TO IT, WHICH IS WHERE THE VERB WENT. The bar's control
    // and the right-click menu are one decision seen twice: capture is chrome,
    // and the rail holds only the work. A bundle carrying neither is a document
    // whose whole-file instruction cannot be started at all.
    // `contextmenu` ALONE IS NOT A READ. The bundle carries third-party code
    // that names the event too, so the word is present whether galley claims
    // the gesture or not — measured: re-pointing the listener at `mousedown`
    // left a check on that string green. What only this menu puts in the
    // bundle is its own rows and their classes, so those are what is read.
    check(
      'the built bundle offers capture from the bar AND from a right-click',
      src.includes('+ Instruction') &&
        src.includes('gly-menu-item') &&
        src.includes('gly-menu-detail') &&
        src.includes('Instruction on the whole document') &&
        src.includes('Instruction on this passage'),
    );

    // THE CENSUS STRIP SURVIVES ITS SWEEP. `✓ all` is deleted — it POSTed
    // /_galley/sweep, a 404, from a button `makeCensus` had already stopped
    // appending — so what the strip carries is the count, and the count is the
    // door to the sheet.
    check(
      'the built bundle carries the census strip',
      src.includes('gly-census') && src.includes('gly-census-count'),
    );
    // AND NONE OF THE TWELVE WORKFLOW ENDPOINTS, asserted as one claim off the
    // one list. internal/serve/rounds_surface_test.go asserts every one of these
    // answers 404; this is the same list read from the other end, so a browser
    // that starts posting to one of them again fails here rather than in a
    // reviewer's network tab. The labels go with the paths: the census renders
    // whatever the bundle ships, and a stray '✗ all' is a stray button waiting
    // to be appended.
    check(
      'the built bundle posts to none of the twelve retired endpoints',
      [
        'suggest',
        'accept',
        'reject',
        'accept-all',
        'reject-all',
        'sweep',
        'decline',
        'reply',
        'resolve',
        'delete',
        'reopen',
        'discard',
      ].every((verb) => !src.includes(`/_galley/${verb}`)),
    );
    check(
      'and it carries none of their labels either',
      !src.includes('✗ all') &&
        !src.includes('✓ all') &&
        !src.includes('✓ accept') &&
        !src.includes('✗ reject'),
    );

    // Realtime is opt-in, and the toggle is the only way a reviewer opts in.
    // Both labels are asserted because they are handoff §4 verbatim, and the
    // endpoint because a toggle that never reaches the server is a switch
    // wired to nothing.
    // Without this, every server-side mutation throws the reviewer's caret to
    // the end of the document and scrolls the window after it — measured, and
    // the reason the mitigation exists. A bundle that lost it looks fine until
    // somebody is typing when a suggestion lands.
    check(
      'the built bundle keeps the reviewer’s place across a rebuild',
      src.includes('galleyKeepPlace') && src.includes('scrollTo'),
    );

    // The arrival strip and its two verbatim strings. A bundle that grew the
    // diff but lost the strip would announce nothing, and the symptom is
    // silence — indistinguishable from an agent that never suggested anything.
    check(
      'the built bundle carries the arrival strip',
      src.includes('gly-strip') &&
        src.includes('show me') &&
        src.includes('fades · the count keeps it'),
    );
    check(
      'the built bundle carries both arrival sentences',
      src.includes('below your viewport') &&
        src.includes('edits while you read — show me steps through them'),
    );
    // The count is the durable record, so its pulse is what outlives the strip.
    // THE CARD'S "new" BADGE IS NOT, and this check used to read it here.
    // `gly-new` and `gly-new-badge` were `suggestionCard`'s — the amber border
    // and the badge an arriving PROPOSAL wore — and that card is deleted,
    // because the rounds-only wire carries no proposals for one to be built
    // from. Asserted absent so the pair cannot come back on a card that has no
    // arrivals to announce.
    check(
      'the built bundle carries the census pulse, and no proposal-card badge',
      src.includes('gly-pulse') && !src.includes('gly-new'),
    );

    // The wiring MOVED out of the shell's inline script and into the bundle,
    // because only the bundle polls and only a poller can count until the
    // revision lands. A bundle without it leaves a Revise button nothing wires.
    check(
      'the built bundle owns the Revise button and its counter',
      src.includes('gly-revise') &&
        src.includes('revising · ') &&
        src.includes('/_galley/revise'),
    );

    // The verdict button's disclosure: over a document with work pending the
    // press opens two exits instead of posting, and both are real buttons
    // whose labels never change. Pinned VERBATIM — the strings are the
    // interface, and a bundle that lost either one is a verdict button that
    // can no longer say one of the three things it exists to say. The trust
    // exit's body is pinned too: `trust:true` riding on an approve is what
    // makes it the trusted handoff rather than a second plain approve.
    check(
      'the built bundle carries the verdict menu and both its exits',
      src.includes('gly-verdict-menu') && src.includes('Revise & Approve'),
    );
    check(
      'and Revise & Approve waits for the successful answer',
      src.includes('approveOnAnswer'),
    );

    // AND IT CARRIES NO SECOND CARD LANGUAGE. This used to assert the
    // OPPOSITE — "the built bundle carries the batch card and a batch decide"
    // — and the card it was pinning is deleted: a revision's results are its
    // own cards, in the band, in the one language every other card is drawn
    // in. The check is inverted rather than dropped, because the bundle is the
    // artifact the binary embeds and a receipt reintroduced anywhere in web/
    // would reach a reviewer through it. `revision · ` goes with it: that
    // string was `batchLabel`'s head and nothing else ever printed it.
    check(
      'the built bundle carries NO revision receipt — one card language',
      !src.includes('gly-batch') && !src.includes('revision · '),
    );

    check(
      'the built bundle carries hold and release',
      src.includes('gly-hold') &&
        src.includes('⏸ hold') &&
        src.includes('▶ release · '),
    );

    // This used to assert both of the old badge's labels — `on ask` and
    // `● live`. There is one label now, and a POSITION carried by
    // aria-checked, so what the bundle has to contain is the switch's parts
    // and its endpoint. The absence of "on ask" is asserted too: the copy that
    // failed a real reviewer must not survive anywhere in the shipped bundle.
    check(
      'the built bundle carries the mode switch, its label and its endpoint',
      src.includes('gly-mode') &&
        src.includes('gly-switch') &&
        src.includes('"switch"') &&
        src.includes('aria-checked') &&
        src.includes('/_galley/mode'),
    );
    check(
      'and the copy that failed a reviewer is gone from it',
      !src.includes('on ask'),
    );
    // The instruction sheet says what it contains. The old proposal-era copy
    // contradicted rounds, where both parties' edits land in the document.
    check(
      'the built bundle names the instruction sheet without proposal-era copy',
      src.includes('instructions in this round') &&
        !src.includes('your edits apply — the agent proposes'),
    );
    check(
      'and the old untracked admission is gone from it',
      !src.includes('formatting and structure apply untracked'),
    );

    check(
      'the stylesheet breaks at the same pixel the bundle does',
      readFileSync(
        new URL('../internal/serve/assets/editor.css', import.meta.url),
        'utf8',
      ).includes('992px') && src.includes('992'),
    );
  }
}

{
  // The chip is a STYLESHEET fact, and the stylesheet is a separate esbuild
  // output that the binary embeds separately. A bundle check would not have
  // caught a CSS build that silently kept the old file.
  const cssPath = new URL(
    '../internal/serve/assets/editor.css',
    import.meta.url,
  );
  let css = '';
  try {
    css = readFileSync(cssPath, 'utf8');
  } catch {
    check('the built stylesheet exists', false, String(cssPath));
  }
  if (css) {
    check(
      'every fence carries a passive read-only chip',
      css.includes('read-only') && css.includes('gly-fence-pulse'),
    );
    // THE DIMMED CARD IS GONE FROM THE STYLESHEET TOO. Three rules hid a
    // dimmed card's four verbs, clipped it to its head line and took the
    // over-cap ones out of the layout; all three are deleted with the fold, and
    // the absence is read off the BUILT css because that is a separate esbuild
    // output the binary embeds separately — a CSS build that silently kept the
    // old file would leave every one of those rules live.
    check(
      'the built stylesheet has no dimmed, clipped or folded-out card left',
      !css.includes('gly-offscreen') &&
        !css.includes('gly-folded-out') &&
        !css.includes('gly-fold-more') &&
        !css.includes('gly-drafting'),
    );
    // The destructive verb is at the FAR END of the row — that margin is what
    // stops a slip aimed at ✓ resolve from landing on it — and it is never the
    // louder of the two.
    // Out of resolve's way, but NOT flush to the right edge: `margin-left:auto`
    // was measured pushing it under the rail on the full-width overall panel,
    // where the click never landed at all.
    check(
      'the stylesheet puts delete out of resolve\u2019s way without exiling it',
      /\.gly-thread-delete\{[^}]*margin-left:2rem/.test(css) &&
        !/\.gly-thread-delete\{[^}]*margin-left:auto/.test(css),
    );
    check(
      'a block note\u2019s words keep their line breaks and wrap',
      /\.gly-note-words\{[^}]*white-space:pre-wrap/.test(css) &&
        /\.gly-note-words\{[^}]*overflow-wrap:anywhere/.test(css) &&
        !css.includes('gly-note-settled'),
    );
    // NO LINE OF ANY KIND, IN ANY SPELLING. The connector had three shapes over
    // two designs — a leg, an arm, and a shared SVG overlay with a hairline
    // curve and a dot — and this is the one check that can say all three are
    // gone from what ships, rather than from what a source file says.
    //
    // Two checks stood here and both are deleted with their subject: `the curve
    // it draws instead is a hairline that honours reduced motion` and `it ends
    // in a dot, in the colour the line is drawn in`. The first was guarding a
    // DRAW — the light is instant, so there is no motion for a reader who asked
    // not to be moved to be spared, and honouring the preference is a no-op
    // rather than a rule. The second was the curve's own end.
    check(
      'the built stylesheet draws no connector at all — no arm, no leg, no curve',
      !css.includes('gly-connector'),
    );
    // AND THE LIGHT IS THERE, in both themes, which is the claim that replaces
    // them. The token is asserted in the light AND the dark block, because a
    // wash defined once reads as a wash that works everywhere right up until
    // somebody opens the page at night — and the pixel itself is read in a real
    // browser by layers §10, which is where "it is not any mark's wash" lives.
    check(
      'the built stylesheet lights the words, and names a colour for both themes',
      /\.gly-lit\{[^}]*background:var\(--gly-lit-bg/.test(css) &&
        (css.match(/--gly-lit-bg:/g) || []).length === 2,
    );
    // THE RAIL IS NOT FIXED, AND IT STILL STARTS AT THE BAR'S MEASURED FOOT.
    // Read off the built stylesheet for the reason above, and asserted as two
    // halves that are easy to think are one. ABSOLUTE is what makes it scroll
    // with the prose. `top` is a SEPARATE question, and this check used to
    // require the bar's height be ABSENT from the rule — on the reasoning that
    // a sticky bar is in flow, so a rail at the page's top begins below it for
    // free. That is true of a flow sibling and false of an out-of-flow box:
    // absolute positioning against the initial containing block put `top: 0` at
    // document y 0, under the opaque bar, and everything paintAnchors did not
    // place by hand was drawn there (see §10a of layers.mjs). The offset is
    // back and it is MEASURED — a constant is what the fold made wrong, in the
    // other direction.
    //
    // AND THE OFFSET NOW CARRIES A SECOND TERM, WHICH IS ONE NUMBER WITH
    // HISTORY'S. The rail begins at the bar's measured foot PLUS the reserved
    // sub-bar row (`--gly-rail-top`), because History's rail hangs off the same
    // token — before that the two columns jumped 64px apart on a mode switch,
    // measured 52.2 against 116.2 at 1440. The bar's measured height is still
    // required to be in there: that is the half a constant got wrong when the
    // bar folded, and it is a different claim from the row being reserved.
    check(
      'the built stylesheet has the rail scrolling with the document, from the bar’s measured foot',
      /\.gly-rail\{[^}]*position:absolute/.test(css) &&
        /\.gly-rail\{[^}]*top:calc\(var\(--gly-bar-h[^}]*var\(--gly-rail-top/.test(
          css,
        ) &&
        !/\.gly-rail\{[^}]*position:fixed/.test(css),
    );
    // AND BOTH RAILS HANG OFF THE ONE TOKEN. Read off the BUILT stylesheet
    // because that is what the binary embeds: a rule that agreed in the source
    // and was overridden in the bundle is the shape this file exists to catch.
    check(
      'and History’s rail hangs off the same reserved row, so a mode switch moves nothing',
      /--gly-rail-top:\s*calc\(var\(--gly-sub-h\)/.test(css) &&
        /--gly-sub-h:\s*40px/.test(css) &&
        /\.gly-versions-rail\{[^}]*top:var\(--gly-rail-top\)/.test(css),
    );
    // AND IT HAS NO DISCLOSURES LEFT TO FLOAT. `bottom: 100%` on
    // `.gly-settled-list` and `.gly-changed-list` is what grew them upward over
    // the map; both lists are gone from the rail entirely, which is the
    // stronger form of the same claim and is checked as one — a stylesheet that
    // grew either of them back would fail here before it could float.
    check(
      'and the rail has no disclosure left to open over its own map',
      !css.includes('.gly-settled-list') && !css.includes('.gly-changed-list'),
    );
    // The sheet's settled list went with the region it listed.
    check(
      "and the sheet's settled list is gone from the stylesheet with it",
      !css.includes('.gly-sheet-settled') && !css.includes('.gly-settled-head'),
    );
    // The chip must not be copyable. "select and copy still work" is the second
    // half of the refusal, and a chip that lands in the clipboard beside the
    // code makes that sentence false.
    check(
      'the chip is excluded from selection',
      css.includes('user-select:none'),
    );

    // --- the bar does not move when you click it ---
    //
    // The browser pass is what proves the pixels; these are the two facts that
    // pass rests on, and they are the two a later edit would quietly undo.
    // `display:none` on hold is what made turning live on insert a button and
    // slide the switch 92.97px out from under the cursor, so its ABSENCE is
    // asserted as well as the reserve that replaced it.
    check(
      'hold reserves its space rather than being taken out of the bar',
      css.includes('.gly-reserved{visibility:hidden}') &&
        !/\.gly-hold\[hidden\]/.test(css),
    );
    // A reserved box is not a button. `visibility:hidden` is what takes it out
    // of the tab order and out of hit testing; paintMode's `disabled` is the
    // other half, and neither is allowed to be the only one. Read here rather
    // than borrowed from the block above: the two esbuild outputs are separate
    // files and this pair of claims spans both.
    const bundle = readFileSync(
      new URL('../internal/serve/assets/editor.js', import.meta.url),
      'utf8',
    );
    check(
      'and the button is disabled as well as invisible while it is reserved',
      bundle.includes('gly-reserved') && /\.disabled=!/.test(bundle),
    );
    check(
      'the panel is a band with a column in it, not a card in the chrome',
      bundle.includes('gly-overall-inner'),
    );
    // Hold's own click swaps its label for a wider one. Two labels in one grid
    // cell is what makes the button as wide as the wider of them at all times.
    check(
      'hold’s two labels share one cell, so swapping them cannot resize it',
      css.includes('.gly-hold-label{grid-area:label') &&
        css.includes('grid-template-areas:"label"'),
    );
    // THE COUNT'S RESERVE MOVED TO THE BUTTON THAT SENDS WHAT IT COUNTS.
    // `Instructions · N` is `display: none` at every width now — the number is
    // on the primary — so its 24ch bounds nothing, and a check reading it would
    // be certifying a reserve on a box with no paint. The claim the reserve was
    // written for is unchanged and is asserted one control over: a number that
    // changes on somebody else's click may move the text and never the button.
    check(
      'the pending count sits in a reserved box on the primary itself',
      /\.gly-revise-count\{[^}]*min-width:5ch/.test(css) &&
        /\.gly-census-count\{display:none/.test(css),
    );
    // And the three nodes read as the resting label: a clean count composes
    // to exactly REVISE_IDLE, the spelling the verdict compares against.
    check(
      'the composed primary at zero is REVISE_IDLE',
      paintedRevise({ pendingCount: 0 }) === REVISE_IDLE,
      paintedRevise({ pendingCount: 0 }),
    );
    // THE FIXED GRAMMAR ALWAYS FITS, which is the whole reason it replaced a
    // sentence that ellipsized to `instructions in this …`.
    check(
      'the readout names the round and where the document is',
      roundPhrase(3, PHASE_DRAFT) === 'round 3 · draft' &&
        roundPhrase(0, PHASE_DRAFT) === 'v1 · draft' &&
        roundPhrase(2, PHASE_AGENT) === 'round 2 · with the agent',
      roundPhrase(0, PHASE_DRAFT),
    );
    // AND IT IS A BUTTON THAT LOOKS LIKE ONE. The whole-document handle that
    // once sat beside it was found only after Court failed to find it, and the diagnosis was that
    // it read as a noun in a row of verbs. A count that opens the review's
    // whole list has to be dressed as a control, so it takes the strip's own
    // button chrome and adds the hover every other control there has.
    check(
      'and it is dressed as the control it became, with a hover of its own',
      bundle.includes('show the current draft and its instructions') &&
        /\.gly-census-count:hover:not\(\[disabled\]\)\{border-color/.test(css),
    );

    // --- no proposal card, and therefore no reply box on one ---
    //
    // This asserted the opposite: that the proposal card carried its own
    // `.gly-proposal-reply` textarea, keyed `reply:${s.run}`, POSTing
    // /_galley/reply by run. The card is deleted (`this.suggestions` is empty
    // by construction on the rounds-only wire, so it was never built) and
    // /_galley/reply is a 404. Inverted rather than dropped, per this file's own
    // rule: a reply box coming back is something a run should be able to see.
    check(
      'no card carries a reply box — an instruction is not a conversation',
      !bundle.includes('gly-proposal-reply') &&
        !bundle.includes('gly-thread-reply'),
    );
    // And the declined head spans both outputs: the bundle writes the
    // `declined · <label>` head, the stylesheet paints it — one class,
    // PAINT ONLY, because a kind-rule that declared `position` once took
    // every replace card out of the band's absolute placement.
    check(
      'an instruction card keeps the established card paint',
      bundle.includes('gly-instruction-actions') && css.includes('.gly-thread'),
    );

    // --- the whole-document instruction has no panel to be set on ---
    //
    // IT IS A CARD IN THE RAIL, ONCE (spec §2.2). The three checks that used to
    // stand here asserted the floating panel's column tokens, its content cap
    // and its `position: fixed` — a whole second surface for one instruction,
    // which also rendered in the prose and in the rail at the same time. They
    // are replaced rather than deleted, because the claim they were protecting
    // (nothing about the whole document opens over the document) is stronger
    // now and is exactly what a re-added panel would break first.
    check(
      'nothing about the whole document floats over the prose any more',
      !/\.gly-overall\{position:fixed/.test(css) &&
        !css.includes('.gly-overall-inner{max-width:calc(var(--gly-col)'),
    );
    check(
      'and the rail spelling is the only spelling left',
      css.includes('.gly-overall-rail') && !/\.gly-overall\{/.test(css),
    );
    // §2.2's invariant, from the one side probe can reach with no browser: the
    // note node is STILL IN THE SCHEMA and is merely not painted. A bundle that
    // dropped it is a bundle that deletes the block out of the Yjs document and
    // writes the deletion to the author's file — see web/note.ts.
    check(
      'the document-anchored note is still a node this schema builds, and is only unpainted',
      bundle.includes("name:'note'") ||
        bundle.includes('name: "note"') ||
        (bundle.includes('parseHTML') &&
          bundle.includes('aside[data-galley-note]')),
    );
    check(
      'and it is CSS that withholds the paint, not the schema',
      /\.ProseMirror \.gly-note\[data-anchor=['"]?document['"]?\]\{display:none/.test(
        css,
      ),
    );

    // --- A CLICK MOVES NOTHING EXCEPT THE THING THAT WAS CLICKED ---
    //
    // web/motion.mjs is what proves the pixels — a real click in a real
    // browser, every rect held across it. These are the facts that pass rests
    // on, and each is one an ordinary-looking edit would quietly undo.

    // The panel FLOATS. In normal flow it inserted a quarter of a screen
    // between the bar and the prose: the document's top went 55.39 → 308.50
    // and every rail card followed its mark down. `position:fixed` off the
    // bar's measured height is the fix, and `--gly-bar-h` is that measurement.
    // The panel is deleted, so what used to be asserted here — that it floats
    // rather than pushing — has no subject. `--gly-bar-h` is still read, by the
    // rail, and is still measured rather than guessed; that is the claim the
    // check below makes and it is the half that survived.
    check(
      'the bar\u2019s height is still a published measurement, not a constant',
      bundle.includes('--gly-bar-h'),
    );
    // Measured, not assumed. The bar's height changes with the viewport, the
    // font size and which of its items the media queries have dropped, so the
    // panel's top is read from the bar itself — and re-read when it changes.
    check(
      'and it hangs off the bar’s MEASURED height, watched for change',
      bundle.includes('ResizeObserver') && /gly-bar["']\)/.test(bundle),
    );
    // AND OPENING IT FLOWS IN THE PANEL AND RE-FLOORS THE CARDS, WHICH IS THE
    // FOURTH ANSWER THIS CHECK HAS HAD.
    //
    // It first asserted *opening it no longer schedules a re-measure*, sound
    // while the panel was chrome floating off the bar: no mark moved. Then the
    // panel became the rail's first card, directly above `.gly-rail-band`, so
    // opening it moved the BAND rather than the marks — and `paintAnchors`
    // writes every card as a band-LOCAL top, so the map went 55.59px stale with
    // the old proxy still green; it was inverted to demand the re-measure. Then
    // the card became `position: absolute` and left the flow, so it moved
    // nothing and the check demanded it place itself against `chromeFrame` and
    // schedule no repaint.
    //
    // The card is back in the flow now, ON PURPOSE — a child of the
    // whole-document panel, so it reads as one of the cards rather than a
    // shadowed box floating over them, which is what Court reported. Opening it
    // moves the band, so the map has to re-floor, and the 39.29px staleness the
    // in-flow version had before is answered by the repaint rather than by
    // fleeing the flow. `openCapture` is read for the two things that make the
    // new contract true: it does NOT write a `top` (it is flowed, not placed)
    // and it DOES call `scheduleAnchors` (the repaint that re-floors the
    // anchored cards on their marks). The pixels themselves are rounds-ux.mjs's
    // before/after comparison of every card in the band.
    check(
      'opening capture flows in the panel and re-floors the cards',
      /openCapture\([^)]*\)\{[\s\S]{0,600}?scheduleAnchors/.test(bundle) &&
        !/openCapture\([^)]*\)\{[\s\S]{0,600}?style\.top=/.test(bundle),
    );

    // Revise is the second, and the only one that keeps changing after the
    // click: `Revise` → `revising · 0s` → `· 10s` → `· 100s`. Both labels are
    // laid out at once and the digits have their own reserve inside the
    // counting one.
    check(
      'Revise reserves both labels, and the counter’s digits inside one',
      css.includes('.gly-revise-label{grid-area:label') &&
        /\.gly-revise\{[^}]*grid-template-areas:"label"/.test(css) &&
        /\.gly-revise-secs\{[^}]*min-width:4ch/.test(css) &&
        bundle.includes('gly-revise-secs'),
    );
  }
}

{
  // THE BAR HAS ONE FLEXIBLE CELL, AND THE READOUTS ARE ON THE LEFT OF IT.
  //
  // This is an ORDER fact about the shell's markup, so it is read from the
  // shell rather than from the bundle. `#gly-status` sat after `.gly-spacer`,
  // so the one word it prints in reply to a click grew the right-hand group
  // and slid every control left — measured at 59px for `.gly-mode` and
  // `.gly-hold` on a single press of Revise. A readout that pushes a button
  // is the same defect as a button that pushes its neighbour.
  const shell = readFileSync(
    new URL('../internal/serve/edit.html', import.meta.url),
    'utf8',
  );
  const status = shell.indexOf('id="gly-status"');
  const spacer = shell.indexOf('class="gly-spacer"');
  const revise = shell.indexOf('id="gly-revise"');
  check(
    'the bar’s readouts sit before its one flexible cell, and its controls after',
    status > 0 &&
      spacer > 0 &&
      revise > 0 &&
      status < spacer &&
      spacer < revise,
    { status, spacer, revise },
  );
}

{
  // --- the rounds ---
  //
  // Pure logic first: the pairing is what the history is FOR, and it is the one
  // thing here a string check can reach without a browser.
  check(
    'a round card says which round it is and how long ago',
    roundHead(3, { at: new Date(Date.now() - 5 * 60 * 1000).toISOString() }) ===
      'ROUND 3 · 5M AGO',
    roundHead(3, { at: new Date(Date.now() - 5 * 60 * 1000).toISOString() }),
  );
  check(
    'a round that just landed says so in words rather than in a zero',
    ageSaid(new Date().toISOString()) === 'JUST NOW',
  );
  // AND THE FOOT CARRIES NO ARROW ANY MORE. ← is the ANSWER's, and the answer
  // is the sentence the agent wrote — the foot is a version and a count beneath
  // it. While the foot held the arrow, the only ← on the card pointed at a
  // number, and the agent's own words were rendered after a → as though the
  // reviewer had said them.
  check(
    'and its foot names the version it produced and how far it moved',
    roundFoot({ n: 4, changed: 3 }) === 'v4 · 3 changes',
    roundFoot({ n: 4, changed: 3 }),
  );
  check(
    'one change is one change, not 1 changes',
    changedSaid(1) === '1 change' &&
      changedSaid(3) === '3 changes' &&
      changedSaid(0) === 'no changes',
  );
  // EACH INSTRUCTION SEPARATELY. The ask carries its instructions as the
  // server lists them, one per ask with its own line breaks; the answer points
  // at it. A round written before asks were recorded has only the server's
  // middot-joined sentence, which is RENDERED as the one entry and never split
  // or re-joined — two joiners are two spellings of one rule.
  check(
    'askedOf returns each instruction separately, and old rounds fall back to the single string',
    JSON.stringify(
      askedOf({
        n: 4,
        instruction: 'shorten it · say why',
        instructions: ['shorten it', 'say why\n\nwith numbers'],
      }),
    ) === JSON.stringify(['shorten it', 'say why\n\nwith numbers']) &&
      JSON.stringify(
        askedOf({
          n: 4,
          instruction: 'shorten the second paragraph · say why',
        }),
      ) === JSON.stringify(['shorten the second paragraph · say why']),
  );
  check(
    'and the round that answered reaches the one it is answering',
    JSON.stringify(
      askedOf({ n: 5, answers: 4, asked: 'shorten the second paragraph' }),
    ) === JSON.stringify(['shorten the second paragraph']),
  );
  check(
    'a round with nothing asked of it says nothing',
    askedOf({ n: 1 }).length === 0,
  );
  // THE FILE AS GALLEY OPENED IT IS NOT A ROUND ANYBODY HAD, so it is not in
  // the list and it does not take an ordinal. It is the landing's dashed foot
  // card, which is a different claim from "it is hidden".
  check(
    'the starting version is not counted as a round of work',
    workRounds([
      { n: 1, reason: 'opened' },
      { n: 2, reason: 'revise' },
    ]).length === 1,
  );
  // ONE EXCHANGE IS ONE CARD. The store records the ask and the answer as two
  // versions and BOTH carry the same instruction, so a card per version drew
  // the same ask twice — once over `no changes` and once over the real count.
  const exchange = [
    { n: 1, reason: 'opened' },
    { n: 2, reason: 'revise', instruction: 'tighten the opening' },
    {
      n: 3,
      reason: 'landed',
      answers: 2,
      asked: 'tighten the opening',
      changed: 2,
    },
    { n: 4, reason: 'revise', instruction: 'name the queue' },
  ];
  check(
    'an ask and the answer that discharged it are one round card, not two',
    roundCards(exchange)
      .map((r) => r.n)
      .join(',') === '3,4',
    JSON.stringify(roundCards(exchange).map((r) => r.n)),
  );
  check(
    'and nothing is dropped — an ask nobody has answered keeps its own card',
    roundCards(exchange).some((r) => r.n === 4),
  );
  check(
    'the asking cut takes the ordinal of the round that answered it',
    ordinalOf(exchange, 3) === 1 &&
      ordinalOf(exchange, 2) === 1 &&
      ordinalOf(exchange, 4) === 2,
    JSON.stringify([
      ordinalOf(exchange, 3),
      ordinalOf(exchange, 2),
      ordinalOf(exchange, 4),
    ]),
  );
  // `CHANGE k OF K` IS COMPUTED AT RENDER, from the list the server just
  // handed back. An ordinal renumbers; nothing persists one.
  // The place arrives LOWERCASED from the server and the head is uppercased by
  // the stylesheet, which is the chrome layer's rule and not this string's.
  check(
    'a change card names its place in this reading and the place on the page',
    changeHead(2, 3, 'the budget') === 'CHANGE 2 OF 3 · the budget',
    changeHead(2, 3, 'the budget'),
  );
  check(
    'and a change with no heading above it says only which change it is',
    changeHead(1, 1, '') === 'CHANGE 1 OF 1',
  );
  check(
    'the sub-bar names the round and the two versions it sits between',
    whereSaid(3, 3, 4) === 'ROUND 3 · V3 → V4',
    whereSaid(3, 3, 4),
  );
  // IDENTICAL SIDES SAY SO. An empty diff with no sentence over it reads as a
  // surface that failed to load, which is the one thing a record must not do.
  check(
    'two identical sides say so rather than showing a blank page',
    IDENTICAL_SAID === 'identical — no changes in this round',
  );
  // --- phase 2: the agent's changes are APPLIED ---
  //
  // THE EXCEPTION READS AS ENGLISH IN THE ONE SLOT THE EYE IS ALREADY ON. A
  // round where nothing happened has no author to name, and `v5 · could-not` is
  // the wire's reason word leaking onto the surface a reviewer reads.
  check(
    'an exception says so where the count would be, in words',
    roundFoot({ n: 5, reason: COULD_NOT, changed: 0 }) ===
      'v5 · the agent could not',
    roundFoot({ n: 5, reason: COULD_NOT, changed: 0 }),
  );
  // AND THE ARRIVAL NAMES THE same visible door the reviewer can press.
  check(
    'a round arriving names what happened and where to read it',
    arrivalSaid({ n: 7 }) === 'v7 · agent revised · see History' &&
      arrivalSaid({ n: 7 }).includes(VERSIONS_LABEL),
  );
  // AND IT FITS THE CELL IT IS PRINTED IN. The readout ellipsises at its END, so
  // a sentence longer than the box loses its last clause — measured at 1440px,
  // a 62-character first draft rendered as `v3 · the agent revised the docum…`
  // and the half that said where to read it never reached anybody. Thirty
  // characters is what survived; the bound is stated as one.
  check(
    'and it is short enough that its last clause is not the one that is lost',
    arrivalSaid({ n: 7 }).length <= 34,
    arrivalSaid({ n: 7 }).length,
  );
  // THE ANSWER TO AN EXCEPTION IS A DIFFERENT INSTRUCTION, and the sentence says
  // so — "'that was bad do it again' doesn't actually work well" is the whole
  // reasoning this phase rests on, and it is even truer of a revision that never
  // happened than of one that did.
  check(
    'and an exception spends its room on the reason, which is what the next instruction is written from',
    arrivalSaid({
      n: 8,
      exception: true,
      why: 'the API is not in my branch',
    }) === 'v8 · could not: the API is not in my branch',
  );
  check(
    'nothing arrives before the page has been told anything',
    arrivalSaid(null) === '' && arrivalSaid({ n: 0 }) === '',
  );
  // THE DEFAULT VIEW IS THE DOCUMENT — decision 6. v12 is just v12, and every
  // other reading is on demand.
  check(
    'the default history reading shows the changes',
    DEFAULT_VIEW === 'inplace',
  );
  check(
    'history offers only the two useful comparisons',
    VIEWS.map((v) => v.key).join(',') === 'inplace,sbs' &&
      VIEWS.map((v) => v.label).join(',') === 'changes,side by side',
  );
  // TWO ARROWS OF ONE WEIGHT A FEW INCHES APART READ AS TWO SPELLINGS OF ONE
  // GESTURE, and they are not one gesture: `‹ all rounds` goes up a level
  // inside History, `← back to draft` leaves it. Apart by shape, in the
  // vocabulary the rest of this surface is drawn with.
  check(
    'the two ways back are told apart by their own glyphs',
    ALL_ROUNDS === '‹ all rounds' && BACK_TO_DRAFT === '← back to draft',
  );
  // ONE LIST AND THEN IT STOPS. The spec names squashing, grouping, filtering
  // and a timeline as deliberately not built, so the panel must not have grown
  // a control for any of them.

  const bundle = (() => {
    try {
      return readFileSync(
        new URL('../internal/serve/assets/editor.js', import.meta.url),
        'utf8',
      );
    } catch {
      return '';
    }
  })();
  if (bundle) {
    // Whole literals, never an interpolated path: a check that reads
    // `'/_galley/' + verb` is a check the minifier can satisfy without the
    // endpoint existing.
    check(
      'the built bundle reaches the rounds and their views',
      bundle.includes('/_galley/versions') &&
        bundle.includes('/_galley/versions/view'),
    );
    check(
      'the built bundle carries the bar’s door to the record, at one width',
      bundle.includes(VERSIONS_LABEL),
    );
    check(
      'and the visible door says what its accessible name says',
      VERSIONS_LABEL === 'History' &&
        VERSIONS_NAME === 'history' &&
        bundle.includes('aria-label'),
    );
    check(
      'and History exposes the explicit restore-as-draft mutation',
      bundle.includes('/_galley/versions/restore'),
    );
    // ONE LIST AND THEN IT STOPS. Squashing, grouping, filtering and a timeline
    // are recorded in the spec as deliberately NOT BUILT, so the shipped bundle
    // must have grown a control for none of them. Read off the BUNDLE and not
    // off versions.ts, because the source says all four words out loud in the
    // comment explaining why they are absent — a check that greps prose for the
    // names of things somebody chose not to build is a check that goes red on
    // the documentation of its own claim.
    check(
      'the history is one list — no squash, no group, no filter, no timeline',
      !/gly-versions-(filter|group|timeline|squash)/.test(bundle),
    );
    // PHASE 2's TWO ENDPOINTS reach the bundle's own poll: the arrival number
    // and the exception ride GET /_galley/revise, which the page already reads
    // every 1.5s, so an applied revision needs no new surface and the server
    // holds no per-reader state.
    check(
      'the built bundle reads the arrival off the poll it already makes',
      bundle.includes('landed') && bundle.includes('cannot'),
    );
    // THE DEFAULT VIEW IS STILL THE DOCUMENT. The flip is which view an ARRIVAL
    // lands on, and it is the only override of DEFAULT_VIEW in the bundle.
    check('an arrival lands on the Changes view', bundle.includes('inplace'));
  }

  const roundsCss = (() => {
    try {
      return readFileSync(
        new URL('../internal/serve/assets/editor.css', import.meta.url),
        'utf8',
      );
    } catch {
      return '';
    }
  })();
  if (roundsCss) {
    // IT COVERS, IT DOES NOT DISPLACE, and it hangs off the bar's MEASURED
    // height — the bar wraps, so a constant here is drawn under the fold.
    check(
      'History uses the page layout rather than a full-screen overlay',
      /\.gly-versions\{[^}]*position:relative/.test(roundsCss) &&
        /body\.gly-history-mode>main\{display:none/.test(roundsCss),
    );
    // BELOW the bottom bar, which carries the count, the step and the way back
    // at narrow widths. A record must never be what covers them.
    const z = roundsCss.match(/\.gly-versions\{[^}]*z-index:(\d+)/);
    check(
      'and it sits below the bottom bar, which is the way back',
      !!z && Number(z[1]) < 50,
      z && z[1],
    );
    // A MOVE IS NEITHER AN INSERTION NOR A DELETION, and the check is the
    // INEQUALITY — reading "it is muted" would pin one treatment and go green
    // the day another replaced it. Same shape as the trail ghost's, which was
    // certified as del-red by a check that never compared the two.
    const moved = roundsCss.match(
      /\.gly-versions-paper \.gly-moved\{([^}]*)\}/,
    );
    const del = roundsCss.match(/\.gly-del\{([^}]*)\}/);
    const ins = roundsCss.match(/\.gly-ins\{([^}]*)\}/);
    const bg = (rule) =>
      rule ? (rule[1].match(/background:([^;]*)/) || [])[1] || '' : '';
    check(
      'a moved sentence is drawn outside the ins/del vocabulary — no wash of either',
      !!moved &&
        bg(moved) !== bg(del) &&
        bg(moved) !== bg(ins) &&
        !/text-decoration:line-through/.test(moved ? moved[1] : '') &&
        /dashed/.test(moved ? moved[1] : ''),
      { moved: moved && moved[1], del: bg(del), ins: bg(ins) },
    );
    // The renderer's space, for the reason the prose's own rule states: two
    // words at one position must still read as two words.
    check(
      'a replacement in a view is spaced from what it replaced',
      /\.gly-versions-paper \.gly-del\+\.gly-ins\{[^}]*margin-left/.test(
        roundsCss,
      ),
    );
    // AN EXCEPTION IS APART BY SHAPE AS WELL AS BY WORD, which is the same
    // discipline the moved rule above is drawn with and the trail ghost was
    // corrected to. It is a round in which nothing happened, in a list where
    // every other entry is a diff.
    const cannot = roundsCss.match(
      /\.gly-versions \.gly-versions-round\.gly-versions-cannot\{([^}]*)\}/,
    );
    check(
      'a round the agent could not do is apart by shape, not only by word',
      !!cannot && /dashed/.test(cannot ? cannot[1] : ''),
      cannot && cannot[1],
    );
    // THE DOOR'S ARRIVAL STATE IS PAINT AND NOTHING ELSE. It is toggled by
    // SOMEBODY ELSE'S event, in a bar whose oldest rule is that nothing may move
    // under the cursor — so this declaration may not carry width, padding,
    // border-width, font-size or margin. `box-shadow` draws inside the border
    // box and contributes no geometry.
    const isNew = roundsCss.match(
      /\.gly-bar \.gly-versions-open\.is-new\{([^}]*)\}/,
    );
    check(
      'the arrival marks the door in paint only — no geometry on somebody else’s event',
      !!isNew &&
        !/(^|;)(width|padding|margin|font-size|border-width|border:)/.test(
          isNew ? isNew[1] : '',
        ),
      isNew && isNew[1],
    );
  }
}

// AN EDIT'S WORDS, HANDED TO THE WHOLE-DOCUMENT BOX WHEN ITS INSTRUCTION IS
// GONE. Both callers (the save's 404, and paintRail finding the card gone) can
// fire for one edit, so the words must not land twice; nothing typed has
// nothing to keep; and words already in the box stay first.
{
  check(
    'a stranded edit lands in an empty box as typed',
    strandedValue('', 'a\n\nb') === 'a\n\nb',
  );
  check(
    'a stranded edit follows a draft already in the box',
    strandedValue('draft', 'mine') === 'draft\n\nmine',
  );
  check(
    'the same stranded words handed twice land once',
    strandedValue('draft\n\nmine', 'mine') === null,
  );
  check(
    'an edit with no words strands nothing',
    strandedValue('draft', '  \n ') === null,
  );
}

process.exit(failures === 0 ? 0 : 1);
