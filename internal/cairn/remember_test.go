package cairn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEntryIDIncludesTopicKeyButIsUnique(t *testing.T) {
	a, err := NewEntry("shared-topic", []string{"agent:bot"}, "a body", "agent:bot")
	require.NoError(t, err)
	b, err := NewEntry("shared-topic", []string{"agent:bot"}, "a body", "agent:bot")
	require.NoError(t, err)

	assert.Equal(t, "shared-topic", a.TopicKey)
	assert.True(t, strings.HasPrefix(a.ID, "shared-topic-"))
	assert.NotEqual(t, a.ID, b.ID,
		"several entries may deliberately share one topic_key -- shadow() picks the winner at read time -- so id must never be derived from topic_key alone")
}

func TestNewEntryTitleAndSummary(t *testing.T) {
	cases := map[string]struct {
		body            string
		wantTitle       string
		wantSummaryFunc func(t *testing.T, summary string)
	}{
		"one-liner": {
			body:      "fixed the flaky test by seeding the RNG",
			wantTitle: "fixed the flaky test by seeding the RNG",
		},
		"multi-line": {
			body:      "short heading\n\nlonger explanation across\nmultiple lines",
			wantTitle: "short heading",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e, err := NewEntry("t", nil, tc.body, "")
			require.NoError(t, err)
			assert.Equal(t, tc.wantTitle, e.Title)
			assert.Equal(t, strings.TrimSpace(tc.body), e.Summary)
		})
	}
}

func TestNewEntryAnchorIsNone(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	assert.Equal(t, "none", e.Anchor.Type)
}

func TestNewEntryStampsCreatedAtAsDateOnly(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	_, err = time.Parse(time.DateOnly, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an ISO-8601 date so lexical and chronological order agree, see moreSpecific")
}

func TestScopeDirPicksTierByPriorityWhenScopeSpansMultiple(t *testing.T) {
	cases := []struct {
		name  string
		scope []string
		want  string
	}{
		{"empty scope is global", nil, filepath.Join("store", "global")},
		{"single rig tag", []string{"rig:web"}, filepath.Join("store", "rig", "web")},
		{"single role tag", []string{"role:reviewer"}, filepath.Join("store", "role", "reviewer")},
		{"single agent tag", []string{"agent:bot"}, filepath.Join("store", "agent", "bot")},
		{"rig beats role+agent", []string{"agent:bot", "role:reviewer", "rig:web"}, filepath.Join("store", "rig", "web")},
		{"role beats agent", []string{"agent:bot", "role:reviewer"}, filepath.Join("store", "role", "reviewer")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, scopeDir("store", tc.scope))
		})
	}
}

func TestEntryCreateRoundTrip(t *testing.T) {
	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))
	assert.Equal(t, filepath.Join(store, "rig", "web", e.ID+".md"), e.BodyPath)

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Title, got.Title)
	assert.Equal(t, e.Summary, got.Summary)
	assert.Equal(t, e.TopicKey, got.TopicKey)
	assert.Equal(t, e.Scope, got.Scope)
	assert.Equal(t, e.Anchor, got.Anchor)
	assert.Equal(t, e.CreatedBy, got.CreatedBy)
	assert.Equal(t, e.CreatedAt, got.CreatedAt)
	assert.Equal(t, e.Body, got.Body)
}

func TestEntryCreateGlobalTier(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))
	assert.Equal(t, filepath.Join(store, "global", e.ID+".md"), e.BodyPath)

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Empty(t, got.Scope)
}

func TestEntryCreateMakesParentDirs(t *testing.T) {
	e, err := NewEntry("t", []string{"agent:brand-new"}, "body", "")
	require.NoError(t, err)

	store := t.TempDir() // store/agent/brand-new does not exist yet
	require.NoError(t, e.Create(store))

	_, err = ParseEntry(e.BodyPath)
	require.NoError(t, err)
}

func TestEntryCreateRetriesOnIDCollision(t *testing.T) {
	e, err := NewEntry("shared-topic", []string{"agent:bot"}, "body", "agent:bot")
	require.NoError(t, err)
	firstID := e.ID

	store := t.TempDir()
	dir := scopeDir(store, e.Scope)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	collisionPath := filepath.Join(dir, firstID+".md")
	require.NoError(t, os.WriteFile(collisionPath, []byte("sentinel: pre-existing entry, must not be overwritten"), 0o600))

	require.NoError(t, e.Create(store))

	assert.NotEqual(t, firstID, e.ID, "Create must regenerate the ID on collision rather than overwrite the existing file")
	assert.NotEqual(t, collisionPath, e.BodyPath)

	untouched, err := os.ReadFile(collisionPath)
	require.NoError(t, err)
	assert.Equal(t, "sentinel: pre-existing entry, must not be overwritten", string(untouched))

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
}

func TestIsPrivateScope(t *testing.T) {
	cases := []struct {
		name  string
		scope []string
		want  bool
	}{
		{"empty scope is not private", nil, false},
		{"rig tag is not private", []string{"rig:web"}, false},
		{"role tag is not private", []string{"role:reviewer"}, false},
		{"agent tag is private", []string{"agent:bot"}, true},
		{"rig beats agent -- not private", []string{"agent:bot", "rig:web"}, false},
		{"role beats agent -- not private", []string{"agent:bot", "role:reviewer"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPrivateScope(tc.scope))
		})
	}
}

