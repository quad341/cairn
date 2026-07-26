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

---

## Round 2 — origin/main advanced again (2026-07-26)

Reviewer delivered a fresh SHA-pinned confirmation PASS at `89b98b308ac9986861877f60ef5681c9ed650594`
(independently verified patch-id `36beab7594834823494c4ce6b3e5b6f973691830` vs `f06cf947`'s own
patch-id, identical; full gates rerun, one pre-existing flaky test
`TestConcurrentFindAndReindexDoNotHardFail` correctly attributed to already-tracked `crn-t42e`,
unrelated to this bead's `cmd/list.go`-only diff — see bd notes on crn-od2x.3 for the reviewer's
complete writeup). Criterion 1 satisfied at that point: R == D == `89b98b30`.

Before deploy could proceed, `origin/main` advanced a third time today, to
`f5e0b6018764f4b343d85c983e76a2b3256d0de7` (`f5e0b60` "Add cairn doctor: aggregation, tolerant
iteration, explain modes (#60)"). `f5e0b60` touches `cmd/doctor.go`, `cmd/doctor_test.go`,
`internal/cairn/dedup.go`, `internal/cairn/dedup_test.go`, `internal/cairn/doctor.go`,
`internal/cairn/doctor_test.go`, `internal/cairn/entry.go`, `internal/cairn/entry_test.go`,
`internal/cairn/index.go`, `internal/cairn/index_test.go`, `internal/cairn/sweep.go`,
`internal/cairn/sweep_test.go`, `release-gates/crn-4m7k-doctor-gate.md` — none of which overlap this
branch's diff (`cmd/list.go` + `cmd/commands_json_test.go` only; confirmed by direct set comparison).

`git merge-base --is-ancestor origin/main 89b98b30` → false. Bounded self-rebase attempted again per
protocol: `attempt_bounded_self_rebase "builder/crn-od2x.3" "main"` (dedicated worktree, clean tree,
no concurrent session) returned **rc=0** — clean, zero conflicts (non-overlapping file sets, as
expected). Force-with-lease pushed to `origin builder/crn-od2x.3`:

- `BEFORE_SHA=31119e4d0cea7c3fd4bd94ec957598a371738ccd` (code `89b98b30` + this gate doc, as one unit)
- `AFTER_SHA=1d3512a90268614f4b3569f0e842e84cfb10164e`
- Pure-code commit (pre-gate-doc), analogous to round 1's `89b98b30`: `ebfae1105af4eb7d7f57067ab6677a02a41100db`

Criterion 6 now PASSes again against current `origin/main` (`ebfae110`'s parent chain includes
`f5e0b60`). Verified content identity independently (not just asserting it, mirroring the reviewer's
own round-1 rigor) — diffing each tip against its true parent:

```
git diff 0efcc5d5 89b98b30  | git patch-id --stable  ->  36beab7594834823494c4ce6b3e5b6f973691830
git diff 2fac32f  ebfae110  | git patch-id --stable  ->  36beab7594834823494c4ce6b3e5b6f973691830
```

Identical — confirming this round's rebase, like round 1's, is a pure mechanical replay with zero
content drift.

### Criterion 1 — Review PASS present, SHA match: FAIL (again)

Same structural reason as round 1: the commit SHA changed again (`89b98b30` → `ebfae110`, different
parent — `2fac32f` is this same bead's own red/TDD commit rebased forward, not a main commit). Per the
SHA-pinning mandate (`crn-fie5`), the reviewer's round-2 PASS — recorded against
`89b98b308ac9986861877f60ef5681c9ed650594` — does not carry forward to
`ebfae1105af4eb7d7f57067ab6677a02a41100db`, regardless of the rebase again being purely mechanical.

### Remaining criteria: SKIPPED (fail-fast per mandated evaluation order)

Criteria 2/3/4/5/7 not independently re-evaluated this round, for the same reason as round 1 — and per
the patch-id proof above, whatever the reviewer verified at `89b98b30` (including round 2's own fresh
full-gate rerun) is byte-identical in content at `ebfae110`.

### Action (round 2)

Routing to `cairn/reviewer` for a second fresh (re-confirmation-style) PASS, this time at
`ebfae1105af4eb7d7f57067ab6677a02a41100db`. `origin/main` has now moved three times today
(`#58` → `#59` → `#60`); per the `deploy-gate-infra-blocker-status` precedent (crn-2xpm) this
repeated rebase → SHA-change → fresh-review cycling is expected/by-design while main is moving fast,
not a bug to route around. If `origin/main` advances again before this round's reviewer PASS lands,
expect a round 3.
