package cmd

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRemember executes "cairn remember" (plus extraArgs) against the shared
// rootCmd, stubbing gc to always succeed (stubGC). See runRememberWithGC for
// the full mechanics; this is the always-succeeding common case nearly every
// test in this file wants.
func runRemember(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	return runRememberWithGC(t, stubGC, extraArgs...)
}

// runRememberWithGC is runRemember parameterized on the gc stub, so a test
// can exercise a shared-tier remember call's reviewer-mail failure path
// (stubGCFail, crn-419.4 AC4) instead of always wiring up the succeeding
// one. rootCmd/rememberCmd are package-level singletons, so pflag state
// otherwise leaks across tests in this binary: resetRememberFlags clears
// --topic/--scope (this file's own flags) and the inherited --identity flag
// before and after every call. --identity is a StringSlice; commands_test.go's
// runStatus only resets its Changed bit, not its underlying value, so a prior
// test's "--identity rig:alpha" would otherwise leak into every test here that
// relies on identity defaulting. Replace (not Set) is used to clear it because
// stringSliceValue.Set treats a repeat call as an append, not a replace.
// Returns the temp store dir passed via --store, so callers can assert zero
// filesystem writes.
//
// The store is git-initialized before the command runs: a private-tier
// (agent/) remember now commits straight to the store's current branch
// (crn-419.3), so a plain non-git t.TempDir() would fail that step even on
// otherwise-valid input.
func runRememberWithGC(t *testing.T, stub func(*testing.T), extraArgs ...string) (string, error) {
	t.Helper()
	resetRememberFlags(t)
	t.Cleanup(func() { resetRememberFlags(t) })

	store := t.TempDir()
	gitInit(t, store)
	stub(t)
	args := append([]string{"remember", "--store", store}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return store, rootCmd.Execute()
}

// gitInit turns dir into a git repo with a resolvable HEAD -- an empty
// initial commit, not just `git init`, since a shared-tier remember call's
// review branch is created via `git worktree add -b branch wt HEAD`, which
// needs HEAD to already resolve before Create ever writes the entry's first
// file. Same setup as internal/cairn/freshness_test.go's gitInit
// (commit.gpgsign=false so a test commit never blocks on a signing key),
// duplicated locally since that helper is unexported in a different package.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
}

// gitOutput runs git -C dir args... and returns combined stdout+stderr,
// failing the test on a non-zero exit.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return string(out)
}

// stubGC shadows the real gc binary on PATH with a stub that always
// succeeds, and pins GC_RIG to a fixed value, so a shared-tier remember
// call's reviewer-mail step resolves and "sends" deterministically without
// ever reaching a real fleet mail system: this test binary may itself be
// running inside a real gc rig, where GC_RIG is already set and a real gc
// is already on PATH.
func stubGC(t *testing.T) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\nexit 0\n")
}

// stubGCFail is stubGC's mirror image: the stubbed gc binary always fails
// (exit 1), so a shared-tier remember call's reviewer-mail step
// (requestReview's sendReviewMail) fails deterministically -- after the
// entry has already been committed to its review branch, since
// CommitToReviewBranch runs first. Covers crn-419.4 AC4 (crn-kbf): that
// failure must not roll back the already-durable review-branch commit, and
// must be reported clearly.
func stubGCFail(t *testing.T) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\nexit 1\n")
}

