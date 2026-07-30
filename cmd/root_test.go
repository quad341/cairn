package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain isolates every test in this package from the real XDG state
// directory: PersistentPreRunE (see root.go) logs a context record on every
// rootCmd.Execute() call, and dozens of existing tests in this package
// invoke it. Without this, `go test ./cmd/...` would append to the
// developer's actual ~/.local/state/cairn/debug.jsonl on every run.
func TestMain(m *testing.M) {
	_ = os.Setenv("XDG_STATE_HOME", filepath.Join(os.TempDir(), "cairn-cmd-test-state"))
	os.Exit(m.Run())
}

func TestStorePathWithSourceFlag(t *testing.T) {
	orig := storeFlag
	defer func() { storeFlag = orig }()
	storeFlag = "/some/path"
	t.Setenv("CAIRN_STORE", "/should/be/ignored")

	path, source := storePathWithSource()
	assert.Equal(t, "/some/path", path)
	assert.Equal(t, "flag", source)
}

func TestStorePathWithSourceEnv(t *testing.T) {
	orig := storeFlag
	defer func() { storeFlag = orig }()
	storeFlag = ""
	t.Setenv("CAIRN_STORE", "/env/path")

	path, source := storePathWithSource()
	assert.Equal(t, "/env/path", path)
	assert.Equal(t, "env", source)
}

func TestStorePathWithSourceDefault(t *testing.T) {
	orig := storeFlag
	defer func() { storeFlag = orig }()
	storeFlag = ""
	t.Setenv("CAIRN_STORE", "")

	path, source := storePathWithSource()
	assert.Equal(t, "", path)
	assert.Equal(t, "default", source)
}

// TestRootRefusesWhenNoStoreConfigured is the acceptance test for crn-jzhd:
// with neither --store nor $CAIRN_STORE set, cairn must refuse to run a
// store-touching command rather than silently operating on the current
// directory (see cmd/root.go's PersistentPreRunE gate).
func TestRootRefusesWhenNoStoreConfigured(t *testing.T) {
	resetRootFlagsForTest(t)
	orig := storeFlag
	storeFlag = ""
	defer func() { storeFlag = orig }()
	t.Setenv("CAIRN_STORE", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	rootCmd.SetArgs([]string{"status"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cairn store configured")
}

// TestRootExemptsVersionFromStoreGate confirms commands that never touch
// the store (e.g. "version") still run with no store configured -- the
// gate must not block the entire CLI, only the commands that need a store.
func TestRootExemptsVersionFromStoreGate(t *testing.T) {
	resetRootFlagsForTest(t)
	orig := storeFlag
	storeFlag = ""
	defer func() { storeFlag = orig }()
	t.Setenv("CAIRN_STORE", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	rootCmd.SetArgs([]string{"version"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	require.NoError(t, rootCmd.Execute())
}

func TestIdentityWithSourceFlag(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "should-be-ignored")
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("identity", nil, "")
	require.NoError(t, cmd.Flags().Parse([]string{"--identity", "rig:web,role:reviewer"}))

	tags, source := identityWithSource(cmd)
	assert.Equal(t, []string{"rig:web", "role:reviewer"}, tags)
	assert.Equal(t, "flag", source)
}

func TestIdentityWithSourceEnv(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:web role:reviewer")
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("identity", nil, "")

	tags, source := identityWithSource(cmd)
	assert.Equal(t, []string{"rig:web", "role:reviewer"}, tags)
	assert.Equal(t, "env", source)
}

func TestIdentityWithSourceDefault(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "")
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("identity", nil, "")

	tags, source := identityWithSource(cmd)
	assert.Nil(t, tags)
	assert.Equal(t, "default", source)
}

// resetRootFlagsForTest restores rootCmd's shared persistent-flag state
// (identity's Changed bit, traceFlag) around the integration tests below, so
// they don't leak into each other or into unrelated tests in this package --
// mirroring the existing runStatus helper's identity-flag reset in
// commands_test.go.
func resetRootFlagsForTest(t *testing.T) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	traceFlag = false
	t.Cleanup(func() { traceFlag = false })
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestRootLogsContextRecordUnconditionally(t *testing.T) {
	resetRootFlagsForTest(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	dir := t.TempDir()
	rootCmd.SetArgs([]string{"status", "--store", dir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, rootCmd.Execute())

	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines)

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))
	assert.Equal(t, "context", rec["kind"])
	assert.Equal(t, version, rec["version"])
	assert.Equal(t, commit, rec["commit"])
	assert.Equal(t, dir, rec["store_path"])
	assert.Equal(t, "flag", rec["store_source"])
	assert.Nil(t, rec["identity_tags"])
	assert.Equal(t, "default", rec["identity_source"])
}

func TestTraceFlagMirrorsContextRecordToStderr(t *testing.T) {
	resetRootFlagsForTest(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	dir := t.TempDir()
	rootCmd.SetArgs([]string{"status", "--store", dir, "--trace"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	stderr := captureStderr(t, func() {
		require.NoError(t, rootCmd.Execute())
	})

	assert.Contains(t, stderr, `"kind":"context"`)
}

func TestRootHelpDocumentsLogPath(t *testing.T) {
	assert.Contains(t, rootCmd.Long, filepath.Join("cairn", "debug.jsonl"))
}
