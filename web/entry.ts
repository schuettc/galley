// entry.ts — the front door to web/: builds the editor, owns the App shell,
// and assembles the ten mixins everything else in this directory hangs off.
//
// The shell (internal/serve/edit.html) supplies #editor with data-room and
// data-doc and calls window.galleyEdit.init({room, wsURL}). `init()` below
// does the one-time construction: the Yjs document, the websocket provider,
// and TipTap bound to the SAME fragment the Go side writes — suggestion mode,
// litigation mode, and trail mode are extensions wired in here, alongside
// StarterKit and the node views (NoteBlock, FrontMatterBlock, MathBlock,
// TableRow/Cell/Header, the image and mermaid node views).
//
// `class App` is what init() constructs one of. It holds the state every
// surface in this directory reads — the editor, the ydoc, the provider, the
// latest poll response, which surfaces are on screen. Its own method list,
// 31 methods including the constructor, is not a handful and is not
// leftover scraps: the arrival strip (makeStrip/showStrip/hideStrip/showMe),
// the instruction rail itself (makeRail/paintRail), the trail
// (trailEntries/clearTrail), note-words painting (paintNoteWords), keeping
// the reviewer's place across a whole-document rebuild
// (keepPlace/rememberPlace/restorePlace/findSpan/spanIsShown/sectionFor),
// draft-carrying across that same rebuild
// (draftRoots/draftFields/captureDrafts/restoreDrafts), lit-run highlighting
// (watchLit/litRuns/litRunAt/paintLit/runsNow/reveal), and the bar/census
// wiring that watches the DOM for it (watchBar/chromeFrame/pulseCensus/
// clearNewLater/readChanges) all stayed here because they are the shell's
// own state, not because nobody got to them yet.
//
// Its READ surface is bigger than even that: immediately after the class
// declaration, ten `Object.assign(App.prototype, …Methods)` calls mix in
// the modules this file used to be — `keys.ts`, `pending.ts`, `sheet.ts`,
// `cards.ts`, `composer.ts`, `figures.ts`, `history.ts`, `bar.ts`,
// `verdict.ts`, `seal.ts` — each commented at its own assignment with what
// it owns. Read those comments, not this one, for what any given surface
// does; this file is the shell they all attach to, not a description of
// them.
//
// This file also imports several modules it does not own the state of:
// `rail.ts`, `card.ts`, `runs.ts`, `trail.ts`, `suggestions.ts`, `note.ts`,
// `figure.ts` and others are the arithmetic and DOM-building the mixins and
// this file call into, most of them pure (no `this`, checkable under
// `probe.mjs` with no browser at all); `versions.ts` exports its own
// `VersionsPanel` class with its own state, constructed once here and held
// on `this.versionsPanel` for the mixins to read. Their own headers say what
// each is for and, for the pure ones, why they are not mixins themselves.
//
// The contracts this file must not drift from:
//
//   fragment    "content" (ydoc.FragmentName). y-prosemirror's default is
//               "prosemirror"; a client that forgets to configure this edits a
//               different, empty fragment and reports no error at all.
//   marks       ins / del / highlight, attributes {author, at} as plain
//               objects — the shape internal/ydoc writes (see encodeMarkAttrs).
//   heading     level arrives as the STRING "2", not the number 2, because the
//               Go bridge writes XML attributes and those are strings. See
//               TolerantHeading.
//   undo        Yjs owns it. StarterKit's history extension is off; the
//               Collaboration extension brings y-prosemirror's yUndoPlugin and
//               binds Mod-z / Mod-y / Shift-Mod-z to it.
//   decisions   accept/reject are POSTs. The server transforms the document
//               and the result arrives over the websocket.
//
// TYPING `class App`, AND WHAT THIS TASK FOUND WRONG IN THE THREE TASKS THAT
// PRECEDED IT. `web/appshell.ts` carries the whole `this`-typing decision;
// read it first. Its conclusion applies here as designed: `class App
// implements AppState` is real verification (every field below is one the
// constructor assigns, checked against the interface's 78 assertions), and
// `interface App extends AppMethods {}` immediately after the ten
// `Object.assign` calls is a declaration of the other 82, not a check —
// necessarily so, since nothing in the class body defines them.
//
// THE MECHANISM WORKED EXACTLY AS DESIGNED. `class App implements AppState`
// compiles and genuinely checks every field; `interface App extends
// AppMethods {}` compiles and (as appshell.ts's header already says) proves
// nothing about the 82 beyond their names existing somewhere.
//
// TWO OF THE 78 ASSERTIONS WERE ALREADY WRONG BEFORE THIS TASK STARTED —
// `blurDismiss` and the six revise-label fields — both retyped `| undefined`
// by earlier tasks and left that way here; see appshell.ts for the
// constructor lines. TASK 8 FOUND NO FURTHER AppState FIELD WRONG: every
// other field really is assigned unconditionally in this constructor, in
// the order AppState lists them. What Task 8 DID find wrong were two things
// one level up from AppState's own fields:
//
//   - `scheduleAnchors` was declared as a METHOD in AppMethods, on the
//     assumption that everything reaching App via Object.assign belongs
//     there. It does not: `this.scheduleAnchors = coalesce(...)` a few
//     hundred lines down is a direct, unconditional FIELD assignment in
//     THIS constructor, the same shape as `cardSizes` beside it. Moved to
//     AppState; see appshell.ts's note at that field for the full argument.
//   - AppMethods was missing every constructor-only builder this file calls
//     by name (`makeCensus`, `makeSheet`, `makeRevise`, `onKey`, `refuse`,
//     `enterHistory`, and sixteen more) — real mixin methods from Tasks 5-7,
//     each already carrying `this: AppShell`, that nothing before this task
//     needed typed because nothing before this task's own constructor CALLED
//     them through a typed `this`. They are added to AppMethods now, for the
//     same reason every earlier member was: a member silently missing from
//     the shared surface that a real caller still needs is exactly the wrong
//     assumption AppShell exists to make loud. See appshell.ts's own
//     addition for the full list.
//
// `barSize` is the one field genuinely conditional at construction —
// `this.barSize = new ResizeObserver(...)` runs only inside watchBar()'s
// `typeof window.ResizeObserver === 'function'` branch — and is typed
// `ResizeObserver | undefined` for exactly that reason; it is not in
// AppState because no mixin reads it, only watchBar's own unpublish path
// would (there is none).

import type {
  AppState,
  AppMethods,
  Thread,
  CardEntry,
  ArrivalItem,
} from './appshell.ts';
import { Editor, Mark, mergeAttributes } from '@tiptap/core';
import { Extension } from '@tiptap/core';
import type { Node as PMNode, ResolvedPos } from '@tiptap/pm/model';
import type { EditorState, Transaction } from '@tiptap/pm/state';
import StarterKit from '@tiptap/starter-kit';
import Heading from '@tiptap/extension-heading';
import Link from '@tiptap/extension-link';
import Image from '@tiptap/extension-image';
import CodeBlock from '@tiptap/extension-code-block';
import type { NodeView } from '@tiptap/pm/view';
import Table from '@tiptap/extension-table';
import TableRow from '@tiptap/extension-table-row';
import TableCell from '@tiptap/extension-table-cell';
import TableHeader from '@tiptap/extension-table-header';
import { NoteBlock, noteWordsKey } from './note.ts';
import { FrontMatterBlock } from './frontmatter.ts';
import { MathBlock } from './math.ts';
import { litKey, litPlugin, sameRuns } from './lit.ts';
import { scopePlugin } from './scope.ts';
import { keyMethods } from './keys.ts';
import { pendingMethods } from './pending.ts';
import { sheetMethods } from './sheet.ts';
import { cardMethods } from './cards.ts';
import { composerMethods, holdsWords } from './composer.ts';
import { figureMethods } from './figures.ts';
import { historyMethods } from './history.ts';
import { barMethods, MODE_ASK } from './bar.ts';
import { verdictMethods } from './verdict.ts';
import { sealMethods } from './seal.ts';
import { coerceLevel } from './heading.ts';
import { runFor, markElement } from './runs.ts';
// ONE CARD AND ONE REVEAL, shared with History's rail. See web/card.ts, whose
// header carries the whole argument: the two rails are one surface and were
// built twice.
import { revealMark, coalesce, growthWatch, anchorNote } from './card.ts';
import { VersionsPanel } from './versions.ts';
// getJSON/postJSON own talking to the server; see web/net.ts's header for why
// they cannot live in a mixin.
import { getJSON } from './net.ts';
// The verdict vocabulary — see web/verdict.ts's header. Not everything this
// file exports is needed here; only what the constructor and the methods
// that stayed in this file (the App shell itself, not a mixin) read.
import { REVISE_IDLE } from './verdict.ts';
import { imageNodeView, mermaidNodeView } from './figure.ts';
import Collaboration from '@tiptap/extension-collaboration';
import { TextSelection } from '@tiptap/pm/state';
import { ySyncPluginKey } from 'y-prosemirror';
import * as Y from 'yjs';
import { WebsocketProvider } from 'y-websocket';

import {
  suggestionPlugin,
  SuggestionUI,
  age,
  markRuns,
} from './suggestions.ts';
import type {
  SuggestionLike,
  SuggestionRun,
  LiteralHit,
  ThreadLike,
} from './suggestions.ts';
import { trailPlugin, trailPluginKey } from './trail.ts';
import {
  nextArrival,
  ARRIVAL_AGENT,
  ARRIVAL_FADE_MS,
  ARRIVAL_SUFFIX,
} from './arrivals.ts';
import { carryDrafts } from './rail.ts';
import { mountPreview } from './preview.ts';
import { shellMarkerQuiet } from './marker.ts';
import { headingAlign } from './headingalign.ts';
import type { Arrival } from './versions.ts';
import type { Composer } from './composer.ts';
import type { Mode, ModeUI } from './bar.ts';
import { makeMenu, openMenu, closeMenu, menuOpen } from './menu.ts';
import type { DocMenu } from './menu.ts';
import type { OverallCard, CaptureCard } from './cards.ts';
import type { BlockRef, ReviewerChange } from './wire';

// The fragment field. Spelled once here, once in Go's ydoc.FragmentName.
const FIELD = 'content';

// The SVG namespace. `document.createElement` in the HTML namespace produces an
// element that LOOKS like an <svg> in the inspector and renders nothing at all.

const POLL_MS = 1500;

// DELETE_ARM_MS and DELETE_ARM_NOTE moved to web/cards.ts with deleteButton,
// their only reader.

// How long a revealed mark keeps its ring lived here, beside `flash()`. Both
// moved to web/card.ts with the whole reveal, because History was answering the
// same question a second, shorter way — see that file's header.

// The meta App.keepPlace stamps on the transaction it dispatches to put the
// caret back, so it does not read its own correction as the reviewer moving.
const PLACE_META = 'galleyKeepPlace';

// --- suggestion marks ---
//
// Three marks, one shape. Each renders the span the stylesheet knows about and
// carries the author and timestamp the Go side stores as a plain object on the
// formatting attribute. `inclusive: false` matters: without it, typing at the
// end of struck text would silently join the strike.

function suggestionMark(name: string, className: string, label: string) {
  return Mark.create({
    name,
    inclusive: false,
    addAttributes() {
      return {
        author: {
          default: '',
          parseHTML: (element) => element.getAttribute('data-author') || '',
          renderHTML: (attrs) =>
            attrs.author ? { 'data-author': attrs.author } : {},
        },
        at: {
          default: '',
          parseHTML: (element) => element.getAttribute('data-at') || '',
          renderHTML: (attrs) => (attrs.at ? { 'data-at': attrs.at } : {}),
        },
        // The mark's identity, minted server-side (docmodel.RunAttr). It
        // reaches the DOM as data-run so a card can find its own anchor by
        // querying for it, instead of searching the document for text that
        // may occur more than once.
        run: {
          default: '',
          parseHTML: (element) => element.getAttribute('data-run') || '',
          renderHTML: (attrs) => (attrs.run ? { 'data-run': attrs.run } : {}),
        },
        // A comment's ID (docmodel.CommentIDAttr). Only a highlight carries
        // one, but the three marks share this factory. Unlike the run it is
        // in the file — "{==…==}{>>@comment cm-…<<}" — so an undeclared
        // attribute here would be dropped by y-prosemirror and the browser's
        // next write would take the comment's mark out of the .md.
        id: {
          default: '',
          parseHTML: (element) => element.getAttribute('data-comment-id') || '',
          renderHTML: (attrs) =>
            attrs.id ? { 'data-comment-id': attrs.id } : {},
        },
      };
    },
    parseHTML() {
      return [{ tag: `span.${className}` }];
    },
    renderHTML({ HTMLAttributes, mark }) {
      const who = mark.attrs.author || 'unattributed';
      const when = age(mark.attrs.at);
      return [
        'span',
        mergeAttributes(HTMLAttributes, {
          class: className,
          title: when ? `${label} · ${who} · ${when}` : `${label} · ${who}`,
        }),
        0,
      ];
    },
  });
}

export const Ins = suggestionMark('ins', 'gly-ins', 'insertion');
export const Del = suggestionMark('del', 'gly-del', 'deletion');
export const Highlight = suggestionMark('highlight', 'gly-hl', 'comment');

// coerceLevel moved to web/heading.ts — shared with sectionSpan
// (web/figures.ts), which is what moved a helper that used to live here
// alone out to a module both this file and that one import.

export const TolerantHeading = Heading.extend({
  addAttributes() {
    return {
      level: {
        default: 1,
        rendered: false,
        parseHTML: (element) =>
          coerceLevel(String(element.tagName).replace(/\D+/g, '')),
      },
    };
  },
  renderHTML({ node, HTMLAttributes }) {
    return [
      `h${coerceLevel(node.attrs.level)}`,
      mergeAttributes(this.options.HTMLAttributes, HTMLAttributes),
      0,
    ];
  },
});

