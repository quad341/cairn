package cairn

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxFreshnessChecksPerPrime bounds the number of real (git shell-out)
// freshness checks a single Prime call will perform, regardless of how many
// checkable entries are in scope (crn-0vqk FR-5): past the cap, remaining
// checkable entries are classified unknown rather than fabricated fresh. It
// is a var, not a const, so tests can retune it without needing dozens of
// git fixtures; production callers get the default below. This is a
// starting point, not a calibrated final answer -- recalibrate if real
// fleet stores show it's too tight or too loose.
var maxFreshnessChecksPerPrime = 50

// explorationSlots is the number of most-recently-created HitCount==0
// entries promoted to Prime's exploration band (band 0), ranked ahead of
// every other entry regardless of HitCount -- so a brand-new entry gets at
// least one prime cycle of visibility instead of being buried under a
// proven, high-hit_count entry it could otherwise never outrank on
// HitCount alone (crn-3476/crn-zcxq FR-5, Finding 3). A var, not a const,
// for the same reason as maxFreshnessChecksPerPrime above: a starting
// point, not a calibrated final answer.
var explorationSlots = 3

// freshnessCappedDetail is the detail Check would have produced if the
// freshness-check cap hadn't intervened first.
const freshnessCappedDetail = "not checked this pass (freshness-check budget reached)"

// FreshnessInfo is an entry's freshness classification as surfaced by Prime.
type FreshnessInfo struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// PrimeItem is one entry itemized in a PrimeResult.
type PrimeItem struct {
	ID           string         `json:"id"`
	TopicKey     string         `json:"topic_key"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary,omitempty"`
	HitCount     int            `json:"hit_count"`
	Freshness    FreshnessInfo  `json:"freshness"`
	Conflict     *TopicConflict `json:"conflict,omitempty"`
	ReviewStatus string         `json:"review_status"`
}

// TopicCount is one topic_key's entry count in a PrimeResult's index view
// (crn-3476 FR-1/FR-2). An entry with no topic_key is counted under
// UntopicedLabel rather than dropped, so the breakdown's counts always sum
// to TotalVisible.
type TopicCount struct {
	TopicKey string `json:"topic_key"`
	Count    int    `json:"count"`
}

// PrimeResult is Prime's structured output: a budget-bounded,
// deterministically ordered slice of the caller's visible entries, plus an
// index view -- TotalVisible and the aggregate freshness counts -- that
// always covers the entire visible set regardless of what the byte budget
// let through into Items (crn-0vqk FR-2, FR-3; crn-3476 FR-1, FR-2).
//
// TopicCounts is the one part of that index view which is NOT unconditional:
// it is an enumeration, so its size grows with the number of distinct topic
// keys, and crn-s3749 measured it at 88.2% of a payload running 2.5x over
// budget. It is now bounded like Items, with TopicCountsTruncated reporting
// what the cap dropped. The scalars stay unconditional because they are O(1)
// bytes -- bounding those would turn an honest overrun into an undercount.
type PrimeResult struct {
	Store        string       `json:"store"`
	Identity     []string     `json:"identity"`
	Items        []PrimeItem  `json:"items"`
	TotalVisible int          `json:"total_visible"`
	TopicCounts  []TopicCount `json:"topic_counts"`
	// TopicCountsTruncated is how many topic keys the byte budget kept out of
	// TopicCounts. Non-zero means the index enumeration is PARTIAL; the
	// aggregate scalars above still describe the whole visible set.
	TopicCountsTruncated int             `json:"topic_counts_truncated"`
	TruncatedCount       int             `json:"truncated_count"`
	FreshCount           int             `json:"fresh_count"`
	StaleCount           int             `json:"stale_count"`
	UnknownCount         int             `json:"unknown_count"`
	ChecksCapped         bool            `json:"checks_capped"`
	Warnings             []string        `json:"warnings"`
	Conflicts            []TopicConflict `json:"conflicts,omitempty"`
}

