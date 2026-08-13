package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestShellPortability runs scripts/test-shell-portability.sh, which guards
// the "sourced into the caller's shell" contract of rebase-resolve-lib.sh:
// the library must not use bash-4-only syntax (e.g. ${var,,}) or `local`
// names that collide with zsh special parameters (e.g. `local path`), since
// either fault is fatal-but-falsely-green when the library is sourced into a
// zsh caller (crn-k1hw). Hermetic: temp git repos only, no network/gh/model
// calls; live zsh layers skip (not fail) when zsh is unavailable.
func TestShellPortability(t *testing.T) {
	root := repoRoot(t)

	cmd := exec.CommandContext(t.Context(), filepath.Join(root, "scripts", "test-shell-portability.sh"))
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"TMPDIR=" + t.TempDir(),
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("test-shell-portability.sh failed: %v\n%s", err, out)
	}
}