// --- a code fence cannot be CREATED here either ---
//
// A fence is read-only (see suggestions.ts), and phase 1 has nowhere to record
// an edit to one. Creating one is the same problem wearing a friendlier face:
// ``` + space made an untracked, empty codeBlock — nothing in
// /_galley/pending, nothing to accept — and then trapped the reviewer inside
// it. Typing refused, Backspace refused (a join), Mod-Alt-c refused
// (un-fencing): Ctrl-Z was the only way out, and anyone who did not undo left
// an empty fence in the author's file with no record that it happened. The
// refusal's advice ("edit fenced code in your own editor") was wrong for a
// fence made two seconds ago in THIS editor.
//
// The fix is not to allow editing the fences you made — that means tracking
// provenance, and reopens the "nowhere to record it" problem that made the
// original three bugs one bug. It is to make the policy on screen true exactly
// as written: you cannot make a fence here, and you cannot change one.
// Recording fence content properly is phase 3's per-line mechanism, not a
// fourth special case.
//
// So the node stays in the schema, exactly as it is — documents are FULL of
// fences and every one of them must still parse, render, select and copy — and
// only the three ways this editor could CREATE one are removed:
//
//   input rules            ``` and ~~~ turning a paragraph into a fence
//   Mod-Alt-c              toggleCodeBlock, which also un-fences
//   the VS Code paste      the extension's own plugin, which turns a paste
//   handler                carrying `vscode-editor-data` into a new fence
//
// Everything else the extension does is kept — including its Backspace, Enter
// and ArrowDown handlers, which act INSIDE a fence and are refused by the
// transaction filter with a note, the same as any other fence edit.
export const UncreatableCodeBlock = CodeBlock.extend({
  addInputRules() {
    return [];
  },
  addKeyboardShortcuts() {
    const inherited = (this.parent && this.parent()) || {};
    const kept: typeof inherited = {};
    for (const [key, handler] of Object.entries(inherited)) {
      if (key !== 'Mod-Alt-c') {
        kept[key] = handler;
      }
    }
    return kept;
  },
  addProseMirrorPlugins() {
    return [];
  },
});

// --- a table renders, and refuses every edit ---
//
// The same cut as the code fence, for the same reason, in the same voice.
// Tables are read-only in phase 1: they render, prose around them is fully
// markable, and cells are not editable and carry no suggestion marks.
//
// Why the cut is here and not one notch further. A cell is prose, so
// half-supporting it is tempting in a way a fence never was — and
// half-supporting is precisely how the fence bug happened. Creating a fence
// was allowed where editing one was not, so ``` + space made an untracked
// empty block with nothing in /_galley/pending, and then trapped the reviewer
// inside it. A table has more ways to go wrong, not fewer: suggest.List reads
// Block.Inlines and a cell's inlines are a level below anything the
// suggestion pipeline reaches, and the column count and the delimiter row are
// STRUCTURE, which phase 1 does not track at all (the README says so, and the
// editor's panel says so). Cell-level tracked changes are phase 3, alongside
// per-line code suggestions.
//
// So the nodes stay in the schema — documents are full of tables and every
// one must parse, render, select and copy — and every way this editor could
// change or create one is removed:
//
//   tableEditing plugin   cell selection, the Tab/Backspace cell handling and
//                         the goToNextCell keymap
//   columnResizing        drags a column border and writes colwidth onto
//                         every cell in it: a real mutation of the author's
//                         document, with no keystroke and no record of it
//   the commands          insertTable, addColumnBefore, deleteRow and the
//                         rest — none reachable from the toolbar today, and
//                         none left armed for a keybinding to find tomorrow
//
// The transaction filter refuses anything that reaches a table anyway (see
// literalHit), so this is the second lock rather than the only one — same
// belt-and-braces the fence has.
// --- the passive half of the refusal voice, as an element ---
//
// A fence and a table wear their "read-only" chip through a ::after rule in the
// stylesheet — generated content, so there is no DOM node inside the
// contenteditable for ProseMirror to be told to ignore. A figure's NodeView has
// no such constraint (the whole subtree is already ours and already ignored),
// and it needs a real element because the chip has to sit above the rendered
// SVG rather than above whatever box the browser gives a replaced element.
//
// Same word, one place. A second spelling of "read-only" is a second voice.
export function readOnlyChip() {
  const el = document.createElement('span');
  el.className = 'gly-chip';
  el.textContent = 'read-only';
  return el;
}

// A figure RENDERS, and refuses every edit — the same cut as the fence and the
// table, for the same reason. An image and a mermaid fence are the two blocks
// phase 1 cannot let anyone type into: there is nothing in Block.Inlines to
// hang a mark on, and a fence is literal text galley never rewrites.
//
// Both NodeViews are presentation-only. See web/figure.ts.
export const FiguredImage = Image.extend({
  addNodeView() {
    return imageNodeView(readOnlyChip);
  },
});

// THE ONE `as` IN THIS CONVERSION, AND IT IS DELIBERATE — read this whole
// comment before touching the line below.
//
// entry.ts returned `null` from addNodeView's inner function for a non-
// mermaid fence: "A falsy NodeView is how ProseMirror is told 'render this
// one the ordinary way': NodeViewDesc.create falls through to the schema's
// toDOM when the custom view returns nothing" — verified against
// prosemirror-view's own source (`NodeViewDesc.create`,
// node_modules/prosemirror-view/dist/index.js: `let spec = custom &&
// custom(...)`, and the plain-`NodeViewDesc` branch runs only when `spec`
// is falsy) and against THIS REPOSITORY'S OWN TEST: probe.mjs's "a
// non-mermaid fence gets no NodeView at all" calls
// `FiguredCodeBlock.config.addNodeView` directly and asserts the result is
// `=== null`. A reconstruction that builds a real, DOM-shaped stand-in
// (tried first, reverted) fails that exact assertion and, worse, throws in
// probe.mjs's no-DOM environment the moment it touches `document` — this
// module's own header requires every module-scope-reachable path to run
// with no DOM at all, and this one is reached from a real test, not merely
// imported.
//
// `@tiptap/core`'s `NodeViewRenderer` type, and prosemirror-view's
// `NodeViewConstructor` beneath it, both declare a NON-nullable `NodeView`
// return — no version of either package's shipped .d.ts admits `null`, even
// though returning it is the library's own documented mechanism for falling
// back to default rendering. There is no way to state "this returns
// `NodeView | null`, and that is fine" under this task's constraints: a
// generic identity wrapper, a local type alias, a `declare module`
// augmentation (impossible here regardless — `NodeViewRenderer` is a `type`
// alias, and TypeScript does not merge declarations into type aliases) —
// every route bottoms out at asserting a `null` value has a non-nullable
// object type, which is what `as` is FOR. This is that one time.
export const FiguredCodeBlock = UncreatableCodeBlock.extend({
  addNodeView() {
    const draw = mermaidNodeView(readOnlyChip);
    return (props) => {
      // ONLY a mermaid fence. Every other fence keeps the plain <pre><code>
      // rendering, which is what makes its text selectable and copyable —
      // "select and copy still work" is half the refusal, and replacing an
      // ordinary fence with a picture would make that sentence false.
      //
      // A falsy NodeView is how ProseMirror is told "render this one the
      // ordinary way": NodeViewDesc.create falls through to the schema's toDOM
      // when the custom view returns nothing.
      if (props.node.attrs.language !== 'mermaid') {
        return null as unknown as NodeView;
      }
      return draw(props);
    };
  },
});

export const ReadOnlyTable = Table.extend({
  addProseMirrorPlugins() {
    return [];
  },
  addKeyboardShortcuts() {
    return {};
  },
  addCommands() {
    return {};
  },
});

// alignAttribute is docmodel.AlignAttr arriving from the Go side. The bridge
// writes "align" onto every cell element in the CRDT, and an attribute the
// TipTap schema has not DECLARED is one ProseMirror drops on the way in — the
// column would render left-aligned however the file spells it, silently, in
// the one direction nobody would think to test.
const alignAttribute = {
  align: {
    default: null,
    parseHTML: (el: HTMLElement) => el.style.textAlign || null,
    renderHTML: (attrs: { align?: string | null }) =>
      attrs.align ? { style: `text-align: ${attrs.align}` } : {},
  },
};

export const AlignedTableCell = TableCell.extend({
  addAttributes() {
    return { ...(this.parent ? this.parent() : {}), ...alignAttribute };
  },
});

export const AlignedTableHeader = TableHeader.extend({
  addAttributes() {
    return { ...(this.parent ? this.parent() : {}), ...alignAttribute };
  },
});

// onRefused is handed in rather than reached for, because the extension is
// built while the Editor is being constructed and the App that paints the
// refusal does not exist until after that returns.
function suggestionMode(onRefused: (hit: LiteralHit) => void) {
  return Extension.create({
    name: 'galleySuggestionMode',
    addProseMirrorPlugins() {
      return [suggestionPlugin(onRefused)];
    },
  });
}

// The light: the plugin that holds which runs are being attended to and paints
// them as decorations. It takes no injection — the set arrives as transaction
// meta from App.paintLit, because where the pointer is is not in the document
// and must never enter it. See lit.ts.
function litMode() {
  return Extension.create({
    name: 'galleyLitMode',
    addProseMirrorPlugins() {
      return [litPlugin()];
    },
  });
}

// The block grip's scope: the outline over what a block instruction will be
// about, while its composer is up. Meta from openBlockComposer and
// hideComposer, for litMode's reason. See scope.ts.
function scopeMode() {
  return Extension.create({
    name: 'galleyScopeMode',
    addProseMirrorPlugins() {
      return [scopePlugin()];
    },
  });
}

// The trail: the plugin that records the reviewer's direct edits and paints
// their ghosts and highlights. getBlocks is injected for suggestionMode's
// reason — the App that holds the block list does not exist yet.
function trailMode(getBlocks: () => BlockRef[]) {
  return Extension.create({
    name: 'galleyTrailMode',
    addProseMirrorPlugins() {
      return [trailPlugin(getBlocks)];
    },
  });
}

// --- the app ---

// The shell's own global contract: `edit.html`'s load handler calls
// `window.galleyEdit.init({room, wsURL})`, and `init()` below fills in the
// other three fields once the editor and its provider exist. Declared here
// rather than left as an implicit `any` write, which is what an untyped
// `window.galleyEdit.editor = editor` would otherwise silently be.
declare global {
  interface Window {
    galleyEdit: {
      init: typeof init;
      editor?: Editor;
      provider?: WebsocketProvider;
      app?: App;
    };
  }
}

let started = false;

function init(opts?: { room?: string; wsURL?: string }): void {
  if (started) {
    return;
  }
  started = true;

  const mount = document.getElementById('editor');
  if (!mount) {
    // #editor is always in the served shell (internal/serve/edit.html:250
    // unconditionally renders `<div id="editor" ...>`), so this is
    // unreachable in production — `getElementById` is typed
    // `HTMLElement | null` and strict mode requires every later read of
    // `mount` to be honest about that. A DEAD GUARD, WRITTEN DOWN AND NOT
    // DELETED per CLAUDE.md's rule of the same name; Task 9 sweeps it.
    return;
  }
  // Same reasoning as `mount` above: `data-room` is unconditionally rendered
  // by the same template, so `room` is never actually null in production —
  // the `|| ''` is the honest fallback strict null checks ask for on
  // `getAttribute`'s return type, not a behaviour change to anything a real
  // page load can reach.
  const room = (opts && opts.room) || mount.getAttribute('data-room') || '';
  const wsURL = (opts && opts.wsURL) || defaultWsURL();

  // PAGE MODE, read HERE so the shell-marker extension is only ever installed
  // for a page-backed review. The shell writes data-page-mode="1" onto the
  // mount when `galley edit page.html` runs (internal/serve/editmode.go); in
  // markdown mode it is empty, no document there carries `⟦ shell N ⟧`
  // markers, and the quieting extension is never even added — the markdown
  // editor's schema and plugins are byte-identical to before this pass.
  const pageMode = mount.getAttribute('data-page-mode') === '1';

  const ydoc = new Y.Doc();
  const provider = new WebsocketProvider(wsURL, room, ydoc);

  // Set once the App exists. A refusal before then is impossible (nothing has
  // been typed yet) but costs nothing to survive.
  let app: App | null = null;

  const editor = new Editor({
    element: mount,
    extensions: [
      // history: false — Yjs owns undo. Leaving it on gives two undo stacks
      // over one document, and the ProseMirror one does not know about
      // anybody else's edits.
      //
      // codeBlock: false does NOT remove fences from the schema — it removes
      // StarterKit's copy of the extension so UncreatableCodeBlock can take its
      // place under the same node name. Dropping the node itself would delete
      // every fence in the document on load. Same pattern as heading.
      StarterKit.configure({
        history: false,
        heading: false,
        codeBlock: false,
      }),
      TolerantHeading,
      // The fence extension WITH the mermaid NodeView on it. The node, its
      // schema entry and every refusal are UncreatableCodeBlock's, unchanged —
      // this only decides how a mermaid one is drawn.
      FiguredCodeBlock,
      Link.configure({ openOnClick: false, autolink: false }),
      FiguredImage,
      // resizable: false as well as the stripped plugins — the option is what
      // the extension reads, the stripped plugins are what actually removes
      // the drag handles. Either alone leaves half the mechanism armed.
      ReadOnlyTable.configure({ resizable: false }),
      TableRow,
      AlignedTableHeader,
      AlignedTableCell,
      // Registered even though nothing here edits it: y-prosemirror does not
      // ignore an element whose nodeName the schema lacks, it DELETES it from
      // the Yjs document — and galley projects that deletion to the .md. A
      // reviewer's block note vanished from their file the first time the
      // document was opened. See note.ts.
      NoteBlock,
      // Same sentence, a different tag: front matter is written into the
      // fragment by the Go side, so a schema with no <frontMatter> node is a
      // schema that DELETES the author's metadata on open — and the projection
      // writes the deletion to their file. See frontmatter.ts.
      FrontMatterBlock,
      // And a third tag under the same sentence: display math is written into
      // the fragment by the Go side as <mathBlock>, so a schema without it is a
      // schema that DELETES the author's formula on open. See math.ts —
      // internal/ydoc's TestEveryBlockKindExistsInTheBrowserSchema is the gate
      // that made this line unskippable.
      MathBlock,
      Ins,
      Del,
      Highlight,
      Collaboration.configure({ document: ydoc, field: FIELD }),
      suggestionMode((hit) => {
        if (app) {
          app.refuse(hit);
        }
      }),
      trailMode(() => (app ? app.blocks : [])),
      litMode(),
      scopeMode(),
      // PAGE MODE ONLY: quiet the `⟦ shell N ⟧` marker paragraphs. A node
      // decoration restyles them inert (hidden via CSS, contenteditable=false
      // so the caret skips them) while leaving the nodes — and content.md —
      // untouched. Not installed at all in markdown mode. See web/marker.ts.
      ...(pageMode ? [shellMarkerQuiet()] : []),
      // PAGE MODE ONLY: hold the measured per-heading spacers that align the
      // content pane to the live page at the section level. Inert until
      // preview.ts pushes a map; never installed in markdown mode. See
      // web/headingalign.ts.
      ...(pageMode ? [headingAlign()] : []),
    ],
    autofocus: false,
  });

  app = new App({ editor, provider, room });
  window.galleyEdit.editor = editor;
  window.galleyEdit.provider = provider;
  window.galleyEdit.app = app;

  // PAGE MODE ONLY. The shell writes data-page-mode="1" and a data-preview URL
  // onto the mount when `galley edit page.html` runs (internal/serve/editmode.go);
  // in markdown mode both are empty, the guard fails, and nothing below this
  // line runs — the markdown editor is byte-identical to before this pass. The
  // preview owns its own DOM restructuring and its own poll; see web/preview.ts.
  // `pageMode` is read once above (it also guards the shell-marker extension).
  const previewURL = mount.getAttribute('data-preview') || '';
  if (pageMode && previewURL) {
    mountPreview(mount, previewURL, editor);
  }
}

