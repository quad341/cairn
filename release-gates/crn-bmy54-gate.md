# Release Gate: crn-bmy54

**Bead:** crn-bmy54 (deploy) — source: crn-x81gk (review), build: crn-rott9.3 (architect: crn-rott9, child F)
**Reviewed commit:** `d5d7b1e1fb05d32a24e44b166b2252f8b6c363e2` (matches crn-x81gk's verdict and the deploy bead's `metadata.gc.deploy_commit` — see criterion 1)
**Deploy commit:** `a6173d9cc6d729748da07cf93bfb705b14045f2f` — a bounded self-rebase of the reviewed commit onto `origin/main`'s current tip (see criterion 6), cut onto `deploy/crn-bmy54-gate`
**Date:** 2026-08-20

## Background

cairn's `WriteBackBackfill` and `WriteBackRetrievalMetadata` patch an entry's
frontmatter file directly (`writeBackPatched` — pure `os.ReadFile`/
`os.WriteFile`, zero git ops), without ever advancing the store's checked-out
HEAD. Because `ensureFresh`'s staleness check is HEAD-watermark-based, it
never noticed the file-only edit, so `Status` (`cairn prime`/`list`) kept
serving stale title/summary indefinitely until an unrelated `Reindex`
happened to run.

The fix adds an `Entry.store` field (set by `Find`) and a
`syncIndexRetrievalMetadata` method that directly `UPDATE`s the entries
table's `title`/`summary` columns after a successful file patch, called from
both `WriteBackRetrievalMetadata` (always) and `WriteBackBackfill` (when
`updateRetrievalMetadata=true`). `Find`/`recall`/`get` were never affected —
they always re-read the body file fresh — this bug was specific to the
cached index columns `Status` reads from.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-x81gk's verdict cites `d5d7b1e1fb05d32a24e44b166b2252f8b6c363e2`, identical to the deploy bead's `metadata.gc.deploy_commit` — exact SHA match (D = R). Independently re-resolved both sides this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as transcribed strings. |
| 2 | Acceptance criteria met | PASS | Verified directly against crn-rott9.3's full `exit_contract` (5 bullets: live-repro, check-evw98.2-first, close-if-resolved-by-dependency, implement-if-not, record-evidence-either-way). Builder's own notes confirm `evw98.2` was not merged and is mechanistically unrelated, so the "implement fix" branch correctly applied, with live-repro evidence recorded. |
| 3 | Tests pass | PASS | Independently re-run twice: first against the exact reviewed commit `d5d7b1e1` (detached checkout), then again against the actual deploy commit `a6173d9c` after the self-rebase (criterion 6) — not trusting the reviewer's report alone. Both: `go build ./...` exit 0; `go test ./... -race -count=1` — **7/7 packages ok** (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`). Diff-owned tests re-run individually by exact name at `d5d7b1e1`: `TestStatusServesStaleTitleSummaryAfterFileOnlyWriteback` (new) and `TestWriteBackRetrievalMetadataPreservesBody` (modified — trivial ctx-param addition) — both PASS. 3a (attribute failing tests) not applicable, zero failures. 3b (policy/lint lane, per `.github/workflows/ci.yml`'s `lint` job), re-run at both SHAs with an isolated `GOLANGCI_LINT_CACHE` (avoiding known cross-session cache contamination on this machine): `golangci-lint run ./...` — 0 issues; `go vet ./...` exit 0; `golangci-lint fmt -d ./...` exit 0, no diff. |
| 4 | No open HIGH findings | PASS | crn-x81gk records exactly 1 finding, `[low, non-blocking]`. No open HIGH/blocking finding anywhere in the chain. |
| 5 | Clean tree | PASS | Verification performed via direct checkout of both `d5d7b1e1fb05d32a24e44b166b2252f8b6c363e2^{commit}` and, after the self-rebase, `a6173d9cc6d729748da07cf93bfb705b14045f2f^{commit}`; `git status --porcelain` empty throughout both. |
| 6 | Clean divergence from main | PASS | `git merge-tree --write-tree origin/main d5d7b1e1fb05d32a24e44b166b2252f8b6c363e2` → clean merge tree `68068d4e84afd929622533a2b8604e4204afb92d`, exit 0, zero conflicts. `d5d7b1e1`'s own history predates several now-merged `origin/main` commits (#125–#130), so per this formula's push-and-pr step a **bounded self-rebase onto `origin/main`** was performed, producing AFTER_SHA `a6173d9cc6d729748da07cf93bfb705b14045f2f`. Verified byte-identical in content to the precomputed clean-merge tree: `git rev-parse a6173d9c^{tree}` → `68068d4e84afd929622533a2b8604e4204afb92d`, exact match — confirms the rebase applied with zero fuzzy/conflicted resolution (rebase and merge onto the same target necessarily agree when conflict-free). Also confirmed `origin/main` is a strict ancestor of `a6173d9c` (no gap) via `git merge-base --is-ancestor origin/main a6173d9c`. See Process note 4 for how this was rediscovered and re-verified this session. |
| 7 | Single feature theme | PASS | `git diff --name-only origin/main a6173d9cc6d729748da07cf93bfb705b14045f2f` (post-rebase) → exactly the same 4 tightly-related files as pre-rebase (`cmd/backfill.go`, `cmd/backfill_test.go`, `internal/cairn/entry.go`, `internal/cairn/index_test.go`), +118/-8 lines — 2 source + 2 test, no unrelated changes. `assert_deploy_ancestry_scope crn-rott9.3 main <these 4 paths>` exit 0 — both commits in range cite the build bead `crn-rott9.3`, no out-of-scope path. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority — self-merge authorized under the reinstated standing
   authorization.** crn-bmy54's own notes carry the same generic "PR-based,
   mayor-routed ... route the merge request to mayor" boilerplate seen on the
   three recently-flagged reviewer-authored beads (`crn-e6pc7`/PR#118,
   `crn-daxbq`/PR#119, `crn-y0caj`/PR#120). That recurrence pattern triggered
   a temporary SUPERSEDED state on the fleet's
   `cairn-auto-merge-requires-explicit-strategy` standing authorization on
   2026-08-19 (see `release-gates/crn-y0caj-gate.md`, which paused and routed
   to mayor under that state) — but a fresh, explicit mayor ruling later the
   same day (msg `gm-wisp-2yhv7u`, 2026-08-19 03:01:50) **REINSTATED**
   deployer self-merge for quad341/cairn, naming and clearing all three PRs
   directly. Root cause per mayor: quad341/cairn is not covered by `mpr`
   (maintainer-pr-review) at all — the reviewer-authored "route through mpr"
   boilerplate is a structural miscitation for this repo; the real choices
   here were always deployer self-merge / mayor / operator. Mayor's ruling
   explicitly instructs future sessions **not** to re-pause on a bare repeat
   of this same boilerplate — only on a genuine, bead-specific objection
   distinct from the recycled clause. crn-bmy54's notes raise nothing beyond
   the recycled clause (the "do NOT push to `builder/crn-rott9.3` directly"
   instruction is branch-target guidance, addressed in note 2 below, not a
   merge-authority objection). Proceeding under the STANDING AUTHORIZATION
   per mayor's explicit instruction, subject to the reinstatement's four
   conditions: (1) gate 7/7 PASS — this table; (2) PR state confirmed via a
   direct, fresh `gh pr view --json` read at merge time, not reused/stale
   data; (3) CLEAN/MERGEABLE with both required checks (`build-test`,
   `lint`) COMPLETED/SUCCESS; (4) no `--auto` arm — plain
   `gh pr merge <n> --squash` only, then independently verified via a fresh
   re-read (`state=MERGED`, `mergedAt` non-null) per the FR-03 discipline,
   never trusting a bare exit 0.

2. **Branch target:** `builder/crn-rott9.3` is provenance-only per the
   deploy bead's own instruction — a possibly shared builder branch, not a
   push target. `deploy/crn-bmy54-gate` was cut fresh from the exact
   reviewed SHA (`d5d7b1e1fb05d32a24e44b166b2252f8b6c363e2`).

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` on both the D (deploy
   bead metadata) and R (review bead metadata) sides before comparison, per
   the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.

4. **Branch-anomaly investigation.** Before pushing, `git ls-remote origin
   refs/heads/deploy/crn-bmy54-gate` unexpectedly returned `a6173d9c`, not the
   locally-expected `d5d7b1e1`, with no ancestor relationship either
   direction — raising the possibility of foreign/clobbered state per the
   "investigate before overwriting" principle, so no push was attempted until
   this was fully characterized. Root cause, confirmed via `git reflog`: this
   same local worktree had already, earlier this session (07:14:10), run the
   bounded self-rebase referenced in criterion 6 (`rebase (start): checkout
   origin/main` → picked the 3 crn-rott9.3 commits → `rebase (finish):
   returning to refs/heads/deploy/crn-bmy54-gate`, landing on `a6173d9c`) and
   pushed it, before switching to a detached checkout of the pristine
   `d5d7b1e1` (09:35:42) to gather the criterion-3 evidence above — i.e. this
   was this session's own completed work-in-progress, interrupted before `gh
   pr create`, not foreign or unrelated content. Confirmed via three
   independent checks before relying on it: (a) `a6173d9c`'s tree hash
   exactly matches the precomputed clean-merge tree (criterion 6); (b)
   `origin/main` is a strict ancestor of `a6173d9c`, and `a6173d9c`'s diff
   against `origin/main` touches exactly the same 4 files as the reviewed
   commit, same diffstat; (c) `assert_deploy_ancestry_scope` passes clean
   against it. `gh pr list --head deploy/crn-bmy54-gate --state all` returned
   `[]` throughout — no PR had been opened from either SHA. Proceeded by
   resetting the local branch to the already-pushed `a6173d9c` (via
   `resolve_deploy_branch_target`) and building this commit on top of it,
   rather than re-doing the rebase or force-pushing over it.
