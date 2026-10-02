# ---- .tools family standard: identical in every family repo ----------------
# `just verify` is exactly what CI runs: this tool's `prepare` (files the build
# needs, e.g. an embedded asset), the family gate (tools-actions go-ci, at the
# version .github/workflows/ci.yml pins), then this tool's `verify-extra`.
# The pre-push hook (lefthook.yml) runs it too, so local and CI never differ.
set shell := ["bash", "-euo", "pipefail", "-c"]

default: verify

verify: prepare gate verify-extra

# Everything: verify plus this tool's slow checks (browser, containers), which
# CI runs as their own required jobs.
verify-all: verify verify-slow

# The family Go gate: gofmt, vet, golangci-lint (family config), race tests,
# cross-build. Fetched once per tools-actions version into ~/.cache.
gate:
    #!/usr/bin/env bash
    set -euo pipefail
    v="$(grep -oE 'go-ci@v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/ci.yml | head -1 | cut -d@ -f2)"
    f="${XDG_CACHE_HOME:-$HOME/.cache}/tools-actions/$v/go-ci/local.sh"
    [ -f "$f" ] || { mkdir -p "$(dirname "$f")"; curl -fsSL "https://raw.githubusercontent.com/schuettc/tools-actions/$v/go-ci/local.sh" -o "$f"; }
    bash "$f"

fmt:
    gofmt -w $(git ls-files '*.go')

# Install the lefthook hooks into this clone's own .git/hooks (once per
# clone). A global core.hooksPath (casebook's recorder) forwards to them;
# plain `lefthook install` refuses to run under one.
hooks:
    #!/usr/bin/env bash
    set -euo pipefail
    d="$(cd "$(git rev-parse --git-common-dir)" && pwd)/hooks"
    git config --local core.hooksPath "$d"
    trap 'git config --local --unset core.hooksPath' EXIT
    lefthook install --force >/dev/null
    echo "lefthook hooks installed in $d"

# ---- galley -----------------------------------------------------------------
# Nothing to prepare: everything the gate builds is committed, including the
# editor bundle (kept honest by `bundle-fresh` below). The slot stays because
# `verify` and ci.yml's gate job both run it.
prepare:

# Tool-specific checks beyond the gate (CI runs this too): the TypeScript gate
# and the committed editor bundle matching web/.
verify-extra: verify-web bundle-fresh

# The editor bundle is COMMITTED, so nothing else notices
# when web/ changes and the bundle does not: the Go tests pass, the binary
# builds, and the browser silently runs last week's editor. mermaid.js is
# checked with the other two; it is the output a page only fetches when it
# holds a diagram, so nobody would find a stale one by opening a document.
bundle-fresh: assets
    git diff --exit-code -- internal/serve/assets/editor.js internal/serve/assets/editor.css internal/serve/assets/mermaid.js \
      || { echo "the committed editor bundle does not match web/: run 'just assets' and commit the result"; exit 1; }

# The browser gates (below): a real chromium, minutes, one at a time. Not in
# `verify` (the push gate); CI runs them as the required `gates` job, and
# `just verify-all` runs everything. The umbrella used to be `verify` itself,
# and the gap cost a red dev: `verify` passed, two PRs merged on it, and
# loop.mjs, which nothing in it ran, had been failing the whole time. The honest
# name for everything is now verify-all, and CI requires the gates either way.
verify-slow: gates

# Version stamp: cmd/galley, this justfile, and the release workflow all target
# the SAME internal/version vars via -ldflags -X, so a local `just build`, `just
# verify`, and a release build report the same thing.
version := `cat VERSION`
commit := `git rev-parse --short HEAD 2>/dev/null || echo none`
date := `date -u +%Y-%m-%d`
ldflags := "-X github.com/schuettc/galley/internal/version.version=" + version + " -X github.com/schuettc/galley/internal/version.commit=" + commit + " -X github.com/schuettc/galley/internal/version.date=" + date

