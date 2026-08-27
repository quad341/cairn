package cairn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testBudget is a generously large byte budget for tests that don't care
// about truncation -- large enough that every fixture entry in this file
// itemizes.
const testBudget = 1 << 20

func TestPrime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md",
		"+++\nid = \"r\"\ntitle = \"r\"\ntopic_key = \"alpha/thing\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	result, err := Prime(t.Context(), dir, []string{"rig:alpha"}, testBudget)
	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalVisible, "the alpha-scoped agent should see both the global and alpha entries")
	var sawAlpha bool
	for _, it := range result.Items {
		if it.TopicKey == "alpha/thing" {
			sawAlpha = true
		}
	}
	assert.True(t, sawAlpha, "an alpha-scoped agent should see the alpha topic")

	out := RenderPrimeText(result)
	assert.Contains(t, out, "cairn remember", "prime should still nudge agents to capture what they learn")

	bare, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	for _, it := range bare.Items {
		assert.NotEqual(t, "alpha/thing", it.TopicKey, "a bare identity should not see the alpha topic")
	}
}

func TestPrimeEmpty(t *testing.T) {
	dir := t.TempDir()
	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalVisible)
	assert.Empty(t, result.Items)

	out := RenderPrimeText(result)
	assert.Contains(t, out, "No cached knowledge")
}

// TestPrimeDoesNotClaimRememberMissing covers crn-6az.2: prime's footer used
// to hardcode "no `remember` command yet" and tell agents to hand-author
// entries directly, which went stale the moment the remember command
// shipped (it now writes entries itself, including committing/routing them
// for review) -- leaving prime denying a command that cairn --help lists.
func TestPrimeDoesNotClaimRememberMissing(t *testing.T) {
	dir := t.TempDir()
	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	out := RenderPrimeText(result)
	assert.NotContains(t, out, "no `remember` command yet")
	assert.NotContains(t, out, "hand-author")
	assert.Contains(t, out, "cairn remember")
}

// TestPrimeTeachesAnchoredRemember is crn-5wus. The footer is the only cairn
// text every agent reads every session, and it taught the unanchored form as
// THE way to write -- so that is the form agents wrote. Measured on the
// mayor's store, entries created after the flat-file migration (which had no
// anchor concept to inherit) were still 84% unanchored. Anchored freshness is
// what cairn has over flat files; the footer has to name it, or it stays off.
func TestPrimeTeachesAnchoredRemember(t *testing.T) {
	dir := t.TempDir()
	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	out := RenderPrimeText(result)

	assert.Contains(t, out, "--anchor-repo", "the footer must name the flag that makes freshness real")
	assert.Contains(t, out, "--anchor-path")
	assert.Contains(t, out, "cairn remember", "and must still teach the write verb itself")
}

// TestPrimeExplainsWhyAnchoringMatters pins the reason, not just the flags.
// The same output already warns that entries go stale; stating the hazard
// without the remedy is what produced the unanchored store in the first
// place, so the two must travel together.
func TestPrimeExplainsWhyAnchoringMatters(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")
	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	out := RenderPrimeText(result)

	assert.Contains(t, out, "stale", "the staleness warning is what motivates anchoring")
	assert.Regexp(t, `(?is)anchor.*freshness|freshness.*anchor`, out,
		"the footer must connect anchoring to freshness, not just list flags")
}

// TestPrimeWarnsOnUnmatchedScopeDimension is crn-ln1 acceptance criterion 1
// and 3 (populated-but-unmatched case): the store has role-scoped entries,
// the identity carries a role: tag, but no entry's scope matches it -- this
// is the silent-miss shape the diagnostic exists to catch, including when it
// drives the visible count to zero (the "No cached knowledge" branch).
func TestPrimeWarnsOnUnmatchedScopeDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/o.md",
		"+++\nid = \"o\"\ntitle = \"o\"\ntopic_key = \"o/thing\"\nscope = [\"role:investigator\"]\n+++\nx\n")

	result, err := Prime(t.Context(), dir, []string{"role:builder"}, testBudget)
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalVisible, "precondition: the mismatch should leave nothing visible")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "role:", "warning should name the mismatched dimension")
	assert.Contains(t, result.Warnings[0], "tag-shape mismatch")

	out := RenderPrimeText(result)
	assert.Contains(t, out, "No cached knowledge")
	assert.Contains(t, out, "tag-shape mismatch")
}

