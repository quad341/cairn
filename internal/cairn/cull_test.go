package cairn

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisuseReferencePrefersLastRecalledOverCreatedAt(t *testing.T) {
	ref, ok := disuseReference("2026-07-01T00:00:00Z", "2020-01-01")
	require.True(t, ok)
	assert.Equal(t, 2026, ref.Year())
}

func TestDisuseReferenceFallsBackToCreatedAtWhenNeverRecalled(t *testing.T) {
	ref, ok := disuseReference("", "2020-01-01")
	require.True(t, ok)
	assert.Equal(t, 2020, ref.Year())
}

func TestDisuseReferenceNeitherFieldParses(t *testing.T) {
	_, ok := disuseReference("", "")
	assert.False(t, ok)
}

// TestCullCandidatesIndependentOfFreshness covers FR-10: CULL (disuse) and
// FRESHNESS (anchor-drift, Check()) are independent signals. A synthetic
// entry that carries a populated anchor+verified_at (the shape a Fresh
// Check() result needs) but is long disused must still be reported; a
// synthetic entry with no anchor/verified_at at all (the shape an
// Unknown/Stale Check() result needs) but recently recalled must not.
// CullCandidates' own query never selects verified_at or any anchor_*
// column, so this also guards against a future change accidentally wiring
// freshness state into the disuse decision.
func TestCullCandidatesIndependentOfFreshness(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	disusedSince := time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	recalledRecently := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	writeFile(t, store, "global/fresh-but-disused.md", "+++\n"+
		"id = \"fresh-but-disused\"\n"+
		"title = \"Fresh but disused\"\n"+
		"topic_key = \"topic-a\"\n"+
		"last_recalled_at = \""+disusedSince+"\"\n"+
		"verified_at = \"2026-07-20\"\n"+
		"\n"+
		"[anchor]\n"+
		"type = \"none\"\n"+
		"+++\n"+
		"body\n")
	writeFile(t, store, "global/stale-but-recent.md", "+++\n"+
		"id = \"stale-but-recent\"\n"+
		"title = \"Stale but recent\"\n"+
		"topic_key = \"topic-b\"\n"+
		"last_recalled_at = \""+recalledRecently+"\"\n"+
		"+++\n"+
		"body\n")

	findings, err := CullCandidates(ctx, store, 30*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, findings, 1, "only the disused entry is cull-eligible, regardless of its own freshness signal")
	assert.Equal(t, "fresh-but-disused", findings[0].EntryID)
	assert.Equal(t, disusedSince, findings[0].DisusedSince)
}

func TestCullCandidatesNeverRecalledFallsBackToCreatedAt(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	oldDate := time.Now().Add(-60 * 24 * time.Hour).Format(time.DateOnly)
	recentDate := time.Now().Format(time.DateOnly)

	writeFile(t, store, "global/old-never-recalled.md", "+++\n"+
		"id = \"old-never-recalled\"\n"+
		"title = \"Old\"\n"+
		"created_at = \""+oldDate+"\"\n"+
		"+++\n"+
		"body\n")
	writeFile(t, store, "global/new-never-recalled.md", "+++\n"+
		"id = \"new-never-recalled\"\n"+
		"title = \"New\"\n"+
		"created_at = \""+recentDate+"\"\n"+
		"+++\n"+
		"body\n")

	findings, err := CullCandidates(ctx, store, 30*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, findings, 1, "an entry never recalled must age into cull-eligibility from its created_at date")
	assert.Equal(t, "old-never-recalled", findings[0].EntryID)
}

func TestCullCandidatesThresholdConfigurable(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	disusedFor40Days := time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\nlast_recalled_at = \""+disusedFor40Days+"\"\n+++\nbody\n")

	at30, err := CullCandidates(ctx, store, 30*24*time.Hour)
	require.NoError(t, err)
	assert.Len(t, at30, 1, "disused 40d ago exceeds a 30d threshold")

	at60, err := CullCandidates(ctx, store, 60*24*time.Hour)
	require.NoError(t, err)
	assert.Empty(t, at60, "disused 40d ago does not exceed a 60d threshold")
}

func TestCullCandidatesIncludesScopeForDownstreamTierDecision(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	old := time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	writeFile(t, store, "rig/web/a.md", "+++\nid = \"a\"\ntitle = \"A\"\nscope = [\"rig:web\"]\nlast_recalled_at = \""+old+"\"\n+++\nbody\n")

	findings, err := CullCandidates(ctx, store, 30*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, []string{"rig:web"}, findings[0].Scope,
		"scope must be reported so a downstream consumer (e.g. the librarian formula) can decide direct-evict vs review-branch-propose")
}

func TestCullCandidatesNotYetDisusedIsExcluded(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	recent := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\nlast_recalled_at = \""+recent+"\"\n+++\nbody\n")

	findings, err := CullCandidates(ctx, store, 30*24*time.Hour)
	require.NoError(t, err)
	assert.Empty(t, findings)
}
