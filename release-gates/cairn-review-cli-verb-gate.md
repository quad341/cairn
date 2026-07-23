# Release Gate: cairn-review-cli-verb

**Bead:** crn-j1uh (deploy) — source review crn-8emp — source feature crn-ffm
**Commit:** `ead7b005a4146fcb3fee168a6b00e4409ae44013`
**Date:** 2026-07-23

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git fetch origin` + `git merge-tree --write-tree origin/main ead7b00` — exit 0, single resolved tree, zero conflict markers. Merge base `c86480c` is 16 commits behind current `origin/main` (`a17edd2`), but the deploy commits (2, on top of the base) only add new files (`cmd/review.go`, `internal/cairn/review.go`, + tests) that don't exist on main, so no self-rebase was needed. |
| 1 | Review PASS present | PASS | crn-8emp notes: "RE-REVIEW VERDICT: PASS" by cairn/reviewer (session reviewer-gm-dgv29), after one request-changes round-trip. Re-review independently re-ran the full gate and confirmed the fix commit in isolation. |
| 2 | Acceptance criteria met | PASS | All 11 ACs in crn-8emp confirmed passing, each with a named test: tier derivation (list), diff+field rendering (show), surgical patch (merge), `ValidatePathSegment` reuse now covering topic_key/scope **and** anchor_type, `--no-ff` commit shape, branch deletion, reindex, NFR-2 no-partial-state-on-failure, secret-pattern guard with explicit-only override, both flagged judgment calls (curate/merge split; secret-scan scope) confirmed, full gate green. |
| 3 | Tests pass | PASS | Independently re-run on `ead7b00` (not just trusting builder/reviewer reports): `gofmt -l .` clean, `go vet ./...` clean, `go build -o build/cairn .` clean, `go test ./... -race -count=1` — all packages ok (`cmd`, `internal/cairn`, `internal/critic`), `golangci-lint run ./...` (cache cleaned first per the shared-cache gotcha) — 0 issues. |
| 4 | No high-severity findings open | PASS | One HIGH finding raised in original review (`opts.AnchorType` never validated via `ValidatePathSegment`, unlike `TopicKey`/`Scope` — newline-injection payload corrupts the default branch's TOML frontmatter and breaks store-wide reads via `IterEntries` until hand-fixed). Fixed in `ead7b00` (same placement pattern, before any mutation) with a pinning regression test (`TestMergeReviewBranchRejectsInvalidAnchorType`), independently re-verified by the re-review: correct placement, single call path (no bypass), repro payload now rejected, no regressions on valid-usage tests. Zero HIGH findings remain open. |
| 5 | Final branch clean | PASS | `git status --short` clean on the deploy source commit — no modified/staged files; only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Deploy commits touch `cmd/review.go`, `cmd/review_test.go`, `internal/cairn/review.go`, `internal/cairn/review_test.go` only — one subsystem (the `cairn review {list,show,merge}` CLI verb and its input validation). No unrelated changes bundled. |

## Verdict: PASS — proceeding to PR.
