package cairn

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// reviewBranchPrefix is the git ref namespace every review branch
// reviewBranchName (remember.go) creates falls under -- the prefix this
// file lists against for the librarian's stale-branch recovery step
// (crn-xw3 UC4, AF3).
const reviewBranchPrefix = "remember/"

// ReviewBranch is one remember/* branch not yet merged into the store's
// checked-out branch, together with the tier (and, for rig/role, the
// tier's value) its changed file belongs to and its age relative to the
// caller-supplied now.
//
// Error is set, and Path/Tier/Value left zero, when this branch's own
// merge status or changed-file tier could not be resolved -- one malformed
// or unusual branch must not blank out visibility into every other one, the
// same reporting-not-erroring stance Sweep takes for a single bad entry.
type ReviewBranch struct {
	Name    string
	EntryID string
	Path    string
	Tier    string
	Value   string
	Age     time.Duration
	// SHA is the branch's current tip commit hash -- evaluateBranch
	// (cmd/branches.go) keys its cross-pass notify state on Name+SHA, so a
	// new commit (a different SHA) is indistinguishable from a never-before-
	// seen branch, per crn-3l6.
	SHA   string
	Error string
}

// ListReviewBranches lists every remember/* branch in store not already
// merged into checkedOut (the store's currently checked-out branch --
// CommitDirect's own "whatever branch is already checked out"), together
// with each one's changed-file tier and age relative to now. Branches are
// discovered via git for-each-ref against reviewBranchName's own prefix,
// not a list any caller maintains separately, so the two can never silently
// desync.
//
// Tier is derived the same way entryTier (sweep.go) derives it for an
// already-committed entry's body path -- from the file the branch actually
// changed, never the branch name or any parsed identifier -- so an entry
// whose topic_key happens to look like a tier tag can never misroute a
// notification (see TestListReviewBranchesTierFromPathNotBranchName).
func ListReviewBranches(ctx context.Context, store string, now time.Time) ([]ReviewBranch, error) {
	checkedOut, err := gitRun(ctx, store, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve store's checked-out branch: %w", err)
	}
	checkedOut = strings.TrimSpace(checkedOut)

	refs, err := gitRun(ctx, store, "for-each-ref",
		"--format=%(refname:short)%00%(committerdate:unix)%00%(objectname)",
		"refs/heads/"+reviewBranchPrefix)
	if err != nil {
		return nil, fmt.Errorf("list review branches: %w", err)
	}

	var branches []ReviewBranch
	for _, line := range strings.Split(strings.TrimRight(refs, "\n"), "\n") {
		if line == "" {
			continue
		}
		name, commitAt, sha, err := parseRefLine(line)
		if err != nil {
			return nil, fmt.Errorf("list review branches: %w", err)
		}

		rb := ReviewBranch{
			Name:    name,
			EntryID: strings.TrimPrefix(name, reviewBranchPrefix),
			Age:     now.Sub(commitAt),
			SHA:     sha,
		}

		merged, err := isMergedInto(ctx, store, name, checkedOut)
		if err != nil {
			rb.Error = fmt.Sprintf("check merge status against %s: %v", checkedOut, err)
			branches = append(branches, rb)
			continue
		}
		if merged {
			continue // already merged by a reviewer -- not awaiting review
		}

		path, err := changedPath(ctx, store, checkedOut, name)
		if err != nil {
			rb.Error = fmt.Sprintf("resolve changed file: %v", err)
			branches = append(branches, rb)
			continue
		}
		rb.Path = path
		rb.Tier, rb.Value = tierFromPath(path)
		branches = append(branches, rb)
	}
	return branches, nil
}

// parseRefLine splits one NUL-joined "refname\0committerdate\0objectname"
// line from ListReviewBranches' for-each-ref call.
func parseRefLine(line string) (name string, commitAt time.Time, sha string, err error) {
	fields := strings.SplitN(line, "\x00", 3)
	if len(fields) != 3 {
		return "", time.Time{}, "", fmt.Errorf("unexpected for-each-ref output %q", line)
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return "", time.Time{}, "", fmt.Errorf("parse committer date for %s: %w", fields[0], err)
	}
	return fields[0], time.Unix(sec, 0), strings.TrimSpace(fields[2]), nil
}

// isMergedInto reports whether branch's tip commit is already an ancestor
// of target -- i.e. a reviewer has merged it -- using git's own ancestry
// check rather than comparing SHAs or tree contents, so a merge via rebase,
// squash, or an ordinary merge commit are all recognized alike as long as
// target actually contains branch's commit.
func isMergedInto(ctx context.Context, repo, branch, target string) (bool, error) {
	err := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", branch, target).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil // a clean "not an ancestor" result, not a failure
	}
	return false, err
}

// changedPath returns the single entry file a review branch touched,
// relative to store -- the file whose location determines the branch's
// tier (tierFromPath), the same way entryTier (sweep.go) derives tier from
// an already-committed entry's body path. It diffs against target's
// merge-base with branch, not target's current tip, so a branch still
// resolves correctly even after further commits have landed on target since
// the branch was created.
func changedPath(ctx context.Context, store, target, branch string) (string, error) {
	base, err := gitRun(ctx, store, "merge-base", target, branch)
	if err != nil {
		return "", fmt.Errorf("find merge-base with %s: %w", target, err)
	}
	out, err := gitRun(ctx, store, "diff", "--name-only", strings.TrimSpace(base), branch)
	if err != nil {
		return "", fmt.Errorf("diff against merge-base: %w", err)
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("branch %s has no changes relative to %s", branch, target)
	}
	return paths[0], nil
}

// tierFromPath derives the DESIGN.md §2 tier (and, for rig/role, the
// tier's value) that a review branch's changed file belongs to, mirroring
// entryTier's file-location-not-parsed-identifier rule (sweep.go) so both
// steps resolve tier the same way. rel is store-relative, e.g.
// "rig/web/foo-1a2b3c4d.md".
func tierFromPath(rel string) (tier, value string) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	tier = parts[0]
	if (tier == "rig" || tier == "role") && len(parts) >= 3 {
		value = parts[1]
	}
	return tier, value
}
