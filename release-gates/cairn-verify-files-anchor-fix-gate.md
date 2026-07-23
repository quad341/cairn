# Release Gate: cairn-verify-files-anchor-fix

**Bead:** crn-8xj7 (deploy) — source review crn-44l — implementation crn-6az.8.2
**Commit:** `131c2b09a0edeb936e53afcda247c084178e6f13`, cut onto `deploy/crn-8xj7-gate` off `origin/main`
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Re-fetched `origin/main` immediately before evaluation (tip `407db36`, several commits past the commit's fork point `c86480c`). Ran `git merge-tree --write-tree origin/main 131c2b0` against this current tip: single clean tree hash (`4d13400`), exit 0, no conflict markers. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-44l closed (reason: pass) with "VERDICT: pass" from cairn/reviewer, independently verified in an isolated worktree per its own notes (build/vet/gofmt/lint/test all green, 100% statement coverage on changed lines, no OWASP/security concerns). Re-confirmed myself in this session directly on commit `131c2b0`, not trusting the recorded evidence secondhand. |
| 2 | Acceptance criteria met | PASS | crn-6az.8.2's AC: a files-anchor path untracked at HEAD makes `ComputeFingerprint` return `""` and `Check` report `Unknown`, not `Fresh`. Confirmed by direct diff inspection: `freshness.go`'s `ComputeFingerprint` now returns `""` as soon as `objectHash` yields the `"?"` sentinel for any configured path (previously the sentinel was hashed like any other value, producing a stable-but-meaningless fingerprint). `sweep.go`'s enrichment gate (`status != Unknown`) removed since `Check` now already reports `Unknown` on its own; the enrichment block still runs unconditionally for files-anchors to name the specific untracked path in Detail. Scoping decision (type=commit anchors left out of this fix) reasoned in both the implementation and review notes and tracked via non-blocking follow-up crn-fqe (P3) — not a gap in this bead's own AC. |
| 3 | Tests pass | PASS | On commit `131c2b0`, run directly in this session: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `golangci-lint run ./...` 0 issues (shared cache cleaned first — known cross-worktree staleness issue), `go test ./... -race -count=1` — all packages (`cmd`, `internal/cairn`, `internal/critic`) ok, zero regressions. |
| 4 | No high-severity review findings open | PASS | crn-44l's review raised no blocking findings; all 6 review checklist points satisfied (AC coverage, objectHash/expand signature stability, sweep.go gate-removal correctness by construction, non-vacuous test rewrites, scoping call, full gate green). One non-blocking P3 follow-up filed (crn-fqe: extend the same untracked-anchor protection to type=commit anchors) — explicitly scoped out of this fix, tracked separately. |
| 5 | Final branch clean | PASS | `git status` clean on `deploy/crn-8xj7-gate` (no modified/staged tracked files); only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` merge-base touches exactly 4 files: `internal/cairn/freshness.go`, `internal/cairn/freshness_test.go`, `internal/cairn/sweep.go`, `internal/cairn/sweep_test.go` (63 insertions, 37 deletions) — one subsystem (freshness-check fingerprinting) plus its directly-coupled Sweep collateral fix, one coherent root-cause fix. |

## Verdict: PASS — proceeding to PR.

## Note on prior blocker

This bead was previously blocked with a recorded PASS but no push: `scripts/rebase-resolve-lib.sh` was absent rig-wide at the time of the original evaluation (see bead notes and mayor escalation msg `gm-wisp-0qrv2gi`). That script has since landed on `origin/main` (commit `128c214`, PR #18) and is confirmed present and sourceable. The gate above is a full independent re-evaluation, not a replay of the earlier PASS — the original gate markdown was never committed and no longer exists in any live worktree.