// suggestions.ts's own boundary is deliberately opaque: `SuggestionUI`'s
// `threadCard`/`onReveal` callbacks are typed to receive a `ThreadLike`
// (`{run?: string}`) because that module reads nothing else off a thread —
// see its own header at ThreadLike's definition. But the OBJECT it actually
// hands back always comes straight from `this.comments` (threadFor filters
// the very array `threads()` returns, see suggestions.ts's threadFor), which
// this App always populates as `Thread[]`. So the richer shape is real, not
// assumed — narrowed here with a real runtime check rather than trusted,
// the same discipline rail.ts's `keyTargetIsEditable` uses for the same
// reason: a boundary's declared type is narrower than what it actually
// carries, and `in` says so honestly instead of asserting it away.
function isThread(t: ThreadLike): t is Thread {
  return 'key' in t;
}

function defaultWsURL(): string {
  const scheme = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
  return `${scheme}${window.location.host}/yjs`;
}

// keepPlace's own coordinates — see placeOf below for what each one means and
// why these two are the only pair a whole-document rebuild cannot destroy.
interface PlaceEnd {
  index: number;
  prefix: string;
}
interface Place extends PlaceEnd {
  end: PlaceEnd | null;
  text: string;
  back: boolean;
}
interface Span {
  from: number;
  to: number;
  back: boolean;
}

// `class App implements AppState` is REAL verification — see this file's own
// header and web/appshell.ts's for what that means and what it does not.
// `interface App extends AppMethods {}`, straight after the ten
// `Object.assign` calls below, is the other half: a declaration of the 82
// methods that arrive at runtime, checkable only as far as "something by
// this name exists somewhere in AppMethods".
class App implements AppState {
  // --- editor and identity ---
  editor: Editor;
  // Not part of AppState: no mixin reads `this.provider` (the websocket
  // connection status arrives through `provider.on('status', ...)`, wired
  // once below, and nothing else touches the provider itself), so it is
  // typed here and only here.
  provider: WebsocketProvider;
  docName: string;
  room: string | null;
  sealed: boolean;

  // --- the pending view: suggestions, threads, blocks ---
  suggestions: SuggestionLike[];
  prevSuggestions: SuggestionLike[] | null;
  comments: Thread[];
  changes: ReviewerChange[];
  armedRevert: string | null;
  armedRevertAt: number;
  blocks: BlockRef[];
  pendingCount: number;
  verdict: string;

  // --- the rail, the sheet, and their shared card lists ---
  rail: { root: HTMLElement; band: HTMLElement; notice: HTMLElement };
  sheet: {
    root: HTMLElement;
    head: HTMLElement;
    body: HTMLElement;
  };
  cards: CardEntry[];
  sheetCards: CardEntry[];
  sheetOpen: boolean;
  overall?: OverallCard;
  capture?: CaptureCard;
  cardSizes: ReturnType<typeof growthWatch>;
  // See this file's own header for why this moved here from AppMethods.
  scheduleAnchors: () => void;
  armedDelete: string | null;
  armedAt: number;
  editingThread: string | null;
  stepped: string | null;

  // --- the bubble, and the census/bar chrome ---
  bubble: SuggestionUI;
  census: { root: HTMLElement; count: HTMLButtonElement };
  bar: {
    root: HTMLElement;
    count: HTMLButtonElement;
    versions: HTMLButtonElement;
  };
  strip: {
    root: HTMLElement;
    text: HTMLElement;
    read: HTMLButtonElement;
    show: HTMLButtonElement;
    dismiss: HTMLButtonElement;
    fade: HTMLElement;
  };

  // --- arrivals: the queue, the hold, the strip's batch ---
  arrivalQueue: ArrivalItem[];
  stripBatch: ArrivalItem[];
  held: Set<string>;
  heldArrivals: ArrivalItem[];
  holding: boolean;
  newRuns: Set<string>;
  // Two timer handles private to this file's own strip/badge painters — not
  // shared with any mixin, so not in AppState. Both are direct, unconditional
  // constructor assignments (`this.stripTimer = 0; this.newTimer = 0;`).
  stripTimer: number;
  newTimer: number;

  // --- History: the door, the round, the reading state ---
  versionsPanel: VersionsPanel;
  versionsButton: HTMLButtonElement;
  // The bar's capture door. Built once in the constructor and never rebuilt,
  // like its neighbour — the bar's controls outlive every paint.
  captureBtn: HTMLButtonElement;
  historyScroll: number;
  seenRound: number | null;
  seenCannot: string;
  arrival: Arrival | null;

  // --- the verdict menu, and polling ---
  verdictOpen: boolean;
  rev: number | null;
  savedMs: number;

  // --- Revise (web/verdict.ts) ---
  revise: HTMLButtonElement | null;
  reviseCount?: HTMLElement;
  reviseIdle?: HTMLElement;
  reviseBusy?: HTMLElement;
  reviseSecs?: HTMLElement;
  reviseApprove?: HTMLElement;
  reviseBack?: HTMLElement;
  verdictMenu: HTMLElement | null;
  reviseRunning: boolean;
  approveNotBefore: number;
  reviseStartedAt: number;

  // --- the seal and the handoff (web/seal.ts) ---
  sealVerdict: string;
  sealAt: number;
  sealEntrusted: number;
  sealOutstanding: number;
  sealLanding: number;
  sealUI: { readout: HTMLElement };
  cancelBtn: HTMLButtonElement | null;

  // --- the block grips (web/figures.ts) ---
  grips: HTMLElement;
  scopeBox: HTMLElement;
  scheduleGrips: () => void;
  gripSizes?: ResizeObserver;

  // --- the right-click menu in the document (web/menu.ts) ---
  //
  // Built once, on `body`, and never rebuilt — only its rows are. It is
  // unconditional (`this.menu = makeMenu()` in the constructor), so unlike
  // `overall` and `capture` it is never optional.
  menu: DocMenu;

  // --- the composer, and the refused-keystroke note (web/composer.ts) ---
  composer: Composer;
  refusal: HTMLElement;
  refusalPending: LiteralHit | null;
  refusalTick: number;
  refusalTimer: number;
  refusalShownAt: number;
  blurDismiss: number | undefined;

  // --- the bar's status readout and mode toggle (web/bar.ts) ---
  status: HTMLElement;
  said: string;
  connection: string;
  historyCount: number;
  approved: boolean;
  handoff: boolean;
  draftError: string;
  diskNotice: string;
  reviseWaiting: boolean;
  mode: Mode;
  modeUI: ModeUI;

  // --- fields private to this file's own methods (lit-run highlighting,
  //     the caret-restoring rebuild, run memoisation) — no mixin reads any
  //     of these, so none of them are in AppState. Every one is a direct,
  //     unconditional constructor assignment; see the constructor body for
  //     the exact line. ---

  // runsNow's memoisation key and cache — see that method for why identity
  // is an exact cache key over an immutable ProseMirror document.
  runsDoc: PMNode | null;
  runs: SuggestionRun[];
  // The two intents that light a card and its words — see watchLit.
  litHover: string;
  litFocus: string;
  // Where the reviewer was, and whether a restore is in flight — see
  // keepPlace and restorePlace.
  place: Place | null;
  restoring: boolean;
  placeScrollY: number;

  // barSize IS GENUINELY CONDITIONAL, unlike every field above. It is
  // assigned inside `watchBar()` (`this.barSize = new
  // window.ResizeObserver(publish);`), a method the constructor CALLS rather
  // than a constructor-own statement — strictPropertyInitialization does not
  // trace into a called method any more than it credits a callback or a
  // conditional branch (see this file's header, and blurDismiss/the revise
  // labels in appshell.ts for the same shape) — and even inside that method
  // the assignment is itself behind `typeof window.ResizeObserver ===
  // 'function'`. No mixin reads it; it exists only so `watchBar`'s own
  // resize fallback below it can tell whether an observer was ever made.
  barSize?: ResizeObserver;

