# Release Gate: cairn-entry-recall-promotion-schema

**Bead:** crn-n2cf (deploy) — source implementation crn-28ge.1.1 (parent crn-28ge.1)
**Commit:** `7bbd58cd15713d9996a6901f31862cf14858aed6` on `deploy/crn-n2cf-gate`, constructed fresh off `origin/main` and evaluated in the deployer's own worktree
**Date:** 2026-07-23

## Verification method note

`builder/crn-28ge.1.1`'s tip SHA is unstable — known harness bug crn-6qbb causes
session-start automation to periodically replay already-merged main-line commits
on top of the bead's own commits, breaking normal ancestry (`origin/main` is not
an ancestor of that branch) while leaving file content fully compatible. Per the
bead's explicit instructions, this deploy does not pin to that branch's SHA.
Instead:

1. Content was verified by two-dot diff signature: `git diff origin/main
   builder/crn-28ge.1.1 --stat` matched the bead's expected signature exactly
   (4 files, 230 insertions(+), 6 deletions(-), same per-file line counts).
2. A clean merge was proven non-destructively before touching anything:
   `git merge-tree --write-tree origin/main builder/crn-28ge.1.1` completed
   with no conflicts.
3. `deploy/crn-n2cf-gate` was then constructed fresh from `origin/main`
   (`git checkout -B deploy/crn-n2cf-gate origin/main`), with only the 4
   reviewed files' content copied in from the builder tip
   (`git checkout builder/crn-28ge.1.1 -- <paths>`) and committed directly —
   producing real, clean ancestry (`origin/main` is an ancestor of this
   commit) instead of carrying over the builder branch's broken topology.
   This avoids a misleading ~10-file/806-line three-dot PR diff that a raw
   SHA-checkout of the builder tip would otherwise have produced.

The resulting commit `7bbd58c` is under the deployer's own control and is not
subject to the churn affecting `builder/crn-28ge.1.1` — it is stable and safe
to reference directly.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git merge-base HEAD origin/main` == `git rev-parse origin/main` (`15b85b98`) exactly — HEAD is origin/main plus exactly one commit, zero rebase needed, real ancestry (not just content-compatible). |
| 1 | Review PASS present | PASS | crn-28ge.1.1's notes record a full "REVIEW VERDICT: PASS" from cairn/reviewer (gates, scope verification, security walk, spec compliance, coverage check all documented) against this exact content, independently confirmed via content-signature match (see above) since the reviewed branch's SHA itself is unstable. |
| 2 | Acceptance criteria met | PASS | Diff confirms all 5 fields added to `Entry` in `internal/cairn/entry.go`: `Kind`, `AutoActionable`, `RecurrenceCount`, `PromotedBeadID`, `LastRecalledAt`, each correctly tagged (`omitempty`/`omitzero` as appropriate). Matching SQLite columns and forward-migration entries added in `internal/cairn/index.go`; `reindexTx` correctly converts `AutoActionable` bool→int, extends the INSERT to 20 columns/20 placeholders/20 args (parameterized, no injection surface), and deliberately excludes all 5 new columns from `ON CONFLICT(id) DO UPDATE SET` — mirroring the existing `hit_count` precedent so index-only state (recall timestamps, recurrence counts, promotion links) survives reindex without being clobbered by stale body values. Independently re-read both files myself; matches reviewer's notes exactly. |
| 3 | Tests pass | PASS | On commit `7bbd58c`, in this worktree: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./internal/cairn/... -count=1` ok (16.9s), `go test ./... -race -count=1` ok across all 5 packages (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `scripts`). `golangci-lint run ./...` → 0 issues (shared cache cleared first). New tests confirmed present and passing: `TestParseEntryNewFieldsZeroValues`, `TestParseEntryNewFieldsRoundTrip`, `TestEntryMarshalRoundTripsNewFields`, `TestEntryMarshalOmitsZeroValueNewFields` (entry_test.go, +72 lines); `TestReindexPreservesNewIndexOnlyFields`, `TestReindexPopulatesNewFieldColumns`, `TestReindexMigratesLegacyIndexSchemaNewFields` (index_test.go, +128 lines). |
| 4 | No high-severity review findings open | PASS | No HIGH findings recorded against crn-28ge.1.1 or its lineage in bead notes; pure additive schema/struct change, no security-relevant surface (parameterized SQL throughout). |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on the evaluated commit. |
| 7 | Single feature theme | PASS | Diff vs. `origin/main` touches exactly 4 files, all one theme (recall/promotion tracking schema): `internal/cairn/entry.go` (+6), `internal/cairn/entry_test.go` (+72), `internal/cairn/index.go` (+30/-6), `internal/cairn/index_test.go` (+128). 230 insertions(+), 6 deletions(-) total. |

## Verdict: PASS

Proceeding: push `deploy/crn-n2cf-gate` to `origin`, open PR against `main`, run bounded CI check, arm `gh pr merge --auto`.
