package cairn

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:gosec // AWS's own documented placeholder access key id, not a real credential
const testAWSKeyExample = "AKIAIOSFODNN7EXAMPLE"

// reviewStore creates a git-initialized store with one seed commit, so
// DefaultBranch and the diff machinery have a real, resolvable HEAD to work
// against -- mirroring TestCommitDirectCommitsOnlyTheEntryFile's setup.
func reviewStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")
	return store
}

// fixtureBranch builds a shared-tier review branch through the same
// production path `cairn remember` uses (NewEntry -> Create ->
// CommitToReviewBranch), so list/show/merge tests exercise exactly what a
// real review branch looks like -- including Create's untracked draft copy
// left behind in store's own working tree.
func fixtureBranch(t *testing.T, store, topicKey string, scope []string, body string) (branch string, e *Entry) {
	t.Helper()
	e, err := NewEntry(topicKey, scope, body, "agent:bot")
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	branch, err = e.CommitToReviewBranch(t.Context(), store)
	require.NoError(t, err)
	return branch, e
}

func TestDefaultBranch(t *testing.T) {
	store := reviewStore(t)
	want, err := gitRun(t.Context(), store, "branch", "--show-current")
	require.NoError(t, err)

	got, err := DefaultBranch(t.Context(), store)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(want), got)
}

func TestDefaultBranchErrorsOutsideGitRepo(t *testing.T) {
	_, err := DefaultBranch(t.Context(), t.TempDir())
	require.Error(t, err)
}

func TestListReviewBranchesEmptyStore(t *testing.T) {
	store := reviewStore(t)
	branches, err := ListReviewBranches(t.Context(), store)
	require.NoError(t, err)
	assert.Empty(t, branches)
}

// TestListReviewBranchesDerivesTierFromEntryPath covers crn-ffm's acceptance
// criterion directly: tier derivation, for at least one branch per tier,
// read from the changed entry file's path -- never the branch name, which
// only ever encodes the entry's random id (reviewBranchName).
func TestListReviewBranchesDerivesTierFromEntryPath(t *testing.T) {
	store := reviewStore(t)
	globalBranch, _ := fixtureBranch(t, store, "global-topic", nil, "a global note")
	rigBranch, _ := fixtureBranch(t, store, "rig-topic", []string{"rig:web"}, "a rig note")
	roleBranch, _ := fixtureBranch(t, store, "role-topic", []string{"role:reviewer"}, "a role note")

	branches, err := ListReviewBranches(t.Context(), store)
	require.NoError(t, err)
	require.Len(t, branches, 3)

	byName := make(map[string]ReviewBranch, len(branches))
	for _, b := range branches {
		byName[b.Name] = b
	}

	global := byName[globalBranch]
	assert.Equal(t, "global", global.Tier)
	assert.Empty(t, global.TierValue)
	assert.True(t, strings.HasPrefix(global.EntryPath, "global/"))

	rig := byName[rigBranch]
	assert.Equal(t, "rig", rig.Tier)
	assert.Equal(t, "web", rig.TierValue)
	assert.True(t, strings.HasPrefix(rig.EntryPath, "rig/web/"))

	role := byName[roleBranch]
	assert.Equal(t, "role", role.Tier)
	assert.Equal(t, "reviewer", role.TierValue)
	assert.True(t, strings.HasPrefix(role.EntryPath, "role/reviewer/"))
}

