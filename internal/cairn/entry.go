// Package cairn implements the knowledge store — entries (markdown bodies with
// TOML frontmatter), the rebuildable SQLite index, and source-anchored freshness.
package cairn

import (
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
	var sb strings.Builder
	sb.WriteString(fence + "\n")
	if err := toml.NewEncoder(&sb).Encode(e); err != nil {
		return err
	}
	sb.WriteString(fence + "\n\n")
	sb.WriteString(strings.TrimLeft(e.Body, "\n"))
	return os.WriteFile(e.BodyPath, []byte(sb.String()), 0o600)
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
