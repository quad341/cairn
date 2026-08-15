package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const schemaVersionEntry = "+++\nid = \"sv1\"\ntitle = \"Schema Version\"\n" +
	"topic_key = \"schema-version\"\nscope = [\"rig:alpha\"]\n+++\nbody text\n"

// TestReadPathHealsAnIndexBuiltByAnOlderBinary is the upgrade path.
//
// Every existing deployment carries an index built by the previous release.
// Its git watermark matches HEAD, so nothing marked it stale, so the ALTER
// TABLE migrations never ran -- and the first read after upgrading failed
// with "SQL logic error: no such column: title_source". No existing test
// could catch it: they all build their index from scratch with the current
// binary, which is the one case that cannot reproduce the failure. So this
// test deliberately degrades a fresh index to the previous shape first.
func TestReadPathHealsAnIndexBuiltByAnOlderBinary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sv1.md", schemaVersionEntry)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	// Rewind the index to the shape a previous release would have left:
	// a column this binary selects is simply absent, and the version stamp
	// is behind. The watermark is deliberately left correct -- that is the
	// whole point, it is what stopped the rebuild from happening.
	_, err = db.ExecContext(t.Context(), `ALTER TABLE entries DROP COLUMN title_source`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE index_meta SET schema_version = 1`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	entries, err := Status(t.Context(), dir)
	require.NoError(t, err, "a read against an index built by an older binary must heal it, not fail")
	require.Len(t, entries, 1)
	assert.Equal(t, "sv1", entries[0].ID)
}

// TestReindexStampsCurrentSchemaVersion pins that a rebuilt index records the
// shape it was built at. Without the stamp, the healing above would run on
// every single read forever.
func TestReindexStampsCurrentSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sv1.md", schemaVersionEntry)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var got int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT schema_version FROM index_meta WHERE id = 1`).Scan(&got))
	assert.Equal(t, indexSchemaVersion, got)

	stale, _, err := indexStale(t.Context(), dir)
	require.NoError(t, err)
	assert.False(t, stale, "a freshly reindexed store must not report stale, or every read reindexes")
}

// TestSchemaVersionMismatchReportsItsOwnReason pins that the mismatch is
// distinguishable from a body edit. They warrant different rebuild budgets:
// a body edit is a small delta, a schema change rebuilds everything.
func TestSchemaVersionMismatchReportsItsOwnReason(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sv1.md", schemaVersionEntry)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE index_meta SET schema_version = 1`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	stale, reason, err := indexStale(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, stale)
	assert.Equal(t, staleSchemaVersion, reason)
}
