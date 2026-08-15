# Release gate: entry content type enforcement

## Contract change

New `remember` writes require `type = "knowledge"` or `type = "remediation"`.
Single-entry callers pass `--type`; batch manifests carry a `type` field.
Missing and unknown values fail before an entry ID, file, commit, or review
branch is created. Declared `policy` is refused with guidance to put the rule in
the agent prompt.

Successful structured `remember` and batch results add a non-null `type` field;
`get` adds the persisted `type` while retaining legacy `kind` output during the
migration.

`NewEntry` enforces the same boundary independently of the CLI. Existing entries
with missing or historical type values remain readable. `cairn entries --type
knowledge|remediation|policy|unclassified` provides the store-wide structured
query used by unattended maintenance. `Entry.Anchor.Type` is unchanged.

`Entry.Kind` remains readable for compatibility but is retired from new
`remember` writes. Remediation behavior uses `Entry.Type`, with legacy `Kind` as
a fallback while old entries are migrated.

Go cannot tell whether a caller lied and labelled policy as knowledge.
Structurally inadmissible is not semantically impossible. An admission-time
classifier is the only thing that would close it, and that is a later decision,
not a gap to paper over now.

## Verification

```sh
go build ./...
go test ./... -race -count=1
golangci-lint run ./...
```

Verify that allowed types round-trip and are returned by `cairn entries`; legacy
unclassified entries remain readable; and missing, unknown, and policy writes
leave no file or review branch behind.
