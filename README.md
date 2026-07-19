# cairn

<p align="center">
  <img src="docs/cairn.png" alt="a stone cairn on a misty ridge at dawn" width="420">
</p>

[![CI](https://github.com/quad341/cairn/actions/workflows/ci.yml/badge.svg)](https://github.com/quad341/cairn/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/quad341/cairn)](https://goreportcard.com/report/github.com/quad341/cairn)
[![Go Reference](https://pkg.go.dev/badge/github.com/quad341/cairn.svg)](https://pkg.go.dev/github.com/quad341/cairn)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> A cairn is the stack of stones a traveler leaves to mark the trail — so whoever
> comes next doesn't lose the path or have to re-find it from scratch.

**cairn** is a scoped, freshness-tracked knowledge cache for AI agent fleets. It
lets high-level agents (investigators, architects, designers) and interactive
agents stop re-solving solved problems and re-deriving known infrastructure: each
agent sees the union of knowledge relevant to *it*, and every entry knows when
it's gone stale.

This repo is the **engine** — CLI, the rebuildable SQLite index over markdown
bodies, the freshness/drift checker, the scope/union query, schemas, and agent
integration. The actual knowledge lives in a **separate private store repo** (one
per fleet/deployment): cairn is generic; your notes are yours.

Design & architecture → [`docs/DESIGN.md`](docs/DESIGN.md).

## Concepts, one breath
- **Entry** = a markdown body (source of truth) + an index row (queryable metadata).
- **Scope** = tags on an entry; an agent sees it *iff every tag is satisfied by its
  identity*. Union = one query; conflict precedence = specificity.
- **Freshness** = `confidence = f(age-since-verified, source-anchor-drift)`. Reads
  lazily re-verify; a drift sweep re-checks high-traffic + low-confidence first.
- **Recall** = a bounded topic **map** always in context + bodies pulled on demand
  by semantic search. (You can't miss what you can see on the map.)
- **Curation** = friction ∝ blast radius, via a **local, forge-free PR pipeline**:
  private = direct commit; shared = branch → merge-request → librarian review → merge.

## Status
Early. `docs/DESIGN.md` has the ratified spine, the open questions, and the v1
plan — prove source-anchored freshness on one real entry before building the lattice.

## Store
Point cairn at a private store laid out as `global/ · rig/<rig>/ · role/<role>/ ·
agent/<agent>/`. Reference layout lives in the sibling `cairn-store` repo (private).
