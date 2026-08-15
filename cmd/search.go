package cmd

import (
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
)

const searchInstruction = "Inspect these ranked lexical candidates. If one addresses your situation, " +
	"read it with cairn get <id> before acting. If none does, stop."

// searchNoneInstruction replaces the above when the abstain signal fires. It
// is a different instruction rather than a flag the model must interpret,
// because the two cases call for opposite behaviour and the whole point is
// that "cairn does not know this" must be as easy to act on as a hit.
const searchNoneInstruction = "No candidate covers enough of this query for the store to " +
	"plausibly hold the answer. Do not force a match from the list below. Solve it yourself, " +
	"then consider recording what you learn with cairn remember."

type searchOutput struct {
	Instruction  string   `json:"instruction"`
	Query        string   `json:"query"`
	QueryTerms   []string `json:"query_terms"`
	TotalMatched int      `json:"total_matched"`
	TotalVisible int      `json:"total_visible"`
	// Confidence lets a caller act on the abstain signal structurally rather
	// than by parsing the instruction string.
	Confidence searchConfidence `json:"confidence"`
	Hits       []searchItem     `json:"hits"`
}

// searchConfidence is the model-facing projection of cairn.SearchConfidence.
// Coverage travels with the verdict so a caller can see how marginal a call
// was, rather than only which side of the threshold it landed on.
type searchConfidence struct {
	Verdict        string   `json:"verdict"`
	Coverage       float64  `json:"coverage"`
	UnmatchedTerms []string `json:"unmatched_terms"`
}

type searchItem struct {
	ID        string               `json:"id"`
	TopicKey  *string              `json:"topic_key"`
	Title     string               `json:"title"`
	Summary   string               `json:"summary"`
	Score     float64              `json:"score"`
	Snippet   string               `json:"snippet"`
	Freshness string               `json:"freshness"`
	Conflict  *cairn.TopicConflict `json:"conflict"`
}

func projectSearch(result cairn.SearchResult) searchOutput {
	hits := make([]searchItem, 0, len(result.Hits))
	for _, hit := range result.Hits {
		var topicKey *string
		if hit.TopicKey != "" {
			key := hit.TopicKey
			topicKey = &key
		}
		hits = append(hits, searchItem{
			ID: hit.ID, TopicKey: topicKey, Title: hit.Title, Summary: hit.Summary,
			Score: hit.Score, Snippet: collapseWhitespace(hit.Snippet),
			Freshness: hit.Freshness.Status, Conflict: hit.Conflict,
		})
	}
	instruction := searchInstruction
	if result.Confidence.Verdict == cairn.VerdictNone {
		instruction = searchNoneInstruction
	}
	return searchOutput{
		Instruction: instruction, Query: result.Query,
		QueryTerms: nonNil(result.QueryTerms), TotalMatched: result.TotalMatched,
		TotalVisible: result.TotalVisible,
		Confidence: searchConfidence{
			Verdict:        result.Confidence.Verdict,
			Coverage:       result.Confidence.Coverage,
			UnmatchedTerms: nonNil(result.Confidence.UnmatchedTerms),
		},
		Hits: hits,
	}
}

func init() {
	searchCmd.Flags().Int("limit", 0,
		"maximum hits to return (0 uses the built-in default)")
	rootCmd.AddCommand(searchCmd)
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Rank visible entries against free text (identity-scoped, lexical)",
	Long: `Find entries by describing the situation, not by naming them.

list and get both require you to already know what you are looking for: list
needs the exact topic key some other agent chose, get needs an ID. search does
not -- pass the problem in your own words:

  cairn search "agent process disappeared after the rig restarted"

Ranking is lexical: entries are scored by how many distinct, rare query terms
they contain, weighted up when a term lands in the topic key or title. Scoring
runs over your identity's scope only.

That means results are ranked CANDIDATES, not answers. Lexical overlap is not
relevance -- a query whose answer is not in the store still returns whatever
overlapped most. Read the summaries, and pull a body with 'cairn get <id>' only
if a candidate actually addresses your situation. If none does, stop; do not
force a match.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Accept the query as multiple words so an agent can write
		// `cairn search why did the build fail` without quoting. Joining is
		// safe: buildFTSQuery tokenizes anyway, so word boundaries are all
		// that survive either way.
		query := strings.Join(args, " ")
		limit, _ := cmd.Flags().GetInt("limit")

		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitModelError(cmd, err)
		}
		res, err := cairn.Search(cmd.Context(), storePath(), query, identity, limit)
		if err != nil {
			return emitModelError(cmd, err)
		}
		logSearchOutcome(cmd, identity, res)
		return emitModelJSON(cmd, projectSearch(res))
	},
}

// collapseWhitespace flattens a snippet to a single line. FTS5 snippets are
// cut from raw markdown bodies, so they routinely carry newlines and runs of
// indentation that would break the one-record-per-line shape the rest of this
// output holds to.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// logSearchOutcome records the invocation for later audit. Logged before the
// result is rendered, so the record exists identically regardless of output
// mode -- an audit of what agents searched for must not depend on which flags
// they happened to pass.
func logSearchOutcome(cmd *cobra.Command, identity []string, res cairn.SearchResult) {
	ids := make([]string, len(res.Hits))
	scores := make([]float64, len(res.Hits))
	for i, h := range res.Hits {
		ids[i] = h.ID
		scores[i] = h.Score
	}
	obslog.FromContext(cmd.Context()).SearchOutcome(obslog.SearchOutcomeFields{
		IdentityTags:   identity,
		RunID:          resolveRunID(cmd),
		Query:          res.Query,
		Terms:          res.QueryTerms,
		UnmatchedTerms: res.Confidence.UnmatchedTerms,
		Verdict:        res.Confidence.Verdict,
		Coverage:       res.Confidence.Coverage,
		TotalMatched:   res.TotalMatched,
		TotalVisible:   res.TotalVisible,
		HitIDs:         ids,
		HitScores:      scores,
	})
}
