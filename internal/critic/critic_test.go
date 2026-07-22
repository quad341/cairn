package critic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectTargetRotatesThroughAllDimensions(t *testing.T) {
	seen := make(map[Dimension]bool)
	for i := range Dimensions {
		seen[SelectTarget(i)] = true
	}
	assert.Len(t, seen, len(Dimensions), "one full cycle must cover every dimension exactly once")
}

func TestSelectTargetIsDeterministic(t *testing.T) {
	for i := -10; i < 10; i++ {
		assert.Equal(t, SelectTarget(i), SelectTarget(i), "SelectTarget(%d) must be stable across calls", i)
	}
}

func TestSelectTargetWrapsAfterFullCycle(t *testing.T) {
	n := len(Dimensions)
	for i := range n * 3 {
		assert.Equal(t, SelectTarget(i), SelectTarget(i+n), "a full cycle (%d) must repeat", n)
	}
}

func TestSelectTargetHandlesNegativeIterations(t *testing.T) {
	n := len(Dimensions)
	for i := range n {
		assert.Equal(t, SelectTarget(i), SelectTarget(i-n),
			"SelectTarget(%d) and SelectTarget(%d) must match: negative iterations wrap the same as positive", i, i-n)
	}
}

func TestDimensionsOrderIsStable(t *testing.T) {
	want := []Dimension{
		DimensionRecall,
		DimensionScopePrecedence,
		DimensionFreshness,
		DimensionPerf,
		DimensionErgonomics,
	}
	assert.Equal(t, want, Dimensions)
}

func TestNewResultStampsTimestamp(t *testing.T) {
	r := NewResult(DimensionRecall, "scenario-x", Pass, "detail text")
	assert.Equal(t, DimensionRecall, r.Dimension)
	assert.Equal(t, "scenario-x", r.ScenarioID)
	assert.Equal(t, Pass, r.Verdict)
	assert.Equal(t, "detail text", r.Detail)
	assert.NotEmpty(t, r.Timestamp)
}

func TestNonceIsNonEmptyAndVaries(t *testing.T) {
	a, err := nonce()
	assert.NoError(t, err)
	assert.NotEmpty(t, a)

	b, err := nonce()
	assert.NoError(t, err)
	assert.NotEqual(t, a, b, "two nonces in a row should not collide")
}
