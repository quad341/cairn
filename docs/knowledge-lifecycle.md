# cairn — usage & the knowledge lifecycle

This is the practical companion to [`DESIGN.md`](DESIGN.md): what cairn is *for*,
how an agent actually uses it, and the full life of a knowledge entry — from the
moment an agent decides to remember something to the moment it's recalled,
reviewed, curated, and re-verified for freshness.

## What cairn is (and why it exists)

cairn is a **scoped, freshness-tracked knowledge cache for a fleet of AI agents.**
It lets agents stop re-solving problems that were already solved and re-deriving
infrastructure that's already understood — while **guaranteeing a cached answer
is either known-fresh or flagged stale, never silently wrong.**

It targets three failure modes of ad-hoc memory (a big prose file, scattered notes):

- **Lossy recall** — you can't tell what *didn't* surface. cairn keeps an
  always-in-context *map* so an agent can see the menu of what exists.
- **Unmanaged staleness** — a note referencing code that has since changed
  silently misleads. cairn *anchors* entries to their source so drift is
  mechanically detectable; a stale entry is served ⚠, never as fresh.
- **All-or-nothing sharing** — cairn *scopes* each fact to everyone / a project /
  a role / a single agent, so a fact lands exactly where it's useful.

## How cairn relates to MEMORY.md and `bd remember`

Three memory systems coexist in this ecosystem; they solve different problems.
Use the smallest one that fits.

| | **MEMORY.md** (auto-memory) | **`bd remember`** | **cairn** |
|---|---|---|---|
| **Whose memory** | one agent's own | one rig / project | the whole fleet, *scoped* |
| **Scope model** | single agent, private | flat, rig-local | tags: global / rig / role / agent, with **precedence by specificity** |
| **Sharing** | none (personal) | shared within a rig's beads DB | cross-agent + cross-project (global tier) |
| **Freshness** | none | none | **anchored** — drift-detected, stale-flagged, re-verified |
| **Curation / review** | you edit your own files | key-value, overwrite in place | **shared tiers reviewed like a PR**; a librarian dedups/re-scopes |
| **Recall** | whole index loaded each session | `bd memories <kw>` / `bd prime` | bounded **scoped map** + bodies-on-demand by id |
| **Storage** | markdown files + an index line | rows in the Dolt beads DB | markdown+TOML bodies on disk (git), SQLite index (disposable) |
| **Reach for it when…** | *your own* working notes, prefs, corrections | task/project knowledge tied to the **tracker** | knowledge worth **sharing across agents** that must **not go silently stale** |

Rule of thumb: **MEMORY.md** is your private notebook, **`bd remember`** is the
project's shared scratchpad tied to its issue tracker, and **cairn** is the
fleet's curated, freshness-guaranteed library — the one place a fact can be
scoped to exactly the right audience and trusted not to be quietly out of date.

## The lifecycle of an entry

```
  SUBMIT ─▶ SURFACE ─▶ RECALL ─▶ REVIEW ─▶ CURATE ─▶ FRESHNESS ─┐
    ▲                                                            │
    └────────────────────── (re-derive / re-verify) ◀────────────┘
```

### 1. Submit — `cairn remember`

```
cairn remember "<body>" --scope rig:web,role:reviewer --topic build/oom-caps
```

An agent writes a knowledge entry. The body is **markdown with TOML frontmatter**
(`+++` fences) and is laid out on disk *by scope*:

```
global/            # every agent
rig/<rig>/         # every agent on that project
role/<role>/       # that role, across projects
agent/<agent>/     # one agent, private
```

- `--scope` sets the entry's tier tags. **Default is private** (the
  `agent:<id>` tag from the resolved identity) — cheap and safe.
- `--topic` is a *hint* for the canonical topic key; a curator normalizes it when
  the entry is promoted to a shared scope (consistency needs one naming authority).
- Bodies are the **source of truth** — human-readable, git-versioned, diffable,
  reviewable like code. The SQLite index is a rebuildable view, never the truth.

### 2. Surface — `cairn prime` / `cairn map`

```
cairn prime            # emit the scoped map + usage (wire into a SessionStart hook)
cairn map              # just the bounded topic map for this identity
```

Each agent gets a **bounded, identity-scoped topic map** injected at session
start — a topic tree with counts, *not* the full index. It scales with topic
count (sub-linear in entries), so it stays small at fleet scale, and it makes the
agent **aware of what exists**. This is what prevents "queried wrong, silently
missed it" — you can see the menu before you ask.

