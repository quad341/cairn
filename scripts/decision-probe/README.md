# Cairn decision probe

This is a cheap, paired probe for the step the ranker eval assumes away: when an
agent reaches a real pre-answer moment, does SessionStart knowledge make it
decide to consult Cairn rather than immediately re-derive the answer?

It also has a separate usefulness probe. That probe gives the model the actual
top search hit and asks whether it changes the next plan, does nothing, or would
actively mislead it.

## What it measures

- **Decision mode:** binary mention of consulting Cairn in one completion from
  an absent arm and one completion from a present arm. Both receive the same
  transcript prefix; only the present arm receives a real `cairn prime` payload
  and a short description of the available read commands.
- **Usefulness mode:** the model's stated operational effect of the actual
  top-ranked entry: `changes-plan`, `no-change`, or `actively-misleads`.

Every JSONL record contains the exact prompt, extracted response, complete raw
model output, model command, source path/line, identity, and deterministic score.
`audit.md` renders the raw prompts and responses for inspection. `summary.md`
contains aggregate and per-pair tables.

## What it does **not** measure

- It does not show that an agent would execute the stated plan or use a fetched
  entry correctly. There is no tool loop.
- Decision rate is not usefulness. An agent can decide to search and receive
  garbage; the usefulness mode is reported separately for that reason.
- Usefulness is a one-model judgment, not observed task success or human truth.
- One completion per arm is cheap but noisy. Pairing cancels task variance, not
  sampling variance or model-family habits.
- The default all-scope identity is a diagnostic convenience, not production
  scope behavior. Prefer pair-specific `identity` metadata in future eval sets
  (`identity_tags` or whitespace-delimited `identity`) or pass `--identity`
  when probing a homogeneous slice.
- Transcript reconstruction excludes hooks/system messages and clips old/large
  messages. It is a controlled prefix, not a byte-perfect session replay.
- The probe does not repair source-ancestry leakage in the eval pairs.

## Run

Never point it at the live store. Build Cairn and copy the store first:

```bash
go build -o /tmp/cairn-probe-bin .
STORE_COPY=$(mktemp -d /tmp/cairn-probe-store.XXXXXX)
cp -a /path/to/your/store/. "$STORE_COPY"

python3 scripts/decision-probe/decision_probe.py \
  --eval /path/to/evaluation.jsonl \
  --store "$STORE_COPY" \
  --cairn-bin /tmp/cairn-probe-bin \
  --output-dir /tmp/cairn-decision-probe \
  --mode both --ids synthetic-001 synthetic-002 synthetic-005
```

The default runner is Claude Haiku. The prompt is sent on stdin, so another
model family can be substituted without changing the harness:

```bash
... --model-command 'my-model-cli --temperature 0 --stdin'
```

The command must emit either plain response text or a JSON object with a string
`result` field (Claude CLI's `--output-format json` shape). It must perform one
completion and exit. The harness does not retry model failures, because a retry
would violate the one-completion-per-arm contract.

Scoring can be changed and audited without buying new completions:

```bash
python3 scripts/decision-probe/decision_probe.py \
  --rescore /tmp/cairn-decision-probe/results.jsonl
```

The default Claude adapter disables configured SessionStart hooks for the probe
process. Otherwise the absent arm could silently receive the machine's real
Cairn hook, invalidating the experiment.

## Scoring details

Decision generation is deliberately neutral: it asks only what to do next. It
does not ask “would you use Cairn?”, which would leak the treatment into the
absent arm. A fixed regex then recognizes explicit `cairn search/list/get/prime`
or an unambiguous statement such as “consult Cairn,” and stores the matching
line. Ambiguous “check memory/docs” language scores false and remains auditable.
Arm order is stably alternated by eval ID so neither treatment is systematically
first while repeated runs remain comparable.

Usefulness asks for one JSON classification. Invalid output is `unscored`, not
silently guessed. The top hit is used rather than the eval's expected entry so
the probe can expose harm from what the live ranker actually returns.
