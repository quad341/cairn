package cairn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filesEntry renders a minimal files-anchor entry as TOML+markdown. Sweep
// walks the store from disk (unlike Check's in-memory callers in
// freshness_test.go), so these tests need real files, not just *Entry values.
func filesEntry(id, repo string, paths []string, fingerprint string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return fmt.Sprintf(
		"+++\nid = %q\ntitle = %q\nscope = []\n\n[anchor]\ntype = \"files\"\nrepo = %q\npaths = [%s]\nfingerprint = %q\n+++\nbody\n",
		id, id, repo, strings.Join(quoted, ", "), fingerprint,
	)
}

func findingFor(t *testing.T, findings []SweepFinding, id string) SweepFinding {
	t.Helper()
	for _, f := range findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no sweep finding for entry %q in %+v", id, findings)
	return SweepFinding{}
}

// TestSweepMatchesCheckForHealthyAnchors is acceptance criterion 1: entries
// whose anchor paths are genuinely tracked must get exactly Check's own
// verdict, with no override.
func TestSweepMatchesCheckForHealthyAnchors(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	fp := ComputeFingerprint(ctx, Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NotEmpty(t, fp)

	writeFile(t, dir, "global/fresh.md", filesEntry("fresh-1", repo, []string{"a.go"}, fp))
	writeFile(t, dir, "global/unverified.md", filesEntry("unverified-1", repo, []string{"a.go"}, ""))
	writeFile(t, dir, "global/noanchor.md", "+++\nid = \"no-anchor-1\"\ntitle = \"n\"\nscope = []\n+++\nbody\n")

	findings, err := Sweep(ctx, dir)
	require.NoError(t, err)

	assert.Equal(t, Fresh, findingFor(t, findings, "fresh-1").Status)
	assert.Equal(t, Unknown, findingFor(t, findings, "unverified-1").Status)
	assert.Equal(t, Unknown, findingFor(t, findings, "no-anchor-1").Status)
}

// TestSweepMatchesCheckForDriftedAnchor: a real content drift (source
// changed after the stored fingerprint was stamped) must come through as
// Stale — the sanity check must not mask genuine drift on a healthy anchor.
func TestSweepMatchesCheckForDriftedAnchor(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	src := filepath.Join(repo, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	fp := ComputeFingerprint(ctx, Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NotEmpty(t, fp)
	writeFile(t, dir, "global/stale.md", filesEntry("stale-1", repo, []string{"a.go"}, fp))

	require.NoError(t, os.WriteFile(src, []byte("package a\n\n// changed\n"), 0o600))
	gitCommitAll(t, repo, "change")

	findings, err := Sweep(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, Stale, findingFor(t, findings, "stale-1").Status)
}

// TestSweepOverridesUntrackedAnchorToUnknown is acceptance criterion 2: the
// crn-6az.8.2 regression test. It first proves the bug is real against
// today's Check/ComputeFingerprint (a stable-but-meaningless fingerprint for
// an untracked path, so a once-verified entry reads Fresh forever) and then
// proves Sweep's independent trackedness check catches it regardless.
func TestSweepOverridesUntrackedAnchorToUnknown(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	brokenAnchor := Anchor{Type: "files", Repo: repo, Paths: []string{"gone.txt"}}
	bogusFP := ComputeFingerprint(ctx, brokenAnchor)
	require.NotEmpty(t, bogusFP, "crn-6az.8.2: an untracked path must still produce a non-empty (bogus) fingerprint for this regression test to be meaningful")

	writeFile(t, dir, "global/broken.md", filesEntry("broken-1", repo, []string{"gone.txt"}, bogusFP))

	e, err := Find(dir, "broken-1")
	require.NoError(t, err)
	naiveStatus, _ := Check(ctx, e)
	require.Equal(t, Fresh, naiveStatus,
		"crn-6az.8.2 regression baseline: Check alone must still be fooled by the stamped bogus fingerprint — "+
			"if this fails, the underlying bug was fixed and Sweep's workaround may no longer be needed")

	findings, err := Sweep(ctx, dir)
	require.NoError(t, err)
	f := findingFor(t, findings, "broken-1")
	assert.Equal(t, Unknown, f.Status, "an untracked anchor path must override Check's fabricated Fresh verdict")
	assert.Contains(t, f.Detail, "gone.txt", "the detail should name the untracked path for the eventual bd bead body")
}

// TestSweepTierScoping is acceptance criterion 5: global/rig/role are in
// remit, agent/ private entries are not.
func TestSweepTierScoping(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n")
	writeFile(t, dir, "rig/alpha/r.md", "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	writeFile(t, dir, "role/investigator/o.md", "+++\nid = \"o\"\ntitle = \"o\"\nscope = [\"role:investigator\"]\n+++\nx\n")
	writeFile(t, dir, "agent/bot/a.md", "+++\nid = \"a\"\ntitle = \"a\"\nscope = [\"agent:bot\"]\n+++\nx\n")

	findings, err := Sweep(t.Context(), dir)
	require.NoError(t, err)

	seen := map[string]string{}
	for _, f := range findings {
		seen[f.ID] = f.Tier
	}
	assert.Equal(t, "global", seen["g"])
	assert.Equal(t, "rig", seen["r"])
	assert.Equal(t, "role", seen["o"])
	assert.NotContains(t, seen, "a", "agent/ private entries are out of the sweep's remit")
}

// TestSweepNeverWrites is acceptance criterion 4: Sweep must be pure
// read/report, even for entries it flags as Stale — the librarian sweep
// files a bead, it does not rewrite an already-curated entry itself.
func TestSweepNeverWrites(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	src := filepath.Join(repo, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	fp := ComputeFingerprint(ctx, Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NotEmpty(t, fp)
	entryPath := writeFile(t, dir, "global/stale.md", filesEntry("stale-1", repo, []string{"a.go"}, fp))

	require.NoError(t, os.WriteFile(src, []byte("package a\n\n// changed\n"), 0o600))
	gitCommitAll(t, repo, "change")

	before, err := os.ReadFile(entryPath)
	require.NoError(t, err)

	findings, err := Sweep(ctx, dir)
	require.NoError(t, err)
	require.Equal(t, Stale, findingFor(t, findings, "stale-1").Status, "precondition: this entry must actually be flagged")

	after, err := os.ReadFile(entryPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "Sweep must never modify an entry file on disk, even one it flags as Stale")
}
