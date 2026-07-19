package cairn

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReindex(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"t/a\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))

	n, err := Reindex(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var id, topic string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id, topic_key FROM entries").Scan(&id, &topic))
	assert.Equal(t, "a", id)
	assert.Equal(t, "t/a", topic)

	var tag string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT tag FROM entry_tags WHERE entry_id = 'a'").Scan(&tag))
	assert.Equal(t, "rig:alpha", tag)

	// reindex is idempotent: run again, still one row.
	_, err = Reindex(ctx, store)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count))
	assert.Equal(t, 1, count)
}
