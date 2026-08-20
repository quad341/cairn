package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetJSONIncludesReviewStatus covers exit-contract bullet 6 for get:
// EntryResult carries a review_status field in --json output, both for a
// pending entry and a merged one.
func TestGetJSONIncludesReviewStatus(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")
	seedEntry(t, dir, "global/b.md",
		"+++\nid = \"g/b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nreview_status = \"merged\"\nscope = []\n+++\nbody\n")

	outA, err := execRootJSON(t, "get", "g/a", "--json", "--store", dir)
	require.NoError(t, err)
	var resultA EntryResult
	require.NoError(t, json.Unmarshal([]byte(outA), &resultA))
	assert.Equal(t, "pending", resultA.ReviewStatus)

	outB, err := execRootJSON(t, "get", "g/b", "--json", "--store", dir)
	require.NoError(t, err)
	var resultB EntryResult
	require.NoError(t, json.Unmarshal([]byte(outB), &resultB))
	assert.Equal(t, "merged", resultB.ReviewStatus)
}

// TestGetTextShowsPendingReviewMarker covers exit-contract bullet 5 for
// get: plain-text output carries a standalone [PENDING REVIEW] marker line
// for a pending entry, and no such marker for a merged one. Standalone
// (not inline) is deliberate -- get only ever renders a single entry per
// call, so there's no multi-entry ambiguity, and a standalone line is more
// visible for a single-entry display (unlike prime's inline marker).
func TestGetTextShowsPendingReviewMarker(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")
	seedEntry(t, dir, "global/b.md",
		"+++\nid = \"g/b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nreview_status = \"merged\"\nscope = []\n+++\nbody\n")

	outA, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.Split(outA, "\n"), "[PENDING REVIEW]")

	outB, err := execRoot("get", "g/b", "--store", dir)
	require.NoError(t, err)
	assert.NotContains(t, outB, "[PENDING REVIEW]")
}

// TestListJSONIncludesReviewStatus covers exit-contract bullet 6 for list:
// listItem carries a review_status field in --json output.
func TestListJSONIncludesReviewStatus(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "list", "topic-a", "--json", "--store", dir)
	require.NoError(t, err)

	var result listOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 1)
	assert.Equal(t, "pending", result.Entries[0].ReviewStatus)
}

// TestPrimeJSONIncludesReviewStatus covers exit-contract bullet 6 for
// prime: primeTrigger carries a review_status field in --json output.
func TestPrimeJSONIncludesReviewStatus(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--json", "--store", dir)
	require.NoError(t, err)

	var result primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Triggers, 1)
	assert.Equal(t, "pending", result.Triggers[0].ReviewStatus)
}