  constructor({
    editor,
    provider,
    room,
  }: {
    editor: Editor;
    provider: WebsocketProvider;
    room: string | null;
  }) {
    this.editor = editor;
    this.provider = provider;
    // The room this page booted into. /rev reports the room the server is
    // running now; a difference means we outlived a restart. See tick().
    this.room = room || null;
    this.suggestions = [];
    this.comments = [];
    this.changes = [];
    this.armedRevert = null;
    this.armedRevertAt = 0;
    // WHAT THE PRESS WILL SEND, as the server counted it. It is read off the
    // /_galley/pending payload beside the verdict itself (refreshPending) and
    // never off this page's own rail, for the reason the verdict is: a count
    // derived from what got drawn is a count that can disagree with the
    // document the press is about to hand over.
    this.pendingCount = 0;
    // `this.changes` IS DELETED WITH THE TRAIL'S SERVER HALF. It held the
    // SERVER's copy of the trail for one purpose — the count on the verdict
    // button — and `pendingView` is `{instructions, blocks}`: it has no
    // `changes` field and never had one on this wire, so the assignment read
    // `undefined || []` on every poll and nothing ever read the result back.
    // The verdict button counts `pendingCount`, which the same payload really
    // does carry.
    this.savedMs = 0;
    this.rev = null;
    this.connection = 'connecting';
    // What the server last said in reply to something the reviewer pressed —
    // `revision requested`, `approving…`, a failure, a reopen's reason. The
    // FIRST clause of the one readout, and the only clause a poll cannot
    // write: the two used to be separate elements precisely so a poll could
    // not swallow the reply, and now they are separate FIELDS, which is the
    // same guarantee without a second box on the bar. It is held until
    // something supersedes it (`say`) — a reply that faded on a timer would be
    // the swallowing back, just politer.
    this.said = '';
    // One entry per rendered card: its element, the run it is anchored to, and
    // paintAnchors walks it; nothing else may.
    this.cards = [];
    this.runsDoc = null;
    this.runs = [];
    // A CARD THAT GROWS AFTER IT WAS PLACED LEAVES EVERY CARD BELOW IT STALE.
    // The stack is exact arithmetic over the heights it was handed, and those
    // heights were measured once — so a reply box expanding under a second line
    // of typing, or an entry arriving into an already-positioned card, moves
    // that card's bottom edge without moving anything else, and it lands on top
    // of the reply box and the buttons of its neighbour. Observed with a thread
    // card drawn over the card below it; the same class of bug as the ceiling
    // one, which was also geometry computed once against something that then
    // changed size.
    //
    // The answer is to stop assuming heights hold — `growthWatch` in card.ts,
    // which History's rail uses over its own cards for the same reason and
    // carries the whole argument. It cannot feed itself because a placement
    // pass writes only `top` on a card.
    this.cardSizes = growthWatch(() => this.scheduleAnchors());

    const mount = document.getElementById('editor');
    this.docName = (mount && mount.dataset.doc) || '';
    // THE TRAIL HAS NO REGION TO REMEMBER A COLLAPSE FOR, AND NO SAVE STATE
    // EITHER. `changedOpen` was a collapse flag for the log at the rail's foot; the trail is an outgoing message and not a history, so there
    // is no log, no head to open and nothing to persist.
    //
    // `trailPosted`, `trailLoaded`, `trailTimer`, `trailSaving` and `trailEpoch`
    // are DELETED with the save they bookkept. See the note where `syncTrail`
    // was: the browser POSTed the whole trail to /_galley/trail on a 600ms
    // debounce after every keystroke and again on every 1.5s poll, that route
    // does not exist, and the handler advanced its baseline only on `res.ok` —
    // so a typing session retried a 404 forever and never persisted anything.
    this.sheetOpen = false;
    // Which thread's delete control is armed, and since when. On the APP rather
    // than in the button, because every card is destroyed and rebuilt on every
    // pending refresh — see deleteButton.
    this.armedDelete = null;
    this.armedAt = 0;
    // Which instruction is open for editing, keyed by the thread's stable key
    // — the same rule as the armed delete flag directly above, and for the
    // same reason: paintRail destroys every card, so state held in a card's
    // closure is state that lasts about a second. See editButton.
    this.editingThread = null;
    // The stepped mark's run. Not an index: indices renumber when a suggestion
    // resolves, and the whole point of a run is that it does not.
    this.stepped = null;
    this.sheetCards = [];

    // The addressable blocks, as the SERVER sees them. A block grip files its
    // instruction against a block key, and a key is a content hash the browser
    // cannot compute — so a grip's composer can send only for a block the last
    // /_galley/pending actually reported.
    this.blocks = [];

    this.status = this.makeStatus();
    this.census = this.makeCensus();
    this.rail = this.makeRail();
    this.composer = this.makeComposer();
    // ONE GRIP BESIDE EVERY BLOCK that takes a whole-block instruction, in a
    // layer beside the document. Painted on one animation frame however many
    // things ask in it: every doc change, every pending refresh, and every
    // change in the document's size (an image loading, a diagram rendering,
    // a line wrapping, the window resizing), which moves the blocks under the
    // grips without changing the document.
    //
    // THE LAYER'S PARENT IS OBSERVED AS WELL AS THE COLUMN. In page mode the
    // column is a fixed width centred in `#editor`, so a window resize moves
    // every block sideways and leaves `.ProseMirror`'s own size alone: an
    // observer on the column alone never fires, and every grip stays where
    // the column used to be. `#editor` is what changes size then.
    this.grips = this.makeGripLayer();
    this.scopeBox = this.makeScopeBox();
    this.scheduleGrips = coalesce(() => this.paintGrips());
    if (typeof window.ResizeObserver === 'function') {
      this.gripSizes = new window.ResizeObserver(() => this.scheduleGrips());
      this.gripSizes.observe(editor.view.dom);
      if (this.grips.parentElement) {
        this.gripSizes.observe(this.grips.parentElement);
      }
    } else {
      window.addEventListener('resize', () => this.scheduleGrips());
    }
    this.refusal = this.makeRefusal();
    // The right-click menu, built once and on `body` — see web/menu.ts for
    // why `body` and never `.ProseMirror`.
    this.menu = makeMenu();
    // THE ROUNDS, read-only, and additive in the strictest sense: nothing on
    // this page behaves differently because it exists. It builds its own
    // surface in body and its own control in the bar, and neither the rail,
    // the sheet, the census nor the verdict knows about it.
    this.historyScroll = 0;
    this.historyCount = 0;
    this.versionsPanel = new VersionsPanel({
      getJSON,
      docName: this.docName,
      onOpen: () => this.enterHistory(),
      onClose: () => this.leaveHistory(),
      onCount: (count) => {
        this.historyCount = count;
        this.paintVersionsButton();
        // The readout names the round, so a round landing has to repaint it.
        this.paintReadout();
      },
      onRestored: (version, error) => this.didRestore(version, error),
      // The readout says WHICH of History's two stages is on screen, and the
      // primary's face follows the panel being open at all — both are read off
      // the panel rather than mirrored into a field here, because two copies of
      // "which stage is this" is how the bar comes to say one thing while the
      // page shows another.
      onStage: () => {
        this.paintReadout();
        this.paintRevise();
      },
    });
    this.versionsButton = this.makeVersionsButton();
    // AFTER History, so the bar reads Instructions · History · + Instruction:
    // the two doors to what already exists, and then the verb that makes one.
    this.captureBtn = this.makeCaptureButton();
    // Paint the peer control's count on first load without opening History.
    // Fire-and-forget: this is the constructor, nothing is awaiting a
    // result, and a failure here means the History chip reads 0 until the
    // reviewer opens it — refresh()'s own bare `.catch(() => {})`
    // (versions.ts) already absorbs the error, so there is nothing left for
    // this call site to report either.
    void this.versionsPanel.refresh();
    // THE ARRIVAL. Phase 2's agent writes its revision INTO the document, so the
    // change lands under the reviewer's cursor with nothing pending to announce
    // it — the rail counts proposals and there are none. `seenRound` is what
    // this page has already been told about; null means "nothing yet", which is
    // NOT the same as 0 and is why it is not initialised to a number: a page
    // that opens onto a document with rounds already in it must adopt them
    // silently rather than announce the last one as news.
    this.seenRound = null;
    this.seenCannot = '';
    this.arrival = null;
    // Realtime is opt-in and the server owns the answer. Assuming `ask` here
    // and never asking would mean a reload silently dropping a live session
    // back to ask on screen while the server kept notifying — the toggle and
    // the behaviour disagreeing is worse than either state.
    this.mode = MODE_ASK;
    // Hold's state has to exist before makeMode paints the button.
    this.holding = false;
    this.held = new Set();
    this.heldArrivals = [];
    this.modeUI = this.makeMode();
    this.paintMode();
    this.readMode();

    // What arrived while the reviewer was reading. prevSuggestions is null
    // until the FIRST payload lands: with no baseline, every suggestion in the
    // document would read as an arrival and a reload would announce the whole
    // backlog.
    this.prevSuggestions = null;
    // Arrivals the reviewer has not been shown yet — what `show me` steps
    // through. Not the rail's card list: a card can be decided, scrolled past
    // or collapsed away without anyone having seen what landed.
    this.arrivalQueue = [];
    // What the CURRENTLY VISIBLE strip is about. Coalescing is tied to the
    // strip being on screen (handoff §4: "a second arrival WHILE THE STRIP IS
    // VISIBLE rewrites it"), so this resets once it has faded.
    this.stripBatch = [];
    this.newRuns = new Set();
    this.stripTimer = 0;
    this.newTimer = 0;
    this.strip = this.makeStrip();

    // Where the reviewer was, in coordinates a fragment rebuild cannot
    // destroy. See keepPlace.
    this.place = null;
    this.restoring = false;

    // R10. The window the button counts against belongs to the SERVER — it
    // outlives this page, so a reload mid-revision still shows the counter,
    // and a `galley revise` from a terminal shows it too.
    this.reviseWaiting = false;
    this.reviseRunning = false;
    this.approveNotBefore = 0;
    this.reviseStartedAt = 0;
    // The verdict on offer, and whether an approve has already landed. Revise
    // until the first /_galley/pending answers: it is the today-behaviour, and
    // the safe default while the census is still unknown — an Approve shown
    // against a document whose markup has not been read yet is exactly the
    // lost-markup failure the label exists to prevent.
    this.verdict = REVISE_IDLE;
    this.approved = false;
    // The verdict menu — built on the first press that needs it, because a
    // shell without the Revise button has nothing to hang it off.
    this.verdictMenu = null;
    this.verdictOpen = false;
    this.revise = this.makeRevise();

    // THE HANDOFF. While the agent holds the file the document is read-only
    // and the one control this page gains is the way back — a cancel in a
    // RESERVED box, so the window opening and closing moves nothing in the
    // bar. Read off /_galley/revise with the seal, painted on the same edge
    // discipline as readSeal.
    this.handoff = false;
    this.draftError = '';
    this.diskNotice = '';
    this.cancelBtn = this.makeHandoffCancel();

    // THE SEAL. Whether this review has ENDED, and how — read off
    // /_galley/revise on every poll, exactly where the button already asks
    // "what should I be showing?". Null until the first answer, so the page
    // never flashes a terminal bar it has not been told about.
    this.sealed = false;
    this.sealVerdict = '';
    this.sealAt = 0;
    this.sealEntrusted = 0;
    this.sealOutstanding = 0;
    this.sealLanding = 0;
    this.sealUI = this.makeSeal();
    this.paintSeal();

    // Last of the chrome, because it MEASURES the chrome: every control above
    // is in the bar by now, so the first reading is the real height rather
    // than the height of a bar half built.
    this.watchBar();

    // A REVISION'S RESULTS ARE ITS OWN CARDS AND NOTHING ELSE. There used to
    // be a second object here — `this.batch`, `this.batchStep` and a
    // `collecting` window that filled them, feeding a receipt card in the
    // anchorless block that listed the very edits the band was already
    // carding. Two things doing one function, in two visual languages, in two
    // containers. It is gone; what it offered is offered elsewhere and always
    // was — `✓ all` in the census strip decides in bulk over the same
    // population, `j`/`k` step through what arrived, and every card already
    // says `agent · just now`, which is the whole of the grouping claim the
    // receipt made.
    this.bar = this.makeBottomBar();
    this.sheet = this.makeSheet();
    // The two intents that light a card and its words. Watched here, once: the
    // listeners are delegated off the document because paintRail destroys every
    // card they could otherwise be bound to. There is no element to build —
    // the prose end is a DECORATION and the card end is the card's own class.
    this.litHover = '';
    this.litFocus = '';
    this.watchLit();
    this.refusalTimer = 0;
    this.refusalTick = 0;
    this.refusalPending = null;
    this.refusalShownAt = 0;

    this.bubble = new SuggestionUI({
      editor,
      pending: () => this.suggestions,
      // NO `post` AND NO `refresh` ANY MORE: the bubble's accept/reject pair is
      // deleted and nothing on that surface asks the server for anything.
      //
      // Below the breakpoint a tap on a highlight is the only way to reach the
      // conversation it is about — there is no rail. So the bubble is handed
      // the thread list and the ONE card component, by injection, exactly as it
      // is handed `pending`: suggestions.ts knows nothing about the App, and
      // entry.ts already imports it, so a module-level import there would be a
      // cycle.
      threads: () => this.comments,
      threadCard: (thread) => {
        if (!isThread(thread)) {
          // See isThread's own comment — unreachable in practice, since
          // every thread this callback receives came straight from
          // `this.comments` above.
          throw new Error('threadCard received a thread with no key');
        }
        return this.bubbleThreadCard(thread);
      },
      // railSurfaces itself, through the one accessor paintSurfaces also uses —
      // not a second spelling of the width test, and not the width test at all.
      // "Is the rail carrying conversations" is the question, and collapsed at
      // 1600px it is not, though the window is as wide as they come.
      railShown: () => this.surfaces().rail,
      frame: () => this.chromeFrame(),
      // AND WHERE THE RAIL IS CARRYING IT, THE CLICK GOES TO THE CARD. A
      // comment highlight used to open a bubble with two verbs and no
      // conversation at wide width — the most instinctive click in the product
      // landing on its worst surface. `reveal()` already does this in the other
      // direction (a card scrolls to its mark); this is the same gesture
      // returning. flashThreadCard is the one implementation, and it already
      // knows which of the rail and the sheet is on screen.
      onReveal: (thread) => {
        if (!isThread(thread)) {
          return;
        }
        this.flashThreadCard(thread.key);
      },
    });

    provider.on('status', (event) => {
      this.connection = event.status;
      this.paintReadout();
    });

    editor.on('selectionUpdate', ({ transaction }) => {
      // Moving the caret is the reader saying they got the message; a refusal
      // that outlives the edit it refused is just clutter. See dismissRefusal
      // for why this is not simply hideRefusal.
      this.dismissRefusal();
      // A REBUILD MOVES THE SELECTION WITHOUT THE REVIEWER: a server-side
      // mutation replaces the document, and keepPlace puts the caret back.
      // Neither is the reviewer choosing something new to comment on.
      this.placeComposerButton(
        !transaction.docChanged &&
          !transaction.getMeta(ySyncPluginKey) &&
          !transaction.getMeta(PLACE_META),
      );
    });
    // A CLICK INTO THE PROSE CLOSES A BLOCK GRIP'S EMPTY COMPOSER, even one
    // that lands where the caret already was and so changes no selection. A
    // selection made by that click places its own composer straight after.
    // One holding words stays (see holdsWords).
    editor.on('focus', () => {
      const c = this.composer;
      if (c.opener && !c.root.hidden && !holdsWords(c)) {
        this.hideComposer();
      }
    });
    editor.on('blur', () => {
      // Not on blur alone: clicking the button itself blurs the editor, and
      // the button's own mousedown handler cancels that. This only fires for
      // a click that landed somewhere else entirely.
      //
      // AND A DEFERRED DISMISSAL MUST BE CANCELLABLE, because the thing it is
      // deferred past can be the composer OPENING AGAIN. The check runs on a
      // zero timeout so `document.activeElement` has settled; a selection made
      // inside that window — Esc out of the field, then straight back into the
      // prose — places a FRESH composer, and the queued callback then finds
      // focus in `.ProseMirror` (not inside the composer, correctly) and hides
      // a popover that is about the reviewer's newest selection. The handle is
      // held so `placeComposerButton` can cancel it; nothing else in this file
      // may hide the composer from a timer without doing the same.
      window.clearTimeout(this.blurDismiss);
      this.blurDismiss = window.setTimeout(() => {
        this.blurDismiss = 0;
        if (
          !this.composer.root.contains(document.activeElement) &&
          !holdsWords(this.composer)
        ) {
          this.hideComposer();
        }
      }, 0);
    });

    // A card's position is a fact about where its mark is in the viewport, so
    // it has to be recomputed on everything that can move a mark relative to
    // it: the page scrolling, the window resizing, and the document changing
    // (which includes every suggestion arriving over the websocket).
    //
    // Coalesced into one animation frame rather than run per event: a scroll
    // fires these faster than a layout read can answer, and coordsAtPos forces
    // layout. `scroll` is captured so it catches a scrollable ancestor too.
    // `coalesce` in card.ts, which History's rail uses too — four lines, which
    // is exactly the size at which two copies drift without anyone noticing.
    this.scheduleAnchors = coalesce(() => this.paintAnchors());
    window.addEventListener('scroll', this.scheduleAnchors, {
      passive: true,
      capture: true,
    });
    // Where the READER put the page, as opposed to where a rebuild threw it.
    // See keepPlace: `restoring` is what tells the two apart.
    this.placeScrollY = window.scrollY;
    window.addEventListener(
      'scroll',
      () => {
        if (!this.restoring) {
          this.placeScrollY = window.scrollY;
        }
      },
      { passive: true },
    );
    editor.on('transaction', ({ transaction }) => this.keepPlace(transaction));
    window.addEventListener('resize', this.scheduleAnchors);
    // Crossing the breakpoint is a resize like any other, and there is exactly
    // one place that decides what a width means.
    window.addEventListener('resize', () => this.paintSurfaces());
    editor.on('update', () => this.scheduleAnchors());
    // A NodeView is rebuilt from scratch when its node changes, and takes
    // every pin on it with it. Re-fitting them is not layout, so it does not
    // belong in the rAF-throttled anchor pass — an update is
    // exactly when it is needed and never more often than that.
    editor.on('update', () => this.paintFigures());
    // The grips are reconciled against the document, so every change to it
    // asks for a paint; the coalesce makes a burst of changes one paint.
    editor.on('update', () => this.scheduleGrips());
    // A note's words are painted on every update, not only on a pending
    // refresh: every server-side mutation replaces the whole document (see
    // CLAUDE.md), and a note that arrives in that rebuild needs its words
    // before the next poll. Same shape as the figure NodeView's rebuild above.
    editor.on('update', () => this.paintNoteWords());
    document.addEventListener('keydown', (event) => this.onKey(event));

    // --- the right-click, and what it offers ---
    //
    // ONE GESTURE, TWO SCOPES, NO MODE. The scope is decided by the selection
    // the reviewer already made: text selected → an instruction on that
    // passage; nothing selected → an instruction on the whole document. That is
    // why there is no mode switch anywhere on this page — the answer to "which
    // scope" is already on screen before the menu opens.
    //
    // SCOPED TO THE PROSE, not to the page. A right-click on the bar, the rail
    // or the composer is the browser's own menu, because those are chrome and
    // the reviewer may genuinely want to copy out of them; the document is the
    // one surface where galley has something better to offer.
    // `preventDefault` is called ONLY where this menu opens, so nothing is
    // taken away where nothing is given.
    //
    // DELEGATED OFF `document`, in both directions, because `paintRail`
    // destroys and rebuilds every card in the rail on every poll — a listener
    // bound to an element the next paint replaces is a listener that stops
    // firing without an error. The menu itself is on `body` and outlives every
    // paint (web/menu.ts), so this pair is bound once.
    document.addEventListener('contextmenu', (event) => {
      const target = event.target;
      if (!(target instanceof Node) || !this.editor.view.dom.contains(target)) {
        return;
      }
      event.preventDefault();
      openMenu(
        this.menu,
        this.menuItems(),
        { x: event.clientX, y: event.clientY },
        // MEASURED per open. The bar folds, so the band this menu is clamped
        // into is not a constant — the reason `--gly-bar-h` exists at all.
        this.chromeFrame(),
      );
    });
    // A PRESS OFF AN EMPTY COMPOSER PUTS IT AWAY, a grip's and a selection's
    // alike: an open box nobody has typed in is not worth a second gesture to
    // dismiss. One holding words stays, so a stray press never costs them.
    // Captured, and on `pointerdown`, so the box is gone before whatever was
    // pressed acts: a press on another grip then opens that grip's box, as it
    // always has. The primary button only, so a right-click is still the
    // menu's. Not while a region is being dragged on a figure: that press is
    // the composer's own gesture.
    document.addEventListener(
      'pointerdown',
      (event) => {
        const c = this.composer;
        const target = event.target;
        if (
          event.button !== 0 ||
          c.root.hidden ||
          c.picking ||
          holdsWords(c) ||
          (target instanceof Node && c.root.contains(target))
        ) {
          return;
        }
        this.hideComposer();
      },
      { capture: true },
    );
    // Any press outside the menu puts it away. `pointerdown` and not `click`:
    // the menu must be gone before whatever was pressed acts, and a menu still
    // on screen over a selection the press just changed is stale by the time
    // `click` fires.
    document.addEventListener('pointerdown', (event) => {
      const target = event.target;
      if (
        menuOpen(this.menu) &&
        (!(target instanceof Node) || !this.menu.root.contains(target))
      ) {
        closeMenu(this.menu);
      }
    });
    // A menu positioned in PAGE coordinates keeps its place on the page while
    // the page moves under the pointer that opened it, so a scroll or a resize
    // retires it rather than letting it drift away from what it is about.
    window.addEventListener('scroll', () => closeMenu(this.menu), {
      passive: true,
      capture: true,
    });
    window.addEventListener('resize', () => closeMenu(this.menu));

    // After scheduleAnchors exists: paintSurfaces re-measures when the rail
    // comes back, and a rail painted while hidden measured every card at zero.
    this.paintSurfaces();
    // Fire-and-forget: the first paint of the pending view on page load. A
    // failure here leaves the rail empty until the next `tick()` poll
    // (pending.ts) succeeds — no proposal is lost, only its first paint is
    // late — and the constructor has nothing to await it into.
    void this.refreshPending();
    this.paintReadout();
    this.readRevise();
    window.setInterval(() => this.tick(), POLL_MS);
    // The counter's own beat. The server is polled at POLL_MS and hands back
    // how long it has been; this only re-renders the seconds between polls, so
    // the number climbs smoothly instead of jumping in 1.5s steps.
    window.setInterval(() => this.paintRevise(), 1000);
  }

