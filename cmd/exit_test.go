package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newExitTestCmd builds a bare "cairn doctor" command wired to a test
// obslog.Logger, for exercising logCommandExit in complete isolation from
// any os.Exit call. Returns the command and the buffer its logger writes to.
func newExitTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	parent := &cobra.Command{Use: "cairn"}
	child := &cobra.Command{Use: "doctor"}
	parent.AddCommand(child)
	child.Flags().String("store", "", "")
	child.Flags().Bool("json", false, "")

	var buf bytes.Buffer
	logger := obslog.NewWithWriter(&buf, obslog.Options{Command: "doctor"}, &bytes.Buffer{})
	child.SetContext(obslog.WithLogger(context.Background(), logger))
	return child, &buf
}

func lastRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &rec))
	return rec
}

// TestLogCommandExitRecordsCommandPathFlagsCodeError covers crn-n5yaz's
// FR-8/FR-9 core acceptance criterion for logCommandExit, the helper both
// root.go's executeAndExit and doctor.go's two os.Exit sites share: the
// logged record carries the full resolved command path (not just the leaf
// name -- that's already the envelope's own "command" key), the exit code,
// and the top-level error text.
func TestLogCommandExitRecordsCommandPathFlagsCodeError(t *testing.T) {
	cmd, buf := newExitTestCmd(t)
	require.NoError(t, cmd.Flags().Parse([]string{"--store", "/some/store", "--json"}))

	logCommandExit(cmd, 2, errors.New("boom"))

	rec := lastRecord(t, buf)
	assert.Equal(t, "exit", rec["kind"])
	assert.Equal(t, "cairn doctor", rec["command_path"])
	assert.Equal(t, float64(2), rec["exit_code"])
	assert.Equal(t, "boom", rec["error"])
}

// TestLogCommandExitCleanRunHasEmptyError covers the nil-error case: a
// successful invocation must still log a record (so a bug report's log tail
// can distinguish "ran, exited 0" from "never got this far"), with error
// rendered as "", never a JSON null that a naive string-consumer would choke
// on.
func TestLogCommandExitCleanRunHasEmptyError(t *testing.T) {
	cmd, buf := newExitTestCmd(t)

	logCommandExit(cmd, 0, nil)

	rec := lastRecord(t, buf)
	assert.Equal(t, float64(0), rec["exit_code"])
	assert.Equal(t, "", rec["error"])
}

// TestLogCommandExitFlagsOnlyExplicitlySetNames covers the design's
// redaction guardrail (OQ3): Flags must list only the *names* of flags the
// invocation actually set -- never a value, and never an unset flag's name
// even though it's registered on the command.
func TestLogCommandExitFlagsOnlyExplicitlySetNames(t *testing.T) {
	cmd, buf := newExitTestCmd(t)
	require.NoError(t, cmd.Flags().Parse([]string{"--store", "/super/secret/path"}))

	logCommandExit(cmd, 0, nil)

	line := strings.TrimSpace(buf.String())
	assert.NotContains(t, line, "/super/secret/path", "flag values must never be logged")

	rec := lastRecord(t, buf)
	flags, ok := rec["flags"].([]any)
	require.True(t, ok, "flags = %v", rec["flags"])
	assert.Equal(t, []any{"store"}, flags, "only explicitly-set flag names, and --json (never set) must be absent")
}

// TestLogCommandExitNoFlagsSetProducesEmptyList confirms a bare invocation
// (no flags explicitly set) logs an empty/absent flags list rather than
// panicking or nil-dereferencing over cmd.Flags().Visit having nothing to
// visit.
func TestLogCommandExitNoFlagsSetProducesEmptyList(t *testing.T) {
	cmd, buf := newExitTestCmd(t)

	logCommandExit(cmd, 0, nil)

	rec := lastRecord(t, buf)
	if flags, ok := rec["flags"]; ok && flags != nil {
		assert.Empty(t, flags)
	}
}
