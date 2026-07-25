package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	singleWinnerEntry = "+++\nid = \"sw1\"\ntitle = \"Single Winner\"\nsummary = \"the only entry for this topic\"\ntopic_key = \"solo-topic\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	prefixTopicEntry  = "+++\nid = \"pt1\"\ntitle = \"Prefix Topic\"\ntopic_key = \"foo/bar/baz\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
)

func TestListByTopicSingleWinner(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sw1.md", singleWinnerEntry)

	rows, err := ListByTopic(t.Context(), dir, "solo-topic", []string{"rig:alpha"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "sw1", rows[0].ID)
	assert.Equal(t, "Single Winner", rows[0].Title)
	assert.Equal(t, "the only entry for this topic", rows[0].Summary)
	assert.Equal(t, []string{"rig:alpha"}, rows[0].Scope)
	assert.Equal(t, Unknown, rows[0].FreshnessState, "no anchor set -- Check() must report Unknown")
}

func TestListByTopicZeroMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sw1.md", singleWinnerEntry)

	rows, err := ListByTopic(t.Context(), dir, "no-such-topic", []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Empty(t, rows, "querying a topic_key held by no visible entry must return no rows")
}

func TestListByTopicUntopicedMultiEntryBucket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/u1.md", untopiced1)
	writeFile(t, dir, "rig/alpha/u2.md", untopiced2)
	writeFile(t, dir, "rig/alpha/u3.md", untopiced3)

	rows, err := ListByTopic(t.Context(), dir, "", []string{"rig:alpha"})
	require.NoError(t, err)
	require.Len(t, rows, 3, "all simultaneously visible untopiced entries must be returned, none dropped")

	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.ID] = true
	}
	assert.True(t, ids["u1"] && ids["u2"] && ids["u3"])
}

func TestListByTopicShadowConflictPrecedence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	rows, err := ListByTopic(t.Context(), dir, "shared", []string{"rig:alpha", "role:investigator"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "shadow() must collapse the same-topic_key tie to exactly one winner")
	assert.Equal(t, "s2", rows[0].ID, "the more specific entry must be the sole winner")
}

func TestListByTopicNeverTouchesRecallTelemetry(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sw1.md", singleWinnerEntry)

	_, err := ListByTopic(ctx, dir, "solo-topic", []string{"rig:alpha"})
	require.NoError(t, err, "this builds the index")

	// Simulate a prior Find/Get having stamped recall telemetry independently
	// of the body (crn-6az.6.1.1) -- exactly the index-only state
	// ListByTopic must leave alone, since only a subsequent `cairn get <id>`
	// is a confirmed recall (open question 2 in crn-tllb.1's design).
	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE entries SET hit_count = 7, last_recalled_at = '2026-01-01T00:00:00Z' WHERE id = 'sw1'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = ListByTopic(ctx, dir, "solo-topic", []string{"rig:alpha"})
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var hitCount int
	var lastRecalledAt string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT hit_count, last_recalled_at FROM entries WHERE id = 'sw1'`).Scan(&hitCount, &lastRecalledAt))
	assert.Equal(t, 7, hitCount, "ListByTopic must never touch hit_count")
	assert.Equal(t, "2026-01-01T00:00:00Z", lastRecalledAt, "ListByTopic must never touch last_recalled_at")
}

func TestListByTopicExactMatchOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/pt1.md", prefixTopicEntry)
	ctx := t.Context()

	rows, err := ListByTopic(ctx, dir, "foo/bar", []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Empty(t, rows, "a strict prefix of a real topic_key must not match (FR-3: exact match only)")

	rows, err = ListByTopic(ctx, dir, "bar/baz", []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Empty(t, rows, "a strict substring of a real topic_key must not match (FR-3: exact match only)")

	rows, err = ListByTopic(ctx, dir, "foo/bar/baz", []string{"rig:alpha"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the exact topic_key must still match")
}
