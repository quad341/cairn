package cmd

import (
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

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
		identity := resolveIdentity(cmd)
		rows, err := cairn.ListByTopic(cmd.Context(), storePath(), key, identity)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("no entries found for topic %q", topic)
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
