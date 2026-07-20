# Release Gate: status-identity-guard

**Bead:** crn-has (deploy) — source review crn-22u — acceptance criteria crn-4bd.1
**Commit:** `284ea1d3b466ddc709c32c3a8c1f7ee82c869b2d`, cherry-picked onto `deploy/status-identity-guard` off `origin/main`
**Date:** 2026-07-19

## Note on branch provenance

The builder's original branch (`gc-builder-6ac3e0f3c1f3`) carries this commit
stacked on top of an unrelated, already-in-flight commit (`beaaee1`,
"topic_key specificity precedence in `Visible()`") belonging to a separate
bead (crn-9l6) whose own deploy bead (crn-1qb) is already closed with an open
PR (#3, https://github.com/quad341/cairn/pull/3) already merge-requested to
mayor. Pushing the shared branch again would have silently added this commit
to that already-gated PR — and would in fact have been rejected: the remote
branch has since moved to `b6dccaf` (`beaaee1` + crn-1qb's own gate-evidence
commit), which does not contain `284ea1d` as an ancestor.

To keep this deploy's unit of review scoped to exactly what crn-has /
crn-4bd.1 asked for, and to avoid touching PR #3 (not this bead's to touch),
`284ea1d` was cherry-picked in isolation onto a fresh branch cut from
`origin/main`. The cherry-pick applied cleanly — no conflicts — since
`284ea1d` only touches `cmd/commands.go`, `cmd/identity.go`, and
`cmd/commands_test.go`, none of which overlap `beaaee1`'s
`internal/cairn/entry.go` change.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Branch cut directly from `origin/main` (`5edc21d`); single cherry-picked commit on top, clean apply, zero conflicts. |
| 1 | Review PASS present | PASS | crn-22u notes: "REVIEW VERDICT: PASS" by cairn/reviewer — built the binary from the reviewed commit, gofmt/go vet/golangci-lint clean, OWASP security walk clean, no CI-fix concerns. |
| 2 | Acceptance criteria met | PASS | crn-4bd.1 AC verified live against the built binary (not just reading tests): (a) `cairn status --identity rig:alpha` → exit 1, "status is unscoped and does not filter by identity; use 'cairn map' or 'cairn prime' for a scoped view"; (b) `CAIRN_IDENTITY=rig:alpha cairn status` → same error, exit 1; (c) bare `cairn status` → exit 0, unchanged. "Covered by a test" — 3 new tests in `cmd/commands_test.go` map 1:1 to the 3 scenarios. |
| 3 | Tests pass | PASS | `go build ./...` clean. `go test ./...` — both packages ok, zero regressions. `go test -shuffle=on -count=5 -v ./cmd/...` — 15/15 pass, confirming order-independence despite `rootCmd`/`statusCmd` package-level singleton state (matches reviewer's own re-run methodology). |
| 4 | No high-severity findings open | PASS | Reviewer notes: no security findings, no CI-fix concerns. One explicitly non-blocking test-thoroughness nit (assertion checks error substring only, not the specific map/prime wording) — confirmed non-blocking, not a defect. |
| 5 | Final branch clean | PASS | `git status --short` clean on the deploy branch (no modified/staged files; only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked). |
| 7 | Single feature theme | PASS | Isolated commit touches only `cmd/commands.go`, `cmd/identity.go`, `cmd/commands_test.go` — one subsystem (CLI `status` command's identity-flag handling). No unrelated changes bundled — see branch provenance note above. |

## Verdict: PASS — proceeding to PR.
