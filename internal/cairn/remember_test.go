package cairn

import (
	"context"
	"fmt"
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

// TestNewEntryParamsTitleSummaryOverride covers crn-lzn4.1.1's FR-3: an
// explicit Title/Summary in NewEntryParams must win over titleAndSummary's
// auto-derivation from Body, not merely supplement it.
func TestNewEntryParamsTitleSummaryOverride(t *testing.T) {
	e, err := NewEntry(NewEntryParams{
		TopicKey: "t",
		Body:     "auto title\nauto summary from body",
		Title:    "explicit title",
		Summary:  "explicit summary",
	})
	require.NoError(t, err)
	assert.Equal(t, "explicit title", e.Title)
	assert.Equal(t, "explicit summary", e.Summary)
}

// TestNewEntryParamsTitleSummaryDefaultToAutoDerivation covers the omitted
// side of FR-3: leaving Title/Summary unset must fall back to exactly
// titleAndSummary's existing auto-derivation, unchanged from today.
func TestNewEntryParamsTitleSummaryDefaultToAutoDerivation(t *testing.T) {
	e, err := NewEntry(NewEntryParams{
		TopicKey: "t",
		Body:     "auto title\nauto title\nauto summary from body",
	})
	require.NoError(t, err)
	assert.Equal(t, "auto title", e.Title)
	assert.Equal(t, "auto title\nauto title\nauto summary from body", e.Summary)
}

// TestNewEntryParamsCallerSuppliedAnchorIsUsedVerbatim covers crn-lzn4.1.1's
// FR-4: a caller-supplied Anchor (built from --anchor-repo/--anchor-path at
// the CLI layer) must be stored on the entry exactly as given.
func TestNewEntryParamsCallerSuppliedAnchorIsUsedVerbatim(t *testing.T) {
	e, err := NewEntry(NewEntryParams{
		TopicKey: "t",
		Body:     "body",
		Anchor:   Anchor{Type: "files", Repo: "/tmp/repo", Paths: []string{"a.go", "b.go"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Equal(t, "/tmp/repo", e.Anchor.Repo)
	assert.Equal(t, []string{"a.go", "b.go"}, e.Anchor.Paths)
}

// TestNewEntryParamsAnchorDefaultsToNoneWhenZeroValue confirms an omitted
// Anchor field (the zero value, Type == "") still defaults to {Type: "none"},
// matching today's hardcoded behavior -- capture without any anchor flags
// must be unaffected by FR-4.
func TestNewEntryParamsAnchorDefaultsToNoneWhenZeroValue(t *testing.T) {
	e, err := NewEntry(NewEntryParams{TopicKey: "t", Body: "body"})
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

func TestEntryCreateOmitsZeroHitCount(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	require.Equal(t, 0, e.HitCount, "a freshly constructed entry must start at the zero value")

	store := t.TempDir()
	require.NoError(t, e.Create(store))

	raw, err := os.ReadFile(e.BodyPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "hit_count",
		"a zero HitCount must not appear in the serialized frontmatter at all")
}

func TestEntryCreateSerializesNonZeroHitCount(t *testing.T) {
	e, err := NewEntry("t", nil, "body", "")
	require.NoError(t, err)
	e.HitCount = 7

	store := t.TempDir()
	require.NoError(t, e.Create(store))

	raw, err := os.ReadFile(e.BodyPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "hit_count", "a non-zero HitCount must still be serialized")

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, 7, got.HitCount)
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

// TestCommitRecurrenceToReviewBranchCreatesBranchWhenAbsent covers
// CommitRecurrenceToReviewBranch's fallback-to-create path (crn-28ge.1.4):
// when the entry's own remember/<id> branch does not exist yet, it must
// behave identically to CommitToReviewBranch itself -- exactly one commit,
// forked from the store's pre-existing HEAD, the store's own branch/HEAD
// left untouched.
func TestCommitRecurrenceToReviewBranchCreatesBranchWhenAbsent(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	e.RecurrenceCount = 1
	require.NoError(t, e.WriteBackRecurrenceCount())

	branch, err := e.CommitRecurrenceToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, "remember/"+e.ID, branch)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	changed, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-only", "-r", branch)
	require.NoError(t, err)
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the review commit must contain only the entry file")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(parent),
		"the fallback-to-create path must fork from the store's pre-existing HEAD and add exactly one commit on top, same as CommitToReviewBranch")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "recurrence")
	assert.Contains(t, msg, "count 1")

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the store's own HEAD must be unaffected")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1, "the scratch review worktree must be cleaned up")
}

// TestCommitRecurrenceToReviewBranchAppendsSecondCommitToExistingBranch
// covers the ordinary case (crn-28ge.1.4): a recurrence hit almost always
// happens while the entry's original review commit from CommitToReviewBranch
// is still pending on its remember/<id> branch -- a recurring topic tends to
// recur before anyone has reviewed the first report, not only after.
// CommitRecurrenceToReviewBranch must append a second commit to that SAME
// branch rather than fail on "branch already exists" (which reusing
// CommitToReviewBranch as-is would do on this, the common case) or fork a
// competing branch.
func TestCommitRecurrenceToReviewBranchAppendsSecondCommitToExistingBranch(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	firstBranch, err := e.CommitToReviewBranch(ctx, store)
	require.NoError(t, err)
	firstCommit, err := gitRun(ctx, store, "rev-parse", firstBranch)
	require.NoError(t, err)
	firstCommit = strings.TrimSpace(firstCommit)

	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	e.RecurrenceCount = 1
	require.NoError(t, e.WriteBackRecurrenceCount())
	branch, err := e.CommitRecurrenceToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, firstBranch, branch, "a recurrence commit must reuse the entry's existing review branch, not a new one")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, firstCommit, strings.TrimSpace(parent),
		"the recurrence commit must be appended on top of the original review commit, not replace or precede it, and must be the only new commit")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "recurrence")
	assert.Contains(t, msg, "count 1")

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter),
		"CommitRecurrenceToReviewBranch must not switch the store's checked-out branch")
	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the store's own HEAD must be unaffected")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"the scratch review worktree must be cleaned up, leaving only the store's own")
}

