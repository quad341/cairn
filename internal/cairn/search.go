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
	Snippet   string        `json:"snippet"`
	Scope     []string      `json:"scope"`
	Freshness FreshnessInfo `json:"freshness"`
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
	for _, e := range visible {
		byID[e.ID] = e
	}

	db, err := openDB(store)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = db.Close() }()

	ranked, err := ftsRanked(ctx, db, terms)
	if err != nil {
		return SearchResult{}, err
	}

	result := SearchResult{
		Store:        store,
		Identity:     identity,
		Query:        query,
		QueryTerms:   terms,
		TotalVisible: len(visible),
		Hits:         []SearchHit{},
	}
	for _, r := range ranked {
		e, ok := byID[r.id]
		if !ok {
			// Matched the index but is not visible to this identity, or lost
			// its topic key to a more specific entry. Not an error: it is the
			// scope filter and the shadow resolver doing their job.
			continue
		}
		result.TotalMatched++
		if len(result.Hits) >= limit {
			continue
		}
		status, detail := Check(ctx, e)
		result.Hits = append(result.Hits, SearchHit{
			ID:        e.ID,
			TopicKey:  e.TopicKey,
			Title:     truncateRunes(e.Title, searchTitleCap),
			Summary:   truncateRunes(e.Summary, searchSummaryCap),
			HitCount:  e.HitCount,
			Score:     r.score,
			Snippet:   r.snippet,
			Scope:     e.Scope,
			Freshness: FreshnessInfo{Status: status, Detail: detail},
		})
	}
	return result, nil
}

// rankedRow is one FTS5 match, before visibility filtering.
type rankedRow struct {
	id      string
	score   float64
	snippet string
}

// ftsRanked runs the MATCH query and returns every hit, best first.
//
// It deliberately does not push the caller's limit into SQL. The visibility
// filter runs in Go over these rows, so a SQL LIMIT could return a page made
// entirely of entries the caller cannot see and report zero hits while
// relevant visible ones sat just past the cutoff. Store size here is
// hundreds to low thousands of entries and only matched rows are returned,
// so ranking the full match set is cheap.
func ftsRanked(ctx context.Context, db *sql.DB, terms []string) ([]rankedRow, error) {
	var total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM entries_fts`).Scan(&total); err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}

	// One lookup per term, so scoring knows exactly which terms each entry
	// matched. FTS5 can rank a whole OR expression in a single query, but it
	// will not tell you *why* a row matched, and "how many distinct
	// meaningful query terms does this entry contain" is precisely the
	// signal that separates a real answer from an entry that happened to
	// share one common word.
	matchedBy := map[string][]string{} // entry id -> terms it matched
	idf := map[string]float64{}
	for _, t := range terms {
		ids, err := idsMatching(ctx, db, t)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			continue
		}
		idf[t] = inverseDocFrequency(len(ids), total)
		for _, id := range ids {
			matchedBy[id] = append(matchedBy[id], t)
		}
	}
	if len(matchedBy) == 0 {
		return nil, nil
	}

	fields, err := entryFields(ctx, db)
	if err != nil {
		return nil, err
	}

	out := make([]rankedRow, 0, len(matchedBy))
	for id, hitTerms := range matchedBy {
		f := fields[id]
		var score float64
		for _, t := range hitTerms {
			// A term is worth its rarity, multiplied up when it lands in a
			// field that identifies the entry rather than merely mentioning
			// it. Summing over *distinct* terms is what makes an entry that
			// matches four of the query's words beat one that repeats a
			// single common word forty times -- the failure mode that made
			// raw BM25 over a sentence-length OR query score 6.7% recall@10
			// on the reference store.
			weight := 1.0
			if containsTerm(f.topicKey, t) {
				weight += topicKeyTermBoost
			}
			if containsTerm(f.title, t) {
				weight += titleTermBoost
			}
			score += idf[t] * weight
		}
		out = append(out, rankedRow{id: id, score: score, snippet: f.snippet(terms)})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out, nil
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
// The boosts themselves clearly earn their keep -- they nearly double
// recall@10 over pure IDF. Choosing between the three non-zero rows is a
// different matter: at n=15 they differ by one or two queries, which is
// noise. These are the best of the four measured, not a calibrated optimum.
// Re-tune when the eval set is larger; do not hand-tune them on 15 samples.
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

// idsMatching returns the ids of every entry matching a single term.
func idsMatching(ctx context.Context, db *sql.DB, term string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM entries_fts WHERE entries_fts MATCH ?`, buildFTSQuery([]string{term}))
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

// indexedFields is an entry's searchable text, as stored.
type indexedFields struct {
	topicKey string
	title    string
	summary  string
	body     string
}

// snippet returns a short window of body text around the first query term
// that occurs in it, so a caller can see *why* an entry matched without
// pulling the whole body.
//
// FTS5's own snippet() is not used: it is only available on a query that
// MATCHes, and scoring here is assembled from per-term lookups rather than
// one ranked MATCH.
func (f indexedFields) snippet(terms []string) string {
	const window = 160
	hay := strings.ToLower(f.body)
	for _, t := range terms {
		i := strings.Index(hay, t)
		if i < 0 {
			continue
		}
		start := max(i-window/2, 0)
		end := min(start+window, len(f.body))
		out := strings.Join(strings.Fields(f.body[start:end]), " ")
		if start > 0 {
			out = "..." + out
		}
		if end < len(f.body) {
			out += "..."
		}
		return out
	}
	return ""
}

// entryFields loads every entry's indexed text in one query. The store is
// hundreds to low thousands of entries, so reading them all costs less than
// issuing a second round of per-candidate lookups.
func entryFields(ctx context.Context, db *sql.DB) (map[string]indexedFields, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, topic_key, title, summary, body FROM entries_fts`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]indexedFields{}
	for rows.Next() {
		var id string
		var f indexedFields
		if err := rows.Scan(&id, &f.topicKey, &f.title, &f.summary, &f.body); err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rows.Err()
}

// containsTerm reports whether field contains term as a token.
//
// Matching is prefix-tolerant in both directions because the index is
// porter-stemmed but this comparison is not: the stored token for "merges"
// and the query token "merged" share only the stem "merg". Requiring four
// leading characters keeps that from collapsing short unrelated words into
// each other.
func containsTerm(field, term string) bool {
	if field == "" {
		return false
	}
	for _, ft := range ftsTokenPattern.FindAllString(strings.ToLower(field), -1) {
		if ft == term || (len(term) >= 4 && len(ft) >= 4 &&
			(strings.HasPrefix(ft, term) || strings.HasPrefix(term, ft))) {
			return true
		}
	}
	return false
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
