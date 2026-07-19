package cairn

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	// modernc.org/sqlite registers a pure-Go SQLite driver ("sqlite") for database/sql.
	_ "modernc.org/sqlite"
)

const schema = `
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
CREATE TABLE entry_tags (
  entry_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY (entry_id, tag)
);
CREATE INDEX idx_tags_tag ON entry_tags(tag);
CREATE INDEX idx_entries_topic ON entries(topic_key);
`

// IndexPath is the rebuildable SQLite index (gitignored; not source of truth).
func IndexPath(store string) string {
	return filepath.Join(store, "index", "cairn.sqlite")
}

func openDB(store string) (*sql.DB, error) {
	p := IndexPath(store)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", p)
}

// Reindex drops and rebuilds the index from the bodies. It returns the entry count.
func Reindex(ctx context.Context, store string) (int, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return 0, err
	}
	db, err := openDB(store)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS entries; DROP TABLE IF EXISTS entry_tags;`); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO entries
			 (id,title,summary,type,topic_key,body_path,anchor_type,anchor_fingerprint,verified_at,created_by,hit_count)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, e.Title, e.Summary, e.Type, e.TopicKey, e.BodyPath,
			e.Anchor.Type, e.Anchor.Fingerprint, e.VerifiedAt, e.CreatedBy, e.HitCount,
		); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		for _, tag := range e.Scope {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_tags(entry_id,tag) VALUES (?,?)`, e.ID, tag); err != nil {
				_ = tx.Rollback()
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(entries), nil
}
