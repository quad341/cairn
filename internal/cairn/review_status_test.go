package cairn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntryCreateStampsPendingReviewStatus covers crn-evw98.1's exit
// contract bullet 2: Create stamps review_status=pending at write time for
// entries that go straight to a tier's working tree.
func TestEntryCreateStampsPendingReviewStatus(t *testing.T) {
	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "build-flags", Scope: []string{"rig:web"}, Body: "prefer feature flags over env vars", CreatedBy: "agent:bot"})
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))
	assert.Equal(t, ReviewStatusPending, e.ReviewStatus, "Create must stamp review_status=pending in memory")

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusPending, got.ReviewStatus, "the on-disk frontmatter must carry the pending stamp too")
}

// TestEntryCreatePrivateTierAlsoStampsPending confirms the pending stamp is
// tier-agnostic, not just applied to entries destined for a shared tier.
func TestEntryCreatePrivateTierAlsoStampsPending(t *testing.T) {
	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "private-note", Scope: []string{"agent:bot"}, Body: "a private note", CreatedBy: "agent:bot"})
	require.NoError(t, err)

	store := t.TempDir()
	require.NoError(t, e.Create(store))

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusPending, got.ReviewStatus)
}

// TestCommitToReviewBranchStampsPendingReviewStatus covers exit-contract
// bullet 3: entries routed to review via CommitToReviewBranch carry
// review_status=pending on the review branch's committed copy.
func TestCommitToReviewBranchStampsPendingReviewStatus(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "build-flags", Scope: []string{"rig:web"}, Body: "prefer feature flags over env vars", CreatedBy: "agent:bot"})
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	branch, err := e.CommitToReviewBranch(ctx, store)
	require.NoError(t, err)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	committed, err := gitRun(ctx, store, "show", branch+":"+rel)
	require.NoError(t, err)
	assert.Contains(t, committed, `review_status = "pending"`)
}

// TestCommitPromotionToReviewBranchStampsPendingReviewStatus mirrors the
// base case above for the promotion sibling -- CommitRecurrenceToReviewBranch
// and CommitPromotionToReviewBranch share commitToReviewWorktree's pending
// stamp, exercised for the harder already-merged case (via the recurrence
// sibling) below.
func TestCommitPromotionToReviewBranchStampsPendingReviewStatus(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "build-flags", Scope: []string{"rig:web"}, Body: "prefer feature flags over env vars", CreatedBy: "agent:bot"})
	require.NoError(t, err)
	require.NoError(t, e.Create(store))
	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID(ctx, store))

	branch, err := e.CommitPromotionToReviewBranch(ctx, store)
	require.NoError(t, err)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)
	committed, err := gitRun(ctx, store, "show", branch+":"+rel)
	require.NoError(t, err)
	assert.Contains(t, committed, `review_status = "pending"`)
}

// TestCommitRecurrenceToReviewBranchOnAlreadyMergedEntryReflipsToPending is
// the crux test for exit-contract bullet 3's substantive case: an entry
// already merged (review_status=merged, tracked on the store's default
// branch) recurs again. The copy landing back on the review branch must
// show review_status=pending once more -- it is not the authoritative,
// merged copy until *this* recurrence is itself reviewed and merged --
// while the store's own working tree, restored to its last-committed HEAD
// content by commitToReviewWorktree, must still show merged. Mirrors
// TestCommitRecurrenceToReviewBranchOnAlreadyMergedEntryRestoresWorkingTree's
// fixture shape, adding an explicit review_status=merged stamp before the
// "simulate merge" step so that step accurately models a real post-merge
// state (a real merge flips this via ReviewMergeBranch/patchFrontmatterFields,
// tested separately below; this fixture deliberately bypasses that
// mechanism to isolate the recurrence side).
func TestCommitRecurrenceToReviewBranchOnAlreadyMergedEntryReflipsToPending(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	gitInit(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "README.md"), []byte("seed\n"), 0o600))
	gitCommitAll(t, store, "seed")

	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "build-flags", Scope: []string{"rig:web"}, Body: "prefer feature flags over env vars", CreatedBy: "agent:bot"})
	require.NoError(t, err)
	require.NoError(t, e.Create(store))

	firstBranch, err := e.CommitToReviewBranch(ctx, store)
	require.NoError(t, err)

	rel, err := filepath.Rel(store, e.BodyPath)
	require.NoError(t, err)

	// commitToReviewWorktree removes the store's own working-tree copy right
	// after the review commit (crn-rymq3); re-materialize it from the review
	// branch to feed the "simulate merge" commit below.
	committed, err := gitRun(ctx, store, "show", firstBranch+":"+rel)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(e.BodyPath, []byte(committed), 0o600))

	// Stamp merged before "simulating" the merge landing on default, so the
	// fixture's default-branch content accurately models a real post-merge
	// state instead of just replaying the pre-merge pending content.
	e.ReviewStatus = ReviewStatusMerged
	require.NoError(t, e.WriteBackReviewStatus())
	gitCommitAll(t, store, "simulate merge of "+firstBranch)

	e.RecurrenceCount = 1
	require.NoError(t, e.WriteBackRecurrenceCount(ctx, store))

	branch, err := e.CommitRecurrenceToReviewBranch(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, firstBranch, branch, "a recurrence commit must reuse the entry's existing review branch")

	committedAgain, err := gitRun(ctx, store, "show", branch+":"+rel)
	require.NoError(t, err)
	assert.Contains(t, committedAgain, `review_status = "pending"`,
		"a recurrence on an already-merged entry must re-flip the review-branch copy back to pending")

	restored, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusMerged, restored.ReviewStatus,
		"the store's own working-tree copy, restored to its last-committed HEAD content, must still show merged -- only the review-branch copy is pending until this recurrence itself is reviewed")
}

