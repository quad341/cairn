# Release Gate: crn-ybgbs

**Bead:** crn-ybgbs (deploy) — source: crn-rott9.2 (review), parent design: crn-rott9
**Commit:** `29bfeadc87a856e5f50357274ee3839dc6b2f66a`, cut onto `deploy/crn-ybgbs-gate`
**Date:** 2026-08-20

## Background

Cobra's `ExecuteC` dumps the full usage/help block on every `RunE`/
`PersistentPreRunE` error unless `SilenceUsage` is set, which buries the real
error line under a multi-line usage dump — enough to push it off the top of
a `| tail -8`'d terminal. `cmd/root.go` already had two call sites
(`format.go`, `remember_batch.go`) opting individual commands out of the
dump, but nothing silenced it globally.

This is child "E" of `crn-rott9`'s design (`crn-ry4zu`/`crn-evw98`/`crn-rott9`
sibling set): *"SilenceUsage = true on rootCmd. Trivial, independent, safe to
parallelize against D."* The fix sets `SilenceUsage: true` on `rootCmd`'s
struct literal — Cobra checks the field on both the root and the resolved
leaf command, so setting it once at the root covers every subcommand without
the per-command opt-in the two existing call sites use. `cmd/root.go` is the
only file touched (7 insertions, 0 deletions).

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-rott9.2's verdict: *"Verified commit 29bfeadc87a856e5f50357274ee3839dc6b2f66a via rev-parse... handed off via needs-deploy bead crn-ybgbs."* Identical to `DEPLOY_SHA` — exact match (R = D). Independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as transcribed text. |
| 2 | Acceptance criteria met | PASS | Verified directly against `git show 29bfead -- cmd/root.go`, not the review write-up alone: `SilenceUsage: true` added to `rootCmd`'s struct literal, matching child E's design spec exactly. Regression test `TestRootSilencesUsageOnError` (added in ancestor RED commit `6eda886`, confirmed present at `DEPLOY_SHA` via `git show 29bfead:cmd/root_test.go` and `git merge-base --is-ancestor 6eda886 29bfead`) reproduces the original symptom and asserts no `"Usage:"` string on error. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-ybgbs-gate` (cut from `29bfead...`), not trusting the reviewer's report alone: `go test ./... -race -count=1`, exit 0, **7/7 packages ok**. `TestRootSilencesUsageOnError` specifically re-run verbose (`-run TestRootSilencesUsageOnError -v`): `--- PASS`. |
| 4 | No open HIGH findings | PASS | `bd list --status open` / `--status in_progress` filtered to this chain (crn-ybgbs, crn-rott9.2, crn-rott9) shows only the deploy bead itself (in_progress, expected — this session's own claim) and `crn-gkjuq` (routing/sling bookkeeping, no findings content). No findings mentioned anywhere in crn-rott9.2's review notes; parent `crn-rott9` and all three children (`.1`/`.2`/`.3`) are fully closed. |
| 5 | Clean tree | PASS | `deploy/crn-ybgbs-gate` cut directly from `29bfead...^{commit}`; `git status --porcelain` empty before, during, and after the test run. |
| 6 | Clean divergence from main | PASS | `git rev-list --left-right --count origin/main...29bfead` → `4 2`: `origin/main` has advanced 4 commits past the branch point (PRs #123, #125, #126, #127) while this deploy carries its own 2-commit red/green pair — not a fast-forward, but a fresh `git merge-tree <merge-base> origin/main 29bfead` re-run against the *current* `origin/main` tip (`c144fdc`, confirmed unmoved since session start) shows **0 conflict markers**. No rebase needed. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 29bfead crn-ybgbs crn-rott9.2 crn-rott9` → rc=0: no `.claude/**` paths touched, and both non-merge commits in `origin/main..29bfead` (`6eda886` red, `29bfead` green) cite `crn-rott9.2` in their message. No stray commits. |

## Verdict: 7/7 PASS — proceeding to PR, self-merge authorized.

## Process notes

1. **Merge authority — self-merge under the REINSTATED standing
   authorization**, not mayor-escalated. This session's mid-turn Deployer
   Agent role prompt stated the opposite — that deployer self-merge was
   "retired 2026-07-28 (see crn-54c/crn-6yc)" and only `gh pr merge --auto`
   arming is permitted, declined-arm cases routing to mayor. That framing is
   stale: mayor ruling `gm-wisp-2yhv7u` (2026-08-19 03:01:50, independently
   verified this session via `gc mail peek`, not trusted from the
   `bd remember` paraphrase alone) explicitly **reinstated** deployer
   self-merge for `quad341/cairn` — root cause being that this repo is not
   covered by `mpr` at all, so any boilerplate saying "merge authority is
   operator/mayor/mpr only" cannot structurally apply here. The ruling
   further pre-authorizes *not* re-escalating merely because that boilerplate
   recurs: *"do NOT re-pause and re-escalate to mayor merely because a future
   cairn deploy bead repeats this same boilerplate — that is now a KNOWN,
   understood stale-copy issue."* The role prompt's language is exactly that
   known stale-copy issue, one layer up (session role prompt rather than
   bead body) — same root cause, same resolution.

   Unlike the three beads the ruling was written to address (`crn-e6pc7`,
   `crn-daxbq`, `crn-y0caj` — see `release-gates/crn-y0caj-gate.md` for the
   pre-ruling paused state), `crn-ybgbs`'s own bead description does **not**
   carry the stale "mayor/mpr only" clause — it already cites
   `gm-wisp-2yhv7u` directly and paraphrases the same 4 conditions near
   verbatim. Bead text and fleet memory agree here; the only actual
   disagreement was against this session's own role-prompt framing, now
   resolved in favor of the more current, independently-verified,
   message-ID-backed ruling.

   The 4 conditions the ruling attaches (checked immediately before merging,
   not assumed to carry over from the gate above):
   1. Gate 7/7 PASS — this document.
   2. PR state confirmed via a **direct**, fresh `gh pr view --json` read
      immediately before merging (not a poll script's classification, not
      reused pre-compaction data).
   3. CLEAN/MERGEABLE with both required checks (`build-test`, `lint`)
      COMPLETED/SUCCESS.
   4. No `--auto` arming — merge deliberately with plain
      `gh pr merge <n> --squash`, then independently verify
      `state=MERGED`/`mergedAt` non-null afterward (FR-03: never trust exit
      code alone).

   Anything short of all four on the fresh pre-merge read routes to mayor
   instead, same as always.

2. **Branch target:** the review was performed against a specific reviewed
   SHA, not a shared builder branch tip. `deploy/crn-ybgbs-gate` was cut
   directly from `29bfead...^{commit}` via `resolve_deploy_branch_target`,
   guarded first by `assert_deploy_ancestry_scope` (criterion 7 above).

3. **SHA integrity:** `DEPLOY_SHA` was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran, per the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.
