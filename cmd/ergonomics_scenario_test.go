package cmd

import (
	"testing"

	"github.com/quad341/cairn/internal/critic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunErgonomicsScenarioPasses(t *testing.T) {
	r := RunErgonomicsScenario()
	require.Equal(t, critic.Pass, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, critic.DimensionErgonomics, r.Dimension)
	assert.Equal(t, ergonomicsScenarioID, r.ScenarioID)
}

func TestRunErgonomicsScenarioIsRepeatable(t *testing.T) {
	r1 := RunErgonomicsScenario()
	require.Equal(t, critic.Pass, r1.Verdict, "detail: %s", r1.Detail)
	r2 := RunErgonomicsScenario()
	require.Equal(t, critic.Pass, r2.Verdict, "detail: %s", r2.Detail)
}

func TestCheckStatusRejectsIdentity(t *testing.T) {
	r := checkStatusRejectsIdentity()
	assert.Equal(t, critic.Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckGetErrorsOnMissingID(t *testing.T) {
	r := checkGetErrorsOnMissingID()
	assert.Equal(t, critic.Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestCheckMapOutputShape(t *testing.T) {
	r := checkMapOutputShape()
	assert.Equal(t, critic.Pass, r.Verdict, "detail: %s", r.Detail)
}

func TestExecRootCapturesDirectStdoutPrints(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	store := t.TempDir()
	out, err := execRoot("map", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, out, "cairn map", "mapCmd prints via fmt.Printf directly; execRoot must still capture it")
}

func TestResetIdentityFlagClearsPriorValue(t *testing.T) {
	_, err := execRoot("map", "--store", t.TempDir(), "--identity", "rig:leftover")
	require.NoError(t, err)

	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	assert.False(t, f.Changed, "Changed must be cleared")
	assert.Equal(t, "[]", f.Value.String(), "the underlying StringSlice value must be replaced, not merely marked unchanged")
}
