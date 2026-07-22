package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(dedupCmd)
}

var dedupCmd = &cobra.Command{
	Use:   "dedup",
	Short: "Duplicate/re-scope candidates across shared-tier entries, JSON (read-only; librarian maintenance use)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("dedup covers every shared-tier entry and does not filter by identity; " +
				"use 'cairn map' or 'cairn prime' for a scoped view")
		}
		findings, err := cairn.Dedup(storePath())
		if err != nil {
			return err
		}
		if findings == nil {
			findings = []cairn.DedupFinding{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(findings)
	},
}
