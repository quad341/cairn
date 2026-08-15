package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerifyWritesOnlyToAddressedStore is crn-2c8e. A copied store may carry
// an index whose body_path values still point at the origin. Verify must
// re-root that path under --store before writing the refreshed fingerprint.
func TestVerifyWritesOnlyToAddressedStore(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	gitCommitFile(t, repo, "source.txt", "source\n")

	origin := t.TempDir()
	originEntry := filepath.Join(origin, "global", "a.md")
	body := "+++\nid = \"a\"\ntitle = \"A\"\ncreated_at = \"2026-08-15\"\n\n[anchor]\n  type = \"files\"\n  repo = \"" + repo + "\"\n  paths = [\"source.txt\"]\n+++\nbody\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(originEntry), 0o750))
	require.NoError(t, os.WriteFile(originEntry, []byte(body), 0o600))
	_, err := cairn.Reindex(t.Context(), origin)
	require.NoError(t, err)

	copyDir := t.TempDir()
	copyEntry := filepath.Join(copyDir, "global", "a.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(copyEntry), 0o750))
	require.NoError(t, os.WriteFile(copyEntry, []byte(body), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(cairn.IndexPath(copyDir)), 0o750))
	index, err := os.ReadFile(cairn.IndexPath(origin)) //nolint:gosec // test-owned paths
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cairn.IndexPath(copyDir), index, 0o600)) //nolint:gosec // test-owned paths

	_, err = execRoot("verify", "--store", copyDir, "a")
	require.NoError(t, err)

	originAfter, err := os.ReadFile(originEntry)
	require.NoError(t, err)
	assert.Equal(t, body, string(originAfter), "verify --store <copy> must not modify the origin")
	copyAfter, err := os.ReadFile(copyEntry)
	require.NoError(t, err)
	assert.NotEqual(t, body, string(copyAfter), "verify must write the addressed copy")
}
