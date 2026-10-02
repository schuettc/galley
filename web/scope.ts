// scope.ts — what a block instruction will be about, outlined.
//
// A grip opens the composer on a block, and the reviewer has to be able to
// see what that block is: the whole section for a heading, the block itself
// for anything else. The grip used to show it by SELECTING the block, and the
// selection outlived the composer — the next keystroke replaced the section.
// The selection is the reviewer's and nothing here touches it. The scope is
// drawn instead.
//
// IT HAS TO BE A DECORATION. A class on a ProseMirror-rendered element is not
// yours to keep: the editor rewrites those attributes from the node on every
// redraw (see lit.ts, which is this file's model). The range arrives as
// transaction META from the code that opens and closes the composer, and is
// held here as view state, never as content.

import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Transaction } from '@tiptap/pm/state';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';

// The span the outline covers, in document positions: from before the first
// top-level block in scope to after the last. Null is no outline.
export type ScopeRange = { from: number; to: number } | null;

export interface ScopePluginState {
  range: ScopeRange;
  decos: DecorationSet;
}

export const scopeKey = new PluginKey<ScopePluginState>('glyScope');

// scopeDecorations outlines every top-level block lying wholly inside the
// range. Whole blocks only: the scope of a block instruction is blocks.
function scopeDecorations(doc: PMNode, range: ScopeRange): DecorationSet {
  if (!range || range.from >= range.to) {
    return DecorationSet.empty;
  }
  const decos: Decoration[] = [];
  doc.forEach((node, pos) => {
    if (pos >= range.from && pos + node.nodeSize <= range.to) {
      decos.push(
        Decoration.node(pos, pos + node.nodeSize, { class: 'gly-grip-scope' }),
      );
    }
  });
  return DecorationSet.create(doc, decos);
}

const isRange = (v: unknown): v is { from: number; to: number } =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as { from?: unknown }).from === 'number' &&
  typeof (v as { to?: unknown }).to === 'number';

export function scopePlugin(): Plugin<ScopePluginState> {
  return new Plugin<ScopePluginState>({
    key: scopeKey,
    state: {
      init: () => ({ range: null, decos: DecorationSet.empty }),
      apply(tr: Transaction, prev: ScopePluginState): ScopePluginState {
        // `getMeta` is typed `any`; it is read once and narrowed. Absent
        // (undefined) is "no word about the scope"; anything else sets it,
        // and anything that is not a range clears it.
        const meta: unknown = tr.getMeta(scopeKey);
        if (meta !== undefined) {
          const range = isRange(meta) ? meta : null;
          return { range, decos: scopeDecorations(tr.doc, range) };
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
        const range = from < to ? { from, to } : null;
        return { range, decos: scopeDecorations(tr.doc, range) };
      },
    },
    props: {
      decorations(state) {
        return scopeKey.getState(state)?.decos ?? DecorationSet.empty;
      },
    },
  });
}
