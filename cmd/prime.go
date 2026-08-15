package cmd

import (
	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
)

// defaultPrimeBudgetBytes is a starting point sized to keep a SessionStart
// hook injection small while still fitting a useful number of entries, not a
// calibrated final answer.
const defaultPrimeBudgetBytes = 8192

const primeInstruction = "Use cairn search <your situation> when cached knowledge may help; " +
	"then cairn get <id> before acting. Stale knowledge is a lead, not truth."

type primeOutput struct {
	Instruction  string             `json:"instruction"`
	TotalVisible int                `json:"total_visible"`
	FreshCount   int                `json:"fresh_count"`
	StaleCount   int                `json:"stale_count"`
	UnknownCount int                `json:"unknown_count"`
	Triggers     []primeTrigger     `json:"triggers"`
	TopicCounts  []cairn.TopicCount `json:"topic_counts"`
	Warnings     []string           `json:"warnings"`
}

type primeTrigger struct {
	ID        string               `json:"id"`
	TopicKey  *string              `json:"topic_key"`
	Title     string               `json:"title"`
	Freshness string               `json:"freshness"`
	Conflict  *cairn.TopicConflict `json:"conflict"`
}

func projectPrime(result cairn.PrimeResult) primeOutput {
	triggers := make([]primeTrigger, 0, len(result.Items))
	for _, item := range result.Items {
		var topicKey *string
		if item.TopicKey != "" {
			key := item.TopicKey
			topicKey = &key
		}
		triggers = append(triggers, primeTrigger{
			ID: item.ID, TopicKey: topicKey, Title: item.Title, Freshness: item.Freshness.Status, Conflict: item.Conflict,
		})
	}
	warnings := append([]string(nil), result.Warnings...)
	if result.ChecksCapped {
		warnings = append(warnings, "freshness-check budget reached; some entries are unknown rather than verified")
	}
	return primeOutput{
		Instruction: primeInstruction, TotalVisible: result.TotalVisible,
		FreshCount: result.FreshCount, StaleCount: result.StaleCount, UnknownCount: result.UnknownCount,
		Triggers: triggers, TopicCounts: nonNil(result.TopicCounts), Warnings: nonNil(warnings),
	}
}

func init() {
	primeCmd.Flags().Int("budget-bytes", defaultPrimeBudgetBytes,
		"cap on itemized payload bytes; entries past the cap are counted but not itemized (crn-0vqk FR-2)")
	rootCmd.AddCommand(primeCmd)
}

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Emit the agent's scoped knowledge map + usage (for a SessionStart hook)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitModelError(cmd, err)
		}

		budgetBytes, _ := cmd.Flags().GetInt("budget-bytes")
		result, err := cairn.Prime(cmd.Context(), storePath(), identity, budgetBytes)
		if err != nil {
			return emitModelError(cmd, err)
		}

		itemIDs := make([]string, len(result.Items))
		for i, item := range result.Items {
			itemIDs[i] = item.ID
		}
		obslog.FromContext(cmd.Context()).PrimeEmit(obslog.PrimeEmitFields{
			IdentityTags:   identity,
			RunID:          resolveRunID(cmd),
			ItemIDs:        itemIDs,
			TotalVisible:   result.TotalVisible,
			TruncatedCount: result.TruncatedCount,
		})

		return emitModelJSON(cmd, projectPrime(result))
	},
}
