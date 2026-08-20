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

// TestRootSilencesUsageOnError reproduces crn-rott9's reported symptom: a
// RunE/PersistentPreRunE error (here, the no-store-configured error above)
// must surface as just the error text, not buried under Cobra's default
// full usage/help dump -- which pushes the real message off the top of a
// `| tail -8`'d terminal.
func TestRootSilencesUsageOnError(t *testing.T) {
	resetRootFlagsForTest(t)
	orig := storeFlag
	storeFlag = ""
	defer func() { storeFlag = orig }()
	t.Setenv("CAIRN_STORE", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	rootCmd.SetArgs([]string{"status"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, buf.String(), "Usage:")
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

	// Args must reflect the real process argv (os.Args), not the shorter
	// list rootCmd.SetArgs configured above for cobra's own parsing --
	// proving PersistentPreRunE reads os.Args directly rather than deriving
	// it from the command's parsed flags/args.
	rawArgs, ok := rec["args"].([]any)
	require.True(t, ok, "context record must carry an args array")
	gotArgs := make([]string, len(rawArgs))
	for i, a := range rawArgs {
		s, ok := a.(string)
		require.True(t, ok, "args[%d] must be a string", i)
		gotArgs[i] = s
	}
	assert.Equal(t, os.Args, gotArgs)
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

func TestRedactArgvRedactsRememberPositional(t *testing.T) {
	cmd := &cobra.Command{Use: "remember [body]"}
	require.NoError(t, cmd.Flags().Parse([]string{"super secret body text"}))

	argv := []string{"cairn", "remember", "super secret body text"}
	got := redactArgv(argv, cmd)
	assert.Equal(t, []string{"cairn", "remember", "«redacted»"}, got)
}

func TestRedactArgvLeavesNonFreeformPositionalAlone(t *testing.T) {
	cmd := &cobra.Command{Use: "status"}
	require.NoError(t, cmd.Flags().Parse([]string{"some-positional"}))

	argv := []string{"cairn", "status", "some-positional"}
	got := redactArgv(argv, cmd)
	assert.Equal(t, argv, got)
}

func TestRedactArgvRedactsSuspiciousFlagValue(t *testing.T) {
	tests := []struct {
		name          string
		flag          string
		shorthand     string
		boolFlag      string
		boolShorthand string
		args          []string
		argv          []string
		want          []string
	}{
		{
			name: "equals-joined token flag",
			flag: "token",
			args: []string{"--token=hunter2"},
			argv: []string{"cairn", "somecmd", "--token=hunter2"},
			want: []string{"cairn", "somecmd", "--token=«redacted»"},
		},
		{
			name: "space-separated secret flag",
			flag: "secret",
			args: []string{"--secret", "hunter2"},
			argv: []string{"cairn", "somecmd", "--secret", "hunter2"},
			want: []string{"cairn", "somecmd", "--secret", "«redacted»"},
		},
		{
			name: "case-insensitive credential match",
			flag: "API-Credential",
			args: []string{"--API-Credential=hunter2"},
			argv: []string{"cairn", "somecmd", "--API-Credential=hunter2"},
			want: []string{"cairn", "somecmd", "--API-Credential=«redacted»"},
		},
		{
			// POSIX/GNU shorthand-combined form: letter and value concatenated
			// with no separator (e.g. curl -ofile.txt). Contains no "=" and the
			// token itself isn't a bare redact-set value, so it must be caught
			// by a dedicated shorthand branch rather than the equals-joined or
			// space-separated ones.
			name:      "shorthand-combined token flag",
			flag:      "token",
			shorthand: "t",
			args:      []string{"-thunter2"},
			argv:      []string{"cairn", "somecmd", "-thunter2"},
			want:      []string{"cairn", "somecmd", "-t«redacted»"},
		},
		{
			// A boolean shorthand bundled ahead of a value-taking suspicious
			// shorthand in the same token (e.g. -v then -t, POSIX/GNU-style:
			// -vtSECRET means -v -tSECRET). The naive fixed-offset a[2:] slice
			// assumes the suspicious shorthand is always the character right
			// after '-', so it looks up a[2:] ("tSECRETVALUE") instead of the
			// true value ("SECRETVALUE") -- that's not in the redact set, so
			// the whole token falls through unredacted. Only a walk that skips
			// no-value (NoOptDefVal-bearing) shorthands before checking for a
			// value-taking one catches this.
			name:          "bundled boolean plus suspicious shorthand",
			flag:          "token",
			shorthand:     "t",
			boolFlag:      "verbose",
			boolShorthand: "v",
			args:          []string{"-vtSECRETVALUE"},
			argv:          []string{"cairn", "somecmd", "-vtSECRETVALUE"},
			want:          []string{"cairn", "somecmd", "-vt«redacted»"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "somecmd"}
			if tt.boolFlag != "" {
				cmd.Flags().BoolP(tt.boolFlag, tt.boolShorthand, false, "")
			}
			if tt.shorthand != "" {
				cmd.Flags().StringP(tt.flag, tt.shorthand, "", "")
			} else {
				cmd.Flags().String(tt.flag, "", "")
			}
			require.NoError(t, cmd.Flags().Parse(tt.args))

			got := redactArgv(tt.argv, cmd)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedactArgvNoopWhenNeitherRuleMatches(t *testing.T) {
	cmd := &cobra.Command{Use: "status"}
	cmd.Flags().String("store", "", "")
	require.NoError(t, cmd.Flags().Parse([]string{"--store", "/some/dir"}))

	argv := []string{"cairn", "status", "--store", "/some/dir"}
	got := redactArgv(argv, cmd)
	assert.Equal(t, argv, got)
}

// TestRootRedactsRememberBodyFromLoggedArgs proves the PersistentPreRunE call
// site actually invokes redactArgv on the real process argv -- the unit
// tests above only prove redactArgv's own logic in isolation. os.Args is
// mutated directly (and restored via cleanup) because that is what the call
// site reads, mirroring TestRootLogsContextRecordUnconditionally's own proof
// that PersistentPreRunE reads os.Args rather than SetArgs's shorter list.
func TestRootRedactsRememberBodyFromLoggedArgs(t *testing.T) {
	resetRootFlagsForTest(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	dir := t.TempDir()
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	secretBody := "super secret remembered body text"
	os.Args = []string{"cairn", "remember", secretBody, "--store", dir, "--scope", ""}

	rootCmd.SetArgs([]string{"remember", secretBody, "--store", dir, "--scope", ""})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	_ = rootCmd.Execute() // remember may fail downstream; only the logged context record is under test here

	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines)

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))

	rawArgs, ok := rec["args"].([]any)
	require.True(t, ok, "context record must carry an args array")
	gotArgs := make([]string, len(rawArgs))
	for i, a := range rawArgs {
		s, ok := a.(string)
		require.True(t, ok, "args[%d] must be a string", i)
		gotArgs[i] = s
	}

	assert.NotContains(t, gotArgs, secretBody)
	assert.Contains(t, gotArgs, "«redacted»")
}
