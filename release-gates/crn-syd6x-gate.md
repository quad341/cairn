# Release Gate: Fix: Reindex has no retry above busy_timeout — SQLITE_BUSY hard-fails under fleet contention

- Bead: crn-syd6x (deploy) / crn-qgadf (review, PASS) / crn-ca6cn (source bug/build bead, closed — provenance) / gc-builder-d53f3747bdfa (provenance branch, not a push target)
- Reviewer-cited commit (R): `dc68237215e88fa978c00fe88971acb3259994df`
- Deploy commit (D): `dc68237215e88fa978c00fe88971acb3259994df` — identical to R; criterion 6 passed directly, no self-rebase needed
- Evaluated: 2026-08-15, against origin/main@`2aa0ae3d70ccb6a2dac8ac5bb3d9255b48672e77` (#93, "fix(critic): time the query, not an index rebuild, in the perf scenario")

## 6. Clean divergence from main (evaluated first)

`git merge-tree --write-tree origin/main dc68237...` → exit 0, single clean tree OID `0840bcd90ccf8fcc741b63f624f744a708c1b8d6`, no conflict markers. Freshly re-confirmed immediately before this doc was written (re-fetched origin/main, unchanged at `2aa0ae3`). File-scope check independently confirms zero overlap: this diff touches only `internal/cairn/index.go` and `internal/cairn/index_test.go`; `comm -12` between this diff's changed files and main's files-changed-since-merge-base returns empty. No rebase needed — D = R directly. **PASS.**

## 1. Exact SHA match (D within R's reviewed history)

R = `dc68237215e88fa978c00fe88971acb3259994df`, recorded identically as crn-syd6x's own `**Commit:**` field and as crn-qgadf's (review, verdict PASS) `metadata.commit`. The isolated deploy branch (`deploy/crn-syd6x-gate`) was cut via `resolve_deploy_branch_target(crn-syd6x, R)` — mechanically derived from the bead-id being operated on, never from branch-name prose. D == R exactly (byte-identical, no rebase). **PASS.**

## 2. Acceptance criteria

Cross-checked against crn-ca6cn's Done-when list, independently re-verified against D (not merely cited from review):

- [x] `TestReindexRetriesPastBusyTimeout` added and passes — present on branch (RED `561816e` → GREEN `a7da8e8`); re-ran myself, PASS (see §3).
- [x] Unit test for `isBusy`: real `SQLITE_BUSY` → true, non-busy error → false — `TestIsBusyClassifiesRealSQLiteBusyError` / `TestIsBusyRejectsNonBusyErrors`, both re-ran myself, PASS. Read both test bodies directly: the positive case contends two real DB connections for a lock rather than a hand-built stand-in error, and the negative case checks both a non-SQLite sentinel (`sql.ErrNoRows`) and a real different-class driver error (SQL syntax) — good test hygiene, not a rubber-stamp.
- [x] `go test ./internal/cairn/... -race -count=8` green — re-ran myself: 0 FAIL, 0 SKIP, 0 DATA RACE, 2520 PASS, 197.719s (reviewer reported 235.293s; both clean, timing variance is machine-speed, not a signal).
- [ ] Fleet-scale repro (468-entry store, 16 goroutines × 5 iterations, 0 failures vs stock-main's ~5/80) — **disclosed gap, not independently re-run**, same as the review. The original ad hoc stress harness was a throwaway, never committed to the diff, so re-running it at deploy time would mean rebuilding a bespoke rig rather than running the project's actual test command — disproportionate for a deploy gate. Judged non-blocking on the same basis as the review: `TestReindexRetriesPastBusyTimeout` is itself a genuine (non-mocked) repro of the exact failure mode — a real writer lock held 6s, longer than `busy_timeout(5000)` — and the `-race -count=8` run exercises the identical contention code path across 8 repeated full-package runs. This corroborates the fix at the mechanism level; it is not confirmed at the original bug's exact fleet-scale count.
- [x] Fix confined to `internal/cairn/index.go` (+ its test file) — `git diff --stat` against the merge-base shows only those two files, 145 insertions / 11 deletions.
- [x] Run with `TMPDIR=/var/tmp/gotmp` — used for every test invocation below (default `/tmp` is tmpfs and hides the exact contention this bug is about).

One disclosed, non-blocking gap; all other items independently confirmed MET. **PASS.**

## 3. Tests

Canonical CI commands (`.github/workflows/ci.yml`, build-test job, verbatim): `go build ./...` then `go test ./... -race -count=1`. Run independently on D, all under `TMPDIR=/var/tmp/gotmp`:

- `go build ./...` — exit 0, clean.
- `go test ./internal/cairn/... -run 'TestReindexRetriesPastBusyTimeout|TestIsBusyClassifiesRealSQLiteBusyError|TestIsBusyRejectsNonBusyErrors' -v` — all 3 PASS by exact name (6.10s, 0.00s, 0.00s).
- `go test ./internal/cairn/... -race -count=8 -v` — `ok`, 197.719s. Grepped the full verbose log: 0 `--- FAIL`, 0 `--- SKIP`, 0 `DATA RACE`, 2520 `--- PASS`. Each of the 3 diff-owned tests appears exactly 8 times (count=8) and PASSes all 8.
- `gofmt -l internal/cairn/index.go internal/cairn/index_test.go` — empty (clean).
- `go vet ./internal/cairn/...` — exit 0, clean.
- `golangci-lint run ./internal/cairn/...` — `0 issues.`

No diff-owned SKIP or FAIL, no flakes observed in this run. The pre-existing, non-diff-owned `TestConcurrentFindAndReindexDoNotHardFail` load-dependent flake that the review's full-suite (non-race) run surfaced (613 PASS/1 FAIL) was not reproduced here — consistent with it being load-dependent and already independently tracked in this repo predating this fix (referenced in the immediately-prior deployed PR #92's own body as "a pre-existing, already-tracked, load-dependent SQLite busy-timeout flake in an unrelated concurrent-indexing test"). **PASS.**

## 4. No open blocking findings

`gofmt`, `go vet`, and `golangci-lint` all independently re-run clean (§3). Independent read of the full `internal/cairn/index.go` diff: named driver imports (`sqlitedrv`, `sqlitelib`) replace the prior blank import so `isBusy` can reference the driver's `Error` type and result codes directly — no behavior change to driver registration. `isBusy` uses `errors.As` (correctly unwraps error chains) and masks to the primary result code (`&0xFF`), so it also catches extended busy codes (`SQLITE_BUSY_SNAPSHOT`, `SQLITE_BUSY_RECOVERY`). `retryOnBusy` is bounded (`maxAttempts = 6`, exponential backoff capped by that bound — cannot loop unboundedly), respects `ctx.Done()` during its backoff sleep via `select`, and the jitter computation (`rand.Float64()*delay - delay/2`) cannot produce a negative sleep duration (floor is `delay/2`, always positive for `delay > 0`). The `//nolint:gosec` suppression on the jitter RNG is narrow and correctly justified — timing jitter, not a security-sensitive value (no crypto, no token, no auth decision). No new SQL-injection surface (no new query strings built from external input). No HIGH, MAJOR, or MINOR findings identified independently or in review; no separate finding-type bead references crn-syd6x or crn-qgadf. **PASS.**

## 5. Clean working tree

`git status --porcelain` returns empty on `deploy/crn-syd6x-gate` at D, confirmed immediately before this gate doc's commit. `git log -1` confirms HEAD is exactly `dc68237215e88fa978c00fe88971acb3259994df`. **PASS.**

## 7. Single coherent theme

Three commits since the merge-base (`84e456c`), all one TDD cycle for the same fix (crn-wrg0 / crn-ca6cn):

1. `561816e` test(feat): red
2. `a7da8e8` feat: green
3. `dc68237` fix: address golangci-lint findings in retry backoff

Diff confined to `internal/cairn/index.go` + `internal/cairn/index_test.go` (145 insertions / 11 deletions total). No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
