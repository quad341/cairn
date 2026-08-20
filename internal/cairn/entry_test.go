package cairn

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quad341/cairn/internal/obslog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

const sampleEntry = `+++
id = "test/one"
title = "One"
title_source = "authored"
summary = "s"
summary_source = "derived"
type = "reference"
topic_key = "test/one"
scope = ["rig:alpha"]

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body here
`

func TestParseEntry(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/one.md", sampleEntry))
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Equal(t, "test/one", e.ID)
	assert.Equal(t, "One", e.Title)
	assert.Equal(t, MetadataSourceAuthored, e.TitleSource)
	assert.Equal(t, MetadataSourceDerived, e.SummarySource)
	assert.Equal(t, []string{"rig:alpha"}, e.Scope)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Len(t, e.Anchor.Paths, 1)
	assert.Equal(t, "body here\n", e.Body)
}

func TestParseEntryLegacyMetadataSourcesRemainEmpty(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/legacy.md",
		"+++\nid = \"legacy\"\ntitle = \"Legacy\"\nsummary = \"summary\"\n+++\nbody\n"))
	require.NoError(t, err)
	assert.Empty(t, e.TitleSource)
	assert.Empty(t, e.SummarySource)
}

func TestParseEntryNoFrontmatter(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "# just markdown\n"))
	assert.Nil(t, e)
	require.ErrorIs(t, err, errNotEntry)
}

func TestParseEntryUnterminated(t *testing.T) {
	_, err := ParseEntry(writeFile(t, t.TempDir(), "x.md", "+++\nid = \"a\"\nno closing fence\n"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, errNotEntry) // a real parse error, not "not an entry"
}

func TestIterEntriesTolerantCleanStoreNoFailures(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", sampleEntry)
	writeFile(t, dir, "rig/alpha/r.md", "+++\nid = \"r1\"\ntitle = \"r1\"\nscope = [\"rig:alpha\"]\n+++\nx\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err)
	assert.Empty(t, failures)
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	assert.True(t, ids["test/one"] && ids["r1"])
}

func TestIterEntriesTolerantCollectsParseFailures(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/good.md", sampleEntry)
	badPath := writeFile(t, dir, "global/bad.md", "+++\nid = \"a\"\nno closing fence\n")

	entries, failures, err := IterEntriesTolerant(dir)
	require.NoError(t, err, "a malformed entry must not abort the tolerant walk")

	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	assert.True(t, ids["test/one"], "the well-formed entry must still be collected")

	require.Len(t, failures, 1)
	assert.Equal(t, badPath, failures[0].Path)
	assert.Error(t, failures[0].Err)
	assert.NotErrorIs(t, failures[0].Err, errNotEntry)
}

func TestIterEntriesStillAbortsOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/good.md", sampleEntry)
	writeFile(t, dir, "global/bad.md", "+++\nid = \"a\"\nno closing fence\n")

	_, err := IterEntries(dir)
	require.Error(t, err, "IterEntries' existing abort-on-first-error contract must be unchanged")
}

func TestParseEntryNewFieldsZeroValues(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/one.md", sampleEntry))
	require.NoError(t, err)
	assert.Empty(t, e.Kind, `unset Kind means "note" by convention`)
	assert.False(t, e.AutoActionable)
	assert.Zero(t, e.RecurrenceCount)
	assert.Empty(t, e.PromotedBeadID)
	assert.Empty(t, e.LastRecalledAt)
}

const sampleEntryAllFields = `+++
id = "test/all"
title = "All"
kind = "remediation"
auto_actionable = true
recurrence_count = 3
promoted_bead_id = "crn-abcd"
last_recalled_at = "2026-07-20T00:00:00Z"

[anchor]
type = "none"
+++

body here
`

func TestParseEntryNewFieldsRoundTrip(t *testing.T) {
	e, err := ParseEntry(writeFile(t, t.TempDir(), "global/all.md", sampleEntryAllFields))
	require.NoError(t, err)
	assert.Equal(t, "remediation", e.Kind)
	assert.True(t, e.AutoActionable)
	assert.Equal(t, 3, e.RecurrenceCount)
	assert.Equal(t, "crn-abcd", e.PromotedBeadID)
	assert.Equal(t, "2026-07-20T00:00:00Z", e.LastRecalledAt)
}

func TestEntryMarshalRoundTripsNewFields(t *testing.T) {
	e := &Entry{
		ID:              "rt/1",
		Title:           "RT",
		Kind:            "remediation",
		AutoActionable:  true,
		RecurrenceCount: 3,
		PromotedBeadID:  "crn-abcd",
		LastRecalledAt:  "2026-07-20T00:00:00Z",
		Anchor:          Anchor{Type: "none"},
	}
	raw, err := e.marshal()
	require.NoError(t, err)

	e2, err := ParseEntry(writeFile(t, t.TempDir(), "global/rt.md", string(raw)))
	require.NoError(t, err)
	assert.Equal(t, "remediation", e2.Kind)
	assert.True(t, e2.AutoActionable)
	assert.Equal(t, 3, e2.RecurrenceCount)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID)
	assert.Equal(t, "2026-07-20T00:00:00Z", e2.LastRecalledAt)
}

func TestEntryMarshalOmitsZeroValueNewFields(t *testing.T) {
	e := &Entry{ID: "rt/2", Title: "RT2", Anchor: Anchor{Type: "none"}}
	raw, err := e.marshal()
	require.NoError(t, err)

	s := string(raw)
	assert.NotContains(t, s, "kind", "zero-value Kind must be omitted from marshaled output")
	assert.NotContains(t, s, "auto_actionable", "zero-value AutoActionable must be omitted from marshaled output")
	assert.NotContains(t, s, "recurrence_count", "zero-value RecurrenceCount must be omitted from marshaled output")
	assert.NotContains(t, s, "promoted_bead_id", "zero-value PromotedBeadID must be omitted from marshaled output")
	assert.NotContains(t, s, "last_recalled_at", "zero-value LastRecalledAt must be omitted from marshaled output")
}

func TestWriteBackRoundTrip(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", sampleEntry)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.Anchor.Fingerprint = "abc123"
	e.VerifiedAt = "2026-07-19"
	require.NoError(t, e.WriteBack())

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "abc123", e2.Anchor.Fingerprint)
	assert.Equal(t, "2026-07-19", e2.VerifiedAt)
	assert.Equal(t, e.ID, e2.ID)
	assert.Equal(t, e.Body, e2.Body)
}

const writeBackFixtureUnverified = `+++
id = "wb/unverified"
title = "Unverified"
summary = "s"
type = "reference"
topic_key = "wb/unverified"
scope = []

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body text
`

