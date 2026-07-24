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

// pairSignals computes Dedup's two duplicate/conflict signals for a single
// pair, independent of tier: sameTopicKey is an exact, non-empty topic_key
// match; similarityScore is the Title+Summary Jaccard score (see
// similarity); shadowExempt is true when the pair qualifies as legitimate
// ShadowMap-style shadowing (shares a topic_key and one's Scope is a
// genuine, non-strict superset of the other's) — entry.go's own condition
// for "this is intentional precedence, not a duplicate"; contentMatch is
// true iff similarityScore meets dedupSimilarityThreshold and the pair is
// not shadowExempt.
//
// This is the one place both signals are computed (NFR-05): Dedup's
// whole-store scan calls it per-pair for the content signal
// (contentSimilarityPairs); Conflicts, the single-candidate-callable entry
// point `cairn get` uses (and crn-28ge.1.4's capture-time check will reuse),
// calls it for both. topicKeyCollisions' own same-tier grouping is the same
// sameTopicKey equality relationship applied across a whole tier at once
// (an O(n) group-by, not a pairwise scan) — restructuring it into a
// pairwise loop just to call this function would regress it to O(n²) for no
// behavioral gain, so it is left as is.
func pairSignals(a, b *Entry) (sameTopicKey bool, similarityScore float64, shadowExempt bool, contentMatch bool) {
	sameTopicKey = a.TopicKey != "" && a.TopicKey == b.TopicKey
	similarityScore = similarity(a, b)
	shadowExempt = sameTopicKey && (scopeSuperset(a.Scope, b.Scope) || scopeSuperset(b.Scope, a.Scope))
	contentMatch = !shadowExempt && similarityScore >= dedupSimilarityThreshold
	return sameTopicKey, similarityScore, shadowExempt, contentMatch
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
			sameKey, score, _, contentMatch := pairSignals(a, b)
			if !contentMatch {
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
			if sameKey {
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

// Conflicts reports every Dedup-style finding between candidate and each
// entry in others — the single-candidate-callable primitive FR-03 needs so
// `cairn get` can surface an entry's conflicts against other visible
// entries, and crn-28ge.1.4's capture-time check will reuse (NFR-05: built
// on pairSignals, the same shared signal computation Dedup's whole-store
// scan uses, not a second implementation).
//
// Unlike Dedup's own topic_key signal (topicKeyCollisions, which only ever
// compares within a single tier — a cross-tier topic_key match is assumed
// intentional shadowing), Conflicts has no tier partitioning to fall back
// on: it works over a flat "other visible entries" list, not a per-tier
// scan. So it implements the architecture doc's literal recall-time
// contract — "match OR similarity >= threshold" — as two independent
// signals, each still exempted when the pair is legitimate ShadowMap-style
// shadowing (shadowExempt from pairSignals). A candidate can therefore end
// up with both a topic_key and a content finding against the very same
// other entry (see TestConflictsBothSignalsCanFireForSamePair). This is a
// lexical proxy (topic_key/word-similarity), the same one Dedup itself
// uses, not semantic contradiction detection.
//
// others is typically Visible()'s result for some identity; a candidate
// that happens to also appear there (by ID — Visible() has no reason to
// exclude the very entry being looked up) is skipped rather than reported
// as conflicting with itself.
func Conflicts(candidate *Entry, others []*Entry) []DedupFinding {
	var out []DedupFinding
	for _, other := range others {
		if other.ID == candidate.ID {
			continue
		}
		sameKey, score, shadowExempt, contentMatch := pairSignals(candidate, other)
		if shadowExempt || (!sameKey && !contentMatch) {
			continue
		}
		lo, hi := candidate.ID, other.ID
		if lo > hi {
			lo, hi = hi, lo
		}
		if sameKey {
			out = append(out, DedupFinding{
				Kind:     "topic_key",
				TopicKey: candidate.TopicKey,
				EntryIDs: []string{lo, hi},
				Detail:   fmt.Sprintf("entries %s and %s share topic_key %q", lo, hi, candidate.TopicKey),
			})
		}
		if contentMatch {
			detail := fmt.Sprintf("title+summary Jaccard similarity %.2f", score)
			if sameKey {
				detail += fmt.Sprintf(
					"; also share topic_key %q with incomparable scopes (not ShadowMap-legitimate shadowing)",
					candidate.TopicKey,
				)
			}
			out = append(out, DedupFinding{
				Kind:       "content",
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
		if out[i].EntryIDs[1] != out[j].EntryIDs[1] {
			return out[i].EntryIDs[1] < out[j].EntryIDs[1]
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
