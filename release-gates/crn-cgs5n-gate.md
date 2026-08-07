# Release Gate: Implement prime index/payload separation, cost caps, and unified ranking

- Bead: crn-cgs5n (deploy) / crn-lud1n (review, PASS round 2) / builder/crn-3476 (provenance branch, not a push target)
- Reviewer-cited commit (R): `64b816661cb0c3e376a4273c3a699af9a791d952`
- Final deployed commit (D): `64b816661cb0c3e376a4273c3a699af9a791d952` (identical to R — origin/main was already an ancestor, no self-rebase required)
- Evaluated: 2026-08-07, against origin/main@2ba5035 (#77, "Docs: clarify that freshness checks detect change, not correctness")
- Downstream: crn-svp6c (sling-crn-cgs5n) is a routing convoy blocked on this bead closing — not a merge dependency, not blocking this gate

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before this evaluation. origin/main tip is `2ba5035` (#77). `git merge-base origin/main 64b8166` returns `2ba503524d813831f2f4a43512c042cd9c53b990`, identical to `git rev-parse origin/main` — D's own commit chain (`688c8c4` → `ca50912` → `64b8166`) already contains origin/main's tip as a direct ancestor. No bounded self-rebase needed. Additionally ran `git merge-tree <merge-base> origin/main 64b8166` across all 13 changed files: zero true conflicts (the one raw `CONFLICT` string match is a false positive — the literal SQL text `ON CONFLICT(id) DO UPDATE SET` inside `index.go`'s diff hunk, not a git-generated conflict banner; confirmed no `CONFLICT (content):` banner and no `<<<<<<<`/`>>>>>>>` diff3 markers anywhere in the output). **PASS.**

## 1. Exact SHA match (D == R)

R = `64b816661cb0c3e376a4273c3a699af9a791d952`, the commit crn-lud1n's round-2 verdict explicitly cites as PASS (`**Commit:**` field), matching crn-cgs5n's own `commit` field exactly. No rebase was applied (criterion 6 already structurally satisfied), so D == R literally. **PASS.**

## 2. Acceptance criteria

crn-zcxq's design (FR-1–FR-8, NFR-1–NFR-4: index/payload separation, per-item cost caps at write+read time, cold-start exploration band via RFC3339 timestamps, and an `OverriddenDuplicateOf` hard-override unifying crn-h5zx into the same ranking decision) is implemented per crn-3476's build summary. Independently spot-checked the two highest-risk requirements directly against source at D, rather than relying solely on the reviewer's report:

- **FR-6/NFR-4** (override hard-win + cycle guard) — `entry.go`'s `moreSpecificReason`: computes `aOverridesB`/`bOverridesA`, then `if aOverridesB != bOverridesA { return aOverridesB, "override" }`. This XOR correctly gives an unconditional win to whichever single side overrides the other, checked before the scope/verified_at/created_at/id chain; a malformed mutual double-override makes the XOR false and falls through safely to the existing chain instead of looping or picking an undefined winner. Matches NFR-4 exactly.
- **FR-3/FR-7** (write+read-time caps as tunable vars) — `validate.go`'s `ValidateTitleLength`/`ValidateSummaryLength` check `utf8.RuneCountInString` against package-level `titleCap = 100`/`summaryCap = 280` (vars, not consts, per the `maxFreshnessChecksPerPrime` convention), wired into `cmd/remember.go`'s explicit-flag path (rejects over-cap with `CategoryInvalidInput`).

Both rounds of crn-lud1n's independent review are additionally relied on: round 1 found 2 BLOCKERs + 1 minor finding; round 2 confirmed both blocker fixes by diff-read (not commit-message trust) and reran the full gate in an isolated worktree before PASSing. The cumulative diff vs origin/main (13 files, +572/-57) maps cleanly onto the FR/NFR file list — `prime.go`, `entry.go`, `remember.go`+`cmd/remember.go`, `index.go`, `cull.go`, `validate.go` (all `internal/cairn`/`cmd`), plus `docs/DESIGN.md` §5's wording fix per the design's own §14 handoff — no files outside that set. **PASS.**

## 3. Tests

Canonical command per `Makefile`'s `test:` target: `go test ./... -race -count=1`. Ran independently on D (detached HEAD at `64b8166`):

- `go build ./...` clean, `go vet ./...` clean, `gofmt -l .` clean (zero output), `golangci-lint run ./...` — **0 issues**.
- `go test ./... -race -count=1` — all 7 packages (`cairn`, `cmd`, `formulas`, `internal/cairn`, `internal/critic`, `internal/obslog`, `scripts`) report `ok`, exit 0.

`internal/critic` passed clean this run despite a known pre-existing flake (crn-tw3bl, ~33-67% observed fail rate on `TestRunPerfScenario*`, a tempdir/`.git` cleanup race) not manifesting — independently confirmed unrelated to this diff (see criterion 4). **PASS.**

## 4. No open blocking findings

Two open beads reference this work, neither a HIGH finding against crn-cgs5n's implementation:

- **crn-tw3bl** (P2, `flaky-test`): `internal/critic` tempdir/`.git` cleanup race. The reviewer independently reproduced it both on D's own diff in isolation (4/6 failed, `-count=3`) and on the pre-fix GREEN commit `ca50912` one commit earlier (identical failure) — confirmed pre-existing, not introduced by this diff. `internal/critic` imports `internal/cairn` one-way (`go list -deps` confirmed), not the reverse, so it cannot be downstream of this change either. Non-blocking.
- **crn-umk0r** (P1, `human`-labeled BLOCKER): GitHub Actions has produced zero check suites for `quad341/cairn` since 2026-08-04T19:52:24Z — a personal-GitHub-account-level issue needing operator access no rig agent has. Independently reconfirmed live during this evaluation (`gh run list`: no runs since that timestamp; PRs #78/#79 both OPEN/BLOCKED/zero-checks). This is a deploy-mechanism/CI-visibility issue, not a finding against this diff's code, and is already tracked and already escalated to mayor by a prior deployer run (see the caveat below the verdict).

No HIGH-severity finding alleges a defect in this diff. **PASS.**

## 5. Clean working tree

`git status --porcelain` empty at D (detached HEAD, no local modifications). **PASS.**

## 7. Single coherent theme

13 files, all under `internal/cairn/`, `cmd/remember*.go`, and `docs/DESIGN.md` — one cohesive architectural change (prime index/payload separation, per-item cost caps, and unified cold-start/explicit-override ranking), unifying crn-zcxq and crn-h5zx per the design's own §0 Finding 4 rationale ("same tie-break cascade, different call site... fixed together as one ranking-function decision"). No unrelated changes bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.

**Known infra caveat (not a gate criterion).** GitHub Actions is confirmed still dead for this repo as of this evaluation (`gh run list`: zero runs since 2026-08-04T19:52:24Z; PRs #78/#79 both OPEN/`BLOCKED`/zero status checks). Tracked in crn-umk0r (P1, `human`-labeled), already escalated to mayor by a prior deployer run when the first two PRs hit this. This PR will be the third affected. Proceeding per the documented policy: a `none` CI state after the 120s bound is not a gate FAIL — arm auto-merge anyway (the merge queue is the real backstop once checks resume), and the close reason will honestly record the CI state observed at arm time rather than claim a verified-green arm.
