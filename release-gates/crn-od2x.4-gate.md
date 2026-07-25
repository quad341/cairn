# Release Gate: --json output, error categories, version/help unification (cairn CLI)

- Bead: crn-od2x.4 (deploy) / crn-od2x.2 (implementation) / crn-od2x.6 (fresh review) / crn-mfvu (reconciliation) / crn-od2x (epic)
- Reviewer-cited commit (R): 8155996b8ffed01e7b221053fa7d52e04476030d (branch builder/crn-od2x.2)
- Final deployed commit (D): 8155996b8ffed01e7b221053fa7d52e04476030d (identical — no rebase required, see criterion 6)
- Evaluated: 2026-07-25, against origin/main@be93ae7
- Third deploy attempt on this bead. Attempt 1 (PR #55 @ c4540ff9) and attempt 2 (PR #56 @ 775465c8) both failed on golangci-lint findings the original reviewer pass missed; both closed, not merged. crn-mfvu's reconciliation (this SHA) and crn-od2x.6's fresh review independently re-derive equivalent fixes for those same findings. See crn-od2x.4's bd notes for the full chain.

## 6. Clean divergence from main (evaluated first)

Clean fast-forward. `git merge-base origin/main 8155996b8f...` == `git rev-parse origin/main` exactly (both `be93ae7`). `git rev-list --left-right --count origin/main...8155996` reports `0  17` — zero commits on main not already in this branch, seventeen commits ahead. No self-rebase needed; `attempt_bounded_self_rebase` was not invoked. **PASS.**

## 1. Exact SHA match (D == R)

D and R are the literal same commit, `8155996b8ffed01e7b221053fa7d52e04476030d`. crn-od2x.6 reviewed exactly this SHA and returned PASS (SHA-pinning mandate, crn-fie5, trivially satisfied). **PASS.**

## 2. Acceptance criteria

Epic crn-od2x's acceptance criteria: "Core agent-facing commands support documented JSON output with stable field names; structured output contains raw freshness state rather than prose-only status; representative success and failure shapes are tested; human output remains the default and backward-compatible."

crn-od2x.6's review independently verified each clause via direct code inspection (not the builder's self-report): stable snake_case JSON tags across `PrimeResult`/`StatusItem`/`EntryResult`/`MapResult`/`MapTopicCount`; `FreshnessInfo{Status, Detail}` carries the raw machine status separately from human prose; success/failure/edge-case shapes covered by `TestPrimeJSON*`, `TestStatusJSON*`, `TestGetJSON*`, `TestMapJSON*`; `wantsJSON(cmd)` gating confirmed on every wired command with text renderers preserved for the default path.

Independently spot-checked three of the cited tests exist at their exact cited names in this SHA's tree (not just in the review's prose): `TestPrimeJSONOutputsResult` (cmd/prime_test.go:30), `TestClassifyClassifiedError` (cmd/format_test.go:18), `TestGetJSONNotFoundEmitsErrorEnvelope` (cmd/commands_json_test.go:128) — all present. **PASS.**

## 3. Tests

Fresh run in an isolated worktree pinned to `8155996` (not trusted from prior notes):

- `gofmt -l .` — clean
- `go build ./...` — OK
- `go vet ./...` — OK
- `go test ./... -race -count=1` (matches CI's own invocation) — all packages pass: `cmd` 8.977s, `formulas` 1.035s, `internal/cairn` 12.823s, `internal/critic` 7.262s, `scripts` 2.341s. Neither of the two previously-dispositioned flaky tests (`TestConcurrentFindAndReindexDoNotHardFail` in internal/cairn — SQLITE_BUSY timing; `TestRunPerfScenario*` in internal/critic — perf-threshold/TempDir) fired this run.
- `golangci-lint cache clean && golangci-lint run ./...` — 0 issues. This is the exact gate that failed both prior attempts (4 findings: 2x `lll` in cmd/reviewer.go, 1x `revive` in cmd/format.go, 1x `unparam` in cmd/remember_test.go); confirmed clean on this SHA with a freshly-cleared cache (avoids the known shared-cache staleness gotcha).

**PASS.**

## 4. No open blocking findings

crn-od2x.6's review recorded zero blocking findings. One non-blocking finding (mayor: `cairn.Prime`'s `budgetBytes<=0` has no "unlimited" sentinel, so `--json` now inherits the existing 8192-byte default cap) was independently confirmed by the reviewer to be byte-for-byte pre-existing on `origin/main` since PR #57 — not a regression introduced by this diff — and filed separately as `crn-od2x.7` (P3, non-blocking, ready-to-build) per the mayor's explicit request.

Independently queried bd for open HIGH-severity issues fleet-wide: zero open P0. Open P1s (`crn-2sdw`, `crn-633u`, `crn-6qbb`, `crn-7vrp`, `crn-ph0i`, `crn-tq8s`, `crn-vm4g`) are unrelated fleet/tooling infrastructure bugs — none reference crn-od2x.2/.4/.5/.6 or touch this diff. Dependent beads `crn-od2x.3` (cmd/list.go JSON gap) and `crn-od2x.7` are both explicitly filed as separate, non-blocking follow-ups. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty in the isolated worktree at `8155996`. **PASS.**

## 7. Single coherent theme

`--json` output mode + four stable error categories + version/help unification, wired into get/status/map/prime/remember, is one coherent theme under the crn-od2x epic. The only content beyond crn-od2x.2's original reviewed scope is crn-mfvu's reconciliation merge with `origin/main` (PR #54's Incomplete freshness status + PR #57's budget-bounded Prime) — a same-theme mergeability fix forced by two already-merged siblings touching the same output paths (cmd/commands.go, internal/cairn/prime.go, internal/cairn/entry.go, internal/cairn/freshness.go), not an independent feature bundled in. **PASS.**

## Verdict: GATE PASS — proceeding to isolated deploy branch push + PR.
