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

- **Every new entry declares a content `type`.** `knowledge` is an
  independently true fact, mechanism, interface, location, or observation;
  `remediation` is conditional, independently testable recovery knowledge.
  `policy` names directives, preferences, permissions, and behavioral rules,
  but is refused at write time: policy belongs in the agent prompt, where its
  enforcement cannot depend on retrieval. Legacy entries without a type remain
  readable and queryable as `unclassified`. This top-level `Entry.Type` is
  distinct from `Entry.Anchor.Type`, which describes freshness evidence. This
  keeps OKF's minimum-viable-schema principle while making its one classification
  field an enforced contract rather than a producer convention.

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
After specificity, newer verification and creation timestamps break ties.
If every meaningful signal ties, cairn returns all top revisions explicitly
marked `indistinguishable`; entry IDs order the report but never select truth.

Scope is stored as a tags relation from the start, but most entries carry 0–1
tags in practice; cross-cutting `{rig, role}` entries are supported without a
schema change.

## 4. Freshness

Cairn detects changes to declared evidence and prevents affected knowledge from being presented as verified without re-investigation.

That is a claim about evidence *change detection*, not about correctness — an
unchanged anchor proves neither that the derived conclusion was right, nor
that the anchor selection itself was right.

Every entry may carry a source **anchor** — what it was derived from — so drift
is *mechanically detectable*:

- `files` — a repo + path globs; fingerprint = the git object hashes of those
  paths at `origin/main`, falling back to `HEAD` only when `origin/main` does
  not resolve (no origin remote, or the path is not tracked there) — so "the
  source changed" means a *commit* touched it, not a work-in-progress edit.
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

- **Always in context: two independent views** — an **index view** (total visible
  count plus a per-`topic_key` breakdown with counts) whose cost is independent of
  entry content size, so it stays cheap and complete no matter how large the store
  gets; and a **payload view** (`Items`) — a ranked, budget-bounded itemization
  that may be partial. The index view is what makes the agent aware of what
  exists even when the payload had to truncate, preventing "queried wrong,
  silently missed it"; the payload view is what actually pulls entry IDs, hit
  counts, and freshness into context.
- **Bodies on demand** — pulled by exact id, so context isn't bloated by entries
  the task doesn't need.

The payload view's items are ranked by hit count, then recency, with a small
exploration band that gives brand-new entries a cycle of visibility before
they've earned any hits — and truncated to a byte budget, stopping at the first
entry that doesn't fit rather than skipping ahead to a cheaper, lower-priority
one. Exact topic-to-entry lookup ships as `cairn list <topic>`; semantic search
remains roadmap work.

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

**A shared-tier `remember` leaves the working-tree copy untracked — on
purpose.** The entry file is written straight into the store's live working
tree first (so local reads work immediately), then committed separately onto
its own `remember/<id>` branch via a throwaway git worktree. That commit is
the real durability mechanism; the original file is deliberately never
`git add`ed or removed on the checked-out branch. So `git status` on the
store continues to show the new entry as `??` even after it's durably
committed — that's expected, not data loss. `git status` alone will not show the commit;
confirm it with `git branch -a`, `git log --all`, or
`git branch --list 'remember/*'`.

## 8. CLI (v0)

```
cairn prime                   # scoped topic map + agent usage prompt
cairn map                     # scoped topic map only
cairn get <id>                # unscoped exact-ID body + freshness lookup
cairn remember --type knowledge <body> # private commit or shared review-branch proposal
cairn remember --batch-file <path>  # JSONL manifest: many entries in one call
cairn entries --type <value>  # machine-readable maintenance query by content type
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
compact model-facing JSON defaults for discovery reads (`prime`, `search`, and
`list`) with `--pretty` for human inspection, context-budgeted `prime` output
carrying IDs and freshness, stdin/`--file` capture and anchor flags on
`remember`, and store validation plus precedence explanation (`doctor`,
`doctor explain`). `get` deliberately keeps raw Markdown as its default because
the body is the product.

Still open:

- Semantic pull, once exact topic retrieval proves insufficient.
- Prioritized scheduling around the existing drift sweep.
- `query` / `external` anchor types.
- Inferring an anchor from an entry's body rather than being told one.
- Time-decay freshness confidence.
