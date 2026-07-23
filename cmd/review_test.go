package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewCLIStore is a git-initialized store with a resolvable HEAD (gitInit's
// empty initial commit), which CommitToReviewBranch requires when it does
// `git worktree add -b branch wt HEAD`.
func reviewCLIStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	gitInit(t, store)
	return store
}

// seedReviewBranch creates a shared-tier review branch through the real
// NewEntry -> Create -> CommitToReviewBranch path -- the same path
// cmd/remember.go's requestReview uses -- so these CLI-wiring tests exercise
// cairn review against exactly what `cairn remember` produces.
func seedReviewBranch(t *testing.T, store, topicKey string, scope []string, body string) (branch string, e *cairn.Entry) {
	t.Helper()
	e, err := cairn.NewEntry(topicKey, scope, body, "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	branch, err = e.CommitToReviewBranch(t.Context(), store)
	require.NoError(t, err)
	return branch, e
}

// runReviewCmd executes rootCmd with args against the shared package-level
// singletons, resetting review's own flags before and after (same rationale
// as runRememberWithGC: cobra flag values otherwise leak across tests in
// this binary).
func runReviewCmd(t *testing.T, args ...string) error {
	t.Helper()
	resetReviewFlags(t)
	t.Cleanup(func() { resetReviewFlags(t) })

	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

func resetReviewFlags(t *testing.T) {
	t.Helper()
	reset := func(cmd *cobra.Command, name, def string) {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(def))
		f.Changed = false
	}
	reset(reviewListCmd, "tier", "")
	reset(reviewMergeCmd, "topic-key", "")
	reset(reviewMergeCmd, "anchor-type", "")
	reset(reviewMergeCmd, "scope", "")
	reset(reviewMergeCmd, "bead", "")
	reset(reviewMergeCmd, "allow-secret-pattern", "false")
}

func TestReviewRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"review"})
	require.NoError(t, err)
	assert.Same(t, reviewCmd, found)
}

func TestReviewSubcommandsRegistered(t *testing.T) {
	cases := map[string]*cobra.Command{
		"list":  reviewListCmd,
		"show":  reviewShowCmd,
		"merge": reviewMergeCmd,
	}
	for use, want := range cases {
		t.Run(use, func(t *testing.T) {
			found, _, err := rootCmd.Find([]string{"review", use})
			require.NoError(t, err)
			assert.Same(t, want, found)
		})
	}
}

func TestReviewListPrintsTierAndPath(t *testing.T) {
	store := reviewCLIStore(t)
	branch, e := seedReviewBranch(t, store, "topic-a", []string{"rig:web"}, "body")

	var err error
	stdout := captureStdout(t, func() {
		err = runReviewCmd(t, "review", "list", "--store", store)
	})
	require.NoError(t, err)
	assert.Contains(t, stdout, branch)
	assert.Contains(t, stdout, "tier=rig")
	assert.Contains(t, stdout, "value=web")
	assert.Contains(t, stdout, e.ID)
}

func TestReviewListFiltersByTier(t *testing.T) {
	store := reviewCLIStore(t)
	rigBranch, _ := seedReviewBranch(t, store, "topic-rig", []string{"rig:web"}, "body")
	roleBranch, _ := seedReviewBranch(t, store, "topic-role", []string{"role:reviewer"}, "body")

	var err error
	stdout := captureStdout(t, func() {
		err = runReviewCmd(t, "review", "list", "--store", store, "--tier", "rig")
	})
	require.NoError(t, err)
	assert.Contains(t, stdout, rigBranch)
	assert.NotContains(t, stdout, roleBranch)
}

func TestReviewShowPrintsDiffAndEntryFields(t *testing.T) {
	store := reviewCLIStore(t)
	branch, e := seedReviewBranch(t, store, "topic-a", []string{"rig:web"}, "the body text")

	var err error
	stdout := captureStdout(t, func() {
		err = runReviewCmd(t, "review", "show", branch, "--store", store)
	})
	require.NoError(t, err)
	assert.Contains(t, stdout, "the body text")
	assert.Contains(t, stdout, "id:        "+e.ID)
	assert.Contains(t, stdout, "topic_key: topic-a")
	assert.Contains(t, stdout, "scope:     rig:web")
}

