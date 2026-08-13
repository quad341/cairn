package obslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "debug.jsonl")
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestTailEmptyFileReturnsNil(t *testing.T) {
	path := writeJSONL(t, nil)

	got, err := Tail(path, 1<<20)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for an empty file", got)
	}
}

func TestTailNonexistentFileReturnsError(t *testing.T) {
	_, err := Tail(filepath.Join(t.TempDir(), "does-not-exist.jsonl"), 1<<20)
	if err == nil {
		t.Fatal("Tail on a nonexistent path returned no error")
	}
}

func TestTailSingleLineNoTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"a"}`), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got, err := Tail(path, 1<<20)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(got), got)
	}
	if string(got[0]) != `{"kind":"a"}` {
		t.Errorf("got %q, want %q", got[0], `{"kind":"a"}`)
	}
}

// TestTailBudgetSmallerThanOneLineStillReturnsIt is the explicit acceptance
// case: a budget smaller than even the newest line must not starve Tail down
// to zero results (mirrors cairn.Prime's own byte-budget guard).
func TestTailBudgetSmallerThanOneLineStillReturnsIt(t *testing.T) {
	path := writeJSONL(t, []string{`{"kind":"first"}`, `{"kind":"second-and-newest"}`})

	got, err := Tail(path, 1)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1 (the newest line must never be starved by budget): %v", len(got), got)
	}
	if string(got[0]) != `{"kind":"second-and-newest"}` {
		t.Errorf("got %q, want the newest line", got[0])
	}
}

func TestTailReturnsOldestOfTailFirstWithinBudget(t *testing.T) {
	lines := []string{
		`{"kind":"one"}`,
		`{"kind":"two"}`,
		`{"kind":"three"}`,
		`{"kind":"four"}`,
	}
	path := writeJSONL(t, lines)

	// Room for exactly the newest two lines (plus their newlines), not the
	// third.
	budget := len(lines[2]) + 1 + len(lines[3]) + 1
	got, err := Tail(path, budget)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(got), got)
	}
	if string(got[0]) != lines[2] || string(got[1]) != lines[3] {
		t.Errorf("got %q, %q; want oldest-of-tail-first: %q, %q", got[0], got[1], lines[2], lines[3])
	}
}

func TestTailUnlimitedBudgetReturnsEveryLine(t *testing.T) {
	lines := []string{`{"a":1}`, `{"a":2}`, `{"a":3}`}
	path := writeJSONL(t, lines)

	got, err := Tail(path, 1<<20)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != len(lines) {
		t.Fatalf("got %d lines, want %d", len(got), len(lines))
	}
	for i, want := range lines {
		if string(got[i]) != want {
			t.Errorf("line %d = %q, want %q", i, got[i], want)
		}
	}
}

// TestTailAcrossManyLinesExercisesWindowGrowth forces Tail's read window to
// double repeatedly (8KiB start, this fixture runs well past that) rather
// than being satisfiable from the first window, and confirms the boundary
// between widened reads never drops or corrupts a line: every one of 2000
// lines comes back intact, in order, starting from the file's genuine first
// line.
func TestTailAcrossManyLinesExercisesWindowGrowth(t *testing.T) {
	const n = 2000
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"kind":"line","seq":%d,"pad":"%s"}`, i, strings.Repeat("x", 20))
	}
	path := writeJSONL(t, lines)

	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat fixture: %v", err)
	} else if info.Size() <= initialTailWindow {
		t.Fatalf("fixture is %d bytes, must exceed the %d-byte initial window to exercise growth", info.Size(), initialTailWindow)
	}

	// Effectively unlimited: forces a full-file read via repeated widening.
	got, err := Tail(path, 1<<30)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d lines, want %d", len(got), n)
	}
	for i, want := range lines {
		if string(got[i]) != want {
			t.Fatalf("line %d = %q, want %q", i, got[i], want)
		}
	}
}

func TestTailLinesAreValidJSON(t *testing.T) {
	path := writeJSONL(t, []string{`{"kind":"a"}`, `{"kind":"b"}`})

	got, err := Tail(path, 1<<20)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	for i, line := range got {
		var v map[string]any
		if err := json.Unmarshal(line, &v); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, line)
		}
	}
}