// TestListReviewBranchesErrorsOnPrivateTierBranch documents the other side
// of tierFromEntryPath's contract: ListReviewBranches has no tier filter of
// its own, so a remember/* branch that (unusually) changes a file under
// agent/ surfaces as an explicit error rather than being silently skipped or
// reported as a fourth tier. In production cmd/remember.go never routes a
// private-tier entry through CommitToReviewBranch, but the guarantee lives
// here, not there.
func TestListReviewBranchesErrorsOnPrivateTierBranch(t *testing.T) {
	store := reviewStore(t)
	fixtureBranch(t, store, "private-topic", []string{"agent:bot"}, "a private note")

	_, err := ListReviewBranches(t.Context(), store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private agent/ tier")
}

func TestTierFromEntryPath(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		wantTier  string
		wantValue string
		wantErr   string
	}{
		{name: "global", path: "global/foo.md", wantTier: "global"},
		{name: "rig", path: "rig/web/foo.md", wantTier: "rig", wantValue: "web"},
		{name: "role", path: "role/reviewer/foo.md", wantTier: "role", wantValue: "reviewer"},
		{name: "agent is private", path: "agent/bot/foo.md", wantErr: "private agent/ tier"},
		{name: "too short", path: "global.md", wantErr: "too short"},
		{name: "unrecognized top-level dir", path: "other/foo.md", wantErr: "unrecognized top-level scope dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, value, err := tierFromEntryPath(tc.path)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantTier, tier)
			assert.Equal(t, tc.wantValue, value)
		})
	}
}

func TestChangedEntryFileErrorsWhenBranchTouchesMoreThanOneFile(t *testing.T) {
	store := reviewStore(t)
	def, err := DefaultBranch(t.Context(), store)
	require.NoError(t, err)

	_, err = gitRun(t.Context(), store, "checkout", "-q", "-b", "remember/multi")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(store+"/a.md", []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(store+"/b.md", []byte("b"), 0o600))
	gitCommitAll(t, store, "two files")
	_, err = gitRun(t.Context(), store, "checkout", "-q", def)
	require.NoError(t, err)

	_, err = changedEntryFile(t.Context(), store, def, "remember/multi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 1 entry file")
}

func TestShowReviewBranch(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "shown-topic", []string{"rig:web"}, "the body text")

	diff, entry, err := ShowReviewBranch(t.Context(), store, branch)
	require.NoError(t, err)
	assert.Contains(t, diff, "the body text")
	assert.Contains(t, diff, e.ID)
	assert.Equal(t, e.ID, entry.ID)
	assert.Equal(t, "shown-topic", entry.TopicKey)
	assert.Equal(t, []string{"rig:web"}, entry.Scope)
	assert.Equal(t, "the body text", entry.Body)
}

func TestShowReviewBranchErrorsOnUnknownBranch(t *testing.T) {
	store := reviewStore(t)
	_, _, err := ShowReviewBranch(t.Context(), store, "remember/does-not-exist")
	require.Error(t, err)
}

func TestParseEntryContentRejectsNonEntryContent(t *testing.T) {
	_, err := parseEntryContent([]byte("no frontmatter here"), "src")
	require.Error(t, err)
	assert.ErrorIs(t, err, errNotEntry)
}

func TestParseEntryContentRoundTrip(t *testing.T) {
	e, err := NewEntry("tk", []string{"rig:web"}, "body line", "agent:bot")
	require.NoError(t, err)
	raw, err := e.marshal()
	require.NoError(t, err)

	got, err := parseEntryContent(raw, "branch:path")
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.TopicKey, got.TopicKey)
	assert.Equal(t, e.Scope, got.Scope)
	assert.Equal(t, e.Body, got.Body)
}

func TestDetectSecretPattern(t *testing.T) {
	cases := map[string]struct {
		text string
		want string
	}{
		"clean text":     {"just a normal note about deploys", ""},
		"aws access key": {"key is " + testAWSKeyExample + " here", "AWS access key ID"},
		"github token":   {"token ghp_" + strings.Repeat("a", 36), "GitHub token"},
		"private key block": {
			"-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
			"private key block",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, detectSecretPattern(tc.text))
		})
	}
}

