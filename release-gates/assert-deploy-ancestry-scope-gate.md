# Release Gate: assert-deploy-ancestry-scope

**Bead:** crn-lqy6l (deploy) — review: crn-164jo, build: crn-oj8dl
**Commit:** `630568d32c4e9119c50030c76dc7c515a9125731`, cut onto `deploy/crn-lqy6l-gate`
**Date:** 2026-08-18

## Background

The deployer role prompt's mandatory branch-safety recipe sources
`scripts/rebase-resolve-lib.sh` (cairn's own copy) and calls three functions:
`resolve_deploy_branch_target`, `assert_safe_push_target`, and
`assert_deploy_ancestry_scope`. Only the first two existed. crn-oj8dl
(discovered during the crn-a17wj deploy gate, which fell back to manual
scope-verification as a stopgap) tracked implementing the missing function:
given a deploy branch's commit range against its cut point, verify (1) every
commit cites an authorized bead id and (2) the range doesn't touch anything
under `.claude/**`.

crn-oj8dl's build added `assert_deploy_ancestry_scope` to
`scripts/rebase-resolve-lib.sh` (cairn product repo copy — a separate
codebase from the gc-management meta-repo tool of the same name that this
gate itself uses below) plus a dedicated `scripts/test-rebase-resolve.sh`
suite (6 new `ancestry-scope/*` cases). Purely additive: 2 files, 266
insertions, 0 deletions. crn-164jo reviewed and passed it, independently
reproducing build/vet/shellcheck/tests in an isolated scratch worktree at
the resolved commit (the bead's originally-recorded SHA went stale mid-review
when the shared builder branch was rebased onto a newer `origin/main`;
crn-164jo verified via `git patch-id --stable` that pre/post-rebase trees are
content-identical and cited the resolved, reachable SHA going forward).

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-164jo's close_reason and VERDICT line cite `630568d32c4e9119c50030c76dc7c515a9125731`, identical to crn-lqy6l's `metadata.commit` — exact SHA match (D = R), both independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as transcribed text. |
| 2 | Acceptance criteria met | PASS | crn-oj8dl's suggested direction (bead-citation check + path-scope check) verified directly against the shipped code, not just the reviewer's write-up: `assert_deploy_ancestry_scope(bead_id, base_ref, allowed-path-pattern...)` in the cairn-repo `scripts/rebase-resolve-lib.sh` checks citation first (`grep -qF -- "$bead_id"` against full commit body) then changed-path scope (`case "$f" in $pattern)` glob match against caller-supplied patterns), matching the documented 0/1/2/10 return contract exactly. All 6 new named tests (`ancestry-scope/passes-when-cited-and-in-scope`, `refuses-uncited-commit` rc=1, `refuses-out-of-scope-path` rc=2, `multiple-patterns-or-matching`, `rejects-bad-usage` rc=10, `noop-when-no-new-commits` rc=0) confirm each return path independently. |
| 3 | Tests pass | PASS | Independently re-run by the deployer in an isolated scratch worktree (`git worktree add --detach`) at `630568d...`, not trusting the reviewer's report alone: `go build ./...` exit 0, `go vet ./...` exit 0, `bash scripts/test-rebase-resolve.sh` → **pass=33 fail=0**, all 6 new `ancestry-scope/*` cases confirmed passing by name, 27 pre-existing cases unaffected. Matches both the builder's self-report and crn-164jo's independent reproduction exactly. Repo CI (`build-test`, `lint`) is Go-only and does not execute this shell suite — pre-existing gap, not introduced by this diff (crn-164jo flagged it as a non-blocking follow-up candidate). |
| 4 | No open HIGH findings | PASS | crn-164jo: 0 HIGH findings. Two shellcheck notes on the diff, neither blocking: SC2018/SC2019 (line 124) predate this diff entirely — the diff only appends lines 655–775, confirmed via `git diff --stat origin/main..630568d...` (`@@ -652,3 +652,124 @@`, i.e. new content starts after the pre-existing line 124). SC2254 (line 756, diff-owned) is the unquoted `$pattern` in `case "$f" in $pattern)` — independently confirmed this is required, not a defect: the function's entire path-scope mechanism depends on `$pattern` being glob-matched, not literal-matched; quoting it would silently break the feature. Security: bead_id/base_ref/patterns are trusted caller-supplied args (deployer's own call sites), citation check uses fixed-string `grep -qF`, all 4 setup-failure paths fail closed (rc=10). |
| 5 | Clean tree | PASS | `deploy/crn-lqy6l-gate` cut directly from `630568d...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty throughout, including in the independent scratch-worktree verification. |
| 6 | Clean divergence from main | PASS | `git merge-base origin/main 630568d...` equals `origin/main`'s own tip (`1c2a370...`) exactly — `630568d...` is a pure fast-forward, 2 commits ahead, zero divergence. No self-rebase needed. |
| 7 | Single feature theme | PASS | Diff is exactly one function (`assert_deploy_ancestry_scope`) plus its dedicated test suite, 2 files, 0 deletions. `assert_deploy_ancestry_scope origin/main 630568d32c4e9119c50030c76dc7c515a9125731 crn-lqy6l crn-oj8dl crn-164jo` (meta-repo copy, this gate's own tool) → rc=0: no `.claude/**` paths, both commits (`1322032` RED, `630568d` GREEN) cite `crn-oj8dl` — the build bead — literally in their subject lines. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority:** per the mayor-ruled standing authorization
   (`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15) and
   the current deployer role prompt — re-verified this session directly
   against current branch protection (`build-test`+`lint` required, 0
   required approvals) and repo merge settings (squash-only:
   `allow_squash_merge=true`, `allow_merge_commit=false`,
   `allow_rebase_merge=false`) — for `quad341/cairn`, gate 7/7 PASS + CI green
   ⇒ the deployer merges directly, no mayor escalation required. This
   supersedes crn-lqy6l's own template body text calling for a mayor/mpr
   merge-request; that language, per the immediately-preceding sibling gate
   (crn-62m6k, `release-gates/doctor-explain-malformed-identity-gate.md`,
   merged today as PR #116 / `1c2a370...`), covers what happens if the merge
   mechanics themselves don't go through cleanly, not a precondition to ask
   permission before merging.

2. **Ancestry-scope bead ids:** both commits in the deploy range cite
   `crn-oj8dl` (the build bead), not `crn-lqy6l` (this deploy bead) or
   `crn-164jo` (the review bead) — expected, since the commits were authored
   during the build phase before the review/deploy beads existed. Passed all
   three ids to `assert_deploy_ancestry_scope`, matching the same
   deploy+build+review pattern the crn-62m6k precedent used.

3. **Two same-named, independent functions:** the cairn-repo copy of
   `assert_deploy_ancestry_scope` (shipped by this deploy, signature
   `(bead_id, base_ref, path-pattern...)`, checked in criterion 2 above) and
   the gc-management meta-repo copy (this gate's own tool, signature
   `(base_ref, deploy_sha, bead-id...)`, used in criterion 7 above) are
   separate implementations in separate repos that happen to share a name
   and purpose. Not a duplication bug — confirmed no cross-reference between
   them.

4. **Branch target:** `gc-builder-d53f3747bdfa` is the shared per-role
   builder session branch (provenance only, not bead-scoped) — this gate does
   not push to it. `deploy/crn-lqy6l-gate` was cut fresh from the exact
   reviewed SHA via `resolve_deploy_branch_target`, confirmed a safe push
   target via `assert_safe_push_target`.
