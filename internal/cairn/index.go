package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/obslog"
	// sqlitedrv registers a pure-Go SQLite driver ("sqlite") for database/sql;
	// its Error type and sqlitelib's result codes are also used directly by
	// isBusy below.
	sqlitedrv "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// entriesSchema covers a fresh index. entries and index_meta persist across
// reindexes (Reindex upserts rather than drops entries, so index-only state
// like hit_count survives a rebuild); entry_tags carries no such state and
// is dropped and recreated wholesale each time.
const entriesSchema = `
CREATE TABLE IF NOT EXISTS entries (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  title_source TEXT,
  summary TEXT,
  summary_source TEXT,
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
  overridden_duplicate_of TEXT,
  review_status TEXT
);
CREATE INDEX IF NOT EXISTS idx_entries_topic ON entries(topic_key);
CREATE TABLE IF NOT EXISTS index_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  indexed_at_commit TEXT,
  schema_version INTEGER
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
	{"title_source", "TEXT"},
	{"summary_source", "TEXT"},
	{"review_status", "TEXT"},
}

// indexSchemaVersion is the shape of the index this binary expects. BUMP IT
// in the same commit as any change to entriesSchema, tagsSchema,
// entriesMigrationCols or the FTS table -- a column added without a bump is
// invisible to every already-built index.
//
// It exists because the git watermark alone cannot detect a schema change.
// indexStale compared indexed_at_commit against HEAD, which answers "did the
// bodies change", not "does this index have the columns this binary selects".
// On upgrade, an index built by the previous release is watermark-current, so
// nothing triggered a rebuild, the ALTER TABLE migrations in reindexOnce
// never ran, and the first read failed hard:
//
//	Error: SQL logic error: no such column: title_source (1)
//
// That is every existing deployment's first command after upgrading, and no
// test caught it because tests build their index from scratch with the
// current binary -- the one case that cannot reproduce it. The regression
// test below builds an index at the old shape on purpose.
const indexSchemaVersion = 3

// indexMetaMigrationCols forward-migrate an index_meta built by an older
// binary, mirroring entriesMigrationCols.
var indexMetaMigrationCols = []struct{ name, def string }{
	{"schema_version", "INTEGER"},
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

	if err := reindexOnce(ctx, db, store, entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}

// reindexChunkSize bounds how many entries' upserts share one write
// transaction during reindexOnce (crn-f0rb7.2). Before this, a single
// mega-transaction held SQLite's write lock for the entire store -- measured
// p99=15.7s/max=17.1s on a 900-entry store under 24 concurrent reindexers
// (see TestFindRetriesPastBusyTimeout's comment) -- so any losing caller
// could queue behind one Reindex call for that long. Splitting into
// independently-committed, independently-retried chunks bounds any single
// transaction's lock hold to roughly this many entries' worth of writes, and
// means a busy failure mid-store only retries the chunk it hit rather than
// redoing every already-committed chunk. It is a var, not a const, so tests
// can shrink it to exercise multi-chunk behavior without needing hundreds of
// fixture files; 150 is a starting point, not a calibrated final answer.
var reindexChunkSize = 150

// reindexStepObserved, when non-nil, is called after each transactional step
// inside reindexOnce (the schema/migration step, each upsert chunk, and the
// finalize step) with how long that step's retryOnBusy call took. It exists
// purely so a test can measure per-transaction lock-hold time directly from
// the code under test, rather than inferring it by racing a polling probe
// against the real transactions -- on a fast, uncontended call the two are
// close, but a probe's sampling interval can miss brief gaps between
// back-to-back chunk commits and undercount them (crn-f0rb7.2). nil in
// production, at zero cost on the hot path.
var reindexStepObserved func(step string, d time.Duration)

// timedRetryOnBusy wraps retryOnBusy with reindexStepObserved's timing hook.
// The duration covers the whole retryOnBusy call including any retries, so
// under real contention it is not a pure lock-hold measurement -- only
// meaningful for reasoning about lock-hold time on an uncontended call, which
// is what the one test using the hook relies on.
func timedRetryOnBusy(ctx context.Context, step string, fn func() error) error {
	start := time.Now()
	err := retryOnBusy(ctx, fn)
	if reindexStepObserved != nil {
		reindexStepObserved(step, time.Since(start))
	}
	return err
}

// reindexOnce rebuilds the index for one Reindex call. Unlike the old
// single-mega-transaction shape, each step below is its own
// retryOnBusy-wrapped transaction: a busy failure partway through only
// retries the step it hit, not the whole store from scratch.
func reindexOnce(ctx context.Context, db *sql.DB, store string, entries []*Entry) error {
	// Schema/migration/entry_tags DROP+CREATE stay a single short leading
	// transaction, unchanged from before this bead -- it's already fast (a
	// handful of DDL statements, not one per entry) and not the bottleneck
	// the chunking below addresses. See its own comment for why these
	// specific statements must run inside a tx rather than as autocommit
	// statements (crn-t42e, crn-j3k4).
	if err := timedRetryOnBusy(ctx, "schema", func() error {
		return reindexSchemaTx(ctx, db)
	}); err != nil {
		return err
	}

	// Read before any chunk below writes, so the diff against entries (the
	// caller's fresh list) below is "what the table had going into this
	// reindex" -- exactly what the old current_ids TEMP TABLE approach also
	// compared against, just computed as an in-memory Go set instead of a
	// SQL side-table (crn-f0rb7.2).
	existingIDs, err := existingEntryIDs(ctx, db)
	if err != nil {
		return err
	}

	for _, batch := range chunkEntries(entries, reindexChunkSize) {
		b := batch
		if err := timedRetryOnBusy(ctx, "chunk", func() error {
			return reindexUpsertChunkTx(ctx, db, b)
		}); err != nil {
			return err
		}
	}

	currentIDs := make(map[string]bool, len(entries))
	for _, e := range entries {
		currentIDs[e.ID] = true
	}
	var missing []string
	for id := range existingIDs {
		if !currentIDs[id] {
			missing = append(missing, id)
		}
	}

	return timedRetryOnBusy(ctx, "finalize", func() error {
		return reindexFinalizeTx(ctx, db, store, entries, missing)
	})
}

// reindexSchemaTx creates/migrates entries and drops+recreates entry_tags --
// see reindexOnce's comment for why this stays one small unchanged
// transaction rather than being folded into the chunked upserts below.
func reindexSchemaTx(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
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
		return err
	}
	for _, col := range entriesMigrationCols {
		if err := addColumnIfMissing(ctx, tx, "entries", col.name, col.def); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, col := range indexMetaMigrationCols {
		if err := addColumnIfMissing(ctx, tx, "index_meta", col.name, col.def); err != nil {
			_ = tx.Rollback()
			return err
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
		return err
	}
	if _, err := tx.ExecContext(ctx, tagsSchema); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// existingEntryIDs reads the id column of entries as it stands before this
// reindex's chunked upserts run -- reindexOnce's baseline for computing which
// ids fell out of the fresh entries list (deleted/renamed source files).
// Called after reindexSchemaTx, so entries is guaranteed to exist (possibly
// empty) even on a cold store.
func existingEntryIDs(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM entries`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// chunkEntries splits entries into slices of at most size, preserving order.
// A size <= 0 is only reachable if reindexChunkSize is misconfigured (never
// true for the production default or any test override in this package), so
// it isn't special-cased; that call would simply loop forever, same as any
// other zero-length step bug would.
func chunkEntries(entries []*Entry, size int) [][]*Entry {
	var chunks [][]*Entry
	for i := 0; i < len(entries); i += size {
		end := min(i+size, len(entries))
		chunks = append(chunks, entries[i:end])
	}
	return chunks
}

