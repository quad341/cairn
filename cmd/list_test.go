package cmd

import (
	"bytes"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runList executes "cairn list <topic>" (plus any extra args) against the
// shared rootCmd, mirroring runStatus/runMap's --identity Changed-bit reset.
func runList(t *testing.T, dir, topic string, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"list", topic, "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

const (
	listSingleEntry    = "+++\nid = \"lst1\"\ntitle = \"List Target\"\nsummary = \"summary text\"\ntopic_key = \"list-topic\"\nscope = [\"rig:list-cmd\"]\n+++\nbody\n"
	listUntopicedEntry = "+++\nid = \"lst-u1\"\ntitle = \"Untopiced Target\"\nsummary = \"no topic here\"\nscope = [\"rig:list-cmd\"]\n+++\nbody\n"
)

func TestListCommandPrintsMatchedEntry(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/list-cmd/lst1.md", listSingleEntry)

	var err error
	out := captureStdout(t, func() { err = runList(t, dir, "list-topic", "--identity", "rig:list-cmd") })
	require.NoError(t, err)
	assert.Contains(t, out, "lst1")
	assert.Contains(t, out, "List Target")
	assert.Contains(t, out, "summary text")
}

func TestListCommandErrorsOnZeroMatches(t *testing.T) {
	err := runList(t, t.TempDir(), "no-such-topic", "--identity", "rig:list-cmd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no entries found for topic "no-such-topic"`)
}

func TestListCommandTranslatesUntopicedLabel(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/list-cmd/lst-u1.md", listUntopicedEntry)

	var err error
	out := captureStdout(t, func() { err = runList(t, dir, cairn.UntopicedLabel, "--identity", "rig:list-cmd") })
	require.NoError(t, err)
	assert.Contains(t, out, "lst-u1")
	assert.Contains(t, out, "Untopiced Target")
}