// Prime computes an agent's always-in-context payload: a budget-bounded,
// deterministically ordered map of its unioned scope, plus aggregate
// freshness counts over the whole visible set. It is meant to be injected at
// session start (e.g. via a SessionStart hook), so its cost is bounded on
// two independent axes regardless of store size: itemized payload bytes
// (budgetBytes) and freshness-check git shell-outs
// (maxFreshnessChecksPerPrime). Rendering (human text or --json) is a
// separate step over the returned PrimeResult -- see RenderPrimeText.
func Prime(ctx context.Context, store string, identity []string, budgetBytes int) (PrimeResult, error) {
	all, err := Status(ctx, store)
	if err != nil {
		return PrimeResult{}, err
	}
	resolution := resolveVisibleFrom(ctx, all, identity)
	visible := make([]*Entry, len(resolution.Entries))
	conflictsByID := make(map[string]*TopicConflict)
	for i, resolved := range resolution.Entries {
		visible[i] = resolved.Entry
		if resolved.Conflict != nil {
			conflictsByID[resolved.Entry.ID] = resolved.Conflict
		}
	}

	ordered := orderByPriority(visible)

	result := PrimeResult{
		Store:        store,
		Identity:     identity,
		TotalVisible: len(ordered),
		Warnings:     scopeMismatchWarnings(all, visible, identity),
		Conflicts:    resolution.Conflicts,
	}

	budget := &freshnessBudget{remaining: maxFreshnessChecksPerPrime}
	itemBudget := itemBudgetFor(budgetBytes)
	usedBytes := 0
	truncating := false
	for _, e := range ordered {
		status, detail := budget.classify(ctx, e)
		switch status {
		case Fresh:
			result.FreshCount++
		case Stale:
			result.StaleCount++
		default:
			result.UnknownCount++
		}
		// Truncation never affects the aggregate counts above -- they're
		// computed over every visible entry regardless of whether it's
		// itemized below, even after truncating has latched true.
		if truncating {
			continue
		}

		item := PrimeItem{
			ID:           e.ID,
			TopicKey:     e.TopicKey,
			Title:        truncateRunes(e.Title, titleCap),
			Summary:      truncateRunes(e.Summary, summaryCap),
			HitCount:     e.HitCount,
			Freshness:    FreshnessInfo{Status: status, Detail: detail},
			Conflict:     conflictsByID[e.ID],
			ReviewStatus: e.ReviewStatus,
		}
		// Once an entry doesn't fit in the remaining budget, stop itemizing
		// entirely rather than skipping ahead to try later, lower-priority
		// entries -- otherwise a later entry with a smaller byte cost could
		// slip into Items past a pricier, higher-priority one that got
		// skipped, silently violating the priority order truncation is
		// supposed to respect. Always itemize at least one entry (the
		// len==0 guard) so a budget too small for even one item doesn't
		// leave a caller with zero items despite a nonzero visible count.
		cost := itemByteCost(item)
		if usedBytes+cost > itemBudget && len(result.Items) > 0 {
			truncating = true
			continue
		}
		usedBytes += cost
		result.Items = append(result.Items, item)
	}
	result.ChecksCapped = budget.capped
	result.TruncatedCount = result.TotalVisible - len(result.Items)

	applyIndexBudget(&result, visible, budgetBytes-usedBytes)

	return result, nil
}

// applyIndexBudget fills in the topic index from whatever the items left
// unspent, and records what the cap dropped. Items keep first claim because
// they are the ranked, high-salience payload; the index is for routing.
//
// Split out of Prime rather than inlined so Prime stays under the cyclomatic
// complexity limit, and so the "report what you dropped" half sits next to the
// truncation that makes it necessary.
func applyIndexBudget(result *PrimeResult, visible []*Entry, allowance int) {
	// Clamp at zero: the always-itemize-one guard in Prime can push usedBytes
	// past itemBudget on a tiny budget, and the index must not then be handed
	// a negative allowance.
	if allowance < 0 {
		allowance = 0
	}
	allTopics := topicCounts(visible)
	result.TopicCounts, result.TopicCountsTruncated = boundTopicCounts(allTopics, allowance)
	if result.TopicCountsTruncated == 0 {
		return
	}
	result.Warnings = append(result.Warnings, fmt.Sprintf(
		"topic index truncated by the byte budget: %d of %d topic keys not listed (aggregate counts still cover all %d entries)",
		result.TopicCountsTruncated, len(allTopics), result.TotalVisible))
}

