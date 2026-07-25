package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetVersionFlag clears the shared rootCmd's --version/-v flag, mirroring
// resetJSONFlag (commands_json_test.go): rootCmd is a package-level
// singleton, so a bool flag's value set by one execRoot("--version") call
// otherwise leaks into every later test that invokes bare cairn expecting
// the help fallback.
func resetVersionFlag() error {
	f := rootCmd.Flags().Lookup("version")
	if f == nil {
		return errors.New("version flag not registered on rootCmd")
	}
	if err := f.Value.Set("false"); err != nil {
		return err
	}
	f.Changed = false
	return nil
}

func TestVersionCommandRuns(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, rootCmd.Execute())
}

// TestVersionThreeSpellingsProduceByteIdenticalOutput covers FR-5: `cairn
// version`, `cairn --version`, and `cairn -v` must be byte-identical in
// human mode, by construction (all three call the same printVersion), not
// by convention.
func TestVersionThreeSpellingsProduceByteIdenticalOutput(t *testing.T) {
	sub, err := execRoot("version")
	require.NoError(t, err)

	long, err := execRoot("--version")
	require.NoError(t, err)
	require.NoError(t, resetVersionFlag())

	short, err := execRoot("-v")
	require.NoError(t, err)
	require.NoError(t, resetVersionFlag())

	assert.NotEmpty(t, sub)
	assert.Equal(t, sub, long)
	assert.Equal(t, sub, short)
}

// TestVersionJSONThreeSpellingsProduceByteIdenticalOutput covers FR-5's JSON
// half: --json output must match across all three spellings too.
func TestVersionJSONThreeSpellingsProduceByteIdenticalOutput(t *testing.T) {
	sub, err := execRootJSON(t, "version", "--json")
	require.NoError(t, err)

	long, err := execRootJSON(t, "--version", "--json")
	require.NoError(t, err)
	require.NoError(t, resetVersionFlag())

	var result VersionResult
	require.NoError(t, json.Unmarshal([]byte(sub), &result))
	assert.Equal(t, version, result.Version)
	assert.Equal(t, commit, result.Commit)
	assert.Equal(t, date, result.Date)
	assert.Equal(t, sub, long)
}

// TestBareCommandFallsBackToHelp covers FR-6: bare `cairn` (no subcommand,
// no --version) must keep printing help and exiting success, not regress
// now that rootCmd has a real RunE. Cobra's Help() writes via
// cmd.OutOrStdout(), not fmt.Print*, so this needs execRootJSON's capture
// (which reads that buffer back) rather than execRoot's os.Stdout pipe
// swap, which only sees direct fmt.Print* output and would otherwise see
// nothing here.
func TestBareCommandFallsBackToHelp(t *testing.T) {
	out, err := execRootJSON(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Available Commands:")
}

// TestUnknownSubcommandStillErrors guards against a regression Cobra's
// Runnable()-gated help fallback makes easy to introduce by accident: giving
// rootCmd a RunE must not make an unrecognized subcommand name silently fall
// through to it instead of erroring.
func TestUnknownSubcommandStillErrors(t *testing.T) {
	_, err := execRoot("bogus-command")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestVersionCommandPrintsLogPath(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	rootCmd.SetArgs([]string{"version"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	out := captureStdout(t, func() {
		require.NoError(t, rootCmd.Execute())
	})

	assert.Contains(t, out, filepath.Join(xdg, "cairn", "debug.jsonl"))
}
