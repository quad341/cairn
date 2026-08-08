package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for `cairn import <dir>` (crn-qjxj2, design at
// scratchpad/design.md, PRD docs/PRD.md @ a1b6898). FR/NFR numbers in
// comments below refer to that PRD.

// importEntry renders a minimal valid cairn entry file (TOML frontmatter +
// markdown body) for an import source directory. This is the same on-disk
// shape seedEntry writes for store fixtures elsewhere in this package
// (commands_test.go) -- OQ2 explicitly reuses ParseEntry's existing format
// unchanged, so a source-directory fixture and a store fixture differ only
// in which directory the caller points at, never in content shape.
func importEntry(id, topicKey string, scope []string) string {
	quoted := make([]string, len(scope))
	for i, s := range scope {
		quoted[i] = strconv.Quote(s)
	}
	return fmt.Sprintf("+++\nid = %q\ntitle = %q\ntopic_key = %q\nscope = [%s]\n+++\nbody for %s\n",
		id, id, topicKey, strings.Join(quoted, ", "), id)
}

// resetImportFlags clears importCmd's own --recursive flag. rootCmd and its
// subcommands are package-level singletons whose pflag state otherwise
// leaks across tests in this binary -- mirrors resetDoctorFlags/
// resetReviewFlags/resetRememberFlags elsewhere in this package.
func resetImportFlags(t *testing.T) {
	t.Helper()
	f := importCmd.Flags().Lookup("recursive")
	require.NotNil(t, f)
	require.NoError(t, f.Value.Set("false"))
	f.Changed = false
}

// runImportCmd executes "cairn import <dir>" (plus extraArgs) against the
// shared rootCmd. Only safe for a scenario expected to exit 0: importCmd's
// RunE mirrors doctorCmd's os.Exit(code) convention (see doctor.go) for any
// non-zero runImport outcome, which would kill the test binary outright. A
// test expecting a reported failure/non-zero exit must use
// newImportTestCmd + a direct runImport call instead, exactly as
// doctor_test.go's TestRunDoctorFindingsPresentExitOne does.
func runImportCmd(t *testing.T, dir string, extraArgs ...string) error {
	t.Helper()
	resetImportFlags(t)
	t.Cleanup(func() { resetImportFlags(t) })

	args := append([]string{"import", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

// newImportTestCmd points storePath() at dir and returns importCmd wired
// with a resolvable context and its own output buffer, for driving
// runImport directly -- bypassing rootCmd.Execute()/os.Exit entirely --
// the same way doctor_test.go's newDoctorTestCmd drives runDoctor.
//
// The returned buffer alone is not a reliable capture: doctorCmd's RunE is
// confirmed (doctor.go) to print via cmd.OutOrStdout(), which SetOut(&buf)
// catches, but import.go mirrors remember.go's structure (design.md,
// "following cmd/remember.go's existing structure") and remember.go's RunE
// instead prints via bare fmt.Println/Printf (see captureStdout's doc
// comment in commands_test.go), which SetOut cannot intercept at all.
// Callers needing output content must wrap the runImport call in
// captureStdout too and combine both: buf.String() + the piped capture --
// correct regardless of which of the two conventions the implementation
// actually uses.
func newImportTestCmd(t *testing.T, dir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	resetImportFlags(t)
	t.Cleanup(func() { resetImportFlags(t) })

	prevStore := storeFlag
	storeFlag = dir
	t.Cleanup(func() { storeFlag = prevStore })

	var buf bytes.Buffer
	importCmd.SetOut(&buf)
	importCmd.SetErr(&buf)
	importCmd.SetContext(t.Context())
	t.Cleanup(func() {
		importCmd.SetContext(nil) //nolint:staticcheck // restores cobra's own "never set" zero value
		importCmd.SetOut(nil)
		importCmd.SetErr(nil)
	})
	return importCmd, &buf
}

// stubGCCapturingAllCalls shadows gc with a stub that appends every
// invocation's argv to captureFile -- one base64-encoded line per argument
// (matching remember_test.go's readStubGCArgs encoding), with a "---\n"
// sentinel between invocations. Unlike remember_test.go's stubGCCapturing
// (a single `>` redirect, overwritten on each call, sufficient because no
// existing remember.go test triggers more than one mail call), a batch
// import can invoke `gc mail send` once per distinct resolved reviewer
// within a single run, and these tests need to observe every call, not
// just the last.
func stubGCCapturingAllCalls(t *testing.T, captureFile string) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\n"+
		"{ for a in \"$@\"; do printf '%s' \"$a\" | base64 -w0; printf '\\n'; done; printf -- '---\\n'; } >> "+shellQuote(captureFile)+"\n"+
		"exit 0\n")
}

// readStubGCCalls reads back every stubGCCapturingAllCalls invocation's
// argv, in call order.
func readStubGCCalls(t *testing.T, captureFile string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(captureFile)
	require.NoError(t, err, "the gc stub must have run and recorded at least one invocation")

	var calls [][]string
	for _, block := range strings.Split(string(raw), "---\n") {
		block = strings.TrimSuffix(block, "\n")
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		args := make([]string, len(lines))
		for i, l := range lines {
			decoded, err := base64.StdEncoding.DecodeString(l)
			require.NoError(t, err, "line %d (%q) must be valid base64", i, l)
			args[i] = string(decoded)
		}
		calls = append(calls, args)
	}
	return calls
}

func TestImportRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"import"})
	require.NoError(t, err)
	assert.Same(t, importCmd, found)
}

