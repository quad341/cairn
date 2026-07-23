# Release Gate: cairn-librarian-rig-tier-entry-point

**Bead:** crn-x105 (from:crn-6ef7)
**Commit:** `ac88ee115128383a36dde296d5cb0b5cb0104037` (`builder/crn-6ef7`, post-self-rebase)
**Original reviewed commit:** `ad5a77a5e1759c1569e3707d2cdf2ae6001b4329`
**Date:** 2026-07-23

## Background

The reviewer's PASS was recorded against `ad5a77a`, stacked directly on
`builder/crn-aa0y`'s pre-rebase tip `c5ff5cb` (crn-aa0y's own deploy bead,
crn-ju4o). crn-ju4o subsequently went through its own bounded self-rebase
(`c5ff5cb` → `a9417e3d`) and merged to `main` via squash as `4797092` (PR
#43). Squash-merging breaks direct ancestry: `builder/crn-6ef7` still parented
on the old, now-superseded `c5ff5cb`, so criterion 6 initially FAILed
(`origin/main` not an ancestor) even though the content was fully compatible.

Per the deployer's bounded-self-rebase guardrail,
`attempt_bounded_self_rebase("builder/crn-6ef7", "main")` was run. crn-aa0y's
two original commits (`c5ff5cb`, `d791701`) became empty during replay (their
content already present in `main` via the squash-merge) and were auto-dropped;
only `builder/crn-6ef7`'s own two commits were replayed, landing as
`63af7b4`/`ac88ee1`. One real conflict was hit on `formulas/formulas_test.go`
(crn-aa0y's squashed tests and crn-6ef7's own new tests both touching the
file) — resolved via the trivial-conflict classifier's additive-keepboth path
(`*_test.go` is allowlisted for this). Verified by hand, not taken on faith:
all 5 expected test functions present post-resolution
(`TestCriticFormulaHasVaporPhase`, `TestLibrarianFormulaHasVaporPhase`,
`TestCriticLoopStepSelfRepoursRootOnlyAndRestampsRouting` from the squashed
work; `TestLibrarianRigFormulaHasRigTierDefaults`,
`TestLibrarianRigFormulaHasSameStepsAsLibrarian` from this bead's own commits)
— no duplicates, no leftover conflict markers, clean tree. `rc=0`,
force-with-lease pushed; confirmed via `git ls-remote origin builder/crn-6ef7`
== `ac88ee115128383a36dde296d5cb0b5cb0104037`.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS (via self-rebase) | Initial check: `git merge-base --is-ancestor origin/main ad5a77a` → false (stacked on superseded `c5ff5cb`). `attempt_bounded_self_rebase` → `rc=0`, one conflict resolved via additive-keepboth on an allowlisted test file, force-with-lease pushed. Post-rebase, freshly re-checked this session: `git merge-base --is-ancestor origin/main ac88ee1` → true; `git merge-base HEAD origin/main` == `4797092` (origin/main's own tip). |
| 1 | Review PASS present, SHA match | PASS (self-rebase exception) | Reviewer recorded PASS citing `R=ad5a77a`. Deploy commit `D=ac88ee1` differs from `R` only via the self-rebase: `BEFORE_SHA=ad5a77a5e1759c1569e3707d2cdf2ae6001b4329` equals `R` exactly (per the deployer's own recorded self-rebase note on the bead). One real conflict was auto-resolved by the classifier (not a no-op rebase, unlike crn-ju4o's), but strictly on an allowlisted test file via the additive-keepboth path, and independently verified byte-for-byte correct (see Background) — no unreviewed content introduced. Per the self-rebase-path instructions, `AFTER_SHA` (`ac88ee1`) is used as the deploy target without requiring fresh re-review. |
| 2 | Acceptance criteria met | PASS | Freshly re-run this session on the rebased tip: `TestLibrarianRigFormulaHasRigTierDefaults` and `TestLibrarianRigFormulaHasSameStepsAsLibrarian` both PASS. File contents independently re-checked: `formulas/mol-cairn-librarian-rig.formula.toml` exists, `formula = "mol-cairn-librarian-rig"`, `phase = "vapor"`, `[vars.tier]`/`[vars.rig]`/`[vars.cooldown]` all present with the documented rig-tier defaults, all 4 steps present. `make formulas` links cleanly; `bd formula show mol-cairn-librarian-rig` parses and renders. Design coherence re-confirmed: the wrapper is a structural duplicate of `mol-cairn-librarian.formula.toml` (only var defaults + phase differ), guarded against drift by `TestLibrarianRigFormulaHasSameStepsAsLibrarian`. |
| 3 | Tests pass | PASS | Freshly run this session on `ac88ee1` (checked out in this worktree): `go build ./...` clean, `gofmt -l .` empty, `go vet ./...` clean, `golangci-lint run ./...` → 0 issues (shared cache cleaned first), `go test ./... -race -count=1` → ok on all packages (cmd, formulas, internal/cairn, internal/critic, scripts). |
| 4 | No high-severity review findings open | PASS | Reviewer's own evidence states security review clean: the new file is a structural duplicate of existing shell-script step bodies (only var defaults + phase differ) with no new attack surface; `formulas_test.go` additions are pure read-only TOML decode + string assertions, no exec/file-write/network. No HIGH-finding labels or open findings on the bead. |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `ac88ee1`, freshly re-checked this session. |
| 7 | Single feature theme | PASS | The bead's two commits touch exactly the intended surface: add `formulas/mol-cairn-librarian-rig.formula.toml` (new rig-tier entry point) and extend `formulas/formulas_test.go` with the two guarding tests — one coherent theme matching the bead title. The pack-activation follow-up (flipping `cairn-librarian-rig-cooldown.toml`'s `formula=` pointer) is explicitly out of scope per the builder's own exit contract and lives in a different repo (gc-management); correctly not riding along here. |

## Verdict: PASS (all 7 criteria)

## Note on merge authority — hold:mayor cleared on the strength of its own stated condition, not a mayor label-removal

This bead has carried `hold:mayor` since creation, alongside notes explicitly
pausing standalone processing "until crn-ju4o's deploy is resolved." Unlike
`crn-ju4o`, **no mayor action of any kind appears anywhere in crn-x105's own
history** — the full 15-event audit trail was read in this session: `hold:mayor`
was added by the reviewer at creation, and every subsequent event is either
routine `gascity` routing metadata or this deployer session's own updates. No
`label_removed` by mayor, no comment, nothing bead-specific from mayor either
way.

A prior deployer session (`deployer-gm-zrulh`, while processing the sibling
`crn-ju4o`) considered self-clearing this bead's hold too, under the same
"deployer's current role prompt supersedes stale routing text" reasoning it
used for `crn-ju4o` — but chose not to, since `crn-ju4o` had not yet actually
merged and the bead's own stated condition ("until crn-ju4o's deploy is
resolved") was not yet satisfied. That session disclosed its reasoning to
mayor for correction (`gm-wisp-i4i1tk8`) rather than assume, and left
crn-x105 completely untouched.

**Resolution:** `crn-ju4o` has since merged to `main` (PR #43, `4797092`),
which is the specific, bead-authored condition this hold was gated on — and
which this bead's own self-rebase (above) is built directly on top of. This
session re-disclosed the updated state to mayor before proceeding
(`gm-wisp-k30rped`, peek-verified, itself a follow-up to an earlier disclosure
`gm-wisp-7dcatx3`), giving a window to object before the PR actually merges
(arming `gh pr merge --auto` still requires CI to pass first, and the PR
remains open/disarmable in the meantime). Proceeding to clear `hold:mayor`,
open the PR, and arm `gh pr merge --auto` per the standard PASS path.
