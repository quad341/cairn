# Plan: squash-merge-aware landing-verification gate

Root bead: crn-rqf.3 (source: crn-rqf step 5, crn-7oa FR-7)

## Why

`crn-7oa` (FR-7) requires that no critic-loop-filed bead close until its fix has
actually landed on `origin/main` — not merely committed on a branch. This is not
speculative hardening: `crn-wok`, `crn-1qb`, and `crn-eat` were all previously
false-closed on a branch commit that never made it to `main`, and `crn-rqf`'s own
notes record a second, independently-found instance of the identical pattern (its
own close reason cited a commit that turned out to live only on a stray unmerged
branch, `gc-pm-d67a5423a66c`).

`crn-7oa`'s literal wording prescribes `git merge-base --is-ancestor <sha>
origin/main`. This rig squash-merges every PR, and under squash-merge a fix
branch's commit SHA is **never** an ancestor of the resulting squashed commit on
`main` — `--is-ancestor` silently never succeeds, which would make the gate
permanently, silently non-functional. `mol-witness-patrol.formula.toml`
(`recover-orphans` step) already hit this exact failure in a real incident (the
77wo8 ledger bead false-reopen) and worked around it by grepping `main`'s log for
the bead id instead.

## The assumption that needed correcting

`mol-witness-patrol` and `crn-rqf`'s own body both describe the workaround as
"main's commit subjects embed the bead id in parentheses" and grep
`git log main --oneline` accordingly. That's accurate for gc-management's own
squash convention. It is **not** accurate for cairn: checked directly against
cairn's real `origin/main` (11 most recent PR-merge commits), only 1 carried a
bead-id in its `--oneline` subject (`1f8a13b ... (refs crn-419) (#13)`, added by
hand to that one PR's title) — the other 10 show GitHub's default squash subject,
`<title> (#<PR-number>)`, with no bead-id at all. A bare subject-line grep would
have missed 10 of 11 real, landed beads.

The bead id **is** reliably present, though — just not in the subject. This
fleet's own commit convention puts `(<bead-id>)` in every individual commit's
own message before it's squashed (e.g. this repo's own unmerged
`f11ab90 feat(cairn): ... (crn-419.3)`), and GitHub's default squash-merge body
preserves each constituent commit's original subject+body verbatim. So the id
survives squash — in the full message, not the truncated `--oneline` subject.
Confirmed directly: `git log -1 --format=%B 3bc2df7` (squashed subject shows only
`(#12)`) contains `(crn-419.2)` in its body.

## Validated technique

```sh
git log <ref> -E --grep="<id-with-literal-dot-escaped>(\$|[^.0-9])"
```

- `git log --grep` matches the full commit message (subject **and** body), so it
  finds ids that only survive in the squashed body — unlike a raw
  `git log --oneline | grep` pipeline, which only ever sees the subject line.
- **Anchoring is required, not cosmetic.** This rig's bead ids are pervasively
  dotted/hierarchical (`crn-419.1`..`.4`, `crn-rqf.1`..`.5`, `crn-6az.2`..`.8`
  with grandchildren `crn-6az.8.1`/`crn-6az.8.2` — all real, concurrently open at
  the time of writing). A bare substring grep for a parent id (`crn-6az.8`) is a
  literal string-prefix of its children's ids (`crn-6az.8.1`), so it would
  false-positive as "landed" the instant *any one child* merges, even if the
  parent itself never did. The pattern above escapes the id's literal `.` (so it
  doesn't act as a regex wildcard) and requires the character immediately after
  the id to be either end-of-message or anything other than `.`/a digit — i.e.
  "this id, and nothing that extends it into a longer sibling id."

  Sanity-checked in isolation before trusting it against real history:
  `printf 'crn-6az.8 done\ncrn-6az.8.1 fix\ncrn-6az.8.2 fix\n' | grep -E
  "crn-6az\.8(\$|[^.0-9])"` matches only the `crn-6az.8` line.

## Empirical validation (real cairn history, checked 2026-07-22)

