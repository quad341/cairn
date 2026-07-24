package cairn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntryForEvictDoesNotStampRecallTelemetry(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	e, err := EntryForEvict(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, "a", e.ID)
	assert.Equal(t, 0, e.HitCount)
	assert.Empty(t, e.LastRecalledAt)

	raw, err := os.ReadFile(e.BodyPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "hit_count", "EntryForEvict must not stamp recall telemetry (hit_count/last_recalled_at) as a side effect -- it would corrupt the very disuse signal CullCandidates measures")
}

func TestEntryForEvictNotFound(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	_, err := EntryForEvict(ctx, store, "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

// TestEvictDirectDeletesOnlyTheEntryFile mirrors
// TestCommitDirectCommitsOnlyTheEntryFile (remember_test.go) one operation
// over: exactly one new commit lands on the store's current branch, it
// deletes only the entry file, no branch is created, and the reported SHA
// matches the store's new HEAD.
func TestEvictDirectDeletesOnlyTheEntryFile(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	gitCommitAll(t, store, "add entry")

	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	seedSHA, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	seedSHA = strings.TrimSpace(seedSHA)

	sha, err := e.EvictDirect(ctx, store)
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
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the eviction commit must contain only the entry file")

	_, statErr := os.Stat(e.BodyPath)
	assert.True(t, os.IsNotExist(statErr), "the entry file must be gone from the working tree")

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter), "EvictDirect must not switch branches")
	allBranches, err := gitRun(ctx, store, "branch", "--list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(allBranches), "\n"), 1, "EvictDirect must not create a new branch")

	status, err := gitRun(ctx, store, "status", "--porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "working tree must be clean after a successful eviction commit")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", "HEAD")
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID, "the commit message must name the evicted entry, for auditability (NFR-04)")
}

// TestEvictDirectFailureLeavesEntryOnDiskAndReportsError mirrors
// TestCommitDirectFailureLeavesEntryUncommittedAndReportsError: a git
// failure surfaces as a clear error and the entry file is left untouched.
func TestEvictDirectFailureLeavesEntryOnDiskAndReportsError(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir() // deliberately not a git repo

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	_, err = e.EvictDirect(ctx, store)
	require.Error(t, err, "a git failure must be surfaced, not swallowed")

	got, perr := ParseEntry(e.BodyPath)
	require.NoError(t, perr, "the entry file must survive a failed eviction attempt, not be rolled back or lost")
	assert.Equal(t, e.ID, got.ID)
}

// TestEvictDirectRefusesSharedTierEntry is the AC-required NFR-07 negative
// test: a direct-delete attempt on a shared-tier (non-agent:) entry must be
// refused, as a hard invariant enforced inside EvictDirect itself -- not a
// default a caller can opt out of. Deliberately uses a non-git store (unlike
// the positive-path tests above): the guard must fire before any git call at
// all, so a non-git store both proves that and keeps the test minimal.
func TestEvictDirectRefusesSharedTierEntry(t *testing.T) {
	ctx := t.Context()
	cases := []struct {
		name  string
		scope []string
	}{
		{"rig scope", []string{"rig:web"}},
		{"role scope", []string{"role:reviewer"}},
		{"global (empty) scope", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := t.TempDir()
			e, err := NewEntry("build-flags", tc.scope, "body", "agent:bot")
			require.NoError(t, err)
			require.NoError(t, e.Create(store))

			_, err = e.EvictDirect(ctx, store)
			require.Error(t, err, "a direct-delete attempt on a shared-tier entry must be refused (NFR-07)")
			assert.Contains(t, err.Error(), "not private")

			got, perr := ParseEntry(e.BodyPath)
			require.NoError(t, perr, "the entry file must remain present and unchanged when direct eviction is refused")
			assert.Equal(t, e.ID, got.ID)
		})
	}
}

// TestEvictToReviewBranchDeletesOnlyTheEntryFileLeavingDefaultUntouched
// mirrors TestCommitToReviewBranchCreatesIsolatedBranchLeavingDefaultUntouched
// one tier over: a shared-tier eviction proposal lands as a deletion commit
// on its own cull/<id> branch, never touching the store's own checked-out
// branch, HEAD, or working tree.
func TestEvictToReviewBranchDeletesOnlyTheEntryFileLeavingDefaultUntouched(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	gitCommitAll(t, store, "add entry")

	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	branch, err := e.EvictToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, "cull/"+e.ID, branch)

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter),
		"EvictToReviewBranch must not switch the store's checked-out branch")

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the default branch's tip commit must be unchanged")

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	changed, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-only", "-r", branch)
	require.NoError(t, err)
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the cull commit must touch only the entry file")

	nameStatus, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-status", "-r", branch)
	require.NoError(t, err)
	assert.Contains(t, nameStatus, "D\t"+rel, "the cull commit must be a deletion, not some other change")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(parent),
		"the cull branch must fork from the store's pre-existing HEAD, not carry unrelated history")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "rig:web")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"the scratch cull worktree must be cleaned up, leaving only the store's own")

	_, statErr := os.Stat(e.BodyPath)
	assert.NoError(t, statErr, "the entry file must still be present on the store's own working tree -- only the isolated cull branch deletes it")
}

// TestEvictToReviewBranchFailureLeavesEntryUntouchedAndReportsError mirrors
// TestCommitToReviewBranchFailureLeavesEntryWrittenButUncommittedAndReportsError:
// a branch already named exactly what EvictToReviewBranch is about to create
// makes `git worktree add -b` fail deterministically.
func TestEvictToReviewBranchFailureLeavesEntryUntouchedAndReportsError(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	gitCommitAll(t, store, "add entry")

	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)

	_, err = gitRun(ctx, store, "branch", "cull/"+e.ID)
	require.NoError(t, err)

	_, err = e.EvictToReviewBranch(ctx, store)
	require.Error(t, err, "a pre-existing branch name must surface as a clear error, not panic or silently continue")
	assert.Contains(t, err.Error(), "cull/"+e.ID)

	got, perr := ParseEntry(e.BodyPath)
	require.NoError(t, perr, "the entry must survive a failed eviction-proposal attempt, not be deleted or corrupted")
	assert.Equal(t, e.ID, got.ID)

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter),
		"a failed eviction-proposal attempt must not move the store's own HEAD")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"a failed worktree add must not leave a stray worktree registered")
}

// TestEvictToReviewBranchRefusesWhenProposalAlreadyPending: a second
// concurrent cull of an already-proposed entry is not an expected
// steady-state case (unlike CommitRecurrenceToReviewBranch's deliberate
// reuse of an existing remember/ branch) -- it must error, not silently
// fold into or fork past the existing proposal.
func TestEvictToReviewBranchRefusesWhenProposalAlreadyPending(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	gitCommitAll(t, store, "add entry")

	firstBranch, err := e.EvictToReviewBranch(ctx, store)
	require.NoError(t, err)

	_, err = e.EvictToReviewBranch(ctx, store)
	require.Error(t, err, "a second concurrent cull proposal for the same entry must be refused")
	assert.Contains(t, err.Error(), firstBranch)
}
