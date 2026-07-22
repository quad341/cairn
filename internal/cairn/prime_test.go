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

	out, err := Prime(dir, []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Contains(t, out, "alpha/thing", "an alpha-scoped agent should see the alpha topic")
	assert.Contains(t, out, "hand-author", "prime should still nudge agents to capture what they learn")

	bare, err := Prime(dir, nil)
	require.NoError(t, err)
	assert.NotContains(t, bare, "alpha/thing", "a bare identity should not see the alpha topic")
}

func TestPrimeEmpty(t *testing.T) {
	out, err := Prime(t.TempDir(), nil)
	require.NoError(t, err)
	assert.Contains(t, out, "No cached knowledge")
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

	out, err := Prime(dir, []string{"role:builder"})
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

	out, err := Prime(dir, []string{"role:investigator"})
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

	out, err := Prime(dir, []string{"role:investigator"})
	require.NoError(t, err)
	assert.NotContains(t, out, "tag-shape mismatch")
}
