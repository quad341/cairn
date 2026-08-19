# Release Gate: cairn librarian `branches --stale` escalate-pass reviewer-resolution failure renders as literal "null" in bead body

- Bead: crn-e6pc7 (deploy) / crn-4vys9 (review, PASS) / builder/crn-w4c6 (build bead — provenance, not a push target)
- Reviewer-cited commit (R): `752c649a67a17e8d07db9586fbf68920bae416bb` (tdd_green)
- Original deploy commit (D₀): `752c649a67a17e8d07db9586fbf68920bae416bb` (identical to R)
- Final deployed commit (D): `48516a4b3c54043c638641c2b874d5b6978f1f46` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (rc=0, single-shot, no conflict-resolution loop entered), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-19, against origin/main@95086a9 (#117, "Add ancestry-scope check for deploy branches")

## 6. Clean divergence from main (evaluated first)

Raw check at gate start FAILed: D₀ was cut from `4ad2f82` (#114); origin/main had since advanced to `95086a9` (#117 — two commits ahead: #116 doctor-explain fix, #117 ancestry-scope check) — neither an ancestor of the other from D₀'s perspective. `git merge-tree --write-tree origin/main HEAD` first confirmed a conflict-free 3-way merge (no CONFLICT markers), then resolved for real via `attempt_bounded_self_rebase` (`rebase-resolve-lib.sh`) on the isolated deploy branch: rc=0, `BEFORE_SHA=752c649a67a17e8d07db9586fbf68920bae416bb` → `AFTER_SHA=48516a4b3c54043c638641c2b874d5b6978f1f46`. The branch's own two commits (`4638a51` red, `48516a4` green) contain no internal merge commits, so there is no stale-pre-resolution-patch risk; the rebase applied with zero conflict-resolution loop iterations. Force-with-lease pushed to `origin/deploy/crn-e6pc7-gate`; independently re-verified via `git ls-remote origin refs/heads/deploy/crn-e6pc7-gate` matching local HEAD exactly (`48516a4...`). `git merge-base --is-ancestor origin/main HEAD` now succeeds — the branch is a clean fast-forward candidate onto current main. Before/after SHAs recorded here for audit. Freshly re-verified at evaluation time: `git fetch origin main` → origin/main tip still `95086a9`. **PASS**; remaining criteria evaluated against D = `48516a4`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `752c649a67a17e8d07db9586fbf68920bae416bb`, recorded as both `deploy_commit` on crn-4vys9 (review, verdict `pass`) and crn-e6pc7's own `**Commit:**` field — D₀ == R literally. The isolated deploy branch (`deploy/crn-e6pc7-gate`) was cut from R via `resolve_deploy_branch_target(crn-e6pc7, R)`, after `assert_deploy_ancestry_scope` confirmed the branch's commits cite only `crn-e6pc7` and its named sibling `crn-w4c6` (the build bead) — no unrelated bead IDs. Criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (clean, zero-conflict rebase, confirmed above; diff content byte-identical, only base commit changed — same 2 commits, same messages, same 60-line diff). D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

Build bead crn-w4c6 named the exact remediation: "have evaluateBranch clear f.Status back to a non-actionable value when reviewer resolution fails on the escalate path too." The diff implements precisely this — `cmd/branches.go`'s `evaluateBranch` now remaps `f.Status` from `"escalate"` to `"error"` when `resolveReviewer` fails on the escalate pass, alongside the existing `f.Error` population. crn-4vys9's `uncovered_criteria: none` confirms full 1:1 coverage. New test `TestStaleBranchesEscalateReviewerResolutionFailureReportsError` (`cmd/branches_test.go`) asserts all four resulting fields after a successful first (notify) pass followed by a failing second (escalate) pass: `Status=error`, `Reviewer` empty, `Notified=false`, `Error` non-empty. Independently re-confirmed by exact name against D (post-rebase, not merely cited from review):

- `TestStaleBranchesEscalateReviewerResolutionFailureReportsError` — PASS (0.03s)

**PASS.**

## 3. Tests

Canonical command — matches the Makefile's documented `test` target and crn-4vys9's own `test_cmd`: `go build ./...` then `go test ./... -race -count=1`. Run independently on D (post-rebase, not merely cited from review):

- `go build ./...` — exit 0, clean.
- `go test ./... -race -count=1` — all 7 packages `ok` (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`), 0 FAIL, 0 SKIP (crn-4vys9 independently recorded 714 PASS / 0 FAIL / 0 SKIP on D₀; re-run against D shows the same all-`ok` result, as expected for a zero-conflict rebase that changed only the base commit).
- The one diff-owned test re-checked individually by exact name against D (listed above under criterion 2) — PASS.

No diff-owned SKIP or FAIL, no flakes observed. **PASS.**

## 4. No open blocking findings

crn-4vys9 recorded `style_findings: none` (`gofmt -l` clean on all 3 changed files; `go vet ./...` exit 0) and `security_findings: none` (3-line control-flow-only change; no injection/auth/SSRF/access-control/XSS surface touched; no new deps; reuses an already-reviewed `Error` field rather than introducing a new exposure surface). No HIGH, MAJOR, or MINOR findings of any kind recorded against this diff; no separate finding-type bead exists referencing crn-e6pc7 or crn-4vys9. **PASS.**

## 5. Clean working tree

`git status --porcelain` returns empty on `deploy/crn-e6pc7-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Diff is `cmd/branches.go` (+3), `cmd/branches_test.go` (+41), `cmd/remember_test.go` (+16) — 3 files, 60 insertions, 0 deletions, two commits (`4638a51` red / `48516a4` green, standard TDD red-green pair, unchanged in content by the base rebase). `cmd/remember_test.go`'s diff adds one non-test helper (`stubGCWithRig`) with no `Test*` of its own — exercised transitively via the one new test in `branches_test.go`, which uses it for both its successful (notify) and failing (escalate) `resolveReviewer` passes. Entirely confined to one control-flow bug fix in `evaluateBranch`'s escalate path. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7)

Proceeding to: PR open from `deploy/crn-e6pc7-gate` → `main`. Per crn-e6pc7's own routing instruction ("merge authority is operator/mayor/mpr only — no rig agent runs `gh pr merge`"), this deploy does NOT proceed to deployer-run auto-merge arming; a merge-request is routed to mayor/mpr instead, bead left open under `hold:mayor` pending mayor's own merge action.
