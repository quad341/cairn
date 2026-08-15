package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestReindexEntriesBuildsIndexFromSuppliedList(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"t/a\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))

	entries, failures, err := IterEntriesTolerant(store)
	require.NoError(t, err)
	require.Empty(t, failures)

	n, err := ReindexEntries(ctx, store, entries)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var id string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM entries").Scan(&id))
	assert.Equal(t, "a", id)
}

func TestIndexStaleDelegatesToUnexportedCheck(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))

	stale, err := IndexStale(ctx, store)
	require.NoError(t, err)
	assert.True(t, stale, "a store with no index built yet must report stale")

	_, err = Reindex(ctx, store)
	require.NoError(t, err)

	stale, err = IndexStale(ctx, store)
	require.NoError(t, err)
	assert.False(t, stale, "immediately after a reindex on a non-git store the index must report fresh")
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

	wantCommit, ok, err := git(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
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

func TestReindexPopulatesNewFieldColumns(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\n" +
		"id = \"a\"\n" +
		"title = \"A\"\n" +
		"kind = \"remediation\"\n" +
		"auto_actionable = true\n" +
		"recurrence_count = 3\n" +
		"promoted_bead_id = \"crn-abcd\"\n" +
		"last_recalled_at = \"2026-07-20T00:00:00Z\"\n" +
		"+++\n" +
		"body\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var kind, promotedBeadID, lastRecalledAt string
	var autoActionable, recurrenceCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT kind, auto_actionable, recurrence_count, promoted_bead_id, last_recalled_at FROM entries WHERE id = 'a'",
	).Scan(&kind, &autoActionable, &recurrenceCount, &promotedBeadID, &lastRecalledAt))
	assert.Equal(t, "remediation", kind)
	assert.Equal(t, 1, autoActionable)
	assert.Equal(t, 3, recurrenceCount)
	assert.Equal(t, "crn-abcd", promotedBeadID)
	assert.Equal(t, "2026-07-20T00:00:00Z", lastRecalledAt)
}

// TestReindexPreservesNewIndexOnlyFields covers this bead's core acceptance
// criterion: LastRecalledAt/RecurrenceCount/PromotedBeadID/Kind/AutoActionable
// are index-only state like hit_count (TestReindexPreservesHitCount) once a
// future call site (crn-28ge.1.2/.1.4/.1.5) writes them directly via SQL --
// a reindex must leave them alone rather than resetting them from the body's
// stale-by-construction zero values.
func TestReindexPreservesNewIndexOnlyFields(t *testing.T) {
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

	_, err = db.ExecContext(ctx, `UPDATE entries SET
		kind = 'remediation', auto_actionable = 1, recurrence_count = 5,
		promoted_bead_id = 'crn-live', last_recalled_at = '2026-07-21T00:00:00Z'
		WHERE id = 'a'`)
	require.NoError(t, err)

	_, err = Reindex(ctx, store)
	require.NoError(t, err)

	var kind, promotedBeadID, lastRecalledAt string
	var autoActionable, recurrenceCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT kind, auto_actionable, recurrence_count, promoted_bead_id, last_recalled_at FROM entries WHERE id = 'a'",
	).Scan(&kind, &autoActionable, &recurrenceCount, &promotedBeadID, &lastRecalledAt))
	assert.Equal(t, "remediation", kind, "kind is index-only state; reindex must not reset it from the body")
	assert.Equal(t, 1, autoActionable, "auto_actionable is index-only state; reindex must not reset it from the body")
	assert.Equal(t, 5, recurrenceCount, "recurrence_count is index-only state; reindex must not reset it from the body")
	assert.Equal(t, "crn-live", promotedBeadID, "promoted_bead_id is index-only state; reindex must not reset it from the body")
	assert.Equal(t, "2026-07-21T00:00:00Z", lastRecalledAt, "last_recalled_at is index-only state; reindex must not reset it from the body")
}

