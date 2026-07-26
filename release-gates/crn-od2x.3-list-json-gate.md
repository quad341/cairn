# Release Gate: crn-od2x.3 — wire --json + resolveIdentityValidated into `cairn list <topic>`

**Bead:** crn-od2x.3
**Evaluated:** 2026-07-26 (round 3, deployer-independent final pass)
**Deploy source (D):** `ebfae1105af4eb7d7f57067ab6677a02a41100db`
(branch `builder/crn-od2x.3` tip after round-2 fresh reviewer re-review; two prior rounds of this
gate — see history below — are superseded by this evaluation)

## History

- Round 1: reviewer PASS at `f06cf9470a15e7cc4b5b601afc06c9aa8684427c`. Deployer gate FAIL at
  criterion 6 (`origin/main` had advanced past base); resolved via bounded self-rebase
  (`f06cf947` → `89b98b308ac9986861877f60ef5681c9ed650594`, rc=0, zero conflicts). That SHA change
  then FAILed criterion 1 per the SHA-pinning mandate (reviewer's PASS no longer covered the new
  SHA) and was routed to `cairn/reviewer` for re-confirmation.
- Round 2: reviewer re-reviewed at the new SHA, confirmed via `git diff | git patch-id --stable`
  that content is byte-identical across `f06cf947` → `89b98b30` → `ebfae1105af` (the branch's final
  tip after a second necessary rebase), and issued "VERDICT STANDS: PASS at
  `ebfae1105af4eb7d7f57067ab6677a02a41100db`". Confirmed the branch tip is exactly that SHA plus two
  gate-doc-only chore commits on top, which do not touch reviewed source.
- Round 3 (this evaluation): full independent re-verification by deployer, all 7 criteria, gated
  directly against `ebfae1105af4eb7d7f57067ab6677a02a41100db`.

## Criterion 6 — Branch diverges cleanly from main: PASS

Re-fetched `origin/main` immediately before this evaluation: `f5e0b60` ("Add cairn doctor:
aggregation, tolerant iteration, explain modes (#60)"). `git merge-base --is-ancestor origin/main
ebfae1105af4eb7d7f57067ab6677a02a41100db` → true. No further rebase needed.

## Criterion 1 — Review PASS present, SHA match: PASS

D == R == `ebfae1105af4eb7d7f57067ab6677a02a41100db` exactly. Reviewer's round-2 verdict is pinned
to this precise SHA, per the SHA-pinning mandate.

## Criterion 2 — Acceptance criteria met: PASS

Reviewer's round-1 review confirmed all 6 bead scope points, NFRs, and an OWASP-style security pass
CONFIRMED, and round 2 proved via patch-id that content is unchanged since. Independently spot-
checked this round: `git diff origin/main ebfae1105af... -- cmd/list.go` shows exactly the
described change — new `ListResult` struct (`ID`/`Title`/`Summary`/`Scope`/`Freshness`, matching
JSON tags), `resolveIdentity` → `resolveIdentityValidated` swap with `emitError` wrapping, the
empty-result path reclassified through `classifiedErr(CategoryNotFound, ...)`, and a `wantsJSON`
branch emitting `[]ListResult` via `emitJSON` while leaving the human-text path byte-identical. This
mirrors the pattern already shipped for `get`/`status`/`map`/`prime`/`remember`.

## Criterion 3 — Tests pass: PASS

Independently re-ran the full gate in an isolated worktree (`git worktree add --detach` at
`ebfae1105af4eb7d7f57067ab6677a02a41100db`), not trusting the reviewer's report alone:

```
gofmt -l .                                    → clean, zero files
go build ./...                                → exit 0
go vet ./...                                  → exit 0
golangci-lint cache clean && golangci-lint run ./...  → 0 issues
go test ./... -race -count=1 -shuffle=on      → ok, all packages
```

All packages passed: `cmd` (6.070s), `formulas` (1.032s), `internal/cairn` (8.774s),
`internal/critic` (4.779s), `internal/obslog` (1.012s), `scripts` (1.908s). Matches the reviewer's
independently-reported round-2 results exactly.

## Criterion 4 — No HIGH findings open: PASS

Reviewer's round-1 security review (OWASP-style pass over the new JSON-emission and identity-
validation path) found nothing HIGH or above; round-2 patch-id proof confirms no content changed
since that review.

## Criterion 5 — Final branch clean: PASS

Deploy branch `deploy/crn-od2x.3-gate` cut directly from `ebfae1105af4eb7d7f57067ab6677a02a41100db`
via `resolve_deploy_branch_target`. `git diff --stat HEAD -- ':!release-gates'` is empty immediately
after cut — no stray tracked changes beyond this gate document.

## Criterion 7 — Single feature theme: PASS

Diff is exactly two files, `cmd/list.go` (+36/-3) and `cmd/commands_json_test.go` (+40), both
serving one theme: bringing `cairn list <topic>` in line with the other five commands' `--json` +
validated-identity contract. No unrelated changes.

## Result: PASS (7/7)

## Action

Cutting isolated branch `deploy/crn-od2x.3-gate` from `ebfae1105af4eb7d7f57067ab6677a02a41100db`,
opening PR, arming GitHub-native auto-merge.
