package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.AddCommand(doctorExplainCmd)

	doctorCmd.Flags().Bool("json", false, "emit the full Report as JSON instead of a human-readable summary")
	doctorExplainCmd.Flags().Bool("json", false, "emit the explain result as JSON instead of a human-readable summary")
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Full-store health report: malformed entries, duplicate ids, scope/tag problems, dedup and freshness findings, index drift",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("doctor covers every entry in the store and does not filter by identity; " +
				"use 'cairn doctor explain --identity ... <topic-key>' to check one identity's resolution")
		}
		code, err := runDoctor(cmd, args)
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

// runDoctor is doctorCmd's body, factored out as a plain function that never
// calls os.Exit itself, so tests can drive all three of FR-9's exit-code
// outcomes directly: (0, nil) no findings, (1, nil) findings present, (2,
// err) Diagnose itself could not complete. rootCmd's own Execute() always
// calls os.Exit(1) on any RunE error, which cannot express that three-way
// split -- so doctorCmd.RunE is the only place that actually calls os.Exit,
// using the code runDoctor returns.
func runDoctor(cmd *cobra.Command, _ []string) (int, error) {
	report, err := cairn.Diagnose(cmd.Context(), storePath())
	if err != nil {
		return 2, err
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 2, err
		}
		return report.ExitCode, nil
	}

	renderReport(cmd, report)
	return report.ExitCode, nil
}

func renderReport(cmd *cobra.Command, report *cairn.Report) {
	out := cmd.OutOrStdout()
	if len(report.Findings) == 0 {
		_, _ = fmt.Fprintf(out, "cairn doctor: %s — no findings\n", report.Store)
		return
	}
	_, _ = fmt.Fprintf(out, "cairn doctor: %s — %d finding(s)\n", report.Store, len(report.Findings))
	for _, f := range report.Findings {
		flag := "!!"
		if f.Severity == cairn.SeverityWarning {
			flag = "??"
		}
		line := fmt.Sprintf("%s [%s] %s", flag, f.Category, f.Message)
		if len(f.EntryIDs) > 0 {
			line += fmt.Sprintf("  (%s)", strings.Join(f.EntryIDs, ", "))
		}
		_, _ = fmt.Fprintln(out, line)
	}
}

var doctorExplainCmd = &cobra.Command{
	Use:   "explain <entry-id>",
	Short: "Explain one entry's shadow status store-wide, or (with --identity) one topic-key's resolution for an identity",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")

		if identityRequested(cmd) {
			res, err := cairn.ExplainForIdentity(cmd.Context(), storePath(), resolveIdentity(cmd), args[0])
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}
			renderIdentityExplain(cmd, res)
			return nil
		}

		res, err := cairn.ExplainEntry(cmd.Context(), storePath(), args[0])
		if err != nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}
		renderEntryExplain(cmd, res)
		return nil
	},
}

func renderEntryExplain(cmd *cobra.Command, res *cairn.ExplainResult) {
	out := cmd.OutOrStdout()
	if res.TopicKey == "" {
		_, _ = fmt.Fprintf(out, "%s: no topic_key — never shadowed\n", res.EntryID)
		return
	}
	if res.ShadowedByID == "" {
		_, _ = fmt.Fprintf(out, "%s: topic_key %q — not shadowed\n", res.EntryID, res.TopicKey)
		return
	}
	_, _ = fmt.Fprintf(out, "%s: topic_key %q — shadowed by %s (%s)\n", res.EntryID, res.TopicKey, res.ShadowedByID, res.Rule)
}

func renderIdentityExplain(cmd *cobra.Command, res *cairn.IdentityExplainResult) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "topic_key %q — %d candidate(s)\n", res.TopicKey, len(res.Candidates))
	for _, c := range res.Candidates {
		verdict := "does not satisfy"
		if c.Satisfied {
			verdict = "satisfies"
		}
		marker := "  "
		if c.EntryID == res.WinnerID {
			marker = "->"
		}
		_, _ = fmt.Fprintf(out, "%s %s  scope=%v  %s this identity\n", marker, c.EntryID, c.Scope, verdict)
	}
	if res.WinnerID == "" {
		_, _ = fmt.Fprintln(out, "winner: none (no candidate satisfies this identity)")
		return
	}
	rule := res.Rule
	if rule == "" {
		rule = "sole match"
	}
	_, _ = fmt.Fprintf(out, "winner: %s (%s)\n", res.WinnerID, rule)
}
