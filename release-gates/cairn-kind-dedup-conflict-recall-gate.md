# Release Gate: kind/auto-actionable flags, dedup conflict primitive, recall stamping + reporting

**Bead:** crn-c13d (deploy) — covers crn-28ge.1.2, crn-28ge.1.3, crn-28ge.1.5, crn-28ge.1.6
**Source:** shared builder branch `gc-builder-769138d1bf3c`, reviewed tip `6b6d940` (`6b6d9408f3e4c3f7c5c6cb0def7219414898dd30`)
**Deploy branch:** `deploy/crn-c13d-gate`, cut from exactly the reviewed SHA above
**Base:** `origin/main` @ `b4619c7b7af7c24b83bb0476c438a6ce16ed31b2`

Note: crn-28ge.1.6 landed on this shared branch (tip moved `3591479` ->
`6b6d940`) after this deploy bead — and its PR — were first opened against
the earlier tip. Caught before arming auto-merge; the deploy branch was
re-cut from the corrected tip and this gate re-evaluated in full against
`6b6d940` (not just the delta) before proceeding. See PR #46 for the
updated history.

## Criterion 6 — Branch diverges cleanly from main (checked first)

`git merge-base origin/main 6b6d940` == `b4619c7` == current `origin/main`.
The reviewed commit is already a strict fast-forward descendant of
`origin/main` — no rebase needed. **PASS.**

## Criterion 1 — Review PASS present for the deployed commit (SHA match)

All four sub-beads' reviewer PASS verdicts were evaluated on this exact
branch/tip (crn-28ge.1.2/.1.3/.1.5 individually at their own tips, all
ancestors of `6b6d940`; crn-28ge.1.6 explicitly "run on branch
gc-builder-769138d1bf3c tip 6b6d940, isolated detached worktree, covering
3591479..6b6d940"). R (reviewed SHA) = D (deploy SHA) = `6b6d940`. **PASS.**

## Criterion 2 — Acceptance criteria met

- **crn-28ge.1.2** — `--kind`/`--auto-actionable` flags on `cairn review
  merge`, gated so auto-actionable requires effective kind == remediation;
  private-tier entries confirmed to have no code path through this command
  (two independent enforcement points verified by reviewer). AC met.
- **crn-28ge.1.3** — `pairSignals()` extracted as the single shared
  dedup/conflict primitive (NFR-05), used by both the existing whole-store
  scan and the new exported `Conflicts()`; `cairn get` surfaces
  kind/auto_actionable/conflicts; all 12 pre-existing dedup tests pass
  unmodified (byte-identical `cairn dedup` output preserved). AC met.
- **crn-28ge.1.5** — `Find()`'s existing `hit_count` UPDATE...RETURNING
  transaction extended to also stamp `last_recalled_at`, same transaction,
  same scoping (get/freshness/verify only; map/prime untouched, confirmed by
  dedicated negative tests). AC met.
- **crn-28ge.1.6** — new read-only `cairn recall-stats` (per-entry
  HitCount/LastRecalledAt) and `cairn promote-candidates`
  (RecurrenceCount >= configurable `--threshold`, default 3, AND
  PromotedBeadID=="", each finding carrying Anchor.Repo) subcommands, built
  on a single shared `loadEntryRecallRows` query (NFR-05); shape mirrors
  `cmd/dedup.go` (flags + JSON findings, zero side effects) rather than
  `cmd/branches.go` (which mails reviewers / persists notify-state) per this
  bead's own "strictly read-only" AC — deviation independently verified by
  reviewer against the actual `branches.go` source, not just builder's
  claim. AC met.

**PASS** (per-child, evidence above from each bead's reviewer notes).

## Criterion 3 — Tests pass

Independently re-run in an isolated detached worktree at the corrected tip
`6b6d940` (not trusting the reviewer's report alone, and re-run in full
rather than just diffed against the earlier partial check at `3591479`):

- `gofmt -l .`: clean
- `go vet ./...`: clean
- `go build ./...`: clean
- `golangci-lint run ./...` (private cache, cleared first): 0 issues. First
  attempt hit "parallel golangci-lint is running" from a concurrent agent
  session sharing the build cache in this environment; resolved on retry
  with no code changes involved — transient environmental contention, not a
  gate finding.
- `go test ./...`: all 5 packages ok, **including** `internal/critic`
  (no repeat of the `TestRunPerfScenarioDoesNotFail` TempDir-cleanup flake
  seen during the earlier partial evaluation at `3591479` — consistent with
  that being a pre-existing, non-deterministic, out-of-diff-scope flake
  rather than a regression; `internal/critic` remains untouched by this
  diff, confirmed again below).
- `go test ./internal/cairn/... -race -count=1`: ok (12.338s)
- Targeted new-feature tests (`TestRecallStatsReportsHitCountAndLastRecalledAt`,
  `TestPromoteCandidatesFiltersByThresholdAndIdempotency`,
  `TestPromoteCandidatesThresholdConfigurable`): all PASS individually with
  `-v`.

**PASS.**

## Criterion 4 — No high-severity review findings open

No HIGH findings recorded in any of the four sub-beads' reviewer notes; all
four closed with clean PASS verdicts. **PASS.**

## Criterion 5 — Final branch clean

`git status --porcelain` on `deploy/crn-c13d-gate` shows no tracked changes
(only pre-existing untracked sibling worktree directories from unrelated
concurrent deployer sessions, not part of this branch). **PASS.**

## Criterion 7 — Single feature theme

All four sub-beads are children of the same parent epic (crn-28ge.1,
"Build CAPTURE/AUTO-ACT/PROMOTE/CULL cairn extensions") and form one
cohesive theme: entry lifecycle metadata surfaced and reported through the
cairn CLI — reviewer-granted kind/auto-actionable flags, the dedup/conflict
signal that gates auto-act, recall-time tracking (`last_recalled_at`)
alongside the pre-existing `hit_count`, and read-only reporting
(`recall-stats`, `promote-candidates`) built directly on that same tracking
data. All four touch the same adjacent subsystems (`cmd/review.go`,
`cmd/commands.go`, `cmd/recall.go`, `internal/cairn/{dedup,entry,recall}.go`)
and none is independently shippable without the others losing context — the
reporting commands in .1.6 are direct consumers of the `last_recalled_at`
column .1.5 introduces, and both feed the same FR-07/FR-08 promotion-
tracking goal this epic exists to support. **PASS.**

## Verdict: PASS — all 7 criteria green

Diff scope (`git diff origin/main..6b6d940 --stat`): `cmd/commands.go`,
`cmd/commands_test.go`, `cmd/recall.go`, `cmd/review.go`,
`cmd/review_test.go`, `internal/cairn/dedup.go`,
`internal/cairn/dedup_test.go`, `internal/cairn/entry.go`,
`internal/cairn/entry_test.go`, `internal/cairn/recall.go`,
`internal/cairn/recall_test.go`, `internal/cairn/review.go`,
`internal/cairn/review_test.go` (13 files, 989 insertions, 20 deletions) —
exactly matches the scope recorded in the deploy bead.
