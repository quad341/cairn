package cairn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchStore returns a fresh git repo with a resolvable HEAD (an empty
// initial commit) -- CommitToReviewBranch's `git worktree add -b branch wt
// HEAD` needs HEAD to already resolve before any entry is ever written.
// gitInit (freshness_test.go) alone doesn't commit, unlike
// cmd/remember_test.go's own local gitInit.
func branchStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	gitInit(t, store)
	out, err := exec.CommandContext(t.Context(), "git", "-C", store, "commit", "-q", "--allow-empty", "-m", "init").CombinedOutput()
	require.NoErrorf(t, err, "git commit --allow-empty: %s", out)
	return store
}

// commitReviewBranchAt creates a shared-tier entry and commits it to its own
// review branch with a fixed author/committer date, so ListReviewBranches'
// age computation is deterministic instead of racing the wall clock.
func commitReviewBranchAt(t *testing.T, store, topicKey string, scope []string, at time.Time) *Entry {
	t.Helper()
	e, err := NewEntry(topicKey, scope, "body text", "tester")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	iso := at.Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", iso)
	t.Setenv("GIT_COMMITTER_DATE", iso)
	_, err = e.CommitToReviewBranch(t.Context(), store)
	require.NoError(t, err)
	return e
}

func TestTierFromPath(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		wantTier  string
		wantValue string
	}{
		{"global", "global/foo-1a2b.md", "global", ""},
		{"rig", "rig/web/foo-1a2b.md", "rig", "web"},
		{"role", "role/reviewer/foo-1a2b.md", "role", "reviewer"},
		{"agent has no value", "agent/bot/foo-1a2b.md", "agent", ""},
		{"empty path", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, value := tierFromPath(tc.path)
			assert.Equal(t, tc.wantTier, tier)
			assert.Equal(t, tc.wantValue, value)
		})
	}
}

// TestListReviewBranchesComputesAgeAndTier is the base case: a single
// unmerged review branch is reported with the age and tier a librarian
// sweep step needs to decide whether to notify or escalate.
func TestListReviewBranchesComputesAgeAndTier(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	e := commitReviewBranchAt(t, store, "rig-topic", []string{"rig:web"}, base)

	now := base.Add(30 * time.Hour)
	branches, err := ListReviewBranches(t.Context(), store, now)
	require.NoError(t, err)
	require.Len(t, branches, 1)

	got := branches[0]
	assert.Equal(t, "remember/"+e.ID, got.Name)
	assert.Equal(t, e.ID, got.EntryID)
	assert.Equal(t, "rig", got.Tier)
	assert.Equal(t, "web", got.Value)
	assert.Equal(t, 30*time.Hour, got.Age)
	assert.Empty(t, got.Error)
}

