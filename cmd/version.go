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

// VersionResult is cairn --json's shape for version information.
type VersionResult struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// printVersion renders version information. `cairn version`, `cairn
// --version`, and `cairn -v` all call this one function, so their output is
// byte-identical by construction, not by three independently maintained
// copies.
func printVersion(cmd *cobra.Command) error {
	if wantsJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), VersionResult{Version: version, Commit: commit, Date: date})
	}
	fmt.Printf("cairn version %s (commit %s, built %s)\n", version, commit, date)
	return nil
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return printVersion(cmd)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
