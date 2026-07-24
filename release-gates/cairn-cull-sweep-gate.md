# Release Gate: cairn-cull-sweep

**Bead:** crn-whno (deploy) — source review crn-28ge.1.7 (closed, PASS)
**Commit:** `5a99edc8a31a48b4c5cba1a6392a182b94e01271`, cut onto `deploy/crn-whno-gate` off `origin/main` (`9bfa28aca065bf064be6fba915c9e19354c774ea`) via cherry-pick of `dbcc32c b9a97da 0ae7941` (reviewed stack on `origin/gc-builder-769138d1bf3c`), per the bead's MERGE_POLICY note
**Date:** 2026-07-24

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | The reviewed branch (`origin/gc-builder-769138d1bf3c`) is not itself mergeable — its merge-base with `origin/main` is `b4619c7`, and it carries stale duplicate copies of crn-28ge.1.4's commits already landed on `main` under different SHAs (`eea622c`/`f0c6a46`/`eb070de` vs. the branch's `24e19d2`/`678775a`/`0d7c818`). Per the bead's MERGE_POLICY, cut `deploy/crn-whno-gate` fresh off `origin/main` (`9bfa28a`, up to date at fetch time) and cherry-picked exactly `dbcc32c b9a97da 0ae7941` onto it — zero conflicts, matching the reviewer's own dry-run. `git range-diff b4619c7..0ae7941 origin/main..HEAD` confirms all three cherry-picked commits are content-equivalent (`=`) to the reviewed originals, and that none of the stale duplicate commits (entries 1-12 in the range-diff) rode along. |
| 1 | Review PASS present, SHA-matched | PASS | crn-whno's description states "Reviewed and PASSED: crn-28ge.1.7" with full evidence (build/vet/gofmt/lint clean, tests green, dedicated OWASP pass, NFR-07 invariant verified). Reviewed commit `R` = `0ae7941`. Deployed commit `D` = `5a99edc` is not a literal git ancestor of `R` (different parent chain, per the cherry-pick above) but is **content-identical** to `R`'s three-commit stack per the range-diff `=` markers — i.e. exactly the reviewed diff, re-parented onto a corrected base, not a superseded or newer, unreviewed commit. Source review bead crn-28ge.1.7 is closed. |
| 2 | Acceptance criteria met | PASS | FR-10 (cull-candidates independent of freshness/Check()): `TestCullCandidatesIndependentOfFreshness`, `TestCullCandidatesNeverRecalledFallsBackToCreatedAt`, `TestCullCandidatesThresholdConfigurable`, `TestCullCandidatesIncludesScopeForDownstreamTierDecision`, `TestCullCandidatesNotYetDisusedIsExcluded` all pass. NFR-07 (private `agent:` scope = direct delete; shared `role:`/`rig:`/`global:` = review-branch proposal only, never direct): `TestEvictDirectDeletesOnlyTheEntryFile`, `TestEvictDirectRefusesSharedTierEntry` (table-driven over `rig_scope`/`role_scope`/`global_(empty)_scope`, all pass), `TestEvictToReviewBranchDeletesOnlyTheEntryFileLeavingDefaultUntouched`, `TestEvictToReviewBranchRefusesWhenProposalAlreadyPending` all pass. CLI wiring: `TestCullEvictPrivateTierDeletesDirectlyAndReportsSHA`, `TestCullEvictSharedTierProposesReviewBranchAndDoesNotDeleteDirectly`, `TestCullEvictUnknownIDReturnsClearError` all pass. Re-ran these targeted (`-v`) myself on the cherry-picked branch rather than trusting the reviewer's report secondhand — all pass, matching the review notes' AC-to-test mapping. |
| 3 | Tests pass | PASS | On commit `5a99edc`, run in this worktree: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `golangci-lint run ./...` — cache cleaned first (`golangci-lint cache clean`), 0 issues. `go test ./... -race -count=1`: all 5 packages ok (`cairn`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `scripts`), zero regressions. |
| 4 | No high-severity review findings open | PASS | Review notes report a dedicated OWASP pass with no injection/traversal/access-control issues found, and no HIGH findings are recorded against crn-whno / crn-28ge.1.7. |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `deploy/crn-whno-gate` at `5a99edc` immediately before gate write. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` touches exactly the 7 files the reviewer's dry-run predicted: `cmd/cull.go`, `cmd/cull_test.go`, `cmd/reviewer.go`, `internal/cairn/cull.go`, `internal/cairn/cull_test.go`, `internal/cairn/evict.go`, `internal/cairn/evict_test.go` — one subsystem (CULL sweep: cull-candidates detection + tier-conditional eviction), one coherent feature. |

## Verdict: PASS — proceeding to PR.

## Note on downstream follow-on

crn-28ge.1.10 (assigned cairn/validator) is an already-filed, independent
follow-on that re-confirms FR-10/NFR-07 test coverage once this lands on
`origin/main`. It does not complete until this deploy merges; no action
required here beyond landing normally.
