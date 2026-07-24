# Release Gate: cairn librarian PROMOTE/CULL step idempotency test

Isolated single-commit deploy for crn-0sdm (crn-28ge.1.11, tracked via
crn-7ttk). Source branch `builder/crn-28ge.1.11` is a dedicated
single-commit branch, not a shared/stale fallback branch — no
cherry-pick/history caveat was expected at review time, since the reviewer
confirmed it sat directly atop `origin/main`'s then-exact tip (`4858187`).

By the time this deploy started, `origin/main` had advanced one more commit
(`5bf87a6`, PR #50) past that assumed base, so the reviewed commit was no
longer an ancestor of `main` (`git merge-base --is-ancestor edc4937
origin/main` → not an ancestor). Confirmed provably trivial: the reviewed
commit touches only `formulas/formulas_test.go`, while `5bf87a6` touches a
fully disjoint file set (`cmd/*`, `internal/cairn/*`,
`formulas/mol-cairn-librarian*.formula.toml`, `release-gates/*`), and
`git merge-tree --write-tree origin/main edc4937` resolved with zero
conflicts. `scripts/rebase-resolve-lib.sh` is absent from this repo
(confirmed via a repo-wide search), so the reconciliation was performed
manually via direct cherry-pick rather than the assumed helper script.

Deploy source: `edc49372b127bddb292649a35bdb2ca21cac6665` (test: librarian
PROMOTE/CULL step idempotency, refs crn-28ge.1.11), cherry-picked onto
`deploy/crn-0sdm-gate` = `origin/main` @ `5bf87a6` + 1 commit, new tip
`34eb1fa`, content-identical to the reviewed commit.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit | PASS | crn-7ttk: cairn/reviewer VERDICT: PASS, independently re-verified (not trusted from the builder's/validator's own claims) — diff re-read in full via `git diff`, both new tests' assertions confirmed against the actual step content in `formulas/mol-cairn-librarian.formula.toml`, underlying hard-error premise cross-checked against `internal/cairn/evict.go:92` and `TestEvictToReviewBranchRefusesWhenProposalAlreadyPending`. Reviewer's PASS cites `edc4937` explicitly; deploy SHA (post-cherry-pick content) == reviewed content. |
| 2 | Acceptance criteria met | PASS | Single-file change, `formulas/formulas_test.go` (+61/-0, test-only). `TestLibrarianPromoteAndCullStepsSkipWhenAlreadyTracked` asserts both `promote-candidate-beads` and `cull-candidate-beads` step Descriptions contain the `ANCHOR="[entry:${ENTRY_ID}]"` / `bd list ... --title-contains=$ANCHOR` / `EXISTING`-check dedup idiom. `TestLibrarianCullStepChecksExistingBeforeCallingCullEvict` asserts the `EXISTING` check textually precedes the `cairn cull-evict "$ENTRY_ID"` call in `cull-candidate-beads`' Description — ordering-specific, since `EvictToReviewBranch` hard-errors on a repeat call for the same entry. Both independently re-verified against the real formula step bodies, not just the test code. Consistent with this file's established structural-assertion idiom (`TestLibrarianRigFormulaHasSameStepsAsLibrarian`, `TestCriticFormulaHasVaporPhase`) — not a novel or weaker pattern. |
| 3 | Tests pass | PASS | Fresh run from `deploy/crn-0sdm-gate` (golangci-lint cache cleaned first, per known shared cross-worktree cache staleness): `gofmt -l` clean, `go build ./...` clean, `go vet ./...` clean, `golangci-lint run ./...` — 0 issues, `go test ./... -race -count=1` — all 5 packages green (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `scripts`), no FAILs, no flake noise. Both new tests individually, `-v -race`: `TestLibrarianPromoteAndCullStepsSkipWhenAlreadyTracked` PASS, `TestLibrarianCullStepChecksExistingBeforeCallingCullEvict` PASS. |
| 4 | No high-severity findings open | PASS | Reviewer recorded 0 open HIGH findings; no new I/O or shell-outs, no production code touched — test-only diff. |
| 5 | Final branch is clean | PASS | `git status --porcelain` empty on `deploy/crn-0sdm-gate`, ahead of `origin/main` by exactly 1 commit (`34eb1fa`). |
| 6 | Branch diverges cleanly from main | PASS (after bounded self-rebase) | `origin/main` advanced to `5bf87a6` (PR #50) past the reviewed commit's assumed base. Reconciled by cherry-picking `edc4937` onto current `origin/main` (disjoint file-touch sets + clean `git merge-tree --write-tree` confirmed zero conflicts). Before: `origin/main` @ `5bf87a6`, cherry-pick source `edc4937`. After: `34eb1fa`, content-identical to `edc4937`. |
| 7 | Single feature theme | PASS | One test file, `formulas/formulas_test.go`, +61/-0, two tests both verifying the same idempotency guarantee (skip-when-already-tracked, and check-before-evict ordering) for the sibling PROMOTE/CULL sweep steps added in PR #49 — not independent features. |

## Disposition

**GATE PASS.** PR to be opened from `deploy/crn-0sdm-gate` onto `main`,
GitHub-native auto-merge armed per cairn's scoped deployer merge authority
(squash, per fleet memory `cairn-auto-merge-requires-explicit-strategy` — no
merge-queue ruleset configured on `quad341/cairn`), pending a bounded
post-push CI check.

A bounded self-rebase was performed this run (`origin/main` @ `5bf87a6` ->
`34eb1fa`, cherry-picking `edc4937`) to reconcile the one-commit staleness
described above; recorded in `crn-0sdm`'s notes per the deployer's
self-rebase guardrails.

Mayor notified via FYI mail with the gate result and PR URL, noting that the
current deployer role prompt's self-arm-then-report process was followed for
this deploy rather than the bead's literal "route a merge-request to
mayor/mpr" phrasing — a disclosure, not a permission request.

Downstream: crn-pfdl (`sling-crn-0sdm`) remains blocked on this bead; expected
to resolve once crn-0sdm closes.
