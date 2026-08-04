package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidationGitAnchorLoop is crn-ie0m.1: one end-to-end dogfooding pass
// over cairn's git-anchor lifecycle (remember -> reuse -> source changes ->
// stale detect -> re-derive -> verify fresh again), run against the real
// cobra command tree via execRoot/execRootJSON -- the same rootCmd.Execute()
// path a real agent invocation takes, not a shortcut through internal/cairn
// directly. Per crn-ie0m's operator directive this sequence is "resolved by
// dogfooding, not design debate": every step below observes real captured
// CLI output rather than asserting on an assumption about what the CLI
// should do. Each step is logged via t.Logf so the full trace can be posted
// to the bead's notes close to verbatim.
func TestValidationGitAnchorLoop(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	require.NoError(t, resetJSONFlag())
	resetRunIDFlag(t)
	t.Cleanup(func() {
		_ = resetIdentityFlag()
		_ = resetJSONFlag()
		resetRunIDFlag(t)
	})
	t.Setenv("CAIRN_IDENTITY", "")
	t.Setenv("CAIRN_RUN_ID", "")

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	const identity = "agent:validation-crn-ie0m1"
	const runID = "validation-crn-ie0m1"
	const topic = "cairn-freshness-status-values"

	store := t.TempDir()
	gitInit(t, store)

	// runPlain wraps execRoot with the same rootCmd flag hygiene
	// resetRememberFlags/execRootJSON already give their own callers:
	// freshnessCmd/verifyCmd don't reset --identity or --json themselves,
	// and both are rootCmd persistent flags that stick (Changed=true) once
	// any earlier call in this test sets them -- statusCmd in particular
	// actively errors on a stale --identity, and would silently route its
	// output through the (here, discarded) cmd.OutOrStdout() buffer instead
	// of the os.Stdout pipe execRoot captures if --json were still set.
	runPlain := func(args ...string) (string, error) {
		t.Helper()
		require.NoError(t, resetIdentityFlag())
		require.NoError(t, resetJSONFlag())
		return execRoot(args...)
	}

	// --- Step 1: pick a real, currently-true fact, tied to a specific
	// tracked file/commit ---
	//
	// Verbatim excerpt of internal/cairn/freshness.go's own status
	// constants, confirmed true by direct Read earlier this session.
	// Anchored in a throwaway fixture repo rather than this actual cairn
	// checkout: the point of this exercise is the anchor lifecycle
	// (fresh -> stale -> fresh), which needs a file this test itself
	// controls the edits to. Pointing the anchor at cairn's own live source
	// would make a future, unrelated edit to freshness.go silently change
	// what this test is asserting about.
	fixtureRepo := t.TempDir()
	gitInit(t, fixtureRepo)
	const factFile = "cairn-freshness-status-values.md"
	const v1Content = "// Freshness statuses.\nconst (\n\tFresh      = \"fresh\"\n\tStale      = \"stale\"\n\tUnknown    = \"unknown\"\n\tIncomplete = \"incomplete\"\n)\n"
	gitCommitFile(t, fixtureRepo, factFile, v1Content)
	v1SHA := strings.TrimSpace(gitOutput(t, fixtureRepo, "rev-parse", "HEAD"))
	t.Logf("STEP 1: fact picked -- internal/cairn/freshness.go's own Fresh/Stale/Unknown/Incomplete status constants. Anchored via fixture file %s committed at %s in throwaway repo %s.",
		factFile, v1SHA, fixtureRepo)

	const v1Body = "cairn's Check() function (internal/cairn/freshness.go) reports one of four freshness statuses: fresh, stale, unknown, or incomplete."

	// --- Step 2: cairn remember with a files anchor ---
	resetRememberFlags(t)
	rememberOut, err := execRootJSON(t, "remember", "--json", "--store", store,
		"--identity", identity, "--topic", topic,
		"--anchor-repo", fixtureRepo, "--anchor-path", factFile, "--verify",
		v1Body)
	require.NoError(t, err, "remember (v1) must succeed: %s", rememberOut)
	var v1Result RememberResult
	require.NoError(t, json.Unmarshal([]byte(rememberOut), &v1Result))
	require.NotEmpty(t, v1Result.ID)
	assert.Equal(t, []string{identity}, v1Result.Scope)
	t.Logf("STEP 2: `cairn remember --json --store %s --identity %s --topic %s --anchor-repo %s --anchor-path %s --verify '%s'` ->\n%s",
		store, identity, topic, fixtureRepo, factFile, v1Body, rememberOut)

	entryID := v1Result.ID

	// --- Step 3: reuse the entry via `cairn get`, and independently
	// observe (not assume) that the reuse was recorded ---
	getOut, err := execRootJSON(t, "get", "--json", entryID, "--store", store,
		"--identity", identity, "--run-id", runID)
	require.NoError(t, err, "get must succeed: %s", getOut)
	var got EntryResult
	require.NoError(t, json.Unmarshal([]byte(getOut), &got))
	assert.Equal(t, v1Body, got.Body)
	assert.Equal(t, cairn.Fresh, got.Freshness.Status,
		"a --verify'd anchor matching the fixture's current HEAD must read fresh before any edit")
	t.Logf("STEP 3a: `cairn get --json %s --store %s --identity %s --run-id %s` ->\n%s",
		entryID, store, identity, runID, getOut)

	rec := findRetrievalOutcomeRecord(t, xdg)
	assert.Equal(t, "hit", rec["outcome"])
	assert.Equal(t, entryID, rec["entry_id"])
	assert.Equal(t, runID, rec["run_id"])
	assert.EqualValues(t, 1, rec["reuse_count"], "Find's hit_count increment must flow through to the telemetry record")
	t.Logf("STEP 3b: obslog retrieval_outcome record (independent confirmation the reuse was actually recorded, not just that `get` printed something): %v", rec)

	// --- Step 4: a real, meaningful edit to the anchored source file ---
	const v2Content = "// Freshness statuses.\nconst (\n\tFresh      = \"fresh\"\n\tStale      = \"stale\"\n\tUnknown    = \"unknown\"\n\tErrored    = \"errored\" // renamed from Incomplete\n)\n"
	gitCommitFile(t, fixtureRepo, factFile, v2Content)
	v2SHA := strings.TrimSpace(gitOutput(t, fixtureRepo, "rev-parse", "HEAD"))
	require.NotEqual(t, v1SHA, v2SHA, "the edit must actually move the fixture's HEAD, not be a no-op")
	t.Logf("STEP 4: fixture edited (Incomplete=\"incomplete\" -> Errored=\"errored\", simulating a real rename) and committed at %s (was %s)",
		v2SHA, v1SHA)

	// --- Step 5: freshness/status must now observe Stale, not assume it ---
	freshOut, err := runPlain("freshness", entryID, "--store", store)
	require.NoError(t, err, "freshness returns a Go error only for Incomplete status, never for Stale: %s", freshOut)
	assert.True(t, strings.HasPrefix(freshOut, entryID+": "+cairn.Stale),
		"expected %q to report stale, got %q", entryID, freshOut)
	t.Logf("STEP 5a: `cairn freshness %s --store %s` -> %q", entryID, store, freshOut)

	statusOut, err := runPlain("status", "--store", store)
	require.NoError(t, err)
	assert.Contains(t, statusOut, entryID)
	assert.Contains(t, statusOut, cairn.Stale)
	t.Logf("STEP 5b: `cairn status --store %s` -> %q (cairn sweep intentionally not used here: it is shared-tier-only and errors given --identity, so it is architecturally inapplicable to this private agent:-scoped entry)",
		store, statusOut)

	// --- Step 6: re-derive the entry's content to match new reality ---
	//
	// cairn has no API to edit an existing entry's Body in place -- the
	// WriteBack* family only patches frontmatter fields (fingerprint/
	// verified_at, anchor, recurrence_count, promoted_bead_id), never Body
	// text. Re-deriving therefore means minting a fresh entry via remember:
	// same topic, same anchor, corrected body. Whether crn-qxj3's
	// recurrence guard (topic_key match + Jaccard content similarity) blocks
	// a plain re-remember here is a real open question this test observes
	// rather than assumes -- --force is only used if the first attempt
	// actually reports a conflict.
	const v2Body = "cairn's Check() function (internal/cairn/freshness.go) reports one of four freshness statuses: fresh, stale, unknown, or errored (renamed from incomplete)."
	resetRememberFlags(t)
	rederiveOut, rederiveErr := execRootJSON(t, "remember", "--json", "--store", store,
		"--identity", identity, "--topic", topic,
		"--anchor-repo", fixtureRepo, "--anchor-path", factFile, "--verify",
		v2Body)
	forced := false
	if rederiveErr != nil {
		t.Logf("STEP 6a: plain re-remember was blocked (observed, not assumed) -- err=%v out=%s", rederiveErr, rederiveOut)
		resetRememberFlags(t)
		rederiveOut, rederiveErr = execRootJSON(t, "remember", "--json", "--store", store,
			"--identity", identity, "--topic", topic,
			"--anchor-repo", fixtureRepo, "--anchor-path", factFile, "--verify", "--force",
			v2Body)
		forced = true
	}
	require.NoError(t, rederiveErr, "re-derive (v2, forced=%t) must succeed: %s", forced, rederiveOut)
	var v2Result RememberResult
	require.NoError(t, json.Unmarshal([]byte(rederiveOut), &v2Result))
	require.NotEmpty(t, v2Result.ID)
	require.NotEqual(t, entryID, v2Result.ID, "re-deriving mints a new entry id; there is no in-place Body edit")
	t.Logf("STEP 6b: `cairn remember --json --store %s --identity %s --topic %s --anchor-repo %s --anchor-path %s --verify%s '%s'` (forced=%t) ->\n%s",
		store, identity, topic, fixtureRepo, factFile, map[bool]string{true: " --force", false: ""}[forced], v2Body, forced, rederiveOut)

	newEntryID := v2Result.ID

	// --- Step 7: cairn verify, then observe (not assume) Fresh again ---
	verifyOut, err := runPlain("verify", newEntryID, "--store", store)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(verifyOut, "verified "+newEntryID+": fingerprint "),
		"expected a written-back fingerprint line, got %q", verifyOut)
	t.Logf("STEP 7a: `cairn verify %s --store %s` -> %q", newEntryID, store, verifyOut)

	freshOut2, err := runPlain("freshness", newEntryID, "--store", store)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(freshOut2, newEntryID+": "+cairn.Fresh),
		"the re-derived entry must now report fresh against the fixture's new HEAD, got %q", freshOut2)
	t.Logf("STEP 7b: `cairn freshness %s --store %s` -> %q", newEntryID, store, freshOut2)

	t.Logf("SUMMARY: v1 entry %s (anchored at fixture %s) went fresh -> stale when the fixture's HEAD moved to %s; re-derived entry %s (forced=%t) is fresh again at the new HEAD. v1 remains permanently stale -- cairn has no supersession/edit link between the two beyond sharing topic_key %q.",
		entryID, v1SHA, v2SHA, newEntryID, forced, topic)
}
