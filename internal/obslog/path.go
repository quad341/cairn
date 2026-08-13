package obslog

import (
	"errors"
	"os"
	"path/filepath"
)

// LogPath resolves the JSONL debug log's path: $XDG_STATE_HOME/cairn/debug.jsonl,
// falling back to $HOME/.local/state/cairn/debug.jsonl. It returns an error
// only when neither can be resolved (no $XDG_STATE_HOME and no usable
// $HOME) -- callers treat that as fail-open (a no-op logger), never a hard
// error to the calling command.
func LogPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cairn", "debug.jsonl"), nil
}

// stateDir resolves the XDG state directory ($XDG_STATE_HOME, falling back
// to $HOME/.local/state) -- split out of LogPath so a caller that needs the
// state root itself, not specifically debug.jsonl's path, can reuse the same
// resolution instead of re-deriving it.
func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("obslog: cannot resolve state directory ($XDG_STATE_HOME unset, $HOME unavailable)")
	}
	return filepath.Join(home, ".local", "state"), nil
}
