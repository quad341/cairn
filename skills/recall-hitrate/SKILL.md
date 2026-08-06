---
name: recall-hitrate
description: Measure cairn's recall hit-rate at point-of-need — of the entries an agent actually looked up (cairn get, hit or stale), what fraction had already been surfaced to that agent by a prior cairn prime call? Use when asked whether prime is actually working, whether an entry that's clearly "in the store" is getting surfaced, or to distinguish a recall problem (surfaced fine, agent didn't use it) from a prime problem (never surfaced at all). Distinct from store size or hit/miss alone — an entry can be a hit and still never have been shown to the agent that fetched it.
---

# /recall-hitrate — was it surfaced, or just present?

A `cairn get` hit only says the entry existed in the store. It says nothing about
whether `cairn prime` had already put that entry in front of the agent. This skill
joins the two obslog record kinds cairn already writes to `debug.jsonl` — `prime_emit`
(what a `prime` call surfaced) and `retrieval_outcome` (what a later `get` call found)
— and classifies each hit/stale retrieval as **surfaced** (prime showed it first),
**recall-miss** (in the store, successfully fetched, but prime never surfaced it — the
gap crn-894i asks about), or **unjoined** (no preceding prime call for that identity at
all, so there's nothing to judge prime against).

## Invocation

```bash
python3 {SKILL_DIR}/recall_hitrate.py                    # human-readable
python3 {SKILL_DIR}/recall_hitrate.py --json              # machine-readable
python3 {SKILL_DIR}/recall_hitrate.py --homes ~/a-claude ~/b-claude
RECALL_HITRATE_HOMES=~/a-claude:~/b-claude python3 {SKILL_DIR}/recall_hitrate.py
```

Precedence is `--homes` > `$RECALL_HITRATE_HOMES` > discovery. Account HOMEs are
auto-discovered the same way burn-report's are: `$HOME`, its parent, and their
`*-claude` siblings, filtered to ones holding a `.local/state/cairn` directory.

Log source per home is `$XDG_STATE_HOME/cairn/debug.jsonl`, falling back to
`$HOME/.local/state/cairn/debug.jsonl` — the same resolution
`internal/obslog/path.go`'s `LogPath()` uses. `$XDG_STATE_HOME` is a single
environment variable for this process, so it can only be applied to the home
matching this process's own `$HOME`; other scanned homes always use the fallback
path, since their own override (if any) isn't observable from outside that process.

## Reading it

- **recall hit-rate** = `surfaced / (surfaced + recall_miss)`. Low means entries are
  reaching the store and being fetched successfully, but prime isn't putting them in
  front of the agent beforehand — a prime ranking/budget problem, not a store problem.
- **join coverage** = `(surfaced + recall_miss) / (surfaced + recall_miss + unjoined)`.
  Low coverage means most hit/stale retrievals have no preceding prime call to judge
  against at all (e.g. the agent called `get` directly without ever calling `prime` for
  that identity) — a low hit-rate alongside low coverage is a data problem, not
  necessarily a prime problem; a low hit-rate with *high* coverage is the real signal.
- **miss_excluded** is retrievals where `outcome == "miss"` — the entry wasn't in the
  store at all, a different failure mode than "in the store but not surfaced," so it's
  reported separately and left out of both rates above.
- The join is against the **nearest**-preceding `prime_emit` for the same
  `identity_tags`, not just any preceding one: an identity can be primed multiple times,
  and only the most recent prime call before a given retrieval is the one that actually
  had a chance to surface it.

## Caveats

- Coverage is only as wide as the HOMEs found, same caveat as burn-report: the `sources
  (...)` line names every home scanned and what each gave (`--json` puts it on stderr,
  so stdout stays pure JSON); a home that's absent altogether cannot warn you, but a home
  you named explicitly *does* warn when it yields no records.
- `identity_tags` are compared as an unordered set (sorted before comparison), since
  there's no ordering guarantee on how a caller assembles them.
- Debug logs rotate (`internal/obslog/writer.go`, kept 3 generations); this reads only
  the active `debug.jsonl`, not `debug.jsonl.1`/`.2`/`.3`, so the window is bounded by
  what hasn't rotated out yet.

## Changing it

`./test-recall-hitrate.sh` covers the contracts worth not breaking: nearest-preceding
(not just any-preceding) join semantics, all four classification buckets, hit-rate/
join-coverage arithmetic, the JSON key set, `--homes`/env/discovery precedence,
`$XDG_STATE_HOME` override, stdout staying pure JSON while warnings go to stderr, and
the guard against counting an aliased home twice. It builds fixture homes in a tmpdir,
so it's fast and passes on any machine.
