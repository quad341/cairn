package cairn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	searchBodyOnly = "+++\nid = \"sb1\"\ntitle = \"Unrelated Heading\"\ntopic_key = \"body-only\"\n" +
		"scope = [\"rig:alpha\"]\n+++\nThe supervisor reaps orphaned zygote processes on restart.\n"
	searchTopicMatch = "+++\nid = \"stm\"\ntitle = \"Heading\"\ntopic_key = \"zygote-reaping\"\n" +
		"scope = [\"rig:alpha\"]\n+++\nunrelated prose about databases\n"
	searchOtherRig = "+++\nid = \"oth\"\ntitle = \"Other Rig\"\ntopic_key = \"other-zygote\"\n" +
		"scope = [\"rig:beta\"]\n+++\nzygote zygote zygote\n"
)

func ids(hits []SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

// TestSearchFindsEntryByBodyTerm is the whole point of the command: an entry
// is reachable by a word that appears only in its body, without the caller
// knowing its topic key or ID.
func TestSearchFindsEntryByBodyTerm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)

	res, err := Search(t.Context(), dir, "why are my processes being reaped", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, res.Hits)
	assert.Contains(t, ids(res.Hits), "sb1")
}

// TestSearchRespectsIdentityScope pins that scope filtering happens before
// ranking. The beta entry is the strongest lexical match in the store by a
// wide margin, so if scope were applied after ranking -- or not at all -- it
// would take the top slot.
func TestSearchRespectsIdentityScope(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	writeFile(t, dir, "rig/beta/oth.md", searchOtherRig)

	res, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.NotContains(t, ids(res.Hits), "oth", "an out-of-scope entry must never occupy a result slot")
}

// TestSearchExcludesShadowedEntry pins that Search agrees with list/prime
// about which entry wins a topic key: the shadowed one is not a separate
// result.
func TestSearchExcludesShadowedEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	res, err := Search(t.Context(), dir, "shared", []string{"rig:alpha", "role:investigator"}, 0)
	require.NoError(t, err)
	got := ids(res.Hits)
	assert.NotContains(t, got, "s1", "the shadowed entry must not appear alongside its winner")
}

// TestSearchTopicKeyMatchOutranksBodyMatch pins the field boost. Both entries
// match "zygote"; only one carries it in its topic key, and that one is the
// better answer to a question about zygotes.
func TestSearchTopicKeyMatchOutranksBodyMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	writeFile(t, dir, "rig/alpha/stm.md", searchTopicMatch)

	res, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, res.Hits)
	assert.Equal(t, "stm", res.Hits[0].ID, "a topic_key match must outrank a body-only match")
}

func TestSearchLimitBoundsHitsButNotTotals(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	writeFile(t, dir, "rig/alpha/stm.md", searchTopicMatch)

	res, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 1)
	require.NoError(t, err)
	assert.Len(t, res.Hits, 1)
	assert.Equal(t, 2, res.TotalMatched, "TotalMatched must count every visible match, not just the returned page")
}

// TestSearchTruncatesOversizedProjection pins that Search bounds what it
// projects regardless of what is on disk -- an entry written before the
// validate.go caps, or straight to disk bypassing them, must not be able to
// crowd every other hit out of the caller's context.
func TestSearchTruncatesOversizedProjection(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("verbose ", 400)
	writeFile(t, dir, "rig/alpha/big.md",
		"+++\nid = \"big\"\ntitle = \""+huge+"\"\nsummary = \""+huge+"\"\ntopic_key = \"oversized\"\n"+
			"scope = [\"rig:alpha\"]\n+++\noversized body\n")

	res, err := Search(t.Context(), dir, "oversized", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, res.Hits)
	assert.LessOrEqual(t, len([]rune(res.Hits[0].Title)), searchTitleCap)
	assert.LessOrEqual(t, len([]rune(res.Hits[0].Summary)), searchSummaryCap)
}

