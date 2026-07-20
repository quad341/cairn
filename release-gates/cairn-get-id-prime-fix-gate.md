# Release Gate: cairn-get-id-prime-fix

**Bead:** crn-ri3 (deploy) — source review crn-keq — acceptance criteria crn-0vo
**Commit:** `45b262325b75148a84834af21118d42b43a76f97`, cherry-picked onto `deploy/cairn-get-id-prime-fix` off `origin/main`
**Date:** 2026-07-19

## Note on branch provenance

The builder's shared branch (`gc-builder-6ac3e0f3c1f3`) carries this commit
stacked on top of two unrelated, already-in-flight commits — `beaaee1`
("topic_key specificity precedence in `Visible()`", bead crn-9l6, deploy
bead crn-1qb closed, open PR #3) and `284ea1d` ("status errors on explicit
--identity", bead crn-4bd.1, deploy bead crn-has closed, isolated onto its
own `deploy/status-identity-guard` branch, open PR #4) — and is followed by
a third, later commit, `cc5bca2` ("add ShadowMap, annotate shadowed entries
in status output"), which touches `internal/cairn/entry.go` and
`cmd/commands.go`'s `statusCmd` and was **never reviewed under crn-keq**
(crn-keq's reviewed diff covers only `cmd/commands.go`'s new `getCmd`,
`internal/cairn/prime.go`, and `internal/cairn/prime_test.go`).

Pushing the shared branch as-is would both (a) silently add unrelated,
already-gated work to PR #3 — same hazard the `deploy/status-identity-guard`
gate documented — and (b), worse, ship `cc5bca2` to production without any
review verdict covering it. To keep this deploy's unit of review scoped to
exactly what crn-ri3 / crn-0vo asked for, `45b262325b75148a84834af21118d42b43a76f97`
was cherry-picked in isolation onto a fresh branch cut from `origin/main`
(same pattern as `deploy/status-identity-guard`). The cherry-pick applied
cleanly via git's three-way merge — no conflicts — and a full-tree diff
against the original reviewed commit confirms the result differs from it
**only** by the absence of `284ea1d`'s not-yet-merged `statusCmd` guard
(expected, since this branch doesn't include that commit); the new `getCmd`,
`prime.go`, and `prime_test.go` changes are byte-identical to what crn-keq
reviewed.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Branch cut directly from `origin/main` (`5edc21d`); single cherry-picked commit (`ca39dc9`) on top, `git cherry-pick` auto-merged with zero conflicts. |
| 1 | Review PASS present | PASS | crn-keq notes: "REVIEW VERDICT: PASS" by cairn/reviewer, with AC1..AC8 independently verified against the exact commit `45b2623` (via `git show`/`diff`, not just bead notes) — confirmed byte-identical to this branch's cherry-picked commit (see provenance note above). |
| 2 | Acceptance criteria met | PASS | crn-0vo AC1-8 all verified by the reviewer against code and re-confirmed here since the diff is identical: `getCmd` registered alongside reindex/map/status/freshness/verify; not-found error matches freshness/verify wording exactly; unscoped `cairn.Find` (no `Visible()`/identity filtering); read-only (no `HitCount`/`WriteBack`); `prime.go`'s remember line replaced with a hand-author hint citing `DESIGN.md` §2/§6-7 (checked against the doc — accurate); `prime_test.go` assertion updated to match; `cairn remember` NOT implemented (out of scope by design). |
| 3 | Tests pass | PASS | On `deploy/cairn-get-id-prime-fix` @ `ca39dc9`: `gofmt -l .` clean, `go vet ./...` clean, `go build ./...` clean, `go test ./... -race -count=1` — `internal/cairn` package green (`ok`, 1.091s), no test files in root/`cmd` packages, zero regressions. `golangci-lint run ./...` — 0 issues (the pre-existing `ShadowMap` gocognit finding the reviewer flagged does not appear here because this branch, unlike the shared builder branch, does not include `beaaee1`/`cc5bca2`'s `entry.go` changes — consistent with that finding being out-of-scope and tracked separately under crn-dgh). |
| 4 | No high-severity findings open | PASS | crn-keq notes: no security findings (pure read path mirroring existing `Find` precedent, no new I/O or trust boundary). Two non-blocking nits (missing `docs/` prefix in the DESIGN.md citation; `DESIGN.md` §8's CLI list not updated) — both cosmetic/doc staleness, not defects. Coverage gap for `getCmd` has a filed, in-progress follow-up (crn-yhc) rather than being silently skipped. |
| 5 | Final branch is clean | PASS | `git status --short` on the deploy branch: clean except the pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) that predates this deploy and isn't part of this commit. |
| 7 | Single feature theme | PASS | Isolated commit touches exactly `cmd/commands.go` (+`getCmd`), `internal/cairn/prime.go`, `internal/cairn/prime_test.go` — one subsystem (a new read-only lookup command plus the prime-output text fix that only makes sense once that command exists). No unrelated changes bundled — see branch provenance note above. |

## Verdict: PASS — proceeding to PR.
