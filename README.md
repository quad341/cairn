# cairn

<p align="center">
  <img src="docs/cairn.png" alt="a stone cairn on a misty ridge at dawn" width="420">
</p>

[![CI](https://github.com/quad341/cairn/actions/workflows/ci.yml/badge.svg)](https://github.com/quad341/cairn/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/quad341/cairn)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> A cairn is the stack of stones a traveler leaves to mark the trail — so whoever
> comes next doesn't lose the path or have to re-find it from scratch.

**cairn** is a scoped, freshness-tracked knowledge cache for AI agent fleets. It
lets high-level agents (investigators, architects, designers) and interactive
agents stop re-solving solved problems and re-deriving known infrastructure: each
agent sees the union of knowledge relevant to *it*, and supported source anchors
make drift visible.

This repo is the **engine** — CLI, the rebuildable SQLite index over markdown
bodies, the freshness/drift checker, the scope/union query, schemas, and agent
integration. The actual knowledge lives in a **separate store repo** (one
per fleet/deployment): cairn is generic; your notes are yours.

Design & architecture → [`docs/DESIGN.md`](docs/DESIGN.md).
Usage & the knowledge lifecycle (+ how it differs from MEMORY.md / `bd remember`) → [`docs/knowledge-lifecycle.md`](docs/knowledge-lifecycle.md).

## Concepts, one breath

- **Entry** = a markdown body (source of truth) + an index row (queryable metadata).
- **Scope** = tags on an entry; an agent sees it *iff every tag is satisfied by its
  identity*. Scope is relevance routing, not access control; direct by-ID lookup is
  intentionally unscoped. Conflict precedence = specificity.
- **Freshness** = source-anchor drift is checked on reads and by a shared-tier
  sweep. Unanchored and unsupported anchors are reported as `unknown`.
- **Recall** = a bounded topic **map** in session context + bodies pulled on demand
  by exact ID. Exact topic lookup and semantic search are not implemented yet.
- **Curation** = friction ∝ blast radius, via a **local review-branch pipeline**:
  private = direct commit; shared = branch → merge-request → librarian review → merge.

## Current capabilities

| Area | Shipped today | Not yet implemented |
|---|---|---|
| Recall | identity-scoped `prime`/`map`; unscoped `get <id>`; recall counters and timestamps | topic-to-ID lookup; semantic search; general JSON mode |
| Freshness | `files` and `commit` anchors; lazy checks; `verify`; shared-tier JSON sweep | time-decay confidence; `query`/`external` verification; prioritized scheduling |
| Capture | `remember`; optional topic hint; exact-topic recurrence detection; private direct commit | stdin/body-file capture; anchor flags on `remember` |
| Curation | review branches and `review`; stale-branch reporting; dedup, promotion, and cull candidate workflows | hosted pull requests; fully autonomous curation |
| Index | self-healing SQLite-backed reads plus operational recall/curation fields | no manual reindex is normally required, but `reindex` remains available for repair |

The CLI is still early and its text output is primarily human-oriented. See
[`docs/knowledge-lifecycle.md`](docs/knowledge-lifecycle.md) for the current
commands and workflows.

## Non-goal: security boundaries

Cairn assumes one trusted fleet and store. Scope tags, tier names, shadowing, and
review friction exist to improve relevance and limit the efficiency blast radius
of bad knowledge. They do **not** provide confidentiality, authorization, or
tenant isolation. `cairn get <id>` deliberately bypasses identity filtering.

## Store
Point cairn at a store laid out as `global/ · rig/<rig>/ · role/<role>/ ·
agent/<agent>/`. Reference layout lives in the sibling `cairn-store` repo.
