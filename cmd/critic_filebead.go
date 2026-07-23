package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/quad341/cairn/internal/critic"
)

// criticBeadTaxonomyEntry is one dimension's row in crn-7oa section 10's
// feedback-bead taxonomy: what kind of bd issue a fail/degraded verdict in
// this dimension becomes, how urgent it is, and how to reproduce it.
type criticBeadTaxonomyEntry struct {
	issueType string
	priority  int
	repro     string
	rationale string
}

// criticBeadTaxonomy maps each of the 5 critic-loop dimensions (see
// critic.Dimensions) to its bd issue type, priority, and repro/rationale
// text, per crn-7oa section 10. recall, scope-precedence, and freshness are
// all "wrong or silently-wrong behavior" -> bug; freshness alone gets the
// top priority tier because DESIGN.md frames a stale entry served as fresh
// as worse than serving none at all, a stronger claim than the other two
// get. perf and ergonomics are friction, not wrong, just costly -> task at
// a shared, lower priority. The repro commands point at each dimension's
// own "...Passes" test (confirmed present in internal/critic and cmd) since
// no CLI entry point for the critic loop exists yet (that's crn-rqf.5's
// job) -- a red run of that test exercises the exact same Run*Scenario code
// path the loop itself called.
var criticBeadTaxonomy = map[critic.Dimension]criticBeadTaxonomyEntry{
	critic.DimensionRecall: {
		issueType: "bug",
		priority:  1,
		repro:     "go test ./internal/critic/ -run TestRunRecallScenarioPasses -v",
		rationale: "recall: Visible()'s subset-match recall served a false negative or false " +
			"positive to a real identity -- wrong or silently-wrong behavior.",
	},
	critic.DimensionScopePrecedence: {
		issueType: "bug",
		priority:  1,
		repro:     "go test ./internal/critic/ -run TestRunScopePrecedenceScenarioPasses -v",
		rationale: "scope-precedence: shadow() or ShadowMap() resolved the wrong entry for a topic -- wrong or silently-wrong behavior.",
	},
	critic.DimensionFreshness: {
		issueType: "bug",
		priority:  0,
		repro:     "go test ./internal/critic/ -run TestRunFreshnessScenarioPasses -v",
		rationale: "freshness: Check() misreported an entry's freshness state -- a stale entry " +
			"served as fresh is worse than serving none at all (DESIGN.md).",
	},
	critic.DimensionPerf: {
		issueType: "task",
		priority:  2,
		repro:     "go test ./internal/critic/ -run TestRunPerfScenarioDoesNotFail -v",
		rationale: "perf: Visible() at scale missed its latency threshold -- friction, not wrong, just costly.",
	},
	critic.DimensionErgonomics: {
		issueType: "task",
		priority:  2,
		repro:     "go test ./cmd/ -run TestRunErgonomicsScenarioPasses -v",
		rationale: "ergonomics: the CLI's observable behavior deviated from its documented contract -- friction, not wrong, just costly.",
	},
}

// criticBeadTaxonomyFallbackPriority is used for a dimension criticBeadTaxonomy
// doesn't recognize -- lower than every real dimension's priority, since an
// unrecognized dimension is a mapping gap, not a confirmed finding.
const criticBeadTaxonomyFallbackPriority = 3

// criticBeadTaxonomyFor looks up dim's taxonomy row. A dimension outside the
// 5 the loop currently rotates across (critic.Dimensions) is, from this
// mapping's own point of view, a capability it doesn't yet know how to file
// -- it degrades to "feature" at the lowest priority tier rather than
// guessing bug or task for a dimension it has never seen.
func criticBeadTaxonomyFor(dim critic.Dimension) criticBeadTaxonomyEntry {
	if e, ok := criticBeadTaxonomy[dim]; ok {
		return e
	}
	return criticBeadTaxonomyEntry{
		issueType: "feature",
		priority:  criticBeadTaxonomyFallbackPriority,
		repro: fmt.Sprintf(
			"go test ./... -run TestRun -v  # dimension %q has no row in criticBeadTaxonomy "+
				"-- locate its Run*Scenario under internal/critic or cmd", dim),
		rationale: fmt.Sprintf(
			"dimension %q is not one of the 5 crn-7oa section 10 recognizes -- filed as a "+
				"capability gap in this mapping itself, not a judgment on the scenario's own verdict.", dim),
	}
}

// FileCriticBead turns a fail or degraded critic.Result into exactly one bd
// bead, per crn-7oa section 10's feedback-bead taxonomy (crn-rqf's own step
// 4). A pass verdict is not a finding and files nothing. bd is this fleet's
// issue tracker, a different concern from cairn's own generic store model,
// so -- like sendReviewMail's gc integration -- this lives here in cmd, not
// internal/critic. The filed bead gets no assignee, parent, or custom
// metadata beyond the taxonomy's own labels: routing beyond that is the
// fleet's normal ready-to-build/needs-pm pipeline, not this function's
// concern. Returns the created bead's ID.
func FileCriticBead(ctx context.Context, r critic.Result) (string, error) {
	if r.Verdict == critic.Pass {
		return "", nil
	}
	args := criticBeadArgs(r)
	out, err := exec.CommandContext(ctx, "bd", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bd %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// criticBeadArgs builds the bd create argv for a fail or degraded
// critic.Result. Split out from FileCriticBead so the mapping itself --
// what the acceptance criteria actually grades -- is unit-testable without
// shelling out to bd. Every dynamic value is passed as its own "--flag
// value" pair (never a bare positional), so bd's own flag parser can never
// mistake one for a new flag regardless of its content.
func criticBeadArgs(r critic.Result) []string {
	e := criticBeadTaxonomyFor(r.Dimension)
	return []string{
		"create",
		"--title", fmt.Sprintf("critic-loop: %s %s (%s)", r.Dimension, r.Verdict, r.ScenarioID),
		"--type", e.issueType,
		"--priority", strconv.Itoa(e.priority),
		"--labels", "dim:" + string(r.Dimension) + ",source:cairn-critic-loop",
		"--description", fmt.Sprintf(
			"The cairn dogfood critic loop's %s scenario (%s) returned a %s verdict against a real store. "+
				"Filed automatically (crn-7oa section 10); see --acceptance for the repro, expected-vs-actual, and dimension rationale.",
			r.Dimension, r.ScenarioID, r.Verdict),
		"--acceptance", fmt.Sprintf(
			"Repro: %s\n\nExpected vs. actual: %s\n\nDimension rationale: %s",
			e.repro, r.Detail, e.rationale),
		"--silent",
	}
}
