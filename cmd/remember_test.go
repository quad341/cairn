package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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

// runRememberCapturingStderr is runRemember with the command's error stream
// returned instead of discarded, for tests asserting on advisory output that
// must not affect the exit status (crn-5wus's unanchored nudge).
func runRememberCapturingStderr(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	resetRememberFlags(t)
	t.Cleanup(func() { resetRememberFlags(t) })

	t.Setenv("CAIRN_IDENTITY", "agent:tester")
	store := t.TempDir()
	gitInit(t, store)
	stubGC(t)
	var errBuf bytes.Buffer
	rootCmd.SetArgs(append([]string{"remember", "--store", store}, extraArgs...))
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&errBuf)
	err := rootCmd.Execute()
	return errBuf.String(), err
}

// TestRememberNudgesWhenUnanchored is crn-5wus. An entry with no anchor can
// only ever report time-based freshness, and agents wrote 84% of new entries
// that way because nothing ever mentioned the alternative at the moment of
// writing. The nudge names the flags; it must never change the exit status,
// because plenty of entries legitimately have no source file.
func TestRememberNudgesWhenUnanchored(t *testing.T) {
	stderr, err := runRememberCapturingStderr(t, "--topic", "valid-topic", "a body")
	require.NoError(t, err, "the nudge is advisory and must not fail the write")
	assert.Contains(t, stderr, "--anchor-repo")
	assert.Contains(t, stderr, "--anchor-path")
}

// TestRememberDoesNotNudgeWhenAnchored is the other half: an agent that did
// the right thing must not be nagged, or the nudge becomes noise agents learn
// to filter out -- the same failure mode as a permanently-red test.
func TestRememberDoesNotNudgeWhenAnchored(t *testing.T) {
	stderr, err := runRememberCapturingStderr(t,
		"--topic", "valid-topic",
		"--anchor-repo", t.TempDir(),
		"--anchor-path", "some/file.go",
		"a body")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "--anchor-repo",
		"an already-anchored write must produce no anchor advice")
}

// runRememberAgainstStore runs "cairn remember" (plus extraArgs) against an
// already-existing, already-git-initialized store, for tests that need two
// remember calls to land in the same store (crn-28ge.1.4's capture-time
// recurrence detection): runRemember/runRememberWithGC each mint their own
// fresh store per call, which can't exercise it -- the second call must see
// the first call's own entry as VISIBLE and already committed. Callers own
// the store's lifecycle (t.TempDir + gitInit) since, unlike a single-call
// test, more than one invocation needs to agree on it. Always stubs gc with
// the always-succeeding stubGC; see runRememberAgainstStoreWithGC for a
// caller that also needs to observe the stub's own invocation.
func runRememberAgainstStore(t *testing.T, store string, extraArgs ...string) error {
	t.Helper()
	return runRememberAgainstStoreWithGC(t, store, stubGC, extraArgs...)
}

// runRememberAgainstStoreWithGC is runRememberAgainstStore parameterized on
// the gc stub, the same split runRemember/runRememberWithGC already make:
// crn-0tsu FR-6's shared-tier round-trip tests need both an existing store
// (so a later `cairn review merge` call can see what this remember call
// wrote) and a capturing stub (to assert the mailed reviewer's recipient),
// which neither existing single-purpose helper covers alone.
func runRememberAgainstStoreWithGC(t *testing.T, store string, stub func(*testing.T), extraArgs ...string) error {
	t.Helper()
	resetRememberFlags(t)
	t.Cleanup(func() { resetRememberFlags(t) })

	stub(t)
	args := append([]string{"remember", "--store", store}, extraArgs...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	return rootCmd.Execute()
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

// withStdin redirects os.Stdin to a real temp file containing body for the
// duration of fn, restoring the original afterward. FR-1's stdin-piped
// detection relies on os.ModeCharDevice: go test's own os.Stdin is reliably a
// char device (a real terminal, or the test runner's own attached input), so
// an in-memory substitute wouldn't exercise the same code path a real pipe
// does -- a plain regular file has none of the special mode bits set, which
// is what makes it read as "redirected" rather than "interactive".
func withStdin(t *testing.T, body string, fn func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	orig := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = orig }()

	fn()
}

func resetRememberFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"topic", "scope", "reviewer", "file", "title", "summary", "anchor-repo"} {
		f := rememberCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set(""))
		f.Changed = false
	}
	for _, name := range []string{"verify", "force"} {
		f := rememberCmd.Flags().Lookup(name)
		require.NotNil(t, f)
		require.NoError(t, f.Value.Set("false"))
		f.Changed = false
	}

	apf := rememberCmd.Flags().Lookup("anchor-path")
	require.NotNil(t, apf)
	apsv, ok := apf.Value.(pflag.SliceValue)
	require.True(t, ok, "anchor-path flag must implement pflag.SliceValue")
	require.NoError(t, apsv.Replace(nil))
	apf.Changed = false

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

// TestRememberRejectsOverCapTitle and TestRememberRejectsOverCapSummary
// cover crn-3476 FR-3's write-time layer at the CLI level, mirroring
// TestRememberRejectsAttackTopics/Scopes: an explicit --title/--summary over
// the cap is CategoryInvalidInput, same UX as --topic/--scope, and nothing
// is written. The exact cap value lives in package cairn (unexported, see
// internal/cairn/validate_test.go for boundary-precise coverage); these use
// values far past the design's recommended starting caps (Title 100 runes,
// Summary 280 runes) so the test stays valid across retuning.
func TestRememberRejectsOverCapTitle(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--title", strings.Repeat("t", 200), "a body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--title")
	assertNoFilesWritten(t, store)
}

func TestRememberRejectsOverCapSummary(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--summary", strings.Repeat("s", 400), "a body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--summary")
	assertNoFilesWritten(t, store)
}

// TestRememberRejectsMalformedScopeTags is TestRememberRejectsAttackScopes'
// sibling for crn-pa7v's own escaped shapes (crn-0tsu): none of these are
// attacks (no traversal, no injection) -- they're just not tier:value, and
// crn-pa7v shipped because nothing rejected them. "global" and "*" have no
// tier at all; "tier:global" has a colon, but the component before it --
// literally the word "tier" -- is not itself one of rig/role/agent (a user
// typing the tier name as if it were a valid tier, not a placeholder).
func TestRememberRejectsMalformedScopeTags(t *testing.T) {
	malformed := map[string]string{
		"bare word global, no tier":     "global",
		"bare asterisk, no tier":        "*",
		"tier component itself invalid": "tier:global",
	}
	for name, tag := range malformed {
		t.Run(name, func(t *testing.T) {
			store, err := runRemember(t, "--topic", "valid-topic", "--scope", tag, "a body")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "scope tag")
			assert.Contains(t, err.Error(), tag, "the error must name the offending tag")
			assertNoFilesWritten(t, store)
		})
	}
}

