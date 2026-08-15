package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRageFlags clears rageCmd's own flags plus the shared --identity flag
// between tests, the same pflag-state-leakage concern resetDoctorFlags
// documents for doctorCmd: these are package-level singletons shared across
// every test in this binary.
func resetRageFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"include-bodies", "command", "exit-code", "max-log-bytes"} {
		f := rageCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(f.DefValue))
		f.Changed = false
	}
	require.NoError(t, resetIdentityFlag())
}

// newRageTestCmd points storePath() at dir, isolates obslog's XDG state
// directory under a fresh t.TempDir() (so a test never touches the real
// user's debug log or leaves a rage bundle behind on the host), and returns
// rageCmd wired to a fresh output buffer, for driving runRage directly
// (bypassing rootCmd.Execute()/os.Exit), mirroring doctor_test.go's
// newDoctorTestCmd. dir == "" leaves storeFlag unset, e.g. to exercise
// $CAIRN_STORE-sourced resolution instead.
func newRageTestCmd(t *testing.T, dir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	resetRageFlags(t)
	t.Cleanup(func() { resetRageFlags(t) })

	prevStore := storeFlag
	storeFlag = dir
	t.Cleanup(func() { storeFlag = prevStore })

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var buf bytes.Buffer
	rageCmd.SetOut(&buf)
	rageCmd.SetErr(&buf)
	rageCmd.SetContext(t.Context())
	t.Cleanup(func() {
		rageCmd.SetContext(nil) //nolint:staticcheck // deliberate: restores cobra's own "never set" zero value, not a real call site
		rageCmd.SetOut(nil)
		rageCmd.SetErr(nil)
	})
	return rageCmd, &buf
}

// rageStdoutLines splits buf's captured stdout into its non-empty lines:
// exit_contract item 1 requires stdout to be exactly the bundle path
// followed by the issue URL, nothing else.
func rageStdoutLines(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// runRageForBundle drives runRage to completion and decodes the bundle file
// it wrote (named by stdout's first line) back into a RageBundle, failing
// the test on any error along the way.
func runRageForBundle(t *testing.T, cmd *cobra.Command, buf *bytes.Buffer) RageBundle {
	t.Helper()
	code, err := runRage(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	lines := rageStdoutLines(t, buf)
	require.Len(t, lines, 2, "stdout must be exactly the bundle path and the issue URL: %q", buf.String())

	data, err := os.ReadFile(lines[0])
	require.NoErrorf(t, err, "bundle file named on stdout must exist: %s", lines[0])

	var bundle RageBundle
	require.NoError(t, json.Unmarshal(data, &bundle))
	return bundle
}

func TestRageRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"rage"})
	require.NoError(t, err)
	assert.Same(t, rageCmd, found)
}

