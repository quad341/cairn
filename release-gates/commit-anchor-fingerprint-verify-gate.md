# Release Gate: commit-anchor-fingerprint-verify

**Bead:** crn-a17wj (deploy) — source: crn-ttnl4 (review), build: crn-fqe
**Commit:** `471733d4b3d9f27a51130e753654e0662ab55efd`, cut onto `deploy/crn-a17wj-gate`
**Date:** 2026-08-15

## Background

Extends the crn-6az.8.2 untracked-anchor fix (commit 131c2b0, reviewed in
crn-44l) to the `type=commit` case named in crn-6az.8.2's own acceptance
criteria but not covered by that fix. `ComputeFingerprint`'s "commit" case in
`internal/cairn/freshness.go` previously did `return a.Spec` unconditionally
— no check that the SHA resolves to a real commit object in the target repo,
so a bogus/nonexistent commit-SHA anchor was treated as a stable fingerprint
forever and never surfaced `Unknown`. The fix verifies `a.Spec` resolves via
`git cat-file -e <spec>^{commit}`, returning `("", nil)` on failure —
mirroring the already-accepted "files"-case guard/call/branch structure.
Fingerprint value itself is unchanged for valid commits (still `a.Spec`), so
existing stored fingerprints remain compatible — only the verifiability gate
changed. A collateral test fix in `cmd/commands_test.go` was required because
the old unconditional pass-through behavior it depended on no longer exists.

Reviewed and PASSED by `cairn/reviewer` (crn-ttnl4, closed, verdict: pass)
against exactly this commit.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-ttnl4's notes cite `tdd_green: 471733d4b3d9f27a51130e753654e0662ab55efd`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R), not an ancestor relationship. Both independently re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` this session (not trusted as a well-formed string). |
| 2 | Acceptance criteria met | PASS | crn-fqe's exit_contract (verify `a.Spec` resolves via `git cat-file -e <spec>^{commit}`; unresolvable spec/missing repo → `("", nil)`/Unknown, not a fabricated stable fingerprint; mirror the files-case pattern) is met exactly per crn-ttnl4's spec-compliance write-up, cross-checked against the diff-owned test names below, which cover the happy path, invalid-SHA, and missing-repo branches plus a `Check()`-level regression pin. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-a17wj-gate` at `471733d4...` (not trusting the reviewer's report alone): `go build ./...` exit 0, `go vet ./...` clean, `gofmt -l .` empty. `make test` (`go test ./... -race -count=1`): **7/7 packages ok, 0 FAIL, 0 SKIP.** All 5 diff-owned tests individually re-confirmed PASS by name via `-run '<regex>' -v`: `TestCommitAnchorInvalidSHAFingerprintEmpty`, `TestCommitAnchorValidSHAFingerprintUnchanged`, `TestComputeFingerprintCommitAnchorMissingRepoEmpty`, `TestCheckCommitAnchorInvalidSHANoLongerFreshForever`, `TestGetLogsRetrievalOutcomeStaleWhenAnchorDrifted`. |
| 4 | No open HIGH findings | PASS | crn-ttnl4: 0 style findings (gofmt/vet/golangci-lint clean), 0 security findings (argv-array `exec.CommandContext`, no shell interpolation, no widened trust boundary). `bd show crn-ttnl4 --json` (`comment_count: 0`), `bd list --parent crn-ttnl4`/`--parent crn-fqe` (both empty — no child finding beads), `bd search "crn-ttnl4"` (only this deploy bead + already-closed routing/review beads) — no open HIGH finding anywhere in the chain. |
| 5 | Clean tree | PASS | `deploy/crn-a17wj-gate` cut directly from `471733d4...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty. |
| 6 | Clean divergence from main | PASS | `merge-base(D, origin/main)` = `0a3efd2` = `origin/main`'s own tip exactly — zero staleness, no rebase needed. Matches crn-ttnl4's own note that the branch was "rebased cleanly onto origin/main @ 0a3efd2." |
| 7 | Single feature theme | PASS | All 3 changed files (`internal/cairn/freshness.go`, `internal/cairn/freshness_test.go`, `cmd/commands_test.go`) implement one cohesive theme: commit-anchor fingerprint verification. The `cmd/commands_test.go` change is a collateral fix for the same behavior change (old pass-through assumption no longer holds), not a drive-by or independent theme. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. Merge authority: per the mayor-ruled standing authorization
   (`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15) and
   the current deployer role prompt, for `quad341/cairn` gate 7/7 PASS + CI
   green (`build-test`, `lint`) ⇒ the deployer arms `gh pr merge --auto`
   directly, no mayor escalation required. This supersedes the deploy bead's
   own stale body text calling for a mayor/mpr merge-request.

2. Branch target: per the deploy bead's explicit MERGE_POLICY note,
   `gc-builder-d53f3747bdfa` is a generic hash-named builder-session branch
   (not bead-scoped) that the builder session may reuse for unrelated future
   work — this gate does not push to it. `deploy/crn-a17wj-gate` was cut
   fresh from the exact reviewed SHA via `resolve_deploy_branch_target`.

3. **Gap disclosure — `assert_deploy_ancestry_scope` does not exist.** The
   deployer process calls for sourcing `scripts/rebase-resolve-lib.sh` and
   running `assert_deploy_ancestry_scope` alongside `resolve_deploy_branch_target`
   and `assert_safe_push_target`. Confirmed absent from the codebase this
   session: `grep -rn "assert_deploy_ancestry_scope"` across `*.sh`/`*.go`/
   Makefile returns nothing outside the role prompt itself; sourcing the
   library cleanly (`. scripts/rebase-resolve-lib.sh; type assert_deploy_ancestry_scope`)
   confirms no such function is defined; `bd search` finds no existing bead
   tracking this gap. The library defines only `assert_safe_push_target`
   (line 580) and `resolve_deploy_branch_target` (line 618). Rather than
   fabricate or hand-implement missing gate-safety logic, the substantive
   property that function would presumably check — that this deploy's diff
   stays within the scope authorized for it — was verified manually: the
   diff-owned file list (`internal/cairn/freshness.go`,
   `internal/cairn/freshness_test.go`, `cmd/commands_test.go`) contains zero
   `.claude/**` or other out-of-scope paths, and both commits on the reviewed
   branch (`638a9b6` tdd_red, `471733d4...` tdd_green) explicitly cite
   `crn-fqe` — this bead's own build bead — in their messages. A follow-up
   bead has been filed to implement the missing function (see bead notes on
   crn-a17wj for the ID) so future deploys don't have to repeat this manual
   step.
