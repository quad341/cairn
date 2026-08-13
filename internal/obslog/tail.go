package obslog

import (
	"bytes"
	"encoding/json"
	"os"
)

// initialTailWindow is the first (and smallest) chunk Tail reads from EOF
// before deciding whether it needs to widen. Doubled on each retry.
const initialTailWindow = 8 * 1024

// Tail reads path (a JSONL log) from EOF backward and returns whole lines
// whose combined size fits within budgetBytes, oldest-of-the-tail-first. A
// budget smaller than even the newest line still returns that one line --
// the newest entry is never starved by budget, mirroring cairn.Prime's own
// byte-budget guard. Lines are kept as raw json.RawMessage rather than
// parsed into any typed record shape, so a new obslog record kind added
// later needs no change here. Returns (nil, nil) for an empty file.
func Tail(path string, budgetBytes int) ([]json.RawMessage, error) {
	f, err := os.Open(path) //nolint:gosec // path is caller-provided (a resolved log path), not attacker input
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	for window := int64(initialTailWindow); ; window *= 2 {
		full := window >= size
		if full {
			window = size
		}
		start := size - window

		buf := make([]byte, window)
		if _, err := f.ReadAt(buf, start); err != nil {
			return nil, err
		}
		segments := bytes.Split(bytes.TrimSuffix(buf, []byte("\n")), []byte("\n"))

		lines, truncatedByBudget := tailAccumulate(segments, budgetBytes)
		if full || truncatedByBudget {
			return lines, nil
		}
		// The window's leading segment may be a fragment of a longer line
		// that starts further back, but tailAccumulate consumed every
		// segment without hitting budget -- widen and rescan from scratch
		// rather than risk returning that fragment.
	}
}

// tailAccumulate walks segments (newest-last) from the end backward, keeping
// whole segments until the next one would push the running total over
// budgetBytes -- except the first (newest) segment, which is always kept
// regardless of budget. It returns the kept segments in their original
// (oldest-first) order, plus whether budget -- not the start of segments --
// is what stopped accumulation.
func tailAccumulate(segments [][]byte, budgetBytes int) (lines []json.RawMessage, truncatedByBudget bool) {
	kept := 0
	used := 0
	for i := len(segments) - 1; i >= 0; i-- {
		cost := len(segments[i]) + 1 // +1 for the newline that followed it
		if used+cost > budgetBytes && kept > 0 {
			break
		}
		used += cost
		kept++
	}

	start := len(segments) - kept
	lines = make([]json.RawMessage, kept)
	for i := range kept {
		lines[i] = json.RawMessage(append([]byte(nil), segments[start+i]...))
	}
	return lines, kept < len(segments)
}