| bead-id | ground truth | `git log origin/main -E --grep=...` result | verdict |
|---|---|---|---|
| `crn-419.2` | landed (real, merged PR #12) | `3bc2df7`, `571515d` | matched — correctly landing-verified |
| `crn-di7` | landed (real, merged PR #11) | `c28b0ed` | matched — correctly landing-verified |
| `crn-419.3` | **not** landed (local-only, unmerged) | *(no match)* | correctly not-yet-landed |
| `crn-419.4` | **not** landed (local-only, unmerged) | *(no match)* | correctly not-yet-landed |

The `crn-419.3`/`crn-419.4` negative result was cross-checked against a positive
control (`git log HEAD -E --grep=...` on the local branch that actually holds
those two commits, which *does* match) to rule out a tooling false-negative —
the technique genuinely distinguishes landed from not-yet-landed, it isn't just
silently failing to match anything.

This satisfies crn-rqf.3's acceptance criteria: tested against real merged
bead-ids (not synthetic), tested against a real bead that exists only on an
unmerged branch, and validated against this rig's actual merge convention
(squash-merge) rather than assumed from `crn-7oa`'s literal `--is-ancestor`
wording or `mol-witness-patrol`'s literal `--oneline`-subject wording.

## Gate step (ready for crn-rqf.5 to assemble into `mol-cairn-critic.formula.toml`)

```toml
[[steps]]
id = "landing-check"
title = "Verify a critic-loop-filed bead's fix landed on origin/main before closing"
description = '''
Before closing any bead this loop filed (step 4), verify its fix is actually
on origin/main. This rig squash-merges: git merge-base --is-ancestor never
succeeds (a branch commit is never an ancestor of the squashed result), and a
bare `git log main --oneline | grep <id>` misses ids that only survive in the
squashed commit body (cairn's own PR-merge subjects usually carry only a PR
number, not the bead id). Use --grep, which searches the full message, and
anchor the id so it can't false-match a longer sibling id under this rig's
dotted hierarchical ids (crn-419.1 is a literal prefix of crn-419.10, were
that ever to exist):

  git fetch origin --quiet
  ID="<the bead id>"
  PATTERN=$(printf '%s' "$ID" | sed 's/\./\\./g')
  MATCH=$(git log origin/main -E --grep="${PATTERN}(\$|[^.0-9])" --format="%H %s" -1)

  if [ -n "$MATCH" ]; then
    SHA=$(printf '%s' "$MATCH" | cut -d' ' -f1)
    bd close "$ID" --reason="landing-check: found in origin/main $SHA (\"$(printf '%s' "$MATCH" | cut -d' ' -f2-)\")"
  else
    bd update "$ID" --notes="landing-check $(date -u +%Y-%m-%dT%H:%M:%SZ): no match on origin/main — NOT landed, left open, escalating"
    gc mail send mayor "landing-check: $ID not yet landed" "Critic-loop wanted to close $ID but its fix isn't on origin/main yet — left open, needs follow-up."
    echo "$ID not landed — left open, escalated"
  fi
'''
```

This satisfies the remaining acceptance criteria directly: not-yet-landed never
closes (state is appended via `bd update --notes` and escalated via mail, bead
stays open); landed closes with a `--reason` that cites the specific commit SHA
and subject matched, not a vague assertion.

## Scope note for crn-rqf.5

This bead delivers the validated technique and a ready-to-paste step block.
Assembling the full `mol-cairn-critic.formula.toml` — this step plus steps 1-4
and 6 from `crn-rqf.1`/`crn-rqf.2`/`crn-rqf.4` — is `crn-rqf.5`'s explicit scope,
not duplicated here.

## Scope gap flagged, deliberately not addressed here

PM commented on `crn-rqf.3` (2026-07-22 00:28) that this same false-landing-
citation pattern has now hit *ordinary* PM-decomposition closes twice
(`crn-419`'s own close, and `crn-rqf`'s own close — both cited commits that
existed only on a stray unmerged branch), not just critic-loop-filed ones, and
suggested this check might belong somewhere reusable fleet-wide (`bd close
--verify-landed`, or a `bd doctor`/`bd preflight` check) rather than living only
as critic-loop-formula-internal machinery.

That's a real gap, but it's a `bd` (beads tool) feature, not a `cairn` one — out
of scope for a task rooted in cairn's own repo and formula set. Filed as a
separate follow-up (see bead cross-referenced in `crn-rqf.3`'s notes) rather than
silently expanding this bead's scope or building it unreviewed into the wrong
codebase.