// TestRememberExplicitEmptyScopeWritesGlobalEntry covers crn-pa7v symptom
// (a)'s fix directly: --scope="" must actually write a global-tier entry
// (empty e.Scope), not silently collapse to the private agent: tier the
// same way an entirely-omitted --scope does. No existing test passed a
// literal "" as --scope's value at all before crn-0tsu.
func TestRememberExplicitEmptyScopeWritesGlobalEntry(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "global"))
	assert.Empty(t, e.Scope)
}

// TestRememberGlobalEntryIsVisibleToItsOwnAuthor is crn-0tsu's "critical
// one": a doctor-only check would not have caught crn-pa7v, since the
// escaped bug produced well-formed-looking output that was simply never
// checked against Visible() before shipping -- "well-formed but never
// actually checked against Visible()" is exactly how it survived. global is
// a shared tier like rig:/role: (only agent: is private -- IsPrivateScope),
// so a --scope="" write lands on its own review branch and mails "mayor"
// (defaultReviewer's "global" case) rather than becoming visible
// immediately; this mirrors
// TestRememberSharedTierRigScopeVisibleAfterReviewMerge's full chain one
// tier over, proving through the actual CLI surface (cairn review merge,
// then cairn list) that the identity which authored a global entry can
// still see it after curation -- not just that Create didn't error.
func TestRememberGlobalEntryIsVisibleToItsOwnAuthor(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:author")

	require.NoError(t, runRememberAgainstStore(t, store, "--topic", "global-roundtrip", "--scope", "", "a body"))

	e := requireSingleEntry(t, filepath.Join(store, "global"))
	assert.Empty(t, e.Scope)
	branch := "remember/" + e.ID

	require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "global-roundtrip"))

	var listErr error
	out := captureStdout(t, func() {
		listErr = runList(t, store, "global-roundtrip", "--identity", "agent:author")
	})
	require.NoError(t, listErr)
	assert.Contains(t, out, e.ID, "a merged global entry must be visible to any identity, including its own author's, via cairn list")
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

// TestRememberAcceptsSlashTopic covers crn-kp9rr.1: --topic may now contain
// slashes (ValidateTopicKey validates each '/'-delimited segment
// independently, DESIGN.md §6 Option A) -- the CLI must accept it, store it
// verbatim as topic_key, and never let the raw slash leak into the entry's
// filesystem ID (flattenTopicKey).
func TestRememberAcceptsSlashTopic(t *testing.T) {
	store, err := runRemember(t, "--topic", "team/alpha", "--scope", "agent:test", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "team/alpha", e.TopicKey, "topic_key must round-trip with its slash intact")
	assert.NotContains(t, e.ID, "/", "the derived filesystem ID must not contain a raw slash")
}

// TestRememberAndReviewMergeRoundTripSlashTopicKey covers crn-kp9rr.1 through
// the full contributor-write, curator-merge path (mirroring
// TestRememberGlobalEntryIsVisibleToItsOwnAuthor's chain): a slash-delimited
// --topic-key supplied by the reviewer at merge time
// (internal/cairn/review.go's own ValidatePathSegment -> ValidateTopicKey
// swap) must be accepted and persisted verbatim, the same as at initial
// --topic write time.
func TestRememberAndReviewMergeRoundTripSlashTopicKey(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:author")

	require.NoError(t, runRememberAgainstStore(t, store, "--topic", "draft-topic", "--scope", "rig:web", "a body"))

	e := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	branch := "remember/" + e.ID

	require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "team/alpha"))

	got, err := cairn.Find(t.Context(), store, e.ID)
	require.NoError(t, err)
	assert.Equal(t, "team/alpha", got.TopicKey, "a slash-delimited --topic-key at merge time must be accepted and persisted verbatim")
}

func TestRememberRequiresExactlyOneBodyArg(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic")
	require.Error(t, err, "a missing body argument must be rejected")
	assertNoFilesWritten(t, store)
}

// TestRememberBodyRequiredWhenNoSourceProvided covers rememberBody's default
// branch specifically: with no positional argument, no --file, and stdin not
// piped (go test's own stdin is reliably a char device, see withStdin's doc
// comment), the error must be the zero-source "a body is required" message,
// not just any error -- every other branch of the 3-source resolution
// already pins its own message (e.g.
// TestRememberRejectsPositionalAndFileTogether's "ambiguous"); this is that
// same precision for the default case.
func TestRememberBodyRequiredWhenNoSourceProvided(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a body is required")
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
// each of rig:/role:/agent: lands under that tier's own directory.
// global/'s own CLI path (an explicit --scope="", rather than --scope
// omitted -- rememberScope still defaults an omitted --scope to a single
// agent: tag) is covered separately by
// TestRememberExplicitEmptyScopeWritesGlobalEntry and
// TestRememberGlobalEntryIsVisibleToItsOwnAuthor (crn-0tsu), since it needs
// its own dedicated round-trip rather than fitting this table's
// tag/tierDir/subdirName shape.
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
	// global's own path has no subdirName segment (global/<id>.md, not
	// <tier>/<value>/<id>.md), so it can't fit the table above as a naive
	// fourth row; this dedicated case only proves the directory-selection
	// half of AC#2 that the doc comment's two dedicated round-trip tests
	// don't -- it does not replace them.
	t.Run("global", func(t *testing.T) {
		store, err := runRemember(t, "--topic", "valid-topic", "--scope", "", "a body")
		require.NoError(t, err)
		e := requireSingleEntry(t, filepath.Join(store, "global"))
		assert.Empty(t, e.Scope)
	})
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

// TestRememberNonPrivateTierDoesNotCommitGlobalTier is
// TestRememberNonPrivateTierDoesNotCommit's global-tier counterpart: global
// is a shared tier exactly like rig:/role: (only agent: is private --
// IsPrivateScope), so an explicit --scope="" write must be left untracked on
// the store's own branch too, not auto-committed.
func TestRememberNonPrivateTierDoesNotCommitGlobalTier(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "", "a body")
	})
	require.NoError(t, runErr)

	requireSingleEntry(t, filepath.Join(store, "global"))

	status := gitOutput(t, store, "status", "--porcelain")
	assert.Contains(t, status, "??", "a global-tier entry must be left untracked on the store's own branch, not auto-committed")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 3, "a non-private-tier remember must print the entry id, the review branch, and the mailed reviewer -- no commit SHA")
	assert.NotContains(t, lines[0], "/", "the first line must be the bare entry id, not a branch or reviewer address")
}

// TestRememberNonPrivateTierDoesNotCommitRoleTier is
// TestRememberNonPrivateTierDoesNotCommit's role:-tier counterpart: role: is
// a shared tier exactly like rig:/global (only agent: is private --
// IsPrivateScope), so a role:-scope write must be left untracked on the
// store's own branch too, not auto-committed.
func TestRememberNonPrivateTierDoesNotCommitRoleTier(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "role:reviewer", "a body")
	})
	require.NoError(t, runErr)

	requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))

	status := gitOutput(t, store, "status", "--porcelain")
	assert.Contains(t, status, "??", "a role:-tier entry must be left untracked on the store's own branch, not auto-committed")

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

// TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorGlobalTier
// is TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError's
// global-tier counterpart: the already-durable review-branch commit must
// survive a mail failure regardless of which shared tier the entry resolved
// to.
func TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorGlobalTier(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, stubGCFail, "--topic", "valid-topic", "--scope", "", "a body")
	})
	require.Error(t, runErr, "a failed reviewer-mail step must surface as a command error (and thus a non-zero process exit via cmd/root.go), not be swallowed")

	e := requireSingleEntry(t, filepath.Join(store, "global"))
	branch := "remember/" + e.ID

	assert.Contains(t, runErr.Error(), branch, "the error must name the review branch the entry already landed on")
	assert.Contains(t, runErr.Error(), "mail", "the error must make clear the mail step is what failed")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "the id and review-branch lines print before the mail step fails; no third 'mailed reviewer' line follows")
	assert.Equal(t, e.ID, lines[0])
	assert.Equal(t, "review branch: "+branch, lines[1])

	gitOutput(t, store, "rev-parse", "--verify", branch)
}

// TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorRoleTier
// is TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsError's
// role:-tier counterpart: the already-durable review-branch commit must
// survive a mail failure regardless of which shared tier the entry resolved
// to.
func TestRememberSharedTierMailFailureLeavesReviewBranchAndReportsErrorRoleTier(t *testing.T) {
	var store string
	var runErr error
	stdout := captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, stubGCFail, "--topic", "valid-topic", "--scope", "role:reviewer", "a body")
	})
	require.Error(t, runErr, "a failed reviewer-mail step must surface as a command error (and thus a non-zero process exit via cmd/root.go), not be swallowed")

	e := requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))
	branch := "remember/" + e.ID

	assert.Contains(t, runErr.Error(), branch, "the error must name the review branch the entry already landed on")
	assert.Contains(t, runErr.Error(), "mail", "the error must make clear the mail step is what failed")

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "the id and review-branch lines print before the mail step fails; no third 'mailed reviewer' line follows")
	assert.Equal(t, e.ID, lines[0])
	assert.Equal(t, "review branch: "+branch, lines[1])

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

// TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentGlobalTier
// is TestRememberSharedTierMailInvokedWithExpectedRecipientAndContent's
// global-tier counterpart. Unlike rig:/role:, a global-tier entry's Scope is
// empty (strings.Join(e.Scope, " ") in sendReviewMail's body template
// yields ""), so there is no scope tag for the mail body to name -- that one
// assertion has no global equivalent; every other assertion carries over
// unchanged.
func TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentGlobalTier(t *testing.T) {
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	var store string
	var runErr error
	captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, func(t *testing.T) {
			t.Helper()
			stubGCCapturing(t, captureFile)
		}, "--topic", "valid-topic", "--scope", "", "--reviewer", "custom-reviewer", "a body")
	})
	require.NoError(t, runErr)

	e := requireSingleEntry(t, filepath.Join(store, "global"))
	branch := "remember/" + e.ID

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, []string{"mail", "send", "custom-reviewer", "-s"}, args[:4],
		"the --reviewer flag's value must be passed through verbatim as the mail recipient")
	assert.Contains(t, args[4], e.TopicKey, "the subject must name the entry's topic")
	assert.Equal(t, "-m", args[5])
	assert.Contains(t, args[6], e.ID, "the mail body must name the entry id")
	assert.Contains(t, args[6], branch, "the mail body must name the review branch")
}

// TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentRoleTier is
// TestRememberSharedTierMailInvokedWithExpectedRecipientAndContent's
// role:-tier counterpart.
func TestRememberSharedTierMailInvokedWithExpectedRecipientAndContentRoleTier(t *testing.T) {
	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	var store string
	var runErr error
	captureStdout(t, func() {
		store, runErr = runRememberWithGC(t, func(t *testing.T) {
			t.Helper()
			stubGCCapturing(t, captureFile)
		}, "--topic", "valid-topic", "--scope", "role:reviewer", "--reviewer", "custom-reviewer", "a body")
	})
	require.NoError(t, runErr)

	e := requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))
	branch := "remember/" + e.ID

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, []string{"mail", "send", "custom-reviewer", "-s"}, args[:4],
		"the --reviewer flag's value must be passed through verbatim as the mail recipient")
	assert.Contains(t, args[4], e.TopicKey, "the subject must name the entry's topic")
	assert.Equal(t, "-m", args[5])
	assert.Contains(t, args[6], e.ID, "the mail body must name the entry id")
	assert.Contains(t, args[6], branch, "the mail body must name the review branch")
	assert.Contains(t, args[6], "role:reviewer", "the mail body must name the entry's scope")
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
	_, err = time.Parse(time.RFC3339, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an RFC3339 timestamp (crn-3476/crn-zcxq FR-5)")
}

// TestRememberCLIRoundTripAllFieldsGlobalTier is
// TestRememberCLIRoundTripAllFields' global-tier counterpart: defaultScope
// only ever produces a private agent: tag, so global -- unlike the private
// tier the original exercises via identity resolution alone -- can only be
// reached with an explicit --scope="". Every other field-level assertion
// carries over unchanged.
func TestRememberCLIRoundTripAllFieldsGlobalTier(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha agent:bot")
	store, err := runRemember(t, "--topic", "build-flags", "--scope", "", "prefer feature flags over env vars")
	require.NoError(t, err)

	e := requireSingleEntry(t, filepath.Join(store, "global"))
	assert.True(t, strings.HasPrefix(e.ID, "build-flags-"), "id must be derived from topic_key")
	assert.Equal(t, "build-flags", e.TopicKey)
	assert.Empty(t, e.Scope, "an explicit --scope=\"\" must write a global-tier entry with no scope tags")
	assert.Equal(t, "prefer feature flags over env vars", e.Body)
	assert.Equal(t, "prefer feature flags over env vars", e.Title)
	assert.Equal(t, "none", e.Anchor.Type)
	assert.Equal(t, "rig:alpha agent:bot", e.CreatedBy, "created_by must be the CLI's resolved identity, space-joined -- not collapsed like scope")
	_, err = time.Parse(time.RFC3339, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an RFC3339 timestamp (crn-3476/crn-zcxq FR-5)")
}

// TestRememberCLIRoundTripAllFieldsRoleTier is
// TestRememberCLIRoundTripAllFields' role:-tier counterpart: reached with an
// explicit --scope the same way the global-tier equivalent is, since
// defaultScope never produces a role: tag either.
func TestRememberCLIRoundTripAllFieldsRoleTier(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:alpha agent:bot")
	store, err := runRemember(t, "--topic", "build-flags", "--scope", "role:reviewer", "prefer feature flags over env vars")
	require.NoError(t, err)

	e := requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))
	assert.True(t, strings.HasPrefix(e.ID, "build-flags-"), "id must be derived from topic_key")
	assert.Equal(t, "build-flags", e.TopicKey)
	assert.Equal(t, []string{"role:reviewer"}, e.Scope, "an explicit --scope must be used verbatim, not collapsed like the default agent: scope")
	assert.Equal(t, "prefer feature flags over env vars", e.Body)
	assert.Equal(t, "prefer feature flags over env vars", e.Title)
	assert.Equal(t, "none", e.Anchor.Type)
	assert.Equal(t, "rig:alpha agent:bot", e.CreatedBy, "created_by must be the CLI's resolved identity, space-joined -- not collapsed like scope")
	_, err = time.Parse(time.RFC3339, e.CreatedAt)
	assert.NoError(t, err, "created_at must be an RFC3339 timestamp (crn-3476/crn-zcxq FR-5)")
}

