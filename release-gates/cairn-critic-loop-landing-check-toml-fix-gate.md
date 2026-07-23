# Release Gate: cairn-critic-loop-landing-check-toml-fix

**Bead:** crn-hn8h (deploy) — source review crn-gim — implementation crn-rqf.3 (doc-only, TOML delimiter fix)
**Commit:** `f334453`, on `feature/crn-rqf.3` (stacked on `e37eebe`, tip confirmed as of 2026-07-22)
**Base:** `origin/main@1f8a13b` (original eval); re-verified against current tip `d325f40` below
**Date:** 2026-07-22

> Criteria 1-5 and 7 below transcribe the verdict already recorded in crn-hn8h's bd notes
> (session deployer-gm-4mlmk's fresh independent gate run) — not re-evaluated here. Criterion
> 6 was re-verified fresh in this session (2026-07-22, this worktree) against the current
> `origin/main` tip, since main had advanced substantially (PR #18 + others) since the
> original eval. The infra blocker that previously held this bead (`scripts/rebase-resolve-lib.sh`
> missing rig-wide) is resolved — confirmed present on `origin/main` at commit `128c214`,
> unreverted through current tip `d325f40`; crn-elja/.1/.2 all CLOSED. Proceeding to branch
> cut + PR.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | **Re-verified 2026-07-22 against current tip** (was checked against stale `c86480c`; `origin/main` has since advanced to `d325f40`). `git merge-base f334453 origin/main` = `1f8a13b`. `git merge-tree --write-tree f334453 origin/main` (current tip `d325f40`) returned a single clean tree hash (`ae8532c`), no conflicts, exit 0. Corroborated at file level: `f334453`'s entire diff since merge-base touches exactly one file (`docs/plans/cairn-critic-loop-landing-check.md`, 149 lines, new file); `origin/main`'s advance since merge-base touches 37 files entirely under `internal/critic/`, `internal/cairn/`, `scripts/`, and other `release-gates/*.md` — zero overlap. The clean merge is structural, not incidental. |
| 1 | Review PASS present | PASS | crn-gim closed `reason=approve`, re-check of the TOML delimiter fix, citing exact commit `f334453` and specific parser verification. |
| 2 | Acceptance criteria met | PASS | crn-gim's own AC (verdict recorded, approved) satisfied. Confirmed `f334453` is exactly the commit crn-gim's re-check evaluated. crn-rqf.3's broader ACs correctly out of scope (crn-rqf.3 stays IN_PROGRESS with cairn/builder independently of this doc landing). |
| 3 | Tests pass | PASS | Re-ran build/fmt-check/vet/golangci-lint/test-race in a fresh isolated worktree at `f334453` — all clean. Independently re-verified the substance of the fix (TOML validity), not just the claim: extracted the toml block and parsed it with both Python 3.14 `tomllib` and a throwaway Go program against this repo's real pinned `github.com/BurntSushi/toml v1.6.0` dependency — both parse cleanly, both confirm literal `.` and `\$` sequences survive un-interpreted in the decoded description field. |
| 4 | No high-severity review findings open | PASS | Only two P2 follow-ups open (crn-78d, crn-rqf.4), both forward-looking scope questions on crn-rqf.3's still-open broader implementation, not defects in this doc commit. |
| 5 | Final branch clean | PASS | Recorded PASS in source notes. |
| 7 | Single feature theme | PASS | Both commits since merge-base (`e37eebe`, `f334453`) touch exactly one file. |

## Verdict: PASS — all 7 criteria, proceeding to branch cut + PR.

Former infra blocker (`scripts/rebase-resolve-lib.sh` absent rig-wide,
needed for `resolve_deploy_branch_target`/`assert_safe_push_target`) is
now **resolved**: landed on `origin/main` via PR #18 (commit `128c214`,
2026-07-22 09:18:46 -0700), unreverted through current tip `d325f40`.
crn-elja, crn-elja.1, crn-elja.2 all CLOSED. Verified fresh this session
via `git fetch origin` + `git cat-file -e origin/main:scripts/rebase-resolve-lib.sh`
+ sourcing it and confirming `resolve_deploy_branch_target` /
`assert_safe_push_target` / `attempt_bounded_self_rebase` are all defined
and callable. Cutting `deploy/crn-hn8h-gate` from `f334453` now.
