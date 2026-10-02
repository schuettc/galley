// arrivals.ts — what changed while the reviewer was reading, as arithmetic.
//
// THE STRIP'S WHOLE JOB IS TO SAY WHAT HAPPENED WITHOUT DOING ANYTHING ABOUT
// IT. The caret and the scroll never move on their own; an arrival is
// announced, never navigated to, and only a `show me` click scrolls. That is
// the entire difference between a co-author and an interruption.
//
// Nothing here touches the DOM or the network, for the same reason nothing in
// rail.ts does: the decisions are over plain values, probe.mjs drives every one
// of them with no browser, and the DOM half in entry.ts only renders what it is
// told and starts the fade.

// NOTHING HERE ASKS `decidable` ANY MORE, and the absence is worth a line.
// `batchRows` lived in this file and was the browser's one reader of the
// server's decidability answer that had no rule at all — it paired a
// revision's runs against the whole pending list with no kind filter, and a
// comment's run went into POST /_galley/accept. That is why
// `suggest.Kind.Decidable` is carried onto the wire and why `rail.ts` exports
// `decidable` to read it. The card `batchRows` fed is deleted, so this module
// no longer offers a verdict about anything — it announces, and announcing is
// not offering. `decidable` is untouched and still governs every surface that
// does offer one; see rail.ts.

// The reviewer's own author name. Nothing of theirs is tracked any more (the
// reviewer's-hand cut), so in practice every pending suggestion is the
// agent's — but the filter stays: announcing anything of the reviewer's back
// to them would be the editor telling them what they just did, and a mark
// attributed 'court' loaded from an old document must never be announced.
const SELF = 'court';

// How long the strip stays before it fades. THE STRIP IS NOT THE RECORD — the
// census count is, and it does not fade. Someone who looked away must still be
// able to find what arrived. Handoff §4: "Strip auto-fades after 8s — the
// census count is the durable record."
export const ARRIVAL_FADE_MS = 8000;

// Handoff §11, verbatim. The suffix is what tells the reviewer the strip is
// about to go and where the fact survives.
export const ARRIVAL_SUFFIX = 'fades · the count keeps it';

/**
 * diffPending says what is new since the last time we looked, and what is gone.
 *
 * KEYED BY RUN, not by index and not by text. An ordinal renumbers the moment a
 * neighbouring suggestion resolves, and two suggestions can read exactly the
 * same — run is the one coordinate that means the same thing across two polls
 * of the same session.
 *
 * The reviewer's OWN work is never an arrival. Their edits apply directly and
 * never enter the pending set at all now; the author filter stays for the
 * mark loaded from an old document that still reads 'court', because
 * announcing the reviewer to the reviewer would be the editor narrating their
 * own hands.
 *
 * @param prev the pending set as last seen
 * @param next the pending set now
 */
export function diffPending(
  prev: Arrival[] | null | undefined,
  next: Arrival[] | null | undefined,
  opts?: { self?: string },
): { arrived: Arrival[]; resolved: (string | undefined)[] } {
  const self = (opts && opts.self) || SELF;
  const before = new Set((prev || []).map((s) => s.run).filter(Boolean));
  const after = new Set((next || []).map((s) => s.run).filter(Boolean));
  const arrived = (next || []).filter(
    (s) => s.run && !before.has(s.run) && (s.author || '') !== self,
  );
  const resolved = (prev || [])
    .filter((s) => s.run && !after.has(s.run))
    .map((s) => s.run);
  return { arrived, resolved };
}

// The shape of a pending-payload entry, in the loose form this module has
// always read it: every field optional, because an arrival arrives over JSON
// and this file's whole discipline is treating what it cannot see as absent
// rather than assuming a shape it hasn't checked.
type Arrival = {
  run?: string;
  author?: string;
  kind?: string;
  section?: string;
};

// The article a kind takes, so the sentence reads as English rather than as a
// field name. An unknown kind falls back to "edit", which is what every kind is
// a species of.
// A substitution is ONE change, not a delete followed by an insert, so it
// takes a word of its own: "a delete" would name half of what arrived and
// send the reviewer looking for the wrong thing.
const KIND_WORD: Record<string, string> = {
  insert: 'an insert',
  delete: 'a delete',
  replace: 'a replacement',
  comment: 'a comment',
};

/**
 * ARRIVAL_AGENT is what the strip calls a proposer it cannot name.
 *
 * `the agent` and never `claude`, which is the word this sentence carried for
 * its whole life. THE PARTY HAS ONE NAME EVERYWHERE ELSE: every card head reads
 * `REPLACE · AGENT · JUST NOW`, the standing sentence reads "the agent
 * proposes", and `galley suggest --author` defaulted to `agent` and took ANY
 * name — so a proposal filed by `galley suggest --author dana` was announced as
 * having come from claude, on the same screen as three cards saying DANA. The
 * strip was the only surface naming a vendor, and it named the wrong one as
 * soon as anybody used the flag the CLI documents.
 */
export const ARRIVAL_AGENT = 'the agent';

