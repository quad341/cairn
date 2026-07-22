package critic

import (
	"context"
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
)

const scopePrecedenceScenarioID = "scope-precedence-shadow-and-shadowmap"

// RunScopePrecedenceScenario exercises the store's two distinct precedence
// algorithms — entry.go's own doc comments are explicit that these are
// deliberately different, not redundant: shadow() picks a single winner for
// one identity by scope-tag count (used by Visible()), while ShadowMap()
// reports store-wide, identity-free shadow relationships using an
// exact-superset test (used by `cairn status`), because a tag-count-only
// proxy is unsound store-wide when two entries have an equal or
// incomparable tag count with neither a superset of the other.
func RunScopePrecedenceScenario(ctx context.Context, store string) Result {
	n, err := nonce()
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("nonce: %v", err))
	}
	if r := checkShadowWinsBySpecificity(ctx, store, n); r.Verdict != Pass {
		return r
	}
	if r := checkShadowTiebreak(ctx, store, n); r.Verdict != Pass {
		return r
	}
	return checkShadowMapSupersetSemantics(ctx, store, n)
}

// checkShadowWinsBySpecificity seeds 3 entries sharing one topic key at
// increasing scope-tag count and asserts Visible() returns exactly the
// most specific one.
func checkShadowWinsBySpecificity(ctx context.Context, store, n string) Result {
	topic := "critic-scope-" + n
	rig := "rig:critic-" + n

	global, err := cairn.NewEntry(topic, nil, "global body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build global entry: %v", err))
	}
	rigOnly, err := cairn.NewEntry(topic, []string{rig}, "rig body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build rig entry: %v", err))
	}
	rigAndRole, err := cairn.NewEntry(topic, []string{rig, "role:builder"}, "rig+role body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build rig+role entry: %v", err))
	}

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{global, rigOnly, rigAndRole})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	visible, err := cairn.Visible(ctx, store, []string{rig, "role:builder"})
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("Visible: %v", err))
	}

	matches := matchingTopic(visible, topic)
	if len(matches) != 1 || matches[0] != rigAndRole.ID {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail,
			fmt.Sprintf("expected exactly [%s] for topic %q, got %v", rigAndRole.ID, topic, matches))
	}
	return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Pass, "most-specific entry won shadow resolution")
}

// checkShadowTiebreak seeds 2 entries sharing one topic key with equal
// scope-tag count (so specificity alone can't decide) but different
// VerifiedAt, and asserts Visible() returns the later-verified one — the
// first link in shadow()'s documented tiebreak chain.
func checkShadowTiebreak(ctx context.Context, store, n string) Result {
	topic := "critic-scope-tiebreak-" + n
	rig := "rig:critic-" + n

	older, err := cairn.NewEntry(topic, []string{rig}, "older body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build older entry: %v", err))
	}
	older.VerifiedAt = "2020-01-01"
	newer, err := cairn.NewEntry(topic, []string{rig}, "newer body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build newer entry: %v", err))
	}
	newer.VerifiedAt = "2026-01-01"

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{older, newer})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	visible, err := cairn.Visible(ctx, store, []string{rig})
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("Visible: %v", err))
	}

	matches := matchingTopic(visible, topic)
	if len(matches) != 1 || matches[0] != newer.ID {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail,
			fmt.Sprintf("equal-specificity tie: expected the later-VerifiedAt entry [%s] to win, got %v", newer.ID, matches))
	}
	return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Pass, "equal-specificity tie correctly broken by VerifiedAt")
}

// checkShadowMapSupersetSemantics seeds an incomparable-scope pair (neither
// side a superset of the other, despite an equal 1-tag count) and a
// genuine-superset pair sharing separate topic keys, and asserts
// ShadowMap() never shadows the incomparable pair but does shadow the
// superset pair — entry.go's own documented edge case for why ShadowMap()
// cannot reuse shadow()'s tag-count proxy store-wide.
func checkShadowMapSupersetSemantics(ctx context.Context, store, n string) Result {
	incTopic := "critic-scope-inc-" + n
	a, err := cairn.NewEntry(incTopic, []string{"rig:critic-" + n}, "a body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build incomparable a: %v", err))
	}
	b, err := cairn.NewEntry(incTopic, []string{"role:builder"}, "b body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build incomparable b: %v", err))
	}

	supTopic := "critic-scope-sup-" + n
	rig := "rig:critic-" + n
	rigOnly, err := cairn.NewEntry(supTopic, []string{rig}, "rig body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build superset rig-only: %v", err))
	}
	rigRole, err := cairn.NewEntry(supTopic, []string{rig, "role:builder"}, "rig+role body", "critic")
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("build superset rig+role: %v", err))
	}

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{a, b, rigOnly, rigRole})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("seed fixtures: %v", err))
	}

	all, err := cairn.IterEntries(store)
	if err != nil {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail, fmt.Sprintf("IterEntries: %v", err))
	}
	sm := cairn.ShadowMap(all)

	if _, shadowed := sm[a.ID]; shadowed {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail,
			fmt.Sprintf("%s incorrectly reported as shadowed by an incomparable-scope sibling", a.ID))
	}
	if _, shadowed := sm[b.ID]; shadowed {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail,
			fmt.Sprintf("%s incorrectly reported as shadowed by an incomparable-scope sibling", b.ID))
	}
	shadower, shadowed := sm[rigOnly.ID]
	if !shadowed || shadower.ID != rigRole.ID {
		return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Fail,
			fmt.Sprintf("expected %s shadowed by %s, got shadowed=%v shadower=%v", rigOnly.ID, rigRole.ID, shadowed, shadower))
	}
	return NewResult(DimensionScopePrecedence, scopePrecedenceScenarioID, Pass,
		"ShadowMap correctly ignored the incomparable pair and reported the genuine superset shadow")
}

// matchingTopic returns the IDs of entries in visible whose topic key is
// topic.
func matchingTopic(visible []*cairn.Entry, topic string) []string {
	var matches []string
	for _, e := range visible {
		if e.TopicKey == topic {
			matches = append(matches, e.ID)
		}
	}
	return matches
}
