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
// For files-type anchors, Sweep independently names every configured path
// that fails to resolve to a tracked object at the anchor repo's HEAD.
// ComputeFingerprint itself now refuses to fabricate a fingerprint for such
// a path (crn-6az.8.2, fixed) and Check already reports Unknown on its own,
// so this no longer changes the verdict — it exists purely to enrich the
// detail with the specific untracked path(s), which Check's generic "not
// verifiable" message can't name and which the eventual bd bead body needs.
func Sweep(ctx context.Context, store string) ([]SweepFinding, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return nil, err
	}
	return SweepEntries(ctx, store, entries)
}

// SweepEntries is Sweep's body factored out to take an already-gathered
// entry list, so a caller with its own tolerantly-gathered entries (doctor.go)
// can reuse Sweep's real logic without re-triggering IterEntries' own
// abort-on-first-parse-error walk (OQ5/OQ3). store is still required (not
// just entries) because entryTier derives an entry's tier from its BodyPath
// relative to store -- Sweep itself is unchanged for its existing caller.
func SweepEntries(ctx context.Context, store string, entries []*Entry) ([]SweepFinding, error) {
	var out []SweepFinding
	for _, e := range entries {
		tier := entryTier(store, e)
		if tier == "" || tier == "agent" {
			continue
		}
		status, detail := Check(ctx, e)
		if e.Anchor.Type == "files" {
			bad, incomplete := untrackedPaths(ctx, e.Anchor)
			if !incomplete && len(bad) > 0 {
				detail = fmt.Sprintf(
					"anchor path(s) not tracked at HEAD in %s: %s (Check reported %s: %s)",
					e.Anchor.Repo, strings.Join(bad, ", "), status, detail,
				)
				status = Unknown
			}
			// incomplete: leave status/detail exactly as Check already set them --
			// Check derived Incomplete from this same anchor/repo/ctx, so
			// re-deriving a second message here would be redundant, not more
			// correct.
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

// untrackedPaths returns the subset of a's configured paths that are
// confirmed to not resolve to a tracked object at a.Repo's HEAD, plus
// whether the check was incomplete (a genuine git invocation failure part
// way through, as distinct from a confirmed miss). It reuses expand() and
// objectHash() directly — the same two calls ComputeFingerprint makes —
// rather than re-deriving an equivalent check: an earlier version re-derived
// this via a separate index-based `git ls-files` probe, which diverged from
// objectHash's HEAD-tree-based `git rev-parse HEAD:p` resolution for a path
// that is staged (`git add`ed) but not yet committed. `git ls-files` finds a
// staged path in the index and reports it clean, but `git rev-parse HEAD:p`
// cannot resolve it, so objectHash still fell back to the fabricated "?"
// value — the exact crn-6az.8.2 failure mode this guardrail exists to catch,
// reached through a different door than the never-added case (crn-8x4).
// Calling objectHash here instead of reimplementing its resolution keeps the
// two permanently in sync. incomplete must short-circuit "bad" entirely
// (crn-fdjc.1, FR-3): a genuine invocation failure must never be asserted as
// a confirmed untracked-path finding.
func untrackedPaths(ctx context.Context, a Anchor) (bad []string, incomplete bool) {
	if a.Repo == "" {
		return a.Paths, false
	}
	paths, err := expand(ctx, a.Repo, a.Paths)
	if err != nil {
		return nil, true // can't even enumerate -- assert nothing about tracked/untracked
	}
	for _, p := range paths {
		_, ok, err := objectHash(ctx, a.Repo, p)
		switch {
		case err != nil:
			incomplete = true // don't add to bad -- we don't know
		case !ok:
			bad = append(bad, p)
		}
	}
	return bad, incomplete
}