// TestReindexMigratesLegacyIndexSchemaNewFields covers an index.sqlite built
// by a binary that predates this bead's 5 new columns -- mirrors
// TestReindexMigratesLegacyIndexSchema's coverage of the crn-6az.6.1.1
// migration, one bead's schema addition later.
func TestReindexMigratesLegacyIndexSchemaNewFields(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

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
  anchor_repo TEXT,
  anchor_paths TEXT,
  anchor_spec TEXT,
  anchor_fingerprint TEXT,
  verified_at TEXT,
  created_by TEXT,
  created_at TEXT,
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

	var kind, promotedBeadID, lastRecalledAt string
	var autoActionable, recurrenceCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT kind, auto_actionable, recurrence_count, promoted_bead_id, last_recalled_at FROM entries WHERE id = 'a'",
	).Scan(&kind, &autoActionable, &recurrenceCount, &promotedBeadID, &lastRecalledAt))
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

	wantCommit, ok, err := git(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
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

func TestEnsureFreshWithLogsIndexDriftWhenStale(t *testing.T) {
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	recs := testLogRecords(t, func(ctx context.Context) {
		require.NoError(t, ensureFreshWith(ctx, store, Reindex))
	})

	require.Len(t, recs, 1, "ensureFreshWith must emit exactly one index_drift record")
	rec := recs[0]
	assert.Equal(t, "index_drift", rec["kind"])
	assert.Equal(t, true, rec["stale"])
	assert.Equal(t, true, rec["reindexed"])
	assert.Equal(t, float64(1), rec["reindex_count"])
	assert.Equal(t, "", rec["reindex_error"])
	assert.GreaterOrEqual(t, rec["duration_ms"], float64(0))
}

func TestEnsureFreshWithLogsIndexDriftWhenFresh(t *testing.T) {
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(t.Context(), store)
	require.NoError(t, err)

	recs := testLogRecords(t, func(ctx context.Context) {
		require.NoError(t, ensureFreshWith(ctx, store, Reindex))
	})

	require.Len(t, recs, 1, "ensureFreshWith must emit exactly one index_drift record")
	rec := recs[0]
	assert.Equal(t, "index_drift", rec["kind"])
	assert.Equal(t, false, rec["stale"])
	assert.Equal(t, false, rec["reindexed"])
	assert.Equal(t, float64(0), rec["reindex_count"])
	assert.Equal(t, "", rec["reindex_error"])
}

func TestEnsureFreshWithLogsIndexDriftOnReindexError(t *testing.T) {
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	failingReindex := func(_ context.Context, _ string) (int, error) {
		return 0, errors.New("boom")
	}

	var callErr error
	recs := testLogRecords(t, func(ctx context.Context) {
		callErr = ensureFreshWith(ctx, store, failingReindex)
	})

	require.Error(t, callErr)
	require.Len(t, recs, 1, "ensureFreshWith must emit exactly one index_drift record even when reindex fails")
	rec := recs[0]
	assert.Equal(t, "index_drift", rec["kind"])
	assert.Equal(t, true, rec["stale"])
	assert.Equal(t, false, rec["reindexed"])
	assert.Equal(t, float64(0), rec["reindex_count"])
	assert.Equal(t, "boom", rec["reindex_error"])
}

func TestEnsureFreshWithRedactsSecretsInReindexError(t *testing.T) {
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))

	failingReindex := func(_ context.Context, _ string) (int, error) {
		return 0, fmt.Errorf("db error near token %s", testAWSKeyExample)
	}

	recs := testLogRecords(t, func(ctx context.Context) {
		require.Error(t, ensureFreshWith(ctx, store, failingReindex))
	})

	require.Len(t, recs, 1)
	rec := recs[0]
	assert.NotContains(t, rec["reindex_error"], testAWSKeyExample, "a secret-shaped string must never reach the log verbatim")
	assert.Contains(t, rec["reindex_error"], "[redacted:AWS access key ID]")
}

