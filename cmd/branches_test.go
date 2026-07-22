package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runStaleBranches executes "cairn stale-branches" (plus extraArgs) against
// the shared rootCmd. Unlike remember's RunE (which prints via a raw
// fmt.Printf), stale-branches writes its JSON through cmd.OutOrStdout(), so
// the output can be captured directly off the buffer passed to SetOut
// instead of needing an OS-level stdout capture (see commands_test.go's
// captureStdout, which remember_test.go needs and this file doesn't).
func runStaleBranches(t *testing.T, store string, stub func(*testing.T), extraArgs ...string) ([]StaleBranchFinding, error) {
	t.Helper()
	resetStaleBranchesFlags(t)
	t.Cleanup(func() { resetStaleBranchesFlags(t) })

	stub(t)
	var out bytes.Buffer
	args := append([]string{"stale-branches", "--store", store}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	if err != nil {
		return nil, err
	}

	var findings []StaleBranchFinding
	require.NoErrorf(t, json.Unmarshal(out.Bytes(), &findings), "stale-branches output must be valid JSON: %s", out.String())
	return findings, nil
}

// resetStaleBranchesFlags clears rootCmd/staleBranchesCmd pflag state
// between tests, the same leakage concern remember_test.go's
// resetRememberFlags documents: these are package-level singletons shared
// across every test in this binary.
func resetStaleBranchesFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"notify-after", "escalate-after", "dry-run", "reviewer"} {
		f := staleBranchesCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(f.DefValue))
		f.Changed = false
	}

	idf := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, idf)
	sv, ok := idf.Value.(pflag.SliceValue)
	require.True(t, ok, "identity flag must implement pflag.SliceValue")
	require.NoError(t, sv.Replace(nil))
	idf.Changed = false
}

// commitReviewBranchAt creates a shared-tier entry and commits it to its own
// review branch with a fixed author/committer date, so these tests'
// age-bucket assertions don't race the wall clock. Mirrors
// internal/cairn/branches_test.go's helper of the same name/shape.
func commitReviewBranchAt(t *testing.T, store, topicKey string, scope []string, at time.Time) *cairn.Entry {
	t.Helper()
	e, err := cairn.NewEntry(topicKey, scope, "body text", "tester")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	iso := at.Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", iso)
	t.Setenv("GIT_COMMITTER_DATE", iso)
	_, err = e.CommitToReviewBranch(t.Context(), store)
	require.NoError(t, err)
	return e
}

func TestStaleBranchesRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"stale-branches"})
	require.NoError(t, err)
	assert.Same(t, staleBranchesCmd, found)
}

func TestStaleBranchesRejectsIdentityFlag(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	_, err := runStaleBranches(t, store, stubGC, "--identity", "rig:web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity")
}

func TestStaleBranchesRejectsEscalateAfterNotGreaterThanNotifyAfter(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	_, err := runStaleBranches(t, store, stubGC, "--notify-after", "2h", "--escalate-after", "2h")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--escalate-after")
}

// TestStaleBranchesBucketsByAge covers the AC's core detection requirement:
// three review branches at different ages resolve to fresh/notify/escalate
// independently, and only the notify-status branch is actually mailed (a
// mail send this test's stubGC always succeeds at) -- escalate gets its
// reviewer resolved but is left un-notified, since a repeat reminder once a
// branch is already escalate-eligible would just be redundant nagging.
func TestStaleBranchesBucketsByAge(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	now := time.Now()
	fresh := commitReviewBranchAt(t, store, "fresh-topic", nil, now)
	notify := commitReviewBranchAt(t, store, "notify-topic", nil, now.Add(-90*time.Minute))
	escalate := commitReviewBranchAt(t, store, "escalate-topic", nil, now.Add(-3*time.Hour))

	findings, err := runStaleBranches(t, store, stubGC, "--notify-after", "1h", "--escalate-after", "2h")
	require.NoError(t, err)
	require.Len(t, findings, 3)

	byID := map[string]StaleBranchFinding{}
	for _, f := range findings {
		byID[f.EntryID] = f
	}

	f := byID[fresh.ID]
	assert.Equal(t, "fresh", f.Status)
	assert.Empty(t, f.Reviewer, "a fresh branch must not have a reviewer resolved at all")
	assert.False(t, f.Notified)
	assert.Empty(t, f.Error)

	n := byID[notify.ID]
	assert.Equal(t, "notify", n.Status)
	assert.Equal(t, "mayor", n.Reviewer, "global tier's default reviewer")
	assert.True(t, n.Notified)
	assert.Empty(t, n.Error)

	esc := byID[escalate.ID]
	assert.Equal(t, "escalate", esc.Status)
	assert.Equal(t, "mayor", esc.Reviewer, "reviewer must still be resolved and reported for an escalate branch")
	assert.False(t, esc.Notified, "an escalate-status branch must not be mailed a redundant reminder")
	assert.Empty(t, esc.Error)
}