// TestWriteBackFirstVerifyInsertsAndPreservesRest covers crn-6az.5.1's core
// claim: a first-ever verify (neither verified_at nor fingerprint present
// yet) must insert both fields -- verified_at immediately before [anchor],
// fingerprint inside the anchor table -- while every other original line,
// including an empty `scope = []`, survives verbatim. A value-equality
// round-trip check (TestWriteBackRoundTrip) can't tell "surgically patched"
// from "fully re-encoded"; only a byte-level comparison against the original
// text can.
func TestWriteBackFirstVerifyInsertsAndPreservesRest(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureUnverified)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Empty(t, e.VerifiedAt, "fixture must start unverified")
	require.Empty(t, e.Anchor.Fingerprint, "fixture must start unverified")

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "abc123"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)

	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")
	for _, l := range beforeLines {
		if l == "" {
			continue
		}
		assert.Contains(t, afterLines, l, "every original line must survive verbatim: %q", l)
	}
	assert.Contains(t, string(after), "scope = []", "empty scope must not be dropped or reformatted")

	idx := func(lines []string, target string) int {
		for i, l := range lines {
			if l == target {
				return i
			}
		}
		return -1
	}
	anchorAt := idx(afterLines, "[anchor]")
	vaAt := idx(afterLines, `verified_at = "2026-07-19"`)
	require.NotEqual(t, -1, anchorAt)
	require.NotEqual(t, -1, vaAt)
	assert.Equal(t, anchorAt-1, vaAt, "verified_at must be inserted immediately before [anchor]")
	assert.Contains(t, afterLines, `fingerprint = "abc123"`, "fingerprint must be inserted into the anchor table")

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-19", e2.VerifiedAt)
	assert.Equal(t, "abc123", e2.Anchor.Fingerprint)
	assert.Equal(t, "body text\n", e2.Body)
}

const writeBackFixtureAlreadyVerified = `+++
id = "wb/verified"
title = "Verified"
scope = []
verified_at = "2026-01-01"

[anchor]
type = "files"
repo = "/tmp/x"
fingerprint = "oldfp000"
+++

body text
`

// TestWriteBackSecondVerifyUpdatesInPlace covers a re-verify: both fields
// already present must update in place with zero line-count delta, not grow
// the file or reorder anything.
func TestWriteBackSecondVerifyUpdatesInPlace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureAlreadyVerified)
	before, err := os.ReadFile(p)
	require.NoError(t, err)
	beforeLines := strings.Split(string(before), "\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Equal(t, "2026-01-01", e.VerifiedAt)
	require.Equal(t, "oldfp000", e.Anchor.Fingerprint)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp111"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	afterLines := strings.Split(string(after), "\n")

	require.Equal(t, len(beforeLines), len(afterLines), "an in-place update must not change the line count")
	for i := range beforeLines {
		if strings.Contains(beforeLines[i], "verified_at") || strings.Contains(beforeLines[i], "fingerprint") {
			continue
		}
		assert.Equal(t, beforeLines[i], afterLines[i], "line %d is unrelated to the patched fields and must be byte-identical", i)
	}
	assert.NotContains(t, string(after), "2026-01-01")
	assert.NotContains(t, string(after), "oldfp000")
	assert.Contains(t, string(after), `verified_at = "2026-07-19"`)
	assert.Contains(t, string(after), `fingerprint = "newfp111"`)
}

const writeBackFixtureIndentedReplace = `+++
id = "wb/indented-replace"
title = "IndentedReplace"
scope = []

[anchor]
    type = "files"
    repo = "/tmp/x"
    fingerprint = "oldfp"
+++

body
`

// TestWriteBackPreservesAnchorIndentOnReplace covers replacing an existing,
// non-default-indented fingerprint line: its own indentation must survive,
// even though the codebase's own encoder never produces indented tables --
// WriteBack patches whatever text is actually on disk, hand-edited or not.
func TestWriteBackPreservesAnchorIndentOnReplace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureIndentedReplace)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(after), "    fingerprint = \"newfp\"", "the replaced line must keep its original 4-space indent")
	assert.Contains(t, string(after), "    type = \"files\"", "sibling lines must stay untouched")
	assert.Contains(t, string(after), "    repo = \"/tmp/x\"", "sibling lines must stay untouched")
	assert.NotContains(t, string(after), "oldfp")
}

const writeBackFixtureIndentedAppend = `+++
id = "wb/indented-append"
title = "IndentedAppend"
scope = []

[anchor]
    type = "files"
    repo = "/tmp/x"
+++

body
`

// TestWriteBackMatchesAnchorIndentOnAppend covers a first-ever verify inside
// an anchor block whose existing keys are indented: the newly appended
// fingerprint line must match that indentation, not default to none.
func TestWriteBackMatchesAnchorIndentOnAppend(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureIndentedAppend)
	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Empty(t, e.Anchor.Fingerprint)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "newfp"
	require.NoError(t, e.WriteBack())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(after), "    fingerprint = \"newfp\"", "an appended line must match the indentation of its sibling keys")
}

const writeBackFixtureNoAnchor = "+++\nid = \"wb/no-anchor\"\ntitle = \"NoAnchor\"\nscope = []\n+++\nbody\n"

// TestWriteBackMissingAnchorTableErrorsWithoutWriting covers the one
// hard-failure path: with no [anchor] table to patch into, WriteBack must
// return an error naming the entry id and leave the file exactly as it was
// -- never a partial write.
func TestWriteBackMissingAnchorTableErrorsWithoutWriting(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoAnchor)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.VerifiedAt = "2026-07-19"
	e.Anchor.Fingerprint = "abc123"
	err = e.WriteBack()
	require.Error(t, err)
	assert.Contains(t, err.Error(), e.ID, "the error must name the entry id")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed WriteBack must leave the file byte-identical -- no partial write")
}

func TestWriteBackRecurrenceCountRoundTrip(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", sampleEntry)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.RecurrenceCount = 5
	require.NoError(t, e.WriteBackRecurrenceCount())

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, 5, e2.RecurrenceCount)
	assert.Equal(t, e.ID, e2.ID)
	assert.Equal(t, e.Body, e2.Body)
}

const writeBackFixtureNoRecurrence = `+++
id = "wb/no-recurrence"
title = "NoRecurrence"
summary = "s"
type = "reference"
topic_key = "wb/no-recurrence"
scope = []

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body text
`

// TestWriteBackRecurrenceCountFirstIncrementAppendsAndPreservesRest mirrors
// TestWriteBackFirstVerifyInsertsAndPreservesRest one field over: a first-ever
// increment (recurrence_count absent) must insert it immediately before
// [anchor] -- the same insertion point patchVerification uses for
// verified_at, since both are top-level fields patched via the same
// [anchor]-boundary-finding shape -- while every other original line
// survives verbatim.
func TestWriteBackRecurrenceCountFirstIncrementAppendsAndPreservesRest(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoRecurrence)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Equal(t, 0, e.RecurrenceCount, "fixture must start with no recurrence_count")

	e.RecurrenceCount = 1
	require.NoError(t, e.WriteBackRecurrenceCount())

	after, err := os.ReadFile(p)
	require.NoError(t, err)

	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")
	for _, l := range beforeLines {
		if l == "" {
			continue
		}
		assert.Contains(t, afterLines, l, "every original line must survive verbatim: %q", l)
	}
	assert.Contains(t, string(after), "scope = []", "empty scope must not be dropped or reformatted")

	idx := func(lines []string, target string) int {
		for i, l := range lines {
			if l == target {
				return i
			}
		}
		return -1
	}
	anchorAt := idx(afterLines, "[anchor]")
	rcAt := idx(afterLines, "recurrence_count = 1")
	require.NotEqual(t, -1, anchorAt)
	require.NotEqual(t, -1, rcAt)
	assert.Equal(t, anchorAt-1, rcAt, "recurrence_count must be inserted immediately before [anchor]")

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, 1, e2.RecurrenceCount)
	assert.Equal(t, "body text\n", e2.Body)
}

const writeBackFixtureRecurrenceAlreadySet = `+++
id = "wb/recurrence-set"
title = "RecurrenceSet"
scope = []
recurrence_count = 3

[anchor]
type = "files"
repo = "/tmp/x"
+++

body text
`

