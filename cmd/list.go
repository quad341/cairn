package cmd

import (
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

const listInstruction = "Run cairn get <id> before acting on a relevant entry. If none is relevant, stop."

type listOutput struct {
	Instruction string     `json:"instruction"`
	TopicKey    *string    `json:"topic_key"`
	Entries     []listItem `json:"entries"`
}

type listItem struct {
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	Freshness    string               `json:"freshness"`
	Conflict     *cairn.TopicConflict `json:"conflict"`
	ReviewStatus string               `json:"review_status"`
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
			return emitModelError(cmd, err)
		}
		rows, err := cairn.ListByTopic(cmd.Context(), storePath(), key, identity)
		if err != nil {
			return emitModelError(cmd, err)
		}
		if len(rows) == 0 {
			return emitModelError(cmd, classifiedErr(CategoryNotFound, topic, fmt.Errorf("no entries found for topic %q", topic)))
		}

		items := make([]listItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, listItem{
				ID:           r.ID,
				Title:        r.Title,
				Freshness:    r.FreshnessState,
				Conflict:     r.Conflict,
				ReviewStatus: r.ReviewStatus,
			})
		}
		var topicKey *string
		if key != "" {
			topicKey = &key
		}
		return emitModelJSON(cmd, listOutput{Instruction: listInstruction, TopicKey: topicKey, Entries: items})
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
