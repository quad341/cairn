package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

type backfillRecord struct {
	ID                    string `json:"id"`
	TopicKey              string `json:"topic_key"`
	Body                  string `json:"body"`
	OriginalTitle         string `json:"original_title"`
	OriginalSummary       string `json:"original_summary"`
	OriginalTitleSource   string `json:"original_title_source"`
	OriginalSummarySource string `json:"original_summary_source"`
	BodySHA256            string `json:"body_sha256"`
	ProposedTitle         string `json:"proposed_title"`
	ProposedSummary       string `json:"proposed_summary"`
	Path                  string `json:"-"`
}

type classificationCounts struct {
	Total          int `json:"total"`
	Derived        int `json:"derived"`
	Authored       int `json:"authored"`
	Unclassifiable int `json:"unclassifiable"`
	Emitted        int `json:"emitted"`
}

var backfillCmd = &cobra.Command{Use: "backfill", Short: "Propose and apply retrieval-metadata backfills"}

func init() {
	rootCmd.AddCommand(backfillCmd)
	backfillCmd.AddCommand(backfillExportCmd, backfillValidateCmd, backfillApplyCmd)
	backfillExportCmd.Flags().Int("sample", 0, "emit a deterministic random sample of N eligible entries (0: all)")
	backfillExportCmd.Flags().Int64("seed", 1, "random seed used with --sample")
	backfillValidateCmd.Flags().String("file", "", "proposal JSONL file (required)")
	_ = backfillValidateCmd.MarkFlagRequired("file")
	backfillApplyCmd.Flags().String("file", "", "proposal JSONL file (required)")
	_ = backfillApplyCmd.MarkFlagRequired("file")
	backfillApplyCmd.Flags().Bool("write", false, "apply validated metadata changes (default: preview only)")
}

var backfillExportCmd = &cobra.Command{
	Use: "export", Short: "Emit legacy metadata work as JSONL", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		records, counts, err := collectBackfill(storePath())
		if err != nil {
			return err
		}
		sample, _ := cmd.Flags().GetInt("sample")
		seed, _ := cmd.Flags().GetInt64("seed")
		if sample > 0 && sample < len(records) {
			r := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible sampling, not security
			r.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })
			records = records[:sample]
			sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
		}
		counts.Emitted = len(records)
		enc := json.NewEncoder(cmd.OutOrStdout())
		for _, record := range records {
			if err := enc.Encode(record); err != nil {
				return err
			}
		}
		return json.NewEncoder(cmd.ErrOrStderr()).Encode(counts)
	},
}

var backfillValidateCmd = &cobra.Command{
	Use: "validate", Short: "Validate a completed proposal JSONL without writing", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		records, err := readBackfill(file)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := validateBackfillRecord(record); err != nil {
				return err
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"valid": true, "proposals": len(records)})
	},
}

type applyResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

var backfillApplyCmd = &cobra.Command{
	Use: "apply", Short: "Preview or apply a completed proposal JSONL", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		write, _ := cmd.Flags().GetBool("write")
		records, err := readBackfill(file)
		if err != nil {
			return err
		}
		results := make([]applyResult, 0, len(records))
		landed, failed := 0, 0
		for _, record := range records {
			result, applyErr := applyBackfillRecord(cmd.Context(), storePath(), record, write)
			results = append(results, result)
			if applyErr != nil {
				failed++
			} else if result.Status == "applied" {
				landed++
			}
		}
		if write && landed > 0 {
			if _, reindexErr := cairn.Reindex(cmd.Context(), storePath()); reindexErr != nil {
				failed++
				results = append(results, applyResult{Status: "reindex_failed", Detail: reindexErr.Error()})
			}
		}
		out := map[string]any{"write": write, "landed": landed, "failed": failed, "results": results}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(out); err != nil {
			return err
		}
		if failed != 0 {
			if !write {
				return fmt.Errorf("backfill preview rejected: %d stale or invalid proposals; inspect entry-level results", failed)
			}
			return fmt.Errorf("backfill partially applied: %d landed, %d failed; inspect entry-level results", landed, failed)
		}
		return nil
	},
}

