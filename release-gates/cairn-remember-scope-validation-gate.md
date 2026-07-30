# Release Gate: cairn remember --scope validation fix

Bead: crn-3fpj (deploy) — from crn-ijsn (review) — from crn-pa7v (bug/design)
Deployed commit (D): `9fef49ba6b52c200d1597e264e2bb0d3f5f52df1`
Reviewed commit (R, review_round_2_verdict): `9fef49ba6b52c200d1597e264e2bb0d3f5f52df1`

## Summary

`cairn remember --scope` accepted malformed tags (bare `global`, `*`,
unknown tiers) and silently routed them to the highest-blast-radius
(global) tier, while an explicit `--scope ''` (the intuitive spelling for
"global") silently degraded to a private entry invisible to its own
author. Fix adds `internal/cairn.ValidateScopeTag` (allow-listed
`tier:value` grammar) and wires `Changed("scope")` detection so an
explicit empty string is treated as a deliberate global-tier request,
distinct from the flag being omitted entirely.

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | **PASS** | `git merge-base D origin/main` == `origin/main` tip (`05b93505c`) == same as `merge-base --is-ancestor origin/main D`. Clean 4-commit linear stack directly on current main; no rebase needed. |
| 1 | Review PASS present for the deployed commit | **PASS** | crn-ijsn `review_round_2_verdict: PASS`, reviewed commit cited as `9fef49ba6b52c200d1597e264e2bb0d3f5f52df1` — identical to D. No commits appended after review. |
| 2 | Acceptance criteria met | **PASS** | Both crn-pa7v design-doc AC(a) (explicit `--scope ''` → global, visible to own author) and AC(b) (malformed tag rejected at both CLI entry points) independently spot-checked: read `internal/cairn/validate.go`'s new `ValidateScopeTag` and `cmd/remember.go`'s `Changed("scope")` handling directly — matches design Part C line for line. Ran the 5 named acceptance tests individually (`-run`): all PASS. |
| 3 | Tests pass | **PASS** | `make test` (`go test ./... -race -count=1`, Makefile:38, documented CI-equivalent) on D: 6/6 packages ok. Verbose recount: 661 PASS, 0 FAIL, 0 SKIP. `gofmt -l .`: clean. `go build ./...`: clean. `go vet ./...`: clean. `go.mod`/`go.sum`: no diff (no new deps). |
| 4 | No high-severity review findings open | **PASS** | Reviewer's OWASP walk (both rounds) found no blocking findings; net effect is an access-control/misconfiguration risk *reduction*. Independently read the diff: value still passes through existing `ValidatePathSegment`, no new I/O/deps/auth surface. |
| 5 | Final branch is clean | **PASS** | `git status --porcelain` at D: empty. |
| 7 | Single feature theme | **PASS** | All 8 changed files (`cmd/remember.go`, `cmd/review.go`, `internal/cairn/{validate,doctor,review}.go` + 3 test files) serve one theme: scope-tag validation grammar. Diffstat: 8 files, +289/-41 — matches reviewer's independently-confirmed figure exactly. |

**All 7 criteria PASS.**

## Test evidence detail

- Command: `go test ./... -race -count=1` (Makefile `test` target, line 38)
- Result: `ok` on all 6 packages (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`)
- Verbose subtest count: 661 PASS, 0 FAIL, 0 SKIP
- Named acceptance tests individually confirmed passing via `-run`:
  `TestRememberRejectsMalformedScopeTags`,
  `TestRememberExplicitEmptyScopeWritesGlobalEntry`,
  `TestRememberGlobalEntryIsVisibleToItsOwnAuthor`,
  `TestReviewMergeRejectsMalformedScopeTags`,
  `TestReviewMergeExplicitEmptyScopeClearsToGlobal`

## Notes

No self-rebase was needed (criterion 6 passed directly). Deploying from an
isolated `deploy/crn-3fpj-gate` branch cut at commit D, never from the
shared `builder/crn-pa7v` branch name carried in bead prose.
