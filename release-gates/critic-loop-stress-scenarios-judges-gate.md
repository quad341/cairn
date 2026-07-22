# Release Gate: critic-loop-stress-scenarios-judges

**Bead:** crn-q84 (deploy) — source review crn-655 — implementation crn-rqf.1
**Commit:** `c07d15a251e5e0af61373cc3b3b7b1f3c8a00c02`, cut onto `deploy/crn-q84-gate` off `origin/main`
**Date:** 2026-07-22

## Note on branch-safety tooling

`scripts/rebase-resolve-lib.sh` (`resolve_deploy_branch_target` +
`assert_safe_push_target`), which the deployer playbook directs to source for
step 5's branch derivation, does not exist anywhere in this repository or its
history (confirmed via `git log --all -- scripts/rebase-resolve-lib.sh` and a
repo-wide grep for `assert_safe_push_target`/`resolve_deploy_branch_target`
under any name) — it appears to belong to an unrelated project's copy of this
playbook and was never ported into cairn. In its absence, the underlying
safety invariant was verified by hand instead of skipped: `deploy/crn-q84-gate`
does not collide with any existing local or remote ref (`git rev-parse
--verify` and `git ls-remote origin` both empty), it does not match the
shared-worktree naming pattern (`gc-<agent>-<12hex>`, e.g.
`gc-builder-6ac3e0f3c1f3`) that caused the crn-wya / cairn#3 incident, and it
was created with an explicit `git checkout -b deploy/crn-q84-gate
c07d15a...` against the exact reviewed SHA rather than any mutable branch
tip. This mirrors the naming precedent already established by five prior
gates in this repo (`deploy/crn-1qb-gate`, `deploy/crn-4do-gate`,
`deploy/crn-eat-gate`, `deploy/crn-ga1-gate`, `deploy/crn-psq-gate`). Flagging
here for visibility; porting the actual library script into cairn would
remove the need for this manual check on future deploys.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `c07d15a`'s sole parent is `6bfdc6a`, exactly `origin/main`'s current tip (re-fetched immediately before evaluation: `git merge-base --is-ancestor origin/main c07d15a` → true). Single commit ahead, no self-rebase needed. |
| 1 | Review PASS present | PASS | crn-655 closed (reason: pass) with explicit "VERDICT: PASS" from cairn/reviewer covering all 5 dimensions (recall, scope-precedence, freshness, perf, ergonomics), including independent hand-verification of the highest-stakes claim (scope-precedence's incomparable-scopes edge case, traced against `entry.go` directly rather than accepted from subagent report). |
| 2 | Acceptance criteria met | PASS | crn-rqf.1 AC: a scenario + mechanical judge for all 5 dimensions, none subjective/LLM-vibes, each with a concrete expected-value/threshold/shape check, executed against the real store (SQLite-index/live-binary end-to-end run explicitly deferred to crn-di7 per the AC's own note — unit-level exercise against `internal/cairn` was in scope here), deterministic across repeated runs. Independently re-verified in this session: `internal/critic/critic.go`'s package doc comment states intent explicitly ("a deterministic pass/fail/degraded verdict rather than an LLM's subjective read of the output"); `grep -rn "mock\|Mock\|stub\|Stub"` across `internal/critic/*.go` and `cmd/ergonomics_scenario*.go` returns only a comment stating *"and no mocks"* — no actual mock/stub usage found, confirming real-store exercise. |
| 3 | Tests pass | PASS | On `c07d15a` (detached checkout): `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty. `go test ./... -race -count=1` run twice independently: both runs green across all 3 packages (`cmd`, `internal/cairn`, `internal/critic`), 86 top-level / 0 FAIL each run — matches the reviewer's independently reported 86/138 PASS, 0 FAIL exactly. `golangci-lint run ./...` — first pass surfaced a stale-cache artifact referencing a since-removed scratch worktree path from an unrelated concurrent session (same class of issue the reviewer flagged); `golangci-lint cache clean && golangci-lint run ./...` — 0 issues. |
| 4 | No high-severity findings open | PASS | crn-655 notes record zero HIGH findings; security section explicitly clean (no new I/O/trust-boundary surface, `exec.CommandContext` with arg slices only, no shell interpolation, `crypto/rand` for nonces, standard 0600/0700 file perms unchanged). Two documented suggestions are explicitly "non-blocking, optional" / coverage nits, not defects. |
| 5 | Final branch is clean | PASS | `git status --short` at `c07d15a`: clean except pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) that predates this deploy and isn't part of this commit. |
| 7 | Single feature theme | PASS | All 13 changed files are exactly `internal/critic/{critic,fixture,recall,scope_precedence,freshness,perf}.go` (+ matching `_test.go`) and `cmd/ergonomics_scenario{,_test}.go` — one cohesive feature (mechanical stress scenarios + judges for all 5 critic-loop dimensions). The ergonomics scenario's placement under `cmd/` rather than `internal/critic/` is explained by a real structural constraint (needs unexported `cobra` `rootCmd` state to drive real `RunE` invocations in-process), not an unrelated bundled change — it is still the ergonomics dimension of the same feature. No independent themes present; removing this commit leaves `main` fully working with no partial feature. |

## Verdict: PASS — proceeding to PR.
