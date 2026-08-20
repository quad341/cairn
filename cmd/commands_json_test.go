package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetJSONFlag clears the shared rootCmd's --json flag, mirroring
// resetIdentityFlag: rootCmd is a package-level singleton, so a bool flag's
// value from one execRootJSON call otherwise leaks into every later test in
// the binary that never mentions --json at all (their JSON output would then
// silently go to cmd.OutOrStdout() instead of the bare stdout those tests
// capture, and they'd see empty output). Set("false") replaces a bool
// flag's value outright -- no SliceValue.Replace trick needed here.
func resetJSONFlag() error {
	f := rootCmd.PersistentFlags().Lookup("json")
	if f == nil {
		return errors.New("json flag not registered on rootCmd")
	}
	if err := f.Value.Set("false"); err != nil {
		return err
	}
	f.Changed = false
	return nil
}

func resetPrettyFlag() error {
	f := rootCmd.PersistentFlags().Lookup("pretty")
	if f == nil {
		return errors.New("pretty flag not registered on rootCmd")
	}
	if err := f.Value.Set("false"); err != nil {
		return err
	}
	f.Changed = false
	return nil
}

// execRootJSON runs the cairn CLI in-process with args (mirroring execRoot in
// ergonomics_scenario.go) and returns cmd.OutOrStdout()'s buffered content
// instead of bare stdout: --json output is written via emitJSON/emitError,
// which target cmd.OutOrStdout(), not fmt.Println/Printf, so execRoot's
// os.Stdout pipe capture would see nothing.
func execRootJSON(t *testing.T, args ...string) (string, error) {
	t.Helper()
	require.NoError(t, resetIdentityFlag())
	require.NoError(t, resetJSONFlag())
	require.NoError(t, resetPrettyFlag())
	t.Cleanup(func() {
		_ = resetIdentityFlag()
		_ = resetJSONFlag()
		_ = resetPrettyFlag()
	})

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

// TestGetJSONReportsRedirectedFrom is crn-evw98.3's JSON-mode counterpart
// to TestGetRedirectsToCorrectionWhenOriginalIsRequested: get --json on an
// entry that another entry Corrects must return the corrector's own
// EntryResult, with RedirectedFrom naming the originally-requested id.
func TestGetJSONReportsRedirectedFrom(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/orig.md",
		"+++\nid = \"orig\"\ntitle = \"Old fact\"\ntopic_key = \"topic-old\"\nscope = []\n+++\nthe old, wrong claim\n")
	seedEntry(t, dir, "global/fix.md",
		"+++\nid = \"fix\"\ntitle = \"Corrected fact\"\ntopic_key = \"topic-new\"\ncorrects = \"orig\"\nscope = []\n+++\nthe corrected claim\n")

	out, err := execRootJSON(t, "get", "orig", "--json", "--store", dir)
	require.NoError(t, err)

	var result EntryResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "fix", result.ID, "the corrector's own id must be reported, not the superseded entry's")
	assert.Equal(t, "Corrected fact", result.Title)
	assert.Equal(t, "orig", result.RedirectedFrom, "RedirectedFrom must name the originally-requested id")
}

// TestGetJSONOmitsRedirectedFromWhenNoCorrectionExists is the JSON-mode
// negative case: RedirectedFrom must be absent (omitempty) when no
// redirect occurred, not present-but-empty.
func TestGetJSONOmitsRedirectedFromWhenNoCorrectionExists(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "get", "g/a", "--json", "--store", dir)
	require.NoError(t, err)
	assert.NotContains(t, out, "redirected_from")
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

func TestListJSONOutputsResults(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\nsummary = \"summary text\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "list", "topic-a", "--json", "--store", dir)
	require.NoError(t, err)

	var result listOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 1)
	assert.Equal(t, listInstruction, result.Instruction)
	require.NotNil(t, result.TopicKey)
	assert.Equal(t, "topic-a", *result.TopicKey)
	assert.Equal(t, "g/a", result.Entries[0].ID)
	assert.Equal(t, "A", result.Entries[0].Title)
	assert.NotEmpty(t, result.Entries[0].Freshness)
	assert.NotContains(t, out, "summary text")
}

func TestListJSONNotFoundEmitsErrorEnvelope(t *testing.T) {
	out, err := execRootJSON(t, "list", "no-such-topic", "--json", "--store", t.TempDir())
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryNotFound, result.Error.Category)
	assert.Equal(t, "no-such-topic", result.Error.Subject)
}

func TestListJSONRejectsBadIdentityTag(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "list", "topic-a", "--json", "--store", dir, "--identity", "role/bad")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "role/bad", result.Error.Subject)
}
