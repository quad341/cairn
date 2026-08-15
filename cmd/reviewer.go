package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

// requestReview hands e off for review: DESIGN.md §7's curation model never
// commits a shared-tier (rig/role/global) write directly, so it must land on
// its own branch and be mailed to that tier's reviewer instead. Callers must
// only invoke this for a scope that has already resolved away from the
// private agent tier (RunE branches on cairn.IsPrivateScope before ever
// reaching here). e's entry file already exists on disk by this point;
// nothing here can undo that, only report a downstream failure.
func requestReview(cmd *cobra.Command, e *cairn.Entry, scope []string) error {
	tier, value := cairn.ResolvedTier(scope)

	branch, err := e.CommitToReviewBranch(cmd.Context(), storePath())
	if err != nil {
		return emitError(cmd, fmt.Errorf("commit shared-tier entry to review branch: %w", err))
	}
	if !wantsJSON(cmd) {
		fmt.Printf("review branch: %s\n", branch)
	}

	reviewer, err := resolveReviewer(cmd, tier, value)
	if err != nil {
		return emitError(cmd, fmt.Errorf("entry %s committed to review branch %s, but resolving a reviewer to mail failed: %w",
			e.ID, branch, err))
	}
	if err := sendReviewMail(cmd.Context(), reviewer, e, branch); err != nil {
		return emitError(cmd, fmt.Errorf("entry %s committed to review branch %s, but mail to reviewer %q failed: %w",
			e.ID, branch, reviewer, err))
	}
	if wantsJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), RememberResult{
			ID:           e.ID,
			Scope:        nonNil(e.Scope),
			ReviewBranch: branch,
			Reviewer:     reviewer,
			Metadata:     metadataQuality(e),
		})
	}
	fmt.Printf("mailed reviewer: %s\n", reviewer)
	return nil
}

// resolveReviewer returns who a shared-tier remember call should mail for
// review: --reviewer if given, else $CAIRN_REVIEWER, else a computed
// default for tier (rig/role/global -- never called for the private agent
// tier). Mirrors the --store/$CAIRN_STORE and --identity/$CAIRN_IDENTITY
// flag-then-env precedent.
func resolveReviewer(cmd *cobra.Command, tier, value string) (string, error) {
	if f, _ := cmd.Flags().GetString("reviewer"); f != "" {
		return validateReviewerAddress(f)
	}
	if e := os.Getenv("CAIRN_REVIEWER"); strings.TrimSpace(e) != "" {
		return validateReviewerAddress(e)
	}
	return defaultReviewer(tier, value)
}

// defaultReviewer computes the per-tier default reviewer: a distinct
// recipient per tier, not one address shared across role/rig/global.
// global's "mayor" is a permanent constant, not an interim placeholder --
// the sole fleet-wide singleton reviewer.
func defaultReviewer(tier, value string) (string, error) {
	switch tier {
	case "global":
		return "mayor", nil
	case "rig":
		rig := strings.TrimSpace(os.Getenv("GC_RIG"))
		if rig == "" {
			return "", errors.New("cannot compute the default rig reviewer: $GC_RIG is not set; pass --reviewer or $CAIRN_REVIEWER")
		}
		return rig + "/architect", nil
	case "role":
		rig := strings.TrimSpace(os.Getenv("GC_RIG"))
		if rig == "" {
			return "", errors.New("cannot compute the default role reviewer: $GC_RIG is not set; pass --reviewer or $CAIRN_REVIEWER")
		}
		return rig + "/" + value, nil
	default:
		return "", fmt.Errorf("no default reviewer for tier %q", tier)
	}
}

// validateReviewerAddress rejects an explicit --reviewer/$CAIRN_REVIEWER
// override that is empty, starts with '-', or carries a control/null byte,
// rather than silently passing a garbage recipient through to `gc mail
// send`. The leading-'-' check exists because sendReviewMail passes reviewer
// as a bare positional argument with no `--` separator guard (unlike this
// package's git call sites, which always guard a positional pathspec with
// `--`) -- rejecting it here keeps a malformed override from being misread
// as a flag by `gc mail send`'s own argument parser. It deliberately does
// not reuse cairn.ValidatePathSegment: a real reviewer address contains a
// slash (e.g. "myrig/architect"), which that validator rejects outright. An
// unset override is not a misconfiguration -- that path never reaches this
// function.
func validateReviewerAddress(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", errors.New("reviewer address must not be empty")
	}
	if strings.HasPrefix(trimmed, "-") {
		return "", errors.New("reviewer address must not start with '-' (would be misread as a flag by gc mail send)")
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("reviewer address must not contain a control or null byte")
		}
	}
	return trimmed, nil
}

