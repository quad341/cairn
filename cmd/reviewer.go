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
		return fmt.Errorf("commit shared-tier entry to review branch: %w", err)
	}
	fmt.Printf("review branch: %s\n", branch)

	reviewer, err := resolveReviewer(cmd, tier, value)
	if err != nil {
		return fmt.Errorf("entry %s committed to review branch %s, but resolving a reviewer to mail failed: %w", e.ID, branch, err)
	}
	if err := sendReviewMail(cmd.Context(), reviewer, e, branch); err != nil {
		return fmt.Errorf("entry %s committed to review branch %s, but mail to reviewer %q failed: %w", e.ID, branch, reviewer, err)
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
// override that is empty or carries a control/null byte, rather than
// silently passing a garbage recipient through to `gc mail send`. It
// deliberately does not reuse cairn.ValidatePathSegment: a real reviewer
// address contains a slash (e.g. "myrig/architect"), which that validator
// rejects outright. An unset override is not a misconfiguration -- that
// path never reaches this function.
func validateReviewerAddress(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", errors.New("reviewer address must not be empty")
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
	out, err := exec.CommandContext(ctx, "gc", "mail", "send", reviewer, "-s", subject, "-m", body).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gc mail send %s: %w: %s", reviewer, err, strings.TrimSpace(string(out)))
	}
	return nil
}
