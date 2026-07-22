# Release Gate: cairn-remember-shared-tier

**Bead:** crn-apg (deploy) — source review crn-kbf (re-review, resubmission) — implementation crn-419.4, stacked on crn-419.3 (private-tier direct commit, review crn-ovf PASS)
**Commit:** `d45b4a12186eb4c34503120030c703a76ef2f372`, cut onto `deploy/crn-apg-gate` off `origin/main`
**Date:** 2026-07-22

## Note on commit range

`d45b4a1` (feature/crn-419.4) is stacked directly on `f11ab90` (crn-419.3's
private-tier direct-commit work), which is not yet on `origin/main`. This is
intentional, not scope creep: crn-419.4's `CommitToReviewBranch` sits
directly on top of crn-419.3's `CommitDirect`/`ResolvedTier` foundation
(reconciled onto shared names during the same builder assignment — see
crn-419.3/crn-ovf notes), so the two are not independently landable. crn-ovf
(crn-419.3's review, VERDICT: pass) explicitly deferred filing its own
deploy bead for this reason and deferred to crn-419.4's review clearing
first. Both land together via this one isolated deploy.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Re-fetched `origin/main` immediately before evaluation (tip `22a58ba`, two commits past the merge-base `1f8a13b` — `6bfdc6a` #14 and `22a58ba` #15). Diffed the file sets: `origin/main`-only commits touch `cmd/ergonomics_scenario*.go`, `internal/critic/**`, `docs/plans/**`, `release-gates/critic-loop-stress-scenarios-judges-gate.md` — entirely disjoint from this branch's `cmd/remember.go`, `cmd/remember_test.go`, `cmd/reviewer.go`, `cmd/reviewer_test.go`, `internal/cairn/remember.go`, `internal/cairn/remember_test.go`. Independently ran `git merge-tree --write-tree d45b4a1 origin/main`: single clean tree hash (`5f14947`), exit 0, no conflict markers. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-kbf closed (reason: pass) with "RE-REVIEW (resubmission), VERDICT: pass" from cairn/reviewer, independently verified by the reviewer in a fresh detached-HEAD worktree at `d45b4a1` (not the shared builder worktree). crn-419.3's own review (crn-ovf) also independently PASS, verified at `f11ab90` (the ancestor commit this branch is stacked on). |
| 2 | Acceptance criteria met | PASS | crn-419.4's 4 ACs (shared-tier branch+isolated-worktree commit; never commits a shared-tier entry to the default branch incl. on mail failure; reviewer resolved via flag > env > per-tier default; mail-send failure leaves branch/commit intact and reports clearly) — AC1/AC2 confirmed unchanged from the first (already-passing) review pass (`git diff 293a1e9..d45b4a1` touches only `cmd/reviewer.go`, `cmd/remember_test.go`, new `cmd/reviewer_test.go` — zero diff to the AC1/AC2-implementing files). AC3 upgraded to fully tested: `TestDefaultReviewerPerTier` (all 3 tiers), `TestDefaultReviewerSharedTiersRequireGCRig`, `TestDefaultReviewerUnknownTier`, `TestResolveReviewerPrecedence` (flag/env/default, 3 subtests). AC4's required gap closed: `TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError` asserts (a) the review branch survives via `git rev-parse --verify` against the store's actual object database, (b) the error names both branch and "mail", (c) stdout has exactly 2 lines (no false-success "mailed reviewer" line). crn-419.3's 4 ACs (single direct commit to default branch, only the entry file; no branch created; SHA reported; git failure surfaced without data loss) independently confirmed PASS in crn-ovf. |
| 3 | Tests pass | PASS | On commit `d45b4a1`, in a fresh isolated detached-HEAD worktree created in this session (not the shared builder worktree, not trusted from review notes alone): `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `go test ./... -race -count=1` — both packages (`cmd`, `internal/cairn`) ok, zero regressions. `golangci-lint run ./...` initially surfaced one `gosec` (G306) finding, but its file path pointed at a *different, no-longer-existing* worktree (the reviewer's own scratch worktree from an earlier session) — a stale lint-cache artifact, not a real finding. Confirmed by reading the actual line 123 of `cmd/remember_test.go` in this worktree: it already carries `//nolint:gosec // must be executable to stand in for the gc binary on PATH`. After `golangci-lint cache clean`, re-run: 0 issues. |
| 4 | No high-severity review findings open | PASS | crn-kbf's one required item (AC4 missing test) closed and independently re-confirmed by direct inspection of the test body, not just notes. The one optional/suggested low-severity item (reject leading `-` in `validateReviewerAddress`) was applied and tested anyway (`TestValidateReviewerAddressRejectsLeadingDash`), confirmed present in `reviewer.go`. crn-ovf (crn-419.3's review) raised no blocking findings; its one non-blocking coverage observation (untested `git commit`-failure branch in `CommitDirect`) was explicitly marked not required. Zero HIGH findings open across either review. |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on `deploy/crn-apg-gate` (no modified/staged files); only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Diff vs. the `origin/main` merge-base (`1f8a13b`) touches exactly 6 files, all in one subsystem — the `remember` write-back verb's non-private commit path: `cmd/remember.go`, `cmd/remember_test.go`, `cmd/reviewer.go` (new), `cmd/reviewer_test.go` (new), `internal/cairn/remember.go`, `internal/cairn/remember_test.go`. crn-419.3 and crn-419.4 are not independent themes: crn-419.4's `CommitToReviewBranch`/tier switch is built directly on crn-419.3's `ResolvedTier`/`CommitDirect`/`gitRun`, reconciled onto shared exported names during the same builder assignment — removing crn-419.3's commit would break crn-419.4's build, not just its behavior. |

## Verdict: PASS — proceeding to PR.