func TestOpenDBAppliesBusyTimeoutAndWALPragmas(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()

	db, err := openDB(store)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var busyTimeout int
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
	assert.Equal(t, 5000, busyTimeout, "busy_timeout must be set so a losing concurrent caller waits instead of hard-failing (crn-t250)")

	var journalMode string
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode))
	assert.Equal(t, "wal", journalMode)
}

func TestIndexStalePropagatesGitInvocationError(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"),
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n"), 0o600))
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	t.Setenv("PATH", t.TempDir()) // git binary now unreachable

	_, err = indexStale(ctx, store)
	require.Error(t, err, `a git invocation failure must propagate as an error, not be silently reported as "not stale" (crn-t250)`)
}

// TestConcurrentFindAndReindexDoNotHardFail is the crn-gjmy-mandated
// regression test: without openDB's busy_timeout/WAL pragmas, concurrent
// Find (which both reads and writes hit_count) and Reindex (which opens its
// own write transaction) racing against the one shared CAIRN_STORE index
// reliably produced a hard "database is locked" failure for the losing
// caller, rather than the loser simply waiting its turn (crn-t250).
//
// This deliberately runs a single background reindexer, not several:
// concurrent Reindex-vs-Reindex calls exercise a separate, independently-
// fixed race on entry_tags' schema statements (crn-j3k4, see
// TestConcurrentReindexDoesNotRaceOnEntryTagsSchema below) -- a different
// failure mode with a different root cause than what this test covers, so it
// doesn't belong here.
func TestConcurrentFindAndReindexDoNotHardFail(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		body := "+++\nid = \"" + id + "\"\ntitle = \"" + id + "\"\n+++\nbody\n"
		require.NoError(t, os.WriteFile(filepath.Join(store, "global", id+".md"), []byte(body), 0o600))
	}
	gitInit(t, store)
	gitCommitAll(t, store, "init")

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	const (
		finders             = 8
		finderIterations    = 15
		reindexerIterations = 20
	)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	record := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(finders + 1)
	for range finders {
		go func() {
			defer wg.Done()
			for j := range finderIterations {
				_, err := Find(ctx, store, ids[j%len(ids)])
				record(err)
			}
		}()
	}
	go func() {
		defer wg.Done()
		for range reindexerIterations {
			_, err := Reindex(ctx, store)
			record(err)
		}
	}()
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf(`%d/%d concurrent Find/Reindex calls failed (want 0) -- first error: %v (openDB must set busy_timeout so a losing caller waits instead of hard-failing with "database is locked")`,
			len(errs), finders*finderIterations+reindexerIterations, errs[0])
	}
}