  // --- where a suggestion is ---

  // runsNow is markRuns, memoised on the document it read. ProseMirror
  // documents are immutable, so identity is an exact cache key — and a scroll
  // handler that re-walked the whole document every frame would be the one
  // place this panel could make typing feel slow.
  runsNow() {
    const doc = this.editor.state.doc;
    if (this.runsDoc !== doc) {
      this.runsDoc = doc;
      this.runs = markRuns(doc);
    }
    return this.runs;
  }

  // --- lighting the text: which words is this card about ---
  //
  // ONLY WHILE SOMEBODY IS ASKING. The question *which words is this about*
  // only exists while it is being asked, so the light is on hover or keyboard
  // focus, from EITHER END, and at rest nothing on the page is lit.
  //
  // THE LINE IS DELETED, NOT RESTYLED, and lit.ts's header carries the whole
  // measurement. In one sentence: the connector was invented to bridge the gap
  // when a card had drifted from its mark and was legible only because it was
  // long; once cards sat beside their text it became a 26px stub across a 376px
  // gutter, and aimed at the card's middle it sagged 250px diagonally through
  // the paragraph being read. A gutter that wide cannot be crossed by a line
  // that is not the loudest thing on the page.
  //
  // TWO ENDS, ONE STATE, AND THAT IS WHAT MAKES IT SYMMETRIC BY CONSTRUCTION.
  // `litHover`/`litFocus` resolve to ONE run and both ends are painted from it,
  // so hovering the card and hovering the words cannot come to disagree about
  // what they are about — the same argument `litRunAt` is one function for.

  // DELEGATED, because `paintRail` destroys and rebuilds every card on every
  // pending refresh — a listener bound to a card is a listener bound to an
  // element that will not exist in 1.5 seconds. The document is the one node
  // that outlives every rebuild.
  //
  // TWO SOURCES, HELD APART. Hover and focus are different intents and either
  // may be live while the other is not: a card focused from the keyboard must
  // not go dark because the pointer happened to move across the page. Hover
  // wins when both are set, because the pointer is the more recent act.
  watchLit() {
    const on = (source: 'litHover' | 'litFocus', node: EventTarget | null) => {
      const run = this.litRunAt(node);
      if (this[source] === run) {
        return;
      }
      this[source] = run;
      this.paintLit();
    };
    document.addEventListener('mouseover', (e) => on('litHover', e.target));
    // relatedTarget is where the pointer WENT. Leaving the window entirely
    // gives null, which reads as "nothing", which is the right answer.
    //
    // A DETACHED TARGET IS NOT A DEPARTURE, and that clause is what carries the
    // light across a repaint. Chrome fires `mouseout` with a null relatedTarget
    // when the hovered element is REMOVED, and `paintRail` removes every card
    // on every pending refresh — including the refresh that follows somebody
    // else's `galley suggest`. Without this the light went out on a mutation
    // the reviewer did not make, one frame before `paintLit` put the class back
    // on the new card, and the two disagreed until the pointer moved. Moving
    // the pointer somewhere real still fires `mouseover` on whatever is under
    // it, so nothing is left stuck on.
    document.addEventListener('mouseout', (e) => {
      if (e.target instanceof Node && e.target.isConnected === false) {
        return;
      }
      on('litHover', e.relatedTarget);
    });
    document.addEventListener('focusin', (e) => on('litFocus', e.target));
    document.addEventListener('focusout', (e) =>
      on('litFocus', e.relatedTarget),
    );
  }

  // litRunAt says which CARD a node belongs to, from EITHER END: a card in the
  // band, or a mark in the prose. One method, so hovering the word and hovering
  // the card cannot come to disagree about what they are about.
  //
  // A SUBSTITUTION IS ADDRESSABLE BY EITHER HALF, and the green half's run is
  // not the card's. `{~~brown~>red~~}` is one span in the file and one decision
  // everywhere, but in the fragment it is a Del and an Ins with two runs, and
  // the wire's `run` is the DELETED half's — deliberately, and for reasons
  // stated in suggest.substitutionSpan. So the green half is normalised through
  // `insRun`, the other half's address, published by the side that computed the
  // pairing. Reading it is not re-deriving the pairing; that rule is
  // `markdown.planSubstitution`'s and stays there. Without it, hovering the
  // words galley is proposing to ADD lit them and lit no card at all.
  //
  // Anything else answers '' — which is what makes moving the pointer onto the
  // page background put the light out, with no separate path to keep in step.
  litRunAt(node: EventTarget | null): string {
    const domNode = node instanceof Node ? node : null;
    const el: Element | null =
      domNode instanceof Element
        ? domNode
        : domNode
          ? domNode.parentElement
          : null;
    if (!el) {
      return '';
    }
    const card = el.closest<HTMLElement>('.gly-rail-band .gly-card[data-run]');
    if (card) {
      return card.dataset.run || '';
    }
    const mark = el.closest('.ProseMirror [data-run]');
    const run = mark ? mark.getAttribute('data-run') || '' : '';
    if (!run) {
      return '';
    }
    const paired = this.suggestions.find((s) => s.insRun && s.insRun === run);
    return paired ? paired.run || run : run;
  }

  // litRuns is every run the lit card covers: its own, and — for a
  // substitution — the inserted half's, so BOTH halves of one decision light
  // together. One span in the file is one decision everywhere, and a light on
  // half of it would be the third reading of one span this codebase has already
  // paid for once.
  //
  // ONE REFUSAL, WHERE THERE WERE FOUR. `connectorGeometryFor` stated four —
  // no rail on screen, no mark in the document, no card in the band, an adrift
  // card — because a LINE has two ends and either can be missing. A light has
  // one: the words say *the card over there is about these*, so the only thing
  // that has to be true is that the card is there. The rail collapsed and the
  // sheet open both take the band's cards off the page, an adrift card is
  // adrift precisely because its mark is NOT in the document (so `markRuns`
  // finds nothing to light and the old fourth clause is moot), and a thread
  // with no card in the band was never in this query's answer.
  litRuns(run: string): string[] {
    if (!run) {
      return [];
    }
    const card = this.rail.band.querySelector(
      `.gly-card[data-run="${cssEscape(run)}"]`,
    );
    if (!card) {
      return [];
    }
    const s = this.suggestions.find((x) => x.run === run);
    return s && s.insRun ? [run, s.insRun] : [run];
  }

  // paintLit writes both ends from the one state.
  //
  // IT IS CALLED AT THE END OF EVERY REPAINT, and that is not belt and braces.
  // The card's class is on an element `paintRail` has just destroyed and built
  // again, so it lives on the App — the same rule this codebase already applies
  // to the delete control's armed flag — and this is where it is put back. The
  // prose's half needs no such help and that is the point of it being a
  // decoration: ProseMirror re-derives it on every redraw instead of wiping it.
  paintLit() {
    const run = this.litHover || this.litFocus;
    const runs = this.litRuns(run);
    // The card end. Its own element, so a class here is ours to write.
    for (const card of this.rail.band.querySelectorAll<HTMLElement>(
      '.gly-card',
    )) {
      card.classList.toggle(
        'gly-lit',
        runs.length > 0 && card.dataset.run === run,
      );
    }
    // The prose end. NEVER a class on a mark — see lit.ts.
    const view = this.editor && this.editor.view;
    if (!view) {
      return;
    }
    const now = litKey.getState(view.state);
    if (now && sameRuns(now.runs, runs)) {
      return;
    }
    view.dispatch(view.state.tr.setMeta(litKey, runs));
  }

  // reveal scrolls a card's mark into view and rings it. Binding by run rather
  // than by text is what makes this exact for two suggestions that read the
  // same: there is nothing to disambiguate and nothing to report as ambiguous.
  reveal(run: string, el: HTMLElement) {
    // Asking a full-screen list to show you something and then having to
    // dismiss it by hand is a tap wasted.
    if (this.sheetOpen) {
      this.closeSheet();
    }
    // The same lookup paintAnchors uses, for the same reason: a card whose mark
    // the fragment has not yet been given an id for still has somewhere to jump.
    const card = this.cards.find((c) => c.el === el);
    // CardEntry carries no `suggestion` field (nothing ever puts one there —
    // see appshell.ts's own CardEntry comment), so the fallback object is
    // built explicitly here rather than spread from `card`, which runFor's
    // declared parameter type requires anyway.
    const found = runFor(
      this.runsNow(),
      card ? { run: card.run, suggestion: null } : { run, suggestion: null },
    );
    if (!found) {
      anchorNote(el).textContent =
        'not in the document yet — it lands on the next sync';
      return;
    }
    const mark = markElement(this.editor.view, found);
    if (!mark) {
      anchorNote(el).textContent = 'cannot locate it on the page';
      return;
    }
    anchorNote(el).textContent = '';
    revealMark(mark);
    // The scroll moves every anchor, so every card's position is now stale —
    // and a smooth scroll is still running after this frame returns.
    this.scheduleAnchors();
    window.setTimeout(() => this.paintAnchors(), 450);
  }

