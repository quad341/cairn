# Release gate: topic_key specificity precedence in Visible()

Bead: crn-1qb (from crn-ki1 review, crn-9l6 feature)
Commit evaluated: `beaaee191672ce25b408ffd155bc6bb6c010dc3a`
Feature branch: `gc-builder-6ac3e0f3c1f3`

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `git merge-tree --write-tree gc-builder-6ac3e0f3c1f3 origin/main` exits 0, no conflict markers. Branch showed 1 commit "behind" (`5edc21d`) but its tree is content-identical to the branch's own `78f450c` (`git diff 78f450c 5edc21d` empty) — a squash-merge duplicate, not a real conflict. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-ki1 notes: "REVIEW VERDICT: PASS" by cairn/reviewer. |
| 2 | Acceptance criteria met | PASS | Independently read `internal/cairn/entry.go` diff: `shadow()`/`moreSpecific()` match crn-9l6's spec verbatim (winner-map keyed on TopicKey, len(Scope) comparator, VerifiedAt>CreatedAt>ID tiebreak, empty-TopicKey skip). Matches crn-9l6 acceptance criteria and FR-1..FR-6. |
| 3 | Tests pass | PASS | `go test ./...` at `beaaee191672ce25b408ffd155bc6bb6c010dc3a`: full suite green, including the 4 new tests (`TestVisibleShadowsBySpecificity`, `TestVisibleShadowTiebreakVerifiedAt`, `TestVisibleShadowTiebreakID`, `TestVisibleUntopicedNeverShadow`) and no regressions in `TestVisible`/`TestPrime`/`TestPrimeEmpty`. Also independently re-ran `gofmt -l` (clean), `go build ./...` (clean), `go vet ./...` (clean), `golangci-lint run ./...` (0 issues). |
| 4 | No high-severity review findings open | PASS | Review notes list only 2 non-blocking nits (untested CreatedAt tiebreak tier; CreatedAt never written today) — 0 HIGH findings. |
| 5 | Final branch is clean | PASS | `git status` clean at commit `beaaee191672ce25b408ffd155bc6bb6c010dc3a`. |
| 7 | Single feature theme | PASS | Commit `beaaee19` touches exactly `internal/cairn/entry.go` + `internal/cairn/entry_test.go` — one subsystem (`Visible()`/shadowing), no unrelated changes. |

**Verdict: PASS — all 7 criteria met.** Proceeding to push + PR, merge-request routed to mayor.
