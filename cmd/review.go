package cmd

import (
	"fmt"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(reviewCmd)
	reviewCmd.AddCommand(reviewListCmd, reviewShowCmd, reviewMergeCmd)

	reviewListCmd.Flags().String("tier", "", "filter to one tier: global|rig|role")

	reviewMergeCmd.Flags().String("topic-key", "", "canonical topic_key to assign at merge (required)")
	reviewMergeCmd.Flags().String("anchor-type", "", "anchor type to set (default: leave as the contributor wrote it)")
	reviewMergeCmd.Flags().String("scope", "", "comma-separated scope tags to set (default: leave as the contributor wrote it)")
	reviewMergeCmd.Flags().String("bead", "", "bead id for the merge commit message (default: the --topic-key value)")
	reviewMergeCmd.Flags().Bool("allow-secret-pattern", false,
		"override the secret-pattern merge guard, after confirming a false positive via 'review show'")
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Reviewer actions on pending shared-tier remember/* branches",
}

var reviewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending remember/* review branches",
	RunE: func(cmd *cobra.Command, _ []string) error {
		tier, _ := cmd.Flags().GetString("tier")
		branches, err := cairn.ListReviewMergeBranches(cmd.Context(), storePath())
		if err != nil {
			return err
		}
		for _, b := range branches {
			if tier != "" && b.Tier != tier {
				continue
			}
			value := b.TierValue
			if value == "" {
				value = "-"
			}
			fmt.Printf("%s\ttier=%s\tvalue=%s\t%s\n", b.Name, b.Tier, value, b.EntryPath)
		}
		return nil
	},
}

var reviewShowCmd = &cobra.Command{
	Use:   "show <branch>",
	Short: "Show a review branch's diff and parsed entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		diff, e, err := cairn.ShowReviewBranch(cmd.Context(), storePath(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(diff)
		fmt.Println("--- entry ---")
		fmt.Printf("id:        %s\n", e.ID)
		fmt.Printf("title:     %s\n", e.Title)
		fmt.Printf("topic_key: %s\n", e.TopicKey)
		scope := "(global)"
		if len(e.Scope) > 0 {
			scope = strings.Join(e.Scope, " ")
		}
		fmt.Printf("scope:     %s\n", scope)
		fmt.Printf("anchor:    type=%s repo=%s paths=%v spec=%s\n", e.Anchor.Type, e.Anchor.Repo, e.Anchor.Paths, e.Anchor.Spec)
		return nil
	},
}

var reviewMergeCmd = &cobra.Command{
	Use:   "merge <branch>",
	Short: "Curate and merge a reviewed branch into the default branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		topicKey, _ := cmd.Flags().GetString("topic-key")
		anchorType, _ := cmd.Flags().GetString("anchor-type")
		scopeRaw, _ := cmd.Flags().GetString("scope")
		bead, _ := cmd.Flags().GetString("bead")
		allowSecret, _ := cmd.Flags().GetBool("allow-secret-pattern")

		opts := cairn.ReviewMergeOptions{
			TopicKey:           topicKey,
			AnchorType:         anchorType,
			Bead:               bead,
			AllowSecretPattern: allowSecret,
		}
		if scopeRaw != "" {
			opts.Scope = strings.Split(scopeRaw, ",")
		}

		res, err := cairn.MergeReviewBranch(cmd.Context(), storePath(), args[0], opts)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", res.SHA)
		return nil
	},
}
