# Release Gate: Branch-aware discovery: Reindex/IterEntries read pending remember/* branches

- Bead: crn-hy7ll (deploy) / crn-8co68 (review, PASS) / crn-evw98.2 (build bead, closed — provenance, not a push target)
- Reviewer-cited commit (R): `5281f2e08cacf4689a682407c59db6f1b5edce2d`
- Deploy commit (D): `5281f2e08cacf4689a682407c59db6f1b5edce2d` (identical to R — no rebase needed, see criterion 6)
- Branch: `builder/crn-evw98.2`
- Evaluated: 2026-08-19, against `origin/main`@`aa484f2` (#122, "cairn search: summary re-truncation no longer cuts mid-word")

## Pre-flight: has target already merged?

`gh api repos/quad341/cairn/commits/5281f2e08cacf4689a682407c59db6f1b5edce2d/pulls` → `[]` (empty). No PR exists yet for D. **CLEAR** — proceed with normal flow.

## 6. Clean divergence from main (evaluated first)

`git merge-base --is-ancestor origin/main 5281f2e0...` → yes. D's parent chain fast-forwards cleanly from `origin/main`@`aa484f2`; no rebase required. **PASS.**

## 1. Exact SHA match (D within R's reviewed history)

R = `5281f2e08cacf4689a682407c59db6f1b5edce2d`, recorded as both crn-hy7ll's own `metadata.commit` and crn-8co68's review verdict (`tdd_green: 5281f2e08cacf4689a682407c59db6f1b5edce2d`). D == R exactly, independently re-resolved via `git rev-parse --verify --quiet "5281f2e0...^{commit}"`. **PASS.**

## 2. Acceptance criteria

crn-evw98.2's exit_contract (from crn-evw98's design) requires: (a) `Reindex`/`IterEntries` also enumerate open `remember/*` branches and read content via the `ShowReviewBranch` pattern; (b) 4 named pinned tests flip from fail-loudly to succeed-via-branch-read; (c) new coverage for multiple pending branches, a branch whose entry was later merged (no double-count), and a branch for an entry later closed/abandoned; (d) depends on crn-evw98.1's `review_status` marker — branch-sourced reads must stamp `review_status=pending`.

crn-8co68 (reviewer) independently cross-checked all 4 items against the diff and confirmed each satisfied. Independently re-verified directly against D in this session:
- `internal/cairn/entry.go`: `reviewBranchEntries`/`ListReviewBranches`/`reviewBranchContent` present and wired into the discovery path (comments explicitly cite "crn-evw98's fix for the gap").
- All 4 pinned tests exist under their renamed, outcome-describing names and are in the green 743/0/0 run (see criterion 3): `TestWriteBackRecurrenceCountSucceedsViaReviewBranch`, `TestWriteBackPromotedBeadIDSucceedsViaReviewBranch` (internal/cairn/remember_test.go), `TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch`, `TestRememberForceOverridesRecurrenceMatchSharedTier` (cmd/remember_test.go). No leftover fail-loudly (`IsNotExist`/`require.Error`) assertions found in the first of these.

**PASS.**

## 3. Tests

Independently re-run on D in this worktree: `go build ./...` clean; `go test ./... -race -count=1` → **743 PASS, 0 FAIL, 0 SKIP** (both plain and `-v` runs), exactly matching crn-8co68's own independently-reported tally. Diff-owned test files (16, via `git diff --name-only origin/main...5281f2e0 -- '*_test.go'`) all accounted for in the green run; global SKIP=0 rules out any diff-owned test silently skipping.

**Sub-part 3b (policy/lint lane) — FAIL.** Per `.github/workflows/ci.yml`'s `lint` job, the documented CI-equivalent command is `golangci-lint run ./...` (golangci-lint v2, matching the v2.12.0 available locally). Run independently against D:

```
internal/cairn/remember.go:373:1: cyclomatic complexity 16 of func `commitToReviewWorktree` is high (> 15) (gocyclo)
cmd/list.go:50:1: line is 141 characters (> 140 characters) (lll)
internal/cairn/prime.go:354:2: nested if statements exceed 6 (nestif)
```

These are the **identical 3 issues** already failing sibling bead crn-8lq2z's GitHub CI (PR #123, same file:line, same linters). Attribution:
- `cmd/list.go:50` (lll) and `internal/cairn/prime.go:354` (nestif): diffstat comparison confirms D's diff against `origin/main` is byte-identical to crn-8lq2z's own reviewed SHA (`c3487508`) in these two files — **100% inherited from crn-evw98.1, zero crn-evw98.2 contribution.**
- `internal/cairn/remember.go:373` (gocyclo, `commitToReviewWorktree`): the flagged function originates in crn-evw98.1, but crn-evw98.2 adds its own lines to this same file (61 lines changed in D vs. 19 in `c3487508`) — partially compounded by crn-evw98.2, not purely inherited.

Pre-existing-failure attribution (criterion 3a) does not rescue this: `origin/main` does not contain this code at all (introduced fresh by crn-evw98.1's own unmerged commits), so there is no host/environment cause to attribute to — this is diff-introduced. Reviewer notes (crn-8co68) confirm the review process never ran the lint lane at all (`style_findings` covers only gofmt/vet/build/manual-read), so this is the first point in the pipeline this was checked.

**FAIL — this is the blocking finding for the overall gate.**

## 4. No open HIGH findings

crn-8co68: `security_findings: none` (all 9 OWASP categories walked explicitly), `style_findings: none`, `verdict: pass`, no blocker/major/minor findings recorded. `bd list` sweep for the chain (crn-hy7ll, crn-8co68, crn-evw98.2, crn-evw98.1, crn-evw98) surfaces no separate open finding bead. **PASS** (on pre-existing findings) — moot given criterion 3b; this gate evaluation itself is what surfaced the lint issue, now being routed forward.

## 5. Clean working tree

`git status --short` empty on `builder/crn-evw98.2` at D. **PASS.**

## 7. Single feature theme

crn-evw98.2 formally `blocks`-depends on crn-evw98.1 in the bd dependency graph — an intentional, tracker-documented sequencing (parent bead crn-evw98's design doc lays out branch-aware discovery as depending on the `review_status` marker crn-evw98.1 introduces), not incidental scope contamination. D's diff against `origin/main` carries crn-evw98.1's commits because crn-evw98.2 was built directly on top of them, exactly as designed — the same underlying content sibling bead crn-8lq2z already has under review for its own deploy. One coherent feature (branch-aware discovery, part 2 of 2), correctly sequenced on its documented dependency. **PASS.**

## Verdict: GATE FAIL (criterion 3b — policy/lint lane) — NOT deployed

No deploy branch was cut; no PR was opened. Routed to cairn/builder for a fix (see bd notes + mail for the actual handoff).

**Recommendation for builder:** 2 of the 3 lint violations (`cmd/list.go:50`, `internal/cairn/prime.go:354`) are identical, unmodified inheritance from crn-evw98.1 — already tracked as crn-8lq2z's own gate failure on PR #123, currently sitting with builder. Fix crn-evw98.1/crn-8lq2z's branch once, then rebuild/rebase crn-evw98.2 on top, rather than duplicating the fix independently across both branches (which would produce two divergent fixes for the same `list.go`/`prime.go` lines). The 3rd violation (`internal/cairn/remember.go:373`, gocyclo in `commitToReviewWorktree`) will need its own attention on crn-evw98.2's branch regardless, since crn-evw98.2 adds further lines to that same function on top of crn-evw98.1's original.
