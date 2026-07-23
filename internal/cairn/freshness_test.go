package cairn

import (
	"context"
	"errors"
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

// gitAdd stages a path without committing it — for fixtures that need a
// path tracked in the index but not resolvable at HEAD (crn-8x4).
func gitAdd(t *testing.T, dir, path string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "add", path).CombinedOutput()
	require.NoErrorf(t, err, "git add: %s", out)
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

// TestFileAnchorNonexistentPathFingerprintEmpty covers crn-6az.8.2: a
// files anchor pointing at a path that isn't tracked at the target repo's
// HEAD must never produce a fingerprint. Before the fix, expand()/
// objectHash() silently fell back to a deterministic-but-meaningless
// placeholder instead of propagating the failure, so verify would happily
// write back a bogus fingerprint and report Fresh forever after.
func TestFileAnchorNonexistentPathFingerprintEmpty(t *testing.T) {
	ctx := t.Context()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	a := Anchor{Type: "files", Repo: repo, Paths: []string{"does-not-exist.go"}}
	assert.Empty(t, ComputeFingerprint(ctx, a),
		"a files anchor path untracked at HEAD must not produce a fingerprint")

	e := &Entry{ID: "x", Anchor: a}
	st, detail := Check(ctx, e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
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

func TestGitConfirmedNonRepoIsNotAnError(t *testing.T) {
	dir := t.TempDir() // not a git repo at all

	_, ok, err := git(t.Context(), dir, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestGitNoCommitsIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir) // repo exists, but has zero commits

	_, ok, err := git(t.Context(), dir, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestGitInvocationFailurePropagatesAsError guards crn-t250 finding #2: a
// git invocation that fails for a reason other than a confirmed non-repo
// verdict (here, the git binary being unreachable via PATH — the bug
// report's own named example) must surface as an error, not be silently
// folded into the same "not found" result as a real non-repo/no-commits
// store.
func TestGitInvocationFailurePropagatesAsError(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, dir, "init")

	t.Setenv("PATH", t.TempDir()) // git binary now unreachable

	_, ok, err := git(t.Context(), dir, "rev-parse", "HEAD")
	require.Error(t, err)
	assert.False(t, ok)
	var exitErr *exec.ExitError
	assert.False(t, errors.As(err, &exitErr), "a PATH lookup failure must not be misclassified as a confirmed git verdict")
}

func TestGitContextCanceledPropagatesAsError(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, dir, "init")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, ok, err := git(ctx, dir, "rev-parse", "HEAD")
	require.Error(t, err)
	assert.False(t, ok)
	assert.ErrorIs(t, err, context.Canceled)
}
