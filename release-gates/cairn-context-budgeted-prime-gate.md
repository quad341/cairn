# Release Gate: cairn CLI context-budgeted prime (byte budget, truncation, bounded freshness)

- Source bead: crn-ouoi (deploy handoff for crn-0vqk.2)
- Reviewer-cited commit (R): b61b9a51149b3e1e328d940f0c79ff8a7afb1f3a (branch builder/crn-0vqk.2)
- Final deployed commit (D): f4eea32 (deploy/crn-ouoi-gate, post-reconciliation — see criterion 6)

## 6. Clean divergence from main (evaluated first)

NOT a clean fast-forward as authored: R's branch merge-base with origin/main was 516262f,
while origin/main had since advanced by one commit (18db119, PR #53 "Add `cairn list <topic>`
command") that added the shared `cairn.UntopicedLabel` constant and updated the (now-superseded)
single-line `Prime()` to use it. Both the builder's and reviewer's notes on crn-0vqk.2 explicitly
flagged this as an anticipated deployer-time reconciliation ("expect a rebase/merge conflict or
silent semantic overlap in that area -- reconcile at merge time, re-run the full quality gate
suite... after reconciling before merging"), not a reason to bounce back.

Ran `attempt_bounded_self_rebase` (scripts/rebase-resolve-lib.sh) first: it correctly identified
the conflict in internal/cairn/prime.go as non-trivial (both sides non-empty, differ, source file
not on the additive-keepboth allowlist) and safely aborted (RC=12), leaving the branch untouched
at R. This is its designed behavior, not a malfunction -- the conflict genuinely requires semantic
judgment (see below), which the mechanical trivial-resolver is deliberately conservative about
attempting.

Performed the reconciliation manually, since it was already fully understood and explicitly
pre-authorized as deployer-time work by both builder and reviewer:
- `git rebase origin/main` conflicted in internal/cairn/prime.go exactly where expected: the old
  (now fully superseded) single-line `Prime()` body that PR #53 touched vs. the new `Prime()`
  rewrite's opening lines. Resolved by taking the new implementation wholesale for that hunk
  (the old function no longer exists in any form after crn-0vqk.2's rewrite, so there was nothing
  from "ours" to preserve). internal/cairn/entry.go auto-merged cleanly (PR #53's new
  `UntopicedLabel` const and this bead's Status() column extension are additive in different
  regions of the same file).
- The actual semantic gap: RenderPrimeText (new in this bead, so git had nothing to conflict
  against) had its own literal `"(untopiced)"` string, independently duplicating what PR #53's
  UntopicedLabel constant exists specifically to prevent ("shared by map, prime, get, and list so
  all four can never drift out of sync"). Fixed in a separate, clearly-messaged commit (f4eea32):
  swapped the literal for `UntopicedLabel`. Same string value -- confirmed zero behavior change,
  purely adopts the shared constant. Swept the whole diff (`grep -rn '"(untopiced)"' internal/cairn/
  cmd/`) afterward to confirm no other stray literal remained anywhere touched by this bead.

Post-reconciliation: `git merge-base origin/main HEAD` == origin/main's own tip (18db119) exactly.
Zero divergence, clean fast-forward. **PASS** (post-reconciliation; not clean as originally authored
-- see above).

## 1. Exact SHA match (D == R)

D (f4eea32) != R (b61b9a5) literally -- a reconciliation rebase was required (criterion 6). This
mirrors this repo's established "post-rebase re-evaluation" pattern (e.g. prior gates for
cairn-remember-topic-optional, cairn-kind-dedup-conflict-recall). D's content is R's exact reviewed
diff (c459c94 + b61b9a5, both fully re-applied via rebase with no logic changes -- the only
resolved hunk *dropped* dead code, it did not alter any reviewed line) plus one additional,
independently-authored, 1-line reconciliation commit with zero behavior change (detailed in
criterion 6). No part of the originally reviewed logic was altered by the rebase or the
reconciliation commit. **PASS** (rebased content, not literal SHA -- fully accounted for above).

## 2. Acceptance criteria

Reviewer's final-round verdict (two full review rounds; second round independently re-verified
commit b61b9a5 in an isolated worktree, not the builder's self-report) walked every FR/NFR/
Guardrail from crn-0vqk.1's design directly against the diff: FR-1 (Status() column extension),
FR-2 (byte-budget truncation, stop-at-first-over-budget semantics -- the fix for the reviewer's own
Finding 1), FR-3 (Check() reorder), FR-4 (bounded freshness-check cap, fail-toward-unknown), FR-5
(RenderPrimeText separated from Prime, reduced to a single-arg signature -- the fix for Finding 3),
all 5 NFRs, all 4 Guardrails -- all confirmed PASS by direct code inspection across both rounds.
Independently spot-checked the final file post-rebase (read in full): the `truncating`-bool latch,
`freshnessBudget` cap, and single-arg `RenderPrimeText(r PrimeResult)` are all present and structurally
match the verdict's description. My own reconciliation touched only prime.go's already-superseded
opening lines (dropped, not altered) and one cosmetic literal-to-constant swap in RenderPrimeText --
no FR/NFR/Guardrail-relevant logic was touched. **PASS.**

## 3. Tests

Independently re-ran `go test ./... -race -count=1` twice in this session: once on b61b9a5 as
authored (all packages green, matching the reviewer's own re-run), and again after the full
reconciliation rebase + UntopicedLabel fix (all packages green: cmd, formulas, internal/cairn,
internal/critic, scripts -- no failures, no new flakes). **PASS.**

## 4. No open blocking findings

crn-0vqk.2 had 3 findings across its two review rounds (1 initial + 1 addendum), all fixed by the
builder and independently re-verified fixed by the reviewer's final PASS (confirmed via direct diff
read of each fix, not the builder's self-report). No new findings surfaced by my own reconciliation
(mechanical rebase + one cosmetic constant adoption, independently gate-verified clean above).
**PASS.**

## 5. Clean working tree

`git status --porcelain` empty on the final reconciled HEAD (f4eea32). **PASS.**

## 7. Single coherent theme

Context-budgeted prime (byte budget, truncation, bounded freshness) is one coherent feature theme
(crn-0vqk epic). The reconciliation commit is a tightly-scoped, directly-related follow-up (adopting
a shared constant this same feature's rendering code needs to stay in sync with), not an independent
theme. **PASS.**

## Verdict: GATE PASS — proceeding to isolated deploy branch push + PR.
