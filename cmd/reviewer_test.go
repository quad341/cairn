package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewerFlagCmd returns a throwaway *cobra.Command carrying just the
// --reviewer flag resolveReviewer reads, isolated from the package's shared
// rootCmd/rememberCmd singletons (whose pflag state leaks across tests in
// this binary -- see runRememberWithGC's doc comment) so these tests never
// need to reset global flag state afterward.
func reviewerFlagCmd(t *testing.T, reviewerFlag string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("reviewer", "", "")
	if reviewerFlag != "" {
		require.NoError(t, cmd.Flags().Set("reviewer", reviewerFlag))
	}
	return cmd
}

// TestDefaultReviewerPerTier covers crn-419.4 AC3's three computed defaults.
// Only the "rig" case was previously exercised, indirectly, via the CLI
// test's fixed --scope rig:web (crn-kbf).
func TestDefaultReviewerPerTier(t *testing.T) {
	t.Setenv("GC_RIG", "myrig")
	cases := []struct {
		tier, value, want string
	}{
		{"global", "", "mayor"},
		{"rig", "web", "myrig/architect"},
		{"role", "reviewer", "myrig/reviewer"},
	}
	for _, tc := range cases {
		t.Run(tc.tier, func(t *testing.T) {
			got, err := defaultReviewer(tc.tier, tc.value)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDefaultReviewerSharedTiersRequireGCRig(t *testing.T) {
	for _, tier := range []string{"rig", "role"} {
		t.Run(tier, func(t *testing.T) {
			t.Setenv("GC_RIG", "")
			_, err := defaultReviewer(tier, "web")
			require.Error(t, err, "a rig/role default reviewer can't be computed without $GC_RIG")
			assert.Contains(t, err.Error(), "GC_RIG")
		})
	}
}

func TestDefaultReviewerUnknownTier(t *testing.T) {
	_, err := defaultReviewer("agent", "bot")
	require.Error(t, err, "the private agent tier never reaches requestReview and has no default reviewer")
	assert.Contains(t, err.Error(), `tier "agent"`)
}

// TestResolveReviewerPrecedence covers crn-419.4 AC3's flag > env > default
// order. Previously only the "falls back to computed default" path was
// exercised (crn-kbf): the --reviewer flag and $CAIRN_REVIEWER env overrides
// were untested.
func TestResolveReviewerPrecedence(t *testing.T) {
	t.Setenv("GC_RIG", "myrig")

	t.Run("flag wins over env and default", func(t *testing.T) {
		t.Setenv("CAIRN_REVIEWER", "env-reviewer")
		got, err := resolveReviewer(reviewerFlagCmd(t, "flag-reviewer"), "rig", "web")
		require.NoError(t, err)
		assert.Equal(t, "flag-reviewer", got)
	})

	t.Run("env wins over default when flag is unset", func(t *testing.T) {
		t.Setenv("CAIRN_REVIEWER", "env-reviewer")
		got, err := resolveReviewer(reviewerFlagCmd(t, ""), "rig", "web")
		require.NoError(t, err)
		assert.Equal(t, "env-reviewer", got)
	})

	t.Run("falls back to computed default when flag and env are both unset", func(t *testing.T) {
		t.Setenv("CAIRN_REVIEWER", "")
		got, err := resolveReviewer(reviewerFlagCmd(t, ""), "role", "reviewer")
		require.NoError(t, err)
		assert.Equal(t, "myrig/reviewer", got)
	})
}

// TestValidateReviewerAddress covers validateReviewerAddress, previously at
// 0% coverage (crn-kbf): its only two callers, the --reviewer flag and
// $CAIRN_REVIEWER env paths in resolveReviewer, were never exercised by
// anything.
func TestValidateReviewerAddress(t *testing.T) {
	rejects := map[string]string{
		"empty":            "",
		"whitespace only":  "   ",
		"embedded NUL":     "foo\x00bar",
		"embedded DEL":     "foo\x7fbar",
		"embedded newline": "foo\nbar",
	}
	for name, addr := range rejects {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := validateReviewerAddress(addr)
			require.Error(t, err)
		})
	}

	got, err := validateReviewerAddress("  myrig/architect  ")
	require.NoError(t, err)
	assert.Equal(t, "myrig/architect", got, "a valid address is trimmed but otherwise passed through unchanged")
}

// TestValidateReviewerAddressRejectsLeadingDash covers the leading-'-' guard
// separately from the table above so the specific reason (misread as a flag
// by gc mail send, which sendReviewMail invokes with reviewer as a bare
// positional arg) is asserted, not just "some error occurred".
func TestValidateReviewerAddressRejectsLeadingDash(t *testing.T) {
	_, err := validateReviewerAddress("-x")
	require.Error(t, err, "a reviewer address starting with '-' must be rejected, not passed through to gc mail send as a bare positional arg")
	assert.Contains(t, err.Error(), "start with '-'")
}