  // --- keeping the reviewer's place across a remote rebuild ---
  //
  // EVERY SERVER-SIDE MUTATION REPLACES THE WHOLE DOCUMENT. `ydoc.Load` deletes
  // the fragment's children and writes them again (internal/ydoc/bridge.go), so
  // an accept, a reject, a reply, and every suggestion an agent makes all reach
  // this browser as "the document you had is gone, here is another one".
  // y-prosemirror then has nothing to map the caret through: the item its
  // relative position was anchored to no longer exists.
  //
  // MEASURED, NOT INFERRED. Caret at position 90 of a 4079-position document,
  // one `galley suggest` from another terminal: the selection moved to 4103 —
  // the very end — and the window went from scrollY 0 to 2067. Mid-sentence,
  // unasked, with the editor focused the whole time.
  //
  // That is exactly the constraint this phase's whole arrival surface exists to
  // protect, and it was already false underneath it. THIS IS A MITIGATION, NOT
  // THE FIX. The fix is to stop rebuilding the fragment — phase 3's targeted
  // mutation, which CLAUDE.md says in so many words not to attempt earlier —
  // and when that lands, this becomes a no-op and should be deleted.
  //
  // The coordinates are the caret's TOP-LEVEL BLOCK INDEX and the text of that
  // block up to the caret. Both survive a rebuild that changed some other
  // block, which is what an agent's suggestion is. Neither is exact when the
  // reviewer's OWN block is the one that changed — and in that case the mapped
  // position is left alone rather than guessed at, because a caret put back in
  // the wrong place is worse than one the reviewer can see has moved.
  //
  // A SELECTION IS TWO OF THOSE PLUS A DIRECTION, and it is restored as one —
  // a range is not merely a caret with a decoration, it is the input strike and
  // the comment composer read. See placeOf and spanIn.
  keepPlace(tr: Transaction) {
    // y-prosemirror stamps every transaction it creates from a remote update,
    // which is the same discriminator suggestions.ts uses to tell a reviewer's
    // keystroke from a change arriving over the websocket.
    if (!tr.getMeta(ySyncPluginKey)) {
      if (!tr.getMeta(PLACE_META)) {
        this.rememberPlace();
      }
      return;
    }
    if (!tr.docChanged) {
      return;
    }
    // Set synchronously, before the browser can dispatch the scroll event the
    // rebuild is about to cause — scroll events fire at the next rendering
    // opportunity, so placeScrollY still holds the reader's own value here.
    this.restoring = true;
    this.restorePlace(this.placeScrollY);
  }

  rememberPlace() {
    this.place = placeOf(this.editor.state);
  }

  restorePlace(wantScrollY: number) {
    const view = this.editor.view;
    const focused = view.hasFocus();
    const span = focused ? this.findSpan() : null;
    if (span !== null && !this.spanIsShown(span)) {
      // Anchor and head, not from and to: a range swept right-to-left has its
      // head at the START, and putting it back forwards moves the caret to the
      // far end of the phrase — which is where the next keystroke goes.
      const $anchor = view.state.doc.resolve(span.back ? span.to : span.from);
      const $head = view.state.doc.resolve(span.back ? span.from : span.to);
      const tr = view.state.tr.setSelection(
        TextSelection.between($anchor, $head),
      );
      // Marked so keepPlace does not treat our own correction as the reviewer
      // moving the caret, which would record the wrong place for next time.
      tr.setMeta(PLACE_META, true);
      view.dispatch(tr);
    }

    // A range that came back as a caret is a PARTIAL refusal, and it is
    // silent in exactly the way the whole refusal was: the reviewer's phrase
    // is no longer selected, the caret is somewhere sensible, and the next
    // strike or comment operates on nothing. Say so.
    if (
      focused &&
      this.place !== null &&
      this.place.end !== null &&
      span !== null &&
      span.from === span.to
    ) {
      // `the agent`, not `claude` — the party has ONE name on this screen, and
      // this sentence is about whoever's server-side mutation replaced the
      // document, which this page cannot name and must not guess. See
      // arrivals.ts's ARRIVAL_AGENT.
      this.showStrip(
        `${ARRIVAL_AGENT} edited inside your selection — only your cursor was put back`,
      );
    }

    // THE REFUSAL MUST NOT BE SILENT.
    //
    // findSpan returning null is deliberate — the block the caret was in no
    // longer starts with the text it did, so there is no honest way to locate
    // it, and guessing is worse than not guessing. But restoring the SCROLL
    // after refusing the caret is what made this dangerous: the page looked
    // untouched while the selection sat at the end of the document, off
    // screen, and the next keystroke landed a thousand positions from where
    // the reviewer was looking. Measured: caret at 203 of 1349, one
    // `galley suggest` into that same paragraph, three characters typed, and
    // they appeared in the last block of the document with nothing on screen
    // having moved.
    //
    // Before the mitigation existed the page jumped and the reviewer knew
    // something had happened. So when the caret cannot be placed, the scroll
    // is left where the rebuild put it and the strip says why. The reviewer
    // gets the signal back, and now also gets a sentence.
    const lost = focused && this.place !== null && span === null;
    if (lost) {
      this.showStrip(
        `${ARRIVAL_AGENT} edited the paragraph you were in — your cursor moved`,
      );
      this.restoring = false;
      this.placeScrollY = window.scrollY;
      return;
    }

    const put = () => {
      if (window.scrollY !== wantScrollY) {
        window.scrollTo(0, wantScrollY);
      }
    };
    put();
    // Again next frame: the rebuild's own scroll can land after this handler
    // returns, and putting it back once is not enough if it has not happened
    // yet.
    window.requestAnimationFrame(() => {
      put();
      this.restoring = false;
      this.placeScrollY = window.scrollY;
    });
  }

  // findSpan turns a remembered place back into the selection it named in the
  // document that just arrived, or null when neither end can be found
  // honestly. A collapsed caret comes back as a zero-width span.
  findSpan() {
    return spanIn(this.editor.state.doc, this.place);
  }

  // Whether the selection the editor already has IS the one findSpan wants —
  // direction included, since dispatching a same-span selection the other way
  // round would move the caret from one end of the phrase to the other.
  spanIsShown(span: Span) {
    const sel = this.editor.view.state.selection;
    return (
      sel.from === span.from &&
      sel.to === span.to &&
      sel.anchor > sel.head === span.back
    );
  }

  // sectionFor names the heading an arrival landed under, for the strip's
  // sentence. Top-level children only: a heading nested inside a list item or a
  // blockquote is not what a reader means by "the section this is in".
  sectionFor(run: SuggestionRun | null): string {
    if (!run) {
      return '';
    }
    let section = '';
    this.editor.state.doc.forEach((node, offset) => {
      if (offset < run.from && node.type.name === 'heading') {
        section = node.textContent;
      }
    });
    return section;
  }

  makeStrip() {
    const root = document.createElement('div');
    root.className = 'gly-strip';
    root.hidden = true;
    // The strip is the only notice an off-screen arrival gets, so it has to
    // reach a screen reader too. polite, never assertive: interrupting what is
    // being read is the same discourtesy as moving the caret.
    root.setAttribute('role', 'status');
    root.setAttribute('aria-live', 'polite');

    const text = document.createElement('span');
    text.className = 'gly-strip-text';

    // THE ONE FILLED VERB ON THE STRIP, and it is a DEEP LINK rather than a
    // scroll. Board 1e: the question a reviewer has the instant a round lands
    // is not "where is it" but "what did it do to my document", and the only
    // surface that answers that is the round's own reading state. This lands
    // there — `showRound(n, 'inplace')`, through toggleVersions' arrival branch
    // so there is ONE spelling of the deep link — not on History's landing,
    // which answers the other question.
    const read = document.createElement('button');
    read.type = 'button';
    read.className = 'gly-strip-read';
    read.textContent = 'read changes';
    read.title = 'open this round in History, on what it changed';
    read.addEventListener('click', () => this.readChanges());

    const show = document.createElement('button');
    show.type = 'button';
    show.className = 'gly-strip-show';
    show.textContent = 'show me';
    show.title =
      'scroll to what arrived — the only thing on this strip that moves the page';
    show.addEventListener('click', () => this.showMe());

    // THE QUIET WAY OUT, and it takes nothing with it. Dismissing is "not now",
    // never "seen" — the History chip stays amber until the changes are
    // actually READ, because a notice a reviewer waved away is not a round they
    // looked at, and the amber is the only durable trace the arrival leaves.
    const dismiss = document.createElement('button');
    dismiss.type = 'button';
    dismiss.className = 'gly-strip-dismiss';
    dismiss.textContent = 'dismiss';
    dismiss.title = 'put this away — History stays marked until you read it';
    dismiss.addEventListener('click', () => this.hideStrip());

    // Handoff §11, verbatim. It says both that this notice is about to go and
    // that the fact survives it, which is what makes fading acceptable.
    const fade = document.createElement('span');
    fade.className = 'gly-strip-fade';
    fade.textContent = ARRIVAL_SUFFIX;

    root.append(text, read, show, dismiss, fade);
    document.body.appendChild(root);
    return { root, text, read, show, dismiss, fade };
  }

  // showStrip has TWO CALLERS SAYING TWO DIFFERENT THINGS, and the difference
  // is which verb the strip offers and whether it fades.
  //
  // An ARRIVED ROUND (`round`, board 1e) is a durable fact with somewhere to
  // go: `read changes` and `dismiss`, and NO TIMER. The 8-second fade was
  // designed for a strip with no way out and a count that kept the fact for it;
  // this one HAS a way out, and what it announces is not "three edits went past
  // while you were reading" but "the round you sent came back", which is still
  // worth acting on after the paragraph you are in the middle of.
  //
  // Anything else keeps the old shape — `show me` and the fading suffix.
  //
  // The verbs are switched with `hidden` and not with visibility: nothing here
  // is reserving a box (the strip is absent entirely between arrivals, so there
  // is no neighbour to slide), and `hidden` takes the unused verb out of the
  // TAB ORDER as well as out of the paint. A button hidden in CSS is still a
  // button to the keyboard — this codebase learned that from the fold clusters
  // and pays for it every time it forgets.
  showStrip(message: string, round: Arrival | null = null) {
    if (!message) {
      return;
    }
    this.strip.text.textContent = message;
    this.strip.read.hidden = !round;
    this.strip.dismiss.hidden = !round;
    this.strip.show.hidden = !!round;
    this.strip.fade.hidden = !!round;
    this.strip.root.hidden = false;
    if (this.stripTimer) {
      window.clearTimeout(this.stripTimer);
      this.stripTimer = 0;
    }
    if (!round) {
      this.stripTimer = window.setTimeout(
        () => this.hideStrip(),
        ARRIVAL_FADE_MS,
      );
    }
  }

  // readChanges is the strip's filled verb. It hides the notice and hands the
  // gesture to toggleVersions, which already owns the arrival deep link — the
  // strip does not get a second copy of `showRound(n, 'inplace')`, because two
  // spellings of one landing is how the two come to disagree about which round
  // an arrival opens.
  readChanges() {
    this.hideStrip();
    this.toggleVersions();
  }

  hideStrip() {
    if (this.stripTimer) {
      window.clearTimeout(this.stripTimer);
      this.stripTimer = 0;
    }
    this.strip.root.hidden = true;
    // The batch is what THIS strip was about. The queue is not cleared: the
    // arrivals are still unseen, and `show me` is still how they are reached
    // once the strip has gone — through the card, or j/k, or the sheet.
    this.stripBatch = [];
  }

  // showMe is the ONE thing on this surface that moves the page, and it moves
  // it because the reviewer asked. Repeat clicks step through the queue.
  showMe() {
    const step = nextArrival(this.arrivalQueue, this.suggestions);
    this.arrivalQueue = step.queue;
    if (!step.arrival) {
      this.hideStrip();
      return;
    }
    const arrival = step.arrival;
    if (!arrival.run) {
      // `nextArrival` only ever returns an entry it found in `live`
      // (`arrivals.ts`'s own Set of defined run strings), so this is
      // unreachable in practice — narrowed rather than trusted, since
      // ArrivalItem's `run` is genuinely optional at the type.
      this.hideStrip();
      return;
    }
    const card = this.cards.find((c) => c.run === arrival.run);
    this.reveal(arrival.run, card ? card.el : this.strip.root);
    if (this.arrivalQueue.length === 0) {
      this.hideStrip();
    }
  }

  // The count pulsing is how an arrival reaches someone whose eyes are in the
  // document: a number that changed is the smallest possible interruption, and
  // unlike the strip it is telling the truth for as long as it stands.
  //
  // BOTH COUNTS, because only one of them is ever on screen. The strip's count
  // is hidden below the breakpoint — it is the same string the bottom bar
  // already prints and the strip needed the width back (see editor.css's
  // narrow block) — so pulsing only that one left the arrival's single
  // permanent trace invisible on exactly the layout that has no rail to watch
  // it land in. The hidden one takes the class harmlessly; the visible one
  // shows it. Which is which is the stylesheet's business, not this method's.
  pulseCensus() {
    for (const el of [this.census.count, this.bar && this.bar.count]) {
      if (!el) {
        continue;
      }
      el.classList.remove('gly-pulse');
      void el.offsetWidth;
      el.classList.add('gly-pulse');
      window.setTimeout(() => el.classList.remove('gly-pulse'), 900);
    }
  }

  // The "new" badge is a reading aid, not a state: it says "this is the one
  // that just landed" and then gets out of the way on the same clock the strip
  // fades on, so a rail left open overnight is not a wall of amber.
  clearNewLater() {
    if (this.newTimer) {
      window.clearTimeout(this.newTimer);
    }
    this.newTimer = window.setTimeout(() => {
      this.newTimer = 0;
      if (this.newRuns.size === 0) {
        return;
      }
      this.newRuns.clear();
      this.paintRail();
    }, ARRIVAL_FADE_MS);
  }

  // pressReopen AND pressDone ARE DELETED WITH THE BUTTONS THAT CALLED THEM —
  // see makeSeal for the whole argument, and for the one asymmetry between the
  // two verbs that is worth knowing before either comes back.

  // --- the notes rail ---

