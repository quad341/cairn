# Release Gate: topic-key-allow-slashes

**Bead:** crn-ozwbm (deploy) — source: crn-x01ko (review), build: crn-kp9rr.1, root: crn-kp9rr
**Commit:** `7ab6321b7e35e21e79084428828da64eb537040d`, cut onto `deploy/crn-x01ko-gate`
**Date:** 2026-08-15

## Background

Implements Option A from crn-kp9rr's architecture ruling: allow slashes in
`topic_key` (matching the store's own established convention, e.g.
`cairn/entry-format`, `architect/cairn-write-back-guardrails`), while keeping
the path-traversal guard on the thing that actually becomes a filesystem
path — the derived entry ID, via a new `flattenTopicKey` (slash → dash) — not
on `topic_key` itself. A new `ValidateTopicKey` splits on `/` and validates
each segment with the existing, unmodified `ValidatePathSegment`.

Applied at three call sites: `cmd/remember.go` (`--topic`),
`internal/cairn/review.go`'s `MergeReviewBranch` (`--topic-key`), and
`cmd/remember_batch.go` (a third site found independently by the builder
during implementation — same theme, not a separate feature). `flattenTopicKey`
is applied at both ID-construction sites in `internal/cairn/remember.go`
(`NewEntry` and `Create`'s collision-retry branch).

Reviewed and PASSED by `cairn/reviewer` (crn-x01ko, closed, verdict: pass)
against exactly this commit.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-x01ko's notes cite `deploy_commit: 7ab6321b7e35e21e79084428828da64eb537040d`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R), not an ancestor relationship. |
| 2 | Acceptance criteria met | PASS | Build bead crn-kp9rr.1's Definition of Done (go build/test pass; `cairn remember --topic` with a slash succeeds with flattened ID + preserved `topic_key`; `cairn review merge --topic-key` with a slash succeeds + preserved; existing slash-free usage unaffected) is fully exercised — reviewer's `diff_tests_executed` lists all 10 top-level / 17-including-subtests diff-owned tests by name, all PASS. Reviewer states explicitly: "uncovered_criteria: none." |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-x01ko-gate` at `7ab6321b...` (not trusting the reviewer's report alone): `go build ./...` exit 0. `go test ./... -race -count=1 -v`: **7/7 packages ok, 0 FAIL, 0 SKIP.** All 10 diff-owned tests individually confirmed PASS by name: `TestValidateTopicKeyAcceptsSlashDelimitedSegments`, `TestValidateTopicKeyRejectsInvalidSegment`, `TestFlattenTopicKeyReplacesSlashesWithDashes`, `TestNewEntryFlattensSlashTopicKeyForID`, `TestEntryCreateSucceedsWithSlashTopicKey`, `TestEntryCreateRetriesOnIDCollisionFlattensSlashTopicKey`, `TestMergeReviewBranchAcceptsSlashTopicKey`, `TestRememberAcceptsSlashTopic`, `TestRememberAndReviewMergeRoundTripSlashTopicKey`, `TestRememberBatchAcceptsSlashTopic`. (Note: an initial synchronous run hit a 5-minute shell timeout mid-suite under real fleet contention — treated as inconclusive and discarded, not as a pass or fail; the unbounded background re-run completed cleanly with exit 0.) |
| 4 | No open HIGH findings | PASS | Reviewer's `style_findings: none` (gofmt -l clean on all 6 changed files, go vet clean). `security_findings: none` — explicit OWASP-lens walkthrough, including an adversarial path-traversal trace (leading/trailing/doubled slash, `../` attempts) confirming `flattenTopicKey` cannot reconstruct a pattern `ValidatePathSegment` would have blocked. Fleet-wide bead search found no open HIGH/blocker finding linked to crn-kp9rr.1, crn-x01ko, or crn-ozwbm. |
| 5 | Clean tree | PASS | `deploy/crn-x01ko-gate` cut directly from `7ab6321b...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty immediately after checkout, before this gate doc's own commit. |
| 6 | Clean divergence from main | PASS | `merge-base(D, origin/main)` = `7cd1d16`. `origin/main` has exactly **one** commit since (`dd17d31`, an unrelated `Find` hit_count busy_timeout retry fix touching only `internal/cairn/entry.go` and `internal/cairn/index_test.go`) — zero file overlap with this diff's 6 changed files. |
| 7 | Single feature theme | PASS | All changed files (`internal/cairn/validate.go`, `internal/cairn/validate_test.go`, `internal/cairn/remember.go`, `internal/cairn/review.go`, `cmd/remember.go`, `cmd/remember_batch.go`) implement one cohesive theme: topic_key slash support. The `cmd/remember_batch.go` site is an additional call site for the same validation swap, found by the builder during implementation and flagged by the reviewer as informational — not a drive-by or independent theme. |

## Verdict: PASS — proceeding to PR.

## Process note

The deploy bead's own body text ("Route a merge-request to mayor/mpr; merge
authority is operator/mayor/mpr only — no rig agent runs `gh pr merge`.
Report the gate result back to mayor.") reflects process superseded, same
day, by the mayor-ruled standing authorization
(`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15) and
the current deployer role prompt: for `quad341/cairn` only, gate 7/7 PASS +
CI green (`build-test`, `lint`) ⇒ the deployer arms `gh pr merge --auto`
directly, with no mayor escalation required. This gate follows the newer,
more specific, same-day authorization rather than the bead's stale prose.
