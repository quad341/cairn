# Release Gate: crn-8lq2z (round 2)

**Bead:** crn-8lq2z (deploy) — source: crn-osx2p (review), build: crn-evw98.1
**Reviewed commit:** `ef1e3a371495640b6d6ae1947f455043a9a5ae8a` (round-2 reviewer PASS, on `builder/crn-evw98.1`)
**Deployed commit:** `feb62722b40fb4a390eed06b37f7b8b5e0a2c66c` (bounded self-rebase of the reviewed commit onto `origin/main`, see Process note 1), cut onto `deploy/crn-8lq2z-gate`
**Date:** 2026-08-20

## Background

Round 1 of this deploy (source `c3487508df3675ac78ea7ed609e48e6e526269e9`,
reviewed/PASSed under crn-osx2p) passed the local gate 1-6 but failed the
post-push GitHub CI check (criterion 7): golangci-lint flagged 3 diff-owned
issues (gocyclo in `commitToReviewWorktree`, `lll` on a struct literal in
`cmd/list.go`, `nestif` in `RenderPrimeText`). Routed back to builder per
criterion-1 SHA-match discipline. Builder fixed all 3 sites via mechanical
extraction/reflow (`ef1e3a3` on `builder/crn-evw98.1`), independently
re-verified clean (build/vet/race-test/lint), and reviewer round-2 PASSed.
Per the crn-wcv7 recycling convention, the same bead id was bounced
`needs-deploy → needs-review → needs-deploy` rather than spawning a new one.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | Round-2 reviewer verdict (recorded in this bead's own notes, per the recycling convention) cites `ef1e3a371495640b6d6ae1947f455043a9a5ae8a`, matching `metadata.gc.deploy_commit` at PASS time. The commit actually pushed for the PR (`feb62722b`) is a rebase of this exact reviewed tree onto a newer `origin/main` — see Process note 1; `git diff ef1e3a3..feb6272` shows zero change to any file the reviewed diff touched, only files that were already present on `origin/main` independent of this feature (`internal/cairn/search.go`, `internal/cairn/search_test.go`, `release-gates/crn-irtjr-gate.md` — all three confirmed via `git show aa484f2:<path>` to already exist on main, i.e. ordinary main-drift, not injected content). |
| 2 | Acceptance criteria met | PASS | Same reviewed diff, content-identical post-rebase. Reviewer round-2 notes: mechanical extraction/reflow across 3 files, no behavior change, matches commit message exactly; underlying feature acceptance already validated under crn-osx2p round 1. |
| 3 | Tests pass | PASS | Independently re-run by the deployer in an isolated scratch worktree at the actual deployed commit (`feb6272`), not assumed carried over from `ef1e3a3`: `go build ./...` clean, `go vet ./...` clean, `go test -race -count=1 ./...` → ok, all 7 packages (root, cmd, formulas, internal/cairn, internal/critic, internal/obslog, scripts), fresh run. `golangci-lint run ./...` with an isolated `GOLANGCI_LINT_CACHE` → 0 issues. |
| 4 | No open HIGH findings | PASS | Reviewer round-2 notes show clean build/vet/test/lint with no findings text. `bd show crn-8lq2z` comment_count: 0. No HIGH-tagged findings anywhere in bead notes or metadata. |
| 5 | Clean tree | PASS | Isolated scratch worktree at `feb6272`: `git status --porcelain` empty before and after verification. |
| 6 | Clean divergence from main | **Initially FAIL, resolved.** | `ef1e3a3` forked from an `origin/main` tip that predated PR #122 (`aa484f2`, "cairn search: summary re-truncation..."); by the time this gate re-ran, `git merge-base --is-ancestor origin/main ef1e3a3` was NO. Resolved via bounded self-rebase — see Process note 1. Post-rebase: `git log aa484f2..origin/deploy/crn-8lq2z-gate` shows exactly the 3 reviewed commits (red/green/style), no merge commits, clean linear divergence directly off current `origin/main` tip. |
| 7 | Post-push CI green | PASS | Fresh `gh pr view 123 --json headRefOid,mergeStateStatus,mergeable,statusCheckRollup,state`: `headRefOid` = `feb62722b40fb4a390eed06b37f7b8b5e0a2c66c`, `mergeStateStatus` = CLEAN, `mergeable` = MERGEABLE, `state` = OPEN. Both required checks COMPLETED/SUCCESS: `build-test` (completed 2026-08-20T04:11:20Z), `lint` (completed 2026-08-20T04:10:31Z). |

## Verdict: PASS 7/7 — proceeding to merge.

## Process notes

1. **Bounded self-rebase (criterion 6).** `deploy/crn-8lq2z-gate` was re-cut
   from the round-2 reviewed commit `ef1e3a3`. Criterion 6 failed against
   the then-current `origin/main` (which had advanced past `ef1e3a3`'s fork
   point via PR #122). A bounded self-rebase onto `origin/main` was
   performed: `BEFORE_SHA=ef1e3a371495640b6d6ae1947f455043a9a5ae8a
   AFTER_SHA=feb62722b40fb4a390eed06b37f7b8b5e0a2c66c`, clean replay of the
   3-commit reviewed chain (`dec3ea4` red → `628944b` green → `feb6272`
   style) with no real conflicts, force-with-lease-pushed to
   `origin/deploy/crn-8lq2z-gate`. The rebase and push occurred in an
   earlier turn of this same session that was interrupted (context-cycle
   restart) before the audit note, this gate doc, and the bead's
   `gc.deploy_commit` metadata were updated to reflect it — the bead's
   notes and metadata still showed `ef1e3a3` as the deploy source with no
   mention of the rebase. On resuming, rather than trusting that the
   interrupted work had completed correctly, independently re-verified the
   actual resulting state from first principles: confirmed neither `ef1e3a3`
   nor `feb6272` is an ancestor of the other (as expected — rebase creates
   new commit objects), then confirmed the *tree* diff between them is
   confined to ordinary main-drift files (criterion 1 evidence above), then
   re-ran the full build/vet/test/lint suite fresh against `feb6272` itself
   (criterion 3) rather than assuming the pre-rebase verification still
   applied.

2. **Merge authority.** Per mayor ruling `gm-wisp-2yhv7u` (2026-08-19
   03:01:50, "RULING: deployer self-merge REINSTATED for quad341/cairn —
   mpr does not cover it"), freshly re-peeked and confirmed unsuperseded
   (inbox checked for newer mail) before acting rather than relied on from
   memory: deployer self-merge on quad341/cairn is reinstated, gated on 4
   conditions — (1) gate 7/7 PASS, (2) PR state confirmed by direct
   `gh pr view --json` field read, (3) CLEAN/MERGEABLE with required checks
   COMPLETED/SUCCESS, (4) no `--auto` arm, merge deliberately or not at all.
   All 4 met here. Merging via a direct `gh pr merge --squash`, not `--auto`.

3. **Lease.** This bead's lease had expired (`04:10:15Z`) by the time work
   resumed after the session interruption (`04:28:02Z`). Re-claimed via
   `bd update crn-8lq2z --claim` before taking any further action.

4. **SHA integrity:** both `ef1e3a3` and `feb6272` independently
   re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` before
   use; ancestry and tree-hash comparisons run bidirectionally.
