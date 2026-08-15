package cmd

import (
	"encoding/json"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntriesQueriesPersistedTypeAndLegacyUnclassified(t *testing.T) {
	store := t.TempDir()
	seedEntry(t, store, "global/knowledge.md", "+++\nid = \"knowledge-1\"\ntitle = \"Synthetic mechanism\"\ntype = \"knowledge\"\nscope = []\n+++\nsynthetic body\n")
	seedEntry(t, store, "global/legacy.md", "+++\nid = \"legacy-1\"\ntitle = \"Synthetic legacy\"\nscope = []\n+++\nsynthetic body\n")

	out, err := execRootJSON(t, "entries", "--type", cairn.EntryTypeKnowledge, "--store", store)
	require.NoError(t, err)
	var knowledge entriesOutput
	require.NoError(t, json.Unmarshal([]byte(out), &knowledge))
	assert.Equal(t, []entriesItem{{ID: "knowledge-1", Type: cairn.EntryTypeKnowledge}}, knowledge.Entries)

	out, err = execRootJSON(t, "entries", "--type", "unclassified", "--store", store)
	require.NoError(t, err)
	var legacy entriesOutput
	require.NoError(t, json.Unmarshal([]byte(out), &legacy))
	assert.Equal(t, []entriesItem{{ID: "legacy-1", Type: ""}}, legacy.Entries)
}

func TestEntriesEmptyResultIsExplicitArray(t *testing.T) {
	out, err := execRootJSON(t, "entries", "--type", cairn.EntryTypePolicy, "--store", t.TempDir())
	require.NoError(t, err)
	var result entriesOutput
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, []entriesItem{}, result.Entries)
}
