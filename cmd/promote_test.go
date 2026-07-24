package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetPromoteMarkFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"bead", "reviewer"} {
		f := promoteMarkCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(""))
		f.Changed = false
	}
}

// runPromoteMark executes "cairn promote-mark <id> --bead <bead>" against
// the shared rootCmd, stubbing gc to always succeed. Mirrors runCullEvict.
func runPromoteMark(t *testing.T, store, id, bead string) error {
	t.Helper()
	resetPromoteMarkFlags(t)
	t.Cleanup(func() { resetPromoteMarkFlags(t) })

	stubGC(t)
	rootCmd.SetArgs([]string{"promote-mark", "--store", store, id, "--bead", bead})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

func TestPromoteMarkPrivateTierCommitsDirectlyAndReportsSHA(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, []string{"agent:bot"})

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = runPromoteMark(t, store, e.ID, "crn-abcd")
	})
	require.NoError(t, runErr)

	e2, err := cairn.ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID)

	head := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	lines := strings.Fields(strings.TrimSpace(stdout))
	require.Len(t, lines, 1, "a private-tier promote-mark must print only the commit SHA")
	assert.Equal(t, head, lines[0])

	log := strings.TrimSpace(gitOutput(t, store, "log", "--oneline"))
	assert.Len(t, strings.Split(log, "\n"), 3, "exactly one new commit (the promotion mark) on top of init+add")
}

func TestPromoteMarkSharedTierProposesReviewBranchAndDoesNotCommitDirectly(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, []string{"rig:web"})
	headBefore := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = runPromoteMark(t, store, e.ID, "crn-abcd")
	})
	require.NoError(t, runErr)

	headAfter := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, headAfter, "a shared-tier promote-mark must NOT commit the mutation to the store's own branch")

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	committed := gitOutput(t, store, "show", "HEAD:"+filepath.ToSlash(rel))
	assert.NotContains(t, committed, "promoted_bead_id", "the store's own HEAD must not contain the promotion mark")

	branch := "remember/" + e.ID
	gitOutput(t, store, "rev-parse", "--verify", branch)

	content := gitOutput(t, store, "show", branch+":"+filepath.ToSlash(rel))
	assert.Contains(t, content, `promoted_bead_id = "crn-abcd"`, "the review branch's commit must contain the promotion mark")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "a shared-tier promote-mark must print the review branch and the mailed reviewer -- no commit SHA")
	assert.Contains(t, lines[0], branch)
}

func TestPromoteMarkUnknownIDReturnsClearError(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	err := runPromoteMark(t, store, "does-not-exist", "crn-abcd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

func TestPromoteMarkRequiresBeadFlag(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, []string{"agent:bot"})

	err := runPromoteMark(t, store, e.ID, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bead")
}

func TestPromoteMarkSameBeadIDTwiceSucceedsAsNoOp(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, []string{"agent:bot"})

	require.NoError(t, runPromoteMark(t, store, e.ID, "crn-abcd"))
	headAfterFirst := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))

	runErr := runPromoteMark(t, store, e.ID, "crn-abcd")
	require.NoError(t, runErr, "repeating the same bead ID must succeed as a no-op")

	headAfterSecond := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	assert.Equal(t, headAfterFirst, headAfterSecond, "a repeat call with the same bead ID must not create a new commit")

	e2, err := cairn.ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID)
}

func TestPromoteMarkDifferentBeadIDOnAlreadyPromotedEntryErrors(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, []string{"agent:bot"})

	require.NoError(t, runPromoteMark(t, store, e.ID, "crn-abcd"))

	err := runPromoteMark(t, store, e.ID, "crn-wxyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), e.ID)
	assert.Contains(t, err.Error(), "crn-abcd")

	e2, err := cairn.ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID, "a rejected different-bead-ID call must not overwrite the existing promotion mark")
}
