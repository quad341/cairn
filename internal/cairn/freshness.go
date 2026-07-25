package cairn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Freshness statuses.
const (
	Fresh      = "fresh"
	Stale      = "stale"
	Unknown    = "unknown"
	Incomplete = "incomplete"
)

// git runs a git subcommand against repo and returns (output, found, err).
// found is false only on a confirmed negative verdict from git itself (not a
// repo, or a repo with no commits yet -- both exit non-zero). err is non-nil
// only for a genuine invocation failure (context cancellation, git missing
// from PATH, resource exhaustion preventing fork/exec, etc.); callers must
// not treat that the same as a confirmed "not a git store" (crn-t250). ctx's
// own error is checked first so a canceled/deadline-exceeded context is
// always classified as an error, regardless of how the killed process's exit
// status happens to come back from exec.
func git(ctx context.Context, repo string, args ...string) (string, bool, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", false, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}

// objectHash is the git object id of a path at HEAD (blob for files, tree for
// dirs). ok is false only on a confirmed negative (path not resolvable at
// HEAD); err is non-nil only for a genuine git invocation failure -- a
// direct passthrough of git()'s own (result, ok, err) classification.
func objectHash(ctx context.Context, repo, path string) (hash string, ok bool, err error) {
	return git(ctx, repo, "rev-parse", "HEAD:"+path)
}

// expand resolves globs to tracked paths via git ls-files; literals pass
// through. Returns a non-nil error only for a genuine invocation failure,
// short-circuiting on the first one -- a broken PATH fails identically for
// every remaining path, so there is no value in collecting N duplicate
// errors.
func expand(ctx context.Context, repo string, paths []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, p := range paths {
		out, ok, err := git(ctx, repo, "ls-files", "--", p)
		if err != nil {
			return nil, err
		}
		if ok && strings.TrimSpace(out) != "" {
			for _, ln := range strings.Split(out, "\n") {
				if strings.TrimSpace(ln) != "" {
					set[ln] = struct{}{}
				}
			}
		} else {
			set[p] = struct{}{}
		}
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// ComputeFingerprint returns a deterministic fingerprint of the anchored
// source. A confirmed negative -- none/query/external in v1, or a files
// anchor with a path that doesn't resolve to a real tracked object at
// repo's HEAD -- returns ("", nil), unchanged from before crn-fdjc.1. A
// genuine git invocation failure returns ("", err); callers must not fold
// this into the same empty string as a confirmed negative.
func ComputeFingerprint(ctx context.Context, a Anchor) (string, error) {
	switch a.Type {
	case "commit":
		return a.Spec, nil
	case "files":
		if a.Repo == "" || len(a.Paths) == 0 {
			return "", nil
		}
		expanded, err := expand(ctx, a.Repo, a.Paths)
		if err != nil {
			return "", err
		}
		parts := make([]string, 0, len(expanded))
		for _, p := range expanded {
			h, ok, err := objectHash(ctx, a.Repo, p)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", nil
			}
			parts = append(parts, p+":"+h)
		}
		sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
		return hex.EncodeToString(sum[:])[:16], nil
	default:
		return "", nil
	}
}

// Check returns (status, human-readable detail) for an entry's freshness.
func Check(ctx context.Context, e *Entry) (string, string) {
	a := e.Anchor
	if a.Type == "" || a.Type == "none" {
		return Unknown, "no source anchor (time-based freshness only)"
	}
	// Anchor shape (type + presence of the fields the type needs) decides
	// verifiability without any git call -- ComputeFingerprint's own switch
	// makes exactly this same free decision internally. Checking it here
	// first, before the never-verified check below, keeps "not verifiable"
	// winning over "never verified" for a fundamentally unverifiable anchor
	// (crn-0vqk.2).
	if a.Type != "commit" && (a.Type != "files" || a.Repo == "" || len(a.Paths) == 0) {
		return Unknown, fmt.Sprintf("anchor type %q not verifiable in v1", a.Type)
	}
	// Once the anchor is at least shape-checkable, "never verified" is also
	// knowable for free from a.Fingerprint alone -- check it before paying
	// for ComputeFingerprint's git shell-out (the only expensive branch,
	// reached only for a files anchor with repo+paths set). This benefits
	// every Check() caller (status/freshness/get/verify), not just Prime's
	// budgeted classifier, and is a currently-live inefficiency fixed in
	// scope for crn-0vqk.2 rather than filed separately.
	if a.Fingerprint == "" {
		return Unknown, "never verified (no stored fingerprint)"
	}
	cur, err := ComputeFingerprint(ctx, a)
	if err != nil {
		return Incomplete, fmt.Sprintf("git check did not complete: %v", err)
	}
	if cur == "" {
		return Unknown, fmt.Sprintf("anchor type %q not verifiable in v1", a.Type)
	}
	if cur == a.Fingerprint {
		return Fresh, fmt.Sprintf("anchor matches (%s)", cur)
	}
	return Stale, fmt.Sprintf("source drifted: stored %s != current %s", a.Fingerprint, cur)
}
