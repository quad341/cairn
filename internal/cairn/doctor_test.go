package cairn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- DuplicateIDs (FR-4) ----

const (
	dupFixtureOne   = "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n"
	dupFixtureTwo   = "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	uniqueIDFixture = "+++\nid = \"only\"\ntitle = \"Only\"\nscope = []\n+++\nx\n"
)

func TestDuplicateIDsCleanForUniqueIDs(t *testing.T) {
	a := parseFixture(t, uniqueIDFixture)
	assert.Empty(t, DuplicateIDs([]*Entry{a}))
}

func TestDuplicateIDsReportsCollisionWithBothPaths(t *testing.T) {
	a := parseFixture(t, dupFixtureOne)
	b := parseFixture(t, dupFixtureTwo)

	findings := DuplicateIDs([]*Entry{a, b})
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, CategoryDuplicateID, f.Category)
	assert.Equal(t, SeverityError, f.Severity)
	assert.Equal(t, []string{"dup"}, f.EntryIDs)

	paths, ok := f.Detail["paths"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{a.BodyPath, b.BodyPath}, paths)
}

// ---- scopeMismatch (FR-5) ----

func TestScopeMismatchCleanWhenTagMatchesDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/r.md", "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	assert.Empty(t, scopeMismatch(dir, entries))
}

func TestScopeMismatchReportsWrongTag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/r.md", "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:beta\"]\n+++\nx\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	findings := scopeMismatch(dir, entries)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, CategoryScopeMismatch, f.Category)
	assert.Equal(t, []string{"r"}, f.EntryIDs)
	assert.Contains(t, f.Message, "rig:alpha")
}

func TestScopeMismatchExemptsGlobalTier(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = [\"rig:anything\"]\n+++\nx\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	assert.Empty(t, scopeMismatch(dir, entries), "global/ entries are exempt from the disk-tier/scope-tag match")
}

// ---- invalidTags / tagInvalidReason (FR-6, OQ1's two-part test) ----

func TestTagInvalidReason(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		invalid bool
	}{
		{"global literal", "global", false},
		{"well-formed rig tag", "rig:alpha", false},
		{"well-formed role tag", "role:investigator", false},
		{"well-formed agent tag", "agent:bot", false},
		{"no colon at all", "rigalpha", true},
		{"unknown tier", "team:alpha", true},
		{"extra colon in value", "rig:alpha:beta", true},
		{"unsafe value traversal", "rig:..", true},
		{"unsafe value path separator", "rig:a/b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := tagInvalidReason(tc.tag)
			if tc.invalid {
				assert.NotEmpty(t, reason, "tag %q should be invalid", tc.tag)
			} else {
				assert.Empty(t, reason, "tag %q should be valid, got reason %q", tc.tag, reason)
			}
		})
	}
}

func TestInvalidTagsCleanForWellFormedTags(t *testing.T) {
	e := parseFixture(t, "+++\nid = \"a\"\ntitle = \"a\"\nscope = [\"global\", \"rig:alpha\"]\n+++\nx\n")
	assert.Empty(t, invalidTags([]*Entry{e}))
}

func TestInvalidTagsReportsBadTag(t *testing.T) {
	e := parseFixture(t, "+++\nid = \"a\"\ntitle = \"a\"\nscope = [\"team:alpha\"]\n+++\nx\n")

	findings := invalidTags([]*Entry{e})
	require.Len(t, findings, 1)
	assert.Equal(t, CategoryInvalidTag, findings[0].Category)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Equal(t, []string{"a"}, findings[0].EntryIDs)
}

// ---- fromDedupFinding (FR-2 adapter over dedup.go's existing Dedup/DedupEntries) ----

func TestFromDedupFindingPreservesShapeAndMessage(t *testing.T) {
	df := DedupFinding{Kind: "topic_key", TopicKey: "tk", EntryIDs: []string{"a", "b"}, Detail: "collision"}
	f := fromDedupFinding(df)
	assert.Equal(t, CategoryDedup, f.Category)
	assert.Equal(t, SeverityWarning, f.Severity)
	assert.Equal(t, "collision", f.Message)
	assert.Equal(t, []string{"a", "b"}, f.EntryIDs)
	assert.Equal(t, "tk", f.Detail["topic_key"])
}

// ---- fromSweepFinding / agentFreshness (FR-8 over sweep.go's existing Sweep/SweepEntries) ----

func TestFromSweepFindingFiltersFresh(t *testing.T) {
	assert.Nil(t, fromSweepFinding(SweepFinding{ID: "a", Status: Fresh}), "a healthy anchor must never become a finding")
}

