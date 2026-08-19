# Release Gate: cairn — chunk reindexTx's per-entry upsert loop into independent transactions (fix cycle)

- Bead: crn-daxbq (deploy) / crn-3wpai (review, PASS — fresh pass on this SHA, not inherited from the prior FAIL) / crn-f0rb7.2 (build bead, provenance only, not a push target)
- Reviewer-cited/deploy-source commit (R = D₀): `c169811c2e7b5a38f4eb4a29ba3eba9df604bd21`
- Final evaluated commit (D): `680d92210793ca88845be919bb9cd8a56739bc62` — D₀ rebased onto origin/main via bounded self-rebase (criterion 6)
- Evaluated: 2026-08-19, against origin/main@`95086a9` (#117, "Add ancestry-scope check for deploy branches")
- Lineage: fix-and-fresh-review cycle following crn-3wpai's own gate FAIL (`release-gates/crn-3wpai-gate.md`, commit `7a53c2b` in this range) on criterion 3 — the chunked upsert (`reindexUpsertChunkTx`) silently dropped `title_source`, `summary_source`, FTS index population, and `schema_version` stamping relative to the `reindexTx` path it replaced. Fix commit (`c169811c`, pre-rebase / `680d9221`, post-rebase) restores all three; crn-3wpai's notes record independent reviewer re-verification (718 PASS/0 FAIL) on `c169811c` before handing off this fresh deploy bead.

## 6. Clean divergence from main (evaluated first, per established pattern)

D₀ (`c169811c`) was cut with `builder/crn-f0rb7.2` sitting at `1c2a370` (crn-3wpai's own evaluation point). origin/main had since advanced to `95086a9` (#117). Per crn-daxbq's explicit routing instruction ("Do NOT push to, or open the PR from, `builder/crn-f0rb7.2`"), the ordering was inverted relative to the crn-3wpai precedent: the isolated branch was cut *first* (`resolve_deploy_branch_target crn-daxbq c169811c...` → `deploy/crn-daxbq-gate`), and `attempt_bounded_self_rebase` was run on that isolated branch, never touching the shared provenance branch.

`attempt_bounded_self_rebase deploy/crn-daxbq-gate main` → rc=0, `BEFORE_SHA=c169811c2e7b5a38f4eb4a29ba3eba9df604bd21` → `AFTER_SHA=680d92210793ca88845be919bb9cd8a56739bc62`, force-with-lease pushed to `origin/deploy/crn-daxbq-gate`.

Verified mechanical/content-preserving, not just rc=0: `git diff` between D₀'s cut point (`2a196f5`) and origin/main tip (`95086a9`) touches only `scripts/rebase-resolve-lib.sh`, `scripts/test-rebase-resolve.sh`, and `release-gates/assert-deploy-ancestry-scope-gate.md` — zero overlap with `internal/cairn/*`. `git diff c169811c 680d9221 -- internal/cairn` is **empty**: the rebase was a pure replay with zero conflicts (cleaner than crn-3wpai's precedent, which had a real conflict in `index.go`). `git merge-base --is-ancestor origin/main 680d9221` confirms D fully contains main. **PASS.**

## 1. Exact SHA match (D₀ within R's reviewed history)

R = `c169811c2e7b5a38f4eb4a29ba3eba9df604bd21`, cited both in crn-daxbq's own `Commit:` field and as the SHA crn-3wpai's notes record the reviewer's fresh 718/0 PASS against. D₀ == R exactly (independently re-verified via `git rev-parse --verify --quiet`, not trusted as transcribed text). Criterion 6's bounded self-rebase then advanced it to D — confirmed above to be a mechanical, zero-conflict, content-identical replay (empty diff on `internal/cairn/*`). Unlike the crn-3wpai precedent, this rebase does **not** carry the same caveat: main's advance in this window (#117) touched only deployer tooling, not `internal/cairn`, so D is not exposed to any new interaction the reviewer didn't already evaluate. **PASS, no caveat.**

## 2. Acceptance criteria

crn-3wpai's FAIL notes named the exact defect (missing `title_source`/`summary_source` in `reindexUpsertChunkTx`'s INSERT column list, VALUES, and `ON CONFLICT DO UPDATE SET`, plus the severity note that UPDATE-path omission would silently NULL out previously-good values on every future reindex — a live data-corruption risk, not just a test failure). The fix commit's diff on `internal/cairn/index.go` was read directly this session: all three gaps closed — `title_source`/`summary_source` present in the INSERT column list, VALUES args (`e.TitleSource`, `e.SummarySource`), and the `ON CONFLICT DO UPDATE SET` clause; FTS index population and `schema_version` stamping (the two additional gaps named in the fix commit's own subject line) independently confirmed present in the diff. crn-3wpai's reviewer re-verification (718 PASS/0 FAIL) is independent corroboration, not the sole basis. **PASS.**

