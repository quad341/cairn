# Plan: CAPTURE/AUTO-ACT/PROMOTE/CULL cairn extensions (incident→knowledge feedback loop)

Root bead: crn-28ge.1 (source: crn-28ge architecture doc, full design in the
parent bead's `design` field)

## Why

cairn's RECALL path is fully built. The other four stages of the
incident→knowledge feedback loop are not: entries don't track how often
they're re-hit (no capture-time recurrence counting), nothing checks a new
remediation against conflicting existing guidance before it's allowed to
auto-act, recurring findings never graduate into a durable tracked fix, and
disused entries never get flagged for removal. crn-28ge's design doc
(FR-01–FR-10, NFR-01–NFR-07) specifies all four gaps precisely, grounded
against the current codebase — this bead decomposes that design into
builder/validator-ready children.

## Scope split

The architect's sketch enumerated the work as roughly eight items. This
decomposition produces **nine** child beads, not eight — deliberately, not by
miscount. Two deviations from a literal 1:1 split:

1. **CULL splits into two beads, not one.** The architect's sketch bundles
   "cull-candidates reporting" in with the other two read-only reporting
   subcommands (recall-stats, promote-candidates). But per FR-09/NFR-07,
   CULL's "proposal" for shared-tier entries is a git review branch that
   deletes a file — an actual (review-gated) mutation capability — not a pure
   report. Bundling a low-risk read-only report with a higher-risk
   git-branch-opening eviction path in one bead would recreate the tangled-PR
   problem this fleet's own fold-vs-split precedent (crn-6az.6.1) warns
   against. crn-28ge.1.6 stays pure-read (recall-stats, promote-candidates);
   crn-28ge.1.7 owns cull-candidates plus the tier-conditional eviction logic.
2. **The unnumbered conflict-check consumer folds into the extraction bead,
   not a tenth bead.** The architect's text describes extracting a
   single-candidate dedup primitive (their item #5) and separately mentions
   "the recall-time conflict-check" as a second consumer, but never gives that
   consumer its own item number. An extracted function with zero callers is
   effectively dead code, so crn-28ge.1.3 owns both the extraction and its
   `cairn get` exposure — one bead, per NFR-05's own requirement that both
   consumers share a single primitive rather than drift independently.

| Bead | Title | Routing | Depends on |
|---|---|---|---|
| crn-28ge.1.1 | Schema: add `LastRecalledAt`/`RecurrenceCount`/`PromotedBeadID`/`Kind`/`AutoActionable` to Entry + index | ready-to-build → cairn/builder | — |
| crn-28ge.1.2 | `cairn review merge`: add `--kind`/`--auto-actionable` reviewer-patch flags | ready-to-build → cairn/builder | crn-28ge.1.1 |
| crn-28ge.1.3 | Extract single-candidate dedup/conflict primitive; expose `kind`/`auto_actionable`/conflicts via `cairn get` | ready-to-build → cairn/builder | crn-28ge.1.1 |
| crn-28ge.1.4 | `cairn remember`: capture-time recurrence detection | ready-to-build → cairn/builder | crn-28ge.1.1, crn-28ge.1.3 |
| crn-28ge.1.5 | `Find()`: stamp `LastRecalledAt` alongside `HitCount` on get/freshness/verify (FR-08) | ready-to-build → cairn/builder | crn-28ge.1.1 (+ external, see below) |
| crn-28ge.1.6 | New read-only reporting: `cairn recall-stats` + `cairn promote-candidates` | ready-to-build → cairn/builder | crn-28ge.1.1 |
| crn-28ge.1.7 | CULL sweep: `cairn cull-candidates` + eviction (direct for private, review-branch proposal for shared) | ready-to-build → cairn/builder | crn-28ge.1.1 |
| crn-28ge.1.8 | `mol-cairn-librarian`: add PROMOTE-candidate + CULL-candidate steps | ready-to-build → cairn/builder | crn-28ge.1.6, crn-28ge.1.7 |
| crn-28ge.1.9 | Test + benchmark pass: schema/recurrence/conflict/cull paths | needs-tests → cairn/validator | crn-28ge.1.1–.8 (all) |

crn-28ge.1.1 is the schema foundation everything else needs. crn-28ge.1.2–.7
are the six independent-ish capability beads (review-merge flags, the shared
dedup/conflict primitive, capture-time recurrence, recall-time
`LastRecalledAt`, the two reporting commands, and the CULL sweep) and can
mostly land in any order once the schema exists, modulo the explicit .3→.4
edge (NFR-05: recurrence detection must call the shared primitive, not
reimplement equality). crn-28ge.1.8 wires the librarian formula's two new
sweep steps on top of .6/.7's reporting output. crn-28ge.1.9 is a dedicated
adversarial test/benchmark pass over the whole family, not inline tests from
whoever builds each piece — same convention as crn-6az.6.1.6 and crn-419.5.

### External blocker on crn-28ge.1.5 (text-only, not a bd edge)

crn-28ge.1.5 extends the `UPDATE ... RETURNING hit_count` transaction that
crn-6az.6.1.3 added to `Find()`. crn-6az.6.1.3 itself is bd-closed, but as of
2026-07-23 (re-verified live via `gh pr view 41`) its containing PR
(quad341/cairn #41, branch `deploy/crn-2xpm-gate`) is still **OPEN,
`mergedAt: null`** — not on `origin/main`. A bd dependency edge on
crn-6az.6.1.3 would be inert (the bead is already closed, so the edge would
never gate anything), so the blocker is conveyed as explicit text in
crn-28ge.1.5's description instead, with instructions to re-verify current
merge status before starting rather than trusting the note. This mirrors the
architect's own stated risk mitigation for this exact collision.

## Deliberate out-of-scope calls (PM sizing, per the architect's delegation)

The design doc explicitly delegates two sizing questions to pm/builder. Both
are scoped **out** of this decomposition, not silently dropped:

- **Role-tier CULL.** The design doc flags this as an open question it
  doesn't solve, noting role-tier already has an established "light
  self-curation" policy (DESIGN.md §7) that this work doesn't change. No
  escalation bead is filed for this (unlike crn-419.6's precedent) because
  it's an already-resolved architect boundary, not an open ambiguity needing
  a decision.
- **The `pinned` opt-out tag.** Raised in the design doc's Risks table as a
  mitigation for "cull evicts a rarely-needed-but-critical shared entry."
  Deferred for v1: the existing per-instance reviewer-reject path on any
  shared-tier cull proposal (crn-28ge.1.7) already lets a reviewer keep a
  specific entry, which covers the immediate risk. Add `pinned` later as a
  follow-on if real cull proposals demonstrate the need.

## Dependency-graph fix on a sibling bead (crn-28ge.2)

crn-28ge.2 (pack-author, prompt-wiring for `cairn-usage.md.tmpl`) had a
pre-existing bd edge on crn-28ge.1 as a whole. Since crn-28ge.1 closes once
decomposition is complete — while the actual code crn-28ge.2 needs (the
`Kind`/`AutoActionable` review-merge flags) won't exist until crn-28ge.1.2 is
separately built and merged — that edge would become trivially satisfied the
moment this bead closes, well before the real dependency lands. An additive
edge `crn-28ge.2 → crn-28ge.1.2` has been added (alongside, not replacing,
the existing edge to crn-28ge.1) so crn-28ge.2 stays correctly gated.
Pack-author is notified of this addition directly.

## Acceptance criteria

See each child bead's `--acceptance` for the measurable, per-bead criteria.
Summary: all five new Entry fields round-trip through TOML and survive
`Reindex` via upsert, not blind overwrite; `AutoActionable` is reachable only
through the reviewer-granted `cairn review merge --auto-actionable` path and
never self-declared or reachable from a private-tier entry; the recurrence
and conflict checks share one primitive (NFR-05) rather than two
independently-drifting implementations; capture-time recurrence increments
existing entries through the correct tier-appropriate commit path with no
duplicate written; `LastRecalledAt` is stamped only by get/freshness/verify,
never by map/prime, matching `HitCount`'s existing scoping; FRESHNESS and
CULL remain independent signals (FR-10); shared-tier eviction is only ever a
review-branch proposal, never a direct delete (NFR-07, enforced with a
negative test, not just a positive one); the librarian's existing
read-only/proposal-only charter is explicitly re-verified as unchanged by the
two new sweep steps.
