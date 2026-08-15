# Release Gate: crn-dozeb

**Bead:** crn-dozeb (deploy) — source: crn-txkza (review), build: crn-f0rb7.1, architecture: crn-f0rb7, molecule: crn-o9j5c
**Commit:** `29f9a6e2d97ad6cf80ae9c2c568f34012c3b25ef`, cut onto `deploy/crn-dozeb-gate`
**Date:** 2026-08-15

## Background

Implements recommendation (1) of crn-f0rb7's two-part architecture decision:
bound `ensureFreshWith`'s self-heal reindex with a `context.WithTimeout`
(6s), scoped only to the "confirmed stale against a different HEAD" case
(`staleBehindHEAD`) — not the "no watermark row yet" cold-store case
(`staleNoWatermark`), which keeps its unbounded budget per FR-4 so a
first-ever index build is never penalized. `indexStale` now reports *why*
it's stale via a `staleReason` enum instead of a bare bool; `retryOnBusy`
is unchanged (it already honors `ctx.Done()` between attempts, so the real
ceiling lands around 6-11s rather than exactly 6s); `obslog.IndexDriftFields`
gains additive `BoundedBudget`/`BudgetExceeded` fields (NFR-5) so real-world
fast-fail frequency is measurable post-ship. Recommendation (2) (chunked
upsert batching) and (3) (deferred single-flight collapse) are tracked
separately and are not part of this diff.

Reviewed and PASSED by `cairn/reviewer` (crn-txkza, closed, verdict: pass)
against exactly this commit.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-txkza's notes cite `deploy_commit: 29f9a6e2d97ad6cf80ae9c2c568f34012c3b25ef`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R), not an ancestor relationship. |
| 2 | Acceptance criteria met | PASS | Build bead crn-f0rb7.1's `exit_contract` (read directly from source, not just the reviewer's restatement) lists 4 criteria: (a) stale-behind-HEAD unit test bounds to ~6-11s, (b) cold-store unit test confirms unbounded/runs-to-completion, (c) `go test ./internal/cairn/...` passes, (d) no new external dependency. Reviewer's `uncovered_criteria: none` maps all 4 to named tests/checks; independently re-verified by the deployer below (row 3) rather than trusted alone. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-dozeb-gate` at `29f9a6e2...`, using CI's exact documented command (confirmed identical in `.github/workflows/ci.yml` and the Makefile `test` target): `go test ./... -race -count=1` — **7/7 packages ok, 0 FAIL, 0 SKIP.** All 3 diff-owned tests individually confirmed PASS by name: `TestEnsureFreshBoundsSelfHealReindexWhenStaleBehindHEAD` (10.26s, within the ~6-11s bound), `TestEnsureFreshDoesNotBoundColdStoreSelfHealReindex` (12.19s, confirms the cold-store exemption runs to completion unbounded), `TestIndexStalePropagatesGitInvocationError` (0.07s). `git diff --stat origin/main..HEAD -- go.mod go.sum` empty — no new dependency, independently confirmed rather than trusted from the reviewer's notes alone. |
| 4 | No open HIGH findings | PASS | Reviewer's `style_findings`: gofmt/go vet/golangci-lint v2 all clean on the 3 changed files. `security_findings`: explicit 9-category OWASP walk, all none/improved — notably the pre-existing `redactSecrets` call on the reindex error path is preserved unchanged, new `BoundedBudget`/`BudgetExceeded` fields are plain bools with no sensitive content, and fail-closed semantics are confirmed (`DeadlineExceeded` propagates as an error, never serves stale data as fresh). crn-txkza's `dependent_count: 1` is only the deploy bead itself (crn-dozeb) — no separate open HIGH/blocker finding bead exists. |
| 5 | Clean tree | PASS | `deploy/crn-dozeb-gate` re-cut directly from `29f9a6e2...^{commit}` via `resolve_deploy_branch_target` (idempotent — the branch pre-existed pointing at the wrong commit, `origin/main` tip, left over from an interrupted prior run; the re-cut corrected it). `git status --porcelain` empty immediately after. |
| 6 | Clean divergence from main | PASS | Freshly re-checked post-recut: `git merge-base HEAD origin/main` = `36badd704f9134b47e38ea1ebb7108ef6383a32a` = `origin/main` exactly; `git merge-base --is-ancestor origin/main HEAD` confirms origin/main is a direct ancestor of the reviewed commit — a clean fast-forward with zero conflicting commits. No self-rebase needed. |
| 7 | Single feature theme | PASS | All changed files (`internal/cairn/index.go`, `internal/cairn/index_test.go`, `internal/obslog/obslog.go`; 202 insertions/20 deletions) implement one cohesive theme: the bounded self-heal reindex timeout, its `staleReason`-distinguishing test coverage, and the observability fields that support measuring it post-ship. No unrelated or drive-by changes. |

## Verdict: PASS — proceeding to PR.

## Process note

The deploy bead's own body text ("Route a merge-request to mayor/mpr; merge
authority is operator/mayor/mpr only — no rig agent runs `gh pr merge`.
Report the gate result back to mayor.") reflects process superseded, same
day, by the mayor-ruled standing authorization
(`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15) and
the current deployer role prompt: for `quad341/cairn` only, gate 7/7 PASS +
CI green (`build-test`, `lint`) ⇒ the deployer arms `gh pr merge --auto`
directly, with no mayor escalation required. This gate follows the newer,
more specific, same-day authorization rather than the bead's stale prose.
