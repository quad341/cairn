package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runStatus executes "cairn status" (plus any extra args) against the shared
// rootCmd. It resets the --identity flag's Changed bit first: rootCmd is a
// package-level singleton, so pflag's Changed state otherwise leaks across
// tests that run in the same binary.
func runStatus(t *testing.T, dir string, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"status", "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. statusCmd's RunE prints via fmt.Println/
// fmt.Printf directly rather than cmd.OutOrStdout(), so rootCmd.SetOut has no
// effect on that output — this is the only way to observe it from a test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// seedEntry writes a fixture entry (TOML-frontmatter markdown) under one of
// the store's scope dirs, mirroring internal/cairn's own test fixtures.
func seedEntry(t *testing.T, storeDir, relPath, content string) {
	t.Helper()
	p := filepath.Join(storeDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

// lineFor returns the status line for the given entry id, matched as the
// second whitespace-delimited field (after the freshness flag) so a shared
// prefix between two ids can't produce a false match.
func lineFor(t *testing.T, output, id string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == id {
			return line
		}
	}
	t.Fatalf("no status line for entry %q in output:\n%s", id, output)
	return ""
}

func TestStatusRejectsExplicitIdentityFlag(t *testing.T) {
	err := runStatus(t, t.TempDir(), "--identity", "rig:alpha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusRejectsIdentityEnv(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha")
	err := runStatus(t, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

func TestStatusBareIsUnchanged(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "")
	err := runStatus(t, t.TempDir())
	require.NoError(t, err)
}

func TestStatusRejectsStrayPositionalArgs(t *testing.T) {
	err := runStatus(t, t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

const (
	shadowedByScoped = "+++\nid = \"less-specific\"\ntitle = \"Less specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	shadowsScoped    = "+++\nid = \"more-specific\"\ntitle = \"More specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nbody\n"

	unrelatedTopicA = "+++\nid = \"unrelated-a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n"
	unrelatedTopicB = "+++\nid = \"unrelated-b\"\ntitle = \"B\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nbody\n"
)

func TestStatusAnnotatesShadowedEntries(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/less-specific.md", shadowedByScoped)
	seedEntry(t, dir, "role/investigator/more-specific.md", shadowsScoped)

	var err error
	out := captureStdout(t, func() { err = runStatus(t, dir) })
	require.NoError(t, err)

	assert.Contains(t, lineFor(t, out, "less-specific"), "[SHADOWED BY more-specific]",
		"the less specific entry's line must be annotated with its shadower")
	assert.NotContains(t, lineFor(t, out, "more-specific"), "SHADOWED BY",
		"the winning (more specific) entry's line must not be annotated")
}

func TestStatusNoAnnotationWithoutShadow(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/unrelated-a.md", unrelatedTopicA)
	seedEntry(t, dir, "global/unrelated-b.md", unrelatedTopicB)

	var err error
	out := captureStdout(t, func() { err = runStatus(t, dir) })
	require.NoError(t, err)

	assert.NotContains(t, out, "SHADOWED BY",
		"entries with no shadow relationship must never be annotated")
}

// runMap executes "cairn map" (plus any extra args) against the shared
// rootCmd, mirroring runStatus above (including the same --identity
// Changed-bit reset, since rootCmd is a package-level singleton).
func runMap(t *testing.T, dir string, extraArgs ...string) error {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, f)
	f.Changed = false
	t.Cleanup(func() { f.Changed = false })

	args := append([]string{"map", "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
}

// mapLineRE matches mapCmd's "  %s  (%d)\n" per-topic line format.
var mapLineRE = regexp.MustCompile(`(?m)^  (\S+)  \((\d+)\)$`)

// topicCounts parses a map rendering's per-topic lines into a plain
// topic->count map, so it can be compared against prime's own tally for
// exact agreement independent of presentation.
func topicCounts(t *testing.T, out string, re *regexp.Regexp) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		n, err := strconv.Atoi(m[2])
		require.NoError(t, err)
		counts[m[1]] = n
	}
	return counts
}

// primeItemTopicCounts tallies a PrimeResult's items by topic key -- the
// structured-API equivalent of topicCounts, used for prime's side of the
// comparison below. crn-0vqk.2 changed prime's text rendering from a
// per-topic grouped summary (matching map's format) to a per-entry itemized
// list, since budget truncation, freshness, and hit_count are all per-entry
// concepts; the map/prime agreement check now reads prime's structured
// PrimeResult directly instead of regex-scraping its rendered text.
func primeItemTopicCounts(items []cairn.PrimeItem) map[string]int {
	counts := map[string]int{}
	for _, it := range items {
		counts[it.TopicKey]++
	}
	return counts
}

// TestMapAndPrimeAgreeOnTopicCounts guards the specificity-shadowing
// semantics Visible implements (crn-6az.6.1.4): map and prime independently
// recompute topic->count from the same Visible() result, so a shadowed
// entry must vanish from both tallies identically, not just one, and
// neither surface may double-count the topic key it shares with its
// shadower.
func TestMapAndPrimeAgreeOnTopicCounts(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/less-specific.md", shadowedByScoped)
	seedEntry(t, dir, "role/investigator/more-specific.md", shadowsScoped)
	seedEntry(t, dir, "global/unrelated-a.md", unrelatedTopicA)
	seedEntry(t, dir, "global/unrelated-b.md", unrelatedTopicB)

	var mapErr error
	mapOut := captureStdout(t, func() {
		mapErr = runMap(t, dir, "--identity", "rig:alpha,role:investigator")
	})
	require.NoError(t, mapErr)

	const generousBudget = 1 << 20
	primeResult, err := cairn.Prime(t.Context(), dir, []string{"rig:alpha", "role:investigator"}, generousBudget)
	require.NoError(t, err)

	want := map[string]int{"shared": 1, "topic-a": 1, "topic-b": 1}
	assert.Equal(t, want, topicCounts(t, mapOut, mapLineRE), "map's topic counts")
	assert.Equal(t, want, primeItemTopicCounts(primeResult.Items), "prime's topic counts")
}

func TestReindexRejectsStrayPositionalArgs(t *testing.T) {
	_, err := execRoot("reindex", "--store", t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestMapRejectsStrayPositionalArgs(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	// Mirrors crn-6az.3's own repro: a second --identity tag typed as a
	// space-separated arg (instead of comma-joined into one flag value) used
	// to be silently swallowed as a stray positional, narrowing the resolved
	// identity to just the first tag with no indication anything was dropped.
	_, err := execRoot("map", "--store", t.TempDir(), "--identity", "role:architect", "role:pm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role:pm")
}

func TestGetShowsKindAndAutoActionable(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\nkind = \"remediation\"\nauto_actionable = true\nscope = []\n+++\nbody\n")

	out, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "kind: remediation")
	assert.Contains(t, out, "auto_actionable: true")
}

func TestGetDefaultsKindToNoteWhenUnset(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	out, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "kind: note")
	assert.Contains(t, out, "auto_actionable: false")
}

func TestGetNoConflictsWhenNoneVisible(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	out, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "conflicts: none")
}

// TestGetReportsTopicKeyConflict seeds two entries sharing a topic_key with
// incomparable scopes (neither a superset of the other -- not legitimate
// ShadowMap shadowing) and confirms `cairn get` on one reports a conflict
// against the other. role/hook is the one Visible()'s cruder tag-count
// shadow() would otherwise drop (it loses the lexical-id tiebreak against
// rig/hook at equal scope length) -- fetching it directly via get bypasses
// that (Find doesn't filter by scope/shadow at all), while rig/hook, the
// shadow "winner", survives into the --identity-scoped Visible() list get
// uses to compute conflicts against.
func TestGetReportsTopicKeyConflict(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/hook.md",
		"+++\nid = \"rig/hook\"\ntitle = \"Configuring the hook\"\ntopic_key = \"shared-hook\"\nscope = [\"rig:alpha\"]\n+++\nbody\n")
	seedEntry(t, dir, "role/builder/hook.md",
		"+++\nid = \"role/hook\"\ntitle = \"Totally unrelated deploy pipeline notes\"\ntopic_key = \"shared-hook\"\nscope = [\"role:builder\"]\n+++\nbody\n")

	out, err := execRoot("get", "role/hook", "--store", dir, "--identity", "rig:alpha,role:builder")
	require.NoError(t, err)

	assert.Contains(t, out, "conflicts: 1")
	assert.Contains(t, out, "rig/hook")
}

// TestGetReportsContentConflict uses TestSimilarityThresholdExamples' own
// passing pair (distinct topic_keys, so the topic_key signal never engages)
// to exercise get's content-similarity conflict reporting end to end. Both
// entries are global (empty scope), so no --identity is needed for either to
// be visible; g/hook-a is naturally included in its own Visible() output too
// (get's real call pattern), which is what exercises Conflicts' self-skip.
func TestGetReportsContentConflict(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/hook-a\"\ntitle = \"Configuring the git pre-commit hook\"\nsummary = \"Steps to enable the shared pre-commit hook for this repo\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")
	seedEntry(t, dir, "global/b.md",
		"+++\nid = \"g/hook-b\"\ntitle = \"Enabling the shared pre-commit hook\"\nsummary = \"How to configure the git pre-commit hook in this repo\"\ntopic_key = \"topic-b\"\nscope = []\n+++\nbody\n")

	out, err := execRoot("get", "g/hook-a", "--store", dir)
	require.NoError(t, err)

	assert.Contains(t, out, "conflicts: 1")
	assert.Contains(t, out, "g/hook-b")
}