// TestPrimeNoWarningOnScopeMatch is crn-ln1 acceptance criterion 3 (working
// as intended case): a genuine, non-empty match must never warn.
func TestPrimeNoWarningOnScopeMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/o.md",
		"+++\nid = \"o\"\ntitle = \"o\"\ntopic_key = \"o/thing\"\nscope = [\"role:investigator\"]\n+++\nx\n")

	result, err := Prime(t.Context(), dir, []string{"role:investigator"}, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 1, "precondition: the entry should actually be visible")
	assert.Equal(t, "o/thing", result.Items[0].TopicKey)
	assert.Empty(t, result.Warnings)

	out := RenderPrimeText(result)
	assert.NotContains(t, out, "tag-shape mismatch")
}

// TestPrimeNoWarningOnEmptyScopeDimension is crn-ln1 acceptance criterion 3
// (nothing-to-warn-about case): the identity carries a role: tag, but the
// store has zero role-scoped entries anywhere -- there is nothing to have
// silently missed, so this must stay quiet too.
func TestPrimeNoWarningOnEmptyScopeDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	result, err := Prime(t.Context(), dir, []string{"role:investigator"}, testBudget)
	require.NoError(t, err)
	assert.Empty(t, result.Warnings)

	out := RenderPrimeText(result)
	assert.NotContains(t, out, "tag-shape mismatch")
}

// TestPrimeOrdersByHitCountThenCreatedAtThenID pins crn-0vqk.2's
// deterministic truncation order: hit_count desc, then created_at desc, then
// id asc -- not alphabetical (the prior topic-map baseline) and not
// staleness-first (rejected by the design: sorting by freshness would need
// freshness computed for every candidate before truncation could even start,
// defeating the point of bounding cost).
func TestPrimeOrdersByHitCountThenCreatedAtThenID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"a\"\nhit_count = 1\ncreated_at = \"2026-01-01T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/b.md",
		"+++\nid = \"b\"\ntitle = \"b\"\nhit_count = 5\ncreated_at = \"2026-01-01T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/c.md",
		"+++\nid = \"c\"\ntitle = \"c\"\nhit_count = 5\ncreated_at = \"2026-02-01T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/d.md",
		"+++\nid = \"d\"\ntitle = \"d\"\nhit_count = 5\ncreated_at = \"2026-02-01T00:00:00Z\"\nscope = []\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 4)

	ids := make([]string, len(result.Items))
	for i, it := range result.Items {
		ids[i] = it.ID
	}
	// c and d tie on hit_count and created_at, broken by id asc; both outrank
	// b (same hit_count, older), which outranks a (fewer hits).
	assert.Equal(t, []string{"c", "d", "b", "a"}, ids)
}

// TestPrimeTruncatesByByteBudgetWithoutAffectingAggregateCounts is the
// design's central guardrail: fresh_count/stale_count/unknown_count/
// truncated_count must always describe the entire visible set, independent
// of what the byte budget let into items[] -- otherwise a caller could
// misread a budget-truncated payload as "everything's fine".
func TestPrimeTruncatesByByteBudgetWithoutAffectingAggregateCounts(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"a", "b", "c"} {
		writeFile(t, dir, "global/"+id+".md",
			"+++\nid = \""+id+"\"\ntitle = \""+id+"\"\nscope = []\n+++\nx\n")
	}

	full, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, full.Items, 3, "precondition: a generous budget itemizes everything visible")

	tiny, err := Prime(t.Context(), dir, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, tiny.TotalVisible, "total_visible must reflect the whole visible set, not just what got itemized")
	require.Less(t, len(tiny.Items), tiny.TotalVisible, "precondition: the tiny budget must actually truncate something")
	assert.Equal(t, tiny.TotalVisible-len(tiny.Items), tiny.TruncatedCount)
	assert.Equal(t,
		tiny.TotalVisible,
		tiny.FreshCount+tiny.StaleCount+tiny.UnknownCount,
		"aggregate freshness counts must cover the entire visible set regardless of truncation",
	)
}

