package cairn

import "context"

// RecallStatsFinding is one entry's recall signal (FR-08): HitCount and
// LastRecalledAt as stamped by Find on a hit (crn-28ge.1.5). Every entry in
// the store is reported, including one never recalled (HitCount 0,
// LastRecalledAt "") -- this is a report, not a filter.
type RecallStatsFinding struct {
	EntryID        string `json:"entry_id"`
	TopicKey       string `json:"topic_key,omitempty"`
	HitCount       int    `json:"hit_count"`
	LastRecalledAt string `json:"last_recalled_at,omitempty"`
}

// PromoteCandidateFinding is one entry recurring at least threshold times
// (crn-28ge.1.4's RecurrenceCount) that has not yet been promoted to a
// durable bead (PromotedBeadID == ""; NFR-02 idempotency -- once promoted, an
// entry never reappears here). AnchorRepo is always populated so a
// downstream consumer (the librarian formula's auto-file step, crn-28ge.1.8)
// can file the resulting bead in the entry's own repo/tracker rather than
// defaulting to wherever the librarian happens to run (FR-07).
type PromoteCandidateFinding struct {
	EntryID         string `json:"entry_id"`
	TopicKey        string `json:"topic_key,omitempty"`
	RecurrenceCount int    `json:"recurrence_count"`
	AnchorRepo      string `json:"anchor_repo"`
}

// entryRecallRow is the index columns both RecallStats and PromoteCandidates
// need. One shared row shape, so a future third consumer extends
// loadEntryRecallRows instead of hand-rolling its own SELECT (NFR-05).
type entryRecallRow struct {
	id              string
	topicKey        string
	hitCount        int
	lastRecalledAt  string
	recurrenceCount int
	promotedBeadID  string
	anchorRepo      string
}

// loadEntryRecallRows is the shared read behind RecallStats and
// PromoteCandidates (NFR-05: one query serves both, rather than each
// re-deriving its own and silently drifting). Like Status, it reads index
// columns only -- no body parse, no hit_count/last_recalled_at side effect.
func loadEntryRecallRows(ctx context.Context, store string) ([]entryRecallRow, error) {
	if err := ensureFresh(ctx, store); err != nil {
		return nil, err
	}
	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, `SELECT
		id, topic_key, hit_count, last_recalled_at, recurrence_count, promoted_bead_id, anchor_repo
		FROM entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []entryRecallRow
	for rows.Next() {
		var r entryRecallRow
		if err := rows.Scan(
			&r.id, &r.topicKey, &r.hitCount, &r.lastRecalledAt,
			&r.recurrenceCount, &r.promotedBeadID, &r.anchorRepo,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RecallStats reports every entry's recall signal (FR-08). Strictly
// read-only: never mutates the store, never files a bead.
func RecallStats(ctx context.Context, store string) ([]RecallStatsFinding, error) {
	rows, err := loadEntryRecallRows(ctx, store)
	if err != nil {
		return nil, err
	}
	var findings []RecallStatsFinding
	for _, r := range rows {
		findings = append(findings, RecallStatsFinding{
			EntryID:        r.id,
			TopicKey:       r.topicKey,
			HitCount:       r.hitCount,
			LastRecalledAt: r.lastRecalledAt,
		})
	}
	return findings, nil
}

// PromoteCandidates reports entries eligible for promotion to a durable bead
// (FR-07): RecurrenceCount >= threshold AND PromotedBeadID == "" (NFR-02
// idempotency). Strictly read-only -- the actual bd create happens
// downstream, in the librarian formula's auto-file step (crn-28ge.1.8).
func PromoteCandidates(ctx context.Context, store string, threshold int) ([]PromoteCandidateFinding, error) {
	rows, err := loadEntryRecallRows(ctx, store)
	if err != nil {
		return nil, err
	}
	var findings []PromoteCandidateFinding
	for _, r := range rows {
		if r.recurrenceCount < threshold || r.promotedBeadID != "" {
			continue
		}
		findings = append(findings, PromoteCandidateFinding{
			EntryID:         r.id,
			TopicKey:        r.topicKey,
			RecurrenceCount: r.recurrenceCount,
			AnchorRepo:      r.anchorRepo,
		})
	}
	return findings, nil
}
