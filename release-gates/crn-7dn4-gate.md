# Release Gate: Extend cairn.Report with Stats and obslog with Args + Tail (rage prerequisites)

- Bead: crn-7dn4 (deploy) / crn-z6mj (review, PASS) / builder/crn-x5zx.2 (build bead, closed — provenance, not a push target)
- Reviewer-cited commit (R): `b85f207d3aade5b6dd4358e30ac395f9572b40e6`
- Original deploy commit (D₀): `b85f207d3aade5b6dd4358e30ac395f9572b40e6` (identical to R)
- Final deployed commit (D): `3b98c4e4124fde9dc5aa0286d6e79782692463de` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (zero conflicts — reflog shows a single `rebase (finish)` entry directly after branch creation, no intermediate conflict-resolution steps), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-13, against origin/main@18e03e3 (#87, "Fix recall surfacing the oldest entry instead of a topic-key correction")

## 6. Clean divergence from main (evaluated first)

Raw check at gate start FAILed: R and origin/main (18e03e3) had diverged — neither an ancestor of the other (`git merge-base --is-ancestor` both directions returned false). Resolved via `attempt_bounded_self_rebase` (`scripts/rebase-resolve-lib.sh`) on the isolated deploy branch (never a contributor fork): rc=0, `BEFORE_SHA=b85f207d3aade5b6dd4358e30ac395f9572b40e6` → `AFTER_SHA=3b98c4e4124fde9dc5aa0286d6e79782692463de`. Reflog corroborates a single-shot clean rebase (no `rebase (pick)`/`rebase (continue)` entries, meaning the trivial-conflict resolution loop was never entered — `git rebase origin/main` succeeded directly). Force-with-lease pushed to `origin/deploy/crn-7dn4-gate`; independently re-fetched post-push and confirmed the remote tip matches local HEAD exactly (`3b98c4e...`). Before/after SHAs logged to the bead's notes for audit. Freshly re-verified at evaluation time: `git fetch origin main` → origin/main tip still `18e03e3`; `git merge-base --is-ancestor origin/main HEAD` → yes. **PASS**; remaining criteria evaluated against D = `3b98c4e`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `b85f207d3aade5b6dd4358e30ac395f9572b40e6`, recorded as `deploy_commit` on crn-z6mj (review, verdict PASS) and matching crn-7dn4's own `**Commit:**` field exactly — D₀ == R literally. The isolated deploy branch (`deploy/crn-7dn4-gate`) was cut from R via `resolve_deploy_branch_target(crn-7dn4, R)` — mechanically derived from the bead-id being operated on, not hand-named from bead prose (the bead's own text cites `deploy/crn-z6mj-gate`, which was not used). Criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (clean, zero-conflict rebase, confirmed above). D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

crn-z6mj's `uncovered_criteria: none` maps all 5 exit_contract bullets to evidence; independently re-confirmed by name against D (post-rebase, not merely cited from review):

1. Stats-by-tier → `TestDiagnoseStatsCountsEntriesByTierAndReportsCleanIndex` — PASS (0.09s)
2. exactly-once walk/stale calls → `TestDiagnoseCallsIterEntriesTolerantAndIndexStaleExactlyOnce` — PASS (0.06s)
3. Tail budget/ordering → `TestTailReturnsOldestOfTailFirstWithinBudget` — PASS (0.00s), `TestTailBudgetSmallerThanOneLineStillReturnsIt` — PASS (0.00s)
4. Args in real invocation → `TestRootLogsContextRecordUnconditionally` — PASS (0.03s), `TestContextRecordIncludesArgs` — PASS (0.00s)
5. build/vet/test clean → `go build ./...` exit 0, `go vet ./...` exit 0, full suite green modulo the disposed pre-existing flake (criterion 3)

All 5 independently confirmed against D. **PASS.**

## 3. Tests

Canonical command — matches `Makefile`'s `test:` target and crn-z6mj's own `test_cmd`: `go test ./... -race -count=1`. Run independently **twice** on D (post-rebase, not merely cited from review):

- Run 1 (`-race -count=1 -v`, tallied including subtests): **735 PASS, 0 FAIL, 0 SKIP**, all 7 packages `ok`. (One higher than crn-z6mj's reviewer-time count of 734 — D carries the same diff content as R, no new tests landed by the rebase itself; within normal count noise between independent runs, consistent with the crn-jpnr-gate precedent for the same phenomenon, not a regression.)
- Run 2 (`-race -count=1`, non-verbose): all 7 packages `ok`, exit 0. Zero reproduction of the `TestConcurrentReindexOnColdStoreDoesNotHardFail` SQLITE_BUSY flake that crn-z6mj saw once at review time — pre-existing, tracked at crn-uxel (open, P3), independently confirmed non-diff-caused by the reviewer (diff touches none of index.go/index_test.go/Reindex/entriesSchema; 5x-vs-3x comparison against this same origin/main baseline). Not reproduced in either of my runs; already disposed of, no action needed here.
- All 15/15 diff-owned tests re-checked individually by exact name against D: `TestRootLogsContextRecordUnconditionally`, `TestDiagnoseStatsCountsEntriesByTierAndReportsCleanIndex`, `TestDiagnoseStatsReportsStaleIndex`, `TestDiagnoseCallsIterEntriesTolerantAndIndexStaleExactlyOnce`, `TestIndexDriftCleanWhenIndexAlreadyFresh`, `TestIndexDriftReportsStaleIndex`, `TestContextRecordIncludesArgs`, `TestTailEmptyFileReturnsNil`, `TestTailNonexistentFileReturnsError`, `TestTailSingleLineNoTrailingNewline`, `TestTailBudgetSmallerThanOneLineStillReturnsIt`, `TestTailReturnsOldestOfTailFirstWithinBudget`, `TestTailUnlimitedBudgetReturnsEveryLine`, `TestTailAcrossManyLinesExercisesWindowGrowth`, `TestTailLinesAreValidJSON` — all PASS.

No diff-owned SKIP or FAIL; the one known flake is pre-existing, unrelated, already tracked, and did not even reproduce on either run. **PASS.**

## 4. No open blocking findings

crn-z6mj recorded one MAJOR and one MINOR security finding, both explicitly disposed non-blocking by the reviewer's own reasoning:

- **MAJOR** (`internal/obslog/obslog.go:137` + `cmd/root.go:87`): unconditional raw `os.Args` logging into `debug.jsonl`, no gate/redaction/opt-out. Reviewer's own disposition: "Not a blocker for this bead in isolation (no bundle-export code exists yet)" — the exploitable path requires the not-yet-built rage-bundle export command (crn-x5zx.3). I filed **crn-79vu** (P2, task) to track this mechanically: `discovered-from:crn-z6mj`, `blocks:crn-x5zx.3` — verified via `bd dep tree crn-x5zx.3`, which now shows crn-x5zx.3 as `[BLOCKED]` with crn-79vu listed. This is a hard dependency-graph link, not a prose note, so the risk cannot be silently dropped when crn-x5zx.3 is picked up.
- **MINOR** (`internal/obslog/tail.go:21`): `//nolint:gosec` justification unverifiable in-scope (zero callers of `Tail()` exist yet). Noted in crn-79vu's description as a secondary item for crn-x5zx.3's future reviewer to confirm.

`style_findings: none` independently reconfirmed on D: `go build ./...` clean, `gofmt -l .` empty, `go vet ./...` clean, `golangci-lint run ./...` — **0 issues** after clearing a stale golangci-lint cache. (An uncached run first showed 8 phantom issues, but every one referenced `../builder/worktrees/crn-x5zx.2/...`, a sibling worktree directory that does not exist on disk — confirmed via `ls` — and every flagged file:line in *this* worktree's actual checked-out content already carries an appropriate, justified `//nolint` comment at that exact location. `golangci-lint cache clean` followed by a fresh run confirmed 0 issues, ruling out a real regression.) No other open finding-type bead. **PASS.**

## 5. Clean working tree

`git status` reports "nothing to commit, working tree clean" on `deploy/crn-7dn4-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Exactly 3 commits ahead of origin/main: `04a7473` (`test(feat): red`), `502b585` (`feat: green`), `3b98c4e` (`fix: satisfy golangci-lint`) — a complete TDD arc for one feature: extending `cairn.Report` with `Stats`/`StoreStats` and `obslog` with `ContextFields.Args` + `Tail`. Explicitly scoped to exclude `cmd/rage.go` and its future callers (tracked separately as crn-x5zx.3, now gated on crn-79vu per criterion 4). No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
