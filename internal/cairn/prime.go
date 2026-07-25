package cairn

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	ID        string        `json:"id"`
	TopicKey  string        `json:"topic_key"`
	Title     string        `json:"title"`
	Summary   string        `json:"summary,omitempty"`
	HitCount  int           `json:"hit_count"`
	Freshness FreshnessInfo `json:"freshness"`
}

// PrimeResult is Prime's structured output: a budget-bounded,
// deterministically ordered slice of the caller's visible entries, plus
// aggregate freshness counts that always cover the entire visible set
// regardless of what the byte budget let through (crn-0vqk FR-2, FR-3).
type PrimeResult struct {
	Store          string      `json:"store"`
	Identity       []string    `json:"identity"`
	Items          []PrimeItem `json:"items"`
	TotalVisible   int         `json:"total_visible"`
	TruncatedCount int         `json:"truncated_count"`
	FreshCount     int         `json:"fresh_count"`
	StaleCount     int         `json:"stale_count"`
	UnknownCount   int         `json:"unknown_count"`
	ChecksCapped   bool        `json:"checks_capped"`
	Warnings       []string    `json:"warnings,omitempty"`
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
	visible := visibleFrom(all, identity)

	ordered := make([]*Entry, len(visible))
	copy(ordered, visible)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.HitCount != b.HitCount {
			return a.HitCount > b.HitCount
		}
		if a.CreatedAt != b.CreatedAt {
			// CreatedAt is stamped with time.DateOnly (see remember.go);
			// lexicographic order matches chronological order for that
			// format, so no time parsing is needed here.
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID < b.ID
	})

	result := PrimeResult{
		Store:        store,
		Identity:     identity,
		TotalVisible: len(ordered),
		Warnings:     scopeMismatchWarnings(all, visible, identity),
	}

	budget := &freshnessBudget{remaining: maxFreshnessChecksPerPrime}
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
			ID:        e.ID,
			TopicKey:  e.TopicKey,
			Title:     e.Title,
			Summary:   e.Summary,
			HitCount:  e.HitCount,
			Freshness: FreshnessInfo{Status: status, Detail: detail},
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
		if usedBytes+cost > budgetBytes && len(result.Items) > 0 {
			truncating = true
			continue
		}
		usedBytes += cost
		result.Items = append(result.Items, item)
	}
	result.ChecksCapped = budget.capped
	result.TruncatedCount = result.TotalVisible - len(result.Items)

	return result, nil
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
			topic := it.TopicKey
			if topic == "" {
				topic = UntopicedLabel
			}
			fmt.Fprintf(&b, "  %-12s %-30s hits:%-4d %-7s %s\n", it.ID, topic, it.HitCount, it.Freshness.Status, it.Title)
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
	b.WriteString("Capture what you learn: `cairn remember <body>` writes a new entry (private tier\n" +
		"commits directly; shared tiers route through review — see `cairn remember --help`\n" +
		"and DESIGN.md §6-§7).\n")
	return b.String()
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
