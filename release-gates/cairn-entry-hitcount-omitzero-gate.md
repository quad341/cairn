# Release Gate: cairn-entry-hitcount-omitzero

**Bead:** crn-wcv7 (deploy) — source review crn-9hw7 — implementation crn-6az.5.2
**Commit:** `698153140a48bc57c370d8556ab66dd15d1a8b5b`, cut onto `deploy/crn-wcv7-gate` off `origin/main`
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Re-fetched `origin/main` immediately before evaluation (tip `ff374ca4446c319cd9f8d81c79cdb7eafcf63c96`). Ran `git merge-tree --write-tree origin/main HEAD`: clean tree, exit 0, no conflict markers. |
| 1 | Review PASS present | PASS | crn-9hw7 recorded PASS from cairn/reviewer, root cause independently confirmed against the vendored BurntSushi/toml v1.6.0 source (`isEmpty` has no `Int` case, `isZero` does). |
| 2 | Acceptance criteria met | PASS | `Entry.HitCount` TOML tag changed `omitempty` → `omitzero`; mutation-tested (reverting the tag makes `TestWriteBackOmitsZeroHitCount` fail as expected); `HitCount` confirmed the only int-kind field on Entry/Anchor needing the change. |
| 3 | Tests pass | PASS | `gofmt -l` empty, `go vet ./...` clean, `go build ./...` clean, `golangci-lint run ./...` 0 issues, `go test ./... -race -count=1` — all packages ok, zero regressions. |
| 4 | No high-severity review findings open | PASS | `bd search crn-wcv7` / `crn-9hw7` show no open HIGH findings — only this deploy bead and its sling-convoy sibling (crn-ljsi). |
| 5 | Final branch clean | PASS | `git status` clean on `deploy/crn-wcv7-gate` immediately after cut. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` merge-base touches exactly 2 files: `internal/cairn/entry.go` (1 line), `internal/cairn/entry_test.go` (40 insertions) — one coherent fix. |

## Verdict: PASS — proceeding to PR.

## Note

Source branch `feature/crn-6az.5.2` was local-only/unpushed in the builder's own scratch worktree per the bead's description — extra reason the deploy branch was cut directly from the reviewed commit rather than reused as a push target.
