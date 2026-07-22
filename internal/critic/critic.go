// Package critic implements the select-target, stress-test, and judge
// steps of the mol-cairn-critic dogfood loop (crn-7oa section 6 AF-3): a
// rotating mechanical check of internal/cairn's real behavior against a
// real store, with a deterministic pass/fail/degraded verdict rather than
// an LLM's subjective read of the output.
package critic

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Dimension is one of the 5 axes the critic loop rotates across.
type Dimension string

// The 5 critic-loop dimensions (crn-7oa section 13 item 4).
const (
	DimensionRecall          Dimension = "recall"
	DimensionScopePrecedence Dimension = "scope-precedence"
	DimensionFreshness       Dimension = "freshness"
	DimensionPerf            Dimension = "perf"
	DimensionErgonomics      Dimension = "ergonomics"
)

// Dimensions is the canonical, stable rotation order used by SelectTarget.
var Dimensions = []Dimension{
	DimensionRecall,
	DimensionScopePrecedence,
	DimensionFreshness,
	DimensionPerf,
	DimensionErgonomics,
}

// SelectTarget picks the dimension for a given loop iteration (crn-rqf's
// own step 1): a deterministic round-robin, so the same iteration number
// always selects the same dimension and a full 5-iteration cycle covers
// every dimension exactly once. Negative iterations wrap the same as
// positive ones.
func SelectTarget(iteration int) Dimension {
	n := len(Dimensions)
	return Dimensions[((iteration%n)+n)%n]
}

// Verdict is a scenario's mechanical outcome — always derived from a
// concrete expected value, threshold, or shape check, never inferred.
type Verdict string

// The 3 mechanical verdicts a scenario judge may return.
const (
	Pass     Verdict = "pass"
	Fail     Verdict = "fail"
	Degraded Verdict = "degraded"
)

// Result is one scenario run, matching crn-7oa section 4's DOGFOOD_RUN
// entity (dimension, scenario_id, verdict, timestamp) plus a human-readable
// Detail explaining the verdict.
type Result struct {
	Dimension  Dimension
	ScenarioID string
	Verdict    Verdict
	Detail     string
	Timestamp  string
}

// NewResult builds a Result, stamping the current time so callers never
// have to. Exported so a scenario living outside this package (cmd's
// ergonomics scenario needs cobra internals this package can't import
// without a cycle) still produces a Result in the exact same shape.
func NewResult(dim Dimension, scenarioID string, verdict Verdict, detail string) Result {
	return Result{
		Dimension:  dim,
		ScenarioID: scenarioID,
		Verdict:    verdict,
		Detail:     detail,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// nonce returns a short random hex string. Every scenario uses one to
// namespace the topic keys and scope tags of the fixture entries it
// creates, so repeated or concurrent runs against a real, shared store can
// never collide with each other or with real content.
func nonce() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
