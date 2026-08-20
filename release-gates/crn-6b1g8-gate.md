# Release Gate: crn-6b1g8

**Bead:** crn-6b1g8 (deploy) — source: crn-w0gc3 (review), feature: crn-evw98.3
**Commit:** `161eaa9e7fe38668a24c47ad198b393e146cf8bf`, cut onto `deploy/crn-6b1g8-gate`
**Date:** 2026-08-20

## Background

This is the redo of a previously gate-FAILED deploy round for the same feature
(Cross-topic-key Corrects/Supersedes link, consulted at read time, crn-evw98.3).
The prior deploy bead crn-v6jix (reviewing commit 50cdb919, review crn-0mdsv
round 2) hit a genuine criterion-6 merge conflict against origin/main (PR #129,
mergeable=CONFLICTING) and was correctly routed back to the builder rather than
auto-resolved — confirmed non-trivial by an aborted bounded self-rebase attempt.
The builder rebased builder/crn-evw98.3 onto current main and landed the fix for
round 1's outstanding presentational gap (spec criterion 5: plain-text NOTE-line
format/ordering), producing commit 161eaa9e. crn-w0gc3 (this round's review)
independently re-verified the full 8-file feature diff (storage:
internal/cairn/entry.go / index.go; write path: cmd/remember.go; read path:
cmd/commands.go; plus tests) and passed. crn-6b1g8 is the resulting deploy bead.

Per mayor's standing authorization (recorded on crn-v6jix's notes, 2026-08-20):
once this redo lands on a fresh branch/PR, any seat may close the stale PR #129
and delete origin/deploy/crn-v6jix-gate — planned as post-merge cleanup below.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-w0gc3 (closed, close_reason=pass) metadata.deploy_commit and notes' `deploy_commit:` field both cite `161eaa9e7fe38668a24c47ad198b393e146cf8bf`, byte-identical to crn-6b1g8's own `**Commit:**` field (D = R). Independently re-resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` — a real commit object, not a transcribed string. |
| 2 | Acceptance criteria met | PASS | This deploy exists specifically to close spec criterion 5 (plain-text NOTE-line format), the one gap crn-0mdsv round 1 found. crn-w0gc3 independently re-verified via a manual CLI smoke test on this exact SHA: `cairn get orig` → NOTE line leads (`NOTE: orig redirected to fix`); `cairn get fix` (uncorrected) → no NOTE line. Both directions are also pinned as automated, order-sensitive assertions in `TestGetRedirectsToCorrectionWhenOriginalIsRequested` / `TestGetDoesNotRedirectWhenNoCorrectionExists`, independently re-run by me (see #3) and confirmed passing on this branch. |
| 3 | Tests pass | PASS* | `go test ./... -race -count=1` on `deploy/crn-6b1g8-gate` (=161eaa9e): 7/8 packages clean (root, cmd, formulas, internal/cairn, obslog, scripts all `ok`). One flake: `internal/critic`'s `TestRunFreshnessScenarioPasses` FAILed once under full concurrent load (`context deadline exceeded`). Not diff-owned — internal/critic is absent from this deploy's 8-file diff, so its source is byte-identical to origin/main — zero path overlap, and it reproduces clean 5/5 in an isolated re-run (`-run TestRunFreshnessScenarioPasses -count=5`, ~0.29s each vs. 18.65s under full-suite contention). Consistent with the fleet's tracked internal/critic load-sensitivity precedent (crn-mxvn / crn-u8c0 / crn-9k30 — same package/class, different specific test/symptom). Filed as crn-n7svm for investigator follow-up; treated as non-blocking per the non-diff-owned-failure-attribution protocol (not diff-owned, tracked precedent predates this run, proven same-bytes-as-main, zero path overlap). |
| 4 | No open HIGH findings | PASS | crn-w0gc3: security_findings none (OWASP walk: no injection/auth/exposure/XXE/SSRF surface — pure Printf reorder), style_findings none (gofmt/vet/golangci-lint all clean). Independently corroborated on this exact branch: `gofmt -l .` empty, `go vet ./...` clean (rc 0). |
| 5 | Clean tree | PASS | `deploy/crn-6b1g8-gate` cut directly from `161eaa9e...^{commit}` via `resolve_deploy_branch_target`; `git status --porcelain` empty throughout gate evaluation. |
| 6 | Clean divergence from main | PASS | `git merge-tree --write-tree origin/main 161eaa9e` → exit 0, single tree `242d7988f43db206817fd45c7967f4db197843c5`, no conflict markers — clean merge despite main having advanced 2 commits since the branch's merge-base. No self-rebase needed. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope main 161eaa9e crn-6b1g8 crn-w0gc3 crn-0mdsv crn-evw98.3 crn-v6jix` → rc=0: no `.claude/**` paths touched; all 4 non-merge commits in `main..161eaa9e` cite an accepted id (`crn-evw98.3` ×3, `crn-0mdsv` ×1 in-body). `diff --stat` confirms all 8 changed files sit under `cmd/` and `internal/cairn/` — one coherent feature (cross-topic-key corrects/supersedes link) end to end: storage, write path, read path, tests. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority — proceeding under the standing self-merge authorization**,
   per fleet memory `cairn-auto-merge-requires-explicit-strategy`. Re-read the
   full memory (not a cached paraphrase) before acting: reinstated 2026-08-19
   (mayor ruling gm-wisp-2yhv7u — quad341/cairn is not mpr-covered), and
   reaffirmed as recently as today via an addendum (2026-08-20, mayor msg
   gm-wisp-fvdoja, re PR #130) that describes the deployer merging "under its
   own authority going forward" — mayor's own most recent message treats
   self-merge as ongoing practice, not something to re-litigate per bead.
   crn-6b1g8's bead body carries the familiar "route through mayor/mpr"
   boilerplate; per the ruling this is the known stale-copy artifact, not a
   fresh signal requiring escalation.

   Mechanism (THE RULE, verbatim from the memory): check
   `gh pr view <n> --json mergeStateStatus,mergeable,autoMergeRequest` fresh
   before merging. If already CLEAN/MERGEABLE with CI green: plain
   `gh pr merge <n> --squash --delete-branch` (never `--auto` in this state —
   it has nothing left to arm). If checks are still running:
   `gh pr merge <n> --auto --squash --delete-branch`, then verify
   `autoMergeRequest` is non-null. Either way, verify via a fresh read
   afterward (state=MERGED/mergedAt for plain; autoMergeRequest non-null for
   armed) — never trust exit code alone (FR-03).

   4 standing conditions (mayor, 2026-08-19), checked per-PR, not assumed
   carried-over: (1) gate 7/7 PASS — this doc; (2) PR state via a direct fresh
   `gh pr view --json` read; (3) CLEAN/MERGEABLE with both required checks
   (`build-test`, `lint`) COMPLETED/SUCCESS; (4) `--delete-branch` appended by
   default (2026-08-20 addendum — closes the crn-v6jix stale-branch failure
   class at the source).

2. **Branch target:** `builder/crn-evw98.3` is provenance-only per the deploy
   bead's own instruction — a possibly shared builder branch, not a push
   target. `deploy/crn-6b1g8-gate` was cut fresh from the exact reviewed SHA
   via `resolve_deploy_branch_target`; `assert_safe_push_target` confirmed the
   derived name does not match the shared-worktree-branch signature.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step ran.

4. **Post-merge cleanup (pre-authorized):** once this PR is merged, close the
   now-fully-superseded PR #129 and delete `origin/deploy/crn-v6jix-gate` per
   mayor's standing authorization recorded on crn-v6jix's notes (2026-08-20) —
   that branch is frozen at a pre-rebase commit (169562d) and this deploy is
   the redo it was waiting on.
