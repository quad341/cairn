package cmd

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRage executes "cairn rage --store <store>" (plus extraArgs) against the
// shared rootCmd, returning raw stdout. Unlike runStaleBranches's JSON
// decode, rage's stdout is plain text (a bundle path then an issue URL, per
// crn-n5yaz's stdout-stays-small acceptance criterion), so this returns the
// string unparsed -- readBundle below does the two-line split.
func runRage(t *testing.T, store string, extraArgs ...string) (string, error) {
	t.Helper()
	resetRageFlags(t)
	t.Cleanup(func() { resetRageFlags(t) })

	var out bytes.Buffer
	args := append([]string{"rage", "--store", store}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	return out.String(), err
}

// resetRageFlags clears rageCmd's own flags plus the shared --identity flag
// between tests, mirroring resetStaleBranchesFlags (branches_test.go).
func resetRageFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"include-bodies", "log-bytes", "failed-cmd", "exit-code"} {
		f := rageCmd.Flags().Lookup(name)
		require.NotNil(t, f, "rage must register --%s", name)
		require.NoError(t, f.Value.Set(f.DefValue))
		f.Changed = false
	}
	require.NoError(t, resetIdentityFlag())

	// storeFlag is a package-level var that pflag only overwrites when
	// --store is actually present in that invocation's args -- an omitted
	// flag leaves whatever a *prior* test last set. Reset it here (mirroring
	// resetIdentityFlag's own Changed reset) so a test that relies solely on
	// $CAIRN_STORE, like TestRageBundleReportsStoreSourceEnv, never inherits
	// another test's --store value.
	storeFlag = ""
	if f := rootCmd.PersistentFlags().Lookup("store"); f != nil {
		f.Changed = false
	}
}

// readBundle parses rage's stdout (bundle path, then issue URL -- exactly
// two lines) and returns the bundle file's own contents, failing the test if
// stdout isn't exactly two lines or the file can't be read.
func readBundle(t *testing.T, stdout string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "rage stdout must be exactly bundle path then URL:\n%s", stdout)
	assert.FileExists(t, lines[0])
	data, err := os.ReadFile(lines[0])
	require.NoError(t, err)
	return string(data)
}

func TestRageRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"rage"})
	require.NoError(t, err)
	assert.Same(t, rageCmd, found)
}

func TestRageRejectsIdentityFlag(t *testing.T) {
	dir := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	_, err := runRage(t, dir, "--identity", "rig:web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity")
}

func TestRageRejectsPositionalArgs(t *testing.T) {
	dir := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	_, err := runRage(t, dir, "extra-arg")
	require.Error(t, err)
}

func TestRageDefaultFlagValues(t *testing.T) {
	resetRageFlags(t)
	t.Cleanup(func() { resetRageFlags(t) })

	includeBodies, err := rageCmd.Flags().GetBool("include-bodies")
	require.NoError(t, err)
	assert.False(t, includeBodies)

	logBytes, err := rageCmd.Flags().GetInt("log-bytes")
	require.NoError(t, err)
	assert.Equal(t, 32768, logBytes)

	failedCmd, err := rageCmd.Flags().GetString("failed-cmd")
	require.NoError(t, err)
	assert.Equal(t, "", failedCmd)

	exitCode, err := rageCmd.Flags().GetInt("exit-code")
	require.NoError(t, err)
	assert.Equal(t, -1, exitCode, "0 is a valid real exit code, so the unset sentinel must not be 0")
}

