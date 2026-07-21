# Release Gate: ci.yml merge_group trigger

Re-cut as an isolated single-commit deploy (crn-1xp, deduped out of the
crn-811 batch per mayor's scope-update comment) to avoid repeating the same
multi-bead tangle crn-811 exists to fix. The reviewed commit
(`9f2750329db5489b436760e6128860b8617a3367`) sat on top of five unrelated
in-flight commits (ShadowMap, topic_key precedence, `get` command, identity
guard) on the shared builder branch `gc-builder-6ac3e0f3c1f3` — cutting the
deploy branch directly at that SHA would have pulled all of them in. Per
mayor's comment on crn-1xp (2026-07-21 19:19), the 1-line change was
recreated fresh: this branch (`deploy/crn-1xp-mergegroup`) is `origin/main`
+ exactly one cherry-picked commit.

Deploy source: `9f2750329db5489b436760e6128860b8617a3367` (ci(cairn): add
merge_group trigger to ci.yml), cherry-picked onto `origin/main` @
`8e04de23035120c3c9e05cc5d828461aae59544e`. Pre-image of `ci.yml` on
`origin/main` verified byte-identical to the commit's parent, so the
cherry-pick applied with zero conflicts.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | Reviewed + PASSED by cairn/reviewer (crn-6k6 notes: "REVIEW VERDICT: PASS"). Workflow-backed max-effort review (6 finders, 2 verifier groups, 1 sweep) against the exact commit diff. |
| 2 | Acceptance criteria met | PASS | crn-wok AC: add bare `merge_group:` key to `ci.yml`'s `on:` block, matching `pull_request`'s bare-key style. Verified via `git diff origin/main -- .github/workflows/ci.yml` — exactly the one line added, nothing else. |
| 3 | Tests pass | PASS | `go build ./...` clean. `go test ./... -race -count=1` — all packages green (`cmd`, `internal/cairn`), no regressions. `python3 -c "import yaml; yaml.safe_load(...)"` confirms `ci.yml` still parses. |
| 4 | No high-severity findings open | PASS | Review surfaced 2 findings, both explicitly non-blocking (CI efficiency: no `concurrency:` group; hardening: no `timeout-minutes:`), both pre-existing gaps not introduced by this diff, tracked separately in crn-r6z. No open HIGH findings against this commit. |
| 5 | Final branch is clean | PASS | `git status` clean on `deploy/crn-1xp-mergegroup` (untracked `.gc/`/`.gitkeep` rig-scaffold files only, unrelated). |
| 6 | Branch diverges cleanly from main | PASS | Branch is `origin/main` (8e04de2) + exactly 1 cherry-picked commit. The *original* recorded SHA/branch (9f27503 on `gc-builder-6ac3e0f3c1f3`) did NOT diverge cleanly (5 unrelated commits ahead) — this was not a case for the bounded self-rebase (divergence was bundled unrelated work, not trivial staleness), so per mayor's explicit direction the isolated diff was recreated fresh on a new branch off current `origin/main` instead. |
| 7 | Single feature theme | PASS | Exactly one commit, scoped to `.github/workflows/ci.yml` only (verified via `git diff --stat origin/main..HEAD` — 1 file, 1 insertion). |

Additional checks:
- `gofmt -l .` — clean, no files listed.
- `golangci-lint run ./...` — 0 issues.

## Provenance note

This is the merge_group trigger prerequisite for crn-54c (mayor enabling the
GitHub merge queue on `quad341/cairn` main). Once this lands, `crn-wok`
closes and crn-54c can proceed to enable the queue. `scripts/rebase-resolve-lib.sh`
does not exist in this repo, so the shared-branch-name safety check
(equivalent to `assert_safe_push_target`) was performed manually: confirmed
`deploy/crn-1xp-mergegroup` does not match the shared `gc-<agent>-<hex>`
pattern and does not already exist on the remote before pushing.

## Disposition

PR opened from `deploy/crn-1xp-mergegroup` onto `main`. Per this deployer's
narrowly-scoped auto-merge exception, `gh pr merge --auto` is armed on this
PR (opened in this same work-loop run, from a `deploy/*` branch) so main's
merge queue merges it once required checks pass. If arming fails to verify
(most likely because crn-54c hasn't enabled the queue yet — this PR is
crn-54c's own prerequisite), that is escalated to mayor rather than papered
over with a manual merge strategy.
