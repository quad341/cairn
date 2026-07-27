package cairn

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecallReadsTolerateNullIndexColumns pins the migrated-store case that
// every from-scratch test misses. hit_count, last_recalled_at,
// recurrence_count, promoted_bead_id and anchor_repo were added to an existing
// index by ALTER TABLE ADD COLUMN with no DEFAULT, so every pre-migration row
// holds NULL. A plain string scan target then fails with "converting NULL to
// string is unsupported" and takes the whole report down.
//
// Reindex does not clear those NULLs and a fresh store never creates them,
// which is why the unit suite was green while all three commands were dead on
// the live store: 11 of 19 rows had a NULL last_recalled_at, 15 of 19 a NULL
// promoted_bead_id, and recall-stats, promote-candidates and cull-candidates
// all aborted.
func TestRecallReadsTolerateNullIndexColumns(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	// Build the index, then age it into the migrated shape.
	_, err := RecallStats(ctx, store)
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE entries SET
		topic_key = NULL, hit_count = NULL, last_recalled_at = NULL,
		recurrence_count = NULL, promoted_bead_id = NULL, anchor_repo = NULL,
		created_at = NULL`)
	require.NoError(t, err)

	// Guard: if a reindex silently repaired the row this test proves nothing.
	var nulls int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM entries WHERE last_recalled_at IS NULL").Scan(&nulls))
	require.Equal(t, 1, nulls, "the NULL must survive to the read under test")
	require.NoError(t, db.Close())

	stats, err := RecallStats(ctx, store)
	require.NoError(t, err, "recall-stats must survive NULL index columns (FR-08)")
	require.Len(t, stats, 1)
	assert.Equal(t, RecallStatsFinding{EntryID: "a", HitCount: 0, LastRecalledAt: ""}, stats[0],
		"a never-recalled entry reports empty string, per RecallStatsFinding's contract")

	_, err = PromoteCandidates(ctx, store, 1)
	require.NoError(t, err, "promote-candidates shares the same loader (FR-07)")

	_, err = CullCandidates(ctx, store, 0)
	require.NoError(t, err, "cull-candidates runs its own query over the same columns (FR-10)")
}

func TestRecallStatsReportsHitCountAndLastRecalledAt(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\n"+
		"id = \"a\"\n"+
		"title = \"A\"\n"+
		"topic_key = \"topic-a\"\n"+
		"hit_count = 5\n"+
		"last_recalled_at = \"2026-07-20T00:00:00Z\"\n"+
		"+++\n"+
		"body\n")
	// never recalled: hit_count and last_recalled_at left at their zero values.
	writeFile(t, store, "global/b.md", "+++\nid = \"b\"\ntitle = \"B\"\n+++\nbody\n")

	findings, err := RecallStats(ctx, store)
	require.NoError(t, err)
	require.Len(t, findings, 2)

	assert.Equal(t, RecallStatsFinding{
		EntryID: "a", TopicKey: "topic-a", HitCount: 5, LastRecalledAt: "2026-07-20T00:00:00Z",
	}, findings[0])
	assert.Equal(t, RecallStatsFinding{
		EntryID: "b", TopicKey: "", HitCount: 0, LastRecalledAt: "",
	}, findings[1], "an entry with no recall history is still reported, not filtered out")
}

func TestPromoteCandidatesFiltersByThresholdAndIdempotency(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\n"+
		"id = \"a\"\n"+
		"title = \"A\"\n"+
		"topic_key = \"topic-a\"\n"+
		"recurrence_count = 3\n"+
		"\n"+
		"[anchor]\n"+
		"type = \"files\"\n"+
		"repo = \"github.com/example/a\"\n"+
		"+++\n"+
		"body\n")
	// below threshold: excluded regardless of promotion state.
	writeFile(t, store, "global/b.md", "+++\n"+
		"id = \"b\"\n"+
		"title = \"B\"\n"+
		"recurrence_count = 2\n"+
		"+++\n"+
		"body\n")
	// meets threshold but already promoted: excluded (NFR-02 idempotency).
	writeFile(t, store, "global/c.md", "+++\n"+
		"id = \"c\"\n"+
		"title = \"C\"\n"+
		"recurrence_count = 5\n"+
		"promoted_bead_id = \"crn-existing\"\n"+
		"+++\n"+
		"body\n")

	findings, err := PromoteCandidates(ctx, store, 3)
	require.NoError(t, err)
	require.Len(t, findings, 1, "only 'a' meets the threshold and is unpromoted")
	assert.Equal(t, PromoteCandidateFinding{
		EntryID: "a", TopicKey: "topic-a", RecurrenceCount: 3, AnchorRepo: "github.com/example/a",
	}, findings[0])
}

func TestPromoteCandidatesThresholdConfigurable(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\nrecurrence_count = 3\n+++\nbody\n")

	at3, err := PromoteCandidates(ctx, store, 3)
	require.NoError(t, err)
	assert.Len(t, at3, 1, "recurrence_count 3 meets a threshold of 3")

	at4, err := PromoteCandidates(ctx, store, 4)
	require.NoError(t, err)
	assert.Empty(t, at4, "recurrence_count 3 no longer meets a threshold of 4")
}
