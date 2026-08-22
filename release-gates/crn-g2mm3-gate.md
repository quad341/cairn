# Release Gate: crn-g2mm3

**Bead:** crn-g2mm3 (deploy) — source: crn-gx2v0 (review), origin: ga-1jk7la (gascity store, cross-rig handback)
**Commit:** `bdab0daab443f56e81db0eb294063add6383fd62`, cut onto `deploy/crn-g2mm3-gate`
**Date:** 2026-08-21

## Background

`internal/cairn/review.go`'s `ListReviewMergeBranches` had three early
`return nil, err` points in its per-branch loop (`isMergedInto`,
`changedEntryFile`, `tierFromEntryPath`). Any single malformed `remember/*`
branch (real trigger: `remember/bd-ready-json-pagination-f249525c`) aborted
the entire listing for every tier/identity/reviewer — `cairn review list`
went down fleet-wide on one bad branch.

The fix mirrors the existing sibling pattern in `branches.go`'s
`ListReviewBranches`/`ReviewBranch.Error` (report-don't-abort): adds
`ReviewMergeBranch.Error`; the three failure points append
`{Name, Error}` and `continue` instead of returning early.
`cmd/review.go` prints malformed entries as `"<name>\tmalformed: <reason>"`
(unconditionally, before the `--tier` filter) then continues the normal
listing. `DefaultBranch`/for-each-ref failures (genuinely
whole-listing-impossible) are untouched and still return a real error.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-gx2v0 (closed, reviewer verdict PASS) notes cite `bdab0daab443f56e81db0eb294063add6383fd62` as the GREEN/fix commit; crn-g2mm3's own `**Commit:**` field and `gc.deploy_commit` metadata cite the identical SHA (D = R, byte-for-byte). Both independently re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` (not trusted as transcribed mail/bead text) — real commit object, matches itself exactly, so not a short-prefix collision. |
| 2 | Acceptance criteria met | PASS | Read the actual diff directly (`git diff origin/main bdab0da -- internal/cairn/review.go cmd/review.go`), not just reviewer prose: confirmed all three early-return sites (`isMergedInto`, `changedEntryFile`, `tierFromEntryPath` error paths) replaced with `branches = append(branches, ReviewMergeBranch{Name: name, Error: ...}); continue`; new `Error` field added with doc comment; `DefaultBranch`/for-each-ref error returns untouched (correct scope — those really are whole-listing-impossible); `cmd/review.go` prints malformed branches unconditionally before the `--tier` filter check. Matches the claimed mirror of `branches.go:41`'s `ReviewBranch.Error` pattern. |
| 3 | Tests pass | PASS | Independently re-run in a fresh `mktemp` scratch worktree (detached at `bdab0da`, removed after) — not reused from the reviewer's report. `go build ./...` clean; `go vet ./...` clean; `gofmt -l .` empty; `golangci-lint run ./...` 0 issues; `go test -race ./...` — all 7 packages `ok` (root, cmd, formulas, internal/cairn, internal/critic, internal/obslog, scripts), zero FAIL, zero SKIP. Diff-owned tests resolved by exact name (`-run` + `-v`), all PASS individually: `TestReviewListReportsMalformedBranchAndStillListsValidOnes` (cmd), `TestListReviewBranchesReportsPrivateTierBranchAsError` + `TestListReviewMergeBranchesToleratesMalformedBranch` (internal/cairn). No go.mod/go.sum changes — no dependency surface. |
| 4 | No open HIGH findings | PASS | crn-gx2v0: "No blockers found. PASSing to deploy." One non-blocking nit noted (a `--tier`-while-malformed interaction not separately tested) — explicitly called out as trivial/non-blocking, not a HIGH finding. |
| 5 | Clean tree | PASS | `deploy/crn-g2mm3-gate` cut directly from `bdab0daab443f56e81db0eb294063add6383fd62^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty before the gate-file commit. |
| 6 | Clean divergence from main | PASS | `git merge-base --is-ancestor origin/main bdab0daab443f56e81db0eb294063add6383fd62` → true: `origin/main` (`afc97ffe5c16aaccd450b3c790e48966207387b1`) is the exact, direct parent-chain ancestor of the deploy commit (2 commits ahead: RED `ebbcdde3`, GREEN `bdab0da`). This is also the exact base the reviewer recorded. Trivial fast-forward — **no self-rebase needed at all**. |
| 7 | Single feature theme | PASS | Single bead, no `rollup-ship` label. All 4 changed files (`cmd/review.go`, `cmd/review_test.go`, `internal/cairn/review.go`, `internal/cairn/review_test.go`) sit under one subsystem: review-branch listing. Removing this commit pair from main would cleanly revert to the old abort-on-malformed behavior with nothing else affected. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority.** Per this session's role instructions: arm
   `gh pr merge --auto` on the PR opened below (no strategy flag), then
   verify by re-reading PR state. A declined arm (expected once the PR is
   already MERGEABLE/CLEAN with nothing left pending to defer) routes to
   mayor as a `MERGE-REQUEST` — the deployer never self-merges on
   quad341/cairn. This matches both the current role prompt and the standing
   memory `cairn-auto-merge-requires-explicit-strategy`, whose top-of-file
   banner records self-merge as PAUSED (2026-08-20) pending an operator
   ruling, with the operator-authored `deployer.md.tmpl` guardrail
   controlling until then. The most recent prior deploy (`crn-tk8qd`, PR
   #134) already reached this same conclusion and flagged the underlying
   mayor-ruling/operator-guardrail contradiction separately rather than
   acting on the old "standing authorization" — not re-litigated again here.

2. **Branch target.** `builder/ga-1jk7la` (bead prose) and
   `gc.deploy_branch` metadata are provenance-only, local-only (not on
   origin) per the deploy bead's own instruction. `deploy/crn-g2mm3-gate`
   was cut fresh from the exact reviewed SHA via `resolve_deploy_branch_target`;
   `assert_safe_push_target` confirmed the derived name does not match the
   shared-worktree-branch signature (`gc-<agent>-<hex>`).

3. **SHA integrity.** The deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` on both the D
   (deploy-bead metadata) and R (review-bead notes) sides before comparison,
   before any gate step ran — never trusted as an eyeballed or transcribed
   string from the routing mail.

4. **Ancestry scope.** No `.claude/**` paths in the diff; all commits in the
   deploy range cite bead ids belonging to this deploy chain
   (crn-gx2v0 / ga-1jk7la).
