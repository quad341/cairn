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

	"github.com/quad341/cairn/internal/obslog"
)

const (
	// MetadataSourceAuthored marks a retrieval field supplied explicitly by
	// the caller rather than synthesized from the entry body.
	MetadataSourceAuthored = "authored"
	// MetadataSourceDerived marks a retrieval field synthesized from the body.
	MetadataSourceDerived = "derived"

	// EntryTypeKnowledge is independently true knowledge about a system.
	EntryTypeKnowledge = "knowledge"
	// EntryTypeRemediation is conditional, independently testable recovery knowledge.
	EntryTypeRemediation = "remediation"
	// EntryTypePolicy is a directive that belongs in an agent prompt, not cairn.
	EntryTypePolicy = "policy"
)

// ValidateNewEntryType enforces cairn's write-time content boundary. Policy is
// named in the vocabulary so callers can classify honestly, then refused.
func ValidateNewEntryType(entryType string) error {
	switch entryType {
	case EntryTypeKnowledge, EntryTypeRemediation:
		return nil
	case EntryTypePolicy:
		return errors.New("policy is not knowledge and cannot be stored in cairn; " +
			"put policy in the agent's prompt so it is always enforced rather than depending on retrieval")
	case "":
		return errors.New("entry type is required: classify it as knowledge, remediation, or policy")
	default:
		return fmt.Errorf("invalid entry type %q: must be knowledge, remediation, or policy", entryType)
	}
}

// NewEntryParams holds NewEntry's inputs. An options struct rather than a
// growing positional-arg list: the original 4 positional args (topicKey,
// scope, body, createdBy) would otherwise grow to 7 for crn-lzn4.1.1's
// FR-3/FR-4 (Title, Summary, Anchor), which is where positional args stop
// being readable at the call site (DESIGN.md §11 Trade-offs).
//
// Type is required and validated by NewEntry itself. Title and Summary
// independently default to titleAndSummary(Body)'s existing auto-derivation
// when left unset (the zero value, ""); Anchor
// defaults to Anchor{Type: "none"} when left at its zero value (Type == "").
// A caller that wants only one of Title/Summary auto-derived may leave just
// that field unset.
type NewEntryParams struct {
	Type      string
	TopicKey  string
	Scope     []string
	Body      string
	CreatedBy string
	Title     string
	Summary   string
	Anchor    Anchor
}

// NewEntry constructs a new entry for `cairn remember`: a contributor's
// freeform write, not yet curator-normalized (DESIGN.md §6). ID combines
// TopicKey with a random suffix -- never just TopicKey, since several
// entries may deliberately share one topic_key (that's the whole point:
// shadow() picks the most specific at read time, DESIGN.md §3).
func NewEntry(p NewEntryParams) (*Entry, error) {
	if err := ValidateNewEntryType(p.Type); err != nil {
		return nil, err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}

	// An explicit p.Title/p.Summary was already validated against the cap by
	// the caller (cmd/remember.go's ValidateTitleLength/ValidateSummaryLength)
	// and must be rejected there, not silently altered here -- so only the
	// auto-derived branch truncates. Auto-derivation can otherwise return the
	// entry's entire body (titleAndSummary), which has no cap of its own
	// (crn-3476 FR-3, NFR-3).
	title, summary := p.Title, p.Summary
	titleSource, summarySource := MetadataSourceAuthored, MetadataSourceAuthored
	if title == "" || summary == "" {
		autoTitle, autoSummary := titleAndSummary(p.Body)
		if title == "" {
			title = truncateRunes(autoTitle, titleCap)
			titleSource = MetadataSourceDerived
		}
		if summary == "" {
			summary = truncateRunes(autoSummary, summaryCap)
			summarySource = MetadataSourceDerived
		}
	}

	anchor := p.Anchor
	if anchor.Type == "" {
		anchor = Anchor{Type: "none"}
	}

	return &Entry{
		ID:            flattenTopicKey(p.TopicKey) + "-" + suffix,
		Title:         title,
		TitleSource:   titleSource,
		Summary:       summary,
		SummarySource: summarySource,
		Type:          p.Type,
		TopicKey:      p.TopicKey,
		Scope:         p.Scope,
		Anchor:        anchor,
		CreatedBy:     p.CreatedBy,
		CreatedAt:     time.Now().Format(time.RFC3339),
		Body:          p.Body,
	}, nil
}

// flattenTopicKey derives a filesystem-safe path component from a topic_key
// that may contain ValidateTopicKey-permitted slashes: Entry.ID is a single
// path segment (used directly as "<ID>.md" under the scope-tier directory,
// see Create), never a nested path, so every '/' is replaced with '-' rather
// than preserved as a directory separator. Only ID construction uses this --
// Entry.TopicKey itself always keeps the raw, slash-intact value.
func flattenTopicKey(topicKey string) string {
	return strings.ReplaceAll(topicKey, "/", "-")
}

func titleAndSummary(body string) (title, summary string) {
	trimmed := strings.TrimSpace(body)
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		return strings.TrimSpace(trimmed[:i]), trimmed
	}
	return trimmed, trimmed
}

