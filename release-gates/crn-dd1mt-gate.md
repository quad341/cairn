# Release Gate: Add a recall hit-rate measurement skill

- Bead: crn-dd1mt (deploy) / crn-p06ni (review, PASS) / builder/crn-s907 (provenance branch, not a push target)
- Reviewer-cited commit (R): `86197d35b738dd9fef668f3512f4b985e9a73d60`
- Final deployed commit (D): `86197d35b738dd9fef668f3512f4b985e9a73d60` (identical to R — origin/main was already an ancestor, no self-rebase required)
- Evaluated: 2026-08-06, against origin/main@2ba5035 (#77, "Docs: clarify that freshness checks detect change, not correctness")
- Downstream: crn-470gf (sling-crn-dd1mt) is a routing convoy blocked on this bead closing — not a merge dependency, not blocking this gate

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin main` immediately before this evaluation. origin/main tip is `2ba5035` (#77), unchanged from the pre-evaluation check. `git merge-base --is-ancestor origin/main HEAD` is true both before and after the fetch: D's own commit chain (`86197d3` → `24189dd` → `2ba5035`) already contains origin/main's tip as a direct ancestor. No bounded self-rebase needed. **PASS.**

## 1. Exact SHA match (D == R)

R = `86197d35b738dd9fef668f3512f4b985e9a73d60`, the `tdd_green` commit recorded on crn-p06ni (review) and matching crn-dd1mt's own `commit` metadata. The isolated deploy branch (`deploy/crn-dd1mt-gate`) was cut directly from R via `resolve_deploy_branch_target`, not from the bead description's hand-written `deploy/crn-p06ni-gate` (that string names the review bead, not the deploy bead — the mechanical resolver is authoritative). No rebase was applied (criterion 6 was already structurally satisfied), so D == R literally, exact match. **PASS.**

## 2. Acceptance criteria

Independently read all three new files (`skills/recall-hitrate/SKILL.md`, `recall_hitrate.py`, `test-recall-hitrate.sh`) rather than relying solely on the reviewer's report. Checked against crn-s907's 8-item acceptance list:

1. **Three-file skill shape** (script + SKILL.md + test script), mirroring burn-report: present. Note: placed at tracked top-level `skills/recall-hitrate/`, not the literal `.claude/skills/recall-hitrate/` the bead text names — `.claude/skills/` in this repo is fleet-managed symlink infra, gitignored via local `.git/info/exclude`, never tracked in cairn history. This deviation is deliberate, documented in the builder's notes (`skill_location_note`/`scope_note` on crn-s907), and was disclosed to mayor via notify-fanout + mail during the build. Accepted as a justified, already-flagged scope adjustment, not a silent gap.
2. **Multi-home discovery + XDG_STATE_HOME-with-fallback log path**: `discover_homes()` (recall_hitrate.py:45-55) globs `*-claude` siblings of `$HOME`/parent, filtered to dirs holding `.local/state/cairn`; `log_path_for_home()` (:68-79) implements the exact XDG-override-for-own-home / fallback-for-others logic, explicitly documented as mirroring `internal/obslog/path.go`'s `LogPath()`. Confirmed.
3. **Join algorithm** (nearest-preceding `prime_emit` by identity_tags+ts): `harvest()` (:82-136) tracks per-identity prime history in file order and selects the prime with the latest `pwhen <= when`. Confirmed, including the deliberate "two prime_emit calls for the same identity" trap in the test fixture that would catch a naive any-preceding join.
4. **Human-readable report** (hit-rate, join coverage, raw counts): recall_hitrate.py:203-215. Confirmed.
5. **`--json` flag, same numbers as structured JSON**: recall_hitrate.py:184-196, stdout kept pure JSON with sources/warnings on stderr. Confirmed.
6. **Test fixtures cover all four buckets** (surfaced/recall-miss/miss-excluded/unjoined): test-recall-hitrate.sh section 1 (lines 51-56) asserts exact counts for all four from a single engineered fixture. Confirmed.
7. **Blocked-by crn-jkth, synthetic fixtures OK in the interim**: crn-jkth shows CLOSED in crn-s907's `DEPENDS ON`; test fixtures are synthetic JSONL built in a tmpdir (test-recall-hitrate.sh `mk_alpha`/`mk_beta`), exactly as sanctioned. Confirmed.
8. **SKILL.md states the "distinct from store size" framing explicitly**: SKILL.md:8-9 states plainly that a `cairn get` hit only proves the entry existed, not that `prime` ever surfaced it. Confirmed.

All 8 criteria independently verified against source, not just the reviewer's summary. **PASS.**

## 3. Tests

Canonical command per `Makefile`'s `test:` target: `go test ./... -race -count=1`. Ran independently on D:

- `go test ./... -race -count=1` — all 7 packages (`cairn`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`) report `ok`, exit 0.
- Diff-owned `./skills/recall-hitrate/test-recall-hitrate.sh` — **21 passed, 0 failed**, exit 0. Matches the reviewer's cited count exactly.

This diff touches zero Go files, so the repo-wide Go suite is a pure regression check. The reviewer's crn-p06ni report cited 544 Go tests PASS/0 FAIL/0 SKIP; my run shows a clean `ok` on every package with no `FAIL` markers, consistent with no regression. Builder notes on crn-s907 document one pre-existing, unrelated flake (`TestConcurrentReindexDoesNotRaceOnEntryTagsSchema`, `internal/cairn`, ~1/80 runs, SQLITE_BUSY on a schema-migration race) — it did not manifest in this run (`internal/cairn` passed clean), and is out of scope for this diff regardless. **PASS.**

## 4. No open blocking findings

Diff is a new standalone Python skill (script, doc, shell test) — no production Go code touched, stdlib-only imports (`argparse`, `collections`, `datetime`, `glob`, `json`, `os`, `sys`), no new dependencies, no network calls, no shell-out/`eval`, no secrets, no injection surface. Reviewer (crn-p06ni) recorded all style/security/spec findings as clean or info-severity, zero HIGH findings.

Independently re-ran on D: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` clean (zero output). `golangci-lint run ./...` hit a transient "parallel golangci-lint is running" lock contention on the first attempt (another process holding the lock, not a code issue); retried alone and got **0 issues**. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty, confirmed immediately before writing this doc. **PASS.**

## 7. Single coherent theme

3 new files, all under `skills/recall-hitrate/`, +403/-0. One cohesive feature: measuring whether `cairn prime` actually surfaces entries that later get retrieved, as opposed to just tracking store hit/miss. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