// reindexUpsertChunkTx upserts one chunk's entries -- and their scope tags --
// in its own transaction, so a busy failure here (retried by reindexOnce's
// caller) only redoes this chunk, not every chunk already committed before
// it (crn-f0rb7.2).
func reindexUpsertChunkTx(ctx context.Context, db *sql.DB, batch []*Entry) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, e := range batch {
		anchorPaths, err := json.Marshal(e.Anchor.Paths)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		autoActionable := 0
		if e.AutoActionable {
			autoActionable = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entries (
				id, title, title_source, summary, summary_source, type, topic_key, body_path,
				anchor_type, anchor_repo, anchor_paths, anchor_spec, anchor_fingerprint,
				verified_at, created_by, created_at, hit_count,
				kind, auto_actionable, recurrence_count, promoted_bead_id, last_recalled_at,
				overridden_duplicate_of, review_status
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				title=excluded.title, title_source=excluded.title_source,
				summary=excluded.summary, summary_source=excluded.summary_source, type=excluded.type,
				topic_key=excluded.topic_key, body_path=excluded.body_path,
				anchor_type=excluded.anchor_type, anchor_repo=excluded.anchor_repo,
				anchor_paths=excluded.anchor_paths, anchor_spec=excluded.anchor_spec,
				anchor_fingerprint=excluded.anchor_fingerprint,
				verified_at=excluded.verified_at, created_by=excluded.created_by,
				created_at=excluded.created_at,
				overridden_duplicate_of=excluded.overridden_duplicate_of,
				review_status=excluded.review_status`,
			// hit_count, kind, auto_actionable, recurrence_count, promoted_bead_id,
			// and last_recalled_at are deliberately not in the UPDATE SET: like
			// hit_count (crn-6az.6.1.1), they're index-only state a future call
			// site writes directly via SQL (crn-28ge.1.1), so a reindex must not
			// stamp a surviving row back to the body's stale seed value.
			// overridden_duplicate_of and review_status are body-sourced (like
			// created_at), not index-only, so unlike those they ARE in the UPDATE
			// SET -- a --force correction or a review-status flip edited into the
			// body must overwrite a stale indexed copy.
			e.ID, e.Title, e.TitleSource, e.Summary, e.SummarySource, e.Type, e.TopicKey, e.BodyPath,
			e.Anchor.Type, e.Anchor.Repo, string(anchorPaths), e.Anchor.Spec, e.Anchor.Fingerprint,
			e.VerifiedAt, e.CreatedBy, e.CreatedAt, e.HitCount,
			e.Kind, autoActionable, e.RecurrenceCount, e.PromotedBeadID, e.LastRecalledAt,
			e.OverriddenDuplicateOf, e.ReviewStatus,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		for _, tag := range e.Scope {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags(entry_id,tag) VALUES (?,?)`, e.ID, tag); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

// reindexFinalizeTx rebuilds entries_fts from the fresh entry list, drops
// entries whose source file is gone, and stamps the index_meta watermark, as
// the last transaction of a Reindex call. missing is reindexOnce's
// precomputed diff (existing ids not in the fresh entries list) rather than a
// SQL NOT IN over every current id, so this DELETE scales with how many
// entries actually disappeared, not with store size.
//
// entries_fts is rebuilt here from the same full entries list reindexOnce
// upserted (across possibly several chunk transactions) into entries, rather
// than per-chunk: rebuildSearchIndexTx drops and repopulates the whole table
// in one shot, and doing that once per Reindex call -- in the same
// transaction as the watermark stamp below -- mirrors the old
// single-transaction reindexTx's invariant that the full-text index can never
// disagree with what the store currently contains (crn-f0rb7.2 split the
// per-entry upserts into per-chunk transactions, but a full-list rebuild like
// this one doesn't chunk, so it stays here rather than moving into
// reindexUpsertChunkTx).
//
// The watermark update is deliberately the last statement of the last
// transaction: ensureFresh's "stale never served as fresh" guarantee (NFR-3,
// crn-t250) depends on the commit stamped here only ever describing a fully
// applied reindex.
func reindexFinalizeTx(ctx context.Context, db *sql.DB, store string, entries []*Entry, missing []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := rebuildSearchIndexTx(ctx, tx, entries); err != nil {
		_ = tx.Rollback()
		return err
	}
	if len(missing) > 0 {
		placeholders := strings.Repeat("?,", len(missing))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(missing))
		for i, id := range missing {
			args[i] = id
		}
		//nolint:gosec // placeholders is a repeated "?,", never interpolated data
		if _, err := tx.ExecContext(ctx, `DELETE FROM entries WHERE id IN (`+placeholders+`)`, args...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	// commit is "" if store isn't a git repo (yet), has no commits, or git
	// couldn't be invoked -- this function's own correctness doesn't depend on
	// which; indexStale (not this stamp) is what must distinguish "confirmed
	// non-git" from "invocation error" to avoid silently under-detecting
	// staleness (crn-t250).
	commit, _, _ := git(ctx, store, "rev-parse", "HEAD")
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO index_meta (id, indexed_at_commit, schema_version) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   indexed_at_commit = excluded.indexed_at_commit,
		   schema_version = excluded.schema_version`,
		commit, indexSchemaVersion,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// isBusy reports whether err is SQLite's lock-contention error. The primary
// result code is the low 8 bits, so this also catches the extended forms
// (SQLITE_BUSY_SNAPSHOT 517, SQLITE_BUSY_RECOVERY 261).
func isBusy(err error) bool {
	var serr *sqlitedrv.Error
	return errors.As(err, &serr) && serr.Code()&0xFF == sqlitelib.SQLITE_BUSY
}

// retryOnBusy runs fn, retrying only while it fails with isBusy. Each
// attempt already spends up to openDB's busy_timeout(5000) waiting inside
// SQLite itself, so this loop exists purely for contention that outlasts a
// single busy_timeout window -- a writer lock held across several such
// windows under fleet-scale contention (crn-wrg0). Backoff is exponential
// with jitter so a queue of waiters spreads its retries instead of all
// waking in lockstep and re-losing the race together.
func retryOnBusy(ctx context.Context, fn func() error) error {
	const (
		maxAttempts = 6
		baseDelay   = 100 * time.Millisecond
	)
	var err error
	for attempt := range maxAttempts {
		if attempt > 0 {
			delay := baseDelay << (attempt - 1)
			jitter := time.Duration(rand.Float64()*float64(delay)) - delay/2 //nolint:gosec // jitter is timing-only, not security-sensitive
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay + jitter):
			}
		}
		err = fn()
		if err == nil || !isBusy(err) {
			return err
		}
	}
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

// selfHealReindexTimeout bounds ensureFreshWith's synchronous self-heal
// reindex when the index merely lags HEAD (staleBehindHEAD): without it, a
// writer holding the entries lock forces the caller through retryOnBusy's
// full ~33s worst case (6 attempts x up to 5s busy_timeout each, plus
// backoff) (crn-f0rb7). It does not apply to a cold store
// (staleNoWatermark) -- see the branch in ensureFreshWith below.
const selfHealReindexTimeout = 6 * time.Second

// staleReason distinguishes indexStale's two "stale" cases so
// ensureFreshWith can decide whether selfHealReindexTimeout applies.
type staleReason int

const (
	staleNone staleReason = iota
	// staleNoWatermark: no index built yet, or a partial one left behind by
	// an interrupted Reindex. There's no existing index to fall back to, so
	// this case must run to completion under the old unbounded retryOnBusy
	// budget rather than being capped.
	staleNoWatermark
	// staleBehindHEAD: an index exists but a body edit landed outside
	// cairn's own write path. Bounded by selfHealReindexTimeout.
	staleBehindHEAD
	// staleSchemaVersion: an index exists and matches HEAD, but was built by
	// a binary with a different index shape. Rebuilt in full rather than
	// under the bounded budget staleBehindHEAD uses -- a body edit is a small
	// delta, a schema change is not.
	staleSchemaVersion
)

// ensureFreshWith takes the reindex step as a parameter so tests can count
// invocations of it directly, since ensureFresh's "no needless reindex"
// contract can only be verified by call count, not by inspecting state.
func ensureFreshWith(ctx context.Context, store string, reindex func(context.Context, string) (int, error)) error {
	start := time.Now()
	stale, reason, err := indexStale(ctx, store)
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

	bounded := reason == staleBehindHEAD
	reindexCtx := ctx
	if bounded {
		var cancel context.CancelFunc
		reindexCtx, cancel = context.WithTimeout(ctx, selfHealReindexTimeout)
		defer cancel()
	}

	count, reindexErr := reindex(reindexCtx, store)
	fields := obslog.IndexDriftFields{
		Stale:          true,
		Reindexed:      reindexErr == nil,
		ReindexCount:   count,
		DurationMS:     time.Since(start).Milliseconds(),
		BoundedBudget:  bounded,
		BudgetExceeded: bounded && errors.Is(reindexErr, context.DeadlineExceeded),
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
// Its signature stays (bool, error): the staleReason distinction inside
// indexStale exists only to let ensureFreshWith bound its self-heal
// reindex, not to change doctor.go's output contract (NFR-3).
func IndexStale(ctx context.Context, store string) (bool, error) {
	stale, _, err := indexStale(ctx, store)
	return stale, err
}

func indexStale(ctx context.Context, store string) (bool, staleReason, error) {
	db, err := openDB(store)
	if err != nil {
		return false, staleNone, err
	}
	defer func() { _ = db.Close() }()

	// Checked before the watermark: a schema mismatch must force a rebuild
	// even when the bodies have not moved, which is exactly the upgrade case.
	// A missing schema_version COLUMN (not just a null value) means the index
	// predates versioning entirely, which is equally stale -- so any error
	// here is treated as a mismatch rather than propagated.
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT schema_version FROM index_meta WHERE id = 1`).Scan(&version); err != nil ||
		!version.Valid || version.Int64 != indexSchemaVersion {
		// Fall through to the watermark probe only if the row is absent
		// entirely; otherwise report the schema mismatch.
		var exists int
		if probeErr := db.QueryRowContext(ctx,
			`SELECT count(*) FROM index_meta WHERE id = 1`).Scan(&exists); probeErr == nil && exists > 0 {
			return true, staleSchemaVersion, nil
		}
	}

	var indexed string
	err = db.QueryRowContext(ctx, `SELECT indexed_at_commit FROM index_meta WHERE id = 1`).Scan(&indexed)
	if err != nil {
		// No watermark row -- a brand-new store (index.sqlite/index_meta don't
		// exist yet) or a partially-built index left behind by an interrupted
		// Reindex. Nothing to trust either way, so per NFR-3 ("stale never
		// served as fresh") the safe default is stale.
		return true, staleNoWatermark, nil
	}

	head, ok, err := git(ctx, store, "rev-parse", "HEAD")
	if err != nil {
		return false, staleNone, err
	}
	if !ok {
		return false, staleNone, nil
	}
	if indexed != head {
		return true, staleBehindHEAD, nil
	}
	return false, staleNone, nil
}
