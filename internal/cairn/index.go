package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/obslog"
	// modernc.org/sqlite registers a pure-Go SQLite driver ("sqlite") for database/sql.
	_ "modernc.org/sqlite"
)

// entriesSchema covers a fresh index. entries and index_meta persist across
// reindexes (Reindex upserts rather than drops entries, so index-only state
// like hit_count survives a rebuild); entry_tags carries no such state and
// is dropped and recreated wholesale each time.
const entriesSchema = `
CREATE TABLE IF NOT EXISTS entries (
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
  hit_count INTEGER DEFAULT 0,
  kind TEXT,
  auto_actionable INTEGER,
  recurrence_count INTEGER DEFAULT 0,
  promoted_bead_id TEXT,
  last_recalled_at TEXT,
  overridden_duplicate_of TEXT
);
CREATE INDEX IF NOT EXISTS idx_entries_topic ON entries(topic_key);
CREATE TABLE IF NOT EXISTS index_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  indexed_at_commit TEXT
);
`

const tagsSchema = `
CREATE TABLE entry_tags (
  entry_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY (entry_id, tag)
);
CREATE INDEX idx_tags_tag ON entry_tags(tag);
`

// entriesMigrationCols are columns added to entries after its initial
// release. entriesSchema's CREATE TABLE IF NOT EXISTS covers a fresh index;
// these forward-migrate an index.sqlite built by an older binary version.
var entriesMigrationCols = []struct{ name, def string }{
	{"anchor_repo", "TEXT"},
	{"anchor_paths", "TEXT"},
	{"anchor_spec", "TEXT"},
	{"created_at", "TEXT"},
	{"kind", "TEXT"},
	{"auto_actionable", "INTEGER"},
	{"recurrence_count", "INTEGER DEFAULT 0"},
	{"promoted_bead_id", "TEXT"},
	{"last_recalled_at", "TEXT"},
	{"overridden_duplicate_of", "TEXT"},
}

// IndexPath is the rebuildable SQLite index (gitignored; not source of truth).
func IndexPath(store string) string {
	return filepath.Join(store, "index", "cairn.sqlite")
}

func openDB(store string) (*sql.DB, error) {
	p := IndexPath(store)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, err
	}
	// busy_timeout+WAL: CAIRN_STORE defaults to one shared path across the
	// whole agent fleet, so concurrent CLI invocations routinely race on
	// ensureFresh's synchronous Reindex; without these, the loser of the
	// race gets a hard "database is locked" failure instead of waiting
	// (crn-t250). busy_timeout is applied first by the driver regardless of
	// DSN param order.
	//
	// txlock=immediate: the only db.BeginTx in this package is Reindex's
	// write transaction, which never reads before its first write. Left on
	// the driver default ("deferred"), that transaction's write lock is
	// acquired lazily at its first write statement rather than at BEGIN;
	// under concurrent Reindex calls that upgrade can itself return
	// SQLITE_BUSY without honoring busy_timeout's retry loop, still
	// surfacing a hard failure despite the pragma above (crn-j3k4).
	// "immediate" acquires the write lock at BEGIN, where busy_timeout's
	// retry does apply.
	return sql.Open("sqlite", p+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
}

// Reindex rebuilds the index from the bodies. It returns the entry count.
func Reindex(ctx context.Context, store string) (int, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return 0, err
	}
	return ReindexEntries(ctx, store, entries)
}

