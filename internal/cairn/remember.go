package cairn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// scopeDir maps scope tags to their DESIGN.md §2 directory. An empty scope
// (or one with no rig:/role:/agent: tag) is filed under global/; otherwise
// the first matching tier in rig > role > agent order wins, using the tag's
// value (the part after the colon) as the subdirectory name.
func scopeDir(store string, scope []string) string {
	tier, val := tierOf(scope)
	if tier == "global" {
		return filepath.Join(store, "global")
	}
	return filepath.Join(store, tier, val)
}

// tierOf reports the DESIGN.md §2 tier scope resolves to -- the first
// matching tag in rig > role > agent order, or "global" (with an empty val)
// if scope carries none of those. scopeDir and IsPrivateScope both derive
// from this, so the two can never disagree about which tier a scope
// resolves to.
func tierOf(scope []string) (tier, val string) {
	for _, t := range scopeDirs[1:] { // rig, role, agent -- global is the fallback
		for _, tag := range scope {
			if v, ok := strings.CutPrefix(tag, t+":"); ok {
				return t, v
			}
		}
	}
	return "global", ""
}

// IsPrivateScope reports whether scope resolves to the DESIGN.md §7 private
// (agent/) tier: commit straight to the store's current branch, no review.
// A scope that also carries a rig: or role: tag does not qualify -- those
// tiers take precedence over agent: in tierOf, matching scopeDir exactly.
func IsPrivateScope(scope []string) bool {
	tier, _ := tierOf(scope)
	return tier == "agent"
}

// gitRun runs git -C repo args..., returning combined stdout+stderr on
// success. On failure it returns an error embedding that output, so callers
// see git's own diagnostic (e.g. "nothing to commit", a merge conflict)
// instead of a bare "exit status 1". This is distinct from freshness.go's
// git() helper, which collapses failure to a bool -- CommitDirect's callers
// need a clear, detailed error (DESIGN.md-adjacent crn-419.3 AC4), not just
// a yes/no.
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
