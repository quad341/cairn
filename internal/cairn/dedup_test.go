package cairn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dedupFindingsOfKind(findings []DedupFinding, kind string) []DedupFinding {
	var out []DedupFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func minimalEntry(id, title, summary, topicKey, scopeTOML string) string {
	return "+++\n" +
		"id = \"" + id + "\"\n" +
		"title = \"" + title + "\"\n" +
		"summary = \"" + summary + "\"\n" +
		"topic_key = \"" + topicKey + "\"\n" +
		"scope = " + scopeTOML + "\n" +
		"+++\nbody\n"
}

// TestDedupTopicKeyCollisionSameTier is acceptance criterion 1: an exact
// topic_key collision between two shared-tier entries in the same scope
// tier always produces a filed bead identifying both entry IDs.
func TestDedupTopicKeyCollisionSameTier(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/one.md", minimalEntry("rig/one", "One", "s1", "dup-key", `["rig:alpha"]`))
	writeFile(t, dir, "rig/beta/two.md", minimalEntry("rig/two", "Two", "s2", "dup-key", `["rig:beta"]`))

	findings, err := Dedup(dir)
	require.NoError(t, err)

	tk := dedupFindingsOfKind(findings, "topic_key")
	require.Len(t, tk, 1, "exactly one topic_key collision finding")
	assert.Equal(t, "rig", tk[0].Tier)
	assert.Equal(t, "dup-key", tk[0].TopicKey)
	assert.Equal(t, []string{"rig/one", "rig/two"}, tk[0].EntryIDs, "both colliding entry IDs must be identified")
}

// TestDedupTopicKeyCollisionGroupOfThree covers a key held by more than two
// entries: one finding, covering the whole group, not three pairwise ones.
func TestDedupTopicKeyCollisionGroupOfThree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", minimalEntry("g/a", "A", "sa", "shared", "[]"))
	writeFile(t, dir, "global/b.md", minimalEntry("g/b", "B", "sb", "shared", "[]"))
	writeFile(t, dir, "global/c.md", minimalEntry("g/c", "C", "sc", "shared", "[]"))

	findings, err := Dedup(dir)
	require.NoError(t, err)

	tk := dedupFindingsOfKind(findings, "topic_key")
	require.Len(t, tk, 1, "one group finding, not one per pair")
	assert.Equal(t, []string{"g/a", "g/b", "g/c"}, tk[0].EntryIDs)
}

// TestDedupTopicKeyNoCollisionAcrossTiers confirms a shared topic_key across
// tiers is CSS-style shadowing (DESIGN.md §3), not a duplicate: a rig-tier
// default and a role-tier override sharing a key must not be flagged.
func TestDedupTopicKeyNoCollisionAcrossTiers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/default.md", minimalEntry("rig/default", "Default", "d", "override-me", `["rig:alpha"]`))
	writeFile(t, dir, "role/builder/override.md", minimalEntry("role/override", "Override", "o", "override-me", `["rig:alpha", "role:builder"]`))

	findings, err := Dedup(dir)
	require.NoError(t, err)

	assert.Empty(t, dedupFindingsOfKind(findings, "topic_key"), "cross-tier shared topic_key is intentional shadowing, not a dup")
}

// TestDedupUntopicedNeverCollide mirrors entry.go's own
// TestVisibleUntopicedNeverShadow: two entries that both simply lack a
// topic_key must never be reported as colliding on "".
func TestDedupUntopicedNeverCollide(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")
	writeFile(t, dir, "global/b.md", "+++\nid = \"g/b\"\ntitle = \"B\"\nscope = []\n+++\nbody\n")

	findings, err := Dedup(dir)
	require.NoError(t, err)

	assert.Empty(t, dedupFindingsOfKind(findings, "topic_key"))
}