func TestFromSweepFindingWrapsNonFresh(t *testing.T) {
	f := fromSweepFinding(SweepFinding{ID: "a", Tier: "global", Status: Stale, Detail: "drifted", AnchorType: "files"})
	require.NotNil(t, f)
	assert.Equal(t, CategoryFreshness, f.Category)
	assert.Equal(t, SeverityWarning, f.Severity)
	assert.Equal(t, []string{"a"}, f.EntryIDs)
	assert.Contains(t, f.Message, "stale")
}

func TestAgentFreshnessOnlyChecksAgentTier(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "agent/bot/a.md", "+++\nid = \"a\"\ntitle = \"a\"\nscope = [\"agent:bot\"]\n+++\nx\n")
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	findings := agentFreshness(ctx, dir, entries)
	require.Len(t, findings, 1, "both entries lack an anchor and are equally Unknown; only the agent-tier one is this check's remit")
	assert.Equal(t, []string{"a"}, findings[0].EntryIDs, "global is SweepEntries' remit, not agentFreshness'")
}

// ---- indexDrift (FR-8) ----

func TestIndexDriftCleanWhenIndexAlreadyFresh(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	_, err = ReindexEntries(ctx, dir, entries)
	require.NoError(t, err)

	assert.Empty(t, indexDrift(ctx, dir, entries))
}

func TestIndexDriftReportsStaleIndex(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")
	gitInit(t, dir)
	gitCommitAll(t, dir, "init")

	_, err := Reindex(ctx, dir)
	require.NoError(t, err)

	writeFile(t, dir, "global/b.md", "+++\nid = \"b\"\ntitle = \"B\"\n+++\nbody\n")
	gitCommitAll(t, dir, "add b")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	require.Empty(t, failures)

	findings := indexDrift(ctx, dir, entries)
	require.Len(t, findings, 1, "the index is genuinely behind HEAD, but reindexing the valid entry set must still succeed cleanly")
	assert.Equal(t, CategoryIndexDrift, findings[0].Category)
	assert.Contains(t, findings[0].Message, "stale")
}

// ---- ExplainEntry (FR-7, store-wide mode) ----

func TestExplainEntryReportsShadowRule(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	res, err := ExplainEntry(ctx, dir, "s1")
	require.NoError(t, err)
	assert.Equal(t, "shared", res.TopicKey)
	assert.Equal(t, "s2", res.ShadowedByID)
	assert.Equal(t, "scope_size", res.Rule)

	res2, err := ExplainEntry(ctx, dir, "s2")
	require.NoError(t, err)
	assert.Empty(t, res2.ShadowedByID, "the most specific entry must not be reported as shadowed")
	assert.Empty(t, res2.Rule)
}

func TestExplainEntryUnknownIDErrors(t *testing.T) {
	_, err := ExplainEntry(t.Context(), t.TempDir(), "does-not-exist")
	require.Error(t, err)
}

func TestExplainEntryUntopicedNeverShadowed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/u1.md", untopiced1)

	res, err := ExplainEntry(t.Context(), dir, "u1")
	require.NoError(t, err)
	assert.Empty(t, res.TopicKey)
	assert.Empty(t, res.ShadowedByID)
}

// ---- ExplainForIdentity (FR-7, identity-scoped mode) ----

// TestExplainForIdentityReportsWinnerAndRuleWhenBothSatisfy exercises the
// shadowReason tiebreak path: both candidates are visible to this identity,
// so the contest -- and its rule -- is genuinely resolved, not shortcut by
// the sole-candidate case.
func TestExplainForIdentityReportsWinnerAndRuleWhenBothSatisfy(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	res, err := ExplainForIdentity(ctx, dir, []string{"rig:alpha", "role:investigator"}, "shared")
	require.NoError(t, err)
	require.Len(t, res.Candidates, 2)
	for _, c := range res.Candidates {
		assert.True(t, c.Satisfied, "both entries' scope tags are a subset of this identity")
	}
	assert.Equal(t, "s2", res.WinnerID)
	assert.Equal(t, "scope_size", res.Rule)
}

