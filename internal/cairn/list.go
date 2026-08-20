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
	Conflict        *TopicConflict
	ReviewStatus    string
}

// ListByTopic returns the identity-scoped visible winner(s) for an exact
// topic key (FR-1, FR-3, FR-4, FR-6). It uses the same ResolveTopics path as
// prime, retaining and annotating every top-ranked revision when meaningful
// precedence cannot select one. Bounded to the matched result set only
// (NFR-1): body parsing scales with len(matches), never with store size.
// Never stamps hit_count/last_recalled_at (resolved open question 2) -- only
// a subsequent cairn get <id> is a confirmed recall.
func ListByTopic(ctx context.Context, store, topicKey string, identity []string) ([]ListingEntry, error) {
	all, err := Status(ctx, store)
	if err != nil {
		return nil, err
	}
	resolution := resolveVisibleFrom(ctx, all, identity)

	var matches []ResolvedEntry
	for _, resolved := range resolution.Entries {
		if resolved.Entry.TopicKey == topicKey {
			matches = append(matches, resolved)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}

	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.Entry.ID
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
		full, err := ParseEntry(resolveBodyPath(store, paths[m.Entry.ID]))
		if err != nil {
			return nil, err
		}
		state, detail := Check(ctx, full)
		out = append(out, ListingEntry{
			ID:              full.ID,
			Title:           full.Title,
			Summary:         full.Summary,
			Scope:           m.Entry.Scope,
			FreshnessState:  state,
			FreshnessDetail: detail,
			Conflict:        m.Conflict,
			ReviewStatus:    full.ReviewStatus,
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
