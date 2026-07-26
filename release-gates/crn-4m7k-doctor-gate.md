# Release Gate: crn-4m7k — `cairn doctor` (aggregation, tolerant iteration, explain modes)

**Bead:** crn-4m7k (deploy) / crn-52z7 (implementation)
**Evaluated:** 2026-07-26
**Deploy source (D):** `836e5f6d263f3193e015b86ddad6bb7b1dab2a37` (builder/crn-52z7, base origin/main@947e7ff)

**Result: PASS — proceeding to isolated deploy branch push + PR (#60)**

## Background

This is crn-52z7's third reviewer cycle, following two prior deployer bounces:

1. Original PASS at `e12da46` (this bead's DESCRIPTION field still cites this SHA — stale,
   never updated across subsequent cycles; superseded here by the NOTES history below).
2. Deployer gate-6 FAIL (`origin/main` advanced via PR #58, `internal/cairn/entry.go` conflict) →
   builder rebased → fresh reviewer PASS at `749b789`.
3. Deployer HOLD (`origin/main` advanced again via PR #59 / crn-4x9g's squash-merge, creating a
   duplicate-symbol collision in `entry.go` between crn-52z7's and crn-4x9g's independent
   `ShadowReason`/`shadowReason`/`moreSpecificReason`/`bestShadowerExplain` additions — the risk
   crn-4x9g's own gate doc had flagged as informational back when it was still hypothetical) →
   builder rebased again, adopting crn-4x9g's now-canonical ctx+obslog-threaded `ShadowMap`/
   `visibleFrom` and layering crn-52z7's doctor-specific consumers on top additively → fresh
   reviewer PASS at `836e5f6`, this cycle's SHA.

## Criterion 6 — Branch diverges cleanly from main: PASS

Re-fetched immediately before this evaluation: `origin/main` is still `947e7ff4b19e1c311ebb543ca1d0e698893e7ec3`
(unchanged since the reviewer's own check — neither `crn-od2x.3` nor `crn-lzn4.1.1`, this session's
other two candidates, has merged). `git merge-base --is-ancestor origin/main 836e5f6` → true (clean
ancestor, zero divergence). `git merge-tree --write-tree origin/main 836e5f6` produced a single clean
tree hash with no conflict markers. No rebase needed.

## Criterion 1 — Review PASS present, SHA match: PASS

Reviewer's fresh SHA-pinned PASS (per the `crn-fie5` mandate — the prior PASS at `749b789` does not
carry forward) cites `836e5f6d263f3193e015b86ddad6bb7b1dab2a37` exactly. D == R.

## Criterion 2 — Acceptance criteria met: PASS

Reviewer's PASS walks crn-52z7's 7 deliverables individually against the final diff, with direct
evidence (not just assertion): exit-code contract (`cmd/doctor.go`'s sole `os.Exit` call site),
all 7 named Finding categories present and distinct, read-only guarantee (grepped for
`Reindex(`/`Sweep(`/`Dedup(`/write-back calls in `doctor.go` — zero matches, only the dry `*Entries`
variants are called), NFR-4 authorization-language check (grepped for denied/unauthorized/forbidden/
permission — zero matches). Scope-isolated diff (`git diff origin/main 836e5f6 --stat`, independently
re-run below) matches the original 7-deliverable description with no drift.

## Criterion 3 — Tests pass: PASS (independently re-verified)

Ran from a fresh scratch worktree at `836e5f6` (not the reviewer's or builder's worktree, and not
trusting self-reported numbers):

```
go build ./...                                    build_exit=0
go vet ./...                                       vet_exit=0
golangci-lint cache clean && golangci-lint run ./... lint_exit=0 ("0 issues.")
go test ./... -race -count=1 -shuffle=on           test_exit=0
  ok  github.com/quad341/cairn/cmd            6.948s
  ok  github.com/quad341/cairn/formulas       1.035s
  ok  github.com/quad341/cairn/internal/cairn 10.741s
  ok  github.com/quad341/cairn/internal/critic 5.498s
  ok  github.com/quad341/cairn/internal/obslog 1.014s
  ok  github.com/quad341/cairn/scripts        2.041s
```

All 6 packages green, matching the reviewer's own independently-run numbers.

## Criterion 4 — No open blocking findings: PASS

`bd show crn-52z7`: status closed, labels `needs-review`/`ready-to-build` (stale historical labels,
not currently blocking). No HIGH-severity-labeled open issues against crn-52z7 or crn-4m7k. The five
`sling-crn-4m7k` beads (crn-1pts, crn-2161, crn-f1p1, crn-f7r4, crn-wjbu) are `gc sling` routing/convoy
artifacts from this bead's three builder↔reviewer↔deployer hops, not findings.

## Criterion 5 — Final branch clean: PASS

`git status --porcelain` empty at `836e5f6` in the gate worktree.

## Criterion 7 — Single feature theme: PASS

`git diff origin/main 836e5f6 --stat`: 12 files, 1559(+)/4(-) — `cmd/doctor.go` + test,
`internal/cairn/{dedup,doctor,entry,index,sweep}.go` + tests. Matches crn-52z7's original
7-deliverable scope exactly (per its bead description). No unrelated changes riding along across
the three rebase cycles.

## Duplicate-symbol collision: resolved, re-confirmed independently

The `entry.go` collision with crn-4x9g (flagged informationally in crn-4x9g's own gate doc, then as
a live PRE-MERGE CHECK blocker on this bead across its 2nd and 3rd cycles) is resolved: anchored
grep (`^func bestShadower(`, `^func bestShadowerExplain(`, etc.) at `836e5f6` confirms exactly one
definition of each symbol; `ShadowMap`/`visibleFrom` both carry `ctx context.Context` as their first
parameter, confirming crn-4x9g's canonical ctx+obslog-threaded version was adopted (not left as two
parallel implementations). crn-52z7's own additions (`IterEntriesTolerant`, `walkEntries`,
`ReindexEntries`/`SweepEntries`/`DedupEntries`, `IndexStale`, `Diagnose`/`ExplainEntry`/
`ExplainForIdentity`) are all present and correctly wired — confirmed by the clean build above (Go
would not compile a signature mismatch).

## Action

**PASS.** Cut isolated branch `deploy/crn-4m7k-gate` from `836e5f6` via `resolve_deploy_branch_target`
(safety-checked against the shared-worktree-branch signature via `assert_safe_push_target` before both
the branch push and `gh pr create --head`, per the `crn-wya` incident precedent). Opened
[PR #60](https://github.com/quad341/cairn/pull/60). At PR-creation time, `mergeStateStatus` was
`BLOCKED` (CI checks still `IN_PROGRESS`, not yet `CLEAN`) — per the mayor's confirmed SOP, this is
the arm-auto-merge path rather than the escalate-for-direct-merge path. Auto-merge armed
(`gh pr merge 60 --auto --squash`), confirmed via `autoMergeRequest` on the PR (method `SQUASH`,
enabled). No further action needed unless CI fails or the PR falls out of `MERGEABLE`.
