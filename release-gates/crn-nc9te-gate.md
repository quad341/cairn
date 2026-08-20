# Release Gate: crn-nc9te

**Bead:** crn-nc9te (deploy) — source review + implementation crn-27con.4 (epic crn-27con)
**Commit:** `1d215d78ed8745d4a874a30e4da9ccd95068c56c`, cut onto `deploy/crn-nc9te-gate` off `origin/main`
**Date:** 2026-08-20

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Merge-base with `origin/main` (tip `817e9b528f4bab9dbf7d0122f7b28f1f0fd6dd1e`) is `aa484f2bf631907b0112fa886c38ae6927526b9b`. Main gained 2 commits since (#123, #125 — `cmd/`, `internal/{cairn,critic}`, `release-gates/`); this branch touches only `formulas/`. `comm -12` on the two changed-file sets: zero overlap. `assert_deploy_ancestry_scope origin/main <sha> crn-27con.4`: rc=0. |
| 1 | Review PASS present | PASS | crn-27con.4 closed, REVIEW VERDICT: PASS from cairn/reviewer, independently verified by reviewer in an isolated scratch worktree at this exact commit (build/vet/gofmt clean, full suite + race clean, lint 0 issues on isolated cache). |
| 2 | Acceptance criteria met | PASS | Bug required `needs-investigation` added at the escalation point in both `mol-cairn-librarian*.formula.toml` files, without removing existing labels. Diff confirms: `needs-pm` preserved, `needs-investigation` added to both `--labels` args, guard block in both files extended to assert its presence. Secondary discretionary "second ask" (notify-bucket mail) investigated, found premised on stale evidence, correctly not implemented — follow-up crn-27con.7 filed instead of guessing; bug text itself left this ask optional ("your call"), so it does not block. |
| 3 | Tests pass | PASS | Independently re-verified in a fresh isolated scratch worktree at `1d215d7` (not just trusting the reviewer's claim): `go build ./...`, `go vet ./...`, `gofmt -l .` all clean; `golangci-lint run ./...` (isolated `GOLANGCI_LINT_CACHE`) 0 issues; `go test ./... -race -count=1` — all 7 packages `ok`, 0 FAIL. New regression test `TestLibrarianStaleReviewBranchRecoveryStepHasNeedsInvestigationLabelAndGuard` re-run directly with `-v`: PASS. |
| 4 | No high-severity review findings open | PASS | No `finding`-labeled beads reference crn-27con.4 or crn-nc9te. Related beads: crn-27con.7 (documented non-blocking follow-up), crn-27con/crn-27con.3 (separate parent-epic backlog concern, unrelated to this fix), crn-4j0pa (separate PM task), crn-9r7kn (empty fleet-routing "convoy" artifact, not a code finding). |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `deploy/crn-nc9te-gate` immediately after cut. |
| 7 | Single feature theme | PASS | Diff vs. merge-base touches exactly 3 files: `formulas/mol-cairn-librarian.formula.toml`, `formulas/mol-cairn-librarian-rig.formula.toml`, `formulas/formulas_test.go` (51 insertions, 16 deletions) — one coherent theme (add `needs-investigation` label + guard + regression test, symmetric across both formula files). |

## Verdict: PASS (7/7) — proceeding to PR.

## Merge authority

quad341/cairn — standing self-merge authorization applies (mayor ruling `gm-wisp-2yhv7u`, 2026-08-19), governed by memory `cairn-auto-merge-requires-explicit-strategy`. This bead's description does not carry the older "route to mayor/mpr" boilerplate at all (unlike some sibling deploy beads this cycle), so there is no stale-copy language to address here either way. Deployer will merge directly (`gh pr merge --squash`, plain, once CI is green) under the ruling's 4 standing conditions.

## Note

Source branch `builder/crn-27con.4` is provenance-only per the bead's own instructions — deploy branch cut directly from commit `1d215d7`, not pushed onto or opened from the builder branch.
