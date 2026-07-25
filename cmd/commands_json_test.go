package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execRootJSON runs the cairn CLI in-process with args (mirroring execRoot in
// ergonomics_scenario.go) and returns cmd.OutOrStdout()'s buffered content
// instead of bare stdout: --json output is written via emitJSON/emitError,
// which target cmd.OutOrStdout(), not fmt.Println/Printf, so execRoot's
// os.Stdout pipe capture would see nothing.
func execRootJSON(t *testing.T, args ...string) (string, error) {
	t.Helper()
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	var buf bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestStatusJSONOutputsItems(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "status", "--json", "--store", dir)
	require.NoError(t, err)

	var items []StatusItem
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "g/a", items[0].ID)
	assert.Equal(t, "topic-a", items[0].TopicKey)
	assert.NotEmpty(t, items[0].Freshness.Status)
}

func TestStatusJSONEmptyStoreIsEmptyArray(t *testing.T) {
	dir := t.TempDir()

	out, err := execRootJSON(t, "status", "--json", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, "[]\n", out)
}

func TestStatusJSONRejectsIdentity(t *testing.T) {
	out, err := execRootJSON(t, "status", "--json", "--store", t.TempDir(), "--identity", "rig:alpha")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Contains(t, result.Error.Message, "does not filter by identity")
}

func TestStatusJSONAnnotatesShadowedEntries(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/less-specific.md", shadowedByScoped)
	seedEntry(t, dir, "role/investigator/more-specific.md", shadowsScoped)

	out, err := execRootJSON(t, "status", "--json", "--store", dir)
	require.NoError(t, err)

	var items []StatusItem
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	require.Len(t, items, 2)

	byID := make(map[string]StatusItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	assert.Equal(t, "more-specific", byID["less-specific"].ShadowedBy)
	assert.Empty(t, byID["more-specific"].ShadowedBy)
}

func TestGetJSONOutputsEntryResult(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nkind = \"remediation\"\nauto_actionable = true\nscope = []\n+++\nbody text\n")

	out, err := execRootJSON(t, "get", "g/a", "--json", "--store", dir)
	require.NoError(t, err)

	var result EntryResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "g/a", result.ID)
	assert.Equal(t, "A", result.Title)
	assert.Equal(t, "topic-a", result.TopicKey)
	assert.Equal(t, "remediation", result.Kind)
	assert.True(t, result.AutoActionable)
	assert.Equal(t, []cairn.DedupFinding{}, result.Conflicts)
	assert.Contains(t, result.Body, "body text")
}

func TestGetJSONNotFoundEmitsErrorEnvelope(t *testing.T) {
	out, err := execRootJSON(t, "get", "missing-id", "--json", "--store", t.TempDir())
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryNotFound, result.Error.Category)
	assert.Equal(t, "missing-id", result.Error.Subject)
}

func TestGetJSONReportsConflicts(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/hook.md",
		"+++\nid = \"rig/hook\"\ntitle = \"Configuring the hook\"\ntopic_key = \"shared-hook\"\nscope = [\"rig:alpha\"]\n+++\nbody\n")
	seedEntry(t, dir, "role/builder/hook.md",
		"+++\nid = \"role/hook\"\ntitle = \"Totally unrelated deploy pipeline notes\"\ntopic_key = \"shared-hook\"\nscope = [\"role:builder\"]\n+++\nbody\n")

	out, err := execRootJSON(t, "get", "role/hook", "--json", "--store", dir, "--identity", "rig:alpha,role:builder")
	require.NoError(t, err)

	var result EntryResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "topic_key", result.Conflicts[0].Kind)
	assert.Contains(t, result.Conflicts[0].EntryIDs, "rig/hook")
}

func TestGetJSONRejectsBadIdentityTag(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "get", "g/a", "--json", "--store", dir, "--identity", "role/bad")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "role/bad", result.Error.Subject)
}

func TestMapJSONOutputsTopics(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = [\"rig:alpha\"]\n+++\nbody\n")

	out, err := execRootJSON(t, "map", "--json", "--store", dir, "--identity", "rig:alpha")
	require.NoError(t, err)

	var result MapResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []string{"rig:alpha"}, result.Identity)
	assert.Equal(t, 1, result.Total)
	require.Len(t, result.Topics, 1)
	assert.Equal(t, "topic-a", result.Topics[0].TopicKey)
	assert.Equal(t, 1, result.Topics[0].Count)
}

func TestMapJSONEmptyIdentityIsEmptyArray(t *testing.T) {
	dir := t.TempDir()

	out, err := execRootJSON(t, "map", "--json", "--store", dir)
	require.NoError(t, err)

	var result MapResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []string{}, result.Identity)
	assert.Equal(t, 0, result.Total)
	assert.Equal(t, []MapTopicCount{}, result.Topics)
}
