// grips.ts — the block grip's rules, with no DOM in them.
//
// Every top-level heading, fence, table, figure, display-math block and front
// matter carries one grip in the left gutter, and pressing it opens the block
// composer on that block. Which blocks get one, what its face and its label
// say, how many instructions it already carries, and where two grips go when
// their blocks are closer than one grip are all decisions over a document, a
// list of threads and plain numbers. They live here, where web/probe.mjs
// drives every one of them with no browser — rail.ts's rule, for rail.ts's
// reason. The DOM half (web/figures.ts) measures, calls in here, and paints
// what it is told.
//
// NOTHING HERE TOUCHES `document`, `window` or an EditorView, and nothing here
// imports from card.ts, figures.ts or entry.ts, which all do.
//
// THE SIZES ARE NOT HERE. The grip's size and the gutter it sits in are
// `--gly-grip-size` and `--gly-grip-gutter` in editor.css, and the DOM half
// hands `stackGrips` the height it MEASURED. A pixel constant here beside a
// pixel value there is two spellings of one size, and the day one is retuned
// without the other the grips overlap by exactly the difference.

import type { Node as PMNode } from '@tiptap/pm/model';
import { elide, type PendingThread } from './rail.ts';

// The top-level node types that get a grip, in the browser schema's names —
// which are also the Go side's BlockKind strings, so a BlockRef's `kind`
// compares against them directly. A mermaid diagram is a `codeBlock` with
// `language: mermaid`. Paragraphs, lists, blockquotes, rules and notes get
// none: a grip on every paragraph is a gutter of buttons the reviewer reads as
// noise, and a selection is already the way to point at prose.
export const GRIP_KINDS: readonly string[] = [
  'heading',
  'codeBlock',
  'table',
  'image',
  'mathBlock',
  'frontMatter',
];

// One block that gets a grip.
//
// `index` is the block's ordinal among the document's TOP-LEVEL children,
// notes counted, which is exactly BlockRef.Index. A block's key is a content
// hash the browser cannot compute, so the grip finds it at click time by
// index and kind in the server's block list. `figure` is true for the blocks
// that take a region comment as well as a whole-block one: an image, and a
// mermaid diagram.
export type GripTarget = {
  pos: number;
  index: number;
  kind: string;
  figure: boolean;
};

const isDiagram = (node: PMNode) =>
  node.type.name === 'codeBlock' && node.attrs.language === 'mermaid';

/**
 * gripTargets lists the blocks that get a grip, in document order.
 *
 * TOP-LEVEL ONLY. A fence inside a list item or a heading inside a blockquote
 * is not a top-level block, so the Go side has no key for it and an
 * instruction could not be filed against it; a grip there would open a
 * composer that can never send.
 */
export function gripTargets(doc: PMNode): GripTarget[] {
  const out: GripTarget[] = [];
  doc.forEach((node, pos, index) => {
    const kind = node.type.name;
    if (!GRIP_KINDS.includes(kind)) {
      return;
    }
    out.push({
      pos,
      index,
      kind,
      figure: kind === 'image' || isDiagram(node),
    });
  });
  return out;
}

/**
 * gripCount is how many instructions are already on the block with this key.
 * A region comment on a figure is one of them: it is filed on the figure's
 * key, and the grip is the one way back to the figure. A range or a
 * whole-document instruction has no block key and is never counted.
 */
export function gripCount(
  threads: readonly PendingThread[],
  key: string,
): number {
  if (!key) {
    return 0;
  }
  return threads.filter((t) => t.anchor === 'block' && t.anchorKey === key)
    .length;
}

// Past nine the face says 9+: two digits do not fit the grip's one fixed box,
// and a box that widens with its count moves the thing beside it.
const FACE_MAX = 9;

/** gripFace is what the grip shows: `+` when the block has no instructions,
 * otherwise their count, capped at `9+`. */
export function gripFace(count: number): string {
  if (count <= 0) {
    return '+';
  }
  return count > FACE_MAX ? `${FACE_MAX}+` : String(count);
}

// What each kind is called in a label. A heading is not here: it names its
// section by its own words.
const NOUNS: Record<string, string> = {
  codeBlock: 'this code block',
  table: 'this table',
  image: 'this figure',
  mathBlock: 'this equation',
  frontMatter: 'the front matter',
};

/**
 * gripLabel is the grip's aria-label and title, in words: `Add an instruction
 * on this table`, `Add an instruction on the section "Design" (2 already)`.
 *
 * It takes the TARGET rather than its kind because a mermaid diagram and a
 * fence are both `codeBlock`, and a reviewer calls one a diagram.
 *
 * A heading's label arrives as the Go side's blockLabel, `## Design`; the
 * hashes are how a file names a level and are noise in a sentence, so they
 * are dropped. The words are bounded by the card head's own bound (`elide`),
 * so the grip and the card it files cannot disagree about where a long
 * heading is cut. Every other kind is named by what it is, never by its
 * label: a table's header or a fence's first line in a button's name reads
 * as an instruction about those words.
 */
export function gripLabel(
  target: Pick<GripTarget, 'kind' | 'figure'>,
  label: string,
  count: number,
): string {
  let on: string;
  if (target.kind === 'heading') {
    const words = elide((label || '').replace(/^#+\s*/, ''));
    on = words ? `the section "${words}"` : 'this section';
  } else if (target.kind === 'codeBlock' && target.figure) {
    on = 'this diagram';
  } else {
    on = NOUNS[target.kind] || 'this block';
  }
  const already = count > 0 ? ` (${count} already)` : '';
  return `Add an instruction on ${on}${already}`;
}

/**
 * stackGrips pushes each grip down until it clears the one above it by `gap`,
 * and leaves every grip that is already clear where its block put it.
 *
 * `tops` are in document order, which is the order the blocks measure in;
 * `size` is the grip's MEASURED height (see the header for why it is not a
 * constant here). The output is non-decreasing whatever the input.
 */
export function stackGrips(
  tops: readonly number[],
  size: number,
  gap: number,
): number[] {
  const out: number[] = [];
  let floor = -Infinity;
  for (const top of tops) {
    const at = Math.max(top, floor);
    out.push(at);
    floor = at + size + gap;
  }
  return out;
}