// TestCommitPromotionToReviewBranchCreatesBranchWhenAbsent covers
// CommitPromotionToReviewBranch's fallback-to-create path (crn-ghn8.1): when
// the entry's own remember/<id> branch does not exist yet, it must behave
// identically to CommitToReviewBranch itself -- exactly one commit, forked
// from the store's pre-existing HEAD, the store's own branch/HEAD left
// untouched. Mirrors TestCommitRecurrenceToReviewBranchCreatesBranchWhenAbsent
// one field over.
func TestCommitPromotionToReviewBranchCreatesBranchWhenAbsent(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID())

	branch, err := e.CommitPromotionToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, "remember/"+e.ID, branch)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	changed, err := gitRun(ctx, store, "diff-tree", "--no-commit-id", "--name-only", "-r", branch)
	require.NoError(t, err)
	assert.Equal(t, []string{rel}, strings.Fields(changed), "the review commit must contain only the entry file")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(parent),
		"the fallback-to-create path must fork from the store's pre-existing HEAD and add exactly one commit on top, same as CommitToReviewBranch")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "promote")
	assert.Contains(t, msg, "crn-abcd")

	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the store's own HEAD must be unaffected")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1, "the scratch review worktree must be cleaned up")
}

// TestCommitPromotionToReviewBranchAppendsSecondCommitToExistingBranch covers
// the ordinary case (crn-ghn8.1): a promotion frequently happens while the
// entry's original review commit from CommitToReviewBranch is still pending
// on its remember/<id> branch. CommitPromotionToReviewBranch must append a
// second commit to that SAME branch rather than fail on "branch already
// exists" or fork a competing branch -- mirrors
// TestCommitRecurrenceToReviewBranchAppendsSecondCommitToExistingBranch one
// field over.
func TestCommitPromotionToReviewBranchAppendsSecondCommitToExistingBranch(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	firstBranch, err := e.CommitToReviewBranch(ctx, store)
	require.NoError(t, err)
	firstCommit, err := gitRun(ctx, store, "rev-parse", firstBranch)
	require.NoError(t, err)
	firstCommit = strings.TrimSpace(firstCommit)

	branchBefore, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	headBefore, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	headBefore = strings.TrimSpace(headBefore)

	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID())
	branch, err := e.CommitPromotionToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, firstBranch, branch, "a promotion commit must reuse the entry's existing review branch, not a new one")

	parent, err := gitRun(ctx, store, "rev-parse", branch+"~1")
	require.NoError(t, err)
	assert.Equal(t, firstCommit, strings.TrimSpace(parent),
		"the promotion commit must be appended on top of the original review commit, not replace or precede it, and must be the only new commit")

	msg, err := gitRun(ctx, store, "log", "-1", "--format=%B", branch)
	require.NoError(t, err)
	assert.Contains(t, msg, e.ID)
	assert.Contains(t, msg, "promote")
	assert.Contains(t, msg, "crn-abcd")

	branchAfter, err := gitRun(ctx, store, "branch", "--show-current")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(branchBefore), strings.TrimSpace(branchAfter),
		"CommitPromotionToReviewBranch must not switch the store's checked-out branch")
	headAfter, err := gitRun(ctx, store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, headBefore, strings.TrimSpace(headAfter), "the store's own HEAD must be unaffected")

	worktrees, err := gitRun(ctx, store, "worktree", "list")
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(worktrees), "\n"), 1,
		"the scratch review worktree must be cleaned up, leaving only the store's own")
}