// TestWriteBackRecurrenceCountSecondIncrementUpdatesInPlace mirrors
// TestWriteBackSecondVerifyUpdatesInPlace: recurrence_count already present
// must update in place with zero line-count delta, not grow the file or
// reorder anything.
func TestWriteBackRecurrenceCountSecondIncrementUpdatesInPlace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureRecurrenceAlreadySet)
	before, err := os.ReadFile(p)
	require.NoError(t, err)
	beforeLines := strings.Split(string(before), "\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Equal(t, 3, e.RecurrenceCount)

	e.RecurrenceCount = 4
	require.NoError(t, e.WriteBackRecurrenceCount())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	afterLines := strings.Split(string(after), "\n")

	require.Equal(t, len(beforeLines), len(afterLines), "an in-place update must not change the line count")
	for i := range beforeLines {
		if strings.Contains(beforeLines[i], "recurrence_count") {
			continue
		}
		assert.Equal(t, beforeLines[i], afterLines[i], "line %d is unrelated to the patched field and must be byte-identical", i)
	}
	assert.NotContains(t, string(after), "recurrence_count = 3")
	assert.Contains(t, string(after), "recurrence_count = 4")
}

// TestWriteBackRecurrenceCountMissingAnchorTableErrorsWithoutWriting mirrors
// TestWriteBackMissingAnchorTableErrorsWithoutWriting, reusing the same
// no-[anchor] fixture: WriteBackRecurrenceCount shares writeBackPatched's
// same hard-failure path, so it must behave identically -- an error naming
// the entry id, and the file left exactly as it was, never a partial write.
func TestWriteBackRecurrenceCountMissingAnchorTableErrorsWithoutWriting(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoAnchor)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.RecurrenceCount = 1
	err = e.WriteBackRecurrenceCount()
	require.Error(t, err)
	assert.Contains(t, err.Error(), e.ID, "the error must name the entry id")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed WriteBackRecurrenceCount must leave the file byte-identical -- no partial write")
}

func TestWriteBackPromotedBeadIDRoundTrip(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", sampleEntry)
	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID())

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID)
	assert.Equal(t, e.ID, e2.ID)
	assert.Equal(t, e.Body, e2.Body)
}

const writeBackFixtureNoPromotedBeadID = `+++
id = "wb/no-promoted"
title = "NoPromoted"
summary = "s"
type = "reference"
topic_key = "wb/no-promoted"
scope = []

[anchor]
type = "files"
repo = "/tmp/x"
paths = ["a.go"]
+++

body text
`

// TestWriteBackPromotedBeadIDFirstSetAppendsAndPreservesRest mirrors
// TestWriteBackRecurrenceCountFirstIncrementAppendsAndPreservesRest one field
// over: a first-ever promotion (promoted_bead_id absent) must insert it
// immediately before [anchor] -- the same insertion point patchVerification
// and patchRecurrenceCount use, since all three are top-level fields patched
// via the same [anchor]-boundary-finding shape -- while every other original
// line survives verbatim.
func TestWriteBackPromotedBeadIDFirstSetAppendsAndPreservesRest(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoPromotedBeadID)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Empty(t, e.PromotedBeadID, "fixture must start with no promoted_bead_id")

	e.PromotedBeadID = "crn-abcd"
	require.NoError(t, e.WriteBackPromotedBeadID())

	after, err := os.ReadFile(p)
	require.NoError(t, err)

	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")
	for _, l := range beforeLines {
		if l == "" {
			continue
		}
		assert.Contains(t, afterLines, l, "every original line must survive verbatim: %q", l)
	}
	assert.Contains(t, string(after), "scope = []", "empty scope must not be dropped or reformatted")

	idx := func(lines []string, target string) int {
		for i, l := range lines {
			if l == target {
				return i
			}
		}
		return -1
	}
	anchorAt := idx(afterLines, "[anchor]")
	pbAt := idx(afterLines, `promoted_bead_id = "crn-abcd"`)
	require.NotEqual(t, -1, anchorAt)
	require.NotEqual(t, -1, pbAt)
	assert.Equal(t, anchorAt-1, pbAt, "promoted_bead_id must be inserted immediately before [anchor]")

	e2, err := ParseEntry(p)
	require.NoError(t, err)
	assert.Equal(t, "crn-abcd", e2.PromotedBeadID)
	assert.Equal(t, "body text\n", e2.Body)
}

const writeBackFixturePromotedBeadIDAlreadySet = `+++
id = "wb/promoted-set"
title = "PromotedSet"
scope = []
promoted_bead_id = "crn-old1"

[anchor]
type = "files"
repo = "/tmp/x"
+++

body text
`

// TestWriteBackPromotedBeadIDSecondSetUpdatesInPlace mirrors
// TestWriteBackRecurrenceCountSecondIncrementUpdatesInPlace: promoted_bead_id
// already present must update in place with zero line-count delta, not grow
// the file or reorder anything.
func TestWriteBackPromotedBeadIDSecondSetUpdatesInPlace(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixturePromotedBeadIDAlreadySet)
	before, err := os.ReadFile(p)
	require.NoError(t, err)
	beforeLines := strings.Split(string(before), "\n")

	e, err := ParseEntry(p)
	require.NoError(t, err)
	require.Equal(t, "crn-old1", e.PromotedBeadID)

	e.PromotedBeadID = "crn-new2"
	require.NoError(t, e.WriteBackPromotedBeadID())

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	afterLines := strings.Split(string(after), "\n")

	require.Equal(t, len(beforeLines), len(afterLines), "an in-place update must not change the line count")
	for i := range beforeLines {
		if strings.Contains(beforeLines[i], "promoted_bead_id") {
			continue
		}
		assert.Equal(t, beforeLines[i], afterLines[i], "line %d is unrelated to the patched field and must be byte-identical", i)
	}
	assert.NotContains(t, string(after), "crn-old1")
	assert.Contains(t, string(after), `promoted_bead_id = "crn-new2"`)
}

// TestWriteBackPromotedBeadIDMissingAnchorTableErrorsWithoutWriting mirrors
// TestWriteBackRecurrenceCountMissingAnchorTableErrorsWithoutWriting, reusing
// the same no-[anchor] fixture: WriteBackPromotedBeadID shares
// writeBackPatched's same hard-failure path, so it must behave identically --
// an error naming the entry id, and the file left exactly as it was, never a
// partial write.
func TestWriteBackPromotedBeadIDMissingAnchorTableErrorsWithoutWriting(t *testing.T) {
	p := writeFile(t, t.TempDir(), "global/one.md", writeBackFixtureNoAnchor)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	e, err := ParseEntry(p)
	require.NoError(t, err)

	e.PromotedBeadID = "crn-abcd"
	err = e.WriteBackPromotedBeadID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), e.ID, "the error must name the entry id")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed WriteBackPromotedBeadID must leave the file byte-identical -- no partial write")
}

const (
	globalEntry = "+++\nid = \"g\"\ntitle = \"g\"\nscope = []\n+++\nx\n"
	alphaEntry  = "+++\nid = \"r\"\ntitle = \"r\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	betaEntry   = "+++\nid = \"t\"\ntitle = \"t\"\nscope = [\"rig:beta\"]\n+++\nx\n"
	crossEntry  = "+++\nid = \"x\"\ntitle = \"x\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"
)

