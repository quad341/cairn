package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStoreShapeTierCounts covers crn-n5yaz's FR-7 acceptance criterion:
// StoreShape must count every tier's entries, including agent/ -- unlike
// Sweep (whose remit deliberately excludes agent/, see TestSweepTierScoping),
// a store-shape summary for a bug report has no reason to omit a whole tier
// from its counts.
func TestStoreShapeTierCounts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "rig/alpha/r.md", "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	writeFile(t, dir, "role/investigator/o.md", "+++\nid = \"o\"\ntitle = \"o\"\nscope = [\"role:investigator\"]\n+++\nx\n")
	writeFile(t, dir, "agent/bot/a.md", "+++\nid = \"a\"\ntitle = \"a\"\nscope = [\"agent:bot\"]\n+++\nx\n")

	shape, err := StoreShape(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, 1, shape.TierCounts["global"])
	assert.Equal(t, 1, shape.TierCounts["rig"])
	assert.Equal(t, 1, shape.TierCounts["role"])
	assert.Equal(t, 1, shape.TierCounts["agent"], "unlike Sweep, StoreShape must count agent/ entries")
}

// TestStoreShapeBodyCount is the total-entries-found-on-disk acceptance
// criterion: BodyCount is the sum across every tier, independent of whatever
// the index currently holds.
func TestStoreShapeBodyCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g1.md", "+++\nid = \"g1\"\ntitle = \"g1\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/g2.md", "+++\nid = \"g2\"\ntitle = \"g2\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "rig/alpha/r1.md", "+++\nid = \"r1\"\ntitle = \"r1\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	shape, err := StoreShape(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, 3, shape.BodyCount)
}

// TestStoreShapeIndexDriftWhenUnindexed covers the index-vs-bodies drift
// signal: a store with bodies on disk but no index rebuild ever run must
// report IndexCount == 0 and IndexDrift == true. This is deliberately a
// different signal than Diagnose's own CategoryIndexDrift finding (which
// checks the indexed git-commit watermark via IndexStale) -- StoreShape's
// IndexDrift is a raw row-count comparison, so it stays meaningful even
// alongside an embedded Diagnose Report in the same rage bundle instead of
// duplicating it.
func TestStoreShapeIndexDriftWhenUnindexed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n")

	shape, err := StoreShape(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, 0, shape.IndexCount)
	assert.Equal(t, 1, shape.BodyCount)
	assert.True(t, shape.IndexDrift, "index has 0 rows but 1 body exists on disk")
}

// TestStoreShapeNoDriftAfterReindex confirms IndexDrift clears once the
// index actually matches the bodies on disk -- StoreShape must read the
// index's current state, never rebuild it itself (a self-healing reindex
// inside StoreShape would make IndexDrift permanently false, defeating the
// signal FR-7 asks for).
func TestStoreShapeNoDriftAfterReindex(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n")

	entries, err := IterEntries(dir)
	require.NoError(t, err)
	_, err = ReindexEntries(ctx, dir, entries)
	require.NoError(t, err)

	shape, err := StoreShape(ctx, dir)
	require.NoError(t, err)

	assert.Equal(t, 1, shape.IndexCount)
	assert.Equal(t, 1, shape.BodyCount)
	assert.False(t, shape.IndexDrift)
}

// TestStoreShapeTolerantOfMalformedEntry mirrors Diagnose's own contract
// (internal/cairn/doctor.go): one corrupted file must never abort the whole
// shape computation, since rage's entire purpose is to still produce a
// useful bundle from an unhealthy store.
func TestStoreShapeTolerantOfMalformedEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/good.md", "+++\nid = \"good\"\ntitle = \"good\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/bad.md", "not even toml frontmatter")

	shape, err := StoreShape(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, 1, shape.BodyCount)
	assert.Equal(t, 1, shape.TierCounts["global"])
}
