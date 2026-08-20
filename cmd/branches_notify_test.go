package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStaleBranchesNotifyBucketDoesNotRemailUnchangedBranch is crn-pr1al's
// repro. notifyState records the tip SHA a branch was last notified at, but
// evaluateBranch consulted it only to gate the *escalate* bucket -- so a
// branch sitting in the notify bucket was mailed a fresh reminder on every
// single pass, forever, with nothing about it changed: same branch, same tip,
// same reviewer. Measured against the live store before this fix, three
// consecutive passes over one frozen 32-branch snapshot sent 15, then 21,
// then 27 reminders, six of them redundant per pass and growing without
// bound; the reviewers on the receiving end had no way to tell a repeat from
// a new request.
//
// The notify window here is [1h, 1000h) so the branch stays in the notify
// bucket on every pass -- otherwise the escalate path (which already
// suppresses its own mail) would mask the behavior under test.
func TestStaleBranchesNotifyBucketDoesNotRemailUnchangedBranch(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	commitReviewBranchAt(t, store, "notify-dedup-topic", nil, time.Now().Add(-3*time.Hour))

	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	countFile := filepath.Join(dir, "gc-calls.txt")
	counting := func(t *testing.T) {
		t.Helper()
		stubGCCountingCalls(t, countFile)
	}
	args := []string{"--notify-after", "1h", "--escalate-after", "1000h", "--state-file", stateFile}

	first, err := runStaleBranches(t, store, counting, args...)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "notify", first[0].Status, "precondition: branch must sit in the notify bucket")
	require.True(t, first[0].Notified, "precondition: the first pass must actually mail")
	require.Equal(t, 1, readGCCallCount(t, countFile), "precondition: exactly one reminder so far")

	second, err := runStaleBranches(t, store, counting, args...)
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.False(t, second[0].Notified,
		"a notify-bucket branch already reminded at this exact tip must not be re-mailed")
	assert.Equal(t, 1, readGCCallCount(t, countFile),
		"an unchanged branch must not generate a second reminder mail")
}
