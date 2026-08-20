# Release Gate: crn-kq7tz

**Bead:** crn-kq7tz (deploy) — source: crn-lhchy (review), build: crn-4j0pa (plan: crn-zg69q)
**Reviewed commit:** `cacda2f2e3eff9455344abf81a33b4a59aaf17bc` (matches crn-lhchy's verdict and the deploy bead's `metadata.gc.deploy_commit` — see criterion 1)
**Deploy commit:** `cacda2f2e3eff9455344abf81a33b4a59aaf17bc` — no self-rebase needed, criterion 6 merge-tree came back clean directly
**Date:** 2026-08-20

## Background

`cairn stale-branches` reports two distinct finding shapes on the same
review-branch check: `status == "escalate"` (a reviewer exists but hasn't
acted) and `status == "error"` (cairn itself failed to evaluate the branch —
a reviewer-lookup failure, or `ListReviewBranches` erroring). `evaluateBranch`
sets exactly one of `.reviewer` / `.error`, never both. The
`stale-review-branch-recovery` step's jq filter selected only
`status=="escalate"`, so error-status findings — arguably the ones most
needing a human's attention, since cairn couldn't even evaluate the branch —
were silently dropped every sweep cycle instead of filed.

The fix widens the filter to `select(.status == "escalate" or .status ==
"error")` in both `formulas/mol-cairn-librarian.formula.toml` and
`formulas/mol-cairn-librarian-rig.formula.toml`, adds `ERROR_TEXT` extraction
(`.error // empty`) included in the filed bead body when present, and guards
the "reminder mail already sent to $REVIEWER" sentence behind a
non-null-`$REVIEWER` check (error findings never carry `.reviewer`) — avoiding
a crn-w4c6-shaped "sent to " defect on error findings.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-lhchy's verdict cites `cacda2f2e3eff9455344abf81a33b4a59aaf17bc`, identical to the deploy bead's `metadata.gc.deploy_commit` — exact SHA match (D = R). Independently re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as a transcribed string. |
| 2 | Acceptance criteria met | PASS | Verified directly against crn-4j0pa's full `exit_contract` (5 bullets). Read the actual diff, not just builder/reviewer prose: confirmed jq filter widened in both formula files, `ERROR_TEXT` extraction present, `NOTIFY_LINE`/`RESOLVER` branching guards the reminder sentence on `[ -n "$REVIEWER" ]` in both files identically. Sequencing precondition crn-nc9te (6b45121, PR #126) independently confirmed reachable from origin/main via `git merge-base --is-ancestor` (also visible directly in `git log origin/main`). |
| 3 | Tests pass | PASS | Independently re-run at the deploy commit (detached checkout, clean tree). Pre-flight: this repo's suite has no container/testcontainers dependency (`grep -rl testcontainers\|DOCKER_HOST` empty), so the podman setup step is not applicable here. `go build ./...` exit 0; `go vet ./...` exit 0; `go test ./... -race -count=1` — **7/7 packages ok** (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`), 0 FAIL, 0 SKIP. Diff-owned test mapped by exact name: `TestLibrarianStaleReviewBranchRecoveryStepHandlesErrorStatus` re-run individually (`-run '^...$' -v`) — PASS. 3a (attribute failing tests) not applicable, zero failures anywhere. 3b (policy/lint lane): this repo's only policy lane is the CI `lint` job itself (`golangci-lint run ./...` — confirmed via `.github/workflows/ci.yml`; no separate `ci-pr-policy`-style Makefile target exists here, `Makefile` targets are only `all/build/formulas/test/install/fmt/fmt-check/clean/help`). Run with an isolated `GOLANGCI_LINT_CACHE` (avoiding known cross-session cache contamination on this machine): `golangci-lint run ./...` — 0 issues; `golangci-lint fmt -d ./...` exit 0, no diff; `gofmt -l .` empty. |
| 4 | No open HIGH findings | PASS | crn-lhchy records **no blocking findings** ("No blocking findings. Handing off to deployer."). No open HIGH/blocking finding anywhere in the chain. |
| 5 | Clean tree | PASS | Verified via direct detached checkout of `cacda2f2e3eff9455344abf81a33b4a59aaf17bc^{commit}`; `git status --porcelain` empty. |
| 6 | Clean divergence from main | PASS | `git merge-tree --write-tree origin/main cacda2f2e3eff9455344abf81a33b4a59aaf17bc` → clean merge tree `4b1a3794db0e1c91c0d157a6b967b0f366ff5b49`, exit 0, zero conflicts — **no self-rebase needed**. `origin/main` has advanced by exactly one commit since this branch's fork point (`93c2593`) — the crn-bmy54 merge (`a7f9cdc`, PR #131) — and it does not touch either formula file this diff owns, so the merge stays conflict-free without rebasing. Pre-flight (has the target already merged?): `gh api repos/quad341/cairn/commits/cacda2f2e.../pulls` returned empty — no PR exists for this SHA — and `git merge-base --is-ancestor cacda2f2e... origin/main` confirmed it is NOT already on main. Normal flow applies. |
| 7 | Single feature theme | PASS | `git diff origin/main...cacda2f2e3eff9455344abf81a33b4a59aaf17bc --stat` (correct triple-dot form, symmetric-difference from the merge-base — a plain two-dot diff against current `origin/main` is misleading here since main advanced past the branch's fork point and a two-dot diff spuriously shows those advances "reverted") → exactly 3 files: `formulas/formulas_test.go` (+32), `formulas/mol-cairn-librarian-rig.formula.toml`, `formulas/mol-cairn-librarian.formula.toml` — 94 insertions, 16 deletions, all tightly related to the single fix. Out-of-scope paths (`packs/cairn-loop-orders/orders/cairn-librarian-rig-cooldown.toml`, `TestLibrarianStepsHaveNeedsPmLabelAndGuard`) confirmed untouched. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority — self-merge authorized under the reinstated standing
   authorization.** Same basis as `release-gates/crn-bmy54-gate.md` note 1:
   mayor ruling `gm-wisp-2yhv7u` (2026-08-19) reinstated deployer self-merge
   for quad341/cairn (not covered by `mpr`), and explicitly instructs future
   sessions not to re-pause on a bare repeat of the generic "route to
   mayor/mpr" boilerplate carried in this formula's own step text. crn-kq7tz's
   notes raise nothing beyond that same recycled clause. Proceeding under the
   STANDING AUTHORIZATION subject to the reinstatement's four conditions: (1)
   gate 7/7 PASS — this table; (2) PR state confirmed via a direct, fresh `gh
   pr view --json` read at merge time; (3) CLEAN/MERGEABLE with both required
   checks (`build-test`, `lint`) COMPLETED/SUCCESS; (4) no `--auto` arm —
   plain `gh pr merge <n> --squash --delete-branch` only (per mayor's
   `gm-wisp-fvdoja` addendum), then independently verified.

2. **Pre-flight (target-already-merged check) run before criterion 6**, per
   the formula's own ordering requirement (ga-xykek3 incident). No PR existed
   for the deploy SHA and it is not yet an ancestor of `origin/main` — normal
   flow, not reconciliation.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` on both the D (deploy
   bead metadata) and R (review bead metadata) sides before comparison, never
   trusted as an eyeballed or transcribed string.

4. **Two-dot vs. triple-dot diff pitfall (recorded for future gate
   sessions):** a first pass at criterion 7 used `git diff origin/main
   cacda2f2e...` (two-dot) and produced an alarming diffstat showing large
   deletions across unrelated files (`internal/cairn/entry.go`,
   `release-gates/crn-bmy54-gate.md`, etc.) — these were `origin/main`'s own
   advances past this branch's fork point being spuriously shown as
   "reverted," not real changes in this diff. Re-run with the triple-dot form
   (`origin/main...cacda2f2e...`, symmetric difference from the merge-base)
   immediately resolved to the correct, narrow 3-file diffstat matching the
   reviewer's own count. Worth a formula-text callout if this recurs.