func TestVisible(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)
	writeFile(t, dir, "rig/alpha/r.md", alphaEntry)
	writeFile(t, dir, "rig/beta/t.md", betaEntry)
	writeFile(t, dir, "role/investigator/x.md", crossEntry)

	seen := func(identity []string) map[string]bool {
		vs, err := Visible(t.Context(), dir, identity)
		require.NoError(t, err)
		m := map[string]bool{}
		for _, e := range vs {
			m[e.ID] = true
		}
		return m
	}

	inv := seen([]string{"rig:alpha", "role:investigator"})
	assert.True(t, inv["g"] && inv["r"] && inv["x"], "alpha-investigator should see g, r, x")
	assert.False(t, inv["t"], "alpha-investigator should not see the beta entry")

	bare := seen(nil)
	assert.True(t, bare["g"], "bare identity should see global")
	assert.False(t, bare["r"] || bare["t"] || bare["x"], "bare identity should see only global")

	builder := seen([]string{"rig:alpha", "role:builder"})
	assert.True(t, builder["g"] && builder["r"], "alpha-builder should see g and r")
	assert.False(t, builder["x"] || builder["t"], "alpha-builder should not see x or t")
}

const (
	lessSpecificShared = "+++\nid = \"s1\"\ntitle = \"s1\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	moreSpecificShared = "+++\nid = \"s2\"\ntitle = \"s2\"\ntopic_key = \"shared\"\nscope = [\"rig:alpha\", \"role:investigator\"]\n+++\nx\n"

	earlyVerifiedShared = "+++\nid = \"v1\"\ntitle = \"v1\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-01-01\"\n+++\nx\n"
	lateVerifiedShared  = "+++\nid = \"v2\"\ntitle = \"v2\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\nverified_at = \"2026-06-01\"\n+++\nx\n"

	tiebreakLowID  = "+++\nid = \"c1\"\ntitle = \"c1\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	tiebreakHighID = "+++\nid = \"c2\"\ntitle = \"c2\"\ntopic_key = \"tk3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"

	untopiced1 = "+++\nid = \"u1\"\ntitle = \"u1\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced2 = "+++\nid = \"u2\"\ntitle = \"u2\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	untopiced3 = "+++\nid = \"u3\"\ntitle = \"u3\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestVisibleShadowsBySpecificity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/s1.md", lessSpecificShared)
	writeFile(t, dir, "role/investigator/s2.md", moreSpecificShared)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha", "role:investigator"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["s2"], "the 2-tag entry must be visible")
	assert.False(t, ids["s1"], "the 1-tag entry must be shadowed by the more specific one")
}

func TestVisibleShadowTiebreakVerifiedAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/v1.md", earlyVerifiedShared)
	writeFile(t, dir, "rig/alpha/v2.md", lateVerifiedShared)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["v2"], "the more recently verified entry must win")
	assert.False(t, ids["v1"], "the earlier-verified entry must be shadowed")
}

func TestVisibleRetainsIndistinguishableRevisions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/c2.md", tiebreakHighID)
	writeFile(t, dir, "rig/alpha/c1.md", tiebreakLowID)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["c1"], "one indistinguishable revision must remain visible")
	assert.True(t, ids["c2"], "ID order must not fabricate a winner")
}

func TestResolveTopicsMeaningfulPrecedenceProducesSingleWinner(t *testing.T) {
	tests := []struct {
		name       string
		candidates []*Entry
		winner     string
	}{
		{
			name: "override",
			candidates: []*Entry{
				{ID: "old", TopicKey: "t", Scope: []string{"rig:x"}, VerifiedAt: "2026-08-15", CreatedAt: "2026-08-15"},
				{ID: "new", TopicKey: "t", Scope: []string{"rig:x"}, OverriddenDuplicateOf: "old"},
			},
			winner: "new",
		},
		{
			name: "scope specificity",
			candidates: []*Entry{
				{ID: "rig", TopicKey: "t", Scope: []string{"rig:x"}},
				{ID: "cross", TopicKey: "t", Scope: []string{"rig:x", "role:y"}},
			},
			winner: "cross",
		},
		{
			name: "verified at",
			candidates: []*Entry{
				{ID: "early", TopicKey: "t", Scope: []string{"rig:x"}, VerifiedAt: "2026-08-14"},
				{ID: "late", TopicKey: "t", Scope: []string{"rig:x"}, VerifiedAt: "2026-08-15"},
			},
			winner: "late",
		},
		{
			name: "created at",
			candidates: []*Entry{
				{ID: "early", TopicKey: "t", Scope: []string{"rig:x"}, CreatedAt: "2026-08-15T01:00:00Z"},
				{ID: "late", TopicKey: "t", Scope: []string{"rig:x"}, CreatedAt: "2026-08-15T02:00:00Z"},
			},
			winner: "late",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := ResolveTopics(tt.candidates)
			require.Len(t, resolution.Entries, 1)
			assert.Equal(t, tt.winner, resolution.Entries[0].Entry.ID)
			assert.Nil(t, resolution.Entries[0].Conflict)
			assert.Empty(t, resolution.Conflicts)
		})
	}
}

func TestResolveTopicsAnnotatesEveryIndistinguishableRevision(t *testing.T) {
	b := &Entry{ID: "b", TopicKey: "t", Scope: []string{"rig:x"}, CreatedAt: "2026-08-15"}
	a := &Entry{ID: "a", TopicKey: "t", Scope: []string{"rig:x"}, CreatedAt: "2026-08-15"}

	resolution := ResolveTopics([]*Entry{b, a})

	require.Len(t, resolution.Entries, 2)
	require.Len(t, resolution.Conflicts, 1)
	want := TopicConflict{TopicKey: "t", EntryIDs: []string{"a", "b"}, Reason: "indistinguishable"}
	assert.Equal(t, want, resolution.Conflicts[0])
	for _, resolved := range resolution.Entries {
		require.NotNil(t, resolved.Conflict, "each contested hit must carry its conflict")
		assert.Equal(t, want, *resolved.Conflict)
	}
}

func TestResolveTopicsConflictContainsOnlyTopRankedTies(t *testing.T) {
	topA := &Entry{ID: "top-a", TopicKey: "t", Scope: []string{"rig:x", "role:y"}, CreatedAt: "2026-08-15"}
	lower := &Entry{ID: "lower", TopicKey: "t", Scope: []string{"rig:x"}, CreatedAt: "2026-08-16"}
	topB := &Entry{ID: "top-b", TopicKey: "t", Scope: []string{"rig:x", "role:y"}, CreatedAt: "2026-08-15"}

	resolution := ResolveTopics([]*Entry{topA, lower, topB})
	require.Len(t, resolution.Entries, 2)
	require.Len(t, resolution.Conflicts, 1)
	assert.Equal(t, []string{"top-a", "top-b"}, resolution.Conflicts[0].EntryIDs)
	for _, resolved := range resolution.Entries {
		assert.NotEqual(t, "lower", resolved.Entry.ID, "lower specificity must be suppressed before conflict detection")
	}
}

func TestResolveTopicsUntopicedEntriesNeverConflict(t *testing.T) {
	resolution := ResolveTopics([]*Entry{{ID: "a"}, {ID: "b"}})
	require.Len(t, resolution.Entries, 2)
	assert.Empty(t, resolution.Conflicts)
	assert.Nil(t, resolution.Entries[0].Conflict)
	assert.Nil(t, resolution.Entries[1].Conflict)
}

func TestVisibleUntopicedNeverShadow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/u1.md", untopiced1)
	writeFile(t, dir, "rig/alpha/u2.md", untopiced2)
	writeFile(t, dir, "rig/alpha/u3.md", untopiced3)

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["u1"] && ids["u2"] && ids["u3"], "entries without a topic_key must never shadow one another")
}