// TestPrimeTruncationStopsAtFirstOverBudgetItem pins the Finding-1 fix from
// crn-0vqk.2's review: once a higher-priority entry doesn't fit in the
// remaining budget, Prime must stop itemizing entirely rather than skipping
// ahead to try a later, lower-priority entry that happens to be cheaper.
// TestPrimeTruncatesByByteBudgetWithoutAffectingAggregateCounts can't catch
// this because its fixtures are same-cost, where "skip the expensive one and
// keep going" and "stop entirely" produce identical results -- this test
// uses fixtures with deliberately varying per-entry byte cost across the
// priority order (mirroring the reviewer's own repro: a high-priority entry
// too big to fit, followed by a lower-priority entry small enough that it
// would slip in if truncation merely skipped instead of stopping).
func TestPrimeTruncationStopsAtFirstOverBudgetItem(t *testing.T) {
	dir := t.TempDir()
	// x: highest priority (hit_count=10), large title -> expensive.
	// y: middle priority (hit_count=5), medium title.
	// z: lowest priority (hit_count=1), tiny title -> cheap.
	writeFile(t, dir, "global/x.md",
		"+++\nid = \"x\"\ntitle = \""+strings.Repeat("x", 200)+"\"\nhit_count = 10\nscope = []\n+++\nbody\n")
	writeFile(t, dir, "global/y.md",
		"+++\nid = \"y\"\ntitle = \""+strings.Repeat("y", 60)+"\"\nhit_count = 5\nscope = []\n+++\nbody\n")
	writeFile(t, dir, "global/z.md",
		"+++\nid = \"z\"\ntitle = \"z\"\nhit_count = 1\nscope = []\n+++\nbody\n")

	full, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, full.Items, 3, "precondition: a generous budget itemizes everything visible")

	byID := map[string]PrimeItem{}
	for _, it := range full.Items {
		byID[it.ID] = it
	}
	xCost := itemByteCost(byID["x"])
	yCost := itemByteCost(byID["y"])
	zCost := itemByteCost(byID["z"])
	require.Less(t, zCost, yCost, "precondition: z must be cheaper than y for this repro to distinguish continue from break")
	require.Less(t, yCost, xCost, "precondition: y must be cheaper than x for this repro to distinguish continue from break")

	// Budget fits x+z together but not x+y -- so under the old `continue`
	// behavior, y gets skipped (too expensive) but z (cheap enough) slips in
	// right after it, producing {x, z}. Under the fixed `break` behavior,
	// truncation must stop the moment y doesn't fit, producing {x} alone
	// even though z alone would have fit in the leftover budget.
	budget := xCost + zCost
	require.Less(t, budget, xCost+yCost, "sanity: budget must fit x+z together but not x+y")

	result, err := Prime(t.Context(), dir, nil, budget)
	require.NoError(t, err)
	ids := make([]string, len(result.Items))
	for i, it := range result.Items {
		ids[i] = it.ID
	}
	assert.Equal(t, []string{"x"}, ids, "truncation must stop at the first over-budget item, not let a later cheaper item slip in")
	assert.Equal(t, 3, result.TotalVisible)
	assert.Equal(t, 2, result.TruncatedCount)
	assert.Equal(t,
		result.TotalVisible,
		result.FreshCount+result.StaleCount+result.UnknownCount,
		"aggregate freshness counts must still cover the entire visible set even once truncation has latched",
	)
}

