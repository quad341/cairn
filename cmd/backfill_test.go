package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyLegacyMetadata(t *testing.T) {
	body := "**Merged is not deployed — check the artifact on PATH before claiming success**\nDetails."
	derived := &cairn.Entry{Title: "Merged is not deployed — check the artifact on PATH before claiming success", Body: body}
	assert.Equal(t, "derived", classifyLegacyMetadata(derived))
	authored := &cairn.Entry{TitleSource: cairn.MetadataSourceAuthored, SummarySource: cairn.MetadataSourceAuthored}
	assert.Equal(t, "authored", classifyLegacyMetadata(authored))
	unknown := &cairn.Entry{Title: "Check the deployed artifact", Body: body}
	assert.Equal(t, "unclassifiable", classifyLegacyMetadata(unknown), "do not guess that an authored compression was derived")
}

func TestApplyBackfillDryRunAndStaleAreNonWriting(t *testing.T) {
	store := t.TempDir()
	path := filepath.Join(store, "global", "a.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	raw := "+++\nid = \"a\"\ntitle = \"Old title\"\nsummary = \"Old summary\"\nscope = []\n[anchor]\ntype = \"none\"\n+++\n\nBody unchanged.\n"
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	gitInit(t, store)
	e, err := cairn.ParseEntry(path)
	require.NoError(t, err)
	r := backfillRecord{ID: "a", OriginalTitle: e.Title, OriginalSummary: e.Summary, BodySHA256: bodyHash(e.Body), ProposedTitle: "New situational title", ProposedSummary: "Use this when the body applies."}

	result, err := applyBackfillRecord(context.Background(), store, r, false)
	require.NoError(t, err)
	assert.Equal(t, "preview", result.Status)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, raw, string(after))

	r.OriginalTitle = "Different original"
	result, err = applyBackfillRecord(context.Background(), store, r, false)
	require.Error(t, err)
	assert.Equal(t, "stale", result.Status)
	assert.Contains(t, result.Detail, "entry a")
	assert.Contains(t, result.Detail, "title")
}

func TestWriteBackRetrievalMetadataPreservesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	body := "Body bytes stay exactly here.\n"
	raw := "+++\nid = \"a\"\ntitle = \"Old\"\nsummary = \"Old summary\"\n[anchor]\ntype = \"none\"\n+++\n\n" + body
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	e, err := cairn.ParseEntry(path)
	require.NoError(t, err)
	e.Title, e.Summary = "New claim", "New retrieval summary"
	require.NoError(t, e.WriteBackRetrievalMetadata())
	got, err := cairn.ParseEntry(path)
	require.NoError(t, err)
	assert.Equal(t, body, got.Body)
	assert.Equal(t, cairn.MetadataSourceAuthored, got.TitleSource)
	assert.Equal(t, cairn.MetadataSourceAuthored, got.SummarySource)
}