// TestCommitDirectCommitsOnlyTheEntryFile covers AC1-3 of crn-419.3: exactly
// one new commit lands on the store's current branch, it contains only the
// new entry file, no branch is created, and the reported SHA matches the
// store's new HEAD.
func TestCommitDirectCommitsOnlyTheEntryFile(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	seedSHA, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	seedSHA = strings.TrimSpace(seedSHA)
	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	sha, err := e.CommitDirect(ctx, store)
	require.NoError(t, err)
	assert.NotEmpty(t, sha)

	head, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(head), sha, "the returned SHA must be the store's new HEAD")

	parent, err := gitRun(ctx, store, "rev-parse", "HEAD~1")
	require.NoError(t, err)
	assert.Equal(t, seedSHA, strings.TrimSpace(parent), "exactly one new commit must land on top of the prior HEAD")

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	changed, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the commit must contain only the new entry file")

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter), "CommitDirect must not switch branches")
	allBranches, err := gitRun(ctx, store, "branch", "--list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(allBranches), "\n"), 1, "CommitDirect must not create a new branch")

	status, err := gitRun(ctx, store, "status", "--porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "working tree must be clean after a successful commit")
}

// TestCommitDirectFailureLeavesEntryUncommittedAndReportsError covers AC4:
// a git failure surfaces as a clear error (naming the git step that failed),
// and the already-written entry file is left on disk exactly as Create wrote
// it -- reported as uncommitted, not silently rolled back or lost.
func TestCommitDirectFailureLeavesEntryUncommittedAndReportsError(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir() // deliberately not a git repo

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	_, err = e.CommitDirect(ctx, store)
	require.Error(t, err, "a git failure must be surfaced, not swallowed")
	assert.Contains(t, err.Error(), "git add", "the error should make clear which git step failed")

	got, perr := ParseEntry(e.BodyPath)
	require.NoError(t, perr, "the written entry file must survive a commit failure, not be rolled back or silently lost")
	assert.Equal(t, e.ID, got.ID)
}

// TestCommitToReviewBranchCreatesIsolatedBranchLeavingDefaultUntouched covers
// crn-419.5 AC4 (the shared-tier branch-plus-mail path's git half) at the
// internal/cairn level: CommitToReviewBranch had no dedicated unit test
// before this, only indirect, incomplete exercise via cmd's CLI-level tests,
// none of which capture the store's original branch/HEAD before the call to
// actually prove it is left untouched -- they only observe the entry file's
// own status afterward. Mirrors TestCommitDirectCommitsOnlyTheEntryFile
// above, one tier over.
func TestCommitToReviewBranchCreatesIsolatedBranchLeavingDefaultUntouched(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	branch, err := e.CommitToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, "remember/"+e.ID, branch)

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter),
		"CommitToReviewBranch must not switch the store's checked-out branch")

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the default branch's tip commit must be unchanged")

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	changed, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-only", "-r", branch)
	require.NoError(t, err)
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the review commit must contain only the new entry file")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(parent),
		"the review branch must fork from the store's pre-existing HEAD, not carry unrelated history")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "rig:web")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"the scratch review worktree must be cleaned up, leaving only the store's own")

	// --untracked-files=all: rig/ is an entirely-new, entirely-untracked
	// directory, and plain --porcelain collapses that to a single "?? rig/"
	// line rather than naming the file inside it.
	status, err := gitRun(ctx, store, "status", "--porcelain", "--untracked-files=all")
	require.NoError(t, err)
	assert.Contains(t, status, "?? "+rel,
		"the entry file Create wrote must remain untracked on the store's own branch -- only the review branch's isolated worktree copy was committed")
}

// TestCommitToReviewBranchFailureLeavesEntryWrittenButUncommittedAndReportsError
// covers crn-419.5 AC5's git-error failure-injection requirement for the
// shared-tier path -- the private-tier equivalent is
// TestCommitDirectFailureLeavesEntryUncommittedAndReportsError above. A
// branch already named exactly what CommitToReviewBranch is about to create
// makes `git worktree add -b` fail deterministically, with no race or mock
// needed: the entry Create already wrote must survive untouched, and the
// store's own HEAD and worktree list must be unaffected by the failed
// attempt.
func TestCommitToReviewBranchFailureLeavesEntryWrittenButUncommittedAndReportsError(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)

	e, err := NewEntry("build-flags", []string{"rig:web"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	// Pre-create the exact branch name CommitToReviewBranch will try to
	// create, so `git worktree add -b` fails deterministically on a branch
	// that already exists.
	_, err = gitRun(ctx, store, "branch", "remember/"+e.ID)
	require.NoError(t, err)

	_, err = e.CommitToReviewBranch(ctx, store)
	require.Error(t, err, "a pre-existing branch name must surface as a clear error, not panic or silently continue")
	assert.Contains(t, err.Error(), "remember/"+e.ID)
	assert.Contains(t, err.Error(), "review branch")

	got, perr := ParseEntry(e.BodyPath)
	require.NoError(t, perr, "the entry Create already wrote must survive a review-branch failure, not be rolled back or corrupted")
	assert.Equal(t, e.ID, got.ID)

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter),
		"a failed review-branch attempt must not move the store's own HEAD")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"a failed worktree add must not leave a stray worktree registered")
}
