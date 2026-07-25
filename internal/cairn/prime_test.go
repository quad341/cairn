package cairn

import (
	"os"
	"path/filepath"
	"testing"

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

	out := RenderPrimeText(result, dir, []string{"rig:alpha"})
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

	out := RenderPrimeText(result, dir, nil)
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
	out := RenderPrimeText(result, dir, nil)
	assert.NotContains(t, out, "no `remember` command yet")
	assert.NotContains(t, out, "hand-author")
	assert.Contains(t, out, "cairn remember")
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

	out := RenderPrimeText(result, dir, []string{"role:builder"})
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

	out := RenderPrimeText(result, dir, []string{"role:investigator"})
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

	out := RenderPrimeText(result, dir, []string{"role:investigator"})
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
	fp := ComputeFingerprint(t.Context(), Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
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
