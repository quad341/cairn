// Package cairn implements the knowledge store — entries (markdown bodies with
// TOML frontmatter), the rebuildable SQLite index, and source-anchored freshness.
package cairn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/quad341/cairn/internal/obslog"
)

const fence = "+++"

// constError is an immutable, comparable sentinel error usable as a const.
type constError string

func (e constError) Error() string { return string(e) }

// ErrNotFound is returned by Find when no entry has the requested id.
const ErrNotFound constError = "entry not found"

// errNotEntry marks a markdown file that carries no cairn frontmatter.
const errNotEntry constError = "not a cairn entry"

// MalformedEntryError marks a real ParseEntry failure IterEntries hit while
// walking the store -- distinct from errNotEntry (a file that simply isn't a
// cairn entry, silently skipped). It exists purely so a caller can classify a
// partially-corrupt store as a structured, stable condition (cmd/format.go's
// malformed_store category) without changing IterEntries' existing
// abort-the-walk behavior on this error: Error() renders identically to the
// unwrapped error, so this is a legibility-only addition.
type MalformedEntryError struct {
	Path string
	Err  error
}

func (e *MalformedEntryError) Error() string { return e.Err.Error() }
func (e *MalformedEntryError) Unwrap() error { return e.Err }

// Anchor records what an entry was derived from, so drift is detectable.
type Anchor struct {
	Type        string   `toml:"type"` // none | files | commit | query | external
	Repo        string   `toml:"repo,omitempty"`
	Paths       []string `toml:"paths,omitempty"`
	Spec        string   `toml:"spec,omitempty"`
	Fingerprint string   `toml:"fingerprint,omitempty"`
}

// Entry is one unit of knowledge.
type Entry struct {
	ID            string   `toml:"id"`
	Title         string   `toml:"title"`
	TitleSource   string   `toml:"title_source,omitempty"`
	Summary       string   `toml:"summary,omitempty"`
	SummarySource string   `toml:"summary_source,omitempty"`
	Type          string   `toml:"type,omitempty"` // legacy empty | knowledge | remediation | migration-only policy
	TopicKey      string   `toml:"topic_key,omitempty"`
	Scope         []string `toml:"scope,omitempty"` // tags, e.g. ["rig:web"]
	Anchor        Anchor   `toml:"anchor"`
	VerifiedAt    string   `toml:"verified_at,omitempty"`
	CreatedBy     string   `toml:"created_by,omitempty"`
	CreatedAt     string   `toml:"created_at,omitempty"`
	HitCount      int      `toml:"hit_count,omitzero"`

	Kind            string `toml:"kind,omitempty"`             // legacy; new writes classify with Type
	AutoActionable  bool   `toml:"auto_actionable,omitempty"`  // only for Type/legacy Kind == "remediation"
	RecurrenceCount int    `toml:"recurrence_count,omitzero"`  // incremented on a recurrence match, topic_key + content (crn-qxj3)
	PromotedBeadID  string `toml:"promoted_bead_id,omitempty"` // empty until promoted; promotion idempotency guard
	LastRecalledAt  string `toml:"last_recalled_at,omitempty"` // RFC3339; written only by the get/freshness/verify call site (crn-28ge.1.5)
	// OverriddenDuplicateOf is set to the matched entry's ID when --force
	// creates a new entry past a detected duplicate (crn-lzn4.1.1).
	OverriddenDuplicateOf string `toml:"overridden_duplicate_of,omitempty"`

	BodyPath string `toml:"-"`
	Body     string `toml:"-"`
}

var scopeDirs = []string{"global", "rig", "role", "agent"}

// splitFrontmatter splits raw file text into its +++-fenced frontmatter and
// body -- the fence-finding ParseEntry and WriteBack both need. ok is false
// (with a nil error) when text carries no +++ frontmatter at all, distinct
// from a real parse error (an opened-but-never-closed fence).
func splitFrontmatter(text string) (front, body string, ok bool, err error) {
	if !strings.HasPrefix(text, fence) {
		return "", "", false, nil
	}
	rest := text[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return "", "", false, fmt.Errorf("unterminated %s frontmatter", fence)
	}
	front = rest[:end]
	body = strings.TrimLeft(rest[end+len("\n"+fence):], "\n")
	return front, body, true, nil
}

