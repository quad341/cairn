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

// TestStaleBranchesNotifyReviewerResolutionFailureReportsError is the notify
// half of what crn-w4c6 fixed for escalate only. A reviewer that cannot be
// resolved is terminal for this branch on this pass in either bucket -- no
// mail goes out and no notify state is recorded -- but evaluateBranch still
// reported status "notify", which reads downstream as "a reminder is on its
// way" when in fact nothing was sent and nothing ever will be: the branch
// cannot reach escalate either, because escalate requires a prior recorded
// notify at this tip. Reporting "error" is what makes that visible.
//
// Role tier is used deliberately: it is the tier that resolves through
// $GC_RIG, and crn-rott9.1 leaves that so while moving rig tier onto the
// entry's own declared rig -- so this test stays meaningful either side of
// that change.
func TestStaleBranchesNotifyReviewerResolutionFailureReportsError(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := commitReviewBranchAt(t, store, "notify-resolve-fail", []string{"role:reviewer"}, time.Now().Add(-3*time.Hour))

	findings, err := runStaleBranches(t, store, stubGCWithRig(""),
		"--notify-after", "1h", "--escalate-after", "1000h",
		"--state-file", filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err, "one branch's reviewer-resolution failure must not fail the whole command")
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, e.ID, f.EntryID)
	assert.Equal(t, "error", f.Status,
		`a notify-bucket finding whose reviewer resolution fails must report status "error", not "notify" -- nothing was mailed and no notify state was recorded, so it cannot progress to escalate either`)
	assert.Empty(t, f.Reviewer, "no reviewer was actually resolved")
	assert.False(t, f.Notified)
	assert.NotEmpty(t, f.Error, "the real resolveReviewer diagnostic must be surfaced")
}
