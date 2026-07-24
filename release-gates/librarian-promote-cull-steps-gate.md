# Release Gate: mol-cairn-librarian PROMOTE + CULL candidate-bead steps

Isolated single-commit deploy for crn-28ge.1.8 (crn-gfd9). Source branch
`gc-builder-769138d1bf3c` is the persistent bare-commit-fallback builder
branch and carries stale duplicate history relative to `origin/main` (most
of its stack is already squash-merged, e.g. PR #48 -> `99d1f96`). Per the
reviewer's explicit merge policy, only the one new commit was cherry-picked
onto a fresh branch off `origin/main` rather than merging the branch
directly.

Deploy source: `58ffab62b82ff870e745c3a1bcd04bf77b5d28dc` (feat(cairn):
green — librarian PROMOTE + CULL candidate beads, refs crn-28ge.1.8),
cherry-picked onto `deploy/crn-28ge.1.8` = `origin/main` @
`99d1f960266573c4557d83a22a1ae8cdc9ec9943` + 1 commit.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit | PASS | crn-ynmf: cairn/reviewer PASS, all 9 checklist items independently re-verified. Reviewer's PASS cites `58ffab6` explicitly; deploy SHA `D` == review SHA `R`. |
| 2 | Acceptance criteria met | PASS | crn-28ge.1.8 AC: two new `[[steps]]` following the existing idempotent-bead-filing shape; PROMOTE files a proposal bead naming each finding's `anchor_repo`; CULL filters to rig/global tier and routes through the review-branch eviction path (never a direct delete); loop step's `needs`/prose updated for five steps. Independently confirmed by reading the full diff — additive only, matches shape of the three existing steps, byte-identical between `mol-cairn-librarian.formula.toml` and `mol-cairn-librarian-rig.formula.toml`. |
| 3 | Tests pass | PASS | `go build ./...` clean. `go vet ./...` clean. `golangci-lint run ./...` — 0 issues (cache cleaned first per known shared-cache staleness gotcha). `go test ./... -race` — all packages green (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `scripts`), including `internal/critic` where the reviewer previously saw a known-flaky, pre-existing, unrelated failure (`TestRunPerfScenarioCleansUpAfterItself`) — passed clean on this run. |
| 4 | No high-severity findings open | PASS | Reviewer recorded 0 open HIGH findings. Item 9 of the review (`Entry.PromotedBeadID` has no writer) is informational only, explicitly out of scope for this bead, tracked separately as crn-ghn8 — not blocking. |
| 5 | Final branch is clean | PASS | `git status` clean on `deploy/crn-28ge.1.8`, ahead of `origin/main` by exactly 1 commit. |
| 6 | Branch diverges cleanly from main | PASS | `deploy/crn-28ge.1.8` = `origin/main` (`99d1f96`, unchanged since the reviewer's own verification) + 1 cherry-picked commit; applied with zero conflicts. |
| 7 | Single feature theme | PASS | TOML-only diff, 2 files (`formulas/mol-cairn-librarian.formula.toml`, `formulas/mol-cairn-librarian-rig.formula.toml`), +362/-6. Both new steps belong to the same mol-cairn-librarian sweep (PROMOTE and CULL candidate-bead filing) — not independent features; removing either step would leave the formula's other steps working. |

## Disposition

PR to be opened from `deploy/crn-28ge.1.8` onto `main`, GitHub-native
auto-merge armed per cairn's scoped deployer merge authority (squash, per
fleet memory `cairn-auto-merge-requires-explicit-strategy` — no merge-queue
ruleset configured on `quad341/cairn`).
