# Release Gate: shadowmap-status-shadow-detection

**Bead:** crn-811 (re-cut, step 2/2) — original deploy bead crn-eat (closed) —
source review crn-dgh — design crn-4bd.2 — test-coverage follow-up crn-caz (closed)
**Commit set:** `cc5bca2148f7cbf6f954eac1ce07adf7837504ad` (ShadowMap feature),
`6f96e9f8eba8c7e47da2a7748132220e4980e914` (bestShadower extraction),
`633c2f73f38b5b83f4c21afdd76c8d2b9db8863a` (crn-caz CLI-wiring test coverage)
— cherry-picked in this order onto a fresh branch cut from `origin/main`
**Branch:** `deploy/crn-eat-shadowmap`, HEAD `02f806325509025c4e62dbaf276e15c1aa5aff08`
**Date:** 2026-07-21

## Why a re-cut (supersedes the `deploy/crn-eat-gate` / PR #6 gate)

The original `crn-eat` gate branched *directly at* `6f96e9f` on top of the
reviewed branch's own history, which meant it also stacked three unrelated,
independently-gated commits (`beaaee1` topic-key, `284ea1d` identity guard,
`45b2623` `get <id>`) that hadn't landed on `main` yet at the time — that
became PR #6, one of the multi-bead tangles crn-811 exists to clean up.

Topic-key (`beaaee1`, PR #7) is now merged to `origin/main` (`8e04de2`), so
`moreSpecific()` — the function `cc5bca2` calls — already exists on `main`.
That removes the only real dependency forcing the stack, so this gate cuts
fresh from `origin/main` and cherry-picks **only** the three ShadowMap-theme
commits. Per mayor's explicit instruction (crn-811 comment, 2026-07-21
19:19), the third commit (`633c2f7`, crn-caz's CLI-wiring test coverage —
previously stranded, unpushed, on validator's ephemeral branch
`gc-validator-3f0b4674badd`) is included so the deploy doesn't ship without
its tests a second time.

## Note on a missing mechanical guard

`scripts/rebase-resolve-lib.sh` (the `resolve_deploy_branch_target` /
`assert_safe_push_target` helpers this role's process calls for) is not
present in this repo. Checked by hand instead:

- Target `deploy/crn-eat-shadowmap` is non-empty and was chosen explicitly
  by the routing bead (crn-811), not hand-named ad hoc.
- Does not match the shared-worktree pattern `^gc-[A-Za-z0-9._-]+-[0-9a-f]{12}$`.
- Confirmed via `git ls-remote origin refs/heads/deploy/crn-eat-shadowmap`
  (empty result) that the name did not already exist locally or on `origin`
  before creation — pushing it cannot clobber any existing ref.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Branch cut via `git checkout -b deploy/crn-eat-shadowmap origin/main` at `122bacf` (which already includes topic-key `#7` and merge_group `#8`) — starts identical to `main`. All three cherry-picks (`cc5bca2`, `6f96e9f`, `633c2f7`) applied with zero conflicts, no manual resolution, no self-rebase needed. |
| 1 | Review PASS present | PASS | `cc5bca2` + `6f96e9f`: reviewed + PASSED by cairn/reviewer (crn-dgh, focused re-review 2026-07-20, verdict PASS against `6f96e9f8eba8c7e47da2a7748132220e4980e914`). `633c2f7`: test-only (zero production code), added per crn-caz (closed) directly to satisfy crn-dgh's own Finding #2, and its inclusion here was explicitly mandated by mayor — no separate review bead exists for it, appropriate given it's test-only and adds coverage for behavior already reviewed and passed in the other two commits. |
| 2 | Acceptance criteria met | PASS | FR-1..FR-7 (crn-4bd.2 design doc) reconfirmed: (a) full automated suite green — 6 `ShadowMap`/`scopeSuperset` unit tests in `internal/cairn/entry_test.go` plus `TestStatusAnnotatesShadowedEntries` + `TestStatusNoAnnotationWithoutShadow` in `cmd/commands_test.go`; (b) guardrail §13 — `grep -rn "ShadowMap("` shows exactly one production call site, `cmd/commands.go:42`, inside `statusCmd`; (c) identity guard not regressed — see live smoke test below; (d) **independent live binary smoke test** (built the binary fresh, not relying on `go test`): seeded `less-specific` (`topic_key=shared`, `scope=[rig:alpha]`) and `more-specific` (`topic_key=shared`, `scope=[rig:alpha,role:investigator]`) and ran `cairn status --store <dir>` — output annotates `less-specific`'s line with `[SHADOWED BY more-specific]` and leaves `more-specific`'s line unannotated, exactly as designed. Also ran `status --identity rig:alpha` and `CAIRN_IDENTITY=rig:alpha status` — both correctly rejected with `"status is unscoped and does not filter by identity"` (exit 1). |
| 3 | Tests pass | PASS | Fresh run on `deploy/crn-eat-shadowmap` (HEAD `02f8063`): `go build ./...` clean; `go vet ./...` clean; `gofmt -l .` empty; `go test ./... -race -count=1 -cover` → all pass, `cmd` 22.0% / `internal/cairn` 82.3% coverage (internal/cairn matches the superseded gate's reported 82.3% exactly; `cmd` rose from that gate's 17.0% to 22.0% because `633c2f7`'s tests are now included instead of stranded); `golangci-lint run ./...` → 0 issues. |
| 4 | No high-severity findings open | PASS | The one blocking finding from crn-dgh (gocognit complexity 28>25 on `ShadowMap`) was resolved by `6f96e9f` and reconfirmed via clean lint above. The non-blocking finding (crn-dgh Finding #2: CLI-wiring test coverage) is exactly what `633c2f7` closes — no longer open. Zero HIGH findings remain against this commit set. |
| 5 | Final branch clean | PASS | `git status` clean immediately after the three cherry-picks — no modified/staged files; only pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) untracked. |
| 7 | Single feature theme | PASS | Exactly three commits, one subsystem (shadow-detection annotation on `cairn status`): `cc5bca2` (`cmd/commands.go` + `internal/cairn/entry.go` + `entry_test.go` — add `ShadowMap`/`scopeSuperset`, wire into `status`), `6f96e9f` (`internal/cairn/entry.go` only — mechanical `bestShadower` extraction, zero behavior change), `633c2f7` (`cmd/commands_test.go` only — test coverage for the wiring `cc5bca2` added). Unlike the superseded `deploy/crn-eat-gate`, this branch does not stack unrelated commits — topic-key, identity-guard, and `get <id>` are already independently merged to `origin/main` (`#7`, and pre-existing `#4`/`#5`) and this branch was cut fresh from that tip, so the PR diff is exactly these three commits. |

## Verdict: PASS — proceeding to PR. Per crn-811's explicit instruction, merge authority is mayor — auto-merge will NOT be armed; a merge-request will be routed to mayor instead (same pattern as step 1/PR #7).