// TestPrimeNeverTruncatesToZeroItemsWhenEntriesAreVisible guards the
// degenerate case: a budget too small for even one entry must still itemize
// one rather than leaving a caller with zero items despite nonzero
// total_visible, which would be actively misleading rather than merely
// terse.
func TestPrimeNeverTruncatesToZeroItemsWhenEntriesAreVisible(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	result, err := Prime(t.Context(), dir, nil, 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.TotalVisible)
	assert.Len(t, result.Items, 1, "a budget too small for even one entry should still itemize one, not zero")
}

// TestPrimeCapsFreshnessChecksAndFailsTowardUnknown pins FR-5 and the
// design's freshness-cap guardrail: a bounded number of git shell-outs per
// Prime call, and entries past the cap classified unknown rather than
// fabricated fresh. Entry b points at a repo path that was never
// git-initialized; if the cap failed to gate it, Check would still handle
// that gracefully and return Unknown anyway, so a bare status assertion
// wouldn't distinguish a working cap from a broken one -- the capped detail
// message is what actually proves the real check never ran.
func TestPrimeCapsFreshnessChecksAndFailsTowardUnknown(t *testing.T) {
	orig := maxFreshnessChecksPerPrime
	maxFreshnessChecksPerPrime = 1
	defer func() { maxFreshnessChecksPerPrime = orig }()

	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")
	fp, err := ComputeFingerprint(t.Context(), Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NoError(t, err)
	require.NotEmpty(t, fp)

	writeFile(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"a\"\nhit_count = 5\nscope = []\n\n"+
		"[anchor]\ntype = \"files\"\nrepo = \""+repo+"\"\npaths = [\"a.go\"]\nfingerprint = \""+fp+"\"\n+++\nx\n")
	writeFile(t, dir, "global/b.md", "+++\nid = \"b\"\ntitle = \"b\"\nhit_count = 1\nscope = []\n\n"+
		"[anchor]\ntype = \"files\"\nrepo = \"/does/not/exist\"\npaths = [\"a.go\"]\nfingerprint = \"deadbeef\"\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	assert.True(t, result.ChecksCapped)

	byID := map[string]PrimeItem{}
	for _, it := range result.Items {
		byID[it.ID] = it
	}
	assert.Equal(t, Fresh, byID["a"].Freshness.Status, "the higher hit_count entry should win the one available check")
	assert.Equal(t, Unknown, byID["b"].Freshness.Status)
	assert.Contains(t, byID["b"].Freshness.Detail, "not checked this pass",
		"past the cap, classification must short-circuit rather than actually invoking Check on a bogus repo")
}

// TestPrimeIndexIncludesPerTopicKeyBreakdown covers crn-3476 FR-1/FR-2: the
// index view must include a per-topic_key breakdown with counts, restoring
// DESIGN.md §5's "topic tree with counts." The breakdown is computed over
// the same post-shadow visible set as TotalVisible (NFR-2), so its counts
// must always sum to TotalVisible: a1/a2 share topic-a but have no meaningful
// precedence signal, so both remain visible as an explicit conflict. An
// untopiced entry is never shadow-collapsed (shadowReason skips
// TopicKey == ""), so it still shows up as a count under UntopicedLabel
// rather than vanishing.
func TestPrimeIndexIncludesPerTopicKeyBreakdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a1.md", "+++\nid = \"a1\"\ntitle = \"a1\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/a2.md", "+++\nid = \"a2\"\ntitle = \"a2\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/b1.md", "+++\nid = \"b1\"\ntitle = \"b1\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/u1.md", "+++\nid = \"u1\"\ntitle = \"u1\"\nscope = []\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)

	counts := map[string]int{}
	sum := 0
	for _, tc := range result.TopicCounts {
		counts[tc.TopicKey] = tc.Count
		sum += tc.Count
	}
	assert.Equal(t, 2, counts["topic-a"], "indistinguishable revisions remain visible instead of fabricating a winner")
	assert.Equal(t, 1, counts["topic-b"])
	assert.Equal(t, 1, counts[UntopicedLabel], "an untopiced entry must still be counted, not silently dropped from the index view")
	assert.Equal(t, result.TotalVisible, sum, "topic counts must always sum to TotalVisible (NFR-2)")
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, []string{"a1", "a2"}, result.Conflicts[0].EntryIDs)
	for _, item := range result.Items {
		if item.TopicKey == "topic-a" {
			require.NotNil(t, item.Conflict, "a contested prime item must carry its conflict directly")
		}
	}
}