// TestRememberSharedTierRigScopeVisibleAfterReviewMerge is the full rig:
// scope lifecycle no existing test exercised end to end before crn-0tsu
// FR-6: every other shared-tier test stops at "the review branch exists" or
// "the mail stub was invoked with the right recipient" -- none of them ever
// ran the resulting branch through `cairn review merge` and then checked
// the merged entry is actually visible to a rig:-carrying identity. That
// last hop is exactly the shape of crn-pa7v's own escaped bug -- well-formed
// but never checked against Visible() -- one tier over from the
// global-scope case TestRememberGlobalEntryIsVisibleToItsOwnAuthor covers
// directly.
func TestRememberSharedTierRigScopeVisibleAfterReviewMerge(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	// Pinned explicitly (not just relied on as writeStubGC's side effect):
	// this round trip's correctness claim is specifically about $GC_RIG
	// flowing through to the reviewer address, so the test states that
	// dependency itself rather than borrowing it from the stub helper.
	t.Setenv("GC_RIG", "test-rig")

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	require.NoError(t, runRememberAgainstStoreWithGC(t, store, func(t *testing.T) {
		t.Helper()
		stubGCCapturing(t, captureFile)
	}, "--topic", "rig-roundtrip", "--scope", "rig:web", "a body"))

	e := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	branch := "remember/" + e.ID

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, "test-rig/architect", args[2], "a rig:-scope entry's default reviewer must be $GC_RIG/architect")

	require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "rig-roundtrip"))

	var listErr error
	out := captureStdout(t, func() { listErr = runList(t, store, "rig-roundtrip", "--identity", "rig:web") })
	require.NoError(t, listErr)
	assert.Contains(t, out, e.ID, "the merged entry must be visible to a rig:web identity via cairn list")
}

// TestRememberSharedTierRoleScopeVisibleAfterReviewMerge is
// TestRememberSharedTierRigScopeVisibleAfterReviewMerge's direct role:
// counterpart (crn-0tsu FR-6): role:'s reviewer default takes a different
// branch of defaultReviewer ($GC_RIG + "/" + value, rather than rig:'s
// $GC_RIG + "/architect") and is otherwise identical wiring -- ResolvedTier
// and ValidateScopeTag both treat rig:/role:/agent: uniformly. A coverage
// gap, not a suspected logic bug, but the rig: case above is the only one
// any existing test exercised this deeply before crn-0tsu.
func TestRememberSharedTierRoleScopeVisibleAfterReviewMerge(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("GC_RIG", "test-rig")

	captureFile := filepath.Join(t.TempDir(), "gc-invocation")
	require.NoError(t, runRememberAgainstStoreWithGC(t, store, func(t *testing.T) {
		t.Helper()
		stubGCCapturing(t, captureFile)
	}, "--topic", "role-roundtrip", "--scope", "role:builder", "a body"))

	e := requireSingleEntry(t, filepath.Join(store, "role", "builder"))
	branch := "remember/" + e.ID

	args := readStubGCArgs(t, captureFile)
	require.Len(t, args, 7, "gc mail send <reviewer> -s <subject> -m <body>")
	assert.Equal(t, "test-rig/builder", args[2], "a role:-scope entry's default reviewer must be $GC_RIG/<role value>")

	require.NoError(t, runReviewCmd(t, "review", "merge", branch, "--store", store, "--topic-key", "role-roundtrip"))

	var listErr error
	out := captureStdout(t, func() { listErr = runList(t, store, "role-roundtrip", "--identity", "role:builder") })
	require.NoError(t, listErr)
	assert.Contains(t, out, e.ID, "the merged entry must be visible to a role:builder identity via cairn list")
}

// TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch covers
// crn-28ge.1.4's primary end-to-end path: a second remember call sharing an
// exact topic_key with an already-visible entry, but an incomparable (not
// equal, not superset/subset) scope, must be detected as a recurrence of
// that entry -- not written as a second, separate entry -- and the resulting
// RecurrenceCount bump must land on the SAME remember/<id> review branch the
// entry's own first-capture review commit already created, as a second
// commit, rather than fail on a branch-already-exists collision. That
// collision isn't a rare edge case: requestReview creates remember/<id> for
// every shared-tier entry at its own creation time, so it is the
// deterministic, 100%-of-the-time state for any entry that could ever become
// a recurrence match. See internal/cairn's
// TestCommitRecurrenceToReviewBranchAppendsSecondCommitToExistingBranch for
// the same fix exercised directly against the git primitive; this proves
// RunE actually wires it up end to end.
func TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig:web role:reviewer")
	store := t.TempDir()
	gitInit(t, store)

	firstOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "rig:web", "configure the shared hook")
		require.NoError(t, err)
	})
	firstLines := strings.Split(strings.TrimSpace(firstOut), "\n")
	require.Len(t, firstLines, 3, "first call is an ordinary new shared-tier entry: id, review branch, mailed reviewer")
	e1 := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	branch := "remember/" + e1.ID
	firstCommit := strings.TrimSpace(gitOutput(t, store, "rev-parse", branch))

	var secondErr error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			secondErr = runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "role:reviewer", "configure the shared hook")
		})
	})
	require.Error(t, secondErr, "crn-qxj3: a genuine (near-identical body) recurrence must be reported as a discarded write, not silent success")
	assert.Contains(t, secondErr.Error(), e1.ID)
	assert.Contains(t, stderr, e1.ID, "the discard must be explained on stderr")

	entries, err := os.ReadDir(filepath.Join(store, "rig", "web"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "a recurrence hit must not write a duplicate entry file")

	e1After, err := cairn.ParseEntry(e1.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, 1, e1After.RecurrenceCount, "the matched entry's on-disk RecurrenceCount must be incremented")

	secondParent := strings.TrimSpace(gitOutput(t, store, "rev-parse", branch+"~1"))
	assert.Equal(t, firstCommit, secondParent, "the recurrence commit must append directly on top of the original review commit, not fail or fork a new one")
}

