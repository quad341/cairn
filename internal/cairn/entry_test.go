package cairn

import (
	"os"
	"path/filepath"
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
