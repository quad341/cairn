package cairn

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEntryIDIncludesTopicKeyButIsUnique(t *testing.T) {
	a, err := NewEntry("shared-topic", []string{"agent:bot"}, "a body", "agent:bot")
	require.NoError(t, err)
	b, err := NewEntry("shared-topic", []string{"agent:bot"}, "a body", "agent:bot")
	require.NoError(t, err)

	assert.Equal(t, "shared-topic", a.TopicKey)
	assert.True(t, strings.HasPrefix(a.ID, "shared-topic-"))
	assert.NotEqual(t, a.ID, b.ID,
		"several entries may deliberately share one topic_key -- shadow() picks the winner at read time -- so id must never be derived from topic_key alone")
}

func TestNewEntryTitleAndSummary(t *testing.T) {
	cases := map[string]struct {
		body            string
		wantTitle       string
		wantSummaryFunc func(t *testing.T, summary string)
	}{
		"one-liner": {
			body:      "fixed the flaky test by seeding the RNG",
			wantTitle: "fixed the flaky test by seeding the RNG",
		},
		"multi-line": {
			body:      "short heading\n\nlonger explanation across\nmultiple lines",
			wantTitle: "short heading",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e, err := NewEntry("t", nil, tc.body, "")
			require.NoError(t, err)
			assert.Equal(t, tc.wantTitle, e.Title)
			assert.Equal(t, strings.TrimSpace(tc.body), e.Summary)
		})
	}
}

func TestNewEntryAnchorIsNone(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	assert.Equal(t, "none", e.Anchor.Type)
}

func TestNewEntryStampsCreatedAtAsDateOnly(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	_, err = time.Parse(time.DateOnly, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an ISO-8601 date so lexical and chronological order agree, see moreSpecific")
}

func TestScopeDirPicksTierByPriorityWhenScopeSpansMultiple(t *testing.T) {
	cases := []struct {
		name  string
		scope []string
		want  string
	}{
		{"empty scope is global", nil, filepath.Join("store", "global")},
		{"single rig tag", []string{"rig:web"}, filepath.Join("store", "rig", "web")},
		{"single role tag", []string{"role:reviewer"}, filepath.Join("store", "role", "reviewer")},
		{"single agent tag", []string{"agent:bot"}, filepath.Join("store", "agent", "bot")},
		{"rig beats role+agent", []string{"agent:bot", "role:reviewer", "rig:web"}, filepath.Join("store", "rig", "web")},
		{"role beats agent", []string{"agent:bot", "role:reviewer"}, filepath.Join("store", "role", "reviewer")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, scopeDir("store", tc.scope))
		})
	}
}

func TestEntryCreateRoundTrip(t *testing.T) {
	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))
	assert.Equal(t, filepath.Join(store, "rig", "web", e.ID+".md"), e.BodyPath)

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Title, got.Title)
	assert.Equal(t, e.Summary, got.Summary)
	assert.Equal(t, e.TopicKey, got.TopicKey)
	assert.Equal(t, e.Scope, got.Scope)
	assert.Equal(t, e.Anchor, got.Anchor)
	assert.Equal(t, e.CreatedBy, got.CreatedBy)
	assert.Equal(t, e.CreatedAt, got.CreatedAt)
	assert.Equal(t, e.Body, got.Body)
}

func TestEntryCreateGlobalTier(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))
	assert.Equal(t, filepath.Join(store, "global", e.ID+".md"), e.BodyPath)

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Empty(t, got.Scope)
}

func TestEntryCreateMakesParentDirs(t *testing.T) {
	e, err := NewEntry("t", []string{"agent:brand-new"}, "body", "")
	require.NoError(t, err)

	store := t.TempDir() // store/agent/brand-new does not exist yet
	require.NoError(t, e.Create(store))

	_, err = ParseEntry(e.BodyPath)
	require.NoError(t, err)
}
