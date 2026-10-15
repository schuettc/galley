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
// TWO WAYS TO HAND IT OVER. A download is the browser's: Download HTML is
// GET /_galley/render?download=1 (the branded page, no refresh script) and
// Download Markdown GET /_galley/download/md, each an attachment the browser
// puts in Downloads. A Share is the server's, macOS only: POST
// /_galley/share/html writes <name>.html beside <name>.md and puts that file
// on the clipboard, /_galley/share/md puts the .md there, so a paste into a
// chat attaches the file. The server says whether it can (`data-can-share`),
// and the Shares are absent where it cannot. A page under review (page mode)
// is already the HTML a reader gets: its menu hands over the page itself and
// has no Markdown items.

import { postJSON } from './net.ts';
import type { AppShell } from './appshell.ts';

export const SHARE_LABEL = 'Share ▾';
export const SHARE_DOWNLOAD_HTML = 'Download HTML';
export const SHARE_DOWNLOAD_MD = 'Download Markdown';
export const SHARE_SHARE_HTML = 'Share HTML';
export const SHARE_SHARE_MD = 'Share Markdown';
export const PREVIEW_LABEL = 'HTML preview ↗';
const PREVIEW_HREF = '/_galley/render';

function baseName(path: string): string {
  const i = path.lastIndexOf('/');
  return i === -1 ? path : path.slice(i + 1);
}

/** sharedLine is the status line after a Share put the file at path on the
 * clipboard. */
export function sharedLine(path: string): string {
  return `${baseName(path)} copied · paste to attach`;
}

export interface ShareItem {
  label: string;
  act: 'download-html' | 'download-md' | 'share-html' | 'share-md';
}

/** shareItems is the menu, in order: the downloads, then, where the server
 * can put a file on the clipboard, the Shares. A page under review has no
 * Markdown items. */
export function shareItems(pageMode: boolean, canShare: boolean): ShareItem[] {
  const items: ShareItem[] = [
    { label: SHARE_DOWNLOAD_HTML, act: 'download-html' },
  ];
  if (!pageMode) {
    items.push({ label: SHARE_DOWNLOAD_MD, act: 'download-md' });
  }
  if (canShare) {
    items.push({ label: SHARE_SHARE_HTML, act: 'share-html' });
    if (!pageMode) {
      items.push({ label: SHARE_SHARE_MD, act: 'share-md' });
    }
  }
  return items;
}

const DOWNLOADS: Record<string, string> = {
  'download-html': '/_galley/render?download=1',
  'download-md': '/_galley/download/md',
};

// download starts the browser's download of href; the server's
// Content-Disposition names the file.
function download(href: string) {
  const a = document.createElement('a');
  a.href = href;
  a.download = '';
  document.body.appendChild(a);
  a.click();
  a.remove();
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
    for (const item of shareItems(this.pageMode, this.canShare)) {
      const b = document.createElement('button');
      b.type = 'button';
      b.setAttribute('role', 'menuitem');
      b.textContent = item.label;
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
    const href = DOWNLOADS[act];
    if (href) {
      download(href);
      return;
    }
    const route = act === 'share-md' ? 'md' : 'html';
    this.say('copying…');
    postJSON(`/_galley/share/${route}`, {})
      .then((res) =>
        res.ok
          ? res
              .json()
              .then((v: { path: string }) => this.say(sharedLine(v.path)))
          : res
              .text()
              .then((t) =>
                this.say(t.trim() || `share failed (${res.status})`),
              ),
      )
      .catch((err: unknown) =>
        this.say(
          `share failed: ${err instanceof Error ? err.message : String(err)}`,
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
    b.title = 'the branded page Download HTML saves, refreshed on every save';
    b.addEventListener('click', () => {
      window.open(PREVIEW_HREF, '_blank', 'noopener');
    });
    revise.parentNode.insertBefore(b, revise);
    return b;
  },
};
