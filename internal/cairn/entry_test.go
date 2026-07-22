package cairn

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

const sampleEntry = `+++
id = "test/one"
title = "One"
summary = "s"
type = "reference"
topic_key = "test/one"
scope = ["rig:alpha"]

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body here
`

func TestParseEntry(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/one.md", sampleEntry))
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Equal(t, "test/one", e.ID)
	assert.Equal(t, "One", e.Title)
	assert.Equal(t, []string{"rig:alpha"}, e.Scope)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Len(t, e.Anchor.Paths, 1)
	assert.Equal(t, "body here\n", e.Body)
}

func TestParseEntryNoFrontmatter(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "# just markdown\n"))
	assert.Nil(t, e)
	require.ErrorIs(t, err, errNotEntry)
}

func TestParseEntryUnterminated(t *testing.T) {
	_, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "+++\nid = \"a\"\nno closing fence\n"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, errNotEntry) // a real parse error, not "not an entry"
}

func TestWriteBackRoundTrip(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", sampleEntry)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.Anchor.Fingerprint = "abc123"
	e.VerifiedAt = "2026-07-19"
	require.NoError(t, e.WriteBack())

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "abc123", e2.Anchor.Fingerprint)
	assert.Equal(t, "2026-07-19", e2.VerifiedAt)
	assert.Equal(t, e.ID, e2.ID)
	assert.Equal(t, e.Body, e2.Body)
}

const (
	globalEntry = "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n"
	alphaEntry  = "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	betaEntry   = "+++\nid = \"t\"\ntitle = \"t\"\nscope = [\"rig:beta\"]\n+++\nx\n"
	crossEntry  = "+++\nid = \"x\"\ntitle = \"x\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"
)

func TestVisible(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md", alphaEntry)
	writeFile(t, dir, "rig/beta/t.md", betaEntry)
	writeFile(t, dir, "role/investigator/x.md", crossEntry)

	seen := func(identity []string) map[string]bool {
		vs, err := Visible(t.Context(), dir, identity)
		require.NoError(t, err)
		m := map[string]bool{}
		for _, e := range vs {
			m[e.ID] = true
		}
		return m
	}

	inv := seen([]string{"rig:alpha", "role:investigator"})
	assert.True(t, inv["g"] && inv["r"] && inv["x"], "alpha-investigator should see g, r, x")
	assert.False(t, inv["t"], "alpha-investigator should not see the beta entry")

	bare := seen(nil)
	assert.True(t, bare["g"], "bare identity should see global")
	assert.False(t, bare["r"] || bare["t"] || bare["x"], "bare identity should see only global")

	builder := seen([]string{"rig:alpha", "role:builder"})
	assert.True(t, builder["g"] && builder["r"], "alpha-builder should see g and r")
	assert.False(t, builder["x"] || builder["t"], "alpha-builder should not see x or t")
}

const (
	lessSpecificShared = "+++\nid = \"s1\"\ntitle = \"s1\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	moreSpecificShared = "+++\nid = \"s2\"\ntitle = \"s2\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"

	earlyVerifiedShared = "+++\nid = \"v1\"\ntitle = \"v1\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-01-01\"\n+++\nx\n"
	lateVerifiedShared  = "+++\nid = \"v2\"\ntitle = \"v2\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-06-01\"\n+++\nx\n"

	tiebreakLowID  = "+++\nid = \"c1\"\ntitle = \"c1\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	tiebreakHighID = "+++\nid = \"c2\"\ntitle = \"c2\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"

	untopiced1 = "+++\nid = \"u1\"\ntitle = \"u1\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced2 = "+++\nid = \"u2\"\ntitle = \"u2\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced3 = "+++\nid = \"u3\"\ntitle = \"u3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestVisibleShadowsBySpecificity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha", "role:investigator"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["s2"], "the 2-tag entry must be visible")
	assert.False(t, ids["s1"], "the 1-tag entry must be shadowed by the more specific one")
}

func TestVisibleShadowTiebreakVerifiedAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/v1.md", earlyVerifiedShared)
	writeFile(t, dir, "rig/alpha/v2.md", lateVerifiedShared)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["v2"], "the more recently verified entry must win")
	assert.False(t, ids["v1"], "the earlier-verified entry must be shadowed")
}

func TestVisibleShadowTiebreakID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/c2.md", tiebreakHighID)
	writeFile(t, dir, "rig/alpha/c1.md", tiebreakLowID)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["c1"], "the lexicographically lower id must win when fully tied")
	assert.False(t, ids["c2"], "the higher id must be shadowed")
}

func TestVisibleUntopicedNeverShadow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/u1.md", untopiced1)
	writeFile(t, dir, "rig/alpha/u2.md", untopiced2)
	writeFile(t, dir, "rig/alpha/u3.md", untopiced3)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["u1"] && ids["u2"] && ids["u3"], "entries without a topic_key must never shadow one another")
}

// parseFixture parses a fixture markdown const into an *Entry, independent of
// IterEntries' scope-dir layout — ShadowMap takes an entry slice directly.
func parseFixture(t *testing.T, content string) *Entry {
	t.Helper()
	e, err := ParseEntry(writeFile(t, t.TempDir(), "e.md", content))
	require.NoError(t, err)
	return e
}