// ReindexEntries is Reindex's body factored out to take an already-gathered
// entry list, so a caller that needs a tolerant walk (doctor.go's
// IterEntriesTolerant) can rebuild the index from it without re-triggering
// IterEntries' own abort-on-first-parse-error walk (OQ5/OQ3). Reindex itself
// is unchanged for its existing callers -- this is purely a factoring.
func ReindexEntries(ctx context.Context, store string, entries []*Entry) (int, error) {
	db, err := openDB(store)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	// entriesSchema and the column migrations run INSIDE this tx, not as
	// autocommit statements before it (crn-t42e). openDB's DSN sets
	// _txlock=immediate, which takes the write lock at BEGIN -- the one place
	// busy_timeout's retry loop actually applies. An autocommit CREATE TABLE
	// upgrades from a read lock to a write lock mid-statement instead, and
	// SQLite answers that with an immediate SQLITE_BUSY rather than invoking
	// the busy handler, because retrying a lock upgrade can deadlock. That is
	// why the old code failed *instantly* instead of after the 5000ms
	// busy_timeout: the timeout was never consulted.
	//
	// Only reachable on a cold store, where several processes race to create
	// index.sqlite at once -- ensureFresh treats "no watermark row" as stale
	// and reindexes synchronously, so a fleet cold-start put every agent on
	// this line simultaneously. Measured before the move: 1 failure per 80
	// concurrent Reindex calls, ~13% of stress runs.
	if _, err := tx.ExecContext(ctx, entriesSchema); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	for _, col := range entriesMigrationCols {
		if err := addColumnIfMissing(ctx, tx, "entries", col.name, col.def); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}
	// entry_tags carries no index-only state worth preserving across a
	// rebuild (unlike entries -- see entriesSchema's comment), so it's
	// dropped and recreated wholesale each reindex. Both statements run
	// inside this tx rather than as separate autocommit statements, so two
	// concurrent Reindex() calls fully serialize on SQLite's single-writer
	// lock instead of interleaving their DROP/CREATE and one of them hitting
	// "table entry_tags already exists" (crn-j3k4).
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS entry_tags;`); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, tagsSchema); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := reindexTx(ctx, tx, store, entries); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(entries), nil
}

// reindexTx does the per-entry upsert, drops entries whose source file is
// gone, and stamps the index_meta watermark, all inside the caller's tx.
func reindexTx(ctx context.Context, tx *sql.Tx, store string, entries []*Entry) error {
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE current_ids (id TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	for _, e := range entries {
		anchorPaths, err := json.Marshal(e.Anchor.Paths)
		if err != nil {
			return err
		}
		autoActionable := 0
		if e.AutoActionable {
			autoActionable = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entries (
				id, title, summary, type, topic_key, body_path,
				anchor_type, anchor_repo, anchor_paths, anchor_spec, anchor_fingerprint,
				verified_at, created_by, created_at, hit_count,
				kind, auto_actionable, recurrence_count, promoted_bead_id, last_recalled_at,
				overridden_duplicate_of
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				title=excluded.title, summary=excluded.summary, type=excluded.type,
				topic_key=excluded.topic_key, body_path=excluded.body_path,
				anchor_type=excluded.anchor_type, anchor_repo=excluded.anchor_repo,
				anchor_paths=excluded.anchor_paths, anchor_spec=excluded.anchor_spec,
				anchor_fingerprint=excluded.anchor_fingerprint,
				verified_at=excluded.verified_at, created_by=excluded.created_by,
				created_at=excluded.created_at,
				overridden_duplicate_of=excluded.overridden_duplicate_of`,
			// hit_count, kind, auto_actionable, recurrence_count, promoted_bead_id,
			// and last_recalled_at are deliberately not in the UPDATE SET: like
			// hit_count (crn-6az.6.1.1), they're index-only state a future call
			// site writes directly via SQL (crn-28ge.1.1), so a reindex must not
			// stamp a surviving row back to the body's stale seed value.
			// overridden_duplicate_of is body-sourced (like created_at), not
			// index-only, so unlike those it IS in the UPDATE SET -- a --force
			// correction edited into the body must overwrite a stale indexed copy.
			e.ID, e.Title, e.Summary, e.Type, e.TopicKey, e.BodyPath,
			e.Anchor.Type, e.Anchor.Repo, string(anchorPaths), e.Anchor.Spec, e.Anchor.Fingerprint,
			e.VerifiedAt, e.CreatedBy, e.CreatedAt, e.HitCount,
			e.Kind, autoActionable, e.RecurrenceCount, e.PromotedBeadID, e.LastRecalledAt,
			e.OverriddenDuplicateOf,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO current_ids(id) VALUES (?)`, e.ID); err != nil {
			return err
		}
		for _, tag := range e.Scope {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags(entry_id,tag) VALUES (?,?)`, e.ID, tag); err != nil {
				return err
			}
		}
	}
	// entries no longer backed by a body (deleted/renamed) don't belong in a
	// rebuilt index -- the ON CONFLICT upsert above only ever adds or
	// refreshes rows, so without this they'd linger forever.
	if _, err := tx.ExecContext(ctx, `DELETE FROM entries WHERE id NOT IN (SELECT id FROM current_ids)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE current_ids`); err != nil {
		return err
	}

	// commit is "" if store isn't a git repo (yet), has no commits, or git
	// couldn't be invoked -- reindexTx's own correctness doesn't depend on
	// which; indexStale (not this stamp) is what must distinguish "confirmed
	// non-git" from "invocation error" to avoid silently under-detecting
	// staleness (crn-t250).
	commit, _, _ := git(ctx, store, "rev-parse", "HEAD")
	_, err := tx.ExecContext(ctx,
		`INSERT INTO index_meta (id, indexed_at_commit) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET indexed_at_commit = excluded.indexed_at_commit`,
		commit,
	)
	return err
}

