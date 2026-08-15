# Release Gate: cairn-redact-argv-shorthand-bundle

**Bead:** crn-8kd13 (deploy) — source: crn-1aa43 (review, round 3), root: crn-s88w
**Commit:** `9e82f2c098450bf7945eda3693a70040db0e162c` (self-rebase of reviewed
commit `d9680d20e2513252715f6051e7c3e2acfb19d302` onto current `origin/main`;
see criterion 6), cut onto `deploy/crn-8kd13-gate`
**Date:** 2026-08-15

## Background

Round 3 of the free-text-argv redaction fix (`redactShorthandBundle` in
`cmd/root.go`), closing a secret-redaction bypass where a bundled shorthand
flag token (e.g. `-vtSECRET`, boolean `-v` immediately ahead of string `-t`)
fell through unredacted because the round-2 code assumed the suspicious
shorthand was always the character right after `-`. The fix walks the token
character by character via `cmd.Flags().ShorthandLookup()`, mirroring
pflag's own `parseSingleShortArg`, stopping at the first value-taking flag
and redacting only if that flag is in the redact set.

`d9680d2` was reviewed and PASSED independently by `cairn/reviewer`
(crn-1aa43, closed). At gate-eval time criterion 6 failed (branch one commit
behind `origin/main` after PR #91 landed); a bounded self-rebase
(`attempt_bounded_self_rebase`, rc=0) replayed it cleanly onto current main
with zero file overlap between this diff and main's new commits, producing
`9e82f2c`. Deploying that rebased tip per process (never the stale
pre-rebase SHA).

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | Was FAIL at first eval (1 commit behind `origin/main` post-PR #91). Bounded self-rebase: `d9680d2` → `9e82f2c` (`attempt_bounded_self_rebase` rc=0, linear no-conflict replay, zero file overlap between this diff (`cmd/root.go`, `cmd/root_test.go`) and main's new commits (`cmd/remember*.go`, `cmd/reviewer.go`, docs); `--force-with-lease` pushed to `origin/builder/crn-79vu.1`). Re-verified clean against **current** `origin/main` at gate-write time (still `84e456c`/PR #91, unchanged since the rebase): `git merge-tree --write-tree origin/main 9e82f2c` → clean tree `9fd76b33...`, no CONFLICT markers, exit 0. |
| 1 | Review PASS present | PASS | `cairn/reviewer` recorded an independent PASS on crn-1aa43 (closed) for `reviewed_commit: d9680d20e2513252715f6051e7c3e2acfb19d302` — not a builder self-report; all 6 round-3 exit_contract items verified against actual code with line citations. Deployed commit `9e82f2c` is a bounded, verified-trivial self-rebase of that exact reviewed commit onto fresh main (zero file overlap with main's new commits, confirmed above) — it carries the identical reviewed diff, only its parent changed. Per the self-rebase carve-out, this satisfies the review-coverage requirement; no new review round needed. |
| 2 | Acceptance criteria met | PASS | All 6 round-3 exit_contract items independently confirmed against actual code (not builder self-report) in crn-1aa43's notes: character-by-character walk via `ShorthandLookup`/`NoOptDefVal` (`cmd/root.go:89-107`), stops at first value-taking flag, equals-forms unaffected (pre-existing `hasEq` branch untouched), new regression test `bundled_boolean_plus_suspicious_shorthand` present and passing (the exact round-2 BLOCKER case), all prior regression tests still passing by name, diff scope confined to `cmd/root.go`+`cmd/root_test.go`. |
| 3 | Tests pass | PASS | Independently re-run in an isolated detached scratch worktree at `9e82f2c` this session (not trusting the reviewer's report): `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` clean. Targeted `go test ./cmd/... -race -v -run TestRedactArgv`: 6 top-level / 9 subtests, ALL PASS by name, including `bundled_boolean_plus_suspicious_shorthand`. Full suite `make test` (= `go test ./... -race -count=1`, matching `.github/workflows/ci.yml` exactly): green in `github.com/quad341/cairn`, `cmd`, `formulas`, `internal/critic`, `internal/obslog`, `scripts`. One FAIL in `internal/cairn` (`TestConcurrentReindexOnColdStoreDoesNotHardFail`, 1/80 concurrent Reindex calls, SQLITE_BUSY) — diff-unrelated (this diff touches only `cmd/root.go`; confirmed via `git diff --stat` and independently by the reviewer) and a known, root-caused, already-fix-spec'd flake: `crn-wrg0` (closed, superseded by `crn-ca6cn`) proved it is `busy_timeout` starvation under concurrent fleet load in `internal/cairn`'s Reindex path (measured 5/80 failures at 16 concurrent writers on the shared store; explicitly not caught by CI's single uncontended `-count=1` run), not a code defect in this diff. Confirmed non-deterministic by re-running both this test and the reviewer's originally-cited `TestConcurrentReindexDoesNotRaceOnEntryTagsSchema` twice in isolation: PASS both times, both tests. No diff-owned SKIP or FAIL. |
| 4 | No high-severity review findings open | PASS | Reviewer's OWASP walk: no new or reopened findings (fix closes a real sensitive-data-exposure bypass, introduces no panic/crash surface — traced for index-out-of-range risk on adversarial argv, always fails closed to "leave untouched"). `style_findings: none` (gofmt/vet/lint clean on both changed files). Conclusion: PASS, no BLOCKER/HIGH items open. |
| 5 | Final branch clean | PASS | `deploy/crn-8kd13-gate` cut directly from `9e82f2c^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty immediately after checkout, before this gate doc's own commit. |
| 7 | Single feature theme | PASS | Diff confined to `cmd/root.go` + `cmd/root_test.go` (confirmed via `git diff --stat` on the branch commit range) — one coherent fix: bundled-shorthand argv redaction. No drive-by changes, no unrelated files, no doc contamination. |

## Verdict: PASS — proceeding to PR.
