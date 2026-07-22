package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	staleBranchesCmd.Flags().String("state-file", "",
		"path to the JSON file tracking prior notifies, keyed by branch+commit (default: <store>/.git/cairn-stale-branches-state.json)")
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

		statePath := stateFilePath(cmd)
		state, err := loadNotifyState(statePath)
		if err != nil {
			return err
		}

		findings := make([]StaleBranchFinding, 0, len(reviewBranches))
		for _, b := range reviewBranches {
			findings = append(findings, evaluateBranch(cmd, b, notifyAfter, escalateAfter, dryRun, state))
		}

		if !dryRun {
			if err := saveNotifyState(statePath, state); err != nil {
				return err
			}
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

// notifyState maps a review branch's name to the tip SHA it was last
// successfully notified at. evaluateBranch consults it to require at least
// one prior notify at the branch's *current* tip before ever reporting
// escalate -- a fresh commit (a different SHA) is indistinguishable from a
// never-before-seen branch, so it must be renotified before it can escalate
// again too (crn-3l6).
type notifyState map[string]string

// stateFilePath resolves --state-file (flag override, else a default rooted
// at storePath()'s .git directory -- unconditionally excluded from any
// commit regardless of that store repo's own .gitignore content, unlike
// index/cairn.sqlite's convention).
//
// This default only carries memory across sweep passes if the same store
// checkout is reused call to call. A caller that re-clones store fresh every
// sweep tick must pass --state-file pointing at a durable path outside the
// ephemeral checkout, or this command's cross-pass memory is lost every
// cycle.
func stateFilePath(cmd *cobra.Command) string {
	if f, _ := cmd.Flags().GetString("state-file"); f != "" {
		return f
	}
	return filepath.Join(storePath(), ".git", "cairn-stale-branches-state.json")
}

// loadNotifyState reads path's persisted notifyState. A missing file --
// the common case, since it means no sweep pass has ever recorded a notify
// -- is not an error.
func loadNotifyState(path string) (notifyState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return notifyState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file %s: %w", path, err)
	}
	state := notifyState{}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", path, err)
	}
	return state, nil
}

// saveNotifyState persists state to path as JSON, creating path's parent
// directory if needed -- a store's own .git directory always already
// exists by the time this runs, but an explicit --state-file override
// pointing elsewhere might not.
func saveNotifyState(path string, state notifyState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state file directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write state file %s: %w", path, err)
	}
	return nil
}

// evaluateBranch buckets b by age and, for a notify-status branch, resolves
// and (unless dryRun) mails its reviewer a reminder. It also resolves (but
// never mails) the reviewer for an escalate-status branch -- the downstream
// bd-bead-filing step wants to name who was already reminded and didn't
// act, not just that nobody did.
//
// A raw escalate bucket is downgraded to notify whenever state has no
// record of a prior notify at b's exact current SHA: escalate must never be
// reachable on a branch's first-ever observed pass (or its first pass since
// a new commit landed), since nobody has been given a chance to act on a
// reminder yet (crn-3l6, crn-0yv.1 AC2). state is mutated in place -- a
// successful notify (whether from a raw notify bucket or a downgraded one)
// records b's SHA, so a later pass over the same tip can escalate.
func evaluateBranch(cmd *cobra.Command, b cairn.ReviewBranch, notifyAfter, escalateAfter time.Duration,
	dryRun bool, state notifyState,
) StaleBranchFinding {
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
	if f.Status == "escalate" && state[b.Name] != b.SHA {
		f.Status = "notify"
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
		state[b.Name] = b.SHA
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
