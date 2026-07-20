# Release Gate: shadowmap-status-shadow-detection

**Bead:** crn-eat (deploy) — source review crn-dgh — design crn-4bd.2
**Commit:** `6f96e9f8eba8c7e47da2a7748132220e4980e914`, isolated deploy branch `deploy/crn-eat-gate` cut directly from this SHA
**Date:** 2026-07-20

## Note on branch provenance (stacked, not cherry-picked)

Unlike `status-identity-guard`'s gate (which cherry-picked in isolation),
this deploy branches directly at the reviewed SHA and therefore stacks on
top of three already-separately-gated commits: `beaaee1` (topic-key
specificity precedence, deploy bead crn-1qb → PR #3, open/mergeable),
`284ea1d` (identity guard, deploy bead crn-has → PR #4, open/mergeable),
and `45b2623` (`cairn get <id>`, deploy bead crn-1?? → PR #5,
open/mergeable). This is not avoidable bundling: `cc5bca2` (ShadowMap)
calls `moreSpecific()`, which `beaaee1` defines — `moreSpecific` does not
exist anywhere on `origin/main` yet (confirmed via `git log --all -S`), so
a cherry-pick of just the ShadowMap commits onto fresh `main` would not
compile. Stacking is the correct shape here: this bead's own thematic
contribution is exactly `cc5bca2` + `6f96e9f` (see criterion 7), and the
PR's diff will narrow automatically as PRs #3/#4/#5 land ahead of it,
since all are literal shared ancestor commits, not rebased copies.

## Note on a missing mechanical guard

`scripts/rebase-resolve-lib.sh` (the `resolve_deploy_branch_target` /
`assert_safe_push_target` helpers mandated by this role's process,
built per crn-wya) is not present in this worktree — crn-wya's fix landed
only on gc-management's `pack-author` branch (commit b8a9fbba9) and has
not yet been merged to gc-management `main`, so it was never materialized
here. In its absence, the safety properties it would have enforced were
checked by hand before cutting the branch:

- Target `deploy/crn-eat-gate` is non-empty.
- Does not match the shared-worktree pattern `^gc-[A-Za-z0-9._-]+-[0-9a-f]{12}$`.
- Is not `fix/crn-dgh-shadowmap-gocognit` or `gc-builder-6ac3e0f3c1f3` (the
  two names the reviewed bead explicitly forbids as push targets).
- Did not already exist locally or on `origin` before creation (verified
  via `git show-ref` / `git ls-remote --heads origin`), so pushing it can't
  clobber any existing ref — the failure mode crn-wya guards against is
  structurally impossible for a brand-new branch name.

Flagged to mayor separately as a standing gap (see mail below); not a gate
criterion, since it only affects deployer-side branch hygiene, not the
correctness of what's being deployed.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree 6f96e9f origin/main` produced a tree with exit 0 (no conflict markers). Only `origin/main`'s tip commit (`5edc21d`, docs-only hero-logo/badges, content-equivalent to this branch's own `78f450c`) separates the two histories, and it merges without conflict. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-dgh notes, focused re-review dated 2026-07-20 ~11:37 UTC: "## Verdict: PASS" against exactly `6f96e9f8eba8c7e47da2a7748132220e4980e914`. Finding #1 (blocking, gocognit complexity 28>25) confirmed resolved; identity guard reconfirmed intact live. |
| 2 | Acceptance criteria met | PASS | FR-1..FR-7 (crn-4bd.2 design doc) independently verified by builder and twice by reviewer across the review history. I independently re-ran the live smoke tests myself on the deploy branch rather than trusting notes alone (see criterion 3) and reconfirmed: `ShadowMap` has exactly one call site (`cmd/commands.go`'s `statusCmd`, guardrail §13 — grep shows no calls from `map`/`prime`), and both `--identity` flag and `$CAIRN_IDENTITY` env are still correctly rejected on `status` at the built-binary level. |
| 3 | Tests pass | PASS | Re-ran fresh on `deploy/crn-eat-gate` (not reused from notes): `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `golangci-lint run ./...` → 0 issues (was: `gocognit 28>25` on `ShadowMap`, resolved by this SHA's `bestShadower` extraction), `go test ./... -cover` → all pass, `cmd` 17.0% / `internal/cairn` 82.3% coverage — matches reviewer's independently-reported numbers exactly. |
| 4 | No high-severity findings open | PASS | Finding #1 (the only blocking finding) resolved by this commit and reconfirmed via lint above. Finding #2 (non-blocking: CLI-wiring test coverage) tracked separately as crn-caz (closed) — its coverage fix lives on an unmerged validator branch and is explicitly out of this bead's scope per the reviewer's own note ("No action needed from this review"). A non-blocking output-sanitization observation was explicitly scoped out as a suggested follow-up, not a defect. |
| 5 | Final branch clean | PASS | `git status` clean on `deploy/crn-eat-gate` immediately after checkout at the exact reviewed SHA — no modified/staged files; only pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) untracked. |
| 7 | Single feature theme | PASS | This bead's own reviewed scope is exactly two commits: `cc5bca2` (`cmd/commands.go`, `internal/cairn/entry.go`, `internal/cairn/entry_test.go` — add `ShadowMap`/`scopeSuperset`, wire into `status`) and `6f96e9f` (`internal/cairn/entry.go` only — mechanical `bestShadower` extraction, zero behavior change). One subsystem: shadow-detection annotation on `cairn status` output. The three stacked prerequisite commits belong to other beads with their own PRs (see provenance note above) — carried in history because ShadowMap depends on `beaaee1`'s `moreSpecific()`, not because independent themes were bundled into this bead. |

## Verdict: PASS — proceeding to PR.