// ParseEntry reads a markdown file with TOML frontmatter (+++ fences). It
// returns errNotEntry for files that carry no frontmatter or no id.
func ParseEntry(path string) (*Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	front, body, ok, err := splitFrontmatter(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !ok {
		return nil, errNotEntry
	}

	var e Entry
	if _, err := toml.Decode(front, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if e.ID == "" {
		return nil, errNotEntry
	}
	e.BodyPath = path
	e.Body = body
	return &e, nil
}

// WriteBack surgically patches verified_at and anchor.fingerprint into the
// on-disk frontmatter, leaving every other line byte-for-byte untouched --
// unlike marshal's full re-encode (used by Create, where there is no prior
// on-disk text to preserve), WriteBack's only production caller
// (cmd/commands.go verifyCmd) always patches an existing file, and a
// `cairn verify` diff should show only what actually changed.
func (e *Entry) WriteBack() error {
	return e.writeBackPatched(func(front string) (string, error) {
		return patchVerification(front, e.VerifiedAt, e.Anchor.Fingerprint)
	})
}

// WriteBackAnchor surgically patches the [anchor] table's type/repo/paths
// into the on-disk frontmatter, under the same "patch, don't re-encode"
// contract as WriteBack. It is what makes an ALREADY-WRITTEN entry
// anchorable (crn-01fj): remember's --anchor-repo/--anchor-path only build
// an anchor at creation time, and verify only recomputes a fingerprint for
// an entry that already carries repo+paths, so before this there was no
// path at all from "entry exists, unanchored" to "entry is anchored" short
// of hand-editing the file. Most of a migrated store is in exactly that
// state, since flat files had no anchor concept to carry over.
//
// Unlike its siblings, this may have to CREATE the [anchor] table: entries
// hand-authored or written by early versions can lack it entirely, and
// those are the ones most in need of an anchor.
func (e *Entry) WriteBackAnchor() error {
	return e.writeBackPatched(func(front string) (string, error) {
		return patchAnchor(front, e.Anchor)
	})
}

// WriteBackRetrievalMetadata surgically replaces title/summary and records
// both as authored. The body and unrelated frontmatter remain byte-identical.
func (e *Entry) WriteBackRetrievalMetadata() error {
	return e.writeBackPatched(func(front string) (string, error) {
		lines := strings.Split(front, "\n")
		lines = patchTopLevelScalar(lines, "title", strconv.Quote(e.Title))
		lines = patchTopLevelScalar(lines, "title_source", strconv.Quote(MetadataSourceAuthored))
		lines = patchTopLevelScalar(lines, "summary", strconv.Quote(e.Summary))
		lines = patchTopLevelScalar(lines, "summary_source", strconv.Quote(MetadataSourceAuthored))
		return strings.Join(lines, "\n"), nil
	})
}

// WriteBackBackfill atomically patches a legacy entry's classification and,
// when requested, its retrieval metadata. It exists for the migration path:
// new-entry validation intentionally rejects policy, while a migration may
// classify an existing policy entry so a librarian can query and remove it.
func (e *Entry) WriteBackBackfill(updateRetrievalMetadata bool) error {
	switch e.Type {
	case EntryTypeKnowledge, EntryTypeRemediation, EntryTypePolicy:
	default:
		return fmt.Errorf("invalid backfill type %q", e.Type)
	}
	if e.Kind == EntryTypeRemediation && e.Type != EntryTypeRemediation {
		return fmt.Errorf("type %q contradicts legacy kind %q", e.Type, e.Kind)
	}
	return e.writeBackPatched(func(front string) (string, error) {
		lines := strings.Split(front, "\n")
		lines = patchTopLevelScalar(lines, "type", strconv.Quote(e.Type))
		if updateRetrievalMetadata {
			lines = patchTopLevelScalar(lines, "title", strconv.Quote(e.Title))
			lines = patchTopLevelScalar(lines, "title_source", strconv.Quote(MetadataSourceAuthored))
			lines = patchTopLevelScalar(lines, "summary", strconv.Quote(e.Summary))
			lines = patchTopLevelScalar(lines, "summary_source", strconv.Quote(MetadataSourceAuthored))
		}
		return strings.Join(lines, "\n"), nil
	})
}

func patchTopLevelScalar(lines []string, key, value string) []string {
	line := key + " = " + value
	for i, existing := range lines {
		if strings.HasPrefix(strings.TrimSpace(existing), "[") {
			break
		}
		if k, _, ok := strings.Cut(existing, "="); ok && strings.TrimSpace(k) == key {
			lines[i] = line
			return lines
		}
	}
	for i, existing := range lines {
		if k, _, ok := strings.Cut(existing, "="); ok && strings.TrimSpace(k) == "id" {
			return append(lines[:i+1], append([]string{line}, lines[i+1:]...)...)
		}
	}
	return append([]string{line}, lines...)
}

// WriteBackRecurrenceCount surgically patches recurrence_count into the
// on-disk frontmatter -- the same "patch, don't re-encode" contract
// WriteBack uses for verified_at/fingerprint above. cmd/remember.go's
// capture-time recurrence path (crn-28ge.1.4) increments e.RecurrenceCount
// in memory on a genuine recurrence match (topic_key AND content, crn-qxj3)
// and calls this to persist it, without disturbing any other field a
// curator or a prior WriteBack already wrote. As with WriteBack,
// incrementing the field is the caller's responsibility; this only persists
// whatever value is already there.
func (e *Entry) WriteBackRecurrenceCount() error {
	return e.writeBackPatched(func(front string) (string, error) {
		return patchRecurrenceCount(front, e.RecurrenceCount)
	})
}

// WriteBackPromotedBeadID surgically patches promoted_bead_id into the
// on-disk frontmatter -- the same "patch, don't re-encode" contract
// WriteBack and WriteBackRecurrenceCount use. cmd/promote.go's promote-mark
// command sets e.PromotedBeadID in memory and calls this to persist it,
// without disturbing any other field a curator or a prior WriteBack already
// wrote.
func (e *Entry) WriteBackPromotedBeadID() error {
	return e.writeBackPatched(func(front string) (string, error) {
		return patchPromotedBeadID(front, e.PromotedBeadID)
	})
}

// writeBackPatched is the read/split/patch/reassemble/write shell shared by
// WriteBack, WriteBackRecurrenceCount, and WriteBackPromotedBeadID: read the
// on-disk file, split it into frontmatter and body, hand the frontmatter to
// patch, and write the merged result back. Every line patch itself doesn't
// touch survives byte-for-byte, matching WriteBack's own "surgical patch,
// not a full re-encode" contract.
func (e *Entry) writeBackPatched(patch func(front string) (string, error)) error {
	raw, err := os.ReadFile(e.BodyPath)
	if err != nil {
		return err
	}
	front, body, ok, err := splitFrontmatter(string(raw))
	if err != nil {
		return fmt.Errorf("%s: %w", e.BodyPath, err)
	}
	if !ok {
		return fmt.Errorf("%s: %w", e.BodyPath, errNotEntry)
	}

	patched, err := patch(front)
	if err != nil {
		return fmt.Errorf("%s (id %s): %w", e.BodyPath, e.ID, err)
	}

	var sb strings.Builder
	sb.WriteString(fence)
	sb.WriteString(patched)
	sb.WriteString("\n" + fence + "\n\n")
	sb.WriteString(body)
	return os.WriteFile(e.BodyPath, []byte(sb.String()), 0o600)
}

// patchVerification patches verified_at (top-level) and anchor.fingerprint
// (inside the [anchor] table) into front, a splitFrontmatter frontmatter
// blob, in place -- every other line, including field order, indentation,
// and empty collections like `scope = []`, passes through unchanged. front
// must contain an [anchor] table; every entry that reaches WriteBack has one
// (Anchor.Type is always set, even to "none"), so a missing table means
// corruption or an unsupported hand-edit, reported as an error rather than
// guessed at.
// patchAnchor rewrites the [anchor] table's type/repo/paths in front,
// appending the table when it is absent. Keys the caller left empty are
// removed rather than written blank, so re-anchoring an entry that once had
// a `spec` or a stale `fingerprint` does not leave contradictory leftovers
// describing an anchor that no longer exists.
func patchAnchor(front string, a Anchor) (string, error) {
	lines := strings.Split(front, "\n")

	anchorAt := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[anchor]" {
			anchorAt = i
			break
		}
	}
	if anchorAt < 0 {
		// No table yet. Append one after a blank separator, matching the
		// shape marshal emits so a patched file and a freshly created one
		// are indistinguishable.
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "[anchor]")
		anchorAt = len(lines) - 1
	}
	anchorEnd := len(lines)
	for i := anchorAt + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			anchorEnd = i
			break
		}
	}

	top := lines[:anchorAt:anchorAt]
	anchor := lines[anchorAt:anchorEnd:anchorEnd]
	rest := lines[anchorEnd:]

	anchor = setTOMLLine(anchor, "type", tomlQuote(a.Type))
	anchor = setOrDropTOMLLine(anchor, "repo", a.Repo != "", tomlQuote(a.Repo))
	anchor = setOrDropTOMLLine(anchor, "paths", len(a.Paths) > 0, tomlStringArray(a.Paths))
	anchor = setOrDropTOMLLine(anchor, "spec", a.Spec != "", tomlQuote(a.Spec))
	anchor = setOrDropTOMLLine(anchor, "fingerprint", a.Fingerprint != "", tomlQuote(a.Fingerprint))

	out := append(append(append([]string{}, top...), anchor...), rest...)
	return strings.Join(out, "\n"), nil
}

