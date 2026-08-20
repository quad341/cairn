package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
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

// StatusItem is one entry's status-line summary in cairn status --json's
// output. Bare array, no wrapper object: status's human mode prints no
// header/metadata either, just one line per entry.
type StatusItem struct {
	ID         string              `json:"id"`
	TopicKey   string              `json:"topic_key"`
	VerifiedAt string              `json:"verified_at,omitempty"`
	CreatedAt  string              `json:"created_at,omitempty"`
	Freshness  cairn.FreshnessInfo `json:"freshness"`
	ShadowedBy string              `json:"shadowed_by,omitempty"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Freshness of every entry",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return emitError(cmd, classifiedErr(CategoryInvalidInput, "",
				fmt.Errorf("status is unscoped and does not filter by identity; use 'cairn map' or 'cairn prime' for a scoped view")))
		}
		entries, err := cairn.Status(cmd.Context(), storePath())
		if err != nil {
			return emitError(cmd, err)
		}
		shadowedBy := cairn.ShadowMap(cmd.Context(), entries)

		if wantsJSON(cmd) {
			items := make([]StatusItem, 0, len(entries))
			for _, e := range entries {
				st, detail := cairn.Check(cmd.Context(), e)
				item := StatusItem{
					ID:         e.ID,
					TopicKey:   e.TopicKey,
					VerifiedAt: e.VerifiedAt,
					CreatedAt:  e.CreatedAt,
					Freshness:  cairn.FreshnessInfo{Status: st, Detail: detail},
				}
				if by, ok := shadowedBy[e.ID]; ok {
					item.ShadowedBy = by.ID
				}
				items = append(items, item)
			}
			return emitJSON(cmd.OutOrStdout(), nonNil(items))
		}

		flags := map[string]string{cairn.Fresh: "OK ", cairn.Stale: "!! ", cairn.Unknown: "?? ", cairn.Incomplete: "!X "}
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
			return fmt.Errorf("no entry %q: %w", args[0], err)
		}
		if err != nil {
			return err
		}
		st, detail := cairn.Check(cmd.Context(), e)
		if st == cairn.Incomplete {
			return fmt.Errorf("%s: %s", args[0], detail)
		}
		fmt.Printf("%s: %s — %s\n", args[0], st, detail)
		return nil
	},
}

// payloadTokenCharsPerToken and payloadTokensErrorBoundPct are calibrated
// from real measured data (crn-666s.1), not assumed: chars-per-token from
// n=418 text-only assistant turns (no thinking, no tool_use) across local
// Claude Code transcripts, real usage.output_tokens grouped by message.id.
// Median 2.64, p5=2.27/p95=3.09 -> ~17% envelope (rounded up from the
// larger of the two tails). See RetrievalOutcomeFields's doc comment for
// the full methodology, including the two contaminated pairings this
// supersedes.
const (
	payloadTokenCharsPerToken  = 2.64
	payloadTokensErrorBoundPct = 17
)

// EntryResult is cairn get --json's top-level shape.
type EntryResult struct {
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	TopicKey       string               `json:"topic_key"`
	Scope          []string             `json:"scope"`
	Freshness      cairn.FreshnessInfo  `json:"freshness"`
	Kind           string               `json:"kind"`
	Type           string               `json:"type"`
	AutoActionable bool                 `json:"auto_actionable"`
	Conflicts      []cairn.DedupFinding `json:"conflicts"`
	Body           string               `json:"body"`
	// RedirectedFrom is the originally-requested id when it was redirected
	// to an entry that explicitly Corrects it (crn-evw98.3); empty when no
	// redirect occurred.
	RedirectedFrom string `json:"redirected_from,omitempty"`
}

var getCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Pull an entry's full body + freshness (direct by-id lookup, bypasses scope)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := cairn.Find(cmd.Context(), storePath(), args[0])
		if errors.Is(err, cairn.ErrNotFound) {
			obslog.FromContext(cmd.Context()).RetrievalOutcome(obslog.RetrievalOutcomeFields{
				IdentityTags: resolveIdentity(cmd),
				RunID:        resolveRunID(cmd),
				Outcome:      "miss",
				EntryID:      args[0],
			})
			return emitError(cmd, classifiedErr(CategoryNotFound, args[0], fmt.Errorf("no entry %q: %w", args[0], err)))
		}
		if err != nil {
			return emitError(cmd, err)
		}

		// A redirect is resolved before any telemetry/freshness/conflict
		// work below, so every downstream signal (obslog outcome, payload
		// tokens, reuse count, conflicts) reflects what was actually served
		// -- the corrector, when one exists -- not the superseded entry
		// that was originally requested (crn-evw98.3).
		var redirectedFrom string
		if corrector, cerr := cairn.FindCorrection(cmd.Context(), storePath(), e.ID); cerr != nil {
			return emitError(cmd, cerr)
		} else if corrector != nil {
			redirectedFrom = e.ID
			e = corrector
		}

		st, detail := cairn.Check(cmd.Context(), e)

		outcome := "hit"
		if st == cairn.Stale {
			outcome = "stale"
		}
		obslog.FromContext(cmd.Context()).RetrievalOutcome(obslog.RetrievalOutcomeFields{
			IdentityTags:               resolveIdentity(cmd),
			RunID:                      resolveRunID(cmd),
			Outcome:                    outcome,
			EntryID:                    e.ID,
			PayloadTokens:              int(float64(len(e.Body)) / payloadTokenCharsPerToken),
			PayloadTokensMethod:        "calibrated_chars_per_token_v1",
			PayloadTokensErrorBoundPct: payloadTokensErrorBoundPct,
			ReuseCount:                 e.HitCount,
		})

		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitError(cmd, err)
		}
		visible, err := cairn.Visible(cmd.Context(), storePath(), identity)
		if err != nil {
			return emitError(cmd, err)
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
			return emitError(cmd, err)
		}
		others := make([]*cairn.Entry, 0, len(visibleIDs))
		for _, full := range all {
			if visibleIDs[full.ID] {
				others = append(others, full)
			}
		}
		conflicts := cairn.Conflicts(e, others)

		kind := e.Kind
		if kind == "" {
			if e.Type == cairn.EntryTypeRemediation {
				kind = cairn.EntryTypeRemediation
			} else {
				kind = "note"
			}
		}

		if wantsJSON(cmd) {
			return emitJSON(cmd.OutOrStdout(), EntryResult{
				ID:             e.ID,
				Title:          e.Title,
				TopicKey:       e.TopicKey,
				Scope:          nonNil(e.Scope),
				Freshness:      cairn.FreshnessInfo{Status: st, Detail: detail},
				Kind:           kind,
				Type:           e.Type,
				AutoActionable: e.AutoActionable,
				Conflicts:      nonNil(conflicts),
				Body:           e.Body,
				RedirectedFrom: redirectedFrom,
			})
		}

		topic := e.TopicKey
		if topic == "" {
			topic = cairn.UntopicedLabel
		}
		scope := "global"
		if len(e.Scope) > 0 {
			scope = strings.Join(e.Scope, " ")
		}
		fmt.Printf("%s: %s\n", e.ID, e.Title)
		if redirectedFrom != "" {
			fmt.Printf("redirected_from: %s\n", redirectedFrom)
		}
		fmt.Printf("topic: %s  scope: %s\n", topic, scope)
		fmt.Printf("freshness: %s — %s\n", st, detail)
		fmt.Printf("type: %s  kind: %s  auto_actionable: %t\n", e.Type, kind, e.AutoActionable)

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
			return fmt.Errorf("no entry %q: %w", args[0], err)
		}
		if err != nil {
			return err
		}
		// Containment guard (crn-2c8e), matching anchor: a copied store's
		// index can retain body_paths into its origin. Verify must write only
		// beneath the store explicitly addressed by --store.
		contained, err := containedBodyPath(storePath(), e.BodyPath)
		if err != nil {
			return err
		}
		e.BodyPath = contained
		fp, err := cairn.ComputeFingerprint(cmd.Context(), e.Anchor)
		if err != nil {
			return fmt.Errorf("%s: git check did not complete: %w", args[0], err)
		}
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

// MapTopicCount is one topic's visible-entry count in cairn map --json output.
type MapTopicCount struct {
	TopicKey string `json:"topic_key"`
	Count    int    `json:"count"`
}

// MapResult is cairn map --json's top-level shape.
type MapResult struct {
	Identity []string        `json:"identity"`
	Total    int             `json:"total"`
	Topics   []MapTopicCount `json:"topics"`
}

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Bounded topic map for an identity (the always-in-context payload)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		identity, err := resolveIdentityValidated(cmd)
		if err != nil {
			return emitError(cmd, err)
		}
		rows, err := cairn.Visible(cmd.Context(), storePath(), identity)
		if err != nil {
			return emitError(cmd, err)
		}
		counts := map[string]int{}
		for _, e := range rows {
			t := e.TopicKey
			if t == "" {
				t = cairn.UntopicedLabel
			}
			counts[t]++
		}
		topics := make([]string, 0, len(counts))
		for t := range counts {
			topics = append(topics, t)
		}
		sort.Strings(topics)

		if wantsJSON(cmd) {
			mapTopics := make([]MapTopicCount, 0, len(topics))
			for _, t := range topics {
				mapTopics = append(mapTopics, MapTopicCount{TopicKey: t, Count: counts[t]})
			}
			return emitJSON(cmd.OutOrStdout(), MapResult{
				Identity: nonNil(identity),
				Total:    len(rows),
				Topics:   nonNil(mapTopics),
			})
		}

		fmt.Printf("# cairn map — %d entries visible to identity %v\n", len(rows), identity)
		for _, t := range topics {
			fmt.Printf("  %s  (%d)\n", t, counts[t])
		}
		return nil
	},
}
