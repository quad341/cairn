package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyClassifiedError matches crn-od2x.1's Error Envelope Contract
// example byte-for-byte: a classifiedError's category/subject are read
// directly, and its Error() text becomes the JSON message unchanged.
func TestClassifyClassifiedError(t *testing.T) {
	err := classifiedErr(CategoryNotFound, "foo", fmt.Errorf("no entry %q", "foo"))
	result := classify(err)
	assert.Equal(t, ErrorResult{Error: ErrorDetail{
		Category: CategoryNotFound,
		Message:  `no entry "foo"`,
		Subject:  "foo",
	}}, result)
}

// TestClassifyClassifiedErrorPreservesErrorsIs confirms wrapping an error in
// classifiedErr does not break errors.Is on the original sentinel -- callers
// that never look at classify() output still need this to work.
func TestClassifyClassifiedErrorPreservesErrorsIs(t *testing.T) {
	err := classifiedErr(CategoryNotFound, "foo", fmt.Errorf("no entry %q: %w", "foo", cairn.ErrNotFound))
	assert.True(t, errors.Is(err, cairn.ErrNotFound))
}

// TestClassifyFallsBackToNotFound covers a plain %w-wrapped cairn.ErrNotFound
// that was never passed through classifiedErr (e.g. freshness/verify, which
// don't wire up --json but must still classify as not_found if ever asked).
func TestClassifyFallsBackToNotFound(t *testing.T) {
	err := fmt.Errorf("no entry %q: %w", "foo", cairn.ErrNotFound)
	result := classify(err)
	assert.Equal(t, CategoryNotFound, result.Error.Category)
}

func TestClassifyFallsBackToMalformedStore(t *testing.T) {
	err := &cairn.MalformedEntryError{Path: "bad.md", Err: errors.New("boom")}
	result := classify(err)
	assert.Equal(t, CategoryMalformedStore, result.Error.Category)
	assert.Equal(t, "bad.md", result.Error.Subject)
}

func TestClassifyDefaultsToInternal(t *testing.T) {
	result := classify(errors.New("something broke"))
	assert.Equal(t, CategoryInternal, result.Error.Category)
}

func TestWantsJSON(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().Bool("json", false, "")
	assert.False(t, wantsJSON(cmd))
	require.NoError(t, cmd.Flags().Set("json", "true"))
	assert.True(t, wantsJSON(cmd))
}

func TestEmitJSON(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, emitJSON(&buf, map[string]string{"a": "b"}))
	assert.Equal(t, "{\n  \"a\": \"b\"\n}\n", buf.String())
}

func TestRootFlagsShowPrettyButHideDeprecatedJSON(t *testing.T) {
	jsonFlag := rootCmd.PersistentFlags().Lookup("json")
	prettyFlag := rootCmd.PersistentFlags().Lookup("pretty")
	require.NotNil(t, jsonFlag)
	require.NotNil(t, prettyFlag)
	assert.True(t, jsonFlag.Hidden)
	assert.False(t, prettyFlag.Hidden)
}

func TestEmitErrorNoopsWithoutJSON(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().Bool("json", false, "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := errors.New("boom")
	assert.Equal(t, err, emitError(cmd, err))
	assert.Empty(t, buf.String())
	assert.False(t, cmd.SilenceErrors)
}

func TestEmitErrorWritesEnvelopeWithJSON(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().Bool("json", true, "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := classifiedErr(CategoryInvalidInput, "id", errors.New("bad id"))

	got := emitError(cmd, err)

	assert.Equal(t, err, got, "emitError must return the original error unchanged so root's os.Exit(1) still fires")
	assert.True(t, cmd.SilenceErrors)
	assert.True(t, cmd.SilenceUsage)
	assert.Contains(t, buf.String(), `"category": "invalid_input"`)
	assert.Contains(t, buf.String(), `"subject": "id"`)
}

func TestEmitErrorNoopsOnNilError(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().Bool("json", true, "")
	assert.NoError(t, emitError(cmd, nil))
}

func TestNonNilNormalizesNilSlice(t *testing.T) {
	var s []string
	assert.Equal(t, []string{}, nonNil(s))
	assert.Equal(t, []string{"a"}, nonNil([]string{"a"}))
}
