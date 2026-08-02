package cairn

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexFromInsideStore indexes the store the way default store resolution
// does it: cwd'd into the store, with a relative store argument. That is the
// path `cairn <cmd>` takes when it's run from the store with no --store and no
// $CAIRN_STORE, so it is the shape real stores get built with -- not a
// contrived input.
func indexFromInsideStore(t *testing.T, store string) {
	t.Helper()
	t.Chdir(store)
	_, err := Reindex(t.Context(), ".")
	require.NoError(t, err)
}

// TestFindResolvesBodyPathAgainstStoreNotCWD is crn-o6mn. A store indexed with
// a relative --store records cwd-relative body_paths, and every later reader
// outside that cwd fails to open the body. Because a non-git store is never
// considered stale once indexed, it never self-heals either.
func TestFindResolvesBodyPathAgainstStoreNotCWD(t *testing.T) {
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")
	indexFromInsideStore(t, store)

	// Now read from somewhere else entirely, which is where every agent runs.
	t.Chdir(t.TempDir())

	e, err := Find(t.Context(), store, "a")
	require.NoError(t, err, "recall must work from a cwd that is not the store")
	assert.Equal(t, "a", e.ID)
}

// TestListResolvesBodyPathAgainstStoreNotCWD covers the batched read path,
// which does its own body_path lookup separate from Find's point query.
func TestListResolvesBodyPathAgainstStoreNotCWD(t *testing.T) {
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"t\"\n+++\nbody\n")
	indexFromInsideStore(t, store)

	t.Chdir(t.TempDir())

	got, err := ListByTopic(t.Context(), store, "t", nil)
	require.NoError(t, err, "list must work from a cwd that is not the store")
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
}

// TestReindexRecordsAbsoluteBodyPath pins the write side. Storing an absolute
// path is what stops the index from being cwd-dependent in the first place;
// the read-side resolution above is the safety net for indexes already on
// disk carrying relative paths.
func TestReindexRecordsAbsoluteBodyPath(t *testing.T) {
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")
	indexFromInsideStore(t, store)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var bodyPath string
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT body_path FROM entries WHERE id = 'a'").Scan(&bodyPath))
	assert.True(t, filepath.IsAbs(bodyPath),
		"body_path must be absolute so it does not depend on the indexing cwd, got %q", bodyPath)

	_, err = os.Stat(bodyPath)
	assert.NoError(t, err, "the recorded body_path must resolve to the real file")
}