Identity is a set of tags (`--identity rig:web,role:reviewer`, or `$CAIRN_IDENTITY`).
An agent sees an entry **iff every scope-tag on the entry is satisfied by its
identity.**

### 3. Recall — `cairn get`

```
cairn get <id>         # full body + freshness, direct by-id (bypasses scope)
```

The map shows what exists; `get` pulls the full body of a specific entry on
demand (plus its freshness verdict), so context isn't bloated by entries the task
doesn't need. On a conflict for the same `topic_key`, **precedence = specificity**
— a `{rig, role}` entry shadows a `{rig}` entry shadows a global one, CSS-style.

### 4. Review — shared-tier gate (friction ∝ blast radius)

Curation cost scales with blast radius, because a bad global note poisons everyone
while a bad private note hurts one agent:

| Scope | Flow |
|---|---|
| `agent/…` (private) | commit straight to `main` — no review |
| `role/…` | light — the role's own agents curate |
| `rig/…`, `global/…` | **owned**: propose on a branch → the layer's curator reviews the diff (sets anchor + tags + topic key) → merge |

Because bodies are just files in a git repo, **the shared-scope review *is* a pull
request** — no separate forge. (The `cairn review` verb wraps the list/show/merge
of these review branches.)

### 5. Curate — the librarian

A recurring, tier-parameterized **librarian** sweep keeps the shared corpus
healthy: it recovers stale review branches, re-verifies freshness and files drift
beads, and flags **dedup / re-scope** candidates. Today it is **proposal-only** —
it mails a reviewer or files a bead, never merges/deletes/rewrites a curated entry.
(The design intent is a path to autonomy: proposal → agent-reviewed → autonomous
with the critic loop verifying that a curation didn't break recall/scope/freshness.)

### 6. Freshness — anchors, lazy verify, write-back

```
cairn freshness <id>   # freshness of one entry
cairn status           # freshness of every entry
cairn verify <id>      # recompute + write back an entry's anchor fingerprint
```

Every entry may carry a source **anchor** — what it was derived from — so drift is
mechanically detectable:

- **`files`** — repo + path globs; fingerprint = the git object hashes at `HEAD`
  (so "source changed" means a *commit* touched it, not a WIP edit).
- **`commit`** — pinned to a specific commit.
- **`query` / `external`** — re-run or TTL *(roadmap)*.

`confidence = f(age-since-verified, anchor-drift)`. An anchored entry whose source
is untouched stays high-confidence for a long TTL; an un-anchored note decays on
time alone. Three loops keep it honest:

- **Lazy verify on read** — a read cheaply re-checks the anchor; if it drifted, the
  entry is served **⚠-stale** ("true as of X; re-derive"). Stale is never served as fresh.
- **Write-back on miss** — re-deriving an entry re-stamps its fingerprint, so the
  next reader gets it fresh for free.
- **Prioritized sweep** — a background pass re-verifies high-traffic, low-confidence
  entries first; the cold tail is re-verified lazily on next read.

## Storage & index, in one line

**Bodies** (markdown+TOML, on disk by scope) are the git-versioned source of truth;
the **SQLite index** is a disposable materialized view rebuilt from them with
`cairn reindex`. It holds no state that isn't in a body.

## Keeping cairn honest — the dogfood loops

Two recurring loops dogfood cairn itself (see [`../formulas/README.md`](../formulas/README.md)):

- **critic** (`mol-cairn-critic`) — mechanically stress-tests the *engine* on 5
  dimensions (recall, scope-precedence, freshness, perf, ergonomics), files a bug
  bead on any failure, and won't close it until the fix is verified on `main`.
- **librarian** (`mol-cairn-librarian`) — maintains the *content* (dedup, re-scope,
  freshness) as described in step 5.

## CLI reference

| Verb | Purpose |
|---|---|
| `remember <body>` | write a new entry (curation-tier routing via `--scope`) |
| `prime` | emit the scoped map + usage (for a SessionStart hook) |
| `map` | the bounded topic map for an identity |
| `get <id>` | pull an entry's full body + freshness (bypasses scope) |
| `freshness <id>` / `status` | freshness of one / of every entry |
| `verify <id>` | recompute + write back an entry's anchor fingerprint |
| `reindex` | rebuild the SQLite index from the bodies |

Global flags: `--identity` (recall scope, or `$CAIRN_IDENTITY`), `--store` (store
repo path, or `$CAIRN_STORE`).
