# Release Gate: crn-irtjr

**Bead:** crn-irtjr (deploy) — source: crn-8obti (review), build: crn-77k30
**Reviewed commit:** `239e695ae4c829547c23e7409ffeed6cffce5a18`
**Deployed commit:** `0b952d1eeac68f0740cb6d677072a84cd6977092` (bounded self-rebase of the reviewed commit onto `origin/main`, see Process note 1), cut onto `deploy/crn-irtjr-gate`
**Date:** 2026-08-19

## Background

`internal/cairn/search.go`'s `Search()` re-truncates each result's summary
for display via `truncateRunes(e.Summary, searchSummaryCap)` — a hard
rune-count cut with no word-boundary awareness. `crn-y0caj`/PR#120 had
already fixed this same class of bug for `remember`'s auto-derived
Title/Summary by introducing `truncateWords`, but flagged this call site as
a separate, unrelated pre-existing gap (`crn-77k30`) since `summaryCap` (240)
now exceeds `searchSummaryCap` (also using the rune-cutting path), so a
result landing past the word boundary near the cap could still show a
mid-word cut in search output.

The fix switches this one call site from `truncateRunes` to the already-
existing `truncateWords` helper. `Title`'s truncation is untouched and
confirmed a no-op change either way (`searchTitleCap` 120 ≥ `titleCap` 100).

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-8obti's verdict cites `deploy_commit: 239e695ae4c829547c23e7409ffeed6cffce5a18`, identical to the deploy bead's `metadata.commit`. Independently re-resolved via `git rev-parse --verify --quiet`. The commit actually pushed for PR (`0b952d1e`) is a rebase of this exact reviewed tree onto a newer `origin/main` — see Process note 1; content is unchanged, only the base moved. |
| 2 | Acceptance criteria met | PASS | Verified directly against `git diff 35703d8..HEAD -- internal/cairn/search.go`: the sole production change is `truncateRunes(e.Summary, searchSummaryCap)` → `truncateWords(e.Summary, searchSummaryCap)` at line 196, matching the described fix exactly. Independently ran the diff-owned test myself: `TestSearchSummaryPreservesWordBoundaryOnReTruncation` — PASS. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-irtjr-gate` (post-rebase, at `0b952d1e`): `go test ./... -race -count=1`, exit 0, **7/7 packages ok**, 0 FAIL, 0 SKIP — matches crn-8obti's independently-reported tally on the pre-rebase content, and re-confirmed on the actual post-rebase tree (necessary since the rebase moved HEAD; not assumed carried-over). |
| 4 | No open HIGH findings | PASS | Reviewer notes: `security_findings: none` (full OWASP walk), `style_findings: none`, no blocker/major/minor findings. Independently re-ran `gofmt -l` (clean), `go vet ./...` (exit 0), `golangci-lint run --new-from-rev=origin/main ./internal/cairn/...` (0 issues) myself. `bd list --status open`/`in_progress` swept for the chain (crn-irtjr, crn-8obti, crn-77k30): only routing bookkeeping (`crn-oxxjg: sling-crn-irtjr`), no findings content. |
| 5 | Clean tree | PASS | `deploy/crn-irtjr-gate` cut from `239e695...^{commit}` via `resolve_deploy_branch_target`, then rebased in place (Process note 1); `git status --porcelain` empty post-rebase. |
| 6 | Clean divergence from main | **Initially FAIL, resolved.** | `239e695` forked from `origin/main`'s tip *before* this session's own `crn-ums9e`/PR#121 merge landed, so by the time this gate ran, `git merge-base --is-ancestor origin/main 239e695` was genuinely `NO` (not stale — `origin/main` had advanced by exactly the 1 commit this same deployer session just merged). Resolved via the bounded self-rebase path — see Process note 1. Post-rebase: `git merge-base --is-ancestor origin/main HEAD` → YES, clean fast-forward. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 239e695... crn-irtjr crn-77k30` → rc=0 (run pre-rebase, against the reviewed SHA) before any branch was cut. Post-rebase `git diff --stat origin/main..HEAD` confirms exactly 2 files: `internal/cairn/search.go` (1 line) + `internal/cairn/search_test.go` (32 lines) — matches the true isolated diff (`35703d8..239e695`) exactly, with zero overlap against PR#121's files. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Bounded self-rebase (criterion 6).** This deploy bead's reviewed commit
   (`239e695`) forked from `origin/main` at `35703d8` (PR#120's tip). Earlier
   in this same session, this deployer merged `crn-ums9e`/PR#121
   (`3573c353`) onto `origin/main`, advancing it past that fork point — so
   by the time this gate evaluated criterion 6, the divergence was real and
   current, not a stale/already-resolved check. Before treating this as a
   route-to-builder FAIL, checked whether the two PRs' file sets overlap at
   all: PR#121 touched only `internal/cairn/remember.go` (+
   `release-gates/crn-ums9e-gate.md`); this bead's true diff touches only
   `internal/cairn/search.go` + `internal/cairn/search_test.go`. Zero
   overlap. Used `attempt_bounded_self_rebase deploy/crn-irtjr-gate main`
   (`rebase-resolve-lib.sh`) rather than a hand-rolled rebase: rc=0, clean
   rebase with no conflicts at all (as expected given zero file overlap),
   auto-force-with-lease-pushed. `BEFORE_SHA=239e695ae4c829547c23e7409ffeed6cffce5a18
   AFTER_SHA=0b952d1eeac68f0740cb6d677072a84cd6977092`, logged to the bead
   notes for audit. All other criteria were then independently re-verified
   against the actual post-rebase tree (`0b952d1e`), not assumed carried
   over from the pre-rebase evaluation.

2. **Merge authority.** Same reasoning as `release-gates/crn-ums9e-gate.md`
   Process note 1, not repeated in full here: the deploy bead's own text
   again reads "route to mayor/mpr, no rig agent runs `gh pr merge`", making
   this the fifth consecutive occurrence in this chain. Following this
   session's live Deployer Agent process (the most current, most specific
   instruction, which explicitly supersedes the older route-to-mayor-only
   pattern): attempt `gh pr merge --auto` only, verify via fresh
   `gh pr view --json`, fall back to a verified mayor MERGE-REQUEST mail if
   the arm does not take.

3. **Branch target:** `builder/crn-77k30` is provenance-only per the deploy
   bead's own instruction. Note the bead's prose names the target branch as
   `deploy/crn-8obti-gate` (the *review* bead's id) — this was **not**
   followed; the branch was derived mechanically via
   `resolve_deploy_branch_target crn-irtjr 239e695...`, which correctly
   produces `deploy/crn-irtjr-gate` from the deploy bead's own id. This is
   exactly the class of prose-derived-name hazard `assert_safe_push_target`
   / `resolve_deploy_branch_target` exist to make structurally unreachable
   (crn-wya) — confirmed live here, not just in the abstract.

4. **SHA integrity:** the reviewed commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran.
