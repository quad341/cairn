# Plan: freshness-drift bead filing for mol-cairn-librarian

Root bead: crn-0yv.2 (source: crn-xw3 AF3 steps 4-5, crn-0yv.2 acceptance criterion 3)

## Why

`internal/cairn/sweep.go` (this same bead, crn-0yv.2) already implements the
read-only detection half: `Sweep()` walks every shared-tier entry, reports
each one's freshness, and independently sanity-checks that a files-anchor's
paths are actually tracked at the anchor repo's HEAD before trusting a Fresh
verdict — working around `cairn verify`'s untracked-path fingerprint
fabrication (crn-6az.8.2, open) rather than depending on that bug being fixed
first. That satisfies acceptance criteria 1, 2, 4, and 5.

What's left is criterion 3: turn a non-fresh `Sweep()` finding into exactly
one open bd bead, not a fresh duplicate every time the sweep cycle runs. Per
crn-xw3 AF3 step 5 and guardrail 8, this is strictly a proposal — the step
must never rewrite an already-curated shared entry itself, only file a bead
for a human or a future targeted step to act on.

## What Sweep() already gives this step

`cairn sweep --store "$CAIRN_STORE"` prints a JSON array of
`{id, tier, status, detail, anchor_type}` objects (`internal/cairn/sweep.go`,
`cmd/sweep.go`). `status` is one of the lowercase freshness constants —
`"fresh"`, `"stale"`, `"unknown"` (`internal/cairn/freshness.go:15-17`) — so a
`jq 'select(.status != "fresh")'` filter is a correct, exact match on the two
non-fresh states, not a fuzzy string check.

## Design decision: a single non-fresh observation is enough to file

crn-0yv.2's acceptance criterion and crn-xw3 AF3 step 5 both describe the
target as "**persistent** drift" producing "exactly one" bead. Two readings
are possible: (a) file on the first non-fresh sighting, relying on a dedup
check to satisfy "exactly one" across later cycles, or (b) require the same
entry to come back non-fresh on two or more consecutive cycles before filing
anything, which would need this step to remember what the *previous* cycle
saw.

This plan implements (a). Reasons:

- AF3 step 4 frames the sweep's purpose as catching "source drift the lazy
  on-read check hasn't surfaced yet" — by the time a sweep cycle observes an
  entry as Stale or Unknown, that drift has already been sitting unnoticed
  since the last real read. It is already "persistent" in the sense that
  matters; waiting for a second sweep cycle to confirm it only delays
  visibility, it doesn't change the underlying fact.
- The dominant non-fresh signal, content-hash Stale (`Check()` comparing the
  stored fingerprint against a freshly recomputed one), is deterministic
  given the same git HEAD. It cannot flip back to Fresh on its own between
  cycles, so there's no risk of filing a bead for a transient blip that
  would have self-resolved.
- Reading (b) would need real cross-cycle state (a marker file, or querying
  bd for what was seen last time) purely to answer "did I already see this
  entry once before" — machinery this bead's acceptance criteria don't ask
  for and that has no evidence of being needed yet (see the residual risk
  noted below, which is deliberately not built around speculatively).

"Not a duplicate per cycle" is satisfied instead by a dedup check against bd
itself before filing (next section) — cycle 2 finds cycle 1's bead and skips.

One caveat worth stating plainly: the *other* non-fresh path, `Sweep()`'s
untracked-path sanity check, shells out to `git ls-files` against the
anchor's repo. Unlike a content-hash mismatch, that check could in principle
report a false Unknown from a transient environment problem — the anchor
repo momentarily unmounted or not yet cloned — rather than a genuinely
untracked path. If that proves to be a real source of noisy beads in
practice, a future iteration could gate specifically that path on repeated
sightings (e.g. using the filed bead's own age as the counter, so no new
state store is needed). Not built now: no evidence yet that it's needed, and
per crn-6az.8.2's own severity (Medium, workaround already in place) it
isn't worth the added complexity pre-emptively.