// TestPrimeIndexBreakdownIndependentOfByteBudget covers FR-1's "cost
// independent of entry content size" and NFR-2: the per-topic breakdown
// belongs to the index view, computed over the full visible set regardless
// of what the byte budget lets into Items -- the same guarantee
// TestPrimeTruncatesByByteBudgetWithoutAffectingAggregateCounts already pins
// for the freshness counts.
func TestPrimeIndexBreakdownIndependentOfByteBudget(t *testing.T) {
	dir := t.TempDir()
	// Distinct topic_keys, not a shared one: two entries that share a
	// topic_key would shadow-collapse to a single visible winner before the
	// budget is ever applied (see TestPrimeIndexIncludesPerTopicKeyBreakdown),
	// which would satisfy the "< 2 Items" precondition below for the wrong
	// reason -- via shadowing, not truncation -- and stop this test from
	// actually exercising the byte-budget guarantee it's named for.
	writeFile(t, dir, "global/a1.md", "+++\nid = \"a1\"\ntitle = \"a1\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/b1.md", "+++\nid = \"b1\"\ntitle = \"b1\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nx\n")

	// AMENDED by crn-s3749. This test used to assert the breakdown listed
	// every topic even at budget=1. That guarantee was the bug: the
	// enumeration was exempt from the budget, so it grew without limit (88.2%
	// of a payload measured 2.5x over budget on the mayor's live store).
	//
	// What survives is the half that was always right, and is what FR-1/NFR-2
	// actually needed: the index view's cost is independent of ENTRY CONTENT
	// size, and its aggregate scalars still describe the whole visible set. The
	// per-topic enumeration is now bounded like Items, and says what it dropped.
	generous, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, tc := range generous.TopicCounts {
		counts[tc.TopicKey] = tc.Count
	}
	assert.Equal(t, 1, counts["topic-a"], "with budget to spare the breakdown must still list every topic")
	assert.Equal(t, 1, counts["topic-b"], "with budget to spare the breakdown must still list every topic")
	assert.Zero(t, generous.TopicCountsTruncated, "nothing should be reported dropped when everything fits")

	tiny, err := Prime(t.Context(), dir, nil, 1)
	require.NoError(t, err)
	require.Less(t, len(tiny.Items), 2, "precondition: the tiny budget must actually truncate something")

	assert.Equal(t, 2, tiny.TotalVisible,
		"the index view's SCALARS must still cover the whole visible set under any budget")
	assert.Equal(t, 2-len(tiny.TopicCounts), tiny.TopicCountsTruncated,
		"whatever the budget keeps out of the enumeration must be reported, not silently missing")
}

// TestPrimeTruncatesOversizedTitleAndSummaryToCap covers crn-3476 FR-3's
// read-time layer and NFR-3: an entry written before the cap shipped (or
// written directly to disk, bypassing cmd/remember's write-time validation)
// must still have its PrimeItem Title/Summary bounded -- this is what makes
// itemByteCost a provable constant ceiling regardless of what's on disk.
func TestPrimeTruncatesOversizedTitleAndSummaryToCap(t *testing.T) {
	dir := t.TempDir()
	longTitle := strings.Repeat("t", 500)
	longSummary := strings.Repeat("s", 1000)
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \""+longTitle+"\"\nsummary = \""+longSummary+"\"\nscope = []\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
	assert.LessOrEqual(t, utf8.RuneCountInString(item.Title), titleCap, "Title must be truncated to the cap regardless of what's stored on disk")
	assert.LessOrEqual(t, utf8.RuneCountInString(item.Summary), summaryCap, "Summary must be truncated to the cap regardless of what's stored on disk")
}