const (
	incomparableRig  = "+++\nid = \"i1\"\ntitle = \"i1\"\ntopic_key = \"inc\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	incomparableRole = "+++\nid = \"i2\"\ntitle = \"i2\"\ntopic_key = \"inc\"\nscope = [\"role:builder\"]\n+++\nx\n"

	chainOneTag    = "+++\nid = \"ch1\"\ntitle = \"ch1\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	chainTwoTags   = "+++\nid = \"ch2\"\ntitle = \"ch2\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\"]\n+++\nx\n"
	chainThreeTags = "+++\nid = \"ch3\"\ntitle = \"ch3\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\", \"agent:x\"]\n+++\nx\n"

	globalShared = "+++\nid = \"gs\"\ntitle = \"gs\"\ntopic_key = \"glob\"\nscope = []\n+++\nx\n"
	scopedShared = "+++\nid = \"rs\"\ntitle = \"rs\"\ntopic_key = \"glob\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestShadowMapSuperset(t *testing.T) {
	s1 := parseFixture(t, lessSpecificShared)
	s2 := parseFixture(t, moreSpecificShared)

	sm := ShadowMap([]*Entry{s1, s2})

	require.Contains(t, sm, "s1", "the 1-tag entry must be shadowed")
	assert.Equal(t, "s2", sm["s1"].ID, "the 1-tag entry must be shadowed by the 2-tag superset entry")
	assert.NotContains(t, sm, "s2", "the more specific entry must not appear as shadowed")
}

func TestShadowMapIncomparableScopesNeverShadow(t *testing.T) {
	i1 := parseFixture(t, incomparableRig)
	i2 := parseFixture(t, incomparableRole)

	sm := ShadowMap([]*Entry{i1, i2})

	assert.NotContains(t, sm, "i1", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
	assert.NotContains(t, sm, "i2", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
}

func TestShadowMapTiebreakOnEqualScope(t *testing.T) {
	v1 := parseFixture(t, earlyVerifiedShared)
	v2 := parseFixture(t, lateVerifiedShared)

	sm := ShadowMap([]*Entry{v1, v2})

	require.Contains(t, sm, "v1", "the earlier-verified entry must be shadowed")
	assert.Equal(t, "v2", sm["v1"].ID, "the earlier-verified entry must be shadowed by the later-verified one")
	assert.NotContains(t, sm, "v2", "the later-verified (winning) entry must not appear as shadowed")
}

func TestShadowMapChainReportsMostSpecific(t *testing.T) {
	ch1 := parseFixture(t, chainOneTag)
	ch2 := parseFixture(t, chainTwoTags)
	ch3 := parseFixture(t, chainThreeTags)

	sm := ShadowMap([]*Entry{ch1, ch2, ch3})

	require.Contains(t, sm, "ch1")
	assert.Equal(t, "ch3", sm["ch1"].ID, "the 1-tag entry must be shadowed by the most specific entry in the chain, not its nearest neighbor")
	require.Contains(t, sm, "ch2")
	assert.Equal(t, "ch3", sm["ch2"].ID, "the 2-tag entry must be shadowed by the most specific entry in the chain")
	assert.NotContains(t, sm, "ch3", "the most specific entry in the chain must not appear as shadowed")
}

func TestShadowMapUntopicedNeverShadow(t *testing.T) {
	u1 := parseFixture(t, untopiced1)
	u2 := parseFixture(t, untopiced2)
	u3 := parseFixture(t, untopiced3)

	sm := ShadowMap([]*Entry{u1, u2, u3})

	assert.Empty(t, sm, "entries without a topic_key must never appear in the shadow map")
}

func TestShadowMapGlobalShadowedByScoped(t *testing.T) {
	g := parseFixture(t, globalShared)
	r := parseFixture(t, scopedShared)

	sm := ShadowMap([]*Entry{g, r})

	require.Contains(t, sm, "gs", "the global (empty-scope) entry must be shadowed by the scoped one")
	assert.Equal(t, "rs", sm["gs"].ID)
	assert.NotContains(t, sm, "rs", "the scoped entry must not appear as shadowed by the global one")
}

func TestFindReturnsErrNotFoundForUnknownID(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()

	_, err := Find(ctx, store, "does-not-exist")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFindIncrementsHitCount(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	e, err := Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, 1, e.HitCount, "the returned entry must carry the authoritative post-increment count")

	e, err = Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, 2, e.HitCount)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var hitCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT hit_count FROM entries WHERE id = 'a'").Scan(&hitCount))
	assert.Equal(t, 2, hitCount, "the index's own counter must match what Find returned")
}

func TestFindUsesIndexPointLookupNotWalk(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	_, err := Find(ctx, store, "a")
	require.NoError(t, err, "this builds the index")

	// A genuinely malformed entry (unterminated frontmatter) added after
	// indexing would break a walk-based lookup: IterEntries propagates any
	// ParseEntry error that isn't errNotEntry.
	writeFile(t, store, "global/bad.md", "+++\nid = \"bad\nno closing fence\n")
	_, err = IterEntries(store)
	require.Error(t, err, "sanity check: the malformed sibling file must break a walk")

	// Find resolves "a" via the already-built index rather than re-walking --
	// a non-git store's index is trusted fresh once built (crn-6az.6.1.2) --
	// so the malformed sibling is never parsed.
	e, err := Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, "a", e.ID)
}

