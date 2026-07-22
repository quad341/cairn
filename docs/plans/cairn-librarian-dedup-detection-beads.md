# Plan: dedup/re-scope candidate bead filing for mol-cairn-librarian

Root bead: crn-0yv.3 (source: crn-xw3 AF3 steps 6-7 and guardrail 8)

## Why

`internal/cairn/dedup.go` (this same bead, crn-0yv.3) already implements the
read-only detection half: `Dedup()` walks every shared-tier entry
(`global/`, `rig/*/`, `role/*/` — `agent/` private entries are out of the
librarian's remit, matching crn-0yv.2's `Sweep()`) and reports two kinds of
duplicate/re-scope candidate — an exact `topic_key` collision within a
single tier, and a Title+Summary word-Jaccard content-similarity pair above
a fixed threshold. That satisfies acceptance criteria 1, 2, 4, and 5.

What's left is criterion 3: turn a `Dedup()` finding into exactly one open
bd bead, not a fresh duplicate every time the sweep cycle runs. Per crn-xw3
guardrail 8, this is strictly a proposal — the step must never merge,
delete, or rewrite a cairn entry itself, only file a bead for a human or
reviewer to act on.

## What Dedup() already gives this step

`cairn dedup --store "$CAIRN_STORE"` prints a JSON array of `DedupFinding`
objects (`internal/cairn/dedup.go`, `cmd/dedup.go`):

```json
{"kind":"topic_key","tier":"rig","topic_key":"dup-key","entry_ids":["rig/one","rig/two"],"detail":"..."}
{"kind":"content","tier":"","entry_ids":["g/hook-a","g/hook-b"],"similarity":0.53,"detail":"..."}
```

`kind` is one of `"topic_key"` or `"content"`. `entry_ids` is always sorted
(`sort.Strings` in Go) and stable across cycles as long as the underlying
entry IDs and tier membership don't change — that stability is what makes
the anchor-based dedup check below work at all. `tier` is set for a
`topic_key` finding (always, since that kind is computed per-tier) and for
a `content` finding only when both entries happen to share a tier;
`entry_ids` and `kind` are the only fields every finding is guaranteed to
have populated.

## Design decision: a bracket-delimited anchor token, not a natural-language phrase

crn-0yv.2's freshness-drift step anchors its dedup check on a natural-
language substring, `"on $ID ("`, checked case by case against real
examples to confirm no id could be a prefix of another in a way that
produces a false match. That approach fits a finding shaped around a
*single* id. A dedup finding here is shaped around a *set* of ids — always
exactly 2 for `content`, but 2 or more for `topic_key` (a key held by three
entries produces one finding covering all three, not three pairwise ones) —
so this step needs an anchor form that extends uniformly to a variable-size
set without inventing a different phrasing per group size.

This plan uses a machine-readable bracketed token instead:

- `content`: `[pair:$ID_LO|$ID_HI]` (the two ids, already sorted low/high
  by the Go code)
- `topic_key`: `[ids:$ID_1,$ID_2,...]` (all ids in the group, comma-joined,
  already sorted by the Go code)

This is safer than a natural-language anchor, not just simpler: the leading
`[` character cannot appear inside a cairn entry id (ids are namespaced
slug paths, e.g. `architect/foo`), and it only ever appears once per anchor
token. For a different finding's anchor to accidentally substring-match
this one, the needle's leading `[` would have to align with the haystack's
own `[`, at which point the fixed prefix (`pair:` vs `ids:`) and the `|`/`,`
separators (also not legal id characters) pin down every subsequent
character. There is exactly one place in a title where this exact
sequence of literal delimiters and ids can occur. This was still verified
against a concrete adversarial case, not just argued abstractly — see
"Smoke-tested end-to-end" below, which deliberately includes a
`content` pair (`rig/one`, `rig/onextra`) chosen to share a raw substring
(`rig/one`) with an unrelated `topic_key` group's anchor, to confirm the
two do not cross-match.

`bd list --title-contains` is documented as case-insensitive. That has no
effect on this safety argument: none of `[`, `]`, `|`, `,`, or `:` have a
case, and cairn ids observed in this codebase are already lowercase.

## Design decision: topic_key group membership is part of the anchor's identity

