package cairn

import (
	"context"
	"database/sql"
	"strings"
)

// ListingEntry is one visible winner for a topic-key match (FR-2): ID,
// title, summary, scope, and freshness. Body is deliberately absent --
// pulling it remains cairn get <id>'s job (DESIGN.md §5's "bodies on demand"
// split), so a multi-winner untopiced listing can't bloat context with
// bodies the task may not need.
type ListingEntry struct {
	ID              string
	Title           string
	Summary         string
	Scope           []string
	FreshnessState  string
	FreshnessDetail string
}

// ListByTopic returns the identity-scoped visible winner(s) for an exact
// topic key (FR-1, FR-3, FR-4, FR-6). Reuses Visible() unmodified -- the
// same identity-filter-then-shadow call map/prime already make -- so a
// topic-key match here can never disagree with what map/prime show for the
// same identity. Because Visible() applies shadow()'s topic_key precedence
// before returning, a non-empty topicKey yields at most one match; only
// topicKey == "" (the untopiced bucket) can yield many, since entries
// without a topic_key never shadow each other. Bounded to the matched
// result set only (NFR-1): body parsing scales with len(matches), never
// with store size. Never stamps hit_count/last_recalled_at (resolved open
// question 2) -- only a subsequent cairn get <id> is a confirmed recall.
func ListByTopic(ctx context.Context, store, topicKey string, identity []string) ([]ListingEntry, error) {
	visible, err := Visible(ctx, store, identity)
	if err != nil {
		return nil, err
	}

	var matches []*Entry
	for _, e := range visible {
		if e.TopicKey == topicKey {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}

	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
	}

	db, err := openDB(store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	paths, err := bodyPathsFor(ctx, db, ids)
	if err != nil {
		return nil, err
	}

	out := make([]ListingEntry, 0, len(matches))
	for _, m := range matches {
		full, err := ParseEntry(resolveBodyPath(store, paths[m.ID]))
		if err != nil {
			return nil, err
		}
		state, detail := Check(ctx, full)
		out = append(out, ListingEntry{
			ID:              full.ID,
			Title:           full.Title,
			Summary:         full.Summary,
			Scope:           m.Scope,
			FreshnessState:  state,
			FreshnessDetail: detail,
		})
	}
	return out, nil
}

// bodyPathsFor resolves body_path for a bounded set of ids in one query --
// findBodyPath's single-id point lookup, batched for ListByTopic's matched
// set instead of called once per id (NFR-1: one indexed query sized to the
// match count, not the store).
func bodyPathsFor(ctx context.Context, db *sql.DB, ids []string) (map[string]string, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	// The concatenated segment is always literal "?" placeholders, one per
	// id -- never derived from ids' contents. Actual values flow only
	// through the parameterized args... below, so there's no injection
	// vector here.
	//nolint:gosec // placeholders are literal "?"s; values are parameterized via args
	rows, err := db.QueryContext(ctx,
		`SELECT id, body_path FROM entries WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]string, len(ids))
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out[id] = path
	}
	return out, rows.Err()
}
