package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

func init() {
	anchorCmd.Flags().String("repo", "",
		"git repo the --path values are tracked in")
	anchorCmd.Flags().StringArray("path", nil,
		"a path (relative to --repo) anchoring this entry to its source; repeatable")
	anchorCmd.Flags().Bool("verify", false,
		"also compute and persist the anchor's fingerprint (soft-fails to stderr if it doesn't resolve)")
	rootCmd.AddCommand(anchorCmd)
}

// anchorCmd closes the gap that left most of a store permanently unanchored
// (crn-01fj): `remember --anchor-repo/--anchor-path` only builds an anchor
// at creation time, and `verify` only RECOMPUTES a fingerprint for an entry
// that already carries repo+paths. Neither can give an anchor to an entry
// that already exists — which is nearly every entry in a store migrated
// from flat files, since those had no anchor concept to carry over.
//
// Without an anchor an entry's freshness is a guess from its age, so the
// entries this verb reaches are exactly the ones whose staleness currently
// cannot be detected at all.
var anchorCmd = &cobra.Command{
	Use:   "anchor <id>",
	Short: "Attach a files source anchor to an existing entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		paths, _ := cmd.Flags().GetStringArray("path")
		if repo == "" {
			return emitError(cmd, errors.New("--anchor-repo is required: an anchor without a repo cannot resolve"))
		}
		if len(paths) == 0 {
			return emitError(cmd, errors.New("--anchor-path is required (repeatable): an anchor without paths cannot resolve"))
		}

		e, err := cairn.EntryByID(cmd.Context(), storePath(), args[0])
		if errors.Is(err, cairn.ErrNotFound) {
			return emitError(cmd, fmt.Errorf("no entry %q: %w", args[0], err))
		}
		if err != nil {
			return emitError(cmd, err)
		}

		// Containment guard (crn-2c8e). Reads resolve through the index,
		// which records body_paths pointing at wherever the index was
		// built, so a copied/restored/moved store addresses its ORIGIN.
		// Writing through that path would make `--store <copy>` modify the
		// original — the precise opposite of why anyone passes --store, and
		// silently, since the write reports success. Re-root the path inside
		// the store actually being addressed.
		contained, err := containedBodyPath(storePath(), e.BodyPath)
		if err != nil {
			return emitError(cmd, err)
		}
		e.BodyPath = contained

		e.Anchor = cairn.Anchor{Type: "files", Repo: repo, Paths: paths}

		// Fingerprint before the write so a single WriteBackAnchor persists
		// the whole anchor, rather than leaving a window where the entry
		// claims a files anchor with no fingerprint behind it.
		if verify, _ := cmd.Flags().GetBool("verify"); verify {
			fp, ferr := cairn.ComputeFingerprint(cmd.Context(), e.Anchor)
			switch {
			case ferr != nil:
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: --verify: could not compute anchor fingerprint: %v\n", ferr)
			case fp == "":
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: --verify: anchor does not resolve to a trackable object; skipping\n")
			default:
				e.Anchor.Fingerprint = fp
				e.VerifiedAt = time.Now().Format(time.DateOnly)
			}
		}

		if err := e.WriteBackAnchor(); err != nil {
			return emitError(cmd, err)
		}
		if e.Anchor.Fingerprint != "" {
			if err := e.WriteBack(); err != nil {
				return emitError(cmd, err)
			}
		}

		if wantsJSON(cmd) {
			return emitJSON(cmd.OutOrStdout(), AnchorResult{
				ID:          e.ID,
				Repo:        repo,
				Paths:       paths,
				Fingerprint: e.Anchor.Fingerprint,
			})
		}
		if e.Anchor.Fingerprint != "" {
			fmt.Printf("anchored %s to %d path(s) in %s: fingerprint %s @ %s\n",
				e.ID, len(paths), repo, e.Anchor.Fingerprint, e.VerifiedAt)
		} else {
			fmt.Printf("anchored %s to %d path(s) in %s (unverified — run `cairn verify %s` to fingerprint it)\n",
				e.ID, len(paths), repo, e.ID)
		}
		return nil
	},
}

// containedBodyPath re-roots bodyPath inside store when it addresses some
// other location, so a write can never escape the store the caller named.
//
// Entry bodies always live at <store>/<tier>/…/<id>.md, with tier one of the
// known scope dirs, so the tail from the LAST tier segment onward is the
// path's identity within any store and can be re-attached to this one. A
// path already inside store is returned unchanged; one with no recognisable
// tier segment is refused rather than guessed at.
func containedBodyPath(store, bodyPath string) (string, error) {
	absStore, err := filepath.Abs(store)
	if err != nil {
		return "", err
	}
	absBody, err := filepath.Abs(bodyPath)
	if err != nil {
		return "", err
	}
	if rel, rerr := filepath.Rel(absStore, absBody); rerr == nil && !strings.HasPrefix(rel, "..") {
		return absBody, nil
	}

	segs := strings.Split(filepath.ToSlash(absBody), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		switch segs[i] {
		case "global", "rig", "role", "agent":
			return filepath.Join(absStore, filepath.Join(segs[i:]...)), nil
		}
	}
	return "", fmt.Errorf(
		"entry body %q is outside store %q and carries no recognisable scope directory; refusing to write",
		bodyPath, store)
}

// AnchorResult is cairn anchor --json's shape.
type AnchorResult struct {
	ID          string   `json:"id"`
	Repo        string   `json:"repo"`
	Paths       []string `json:"paths"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}
