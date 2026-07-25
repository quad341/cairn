# Release Gate: cairn CLI --json output, error categories, version/help unification

- Source bead: crn-od2x.4 (deploy handoff for crn-od2x.2)
- Deployed commit (D): c4540ff943314b8ef158e421baeaaa672dd4c908 (branch builder/crn-od2x.2)
- Reviewer-cited commit (R): c4540ff943314b8ef158e421baeaaa672dd4c908 (crn-od2x.2 reviewer verdict)

## 6. Clean divergence from main (evaluated first)
`git merge-base c4540ff9 origin/main` == origin/main's own tip (18db119f38ca).
origin/main is 0 commits ahead of the branch's merge-base; branch is 16 commits ahead of origin/main.
Pure fast-forward, zero divergence — no rebase needed. `git merge-tree --write-tree origin/main c4540ff9`
produced a clean tree hash with no conflict markers. **PASS.**

## 1. Exact SHA match (D == R)
D = c4540ff9, R = c4540ff9 (reviewer's "Reviewed at commit c4540ff9" citation in crn-od2x.2 notes).
Exact match. **PASS.**

## 2. Acceptance criteria
Reviewer walked every FR from crn-od2x.1's design against the diff by direct code inspection (not
builder self-report): --json + wantsJSON/emitJSON/ErrorCategory(4 values)/classifiedError in new
cmd/format.go; wired into get/status/map/prime/remember; resolveIdentityValidated identity hygiene;
version/--version/-v/--help unified via single printVersion(cmd); NFR-5's 6 pre-existing always-JSON
commands (sweep/dedup/recall-stats/promote-candidates/cull-candidates/stale-branches) confirmed
untouched. Independently re-verified via clean gofmt/build/vet on c4540ff9 in this session. **PASS.**

## 3. Tests
Independently re-ran `go test ./... -count=1` on c4540ff9 in an isolated checkout this session:
all packages pass clean (cmd, formulas, internal/cairn, internal/critic, scripts) — no failures,
including the two packages the reviewer had separately dispositioned as pre-existing/environmental
flakes (internal/cairn's SQLITE_BUSY timing flake, internal/critic's perf-threshold/TempDir flake).
**PASS.**

## 4. No open blocking findings
One non-blocking finding (cmd/list.go missing --json/resolveIdentityValidated wiring) filed
separately as crn-od2x.3 (P3, ready-to-build) rather than reopening this bead — orthogonal,
predates this diff (list.go merged to main after crn-od2x.1's design closed), not blocking.
**PASS.**

## 5. Clean working tree
`git status --porcelain` empty on c4540ff9. **PASS.**

## 7. Single coherent theme
--json output + 4 stable error categories + version/help unification are one coherent CLI
machine-readable-contract theme (crn-od2x epic). Not multiple independent feature themes.
**PASS.**

## Verdict: GATE PASS — proceeding to isolated deploy branch + PR.