// parseFixture parses a fixture markdown const into an *Entry, independent of
// IterEntries' scope-dir layout — ShadowMap takes an entry slice directly.
func parseFixture(t *testing.T, content string) *Entry {
	t.Helper()
	e, err := ParseEntry(writeFile(t, t.TempDir(), "e.md", content))
	require.NoError(t, err)
	return e
}

const (
	incomparableRig  = "+++\nid = \"i1\"\ntitle = \"i1\"\ntopic_key = \"inc\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	incomparableRole = "+++\nid = \"i2\"\ntitle = \"i2\"\ntopic_key = \"inc\"\nscope = [\"role:builder\"]\n+++\nx\n"

	chainOneTag    = "+++\nid = \"ch1\"\ntitle = \"ch1\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
	chainTwoTags   = "+++\nid = \"ch2\"\ntitle = \"ch2\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\"]\n+++\nx\n"
	chainThreeTags = "+++\nid = \"ch3\"\ntitle = \"ch3\"\ntopic_key = \"chain\"\nscope = [\"rig:alpha\", \"role:builder\", \"agent:x\"]\n+++\nx\n"

	globalShared = "+++\nid = \"gs\"\ntitle = \"gs\"\ntopic_key = \"glob\"\nscope = []\n+++\nx\n"
	scopedShared = "+++\nid = \"rs\"\ntitle = \"rs\"\ntopic_key = \"glob\"\nscope = [\"rig:alpha\"]\n+++\nx\n"
)

func TestShadowMapSuperset(t *testing.T) {
	s1 := parseFixture(t, lessSpecificShared)
	s2 := parseFixture(t, moreSpecificShared)

	sm := ShadowMap(context.Background(), []*Entry{s1, s2})

	require.Contains(t, sm, "s1", "the 1-tag entry must be shadowed")
	assert.Equal(t, "s2", sm["s1"].ID, "the 1-tag entry must be shadowed by the 2-tag superset entry")
	assert.NotContains(t, sm, "s2", "the more specific entry must not appear as shadowed")
}

func TestShadowMapIncomparableScopesNeverShadow(t *testing.T) {
	i1 := parseFixture(t, incomparableRig)
	i2 := parseFixture(t, incomparableRole)

	sm := ShadowMap(context.Background(), []*Entry{i1, i2})

	assert.NotContains(t, sm, "i1", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
	assert.NotContains(t, sm, "i2", "neither-subset-nor-superset scopes must never shadow, even on an equal-tag-count tie")
}

func TestShadowMapTiebreakOnEqualScope(t *testing.T) {
	v1 := parseFixture(t, earlyVerifiedShared)
	v2 := parseFixture(t, lateVerifiedShared)

	sm := ShadowMap(context.Background(), []*Entry{v1, v2})

	require.Contains(t, sm, "v1", "the earlier-verified entry must be shadowed")
	assert.Equal(t, "v2", sm["v1"].ID, "the earlier-verified entry must be shadowed by the later-verified one")
	assert.NotContains(t, sm, "v2", "the later-verified (winning) entry must not appear as shadowed")
}

func TestShadowMapChainReportsMostSpecific(t *testing.T) {
	ch1 := parseFixture(t, chainOneTag)
	ch2 := parseFixture(t, chainTwoTags)
	ch3 := parseFixture(t, chainThreeTags)

	sm := ShadowMap(context.Background(), []*Entry{ch1, ch2, ch3})

	require.Contains(t, sm, "ch1")
	assert.Equal(t, "ch3", sm["ch1"].ID, "the 1-tag entry must be shadowed by the most specific entry in the chain, not its nearest neighbor")
	require.Contains(t, sm, "ch2")
	assert.Equal(t, "ch3", sm["ch2"].ID, "the 2-tag entry must be shadowed by the most specific entry in the chain")
	assert.NotContains(t, sm, "ch3", "the most specific entry in the chain must not appear as shadowed")
}

func TestShadowMapUntopicedNeverShadow(t *testing.T) {
	u1 := parseFixture(t, untopiced1)
	u2 := parseFixture(t, untopiced2)
	u3 := parseFixture(t, untopiced3)

	sm := ShadowMap(context.Background(), []*Entry{u1, u2, u3})

	assert.Empty(t, sm, "entries without a topic_key must never appear in the shadow map")
}

func TestShadowMapGlobalShadowedByScoped(t *testing.T) {
	g := parseFixture(t, globalShared)
	r := parseFixture(t, scopedShared)

	sm := ShadowMap(context.Background(), []*Entry{g, r})

	require.Contains(t, sm, "gs", "the global (empty-scope) entry must be shadowed by the scoped one")
	assert.Equal(t, "rs", sm["gs"].ID)
	assert.NotContains(t, sm, "rs", "the scoped entry must not appear as shadowed by the global one")
}

func TestMoreSpecificReasonScopeSize(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x", "role:y"}}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}}
	more, rule := moreSpecificReason(a, b)
	assert.True(t, more)
	assert.Equal(t, "scope_size", rule)
}

func TestMoreSpecificReasonVerifiedAt(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x"}, VerifiedAt: "2026-02-01"}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}, VerifiedAt: "2026-01-01"}
	more, rule := moreSpecificReason(a, b)
	assert.True(t, more)
	assert.Equal(t, "verified_at", rule)
}

func TestMoreSpecificReasonCreatedAt(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x"}, CreatedAt: "2026-02-01"}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}, CreatedAt: "2026-01-01"}
	more, rule := moreSpecificReason(a, b)
	assert.True(t, more)
	assert.Equal(t, "created_at", rule)
}

func TestMoreSpecificReasonIndistinguishable(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x"}}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}}
	more, rule := moreSpecificReason(a, b)
	assert.False(t, more, "an unrelated ID suffix must not fabricate authority")
	assert.Equal(t, "indistinguishable", rule)
	more, rule = moreSpecificReason(b, a)
	assert.False(t, more)
	assert.Equal(t, "indistinguishable", rule)
}

func TestMoreSpecificDelegatesToReason(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x", "role:y"}}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}}
	assert.True(t, moreSpecific(a, b))
	assert.False(t, moreSpecific(b, a))
}

// TestMoreSpecificReasonOverrideWinsRegardlessOfChain covers crn-3476 FR-6
// (crn-h5zx): an explicit --force correction must win shadow resolution
// unconditionally via OverriddenDuplicateOf, checked before the existing
// scope_size/verified_at/created_at chain -- so it still wins
// even when it loses every existing chain key (smaller scope, earlier
// verified_at, earlier created_at, and a lexicographically higher id).
func TestMoreSpecificReasonOverrideWinsRegardlessOfChain(t *testing.T) {
	old := &Entry{ID: "old", Scope: []string{"rig:x", "role:y", "agent:z"}, VerifiedAt: "2026-06-01", CreatedAt: "2026-06-01T00:00:00Z"}
	newer := &Entry{ID: "zzz-newer", Scope: []string{"rig:x"}, OverriddenDuplicateOf: "old", VerifiedAt: "2026-01-01", CreatedAt: "2026-01-01T00:00:00Z"}

	more, rule := moreSpecificReason(newer, old)
	assert.True(t, more, "the entry that explicitly overrides the other must win even though it loses every existing chain key")
	assert.Equal(t, "override", rule)

	more, rule = moreSpecificReason(old, newer)
	assert.False(t, more, "the overridden entry must lose regardless of argument order")
	assert.Equal(t, "override", rule)
}

// TestMoreSpecificReasonOverrideCycleGuardFallsBackToChain covers crn-3476
// NFR-4: if both candidates in a comparison claim to override each other (a
// malformed double-force), the override signal must be discarded entirely
// and resolution must fall through to the existing chain -- never loop,
// never pick an undefined winner. Scope sizes differ here specifically so
// the chain's decision is unambiguous proof the fallback actually ran.
func TestMoreSpecificReasonOverrideCycleGuardFallsBackToChain(t *testing.T) {
	a := &Entry{ID: "a", Scope: []string{"rig:x", "role:y"}, OverriddenDuplicateOf: "b"}
	b := &Entry{ID: "b", Scope: []string{"rig:x"}, OverriddenDuplicateOf: "a"}

	more, rule := moreSpecificReason(a, b)
	assert.True(t, more, "mutual override claims must be discarded, falling back to the existing chain")
	assert.Equal(t, "scope_size", rule, "the rule reported must be the chain's, proving the override short-circuit was skipped")

	more, rule = moreSpecificReason(b, a)
	assert.False(t, more)
	assert.Equal(t, "scope_size", rule)
}

// TestVisibleShadowRespectsExplicitOverride is crn-h5zx's real-world shape
// end-to-end: a --force correction (z-correction) and the entry it corrects
// (a) share a topic_key and scope, so absent FR-6 they would be surfaced as
// indistinguishable. OverriddenDuplicateOf must select the correction
// regardless, and the shared resolver makes every read path inherit it.
func TestVisibleShadowRespectsExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rig/alpha/a.md",
		"+++\nid = \"a\"\ntitle = \"a\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\n+++\nx\n")
	writeFile(t, dir, "rig/alpha/z-correction.md",
		"+++\nid = \"z-correction\"\ntitle = \"z-correction\"\ntopic_key = \"tk\"\nscope = [\"rig:alpha\"]\noverridden_duplicate_of = \"a\"\n+++\nx\n")

	vs, err := Visible(t.Context(), dir, []string{"rig:alpha"})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range vs {
		ids[e.ID] = true
	}
	assert.True(t, ids["z-correction"], "the entry that explicitly overrides the other must be visible")
	assert.False(t, ids["a"], "the overridden entry must be shadowed even though its id would otherwise win the tiebreak")
}

