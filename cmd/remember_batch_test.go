package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batchManifestLine marshals fields as a single-line JSON object suitable for
// one line of a --batch-file manifest.
func batchManifestLine(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	require.NoError(t, err)
	return string(b)
}

// writeBatchManifest writes lines (each already one JSON object, typically
// from batchManifestLine, but a deliberately-malformed raw string works too
// for negative fixtures) as a newline-delimited JSONL file under a fresh temp
// dir, returning its path.
func writeBatchManifest(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.jsonl")
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// requireEntries requires exactly n files under dir and reads them all back
// through cairn.ParseEntry -- requireSingleEntry's multi-file sibling, since
// batch tests routinely write more than one entry into the same scope
// directory.
func requireEntries(t *testing.T, dir string, n int) []*cairn.Entry {
	t.Helper()
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, n, "expected exactly %d files written under %s", n, dir)
	out := make([]*cairn.Entry, len(files))
	for i, f := range files {
		e, err := cairn.ParseEntry(filepath.Join(dir, f.Name()))
		require.NoError(t, err)
		out[i] = e
	}
	return out
}

// resetBatchFlag resets rememberCmd's --batch-file flag. Kept separate from
// cmd/remember_test.go's resetRememberFlags (never folded into it) because
// that file must receive zero diffs (FR-2/NFR-1). rememberCmd is a
// package-level singleton reused across every test in this binary, so a
// batch test that skipped this would leak --batch-file's Changed bit and
// value into a later, unrelated remember_test.go test in the same run,
// silently pushing it onto the batch code path.
func resetBatchFlag(t *testing.T) {
	t.Helper()
	f := rememberCmd.Flags().Lookup("batch-file")
	require.NotNil(t, f, "cairn remember must register a --batch-file flag")
	require.NoError(t, f.Value.Set(""))
	f.Changed = false
}

// runBatchRemember executes "cairn remember --batch-file <manifest>" (plus
// extraArgs) against a fresh git-initialized store, stubbing gc to always
// succeed. See runRememberWithGC (remember_test.go) for why the store must
// be git-initialized and why flags are reset both before and after.
func runBatchRemember(t *testing.T, manifest string, extraArgs ...string) (string, error) {
	t.Helper()
	return runBatchRememberWithGC(t, stubGC, manifest, extraArgs...)
}

