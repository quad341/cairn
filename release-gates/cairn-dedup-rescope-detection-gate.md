# Release Gate: cairn dedup/re-scope detection step (crn-0yv.3)

Deploy source: `e76112b16c4fcded8a714f9195ea554c25932d6d` (feat(cairn):
dedup/re-scope detection step (crn-0yv.3)) on `feature/crn-0yv.3`, rebased
onto `origin/main` @ `c86480c16997f63b74a04c9cce61c1b70675ea40` from the
originally-reviewed `9b9fe32a90870f1bbed1979f86744268d1773293`.

Branch `deploy/crn-s1x-gate` cut from `e76112b` via `resolve_deploy_branch_target`,
then self-rebased onto current `origin/main` and pushed via
`attempt_bounded_self_rebase` — tip is now `442a2818440c5e24bd41b584d7d1bf55016e3a5b`.
See "Deploy branch cut, self-rebased, and pushed" below.

## History

Criterion 6 FAILed on `9b9fe32` (origin/main had advanced to `c86480c` since
the branch's `65374e1` fork point; see git history of this file for the
original FAIL table). Routed back to builder per protocol. Builder rebased
`feature/crn-0yv.3` onto current `origin/main` in a scratch worktree,
surfaced and fixed a build break in the process (see below), and routed back
to deployer with the rebase+fix reasoning made explicit rather than assuming
it's silently still covered by the original review. This evaluation
independently re-runs the full gate against the result, `e76112b`.

A second blocker followed: `scripts/rebase-resolve-lib.sh` (needed for
`resolve_deploy_branch_target`/`assert_safe_push_target`/`attempt_bounded_self_rebase`)
was absent from every cairn worktree, parking this bead (and several others)
under `hold:mayor`. Root-caused and fixed via `crn-elja` (ported the
canonical script from `gc-management`'s
`packs/actual/deployer/scripts/rebase-resolve-lib.sh`); `crn-elja.1` closed,
landed on `origin/main`. This bead's `crn-elja.1` dependency is now
satisfied, clearing the way to cut a real deploy branch instead of continuing
to hold.

