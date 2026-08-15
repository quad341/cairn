package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchDefaultIsSlimMinifiedModelJSON(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"Database recovery\"\nsummary = \"Restore the database safely\"\nscope = []\n+++\nRecovery steps for a database.\n")

	out, err := execRootJSON(t, "search", "database recovery", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, "\n"))
	assert.NotContains(t, out, dir)
	assert.NotContains(t, out, `"identity"`)
	assert.NotContains(t, out, `"scope"`)
	assert.NotContains(t, out, `"hit_count"`)
	assert.NotContains(t, out, `"detail"`)

	var got searchOutput
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, searchInstruction, got.Instruction)
	require.Len(t, got.Hits, 1)
	assert.Nil(t, got.Hits[0].TopicKey)
	assert.Nil(t, got.Hits[0].Conflict)
}

func TestSearchPrettyAndHiddenJSONNoOp(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"Needle\"\nscope = []\n+++\nneedle\n")

	compact, err := execRootJSON(t, "search", "needle", "--store", dir)
	require.NoError(t, err)
	compat, err := execRootJSON(t, "search", "needle", "--json", "--store", dir)
	require.NoError(t, err)
	pretty, err := execRootJSON(t, "search", "needle", "--pretty", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, compact, compat)
	assert.Contains(t, pretty, "\n  \"instruction\"")
}

func TestSearchNoCandidatesUsesExplicitEmptyHits(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"Haystack\"\nscope = []\n+++\nhaystack\n")

	out, err := execRootJSON(t, "search", "needle", "--store", dir)
	require.NoError(t, err)
	var got searchOutput
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, []searchItem{}, got.Hits)
	assert.Equal(t, 0, got.TotalMatched)
	assert.Equal(t, 1, got.TotalVisible)
}

func TestSearchCarriesConflictOnEveryContestedHit(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"Shared needle A\"\ntopic_key = \"tied\"\nscope = []\ncreated_at = \"2026-08-15\"\n+++\nneedle alpha\n")
	seedEntry(t, dir, "global/b.md", "+++\nid = \"b\"\ntitle = \"Shared needle B\"\ntopic_key = \"tied\"\nscope = []\ncreated_at = \"2026-08-15\"\n+++\nneedle beta\n")

	out, err := execRootJSON(t, "search", "needle", "--store", dir)
	require.NoError(t, err)
	var got searchOutput
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got.Hits, 2)
	for _, hit := range got.Hits {
		require.NotNil(t, hit.Conflict)
		assert.Equal(t, []string{"a", "b"}, hit.Conflict.EntryIDs)
	}
}
