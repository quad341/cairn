# Release Gate: crn-jbnm — route critic/librarian filed beads into pm pipeline (from:crn-go8o)

**Bead:** crn-jbnm (deploy) / crn-go8o (review) / crn-wqgm (original bug, closed)
**Evaluated:** 2026-07-27
**Deploy source (D):** `6f138cdd4cf9f68edf61b131c854ba5ca5f99664` (provenance: `gc-builder-d53f3747bdfa`, base `origin/main@ebad2fb` — D's own parent already IS origin/main's current tip)

**Result: PASS — all 7 criteria**

## Criterion 6 — Branch diverges cleanly from main: PASS (evaluated first)

`git merge-base 6f138cdd... origin/main` == `ebad2fbb0f941f30e2032de6a490e79153642424`, i.e.
origin/main's exact current HEAD. The deploy commit's own parent already **is** origin/main's tip —
zero divergence, no rebase needed. `git merge-tree --write-tree origin/main 6f138cdd...` reports a
clean merge tree (no conflict markers).

## Criterion 1 — Review PASS present, SHA match: PASS

Reviewer's fresh PASS (crn-go8o notes, round 2 — "REVIEWER VERDICT: PASS (re-review)") cites
"Commit reviewed: 6f138cdd4cf9f68edf61b131c854ba5ca5f99664" — exactly D. R == D.

## Criterion 2 — Acceptance criteria met: PASS

Original bug (crn-wqgm): critic + librarian loop formulas filed beads without a routable label, so
the deacon patrol's auto-router never picked them up. Round 1 of this fix patched
`mol-cairn-critic.formula.toml` (1 site) and `mol-cairn-librarian.formula.toml` (5 sites) but missed
`mol-cairn-librarian-rig.formula.toml` (a full structural duplicate — bd's formula system has no
alias/extends/include mechanism), which the reviewer caught via
`TestLibrarianRigFormulaHasSameStepsAsLibrarian` and REQUEST-CHANGES'd. This commit (round 2) applies
the identical fix — `,needs-pm` on `--labels` plus a Guard block re-fetching the filed bead and
asserting it matches the deacon patrol's own selection query (needs-pm label present, assignee empty,
gc.routed_to empty) — to all 5 remaining sites in the rig-tier wrapper.

Independently re-verified (not just trusting reviewer notes):

- `formulas/mol-cairn-critic.formula.toml`: 1 site, `--labels "dim:$DIM,source:cairn-critic-loop,needs-pm"` + Guard — confirmed.
- `formulas/mol-cairn-librarian.formula.toml`: 5 sites, all carry `needs-pm` — confirmed via grep.
- `formulas/mol-cairn-librarian-rig.formula.toml`: 5 sites, all carry `needs-pm` — confirmed via grep (this is the file round 1 missed).
- Total: 11/11 `bd create --labels` call sites across all 3 formula files carry `needs-pm`. `ls formulas/*.formula.toml` confirms no fourth formula file exists.

## Criterion 3 — Tests pass: PASS (one disclosed pre-existing flake, unrelated to this diff)

- `go build ./...`, `go vet ./...`: clean.
- `go test ./...`: one failure on the first run — `TestRunPerfScenarioCleansUpAfterItself`
  (internal/critic), a `testing.TempDir()` cleanup-time `unlinkat .../.git/info: directory not empty`
  race. This commit's diff touches only `formulas/formulas_test.go` and
  `formulas/mol-cairn-librarian-rig.formula.toml` (confirmed via `git diff-tree --name-only`) —
  nothing in `internal/critic`. Isolated reruns of the failing test: 3/3 PASS. Full-suite rerun:
  clean (exit 0), all packages green. This is the same known flake already filed and tracked as
  **crn-mxvn** (open, P2, "internal/critic perf-scenario tests flake on TempDir .git cleanup race
  under load", filed from a prior deploy gate on crn-4x9g/crn-p5gy) — not a new finding, not caused
  by this diff.
- Targeted regression tests (matching reviewer's citation):
  `TestLibrarianRigFormulaHasSameStepsAsLibrarian` and `TestLibrarianStepsHaveNeedsPmLabelAndGuard` —
  both PASS.

## Criterion 4 — No high-severity review findings open: PASS

Reviewer's round-2 PASS verdict records zero open HIGH findings. Round 1's REQUEST-CHANGES (missed
3rd formula file) was fully addressed and re-reviewed PASS.

## Criterion 5 — Final branch clean: PASS

`git status` clean at `6f138cdd...`.

## Criterion 7 — Single feature theme: PASS

One cohesive fix: propagate the identical `needs-pm` + Guard pattern to the one remaining unpatched
formula file (the rig-tier wrapper), completing the same bug fix crn-wqgm started. No unrelated
changes ride along.

**Verdict: PASS — proceeding to isolated deploy branch + PR + auto-merge arm.**