// TestRememberCrossCallPrivateTierRecurrenceCommitsDirectly covers
// crn-28ge.1.4's private-tier path: a second remember call from a different
// agent, sharing an exact topic_key with an already-visible (cross-agent)
// private-tier entry, increments that entry's RecurrenceCount and commits
// the change straight to the store's current branch via CommitDirect -- the
// private tier's ordinary commit path, exactly as its own first-capture
// commit used.
func TestRememberCrossCallPrivateTierRecurrenceCommitsDirectly(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	t.Setenv("CAIRN_IDENTITY", "agent:bob")
	firstOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:bob", "prefer feature flags over env vars")
		require.NoError(t, err)
	})
	firstLines := strings.Split(strings.TrimSpace(firstOut), "\n")
	require.Len(t, firstLines, 2, "first call is an ordinary new private-tier entry: id, commit SHA")
	e1 := requireSingleEntry(t, filepath.Join(store, "agent", "bob"))
	headBefore := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	assert.Equal(t, headBefore, firstLines[1])

	// A second agent, but with an --identity broad enough to also see
	// agent:bob's entry (Visible is a subset match: every scope tag on the
	// entry -- here just "agent:bob" -- must be in the resolved identity).
	t.Setenv("CAIRN_IDENTITY", "agent:bob agent:alice")
	var secondErr error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			secondErr = runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:alice", "prefer feature flags over env vars")
		})
	})
	require.Error(t, secondErr, "crn-qxj3: a genuine same-body recurrence must be reported as a discarded write, not silent success")
	assert.Contains(t, secondErr.Error(), e1.ID)
	assert.Contains(t, stderr, e1.ID, "the discard must be explained on stderr")

	headAfter := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD"))
	assert.NotEqual(t, headBefore, headAfter, "the RecurrenceCount bump must still commit for real even though the write is reported as an error")

	parent := strings.TrimSpace(gitOutput(t, store, "rev-parse", "HEAD~1"))
	assert.Equal(t, headBefore, parent, "the recurrence commit must land directly on top of the entry's first-capture commit")

	e1After, err := cairn.ParseEntry(e1.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, 1, e1After.RecurrenceCount)

	entries, err := os.ReadDir(filepath.Join(store, "agent", "bob"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "a recurrence hit must not write a duplicate entry file")
	assert.NoDirExists(t, filepath.Join(store, "agent", "alice"),
		"a recurrence hit must never create the discarded candidate's own scope directory, since Create is never called")
}

// TestRememberSameScopeTopicKeyRepeatIncrementsRecurrence covers crn-lzn4.1.1's
// FR-6 fix. This test used to document a KNOWN LIMITATION: two entries
// sharing both an exact topic_key AND an equal scope were "shadow exempt" in
// Conflicts' own signal computation (pairSignals, internal/cairn/dedup.go),
// because an equal scope is a mutual superset of itself in both directions
// and the old shadowExempt check was non-strict (either direction, not
// exclusive). That silently suppressed ANY finding for such a pair, topic_key
// included, so the single most intuitive recurrence scenario -- an agent
// re-capturing the exact same fact under its own single private scope -- was
// never detected: each call created its own separate entry. pairSignals'
// shadowExempt is now a strict (exclusive-or) superset check, so an equal
// scope no longer counts as legitimate shadowing, and this now behaves like
// every other recurrence match: the second call increments the first entry's
// RecurrenceCount instead of creating a second entry. See
// internal/cairn/dedup_test.go's TestConflictsEqualScopeSameTopicKeyIsNotShadowing
// for the same fix exercised directly against pairSignals/Conflicts.
func TestRememberSameScopeTopicKeyRepeatIncrementsRecurrence(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:test", "prefer feature flags over env vars")
		require.NoError(t, err)
	})
	var secondErr error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			secondErr = runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:test", "prefer feature flags over env vars")
		})
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "an equal-scope same-topic_key repeat must now be detected as a recurrence, not written as a second separate entry")

	parsed, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", entries[0].Name()))
	require.NoError(t, err)
	assert.Equal(t, 1, parsed.RecurrenceCount)

	require.Error(t, secondErr, "crn-qxj3: a genuine same-body recurrence must exit non-zero, not report silent success")
	assert.Contains(t, secondErr.Error(), parsed.ID)
	assert.Contains(t, stderr, parsed.ID, "the discard must be explained on stderr")
}

// TestRememberNearMissTopicKeyDoesNotIncrementRecurrence proves Conflicts'
// "content" (Jaccard word-similarity) finding is correctly ignored by
// recurrenceMatch even when it fires: two calls with different topic_key but
// identical body text produce a real content-signal finding (the same
// signal `cairn get` surfaces as a soft "this looks similar" hint), but a
// similar-but-different topic is a near-miss, not a repeat -- only an exact
// topic_key match (crn-28ge.1.4's AC) may increment RecurrenceCount. Safe to
// use the SAME scope for both calls here: shadowExempt requires
// sameTopicKey as a precondition (internal/cairn/dedup.go's pairSignals), so
// two different topic_keys are never shadow-exempt regardless of scope, and
// the content signal is exercised cleanly.
func TestRememberNearMissTopicKeyDoesNotIncrementRecurrence(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:test", "prefer feature flags over env vars")
		require.NoError(t, err)
	})
	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags-alt", "--scope", "agent:test", "prefer feature flags over env vars")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "a near-miss topic_key must never be treated as a recurrence, even though the content signal fires")
	for _, ent := range entries {
		parsed, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, 0, parsed.RecurrenceCount)
	}
}

// TestRememberDistinctBodySameTopicKeyIsStoredNotDiscarded covers crn-qxj3:
// recurrenceMatch used to match on a bare topic_key collision alone,
// ignoring Conflicts' separate "content" (Jaccard) signal entirely -- so any
// second entry sharing an exact --topic string was discarded as a
// "recurrence" and its RecurrenceCount-bump reported as success, no matter
// how unrelated its body was to the first entry's. dedup.go's Conflicts
// already computes both signals independently for exactly this reason (see
// pairSignals); recurrenceMatch must require BOTH a topic_key match AND a
// content match. This is the mirror image of
// TestRememberNearMissTopicKeyDoesNotIncrementRecurrence above (same body,
// different topic -- correctly not a recurrence): here the topic is the same
// and the body is what differs, and it must be no less correctly not a
// recurrence.
//
// crn-pip8: storing it was never the whole story. A topic-only match (no
// --force) used to fall through with no link between the two entries at
// all, leaving shadow resolution to moreSpecificReason's scope_size ->
// verified_at -> created_at -> id_tiebreak chain -- and since both calls
// here share a scope and typically tie on CreatedAt's second-precision
// timestamp, id_tiebreak (unrelated to write recency) could just as easily
// pick the FIRST (superseded) body as the second. The second call must now
// record OverriddenDuplicateOf against the first automatically, same as an
// explicit --force override, so the correction deterministically wins.
func TestRememberDistinctBodySameTopicKeyIsStoredNotDiscarded(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "pr-triage", "--scope", "agent:test", "always tag every reviewer before merging a pull request")
		require.NoError(t, err)
	})
	first := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	secondOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "pr-triage", "--scope", "agent:test", "rollback scripts must be tested against staging data first")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "a shared --topic alone must not discard a genuinely distinct second body -- crn-qxj3")
	var second *cairn.Entry
	for _, ent := range entries {
		parsed, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, "pr-triage", parsed.TopicKey)
		assert.Equal(t, 0, parsed.RecurrenceCount, "neither entry was a genuine recurrence, so RecurrenceCount must stay 0 on both")
		if parsed.ID != first.ID {
			second = parsed
		}
	}
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.OverriddenDuplicateOf,
		"crn-pip8: a topic-only match must auto-record the override, same as --force does for a content match, so shadow resolution can never fall through to the recency-blind id_tiebreak for a plain correction")

	secondLines := strings.Split(strings.TrimSpace(secondOut), "\n")
	require.Len(t, secondLines, 3, "the second call now reports the auto-override: id, override line, commit SHA")
	assert.NotContains(t, secondLines[0], "recurrence", "a distinct body under the same topic must never be reported as a recurrence")
	assert.Equal(t, "override: supersedes prior entry "+first.ID+" for topic \"pr-triage\"", secondLines[1],
		"the auto-override line must not claim --force was used ('forced past duplicate') when it was not")
}

