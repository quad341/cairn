package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(reindexCmd, mapCmd, statusCmd, freshnessCmd, verifyCmd, getCmd)
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the SQLite index from the bodies (disposable materialized view)",
	Args:  cobra.NoArgs,
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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("status is unscoped and does not filter by identity; use 'cairn map' or 'cairn prime' for a scoped view")
		}
		entries, err := cairn.Status(cmd.Context(), storePath())
		if err != nil {
			return err
		}
		shadowedBy := cairn.ShadowMap(entries)
		flags := map[string]string{cairn.Fresh: "OK ", cairn.Stale: "!! ", cairn.Unknown: "?? "}
		for _, e := range entries {
			st, detail := cairn.Check(cmd.Context(), e)
			line := fmt.Sprintf("%s%-38s %-8s %s", flags[st], e.ID, st, detail)
			if by, ok := shadowedBy[e.ID]; ok {
				line += fmt.Sprintf("  [SHADOWED BY %s]", by.ID)
			}
			fmt.Println(line)
		}
		return nil
	},
}

var freshnessCmd = &cobra.Command{
	Use:   "freshness <id>",
	Short: "Freshness of one entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(cmd.Context(), storePath(), args[0])
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

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Pull an entry's full body + freshness (direct by-id lookup, bypasses scope)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(cmd.Context(), storePath(), args[0])
		if errors.Is(err, cairn.ErrNotFound) {
			return fmt.Errorf("no entry %q", args[0])
		}
		if err != nil {
			return err
		}
		st, detail := cairn.Check(cmd.Context(), e)
		topic := e.TopicKey
		if topic == "" {
			topic = "(untopiced)"
		}
		scope := "global"
		if len(e.Scope) > 0 {
			scope = strings.Join(e.Scope, " ")
		}
		fmt.Printf("%s: %s\n", e.ID, e.Title)
		fmt.Printf("topic: %s  scope: %s\n", topic, scope)
		fmt.Printf("freshness: %s — %s\n", st, detail)

		kind := e.Kind
		if kind == "" {
			kind = "note"
		}
		fmt.Printf("kind: %s  auto_actionable: %t\n", kind, e.AutoActionable)

		identity := resolveIdentity(cmd)
		visible, err := cairn.Visible(cmd.Context(), storePath(), identity)
		if err != nil {
			return err
		}
		// Visible only populates the fields shadow/scope resolution needs
		// (ID, TopicKey, Scope, VerifiedAt, CreatedAt, Anchor) — Title and
		// Summary are always zero-valued there, which would make Conflicts'
		// content-similarity signal silently never match. IterEntries fully
		// parses every entry (the same data source Dedup itself scans), so
		// resolving the visible ID set against it gives Conflicts real
		// Title/Summary data without the hit_count side effect a per-ID
		// Find call would have on every other visible entry.
		visibleIDs := make(map[string]bool, len(visible))
		for _, v := range visible {
			visibleIDs[v.ID] = true
		}
		all, err := cairn.IterEntries(storePath())
		if err != nil {
			return err
		}
		others := make([]*cairn.Entry, 0, len(visibleIDs))
		for _, full := range all {
			if visibleIDs[full.ID] {
				others = append(others, full)
			}
		}
		conflicts := cairn.Conflicts(e, others)
		if len(conflicts) == 0 {
			fmt.Println("conflicts: none")
		} else {
			fmt.Printf("conflicts: %d\n", len(conflicts))
			for _, c := range conflicts {
				other := c.EntryIDs[0]
				if other == e.ID {
					other = c.EntryIDs[1]
				}
				if c.Kind == "content" {
					fmt.Printf("  - %s: %s (similarity %.2f)\n", c.Kind, other, c.Similarity)
				} else {
					fmt.Printf("  - %s: %s\n", c.Kind, other)
				}
			}
		}

		fmt.Println()
		fmt.Print(e.Body)
		return nil
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify <id>",
	Short: "Recompute + write back an entry's anchor fingerprint (mark verified)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(cmd.Context(), storePath(), args[0])
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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity := resolveIdentity(cmd)
		rows, err := cairn.Visible(cmd.Context(), storePath(), identity)
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
