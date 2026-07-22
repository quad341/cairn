package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestReindexPopulatesAnchorAndTimestampColumns(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\n" +
		"id = \"a\"\n" +
		"title = \"A\"\n" +
		"created_at = \"2026-01-01T00:00:00Z\"\n" +
		"\n" +
		"[anchor]\n" +
		"type = \"files\"\n" +
		"repo = \"/some/repo\"\n" +
		"paths = [\"a.go\", \"b.go\"]\n" +
		"spec = \"main\"\n" +
		"fingerprint = \"abc123\"\n" +
		"+++\n" +
		"body\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var repo, paths, spec, createdAt string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT anchor_repo, anchor_paths, anchor_spec, created_at FROM entries WHERE id = 'a'",
	).Scan(&repo, &paths, &spec, &createdAt))
	assert.Equal(t, "/some/repo", repo)
	assert.Equal(t, "main", spec)
	assert.Equal(t, "2026-01-01T00:00:00Z", createdAt)

	var gotPaths []string
	require.NoError(t, json.Unmarshal([]byte(paths), &gotPaths))
	assert.Equal(t, []string{"a.go", "b.go"}, gotPaths)
}

func TestReindexStampsIndexMeta(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	wantCommit, ok := git(ctx, store, "rev-parse", "HEAD")
	require.True(t, ok)
	require.NotEmpty(t, wantCommit)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var gotCommit string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT indexed_at_commit FROM index_meta WHERE id = 1").Scan(&gotCommit))
	assert.Equal(t, wantCommit, gotCommit)
}

func TestReindexPreservesHitCount(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, "UPDATE entries SET hit_count = 5 WHERE id = 'a'")
	require.NoError(t, err)

	// A reindex reflects the body, not the index's own hit_count, so this
	// upsert must leave the counter alone rather than reseting it from the
	// body's implicit zero (crn-6az.6.1.1).
	_, err = Reindex(ctx, store)
	require.NoError(t, err)

	var hitCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT hit_count FROM entries WHERE id = 'a'").Scan(&hitCount))
	assert.Equal(t, 5, hitCount)
}

func TestReindexRemovesDeletedEntries(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "b.md"),
		[]byte("+++\nid = \"b\"\ntitle = \"B\"\n+++\nbody\n"), 0o600))

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(store, "global", "b.md")))

	n, err := Reindex(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count))
	assert.Equal(t, 1, count)

	var id string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM entries").Scan(&id))
	assert.Equal(t, "a", id)
}

func TestReindexMigratesLegacyIndexSchema(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	// Simulate an index.sqlite built by a pre-crn-6az.6.1.1 binary: entries
	// exists but lacks the anchor_repo/anchor_paths/anchor_spec/created_at
	// columns this bead adds.
	require.NoError(t, os.MkdirAll(filepath.Dir(IndexPath(store)), 0o750))
	legacyDB, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	_, err = legacyDB.ExecContext(ctx, `
CREATE TABLE entries (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  summary TEXT,
  type TEXT,
  topic_key TEXT,
  body_path TEXT NOT NULL,
  anchor_type TEXT,
  anchor_fingerprint TEXT,
  verified_at TEXT,
  created_by TEXT,
  hit_count INTEGER DEFAULT 0
);
CREATE TABLE entry_tags (entry_id TEXT, tag TEXT);
`)
	require.NoError(t, err)
	require.NoError(t, legacyDB.Close())

	n, err := Reindex(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var repo, paths, spec, createdAt string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT anchor_repo, anchor_paths, anchor_spec, created_at FROM entries WHERE id = 'a'",
	).Scan(&repo, &paths, &spec, &createdAt))
}

func TestEnsureFreshDelegatesToReindex(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	require.NoError(t, ensureFresh(ctx, store))

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestEnsureFreshReindexesWhenIndexMissing(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	calls := 0
	countingReindex := func(ctx context.Context, store string) (int, error) {
		calls++
		return Reindex(ctx, store)
	}

	require.NoError(t, ensureFreshWith(ctx, store, countingReindex))
	assert.Equal(t, 1, calls)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestEnsureFreshSkipsReindexWhenAlreadyFresh(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	calls := 0
	countingReindex := func(ctx context.Context, store string) (int, error) {
		calls++
		return Reindex(ctx, store)
	}

	require.NoError(t, ensureFreshWith(ctx, store, countingReindex))
	assert.Equal(t, 0, calls, "index already matched HEAD; ensureFresh must not reindex")
}

func TestEnsureFreshReindexesOnHeadMismatch(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "b.md"),
		[]byte("+++\nid = \"b\"\ntitle = \"B\"\n+++\nbody\n"), 0o600))
	gitCommitAll(t, store, "add b")

	calls := 0
	countingReindex := func(ctx context.Context, store string) (int, error) {
		calls++
		return Reindex(ctx, store)
	}

	require.NoError(t, ensureFreshWith(ctx, store, countingReindex))
	assert.Equal(t, 1, calls)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count))
	assert.Equal(t, 2, count)

	wantCommit, ok := git(ctx, store, "rev-parse", "HEAD")
	require.True(t, ok)
	var gotCommit string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT indexed_at_commit FROM index_meta WHERE id = 1").Scan(&gotCommit))
	assert.Equal(t, wantCommit, gotCommit)
}

func TestEnsureFreshNoopOnNonGitStore(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	calls := 0
	countingReindex := func(ctx context.Context, store string) (int, error) {
		calls++
		return Reindex(ctx, store)
	}

	require.NoError(t, ensureFreshWith(ctx, store, countingReindex))
	assert.Equal(t, 0, calls, "non-git store has no HEAD to drift from; an already-built index must not be reindexed")
}
