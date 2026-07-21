package cairn

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NewEntry constructs a new entry for `cairn remember`: a contributor's
// freeform write, not yet curator-normalized (DESIGN.md §6). id combines
// topicKey with a random suffix -- never just topicKey, since several
// entries may deliberately share one topic_key (that's the whole point:
// shadow() picks the most specific at read time, DESIGN.md §3). title is
// body's first line, a scannable heading for status output; summary is the
// full trimmed body, so the two are often identical for remember's typical
// one-liner input.
func NewEntry(topicKey string, scope []string, body, createdBy string) (*Entry, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	title, summary := titleAndSummary(body)
	return &Entry{
		ID:        topicKey + "-" + suffix,
		Title:     title,
		Summary:   summary,
		TopicKey:  topicKey,
		Scope:     scope,
		Anchor:    Anchor{Type: "none"},
		CreatedBy: createdBy,
		CreatedAt: time.Now().Format(time.DateOnly),
		Body:      body,
	}, nil
}

func titleAndSummary(body string) (title, summary string) {
	trimmed := strings.TrimSpace(body)
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		return strings.TrimSpace(trimmed[:i]), trimmed
	}
	return trimmed, trimmed
}

func randomSuffix() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create places a brand-new entry in the store: it derives the file's
// location from e.Scope (the DESIGN.md §2 tiers) and e.ID, creates the
// scope-tier directory if needed -- WriteBack does not -- and writes it.
func (e *Entry) Create(store string) error {
	dir := scopeDir(store, e.Scope)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	e.BodyPath = filepath.Join(dir, e.ID+".md")
	return e.WriteBack()
}

// scopeDir maps scope tags to their DESIGN.md §2 directory. An empty scope
// (or one with no rig:/role:/agent: tag) is filed under global/; otherwise
// the first matching tier in rig > role > agent order wins, using the tag's
// value (the part after the colon) as the subdirectory name.
func scopeDir(store string, scope []string) string {
	for _, tier := range scopeDirs[1:] { // rig, role, agent -- global is the fallback
		for _, tag := range scope {
			if val, ok := strings.CutPrefix(tag, tier+":"); ok {
				return filepath.Join(store, tier, val)
			}
		}
	}
	return filepath.Join(store, "global")
}