func TestRageRejectsIdentityFlag(t *testing.T) {
	resetRageFlags(t)
	t.Cleanup(func() { resetRageFlags(t) })
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	rootCmd.SetArgs([]string{"rage", "--store", t.TempDir(), "--identity", "rig:alpha"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

// TestRageBundleIsSingleFileWithConstantStdout covers exit_contract item 1:
// stdout is always exactly the bundle path plus the issue URL, regardless of
// how large the store or the debug log behind it is -- the bulk of the
// evidence lives in the bundle file on disk, never on stdout (NFR-2).
func TestRageBundleIsSingleFileWithConstantStdout(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 300; i++ {
		seedEntry(t, dir, fmt.Sprintf("global/e%04d.md", i),
			fmt.Sprintf("+++\nid = \"e%04d\"\ntitle = \"entry %d\"\nscope = []\n+++\nbody text for entry %d\n", i, i, i))
	}
	cmd, buf := newRageTestCmd(t, dir)

	logPath, err := obslog.LogPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o750))
	var logLines []string
	for i := 0; i < 2000; i++ {
		logLines = append(logLines, fmt.Sprintf(`{"kind":"context","n":%d,"filler":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}`, i))
	}
	require.NoError(t, os.WriteFile(logPath, []byte(strings.Join(logLines, "\n")+"\n"), 0o600))
	require.NoError(t, cmd.Flags().Set("max-log-bytes", "4096"))

	code, err := runRage(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	lines := rageStdoutLines(t, buf)
	require.Len(t, lines, 2)
	assert.Less(t, len(buf.String()), 600, "stdout must stay small regardless of store/log size")

	info, err := os.Stat(lines[0])
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(20000), "the bundle file itself must carry the bulk of the evidence")
}

// TestRageBundleVersionMatchesCairnVersion covers exit_contract item 2:
// version/commit/date must come from the exact same package-level vars
// printVersion's --json output serializes (cmd/version.go), not a
// re-derivation.
func TestRageBundleVersionMatchesCairnVersion(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Equal(t, VersionResult{Version: version, Commit: commit, Date: date}, bundle.Version)
}

// TestRageBundleStoreProvenanceFromFlag and TestRageBundleStoreProvenanceFromEnv
// cover exit_contract item 3's store-path half. The third source, "default"
// (neither --store nor $CAIRN_STORE set), can never reach runRage in real
// usage -- rootCmd's PersistentPreRunE hard-errors on an empty store path
// before any RunE body runs (rage is not in commandsExemptFromStoreGate) --
// so it is not exercised here.
func TestRageBundleStoreProvenanceFromFlag(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Equal(t, dir, bundle.StorePath)
	assert.Equal(t, "flag", bundle.StoreSource)
}

func TestRageBundleStoreProvenanceFromEnv(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, "")
	t.Setenv("CAIRN_STORE", dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Equal(t, dir, bundle.StorePath)
	assert.Equal(t, "env", bundle.StoreSource)
}

// TestRageBundleIdentityAlwaysDefault covers exit_contract item 3's identity
// half: TestRageRejectsIdentityFlag proves an explicit --identity/
// $CAIRN_IDENTITY is rejected outright before runRage ever executes, so
// "flag" and "env" identity provenance can never appear in a real rage
// bundle -- "default" is the only reachable case, and this is that case.
func TestRageBundleIdentityAlwaysDefault(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Equal(t, "default", bundle.IdentitySource)
	assert.Empty(t, bundle.IdentityTags)
}

// TestRageBundleIncludesDoctorFindingsAndStats covers exit_contract item 4.
func TestRageBundleIncludesDoctorFindingsAndStats(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/dup1.md", "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n")
	seedEntry(t, dir, "rig/alpha/dup2.md", "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)

	require.NotNil(t, bundle.Doctor)
	assert.Equal(t, 1, bundle.Doctor.ExitCode)
	found := false
	for _, f := range bundle.Doctor.Findings {
		if f.Category == cairn.CategoryDuplicateID {
			found = true
		}
	}
	assert.True(t, found, "duplicate_id finding must be present in the bundle's doctor report")
	assert.Equal(t, 1, bundle.Doctor.Stats.EntriesByTier["global"])
	assert.Equal(t, 1, bundle.Doctor.Stats.EntriesByTier["rig"])
}

// TestRageBundleLogTailEmptyFileNoCrash and TestRageBundleLogTailBoundedByBudget
// cover exit_contract item 5.
func TestRageBundleLogTailEmptyFileNoCrash(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)

	logPath, err := obslog.LogPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o750))
	require.NoError(t, os.WriteFile(logPath, []byte{}, 0o600))

	bundle := runRageForBundle(t, cmd, buf)
	assert.Empty(t, bundle.LogTail)
}

func TestRageBundleLogTailBoundedByBudget(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)

	logPath, err := obslog.LogPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o750))

	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf(`{"kind":"context","n":%d}`, i))
	}
	lines = append(lines, `{"kind":"context","n":"newest-marker"}`)
	require.NoError(t, os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	require.NoError(t, cmd.Flags().Set("max-log-bytes", "512"))

	bundle := runRageForBundle(t, cmd, buf)

	require.NotEmpty(t, bundle.LogTail)
	assert.Less(t, len(bundle.LogTail), 500, "the tail must be bounded well below the full log's line count")
	last := string(bundle.LogTail[len(bundle.LogTail)-1])
	assert.Contains(t, last, "newest-marker", "the newest line must never be starved by the byte budget")
}