func TestCommitDirectLogsWritePathAndSteps(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	recs := testLogRecords(t, func(ctx context.Context) {
		_, err := e.CommitDirect(ctx, store)
		require.NoError(t, err)
	})

	require.Len(t, recs, 4, "expected 1 write_path + 3 write_path_step (git_add, git_commit, git_rev_parse_head)")

	wp := recs[0]
	assert.Equal(t, "write_path", wp["kind"])
	assert.Equal(t, "commit_direct", wp["operation"])
	assert.Equal(t, []any{"agent:bot"}, wp["scope"])
	assert.Equal(t, "agent", wp["tier"])
	assert.Equal(t, true, wp["private"])

	wantSteps := []string{"git_add", "git_commit", "git_rev_parse_head"}
	for i, name := range wantSteps {
		step := recs[i+1]
		assert.Equal(t, "write_path_step", step["kind"])
		assert.Equal(t, "commit_direct", step["operation"])
		assert.Equal(t, name, step["name"])
		assert.Equal(t, "ok", step["outcome"])
		assert.GreaterOrEqual(t, step["duration_ms"], float64(0))
	}
}

func TestCommitDirectFailureLogsStepOutcome(t *testing.T) {
	store := t.TempDir() // deliberately not a git repo

	e, err := NewEntry("build-flags", []string{"agent:bot"}, "body", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	recs := testLogRecords(t, func(ctx context.Context) {
		_, err := e.CommitDirect(ctx, store)
		require.Error(t, err)
	})

	require.Len(t, recs, 2, "expected 1 write_path + 1 write_path_step (git_add) -- CommitDirect must stop at the first failing step")
	assert.Equal(t, "write_path", recs[0]["kind"])

	step := recs[1]
	assert.Equal(t, "write_path_step", step["kind"])
	assert.Equal(t, "commit_direct", step["operation"])
	assert.Equal(t, "git_add", step["name"])
	assert.Equal(t, "error", step["outcome"])
	assert.NotEmpty(t, step["detail"], "a failed step must record what went wrong")
}

func TestGitStepRedactsSecretsInErrorDetail(t *testing.T) {
	recs := testLogRecords(t, func(ctx context.Context) {
		_, err := gitStep(ctx, "test_op", "test_step", func() (string, error) {
			return "", fmt.Errorf("remote rejected: token %s invalid", testAWSKeyExample)
		})
		require.Error(t, err)
	})

	require.Len(t, recs, 1)
	step := recs[0]
	assert.Equal(t, "error", step["outcome"])
	assert.NotContains(t, step["detail"], testAWSKeyExample, "a secret-shaped string must never reach the log verbatim")
	assert.Contains(t, step["detail"], "[redacted:AWS access key ID]")
}

func TestCommitToReviewBranchLogsWritePathAndSteps(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	recs := testLogRecords(t, func(ctx context.Context) {
		_, err := e.CommitToReviewBranch(ctx, store)
		require.NoError(t, err)
	})

	require.Len(t, recs, 5,
		"expected 1 write_path + 4 write_path_step (git_worktree_add, git_add, git_commit, git_worktree_remove)")

	wp := recs[0]
	assert.Equal(t, "write_path", wp["kind"])
	assert.Equal(t, "commit_to_review_branch", wp["operation"])
	assert.Equal(t, []any{"rig:web"}, wp["scope"])
	assert.Equal(t, "rig", wp["tier"])
	assert.Equal(t, false, wp["private"])

	wantSteps := []string{"git_worktree_add", "git_add", "git_commit", "git_worktree_remove"}
	for i, name := range wantSteps {
		step := recs[i+1]
		assert.Equal(t, "write_path_step", step["kind"])
		assert.Equal(t, "commit_to_review_branch", step["operation"])
		assert.Equal(t, name, step["name"])
		assert.Equal(t, "ok", step["outcome"])
	}
}

func TestCommitRecurrenceAndPromotionLogDistinctOperationNames(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry("build-flags", []string{"rig:web"}, "prefer feature flags over env vars", "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	_, err = e.CommitToReviewBranch(t.Context(), store)
	require.NoError(t, err)

	e.RecurrenceCount = 1
	require.NoError(t, e.WriteBackRecurrenceCount())
	recurRecs := testLogRecords(t, func(ctx context.Context) {
		_, err := e.CommitRecurrenceToReviewBranch(ctx, store)
		require.NoError(t, err)
	})
	require.NotEmpty(t, recurRecs)
	assert.Equal(t, "write_path", recurRecs[0]["kind"])
	assert.Equal(t, "commit_recurrence_to_review_branch", recurRecs[0]["operation"])
	for _, rec := range recurRecs {
		if rec["kind"] == "write_path_step" {
			assert.Equal(t, "commit_recurrence_to_review_branch", rec["operation"])
		}
	}

	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID())
	promoRecs := testLogRecords(t, func(ctx context.Context) {
		_, err := e.CommitPromotionToReviewBranch(ctx, store)
		require.NoError(t, err)
	})
	require.NotEmpty(t, promoRecs)
	assert.Equal(t, "write_path", promoRecs[0]["kind"])
	assert.Equal(t, "commit_promotion_to_review_branch", promoRecs[0]["operation"])
	for _, rec := range promoRecs {
		if rec["kind"] == "write_path_step" {
			assert.Equal(t, "commit_promotion_to_review_branch", rec["operation"])
		}
	}
}
