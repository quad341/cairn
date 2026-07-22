package critic

import (
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunFreshnessScenarioPasses(t *testing.T) {
	store := t.TempDir()
	r := RunFreshnessScenario(t.Context(), store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, DimensionFreshness, r.Dimension)
	assert.Equal(t, freshnessScenarioID, r.ScenarioID)
}

func TestRunFreshnessScenarioIsRepeatable(t *testing.T) {
	store := t.TempDir()
	r1 := RunFreshnessScenario(t.Context(), store)
	require.Equal(t, Pass, r1.Verdict, "detail: %s", r1.Detail)
	r2 := RunFreshnessScenario(t.Context(), store)
	require.Equal(t, Pass, r2.Verdict, "detail: %s", r2.Detail)
}

func TestRunFreshnessScenarioCleansUpAfterItself(t *testing.T) {
	store := t.TempDir()
	r := RunFreshnessScenario(t.Context(), store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)

	entries, err := cairn.IterEntries(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "RunFreshnessScenario must leave no entries behind in the store after a passing run")
}

func TestGitInitAndCommitThenCommitFileChangesFingerprint(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	require.NoError(t, gitInitAndCommit(ctx, dir, "f.txt", "v1\n"))

	anchor := cairn.Anchor{Type: "files", Repo: dir, Paths: []string{"f.txt"}}
	fp1 := cairn.ComputeFingerprint(ctx, anchor)
	require.NotEmpty(t, fp1)

	require.NoError(t, commitFile(ctx, dir, "f.txt", "v2\n", "update"))
	fp2 := cairn.ComputeFingerprint(ctx, anchor)
	assert.NotEqual(t, fp1, fp2, "the fingerprint must change once the anchored file's committed content changes")
}

func TestCheckFreshnessNoAnchor(t *testing.T) {
	r := checkFreshnessNoAnchor(t.Context(), t.TempDir(), "unit-no-anchor")
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckFreshnessNeverVerified(t *testing.T) {
	ctx := t.Context()
	repo := t.TempDir()
	require.NoError(t, gitInitAndCommit(ctx, repo, freshnessFixtureFile, "line one\n"))

	r := checkFreshnessNeverVerified(ctx, t.TempDir(), "unit-never-verified", repo)
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckFreshnessDriftDetection(t *testing.T) {
	ctx := t.Context()
	repo := t.TempDir()
	require.NoError(t, gitInitAndCommit(ctx, repo, freshnessFixtureFile, "line one\n"))

	r := checkFreshnessDriftDetection(ctx, t.TempDir(), "unit-drift", repo)
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}
