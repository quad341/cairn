package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitCommitAll stages and commits everything in dir -- the cmd package has
// no shared helper for this (only gitOutput); mirrors
// internal/cairn/freshness_test.go's package-scoped gitCommitAll, which
// cannot be reused across packages.
func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitOutput(t, dir, "add", "-A")
	gitOutput(t, dir, "commit", "-q", "-m", msg)
}

// seedCommittedEntry creates and commits an entry directly to store's
// current branch -- cull-evict's precondition: both EvictDirect and
// EvictToReviewBranch operate via `git rm` on an already-tracked file,
// unlike remember's add-only commit primitives, so the fixture must be
// committed, not just written.
func seedCommittedEntry(t *testing.T, store, topic string, scope []string) *cairn.Entry {
	t.Helper()
	e, err := cairn.NewEntry(topic, scope, "a body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	gitCommitAll(t, store, "add "+e.ID)
	return e
}

func resetCullEvictFlags(t *testing.T) {
	t.Helper()
	f := cullEvictCmd.Flags().Lookup("reviewer")
	require.NotNil(t, f)
	require.NoError(t, f.Value.Set(""))
	f.Changed = false
}

// runCullEvict executes "cairn cull-evict <id>" against the shared
// rootCmd, stubbing gc to always succeed. Mirrors runRemember.
func runCullEvict(t *testing.T, store, id string, extraArgs ...string) error {
	t.Helper()
	resetCullEvictFlags(t)
	t.Cleanup(func() { resetCullEvictFlags(t) })

	stubGC(t)
	args := append([]string{"cull-evict", "--store", store, id}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

func TestCullEvictPrivateTierDeletesDirectlyAndReportsSHA(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, "old-fact", []string{"agent:bot"})

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = runCullEvict(t, store, e.ID)
	})
	require.NoError(t, runErr)

	_, statErr := os.Stat(e.BodyPath)
	assert.True(t, os.IsNotExist(statErr), "a private-tier cull-evict must delete the entry file directly")

	head := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	lines := strings.Fields(strings.TrimSpace(stdout))
	require.Len(t, lines, 1, "a private-tier cull-evict must print only the eviction commit SHA")
	assert.Equal(t, head, lines[0])

	log := strings.TrimSpace(gitOutput(t, store, "log", "--oneline"))
	assert.Len(t, strings.Split(log, "\n"), 3, "exactly one new commit (the eviction) on top of init+add")
}

func TestCullEvictSharedTierProposesReviewBranchAndDoesNotDeleteDirectly(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := seedCommittedEntry(t, store, "old-fact", []string{"rig:web"})

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = runCullEvict(t, store, e.ID)
	})
	require.NoError(t, runErr)

	_, statErr := os.Stat(e.BodyPath)
	assert.NoError(t, statErr, "a shared-tier cull-evict must NOT delete the entry file on the store's own branch -- NFR-07")

	branch := "cull/" + e.ID
	gitOutput(t, store, "rev-parse", "--verify", branch)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	status := gitOutput(t, store, "diff-tree", "--no-commit-id", "--name-status", "-r", branch)
	assert.Contains(t, status, "D\t"+rel, "the cull branch's commit must delete the entry file")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "a shared-tier cull-evict must print the review branch and the mailed reviewer -- no commit SHA")
	assert.Contains(t, lines[0], branch)
}

func TestCullEvictUnknownIDReturnsClearError(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	err := runCullEvict(t, store, "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