# Worktrees live INSIDE the repo at galley/.worktrees/<name>, which puts the
# BARE repo directory on their ancestor path. Go's VCS stamping walks up past
# the worktree, finds that bare .git, and runs a git command that can only
# fail there:
#
#     error obtaining VCS status: exit status 128
#
# The stamp it would have written is one we already set ourselves, and more
# precisely — version, commit and date all come from ldflags above, on every
# build, local and CI alike. So there is nothing to lose by turning it off,
# and a build that works from a worktree to gain.
buildflags := "-buildvcs=false"

# Build the edit-mode editor bundle from web/ into internal/serve/assets.
#
# A PASS-THROUGH, LIKE `just types`. Every esbuild invocation and the probe now
# live in web/package.json's `build` script, and this recipe spells none of
# them. It used to spell all of them, beside an npm `build` script that spelled
# two of them differently — the same two-things-doing-one-job shape this
# codebase keeps deleting from the product, sitting in the build. The drift was
# not theoretical: `npm run build` omitted --charset=utf8 on both calls, never
# built mermaid.js at all, and never ran probe.mjs, so anyone who reached for
# the ecosystem-normal command got a bundle with escaped copy, a stale
# renderer, and 620 unrun checks. Somebody who knows npm runs `npm run build`;
# somebody who knows this repo runs `just assets`; both reach the same code.
#
# npm is DEV-TIME ONLY. The built editor.js, editor.css and mermaid.js are
# COMMITTED, so neither a `go build` nor a release needs node anywhere near
# it. Run this after changing anything under web/, and commit what it writes.
#
# WHY THE SCRIPTS ARE SHAPED THE WAY THEY ARE. package.json is JSON and cannot
# carry a comment, so the reasoning lives here, next to the recipe that runs it:
#
#   · The CSS goes through esbuild too rather than being copied: one tool, and
#     it minifies and reports a syntax error in the stylesheet the same way it
#     does for the script.
#
#   · mermaid is a THIRD output rather than part of editor.js, because it is
#     several megabytes and editor.js is loaded by every document. A page with
#     no diagram in it must not pay for the renderer; the figure NodeView
#     fetches /_galley/mermaid.js on the first mermaid fence and never
#     otherwise.
#
#   · --charset=utf8 is not cosmetic. esbuild defaults to ASCII output and
#     escapes every non-ASCII character in a string literal, so the
#     reviewer-facing copy — which the handoff spec fixes VERBATIM, em dashes
#     and all — reaches the bundle as `nothing pending \u2014 the document is
#     settled`. probe.mjs asserts those sentences are in what the binary
#     embeds, and it cannot assert a string the build has rewritten. Both files
#     are served with `charset=utf-8` already (see EditServer.Handler), so
#     emitting UTF-8 is what the Content-Type has always claimed.
#
#   · probe.mjs is the last step on purpose — it drives the suggestion plugin
#     without a browser AND checks the bundle it just wrote parses.

# Install web/ dependencies ONCE, and be a near-no-op on every call after the
# first. Every web recipe below depends on this instead of spelling its own
# `npm install`. That mattered: the CI job runs seven of these recipes as
# separate steps, and a plain `npm install` re-resolves the 360-package tree
# for ~2.5 minutes EVEN WHEN node_modules is already populated — so seven of
# them was ~15 minutes of the run doing nothing. The guard skips the install
# when node_modules is present (CI caches it keyed on package-lock.json, so a
# lockfile change is a cache miss and this reinstalls); `npm ci` on a cold
# checkout because it is deterministic against the lockfile.
_web-deps:
    cd web && [ -d node_modules ] || npm ci

assets: _web-deps
    cd web && npm run --silent build

# Regenerate web/wire.d.ts from the Go structs that ARE the wire.
#
# The generator lives in `go test` (internal/serve/wire_test.go) and NOT in a
# node script, which is the whole design: `just verify` is Go-only and always
# runs — in CI, in the pre-push hook, on every machine — so the wire contract
# is checked even by someone who never installs node. That is the property the
# browser gates lacked and the reason four of them went dark for weeks, one of
# them on this exact contract (`pending.suggestions` became
# `pending.instructions` and two gates died reading the old field).
#
# `just verify` FAILS when the committed file has drifted; this is the fix it
# names. Run it and commit what it writes.
wire:
    go test ./internal/serve -run TestTheWireTypeIsCommitted -update

