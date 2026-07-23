# Plan: squash-merge-aware landing-verification gate

Root bead: crn-rqf.3 (source: crn-rqf step 5, crn-7oa FR-7)
Fix: crn-rqf.3.1 (parenthetical-anchoring correction for a prose-cross-reference false-positive found by crn-rqf.4)

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
git log <ref> -E --grep="\(<id-with-literal-dot-escaped>\)"
```

- `git log --grep` matches the full commit message (subject **and** body), so it
  finds ids that only survive in the squashed body — unlike a raw
  `git log --oneline | grep` pipeline, which only ever sees the subject line.
- **The id must sit in its own bare parentheses, touching on both sides.** This
  rig's commit convention tags the commit that actually implements a bead with
  `(<bead-id>)` — the id alone, nothing else inside the parens — on that
  commit's own subject line before squashing (verified directly against real
  history: `(crn-419.2)`, `(crn-di7)`, `(crn-rqf.1)`, and `(crn-rqf.3)` all
  appear exactly this way). A reference to a *different* bead never takes this
  shape: it shows up bare (`refs crn-rqf.9`) or with other words sharing the
  parens (`(refs crn-419)`), never as that bead's id alone in its own parens.
  Requiring the parens to touch the id on both sides also fully subsumes the
  weaker boundary-character anchoring this replaced (see correction below):
  `\(crn-6az\.8\)` still can't match inside `(crn-6az.8.1)`, since `.1)`
  follows the `8`, not a bare `)`.

  Sanity-checked in isolation before trusting it against real history:
  `printf '* fix: foo (crn-6az.8)\n* fix: bar (crn-6az.8.1)\n* fix: baz
  (crn-6az.8.2)\n' | grep -E "\(crn-6az\.8\)"` matches only the `crn-6az.8`
  line.

## Correction (crn-rqf.3.1): boundary-anchoring alone wasn't enough

The pattern above replaces an earlier version that anchored on a boundary
character instead of parens — `<id>($|[^.0-9])`, matching the id anywhere in
the message as long as it wasn't extended into a longer sibling id. An
independent validator re-test (`crn-rqf.4`) found that pattern has a real
false-positive: it matches a bead id *anywhere*, including a plain-English
forward-reference to a different, unimplemented bead. Confirmed on real
history — this doc's own landing commit, `407db36`, reads "Includes a
ready-to-paste formula step for crn-rqf.5 to assemble into
mol-cairn-critic.formula.toml." `crn-rqf.5` there is a bare mention, not an
implementation — the bead was open and unimplemented at the time (and remains
so) — but a trailing space is a valid non-dot/non-digit boundary, so the old
pattern matched it, which would have fired `bd close crn-rqf.5` on a bead with
zero lines of actual implementation.

The parens-must-touch-the-id fix above closes this: `crn-rqf.5` never appears
as bare `(crn-rqf.5)` anywhere in `407db36`, so the corrected pattern
correctly returns no match, while `crn-rqf.3` — what that commit actually
implements, tagged `(crn-rqf.3)` — still matches. Full before/after in the
table below.

## Empirical validation (real cairn history)

| bead-id | ground truth | old pattern `id($\|[^.0-9])` | new pattern `\(id\)` | verdict |
|---|---|---|---|---|
| `crn-419.2` | landed (real, merged PR #12) | matched `3bc2df7` | matched `3bc2df7` | correct under both |
| `crn-di7` | landed (real, merged PR #11) | matched `c28b0ed` | matched `c28b0ed` | correct under both |
| `crn-rqf.1` | landed (real, merged PR #15) | matched `22a58ba` | matched `22a58ba` | correct under both |
| `crn-rqf.5` | **not** landed — OPEN, unimplemented, zero commits on any branch (`bd show crn-rqf.5`; `git log --all --grep`) | matched `407db36` — a bare prose mention ("...for crn-rqf.5 to assemble into...", not an implementation) | *(no match)* | **old pattern false-positived (this bug); new pattern fixes it** |
| `crn-78d` | **not** landed — decided not to build in this repo (filed upstream as `gastownhall/beads#4960` instead), zero commits on any branch | *(no match)* | *(no match)* | correct under both |

`crn-419.2`, `crn-di7`, and `crn-rqf.1` are this doc's and `crn-rqf.4`'s
already-validated positive controls, re-run here to confirm the parens
requirement doesn't regress them — all three carry their id as a bare `(id)`
at the end of a sub-commit subject, so both patterns still match. `crn-rqf.5`
is the exact case `crn-rqf.4` found broken: it's this bug's reproduction, not
a synthetic example. This doc's original negative controls, `crn-419.3` and
`crn-419.4`, have since actually landed (both closed 2026-07-22, confirmed via
`git log --grep` finding commit `65374e1`) — no longer valid not-yet-landed
examples, superseded above by `crn-78d`.

This satisfies both crn-rqf.3's original acceptance criteria (tested against
real merged bead-ids, a real not-yet-landed bead, and this rig's actual
squash-merge convention) and crn-rqf.3.1's (all previously-validated positive
controls still match; all previously-validated negative controls — `crn-78d`
directly, `crn-419.3`/`crn-419.4` no longer applicable since they've since
landed — still correctly reject; the specific reported false-positive,
`crn-rqf.5` against `407db36`, no longer matches).

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
require the id to sit alone in its own parens: this rig's convention tags the
commit that actually implements a bead with `(<bead-id>)`, so requiring the
parens closes two gaps at once — it can't false-match a longer sibling id
under this rig's dotted hierarchical ids (crn-419.1 is a literal prefix of
crn-419.10, were that ever to exist), and it can't false-match a plain-English
mention of a different bead elsewhere in the same message (crn-rqf.3.1: an
earlier version matched "...for crn-rqf.5 to assemble into..." even though
crn-rqf.5 was never implemented there):

  git fetch origin --quiet
  ID="<the bead id>"
  PATTERN=$(printf '%s' "$ID" | sed 's/\./\\./g')
  MATCH=$(git log origin/main -E --grep="\(${PATTERN}\)" --format="%H %s" -1)

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