// TestImportSingleReviewerTierSendsExactlyOneMail covers FR-1: a batch of
// many entries that all resolve to the same reviewer must produce exactly
// one `gc mail send` call, not one per entry -- the entire point of
// sendBatchReviewMail's manifest grouping (O(distinct reviewers), not
// O(entries)).
func TestImportSingleReviewerTierSendsExactlyOneMail(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	source := t.TempDir()

	const n = 55
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("imp-fr1-%03d", i)
		seedEntry(t, source, id+".md", importEntry(id, "fr1-topic", []string{"rig:web"}))
	}

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	stubGCCapturingAllCalls(t, captureFile)

	var err error
	captureStdout(t, func() { err = runImportCmd(t, source, "--store", store) })
	require.NoError(t, err)

	calls := readStubGCCalls(t, captureFile)
	require.Len(t, calls, 1, "a single-reviewer-tier batch must send exactly one mail, not one per entry")
}

// TestImportMailInvokedWithExpectedRecipientAndContent covers FR-4: the one
// manifest mail per reviewer must enumerate every entry it covers -- id,
// topic_key, and review branch -- mirroring remember_test.go's
// TestRememberSharedTierMailInvokedWithExpectedRecipientAndContent for the
// single-entry case.
func TestImportMailInvokedWithExpectedRecipientAndContent(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	source := t.TempDir()
	seedEntry(t, source, "a.md", importEntry("imp-fr4-a", "fr4-topic", []string{"rig:web"}))
	seedEntry(t, source, "b.md", importEntry("imp-fr4-b", "fr4-topic", []string{"rig:web"}))

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	stubGCCapturingAllCalls(t, captureFile)

	var err error
	captureStdout(t, func() { err = runImportCmd(t, source, "--store", store) })
	require.NoError(t, err)

	calls := readStubGCCalls(t, captureFile)
	require.Len(t, calls, 1)
	args := calls[0]
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, []string{"mail", "send", "test-rig/architect", "-s"}, args[:4],
		"a rig:-scope batch's default reviewer must be $GC_RIG/architect (defaultReviewer ignores the rig: tag's own value)")
	assert.Equal(t, "-m", args[5])

	body := args[6]
	for _, id := range []string{"imp-fr4-a", "imp-fr4-b"} {
		assert.Contains(t, body, id, "the mail body must name every imported entry in this reviewer's group")
		assert.Contains(t, body, "remember/"+id, "the mail body must name each entry's own review branch")
	}
	assert.Contains(t, body, "fr4-topic", "the mail body must name the entries' topic")
}

