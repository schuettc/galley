// web/bar.ts owns the bar's chrome: the status readout, the census strip,
// which surfaces are on screen at this width, and the mode toggle.
//
// It is a MIXIN — an object of methods `Object.assign`ed onto `App.prototype`
// in entry.ts — not a class of its own, so every method here still reads and
// writes `this` on the live App instance exactly as it did before the move
// (`this.status`, `this.census`, `this.bar`, `this.modeUI`, and so on). `this`
// IS TYPED AGAINST `AppShell` (web/appshell.ts) — see that file's own header
// for the this-typing decision.
//
// THIS IS WHAT REMAINS OF THE BAR once History (web/history.ts), the primary
// action (makeRevise, now in web/verdict.ts) and the seal have their own homes:
// the status line, the census, the surfaces decision, and the mode switch.
// paintSurfaces is the second-most behaviour-dense method in the file after
// paintAnchors — it is the ONLY place any of the three surfaces is shown or
// hidden, so every other piece of code sets state and calls it rather than
// touching `hidden` itself.
//
// The mode vocabulary — MODE_ASK, MODE_LIVE, modeLabel, modeOn, modeTitle,
// nextMode — and the readout's phase vocabulary — PHASE_DRAFT, PHASE_AGENT,
// PHASE_SETTLED, roundPhrase, HANDOFF_NOTE_HELD — move here with the methods
// that are their only callers. entry.ts's constructor still reads MODE_ASK
// to seed `this.mode`, and imports it back from here — the same shape as
// every other mixin wiring, not a cycle: bar.ts never imports from entry.ts.

import { getJSON, postJSON } from './net.ts';
import { railSurfaces } from './rail.ts';
import { age } from './suggestions.ts';
import { roundCards, STAGE_READING } from './versions.ts';
import type { AppShell } from './appshell.ts';

/** THE READOUT SAYS WHERE THE DOCUMENT IS, IN A GRAMMAR THAT ALWAYS FITS.
 *
 * `instructions in this round` was a sentence competing for a cell that
 * ellipsizes at its end, and what it printed on an ordinary window was
 * `instructions in this …` — a fragment that names no state at all. The
 * replacement is a fixed three-part line, `round N · draft · saved`, whose
 * every form is short enough that the cell never has to take any of it away.
 *
 * TWO FACTS AND NOTHING ELSE. WHICH ROUND: the number of rounds this document
 * has been through, which is History's own count read from the server rather
 * than derived here (App.historyCount, fed by VersionsPanel's onCount). Before
 * the first round is cut there is no round to name, and `v1` is what the record
 * itself calls that state — the file as galley opened it. WHERE IT IS: `draft`
 * while it is the reviewer's, `with the agent` while it is not, `settled` once
 * the review has ended.
 *
 * The saved clause is composed separately and stays last, because it is the
 * one part that changes on a timer and the truncation order is oldest-last.
 * See paintReadout. */
export const PHASE_DRAFT = 'draft';
export const PHASE_AGENT = 'with the agent';
const PHASE_SETTLED = 'settled';

export function roundPhrase(rounds: number, phase: string): string {
  const where = rounds > 0 ? `round ${rounds}` : 'v1';
  return `${where} · ${phase}`;
}

/** THE FILE ITSELF GETS ITS OWN LINE, under the bar's row and full width.
 *
 * Changed outside the editor, gone from its path, or unreadable (galley#44):
 * the server's own sentence, since only it knows where the other version went.
 * It was a clause of the readout, which is grey, ellipsizes at its end and puts
 * this last — so `Nothing is being saved.` was the part a reviewer never saw.
 * It is a child of the sticky bar rather than a sibling, so it scrolls with the
 * bar; it hangs below the bar out of flow (see `.gly-disk-notice`), because it
 * arrives unasked and must move nothing. Absent when empty. */
function paintDiskNotice(text: string) {
  let el = document.getElementById('gly-disk-notice');
  if (!el) {
    const bar = document.querySelector('.gly-bar');
    if (!bar || !text) {
      return;
    }
    el = document.createElement('div');
    el.id = 'gly-disk-notice';
    el.className = 'gly-disk-notice';
    el.setAttribute('role', 'alert');
    bar.appendChild(el);
  }
  el.hidden = !text;
  el.textContent = text;
}

