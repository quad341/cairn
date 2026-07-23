# cairn formulas (version-controlled source)

bd molecule formulas that define cairn's recurring dogfood / maintenance loops.

## Why this directory exists

Gas City's rig setup local-excludes `.beads/formulas/` (see `.git/info/exclude`),
treating it as per-instance runtime state. But cairn's *own* formulas are product
artifacts that need version control and review. So the source of truth lives here
in the tracked `formulas/` directory, and `make formulas` symlinks each one into
the runtime `.beads/formulas/` dir that bd actually reads — populating every
worktree without fighting the GC exclude (no `git add -f`, nothing to clobber on
rig re-setup). bd reads formulas transparently through the symlinks.

## The formulas

- **`mol-cairn-critic`** — *engine* dogfood. Each iteration mechanically
  stress-tests cairn on one of 5 dimensions (recall, scope-precedence, freshness,
  perf, ergonomics) via the landed `internal/critic` scenarios, files a
  taxonomy-shaped bug bead on any fail/degraded verdict, and — via the FR-7
  landing-check — refuses to close a critic bead until its fix is verified on
  origin/main. Self-perpetuating.

- **`mol-cairn-librarian`** — *content* maintenance. A recurring, tier-parameterized
  (global/rig) sweep over shared-tier cairn entries: recovers stale review
  branches, re-verifies freshness and files drift beads, flags dedup/re-scope
  candidates. Strictly read-only against cairn and proposal-only against bd — it
  mails a reviewer or files a bead, never merges, deletes, or rewrites a curated
  entry.

## Usage

```
make formulas          # link formulas/ -> .beads/formulas/ (also run by `make` and `make install`)
bd mol pour mol-cairn-critic     # start an engine-dogfood iteration
bd mol pour mol-cairn-librarian --var tier=global   # start a global content sweep
```