// TestMergeReviewBranchStampsReviewStatusMerged covers exit-contract bullet
// 4: ReviewMergeBranch (via patchFrontmatterFields) flips review_status to
// merged when a remember/<id> branch lands.
func TestMergeReviewBranchStampsReviewStatusMerged(t *testing.T) {
	store := reviewStore(t)
	branch, e := fixtureBranch(t, store, "draft-topic", []string{"rig:web"}, "a note about the rig")

	_, err := MergeReviewBranch(t.Context(), store, branch, ReviewMergeOptions{
		TopicKey: "curated-topic",
		Bead:     "crn-123",
	})
	require.NoError(t, err)

	got, err := Find(t.Context(), store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusMerged, got.ReviewStatus)
}

// TestReindexDerivesReviewStatusFromFrontmatterNotIndexOnlyState proves
// review_status belongs in the body-sourced column category (alongside
// overridden_duplicate_of, not the index-only category): a full
// from-scratch Reindex over a store whose files already carry review_status
// in frontmatter must recover it purely from disk content, with no
// dependency on any prior index state.
func TestReindexDerivesReviewStatusFromFrontmatterNotIndexOnlyState(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"merged\"\nscope = []\n+++\nbody\n")
	writeFile(t, dir, "global/b.md",
		"+++\nid = \"b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")

	_, err := Reindex(t.Context(), dir)
	require.NoError(t, err)

	all, err := Status(t.Context(), dir)
	require.NoError(t, err)
	byID := make(map[string]*Entry, len(all))
	for _, e := range all {
		byID[e.ID] = e
	}
	require.Contains(t, byID, "a")
	require.Contains(t, byID, "b")
	assert.Equal(t, ReviewStatusMerged, byID["a"].ReviewStatus)
	assert.Equal(t, ReviewStatusPending, byID["b"].ReviewStatus)

	// A second reindex-from-scratch (simulating a delete-and-rebuild of the
	// gitignored index) must recover the exact same values purely from the
	// files on disk -- proving no reliance on whatever the first reindex's
	// upsert happened to leave in index-only state.
	require.NoError(t, os.Remove(IndexPath(dir)))
	_, err = Reindex(t.Context(), dir)
	require.NoError(t, err)

	all2, err := Status(t.Context(), dir)
	require.NoError(t, err)
	byID2 := make(map[string]*Entry, len(all2))
	for _, e := range all2 {
		byID2[e.ID] = e
	}
	assert.Equal(t, ReviewStatusMerged, byID2["a"].ReviewStatus)
	assert.Equal(t, ReviewStatusPending, byID2["b"].ReviewStatus)
}

// TestListByTopicSurfacesReviewStatus covers exit-contract bullet 6 for
// list: ListingEntry carries review_status through from the entry's
// frontmatter (ListByTopic re-parses each match from disk).
func TestListByTopicSurfacesReviewStatus(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/p1.md",
		"+++\nid = \"p1\"\ntitle = \"Pending\"\ntopic_key = \"solo-topic\"\nreview_status = \"pending\"\nscope = [\"rig:alpha\"]\n+++\nbody\n")

	rows, err := ListByTopic(t.Context(), dir, "solo-topic", []string{"rig:alpha"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, ReviewStatusPending, rows[0].ReviewStatus)
}

// TestPrimeSurfacesReviewStatus covers exit-contract bullet 6 for prime:
// PrimeItem carries review_status through from Status()'s bulk index read
// (Prime never re-reads disk per entry, unlike ListByTopic).
func TestPrimeSurfacesReviewStatus(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, ReviewStatusPending, result.Items[0].ReviewStatus)
}

// TestRenderPrimeTextShowsPendingReviewMarker covers exit-contract bullet 5
// for prime: a pending entry's rendered line carries a [PENDING REVIEW]
// marker inline; a merged one does not. Inline (not a standalone line) is
// deliberate -- prime renders multiple entries per call, and a standalone
// marker line would be ambiguous about which entry it belongs to.
func TestRenderPrimeTextShowsPendingReviewMarker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"Pending Entry\"\ntopic_key = \"topic-a\"\nreview_status = \"pending\"\nscope = []\n+++\nbody\n")
	writeFile(t, dir, "global/b.md",
		"+++\nid = \"b\"\ntitle = \"Merged Entry\"\ntopic_key = \"topic-b\"\nreview_status = \"merged\"\nscope = []\n+++\nbody\n")

	result, err := Prime(t.Context(), dir, nil, testBudget)
	require.NoError(t, err)
	out := RenderPrimeText(result)

	var pendingLine, mergedLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Pending Entry") {
			pendingLine = l
		}
		if strings.Contains(l, "Merged Entry") {
			mergedLine = l
		}
	}
	require.NotEmpty(t, pendingLine, "expected a rendered line for the pending entry")
	require.NotEmpty(t, mergedLine, "expected a rendered line for the merged entry")
	assert.Contains(t, pendingLine, "[PENDING REVIEW]")
	assert.NotContains(t, mergedLine, "[PENDING REVIEW]")
}

// TestWriteBackReviewStatusRoundTrip mirrors the dedicated round-trip unit
// test each WriteBack* method has (e.g. TestWriteBackPromotedBeadIDRoundTrip
// in entry_test.go).
func TestWriteBackReviewStatusRoundTrip(t *testing.T) {
	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "build-flags", Scope: []string{"rig:web"}, Body: "body", CreatedBy: "agent:bot"})
	require.NoError(t, err)
	store := t.TempDir()
	require.NoError(t, e.Create(store))

	e.ReviewStatus = ReviewStatusMerged
	require.NoError(t, e.WriteBackReviewStatus())

	got, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusMerged, got.ReviewStatus)
}
