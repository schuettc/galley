// share.ts — handing an approved document to a reader.
//
// The vocabulary in the top half is pure — every label the button, its menu
// and the bar's preview button show, and the status line a save ends on — so
// probe.mjs and web/share.mjs import the spellings rather than restating them.
//
// THE BOTTOM HALF, `shareMethods`, IS A MIXIN `Object.assign`ed onto
// `App.prototype` in entry.ts, typed against `AppShell` like web/seal.ts.
//
// SHARE APPEARS ONLY AFTER APPROVAL, IN THE BOX REVISE LEAVES. It is built
// hidden, directly after `#gly-revise`, and `applySeal` swaps the two on the
// sealing edge (and back on the unseal edge) with `hidden`, so exactly one of
// them occupies the slot. Its width and height are Revise's own, read on that
// edge, so approving moves nothing in the bar (web/share.mjs measures it).
//
// SAVING IS THE SERVER'S, because a browser download cannot say where the
// file landed: POST /_galley/share/html renders the document with the
// project's theme, writes <name>.html beside <name>.md and answers the path.
// A page under review (page mode) is already the HTML a reader gets, so its
// menu offers only Copy path, for the page.

import { postJSON } from './net.ts';
import type { AppShell } from './appshell.ts';

export const SHARE_LABEL = 'Share ▾';
export const SHARE_SAVE_HTML = 'Save HTML';
export const SHARE_COPY_PATH = 'Copy path';
export const SHARE_COPY_LINK = 'Copy link · later';
export const SHARE_LINK_LATER = 'links are not available yet';
export const PREVIEW_LABEL = 'HTML preview ↗';
const PREVIEW_HREF = '/_galley/render';

function baseName(path: string): string {
  const i = path.lastIndexOf('/');
  return i === -1 ? path : path.slice(i + 1);
}

/** savedLine is the status line after Save HTML. `copied` is false only when
 * the browser refused the clipboard; the line then carries the whole path, so
 * the reviewer still learns where the file is. */
export function savedLine(path: string, copied: boolean): string {
  const name = baseName(path);
  return copied ? `saved ${name} · path copied` : `saved ${name} · ${path}`;
}

/** copiedLine is the status line after Copy path. */
function copiedLine(path: string, copied: boolean): string {
  return copied ? `path copied · ${baseName(path)}` : path;
}

export interface ShareItem {
  label: string;
  act: 'save' | 'copy' | 'link';
  disabled?: boolean;
  title?: string;
}

/** shareItems is the menu, in order: a page under review offers only Copy
 * path; a markdown review all three, the link held disabled for later. */
export function shareItems(pageMode: boolean): ShareItem[] {
  const copy: ShareItem = { label: SHARE_COPY_PATH, act: 'copy' };
  if (pageMode) {
    return [copy];
  }
  return [
    { label: SHARE_SAVE_HTML, act: 'save' },
    copy,
    {
      label: SHARE_COPY_LINK,
      act: 'link',
      disabled: true,
      title: SHARE_LINK_LATER,
    },
  ];
}

function copyText(text: string): Promise<boolean> {
  if (!navigator.clipboard || !navigator.clipboard.writeText) {
    return Promise.resolve(false);
  }
  return navigator.clipboard.writeText(text).then(
    () => true,
    () => false,
  );
}