// setOrDropTOMLLine is setTOMLLine plus removal: when want is false the key
// is deleted if present rather than written with an empty value, so an
// anchor never carries a key that contradicts its own type.
func setOrDropTOMLLine(region []string, key string, want bool, quotedValue string) []string {
	if want {
		return setTOMLLine(region, key, quotedValue)
	}
	out := region[:0:0]
	for _, l := range region {
		if m := tomlKeyLine.FindStringSubmatch(l); m != nil && m[2] == key {
			continue
		}
		out = append(out, l)
	}
	return out
}

// tomlStringArray renders paths as a TOML inline array of basic strings.
func tomlStringArray(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = tomlQuote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func patchVerification(front, verifiedAt, fingerprint string) (string, error) {
	lines := strings.Split(front, "\n")

	anchorAt := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[anchor]" {
			anchorAt = i
			break
		}
	}
	if anchorAt < 0 {
		return "", errors.New("no [anchor] table in frontmatter")
	}
	anchorEnd := len(lines)
	for i := anchorAt + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			anchorEnd = i
			break
		}
	}

	// Three-index slices cap capacity at each region's own length, so
	// setTOMLLine's append (when a key is absent) always allocates a fresh
	// backing array instead of writing through into the next region.
	top := lines[:anchorAt:anchorAt]
	anchor := lines[anchorAt:anchorEnd:anchorEnd]
	rest := lines[anchorEnd:]

	top = setTOMLLine(top, "verified_at", tomlQuote(verifiedAt))
	anchor = setTOMLLine(anchor, "fingerprint", tomlQuote(fingerprint))

	out := make([]string, 0, len(top)+len(anchor)+len(rest))
	out = append(out, top...)
	out = append(out, anchor...)
	out = append(out, rest...)
	return strings.Join(out, "\n"), nil
}

