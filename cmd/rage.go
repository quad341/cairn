package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
	rageCmd.Flags().Bool("include-bodies", false,
		"embed every entry's full body text in the bundle (off by default -- body text may contain sensitive project details)")
	rageCmd.Flags().Int("log-bytes", 32768, "how many trailing bytes of the debug log to include in the bundle")
	rageCmd.Flags().String("failed-cmd", "", "explicitly name the command that failed, overriding auto-detection from the log tail")
	rageCmd.Flags().Int("exit-code", -1, "exit code for --failed-cmd (sentinel -1 means unset; 0 is a valid real exit code)")
	rootCmd.AddCommand(rageCmd)
}

// rageNotifyAfter and rageEscalateAfter mirror stale-branches's own default
// thresholds (branches.go's init()) -- rage has no exposed threshold flags
// of its own, since a bug-report bundle isn't a tunable maintenance pass.
const (
	rageNotifyAfter   = 24 * time.Hour
	rageEscalateAfter = 72 * time.Hour
)

// rageIssueRepo is the repo rage's generated "file an issue" link points at.
const rageIssueRepo = "quad341/cairn"

var rageCmd = &cobra.Command{
	Use:   "rage",
	Short: "Bundle a markdown diagnostic snapshot (version, store shape, doctor findings, log tail) for filing a bug report",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("rage always covers the whole store and does not support --identity or $CAIRN_IDENTITY")
		}
		return runRageCmd(cmd)
	},
}

// runRageCmd assembles the markdown bundle and prints exactly two lines to
// stdout: the bundle's file path, then a prefilled GitHub issue URL that
// references it. It never makes an outbound network call and never prints
// the bundle's own content to stdout, so stdout stays small and constant
// regardless of store or log size.
func runRageCmd(cmd *cobra.Command) error {
	ctx := cmd.Context()
	store := storePath()
	_, storeSource := storePathWithSource()
	_, identitySource := identityWithSource(cmd)

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "# cairn rage bundle\n\n")
	_, _ = fmt.Fprintf(&b, "generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	_, _ = fmt.Fprintf(&b, "## Version\n\n")
	_, _ = fmt.Fprintf(&b, "version: %s\ncommit: %s\ndate: %s\n\n", version, commit, date)

	_, _ = fmt.Fprintf(&b, "## Store\n\n")
	_, _ = fmt.Fprintf(&b, "path: %s (source: %s)\n", store, storeSource)
	_, _ = fmt.Fprintf(&b, "identity source: %s\n\n", identitySource)

	writeDoctorFindings(&b, ctx, store)
	writeStoreShape(&b, ctx, store)
	writeReviewBranches(&b, ctx, store)

	logBytes, _ := cmd.Flags().GetInt("log-bytes")
	tail := readLogTail(logBytes)
	writeLastFailingCommand(&b, cmd, tail)
	_, _ = fmt.Fprintf(&b, "## Log tail\n\n```\n%s\n```\n\n", string(tail))

	if includeBodies, _ := cmd.Flags().GetBool("include-bodies"); includeBodies {
		writeEntryBodies(&b, store)
	}

	bundlePath, err := writeBundle(b.String())
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, bundlePath)
	_, _ = fmt.Fprintln(out, buildIssueURL(bundlePath))
	return nil
}

func writeDoctorFindings(b *strings.Builder, ctx context.Context, store string) {
	_, _ = fmt.Fprintf(b, "## Doctor findings\n\n")
	report, err := cairn.Diagnose(ctx, store)
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(b, "diagnose failed: %s\n\n", err)
	case len(report.Findings) == 0:
		_, _ = fmt.Fprintf(b, "no findings\n\n")
	default:
		for _, f := range report.Findings {
			_, _ = fmt.Fprintf(b, "- [%s/%s] %s", f.Category, f.Severity, f.Message)
			if len(f.EntryIDs) > 0 {
				_, _ = fmt.Fprintf(b, " (%s)", strings.Join(f.EntryIDs, ", "))
			}
			_, _ = fmt.Fprintln(b)
		}
		_, _ = fmt.Fprintln(b)
	}
}

func writeStoreShape(b *strings.Builder, ctx context.Context, store string) {
	_, _ = fmt.Fprintf(b, "## Store shape\n\n")
	shape, err := cairn.StoreShape(ctx, store)
	if err != nil {
		_, _ = fmt.Fprintf(b, "store shape failed: %s\n\n", err)
		return
	}
	tiers := make([]string, 0, len(shape.TierCounts))
	for t := range shape.TierCounts {
		tiers = append(tiers, t)
	}
	sort.Strings(tiers)
	for _, t := range tiers {
		_, _ = fmt.Fprintf(b, "%s=%d\n", t, shape.TierCounts[t])
	}
	_, _ = fmt.Fprintf(b, "body_count=%d index_count=%d index_drift=%t\n\n", shape.BodyCount, shape.IndexCount, shape.IndexDrift)
}

