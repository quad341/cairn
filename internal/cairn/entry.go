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
	ID         string   `toml:"id"`
	Title      string   `toml:"title"`
	Summary    string   `toml:"summary,omitempty"`
	Type       string   `toml:"type,omitempty"`
	TopicKey   string   `toml:"topic_key,omitempty"`
	Scope      []string `toml:"scope,omitempty"` // tags, e.g. ["rig:web"]
	Anchor     Anchor   `toml:"anchor"`
	VerifiedAt string   `toml:"verified_at,omitempty"`
	CreatedBy  string   `toml:"created_by,omitempty"`
	CreatedAt  string   `toml:"created_at,omitempty"`
	HitCount   int      `toml:"hit_count,omitzero"`

	Kind            string `toml:"kind,omitempty"`             // "" (note, default) | "remediation"
	AutoActionable  bool   `toml:"auto_actionable,omitempty"`  // only for Kind=="remediation"; reviewer-granted, not self-declared
	RecurrenceCount int    `toml:"recurrence_count,omitzero"`  // incremented on exact topic_key match at capture time (crn-28ge.1.4)
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

// WriteBackRecurrenceCount surgically patches recurrence_count into the
// on-disk frontmatter -- the same "patch, don't re-encode" contract
// WriteBack uses for verified_at/fingerprint above. cmd/remember.go's
// capture-time recurrence path (crn-28ge.1.4) increments e.RecurrenceCount
// in memory on an exact topic_key match and calls this to persist it,
// without disturbing any other field a curator or a prior WriteBack already
// wrote. As with WriteBack, incrementing the field is the caller's
// responsibility; this only persists whatever value is already there.
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

	e, err := ParseEntry(bodyPath)
	if err != nil {
		return nil, err
	}

	// hit_count and last_recalled_at are index-only state (crn-6az.6.1.1,
	// crn-28ge.1.1): the freshly-parsed body's values are stale-by-construction,
	// so both are always overwritten with the authoritative post-write values
	// (same transaction, same RETURNING) rather than trusted from the file.
	now := time.Now().Format(time.RFC3339)
	err = db.QueryRowContext(ctx,
		`UPDATE entries SET hit_count = hit_count + 1, last_recalled_at = ? WHERE id = ? RETURNING hit_count, last_recalled_at`,
		now, id,
	).Scan(&e.HitCount, &e.LastRecalledAt)
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

	shadowed, reasons := shadowReason(out)
	logger := obslog.FromContext(ctx)
	for _, r := range reasons {
		logger.ShadowDecision(obslog.ShadowDecisionFields{
			Mode:     "identity",
			TopicKey: r.TopicKey,
			WinnerID: r.WinnerID,
			Rule:     r.Rule,
		})
	}
	return shadowed
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

// ShadowReason explains a single shadow-resolution decision: WinnerID is the
// entry that survived for TopicKey, and Rule names which moreSpecific
// tiebreak decided it ("scope_size", "verified_at", "created_at", or
// "id_tiebreak").
type ShadowReason struct {
	TopicKey string
	WinnerID string
	Rule     string
}

// shadow resolves topic_key conflicts by specificity: the entry with the most
// scope tags wins (CSS-style, DESIGN.md §3). Ties break on most-recent
// VerifiedAt, then most-recent CreatedAt, then lowest ID, so resolution is
// always deterministic regardless of which timestamp fields are populated.
// Entries without a topic_key never shadow one another.
func shadow(candidates []*Entry) []*Entry {
	out, _ := shadowReason(candidates)
	return out
}

// shadowReason is shadow's reason-carrying counterpart: alongside the
// surviving entries, it reports one ShadowReason per topic_key that had two
// or more candidates -- a topic_key held by a single candidate never
// produces a decision, since there was nothing to resolve.
func shadowReason(candidates []*Entry) ([]*Entry, []ShadowReason) {
	winner := make(map[string]*Entry, len(candidates))
	rule := make(map[string]string, len(candidates))
	count := make(map[string]int, len(candidates))
	for _, e := range candidates {
		if e.TopicKey == "" {
			continue
		}
		count[e.TopicKey]++
		cur, ok := winner[e.TopicKey]
		if !ok {
			winner[e.TopicKey] = e
			continue
		}
		more, r := moreSpecificReason(e, cur)
		if more {
			winner[e.TopicKey] = e
		}
		rule[e.TopicKey] = r
	}

	out := make([]*Entry, 0, len(candidates))
	for _, e := range candidates {
		if e.TopicKey == "" || winner[e.TopicKey] == e {
			out = append(out, e)
		}
	}

	var reasons []ShadowReason
	for topicKey, n := range count {
		if n < 2 {
			continue
		}
		reasons = append(reasons, ShadowReason{
			TopicKey: topicKey,
			WinnerID: winner[topicKey].ID,
			Rule:     rule[topicKey],
		})
	}
	return out, reasons
}

// moreSpecific reports whether a should win over b for a shared topic_key.
func moreSpecific(a, b *Entry) bool {
	more, _ := moreSpecificReason(a, b)
	return more
}

// moreSpecificReason is moreSpecific's reason-carrying counterpart: rule
// names which tiebreak decided the comparison ("scope_size", "verified_at",
// "created_at", or "id_tiebreak"), regardless of which of a/b it favors.
func moreSpecificReason(a, b *Entry) (more bool, rule string) {
	if len(a.Scope) != len(b.Scope) {
		return len(a.Scope) > len(b.Scope), "scope_size"
	}
	if a.VerifiedAt != b.VerifiedAt {
		return a.VerifiedAt > b.VerifiedAt, "verified_at" // ISO-8601 strings sort lexically = chronologically
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt, "created_at"
	}
	return a.ID < b.ID, "id_tiebreak"
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
// TopicKey, Scope, VerifiedAt, and CreatedAt, and Prime additionally reads
// Title, Summary, and HitCount -- so those are the only fields populated
// here; Type, CreatedBy, Body, and BodyPath are left zero-valued for every
// caller. Adding a column here is a deliberate, reviewed cost trade-off
// (these three were already indexed and populated by reindexTx at zero
// marginal query cost) -- not a precedent for extending this list on
// request; any future addition needs the same analysis.
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
		id, title, summary, hit_count, topic_key, verified_at, created_at,
		anchor_type, anchor_repo, anchor_paths, anchor_spec, anchor_fingerprint
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
			&e.ID, &e.Title, &e.Summary, &e.HitCount, &e.TopicKey, &e.VerifiedAt, &e.CreatedAt,
			&e.Anchor.Type, &e.Anchor.Repo, &anchorPaths, &e.Anchor.Spec, &e.Anchor.Fingerprint,
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