func TestShadowReasonReportsWinnerAndRule(t *testing.T) {
	rigOnly := &Entry{ID: "r1", TopicKey: "t", Scope: []string{"rig:x"}}
	rigAndRole := &Entry{ID: "r2", TopicKey: "t", Scope: []string{"rig:x", "role:y"}}

	out, reasons := shadowReason([]*Entry{rigOnly, rigAndRole})

	require.Len(t, out, 1, "only the winner should survive shadowing")
	assert.Equal(t, "r2", out[0].ID)
	require.Len(t, reasons, 1)
	assert.Equal(t, "t", reasons[0].TopicKey)
	assert.Equal(t, "r2", reasons[0].WinnerID)
	assert.Equal(t, "scope_size", reasons[0].Rule)
}

func TestShadowReasonNoDecisionForSingleCandidateTopic(t *testing.T) {
	solo := &Entry{ID: "solo", TopicKey: "only-one", Scope: nil}
	out, reasons := shadowReason([]*Entry{solo})
	require.Len(t, out, 1)
	assert.Empty(t, reasons, "a topic_key held by only one candidate is not a decision")
}

func TestShadowReasonReportsOneDecisionPerContestedTopic(t *testing.T) {
	s1 := parseFixture(t, lessSpecificShared)
	s2 := parseFixture(t, moreSpecificShared)
	u1 := parseFixture(t, untopiced1) // never contested -- must produce no reason

	survivors, reasons := shadowReason([]*Entry{s1, s2, u1})

	survivorIDs := map[string]bool{}
	for _, e := range survivors {
		survivorIDs[e.ID] = true
	}
	assert.True(t, survivorIDs["s2"] && survivorIDs["u1"])
	assert.False(t, survivorIDs["s1"])

	require.Len(t, reasons, 1, "only the contested topic_key should produce a reason")
	assert.Equal(t, "shared", reasons[0].TopicKey)
	assert.Equal(t, "s2", reasons[0].WinnerID)
	assert.Equal(t, "scope_size", reasons[0].Rule)
}

func TestShadowDelegatesToShadowReason(t *testing.T) {
	rigOnly := &Entry{ID: "r1", TopicKey: "t", Scope: []string{"rig:x"}}
	rigAndRole := &Entry{ID: "r2", TopicKey: "t", Scope: []string{"rig:x", "role:y"}}
	out := shadow([]*Entry{rigOnly, rigAndRole})
	require.Len(t, out, 1)
	assert.Equal(t, "r2", out[0].ID)
}

func TestBestShadowerExplainReportsRule(t *testing.T) {
	rigOnly := &Entry{ID: "s1", TopicKey: "t", Scope: []string{"rig:x"}}
	rigAndRole := &Entry{ID: "s2", TopicKey: "t", Scope: []string{"rig:x", "role:y"}}
	group := []*Entry{rigOnly, rigAndRole}

	best, rule := bestShadowerExplain(rigOnly, group)
	require.NotNil(t, best)
	assert.Equal(t, "s2", best.ID)
	assert.Equal(t, "scope_size", rule)

	best, rule = bestShadowerExplain(rigAndRole, group)
	assert.Nil(t, best)
	assert.Empty(t, rule)
}

func TestBestShadowerDelegatesToExplain(t *testing.T) {
	rigOnly := &Entry{ID: "s1", TopicKey: "t", Scope: []string{"rig:x"}}
	rigAndRole := &Entry{ID: "s2", TopicKey: "t", Scope: []string{"rig:x", "role:y"}}
	group := []*Entry{rigOnly, rigAndRole}
	best := bestShadower(rigOnly, group)
	require.NotNil(t, best)
	assert.Equal(t, "s2", best.ID)
}

// testLogRecords runs fn with a buffer-backed obslog.Logger attached to
// ctx, then parses every JSONL line the run produced.
func testLogRecords(t *testing.T, fn func(ctx context.Context)) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := obslog.NewWithWriter(&buf, obslog.Options{Command: "test"}, &bytes.Buffer{})
	ctx := obslog.WithLogger(context.Background(), logger)
	fn(ctx)

	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		recs = append(recs, rec)
	}
	return recs
}

func TestVisibleFromLogsShadowDecisionInIdentityMode(t *testing.T) {
	rigOnly := &Entry{ID: "r1", TopicKey: "t", Scope: []string{"rig:x"}}
	rigAndRole := &Entry{ID: "r2", TopicKey: "t", Scope: []string{"rig:x", "role:y"}}

	var out []*Entry
	recs := testLogRecords(t, func(ctx context.Context) {
		out = visibleFrom(ctx, []*Entry{rigOnly, rigAndRole}, []string{"rig:x", "role:y"})
	})

	require.Len(t, out, 1)
	assert.Equal(t, "r2", out[0].ID)

	require.Len(t, recs, 1, "exactly one shadow_decision expected for one multi-candidate topic")
	rec := recs[0]
	assert.Equal(t, "shadow_decision", rec["kind"])
	assert.Equal(t, "identity", rec["mode"])
	assert.Equal(t, "t", rec["topic_key"])
	assert.Equal(t, "r2", rec["winner_id"])
	assert.Equal(t, "scope_size", rec["rule"])
}

func TestVisibleFromNoLogWhenNoShadowingOccurs(t *testing.T) {
	solo := &Entry{ID: "solo", TopicKey: "t", Scope: nil}
	recs := testLogRecords(t, func(ctx context.Context) {
		visibleFrom(ctx, []*Entry{solo}, nil)
	})
	assert.Empty(t, recs, "no shadow_decision expected when no topic had more than one visible candidate")
}

