package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRemember executes "cairn remember" (plus extraArgs) against the shared
// rootCmd. rootCmd/rememberCmd are package-level singletons, so pflag state
// otherwise leaks across tests in this binary: resetRememberFlags clears
// --topic/--scope (this file's own flags) and the inherited --identity flag
// before and after every call. --identity is a StringSlice; commands_test.go's
// runStatus only resets its Changed bit, not its underlying value, so a prior
// test's "--identity rig:alpha" would otherwise leak into every test here that
// relies on identity defaulting. Replace (not Set) is used to clear it because
// stringSliceValue.Set treats a repeat call as an append, not a replace.
// Returns the temp store dir passed via --store, so callers can assert zero
// filesystem writes.
func runRemember(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	resetRememberFlags(t)
	t.Cleanup(func() { resetRememberFlags(t) })

	store := t.TempDir()
	args := append([]string{"remember", "--store", store}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return store, rootCmd.Execute()
}

func resetRememberFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"topic", "scope"} {
		f := rememberCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(""))
		f.Changed = false
	}

	idf := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, idf)
	sv, ok := idf.Value.(pflag.SliceValue)
	require.True(t, ok, "identity flag must implement pflag.SliceValue")
	require.NoError(t, sv.Replace(nil))
	idf.Changed = false
}

func assertNoFilesWritten(t *testing.T, store string) {
	t.Helper()
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "a rejected remember call must not write anything under the store")
}

func TestRememberRejectsAttackTopics(t *testing.T) {
	attacks := map[string]string{
		"path traversal": "../../etc/passwd",
		"absolute path":  "/etc/passwd",
		"leading dot":    ".hidden",
		"embedded NUL":   "foo\x00bar",
		"empty":          "",
	}
	for name, topic := range attacks {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", topic, "a body")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--topic")
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberRejectsAttackScopes(t *testing.T) {
	attacks := map[string]string{
		"path traversal": "../../etc/passwd",
		"absolute path":  "/etc/passwd",
		"leading dot":    ".hidden",
		"embedded NUL":   "foo\x00bar",
	}
	for name, tag := range attacks {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", "valid-topic", "--scope", tag, "a body")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "scope tag")
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberRejectsMissingTopic(t *testing.T) {
	store, err := runRemember(t, "a body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--topic")
	assertNoFilesWritten(t, store)
}

func TestRememberRequiresExactlyOneBodyArg(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic")
	require.Error(t, err, "a missing body argument must be rejected")
	assertNoFilesWritten(t, store)
}

func TestRememberValidInputReachesNotImplemented(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "a body")
	require.ErrorIs(t, err, errRememberNotImplemented)
	assertNoFilesWritten(t, store)
}

func TestRememberDefaultScopeUsesResolvedIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha agent:bot")
	store, err := runRemember(t, "--topic", "valid-topic", "a body")
	require.ErrorIs(t, err, errRememberNotImplemented, "a valid identity-derived scope must pass validation")
	assertNoFilesWritten(t, store)
}

func TestRememberDefaultScopeValidatesResolvedIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "agent:../evil")
	store, err := runRemember(t, "--topic", "valid-topic", "a body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope tag", "an unsafe identity-derived scope tag must be rejected, not silently used")
	assertNoFilesWritten(t, store)
}

func TestRememberDefaultScopeRequiresAgentTag(t *testing.T) {
	cases := map[string]string{
		"no identity at all":             "",
		"identity without an agent: tag": "rig:alpha role:reviewer",
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CAIRN_IDENTITY", identity)
			store, err := runRemember(t, "--topic", "valid-topic", "a body")
			require.Error(t, err)
			assert.NotErrorIs(t, err, errRememberNotImplemented,
				"an identity that can't resolve to a single private tag must not silently proceed")
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberExplicitScopeOverridesIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha")
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "role:reviewer,agent:bot", "a body")
	require.ErrorIs(t, err, errRememberNotImplemented)
	assertNoFilesWritten(t, store)
}

func TestRememberRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"remember"})
	require.NoError(t, err)
	assert.Same(t, rememberCmd, found)
}

// TestDefaultScopeCollapsesToSingleAgentTag proves the actual defect: a
// multi-tag identity spanning rig/role/agent must collapse to exactly the
// agent:<id> tag, not pass through as the full tag set (which doesn't map to
// any single DESIGN.md §2 scope directory).
func TestDefaultScopeCollapsesToSingleAgentTag(t *testing.T) {
	scope, err := defaultScope([]string{"rig:alpha", "role:reviewer", "agent:bot"})
	require.NoError(t, err)
	assert.Equal(t, []string{"agent:bot"}, scope)
}

func TestDefaultScopeErrorsWithoutAgentTag(t *testing.T) {
	cases := map[string][]string{
		"no agent: tag present": {"rig:alpha", "role:reviewer"},
		"empty identity":        nil,
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			scope, err := defaultScope(identity)
			require.Error(t, err)
			assert.Nil(t, scope)
		})
	}
}
