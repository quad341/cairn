package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runList executes "cairn list <topic>" (plus any extra args) against the
// shared rootCmd, mirroring runStatus/runMap's --identity Changed-bit reset.
func runList(t *testing.T, dir, topic string, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"list", topic, "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

const (
	listSingleEntry    = "+++\nid = \"lst1\"\ntitle = \"List Target\"\nsummary = \"summary text\"\ntopic_key = \"list-topic\"\nscope = [\"rig:list-cmd\"]\n+++\nbody\n"
	listUntopicedEntry = "+++\nid = \"lst-u1\"\ntitle = \"Untopiced Target\"\nsummary = \"no topic here\"\nscope = [\"rig:list-cmd\"]\n+++\nbody\n"
)

func TestListCommandPrintsMatchedEntry(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/list-cmd/lst1.md", listSingleEntry)

	out, err := execRootJSON(t, "list", "list-topic", "--store", dir, "--identity", "rig:list-cmd")
	require.NoError(t, err)
	assert.Contains(t, out, "lst1")
	assert.Contains(t, out, "List Target")
	assert.NotContains(t, out, "summary text", "list's default projection is a retrieval stub, not the body")
}

func TestListCommandErrorsOnZeroMatches(t *testing.T) {
	err := runList(t, t.TempDir(), "no-such-topic", "--identity", "rig:list-cmd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no entries found for topic "no-such-topic"`)
}

func TestListCommandTranslatesUntopicedLabel(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/list-cmd/lst-u1.md", listUntopicedEntry)

	out, err := execRootJSON(t, "list", cairn.UntopicedLabel, "--store", dir, "--identity", "rig:list-cmd")
	require.NoError(t, err)
	assert.Contains(t, out, "lst-u1")
	assert.Contains(t, out, "Untopiced Target")
}

func TestListCommandJSONCarriesConflictOnEveryContestedHit(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/list-cmd/a.md", "+++\nid = \"a\"\ntitle = \"A\"\ntopic_key = \"tied\"\nscope = [\"rig:list-cmd\"]\ncreated_at = \"2026-08-15\"\n+++\na\n")
	seedEntry(t, dir, "rig/list-cmd/b.md", "+++\nid = \"b\"\ntitle = \"B\"\ntopic_key = \"tied\"\nscope = [\"rig:list-cmd\"]\ncreated_at = \"2026-08-15\"\n+++\nb\n")

	out, err := execRootJSON(t, "list", "tied", "--json", "--store", dir, "--identity", "rig:list-cmd")
	require.NoError(t, err)
	var result listOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 2)
	for _, item := range result.Entries {
		require.NotNil(t, item.Conflict)
		assert.Equal(t, []string{"a", "b"}, item.Conflict.EntryIDs)
		assert.Equal(t, "indistinguishable", item.Conflict.Reason)
	}
}

// TestListCommandSurfacesNewerEntryAfterTopicKeyCorrection is crn-pip8's own
// repro: `cairn list` must surface the entry that superseded a topic_key,
// not silently keep serving the one it was meant to replace.
//
// The bug only manifests when the two entries' created_at values are equal
// at RFC3339 (second) precision -- when they differ, moreSpecificReason's
// existing created_at rule already picks the newer entry correctly, with no
// dependency on the fix at all. An earlier version of this test drove both
// entries through real, sequential `cairn remember` calls and relied on them
// landing in the same wall-clock second; that tie is common but NOT
// guaranteed (a private-tier remember shells out to `git commit`, and this
// test's own manual repro observed the two calls straddle a real second
// boundary), so that version could pass against unfixed code purely by
// timing luck -- it did, on the first RED run.
//
// cairn has no injectable clock, so this version forces the tie explicitly:
// the first (to-be-superseded) entry is hand-seeded on disk with a
// created_at captured a moment before the second, real `cairn remember`
// call -- which is the only code path that can set OverriddenDuplicateOf,
// so the corrected entry must go through it for this test to exercise the
// fix at all. A require.Equal sanity check confirms the tie actually landed
// before asserting that the explicit override resolves it; a rare timing
// miss fails loudly as a setup problem instead of silently passing for the
// wrong reason.
func TestListCommandSurfacesNewerEntryAfterTopicKeyCorrection(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	const seededID = "000-manually-seeded-first"
	ts := time.Now().Format(time.RFC3339)
	seedEntry(t, store, filepath.Join("agent", "test", seededID+".md"), fmt.Sprintf(
		"+++\nid = %q\ntitle = \"PROBE-A-original-body-xyzzy\"\nsummary = \"PROBE-A-original-body-xyzzy\"\ntopic_key = \"probe-collision-test\"\nscope = [\"agent:test\"]\ncreated_at = %q\n+++\nPROBE-A-original-body-xyzzy\n",
		seededID, ts))
	first, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", seededID+".md"))
	require.NoError(t, err)

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "probe-collision-test", "--scope", "agent:test", "PROBE-B-second-body-plugh")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "both the seed and the real write must persist")
	var second *cairn.Entry
	for _, ent := range entries {
		parsed, parseErr := cairn.ParseEntry(filepath.Join(store, "agent", "test", ent.Name()))
		require.NoError(t, parseErr)
		if parsed.ID != first.ID {
			second = parsed
		}
	}
	require.NotNil(t, second, "expected a second, distinct entry alongside %s", first.ID)
	require.Equal(t, first.CreatedAt, second.CreatedAt,
		"test setup requires a created_at tie; a mismatch means a rare timing race crossed a second boundary between seeding and the real remember call -- rerun")

	out, listErr := execRootJSON(t, "list", "probe-collision-test", "--store", store, "--identity", "agent:test")
	require.NoError(t, listErr)
	var result listOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Entries, 1, "topic_key resolution must still collapse to exactly one winner")
	assert.Contains(t, out, second.ID,
		"crn-pip8: cairn list must surface the newer, corrected entry")
	assert.NotContains(t, out, first.ID,
		"crn-pip8: the superseded entry must not still win topic_key resolution")
}
