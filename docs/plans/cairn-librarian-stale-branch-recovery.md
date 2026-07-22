# Plan: stale review-branch recovery for mol-cairn-librarian

Root bead: crn-0yv.1 (source: crn-xw3 UC4/AF3, crn-0yv.1 acceptance criteria)

## Why

A shared-tier `cairn remember` call (crn-419.4) commits the entry to its own
`remember/<id>` branch and mails the resolved reviewer once, but nothing
currently notices when that mail goes unanswered. A branch can sit unmerged
indefinitely with no second signal to the reviewer and no visibility for
anyone else that it's stuck. crn-0yv.1 closes that gap: reactively re-notify
the same reviewer once a branch has aged past a threshold, then — only if
still unactioned after that — escalate to a bd bead so a human or a future
targeted step picks it up.

## What this bead already gives this step

`internal/cairn/branches.go`'s `ListReviewBranches(ctx, store, now)` lists
every `remember/*` branch not yet merged into the store's checked-out
branch, together with each one's age (relative to the caller-supplied `now`)
and the tier/value its actually-changed file belongs to — never parsed from
the branch name (`TestListReviewBranchesTierFromPathNotBranchName`). A
branch that's been merged, deleted, or received a new commit since the last
pass is excluded or has its age reset automatically, because all three are
just consequences of listing live refs and diffing against each branch's own
tip — no separate bookkeeping tracks "have I seen this branch before."

`cmd/branches.go`'s `cairn stale-branches` wraps that into a CLI command:
buckets each branch into `fresh` / `notify` / `escalate` by age
(`--notify-after`, `--escalate-after`; default 24h/72h), and for a
`notify`-status branch, resolves a reviewer (`resolveReviewer`, crn-419.6 —
`--reviewer` flag > `$CAIRN_REVIEWER` > per-tier computed default) and mails
them a reminder in-process, via a `mailSend` helper extracted from
`sendReviewMail` (`cmd/reviewer.go`) so both call sites share the same `gc
mail send` plumbing instead of duplicating it. `--dry-run` computes and
reports status without sending mail. Every branch — fresh, notify, escalate,
or a per-branch `error` — is reported as JSON on stdout; nothing is filtered
out, mirroring `cairn sweep`'s report-everything-let-the-caller-filter
convention.

## Design decision: Go re-notifies in-process; a wrapping shell step escalates

crn-0yv.2's sibling step (`docs/plans/cairn-librarian-freshness-drift-beads.md`)
puts its entire detect-and-file logic in one shell step wrapped around a
read-only `cairn sweep`. crn-0yv.1 deliberately splits differently: the
reactive re-notify (mailing the reviewer) happens **inside** `cairn
stale-branches` itself, in Go, and only the bd-bead escalation stays a
shell/formula concern.

Reason: crn-0yv.1's own acceptance criteria call for reusing crn-419.6's
reviewer-resolution code, not reimplementing its flag>env>computed-default
precedence a second time in shell. That logic
(`resolveReviewer`/`defaultReviewer`/`validateReviewerAddress`) is
unexported Go in `cmd/reviewer.go` — there is no `cairn resolve-reviewer`
CLI surface for a shell step to call out to, and adding one purely so a
formula step could reimplement `sendReviewMail`'s mail-composition logic
around it would duplicate behavior that already exists and is already
tested (`cmd/reviewer_test.go`). Calling `sendStaleBranchReminder` directly
from the same command that just resolved tier and reviewer keeps that
reuse real (same function calls, not same *rules* re-derived independently
in two languages) and keeps the reminder's mail-composition next to the
data it's built from.

Escalation stays a shell/formula concern for the opposite reason:
`bd create`/`bd list` dedup has no Go-native equivalent in this codebase to
reuse, and crn-0yv.2 already established the bd-CLI-shelling pattern this
step's escalation half should just follow, not reinvent.