// runBatchRememberWithGC is runBatchRemember parameterized on the gc stub, so
// a test can observe the stub's own invocation (call count, captured argv)
// instead of always wiring up the bare always-succeeding one.
func runBatchRememberWithGC(t *testing.T, stub func(*testing.T), manifest string, extraArgs ...string) (string, error) {
	t.Helper()
	resetRememberFlags(t)
	resetBatchFlag(t)
	t.Cleanup(func() {
		resetRememberFlags(t)
		resetBatchFlag(t)
	})

	store := t.TempDir()
	gitInit(t, store)
	stub(t)
	args := append([]string{"remember", "--store", store, "--batch-file", manifest}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return store, rootCmd.Execute()
}

// runBatchRememberCapturingStdout is runBatchRemember with stdout captured
// via captureStdout, for tests asserting on the plain-text summary batch
// mode prints -- mirrors how existing single-entry tests wrap
// runRemember/runRememberWithGC directly in captureStdout, factored out here
// since batch tests need it more than once.
func runBatchRememberCapturingStdout(t *testing.T, manifest string, extraArgs ...string) (store, stdout string, err error) {
	t.Helper()
	stdout = captureStdout(t, func() {
		store, err = runBatchRemember(t, manifest, extraArgs...)
	})
	return store, stdout, err
}

// runBatchRememberCapturingStderr is runBatchRemember with stderr captured
// via rootCmd.SetErr directly -- no os.Pipe() trick needed here, unlike
// captureStdout: nudgeIfUnanchored (remember.go) writes via
// cmd.ErrOrStderr(), which respects SetErr, whereas plain-text remember
// output bypasses cmd.OutOrStdout() entirely via raw fmt.Println (see
// captureStdout's own doc comment in commands_test.go for why *that* needs
// the pipe trick). Deliberately plain-text, not --json: nudgeIfUnanchored
// already suppresses itself whenever wantsJSON(cmd) is true, independent of
// batch mode, so a --json run can't distinguish batch mode's own suppression
// from that pre-existing carve-out.
func runBatchRememberCapturingStderr(t *testing.T, manifest string, extraArgs ...string) (store, stderr string, err error) {
	t.Helper()
	resetRememberFlags(t)
	resetBatchFlag(t)
	t.Cleanup(func() {
		resetRememberFlags(t)
		resetBatchFlag(t)
	})

	store = t.TempDir()
	gitInit(t, store)
	stubGC(t)
	args := append([]string{"remember", "--store", store, "--batch-file", manifest}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	err = rootCmd.Execute()
	return store, errBuf.String(), err
}

// runBatchRememberJSON is runBatchRemember's --json counterpart, mirroring
// execRememberJSONAgainstStore's mechanics (resetRememberFlags alone doesn't
// touch the shared --json persistent flag, registered on rootCmd by
// format.go's init rather than this file's). Also returns the store path
// (unlike execRememberJSONAgainstStore/runRememberJSON): a batch JSON test
// routinely needs to cross-check the structured result against the actual
// files a multi-entry batch wrote, which a single-entry JSON test never
// needs to do.
func runBatchRememberJSON(t *testing.T, manifest string, extraArgs ...string) (store, stdout string, err error) {
	t.Helper()
	resetRememberFlags(t)
	resetBatchFlag(t)
	require.NoError(t, resetJSONFlag())
	t.Cleanup(func() {
		resetRememberFlags(t)
		resetBatchFlag(t)
		_ = resetJSONFlag()
	})

	store = t.TempDir()
	gitInit(t, store)
	stubGC(t)
	args := append([]string{"remember", "--store", store, "--batch-file", manifest, "--json"}, extraArgs...)
	rootCmd.SetArgs(args)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&bytes.Buffer{})
	err = rootCmd.Execute()
	return store, buf.String(), err
}

// stubGCCountingCalls is stubGC's call-counting sibling: the stubbed gc
// binary still exits 0, but appends one line to countFile per invocation, so
// a test can assert how many times gc was invoked across a whole batch run.
// stubGCCapturing can't do this -- its capture file is overwritten, not
// appended, on each call (see its own doc comment) -- and a large batch that
// must collapse many entries into one notification needs exactly this count.
func stubGCCountingCalls(t *testing.T, countFile string) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\n"+
		"echo call >> "+shellQuote(countFile)+"\n"+
		"exit 0\n")
}

// readGCCallCount reports how many times a stubGCCountingCalls stub was
// invoked, by counting countFile's lines. Zero (not a test failure) when the
// stub was never called: unlike readStubGCArgs, "gc was never invoked" is
// itself a meaningful, assertable outcome for an all-private-tier batch.
func readGCCallCount(t *testing.T, countFile string) int {
	t.Helper()
	raw, err := os.ReadFile(countFile)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// stubGCCapturingAllCalls is stubGCCapturing's multi-call sibling: rather
// than overwriting captureFile on each invocation (correct only when exactly
// one gc call is expected), each invocation appends its own base64-encoded
// argv to captureFile as one line per argument, with a blank line separating
// invocations. A batch that groups mail by resolved reviewer (NFR-2) can
// call gc more than once, and a test asserting who each group's mail went to
// needs every call's argv, not just the last one.
func stubGCCapturingAllCalls(t *testing.T, captureFile string) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\n"+
		"for a in \"$@\"; do printf '%s' \"$a\" | base64 -w0; printf '\\n'; done >> "+shellQuote(captureFile)+"\n"+
		"printf '\\n' >> "+shellQuote(captureFile)+"\n"+
		"exit 0\n")
}

