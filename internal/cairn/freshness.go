package cairn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Freshness statuses.
const (
	Fresh   = "fresh"
	Stale   = "stale"
	Unknown = "unknown"
)

func git(ctx context.Context, repo string, args ...string) (string, bool) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// objectHash is the git object id of a path at HEAD (blob for files, tree for dirs).
func objectHash(ctx context.Context, repo, path string) string {
	if h, ok := git(ctx, repo, "rev-parse", "HEAD:"+path); ok {
		return h
	}
	return "?"
}

// expand resolves globs to tracked paths via git ls-files; literals pass through.
func expand(ctx context.Context, repo string, paths []string) []string {
	set := map[string]struct{}{}
	for _, p := range paths {
		if out, ok := git(ctx, repo, "ls-files", "--", p); ok && strings.TrimSpace(out) != "" {
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
	return files
}

// ComputeFingerprint returns a deterministic fingerprint of the anchored
// source, or "" if it cannot be computed: none/query/external in v1, or a
// files anchor with a path that doesn't resolve to a real tracked object at
// repo's HEAD (objectHash's "?" sentinel -- the same one untrackedPaths in
// sweep.go checks for).
func ComputeFingerprint(ctx context.Context, a Anchor) string {
	switch a.Type {
	case "commit":
		return a.Spec
	case "files":
		if a.Repo == "" || len(a.Paths) == 0 {
			return ""
		}
		parts := make([]string, 0, len(a.Paths))
		for _, p := range expand(ctx, a.Repo, a.Paths) {
			h := objectHash(ctx, a.Repo, p)
			if h == "?" {
				return ""
			}
			parts = append(parts, p+":"+h)
		}
		sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
		return hex.EncodeToString(sum[:])[:16]
	default:
		return ""
	}
}

// Check returns (status, human-readable detail) for an entry's freshness.
func Check(ctx context.Context, e *Entry) (string, string) {
	a := e.Anchor
	if a.Type == "" || a.Type == "none" {
		return Unknown, "no source anchor (time-based freshness only)"
	}
	cur := ComputeFingerprint(ctx, a)
	if cur == "" {
		return Unknown, fmt.Sprintf("anchor type %q not verifiable in v1", a.Type)
	}
	if a.Fingerprint == "" {
		return Unknown, "never verified (no stored fingerprint)"
	}
	if cur == a.Fingerprint {
		return Fresh, fmt.Sprintf("anchor matches (%s)", cur)
	}
	return Stale, fmt.Sprintf("source drifted: stored %s != current %s", a.Fingerprint, cur)
}
