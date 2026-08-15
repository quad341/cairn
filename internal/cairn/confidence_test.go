package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confEntry = "+++\nid = \"cf1\"\ntitle = \"Zygote reaping\"\ntopic_key = \"zygote-reaping\"\n" +
	"scope = [\"rig:alpha\"]\n+++\nThe supervisor reaps orphaned zygote processes on restart.\n"

// TestConfidenceAbstainsWhenNoEntryCoversMuchOfTheQuery pins the abstain
// path: the query's meaningful weight is spread across many entries and the
// best of them carries only a small share, which is what a query the store
// cannot answer looks like.
//
// Note what this does NOT test, because the metric deliberately does not
// work that way: a term matching nothing anywhere in the store is excluded
// from the coverage denominator rather than counted against the top hit.
// A single-entry store where most query terms match nothing therefore yields
// coverage 1.0, not 0.1 -- correctly, since the one entry covered everything
// the store had to offer. UnmatchedTerms carries that other signal.
func TestConfidenceAbstainsWhenNoEntryCoversMuchOfTheQuery(t *testing.T) {
	dir := t.TempDir()
	for i, term := range []string{"zygote", "certificate", "rotation", "ingress", "webhook", "staging"} {
		writeFile(t, dir, "rig/alpha/e"+string(rune('a'+i))+".md",
			"+++\nid = \"e"+string(rune('a'+i))+"\"\ntitle = \"E\"\n"+
				"topic_key = \"topic-"+string(rune('a'+i))+"\"\nscope = [\"rig:alpha\"]\n+++\n"+term+"\n")
	}

	res, err := Search(t.Context(), dir,
		"zygote certificate rotation ingress webhook staging", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.Equal(t, VerdictNone, res.Confidence.Verdict,
		"no entry carries more than a sixth of the query's weight")
	assert.Less(t, res.Confidence.Coverage, noneCoverage)
}

// TestConfidenceReportsCandidatesOnAWellCoveredQuery is the other side: when
// the top hit carries most of the query's meaning, search must not abstain.
// Suppressing a correct answer is the more expensive error -- it costs the
// re-derivation AND teaches the agent that cairn has nothing.
func TestConfidenceReportsCandidatesOnAWellCoveredQuery(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/cf1.md", confEntry)

	res, err := Search(t.Context(), dir, "zygote reaping", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.Equal(t, VerdictCandidates, res.Confidence.Verdict)
	require.NotEmpty(t, res.Hits)
}

// TestConfidenceReportsUnmatchedTermsSeparately pins that terms matching
// nothing anywhere are surfaced in their own field. They are evidence about
// the store's gaps, not about the top hit, and an agent deciding whether to
// write a new entry needs to see them.
func TestConfidenceReportsUnmatchedTermsSeparately(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/cf1.md", confEntry)

	res, err := Search(t.Context(), dir, "zygote quaxolotl", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Confidence.UnmatchedTerms, "quaxolotl")
	assert.NotContains(t, res.Confidence.UnmatchedTerms, "zygote")
}

// TestConfidenceOnEmptyResultAbstains pins that a query matching nothing at
// all reports none rather than an empty-but-confident result.
func TestConfidenceOnEmptyResultAbstains(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/cf1.md", confEntry)

	res, err := Search(t.Context(), dir, "quaxolotl", []string{"rig:alpha"}, 0)
	require.NoError(t, err)
	assert.Equal(t, VerdictNone, res.Confidence.Verdict)
	assert.Empty(t, res.Hits)
}
