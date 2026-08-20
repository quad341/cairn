# Release Gate: crn-of9fp

**Bead:** crn-of9fp (deploy) — source: crn-h9vgx (review), build: crn-12ubp
**Commit:** `53e7211cf77e3afed0c7dd2fa1ab1cf2896636c9`, cut onto `deploy/crn-of9fp-gate`
**Date:** 2026-08-20

## Background

`cairn review merge` merges a review branch but does not delete it, and
`ListReviewMergeBranches` (backing `cairn review list`) keyed its "pending"
listing purely on branch existence — with no merge-status check. A branch
merged by any path other than `MergeReviewBranch`, or left behind by a
delete failure after a successful merge, would nag forever as "pending
review" and the backlog count would never fall to reflect the true state.

The fix adds an `isMergedInto` check to `ListReviewMergeBranches`, mirroring
the equivalent check its sibling `ListReviewBranches` (branches.go) already
had — once a branch is merged into the default branch it is skipped,
regardless of whether it was also deleted. A new regression test,
`TestListReviewMergeBranchesExcludesAlreadyMergedBranch`, reproduces the bug
by merging a fixture branch directly with `git merge --no-ff` (bypassing
`MergeReviewBranch`) and confirms the merged branch no longer appears in the
pending list while a genuinely-unmerged sibling branch still does.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-h9vgx's verdict cites `53e7211cf77e3afed0c7dd2fa1ab1cf2896636c9`, identical to the deploy bead's `metadata.gc.deploy_commit` — exact SHA match (D = R). Independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as a transcribed string. |
| 2 | Acceptance criteria met | PASS | Verified directly against `git diff 8002079...53e7211` (not just the reviewer's write-up): `isMergedInto` check added to `ListReviewMergeBranches` in `internal/cairn/review.go`, matching the existing pattern in `ListReviewBranches`. Matches crn-12ubp's 5-item exit_contract and the bead's description exactly. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on a detached checkout of `53e7211...` (own build, not the reviewer's self-report): `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, diff-owned test `TestListReviewMergeBranchesExcludesAlreadyMergedBranch` run by name — PASS, and the full `internal/cairn` package suite — PASS (`ok`, 62.3s). |
| 4 | No open HIGH findings | PASS | `bd list --status open` / `--status in_progress` checked fresh for anything referencing this chain (crn-of9fp, crn-h9vgx, crn-12ubp): only the review bead itself (in-progress review record, not a finding) and two sling/routing bookkeeping beads (crn-sdi8h, crn-xev9b — no findings content). No open HIGH finding anywhere in the chain. |
| 5 | Clean tree | PASS | `deploy/crn-of9fp-gate` cut directly from `53e7211...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty throughout. Diff is exactly 2 files / 49 insertions / 0 deletions — one production change, one test, no stray content. |
| 6 | Clean divergence from main | PASS | `git rev-list --left-right --count origin/main...53e7211` → `4  2`: 4 commits behind (pure staleness), 2 ahead (the red/green TDD pair). Re-verified immediately before cutting the branch that no commit touching `review.go`/`review_test.go` landed on `origin/main` between the fork point and current tip (93c2593) — zero file overlap, no rebase needed, no conflict possible. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 53e7211... crn-of9fp crn-12ubp crn-h9vgx` → rc=0: no `.claude/**` paths touched, and both non-merge commits in `origin/main..53e7211` cite `crn-12ubp` in their message. No stray commits. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority — proceeding under the standing self-merge authorization,
   not routing to mayor.** The deploy bead's own text carries the familiar
   "route a merge-request to mayor/mpr — do NOT merge yourself" clause. Per
   fleet memory `cairn-auto-merge-requires-explicit-strategy`, this is a
   **known, understood stale-copy issue** for this repo, not a fresh signal:
   mayor's explicit, mail-verified ruling (`gm-wisp-2yhv7u`, 2026-08-19)
   already addressed this exact recurring boilerplate — cairn is not covered
   by mpr, so the clause is simply carried over from a template written for
   repos that are — and instructed future sessions *not* to re-pause or
   re-escalate merely because a bead repeats it. This gate proceeds under the
   STANDING AUTHORIZATION (7/7 PASS + CI green on `quad341/cairn`) with the
   four attached conditions: (1) gate 7/7 PASS — this doc; (2) PR state
   confirmed via a direct, fresh `gh pr view --json` read at merge time; (3)
   CLEAN/MERGEABLE with both required checks (`build-test`, `lint`)
   COMPLETED/SUCCESS; (4) no `--auto` — plain `gh pr merge <n> --squash`
   only, then verify `state=MERGED` and `mergedAt` non-null before reporting
   success.

2. **Branch target:** `builder/crn-12ubp` is provenance-only per the deploy
   bead's own instruction — a possibly shared builder branch, not a push
   target. `deploy/crn-of9fp-gate` was cut fresh from the exact reviewed SHA
   via `resolve_deploy_branch_target`, and `assert_safe_push_target` confirmed
   the derived name does not match the shared-worktree-branch signature.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran, per the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.