// TestRageBundleReviewBranchesPresentAndNeverMails covers exit_contract item
// 6: review branch statuses are present, and -- the explicit test the AC
// demands, not just code review -- zero mail is ever sent by a rage run,
// even for a branch old enough to otherwise escalate.
func TestRageBundleReviewBranchesPresentAndNeverMails(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	e := commitReviewBranchAt(t, dir, "stale-topic", nil, time.Now().Add(-100*time.Hour))

	countFile := filepath.Join(t.TempDir(), "gc-calls")
	stubGCCountingCalls(t, countFile)

	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)

	require.Len(t, bundle.ReviewBranches, 1)
	f := bundle.ReviewBranches[0]
	assert.Equal(t, e.ID, f.EntryID)
	assert.Equal(t, "notify", f.Status, "no prior notify state exists, so even a very old branch downgrades to notify")
	assert.Equal(t, "mayor", f.Reviewer, "global tier's default reviewer must still be resolved and reported")
	assert.False(t, f.Notified, "rage must never actually mail a reviewer")
	assert.Equal(t, 0, readGCCallCount(t, countFile), "rage must send zero mail regardless of branch age")
}

// TestRageBundleExcludesBodiesByDefaultIncludesWithFlag covers exit_contract
// item 7.
func TestRageBundleExcludesBodiesByDefaultIncludesWithFlag(t *testing.T) {
	dir := t.TempDir()
	marker := "supersecret-body-marker-xyz123"
	seedEntry(t, dir, "global/a.md",
		fmt.Sprintf("+++\nid = \"a\"\ntitle = \"A\"\nscope = []\n+++\n%s\n", marker))

	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Empty(t, bundle.EntryBodies)
	raw, err := os.ReadFile(rageStdoutLines(t, buf)[0])
	require.NoError(t, err)
	assert.NotContains(t, string(raw), marker, "default bundle must contain zero entry body text")

	cmd2, buf2 := newRageTestCmd(t, dir)
	require.NoError(t, cmd2.Flags().Set("include-bodies", "true"))
	bundle2 := runRageForBundle(t, cmd2, buf2)
	require.Len(t, bundle2.EntryBodies, 1)
	assert.Equal(t, "a", bundle2.EntryBodies[0].EntryID)
	assert.Contains(t, bundle2.EntryBodies[0].Body, marker)
}

// TestRageBundleOmitsFailingCommandWhenNotSupplied and
// TestRageBundleRecordsFailingCommandAsUnverified cover exit_contract item 8.
func TestRageBundleOmitsFailingCommandWhenNotSupplied(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)
	bundle := runRageForBundle(t, cmd, buf)
	assert.Nil(t, bundle.FailingCommand)
}

func TestRageBundleRecordsFailingCommandAsUnverified(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := newRageTestCmd(t, dir)
	require.NoError(t, cmd.Flags().Set("command", "go test ./..."))
	require.NoError(t, cmd.Flags().Set("exit-code", "1"))

	bundle := runRageForBundle(t, cmd, buf)
	require.NotNil(t, bundle.FailingCommand)
	assert.Equal(t, "go test ./...", bundle.FailingCommand.Command)
	assert.Equal(t, 1, bundle.FailingCommand.ExitCode)
	assert.True(t, bundle.FailingCommand.Unverified)
}

// TestRageIssueURLEscapesSpecialCharacters covers exit_contract item 9: the
// issue URL must be built via net/url.Values.Encode(), never manual string
// concatenation, so a literal '&' in the store path can never inject a
// spurious extra query parameter.
func TestRageIssueURLEscapesSpecialCharacters(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "store & co")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	cmd, buf := newRageTestCmd(t, dir)
	code, err := runRage(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	lines := rageStdoutLines(t, buf)
	require.Len(t, lines, 2)
	issueURL := lines[1]

	parsed, err := url.Parse(issueURL)
	require.NoErrorf(t, err, "issue URL must be a well-formed URL: %s", issueURL)

	q := parsed.Query()
	assert.Len(t, q, 2, "an unescaped & in the store path must never inject extra query parameters")
	joined := q.Get("title") + " " + q.Get("body")
	assert.Contains(t, joined, dir, "the store path (with its literal &) must round-trip through URL decoding intact")
}

// TestRageSourceNeverImportsHTTPClient statically enforces exit_contract
// item 10 (NFR-4: no network I/O, ever): rage collects local evidence only
// and must never gain an accidental net/http dependency for, e.g.,
// "conveniently" auto-filing the issue it only ever prints a URL for.
func TestRageSourceNeverImportsHTTPClient(t *testing.T) {
	src, err := os.ReadFile("rage.go")
	require.NoError(t, err)
	for _, forbidden := range []string{`"net/http"`, "http.Get", "http.Post", "http.Client", "http.DefaultClient"} {
		assert.NotContains(t, string(src), forbidden, "rage.go must never perform network I/O (NFR-4)")
	}
}