## Design decision: dedup query and title anchoring

The dedup check is `bd list --label=dim:freshness,source:cairn-librarian
--title-contains="on $ID (" --json`. Two details matter:

- **Anchoring, not a bare ID substring match.** cairn entry ids are
  namespaced paths (e.g. `architect/foo`), and one id can be a literal
  prefix of another (`architect/foo` vs. `architect/foo-bar`) the same way
  this rig's dotted bead ids can be (documented and solved for bead ids in
  `docs/plans/cairn-critic-loop-landing-check.md`, crn-rqf.3). A bare
  `--title-contains="$ID"` would treat a bead about `architect/foo-bar` as
  already covering `architect/foo`. Unlike that prior case, this step
  composes the bead title itself (`cairn librarian: $STATUS freshness on
  $ID ($TIER)`), so instead of a regex-anchored search over free-text commit
  messages it just needs to search for its own fixed delimiter,
  `"on $ID ("`, immediately after the id. Checked in isolation before
  relying on it: `"on architect/foo ("` is not a substring of `"...on
  architect/foo-bar (rig)"` (the character after `foo` differs, `-` vs.
  ` `), so the two ids don't collide.
- **`bd list` uses `--label` (singular); `bd create` uses `--labels`
  (plural).** Both take the same comma-separated form and both accept `-l`,
  but the long flag name differs between the two subcommands (confirmed
  against `bd list --help` / `bd create --help` on the installed binary,
  not assumed from one and applied to the other).