// TestMergeReviewBranchSucceeds covers the acceptance-criteria core of
// MergeReviewBranch: a real --no-ff merge commit (never a fast-forward, even
// though this branch is trivially fast-forwardable), the documented
// "librarian: merge <branch> — <title> (<bead-id>)" message convention, and
// the branch being gone afterward.
func TestMergeReviewBranchSucceeds(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "a note about the rig")

	res, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey: "curated-topic",
		Bead:     "crn-123",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.SHA)

	head, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(head), res.SHA)

	// --no-ff: the branch was a direct, fast-forwardable descendant of the
	// default branch, so a genuine merge commit (two parents) is the only
	// way this could be true -- a fast-forward would leave just one.
	parents, err := gitRun(t.Context(), store, "rev-list", "--parents", "-n", "1", "HEAD")
	require.NoError(t, err)
	assert.Len(t, strings.Fields(strings.TrimSpace(parents)), 3, "HEAD must be a merge commit with two parents (--no-ff), not a fast-forward")

	subject, err := gitRun(t.Context(), store, "log", "-1", "--format=%s")
	require.NoError(t, err)
	assert.Equal(t, "librarian: merge "+branch+" — "+e.Title+" (crn-123)", strings.TrimSpace(subject))

	branchList, err := gitRun(t.Context(), store, "branch", "--list", branch)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(branchList), "the review branch must be deleted after a successful merge")

	got, err := Find(store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, "curated-topic", got.TopicKey, "topic_key must be curated to the reviewer's --topic-key, not the contributor's draft value")
}

func TestMergeReviewBranchMessageFallsBackToTopicKeyWithoutBead(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "a note")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "curated-topic"})
	require.NoError(t, err)

	subject, err := gitRun(t.Context(), store, "log", "-1", "--format=%s")
	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(subject), "(curated-topic)")
}

func TestMergeReviewBranchReindexes(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "note")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "curated-topic"})
	require.NoError(t, err)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var topic string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT topic_key FROM entries WHERE id = ?", e.ID).Scan(&topic))
	assert.Equal(t, "curated-topic", topic)
}

func TestMergeReviewBranchLeavesScopeAndAnchorTypeUntouchedWhenOmitted(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "a note")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "curated"})
	require.NoError(t, err)

	got, err := Find(store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"rig:web"}, got.Scope, "omitted --scope must leave the contributor's scope untouched")
	assert.Equal(t, "none", got.Anchor.Type, "omitted --anchor-type must leave the contributor's anchor type untouched")
}

// TestMergeReviewBranchAppliesScopeAndAnchorTypeWhenGiven documents a
// deliberate v1 limitation alongside the behavior it checks: patching
// --scope only rewrites the frontmatter's scope tags, it never relocates
// the entry file to match its new declared tier (Create's scopeDir
// directory-per-scope placement is a write-time convention, not an
// invariant maintained elsewhere -- IterEntries/Visible/shadow all resolve
// scope from frontmatter, never from directory, so this is inert, not
// broken).
func TestMergeReviewBranchAppliesScopeAndAnchorTypeWhenGiven(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "a note")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey:   "curated",
		Scope:      []string{"role:reviewer"},
		AnchorType: "files",
	})
	require.NoError(t, err)

	got, err := Find(store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"role:reviewer"}, got.Scope)
	assert.Equal(t, "files", got.Anchor.Type)
}

func TestMergeReviewBranchBlocksObviousSecretByDefault(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "leaked key: "+testAWSKeyExample)
	headBefore, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)

	_, err = MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "curated"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS access key ID")
	assert.Contains(t, err.Error(), "--allow-secret-pattern")

	headAfter, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter), "a blocked merge must leave the default branch exactly as it was")

	branchList, err := gitRun(t.Context(), store, "branch", "--list", branch)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(branchList), "the review branch must survive a blocked merge, not be deleted")
}

func TestMergeReviewBranchAllowSecretPatternOverridesGuard(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "leaked key: "+testAWSKeyExample)

	res, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey:           "curated",
		AllowSecretPattern: true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.SHA)
}