// orderByPriority returns visible in the order Items should be itemized: the
// exploration band first, then most-hit, then newest, then ID as a deterministic
// tiebreak. Split out of Prime to keep it inside the funlen limit; the ordering
// is load-bearing because byte-budget truncation walks this slice in order and
// stops at the first entry that does not fit.
func orderByPriority(visible []*Entry) []*Entry {
	ordered := make([]*Entry, len(visible))
	copy(ordered, visible)
	explorationBand := explorationBandIDs(ordered, explorationSlots)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if aBand, bBand := explorationBandRank(a, explorationBand), explorationBandRank(b, explorationBand); aBand != bBand {
			return aBand < bBand
		}
		if a.HitCount != b.HitCount {
			return a.HitCount > b.HitCount
		}
		if a.CreatedAt != b.CreatedAt {
			// CreatedAt is stamped with time.RFC3339 (see remember.go);
			// lexicographic order matches chronological order for that
			// format, so no time parsing is needed here.
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID < b.ID
	})
	return ordered
}

// itemBudgetFor carves the index reserve OUT of budgetBytes before itemizing,
// rather than adding it on top -- otherwise Items could spend the whole budget
// and the reserve would push the total over, which is crn-s3749 in a different
// place. Items keep first claim on everything above the reserve, and anything
// they leave unspent flows back to the index (see applyIndexBudget).
func itemBudgetFor(budgetBytes int) int {
	return budgetBytes - budgetBytes/indexReserveDivisor
}

// indexReserveDivisor sets the share of budgetBytes withheld from Items so the
// topic index cannot be starved to nothing by a store with many entries.
const indexReserveDivisor = 4

// boundTopicCounts caps the topic enumeration at allowance bytes, returning the
// kept rows and the number dropped.
//
// Ordering is count-descending: a topic holding more than one entry is the only
// row carrying information the per-entry Items list does not already have, so
// those must survive a cap that singletons do not. Ties break on topic_key so
// output stays deterministic.
func boundTopicCounts(all []TopicCount, allowance int) ([]TopicCount, int) {
	ranked := append([]TopicCount(nil), all...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].TopicKey < ranked[j].TopicKey
	})

	used, kept := 0, 0
	for _, tc := range ranked {
		b, _ := json.Marshal(tc)
		if used+len(b) > allowance {
			break
		}
		used += len(b)
		kept++
	}
	out := ranked[:kept]
	// Deterministic, human-scannable output: rank decides WHICH rows survive,
	// topic_key decides what order they print in -- matching the key-sorted
	// contract callers had before this cap existed.
	sort.SliceStable(out, func(i, j int) bool { return out[i].TopicKey < out[j].TopicKey })
	return out, len(all) - kept
}

// topicCounts reduces entries to a per-topic_key breakdown for Prime's index
// view (crn-3476 FR-1/FR-2): a pure in-memory map reduction over the
// already-loaded visible set, no new query and no I/O (NFR-2), so this is as
// cheap as TotalVisible and independent of what the byte budget lets into
// Items. An entry with no topic_key still counts, under UntopicedLabel,
// rather than silently vanishing from the index (crn-zcxq Finding 1). The
// result is sorted by topic_key for deterministic --json output.
func topicCounts(entries []*Entry) []TopicCount {
	counts := map[string]int{}
	for _, e := range entries {
		key := e.TopicKey
		if key == "" {
			key = UntopicedLabel
		}
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]TopicCount, len(keys))
	for i, k := range keys {
		out[i] = TopicCount{TopicKey: k, Count: counts[k]}
	}
	return out
}

// truncateRunes bounds s to at most n runes. It makes a PrimeItem's
// Title/Summary -- and therefore itemByteCost -- a provable constant
// ceiling even when an on-disk entry predates titleCap/summaryCap, or was
// hand-written to the store directly, bypassing cmd/remember's write-time
// validation (crn-3476 FR-3, NFR-3).
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// truncateWords bounds s to at most n runes, the same as truncateRunes, but
// breaks at the nearest word boundary before the cut and appends an
// ellipsis rather than cutting mid-word -- this is what NewEntry and
// DerivedTitleSummary use for auto-derived Title/Summary (crn-q08yt), since
// those values are lifted verbatim from a contributor's body text and a
// mid-word cut there reads as a bug, not a bound. truncateRunes itself stays
// a plain hard cut: Prime's read-time re-truncation (NFR-3) needs a cheap,
// provable ceiling over arbitrary on-disk data regardless of shape, not
// word-aware parsing.
func truncateWords(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 1 {
		// No room for both a word-boundary cut and an ellipsis rune --
		// fall back to the same hard cut truncateRunes would produce.
		return truncateRunes(s, n)
	}
	r := []rune(s)
	cut := r[:n-1] // reserve one rune for the ellipsis
	// Only back up to the previous word boundary if the cut actually split
	// a word -- i.e. both the last rune kept and the first rune dropped are
	// non-space. If the boundary already fell on whitespace, the cut is
	// already clean and backing up would discard a whole word that fit.
	if !unicode.IsSpace(cut[len(cut)-1]) && !unicode.IsSpace(r[n-1]) {
		lastSpace := -1
		for i := len(cut) - 1; i >= 0; i-- {
			if unicode.IsSpace(cut[i]) {
				lastSpace = i
				break
			}
		}
		if lastSpace > 0 {
			cut = cut[:lastSpace]
		}
	}
	return strings.TrimRight(string(cut), " \t\n") + "…"
}

