package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
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

// resolveIdentityValidated is resolveIdentity, but validates each resolved
// tag via cairn.ValidatePathSegment first: scope/identity validation is
// input hygiene, not access control, so a malformed --identity/$CAIRN_IDENTITY
// tag classifies as invalid_input under --json rather than falling through
// to whatever error its first downstream use happens to produce. Wired into
// get/map/prime/remember -- status rejects --identity outright
// (identityRequested). remember calls this after its own --topic/--scope
// validation, which already catches an unsafe identity-derived default scope
// tag as a "scope tag" error; this covers what that loop can't: an identity
// tag that never becomes a scope tag at all (e.g. carried only into
// created_by, or present alongside an explicit --scope override).
func resolveIdentityValidated(cmd *cobra.Command) ([]string, error) {
	identity := resolveIdentity(cmd)
	for _, tag := range identity {
		if err := cairn.ValidatePathSegment(tag); err != nil {
			return nil, classifiedErr(CategoryInvalidInput, tag, fmt.Errorf("invalid identity tag %q: %w", tag, err))
		}
	}
	return identity, nil
}
