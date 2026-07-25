package obslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// rotatingWriter is a size- and count-bounded io.Writer over a JSONL file:
// once a write would push the active file past maxBytes, the active file
// and any existing rotated siblings shift down a suffix (path -> path.1 ->
// path.2 -> ...), the oldest sibling beyond keep is dropped, and a fresh
// empty active file is opened in its place. Byte accounting is in-memory
// (no os.Stat per write beyond the one at construction). Rotation failures
// are swallowed and writing continues against whatever file handle is
// valid -- a debug logger must never block or fail the command it's
// attached to.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	f        *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64, keep int) (*rotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, keep: keep, f: f, size: info.Size()}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate shifts the active file through up to keep numbered siblings and
// reopens a fresh active file, writing a "rotation" marker as its first
// line. Best-effort throughout: on any failure it leaves w.f pointing at
// whatever file handle is valid so writes keep flowing, rather than
// propagating the error.
func (w *rotatingWriter) rotate() {
	fromSize := w.size
	if err := w.f.Close(); err != nil {
		return
	}
	_ = os.Remove(w.rotatedPath(w.keep))
	for i := w.keep - 1; i >= 0; i-- {
		_ = os.Rename(w.rotatedPath(i), w.rotatedPath(i+1))
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		if f2, err2 := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err2 == nil {
			w.f = f2
		}
		return
	}
	w.f = f
	w.size = int64(writeRotationRecord(f, fromSize, w.path))
}

func (w *rotatingWriter) rotatedPath(n int) string {
	if n == 0 {
		return w.path
	}
	return fmt.Sprintf("%s.%d", w.path, n)
}

type rotationRecord struct {
	TS               string `json:"ts"`
	Kind             string `json:"kind"`
	RotatedFromBytes int64  `json:"rotated_from_bytes"`
	NewFile          string `json:"new_file"`
}

// writeRotationRecord writes a single self-contained JSON line marking the
// rotation, bypassing slog entirely: this writer sits underneath the slog
// handler whose own Write call triggered rotate(), so logging back through
// that same handler here would reenter it mid-write. Best-effort only, per
// rotate's fail-open contract. Returns the number of bytes written.
func writeRotationRecord(f *os.File, fromBytes int64, path string) int {
	line, err := json.Marshal(rotationRecord{
		TS:               time.Now().UTC().Format(time.RFC3339Nano),
		Kind:             "rotation",
		RotatedFromBytes: fromBytes,
		NewFile:          path,
	})
	if err != nil {
		return 0
	}
	line = append(line, '\n')
	n, _ := f.Write(line)
	return n
}
