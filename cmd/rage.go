package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
)

// defaultRageMaxLogBytes bounds the debug log tail rage embeds in the
// bundle by default -- generous enough to carry real context, small enough
// that a long-lived debug.jsonl doesn't balloon the bundle file.
const defaultRageMaxLogBytes = 256 * 1024

// rageNotifyAfter and rageEscalateAfter mirror staleBranchesCmd's own
// default thresholds (cmd/branches.go): rage registers no flags of its own
// to override them -- it is a fixed, unconfigurable evidence snapshot, not
// a tunable sweep.
const (
	rageNotifyAfter   = 24 * time.Hour
	rageEscalateAfter = 72 * time.Hour
)

// rageIssueRepo is the repository rage's printed issue URL pre-fills a new
// issue against.
const rageIssueRepo = "https://github.com/quad341/cairn/issues/new"

func init() {
	rootCmd.AddCommand(rageCmd)

	rageCmd.Flags().Bool("include-bodies", false,
		"include full entry body text in the bundle (off by default: bodies may carry sensitive content)")
	rageCmd.Flags().String("command", "",
		"the command that prompted this rage run, recorded in the bundle as unverified context (never re-run)")
	rageCmd.Flags().Int("exit-code", 0,
		"the --command's exit code, recorded alongside it")
	rageCmd.Flags().Int("max-log-bytes", defaultRageMaxLogBytes,
		"byte budget for the debug log tail included in the bundle")
}

var rageCmd = &cobra.Command{
	Use:   "rage",
	Short: "Collect a self-contained diagnostic bundle (store health, debug log tail, review branch status) for filing a bug report",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if identityRequested(cmd) {
			return fmt.Errorf("rage covers every entry in the store and does not filter by identity; " +
				"use 'cairn doctor explain --identity ... <topic-key>' to check one identity's resolution")
		}
		code, err := runRage(cmd, args)
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

// RageBundle is the full self-contained diagnostic snapshot runRage writes
// to disk. The bundle file -- not stdout -- carries the bulk of the
// evidence (NFR-2): stdout only ever gets the bundle's path and a
// pre-filled issue URL, constant-size regardless of store or log size.
type RageBundle struct {
	GeneratedAt    time.Time            `json:"generated_at"`
	Version        VersionResult        `json:"version"`
	StorePath      string               `json:"store_path"`
	StoreSource    string               `json:"store_source"`
	IdentityTags   []string             `json:"identity_tags,omitempty"`
	IdentitySource string               `json:"identity_source"`
	Doctor         *cairn.Report        `json:"doctor"`
	LogTail        []json.RawMessage    `json:"log_tail,omitempty"`
	ReviewBranches []StaleBranchFinding `json:"review_branches,omitempty"`
	EntryBodies    []RageEntryBody      `json:"entry_bodies,omitempty"`
	FailingCommand *RageFailingCommand  `json:"failing_command,omitempty"`
}

// RageEntryBody carries one entry's full body text. Only ever populated
// when --include-bodies is passed -- a body may hold content an agent
// wouldn't want swept into a bug report by default.
type RageEntryBody struct {
	EntryID string `json:"entry_id"`
	Body    string `json:"body"`
}

// RageFailingCommand records the --command/--exit-code the caller supplied
// as the command that prompted this rage run. rage never re-runs it --
// Unverified is always true, flagging this as the caller's unverified
// report rather than something rage itself observed.
type RageFailingCommand struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Unverified bool   `json:"unverified"`
}

// RageResult is cairn rage --json's output shape: the same two pieces of
// information as the default two-line stdout (bundle path, then issue URL),
// nothing else -- so NFR-2's "constant-size regardless of store/log size"
// invariant holds in both modes, matching the convention printVersion
// establishes in cmd/version.go.
type RageResult struct {
	BundlePath string `json:"bundle_path"`
	IssueURL   string `json:"issue_url"`
}

