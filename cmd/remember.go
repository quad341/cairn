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
	rememberCmd.Flags().String("reviewer", "",
		"reviewer to mail for a shared-tier (rig/role/global) entry (default: $CAIRN_REVIEWER, else a per-tier computed default)")
}

var rememberCmd = &cobra.Command{
	Use:   "remember <body>",
	Short: "Write a new knowledge entry to the store (curation-tier routing)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		topic, _ := cmd.Flags().GetString("topic")
		if topic != "" {
			if err := cairn.ValidatePathSegment(topic); err != nil {
				return fmt.Errorf("invalid --topic: %w", err)
			}
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

		createdBy := strings.Join(resolveIdentity(cmd), " ")
		e, err := cairn.NewEntry(topic, scope, args[0], createdBy)
		if err != nil {
			return fmt.Errorf("construct entry: %w", err)
		}

		matched, err := recurrenceMatch(cmd, e)
		if err != nil {
			return err
		}
		if matched != nil {
			return recordRecurrence(cmd, matched)
		}

		if err := e.Create(storePath()); err != nil {
			return fmt.Errorf("write entry: %w", err)
		}
		fmt.Printf("%s\n", e.ID)

		if cairn.IsPrivateScope(e.Scope) {
			sha, err := e.CommitDirect(cmd.Context(), storePath())
			if err != nil {
				return fmt.Errorf("commit entry: %w", err)
			}
			fmt.Printf("%s\n", sha)
			return nil
		}
		return requestReview(cmd, e, scope)
	},
}

// recurrenceMatch checks candidate against every entry VISIBLE to the
// resolved identity for an exact topic_key match (crn-28ge.1.4, FR-02/FR-06):
// a repeat capture of the same fact should grow an existing entry's
// RecurrenceCount, not pile up a duplicate. It reuses Conflicts
// (crn-28ge.1.3's single-candidate primitive) -- the same signal computation
// `cairn get` already uses (getCmd, cmd/commands.go) -- rather than a second,
// independent equality check (NFR-05), and only ever acts on Conflicts'
// "topic_key" (exact, strict) finding, never its "content" (Jaccard
// word-similarity) finding: that signal is deliberately fuzzier, and a
// similar-but-different topic is a near-miss, not a repeat.
// candidate.TopicKey == "" never matches anything (pairSignals' sameTopicKey
// requires a non-empty key on both sides), so an untopiced remember is
// unaffected, matching today's behavior.
//
// Like getCmd, the matched entry's full fields come from IterEntries
// filtered down to Visible's ID set, not a per-ID Find call: Visible alone
// never populates Title/Summary (needed for Conflicts to even run its
// content signal), and Find's hit_count/last_recalled_at side effect would
// conflate a write-time re-affirmation with recall-time telemetry
// (crn-28ge.1.5/.1.6).
func recurrenceMatch(cmd *cobra.Command, candidate *cairn.Entry) (*cairn.Entry, error) {
	identity := resolveIdentity(cmd)
	store := storePath()

	// Force the index fresh before checking visibility. ensureFresh's
	// git-HEAD watermark (crn-6az.6.1.2) can self-heal a stale index for
	// most callers, but it is structurally blind to a shared-tier entry: a
	// recurrence hit's whole point is to catch a topic recurring while the
	// first report's CommitToReviewBranch commit is still sitting unmerged
	// on its own remember/<id> branch (the common case, not an edge case --
	// see CommitRecurrenceToReviewBranch's own doc comment), and that path
	// deliberately never advances the store's own HEAD. So the watermark
	// can look unchanged forever even though a brand-new entry now sits on
	// disk, and Visible would silently miss it. Find hits this exact same
	// gap for the identical reason and fixes it the same way (entry.go).
	if _, err := cairn.Reindex(cmd.Context(), store); err != nil {
		return nil, fmt.Errorf("check for a recurring entry: %w", err)
	}

	visible, err := cairn.Visible(cmd.Context(), store, identity)
	if err != nil {
		return nil, fmt.Errorf("check for a recurring entry: %w", err)
	}
	visibleIDs := make(map[string]bool, len(visible))
	for _, v := range visible {
		visibleIDs[v.ID] = true
	}
	all, err := cairn.IterEntries(store)
	if err != nil {
		return nil, fmt.Errorf("check for a recurring entry: %w", err)
	}
	others := make([]*cairn.Entry, 0, len(visibleIDs))
	byID := make(map[string]*cairn.Entry, len(visibleIDs))
	for _, full := range all {
		if visibleIDs[full.ID] {
			others = append(others, full)
			byID[full.ID] = full
		}
	}

	for _, c := range cairn.Conflicts(candidate, others) {
		if c.Kind != "topic_key" {
			continue
		}
		other := c.EntryIDs[0]
		if other == candidate.ID {
			other = c.EntryIDs[1]
		}
		return byID[other], nil
	}
	return nil, nil
}

// recordRecurrence persists a capture-time recurrence hit (crn-28ge.1.4,
// FR-02/FR-06) against an already-existing matched entry: increment
// RecurrenceCount and commit that change via the same tier-appropriate
// commit path matched's own scope already resolves to (CommitDirect for
// private, a review-branch commit for shared) -- never a shortcut direct
// write to a shared-tier file, which would bypass the review gate that tier
// requires. It writes no duplicate entry: the brand-new candidate NewEntry
// already built is discarded entirely, never passed to Create.
//
// Only the commit primitive is reused here, not the full requestReview flow
// a brand-new shared-tier entry goes through: a recurrence hit reconfirms an
// entry that may already be awaiting review, it does not need a second,
// fresh reviewer mail of its own.
func recordRecurrence(cmd *cobra.Command, matched *cairn.Entry) error {
	matched.RecurrenceCount++
	if err := matched.WriteBackRecurrenceCount(); err != nil {
		return fmt.Errorf("record recurrence for %s: %w", matched.ID, err)
	}
	fmt.Printf("recurrence: %s (count: %d)\n", matched.ID, matched.RecurrenceCount)

	if cairn.IsPrivateScope(matched.Scope) {
		sha, err := matched.CommitDirect(cmd.Context(), storePath())
		if err != nil {
			return fmt.Errorf("commit recurrence for %s: %w", matched.ID, err)
		}
		fmt.Printf("%s\n", sha)
		return nil
	}
	branch, err := matched.CommitRecurrenceToReviewBranch(cmd.Context(), storePath())
	if err != nil {
		return fmt.Errorf("commit recurrence for %s to review branch: %w", matched.ID, err)
	}
	fmt.Printf("review branch: %s\n", branch)
	return nil
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
