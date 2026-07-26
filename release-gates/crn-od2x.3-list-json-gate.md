# Release Gate: crn-od2x.3 — wire --json + resolveIdentityValidated into `cairn list <topic>`

**Bead:** crn-od2x.3
**Evaluated:** 2026-07-26
**Deploy source (D):** `builder/crn-od2x.3`, reviewer's PASS cites `f06cf9470a15e7cc4b5b601afc06c9aa8684427c`
(base: 2 commits ahead of `origin/main` at review time, tip `0fe1da3`)

## Criterion 6 — Branch diverges cleanly from main: re-checked, PASS after deployer self-rebase

The reviewer's PASS checked `origin/main` at `0fe1da3`. Re-fetched immediately before this
evaluation: `origin/main` has advanced one commit past that, to `947e7ff4b19e1c311ebb543ca1d0e698893e7ec3`
(`947e7ff` "Add debug logging: JSONL file log + opt-in --trace (#59)", i.e. crn-4x9g's deploy, which
landed after the reviewer's check). `947e7ff` touches `cmd/commands.go`, `cmd/root.go`,
`cmd/version.go`, `cmd/version_test.go`, `internal/cairn/entry.go`, `internal/cairn/entry_test.go`,
`internal/cairn/prime.go` — none of which overlap this branch's diff (`cmd/list.go` +
`cmd/commands_json_test.go` only, per the reviewer's own scope confirmation).

`git merge-base --is-ancestor origin/main f06cf947` → false (real divergence, not just stale-check
noise). Bounded self-rebase attempted per protocol: `attempt_bounded_self_rebase "builder/crn-od2x.3"
"main"` (run from a dedicated worktree, clean tree confirmed, no active concurrent session on the
branch per `gc session list`) returned **rc=0** — clean rebase, zero conflicts (non-overlapping file
sets, as expected). Force-with-lease pushed to `origin builder/crn-od2x.3`:

- `BEFORE_SHA=f06cf9470a15e7cc4b5b601afc06c9aa8684427c`
- `AFTER_SHA=89b98b308ac9986861877f60ef5681c9ed650594`

Criterion 6 now PASSes against current `origin/main` (`89b98b3`'s parent chain includes `947e7ff`).

## Criterion 1 — Review PASS present, SHA match: FAIL

Even though the rebase was fully clean and mechanical (no conflict, trivial or otherwise — the
rebase was a pure replay), **the commit SHA changed** (`f06cf947` → `89b98b30`, different parent).
Per the SHA-pinning mandate (established at `crn-4x9g`'s gate: a review PASS must cite the exact SHA
being deployed, D == R), the reviewer's existing PASS — recorded against `f06cf9470a15e7cc4b5b601afc06c9aa8684427c`
— does not carry forward to `89b98b308ac9986861877f60ef5681c9ed650594`. This holds regardless of how
mechanical the rebase was; the rule is about SHA identity, not about how much the content changed.

## Remaining criteria: SKIPPED (fail-fast per mandated evaluation order)

Criteria 2 (acceptance criteria), 3 (tests), 4 (no high findings), 5 (clean tree), 7 (single theme)
were not independently re-evaluated this round — criterion 1 FAILs first in the mandated order, so
evaluation stops there. For the record, none of these are expected to be at risk: the diff between
`f06cf947` and `89b98b30` is a pure rebase replay with no manual edits, so whatever the reviewer
verified at `f06cf947` is byte-identical in content, just replayed onto a new parent.

## Action

Routing to `cairn/reviewer` for a fresh (re-confirmation-style) PASS at the new SHA
`89b98b308ac9986861877f60ef5681c9ed650594`. This is a deployer-initiated mechanical rebase, not new
code — the diff content is unchanged from what was already reviewed at `f06cf947`, only the parent
commit differs. Once re-confirmed, this bead can return to deploy without needing another criterion-6
check (assuming `origin/main` hasn't moved again in the interim).
