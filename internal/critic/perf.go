package critic

import (
	"fmt"
	"time"

	"github.com/quad341/cairn/internal/cairn"
)

const perfScenarioID = "perf-visible-at-scale"

// perfFixtureCount is how many fixture entries are seeded before timing.
// perfPassThreshold/perfFailThreshold are fixed wall-clock buckets, not a
// relative/comparative benchmark, so the verdict is reproducible run to run.
const (
	perfFixtureCount  = 500
	perfPassThreshold = 500 * time.Millisecond
	perfFailThreshold = 2500 * time.Millisecond
)

// RunPerfScenario seeds perfFixtureCount real entries into store — entry.go
// has no index to lean on, it's a full filesystem walk per query, so this is
// the dimension that would actually notice a regression there — times a
// real Visible() query against that scale, and classifies the elapsed time
// against the 2 fixed thresholds. It also asserts recall didn't silently
// degrade under load: a query that returns fast because it truncated
// results would otherwise read as a pass.
func RunPerfScenario(store string) Result {
	n, err := nonce()
	if err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("nonce: %v", err))
	}
	rig := "rig:critic-" + n

	entries := make([]*cairn.Entry, 0, perfFixtureCount)
	for i := range perfFixtureCount {
		e, err := cairn.NewEntry(fmt.Sprintf("critic-perf-%s-%d", n, i), []string{rig}, "perf fixture body", "critic")
		if err != nil {
			return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("build fixture %d: %v", i, err))
		}
		entries = append(entries, e)
	}

	cleanup, err := seedEntries(store, entries)
	defer cleanup()
	if err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	start := time.Now()
	visible, err := cairn.Visible(store, []string{rig})
	elapsed := time.Since(start)
	if err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("Visible: %v", err))
	}
	if len(visible) < perfFixtureCount {
		return NewResult(DimensionPerf, perfScenarioID, Fail,
			fmt.Sprintf("expected at least %d visible fixtures, got %d - recall broke under scale", perfFixtureCount, len(visible)))
	}

	verdict := classifyPerfLatency(elapsed)
	detail := fmt.Sprintf("Visible() over >=%d entries took %s (pass<%s, fail>=%s)",
		perfFixtureCount, elapsed, perfPassThreshold, perfFailThreshold)
	return NewResult(DimensionPerf, perfScenarioID, verdict, detail)
}

// classifyPerfLatency buckets elapsed against the 2 fixed thresholds. It is
// a pure function, isolated from the timing itself, so the threshold logic
// can be unit-tested directly against synthetic durations rather than real
// (and inherently noisy) wall-clock measurements.
func classifyPerfLatency(elapsed time.Duration) Verdict {
	switch {
	case elapsed < perfPassThreshold:
		return Pass
	case elapsed < perfFailThreshold:
		return Degraded
	default:
		return Fail
	}
}
