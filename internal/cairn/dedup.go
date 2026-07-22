package cairn

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DedupFinding is one duplicate/re-scope candidate a librarian sweep
// surfaces for human or agent judgment. Dedup never resolves a finding
// itself — filing, and merging or deleting an entry, are out of scope here
// by design (crn-xw3 guardrail 8: propose, never auto-merge or
// auto-delete).
type DedupFinding struct {
	Kind       string   `json:"kind"`                 // "topic_key" | "content"
	Tier       string   `json:"tier,omitempty"`       // global | rig | role; omitted for a cross-tier content pair
	TopicKey   string   `json:"topic_key,omitempty"`  // topic_key kind only
	EntryIDs   []string `json:"entry_ids"`            // sorted; len 2 for content, len >=2 for topic_key
	Similarity float64  `json:"similarity,omitempty"` // content kind only
	Detail     string   `json:"detail"`
}

// dedupSimilarityThreshold is the minimum Title+Summary word-Jaccard score
// (see similarity) for a pair to be reported as a content-similarity
// candidate — AC2's "concrete, mechanical threshold". See
// TestSimilarityThresholdExamples for the required written pass/fail
// example pair.
const dedupSimilarityThreshold = 0.5

// Dedup scans every shared-tier entry (global/, rig/*/, role/*/ — agent/
// private entries are out of the librarian's remit, matching Sweep) for two
// kinds of near-duplicate candidate:
//
//   - "topic_key": two or more entries in the same tier share an exact,
//     non-empty topic_key (AC1). One finding per tier per key, covering the
//     whole group. This never compares across tiers: a topic_key shared
//     between, say, a rig-tier entry and a more-specific role-tier entry is
//     CSS-style shadowing (DESIGN.md §3, entry.go's shadow/ShadowMap) —
//     intentional precedence, not a duplicate.
//   - "content": a pair of shared-tier entries — any tier, including a
//     cross-tier pair — whose Title+Summary word-Jaccard similarity meets
//     dedupSimilarityThreshold (AC2). A pair that shares a non-empty
//     topic_key is skipped here only when one's Scope is a genuine
//     (non-strict) superset of the other's — the same scopeSuperset
//     condition ShadowMap uses to qualify legitimate shadowing (entry.go).
//     For that case shadow()/ShadowMap already give the relationship a
//     well-defined meaning, so a content score on top of it would just
//     repeat intended override behavior as a dup. A shared topic_key whose
//     scopes are incomparable is, by ShadowMap's own rule, NOT legitimate
//     shadowing (see TestShadowMapIncomparableScopesNeverShadow) — that
//     pair is left to the ordinary content-similarity check rather than
//     being silently excluded, so an accidental cross-tier key collision
//     with incomparable scopes can still surface as a candidate.
//
// Dedup is strictly read-only, the same guarantee Sweep makes for freshness:
// no code path here calls WriteBack or any other store-mutating operation,
// so this is safe to run on any cadence and re-observe the same candidate
// every cycle without side effects (AC4). Filing a bd bead from a finding —
// including the pair/group-anchored idempotency check that keeps a repeated
// sweep from re-filing an already-open candidate (AC3) — is a formula-level
// concern, deferred to crn-0yv.5 and documented in
// docs/plans/cairn-librarian-dedup-detection-beads.md, the same split
// crn-0yv.2 established for freshness-drift beads.
func Dedup(store string) ([]DedupFinding, error) {
	all, err := IterEntries(store)
	if err != nil {
		return nil, err
	}

	tierOf := make(map[string]string, len(all))
	var shared []*Entry
	for _, e := range all {
		tier := entryTier(store, e)
		if tier == "" || tier == "agent" {
			continue
		}
		tierOf[e.ID] = tier
		shared = append(shared, e)
	}

	byTier := make(map[string][]*Entry)
	for _, e := range shared {
		byTier[tierOf[e.ID]] = append(byTier[tierOf[e.ID]], e)
	}

	var out []DedupFinding
	for _, tier := range []string{"global", "rig", "role"} {
		out = append(out, topicKeyCollisions(tier, byTier[tier])...)
	}
	out = append(out, contentSimilarityPairs(shared, tierOf)...)
	return out, nil
}

