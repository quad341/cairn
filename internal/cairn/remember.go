package cairn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NewEntry constructs a new entry for `cairn remember`: a contributor's
// freeform write, not yet curator-normalized (DESIGN.md §6). id combines
// topicKey with a random suffix -- never just topicKey, since several
// entries may deliberately share one topic_key (that's the whole point:
// shadow() picks the most specific at read time, DESIGN.md §3). title is
// body's first line, a scannable heading for status output; summary is the
// full trimmed body, so the two are often identical for remember's typical
// one-liner input.
func NewEntry(topicKey string, scope []string, body, createdBy string) (*Entry, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	title, summary := titleAndSummary(body)
	return &Entry{
		ID:        topicKey + "-" + suffix,
		Title:     title,
		Summary:   summary,
		TopicKey:  topicKey,
		Scope:     scope,
		Anchor:    Anchor{Type: "none"},
		CreatedBy: createdBy,
		CreatedAt: time.Now().Format(time.DateOnly),
		Body:      body,
	}, nil
}

func titleAndSummary(body string) (title, summary string) {
	trimmed := strings.TrimSpace(body)
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		return strings.TrimSpace(trimmed[:i]), trimmed
	}
	return trimmed, trimmed
}

func randomSuffix() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// maxCreateAttempts bounds the ID-collision retry in Create.
const maxCreateAttempts = 5

// Create places a brand-new entry in the store: it derives the file's
// location from e.Scope (the DESIGN.md §2 tiers) and e.ID, creates the
// scope-tier directory if needed -- WriteBack does not -- and writes it.
// Unlike WriteBack, Create never overwrites an existing file: several
// entries may deliberately share one topic_key (see NewEntry), so a
// same-topic_key, same-scope suffix collision isn't a contrived scenario
// over a long-lived store. On collision it regenerates e.ID and retries,
// rather than silently destroying whatever entry is already at that path.
func (e *Entry) Create(store string) error {
	dir := scopeDir(store, e.Scope)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		e.BodyPath = filepath.Join(dir, e.ID+".md")
		content, err := e.marshal()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(e.BodyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(content)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			return werr
		}
		if !os.IsExist(err) || attempt >= maxCreateAttempts-1 {
			return err
		}
		suffix, err := randomSuffix()
		if err != nil {
			return err
		}
		e.ID = e.TopicKey + "-" + suffix
	}
}

// ResolvedTier reports which DESIGN.md §2 tier scope resolves to -- "rig",
// "role", "agent", or "global" when no tier tag matches -- and that tag's
// value (the part after the colon; empty for global). Shared tiers (every
// value but "agent") require DESIGN.md §7's branch-and-review path rather
// than a direct commit.
func ResolvedTier(scope []string) (tier, value string) {
	for _, t := range scopeDirs[1:] { // rig, role, agent -- global is the fallback
		for _, tag := range scope {
			if val, ok := strings.CutPrefix(tag, t+":"); ok {
				return t, val
			}
		}
	}
	return "global", ""
}

// scopeDir maps scope tags to their DESIGN.md §2 directory. An empty scope
// (or one with no rig:/role:/agent: tag) is filed under global/; otherwise
// the first matching tier in rig > role > agent order wins, using the tag's
// value (the part after the colon) as the subdirectory name.
func scopeDir(store string, scope []string) string {
	tier, value := ResolvedTier(scope)
	if tier == "global" {
		return filepath.Join(store, "global")
	}
	return filepath.Join(store, tier, value)
}

// IsPrivateScope reports whether scope resolves to the DESIGN.md §7 private
// (agent/) tier: commit straight to the store's current branch, no review.
// A scope that also carries a rig: or role: tag does not qualify -- those
// tiers take precedence over agent: in ResolvedTier, matching scopeDir
// exactly.
func IsPrivateScope(scope []string) bool {
	tier, _ := ResolvedTier(scope)
	return tier == "agent"
}

