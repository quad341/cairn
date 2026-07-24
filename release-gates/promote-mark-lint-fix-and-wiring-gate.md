# Release Gate: promote-mark lint fix + librarian formula wiring

Follow-on deploy to `promoted-bead-id-writer-promote-mark-gate.md` (crn-2m8r,
GATE FAIL — lint red on PR #50). This gate covers the two commits needed to
actually land that work: `crn-lox2`'s lint fix and `crn-ghn8.2`'s formula
wiring, both queued as separate `needs-deploy` beads (`crn-2x8o`, `crn-jfhw`)
on the same shared builder branch `gc-builder-769138d1bf3c`.

**Reconciliation note:** `crn-2x8o`'s own suggested deploy action was to
cherry-pick only `309d8a8` (the lint fix) onto `deploy/crn-2m8r-gate`.
Independently verified via `git diff b04cda1 309d8a8 --stat` and `git log
--oneline 309d8a8` that `309d8a8`'s parent is `b04cda1` (`crn-jfhw`'s wiring
commit), and that `crn-jfhw`'s own hold-note explicitly assumed its change
would "ship for free" once this branch redeploys past `309d8a8`. Cherry-picking
`309d8a8` alone onto the existing branch (tip `a2fcdea`, which predates
`b04cda1`) would have applied cleanly (the two commits touch fully disjoint
files — `cmd/*_test.go` vs `formulas/*.toml` — so there's no merge conflict
either way) but would have silently shipped PR #50 **without** `crn-jfhw`'s
reviewed, approved change, contradicting its own note. Both commits are
independently SHA-pinned reviewer PASSes, are content-disjoint, and are the
same feature theme (the `crn-ghn8` promote-mark epic), so both were
cherry-picked onto the existing branch — patching PR #50 in place rather than
opening a second PR or dropping the wiring change. Disclosed to mayor
(`gm-wisp-7otytjy`) before arming.

Deploy source: `deploy/crn-2m8r-gate` fast-forwarded to `origin/deploy/crn-2m8r-gate`
(`a2fcdea` — `origin/main` `4858187` + crn-ghn8.1's 7 commits + 2 gate-record
chores), then `git cherry-pick b04cda1` (`f753d97`) followed by
`git cherry-pick 309d8a8` (`58e3ecf`, new tip). Both cherry-picks applied with
zero conflicts. `origin/main` confirmed unchanged at `4858187` throughout.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit | PASS | `b04cda1` (crn-jfhw/crn-ghn8.2): cairn/reviewer PASS from `crn-uox9`, cites `b04cda1` exactly, `D == R`. `309d8a8` (crn-2x8o/crn-lox2): cairn/reviewer PASS from `crn-dgng`, cites `309d8a8` exactly, `D == R`. Both commits' ancestry (`537eb7e` and earlier) already covered by crn-ghn8.1's own PASS, recorded in `promoted-bead-id-writer-promote-mark-gate.md`. Full range `origin/main..58e3ecf` is therefore reviewed commit-by-commit with no gaps. |
| 2 | Acceptance criteria met | PASS | `b04cda1`: adds `cairn promote-mark <entry-id> --bead <bead-id> --store "$STORE"` immediately after `bd create` in the promote-candidate-beads step of both `formulas/mol-cairn-librarian.formula.toml` and `formulas/mol-cairn-librarian-rig.formula.toml`, applied identically per `TestLibrarianRigFormulaHasSameStepsAsLibrarian` — `PromotedBeadID` now persists, stopping repeat re-reporting of the same findings. `309d8a8`: collapses `seedCommittedEntry(t, store, topic, scope)` to `seedCommittedEntry(t, store, scope)`, hardcoding `"old-fact"` (the literal value at all 7 call sites); test-file-only. |
| 3 | Tests pass | PASS | On the final assembled `deploy/crn-2m8r-gate` (`58e3ecf`): `go build ./...` clean, `go vet ./...` clean, `go test ./...` — all 5 testable packages green. `golangci-lint run ./...` — **0 issues**, confirming `309d8a8` resolves the exact `unparam` finding (`cmd/cull_test.go:30`) that failed PR #50's required `lint` check last attempt. |
| 4 | No high-severity findings open | PASS | Neither `crn-uox9`'s nor `crn-dgng`'s review recorded open HIGH findings. |
| 5 | Final branch is clean | PASS | `git status` clean on `deploy/crn-2m8r-gate`; ahead of `origin/main` by 9 commits (7 crn-ghn8.1 + `b04cda1` + `309d8a8`); no uncommitted changes. |
| 6 | Branch diverges cleanly from main | PASS | `origin/main` re-confirmed unchanged at `4858187` immediately before cherry-picking (no rebase needed — this is a missing-commits fix on an already-correctly-based branch, not a staleness-vs-main situation). Both cherry-picks applied with zero conflicts. |
| 7 | Single feature theme | PASS | All 9 commits implement one cohesive feature arc — the `crn-ghn8` promote-mark epic: the `PromotedBeadID` writer + `promote-mark` CLI (crn-ghn8.1), wiring it into both librarian formulas (crn-ghn8.2/`crn-jfhw`), and a test-lint fix surfaced by crn-ghn8.1's own added test call sites (crn-lox2/`crn-2x8o`). Not independent, unrelated features. |

## Disposition

**GATE PASS.** Pushed `deploy/crn-2m8r-gate` (`a2fcdea..58e3ecf`, fast-forward)
to `origin`, updating the existing PR #50 in place rather than superseding it.
Proceeding to bounded CI poll, then arming `gh pr merge --auto` (no strategy
flag). On verified arm, closing both `crn-2x8o` and `crn-jfhw` against PR #50.
