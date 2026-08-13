# Release Gate: recall-stats crashes on never-recalled entries (NULL last_recalled_at)

- Bead: crn-imi3 (deploy) / crn-689s (review, PASS) / builder/crn-snba (P1 bug, closed — provenance, not a push target)
- Reviewer-cited commit (R): `5cfcc8a0d651757c8b6d1bfd37b3812d82afd727`
- Original deploy commit (D₀): `5cfcc8a0d651757c8b6d1bfd37b3812d82afd727` (identical to R)
- Final deployed commit (D): `a4321049c066623a5374651708b7c4aa4b6f3db5` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (zero conflicts), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-13, against origin/main@8e45879 (#84, "fix(index): run entriesSchema inside Reindex's tx to fix cold-store SQLITE_BUSY")

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before evaluation. D₀ was 3 commits behind / 2 ahead of origin/main (merge-base at 7614b3e) — STALE. Attempted a bounded self-rebase per Guardrails (own deployer-cut branch, not a contributor fork): `attempt_bounded_self_rebase deploy/crn-imi3-gate main` returned rc=0 (clean rebase, zero conflicts), `BEFORE_SHA=5cfcc8a0d651757c8b6d1bfd37b3812d82afd727` → `AFTER_SHA=a4321049c066623a5374651708b7c4aa4b6f3db5`, force-with-lease pushed by the library function. Before/after SHAs recorded in the bead's notes for audit. **PASS** (via successful self-rebase); remaining criteria evaluated against D = `a432104`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `5cfcc8a0d651757c8b6d1bfd37b3812d82afd727`, recorded as `deploy_commit`/`tdd_green` on crn-689s (review, verdict PASS) and matching crn-imi3's own `**Commit:**` field exactly — D₀ == R literally. The isolated deploy branch (`deploy/crn-imi3-gate`) was cut from R via `resolve_deploy_branch_target`; criterion 6's bounded self-rebase then advanced it to D on top of origin/main. The SHA-match check is against D₀ (the bead's original recorded commit), not the post-rebase tip — the rebase is criterion 6's mechanism for landing already-reviewed work cleanly, not new unreviewed work. D₀ == R exactly. **PASS.**

## 2. Acceptance criteria

crn-snba's 4 acceptance criteria (per crn-689s's `uncovered_criteria: none`), independently re-run by name on D (post-rebase):

1. NULL-tolerance (scan doesn't crash on a NULL `last_recalled_at` column) — `TestRecallReadsTolerateNullIndexColumns` — PASS
2. Shown, not omitted (human-readable stats output surfaces never-recalled entries rather than dropping them) — `TestRecallStatsReportsHitCountAndLastRecalledAt` — PASS
3. `--json` emits `null`, not an omitted key (the diff's core new behavior) — `TestRecallStatsJSONEmitsNullNotOmittedForNeverRecalled` (diff-owned) — PASS
4. Mix of recalled/never-recalled entries in one result set — exercised by both tests above (each constructs a 2-entry mix per the reviewer's evidence)

All 4 independently confirmed by name against D, not merely cited from review. **PASS.**

## 3. Tests

Canonical command — matches both `Makefile`'s `test:` target and `.github/workflows/ci.yml`'s test step, and crn-689s's own `test_cmd`: `go test ./... -race -count=1`. Run independently on D (post-rebase), not merely cited from review:

- First full run: `internal/cairn` reported 1 FAIL — `TestConcurrentReindexOnColdStoreDoesNotHardFail`: "1/80 concurrent Reindex calls against a COLD store failed... database is locked (5) (SQLITE_BUSY)". Not diff-owned — this diff touches only `recall.go`/`recall_test.go`; the failing test lives in `index_test.go`, exercising cold-store index initialization, an unrelated subsystem. Matches an already-tracked, extensively-corroborated issue: **crn-t42e** (P3, OPEN, "concurrent Reindex() calls can hard-fail with SQLITE_BUSY when initializing index.sqlite from a cold/nonexistent store"). Prior deployer/reviewer/builder sessions (crn-jrhz, crn-od2x.3, crn-2sdw) independently hit the same signature on unrelated diffs, confirmed clean in isolation and on plain origin/main, and disposed of it as pre-existing-flake citing the same bead without filing a duplicate.
- Isolation check (same methodology as those prior dispositions): `TestConcurrentReindexOnColdStoreDoesNotHardFail` alone, `-count=5` — 5/5 PASS.
- Second full run (`-json`, tallied): **721 PASS, 0 FAIL, 0 SKIP**, all 7 packages `ok`. (Higher total than crn-689s's reviewer-time count of 563 because D now carries 3 additional origin/main commits' own tests, pulled in by criterion 6's rebase.)
- Diff-owned test re-checked by name (criterion 2 above): `TestRecallStatsJSONEmitsNullNotOmittedForNeverRecalled` — PASS, both in the full run and isolated.

No diff-owned SKIP or FAIL. The one FAIL observed is pre-existing, unrelated to this diff, already tracked, and did not reproduce on 5/5 isolated reruns or a second full run. **PASS**, with the known flake documented rather than hidden — see crn-t42e for the underlying (P3, self-healing, narrow-impact) issue.

## 4. No open blocking findings

crn-689s recorded `security_findings: none` (diff is a pure read-path `MarshalJSON`-style change on an existing read-only struct; no new I/O, no new external input) and `style_findings: none` (`gofmt`, `go vet`, `go build`, `golangci-lint` all clean). Independent `bd search` for finding-type beads referencing crn-snba/crn-689s/crn-imi3: only the source bug (crn-snba, P1, already closed by this fix) and routing/convoy beads (crn-8fr5, crn-hoiw, crn-jhte) — no open finding-type bead of any severity. **PASS.**

## 5. Clean working tree

`git status --short` empty on `deploy/crn-imi3-gate` at D, confirmed after the self-rebase and again immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Exactly 2 commits ahead of the pre-rebase merge-base (`9adc24e` tdd_red, `a432104` tdd_green), touching exactly 2 files total: `internal/cairn/recall.go` (+21/-1) and `internal/cairn/recall_test.go` (+40/-0, new tests only). One subsystem (`internal/cairn`), one behavior (never-recalled entries emit `last_recalled_at: null` instead of omitting the key, in both human-readable and `--json` output). No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
