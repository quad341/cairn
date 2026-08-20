# Release Gate: crn-v6jix

**Bead:** crn-v6jix (deploy) — source: crn-0mdsv (review), build: crn-evw98.3, parent design: crn-evw98
**Commit:** `50cdb9195654fb5c87c914b095e88b31f25262ce`, cut onto `deploy/crn-v6jix-gate`
**Date:** 2026-08-20

## Background

`crn-pip8`'s `OverriddenDuplicateOf` pattern links entries only when they
share a `topic_key` (an inferred same-topic match). This change adds a
distinct, explicit `Corrects` field — author-declared via `--corrects`,
followed at read time regardless of `topic_key` — so a lookup on an
original entry redirects to whatever explicitly corrects/supersedes it,
even when that corrector lives under a different topic. This is facet
"cross-topic-key Corrects/Supersedes" of `crn-evw98`'s two-facet design
(the other facet, branch-aware discovery for pending `remember/*` reviews,
is `crn-evw98.1`/`.2` — independent, already merged via #123/#125, no
overlap with this change).

Four commits: RED (`b565913`) / GREEN (`c5e9e79`) for the core
`Corrects`/`FindCorrection` mechanism and `cairn get` redirect, a post-green
self-review nilnil lint fix (`e576acf`), and a round-2 fix (`50cdb91`)
addressing the sole review-round-1 gap — the plain-text redirect notice
didn't lead with a combined line naming both ids. 8 files touched,
299 insertions / 12 deletions.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | `crn-0mdsv` round 2 verdict: *"pass"*, metadata `deploy_commit: 50cdb9195654fb5c87c914b095e88b31f25262ce` — exact match to `DEPLOY_SHA` (R = D). Independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as transcribed text. Round 1 (verdict: request-changes, one presentational gap on criterion 5) is superseded by round 2's full independent re-verification, not merely referenced. |
| 2 | Acceptance criteria met | PASS | Verified directly via `git diff <merge-base>..50cdb919 -- cmd/commands.go internal/cairn/entry.go`, not the review write-up alone: `Entry.Corrects` field added (TOML `corrects,omitempty`); `FindCorrection` queries `WHERE corrects = ?` with **no topic_key filter** — matches crn-evw98.3's design exactly ("followed... regardless of topic_key"); explicitly single-hop and most-recent-wins by design (doc comment); bound `?` param, no injection surface. `getCmd`'s plain-text branch now prints `NOTE: <orig> redirected to <corrector>` **before** the id:title line — matches round 1's criterion 5 gap resolution exactly, confirmed by direct code read, not just trusting the round-2 review note. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-v6jix-gate` (cut from `50cdb919...`), not trusting the reviewer's report alone: `go test ./... -race -count=1`, exit 0, **7/7 packages ok**. All 11 diff-relevant tests re-run individually by name with `-v -race`: `TestGetJSONReportsRedirectedFrom`, `TestGetJSONOmitsRedirectedFromWhenNoCorrectionExists`, `TestGetRedirectsToCorrectionWhenOriginalIsRequested`, `TestGetDoesNotRedirectWhenNoCorrectionExists`, `TestRememberCorrectsSetsFieldAndPrintsLine`, `TestRememberCorrectsRefusesUnknownTargetBeforeAnyStoreWrite`, `TestEntryCorrectsRoundTripsThroughTOML`, `TestFindCorrectionReturnsNilWhenUncorrected`, `TestFindCorrectionFollowsExplicitCorrectsLinkRegardlessOfTopicKey`, `TestFindCorrectionPicksMostRecentWhenMultipleEntriesCorrectTheSameID`, `TestFindCorrectionDoesNotFollowChains` — all `--- PASS`. |
| 4 | No open HIGH findings | PASS | `bd list --status open` / `--status in_progress` filtered to this chain (`crn-v6jix`, `crn-0mdsv`, `crn-evw98.3`, `crn-evw98`) shows only `crn-evw98` (open — the independent facet-A umbrella, already disclosed as out of scope by `crn-evw98.3`'s own description and by the already-merged #123/#125) and `crn-v6jix` itself (in_progress, this session's own claim). Review's two disclosed findings (unscoped `Find` access-control note, redirect-not-separately-logged) are both explicitly "minor, non-blocking," consistent with approved design, not implementation defects — no HIGH findings anywhere in the chain. |
| 5 | Clean tree | PASS | `deploy/crn-v6jix-gate` cut directly from `50cdb919...^{commit}`; `git status --porcelain` empty before, during, and after the test run. |
| 6 | Clean divergence from main | PASS | `git rev-list --left-right --count origin/main...50cdb919` → `5 4`: `origin/main` has advanced 5 commits past the branch point (including #123/#125, this bead's own facet-A sibling work, and the just-merged #128) while this deploy carries its own 4-commit RED/GREEN/lint/round-2 sequence — not a fast-forward, but `git merge-tree <merge-base> origin/main 50cdb919` against the *current* `origin/main` tip (`93c2593`, confirmed unmoved since this session's fetch) shows **0 conflict markers**. No rebase needed. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 50cdb919 crn-v6jix crn-0mdsv crn-evw98.3 crn-evw98` → rc=0: no `.claude/**` paths touched, and all four non-merge commits in `origin/main..50cdb919` (`b565913` red, `c5e9e79` green, `e576acf` lint fix, `50cdb91` round-2 fix) cite `crn-evw98.3` in their message. No stray commits. |

## Verdict: 7/7 PASS — proceeding to PR, self-merge authorized.

## Process notes

1. **Merge authority — self-merge under the REINSTATED standing
   authorization**, not mayor-escalated. This bead's own description
   carries the same generic *"GitHub remote detected — cut isolated
   deploy/\<bead-id\>-gate branch from main, open PR, route merge-request to
   mayor/mpr. No self-merge"* boilerplate as the three precedent beads
   (`crn-e6pc7`/`crn-daxbq`/`crn-y0caj`) and, unlike `crn-ybgbs`, does *not*
   cite `gm-wisp-2yhv7u` directly in its own text. This is the exact,
   already-understood stale-copy pattern: mayor ruling `gm-wisp-2yhv7u`
   (2026-08-19 03:01:50, independently verified via `gc mail peek` during
   this session's `crn-ybgbs` gate, not re-fetched here since the ruling
   text itself hasn't changed) explicitly reinstated deployer self-merge for
   `quad341/cairn` — because this repo is not covered by `mpr` at all — and
   explicitly pre-authorized *not* re-escalating merely because this exact
   boilerplate recurs in future bead bodies: *"that is now a KNOWN,
   understood stale-copy issue."* `crn-v6jix` raises no new, distinct
   objection beyond that boilerplate, so this is not a fresh signal —
   proceeding under the same standing authorization applied to `crn-ybgbs`,
   re-checked against the same 4 conditions immediately before merging (not
   assumed to carry over from the gate above):
   1. Gate 7/7 PASS — this document.
   2. PR state confirmed via a **direct**, fresh `gh pr view --json` read
      immediately before merging.
   3. CLEAN/MERGEABLE with both required checks (`build-test`, `lint`)
      COMPLETED/SUCCESS.
   4. No `--auto` arming — merge deliberately with plain
      `gh pr merge <n> --squash`, then independently verify
      `state=MERGED`/`mergedAt` non-null afterward (FR-03: never trust exit
      code alone).

   Anything short of all four on the fresh pre-merge read routes to mayor
   instead, same as always.

2. **Branch target:** the review was performed against a specific reviewed
   SHA, not a shared builder branch tip. `deploy/crn-v6jix-gate` was cut
   directly from `50cdb919...^{commit}` via `resolve_deploy_branch_target`,
   guarded first by `assert_deploy_ancestry_scope` (criterion 7 above).

3. **SHA integrity:** `DEPLOY_SHA` was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran, per the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.
