# Release Gate: Anchors fingerprint the CANONICAL CHECKOUT's HEAD, which is 57 commits stale — every anchor reports false 'fresh' now and will mass-flip to 'stale' on update

- Bead: crn-k7mq2 (deploy) / crn-5hvjv (review, PASS) / builder/crn-uztp (provenance branch, not a push target)
- Reviewer-cited commit (R): `35f1422bc96c83a3079d9f7dbcaf8eb4d2d9b55d`
- Final deployed commit (D): `35f1422bc96c83a3079d9f7dbcaf8eb4d2d9b55d` (identical to R — origin/main was already an ancestor, no self-rebase required)
- Evaluated: 2026-08-07, against origin/main@7614b3e (#81, "Replace payload_tokens byte/4 estimate with calibrated measurement")
- Root bug: crn-uztp (P1, CLOSED) — files-anchor fingerprints were derived from the anchor repo's own checked-out HEAD (mutable, incidental local state) instead of origin/main, producing false "fresh" verdicts today and a pending mass false-stale flip fleet-wide the moment that checkout moved
- Follow-up (non-blocking): crn-6cwxb (P3, OPEN, docs) — `docs/DESIGN.md` and `docs/knowledge-lifecycle.md` still describe the old HEAD-only behavior; explicitly excluded from crn-uztp's `exit_contract` (code+tests only) and filed separately by the reviewer
- Operational follow-up (explicitly out of scope, flagged by the bug itself): updating the shared `/home/jaword/projects/cairn` anchor-repo checkout to origin/main and re-running `cairn verify` fleet-wide belongs to whoever owns that clone / mayor, not this code change. Expect a one-time mass fresh→stale flip on deploy — per the bug's own MAYOR NOTE this is the expected artifact of the fix, not a regression to react to.

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` run immediately before this evaluation. origin/main tip is `7614b3e`. `git merge-base --is-ancestor origin/main 35f1422bc96c83a3079d9f7dbcaf8eb4d2d9b55d` confirms origin/main is a direct ancestor of R/D — D sits exactly 2 commits ahead (the tdd_red + tdd_green pair), 0 behind. No bounded self-rebase needed. **PASS.**

## 1. Exact SHA match (D == R)

R = `35f1422bc96c83a3079d9f7dbcaf8eb4d2d9b55d`, recorded as both the `commit` and `tdd_green` metadata fields on crn-5hvjv (review, verdict PASS) and matching crn-k7mq2's own deploy bead exactly. The isolated deploy branch (`deploy/crn-k7mq2-gate`) was cut directly from R via `resolve_deploy_branch_target`. No rebase was applied (criterion 6 was already structurally satisfied), so D == R literally, exact match. **PASS.**

## 2. Acceptance criteria

Read crn-uztp's `exit_contract` directly rather than trusting the reviewer's summary alone, then independently read the full diff (`git show 35f1422b`) to confirm each bullet in code:

1. **"objectHash no longer derive a files-anchor fingerprint solely from the anchor repo's checked-out HEAD."** Confirmed: `objectHash` (`internal/cairn/freshness.go`) now attempts `origin/main:path` before ever consulting `HEAD:path`. Met.
2. **"Prefer origin/main:path when it resolves; fall back to HEAD:path only when origin/main doesn't resolve."** Confirmed verbatim in the diff: `git(ctx, repo, "rev-parse", "origin/main:"+path)` first; on a genuine git error, return immediately; on a confirmed `ok`, return immediately; only fall through to the original `git(ctx, repo, "rev-parse", "HEAD:"+path)` call on a confirmed negative from origin/main. Met.
3. **"Fingerprint for a path must be STABLE across changes to the anchor repo's local checkout/working-tree HEAD... as long as origin/main itself is unchanged."** This is the core defect and is behavioral, not just structural — confirmed via test, not just reading: `TestFileAnchorFingerprintUnaffectedByLocalHeadDrift` exercises exactly this (checks out a different/detached local HEAD, asserts the fingerprint is unchanged as long as origin/main is unchanged) and passes independently (see criterion 3 below). Met.
4. **"New tests (RED then GREEN) cover (a) fingerprint sourced from origin/main is unaffected by local HEAD drift; (b) fallback to HEAD when no origin remote exists."** Confirmed both cases exist in the tree at D (`git show 35f1422b:internal/cairn/freshness_test.go`) and independently re-run PASS: `TestFileAnchorFingerprintUnaffectedByLocalHeadDrift` (the (a) case), `TestFileAnchorFingerprintTracksOriginMainDrift` (companion positive case — drift IS reflected when origin/main itself moves), `TestFileAnchorNoOriginRemoteFallsBackToHead` (the (b) case, a regression pin for repos without an origin, e.g. test fixtures). Met.

Resolved a scope question along the way: `git show`'s single-commit diff for D (the tdd_green commit alone) touches only `freshness.go`, which could misread as "the test file wasn't part of this SHA." Confirmed via `git log --oneline origin/main..D` that D's own history is exactly two commits — `d490269` (tdd_red, adds the three tests) then `35f1422` (tdd_green, adds the fix) — with `d490269` a direct-parent ancestor of `35f1422`, so D's checked-out tree carries both files in full (`git diff --numstat origin/main..D`: `freshness.go` +19/-4, `freshness_test.go` +125/-0 — matches the reviewer's cited stats exactly). Not a scope gap.

All 4 `exit_contract` bullets independently confirmed against code and passing tests. **PASS.**

## 3. Tests

Canonical command per `Makefile`'s `test:` target and `.github/workflows/ci.yml`'s `build-test` job: `go test ./... -race -count=1`. Ran independently on D, in an isolated detached scratch worktree (kept separate from the deploy branch's own worktree so nothing was disturbed pre-cut):

- `go test ./... -race -count=1` — all 7 packages (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`) report `ok`, exit 0. No flake this run, including `internal/critic` (previously flagged flaky and unrelated to this diff, tracked separately as crn-9k30).
- Diff-owned tests in `internal/cairn/freshness_test.go`, re-run individually by name (`-run`, `-v`): `TestFileAnchorFingerprintUnaffectedByLocalHeadDrift` — PASS; `TestFileAnchorFingerprintTracksOriginMainDrift` — PASS; `TestFileAnchorNoOriginRemoteFallsBackToHead` — PASS. Matches crn-5hvjv's reviewer report by name and outcome exactly (reviewer's `tdd_green` note: "go test ./internal/cairn/... clean: 4 consecutive full-package runs (-count=1), all pass including the new RED tests").

