# Release Gate: crn-y0caj

**Bead:** crn-y0caj (deploy) — source: crn-azyph (review), build: crn-q08yt
**Commit:** `88397eb7b0a53859029afcaf16e154d8636471e8`, cut onto `deploy/crn-y0caj-gate`
**Date:** 2026-08-19

## Background

`cairn remember`'s auto-derived Title/Summary (used whenever a contributor
doesn't supply `--title`/`--summary` explicitly) were bounded with
`truncateRunes` — a hard rune-count cut with no regard for word boundaries.
Because the source text is a contributor's own prose lifted verbatim, a cut
landing mid-word reads as a bug (`...auto-derived title truncates mid-wor`)
rather than a deliberate bound.

The fix adds `truncateWords`, which bounds the same as `truncateRunes` but
backs up to the nearest preceding word boundary before the cut and appends
an ellipsis, only falling back to a hard cut when there's no room for both a
boundary and an ellipsis rune (`n <= 1`). `NewEntry` and `DerivedTitleSummary`
— the two auto-derivation call sites — now use `truncateWords` for
Title/Summary. `truncateRunes` itself is untouched and still used by Prime's
own read-time re-truncation, which needs a cheap hard ceiling over arbitrary
on-disk data rather than word-aware parsing.

Two non-blocking follow-ups were filed out of scope for this bead rather than
folded in: `crn-ts51d` (one untested branch in the new word-boundary backup
logic, independently verified correct by the reviewer via probe) and
`crn-77k30` (a pre-existing, unrelated gap in `search.go`'s summary
re-truncation that can still cut mid-word for the 241–280 rune range now that
`summaryCap` > `searchSummaryCap`). Both are P3 and filed separately; neither
blocks this deploy.

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS for exact deployed commit | PASS | crn-azyph's verdict cites `88397eb7b0a53859029afcaf16e154d8636471e8`, identical to the deploy bead's `metadata.gc.deploy_commit` — exact SHA match (D = R). Independently re-resolved this session via `git rev-parse --verify --quiet "<sha>^{commit}"`, not trusted as a transcribed string. |
| 2 | Acceptance criteria met | PASS | Verified directly against `git show 88397eb7 -- internal/cairn/remember.go internal/cairn/prime.go`, not just the reviewer's write-up: `truncateWords` added to `prime.go` alongside the untouched `truncateRunes`; both auto-derivation call sites in `remember.go` (`NewEntry`) and `prime.go` (`DerivedTitleSummary`) switched from `truncateRunes` to `truncateWords`. Matches the bead's description exactly. |
| 3 | Tests pass | PASS | Independently re-run by the deployer on `deploy/crn-y0caj-gate` (cut from `88397eb7...`), not trusting the reviewer's report alone: `go test ./... -race -count=1`, exit 0, **7/7 packages ok** — matches crn-azyph's independently-reported tally exactly. |
| 4 | No open HIGH findings | PASS | `bd list --status open` / `--status in_progress` checked for anything referencing this chain (crn-y0caj, crn-azyph, crn-q08yt): only two P3 non-blocking follow-ups (`crn-ts51d`, `crn-77k30`, both correctly filed as separate beads, out of scope by design) and routing/tracking "sling" beads (`crn-bzh6c`, `crn-jrmkr` — `gc sling` bookkeeping, no findings content). No open HIGH finding anywhere in the chain. |
| 5 | Clean tree | PASS | `deploy/crn-y0caj-gate` cut directly from `88397eb7...^{commit}`; `git status --porcelain` empty throughout. |
| 6 | Clean divergence from main | PASS | `git rev-list --left-right --count origin/main...88397eb7` → `0 2`: the deploy commit is a clean 2-commit (red/green TDD pair) fast-forward stack on top of current `origin/main` (`95086a9`), zero behind. No rebase needed, no conflict possible. |
| 7 | Single feature theme | PASS | `assert_deploy_ancestry_scope origin/main 88397eb7... crn-y0caj crn-q08yt crn-azyph` → rc=0: no `.claude/**` paths touched, and both non-merge commits in `origin/main..88397eb7` (`1038940` red, `88397eb` green) cite `crn-q08yt` in their message. No stray commits. |

## Verdict: PASS — proceeding to PR.

## Process notes

1. **Merge authority — deferred to mayor, NOT self-merged.** The deploy
   bead's own text is explicit: *"Route a merge-request to mayor/mpr — do
   NOT merge yourself; merge authority is operator/mayor/mpr only, no rig
   agent runs `gh pr merge`."* This is now the **third** consecutive
   reviewer-authored `quad341/cairn` deploy bead carrying that exact
   instruction (`crn-e6pc7`/PR#118, `crn-daxbq`/PR#119, `crn-y0caj`/this PR),
   directly contradicting the fleet's `cairn-auto-merge-requires-explicit-strategy`
   standing authorization (mayor-ruled 2026-08-15) that a 7/7-PASS +
   CI-green gate on this repo needs no escalation. That memory's own text
   pre-committed the resolution for this exact situation: *"If it recurs
   across multiple reviewer-authored beads, treat the standing authorization
   above as superseded and update this memory accordingly."* The threshold
   is now met, so this gate does not attempt `gh pr merge` in any form —
   plain or `--auto` — and instead pushes the branch, opens the PR, confirms
   CI, labels the bead `hold:mayor`, leaves it open, and mails mayor a
   verified merge-request. The fleet memory is being updated in the same
   session to record this as the resolving third occurrence, per its own
   instruction, rather than leaving the next deployer session to
   independently rediscover the same pattern a fourth time.

   For contrast: an earlier gate this session (`crn-62m6k`, `release-gates/doctor-explain-malformed-identity-gate.md`,
   dated 2026-08-18 — i.e. before the conflict was first flagged) *did*
   self-merge citing this same standing authorization. That gate predates
   the `OPEN CONFLICT flagged 2026-08-19` addendum and is not being treated
   as a valid counter-precedent here; the balance of evidence (3 explicit
   bead-body overrides since, 0 further self-merges) points the other way.

2. **Branch target:** `builder/crn-q08yt` is provenance-only per the deploy
   bead's own instruction — a possibly shared builder branch, not a push
   target. `deploy/crn-y0caj-gate` was cut fresh from the exact reviewed SHA.

3. **SHA integrity:** the deploy commit was independently re-resolved via
   `git rev-parse --verify --quiet "<sha>^{commit}"` before any gate step
   ran, per the sha-integrity discipline — never trusted as an eyeballed or
   transcribed string.
