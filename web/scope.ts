// scope.ts — what a block instruction will be about.
//
// A grip opens the composer on a block, and the reviewer has to be able to
// see what that block is: the whole section for a heading, the block itself
// for anything else. The grip used to show it by SELECTING the block, and the
// selection outlived the composer — the next keystroke replaced the section.
// The selection is the reviewer's and nothing here touches it.
//
// THE RANGE IS VIEW STATE, NEVER CONTENT. It arrives as transaction META from
// the code that opens and closes the composer, and is held here so that an
// edit made while the composer is up maps it rather than leaving it pointing
// at whatever now stands at the old positions.
//
// THE OUTLINE IS ONE BOX, AND IT IS NOT DRAWN HERE. A section is one thing,
// and an outline round each of its blocks read as that many separate things.
// One box from the first block's top to the last block's bottom cannot be a
// node decoration (a decoration belongs to one node), and anything appended
// inside `.ProseMirror` is content the next projection writes to the file. So
// web/figures.ts's paintScope draws it beside the grips, outside the document,
// from the range this plugin holds.

import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Transaction } from '@tiptap/pm/state';

// The span the outline covers, in document positions: from before the first
// top-level block in scope to after the last. Null is no outline.
export type ScopeRange = { from: number; to: number } | null;

export interface ScopePluginState {
  range: ScopeRange;
}

export const scopeKey = new PluginKey<ScopePluginState>('glyScope');

const isRange = (v: unknown): v is { from: number; to: number } =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as { from?: unknown }).from === 'number' &&
  typeof (v as { to?: unknown }).to === 'number';

export function scopePlugin(): Plugin<ScopePluginState> {
  return new Plugin<ScopePluginState>({
    key: scopeKey,
    state: {
      init: () => ({ range: null }),
      apply(tr: Transaction, prev: ScopePluginState): ScopePluginState {
        // `getMeta` is typed `any`; it is read once and narrowed. Absent
        // (undefined) is "no word about the scope"; anything else sets it,
        // and anything that is not a range clears it.
        const meta: unknown = tr.getMeta(scopeKey);
        if (meta !== undefined) {
          return { range: isRange(meta) ? meta : null };
        }
        if (!tr.docChanged || !prev.range) {
          return prev;
        }
        // A DOC CHANGE MAPS THE RANGE, and an edge that would take in a block
        // inserted against it stays outside. A change that swallows the range
        // (every server-side mutation replaces the whole document) leaves it
        // empty, which draws nothing: a missing outline is honest, and an
        // outline on the wrong blocks is not.
        const from = tr.mapping.map(prev.range.from, 1);
        const to = tr.mapping.map(prev.range.to, -1);
        return { range: from < to ? { from, to } : null };
      },
    },
  });
}
