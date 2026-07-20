package cairn

import (
	"fmt"
	"sort"
	"strings"
)

// Prime renders an agent's always-in-context payload: a bounded topic map of its
// unioned scope plus a short usage preamble. It is meant to be injected at session
// start (e.g. via a SessionStart hook) so an agent boots aware of what it knows.
func Prime(store string, identity []string) (string, error) {
	entries, err := Visible(store, identity)
	if err != nil {
		return "", err
	}
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
	b.WriteString("Capture what you learn: hand-author a `+++`-fenced markdown entry under the right scope dir\n" +
		"(DESIGN.md §2, §6-§7) — no `remember` command yet.\n")
	return b.String(), nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
