package critic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
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
	r := RunPerfScenario(t.Context(), store)
	require.NotEqual(t, Fail, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, DimensionPerf, r.Dimension)
	assert.Equal(t, perfScenarioID, r.ScenarioID)
}

func TestRunPerfScenarioCleansUpAfterItself(t *testing.T) {
	store := t.TempDir()
	r := RunPerfScenario(t.Context(), store)
	require.NotEqual(t, Fail, r.Verdict, "detail: %s", r.Detail)

	entries, err := cairn.IterEntries(t.Context(), store)
	require.NoError(t, err)
	assert.Empty(t, entries, "RunPerfScenario must leave no entries behind in the store after a run")
}

// TestPerfScenarioTimesTheQueryNotAnIndexRebuild is the regression test for
// the warm step in RunPerfScenario. It asserts the property that step exists
// for — that nothing inside the timed region rebuilds the index — rather
// than a wall-clock number, which under load is exactly the flaky thing this
// scenario got wrong in the first place (crn-9k30).
//
// index_drift is emitted only by ensureFreshWith (internal/cairn/index.go),
// which Visible reaches via Status. A direct Reindex emits nothing. So the
// records below are a faithful trace of what the *timed* Visible call had to
// do: exactly one record, and stale=false means it answered from a warm
// index. Delete the Reindex from perf.go and that single record flips to
// stale=true/reindexed=true and this test fails — which is the point.
func TestPerfScenarioTimesTheQueryNotAnIndexRebuild(t *testing.T) {
	store := t.TempDir()

	var buf bytes.Buffer
	logger := obslog.NewWithWriter(&buf, obslog.Options{Command: "test"}, &bytes.Buffer{})
	ctx := obslog.WithLogger(t.Context(), logger)

	r := RunPerfScenario(ctx, store)
	require.NotEqual(t, Fail, r.Verdict, "detail: %s", r.Detail)

	drifts := make([]map[string]any, 0, 1)
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["kind"] == "index_drift" {
			drifts = append(drifts, rec)
		}
	}

	require.Len(t, drifts, 1, "the scenario's single Visible call should consult index freshness exactly once")
	assert.Equal(t, false, drifts[0]["stale"],
		"the timed Visible call found the index stale and rebuilt it inside the measurement — RunPerfScenario's warm step is missing, so its number reports a 500-file index rebuild rather than the query")
	assert.NotEqual(t, true, drifts[0]["reindexed"],
		"no reindex may happen inside the timed region")
}

// TestSeedEntriesLeavesIndexStale proves the warm step above is not a no-op.
// seedEntries commits, which moves HEAD past the index watermark, so a
// Visible call made straight afterwards would rebuild the index from a
// perfFixtureCount-file walk before answering. If this ever fails, seeding no
// longer dirties the index and the warm step really has become unnecessary —
// that is the one condition under which removing it is safe.
func TestSeedEntriesLeavesIndexStale(t *testing.T) {
	store := t.TempDir()
	n, err := nonce()
	require.NoError(t, err)
	rig := "rig:critic-" + n

	entries := make([]*cairn.Entry, 0, perfFixtureCount)
	for i := range perfFixtureCount {
		e, err := cairn.NewEntry(cairn.NewEntryParams{
			Type:      cairn.EntryTypeKnowledge,
			TopicKey:  fmt.Sprintf("critic-perf-%s-%d", n, i),
			Scope:     []string{rig},
			Body:      "perf fixture body",
			CreatedBy: "critic",
		})
		require.NoError(t, err)
		entries = append(entries, e)
	}
	cleanup, err := seedEntries(t.Context(), store, entries)
	defer cleanup()
	require.NoError(t, err)

	stale, err := cairn.IndexStale(t.Context(), store)
	require.NoError(t, err)
	require.True(t, stale,
		"seedEntries must leave the index stale — while it does, RunPerfScenario's explicit Reindex is the only thing keeping a full index rebuild out of its timed region")

	_, err = cairn.Reindex(t.Context(), store)
	require.NoError(t, err)

	stale, err = cairn.IndexStale(t.Context(), store)
	require.NoError(t, err)
	require.False(t, stale, "the warm step must leave the index fresh, so the timed region measures the query and not a rebuild")
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