  // makeRail builds the column: the map, and a notice under it.
  //
  // THE RAIL HOLDS LIVE WORK ONLY, and that is the whole of its job: *here is
  // what needs you, beside the text it is about*. Nothing else lives here.
  //
  //   .gly-rail-band    anchored cards, at their marks
  //   .gly-rail-notice  one sentence when the map is empty for a reason
  //
  // THREE SECTIONS ARE GONE AND EACH LEFT FOR ITS OWN REASON.
  //
  //   `.gly-rail-settled` — finished conversations. Not live work, and beside
  //   nothing. They moved to the sheet first, and that region is gone too:
  //   nothing resolves an instruction any more, so there is nothing settled to
  //   show.
  //
  //   `.gly-rail-changed` — the trail's log. Deleted outright rather than
  //   moved, because the trail is an OUTGOING MESSAGE TO THE AGENT and not a
  //   history: nobody browses it, so no surface anywhere holds one. The count
  //   rides the button that sends it. See rail.ts's outgoingCounts, which
  //   carries the argument in full, and answer the question it poses there
  //   (WHEN would the reviewer open it) before proposing a drawer again.
  //
  //   `.gly-rail-anchorless` — the old loose container is gone, but its live
  //   work is not. Threads whose original mark disappeared render as complete
  //   cards in `.gly-rail-unplaced` inside the notice flow, preserving delete
  //   without pretending they can be positioned beside prose.
  //
  // Court, on the four sections it used to have: *"why does the settled/changed
  // float with? why is there a settled vs changed in the first place? what's
  // the difference?"* — settled is finished conversations and changed was the
  // reviewer's own edits, two unlike things in one grey costume, and if the
  // reviewer cannot tell them apart the presentation has already failed.
  //
  // See docs/superpowers/specs/2026-08-16-the-rail-holds-live-work.md.
  makeRail() {
    const root = document.createElement('aside');
    root.className = 'gly-rail';
    root.id = 'gly-rail';
    // The band is the positioned context every anchored card lives in, and it
    // is as tall as the cards it holds (paintAnchors writes that height —
    // absolutely positioned children contribute none of their own). Its
    // coordinates are the DOCUMENT's, less the band's own top; nothing below
    // reads window.scrollY, because paintAnchors has already added it.
    const band = document.createElement('div');
    band.className = 'gly-rail-band';
    // The flow below the positioned map holds empty-state copy and, when an
    // instruction has lost its mark, a small section of complete cards. Those
    // cards receive the same 26px gutter as the positioned band; without it the
    // old loose container measured 1280/304 against 1306/278 for every other
    // card and made one rail speak two visual languages.
    const notice = document.createElement('div');
    notice.className = 'gly-rail-notice';
    root.append(band, notice);
    document.body.appendChild(root);
    return { root, notice, band };
  }

  // --- the trail: the changed region, and the save that persists it ---

  trailEntries() {
    const st = trailPluginKey.getState(this.editor.state);
    return st ? st.entries : [];
  }

  // THE TRAIL IS NEVER POSTED, AND THAT IS THE FIX RATHER THAN A SUBTRACTION.
  //
  // `adoptTrail`, `applyServerTrail`, `readoptTrail`, `scheduleTrailSync` and
  // `syncTrail` are DELETED. Together they were a save loop: every editor
  // update scheduled a 600ms-debounced `POST /_galley/trail`, `tick()` re-armed
  // it on every 1.5s poll as a retry driver, the epoch token guarded a 409
  // re-adoption, and `trailPosted` advanced ONLY on `res.ok`. There is no
  // `/_galley/trail` route on the EditServer — `pendingView` is
  // `{instructions, blocks}`, with no `changes` and no `trailEpoch` — so every
  // one of those requests 404ed, the baseline stayed dirty, and the loop
  // re-sent the whole trail for as long as the tab stayed open. Nothing was
  // ever persisted; the retry was the only thing that worked.
  //
  // AND THE TRAIL LOSES NOTHING BY IT, which is why this is a deletion and not
  // a missing endpoint to build. The trail is an OUTGOING MESSAGE rather than a
  // history (CLAUDE.md, 2026-08-15): its audience is the agent, the ghosts in
  // the prose are its visible form at the moment of typing, and the round the
  // agent is handed is what carries it — deliberately no browsable surface
  // anywhere, and deliberately no stored copy for one to be drawn from. A
  // reviewer's typing IS the document; git has it the moment the export
  // debounce fires. What the server would have stored, nothing read: the
  // verdict button's count comes off `pendingCount`, and `trailEntries()` — the
  // plugin's own live list — is what paints the ghosts.
  //
  // `trailEntries` and `clearTrail` stay: the plugin is live, its ghosts are
  // live, and a verdict still has to wipe them.

  // clearTrail is the verdict's browser half: the ghosts and highlights must
  // not outlive the review they were the record of. The debounce, the epoch
  // bump and the baseline reset that used to stand here went with the save —
  // there is nothing in flight to be refused and nothing stored to be retired,
  // so wiping the plugin's list is the whole of it.
  clearTrail() {
    const view = this.editor.view;
    view.dispatch(view.state.tr.setMeta(trailPluginKey, { clear: true }));
    this.paintRevise();
  }

  // THE CHANGED REGION'S PAINTERS ARE GONE, AND SO IS THE LOG THEY DREW.
  //
  // `setChangedOpen`, `applyChangedOpen`, `paintChanged`, `paintSheetChanged`,
  // `fillChangedList` and `changeRow` rendered the trail as a collapsible,
  // counted list of `old → new` rows at the foot of the rail and again at the
  // foot of the sheet. They are deleted because THE TRAIL IS NOT A HISTORY:
  // its audience is the AGENT (cmd/galley/agentprompt.go tells the paired
  // session that the pending view's `changes` are the reviewer's own direct
  // edits), so the ghosts in the prose are the visible form of an OUTGOING
  // MESSAGE. There are exactly two moments anyone wants it — just after typing
  // (*did that land*, answered by the ghost) and just before sending (*what am
  // I about to tell the agent*, answered by the count on the verdict button) —
  // and after that git has it and the ledger has it.
  //
  // What went with them, and why each is a deletion rather than a relocation:
  //
  //   `changeRow`'s red/green — the one place in the product where red did NOT
  //   mean "waiting on you", excused because the rows sat under a head that
  //   said whose hand they were. With no head and no rows there is no exemption
  //   to keep — and no replace card left to have taught the vocabulary either.
  //   `suggestionCard` went with the proposals, so `.gly-quote-del` and
  //   `.gly-quote-ins` are deleted from web/editor.css too; the prose's own
  //   marks and History's diff are where removal is read now.
  //
  //   `partitionEntries` (web/trail.ts) — anchored rows first, then adrift ones
  //   which rendered "HERE AND ONLY HERE". The log was its only caller, so the
  //   distinction has no surface left to be drawn on. It is not lost: an entry
  //   that could not be re-anchored simply draws no ghost, which `trailDecorations`
  //   already decides, and `review.Change.Placed` still carries the fact to the
  //   server.
  //
  // See rail.ts's outgoingCounts, and answer the question it poses there before
  // proposing any of this again.

  // paintNoteWords hands the block notes their words. A note in the
  // document carries only its comment's ID; the words are the instruction's,
  // and they reach the amber box by that ID alone (see note.ts). A
  // whole-document instruction never has a note, and stays in the overall
  // panel.
  //
  // It goes in as transaction META and comes out as a DECORATION: anything
  // appended inside .ProseMirror is content, and ProseMirror redraws its own
  // elements after every server-side mutation.
  //
  // The dispatch is guarded by a comparison, and that guard is load-bearing —
  // this runs on every editor update, and an unconditional dispatch would be a
  // transaction per update forever.
  paintNoteWords() {
    const view = this.editor && this.editor.view;
    if (!view) {
      return;
    }
    const words: Record<string, string> = {};
    for (const thread of this.comments) {
      if (thread.anchor === 'block' && thread.entries[0]) {
        words[thread.key] = thread.entries[0].text || '';
      }
    }
    const now = noteWordsKey.getState(view.state);
    if (now && sameWords(now.words, words)) {
      return;
    }
    view.dispatch(view.state.tr.setMeta(noteWordsKey, words));
  }

  // --- where the chrome ends ---
  //
  // The whole-document panel hangs off the BOTTOM OF THE BAR and is `fixed`,
  // so it needs a number for where that is. The bar is sticky at top 0, so its
  // own height is exactly that number — and it is not a constant: the status
  // line wraps on a narrow window, the census strip drops its sentence at
  // 1200px, and a reviewer's font size is not this stylesheet's business. So
  // it is MEASURED and published as `--gly-bar-h`, the same way a card's
  // height is measured rather than assumed (see cardSizes).
  //
  // This observer cannot feed itself. It writes a custom property that only
  // the panel reads, and the panel is out of flow — its height can never
  // change the bar's, which is the loop that would otherwise be waiting here.
  // chromeFrame is the band of the window the PROSE is readable in, in viewport
  // coordinates — which is not the window. The top bar is sticky at 0 and the
  // narrow layout's bottom bar is fixed at the foot, so anything floating that
  // clamps to the window is drawn over one of them. Measured with a
  // conversation covering `✓ all`, the since-retired `✗ all` and the whole-doc
  // handle in a short window; the bubble asks this instead (see
  // SuggestionUI.topFor).
  //
  // MEASURED, not read off --gly-bar-h. That property is this side's PUBLICATION
  // of the same number for the stylesheet's use, and reading it back would make
  // a consumer of a value that only exists to be written. The bottom bar is
  // absent above the breakpoint, and `hidden` is exactly what paintSurfaces
  // toggles, so its height is 0 there without a width test here.
  chromeFrame() {
    const view =
      document.documentElement.clientHeight || window.innerHeight || 0;
    const head = document.querySelector('.gly-bar');
    const foot = this.bar && this.bar.root;
    const top = head ? head.getBoundingClientRect().height : 0;
    const bottom =
      foot && !foot.hidden ? foot.getBoundingClientRect().height : 0;
    return { top, bottom: view - bottom };
  }

  watchBar() {
    const bar = document.querySelector('.gly-bar');
    if (!bar) {
      return;
    }
    const publish = () => {
      const h = bar.getBoundingClientRect().height;
      if (h > 0) {
        document.documentElement.style.setProperty('--gly-bar-h', `${h}px`);
      }
    };
    publish();
    if (typeof window.ResizeObserver === 'function') {
      this.barSize = new window.ResizeObserver(publish);
      this.barSize.observe(bar);
      return;
    }
    // No ResizeObserver: a resize is the only thing left that changes the
    // bar's height, and the fallback in the stylesheet covers the rest.
    window.addEventListener('resize', publish);
  }

  // --- what the reviewer was typing, across a rebuild ---
  //
  // Every surface this walks is emptied and rebuilt by paintRail, which runs on
  // every poll and every mutation. draftRoots is the list of places a reply can
  // be half-written when that happens: the RAIL (its root, so the band is
  // walked), the overall card, whose entries are thread cards with reply boxes
  // of their own, and the SHEET, which was the fourth and was missing.
  //
  // It is a LIST, so it goes stale the moment the one card component renders on
  // a surface nobody added here — measured, a thirty-five character reply typed
  // into a sheet thread card came back `''` with focus on `BODY`, while the
  // same gesture in the rail survived in the same run.
  //
  // TWO POPULATIONS CHANGED SURFACE AND THE LIST DID NOT HAVE TO, WHICH IS THE
  // CASE IT HAS GONE STALE ON BEFORE. Settled conversations and anchorless ones
  // used to render inside `this.rail.root` (the settled list and the anchorless
  // section) and they render in the SHEET now — every one of them a thread card
  // with a `.gly-thread-reply` box and a `data-draft` key. Both the surface they
  // left and the surface they arrived at are already here, so no reply box has
  // become unwalkable. That is worth stating rather than trusting, because this
  // list's whole failure mode is a card moving to a root nobody added: had the
  // settled cards moved to a fifth surface instead, this comment would be the
  // change that was missed.
  //
  // THE SHEET GOES FIRST, AND ONLY WHILE IT IS OPEN, because it is the surface
  // in this list that COLLIDES with the others. It renders `railThreads ∪
  // overallThreads` plus the settled region, so its reply boxes carry exactly
  // the `data-draft` keys the rail's and the panel's do — and both
  // `carryDrafts` and `restoreDrafts` take the FIRST field under a key. First
  // therefore has to mean "the one the reviewer can actually type into", which
  // the sheet is whenever it is up: it is `inset: 0` at `z-index: 45`, and
  // `railSurfaces` now hides the rail outright at EVERY width the sheet is open
  // at rather than only below the breakpoint. Closed, it keeps its last render
  // in the DOM, so listing it unconditionally would let a stale hidden field
  // shadow the rail's live one.
  draftRoots(): HTMLElement[] {
    const roots: HTMLElement[] = [];
    if (this.sheetOpen && this.sheet) {
      roots.push(this.sheet.root);
    }
    roots.push(this.rail.root);
    if (this.overall) {
      roots.push(this.overall.root);
    }
    return roots;
  }

  // A field carries its own key in the DOM so capture needs to know nothing
  // about what built it — the pairing survives a card that moved between the
  // band and the anchorless section, or between the rail and the overall card.
  draftFields(): HTMLTextAreaElement[] {
    const out: HTMLTextAreaElement[] = [];
    for (const root of this.draftRoots()) {
      for (const el of root.querySelectorAll<HTMLTextAreaElement>(
        '[data-draft]',
      )) {
        out.push(el);
      }
    }
    return out;
  }

  captureDrafts(): {
    key: string;
    value: string;
    start: number | null;
    end: number | null;
    focused: boolean;
  }[] {
    return this.draftFields().map((el) => ({
      key: el.dataset.draft || '',
      value: el.value,
      // selectionStart is null on some input types; the arithmetic that clamps
      // these treats a non-finite bound as "the end", which is where a caret
      // that cannot be read belongs.
      start: el.selectionStart,
      end: el.selectionEnd,
      focused: document.activeElement === el,
    }));
  }

