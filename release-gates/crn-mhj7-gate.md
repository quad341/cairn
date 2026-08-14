# Release Gate: Global/role: test coverage for shared-tier remember commit-before-mail invariant (FR-4/5/6)

- Bead: crn-mhj7 (deploy) / crn-k9b7 (review, PASS) / builder/crn-q5kk.1 (build bead, closed — provenance, not a push target)
- Reviewer-cited commit (R): `46c5f2e8f0a3bb54d854a6f97c291ff013f534d0`
- Original deploy commit (D₀): `46c5f2e8f0a3bb54d854a6f97c291ff013f534d0` (identical to R)
- Final deployed commit (D): `e0e0f4c6542ff320246dbb16cb11b2ddbe1ec258` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (reflog shows a single `rebase (finish)` entry directly after branch creation, no intermediate conflict-resolution steps), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-14, against origin/main@bfca968 (#88, "Extend Report diagnostics with Stats, add obslog Tail + Args logging")

## 6. Clean divergence from main (evaluated first)

Raw check at gate start FAILed: D₀ was cut from `18e03e3` (#87); origin/main had since advanced to `bfca968` (#88) — neither an ancestor of the other from D₀'s perspective. Resolved via `attempt_bounded_self_rebase` (`scripts/rebase-resolve-lib.sh`) on the isolated deploy branch (never a contributor fork): rc=0, `BEFORE_SHA=46c5f2e8f0a3bb54d854a6f97c291ff013f534d0` → `AFTER_SHA=e0e0f4c6542ff320246dbb16cb11b2ddbe1ec258`. Reflog confirms a single-shot clean rebase (`branch: Created from 46c5f2e8...^{commit}` immediately followed by `rebase (finish): ...onto bfca968...`, no `rebase (pick)`/`rebase (continue)` entries — the trivial-conflict-resolution loop was never entered). Force-with-lease pushed to `origin/deploy/crn-mhj7-gate`; independently re-verified via `git ls-remote origin refs/heads/deploy/crn-mhj7-gate` matching local HEAD exactly (`e0e0f4c...`). Before/after SHAs logged to crn-mhj7's bd notes for audit. Freshly re-verified at evaluation time: `git fetch origin main` → origin/main tip still `bfca968`; `git merge-base --is-ancestor origin/main HEAD` → yes. **PASS**; remaining criteria evaluated against D = `e0e0f4c`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `46c5f2e8f0a3bb54d854a6f97c291ff013f534d0`, recorded as both `deploy_commit` on crn-k9b7 (review, verdict `pass`) and crn-mhj7's own `**Commit:**` field — D₀ == R literally. The isolated deploy branch (`deploy/crn-mhj7-gate`) was cut from R via `resolve_deploy_branch_target(crn-mhj7, R)` — mechanically derived from the bead-id being operated on. Criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (clean, zero-conflict rebase, confirmed above; diff content byte-identical, only base commit changed). D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

crn-k9b7's `uncovered_criteria: none` cross-checks all of crn-q5kk.1's exit_contract (10 named test additions + 1 table extension) 1:1 against 11 resolved tests. Independently re-confirmed by exact name against D (post-rebase, my own `go test ./... -race -count=1 -v` run, not merely cited from review):

1. `TestRememberWritesUnderEachScopeTier/global` — PASS (dedicated global-tier case, no scope-tag subdirectory, per exit_contract's explicit "not a naive 4th row" instruction)
2. `TestRememberNonPrivateTierDoesNotCommitGlobalTier` — PASS
3. `TestRememberNonPrivateTierDoesNotCommitRoleTier` — PASS
4. `TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorGlobalTier` — PASS
5. `TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorRoleTier` — PASS
6. `TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentGlobalTier` — PASS
7. `TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentRoleTier` — PASS
8. `TestRememberCLIRoundTripAllFieldsGlobalTier` — PASS
9. `TestRememberCLIRoundTripAllFieldsRoleTier` — PASS
10. `TestRememberJSONSharedTierOutputsReviewBranchAndReviewerGlobalTier` — PASS
11. `TestRememberJSONSharedTierOutputsReviewBranchAndReviewerRoleTier` — PASS

Both hard constraints honored: skip-rationale comment above `TestRememberWritesUnderEachScopeTier` untouched (diff is additive only — 252 insertions/0 deletions); zero production code changes (`cmd/reviewer.go`, `cmd/remember.go`, `internal/cairn/remember.go` all unchanged, confirmed by diff stat: `cmd/remember_test.go` only). **PASS.**

## 3. Tests

Canonical command — matches `.github/workflows/ci.yml`'s build-test job verbatim and crn-k9b7's own `test_cmd`: `go build ./...` then `go test ./... -race -count=1`. Run independently on D (post-rebase, not merely cited from review):

- `go build ./...` — exit 0, clean.
- `go test ./... -race -count=1 -v` — all 7 packages `ok` (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`), 0 FAIL, 0 SKIP across the full suite (grepped the complete verbose log for `--- FAIL`/`--- SKIP`/bare `FAIL`: none found).
- All 11 diff-owned tests re-checked individually by exact name against D (list above under criterion 2) — all PASS.

No diff-owned SKIP or FAIL, no flakes observed. **PASS.**

## 4. No open blocking findings

crn-k9b7 recorded `style_findings: none` (`golangci-lint run ./...` → 0 issues; `gofmt -l` → clean; `go vet ./...` → exit 0) and `security_findings: none` (full 9-point OWASP-lens walk; diff is test-only, 0 production lines changed, no new attack surface). No HIGH, MAJOR, or MINOR findings of any kind recorded against this diff; no separate finding-type bead exists referencing crn-mhj7 or crn-k9b7. **PASS.**

## 5. Clean working tree

`git status --porcelain` returns empty on `deploy/crn-mhj7-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Diff is `cmd/remember_test.go` only, 252 insertions/0 deletions, one commit (`e0e0f4c`, carrying the same content as the original `46c5f2e8` RED/GREEN commit — per crn-q5kk.1's documented `tdd_red_deviation`/`tdd_green_deviation`, the production code under test was already correct, so no separate implementation commit exists). Entirely test-only coverage extending one feature: global/role:`<x>`-tier equivalents of 5 existing `rig:web`-only tests (FR-4/FR-5/FR-6), plus the matching table-driven extension. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
