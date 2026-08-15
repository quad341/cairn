package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchLogsItsOutcome pins the middle of the funnel. prime_emit records
// what was surfaced at session start and retrieval_outcome records what was
// eventually opened; without a search record in between, an agent that
// searched and was unconvinced looks identical to an agent that never
// searched -- and those two call for opposite fixes.
func TestSearchLogsItsOutcome(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rig", "alpha"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rig", "alpha", "a1.md"), []byte(
		"+++\nid = \"a1\"\ntitle = \"Zygote reaping\"\ntopic_key = \"zygote-reaping\"\n"+
			"scope = [\"rig:alpha\"]\n+++\nThe supervisor reaps orphaned zygote processes.\n"), 0o600))

	// Route the real log to a temp state dir rather than injecting a logger:
	// rootCmd's PersistentPreRunE installs its own logger over the context,
	// so an injected one is discarded. Going through the file exercises the
	// path an operator will actually audit.
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	rootCmd.SetArgs([]string{
		"search", "zygote reaping quaxolotl",
		"--store", dir, "--identity", "rig:alpha",
	})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, rootCmd.ExecuteContext(context.Background()))

	logPath := filepath.Join(state, "cairn", "debug.jsonl")
	raw, err := os.ReadFile(logPath) //nolint:gosec // test-owned temp path
	require.NoError(t, err, "search must write an auditable log record")

	var rec map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		if m["kind"] == "search_outcome" {
			rec = m
		}
	}
	require.NotNil(t, rec, "a search must emit a search_outcome record")

	assert.Equal(t, "zygote reaping quaxolotl", rec["query"])
	assert.Contains(t, rec["hit_ids"], "a1")
	// The auditable part: a term matching nothing the caller can see is
	// recorded, because that is the most direct signal of what is worth
	// writing down next.
	assert.Contains(t, rec["unmatched_terms"], "quaxolotl")
	assert.NotContains(t, rec["unmatched_terms"], "zygote")
	assert.NotEmpty(t, rec["verdict"])
}