// TestMergeReviewBranchConflictLeavesNoPartialState covers NFR-2: a merge
// that fails on a genuine content conflict (as opposed to the secret guard
// or validation, which both already return before touching git at all) must
// leave the default branch's HEAD exactly as it was and the working tree
// clean, not half-merged. Forced by having both branch and the default
// branch independently add the same entry path with different content, so
// git cannot resolve it automatically.
func TestMergeReviewBranchConflictLeavesNoPartialState(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "a note")

	require.NoError(t, os.WriteFile(e.BodyPath, []byte("+++\nid = \"conflict\"\n+++\n\nconflicting content\n"), 0o600))
	gitCommitAll(t, store, "conflicting change")

	headBefore, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)

	_, err = MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "curated"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted")

	headAfter, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter), "a failed merge must leave the default branch's HEAD unchanged")

	status, err := gitRun(t.Context(), store, "status", "--porcelain")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(status), "a failed, aborted merge must leave a clean working tree, not conflict markers")
}

func TestMergeReviewBranchRejectsInvalidTopicKey(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "a note")
	headBefore, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)

	_, err = MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{TopicKey: "../evil"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--topic-key")

	headAfter, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter), "an invalid --topic-key must be rejected before any git mutation")

	branchList, err := gitRun(t.Context(), store, "branch", "--list", branch)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(branchList), "the branch must survive a rejected merge")
}

func TestMergeReviewBranchRejectsInvalidScopeTag(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "a note")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey: "curated",
		Scope:    []string{"../evil"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tag")
}

// TestMergeReviewBranchRejectsInvalidAnchorType pins the exact corruption
// case a real reviewer hit: an embedded newline in --anchor-type reached
// tomlQuote unvalidated, splitting the frontmatter's "type = ..." line into
// invalid TOML that merged successfully before Reindex choked on it --
// corrupting the default branch and breaking every store read path
// (IterEntries) until someone hand-fixed the file. AnchorType must be
// rejected up front, exactly like TopicKey and Scope.
func TestMergeReviewBranchRejectsInvalidAnchorType(t *testing.T) {
	store := reviewStore(t)
	branch, _ := fixtureBranch(t, store, "draft-topic", nil, "a note")
	headBefore, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)

	_, err = MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey:   "curated",
		AnchorType: "files\"\n[bogus]\nevil = \"1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--anchor-type")

	headAfter, err := gitRun(t.Context(), store, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(headBefore), strings.TrimSpace(headAfter), "an invalid --anchor-type must be rejected before any git mutation")

	branchList, err := gitRun(t.Context(), store, "branch", "--list", branch)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(branchList), "the branch must survive a rejected merge")
}

// TestPatchFrontmatterFieldsOnlyTouchesRequestedFields is the core "surgical
// patch, not a full re-encode" guarantee (crn-6az.5.1): every line not named
// by opts survives byte for byte, in place, including the body.
func TestPatchFrontmatterFieldsOnlyTouchesRequestedFields(t *testing.T) {
	e, err := NewEntry("draft", []string{"agent:bot"}, "body text", "agent:bot")
	require.NoError(t, err)
	raw, err := e.marshal()
	require.NoError(t, err)

	patched, err := patchFrontmatterFields(raw, ReviewMergeOptions{TopicKey: "curated"})
	require.NoError(t, err)

	s := string(patched)
	assert.Contains(t, s, `topic_key = "curated"`)
	assert.Contains(t, s, `id = "`+e.ID+`"`)
	assert.Contains(t, s, `scope = ["agent:bot"]`, "scope must be left untouched when opts.Scope is nil")
	assert.Contains(t, s, `type = "none"`, "anchor type must be left untouched when opts.AnchorType is empty")
	assert.Contains(t, s, "body text", "body must survive untouched")
}

func TestPatchFrontmatterFieldsAppliesScopeAndAnchorTypeWhenGiven(t *testing.T) {
	e, err := NewEntry("draft", []string{"agent:bot"}, "body text", "agent:bot")
	require.NoError(t, err)
	raw, err := e.marshal()
	require.NoError(t, err)

	patched, err := patchFrontmatterFields(raw, ReviewMergeOptions{
		TopicKey:   "curated",
		Scope:      []string{"rig:web", "role:reviewer"},
		AnchorType: "files",
	})
	require.NoError(t, err)

	s := string(patched)
	assert.Contains(t, s, `scope = ["rig:web", "role:reviewer"]`)
	assert.Contains(t, s, `type = "files"`)
}

