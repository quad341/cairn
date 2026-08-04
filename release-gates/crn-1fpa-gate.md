# Release Gate: Docs: drop the truth-guarantee claim; state scope is relevance routing, never security

- Bead: crn-1fpa (deploy) / crn-mmbq (review, PASS) / builder/crn-6egs (provenance branch, not a push target)
- Reviewer-cited commit (R): `400eaf0cdd60add69b857fec79ab7f3bc732e16a`
- Final deployed commit (D): `52e2fd71bd8ccbd35215e4f9f15c6425c5f6b2ae` (bounded self-rebase of R onto origin/main — see criterion 6)
- Evaluated: 2026-08-04, against origin/main@2ab91bf (#76, "test(cairn): dogfood the git-anchor loop end-to-end")
- Downstream: crn-1fpa has 2 dependent beads; crn-gs0b (sling-crn-1fpa) identified, not blocking this gate — no dependency runs the other direction

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin main` immediately before this evaluation. R's parent (`37f8327`, #73) is 1 commit behind current origin/main tip (`2ab91bf`, #76 — an unrelated dogfood-test commit). Confirmed via `git merge-base --is-ancestor origin/main HEAD` (false pre-rebase): real divergence, not a stale/already-passing check.

Ran `attempt_bounded_self_rebase "deploy/crn-1fpa-gate" "main"` (sourced from `scripts/rebase-resolve-lib.sh`, not re-implemented inline) on the isolated `deploy/crn-1fpa-gate` branch (cut from R via `resolve_deploy_branch_target`, safe self-rebase target — not a shared worktree branch, not the builder's provenance branch). Result: rc=0, `BEFORE_SHA=400eaf0cdd60add69b857fec79ab7f3bc732e16a`, `AFTER_SHA=52e2fd71bd8ccbd35215e4f9f15c6425c5f6b2ae`, force-with-lease pushed to `origin/deploy/crn-1fpa-gate`. Verified post-rebase: local HEAD == remote branch tip == `52e2fd71bd8ccbd35215e4f9f15c6425c5f6b2ae`; `git merge-base --is-ancestor origin/main HEAD` now true; working tree clean. **PASS.**

## 1. Exact SHA match (D == R, or sanctioned rebase exception)

R = `400eaf0cdd60add69b857fec79ab7f3bc732e16a`. This is the value both the deploy bead's `commit` metadata and the review bead's (corrected) `deploy_commit` field agree on. Note: the review bead's raw `metadata.commit` field independently carried a corrupted SHA (`400eaf01dc47eda2cf4938e69e5cd0dd85a24e1e`) that does not exist in the object graph (`git cat-file -e` exit nonzero, confirmed independently by both the reviewer and me) — both bead texts self-correct to the real tip `400eaf0cdd60add69b857fec79ab7f3bc732e16a` (`git cat-file -e` exit 0), which is what R denotes throughout this doc.

D != R literally, because criterion 6 required the sanctioned bounded self-rebase. The exception applies because the rebase is content-preserving: `git diff origin/main HEAD` (i.e., diff of D against its new base) shows exactly the reviewed change — 3 files, +57/-0 (`doc_content_test.go` +40 new file, `docs/DESIGN.md` +12, `docs/knowledge-lifecycle.md` +5) — identical in content to what crn-mmbq reviewed at R, only the base commit changed. **PASS (via sanctioned rebase exception).**

## 2. Acceptance criteria

Independently read the actual diff (`git diff origin/main HEAD`) rather than trusting the bead's summary alone:

- `docs/DESIGN.md`: adds the OKF minimum-viable-schema `type`-field convention bullet, and the defensible freshness-claim paragraph in section 4.
- `docs/knowledge-lifecycle.md`: adds the identical defensible freshness-claim sentence plus a concrete "a perfectly fresh anchor on a badly-chosen file is still a wrong memory presented as verified" caveat.
- `doc_content_test.go` (new file, +40): two RED-authored acceptance tests — `TestFreshnessCopyStatesDetectionNotCorrectness` (asserts the exact sentence `"Cairn detects changes to declared evidence and prevents affected knowledge from being presented as verified without re-investigation."` appears verbatim in both docs) and `TestDesignDocStatesTypeFieldConvention` (asserts `docs/DESIGN.md` mentions both `` `type` `` and `OKF`).

Confirmed the required sentence appears verbatim, byte-for-byte, in both target docs by direct inspection of the diff. Both acceptance tests are present in my own independent test run (criterion 3) and passing. **PASS.**

## 3. Tests

Confirmed the CI-equivalent command myself by reading both `Makefile`'s `test:` target and `.github/workflows/ci.yml`'s `build-test` job directly (not solely on the reviewer's citation) — identical: `go test ./... -race -count=1`. Ran it on D, the post-rebase tip:

- `go test ./... -race -count=1` — all 7 packages `ok`, exit 0
- Re-ran with `-json`, parsed exact per-test counts: **696 PASS, 0 FAIL, 0 SKIP**

Neither bead's cited count matches exactly: crn-1fpa's description cites 695 PASS; crn-mmbq's review notes cite 544 PASS (substantially lower, unreconciled — likely an earlier or partial run, not investigated further since it is superseded by a fresh, real run on the actual deployed tree). The 695-vs-696 gap is consistent with the bounded self-rebase landing one incidental additional passing test from origin/main's tip. Zero FAIL, zero SKIP under my independently-run count, which is authoritative for this gate. **PASS.**

## 4. No open blocking findings

Diff is docs plus a single new test file (`os.ReadFile` of static doc paths, string containment checks) — no production code touched, no new dependencies, no network/deserialization/injection surface, no secrets. Reviewer recorded 0 style findings, 0 security findings.

Independently re-ran on D myself: `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` clean (zero output), and `golangci-lint run ./...` (cache cleared first to avoid stale-cache false positives, matching the crn-mvnt-gate precedent) — **0 issues**. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty prior to writing this gate doc. **PASS.**

## 7. Single coherent theme

3 files, +57/-0, one cohesive theme: correcting the freshness/anchor documentation to state a defensible detection-not-correctness claim, plus documenting the pre-existing `type`-field convention. No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
