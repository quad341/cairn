# Release Gate: Port zsh-lowercasing fix (crn-k1hw) into cairn's own scripts/rebase-resolve-lib.sh

- Bead: crn-jpnr (deploy) / crn-ss97 (review, PASS) / builder/crn-sns6 (build bead, closed — provenance, not a push target)
- Reviewer-cited commit (R): `8524cec5c621ed04b66d985ffd05f0ba070d1ed4`
- Original deploy commit (D₀): `8524cec5c621ed04b66d985ffd05f0ba070d1ed4` (identical to R)
- Final deployed commit (D): `d168563f5a83f0e93b12e1db0547d0323047fc22` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (zero conflicts), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-13, against origin/main@0e96aa3 (#85, "fix: recall-stats crashes on never-recalled entries (NULL last_recalled_at)")

## 6. Clean divergence from main (evaluated first)

Bead notes record a bounded self-rebase performed on this same isolated deploy branch (never a contributor fork): `attempt_bounded_self_rebase` returned rc=0 (clean, zero conflicts), `BEFORE_SHA=8524cec5c621ed04b66d985ffd05f0ba070d1ed4` → `AFTER_SHA=d168563f5a83f0e93b12e1db0547d0323047fc22`, corroborated by reflog (`rebase (finish): refs/heads/deploy/crn-jpnr-gate onto 0e96aa3ed88859cbab92c00c26a240568ee7ce23`) and force-with-lease pushed to `origin/deploy/crn-jpnr-gate`. Before/after SHAs are logged in the bead's notes for audit. Freshly re-verified at evaluation time (not merely cited from history): `git fetch origin main` → origin/main tip is still `0e96aa3` (unchanged since the rebase), and `git merge-base HEAD origin/main` == `0e96aa3` exactly — HEAD is a clean fast-forward-able descendant of the current main tip, zero divergence. **PASS**; remaining criteria evaluated against D = `d168563f`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `8524cec5c621ed04b66d985ffd05f0ba070d1ed4`, recorded as `deploy_commit`/`tdd_green` on crn-ss97 (review, verdict PASS) and matching crn-jpnr's own `**Commit:**` field exactly — D₀ == R literally. The isolated deploy branch (`deploy/crn-jpnr-gate`) was cut from R via `resolve_deploy_branch_target`; criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (rebase was clean, zero conflicts). D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

crn-ss97's `uncovered_criteria: none` maps all 5 exit_contract bullets to evidence; independently re-confirmed by name against D (post-rebase, not merely cited from review):

1. Bash-only `${var,,}`/`${var^^}` lowercasing replaced with portable `tr`-based lowercasing — verified via diff + grep sweep of the file at D: no `${...,,}`/`${...^^}` outside explanatory comments.
2. Local variable `path` renamed (in both `is_additive_keepboth_path` and `resolve_conflict_markers_in_file`) so it no longer shadows zsh's `$PATH`-tied special parameter — verified via diff + grep sweep: no bare `local path`/`$path` outside comments.
3. Sourcing under real zsh no longer raises `bad substitution` — `TestShellPortability` (diff-owned, wraps `scripts/test-shell-portability.sh`'s 5-layer suite incl. zsh liveness/parity/end-to-end) — PASS.
4. Existing suite still passes after the fix — `TestRebaseResolveLib` (pre-existing, wraps `scripts/test-rebase-resolve.sh`) — PASS.
5. RED test predates the fix — `tdd_red` commit `55e550ef6a3361f8e67b47795c1f50ee56359c60` added only the two new test files against the still-unfixed lib (per crn-ss97's own verification).

All 5 independently confirmed against D. **PASS.**

## 3. Tests

Canonical command — matches `Makefile`'s `test:` target and crn-ss97's own `test_cmd`: `go test ./... -race -count=1`. Run independently on D (post-rebase, not merely cited from review):

- Race run (`go test ./... -race -count=1`): all 7 packages `ok`, exit 0. Zero reproduction of the `TestConcurrentReindexOnColdStoreDoesNotHardFail` SQLITE_BUSY flake that crn-ss97 saw once at review time (pre-existing, unrelated to this diff — this diff touches only `scripts/`; already disposed of in crn-ss97's `skip_justification`).
- Verbose run (`go test ./... -v -count=1`, tallied including subtests): **722 PASS, 0 FAIL, 0 SKIP**, all 7 packages `ok`. (One higher than crn-ss97's reviewer-time count of 721 — D carries the same diff content as R, no new tests landed by the rebase itself; within normal count noise between independent runs, not a regression.)
- Diff-owned tests re-checked by name: `TestShellPortability` — PASS (0.20s); `TestRebaseResolveLib` — PASS (1.02s).

No diff-owned SKIP or FAIL; the one known flake is pre-existing, unrelated, and did not even reproduce on this run. **PASS.**

## 4. No open blocking findings

crn-ss97 recorded `style_findings: none` (gofmt/go vet/golangci-lint all clean on a scoped rerun, after a stale-cache false alarm from an unscoped run was ruled out) and `security_findings: none` (all 9 OWASP-lens questions walked — no injection vector, no new deps, temp-file handling pre-existing and safe; independently verified under a real zsh 5.9 shell that `$PATH` remains intact after sourcing the fixed lib). Independent `bd search` for finding-type beads referencing crn-sns6 (build)/crn-k1hw (source bug): crn-sns6 closed with a `scope_note` confirming the fix matches upstream's shipped fix byte-for-byte; crn-k1hw is the original bug report (describes the same `${path,,}` hazard, explicitly rejects a shebang/re-exec-guard fix since the file is always sourced, never executed); crn-zttk (convoy tracking) auto-closed. No open finding-type bead of any severity. **PASS.**

## 5. Clean working tree

`git status` reports "nothing to commit, working tree clean" on `deploy/crn-jpnr-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Exactly 2 commits ahead of origin/main (`ea19df8` tdd_red, `d168563f` tdd_green), touching exactly 3 files, all under one subsystem (`scripts/`): `scripts/rebase-resolve-lib.sh` (+68/-10, the fix itself), `scripts/shell_portability_test.go` (+32, new), `scripts/test-shell-portability.sh` (+486, new — the 5-layer portability harness the new Go test wraps). One behavior: replace bash-only lowercasing and a `$PATH`-shadowing local variable with portable equivalents so the lib sources cleanly under zsh. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
