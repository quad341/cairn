# Release Gate: cairn PromotedBeadID writer + promote-mark CLI

Isolated multi-commit deploy for crn-ghn8.1 (crn-2m8r). Source branch
`gc-builder-769138d1bf3c` is the persistent bare-commit-fallback builder
branch and carries stale duplicate history relative to `origin/main` (its
base, `58ffab6`, is already squash-merged as PR #49 -> `4858187`, confirmed
content-identical). Per the reviewer's explicit merge policy, only the 7 new
commits on top of that base were cherry-picked onto a fresh branch off
`origin/main` rather than merging the branch directly.

Deploy source: `537eb7e` (feat(cairn): green — promote-mark, refs
crn-ghn8.1), cherry-picked as the range `58ffab6..537eb7e` (7 commits) onto
`deploy/crn-2m8r-gate` = `origin/main` @
`4858187c65155cfbdd65842f5b53334401363df9` + 7 commits, new tip
`dd3d238713eceb2da53c389320236c6901e701fe`.

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Review PASS present for the deployed commit | PASS | crn-2m8r/crn-ghn8.1: cairn/reviewer PASS, 9 checklist items independently re-verified in an isolated worktree (not trusted from the builder's own claims). Reviewer's PASS cites `537eb7e` explicitly as the deployed commit `R`; deploy SHA `D` (post-cherry-pick tip content) == `R`. |
| 2 | Acceptance criteria met | PASS | `WriteBackPromotedBeadID`/`patchPromotedBeadID` (`internal/cairn/entry.go`) mirror the shipped `WriteBackRecurrenceCount`/`patchRecurrenceCount` shell exactly. `CommitPromotionToReviewBranch` (`internal/cairn/remember.go`) mirrors `CommitRecurrenceToReviewBranch`'s reuse-if-exists shape. `cmd/promote.go`'s `promote-mark` RunE requires `--bead`, checks same-bead-ID no-op success before different-bead-ID hard error, then dispatches on `cairn.IsPrivateScope(e.Scope)` -> `CommitDirect` else `requestPromotionReview` — matches the bead spec. `EntryForEvict` -> `EntryByID` rename applied cleanly across `internal/cairn/evict.go`, `evict_test.go`, `cmd/cull.go`. |
| 3 | Tests pass | **FAIL (revised post-push)** | Locally on the assembled `deploy/crn-2m8r-gate` branch: `go build ./...` clean, `go vet ./...` clean, `go test ./... -race` — all 5 testable packages green. `golangci-lint run ./...` flags 1 issue, `cmd/cull_test.go:30` unparam on `seedCommittedEntry`'s `topic` param. Initially assessed as the pre-existing, non-blocking crn-lox2 finding (P3) and recorded PASS here — **that assessment was wrong**. GitHub's actual CI on PR #50 came back `lint: fail` on this exact finding (`build-test: pass`), even though `cmd/cull_test.go` is byte-identical to `origin/main`'s own green `4858187` run (same golangci-lint v2.12.2 both sides — ruled out version drift). Root cause: this bead's own new `cmd/promote_test.go` adds 5 more call sites to the shared `seedCommittedEntry(t, store, topic, scope)` helper, all passing the same literal `"old-fact"` — 7 identical-value call sites total (vs. 2 on `main` alone) is what tips `unparam`'s confidence into flagging the untouched declaration. `lint` is a **required, strictly-enforced** status check on `main` (`enforce_admins: true`) — see Disposition. |
| 4 | No high-severity findings open | PASS | Reviewer recorded 0 open HIGH findings. One non-blocking signature note (`WriteBackPromotedBeadID()` is parameterless, reading `e.PromotedBeadID` from the receiver rather than the bead spec's sketched `(beadID string) error`) confirmed to be a faithful mirror of the real, also-parameterless `WriteBackRecurrenceCount()` precedent — not a deviation. |
| 5 | Final branch is clean | PASS | `git status` clean on `deploy/crn-2m8r-gate`, ahead of `origin/main` by exactly 7 commits (the reviewed range), no uncommitted changes. |
| 6 | Branch diverges cleanly from main | PASS | `deploy/crn-2m8r-gate` cut from `origin/main` (`4858187`, current tip, re-confirmed via `git ls-remote origin main` immediately before cherry-pick) + 7 cherry-picked commits; applied with zero conflicts, matching the reviewer's own `git merge-tree` dry-run verification. |
| 7 | Single feature theme | PASS | All 7 commits implement one cohesive feature — a `PromotedBeadID` writer plus the `promote-mark` CLI verb that uses it (tier-conditional dispatch: private-scope direct commit vs shared-scope review request). The `EntryForEvict` -> `EntryByID` rename is a shared-precondition cleanup enabling reuse between `cull`-evict and `promote-mark`, not an independent feature; removing it would break `promote-mark`'s lookup path. |

## Disposition

**GATE FAIL (post-push CI check).** PR #50 (https://github.com/quad341/cairn/pull/50)
was opened from `deploy/crn-2m8r-gate` onto `main`, but the bounded GitHub-CI
check before arming found `lint` red (see criterion 3) — a required,
strictly-enforced status check (`required_status_checks.contexts:
[build-test, lint]`, `enforce_admins: true`), so auto-merge was **not**
armed. Routed back to `cairn/builder` (`ready-to-build`); PR #50 left open,
unarmed, for reference. Same underlying finding as crn-lox2 (P3, open) —
fixing that (drop/justify `seedCommittedEntry`'s now-effectively-unused
`topic` param) should resolve this gate. A fresh reviewer PASS is required
at whatever new SHA results before the next deploy attempt; this branch/PR
will likely need superseding rather than reuse.

Downstream: crn-ghn8.2 (wiring the librarian's promote-candidate-beads step
to actually call `promote-mark`) remains correctly blocked/unclaimed —
still pending this deploy actually landing.
