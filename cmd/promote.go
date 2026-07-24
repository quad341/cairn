package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	promoteMarkCmd.Flags().String("bead", "", "bead ID this entry was promoted into (required)")
	promoteMarkCmd.Flags().String("reviewer", "",
		"reviewer to mail for a shared-tier (rig/role/global) promotion mark (default: $CAIRN_REVIEWER, else a per-tier computed default)")
	rootCmd.AddCommand(promoteMarkCmd)
}

// promoteMarkCmd records that an entry was promoted into a tracked bead:
// direct commit for private scope, review-branch proposal for shared scope
// -- the same tier-conditional dispatch already shipped for RecurrenceCount
// (cmd/remember.go's recordRecurrence) and CULL (cullEvictCmd), applied to
// PromotedBeadID. e.PromotedBeadID doubles as a promotion idempotency
// guard: a repeat call with the same bead ID is a no-op success, a repeat
// call with a different bead ID is refused rather than silently
// overwriting a prior promotion record.
var promoteMarkCmd = &cobra.Command{
	Use:   "promote-mark <id>",
	Short: "Record that an entry was promoted into a tracked bead: direct commit for private scope, review-branch proposal for shared scope",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		bead, _ := cmd.Flags().GetString("bead")
		bead = strings.TrimSpace(bead)
		if bead == "" {
			return errors.New("--bead is required")
		}

		e, err := cairn.EntryByID(cmd.Context(), storePath(), args[0])
		if err != nil {
			return fmt.Errorf("look up %s for promotion: %w", args[0], err)
		}

		if e.PromotedBeadID == bead {
			fmt.Printf("%s already marked promoted to %s\n", e.ID, bead)
			return nil
		}
		if e.PromotedBeadID != "" {
			return fmt.Errorf("entry %s is already marked promoted to bead %s, refusing to overwrite with %s",
				e.ID, e.PromotedBeadID, bead)
		}

		e.PromotedBeadID = bead
		if err := e.WriteBackPromotedBeadID(); err != nil {
			return fmt.Errorf("write promoted_bead_id for %s: %w", e.ID, err)
		}

		if cairn.IsPrivateScope(e.Scope) {
			sha, err := e.CommitDirect(cmd.Context(), storePath())
			if err != nil {
				return fmt.Errorf("commit promotion mark: %w", err)
			}
			fmt.Printf("%s\n", sha)
			return nil
		}
		return requestPromotionReview(cmd, e)
	},
}
