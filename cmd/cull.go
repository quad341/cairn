package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

const defaultCullDisuseAfter = 30 * 24 * time.Hour

func init() {
	cullCandidatesCmd.Flags().Duration("disuse-after", defaultCullDisuseAfter,
		"disuse threshold: report entries not recalled (or, if never recalled, not created) within this long (NFR-06)")
	cullEvictCmd.Flags().String("reviewer", "",
		"reviewer to mail for a shared-tier (rig/role/global) eviction proposal (default: $CAIRN_REVIEWER, else a per-tier computed default)")
	rootCmd.AddCommand(cullCandidatesCmd, cullEvictCmd)
}

var cullCandidatesCmd = &cobra.Command{
	Use:   "cull-candidates",
	Short: "Entries disused past a threshold, JSON (read-only; FR-10/NFR-06)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("cull-candidates covers every entry and does not filter by identity; " +
				"use 'cairn map' or 'cairn prime' for a scoped view")
		}
		disuseAfter, _ := cmd.Flags().GetDuration("disuse-after")
		findings, err := cairn.CullCandidates(cmd.Context(), storePath(), disuseAfter)
		if err != nil {
			return err
		}
		if findings == nil {
			findings = []cairn.CullCandidateFinding{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(findings)
	},
}

var cullEvictCmd = &cobra.Command{
	Use:   "cull-evict <id>",
	Short: "Evict an entry: direct delete for private scope, review-branch proposal for shared scope (NFR-07)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.EntryForEvict(cmd.Context(), storePath(), args[0])
		if err != nil {
			return fmt.Errorf("look up %s for eviction: %w", args[0], err)
		}

		if cairn.IsPrivateScope(e.Scope) {
			sha, err := e.EvictDirect(cmd.Context(), storePath())
			if err != nil {
				return fmt.Errorf("evict entry: %w", err)
			}
			fmt.Printf("%s\n", sha)
			return nil
		}
		return requestCullReview(cmd, e)
	},
}
