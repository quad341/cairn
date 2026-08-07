# Release Gate: Instrument cairn prime with prime_emit obslog record

- Bead: crn-w9cj5 (deploy) / crn-terxf (review, PASS) / builder/crn-jkth (provenance branch, not a push target)
- Reviewer-cited commit (R): `d10cd758f7539002d57e868c7ab174e8d07c2834`
- Final deployed commit (D): `d10cd758f7539002d57e868c7ab174e8d07c2834` (D == R exactly, no rebase needed)
- Evaluated: 2026-08-06, against origin/main@2ba5035 (#77, "Docs: clarify that freshness checks detect change, not correctness")
- Downstream: crn-w9cj5 has 1 dependent bead; crn-o2unf (sling-crn-w9cj5) identified, not blocking this gate — a routing/notification bead, not a code dependency

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before this evaluation. `git merge-base origin/main D` == `2ba503524d813831f2f4a43512c042cd9c53b990`, which is origin/main's current tip itself — confirmed via `git merge-base --is-ancestor origin/main D` (true). D is a clean fast-forward of origin/main: exactly 2 commits on top (`ab9440f` test-red, `d10cd75` feat-green), touching exactly 4 files (`cmd/prime.go`, `cmd/prime_test.go`, `internal/obslog/obslog.go`, `internal/obslog/obslog_test.go`, +156/-3). No divergence, no rebase needed. **PASS.**

## 1. Exact SHA match (D == R)

R = `d10cd758f7539002d57e868c7ab174e8d07c2834`, cited identically by both the deploy bead's `commit` metadata and the review bead's (crn-terxf) `deploy_commit` field. D == R exactly, trivial match, no ancestor reasoning needed. **PASS.**

## 2. Acceptance criteria

Read the actual diff (`git diff origin/main D`) against crn-jkth's explicit acceptance criteria list rather than trusting the bead summary alone:

- `internal/obslog/obslog.go` gains `PrimeEmitFields` struct + `(*Logger) PrimeEmit(f PrimeEmitFields)`, structurally mirroring the existing `RetrievalOutcomeFields`/`RetrievalOutcome` pattern (same envelope fields: `ts`, `kind`, plus flattened struct fields) — confirmed by direct inspection, +30 lines.
- `cmd/prime.go`'s `RunE` calls `Logger.PrimeEmit(...)` exactly once, immediately after a successful `cairn.Prime(...)`, using `resolveIdentity(cmd)`/`resolveRunID(cmd)` for `IdentityTags`/`RunID` — confirmed, +13 lines, single call site.
- `ItemIDs` populated from `PrimeResult.Items[].ID` in `Prime()`'s own order; `TotalVisible`/`TruncatedCount` copied from `PrimeResult`'s matching fields — confirmed by reading the added code.
- New unit test `TestPrimeEmitRecordShape` (`internal/obslog`) covers `PrimeEmit`'s JSON marshaling (field names, `kind` discriminator).
- New integration test `TestPrimeLogsPrimeEmitRecord` (`cmd`) runs `cairn prime`, reads back the emitted debug.jsonl line, asserts `ItemIDs` matches the rendered output for the same invocation.
- No change to `RenderPrimeText`, `cairn prime --json` shape, or other user-visible behavior — existing prime tests pass unmodified; `TestAllRecordKindsProduceValidJSON` (modified, not new) still passes.
- Fail-open logging on `PrimeEmit` — confirmed by reading the call site; matches every other obslog call's error-swallowing convention.

All criteria covered by the diff; all named tests present and passing in my own independent run (criterion 3). **PASS.**

## 3. Tests

Confirmed the test command myself by reading `Makefile`'s `test:` target directly: `go test ./... -race -count=1`. Ran it on D:

- `go test ./... -race -count=1` — all 7 packages `ok`, exit 0 (including `internal/critic`, which crn-jkth's builder notes flag as carrying a pre-existing ~20%-flake test, `TestRunPerfScenarioDoesNotFail`, unrelated to this diff's scope — passed clean on this run, no flake hit).
- Re-ran with `-json`, parsed exact per-test counts via `jq`: **698 PASS, 0 FAIL, 0 SKIP**.
- Diff-owned tests confirmed individually by name: `TestPrimeLogsPrimeEmitRecord` (cmd) PASS, `TestPrimeEmitRecordShape` (internal/obslog) PASS, `TestAllRecordKindsProduceValidJSON` (internal/obslog, modified) PASS.

Review's cited count (546 PASS) differs from my fresh count (698); not investigated further since a fresh independent run on the actual deployed tree is authoritative for this gate, and both agree on 0 FAIL / 0 SKIP. **PASS.**

## 4. No open blocking findings

Review recorded 0 style findings, 0 security findings, and noted `golangci-lint` was unavailable in its sandbox. Independently re-ran on D myself: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` clean (zero output), and `golangci-lint run ./...` (available in this environment) — **0 issues**. No new dependencies, no network/deserialization/injection surface, purely additive instrumentation. No findings-labeled beads exist against either molecule (`crn-zlt0a`, `crn-v9r4d`). **PASS.**

## 5. Clean working tree

`git status --porcelain` empty both before and after the test run. **PASS.**

## 7. Single coherent theme

4 files, +156/-3, one cohesive theme: add the `prime_emit` obslog record kind and its single call site in `cairn prime`, purely additive instrumentation. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
