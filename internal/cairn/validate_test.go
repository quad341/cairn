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

// TestValidateTitleLengthRejectsOverCap and its siblings cover crn-3476
// FR-3's write-time layer: explicit --title/--summary over the cap must be
// rejected with the same UX as --topic/--scope (CategoryInvalidInput in
// cmd/remember.go), while auto-derived values are silently truncated to the
// cap instead of rejected -- there is no user-supplied value to reject (see
// TestNewEntryTruncatesAutoDerivedTitleAndSummaryToCap). Read time (Prime)
// then unconditionally re-truncates both explicit and auto-derived values
// regardless of what's on disk (NFR-3, see
// TestPrimeTruncatesOversizedTitleAndSummaryToCap), covering entries written
// before the cap shipped or written directly to disk, bypassing
// cmd/remember's write-time validation entirely. titleCap/summaryCap are
// package-level vars per FR-7, so tests override them rather than depend on
// the "starting point, not calibrated" defaults.
func TestValidateTitleLengthRejectsOverCap(t *testing.T) {
	orig := titleCap
	titleCap = 5
	defer func() { titleCap = orig }()

	assert.Error(t, ValidateTitleLength("123456"), "6 runes must be rejected against a cap of 5")
}

func TestValidateTitleLengthAcceptsAtCap(t *testing.T) {
	orig := titleCap
	titleCap = 5
	defer func() { titleCap = orig }()

	assert.NoError(t, ValidateTitleLength("12345"), "exactly at the cap must be accepted")
}

// TestValidateTitleLengthCountsRunesNotBytes proves the cap is a rune count,
// not a byte count -- a 3-rune multi-byte title must be accepted against a
// cap of 3 even though it is 9 bytes long.
func TestValidateTitleLengthCountsRunesNotBytes(t *testing.T) {
	orig := titleCap
	titleCap = 3
	defer func() { titleCap = orig }()

	assert.NoError(t, ValidateTitleLength("日本語"), "3 runes must be accepted against a cap of 3 even though it is 9 bytes")
}

func TestValidateSummaryLengthRejectsOverCap(t *testing.T) {
	orig := summaryCap
	summaryCap = 5
	defer func() { summaryCap = orig }()

	assert.Error(t, ValidateSummaryLength("123456"), "6 runes must be rejected against a cap of 5")
}

func TestValidateSummaryLengthAcceptsAtCap(t *testing.T) {
	orig := summaryCap
	summaryCap = 5
	defer func() { summaryCap = orig }()

	assert.NoError(t, ValidateSummaryLength("12345"), "exactly at the cap must be accepted")
}
