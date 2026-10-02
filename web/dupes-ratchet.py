#!/usr/bin/env python3
"""Fail when web/ gains a clone group.

Reads `fallow dupes --format json` on stdin and compares the clone-group count
against a ceiling.

THE 52 STANDING GROUPS ARE ALMOST ENTIRELY A DECISION. The overwhelming
majority of the instances are in the NINE `.mjs` gate harnesses — loop,
layers, preflight, typing, probe, rounds-ux, page, align, and now
livestructure — and those are deliberately standalone with no cross-file
imports: each boots its own server, builds its own fixture and carries its own
assertion helper.

page.mjs is the seventh, and it added ten groups (33 → 43) for exactly the
reason the six before it did: it mirrors loop.mjs's two-actors-one-document
shape — server spawn, agent child, JSONL journal, `select`/`check` helpers —
because a page-backed review IS that loop over a derived document, and the
brief for it said in so many words to mirror loop.mjs. The duplication is the
same accepted price, paid once more.

Extracting a shared harness module was considered during the module-map work
and REFUSED, because it would add exactly the coupling the independence buys —
and four of these gates were dead for weeks earlier in this project precisely
because nothing forced them to stay runnable alone. Duplication is the price of
that property and it is worth paying.

align.mjs is the eighth, and it added three groups (43 → 46), all of them the
same harness: the fixture copy, the `galley edit` spawn with its stdout drain,
the come-up poll, the chromium launch, the `check` helper and the teardown that
kills the browser and the server. Its own body — one geometry reading of both
panes and six claims about it — is in no group at all, which is the shape the
argument above predicts: the harness repeats, the assertions never do.

livestructure.mjs is the ninth, and it added eight groups (46 → 54) — all of
them gate harness, and mostly shared with page.mjs, because it IS page.mjs's
two-actors-one-document shape (server spawn, an agent child driving real
rounds, the JSONL journal, the `check`/`edit`/`waitFor` helpers) carried once
more, this time to prove a STRUCTURAL round: the agent answers by editing the
.html, galley re-extracts at the round boundary and reloads the reviewer. Every
new instance is that harness; the reviewer-side assertions are in no group. The
same accepted price, for the one gate that can only be written against the real
CLI+browser loop — which is exactly why it caught a structural-only round
silently failing in the product when every hand-projected Go test passed.

Two groups LEFT (54 → 52) when the settled region, the overall card's
collapse and the rail's collapse were deleted as code nothing called: both
were probe.mjs's fake-storage and hostile-storage blocks, written once per
persisted flag, and the flags are gone.

THE OTHER TWO INSTANCES ARE ONE GROUP IN composer.ts, and they are the only
duplication in product code anywhere under web/. It is a `postJSON` +
`.then(res => …)` shape appearing twice in one file — small, real, and not
covered by the harness argument above. Counted here rather than excused,
because a bound that quietly folds a real finding into a justified population
is how a number stops meaning anything.

So the ceiling stops the number GROWING without demanding it shrink. A
forty-seventh group means duplication somewhere new, which is worth looking at
even if the answer turns out to be the same one.
"""
import json
import sys

BOUND = 52

data = json.load(sys.stdin)
groups = data["clone_groups"]
stats = data.get("stats", {})
print(
    f"dupes: {len(groups)} clone groups — bound {BOUND}"
    f"  ({stats.get('duplicated_lines', '?')} of {stats.get('total_lines', '?')} lines)"
)
# The standing 46 live in exactly these files. A new clone group is far more
# likely to touch a file outside that set than to be a forty-seventh harness
# repeat, so name those first — printing the whole population on failure buries
# the one group that is actually new.
KNOWN = {
    "loop.mjs",
    "layers.mjs",
    "preflight.mjs",
    "typing.mjs",
    "probe.mjs",
    "rounds-ux.mjs",
    "page.mjs",
    "align.mjs",
    "livestructure.mjs",
    "composer.ts",
}

if len(groups) > BOUND:

    def files_of(g):
        return sorted({i["file"] for i in g.get("instances", [])})

    fresh = [g for g in groups if not set(files_of(g)) <= KNOWN]
    shown = fresh or sorted(groups, key=lambda g: -g.get("line_count", 0))[:5]
    print(
        "  in files outside the standing set:"
        if fresh
        else "  nothing outside the standing set — largest groups, look for a new one:"
    )
    for g in shown:
        print(f"  {g.get('line_count', '?')} lines across {files_of(g)}")
    sys.exit(1)
