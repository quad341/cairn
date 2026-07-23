# Release Gate: cairn-review-cli-verb

**Bead:** crn-ucxp (deploy) — supersedes crn-j1uh — source review crn-8emp — implementation crn-ffm
**Commit:** `d8c2436d8ad7c35f7168a5c37a866be572e7898e`, tip of `feat/crn-ffm-review-cli`, evaluated in an isolated detached-HEAD worktree
**Date:** 2026-07-23

## Context

Re-evaluation after crn-j1uh's PR #36 (headRefOid `596de13`) broke CI post-gate:
an out-of-band merge landed PR #24/crn-4dxi's `splitFrontmatter`/`tomlQuote` on
`entry.go`, colliding with `review.go`'s independently-defined same-named
helpers. Builder reconciled (renamed `review.go`'s to
`splitFrontmatterForPatch`, consolidated `tomlQuote` onto `entry.go`'s
strict-superset version) at commit `d8c2436`, which also absorbed a second,
later collision (`ReviewBranch`/`ListReviewBranches` vs. PR #39/crn-0yv.1's
`branches.go` — renamed to `ReviewMergeBranch`/`ListReviewMergeBranches`).
cairn/reviewer independently re-reviewed and PASSED `d8c2436` (session
reviewer-gm-gpw5m, full verdict in crn-j1uh notes). **PR #36 is stale
(pinned to pre-reconciliation `596de13`, CI red) and must not be reused** —
this gate cuts a fresh isolated branch from `d8c2436`.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git fetch origin main` (tip `8a914d4`, same tip the reviewer's own PASS was verified against — no drift since). `git merge-base --is-ancestor origin/main d8c2436` exit 0: `origin/main` is a clean ancestor of the reviewed commit. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-j1uh closed (reason: "PASS: re-reviewed commit d8c2436 ... after crn-j1uh bounced deployer->builder->reviewer for the CI-break reconciliation") with a full VERDICT: PASS from cairn/reviewer naming this exact commit — merge cleanliness, all 3 symbol-collision renames, the `ValidatePathSegment(AnchorType)` security guard, full gate — independently re-verified in the reviewer's own isolated worktree. |
| 2 | Acceptance criteria met | PASS | crn-8emp's 11-point checklist (list/show/merge behavior, `ValidatePathSegment` reuse now covering `topic_key`+`scope`+`anchor_type`, `--no-ff` commit shape, branch deletion, reindex, NFR-2 no-partial-state, secret-pattern guard) all pass per the reviewer's re-review PASS. Independently spot-checked in this pass: all 12 named acceptance-test functions (`TestListReviewBranchesDerivesTierFromEntryPath`, `TestShowReviewBranch`, `TestPatchFrontmatterFieldsOnlyTouchesRequestedFields`, `TestMergeReviewBranchRejectsInvalidTopicKey`, `TestMergeReviewBranchRejectsInvalidScopeTag`, `TestMergeReviewBranchRejectsInvalidAnchorType`, `TestMergeReviewBranchSucceeds`, `TestMergeReviewBranchReindexes`, `TestMergeReviewBranchConflictLeavesNoPartialState`, `TestDetectSecretPattern`, `TestMergeReviewBranchBlocksObviousSecretByDefault`, `TestMergeReviewBranchAllowSecretPatternOverridesGuard`) confirmed present via direct grep against `internal/cairn/review_test.go`, not taken on report alone. |
| 3 | Tests pass | PASS | Run directly in this session in a fresh isolated worktree at `d8c2436`: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./... -race -count=1` — all 4 packages (root, `cmd`, `internal/cairn`, `internal/critic`, `scripts`) ok, zero regressions. `golangci-lint run ./...` (cache cleaned first per known shared-cache staleness) — 0 issues. |
| 4 | No high-severity review findings open | PASS | The one HIGH finding from the original review (crn-8emp: unvalidated `AnchorType` allowing newline-injection TOML corruption) was fixed (`ValidatePathSegment` guard + `TestMergeReviewBranchRejectsInvalidAnchorType` regression test) and independently re-reviewed PASS. Only a cosmetic, explicitly non-blocking nit remains (renamed test functions kept pre-rename names) — not gating. |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on `d8c2436` in the isolated worktree — no modified/staged/untracked files. |
| 7 | Single feature theme | PASS | `git diff --stat origin/main` touches exactly 4 files, all pure additions, all within the review-CLI subsystem: `cmd/review.go` (+105), `cmd/review_test.go` (+215), `internal/cairn/review.go` (+500), `internal/cairn/review_test.go` (+562). Zero changes to `entry.go`, `branches.go`, or any file outside this feature — confirms the symbol-collision reconciliation was resolved entirely within `review.go`'s own renames, not by touching the colliding files. |

## Verdict: PASS

All 7 criteria pass. Cutting isolated deploy branch `deploy/crn-ucxp-gate` from
`d8c2436` (never reusing/rebasing stale PR #36) and opening a fresh PR per
deploy SOP.
