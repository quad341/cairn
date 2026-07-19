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

// identityRequested reports whether --identity or $CAIRN_IDENTITY was
// explicitly supplied, as opposed to left at its default. Unlike
// resolveIdentity, it distinguishes "explicitly passed" from "absent" —
// commands that don't support identity scoping (e.g. status) use this to
// reject an explicit request instead of silently ignoring it.
func identityRequested(cmd *cobra.Command) bool {
	if cmd.Flags().Changed("identity") {
		return true
	}
	return strings.TrimSpace(os.Getenv("CAIRN_IDENTITY")) != ""
}
