// web/history.ts owns History: the versions door, entering and leaving the
// reading mode, restoring a version as a fresh draft, and the arrival strip
// that says a round came back.
//
// It is a MIXIN — an object of methods `Object.assign`ed onto `App.prototype`
// in entry.ts — not a class of its own, so every method here still reads and
// writes `this` on the live App instance exactly as it did before the move
// (`this.editor`, `this.census`, `this.versionsPanel`, `this.bar`, and so on).
// `this` IS TYPED AGAINST `AppShell` (web/appshell.ts) — see that file's own
// header for the this-typing decision.
//
// THIS WAS THE PARTITION'S SINGLE BIGGEST OPEN CALL. Most of these methods'
// DOM writes land on bar-owned elements — the versions chip, the census strip
// — so a partition by pixels would file them under the bar. They are not bar
// chrome. web/versions.ts's own header states History "is a READING MODE and
// not a second application," and a reading mode filed under a module named
// for the bar buries that distinction exactly where the next reader will not
// look.
//
// paintVersionsButton also paints `this.bar.versions`, the narrow bottom-bar
// chip owned by web/sheet.ts. That cross-group write is expected: mixins all
// land on one shared prototype, so writing another module's element here is
// ordinary, not a boundary violation.
//
// readArrival is driven from readRevise, which lives in web/verdict.ts now. A
// method here being called from a method in another mixin is fine — same
// prototype, same `this`.

import {
  arrivalSaid,
  arrivedSaid,
  STAGE_ROUNDS,
  VERSIONS_LABEL,
  VERSIONS_NAME,
} from './versions.ts';
import type { AppShell, ReviseView } from './appshell.ts';

