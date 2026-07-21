package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version, commit, and date are overridden via -ldflags "-X ..." at build
// time (see the Makefile's build target).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("cairn version %s (commit %s, built %s)\n", version, commit, date)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
