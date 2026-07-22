package critic

import (
	"fmt"
	"sort"

	"github.com/quad341/cairn/internal/cairn"
)

const recallScenarioID = "recall-subset-match"

// RunRecallScenario exercises Visible()'s subset-match recall: every scope
// tag on an entry must be satisfied by the identity (an AND, not an OR, of
// tags), and a global (untagged) entry is visible to everyone. It seeds 4
// known entries under distinct topic keys (so shadowing, dimension 2's
// concern, can't interfere) spanning global / rig-only / rig+role / a
// different rig, queries Visible() for a single rig-only identity, and
// diffs the actual visible-ID set against an independently computed
// expected set — catching both a false negative (recall miss) and a false
// positive (a scope leak) in one pass.
func RunRecallScenario(store string) Result {
	n, err := nonce()
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("nonce: %v", err))
	}
	rig := "rig:critic-" + n

	global, err := cairn.NewEntry("critic-recall-"+n+"-global", nil, "global body", "critic")
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("build global entry: %v", err))
	}
	rigOnly, err := cairn.NewEntry("critic-recall-"+n+"-rig-only", []string{rig}, "rig-only body", "critic")
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("build rig-only entry: %v", err))
	}
	rigAndRole, err := cairn.NewEntry("critic-recall-"+n+"-rig-and-role", []string{rig, "role:builder"}, "rig+role body", "critic")
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("build rig+role entry: %v", err))
	}
	otherRig, err := cairn.NewEntry("critic-recall-"+n+"-other-rig", []string{"rig:other-" + n}, "other-rig body", "critic")
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("build other-rig entry: %v", err))
	}

	cleanup, err := seedEntries(store, []*cairn.Entry{global, rigOnly, rigAndRole, otherRig})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	visible, err := cairn.Visible(store, []string{rig})
	if err != nil {
		return NewResult(DimensionRecall, recallScenarioID, Fail, fmt.Sprintf("Visible: %v", err))
	}

	got := make(map[string]bool, len(visible))
	for _, e := range visible {
		got[e.ID] = true
	}
	// rig:critic-<n> alone satisfies global (no tags) and rig-only (subset
	// match), but not rig+role (missing role:builder — AND of tags) or
	// other-rig (a different rig tag entirely).
	want := map[string]bool{global.ID: true, rigOnly.ID: true}
	notWant := map[string]bool{rigAndRole.ID: true, otherRig.ID: true}

	missing, leaked := diffRecall(got, want, notWant)
	if len(missing) == 0 && len(leaked) == 0 {
		return NewResult(DimensionRecall, recallScenarioID, Pass, "all expected entries recalled, no leaks")
	}
	return NewResult(DimensionRecall, recallScenarioID, Fail,
		fmt.Sprintf("missing (false negative): %v; leaked (false positive): %v", missing, leaked))
}

// diffRecall reports which expected-visible IDs are missing (false
// negatives) and which expected-not-visible IDs leaked through (false
// positives). It is a pure function, isolated from Visible() itself, so the
// scenario's judge logic can be unit-tested directly against synthetic
// sets.
func diffRecall(got, want, notWant map[string]bool) (missing, leaked []string) {
	for id := range want {
		if !got[id] {
			missing = append(missing, id)
		}
	}
	for id := range notWant {
		if got[id] {
			leaked = append(leaked, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(leaked)
	return missing, leaked
}
