package cairn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// searchFTSTable is the FTS5 virtual table backing Search. Like entry_tags
// (see entriesSchema's comment) it carries no index-only state, so a reindex
// drops and recreates it wholesale rather than upserting into it.
//
// id is UNINDEXED: it is carried so a match can be joined back to entries,
// not so an agent can full-text search for an ID -- IDs are exact-lookup
// keys and `cairn get <id>` already serves that. It still occupies a column
// position, so bm25() below must supply a weight for it.
//
// The porter stemmer wraps unicode61 so an agent describing a situation in
// its own words ("the process got killed") matches an entry written in
// another tense or number ("processes are killed on rig restart"). Without
// stemming, natural-language recall degrades to near-exact word matching,
// which is the failure this command exists to fix.
const searchFTSSchema = `
CREATE VIRTUAL TABLE entries_fts USING fts5(
  id UNINDEXED,
  topic_key,
  title,
  summary,
  body,
  tokenize = 'porter unicode61'
);
`

// defaultSearchLimit is how many hits Search returns when the caller does
// not say. Sized for an agent's working context: enough that the right entry
// is likely present, few enough that reading all of them is cheaper than
// re-deriving the answer.
const defaultSearchLimit = 8

// searchTitleCap and searchSummaryCap bound a hit's projected Title and
// Summary.
//
// validate.go's titleCap/summaryCap bound what a *new* entry may be written
// with. They do not bound what is already in the store: entries predating
// those caps, and entries written straight to disk bypassing cmd/remember,
// routinely carry far more. The reference store holds summaries up to 3531
// characters, and one of those in an 8-hit result set is larger than
// everything else combined -- it would crowd out the other seven hits in the
// context window of the agent this output exists to serve.
//
// So Search projects its own display-length ceiling rather than trusting the
// stored value. The body stays whole on disk; `cairn get <id>` is how you
// read it.
const (
	searchTitleCap   = 120
	searchSummaryCap = 240
)

// SearchHit is one ranked entry from Search.
type SearchHit struct {
	ID string `json:"id"`
	// TopicKey, Summary and Snippet are emitted even when empty. omitempty
	// would leave a machine consumer unable to distinguish "this entry has no
	// topic key" from "this build of cairn does not report topic keys" -- a
	// distinction that matters when the consumer is deciding whether to fall
	// back to `cairn list`.
	TopicKey string  `json:"topic_key"`
	Title    string  `json:"title"`
	Summary  string  `json:"summary"`
	HitCount int     `json:"hit_count"`
	Score    float64 `json:"score"`
	// Snippet is the matched text in context, with the matching terms
	// unmarked -- an agent does not need highlight markers, and they would
	// only cost tokens.
	Snippet   string         `json:"snippet"`
	Scope     []string       `json:"scope"`
	Freshness FreshnessInfo  `json:"freshness"`
	Conflict  *TopicConflict `json:"conflict,omitempty"`
}

// SearchResult is Search's structured output.
type SearchResult struct {
	Store    string   `json:"store"`
	Identity []string `json:"identity"`
	Query    string   `json:"query"`
	// QueryTerms is what was actually searched for, after stopword and
	// common-term pruning. Surfaced because a caller that gets a bad result
	// otherwise has no way to tell a ranking failure from its query having
	// been pruned down to nothing useful -- and because an agent reading its
	// own miss can reformulate if it can see which of its words counted.
	QueryTerms []string    `json:"query_terms"`
	Hits       []SearchHit `json:"hits"`
	// TotalMatched is how many visible entries matched before Limit was
	// applied, so a caller can tell "nothing matched" from "a lot matched
	// and you are seeing the top few".
	TotalMatched int `json:"total_matched"`
	// TotalVisible is the size of the caller's scope, for the same reason
	// Prime reports it: it distinguishes an empty result caused by a narrow
	// query from one caused by an identity that sees almost nothing.
	TotalVisible int `json:"total_visible"`
	// Confidence is the abstain signal over the returned page.
	Confidence SearchConfidence `json:"confidence"`
}

