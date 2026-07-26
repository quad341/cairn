# Release Gate: crn-lzn4.1.1 — remember agent-native capture (stdin/file input, anchors, duplicate-aware writes)

**Bead:** crn-lzn4.1.1
**Evaluated:** 2026-07-26
**Deploy source (D):** `builder/crn-lzn4.1.1`, reviewer's PASS cites `f13fe54c1a05e26de01dc69f2a27dc2580353127`
(base `be93ae7`; 3 commits: `57b9ca4` red, `c5372df` green, `f13fe54` lint-fix)

**Result: FAIL — criterion 6**

## Criterion 6 — Branch diverges cleanly from main: FAIL

The reviewer's PASS checked `origin/main` at a tip no newer than `be93ae7` (the branch's own base).
Re-fetched immediately before this evaluation: `origin/main` is now
`947e7ff4b19e1c311ebb543ca1d0e698893e7ec3`, two commits ahead of `be93ae7`:

- `0fe1da3` Add --json output, stable error categories, and version/help unification (#58)
- `947e7ff` Add debug logging: JSONL file log + opt-in --trace (#59)

`git merge-base --is-ancestor origin/main f13fe54c1` → false. Bounded self-rebase attempted per
protocol (run from a dedicated worktree on a temp local branch pointing at the same commit — the
real `builder/crn-lzn4.1.1` ref was already checked out by name in another active session's worktree,
so a same-name checkout was avoided rather than disturbing that session's state; clean tree
confirmed, no active concurrent session on the branch itself per `gc session list`):

- Commit 1/3 (`57b9ca4`, test/red): conflict in `cmd/remember_test.go`, auto-resolved by the trivial
  classifier (additive-both on an allowlisted test-file path — keep-both). Replayed as `d75b3f4`.
- Commit 2/3 (`c5372df`, feat/green): **real, non-trivial conflict in `cmd/remember.go`**, three
  hunks, none eligible for auto-resolution (non-test source file; hunks are neither identical nor
  one-side-empty). Rebase aborted per protocol; branch left unchanged at
  `f13fe54c1a05e26de01dc69f2a27dc2580353127`. **No push of any kind occurred against the real
  `origin/builder/crn-lzn4.1.1` ref.**

### Conflict detail (`cmd/remember.go`, against `c5372df`)

Root cause: this branch forked from `be93ae7`, *before* `0fe1da3` (PR #58) merged. PR #58 is
crn-od2x.2's own --json unification work, which — per crn-od2x.3's bead description — explicitly
covers `cmd/remember.go` as one of its five targets ("get/status/map/prime/remember"). So `origin/main`
and this branch independently redesigned the same function's internals at the same time, without
knowledge of each other. (`947e7ff`/PR #59, crn-4x9g, does not touch `cmd/remember.go` at all — not
implicated in this conflict, only in how far behind main the branch is.)

1. **Lines 69–90 — `cairn.NewEntry` call-signature collision.** `origin/main`'s side calls
   `resolveIdentityValidated(cmd)` and constructs via the old positional signature:
   `cairn.NewEntry(topic, scope, args[0], createdBy)`. This branch's side calls the unvalidated
   `resolveIdentity(cmd)` and constructs via a new `cairn.NewEntryParams` struct literal with
   `Title`/`Summary`/`Anchor` fields. Both sides changed `NewEntry`'s call shape independently —
   main moved identity resolution to the validated variant, this branch moved the constructor to an
   options-struct to carry the new Title/Summary/Anchor/verify fields. These two evolutions must be
   reconciled by hand: the merged call needs the validated-identity call *and* the params-struct
   shape *and* this branch's new fields.
2. **Lines 117–123 — near-duplicate output line, not safe to auto-resolve as one-side-empty.**
   `origin/main`'s side is empty here because main already gated the entry-ID print behind
   `wantsJSON(cmd)` a few lines earlier (JSON-output support from PR #58). This branch's side
   (pre-JSON-support) unconditionally reprints `e.ID` here, plus the `--force` override line
   (`"override: forced past duplicate of %s\n"`). Concatenating both sides (keep-both) would
   double-print the entry ID — the override line needs to be preserved, but the ID print does not.
   Requires judgment, not textual merge.
3. **Lines 144–241 — two unrelated blocks landing at the same insertion point.** `origin/main`'s
   side adds the `RememberResult` JSON-output struct type (PR #58). This branch's side adds three
   unrelated helper functions: `rememberBody` (positional/--file/stdin body-source resolution),
   `rememberAnchor` (anchor construction from `--anchor-repo`/`--anchor-path`), `verifyAnchor`
   (`--verify` fingerprint computation). These are structurally independent (a JSON result type vs.
   input-resolution helpers) and don't reference each other, but per policy `cmd/remember.go` is not
   an allowlisted path for additive-both auto-resolution — reconciling this correctly also depends on
   how hunk 1 is resolved (`RememberResult` may need new fields for anchor/title/summary; the merged
   `NewEntry` call needs to actually invoke `rememberAnchor`/etc.), so it isn't safe to resolve in
   isolation from hunk 1 either.

## Remaining criteria: SKIPPED (fail-fast per mandated evaluation order)

Criteria 1 (review PASS / SHA match), 2 (acceptance criteria), 3 (tests), 4 (no high findings), 5
(clean tree), 7 (single theme) were not evaluated — criterion 6 FAILs first in the mandated order.
For the record: criterion 1 would have passed trivially if reached — the reviewer's PASS on
crn-lzn4.1.1 cites `f13fe54c1a05e26de01dc69f2a27dc2580353127` exactly, the current deploy SHA (D == R).

## Action

Routing back to `cairn/builder` (bead reopened to `in_progress`, reassigned) to rebase
`builder/crn-lzn4.1.1` onto current `origin/main` (`be93ae7` → `947e7ff`) and resolve the
`cmd/remember.go` conflict by hand — specifically, reconcile `cairn.NewEntry`'s two independently
-evolved call shapes (validated-identity + positional vs. unvalidated-identity + params-struct),
resolve the duplicate-print at lines 117–123 without losing the `--force` override message, and
correctly interleave `RememberResult` with the three new helper functions (including wiring
`rememberAnchor`/`rememberBody`/`verifyAnchor` into whatever the reconciled `NewEntry` call ends up
being). Once rebased, this bead needs a fresh reviewer PASS at the new SHA before returning to deploy
(SHA-pinning mandate).