// TestExplainForIdentityOnlyConsidersSatisfyingCandidates is the direct
// demonstration of why FR-7 needs two distinct resolvers, not one: store-wide
// (ExplainEntry) s2 is the more specific entry and would win outright, but an
// identity that lacks role:investigator cannot see s2 at all -- it must
// resolve to s1 instead, uncontested, with no shadowReason rule to report.
func TestExplainForIdentityOnlyConsidersSatisfyingCandidates(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	res, err := ExplainForIdentity(ctx, dir, []string{"rig:alpha"}, "shared")
	require.NoError(t, err)
	require.Len(t, res.Candidates, 2)

	byID := map[string]IdentityCandidate{}
	for _, c := range res.Candidates {
		byID[c.EntryID] = c
	}
	assert.True(t, byID["s1"].Satisfied)
	assert.False(t, byID["s2"].Satisfied, "s2 needs role:investigator, which this identity lacks")

	assert.Equal(t, "s1", res.WinnerID)
	assert.Empty(t, res.Rule, "an uncontested sole match carries no shadowReason rule")
}

func TestExplainForIdentityNoSatisfyingCandidateReportsNoWinner(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	res, err := ExplainForIdentity(t.Context(), dir, []string{"rig:alpha"}, "shared")
	require.NoError(t, err)
	require.Len(t, res.Candidates, 1)
	assert.False(t, res.Candidates[0].Satisfied)
	assert.Empty(t, res.WinnerID)
}

// ---- Diagnose (FR-1..FR-9 aggregation) ----

// TestDiagnoseCleanStoreExitCodeZero pre-warms the index with an explicit
// Reindex before asserting zero findings: IndexStale legitimately reports
// true for an index that has never been built at all
// (TestIndexStaleDelegatesToUnexportedCheck), which is accurate but would
// otherwise make even a perfectly healthy store's very first-ever doctor run
// report a spurious index_drift finding. Every normal cairn command already
// warms the index this way via ensureFresh before a human would think to run
// doctor, so this mirrors realistic use.
func TestDiagnoseCleanStoreExitCodeZero(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	fp, err := ComputeFingerprint(ctx, Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NoError(t, err)
	require.NotEmpty(t, fp)
	writeFile(t, dir, "global/fresh.md", filesEntry("fresh-1", repo, []string{"a.go"}, fp))

	_, err = Reindex(ctx, dir)
	require.NoError(t, err)

	report, err := Diagnose(ctx, dir)
	require.NoError(t, err)
	assert.Empty(t, report.Findings)
	assert.Equal(t, 0, report.ExitCode)
	assert.Equal(t, dir, report.Store)
}

// TestDiagnoseAggregatesMultipleFindingCategoriesAndSetsExitCodeOne is FR-1's
// core claim at the full aggregation level: one malformed sibling file must
// never prevent every other check from still running and reporting its own
// findings in the same pass.
func TestDiagnoseAggregatesMultipleFindingCategoriesAndSetsExitCodeOne(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	writeFile(t, dir, "global/good.md", "+++\nid = \"good\"\ntitle = \"good\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "global/bad.md", "+++\nid = \"a\"\nno closing fence\n")
	writeFile(t, dir, "global/dup1.md", dupFixtureOne)
	writeFile(t, dir, "rig/alpha/dup2.md", dupFixtureTwo)

	report, err := Diagnose(ctx, dir)
	require.NoError(t, err, "a malformed sibling file must never abort the full diagnostic sweep")
	require.NotEmpty(t, report.Findings)
	assert.Equal(t, 1, report.ExitCode)

	categories := map[string]bool{}
	for _, f := range report.Findings {
		categories[f.Category] = true
	}
	assert.True(t, categories[CategoryMalformed], "the unterminated-frontmatter file must surface as a malformed_entry finding")
	assert.True(t, categories[CategoryDuplicateID], "the two dup-id files must surface as a duplicate_id finding")
}

func TestDiagnosePropagatesWalkError(t *testing.T) {
	dir := t.TempDir()
	// An unreadable scope dir breaks the walk itself (not a single entry's
	// parse) -- Diagnose must surface this as its own error, not silently
	// return an empty, misleadingly-clean Report. A regular file in place of
	// the directory does NOT reproduce this: walkEntries' own info.IsDir()
	// guard just skips it silently, same as a missing dir.
	blocked := filepath.Join(dir, "global")
	require.NoError(t, os.Mkdir(blocked, 0o755)) //nolint:gosec // must be traversable before being locked to 0o000 below
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "a.md"), []byte("+++\nid = \"a\"\ntitle = \"a\"\n+++\nx\n"), 0o600))
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) }) //nolint:gosec // restore traversal so t.TempDir() cleanup can remove it

	report, err := Diagnose(t.Context(), dir)
	require.Error(t, err)
	assert.Nil(t, report)
}
