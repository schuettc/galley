import { Node, mergeAttributes } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import type { Node as PMNode } from '@tiptap/pm/model';

// A block note — docmodel.Note. It is a block comment's MARK in the document
// tree, because a comment on an image or a section has no text range to hang a
// Highlight on. galley writes it as <note anchor id> with no text run: the
// comment's words live in the unsent round, and the file carries only
// "{>>@comment cb-…<<}". The amber box shows the words from the instruction
// data, found by that id (noteWordDecorations, below). A hand-typed
// {>>note<<} on its own line is a note with words and no id: it still builds
// here and shows its own text.
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
    return [noteWordsPlugin()];
  },
});

// --- a block note's words, found by ID ---
//
// An ID note holds no words: the file carries only "{>>@comment cb-…<<}" and
// the words live in the unsent round. The amber box gets them from the
// instruction data, keyed by the note's id — the ONE link between a note and
// its comment. Nothing here matches a note's text or counts its place in the
// document: two comments with the same words are two comments, and a note that
// moves keeps its own.
//
// IT HAS TO BE A DECORATION. Anything appended inside .ProseMirror is content,
// and a class or child put on a ProseMirror-rendered element is wiped the next
// time ProseMirror redraws it, which every server-side mutation causes (each
// one replaces the whole document). A widget is ProseMirror's own, so a redraw
// re-applies it rather than erasing it.
//
// The words are not in the document, and must not be, so they arrive as
// transaction META from the code that reads /_galley/pending, and this plugin
// holds them as view state. A note with no id (a hand-typed {>>words<<}) gets
// no widget: its own text shows through the content hole, with no code here.
export const noteWordsKey = new PluginKey('glyNoteWords');

/**
 * noteWordDecorations paints, inside each note whose id names a block
 * comment, that comment's words.
 *
 * @param doc the editor document
 * @param words comment words by comment ID
 * @returns one widget per note whose id has words
 */
export function noteWordDecorations(
  doc: PMNode,
  words: Record<string, string>,
): DecorationSet {
  const decos: Decoration[] = [];
  doc.descendants((node, pos) => {
    if (node.type.name !== 'note') {
      return true;
    }
    const id: string = node.attrs.id || '';
    const text =
      id && Object.prototype.hasOwnProperty.call(words, id) ? words[id] : '';
    if (text) {
      decos.push(
        Decoration.widget(pos + 1, () => wordsEl(text), {
          side: -1,
          // The words are a readout, not a place the caret can live.
          ignoreSelection: true,
          // The key carries the WORDS as well as the id: two widgets with one
          // key are one widget to ProseMirror and its DOM is not rebuilt, so an
          // id alone would leave an edited comment's old words on screen.
          key: `words:${id}:${text}`,
        }),
      );
    }
    return false;
  });
  return DecorationSet.create(doc, decos);
}

function wordsEl(text: string): HTMLElement {
  const span = document.createElement('span');
  span.className = 'gly-note-words';
  span.textContent = text;
  return span;
}

function noteWordsPlugin() {
  return new Plugin({
    key: noteWordsKey,
    state: {
      init: (_, state) => ({
        words: {},
        decos: noteWordDecorations(state.doc, {}),
      }),
      apply(tr, prev) {
        const next = tr.getMeta(noteWordsKey);
        if (!next && !tr.docChanged) {
          return prev;
        }
        // A rebuilt document keeps the LAST KNOWN words rather than clearing
        // them: the rebuild and the pending refresh that follows it are two
        // round trips, and an amber box that empties in between is a flicker
        // the reviewer reads as a lost comment.
        const words = next || prev.words;
        return { words, decos: noteWordDecorations(tr.doc, words) };
      },
    },
    props: {
      decorations(state) {
        return noteWordsKey.getState(state).decos;
      },
    },
  });
}