export const shareMethods = {
  // makeShare builds the button hidden, directly after Revise, so it can take
  // Revise's place on the seal edge. Null where the shell has no Revise.
  makeShare(this: AppShell): HTMLButtonElement | null {
    const revise = this.revise;
    if (!revise || !revise.parentNode) {
      return null;
    }
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'gly-share';
    b.textContent = SHARE_LABEL;
    b.title = 'hand the approved document to a reader';
    b.hidden = true;
    b.addEventListener('click', () => {
      if (this.shareMenu && !this.shareMenu.hidden) {
        this.closeShareMenu();
      } else {
        this.openShareMenu();
      }
    });
    revise.parentNode.insertBefore(b, revise.nextSibling);
    return b;
  },

  // swapShare is the seal edge's half for the slot: sealed shows Share in
  // Revise's box (one CSS width, `--gly-verb-w`, and the bar's padding),
  // unsealed hands the slot back to Revise.
  swapShare(this: AppShell, sealed: boolean) {
    const share = this.share;
    const revise = this.revise;
    if (!share || !revise) {
      return;
    }
    if (sealed) {
      revise.hidden = true;
      share.hidden = false;
      return;
    }
    this.closeShareMenu();
    share.hidden = true;
    revise.hidden = false;
  },

  makeShareMenu(this: AppShell): HTMLElement {
    const el = document.createElement('div');
    el.className = 'gly-share-menu';
    el.setAttribute('role', 'menu');
    el.hidden = true;
    for (const item of shareItems(this.pageMode)) {
      const b = document.createElement('button');
      b.type = 'button';
      b.setAttribute('role', 'menuitem');
      b.textContent = item.label;
      if (item.disabled) {
        b.disabled = true;
      }
      if (item.title) {
        b.title = item.title;
      }
      b.addEventListener('click', () => {
        this.closeShareMenu();
        this.runShare(item.act);
      });
      el.append(b);
    }
    document.body.appendChild(el);
    // Outside-click closes, in capture, the verdict menu's way: the button
    // itself is not "outside", it owns the toggle.
    document.addEventListener(
      'click',
      (event) => {
        if (el.hidden || !(event.target instanceof Node)) {
          return;
        }
        if (el.contains(event.target)) {
          return;
        }
        if (this.share && this.share.contains(event.target)) {
          return;
        }
        this.closeShareMenu();
      },
      true,
    );
    window.addEventListener('resize', () => this.closeShareMenu());
    return el;
  },

  openShareMenu(this: AppShell) {
    const share = this.share;
    if (!share || share.hidden) {
      return;
    }
    if (!this.shareMenu) {
      this.shareMenu = this.makeShareMenu();
    }
    const menu = this.shareMenu;
    // Right-aligned under the button: the slot is the bar's rightmost, so
    // the menu grows leftward over the page rather than off its edge.
    const at = share.getBoundingClientRect();
    const right = document.documentElement.clientWidth - at.right;
    menu.hidden = false;
    menu.style.top = `${at.bottom + window.scrollY + 4}px`;
    menu.style.right = `${Math.max(4, right - window.scrollX)}px`;
    menu.style.left = 'auto';
  },

  closeShareMenu(this: AppShell) {
    if (this.shareMenu) {
      this.shareMenu.hidden = true;
    }
  },

  runShare(this: AppShell, act: ShareItem['act']) {
    if (act === 'copy') {
      const path = this.docPath;
      void copyText(path).then((ok) => this.say(copiedLine(path, ok)));
      return;
    }
    if (act !== 'save') {
      return;
    }
    this.say('saving HTML…');
    postJSON('/_galley/share/html', {})
      .then((res) =>
        res.ok
          ? res
              .json()
              .then((v: { path: string }) =>
                copyText(v.path).then((ok) => this.say(savedLine(v.path, ok))),
              )
          : res
              .text()
              .then((t) => this.say(t.trim() || `save failed (${res.status})`)),
      )
      .catch((err: unknown) =>
        this.say(
          `save failed: ${err instanceof Error ? err.message : String(err)}`,
        ),
      );
  },

  // The bar's way to the rendered page, markdown reviews only: a page under
  // review already has its own preview pane. It sits just before Revise, to
  // the right of the mode toggle, so the toggle leaving on the seal edge moves
  // it no more than it moves Revise; the seal does not touch it, because
  // reading the shared look is as useful after approval as before.
  makePreview(this: AppShell): HTMLButtonElement | null {
    const revise = this.revise;
    if (this.pageMode || !revise || !revise.parentNode) {
      return null;
    }
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'gly-preview-open';
    b.textContent = PREVIEW_LABEL;
    b.title = 'the branded page Save HTML writes, refreshed on every save';
    b.addEventListener('click', () => {
      window.open(PREVIEW_HREF, '_blank', 'noopener');
    });
    revise.parentNode.insertBefore(b, revise);
    return b;
  },
};
