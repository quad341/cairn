# Plan: index-backed read paths for cairn (map/prime/get/status/freshness/verify)

Root bead: crn-6az.6.1 (source: crn-6az.6 dogfood finding + crn-6az architecture analysis)

## Why

`internal/cairn/index.go` already has `entries` and `entry_tags` SQLite tables,
populated by `Reindex` — but no read path ever queries them back (`openDB` has
exactly one caller). Every read command (`get`, `map`, `prime`, `status`,
`freshness`, `verify`) still walks the store directory and parses every entry's
TOML frontmatter on every invocation, via `IterEntries` plus a linear scan.

The crn-6az.6 dogfood finding measured this directly: latency grows with total
store size even for commands whose *output* only depends on a single entry or a
small visible subset. The architecture analysis (crn-6az.6 notes) confirms the
index schema already has what's needed for point lookups and topic-count
queries — the gap is purely in the read paths, plus a few schema fields
(`created_at`, anchor columns, a staleness watermark) needed to fully retire
the body-parse fallback.

## Scope split

The architecture sketch enumerated the read-path work as roughly seven items,
including keeping `hit_count` semantics correct (must survive `Reindex`,
must only increment on point-lookup commands, never on topic-count commands).
Rather than carry `hit_count` as an isolated seventh bead, this decomposition
folds it into the two beads that actually touch the code paths responsible for
it — the schema/Reindex bead (must not regress it on rebuild) and the `Find`
rewrite bead (owns the only increment call site). Splitting it out separately
would mean two beads editing the same function bodies with no clean seam
between them — the tangled-multi-bead-PR pattern this fleet is already
untangling elsewhere (crn-811). Sequencing/dependency ordering was pm's call
per the architect's own hand-off note.

| Bead | Title | Routing | Depends on |
|---|---|---|---|
| crn-6az.6.1.1 | Schema migration: anchor/created_at columns + index_meta watermark table, harden Reindex upsert (incl. hit_count-survives-reindex) | ready-to-build → cairn/builder | — |
| crn-6az.6.1.2 | Shared staleness self-heal helper (watermark vs. git HEAD, synchronous reindex on mismatch) | ready-to-build → cairn/builder | crn-6az.6.1.1 |
| crn-6az.6.1.3 | Index-backed `Find(store, id)` — drives get/freshness/verify, owns hit_count increment | ready-to-build → cairn/builder | crn-6az.6.1.2 |
| crn-6az.6.1.4 | Index-backed `Visible`/`Prime` — drives map/prime, zero body reads, no hit_count side effect | ready-to-build → cairn/builder | crn-6az.6.1.2 |
| crn-6az.6.1.5 | Index-backed `status` — skip redundant full-body decode, keep existing git-anchor fingerprint checks | ready-to-build → cairn/builder | crn-6az.6.1.2 |
| crn-6az.6.1.6 | Test + benchmark pass (hit_count/reindex, anchor_paths round-trip, Find/Visible boundary, 501/2001/5001 latency) | needs-tests → cairn/validator | crn-6az.6.1.3, crn-6az.6.1.4, crn-6az.6.1.5 |

crn-6az.6.1.1 is the schema foundation everything else needs. crn-6az.6.1.2 is
a single shared staleness-check helper so `Find`/`Visible`/`status` don't each
reinvent watermark comparison. crn-6az.6.1.3–.5 are the three independent read
paths and can land in any order once the helper exists. crn-6az.6.1.6 is a
dedicated adversarial test/benchmark pass over all five implementation beads,
not just inline tests from whoever builds them — same convention as crn-419.5.

## Acceptance criteria

See each child bead's `--acceptance` for the measurable, per-bead criteria.
Summary: `map`/`prime`/`status` output stays byte-identical to today's on the
same store; `get`/`freshness`/`verify` still return today's not-found behavior
on a miss; `hit_count` increments exactly once per point-lookup hit and never
on topic-count traffic, and survives a `Reindex` unchanged; a store edited
outside cairn's own write path is reflected on the very next read with no
manual `cairn reindex`; and the 501/2001/5001-entry benchmark empirically
confirms the O(1)/O(topic count) claims rather than assuming them.