// gitRun runs git -C repo args..., returning combined stdout+stderr on
// success. On failure it returns an error embedding that output, so callers
// see git's own diagnostic (e.g. "nothing to commit", a merge conflict)
// instead of a bare "exit status 1". This is distinct from freshness.go's
// git() helper, which collapses failure to a bool -- CommitDirect and
// CommitToReviewBranch's callers need a clear, detailed error, not just a
// yes/no.
func gitRun(ctx context.Context, repo string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// CommitDirect stages and commits e's already-written body file straight to
// the store repo's current branch: the private agent/ tier's flow
// (DESIGN.md §7, "commit straight to main -- no review"). Callers must only
// invoke this after a successful e.Create, and only when e.Scope resolves to
// the private tier (IsPrivateScope) -- committing a shared-tier entry this
// way would bypass the review DESIGN.md §7 requires for that tier.
//
// The add and commit are both scoped to e.BodyPath alone (never `git add -A`
// or a bare `git commit`), so anything else already staged or dirty in the
// store's index is left untouched -- the resulting commit contains only the
// new entry file, regardless of what else a concurrent writer left in the
// index. No branch is created or switched to; this commits onto whatever
// branch is already checked out.
//
// On a git failure the entry file is left on disk exactly as e.Create wrote
// it -- uncommitted, not rolled back -- and the returned error says so
// explicitly, so that state is reported rather than silently lost.
func (e *Entry) CommitDirect(ctx context.Context, store string) (string, error) {
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s relative to store %s: %w", e.BodyPath, store, err)
	}
	if _, err := gitRun(ctx, store, "add", "--", rel); err != nil {
		return "", fmt.Errorf("git add %s (entry written but not committed -- remove or retry): %w", rel, err)
	}
	if _, err := gitRun(ctx, store, "commit", "-m", "remember: "+e.ID, "--", rel); err != nil {
		return "", fmt.Errorf("git commit %s (entry written and staged but not committed -- remove or retry): %w", rel, err)
	}
	sha, err := gitRun(ctx, store, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("commit succeeded but could not resolve the resulting SHA: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// reviewBranchName is the git branch a shared-tier entry's review commit
// lands on -- namespaced under remember/ and keyed by the entry's own ID, so
// concurrent remember calls (even for the same topic_key) never collide.
func reviewBranchName(e *Entry) string {
	return "remember/" + e.ID
}

// CommitToReviewBranch commits e -- already written to store by Create --
// onto a fresh branch, never the store's current (default) branch, per
// DESIGN.md §7's shared-tier curation model: "shared = branch, merge
// request, review, merge." It uses `git worktree add` for isolation rather
// than checkout-in-place: a checkout/add/commit/checkout-back sequence in
// the store's own working tree would leave a real corruption window if
// interrupted mid-sequence (killed process, panic), which a throwaway
// worktree -- entirely separate from the store's HEAD, index, and working
// tree -- cannot. Returns the branch name on success.
func (e *Entry) CommitToReviewBranch(ctx context.Context, store string) (string, error) {
	branch := reviewBranchName(e)
	msg := fmt.Sprintf("remember: %s\n\nscope: %s", e.ID, strings.Join(e.Scope, " "))
	if err := e.commitToReviewWorktree(ctx, store, branch, true, msg); err != nil {
		return "", err
	}
	return branch, nil
}

// commitToReviewWorktree is the worktree-isolation mechanics shared by
// CommitToReviewBranch and CommitRecurrenceToReviewBranch: create a
// throwaway worktree checked out to branch -- freshly created from the
// store's current HEAD when create is true, or an already-existing local
// branch reused as-is when false -- copy e's current on-disk body into it at
// the same relative path, stage, and commit with msg. See
// CommitToReviewBranch's doc comment for why a throwaway worktree is used
// instead of an in-place checkout; that reasoning applies identically to
// both callers.
func (e *Entry) commitToReviewWorktree(ctx context.Context, store, branch string, create bool, msg string) error {
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return fmt.Errorf("resolve entry path: %w", err)
	}

	scratch, err := os.MkdirTemp("", "cairn-review-*")
	if err != nil {
		return fmt.Errorf("create review worktree scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	wt := filepath.Join(scratch, "wt")
	if create {
		if _, err := gitRun(ctx, store, "worktree", "add", "-b", branch, wt, "HEAD"); err != nil {
			return fmt.Errorf("create review branch %q: %w", branch, err)
		}
	} else {
		if _, err := gitRun(ctx, store, "worktree", "add", wt, branch); err != nil {
			return fmt.Errorf("open existing review branch %q: %w", branch, err)
		}
	}
	defer func() { _, _ = gitRun(ctx, store, "worktree", "remove", "--force", wt) }()

	content, err := os.ReadFile(e.BodyPath)
	if err != nil {
		return fmt.Errorf("read entry for review commit: %w", err)
	}
	dst := filepath.Join(wt, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("prepare review worktree dir: %w", err)
	}
	// dst stays under wt, a throwaway worktree this function created above;
	// rel is the same relative path Create already used to write e.BodyPath,
	// built from topic/scope segments validated at the CLI boundary before
	// any Entry is ever constructed.
	//nolint:gosec // dst is confined to a temp worktree, not attacker-controlled
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		return fmt.Errorf("copy entry into review worktree: %w", err)
	}

	if _, err := gitRun(ctx, wt, "add", "--", rel); err != nil {
		return fmt.Errorf("stage entry in review worktree: %w", err)
	}
	if _, err := gitRun(ctx, wt, "commit", "-q", "-m", msg); err != nil {
		return fmt.Errorf("commit entry to review branch: %w", err)
	}
	return nil
}

// reviewBranchExists reports whether branch already exists as a local
// branch in store, distinguishing "the ref just doesn't exist" (git
// rev-parse's own --quiet exit-1 signal) from a real git failure (repo
// missing, git not runnable, etc.), which is returned as an error rather
// than folded into a false "doesn't exist".
func reviewBranchExists(ctx context.Context, store, branch string) (bool, error) {
	_, err := gitRun(ctx, store, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check for existing review branch %q: %w", branch, err)
}

// CommitRecurrenceToReviewBranch commits e -- an existing shared-tier entry
// whose RecurrenceCount cmd/remember.go's capture-time recurrence path
// (crn-28ge.1.4) just incremented in place -- onto its own remember/<id>
// review branch, the same namespace CommitToReviewBranch uses. Unlike
// CommitToReviewBranch, the branch may already exist: the entry's original
// review (from its first capture) is often still pending exactly when a
// recurrence fires again, since a recurring topic tends to recur before
// anyone has reviewed the first report, not only after. Rather than fail
// with a branch-already-exists error on that ordinary case, this reuses the
// existing branch -- appending a second commit to the same pending review --
// falling back to creating it fresh (identical to CommitToReviewBranch)
// only when no such branch exists yet.
func (e *Entry) CommitRecurrenceToReviewBranch(ctx context.Context, store string) (string, error) {
	branch := reviewBranchName(e)
	exists, err := reviewBranchExists(ctx, store, branch)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("remember: recurrence %s (count %d)\n\nscope: %s", e.ID, e.RecurrenceCount, strings.Join(e.Scope, " "))
	if err := e.commitToReviewWorktree(ctx, store, branch, !exists, msg); err != nil {
		return "", err
	}
	return branch, nil
}