// writeStubGC shadows the real gc binary on PATH with a stub running script,
// and pins GC_RIG to a fixed value so tier-default reviewer resolution
// (defaultReviewer) is deterministic regardless of the real environment this
// test binary happens to run in.
func writeStubGC(t *testing.T, script string) {
	t.Helper()
	t.Setenv("GC_RIG", "test-rig")

	dir := t.TempDir()
	path := filepath.Join(dir, "gc")
	//nolint:gosec // must be executable to stand in for the gc binary on PATH
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubGCCapturing is stubGC's content-observing sibling: the stubbed gc
// binary still exits 0, but first records its own invocation argv to
// captureFile, one base64-encoded line per argument (readStubGCArgs decodes
// it back). Plain newline-per-argument would corrupt the recorded body
// argument, which contains its own embedded blank lines; base64 -w0 never
// emits an embedded newline, so splitting the capture file on "\n" is always
// safe regardless of what an argument itself contains.
func stubGCCapturing(t *testing.T, captureFile string) {
	t.Helper()
	writeStubGC(t, "#!/bin/sh\n"+
		"for a in \"$@\"; do printf '%s' \"$a\" | base64 -w0; printf '\\n'; done > "+shellQuote(captureFile)+"\n"+
		"exit 0\n")
}

// shellQuote wraps s in single quotes for safe interpolation into a /bin/sh
// script body, escaping any embedded single quote. captureFile is always a
// t.TempDir() path in practice (never contains one), but this keeps the stub
// script's construction from silently depending on that.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// readStubGCArgs reads back a stubGCCapturing invocation's recorded argv,
// base64-decoding each line to recover the exact original argument bytes.
func readStubGCArgs(t *testing.T, captureFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(captureFile)
	require.NoError(t, err, "the gc stub must have run and recorded its invocation")
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	args := make([]string, len(lines))
	for i, l := range lines {
		decoded, err := base64.StdEncoding.DecodeString(l)
		require.NoError(t, err, "line %d (%q) must be valid base64", i, l)
		args[i] = string(decoded)
	}
	return args
}

func resetRememberFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"topic", "scope"} {
		f := rememberCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(""))
		f.Changed = false
	}

	idf := rootCmd.PersistentFlags().Lookup("identity")
	require.NotNil(t, idf)
	sv, ok := idf.Value.(pflag.SliceValue)
	require.True(t, ok, "identity flag must implement pflag.SliceValue")
	require.NoError(t, sv.Replace(nil))
	idf.Changed = false
}

// assertNoFilesWritten requires that a rejected remember call wrote nothing
// under store, ignoring the .git directory that gitInit itself creates.
func assertNoFilesWritten(t *testing.T, store string) {
	t.Helper()
	entries, err := os.ReadDir(store)
	require.NoError(t, err)
	var written []string
	for _, e := range entries {
		if e.Name() != ".git" {
			written = append(written, e.Name())
		}
	}
	assert.Empty(t, written, "a rejected remember call must not write anything under the store")
}

// requireSingleEntry requires exactly one file under dir and reads it back
// through cairn.ParseEntry -- the same round-trip AC#3 requires of the
// written file.
func requireSingleEntry(t *testing.T, dir string) *cairn.Entry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one file written under %s", dir)
	e, err := cairn.ParseEntry(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)
	return e
}

// unicodeDotTrickCorpus returns crn-419.5 AC1's "unicode dot tricks" corpus,
// shared between the topic and scope variants below: non-ASCII characters
// that read as multiple dots, or a literal ".." hidden behind a zero-width
// character, meant to disguise a dot-based traversal attempt from a checker
// that only understands ASCII '.'.
func unicodeDotTrickCorpus() map[string]string {
	return map[string]string{
		"doubled fullwidth full stop (U+FF0E)":   "\uFF0E\uFF0E",
		"doubled one-dot leader (U+2024)":        "\u2024\u2024",
		"two-dot leader (U+2025)":                "\u2025",
		"horizontal ellipsis (U+2026)":           "\u2026",
		"doubled ideographic full stop (U+3002)": "\u3002\u3002",
		"dot-dot split by a zero-width space":    "foo.\u200B.bar",
	}
}

func TestRememberRejectsAttackTopics(t *testing.T) {
	attacks := map[string]string{
		"path traversal": "../../etc/passwd",
		"absolute path":  "/etc/passwd",
		"leading dot":    ".hidden",
		"embedded NUL":   "foo\x00bar",
	}
	for name, topic := range attacks {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", topic, "a body")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--topic")
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberRejectsAttackScopes(t *testing.T) {
	attacks := map[string]string{
		"path traversal": "../../etc/passwd",
		"absolute path":  "/etc/passwd",
		"leading dot":    ".hidden",
		"embedded NUL":   "foo\x00bar",
	}
	for name, tag := range attacks {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", "valid-topic", "--scope", tag, "a body")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "scope tag")
			assertNoFilesWritten(t, store)
		})
	}
}