// TestSimilarityThresholdExamples is acceptance criterion 2: the
// content-similarity heuristic is a concrete, mechanical threshold, with
// one written passing example pair and one written failing example pair.
//
// PASS pair — two entries describing the same procedure in different
// words. Title+Summary token sets (lowercased [a-z0-9]+ runs, no stemming):
//
//	A: {configuring, the, git, pre, commit, hook, steps, to, enable,
//	    shared, for, this, repo}                              (13 tokens)
//	B: {enabling, the, shared, pre, commit, hook, how, to,
//	    configure, git, in, this, repo}                        (13 tokens)
//	intersection = {the, git, pre, commit, hook, to, shared, this, repo} = 9
//	union        = 13 + 13 - 9 = 17
//	Jaccard      = 9/17 ≈ 0.529  →  >= 0.5 threshold  →  flagged
//
// FAIL pair — A against an entry on a genuinely unrelated topic:
//
//	C: {deploying, the, worker, service, to, production, rolling,
//	    out, a, new, build, via, deploy, pipeline}              (14 tokens)
//	intersection(A,C) = {the, to} = 2
//	union             = 13 + 14 - 2 = 25
//	Jaccard           = 2/25 = 0.08  →  < 0.5 threshold  →  not flagged
func TestSimilarityThresholdExamples(t *testing.T) {
	a := &Entry{
		Title:   "Configuring the git pre-commit hook",
		Summary: "Steps to enable the shared pre-commit hook for this repo",
	}
	b := &Entry{
		Title:   "Enabling the shared pre-commit hook",
		Summary: "How to configure the git pre-commit hook in this repo",
	}
	c := &Entry{
		Title:   "Deploying the worker service to production",
		Summary: "Rolling out a new worker build via the deploy pipeline",
	}

	pass := similarity(a, b)
	assert.InDelta(t, 9.0/17.0, pass, 0.001, "hand-computed Jaccard for the passing pair")
	assert.GreaterOrEqualf(t, pass, dedupSimilarityThreshold, "passing pair must clear the threshold (got %.4f)", pass)

	fail := similarity(a, c)
	assert.InDelta(t, 2.0/25.0, fail, 0.001, "hand-computed Jaccard for the failing pair")
	assert.Lessf(t, fail, dedupSimilarityThreshold, "failing pair must not clear the threshold (got %.4f)", fail)
}

// TestDedupContentSimilarityPair is AC2 exercised end-to-end through Dedup:
// the passing pair from TestSimilarityThresholdExamples, stored as real
// shared-tier entries with distinct topic_keys, must produce exactly one
// content finding identifying both entries.
func TestDedupContentSimilarityPair(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", minimalEntry(
		"g/hook-a", "Configuring the git pre-commit hook",
		"Steps to enable the shared pre-commit hook for this repo", "topic-a", "[]"))
	writeFile(t, dir, "global/b.md", minimalEntry(
		"g/hook-b", "Enabling the shared pre-commit hook",
		"How to configure the git pre-commit hook in this repo", "topic-b", "[]"))

	findings, err := Dedup(dir)
	require.NoError(t, err)

	content := dedupFindingsOfKind(findings, "content")
	require.Len(t, content, 1)
	assert.Equal(t, []string{"g/hook-a", "g/hook-b"}, content[0].EntryIDs)
	assert.InDelta(t, 9.0/17.0, content[0].Similarity, 0.001)
}

// TestDedupDistinctLowOverlapNoBead is acceptance criterion 5: two entries
// with genuinely distinct topic_keys and low content overlap produce no
// bead (no finding of either kind).
func TestDedupDistinctLowOverlapNoBead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md", minimalEntry(
		"g/hook", "Configuring the git pre-commit hook",
		"Steps to enable the shared pre-commit hook for this repo", "topic-a", "[]"))
	writeFile(t, dir, "global/b.md", minimalEntry(
		"g/deploy", "Deploying the worker service to production",
		"Rolling out a new worker build via the deploy pipeline", "topic-b", "[]"))

	findings, err := Dedup(dir)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestDedupSkipsContentPairAlreadySharingTopicKey confirms a pair that
// shares a non-empty topic_key is never double-reported as a "content"
// finding too, when their scopes are in a genuine superset relationship
// (role/hook's scope is a strict superset of rig/hook's): that relationship
// is already shadow()/ShadowMap's, not this step's, to describe.
func TestDedupSkipsContentPairAlreadySharingTopicKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/default.md", minimalEntry(
		"rig/hook", "Configuring the git pre-commit hook",
		"Steps to enable the shared pre-commit hook for this repo", "shared-hook", `["rig:alpha"]`))
	writeFile(t, dir, "role/builder/override.md", minimalEntry(
		"role/hook", "Enabling the shared pre-commit hook",
		"How to configure the git pre-commit hook in this repo", "shared-hook", `["rig:alpha", "role:builder"]`))

	findings, err := Dedup(dir)
	require.NoError(t, err)
	assert.Empty(t, findings, "same topic_key with a genuine superset scope relationship is legitimate shadowing; no separate content finding")
}

