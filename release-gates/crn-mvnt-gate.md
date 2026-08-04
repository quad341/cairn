# Release Gate: Execute one end-to-end git-anchor validation loop (derive→remember→reuse→stale→re-derive)

- Bead: crn-mvnt (deploy) / crn-3l2v (review, PASS) / crn-ie0m.1 (builder/implementation, provenance branch `builder/crn-ie0m.1`)
- Reviewer-cited commit (R): `898c46f440c9118698e2a5b79d910b0dd8fe455a`
- Final deployed commit (D): `898c46f440c9118698e2a5b79d910b0dd8fe455a` (identical — no rebase required, see criterion 6)
- Evaluated: 2026-08-04, against origin/main@37f8327 (PR #73, "docs: correct capability claims that four merged PRs made false")
- Downstream: crn-mvnt has 1 dependent bead (unidentified at evaluation time; not blocking this gate — no dependency runs the other direction)

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before this evaluation. D's parent is `6db5c0e` (#75) — 2 commits behind current origin/main tip (`37f8327`, #73 + #74). "Behind" is not "conflicting": ran a non-destructive dry-run merge (`git merge --no-commit --no-ff origin/main` on the freshly-cut `deploy/crn-mvnt-gate` branch) — merged clean, 4 files staged, zero conflicts — then `git merge --abort` to restore the branch to exactly D. No rebase needed; `attempt_bounded_self_rebase` not invoked. **PASS.**

## 1. Exact SHA match (D == R)

D and R are the literal same commit, `898c46f440c9118698e2a5b79d910b0dd8fe455a`. Cut via `resolve_deploy_branch_target crn-mvnt 898c46f440c9118698e2a5b79d910b0dd8fe455a`, confirmed via `git rev-parse HEAD` on the deploy branch. This matches both the deploy bead's own `commit` metadata field and the review bead's `deploy_commit` field. **PASS.**

## 2. Acceptance criteria

Acceptance criteria are the 7-step exit_contract on `crn-ie0m.1` (remember → reuse via get → stale-detect via freshness+status → re-derive via remember[--force if blocked] → verify → fresh again). Independently read the actual diff (`git show 898c46f -- cmd/validation_git_anchor_loop_test.go`, +192/-0, the only file this commit touches) rather than trusting the reviewer's summary alone: `TestValidationGitAnchorLoop` implements all 7 steps against the real cobra command tree (`execRoot`/`execRootJSON`), using `resetIdentityFlag`/`resetJSONFlag`/`resetRunIDFlag` to avoid flag-state leakage between steps, independently confirming reuse via an obslog `retrieval_outcome` record (not just trusting `get`'s own exit code), and — since cairn has no in-place body-edit API — correctly modeling "re-derive" as a fresh `remember` call rather than an edit. One-to-one correspondence with all 7 exit_contract items confirmed. **PASS.**

## 3. Tests

Located the documented CI-equivalent command — identical in both `.github/workflows/ci.yml` (`build-test` job) and the `Makefile`'s `test:` target: `go test ./... -race -count=1`. Ran it directly on the deploy branch (at D, not a rebased/de-drifted variant):

- `go test ./... -race -count=1` — all packages `ok`
- Re-ran with `-json`, parsed for exact per-test PASS/FAIL/SKIP counts: **691 PASS, 0 FAIL, 0 SKIP**

This differs from the review bead's recorded `694 PASS, 0 FAIL, 0 SKIP` — investigated rather than waved through. Root cause: the reviewer's notes separately flag that D's parent is 2 commits behind origin/main, and that they additionally ran tests against D *cherry-picked onto current origin/main in an isolated scratch worktree* to de-risk that drift. Confirmed the delta directly: `cmd/store_cwd_test.go` (added by #74, `crn-o6mn`) does not exist at D (`git show 898c46f:cmd/store_cwd_test.go` → no such path) but exists at origin/main with exactly 3 `Test` functions — fully accounting for 694 − 691 = 3. Both counts are correct for what they each measured; 691 is the right number for what will actually run at D, the commit actually being deployed. Zero FAIL, zero SKIP under either tree state. **PASS.**

## 4. No open blocking findings

Reviewer's findings: 0 style (gofmt clean, vet clean, `golangci-lint run ./cmd/...` 0 issues), 0 security (single new test file, no production code touched, no new deps, no injection/deserialization/sensitive-data surface — fixture content is synthetic, JSON parsing is of cairn's own CLI output). Full-repo lint scan's 8 pre-existing issues are confirmed confined to `internal/cairn/`, untouched by this diff.

Independently re-ran `golangci-lint run ./cmd/...` myself on the deploy branch. First pass returned 6 issues (gosec/nilnil/staticcheck) — but every flagged path resolved to `builder/worktrees/crn-ie0m.1/cmd/...`, a different agent's worktree entirely, and the tool's own warnings said those files didn't exist at that path (stale shared cache at `~/.cache/golangci-lint`, not this diff). Ran `golangci-lint cache clean` and re-ran: **0 issues** against the actual deploy-branch tree, matching the reviewer's scoped result exactly. None of the stale-cache findings referenced `cmd/validation_git_anchor_loop_test.go` (this diff's only file) in any case. **PASS.**

## 5. Clean working tree

Worktree at HEAD (deploy branch, cut directly from D) has no tracked modifications beyond this gate doc itself, freshly written for this evaluation (`git status --short` empty prior to writing it). **PASS.**

## 7. Single coherent theme

One new file, `cmd/validation_git_anchor_loop_test.go` (+192/-0), one test function exercising one coherent behavior: the full git-anchor lifecycle loop end-to-end. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
