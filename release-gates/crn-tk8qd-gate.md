# Release Gate: crn-tk8qd

**Bead:** crn-tk8qd (deploy) — source: crn-kpf7m (review), build: crn-pz5l9.1
**Commit:** `4a76402a68196a6ce40e3475610e9e50653710c2`, cut onto `deploy/crn-tk8qd-gate`
**Date:** 2026-08-21

## Background

Splits `internal/cairn`'s `ensureFresh` into a strict variant (unchanged) and a
new `ensureFreshBestEffort`, per architecture bead crn-pz5l9 (design in
crn-pz5l9.1). `ensureFreshBestEffort` swallows `context.DeadlineExceeded` from
the bounded self-heal reindex and serves the (lagging) index instead of
propagating the error — applied at the 6 read-path call sites (`Find`,
`FindCorrection`, `Status`, `Search`, `Recall`, `CullCandidates`).
`EntryByID` (`evict.go`), the sole pre-mutation gate for cull-evict,
promote-mark and anchor-attach, deliberately stays on the strict path.
Separately, `Find`'s `ErrNotFound` force-reindex fallback (previously
unbounded) is now wrapped in the same `selfHealReindexTimeout` used elsewhere,
returning `ErrNotFound` (not `DeadlineExceeded`) on overrun.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-kpf7m (closed, close_reason=pass) notes' `tdd_green:` and `deploy_commit:` both cite `4a76402a68196a6ce40e3475610e9e50653710c2`, byte-identical to crn-tk8qd's own `**Commit:**` field (D = R). Independently re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` — a real commit object, not a transcribed string. |
| 2 | Acceptance criteria met | PASS | Independently re-verified against crn-pz5l9.1's done-when list, not just the reviewer's notes: `grep -n "ensureFreshBestEffort("` finds exactly 6 call sites (entry.go:887,1042,1497; search.go:685; recall.go:68; cull.go:48); `evict.go:19` still calls strict `ensureFresh(` (not the best-effort variant); `entry.go`'s `Find` force-reindex fallback is wrapped in `context.WithTimeout(ctx, selfHealReindexTimeout)` and returns `ErrNotFound` on overrun (entry.go:905-909); all 3 required tests present by exact name in index_test.go (`TestFindReturnsLaggingIndexWhenBoundedSelfHealOverruns`, `TestEntryByIDStillReturnsDeadlineExceededWhenBoundedSelfHealOverruns`, `TestFindMissingIDBoundedUnderOverrunReturnsErrNotFound`). |
| 3 | Tests pass | PASS | `go build ./...` clean, then `go test ./... -race -count=1` on `deploy/crn-tk8qd-gate` (=4a76402a): all 7 packages `ok` (root, cmd, formulas, internal/cairn, internal/critic, internal/obslog, scripts) — zero FAIL, zero SKIP lines in output (skips/fails print even without `-v`). Diff-owned test file is `internal/cairn/index_test.go`; the 3 diff-owned tests above are covered by internal/cairn's clean `ok`, no skip. |
| 4 | No open HIGH findings | PASS | crn-kpf7m: `style_findings: none` (gofmt/vet/golangci-lint all clean), `security_findings: none` (full OWASP walk, all N/A or independently verified — access-control point specifically re-checked: `EntryByID` unchanged, exactly 3 callers). |
| 5 | Clean tree | PASS | `deploy/crn-tk8qd-gate` cut directly from `4a76402a...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty before the gate-file commit. |
| 6 | Clean divergence from main | PASS | `git merge-base --is-ancestor origin/main 4a76402a` → true: `origin/main` is a direct ancestor of the deploy commit (2 commits ahead: RED d971af3f, GREEN 4a76402a). Trivial fast-forward, no self-rebase needed. |
| 7 | Single feature theme | PASS | Single bead, no `rollup-ship` label. All 6 changed files (`cull.go`, `entry.go`, `index.go`, `index_test.go`, `recall.go`, `search.go`) sit under `internal/cairn/` — one coherent change (ensureFresh strict/best-effort split + bounded Find fallback) end to end. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority.** Per this session's role instructions: arm
   `gh pr merge --auto` on the PR opened below (no strategy flag), then verify.
   A declined arm (expected once the PR is already MERGEABLE/CLEAN) routes to
   mayor as a `MERGE-REQUEST` — the deployer never self-merges on quad341/cairn.

   **Observation, not acted on here:** `release-gates/crn-6b1g8-gate.md`
   (previous deploy, 2026-08-20) records that session self-merging its PR
   directly (`gh pr merge --squash`) under a claimed "standing self-merge
   authorization," citing the memory key `cairn-auto-merge-requires-explicit-strategy`
   as authorizing it. This session's role prompt cites the *same* memory key
   as the thing that settled the opposite (self-merge retired 2026-07-28,
   merge authority is mayor's alone, citing crn-54c/crn-6yc). Flagging this
   contradiction to mayor separately rather than adjudicating it here.

2. **Branch target:** `builder/crn-pz5l9.1` is provenance-only per the deploy
   bead's own instruction. `deploy/crn-tk8qd-gate` was cut fresh from the
   exact reviewed SHA via `resolve_deploy_branch_target`; `assert_safe_push_target`
   confirmed the derived name does not match the shared-worktree-branch
   signature.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step ran.
