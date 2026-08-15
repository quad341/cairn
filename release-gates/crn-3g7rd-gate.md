# Release Gate: crn-3g7rd

**Bead:** crn-3g7rd (deploy) — source: crn-ium7i (review), build: crn-44uuq, molecule: crn-u0fh4
**Commit:** `5ef7821f6cfaa66b3e7a1beb5c5c5158fbf53e0c`, cut onto `deploy/crn-3g7rd-gate`
**Date:** 2026-08-15

## Background

Fixes `docs/DESIGN.md` and `docs/knowledge-lifecycle.md`, which still described
a `files`-anchor fingerprint as sourced from `HEAD` — stale prose left behind
by crn-uztp's change to `objectHash()` (`internal/cairn/freshness.go`), which
now prefers `origin/main` and only falls back to `HEAD` when `origin/main`
does not resolve (no origin remote, or the path isn't tracked there). Both
docs now state the origin/main-first, HEAD-fallback behavior — DESIGN.md in
full, knowledge-lifecycle.md in its terser house style, per the bead's
explicit instruction. Docs-only change plus one additive test
(`doc_content_test.go`) that pins the corrected prose so it can't silently
drift again.

Reviewed and PASSED by `cairn/reviewer` (crn-ium7i, closed, verdict: pass)
against exactly this commit.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-ium7i's notes cite `deploy_commit: 5ef7821f6cfaa66b3e7a1beb5c5c5158fbf53e0c`, identical to the deploy bead's `metadata.commit` — exact SHA match (D = R). |
| 2 | Acceptance criteria met | PASS | Independently re-verified, not just trusted from the reviewer's restatement: `git show 5ef7821f -- docs/DESIGN.md docs/knowledge-lifecycle.md` confirms both files now read "`origin/main`, falling back to `HEAD` only when `origin/main` does not resolve"; `git grep -n "hashes.*at .HEAD"  -- docs/` returns zero matches (no stale phrasing survives); `grep -n "origin/main\|func objectHash" internal/cairn/freshness.go` confirms the doc now matches the *actual* shipped code — `objectHash()` calls `git(ctx, repo, "rev-parse", "origin/main:"+path)` first, HEAD only as documented fallback. `git diff origin/main...5ef7821f -- internal/` is empty (0 lines) — no internal/ changes ride along, confirming this is purely a docs-catch-up, not a behavior change. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-3g7rd-gate` at `5ef7821f...`, using the same command the reviewer documented (`Makefile` `test` target): `go test ./... -race -count=1` — **7/7 packages ok, exit 0, 0 FAIL.** Diff-owned test individually confirmed PASS by name: `go test . -race -count=1 -v -run '^TestDesignDocStatesFilesAnchorSourcedFromOriginMain$'` → `--- PASS (0.00s)`. `go build ./...` clean. `git diff --stat origin/main..HEAD -- go.mod go.sum` empty — no new dependency. |
| 4 | No open HIGH findings | PASS | Reviewer's `style_findings`: gofmt/go vet/golangci-lint v2 all clean. `security_findings`: none — diff is 2 prose-only markdown edits plus one additive Go test reading two local files by hardcoded literal path with string-containment assertions; no user input, network, shell/SQL/template concatenation, auth/access-control, or new dependency. No separate open HIGH/blocker finding bead exists. |
| 5 | Clean tree | PASS | `deploy/crn-3g7rd-gate` cut directly from `5ef7821f...^{commit}` via `resolve_deploy_branch_target`. `git status --short` empty, re-confirmed immediately before writing this doc. |
| 6 | Clean divergence from main | PASS | `origin/main` (`79d7ca1c`) has advanced 3 commits past the merge-base (`36badd70`) since this branch was cut — none touching `docs/DESIGN.md`, `docs/knowledge-lifecycle.md`, or the root package — so this is a genuine divergence, not a fast-forward. Checked with the correct tool for that shape: `git merge-tree 36badd70 origin/main 5ef7821f` → exit 0, zero `CONFLICT`/`<<<<<<<` markers across 69 lines of output. No self-rebase needed. |
| 7 | Single feature theme | PASS | All changed files (`docs/DESIGN.md`, `docs/knowledge-lifecycle.md`; 5 insertions/3 deletions) plus the RED-commit test addition (`doc_content_test.go`, ancestor commit `bd8ffbd`) implement one cohesive theme: correcting the files-anchor fingerprint docs to match shipped `origin/main`-first behavior. No unrelated or drive-by changes. |

## Verdict: PASS — proceeding to PR.

## Process note

The deploy bead's own body text ("Route a merge-request to mayor/mpr; merge
authority is operator/mayor/mpr only — no rig agent runs `gh pr merge`.
Report the gate result back to mayor.") reflects process superseded, same
day, by the mayor-ratified standing authorization
(`cairn-auto-merge-requires-explicit-strategy`, reaffirmed 2026-08-15) and
the current deployer role prompt: for `quad341/cairn` only, gate 7/7 PASS +
CI green ⇒ the deployer arms `gh pr merge --auto` directly, with no mayor
escalation required. This gate follows the newer, more specific, same-day
authorization rather than the bead's stale prose.