# Typecheck the browser modules that have opted in with `// @ts-check`.
#
# A PASS-THROUGH, DELIBERATELY. The flags live in web/tsconfig.json and the
# command lives in web/package.json's `typecheck` script; this recipe spells
# neither. `just assets` already reimplements web/package.json's own `build`
# script and the two have DRIFTED — the recipe passes --charset=utf8, bundles
# mermaid-entry.js and runs probe.mjs, and the npm script does none of it —
# which is the two-things-doing-one-job shape this codebase keeps deleting from
# the product, sitting in the build. A third instance of it is not how a type
# checker gets added. Somebody who knows npm runs `npm run typecheck`; somebody
# who knows this repo runs `just types`; both reach the same code.
#
# IT IS NOT PART OF `verify`, AND THAT IS NOT AN OVERSIGHT. `verify` is Go-only
# by design, runs where node may not exist, and is what the release path
# depends on; adding a node step would make a `go build` machine unable to run
# the project's own gate. The half of this that belongs in `verify` is the wire
# generator above, because a Go test is exactly what `verify` is made of. This
# half joins the pre-push hook and the CI job that already has node — the same
# one that proves the editor bundle is not stale.
types: _web-deps
    cd web && npm run --silent typecheck

# THE QUALITY STACK web/ DID NOT HAVE, AND THE THREE RECIPES BELOW ARE THE
# BROWSER'S ANSWER TO `fmt`, `fmt-check` AND `lint` ABOVE.
#
# Go has had gofmt, go vet and golangci-lint since the first commit and no Go
# file reaches a commit without passing all three. web/ had a type checker as
# of yesterday and NOTHING ELSE: 14,500 lines of browser client and 17,000 of
# harness with no format check and no linter at all. The owner's framing is
# that this half is just as important as the Go one, and an asymmetry that
# large is what that sentence is about.
#
# PASS-THROUGHS, LIKE `types` AND `assets`. The formatter's settings live in
# .prettierrc, the linter's in web/eslint.config.mjs, and both commands live in
# web/package.json. These recipes spell neither. Somebody who knows npm runs
# `npm run lint`; somebody who knows this repo runs `just lint-web`; both reach
# the same code — the same rule the two recipes above this one now follow, and
# the one `assets` spent a phase breaking.
#
# THEY ARE NOT IN `verify`, FOR `types`' REASON. That recipe is Go-only by
# design, runs where node may not exist, and is what the release path depends
# on. All three join the pre-push hook and the CI job that already has node.

# Format web/ (prettier, in place).
fmt-web: _web-deps
    cd web && npm run --silent fmt

# Verify web/ formatting is clean — `gofmt -l`'s counterpart, for the browser.
fmt-web-check: _web-deps
    cd web && npm run --silent fmt:check

# Static analysis for web/. See web/eslint.config.mjs for the rule set and for
# why it is ESLint — including the five standing findings the `--max-warnings`
# bound in the `lint` script holds the line on.
lint-web: _web-deps
    cd web && npm run --silent lint

# THE CHECK fallow DOES NOT HAVE, AND THE ONE THIS TASK EXISTS FOR.
#
# fallow (`just dead-code`/`dupes`/`complexity` below) walks the import graph
# from web/.fallowrc.json's `entry` list. An entry naming a file that is not
# there is not an error to fallow — it is silently dropped from the count
# `fallow list`/`dead-code` reports, and the analysis runs anyway, over
# whatever roots DID resolve, with no complaint. That is exactly how
# `entry.js` and `mermaid-entry.js` survived the entry.ts/mermaid-entry.ts
# TypeScript rename: fallow was computing reachability from two dead roots
# and reporting the result as if nothing were wrong. Proven by reintroducing
# a bogus entry (`nonexistent-entry.ts`) — `fallow list` reported the SAME
# entry-point count with it present as without, and `fallow dead-code` never
# mentioned it, in human or JSON output, in a passing or a failing run.
#
# So the check is not fallow's to skip: read every path `.fallowrc.json`
# names and fail loudly if one is not a real file. No node needed — this is
# cheap enough, and foundational enough to the other three below, to run with
# nothing but jq.
check-entries:
    cd web && missing=""; \
      for e in $(jq -r '.entry[]' .fallowrc.json); do \
        [ -f "$e" ] || missing="$missing $e"; \
      done; \
      if [ -n "$missing" ]; then \
        echo "web/.fallowrc.json names entry points that do not exist:$missing"; \
        exit 1; \
      fi