// TestImportEntryCommitFailureExcludesOnlyThatEntryFromManifest covers
// FR-3's "commit-before-mail, per entry" contract. The doomed entry sits in
// its own isolated reviewer tier (role:doomed-reviewer, distinct from the
// two ok entries' rig:web tier) so this test's expected outcome is exact
// under either of two readings of FR-3's bd acceptance-criterion text
// ("asserts zero notification for that reviewer group"): design.md's
// literal pseudocode and sequence diagram show per-entry independence with
// no group-wide suppression from a sibling's failure (manifest[reviewer] is
// appended to one successful entry at a time; a failed entry only ever
// `continue`s past that append, never removes a sibling's), which combined
// with FR-6's own acceptance criterion ("doesn't abort on first bad
// entry" -- self-contradictory with "suppress the whole shared group's
// mail" if good siblings shared a tier with a failure) makes the
// group-wide-suppression reading untenable in general. But it is also
// exactly true in the narrower case a single-member group failed, since an
// empty manifest[reviewer] naturally sends no mail for that reviewer
// regardless of which reading is right. Isolating the tier here means both
// readings agree, instead of this test's result hinging on which one is
// correct.
//
// Per design.md's guardrail ("a review of the implementation should
// specifically check that a failure fixture's entry ID shows up in the
// mail body, not just in a log line"), the failed entry's ID must still
// appear in the ok-group's mail body -- the only mail in this batch --
// as part of the failure list sendBatchReviewMail's body shape describes
// ("plus the failure list if non-empty"), not merely in stdout/--json.
func TestImportEntryCommitFailureExcludesOnlyThatEntryFromManifest(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	source := t.TempDir()
	seedEntry(t, source, "1-ok.md", importEntry("imp-fr3-ok1", "fr3-topic", []string{"rig:web"}))
	seedEntry(t, source, "2-doomed.md", importEntry("imp-fr3-doomed", "fr3-topic", []string{"role:doomed-reviewer"}))
	seedEntry(t, source, "3-ok.md", importEntry("imp-fr3-ok2", "fr3-topic", []string{"rig:web"}))

	// Pre-create the doomed entry's own review branch ref, so
	// CommitToReviewBranch's `git worktree add -b remember/imp-fr3-doomed`
	// collides and fails -- the same technique
	// TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError's
	// sibling tests use in remember_test.go to force that call to fail
	// deterministically. Import's fixture entries (unlike remember's
	// random-suffixed IDs) carry test-chosen IDs, so the resulting branch
	// name ("remember/"+id) is exact and predictable up front.
	gitOutput(t, store, "branch", "remember/imp-fr3-doomed")

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	stubGCCapturingAllCalls(t, captureFile)
	cmd, buf := newImportTestCmd(t, store)

	var code int
	var err error
	piped := captureStdout(t, func() { code, err = runImport(cmd, []string{source}) })
	out := buf.String() + piped
	require.NoError(t, err, "a per-entry failure is a reported finding, not an operational error (mirrors runDoctor's (1, nil) convention)")
	assert.Equal(t, 1, code, "a batch with a reported per-entry failure must exit non-zero")
	assert.Contains(t, out, "imp-fr3-doomed", "the failed entry must be named in cairn import's own reported output")

	calls := readStubGCCalls(t, captureFile)
	require.Len(t, calls, 1, "the doomed entry's isolated tier never accumulates a successful entry to mail; the ok entries' shared tier still sends its one mail")
	require.Len(t, calls[0], 7)
	assert.Equal(t, "test-rig/architect", calls[0][2], "the only mail sent must be the ok entries' rig:web group, not the doomed entry's isolated role:doomed-reviewer tier")
	body := calls[0][6]
	assert.Contains(t, body, "imp-fr3-ok1")
	assert.Contains(t, body, "imp-fr3-ok2")
	assert.Contains(t, body, "imp-fr3-doomed", "a failed entry's id must appear in the manifest mail's failure list, not just stdout (design.md guardrail)")

	// The doomed entry's file must survive on disk: entry.Create succeeded
	// before CommitToReviewBranch was ever attempted, and a downstream
	// commit failure must not roll back an already-durable write.
	_, statErr := os.Stat(filepath.Join(store, "rig", "web", "imp-fr3-doomed.md"))
	assert.NoError(t, statErr, "a commit failure must not roll back the entry's already-written file")
}

// TestImportedEntryVisibleAfterReviewMerge covers FR-5: cmd/review.go needs
// zero changes for an imported entry's branch to merge and become visible.
// Mirrors remember_test.go's
// TestRememberSharedTierRigScopeVisibleAfterReviewMerge, substituting
// `cairn import` for `cairn remember` as the branch's origin. Unlike
// remember (a random suffix appended to topic_key), import's fixture file
// controls the entry's id directly, so the review branch name
// ("remember/"+id) is known up front without capturing and parsing it back
// out of anything.
func TestImportedEntryVisibleAfterReviewMerge(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	source := t.TempDir()
	seedEntry(t, source, "entry.md", importEntry("imp-fr5-merge", "fr5-topic", []string{"rig:web"}))

	stubGC(t) // also pins GC_RIG=test-rig, needed by defaultReviewer
	var err error
	captureStdout(t, func() { err = runImportCmd(t, source, "--store", store) })
	require.NoError(t, err)

	branch := "remember/imp-fr5-merge"
	require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "fr5-topic"))

	var listErr error
	out := captureStdout(t, func() { listErr = runList(t, store, "fr5-topic", "--identity", "rig:web") })
	require.NoError(t, listErr)
	assert.Contains(t, out, "imp-fr5-merge", "the merged entry must be visible to a rig:web identity via cairn list")
}

