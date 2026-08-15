# Release gate: legacy entry classification backfill

## Contract change

`cairn backfill export` now emits every entry whose top-level `type` is absent,
not only entries whose retrieval metadata appears derived. Each JSONL record has
`original_type`, `original_kind`, `proposed_type`, and `needs_metadata`. An
external classifier must fill `proposed_type`; it only has to fill title and
summary when `needs_metadata` is true.

Entries already carrying a type are omitted on reruns. A legacy
`kind = "remediation"` is exported with `proposed_type = "remediation"`
automatically. Validate and apply reject any proposal that would pair that kind
with another type. Apply also treats changes to type or kind after export as a
stale proposal.

Migration may record `policy` so `cairn entries --type policy` can find entries
for unattended culling. This does not weaken new-entry admission: new policy
writes remain refused.

## Caller requirements

1. Fill `proposed_type` with `knowledge`, `remediation`, or `policy` for every
   exported record. Do not alter a mechanically populated remediation value.
2. Fill both retrieval-metadata proposals when `needs_metadata` is true. When it
   is false, omit both or supply both.
3. Run `backfill validate`, then inspect the structured dry-run from `backfill
   apply`. Writing remains an explicit `--write` operation.

## Verification

```sh
go test ./cmd ./internal/cairn
go test ./... -race -count=1
go build ./...
```

The regression suite uses synthetic entries and pins full-pass preservation of
legacy remediation type, kind, and auto-actionable eligibility. No exported
work list or proposal file belongs in version control; repository ignore rules
cover the documented filenames.
