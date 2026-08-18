package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filesAnchorEntry renders a minimal files-anchor entry as TOML+markdown, a
// cmd-package-local copy of internal/cairn/sweep_test.go's unexported
// filesEntry helper (not reachable from this package).
func filesAnchorEntry(id, repo, path, fingerprint string) string {
	return fmt.Sprintf(
		"+++\nid = %q\ntitle = %q\nscope = []\n\n[anchor]\ntype = \"files\"\nrepo = %q\npaths = [%q]\nfingerprint = %q\n+++\nbody\n",
		id, id, repo, path, fingerprint,
	)
}

// resetDoctorFlags clears doctorCmd's and doctorExplainCmd's own --json flags
// plus the shared --identity flag, since rootCmd and its subcommands are
// package-level singletons whose pflag state otherwise leaks across tests
// (mirrors resetPromoteMarkFlags/resetIdentityFlag for this command's flags).
func resetDoctorFlags(t *testing.T) {
	t.Helper()
	for _, c := range []*cobra.Command{doctorCmd, doctorExplainCmd} {
		f := c.Flags().Lookup("json")
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set("false"))
		f.Changed = false
	}
	require.NoError(t, resetIdentityFlag())
}

// newDoctorTestCmd points storePath() at dir and returns doctorCmd wired to a
// fresh output buffer, for driving runDoctor directly (bypassing rootCmd.
// Execute()/os.Exit entirely) so all three of FR-9's exit-code outcomes are
// directly observable in-process.
func newDoctorTestCmd(t *testing.T, dir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	resetDoctorFlags(t)
	t.Cleanup(func() { resetDoctorFlags(t) })

	prevStore := storeFlag
	storeFlag = dir
	t.Cleanup(func() { storeFlag = prevStore })

	var buf bytes.Buffer
	doctorCmd.SetOut(&buf)
	doctorCmd.SetErr(&buf)
	// A bare cobra.Command's Context() is nil (only Execute()'s own call
	// chain fills it in), and Diagnose threads it all the way down to
	// os/exec.CommandContext, which panics on a nil context -- so a direct,
	// non-Execute() call to runDoctor must set one explicitly.
	doctorCmd.SetContext(t.Context())
	// All three of SetContext/SetOut/SetErr must be reset back to nil once
	// this test ends, not just left to the next test's overwrite: cobra's
	// getOut/getErr/ExecuteC dispatch only inherit from the parent command
	// when the child's own field is nil (command.go's getOut/getErr walk
	// the parent chain only on a nil check; ExecuteC's `if cmd.ctx == nil`
	// is the same guard for context). Since doctorExplainCmd is nested
	// under doctorCmd (not rootCmd directly), a stale non-nil writer left
	// here also swallows output for every later rootCmd.Execute()-based
	// test that dispatches through doctorCmd, including its explain child
	// — not just tests that reuse doctorCmd directly. Concretely: this bit
	// us as a silently-killed test binary (canceled context reached
	// os/exec.CommandContext, Diagnose errored, doctorCmd.RunE's real
	// os.Exit(2) fired) followed by empty-buffer assertion failures once
	// the context leak alone was fixed.
	t.Cleanup(func() {
		doctorCmd.SetContext(nil) //nolint:staticcheck // deliberate: restores cobra's own "never set" zero value, not a real call site
		doctorCmd.SetOut(nil)
		doctorCmd.SetErr(nil)
	})
	return doctorCmd, &buf
}

// seedPrewarmedCleanStore builds a store with exactly one entry backed by a
// genuinely fresh files anchor, then pre-builds the index. Diagnose's
// index_drift check (via IndexStale) legitimately reports stale for an index
// that was never built at all, not just one behind HEAD -- so a "clean"
// fixture must warm the index first, mirroring a store some earlier cairn
// command already touched.
func seedPrewarmedCleanStore(t *testing.T) string {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	repo := t.TempDir()
	gitInit(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o600))
	gitCommitAll(t, repo, "init")

	fp, err := cairn.ComputeFingerprint(ctx, cairn.Anchor{Type: "files", Repo: repo, Paths: []string{"a.go"}})
	require.NoError(t, err)
	require.NotEmpty(t, fp)
	seedEntry(t, dir, "global/fresh.md", filesAnchorEntry("fresh-1", repo, "a.go", fp))

	_, err = cairn.Reindex(ctx, dir)
	require.NoError(t, err)
	return dir
}

