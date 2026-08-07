package cairn

import (
	"context"
	"time"
)

// CullCandidateFinding is one entry disused past the configured threshold
// (FR-10/NFR-06): DisusedSince is LastRecalledAt if the entry was ever
// recalled, else CreatedAt.
type CullCandidateFinding struct {
	EntryID        string   `json:"entry_id"`
	TopicKey       string   `json:"topic_key,omitempty"`
	Scope          []string `json:"scope,omitempty"`
	LastRecalledAt string   `json:"last_recalled_at,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
	DisusedSince   string   `json:"disused_since"`
}

func disuseReference(lastRecalledAt, createdAt string) (t time.Time, ok bool) {
	if lastRecalledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, lastRecalledAt); err == nil {
			return parsed, true
		}
	}
	if createdAt != "" {
		// RFC3339 first: NewEntry has stamped created_at with it since
		// crn-3476/crn-zcxq FR-5. DateOnly stays as a fallback so entries
		// written to disk before that change (still legitimately on disk,
		// unrewritten by a reindex) don't silently stop aging into
		// cull-eligibility.
		if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse(time.DateOnly, createdAt); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// CullCandidates reports entries whose disuse (LastRecalledAt, falling back
// to CreatedAt if never recalled) exceeds disuseAfter (NFR-06). This is
// deliberately independent of FRESHNESS (anchor-drift, Check()) per FR-10:
// the query below never selects verified_at or any anchor_* column, so an
// entry's freshness state cannot leak into the disuse decision.
func CullCandidates(ctx context.Context, store string, disuseAfter time.Duration) ([]CullCandidateFinding, error) {
	if err := ensureFresh(ctx, store); err != nil {
		return nil, err
	}
	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	tags, err := scopeTags(ctx, db)
	if err != nil {
		return nil, err
	}

	// COALESCE the nullable columns -- see the note in recall.go's
	// loadEntryRecallRows. Rows predating an ALTER TABLE ADD COLUMN hold NULL,
	// and a plain string scan target aborts the entire sweep.
	rows, err := db.QueryContext(ctx, `SELECT
		id,
		COALESCE(topic_key, ''),
		COALESCE(last_recalled_at, ''),
		COALESCE(created_at, '')
		FROM entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	cutoff := time.Now().Add(-disuseAfter)
	var findings []CullCandidateFinding
	for rows.Next() {
		var id, topicKey, lastRecalledAt, createdAt string
		if err := rows.Scan(&id, &topicKey, &lastRecalledAt, &createdAt); err != nil {
			return nil, err
		}
		ref, ok := disuseReference(lastRecalledAt, createdAt)
		if !ok || ref.After(cutoff) {
			continue
		}
		findings = append(findings, CullCandidateFinding{
			EntryID:        id,
			TopicKey:       topicKey,
			Scope:          tags[id],
			LastRecalledAt: lastRecalledAt,
			CreatedAt:      createdAt,
			DisusedSince:   ref.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return findings, nil
}
