package cairn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

const sampleEntry = `+++
id = "test/one"
title = "One"
summary = "s"
type = "reference"
topic_key = "test/one"
scope = ["rig:alpha"]

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body here
`

func TestParseEntry(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/one.md", sampleEntry))
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Equal(t, "test/one", e.ID)
	assert.Equal(t, "One", e.Title)
	assert.Equal(t, []string{"rig:alpha"}, e.Scope)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Len(t, e.Anchor.Paths, 1)
	assert.Equal(t, "body here\n", e.Body)
}

func TestParseEntryNoFrontmatter(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "# just markdown\n"))
	assert.Nil(t, e)
	require.ErrorIs(t, err, errNotEntry)
}

func TestParseEntryUnterminated(t *testing.T) {
	_, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "+++\nid = \"a\"\nno closing fence\n"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, errNotEntry) // a real parse error, not "not an entry"
}

func TestWriteBackRoundTrip(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", sampleEntry)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.Anchor.Fingerprint = "abc123"
	e.VerifiedAt = "2026-07-19"
	require.NoError(t, e.WriteBack())

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "abc123", e2.Anchor.Fingerprint)
	assert.Equal(t, "2026-07-19", e2.VerifiedAt)
	assert.Equal(t, e.ID, e2.ID)
	assert.Equal(t, e.Body, e2.Body)
}

const writeBackFixtureUnverified = `+++
id = "wb/unverified"
title = "Unverified"
summary = "s"
type = "reference"
topic_key = "wb/unverified"
scope = []

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body text
`

// TestWriteBackFirstVerifyInsertsAndPreservesRest covers crn-6az.5.1's core
// claim: a first-ever verify (neither verified_at nor fingerprint present
// yet) must insert both fields -- verified_at immediately before [anchor],
// fingerprint inside the anchor table -- while every other original line,
// including an empty `scope = []`, survives verbatim. A value-equality
// round-trip check (TestWriteBackRoundTrip) can't tell "surgically patched"
// from "fully re-encoded"; only a byte-level comparison against the original
// text can.
func TestWriteBackFirstVerifyInsertsAndPreservesRest(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureUnverified)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Empty(t, e.VerifiedAt, "fixture must start unverified")
	require.Empty(t, e.Anchor.Fingerprint, "fixture must start unverified")

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "abc123"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)

	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")
	for _, l := range beforeLines {
		if l == "" {
			continue
		}
		assert.Contains(t, afterLines, l, "every original line must survive verbatim: %q", l)
	}
	assert.Contains(t, string(after), "scope = []", "empty scope must not be dropped or reformatted")

	idx := func(lines []string, target string) int {
		for i, l := range lines {
			if l == target {
				return i
			}
		}
		return -1
	}
	anchorAt := idx(afterLines, "[anchor]")
	vaAt := idx(afterLines, `verified_at = "2026-07-19"`)
	require.NotEqual(t, -1, anchorAt)
	require.NotEqual(t, -1, vaAt)
	assert.Equal(t, anchorAt-1, vaAt, "verified_at must be inserted immediately before [anchor]")
	assert.Contains(t, afterLines, `fingerprint = "abc123"`, "fingerprint must be inserted into the anchor table")

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-19", e2.VerifiedAt)
	assert.Equal(t, "abc123", e2.Anchor.Fingerprint)
	assert.Equal(t, "body text\n", e2.Body)
}

const writeBackFixtureAlreadyVerified = `+++
id = "wb/verified"
title = "Verified"
scope = []
verified_at = "2026-01-01"

[anchor]
type = "files"
repo = "/tmp/x"
fingerprint = "oldfp000"
+++

body text
`

// TestWriteBackSecondVerifyUpdatesInPlace covers a re-verify: both fields
// already present must update in place with zero line-count delta, not grow
// the file or reorder anything.
func TestWriteBackSecondVerifyUpdatesInPlace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureAlreadyVerified)
	before, err := os.ReadFile(p)
	require.NoError(t, err)
	beforeLines := strings.Split(string(before), "\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Equal(t, "2026-01-01", e.VerifiedAt)
	require.Equal(t, "oldfp000", e.Anchor.Fingerprint)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp111"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	afterLines := strings.Split(string(after), "\n")

	require.Equal(t, len(beforeLines), len(afterLines), "an in-place update must not change the line count")
	for i := range beforeLines {
		if strings.Contains(beforeLines[i], "verified_at") || strings.Contains(beforeLines[i], "fingerprint") {
			continue
		}
		assert.Equal(t, beforeLines[i], afterLines[i], "line %d is unrelated to the patched fields and must be byte-identical", i)
	}
	assert.NotContains(t, string(after), "2026-01-01")
	assert.NotContains(t, string(after), "oldfp000")
	assert.Contains(t, string(after), `verified_at = "2026-07-19"`)
	assert.Contains(t, string(after), `fingerprint = "newfp111"`)
}

const writeBackFixtureIndentedReplace = `+++
id = "wb/indented-replace"
title = "IndentedReplace"
scope = []

[anchor]
    type = "files"
    repo = "/tmp/x"
    fingerprint = "oldfp"
+++

body
`

