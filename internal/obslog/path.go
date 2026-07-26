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
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cairn", "debug.jsonl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("obslog: cannot resolve state directory ($XDG_STATE_HOME unset, $HOME unavailable)")
	}
	return filepath.Join(home, ".local", "state", "cairn", "debug.jsonl"), nil
}