Because the `topic_key` anchor embeds the *entire* current group
(`[ids:...]`, not just the key or tier), a group that grows between sweep
cycles — a third entry independently picks an already-colliding key — gets
a **new** anchor and therefore a **new** bead, alongside whatever bead
already covers the original pair. This is a deliberate choice, not an
oversight:

- `bd list --title-contains` can only do a literal-substring check, not a
  set-superset/subset comparison, so there is no way to ask bd "is there
  already a bead whose id-set is a subset of this one" without pulling
  every open `dim:dedup` bead's title back and parsing it client-side —
  meaningfully more machinery than this step's other logic for a case with
  no evidence yet of being common.
- A 3-way collision is arguably a materially different situation to review
  than the original 2-way one (a pattern, not a one-off), so surfacing it
  as its own item is defensible on its own terms, not just an accepted
  limitation.
- The original, now-stale-membership bead does not go away or become
  incorrect — the two entries it named still do collide — so nothing false
  is left open; at worst a reviewer sees two related beads instead of one
  and closes the smaller as covered by the larger.

If this proves noisy in practice, a future iteration could special-case it
(e.g. only re-file when the group's size class changes, pair → group).
Not built now, matching crn-0yv.2's own stated bar for speculative
machinery.

## Design decision: label and priority

Labels: `dim:dedup,source:cairn-librarian` — a new `dim:` value distinct
from crn-0yv.2's `dim:freshness`, following the same `dim:<detection-kind>`
convention so a reviewer can filter either librarian dimension
independently. `source:cairn-librarian` is shared across both, matching the
existing convention. Priority `3` (matching crn-0yv.2's freshness beads):
proposal-only maintenance work, not urgent.

## Confirmed against the real bd CLI

Re-checked directly against the installed `bd` binary for this bead rather
than assumed from crn-0yv.2's own write-up: `bd list --help` confirms
`--label` (singular, comma-separated, AND-filter), `--title-contains`
(case-insensitive substring), and `--json`; `bd create --help` confirms
`--labels` (plural), `--title`, `--type`, `--priority`, `--stdin`, and
`--silent` (prints only the new issue id). All match crn-0yv.2's documented
flag split exactly, confirmed independently rather than carried over
unchecked.

## Smoke-tested end-to-end

The step's shell body (extracted from the fenced block below and run
directly, to catch any escaping mistake rather than trust the source by
eye) was run against mock `cairn` and `bd` binaries on `$PATH`, with three
canned findings covering every branch in one pass:

1. A `content` pair (`g/hook-a`, `g/hook-b`) whose anchor the mock `bd list`
   recognizes as already tracked.
2. A `topic_key` 3-way group in tier `rig` (`rig/one`, `rig/three`,
   `rig/two`) with no existing bead.
3. A `content` pair (`rig/one`, `rig/onextra`) — chosen specifically to
   share the raw substring `rig/one` with finding 2's anchor — also with no
   existing bead.

Observed: finding 1 is skipped with `already tracked as beads-042`; finding
2 files a new bead with title `cairn librarian: topic_key collision
"dup-key" in tier rig [ids:rig/one,rig/three,rig/two]`; finding 3
independently files its own new bead with title `cairn librarian:
content-similarity dup [pair:rig/one|rig/onextra] (score 0.6)`, confirming
its anchor did **not** false-match finding 2's despite the shared
substring; the final summary line reads `2 bead(s) filed, 1 already
tracked`. A separate run with an empty `cairn dedup` result (`[]`) produced
`0 bead(s) filed, 0 already tracked` with no errors. Not run against a live
`bd`/`cairn` pair — that's crn-0yv.5's smoke-test scope once this step is
assembled into the real formula — but the shell logic itself is confirmed
correct, not just syntax-checked.

## Bead-filing step (ready for crn-0yv.5 to assemble into mol-cairn-librarian.formula.toml)

```toml
[[steps]]
id = "dedup-candidate-beads"
title = "File a bd bead for every cairn dedup/re-scope candidate, deduplicated across cycles"
description = '''
Run the read-only dedup/re-scope detector (crn-0yv.3) and turn every
finding into exactly one open bd bead. "Not a duplicate per cycle" comes
from checking bd for an already-open bead whose title contains this
finding's bracket-delimited anchor token before creating a new one (see
docs/plans/cairn-librarian-dedup-detection-beads.md for why a bracket token
rather than a natural-language phrase, and for the deliberate limitation
around growing topic_key groups). This step only ever calls "cairn dedup"
(read-only) and "bd list"/"bd create" (bd's own store) — it never merges,
deletes, or rewrites a cairn entry, per crn-xw3 guardrail 8.

  STORE="${CAIRN_STORE:?CAIRN_STORE must be set}"
  FILED=0
  SKIPPED=0

  while IFS= read -r finding; do
    KIND=$(printf '%s' "$finding" | jq -r '.kind')
    IDS_CSV=$(printf '%s' "$finding" | jq -r '.entry_ids | join(",")')
    DETAIL=$(printf '%s' "$finding" | jq -r '.detail')

    if [ "$KIND" = "content" ]; then
      ID_LO=$(printf '%s' "$finding" | jq -r '.entry_ids[0]')
      ID_HI=$(printf '%s' "$finding" | jq -r '.entry_ids[1]')
      SCORE=$(printf '%s' "$finding" | jq -r '.similarity')
      ANCHOR="[pair:${ID_LO}|${ID_HI}]"
      TITLE="cairn librarian: content-similarity dup ${ANCHOR} (score ${SCORE})"
    else
      KEY=$(printf '%s' "$finding" | jq -r '.topic_key')
      TIER=$(printf '%s' "$finding" | jq -r '.tier')
      ANCHOR="[ids:${IDS_CSV}]"
      TITLE="cairn librarian: topic_key collision \"${KEY}\" in tier ${TIER} ${ANCHOR}"
    fi

    EXISTING=$(bd list --label=dim:dedup,source:cairn-librarian \
      --title-contains="$ANCHOR" --json --no-pager 2>/dev/null | jq -r '.[0].id // empty')

    if [ -n "$EXISTING" ]; then
      echo "skip $KIND $ANCHOR: already tracked as $EXISTING"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    NEW_ID=$(bd create \
        --title="$TITLE" \
        --stdin --type=task --priority=3 \
        --labels=dim:dedup,source:cairn-librarian --silent <<BODY
Sweep detected a $KIND duplicate/re-scope candidate across shared-tier cairn entries: $IDS_CSV.

$DETAIL

Filed by the mol-cairn-librarian dedup-detection step (crn-0yv.3). This is a
proposal only: the step itself never merges, deletes, or rewrites an entry
(crn-xw3 guardrail 8). To resolve, a reviewer or the entries' authors should
compare them and decide whether to consolidate, re-scope one of them, or
close this bead as a false positive (e.g. two same-tier entries that
legitimately picked the same topic_key for unrelated reasons).
BODY
    )
    echo "filed $NEW_ID for $KIND $ANCHOR"
    FILED=$((FILED + 1))
  done < <(cairn dedup --store "$STORE" | jq -c '.[]')

  echo "librarian dedup sweep: $FILED bead(s) filed, $SKIPPED already tracked"
'''
```

Notes on the step body itself:

- Findings are consumed via process substitution (`done < <(...)`), not a
  trailing pipe into `while`, for the same reason as crn-0yv.2's step: a
  piped `while read` runs in a subshell in bash, which would silently
  discard the `FILED`/`SKIPPED` counters at loop exit.
- No `set -e`, also matching crn-0yv.2's step: a single `bd create` failure
  doesn't abort the rest of the cycle, and a finding that failed to file
  this cycle gets another chance next cycle.
- The bead body is piped straight into `bd create --stdin` via a heredoc
  rather than built as a `--description="..."` argument, so no manual
  shell-quoting of multi-line text is needed.

## Scope note for crn-0yv.5

This bead delivers the dedup detection logic (`internal/cairn/dedup.go`,
already implemented) and this ready-to-paste bead-filing step. Assembling
the full `mol-cairn-librarian.formula.toml` — this step plus crn-0yv.1's
stale-review-branch recovery step and crn-0yv.2's freshness-drift step,
tier-parameterized and smoke-tested end to end against a live `bd`/`cairn`
pair — is crn-0yv.5's explicit scope, not duplicated here. crn-0yv.5 will
also need to reconcile the `entryTier()` helper, currently duplicated
verbatim between `internal/cairn/sweep.go` (crn-0yv.2) and
`internal/cairn/dedup.go` (this bead) since the two sibling branches were
built independently off `origin/main` per the PM's "3 independent
step-logic beads" decomposition of the crn-0yv epic.