// resetRunIDFlag clears rootCmd's --run-id flag around a test, mirroring
// resetIdentityFlag's rationale: rootCmd is a package-level singleton, so a
// prior test's --run-id value and Changed bit otherwise leak into whichever
// test runs next in this binary.
func resetRunIDFlag(t *testing.T) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("run-id")
	require.NotNil(t, f)
	require.NoError(t, f.Value.Set(""))
	f.Changed = false
}

// findRetrievalOutcomeRecord reads xdg's debug.jsonl and returns the single
// "retrieval_outcome" record it contains. One execRoot("get", ...) call also
// logs a "context" record (root's PersistentPreRunE) and usually a
// "freshness_check" record (cairn.Check), so this filters by kind rather
// than assuming a fixed line index or count.
func findRetrievalOutcomeRecord(t *testing.T, xdg string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(xdg, "cairn", "debug.jsonl"))
	require.NoError(t, err)

	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["kind"] == "retrieval_outcome" {
			found = append(found, rec)
		}
	}
	require.Len(t, found, 1, "expected exactly one retrieval_outcome record in:\n%s", data)
	return found[0]
}

// TestGetLogsRetrievalOutcomeHitOnSuccess covers crn-tekm's core acceptance
// criterion: a successful `cairn get` on a non-stale entry must emit a
// per-interaction "retrieval_outcome" record carrying the join keys (run_id,
// identity_tags) and reuse signal (reuse_count, sourced from Find's own
// post-increment HitCount, not the pre-hit file value) a later report needs
// to compute avoided-vs-spent tokens.
func TestGetLogsRetrievalOutcomeHitOnSuccess(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })
	resetRunIDFlag(t)
	t.Cleanup(func() { resetRunIDFlag(t) })

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_RUN_ID", "")

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody text here\n")

	out, err := execRoot("get", "g/a", "--store", dir, "--identity", "rig:web", "--run-id", "run-123")
	require.NoError(t, err)
	assert.Contains(t, out, "g/a")

	rec := findRetrievalOutcomeRecord(t, xdg)
	assert.Equal(t, "hit", rec["outcome"])
	assert.Equal(t, "g/a", rec["entry_id"])
	assert.Equal(t, "run-123", rec["run_id"])
	assert.Equal(t, []any{"rig:web"}, rec["identity_tags"])
	assert.EqualValues(t, 1, rec["reuse_count"], "Find's hit_count increment must flow through to reuse_count")
	tokens, ok := rec["payload_tokens"].(float64)
	require.True(t, ok, "payload_tokens must be numeric")
	assert.Greater(t, tokens, float64(0), "a non-empty body must report a positive token estimate")
}