// TestWriteBackPreservesAnchorIndentOnReplace covers replacing an existing,
// non-default-indented fingerprint line: its own indentation must survive,
// even though the codebase's own encoder never produces indented tables --
// WriteBack patches whatever text is actually on disk, hand-edited or not.
func TestWriteBackPreservesAnchorIndentOnReplace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureIndentedReplace)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(after), "    fingerprint = \"newfp\"", "the replaced line must keep its original 4-space indent")
	assert.Contains(t, string(after), "    type = \"files\"", "sibling lines must stay untouched")
	assert.Contains(t, string(after), "    repo = \"/tmp/x\"", "sibling lines must stay untouched")
	assert.NotContains(t, string(after), "oldfp")
}

const writeBackFixtureIndentedAppend = `+++
id = "wb/indented-append"
title = "IndentedAppend"
scope = []

[anchor]
    type = "files"
    repo = "/tmp/x"
+++

body
`

// TestWriteBackMatchesAnchorIndentOnAppend covers a first-ever verify inside
// an anchor block whose existing keys are indented: the newly appended
// fingerprint line must match that indentation, not default to none.
func TestWriteBackMatchesAnchorIndentOnAppend(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureIndentedAppend)
	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Empty(t, e.Anchor.Fingerprint)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(after), "    fingerprint = \"newfp\"", "an appended line must match the indentation of its sibling keys")
}

const writeBackFixtureNoAnchor = "+++\nid = \"wb/no-anchor\"\ntitle = \"NoAnchor\"\nscope = []\n+++\nbody\n"

// TestWriteBackMissingAnchorTableErrorsWithoutWriting covers the one
// hard-failure path: with no [anchor] table to patch into, WriteBack must
// return an error naming the entry id and leave the file exactly as it was
// -- never a partial write.
func TestWriteBackMissingAnchorTableErrorsWithoutWriting(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoAnchor)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "abc123"
	err = e.WriteBack()
	require.Error(t, err)
	assert.Contains(t, err.Error(), e.ID, "the error must name the entry id")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed WriteBack must leave the file byte-identical -- no partial write")
}

const (
	globalEntry = "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n"
	alphaEntry  = "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	betaEntry   = "+++\nid = \"t\"\ntitle = \"t\"\nscope = [\"rig:beta\"]\n+++\nx\n"
	crossEntry  = "+++\nid = \"x\"\ntitle = \"x\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"
)

func TestVisible(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md", alphaEntry)
	writeFile(t, dir, "rig/beta/t.md", betaEntry)
	writeFile(t, dir, "role/investigator/x.md", crossEntry)

	seen := func(identity []string) map[string]bool {
		vs, err := Visible(dir, identity)
		require.NoError(t, err)
		m := map[string]bool{}
		for _, e := range vs {
			m[e.ID] = true
		}
		return m
	}

	inv := seen([]string{"rig:alpha", "role:investigator"})
	assert.True(t, inv["g"] && inv["r"] && inv["x"], "alpha-investigator should see g, r, x")
	assert.False(t, inv["t"], "alpha-investigator should not see the beta entry")

	bare := seen(nil)
	assert.True(t, bare["g"], "bare identity should see global")
	assert.False(t, bare["r"] || bare["t"] || bare["x"], "bare identity should see only global")

	builder := seen([]string{"rig:alpha", "role:builder"})
	assert.True(t, builder["g"] && builder["r"], "alpha-builder should see g and r")
	assert.False(t, builder["x"] || builder["t"], "alpha-builder should not see x or t")
}

const (
	lessSpecificShared = "+++\nid = \"s1\"\ntitle = \"s1\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	moreSpecificShared = "+++\nid = \"s2\"\ntitle = \"s2\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"

	earlyVerifiedShared = "+++\nid = \"v1\"\ntitle = \"v1\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-01-01\"\n+++\nx\n"
	lateVerifiedShared  = "+++\nid = \"v2\"\ntitle = \"v2\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-06-01\"\n+++\nx\n"

	tiebreakLowID  = "+++\nid = \"c1\"\ntitle = \"c1\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	tiebreakHighID = "+++\nid = \"c2\"\ntitle = \"c2\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"

	untopiced1 = "+++\nid = \"u1\"\ntitle = \"u1\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced2 = "+++\nid = \"u2\"\ntitle = \"u2\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced3 = "+++\nid = \"u3\"\ntitle = \"u3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestVisibleShadowsBySpecificity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	vs, err := Visible(dir, []string{"rig:alpha", "role:investigator"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["s2"], "the 2-tag entry must be visible")
	assert.False(t, ids["s1"], "the 1-tag entry must be shadowed by the more specific one")
}

func TestVisibleShadowTiebreakVerifiedAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/v1.md", earlyVerifiedShared)
	writeFile(t, dir, "rig/alpha/v2.md", lateVerifiedShared)

	vs, err := Visible(dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["v2"], "the more recently verified entry must win")
	assert.False(t, ids["v1"], "the earlier-verified entry must be shadowed")
}