Evaluation order per protocol: criterion 6 checked first.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | **PASS** | Re-fetched `origin/main` at branch-cut time (tip had advanced to `128c214bc5bb7c164cc953ba54b5080f40c27424` since the builder's `c86480c` rebase target — expected, given the intervening `crn-elja.1` infra landing). `resolve_deploy_branch_target` cut `deploy/crn-s1x-gate` at `e76112b`; `attempt_bounded_self_rebase deploy/crn-s1x-gate main` then rebased it onto the current `origin/main` tip (`128c214`) — clean, zero conflicts (`BEFORE_SHA=e76112b16c4fcded8a714f9195ea554c25932d6d`, `AFTER_SHA=442a2818440c5e24bd41b584d7d1bf55016e3a5b`), matching an earlier `git merge-tree --write-tree` clean-merge prediction, and force-with-lease-pushed to `origin` by the function itself. Independently re-verified post-push via `git fetch origin && git rev-parse origin/deploy/crn-s1x-gate` — confirmed `442a281` landed on the remote, not just trusting the function's own rc=0. |
| 1 | Review PASS present | **PASS** | crn-wuj closed `reason=pass`: full 10-point checklist independently verified in reviewer's own isolated worktree (build/vet/lint/test -race re-run, Jaccard arithmetic hand-recomputed, revert-and-confirm-fails check redone for the `scopeSuperset` guard). Scoped diff `9b9fe32..e76112b` independently confirmed the only change since that review is deletion of `entryTier`, a helper never exercised by the reviewed checklist. The subsequent self-rebase (`e76112b`→`442a281`) changed only the base commit, not the content: `git diff e76112b 442a281 -- cmd/dedup.go internal/cairn/dedup.go internal/cairn/dedup_test.go docs/plans/cairn-librarian-dedup-detection-beads.md` is empty. No re-review loop required. |
| 2 | Acceptance criteria met | **PASS**\* | AC1, AC2, AC4, AC5 implemented, tested, and reviewed. AC3 (idempotent filing across sweep cycles) is intentionally deferred to crn-0yv.5 per the epic's own multi-bead decomposition — crn-0yv.3's scope is detection only (`Dedup()`), fully designed in `docs/plans/cairn-librarian-dedup-detection-beads.md`. This scope split was itself reviewed and explicitly accepted by crn-wuj. One real gap in that *deferred* design (bracket-anchor collision-safety in `ValidatePathSegment`) was found and filed separately as crn-ryi (P3) — explicitly non-blocking for *this* bead. |
| 3 | Tests pass | **PASS** | Re-run fresh on the post-rebase tip `442a281` (not just relying on the pre-rebase `e76112b` run): `go build ./...` clean, `gofmt -l .` no output, `go vet ./...` clean, `golangci-lint run ./...` 0 issues, `go test ./... -race -count=1` all pass (`cmd`, `internal/cairn`, `internal/critic`, and the now-present `scripts` package from the landed `crn-elja.1` port). |
| 4 | No high-severity review findings open | **PASS** | Only open follow-up against this line of work is crn-ryi (P3, bug), explicitly scoped to the *deferred* crn-0yv.5 filing wiring, not to crn-0yv.3 itself. No HIGH-severity findings open. |
| 5 | Final branch is clean | **PASS** | `git status --porcelain` on `deploy/crn-s1x-gate` at `442a281`: clean except the pre-existing, unrelated worktree scaffolding (`.gc/`, `.gitkeep`) present in every cairn deployer worktree. |
| 7 | Single feature theme | **PASS** | Diff vs current `origin/main` touches exactly `cmd/dedup.go`, `internal/cairn/dedup.go`, `internal/cairn/dedup_test.go`, `docs/plans/cairn-librarian-dedup-detection-beads.md` (818 insertions, 4 files) — one coherent detection-step theme, nothing bundled in. |

**Result: PASS (all 7 criteria).**

## Scoped diff confirming review validity (`9b9fe32` → `e76112b`)

`git diff 9b9fe32 e76112b -- cmd/dedup.go internal/cairn/dedup.go internal/cairn/dedup_test.go docs/plans/cairn-librarian-dedup-detection-beads.md`
shows exactly one hunk: removal of the now-unused `"path/filepath"` import
and the entire `entryTier` function (doc comment + body) from
`internal/cairn/dedup.go`. The call site (`tier := entryTier(store, e)`) is
untouched and now resolves to `internal/cairn/sweep.go`'s already-reviewed,
already-merged (PR #17) identical copy. `cmd/dedup.go`,
`internal/cairn/dedup_test.go`, and the plan doc have zero diff between the
two commits.

## Deploy branch cut, self-rebased, and pushed

The infra blocker (`scripts/rebase-resolve-lib.sh` absent rig-wide) is
resolved as of `crn-elja.1` landing on `origin/main`. This bead's deploy
branch was then cut and pushed using the real library functions, not a
hand-rolled substitute:

1. `resolve_deploy_branch_target crn-s1x e76112b16c4fcded8a714f9195ea554c25932d6d`
   → `deploy/crn-s1x-gate` (checked out at `e76112b`).
2. `attempt_bounded_self_rebase deploy/crn-s1x-gate main` — first attempt
   returned rc=10 (setup failure): the strict, literal
   `git status --porcelain` emptiness check the function requires was
   tripped by the pre-existing untracked `.gc/`/`.gitkeep` worktree
   scaffolding present in every cairn deployer worktree (not code, not a
   real dirty-tree condition). Worked around by `git stash -u` immediately
   before the call and `git stash pop` immediately after, confirmed clean
   before and restored after via `git status --porcelain`. Retried: rc=0,
   `BEFORE_SHA=e76112b16c4fcded8a714f9195ea554c25932d6d`,
   `AFTER_SHA=442a2818440c5e24bd41b584d7d1bf55016e3a5b`, force-with-lease
   push succeeded.
3. Independently re-verified the push against the remote (`git fetch origin`
   + `git rev-parse origin/deploy/crn-s1x-gate` → `442a281`), rather than
   trusting the function's own rc=0 alone.
4. Re-ran the full gate fresh on `442a281` (criterion 3 above) rather than
   relying solely on the pre-rebase `e76112b` run or the `git merge-tree`
   clean-merge prediction.

**Verdict: PASS — proceeding to PR.**
