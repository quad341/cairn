# Release Gate: remember agent-native capture (stdin/file input, anchors, duplicate-aware writes)

- Bead: crn-lzn4.1.1 (implementation) / crn-lzn4.1 (design, referenced by reviewer) / crn-idou (deploy convoy)
- Reviewer-cited commit (R): a72f98fc553a8282c27f650e80c3f197c1bdec83 (branch builder/crn-lzn4.1.1-v2), round-2 PASS
- Final deployed commit (D): a72f98fc553a8282c27f650e80c3f197c1bdec83 (identical — no rebase required, see criterion 6)
- Evaluated: 2026-07-26, against origin/main@f5e0b60 (PR #60, "Add cairn doctor")
- Second deploy attempt on this bead. First attempt (this same file, evaluated against f13fe54c on branch builder/crn-lzn4.1.1, committed to main as d806679) FAILed on criterion 6: origin/main had advanced past this branch's fork point (PR #58) and a bounded self-rebase hit a genuine 3-hunk conflict in cmd/remember.go that was correctly left unresolved rather than force-merged. Builder rebased by hand, fixed the conflict, and pushed a new SHA; reviewer re-reviewed from scratch and returned a fresh PASS at this commit. See crn-lzn4.1.1's bd notes for the full round-1/round-2 history.

## 6. Clean divergence from main (evaluated first)

Fresh `git fetch origin` immediately before this evaluation. `git merge-base --is-ancestor origin/main origin/builder/crn-lzn4.1.1-v2` → true: origin/main (f5e0b60) is a full ancestor of this branch's tip (a72f98fc). The round-1 FAIL's rebase-and-fix already folded main (through PR #58) in; main has advanced one further commit since (PR #60, `f5e0b60`, cairn doctor — unrelated, doesn't touch any file this bead's diff touches) and this branch already contains it. No further rebase needed; `attempt_bounded_self_rebase` not invoked this round. **PASS.**

## 1. Exact SHA match (D == R)

D and R are the literal same commit, `a72f98fc553a8282c27f650e80c3f197c1bdec83`. Confirmed directly via fresh `git fetch` + `git log` that this is genuinely the current tip of `builder/crn-lzn4.1.1-v2`, not a stale reference — the branch briefly had an undocumented fixup commit land past the SHA the reviewer was first pointed at (`6927923`), builder corrected reviewer to re-review at `a72f98f`, and reviewer independently cross-checked (`git log 6927923..a72f98f`, `git ls-remote`) before trusting the correction. SHA-pinning mandate satisfied. **PASS.**

## 2. Acceptance criteria

Reviewer's round-2 pass verified all 8 functional requirements + 1 NFR from crn-lzn4.1's design and this bead's exit_contract by reading every implementation path and every asserting test body directly (not inferred from names or notes): body-source resolution (positional/--file/stdin, no short-circuit on ambiguous-pair detection), independent title/summary auto-derivation, anchor-flag construction, --verify fingerprint capture, the FR-6 dedup shadowExempt strict-XOR tightening (the round-1 KNOWN LIMITATION fix, hand-verified against the actual scopeSuperset implementation across all 3 comparability cases), and --force override sequencing/output. ~20 call-site conversions outside the bead's declared 4-file scope (internal/critic/*.go, cmd/ergonomics_scenario.go, various *_test.go) were read diff-by-diff and confirmed mechanical positional-to-struct fallout, not scope creep. **PASS.**

## 3. Tests

Reviewer's round-2 gate run, fresh (not reused from an earlier in-session pass), matching CI's invocation:
- `gofmt -l .` — clean
- `go build ./...` — exit 0
- `go vet ./...` — exit 0
- `golangci-lint run ./...` — "0 issues"
- `go test ./... -race -count=1 -shuffle=on` — all 6 packages pass (cmd 29.876s, formulas 1.082s, internal/cairn 74.672s, internal/critic 11.144s, internal/obslog 1.070s, scripts 6.153s)

Two non-blocking coverage gaps were identified (rememberBody's zero-source error path untested; NewEntryParams' single-field title/summary auto-derivation untested at both CLI and package level) and filed to `crn-lzn4.2` (P3, needs-tests) rather than blocking this pass — both are additive-coverage gaps on already-tested code paths, not unverified behavior. **PASS.**

## 4. No open blocking findings

Reviewer's round-2 security pass (OWASP-style, freshly re-verified rather than carried over from round 1) found zero vulnerabilities: `--file` reads via `os.ReadFile` sit at the same trust boundary as any file-path flag; `--anchor-*`/`--verify` feed into pre-existing, unchanged (confirmed zero diff vs. main) `internal/cairn/freshness.go` with argv-style git invocation and explicit `--` pathspec separation; `--force` is auditable, not destructive; no new third-party dependencies. One doc-quality nit (a stale test-comment cross-reference and now-backwards rationale, `cmd/remember_test.go:968-973`) was found and rolled into `crn-lzn4.2` alongside the coverage gaps — not functional, not blocking.

Independently swept fleet-wide open issues: zero open P0. Open P1s (`crn-3qe6`, `crn-9p6g`, `crn-owim`, `crn-ubhh`, `crn-ulmg`, `crn-vvxk`, `crn-w81a`, `crn-sns6`, `crn-k1hw`, `crn-7vrp`, `crn-3ox4`, `crn-t1r9`, `crn-633u`, `crn-e93v`, `crn-ph0i`, `crn-prh7`, `crn-6qbb`, `crn-vm4g`, `crn-tq8s`) are generic molecule-step tracking beads and unrelated fleet/tooling infrastructure bugs (rebase-lib zsh portability, concurrent-session dedup, branch-freshness hook, actor-identity string normalization, deployer handoff-verification) — none reference crn-lzn4.1.1 or touch any file this diff touches. **PASS.**

## 5. Clean working tree

Worktree at HEAD has no tracked modifications; the only local artifact is this gate doc itself, freshly written for this evaluation (an earlier, stale draft evaluating the superseded round-1 SHA existed uncommitted and is being replaced by this file, not added alongside it). **PASS.**

## 7. Single coherent theme

Agent-native capture for `cairn remember`: stdin/file body input, anchor flags, --verify, duplicate-aware writes (--force + the FR-6 dedup tightening) — one coherent theme per crn-lzn4.1's design. The only content beyond the original round-1 diff is the round-2 rebase-and-fix itself (reconciling two independent `cairn.NewEntry` call-shape evolutions against main's PR #58, resolving a duplicate print line, and interleaving `RememberResult` with three new helper functions) — a same-theme mergeability fix forced by a sibling PR touching the same function, not an independent feature bundled in. **PASS.**

## Verdict: GATE PASS (7/7) — proceeding to isolated deploy branch push + PR.
