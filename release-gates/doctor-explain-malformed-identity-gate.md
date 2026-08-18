# Release Gate: doctor-explain-malformed-identity

**Bead:** crn-62m6k (deploy) — source: crn-1sg23 (review), build: crn-qjpc0
**Commit:** `889c41c1372e1b2e6c9f1e39ef9f459ff3f307c6`, cut onto `deploy/crn-62m6k-gate`
**Date:** 2026-08-18

## Background

`cairn doctor explain` is the command every other diagnostic (`status`,
`doctor`, `recall-stats`, `dedup`, `rage`, `promote-candidates`) tells the
user to reach for when identity resolution looks wrong — but it was the one
command still resolving `$CAIRN_IDENTITY` with the unvalidated
`resolveIdentity(cmd)` instead of `resolveIdentityValidated(cmd)`, which
every other identity-consuming command had already migrated to.

`$CAIRN_IDENTITY` is split on whitespace while the `--identity` flag is
comma-split, so a natural value like
`CAIRN_IDENTITY="rig:alpha,role:investigator"` becomes one malformed tag
containing a literal comma. `resolveIdentityValidated` rejects that via
`cairn.ValidatePathSegment`; the unvalidated path let it flow straight into
scope resolution, where it matched nothing and printed a confident, exit-0
`winner: none (no candidate satisfies this identity)` — indistinguishable
from a genuinely unrelated identity. The tool's own recommended diagnostic
command was the one place that answered wrong without saying so.

The fix resolves identity via `resolveIdentityValidated` and returns
`emitError` on failure, matching `cmd/list.go`'s pattern exactly, so a
malformed value now surfaces as an `invalid_input` error (including in
`--json` output) instead of a silent false negative. The `--identity` flag
path is unaffected (still comma-split by cobra) and is pinned by a
regression test.

Review round 1 (crn-1sg23) requested one addition — end-to-end `--json`
coverage for the malformed-identity path — which the builder added as
`TestDoctorExplainJSONRendersInvalidInputForMalformedIdentity` and resubmitted.
Round 2 passed.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-1sg23 round 2 verdict cites `889c41c1372e1b2e6c9f1e39ef9f459ff3f307c6`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R), not an ancestor relationship. Both independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"` (not trusted as a well-formed string). |
| 2 | Acceptance criteria met | PASS | crn-qjpc0's 3-item fix spec verified directly against the diff, not just the reviewer's write-up: (1) `cmd/doctor.go`'s `identityRequested` branch now calls `resolveIdentityValidated(cmd)` + `emitError` on error, replacing the bare `resolveIdentity(cmd)` — confirmed via `git diff` against the pre-fix commit. (2) `TestDoctorExplainRejectsMalformedEnvIdentity` present. (3) `TestDoctorExplainAcceptsCommaIdentityFlag` present (flag-path regression guard). Round-2's `TestDoctorExplainJSONRendersInvalidInputForMalformedIdentity` also present. All 4 confirmed by function name directly in `cmd/doctor_test.go` at the deploy commit. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on a detached checkout of `889c41c1...` (not trusting the reviewer's report alone): `go test ./... -race -count=1 -v`, exit 0. **7/7 packages ok, 659 PASS, 0 FAIL, 0 SKIP** (explicitly counted from `-v` output, not inferred from package-level `ok` alone) — matches crn-1sg23's independently-reported tally exactly. |
| 4 | No open HIGH findings | PASS | crn-1sg23 round 2: 0 style/security findings. Independently checked `bd list --status open` / `--status in_progress` for any bead referencing this work: only the deploy bead itself and a routing/tracking "convoy" bead (crn-yl7rm, `gc sling`'s own bookkeeping artifact, no findings content) — no open HIGH finding anywhere in the chain. |
| 5 | Clean tree | PASS | `deploy/crn-62m6k-gate` cut directly from `889c41c1...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty throughout. |
| 6 | Clean divergence from main | PASS | `git merge-tree --write-tree 889c41c1... origin/main` resolved cleanly (exit 0, single tree, no conflict markers) despite three files (`cmd/commands_test.go`, `internal/cairn/freshness.go`, `internal/cairn/freshness_test.go`) showing up in both sides' independent history — see criterion 7 for why that overlap is benign. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 889c41c1... crn-62m6k crn-qjpc0 crn-1sg23 crn-fqe` → rc=0 (no `.claude/**` paths; every commit cites an accepted id). Two of the five commits on the reviewed branch (`638a9b6`, `471733d`) cite sibling bead `crn-fqe`, not this deploy's own ids — confirmed legitimate: `crn-fqe` is a real, closed builder bug bead on this same shared branch (`gc-builder-d53f3747bdfa`), already reviewed (crn-ttnl4) and shipped separately via the crn-a17wj deploy (PR #101, squash-merged to main as `2dce629`). Independently confirmed `origin/main`'s current content for the two overlapping non-test files is byte-identical to `crn-fqe`'s `471733d` (`git diff 471733d origin/main -- internal/cairn/freshness.go cmd/commands_test.go` is empty) — so this PR's diff will show that content again only as a git-ancestry artifact of the earlier squash-merge (471733d isn't a `main` ancestor even though its content is), not new or duplicate functionality. That content has already been reviewed twice over: originally in crn-ttnl4, and again in crn-1sg23 round 2's stated "full independent re-verification of `471733d..889c41c1` range." The deploy's own theme — doctor's malformed-identity handling — is carried entirely by `cmd/doctor.go` and `cmd/doctor_test.go`. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority:** per the mayor-ruled standing authorization
   (`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15,
   independently re-verified this session against current branch protection
   — `build-test`+`lint` required, 0 required approvals, squash-only) and the
   current deployer role prompt, for `quad341/cairn` gate 7/7 PASS + CI green
   ⇒ the deployer merges directly, no mayor escalation required. This
   supersedes the deploy bead's own template body text calling for a
   mayor/mpr merge-request — that language covers what happens if the merge
   mechanics themselves don't go through cleanly, not a precondition to ask
   permission before merging.

2. **Branch target:** per the deploy bead's MERGE_POLICY note,
   `gc-builder-d53f3747bdfa` is a generic hash-named builder-session branch
   (not bead-scoped) that the builder session may reuse for unrelated future
   work — this gate does not push to it. `deploy/crn-62m6k-gate` was cut
   fresh from the exact reviewed SHA via `resolve_deploy_branch_target`.

3. **Continuity note:** the immediately-prior sibling deploy (crn-a17wj)
   disclosed that `assert_deploy_ancestry_scope` did not yet exist in
   `rebase-resolve-lib.sh` and verified scope manually as a stopgap, filing a
   follow-up to implement it. That function is now present (line 633 of the
   current library) and was used directly for criterion 7 above — the gap is
   closed.
