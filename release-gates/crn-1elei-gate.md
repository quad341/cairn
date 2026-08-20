# Release Gate: crn-1elei

**Deploy commit:** 3df225ceca4a6358650849bf3365f623cae9099f
**Source branch:** builder/crn-evw98.2-v3
**Deploy-gate branch:** deploy/crn-1elei-gate
**Feature:** Branch-aware discovery: Reindex/IterEntries read pending remember/* branches
**Feature bead:** crn-evw98.2 (epic: crn-evw98)
**Review bead:** crn-hy7ll (verdict: pass, re-review) — original review crn-8co68 (verdict: pass)
**Evaluated:** 2026-08-20 by cairn/deployer

Deploy bead crn-1elei is a freshly-minted deploy-tracking wrapper (per crn-hy7ll's own close
reason: "new standalone needs-deploy bead crn-1elei created ... and slung to cairn/deployer").
It carries no commits of its own — the underlying commits cite crn-evw98.2, the actual feature
bead. This is expected and was confirmed at the commit-ancestry-scope check below, not assumed.

## Evaluation order

Criterion 6 evaluated first (cheap), per protocol. All others follow in numeric order.

## Criterion 6 — Clean divergence from main: PASS

`git merge-base --is-ancestor origin/main 3df225c` → true: the deploy commit already contains
the current origin/main tip (80020792e7812636bcf0b5eae9ba81748a989148) as an ancestor. No
rebase needed. Independently re-confirmed at gate-evaluation time — origin/main is unchanged
since the review's own check against the same SHA (review notes cite the identical origin/main
tip).

## Criterion 1 — SHA integrity / review-deploy match: PASS

Both SHAs resolved via `git rev-parse --verify --quiet "<sha>^{commit}"` before comparison,
not trusted as literal strings:
- Review verdict (crn-hy7ll notes, re-review section): `deploy_commit: 3df225ceca4a6358650849bf3365f623cae9099f`, `deploy_branch: builder/crn-evw98.2-v3`
- crn-1elei deploy commit: `3df225ceca4a6358650849bf3365f623cae9099f`, branch `builder/crn-evw98.2-v3`

Exact match.

## Criterion 2 — Acceptance criteria: PASS

crn-evw98.2 exit_contract (per review notes, re-checked against this SHA): (1) branch-aware
discovery via git-show/ShowReviewBranch reuse, (2) 4 pinned test flips, (3) 3 new-coverage
scenarios, (4) dependency on crn-evw98.1 review_status stamping — satisfied. Independently
corroborated via diff review (see criterion 7): core logic concentrated in
internal/cairn/entry.go (+279/-) and internal/cairn/remember.go, consistent with the described
scope.

## Criterion 3 — Tests pass: PASS

Independently re-ran (not trusting the reviewer's claimed counts alone) from a fresh, isolated
scratch worktree (`/var/tmp/cairn-builder-crn-hy7ll.wk1`, clean, HEAD independently verified to
equal the deploy commit) with an isolated `GOLANGCI_LINT_CACHE`:

- `go build ./...` → clean
- `go vet ./...` → clean
- `golangci-lint run ./...` → 0 issues
- `go test -race -count=1 ./...` → all 7 packages PASS, 0 FAIL:

```
ok  	github.com/quad341/cairn	                1.022s
ok  	github.com/quad341/cairn/cmd	          299.453s
ok  	github.com/quad341/cairn/formulas	        1.062s
ok  	github.com/quad341/cairn/internal/cairn	180.699s
ok  	github.com/quad341/cairn/internal/critic	 30.577s
ok  	github.com/quad341/cairn/internal/obslog	  1.048s
ok  	github.com/quad341/cairn/scripts	          5.262s
```

Matches the review's own more granular count (932 PASS / 0 FAIL / 0 SKIP, with the 9
diff-owned tests individually re-verified by name under `-v -race`).

## Criterion 4 — No open HIGH findings: PASS

Searched for beads referencing crn-hy7ll / crn-evw98.2 / crn-1elei: only the parent
architecture epic (crn-evw98, open/hold:mayor — expected; incremental sub-features ship while
the epic stays open) and one incidental text match on an unrelated bead (crn-x81gk, distinct
branch/commit/molecule — builder/crn-rott9.3 — not this diff, confirmed by inspection). No
finding bead exists against this content. Review notes independently state
`security_findings: none` (mechanical extract-function refactor, no new trust boundary or
external input path; core feature logic byte-identical to crn-8co68's already-fully-walked
OWASP review).

## Criterion 5 — Clean tree: PASS

Own worktree was clean before starting. Verification scratch worktree was clean at the exact
deploy commit with no untracked cruft. Build succeeded (no missing files).

## Criterion 7 — Single feature theme: PASS

Verified two independent ways:

1. **File-level:** `git diff --stat origin/main 3df225c` → 29 files changed, +754/-215,
   entirely within `cmd/` and `internal/{cairn,critic}/`. Core logic in entry.go/remember.go;
   the remainder is mechanical context-threading, per the review's own characterization.
   `cmd/list.go` and `internal/cairn/prime.go` do **not** appear in the diff — confirms the
   earlier squash-merge ancestry-conflict resolution (documented on crn-hy7ll) did not leak
   scope; those two files remain byte-identical to origin/main.
2. **Commit-level:** `assert_deploy_ancestry_scope origin/main 3df225c crn-evw98.2` → PASS
   (rc=0, via `packs/actual/deployer/scripts/rebase-resolve-lib.sh`, sourced by absolute path).
   The only two commits unique to this deploy vs. origin/main both explicitly cite crn-evw98.2
   in their subject line (red/green pair for branch-aware discovery) — no foreign content.

## Gate result: 7/7 PASS

## Merge authority

quad341/cairn — standing self-merge authorization applies (mayor ruling `gm-wisp-2yhv7u`,
2026-08-19 03:01:50; memory `cairn-auto-merge-requires-explicit-strategy`). This bead's own
description carries the older "cut isolated deploy branch, open PR, route merge-request to
mayor/mpr, no self-merge" boilerplate — per the ruling's explicit guidance this is a known
stale-copy artifact on quad341/cairn (a repo mpr does not cover) and is not treated as a fresh
signal to re-escalate.

Proceeding: open PR from `deploy/crn-1elei-gate`, bounded CI check, then plain
`gh pr merge --squash` (never `--auto`) once CLEAN/MERGEABLE with both required checks
(`build-test`, `lint`) COMPLETED/SUCCESS, confirmed via a fresh `gh pr view --json` read taken
immediately before merging. Independently verify `state=MERGED` / `mergedAt` non-null
afterward — never trust a bare exit 0.