// TestRememberEmptyTopicNeverMatchesForRecurrence covers the AC's own edge
// case: candidate.TopicKey == "" never matches anything, since pairSignals'
// sameTopicKey requires a non-empty key on both sides -- so an untopiced
// remember is entirely unaffected by crn-28ge.1.4, matching today's
// behavior exactly, even for two calls with identical scope and body.
func TestRememberEmptyTopicNeverMatchesForRecurrence(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--scope", "agent:test", "a body with no topic")
		require.NoError(t, err)
	})
	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--scope", "agent:test", "a body with no topic")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "two topic-less remembers must always create two separate entries")
	for _, ent := range entries {
		parsed, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, "", parsed.TopicKey)
		assert.Equal(t, 0, parsed.RecurrenceCount)
	}
}

// TestRememberRecurrenceRequiresVisibleMatch proves recurrenceMatch only
// ever considers entries VISIBLE to the resolved identity (crn-28ge.1.4's
// AC), not every entry in the store: E1 is captured under scope
// role:reviewer; an identity of just rig:web can't see it (Visible is a
// subset match -- every scope tag on the entry must be in the identity's own
// tag set, and role:reviewer isn't in {rig:web}) -- even though E1's scope
// IS genuinely incomparable with the second call's rig:web scope, the same
// mechanism that lets TestRememberCrossCallSharedTierRecurrenceReusesReviewBranch
// detect a match. This isolates visibility as the one variable under test.
func TestRememberRecurrenceRequiresVisibleMatch(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	t.Setenv("CAIRN_IDENTITY", "role:reviewer")
	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "role:reviewer", "configure the shared hook")
		require.NoError(t, err)
	})
	e1 := requireSingleEntry(t, filepath.Join(store, "role", "reviewer"))

	t.Setenv("CAIRN_IDENTITY", "rig:web")
	secondOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "rig:web", "configure the shared hook")
		require.NoError(t, err)
	})
	secondLines := strings.Split(strings.TrimSpace(secondOut), "\n")
	require.Len(t, secondLines, 3, "invisible to this identity, E1 can't be matched -- the second call is an ordinary new shared-tier entry: id, review branch, mailed reviewer")

	e2 := requireSingleEntry(t, filepath.Join(store, "rig", "web"))
	assert.NotEqual(t, e1.ID, e2.ID)

	e1After, err := cairn.ParseEntry(e1.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, 0, e1After.RecurrenceCount, "E1 must be untouched: it was never visible to the second call's identity")
}

// execRememberJSONAgainstStore is runRememberAgainstStore's --json
// counterpart: resetRememberFlags alone doesn't touch the shared --json
// persistent flag (it's registered on rootCmd by format.go's init, not by
// this file's own init), so a leftover --json=true from an earlier test
// would otherwise leak into a later non-JSON runRemember call in the same
// binary. Returns cmd.OutOrStdout()'s buffer (see execRootJSON in
// commands_json_test.go for why: emitJSON/emitError write there, not to
// bare os.Stdout, unlike human mode's fmt.Printf/Println).
func execRememberJSONAgainstStore(t *testing.T, store string, extraArgs ...string) (string, error) {
	t.Helper()
	resetRememberFlags(t)
	require.NoError(t, resetJSONFlag())
	t.Cleanup(func() {
		resetRememberFlags(t)
		_ = resetJSONFlag()
	})

	stubGC(t)
	args := append([]string{"remember", "--store", store, "--json"}, extraArgs...)
	rootCmd.SetArgs(args)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&bytes.Buffer{})
	err := rootCmd.Execute()
	return buf.String(), err
}

// runRememberJSON is execRememberJSONAgainstStore against a fresh store, for
// a test that only needs a single call -- mirrors runRemember's relationship
// to runRememberAgainstStore.
func runRememberJSON(t *testing.T, extraArgs ...string) (string, error) {
	t.Helper()
	store := t.TempDir()
	gitInit(t, store)
	return execRememberJSONAgainstStore(t, store, extraArgs...)
}

func TestRememberJSONPrivateTierOutputsResult(t *testing.T) {
	out, err := runRememberJSON(t, "--scope", "agent:test", "capture this")
	require.NoError(t, err)

	var result RememberResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, []string{"agent:test"}, result.Scope)
	assert.NotEmpty(t, result.Commit)
	assert.Empty(t, result.ReviewBranch)
	assert.Empty(t, result.Reviewer)
}

func TestRememberJSONSharedTierOutputsReviewBranchAndReviewer(t *testing.T) {
	out, err := runRememberJSON(t, "--scope", "rig:web", "capture this")
	require.NoError(t, err)

	var result RememberResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, []string{"rig:web"}, result.Scope)
	assert.Empty(t, result.Commit)
	assert.NotEmpty(t, result.ReviewBranch)
	assert.NotEmpty(t, result.Reviewer)
}

// TestRememberJSONSharedTierOutputsReviewBranchAndReviewerGlobalTier is
// TestRememberJSONSharedTierOutputsReviewBranchAndReviewer's global-tier
// counterpart. result.Scope is asserted with assert.Empty rather than
// assert.Equal against a literal []string{}: nonNil's exact empty-vs-nil
// slice shape isn't this test's concern (TestRememberExplicitEmptyScopeWritesGlobalEntry
// already covers e.Scope itself with the same assert.Empty idiom), only
// that --json surfaces no scope tags for a global-tier entry.
func TestRememberJSONSharedTierOutputsReviewBranchAndReviewerGlobalTier(t *testing.T) {
	out, err := runRememberJSON(t, "--scope", "", "capture this")
	require.NoError(t, err)

	var result RememberResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.NotEmpty(t, result.ID)
	assert.Empty(t, result.Scope)
	assert.Empty(t, result.Commit)
	assert.NotEmpty(t, result.ReviewBranch)
	assert.NotEmpty(t, result.Reviewer)
}

