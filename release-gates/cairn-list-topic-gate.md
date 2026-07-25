# Release Gate: cairn list <topic>

Single-bead deploy for crn-r4yz (crn-tllb.1.1, reviewed via crn-swnp).
Source branch `builder/crn-tllb.1.1` sits directly atop `origin/main`'s
exact current tip (`516262f`) — no cherry-pick or rebase was required.

Deploy source: `5465253e8c49a1fd1a3d07e03eacd5af0f81cd56` (fix(cairn):
address review findings on crn-tllb.1.1 (crn-swnp)), 3 commits ahead of
`origin/main`, 0 behind.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit | PASS | crn-swnp: cairn/reviewer VERDICT: PASS cites commit `5465253e8c49a1fd1a3d07e03eacd5af0f81cd56` explicitly ("Fresh pass on commit 5465253..."). Deploy SHA (crn-r4yz's `COMMIT_SHA` field) == reviewed SHA exactly (R == D). |
| 2 | Acceptance criteria met | PASS | All 11 ACs from crn-tllb.1.1 independently re-verified against the actual diff (not taken from the reviewer's/builder's self-report): AC1 (listCmd registered, reuses `resolveIdentity`) — `cmd/list.go`. AC2/AC3 (single-match print, zero-match distinct error `no entries found for topic %q`) — `cmd/list.go` RunE. AC4 (untopiced bucket returns all matches, ID-ascending) — `TestListByTopicUntopicedMultiEntryBucket` asserts `[]string{"u1","u2","u3"}` exactly; order traced to `internal/cairn/entry.go:395`'s `sort.Slice(out, func(i,j) bool { return out[i].ID < out[j].ID })`, independently confirmed present and upstream of `Visible()` (line 469) — `ListByTopic` only filters via a plain range loop, no reordering. AC5 (exact match only) — `e.TopicKey == topicKey`; `TestListByTopicExactMatchOnly` rejects both a strict prefix and a strict substring of a real topic_key. AC6 (no recall-telemetry stamp) — `ListByTopic` never writes `hit_count`/`last_recalled_at`; `TestListByTopicNeverTouchesRecallTelemetry` asserts both fields unchanged across a call. AC7 (bounded body lookup) — `bodyPathsFor` batches one indexed query sized to `len(matches)`, never a full-store walk. AC8 (shared `UntopicedLabel` const) — added once in `entry.go`; diff confirms all 3 existing call sites (`cmd/commands.go` mapCmd + getCmd, `internal/cairn/prime.go` Prime) now reference it with zero other change at those lines. AC9 (test coverage) — 6 cases in `internal/cairn/list_test.go` + 3 in `cmd/list_test.go`, one-to-one with the AC9 list (single winner, zero matches, untopiced multi-entry, shadow-conflict precedence, telemetry non-stamp, exact-match-only; CLI wiring: match print, zero-match error text, label translation). AC10 (no `--json` flag) — confirmed absent from `cmd/list.go`'s flag set. AC11 (clean build/vet/fmt/test) — see criterion 3. |
| 3 | Tests pass | PASS | Independently re-run from a clean checkout at `5465253` (`.gc/worktrees/cairn/builder/crn-tllb.1.1`, golangci-lint cache cleaned first per known shared cross-worktree cache staleness): `gofmt -l .` clean, `go build ./...` clean, `go vet ./...` clean, `golangci-lint run ./cmd/... ./internal/cairn/...` — 0 issues, `go test ./... -race -count=1` — all 5 packages green (`cmd`, `formulas`, `internal/cairn`, `internal/critic`, `scripts`), no FAILs, no flake (the previously-flagged `internal/critic` flake, tracked separately as crn-u8c0, did not reproduce this run). |
| 4 | No high-severity findings open | PASS | Reviewer's two findings (Finding 1: blocking gosec G202 false-positive on the batched `IN (...)` query; Finding 2: non-blocking order-assertion gap) were both fixed in `5465253` and confirmed fixed by the reviewer's own fresh second pass at that exact SHA. 0 open findings of any severity. |
| 5 | Final branch is clean | PASS | `git status --porcelain` empty at `5465253` in the builder worktree. |
| 6 | Branch diverges cleanly from main | PASS (no rebase needed) | `git merge-base origin/main builder/crn-tllb.1.1` == `origin/main` HEAD (`516262f`) exactly — the branch already sits directly on the current tip, 0 behind / 3 ahead. `git merge-tree --write-tree origin/main 5465253` resolves clean (no conflict markers). |
| 7 | Single feature theme | PASS | 7 files touched, all in service of one feature (`cairn list <topic>`): 2 new files (`internal/cairn/list.go`, `cmd/list.go`) + their 2 test files, 1 new const in `entry.go`, and 3 mechanical one-line literal-to-constant swaps at the two existing call sites this feature's const now unifies (`cmd/commands.go` x2, `internal/cairn/prime.go` x1). No unrelated subsystem touched. |

## Disposition

**GATE PASS.** No self-rebase needed — the reviewed commit already sits
directly on `origin/main`'s current tip. PR to be opened from
`deploy/crn-r4yz-gate` (cut at `5465253`) onto `main`, GitHub-native
auto-merge armed per cairn's scoped deployer merge authority (`--squash`,
per fleet memory `cairn-auto-merge-requires-explicit-strategy` — no
merge-queue ruleset configured on `quad341/cairn`), pending a bounded
post-push CI check.
