package cairn

import "context"

// Shape is a store's structural snapshot: how many entries live in each
// tier, how many bodies exist on disk in total, how many rows the index
// currently holds, and whether the two have drifted apart. It exists for
// `cairn rage`'s bundle (FR-7) -- a bug report needs "what does this store
// look like" independent of internal/cairn/doctor.go's Report, which is
// deliberately narrower and must not grow a field for this (Guardrail #3).
type Shape struct {
	TierCounts map[string]int
	BodyCount  int
	IndexCount int
	IndexDrift bool
}

// StoreShape computes a Shape from the store's bodies on disk and the
// index's current row count. It never rebuilds the index itself -- reading
// IndexCount as-is (rather than reindexing first) is what makes IndexDrift a
// meaningful signal instead of one that's always false. A malformed entry
// file never aborts the computation (IterEntriesTolerant), matching
// Diagnose's own tolerance (internal/cairn/doctor.go): rage's whole purpose
// is to still produce a useful bundle from an unhealthy store.
func StoreShape(ctx context.Context, store string) (Shape, error) {
	entries, _, err := IterEntriesTolerant(store)
	if err != nil {
		return Shape{}, err
	}

	tierCounts := make(map[string]int)
	for _, e := range entries {
		tier := entryTier(store, e)
		if tier == "" {
			continue
		}
		tierCounts[tier]++
	}

	indexCount, err := indexRowCount(ctx, store)
	if err != nil {
		return Shape{}, err
	}

	return Shape{
		TierCounts: tierCounts,
		BodyCount:  len(entries),
		IndexCount: indexCount,
		IndexDrift: indexCount != len(entries),
	}, nil
}

// indexRowCount returns the index's current row count in entries, treating
// a never-built index (no entries table yet) as zero rather than an error --
// mirrors indexStale's own "no watermark row" convention for a brand-new or
// never-reindexed store.
func indexRowCount(ctx context.Context, store string) (int, error) {
	db, err := openDB(store)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count); err != nil {
		return 0, nil
	}
	return count, nil
}
