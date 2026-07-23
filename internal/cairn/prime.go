package cairn

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Prime renders an agent's always-in-context payload: a bounded topic map of its
// unioned scope plus a short usage preamble. It is meant to be injected at session
// start (e.g. via a SessionStart hook) so an agent boots aware of what it knows.
func Prime(ctx context.Context, store string, identity []string) (string, error) {
	all, err := Status(ctx, store)
	if err != nil {
		return "", err
	}
	entries := visibleFrom(all, identity)

	counts := map[string]int{}
	for _, e := range entries {
		t := e.TopicKey
		if t == "" {
			t = "(untopiced)"
		}
		counts[t]++
	}
	topics := make([]string, 0, len(counts))
	for t := range counts {
		topics = append(topics, t)
	}
	sort.Strings(topics)

	scope := "global"
	if len(identity) > 0 {
		scope = strings.Join(identity, " ")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# cairn — %d entr%s in your scope (%s)\n\n", len(entries), plural(len(entries)), scope)
	if len(entries) == 0 {
		b.WriteString("No cached knowledge in your scope yet.\n")
	} else {
		b.WriteString("Topics you have cached knowledge on (pull a body with `cairn get <id>`):\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "  %-44s %d\n", t, counts[t])
		}
		b.WriteString("\nEntries can go stale — `cairn get` reports freshness; treat a stale entry as a lead, not truth.\n")
	}
	for _, w := range scopeMismatchWarnings(all, entries, identity) {
		fmt.Fprintf(&b, "\n%s\n", w)
	}
	b.WriteString("Capture what you learn: `cairn remember <body>` writes a new entry (private tier\n" +
		"commits directly; shared tiers route through review — see `cairn remember --help`\n" +
		"and DESIGN.md §6-§7).\n")
	return b.String(), nil
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