// explorationBandIDs returns the IDs of the slots most-recently-created
// entries with HitCount == 0 -- the set Prime promotes to band 0 (see
// explorationBandRank). Ties in CreatedAt break by ID descending purely to
// keep the selection deterministic; slots is typically small enough
// (explorationSlots defaults to 3) that a tie deciding which entry just
// misses the cutoff is not expected to matter in practice.
func explorationBandIDs(entries []*Entry, slots int) map[string]bool {
	var candidates []*Entry
	for _, e := range entries {
		if e.HitCount == 0 {
			candidates = append(candidates, e)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID > b.ID
	})
	if len(candidates) > slots {
		candidates = candidates[:slots]
	}
	ids := make(map[string]bool, len(candidates))
	for _, e := range candidates {
		ids[e.ID] = true
	}
	return ids
}

// explorationBandRank is Prime's sort's leading key: 0 for an entry in the
// exploration band (ranks ahead of everything else regardless of HitCount),
// 1 otherwise.
func explorationBandRank(e *Entry, band map[string]bool) int {
	if band[e.ID] {
		return 0
	}
	return 1
}

// itemByteCost is a PrimeItem's contribution to the byte budget: its JSON
// encoding, the same bytes a --json caller would actually receive, so the
// budget bounds real payload size rather than an unrelated proxy. PrimeItem
// has no type that can fail to marshal (strings, ints, a nested struct of
// the same), so the error is never non-nil in practice.
func itemByteCost(item PrimeItem) int {
	b, _ := json.Marshal(item)
	return len(b)
}

// freshnessBudget gates Check's git shell-out behind a bounded per-Prime-call
// cap (crn-0vqk FR-5). Check always attempts the shell-out for a files anchor
// with repo+paths set -- regardless of whether a fingerprint was ever stored,
// since a genuine invocation failure must surface as Incomplete even for a
// never-verified anchor (crn-fdjc.1.1) -- so that shape is the only case this
// gates; every other classification (including a bare "commit" anchor, which
// never shells out) just delegates straight to Check at no budget cost.
type freshnessBudget struct {
	remaining int
	capped    bool
}

func (b *freshnessBudget) classify(ctx context.Context, e *Entry) (string, string) {
	a := e.Anchor
	if a.Type == "files" && a.Repo != "" && len(a.Paths) > 0 {
		if b.remaining <= 0 {
			b.capped = true
			return Unknown, freshnessCappedDetail
		}
		b.remaining--
	}
	return Check(ctx, e)
}

// RenderPrimeText renders a PrimeResult as the human-readable text `cairn
// prime` prints by default. All truncation and freshness-check decisions are
// already baked into r by Prime; this only formats.
func RenderPrimeText(r PrimeResult) string {
	scope := "global"
	if len(r.Identity) > 0 {
		scope = strings.Join(r.Identity, " ")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# cairn — %d entr%s in your scope (%s)\n\n", r.TotalVisible, plural(r.TotalVisible), scope)
	if r.TotalVisible == 0 {
		b.WriteString("No cached knowledge in your scope yet.\n")
	} else {
		b.WriteString("Entries in your scope, most-recalled first (pull a body with `cairn get <id>`):\n")
		for _, it := range r.Items {
			renderPrimeItem(&b, it)
		}
		if r.TruncatedCount > 0 {
			fmt.Fprintf(&b, "  ... %d more entr%s truncated by the byte budget\n", r.TruncatedCount, plural(r.TruncatedCount))
		}
		fmt.Fprintf(&b, "\n%d fresh, %d stale, %d unknown (of %d total)\n", r.FreshCount, r.StaleCount, r.UnknownCount, r.TotalVisible)
		if r.ChecksCapped {
			b.WriteString("note: freshness-check budget reached this pass; some entries reported unknown rather than being verified.\n")
		}
		b.WriteString("\nEntries can go stale — treat a stale entry as a lead, not truth.\n")
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "\n%s\n", w)
	}
	// Anchor the write advice, not just the write verb (crn-5wus). This
	// footer is the only cairn text every agent reads every session, and
	// while it named `remember` alone that is the form agents wrote: after
	// the flat-file migration -- which had no anchor concept to inherit --
	// newly created entries were still 84% unanchored. An unanchored entry
	// can only ever report time-based freshness, so the paragraph above
	// warning that entries go stale was describing a hazard whose remedy
	// went unmentioned. Name the remedy in the same breath as the warning.
	b.WriteString("Capture what you learn: `cairn remember <body>` writes a new entry (private tier\n" +
		"commits directly; shared tiers route through review — see `cairn remember --help`\n" +
		"and DESIGN.md §6-§7).\n" +
		"\nAnchor it to the source it describes:\n" +
		"  cairn remember <body> --topic <key> --anchor-repo <repo> --anchor-path <file> --verify\n" +
		"An anchored entry reports real freshness — \"fresh, anchor matches <sha>\" — instead of\n" +
		"guessing from its age. Skip the anchor only when there is no source file to point at\n" +
		"(operator preferences, facts about people).\n")
	return b.String()
}

