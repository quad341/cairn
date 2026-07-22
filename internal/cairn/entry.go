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
	"sort"
	"strings"

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
	HitCount   int      `toml:"hit_count,omitempty"`

	BodyPath string `toml:"-"`
	Body     string `toml:"-"`
}

var scopeDirs = []string{"global", "rig", "role", "agent"}

// ParseEntry reads a markdown file with TOML frontmatter (+++ fences). It
// returns errNotEntry for files that carry no frontmatter or no id.
func ParseEntry(path string) (*Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	if !strings.HasPrefix(text, fence) {
		return nil, errNotEntry
	}
	rest := text[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return nil, fmt.Errorf("%s: unterminated +++ frontmatter", path)
	}
	front := rest[:end]
	body := strings.TrimLeft(rest[end+len("\n"+fence):], "\n")

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

// WriteBack re-serializes the frontmatter (+++), preserving the body.
func (e *Entry) WriteBack() error {
	content, err := e.marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(e.BodyPath, content, 0o600)
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

// Find returns the entry with the given id, or ErrNotFound.
func Find(store, id string) (*Entry, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, ErrNotFound
}

// Visible returns entries an identity may see: every scope-tag on the entry
// must be satisfied by the identity (a subset match). Global (untagged)
// entries are visible to all. When multiple visible entries share a
// non-empty topic_key, only the most specific one is returned — CSS-style
// shadowing (DESIGN.md §3).
func Visible(store string, identity []string) ([]*Entry, error) {
	entries, err := IterEntries(store)
	if err != nil {
		return nil, err
	}
	idset := make(map[string]struct{}, len(identity))
	for _, t := range identity {
		idset[t] = struct{}{}
	}
	var out []*Entry
	for _, e := range entries {
		ok := true
		for _, tag := range e.Scope {
			if _, has := idset[tag]; !has {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, e)
		}
	}
	return shadow(out), nil
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
// (crn-6az.6.1.5). Check only ever reads e.Anchor, and ShadowMap only ever
// reads ID, TopicKey, Scope, VerifiedAt, and CreatedAt, so those are the only
// fields populated here — Title, Summary, Type, CreatedBy, HitCount, Body,
// and BodyPath are left zero-valued, mirroring the judgment call Visible
// made in crn-6az.6.1.4.
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
