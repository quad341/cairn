package obslog

import (
	"path/filepath"
	"testing"
)

func TestLogPathPrefersXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	got, err := LogPath()
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	want := filepath.Join("/xdg/state", "cairn", "debug.jsonl")
	if got != want {
		t.Errorf("LogPath() = %q, want %q", got, want)
	}
}

func TestLogPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/agent")
	got, err := LogPath()
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	want := filepath.Join("/home/agent", ".local", "state", "cairn", "debug.jsonl")
	if got != want {
		t.Errorf("LogPath() = %q, want %q", got, want)
	}
}

func TestLogPathErrorsWhenUnresolvable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := LogPath(); err == nil {
		t.Error("LogPath() with no XDG_STATE_HOME and no HOME: expected an error, got nil")
	}
}
