package cairn

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SweepFinding is one shared-tier entry's freshness verdict from a librarian
// sweep.
type SweepFinding struct {
	ID         string `json:"id"`
	Tier       string `json:"tier"` // global | rig | role (agent/ is never swept)
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	AnchorType string `json:"anchor_type"`
}

// Sweep computes freshness for every shared-tier entry (global/, rig/*/,
// role/*/ — agent/ private entries are out of the librarian's remit). It is
// strictly read-only: unlike the verify command, it never calls WriteBack,
// so running it on any cadence can never itself be the thing that stamps a
// fingerprint, and it can safely re-observe the same drifted entry on every
// sweep cycle without erasing the drift signal.
//
// For files-type anchors, Sweep independently confirms every configured
// path resolves to a tracked object at the anchor repo's HEAD before
// trusting Check's verdict. Check (via ComputeFingerprint's git ls-files
// fallback) currently derives a stable-but-meaningless fingerprint for an
// untracked path instead of failing (crn-6az.8.2, open) — once that bogus
// value is stamped by a verify call, Check reports Fresh forever after,
// which is worse than the honest Unknown this sweep exists to surface. An
// anchor that fails this sanity check is reported Unknown here regardless
// of what Check itself says; Sweep does not otherwise second-guess Check.
func Sweep(ctx context.Context, store string) ([]SweepFinding, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return nil, err
	}
	var out []SweepFinding
	for _, e := range entries {
		tier := entryTier(store, e)
		if tier == "" || tier == "agent" {
			continue
		}
		status, detail := Check(ctx, e)
		if e.Anchor.Type == "files" && status != Unknown {
			if bad := untrackedPaths(ctx, e.Anchor); len(bad) > 0 {
				detail = fmt.Sprintf(
					"anchor path(s) not tracked at HEAD in %s: %s (overrides cairn's %s verdict: %s — crn-6az.8.2)",
					e.Anchor.Repo, strings.Join(bad, ", "), status, detail,
				)
				status = Unknown
			}
		}
		out = append(out, SweepFinding{
			ID:         e.ID,
			Tier:       tier,
			Status:     status,
			Detail:     detail,
			AnchorType: e.Anchor.Type,
		})
	}
	return out, nil
}

// entryTier returns the entry's top-level scope directory (global, rig,
// role, or agent), derived from its body path the same way AF1 derives tier
// for review branches — from the file location, not any parsed identifier.
func entryTier(store string, e *Entry) string {
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// untrackedPaths returns the subset of a's configured paths that do not
// resolve to any tracked object at a.Repo's HEAD. It performs the same
// existence check ComputeFingerprint's expand() does internally, but —
// unlike expand() — treats "git ls-files found nothing" as a hard failure
// rather than silently falling back to the literal path.
func untrackedPaths(ctx context.Context, a Anchor) []string {
	if a.Repo == "" {
		return a.Paths
	}
	var bad []string
	for _, p := range a.Paths {
		out, ok := git(ctx, a.Repo, "ls-files", "--", p)
		if !ok || strings.TrimSpace(out) == "" {
			bad = append(bad, p)
		}
	}
	return bad
}