// addColumnIfMissing adds a column to an existing table, tolerating the case
// where it's already present -- SQLite's ADD COLUMN has no IF NOT EXISTS
// clause portable across the versions cairn might run against.
// execer is the ExecContext half of *sql.DB and *sql.Tx, so schema helpers
// can run either standalone or inside a caller's transaction. Reindex needs
// the tx form: see the _txlock=immediate note in ReindexEntries (crn-t42e).
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func addColumnIfMissing(ctx context.Context, db execer, table, column, def string) error {
	// table/column/def are always our own compile-time literals (entriesMigrationCols
	// above), never user input, so building the DDL string is safe despite the shape
	// gosec's G201 flags.
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, def) //nolint:gosec
	_, err := db.ExecContext(ctx, stmt)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}

// ensureFresh self-heals the index for reads that depend on it: it compares
// the store's current git HEAD against index_meta's watermark and
// synchronously reindexes on any mismatch or unreadable watermark (including
// "no index built yet"), so a body edit committed outside cairn's own write
// path can never be served as fresh from a stale index (crn-6az.6.1.2). On a
// store that isn't a git repo (or has no commits), there's no HEAD to
// compare against; once an index exists there it's treated as fresh rather
// than rebuilt on every call.
func ensureFresh(ctx context.Context, store string) error {
	return ensureFreshWith(ctx, store, Reindex)
}

// ensureFreshWith takes the reindex step as a parameter so tests can count
// invocations of it directly, since ensureFresh's "no needless reindex"
// contract can only be verified by call count, not by inspecting state.
func ensureFreshWith(ctx context.Context, store string, reindex func(context.Context, string) (int, error)) error {
	start := time.Now()
	stale, err := indexStale(ctx, store)
	if err != nil {
		return err
	}
	if !stale {
		obslog.FromContext(ctx).IndexDrift(obslog.IndexDriftFields{
			Stale:      false,
			DurationMS: time.Since(start).Milliseconds(),
		})
		return nil
	}

	count, reindexErr := reindex(ctx, store)
	fields := obslog.IndexDriftFields{
		Stale:        true,
		Reindexed:    reindexErr == nil,
		ReindexCount: count,
		DurationMS:   time.Since(start).Milliseconds(),
	}
	if reindexErr != nil {
		fields.ReindexError = redactSecrets(reindexErr.Error())
	}
	obslog.FromContext(ctx).IndexDrift(fields)
	return reindexErr
}

// IndexStale reports whether the index's watermark commit is behind the
// store's current git HEAD (or missing/unbuilt) -- an exported wrapper
// around indexStale so doctor.go's index-drift check can call it (OQ5).
func IndexStale(ctx context.Context, store string) (bool, error) {
	return indexStale(ctx, store)
}

func indexStale(ctx context.Context, store string) (bool, error) {
	db, err := openDB(store)
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()

	var indexed string
	err = db.QueryRowContext(ctx, `SELECT indexed_at_commit FROM index_meta WHERE id = 1`).Scan(&indexed)
	if err != nil {
		// No watermark row -- a brand-new store (index.sqlite/index_meta don't
		// exist yet) or a partially-built index left behind by an interrupted
		// Reindex. Nothing to trust either way, so per NFR-3 ("stale never
		// served as fresh") the safe default is stale.
		return true, nil
	}

	head, ok, err := git(ctx, store, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return indexed != head, nil
}