// Search ranks the caller's visible entries against a free-text query using
// SQLite FTS5 and BM25.
//
// It exists because every other read path requires the agent to already know
// what it is looking for: `list` needs the exact topic_key another agent
// chose, `get` needs an ID. On the reference store that produced 500 entries
// of which 85% had never been read once, the only entries with meaningful
// recall were the handful whose topic_key was guessable from the situation.
// Search removes the guess.
//
// Identity filtering happens before ranking, not after: Visible() applies the
// same scope filter and topic-key shadow resolution that map/prime/list
// apply, and only entries that survive it can be ranked. A shadowed or
// out-of-scope entry can therefore never occupy a result slot, and Search can
// never disagree with `list` about which entry wins a topic key.
func Search(ctx context.Context, store, query string, identity []string, limit int) (SearchResult, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	terms := queryTerms(query)
	if len(terms) == 0 {
		return SearchResult{}, fmt.Errorf("search query has no searchable terms: %q", query)
	}
	if err := ensureSearchIndex(ctx, store); err != nil {
		return SearchResult{}, err
	}

	visible, err := Visible(ctx, store, identity)
	if err != nil {
		return SearchResult{}, err
	}
	byID := make(map[string]*Entry, len(visible))
	conflictsByID := make(map[string]*TopicConflict)
	for _, resolved := range ResolveTopics(visible).Entries {
		conflictsByID[resolved.Entry.ID] = resolved.Conflict
	}
	visibleIDs := make(map[string]bool, len(visible))
	for _, e := range visible {
		byID[e.ID] = e
		visibleIDs[e.ID] = true
	}

	db, err := openDB(store)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = db.Close() }()

	termIDF := map[string]float64{}
	ranked, err := ftsRanked(ctx, db, terms, visibleIDs, &termIDF)
	if err != nil {
		return SearchResult{}, err
	}
	confidence := confidenceFor(ranked, terms, termIDF)
	// Captured before paging: TotalMatched reports how many visible entries
	// matched, not how many fit on this page.
	totalMatched := len(ranked)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	pageIDs := make([]string, len(ranked))
	for i, r := range ranked {
		pageIDs[i] = r.id
	}
	snippets, err := snippetsFor(ctx, db, pageIDs, terms)
	if err != nil {
		return SearchResult{}, err
	}

	result := SearchResult{
		Store:        store,
		Identity:     identity,
		Query:        query,
		QueryTerms:   terms,
		TotalMatched: totalMatched,
		TotalVisible: len(visible),
		Confidence:   confidence,
		Hits:         []SearchHit{},
	}
	for _, r := range ranked {
		e := byID[r.id]
		status, detail := Check(ctx, e)
		result.Hits = append(result.Hits, SearchHit{
			ID:        e.ID,
			TopicKey:  e.TopicKey,
			Title:     truncateRunes(e.Title, searchTitleCap),
			Summary:   truncateRunes(e.Summary, searchSummaryCap),
			HitCount:  e.HitCount,
			Score:     r.score,
			Snippet:   snippets[e.ID],
			Scope:     e.Scope,
			Freshness: FreshnessInfo{Status: status, Detail: detail},
			Conflict:  conflictsByID[e.ID],
		})
	}
	return result, nil
}

// rankedRow is one scored entry, already known to be visible to the caller.
type rankedRow struct {
	id    string
	score float64
	// matchedIDF is the summed IDF of the distinct query terms this entry
	// actually contains. Carried alongside the score because the score alone
	// cannot be compared across queries -- see SearchConfidence.
	matchedIDF float64
}

// termHit records where in an entry a query term landed. Field membership
// comes from FTS5 itself rather than from string comparison in Go, so it uses
// exactly the same porter stemming as the index: "merges" in a title really
// does register for the query term "merged", and "database-migration" really
// does not register for "data".
type termHit struct {
	topicKey bool
	title    bool
}

