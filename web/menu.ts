// web/menu.ts owns the right-click menu in the document.
//
// WHY THERE IS A MENU AT ALL, AND WHY IT IS NOT A POPUP WITH ONE THING IN IT.
// Capture left the rail (see the rail's own header — the rail holds live work
// only, and a `+ add` button needs nothing and is beside nothing). It went two
// places: a control in the bar, where every other document-level action already
// lives, and here. `contextmenu` was UNCLAIMED — no handler existed anywhere in
// web/ — so the gesture cost nothing to take, and Court asked for more items on
// it later. A one-item popup would have to be thrown away to grow; a list of
// `MenuItem`s does not, so this is built as a real menu on the first item.
//
// THE HAZARD IT IS BUILT AGAINST, STATED BEFORE IT IS DISCOVERED AGAIN.
// The menu is `position: absolute` on `document.body` and MUST NEVER be a child
// of `.ProseMirror`. A throwaway surface appended inside the editable subtree
// was parsed as prose and written to the author's file: a fixture went from 1
// pending suggestion to 81 and the `.md` gained the card's text, its placeholder
// and both verb labels as paragraphs. Everything that floats over this document
// — the composer, the refusal note, this — lives on `body` for that one reason.
//
// AND IT HANGS OFF THE BAR'S MEASURED HEIGHT. `openMenu` takes a `frame`
// (App.chromeFrame — the band of the window the prose is readable in, measured
// per call) and clamps into it. A constant here would be wrong the first time
// the bar folds, which is the defect `--gly-bar-h` exists to have stopped.
//
// It draws nothing that DISPLACES: absolute on body, one write of `top`/`left`,
// no transition. Opening the menu moves nothing that was not clicked.

/** One row. `run` is what the row does; `detail` is the muted second line that
 * says what the row is ON, which is how the same verb reads differently for a
 * selection and for the whole document without being two verbs. */
export interface MenuItem {
  label: string;
  detail?: string;
  run: () => void;
}

export interface DocMenu {
  root: HTMLElement;
  list: HTMLElement;
}

/** How far the menu sits from the pointer, and from the edge it is clamped to.
 * One number for both, because they are the same claim: a surface flush against
 * the thing it belongs to reads as part of it. */
const MENU_GAP = 6;

/** makeMenu builds the one menu element. ONCE, on `body`, and never rebuilt —
 * only its rows are. See the header for why `body` and nowhere else. */
export function makeMenu(): DocMenu {
  const root = document.createElement('div');
  root.className = 'gly-menu';
  root.hidden = true;
  const list = document.createElement('div');
  list.className = 'gly-menu-list';
  list.setAttribute('role', 'menu');
  root.appendChild(list);
  document.body.appendChild(root);
  return { root, list };
}

/** rowFor is one row, built. Split out so `openMenu` reads as the placement it
 * is: a menu that grows gets rows added to a list, not branches added to a
 * builder. */
function rowFor(item: MenuItem, close: () => void): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'gly-menu-item';
  button.setAttribute('role', 'menuitem');
  const label = document.createElement('span');
  label.className = 'gly-menu-label';
  label.textContent = item.label;
  button.appendChild(label);
  if (item.detail) {
    const detail = document.createElement('span');
    detail.className = 'gly-menu-detail';
    detail.textContent = item.detail;
    button.appendChild(detail);
  }
  // Close FIRST, then run. Every item here opens something else, and a menu
  // still on screen over the surface it just summoned is the popover that will
  // not go away.
  button.addEventListener('click', () => {
    close();
    item.run();
  });
  return button;
}

/**
 * openMenu fills the menu, places it, and shows it.
 *
 * @param menu  the one element makeMenu built
 * @param items the rows, in the order they read
 * @param at    where the pointer was, in VIEWPORT coordinates
 * @param frame App.chromeFrame's measured band — `{top, bottom}` in viewport
 *              coordinates, the part of the window the prose is readable in
 */
export function openMenu(
  menu: DocMenu,
  items: MenuItem[],
  at: { x: number; y: number },
  frame: { top: number; bottom: number },
): void {
  const close = () => closeMenu(menu);
  menu.list.textContent = '';
  for (const item of items) {
    menu.list.appendChild(rowFor(item, close));
  }
  // Shown before it is measured: a hidden box has no size, and the flip below
  // reads the menu's OWN height rather than a guessed one — the reason
  // `placeComposer` measures too.
  menu.root.hidden = false;
  menu.root.style.top = '0px';
  menu.root.style.left = '0px';
  const box = menu.root.getBoundingClientRect();
  const right = document.documentElement.clientWidth || window.innerWidth;
  // Down and to the right of the pointer where there is room; flipped back
  // inside the measured frame where there is not. `Math.max` against
  // `frame.top` is last so a menu taller than the frame is clipped at the
  // BOTTOM, where a scroll can still reach it, rather than under the bar.
  const left = Math.min(at.x + MENU_GAP, right - box.width - MENU_GAP);
  const below = at.y + MENU_GAP;
  const top =
    below + box.height <= frame.bottom
      ? below
      : Math.max(frame.top + MENU_GAP, at.y - MENU_GAP - box.height);
  menu.root.style.top = `${top + window.scrollY}px`;
  menu.root.style.left = `${Math.max(MENU_GAP, left) + window.scrollX}px`;
  // The first row takes focus, so the keyboard reaches the menu it opened.
  const first = menu.list.querySelector('button');
  if (first instanceof HTMLElement) {
    first.focus();
  }
}

export function closeMenu(menu: DocMenu): void {
  menu.root.hidden = true;
  menu.list.textContent = '';
}

export function menuOpen(menu: DocMenu): boolean {
  return !menu.root.hidden;
}

/**
 * moveMenuFocus is the arrow keys, and it is the only reason the rows are
 * buttons rather than divs: a menu the mouse can reach and the keyboard cannot
 * is half a menu.
 *
 * @param step +1 for the next row, -1 for the previous; it wraps.
 * @returns whether the key was ours to take
 */
export function moveMenuFocus(menu: DocMenu, step: number): boolean {
  const rows = [...menu.list.querySelectorAll('button')];
  if (!rows.length) {
    return false;
  }
  const here = rows.indexOf(document.activeElement as HTMLButtonElement);
  const next = rows[(here + step + rows.length) % rows.length];
  next.focus();
  return true;
}
