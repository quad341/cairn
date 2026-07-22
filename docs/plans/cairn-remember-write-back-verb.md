# Plan: `cairn remember` write-back verb (curation-tier routing)

Root bead: crn-419 (source: crn-7oa architecture, §13 item 2)

## Why

`cairn remember` does not exist today. `internal/cairn/prime.go:47` already prints
the usage hint (`cairn remember --topic <slug> "<one-liner>"`), and
`prime_test.go` asserts that string is present, but there is no `rememberCmd` and
no write-new-entry function anywhere in `internal/cairn` (confirmed by a
repo-wide grep). This is a hard blocker for the passive write-back half of
crn-7oa's "wire cairn into agent workflows" mandate: agents are told to write
learnings back, and the command does not exist.

The full behavioral spec — sequence diagram AF-2, security controls (§7), and
guardrails (§12) — is already ratified in crn-7oa's architecture notes and in
`docs/DESIGN.md` §7 / `cairn-store/README.md`. This is not a new policy to
invent, it is an implementation of an existing one.

## Scope split

The architect flagged crn-419 as spanning two materially different behaviors —
private-tier direct commit vs. shared-tier branch + review — and routed it to
pm for breakdown rather than handing it to the builder as one undifferentiated
task. The decomposition below follows that seam, plus a dedicated test bead and
one escalation for a gap found while grounding the design against the current
fleet roster.

## Children

| Bead | Title | Routing | Depends on |
|---|---|---|---|
| crn-419.1 | CLI scaffold + topic_key/scope validation (path-traversal guard) | ready-to-build → cairn/builder | — |
| crn-419.2 | Construct + write entry (TOML-frontmatter markdown, matches entry.go shape) | ready-to-build → cairn/builder | crn-419.1 |
| crn-419.3 | Private-tier direct commit (agent/ scope) | ready-to-build → cairn/builder | crn-419.2 |
| crn-419.4 | Shared-tier branch + reviewer-mail path (role/rig/global scope) | ready-to-build → cairn/builder | crn-419.2 |
| crn-419.5 | Test coverage (validation corpus, both commit paths, failure handling) | needs-tests → cairn/validator | crn-419.1–4 |
| crn-419.6 | Define recipient-resolution convention for the "librarian" role | needs-architecture → cairn/architect (decision) | none (loose coordination only) |

crn-419.1 and crn-419.2 form the shared core (validate → construct → write the
entry file). crn-419.3 and crn-419.4 are the two curation-tier branches from
DESIGN.md §7 and can be built in either order once the core lands. crn-419.5 is
an independent adversarial test pass, not just inline tests from whoever
implements 1–4. All five are intended to land together as **one isolated
"remember" deploy** — decomposed into beads for review/acceptance granularity,
not because they need to ship as separate PRs. Per crn-419's own body: not
hard-blocked on crn-811, but coordinate landing timing with it if crn-811 is
still open when this is ready to deploy, and do not tangle this into crn-811's
own multi-bead re-cut.

## Open question surfaced: the "librarian" role does not exist

AF-2 and `cairn-store/README.md` both specify that shared-tier writes get
`gc mail send <librarian>` for review. Grepping this fleet
(`grep -ril librarian` across gc-management) returns nothing, and the cairn rig
roster has no librarian agent or role. Inventing a permanent recipient is an
architecture-level call (it is effectively introducing a new reviewing
identity), so:

- crn-419.4 implements the branch + commit + mail **mechanism** now, with the
  recipient as a resolvable parameter and a sensible interim default (e.g.
  mayor) — not a hardcoded permanent guess.
- crn-419.6 asks the architect to define the durable convention. It does not
  block crn-419.4.

## Acceptance criteria

See each child bead's `--acceptance` for the measurable, per-bead criteria.
Summary: reject unsafe topic_key/scope before any filesystem write; write a
spec-shaped entry file into the correct scope directory; private tier commits
straight to the store's default branch; shared tiers only ever land on a
branch plus a review mail, never a direct commit, even on mail-send failure;
none of this may block or crash the calling agent's own task (crn-7oa NFR-2).
