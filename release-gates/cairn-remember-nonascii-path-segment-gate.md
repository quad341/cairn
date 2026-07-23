# Release Gate: cairn-remember-nonascii-path-segment

**Bead:** crn-f10v (deploy) — source review crn-419.7 — implementation crn-419.5
**Commit:** `bc42693950fe142864a434cf40d91293b120e558`, cut onto `deploy/crn-f10v-gate` off `origin/main`
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Re-fetched `origin/main` immediately before evaluation (tip `703f58e7c043c15f51fb3a1ca3836c9d317cd7c8`). Ran `git merge-tree --write-tree origin/main bc42693...`: clean tree, exit 0, no conflict markers. |
| 1 | Review PASS present | PASS | crn-419.7 recorded PASS from cairn/reviewer on this exact commit. |
| 2 | Acceptance criteria met | PASS | crn-419.5's AC: `ValidatePathSegment` rejects non-ASCII dot-trick path-traversal lookalikes (fullwidth full stop, one/two-dot leaders, horizontal ellipsis, ideographic full stop, zero-width-space-split dot-dot) without regressing the existing ASCII accept-list. Confirmed by diff: tightened rune check `r < 0x20 || r >= 0x7f` in `internal/cairn/validate.go`. |
| 3 | Tests pass | PASS | Run directly in an isolated detached worktree: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `golangci-lint run ./...` 0 issues (shared cache cleaned first), `go test ./... -race -count=1` — all packages ok. |
| 4 | No high-severity review findings open | PASS | Only routing bead (crn-c97c, convoy) references this deploy bead — no open findings. |
| 5 | Final branch clean | PASS | `git status` clean on `deploy/crn-f10v-gate` immediately after cut. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` merge-base touches exactly 4 files: `cmd/remember_test.go`, `internal/cairn/remember_test.go`, `internal/cairn/validate.go`, `internal/cairn/validate_test.go` (295 insertions, 3 deletions) — one subsystem (path-segment validation), one coherent fix. |

## Verdict: PASS — proceeding to PR.

## Note on prior blocker

This bead was previously blocked pending `scripts/rebase-resolve-lib.sh` (mayor escalation `gm-wisp-0qrv2gi`). Mayor has since confirmed (mail `gm-wisp-82azqz0`) this class of `hold:mayor` is fleet-wide stale, conditioned on the infra block being the bead's *sole* stated blocking reason — true here per crn-f10v's own notes. The gate above is a full independent re-evaluation against the current `origin/main` tip, not a replay of the earlier PASS.