/** The handoff's three strings, exported for the same reason every other
 * user-facing sentence here is: the checks read the reviewer-visible copy,
 * not an internal flag. HELD is the parse-failure form — the agent's save is
 * on disk and NOT on this page yet, and silence there reads as a lost save. */
/* HANDOFF_NOTE'S PLAIN FORM IS DELETED, AND WHAT IT SAID IS STILL SAID.
 * `agent is revising — the document is read-only` was the readout's way of
 * naming a state the fixed grammar now names in two words (`with the agent`,
 * see roundPhrase), and printing both would be two things doing one job on a
 * cell with room for neither. The HELD form survives because it is not the
 * same claim: the agent's save is on disk and NOT on this page, which is a
 * failure the grammar has no word for and silence reads as a lost save. */
const HANDOFF_NOTE_HELD =
  'agent is revising — its last save is held (not yet importable)';

// The two modes a session can be in, spelled the same way the Go side spells
// them (serve.ModeAsk / serve.ModeLive) because they cross the wire verbatim.
export const MODE_ASK = 'ask';
export const MODE_LIVE = 'live';

// The narrow type both mode constants share — App.mode's own type, and every
// function below that takes a mode.
export type Mode = typeof MODE_ASK | typeof MODE_LIVE;

// The object makeMode builds: the switch itself and the hold button beside
// it, plus the two stacked labels hold's own box never resizes for. See
// makeMode's own comment for why the two hold labels share one grid cell.
export interface ModeUI {
  toggle: HTMLButtonElement;
  hold: HTMLButtonElement;
  holdOff: HTMLElement;
  holdOn: HTMLElement;
}

/**
 * modeLabel is the SWITCH'S LABEL, and it takes no argument on purpose.
 *
 * ONE LABEL AND A STATE, not two labels. This used to be a two-state badge
 * reading `on ask` / `● live`: two nouns, neither of which says "this is a
 * control", and one of which reads as a riddle to anyone who has not read the
 * spec. "on ask" was handoff §4's copy and it was the copy that failed — the
 * mode is the one control whose entire job is to tell you which world you are
 * in, and it was not doing it.
 *
 * Nothing is lost by naming only the on state. The mode's whole effect is
 * whether `⏸ hold` is on screen; Revise is visible in both states already, so
 * the thing you would do instead of going live is in front of you either way
 * and the off state needs no name of its own.
 *
 * The label is a constant, and the signature says so — a caller that thinks it
 * varies with the mode has misunderstood the control.
 */
export function modeLabel(): string {
  return 'live';
}

/** modeOn is the switch's position — what `aria-checked` reports. */
export function modeOn(mode: Mode): boolean {
  return mode === MODE_LIVE;
}

/**
 * modeTitle is the hover text, and it carries the VERB, because a switch's
 * label is its state and a state cannot also be an instruction. Two facts,
 * both ways: what is happening now, and what a click does about it.
 */
export function modeTitle(mode: Mode): string {
  return mode === MODE_LIVE
    ? 'the agent is woken whenever the document settles — click to stop'
    : 'the agent hears nothing until you press Revise — click to go live';
}

/** nextMode is what clicking the toggle asks the server for. */
export function nextMode(mode: Mode): Mode {
  return mode === MODE_LIVE ? MODE_ASK : MODE_LIVE;
}

