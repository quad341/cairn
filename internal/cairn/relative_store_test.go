package cairn

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const relStoreEntryA = "+++\nid = \"r1\"\ntitle = \"R1\"\ntopic_key = \"shared-key\"\n" +
	"scope = [\"rig:alpha\"]\n+++\nbody one\n"

const relStoreEntryB = "+++\nid = \"r2\"\ntitle = \"R2\"\ntopic_key = \"shared-key\"\n" +
	"scope = [\"rig:alpha\"]\n+++\nbody two\n"

// TestEntryTierResolvesFromARelativeStore is the regression.
//
// BodyPath is always absolute -- walkEntries absolute-ises the root so an
// entry's path does not depend on the cwd it was walked from. entryTier then
// compared that absolute path against the store string AS GIVEN, so a
// relative store made filepath.Rel fail and entryTier return "".
//
// Nothing checks that "": it is not an error, just a tier name matching
// nothing. Every tier-gated check downstream silently found no work, and
// `cairn doctor --store .` reported a clean bill of health on a store with
// hundreds of real findings.
func TestEntryTierResolvesFromARelativeStore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/r1.md", relStoreEntryA)

	entries, err := IterEntries(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "rig", entryTier(dir, entries[0]), "absolute store must classify the tier")

	// Same store, named relatively from inside it.
	t.Chdir(dir)
	relEntries, err := IterEntries(".")
	require.NoError(t, err)
	require.Len(t, relEntries, 1)
	assert.Equal(t, "rig", entryTier(".", relEntries[0]),
		"a relative store must classify the tier identically, not fall through to \"\"")
}

// TestDiagnoseFindsTheSameProblemsFromARelativeStore pins the behaviour that
// actually mattered: the report must not depend on how the store was named.
// Asserting on entryTier alone would not have caught this, because the damage
// was downstream -- an unknown tier silently disables dedup, the freshness
// sweep and the agent-tier rule.
func TestDiagnoseFindsTheSameProblemsFromARelativeStore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/r1.md", relStoreEntryA)
	writeFile(t, dir, "rig/alpha/r2.md", relStoreEntryB)

	// Settle the index first: the very first Diagnose on a fresh store
	// legitimately reports index drift, and that one-shot finding would
	// otherwise show up as a spurious difference between the two calls.
	_, err := Diagnose(t.Context(), dir)
	require.NoError(t, err)

	fromAbs, err := Diagnose(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, fromAbs.Findings, "two entries sharing a topic_key must produce findings")

	t.Chdir(dir)
	fromRel, err := Diagnose(t.Context(), ".")
	require.NoError(t, err)

	assert.Equal(t, len(fromAbs.Findings), len(fromRel.Findings),
		"a health report must not depend on whether the store was named absolutely or relatively")
	assert.Equal(t, fromAbs.Stats.EntriesByTier, fromRel.Stats.EntriesByTier)
	assert.NotContains(t, fromRel.Stats.EntriesByTier, "",
		"no entry may land in the empty tier -- that is the silent-failure signature")
}

// TestEntryTierRejectsAnEntryOutsideTheStore pins that an entry which is not
// under the given store reports no tier, rather than the ".." that
// filepath.Rel produces walking upwards. A bogus tier is worse than an
// unknown one: it silently joins dedup and freshness groups it has no
// business in.
func TestEntryTierRejectsAnEntryOutsideTheStore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/r1.md", relStoreEntryA)
	entries, err := IterEntries(dir)
	require.NoError(t, err)

	assert.Equal(t, "", entryTier(filepath.Join(dir, "elsewhere"), entries[0]))
}
