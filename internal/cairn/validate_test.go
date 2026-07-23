package cairn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePathSegmentRejectsAttacks(t *testing.T) {
	cases := map[string]string{
		"path traversal":   "../../etc/passwd",
		"absolute path":    "/etc/passwd",
		"leading dot":      ".hidden",
		"embedded NUL":     "foo\x00bar",
		"empty string":     "",
		"bare dot-dot":     "..",
		"bare dot":         ".",
		"embedded dot-dot": "foo..bar",
		"trailing slash":   "foo/",
		"embedded control": "foo\x01bar",
		"embedded DEL":     "foo\x7fbar",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, ValidatePathSegment(s), "%q must be rejected", s)
		})
	}
}

// TestValidatePathSegmentRejectsUnicodeDotTricks covers crn-419.5 AC1's
// "unicode dot tricks" corpus entry directly against the validator: values
// built from non-ASCII characters that read as multiple dots, or a literal
// ".." hidden behind a zero-width character -- disguising a dot-based
// traversal attempt from a checker that only understands ASCII '.'.
func TestValidatePathSegmentRejectsUnicodeDotTricks(t *testing.T) {
	cases := map[string]string{
		"doubled fullwidth full stop (U+FF0E)":   "\uFF0E\uFF0E",
		"doubled one-dot leader (U+2024)":        "\u2024\u2024",
		"two-dot leader (U+2025)":                "\u2025",
		"horizontal ellipsis (U+2026)":           "\u2026",
		"doubled ideographic full stop (U+3002)": "\u3002\u3002",
		"dot-dot split by a zero-width space":    "foo.\u200B.bar",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, ValidatePathSegment(s), "%q must be rejected", s)
		})
	}
}

// TestValidatePathSegmentRejectsBracketAnchorDelimiters covers crn-ryi: the
// mol-cairn-librarian dedup-candidate-beads step builds bracket-delimited
// anchor tokens ([pair:ID_LO|ID_HI], [ids:ID_1,ID_2,...]) from raw cairn
// entry IDs and relies on substring-uniqueness of those tokens for
// collision-safe idempotent bd-bead-filing. That invariant only holds if a
// contributor-supplied topic_key or scope tag can never itself contain the
// delimiter characters -- otherwise a crafted topic_key could produce an
// entry ID that collides with, or is a substring of, an unrelated anchor
// token.
func TestValidatePathSegmentRejectsBracketAnchorDelimiters(t *testing.T) {
	cases := map[string]string{
		"open bracket":  "[pair:x",
		"close bracket": "ids]",
		"pipe":          "a|b",
		"comma":         "a,b",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, ValidatePathSegment(s), "%q must be rejected", s)
		})
	}
}

func TestValidatePathSegmentAcceptsSafeValues(t *testing.T) {
	cases := map[string]string{
		"simple word":  "alpha",
		"hyphen":       "my-topic",
		"underscore":   "my_topic",
		"colon":        "rig:web",
		"embedded dot": "v2.0",
		"single char":  "a",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, ValidatePathSegment(s), "%q must be accepted", s)
		})
	}
}
