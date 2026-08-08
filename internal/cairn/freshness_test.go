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
		// Preventive, not a reported failure here: `git commit` ends in a
		// DETACHED `git maintenance run --auto`, which repacks after the
		// test that made the repo has returned and races t.TempDir()'s
		// RemoveAll ("unlinkat .../.git/objects: directory not empty").
		// internal/critic hit exactly that and it was refiled four times
		// before anyone traced it (crn-9k30); this helper builds fixture
		// repos the same way for ~20 tests, so it gets the same guard.
		{"config", "gc.auto", "0"},
		{"config", "maintenance.auto", "false"},
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
	fp, err := ComputeFingerprint(ctx, a)
	require.NoError(t, err)
	assert.Empty(t, fp, "a files anchor path untracked at HEAD must not produce a fingerprint")

	e := &Entry{ID: "x", Anchor: a}
	st, detail := Check(ctx, e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
}

// TestCheckNeverVerifiedIsFreeEvenWhenAnchorUnresolvable pins crn-0vqk.2's
// in-scope Check() reorder: a never-verified files anchor must report "never
// verified", not "not verifiable", even when the repo path can't actually be
// resolved -- proving the answer came from the free Fingerprint=="" check,
// not from a git shell-out that happened to come back empty. Before the
// reorder, ComputeFingerprint ran first, so an unresolvable repo produced
// "not verifiable" and silently threw away the (free, already-known)
// never-verified signal.
func TestCheckNeverVerifiedIsFreeEvenWhenAnchorUnresolvable(t *testing.T) {
	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: t.TempDir(), Paths: []string{"a.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equal(t, Unknown, st)
	assert.Contains(t, detail, "never verified")
}

// TestCheckFilesAnchorMissingRepoReportsNotVerifiableEvenWhenNeverVerified
// pins the reorder's ordering the other way: an anchor that isn't even
// shape-checkable (files with no repo configured) must still report "not
// verifiable", not "never verified", regardless of whether Fingerprint is
// also empty -- the shape check wins over the never-verified check.
func TestCheckFilesAnchorMissingRepoReportsNotVerifiableEvenWhenNeverVerified(t *testing.T) {
	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Paths: []string{"a.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equal(t, Unknown, st)
	assert.Contains(t, detail, "not verifiable")
}

func TestFileAnchorDrift(t *testing.T) {
	ctx := t.Context()
	repo := t.TempDir()
	gitInit(t, repo)
	src := filepath.Join(repo, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	a := Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}
	fp1, err := ComputeFingerprint(ctx, a)
	require.NoError(t, err)
	require.NotEmpty(t, fp1)

	a.Fingerprint = fp1
	e := &Entry{ID: "x", Anchor: a}
	st, detail := Check(ctx, e)
	require.Equalf(t, Fresh, st, "detail: %s", detail)

	require.NoError(t, os.WriteFile(src, []byte("package a\n\n// changed\n"), 0o600))
	gitCommitAll(t, repo, "change")

	st, _ = Check(ctx, e)
	assert.Equal(t, Stale, st)
	fp2, err := ComputeFingerprint(ctx, a)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2, "fingerprint should change after the source changed")
}

// TestCheckMissingRepositoryIsUnknown covers FR-5 class 1: an anchor
// repo path that doesn't exist on disk at all. git -C on a nonexistent
// directory still exits non-zero from git itself (a confirmed negative, not
// a Go-level invocation failure), so this must stay Unknown, not Incomplete.
func TestCheckMissingRepositoryIsUnknown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: missing, Paths: []string{"a.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
}

// TestCheckNoCommitsYetIsUnknown covers FR-5 class 2: a real repo with zero
// commits. `git rev-parse HEAD:path` fails with a confirmed non-zero exit
// (ambiguous/unknown revision), so this must stay Unknown.
func TestCheckNoCommitsYetIsUnknown(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo) // repo exists, but has zero commits

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
}

// TestCheckUnmatchedGlobIsUnknown covers FR-5 class 3: a glob pattern that
// matches nothing tracked. `git ls-files` still exits 0 with empty output
// (a confirmed "no matches"), so the literal pattern falls through to
// objectHash's own confirmed-negative path — Unknown, not Incomplete.
func TestCheckUnmatchedGlobIsUnknown(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"*.nonexistent"}}}
	st, detail := Check(t.Context(), e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
}

// TestCheckInvalidRevisionIsUnknown covers FR-5 class 4: a path staged
// (git add) but not yet committed, mirroring the crn-8x4 regression one
// layer up from sweep_test.go's Sweep-level coverage — Check alone must
// already report Unknown for this case.
func TestCheckInvalidRevisionIsUnknown(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	require.NoError(t, os.WriteFile(filepath.Join(repo, "staged.go"), []byte("package a\n"), 0o600))
	gitAdd(t, repo, "staged.go") // staged, deliberately not committed

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"staged.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equalf(t, Unknown, st, "detail: %s", detail)
}

// TestCheckGitInvocationFailureIsIncomplete covers FR-5 class 5 at the Check
// level: a genuine invocation failure (git unreachable via PATH) must
// surface as Incomplete, distinct from every confirmed-negative class above.
func TestCheckGitInvocationFailureIsIncomplete(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	t.Setenv("PATH", t.TempDir()) // git binary now unreachable

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}}
	st, detail := Check(t.Context(), e)
	assert.Equalf(t, Incomplete, st, "detail: %s", detail)
}

// TestCheckContextCanceledIsIncomplete covers FR-5 class 6 at the Check
// level: a canceled context is the operationally realistic failure mode
// (a request-scoped timeout), distinct from every confirmed-negative class
// above.
func TestCheckContextCanceledIsIncomplete(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	e := &Entry{ID: "x", Anchor: Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}}
	st, detail := Check(ctx, e)
	assert.Equalf(t, Incomplete, st, "detail: %s", detail)
}

// TestComputeFingerprintGitInvocationFailureReturnsError is the
// ComputeFingerprint-level counterpart to TestCheckGitInvocationFailureIsIncomplete
// (mirrors the existing git()-level test one layer up, per crn-fdjc.1's test
// plan).
func TestComputeFingerprintGitInvocationFailureReturnsError(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	t.Setenv("PATH", t.TempDir()) // git binary now unreachable

	a := Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}}
	fp, err := ComputeFingerprint(t.Context(), a)
	require.Error(t, err)
	assert.Empty(t, fp)
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

func TestCheckLogsFreshnessCheck(t *testing.T) {
	e := &Entry{ID: "x", Anchor: Anchor{Type: "none"}}

	var status, detail string
	recs := testLogRecords(t, func(ctx context.Context) {
		status, detail = Check(ctx, e)
	})

	require.Len(t, recs, 1, "Check must emit exactly one freshness_check record per call")
	rec := recs[0]
	assert.Equal(t, "freshness_check", rec["kind"])
	assert.Equal(t, "x", rec["entry_id"])
	assert.Equal(t, "none", rec["anchor_type"])
	assert.Equal(t, status, rec["status"])
	assert.Equal(t, detail, rec["detail"])
}
