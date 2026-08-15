package critic

import (
	"context"
	"fmt"
	"time"

	"github.com/quad341/cairn/internal/cairn"
)

const perfScenarioID = "perf-visible-at-scale"

// perfFixtureCount is how many fixture entries are seeded before timing.
// perfPassThreshold/perfFailThreshold are fixed wall-clock buckets, not a
// relative/comparative benchmark, so the verdict is reproducible run to run.
// Under raceEnabled they're scaled way up: the race detector instruments
// every SQL statement modernc.org/sqlite's pure-Go driver executes, which
// measured ~50x slower for this scenario's Reindex than an unraced run —
// an instrumentation cost, not a real regression, so holding race builds to
// the same thresholds as normal builds would just make this scenario flaky
// under `go test -race` rather than tell us anything about Visible().
const perfFixtureCount = 500

var (
	perfPassThreshold = perfThreshold(500*time.Millisecond, 10*time.Second)
	perfFailThreshold = perfThreshold(2500*time.Millisecond, 30*time.Second)
)

func perfThreshold(normal, raced time.Duration) time.Duration {
	if raceEnabled {
		return raced
	}
	return normal
}

// RunPerfScenario seeds perfFixtureCount real entries into store — entry.go
// has no index to lean on, it's a full filesystem walk per query, so this is
// the dimension that would actually notice a regression there — times a
// real Visible() query against that scale, and classifies the elapsed time
// against the 2 fixed thresholds. It also asserts recall didn't silently
// degrade under load: a query that returns fast because it truncated
// results would otherwise read as a pass.
func RunPerfScenario(ctx context.Context, store string) Result {
	n, err := nonce()
	if err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("nonce: %v", err))
	}
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
		if err != nil {
			return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("build fixture %d: %v", i, err))
		}
		entries = append(entries, e)
	}

	cleanup, err := seedEntries(ctx, store, entries)
	defer cleanup()
	if err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	// seedEntries commits, which moves HEAD and leaves the index watermark
	// behind it. Visible() -> Status() -> ensureFresh() would therefore
	// rebuild the whole index from a perfFixtureCount-file walk INSIDE the
	// timed region below, and that rebuild — not the query — is what the
	// number would end up reporting.
	//
	// That is not a rounding error, and the rebuild is where all the
	// variance lives. Idle, the cold call measured 81-115ms against 7.7-12.8ms
	// for the query itself. Under 12 concurrent processes the cold call
	// stretched to 801ms while the warm query held at 5-28ms across the same
	// 48 samples. Running both shapes simultaneously on one box, 72 samples
	// each, the pre-hoist scenario returned 2 Fail and 56 Degraded verdicts
	// (slowest 2546ms) and the hoisted one returned 72 Pass (slowest 71ms) —
	// so the Fail verdict, and the bead auto-filed off it, described an index
	// rebuild while claiming to describe Visible() (crn-9k30). Build the index
	// here so the measurement below is the query this scenario says it times.
	// Reindex cost is real but it is a different dimension's job.
	if _, err := cairn.Reindex(ctx, store); err != nil {
		return NewResult(DimensionPerf, perfScenarioID, Fail, fmt.Sprintf("warm index: %v", err))
	}

	start := time.Now()
	visible, err := cairn.Visible(ctx, store, []string{rig})
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
