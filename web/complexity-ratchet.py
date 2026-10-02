#!/usr/bin/env python3
"""Fail when web/ gains a genuinely complex function.

Reads `fallow health --format json` on stdin and counts only the findings that
exceed a COGNITIVE or CYCLOMATIC threshold. Findings that exceed CRAP alone are
ignored, and that is the whole point of this script existing rather than a grep
on fallow's own summary line.

CRAP is `CC^2 * (1-coverage)^3 + CC`. There is no TypeScript coverage
instrumentation in this repo, so fallow computes it at 0% coverage, where it
collapses to `CC^2 + CC` — and at CC 5 that is exactly the 30-point threshold.
So a CRAP-only finding means "this function has five or more branches", which
describes most functions ever written. 161 of fallow's 177 are that.

It also inverted the incentive: extracting a helper out of a complex function
creates a new small function, which at CC>=5 is a new CRAP finding, so a real
improvement moved the total UP. Measured: 177 -> 178 on an extraction that took
paintChanges from CC19/Cog25 to CC13/Cog18.

If coverage instrumentation ever lands, CRAP becomes meaningful and this should
be revisited.
"""
import json
import sys

BOUND = 13

data = json.load(sys.stdin)
real = [f for f in data["findings"] if f["exceeded"] != "crap"]
print(f"complexity: {len(real)} over a cognitive/cyclomatic threshold — bound {BOUND}")
if len(real) > BOUND:
    for f in sorted(real, key=lambda f: -(f["cyclomatic"] + f["cognitive"])):
        print(f"  {f['path']}:{f['name']}  cyc {f['cyclomatic']}  cog {f['cognitive']}")
    sys.exit(1)
