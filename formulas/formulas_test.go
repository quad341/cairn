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
