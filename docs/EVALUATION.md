# Evaluating cairn without fooling ourselves

Cairn's product goal is that an agent stops re-solving a problem that has
already been solved. Retrieval quality is only a proxy for that goal. A high
recall score shows that a search can rank a designated entry; it does not show
that an agent chose to search, recognized the useful result, trusted it
appropriately, or avoided doing the work again.

This document defines the current evaluation method and its limits. It is a
methodology, not a claim that the product has achieved its goal.

## Privacy boundary

Evaluation inputs are private operational data. Transcripts, entry content,
identifiers, topic keys, generated work lists, and per-case results must not be
committed to this repository or pasted into public issues or pull requests.
Publishable artifacts are limited to the evaluation engine, schemas,
methodology, synthetic fixtures, and aggregate measurements that cannot reveal
individual cases.

Examples in this document are synthetic. Anyone applying this method should run
it against their own transcripts and store outside the source checkout.

## What we measure

The retrieval evaluation starts with pairs of:

```text
(situation an agent encountered, entry expected to help)
```

For each positive pair, run the situation as a search query and record the rank
of the expected entry. The primary retrieval metrics are:

- **Recall@k:** the fraction of pairs whose expected entry appears in the first
  `k` results. Report several useful cutoffs rather than choosing one after
  seeing the scores.
- **Mean reciprocal rank (MRR):** the mean of `1/rank` for the expected entry,
  with a miss contributing zero. MRR rewards putting a useful entry first, but
  still assumes that exactly one entry is correct.

These metrics answer a deliberately narrow question: *if the agent searched for
this situation, how highly did the designated entry rank?* They do not measure
the full product outcome.

## Build cases transcript-first

Mine the situation from what an agent actually encountered, then identify the
entry that should have helped. Do not select an entry first and invent a query
for it afterward.

Entry-first construction leaks the entry's vocabulary into the query. A lexical
retriever then scores near-perfectly even if real agents describe the same
problem differently. Such an evaluation certifies vocabulary reuse, not useful
retrieval, and can make a broken system look excellent.

A positive case should preserve the agent's language at the moment the problem
was encountered. Avoid rewriting it to include the expected entry's title,
topic key, or preferred terminology. Record enough provenance privately to
audit how the pair was selected, but publish only aggregate results.

Synthetic example:

```text
Situation: "The command says the change landed, but the running tool behaves as before."
Expected entry: synthetic-entry-01
```

The situation describes an observed recurrence; it is not a paraphrase written
from the expected entry.

## Source ancestry: independent recurrence versus reconstruction

Transcript-first sampling has a subtler contamination risk. Knowledge entries
are often written in response to an incident. If the query is mined from that
same incident, the entry may literally derive from the query's words. Ranking
it highly measures reconstruction of its source vocabulary, not retrieval on an
independent recurrence.

For every pair, compare the transcript timestamp with the entry's creation
timestamp and inspect the private ancestry evidence. Label the pair:

- **independent:** the situation is a later recurrence observed after the entry
  was created, with no evidence that it was used to author or revise the entry;
- **ancestry risk:** the situation predates or closely accompanies entry
  creation, may be the incident from which the entry was written, or otherwise
  cannot be shown to be independent.

Report metrics for these groups separately as well as in aggregate. Never hide
the labels after discovering contamination.

In the first evaluation set, 11 of 15 positive pairs carried ancestry risk.
When the groups were scored separately, their retrieval numbers were close.
The contamination was real, but its measured effect on that small set was
small. Both facts matter: ancestry must be controlled, and detecting a risk
does not justify claiming an effect that the measurements did not show.

## Negative cases

Positive-only evaluation cannot reveal whether search confidently offers
irrelevant knowledge. Include situations for which the store has no correct
answer and measure whether plausible-looking false positives appear in the top
results. Useful summaries include false-positive rate at the same `k` cutoffs
used for recall, plus a reviewed classification of the action suggested by each
top result.

Easy negatives—situations unrelated to anything in the store—mostly test that
the tokenizer works. Prefer **near-neighbour negatives**: situations sharing
vocabulary or context with an entry whose recommended action would nevertheless
be wrong. These exercise the dangerous failure mode: a result that looks
relevant enough to act on.

Synthetic example:

```text
Situation: "A local development build is stale after changing generated files."
Near neighbour: an entry about verifying a production deployment artifact.
Correct outcome: do not treat the production procedure as the answer.
```