// TestImportMixedBatchReportsPerEntryOutcomeWithoutAborting covers FR-6 and
// NFR-4: a batch mixing good entries with a parse failure and a
// duplicate-id collision must report each bad entry individually (in both
// cairn import's own output and, per design.md's guardrail, the manifest
// mail's body) and keep processing -- not abort on the first bad entry --
// so entries after a bad one still get imported.
func TestImportMixedBatchReportsPerEntryOutcomeWithoutAborting(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	// An entry already present in the store. Find (internal/cairn/entry.go)
	// self-heals a miss with one forced Reindex retry before concluding an
	// id genuinely doesn't exist, so this explicit Reindex isn't strictly
	// load-bearing -- but it removes any doubt, mirrors doctor_test.go's
	// seedPrewarmedCleanStore (same defensive idiom), and documents intent:
	// this entry is meant to be found.
	seedEntry(t, store, "rig/web/existing.md", importEntry("imp-fr6-dup", "existing-topic", []string{"rig:web"}))
	_, err := cairn.Reindex(t.Context(), store)
	require.NoError(t, err)

	source := t.TempDir()
	seedEntry(t, source, "1-good.md", importEntry("imp-fr6-good1", "fr6-topic", []string{"rig:web"}))
	// Unterminated frontmatter fence: a genuine ParseEntry error under any
	// reasonable implementation, unlike a plain frontmatter-less file (whose
	// errNotEntry classification IterEntries silently skips elsewhere in
	// this package). Deliberately avoided here so this fixture is
	// unambiguous regardless of how import.go treats that distinction.
	seedEntry(t, source, "2-malformed.md", "+++\nid = \"imp-fr6-malformed\"\ntitle = \"broken\"\n")
	seedEntry(t, source, "3-duplicate.md", importEntry("imp-fr6-dup", "fr6-topic", []string{"rig:web"}))
	seedEntry(t, source, "4-good.md", importEntry("imp-fr6-good2", "fr6-topic", []string{"rig:web"}))

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	stubGCCapturingAllCalls(t, captureFile)
	cmd, buf := newImportTestCmd(t, store)

	var code int
	var runErr error
	piped := captureStdout(t, func() { code, runErr = runImport(cmd, []string{source}) })
	out := buf.String() + piped
	require.NoError(t, runErr)
	assert.Equal(t, 1, code, "a batch with reported per-entry failures must exit non-zero")

	assert.Contains(t, out, "2-malformed.md", "the unparseable file must be named in cairn import's own reported output")
	assert.Contains(t, out, "imp-fr6-dup", "the duplicate id must be named in cairn import's own reported output")

	for _, id := range []string{"imp-fr6-good1", "imp-fr6-good2"} {
		_, statErr := os.Stat(filepath.Join(store, "rig", "web", id+".md"))
		assert.NoError(t, statErr, "processing must not abort on a bad entry -- later good entries must still be imported: "+id)
		gitOutput(t, store, "rev-parse", "--verify", "remember/"+id)
	}

	calls := readStubGCCalls(t, captureFile)
	require.Len(t, calls, 1, "both good entries share one reviewer and must still be mailed as a group despite the failures")
	require.Len(t, calls[0], 7)
	body := calls[0][6]
	assert.Contains(t, body, "2-malformed.md", "a parse failure must appear in the manifest mail's failure list, not just stdout (design.md guardrail)")
	assert.Contains(t, body, "imp-fr6-dup", "a duplicate-id failure must appear in the manifest mail's failure list, not just stdout (design.md guardrail)")

	originalContent, err := os.ReadFile(filepath.Join(store, "rig", "web", "existing.md"))
	require.NoError(t, err)
	assert.Contains(t, string(originalContent), "existing-topic",
		"the pre-existing entry must survive unmodified -- a duplicate id must be skipped, not overwritten")
}