// TestPrimeExplorationBandPromotesNewestZeroHitEntries covers crn-3476 FR-5's
// exploration band: the explorationSlots most-recently-created HitCount==0
// entries get band 0 and sort ahead of everything else regardless of
// HitCount -- so a brand-new entry gets at least one prime cycle near the
// top instead of being buried under a proven, high-hit_count entry it could
// otherwise never outrank on HitCount alone (crn-zcxq Finding 3).
func TestPrimeExplorationBandPromotesNewestZeroHitEntries(t *testing.T) {
	orig := explorationSlots
	explorationSlots = 2
	defer func() { explorationSlots = orig }()

	dir := t.TempDir()
	writeFile(t, dir, "global/old-high.md",
		"+++\nid = \"old-high\"\ntitle = \"old-high\"\nhit_count = 100\ncreated_at = \"2026-01-01T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/new-1.md",
		"+++\nid = \"new-1\"\ntitle = \"new-1\"\nhit_count = 0\ncreated_at = \"2026-03-03T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/new-2.md",
		"+++\nid = \"new-2\"\ntitle = \"new-2\"\nhit_count = 0\ncreated_at = \"2026-03-02T00:00:00Z\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/new-3.md",
		"+++\nid = \"new-3\"\ntitle = \"new-3\"\nhit_count = 0\ncreated_at = \"2026-03-01T00:00:00Z\"\nscope = []\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 4)

	ids := make([]string, len(result.Items))
	for i, it := range result.Items {
		ids[i] = it.ID
	}
	// band 0 = the 2 most-recently-created zero-hit entries (new-1, new-2),
	// ranked ahead of everything in band 1 regardless of old-high's much
	// larger HitCount. new-3 is only the 3rd-most-recent zero-hit entry, past
	// explorationSlots=2, so it falls to band 1 and loses to old-high there
	// on HitCount desc.
	assert.Equal(t, []string{"new-1", "new-2", "old-high", "new-3"}, ids)
}

// TestRenderPrimeTextIncludesSummary covers crn-3476 FR-8: the text path
// must render each item's (capped) Summary, not just Title -- itemByteCost
// already prices Summary into the budget (crn-zcxq Finding 2), so the budget
// was paying for content the default output never showed.
func TestRenderPrimeTextIncludesSummary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"a title\"\nsummary = \"a distinctive summary sentence\"\nscope = []\n+++\nx\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	out := RenderPrimeText(result)
	assert.Contains(t, out, "a distinctive summary sentence")
}

// topicCountsByteCost mirrors the production accounting for the index view so
// these tests measure the same bytes the budget is supposed to govern.
func topicCountsByteCost(tcs []TopicCount) int {
	total := 0
	for _, tc := range tcs {
		b, _ := json.Marshal(tc)
		total += len(b)
	}
	return total
}

// seedTopics writes n entries each carrying its own distinct topic_key, which
// is the shape that makes TopicCounts grow: in the real store 277 of 278 topic
// keys held exactly one entry, so the index is the key list rather than an
// aggregation.
func seedTopics(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("global/e%03d.md", i),
			fmt.Sprintf("+++\nid = \"e%03d\"\ntitle = \"entry %03d\"\ntopic_key = \"topic-%03d\"\nscope = []\n+++\nx\n", i, i, i))
	}
}

