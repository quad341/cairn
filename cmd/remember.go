package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	rememberCmd.Flags().String("file", "",
		"read the entry body from this file, instead of a positional argument or piped stdin")
	rememberCmd.Flags().String("title", "",
		"explicit title (default: auto-derived from the body)")
	rememberCmd.Flags().String("summary", "",
		"explicit summary (default: auto-derived from the body)")
	rememberCmd.Flags().String("anchor-repo", "",
		`git repo the --anchor-path values are tracked in (with --anchor-path, builds a "files" source anchor)`)
	rememberCmd.Flags().StringArray("anchor-path", nil,
		"a path (relative to --anchor-repo) anchoring this entry to its source; repeatable")
	rememberCmd.Flags().Bool("verify", false,
		"immediately compute and persist the anchor's fingerprint (soft-fails to stderr if it doesn't resolve)")
	rememberCmd.Flags().Bool("force", false,
		"create a new entry even if a recurrence match is found, recording which entry it overrides")
}

var rememberCmd = &cobra.Command{
	Use:   "remember [body]",
	Short: "Write a new knowledge entry to the store (curation-tier routing)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := rememberBody(cmd, args)
		if err != nil {
			return err
		}

		topic, _ := cmd.Flags().GetString("topic")
		if topic != "" {
			if err := cairn.ValidatePathSegment(topic); err != nil {
				return emitError(cmd, classifiedErr(CategoryInvalidInput, topic, fmt.Errorf("invalid --topic: %w", err)))
			}
		}

		scope, err := rememberScope(cmd)
		if err != nil {
			return emitError(cmd, classifiedErr(CategoryInvalidInput, "", err))
		}
		for _, tag := range scope {
			if err := cairn.ValidateScopeTag(tag); err != nil {
				hint := ""
				if !strings.Contains(tag, ":") {
					hint = " (a global-tier entry has no scope tags at all; pass --scope '' explicitly)"
				}
				return emitError(cmd, classifiedErr(CategoryInvalidInput, tag, fmt.Errorf("invalid scope tag %q: %w%s", tag, err, hint)))
			}
		}

		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitError(cmd, err)
		}

		title, _ := cmd.Flags().GetString("title")
		summary, _ := cmd.Flags().GetString("summary")
		createdBy := strings.Join(identity, " ")
		e, err := cairn.NewEntry(cairn.NewEntryParams{
			TopicKey:  topic,
			Scope:     scope,
			Body:      body,
			CreatedBy: createdBy,
			Title:     title,
			Summary:   summary,
			Anchor:    rememberAnchor(cmd),
		})
		if err != nil {
			return emitError(cmd, fmt.Errorf("construct entry: %w", err))
		}

		if verify, _ := cmd.Flags().GetBool("verify"); verify && e.Anchor.Type == "files" {
			verifyAnchor(cmd, e)
		}

		matched, err := recurrenceMatch(cmd, e)
		if err != nil {
			return emitError(cmd, err)
		}
		force, _ := cmd.Flags().GetBool("force")
		if matched != nil && !force {
			return recordRecurrence(cmd, matched)
		}
		if matched != nil {
			e.OverriddenDuplicateOf = matched.ID
		}

		if err := e.Create(storePath()); err != nil {
			return emitError(cmd, fmt.Errorf("write entry: %w", err))
		}
		if !wantsJSON(cmd) {
			fmt.Printf("%s\n", e.ID)
			if matched != nil {
				fmt.Printf("override: forced past duplicate of %s\n", matched.ID)
			}
		}

		if cairn.IsPrivateScope(e.Scope) {
			sha, err := e.CommitDirect(cmd.Context(), storePath())
			if err != nil {
				return emitError(cmd, fmt.Errorf("commit entry: %w", err))
			}
			if wantsJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), RememberResult{
					ID:     e.ID,
					Scope:  nonNil(e.Scope),
					Commit: sha,
				})
			}
			fmt.Printf("%s\n", sha)
			return nil
		}
		return requestReview(cmd, e, scope)
	},
}

