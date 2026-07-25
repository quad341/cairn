package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md",
		"+++\nid = \"r\"\ntitle = \"r\"\ntopic_key = \"alpha/thing\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	out, err := Prime(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Contains(t, out, "alpha/thing", "an alpha-scoped agent should see the alpha topic")
	assert.Contains(t, out, "cairn remember", "prime should still nudge agents to capture what they learn")

	bare, err := Prime(t.Context(), dir, nil)
	require.NoError(t, err)
	assert.NotContains(t, bare, "alpha/thing", "a bare identity should not see the alpha topic")
}

func TestPrimeEmpty(t *testing.T) {
	out, err := Prime(t.Context(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.Contains(t, out, "No cached knowledge")
}

// TestPrimeDoesNotClaimRememberMissing covers crn-6az.2: prime's footer used
// to hardcode "no `remember` command yet" and tell agents to hand-author
// entries directly, which went stale the moment the remember command
// shipped (it now writes entries itself, including committing/routing them
// for review) -- leaving prime denying a command that cairn --help lists.
func TestPrimeDoesNotClaimRememberMissing(t *testing.T) {
	out, err := Prime(t.Context(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.NotContains(t, out, "no `remember` command yet")
	assert.NotContains(t, out, "hand-author")
	assert.Contains(t, out, "cairn remember")
}

// TestPrimeWarnsOnUnmatchedScopeDimension is crn-ln1 acceptance criterion 1
// and 3 (populated-but-unmatched case): the store has role-scoped entries,
// the identity carries a role: tag, but no entry's scope matches it — this
// is the silent-miss shape the diagnostic exists to catch, including when it
// drives the visible count to zero (the "No cached knowledge" branch).
func TestPrimeWarnsOnUnmatchedScopeDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/o.md",
		"+++\nid = \"o\"\ntitle = \"o\"\ntopic_key = \"o/thing\"\nscope = [\"role:investigator\"]\n+++\nx\n")

	out, err := Prime(t.Context(), dir, []string{"role:builder"})
	require.NoError(t, err)
	assert.Contains(t, out, "No cached knowledge", "precondition: the mismatch should leave nothing visible")
	assert.Contains(t, out, "role:", "warning should name the mismatched dimension")
	assert.Contains(t, out, "tag-shape mismatch")
}

// TestPrimeNoWarningOnScopeMatch is crn-ln1 acceptance criterion 3 (working
// as intended case): a genuine, non-empty match must never warn.
func TestPrimeNoWarningOnScopeMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/o.md",
		"+++\nid = \"o\"\ntitle = \"o\"\ntopic_key = \"o/thing\"\nscope = [\"role:investigator\"]\n+++\nx\n")

	out, err := Prime(t.Context(), dir, []string{"role:investigator"})
	require.NoError(t, err)
	assert.Contains(t, out, "o/thing", "precondition: the entry should actually be visible")
	assert.NotContains(t, out, "tag-shape mismatch")
}

// TestPrimeNoWarningOnEmptyScopeDimension is crn-ln1 acceptance criterion 3
// (nothing-to-warn-about case): the identity carries a role: tag, but the
// store has zero role-scoped entries anywhere -- there is nothing to have
// silently missed, so this must stay quiet too.
func TestPrimeNoWarningOnEmptyScopeDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	out, err := Prime(t.Context(), dir, []string{"role:investigator"})
	require.NoError(t, err)
	assert.NotContains(t, out, "tag-shape mismatch")
}

// TestPrimeStructuredReturnsItemsAndCounts covers crn-od2x.2: cairn prime
// --json needs the same visible-set computation Prime's rendered text uses,
// but as structured data an agent can parse without scraping prose. Field
// names mirror crn-0vqk.1/crn-0vqk.2's PrimeResult/PrimeItem exactly so
// either bead's --json consumers can depend on the same shape regardless of
// merge order.
func TestPrimeStructuredReturnsItemsAndCounts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md",
		"+++\nid = \"r\"\ntitle = \"r title\"\nsummary = \"r summary\"\ntopic_key = \"alpha/thing\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	db, err := openDB(dir)
	require.NoError(t, err)
	require.NoError(t, ensureFresh(t.Context(), dir))
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET hit_count = 3 WHERE id = 'r'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	result, err := PrimeStructured(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	assert.Equal(t, dir, result.Store)
	assert.Equal(t, []string{"rig:alpha"}, result.Identity)
	assert.Equal(t, 2, result.TotalVisible)
	require.Len(t, result.Items, 2)
	assert.Equal(t, 0, result.TruncatedCount, "this bead does no budget-aware truncation")
	assert.False(t, result.ChecksCapped, "this bead does no budget-aware capping")
	assert.Equal(t, 0, result.FreshCount, "neither fixture entry has an anchor")
	assert.Equal(t, 0, result.StaleCount, "neither fixture entry has an anchor")
	assert.Equal(t, 2, result.UnknownCount, "neither fixture entry has an anchor")

	var item PrimeItem
	for _, it := range result.Items {
		if it.ID == "r" {
			item = it
		}
	}
	assert.Equal(t, "alpha/thing", item.TopicKey)
	assert.Equal(t, "r title", item.Title)
	assert.Equal(t, "r summary", item.Summary)
	assert.Equal(t, 3, item.HitCount, "hit_count is index-only state, not re-derived from the body")
	assert.Equal(t, Unknown, item.Freshness.Status, "no anchor -> unknown freshness")
	assert.Contains(t, item.Freshness.Detail, "no source anchor")
}

// TestPrimeStructuredWarnsOnUnmatchedScopeDimension confirms PrimeStructured
// surfaces the same scope-mismatch diagnostic as Prime's rendered text
// (crn-ln1), just as a string slice instead of prose lines.
func TestPrimeStructuredWarnsOnUnmatchedScopeDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/o.md",
		"+++\nid = \"o\"\ntitle = \"o\"\ntopic_key = \"o/thing\"\nscope = [\"role:investigator\"]\n+++\nx\n")

	result, err := PrimeStructured(t.Context(), dir, []string{"role:builder"})
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalVisible, "precondition: the mismatch should leave nothing visible")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "tag-shape mismatch")
}