// readStubGCAllCalls reads back a stubGCCapturingAllCalls capture file as one
// []string argv per invocation, in call order.
func readStubGCAllCalls(t *testing.T, captureFile string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(captureFile)
	require.NoError(t, err, "the gc stub must have run and recorded its invocations")
	blocks := strings.Split(strings.TrimRight(string(raw), "\n"), "\n\n")
	calls := make([][]string, len(blocks))
	for i, block := range blocks {
		lines := strings.Split(block, "\n")
		args := make([]string, len(lines))
		for j, l := range lines {
			decoded, err := base64.StdEncoding.DecodeString(l)
			require.NoError(t, err, "call %d line %d (%q) must be valid base64", i, j, l)
			args[j] = string(decoded)
		}
		calls[i] = args
	}
	return calls
}

func TestRememberBatchFlagRegistered(t *testing.T) {
	f := rememberCmd.Flags().Lookup("batch-file")
	require.NotNil(t, f, "cairn remember must register a --batch-file flag")
}

// TestRememberBatchRejectsCombinationWithSingleEntryFlags covers the
// acceptance criteria's forbidden-flag list: single-entry flags whose
// per-entry meaning is expressed as manifest-line fields in batch mode
// instead, so combining them with --batch-file is rejected outright rather
// than silently ignored or misapplied to every line.
func TestRememberBatchRejectsCombinationWithSingleEntryFlags(t *testing.T) {
	forbidden := map[string][]string{
		"--topic":       {"--topic", "some-topic"},
		"--scope":       {"--scope", "agent:test"},
		"--title":       {"--title", "a title"},
		"--summary":     {"--summary", "a summary"},
		"--anchor-repo": {"--anchor-repo", "/tmp/some-repo"},
		"--anchor-path": {"--anchor-path", "a.go"},
		"--force":       {"--force"},
	}
	for name, flagArgs := range forbidden {
		t.Run(name, func(t *testing.T) {
			manifest := writeBatchManifest(t, batchManifestLine(t, map[string]any{
				"body": "a body", "scope": []string{"agent:test"},
			}))
			store, err := runBatchRemember(t, manifest, flagArgs...)
			require.Error(t, err, "--batch-file combined with %s must be rejected", name)
			assert.Contains(t, err.Error(), "--batch-file")
			assert.Contains(t, err.Error(), name)
			assertNoFilesWritten(t, store)
		})
	}
}

// TestRememberBatchLargeSingleTierBatchSendsOneNotification is FR-1+NFR-3: a
// large single-tier batch must collapse into exactly one review notification,
// not one gc mail send per entry.
func TestRememberBatchLargeSingleTierBatchSendsOneNotification(t *testing.T) {
	lines := make([]string, 105)
	for i := range lines {
		lines[i] = batchManifestLine(t, map[string]any{
			"body":  fmt.Sprintf("entry number %d", i),
			"scope": []string{"rig:web"},
		})
	}
	manifest := writeBatchManifest(t, lines...)

	countFile := filepath.Join(t.TempDir(), "gc-calls")
	store, err := runBatchRememberWithGC(t, func(t *testing.T) {
		t.Helper()
		stubGCCountingCalls(t, countFile)
	}, manifest)
	require.NoError(t, err)

	requireEntries(t, filepath.Join(store, "rig", "web"), 105)
	assert.Equal(t, 1, readGCCallCount(t, countFile),
		"a large single-tier batch must send exactly one notification, not one per entry")
}

// TestRememberBatchEntryFailureDoesNotBlockSiblingCommits is FR-3: one bad
// line must not stop its siblings from committing, and the plain-text
// summary must still report both outcomes.
func TestRememberBatchEntryFailureDoesNotBlockSiblingCommits(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "good entry one", "scope": []string{"agent:test"}, "topic": "sibling-good-1"}),
		batchManifestLine(t, map[string]any{"body": "bad entry", "scope": []string{"not-a-valid-scope-tag"}, "topic": "sibling-bad"}),
		batchManifestLine(t, map[string]any{"body": "good entry two", "scope": []string{"agent:test"}, "topic": "sibling-good-2"}),
	)
	store, stdout, err := runBatchRememberCapturingStdout(t, manifest)
	require.Error(t, err, "a batch containing any failed line must exit non-zero")

	requireEntries(t, filepath.Join(store, "agent", "test"), 2)
	assert.Contains(t, stdout, "2 succeeded", "the summary must report how many entries succeeded")
	assert.Contains(t, stdout, "1 failed", "the summary must report how many entries failed")
}

