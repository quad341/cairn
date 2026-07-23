package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
// entry_tags DDL race this bead fixes. Racing concurrent Reindex calls
// against a store where index.sqlite does not exist yet hits a separate,
// still-open intermittent SQLITE_BUSY gap during first-time file/WAL
// creation (crn-t42e) -- a different, narrower failure mode that doesn't
// belong in this test either.
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
