package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandRuns(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, rootCmd.Execute())
}