// TestRememberBatchNotificationBodyListsEveryEntryIDAndBranch is FR-4.
func TestRememberBatchNotificationBodyListsEveryEntryIDAndBranch(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "first shared entry", "scope": []string{"rig:web"}}),
		batchManifestLine(t, map[string]any{"body": "second shared entry", "scope": []string{"rig:web"}}),
	)
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	store, err := runBatchRememberWithGC(t, func(t *testing.T) {
		t.Helper()
		stubGCCapturing(t, captureFile)
	}, manifest)
	require.NoError(t, err)

	entries := requireEntries(t, filepath.Join(store, "rig", "web"), 2)
	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	body := args[6]
	for _, e := range entries {
		assert.Contains(t, body, e.ID, "the notification body must list every batch entry's id")
		assert.Contains(t, body, "remember/"+e.ID, "the notification body must list every batch entry's review branch")
	}
}

// TestRememberBatchEntriesIndependentlyMergeable is FR-5: each batch entry
// gets its own remember/<id> review branch (CommitToReviewBranch, reused
// unchanged), so internal/cairn/review.go's existing per-branch merge logic
// already makes every one of them independently mergeable with zero code
// changes -- this test is what actually proves it for batch specifically.
func TestRememberBatchEntriesIndependentlyMergeable(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "mergeable entry one", "scope": []string{"rig:web"}, "topic": "merge-test-one"}),
		batchManifestLine(t, map[string]any{"body": "mergeable entry two", "scope": []string{"rig:web"}, "topic": "merge-test-two"}),
		batchManifestLine(t, map[string]any{"body": "mergeable entry three", "scope": []string{"rig:web"}, "topic": "merge-test-three"}),
	)
	store, err := runBatchRemember(t, manifest)
	require.NoError(t, err)

	entries := requireEntries(t, filepath.Join(store, "rig", "web"), 3)
	for _, e := range entries {
		branch := "remember/" + e.ID
		require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", e.TopicKey),
			"each batch entry's review branch must merge independently of its siblings")
	}
}

// TestRememberBatchMixedValidInvalidLinesRecordsPerEntryFailures is FR-6.
//
// Unlike single-entry mode's all-or-nothing ErrorResult-on-failure
// convention, a batch with partial failures still emits a BatchResult (not
// an ErrorResult) on --json: the whole point of per-entry Line/Error fields
// is to report which lines succeeded and which failed in one structured
// response, which a flat ErrorResult can't express. The process still exits
// non-zero when any line failed -- only the JSON shape differs from
// single-entry mode's convention.
func TestRememberBatchMixedValidInvalidLinesRecordsPerEntryFailures(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "good entry", "scope": []string{"agent:test"}, "topic": "mix-good-1"}),
		`{not valid json`,
		batchManifestLine(t, map[string]any{"body": "bad scope entry", "scope": []string{"not-a-valid-scope-tag"}, "topic": "mix-bad-1"}),
		batchManifestLine(t, map[string]any{"body": "another good entry", "scope": []string{"agent:test"}, "topic": "mix-good-2"}),
	)
	store, out, err := runBatchRememberJSON(t, manifest)
	require.Error(t, err, "a batch with any failed line must exit non-zero")

	var result BatchResult
	require.NoError(t, json.Unmarshal([]byte(out), &result), "a partially-failed batch must still emit a decodable BatchResult, not an ErrorResult")
	require.Len(t, result.Entries, 4)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 2, result.Failed)

	assert.Empty(t, result.Entries[0].Error)
	assert.NotEmpty(t, result.Entries[0].ID)
	assert.NotEmpty(t, result.Entries[1].Error, "malformed JSON on line 2 must be recorded as that line's own failure")
	assert.NotEmpty(t, result.Entries[2].Error, "an invalid scope tag on line 3 must be recorded as that line's own failure")
	assert.Empty(t, result.Entries[3].Error)
	assert.NotEmpty(t, result.Entries[3].ID)

	for i, e := range result.Entries {
		assert.Equal(t, i+1, e.Line, "Line must be the manifest's 1-indexed line number")
	}

	requireEntries(t, filepath.Join(store, "agent", "test"), 2)
}

