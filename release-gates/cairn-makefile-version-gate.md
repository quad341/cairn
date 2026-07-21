# Release Gate: cairn-makefile-version

**Bead:** crn-psq (deploy) — source review crn-i75 — implementation crn-di7
**Commit:** `a826f1956af06dd8831b6c275300a16319beeb1c`, cut onto `deploy/crn-psq-gate` off `origin/main`
**Date:** 2026-07-21

## Criteria

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 6 | Branch diverges cleanly from main | PASS | `a826f19`'s sole parent is `571515d`, exactly `origin/main`'s current tip (re-fetched immediately before evaluation). `git merge-tree --write-tree a826f19 origin/main` produces a single clean tree hash, no conflict markers. No self-rebase needed. |
| 1 | Review PASS present | PASS | crn-i75 closed (reason: pass) with "VERDICT: pass" from cairn/reviewer, independently re-verified on this exact commit (not the earlier `1ab64f5`) in an isolated scratch worktree — build/vet/fmt/lint/test all clean, diff-of-diffs confirmed byte-identical to the pre-rebase commit (pure rebase, zero logic drift). |
| 2 | Acceptance criteria met | PASS | crn-di7's plan: root Makefile with build/test/install/fmt/fmt-check/clean/help targets, ldflags-stamped version metadata, and a `cairn version` command. Independently exercised every target against `a826f19` in this session (see below) — all behave as specified. |
| 3 | Tests pass | PASS | `go build ./...`, `go vet ./...` clean. `gofmt -l .` empty. `golangci-lint run ./...` — 0 issues. `go test ./... -race -count=1` — both packages ok, zero regressions (also re-run via `make test`, same result). |
| 4 | No high-severity review findings open | PASS | crn-i75's review notes contain only the PASS verdict and confirmatory detail — no HIGH (or any-severity) findings recorded. `bd search HIGH` returns nothing for this bead chain. |
| 5 | Final branch clean | PASS | `git status --porcelain` clean on the deploy branch (no modified/staged files); only the pre-existing, unrelated worktree scaffolding `.gc/`/`.gitkeep` remain untracked (present before this session's work began). |
| 7 | Single feature theme | PASS | Commit touches only `.gitignore`, `Makefile`, `cmd/version.go`, `cmd/version_test.go` — one subsystem (build tooling / version metadata). No unrelated changes bundled. |

## Manual target verification (independent of reviewer's report)

- `make help` — lists all 7 targets with descriptions.
- `make build` — `./cairn version` → `cairn version dev (commit a826f19, built <UTC timestamp>)`; ldflags correctly stamp the commit.
- `make test` — `go test ./... -race -count=1`, both packages ok.
- `make fmt-check` — `golangci-lint fmt -d ./...`, exit 0, no diff.
- `make install INSTALL_DIR=<scratch dir>` — atomic install verified against a scratch directory (not the live `~/.local/bin`, which the mayor's stopgap binary occupies); installed binary runs and reports the correct stamped version.
- `make clean` — removes the built binary.

## Verdict: PASS — proceeding to PR.