// TestRageStdoutIsPathThenURLOnly covers crn-n5yaz's stdout acceptance
// criterion ("stdout on a successful run is small and constant regardless
// of store/log size -- just bundle path + URL") together with the "exactly
// one URL and one file path" half of the network-call criterion: it uses a
// store with an actual finding (not an empty store) so "regardless of...
// size" is meaningfully exercised, and parses line 2 as a real URL rather
// than substring-matching it.
func TestRageStdoutIsPathThenURLOnly(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/dup1.md", "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n")
	seedEntry(t, dir, "rig/alpha/dup2.md", "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2, "stdout must be exactly bundle path then URL:\n%s", out)
	assert.FileExists(t, lines[0])

	u, err := url.Parse(lines[1])
	require.NoError(t, err)
	assert.Equal(t, "github.com", u.Host)
	assert.Equal(t, "/quad341/cairn/issues/new", u.Path)
	assert.NotEmpty(t, u.Query().Get("title"))
	assert.NotEmpty(t, u.Query().Get("body"))
	assert.Contains(t, u.Query().Get("body"), lines[0], "the issue body must reference the bundle path, not embed the bundle's full contents")
}

// TestRageBundleContainsVersionInfo covers the version/commit/date
// acceptance criterion, comparing directly against the same package vars
// `cairn version --json` itself reads (version.go), not hardcoded literals.
func TestRageBundleContainsVersionInfo(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, version)
	assert.Contains(t, bundle, commit)
	assert.Contains(t, bundle, date)
}

func TestRageBundleReportsStoreSourceFlag(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "source: flag")
}

// TestRageBundleReportsStoreSourceEnv covers the env half of the
// store-provenance acceptance criterion; it can't go through runRage
// (which always passes --store explicitly), so it drives rootCmd directly.
func TestRageBundleReportsStoreSourceEnv(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_STORE", dir)

	resetRageFlags(t)
	t.Cleanup(func() { resetRageFlags(t) })
	var out bytes.Buffer
	rootCmd.SetArgs([]string{"rage"})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, rootCmd.Execute())

	bundle := readBundle(t, out.String())
	assert.Contains(t, bundle, "source: env")
}

// TestRageBundleReportsIdentitySourceDefault covers the identity half of the
// provenance acceptance criterion. Only "default" is reachable through a
// successful rage run: any explicit --identity/$CAIRN_IDENTITY is rejected
// outright (TestRageRejectsIdentityFlag), the same as doctor.
func TestRageBundleReportsIdentitySourceDefault(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("CAIRN_IDENTITY", "")

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "identity source: default")
}

// TestRageBundleContainsDoctorFindings covers crn-n5yaz's FR-4 acceptance
// criterion using the exact duplicate-id fixture TestRunDoctorFindingsPresentExitOne
// (doctor_test.go) already establishes produces a "duplicate_id" finding.
func TestRageBundleContainsDoctorFindings(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/dup1.md", "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n")
	seedEntry(t, dir, "rig/alpha/dup2.md", "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "duplicate_id")
	assert.Contains(t, bundle, "dup")
}

// TestRageBundleTailsLogToLastKBytesNeverWholeFile covers the log-tail
// acceptance criterion: given a log far larger than --log-bytes, the bundle
// must contain only the tail, never the earliest records, and never come up
// empty. The 500 synthetic lines total far more than the 2000-byte budget
// used here; OLDEST-MARKER-499 (near EOF) stays comfortably inside a
// 2000-byte tail even after PersistentPreRunE appends its own trailing
// context record, while OLDEST-MARKER-0 (~17KB back from EOF) cannot.
func TestRageBundleTailsLogToLastKBytesNeverWholeFile(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	logDir := filepath.Join(xdg, "cairn")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	lines := make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf(`{"kind":"marker","seq":%d,"tag":"OLDEST-MARKER-%d"}`, i, i))
	}
	logContent := strings.Join(lines, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "debug.jsonl"), []byte(logContent), 0o600))

	out, err := runRage(t, dir, "--log-bytes", "2000")
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.NotContains(t, bundle, `"OLDEST-MARKER-0"`, "must not contain the earliest record once truncated")
	assert.Contains(t, bundle, "OLDEST-MARKER-499", "must contain a record near the end of the log")
}

// TestRageBundleContainsShapeAndBranchStaleness covers the FR-7 acceptance
// criterion: per-tier counts across 2+ tiers, plus an open review branch's
// age/status. commitReviewBranchAt (branches_test.go) both creates the
// per-tier entry and commits it to its own review branch, so one call each
// covers both halves of this criterion. -30h crosses stale-branches's own
// default 24h notify threshold (branches.go) without reaching its 72h
// escalate threshold.
func TestRageBundleContainsShapeAndBranchStaleness(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	commitReviewBranchAt(t, dir, "global-topic", nil, time.Now())
	stale := commitReviewBranchAt(t, dir, "rig-topic", []string{"rig:alpha"}, time.Now().Add(-30*time.Hour))

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "global=1")
	assert.Contains(t, bundle, "rig=1")
	assert.Contains(t, bundle, stale.ID)
	assert.Contains(t, bundle, "notify")
}

