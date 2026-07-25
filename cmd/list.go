package cmd

import (
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

// ListResult is one entry in cairn list <topic> --json's output. Bare
// array, no wrapper object -- mirrors status --json's convention since list,
// like status, has no query-context metadata worth echoing beyond what the
// topic argument itself already carries.
type ListResult struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	Summary   string              `json:"summary"`
	Scope     []string            `json:"scope"`
	Freshness cairn.FreshnessInfo `json:"freshness"`
}

var listCmd = &cobra.Command{
	Use:   "list <topic>",
	Short: "Exact topic-to-entry lookup: visible winner(s) for a topic key (identity-scoped)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		topic := args[0]
		key := topic
		if topic == cairn.UntopicedLabel {
			key = ""
		}
		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitError(cmd, err)
		}
		rows, err := cairn.ListByTopic(cmd.Context(), storePath(), key, identity)
		if err != nil {
			return emitError(cmd, err)
		}
		if len(rows) == 0 {
			return emitError(cmd, classifiedErr(CategoryNotFound, topic, fmt.Errorf("no entries found for topic %q", topic)))
		}

		if wantsJSON(cmd) {
			items := make([]ListResult, 0, len(rows))
			for _, r := range rows {
				items = append(items, ListResult{
					ID:        r.ID,
					Title:     r.Title,
					Summary:   r.Summary,
					Scope:     nonNil(r.Scope),
					Freshness: cairn.FreshnessInfo{Status: r.FreshnessState, Detail: r.FreshnessDetail},
				})
			}
			return emitJSON(cmd.OutOrStdout(), nonNil(items))
		}

		fmt.Printf("# cairn list %q -- %d entries\n", topic, len(rows))
		for _, r := range rows {
			scope := "global"
			if len(r.Scope) > 0 {
				scope = strings.Join(r.Scope, " ")
			}
			fmt.Printf("%s: %s\n", r.ID, r.Title)
			fmt.Printf("  summary: %s\n", r.Summary)
			fmt.Printf("  scope: %s  freshness: %s -- %s\n", scope, r.FreshnessState, r.FreshnessDetail)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
