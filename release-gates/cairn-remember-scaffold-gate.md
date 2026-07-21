# Release Gate: cairn-remember-scaffold

**Bead:** crn-4do (deploy) — source review crn-mrt — acceptance criteria crn-419.1
**Commit:** `2723849647c49f0d7946441565f2d59acd1c31e4` (fix-up on top of `9c7d59de528e4f9a74a91021b4a358c223cca445`), cherry-picked onto `deploy/crn-4do-gate` off `origin/main`
**Date:** 2026-07-21

## Note on branch provenance

The builder's shared branch (`gc-builder-6ac3e0f3c1f3`) carries this bead's
two commits (`9c7d59d` scaffold, `2723849` AC #2 fix) stacked among five
other already-separately-gated beads' commits — topic_key precedence
(crn-9l6, PR #7), status-identity-guard (crn-4bd.1, PR #4), the `get <id>`
command (PR #5), the CI `merge_group` trigger (PR #8), and ShadowMap (PR #9)
— all of which have since squash-merged into `origin/main` under different
commit hashes. Branching at the raw reviewed SHA would silently re-bundle
all of that already-shipped, unrelated work into this PR: confirmed by
`git merge-tree`, which shows a real conflict on `cmd/commands_test.go`
between the SHA's un-squashed ancestry and main's squashed version of the
same content — an artifact of the shared branch's stale history, not a
defect in this bead's own change.

To keep this deploy's unit of review scoped to exactly what crn-4do /
crn-419.1 asked for, `9c7d59d` and `2723849` were cherry-picked in isolation
onto a fresh branch cut from `origin/main` (`c811beb`). Both cherry-picks
applied cleanly — zero conflicts — since they only touch `cmd/remember.go`,
`cmd/remember_test.go`, `internal/cairn/validate.go`,
`internal/cairn/validate_test.go`, and `go.mod`/`go.sum`, none of which
exist or were touched on `origin/main` before this change. A full-tree diff
of the isolated branch against the original reviewed commit `2723849`, for
every file either touches, is empty — byte-identical result.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Branch cut directly from `origin/main` (`c811beb`); two cherry-picked commits on top, clean apply, zero conflicts. See branch provenance note above. |
| 1 | Review PASS present | PASS | crn-mrt notes: "VERDICT: pass (re-review)" by cairn/reviewer. First pass (`9c7d59d`) found one AC #2 gap (request-changes); builder fixed it (`2723849`); re-review independently verified `go build`/`go vet`/`gofmt`/`go test -race`/`golangci-lint` all green and confirmed the fix on the exact commit. |
| 2 | Acceptance criteria met | PASS | All 5 ACs (crn-419.1) independently verified against code on this branch, not just notes: (1) `rememberCmd` registered via `init()`+`AddCommand`, `--topic`/`--scope` flags, `cobra.ExactArgs(1)` body arg — `cmd/remember.go:12-21,28-31`. (2) `defaultScope()` extracts exactly the single `agent:`-prefixed tag from the resolved identity, erroring (not silently widening scope) if none present — `cmd/remember.go:71-79`. (3)+(4) `ValidatePathSegment` rejects empty / slash-containing / `..`-containing / leading-dot / control-or-null-byte values before any filesystem access; a rejection propagates as a non-zero exit via the wrapped error, and zero filesystem code exists anywhere in this scaffold (write-back is deliberately deferred to crn-419.2) — `internal/cairn/validate.go`, `cmd/remember.go:32-48`. (5) Attack corpus present verbatim in `internal/cairn/validate_test.go`: path traversal (`../../etc/passwd`), absolute path (`/etc/passwd`), leading dot (`.hidden`), embedded NUL, empty string, plus bare `..` hardening. |
| 3 | Tests pass | PASS | `go build ./...` clean. `go vet ./...` clean. `gofmt -l .` clean (no output). `go test ./... -race -count=1` — both packages ok, zero regressions. `golangci-lint run ./...` — 0 issues. |
| 4 | No high-severity findings open | PASS | One finding from first-pass review (AC #2, medium-high, spec-compliance: `defaultScope` returned the full multi-tag identity instead of a single `agent:` tag) — fixed in `2723849`, re-review independently re-verified the fix via `TestDefaultScopeCollapsesToSingleAgentTag`/`TestDefaultScopeErrorsWithoutAgentTag` and the original repro. Zero new findings raised in re-review. No open HIGH findings. |
| 5 | Final branch clean | PASS | `git status --short` clean on the deploy branch (no modified/staged files); only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Isolated commits touch only `cmd/remember.go`, `cmd/remember_test.go`, `internal/cairn/validate.go`, `internal/cairn/validate_test.go`, `go.mod`, `go.sum` — one subsystem (the `remember` CLI scaffold and its input-validation guard). No unrelated changes bundled — see branch provenance note above. |

## Verdict: PASS — proceeding to PR.
