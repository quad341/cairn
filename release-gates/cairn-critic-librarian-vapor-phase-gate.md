# Release Gate: cairn-critic-librarian-vapor-phase

**Bead:** crn-ju4o (from:crn-aa0y)
**Commit:** `a9417e3d95a6124ddddac244189b84c97db7c72d` (`builder/crn-aa0y`, post-self-rebase)
**Original reviewed commit:** `c5ff5cbf27b6dfc284a8ee6c84614e393557d88f`
**Date:** 2026-07-23

## Background

The reviewer's PASS was recorded against `c5ff5cb`, cut from `f6970f5` (2
commits behind `origin/main`'s tip at evaluation time: `b17fe3d` docs, then
`ac691c3` index-backed-reads). Criterion 6 initially FAILed (origin/main not
an ancestor). Per the deployer's bounded-self-rebase guardrail,
`attempt_bounded_self_rebase("builder/crn-aa0y", "main")` was run: it reported
a **fully clean rebase — zero conflicts, not even a trivial-conflict-resolver
invocation** (`rc=0`, `BEFORE_SHA=c5ff5cb...`, `AFTER_SHA=a9417e3...`),
force-with-lease pushed to `origin/builder/crn-aa0y`.

Verified independently before trusting the new SHA for criterion 1: the
rebase's merge-base with `origin/main` is `origin/main`'s own tip
(`ac691c3`), and the net diff introduced by the two reviewed commits
(`formulas/formulas_test.go`, `formulas/mol-cairn-critic.formula.toml`,
`formulas/mol-cairn-librarian.formula.toml`) is **byte-identical** before and
after the rebase — confirmed via `diff` of the two isolated diffs (old base
`f6970f5` vs. new base `origin/main`). No file outside those three changed.
Zero content drift; only the base commit shifted. Deploying `AFTER_SHA` per
the self-rebase path.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS (via self-rebase) | Initial check: `git merge-base --is-ancestor origin/main c5ff5cb` → false (2 behind). `attempt_bounded_self_rebase` → `rc=0`, clean, no conflicts. Post-rebase: `git merge-base --is-ancestor origin/main a9417e3d` → true; merge-base of `a9417e3d` and `origin/main` equals `origin/main`'s own tip. |
| 1 | Review PASS present, SHA match | PASS (self-rebase exception) | Reviewer recorded PASS in `crn-ju4o` notes citing `R=c5ff5cb`. Deploy commit `D=a9417e3d` differs from `R` only via the provably-trivial self-rebase (zero conflicts). Net diff proven byte-identical pre/post-rebase (see Background) — the reviewed content is unchanged, only the base commit advanced. Per the deployer's self-rebase-path instructions, `AFTER_SHA` is used as the deploy target without requiring fresh re-review. |
| 2 | Acceptance criteria met | PASS | Independently re-ran the 3 named regression tests by name on the rebased tip: `TestCriticFormulaHasVaporPhase`, `TestLibrarianFormulaHasVaporPhase`, `TestCriticLoopStepSelfRepoursRootOnlyAndRestampsRouting` — all PASS. File contents independently spot-checked: both `formulas/mol-cairn-critic.formula.toml` and `formulas/mol-cairn-librarian.formula.toml` carry `phase = "vapor"`. |
| 3 | Tests pass | PASS | Run fresh on the rebased tip (`a9417e3d`, checked out in this worktree): `go build ./...` clean, `gofmt -l .` empty, `go vet ./...` clean, `golangci-lint run ./...` → 0 issues (shared cache cleaned first per known cross-worktree cache leak), `go test ./... -race -count=1` → ok on all packages (cmd, formulas, internal/cairn, internal/critic, scripts). |
| 4 | No high-severity review findings open | PASS | Reviewer's own evidence states security review clean, no new attack surface. No HIGH-finding labels or open findings present on the bead. |
| 5 | Final branch clean | PASS | `git status --porcelain` empty on `a9417e3d`. |
| 7 | Single feature theme | PASS | The bead's own 2 commits touch exactly 3 files (`formulas/formulas_test.go`, `formulas/mol-cairn-critic.formula.toml`, `formulas/mol-cairn-librarian.formula.toml`) — one coherent theme: add `phase = "vapor"` to both formulas and fix the critic loop's self-repour step, matching the bead title exactly. No unrelated changes riding along. |

## Verdict: PASS (all 7 criteria)

## Note on merge authority — HOLD cleared by mayor, proceeding to PR + arm

This bead previously carried the `hold:mayor` label, and its own description
text instructed "route the merge request to mayor/mpr for actual merge —
deployer does not merge directly." That instruction reflected the deployer's
**prior** protocol; the deployer's current role prompt grants a
narrowly-scoped exception to arm `gh pr merge --auto` itself on green gates,
escalating to mayor only if the arm fails. A prior deployer session
(`deployer-gm-zrulh`) found no mail or memory confirming mayor had cleared
this specific `hold:mayor` under the newer protocol, so it escalated by mail
(`gm-wisp-5gx2pkz`) rather than self-overriding an explicit hold, and left
the bead in progress with no PR opened and no merge armed.

**Resolution:** `bd history crn-ju4o --events` shows an audit-verified
`label_removed by mayor` event at 2026-07-23 20:42:39Z with comment "Removed
label: hold:mayor" — after the escalation mail (sent 20:36:50Z). Sibling
`crn-x105` (from:crn-6ef7, stacked on this bead's original `c5ff5cb`) still
carries `hold:mayor` and was deliberately left untouched, which reads as
mayor's answer to both open questions from the escalation: proceed with this
bead now under the deployer's own-PR auto-merge authority; `crn-x105` stays
held/sequenced behind this bead's landing. This interpretation was mailed
back to mayor (`gm-wisp-i4i1tk8`, peek-verified) before proceeding, so mayor
has a chance to correct it if wrong. Proceeding to open the PR and arm
`gh pr merge --auto` per the standard PASS path.

The cross-bead sequencing hazard remains unchanged: this bead's isolated
deploy branch/PR **must land on `main` before** `crn-x105` can safely be
processed — `crn-x105`'s reviewed commit is a direct descendant of this
bead's *original* `c5ff5cb`, which the self-rebase has moved out from under
it (the sibling's builder branch, `builder/crn-6ef7`, was **not** touched by
this rebase and still points at the old lineage). `crn-x105` is left alone
this session; it already carries a note not to be processed standalone until
this bead's deploy resolves.
