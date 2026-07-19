# cairn — design

cairn is a scoped, freshness-tracked knowledge cache for a fleet of AI agents.
It lets higher-level agents (investigators, architects, designers, interactive
assistants) stop re-solving problems that were already solved and re-deriving
infrastructure that is already understood — while guaranteeing that a cached
answer is either known-fresh or flagged stale, never silently wrong.

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
project / role / agent rather than pooling everything into one store.

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

- **The index** is a **SQLite** database — a *rebuildable materialized view*
  over the frontmatter. It is disposable and gitignored; `reindex` drops and
  repopulates it from the bodies. It holds no state that isn't in a body.

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

`confidence = f(age-since-verified, anchor-drift)`. An anchored entry whose
source is untouched stays high-confidence for a long TTL; an un-anchored note
decays on time alone.

Loops:

- **Lazy verify on read** — a read cheaply re-checks the anchor; if it drifted,
  the entry is served ⚠-stale ("true as of X; re-derive"). Stale is never served
  as fresh.
- **Write-back on miss** — re-deriving an entry re-stamps its fingerprint, so the
  next reader gets it fresh for free.
- **Prioritized sweep** — a background pass re-verifies high-traffic +
  low-confidence entries first (most value per unit of work); the cold tail is
  re-verified lazily on next read.

## 5. Recall

- **Always in context: a bounded map** — a topic tree with counts, *not* the full
  index. It scales with topic count (sub-linear in entries), so it stays small at
  scale, and it makes the agent aware of what exists — which is what prevents
  "queried wrong, silently missed it."
- **Bodies on demand** — pulled by id (and, on the roadmap, by semantic search),
  so context isn't bloated by entries the task doesn't need.

The map is the reason a query rarely misses: you can see the menu.

## 6. Topic keys

An entry's canonical `topic_key` is the identity used for override/precedence.
Keys are assigned by the **curator at ingestion**, not by the writing agent — a
key's whole value is consistency, which needs a single naming authority.
Contributors may supply a freeform hint on write; the curator normalizes it when
an entry is promoted to a shared scope.

## 7. Curation — friction ∝ blast radius

| Scope | Flow |
|---|---|
| `agent/…` (private) | commit straight to `main` — no review |
| `role/…` | light — the role's own agents curate |
| `rig/…`, `global/…` | **owned**: propose on a branch, the layer's curator reviews the diff (sets anchor + tags + topic key), then merge |

Because bodies are just files in a git repo, the shared-scope review *is* a pull
request — no separate forge required. Cost matches damage: a bad private note
hurts one agent; a bad global note poisons everyone.

## 8. CLI (v0)

```
cairn reindex        # rebuild the SQLite index from the bodies
cairn map            # bounded topic map for an identity (--identity rig:web,role:reviewer)
cairn status         # freshness of every entry
cairn freshness <id> # freshness of one entry
cairn verify <id>    # recompute + write back an entry's anchor fingerprint
```

## 9. Roadmap

- Semantic pull (embeddings) for body retrieval.
- The prioritized drift sweep as a scheduled job.
- `query` / `external` anchor types.
- A thin `propose` / `review` wrapper over the git PR flow for shared scopes.