// TestRememberRejectsUnicodeDotTrickTopics covers crn-419.5 AC1's "unicode
// dot tricks" corpus entry for --topic at the CLI level. Kept separate from
// TestRememberRejectsAttackTopics (which asserts the error names "--topic")
// because these values currently pass validation, so without an explicit
// --scope the run would fail for an unrelated reason (no --scope given and
// no identity is set in this test), masking the real gap behind the wrong
// error message. Supplying a valid --scope here means that if
// ValidatePathSegment ever accepts one of these disguised values, the entry
// actually lands on disk and assertNoFilesWritten catches it directly.
func TestRememberRejectsUnicodeDotTrickTopics(t *testing.T) {
	for name, topic := range unicodeDotTrickCorpus() {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", topic, "--scope", "agent:test", "a body")
			require.Error(t, err, "%q must be rejected as a topic_key, not written as a real entry", topic)
			assertNoFilesWritten(t, store)
		})
	}
}

// TestRememberRejectsUnicodeDotTrickScopes is
// TestRememberRejectsUnicodeDotTrickTopics' scope-tag counterpart. Each
// corpus value is placed after a real "agent:" tier prefix rather than bare:
// only the value after a recognized rig:/role:/agent: prefix ever becomes a
// directory name (scopeDir/ResolvedTier) -- a bare tag with no such prefix
// resolves to the fixed "global" directory regardless of its own content, so
// testing a bare value here would exercise validation only, not the actual
// path-construction risk AC1 is about.
func TestRememberRejectsUnicodeDotTrickScopes(t *testing.T) {
	for name, trick := range unicodeDotTrickCorpus() {
		tag := "agent:" + trick
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", "valid-topic", "--scope", tag, "a body")
			require.Error(t, err, "%q must be rejected as a scope tag, not written as a real entry", tag)
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberAcceptsEmptyTopic(t *testing.T) {
	cases := map[string][]string{
		"omitted":        {"--scope", "agent:test", "a body"},
		"explicit empty": {"--topic", "", "--scope", "agent:test", "a body"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, args...)
			require.NoError(t, err, "--topic is documented as an optional freeform hint (DESIGN.md §6), not a required field")
			e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
			assert.Equal(t, "", e.TopicKey)
		})
	}
}

func TestRememberRequiresExactlyOneBodyArg(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic")
	require.Error(t, err, "a missing body argument must be rejected")
	assertNoFilesWritten(t, store)
}

func TestRememberValidInputWritesEntry(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "valid-topic", e.TopicKey)
	assert.Equal(t, []string{"agent:test"}, e.Scope)
	assert.Equal(t, "a body", e.Body)
}

func TestRememberDefaultScopeUsesResolvedIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha agent:bot")
	store, err := runRemember(t, "--topic", "valid-topic", "a body")
	require.NoError(t, err, "a valid identity-derived scope must pass validation")
	e := requireSingleEntry(t, filepath.Join(store, "agent", "bot"))
	assert.Equal(t, []string{"agent:bot"}, e.Scope, "default scope must collapse to the agent: tag, not the full identity")
}

func TestRememberDefaultScopeValidatesResolvedIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "agent:../evil")
	store, err := runRemember(t, "--topic", "valid-topic", "a body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope tag", "an unsafe identity-derived scope tag must be rejected, not silently used")
	assertNoFilesWritten(t, store)
}

func TestRememberDefaultScopeRequiresAgentTag(t *testing.T) {
	cases := map[string]string{
		"no identity at all":             "",
		"identity without an agent: tag": "rig:alpha role:reviewer",
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CAIRN_IDENTITY", identity)
			store, err := runRemember(t, "--topic", "valid-topic", "a body")
			require.Error(t, err, "an identity that can't resolve to a single private tag must not silently proceed")
			assertNoFilesWritten(t, store)
		})
	}
}

func TestRememberExplicitScopeOverridesIdentity(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha")
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "role:reviewer,agent:bot", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))
	assert.Equal(t, []string{"role:reviewer", "agent:bot"}, e.Scope,
		"an explicit --scope must override the identity-derived default, not merge with it")
}

