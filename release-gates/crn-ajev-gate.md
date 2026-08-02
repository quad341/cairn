# Release Gate: cairn remember --store defaults to CWD (security fix)

Bead: crn-ajev (review/deploy) — build bead crn-jzhd
Deployed commit (D): `20327a685e301c8a228fc1e3aa32edb6a42d47b9`
Reviewed commit (R): `20327a685e301c8a228fc1e3aa32edb6a42d47b9`

## Summary

`storePathWithSource()` silently fell back to `.` (the current working
directory) when neither `--store` nor `$CAIRN_STORE` was set — letting an
agent silently write knowledge into whatever repo it happened to be
standing in, including the public `quad341/cairn` repo itself. The fix
removes the silent default and hard-gates via `PersistentPreRunE`, refusing
any command that isn't in a narrow 2-entry exemption list (bare `cairn`,
`version`) unless a store is explicitly configured.

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | **PASS** | `git merge-base origin/builder/crn-jzhd origin/main` == `origin/main` tip (`e34dedf3`) exactly — branch is a pure fast-forward ahead of main, zero divergence, no rebase needed. |
| 1 | Review PASS present for the deployed commit | **PASS** | crn-ajev review verdict: PASS. Reviewed commit cited in bead notes (`HEAD: 20327a6`) — identical to D, full SHA confirmed via `git rev-parse origin/builder/crn-jzhd`. No commits appended after review. |
| 2 | Acceptance criteria met | **PASS** | 3 ACs (default-fallback change, hard-gate on non-exempt commands, exemption map lets store-independent commands run) each named to a passing test (`TestStorePathWithSourceDefault`, `TestRootRefusesWhenNoStoreConfigured`, `TestRootExemptsVersionFromStoreGate`); coverage profile in bead notes confirms both branches of the new gate are hit. Independently confirmed all 3 tests pass in the full run below. |
| 3 | Tests pass | **PASS** | `go test ./... -race -count=1` (Makefile `test` target, documented CI-equivalent, matches `.github/workflows` `build-test` job) on D: 6/6 packages ok. Verbose recount: 513 PASS, 0 FAIL, 0 SKIP — matches reviewer's independently-reported figure exactly. `go build ./...`: clean. `go vet ./...`: clean. `gofmt -l cmd/root.go cmd/root_test.go`: clean. `golangci-lint run ./...`: 0 issues (first run returned 13 stale issues from a golangci-lint cache entry tagged to an unrelated, already-deleted reviewer scratchpad worktree path; `golangci-lint cache clean` + rerun confirmed 0 issues — the same stale-cache artifact the reviewer's own notes already documented for this bead). `go.mod`/`go.sum`: untouched, no new deps. |
| 4 | No high-severity review findings open | **PASS** | All 3 linked finding beads closed clean: crn-ixqw (style/lint, 0 issues), crn-wsca (OWASP Top 10 walk, no blocker/major/minor — diff is itself an insecure-default fix), crn-xf9f (spec compliance, 513/0/0). |
| 5 | Final branch is clean | **PASS** | `git status --porcelain` at D: empty. |
| 7 | Single feature theme | **PASS** | 2 changed files (`cmd/root.go`, `cmd/root_test.go`), +55/-5 — one theme: hard-gating the store-path default. |

**All 7 criteria PASS.**

## Test evidence detail

- Command: `go test ./... -race -count=1` (Makefile `test` target)
- Result: `ok` on all 6 packages (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`)
- Verbose subtest count: 513 PASS, 0 FAIL, 0 SKIP
- Named acceptance tests individually confirmed passing:
  `TestStorePathWithSourceDefault`,
  `TestRootRefusesWhenNoStoreConfigured`,
  `TestRootExemptsVersionFromStoreGate`

## Notes

No self-rebase was needed (criterion 6 passed directly — branch already a
clean fast-forward off current `origin/main`). Deploying from an isolated
`deploy/crn-ajev-gate` branch cut at commit D, never from `builder/crn-jzhd`
directly.
