package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runStatus executes "cairn status" (plus any extra args) against the shared
// rootCmd. It resets the --identity flag's Changed bit first: rootCmd is a
// package-level singleton, so pflag's Changed state otherwise leaks across
// tests that run in the same binary.
func runStatus(t *testing.T, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"status", "--store", t.TempDir()}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

func TestStatusRejectsExplicitIdentityFlag(t *testing.T) {
	err := runStatus(t, "--identity", "rig:alpha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusRejectsIdentityEnv(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha")
	err := runStatus(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusBareIsUnchanged(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "")
	err := runStatus(t)
	require.NoError(t, err)
}
