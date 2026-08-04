package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildStoreFromWithinIt seeds store's index by running `cairn reindex
// --store .` with the process cwd set to store itself. "--store ." is what
// default store resolution also produces (storePathWithSource's cwd
// fallback), so this reproduces the same on-disk index shape a bare `cairn
// remember`/`cairn get` run from inside the store checkout would leave
// behind: a body_path column holding a cwd-relative value (e.g.
// "global/<id>.md") rather than an absolute one. That relative value is
// exactly what crn-o6mn's bug report found baked into the mayor's real
// store, and it persists across later reindex checks because a non-git
// store is never considered stale once it has been indexed once.
func buildStoreFromWithinIt(t *testing.T, store string) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	defer func() { require.NoError(t, os.Chdir(origWD)) }()

	require.NoError(t, os.Chdir(store))
	_, err = execRoot("reindex", "--store", ".")
	require.NoError(t, err)
}

// runFromElsewhere chdirs to a directory that is neither store nor the
// caller's own cwd before calling fn, restoring the original cwd
// afterward -- crn-o6mn's repro condition: reading from any cwd that is not
// the store itself, which is the normal case for every real agent (each
// role runs from its own worktree, never from the store).
func runFromElsewhere(t *testing.T, fn func()) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	defer func() { require.NoError(t, os.Chdir(origWD)) }()

	require.NoError(t, os.Chdir(t.TempDir()))
	fn()
}

// TestGetResolvesBodyPathAgainstStoreNotCWD is crn-o6mn's regression: `cairn
// get` must resolve an entry's body against the resolved store root -- the
// same root already used to open the SQLite index -- not against whatever
// the process's current directory happens to be.
func TestGetResolvesBodyPathAgainstStoreNotCWD(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	store := t.TempDir()
	seedEntry(t, store, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")
	buildStoreFromWithinIt(t, store)

	var out string
	var err error
	runFromElsewhere(t, func() {
		out, err = execRoot("get", "g/a", "--store", store)
	})
	require.NoError(t, err, "cairn get must resolve the entry body against the resolved store root, not the process cwd")
	assert.Contains(t, out, "g/a")
}

// TestListResolvesBodyPathAgainstStoreNotCWD is TestGetResolvesBodyPath...'s
// counterpart for `cairn list`, which reads entry bodies via ListByTopic
// rather than Find but hits the exact same body_path column.
func TestListResolvesBodyPathAgainstStoreNotCWD(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	store := t.TempDir()
	seedEntry(t, store, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"cwd-topic\"\nscope = []\n+++\nbody\n")
	buildStoreFromWithinIt(t, store)

	var out string
	var err error
	runFromElsewhere(t, func() {
		out, err = execRoot("list", "cwd-topic", "--store", store)
	})
	require.NoError(t, err, "cairn list must resolve entry bodies against the resolved store root, not the process cwd")
	assert.Contains(t, out, "g/a")
}

// TestFreshnessResolvesBodyPathAgainstStoreNotCWD is
// TestGetResolvesBodyPath...'s counterpart for `cairn freshness`, which
// loads the entry via the same cairn.Find call get uses.
func TestFreshnessResolvesBodyPathAgainstStoreNotCWD(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	store := t.TempDir()
	seedEntry(t, store, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")
	buildStoreFromWithinIt(t, store)

	var out string
	var err error
	runFromElsewhere(t, func() {
		out, err = execRoot("freshness", "g/a", "--store", store)
	})
	require.NoError(t, err, "cairn freshness must resolve the entry body against the resolved store root, not the process cwd")
	assert.Contains(t, out, "g/a")
}
