package cmd

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(reindexCmd, mapCmd, statusCmd, freshnessCmd, verifyCmd)
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the SQLite index from the bodies (disposable materialized view)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		n, err := cairn.Reindex(cmd.Context(), storePath())
		if err != nil {
			return err
		}
		fmt.Printf("reindexed %d entries -> %s\n", n, cairn.IndexPath(storePath()))
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Freshness of every entry",
	RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := cairn.IterEntries(storePath())
		if err != nil {
			return err
		}
		flags := map[string]string{cairn.Fresh: "OK ", cairn.Stale: "!! ", cairn.Unknown: "?? "}
		for _, e := range entries {
			st, detail := cairn.Check(cmd.Context(), e)
			fmt.Printf("%s%-38s %-8s %s\n", flags[st], e.ID, st, detail)
		}
		return nil
	},
}

var freshnessCmd = &cobra.Command{
	Use:   "freshness <id>",
	Short: "Freshness of one entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(storePath(), args[0])
		if errors.Is(err, cairn.ErrNotFound) {
			return fmt.Errorf("no entry %q", args[0])
		}
		if err != nil {
			return err
		}
		st, detail := cairn.Check(cmd.Context(), e)
		fmt.Printf("%s: %s — %s\n", args[0], st, detail)
		return nil
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify <id>",
	Short: "Recompute + write back an entry's anchor fingerprint (mark verified)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(storePath(), args[0])
		if errors.Is(err, cairn.ErrNotFound) {
			return fmt.Errorf("no entry %q", args[0])
		}
		if err != nil {
			return err
		}
		fp := cairn.ComputeFingerprint(cmd.Context(), e.Anchor)
		if fp == "" {
			return fmt.Errorf("%s: nothing to verify (anchor type %q has no computable fingerprint)", args[0], e.Anchor.Type)
		}
		e.Anchor.Fingerprint = fp
		e.VerifiedAt = time.Now().Format(time.DateOnly)
		if err := e.WriteBack(); err != nil {
			return err
		}
		fmt.Printf("verified %s: fingerprint %s @ %s (written back)\n", args[0], fp, e.VerifiedAt)
		return nil
	},
}

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Bounded topic map for an identity (the always-in-context payload)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity := resolveIdentity(cmd)
		rows, err := cairn.Visible(storePath(), identity)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, e := range rows {
			t := e.TopicKey
			if t == "" {
				t = "(untopiced)"
			}
			counts[t]++
		}
		topics := make([]string, 0, len(counts))
		for t := range counts {
			topics = append(topics, t)
		}
		sort.Strings(topics)
		fmt.Printf("# cairn map — %d entries visible to identity %v\n", len(rows), identity)
		for _, t := range topics {
			fmt.Printf("  %s  (%d)\n", t, counts[t])
		}
		return nil
	},
}
