# Release Gate: freshness invocation-failure vs confirmed-negative (Incomplete status)

- Bead: crn-jrhz (deploy) / crn-fdjc.1.1 (implementation) / crn-fdjc.1 (design)
- Reviewer-cited commit (R): f3937dd (branch builder/crn-fdjc.1.1), cut from origin/main@516262f
- Final deployed commit (D): 18ecf22 (deploy/crn-jrhz-gate, post-rebase reconciliation — see criterion 6)
- Evaluated: 2026-07-25, against origin/main@24a45b3

## 6. Clean divergence from main (evaluated first)

NOT a clean fast-forward as authored, and this bead's gate was PASSED once already under a
now-superseded understanding of that (see note at the end of this section). At the time R was cut
and reviewed, origin/main was at 516262f. By the time this branch was actually pushed, origin/main
had advanced twice further: 18db119 (PR #53, `cairn list <topic>` + shared `UntopicedLabel`
constant) and then 24a45b3 (PR #57, this same deployer's own crn-ouoi work — the context-budgeted
`Prime()` rewrite, including a reordered `Check()` call site and a new `freshnessBudget` type in
`internal/cairn/prime.go`).

Ran `attempt_bounded_self_rebase` first: it correctly identified a real conflict in
`internal/cairn/freshness.go` and safely aborted (RC=12), leaving the branch untouched. This is its
designed behavior, not a malfunction — the conflict is a genuine feature interaction between two
independently-reviewed, already-approved changes, not a mechanical text collision:

- **crn-0vqk.2** (on main via 24a45b3) reordered `Check()` to test `a.Fingerprint == ""`
  ("never verified") *before* calling `ComputeFingerprint`, so a never-verified anchor short-circuits
  for free with no git shell-out.
- **crn-fdjc.1.1** (this bead) needs `ComputeFingerprint`'s error return to surface as `Incomplete`
  — but only reaches that check by calling `ComputeFingerprint` at all.