  restoreDrafts(
    saved: {
      key: string;
      value: string;
      start: number | null;
      end: number | null;
      focused: boolean;
    }[],
  ) {
    if (!saved.length) {
      return;
    }
    const fields = new Map<string, HTMLTextAreaElement>();
    for (const el of this.draftFields()) {
      const key = el.dataset.draft || '';
      if (!fields.has(key)) {
        fields.set(key, el);
      }
    }
    const carried = carryDrafts(
      saved,
      Array.from(fields, ([key, el]) => ({ key, value: el.value })),
    );
    for (const put of carried) {
      const el = fields.get(put.key);
      if (!el) {
        continue;
      }
      el.value = put.value;
      if (put.focus) {
        // Focus BEFORE the selection: focusing a textarea moves the caret to
        // the end of its value, so the order is what decides whether the caret
        // lands where the reviewer left it or where the browser prefers.
        el.focus();
      }
      try {
        el.setSelectionRange(put.start, put.end);
      } catch {
        // A field that cannot carry a selection still carries its text.
      }
    }
  }

  paintRail() {
    // Read first, and out here rather than inside the rebuild: the drafts have
    // to be lifted off the OLD cards before a single one is destroyed.
    const drafts = this.captureDrafts();
    const editing = this.editingThread
      ? this.draftFields().find(
          (el) => el.dataset.draft === `edit:${this.editingThread}`,
        )
      : undefined;
    const editWords = editing && {
      value: editing.value,
      typed: editing.value !== editing.dataset.opened,
    };
    this.paintRailCards();
    // EVERY SURFACE THIS DESTROYS IS REBUILT BEFORE THE RESTORE, or being in
    // draftRoots() buys it nothing. The sheet used to be repainted at the FOOT
    // of this function, after the restore had already run — so the restore
    // wrote into sheet fields that were about to be destroyed, and then they
    // were. Being in the list and being inside the capture/restore window are
    // two separate requirements and a surface needs both.
    //
    // It is safe here, before paintAnchors: paintSheet sets `this.cards` aside
    // and puts it back, so nothing it builds reaches the anchor pass either
    // way, and its cards are in flow and never positioned.
    if (this.sheetOpen) {
      this.paintSheet();
    }
    // Then put back, and only then measure. A restored reply is a taller card
    // than the empty one that replaced it, and stacking against the empty
    // height is exactly the stale-geometry overlap this pass also fixes.
    this.restoreDrafts(drafts);
    // AN EDIT WHOSE INSTRUCTION LEFT TAKES ITS WORDS SOMEWHERE THAT STAYS. The
    // instruction was sent, deleted or retracted between polls, so no card
    // carries its edit box any more, and restoreDrafts had nowhere to put
    // them. A box nobody typed in has nothing to keep. See strandEdit.
    if (
      this.editingThread &&
      !this.draftFields().some(
        (el) => el.dataset.draft === `edit:${this.editingThread}`,
      )
    ) {
      this.editingThread = null;
      if (editWords && editWords.typed && editWords.value.trim()) {
        this.strandEdit(editWords.value);
      }
    }
    // Watch the new cards before the first measure, so a card that grows
    // between now and the reviewer's next scroll re-stacks the ones below it
    // instead of being drawn over them.
    this.watchCards();
    // Last, and after every card is in the DOM at its real height: a card's
    // position is measured from its own height, and a card that is not laid
    // out has none.
    this.paintAnchors();
    // EVERY REBUILD MAKES NEW BUTTONS, so a sealed review has to disable them
    // again — the seal is a state, and this function destroys the elements
    // that were carrying it.
    this.applySealedVerbs();
    // And the light, for the same reason one line up: the card that was lit is
    // a DIFFERENT ELEMENT now and carries none of the old one's classes. The
    // run is held on the App precisely so this line can put it back — and if
    // the run it was about has been decided there is no card for it any more,
    // `litRuns` refuses, and the light goes out. The prose's half needs no such
    // help; a decoration is re-derived by the redraw rather than wiped by it.
    this.paintLit();
  }

  // sendReply AND decideButton ARE DELETED, and they went with the card that
  // was their only site. `sendReply` POSTed /_galley/reply and `decideButton`
  // POSTed /_galley/accept and /_galley/reject; all three endpoints are 404 on
  // the rounds-only server (internal/serve/rounds_surface_test.go asserts it),
  // and both functions were reached only from `suggestionCard`, which nothing
  // builds — see the note where it used to be. A reply and a verdict are the
  // conversation verbs of the proposal era: an instruction is immutable work
  // for the next round, so there is nothing to answer and nothing to decide.
}

// The keyboard is its own module — see web/keys.ts — mixed in here so its
// methods still run as App's own, reading and writing `this` exactly as they
// did before the move.
Object.assign(App.prototype, keyMethods);
// The poll-and-refresh loop is its own module too — see web/pending.ts —
// mixed in for the same reason.
Object.assign(App.prototype, pendingMethods);
// The review sheet — the narrow layout's bottom bar and the full-screen list
// it opens — is its own module too — see web/sheet.ts — mixed in for the
// same reason.
Object.assign(App.prototype, sheetMethods);
// The one card component — threadCard and its bubble and overall-card
// wearings — and paintAnchors, the placement pass that puts every card
// beside its own mark or block, are their own module too — see
// web/cards.ts — mixed in for the same reason.
Object.assign(App.prototype, cardMethods);
// The comment composer — its popover, its placement, its two sends — and the
// refused-fence note that shares its deny line, are their own module too —
// see web/composer.ts — mixed in for the same reason.
Object.assign(App.prototype, composerMethods);
// The ways a reviewer starts an instruction from a PLACE rather than from a
// selection — the block grips and a figure's region — are their own
// module too — see web/figures.ts — mixed in for the same reason.
Object.assign(App.prototype, figureMethods);
// History — the versions door, entering and leaving the reading mode,
// restoring a version as a fresh draft, and the arrival strip that says a
// round came back — is its own module too — see web/history.ts — mixed in
// for the same reason.
Object.assign(App.prototype, historyMethods);
// The bar's chrome — the status readout, the census strip, which surfaces
// are on screen at this width, and the mode toggle — is its own module too —
// see web/bar.ts — mixed in for the same reason.
Object.assign(App.prototype, barMethods);
// The verdict — the primary button's label vocabulary, the menu it discloses
// when work is pending, the poll response that drives it (and History's and
// the seal's own readers), and hold/release — is its own module too — see
// web/verdict.ts — mixed in for the same reason.
Object.assign(App.prototype, verdictMethods);
// The seal and the handoff — reading each off the poll, the edge that flips
// the editor's editability, the terminal bar it paints, and the two verb
// constants it owns — are their own module too — see web/seal.ts — mixed in
// for the same reason.
Object.assign(App.prototype, sealMethods);

// A DECLARATION, NOT A CHECK — see this file's own header and
// web/appshell.ts's for the whole argument. Every member of AppMethods
// arrives through one of the ten `Object.assign` calls immediately above,
// so no class body declares them and `implements` cannot verify them; this
// merges their names and signatures onto `App`'s type so the calls this
// file itself makes to them (`this.makeCensus()` in the constructor,
// `this.onKey(event)` in the keydown listener, and so on) type-check
// against a real claim rather than an implicit `any`.
interface App extends AppMethods {}

// --- keeping the reviewer's place across a whole-document rebuild ---

/**
 * placeOf is the half of App.keepPlace that REMEMBERS where the reviewer was.
 *
 * Two coordinates, chosen because they are the only two a whole-document
 * rebuild cannot destroy: the caret's top-level block index, and the block's
 * text up to the caret. Neither is a Yjs position, so neither dies with the
 * items the rebuild deleted.
 *
 * A RANGE HAS TWO ENDS AND BOTH OF THEM ARE ANCHORABLE. This used to refuse
 * one outright, which cost the reviewer more than a caret: the selection is
 * the INPUT to galley's own verbs — strike and the comment composer both read
 * it — so a reviewer who swept out a phrase, looked up at an arrival and came
 * back was operating on nothing, with the caret at the end of the document.
 * Measured: a 20-character sweep at 194→214 of a 1349-position document ended
 * collapsed at 1351. So both ends get the same two coordinates the caret gets,
 * plus the text between them (see spanIn) and the DIRECTION — a sweep
 * right-to-left has its head at the range's start, and that is where the next
 * keystroke goes.
 *
 * Returns null for a selection at the document level, which has no block to
 * hang off, and for a non-text range — a NodeSelection has no text anchor and
 * rebuilding it as a text range would silently change what is selected. The
 * scroll is still restored in those cases; only the caret is left alone.
 */
export function placeOf(state: EditorState): Place | null {
  const { selection } = state;
  if (selection.$from.depth === 0) {
    return null;
  }
  const anchorAt = ($pos: ResolvedPos): PlaceEnd => ({
    index: $pos.index(0),
    prefix: state.doc.textBetween($pos.start(1), $pos.pos, '\n', '\n'),
  });
  const place: Place = {
    ...anchorAt(selection.$from),
    end: null,
    text: '',
    back: false,
  };
  if (selection.empty) {
    return place;
  }
  if (!(selection instanceof TextSelection) || selection.$to.depth === 0) {
    return null;
  }
  place.end = anchorAt(selection.$to);
  place.text = state.doc.textBetween(selection.from, selection.to, '\n', '\n');
  place.back = selection.head < selection.anchor;
  return place;
}

/**
 * placeIn turns a remembered place back into a position in the document that
 * just arrived, or null when it cannot be found honestly.
 *
 * A PREFIX LENGTH IS NOT A POSITION OFFSET, and treating it as one is how this
 * silently put the caret in the wrong place for every reviewer who was inside
 * a list, a blockquote or a table. `textBetween` flattens: a nested node costs
 * a position on the way in and another on the way out, and a block boundary it
 * crosses costs four where the flattened text spends a single '\n'. So
 * `inner + prefix.length` lands SHORT — two positions in the first item of a
 * list, eight in the third — and it lands short with the guard satisfied,
 * because the block's flattened text does still start with the prefix. The
 * reviewer's next keystroke then goes in mid-word, several characters from
 * where they were typing, with nothing on screen to say so.
 *
 * The search below is over positions instead: `textBetween(inner, p).length`
 * is non-decreasing in p, so the smallest p that reaches the prefix's length
 * is the position that prefix names. The equality check afterwards is the
 * refusal — the block at this index is not the block the caret was in, so say
 * nothing rather than drop the caret at a plausible offset.
 */
export function placeIn(doc: PMNode, place: PlaceEnd | null): number | null {
  if (!place || place.index >= doc.childCount) {
    return null;
  }
  let start = 0;
  for (let i = 0; i < place.index; i += 1) {
    start += doc.child(i).nodeSize;
  }
  const inner = start + 1;
  const end = inner + doc.child(place.index).content.size;
  if (!doc.textBetween(inner, end, '\n', '\n').startsWith(place.prefix)) {
    return null;
  }
  let lo = inner;
  let hi = end;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (doc.textBetween(inner, mid, '\n', '\n').length < place.prefix.length) {
      lo = mid + 1;
    } else {
      hi = mid;
    }
  }
  return doc.textBetween(inner, lo, '\n', '\n') === place.prefix ? lo : null;
}

/**
 * spanIn turns a remembered place back into the SELECTION it named — a span
 * with a direction — or null when neither end can be found honestly.
 *
 * placeIn answers for one end. Two honest ends are still not an honest
 * selection: each is located by the text of its own block up to it, and
 * nothing in that says the text BETWEEN them is what the reviewer swept over.
 * A block in the middle that grew would leave both ends resolving and the span
 * covering an agent's new sentence that the reviewer never selected — and the
 * next thing they do to it is strike it or comment on it. So the text between
 * the two resolved positions has to equal the text that was selected, and
 * anything less collapses to a caret rather than guessing at a span.
 *
 * The three outcomes, in order:
 *
 *   - both ends resolve and the text between them is unchanged — the range
 *     comes back whole, with its direction, so the head is still the end the
 *     next keystroke goes to;
 *   - one end resolves, or both do over changed text — collapse to the HEAD if
 *     that is the end that survived, otherwise to the other. A caret the
 *     reviewer can see beats a selection that is a guess;
 *   - neither end resolves — null, which the caller reports through the
 *     arrival strip rather than absorbing. See App.restorePlace.
 */
export function spanIn(doc: PMNode, place: Place | null): Span | null {
  if (!place) {
    return null;
  }
  const from = placeIn(doc, place);
  if (!place.end) {
    return from === null ? null : { from, to: from, back: false };
  }
  const to = placeIn(doc, place.end);
  if (
    from !== null &&
    to !== null &&
    to >= from &&
    doc.textBetween(from, to, '\n', '\n') === place.text
  ) {
    return { from, to, back: place.back };
  }
  const head = place.back ? from : to;
  const caret = head !== null ? head : place.back ? to : from;
  return caret === null ? null : { from: caret, to: caret, back: false };
}

// sectionSpan and indexOfChild moved to web/figures.ts with the block grip
// and figurePairs, their only readers.

// cssEscape wraps CSS.escape, which every browser this editor supports has —
// but jsdom and some headless harnesses do not, and a delegated listener that
// throws takes every later one down with it. The fallback is deliberately
// narrow: a run is a hex token from the Go side, so quoting is enough.
function cssEscape(value: string): string {
  const s = String(value);
  return typeof CSS !== 'undefined' && CSS.escape
    ? CSS.escape(s)
    : s.replace(/["\\]/g, '\\$&');
}

// sameWords compares two words-by-ID maps. Cheap, and the reason
// paintNoteWords can run on every editor update without dispatching one.
function sameWords(
  a: Record<string, string>,
  b: Record<string, string>,
): boolean {
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k])
  );
}

// Guarded so probe.mjs can import this module in node — where there is no
// window — and check the parts of it that are pure logic (the heading
// coercion, the mark schema). In a browser this always runs; the shell's
// load handler looks for exactly this object.
if (typeof window !== 'undefined') {
  window.galleyEdit = { init };
}

export { init };