func TestFindAfterSequentialCreateOnNonGitStore(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()

	e1, err := NewEntry("dbg-topic-1", nil, "body 1", "tester")
	require.NoError(t, err)
	require.NoError(t, e1.Create(store))

	_, err = Find(ctx, store, e1.ID)
	require.NoError(t, err, "first Find must succeed")

	e2, err := NewEntry("dbg-topic-2", nil, "body 2", "tester")
	require.NoError(t, err)
	require.NoError(t, e2.Create(store))

	// Entry.Create is a pure filesystem write -- no git commit, no reindex --
	// so without Find's retry-on-miss, ensureFresh would treat the index
	// built by the first Find above as fresh forever on this non-git store
	// (crn-6az.6.1.2), falsely reporting this second entry not found.
	_, err = Find(ctx, store, e2.ID)
	require.NoError(t, err, "second Find must succeed even though the store's index predates this entry")
}

func TestVisibleNeverReadsBodiesAfterIndexBuilt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	vs, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err)
	require.Len(t, vs, 1)
	assert.Equal(t, "g", vs[0].ID)

	// Added after the index already exists, on a store with no git HEAD to
	// diff against -- ensureFresh treats an already-indexed, non-git store as
	// forever fresh (see indexStale), so this file is never walked. Confirm
	// it really would break a body walk if it were, so the assertion below
	// is a real proof rather than a vacuous one.
	writeFile(t, dir, "global/broken.md", "+++\nid = \"broken\"\nno closing fence\n")
	_, walkErr := IterEntries(dir)
	require.Error(t, walkErr, "sanity check: the malformed sibling must actually break a body walk")

	vs2, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err, "Visible must never re-walk bodies to satisfy a query")
	require.Len(t, vs2, 1)
	assert.Equal(t, "g", vs2[0].ID)
}

func TestStatusNeverReadsBodiesAfterIndexBuilt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	entries, err := Status(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "g", entries[0].ID)

	// Added after the index already exists, on a store with no git HEAD to
	// diff against -- ensureFresh treats an already-indexed, non-git store as
	// forever fresh (see indexStale), so this file is never walked. Confirm
	// it really would break a body walk if it were, so the assertion below
	// is a real proof rather than a vacuous one.
	writeFile(t, dir, "global/broken.md", "+++\nid = \"broken\"\nno closing fence\n")
	_, walkErr := IterEntries(dir)
	require.Error(t, walkErr, "sanity check: the malformed sibling must actually break a body walk")

	entries2, err := Status(t.Context(), dir)
	require.NoError(t, err, "Status must never re-walk bodies to satisfy a query")
	require.Len(t, entries2, 1)
	assert.Equal(t, "g", entries2[0].ID)
}

func TestVisibleNeverTouchesHitCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	// Simulate a prior Find/Get having bumped hit_count independently of the
	// body (crn-6az.6.1.1, see reindexTx's comment) -- exactly the index-only
	// state Visible must leave alone, since it only ever issues SELECTs.
	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET hit_count = 7 WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT hit_count FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, 7, got, "Visible must never touch hit_count")
}

func TestStatusNeverTouchesHitCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Status(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET hit_count = 7 WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Status(t.Context(), dir)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT hit_count FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, 7, got, "Status must never touch hit_count")
}

func TestStatusPopulatesAnchorAndScopeFields(t *testing.T) {
	dir := t.TempDir()
	body := "+++\n" +
		"id = \"a\"\n" +
		"title = \"A\"\n" +
		"topic_key = \"t/a\"\n" +
		"scope = [\"rig:alpha\", \"role:investigator\"]\n" +
		"verified_at = \"2026-07-01\"\n" +
		"created_at = \"2026-01-01T00:00:00Z\"\n" +
		"\n" +
		"[anchor]\n" +
		"type = \"files\"\n" +
		"repo = \"/some/repo\"\n" +
		"paths = [\"a.go\", \"b.go\"]\n" +
		"spec = \"main\"\n" +
		"fingerprint = \"abc123\"\n" +
		"+++\n" +
		"body\n"
	writeFile(t, dir, "role/investigator/a.md", body)

	entries, err := Status(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "a", e.ID)
	assert.Equal(t, "t/a", e.TopicKey)
	assert.ElementsMatch(t, []string{"rig:alpha", "role:investigator"}, e.Scope)
	assert.Equal(t, "2026-07-01", e.VerifiedAt)
	assert.Equal(t, "2026-01-01T00:00:00Z", e.CreatedAt)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Equal(t, "/some/repo", e.Anchor.Repo)
	assert.Equal(t, []string{"a.go", "b.go"}, e.Anchor.Paths)
	assert.Equal(t, "main", e.Anchor.Spec)
	assert.Equal(t, "abc123", e.Anchor.Fingerprint)
}