func TestShadowMapLogsShadowDecisionInStoreWideMode(t *testing.T) {
	s1 := parseFixture(t, lessSpecificShared)
	s2 := parseFixture(t, moreSpecificShared)

	var sm map[string]*Entry
	recs := testLogRecords(t, func(ctx context.Context) {
		sm = ShadowMap(ctx, []*Entry{s1, s2})
	})

	require.Contains(t, sm, "s1")
	require.Len(t, recs, 1)
	rec := recs[0]
	assert.Equal(t, "shadow_decision", rec["kind"])
	assert.Equal(t, "store_wide", rec["mode"])
	assert.Equal(t, "s1", rec["entry_id"])
	assert.Equal(t, "s2", rec["winner_id"])
}

func TestFindReturnsErrNotFoundForUnknownID(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()

	_, err := Find(ctx, store, "does-not-exist")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFindIncrementsHitCount(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	e, err := Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, 1, e.HitCount, "the returned entry must carry the authoritative post-increment count")

	e, err = Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, 2, e.HitCount)

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var hitCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT hit_count FROM entries WHERE id = 'a'").Scan(&hitCount))
	assert.Equal(t, 2, hitCount, "the index's own counter must match what Find returned")
}

func TestFindStampsLastRecalledAt(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	before := time.Now().Add(-time.Second)
	e, err := Find(ctx, store, "a")
	require.NoError(t, err)
	require.NotEmpty(t, e.LastRecalledAt, "Find must stamp last_recalled_at on a hit")

	stamped, err := time.Parse(time.RFC3339, e.LastRecalledAt)
	require.NoError(t, err, "last_recalled_at must be RFC3339")
	assert.False(t, stamped.Before(before), "stamped time must be no earlier than just before the call")

	db, err := sql.Open("sqlite", IndexPath(store))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var lastRecalledAt string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT last_recalled_at FROM entries WHERE id = 'a'").Scan(&lastRecalledAt))
	assert.Equal(t, e.LastRecalledAt, lastRecalledAt, "the index's own column must match what Find returned")
}

func TestFindUsesIndexPointLookupNotWalk(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	_, err := Find(ctx, store, "a")
	require.NoError(t, err, "this builds the index")

	// A genuinely malformed entry (unterminated frontmatter) added after
	// indexing would break a walk-based lookup: IterEntries propagates any
	// ParseEntry error that isn't errNotEntry.
	writeFile(t, store, "global/bad.md", "+++\nid = \"bad\nno closing fence\n")
	_, err = IterEntries(store)
	require.Error(t, err, "sanity check: the malformed sibling file must break a walk")

	// Find resolves "a" via the already-built index rather than re-walking --
	// a non-git store's index is trusted fresh once built (crn-6az.6.1.2) --
	// so the malformed sibling is never parsed.
	e, err := Find(ctx, store, "a")
	require.NoError(t, err)
	assert.Equal(t, "a", e.ID)
}

func TestFindAfterSequentialCreateOnNonGitStore(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()

	e1, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "dbg-topic-1", Body: "body 1", CreatedBy: "tester"})
	require.NoError(t, err)
	require.NoError(t, e1.Create(store))

	_, err = Find(ctx, store, e1.ID)
	require.NoError(t, err, "first Find must succeed")

	e2, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "dbg-topic-2", Body: "body 2", CreatedBy: "tester"})
	require.NoError(t, err)
	require.NoError(t, e2.Create(store))

	// Entry.Create is a pure filesystem write -- no git commit, no reindex --
	// so without Find's retry-on-miss, ensureFresh would treat the index
	// built by the first Find above as fresh forever on this non-git store
	// (crn-6az.6.1.2), falsely reporting this second entry not found.
	_, err = Find(ctx, store, e2.ID)
	require.NoError(t, err, "second Find must succeed even though the store's index predates this entry")
}

// TestEntryCorrectsRoundTripsThroughTOML is crn-evw98.3. Corrects is an
// explicit, author-declared cross-topic link ("this entry corrects that
// one"), distinct from OverriddenDuplicateOf's inferred same-topic_key
// match. It must round-trip through the TOML frontmatter the same way
// OverriddenDuplicateOf already does.
func TestEntryCorrectsRoundTripsThroughTOML(t *testing.T) {
	dir := t.TempDir()
	e, err := NewEntry(NewEntryParams{Type: EntryTypeKnowledge, TopicKey: "topic-a", Body: "corrected body", CreatedBy: "tester"})
	require.NoError(t, err)
	e.Corrects = "orig-id"
	require.NoError(t, e.Create(dir))

	reparsed, err := ParseEntry(e.BodyPath)
	require.NoError(t, err)
	assert.Equal(t, "orig-id", reparsed.Corrects, "Corrects must round-trip through the TOML frontmatter like OverriddenDuplicateOf does")
}

// TestFindCorrectionReturnsNilWhenUncorrected is crn-evw98.3: an entry with
// no corrector must report no correction rather than erroring.
func TestFindCorrectionReturnsNilWhenUncorrected(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\nbody\n")

	corrector, err := FindCorrection(ctx, store, "a")
	require.NoError(t, err)
	assert.Nil(t, corrector, "an entry nobody corrects must report no correction")
}

// TestFindCorrectionFollowsExplicitCorrectsLinkRegardlessOfTopicKey is
// crn-evw98.3's core contract: unlike OverriddenDuplicateOf (same
// topic_key only), Corrects must be followed across topic_key boundaries
// since it is an explicit author declaration, not an inferred match.
func TestFindCorrectionFollowsExplicitCorrectsLinkRegardlessOfTopicKey(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/orig.md", "+++\nid = \"orig\"\ntitle = \"Old fact\"\ntopic_key = \"topic-old\"\n+++\nthe old, wrong claim\n")
	writeFile(t, store, "global/fix.md", "+++\nid = \"fix\"\ntitle = \"Corrected fact\"\ntopic_key = \"topic-new\"\ncorrects = \"orig\"\n+++\nthe corrected claim\n")

	corrector, err := FindCorrection(ctx, store, "orig")
	require.NoError(t, err)
	require.NotNil(t, corrector, "orig is named by fix's Corrects field -- FindCorrection must surface it even though the two entries do not share a topic_key")
	assert.Equal(t, "fix", corrector.ID)
}

// TestFindCorrectionPicksMostRecentWhenMultipleEntriesCorrectTheSameID is
// crn-evw98.3: if more than one entry claims to correct the same id, the
// most recently created correction must win.
func TestFindCorrectionPicksMostRecentWhenMultipleEntriesCorrectTheSameID(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/orig.md", "+++\nid = \"orig\"\ntitle = \"Old fact\"\n+++\nold\n")
	writeFile(t, store, "global/fix1.md", "+++\nid = \"fix1\"\ntitle = \"First fix\"\ncorrects = \"orig\"\ncreated_at = \"2026-01-01T00:00:00Z\"\n+++\nfirst correction\n")
	writeFile(t, store, "global/fix2.md", "+++\nid = \"fix2\"\ntitle = \"Second fix\"\ncorrects = \"orig\"\ncreated_at = \"2026-06-01T00:00:00Z\"\n+++\nsecond, more current correction\n")

	corrector, err := FindCorrection(ctx, store, "orig")
	require.NoError(t, err)
	require.NotNil(t, corrector)
	assert.Equal(t, "fix2", corrector.ID, "the most recently created correction must win when more than one entry claims to correct the same id")
}