export const barMethods = {
  // --- status ---

  // THE BAR HAS ONE READOUT, AND IT IS THE SHELL'S OWN `#gly-status`.
  //
  // There were three. The editor appended `#gly-editor-status` for
  // `connected · saved just now`, the census strip's neighbour
  // `.gly-census-untracked` carried the one asymmetry, and the shell's
  // `#gly-status` carried the reply to a press. All three are readouts, all
  // three sit between the census and the one flexible cell, and all three
  // carry the same basis-0 yield — so on any bar that is not enormous they
  // ellipsize together and the reviewer reads `your edits…`, `connected …`,
  // `revision r…`. Three fragments of three sentences is not more information
  // than one sentence; it is less, and it looks like a rendering fault.
  //
  // WHY THE SHELL'S ELEMENT AND NOT THE EDITOR'S. `#gly-status` exists on an
  // unbuilt checkout, carries `role="status" aria-live="polite"`, and is what
  // edit.html's own script writes `editor bundle missing` into. An element the
  // page has before the bundle runs is the one to keep; the bundle's extra
  // span is the one to stop making.
  //
  // WHAT IT DOES NOT COST. The two elements were separate so that a poll's
  // reassurance could not overwrite the Revise button's only feedback. That
  // guarantee is now a guarantee between two FIELDS — `this.said` is written
  // only by `say`, and the poll writes `savedMs` and `connection` — which is
  // the same property with one box instead of two.
  makeStatus(): HTMLElement {
    const el = document.getElementById('gly-status');
    if (el) {
      return el;
    }
    // No shell (a test harness mounting the editor into a bare page). Make one
    // rather than carrying a null through every painter.
    const made = document.createElement('span');
    made.className = 'gly-status';
    made.id = 'gly-status';
    const bar = document.querySelector('.gly-bar');
    const anchor = bar && bar.querySelector('.gly-spacer');
    if (bar && anchor) {
      bar.insertBefore(made, anchor);
    } else if (bar) {
      bar.appendChild(made);
    }
    return made;
  },

  /** say records what the SERVER said in reply to a press, and repaints.
   *
   * The one writer of `this.said`, and the reason the merge costs nothing: a
   * poll cannot swallow a reply, because the poll's own readings live in their
   * own fields (`savedMs`, `connection`) and paintReadout composes them.
   * `say('')` clears it, which is what the seal does when the verdict becomes
   * the whole story.
   *
   * A POLL REACHES THIS FIELD ONLY ON A STATE EDGE, and never with routine
   * traffic — the distinction the guarantee actually rests on. `readRevise`
   * polls, `readSeal` reads the seal off that payload, and `applySeal` calls
   * `say('')` on a seal and `say(reopenLine(…))` on a reopen. That is correct and
   * deliberately ordered: a page that has just been sealed or reopened has had
   * its whole state replaced, so the last reply to a press is stale and the
   * seal's own line is the newest thing there is to say. What must never
   * happen — a `saved just now` overwriting `revision requested` because 1500ms
   * went by — cannot, and does not go through here at all. */
  say(this: AppShell, text: string) {
    this.said = text || '';
    this.paintReadout();
  },

  /** paintReadout writes the one line, NEWEST CLAUSE FIRST.
   *
   *   reply · round N · draft · connection-if-it-is-not-good · saved
   *
   * The order is the truncation policy, and it is the only design decision in
   * here. The cell ellipsizes at its END (`text-overflow: ellipsis`), so
   * whatever is last is what a tight bar takes away — and the last clause has
   * to be the one whose loss costs least. What must NOT be taken away is what
   * just happened, so a reply goes first; then where the document IS, which is
   * the fact a reviewer navigates by; then how long ago it was written, which
   * is the clause that changes on a timer and says least.
   *
   * THE STANDING SENTENCE IS GONE FROM HERE, AND THAT IS THE POINT OF THE
   * GRAMMAR. `instructions in this round` was the last clause and it printed
   * as `instructions in this …` on an ordinary window — a fragment naming no
   * state, on the one line that is supposed to say where the document is. Every
   * form of what replaced it fits, so nothing here is composed hoping the cell
   * will be generous. The sentence itself is not deleted: the review sheet's
   * head still carries it whole below the rail breakpoint, which is where it
   * always printed in full, and UNTRACKED_NOTE is still its one spelling.
   *
   * THE WIDTH NO LONGER DECIDES WHAT THIS SAYS, which retires the whole
   * breakpoint clause this docstring used to carry — the readout printed one
   * thing above 992px and another below it, so that the sheet's head and the
   * bar could not say the same sentence twice on a phone. There is nothing left
   * to say twice. paintSurfaces still calls this on a resize and the line it
   * writes is now the same at every width.
   *
   * A SEALED REVIEW PRINTS ONLY WHAT THE SEAL SAID. The terminal bar is the
   * record; this line is where a reopen's reason lands and nothing else. */
  paintReadout(this: AppShell) {
    paintDiskNotice(this.diskNotice);
    // HISTORY SAYS WHERE YOU ARE AND THAT THE DRAFT IS SAFE, and it says both
    // in one clause so the mode can never be mistaken for the draft. The whole
    // fixed grammar below — the phase, the connection, the save age — is about
    // the DRAFT, and every one of those clauses is false of a reading mode:
    // nothing here is being saved, and `round N · draft` under a page showing
    // v2 of a document is the readout arguing with the paper.
    if (this.versionsPanel && this.versionsPanel.open) {
      const panel = this.versionsPanel;
      const where =
        panel.stage === STAGE_READING
          ? `reading round ${panel.selectedOrdinal() || panel.selected}`
          : (() => {
              // THE COUNT IS THE CARDS, NOT THE ROWS. `historyCount` is every row
              // the store holds — v1, which is the file as galley opened it and not
              // a round anybody had, and both halves of every exchange. The landing
              // draws one card per exchange, so counting anything else would put
              // the readout ahead of the cards under it. See roundCards.
              const n = roundCards(panel.rounds).length;
              return `${n} ${n === 1 ? 'round' : 'rounds'}`;
            })();
      this.status.textContent = `${where} · draft is untouched`;
      this.status.title =
        'History is read-only — nothing here changes the draft';
      return;
    }
    if (this.sealed) {
      this.status.textContent = this.said;
      this.status.title = '';
      return;
    }
    const parts: string[] = [];
    if (this.said) {
      parts.push(this.said);
    }
    // WHERE THE DOCUMENT IS, before how long ago it was written: the phase is
    // the part a reviewer navigates by, and `saved` is the part that changes on
    // a timer. The cell ellipsizes at its end, so this order is the truncation
    // policy — see roundPhrase.
    parts.push(
      roundPhrase(
        this.historyCount,
        this.approved
          ? PHASE_SETTLED
          : this.handoff || this.reviseWaiting
            ? PHASE_AGENT
            : PHASE_DRAFT,
      ),
    );
    if (this.connection !== 'connected') {
      parts.push(`${this.connection}…`);
    } else if (this.savedMs) {
      parts.push(`saved ${age(new Date(this.savedMs).toISOString())}`);
    } else {
      // Connected with nothing saved yet. `connected` on its own is the
      // reassurance until the first projection lands and replaces it with the
      // stronger form — a document that has been written is better evidence of
      // a live session than a socket state is.
      parts.push('connected');
    }
    if (this.handoff && this.draftError) {
      // The one case the fixed grammar cannot state: the agent's save is on
      // disk and NOT on this page, and silence there reads as a lost save.
      parts.push(HANDOFF_NOTE_HELD);
    }
    this.status.textContent = parts.join(' · ');
    // The full sentence, always, because the cell is allowed to lose the end of
    // it. This is the title the untracked cell used to carry, moved with it.
    this.status.title =
      'everything you do applies to the document as it happens — ' +
      'your instructions go to the agent as one round when you press Revise';
  },

  // --- the census strip ---

  // The census answers HOW MUCH. It is a separate surface from the rail for one
  // reason: the rail can only show what is near, and a number that changes with
  // the scroll position is not a census. Every number here comes from the
  // /_galley/pending payload — the same projection the document is built from.
  makeCensus(this: AppShell): { root: HTMLElement; count: HTMLButtonElement } {
    const root = document.createElement('div');
    root.className = 'gly-census';

    // THE COUNT IS THE DOOR TO THE WHOLE LIST, and it had to become one.
    //
    // It was a `<span>`: a readout, and the only way to the sheet was the
    // narrow layout's bottom bar (`.gly-bar-count`, `.gly-bar-list`), so above
    // the breakpoint the sheet could not be opened at all. That was correct
    // while the sheet was the rail's REPLACEMENT — a surface for the width the
    // rail does not exist at. It stopped being correct the moment the rail
    // stopped holding settled conversations: `↺ reopen` is the only way back
    // from a mis-clicked `✓ resolve`, and it would have lived on a surface no
    // desktop reviewer could reach. Anchorless conversations went the same way.
    //
    // So the sheet is the review's list at every width (railSurfaces), and this
    // is how it is reached at every width — the same element, in the same
    // place, saying the same number it always said. A button rather than a new
    // control beside it, because the bar's fold arithmetic is paid for in
    // pixels and this one already carries the count somebody would click.
    //
    // `.gly-census-count`'s 24ch reserve is unchanged and is still a BOUND: the
    // label's width varies with the counts, and every control to its right sits
    // downstream of it.
    const count = document.createElement('button');
    count.type = 'button';
    count.className = 'gly-census-count';
    count.title = 'show the current draft and its instructions';
    count.addEventListener('click', () => this.openInstructions());

    // R1'S SURVIVING HALF IS NOT A CELL OF THIS STRIP'S ANY MORE, AND NOT A
    // CELL OF THE BAR'S EITHER. The sentence — the design, since the
    // reviewer's-hand cut, rather than an admission of a gap — used to be
    // appended here as `.gly-census-untracked`, a bar cell beside the strip.
    // Its journey was: inside the strip (where its full width sat in the
    // strip's flex basis and decided where the bar folded — 1467px with a
    // nine-character name), then a basis-0 bar cell of its own beside two other
    // basis-0 readouts. That last shape is what Court read off the screen as
    // three truncated fragments. It is the last clause of the ONE readout now;
    // see paintReadout, which owns the sentence, its order and its title.
    //
    // The strip keeps its `flex: 0 0 auto` and everything left in it really is
    // a count or a control, which is what that declaration has always claimed.

    // `✓ all` IS GONE FROM THIS FUNCTION, NOT MERELY UNAPPENDED. It swept
    // every proposal and settled every answered thread through POST
    // /_galley/sweep — one of the twelve endpoints
    // internal/serve/rounds_surface_test.go asserts are 404 — and it was
    // already doubly unreachable: `makeCensus` built it and never put it in the
    // document, and `paintCensus` wrote `disabled = true` over it on every poll.
    // Neither population it acted on exists any more, so nothing takes its
    // place: an instruction is not decided in bulk or singly, it is written,
    // edited or deleted, and then sent as a round. The strip keeps its
    // `flex: 0 0 auto` and its measured layout; the count beside it is the
    // census's one verb now, and it opens the sheet.
    root.append(count);
    const bar = document.querySelector('.gly-bar');
    // BEFORE THE READOUT, not merely before the spacer. The census carries
    // three controls, and the readout after it changes width whenever the
    // server says something — measured: one press of Revise put
    // `revision requested` into the status and slid `✓ all`, the since-retired
    // `✗ all` and the whole-doc handle 140.89px. A readout that pushes a button
    // is the same defect as a button that pushes its neighbour. The bar reads
    // left to right: controls, the ONE readout, the one flexible cell,
    // controls — so every control sits upstream of the only cell that
    // breathes. There used to be three readouts here and the ordering rule was
    // the same; consolidating them changed how much has to be said, not what
    // has to be true.
    const anchor =
      document.getElementById('gly-status') ||
      document.querySelector('.gly-spacer');
    if (bar && anchor) {
      bar.insertBefore(root, anchor);
    } else if (bar) {
      bar.appendChild(root);
    }
    return { root, count };
  },

  paintCensus(this: AppShell) {
    const marked = this.comments.length;
    const parts = [`Instructions · ${marked}`];
    // THE WHOLE-DOCUMENT CONVERSATION IS THE HANDLE'S TO COUNT, and counting it
    // here as well is how `3 pending · 3 threads` came to sit beside
    // `1 doc note` on a document holding three conversations, one of which was
    // that note. Two surfaces, one partition, each naming its own side of it.
    //
    // THIS IS NOT A COUNT OF THE RAIL'S CARDS, and it must not be read as one.
    // It counts every open conversation that is not about the whole file, and
    // the rail cards only the ones with a PLACE in the document — so on a
    // document holding an anchorless thread the bar says one more than the map
    // shows, correctly, and `paintRailCards`' notice is what reconciles them.
    // A version of this comment claimed the sum WAS what the rail holds; it was
    // written when the anchorless cards were still in the rail and it survived
    // them leaving, which is the shape this file's own entries warn about.
    this.census.count.textContent = parts.join(' · ');
    this.census.count.disabled = false;
    if (this.bar) {
      this.bar.count.textContent = parts.join(' · ');
      this.bar.count.disabled = false;
    }
  },

  // --- which surfaces are on screen ---

  // paintSurfaces is the ONLY place any of the three surfaces is shown or
  // hidden. Every other piece of code sets state and calls this, so "rail and
  // sheet are both rendered" is not a bug that can be introduced by adding a
  // fourth caller — railSurfaces decides, once, from the width and the state.
  // surfaces is that decision, asked. It exists because there are now TWO
  // callers — this paints from it, and the bubble asks it whether the rail is
  // already carrying conversations — and two spellings of the same width test
  // is exactly how the two come to disagree about which surface owns a thread.
  surfaces(this: AppShell): {
    rail: boolean;
    bar: boolean;
    sheet: boolean;
    collapsed: boolean;
  } {
    return railSurfaces({
      width: window.innerWidth || document.documentElement.clientWidth || 0,
      sheetOpen: this.sheetOpen,
    });
  },

  paintSurfaces(this: AppShell) {
    const s = this.surfaces();
    const history = !!(this.versionsPanel && this.versionsPanel.open);
    this.rail.root.hidden = history || !s.rail;
    this.bar.root.hidden = history || !s.bar;
    this.sheet.root.hidden = history || !s.sheet;
    // Below the breakpoint `railSurfaces` forces `collapsed` regardless of any
    // control — there is no rail to be beside, so `main` recentres to the
    // document's own measure. See `body.gly-collapsed > main`.
    document.body.classList.toggle('gly-collapsed', s.collapsed);
    // The readout's last clause is width-dependent — the standing sentence is
    // the sheet's head below the breakpoint and the readout's own above it — so
    // crossing the breakpoint has to repaint it. This is the one place that
    // decides what a width means, so it is the one place that can.
    //
    // A WIDTH THAT CHANGES ON A RESIZE IS FINE; ON A CLICK IT IS NOT. This runs
    // on opening the sheet as well, which does not cross the breakpoint, so
    // the clause it writes is unchanged there and the readout's box does not
    // move — which is what motion.mjs holds.
    this.paintReadout();
    if (s.rail) {
      // Hidden cards measure zero, so anything painted while collapsed has to
      // be re-measured the moment the rail comes back.
      this.scheduleAnchors();
    } else {
      // THE CARD GOES WITH THE COLUMN IT IS IN. The capture card is a child of
      // the rail and positioned against the window at the moment it opened; a
      // rail that has just been hidden takes it off screen with it, and leaving
      // it "open" would mean the next width change put a half-typed instruction
      // back at coordinates measured for a window that no longer exists.
      this.closeCapture();
    }
    // The bar's capture door is dead exactly where its card has nowhere to
    // land, and this is the one place that decides what a width means.
    this.paintCaptureVerb();
    // THE LIGHT GOES WHERE THE RAIL GOES. Opening the sheet over the prose
    // takes the band's cards off the page, and the light says *the
    // card over there is about these words* — `litRuns` refuses when there is
    // no card, so this call is only what makes it happen at the moment the
    // surface changes rather than at the next hover.
    this.paintLit();
  },

  // --- the mode toggle ---
  //
  // The toggle says whether the agent is listening at all. Live also earns a
  // ⏸ hold button beside it (handoff §4) — that one is added where it works,
  // not here, because a control that appears and does nothing when pressed is
  // the failure this editor's refusal note exists to avoid.
  makeMode(this: AppShell): ModeUI {
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'gly-mode';
    // role="switch" is the honest markup for a thing with one label and two
    // positions, and it is not decoration: it is what makes the control
    // announce itself as flippable and report which way it is flipped, to the
    // keyboard and to a screen reader, without a second label to keep in step.
    toggle.setAttribute('role', 'switch');
    // The track and its knob. A <span> pair rather than a checkbox, because
    // the button already carries the semantics and a real checkbox here would
    // be a second focusable thing inside one control.
    const track = document.createElement('span');
    track.className = 'gly-switch';
    track.appendChild(document.createElement('span')).className =
      'gly-switch-knob';
    const label = document.createElement('span');
    label.className = 'gly-mode-label';
    label.textContent = modeLabel();
    toggle.append(track, label);
    toggle.addEventListener('click', () => this.toggleMode());

    // THE BAR DOES NOT MOVE WHEN YOU CLICK IT. Hold used to be `display: none`
    // on ask, so turning live on INSERTED a button and slid every neighbour
    // sideways — measured at 92.97px, which put hold itself under the cursor
    // that had just clicked the switch. A second click there would have held
    // the arrivals the reviewer was turning on. The general rule is Court's:
    // a click must never move the thing it landed on, or the thing beside it.
    //
    // So the space is RESERVED and only the visibility is toggled — see
    // paintMode. And because hold's own label changes on its own click
    // (`⏸ hold` becomes `▶ release · 0`, which is wider), the two labels are
    // stacked in ONE grid cell: the button is always as wide as the wider of
    // them, so the swap cannot change its box either. That is exact rather
    // than a reserved magic number, which is why it is worth two spans.
    const hold = document.createElement('button');
    hold.type = 'button';
    hold.className = 'gly-hold';
    const holdOff = document.createElement('span');
    holdOff.className = 'gly-hold-label';
    const holdOn = document.createElement('span');
    holdOn.className = 'gly-hold-label';
    hold.append(holdOff, holdOn);
    hold.addEventListener('click', () => this.toggleHold());

    const bar = document.querySelector('.gly-bar');
    const anchor = document.getElementById('gly-revise');
    if (bar && anchor) {
      bar.insertBefore(toggle, anchor);
      bar.insertBefore(hold, anchor);
    } else if (bar) {
      bar.append(toggle, hold);
    }
    return { toggle, hold, holdOff, holdOn };
  },

  paintMode(this: AppShell) {
    const { toggle, hold } = this.modeUI;
    // The label never changes — see modeLabel. What changes is the POSITION,
    // and it is written once, to aria-checked, with the stylesheet reading it
    // from there. A class saying the same thing in parallel is a second source
    // of truth for one fact, and the two drift the first time one is forgotten.
    const on = modeOn(this.mode);
    toggle.setAttribute('aria-checked', on ? 'true' : 'false');
    toggle.title = modeTitle(this.mode);
    toggle.classList.toggle('gly-on', on);
    // Hold only exists in live mode: on ask nothing arrives unasked, so there
    // is nothing to hold back, and a control that can never do anything in
    // this state is chrome rather than a choice.
    //
    // It KEEPS ITS BOX in both states and only changes visibility, so that
    // flipping the switch moves nothing. A RESERVED BOX IS NOT A BUTTON: a
    // `visibility: hidden` element is out of the tab order and out of hit
    // testing, and it is `disabled` as well — this codebase already learned
    // the mirror of this from the fold clusters, where a card hidden in CSS
    // was still a button to the keyboard.
    hold.classList.toggle('gly-reserved', !on);
    hold.disabled = !on;
    this.paintHold();
  },

  readMode(this: AppShell) {
    getJSON<{ mode?: Mode }>('/_galley/mode')
      .then((d) => {
        if (!d || !d.mode) {
          return;
        }
        this.mode = d.mode;
        this.paintMode();
      })
      .catch(() => {});
  },

  // The request is what changes the mode, not the click. An optimistic flip
  // followed by a failed POST would leave the header claiming a state the
  // server is not in — and the state in question is "is anyone being woken",
  // which is exactly the thing that must not be guessed at.
  toggleMode(this: AppShell) {
    const want = nextMode(this.mode);
    postJSON('/_galley/mode', { mode: want })
      .then((res) => (res.ok ? res.json() : null))
      .then((d: { mode?: Mode } | null) => {
        if (d && d.mode) {
          this.mode = d.mode;
          // Leaving live strands anything held: the ⏸ button is hidden on ask,
          // so there would be cards the rail is not drawing and no control to
          // bring them back. Release first, then repaint.
          if (this.mode !== MODE_LIVE && this.holding) {
            this.release();
            return;
          }
          this.paintMode();
        }
      })
      .catch(() => {});
  },
};