export const historyMethods = {
  // --- the rounds ---
  //
  // ONE LABEL, ONE WIDTH. A control whose label changes on its own click slides
  // its neighbours out from under the cursor that just pressed it — measured
  // three separate times on this bar — so this one says the same thing open or
  // shut, and reports its state through aria-expanded and a class.
  makeVersionsButton(this: AppShell): HTMLButtonElement {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'gly-versions-open';
    b.textContent = VERSIONS_LABEL;
    // The visible label and accessible name deliberately agree. A glyph used
    // here survived implementation but failed discovery in the first real
    // review: the four views and history existed behind a symbol nobody read.
    b.setAttribute('aria-label', VERSIONS_NAME);
    b.title = `${VERSIONS_NAME} — every round of this document, and what was asked for`;
    b.setAttribute('aria-expanded', 'false');
    b.addEventListener('click', () => this.toggleVersions());
    // Instructions and History are peers, so they live in one group and read
    // as view switches rather than unrelated controls at opposite ends.
    this.census.root.appendChild(b);
    return b;
  },

  // THE INSTRUCTIONS DOOR OPENS THE SURFACE THAT EXISTS AT THIS WIDTH, and for
  // a whole phase below the breakpoint it opened nothing at all.
  //
  // Measured on the built binary at 620×900, before this line existed: the
  // click was DELIVERED (`elementFromPoint` at the control's centre returned
  // `.gly-bar-count`, playwright's click resolved in 9ms with no timeout) and
  // this method RAN (instrumented, `openInstructions called 1 time(s)`) — and
  // afterwards `.gly-rail`, `.gly-sheet` and `.gly-versions` were all still
  // `hidden`. Not a harness artefact: a door that answers the press and shows
  // nothing, which is worse than one that refuses, because the reviewer
  // concludes there is nothing behind it.
  //
  // The cause is these two lines read against `railSurfaces`. Below
  // RAIL_MIN_WIDTH there IS no rail — the sheet is the review's whole list down
  // there — and `sheetOpen = false` is precisely the state in which nothing is
  // painted. The old shape was correct while `▤` was the narrow door to the
  // sheet and this was the door to the rail; retiring the icon (makeBottomBar)
  // makes this the only door, so it has to open whichever surface the width
  // actually has.
  //
  // ONE WIDTH TEST, ASKED THROUGH THE ONE ACCESSOR. `this.surfaces()` is
  // `railSurfaces` with the state as it now stands, so `rail` here means
  // exactly "is this a width with a rail" — a second `window.innerWidth >=`
  // spelled in this method is how the two would come to disagree about which
  // surface owns the instructions.
  openInstructions(this: AppShell) {
    if (this.versionsPanel.open) {
      this.versionsPanel.hide();
    }
    this.sheetOpen = false;
    this.sheetOpen = !this.surfaces().rail;
    this.paintSheet();
    this.paintSurfaces();
    this.paintVersionsButton();
    this.census.count.classList.add('is-open');
  },

  enterHistory(this: AppShell) {
    this.historyScroll = window.scrollY;
    // THE NOTICE GOES WHEN THE READING BEGINS. An announcement that a round
    // arrived, still standing over the surface that round is being read on, is
    // a surface telling the reviewer about the thing they are looking at.
    this.hideStrip();
    document.body.classList.add('gly-history-mode');
    this.editor.setEditable(false);
    this.closeSheet();
    this.closeCapture();
    this.paintSurfaces();
    this.scheduleGrips();
    this.census.count.classList.remove('is-open');
    // The primary becomes the way out and the readout says where you are, and
    // both have to be true BEFORE the first frame of the new surface — a bar
    // that still offers Revise over a reading mode is a bar offering a verb the
    // page cannot take.
    this.paintRevise();
    this.paintReadout();
    window.scrollTo({ top: 0 });
  },

  leaveHistory(this: AppShell) {
    document.body.classList.remove('gly-history-mode');
    this.editor.setEditable(!this.sealed);
    this.paintSurfaces();
    this.scheduleGrips();
    this.paintVersionsButton();
    this.paintRevise();
    this.paintReadout();
    this.census.count.classList.add('is-open');
    // ESC RETURNS WITH THE SCROLL PRESERVED, which is the whole reason History
    // is a mode and not a page: the sentence the reviewer was reading is still
    // under the cursor when they come back to it.
    window.scrollTo({ top: this.historyScroll });
  },

  didRestore(this: AppShell, version: number, error: string) {
    if (error) {
      this.say(`restore failed: ${error}`);
      return;
    }
    this.say(`restored from v${version} · not sent`);
    this.openInstructions();
    // Fire-and-forget: a failure here leaves the rail showing pre-restore
    // work for one poll cycle. `tick`'s own `/_galley/rev` check (pending.ts)
    // picks the same change up moments later and repaints correctly, and the
    // reviewer has already been told the restore itself landed — there is
    // nothing a failure here could say that the next poll doesn't say better.
    void this.refreshPending();
  },

  // readArrival is HOW THE REVIEWER FINDS OUT A ROUND CAME BACK.
  //
  // It rides the revise poll the page already makes, and it compares a NUMBER
  // rather than reading a flag the server would have to clear: the server holds
  // no per-reader state, and a second tab is told the same thing independently.
  //
  // THE DEFAULT VIEW IS STILL THE DOCUMENT. Nothing opens over the prose here —
  // the revision is already in the words under the cursor, which is the point of
  // the phase. What this does is mark the door and, when it is pressed, land on
  // THAT round `in place`. See VersionsPanel.showRound.
  readArrival(this: AppShell, d: ReviseView) {
    const n = Number(d.landed || 0);
    const why = d.cannot || '';
    if (this.seenRound === null) {
      this.seenRound = n;
      this.seenCannot = why;
      return;
    }
    if (n === this.seenRound) {
      return;
    }
    const exception = why !== '' && why !== this.seenCannot;
    this.seenRound = n;
    this.seenCannot = why;
    this.arrival = { n, exception, why };
    this.say(arrivalSaid(this.arrival));
    // THE STRIP IS RAISED FROM THE RECORD, NOT FROM THIS PAYLOAD. `/_galley/revise`
    // carries the number that landed and nothing about what it did; the counts
    // the strip prints — the round's ordinal and its `k changes` — are the
    // server's, off `/_galley/versions`, which this refresh is already
    // fetching. THE COUNT NEVER LIES, and a strip that guessed one from what
    // the browser had drawn would be a count derived from a render.
    //
    // It is raised on the refresh's promise rather than beside it, because the
    // record is what the sentence is composed from and the fetch is where it
    // arrives. A refresh that fails says nothing: the readout and the amber
    // door have already told the reviewer a round came back, and a strip with
    // a blank number in it would be worse than no strip. `refresh` itself
    // ends in a bare `.catch(() => {})` (versions.ts), so this chain cannot
    // reject in practice; `void` records the same "fails silently, on
    // purpose" intent this comment already states.
    void this.versionsPanel.refresh().then(() => this.showArrival());
    this.paintVersionsButton();
  },

  // showArrival composes board 1e's sentence and puts it on the strip. It reads
  // the panel's own rounds — the list the record was just refreshed into — so
  // the strip, the History landing and the reading state are all quoting one
  // fetch of one endpoint.
  showArrival(this: AppShell) {
    if (!this.arrival) {
      return;
    }
    this.showStrip(
      arrivedSaid(this.versionsPanel.rounds, this.arrival.n),
      this.arrival,
    );
  },

  toggleVersions(this: AppShell) {
    const panel = this.versionsPanel;
    // Two full surfaces stacked is one surface nobody can reach. The sheet is
    // the review's list and this is the record; opening one puts the other
    // away rather than drawing them over each other.
    if (!panel.open && this.sheetOpen) {
      this.closeSheet();
    }
    // AN ARRIVAL LANDS ON ITSELF, IN PLACE. Opening the record cold is still the
    // document (DEFAULT_VIEW); opening it because something arrived answers the
    // question that made you open it.
    if (!panel.open && this.arrival) {
      const { n } = this.arrival;
      this.arrival = null;
      // showRound's own promise bottoms out in load()'s bare `.catch(() =>
      // {})` (versions.ts) — a fetch failure is already absorbed there and
      // never reaches here to be reported. `void` names that as intentional
      // rather than leaving the click's own promise unmarked.
      void panel.showRound(n, 'inplace');
      this.paintVersionsButton();
      return;
    }
    // OPENING IT COLD IS THE LANDING. The record's two stages answer two
    // different questions — "what has happened to this document" and "what did
    // round N do" — and a door pressed with no news behind it is the first
    // question. A reviewer who left History mid-round and comes back to a
    // reading state they did not ask for would be reading somebody else's
    // answer to a question they had not put.
    if (!panel.open) {
      panel.stage = STAGE_ROUNDS;
    }
    // Same absorption as showRound above: toggle()'s show() path runs
    // through refresh()'s own bare `.catch(() => {})`, so there is nothing
    // left for a handler here to report.
    void panel.toggle();
    this.paintVersionsButton();
  },

  paintVersionsButton(this: AppShell) {
    const open = !!this.versionsPanel.open;
    // THE CHIP IS A DOOR, NOT A READOUT. It carried `History · 3`, and the
    // number was the one thing on it that changed on somebody else's event —
    // a round landing — in the bar whose oldest rule is that nothing may move
    // under the cursor. The record's size is inside the record; what belongs
    // out here is the way in, and whether there is something new behind it
    // (`is-new`, which is paint and takes no width). historyCount is kept and
    // still read — the readout's `round N` is it (paintReadout).
    //
    // AND IT IS TWO ELEMENTS NOW, PAINTED HERE AND ONLY HERE. The narrow bar
    // carries its own History chip beside its own Instructions door (board 1i),
    // and only one of the two bars is ever on screen. A second painter for the
    // second chip would be two answers to "has something arrived" — this
    // repository's most-repeated defect, on the one signal that tells a
    // reviewer the agent came back.
    // PAINT ONLY. A class on either control may never change its BOX: the wide
    // one sits in a full bar between the mode switch and Revise, and a control
    // that grows on somebody else's event slides the neighbour the cursor is
    // resting on. See the bar's own entry in CLAUDE.md — the reserve is a
    // rule, not a habit.
    const arrival = this.arrival;
    const waiting = !open && !!arrival;
    // `waiting && arrival` rather than the bare `waiting` this used to read:
    // `waiting` is a boolean already computed FROM `arrival`, and narrowing
    // does not travel backward through that computation — TypeScript only
    // narrows a value that is itself part of the condition it is guarded by.
    // Naming `arrival` again here is that, and nothing about which branch
    // runs changes.
    const title =
      waiting && arrival
        ? `${VERSIONS_NAME} — v${arrival.n} just arrived; this opens its changes`
        : `${VERSIONS_NAME} — every version of this document, and what was asked for`;
    for (const chip of [this.versionsButton, this.bar.versions]) {
      if (!chip) {
        continue;
      }
      chip.textContent = VERSIONS_LABEL;
      chip.setAttribute('aria-expanded', open ? 'true' : 'false');
      chip.classList.toggle('is-open', open);
      chip.classList.toggle('is-new', waiting);
      chip.title = title;
    }
  },
};
