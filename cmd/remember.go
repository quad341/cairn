package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(rememberCmd)
	rememberCmd.Flags().String("topic", "",
		"topic key hint for the entry (freeform; a curator normalizes it on shared-scope promotion)")
	// A plain comma-separated string, not StringSlice: StringSlice's Set
	// accumulates across repeated calls on a reused FlagSet (fine for a
	// single process's argv, but a footgun for tests re-executing rootCmd).
	rememberCmd.Flags().String("scope", "",
		"scope tags for the entry, e.g. --scope rig:web,role:reviewer (default: private -- the agent:<id> tag from the resolved identity)")
}

// errRememberNotImplemented is returned once topic_key and scope pass
// validation: entry construction and write-back are a follow-up bead
// (crn-419.2); this scaffold only wires the command and the input guard.
var errRememberNotImplemented = errors.New("remember: writing entries is not implemented yet")

var rememberCmd = &cobra.Command{
	Use:   "remember <body>",
	Short: "Write a new knowledge entry to the store (curation-tier routing)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, _ []string) error {
		topic, _ := cmd.Flags().GetString("topic")
		if err := cairn.ValidatePathSegment(topic); err != nil {
			return fmt.Errorf("invalid --topic: %w", err)
		}

		scope, err := rememberScope(cmd)
		if err != nil {
			return err
		}
		for _, tag := range scope {
			if err := cairn.ValidatePathSegment(tag); err != nil {
				return fmt.Errorf("invalid scope tag %q: %w", tag, err)
			}
		}

		return errRememberNotImplemented
	},
}

// rememberScope returns the entry's scope tags: --scope if given, else the
// private tier for the resolved identity (agent/<agent>/) via defaultScope.
func rememberScope(cmd *cobra.Command) ([]string, error) {
	raw, _ := cmd.Flags().GetString("scope")
	if raw != "" {
		return strings.Split(raw, ","), nil
	}
	return defaultScope(resolveIdentity(cmd))
}

// defaultScope derives the private-tier scope -- a single agent:<id> tag --
// from a resolved identity's full tag set (which may also carry rig: and
// role: tags, per identity.go's doc example). The identity's whole tag set
// is not itself a valid scope: DESIGN.md §2 has exactly one directory per
// entry (global/, rig/<rig>/, role/<role>/, agent/<agent>/), and a multi-tag
// scope spanning rig+role+agent doesn't map to any single one of them.
// Errors if the identity carries no agent: tag, rather than silently
// defaulting to a broader -- and therefore higher-blast-radius, DESIGN.md §7
// -- scope.
func defaultScope(identity []string) ([]string, error) {
	for _, tag := range identity {
		if strings.HasPrefix(tag, "agent:") {
			return []string{tag}, nil
		}
	}
	return nil, errors.New("no --scope given and the resolved identity has no agent:<id> tag " +
		"to default the private tier to; pass --scope explicitly")
}