// ftsRanked scores every visible entry matching any of terms, best first.
//
// Both the ranking and the corpus statistics behind it are computed over the
// caller's visible set alone. That is stronger than filtering results
// afterwards: document frequency, and therefore every term's IDF, is a
// property of the corpus it is measured against, so including entries the
// caller cannot see would let an invisible entry depress a term's weight and
// silently reorder the visible ones. Filtering after ranking prevents hidden
// entries from taking result slots but not from changing the order of the
// slots that remain.
func ftsRanked(ctx context.Context, db *sql.DB, terms []string, visible map[string]bool, termIDF *map[string]float64) ([]rankedRow, error) {
	total := len(visible)
	if total == 0 {
		return nil, nil
	}
	matched, idf, err := collectTermHits(ctx, db, terms, visible, total)
	if err != nil {
		return nil, err
	}
	*termIDF = idf

	out := make([]rankedRow, 0, len(matched))
	for id, hits := range matched {
		var score, matchedIDF float64
		for t, h := range hits {
			// A term is worth its rarity, multiplied up when it lands in a
			// field that identifies the entry rather than merely mentioning
			// it. Summing over *distinct* terms is what makes an entry
			// matching four of the query's words beat one repeating a single
			// common word forty times -- the failure mode that made raw BM25
			// over a sentence-length OR query score 6.7% recall@10 on the
			// reference store.
			weight := 1.0
			if h.topicKey {
				weight += topicKeyTermBoost
			}
			if h.title {
				weight += titleTermBoost
			}
			score += idf[t] * weight
			matchedIDF += idf[t]
		}
		out = append(out, rankedRow{id: id, score: score, matchedIDF: matchedIDF})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out, nil
}

// collectTermHits resolves, for every query term, which visible entries
// contain it and where -- plus that term's IDF over the visible corpus.
func collectTermHits(ctx context.Context, db *sql.DB, terms []string, visible map[string]bool, total int) (
	map[string]map[string]termHit, map[string]float64, error,
) {
	matched := map[string]map[string]termHit{} // entry id -> term -> where it hit
	idf := map[string]float64{}
	for _, t := range terms {
		ids, err := idsMatching(ctx, db, "", t)
		if err != nil {
			return nil, nil, err
		}
		ids = retainVisible(ids, visible)
		if len(ids) == 0 {
			continue
		}
		idf[t] = inverseDocFrequency(len(ids), total)

		topicHits, err := idSet(ctx, db, "topic_key", t)
		if err != nil {
			return nil, nil, err
		}
		titleHits, err := idSet(ctx, db, "title", t)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range ids {
			if matched[id] == nil {
				matched[id] = map[string]termHit{}
			}
			matched[id][t] = termHit{topicKey: topicHits[id], title: titleHits[id]}
		}
	}
	return matched, idf, nil
}

// SearchConfidence is normalized evidence about whether the store plausibly
// knows the answer at all.
//
// It exists because a raw score cannot answer that question. Scores are sums
// of IDF and are therefore query-dependent: a long query over rare terms
// produces large scores whether or not any entry is a real answer, and a
// short query over common terms produces small ones even when the top hit is
// exactly right. Measured on the reference store, the top hit for a query
// with NO correct answer scored at or above the median score of genuinely
// correct entries in 2 of 5 cases.
//
// Coverage is a ratio and therefore comparable across queries: of the query's
// total meaningful (IDF) weight, how much does the top hit actually contain.
//
// MEASURED SEPARATION, on 46 held-out pairs never used to tune ranking
// (23 where a correct entry was retrieved, 23 where none existed or none
// surfaced): coverage AUC 0.694. Real signal, but weak -- it supports saying
// "probably nothing here", and does not support ranking confidence tiers.
//
// A margin field (how far top-1 leads top-2) was implemented and measured at
// AUC 0.501 -- indistinguishable from noise -- and removed. Do not reintroduce
// it without measuring it first; it is an intuitive idea that simply does not
// work on this corpus.
type SearchConfidence struct {
	Coverage float64 `json:"coverage"`
	// UnmatchedTerms are query terms that match nothing anywhere in the
	// store. Reported separately from Coverage because they are evidence
	// about the STORE's gaps rather than about this entry -- a caller
	// deciding whether to write a new entry wants to see them.
	UnmatchedTerms []string `json:"unmatched_terms"`
	// Verdict is the abstain signal. Deliberately a first-class field rather
	// than something a caller infers from prose, because the honest answer
	// "cairn does not know this" is otherwise unreachable: search always
	// returns a ranked list, and an agent with no way to express "nothing
	// here" will pick the top row.
	//
	// Only two values, and that is a measurement result rather than a
	// simplification: the data supports flagging hopeless queries and does
	// not support asserting that any hit is a good one. An earlier revision
	// had a "strong" tier; it fired once in 23 genuine retrievals, because
	// it gated on the noise feature described above.
	Verdict string `json:"verdict"`
}

// Verdict values for SearchConfidence.
const (
	// VerdictNone: the top hit covers so little of the query that the store
	// probably has no answer.
	VerdictNone = "none"
	// VerdictCandidates: ranked lexical candidates, with no claim that any
	// of them is correct.
	VerdictCandidates = "candidates"
)

// noneCoverage is the abstain threshold, fitted on the 46 held-out pairs
// described above. Chosen as the operating point that abstains on a useful
// share of hopeless queries while never suppressing a retrieval that
// succeeded:
//
//	threshold   correct abstain   wrong abstain
//	     0.15                0%              0%
//	     0.18               22%              0%     <- shipped
//	     0.20               30%             13%
//	     0.25               43%             17%
//	     0.30               70%             43%
//
// The asymmetry is deliberate. A miss costs a re-derivation; a suppressed
// correct answer costs a re-derivation AND teaches the agent that cairn has
// nothing, which is the more expensive error. Raise this only against a
// fresh held-out set -- this one has now been used for fitting and can no
// longer measure it honestly.
var noneCoverage = 0.18

// confidenceFor derives the abstain signal from the ranked page.
func confidenceFor(ranked []rankedRow, terms []string, idf map[string]float64) SearchConfidence {
	c := SearchConfidence{Verdict: VerdictNone, UnmatchedTerms: []string{}}
	for _, t := range terms {
		if _, ok := idf[t]; !ok {
			c.UnmatchedTerms = append(c.UnmatchedTerms, t)
		}
	}
	if len(ranked) == 0 {
		return c
	}
	var totalIDF float64
	for _, w := range idf {
		totalIDF += w
	}
	if totalIDF > 0 {
		// Denominator is the weight of terms the STORE knows, not of every
		// term the caller typed. A term matching nothing anywhere cannot be
		// held against the top hit -- no entry could have covered it -- and
		// it is reported separately in UnmatchedTerms instead. This is also
		// the denominator the shipped threshold was fitted against; changing
		// it silently invalidates that fit.
		c.Coverage = ranked[0].matchedIDF / totalIDF
	}
	if c.Coverage >= noneCoverage {
		c.Verdict = VerdictCandidates
	}
	return c
}

// retainVisible drops ids the caller cannot see.
func retainVisible(ids []string, visible map[string]bool) []string {
	out := ids[:0]
	for _, id := range ids {
		if visible[id] {
			out = append(out, id)
		}
	}
	return out
}

// topicKeyTermBoost and titleTermBoost multiply a term's IDF when it appears
// in that field.
//
// Field boosting lives here, in Go, rather than in bm25()'s per-column
// weights, because those weights are close to inert: measured on the
// reference store, raising the topic_key weight from 8 to 800 -- a factor of
// 100 -- moved a row's score by 6%. bm25() normalizes per column against that
// column's average length and the effect swamps the multiplier. An explicit
// boost is both stronger and legible: you can read these two numbers and know
// what the ranker prefers.
//
// A topic_key match outranks a title match because topic keys are
// curator-normalized -- a term hitting one is nearly an exact-lookup hit.
//
// Measured on a 500-entry reference store against a 15-pair eval set:
//
//	boosts        recall@1  recall@10   MRR
//	0.0 / 0.0        0.0%      33.3%   0.109
//	3.0 / 1.5       26.7%      60.0%   0.356
//	6.0 / 3.0       33.3%      60.0%   0.390
//	10.0 / 4.0      26.7%      60.0%   0.352
//
// Read that as: field boosting itself clearly earns its keep, nearly doubling
// recall@10 over pure IDF. The choice *among* the non-zero rows does not --
// every one of them reaches the same recall@10, and 6.0/3.0 wins the earlier
// ranks by one or two queries out of fifteen, which is noise. These are
// provisional defaults selected in-sample, not a calibrated optimum, and the
// 60% is development-set performance rather than an expectation about new
// queries. Re-tune when the eval set is larger; do not hand-tune on n=15.
var (
	topicKeyTermBoost = 6.0
	titleTermBoost    = 3.0
)

// inverseDocFrequency is the BM25 IDF of a term appearing in df of total
// documents. A term in almost every entry approaches zero weight, which is
// what lets domain words like "branch" or "agent" stay in the query --
// contributing when they co-occur with rarer terms, without being able to
// carry a match on their own. Deleting such terms outright, which an earlier
// revision did, silently made entries whose topic key contained them
// unreachable.
func inverseDocFrequency(df, total int) float64 {
	return math.Log(1 + (float64(total)-float64(df)+0.5)/(float64(df)+0.5))
}

// idsMatching returns the ids of every entry matching a single term. An empty
// column searches all of them; otherwise the match is confined to that
// column.
func idsMatching(ctx context.Context, db *sql.DB, column, term string) ([]string, error) {
	match := buildFTSQuery([]string{term})
	if column != "" {
		// column is always one of this file's own literals, never caller
		// input, so it cannot inject FTS5 syntax here.
		match = column + ":" + match
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM entries_fts WHERE entries_fts MATCH ?`, match)
	if err != nil {
		return nil, fmt.Errorf("search query failed for term %q: %w", term, err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// idSet is idsMatching as a set, for membership tests.
func idSet(ctx context.Context, db *sql.DB, column, term string) (map[string]bool, error) {
	ids, err := idsMatching(ctx, db, column, term)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// snippetsFor returns a short window of body text around a query term, for
// the handful of entries actually being returned.
//
// Bodies are loaded here, after ranking and limiting, rather than during
// scoring: scoring needs only which terms an entry matched, which FTS5
// already knows. Loading every body to score a query would read the whole
// store on every search -- tolerable at 500 entries, roughly 30 MB per query
// at 10,000.
func snippetsFor(ctx context.Context, db *sql.DB, ids, terms []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	// The concatenated segment is always literal "?" placeholders, one per
	// id; the ids themselves are parameterized through args.
	//nolint:gosec // placeholders are literal "?"s; values are parameterized
	rows, err := db.QueryContext(ctx,
		`SELECT id, body FROM entries_fts WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		out[id] = bodySnippet(body, terms)
	}
	return out, rows.Err()
}

// bodySnippet cuts a readable window around the first query term occurring in
// body.
//
// FTS5's own snippet() is not used: it is only available on a query that
// MATCHes, and scoring here is assembled from per-term lookups rather than one
// ranked MATCH.
func bodySnippet(body string, terms []string) string {
	const window = 160
	hay := strings.ToLower(body)
	for _, t := range terms {
		i := strings.Index(hay, t)
		if i < 0 {
			continue
		}
		start := max(i-window/2, 0)
		end := min(start+window, len(body))
		out := strings.Join(strings.Fields(body[start:end]), " ")
		if start > 0 {
			out = "..." + out
		}
		if end < len(body) {
			out += "..."
		}
		return out
	}
	return ""
}

// ftsTokenPattern matches the runs of characters FTS5's unicode61 tokenizer
// would itself treat as tokens.
var ftsTokenPattern = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// englishStopwords are function words that carry no discriminating power.
//
// They matter far more here than in ordinary search because queries are whole
// sentences an agent wrote about its situation, and every term is OR-ed: one
// "the" in the query pulls in every entry in the store. Measured on the
// reference store, unpruned queries matched 366 of ~500 entries, which left
// BM25 ranking noise against noise -- recall@10 was 6.7%.
var englishStopwords = map[string]bool{
	"a": true, "about": true, "after": true, "again": true, "all": true,
	"also": true, "am": true, "an": true, "and": true, "any": true,
	"are": true, "as": true, "at": true, "be": true, "because": true,
	"been": true, "before": true, "being": true, "but": true, "by": true,
	"can": true, "did": true, "do": true, "does": true, "doing": true,
	"done": true, "for": true, "from": true, "get": true, "had": true,
	"has": true, "have": true, "he": true, "her": true, "here": true,
	"him": true, "his": true, "how": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "just": true,
	"me": true, "more": true, "most": true, "my": true, "no": true,
	"not": true, "now": true, "of": true, "on": true, "one": true,
	"only": true, "or": true, "other": true, "our": true, "out": true,
	"over": true, "own": true, "ptr": true, "same": true, "she": true,
	"should": true, "so": true, "some": true, "still": true, "such": true,
	"than": true, "that": true, "the": true, "their": true, "them": true,
	"then": true, "there": true, "these": true, "they": true, "this": true,
	"those": true, "through": true, "to": true, "too": true, "under": true,
	"until": true, "up": true, "very": true, "was": true, "we": true,
	"were": true, "what": true, "when": true, "where": true, "which": true,
	"while": true, "who": true, "why": true, "will": true, "with": true,
	"would": true, "you": true, "your": true,
}

// contractionFragments are the trailing halves of English contractions.
// unicode61 splits "don't" into "don" and "t"; the "t" is dropped as too
// short, but "don" survives as a three-letter token that looks like a real
// word and matches a meaningful share of any English corpus. Same for the
// rest of the family.
var contractionFragments = map[string]bool{
	"don": true, "doesn": true, "didn": true, "isn": true, "wasn": true,
	"aren": true, "weren": true, "couldn": true, "wouldn": true,
	"shouldn": true, "won": true, "hasn": true, "haven": true, "hadn": true,
	"ve": true, "ll": true, "re": true, "im": true, "id": true,
}

// queryTerms tokenizes free text into candidate search terms.
//
// The caller's text is never passed through as FTS5 syntax. An agent
// describing a real situation writes things like `cairn get "foo" -- why
// NULL?`, and FTS5 would read the quotes, the double hyphen and the colon as
// operators and fail the query outright, or worse, silently mean something
// else. So the input is tokenized the same way unicode61 would tokenize it
// and each token is later quoted as a literal string.
func queryTerms(raw string) []string {
	tokens := ftsTokenPattern.FindAllString(raw, -1)
	out := make([]string, 0, len(tokens))
	seen := map[string]bool{}
	for _, t := range tokens {
		lower := strings.ToLower(t)
		// Single characters carry almost no discriminating power and only
		// inflate the OR set.
		if len([]rune(lower)) < 2 || englishStopwords[lower] ||
			contractionFragments[lower] || seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, lower)
	}
	return out
}

