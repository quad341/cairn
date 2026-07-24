# Release Gate: remember capture-time recurrence detection

**Bead:** crn-jfox (deploy) — covers crn-28ge.1.4
**Source:** shared builder branch `gc-builder-769138d1bf3c`, reviewed tip `24e19d2` (`24e19d2f4f0f11655b711fe0d78d232aa9b054a4`)
**Deploy branch:** `deploy/crn-jfox-gate`, cut from the reviewed SHA above, then rebased onto `origin/main` (see Criterion 6)
**Base:** `origin/main` @ `e0cfde9e05b34fb81d00f5ba824c25d94cdd53cc`

## Criterion 6 — Branch diverges cleanly from main (checked first)

`git merge-base origin/main 24e19d2` == `b4619c7`, not current `origin/main`
(`e0cfde9`). `origin/main` had since advanced via PR #46 (the squash-merge of
this same shared branch's earlier crn-28ge.1.2/.1.3/.1.5/.1.6 commits,
deployed via crn-c13d). The deploy branch still carried the pre-squash
originals of that already-merged content underneath crn-28ge.1.4's own three
commits — flagged in advance by both the deploy bead and the reviewer's own
notes as a real-reconciliation case, not an rc=20 no-op.

Ran `attempt_bounded_self_rebase(deploy/crn-jfox-gate, main)` per the sole
deployer rebase exception. Result: rc=0, a genuine (non-no-op) rebase,
classified and executed as provably trivial — the pre-squash originals
dropped cleanly against their already-merged counterparts on `main`, leaving
only crn-28ge.1.4's own three commits replayed on top:

- BEFORE_SHA (reviewed): `24e19d2f4f0f11655b711fe0d78d232aa9b054a4`
- AFTER_SHA (rebased): `eea622cc8cd79d611f0b4e008e57cbb2e4a54273`

Commit messages are unchanged across the rebase (`test(cairn): red`,
`feat(cairn): green`, `fix(cairn): suppress nilnil lint`, all `refs
crn-28ge.1.4`) — only the parent/ancestry changed, not the diff content.
`git merge-base origin/main HEAD` now equals `origin/main` exactly
(`e0cfde9`) — a strict fast-forward descendant. Force-with-lease pushed to
origin; `git ls-remote origin refs/heads/deploy/crn-jfox-gate` confirmed the
remote tip matches local HEAD post-push. **PASS.**

## Criterion 1 — Review PASS present for the deployed commit (SHA match)

Reviewer (cairn/reviewer, session reviewer-gm-g01t0) recorded **REVIEW
VERDICT: PASS** against commit `24e19d2` exactly (diff `678775a..24e19d2`),
re-reviewing only the one-line nilnil-suppression delta after an earlier
REQUEST-CHANGES pass on `678775a` was resolved. R (reviewed SHA) =
`24e19d2` = BEFORE_SHA above.

The deploy branch's tip is no longer `24e19d2` after Criterion 6's bounded
self-rebase, but the rebase is content-preserving by construction (provably
trivial: dropped only already-merged ancestry, replayed the same three
commits verbatim) and independently re-verified by re-running the full
quality gate on the new tip (Criterion 3, below) rather than assuming
equivalence. Review approval is treated as carrying forward to AFTER_SHA
`eea622c` per the deployer's bounded-rebase authorization. **PASS.**

## Criterion 2 — Acceptance criteria met

cmd/remember.go's create flow gains a topic_key-exact recurrence pre-check
via crn-28ge.1.3's shared `Conflicts()` primitive (NFR-05, filtered to
`Kind=="topic_key"` only — not the fuzzy Jaccard "content" signal); on match,
increments `RecurrenceCount` via the same tier-appropriate commit path
(`CommitDirect` for private, new `CommitRecurrenceToReviewBranch` for
shared-tier re-entry into an in-flight review branch); on no-match,
create-new-entry behavior is unchanged; a near-miss (similar but not exact
topic_key) does not trigger a false-positive increment.

