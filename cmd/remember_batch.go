package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

// maxBatchLines caps a --batch-file manifest at 5000 lines, enforced by a
// streaming pre-count (countManifestLines) that aborts as soon as the cap is
// exceeded, before any line is processed or any file written.
const maxBatchLines = 5000

// maxManifestLineBytes raises bufio.Scanner's line buffer well past its
// default 64KB: a long entry body on one manifest line would otherwise fail
// with bufio.ErrTooLong.
const maxManifestLineBytes = 1 << 20

// batchOnlyFlags are the single-entry --topic/--scope/... flags whose
// per-entry meaning is expressed as a manifest-line JSON field in batch mode
// instead (see batchManifestEntry) -- --reviewer/--verify/--identity/--json
// remain whole-batch CLI flags and are deliberately not listed here.
var batchOnlyFlags = []string{"topic", "scope", "title", "summary", "type", "anchor-repo", "anchor-path", "force"}

// rejectSingleEntryFlags errors if any single-entry flag was explicitly
// passed alongside --batch-file, rather than silently ignoring it or
// misapplying it to every line.
func rejectSingleEntryFlags(cmd *cobra.Command) error {
	for _, name := range batchOnlyFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--batch-file cannot be combined with --%s (set it per-line in the manifest instead)", name)
		}
	}
	return nil
}

