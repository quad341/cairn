package cmd

import (
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

// defaultPrimeBudgetBytes is a starting point sized to keep a SessionStart
// hook injection small while still fitting a useful number of entries, not a
// calibrated final answer.
const defaultPrimeBudgetBytes = 8192

func init() {
	primeCmd.Flags().Int("budget-bytes", defaultPrimeBudgetBytes,
		"cap on itemized payload bytes; entries past the cap are counted but not itemized (crn-0vqk FR-2); <=0 means unlimited")
	rootCmd.AddCommand(primeCmd)
}

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Emit the agent's scoped knowledge map + usage (for a SessionStart hook)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitError(cmd, err)
		}

		budgetBytes, _ := cmd.Flags().GetInt("budget-bytes")
		result, err := cairn.Prime(cmd.Context(), storePath(), identity, budgetBytes)
		if err != nil {
			return emitError(cmd, err)
		}

		if wantsJSON(cmd) {
			result.Identity = nonNil(result.Identity)
			result.Items = nonNil(result.Items)
			result.Warnings = nonNil(result.Warnings)
			return emitJSON(cmd.OutOrStdout(), result)
		}

		fmt.Print(cairn.RenderPrimeText(result))
		return nil
	},
}