func TestRunDoctorCleanStoreExitZero(t *testing.T) {
	dir := seedPrewarmedCleanStore(t)
	cmd, buf := newDoctorTestCmd(t, dir)

	code, err := runDoctor(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, buf.String(), "no findings")
}

func TestRunDoctorFindingsPresentExitOne(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/dup1.md", "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n")
	seedEntry(t, dir, "rig/alpha/dup2.md", "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	cmd, buf := newDoctorTestCmd(t, dir)

	code, err := runDoctor(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	assert.Contains(t, buf.String(), "duplicate_id")
}

func TestRunDoctorDiagnoseErrorExitTwo(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "global")
	require.NoError(t, os.Mkdir(blocked, 0o755)) //nolint:gosec // must be traversable before being locked to 0o000 below
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) }) //nolint:gosec // restore traversal so t.TempDir() cleanup can remove it

	cmd, _ := newDoctorTestCmd(t, dir)
	code, err := runDoctor(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, 2, code)
}

func TestRunDoctorJSONReportsExitCodeAndFindings(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/dup1.md", "+++\nid = \"dup\"\ntitle = \"one\"\nscope = []\n+++\nx\n")
	seedEntry(t, dir, "rig/alpha/dup2.md", "+++\nid = \"dup\"\ntitle = \"two\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	cmd, buf := newDoctorTestCmd(t, dir)
	require.NoError(t, cmd.Flags().Set("json", "true"))

	code, err := runDoctor(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, code)

	var report cairn.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))
	assert.Equal(t, 1, report.ExitCode)
	assert.NotEmpty(t, report.Findings)
}