// TestRememberJSONSharedTierOutputsReviewBranchAndReviewerRoleTier is
// TestRememberJSONSharedTierOutputsReviewBranchAndReviewer's role:-tier
// counterpart.
func TestRememberJSONSharedTierOutputsReviewBranchAndReviewerRoleTier(t *testing.T) {
	out, err := runRememberJSON(t, "--scope", "role:reviewer", "capture this")
	require.NoError(t, err)

	var result RememberResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, []string{"role:reviewer"}, result.Scope)
	assert.Empty(t, result.Commit)
	assert.NotEmpty(t, result.ReviewBranch)
	assert.NotEmpty(t, result.Reviewer)
}

func TestRememberJSONRejectsInvalidScopeTag(t *testing.T) {
	out, err := runRememberJSON(t, "--scope", "agent:../evil", "capture this")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "agent:../evil", result.Error.Subject)
}

// TestRememberJSONRejectsInvalidIdentityTagNotUsedAsScope proves
// resolveIdentityValidated's new call in remember's RunE covers a case the
// existing --topic/--scope validation loop can't reach: an explicit --scope
// makes rememberScope skip defaultScope (and so never look at identity)
// entirely, so a bad identity tag would otherwise reach cairn.NewEntry
// unchecked, landing only in CreatedBy.
func TestRememberJSONRejectsInvalidIdentityTagNotUsedAsScope(t *testing.T) {
	t.Setenv("CAIRN_IDENTITY", "rig/bad agent:bot")
	out, err := runRememberJSON(t, "--scope", "agent:bot", "capture this")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "rig/bad", result.Error.Subject)
}

// TestRememberJSONRecurrenceReportsConflictError mirrors
// TestRememberCrossCallPrivateTierRecurrenceCommitsDirectly's setup exactly:
// two different agent scopes sharing one topic_key, with a second identity
// broad enough to see both. crn-qxj3: --json must not shield a genuine
// recurrence discard from the same non-zero-exit contract the plain-text
// path now has -- an agent scripting against --json and checking only the
// exit code must not be fooled either.
func TestRememberJSONRecurrenceReportsConflictError(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)

	t.Setenv("CAIRN_IDENTITY", "agent:bob")
	firstOut, err := execRememberJSONAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:bob", "prefer feature flags over env vars")
	require.NoError(t, err)
	var first RememberResult
	require.NoError(t, json.Unmarshal([]byte(firstOut), &first))

	t.Setenv("CAIRN_IDENTITY", "agent:bob agent:alice")
	out, err := execRememberJSONAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:alice", "prefer feature flags over env vars")
	require.Error(t, err, "a genuine recurrence discard must exit non-zero in --json mode too, not report a fake success result")

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result), "the --json error envelope, not a RememberResult success shape, must be printed")
	assert.Contains(t, result.Error.Message, first.ID, "the error must name which entry the discarded body recurred against")
}

// TestRememberReadsBodyFromStdinWhenNoPositionalArg covers crn-lzn4.1.1's
// FR-1: with no positional body argument and no --file, a piped (non-TTY)
// stdin is read as the body.
func TestRememberReadsBodyFromStdinWhenNoPositionalArg(t *testing.T) {
	var store string
	var runErr error
	withStdin(t, "a body from stdin", func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "agent:test")
	})
	require.NoError(t, runErr)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "a body from stdin", e.Body)
}

// TestRememberFileFlagReadsBody covers crn-lzn4.1.1's FR-2: --file reads the
// body from the named file when no positional body argument is given.
func TestRememberFileFlagReadsBody(t *testing.T) {
	bodyFile := filepath.Join(t.TempDir(), "body.txt")
	require.NoError(t, os.WriteFile(bodyFile, []byte("a body from a file"), 0o600))

	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "--file", bodyFile)
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "a body from a file", e.Body)
}

// TestRememberRejectsPositionalAndFileTogether covers crn-lzn4.1.1's FR-1/NFR-1:
// two or more input sources (here, a positional body and --file) must be
// rejected as ambiguous, with nothing written.
func TestRememberRejectsPositionalAndFileTogether(t *testing.T) {
	bodyFile := filepath.Join(t.TempDir(), "body.txt")
	require.NoError(t, os.WriteFile(bodyFile, []byte("file body"), 0o600))

	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "--file", bodyFile, "positional body")
	require.Error(t, err, "a positional body and --file together must be rejected as ambiguous")
	assert.Contains(t, err.Error(), "ambiguous")
	assertNoFilesWritten(t, store)
}

// TestRememberRejectsPositionalAndStdinTogether is
// TestRememberRejectsPositionalAndFileTogether's stdin counterpart: a
// positional body plus piped stdin is the same NFR-1 ambiguity, through a
// different pair of sources.
func TestRememberRejectsPositionalAndStdinTogether(t *testing.T) {
	var store string
	var runErr error
	withStdin(t, "stdin body", func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "positional body")
	})
	require.Error(t, runErr, "a positional body and piped stdin together must be rejected as ambiguous")
	assert.Contains(t, runErr.Error(), "ambiguous")
	assertNoFilesWritten(t, store)
}

// TestRememberTitleAndSummaryFlagsOverrideAutoDerivation covers crn-lzn4.1.1's
// FR-3 at the CLI layer: --title/--summary must win over titleAndSummary's
// auto-derivation from the body.
func TestRememberTitleAndSummaryFlagsOverrideAutoDerivation(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--title", "explicit title", "--summary", "explicit summary",
		"auto-derived title line\nrest of the body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "explicit title", e.Title)
	assert.Equal(t, "explicit summary", e.Summary)
}

// TestRememberTitleFlagAloneAutoDerivesSummary covers NewEntryParams' partial
// defaulting (see internal/cairn/remember_test.go, where the logic lives) at
// the CLI layer: --title alone must leave --summary's auto-derivation from
// the body untouched, not fall back to auto-deriving both.
func TestRememberTitleFlagAloneAutoDerivesSummary(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--title", "explicit title",
		"auto-derived title line\nrest of the body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "explicit title", e.Title)
	assert.Equal(t, "auto-derived title line\nrest of the body", e.Summary)
}

// TestRememberSummaryFlagAloneAutoDerivesTitle is
// TestRememberTitleFlagAloneAutoDerivesSummary's mirror image: --summary
// alone must leave --title's auto-derivation from the body untouched.
func TestRememberSummaryFlagAloneAutoDerivesTitle(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--summary", "explicit summary",
		"auto-derived title line\nrest of the body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "auto-derived title line", e.Title)
	assert.Equal(t, "explicit summary", e.Summary)
}