// TestImportDefaultsNonRecursiveRecursiveFlagDescendsSubdirectories covers
// the --recursive flag: by default only files directly under <dir> are
// imported; --recursive additionally descends into subdirectories.
func TestImportDefaultsNonRecursiveRecursiveFlagDescendsSubdirectories(t *testing.T) {
	source := t.TempDir()
	seedEntry(t, source, "top.md", importEntry("imp-rec-top", "rec-topic", []string{"rig:web"}))
	seedEntry(t, source, "nested/deep.md", importEntry("imp-rec-nested", "rec-topic", []string{"rig:web"}))

	t.Run("non-recursive by default", func(t *testing.T) {
		store := t.TempDir()
		gitInit(t, store)
		stubGC(t)
		var err error
		captureStdout(t, func() { err = runImportCmd(t, source, "--store", store) })
		require.NoError(t, err)

		_, topErr := os.Stat(filepath.Join(store, "rig", "web", "imp-rec-top.md"))
		assert.NoError(t, topErr, "a top-level file must be imported by default")
		_, nestedErr := os.Stat(filepath.Join(store, "rig", "web", "imp-rec-nested.md"))
		assert.True(t, os.IsNotExist(nestedErr), "a file in a subdirectory must not be imported without --recursive")
	})

	t.Run("recursive with the flag", func(t *testing.T) {
		store := t.TempDir()
		gitInit(t, store)
		stubGC(t)
		var err error
		captureStdout(t, func() { err = runImportCmd(t, source, "--store", store, "--recursive") })
		require.NoError(t, err)

		for _, id := range []string{"imp-rec-top", "imp-rec-nested"} {
			_, statErr := os.Stat(filepath.Join(store, "rig", "web", id+".md"))
			assert.NoError(t, statErr, "--recursive must import files in subdirectories too: "+id)
		}
	})
}

// TestImportLargeBatchGroupsMailByReviewerAndCreatesEveryBranch covers
// NFR-3: a 190-entry batch (matching the real incident size that motivated
// this design) split across two distinct reviewers -- role:builder and
// role:reviewer, since a rig:-scope batch's default reviewer ignores its
// tag's own value entirely (always $GC_RIG/architect regardless of which
// rig: value is used), so only role:/agent: tags actually distinguish
// reviewer groups -- must still create every entry's own review branch and
// group mail correctly by reviewer at scale.
func TestImportLargeBatchGroupsMailByReviewerAndCreatesEveryBranch(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	source := t.TempDir()

	const nBuilder = 100
	const nReviewer = 90
	for i := 0; i < nBuilder; i++ {
		id := fmt.Sprintf("imp-nfr3-builder-%03d", i)
		seedEntry(t, source, id+".md", importEntry(id, "nfr3-topic", []string{"role:builder"}))
	}
	for i := 0; i < nReviewer; i++ {
		id := fmt.Sprintf("imp-nfr3-reviewer-%03d", i)
		seedEntry(t, source, id+".md", importEntry(id, "nfr3-topic", []string{"role:reviewer"}))
	}

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	stubGCCapturingAllCalls(t, captureFile)
	var err error
	captureStdout(t, func() { err = runImportCmd(t, source, "--store", store) })
	require.NoError(t, err)

	calls := readStubGCCalls(t, captureFile)
	require.Len(t, calls, 2, "two distinct reviewer groups must produce exactly two mail calls")

	byReviewer := map[string][]string{}
	for _, args := range calls {
		require.Len(t, args, 7)
		byReviewer[args[2]] = args
	}
	require.Contains(t, byReviewer, "test-rig/builder")
	require.Contains(t, byReviewer, "test-rig/reviewer")
	assert.Equal(t, nBuilder, strings.Count(byReviewer["test-rig/builder"][6], "remember/"),
		"the builder-group mail must name exactly one branch per entry in that group")
	assert.Equal(t, nReviewer, strings.Count(byReviewer["test-rig/reviewer"][6], "remember/"),
		"the reviewer-group mail must name exactly one branch per entry in that group")

	for _, id := range []string{
		"imp-nfr3-builder-000", fmt.Sprintf("imp-nfr3-builder-%03d", nBuilder-1),
		"imp-nfr3-reviewer-000", fmt.Sprintf("imp-nfr3-reviewer-%03d", nReviewer-1),
	} {
		gitOutput(t, store, "rev-parse", "--verify", "remember/"+id)
	}
}