/**
 * arrivalAuthor is WHO the strip says filed what arrived.
 *
 * One arrival: its own author. Several: the one author they share, or
 * `the agent` when they do not — a batch from two parties has no single name
 * to put in front of one verb, and picking the first would attribute the
 * other's work to it. An arrival with no author at all (a mark parsed out of a
 * file, which CriticMarkup gives nowhere to record one) is the same case: the
 * strip has no name to use, so it uses the general one.
 *
 * The reviewer's own work never reaches here — diffPending filters it — so the
 * name in this sentence is always somebody else's.
 */
export function arrivalAuthor(arrivals: Arrival[] | null | undefined): string {
  const names = new Set((arrivals || []).map((a) => (a && a.author) || ''));
  if (names.size !== 1) {
    return ARRIVAL_AGENT;
  }
  const [only] = [...names];
  return only || ARRIVAL_AGENT;
}

/**
 * arrivalMessage is the sentence the strip shows. Handoff §11's shape, with
 * the proposer NAMED rather than assumed:
 *
 *   one:       "{who} suggested an insert in §{section}, below your viewport"
 *   coalesced: "{who} suggested {n} edits while you read — show me steps
 *               through them"
 *
 * `{who}` was the literal string `claude` in both, which is the one place in
 * the product that named a vendor — see ARRIVAL_AGENT and arrivalAuthor.
 *
 * "below your viewport" is fixed copy, not a measurement. The strip only ever
 * appears for an arrival the reviewer cannot see, and the spec spells the
 * out-of-sight direction one way; inventing "above" for the other case would be
 * a second sentence nobody has approved.
 *
 * A single arrival whose section cannot be named drops the "in §…" clause
 * rather than printing an empty one — a document with no headings has no
 * section, and "in §, below your viewport" is not a sentence.
 */
export function arrivalMessage(arrivals: Arrival[] | null | undefined): string {
  const list = arrivals || [];
  if (list.length === 0) {
    return '';
  }
  const who = arrivalAuthor(list);
  if (list.length > 1) {
    return `${who} suggested ${list.length} edits while you read — show me steps through them`;
  }
  const one = list[0];
  const kind = (one.kind && KIND_WORD[one.kind]) || 'an edit';
  const where = one.section ? ` in §${one.section}` : '';
  return `${who} suggested ${kind}${where}, below your viewport`;
}

/**
 * arrivalNeedsStrip decides which of the two announcements an arrival gets.
 *
 * Anchor on screen with the rail showing: the card slides in and the text
 * flashes, and that is the whole announcement — a strip on top of a change the
 * reviewer can already see is noise.
 *
 * Anchor off screen, unlocatable, or rail collapsed: there is nothing on screen
 * to notice, so the census count pulses and the strip says what landed.
 *
 * @param arrival the mark's top in VIEWPORT coordinates, or null when the
 *   document does not show it yet
 * @param viewport
 */
export function arrivalNeedsStrip(
  arrival: { top: number | null } | null | undefined,
  viewport: { height: number; railVisible: boolean } | null | undefined,
): boolean {
  if (!viewport || !viewport.railVisible) {
    return true;
  }
  const top = arrival ? arrival.top : null;
  if (top === null || top === undefined) {
    return true;
  }
  return top < 0 || top > (viewport.height || 0);
}

/**
 * queueArrivals appends what just landed to what is still unseen, keyed by run
 * so a poll that reports the same arrival twice does not queue it twice.
 *
 * The queue is what `show me` steps through, and it is deliberately not the
 * rail's card list: a card can be decided, scrolled past, or collapsed out of
 * existence, and none of those mean the reviewer has seen what arrived.
 */
export function queueArrivals(
  queue: Arrival[] | null | undefined,
  arrivals: Arrival[] | null | undefined,
): Arrival[] {
  const seen = new Set((queue || []).map((a) => a.run));
  const out = (queue || []).slice();
  for (const a of arrivals || []) {
    if (a.run && !seen.has(a.run)) {
      seen.add(a.run);
      out.push(a);
    }
  }
  return out;
}

/**
 * holdLabel is what the ⏸ button says. Handoff §4: hold queues arrivals and
 * the button becomes "▶ release · n".
 *
 * WHILE HOLDING, THIS COUNT IS THE ONLY NOTICE AN ARRIVAL GETS. No strip, no
 * pulse, no badge — the reviewer just said they did not want to be told, and a
 * number on the button they pressed is the smallest thing that can still be
 * honest about what is waiting.
 */
export function holdLabel(holding: boolean, n: number): string {
  return holding ? `▶ release · ${n}` : '⏸ hold';
}

/**
 * nextArrival is one `show me` click: the first queued arrival that is still
 * pending, and the queue with it removed.
 *
 * Filtering against the live pending set is the point. An arrival decided from
 * the CLI, or accepted by the reviewer from its card, is no longer somewhere to
 * go — and stepping to a run that is not in the document any more is how a
 * `show me` button starts answering "cannot locate it on the page".
 */
export function nextArrival(
  queue: Arrival[] | null | undefined,
  pending: Arrival[] | null | undefined,
): { arrival: Arrival | null; queue: Arrival[] } {
  const live = new Set((pending || []).map((s) => s.run).filter(Boolean));
  const rest = (queue || []).filter((a) => live.has(a.run));
  if (rest.length === 0) {
    return { arrival: null, queue: [] };
  }
  return { arrival: rest[0], queue: rest.slice(1) };
}