Performed the reconciliation manually (pre-authorized deployer-time work per this bead's own
design intent — crn-fdjc.1's stated goal is exactly "propagate git-invocation-failure through
Check", which is meaningless if Check can skip invoking git in the one case built to prove it):

- **First pass** (mechanical, textually clean): kept the never-verified check first, added the
  `Incomplete`-on-error branch after it. Compiled, vetted, and linted clean. **Wrong.** Running the
  full test suite immediately surfaced 6 failures across 2 packages —
  `TestCheckGitInvocationFailureIsIncomplete`, `TestCheckContextCanceledIsIncomplete`,
  `TestSweepGitInvocationFailureIsIncomplete`, plus 3 in `internal/critic` — all already-reviewed
  pinning tests from crn-fdjc.1.1's own commit stack, each constructing a files anchor with no
  stored fingerprint and asserting `Incomplete`. The never-verified short-circuit was masking every
  one of them.
- **Corrected**: reordered `Check()` to attempt `ComputeFingerprint` (surfacing `Incomplete` on
  error) *before* the never-verified check, which now runs only after a successful call — this
  still wins over the Fresh/Stale comparison below it, preserving crn-0vqk.2's original fix against
  a false Fresh/Stale null-comparison for a never-verified anchor. There is exactly one ordering
  that satisfies both already-approved test suites simultaneously; this is it. Full suite green
  after the correction (see criterion 3).
- **Second-order consequence, different file**: `internal/cairn/prime.go`'s `freshnessBudget.classify`
  gated its shell-out cap on `a.Type == "files" && a.Repo != "" && len(a.Paths) > 0 && a.Fingerprint
  != ""` — correct only under the old ordering, where a never-verified anchor never shelled out.
  Under the corrected `Check()`, every shape-verifiable files anchor shells out regardless of
  fingerprint state, so the old clause under-metered exactly the cost crn-0vqk's FR-5 budget cap
  exists to bound (an unbounded run of never-verified anchors could bypass the cap entirely). Fixed
  by dropping the `&& a.Fingerprint != ""` clause. No existing test pins this exact gating shape
  (none constructs a never-verified files-anchor specifically to check it counts against budget),
  but the fix is a direct, mechanical consequence of the corrected `Check()` maintaining FR-5's
  already-approved invariant (bound git shell-outs regardless of store size) rather than a new
  design choice. Confirmed via grep that the one existing budget test
  (`TestPrimeCapsFreshnessChecksAndFailsTowardUnknown`) is unaffected — both its entries already
  carry non-empty fingerprints.
- **Independent second break, same rebase**: `go vet` failed post-rebase —
  `internal/cairn/prime_test.go:278` called `ComputeFingerprint` with its pre-crn-fdjc.1.1
  single-return signature. This file was merged to main earlier via this same deployer's crn-ouoi/
  PR #57 work and isn't touched by crn-fdjc.1.1's own commits, so the break was invisible to git's
  textual merge — a same-file-untouched, cross-branch API-compatibility gap, not a conflict git
  could ever have flagged. Confirmed via `grep -rn "ComputeFingerprint("` that this was the only
  stale call site anywhere in the repo. Fixed to the two-return form.

All three fixes above are one commit (18ecf22), separately messaged from the rebased RED/GREEN
commits it sits on top of.

Post-reconciliation: `git merge-base origin/main HEAD` == origin/main's own tip (24a45b3) exactly.
Zero divergence, clean fast-forward. **PASS** (post-reconciliation; not clean as originally authored
or as this bead's own now-superseded gate evaluation once assumed — see below).

**Note on this bead's prior PASS**: an earlier pass at this gate (commit eef42ec, still present
lower in this branch's history) evaluated criterion 6 as a trivial clean merge against origin/main@
18db119 ("still trivially mergeable... no self-rebase needed"). That was accurate at the moment it
was written, but origin/main advanced again (24a45b3) before this branch was actually pushed, which
is exactly the staleness the standing armed-PR sweep exists to catch — see crn-jrhz's own bd
close-reason and this deployer's mail trail for that discovery. This document supersedes eef42ec's
evaluation in full; eef42ec is left in history rather than amended, per this repo's git conventions,
but its criterion-6 PASS should not be relied on — this document is the accurate one.

## 1. Exact SHA match (D == R)

D (18ecf22) != R (f3937dd) literally — a reconciliation rebase was required (criterion 6). D's
content is R's exact reviewed diff (96577ca + 3ddadca, both fully re-applied via rebase with no
logic changes to the reviewed lines) plus one additional, independently-authored, clearly-messaged
reconciliation commit (18ecf22) resolving a genuine feature interaction against a since-merged
sibling feature (crn-0vqk.2) and one unrelated cross-branch signature break, both detailed in
criterion 6. No part of the originally reviewed logic was altered by the rebase itself — only the
reconciliation commit changes behavior, and only in the narrow way needed to make both
already-approved features correct in combination. **PASS** (rebased content, not literal SHA — fully
accounted for above).

## 2. Acceptance criteria

Reviewer's PASS on crn-fdjc.1.1 verified `f3937dd` against crn-fdjc.1's design directly: `Incomplete
= "incomplete"` const; propagation through `Check`/`ComputeFingerprint`/`Sweep`; `cmd/commands.go`
flag-marker wiring; `internal/critic/freshness.go`'s new invocation-incomplete sub-check. All of
that is unchanged by the rebase — confirmed by re-reading the final `Check()` and `Sweep()` bodies
in full post-reconciliation: the `Incomplete` branch, its detail message, and every call site listed
in the reviewer's verdict are present and structurally match.

Two files outside the original reviewed list were touched by this deployer's own reconciliation:
`internal/cairn/prime.go` and `internal/cairn/prime_test.go`. Neither belongs to crn-fdjc.1.1's own
scope — both exist only because crn-0vqk.2 (a different, already-merged bead) shares `Check()` as a
dependency. The changes are narrowly the two fixes described in criterion 6 (budget-gate condition,
one call-site signature), not new functionality. **PASS**, with the file-list deviation disclosed
above rather than silently expanded.

## 3. Tests

`go test ./... -race -count=1`, run fresh against the final reconciled HEAD (18ecf22) in this
session: all packages green — `cmd` 16.4s, `formulas` 1.0s, `internal/cairn` 32.4s, `internal/critic`
14.3s, `scripts` 4.6s. No failures.

Two intermittent, unrelated failures were observed on earlier runs during this reconciliation
(before this final clean run): `TestConcurrentReindexDoesNotRaceOnEntryTagsSchema` and
`TestConcurrentFindAndReindexDoNotHardFail`, both in `internal/cairn/index_test.go`, both
"database is locked (5) (SQLITE_BUSY)". Confirmed pre-existing/environmental, not caused by this
diff: `git diff origin/main -- internal/cairn/index_test.go internal/cairn/index.go` is empty (file
untouched by this branch), both failures were non-reproducing on immediate rerun, and this exact
class of flake is already tracked — `crn-j3k4` (closed, the steady-state DDL-race fix, now merged to
main as part of PR #42/ac691c3) and `crn-t42e` (open, describing the precise residual cold-store
race at a matching ~1/80 rate). Left a note on crn-t42e during this session flagging that its own
blocking dependency (crn-2xpm) is now closed, and that its stale-blocker status should be
re-evaluated — unrelated follow-up, not a blocker for this gate. **PASS.**

## 4. No open blocking findings

Reviewer's verdict on crn-fdjc.1.1 (f3937dd) recorded zero Findings. My own reconciliation surfaced
and fixed its own defects before they became findings anyone else would need to catch (criterion 6)
— re-verified via the full test suite re-run above, not by re-reading my own reasoning alone.
**PASS.**

## 5. Clean working tree

`git status --porcelain` empty on the final reconciled HEAD (18ecf22). **PASS.**

## 7. Single coherent theme

Git-invocation-failure vs. confirmed-negative propagation through the freshness subsystem
(`internal/cairn` + `internal/critic` + their `cmd` call site) is one coherent theme (crn-fdjc
epic). The reconciliation commit is a same-theme correctness fix forced by this feature's
interaction with an already-merged sibling (crn-0vqk.2), not an independent feature bundled in.
**PASS.**

## Verdict: GATE PASS — proceeding to isolated deploy branch push + PR.
