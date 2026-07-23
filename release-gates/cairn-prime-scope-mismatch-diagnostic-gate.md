# Release Gate: cairn-prime-scope-mismatch-diagnostic

**Bead:** crn-rbjm (deploy) — source feature crn-ln1 — source review crn-mec
**Commit:** `861021600b6e686dc021fd6dd87d97cc4c7cbda8`, deploy branch `deploy/crn-rbjm-gate` cut directly from this SHA
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree $(git merge-base 8610216 origin/main) 8610216 origin/main` (origin/main at `4fc294a`) — zero conflict markers; origin/main has moved on with unrelated commits but none touch `internal/cairn/entry.go`, `internal/cairn/prime.go`, or `internal/cairn/prime_test.go`. |
| 1 | Review PASS present | PASS | crn-mec: reviewer cairn/reviewer verdict PASS. Full gate green independently (go build, go vet, gofmt, go test ./... -race -count=1, golangci-lint 0 issues). |
| 2 | Acceptance criteria met | PASS | crn-ln1's 6 ACs verified line-by-line by the reviewer against the diff (crn-mec notes): warning text is a byte-for-byte match of the AC's own example; warning placement confirmed unconditional/after-if-else via full-file read; `Visible()`'s external contract confirmed unchanged (`entry_test.go` passes unmodified); all 3 new tests confirmed to genuinely exercise the claimed state. |
| 3 | Tests pass | PASS | Re-verified fresh on `deploy/crn-rbjm-gate` at `8610216`: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `go test ./... -race -count=1` — `cmd`, `internal/cairn`, `internal/critic` all ok, zero regressions. |
| 4 | No high-severity findings open | PASS | `golangci-lint run ./...` (cache cleared first per known shared-cache staleness issue) — 0 issues. crn-mec notes: no security concerns (no externally-controlled format strings, no new I/O). |
| 5 | Final branch clean | PASS | `git status --porcelain` on the deploy branch — no modified/staged files; only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Diff scoped to exactly `internal/cairn/entry.go`, `internal/cairn/prime.go`, `internal/cairn/prime_test.go` (110 insertions, 2 deletions) — one subsystem (`Prime()`'s scope-dimension silent-miss warning). New/changed code (`visibleFrom`, `scopeMismatchWarnings`, `anyTagWithPrefix`) at 100% statement coverage; `Prime()`'s one uncovered branch is a pre-existing untested `IterEntries` error path, not introduced by this diff. |

## Note on infra blocker (resolved)

This bead's gate was originally evaluated PASS on 2026-07-22 but deploy was
blocked because `scripts/rebase-resolve-lib.sh` (`resolve_deploy_branch_target`
/ `assert_safe_push_target`) was not yet present rig-wide. That script landed
on `origin/main` at commit `128c214` (PR #18). This worktree's branch predates
that landing, so the script was sourced from `origin/main` into scratch and
used to cut the isolated deploy branch below — see `bd memories deploy` for
the resolution record.

## Verdict: PASS — proceeding to PR.
