# cairn — design

cairn is a scoped, freshness-tracked knowledge cache for a fleet of AI agents.
It lets higher-level agents (investigators, architects, designers, interactive
assistants) stop re-solving problems that were already solved and re-deriving
infrastructure that is already understood. Anchored answers report source drift;
unanchored or unverifiable answers report unknown freshness.

## 1. Motivation

Agents repeatedly re-read a codebase to re-derive its architecture, or re-derive
a recurring operational fix. Ad-hoc memory (a big prose file, or scattered
notes) has three problems this design targets:

- **Lossy recall** — you can't tell what *didn't* surface.
- **Unmanaged staleness** — a note that references code that has since changed
  silently misleads. A stale summary is worse than none.
- **All-or-nothing sharing** — no clean way to scope a fact to "everyone,"
  "everyone on this project," "this role," or "just me."

Because a real fleet has *differentiated* agents, cairn scopes knowledge per
project / role / agent rather than putting every fact into every agent's working
context.

Scope is a relevance and efficiency mechanism only. Cairn assumes one trusted
fleet and store; it does not provide confidentiality, authorization, or tenant
isolation. Direct by-ID lookup deliberately bypasses identity filtering.

## 2. Storage

- **Bodies** are markdown files with **TOML frontmatter** (`+++` fences),
  laid out on disk by scope:

  ```
  global/            # every agent
  rig/<rig>/         # every agent on that project     (e.g. rig/web/)
  role/<role>/       # that role across projects        (e.g. role/reviewer/)
  agent/<agent>/     # one agent, private
  ```

  Bodies are the **source of truth**: human-readable, git-versioned, diffable,
  reviewable like code.

- **The index** is a gitignored **SQLite** database. Entry content and curated
  metadata come from the bodies; the index also carries operational state such
  as recall counts/timestamps and curation signals. Read paths self-heal the
  index from the store when its Git watermark is stale. `reindex` remains an
  explicit repair tool and preserves index-only operational fields for entries
  that still exist.

## 3. Scope & the union

Scope is a **set of tags** on each entry. The visibility rule:

> An agent sees an entry **iff every scope-tag on the entry is satisfied by the
> agent's identity.**

An identity is a set of tags, e.g. `{rig:web, role:investigator, id:inv-3}`.

| Entry tags | Visible to |
|---|---|
| `{}` (global) | everyone |
| `{rig:web}` | any agent on `web`, any role |
| `{role:investigator}` | investigators on any project |
| `{rig:web, role:investigator}` | only `web` investigators (a cross-cut) |
| `{rig:api}` | not a `web` agent |

The union is a single identity-parameterized query. On conflict for the same
`topic_key`, **precedence = specificity** (more tags wins) — a `{rig, role}`
entry shadows a `{rig}` entry shadows a global one, CSS-style.

Scope is stored as a tags relation from the start, but most entries carry 0–1
tags in practice; cross-cutting `{rig, role}` entries are supported without a
schema change.

## 4. Freshness

Every entry may carry a source **anchor** — what it was derived from — so drift
is *mechanically detectable*:

- `files` — a repo + path globs; fingerprint = the git object hashes of those
  paths at `HEAD` (so "the source changed" means a *commit* touched it, not a
  work-in-progress edit).
- `commit` — pinned to a specific commit.
- `query` / `external` — re-run, or TTL (roadmap).

The current implementation reports three states: `fresh`, `stale`, and
`unknown`. It compares supported anchors to the stored fingerprint. Time-based
confidence decay is not implemented.

Loops:

- **Lazy verify on read** — a read cheaply re-checks the anchor; if it drifted,
  the entry is served ⚠-stale ("true as of X; re-derive"). Stale is never served
  as fresh.
- **Explicit write-back** — `cairn verify <id>` re-stamps a supported anchor
  after an agent has re-derived and checked the entry.
- **Shared-tier sweep** — `cairn sweep` emits JSON freshness findings for
  librarian maintenance. Scheduling and prioritization live outside this CLI.

## 5. Recall

- **Always in context: a bounded map** — a topic tree with counts, *not* the full
  index. It scales with topic count (sub-linear in entries), so it stays small at
  scale, and it makes the agent aware of what exists — which is what prevents
  "queried wrong, silently missed it."
- **Bodies on demand** — pulled by exact id, so context isn't bloated by entries
  the task doesn't need.

The map exposes the menu *and* the entry IDs, hit counts and freshness needed to
pull a body, within a byte budget. Exact topic-to-entry lookup ships as
`cairn list <topic>`; semantic search remains roadmap work.

## 6. Topic keys

An entry's canonical `topic_key` is the identity used for override/precedence.
Keys are assigned by the **curator at ingestion**, not by the writing agent — a
key's whole value is consistency, which needs a single naming authority.
Contributors may supply a freeform hint on write; the curator normalizes it when
an entry is promoted to a shared scope.

## 7. Curation — friction ∝ blast radius

| Scope | Flow |
|---|---|
| `agent/…` (private/narrowest relevance) | commit straight to `main` — no review |
| `role/…` | light — the role's own agents curate |
| `rig/…`, `global/…` | **owned**: propose on a branch, the layer's curator reviews the diff (sets anchor + tags + topic key), then merge |

Because bodies are just files in a git repo, shared-scope review uses a local
review branch; no hosted forge is required. Cost matches efficiency impact: a
bad narrowly scoped note wastes one agent's effort, while a bad global note can
mislead the whole fleet.

## 8. CLI (v0)

```
cairn prime                   # scoped topic map + agent usage prompt
cairn map                     # scoped topic map only
cairn get <id>                # unscoped exact-ID body + freshness lookup
cairn remember <body>         # private commit or shared review-branch proposal
cairn freshness <id>          # freshness of one entry
cairn status                  # freshness of every entry
cairn verify <id>             # recompute and write a supported fingerprint
cairn sweep                   # shared-tier freshness report (JSON)
cairn review ...              # inspect and merge shared review branches
cairn stale-branches          # review-branch age/reporting workflow (JSON)
cairn dedup                   # duplicate/re-scope candidates (JSON)
cairn promote-candidates      # recurring promotion candidates (JSON)
cairn promote-mark <id>       # record promotion to a bead
cairn recall-stats            # recall telemetry (JSON)
cairn cull-candidates         # disused-entry candidates (JSON)
cairn cull-evict <id>         # private eviction or shared review proposal
cairn reindex                 # explicit index rebuild/repair
```

## 9. Roadmap

Shipped since this list was written: exact topic-to-entry retrieval (`list`),
`--json` on every command, context-budgeted `prime` output carrying IDs and
freshness, stdin/`--file` capture and anchor flags on `remember`, and store
validation plus precedence explanation (`doctor`, `doctor explain`).

Still open:

- Semantic pull, once exact topic retrieval proves insufficient.
- Prioritized scheduling around the existing drift sweep.
- `query` / `external` anchor types.
- Inferring an anchor from an entry's body rather than being told one.
- Time-decay freshness confidence.
