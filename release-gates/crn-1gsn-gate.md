# Release Gate: Document untracked-working-tree-copy behavior for shared-tier remember (DESIGN.md §7)

- Bead: crn-1gsn (deploy) / crn-jqhg (review, PASS) / builder/crn-q5kk.2 (build bead, provenance only — not a push target)
- Reviewer-cited commit (R): `255b565b246fa39ce557033273d3443bf2185ea5`
- Original deploy commit (D₀): `255b565b246fa39ce557033273d3443bf2185ea5` (identical to R)
- Final deployed commit (D): `7add8ca0a5b4c4131f673306b5eefdd0791aae79` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (reflog shows a single `rebase (finish)` entry directly after branch creation, no intermediate conflict-resolution steps), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-14, against origin/main@76e3811 (#89, "Add global/role: test coverage for shared-tier remember")

## 6. Clean divergence from main (evaluated first)

Raw check at gate start FAILed: D₀ was cut from origin/main prior to #89 landing; origin/main had since advanced to `76e3811` — neither an ancestor of the other from D₀'s perspective. Resolved via `attempt_bounded_self_rebase` (`scripts/rebase-resolve-lib.sh`) on the isolated deploy branch (never a contributor fork): rc=0, `BEFORE_SHA=255b565b246fa39ce557033273d3443bf2185ea5` → `AFTER_SHA=7add8ca0a5b4c4131f673306b5eefdd0791aae79`. Reflog confirms a single-shot clean rebase (`branch: Created from 255b565b2...^{commit}` immediately followed by `rebase (finish): ...onto 76e3811...`, no `rebase (pick)`/`rebase (continue)` entries). Force-with-lease pushed to `origin/deploy/crn-1gsn-gate`; independently re-verified via `git ls-remote origin refs/heads/deploy/crn-1gsn-gate` matching local HEAD exactly (`7add8ca...`). Before/after SHAs logged to crn-1gsn's bd notes for audit. Freshly re-verified at evaluation time: `git fetch origin main` → origin/main tip `76e3811`; `git merge-base --is-ancestor origin/main HEAD` → yes. Deploy branch deliberately named `deploy/crn-1gsn-gate` (mechanically derived from this deploy bead's own id via `resolve_deploy_branch_target`), not `deploy/crn-jqhg-gate` as the bead's own prose suggests — per established crn-7dn4/crn-mhj7 precedent, prose-suggested names are ignored. **PASS**; remaining criteria evaluated against D = `7add8ca`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `255b565b246fa39ce557033273d3443bf2185ea5`, recorded as both `deploy_commit`/`tdd_green` on crn-jqhg (review, verdict `pass`) and crn-1gsn's own `**Commit:**` field — D₀ == R literally. The isolated deploy branch was cut from R via `resolve_deploy_branch_target(crn-1gsn, R)`. Criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (clean, zero-conflict rebase; diff content byte-identical to R, only base commit changed — confirmed via `git diff --stat origin/main HEAD` matching the reviewer's stated file list exactly: `doc_content_test.go`, `docs/DESIGN.md`). D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

crn-jqhg's `uncovered_criteria: none` states both required DESIGN.md elements (untracked-copy explanation + git-status insufficiency + the 3 verification commands) are covered by the single diff-owned test. Independently re-confirmed against D (post-rebase, not merely cited from review):

- Read the actual diff (`git diff origin/main HEAD -- docs/DESIGN.md`): confirms the added paragraph explains the entry file is written to the live working tree first, then committed separately onto a throwaway `remember/<id>` branch; states plainly "that's expected, not data loss"; and gives all 3 verification commands (`git branch -a`, `git log --all`, `git branch --list 'remember/*'`). Content is accurate against the actual mechanism in `internal/cairn/remember.go` (Create/CommitToReviewBranch/commitToReviewWorktree/reviewBranchName), consistent with this session's own prior firsthand familiarity with that code path.
- `TestDesignDocStatesSharedRememberLeavesWorkingTreeUntracked` (`doc_content_test.go`, the only test file in the diff) — independently re-run by exact name against D: PASS (`go test . -run '^TestDesignDocStatesSharedRememberLeavesWorkingTreeUntracked$' -v`).

Diff is additive-only (36 insertions, 0 deletions across 2 files); zero production code touched. **PASS.**

## 3. Tests

Canonical command — matches `.github/workflows/ci.yml`'s build-test job: `go build ./...` then `go test ./... -race -count=1`. Run independently on D (post-rebase, not merely cited from review):

- `go build ./...` — exit 0, clean.
- `go test ./... -race -count=1 -v` — all 7 packages `ok`, 594 PASS, 0 FAIL, 0 SKIP (grepped the complete verbose log for `--- FAIL`/`--- SKIP`/bare `FAIL`: none found). The previously-tracked pre-existing flake `TestConcurrentReindexOnColdStoreDoesNotHardFail` (crn-uxel) did not reproduce this run.
- The one diff-owned test re-checked individually by exact name against D (above under criterion 2) — PASS.

No diff-owned SKIP or FAIL. **PASS.**

## 4. No open blocking findings

crn-jqhg recorded `security_findings: none` (full 9-point OWASP-lens walk; diff is docs-only prose + one test, no executable production code, no new dependencies, no new I/O) and one non-blocking style nit: `docs/DESIGN.md` line-wrap inconsistency (one sentence at ~88 chars vs. the file's established 69–79 char convention). Independently confirmed present via the diff read above (criterion 2) and confirmed genuinely cosmetic — does not affect rendering, no content/meaning issue. `style_findings` otherwise clean, independently reconfirmed on D: `gofmt -l .` empty, `go vet ./...` exit 0, `golangci-lint run ./...` (fresh cache) — 0 issues. No HIGH or MAJOR findings of any kind; no separate finding-type bead exists referencing crn-1gsn or crn-jqhg. **PASS.**

## 5. Clean working tree

`git status --porcelain` returns empty on `deploy/crn-1gsn-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Exactly 2 commits ahead of origin/main: `b204368` (`test(feat): red`) and `7add8ca` (`feat: green`) — a complete TDD arc for one feature: documenting the shared-tier `remember` untracked-working-tree-copy behavior in DESIGN.md §7. Diff touches exactly 2 files (`doc_content_test.go`, `docs/DESIGN.md`), 36 insertions/0 deletions. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.

## Merge disposition

Per mayor's standing policy (bd comments on crn-mhj7/crn-0e1z, 2026-08-14; independently re-verified by cairn/deployer against `gh api repos/quad341/cairn/branches/main/protection` before relying on it): quad341/cairn gate 7/7 PASS + CI green (build-test, lint) ⇒ deployer merges directly (squash), no mayor escalation required. This bead's own description text still reads "route a merge-request to mayor" — that is the pre-2026-08-14 text mayor has not yet propagated everywhere; the standing policy supersedes it. Will merge directly once CI on the opened PR is confirmed green.
