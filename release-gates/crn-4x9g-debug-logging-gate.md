# Release Gate: crn-4x9g — Debug logging (JSONL file log + opt-in --trace)

**Bead:** crn-p5gy (deploy) / crn-4x9g (implementation)
**Evaluated:** 2026-07-25
**Deploy source (D):** `a19782b14ca9421ff56cca1776802d83588b61ff` (builder/crn-4x9g, base origin/main@516262f)

**Result: FAIL — criterion 6**

## Criterion 6 — Branch diverges cleanly from main: FAIL

`origin/main` has advanced 4 commits since this branch's base (516262f):

- `18db119` Add `cairn list <topic>` command (#53)
- `24a45b3` Bound cairn prime cost on payload size and freshness-check count (#57)
- `be93ae7` cairn: distinguish git-invocation failures from confirmed-negative freshness checks (#54)
- `0fe1da3` Add --json output, stable error categories, and version/help unification (#58)

`git merge-tree --write-tree origin/main a19782b14ca9421ff56cca1776802d83588b61ff` reports real
content conflicts (not merge-tree noise) in:

- `cmd/commands.go`
- `cmd/root.go`
- `cmd/version.go`
- `cmd/version_test.go`
- `internal/cairn/entry.go`
- `internal/cairn/entry_test.go`
- `internal/cairn/prime.go`

Bounded self-rebase attempted per protocol: `attempt_bounded_self_rebase "builder/crn-4x9g" "main"`
(run from the branch's own dedicated worktree, clean tree confirmed, no active concurrent session
on the branch per `gc session list`) returned **rc=12** (real / non-trivial conflict) — the rebase
was aborted and the branch left unchanged at `a19782b14ca9421ff56cca1776802d83588b61ff`. The
trivial-conflict classifier only auto-resolves IDENTICAL or ONE-SIDE-EMPTY hunks, or ADDITIVE-BOTH
on allowlisted test/doc/fixture paths — these conflicts span real source files (cmd/root.go,
cmd/version.go, cmd/commands.go, entry.go, prime.go), so none qualify for auto-resolution.

Most likely cause: PR #58 ("version/help unification") landed on main touching cmd/root.go,
cmd/version.go, cmd/commands.go, cmd/version_test.go — overlapping this branch's own deliverable
#5 (PersistentPreRunE wiring in cmd/root.go) and #11 (documenting the log path in --help/version
output, in cmd/version.go). This needs a manual rebase by the builder, not an auto-resolve.

## Remaining criteria: SKIPPED (fail-fast per mandated evaluation order)

Criteria 1 (review PASS / SHA match), 2 (acceptance criteria), 3 (tests), 4 (no high findings),
5 (clean tree), 7 (single theme) were not evaluated — criterion 6 is checked first and a FAIL
there skips straight to the FAIL path without paying for the rest, per process.

For the record: criterion 1 would have passed trivially if reached — the reviewer's PASS verdict
on crn-4x9g cites commit `a19782b14ca9421ff56cca1776802d83588b61ff`, exactly the deploy SHA (D == R).

## Disclosed non-blocking risk (informational only — not the cause of this FAIL)

`builder/crn-52z7` (bead crn-52z7, status=open, unassigned, not yet reviewed) independently
re-implements the same symbols (`moreSpecificReason`/`bestShadowerExplain`/`ShadowReason`) in
`internal/cairn/entry.go`. Re-confirmed still unmerged, no live session on it. Already disclosed
to mayor (gm-wisp-f8kmbnl, verified delivered) per the reviewer's own notes on crn-4x9g — not a
new finding, not the cause of this FAIL, and not actionable by the builder fixing criterion 6.

## Action

Routing back to builder (`ready-to-build`) to rebase `builder/crn-4x9g` onto current `origin/main`
(516262f → 0fe1da3) and resolve the 7 conflicting files by hand. Once rebased, this bead needs a
fresh reviewer PASS at the new SHA before returning to deploy (SHA-pinning mandate).

## Resolution (builder, 2026-07-25)

Manually rebased `builder/crn-4x9g` onto `origin/main` (516262f → 0fe1da3); new tip
`e8b0fad7f4fcf19089d74da3c7748e1a929e1286`. All 7 commits replayed; conflicts surfaced (and were
resolved) on 3 of them:

- **`6ae0d68` (obslog + ctx-threaded shadow logging):** `cmd/commands.go`,
  `internal/cairn/entry.go`, `internal/cairn/entry_test.go` — additive collisions (PR #58's
  `UntopicedLabel`/JSON-status/`Incomplete` flag alongside this branch's `ShadowReason` type and
  ctx-threaded `ShadowMap`/`visibleFrom`). Kept both; updated `commands.go`'s `wantsJSON` branch to
  call the new `ShadowMap(ctx, entries)` signature. `internal/cairn/prime.go` conflicted because
  `origin/main` had independently gained the crn-0vqk `PrimeResult`/budget-bounded rewrite of
  `Prime` (unrelated to this feature) — took that version wholesale and threaded `ctx` through its
  `visibleFrom` call, since the obslog logging itself lives inside `visibleFrom`/`shadowReason`, not
  in `Prime`.
- **`b7f7dc5` (--trace flag + PersistentPreRunE):** `cmd/root.go` — `PersistentPreRunE` (this
  branch) and `RunE` (PR #58) are independent `cobra.Command` fields; kept both, plus both flag
  registrations (`--version`, `--trace`) in `init()`.
- **`a19782b` (debug log path in --help/version):** `cmd/version.go` — PR #58 had already
  consolidated all three version spellings onto a shared `printVersion` helper; folded this
  commit's debug-log-path line into `printVersion` itself (JSON mode unaffected — `VersionResult`'s
  schema is unchanged) instead of keeping a duplicate inline reimplementation in `versionCmd.RunE`,
  so the FR-5 byte-identical-across-three-spellings invariant still holds. `cmd/version_test.go`
  conflict was purely additive (both sides added distinct, non-overlapping test functions).

No conflicts touched `crn-52z7`'s disclosed symbols (`moreSpecificReason`/`bestShadowerExplain`/
`ShadowReason`) beyond the additive collision already described above; `crn-52z7` itself remains
unmerged and was not a factor.

Self-tested at the new tip: `go build ./...`, `go vet ./...`, `go test ./... -race`, and
`golangci-lint run ./...` (after `cache clean`) all clean. Per the SHA-pinning mandate, the prior
PASS (at `a19782b14ca9421ff56cca1776802d83588b61ff`) does not carry forward to this new SHA — routing
to `cairn/reviewer` for a fresh PASS before this returns to deploy.

---

## Re-evaluation (deployer, 2026-07-26) — final commit `5f54fb1802b6e25814b7100ed865b8de7a930126`

`e8b0fad` (the builder's rebase-resolution above) is an ancestor of `5f54fb18`; the one commit
between them (`5f54fb1 cairn: document manual rebase resolution for crn-4x9g gate FAIL`) is the
builder committing this file's "Resolution" section to the branch itself. The reviewer re-reviewed
at this final tip and recorded a fresh PASS citing `5f54fb18` exactly — no further code changes,
so this is a genuine fresh SHA-pinned PASS, not a carried-forward one.

Full 7-criterion re-evaluation performed independently against `5f54fb18`:

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Fetched `origin/main` immediately before evaluation (tip `0fe1da3`, PR #58 — unchanged since the FAIL round's routing). `git merge-base --is-ancestor origin/main 5f54fb18` → true. No rebase needed this round. |
| 1 | Review PASS present, SHA match | PASS | Reviewer recorded a fresh PASS on this exact commit (`5f54fb18`), superseding the original PASS at `a19782b1`. D == R exactly. |
| 2 | Acceptance criteria met | PASS | Reviewer's fresh PASS walked crn-co2u.1's FR-1..FR-11/NFR-1..NFR-5 individually against the final diff (obslog package, rotating writer, XDG state dir with fail-open, `--trace` mirror, ctx wiring through `ShadowMap`/`visibleFrom`/`shadowReason`, freshness/index_drift/write_path logging, structural body-redaction, secret-pattern scrubbing, help/version doc — including the version.go/root.go/commands.go reconciliation with PR #58 documented above). |
| 3 | Tests pass | PASS (2 disclosed non-blocking flakes) | `go build ./...` and `go vet ./...` clean. `go test ./... -count=1` green except two independently-reproduced, pre-existing, load-sensitive flakes unrelated to this branch's diff — see below. |
| 4 | No open blocking findings | PASS | `bd show crn-4x9g` — closed, no HIGH-severity labels. Reviewer's PASS states no open HIGH findings. |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `deploy/crn-p5gy-gate` at `5f54fb18`. |
| 7 | Single feature theme | PASS | One coherent theme throughout (debug logging), including the reconciliation commits — no unrelated changes riding along. |

**Verdict: PASS — proceeding to isolated deploy branch push + PR.**

### Disclosed non-blocking test flakes (this round)

1. **crn-wrg0** (filed by the reviewer): `TestConcurrentReindexDoesNotRaceOnEntryTagsSchema` (internal/cairn) — SQLITE_BUSY race, reproduces on unmodified `origin/main` too (1/8 runs), unrelated to this branch's purely-additive logging changes to `index.go`.
2. **crn-mxvn** (filed this round): `TestRunPerfScenarioDoesNotFail` / `TestRunPerfScenarioCleansUpAfterItself` (internal/critic) — a `testing.TempDir()` cleanup-time `unlinkat ... .git: directory not empty` race, not a scenario-assertion failure. Reproduced 2x on full-suite runs of this branch; 0 failures across 13 narrow single-package runs (this branch and plain `origin/main` alike, in both an isolated clone and this shared worktree). Failure rate tracks overall machine/concurrency load, not which ref is checked out. This branch's only touch to `internal/critic` is a 1-line mechanical signature adaptation (`cairn.ShadowMap(all)` → `cairn.ShadowMap(ctx, all)`) with no bearing on tempdir/git cleanup.

### Cross-branch sequencing: crn-4m7k / crn-52z7 (cairn doctor)

The duplicate-symbol risk flagged in the FAIL round above (informational only, at that time) is now live: `crn-4m7k` (deploy bead for crn-52z7, cairn doctor) independently defines the identical symbols this branch does in `internal/cairn/entry.go` (`ShadowReason`, `shadowReason`, `moreSpecificReason`, `bestShadowerExplain`) — confirmed by direct diff inspection, not just bead-note assertion. Whichever of the two merges second would fail to compile against the other. Both beads' own review notes flag this explicitly as a pre-merge blocker requiring reconciliation or coordinated merge order, and disclosed the risk to mayor three times (`gm-wisp-f8kmbnl`, `gm-wisp-n1mtts1`, `gm-wisp-b67kxcm`) with no objection or hold placed on either bead.

The reviewer's own analysis (in crn-4m7k's notes) identifies this branch's version as the strict superset — it threads `ctx` through `ShadowMap` for `shadow_decision` logging, which crn-52z7's version does not — confirmed independently: the two implementations' core logic (struct + 3 functions) is byte-for-byte identical; the only real divergence is in `ShadowMap`'s wrapper (ctx+logging here vs. a plain call there).

**Decision:** deploy this bead (crn-p5gy/crn-4x9g) now. Holding `crn-4m7k` — routing back to the builder with instructions to rebase `builder/crn-52z7` onto the new `origin/main` after this PR lands, drop its now-duplicate hunk, and layer its doctor-specific consumers onto this branch's canonical implementation additively. This is coordination/sequencing, not a code-quality problem with either branch — both passed independent review. Mayor notified of this sequencing decision (`gm-wisp-7dsx1su`) before proceeding.