// TestListReviewBranchesExcludesMergedBranches covers the AC's "merged... is
// excluded" case: once a reviewer merges a review branch into the store's
// checked-out branch, it must stop being reported as awaiting review, even
// though the ref itself is still there.
func TestListReviewBranchesExcludesMergedBranches(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	e := commitReviewBranchAt(t, store, "merged-topic", nil, base)

	// Create left the entry file sitting untracked in store's own working
	// tree (CommitToReviewBranch only ever commits a copy of it, in an
	// isolated worktree) -- a real reviewer would be on a separate clone
	// without that stray file, so remove it before merging in place here.
	require.NoError(t, os.Remove(e.BodyPath))

	out, err := exec.CommandContext(t.Context(), "git", "-C", store,
		"merge", "--no-ff", "-m", "merge review branch", "remember/"+e.ID).CombinedOutput()
	require.NoErrorf(t, err, "git merge: %s", out)

	branches, err := ListReviewBranches(t.Context(), store, base.Add(48*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, branches, "a branch already merged into the checked-out branch must not be reported as awaiting review")
}

// TestListReviewBranchesTierFromPathNotBranchName is the AC's explicit
// "never parsed from the branch name" requirement: a topic_key that
// contains a tier name as a plain substring must not fool tier resolution.
func TestListReviewBranchesTierFromPathNotBranchName(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	e := commitReviewBranchAt(t, store, "role-report", []string{"rig:web"}, base)

	branches, err := ListReviewBranches(t.Context(), store, base.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.Contains(t, branches[0].Name, "role",
		"precondition: the branch name must actually contain the confusable substring for this test to be meaningful")
	assert.Equal(t, "rig", branches[0].Tier, "tier must come from the changed file's path, not a substring match on the branch name")
	assert.Equal(t, "web", branches[0].Value)
	_ = e
}

// TestListReviewBranchesNewCommitResetsAge covers the AC's "actioned between
// passes (new commit...) is excluded" case: a follow-up commit (e.g.
// addressing review feedback) must move the branch's age back to ~0,
// relative to its new tip, not the branch's original commit.
func TestListReviewBranchesNewCommitResetsAge(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	e := commitReviewBranchAt(t, store, "amended-topic", nil, base)

	wt := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "-C", store, "worktree", "add", wt, "remember/"+e.ID).CombinedOutput()
	require.NoErrorf(t, err, "git worktree add: %s", out)
	t.Cleanup(func() {
		_, _ = exec.CommandContext(t.Context(), "git", "-C", store, "worktree", "remove", "--force", wt).CombinedOutput()
	})

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	target := filepath.Join(wt, rel)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	// target is confined to a t.TempDir() worktree; rel comes from this
	// test's own hardcoded topic key, not attacker-controlled input.
	//nolint:gosec // see comment above
	require.NoError(t, os.WriteFile(target, append(content, []byte("\nfollow-up edit\n")...), 0o600))

	amendedAt := base.Add(96 * time.Hour)
	t.Setenv("GIT_AUTHOR_DATE", amendedAt.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", amendedAt.Format(time.RFC3339))
	out, err = exec.CommandContext(t.Context(), "git", "-C", wt, "commit", "-q", "-am", "address review feedback").CombinedOutput()
	require.NoErrorf(t, err, "git commit: %s", out)

	now := base.Add(97 * time.Hour)
	branches, err := ListReviewBranches(t.Context(), store, now)
	require.NoError(t, err)
	require.Len(t, branches, 1)
	assert.Equal(t, time.Hour, branches[0].Age,
		"a new commit on the review branch must reset its age to the new tip's timestamp, not the original commit's")
	assert.Equal(t, "global", branches[0].Tier, "the changed-file tier must still resolve correctly after a follow-up commit")
}

// TestListReviewBranchesExcludesDeletedBranches covers the AC's
// "...or deleted) is excluded" case. A deleted branch has no ref left to
// list, so this is really confirming ListReviewBranches doesn't somehow
// remember or fabricate an entry for it.
func TestListReviewBranchesExcludesDeletedBranches(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	e := commitReviewBranchAt(t, store, "deleted-topic", nil, base)

	out, err := exec.CommandContext(t.Context(), "git", "-C", store, "branch", "-D", "remember/"+e.ID).CombinedOutput()
	require.NoErrorf(t, err, "git branch -D: %s", out)

	branches, err := ListReviewBranches(t.Context(), store, base.Add(48*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, branches)
}

// TestListReviewBranchesMultipleTiers exercises global/rig/role/agent in one
// store, confirming each branch resolves its own tier/value independently.
func TestListReviewBranchesMultipleTiers(t *testing.T) {
	store := branchStore(t)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	g := commitReviewBranchAt(t, store, "g-topic", nil, base)
	r := commitReviewBranchAt(t, store, "r-topic", []string{"rig:alpha"}, base)
	o := commitReviewBranchAt(t, store, "o-topic", []string{"role:reviewer"}, base)
	a := commitReviewBranchAt(t, store, "a-topic", []string{"agent:bot"}, base)

	branches, err := ListReviewBranches(t.Context(), store, base.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, branches, 4)

	byID := map[string]ReviewBranch{}
	for _, b := range branches {
		byID[b.EntryID] = b
	}
	assert.Equal(t, "global", byID[g.ID].Tier)
	assert.Equal(t, "rig", byID[r.ID].Tier)
	assert.Equal(t, "alpha", byID[r.ID].Value)
	assert.Equal(t, "role", byID[o.ID].Tier)
	assert.Equal(t, "reviewer", byID[o.ID].Value)
	assert.Equal(t, "agent", byID[a.ID].Tier)
}

// isMergedInto is exercised indirectly through ListReviewBranches above for
// its ordinary true/false outcomes; this pins down its error path
// separately, since a genuine git failure (as opposed to the clean "not an
// ancestor" exit status) is otherwise hard to observe from
// ListReviewBranches' output alone.
func TestIsMergedIntoUnknownRefIsAnError(t *testing.T) {
	store := branchStore(t)
	_, err := isMergedInto(t.Context(), store, "remember/does-not-exist", "HEAD")
	require.Error(t, err, "a nonexistent ref must be reported as an error, not silently treated as not-merged")
}