// TestRememberBatchReviewerFlagOverridesGroupingAcrossTiers is NFR-2's
// override half: --reviewer collapses every tier's computed default into one
// explicit recipient, so a batch spanning rig/role/global tiers still sends
// exactly one notification.
func TestRememberBatchReviewerFlagOverridesGroupingAcrossTiers(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "rig entry", "scope": []string{"rig:web"}, "topic": "override-rig"}),
		batchManifestLine(t, map[string]any{"body": "role entry", "scope": []string{"role:builder"}, "topic": "override-role"}),
		batchManifestLine(t, map[string]any{"body": "global entry", "scope": []string{}, "topic": "override-global"}),
	)
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	_, err := runBatchRememberWithGC(t, func(t *testing.T) {
		t.Helper()
		stubGCCapturing(t, captureFile)
	}, manifest, "--reviewer", "explicit-reviewer")
	require.NoError(t, err)

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, "explicit-reviewer", args[2], "--reviewer must override every tier's computed default, collapsing all groups into one")
}

// TestRememberBatchGroupsMailByDistinctResolvedReviewer is NFR-2's grouping
// half: two rig:web entries (same resolved reviewer) plus one role:builder
// entry (a different resolved reviewer) must produce exactly two mail calls,
// one per distinct reviewer -- not one per entry, and not one flat total.
func TestRememberBatchGroupsMailByDistinctResolvedReviewer(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "first rig entry", "scope": []string{"rig:web"}, "topic": "group-rig-1"}),
		batchManifestLine(t, map[string]any{"body": "second rig entry", "scope": []string{"rig:web"}, "topic": "group-rig-2"}),
		batchManifestLine(t, map[string]any{"body": "role entry", "scope": []string{"role:builder"}, "topic": "group-role-1"}),
	)
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	_, err := runBatchRememberWithGC(t, func(t *testing.T) {
		t.Helper()
		stubGCCapturingAllCalls(t, captureFile)
	}, manifest)
	require.NoError(t, err)

	calls := readStubGCAllCalls(t, captureFile)
	require.Len(t, calls, 2, "two rig:web entries plus one role:builder entry must resolve to exactly two distinct-reviewer mail groups")

	recipients := make([]string, len(calls))
	for i, c := range calls {
		require.Len(t, c, 7, "gc mail send <reviewer> -s <subject> -m <body>")
		recipients[i] = c[2]
	}
	assert.ElementsMatch(t, []string{"test-rig/architect", "test-rig/builder"}, recipients)
}

// TestRememberBatchRejectsManifestOverSizeCap pins the 5000-line cap's
// fail-fast design: a pre-count pass rejects the whole manifest before any
// entry is written, rather than processing 5000 lines and only then failing
// on the 5001st (which would leave partial writes behind).
func TestRememberBatchRejectsManifestOverSizeCap(t *testing.T) {
	lines := make([]string, 5001)
	for i := range lines {
		lines[i] = batchManifestLine(t, map[string]any{
			"body": fmt.Sprintf("cap test entry %d", i), "scope": []string{"agent:test"},
		})
	}
	manifest := writeBatchManifest(t, lines...)

	store, err := runBatchRemember(t, manifest)
	require.Error(t, err, "a manifest over the 5000-line cap must be rejected outright")
	assert.Contains(t, err.Error(), "5000")
	assertNoFilesWritten(t, store)
}

