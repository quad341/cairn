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
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const fence = "+++"

// constError is an immutable, comparable sentinel error usable as a const.
type constError string

func (e constError) Error() string { return string(e) }

// ErrNotFound is returned by Find when no entry has the requested id.
const ErrNotFound constError = "entry not found"

// errNotEntry marks a markdown file that carries no cairn frontmatter.
const errNotEntry constError = "not a cairn entry"

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

	patched, err := patchVerification(front, e.VerifiedAt, e.Anchor.Fingerprint)
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

// IterEntries walks the scope dirs and returns all entries, sorted by id.
func IterEntries(store string) ([]*Entry, error) {
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
				return perr
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
	return visibleFrom(all, identity), nil
}

// visibleFrom applies Visible's subset-match + shadowing rule to an
// already-loaded entry list. Factored out of Visible so callers that also
// need the full unfiltered list (e.g. Prime's scope-mismatch diagnostic,
// crn-ln1) can load the store once via Status and derive both from a single
// pass, instead of Visible re-querying the index a second time.
func visibleFrom(entries []*Entry, identity []string) []*Entry {
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
	return shadow(out)
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

// shadow resolves topic_key conflicts by specificity: the entry with the most
// scope tags wins (CSS-style, DESIGN.md §3). Ties break on most-recent
// VerifiedAt, then most-recent CreatedAt, then lowest ID, so resolution is
// always deterministic regardless of which timestamp fields are populated.
// Entries without a topic_key never shadow one another.
func shadow(candidates []*Entry) []*Entry {
	winner := make(map[string]*Entry, len(candidates))
	for _, e := range candidates {
		if e.TopicKey == "" {
			continue
		}
		if cur, ok := winner[e.TopicKey]; !ok || moreSpecific(e, cur) {
			winner[e.TopicKey] = e
		}
	}
	out := make([]*Entry, 0, len(candidates))
	for _, e := range candidates {
		if e.TopicKey == "" || winner[e.TopicKey] == e {
			out = append(out, e)
		}
	}
	return out
}

// moreSpecific reports whether a should win over b for a shared topic_key.
func moreSpecific(a, b *Entry) bool {
	if len(a.Scope) != len(b.Scope) {
		return len(a.Scope) > len(b.Scope)
	}
	if a.VerifiedAt != b.VerifiedAt {
		return a.VerifiedAt > b.VerifiedAt // ISO-8601 strings sort lexically = chronologically
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return a.ID < b.ID
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
func ShadowMap(entries []*Entry) map[string]*Entry {
	byTopic := make(map[string][]*Entry)
	for _, e := range entries {
		if e.TopicKey == "" {
			continue
		}
		byTopic[e.TopicKey] = append(byTopic[e.TopicKey], e)
	}

	out := make(map[string]*Entry)
	for _, group := range byTopic {
		if len(group) < 2 {
			continue // a topic_key held by only one entry can't be shadowed
		}
		for _, x := range group {
			if best := bestShadower(x, group); best != nil {
				out[x.ID] = best
			}
		}
	}
	return out
}

// bestShadower returns the single most-specific entry in group that shadows
// x (see ShadowMap's doc comment for the shadowing rule), or nil if none
// qualifies.
func bestShadower(x *Entry, group []*Entry) *Entry {
	var best *Entry
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
	return best
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
// (crn-6az.6.1.5). It also backs Visible (via visibleFrom) and Prime's
// scope-mismatch diagnostic (via scopeMismatchWarnings, crn-ln1), which need
// the same bulk index read pre- and post-identity-filtering respectively.
// Check only ever reads e.Anchor, ShadowMap only ever reads ID, TopicKey,
// Scope, VerifiedAt, and CreatedAt, and Visible/scopeMismatchWarnings only
// ever read those same five -- so those are the only fields populated here;
// Title, Summary, Type, CreatedBy, HitCount, Body, and BodyPath are left
// zero-valued for every caller.
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
		id, topic_key, verified_at, created_at,
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
			&e.ID, &e.TopicKey, &e.VerifiedAt, &e.CreatedAt,
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