# Dead code and dependency-graph issues for web/ — unused exports, unused
# class members, duplicate exports, circular deps, unresolved imports. See
# `.fallowrc.json` for the entry points fallow walks from, and the comment above
# `dead-code-ratchet` below for why this recipe is a local survey and
# `dead-code-ratchet` is the thing that gates.
dead-code: check-entries _web-deps
    cd web && npm run --silent dead-code

# COMPLEXITY, COUNTED ON THE METRICS THAT MEAN SOMETHING HERE.
#
# `just complexity` prints fallow's own verdict and exits non-zero whenever
# anything is over threshold — always — so it cannot be a gate. This recipe is
# the gate, and WHAT IT COUNTS took a correction worth writing down.
#
# FALLOW REPORTS 181 FUNCTIONS OVER THRESHOLD AND 167 OF THEM ARE AN ARTIFACT.
# Those 167 exceed on CRAP ALONE, with cyclomatic complexity from 5 to 15,
# median 6. CRAP is `CC^2 * (1-coverage)^3 + CC`, and this repo has no
# TypeScript coverage instrumentation — so fallow computes it at 0% coverage,
# where it collapses to `CC^2 + CC`. At CC 5 that is exactly 30, the threshold.
# So "critical complexity" in that number means "has five or more branches",
# which is an ordinary function, and the 181 is measuring the ABSENCE OF
# COVERAGE DATA rather than complexity.
#
# It also punished the right behaviour: extracting a helper out of a genuinely
# complex function creates a new small function, which at CC>=5 is a new
# finding, so a real improvement moved the number UP (177 -> 178, measured).
# A gate that fails on the refactor it exists to encourage is worse than none.
#
# SO THE BOUND IS 13 — the findings that exceed a COGNITIVE or CYCLOMATIC
# threshold, the ones where the metric is describing the code. It was 16 until
# #143 ("Break up the five accreted functions") took it to 14, and 14 until
# trail.ts `loadedEntry` was deleted as code nothing called; the ratchet was
# lowered both times, because a bound left above the measurement is not a
# bound. The list is short enough to read and `just complexity-ratchet` prints
# it on a red run — today it is trail.ts `apply`/`reanchor`/`settleEntries`/
# `retractReverted`/`placeEmptyBlocks`, schemacheck.mjs `build`/`checkDrift`,
# rail.ts `carryDrafts`, preflight.mjs `armLoop`/`anchor`, keys.ts `onKey`,
# versions.ts `paintChanges`, bar.ts `paintReadout`. No per-function
# inherent/accreted verdict is recorded anywhere in this repo any more (the
# .superpowers/ assessment this comment used to cite is gone), so this does not
# claim a split — what survives #143 is the count.
#
# Thirteen is a CEILING and is said out loud to be one, in the shape
# eslint.config.mjs uses. A fourteenth is a red build, so nothing new arrives
# under cover of the backlog, and the branch that simplifies one lowers the
# number with it. If TypeScript coverage instrumentation ever lands, CRAP
# becomes meaningful and this should be revisited.
# Dead code and duplication, as no-regression ceilings. Both `just dead-code`
# and `just dupes` print fallow's own verdict and exit non-zero whenever there
# is anything at all, so neither can be a gate. These count instead, against
# bounds whose standing findings are named in the scripts themselves. fallow's
# exit status is therefore ignored (`|| true`, since recipes run with pipefail)
# and the script is the verdict: a fallow that crashed hands it no JSON, and
# json.load fails the recipe.
dead-code-ratchet: check-entries _web-deps
    cd web && { npx fallow dead-code --format json || true; } | python3 dead-code-ratchet.py

dupes-ratchet: check-entries _web-deps
    cd web && { npx fallow dupes --format json || true; } | python3 dupes-ratchet.py

complexity-ratchet: check-entries _web-deps
    cd web && { npx fallow health --format json || true; } | python3 complexity-ratchet.py

