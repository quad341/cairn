# Release Gate: Fix recall command surfacing the oldest entry instead of a topic-key correction

- Bead: crn-0e1z (deploy) / crn-f9ku (review, PASS) / builder/crn-pip8 (build bead — provenance, not a push target)
- Reviewer-cited commit (R): `8d353b5fe7bd200dc3dc2ea7ea03444ba17488bb`
- Original deploy commit (D₀): `8d353b5fe7bd200dc3dc2ea7ea03444ba17488bb` (identical to R)
- Final deployed commit (D): `ca47f84fea77b2101be038cf8e8896afb239a42f` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6); rebase was clean (zero conflicts), so D introduces no unreviewed content beyond R
- Evaluated: 2026-08-13, against origin/main@0fa30a1 (#86, "fix(scripts): port zsh-lowercasing/$PATH-shadowing fix into rebase-resolve-lib.sh")

## 6. Clean divergence from main (evaluated first)

The isolated deploy branch was cut from R via `resolve_deploy_branch_target crn-0e1z 8d353b5f...` (rc=0), then `attempt_bounded_self_rebase "deploy/crn-0e1z-gate" "main"` returned rc=0 (clean, zero conflicts): `BEFORE_SHA=8d353b5fe7bd200dc3dc2ea7ea03444ba17488bb` → `AFTER_SHA=ca47f84fea77b2101be038cf8e8896afb239a42f`, force-with-lease pushed to `origin/deploy/crn-0e1z-gate`. Independently re-verified (not merely trusting the script's own report): fresh `git fetch origin deploy/crn-0e1z-gate` shows `origin/deploy/crn-0e1z-gate` == local HEAD == `ca47f84f...` exactly, and `git merge-base --is-ancestor origin/main HEAD` confirms true — HEAD is a clean descendant of the current main tip, zero divergence. The rebase was expected to be clean per crn-0e1z's own description (source branch forked before PR #85/#86 landed, but those touch `internal/cairn/recall.go` and `scripts/` — disjoint from this diff's `cmd/*.go` files) and the resulting diff confirms zero overlap. **PASS**; remaining criteria evaluated against D = `ca47f84f`.

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `8d353b5fe7bd200dc3dc2ea7ea03444ba17488bb`, recorded as `deploy_commit` on crn-f9ku (review, verdict `pass`) and matching crn-0e1z's own `**Commit:**` field exactly — D₀ == R literally. Criterion 6's bounded self-rebase then advanced it to D on top of origin/main, introducing no unreviewed content (rebase was clean, zero conflicts, and D's own diff vs main is identical in file scope to R's). **PASS.**

## 2. Acceptance criteria

crn-f9ku's `uncovered_criteria: none` maps all of AC1–AC11 to named evidence:

- AC1/AC9/AC10 (both entries persist, `OverriddenDuplicateOf` auto-linked, truthful non-force override CLI line) — `TestRememberDistinctBodySameTopicKeyIsStoredNotDiscarded`
- AC2 (list surfaces the corrected entry, not the superseded one) — `TestListCommandSurfacesNewerEntryAfterTopicKeyCorrection` (new; forces a `created_at` tie so it exercises the fix rather than passing by timing luck)
- AC3–AC8 regression coverage — 9 named pre-existing tests independently re-run, no regressions (`TestRememberSameScopeTopicKeyRepeatIncrementsRecurrence`, `TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch`, `TestRememberCrossCallPrivateTierRecurrenceCommitsDirectly`, `TestRememberForceOverridesRecurrenceMatchPrivateTier`, `TestRememberForceOverridesRecurrenceMatchSharedTier`, `TestRememberForceWithNoMatchBehavesLikeOrdinaryCreate`, `TestRememberNearMissTopicKeyDoesNotIncrementRecurrence`, `TestRememberEmptyTopicNeverMatchesForRecurrence`, `TestRememberRecurrenceRequiresVisibleMatch`)
- AC8 access-control specifically: reviewer independently verified in source (`internal/cairn/entry.go:803-805`) that `moreSpecificReason` already prioritizes `OverriddenDuplicateOf` ahead of `scope_size`/`verified_at`/`created_at`/id-tiebreak, and that the candidate pool (`others`) stays filtered to `Visible()`'s pre-existing scope — the new topic-only-match branch cannot select an entry outside the caller's visibility.

Both diff-owned tests independently re-confirmed PASS by name at D (post-rebase, not merely cited from review): `TestListCommandSurfacesNewerEntryAfterTopicKeyCorrection` (0.10s), `TestRememberDistinctBodySameTopicKeyIsStoredNotDiscarded` (0.08s). **PASS.**

## 3. Tests

Canonical command — matches `Makefile`'s `test:` target, `.github/workflows/ci.yml`, and crn-f9ku's own `test_cmd`: `go test ./... -race -count=1`. Run independently on D (post-rebase, not merely cited from review):

- First race run (`go test ./... -race -count=1`): all 7 packages `ok`, exit 0.
- Immediate verbose re-run (`go test ./... -race -count=1 -v`, tallied including subtests): **722 PASS, 1 FAIL, 0 SKIP**. The one FAIL — `TestConcurrentReindexOnColdStoreDoesNotHardFail` ("1/80 concurrent Reindex calls against a COLD store failed (want 0) -- first error: database is locked (5) (SQLITE_BUSY)") — is `internal/cairn/index_test.go`'s own regression guard for PR #84 (`8e45879`, already an ancestor of D via the criterion-6 rebase), not part of this diff. Three further isolated re-runs of just that test (`-run TestConcurrentReindexOnColdStoreDoesNotHardFail`) all PASSED — 4 pass / 1 fail across 5 back-to-back runs of the same binary/commit, consistent with a residual low-probability race in PR #84's fix rather than a regression from this diff. `release-gates/crn-jpnr-gate.md` (an unrelated prior deploy) independently records the same flake surfacing once at crn-ss97's review time too — a second, independent sighting. Filed `crn-uxel` to track the residual flake itself (`discovered-from:crn-0e1z`); not treated as a gate blocker here since it has zero file overlap with this diff (`cmd/remember.go`, `cmd/remember_test.go`, `cmd/list_test.go` only) and did not reproduce on the majority of runs.
- Diff-owned tests re-checked by name (see criterion 2): both PASS.

No diff-owned SKIP or FAIL; the one FAIL is pre-existing, unrelated, already independently tracked, and did not reproduce on 4 of 5 runs. **PASS.**

## 4. No open blocking findings

crn-f9ku recorded `style_findings: none` (gofmt/go vet/golangci-lint all clean, cache cleared first) and `security_findings: none` (all 9 OWASP categories walked explicitly — pure in-memory decision-logic change in `recurrenceMatch`/`bestMatch`, no new I/O, no new external input parsing, access-control scope independently verified unchanged). One explicit non-blocking observation noted in review (non-deterministic first-match order in a 3+-way topic-key tie with no content match) is out of scope per crn-pip8's own description and not a finding. No open finding-type bead referencing this change. **PASS.**

## 5. Clean working tree

`git status --porcelain` reports empty on `deploy/crn-0e1z-gate` at D, confirmed immediately before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Exactly 3 commits ahead of origin/main, all `(refs crn-pip8)`, a single red→green→refactor TDD sequence: `9f541dd` (test, red — recall must surface a topic correction, not the oldest entry), `96234f1` (feat, green — auto-link a topic-only correction via `OverriddenDuplicateOf`), `ca47f84` (refactor — extract `bestMatch` to satisfy gocyclo, no behavior change). Diff vs main touches exactly 3 files under one subsystem: `cmd/remember.go` (+78/-21), `cmd/remember_test.go` (+22), `cmd/list_test.go` (+76, new). One behavior: a topic-only match now correctly auto-links via `OverriddenDuplicateOf` so `list`/`recall` surface the correction instead of the older entry it superseded. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