// TestRememberWritesUnderEachScopeTier covers AC#2: a single-tag scope for
// each of rig:/role:/agent: lands under that tier's own directory (the
// global/ tier -- an empty scope -- has no reachable path through this CLI,
// since rememberScope always defaults to a single agent: tag when --scope is
// omitted; it's covered directly at the cairn.NewEntry/Create level instead,
// see internal/cairn/remember_test.go).
func TestRememberWritesUnderEachScopeTier(t *testing.T) {
	cases := []struct {
		tag        string
		tierDir    string
		subdirName string
	}{
		{"rig:web", "rig", "web"},
		{"role:reviewer", "role", "reviewer"},
		{"agent:bot", "agent", "bot"},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			store, err := runRemember(t, "--topic", "valid-topic", "--scope", tc.tag, "a body")
			require.NoError(t, err)
			e := requireSingleEntry(t, filepath.Join(store, tc.tierDir, tc.subdirName))
			assert.Equal(t, []string{tc.tag}, e.Scope)
		})
	}
}

// TestRememberPrivateTierCommitsDirectlyAndReportsSHA covers crn-419.3's CLI
// wiring: a private-tier (agent/) remember call must commit the entry to the
// store's current branch and print the resulting SHA as a second line, after
// the entry id. The underlying CommitDirect logic (exactly one new commit,
// containing only the entry file, no branch created) is already exhaustively
// covered at the internal/cairn level -- this only proves RunE actually calls
// it and reports what it returns.
func TestRememberPrivateTierCommitsDirectlyAndReportsSHA(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "a body")
	})
	require.NoError(t, runErr)

	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	head := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))

	lines := strings.Fields(strings.TrimSpace(stdout))
	require.Len(t, lines, 2, "a private-tier remember must print the entry id then the commit SHA")
	assert.Equal(t, e.ID, lines[0])
	assert.Equal(t, head, lines[1])

	log := strings.TrimSpace(gitOutput(t, store, "log", "--oneline"))
	assert.Len(t, strings.Split(log, "\n"), 2, "exactly one new commit must land on top of gitInit's initial commit")
}

// TestRememberNonPrivateTierDoesNotCommit covers the other side of the same
// wiring: a shared-tier (rig:/role:) remember call writes the entry but must
// not commit it to the store's own branch -- that tier's DESIGN.md §7 flow is
// propose-on-a-review-branch (crn-419.4's requestReview), never a direct
// commit.
func TestRememberNonPrivateTierDoesNotCommit(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "rig:web", "a body")
	})
	require.NoError(t, runErr)

	requireSingleEntry(t, filepath.Join(store, "rig", "web"))

	status := gitOutput(t, store, "status", "--porcelain")
	assert.Contains(t, status, "??", "a shared-tier entry must be left untracked on the store's own branch, not auto-committed")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 3, "a non-private-tier remember must print the entry id, the review branch, and the mailed reviewer -- no commit SHA")
	assert.NotContains(t, lines[0], "/", "the first line must be the bare entry id, not a branch or reviewer address")
}

// TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError covers
// crn-419.4 AC4 (crn-kbf): a shared-tier remember call whose reviewer-mail
// step fails must not roll back the review-branch commit -- the entry is
// already durably committed to remember/<id> by the time mail could fail
// (CommitToReviewBranch runs before sendReviewMail in requestReview, and
// there is no rollback logic for a later step's failure, by design -- see
// cmd/reviewer.go). The returned error must name both the branch and the
// mail failure, so an operator isn't left guessing whether the entry landed
// anywhere. Mirrors internal/cairn's
// TestCommitDirectFailureLeavesEntryUncommittedAndReportsError one commit
// earlier in this stack (crn-419.3): force the failure, then assert the
// already-durable state survives it and is reported.
func TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, stubGCFail, "--topic", "valid-topic", "--scope", "rig:web", "a body")
	})
	require.Error(t, runErr, "a failed reviewer-mail step must surface as a command error (and thus a non-zero process exit via cmd/root.go), not be swallowed")

	e := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	branch := "remember/" + e.ID

	assert.Contains(t, runErr.Error(), branch, "the error must name the review branch the entry already landed on")
	assert.Contains(t, runErr.Error(), "mail", "the error must make clear the mail step is what failed")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "the id and review-branch lines print before the mail step fails; no third 'mailed reviewer' line follows")
	assert.Equal(t, e.ID, lines[0])
	assert.Equal(t, "review branch: "+branch, lines[1])

	// gitOutput's own require.NoErrorf is the assertion here: if the review
	// branch didn't survive the mail failure, "rev-parse --verify" fails and
	// the test fails with git's own error text.
	gitOutput(t, store, "rev-parse", "--verify", branch)
}

