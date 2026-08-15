# Release Gate — crn-g03qd (deploy bead for crn-d04vw: document 'remember --batch-file' in public docs)

**Deployed commit:** `9c754098b2b99a9e3f507794fa9fcd4f217189e0`
**Source branch (provenance only):** `builder/crn-vkd4l`
**Deploy branch:** `deploy/crn-g03qd-gate`
**Review bead:** crn-d04vw (verdict: pass, reviewer session reviewer-gm-wisp-p3v5xy)

## Evaluation order note

Criterion 6 evaluated first per process. `origin/main..DEPLOY_SHA` is `0 behind, 1 ahead` —
the deploy commit is a single commit sitting directly on top of `origin/main`'s current tip
(`eb7a2c5`), so there is nothing to reconcile. `git merge-tree --write-tree origin/main
9c754098...` returned a single clean tree SHA (no conflict markers). No self-rebase required.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit (SHA match) | PASS | R (reviewer verdict SHA, `deploy_commit` stamped in crn-d04vw's notes) = D (deploy source SHA, bead's `**Commit:**` field) = `9c754098b2b99a9e3f507794fa9fcd4f217189e0`. Exact match, no delta to re-review. |
| 2 | Acceptance criteria met | PASS | Diff is exactly 3 files (README.md, docs/DESIGN.md, docs/knowledge-lifecycle.md), read in full: README capability table row now lists `--batch-file`; DESIGN.md CLI reference gains `cairn remember --batch-file <path>`; knowledge-lifecycle.md gains a full "Batch capture" subsection. Independently spot-checked one implementation fact the docs assert: `maxBatchLines = 5000` (`cmd/remember_batch.go:20`) and `rejectSingleEntryFlags` (`cmd/remember_batch.go:36`) both exist exactly as documented. Reviewer's notes additionally cross-checked every other doc claim (manifest field names, `BatchResult`/`BatchEntryResult` shape) line-by-line against source at this same SHA. |
| 3 | Tests pass | PASS | `go test ./... -race -count=1` (Makefile `test:` target) on the deploy commit, reproduced independently: 7/7 packages `ok` (root, cmd 76.7s, formulas, internal/cairn 55.1s, internal/critic 26.7s, internal/obslog, scripts), 0 FAIL. `diff_tests_executed: none` — no test files in diff (docs-only), matches the docs-only-bead precedent (PR #52); no diff-owned SKIP to justify. |
| 4 | No high-severity review findings open | PASS | Reviewer's OWASP delta walk found no findings (pure prose diff — no code, no auth/secrets/injection surface). Independently searched bd for any open findings/escalation bead referencing crn-g03qd / crn-d04vw / crn-vkd4l: none exist; `needs-claude-review` queue is empty. |
| 5 | Final branch is clean | PASS | `git status` at `9c754098` (post-checkout): working tree clean before the gate-doc commit. |
| 6 | Branch diverges cleanly from main | PASS | See evaluation-order note above. `git merge-tree` clean; no self-rebase needed. |
| 7 | Single feature theme | PASS | All 3 touched files document one feature (`remember --batch-file`) across the README overview table, the CLI reference, and a new lifecycle-doc subsection. One subsystem, one doc surface. |

**Build/vet:** independently re-run on the deploy commit — `go build ./...` clean, `go vet
./...` clean. `gofmt` not applicable: diff touches zero `.go` files (docs-only).

## Verdict: 7/7 PASS

Standing authorization for quad341/cairn (gate 7/7 PASS + CI green ⇒ deployer arms/merges
directly, no mayor escalation required) applies. Proceeding to PR + CI check + auto-merge arm.
