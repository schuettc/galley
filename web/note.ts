import { Node, mergeAttributes } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';

// A block note — docmodel.Note. It is a block comment's MARK in the document
// tree, because a comment on an image or a section has no text range to hang a
// Highlight on. galley writes it as <note anchor id> with no text run: the
// comment's words live in the unsent round, and the file carries only
// "{>>@comment cb-…<<}". A hand-typed {>>note<<} on its own line is a note
// with words and no id, and still builds here.
//
// It must exist in this schema even though nothing here edits it. An element
// whose nodeName the schema does not know is not skipped by y-prosemirror — it
// is DELETED from the Yjs document, and galley then projects the deletion to
// disk. Registering the node is what stops a reviewer's note from vanishing
// the first time the document is opened.
//
// NOT an atom, and the content is `text*`. A word-bearing note arrives as
// <note anchor="…"> with one unmarked YXmlText run inside it (ydoc.writeBlock's
// `len(b.Inlines) > 0` branch), and y-prosemirror builds the ProseMirror node
// from the element's children — so a node declared with no content would have
// nowhere to put that run, and the same catch that deletes an unknown NODE
// deletes it for an invalid content match. `text*` holds both shapes: an ID
// note with no run, and a word note with one. The schema has to describe what
// the fragment actually holds, not what the editor wishes it held.
//
// contenteditable="false" on the rendered aside is what keeps it read-only:
// the note's words are authored through the composer and the rail, never by
// typing into the document. Making it editable would put a second, untracked
// authoring path next to the one the rail owns.
export const NoteBlock = Node.create({
  name: 'note',
  group: 'block',
  content: 'text*',
  marks: '',
  defining: true,
  selectable: false,
  draggable: false,

  addAttributes() {
    return {
      // 'block' — about the block above it — or 'document'.
      anchor: {
        default: 'block',
        parseHTML: (el) => el.getAttribute('data-anchor') || 'block',
        renderHTML: (attrs) => ({ 'data-anchor': attrs.anchor }),
      },
      // The block comment's ID (docmodel.CommentIDAttr), which is the only
      // thing linking this note to its words. DECLARED, not merely tolerated:
      // an attribute the schema does not declare is dropped when the node is
      // built, and the browser's next write takes it out of the fragment and
      // the projection out of the file.
      id: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-comment-id') || '',
        renderHTML: (attrs) =>
          attrs.id ? { 'data-comment-id': attrs.id } : {},
      },
    };
  },

  parseHTML() {
    return [{ tag: 'aside[data-galley-note]' }];
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'aside',
      mergeAttributes(HTMLAttributes, {
        'data-galley-note': '',
        class: 'gly-note',
        contenteditable: 'false',
      }),
      0,
    ];
  },

  addProseMirrorPlugins() {
    return [settledNotePlugin()];
  },
});

// --- which notes are settled ---
//
// A RESOLVED NOTE MUST NOT LOOK LIKE A LIVE ONE. Resolving keeps every word of
// a note — the words ARE the content — so the block stays in the prose, and
// without a mark of some kind it reads as a conversation still waiting for an
// answer. Court hit the panel half of this; this is the document half.
//
// IT HAS TO BE A DECORATION, and that is the whole reason this plugin exists.
// The obvious implementation — walk the .gly-note elements and toggle a class —
// was written first and MEASURED FAILING: ProseMirror owns those elements and
// rewrites their attributes from the node whenever it redraws, which it does on
// every rebuild the server pushes (see CLAUDE.md: every server-side mutation
// replaces the whole document) and on plenty of transactions besides. The class
// landed and was wiped a moment later, leaving the note looking live while the
// rail showed the thread settled — the same lie, one surface over.
//
// The settled set is not in the document, and must not be: resolution lives in
// the sidecar (review.Thread.Resolved), because CriticMarkup has nowhere to
// write it and inventing a marker would rewrite the author's own line. So it
// arrives as transaction META, from the code that reads /_galley/pending, and
// this plugin holds it as view state — never as content.
export const settledNotesKey = new PluginKey('glySettledNotes');

/**
 * noteFlags maps a per-note-in-document-order boolean list onto decorations.
 *
 * Document ORDER is the pairing coordinate, matching rail.ts's settledNotes,
 * which is what produced the list. Nothing here re-derives which note is which:
 * one rule, computed once, applied here.
 */
export function noteDecorations(
  doc: PMNode,
  settled: boolean[],
): DecorationSet {
  const flags = settled || [];
  const decos: Decoration[] = [];
  let i = 0;
  doc.descendants((node, pos) => {
    if (node.type.name !== 'note') {
      return true;
    }
    if (flags[i]) {
      decos.push(
        Decoration.node(pos, pos + node.nodeSize, {
          class: 'gly-note-settled',
        }),
      );
    }
    i += 1;
    return false;
  });
  return DecorationSet.create(doc, decos);
}

function settledNotePlugin() {
  return new Plugin({
    key: settledNotesKey,
    state: {
      init: (_, state) => ({
        settled: [],
        decos: noteDecorations(state.doc, []),
      }),
      apply(tr, prev) {
        const next = tr.getMeta(settledNotesKey);
        if (!next && !tr.docChanged) {
          return prev;
        }
        // A rebuilt document keeps the LAST KNOWN flags rather than clearing
        // them: the rebuild and the pending refresh that follows it are two
        // round trips, and a note that un-settles itself in between is a flicker
        // the reviewer reads as a state change.
        const settled = next || prev.settled;
        return { settled, decos: noteDecorations(tr.doc, settled) };
      },
    },
    props: {
      decorations(state) {
        return settledNotesKey.getState(state).decos;
      },
    },
  });
}
