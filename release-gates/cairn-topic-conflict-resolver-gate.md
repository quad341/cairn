# Release gate: shared topic-conflict resolver

## Contract change

`ResolveTopics` is now the single topic-precedence resolver for identity-filtered
read candidates. Override, scope specificity, `verified_at`, and `created_at`
still produce one winner. If all four signals tie, cairn returns every tied
revision with `conflict.reason = "indistinguishable"`; IDs order the report but
never select authority.

Callers must handle more than one resolved entry for a non-empty topic key and
must carry each `ResolvedEntry.Conflict` onto any list, prime, or search hit.
`Visible` keeps its existing signature as a compatibility projection.

## Verification

```sh
go build ./...
go test ./... -race -count=1
golangci-lint run
```

Resolver tests must prove every meaningful precedence level remains
single-winner, genuine ties retain and annotate every revision, and list/prime
surface the conflict on each affected item.