// renderPrimeItem writes one entry's summary line -- plus its optional
// summary text and conflict note -- to b. Extracted from RenderPrimeText to
// keep that function's own if-nesting under the nestif threshold
// (crn-8lq2z).
func renderPrimeItem(b *strings.Builder, it PrimeItem) {
	topic := it.TopicKey
	if topic == "" {
		topic = UntopicedLabel
	}
	line := fmt.Sprintf("  %-12s %-30s hits:%-4d %-7s %s", it.ID, topic, it.HitCount, it.Freshness.Status, it.Title)
	if it.ReviewStatus == ReviewStatusPending {
		// Inline, not a standalone line like get's marker: prime renders
		// multiple entries per call, so a standalone marker line would be
		// ambiguous about which entry it belongs to (crn-evw98.1).
		line += " [PENDING REVIEW]"
	}
	b.WriteString(line + "\n")
	if it.Summary != "" {
		fmt.Fprintf(b, "      %s\n", it.Summary)
	}
	renderPrimeConflict(b, it.Conflict)
}

func renderPrimeConflict(b *strings.Builder, conflict *TopicConflict) {
	if conflict != nil {
		fmt.Fprintf(b, "      conflict: %s revisions: %s\n", conflict.Reason, strings.Join(conflict.EntryIDs, ", "))
	}
}

// scopeDimensionPrefixes are the scope-tag prefixes Visible does subset
// matching on (see entry.go's Scope doc, e.g. "rig:web"). "global" is
// excluded: global entries carry no tag at all, so there is no "global:"
// prefix that could go missing.
var scopeDimensionPrefixes = []string{"rig:", "role:", "agent:"}

// scopeMismatchWarnings flags a likely tag-shape mismatch between an
// identity and the store's scope tags (crn-ln1): for each scope-dimension
// prefix present in identity, if the store has any entry tagged in that
// dimension anywhere but none of them made it into visible, cairn prime
// would otherwise silently report a low or zero entry count with no signal
// that something (as opposed to nothing) is wrong. A dimension absent from
// the store entirely, or one where the match is simply non-empty, produces
// no warning.
func scopeMismatchWarnings(all, visible []*Entry, identity []string) []string {
	present := map[string]bool{}
	for _, tag := range identity {
		for _, prefix := range scopeDimensionPrefixes {
			if strings.HasPrefix(tag, prefix) {
				present[prefix] = true
			}
		}
	}

	var warnings []string
	for _, prefix := range scopeDimensionPrefixes {
		if !present[prefix] || !anyTagWithPrefix(all, prefix) || anyTagWithPrefix(visible, prefix) {
			continue
		}
		dim := strings.TrimSuffix(prefix, ":")
		warnings = append(warnings, fmt.Sprintf(
			"warning: your identity has a %s tag, and the store has %s-scoped entries, but none matched you -- check for a tag-shape mismatch",
			prefix, dim,
		))
	}
	return warnings
}

// anyTagWithPrefix reports whether any entry carries a scope tag with the
// given prefix.
func anyTagWithPrefix(entries []*Entry, prefix string) bool {
	for _, e := range entries {
		for _, tag := range e.Scope {
			if strings.HasPrefix(tag, prefix) {
				return true
			}
		}
	}
	return false
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
