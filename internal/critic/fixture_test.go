package critic

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/require"
)

// gitConfigValue reads one config key out of the repo at dir.
func gitConfigValue(t *testing.T, dir, key string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "config", "--get", key).CombinedOutput()
	require.NoError(t, err, "git config --get %s: %s", key, out)
	return strings.TrimSpace(string(out))
}

// TestFixtureRepoDisablesAutoMaintenance pins the fix for the cleanup-time
// flake that got refiled four times (crn-9k30, crn-u8c0, crn-mxvn,
// crn-tw3bl).
//
// It asserts the config rather than trying to catch the race, deliberately.
// The failure it guards against is inherently probabilistic — `git commit`
// spawns `git maintenance run --auto --detach`, that detached process
// repacks in the background, and whether it collides with t.TempDir()'s
// RemoveAll depends on machine load (measured 13/30 before the fix, 0/30
// after). A test that re-ran the scenario until it flaked would itself be
// flaky and slow. The config is the actual invariant: if these two settings
// survive, no detached maintenance is ever spawned, and the race has no
// mechanism.
func TestFixtureRepoDisablesAutoMaintenance(t *testing.T) {
	store := t.TempDir()
	e, err := cairn.NewEntry(cairn.NewEntryParams{
		Type:      cairn.EntryTypeKnowledge,
		TopicKey:  "fixture-config-probe",
		Scope:     []string{"rig:critic-test"},
		Body:      "body",
		CreatedBy: "critic",
	})
	require.NoError(t, err)

	cleanup, err := seedEntries(t.Context(), store, []*cairn.Entry{e})
	defer cleanup()
	require.NoError(t, err)

	require.Equal(t, "0", gitConfigValue(t, store, "gc.auto"),
		"fixture repo must disable auto-gc; a detached `git maintenance run --auto` "+
			"repacks after the test returns and races t.TempDir() cleanup")
	require.Equal(t, "false", gitConfigValue(t, store, "maintenance.auto"),
		"fixture repo must disable auto-maintenance; see gc.auto assertion above")
}

// TestGitInitAndCommitDisablesAutoMaintenance covers the freshness scenario's
// own fixture-repo builder, which had an independent copy of the same config
// list. Both now share fixtureRepoInit; this is what keeps them shared.
func TestGitInitAndCommitDisablesAutoMaintenance(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, gitInitAndCommit(t.Context(), dir, "anchored.txt", "contents"))

	require.Equal(t, "0", gitConfigValue(t, dir, "gc.auto"))
	require.Equal(t, "false", gitConfigValue(t, dir, "maintenance.auto"))
}
