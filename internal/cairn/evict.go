package cairn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EntryForEvict looks up id for eviction, bypassing Find's hit_count/
// last_recalled_at side effect: stamping recall telemetry while deciding
// whether to evict an entry would corrupt the very disuse signal
// CullCandidates measures. Mirrors cmd/remember.go's recurrenceMatch, which
// bypasses Find for the identical reason.
func EntryForEvict(ctx context.Context, store, id string) (*Entry, error) {
	if err := ensureFresh(ctx, store); err != nil {
		return nil, err
	}
	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	bodyPath, err := findBodyPath(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return ParseEntry(bodyPath)
}

// EvictDirect deletes e's body file and commits that deletion straight to
// the store repo's current branch -- the private agent/ tier's eviction path
// (mirrors CommitDirect's add-side commit, applied to a delete). Unlike
// CommitDirect, which only documents (but does not enforce in code) its
// private-tier precondition, EvictDirect enforces IsPrivateScope itself:
// NFR-07 ("shared-tier entries are NEVER evicted directly") is a hard
// invariant, not a default a caller can bypass.
func (e *Entry) EvictDirect(ctx context.Context, store string) (string, error) {
	if !IsPrivateScope(e.Scope) {
		tier, _ := ResolvedTier(e.Scope)
		return "", fmt.Errorf("refusing direct eviction of %s: resolved tier %q is not private (agent) -- "+
			"shared-tier entries can only be evicted via a review-branch proposal (NFR-07)", e.ID, tier)
	}
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s relative to store %s: %w", e.BodyPath, store, err)
	}
	if _, err := gitRun(ctx, store, "rm", "--", rel); err != nil {
		return "", fmt.Errorf("git rm %s (entry not evicted -- retry): %w", rel, err)
	}
	if _, err := gitRun(ctx, store, "commit", "-m", "cull: evict "+e.ID, "--", rel); err != nil {
		return "", fmt.Errorf("git commit eviction of %s (removed from working tree and staged but not "+
			"committed -- retry or restore with `git checkout HEAD -- %s`): %w", rel, rel, err)
	}
	sha, err := gitRun(ctx, store, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("eviction commit succeeded but could not resolve the resulting SHA: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// cullBranchName is the git branch a shared-tier eviction proposal lands on
// -- deliberately namespaced separately from reviewBranchName's remember/
// prefix, so a pending cull proposal never collides with a pending add or
// recurrence review for the same entry ID, and stays invisible to
// ListReviewMergeBranches/ListReviewBranches (both scoped to remember/),
// which cannot handle a pure-deletion branch.
func cullBranchName(e *Entry) string {
	return "cull/" + e.ID
}

// EvictToReviewBranch proposes e's eviction on its own cull/<id> branch --
// the role:/rig:/global: tiers' ONLY eviction path (NFR-07): a reviewer
// merging this branch with plain git is the actual eviction. Mirrors
// CommitToReviewBranch's isolation approach, applied to a delete.
//
// Unlike CommitRecurrenceToReviewBranch's deliberate reuse of an
// already-existing branch (an ordinary, expected case for a recurring
// topic), a second concurrent cull proposal for an entry already pending
// review is refused rather than silently folded in or forked past: a
// pending eviction proposal should be resolved (merged or rejected) before
// another is opened.
func (e *Entry) EvictToReviewBranch(ctx context.Context, store string) (string, error) {
	branch := cullBranchName(e)
	exists, err := reviewBranchExists(ctx, store, branch)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("a cull proposal is already pending for %s on branch %s", e.ID, branch)
	}
	msg := fmt.Sprintf("cull: evict %s\n\nscope: %s", e.ID, strings.Join(e.Scope, " "))
	if err := e.commitDeleteToReviewWorktree(ctx, store, branch, msg); err != nil {
		return "", err
	}
	return branch, nil
}

// commitDeleteToReviewWorktree is EvictToReviewBranch's worktree-isolation
// mechanics -- the same "throwaway git worktree, so an interrupted sequence
// can never corrupt the store's own working tree" shape as
// commitToReviewWorktree (see its doc comment), applied to a deletion: `git
// rm` in the scratch worktree instead of writing + adding e's current body
// content.
func (e *Entry) commitDeleteToReviewWorktree(ctx context.Context, store, branch, msg string) error {
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return fmt.Errorf("resolve entry path: %w", err)
	}

	scratch, err := os.MkdirTemp("", "cairn-cull-*")
	if err != nil {
		return fmt.Errorf("create cull worktree scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	wt := filepath.Join(scratch, "wt")
	if _, err := gitRun(ctx, store, "worktree", "add", "-b", branch, wt, "HEAD"); err != nil {
		return fmt.Errorf("create cull branch %q: %w", branch, err)
	}
	defer func() { _, _ = gitRun(ctx, store, "worktree", "remove", "--force", wt) }()

	if _, err := gitRun(ctx, wt, "rm", "--", rel); err != nil {
		return fmt.Errorf("stage eviction in cull worktree: %w", err)
	}
	if _, err := gitRun(ctx, wt, "commit", "-q", "-m", msg); err != nil {
		return fmt.Errorf("commit eviction to cull branch: %w", err)
	}
	return nil
}
