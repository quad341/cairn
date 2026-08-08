# Release Gate: Replace payload_tokens byte/4 estimate with a real or calibrated measurement

- Bead: crn-f2lde (deploy) / crn-15a44 (review, PASS) / gc-builder-d53f3747bdfa (provenance branch, not a push target)
- Reviewer-cited commit (R): `3eca671b9970d45ab98a25e601ccf98df2aa177f`
- Final deployed commit (D): `3eca671b9970d45ab98a25e601ccf98df2aa177f` (identical to R — origin/main was already an ancestor, no self-rebase required)
- Evaluated: 2026-08-07, against origin/main@5b75b0f (#79, "Add a recall hit-rate measurement skill")
- Downstream: crn-6vxuh (sling-crn-f2lde) is a routing convoy blocked on this bead closing — not a merge dependency, not blocking this gate
- Parent: crn-666s ("Telemetry gaps that must close before the ON/OFF verdict can be trusted") stays open after this ships — it also tracks GAP 2 and GAP 3, out of scope for this bead (crn-666s.1 covered GAP 1 only)

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` run as part of worktree freshen immediately before this evaluation. origin/main tip is `5b75b0f`. `git merge-base 3eca671b9970d45ab98a25e601ccf98df2aa177f origin/main` returns `5b75b0f` exactly — origin/main is a direct ancestor of R, which sits exactly 1 commit ahead / 0 behind. No bounded self-rebase needed. **PASS.**

## 1. Exact SHA match (D == R)

R = `3eca671b9970d45ab98a25e601ccf98df2aa177f`, recorded as `deploy_commit` on crn-15a44 (review) and matching crn-f2lde's own `Commit:` field in its description, and matching `origin/gc-builder-d53f3747bdfa`'s tip exactly (three-way agreement). The isolated deploy branch (`deploy/crn-f2lde-gate`) was cut directly from R via `resolve_deploy_branch_target`, not from the shared provenance branch. No rebase was applied (criterion 6 was already structurally satisfied), so D == R literally, exact match. **PASS.**

## 2. Acceptance criteria

Read crn-666s.1's `exit_contract` directly rather than relying solely on the reviewer's or builder's summary, then independently read the full diff (`git show 3eca671b`) to confirm both items in code, not notes:

1. **"A test pins PayloadTokens to ... a calibrated estimate with an explicit, measured error bound recorded and surfaced (not silently dropped) alongside the value."** Confirmed in `internal/obslog/obslog.go`: `RetrievalOutcomeFields` gained `PayloadTokensMethod string` and `PayloadTokensErrorBoundPct int` as real struct fields, both written into the `retrieval_outcome` slog record (`obslog.go:338-339`), not left in a comment. `cmd/commands.go` sets them to `"calibrated_chars_per_token_v1"` and `17` from named constants (`payloadTokenCharsPerToken = 2.64`, `payloadTokensErrorBoundPct = 17`) whose doc comment states the calibration methodology (n=418 text-only assistant turns, grouped by `message.id`, median 2.64 chars/token, p5=2.27/p95=3.09). `cmd/commands_test.go` asserts `payload_tokens_method == "calibrated_chars_per_token_v1"` and `payload_tokens_error_bound_pct > 0` on a hit, both zero-valued on a miss. Met.
2. **"len(e.Body)/4 with no error accounting no longer ships as the value."** Confirmed: `cmd/commands.go`'s `getCmd` now computes `PayloadTokens: int(float64(len(e.Body)) / payloadTokenCharsPerToken)` (2.64, not 4), with the method/error-bound fields traveling alongside it. The old bare `len(e.Body) / 4` is gone from the diff, not just relabeled. Met.

Both `exit_contract` items independently confirmed against the actual code diff. **PASS.**

## 3. Tests

Canonical command per `Makefile`'s `test:` target and `.github/workflows/ci.yml`'s `build-test` job: `go test ./... -race -count=1`. Ran independently on D:

- `go test ./... -race -count=1` — all 7 packages (`.`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`) report `ok`, exit 0.
- Diff-owned tests in `cmd/commands_test.go`, re-run individually by name: `TestGetLogsRetrievalOutcomeHitOnSuccess` — PASS (asserts `payload_tokens_method`/`payload_tokens_error_bound_pct` on a hit); `TestGetLogsRetrievalOutcomeMissOnNotFound` — PASS (asserts both zero-valued on a miss). Matches crn-15a44's reviewer report by name and outcome exactly.

The reviewer's crn-15a44 report cited 562 Go tests PASS/0 FAIL/0 SKIP; my run shows a clean `ok` on every package with no `FAIL` markers, consistent with no regression. Builder notes on crn-666s.1 document one pre-existing, unrelated flake (`TestRunPerfScenarioDoesNotFail`, `internal/critic`, `t.TempDir` cleanup race unlinking `.git/objects`) — already tracked separately (crn-b9iq, crn-ysoo, crn-u8c0, all open, none diff-owned since this diff touches zero files under `internal/critic`); it did not manifest in this run (`internal/critic` passed clean in 29.156s). **PASS.**

## 4. No open blocking findings

Diff is a pure telemetry-precision change (`cmd/commands.go`, `internal/obslog/obslog.go`, `cmd/commands_test.go`) — no new I/O, network calls, endpoints, auth logic, shell/SQL concatenation, deserialization, or dependencies (`go.mod`/`go.sum` untouched). The new logged fields are a hardcoded provenance string and a hardcoded int constant, not derived from request/entry content — no new PII/credential/token exposure; `payload_tokens` itself was already logged pre-diff, only its calibration and precision-labeling changed. Reviewer (crn-15a44) recorded style/security/spec findings as clean across every OWASP-style category checked, zero HIGH findings. Independently re-ran on D: `go build ./...` clean, `go vet ./...` clean, `gofmt -l cmd/commands.go cmd/commands_test.go internal/obslog/obslog.go` clean (zero output), `golangci-lint run ./...` (matches the CI lint job) — **0 issues**. Independent bd search for finding-type beads referencing crn-666s.1/crn-15a44/crn-f2lde: none found. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty on `deploy/crn-f2lde-gate` at D, confirmed immediately before writing this doc. **PASS.**

## 7. Single coherent theme

3 files touched, all part of one cohesive change: `cmd/commands.go` (+27/-6), `cmd/commands_test.go` (+7), `internal/obslog/obslog.go` (+57/-3, mostly the calibration-methodology doc comment). Every hunk is part of replacing the uncalibrated `len(e.Body)/4` estimate with the calibrated `2.64` constant plus its surfaced error bound. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
