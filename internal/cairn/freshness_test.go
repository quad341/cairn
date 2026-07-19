package cairn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "add", "-A").CombinedOutput()
	require.NoErrorf(t, err, "git add: %s", out)
	out, err = exec.CommandContext(t.Context(), "git", "-C", dir, "commit", "-q", "-m", msg).CombinedOutput()
	require.NoErrorf(t, err, "git commit: %s", out)
}

func TestCheckNoAnchor(t *testing.T) {
	st, _ := Check(t.Context(), &Entry{ID: "x", Anchor: Anchor{Type: "none"}})
	assert.Equal(t, Unknown, st)
}

func TestCheckNeverVerified(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}}
	st, _ := Check(t.Context(), e)
	assert.Equal(t, Unknown, st)
}

func TestFileAnchorDrift(t *testing.T) {
	ctx := t.Context()
	repo := t.TempDir()
	gitInit(t, repo)
	src := filepath.Join(repo, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	a := Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}
	fp1 := ComputeFingerprint(ctx, a)
	require.NotEmpty(t, fp1)

	a.Fingerprint = fp1
	e := &Entry{ID: "x", Anchor: a}
	st, detail := Check(ctx, e)
	require.Equalf(t, Fresh, st, "detail: %s", detail)

	require.NoError(t, os.WriteFile(src, []byte("package a\n\n// changed\n"), 0o600))
	gitCommitAll(t, repo, "change")

	st, _ = Check(ctx, e)
	assert.Equal(t, Stale, st)
	assert.NotEqual(t, fp1, ComputeFingerprint(ctx, a), "fingerprint should change after the source changed")
}
