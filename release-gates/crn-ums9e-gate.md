# Release Gate: crn-ums9e

**Bead:** crn-ums9e (deploy) — source: crn-8mtvx (review), build: crn-rymq3
**Commit:** `2f195f292cb6e806292ac417e4ab372b83d4fb22`, cut onto `deploy/crn-ums9e-gate`
**Date:** 2026-08-19

## Background

`cairn remember`'s shared-tier write path (`commitToReviewWorktree`) commits
an entry's content to an isolated `remember/<id>` review branch, but left the
same content also present in the store's own working tree — untracked for a
brand-new entry, or tracked-and-modified for a recurrence/promotion patch of
an already-merged entry. Either shape makes `git merge --no-ff remember/<id>`
refuse later with "untracked working tree files would be overwritten by
merge", so a reviewer's only path was deleting what looked like the memory
before merging. 51 branches had accumulated pending review as a result.

The fix appends a working-tree cleanup step after the review-branch commit:
`git cat-file -e HEAD:<rel>` distinguishes the two pre-states (tracked at
HEAD vs. not), then either `git checkout HEAD -- <rel>` (restore the
tracked-and-modified case) or `os.Remove` (drop the untracked case). Both
leave the store's working tree clean at that path, so the later merge no
longer collides.

Two items were explicitly ruled out-of-scope for this bead rather than
folded in, per the review bead's ruling conditions: the disclosed trade-off
that a pending-review entry is genuinely not recall-visible until merged
(6 tests assert this is intentional, not a regression), and a follow-up on
recall reachability tracked separately as `crn-ry4zu`.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-8mtvx's notes cite `deploy_commit: 2f195f292cb6e806292ac417e4ab372b83d4fb22`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R). Independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as a transcribed string. |
| 2 | Acceptance criteria met | PASS | Verified directly against `git diff origin/main..2f195f2 -- internal/cairn/remember.go`, not just the reviewer's write-up: the ~23-line append implements exactly the claimed exit contract — `cat-file -e HEAD:<rel>` err==nil → `checkout HEAD -- <rel>` (tracked-and-modified restored); err!=nil → `os.Remove(e.BodyPath)` (untracked removed). Independently re-ran the 5 test functions the diff itself touches (`go test -race -v -run`), all PASS: `TestCommitRecurrenceToReviewBranchOnAlreadyMergedEntryRestoresWorkingTree`, `TestRememberCrossCallSharedTierRecurrenceNotDetectedWhilePendingReview`, `TestRememberForceCannotNameDuplicateWhilePendingReview`, `TestWriteBackPromotedBeadIDFailsWhilePendingReview`, `TestWriteBackRecurrenceCountFailsWhilePendingReview`. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-ums9e-gate` (cut from `2f195f292cb6e806292ac417e4ab372b83d4fb22`), not trusting the reviewer's report alone: `go test ./... -race -count=1`, exit 0, **7/7 packages ok**, 0 FAIL, 0 SKIP — matches crn-8mtvx's independently-reported tally. |
| 4 | No open HIGH findings | PASS | Reviewer notes: `security_findings: none` (OWASP Top 10 walk against the diff), `style_findings: none`, no blocker/major/minor findings. Independently re-ran `gofmt -l` on all 5 touched files (clean), `go vet ./...` (exit 0), `golangci-lint run --new-from-rev=origin/main ./internal/cairn/... ./cmd/...` (0 issues) myself rather than trusting the transcript. `bd list --status open`/`in_progress` swept for the chain (crn-ums9e, crn-8mtvx, crn-rymq3): only routing bookkeeping (`crn-2tucn: sling-crn-ums9e`), no findings content. |
| 5 | Clean tree | PASS | `deploy/crn-ums9e-gate` cut directly from `2f195f292cb6e806292ac417e4ab372b83d4fb22^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty throughout. |
| 6 | Clean divergence from main | PASS | `git rev-list --left-right --count origin/main...2f195f2` → `0 2`: the deploy commit is a clean 2-commit (red/green TDD pair) fast-forward stack on top of current `origin/main` (`35703d8`), zero behind. No rebase needed, no conflict possible. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 2f195f292cb6e806292ac417e4ab372b83d4fb22 crn-ums9e crn-rymq3` → rc=0: no `.claude/**` paths touched, and both non-merge commits in `origin/main..2f195f2` cite `crn-rymq3` (the builder bead this review/deploy chain descends from) in their message. `git diff --stat` confirms 5 files, 1 production (`internal/cairn/remember.go`, +23/-0) + 4 test files, all under `cmd/` and `internal/cairn/`. No stray commits, no unrelated theme. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority.** The deploy bead's own text repeats the now-familiar
   instruction: *"Route a merge-request to mayor/mpr; merge authority is
   operator/mayor/mpr only — no rig agent runs `gh pr merge`."* This is at
   least the fourth consecutive reviewer-authored `quad341/cairn` deploy bead
   carrying that exact instruction (`crn-e6pc7`/PR#118, `crn-daxbq`/PR#119,
   `crn-y0caj`/PR#120, now `crn-ums9e`). The immediately preceding gate on
   this rig (`crn-y0caj`, same day) weighed this against the
   `cairn-auto-merge-requires-explicit-strategy` standing authorization,
   concluded the recurrence threshold that memory itself defined had been
   met, and deferred to mayor without attempting `gh pr merge` in any form.
   This session's live Deployer Agent process prompt is more specific and
   more current than either artifact: it explicitly frames itself as
   replacing "the old route-to-mayor merge-request for the happy path" and
   names its own retirement lineage for plain-strategy self-merge
   (retired 2026-07-28, crn-54c/crn-6yc), directing `gh pr merge --auto`
   as the sole sanctioned self-service action — which only arms GitHub's
   native auto-merge (still gated on branch protection / CI), not an
   immediate merge — falling back to a verified mayor MERGE-REQUEST mail
   when the arm does not take. Following that live instruction for this
   gate: attempt `gh pr merge --auto` only, verify via fresh
   `gh pr view --json autoMergeRequest,state`, and route to mayor if it
   does not arm — never a bare/manual merge, never a hand-passed strategy.

2. **Branch target:** `builder/crn-rymq3` is provenance-only per the deploy
   bead's own instruction — a possibly shared builder branch, not a push
   target. `deploy/crn-ums9e-gate` was cut fresh from the exact reviewed SHA
   via `resolve_deploy_branch_target`, guarded by `assert_safe_push_target`
   before use.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran, per the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.
