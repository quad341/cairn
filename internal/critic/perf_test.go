package critic

import (
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPerfScenarioDoesNotFail(t *testing.T) {
	// Asserts != Fail rather than == Pass: the fixed thresholds are already
	// generous for perfFixtureCount entries (see perf.go), but this test
	// runs under `go test -race` on shared, possibly-loaded CI hardware --
	// pinning to the tighter pass/degraded boundary would make this test
	// itself flaky over something classifyPerfLatency's own unit tests
	// below already cover precisely.
	store := t.TempDir()
	r := RunPerfScenario(store)
	require.NotEqual(t, Fail, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, DimensionPerf, r.Dimension)
	assert.Equal(t, perfScenarioID, r.ScenarioID)
}

func TestRunPerfScenarioCleansUpAfterItself(t *testing.T) {
	store := t.TempDir()
	r := RunPerfScenario(store)
	require.NotEqual(t, Fail, r.Verdict, "detail: %s", r.Detail)

	entries, err := cairn.IterEntries(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "RunPerfScenario must leave no entries behind in the store after a run")
}

func TestClassifyPerfLatency(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
		want    Verdict
	}{
		{"well under pass threshold", 1 * time.Millisecond, Pass},
		{"just under pass threshold", perfPassThreshold - time.Millisecond, Pass},
		{"exactly at pass threshold rolls into degraded", perfPassThreshold, Degraded},
		{"midpoint between thresholds", (perfPassThreshold + perfFailThreshold) / 2, Degraded},
		{"just under fail threshold", perfFailThreshold - time.Millisecond, Degraded},
		{"exactly at fail threshold", perfFailThreshold, Fail},
		{"well over fail threshold", perfFailThreshold + time.Second, Fail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyPerfLatency(tc.elapsed))
		})
	}
}
