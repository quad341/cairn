# Release gate: entry metadata provenance

## Contract change

New entries persist `title_source` and `summary_source` as `authored` when the
caller supplied the field and `derived` when cairn synthesized it from the body.
Legacy entries without either field remain valid. Both fields are indexed and
available on index-backed `Entry` reads.

`remember` and per-line batch results now include a `metadata` object with both
sources and actionable warnings for derived fields. Omission remains nonbreaking;
plain output warns but the write still succeeds. Batch summaries also report a
`derived_metadata` count for successful entries where either field was derived.

Existing indexes must be rebuilt once after deployment so the additive
`title_source` and `summary_source` columns are created and populated:

```sh
cairn reindex --store <store>
```

## Verification

```sh
go build ./...
go test ./... -race -count=1
golangci-lint run
```

Verify an explicit-title write records `authored`, an omitted-title write records
`derived`, legacy frontmatter parses with an empty source, and `Status` returns
the indexed provenance.