// TestDoctorRejectsIdentityFlag drives the identity-rejection branch through
// the real rootCmd.Execute() path (safe: this branch returns a plain error
// via Cobra's normal flow and never reaches doctorCmd.RunE's os.Exit calls).
func TestDoctorRejectsIdentityFlag(t *testing.T) {
	resetDoctorFlags(t)
	t.Cleanup(func() { resetDoctorFlags(t) })

	rootCmd.SetArgs([]string{"doctor", "--store", t.TempDir(), "--identity", "rig:alpha"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not filter by identity")
}

// TestDoctorCleanStoreExitsZeroThroughFullWiring is a wiring smoke test
// confirming doctorCmd is correctly registered on rootCmd end-to-end. Only
// the exit-0 path is safe to drive through the real RunE/Execute(): any
// non-zero outcome calls os.Exit for real and would kill the test process,
// so those paths are covered directly via runDoctor above instead.
func TestDoctorCleanStoreExitsZeroThroughFullWiring(t *testing.T) {
	resetDoctorFlags(t)
	t.Cleanup(func() { resetDoctorFlags(t) })

	dir := seedPrewarmedCleanStore(t)

	var buf bytes.Buffer
	rootCmd.SetArgs([]string{"doctor", "--store", dir})
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, buf.String(), "no findings")
}

// runDoctorExplain executes "cairn doctor explain <entry-id>" (plus any extra
// args) against the shared rootCmd and returns everything written via
// cmd.OutOrStdout(). doctorExplainCmd renders through that (not
// fmt.Println), so — unlike execRoot/captureStdout elsewhere in this
// package — capturing the real SetOut buffer is both necessary and
// sufficient here, and never calls os.Exit itself, so any outcome is safe to
// drive through the real Execute() path.
func runDoctorExplain(t *testing.T, dir, entryID string, extraArgs ...string) (string, error) {
	t.Helper()
	resetDoctorFlags(t)
	t.Cleanup(func() { resetDoctorFlags(t) })

	var buf bytes.Buffer
	args := append([]string{"doctor", "explain", entryID, "--store", dir}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	err := rootCmd.Execute()
	return buf.String(), err
}

const (
	explainLessSpecific = "+++\nid = \"s1\"\ntitle = \"Less specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nbody\n"
	explainMoreSpecific = "+++\nid = \"s2\"\ntitle = \"More specific\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nbody\n"
)

func TestDoctorExplainStoreWideReportsShadow(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	out, err := runDoctorExplain(t, dir, "s1")
	require.NoError(t, err)
	assert.Contains(t, out, "shadowed by s2")
	assert.Contains(t, out, "scope_size")
}

func TestDoctorExplainUnknownIDErrors(t *testing.T) {
	_, err := runDoctorExplain(t, t.TempDir(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

// TestDoctorExplainWithIdentityFiltersCandidates confirms --identity engages
// ExplainForIdentity (not ExplainEntry): an identity lacking role:investigator
// cannot see s2 at all, so it must resolve to s1 uncontested, unlike the
// store-wide answer TestDoctorExplainStoreWideReportsShadow asserts.
func TestDoctorExplainWithIdentityFiltersCandidates(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	out, err := runDoctorExplain(t, dir, "shared", "--identity", "rig:alpha")
	require.NoError(t, err)
	assert.Contains(t, out, "winner: s1")
	assert.Contains(t, out, "does not satisfy")
}

// TestDoctorExplainRejectsMalformedEnvIdentity confirms doctor explain
// validates $CAIRN_IDENTITY the same way list/map/prime/remember do.
// $CAIRN_IDENTITY is split on whitespace (identity.go's identityWithSource),
// so a natural comma-joined value like "rig:alpha,role:investigator"
// produces one malformed tag containing a literal comma. Before this fix,
// doctorExplainCmd resolved identity via the unvalidated resolveIdentity and
// that malformed tag flowed straight into scope resolution, silently
// matching nothing ("winner: none") instead of erroring — indistinguishable
// from a genuinely unrelated identity. Must go through the env var, not
// --identity: that flag is a pflag StringSlice and cobra comma-splits it
// before this code ever sees a single string, so it cannot reproduce this.
func TestDoctorExplainRejectsMalformedEnvIdentity(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	t.Setenv("CAIRN_IDENTITY", "rig:alpha,role:investigator")

	_, err := runDoctorExplain(t, dir, "shared")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid identity tag")
}

// TestDoctorExplainAcceptsCommaIdentityFlag is the regression guard for
// TestDoctorExplainRejectsMalformedEnvIdentity's fix: cobra comma-splits a
// StringSlice flag before resolveIdentityValidated ever sees it, so
// "rig:alpha,role:investigator" passed via --identity must keep resolving
// as the two valid tags ["rig:alpha", "role:investigator"], not get
// rejected the way the equivalent env var value now is.
func TestDoctorExplainAcceptsCommaIdentityFlag(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	out, err := runDoctorExplain(t, dir, "shared", "--identity", "rig:alpha,role:investigator")
	require.NoError(t, err)
	assert.Contains(t, out, "winner: s2")
}

// TestDoctorExplainJSONRendersInvalidInputForMalformedIdentity is the
// end-to-end counterpart to TestDoctorExplainRejectsMalformedEnvIdentity:
// it proves the malformed-identity error is not just returned but actually
// rendered through emitError's --json envelope, the same way
// TestEmitErrorWritesEnvelopeWithJSON (cmd/format_test.go) proves emitError
// itself renders a CategoryInvalidInput error. Composition of those two
// facts implies this outcome, but nothing previously exercised doctor
// explain's --json output on this error path directly.
func TestDoctorExplainJSONRendersInvalidInputForMalformedIdentity(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	t.Setenv("CAIRN_IDENTITY", "rig:alpha,role:investigator")

	out, err := runDoctorExplain(t, dir, "shared", "--json")
	require.Error(t, err)
	assert.Contains(t, out, `"category": "invalid_input"`)
}

func TestDoctorExplainJSONOutput(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "rig/alpha/s1.md", explainLessSpecific)
	seedEntry(t, dir, "role/investigator/s2.md", explainMoreSpecific)

	out, err := runDoctorExplain(t, dir, "s1", "--json")
	require.NoError(t, err)

	var res cairn.ExplainResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "s2", res.ShadowedByID)
	assert.Equal(t, "scope_size", res.Rule)
}
