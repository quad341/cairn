package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var storeFlag string

// storePath resolves the knowledge store: --store flag, then $CAIRN_STORE, then cwd.
func storePath() string {
	if storeFlag != "" {
		return storeFlag
	}
	if s := os.Getenv("CAIRN_STORE"); s != "" {
		return s
	}
	return "."
}

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "A scoped, freshness-tracked knowledge cache for AI agent fleets",
	Long: "cairn — markers left by the agent who solved it, so the next one\n" +
		"doesn't re-walk the trail. Scoped per rig/role/agent, freshness-anchored.",
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&storeFlag, "store", "",
		"store repo path (default: $CAIRN_STORE or the current directory)")
	rootCmd.PersistentFlags().StringSlice("identity", nil,
		"scope tags for recall, e.g. --identity rig:web,role:reviewer (or $CAIRN_IDENTITY)")
}
