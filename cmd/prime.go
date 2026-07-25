package cmd

import (
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
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

		if wantsJSON(cmd) {
			result, err := cairn.PrimeStructured(cmd.Context(), storePath(), identity)
			if err != nil {
				return emitError(cmd, err)
			}
			result.Identity = nonNil(result.Identity)
			result.Warnings = nonNil(result.Warnings)
			return emitJSON(cmd.OutOrStdout(), result)
		}

		out, err := cairn.Prime(cmd.Context(), storePath(), identity)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}
