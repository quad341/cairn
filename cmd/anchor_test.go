package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAnchorFlags clears anchorCmd's flag state between runs. rootCmd and
// anchorCmd are package-level singletons, so without this a prior test's
// --repo survives into a test asserting it is missing, and --path (a
// StringArray, whose Set APPENDS) accumulates every path any earlier test
// passed. Mirrors resetRememberFlags, which exists for the same reason.
func resetAnchorFlags(t *testing.T) {
	t.Helper()
	rf := anchorCmd.Flags().Lookup("repo")
	require.NotNil(t, rf)
	require.NoError(t, rf.Value.Set(""))
	rf.Changed = false

	vf := anchorCmd.Flags().Lookup("verify")
	require.NotNil(t, vf)
	require.NoError(t, vf.Value.Set("false"))
	vf.Changed = false

	pf := anchorCmd.Flags().Lookup("path")
	require.NotNil(t, pf)
	psv, ok := pf.Value.(pflag.SliceValue)
	require.True(t, ok, "path flag must implement pflag.SliceValue")
	require.NoError(t, psv.Replace(nil))
	pf.Changed = false
}

// runAnchor runs "cairn anchor" against a store seeded with one unanchored
// entry, returning the store and the entry's path so callers can assert on
// what landed on disk.
func runAnchor(t *testing.T, extraArgs ...string) (entryPath string, err error) {
	t.Helper()
	resetAnchorFlags(t)
	t.Cleanup(func() { resetAnchorFlags(t) })

	store := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(store, "global"), 0o750))
	entryPath = filepath.Join(store, "global", "a.md")
	require.NoError(t, os.WriteFile(entryPath,
		[]byte("+++\nid = \"a\"\ntitle = \"A\"\n\n[anchor]\n  type = \"none\"\n+++\nbody\n"), 0o600))

	rootCmd.SetArgs(append([]string{"anchor", "--store", store}, extraArgs...))
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return entryPath, rootCmd.Execute()
}

// TestAnchorAddsFilesAnchorToExistingEntry is crn-01fj's headline: before
// this verb there was no supported way to anchor an entry that already
// existed. remember only builds anchors at creation; verify only recomputes
// a fingerprint for an entry that already has repo+paths.
func TestAnchorAddsFilesAnchorToExistingEntry(t *testing.T) {
	repo := t.TempDir()
	entryPath, err := runAnchor(t, "--repo", repo, "--path", "x.go", "a")
	require.NoError(t, err)

	raw, err := os.ReadFile(entryPath)
	require.NoError(t, err)
	out := string(raw)
	assert.Contains(t, out, `type = "files"`)
	assert.Contains(t, out, `repo = "`+repo+`"`)
	assert.Contains(t, out, `paths = ["x.go"]`)
	assert.NotContains(t, out, `type = "none"`)
}

// TestAnchorAcceptsRepeatedPaths — an entry often describes a behaviour
// spread across several files, and freshness should go unknown if any of
// them moves.
func TestAnchorAcceptsRepeatedPaths(t *testing.T) {
	repo := t.TempDir()
	entryPath, err := runAnchor(t, "--repo", repo, "--path", "x.go", "--path", "y/z.go", "a")
	require.NoError(t, err)

	raw, err := os.ReadFile(entryPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `paths = ["x.go", "y/z.go"]`)
}

// TestAnchorRequiresRepoAndPath — an anchor missing either half cannot
// resolve, and silently writing one would manufacture exactly the false
// "anchored" signal this work exists to avoid.
func TestAnchorRequiresRepoAndPath(t *testing.T) {
	t.Run("no repo", func(t *testing.T) {
		_, err := runAnchor(t, "--path", "x.go", "a")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--anchor-repo")
	})
	t.Run("no path", func(t *testing.T) {
		_, err := runAnchor(t, "--repo", t.TempDir(), "a")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--anchor-path")
	})
}

// TestAnchorUnknownIDErrors — anchoring a nonexistent id must fail loudly
// rather than silently doing nothing.
func TestAnchorUnknownIDErrors(t *testing.T) {
	_, err := runAnchor(t, "--repo", t.TempDir(), "--path", "x.go", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

// TestAnchorRefusesToWriteOutsideTheAddressedStore is crn-2c8e. Reads resolve
// through the index, which records body_paths pointing at wherever the index
// was built — so a copied, restored, or moved store carries paths addressing
// its ORIGIN. Writing through them means `--store <copy>` silently modifies
// the original, which is the exact opposite of why anyone passes --store.
// Caught for real: anchoring an entry in a `cp -r` of the mayor's store left
// the copy untouched and modified the live store.
func TestAnchorRefusesToWriteOutsideTheAddressedStore(t *testing.T) {
	resetAnchorFlags(t)
	t.Cleanup(func() { resetAnchorFlags(t) })

	origin := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(origin, "global"), 0o750))
	originEntry := filepath.Join(origin, "global", "a.md")
	body := "+++\nid = \"a\"\ntitle = \"A\"\n\n[anchor]\n  type = \"none\"\n+++\nbody\n"
	require.NoError(t, os.WriteFile(originEntry, []byte(body), 0o600))

	// Build the index while addressing origin, so body_paths point there.
	_, err := cairn.Reindex(t.Context(), origin)
	require.NoError(t, err)

	// Copy the whole store, index included — the real-world shape.
	copyDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(copyDir, "global"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(copyDir, "global", "a.md"), []byte(body), 0o600))
	copyIndexDir := filepath.Join(copyDir, filepath.Base(filepath.Dir(cairn.IndexPath(origin))))
	require.NoError(t, os.MkdirAll(copyIndexDir, 0o750))
	// Both paths are t.TempDir()-rooted, built by this test.
	// Both stores are t.TempDir()-rooted and built by this test; the only
	// "taint" is that their paths are computed rather than literal.
	idx, err := os.ReadFile(cairn.IndexPath(origin)) //nolint:gosec // test-owned t.TempDir() path
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cairn.IndexPath(copyDir), idx, 0o600)) //nolint:gosec // test-owned t.TempDir() path

	rootCmd.SetArgs([]string{"anchor", "--store", copyDir, "--repo", t.TempDir(), "--path", "x.go", "a"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	_ = rootCmd.Execute()

	after, err := os.ReadFile(originEntry)
	require.NoError(t, err)
	assert.Equal(t, body, string(after),
		"addressing the copy must never modify the origin store")
}