// DerivedTitleSummary returns the metadata cairn would synthesize for body.
// It is exported for migration tooling that must identify legacy synthesized
// metadata using the exact write-path algorithm rather than a lookalike.
func DerivedTitleSummary(body string) (title, summary string) {
	title, summary = titleAndSummary(body)
	return truncateRunes(title, titleCap), truncateRunes(summary, summaryCap)
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
		e.ID = flattenTopicKey(e.TopicKey) + "-" + suffix
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

// gitStep runs fn -- typically a gitRun call -- logging a write_path_step
// record correlated back to its parent write_path record via operation, and
// timed around the call. fn's own (output, error) result is returned
// unchanged: gitStep only observes, it never alters control flow.
func gitStep(ctx context.Context, operation, name string, fn func() (string, error)) (string, error) {
	start := time.Now()
	out, err := fn()
	fields := obslog.WritePathStepFields{
		Operation:  operation,
		Name:       name,
		Outcome:    "ok",
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		fields.Outcome = "error"
		fields.Detail = redactSecrets(err.Error())
	}
	obslog.FromContext(ctx).WritePathStep(fields)
	return out, err
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
	tier, _ := ResolvedTier(e.Scope)
	obslog.FromContext(ctx).WritePath(obslog.WritePathFields{
		Operation: "commit_direct",
		Scope:     e.Scope,
		Tier:      tier,
		Private:   IsPrivateScope(e.Scope),
	})

	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s relative to store %s: %w", e.BodyPath, store, err)
	}
	if _, err := gitStep(ctx, "commit_direct", "git_add", func() (string, error) {
		return gitRun(ctx, store, "add", "--", rel)
	}); err != nil {
		return "", fmt.Errorf("git add %s (entry written but not committed -- remove or retry): %w", rel, err)
	}
	if _, err := gitStep(ctx, "commit_direct", "git_commit", func() (string, error) {
		return gitRun(ctx, store, "commit", "-m", "remember: "+e.ID, "--", rel)
	}); err != nil {
		return "", fmt.Errorf("git commit %s (entry written and staged but not committed -- remove or retry): %w", rel, err)
	}
	sha, err := gitStep(ctx, "commit_direct", "git_rev_parse_head", func() (string, error) {
		return gitRun(ctx, store, "rev-parse", "HEAD")
	})
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
	if err := e.commitToReviewWorktree(ctx, store, branch, true, msg, "commit_to_review_branch"); err != nil {
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
// both callers. operation is the caller's own write_path operation name
// (e.g. "commit_to_review_branch"), used only to correlate this call's
// write_path/write_path_step log records back to its caller.
func (e *Entry) commitToReviewWorktree(ctx context.Context, store, branch string, create bool, msg, operation string) error {
	tier, _ := ResolvedTier(e.Scope)
	obslog.FromContext(ctx).WritePath(obslog.WritePathFields{
		Operation: operation,
		Scope:     e.Scope,
		Tier:      tier,
		Private:   IsPrivateScope(e.Scope),
	})

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
		if _, err := gitStep(ctx, operation, "git_worktree_add", func() (string, error) {
			return gitRun(ctx, store, "worktree", "add", "-b", branch, wt, "HEAD")
		}); err != nil {
			return fmt.Errorf("create review branch %q: %w", branch, err)
		}
	} else {
		if _, err := gitStep(ctx, operation, "git_worktree_add", func() (string, error) {
			return gitRun(ctx, store, "worktree", "add", wt, branch)
		}); err != nil {
			return fmt.Errorf("open existing review branch %q: %w", branch, err)
		}
	}
	defer func() {
		_, _ = gitStep(ctx, operation, "git_worktree_remove", func() (string, error) {
			return gitRun(ctx, store, "worktree", "remove", "--force", wt)
		})
	}()

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

	if _, err := gitStep(ctx, operation, "git_add", func() (string, error) {
		return gitRun(ctx, wt, "add", "--", rel)
	}); err != nil {
		return fmt.Errorf("stage entry in review worktree: %w", err)
	}
	if _, err := gitStep(ctx, operation, "git_commit", func() (string, error) {
		return gitRun(ctx, wt, "commit", "-q", "-m", msg)
	}); err != nil {
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
	if err := e.commitToReviewWorktree(ctx, store, branch, !exists, msg, "commit_recurrence_to_review_branch"); err != nil {
		return "", err
	}
	return branch, nil
}

// CommitPromotionToReviewBranch commits e -- an existing shared-tier entry
// whose PromotedBeadID cmd/promote.go's promote-mark command just set in
// place -- onto its own remember/<id> review branch, the same namespace
// CommitToReviewBranch and CommitRecurrenceToReviewBranch use. Like
// CommitRecurrenceToReviewBranch (and for the identical reason: a
// promotion-worthy entry's original review is often still pending exactly
// when the librarian marks it promoted), this reuses an already-existing
// branch -- appending a second commit to the same pending review -- falling
// back to creating it fresh only when no such branch exists yet.
func (e *Entry) CommitPromotionToReviewBranch(ctx context.Context, store string) (string, error) {
	branch := reviewBranchName(e)
	exists, err := reviewBranchExists(ctx, store, branch)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("remember: promote %s (bead %s)", e.ID, e.PromotedBeadID)
	if err := e.commitToReviewWorktree(ctx, store, branch, !exists, msg, "commit_promotion_to_review_branch"); err != nil {
		return "", err
	}
	return branch, nil
}
