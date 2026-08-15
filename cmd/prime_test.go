package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimeDefaultIsSlimMinifiedModelJSON(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A title\"\nsummary = \"A verbose summary that must not leak into prime\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, "\n"), "default output must be one minified JSON line")
	assert.NotContains(t, out, "\n  \"")
	assert.NotContains(t, out, dir, "store path is not part of the model projection")
	assert.NotContains(t, out, "verbose summary")
	assert.NotContains(t, out, "hit_count")
	assert.NotContains(t, out, "detail")
	assert.Contains(t, out, "cairn get <id>", "model JSON must not spend tokens HTML-escaping instruction syntax")

	var got primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, primeInstruction, got.Instruction)
	require.Len(t, got.Triggers, 1)
	assert.Equal(t, "a", got.Triggers[0].ID)
	require.NotNil(t, got.Triggers[0].TopicKey)
	assert.Equal(t, "topic-a", *got.Triggers[0].TopicKey)
	assert.Equal(t, []cairn.TopicCount{{TopicKey: "topic-a", Count: 1}}, got.TopicCounts)
}

func TestPrimeCarriesConflictOnEveryContestedTrigger(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"tied\"\nscope = []\ncreated_at = \"2026-08-15\"\n+++\na\n")
	seedEntry(t, dir, "global/b.md", "+++\nid = \"b\"\ntitle = \"B\"\ntopic_key = \"tied\"\nscope = []\ncreated_at = \"2026-08-15\"\n+++\nb\n")

	out, err := execRootJSON(t, "prime", "--store", dir)
	require.NoError(t, err)
	var got primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got.Triggers, 2)
	for _, trigger := range got.Triggers {
		require.NotNil(t, trigger.Conflict)
		assert.Equal(t, []string{"a", "b"}, trigger.Conflict.EntryIDs)
	}
}

func TestPrimePrettyIndentsAndHiddenJSONIsExactNoOp(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"a\"\ntitle = \"A title\"\nscope = []\n+++\nbody\n")

	compact, err := execRootJSON(t, "prime", "--store", dir)
	require.NoError(t, err)
	compat, err := execRootJSON(t, "prime", "--json", "--store", dir)
	require.NoError(t, err)
	pretty, err := execRootJSON(t, "prime", "--pretty", "--store", dir)
	require.NoError(t, err)
	assert.Equal(t, compact, compat, "--json must add neither bytes nor behavior")
	assert.Contains(t, pretty, "\n  \"instruction\"")
	assert.Less(t, len(compact), len(pretty))

	var got primeOutput
	require.NoError(t, json.Unmarshal([]byte(compact), &got))
	require.Len(t, got.Triggers, 1)
	assert.Nil(t, got.Triggers[0].TopicKey, "untopiced is explicit null, never omitted or empty")

	help := rootCmd.HelpTemplate()
	assert.NotContains(t, help, "--json")
}

func TestPrimeBareStillWorks(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--store", t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPrimeRejectsStrayPositionalArgs(t *testing.T) {
	_, err := execRoot("prime", "--store", t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestPrimeAcceptsBudgetBytesFlag(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--store", t.TempDir(), "--budget-bytes", "100")
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPrimeJSONOutputsResult(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--json", "--store", dir)
	require.NoError(t, err)

	var result primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Triggers, 1)
	assert.Equal(t, "g/a", result.Triggers[0].ID)
	require.NotNil(t, result.Triggers[0].TopicKey)
	assert.Equal(t, "topic-a", *result.Triggers[0].TopicKey)
}

func TestPrimeJSONRejectsBadIdentityTag(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--json", "--store", t.TempDir(), "--identity", "role/bad")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "role/bad", result.Error.Subject)
}

func TestPrimeJSONEmptyStoreHasEmptyArrays(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--json", "--store", t.TempDir())
	require.NoError(t, err)

	var result primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []primeTrigger{}, result.Triggers)
	assert.Equal(t, []string{}, result.Warnings)
	assert.Equal(t, []cairn.TopicCount{}, result.TopicCounts)
}

// findPrimeEmitRecord reads xdg's debug.jsonl and returns the single
// "prime_emit" record it contains. One execRootJSON("prime", ...) call also
// logs a "context" record (root's PersistentPreRunE), so this filters by
// kind rather than assuming a fixed line index or count.
func findPrimeEmitRecord(t *testing.T, xdg string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)

	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["kind"] == "prime_emit" {
			found = append(found, rec)
		}
	}
	require.Len(t, found, 1, "expected exactly one prime_emit record in:\n%s", data)
	return found[0]
}

// TestPrimeLogsPrimeEmitRecord covers crn-jkth's core acceptance criterion:
// a `cairn prime` invocation must emit a per-invocation "prime_emit" record
// whose item_ids matches the IDs actually itemized in that same
// invocation's rendered/JSON output -- the join crn-894i needs to measure
// recall hit-rate at point-of-need against retrieval_outcome.
func TestPrimeLogsPrimeEmitRecord(t *testing.T) {
	resetRunIDFlag(t)
	t.Cleanup(func() { resetRunIDFlag(t) })

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_RUN_ID", "")

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")
	seedEntry(t, dir, "global/b.md",
		"+++\nid = \"g/b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--json", "--store", dir, "--identity", "rig:web", "--run-id", "run-123")
	require.NoError(t, err)

	var result primeOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))

	rec := findPrimeEmitRecord(t, xdg)
	assert.Equal(t, "run-123", rec["run_id"])
	assert.Equal(t, []any{"rig:web"}, rec["identity_tags"])
	assert.EqualValues(t, result.TotalVisible, rec["total_visible"])
	assert.EqualValues(t, result.TotalVisible-len(result.Triggers), rec["truncated_count"])

	wantIDs := make([]any, len(result.Triggers))
	for i, item := range result.Triggers {
		wantIDs[i] = item.ID
	}
	assert.Equal(t, wantIDs, rec["item_ids"])
}
