package cmd

import (
	"testing"

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
