# Release Gate: remember recurrence dedup silently discards a --topic collision

Bead: crn-im3h (deploy) — review bead crn-vpa3 — build bead crn-qxj3
Deployed commit (D): `d2b383658f3df87c4f14a285d95949d9aa071069`
Reviewed commit (R): `d2b383658f3df87c4f14a285d95949d9aa071069`

## Summary

`cairn remember` matched recurrence on `--topic` alone rather than on body
similarity, so a second, genuinely distinct entry filed under an
already-used topic was silently discarded while the CLI still printed a
plausible sha and exited 0 — indistinguishable from success. The fix makes
`recurrenceMatch` require both topic-key and content signals to agree
before treating a write as a repeat, and turns a real recurrence into a
loud, non-zero-exit error on stderr (plus a `CategoryConflict` envelope in
`--json` mode) instead of a silent no-op. `--force` still stores the entry
as a distinct record when a recurrence is intentional.

## Gate criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | **PASS** | `git merge-base origin/main D` == `origin/main` tip (`c7d836e5`) exactly — D is a pure fast-forward ahead of main (3 commits: red/green/lint-fix), zero divergence, no rebase needed. |
| 1 | Review PASS present for the deployed commit | **PASS** | crn-vpa3 review verdict: `pass`. `reviewed_commit` field in crn-vpa3's notes is `d2b3836...` — identical to D (full SHA match, not just an ancestor). No commits appended after review. |
| 2 | Acceptance criteria met | **PASS** | All 4 acceptance criteria from crn-qxj3's suggested fix mapped to named passing tests in crn-vpa3 (body-vs-topic matching, loud non-zero-exit discard, no fabricated sha on discard, `--force` escape hatch preserved). Independently re-confirmed via a clean `go build`/`go vet`/full test run on D below. |
| 3 | Tests pass | **PASS** | `go test ./... -race -count=1` (Makefile `test:` target, matches `.github/workflows` CI) on D: 6/6 packages `ok` (cmd 34.0s, internal/cairn 33.4s, internal/critic 18.3s, formulas/obslog/scripts <2s each). Reviewer's independent `-json` recount: 667 PASS, 0 FAIL, 0 SKIP — this run reproduces the same all-green result via the identical command. `go build ./...`: clean. `go vet ./...`: clean. `golangci-lint`: 0 issues per reviewer (re-verified in an isolated worktree after ruling out a stale-cache false positive). |
| 4 | No high-severity review findings open | **PASS** | Review notes report zero blocker/major/minor findings (style, security/OWASP walk, spec compliance all clean). No open child finding-beads under crn-vpa3 or crn-qxj3 (`bd list --parent` empty for both). |
| 5 | Final branch is clean | **PASS** | `git status --porcelain` at D: empty. |
| 7 | Single feature theme | **PASS** | 4 changed files (`cmd/format.go`, `cmd/remember.go`, `cmd/remember_test.go`, `internal/cairn/entry.go`), +152/-96 — one theme: recurrence/conflict detection in the `remember` command. |

**All 7 criteria PASS.**

## Test evidence detail

- Command: `go test ./... -race -count=1` (Makefile `test` target)
- Result: `ok` on all 6 packages (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`)
- Reviewer's verbose `-json` recount: 667 PASS, 0 FAIL, 0 SKIP
- Named acceptance tests confirmed passing in review (crn-vpa3 notes):
  `TestRememberDistinctBodySameTopicKeyIsStoredNotDiscarded`,
  `TestRememberSameScopeTopicKeyRepeatIncrementsRecurrence`,
  `TestRememberNearMissTopicKeyDoesNotIncrementRecurrence`,
  `TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch`,
  `TestRememberCrossCallPrivateTierRecurrenceCommitsDirectly`,
  `TestRememberJSONRecurrenceReportsConflictError`,
  `TestRememberForceOverridesRecurrenceMatchPrivateTier`,
  `TestRememberForceOverridesRecurrenceMatchSharedTier`,
  `TestRememberForceWithNoMatchBehavesLikeOrdinaryCreate`

## Notes

No self-rebase was needed (criterion 6 passed directly — branch already a
clean fast-forward off current `origin/main`). Deploying from an isolated
`deploy/crn-im3h-gate` branch cut at commit D, never from `builder/crn-qxj3`
directly.
