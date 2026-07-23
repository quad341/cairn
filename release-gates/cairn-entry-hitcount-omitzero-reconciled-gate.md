# Release Gate: cairn-entry-hitcount-omitzero-fix (reconciled)

**Bead:** crn-kk8l (deploy) — supersedes crn-wcv7 (closed) — source review crn-9hw7 — reconciliation crn-6az.5.2
**Commit:** `a59f3c1d6b96afb1e6ab87ff02cfa42d8a82ee08`, evaluated in isolated detached-HEAD worktree
**Date:** 2026-07-22

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git fetch origin main` → tip `3070f4fa8396a3e30e0606d8656790dd1678f1bf`. This commit's parent (`git rev-parse a59f3c1^`) is exactly that SHA — zero divergence, no rebase needed. |
| 1 | Review PASS present | PASS | crn-wcv7's notes record "RE-REVIEW VERDICT (post-reconciliation): PASS" from cairn/reviewer, independently re-run in the reviewer's own worktree (build/vet/gofmt/lint/test-race all clean, mutation-tested). |
| 2 | Acceptance criteria met | PASS | Diff confirmed: `internal/cairn/entry.go` line 52 — `HitCount int \`toml:"hit_count,omitzero"\`` (byte-identical tag fix to the original crn-9hw7-reviewed change). Two new tests in `remember_test.go` (`TestEntryCreateOmitsZeroHitCount`, `TestEntryCreateSerializesNonZeroHitCount`) independently re-run here, both PASS, both assert on raw serialized bytes (not decoded round-trip). Retargeting from `entry_test.go`/`WriteBack()` to `remember_test.go`/`Create()` confirmed correct: read `patchVerification()` directly, confirmed `WriteBack()` post-crn-4dxi only touches `verified_at`/`anchor.fingerprint` and no longer serializes `HitCount` at all. |
| 3 | Tests pass | PASS | On commit `a59f3c1d`, in a fresh isolated worktree: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./... -race -count=1` — all four packages (`cmd`, `internal/cairn`, `internal/critic`, `scripts`) ok. `golangci-lint run ./...` 0 issues (shared cache cleared first). |
| 4 | No high-severity review findings open | PASS | No findings recorded against this bead or its crn-9hw7/crn-wcv7 lineage; n/a security (pure struct-tag + test change). |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on the evaluated commit. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` touches exactly 2 files: `internal/cairn/entry.go` (+1/-1), `internal/cairn/remember_test.go` (+31/-0) — one isolated tag fix plus its (relocated) regression tests. |

## Verdict: PASS

Superseded artifact: PR #30 (`deploy/crn-wcv7-gate`, based on the pre-reconciliation commit `6981531`) is DIRTY/CONFLICTING against current `origin/main` and is being closed as part of this deploy, per the reviewer's explicit instruction on crn-wcv7/crn-kk8l — not reused, not rebased.

Proceeding: cut `deploy/crn-kk8l-gate` from `a59f3c1d` via `resolve_deploy_branch_target`, push, open PR, arm `gh pr merge --auto`.