// TestGetLogsRetrievalOutcomeMissOnNotFound covers the miss branch: a
// not-found lookup must still emit a retrieval_outcome record (not just the
// user-facing error) so a miss is visible in the same joinable stream as a
// hit, with the requested id preserved as entry_id and zeroed cost/reuse
// fields since nothing was actually retrieved. It also incidentally covers
// run_id's no-flag/no-env default: empty, not omitted.
func TestGetLogsRetrievalOutcomeMissOnNotFound(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })
	resetRunIDFlag(t)
	t.Cleanup(func() { resetRunIDFlag(t) })

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_RUN_ID", "")

	dir := t.TempDir()

	_, err := execRoot("get", "does-not-exist", "--store", dir)
	require.Error(t, err)

	rec := findRetrievalOutcomeRecord(t, xdg)
	assert.Equal(t, "miss", rec["outcome"])
	assert.Equal(t, "does-not-exist", rec["entry_id"])
	assert.EqualValues(t, 0, rec["payload_tokens"])
	assert.EqualValues(t, 0, rec["reuse_count"])
	assert.Equal(t, "", rec["run_id"])
}

// TestGetLogsRetrievalOutcomeStaleWhenAnchorDrifted covers the third outcome
// branch: found but stale must be distinguished from a plain hit. A
// "commit"-type anchor makes this deterministic with no git repo involved --
// ComputeFingerprint returns Spec directly for that anchor type, so a
// Fingerprint that disagrees with Spec is stale by construction.
func TestGetLogsRetrievalOutcomeStaleWhenAnchorDrifted(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })
	resetRunIDFlag(t)
	t.Cleanup(func() { resetRunIDFlag(t) })

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_RUN_ID", "")

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n\n[anchor]\ntype = \"commit\"\nspec = \"abc123\"\nfingerprint = \"different456\"\n+++\nbody\n")

	_, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)

	rec := findRetrievalOutcomeRecord(t, xdg)
	assert.Equal(t, "stale", rec["outcome"])
	assert.Equal(t, "g/a", rec["entry_id"])
}

// TestGetRetrievalOutcomeRunIDFromEnv covers $CAIRN_RUN_ID as the fallback
// join-key source when --run-id isn't passed, mirroring identityWithSource's
// flag-then-env precedent for the same reason: an agent fleet sets run
// identity via environment far more often than a literal CLI flag.
func TestGetRetrievalOutcomeRunIDFromEnv(t *testing.T) {
	require.NoError(t, resetIdentityFlag())
	t.Cleanup(func() { _ = resetIdentityFlag() })
	resetRunIDFlag(t)
	t.Cleanup(func() { resetRunIDFlag(t) })

	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_RUN_ID", "env-run-456")

	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md", "+++\nid = \"g/a\"\ntitle = \"A\"\nscope = []\n+++\nbody\n")

	_, err := execRoot("get", "g/a", "--store", dir)
	require.NoError(t, err)

	rec := findRetrievalOutcomeRecord(t, xdg)
	assert.Equal(t, "env-run-456", rec["run_id"])
}