// TestStaleBranchesDryRunDoesNotMail proves --dry-run computes and reports
// status without actually shelling out to gc: stubGCFail would surface as a
// per-finding Error if sendStaleBranchReminder were called despite dry-run,
// so a clean run with Notified still false is what proves the call was
// skipped, not just that it happened to succeed.
func TestStaleBranchesDryRunDoesNotMail(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	e := commitReviewBranchAt(t, store, "notify-topic", nil, time.Now().Add(-90*time.Minute))

	findings, err := runStaleBranches(t, store, stubGCFail, "--notify-after", "1h", "--escalate-after", "2h", "--dry-run")
	require.NoError(t, err)
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, e.ID, f.EntryID)
	assert.Equal(t, "notify", f.Status)
	assert.Equal(t, "mayor", f.Reviewer, "dry-run must still resolve and report who would be mailed")
	assert.False(t, f.Notified)
	assert.Empty(t, f.Error, "a stubbed gc failure must never be reached under --dry-run")
}

// TestStaleBranchesMailFailureIsReportedPerBranch covers the same
// report-don't-abort stance ListReviewBranches already takes for a single
// malformed branch (see internal/cairn/branches.go's ReviewBranch.Error
// doc), extended up through this command: a failed reminder mail must not
// fail the whole call, only that one finding.
func TestStaleBranchesMailFailureIsReportedPerBranch(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	commitReviewBranchAt(t, store, "notify-topic", nil, time.Now().Add(-90*time.Minute))

	findings, err := runStaleBranches(t, store, stubGCFail, "--notify-after", "1h", "--escalate-after", "2h")
	require.NoError(t, err, "one branch's mail failure must not fail the whole command")
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, "notify", f.Status)
	assert.False(t, f.Notified)
	assert.NotEmpty(t, f.Error)
}

// TestStaleBranchesReviewerFlagOverridesPerTierDefault covers reuse of
// resolveReviewer's flag>env>computed-default precedence (crn-419.6): an
// explicit --reviewer must win for every branch mailed, regardless of each
// branch's own tier-computed default.
func TestStaleBranchesReviewerFlagOverridesPerTierDefault(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	past := time.Now().Add(-90 * time.Minute)
	rig := commitReviewBranchAt(t, store, "rig-topic", []string{"rig:web"}, past)
	role := commitReviewBranchAt(t, store, "role-topic", []string{"role:reviewer"}, past)

	findings, err := runStaleBranches(t, store, stubGC,
		"--notify-after", "1h", "--escalate-after", "2h", "--reviewer", "someone/else")
	require.NoError(t, err)
	require.Len(t, findings, 2)

	byID := map[string]StaleBranchFinding{}
	for _, f := range findings {
		byID[f.EntryID] = f
	}
	assert.Equal(t, "someone/else", byID[rig.ID].Reviewer)
	assert.Equal(t, "someone/else", byID[role.ID].Reviewer)
}

func TestBranchStatus(t *testing.T) {
	notifyAfter, escalateAfter := time.Hour, 2*time.Hour
	cases := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"well under notify threshold", 10 * time.Minute, "fresh"},
		{"just under notify threshold", notifyAfter - time.Second, "fresh"},
		{"exactly at notify threshold", notifyAfter, "notify"},
		{"between thresholds", 90 * time.Minute, "notify"},
		{"exactly at escalate threshold", escalateAfter, "escalate"},
		{"well past escalate threshold", 5 * time.Hour, "escalate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, branchStatus(tc.age, notifyAfter, escalateAfter))
		})
	}
}
