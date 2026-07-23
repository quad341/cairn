# Release Gate: cairn-index-backed-reads-squashed

**Bead:** crn-2xpm (deploy, tip of stack) — combined stack: crn-sj5r, crn-faj6,
crn-m9kv, crn-32j0, crn-3cmj, crn-ylpn, crn-2xpm
**Commit:** `8f0410600b2304a8d29ae2acd1ee711b1a61239d` (`builder/crn-2xpm-reconcile-v2`), cut onto `deploy/crn-2xpm-gate`
**Date:** 2026-07-23

## Background

Round 2 (`cc6ceb2`) reached PR #41, but CI went red: `origin/main`'s PR #40
(review CLI verb) had landed `internal/cairn/review_test.go` and
`cmd/review_test.go` with four call sites still using `Find`'s
pre-reconciliation 2-arg signature, while this stack's own reconciliation had
already given `Find` a leading `context.Context` parameter. PR #41 was routed
back to `cairn/builder` and left unmerged (see PR #41's comment).

A second re-verification attempt (`e9cdcd2`, zero code delta from `cc6ceb2`)
then failed this gate's own criterion 6: `cc6ceb2`'s internal merge-commit
topology was defeating `attempt_bounded_self_rebase`'s linear-replay check
even though `git merge-tree` showed a clean result against `origin/main`. A
known rebase-mechanics false positive, not a code problem.

Builder's fix for both: squash the full stack (every upstream commit plus the
round-2 reconciliation merge) into a single commit rebased directly onto
current `origin/main`, updating the four flagged call sites to the new
signature along the way. A fully linear, single-commit history sidesteps the
merge-commit topology issue by construction. `cairn/reviewer` independently
re-verified the squashed tip and returned "VERDICT: PASS (round 3, commit
8f04106)", noting zero code delta from `e9cdcd2` other than the squash/rebase
itself and the four signature fixes.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Fresh `git fetch origin main` immediately before evaluation (tip `b17fe3d69012103253c8d85542607102218aa28b`). `git merge-base --is-ancestor origin/main 8f04106` → true; 1 commit ahead, 0 behind. `git merge-tree --write-tree origin/main 8f04106` → clean, tree `5a0072e33d1be12f5d2b665a3404b159a77c357f`, no CONFLICT markers. No self-rebase needed — round 3's squash already targeted this exact topology problem. |
| 1 | Review PASS present | PASS | `cairn/reviewer` recorded "VERDICT: PASS (round 3, commit 8f04106)" in crn-2xpm's notes — an explicit fresh re-verification of the squashed tip, not a carried-over verdict. The commit deployed here (D) is identical to the commit reviewed (R): `8f0410600b2304a8d29ae2acd1ee711b1a61239d`. Handoff mail `gm-wisp-jpcbb2r` independently confirmed via `gc mail peek` (From/To/Subject/Body match the claimed handoff). |
| 2 | Acceptance criteria met | PASS | Round 3 is a squash/rebase of round 2's already-verified reconciliation (`cc6ceb2`, itself carrying round 1's fully-verified 7-bead combined stack) plus the four `Find`-signature fixes needed for PR #40 compatibility — reviewer confirms zero other code delta from `e9cdcd2`. Independently spot-checked in this session: `internal/cairn/index.go` contains both concurrency fixes (`entry_tags` DDL now inside the write transaction; `busy_timeout(5000)`/`journal_mode(WAL)`/`_txlock=immediate` in the DSN), and `internal/cairn/review_test.go:260,300,325` / `cmd/review_test.go:184` all correctly call `Find(t.Context(), ...)` against the new signature. |
| 3 | Tests pass | PASS | Run fresh on an isolated worktree checkout of `8f04106` in this session, not trusting prior reports: `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean, `golangci-lint run ./...` → 0 issues (shared cache cleaned first), `go test ./... -race -count=1` → ok on all packages. |
| 4 | No high-severity review findings open | PASS | The one HIGH finding carried through this stack's history (`index.go` missing `busy_timeout`/WAL locking around `Reindex`, first raised in crn-ynp6's review of crn-faj6, tracked as crn-gjmy → crn-t250) is fully resolved: crn-t250 (fix bead), crn-t250.1 (its review, PASS), and crn-gjmy (the tracking bead) are all CLOSED, and the fix is independently confirmed present at `index.go:90` in this exact commit. No other open HIGH findings identified against this stack's code. (crn-t42e, a P3 cold-start SQLITE_BUSY race, was reviewed and deliberately scoped out as non-blocking across multiple rounds; it is a P3, not a HIGH finding.) |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on the isolated worktree checkout of `8f04106`. |
| 7 | Single feature theme | PASS | Diff vs. the `origin/main` merge-base touches 24 files (1323 insertions, 114 deletions), entirely within `cmd/`, `internal/cairn/`, `internal/critic/` — one coherent theme: `cairn`'s read path (`Find`/`Visible`/`Prime`/`Status`) moved from full-store scans to a SQL index, plus the concurrency hardening (`Reindex` locking + `entry_tags` DDL race fix) required to make that index safe under concurrent access, plus the minimal reconciliation/rebase delta needed to land cleanly on current `main`. None of the constituent pieces is independently shippable. |

## Verdict: PASS — proceeding to PR.

## Note on PR #41

PR #41 (`deploy/crn-2xpm-gate` @ `cc6ceb2`/round 2) is superseded by this
round's squashed tip and is being closed accordingly, per the plan already
recorded in that PR's own comment thread. `deploy/crn-2xpm-gate` is reset to
`8f04106` and reused for the replacement PR — same isolated deploy branch,
new content, no other open PR or worktree was depending on the branch's prior
state.
