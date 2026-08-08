package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().Bool("recursive", false, "also descend into subdirectories of <dir>")
}

var importCmd = &cobra.Command{
	Use:   "import <dir>",
	Short: "Batch-import cairn entry files from a directory, grouping review mail by resolved reviewer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		code, err := runImport(cmd, args)
		if err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
			os.Exit(code)
		}
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

// importedEntry is one successfully committed entry pending review, grouped
// under its resolved reviewer's manifest entry.
type importedEntry struct {
	id       string
	topicKey string
	branch   string
}

// runImport is importCmd's body, factored out as a plain function that never
// calls os.Exit itself, mirroring runDoctor's three-outcome convention (see
// doctor.go): (0, nil) every entry imported cleanly, (1, nil) one or more
// entries were skipped and reported, (2, err) the run itself could not
// complete (source directory or store unreadable).
func runImport(cmd *cobra.Command, args []string) (int, error) {
	dir := args[0]
	recursive, _ := cmd.Flags().GetBool("recursive")
	store := storePath()
	ctx := cmd.Context()

	files, err := collectImportFiles(dir, recursive)
	if err != nil {
		return 2, err
	}

	existing, err := cairn.IterEntries(store)
	if err != nil {
		return 2, err
	}
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.ID] = true
	}

	manifest := map[string][]importedEntry{}
	var failures []string
	imported := 0

	for _, file := range files {
		entry, err := cairn.ParseEntry(file)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file, err))
			logImportSkip(ctx, file, err.Error())
			continue
		}

		if seen[entry.ID] {
			failures = append(failures, fmt.Sprintf("%s: already exists in store, skipped", entry.ID))
			logImportSkip(ctx, entry.ID, "already exists in store")
			continue
		}

		if err := entry.Create(store); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.ID, err))
			continue
		}
		seen[entry.ID] = true

		branch, err := entry.CommitToReviewBranch(ctx, store)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.ID, err))
			continue
		}

		tier, value := cairn.ResolvedTier(entry.Scope)
		reviewer, err := defaultReviewer(tier, value)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.ID, err))
			continue
		}

		manifest[reviewer] = append(manifest[reviewer], importedEntry{
			id:       entry.ID,
			topicKey: entry.TopicKey,
			branch:   branch,
		})
		imported++
	}

	reviewers := make([]string, 0, len(manifest))
	for r := range manifest {
		reviewers = append(reviewers, r)
	}
	sort.Strings(reviewers)

	for _, reviewer := range reviewers {
		if err := sendBatchReviewMail(ctx, reviewer, manifest[reviewer], failures); err != nil {
			return 2, fmt.Errorf("mail reviewer %s: %w", reviewer, err)
		}
	}

	renderImportReport(cmd, imported, len(reviewers), failures)

	if len(failures) > 0 {
		return 1, nil
	}
	return 0, nil
}

// collectImportFiles lists the files under dir to attempt as import
// candidates, in sorted (deterministic) order: only dir's direct entries by
// default, or every file at any depth when recursive is set.
func collectImportFiles(dir string, recursive bool) ([]string, error) {
	var files []string
	if recursive {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// logImportSkip records an import-specific failure that never reaches
// CommitToReviewBranch (a parse failure or an ID collision), so it has no
// other obslog trail -- CommitToReviewBranch already logs its own outcome
// unconditionally (internal/cairn/remember.go), so a downstream commit
// failure needs no separate record here.
func logImportSkip(ctx context.Context, name, detail string) {
	obslog.FromContext(ctx).WritePathStep(obslog.WritePathStepFields{
		Operation: "import_skip",
		Name:      name,
		Outcome:   "error",
		Detail:    detail,
	})
}

// sendBatchReviewMail mirrors sendReviewMail's shape (cmd/reviewer.go) for a
// whole reviewer group at once: one line per entry (id, topic_key, branch),
// plus the batch's failure list when non-empty -- so a skipped entry is
// never visible only in a log line a reviewer would never read.
func sendBatchReviewMail(ctx context.Context, reviewer string, entries []importedEntry, failures []string) error {
	batchID := fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	subject := fmt.Sprintf("cairn import review: %d entries (batch %s)", len(entries), batchID)

	var body strings.Builder
	fmt.Fprintf(&body, "%d cairn entries were imported and committed for review.\n\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(&body, "%s (topic %q): %s\n", e.id, e.topicKey, e.branch)
	}
	body.WriteString("\nMerge each branch individually when satisfied; branches do not auto-merge.\n")

	if len(failures) > 0 {
		body.WriteString("\nFailures (not imported):\n")
		for _, f := range failures {
			fmt.Fprintf(&body, "%s\n", f)
		}
	}

	return mailSend(ctx, reviewer, subject, body.String())
}

// renderImportReport writes a summary of the run to cmd's output: how many
// entries were imported and mailed for review, and every failure by name --
// the same failure list sendBatchReviewMail includes in every group's mail.
func renderImportReport(cmd *cobra.Command, imported, reviewerGroups int, failures []string) {
	out := cmd.OutOrStdout()
	if len(failures) == 0 {
		fmt.Fprintf(out, "cairn import: %d entries imported across %d reviewer group(s), 0 failures\n", imported, reviewerGroups)
		return
	}
	fmt.Fprintf(out, "cairn import: %d entries imported across %d reviewer group(s), %d failure(s)\n", imported, reviewerGroups, len(failures))
	for _, f := range failures {
		fmt.Fprintf(out, "  !! %s\n", f)
	}
}
