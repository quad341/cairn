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