# Code duplication / clones across web/.
dupes: check-entries _web-deps
    cd web && npm run --silent dupes

# Cyclomatic + cognitive complexity hotspots across web/.
complexity: check-entries _web-deps
    cd web && npm run --silent complexity

# All three fallow analyses in one pass, plus the entry-point guard. A local
# survey recipe: each of the three exits non-zero whenever there is anything at
# all, hence the `|| true`, hence it cannot gate. The `*-ratchet` recipes above
# are the gated form — see the comments above `complexity-ratchet` and
# `dead-code-ratchet` for what each one counts and why.
analyse: check-entries _web-deps
    cd web && npm run --silent dead-code || true
    cd web && npm run --silent dupes || true
    cd web && npm run --silent complexity || true

# THE MEASURED BOUNDS (2026-08-22, this branch), NAMED THE WAY
# eslint.config.mjs names its five — a number with nothing behind it is not a
# ratchet, it is a mystery. ALL THREE NOW GATE, in the pre-push hook AND in the
# CI job below, alongside `check-entries`. Each sits exactly ON its bound, so
# the next regression is red on the commit that causes it:
#
#   · dead-code-ratchet: 4 findings, bound 4 — 3 unused class members
#     (VersionsPanel.toggle/.showRound, SuggestionUI.restage; all called
#     cross-file, a false positive of fallow's class-member analysis) and 1
#     duplicate export (`BlockRef`, in trail.ts and in the generated wire.d.ts).
#     The unused-EXPORT backlog this started from — 36 issues, 32 of them
#     unused exports — was cleared by narrowing exports, not by deleting code.
#   · dupes-ratchet: 52 clone groups / 137 instances, 3.9% duplication (1778
#     of 45,676 lines), bound 52. 135 of the 137 instances are the standalone
#     `.mjs` gate harnesses, whose independence is deliberate and was refused a
#     shared module on purpose; the other 2 are one real group in composer.ts.
#     See dupes-ratchet.py for how it got from 33 to 52.
#   · complexity-ratchet: 13 functions over a COGNITIVE or CYCLOMATIC
#     threshold, bound 13 — not fallow's headline 181 of 1786, which is 92% an
#     artifact of having no coverage data (see `complexity-ratchet` above).
#     Worst: trail.ts `apply` (cyc 23, cog 31).
#
# The bounds are considered limits and not backlog sizes, which is what took
# them from a survey to a gate: the sweep that cleared the unused exports and
# #143's break-up of the five accreted functions are what earned the right to
# gate on 4 / 33 / 14 (52 and 13 today). A ratchet stops the number GROWING
# without demanding it shrink; the branch that clears a finding lowers the bound with it, the way
# the five `no-unused-vars` findings became the four `--max-warnings` holds.
# `check-entries` above never needed that argument: it has no backlog and no
# threshold, only "the file is there or it isn't".

# skylos — cross-language dead code / secrets / security, covering what fallow
# above does not: Go, plus Python and everything under web/ that isn't part of
# the TypeScript build graph (this repo's own harness scripts, docs assets).
#
# NOT a devDependency and NOT installed by this recipe, unlike the fallow
# trio above — skylos is a pip/uv tool (`uv tool install skylos`, or
# `pipx install skylos`), and Go coverage additionally needs the native
# `skylos-go` engine on PATH: clone github.com/duriantaco/skylos and
#   cd skylos/engines/go && go build -o skylos-go ./cmd/skylos-go
# then put the binary on PATH (`skylos doctor` confirms with "Go engine
# available"). Investigated because skylos reports Go coverage as
# "incomplete" without it — confirmed buildable with the toolchain already on
# this machine, no missing platform support, so the false answer here is "it
# cannot be done" rather than "it does not matter", and it was fixed rather
# than reported as a dead end.
#
# NOT wired into CI or the pre-push hook, and that IS the "does this add
# anything" conclusion the task asked for: with skylos-go working, Go dead
# code measures at 0 (`unused_functions`/`unused_imports`/`unused_variables`
# all empty) — golangci-lint's default `unused` linter, already gating every
# push in `just verify`, already covers exactly this class for Go, so there is
# no gap here to fill with a second tool. skylos's other Go-side checks
# (`--secrets`, `--danger`) were tried against this tree and returned mostly
# noise on a first look — high-entropy strings inside a vendored, minified
# mermaid.js and a docs/design demo bundle, not real secrets — the same shape
# CLAUDE.md's "six assertions reported ok" entry warns against gating on
# without first proving the check discriminates a real defect, which nobody
# has done for these two. So: installable, wired for manual use below,
# deliberately not a gate. A future task that wants secrets/danger scanning
# gets to start from a proven-working engine instead of re-litigating whether
# skylos-go can be built at all.
skylos:
    skylos . --exclude-folder web/node_modules

