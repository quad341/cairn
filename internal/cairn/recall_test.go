package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