// TestConcurrentReindexDoesNotRaceOnEntryTagsSchema is the crn-j3k4
// regression test: entry_tags' DROP TABLE IF EXISTS + CREATE TABLE used to
// run as two independent autocommit statements, outside any transaction and
// before reindexTx's own tx began. Two concurrent Reindex() calls could
// interleave those statements -- e.g. both DROPs succeed (IF EXISTS), then
// both CREATEs race -- and the loser hit a hard "table entry_tags already
// exists" SQL logic error. This is a different failure mode than crn-t250's
// SQLITE_BUSY: busy_timeout doesn't help a DDL race, only lock contention.
//
// The seed Reindex call below is deliberate: it ensures index.sqlite already
// exists before the concurrent goroutines start, isolating this test to the
// entry_tags DDL race this bead fixes. The cold-store case -- racing Reindex
// calls against a store where index.sqlite does not exist yet -- was a
// separate, narrower SQLITE_BUSY failure (crn-t42e); it is covered by
// TestConcurrentReindexOnColdStoreDoesNotHardFail below rather than here.
func TestConcurrentReindexDoesNotRaceOnEntryTagsSchema(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		body := "+++\nid = \"" + id + "\"\ntitle = \"" + id + "\"\n+++\nbody\n"
		require.NoError(t, os.WriteFile(filepath.Join(store, "global", id+".md"), []byte(body), 0o600))
	}

	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	const (
		reindexers = 4
		iterations = 20
	)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	record := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	wg.Add(reindexers)
	for range reindexers {
		go func() {
			defer wg.Done()
			for range iterations {
				_, err := Reindex(ctx, store)
				record(err)
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf(`%d/%d concurrent Reindex calls failed (want 0) -- first error: %v (entry_tags DROP+CREATE must run inside the same tx as the rest of Reindex so concurrent rebuilds serialize instead of racing on the DDL)`,
			len(errs), reindexers*iterations, errs[0])
	}
}

// TestConcurrentReindexOnColdStoreDoesNotHardFail is the crn-t42e regression
// test: the same shape as TestConcurrentReindexDoesNotRaceOnEntryTagsSchema
// but with NO seed Reindex, so index.sqlite does not exist when the
// goroutines start and they all race to create it.
//
// entriesSchema used to run as an autocommit statement before BeginTx. An
// autocommit CREATE TABLE upgrades a read lock to a write lock mid-statement,
// and SQLite answers a contended upgrade with an immediate SQLITE_BUSY
// instead of invoking the busy handler -- so busy_timeout was never
// consulted and the call failed instantly. Moving it inside the
// _txlock=immediate transaction takes the write lock at BEGIN, where the
// retry loop does apply.
//
// This is the production path, not a synthetic one: ensureFresh treats "no
// watermark row" (which includes "index.sqlite doesn't exist yet") as stale
// and reindexes synchronously, so a fleet cold-start against a fresh
// CAIRN_STORE puts every agent on this line at once.
//
// The failure was probabilistic -- measured at 1 bad call per 80, in ~13% of
// stress runs -- so a single pass here does not prove much on its own; the
// fix was verified at -count=30. What this test pins cheaply is that the
// cold-store path stays exercised at all, and it still catches a hard
// regression (an unconditional failure) on the first run.
func TestConcurrentReindexOnColdStoreDoesNotHardFail(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	for _, id := range []string{"a", "b", "c"} {
		body := "+++\nid = \"" + id + "\"\ntitle = \"" + id + "\"\n+++\nbody\n"
		require.NoError(t, os.WriteFile(filepath.Join(store, "global", id+".md"), []byte(body), 0o600))
	}
	// Deliberately NO seed Reindex here -- that absence is the whole test.

	const (
		reindexers = 4
		iterations = 20
	)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	wg.Add(reindexers)
	for range reindexers {
		go func() {
			defer wg.Done()
			for range iterations {
				if _, err := Reindex(ctx, store); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf(`%d/%d concurrent Reindex calls against a COLD store failed (want 0) -- first error: %v (entriesSchema must run inside ReindexEntries' _txlock=immediate tx; as an autocommit statement its lock upgrade returns SQLITE_BUSY without consulting busy_timeout)`,
			len(errs), reindexers*iterations, errs[0])
	}
}

// TestReindexRetriesPastBusyTimeout pins crn-wrg0: Reindex must survive a
// writer lock held longer than busy_timeout(5000) instead of hard-failing.
func TestReindexRetriesPastBusyTimeout(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	for _, id := range []string{"a", "b", "c"} {
		body := "+++\nid = \"" + id + "\"\ntitle = \"" + id + "\"\n+++\nbody\n"
		require.NoError(t, os.WriteFile(filepath.Join(store, "global", id+".md"), []byte(body), 0o600))
	}
	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (id,title,body_path) VALUES ('lock','lock','lock')`)
	require.NoError(t, err)

	// Release the lock after 6s -- longer than busy_timeout, shorter than the
	// retry budget. Reindex must wait it out rather than fail at 5s.
	go func() {
		time.Sleep(6 * time.Second)
		_ = tx.Rollback()
	}()

	_, err = Reindex(ctx, store)
	require.NoError(t, err, "Reindex must retry past busy_timeout, not hard-fail")
}

// TestFindRetriesPastBusyTimeout pins crn-ui5i5, the residual half of
// crn-wrg0. #94 gave Reindex's transaction a retry above busy_timeout, but
// Find's hit_count/last_recalled_at UPDATE (entry.go) is the package's only
// other write and was left on the bare 5s window. Reindex holds the writer
// lock for longer than that on a real-sized store -- measured p99=15.7s,
// max=17.1s, with 40 of 192 reindexes exceeding 5s on a 900-entry store under
// 24 concurrent reindexers -- so the losing `cairn get` caller hard-failed
// with "database is locked" while Reindex beside it survived.
//
// The asymmetry was confirmed by an A/B control run: two finder cohorts
// against one store under identical contention, one calling Find's pre-fix
// (unretried) statement shape and one calling the real Find. Unretried failed
// 19/1280, all at the hit_count UPDATE with SQLITE_BUSY at ~4.9s; retried
// failed 0/1280, alongside Reindex's own 0/192.
//
// Same shape as TestReindexRetriesPastBusyTimeout above: hold the writer lock
// for 6s, longer than busy_timeout(5000) but well inside the retry budget.
// Before the fix this failed at exactly 5.02s; after it, Find waits the lock
// out. The concurrent form of this is
// TestConcurrentFindAndReindexDoNotHardFail, whose failure is probabilistic
// (3/140 full-suite runs post-#94), so this deterministic test is what
// actually guards the regression.
func TestFindRetriesPastBusyTimeout(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	body := "+++\nid = \"a\"\ntitle = \"a\"\n+++\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "a.md"), []byte(body), 0o600))
	_, err := Reindex(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (id,title,body_path) VALUES ('lock','lock','lock')`)
	require.NoError(t, err)

	go func() {
		time.Sleep(6 * time.Second)
		_ = tx.Rollback()
	}()

	e, err := Find(ctx, store, "a")
	require.NoError(t, err, "Find's hit_count UPDATE must retry past busy_timeout, not hard-fail")
	// The retried statement must still have applied exactly once: a retry that
	// re-ran a partially-committed UPDATE would show 2 here, not 1.
	assert.Equal(t, 1, e.HitCount, "the retried UPDATE must increment hit_count exactly once")
}

// TestIsBusyClassifiesRealSQLiteBusyError pins isBusy's positive case against
// a genuine driver error rather than a hand-built stand-in: connection A
// holds an immediate write lock; connection B has busy_timeout(0) (no retry
// wait, so the failure is immediate) and a deferred txlock, so its own write
// statement -- not BeginTx -- is what contends for the lock and returns
// SQLITE_BUSY.
func TestIsBusyClassifiesRealSQLiteBusyError(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "busy.sqlite")

	dbA, err := sql.Open("sqlite", dbPath+"?_txlock=immediate")
	require.NoError(t, err)
	defer func() { _ = dbA.Close() }()
	_, err = dbA.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	txA, err := dbA.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = txA.Rollback() }()
	_, err = txA.ExecContext(ctx, `INSERT INTO t (id) VALUES (1)`)
	require.NoError(t, err)

	dbB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(0)")
	require.NoError(t, err)
	defer func() { _ = dbB.Close() }()
	txB, err := dbB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = txB.Rollback() }()
	_, err = txB.ExecContext(ctx, `INSERT INTO t (id) VALUES (2)`)
	require.Error(t, err, "connection B must contend with A's held write lock")

	assert.True(t, isBusy(err), "a real SQLITE_BUSY error must classify as busy: %v", err)
}

// TestIsBusyRejectsNonBusyErrors pins isBusy's negative case: neither a
// non-SQLite sentinel error nor a real driver error of a different class
// (SQL syntax) must be misclassified as lock contention.
func TestIsBusyRejectsNonBusyErrors(t *testing.T) {
	assert.False(t, isBusy(sql.ErrNoRows), "sql.ErrNoRows must not classify as busy")

	ctx := t.Context()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "syntax.sqlite"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `NOT VALID SQL`)
	require.Error(t, err)
	assert.False(t, isBusy(err), "a SQL syntax error must not classify as busy: %v", err)
}