// TestRageNeverCallsEvaluateBranchOrSendsMail covers Guardrail #1 directly:
// evaluateBranch (branches.go) has a real mail side effect for a
// notify-status branch, which rage must never trigger even though it reads
// the exact same review-branch data stale-branches does. stubGCCapturing
// only ever writes captureFile if something actually shells out to gc, so
// this is a positive, executable check rather than a code-review-only
// guardrail.
func TestRageNeverCallsEvaluateBranchOrSendsMail(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	commitReviewBranchAt(t, dir, "notify-topic", nil, time.Now().Add(-30*time.Hour))

	captureFile := filepath.Join(t.TempDir(), "gc-capture")
	stubGCCapturing(t, captureFile)

	_, err := runRage(t, dir)
	require.NoError(t, err)

	_, statErr := os.Stat(captureFile)
	assert.True(t, os.IsNotExist(statErr), "rage must never invoke gc -- a notify-status branch would trigger evaluateBranch's mail step if rage called it")
}

// TestRageAutoDetectsFailedCommandFromLogTail covers FR-8's auto-detect
// half: a real prior failing invocation (driven through executeAndExit, not
// hand-crafted JSON, so its exit record has the exact shape logCommandExit
// actually produces) must surface in the bundle labeled "auto-detected from
// log".
func TestRageAutoDetectsFailedCommandFromLogTail(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	resetRootFlagsForTest(t)
	prevStore := storeFlag
	storeFlag = ""
	t.Setenv("CAIRN_STORE", "")
	rootCmd.SetArgs([]string{"status"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.Equal(t, 1, executeAndExit())
	storeFlag = prevStore

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "auto-detected from log")
	assert.Contains(t, bundle, "cairn status")
}

// TestRageExplicitFailedCmdOverridesAutoDetection covers FR-8's override
// half: with a real auto-detectable failure already in the log tail, an
// explicit --failed-cmd/--exit-code must still win, verbatim, and the
// bundle must not claim auto-detection was used.
func TestRageExplicitFailedCmdOverridesAutoDetection(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	resetRootFlagsForTest(t)
	prevStore := storeFlag
	storeFlag = ""
	t.Setenv("CAIRN_STORE", "")
	rootCmd.SetArgs([]string{"status"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.Equal(t, 1, executeAndExit())
	storeFlag = prevStore

	out, err := runRage(t, dir, "--failed-cmd", "cairn synthetic-cmd", "--exit-code", "7")
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "explicit")
	assert.Contains(t, bundle, "cairn synthetic-cmd")
	assert.Contains(t, bundle, "7")
	assert.NotContains(t, bundle, "auto-detected")
}

// TestRageNoFailedCommandFoundStatesPlainly covers FR-8's "never fabricate"
// clause: no explicit override and nothing auto-detectable (the only log
// content is this very invocation's own unconditional context record, which
// is not an exit record) must state plainly that nothing was found.
func TestRageNoFailedCommandFoundStatesPlainly(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "none found")
}

// TestRageExcludesBodiesByDefault and TestRageIncludeBodiesFlagEmbedsBodyText
// cover FR-9: entry body text must never appear in the bundle unless
// --include-bodies is explicitly passed.
func TestRageExcludesBodiesByDefault(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nMARKER-BODY-TEXT\n")
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir)
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.NotContains(t, bundle, "MARKER-BODY-TEXT")
}

func TestRageIncludeBodiesFlagEmbedsBodyText(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/g.md", "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nMARKER-BODY-TEXT\n")
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	out, err := runRage(t, dir, "--include-bodies")
	require.NoError(t, err)
	bundle := readBundle(t, out)

	assert.Contains(t, bundle, "MARKER-BODY-TEXT")
}