func TestVisibleShadowTiebreakID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/c2.md", tiebreakHighID)
	writeFile(t, dir, "rig/alpha/c1.md", tiebreakLowID)

	vs, err := Visible(dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["c1"], "the lexicographically lower id must win when fully tied")
	assert.False(t, ids["c2"], "the higher id must be shadowed")
}

func TestVisibleUntopicedNeverShadow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/u1.md", untopiced1)
	writeFile(t, dir, "rig/alpha/u2.md", untopiced2)
	writeFile(t, dir, "rig/alpha/u3.md", untopiced3)

	vs, err := Visible(dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["u1"] && ids["u2"] && ids["u3"], "entries without a topic_key must never shadow one another")
}

// parseFixture parses a fixture markdown const into an *Entry, independent of
// IterEntries' scope-dir layout — ShadowMap takes an entry slice directly.
func parseFixture(t *testing.T, content string) *Entry {
	t.Helper()
	e, err := ParseEntry(writeFile(t, t.TempDir(), "e.md", content))
	require.NoError(t, err)
	return e
}

const (
	incomparableRig  = "+++\nid = \"i1\"\ntitle = \"i1\"\ntopic_key = \"inc\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	incomparableRole = "+++\nid = \"i2\"\ntitle = \"i2\"\ntopic_key = \"inc\"\nscope = [\"role:builder\"]\n+++\nx\n"

	chainOneTag    = "+++\nid = \"ch1\"\ntitle = \"ch1\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	chainTwoTags   = "+++\nid = \"ch2\"\ntitle = \"ch2\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\"]\n+++\nx\n"
	chainThreeTags = "+++\nid = \"ch3\"\ntitle = \"ch3\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\", \"agent:x\"]\n+++\nx\n"

	globalShared = "+++\nid = \"gs\"\ntitle = \"gs\"\ntopic_key = \"glob\"\nscope = []\n+++\nx\n"
	scopedShared = "+++\nid = \"rs\"\ntitle = \"rs\"\ntopic_key = \"glob\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestShadowMapSuperset(t *testing.T) {
	s1 := parseFixture(t, lessSpecificShared)
	s2 := parseFixture(t, moreSpecificShared)

	sm := ShadowMap([]*Entry{s1, s2})

	require.Contains(t, sm, "s1", "the 1-tag entry must be shadowed")
	assert.Equal(t, "s2", sm["s1"].ID, "the 1-tag entry must be shadowed by the 2-tag superset entry")
	assert.NotContains(t, sm, "s2", "the more specific entry must not appear as shadowed")
}

func TestShadowMapIncomparableScopesNeverShadow(t *testing.T) {
	i1 := parseFixture(t, incomparableRig)
	i2 := parseFixture(t, incomparableRole)

	sm := ShadowMap([]*Entry{i1, i2})

	assert.NotContains(t, sm, "i1", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
	assert.NotContains(t, sm, "i2", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
}

func TestShadowMapTiebreakOnEqualScope(t *testing.T) {
	v1 := parseFixture(t, earlyVerifiedShared)
	v2 := parseFixture(t, lateVerifiedShared)

	sm := ShadowMap([]*Entry{v1, v2})

	require.Contains(t, sm, "v1", "the earlier-verified entry must be shadowed")
	assert.Equal(t, "v2", sm["v1"].ID, "the earlier-verified entry must be shadowed by the later-verified one")
	assert.NotContains(t, sm, "v2", "the later-verified (winning) entry must not appear as shadowed")
}

func TestShadowMapChainReportsMostSpecific(t *testing.T) {
	ch1 := parseFixture(t, chainOneTag)
	ch2 := parseFixture(t, chainTwoTags)
	ch3 := parseFixture(t, chainThreeTags)

	sm := ShadowMap([]*Entry{ch1, ch2, ch3})

	require.Contains(t, sm, "ch1")
	assert.Equal(t, "ch3", sm["ch1"].ID, "the 1-tag entry must be shadowed by the most specific entry in the chain, not its nearest neighbor")
	require.Contains(t, sm, "ch2")
	assert.Equal(t, "ch3", sm["ch2"].ID, "the 2-tag entry must be shadowed by the most specific entry in the chain")
	assert.NotContains(t, sm, "ch3", "the most specific entry in the chain must not appear as shadowed")
}

func TestShadowMapUntopicedNeverShadow(t *testing.T) {
	u1 := parseFixture(t, untopiced1)
	u2 := parseFixture(t, untopiced2)
	u3 := parseFixture(t, untopiced3)

	sm := ShadowMap([]*Entry{u1, u2, u3})

	assert.Empty(t, sm, "entries without a topic_key must never appear in the shadow map")
}

func TestShadowMapGlobalShadowedByScoped(t *testing.T) {
	g := parseFixture(t, globalShared)
	r := parseFixture(t, scopedShared)

	sm := ShadowMap([]*Entry{g, r})

	require.Contains(t, sm, "gs", "the global (empty-scope) entry must be shadowed by the scoped one")
	assert.Equal(t, "rs", sm["gs"].ID)
	assert.NotContains(t, sm, "rs", "the scoped entry must not appear as shadowed by the global one")
}