// TestPrimeBoundsTotalPayloadNotJustItems is crn-s3749. budgetBytes was
// enforced only against Items, while TopicCounts -- exempted as an "index
// view" -- grew unbounded with the number of distinct topic keys. Measured on
// the mayor's live store: 20,807 bytes emitted against an 8,192-byte budget,
// 88.2% of it TopicCounts. The budget must bound the whole itemized payload.
func TestPrimeBoundsTotalPayloadNotJustItems(t *testing.T) {
	dir := t.TempDir()
	seedTopics(t, dir, 200)

	const budget = 2048
	result, err := Prime(t.Context(), dir, nil, budget)
	require.NoError(t, err)

	itemBytes := 0
	for _, it := range result.Items {
		itemBytes += itemByteCost(it)
	}
	topicBytes := topicCountsByteCost(result.TopicCounts)

	require.Equal(t, 200, result.TotalVisible,
		"precondition: every seeded entry is visible, so the index view has something to bound")
	assert.LessOrEqual(t, itemBytes+topicBytes, budget,
		"the byte budget must bound the TOTAL itemized payload (items + topic counts), not items alone")
}

// TestPrimeReportsOmittedTopicsRatherThanTruncatingSilently pins the half that
// makes the cap safe. A silent cap is worse than the overrun it replaces: a
// reader cannot tell a short index from a complete one, which is the same
// false-success shape the store already warns about elsewhere.
func TestPrimeReportsOmittedTopicsRatherThanTruncatingSilently(t *testing.T) {
	dir := t.TempDir()
	seedTopics(t, dir, 200)

	result, err := Prime(t.Context(), dir, nil, 2048)
	require.NoError(t, err)

	require.Less(t, len(result.TopicCounts), 200,
		"precondition: this budget must actually drop topics from the index")
	assert.Equal(t, 200-len(result.TopicCounts), result.TopicCountsTruncated,
		"every topic dropped from the index must be counted in TopicCountsTruncated")
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "topic",
		"a truncated index must say so in Warnings, not just in a field a caller might not read")
}

// TestPrimeIndexKeepsRealAggregationsWhenTruncating covers the ordering the cap
// forces us to choose. Sorted by topic_key, truncation keeps whatever sorts
// early -- arbitrary. Topics holding more than one entry are the only rows that
// carry information a per-entry list does not, so they must survive.
func TestPrimeIndexKeepsRealAggregationsWhenTruncating(t *testing.T) {
	dir := t.TempDir()
	seedTopics(t, dir, 200)
	// "zzz-shared" sorts last by key and holds 3 entries. Under the old
	// key-ascending order it would be dropped first; it must now survive.
	for i := 0; i < 3; i++ {
		writeFile(t, dir, fmt.Sprintf("global/shared%d.md", i),
			fmt.Sprintf("+++\nid = \"shared%d\"\ntitle = \"shared %d\"\ntopic_key = \"zzz-shared\"\nscope = []\n+++\nx\n", i, i))
	}

	result, err := Prime(t.Context(), dir, nil, 2048)
	require.NoError(t, err)
	require.Less(t, len(result.TopicCounts), 201, "precondition: the index must be truncated")

	var shared *TopicCount
	for i := range result.TopicCounts {
		if result.TopicCounts[i].TopicKey == "zzz-shared" {
			shared = &result.TopicCounts[i]
		}
	}
	require.NotNil(t, shared, "a multi-entry topic must survive index truncation ahead of singletons")
	assert.Equal(t, 3, shared.Count)
}

// TestPrimeAggregateCountsStillCoverEverythingWhenIndexTruncates keeps the
// half of crn-3476 FR-1/FR-2 that is still right. The per-topic ENUMERATION is
// now bounded, but the scalars are O(1) bytes and must keep describing the
// whole visible set -- otherwise bounding the index would turn an honest
// overrun into an undercount.
func TestPrimeAggregateCountsStillCoverEverythingWhenIndexTruncates(t *testing.T) {
	dir := t.TempDir()
	seedTopics(t, dir, 200)

	result, err := Prime(t.Context(), dir, nil, 2048)
	require.NoError(t, err)

	require.Less(t, len(result.TopicCounts), 200, "precondition: the index must be truncated")
	assert.Equal(t, 200, result.TotalVisible,
		"TotalVisible must still cover the whole visible set even when the index enumeration is capped")
	assert.Equal(t, 200, result.FreshCount+result.StaleCount+result.UnknownCount,
		"the aggregate freshness counts must still sum to the whole visible set")
}