// RememberResult is cairn remember --json's top-level shape: the freshly
// created entry, how it was committed, and who (if anyone) was mailed for
// review. Commit and ReviewBranch are mutually exclusive (private tier vs.
// shared tier); Reviewer is only ever set alongside ReviewBranch.
//
// A recurrence hit (recordRecurrence, crn-qxj3) never produces a
// RememberResult: the candidate body is discarded, not stored, so it is
// reported through --json's error envelope instead (CategoryConflict), not
// this success shape.
type RememberResult struct {
	ID           string   `json:"id"`
	Scope        []string `json:"scope"`
	Commit       string   `json:"commit,omitempty"`
	ReviewBranch string   `json:"review_branch,omitempty"`
	Reviewer     string   `json:"reviewer,omitempty"`
}

// rememberBody resolves the entry body from exactly one of three sources: a
// positional argument, --file, or piped (non-interactive) stdin. All three
// indicators are always evaluated regardless of which is found first -- an
// already-present positional argument does not short-circuit the stdin
// check -- so two sources given together (e.g. a positional body plus piped
// stdin) are always caught as ambiguous, rather than one silently winning.
// Piped stdin is detected via os.ModeCharDevice: a real terminal (or go
// test's own attached input) is reliably a char device, so this is false
// whenever stdin has not actually been redirected.
func rememberBody(cmd *cobra.Command, args []string) (string, error) {
	file, _ := cmd.Flags().GetString("file")

	stdinPiped := false
	if info, err := os.Stdin.Stat(); err == nil {
		stdinPiped = info.Mode()&os.ModeCharDevice == 0
	}

	sources := 0
	if len(args) == 1 {
		sources++
	}
	if file != "" {
		sources++
	}
	if stdinPiped {
		sources++
	}

	switch {
	case sources > 1:
		return "", errors.New("ambiguous body source: pass exactly one of a positional body argument, --file, or piped stdin")
	case len(args) == 1:
		return args[0], nil
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read --file: %w", err)
		}
		return string(data), nil
	case stdinPiped:
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	default:
		return "", errors.New("a body is required: pass it as a positional argument, --file, or piped stdin")
	}
}

// rememberAnchor builds a "files" source anchor from --anchor-repo/
// --anchor-path when either is given, else returns the zero Anchor (which
// cairn.NewEntry defaults to Type: "none").
func rememberAnchor(cmd *cobra.Command) cairn.Anchor {
	repo, _ := cmd.Flags().GetString("anchor-repo")
	paths, _ := cmd.Flags().GetStringArray("anchor-path")
	if repo == "" && len(paths) == 0 {
		return cairn.Anchor{}
	}
	return cairn.Anchor{Type: "files", Repo: repo, Paths: paths}
}

// verifyAnchor computes e's anchor fingerprint and, on success, stamps it
// and VerifiedAt in memory -- before e.Create ever writes it, so no separate
// WriteBack is needed. A genuine failure to compute a fingerprint and an
// anchor that simply doesn't resolve to a trackable object
// (ComputeFingerprint's own ("", nil) "confirmed negative") both soft-fail
// identically: warn on stderr and leave the entry unverified, rather than
// aborting the whole remember call over an optional, best-effort check.
func verifyAnchor(cmd *cobra.Command, e *cairn.Entry) {
	fp, err := cairn.ComputeFingerprint(cmd.Context(), e.Anchor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: --verify: could not compute anchor fingerprint: %v\n", err)
		return
	}
	if fp == "" {
		fmt.Fprintf(os.Stderr, "warning: --verify: anchor does not resolve to a trackable object; skipping\n")
		return
	}
	e.Anchor.Fingerprint = fp
	e.VerifiedAt = time.Now().Format(time.DateOnly)
}

