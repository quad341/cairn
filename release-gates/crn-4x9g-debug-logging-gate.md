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
