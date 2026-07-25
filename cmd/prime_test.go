package cmd

import (
	"encoding/json"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimeBareStillWorks(t *testing.T) {
	out, err := execRoot("prime", "--store", t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPrimeRejectsStrayPositionalArgs(t *testing.T) {
	_, err := execRoot("prime", "--store", t.TempDir(), "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestPrimeJSONOutputsResult(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, "global/a.md",
		"+++\nid = \"g/a\"\ntitle = \"A\"\ntopic_key = \"topic-a\"\nscope = []\n+++\nbody\n")

	out, err := execRootJSON(t, "prime", "--json", "--store", dir)
	require.NoError(t, err)

	var result cairn.PrimeResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Items, 1)
	assert.Equal(t, "g/a", result.Items[0].ID)
	assert.Equal(t, "topic-a", result.Items[0].TopicKey)
}

func TestPrimeJSONRejectsBadIdentityTag(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--json", "--store", t.TempDir(), "--identity", "role/bad")
	require.Error(t, err)

	var result ErrorResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "role/bad", result.Error.Subject)
}

func TestPrimeJSONEmptyStoreHasEmptyArrays(t *testing.T) {
	out, err := execRootJSON(t, "prime", "--json", "--store", t.TempDir())
	require.NoError(t, err)

	var result cairn.PrimeResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []cairn.PrimeItem{}, result.Items)
	assert.Equal(t, []string{}, result.Warnings)
	assert.Equal(t, []string{}, result.Identity)
}
