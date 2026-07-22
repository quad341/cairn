package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runStatus executes "cairn status" (plus any extra args) against the shared
// rootCmd. It resets the --identity flag's Changed bit first: rootCmd is a
// package-level singleton, so pflag's Changed state otherwise leaks across
// tests that run in the same binary.
func runStatus(t *testing.T, dir string, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"status", "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. statusCmd's RunE prints via fmt.Println/
// fmt.Printf directly rather than cmd.OutOrStdout(), so rootCmd.SetOut has no
// effect on that output — this is the only way to observe it from a test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// seedEntry writes a fixture entry (TOML-frontmatter markdown) under one of
// the store's scope dirs, mirroring internal/cairn's own test fixtures.
func seedEntry(t *testing.T, storeDir, relPath, content string) {
	t.Helper()
	p := filepath.Join(storeDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

// lineFor returns the status line for the given entry id, matched as the
// second whitespace-delimited field (after the freshness flag) so a shared
// prefix between two ids can't produce a false match.
func lineFor(t *testing.T, output, id string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == id {
			return line
		}
	}
	t.Fatalf("no status line for entry %q in output:\n%s", id, output)
	return ""
}

func TestStatusRejectsExplicitIdentityFlag(t *testing.T) {
	err := runStatus(t, t.TempDir(), "--identity", "rig:alpha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusRejectsIdentityEnv(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha")
	err := runStatus(t, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusBareIsUnchanged(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "")
	err := runStatus(t, t.TempDir())
	require.NoError(t, err)
}

func TestStatusRejectsStrayPositionalArgs(t *testing.T) {
	err := runStatus(t, t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

const (
	shadowedByScoped = "+++\nid = \"less-specific\"\ntitle = \"Less specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	shadowsScoped    = "+++\nid = \"more-specific\"\ntitle = \"More specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nbody\n"

	unrelatedTopicA = "+++\nid = \"unrelated-a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n"
	unrelatedTopicB = "+++\nid = \"unrelated-b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nbody\n"
)

func TestStatusAnnotatesShadowedEntries(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/less-specific.md", shadowedByScoped)
	seedEntry(t, dir, "role/investigator/more-specific.md", shadowsScoped)

	var err error
	out := captureStdout(t, func() { err = runStatus(t, dir) })
	require.NoError(t, err)

	assert.Contains(t, lineFor(t, out, "less-specific"), "[SHADOWED BY more-specific]",
		"the less specific entry's line must be annotated with its shadower")
	assert.NotContains(t, lineFor(t, out, "more-specific"), "SHADOWED BY",
		"the winning (more specific) entry's line must not be annotated")
}

func TestStatusNoAnnotationWithoutShadow(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/unrelated-a.md", unrelatedTopicA)
	seedEntry(t, dir, "global/unrelated-b.md", unrelatedTopicB)

	var err error
	out := captureStdout(t, func() { err = runStatus(t, dir) })
	require.NoError(t, err)

	assert.NotContains(t, out, "SHADOWED BY",
		"entries with no shadow relationship must never be annotated")
}

func TestReindexRejectsStrayPositionalArgs(t *testing.T) {
	_, err := execRoot("reindex", "--store", t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestMapRejectsStrayPositionalArgs(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	// Mirrors crn-6az.3's own repro: a second --identity tag typed as a
	// space-separated arg (instead of comma-joined into one flag value) used
	// to be silently swallowed as a stray positional, narrowing the resolved
	// identity to just the first tag with no indication anything was dropped.
	_, err := execRoot("map", "--store", t.TempDir(), "--identity", "role:architect", "role:pm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role:pm")
}