**PASS.**

## 4. No open blocking findings

Independent bd search for finding-type beads referencing crn-uztp/crn-5hvjv/crn-k7mq2: one open item, `crn-6cwxb` (P3, OPEN, docs-only — two doc files still describe the pre-fix HEAD-only behavior). Explicitly excluded from crn-uztp's `exit_contract` (code+tests only) and explicitly filed as a non-blocking follow-up by the reviewer, not a finding against this diff's correctness or safety. No HIGH-severity findings of any kind, open or otherwise. The diff itself is a pure fingerprint-source-precedence change to an existing internal helper (`objectHash`) — no new I/O surface, no new dependency (`go.mod`/`go.sum` untouched), no new external input parsing; the git subprocess invocation pattern is unchanged (same `git(ctx, repo, "rev-parse", ...)` call shape as the pre-existing HEAD path, now also called against `origin/main:`). Reviewer (crn-5hvjv) recorded: `go build ./...` clean, `go vet ./internal/cairn/...` clean, `gofmt -l` clean, `golangci-lint run ./internal/cairn/...` 0 issues. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty on `deploy/crn-k7mq2-gate` at D, confirmed immediately after cutting the branch. **PASS.**

## 7. Single coherent theme

Exactly 2 commits ahead of origin/main (`d490269` tdd_red, `35f1422` tdd_green), touching exactly 2 files total: `internal/cairn/freshness.go` (+19/-4) and `internal/cairn/freshness_test.go` (+125/-0, new tests only). Every hunk serves the same change: making `objectHash` prefer `origin/main` over the anchor repo's local HEAD. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