func TestRememberRegisteredOnRootCmd(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"remember"})
	require.NoError(t, err)
	assert.Same(t, rememberCmd, found)
}

// TestDefaultScopeCollapsesToSingleAgentTag proves the actual defect: a
// multi-tag identity spanning rig/role/agent must collapse to exactly the
// agent:<id> tag, not pass through as the full tag set (which doesn't map to
// any single DESIGN.md §2 scope directory).
func TestDefaultScopeCollapsesToSingleAgentTag(t *testing.T) {
	scope, err := defaultScope([]string{"rig:alpha", "role:reviewer", "agent:bot"})
	require.NoError(t, err)
	assert.Equal(t, []string{"agent:bot"}, scope)
}

func TestDefaultScopeErrorsWithoutAgentTag(t *testing.T) {
	cases := map[string][]string{
		"no agent: tag present": {"rig:alpha", "role:reviewer"},
		"empty identity":        nil,
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			scope, err := defaultScope(identity)
			require.Error(t, err)
			assert.Nil(t, scope)
		})
	}
}

// TestRememberSharedTierMailInvokedWithExpectedRecipientAndContent covers
// crn-419.5 AC4's "the mail-send call is invoked with the expected recipient
// and content, mocked at the interface boundary": every other shared-tier
// test only checks that the gc stub exited 0 or 1, never what it was
// actually invoked with. This captures the real argv sendReviewMail passes
// to `gc mail send` and asserts the recipient, subject, and body match its
// known construction (cmd/reviewer.go).
func TestRememberSharedTierMailInvokedWithExpectedRecipientAndContent(t *testing.T) {
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	var store string
	var runErr error
	captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, func(t *testing.T) {
			t.Helper()
			stubGCCapturing(t, captureFile)
		}, "--topic", "valid-topic", "--scope", "rig:web", "--reviewer", "custom-reviewer", "a body")
	})
	require.NoError(t, runErr)

	e := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	branch := "remember/" + e.ID

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, []string{"mail", "send", "custom-reviewer", "-s"}, args[:4],
		"the --reviewer flag's value must be passed through verbatim as the mail recipient")
	assert.Contains(t, args[4], e.TopicKey, "the subject must name the entry's topic")
	assert.Equal(t, "-m", args[5])
	assert.Contains(t, args[6], e.ID, "the mail body must name the entry id")
	assert.Contains(t, args[6], branch, "the mail body must name the review branch")
	assert.Contains(t, args[6], "rig:web", "the mail body must name the entry's scope")
}

// TestRememberCLIRoundTripAllFields covers AC2 through the actual `cairn
// remember` command, not cairn.NewEntry/Create called directly (already
// covered exhaustively at that level by TestEntryCreateRoundTrip in
// internal/cairn/remember_test.go): every field the CLI layer itself is
// responsible for populating -- including created_by, wired from
// resolveIdentity(cmd), which no existing CLI-level test asserts either way
// -- survives a real invocation and reads back via cairn.ParseEntry.
func TestRememberCLIRoundTripAllFields(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha agent:bot")
	store, err := runRemember(t, "--topic", "build-flags", "prefer feature flags over env vars")
	require.NoError(t, err)

	e := requireSingleEntry(t, filepath.Join(store, "agent", "bot"))
	assert.True(t, strings.HasPrefix(e.ID, "build-flags-"), "id must be derived from topic_key")
	assert.Equal(t, "build-flags", e.TopicKey)
	assert.Equal(t, []string{"agent:bot"}, e.Scope, "default scope must collapse to the agent: tag")
	assert.Equal(t, "prefer feature flags over env vars", e.Body)
	assert.Equal(t, "prefer feature flags over env vars", e.Title)
	assert.Equal(t, "none", e.Anchor.Type)
	assert.Equal(t, "rig:alpha agent:bot", e.CreatedBy, "created_by must be the CLI's resolved identity, space-joined -- not collapsed like scope")
	_, err = time.Parse(time.DateOnly, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an ISO-8601 date")
}