// sendReviewMail shells out to `gc mail send` with a merge-request-style
// message naming e's topic and its review branch. gc is this fleet's
// orchestration tool, a different concern from cairn's own generic store
// model (README.md: "cairn is generic; your notes are yours"), so this
// integration lives here in cmd, not in internal/cairn.
func sendReviewMail(ctx context.Context, reviewer string, e *cairn.Entry, branch string) error {
	subject := fmt.Sprintf("cairn remember review: %s", e.TopicKey)
	body := fmt.Sprintf(
		"New shared-tier cairn entry %s (topic %q, scope %s) is ready for review.\n\n"+
			"Branch: %s\n\nMerge into the store's default branch when satisfied; this branch does not auto-merge.",
		e.ID, e.TopicKey, strings.Join(e.Scope, " "), branch)
	return mailSend(ctx, reviewer, subject, body)
}

// mailSend shells out to `gc mail send` with subject/body -- the exec
// plumbing shared by every reviewer notification this package sends: a
// first review request (sendReviewMail) and a librarian's reactive
// stale-branch reminder (branches.go's sendStaleBranchReminder) alike.
func mailSend(ctx context.Context, to, subject, body string) error {
	out, err := exec.CommandContext(ctx, "gc", "mail", "send", to, "-s", subject, "-m", body).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gc mail send %s: %w: %s", to, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// batchReviewCandidate is one shared-tier --batch-file entry that has
// already committed to its own review branch (commitForBatchReview) and is
// waiting for its group's notification (requestBatchReview).
type batchReviewCandidate struct {
	entry    *cairn.Entry
	branch   string
	reviewer string
}

// commitForBatchReview is requestReview's batch-mode half: commit e to its
// own review branch and resolve who should review it, but do not mail --
// batch mode groups many entries by resolved reviewer and sends one mail per
// group afterward (requestBatchReview) instead of mailing per entry like
// requestReview does.
func commitForBatchReview(cmd *cobra.Command, e *cairn.Entry, scope []string) (branch, reviewer string, err error) {
	tier, value := cairn.ResolvedTier(scope)

	branch, err = e.CommitToReviewBranch(cmd.Context(), storePath())
	if err != nil {
		return "", "", fmt.Errorf("commit shared-tier entry to review branch: %w", err)
	}

	reviewer, err = resolveReviewer(cmd, tier, value)
	if err != nil {
		return branch, "", fmt.Errorf("entry %s committed to review branch %s, but resolving a reviewer to mail failed: %w",
			e.ID, branch, err)
	}
	return branch, reviewer, nil
}

// requestBatchReview groups candidates by resolved reviewer and sends one
// sendBatchReviewMail per distinct group (NFR-2: reuses each candidate's
// already-resolved reviewer from commitForBatchReview/resolveReviewer,
// rather than resolving again), instead of one mail per entry like
// requestReview/sendReviewMail. A reviewer with zero candidates (an
// all-private-tier batch) never reaches here at all -- the caller only
// invokes this when len(candidates) > 0.
func requestBatchReview(ctx context.Context, candidates []batchReviewCandidate) error {
	var order []string
	groups := map[string][]batchReviewCandidate{}
	for _, c := range candidates {
		if _, ok := groups[c.reviewer]; !ok {
			order = append(order, c.reviewer)
		}
		groups[c.reviewer] = append(groups[c.reviewer], c)
	}
	for _, reviewer := range order {
		if err := sendBatchReviewMail(ctx, reviewer, groups[reviewer]); err != nil {
			return fmt.Errorf("mail to reviewer %q failed: %w", reviewer, err)
		}
	}
	return nil
}

// sendBatchReviewMail mirrors sendReviewMail's wording, enumerating every
// entry's id and review branch in the group (FR-4) instead of naming just
// one.
func sendBatchReviewMail(ctx context.Context, reviewer string, group []batchReviewCandidate) error {
	subject := fmt.Sprintf("cairn remember batch review: %d entries", len(group))
	var b strings.Builder
	fmt.Fprintf(&b, "%d new shared-tier cairn entries are ready for review.\n\n", len(group))
	for _, c := range group {
		fmt.Fprintf(&b, "- %s (topic %q, scope %s): %s\n", c.entry.ID, c.entry.TopicKey, strings.Join(c.entry.Scope, " "), c.branch)
	}
	b.WriteString("\nMerge each branch into the store's default branch individually when satisfied; branches do not auto-merge.")
	return mailSend(ctx, reviewer, subject, b.String())
}

// requestCullReview proposes e's eviction on a review branch and mails the
// tier-appropriate reviewer -- mirrors requestReview, applied to a delete
// instead of an add. Callers must only invoke this for a scope that has
// already resolved away from the private agent tier.
func requestCullReview(cmd *cobra.Command, e *cairn.Entry) error {
	tier, value := cairn.ResolvedTier(e.Scope)

	branch, err := e.EvictToReviewBranch(cmd.Context(), storePath())
	if err != nil {
		return fmt.Errorf("propose shared-tier eviction on a review branch: %w", err)
	}
	fmt.Printf("cull review branch: %s\n", branch)

	reviewer, err := resolveReviewer(cmd, tier, value)
	if err != nil {
		return fmt.Errorf("entry %s proposed for eviction on branch %s, but resolving a reviewer to mail failed: %w", e.ID, branch, err)
	}
	if err := sendCullReviewMail(cmd.Context(), reviewer, e, branch); err != nil {
		return fmt.Errorf("entry %s proposed for eviction on branch %s, but mail to reviewer %q failed: %w", e.ID, branch, reviewer, err)
	}
	fmt.Printf("mailed reviewer: %s\n", reviewer)
	return nil
}

// sendCullReviewMail mirrors sendReviewMail, with cull-specific wording: the
// branch deletes the entry rather than adding one, and a reviewer merges it
// with plain git (cairn review merge cannot handle a pure-deletion branch).
func sendCullReviewMail(ctx context.Context, reviewer string, e *cairn.Entry, branch string) error {
	subject := fmt.Sprintf("cairn cull review: %s", e.TopicKey)
	body := fmt.Sprintf(
		"Cairn entry %s (topic %q, scope %s) is proposed for eviction (disuse).\n\n"+
			"Branch: %s\n\nThis branch deletes the entry's file. Merge it with plain git "+
			"(not `cairn review merge`, which does not handle a pure-deletion branch) when satisfied; "+
			"it does not auto-merge. Reject by simply not merging (or deleting the branch) to keep the entry.",
		e.ID, e.TopicKey, strings.Join(e.Scope, " "), branch)
	return mailSend(ctx, reviewer, subject, body)
}

// requestPromotionReview hands a promotion mark off for review -- e's scope
// has already resolved away from the private agent tier, so DESIGN.md §7's
// curation model applies to a promotion mark exactly as it does to any other
// shared-tier mutation. Mirrors requestReview/requestCullReview's three-step
// shape (commit to review branch, resolve a reviewer, mail). Callers must
// only invoke this for a scope that has already resolved away from the
// private agent tier.
func requestPromotionReview(cmd *cobra.Command, e *cairn.Entry) error {
	tier, value := cairn.ResolvedTier(e.Scope)

	branch, err := e.CommitPromotionToReviewBranch(cmd.Context(), storePath())
	if err != nil {
		return fmt.Errorf("commit promotion mark to review branch: %w", err)
	}
	fmt.Printf("review branch: %s\n", branch)

	reviewer, err := resolveReviewer(cmd, tier, value)
	if err != nil {
		return fmt.Errorf("entry %s committed to review branch %s, but resolving a reviewer to mail failed: %w", e.ID, branch, err)
	}
	if err := sendPromotionReviewMail(cmd.Context(), reviewer, e, branch); err != nil {
		return fmt.Errorf("entry %s committed to review branch %s, but mail to reviewer %q failed: %w", e.ID, branch, reviewer, err)
	}
	fmt.Printf("mailed reviewer: %s\n", reviewer)
	return nil
}

// sendPromotionReviewMail mirrors sendReviewMail's wording, not
// sendCullReviewMail's: a promotion-mark branch is a content change to an
// entry that already exists on the store's default branch, not a deletion,
// so a reviewer merges it the same way as any other pending remember/
// recurrence branch -- with `cairn review merge`, which review.go's
// patchFrontmatterFields never touches promoted_bead_id for, so no
// special-casing is needed at merge time either.
func sendPromotionReviewMail(ctx context.Context, reviewer string, e *cairn.Entry, branch string) error {
	subject := fmt.Sprintf("cairn promotion review: %s", e.TopicKey)
	body := fmt.Sprintf(
		"Cairn entry %s (topic %q, scope %s) has been marked promoted to bead %s and is ready for review.\n\n"+
			"Branch: %s\n\nMerge with `cairn review merge %s --topic-key %s` when satisfied; this branch does not auto-merge.",
		e.ID, e.TopicKey, strings.Join(e.Scope, " "), e.PromotedBeadID, branch, branch, e.TopicKey)
	return mailSend(ctx, reviewer, subject, body)
}
