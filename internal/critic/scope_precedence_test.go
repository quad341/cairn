package critic

import (
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScopePrecedenceScenarioPasses(t *testing.T) {
	store := t.TempDir()
	r := RunScopePrecedenceScenario(store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, DimensionScopePrecedence, r.Dimension)
	assert.Equal(t, scopePrecedenceScenarioID, r.ScenarioID)
}

func TestRunScopePrecedenceScenarioIsRepeatable(t *testing.T) {
	store := t.TempDir()
	r1 := RunScopePrecedenceScenario(store)
	require.Equal(t, Pass, r1.Verdict, "detail: %s", r1.Detail)
	r2 := RunScopePrecedenceScenario(store)
	require.Equal(t, Pass, r2.Verdict, "detail: %s", r2.Detail)
}

func TestRunScopePrecedenceScenarioCleansUpAfterItself(t *testing.T) {
	store := t.TempDir()
	r := RunScopePrecedenceScenario(store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)

	entries, err := cairn.IterEntries(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "RunScopePrecedenceScenario must leave no entries behind in the store after a passing run")
}

func TestCheckShadowWinsBySpecificity(t *testing.T) {
	r := checkShadowWinsBySpecificity(t.TempDir(), "unit-specificity")
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckShadowTiebreak(t *testing.T) {
	r := checkShadowTiebreak(t.TempDir(), "unit-tiebreak")
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckShadowMapSupersetSemantics(t *testing.T) {
	r := checkShadowMapSupersetSemantics(t.TempDir(), "unit-supersets")
	assert.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestMatchingTopic(t *testing.T) {
	entries := []*cairn.Entry{
		{ID: "a", TopicKey: "x"},
		{ID: "b", TopicKey: "y"},
		{ID: "c", TopicKey: "x"},
	}
	assert.Equal(t, []string{"a", "c"}, matchingTopic(entries, "x"))
	assert.Equal(t, []string{"b"}, matchingTopic(entries, "y"))
	assert.Nil(t, matchingTopic(entries, "no-such-topic"))
}