// TestDedupCrossTierSharedTopicKeyIncomparableScopesGetsContentCheck covers
// a gap in an earlier version of this file: a pair that shares a topic_key
// but whose scopes are incomparable (neither a superset of the other) is
// NOT legitimate ShadowMap shadowing (entry.go's own
// TestShadowMapIncomparableScopesNeverShadow establishes this), so it must
// still be evaluated by the ordinary content-similarity check rather than
// being unconditionally excluded just because the keys match.
func TestDedupCrossTierSharedTopicKeyIncomparableScopesGetsContentCheck(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/one.md", minimalEntry(
		"rig/hook", "Configuring the git pre-commit hook",
		"Steps to enable the shared pre-commit hook for this repo", "shared-hook", `["rig:alpha"]`))
	writeFile(t, dir, "role/builder/two.md", minimalEntry(
		"role/hook", "Enabling the shared pre-commit hook",
		"How to configure the git pre-commit hook in this repo", "shared-hook", `["role:builder"]`))

	findings, err := Dedup(dir)
	require.NoError(t, err)

	content := dedupFindingsOfKind(findings, "content")
	require.Len(t, content, 1, "incomparable scopes are not legitimate shadowing; the shared key must not suppress the content check")
	assert.Equal(t, []string{"rig/hook", "role/hook"}, content[0].EntryIDs)
	assert.InDelta(t, 9.0/17.0, content[0].Similarity, 0.001)
	assert.Contains(t, content[0].Detail, "shared-hook", "the coincidental key match is itself worth surfacing in the finding detail")
}

// TestDedupCrossTierSharedTopicKeyIncomparableScopesLowSimilarityStillNoBead
// bounds the fix above: an incomparable-scope shared-key pair is routed
// into the ordinary content check, not unconditionally flagged regardless
// of content — with low title+summary overlap it still produces no
// finding. (Whether an exact cross-tier key collision with incomparable
// scopes should be its own always-flagged case, the way AC1 treats a
// same-tier collision, is a judgment call intentionally left open here —
// see the review notes.)
func TestDedupCrossTierSharedTopicKeyIncomparableScopesLowSimilarityStillNoBead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/one.md", minimalEntry(
		"rig/hook", "Configuring the git pre-commit hook",
		"Steps to enable the shared pre-commit hook for this repo", "shared-key", `["rig:alpha"]`))
	writeFile(t, dir, "role/builder/two.md", minimalEntry(
		"role/deploy", "Deploying the worker service to production",
		"Rolling out a new worker build via the deploy pipeline", "shared-key", `["role:builder"]`))

	findings, err := Dedup(dir)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestDedupAgentTierExcluded mirrors Sweep's own remit: agent/ private
// entries are never scanned, even when they'd otherwise collide.
func TestDedupAgentTierExcluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent/bot1/a.md", minimalEntry("agent/a", "A", "sa", "dup", "[]"))
	writeFile(t, dir, "agent/bot2/b.md", minimalEntry("agent/b", "B", "sb", "dup", "[]"))

	findings, err := Dedup(dir)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestDedupNeverWrites is acceptance criterion 4, verified behaviorally in
// addition to by inspection (no WriteBack/os.WriteFile/os.Remove call
// appears anywhere in dedup.go): every entry file's bytes on disk are
// identical before and after a Dedup call that does produce findings of
// both kinds.
func TestDedupNeverWrites(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		writeFile(t, dir, "global/a.md", minimalEntry(
			"g/hook-a", "Configuring the git pre-commit hook",
			"Steps to enable the shared pre-commit hook for this repo", "topic-a", "[]")),
		writeFile(t, dir, "global/b.md", minimalEntry(
			"g/hook-b", "Enabling the shared pre-commit hook",
			"How to configure the git pre-commit hook in this repo", "topic-b", "[]")),
		writeFile(t, dir, "rig/alpha/one.md", minimalEntry("rig/one", "One", "s1", "dup-key", `["rig:alpha"]`)),
		writeFile(t, dir, "rig/beta/two.md", minimalEntry("rig/two", "Two", "s2", "dup-key", `["rig:beta"]`)),
	}

	before := make(map[string][]byte, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		before[p] = b
	}

	findings, err := Dedup(dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings, "this fixture must actually produce findings for the no-write check to mean anything")

	for _, p := range paths {
		after, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, before[p], after, "Dedup must never modify an entry file: %s", filepath.Base(p))
	}
}

// TestDedupEmptyStoreNoPanic guards the zero-entries edge case; Dedup must
// return an empty, non-nil-shaped result rather than erroring.
func TestDedupEmptyStoreNoPanic(t *testing.T) {
	dir := t.TempDir()
	findings, err := Dedup(dir)
	require.NoError(t, err)
	assert.Empty(t, findings)
}
