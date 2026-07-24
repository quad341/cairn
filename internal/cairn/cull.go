package cairn

import (
	"context"
	"time"
)

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

	rows, err := db.QueryContext(ctx, `SELECT id, topic_key, last_recalled_at, created_at FROM entries ORDER BY id`)
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
