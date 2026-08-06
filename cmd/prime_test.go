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

func TestPrimeBareStillWorks(t *testing.T) {
	out, err := execRoot("prime", "--store", t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPrimeRejectsStrayPositionalArgs(t *testing.T) {
	_, err := execRoot("prime", "--store", t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestPrimeAcceptsBudgetBytesFlag(t *testing.T) {
	out, err := execRoot("prime", "--store", t.TempDir(), "--budget-bytes", "100")
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPrimeJSONOutputsResult(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--json", "--store", dir)
	require.NoError(t, err)

	var result cairn.PrimeResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Items, 1)
	assert.Equal(t, "g/a", result.Items[0].ID)
	assert.Equal(t, "topic-a", result.Items[0].TopicKey)
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

	var result cairn.PrimeResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []cairn.PrimeItem{}, result.Items)
	assert.Equal(t, []string{}, result.Warnings)
	assert.Equal(t, []string{}, result.Identity)
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

	var result cairn.PrimeResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))

	rec := findPrimeEmitRecord(t, xdg)
	assert.Equal(t, "run-123", rec["run_id"])
	assert.Equal(t, []any{"rig:web"}, rec["identity_tags"])
	assert.EqualValues(t, result.TotalVisible, rec["total_visible"])
	assert.EqualValues(t, result.TruncatedCount, rec["truncated_count"])

	wantIDs := make([]any, len(result.Items))
	for i, item := range result.Items {
		wantIDs[i] = item.ID
	}
	assert.Equal(t, wantIDs, rec["item_ids"])
}