// TestRememberAnchorFlagsBuildFilesAnchor covers crn-lzn4.1.1's FR-4:
// --anchor-repo plus one or more repeatable --anchor-path flags build a
// "files"-type Anchor, without requiring --verify.
func TestRememberAnchorFlagsBuildFilesAnchor(t *testing.T) {
	anchorRepo := t.TempDir()
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--anchor-repo", anchorRepo, "--anchor-path", "a.go", "--anchor-path", "b.go", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Equal(t, anchorRepo, e.Anchor.Repo)
	assert.Equal(t, []string{"a.go", "b.go"}, e.Anchor.Paths)
}

// gitCommitFile writes relPath under repo (an already gitInit'd directory)
// with content, then git-adds and commits it -- the tracked-object fixture
// FR-5's --verify tests need, distinct from gitInit's own empty initial
// commit and gitCommitAll's "add everything" mode.
func gitCommitFile(t *testing.T, repo, relPath, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, relPath), []byte(content), 0o600))
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "add", relPath).CombinedOutput()
	require.NoErrorf(t, err, "git add: %s", out)
	out, err = exec.CommandContext(t.Context(), "git", "-C", repo, "commit", "-q", "-m", "add "+relPath).CombinedOutput()
	require.NoErrorf(t, err, "git commit: %s", out)
}

// TestRememberVerifyFlagComputesFingerprintOnSuccess covers crn-lzn4.1.1's
// FR-5 success path: --verify against a files anchor that resolves to a real
// tracked object at the anchor repo's HEAD computes and persists a
// fingerprint, and stamps the entry's VerifiedAt.
func TestRememberVerifyFlagComputesFingerprintOnSuccess(t *testing.T) {
	anchorRepo := t.TempDir()
	gitInit(t, anchorRepo)
	gitCommitFile(t, anchorRepo, "a.go", "package a\n")

	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
		"--anchor-repo", anchorRepo, "--anchor-path", "a.go", "--verify", "a body")
	require.NoError(t, err)

	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Equal(t, "files", e.Anchor.Type)
	assert.NotEmpty(t, e.Anchor.Fingerprint, "--verify must compute and persist a fingerprint when the anchor resolves")
	assert.NotEmpty(t, e.VerifiedAt, "--verify must stamp VerifiedAt on success")
}

// TestRememberVerifySoftFailsToStderrWhenAnchorUnresolvable covers
// crn-lzn4.1.1's FR-5 soft-fail path: --verify against an anchor path that
// doesn't resolve to a real tracked object must warn on stderr and continue
// -- not abort the command or leave Fingerprint/VerifiedAt set.
func TestRememberVerifySoftFailsToStderrWhenAnchorUnresolvable(t *testing.T) {
	anchorRepo := t.TempDir()
	gitInit(t, anchorRepo)

	var store string
	var runErr error
	stderr := captureStderr(t, func() {
		store, runErr = runRemember(t, "--topic", "valid-topic", "--scope", "agent:test",
			"--anchor-repo", anchorRepo, "--anchor-path", "does-not-exist.go", "--verify", "a body")
	})
	require.NoError(t, runErr, "an unverifiable anchor must soft-fail, not abort the whole command")
	assert.NotEmpty(t, stderr, "a soft-fail must warn on stderr")

	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Empty(t, e.Anchor.Fingerprint)
	assert.Empty(t, e.VerifiedAt)
}

// TestRememberForceOverridesRecurrenceMatchPrivateTier covers crn-lzn4.1.1's
// FR-7/FR-8 on the private tier: --force against a candidate that would
// otherwise match an existing entry as a recurrence instead creates a new
// entry, records OverriddenDuplicateOf on it, and prints the override line
// between the id and the commit SHA.
func TestRememberForceOverridesRecurrenceMatchPrivateTier(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "agent:test")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:test", "prefer feature flags over env vars")
		require.NoError(t, err)
	})
	first := requireSingleEntry(t, filepath.Join(store, "agent", "test"))

	secondOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "build-flags", "--scope", "agent:test", "--force", "prefer feature flags over env vars")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "agent", "test"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "--force must create a new entry even though a recurrence match exists, not increment the matched entry")

	secondLines := strings.Split(strings.TrimSpace(secondOut), "\n")
	require.Len(t, secondLines, 3, "a forced-override private-tier create prints the id, the override line, then the commit SHA")
	assert.Equal(t, "override: forced past duplicate of "+first.ID, secondLines[1])

	var second *cairn.Entry
	for _, ent := range entries {
		parsed, err := cairn.ParseEntry(filepath.Join(store, "agent", "test", ent.Name()))
		require.NoError(t, err)
		if parsed.ID != first.ID {
			second = parsed
		}
	}
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.OverriddenDuplicateOf, "the forced entry must record which entry it overrode")
}

// TestRememberForceOverridesRecurrenceMatchSharedTier is
// TestRememberForceOverridesRecurrenceMatchPrivateTier's shared-tier
// counterpart: the override line is followed by the review-branch and
// mailed-reviewer lines instead of a commit SHA.
func TestRememberForceOverridesRecurrenceMatchSharedTier(t *testing.T) {
	store := t.TempDir()
	gitInit(t, store)
	t.Setenv("CAIRN_IDENTITY", "rig:web")

	captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "rig:web", "configure the shared hook")
		require.NoError(t, err)
	})
	first := requireSingleEntry(t, filepath.Join(store, "rig", "web"))

	secondOut := captureStdout(t, func() {
		err := runRememberAgainstStore(t, store, "--topic", "shared-hook", "--scope", "rig:web", "--force", "configure the shared hook")
		require.NoError(t, err)
	})

	entries, err := os.ReadDir(filepath.Join(store, "rig", "web"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "--force must create a new entry even for a shared-tier recurrence match")

	secondLines := strings.Split(strings.TrimSpace(secondOut), "\n")
	require.Len(t, secondLines, 4, "a forced-override shared-tier create prints the id, override line, review branch, and mailed reviewer")
	assert.Equal(t, "override: forced past duplicate of "+first.ID, secondLines[1])
	assert.True(t, strings.HasPrefix(secondLines[2], "review branch: "))
	assert.True(t, strings.HasPrefix(secondLines[3], "mailed reviewer: "))
}

// TestRememberForceWithNoMatchBehavesLikeOrdinaryCreate covers crn-lzn4.1.1's
// §7 Output Contract note: --force with no actual duplicate found is
// indistinguishable from an ordinary create -- no override line, no
// OverriddenDuplicateOf.
func TestRememberForceWithNoMatchBehavesLikeOrdinaryCreate(t *testing.T) {
	store, err := runRemember(t, "--topic", "valid-topic", "--scope", "agent:test", "--force", "a body")
	require.NoError(t, err)
	e := requireSingleEntry(t, filepath.Join(store, "agent", "test"))
	assert.Empty(t, e.OverriddenDuplicateOf, "--force with no actual duplicate match must behave like an ordinary create -- nothing to override")
}