// batchManifestEntry is one line of a --batch-file manifest: JSON fields
// mirroring the single-entry flags that batch mode forbids as CLI flags
// (rejectSingleEntryFlags), since each line may need its own topic/scope/etc.
type batchManifestEntry struct {
	Body       string   `json:"body"`
	Type       string   `json:"type"`
	Topic      string   `json:"topic"`
	Scope      []string `json:"scope"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	AnchorRepo string   `json:"anchor_repo"`
	AnchorPath []string `json:"anchor_path"`
	Force      bool     `json:"force"`
}

// BatchEntryResult is one manifest line's outcome. It embeds RememberResult
// so a succeeded line's id/scope/commit/review_branch/reviewer read (and
// serialize to JSON) exactly like single-entry mode's own --json shape; Line
// and Error are batch-only additions with no single-entry equivalent. Error
// is empty for a succeeded line.
type BatchEntryResult struct {
	RememberResult
	Line  int    `json:"line"`
	Error string `json:"error,omitempty"`
}

// BatchResult is cairn remember --batch-file's --json top-level shape.
type BatchResult struct {
	BatchID         string             `json:"batch_id"`
	Total           int                `json:"total"`
	Succeeded       int                `json:"succeeded"`
	Failed          int                `json:"failed"`
	Unanchored      int                `json:"unanchored"`
	DerivedMetadata int                `json:"derived_metadata"`
	Entries         []BatchEntryResult `json:"entries"`
}

// countManifestLines streams path and reports how many lines it has,
// stopping as soon as the count exceeds maxBatchLines rather than reading to
// EOF -- the cap must reject an oversized manifest fast, without ever
// buffering the whole file.
func countManifestLines(path string) (n int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxManifestLineBytes)
	for scanner.Scan() {
		n++
		if n > maxBatchLines {
			return n, nil
		}
	}
	if serr := scanner.Err(); serr != nil {
		return 0, serr
	}
	return n, nil
}

// randomBatchID generates BatchResult.BatchID, mirroring internal/cairn's
// own randomSuffix (unexported there, so not reusable from this package).
func randomBatchID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "batch-" + hex.EncodeToString(b), nil
}

// runRememberBatch is cairn remember --batch-file's entry point, dispatched
// from rememberCmd's RunE before any of its existing single-entry logic
// runs. It validates the whole batch up front (forbidden flags, size cap),
// then processes each manifest line independently -- one bad line is
// recorded as that line's own BatchEntryResult.Error and never aborts its
// siblings (FR-3/FR-6) -- and finally groups successful shared-tier entries
// by resolved reviewer, sending one notification per group (FR-1/FR-4/NFR-2)
// instead of requestReview's one-per-entry mail.
func runRememberBatch(cmd *cobra.Command, batchFile string) error {
	if err := rejectSingleEntryFlags(cmd); err != nil {
		return emitError(cmd, classifiedErr(CategoryInvalidInput, "", err))
	}

	n, err := countManifestLines(batchFile)
	if err != nil {
		return emitError(cmd, fmt.Errorf("read --batch-file %s: %w", batchFile, err))
	}
	if n > maxBatchLines {
		return emitError(cmd, classifiedErr(CategoryInvalidInput, batchFile,
			fmt.Errorf("--batch-file %s has more than %d lines; split it into smaller batches", batchFile, maxBatchLines)))
	}

	identity, err := resolveIdentityValidated(cmd)
	if err != nil {
		return emitError(cmd, err)
	}
	createdBy := strings.Join(identity, " ")

	batchID, err := randomBatchID()
	if err != nil {
		return emitError(cmd, fmt.Errorf("generate batch id: %w", err))
	}

	result, candidates, err := scanBatchFile(cmd, batchFile, createdBy, batchID)
	if err != nil {
		return emitError(cmd, err)
	}

	var notifyErr error
	if len(candidates) > 0 {
		notifyErr = requestBatchReview(cmd.Context(), candidates)
	}

	if wantsJSON(cmd) {
		if jsonErr := emitJSON(cmd.OutOrStdout(), result); jsonErr != nil {
			return jsonErr
		}
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
	} else {
		fmt.Printf("%d succeeded, %d failed, %d used derived metadata\n",
			result.Succeeded, result.Failed, result.DerivedMetadata)
	}

	if notifyErr != nil {
		return notifyErr
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d of %d batch entries failed", result.Failed, result.Total)
	}
	return nil
}

// scanBatchFile opens batchFile and processes each line through
// processBatchLine, accumulating a BatchResult and the shared-tier
// candidates that still need requestBatchReview's grouped notification.
// Split out of runRememberBatch to keep the file handle's defer/Close
// self-contained and out of that function's cognitive-complexity count.
func scanBatchFile(cmd *cobra.Command, batchFile, createdBy, batchID string) (
	result BatchResult, candidates []batchReviewCandidate, err error,
) {
	f, err := os.Open(batchFile)
	if err != nil {
		return BatchResult{}, nil, fmt.Errorf("open --batch-file %s: %w", batchFile, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxManifestLineBytes)

	result = BatchResult{BatchID: batchID}
	line := 0
	for scanner.Scan() {
		line++
		res, candidate, unanchored := processBatchLine(cmd, scanner.Text(), line, createdBy)
		result.Entries = append(result.Entries, res)
		switch {
		case res.Error != "":
			result.Failed++
			if !wantsJSON(cmd) {
				fmt.Printf("line %d: %s\n", line, res.Error)
			}
		default:
			result.Succeeded++
			if !wantsJSON(cmd) {
				fmt.Printf("%s\n", res.ID)
			}
		}
		if unanchored {
			result.Unanchored++
		}
		if res.Error == "" && (res.Metadata.TitleSource == cairn.MetadataSourceDerived ||
			res.Metadata.SummarySource == cairn.MetadataSourceDerived) {
			result.DerivedMetadata++
		}
		if candidate != nil {
			candidates = append(candidates, *candidate)
		}
	}
	if serr := scanner.Err(); serr != nil {
		return BatchResult{}, nil, fmt.Errorf("scan --batch-file %s: %w", batchFile, serr)
	}
	result.Total = len(result.Entries)
	return result, candidates, nil
}

// parseBatchManifestLine unmarshals one manifest line and validates its
// static fields (topic/scope/title/summary), before any entry construction
// or side-effecting work begins. Split out of processBatchLine to keep that
// function's cognitive complexity down.
func parseBatchManifestLine(raw string) (batchManifestEntry, error) {
	var m batchManifestEntry
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return m, fmt.Errorf("parse manifest line: %w", err)
	}

	if m.Topic != "" {
		if err := cairn.ValidateTopicKey(m.Topic); err != nil {
			return m, fmt.Errorf("invalid topic %q: %w", m.Topic, err)
		}
	}
	for _, tag := range m.Scope {
		if err := cairn.ValidateScopeTag(tag); err != nil {
			return m, fmt.Errorf("invalid scope tag %q: %w", tag, err)
		}
	}
	if m.Title != "" {
		if err := cairn.ValidateTitleLength(m.Title); err != nil {
			return m, fmt.Errorf("invalid title: %w", err)
		}
	}
	if m.Summary != "" {
		if err := cairn.ValidateSummaryLength(m.Summary); err != nil {
			return m, fmt.Errorf("invalid summary: %w", err)
		}
	}
	return m, nil
}

// processBatchLine parses and processes one manifest line, mirroring
// rememberCmd.RunE's single-entry logic (validate -> construct -> verify ->
// recurrence check -> Create -> tier-appropriate commit) but reporting every
// failure into the returned BatchEntryResult.Error instead of returning it,
// since batch mode never aborts on one bad line (FR-3/FR-6). A non-nil
// *batchReviewCandidate is returned only for a successfully-committed
// shared-tier entry, still needing its group's mail from requestBatchReview.
func processBatchLine(cmd *cobra.Command, raw string, line int, createdBy string) (
	res BatchEntryResult, candidate *batchReviewCandidate, unanchored bool,
) {
	res.Line = line

	m, err := parseBatchManifestLine(raw)
	if err != nil {
		res.Error = err.Error()
		return res, nil, false
	}

	anchor := cairn.Anchor{}
	if m.AnchorRepo != "" || len(m.AnchorPath) > 0 {
		anchor = cairn.Anchor{Type: "files", Repo: m.AnchorRepo, Paths: m.AnchorPath}
	}

	e, err := cairn.NewEntry(cairn.NewEntryParams{
		Type:      m.Type,
		TopicKey:  m.Topic,
		Scope:     m.Scope,
		Body:      m.Body,
		CreatedBy: createdBy,
		Title:     m.Title,
		Summary:   m.Summary,
		Anchor:    anchor,
	})
	if err != nil {
		res.Error = fmt.Sprintf("construct entry: %v", err)
		return res, nil, false
	}

	if verify, _ := cmd.Flags().GetBool("verify"); verify && e.Anchor.Type == "files" {
		verifyAnchor(cmd, e)
	}
	unanchored = e.Anchor.Type != "files"

	matched, isRecurrence, err := recurrenceMatch(cmd, e)
	if err != nil {
		res.Error = fmt.Sprintf("check for a recurring entry: %v", err)
		return res, nil, unanchored
	}
	if matched != nil && isRecurrence && !m.Force {
		res.Error = recordBatchRecurrence(cmd.Context(), matched).Error()
		return res, nil, unanchored
	}
	if matched != nil {
		e.OverriddenDuplicateOf = matched.ID
	}

	candidate = commitBatchEntry(cmd, e, m.Scope, &res)
	return res, candidate, unanchored
}

// commitBatchEntry persists a validated, constructed entry: writes it via
// Create, then commits it directly (private scope) or onto a review branch
// (shared scope), filling in res's ID/Scope/Commit/ReviewBranch/Reviewer
// fields. Split out of processBatchLine to keep cyclomatic complexity down.
func commitBatchEntry(cmd *cobra.Command, e *cairn.Entry, scope []string, res *BatchEntryResult) *batchReviewCandidate {
	if err := e.Create(storePath()); err != nil {
		res.Error = fmt.Sprintf("write entry: %v", err)
		return nil
	}
	res.ID = e.ID
	res.Type = e.Type
	res.Scope = nonNil(e.Scope)
	res.Metadata = metadataQuality(e)

	if cairn.IsPrivateScope(e.Scope) {
		sha, err := e.CommitDirect(cmd.Context(), storePath())
		if err != nil {
			res.Error = fmt.Sprintf("commit entry: %v", err)
			return nil
		}
		res.Commit = sha
		return nil
	}

	branch, reviewer, err := commitForBatchReview(cmd, e, scope)
	if err != nil {
		res.Error = fmt.Sprintf("commit entry for review: %v", err)
		return nil
	}
	res.ReviewBranch = branch
	res.Reviewer = reviewer
	return &batchReviewCandidate{entry: e, branch: branch, reviewer: reviewer}
}

// recordBatchRecurrence mirrors recordRecurrence's persistence (bump
// RecurrenceCount, commit via matched's own tier-appropriate path) without
// its side effects (stderr print, emitError's immediate --json envelope):
// batch mode reports the discard as this line's own BatchEntryResult.Error
// rather than writing directly to stdout/stderr per line, so recordRecurrence
// itself is not reused here -- matching the design's precise reuse list
// (NewEntry/recurrenceMatch/Create/CommitDirect), which omits it
// deliberately. Always returns a non-nil error: either an operational
// failure persisting the bump, or -- on success -- the same "not stored:
// recurrence of ..." discard message recordRecurrence itself reports, since
// a recurrence hit is a discard either way from the candidate's own point of
// view.
func recordBatchRecurrence(ctx context.Context, matched *cairn.Entry) error {
	matched.RecurrenceCount++
	if err := matched.WriteBackRecurrenceCount(); err != nil {
		return fmt.Errorf("record recurrence for %s: %w", matched.ID, err)
	}

	if cairn.IsPrivateScope(matched.Scope) {
		if _, err := matched.CommitDirect(ctx, storePath()); err != nil {
			return fmt.Errorf("commit recurrence for %s: %w", matched.ID, err)
		}
	} else {
		if _, err := matched.CommitRecurrenceToReviewBranch(ctx, storePath()); err != nil {
			return fmt.Errorf("commit recurrence for %s to review branch: %w", matched.ID, err)
		}
	}

	return fmt.Errorf(
		"not stored: recurrence of %s (count %d); body is a near-identical match of existing content -- pass --force to store it anyway",
		matched.ID, matched.RecurrenceCount,
	)
}