## Three measurements, not one

Do not collapse these questions into a single retrieval score:

1. **Decision probe:** did the agent decide to consult cairn at the moment
   cached knowledge could help?
2. **Retrieval evaluation:** conditional on searching, did the relevant entry
   rank highly without unacceptable false positives?
3. **Usefulness evaluation:** after reading the entry, did the agent apply it
   correctly and avoid re-solving the problem?

A retriever cannot help when it is never invoked. An invoked retriever can rank
an entry that is stale, unclear, or operationally useless. Conversely, an agent
may benefit from a lower-ranked entry despite a miss under single-answer
scoring. Each stage needs its own evidence.

The strongest eventual outcome measure is recurrence-level: on a later instance
of a previously solved problem, compare whether the agent consults the cache,
time or tool use spent before the correct action, and whether it repeats the
original investigation. That evaluation is more expensive and more confounded
than offline retrieval, but it is closer to the product claim.

## Assumptions and known limits

Every report should carry its assumptions. At minimum:

- **Single-right-answer scoring is incomplete.** A pair designates one expected
  entry, but two entries may independently answer the situation. Standard
  recall under-credits the second valid answer unless relevance judgments are
  expanded.
- **Evaluation visibility may be optimistic.** The initial retrieval evaluation
  used an identity able to see the whole corpus. Real agents receive scoped
  slices. An expected entry that is absent or incorrectly scoped cannot be
  retrieved, so production recall is likely worse than the see-everything
  measurement.
- **Generalization is unproven.** A corpus and its transcripts represent one
  fleet's tasks, vocabulary, capture habits, and curation. Results do not
  establish performance for another fleet.
- **A single model is not a population.** Decision and usefulness probes run
  with one model may primarily measure that model's search habits, instruction
  following, or vocabulary. Repeat across relevant models and versions before
  generalizing.
- **Pair labels contain judgment.** The expected entry, ancestry label, and
  negative-case correctness are human or model judgments and can be wrong.
  Preserve review provenance privately and sample disagreements.
- **Offline queries omit interaction.** A real agent can reformulate a query,
  inspect several results, or use session context. One-shot ranking metrics do
  not capture that behavior.
- **Freshness and correctness are separate.** A fresh entry can be wrong; a
  stale entry can remain useful as a lead. Retrieval rank alone evaluates
  neither.
- **The decision probe is not yet the product outcome.** Choosing to search can
  increase tool use without reducing duplicated investigation. Usefulness and
  recurrence measurements must close that loop.

Mark assumptions as verified, contradicted, or still unverified when evidence
changes. Do not silently remove a false assumption from later reports.

## Overfitting and a temporal holdout

Every ranking change evaluated repeatedly against a fixed case set tunes the
system to that set, even without explicitly training on it. Once cases guide
field weights, tokenization, title backfills, or ranking rules, they no longer
provide an honest estimate of future performance.

Use a **temporal holdout**: after a tuning decision is frozen, evaluate on
situations that occurred later than the last tuning date. A live fleet produces
new incidents continuously, so the holdout refreshes itself. Cases may join the
development set after evaluation, but the next claim must use newer events.

This is better suited here than a permanent static split:

- it tests future vocabulary and problem distributions rather than a frozen
  snapshot;
- it naturally exercises new entries, changing scope, and corpus growth;
- it makes the boundary auditable with timestamps;
- it reduces the temptation to inspect and indirectly tune against a fixed
  secret set forever.

Temporal holdout does not solve every bias. Operational practices and models
can change across time, and recent incidents may be few. Record the tuning
cutoff, collection window, model/version, identity policy, corpus snapshot, and
sample sizes with every aggregate report.

## Minimum report shape

A publishable evaluation report should contain:

1. the tuning cutoff and evaluation window;
2. aggregate positive and negative sample counts;
3. independent and ancestry-risk counts;
4. recall at declared cutoffs and MRR, split by ancestry label;
5. near-neighbour negative false-positive rates;
6. decision-probe and usefulness results, clearly separate from retrieval;
7. identity/scoping policy, model/version, and corpus snapshot method;
8. known labeling disagreements and the assumption list above;
9. no private case content or identifiers.

If a measurement cannot support a product claim, say so next to the number.
The purpose of evaluation is not to produce a flattering score. It is to make
the remaining uncertainty visible enough to guide the next experiment.
