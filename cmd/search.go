package cmd

import (
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

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
			return emitError(cmd, err)
		}
		res, err := cairn.Search(cmd.Context(), storePath(), query, identity, limit)
		if err != nil {
			return emitError(cmd, err)
		}

		if wantsJSON(cmd) {
			res.Hits = nonNil(res.Hits)
			return emitJSON(cmd.OutOrStdout(), res)
		}

		out := cmd.OutOrStdout()
		if len(res.Hits) == 0 {
			// Not an error. "Nothing matched" is a real, useful answer, and
			// an agent that gets a non-zero exit here may conclude cairn is
			// broken and stop consulting it -- which is far more expensive
			// than an empty result.
			_, _ = fmt.Fprintf(out, "# cairn search %q -- no candidates (%d entries in scope)\n",
				res.Query, res.TotalVisible)
			return nil
		}

		_, _ = fmt.Fprintf(out,
			"# cairn search %q -- %d of %d lexical candidates (%d entries in scope)\n",
			res.Query, len(res.Hits), res.TotalMatched, res.TotalVisible)
		for _, h := range res.Hits {
			topic := h.TopicKey
			if topic == "" {
				topic = cairn.UntopicedLabel
			}
			_, _ = fmt.Fprintf(out, "%s  %s  score:%.1f  %s\n", h.ID, topic, h.Score, h.Freshness.Status)
			if h.Summary != "" {
				_, _ = fmt.Fprintf(out, "  %s\n", h.Summary)
			} else if h.Title != "" {
				_, _ = fmt.Fprintf(out, "  %s\n", h.Title)
			}
			if h.Snippet != "" {
				_, _ = fmt.Fprintf(out, "  match: %s\n", collapseWhitespace(h.Snippet))
			}
		}
		if res.TotalMatched > len(res.Hits) {
			_, _ = fmt.Fprintf(out, "%d more candidate%s not shown; re-run with --limit\n",
				res.TotalMatched-len(res.Hits), plural(res.TotalMatched-len(res.Hits)))
		}
		// Conditional on relevance, deliberately. These are lexical
		// candidates: a query with no answer in the store still produces a
		// ranked list, and an unconditional "pull one" instruction turns that
		// into a confident wrong answer.
		_, _ = fmt.Fprintf(out,
			"If one of these addresses your situation, read it with `cairn get <id>`; "+
				"if none does, stop.\n")
		return nil
	},
}

// collapseWhitespace flattens a snippet to a single line. FTS5 snippets are
// cut from raw markdown bodies, so they routinely carry newlines and runs of
// indentation that would break the one-record-per-line shape the rest of this
// output holds to.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