## Design decision: two absolute-age thresholds, no cross-cycle state

The acceptance criteria's shape — re-notify, then escalate only "after the
next sweep cycle" if still unactioned — could be read as needing this step
to remember what the *previous* cycle saw (a marker file, or a cycle
counter) to know whether a branch has already had its one reminder.

This plan uses two absolute-age thresholds instead (`--notify-after` /
`--escalate-after`, checked each pass against the branch's own commit
timestamp) and keeps no cross-cycle state, for the same reason
crn-0yv.2's sibling plan gives for its own single-observation design:
the signal this step keys off — git commit age — is itself already a
durable, monotonic record of elapsed time since the last real reviewer
action (any new commit on the branch resets it, per `ListReviewBranches`'
own age computation). A cycle counter would only be answering a question
the commit timestamp already answers, while additionally coupling
correctness to the sweep actually running on a regular cadence. If a sweep
cycle is skipped or delayed, an absolute-age design still buckets branches
correctly on the next run; a cycle-counted design would not.

This also directly satisfies "not a duplicate per cycle" for the escalate
side: once a branch is old enough to escalate, it stays `escalate`-status on
every later pass (its age only grows), so the shell step's own bd dedup
check (next section) is what prevents re-filing — not anything Go needs to
track. And it satisfies "re-notify... then escalate" ordering on the notify
side implicitly: `evaluateBranch` (`cmd/branches.go`) only mails a
`notify`-status branch, never an `escalate`-status one, so once a branch
crosses into escalate range the reminders stop (avoiding redundant nagging
once a bead is about to exist) without any explicit "already reminded" flag
to maintain.

## Design decision: dedup query anchoring

Mirrors crn-0yv.2's own `"on $ID ("` delimiter trick
(`docs/plans/cairn-librarian-freshness-drift-beads.md`), applied to branch
names instead of entry ids: `bd list
--label=dim:review-branch,source:cairn-librarian --title-contains="on
$BRANCH (" --json`. A bare substring match on the branch name isn't safe to
skip here either — `remember/<id>` embeds the entry id, and one entry's id
can be a literal prefix of another's (a topic_key that itself contains a
hyphen collides with `NewEntry`'s own `topicKey + "-" + suffix` id format,
e.g. topic `foo` → id `foo-1a2b3c4d`, topic `foo-1a2b3c4d` → id
`foo-1a2b3c4d-5e6f7a8b`). Anchoring on `"on $BRANCH ("` immediately after the
full branch name, the same way crn-0yv.2 anchors on the full entry id,
avoids that collision the same way.

## Tested

`internal/cairn/branches_test.go`: age computation and tier resolution
(never from the branch name), and each of the AC's three exclusion cases —
merged, deleted, new-commit-resets-age — as independent tests.
`cmd/branches_test.go`: age-threshold bucketing end to end through the CLI,
`--dry-run` genuinely skipping the mail call (proven by pointing it at a
gc stub that always fails and asserting no error surfaces), a per-branch
mail failure being reported without failing the whole command (report, don't
abort — the same stance `cairn sweep` takes for one bad entry), and
`--reviewer` overriding the per-tier default for every branch mailed, not
just one. `go build`, `go vet`, `gofmt -l`, `go test ./... -race`, and
`golangci-lint run ./...` all clean. Not run against a live `bd`/`gc` pair —
that's crn-0yv.5's smoke-test scope once this step is assembled into the
real formula, same as crn-0yv.2's own plan notes for its escalation half.

## Recovery step (ready for crn-0yv.5 to assemble into mol-cairn-librarian.formula.toml)

