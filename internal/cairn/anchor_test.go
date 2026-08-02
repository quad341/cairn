package cairn

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteBackAnchorAddsFilesAnchor is crn-01fj. Until now the only way an
// entry could acquire an anchor was to be born with one: remember's
// --anchor-repo/--anchor-path build it at creation, and verify only
// RECOMPUTES a fingerprint for an entry that already carries repo+paths.
// That left every already-written entry permanently unanchorable, which is
// most of a migrated store -- 292 of 324 in the mayor's.
func TestWriteBackAnchorAddsFilesAnchor(t *testing.T) {
	store := t.TempDir()
	p := writeFile(t, store, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"A\"\n\n[anchor]\n  type = \"none\"\n+++\nbody\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	e.Anchor = Anchor{Type: "files", Repo: "/repo", Paths: []string{"a.go", "b/c.go"}}
	require.NoError(t, e.WriteBackAnchor())

	got, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "files", got.Anchor.Type)
	assert.Equal(t, "/repo", got.Anchor.Repo)
	assert.Equal(t, []string{"a.go", "b/c.go"}, got.Anchor.Paths)
}

// TestWriteBackAnchorLeavesEverythingElseByteForByte holds WriteBack's
// surgical-patch contract: a curator reading `git diff` after an anchor
// operation should see the anchor lines change and nothing else.
func TestWriteBackAnchorLeavesEverythingElseByteForByte(t *testing.T) {
	store := t.TempDir()
	p := writeFile(t, store, "global/a.md",
		"+++\nid = \"a\"\ntitle = \"A\"\nsummary = \"keep me exactly\"\ntopic_key = \"t\"\nhit_count = 7\n\n[anchor]\n  type = \"none\"\n+++\nbody text\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	e.Anchor = Anchor{Type: "files", Repo: "/repo", Paths: []string{"x.go"}}
	require.NoError(t, e.WriteBackAnchor())

	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	out := string(raw)
	for _, keep := range []string{
		`summary = "keep me exactly"`,
		`topic_key = "t"`,
		`hit_count = 7`,
		"body text",
	} {
		assert.Contains(t, out, keep, "unrelated frontmatter and body must survive untouched")
	}
	assert.NotContains(t, out, `type = "none"`, "the old anchor type must be replaced, not duplicated")
	assert.Equal(t, 1, strings.Count(out, "[anchor]"), "exactly one [anchor] table")
}

// TestWriteBackAnchorOnEntryWithNoAnchorTable covers entries whose
// frontmatter never had an [anchor] table at all -- hand-authored and
// early-migration entries both exist in that shape, and they are precisely
// the ones most in need of anchoring.
func TestWriteBackAnchorOnEntryWithNoAnchorTable(t *testing.T) {
	store := t.TempDir()
	p := writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	e.Anchor = Anchor{Type: "files", Repo: "/repo", Paths: []string{"x.go"}}
	require.NoError(t, e.WriteBackAnchor())

	got, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "files", got.Anchor.Type)
	assert.Equal(t, []string{"x.go"}, got.Anchor.Paths)
	assert.Equal(t, "A", got.Title, "the rest of the entry must still parse")
}