No `--status` filter is passed to the dedup query. `bd list`'s own default
(undocumented flag is `--all`, "Show all issues including closed *overrides*
default filter") already excludes closed issues, so omitting `--status`
finds any open/in_progress/blocked/deferred bead already tracking this
entry — narrower than that would risk re-filing while a matching bead is,
say, blocked rather than open.

## Confirmed against the real bd CLI

Flag names, the JSON output shape (`bd list --json` on an empty result
prints `[]`, not a wrapped object), and the anchoring behavior above were
checked directly against the installed `bd` binary and a real (if
currently-empty) query against this rig's own database before writing the
step below, not assumed — this session had already gotten `gc mail`
sub-command flags wrong once by guessing, so CLI surfaces get verified here
on purpose.

## Smoke-tested end-to-end

The step's shell body (TOML-parsed back out of the fenced block below, to
catch any escaping mistake rather than trust the source by eye) was run
against mock `cairn` and `bd` binaries on `$PATH`, covering all three
branches in one pass: a fresh finding, a stale finding with no existing
bead, and an unknown finding whose sibling id (`architect/foo-bar`, prefix-
colliding with the stale finding's `architect/foo`) does have one. Observed:
the fresh finding never reaches `bd` at all; the stale finding files a new
bead with the expected title, labels, and multi-line body (fingerprint
detail interpolated, the id substituted correctly into the "re-run cairn
verify" hint); the unknown finding's dedup query correctly matches only its
own existing bead and not its prefix-colliding sibling's, and is skipped;
and the final summary line reads `1 bead(s) filed, 1 already tracked`. Not
run against a live `bd`/`cairn` pair — that's crn-0yv.5's smoke-test scope
once this step is assembled into the real formula — but the shell logic
itself is confirmed correct, not just syntax-checked.

## Bead-filing step (ready for crn-0yv.5 to assemble into mol-cairn-librarian.formula.toml)

```toml
[[steps]]
id = "freshness-drift-beads"
title = "File a bd bead for every non-fresh cairn sweep finding, deduplicated across cycles"
description = '''
Run the read-only freshness sweep (crn-0yv.2) and turn every non-fresh
finding into exactly one open bd bead. A single Stale/Unknown observation is
enough to file (see docs/plans/cairn-librarian-freshness-drift-beads.md for
why persistence-gating across cycles isn't needed) — "not a duplicate per
cycle" comes from checking bd for an already-open bead before creating a new
one. This step only ever calls "cairn sweep" (read-only) and "bd list"/"bd
create" (bd's own store) — it never rewrites a cairn entry, per crn-xw3
guardrail 8.

  STORE="${CAIRN_STORE:?CAIRN_STORE must be set}"
  FILED=0
  SKIPPED=0

  while IFS= read -r finding; do
    ID=$(printf '%s' "$finding" | jq -r '.id')
    TIER=$(printf '%s' "$finding" | jq -r '.tier')
    STATUS=$(printf '%s' "$finding" | jq -r '.status')
    DETAIL=$(printf '%s' "$finding" | jq -r '.detail')
    ANCHOR=$(printf '%s' "$finding" | jq -r '.anchor_type')

    EXISTING=$(bd list --label=dim:freshness,source:cairn-librarian \
      --title-contains="on $ID (" --json --no-pager 2>/dev/null | jq -r '.[0].id // empty')

    if [ -n "$EXISTING" ]; then
      echo "skip $ID ($STATUS): already tracked as $EXISTING"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    NEW_ID=$(bd create \
        --title="cairn librarian: $STATUS freshness on $ID ($TIER)" \
        --stdin --type=task --priority=3 \
        --labels=dim:freshness,source:cairn-librarian --silent <<BODY
Sweep detected $STATUS freshness for shared-tier cairn entry "$ID" (tier: $TIER, anchor: $ANCHOR).

$DETAIL

Filed by the mol-cairn-librarian freshness-sweep step (crn-0yv.2). This is a
proposal only: the sweep step itself never rewrites an already-curated entry
(crn-xw3 guardrail 8) and never calls "cairn verify" on your behalf. To
resolve, a reviewer or the entry's original author should inspect the
anchor, then either re-run "cairn verify $ID" once the source has settled,
re-anchor the entry, or retire it if it no longer applies.
BODY
    )
    echo "filed $NEW_ID for $ID ($STATUS)"
    FILED=$((FILED + 1))
  done < <(cairn sweep --store "$STORE" | jq -c '.[] | select(.status != "fresh")')

  echo "librarian freshness sweep: $FILED bead(s) filed, $SKIPPED already tracked"
'''
```

Notes on the step body itself:

- Findings are consumed via process substitution (`done < <(...)`), not a
  trailing pipe into `while`. A piped `while read` runs in a subshell in
  bash, which would silently discard the `FILED`/`SKIPPED` counters at loop
  exit; process substitution avoids that.
- No `set -e`. A single `bd create` failure (e.g. a transient dolt-sync
  hiccup) intentionally doesn't abort the rest of the cycle — the loop just
  moves on to the next finding, and a still-non-fresh entry that failed to
  file this cycle gets another chance next cycle.
- The bead body is piped straight into `bd create --stdin` via a heredoc
  rather than built as a `--description="..."` argument, so no manual
  shell-quoting of multi-line text is needed.

## Scope note for crn-0yv.5

This bead delivers the sweep detection logic (`internal/cairn/sweep.go`,
already implemented) and this ready-to-paste bead-filing step. Assembling
the full `mol-cairn-librarian.formula.toml` — this step plus crn-0yv.1's
stale-review-branch recovery step and crn-0yv.3's dedup/re-scope detection
step, tier-parameterized and smoke-tested end to end — is crn-0yv.5's
explicit scope, not duplicated here.

## Scope gap flagged, deliberately not addressed here

`crn-6az.8.2` (the untracked-path fingerprint fabrication bug this bead
works around via `Sweep()`'s independent sanity check) remains open. This
plan's step files a bead pointing whoever picks up the drift at re-running
`cairn verify` once the source settles; it does not fix the underlying
`ComputeFingerprint`/`expand()` fallback. That fix is `crn-6az.8.2`'s own
scope — a `cairn` core-command bug, not a librarian-formula step — and isn't
folded in here.
