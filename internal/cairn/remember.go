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

// maxCreateAttempts bounds the ID-collision retry in Create.
const maxCreateAttempts = 5

// Create places a brand-new entry in the store: it derives the file's
// location from e.Scope (the DESIGN.md §2 tiers) and e.ID, creates the
// scope-tier directory if needed -- WriteBack does not -- and writes it.
// Unlike WriteBack, Create never overwrites an existing file: several
// entries may deliberately share one topic_key (see NewEntry), so a
// same-topic_key, same-scope suffix collision isn't a contrived scenario
// over a long-lived store. On collision it regenerates e.ID and retries,
// rather than silently destroying whatever entry is already at that path.
func (e *Entry) Create(store string) error {
	dir := scopeDir(store, e.Scope)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		e.BodyPath = filepath.Join(dir, e.ID+".md")
		content, err := e.marshal()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(e.BodyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(content)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			return werr
		}
		if !os.IsExist(err) || attempt >= maxCreateAttempts-1 {
			return err
		}
		suffix, err := randomSuffix()
		if err != nil {
			return err
		}
		e.ID = e.TopicKey + "-" + suffix
	}
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