// recurrenceMatch checks candidate against every entry VISIBLE to the
// resolved identity for a genuine recurrence (crn-28ge.1.4, FR-02/FR-06,
// tightened by crn-qxj3): a repeat capture of the same fact should grow an
// existing entry's RecurrenceCount, not pile up a duplicate -- but a bare
// topic_key collision alone is not enough evidence of that, since --topic is
// a short freeform hint an agent can easily reuse for two unrelated facts.
// It reuses Conflicts (crn-28ge.1.3's single-candidate primitive) -- the same
// signal computation `cairn get` already uses (getCmd, cmd/commands.go) --
// rather than a second, independent equality check (NFR-05), and requires
// BOTH of Conflicts' signals to agree on the same other entry: an exact
// "topic_key" match AND a "content" (Jaccard word-similarity) match. Conflicts
// emits these as two separate findings that can both fire for the same pair
// (see internal/cairn's TestConflictsBothSignalsCanFireForSamePair), so
// recurrenceMatch tracks each signal per other-entry ID and only matches an
// entry that cleared both -- a shared topic with a genuinely distinct body
// clears topic_key alone and is correctly left as a new entry (crn-qxj3).
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
	for _, full := range all {
		if visibleIDs[full.ID] {
			others = append(others, full)
		}
	}

	topicMatch := make(map[string]bool)
	contentMatch := make(map[string]bool)
	for _, c := range cairn.Conflicts(candidate, others) {
		other := c.EntryIDs[0]
		if other == candidate.ID {
			other = c.EntryIDs[1]
		}
		switch c.Kind {
		case "topic_key":
			topicMatch[other] = true
		case "content":
			contentMatch[other] = true
		}
	}
	for _, o := range others {
		if topicMatch[o.ID] && contentMatch[o.ID] {
			return o, nil
		}
	}
	//nolint:nilnil // (nil,nil) = "no match"; sole caller checks matched != nil before use
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
//
// crn-qxj3: a recurrence match means the candidate's own body is discarded --
// nothing new was stored -- so this must not report success. The
// RecurrenceCount bump is real and still committed (it is legitimate
// telemetry, not the bug), but the discard itself is reported on stderr, and
// the resulting error is returned so the command exits non-zero in both
// plain and --json mode: an agent scripting against --json and checking
// only the exit code must be told too, not just a human reading stdout.
func recordRecurrence(cmd *cobra.Command, matched *cairn.Entry) error {
	matched.RecurrenceCount++
	if err := matched.WriteBackRecurrenceCount(); err != nil {
		return emitError(cmd, fmt.Errorf("record recurrence for %s: %w", matched.ID, err))
	}

	if cairn.IsPrivateScope(matched.Scope) {
		if _, err := matched.CommitDirect(cmd.Context(), storePath()); err != nil {
			return emitError(cmd, fmt.Errorf("commit recurrence for %s: %w", matched.ID, err))
		}
	} else {
		if _, err := matched.CommitRecurrenceToReviewBranch(cmd.Context(), storePath()); err != nil {
			return emitError(cmd, fmt.Errorf("commit recurrence for %s to review branch: %w", matched.ID, err))
		}
	}

	discardErr := fmt.Errorf(
		"not stored: recurrence of %s (count %d); body is a near-identical match of existing content -- pass --force to store it anyway",
		matched.ID, matched.RecurrenceCount)
	fmt.Fprintf(os.Stderr, "%v\n", discardErr)
	return emitError(cmd, classifiedErr(CategoryConflict, matched.ID, discardErr))
}

// rememberScope returns the entry's scope tags: --scope if given (a literal
// empty string is a deliberate, explicit request for the global tier -- see
// ValidateScopeTag's doc comment), else the private tier for the resolved
// identity (agent/<agent>/) via defaultScope.
func rememberScope(cmd *cobra.Command) ([]string, error) {
	if cmd.Flags().Changed("scope") {
		raw, _ := cmd.Flags().GetString("scope")
		if raw == "" {
			return []string{}, nil
		}
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