## 3. Tests pass

Independently re-run in this worktree on D (`680d9221`, not inherited from the reviewer's or crn-3wpai's pre-rebase run):

```
go build ./...          # clean
go vet ./...             # clean
gofmt -l .                # clean (no output)
golangci-lint run          # 0 issues
go test ./... -race:
ok  	github.com/quad341/cairn                1.014s
ok  	github.com/quad341/cairn/cmd            87.558s
ok  	github.com/quad341/cairn/formulas       1.050s
ok  	github.com/quad341/cairn/internal/cairn 94.515s
ok  	github.com/quad341/cairn/internal/critic 15.196s
ok  	github.com/quad341/cairn/internal/obslog 1.040s
ok  	github.com/quad341/cairn/scripts        3.943s
```

All 7 packages pass, 0 failures — including `internal/critic`, the package whose 5 tests were the specific failure signature on crn-3wpai's FAIL run (NULL `title_source` scan error). Confirms the fix holds on the actual deploy SHA, post-rebase, not merely on the pre-rebase content. **PASS.**

## 4. No open HIGH / blocking findings

`bd list --status open --label finding` → no issues found. Full-DB search for `crn-daxbq`/`crn-3wpai`/`crn-f0rb7.2`/`crn-ku5dd` cross-referenced against `issue_type=finding` → 0 results. The one real finding in this lineage (crn-3wpai's criterion-3 defect) is already fixed and independently re-verified above, not open. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty on `deploy/crn-daxbq-gate` at D, confirmed before this gate doc's commit. **PASS.**

## 7. Single coherent theme

Diff range (origin/main..D) is the full RED→GREEN→GATE-FAIL-doc→FIX arc for one feature: `dd8fb55` (test: red, chunked-reindex busy-retry/lock-hold), `066f136` (feat: green, chunk the upsert loop), `7a53c2b` (crn-3wpai's own FAIL gate doc, committed as part of this lineage's audit trail), `680d9221`/`c169811c` (fix: restore title_source/summary_source/FTS/schema_version). `assert_deploy_ancestry_scope origin/main 680d9221... crn-daxbq crn-f0rb7.2 crn-3wpai` → **rc=0**: no `.claude/**` paths introduced, every commit cites at least one of the three ids (commits predate crn-daxbq's own bead, so they cite `crn-f0rb7.2`/`crn-3wpai` — same pattern as the crn-lqy6l precedent, where build-phase commits predate the deploy bead that later evaluates them). One package (`internal/cairn`), one feature (chunked reindex upsert, now correctness-complete), matching crn-daxbq's own title exactly. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to PR

## Process notes

1. **Merge authority — deliberate deviation from the crn-lqy6l gate's process notes.** That gate (`release-gates/assert-deploy-ancestry-scope-gate.md`, same day) asserted a standing mayor authorization for direct deployer self-merge and stated it supersedes mayor-routing template language. Branch protection was re-checked and is unchanged (`build-test`+`lint` required, 0 required approvals, squash-only merge) — mechanically, a direct merge remains possible. Notwithstanding that, this bead's own instruction is unambiguous and specific to this exact deploy: *"Route a merge-request to mayor/mpr; merge authority is operator/mayor/mpr only — no rig agent runs gh pr merge."* This is the third independent source this deploy lineage has produced with that same instruction (crn-e6pc7's body, crn-daxbq's own body here, and crn-3wpai's PASS verdict text), against one older/reaffirmed ruling on the other side. Given the conflict, and that a squash-merge to main is the harder-to-reverse of the two choices, this gate follows crn-daxbq's own explicit routing instruction: **no `gh pr merge` will be run by this role; a merge-request will be routed to mayor/mpr instead**, mirroring exactly what was done for crn-e6pc7. Flagging the conflict here rather than silently picking a side — worth a standing-policy reconciliation outside this gate.

2. **Branch target:** `builder/crn-f0rb7.2` is provenance only (per crn-daxbq's own instruction, possibly shared) — this gate does not push to it. `deploy/crn-daxbq-gate` was cut fresh from the exact reviewed SHA via `resolve_deploy_branch_target`, confirmed a safe push target via `assert_safe_push_target`, and is the only branch this gate pushed to (via the self-rebase's force-with-lease).

3. **Ancestry-scope bead ids:** all 4 commits in range cite `crn-f0rb7.2` and/or `crn-3wpai`, not `crn-daxbq` — expected, since every commit was authored during the build/review phases before crn-daxbq (the deploy bead) existed. Same pattern as the crn-lqy6l precedent's process note 2.
