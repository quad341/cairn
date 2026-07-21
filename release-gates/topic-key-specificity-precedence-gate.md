# Release Gate: topic_key specificity precedence in Visible()

Re-cut as an isolated single-commit deploy (crn-811) to replace the tangled
PR #3, whose head (`gc-builder-6ac3e0f3c1f3`) carried already-merged docs
content and `.beads/*`/bd-init alongside the feature commit. This branch
(`deploy/crn-1qb-topickey`) is `origin/main` + exactly one cherry-picked
commit.

Deploy source: `beaaee191672ce25b408ffd155bc6bb6c010dc3a` (feat(cairn):
implement topic_key specificity precedence in Visible()), cherry-picked
onto `origin/main` @ `33f1fc1072a2ca130cd33f1415ce3b07206ae71c`.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present | PASS | Reviewed + PASSED by cairn/reviewer; full findings and FR-1..FR-6 verification on crn-ki1 notes, recorded in crn-1qb. |
| 2 | Acceptance criteria met | PASS | crn-9l6 AC: entries sharing a topic_key collapse to the most specific; deterministic tiebreak (VerifiedAt > CreatedAt > ID). Covered by 4 tests: TestVisibleShadowsBySpecificity, TestVisibleShadowTiebreakVerifiedAt, TestVisibleShadowTiebreakID, TestVisibleUntopicedNeverShadow. |
| 3 | Tests pass | PASS | `go build ./...` clean. `go test ./...` — `cmd` and `internal/cairn` packages OK, all green, no regressions. |
| 4 | No high-severity findings open | PASS | No open HIGH findings recorded against this commit (crn-1qb/crn-ki1). |
| 5 | Final branch is clean | PASS | `git status` clean on `deploy/crn-1qb-topickey` (untracked rig-scaffold files only, unrelated). |
| 6 | Branch diverges cleanly from main | PASS | Branch is `origin/main` (33f1fc10) + exactly 1 cherry-picked commit; cherry-pick applied with zero conflicts. |
| 7 | Single feature theme | PASS | Exactly one commit, scoped to `internal/cairn/entry.go` + `internal/cairn/entry_test.go` only (verified via `git show --stat`). No unrelated files. |

Additional checks:
- `gofmt -l .` — clean, no files listed.
- `golangci-lint run ./...` — 0 issues.

## Provenance note (sequencing)

ShadowMap (crn-eat / crn-4bd.2.1, commits `cc5bca214` + `6f96e9f8e`) calls
`moreSpecific()`, which this commit defines. The ShadowMap re-cut
(`deploy/crn-eat-shadowmap`, tracked under crn-811) cannot be built until
this PR merges to `main` — it is sequenced strictly after this one.

## Disposition

PR opened from `deploy/crn-1qb-topickey` onto `main`, replacing the
tangled PR #3 (closed as superseded — same feature commit, but bundled
with already-merged docs and `.beads/*`/bd-init noise). Merge authority
for this PR is mayor (per crn-811), not this deployer run.