```toml
[[steps]]
id = "stale-review-branch-recovery"
title = "Reactively re-notify a stale review branch's reviewer, then escalate to a bd bead if still unactioned"
description = '''
Run "cairn stale-branches" (crn-0yv.1). It reactively re-mails the resolved
reviewer for any branch past --notify-after in-process (reusing crn-419.6's
reviewer resolution) and reports every branch's status as JSON — this step's
own job is only the escalate half: file a bd bead for each escalate-status
finding, deduplicated across cycles. This step never merges or rewrites a
review branch itself, per crn-xw3 guardrail 8 — only mails a reviewer
(delegated to the "cairn stale-branches" call above) or files a bead.

  STORE="${CAIRN_STORE:?CAIRN_STORE must be set}"
  FILED=0
  SKIPPED=0

  while IFS= read -r finding; do
    BRANCH=$(printf '%s' "$finding" | jq -r '.branch')
    ENTRY_ID=$(printf '%s' "$finding" | jq -r '.entry_id')
    TIER=$(printf '%s' "$finding" | jq -r '.tier')
    AGE_SECONDS=$(printf '%s' "$finding" | jq -r '.age_seconds')
    REVIEWER=$(printf '%s' "$finding" | jq -r '.reviewer')

    EXISTING=$(bd list --label=dim:review-branch,source:cairn-librarian \
      --title-contains="on $BRANCH (" --json --no-pager 2>/dev/null | jq -r '.[0].id // empty')

    if [ -n "$EXISTING" ]; then
      echo "skip $BRANCH: already tracked as $EXISTING"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    NEW_ID=$(bd create \
        --title="cairn librarian: stale review branch on $BRANCH ($TIER)" \
        --stdin --type=task --priority=3 \
        --labels=dim:review-branch,source:cairn-librarian --silent <<BODY
Review branch "$BRANCH" (entry $ENTRY_ID, tier $TIER) has been unmerged for
${AGE_SECONDS}s and was already sent a reminder mail (reviewer: $REVIEWER) by
this same cairn stale-branches pass. It is now past the escalate threshold
with no merge and no new commit.

Filed by the mol-cairn-librarian stale-review-branch-recovery step
(crn-0yv.1). This is a proposal only: it never merges the branch itself. To
resolve, $REVIEWER (or another reviewer) should review and merge the branch
into the store's default branch, or an author should update it — either
action removes the branch from future "cairn stale-branches" output.
BODY
    )
    echo "filed $NEW_ID for $BRANCH"
    FILED=$((FILED + 1))
  done < <(cairn stale-branches --store "$STORE" | jq -c '.[] | select(.status == "escalate")')

  echo "librarian stale-branch recovery: $FILED bead(s) filed, $SKIPPED already tracked"
'''
```

Notes on the step body itself, mirroring crn-0yv.2's own:

- Findings are consumed via process substitution (`done < <(...)`), not a
  trailing pipe into `while`, so `FILED`/`SKIPPED` survive loop exit (a
  piped `while read` runs in a bash subshell).
- No `set -e`: a single `bd create` failure doesn't abort the rest of the
  cycle, and a branch that failed to escalate this cycle simply gets another
  chance next cycle (it's still `escalate`-status, still unmerged).
- The reminder mail itself is **not** sent by this shell step — it already
  went out (or was attempted) inside the `cairn stale-branches` call on this
  same line, for every `notify`-status branch, before this step ever sees
  the JSON. This step only reads `.status == "escalate"` findings.
- The bead body is piped into `bd create --stdin` via a heredoc, not a
  `--description="..."` argument, so no manual shell-quoting of multi-line
  text is needed — same as crn-0yv.2's step.

## Scope note for crn-0yv.5

This bead delivers the detection+reactive-renotify logic
(`internal/cairn/branches.go`, `cmd/branches.go`, both implemented and
tested) and this ready-to-paste escalation step. Assembling the full
`mol-cairn-librarian.formula.toml` — this step plus crn-0yv.2's
freshness-drift step and crn-0yv.3's dedup/re-scope detection step,
tier-parameterized and smoke-tested end to end against a live `bd`/`gc`
pair — is crn-0yv.5's explicit scope, not duplicated here.