# A NODE THE BROWSER'S SCHEMA CANNOT BUILD IS DELETED, AND THE PROJECTION
# WRITES THE DELETION TO THE AUTHOR'S FILE.
#
# The generic gate for that whole class. It constructs the SAME TipTap schema
# entry.js builds and replays y-prosemirror's own createNodeFromYElement walk
# over markdown.Parse's real output — so it reaches the browser's verdict with
# NO BROWSER AND NO SERVER, across a generated cross product of every construct
# in every container at every position that changes the answer, plus every .md
# file the repository ships.
#
# It exists because three data-loss bugs were one bug and each was answered with
# a hand-written check for its own shape. A fourth hand-written check is not the
# answer. Nothing else in the tree can see this class: every .md here passes,
# FuzzRoundTrip is Parse<->Serialize only (all eighteen spellings of the
# list-item bug round-trip byte-identically through it), and schema_drift_test.go
# and probe.mjs check node NAMES rather than content rules.
#
# Unlike motion/layers/typing/loop it needs NO chromium and NO server — only
# node, a Go toolchain, and web/node_modules (`cd web && npm install`, the same
# dev-time dependency `just assets` and every other web gate already needs). It
# is not in `verify` because CI has neither node nor those modules.
schema:
    node web/schema.mjs

# Undo survives a server-side mutation. A GATE — it was a probe for one day,
# while the defect it measures was known and deliberately unfixed; see the
# header of web/undo.mjs for what it found and why it asserts now.
undo: build
    cd web && GALLEY="$PWD/../bin/galley" node ./undo.mjs

rounds-ux: build
    GALLEY="$PWD/bin/galley" node web/rounds-ux.mjs

# A page survives the loop, and the shell survives the review. The page-backed
# gate: `galley edit index.html` derives content.md, the agent revises it, and
# index.html re-renders with the reviewed sentence in its wrapper and the shell
# it cannot carry left verbatim. Forces the marker floor (GALLEY_BROWSER unset
# to a missing path, inside page.mjs) so it asserts the CI shape on any machine.
page: build
    GALLEY="$PWD/bin/galley" node web/page.mjs

# A code block takes a block-level instruction, and only from its own grip. The
# fence gate: the fence's grip sits in the gutter at rest, pressing it opens
# the composer in BLOCK mode, sending files one block thread on the
# fence's key and a card lands in the rail beside it — while a SELECTION inside
# the fence still gets the deny line and no range composer. Ends by stopping
# the server and reopening the document, because the note's round trip is
# through the FILE.
codeblock: build
    GALLEY="$PWD/bin/galley" node web/codeblock.mjs

# One grip for every block. The cross-kind gate: every top-level heading,
# fence, table, figure, display-math block and front matter carries one grip
# in the left gutter, visible without hovering, and pressing it opens the block
# composer on that block. The rules that need no browser are web/grips.ts's,
# checked in probe.mjs; this is what only a browser can see.
grip: build
    GALLEY="$PWD/bin/galley" node web/grip.mjs

# The content pane aligns to the live page, and clears outside Both view. The
# page-mode geometry gate: in Both view each content heading carries exactly
# the measured gap to the same heading on the page as padding — floored at
# zero (it only ever pushes down) and capped at 260px, so the worst mismatch
# is a short gap rather than a blank screen — and every spacer is gone in
# Content view and re-measured on the way back. Measured in a real chromium,
# in thresholds rather than exact pixels.
align: build
    GALLEY="$PWD/bin/galley" node web/align.mjs

