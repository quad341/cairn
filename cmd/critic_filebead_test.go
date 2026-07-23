package cmd

import (
	"context"
	"testing"

	"github.com/quad341/cairn/internal/critic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// argValue returns the value following flag in args (e.g. "--type" ->
// "bug"), failing the test if flag is absent or has no following value.
func argValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, a := range args {
		if a == flag {
			require.Less(t, i+1, len(args), "flag %s has no following value", flag)
			return args[i+1]
		}
	}
	t.Fatalf("args %v does not contain flag %s", args, flag)
	return ""
}

// TestFileCriticBeadPassFilesNothing is crn-rqf.2's acceptance criterion "a
// pass verdict produces no bead". This is safely testable without mocking
// bd: the Pass check short-circuits before FileCriticBead ever reaches
// exec.CommandContext.
func TestFileCriticBeadPassFilesNothing(t *testing.T) {
	r := critic.NewResult(critic.DimensionRecall, "some-scenario", critic.Pass, "all good")
	id, err := FileCriticBead(context.Background(), r)
	require.NoError(t, err)
	assert.Empty(t, id)
}

// TestCriticBeadTaxonomyCoversAllDimensions guards against a future 6th
// critic-loop dimension silently degrading to the "unknown dimension"
// fallback: every dimension SelectTarget can ever choose must have its own
// taxonomy row.
func TestCriticBeadTaxonomyCoversAllDimensions(t *testing.T) {
	for _, dim := range critic.Dimensions {
		_, ok := criticBeadTaxonomy[dim]
		assert.True(t, ok, "dimension %s has no criticBeadTaxonomy row", dim)
	}
}

// TestCriticBeadArgsMapping is crn-rqf.2's core acceptance criterion: a fail
// or degraded verdict produces exactly one bd create call with type,
// dimension label, source:cairn-critic-loop label, and priority set
// correctly per the mapping (bug/P1 for recall and scope-precedence, bug/P0
// for freshness -- the "worse than a crash" dimension -- task/P2 for perf
// and ergonomics).
func TestCriticBeadArgsMapping(t *testing.T) {
	cases := []struct {
		dim          critic.Dimension
		wantType     string
		wantPriority string
	}{
		{critic.DimensionRecall, "bug", "1"},
		{critic.DimensionScopePrecedence, "bug", "1"},
		{critic.DimensionFreshness, "bug", "0"},
		{critic.DimensionPerf, "task", "2"},
		{critic.DimensionErgonomics, "task", "2"},
	}
	for _, verdict := range []critic.Verdict{critic.Fail, critic.Degraded} {
		for _, c := range cases {
			t.Run(string(c.dim)+"_"+string(verdict), func(t *testing.T) {
				r := critic.NewResult(c.dim, "scenario-id", verdict, "expected X, got Y")
				args := criticBeadArgs(r)

				assert.Equal(t, "create", args[0])
				assert.Equal(t, c.wantType, argValue(t, args, "--type"))
				assert.Equal(t, c.wantPriority, argValue(t, args, "--priority"))
				assert.Equal(t, "dim:"+string(c.dim)+",source:cairn-critic-loop", argValue(t, args, "--labels"))
				assert.Contains(t, args, "--silent")
			})
		}
	}
}

// TestCriticBeadArgsAcceptanceCarriesReproExpectedActualAndRationale is
// crn-rqf.2's acceptance criterion for the --acceptance string: it must
// carry the exact repro command, expected-versus-actual output, and the
// scenario dimension rationale.
func TestCriticBeadArgsAcceptanceCarriesReproExpectedActualAndRationale(t *testing.T) {
	r := critic.NewResult(critic.DimensionRecall, recallScenarioIDForTest, critic.Fail, "missing (false negative): [abc]; leaked (false positive): []")
	args := criticBeadArgs(r)
	acceptance := argValue(t, args, "--acceptance")

	assert.Contains(t, acceptance, "go test ./internal/critic/ -run TestRunRecallScenarioPasses -v", "must carry the exact repro command")
	assert.Contains(t, acceptance, "missing (false negative): [abc]; leaked (false positive): []", "must carry the expected-vs-actual output")
	assert.Contains(t, acceptance, "recall:", "must carry the dimension rationale")
}

// recallScenarioIDForTest stands in for internal/critic's own unexported
// recallScenarioID: cmd has no access to it, and the exact value doesn't
// matter to criticBeadArgs, only that ScenarioID flows through untouched.
const recallScenarioIDForTest = "recall-subset-match"

// TestCriticBeadArgsUnknownDimensionDegradesToFeature covers a dimension
// criticBeadTaxonomy has never seen: it must not guess bug or task, and its
// repro/rationale text must name the offending dimension rather than going
// silent.
func TestCriticBeadArgsUnknownDimensionDegradesToFeature(t *testing.T) {
	r := critic.NewResult(critic.Dimension("made-up-dimension"), "scenario-id", critic.Fail, "some detail")
	args := criticBeadArgs(r)

	assert.Equal(t, "feature", argValue(t, args, "--type"))
	assert.Equal(t, "3", argValue(t, args, "--priority"))
	assert.Equal(t, "dim:made-up-dimension,source:cairn-critic-loop", argValue(t, args, "--labels"))
	assert.Contains(t, argValue(t, args, "--acceptance"), "made-up-dimension")
}

// TestCriticBeadArgsTitleNamesDimensionVerdictAndScenario asserts the bead
// title is identifiable at a glance in a bd list, without needing to open
// the bead.
func TestCriticBeadArgsTitleNamesDimensionVerdictAndScenario(t *testing.T) {
	r := critic.NewResult(critic.DimensionPerf, "perf-visible-at-scale", critic.Degraded, "took 1.2s")
	args := criticBeadArgs(r)
	title := argValue(t, args, "--title")

	assert.Contains(t, title, "perf")
	assert.Contains(t, title, "degraded")
	assert.Contains(t, title, "perf-visible-at-scale")
}

// TestCriticBeadArgsNoSpecialRouting is crn-rqf.2's acceptance criterion
// that a filed bead carries no special routing outside the fleet normal
// ready-to-build/needs-pm pipeline: no assignee, parent, or metadata flag.
func TestCriticBeadArgsNoSpecialRouting(t *testing.T) {
	r := critic.NewResult(critic.DimensionRecall, "scenario-id", critic.Fail, "detail")
	args := criticBeadArgs(r)

	for _, forbidden := range []string{"--assignee", "-a", "--parent", "--metadata", "--deps"} {
		assert.NotContains(t, args, forbidden, "critic-loop findings must not set special routing beyond the taxonomy labels")
	}
}
