# Release Gate: cairn-prime-remember-footer-fix (reconciled)

**Bead:** crn-ilu8 (deploy) — supersedes crn-3b85 (closed) — source review crn-o6j7 — reconciliation crn-6az.2
**Commit:** `1da41e803af62d2bdfe8b397ff387158b445132f`, evaluated in isolated detached-HEAD worktree
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git fetch origin main` → tip `0b81dae4b1ca2fa85866c0575cb44faf885870ce`. This commit's recorded parent is exactly that SHA (confirmed via `git merge-base --is-ancestor`) — zero divergence, no rebase needed. |
| 1 | Review PASS present | PASS | crn-ilu8's description records "Re-reviewed + PASSED by reviewer cairn/reviewer after builder reconciliation." Original footer-text fix independently reviewed PASS in crn-o6j7 ("Two low-severity, non-blocking observations only", nothing blocking). |
| 2 | Acceptance criteria met | PASS | Diff confirmed: `internal/cairn/prime.go`'s footer `b.WriteString` call replaced with a pointer to `cairn remember <body>` (private tier commits directly; shared tiers route through review). Cross-checked against `cmd/remember.go` (confirms private/shared tier flags + `--reviewer` review-routing) and `docs/DESIGN.md` §7 (table: `agent/…` commits straight to main, `rig/…`/`global/…` route through branch+curator review) and §8 (CLI list already includes `cairn remember <body>`) — footer text is accurate. PR #23/crn-rbjm's `scopeMismatchWarnings` diagnostic block (merged to main at 703f58e) is preserved byte-for-byte immediately above the footer — confirmed via direct diff read, not just the commit message's claim. |
| 3 | Tests pass | PASS | On commit `1da41e80`, in a fresh isolated detached-HEAD worktree: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./... -race -count=1` — all packages (`cmd`, `internal/cairn`, `internal/critic`, `scripts`) ok. `golangci-lint run ./...` 0 issues (shared cache cleared first, per known cross-worktree cache gotcha). PR #23's own regression tests (`TestPrimeWarnsOnUnmatchedScopeDimension`, `TestPrimeNoWarningOnScopeMatch`, `TestPrimeNoWarningOnEmptyScopeDimension`) all still pass — reconciliation didn't regress that feature. |
| 4 | No high-severity review findings open | PASS | crn-o6j7 (original review) confirms "Two low-severity, non-blocking observations only" — nothing blocking; no findings recorded against crn-ilu8/crn-3b85. |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on the evaluated commit. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` touches exactly 2 files: `internal/cairn/prime.go`, `internal/cairn/prime_test.go` (17 insertions, 3 deletions total) — one isolated footer-text fix plus its updated/added assertions. |

## Independent verification

Mutation-tested beyond what the reviewer reported: reverted only the footer text (keeping the new assertions) in the isolated worktree via an exact string swap — both `TestPrime` and `TestPrimeDoesNotClaimRememberMissing` fail exactly as expected (old stale text detected: "does not contain \"cairn remember\""; "should not contain \"no `remember` command yet\""). Restored cleanly via `git checkout --`; full suite re-confirmed green afterward. Confirms the new tests are load-bearing, not vacuous.

## Verdict: PASS

Superseded artifact: crn-3b85 (original deploy bead) is closed. Its would-be `deploy/crn-3b85-gate` was a stale local-only branch, never pushed and no PR opened (deploy hit the real conflict against PR #23 before a branch was cut) — nothing to close on GitHub for it.

Proceeding: cut `deploy/crn-ilu8-gate` from `1da41e80` via `resolve_deploy_branch_target`, push, open PR, arm `gh pr merge --auto`.