// topicKeyCollisions groups a single tier's entries by exact, non-empty
// topic_key and reports one finding per key held by more than one entry.
//
// Design decision: this does not further exclude a same-tier pair whose
// Scope differs (e.g. two rig-tier entries under different rig tags) via a
// ShadowMap-style superset check. AC1 calls for an unconditional filed bead
// on a same-tier collision, and a same-tier pair with incomparable scopes
// is exactly the case entry.go's own ShadowMap docs call out as ambiguous
// under shadow()'s cruder tag-count proxy (some identity holding both tags
// would hit an arbitrary tie-break) — worth a human/agent glance even when
// it turns out to be two unrelated entries that happened to pick the same
// key. Since Dedup only ever proposes (guardrail 8), a false positive here
// costs a few seconds of judgment on an already-open bead, not a bad merge.
func topicKeyCollisions(tier string, entries []*Entry) []DedupFinding {
	byKey := make(map[string][]*Entry)
	for _, e := range entries {
		if e.TopicKey == "" {
			continue
		}
		byKey[e.TopicKey] = append(byKey[e.TopicKey], e)
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []DedupFinding
	for _, key := range keys {
		group := byKey[key]
		if len(group) < 2 {
			continue
		}
		ids := make([]string, len(group))
		for i, e := range group {
			ids[i] = e.ID
		}
		sort.Strings(ids)
		out = append(out, DedupFinding{
			Kind:     "topic_key",
			Tier:     tier,
			TopicKey: key,
			EntryIDs: ids,
			Detail:   fmt.Sprintf("%d entries in tier %q share topic_key %q", len(ids), tier, key),
		})
	}
	return out
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// tokens lowercases s and splits it into a set of [a-z0-9]+ runs. There is
// no stemming and no synonym matching — "enable" and "enabling" are
// distinct tokens — so similarity is deliberately a blunt, mechanical
// measure, not a semantic one.
func tokens(s string) map[string]bool {
	out := make(map[string]bool)
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		out[w] = true
	}
	return out
}

// similarity is the Jaccard index between a's and b's Title+Summary token
// sets: |tokens(a) ∩ tokens(b)| / |tokens(a) ∪ tokens(b)|. An entry with no
// tokens at all (empty Title and Summary) is defined to have zero
// similarity to anything, rather than dividing by zero — an empty entry is
// not evidence that it duplicates another.
func similarity(a, b *Entry) float64 {
	ta := tokens(a.Title + " " + a.Summary)
	tb := tokens(b.Title + " " + b.Summary)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for w := range ta {
		if tb[w] {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	return float64(inter) / float64(union)
}

// contentSimilarityPairs reports every pair of shared-tier entries — any
// tier, including cross-tier — whose Title+Summary Jaccard similarity meets
// dedupSimilarityThreshold, skipping a pair that shares a non-empty
// topic_key only when it also qualifies as legitimate ShadowMap-style
// shadowing (see Dedup's doc comment for why).
func contentSimilarityPairs(entries []*Entry, tierOf map[string]string) []DedupFinding {
	var out []DedupFinding
	for i := range entries {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a.TopicKey != "" && a.TopicKey == b.TopicKey &&
				(scopeSuperset(a.Scope, b.Scope) || scopeSuperset(b.Scope, a.Scope)) {
				continue
			}
			score := similarity(a, b)
			if score < dedupSimilarityThreshold {
				continue
			}
			lo, hi := a.ID, b.ID
			if lo > hi {
				lo, hi = hi, lo
			}
			tier := ""
			if tierOf[a.ID] == tierOf[b.ID] {
				tier = tierOf[a.ID]
			}
			detail := fmt.Sprintf(
				"title+summary Jaccard similarity %.2f (tiers: %s, %s)",
				score, tierOf[a.ID], tierOf[b.ID],
			)
			if a.TopicKey != "" && a.TopicKey == b.TopicKey {
				detail += fmt.Sprintf(
					"; also share topic_key %q with incomparable scopes (not ShadowMap-legitimate shadowing)",
					a.TopicKey,
				)
			}
			out = append(out, DedupFinding{
				Kind:       "content",
				Tier:       tier,
				EntryIDs:   []string{lo, hi},
				Similarity: score,
				Detail:     detail,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EntryIDs[0] != out[j].EntryIDs[0] {
			return out[i].EntryIDs[0] < out[j].EntryIDs[0]
		}
		return out[i].EntryIDs[1] < out[j].EntryIDs[1]
	})
	return out
}