// buildFTSQuery assembles pruned terms into an FTS5 MATCH expression.
//
// OR rather than AND is deliberate. A situation description is mostly words
// that do not appear in the entry that answers it; requiring all of them to
// match would return nothing for exactly the natural-language queries this
// command exists to serve. Pruning is what keeps OR from matching everything.
func buildFTSQuery(terms []string) string {
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		// A token can never contain a double quote -- ftsTokenPattern
		// matches only letters, digits and underscore -- but quote-doubling
		// is the correct escape for an FTS5 string literal and costs
		// nothing to keep should the pattern ever widen.
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// ensureSearchIndex guarantees entries_fts exists and is populated.
//
// ensureFresh only rebuilds when the store's git watermark moved, so an
// index.sqlite built by a cairn binary that predates search is "fresh" by
// that measure and yet has no FTS table at all -- every existing deployment
// is in exactly that state on first upgrade. Detecting the missing table and
// forcing one rebuild is what makes search work on those stores without
// requiring an operator to run `cairn reindex` by hand.
func ensureSearchIndex(ctx context.Context, store string) error {
	if err := ensureFresh(ctx, store); err != nil {
		return err
	}
	present, err := searchIndexPresent(ctx, store)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	_, err = Reindex(ctx, store)
	return err
}

func searchIndexPresent(ctx context.Context, store string) (bool, error) {
	db, err := openDB(store)
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()

	var name string
	err = db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='entries_fts'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// rebuildSearchIndexTx drops and repopulates entries_fts inside the caller's
// reindex transaction. Both statements run in that transaction for the same
// reason entry_tags' DROP/CREATE pair does: two concurrent Reindex calls
// then serialize on SQLite's single-writer lock instead of interleaving and
// one of them failing with "table entries_fts already exists" (crn-j3k4).
func rebuildSearchIndexTx(ctx context.Context, tx *sql.Tx, entries []*Entry) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS entries_fts;`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, searchFTSSchema); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entries_fts (id, topic_key, title, summary, body) VALUES (?,?,?,?,?)`,
			e.ID, e.TopicKey, e.Title, e.Summary, e.Body,
		); err != nil {
			return err
		}
	}
	return nil
}