func TestReviewShowGlobalScopePrintsPlaceholder(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", nil, "body")

	var err error
	stdout := captureStdout(t, func() {
		err = runReviewCmd(t, "review", "show", branch, "--store", store)
	})
	require.NoError(t, err)
	assert.Contains(t, stdout, "scope:     (global)")
}

func TestReviewMergePassesFlagsThroughAndPrintsSHA(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", []string{"rig:web"}, "body")

	var err error
	stdout := captureStdout(t, func() {
		err = runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "curated")
	})
	require.NoError(t, err)

	sha := strings.TrimSpace(stdout)
	assert.NotEmpty(t, sha)
	head := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	assert.Equal(t, head, sha)
}

// TestReviewMergeMissingTopicKeyErrors covers that reviewMergeCmd has no
// cobra-level MarkFlagRequired on --topic-key -- it relies on
// MergeReviewBranch's own ValidatePathSegment("") rejection, wrapped with
// "--topic-key" so the CLI error still names the flag.
func TestReviewMergeMissingTopicKeyErrors(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", nil, "body")

	err := runReviewCmd(t, "review", "merge", branch, "--store", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--topic-key")
}

func TestReviewMergeSplitsCommaSeparatedScope(t *testing.T) {
	store := reviewCLIStore(t)
	branch, e := seedReviewBranch(t, store, "topic-a", []string{"rig:web"}, "body")

	captureStdout(t, func() {
		require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store,
			"--topic-key", "curated", "--scope", "role:a,role:b"))
	})

	got, err := cairn.Find(t.Context(), store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"role:a", "role:b"}, got.Scope)
}

func TestReviewMergeBeadFlagAppearsInCommitMessage(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", nil, "body")

	captureStdout(t, func() {
		require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store,
			"--topic-key", "curated", "--bead", "crn-999"))
	})

	subject := strings.TrimSpace(gitOutput(t, store, "log", "-1", "--format=%s"))
	assert.Contains(t, subject, "(crn-999)")
}

func TestReviewMergeKindAndAutoActionableFlagWiring(t *testing.T) {
	store := reviewCLIStore(t)
	branch, e := seedReviewBranch(t, store, "topic-a", []string{"rig:web"}, "body")

	captureStdout(t, func() {
		require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store,
			"--topic-key", "curated", "--kind", "remediation", "--auto-actionable"))
	})

	got, err := cairn.Find(t.Context(), store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, "remediation", got.Kind)
	assert.True(t, got.AutoActionable)
}

// TestReviewMergeAutoActionableWithoutRemediationKindErrors covers the CLI
// wiring of the --auto-actionable gate: passing it without --kind on an
// entry that isn't already remediation must be rejected.
func TestReviewMergeAutoActionableWithoutRemediationKindErrors(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", nil, "body")

	err := runReviewCmd(t, "review", "merge", branch, "--store", store,
		"--topic-key", "curated", "--auto-actionable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--auto-actionable")
	assert.Contains(t, err.Error(), "remediation")
}

func TestReviewMergeAllowSecretPatternFlagWiring(t *testing.T) {
	store := reviewCLIStore(t)
	branch, _ := seedReviewBranch(t, store, "topic-a", nil, "leaked: AKIAIOSFODNN7EXAMPLE")

	err := runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "curated")
	require.Error(t, err, "a secret-shaped entry must be blocked by default")

	var mergeErr error
	captureStdout(t, func() {
		mergeErr = runReviewCmd(t, "review", "merge", branch, "--store", store,
			"--topic-key", "curated", "--allow-secret-pattern")
	})
	require.NoError(t, mergeErr, "--allow-secret-pattern must override the guard")
}
