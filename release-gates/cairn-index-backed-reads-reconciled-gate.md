# Release Gate: cairn-index-backed-reads-reconciled

**Bead:** crn-2xpm (deploy, tip of stack) — combined stack: crn-sj5r, crn-faj6,
crn-m9kv, crn-32j0, crn-3cmj, crn-ylpn, crn-2xpm — plus a builder
reconciliation against `origin/main`'s independently-landed PR #23
(crn-ln1/crn-rbjm scope-mismatch diagnostic) and PR #39 (crn-0yv.1
stale-branch review helpers)
**Commit:** `cc6ceb25ecb0493a0a1e6f9a346eefad8507e525` (merge commit on
`builder/crn-2xpm-reconcile`), cut onto `deploy/crn-2xpm-gate`
**Date:** 2026-07-23

## Background

This bead's stack (tip originally `ac9cff9`) previously earned a combined
7-criterion PASS in isolation, but `origin/main` had since landed PR #23,
which added a `scopeMismatchWarnings` diagnostic to the *old*
`IterEntries`-based `Visible`/`Prime` — colliding with this stack's own
rewrite of those same functions to be index-backed with a new leading
`context.Context` parameter. That was a genuine two-sided architecture
conflict (confirmed via `git merge-tree` CONFLICT + `attempt_bounded_self_rebase`
rc=12), routed to `cairn/builder` rather than hand-resolved. Builder
reconciled by sourcing both `Visible` and `Prime`'s diagnostic from the
stack's own `Status(ctx, store)`, renaming (not merging) `splitFrontmatter`/
`tomlQuote`/`ReviewBranch`/`ListReviewBranches` collisions that surfaced
against two more independently-landed PRs (#24, #39) along the way, and
handed off to `cairn/reviewer`, who independently re-reviewed the full
reconciliation delta and returned VERDICT: PASS pinned to `cc6ceb2`. This
gate resumes arming per that verdict's explicit routing back to deployer.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Re-fetched `origin/main` immediately before evaluation (tip `f6970f5cf4b8526148d72ed3cd46bccf4d2cb5be`). `git merge-tree --write-tree origin/main cc6ceb2` → exit 0, tree `844ce2bc1b34cd25370a7c6b3d73e83481eb79fd`, no CONFLICT text — identical tree hash to reviewer's own independent check against the same tip. No self-rebase needed. |
| 1 | Review PASS present | PASS | `cairn/reviewer` recorded "VERDICT: PASS" on `cc6ceb2` (`builder/crn-2xpm-reconcile`) in crn-2xpm's notes — an explicit fresh re-review of the reconciliation merge commit itself (not a carry-over of the earlier `crn-j3k4.1` PASS, which only covered `ac9cff9`'s narrow entry_tags fix). Confirmed by reading the notes directly in this session. |
| 2 | Acceptance criteria met | PASS | Composed from two independently-verified layers: (a) the pre-reconciliation combined gate PASS already verified all 7 sub-beads' acceptance criteria against tip `ac9cff9` (index-backed `Reindex`/`Find`/`Status`/`Visible`/`Prime`, entry_tags DDL race fix, `_txlock=immediate`, busy_timeout/WAL); (b) reviewer's reconciliation-delta review confirms the delta introduced on top — `Visible`/`Prime` now composing `Status(ctx, store)` + `visibleFrom`/`scopeMismatchWarnings`, ctx threading through `cmd/*`, symbol renames (`splitFrontmatterForPatch`, `ReviewMergeBranch`/`ListReviewMergeBranches`, consolidated `tomlQuote`) — is behaviorally equivalent and correctly scoped, with zero old-signature call sites remaining anywhere in the tree (tree-wide grep) and `go build` as a hard backstop for dangling references. |
| 3 | Tests pass | PASS | Run directly on `cc6ceb2` in this session, not trusting prior reports: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./... -race -count=1` — `ok` on all 4 packages (root, cmd, internal/cairn, internal/critic), `golangci-lint run ./...` → `0 issues` (shared cache cleaned first per known cross-worktree staleness). |
| 4 | No high-severity review findings open | PASS | Reviewer's only recorded finding on `cc6ceb2` is a single non-blocking cosmetic nit (stale test-function names post-rename, e.g. `TestListReviewBranchesEmptyStore` now calling `ListReviewMergeBranches`) — explicitly marked optional/non-gating. No HIGH findings open. |
| 5 | Final branch clean | PASS | `git status --short` clean on `deploy/crn-2xpm-gate` immediately after cut (no modified/staged tracked files); only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` merge-base (`8a914d4`) touches 22 files, all within `cmd/`, `internal/cairn/`, `internal/critic/` (1319 insertions, 110 deletions) — one coherent theme: moving `cairn`'s read path (`Find`/`Visible`/`Prime`/`Status`) from full-store `IterEntries` scans to a SQL index (`index.sqlite`), plus the `internal/critic` scope-precedence/freshness logic built on top of it. The reconciliation delta is entirely in service of preserving PR #23's diagnostic on this new architecture, not a second independent theme. |

## Verdict: PASS — proceeding to PR.

## Note on stale artifacts

PR #36 (`deploy/crn-j1uh-gate`) and the underlying `crn-j1uh`/`crn-ucxp` beads
are an unrelated deploy lineage (cairn review CLI verb) that happened to
collide with this same PR #23/#39 landing window — already independently
resolved and closed (PR #40 merged at `f6970f5`). Not in scope here.