// patchRecurrenceCount patches recurrence_count -- a top-level field
// alongside verified_at, not one of anchor's -- into front, in place, using
// the same [anchor]-boundary-finding approach as patchVerification: every
// line at or after [anchor] passes through completely unchanged, since
// recurrence_count never lives there.
func patchRecurrenceCount(front string, count int) (string, error) {
	lines := strings.Split(front, "\n")

	anchorAt := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[anchor]" {
			anchorAt = i
			break
		}
	}
	if anchorAt < 0 {
		return "", errors.New("no [anchor] table in frontmatter")
	}

	// Three-index slice caps capacity at the region's own length, so
	// setTOMLLine's append (when the key is absent) always allocates a fresh
	// backing array instead of writing through into rest.
	top := lines[:anchorAt:anchorAt]
	rest := lines[anchorAt:]

	top = setTOMLLine(top, "recurrence_count", strconv.Itoa(count))

	out := make([]string, 0, len(top)+len(rest))
	out = append(out, top...)
	out = append(out, rest...)
	return strings.Join(out, "\n"), nil
}

// patchPromotedBeadID patches promoted_bead_id -- a top-level field
// alongside verified_at and recurrence_count, not one of anchor's -- into
// front, in place, using the same [anchor]-boundary-finding approach as
// patchRecurrenceCount: every line at or after [anchor] passes through
// completely unchanged, since promoted_bead_id never lives there.
func patchPromotedBeadID(front, beadID string) (string, error) {
	lines := strings.Split(front, "\n")

	anchorAt := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[anchor]" {
			anchorAt = i
			break
		}
	}
	if anchorAt < 0 {
		return "", errors.New("no [anchor] table in frontmatter")
	}

	// Three-index slice caps capacity at the region's own length, so
	// setTOMLLine's append (when the key is absent) always allocates a fresh
	// backing array instead of writing through into rest.
	top := lines[:anchorAt:anchorAt]
	rest := lines[anchorAt:]

	top = setTOMLLine(top, "promoted_bead_id", tomlQuote(beadID))

	out := make([]string, 0, len(top)+len(rest))
	out = append(out, top...)
	out = append(out, rest...)
	return strings.Join(out, "\n"), nil
}

// tomlKeyLine matches a "key = value" line, capturing its leading
// whitespace and bare key name.
var tomlKeyLine = regexp.MustCompile(`^(\s*)([A-Za-z0-9_-]+)\s*=`)

// setTOMLLine replaces the value on region's existing "key = value" line,
// preserving that line's own indentation, or -- if key isn't present --
// appends a new line at the end of region using the indentation of an
// existing sibling key = value line there (or none, if region has no such
// line to copy from).
func setTOMLLine(region []string, key, quotedValue string) []string {
	for i, l := range region {
		if m := tomlKeyLine.FindStringSubmatch(l); m != nil && m[2] == key {
			region[i] = m[1] + key + " = " + quotedValue
			return region
		}
	}
	indent := ""
	for _, l := range region {
		if m := tomlKeyLine.FindStringSubmatch(l); m != nil {
			indent = m[1]
			break
		}
	}
	return append(region, indent+key+" = "+quotedValue)
}

// tomlQuote renders s as a TOML basic string. WriteBack's two patched values
// (a verified_at date and a hex fingerprint) never need it in practice, but
// the patch is line-based text surgery, not a TOML encode, so it must not
// assume that and hand-escape only what those two callers happen to produce
// today.
func tomlQuote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r < 0x20:
			fmt.Fprintf(&sb, `\u%04X`, r)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// marshal renders the +++-fenced TOML frontmatter followed by the body --
// the on-disk format shared by WriteBack and Create.
func (e *Entry) marshal() ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(fence + "\n")
	if err := toml.NewEncoder(&sb).Encode(e); err != nil {
		return nil, err
	}
	sb.WriteString(fence + "\n\n")
	sb.WriteString(strings.TrimLeft(e.Body, "\n"))
	return []byte(sb.String()), nil
}

