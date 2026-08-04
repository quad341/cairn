# Release Gate: Token-cost + retrieval-outcome telemetry (gates the on/off experiment)

- Bead: crn-f9vj (deploy) / crn-e51q (review, round 2, recycled) / crn-tekm (builder/implementation, source branch `builder/crn-tekm`)
- Reviewer-cited commit (R): `87692f572cf3d418115716ff2e99cee9959c3099`, round-2 PASS
- Final deployed commit (D): `87692f572cf3d418115716ff2e99cee9959c3099` (identical — no rebase required, see criterion 6)
- Evaluated: 2026-08-04, against origin/main@a4ea553 (PR #72, "add the anchor verb, and stop write-back escaping its store")
- Downstream consumer: crn-ie0m.2 (bd-blocked governing-equation go/no-go verdict bead) — this deploy resolves one of its two dependencies (crn-tekm), it does not depend on it.

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before this evaluation. `git merge-base --is-ancestor origin/main HEAD` → true: origin/main (a4ea553) is a full ancestor of this branch's tip. 3 commits ahead, 0 behind. No rebase needed; `attempt_bounded_self_rebase` not invoked. **PASS.**

## 1. Exact SHA match (D == R)

D and R are the literal same commit, `87692f572cf3d418115716ff2e99cee9959c3099`. Confirmed against both the bead's own description (which pins this SHA as the deploy source, explicitly distinct from the shared `builder/crn-tekm` provenance branch) and the reviewer's round-2 re-verification, which independently confirmed via `git fetch` + `git log` that the branch tip was unchanged across both review rounds — no new commits landed between round 1 and round 2. **PASS.**

## 2. Acceptance criteria

Round 1's sole blocker was that `crn-tekm`'s exit_contract overclaimed relative to what shipped (listed `OutcomeLabel` plus three cost fields — `CreationCost`/`ReviewCost`/`ReverificationCost` — not implemented in this slice). Round 2 verified the fix: the exit_contract was narrowed to match what actually shipped (`identity_tags`/`run_id`/`outcome`/`entry_id`/`payload_tokens`/`reuse_count`), with the deferred fields explicitly cross-referenced to `crn-ie0m.2`.

I independently confirmed this cross-reference actually landed rather than trusting the reviewer's claim: read `crn-ie0m.2` directly, and its notes field (added during the crn-tekm/crn-e51q round-1 reconciliation) documents the deferred fields by name and ties them to the architect's `crn-ie0m` design (FR2, §5 data model, §4/§9) — matching what round 2 claimed. **PASS.**

## 3. Tests

Independently reproduced from scratch in an isolated worktree at the exact reviewed SHA (not reused from the reviewer's report):
- `gofmt -l .` — clean
- `go build ./...` — exit 0
- `go vet ./...` — exit 0
- `golangci-lint run ./...` — 0 issues
- `go test ./... -race -count=1` — 690 PASS, 0 FAIL, 0 SKIP

Matches the reviewer's reported counts exactly. **PASS.**

## 4. No open blocking findings

Both sub-finding beads for this review closed clean: `crn-nfsc` (style/lint) and `crn-5ok2` (security/OWASP Top 10), both under `mol-code-review` (`crn-xdhx`). Reviewer's security pass found zero vulnerabilities: telemetry writes go through `slog.NewJSONHandler` (safe JSON encoding), there's no free-form-content logging surface, and no new third-party dependencies were introduced.

Independently swept fleet-wide open issues: one open P0, `crn-ie0m.2` — this is the downstream *consumer* of this telemetry, blocked on `crn-tekm` shipping (i.e. blocked on this very deploy), not a finding against this diff. Open P1s scanned for relevance to the changed files (`cmd/commands.go`, `cmd/commands_test.go`, `cmd/identity.go`, `cmd/root.go`, `cmd/root_test.go`, `internal/obslog/obslog.go`, `internal/obslog/obslog_test.go`): `crn-knev` (cmd test suite hermeticity re: ambient `CAIRN_IDENTITY`) touches adjacent files but its fix is already merged into main (`04b0b50`, PR #69 — present in origin/main, which this branch already sits ahead of); it remains open in bd only as a stale bookkeeping gap, not live work, and out of scope for this deploy to correct. All other open P1s are generic molecule-step tracking scaffolding or unrelated fleet/tooling infrastructure bugs (rebase-lib zsh portability, actor-identity normalization, deployer handoff-verification, concurrent-builder dedup) — none reference `crn-f9vj`/`crn-tekm`/`crn-e51q` or touch any file this diff touches. **PASS.**

## 5. Clean working tree

Worktree at HEAD (deploy branch, freshly cut directly from the reviewed SHA) has no tracked modifications beyond this gate doc itself, freshly written for this evaluation. **PASS.**

## 7. Single coherent theme

Token-cost + retrieval-outcome telemetry gating the Cairn-ON-vs-OFF experiment (`crn-ie0m`): per-interaction `identity_tags`/`run_id`/`outcome`/`entry_id`/`payload_tokens`/`reuse_count` capture added to `internal/obslog`, wired through `cmd/commands.go` and `cmd/identity.go`. Diff-stat confirms scope: 7 files, all under `cmd/` or `internal/obslog/` (232 insertions, 3 deletions) — no unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
