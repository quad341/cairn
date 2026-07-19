package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// resolveIdentity returns the agent's scope tags from the --identity flag or the
// CAIRN_IDENTITY env var (space-separated), e.g. "rig:web role:reviewer agent:bot".
func resolveIdentity(cmd *cobra.Command) []string {
	if f, _ := cmd.Flags().GetStringSlice("identity"); len(f) > 0 {
		return f
	}
	if e := strings.TrimSpace(os.Getenv("CAIRN_IDENTITY")); e != "" {
		return strings.Fields(e)
	}
	return nil
}