// TestPatchFrontmatterFieldsIsLineForLineSurgical proves the "not a full
// WriteBack re-encode" requirement precisely: patching one field changes
// exactly that one line, and every other line -- including line count and
// order -- is untouched.
func TestPatchFrontmatterFieldsIsLineForLineSurgical(t *testing.T) {
	e, err := NewEntry("draft", []string{"agent:bot"}, "body text\nsecond line", "agent:bot")
	require.NoError(t, err)
	raw, err := e.marshal()
	require.NoError(t, err)

	patched, err := patchFrontmatterFields(raw, ReviewMergeOptions{TopicKey: "curated"})
	require.NoError(t, err)

	origLines := strings.Split(string(raw), "\n")
	newLines := strings.Split(string(patched), "\n")
	require.Equal(t, len(origLines), len(newLines), "a single scalar-field patch must not add or remove any line")
	for i := range origLines {
		if strings.HasPrefix(strings.TrimSpace(origLines[i]), "topic_key") {
			assert.Equal(t, `topic_key = "curated"`, newLines[i])
			continue
		}
		assert.Equal(t, origLines[i], newLines[i], "line %d must be untouched", i)
	}
}

func TestSetScalarLineReplacesExisting(t *testing.T) {
	lines := []string{`id = "x"`, `title = "T"`, `topic_key = "old"`, `[anchor]`, `type = "none"`}
	out := setScalarLine(lines, "topic_key", `"new"`)
	assert.Equal(t, []string{`id = "x"`, `title = "T"`, `topic_key = "new"`, `[anchor]`, `type = "none"`}, out)
}

func TestSetScalarLineInsertsAfterIDWhenMissing(t *testing.T) {
	lines := []string{`id = "x"`, `title = "T"`}
	out := setScalarLine(lines, "topic_key", `"new"`)
	assert.Equal(t, []string{`id = "x"`, `topic_key = "new"`, `title = "T"`}, out)
}

// TestSetScalarLineNeverTouchesSameKeyInsideTable is the disambiguation the
// naive approach gets wrong: Entry.Type (top-level "type") and Anchor.Type
// ([anchor]'s "type") serialize to the same bare key name. setScalarLine's
// top-level search must stop at the first "[table]" header.
func TestSetScalarLineNeverTouchesSameKeyInsideTable(t *testing.T) {
	lines := []string{`id = "x"`, `type = "decision"`, `[anchor]`, `type = "none"`}
	out := setScalarLine(lines, "type", `"changed"`)
	assert.Equal(t, []string{`id = "x"`, `type = "changed"`, `[anchor]`, `type = "none"`}, out)
}

func TestSetAnchorTypeLineOnlyTouchesAnchorTable(t *testing.T) {
	lines := []string{`id = "x"`, `type = "decision"`, `[anchor]`, `type = "none"`}
	out, err := setAnchorTypeLine(lines, `"files"`)
	require.NoError(t, err)
	assert.Equal(t, []string{`id = "x"`, `type = "decision"`, `[anchor]`, `type = "files"`}, out)
}

func TestSetAnchorTypeLineErrorsWithoutAnchorTable(t *testing.T) {
	lines := []string{`id = "x"`, `title = "T"`}
	_, err := setAnchorTypeLine(lines, `"files"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "[anchor]")
}

func TestTomlQuoteEscapesBackslashAndDoubleQuote(t *testing.T) {
	assert.Equal(t, `"plain"`, tomlQuote("plain"))
	assert.Equal(t, `"has \"quote\""`, tomlQuote(`has "quote"`))
	assert.Equal(t, `"back\\slash"`, tomlQuote(`back\slash`))
}

func TestTomlArray(t *testing.T) {
	assert.Equal(t, `[]`, tomlArray(nil))
	assert.Equal(t, `["rig:web"]`, tomlArray([]string{"rig:web"}))
	assert.Equal(t, `["rig:web", "role:reviewer"]`, tomlArray([]string{"rig:web", "role:reviewer"}))
}
