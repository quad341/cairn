package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md",
		"+++\nid = \"r\"\ntitle = \"r\"\ntopic_key = \"alpha/thing\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	out, err := Prime(dir, []string{"rig:alpha"})
	require.NoError(t, err)
	assert.Contains(t, out, "alpha/thing", "an alpha-scoped agent should see the alpha topic")
	assert.Contains(t, out, "cairn remember", "prime should include the capture hint")

	bare, err := Prime(dir, nil)
	require.NoError(t, err)
	assert.NotContains(t, bare, "alpha/thing", "a bare identity should not see the alpha topic")
}

func TestPrimeEmpty(t *testing.T) {
	out, err := Prime(t.TempDir(), nil)
	require.NoError(t, err)
	assert.Contains(t, out, "No cached knowledge")
}
