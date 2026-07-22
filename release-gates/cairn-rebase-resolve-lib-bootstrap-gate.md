# Release Gate: cairn-rebase-resolve-lib-bootstrap

**Bead:** crn-elja.2 (deploy) — source review crn-dyxo — implementation crn-elja.1 — parent crn-elja
**Commit:** `35ebd215181f138144e8468d5111496eaced3638`, cut onto `deploy/crn-elja.2-gate` off `origin/main` (`c86480c16997f63b74a04c9cce61c1b70675ea40`)
**Date:** 2026-07-22

## Why this is a manual cut, not the automated flow

This PR lands `scripts/rebase-resolve-lib.sh` itself — the library that
`resolve_deploy_branch_target` / `assert_safe_push_target` come from. That
library does not exist on `origin/main` yet, so the standard automated
deployer branch-cut can't be used to land it (bootstrap circularity). This
is a one-time, operator-authorized manual cut, per direct operator→pm→deployer
instruction relayed in mail `gm-wisp-s350mjm`: "a carefully-documented manual
cut giving the same guarantees resolve_deploy_branch_target/assert_safe_push_target
would (verify source SHA isn't a shared branch tip, fresh branch name,
force-with-lease not force, full writeup in the gate file). Scoped strictly
to this one bootstrap PR, not a standing bypass." The manual safety checks
below substitute for the automation this same commit will enable for every
deploy after it.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Fresh-fetched `origin/main` immediately before cutting (tip `c86480c`). Branched directly off it and cherry-picked the reviewed commit — see "Bundling discovery and remediation" below for why a cherry-pick was used instead of branching at the reviewed SHA directly. `git diff --stat origin/main deploy/crn-elja.2-gate` shows exactly the 3 expected new files, `git status --porcelain` clean, no conflicts at any point. |
| 1 | Review PASS present | PASS | crn-dyxo closed (reason: pass) with verdict PASS on crn-elja.1 at commit `2abdbe5` — full independent gate reproduction (build/vet/gofmt/lint/race-test clean) plus adversarial AC4 smoke tests (rejected `gc-builder-<12hex>`-shaped branch names, path-traversal bead-id, bogus SHA — all correctly rejected with proper exit codes). |
| 2 | Acceptance criteria met | PASS | crn-elja.1's 5 ACs (byte-identical port of `scripts/rebase-resolve-lib.sh`, `scripts/test-rebase-resolve.sh`, `scripts/rebase_resolve_lib_test.go`; build/test/lint clean; fresh-repo smoke test of both safety functions) — re-verified independently in this session in a freshly-synced worktree: sourced the lib standalone, confirmed all 4 functions (`resolve_deploy_branch_target`, `assert_safe_push_target`, `is_shared_worktree_branch`, `attempt_bounded_self_rebase`) present and callable, re-ran the adversarial shared-branch-shape check (`gc-builder-1a2b3c4d5e6f` correctly detected as shared; this PR's own branch name `deploy/crn-elja.2-gate` correctly NOT detected as shared), and ran the full bundled `scripts/test-rebase-resolve.sh` suite: **27/27 pass, 0 fail**. |
| 3 | Tests pass | PASS | On the cherry-picked commit, in this session: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` empty, `go test ./... -race -count=1` — all 4 packages ok (`cmd`, `internal/cairn`, `internal/critic`, `scripts`), zero regressions. |
| 4 | No high-severity review findings open | PASS | crn-dyxo's review recorded zero open findings (byte-identical port, no logic drift from the gc-management source; the two dropped static-assertion tests were noted as intentional and non-blocking). Nothing further surfaced in this session's independent re-verification. |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on `deploy/crn-elja.2-gate`; only the pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) remain untracked. |
| 7 | Single feature theme | PASS (after remediation — see below) | Diff vs. `origin/main` touches exactly 3 files, all new, all under `scripts/`: `rebase-resolve-lib.sh`, `rebase_resolve_lib_test.go`, `test-rebase-resolve.sh`. `docs/DESIGN.md` is untouched relative to `origin/main`. One subsystem, one concern. |

## Bundling discovery and remediation

The reviewed commit `2abdbe5` (crn-elja.1's builder branch tip) was, at
review time, stacked on top of `5110e50` — an unrelated, independently
gate-PASSED but **not-yet-landed** commit from a different bead (crn-906q,
"DESIGN.md CLI list add remember/get/sweep/prime"). This was not visible
from the bead records alone; it surfaced from `git log --oneline
origin/main..2abdbe5`, which showed two commits instead of the expected
one.

Branching directly at `2abdbe5` (the literal SHA named in crn-elja.2's
acceptance criteria) would have silently bundled crn-906q's unrelated docs
change into this bootstrap PR — a criterion-7 violation, since the two
beads are independent and crn-906q is still awaiting its own deploy.

Verified before remediating: `2abdbe5`'s own diff (relative to its parent
`5110e50`) touches only `scripts/` — confirmed via `git diff 5110e50
2abdbe5 -- scripts/`. So the reviewed change itself never touched
`docs/DESIGN.md`; the bundling was purely an artifact of what the builder
branch happened to be stacked on.

Remediation: rather than branching at `2abdbe5` directly, this branch was
cut fresh off `origin/main` and the reviewed commit was cherry-picked onto
it (new commit `35ebd21`, byte-identical file contents to `2abdbe5`, no
conflicts). Verified after the fact:
- `git diff origin/main deploy/crn-elja.2-gate -- docs/DESIGN.md` — empty (untouched).
- `git diff 5110e50 2abdbe5 -- scripts/` vs `git diff origin/main deploy/crn-elja.2-gate -- scripts/` — byte-identical.

This satisfies crn-elja.2's AC1 more precisely than a same-SHA branch
would have ("origin/main has `scripts/rebase-resolve-lib.sh` at commit
`2abdbe5`'s **revision**" — i.e. that exact file content, reproduced here
exactly) while correctly excluding the unrelated bead's content. Flagging
this explicitly here, and in the mail to mayor, since it's a deliberate
(believed-correct) deviation from the literal branch-at-that-SHA
instruction rather than a mechanical follow of the authorized procedure.

## Bootstrap-specific safety checks (substituting for the not-yet-landed automation)

- **Source SHA not a shared-worktree-branch tip:** the reviewed commit's
  branch was `crn-elja.1-port-rebase-resolve-lib` (bead-named), not
  `gc-<agent>-<12hex>`-shaped. Re-confirmed programmatically in this
  session using the now-ported `is_shared_worktree_branch` function itself.
- **Fresh branch name:** `deploy/crn-elja.2-gate` confirmed absent from
  `origin` before use (`git ls-remote origin refs/heads/deploy/crn-elja.2-gate`
  → empty) and confirmed not shared-worktree-branch-shaped.
- **force-with-lease, not force:** the push below uses
  `--force-with-lease` exclusively; bare `--force` is not used anywhere in
  this procedure.
- **Merge routing:** per crn-elja.2's explicit AC, this PR is routed to
  mayor/mpr for actual merge — deployer does not self-arm auto-merge or
  merge this PR directly. This is a deliberate, one-time deviation from
  the standard deployer arm-and-verify-auto-merge flow, scoped to this
  bootstrap PR only.

## Verdict: PASS — proceeding to PR (routed to mayor for merge, not self-armed).