// TestSearchSummaryPreservesWordBoundaryOnReTruncation covers crn-77k30: a
// Summary already cleanly word/ellipsis-truncated at write time
// (NewEntry/DerivedTitleSummary, crn-q08yt) can still land in the 241-280
// rune range -- inside summaryCap (280) but past searchSummaryCap (240).
// Search's own re-truncation must not hard-cut that value mid-word merely
// because it is applying a second, smaller ceiling on top of an
// already-truncated one -- the same bug class as crn-q08yt, a different call
// site (search.go:196 instead of remember.go/prime.go).
func TestSearchSummaryPreservesWordBoundaryOnReTruncation(t *testing.T) {
	dir := t.TempDir()
	// 235 "a"s, a space, then 23 "b"s and a trailing ellipsis -- as if
	// DerivedTitleSummary had already word-truncated a longer body down to
	// 260 runes (within summaryCap's 280, past searchSummaryCap's 240).
	// truncateRunes(_, 240) cuts 4 runes into the "b" word and drops the
	// original ellipsis entirely; a word-boundary re-truncation must instead
	// drop the whole trailing word and append its own fresh ellipsis.
	summary := strings.Repeat("a", 235) + " " + strings.Repeat("b", 23) + "…"
	require.Len(t, []rune(summary), 260, "fixture must sit in the 241-280 rune band this bug is scoped to")
	writeFile(t, dir, "rig/alpha/wb1.md",
		"+++\nid = \"wb1\"\ntitle = \"Word Boundary Regression\"\nsummary = \""+summary+"\"\ntopic_key = \"wordbound\"\n"+
			"scope = [\"rig:alpha\"]\n+++\nbody text\n")

	res, err := Search(t.Context(), dir, "wordbound", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, res.Hits)

	got := res.Hits[0].Summary
	assert.LessOrEqual(t, len([]rune(got)), searchSummaryCap)
	assert.Equal(t, strings.Repeat("a", 235)+"…", got,
		"re-truncation must back up to the word boundary and append its own ellipsis, not hard-cut into the \"b\" word")
}

// TestSearchSelfHealsMissingFTSIndex covers every existing deployment's first
// upgrade: index.sqlite is current by the git watermark ensureFresh consults,
// so nothing would rebuild it, yet it was built by a binary that had no FTS
// table at all.
func TestSearchSelfHealsMissingFTSIndex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DROP TABLE entries_fts`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	res, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err, "search must rebuild an index that predates the FTS table")
	assert.Contains(t, ids(res.Hits), "sb1")
}

func TestQueryTermsDropsNoiseWords(t *testing.T) {
	got := queryTerms("The worktree name and the current branch don't match.")
	assert.Equal(t, []string{"worktree", "name", "current", "branch", "match"}, got,
		"stopwords, the contraction fragment 'don', and duplicates must all be dropped")
}

func TestQueryTermsDeduplicatesCaseInsensitively(t *testing.T) {
	assert.Equal(t, []string{"reindex"}, queryTerms("Reindex REINDEX reindex"))
}

// TestSearchRejectsQueryWithNoSearchableTerms pins that a query of pure
// stopwords is an explicit error rather than a silent empty result, which a
// caller would otherwise read as "the store has nothing on this".
func TestSearchRejectsQueryWithNoSearchableTerms(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)

	_, err := Search(t.Context(), dir, "the and of it", []string{"rig:alpha"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no searchable terms")
}

// TestBuildFTSQueryQuotesTerms pins that terms reach FTS5 as quoted string
// literals. Unquoted, a term colliding with an FTS5 keyword (OR, NOT, NEAR)
// would be parsed as an operator and change the query's meaning.
func TestBuildFTSQueryQuotesTerms(t *testing.T) {
	assert.Equal(t, `"not" OR "near"`, buildFTSQuery([]string{"not", "near"}))
}

func TestSearchReportsQueryTerms(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)

	res, err := Search(t.Context(), dir, "the reaped processes", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"reaped", "processes"}, res.QueryTerms,
		"a caller must be able to see which of its words actually counted")
}

// TestReindexPopulatesFTS pins that the full-text index is rebuilt from the
// same entry list as the entries table, in the same transaction, so the two
// cannot disagree about what the store holds.
func TestReindexPopulatesFTS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM entries_fts`).Scan(&n))
	assert.Equal(t, 1, n)
}

