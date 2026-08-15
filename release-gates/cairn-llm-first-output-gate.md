# Release gate: LLM-first read output

This change alters the read-path output contract for `prime`, `search`, and
`list`. Their default is one minified JSON value with an instruction first and
only fields that can change the model's next action. `--pretty` emits the same
projection indented. The hidden deprecated `--json` compatibility flag is a
no-op on these commands, preventing both a fleet-wide unknown-flag failure and
an accidental pretty-print token tax. `get` remains raw Markdown by default.

Gate evidence:

- default output parses as JSON and contains exactly one trailing newline;
- `--json` is hidden and byte-identical to default;
- `--pretty` changes whitespace only;
- absent topic keys are explicit `null`, and empty arrays are `[]`;
- slim projections omit store/scope echoes, verbose freshness detail, and
  bookkeeping;
- contested `prime`, `search`, and `list` items preserve their conflict object;
- command tests, full tests, race tests, lint, and build pass after rebasing the
  search and shared topic-resolution changes.
