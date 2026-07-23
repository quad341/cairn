package cairn

import (
	"errors"
	"strings"
)

// ValidatePathSegment reports whether s is safe to use as a single
// filesystem path segment -- a topic_key or one scope tag supplied by a
// contributor via `cairn remember`, before it is ever used to build a path
// under the store root (DESIGN.md §7: an unreviewed write gets the
// strictest guard). It rejects an empty value, a value containing a slash,
// a value containing two consecutive dots, a value starting with a dot,
// a value containing any non-ASCII, control, or null byte, and a value
// containing '[', ']', '|', or ',' -- non-ASCII runes are rejected outright
// rather than normalized, since Unicode confusables (lookalike dots,
// zero-width characters) can otherwise disguise a traversal attempt from a
// checker that only understands ASCII '.'. The bracket/pipe/comma rejection
// (crn-ryi) preserves collision-safety for the mol-cairn-librarian dedup
// step's bracket-delimited anchor tokens ([pair:ID_LO|ID_HI],
// [ids:ID_1,ID_2,...]), which rely on those characters never appearing
// inside a cairn entry ID for their substring-uniqueness argument to hold.
func ValidatePathSegment(s string) error {
	if s == "" {
		return errors.New("must not be empty")
	}
	if strings.ContainsRune(s, '/') {
		return errors.New("must not contain a slash")
	}
	if strings.Contains(s, "..") {
		return errors.New("must not contain two consecutive dots")
	}
	if strings.HasPrefix(s, ".") {
		return errors.New("must not start with a dot")
	}
	if strings.ContainsAny(s, "[]|,") {
		return errors.New("must not contain '[', ']', '|', or ','")
	}
	for _, r := range s {
		if r < 0x20 || r >= 0x7f {
			return errors.New("must not contain a non-ASCII, control, or null byte")
		}
	}
	return nil
}
