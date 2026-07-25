package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveIdentityValidatedPassesCleanTags(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().StringSlice("identity", nil, "")
	require.NoError(t, cmd.Flags().Set("identity", "rig:web,role:reviewer"))

	got, err := resolveIdentityValidated(cmd)

	require.NoError(t, err)
	assert.Equal(t, []string{"rig:web", "role:reviewer"}, got)
}

func TestResolveIdentityValidatedRejectsBadTag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().StringSlice("identity", nil, "")
	require.NoError(t, cmd.Flags().Set("identity", "rig:web,role/bad"))

	_, err := resolveIdentityValidated(cmd)

	require.Error(t, err)
	result := classify(err)
	assert.Equal(t, CategoryInvalidInput, result.Error.Category)
	assert.Equal(t, "role/bad", result.Error.Subject)
}
