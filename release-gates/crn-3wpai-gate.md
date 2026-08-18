# Release Gate: cairn: chunk reindexTx's per-entry upsert loop into independent transactions

- Bead: crn-3wpai (deploy) / crn-ku5dd (review, PASS) / crn-f0rb7.2 (build bead, closed — provenance, not a push target)
- Reviewer-cited commit (R): `f1f00454a0dcd5327d3018991d5378e30f16f7b8`
- Original deploy commit (D₀): `f1f00454a0dcd5327d3018991d5378e30f16f7b8` (identical to R)
- Final evaluated commit (D): `b78d4e04964602aa2d33dd1635d400a06f60f1a4` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6)
- Evaluated: 2026-08-18, against origin/main@`1c2a370` (#116, "Fix: doctor explain silently ignores a malformed identity")

## 6. Clean divergence from main (evaluated first)

Raw check at gate start FAILed: `git merge-tree --write-tree origin/main f1f00454a0dcd5327d3018991d5378e30f16f7b8` → `CONFLICT (content): internal/cairn/index.go`. D₀ was cut from origin/main@2dce629; origin/main had advanced to 1c2a370 (#112–#116) since, at least one of which also touched `internal/cairn/index.go`. Resolved via `attempt_bounded_self_rebase` (`packs/actual/deployer/scripts/rebase-resolve-lib.sh`) on the isolated deploy source branch `builder/crn-f0rb7.2` (a per-bead builder branch, never a contributor fork): rc=0, `BEFORE_SHA=f1f00454a0dcd5327d3018991d5378e30f16f7b8` → `AFTER_SHA=b78d4e04964602aa2d33dd1635d400a06f60f1a4`, trivial conflict in `internal/cairn/index.go` auto-resolved by the ported classifier, force-with-lease pushed. Verified: `git merge-base --is-ancestor origin/main b78d4e0...` → yes (clean). Net diff origin/main..D is scoped identically to D₀'s own diff against its base: 5 files (`internal/cairn/{index,index_test,entry,entry_test,doctor}.go`), 411 insertions/19 deletions, same 2 commits (RED `9f70a2a`, GREEN `b78d4e0` — replayed from R's own `b6e2d7b6`/`f1f00454`). **PASS** (via self-rebase); remaining criteria evaluated against D = `b78d4e0`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `f1f00454a0dcd5327d3018991d5378e30f16f7b8`, recorded as both crn-3wpai's own `Commit:` field and crn-ku5dd's review verdict. D₀ == R exactly. Criterion 6's bounded self-rebase then advanced it to D — a mechanical, auditable rebase, not new authored content: net diff against origin/main is scoped identically to R's own reviewed diff (same 5 files, same 411/-19, same two logical commits). The reviewer's PASS verdict (crn-ku5dd) explicitly examined and approved the same 3 "drive-by comment fix" lines in doctor.go/entry.go/entry_test.go that survived the rebase unchanged. Treated as covering D under the self-rebase exception. **PASS, with a caveat**: this rebase, by construction, exposes D to interaction with main commits R was never evaluated against — and that interaction is not benign (see criterion 3). A fresh review of the post-fix SHA should be expected before a future deploy attempt, regardless of how this criterion is scored here.

## 2. Acceptance criteria

crn-ku5dd's review verdict cross-checks all of crn-f0rb7's exit_contract (items 1, 2, 3, 5, 6) individually, independently re-derived by the reviewer (not taken on the builder's word) — busy-retry-across-chunks, lock-hold bound (48x improvement measured), the torn-chunked-read trade-off re-checked against prime.go's actual ranking comparator, no new external dependency, watermark-update ordering. Diff-owned tests named explicitly and confirmed PASS by the reviewer pre-rebase. **PASS** (on the reviewed content; see criterion 3 for what changed post-rebase).

## 3. Tests — FAIL

`go test ./... -race` on D (`b78d4e0`), run independently in this worktree:

```
--- FAIL: TestRunScopePrecedenceScenarioPasses
--- FAIL: TestRunScopePrecedenceScenarioIsRepeatable
--- FAIL: TestRunScopePrecedenceScenarioCleansUpAfterItself
--- FAIL: TestCheckShadowWinsBySpecificity
--- FAIL: TestCheckShadowTiebreak
FAIL	github.com/quad341/cairn/internal/critic	10.896s
```

All 5 fail identically: `sql: Scan error on column index 2, name "title_source": converting NULL to string is unsupported`. None are diff-owned (they live in `internal/critic`, a package this bead's diff never touches) and none appeared in the reviewer's pre-rebase run (658 PASS/0 FAIL/0 SKIP on R).

Isolation proof (deterministic, 2/2 reruns):
- origin/main alone (`1c2a370`, unmodified, isolated worktree): same 5 tests, all PASS (`go test ./internal/critic/... -run '...' -v` → PASS, 0.333s). Confirms this is **not** pre-existing breakage on current main.
- D (`b78d4e0`) alone, `internal/critic` package only (ruling out cross-package test pollution from the full `./...` run): same 5 tests, all FAIL, identical error. Confirms it's D's own content, deterministic, not a flake.

Root cause (read directly, `internal/cairn/index.go`): this feature adds `reindexUpsertChunkTx` (new, chunked per-batch upsert) and `reindexOnce` now calls it instead of the pre-existing `reindexTx`, which is left in the file but is now dead code (zero call sites anywhere in the repo, confirmed by grep). `reindexTx`'s own `INSERT INTO entries` (index.go:517–534) correctly includes `title_source` and `summary_source` in the column list, VALUES args (`e.TitleSource, e.SummarySource`), and the `ON CONFLICT DO UPDATE SET` clause. `reindexUpsertChunkTx`'s INSERT (same file, the new function) omits both columns entirely from all three places. D₀ was forked from origin/main@2dce629; whatever main commit(s) added/began enforcing `title_source` (likely #114 "Enforce entry content types at write time" or #115 "Extend backfill to classify legacy entries," both landed between 2dce629 and 1c2a370) post-date that fork point — the reviewer's clean run on R was correct for the schema/expectations at the time; this interaction did not exist yet when crn-ku5dd reviewed it. The self-rebase (criterion 6) is what surfaces it now.

Severity beyond the immediate test failures: `ON CONFLICT DO UPDATE SET` is equally missing `title_source=excluded.title_source, summary_source=excluded.summary_source` — so this is not just a NULL-on-first-insert issue, every future `Reindex` call through this path would silently NULL out a previously-good `title_source`/`summary_source` on UPDATE too, for every existing entry. If merged as-is, this is a live data-corruption risk on the real store, not only a test-suite failure.

Suggested fix (for builder, not attempted here — out of scope for the deployer seat): mirror `reindexTx`'s column list/VALUES/UPDATE SET in `reindexUpsertChunkTx` exactly (add `title_source`, `summary_source` in all three places, sourced from `e.TitleSource`/`e.SummarySource`). Separately, now-dead `reindexTx` should probably be deleted in the same pass (builder's call) — flagged as secondary, non-blocking cleanup, not itself the bug.

**FAIL.**

## 4. No open blocking findings

`bd list --status open --label finding` plus a full-DB text search for crn-f0rb7/crn-ku5dd/crn-3wpai cross-referenced against `issue_type=finding`: 0 results. No pre-existing HIGH/blocking finding was on file at gate-start. **PASS** (on pre-existing findings) — moot given criterion 3; this gate evaluation itself surfaced the correctness issue documented above, now being routed forward rather than left as a silent finding.

## 5. Clean working tree

`git status --porcelain` empty on `builder/crn-f0rb7.2` at D, confirmed before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Diff (origin/main..D) is `internal/cairn/{index.go, index_test.go, entry.go, entry_test.go, doctor.go}` only, 411 insertions/19 deletions, two commits (RED `9f70a2a`, GREEN `b78d4e0`). entry.go/entry_test.go/doctor.go changes are comment-only (`reindexTx` → `reindexUpsertChunkTx` renames), already independently confirmed in-scope by the reviewer. One feature, one package. **PASS.**

## Verdict: GATE FAIL (criterion 3) — NOT deployed

No deploy branch was cut; no PR was opened. Routed to cairn/builder for a fix (see bd notes + mail for the actual handoff). A fresh reviewer PASS on whatever new SHA fixes this is expected before the next deploy attempt.