// TestReindexDropsRemovedEntryFromFTS pins that a deleted body leaves no
// searchable ghost behind -- otherwise search would keep serving an entry
// that `get` can no longer resolve.
func TestReindexDropsRemovedEntryFromFTS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)
	writeFile(t, dir, "rig/alpha/stm.md", searchTopicMatch)
	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(dir, "rig/alpha/stm.md")))
	_, err = Reindex(t.Context(), dir)
	require.NoError(t, err)

	res, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.NotContains(t, ids(res.Hits), "stm", "a deleted entry must not remain searchable")
}

// TestSearchScoreIsIndependentOfInvisibleEntries pins that corpus statistics
// are computed over the caller's visible set alone.
//
// IDF is a property of the corpus it is measured against. If document
// frequency counted entries the caller cannot see, a term common among hidden
// entries would be down-weighted for everyone, silently reordering the visible
// results. Filtering after ranking stops hidden entries taking result slots
// but not from changing the order of the slots that remain -- which is why
// this asserts an unchanged score rather than an unchanged result set.
func TestSearchScoreIsIndependentOfInvisibleEntries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/sb1.md", searchBodyOnly)

	before, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.Len(t, before.Hits, 1)

	for i := range 20 {
		writeFile(t, dir, "rig/beta/noise"+string(rune('a'+i))+".md",
			"+++\nid = \"noise"+string(rune('a'+i))+"\"\ntitle = \"Noise\"\n"+
				"topic_key = \"noise-"+string(rune('a'+i))+"\"\nscope = [\"rig:beta\"]\n+++\nzygote\n")
	}

	after, err := Search(t.Context(), dir, "zygote", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.Len(t, after.Hits, 1)
	assert.InDelta(t, before.Hits[0].Score, after.Hits[0].Score, 1e-9,
		"20 invisible entries sharing the query term must not move a visible entry's score")
}

// TestSearchFieldBoostDoesNotFireOnPrefixCollision pins that field membership
// comes from FTS5's own stemming rather than a Go prefix heuristic.
//
// An earlier revision approximated porter stemming with a four-character
// prefix rule, which handed "database-migration" the full topic-key boost for
// the query term "data". porter stems "data" to "data" and "database" to
// "databas", so the index itself already answers this correctly.
func TestSearchFieldBoostDoesNotFireOnPrefixCollision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/collide.md",
		"+++\nid = \"collide\"\ntitle = \"Collide\"\ntopic_key = \"database-migration\"\n"+
			"scope = [\"rig:alpha\"]\n+++\nthe data pipeline runs nightly\n")
	writeFile(t, dir, "rig/alpha/real.md",
		"+++\nid = \"real\"\ntitle = \"Real\"\ntopic_key = \"data-retention\"\n"+
			"scope = [\"rig:alpha\"]\n+++\nthe data pipeline runs nightly\n")

	res, err := Search(t.Context(), dir, "data", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	require.Len(t, res.Hits, 2)
	assert.Equal(t, "real", res.Hits[0].ID,
		"only the entry whose topic key really contains the term may take the topic-key boost")
	assert.Greater(t, res.Hits[0].Score, res.Hits[1].Score)
}

// TestSearchScopeComesFromFrontmatterNotPath is a regression fixture for a
// class of bug that has already cost time once: an entry's directory is not
// its scope. An entry filed under rig/gascity/ can carry an additional
// role:builder tag, and is then invisible to a rig:gascity-only identity.
// Anything that infers scope from the path will disagree with Visible().
func TestSearchScopeComesFromFrontmatterNotPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/gascity/cross.md",
		"+++\nid = \"cross\"\ntitle = \"Cross cut\"\ntopic_key = \"pool-slot-return\"\n"+
			"scope = [\"rig:gascity\", \"role:builder\"]\n+++\nreturn the pool slot to its home branch\n")

	rigOnly, err := Search(t.Context(), dir, "pool slot", []string{"rig:gascity"}, 0)
	require.NoError(t, err)
	assert.Empty(t, rigOnly.Hits,
		"a rig-only identity must not see an entry that also requires role:builder")

	both, err := Search(t.Context(), dir, "pool slot", []string{"rig:gascity", "role:builder"}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"cross"}, ids(both.Hits))
}