// runRage is rageCmd's body, factored out as a plain function that never
// calls os.Exit itself (mirroring runDoctor), so tests can drive it
// directly. Unlike doctor's three-way exit code split, rage's job is
// evidence collection, not health assessment: (0, nil) whenever a bundle
// was successfully written, regardless of what it contains; (1, err) only
// when the bundle itself could not be produced.
func runRage(cmd *cobra.Command, _ []string) (int, error) {
	storeP, storeSource := storePathWithSource()
	identityTags, identitySource := identityWithSource(cmd)

	report, err := cairn.Diagnose(cmd.Context(), storeP)
	if err != nil {
		return 1, fmt.Errorf("diagnose store: %w", err)
	}

	maxLogBytes, _ := cmd.Flags().GetInt("max-log-bytes")
	var logTail []json.RawMessage
	if logPath, lerr := obslog.LogPath(); lerr == nil {
		if tail, terr := obslog.Tail(logPath, maxLogBytes); terr == nil {
			logTail = tail
		}
	}

	// A store that isn't a git repository (or has no commits yet) is a
	// normal case for rage's callers -- e.g. a freshly initialized rig.
	// Review branch status degrades gracefully here the same way Diagnose's
	// entry walk tolerates one malformed file: the rest of the bundle still
	// gets built.
	var findings []StaleBranchFinding
	if reviewBranches, rerr := cairn.ListReviewBranches(cmd.Context(), storeP, time.Now()); rerr == nil {
		statePath := stateFilePath(cmd)
		state, serr := loadNotifyState(statePath)
		if serr != nil {
			return 1, fmt.Errorf("load review branch state: %w", serr)
		}
		findings = make([]StaleBranchFinding, 0, len(reviewBranches))
		for _, b := range reviewBranches {
			// dryRun is hardcoded true: rage is read-only evidence
			// collection and must never mail a reviewer as a side effect of
			// a diagnostic run.
			findings = append(findings, evaluateBranch(cmd, b, rageNotifyAfter, rageEscalateAfter, true, state))
		}
	}

	var entryBodies []RageEntryBody
	if includeBodies, _ := cmd.Flags().GetBool("include-bodies"); includeBodies {
		entries, _, ierr := cairn.IterEntriesTolerant(storeP)
		if ierr != nil {
			return 1, fmt.Errorf("read entry bodies: %w", ierr)
		}
		entryBodies = make([]RageEntryBody, 0, len(entries))
		for _, e := range entries {
			entryBodies = append(entryBodies, RageEntryBody{EntryID: e.ID, Body: e.Body})
		}
	}

	var failingCommand *RageFailingCommand
	if failCmd, _ := cmd.Flags().GetString("command"); failCmd != "" {
		exitCode, _ := cmd.Flags().GetInt("exit-code")
		failingCommand = &RageFailingCommand{Command: failCmd, ExitCode: exitCode, Unverified: true}
	}

	bundle := RageBundle{
		GeneratedAt:    time.Now(),
		Version:        VersionResult{Version: version, Commit: commit, Date: date},
		StorePath:      storeP,
		StoreSource:    storeSource,
		IdentityTags:   identityTags,
		IdentitySource: identitySource,
		Doctor:         report,
		LogTail:        logTail,
		ReviewBranches: findings,
		EntryBodies:    entryBodies,
		FailingCommand: failingCommand,
	}

	bundlePath, err := writeRageBundle(bundle)
	if err != nil {
		return 1, fmt.Errorf("write rage bundle: %w", err)
	}

	issueURL := rageIssueURL(storeP, bundlePath)
	if wantsJSON(cmd) {
		if err := emitJSON(cmd.OutOrStdout(), RageResult{BundlePath: bundlePath, IssueURL: issueURL}); err != nil {
			return 1, fmt.Errorf("emit json: %w", err)
		}
		return 0, nil
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, bundlePath)
	_, _ = fmt.Fprintln(out, issueURL)
	return 0, nil
}

// writeRageBundle marshals bundle and writes it under the same directory as
// obslog's own debug log (obslog.LogPath), so both diagnostic artifacts
// live in one well-known place -- matching that log file's own
// directory/file permissions (internal/obslog/writer.go's
// newRotatingWriter). The filename carries invocation-unique entropy in the
// same category as obslog's own invocationID (pid + nanosecond timestamp),
// so two concurrent agents' rage runs can never collide on the same second.
func writeRageBundle(bundle RageBundle) (string, error) {
	logPath, err := obslog.LogPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}

	name := fmt.Sprintf("rage-%d-%d.json", os.Getpid(), time.Now().UnixNano())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// rageIssueURL builds a prefilled "new issue" link via net/url.Values,
// never manual string concatenation, so a special character in storePath
// (e.g. '&') can never inject a spurious extra query parameter. The body
// only ever points at the bundle file's path, never its contents -- the
// bundle itself, not the URL, is meant to carry the evidence.
func rageIssueURL(storePath, bundlePath string) string {
	v := url.Values{}
	v.Set("title", "cairn rage report")
	v.Set("body", fmt.Sprintf(
		"Store: %s\nBundle: %s\n\n<describe what went wrong>\n",
		storePath, bundlePath))
	return rageIssueRepo + "?" + v.Encode()
}
