package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	r := backfillRecord{
		ID: "a", OriginalTitle: e.Title, OriginalSummary: e.Summary,
		OriginalType: e.Type, OriginalKind: e.Kind, BodySHA256: bodyHash(e.Body),
		ProposedTitle: "New situational title", ProposedSummary: "Use this when the body applies.",
		ProposedType: cairn.EntryTypeKnowledge,
	}

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

func TestBackfillPreservesLegacyRemediationClassification(t *testing.T) {
	store := t.TempDir()
	path := filepath.Join(store, "global", "repair.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	raw := "+++\nid = \"repair\"\ntitle = \"Recover the synthetic service\"\nsummary = \"Use after the synthetic service reports a known failure.\"\ntitle_source = \"authored\"\nsummary_source = \"authored\"\nkind = \"remediation\"\nauto_actionable = true\nscope = []\n[anchor]\ntype = \"none\"\n+++\n\nSynthetic recovery procedure.\n"
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	classified := "+++\nid = \"known\"\ntitle = \"Known synthetic behavior\"\ntype = \"knowledge\"\nscope = []\n[anchor]\ntype = \"none\"\n+++\n\nSynthetic fact.\n"
	require.NoError(t, os.WriteFile(filepath.Join(store, "global", "known.md"), []byte(classified), 0o600))
	gitInit(t, store)

	records, counts, err := collectBackfill(store)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 1, counts.Classified, "reruns skip entries already carrying a type")
	assert.Equal(t, 1, counts.LegacyRemediation)
	record := records[0]
	assert.Equal(t, cairn.EntryTypeRemediation, record.ProposedType,
		"legacy remediation is mapped mechanically, without classifier input")
	assert.False(t, record.NeedsMetadata)

	result, err := applyBackfillRecord(context.Background(), store, record, true)
	require.NoError(t, err)
	assert.Equal(t, "applied", result.Status)

	got, err := cairn.ParseEntry(path)
	require.NoError(t, err)
	assert.Equal(t, cairn.EntryTypeRemediation, got.Type)
	assert.Equal(t, cairn.EntryTypeRemediation, got.Kind)
	assert.True(t, got.AutoActionable, "classification backfill must preserve auto-actionable eligibility")
}

func TestBackfillRejectsContradictoryLegacyRemediationType(t *testing.T) {
	record := backfillRecord{
		ID: "repair", OriginalKind: cairn.EntryTypeRemediation,
		BodySHA256: strings.Repeat("0", 64), ProposedType: cairn.EntryTypeKnowledge,
	}
	err := validateBackfillRecord(record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry repair")
	assert.Contains(t, err.Error(), "contradicts legacy kind")
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
