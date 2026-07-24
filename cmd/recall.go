package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

const defaultPromoteThreshold = 3

func init() {
	promoteCandidatesCmd.Flags().Int("threshold", defaultPromoteThreshold,
		"minimum RecurrenceCount for an entry to be reported as a promotion candidate (NFR-06)")
	rootCmd.AddCommand(recallStatsCmd, promoteCandidatesCmd)
}

var recallStatsCmd = &cobra.Command{
	Use:   "recall-stats",
	Short: "Per-entry HitCount/LastRecalledAt report, JSON (read-only; FR-08)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("recall-stats covers every entry and does not filter by identity; " +
				"use 'cairn map' or 'cairn prime' for a scoped view")
		}
		findings, err := cairn.RecallStats(cmd.Context(), storePath())
		if err != nil {
			return err
		}
		if findings == nil {
			findings = []cairn.RecallStatsFinding{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(findings)
	},
}

var promoteCandidatesCmd = &cobra.Command{
	Use:   "promote-candidates",
	Short: "Entries recurring >= threshold times and not yet promoted, JSON (read-only; FR-07)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("promote-candidates covers every entry and does not filter by identity; " +
				"use 'cairn map' or 'cairn prime' for a scoped view")
		}
		threshold, _ := cmd.Flags().GetInt("threshold")
		findings, err := cairn.PromoteCandidates(cmd.Context(), storePath(), threshold)
		if err != nil {
			return err
		}
		if findings == nil {
			findings = []cairn.PromoteCandidateFinding{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(findings)
	},
}