// walkEntries walks the scope dirs, parsing every .md file into an Entry.
// onParseErr is invoked for each file that fails to parse for a reason other
// than errNotEntry (which always just means "skip, not an entry"); returning
// the error aborts the walk (IterEntries' contract), while recording it and
// returning nil keeps going (IterEntriesTolerant's contract) -- the
// traversal itself is identical between the two, only the parse-failure
// policy differs.
func walkEntries(store string, onParseErr func(path string, err error) error) ([]*Entry, error) {
	// Absolute-ise the store root before joining, so the BodyPath on every
	// entry -- and therefore every body_path the indexer records -- describes
	// the file independently of the cwd this walk ran from. Default store
	// resolution hands us "." whenever cairn runs from inside the store, and
	// a relative BodyPath is only openable from that one directory (crn-o6mn).
	store, err := filepath.Abs(store)
	if err != nil {
		return nil, err
	}
	var out []*Entry
	for _, sd := range scopeDirs {
		base := filepath.Join(store, sd)
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			e, perr := ParseEntry(p)
			if perr != nil {
				if errors.Is(perr, errNotEntry) {
					return nil // not an entry — skip it
				}
				return onParseErr(p, perr)
			}
			out = append(out, e)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// IterEntries walks the scope dirs and returns all entries, sorted by id.
func IterEntries(store string) ([]*Entry, error) {
	return walkEntries(store, func(p string, err error) error { return &MalformedEntryError{Path: p, Err: err} })
}

// ParseFailure records one file that failed to parse during a tolerant walk.
type ParseFailure struct {
	Path string
	Err  error
}

// IterEntriesTolerant walks the scope dirs the same way IterEntries does,
// but a malformed file never aborts the whole scan (FR-3/NFR-1): its path
// and error are appended to the returned failures instead. The error return
// is reserved for a genuine I/O failure on the store root itself (e.g. an
// unreadable directory), never a parse error.
func IterEntriesTolerant(store string) ([]*Entry, []ParseFailure, error) {
	var failures []ParseFailure
	out, err := walkEntries(store, func(p string, perr error) error {
		failures = append(failures, ParseFailure{Path: p, Err: perr})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, failures, nil
}

// Find returns the entry with the given id, or ErrNotFound. It resolves via
// the index (one point query) rather than IterEntries' walk-plus-scan, so a
// lookup costs one SQL query and one file read regardless of store size
// (crn-6az.6.1.3). On a hit it increments the index's hit_count and stamps
// last_recalled_at (FR-4, FR-08); a miss has no side effect.
func Find(ctx context.Context, store, id string) (*Entry, error) {
	if err := ensureFresh(ctx, store); err != nil {
		return nil, err
	}
	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	bodyPath, err := findBodyPath(ctx, db, id)
	if errors.Is(err, ErrNotFound) {
		// ensureFresh's git-HEAD staleness check can't see a body written
		// since the last commit -- on a non-git store, or one whose HEAD
		// hasn't moved, it treats an already-built index as fresh forever
		// (crn-6az.6.1.2). Rather than weaken that self-heal's "don't
		// reindex needlessly" contract for every caller, force one reindex
		// here before concluding id genuinely doesn't exist, so an entry
		// created since the last reindex is never reported missing.
		if _, rerr := Reindex(ctx, store); rerr != nil {
			return nil, rerr
		}
		bodyPath, err = findBodyPath(ctx, db, id)
	}
	if err != nil {
		return nil, err
	}

	e, err := ParseEntry(resolveBodyPath(store, bodyPath))
	if err != nil {
		return nil, err
	}

	// hit_count and last_recalled_at are index-only state (crn-6az.6.1.1,
	// crn-28ge.1.1): the freshly-parsed body's values are stale-by-construction,
	// so both are always overwritten with the authoritative post-write values
	// (same transaction, same RETURNING) rather than trusted from the file.
	// This is Find's only write, and the only one in the package outside
	// Reindex's transaction. Like that transaction (crn-wrg0/#94), a single
	// busy_timeout(5000) window is not enough under fleet contention: it
	// contends with every concurrent Reindex's write lock, and Reindex's own
	// lock-hold time runs past 5s on a real-sized store. Without a retry above
	// the timeout the losing caller hard-fails `cairn get` with "database is
	// locked" (crn-ui5i5).
	err = retryOnBusy(ctx, func() error {
		now := time.Now().Format(time.RFC3339)
		return db.QueryRowContext(
			ctx,
			`UPDATE entries SET hit_count = hit_count + 1, last_recalled_at = ? WHERE id = ? RETURNING hit_count, last_recalled_at`,
			now, id,
		).Scan(&e.HitCount, &e.LastRecalledAt)
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// findBodyPath is Find's point lookup, factored out so Find can retry it
// once after a forced reindex without duplicating the query.
func findBodyPath(ctx context.Context, db *sql.DB, id string) (string, error) {
	var bodyPath string
	err := db.QueryRowContext(ctx, `SELECT body_path FROM entries WHERE id = ?`, id).Scan(&bodyPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return bodyPath, err
}

// resolveBodyPath interprets an index-recorded body_path against the store it
// came from. walkEntries now records absolute paths, but indexes already on
// disk were built before that and carry paths relative to whatever cwd did
// the indexing -- normally the store root itself, since default resolution
// passes ".". Those indexes cannot self-heal: a non-git store is never judged
// stale once built, so it would stay unreadable from every other cwd forever
// (crn-o6mn). Resolving on read makes them work without a forced reindex, and
// is a no-op for the absolute paths written from here on.
func resolveBodyPath(store, bodyPath string) string {
	if bodyPath == "" || filepath.IsAbs(bodyPath) {
		return bodyPath
	}
	return filepath.Join(store, bodyPath)
}

// Visible returns entries an identity may see: every scope-tag on the entry
// must be satisfied by the identity (a subset match). Global (untagged)
// entries are visible to all. When multiple visible entries share a
// non-empty topic_key, only the most specific one is returned — CSS-style
// shadowing (DESIGN.md §3). Built on Status's index-backed bulk read (never
// a body file, never touches hit_count), filtered through visibleFrom.
func Visible(ctx context.Context, store string, identity []string) ([]*Entry, error) {
	all, err := Status(ctx, store)
	if err != nil {
		return nil, err
	}
	return visibleFrom(ctx, all, identity), nil
}

// visibleFrom applies Visible's subset-match + shadowing rule to an
// already-loaded entry list. Factored out of Visible so callers that also
// need the full unfiltered list (e.g. Prime's scope-mismatch diagnostic,
// crn-ln1) can load the store once via Status and derive both from a single
// pass, instead of Visible re-querying the index a second time.
func visibleFrom(ctx context.Context, entries []*Entry, identity []string) []*Entry {
	resolution := resolveVisibleFrom(ctx, entries, identity)
	out := make([]*Entry, len(resolution.Entries))
	for i, resolved := range resolution.Entries {
		out[i] = resolved.Entry
	}
	return out
}

// resolveVisibleFrom is visibleFrom's conflict-preserving counterpart for
// read paths whose output must expose contested entries directly.
func resolveVisibleFrom(ctx context.Context, entries []*Entry, identity []string) TopicResolution {
	resolution := ResolveTopics(scopeMatch(entries, identity))
	logger := obslog.FromContext(ctx)
	for _, r := range resolution.decisions {
		logger.ShadowDecision(obslog.ShadowDecisionFields{
			Mode:     "identity",
			TopicKey: r.TopicKey,
			WinnerID: r.WinnerID,
			Rule:     r.Rule,
		})
	}
	return resolution
}

// scopeMatch returns entries whose every scope tag is satisfied by identity --
// visibleFrom's subset-match rule without its shadowReason collapse. Factored
// out as its own step so visibleFrom's two rules (subset-match, then
// same-topic_key shadow collapse) are each independently readable and
// testable.
func scopeMatch(entries []*Entry, identity []string) []*Entry {
	idset := make(map[string]struct{}, len(identity))
	for _, t := range identity {
		idset[t] = struct{}{}
	}

	var out []*Entry
	for _, e := range entries {
		visible := true
		for _, tag := range e.Scope {
			if _, has := idset[tag]; !has {
				visible = false
				break
			}
		}
		if visible {
			out = append(out, e)
		}
	}
	return out
}

// scopeTags loads every entry's scope tags from the index, keyed by entry id.
func scopeTags(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT entry_id, tag FROM entry_tags`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := make(map[string][]string)
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		tags[id] = append(tags[id], tag)
	}
	return tags, rows.Err()
}

// UntopicedLabel is the display/query sentinel for an empty TopicKey, shared
// by map, prime, get, and list so all four can never drift out of sync on
// what string represents "no topic".
const UntopicedLabel = "(untopiced)"

// TopicConflict describes a topic whose top-ranked entries cannot be
// distinguished by any meaningful precedence signal. EntryIDs is sorted so
// callers get stable output without treating that order as authority.
type TopicConflict struct {
	TopicKey string   `json:"topic_key"`
	EntryIDs []string `json:"entry_ids"`
	Reason   string   `json:"reason"`
}

// ResolvedEntry is one entry retained by ResolveTopics. Conflict is non-nil
// exactly when this entry is one of multiple indistinguishable revisions, so
// a caller can surface contested knowledge directly on a search/list hit.
type ResolvedEntry struct {
	Entry    *Entry         `json:"entry"`
	Conflict *TopicConflict `json:"conflict,omitempty"`
}

// TopicResolution is ResolveTopics' complete result. Entries contains the
// meaningful winner for each topic, or every tied top-ranked entry when no
// meaningful winner exists. Conflicts is the same conflict information as
// the per-entry annotation, collected once for callers that need an index.
type TopicResolution struct {
	Entries   []ResolvedEntry `json:"entries"`
	Conflicts []TopicConflict `json:"conflicts"`

	decisions []ShadowReason
}

// ResolveTopics applies topic-key precedence to identity-filtered candidates.
//
// Precondition: callers MUST filter candidates to a single identity before
// calling ResolveTopics. Scope-tag count is a specificity proxy only inside
// one identity's visible set; using a store-wide set can incorrectly compare
// entries with incomparable scopes.
//
// Precedence is explicit override, scope specificity, verified_at, then
// created_at. When every meaningful signal ties, ResolveTopics retains all
// tied entries and annotates each with a TopicConflict instead of fabricating
// authority from an unrelated ID suffix. Untopiced entries never conflict.
func ResolveTopics(candidates []*Entry) TopicResolution {
	return resolveTopics(candidates)
}

// ShadowReason explains a resolved single-winner topic. WinnerID is the entry
// that survived and Rule is the meaningful precedence signal that separated
// it from the other candidates.
type ShadowReason struct {
	TopicKey string
	WinnerID string
	Rule     string
}

// shadow is the legacy entry-only projection used by store-wide diagnostics.
func shadow(candidates []*Entry) []*Entry {
	out, _ := shadowReason(candidates)
	return out
}

// shadowReason preserves the old internal shape while delegating all
// read-path precedence to ResolveTopics.
func shadowReason(candidates []*Entry) ([]*Entry, []ShadowReason) {
	resolution := resolveTopics(candidates)
	out := make([]*Entry, len(resolution.Entries))
	for i, resolved := range resolution.Entries {
		out[i] = resolved.Entry
	}
	return out, resolution.decisions
}

func resolveTopics(candidates []*Entry) TopicResolution {
	groups := make(map[string][]*Entry, len(candidates))
	for _, e := range candidates {
		groups[e.TopicKey] = append(groups[e.TopicKey], e)
	}

	selected := make(map[string]*TopicConflict, len(candidates))
	var result TopicResolution
	for topicKey, group := range groups {
		if topicKey == "" || len(group) == 1 {
			for _, e := range group {
				selected[e.ID] = nil
			}
			continue
		}

		remaining, rule := removeOverridden(group)
		remaining, rule = retainMaxScope(remaining, rule)
		remaining, rule = retainMaxString(remaining, rule, "verified_at", func(e *Entry) string { return e.VerifiedAt })
		remaining, rule = retainMaxString(remaining, rule, "created_at", func(e *Entry) string { return e.CreatedAt })

		if len(remaining) == 1 {
			selected[remaining[0].ID] = nil
			result.decisions = append(result.decisions, ShadowReason{
				TopicKey: topicKey,
				WinnerID: remaining[0].ID,
				Rule:     rule,
			})
			continue
		}

		ids := make([]string, len(remaining))
		for i, e := range remaining {
			ids[i] = e.ID
		}
		sort.Strings(ids)
		conflict := &TopicConflict{TopicKey: topicKey, EntryIDs: ids, Reason: "indistinguishable"}
		result.Conflicts = append(result.Conflicts, *conflict)
		for _, e := range remaining {
			selected[e.ID] = conflict
		}
	}

	for _, e := range candidates {
		conflict, ok := selected[e.ID]
		if ok {
			result.Entries = append(result.Entries, ResolvedEntry{Entry: e, Conflict: conflict})
		}
	}
	sort.Slice(result.Conflicts, func(i, j int) bool { return result.Conflicts[i].TopicKey < result.Conflicts[j].TopicKey })
	sort.Slice(result.decisions, func(i, j int) bool { return result.decisions[i].TopicKey < result.decisions[j].TopicKey })
	return result
}

func removeOverridden(group []*Entry) ([]*Entry, string) {
	remaining := make([]*Entry, 0, len(group))
	for _, candidate := range group {
		overridden := false
		for _, other := range group {
			if other.OverriddenDuplicateOf == candidate.ID && candidate.OverriddenDuplicateOf != other.ID {
				overridden = true
				break
			}
		}
		if !overridden {
			remaining = append(remaining, candidate)
		}
	}
	if len(remaining) == 0 {
		return group, ""
	}
	if len(remaining) != len(group) {
		return remaining, "override"
	}
	return remaining, ""
}

func retainMaxScope(entries []*Entry, rule string) ([]*Entry, string) {
	maxScope := -1
	for _, e := range entries {
		maxScope = max(maxScope, len(e.Scope))
	}
	remaining := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if len(e.Scope) == maxScope {
			remaining = append(remaining, e)
		}
	}
	if len(remaining) != len(entries) {
		rule = "scope_size"
	}
	return remaining, rule
}

func retainMaxString(entries []*Entry, rule, field string, value func(*Entry) string) ([]*Entry, string) {
	maxValue := ""
	for _, e := range entries {
		if value(e) > maxValue {
			maxValue = value(e)
		}
	}
	remaining := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if value(e) == maxValue {
			remaining = append(remaining, e)
		}
	}
	if len(remaining) != len(entries) {
		rule = field
	}
	return remaining, rule
}

// moreSpecific reports whether a should win over b for a shared topic_key.
func moreSpecific(a, b *Entry) bool {
	more, _ := moreSpecificReason(a, b)
	return more
}

// moreSpecificReason is moreSpecific's reason-carrying counterpart: rule
// names which meaningful signal decided the comparison ("override",
// "scope_size", "verified_at", or "created_at"). Fully tied entries are
// indistinguishable; neither is more specific.
//
// An explicit --force correction (OverriddenDuplicateOf) is checked first
// and wins unconditionally, before the scope/verified_at/created_at
// chain below ever runs -- otherwise a corrected entry can still lose to
// the entry it corrects (crn-h5zx). If both sides claim to override the
// other (a malformed double-force), that is a cycle, not a decision: the
// override signal is discarded and resolution falls through to the
// existing chain instead of looping or picking an undefined winner.
func moreSpecificReason(a, b *Entry) (more bool, rule string) {
	// ShadowMap compares a pair already known to share a topic. Tests of this
	// helper historically omit TopicKey, so use shallow copies with a common
	// synthetic key and delegate the actual precedence to the shared resolver.
	aa, bb := *a, *b
	aa.TopicKey, bb.TopicKey = "__comparison__", "__comparison__"
	resolution := ResolveTopics([]*Entry{&aa, &bb})
	if len(resolution.Entries) != 1 {
		return false, "indistinguishable"
	}
	if len(resolution.decisions) == 1 {
		rule = resolution.decisions[0].Rule
	}
	return resolution.Entries[0].Entry.ID == a.ID, rule
}

// ShadowMap reports, store-wide with no identity in scope, which entries are
// shadowed and by what. Visible()'s shadow() cannot answer this: its
// tag-count specificity proxy is only sound over a single identity's
// pre-filtered candidate list, and applying it to the whole store produces
// false positives for entries whose scopes are incomparable (see
// TestShadowMapIncomparableScopesNeverShadow).
//
// X is shadowed by Y iff they share a non-empty TopicKey, Y's Scope is a
// (non-strict) superset of X's Scope, and moreSpecific(Y, X) is true. The
// superset condition is what makes the claim identity-free: every identity
// that can see Y can also see X (X.Scope ⊆ Y.Scope), and moreSpecific(Y, X)
// then holds for all of them — so "X shadowed by Y" means X loses to Y
// whenever Y is in view, not that X is unreachable outright. Entries with
// incomparable scopes never shadow each other, even on an equal-tag-count
// tie, because no such "Y always wins where both are visible" claim holds
// for them.
//
// When more than one entry qualifies as a shadower of X, the single most
// specific qualifying shadower is reported (same moreSpecific reduction
// shadow() uses to pick winners) — a deliberate v1 scope limit, not an
// exhaustive list. The returned map is keyed by the shadowed entry's ID.
func ShadowMap(ctx context.Context, entries []*Entry) map[string]*Entry {
	byTopic := make(map[string][]*Entry)
	for _, e := range entries {
		if e.TopicKey == "" {
			continue
		}
		byTopic[e.TopicKey] = append(byTopic[e.TopicKey], e)
	}

	logger := obslog.FromContext(ctx)
	out := make(map[string]*Entry)
	for _, group := range byTopic {
		if len(group) < 2 {
			continue // a topic_key held by only one entry can't be shadowed
		}
		for _, x := range group {
			best, rule := bestShadowerExplain(x, group)
			if best == nil {
				continue
			}
			out[x.ID] = best
			logger.ShadowDecision(obslog.ShadowDecisionFields{
				Mode:     "store_wide",
				TopicKey: x.TopicKey,
				EntryID:  x.ID,
				WinnerID: best.ID,
				Rule:     rule,
			})
		}
	}
	return out
}

// bestShadower returns the single most-specific entry in group that shadows
// x (see ShadowMap's doc comment for the shadowing rule), or nil if none
// qualifies.
func bestShadower(x *Entry, group []*Entry) *Entry {
	best, _ := bestShadowerExplain(x, group)
	return best
}

// bestShadowerExplain is bestShadower's reason-carrying counterpart: rule
// names which moreSpecific tiebreak makes best win over x (empty when best
// is nil, i.e. nothing in group shadows x).
func bestShadowerExplain(x *Entry, group []*Entry) (best *Entry, rule string) {
	for _, y := range group {
		if y == x || !scopeSuperset(y.Scope, x.Scope) {
			continue
		}
		if !moreSpecific(y, x) {
			continue
		}
		if best == nil || moreSpecific(y, best) {
			best = y
		}
	}
	if best != nil {
		_, rule = moreSpecificReason(best, x)
	}
	return best, rule
}

// scopeSuperset reports whether every tag in sub also appears in super —
// i.e. super is a (non-strict) superset of sub, as sets. An empty sub is
// vacuously a subset of anything, including an empty super.
func scopeSuperset(super, sub []string) bool {
	set := make(map[string]struct{}, len(super))
	for _, t := range super {
		set[t] = struct{}{}
	}
	for _, t := range sub {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// Status returns every entry for the freshness/shadow report `cairn status`
// prints, reading index columns only instead of walking + parsing every body
// (crn-6az.6.1.5). It also backs Visible (via visibleFrom) and Prime (both
// its scope-mismatch diagnostic via scopeMismatchWarnings, crn-ln1, and its
// budgeted item list, which backs both text and --json rendering and needs
// Title/Summary/HitCount to render entries without a body read, crn-0vqk.2/
// crn-od2x.2). Check only ever reads e.Anchor, ShadowMap only ever reads ID,
// TopicKey, Scope, VerifiedAt, CreatedAt, and OverriddenDuplicateOf, and Prime
// additionally reads Title, Summary, and HitCount. EntriesByType requires Type
// for unattended maintenance. Those are the populated fields; CreatedBy, Body,
// and BodyPath remain zero-valued for every caller. OverriddenDuplicateOf was added for
// moreSpecificReason's override check (crn-3476/crn-zcxq FR-6): without it,
// every entry Visible/Prime ever see from this path has an empty override,
// silently no-opping the --force correction shadowReason is supposed to
// honor. Adding a column here is a deliberate, reviewed cost trade-off (these
// were already indexed and populated by reindexTx at zero marginal query
// cost) -- not a precedent for extending this list on request; any future
// addition needs the same analysis.
func Status(ctx context.Context, store string) ([]*Entry, error) {
	if err := ensureFresh(ctx, store); err != nil {
		return nil, err
	}
	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	tags, err := scopeTags(ctx, db)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `SELECT
		id, title, title_source, summary, summary_source, type, hit_count, topic_key, verified_at, created_at,
		anchor_type, anchor_repo, anchor_paths, anchor_spec, anchor_fingerprint,
		overridden_duplicate_of
		FROM entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*Entry
	for rows.Next() {
		e := &Entry{}
		var anchorPaths string
		if err := rows.Scan(
			&e.ID, &e.Title, &e.TitleSource, &e.Summary, &e.SummarySource, &e.Type,
			&e.HitCount, &e.TopicKey, &e.VerifiedAt, &e.CreatedAt,
			&e.Anchor.Type, &e.Anchor.Repo, &anchorPaths, &e.Anchor.Spec, &e.Anchor.Fingerprint,
			&e.OverriddenDuplicateOf,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(anchorPaths), &e.Anchor.Paths); err != nil {
			return nil, err
		}
		e.Scope = tags[e.ID]
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// EntriesByType returns every entry whose persisted content classification
// exactly matches entryType. The special value "unclassified" selects legacy
// entries with no Type. This is store-wide maintenance data, not identity-
// scoped retrieval.
func EntriesByType(ctx context.Context, store, entryType string) ([]*Entry, error) {
	entries, err := Status(ctx, store)
	if err != nil {
		return nil, err
	}
	want := entryType
	if entryType == "unclassified" {
		want = ""
	}
	out := make([]*Entry, 0)
	for _, e := range entries {
		if e.Type == want {
			out = append(out, e)
		}
	}
	return out, nil
}
