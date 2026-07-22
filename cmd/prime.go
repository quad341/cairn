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
		out, err := cairn.Prime(storePath(), resolveIdentity(cmd))
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}
