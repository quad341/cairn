# Release Gate: crn-9qlxu

**Bead:** crn-9qlxu (deploy) — source review crn-rott9.1 (epic: crn-rott9)
**Commit:** `b1320d785ae8b5d28527f6ffd2356f7f944291ce`, cut onto `deploy/crn-9qlxu-gate` off `origin/main`
**Date:** 2026-08-20

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Merge-base with `origin/main` (tip `6b45121772bbd54215945aaadf948d9f72da48f3`) is `aa484f2bf631907b0112fa886c38ae6927526b9b`. File-level overlap found on `cmd/remember_test.go` between this branch's 7 changed files and main's intervening changes — escalated beyond a bare `comm` check: `git merge-tree --write-tree origin/main b1320d7` produced a clean tree (`69570e03417f757e839838d565b8c4a72d5528bf`, exit 0), and manual hunk-range inspection confirmed the overlapping edits sit in disjoint line ranges (this branch: ~234-269 and a new test inserted after line 1169; main: line 1256 onward) — no real conflict. `assert_deploy_ancestry_scope origin/main <sha> crn-9qlxu crn-rott9.1`: rc=0. |
| 1 | Review PASS present | PASS | crn-rott9.1 closed, REVIEW VERDICT: PASS from cairn/reviewer at this exact commit — build/vet clean, `go test ./...` all green (7 packages), `go test -race ./cmd/...` clean, `golangci-lint run ./...` 0 issues (isolated cache). |
| 2 | Acceptance criteria met | PASS | Bug required (a) `resolveReviewer` reordered to run BEFORE the review-branch commit/eviction/promotion-mark commit at all 4 call sites (`requestReview`, `commitForBatchReview`, `requestCullReview`, `requestPromotionReview`), so a resolution failure leaves nothing committed; (b) `defaultReviewer`'s rig-tier case switched from `$GC_RIG` to the entry's own declared rig value (`ResolvedTier`'s `value`). Diff confirms both, at all 4 sites, symmetrically. role tier deliberately left `$GC_RIG`-based per the bead's own text — diff confirms that switch case is untouched. Security claim in the bead ("rig value already trusted for `scopeDir`'s path join, so reusing it for a mail-recipient string is not a new/larger trust extension") independently verified: `scopeDir` (`internal/cairn/remember.go:222`) calls the same `ResolvedTier` and joins its `value` straight into a `filepath.Join`, pre-dating this change — claim holds. |
| 3 | Tests pass | PASS | Independently re-verified in a fresh isolated scratch worktree at `b1320d7` (not just trusting the reviewer's claim): `go build ./...`, `go vet ./...`, `gofmt -l .` all clean; `golangci-lint run ./...` (isolated `GOLANGCI_LINT_CACHE`) 0 issues; `go test ./... -race -count=1` — all 7 packages `ok`, 0 FAIL. All 5 named new/changed regression tests re-run directly with `-v`, individually PASS: `TestDefaultReviewerRigTierIgnoresGCRig`, `TestDefaultReviewerRigTierRequiresNonEmptyValue`, `TestRememberBatchReviewerResolutionFailureCommitsNothing`, `TestCullEvictReviewerResolutionFailureCommitsNothing`, `TestPromoteMarkReviewerResolutionFailureCommitsNothing`. |
| 4 | No high-severity review findings open | PASS | No `finding`-labeled beads reference crn-rott9.1 or crn-9qlxu. Related beads checked individually: crn-m98hp (molecule container, not a finding), crn-ry4zu (architect-level design-decision root bead for the broader "recall serves unreviewed entries" problem — crn-rott9, parent of crn-rott9.1, is itself the delegated implementation that discharges it; held open only pending sibling children and an unrelated `gc.work_outcome` guard defect `gm-84dngi`, not because of any defect in this fix), crn-ppw5w (empty fleet-routing "convoy" artifact, not a code finding), crn-27con.7 (unrelated prior-cycle follow-up; incidental text match, not topically connected). |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `deploy/crn-9qlxu-gate` immediately after cut. |
| 7 | Single feature theme | PASS | Diff vs. merge-base touches 7 files, all under `cmd/`: `reviewer.go` (the fix — 4 call-site reorders + rig-tier default fix), `reviewer_test.go`, `remember_batch_test.go`, `remember_test.go`, `cull_test.go`, `promote_test.go` (regression tests per call site), and `branches_test.go` (a necessary downstream fixture fix — an existing test that forced rig-tier resolution to fail via `stubGCWithRig("")` no longer can, since rig tier no longer reads `$GC_RIG`, so its fixture switched from `rig:web` to `role:reviewer` to keep testing the same failure path). One coherent theme, no unrelated changes. |

## Verdict: PASS (7/7) — proceeding to PR.

## Merge authority

quad341/cairn — standing self-merge authorization applies (mayor ruling `gm-wisp-2yhv7u`, 2026-08-19), governed by memory `cairn-auto-merge-requires-explicit-strategy`. This bead's own description already states the 4 standing conditions explicitly and carries no stale "route to mayor/mpr, no self-merge" boilerplate. Deployer will merge directly (`gh pr merge --squash`, plain, once CI is green) under the ruling's 4 standing conditions.

## Note

Source branch `builder/crn-rott9.1` is provenance-only per the bead's own instructions — deploy branch cut directly from commit `b1320d7`, not pushed onto or opened from the builder branch.
