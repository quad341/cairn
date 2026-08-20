package formulas

import (
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

type step struct {
	ID          string   `toml:"id"`
	Title       string   `toml:"title"`
	Needs       []string `toml:"needs"`
	Description string   `toml:"description"`
}

type varDef struct {
	Description string `toml:"description"`
	Default     string `toml:"default"`
}

type formula struct {
	Formula string            `toml:"formula"`
	Version int               `toml:"version"`
	Phase   string            `toml:"phase"`
	Vars    map[string]varDef `toml:"vars"`
	Steps   []step            `toml:"steps"`
}

func decodeFormula(t *testing.T, path string) formula {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var f formula
	if _, err := toml.Decode(string(data), &f); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return f
}

func stepByID(f formula, id string) (step, bool) {
	for _, s := range f.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return step{}, false
}

// order-triggered (pool) dispatch calls molecule.Instantiate with empty
// Options{}, so root-only-ness depends entirely on the compiled recipe's own
// phase="vapor" field (crn-gc9m.1 / crn-aa0y). Without it, gascity's
// poolOrderRouteVisibilityWarning fires and a scale-from-zero pool never
// wakes for the resulting wisp.
func TestCriticFormulaHasVaporPhase(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-critic.formula.toml")
	if f.Phase != "vapor" {
		t.Errorf("mol-cairn-critic.formula.toml: phase = %q, want \"vapor\"", f.Phase)
	}
}

func TestLibrarianFormulaHasVaporPhase(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian.formula.toml")
	if f.Phase != "vapor" {
		t.Errorf("mol-cairn-librarian.formula.toml: phase = %q, want \"vapor\"", f.Phase)
	}
}

// bd mol wisp has no --metadata flag, and a self-repour bypasses the order
// controller that would otherwise stamp gc.routed_to -- so the loop step
// must restamp it by hand on every generation, or generation-2+ silently
// goes unrouted and the scale-from-zero cairn/dogfood pool stops waking.
func TestCriticLoopStepSelfRepoursRootOnlyAndRestampsRouting(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-critic.formula.toml")
	loop, ok := stepByID(f, "loop")
	if !ok {
		t.Fatal(`mol-cairn-critic.formula.toml: no "loop" step found`)
	}

	if strings.Contains(loop.Description, "bd mol pour mol-cairn-critic") {
		t.Error(`loop step must not self-repour via "bd mol pour mol-cairn-critic" -- that sprays orphanable child-step beads every generation instead of a root-only wisp`)
	}
	if !strings.Contains(loop.Description, "bd mol wisp mol-cairn-critic --root-only") {
		t.Error(`loop step must self-repour via "bd mol wisp mol-cairn-critic --root-only" to stay root-only across generations`)
	}
	if !strings.Contains(loop.Description, "gc.routed_to=cairn/dogfood") {
		t.Error(`loop step must restamp gc.routed_to=cairn/dogfood on the newly-poured bead so generation-2+ doesn't silently go unrouted`)
	}
}

// Cooldown/condition order dispatch carries vars=nil (an order's [params]
// only validate presence, they cannot supply values), so mol-cairn-librarian
// falls back to its own defaults (tier="global") -- wrong owner for a
// rig-scoped cooldown order. This wrapper's var defaults must resolve the
// rig-tier sweep on its own, with no vars supplied (crn-6ef7 / crn-gc9m.2).
func TestLibrarianRigFormulaHasRigTierDefaults(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian-rig.formula.toml")
	if f.Phase != "vapor" {
		t.Errorf("mol-cairn-librarian-rig.formula.toml: phase = %q, want \"vapor\"", f.Phase)
	}
	if got := f.Vars["tier"].Default; got != "rig" {
		t.Errorf("mol-cairn-librarian-rig.formula.toml: vars.tier.default = %q, want \"rig\"", got)
	}
	if got := f.Vars["rig"].Default; got != "cairn" {
		t.Errorf("mol-cairn-librarian-rig.formula.toml: vars.rig.default = %q, want \"cairn\"", got)
	}
}

// bd's formula system has no alias/extends/include mechanism (confirmed via
// bd formula --help / bd mol --help), so this wrapper can only stay in sync
// with mol-cairn-librarian.formula.toml by being a full structural
// duplicate of its steps. Guard against the two formulas silently drifting.
func TestLibrarianRigFormulaHasSameStepsAsLibrarian(t *testing.T) {
	base := decodeFormula(t, "mol-cairn-librarian.formula.toml")
	rig := decodeFormula(t, "mol-cairn-librarian-rig.formula.toml")

	if len(rig.Steps) != len(base.Steps) {
		t.Fatalf("mol-cairn-librarian-rig.formula.toml: %d steps, want %d (same as mol-cairn-librarian.formula.toml)", len(rig.Steps), len(base.Steps))
	}
	for i, baseStep := range base.Steps {
		rigStep := rig.Steps[i]
		if rigStep.ID != baseStep.ID {
			t.Errorf("step %d: id = %q, want %q", i, rigStep.ID, baseStep.ID)
		}
		if rigStep.Description != baseStep.Description {
			t.Errorf("step %q: description differs from mol-cairn-librarian.formula.toml", baseStep.ID)
		}
	}
}

// promote-candidate-beads and cull-candidate-beads (crn-28ge.1.8) must follow
// the same "check bd for an already-tracked bead before creating a new one"
// idiom the other three sweep steps already use (crn-28ge.1.11) -- otherwise
// re-running the loop against an unchanged findings set files a duplicate
// bead every cycle instead of a no-op skip.
func TestLibrarianPromoteAndCullStepsSkipWhenAlreadyTracked(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian.formula.toml")

	for _, tc := range []struct {
		stepID string
		label  string
	}{
		{"promote-candidate-beads", "dim:promote,source:cairn-librarian"},
		{"cull-candidate-beads", "dim:cull,source:cairn-librarian"},
	} {
		s, ok := stepByID(f, tc.stepID)
		if !ok {
			t.Fatalf("mol-cairn-librarian.formula.toml: no %q step found", tc.stepID)
		}

		if !strings.Contains(s.Description, `ANCHOR="[entry:${ENTRY_ID}]"`) {
			t.Errorf("%s: must build a stable per-entry ANCHOR token, or the dedup check and the eventual bd create could target different findings", tc.stepID)
		}
		if !strings.Contains(s.Description, "bd list --label="+tc.label) {
			t.Errorf("%s: must check `bd list --label=%s --title-contains=$ANCHOR` for an already-tracked bead, or a second sweep pass over an unchanged finding files a duplicate", tc.stepID, tc.label)
		}
		if !strings.Contains(s.Description, `if [ -n "$EXISTING" ]; then`) {
			t.Errorf("%s: must skip once EXISTING is already set, or a second sweep pass over an unchanged finding files a duplicate", tc.stepID)
		}
	}
}

// crn-wqgm / crn-go8o: the needs-pm label and its Guard assertion are what
// make a filed bead visible to the deacon patrol at all -- this exact class
// of regression (prose claims "routes into the pipeline", the --labels
// argument doesn't) already happened once, and a second instance of it (the
// -rig wrapper's then-unpatched duplicate steps) slipped past self-test and
// was only caught in review. Assert both directly per step instead of
// relying solely on the rig/base parity test as an indirect proxy.
func TestLibrarianStepsHaveNeedsPmLabelAndGuard(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian.formula.toml")

	for _, stepID := range []string{
		"stale-review-branch-recovery",
		"freshness-drift-beads",
		"dedup-candidate-beads",
		"promote-candidate-beads",
		"cull-candidate-beads",
	} {
		s, ok := stepByID(f, stepID)
		if !ok {
			t.Fatalf("mol-cairn-librarian.formula.toml: no %q step found", stepID)
		}

		if !strings.Contains(s.Description, ",needs-pm") {
			t.Errorf("%s: --labels must include needs-pm, or the deacon patrol's auto-router never picks up the filed bead (crn-wqgm)", stepID)
		}
		if !strings.Contains(s.Description, `index("needs-pm")`) {
			t.Errorf("%s: missing the Guard block asserting the filed bead matches the deacon patrol's own selection query (needs-pm label present)", stepID)
		}
		if !strings.Contains(s.Description, `($b.assignee // "") == ""`) || !strings.Contains(s.Description, `($b.metadata["gc.routed_to"] // "") == ""`) {
			t.Errorf("%s: Guard block must also check assignee and gc.routed_to are empty, matching the deacon patrol's full selection query", stepID)
		}
	}
}

// crn-27con.4: dim:review-branch,source:cairn-librarian are provenance-only
// labels -- nothing in packs/ uses either as a routing inclusion filter (the
// only reference is an exclusion, in the labeller's own "unlabeled ready
// work" health check, which deliberately skips beads carrying
// source:cairn-librarian). needs-pm alone (crn-wqgm) already makes the filed
// bead deacon-patrol-routable, but needs-investigation additionally puts it
// directly in investigator's own pull-based ready query
// (packs/actual/investigator/pack.toml: --label-any
// needs-pr-response,needs-investigation), giving the filed bead two
// independent paths into a real work queue instead of relying on deacon
// dispatch alone. Scoped to stale-review-branch-recovery only, unlike
// TestLibrarianStepsHaveNeedsPmLabelAndGuard's five steps -- the other four
// are explicitly out of scope for this bead.
func TestLibrarianStaleReviewBranchRecoveryStepHasNeedsInvestigationLabelAndGuard(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian.formula.toml")
	s, ok := stepByID(f, "stale-review-branch-recovery")
	if !ok {
		t.Fatal(`mol-cairn-librarian.formula.toml: no "stale-review-branch-recovery" step found`)
	}

	if !strings.Contains(s.Description, ",needs-investigation") {
		t.Error(`stale-review-branch-recovery: --labels must include needs-investigation, or the filed bead never enters investigator's own "--label-any needs-pr-response,needs-investigation" ready query (crn-27con.4)`)
	}
	if !strings.Contains(s.Description, `index("needs-investigation")`) {
		t.Error(`stale-review-branch-recovery: missing a Guard assertion that needs-investigation is present, matching the label it now requires (crn-27con.4)`)
	}
	if !strings.Contains(s.Description, ",needs-pm") {
		t.Error(`stale-review-branch-recovery: must still include needs-pm -- crn-27con.4 adds needs-investigation alongside it, not in place of it`)
	}
}

// cairn cull-evict itself is not safely repeatable for the same entry --
// EvictToReviewBranch hard-errors once a proposal is already pending, since
// its review branch name is deterministic ("cull/" + entry ID; see
// internal/cairn.TestEvictToReviewBranchRefusesWhenProposalAlreadyPending).
// So cull-candidate-beads' idempotency is an ordering property, not just a
// presence one: the EXISTING dedup-check-and-skip must run BEFORE "cairn
// cull-evict" is invoked, or a second sweep pass over an unchanged finding
// hard-errors every cycle instead of quietly skipping.
func TestLibrarianCullStepChecksExistingBeforeCallingCullEvict(t *testing.T) {
	f := decodeFormula(t, "mol-cairn-librarian.formula.toml")
	s, ok := stepByID(f, "cull-candidate-beads")
	if !ok {
		t.Fatal(`mol-cairn-librarian.formula.toml: no "cull-candidate-beads" step found`)
	}

	existingIdx := strings.Index(s.Description, `if [ -n "$EXISTING" ]; then`)
	cullEvictIdx := strings.Index(s.Description, `cairn cull-evict "$ENTRY_ID"`)

	if existingIdx == -1 {
		t.Fatal(`cull-candidate-beads: no EXISTING dedup-check found`)
	}
	if cullEvictIdx == -1 {
		t.Fatal(`cull-candidate-beads: no "cairn cull-evict" call found`)
	}
	if existingIdx > cullEvictIdx {
		t.Error(`cull-candidate-beads: the EXISTING dedup-check-and-skip must run BEFORE "cairn cull-evict" is called, since a second call for an already-proposed entry hard-errors instead of no-op'ing`)
	}
}