// TestRememberBatchJSONOutputMatchesBatchResultShape decodes stdout directly
// into the production BatchResult/BatchEntryResult types, and folds in a
// bonus check that a mixed batch handles both tiers correctly in the same
// run: private entries commit silently (Commit set, no ReviewBranch/Reviewer)
// while shared entries get a review branch and reviewer (no Commit) exactly
// like single-entry mode's RememberResult already does per-entry.
func TestRememberBatchJSONOutputMatchesBatchResultShape(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "private entry", "scope": []string{"agent:test"}, "topic": "shape-private"}),
		batchManifestLine(t, map[string]any{"body": "shared entry", "scope": []string{"rig:web"}, "topic": "shape-shared"}),
	)
	store, out, err := runBatchRememberJSON(t, manifest)
	require.NoError(t, err)

	var result BatchResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 2)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)

	priv := result.Entries[0]
	assert.NotEmpty(t, priv.ID)
	assert.Equal(t, []string{"agent:test"}, priv.Scope)
	assert.NotEmpty(t, priv.Commit, "a private-tier batch entry must report a commit sha")
	assert.Empty(t, priv.ReviewBranch)
	assert.Empty(t, priv.Reviewer)

	shared := result.Entries[1]
	assert.NotEmpty(t, shared.ID)
	assert.Equal(t, []string{"rig:web"}, shared.Scope)
	assert.Empty(t, shared.Commit)
	assert.NotEmpty(t, shared.ReviewBranch, "a shared-tier batch entry must report its review branch")
	assert.NotEmpty(t, shared.Reviewer)

	requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	requireSingleEntry(t, filepath.Join(store, "rig", "web"))
}

func TestRememberBatchExitsNonZeroWhenAnyEntryFails(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "good entry", "scope": []string{"agent:test"}, "topic": "exit-good"}),
		batchManifestLine(t, map[string]any{"body": "bad entry", "scope": []string{"not-a-valid-scope-tag"}, "topic": "exit-bad"}),
	)
	_, err := runBatchRemember(t, manifest)
	assert.Error(t, err, "any per-line failure must make the overall batch command exit non-zero")
}

func TestRememberBatchExitsZeroWhenAllEntriesSucceed(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "good entry one", "scope": []string{"agent:test"}, "topic": "exit-ok-1"}),
		batchManifestLine(t, map[string]any{"body": "good entry two", "scope": []string{"agent:test"}, "topic": "exit-ok-2"}),
	)
	_, err := runBatchRemember(t, manifest)
	assert.NoError(t, err, "a fully-successful batch must exit zero")
}

// TestRememberBatchRecurrenceHitRecordedAsPerEntryFailure confirms batch mode
// reuses recurrenceMatch/recordRecurrence (remember.go) per line exactly as
// the bead's design summary specifies -- "per-line loop reusing existing
// cairn.NewEntry/recurrenceMatch/Create/CommitDirect unchanged" -- rather
// than silently double-creating a duplicate or skipping the check entirely.
// Both lines deliberately share one non-empty topic key: recurrenceMatch's
// own doc comment guarantees candidate.TopicKey == "" never matches anything
// (pairSignals' sameTopicKey requires a non-empty key on both sides), so an
// empty topic -- what every other multi-line fixture in this file uses --
// would never exercise this path at all. Identical body text on both lines
// guarantees the content signal's Jaccard similarity is maximal (1.0),
// tripping the pairing regardless of the exact threshold recurrenceMatch's
// content signal uses. Single-entry mode's own recordRecurrence discards the
// new candidate rather than storing it -- batch mode must record that
// discard as the second line's own per-entry failure, not crash and not
// write a second file.
func TestRememberBatchRecurrenceHitRecordedAsPerEntryFailure(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "a recurring fact about the build system", "scope": []string{"agent:test"}, "topic": "recur-test"}),
		batchManifestLine(t, map[string]any{"body": "a recurring fact about the build system", "scope": []string{"agent:test"}, "topic": "recur-test"}),
	)
	// recurrenceMatch only considers entries cairn.Visible to the caller's own
	// identity, so the caller must identify as the manifest's own agent:test
	// scope for line 2 to ever see line 1 as a recurrence candidate.
	store, out, err := runBatchRememberJSON(t, manifest, "--identity", "agent:test")
	require.Error(t, err, "a batch containing a recurrence hit must exit non-zero")

	var result BatchResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 2)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, result.Failed)

	assert.Empty(t, result.Entries[0].Error)
	assert.NotEmpty(t, result.Entries[0].ID)
	assert.NotEmpty(t, result.Entries[1].Error, "a same-topic, same-body second line is a genuine recurrence and must be recorded as that line's own failure")
	assert.Contains(t, result.Entries[1].Error, "recurrence")

	requireEntries(t, filepath.Join(store, "agent", "test"), 1)
}