// writeReviewBranches reports each open review branch's age/status by
// calling ListReviewBranches + branchStatus directly -- never evaluateBranch
// (branches.go), which mails a reviewer for a notify-status branch. rage
// must never trigger that side effect just by reading the same data
// stale-branches reports on (Guardrail #1).
func writeReviewBranches(b *strings.Builder, ctx context.Context, store string) {
	_, _ = fmt.Fprintf(b, "## Review branches\n\n")
	branches, err := cairn.ListReviewBranches(ctx, store, time.Now())
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(b, "list review branches failed: %s\n\n", err)
	case len(branches) == 0:
		_, _ = fmt.Fprintf(b, "none\n\n")
	default:
		for _, br := range branches {
			if br.Error != "" {
				_, _ = fmt.Fprintf(b, "- %s: error: %s\n", br.Name, br.Error)
				continue
			}
			status := branchStatus(br.Age, rageNotifyAfter, rageEscalateAfter)
			_, _ = fmt.Fprintf(b, "- %s entry=%s tier=%s age=%s status=%s\n",
				br.Name, br.EntryID, br.Tier, br.Age.Round(time.Minute), status)
		}
		_, _ = fmt.Fprintln(b)
	}
}

// writeEntryBodies embeds every entry's full body text in the store --not
// just entries referenced by a doctor finding-- since --include-bodies is a
// blanket opt-in to a bundle that may contain sensitive text, not a
// finding-scoped one.
func writeEntryBodies(b *strings.Builder, store string) {
	_, _ = fmt.Fprintf(b, "## Entry bodies\n\n")
	entries, _, err := cairn.IterEntriesTolerant(store)
	if err != nil {
		_, _ = fmt.Fprintf(b, "read bodies failed: %s\n\n", err)
		return
	}
	for _, e := range entries {
		_, _ = fmt.Fprintf(b, "### %s\n\n%s\n\n", e.ID, e.Body)
	}
}

// writeLastFailingCommand honors an explicit --failed-cmd/--exit-code pair
// verbatim over whatever auto-detection would find, and otherwise looks for
// the most recent nonzero-exit "exit" record in the log tail. With neither,
// it states plainly that nothing was found rather than guessing (FR-8).
func writeLastFailingCommand(b *strings.Builder, cmd *cobra.Command, tail []byte) {
	_, _ = fmt.Fprintf(b, "## Last failing command\n\n")
	if cmd.Flags().Changed("failed-cmd") {
		failedCmd, _ := cmd.Flags().GetString("failed-cmd")
		exitCode, _ := cmd.Flags().GetInt("exit-code")
		_, _ = fmt.Fprintf(b, "explicit: %s (exit code %d)\n\n", failedCmd, exitCode)
		return
	}
	if cp, code, ok := lastFailingCommand(tail); ok {
		_, _ = fmt.Fprintf(b, "auto-detected from log: %s (exit code %d)\n\n", cp, code)
		return
	}
	_, _ = fmt.Fprintf(b, "none found\n\n")
}

// lastFailingCommand scans tail's JSONL lines for the most recent "exit"
// record with a nonzero exit code, tolerating a truncated first line (tail
// is a raw byte window, not line-aligned) by skipping whatever doesn't parse.
func lastFailingCommand(tail []byte) (commandPath string, exitCode int, found bool) {
	for _, line := range strings.Split(string(tail), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["kind"] != "exit" {
			continue
		}
		ec, _ := rec["exit_code"].(float64)
		if ec == 0 {
			continue
		}
		cp, _ := rec["command_path"].(string)
		commandPath, exitCode, found = cp, int(ec), true
	}
	return commandPath, exitCode, found
}

// readLogTail returns the debug log's last n bytes, or nil if the log path
// can't be resolved or the file doesn't exist yet -- rage must still
// produce a bundle either way.
func readLogTail(n int) []byte {
	logPath, err := obslog.LogPath()
	if err != nil {
		return nil
	}
	tail, err := readTail(logPath, n)
	if err != nil {
		return nil
	}
	return tail
}

func readTail(path string, n int) ([]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := int64(0)
	if info.Size() > int64(n) {
		offset = info.Size() - int64(n)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

// writeBundle writes content to a fresh file under the same XDG state
// directory the debug log lives in, so both are discoverable from one
// well-known root, and returns its path.
func writeBundle(content string) (string, error) {
	logPath, err := obslog.LogPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(logPath), "rage")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("rage-%d.md", time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// buildIssueURL builds a prefilled "new issue" link referencing bundlePath
// -- never the bundle's own contents, which may be large or sensitive; a
// reporter attaches the file after reviewing it.
func buildIssueURL(bundlePath string) string {
	u := url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "/" + rageIssueRepo + "/issues/new",
	}
	q := url.Values{}
	q.Set("title", "cairn bug report")
	q.Set("body", fmt.Sprintf("Diagnostic bundle: %s\n\nDescribe what happened:\n", bundlePath))
	u.RawQuery = q.Encode()
	return u.String()
}
