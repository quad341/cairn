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
//
// It also clears the ambient CAIRN_* configuration. Every agent in the fleet
// runs with CAIRN_IDENTITY (and usually CAIRN_STORE) exported, so inheriting
// them makes the suite's result depend on who runs it: identity-sensitive
// commands take their scoped branch and 18 tests fail, while CI — which never
// exports either — stays green. Tests that need a particular identity or store
// set it themselves with t.Setenv, which overrides this and restores after.
func TestMain(m *testing.M) {
	_ = os.Setenv("XDG_STATE_HOME", filepath.Join(os.TempDir(), "cairn-cmd-test-state"))
	_ = os.Unsetenv("CAIRN_IDENTITY")
	_ = os.Unsetenv("CAIRN_STORE")
	_ = os.Unsetenv("CAIRN_REVIEWER")
	_ = os.Unsetenv("CAIRN_RUN_ID")
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

// findExitRecord reads xdg's debug.jsonl and returns the single "exit"
// record it contains, mirroring findRetrievalOutcomeRecord/
// findPrimeEmitRecord's kind-filtering convention (commands_test.go,
// prime_test.go) rather than assuming a fixed line index.
func findExitRecord(t *testing.T, xdg string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)

	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["kind"] == "exit" {
			found = append(found, rec)
		}
	}
	require.Len(t, found, 1, "expected exactly one exit record in:\n%s", data)
	return found[0]
}

// TestExecuteAndExitLogsExitRecordOnSuccess covers crn-n5yaz's FR-8/FR-9
// acceptance criterion that every invocation logs exactly one "exit" record,
// using ExecuteC (not the plain Execute) so this observes the exit code
// derived from the resolved leaf command's own error return.
// executeAndExit is Execute's factored-out body, mirroring runDoctor's own
// split (doctor.go) so a test can drive it without an os.Exit call killing
// the test binary.
func TestExecuteAndExitLogsExitRecordOnSuccess(t *testing.T) {
	resetRootFlagsForTest(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	dir := t.TempDir()
	rootCmd.SetArgs([]string{"status", "--store", dir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	code := executeAndExit()
	assert.Equal(t, 0, code)

	rec := findExitRecord(t, xdg)
	assert.Equal(t, "cairn status", rec["command_path"])
	assert.Equal(t, float64(0), rec["exit_code"])
	assert.Equal(t, "", rec["error"])
}

// TestExecuteAndExitLogsExitRecordOnError covers the error-return path (a
// command that fails via cobra's normal RunE/PersistentPreRunE error return,
// never via a direct os.Exit call): executeAndExit must still return the
// process exit code the real Execute() would have used (1, matching
// Execute's pre-existing os.Exit(1) behavior on any error), and log it.
func TestExecuteAndExitLogsExitRecordOnError(t *testing.T) {
	resetRootFlagsForTest(t)
	orig := storeFlag
	storeFlag = ""
	defer func() { storeFlag = orig }()
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_STORE", "")
	t.Setenv("CAIRN_IDENTITY", "")

	rootCmd.SetArgs([]string{"status"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	code := executeAndExit()
	assert.Equal(t, 1, code)

	rec := findExitRecord(t, xdg)
	assert.Equal(t, float64(1), rec["exit_code"])
	assert.Contains(t, rec["error"], "no cairn store configured")
}

// TestExecuteAndExitCommandPathIncludesSubcommand confirms the logged
// command_path is the resolved leaf command's full path ("cairn doctor
// explain"), not just rootCmd's own name -- ExecuteC resolves the leaf
// command that actually ran even when it returns an error, which is the
// mechanism this whole design relies on (see executeAndExit's doc comment).
func TestExecuteAndExitCommandPathIncludesSubcommand(t *testing.T) {
	resetRootFlagsForTest(t)
	resetDoctorFlags(t)
	t.Cleanup(func() { resetDoctorFlags(t) })
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	dir := t.TempDir()
	rootCmd.SetArgs([]string{"doctor", "explain", "does-not-exist", "--store", dir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	code := executeAndExit()
	assert.Equal(t, 1, code)

	rec := findExitRecord(t, xdg)
	assert.Equal(t, "cairn doctor explain", rec["command_path"])
}

// TestExecuteAndExitRememberMarkerTextNeverLeaksIntoExitRecord covers
// crn-n5yaz's 10th acceptance criterion end-to-end: "cairn remember [body]"
// takes entry content as a bare positional argument (remember.go), so this
// is the concrete command the design doc's OQ3/Guardrail #2 redaction rule
// exists for. No store is configured, so PersistentPreRunE fails before the
// body is ever read -- exercised here through the real remember command,
// not just logCommandExit's own synthetic unit tests (exit_test.go).
func TestExecuteAndExitRememberMarkerTextNeverLeaksIntoExitRecord(t *testing.T) {
	resetRootFlagsForTest(t)
	orig := storeFlag
	storeFlag = ""
	defer func() { storeFlag = orig }()
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_STORE", "")
	t.Setenv("CAIRN_IDENTITY", "")

	rootCmd.SetArgs([]string{"remember", "MARKER-TEXT-f7a2c9"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})

	code := executeAndExit()
	assert.Equal(t, 1, code, "remember with no store configured must fail before ever touching the body")

	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "MARKER-TEXT-f7a2c9", "positional body text must never reach the debug log")

	rec := findExitRecord(t, xdg)
	assert.Equal(t, "cairn remember", rec["command_path"])
}