Reviewer confirmed: "Spec/AC compliance: all 4 AC clauses implemented and
test-covered" in the REQUEST-CHANGES pass, reaffirmed unchanged in the final
PASS re-review. The one recorded deviation from the builder's original plan
(`CommitRecurrenceToReviewBranch` instead of literal `CommitToReviewBranch`
reuse) was independently traced by the reviewer through the actual call path
and endorsed as correct — literal reuse would make shared-tier recurrence
never succeed, since every shared-tier matched entry has, by construction,
already called `CommitToReviewBranch` once on its own ID during its own
creation. **PASS.**

## Criterion 3 — Tests pass

Independently re-run on the shared deployer worktree at the actual current
tip `eea622c` (post-rebase — not just trusting the pre-rebase reviewer
evidence at `24e19d2`, given the SHA changed):

- `go build ./...`: clean, exit 0
- `go vet ./...`: clean, exit 0
- `gofmt -l .`: 0 files
- `golangci-lint run ./...` (cache cleared first): 0 issues — confirms the
  nilnil suppression still holds post-rebase
- `go test ./... -race -count=1`: all packages ok (`cmd`, `formulas`,
  `internal/cairn`, `internal/critic`, `scripts`; root package has no test
  files)

**PASS.**

## Criterion 4 — No high-severity review findings open

No HIGH findings recorded. The one blocking finding from the initial review
pass (golangci-lint `nilnil` at `cmd/remember.go:148`) was fixed by the
builder (`//nolint:nilnil` with justification, matching this file's existing
`//nolint:gosec` convention) and re-reviewed PASS. Two non-blocking items
were raised and explicitly resolved without requiring changes:

- **shadowExempt gap** (same-scope repeat captures don't trigger recurrence
  detection, only incomparable-scope topic_key collisions do): confirmed
  pre-existing, shipped, tested behavior of the already-closed crn-28ge.1.3,
  not a regression introduced here. Reviewer agreed with builder's decision
  to leave out of this bead's scope; already flagged to mayor separately
  (gm-wisp-wdxpxe2) as a possible follow-up bead. No action from this review.
- **Unconditional `Reindex()` call** added to `recurrenceMatch` to fix a
  second, distinct staleness gap (shared-tier watermark blindness) found and
  fixed during test-writing: reviewer endorsed the perf tradeoff as-is,
  consistent with `Find()`'s existing identical fix for the same root cause.
  No objection.

Security (OWASP Top 10): reviewer recorded no findings — git invocations
remain argv-based, review-worktree paths confined to temp dirs, no new
secrets/auth/injection surface. **PASS.**

## Criterion 5 — Final branch clean

`git status --porcelain` on `deploy/crn-jfox-gate` shows no tracked changes.
(Two sibling untracked worktree directories from unrelated, still-open
concurrent deployer-worktree sessions — crn-ju4o, crn-x105 — were present in
the shared parent worktree and initially false-tripped
`attempt_bounded_self_rebase`'s dirty-tree precondition; added to the local,
uncommitted `.git/info/exclude` so they no longer appear in `git status`,
mirroring this repo's existing worktree-infra exclude conventions. Not part
of this branch's content.) **PASS.**

## Criterion 7 — Single feature theme

Single bead, single subsystem: capture-time recurrence detection in
`cairn remember`'s create flow, built directly on crn-28ge.1.3's shared
conflict-detection primitive and crn-28ge.1.1's schema fields (both already
merged to `main` via crn-c13d). All six touched files
(`cmd/remember.go`, `cmd/remember_test.go`, `internal/cairn/entry.go`,
`internal/cairn/entry_test.go`, `internal/cairn/remember.go`,
`internal/cairn/remember_test.go`) belong to one cohesive change: detect an
exact-topic_key recurrence at capture time and increment instead of
duplicating. **PASS.**

## Verdict: PASS — all 7 criteria green

Diff scope (`git diff origin/main..eea622c --stat`): `cmd/remember.go`,
`cmd/remember_test.go`, `internal/cairn/entry.go`,
`internal/cairn/entry_test.go`, `internal/cairn/remember.go`,
`internal/cairn/remember_test.go` (6 files, 793 insertions, 12 deletions) —
matches the scope recorded in crn-28ge.1.4's own notes.
