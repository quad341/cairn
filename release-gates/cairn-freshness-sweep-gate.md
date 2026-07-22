# Release Gate: cairn-freshness-sweep

**Bead:** crn-3bs — needs-deploy: cairn freshness sweep + drift bead-filing
plan (crn-0yv.2), from review crn-5dg (source bead crn-0yv.2, parent crn-0yv)
**Commit set:** `3b980fde4eb4621d5d055142aca8744e44f132d9` (feat: read-only
freshness sweep + drift bead-filing plan), `462488de3ab68e1697d62ec86c3bcd845588a237`
(fix: untrackedPaths matches objectHash's HEAD resolution, crn-8x4) —
cherry-picked in this order onto a fresh branch cut from `origin/main`
**Branch:** `deploy/crn-3bs-gate`, HEAD `9da08e7`
**Date:** 2026-07-22

## Note on a missing mechanical guard

`scripts/rebase-resolve-lib.sh` (the `resolve_deploy_branch_target` /
`assert_safe_push_target` helpers this role's process calls for) is not
present in this repo — consistent with the same gap already documented in
`release-gates/ci-merge-group-trigger-gate.md` and
`release-gates/shadowmap-status-shadow-detection-gate.md`. Checked by hand
instead, same as those two precedents:

- Target `deploy/crn-3bs-gate` does not match the shared-worktree pattern
  `^gc-[A-Za-z0-9._-]+-[0-9a-f]{12}$`.
- Confirmed via `git ls-remote origin refs/heads/deploy/crn-3bs-gate` (empty
  result) that the name did not already exist locally or on `origin` before
  creation — pushing it cannot clobber any existing ref.

## Note on criterion 6 (branch staleness) and the bounded self-rebase

The bead's recorded commit (`462488d` on `feature/crn-0yv.2`) has `6bfdc6a`
as its merge-base with `origin/main`, but `origin/main` has since advanced to
`65374e1` (2 commits: `#15` critic-loop stress scenarios, `#16` cairn
remember shared-tier review). This is a criterion-6 FAIL on the recorded
branch as-is.

Diagnosed as **provably trivial** before attempting anything: `git diff
--stat` between the merge-base and each side shows **zero file overlap** —
origin/main's new commits touch `cmd/ergonomics_scenario.go`, `cmd/remember.go`,
`cmd/reviewer.go`, `internal/cairn/remember.go`, `internal/critic/**`, and
`release-gates/**`; this bead's commits touch only `cmd/sweep.go`,
`internal/cairn/sweep.go`, `internal/cairn/sweep_test.go`,
`internal/cairn/freshness_test.go`, and `docs/plans/cairn-librarian-freshness-drift-beads.md`.
No shared files, so no textual conflict is possible.

`scripts/rebase-resolve-lib.sh`'s `attempt_bounded_self_rebase` (the sanctioned
mechanism for resolving this) does not exist in this repo, so per Guardrails
this is treated as a setup failure for the *in-place rebase* path specifically.
Rather than hand-rolling that function inline (explicitly disallowed — "sourced
fresh, not re-implemented inline"), this gate follows the same precedent as
`shadowmap-status-shadow-detection-gate.md`: cut a fresh branch directly from
current `origin/main` and cherry-pick the bead's own commits in order, which
achieves an equivalent result (this bead's diff, replayed cleanly on top of
current main) without inventing a rebase mechanism. Both cherry-picks applied
with **zero conflicts**, confirming the triviality diagnosis.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | See note above — cut fresh from `origin/main` (`65374e1`), both commits (`3b980fd`, `462488d`) cherry-picked with zero conflicts, no manual resolution needed. |
| 1 | Review PASS present | PASS | crn-5dg (closed, reason=pass): first-pass reviewed `3b980fd`, found one blocking finding (crn-8x4, corroborating validator crn-0yv.4's independent discovery); builder fixed it in `462488d`; re-review verdict **PASS**, independently confirmed in an isolated detached worktree via both a direct source read AND an adversarial check (temporarily reinstated the pre-fix code and confirmed the new regression test fails against it, then passes again with the fix restored — proving the test is a genuine non-tautological repro). All 9 of the review's own checklist points confirmed clean. |
| 2 | Acceptance criteria met | PASS | crn-0yv.2's 5 ACs, each independently traced by the reviewer (crn-5dg) and spot-checked again here: (1) no-override for tracked anchors — hand-traced, genuine drift never masked; (2) untracked/staged-but-uncommitted anchor path correctly classified as unknown, not fabricated-Fresh (crn-8x4 fix, verified adversarially); (3) `Sweep()` never writes — confirmed via direct source read, no `WriteBack`/`os.WriteFile` call anywhere in `sweep.go`/`cmd/sweep.go`; (4) tier scoping (global/rig/role included, agent/ excluded) — confirmed structurally sound against `IterEntries`; (5) bead-filing dedup design doc reviewed and judgment call signed off. Re-ran the specific named tests myself on the assembled deploy branch (not just trusting the review): `TestSweepMatchesCheckForHealthyAnchors`, `TestSweepMatchesCheckForDriftedAnchor`, `TestSweepOverridesUntrackedAnchorToUnknown`, `TestSweepOverridesStagedUncommittedAnchorToUnknown`, `TestSweepTierScoping`, `TestSweepNeverWrites` — all PASS. |
| 3 | Tests pass | PASS | Fresh run on `deploy/crn-3bs-gate` (HEAD `9da08e7`): `go build ./...` clean; `go vet ./...` clean; `gofmt -l .` empty; `go test ./... -race -count=1 -cover` → all green, `cmd` 67.3%, `internal/cairn` 79.0%, `internal/critic` 75.4%, no regressions; `golangci-lint run ./...` → 0 issues. |
| 4 | No high-severity findings open | PASS | The sole blocking finding from crn-5dg's first pass (crn-8x4) is closed/fixed and independently re-verified above. Zero open HIGH findings against this commit set. |
| 5 | Final branch is clean | PASS | `git status` clean immediately after both cherry-picks — no modified/staged files; only pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) untracked. |
| 7 | Single feature theme | PASS | Two commits, one subsystem (the freshness re-verification sweep step for `mol-cairn-librarian`): `3b980fd` adds `cmd/sweep.go` + `internal/cairn/sweep.go`/`sweep_test.go` + a design doc for a *downstream* step (crn-0yv.5) that consumes this one's output; `462488d` is a direct, same-subsystem bugfix (crn-8x4) discovered during that same feature's review. The design doc is documentation, not a second executable feature — nothing in this commit set is independently shippable or removable without the other. |

## Verdict: PASS

Proceeding to PR from `deploy/crn-3bs-gate`. Per this deployer's narrowly-scoped
auto-merge exception, `gh pr merge --auto` will be armed on the PR opened from
this branch (this same work-loop run) so main's merge queue merges it once
required checks pass. Per `crn-6yc`/`crn-54c` (open, tracked separately): main
has neither a classic merge queue nor a ruleset configured yet, so arming with
no strategy flag is expected to fail verification — if so, this is a genuine
arm-failed blocker per the deployer SOP and will be escalated to mayor
accordingly, not papered over with a hand-picked merge strategy.