// TestRememberBatchForceFieldBypassesRecurrence confirms the manifest's
// per-line "force" field -- the batch-mode equivalent of single-entry mode's
// --force flag, per the design summary's list of flags that "become
// manifest-line JSON fields, not CLI flags, when --batch-file is set" --
// bypasses a detected recurrence exactly like single-entry mode's --force
// does: the second line (same topic and body as
// TestRememberBatchRecurrenceHitRecordedAsPerEntryFailure's pair) still gets
// created and committed, recording which entry it overrides, rather than
// being discarded.
func TestRememberBatchForceFieldBypassesRecurrence(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "a recurring fact about the build system", "scope": []string{"agent:test"}, "topic": "force-recur-test"}),
		batchManifestLine(t, map[string]any{"body": "a recurring fact about the build system", "scope": []string{"agent:test"}, "topic": "force-recur-test", "force": true}),
	)
	// See TestRememberBatchRecurrenceHitRecordedAsPerEntryFailure: identity
	// must match the manifest's own scope for recurrenceMatch to see line 1 as
	// a candidate when checking line 2, force field or not.
	store, out, err := runBatchRememberJSON(t, manifest, "--identity", "agent:test")
	require.NoError(t, err, "--force must let a genuine recurrence through, so the whole batch succeeds")

	var result BatchResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 2)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	assert.Empty(t, result.Entries[0].Error)
	assert.Empty(t, result.Entries[1].Error)

	entries := requireEntries(t, filepath.Join(store, "agent", "test"), 2)
	var forced *cairn.Entry
	for _, e := range entries {
		if e.ID == result.Entries[1].ID {
			forced = e
		}
	}
	require.NotNil(t, forced, "the forced second entry must be findable by the id BatchResult reported for it")
	assert.Equal(t, result.Entries[0].ID, forced.OverriddenDuplicateOf, "a forced-past recurrence must record which entry it overrides")
}

// TestRememberBatchSuppressesUnanchoredStderrNudge covers the design
// summary's nudgeIfUnanchored carve-out: "nudgeIfUnanchored's per-entry
// stderr nudge is suppressed in batch mode." Deliberately plain-text (see
// runBatchRememberCapturingStderr's doc comment): only batch mode's own
// suppression -- not the pre-existing wantsJSON carve-out -- can be
// responsible for a clean stderr here, since single-entry mode's nudge does
// fire in plain-text mode today. A 105-line batch printing single-entry
// mode's three-line nudge per unanchored line would flood stderr with no
// value (crn-5wus's whole point was avoiding exactly that noise).
func TestRememberBatchSuppressesUnanchoredStderrNudge(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "unanchored entry one", "scope": []string{"agent:test"}, "topic": "unanchored-1"}),
		batchManifestLine(t, map[string]any{"body": "unanchored entry two", "scope": []string{"agent:test"}, "topic": "unanchored-2"}),
	)
	_, stderr, err := runBatchRememberCapturingStderr(t, manifest)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "no source anchor", "batch mode must suppress nudgeIfUnanchored's per-entry stderr nudge even in plain-text mode")
}

// TestRememberBatchTalliesUnanchoredEntryCount covers Unanchored's other
// half: the count nudgeIfUnanchored's suppressed nudge rolls into instead,
// per the design summary ("tally into BatchResult.Unanchored instead").
func TestRememberBatchTalliesUnanchoredEntryCount(t *testing.T) {
	manifest := writeBatchManifest(t,
		batchManifestLine(t, map[string]any{"body": "unanchored entry one", "scope": []string{"agent:test"}, "topic": "unanchored-count-1"}),
		batchManifestLine(t, map[string]any{"body": "unanchored entry two", "scope": []string{"agent:test"}, "topic": "unanchored-count-2"}),
	)
	_, out, err := runBatchRememberJSON(t, manifest)
	require.NoError(t, err)

	var result BatchResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, 2, result.Unanchored, "both unanchored entries must be tallied into BatchResult.Unanchored")
}