// TestFindCorrectionDoesNotFollowChains is crn-evw98.3's documented scope
// boundary: FindCorrection is single-hop only and must not chase a
// corrects-of-a-corrects chain transitively.
func TestFindCorrectionDoesNotFollowChains(t *testing.T) {
	ctx := t.Context()
	store := t.TempDir()
	writeFile(t, store, "global/a.md", "+++\nid = \"a\"\ntitle = \"A\"\n+++\na\n")
	writeFile(t, store, "global/b.md", "+++\nid = \"b\"\ntitle = \"B\"\ncorrects = \"a\"\n+++\nb\n")
	writeFile(t, store, "global/c.md", "+++\nid = \"c\"\ntitle = \"C\"\ncorrects = \"b\"\n+++\nc\n")

	corrector, err := FindCorrection(ctx, store, "a")
	require.NoError(t, err)
	require.NotNil(t, corrector)
	assert.Equal(t, "b", corrector.ID, "FindCorrection is single-hop by design (crn-evw98.3 scope) -- it must not chase corrects chains transitively")
}

func TestVisibleNeverReadsBodiesAfterIndexBuilt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	vs, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err)
	require.Len(t, vs, 1)
	assert.Equal(t, "g", vs[0].ID)

	// Added after the index already exists, on a store with no git HEAD to
	// diff against -- ensureFresh treats an already-indexed, non-git store as
	// forever fresh (see indexStale), so this file is never walked. Confirm
	// it really would break a body walk if it were, so the assertion below
	// is a real proof rather than a vacuous one.
	writeFile(t, dir, "global/broken.md", "+++\nid = \"broken\"\nno closing fence\n")
	_, walkErr := IterEntries(dir)
	require.Error(t, walkErr, "sanity check: the malformed sibling must actually break a body walk")

	vs2, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err, "Visible must never re-walk bodies to satisfy a query")
	require.Len(t, vs2, 1)
	assert.Equal(t, "g", vs2[0].ID)
}

func TestStatusNeverReadsBodiesAfterIndexBuilt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	entries, err := Status(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "g", entries[0].ID)

	// Added after the index already exists, on a store with no git HEAD to
	// diff against -- ensureFresh treats an already-indexed, non-git store as
	// forever fresh (see indexStale), so this file is never walked. Confirm
	// it really would break a body walk if it were, so the assertion below
	// is a real proof rather than a vacuous one.
	writeFile(t, dir, "global/broken.md", "+++\nid = \"broken\"\nno closing fence\n")
	_, walkErr := IterEntries(dir)
	require.Error(t, walkErr, "sanity check: the malformed sibling must actually break a body walk")

	entries2, err := Status(t.Context(), dir)
	require.NoError(t, err, "Status must never re-walk bodies to satisfy a query")
	require.Len(t, entries2, 1)
	assert.Equal(t, "g", entries2[0].ID)
}

func TestVisibleNeverTouchesHitCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	// Simulate a prior Find/Get having bumped hit_count independently of the
	// body (crn-6az.6.1.1, see reindexUpsertChunkTx's comment) -- exactly the
	// index-only state Visible must leave alone, since it only ever issues
	// SELECTs.
	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET hit_count = 7 WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT hit_count FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, 7, got, "Visible must never touch hit_count")
}

func TestVisibleNeverTouchesLastRecalledAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	// Simulate a prior Find/Get having stamped last_recalled_at independently
	// of the body -- exactly the index-only state Visible must leave alone,
	// since it only ever issues SELECTs (same scoping as hit_count).
	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET last_recalled_at = '2026-01-01T00:00:00Z' WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Visible(t.Context(), dir, nil)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT last_recalled_at FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, "2026-01-01T00:00:00Z", got, "Visible must never touch last_recalled_at")
}

func TestStatusNeverTouchesHitCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Status(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET hit_count = 7 WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Status(t.Context(), dir)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT hit_count FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, 7, got, "Status must never touch hit_count")
}

func TestStatusNeverTouchesLastRecalledAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global/g.md", globalEntry)

	_, err := Status(t.Context(), dir)
	require.NoError(t, err)

	db, err := openDB(dir)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE entries SET last_recalled_at = '2026-01-01T00:00:00Z' WHERE id = 'g'`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Status(t.Context(), dir)
	require.NoError(t, err)

	db, err = openDB(dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var got string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT last_recalled_at FROM entries WHERE id = 'g'`).Scan(&got))
	assert.Equal(t, "2026-01-01T00:00:00Z", got, "Status must never touch last_recalled_at")
}

func TestStatusPopulatesAnchorAndScopeFields(t *testing.T) {
	dir := t.TempDir()
	body := "+++\n" +
		"id = \"a\"\n" +
		"title = \"A\"\n" +
		"summary = \"a short summary\"\n" +
		"hit_count = 5\n" +
		"topic_key = \"t/a\"\n" +
		"scope = [\"rig:alpha\", \"role:investigator\"]\n" +
		"verified_at = \"2026-07-01\"\n" +
		"created_at = \"2026-01-01T00:00:00Z\"\n" +
		"\n" +
		"[anchor]\n" +
		"type = \"files\"\n" +
		"repo = \"/some/repo\"\n" +
		"paths = [\"a.go\", \"b.go\"]\n" +
		"spec = \"main\"\n" +
		"fingerprint = \"abc123\"\n" +
		"+++\n" +
		"body\n"
	writeFile(t, dir, "role/investigator/a.md", body)

	entries, err := Status(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "a", e.ID)
	assert.Equal(t, "t/a", e.TopicKey)
	assert.ElementsMatch(t, []string{"rig:alpha", "role:investigator"}, e.Scope)
	assert.Equal(t, "2026-07-01", e.VerifiedAt)
	assert.Equal(t, "2026-01-01T00:00:00Z", e.CreatedAt)
	assert.Equal(t, "files", e.Anchor.Type)
	assert.Equal(t, "/some/repo", e.Anchor.Repo)
	assert.Equal(t, []string{"a.go", "b.go"}, e.Anchor.Paths)
	assert.Equal(t, "main", e.Anchor.Spec)
	assert.Equal(t, "abc123", e.Anchor.Fingerprint)

	// crn-0vqk.2: Status's SELECT was extended to also cover these three --
	// already-indexed at zero marginal query cost (reindexUpsertChunkTx
	// populates them unconditionally) -- so Prime can render entries without
	// a body read.
	assert.Equal(t, "A", e.Title)
	assert.Equal(t, "a short summary", e.Summary)
	assert.Equal(t, 5, e.HitCount)
}

// TestIterEntriesWrapsParseFailureAsMalformedEntryError covers crn-od2x.2:
// a real parse failure (as opposed to errNotEntry, silently skipped) must be
// classifiable by cmd/format.go's malformed_store category without changing
// IterEntries' existing abort-the-walk behavior -- so the returned error
// must still satisfy errors.Is-style propagation (it's still a non-nil error
// that aborts the walk) while also exposing the offending path via
// errors.As(&MalformedEntryError{}).
func TestIterEntriesWrapsParseFailureAsMalformedEntryError(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "global/broken.md", "+++\nid = \"broken\"\nno closing fence\n")

	_, err := IterEntries(dir)
	require.Error(t, err)

	var malformed *MalformedEntryError
	require.True(t, errors.As(err, &malformed), "IterEntries' parse-failure error must be a *MalformedEntryError")
	assert.Equal(t, bad, malformed.Path, "MalformedEntryError.Path must name the offending file")
}
