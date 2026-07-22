package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	staleBranchesCmd.Flags().Duration("notify-after", 24*time.Hour,
		"age at which an unmerged review branch gets a reactive reminder mailed to its reviewer")
	staleBranchesCmd.Flags().Duration("escalate-after", 72*time.Hour,
		"age at which an unmerged review branch is reported ready for bead escalation (no further mail is sent)")
	staleBranchesCmd.Flags().Bool("dry-run", false,
		"compute and report status without actually mailing a reviewer")
	staleBranchesCmd.Flags().String("reviewer", "",
		"reviewer to mail for every notify-status branch (default: $CAIRN_REVIEWER, else a per-tier computed default)")
	rootCmd.AddCommand(staleBranchesCmd)
}

var staleBranchesCmd = &cobra.Command{
	Use:   "stale-branches",
	Short: "Unmerged review branches by age, JSON (read-only detection + reactive re-notify; librarian maintenance use)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("stale-branches covers every shared-tier review branch and does not filter by identity")
		}
		notifyAfter, _ := cmd.Flags().GetDuration("notify-after")
		escalateAfter, _ := cmd.Flags().GetDuration("escalate-after")
		if escalateAfter <= notifyAfter {
			return fmt.Errorf("--escalate-after (%s) must be greater than --notify-after (%s)", escalateAfter, notifyAfter)
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		reviewBranches, err := cairn.ListReviewBranches(cmd.Context(), storePath(), time.Now())
		if err != nil {
			return err
		}

		findings := make([]StaleBranchFinding, 0, len(reviewBranches))
		for _, b := range reviewBranches {
			findings = append(findings, evaluateBranch(cmd, b, notifyAfter, escalateAfter, dryRun))
		}

		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(findings)
	},
}

// StaleBranchFinding is one remember/* review branch's status as of a
// stale-branches pass: every branch ListReviewBranches reports is included
// here (fresh/notify/escalate/error alike), mirroring cairn sweep's
// report-everything-let-the-caller-filter convention -- the escalate/bd-bead
// filing step downstream needs to see escalate findings, not just notify
// ones.
type StaleBranchFinding struct {
	Branch     string `json:"branch"`
	EntryID    string `json:"entry_id"`
	Tier       string `json:"tier"`
	Value      string `json:"value,omitempty"`
	AgeSeconds int64  `json:"age_seconds"`
	Status     string `json:"status"` // fresh | notify | escalate | error
	Reviewer   string `json:"reviewer,omitempty"`
	Notified   bool   `json:"notified"`
	Error      string `json:"error,omitempty"`
}

// evaluateBranch buckets b by age and, for a notify-status branch, resolves
// and (unless dryRun) mails its reviewer a reminder. It also resolves (but
// never mails) the reviewer for an escalate-status branch -- the downstream
// bd-bead-filing step wants to name who was already reminded and didn't
// act, not just that nobody did.
func evaluateBranch(cmd *cobra.Command, b cairn.ReviewBranch, notifyAfter, escalateAfter time.Duration, dryRun bool) StaleBranchFinding {
	f := StaleBranchFinding{
		Branch:     b.Name,
		EntryID:    b.EntryID,
		Tier:       b.Tier,
		Value:      b.Value,
		AgeSeconds: int64(b.Age.Seconds()),
	}
	if b.Error != "" {
		f.Status = "error"
		f.Error = b.Error
		return f
	}

	f.Status = branchStatus(b.Age, notifyAfter, escalateAfter)
	if f.Status == "fresh" {
		return f
	}

	reviewer, err := resolveReviewer(cmd, b.Tier, b.Value)
	if err != nil {
		f.Error = fmt.Sprintf("resolve reviewer: %v", err)
		return f
	}
	f.Reviewer = reviewer

	if f.Status == "notify" && !dryRun {
		if err := sendStaleBranchReminder(cmd.Context(), reviewer, b); err != nil {
			f.Error = fmt.Sprintf("send reminder: %v", err)
			return f
		}
		f.Notified = true
	}
	return f
}

// branchStatus buckets age against the two caller-supplied thresholds.
// escalateAfter is checked first: it is always the larger threshold (RunE
// rejects escalateAfter <= notifyAfter), and an escalate-eligible branch is
// definitionally also notify-eligible, but escalate must win so a branch
// stale enough to file a bead over doesn't also get an every-pass reminder
// mail once it's there.
func branchStatus(age, notifyAfter, escalateAfter time.Duration) string {
	switch {
	case age >= escalateAfter:
		return "escalate"
	case age >= notifyAfter:
		return "notify"
	default:
		return "fresh"
	}
}

// sendStaleBranchReminder mails reviewer a reminder distinct from
// sendReviewMail's initial "ready for review" notice -- it leads with the
// branch's age, since this call only ever fires on a repeat sweep pass, not
// the first time a reviewer hears about the branch.
func sendStaleBranchReminder(ctx context.Context, reviewer string, b cairn.ReviewBranch) error {
	subject := fmt.Sprintf("cairn review branch stale: %s", b.EntryID)
	body := fmt.Sprintf(
		"Review branch %s has been awaiting review for %s.\n\n"+
			"Merge into the store's default branch when satisfied; this branch does not auto-merge.",
		b.Name, b.Age.Round(time.Minute))
	return mailSend(ctx, reviewer, subject, body)
}
