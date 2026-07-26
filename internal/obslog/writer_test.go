package obslog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterWritesToActiveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	w, err := newRotatingWriter(path, 1024, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	if _, err := w.Write([]byte("line one\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "line one\n" {
		t.Errorf("file contents = %q, want %q", got, "line one\n")
	}
}

func TestRotatingWriterUsesFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	w, err := newRotatingWriter(path, 1024, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	if _, err := w.Write([]byte("x\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want %o", perm, 0o600)
	}
}

func TestRotatingWriterRotatesPastThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	// Small threshold: each line is 6 bytes ("aaaaa\n"), maxBytes=10 means
	// the second write (would push 6+6=12 > 10) triggers rotation.
	w, err := newRotatingWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	line := []byte("aaaaa\n")
	if _, err := w.Write(line); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write(line); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	rotated, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("ReadFile .1: %v", err)
	}
	if !bytes.HasPrefix(rotated, []byte("aaaaa\n")) {
		t.Errorf(".1 contents = %q, want to start with %q", rotated, "aaaaa\n")
	}

	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile active: %v", err)
	}
	// Active file now holds a rotation marker line followed by the second write.
	if !strings.Contains(string(active), `"kind":"rotation"`) {
		t.Errorf("active file after rotation = %q, want a rotation marker line", active)
	}
	if !strings.Contains(string(active), "aaaaa") {
		t.Errorf("active file after rotation = %q, want it to contain the triggering write", active)
	}
}

func TestRotatingWriterKeepsOnlyConfiguredSiblings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	w, err := newRotatingWriter(path, 6, 2) // keep only 2 rotated siblings
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	line := []byte("aaaaaa\n") // 7 bytes, exceeds maxBytes=6 on every write after the first
	for i := range 5 {
		if _, err := w.Write(line); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("expected %s.3 to not exist (keep=2), stat err = %v", path, err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf("expected %s.2 to exist: %v", path, err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected %s.1 to exist: %v", path, err)
	}
}

func TestRotatingWriterInMemoryByteCounterSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	// Pre-seed the file with existing content before construction, so the
	// writer must pick up the starting size from Stat, not assume 0.
	if err := os.WriteFile(path, []byte("preexisting\n"), 0o600); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}
	w, err := newRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	if w.size != int64(len("preexisting\n")) {
		t.Errorf("initial size = %d, want %d", w.size, len("preexisting\n"))
	}
}

func TestRotationRecordIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonl")
	w, err := newRotatingWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	line := []byte("aaaaa\n")
	if _, err := w.Write(line); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write(line); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	firstLine := strings.SplitN(string(active), "\n", 2)[0]
	var rec map[string]any
	if err := json.Unmarshal([]byte(firstLine), &rec); err != nil {
		t.Fatalf("rotation marker line is not valid JSON: %v\nline: %s", err, firstLine)
	}
	if rec["kind"] != "rotation" {
		t.Errorf("rotation record kind = %v, want %q", rec["kind"], "rotation")
	}
}