# A structural round removes a section and reloads the reviewer. The live-loop
# gate: the agent answers a round by editing page.html (remove a section),
# galley re-extracts at the round boundary and reloads the reviewer's live
# editor on the re-derived content — the cut stays cut, wrapper and all, the
# reviewer's words survive a restructure (C1), and a following content-only
# round shows no churn. Drives the real CLI + chromium + a simulated agent;
# the only place the boundary reload is proven end to end.
livestructure: build
    GALLEY="$PWD/bin/galley" node web/livestructure.mjs

# THE MEASUREMENT THE DIFF RESTS ON, RE-RUN.
#
# internal/diff is a port of a spike that settled two things BY MEASURING — that
# sentence granularity is worth having, and that the rule for going inside a
# sentence is PIECES rather than SIMILARITY — and both are statements about a
# distribution over real revisions of real markdown. A port that reads well and
# counts differently is a different algorithm, so the sweep is how the port is
# believed.
#
# It is NOT part of `verify` and cannot be: it needs a git repository with
# markdown history, which CI does not have and which is not this repository's to
# pin. `internal/diff/corpus_test.go` is what `verify` runs instead — the
# spike's own five cases, carried into testdata, with their counts pinned.
#
#     just sweep                    # this repository's own history
#     just sweep /path/to/a/repo    # somebody else's
sweep corpus=".":
    GALLEY_CORPUS="$(cd {{ corpus }} && pwd)" go test ./internal/diff -run Sweep -v -count=1

# Build the binary, stamped the way a release build is.
build:
    CGO_ENABLED=0 go build {{ buildflags }} -ldflags "{{ ldflags }}" -o bin/galley ./cmd/galley

# THE TYPESCRIPT GATE, in TypeScript's own tools: tsc for types, prettier for
# format, eslint for lint, fallow for the three ratchets. Go tooling has no
# business here and none of these is reachable from `go test`.
#
# `check-entries` leads because the ratchets analyse from the roots
# `web/.fallowrc.json` names, and a stale entry makes all three compute from
# the wrong place in silence.
verify-web: check-entries types fmt-web-check lint-web complexity-ratchet dead-code-ratchet dupes-ratchet

# THE BROWSER GATES — a real chromium against a real editor. These are the only
# checks that can see what a reviewer sees, and every other gate in this repo
# is blind to it.
#
# `layers` is deliberately absent: it measures pixels and two of its checks are
# font-dependent, so it stays a local gate run by hand rather than one whose
# red is ambiguous. Run it with `node web/layers.mjs`.
#
# ONE AT A TIME, and that is not caution — they bind fixed ports and running
# two at once produces failures that look exactly like regressions. Two agents
# lost a day to that.
#
# A KNOWN SECOND SPELLING, named rather than hidden: ci.yml runs these same
# gates as separately-NAMED steps, deliberately, so a red check in the PR list
# says which promise broke without anyone opening a log. That is worth the
# duplication, and the duplication is real — a gate added here has to be added
# there too, and vice versa. If a third caller ever appears, collapse them; two
# with a stated reason is the honest shape.
gates: build
    GALLEY="$PWD/bin/galley" node web/loop.mjs
    GALLEY="$PWD/bin/galley" node web/typing.mjs
    GALLEY="$PWD/bin/galley" node web/rounds-ux.mjs
    GALLEY="$PWD/bin/galley" node web/page.mjs
    GALLEY="$PWD/bin/galley" node web/codeblock.mjs
    GALLEY="$PWD/bin/galley" node web/grip.mjs
    GALLEY="$PWD/bin/galley" node web/align.mjs
    GALLEY="$PWD/bin/galley" node web/livestructure.mjs
    cd web && GALLEY="$PWD/../bin/galley" node ./undo.mjs

# Unwrap markdown prose. NOT part of `verify` — that gate is the Go binary,
# and reformatting prose is not something a push should do.
#
# embeddedLanguageFormatting is off in .prettierrc for a reason: with it on,
# prettier reformats code INSIDE fenced blocks, and it silently rewrote a js
# sample's quotes in a plan an implementer copies verbatim.
fmt-md:
    npx --yes prettier@3 --write "*.md" "docs/**/*.md"