func collectBackfill(store string) ([]backfillRecord, classificationCounts, error) {
	entries, err := cairn.IterEntries(store)
	if err != nil {
		return nil, classificationCounts{}, err
	}
	counts := classificationCounts{Total: len(entries)}
	var records []backfillRecord
	for _, e := range entries {
		class := classifyLegacyMetadata(e)
		switch class {
		case "derived":
			counts.Derived++
		case "authored":
			counts.Authored++
		default:
			counts.Unclassifiable++
		}
		if class != "derived" {
			continue
		}
		records = append(records, backfillRecord{
			ID: e.ID, TopicKey: e.TopicKey, Body: e.Body,
			OriginalTitle: e.Title, OriginalSummary: e.Summary,
			OriginalTitleSource: e.TitleSource, OriginalSummarySource: e.SummarySource,
			BodySHA256: bodyHash(e.Body), Path: e.BodyPath,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, counts, nil
}

func classifyLegacyMetadata(e *cairn.Entry) string {
	if e.TitleSource == cairn.MetadataSourceDerived || e.SummarySource == cairn.MetadataSourceDerived {
		return "derived"
	}
	if e.TitleSource == cairn.MetadataSourceAuthored && e.SummarySource == cairn.MetadataSourceAuthored {
		return "authored"
	}
	title, _ := cairn.DerivedTitleSummary(e.Body)
	// Title derivation is the retrieval-quality signal this migration targets.
	// Legacy summary algorithms changed over time, so requiring a simultaneous
	// summary match hides otherwise exact evidence that title was synthesized.
	legacyTitle := normalizeLegacyTitle(e.Title)
	firstLine := strings.TrimSpace(e.Body)
	if before, _, ok := strings.Cut(firstLine, "\n"); ok {
		firstLine = strings.TrimSpace(before)
	}
	derivedTitle := normalizeLegacyTitle(firstLine)
	if e.Title == title || legacyTitle == derivedTitle ||
		(len(legacyTitle) >= 50 && strings.HasPrefix(derivedTitle, legacyTitle)) {
		return "derived"
	}
	return "unclassifiable"
}

func normalizeLegacyTitle(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '`', '*', '_', '#', '>':
			return -1
		default:
			return unicode.ToLower(r)
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func readBackfill(path string) ([]backfillRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []backfillRecord
	seen := make(map[string]bool)
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for line := 1; scan.Scan(); line++ {
		var r backfillRecord
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("%s:%d: duplicate proposal for entry %s", path, line, r.ID)
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out, scan.Err()
}

func validateBackfillRecord(r backfillRecord) error {
	if r.ID == "" {
		return fmt.Errorf("proposal has empty id")
	}
	if len(r.BodySHA256) != sha256.Size*2 {
		return fmt.Errorf("entry %s: body_sha256 must be a 64-character digest", r.ID)
	}
	if strings.TrimSpace(r.ProposedTitle) == "" {
		return fmt.Errorf("entry %s: proposed_title is required", r.ID)
	}
	if strings.TrimSpace(r.ProposedSummary) == "" {
		return fmt.Errorf("entry %s: proposed_summary is required", r.ID)
	}
	if err := cairn.ValidateTitleLength(r.ProposedTitle); err != nil {
		return fmt.Errorf("entry %s title: %w", r.ID, err)
	}
	if err := cairn.ValidateSummaryLength(r.ProposedSummary); err != nil {
		return fmt.Errorf("entry %s summary: %w", r.ID, err)
	}
	return nil
}

func applyBackfillRecord(ctx context.Context, store string, r backfillRecord, write bool) (applyResult, error) {
	if err := validateBackfillRecord(r); err != nil {
		return applyResult{ID: r.ID, Status: "failed", Detail: err.Error()}, err
	}
	e, err := cairn.Find(ctx, store, r.ID)
	if err != nil {
		return applyResult{ID: r.ID, Status: "failed", Detail: err.Error()}, err
	}
	if e.Title == r.ProposedTitle && e.Summary == r.ProposedSummary &&
		e.TitleSource == cairn.MetadataSourceAuthored && e.SummarySource == cairn.MetadataSourceAuthored {
		return applyResult{ID: r.ID, Status: "already_applied"}, nil
	}
	var diverged []string
	if e.Title != r.OriginalTitle {
		diverged = append(diverged, "title")
	}
	if e.Summary != r.OriginalSummary {
		diverged = append(diverged, "summary")
	}
	if e.TitleSource != r.OriginalTitleSource {
		diverged = append(diverged, "title_source")
	}
	if e.SummarySource != r.OriginalSummarySource {
		diverged = append(diverged, "summary_source")
	}
	if bodyHash(e.Body) != r.BodySHA256 {
		diverged = append(diverged, "body")
	}
	if len(diverged) > 0 {
		err = fmt.Errorf("entry %s is stale: diverged fields: %s", r.ID, strings.Join(diverged, ", "))
		return applyResult{ID: r.ID, Status: "stale", Detail: err.Error()}, err
	}
	if !write {
		detail := fmt.Sprintf("title: %q -> %q; summary: %q -> %q",
			e.Title, r.ProposedTitle, e.Summary, r.ProposedSummary)
		return applyResult{ID: r.ID, Status: "preview", Detail: detail}, nil
	}
	e.Title = r.ProposedTitle
	e.Summary = r.ProposedSummary
	if err = e.WriteBackRetrievalMetadata(); err != nil {
		return applyResult{ID: r.ID, Status: "failed", Detail: err.Error()}, err
	}
	return applyResult{ID: r.ID, Status: "applied"}, nil
}
